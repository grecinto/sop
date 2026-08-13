package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
)

type sessionCognitiveDecision struct {
	Action         string
	Reason         string
	SuggestedPhase string
}

func (a *CopilotAgent) buildSessionCognitiveState(ctx context.Context, query string, actor string, turn int, phase string) *ai.SessionCognitiveState {
	payload := ai.GetSessionPayload(ctx)
	if payload == nil {
		return nil
	}

	state := payload.CognitiveState
	if state == nil {
		state = &ai.SessionCognitiveState{}
	}

	state.CurrentTurn = turn
	state.CurrentActor = strings.TrimSpace(actor)
	state.CurrentPhase = strings.TrimSpace(phase)
	state.CurrentQuery = strings.TrimSpace(query)
	if payload.ClarificationState != nil {
		state.PendingClarification = strings.EqualFold(strings.TrimSpace(payload.ClarificationState.Status), "pending") || strings.EqualFold(strings.TrimSpace(payload.ClarificationState.Status), "clarifying")
		state.ClarificationStatus = strings.TrimSpace(payload.ClarificationState.Status)
	} else {
		state.PendingClarification = false
		state.ClarificationStatus = ""
	}
	state.MemorySignals = a.collectSessionMemorySignals(ctx)
	payload.CognitiveState = state
	return state
}

func (a *CopilotAgent) buildSessionCognitiveObservation(
	ctx context.Context,
	query string,
	actor string,
	turn int,
	phase string,
	outcome string,
	terminalReason string,
	toolCalls []ai.ToolCall,
	escalationState map[string]any,
	clarificationRequest *ai.ClarificationRequest,
) ai.SessionCognitiveObservation {
	state := a.buildSessionCognitiveState(ctx, query, actor, turn, phase)
	observation := ai.SessionCognitiveObservation{
		CurrentTurn:     turn,
		CurrentActor:    strings.TrimSpace(actor),
		CurrentPhase:    strings.TrimSpace(phase),
		CurrentQuery:    strings.TrimSpace(query),
		LastToolOutcome: strings.TrimSpace(outcome),
		TerminalReason:  strings.TrimSpace(terminalReason),
		Metadata:        map[string]any{},
	}
	if state != nil {
		observation.PendingClarification = state.PendingClarification
		observation.ClarificationStatus = state.ClarificationStatus
		observation.QueuePaused = state.QueuePaused
		observation.RetryTier = state.RetryTier
		observation.EscalationTier = state.EscalationTier
		observation.MemorySignals = append([]string(nil), state.MemorySignals...)
		observation.RecentToolNames = append([]string(nil), state.RecentToolNames...)
		observation.RecentProgressDeltas = append([]float64(nil), state.RecentProgressDeltas...)
		observation.RecentFailureClasses = append([]string(nil), state.RecentFailureClasses...)
	}
	if clarificationRequest != nil {
		observation.PendingClarification = true
		observation.ClarificationStatus = firstNonEmptyStatus(strings.TrimSpace(clarificationRequest.Status), "pending")
		observation.Metadata["clarification_reason"] = strings.TrimSpace(clarificationRequest.Reason)
		observation.Metadata["missing_fact"] = strings.TrimSpace(clarificationRequest.MissingFact)
	}
	if escalationState != nil {
		if tier, ok := intFromAny(escalationState["tier"]); ok {
			observation.EscalationTier = tier
			if state != nil {
				state.EscalationTier = tier
			}
		}
		if failureCount, ok := intFromAny(escalationState["failure_count"]); ok {
			observation.RetryTier = maxInt(observation.RetryTier, failureCount)
			observation.Metadata["failure_count"] = failureCount
			if state != nil {
				state.RetryTier = maxInt(state.RetryTier, failureCount)
			}
		}
		if failedTool, _ := escalationState["failed_tool"].(string); strings.TrimSpace(failedTool) != "" {
			observation.RecentToolNames = appendTrimmedWindow(observation.RecentToolNames, failedTool, 5)
			observation.Metadata["failed_tool"] = strings.TrimSpace(failedTool)
		}
		if errorType, _ := escalationState["error_type"].(string); strings.TrimSpace(errorType) != "" {
			observation.RecentOutcomes = appendTrimmedWindow(observation.RecentOutcomes, errorType, 5)
			observation.Metadata["error_type"] = strings.TrimSpace(errorType)
		}
	}
	for _, toolCall := range toolCalls {
		observation.RecentToolNames = appendTrimmedWindow(observation.RecentToolNames, toolCall.Name, 5)
	}
	if state != nil {
		state.RecentToolNames = append([]string(nil), observation.RecentToolNames...)
		state.RecentFailureClasses = append([]string(nil), observation.RecentFailureClasses...)
		state.RecentProgressDeltas = append([]float64(nil), observation.RecentProgressDeltas...)
	}
	return observation
}

func (a *CopilotAgent) evaluateCognitiveAssessment(ctx context.Context, observation ai.SessionCognitiveObservation, gen ai.Generator) ai.SessionCognitiveAssessment {
	fallback := ai.NewDefaultCognitivePolicySurface()
	input := ai.SessionCognitiveInput{Observation: observation}
	assessment := fallback.EvaluateSessionCognition(input)
	if gen == nil || !fallback.ShouldEvaluateSessionCognition(input) {
		return assessment
	}

	prompt := a.buildSessionCognitiveAssessmentPrompt(observation)
	providerOverride := extractProviderOverrideFromContext(ctx)
	provider := ""
	model := ""
	if providerOverride != nil {
		provider = providerOverride.Provider
		model = providerOverride.Model
	}

	resp, err := gen.Generate(ctx, prompt, ai.GenOptions{
		SystemPrompt:  "Return strict JSON only.",
		Temperature:   0.0,
		ThinkingLevel: NormalizeThinkingLevelForGenerator(gen, provider, model, "low"),
	})
	if err != nil {
		return assessment
	}

	parsed, ok := parseSessionCognitiveAssessment(resp.Text)
	if !ok {
		return assessment
	}
	if parsed.Rationale == "" {
		parsed.Rationale = assessment.Rationale
	}
	if parsed.Confidence == "" {
		parsed.Confidence = assessment.Confidence
	}
	return parsed
}

func (a *CopilotAgent) refreshSessionCognitiveAssessment(
	ctx context.Context,
	query string,
	phase string,
	outcome string,
	toolCalls []ai.ToolCall,
	escalationState map[string]any,
	clarificationRequest *ai.ClarificationRequest,
	gen ai.Generator,
) *ai.SessionCognitiveAssessment {
	actor, _ := ctx.Value(ctxKeyAskWorkItemActor).(string)
	actor = strings.TrimSpace(actor)
	if actor == "" && strings.TrimSpace(phase) != "" {
		actor = "phase:" + strings.TrimSpace(phase)
	}
	observation := a.buildSessionCognitiveObservation(ctx, query, actor, 0, phase, outcome, "", toolCalls, escalationState, clarificationRequest)
	assessment := a.evaluateCognitiveAssessment(ctx, observation, gen)
	payload := ai.GetSessionPayload(ctx)
	if payload != nil {
		state := payload.CognitiveState
		if state == nil {
			state = &ai.SessionCognitiveState{}
			payload.CognitiveState = state
		}
		state.LastAssessment = &assessment
		state.LoopRisk = strings.TrimSpace(assessment.LoopRisk)
		state.LastDecision = strings.TrimSpace(string(assessment.RetryAction))
		state.LastDecisionReason = strings.TrimSpace(assessment.Rationale)
	}
	if escalationState != nil {
		escalationState["cognitive_assessment"] = map[string]any{
			"failure_class":        string(assessment.FailureClass),
			"progress_state":       string(assessment.ProgressState),
			"phase_action":         string(assessment.PhaseAction),
			"retry_action":         string(assessment.RetryAction),
			"suggested_phase":      strings.TrimSpace(assessment.SuggestedPhase),
			"clarification_needed": assessment.ClarificationNeeded,
			"loop_risk":            strings.TrimSpace(assessment.LoopRisk),
			"confidence":           strings.TrimSpace(assessment.Confidence),
			"rationale":            strings.TrimSpace(assessment.Rationale),
		}
		if strings.TrimSpace(assessment.SuggestedPhase) != "" {
			escalationState["suggested_phase"] = strings.TrimSpace(assessment.SuggestedPhase)
		}
		if strings.TrimSpace(string(assessment.PhaseAction)) != "" {
			escalationState["phase_action"] = strings.TrimSpace(string(assessment.PhaseAction))
		}
		if strings.TrimSpace(assessment.Rationale) != "" {
			escalationState["explanation"] = strings.TrimSpace(assessment.Rationale)
		}
	}
	return &assessment
}

func (a *CopilotAgent) buildSessionCognitiveAssessmentPrompt(observation ai.SessionCognitiveObservation) string {
	raw, _ := json.MarshalIndent(observation, "", "  ")
	return fmt.Sprintf(`You are evaluating a failing or blocked Ask workflow turn.

Analyze the observation and return strict JSON with this schema:
{
  "failure_class": "none|implementation_bug|missing_fact|wrong_tool|wrong_phase|requirements_shift|environment_block|terminal_failure|looping",
  "progress_state": "unknown|progressing|stalled|regressing|looping|complete",
  "phase_action": "stay_current|move_forward|move_backward|exit",
  "retry_action": "continue|retry_same_tool|retry_different_tool|research_first|ask_clarification|stop",
  "suggested_phase": "",
  "clarification_needed": false,
  "loop_risk": "low|medium|high",
  "confidence": "low|medium|high",
  "rationale": "one short sentence"
}

Rules:
- Prefer move_backward when the current phase is wrong or more evidence/design work is needed.
- Prefer suggested_phase="investigation" when facts are missing, and suggested_phase="planning" when architecture or design needs rework.
- Prefer suggested_phase="execution" only for implementation-local fixes.
- Prefer ask_clarification when one missing fact blocks progress.
- Prefer stop or exit when the situation is terminal or the user needs control back.
- Do not output markdown.

Observation JSON:
%s`, string(raw))
}

func parseSessionCognitiveAssessment(text string) (ai.SessionCognitiveAssessment, bool) {
	var parsed ai.SessionCognitiveAssessment
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		return normalizeSessionCognitiveAssessment(parsed), true
	}
	cleaned := extractJSONFromMarkdown(text)
	if strings.TrimSpace(cleaned) == "" {
		return ai.SessionCognitiveAssessment{}, false
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return ai.SessionCognitiveAssessment{}, false
	}
	return normalizeSessionCognitiveAssessment(parsed), true
}

func normalizeSessionCognitiveAssessment(assessment ai.SessionCognitiveAssessment) ai.SessionCognitiveAssessment {
	assessment.FailureClass = normalizeCognitiveFailureClass(assessment.FailureClass)
	assessment.ProgressState = normalizeCognitiveProgressState(assessment.ProgressState)
	assessment.PhaseAction = normalizeCognitivePhaseAction(assessment.PhaseAction)
	assessment.RetryAction = normalizeCognitiveRetryAction(assessment.RetryAction)
	assessment.SuggestedPhase = normalizeSuggestedPhase(assessment.SuggestedPhase)
	assessment.LoopRisk = normalizeLoopRisk(assessment.LoopRisk)
	assessment.Confidence = normalizeConfidence(assessment.Confidence)
	assessment.Rationale = strings.TrimSpace(assessment.Rationale)
	return assessment
}

func normalizeCognitiveFailureClass(value ai.CognitiveFailureClass) ai.CognitiveFailureClass {
	switch strings.TrimSpace(string(value)) {
	case string(ai.CognitiveFailureClassImplementationBug):
		return ai.CognitiveFailureClassImplementationBug
	case string(ai.CognitiveFailureClassMissingFact):
		return ai.CognitiveFailureClassMissingFact
	case string(ai.CognitiveFailureClassWrongTool):
		return ai.CognitiveFailureClassWrongTool
	case string(ai.CognitiveFailureClassWrongPhase):
		return ai.CognitiveFailureClassWrongPhase
	case string(ai.CognitiveFailureClassRequirementsShift):
		return ai.CognitiveFailureClassRequirementsShift
	case string(ai.CognitiveFailureClassEnvironmentBlock):
		return ai.CognitiveFailureClassEnvironmentBlock
	case string(ai.CognitiveFailureClassTerminalFailure):
		return ai.CognitiveFailureClassTerminalFailure
	case string(ai.CognitiveFailureClassLooping):
		return ai.CognitiveFailureClassLooping
	default:
		return ai.CognitiveFailureClassNone
	}
}

func normalizeCognitiveProgressState(value ai.CognitiveProgressState) ai.CognitiveProgressState {
	switch strings.TrimSpace(string(value)) {
	case string(ai.CognitiveProgressStateProgressing):
		return ai.CognitiveProgressStateProgressing
	case string(ai.CognitiveProgressStateStalled):
		return ai.CognitiveProgressStateStalled
	case string(ai.CognitiveProgressStateRegressing):
		return ai.CognitiveProgressStateRegressing
	case string(ai.CognitiveProgressStateLooping):
		return ai.CognitiveProgressStateLooping
	case string(ai.CognitiveProgressStateComplete):
		return ai.CognitiveProgressStateComplete
	default:
		return ai.CognitiveProgressStateUnknown
	}
}

func normalizeCognitivePhaseAction(value ai.CognitivePhaseAction) ai.CognitivePhaseAction {
	switch strings.TrimSpace(string(value)) {
	case string(ai.CognitivePhaseActionMoveForward):
		return ai.CognitivePhaseActionMoveForward
	case string(ai.CognitivePhaseActionMoveBackward):
		return ai.CognitivePhaseActionMoveBackward
	case string(ai.CognitivePhaseActionExit):
		return ai.CognitivePhaseActionExit
	default:
		return ai.CognitivePhaseActionStayCurrent
	}
}

func normalizeCognitiveRetryAction(value ai.CognitiveRetryAction) ai.CognitiveRetryAction {
	switch strings.TrimSpace(string(value)) {
	case string(ai.CognitiveRetryActionRetrySameTool):
		return ai.CognitiveRetryActionRetrySameTool
	case string(ai.CognitiveRetryActionRetryDifferent):
		return ai.CognitiveRetryActionRetryDifferent
	case string(ai.CognitiveRetryActionResearchFirst):
		return ai.CognitiveRetryActionResearchFirst
	case string(ai.CognitiveRetryActionAskClarification):
		return ai.CognitiveRetryActionAskClarification
	case string(ai.CognitiveRetryActionStop):
		return ai.CognitiveRetryActionStop
	default:
		return ai.CognitiveRetryActionContinue
	}
}

func normalizeSuggestedPhase(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "intake", "investigation", "planning", "execution", "review", "testing", "delivery":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func normalizeLoopRisk(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "medium", "high":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "low"
	}
}

func normalizeConfidence(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "low", "high":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "medium"
	}
}

func appendTrimmedWindow(values []string, value string, maxLen int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	values = append(values, value)
	if maxLen > 0 && len(values) > maxLen {
		return append([]string(nil), values[len(values)-maxLen:]...)
	}
	return values
}

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *CopilotAgent) collectSessionMemorySignals(ctx context.Context) []string {
	var signals []string
	payload := ai.GetSessionPayload(ctx)
	if payload == nil || a.service == nil || a.service.session == nil {
		return signals
	}
	for _, item := range a.service.session.MRU {
		if strings.TrimSpace(item.Context) == "" {
			continue
		}
		signals = append(signals, item.Context)
	}
	if payload.CognitiveState != nil && len(payload.CognitiveState.MemorySignals) > 0 {
		return append(signals, payload.CognitiveState.MemorySignals...)
	}
	return signals
}

func (a *CopilotAgent) evaluateSessionCognitivePolicy(ctx context.Context, query string, actor string, turn int, phase string, outcome string, terminalReason string) sessionCognitiveDecision {
	state := a.buildSessionCognitiveState(ctx, query, actor, turn, phase)
	if state == nil {
		return sessionCognitiveDecision{Action: "continue", Reason: "no session payload"}
	}
	state.LastToolOutcome = strings.TrimSpace(outcome)
	state.TerminalReason = strings.TrimSpace(terminalReason)

	if state.LastAssessment != nil {
		switch state.LastAssessment.RetryAction {
		case ai.CognitiveRetryActionAskClarification:
			state.QueuePaused = true
			state.LastDecision = "pause_for_clarification"
			state.LastDecisionReason = strings.TrimSpace(state.LastAssessment.Rationale)
			return sessionCognitiveDecision{Action: "pause_for_clarification", Reason: state.LastDecisionReason, SuggestedPhase: strings.TrimSpace(state.LastAssessment.SuggestedPhase)}
		case ai.CognitiveRetryActionStop:
			state.LastDecision = "stop"
			state.LastDecisionReason = strings.TrimSpace(state.LastAssessment.Rationale)
			return sessionCognitiveDecision{Action: "stop", Reason: state.LastDecisionReason, SuggestedPhase: strings.TrimSpace(state.LastAssessment.SuggestedPhase)}
		}
		if state.LastAssessment.PhaseAction == ai.CognitivePhaseActionMoveBackward || state.LastAssessment.PhaseAction == ai.CognitivePhaseActionMoveForward {
			state.LastDecision = "escalate_retry"
			state.LastDecisionReason = strings.TrimSpace(state.LastAssessment.Rationale)
			return sessionCognitiveDecision{Action: "escalate_retry", Reason: state.LastDecisionReason, SuggestedPhase: strings.TrimSpace(state.LastAssessment.SuggestedPhase)}
		}
	}

	if state.PendingClarification {
		state.QueuePaused = true
		state.LastDecision = "pause_for_clarification"
		state.LastDecisionReason = "pending clarification should halt further queue progress"
		return sessionCognitiveDecision{Action: "pause_for_clarification", Reason: state.LastDecisionReason}
	}
	if state.EscalationTier >= 3 || state.RetryTier >= 3 {
		state.LastDecision = "escalate_retry"
		state.LastDecisionReason = "retry policy reached escalation tier"
		return sessionCognitiveDecision{Action: "escalate_retry", Reason: state.LastDecisionReason}
	}
	if strings.Contains(strings.ToLower(terminalReason), "hard stop") || strings.Contains(strings.ToLower(terminalReason), "terminal") {
		state.LastDecision = "stop"
		state.LastDecisionReason = "terminal outcome was observed"
		return sessionCognitiveDecision{Action: "stop", Reason: state.LastDecisionReason}
	}

	state.LastDecision = "continue"
	state.LastDecisionReason = "no blocking clarification or escalation state"
	return sessionCognitiveDecision{Action: "continue", Reason: state.LastDecisionReason}
}
