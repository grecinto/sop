package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop/ai"
)

type sessionCognitiveDecision struct {
	Action         string
	Reason         string
	SuggestedPhase string
}

func (a *CopilotAgent) evaluatePhaseTransactionCognitiveDecision(ctx context.Context, query string, classification *AskTaskContext, toolCalls []ai.ToolCall, outcomeFacts []string, finalText string, clarificationRequest *ai.ClarificationRequest) phaseTransactionDecision {
	if a == nil || classification == nil {
		log.Info("Transaction evaluation: nil agent or classification")
		return phaseTransactionDecision{}
	}
	if clarificationRequest != nil {
		log.Info("Transaction evaluation: clarification pending")
		return phaseTransactionDecision{}
	}
	currentPhase := strings.TrimSpace(strings.ToLower(classification.Phase))

	// Get WorkStreamContext to check accumulated state
	var workStreamCtx *WorkStreamContext
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if thread := a.service.session.Memory.GetCurrentThread(); thread != nil {
			workStreamCtx = thread.StreamContext
		}
	}

	log.Info("Transaction evaluation start",
		"current_phase", currentPhase,
		"orchestration_level", classification.OrchestrationLevel,
		"execution_ready", classification.ExecutionReady,
		"domains", classification.Domains,
		"tool_count", len(toolCalls),
		"phase_iteration", getPhaseIterationCount(workStreamCtx, currentPhase))

	if !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		log.Info("Transaction evaluation: not coding_workflow domain", "domains", classification.Domains)
		if currentPhase == "execution" && !phaseTransactionHasGenericCompletionEvidence(toolCalls, outcomeFacts, finalText) {
			reason := "current execution step is still incomplete; continue in the active phase until the capability confirms completion"
			return phaseTransactionDecision{
				EnqueueNextPhase: true,
				NextPhase:        "execution",
				Reason:           reason,
			}
		}
		return phaseTransactionDecision{}
	}
	if strings.TrimSpace(strings.ToLower(classification.OrchestrationLevel)) != orchestrationManagedWorkflow {
		log.Info("Transaction evaluation: not managed_workflow",
			"orchestration_level", classification.OrchestrationLevel,
			"expected", orchestrationManagedWorkflow)
		return phaseTransactionDecision{}
	}

	// GENERAL COGNITIVE GATE: LLM-driven assessment for all phases
	// If significant work was done (3+ tool calls), ask LLM what to do next
	if len(toolCalls) >= 3 {
		iterationCount := getPhaseIterationCount(workStreamCtx, currentPhase)
		if iterationCount < 2 { // Prevent infinite loops
			directQuery, nextPhase, reason := a.buildDirectActionFromToolResults(ctx, toolCalls)
			if directQuery != "" {
				// LLM recommended specific action
				log.Info("Cognitive gate: direct action detected",
					"current_phase", currentPhase,
					"next_phase", nextPhase,
					"query_preview", truncateString(directQuery, 100),
					"reason", reason)

				a.recordPhaseTransactionCognitiveState(ctx, nextPhase, reason)
				return phaseTransactionDecision{
					EnqueueNextPhase: true,
					NextPhase:        nextPhase,
					Reason:           reason,
					DirectQuery:      directQuery,
				}
			}
		}
	}

	// PHASE-SPECIFIC CHECKS (fallback heuristics)

	// Execution phase continuation logic - check actual phase, not static classification flag
	if currentPhase == "execution" {
		// Check if code was mutated but needs validation
		if phaseTransactionMutatedCode(toolCalls) && !phaseTransactionHasValidationEvidence(toolCalls, outcomeFacts, finalText) {
			reason := "managed coding execution requires focused validation evidence after code changes before completion"
			a.recordPhaseTransactionCognitiveState(ctx, "execution", reason)
			return phaseTransactionDecision{
				EnqueueNextPhase: true,
				NextPhase:        "execution",
				Reason:           reason,
			}
		}

		// Check if execution is still investigating/searching without generating patches yet
		if phaseTransactionIsStillInvestigating(toolCalls) && !phaseTransactionMutatedCode(toolCalls) {
			// Check phase iteration count to prevent infinite loops
			iterationCount := getPhaseIterationCount(workStreamCtx, currentPhase)
			if iterationCount >= 2 {
				// Already ran execution 2+ times - let it complete naturally
				log.Info("Execution phase iteration limit reached - allowing natural completion",
					"iteration_count", iterationCount,
					"phase", currentPhase)
				return phaseTransactionDecision{}
			}

			// Try to build a direct, actionable query from accumulated tool results
			directQuery, nextPhase, reason := a.buildDirectActionFromToolResults(ctx, toolCalls)
			if directQuery != "" {
				// Found specific action - enqueue direct query
				log.Info("Cognitive gate: direct action detected (execution fallback)",
					"next_phase", nextPhase,
					"query_preview", truncateString(directQuery, 100),
					"reason", reason)
				a.recordPhaseTransactionCognitiveState(ctx, nextPhase, reason)
				return phaseTransactionDecision{
					EnqueueNextPhase: true,
					NextPhase:        nextPhase,
					Reason:           reason,
					DirectQuery:      directQuery,
				}
			}

			// Fallback to generic guidance
			reason = a.buildExecutionGuidanceFromState(workStreamCtx, toolCalls)
			if reason == "" {
				// No specific guidance - let it complete naturally
				return phaseTransactionDecision{}
			}

			a.recordPhaseTransactionCognitiveState(ctx, "execution", reason)
			return phaseTransactionDecision{
				EnqueueNextPhase: true,
				NextPhase:        "execution",
				Reason:           reason,
			}
		}

		return phaseTransactionDecision{}
	}

	// Planning phase to execution transition
	if currentPhase == "execution" && !a.hasExecutionReadyPlanningArtifact() {
		// Only require planning artifact for NEW execution phases that haven't done work yet
		// If execution came from investigation with test failures, let it proceed without planning
		hasTestEvidence := false
		for _, toolCall := range toolCalls {
			name := strings.TrimSpace(strings.ToLower(toolCall.Name))
			if strings.Contains(name, "run_tests") || strings.Contains(name, "run_repo_tests") {
				hasTestEvidence = true
				break
			}
		}
		// If this execution turn did actual work, don't require planning artifact
		if len(toolCalls) == 0 && !hasTestEvidence {
			reason := "managed coding execution requires a ready_for_execution planning artifact before implementation can proceed"
			a.recordPhaseTransactionCognitiveState(ctx, "planning", reason)
			return phaseTransactionDecision{
				EnqueueNextPhase: true,
				NextPhase:        "planning",
				Reason:           reason,
			}
		}
	}
	if currentPhase == "planning" && a.hasExecutionReadyPlanningArtifact() {
		reason := "managed coding planning produced a ready_for_execution planning artifact; continue in execution and use the persisted plan"
		a.recordPhaseTransactionCognitiveState(ctx, "execution", reason)
		return phaseTransactionDecision{
			EnqueueNextPhase: true,
			NextPhase:        "execution",
			Reason:           reason,
		}
	}
	if classification.ExecutionReady {
		return phaseTransactionDecision{}
	}
	if currentPhase != "investigation" {
		return phaseTransactionDecision{}
	}

	// If investigation ran tests or did sufficient context gathering, enqueue execution
	// Don't rely on string matching for failures (test results are file-backed)
	ranTests := false
	didSearch := false
	for _, toolCall := range toolCalls {
		name := strings.TrimSpace(strings.ToLower(toolCall.Name))
		if strings.Contains(name, "run_tests") || strings.Contains(name, "run_repo_tests") {
			ranTests = true
		}
		if strings.Contains(name, "search_repo") || strings.Contains(name, "inspect_repo") || strings.Contains(name, "build_context") {
			didSearch = true
		}
	}

	// If investigation gathered evidence (tests + searches), move to execution
	if ranTests && didSearch {
		reason := "investigation gathered test results and context; ready for execution"
		a.recordPhaseTransactionCognitiveState(ctx, "execution", reason)
		return phaseTransactionDecision{
			EnqueueNextPhase: true,
			NextPhase:        "execution",
			Reason:           reason,
		}
	}

	// Legacy: Check for explicit failure strings (won't work with file-backed results)
	if a.hasExecutionReadyPlanningArtifact() || a.isPhaseComplete(ctx, currentPhase, toolCalls, finalText) {
		return phaseTransactionDecision{}
	}
	if !phaseTransactionFoundFailingTests(toolCalls, outcomeFacts, finalText) {
		return phaseTransactionDecision{}
	}
	reason := "managed coding issue workflow requires planning after failing-test investigation"
	a.recordPhaseTransactionCognitiveState(ctx, "planning", reason)
	return phaseTransactionDecision{
		EnqueueNextPhase: true,
		NextPhase:        "planning",
		Reason:           reason,
	}
}

func (a *CopilotAgent) recordPhaseTransactionCognitiveState(ctx context.Context, suggestedPhase string, reason string) {
	if payload := ai.GetSessionPayload(ctx); payload != nil {
		state := payload.CognitiveState
		if state == nil {
			state = &ai.SessionCognitiveState{}
			payload.CognitiveState = state
		}
		state.LastDecision = "enqueue_phase"
		state.LastDecisionReason = reason
		state.LastAssessment = &ai.SessionCognitiveAssessment{
			FailureClass:   ai.CognitiveFailureClassImplementationBug,
			ProgressState:  ai.CognitiveProgressStateProgressing,
			PhaseAction:    ai.CognitivePhaseActionMoveForward,
			RetryAction:    ai.CognitiveRetryActionContinue,
			SuggestedPhase: strings.TrimSpace(suggestedPhase),
			LoopRisk:       "low",
			Confidence:     "high",
			Rationale:      reason,
		}
		state.LoopRisk = "low"
	}
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
	state.RecentQueries = appendTrimmedWindow(state.RecentQueries, state.CurrentQuery, 5)
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
		observation.RecentQueries = append([]string(nil), state.RecentQueries...)
		observation.MemorySignals = append([]string(nil), state.MemorySignals...)
		observation.RecentToolNames = append([]string(nil), state.RecentToolNames...)
		observation.RecentProgressDeltas = append([]float64(nil), state.RecentProgressDeltas...)
		observation.RecentFailureClasses = append([]string(nil), state.RecentFailureClasses...)
		if repeated, repeatCount, descriptor := repeatedContinuationPrompt(state.RecentQueries); repeated {
			observation.Metadata["repeated_continuation_prompt"] = true
			observation.Metadata["continuation_prompt_repeat_count"] = repeatCount
			observation.Metadata["continuation_prompt_pattern"] = descriptor.Pattern
			observation.Metadata["continuation_prompt_examples"] = append([]string(nil), descriptor.Examples...)
		}
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

	if repeated, repeatCount, descriptor := repeatedContinuationPrompt(state.RecentQueries); repeated && !state.PendingClarification {
		state.LastDecision = "escalate_retry"
		state.LastDecisionReason = fmt.Sprintf("repeated unresolved continuation prompt pattern %q observed %d times without progress; use cognitive escalation to choose whether to clarify, change phase, refine the plan, or stop looping", descriptor.Pattern, repeatCount)
		state.RetryTier = maxInt(state.RetryTier, 2)
		return sessionCognitiveDecision{Action: "escalate_retry", Reason: state.LastDecisionReason, SuggestedPhase: strings.TrimSpace(state.CurrentPhase)}
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

type continuationPromptDescriptor struct {
	Pattern  string
	Examples []string
}

func repeatedContinuationPrompt(queries []string) (bool, int, continuationPromptDescriptor) {
	if len(queries) < 2 {
		return false, 0, continuationPromptDescriptor{}
	}
	lastPattern, lastExample, ok := classifyContinuationPrompt(queries[len(queries)-1])
	if !ok {
		return false, 0, continuationPromptDescriptor{}
	}
	count := 1
	examples := []string{lastExample}
	for i := len(queries) - 2; i >= 0; i-- {
		candidatePattern, candidateExample, ok := classifyContinuationPrompt(queries[i])
		if !ok || candidatePattern != lastPattern {
			break
		}
		count++
		examples = append([]string{candidateExample}, examples...)
	}
	if count < 2 {
		return false, count, continuationPromptDescriptor{}
	}
	return true, count, continuationPromptDescriptor{Pattern: lastPattern, Examples: examples}
}

func classifyContinuationPrompt(query string) (string, string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return "", "", false
	}
	example := normalized
	normalized = strings.Trim(normalized, ".!? ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	if normalized == "" {
		return "", "", false
	}
	tokens := strings.Fields(normalized)
	if len(tokens) > 8 || len(normalized) > 64 {
		return "", "", false
	}

	filler := map[string]bool{"please": true, "just": true, "okay": true, "ok": true, "well": true, "then": true, "now": true, "so": true, "alright": true}
	for len(tokens) > 0 && filler[tokens[0]] {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && filler[tokens[len(tokens)-1]] {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 {
		return "", "", false
	}
	normalized = strings.Join(tokens, " ")

	advancePairs := map[string]bool{
		"go ahead":   true,
		"go on":      true,
		"keep going": true,
		"move on":    true,
		"carry on":   true,
		"do it":      true,
		"next step":  true,
	}
	advanceSingles := map[string]bool{
		"proceed":  true,
		"continue": true,
		"resume":   true,
		"advance":  true,
		"next":     true,
	}
	statusAnchors := map[string]bool{
		"done":     true,
		"fixed":    true,
		"fix":      true,
		"resolved": true,
		"working":  true,
		"ready":    true,
		"finished": true,
	}
	questionOpeners := map[string]bool{
		"did":    true,
		"are":    true,
		"is":     true,
		"has":    true,
		"have":   true,
		"can":    true,
		"could":  true,
		"should": true,
	}

	if advanceSingles[normalized] || advancePairs[normalized] {
		return "advance_request", example, true
	}
	if len(tokens) >= 2 {
		pair := tokens[0] + " " + tokens[1]
		if advancePairs[pair] {
			return "advance_request", example, true
		}
	}
	if questionOpeners[tokens[0]] {
		for _, token := range tokens[1:] {
			if statusAnchors[token] {
				return "status_check", example, true
			}
		}
	}
	if len(tokens) <= 3 {
		for _, token := range tokens {
			if statusAnchors[token] {
				return "status_check", example, true
			}
		}
	}
	return "", "", false
}

// getPhaseIterationCount returns how many times a phase has been executed
func getPhaseIterationCount(workStreamCtx *WorkStreamContext, phase string) int {
	if workStreamCtx == nil || workStreamCtx.PhaseIterations == nil {
		return 0
	}
	return workStreamCtx.PhaseIterations[phase]
}

// buildDirectActionFromToolResults uses LLM to assess accumulated tool results and recommend a direct action.
// Returns (directQuery, nextPhase, reason) if LLM recommends specific action, or ("", "", "") to continue naturally.
// This is domain-neutral: works for coding, testing, research, etc.
func (a *CopilotAgent) buildDirectActionFromToolResults(ctx context.Context, toolCalls []ai.ToolCall) (string, string, string) {
	if a == nil || a.brain == nil || len(toolCalls) == 0 {
		return "", "", ""
	}

	// Build tool call summary for LLM
	var toolSummary strings.Builder
	toolSummary.WriteString("Recent tool executions in this phase:\n\n")

	for i, tc := range toolCalls {
		if i >= 10 { // Limit to last 10 tool calls
			break
		}
		toolSummary.WriteString(fmt.Sprintf("%d. Tool: %s\n", i+1, tc.Name))
		if len(tc.Args) > 0 {
			argsJSON, _ := json.Marshal(tc.Args)
			argsSummary := string(argsJSON)
			if len(argsSummary) > 200 {
				argsSummary = argsSummary[:200] + "..."
			}
			toolSummary.WriteString(fmt.Sprintf("   Args: %s\n", argsSummary))
		}
		toolSummary.WriteString("\n")
	}

	prompt := fmt.Sprintf(`You are evaluating a work phase that has collected information via tool calls.

%s

Based on what has been collected, determine if there is a specific, actionable next step that should be taken directly, or if the phase should continue naturally.

Examples of direct actions:
- Test results obtained → "READ and FIX the failing test files: auth/user_test.go, db/query_test.go" (phase: execution)
- Investigation searched files → "READ the identified source files and generate patches" (phase: execution)
- Planning complete → "Execute step 1: fix authentication in auth/user.go" (phase: execution)  
- Context gathered → "Create detailed fix plan for the 3 test failures" (phase: planning)

CRITICAL RULES:
- If test results with file paths were obtained: return "direct_query" to READ those files and generate patches
- If searches found files: return "direct_query" to READ (not search) those files
- If more investigation needed: return "continue_naturally"
- DO NOT search repeatedly - transition to reading/fixing after gathering file paths

Return strict JSON:
{
  "action": "direct_query|continue_naturally",
  "query": "specific actionable statement if action=direct_query, empty otherwise",
  "next_phase": "investigation|planning|execution (only if action=direct_query)",
  "reason": "one sentence explaining the decision"
}

Focus on WHAT to do, not HOW (LLM will figure out how).`, toolSummary.String())

	providerOverride := extractProviderOverrideFromContext(ctx)
	provider := ""
	model := ""
	if providerOverride != nil {
		provider = providerOverride.Provider
		model = providerOverride.Model
	}

	resp, err := a.brain.Generate(ctx, prompt, ai.GenOptions{
		SystemPrompt:  "Return strict JSON only.",
		Temperature:   0.0,
		MaxTokens:     500,
		ThinkingLevel: NormalizeThinkingLevelForGenerator(a.brain, provider, model, "low"),
	})

	if err != nil {
		log.Info("LLM cognitive assessment failed", "error", err)
		return "", "", ""
	}

	// Parse JSON response
	type cognitiveRecommendation struct {
		Action    string `json:"action"`
		Query     string `json:"query"`
		NextPhase string `json:"next_phase"`
		Reason    string `json:"reason"`
	}

	var rec cognitiveRecommendation
	if err := json.Unmarshal([]byte(resp.Text), &rec); err != nil {
		log.Info("Failed to parse cognitive recommendation", "error", err, "response", resp.Text)
		return "", "", ""
	}

	if rec.Action == "direct_query" && strings.TrimSpace(rec.Query) != "" {
		nextPhase := strings.TrimSpace(strings.ToLower(rec.NextPhase))
		if nextPhase == "" {
			nextPhase = "execution" // Default to execution if not specified
		}
		return strings.TrimSpace(rec.Query), nextPhase, strings.TrimSpace(rec.Reason)
	}

	return "", "", ""
}

// buildExecutionGuidanceFromState analyzes WorkStreamContext artifacts and builds specific guidance
// This is a fallback heuristic when LLM assessment doesn't provide direct action
func (a *CopilotAgent) buildExecutionGuidanceFromState(workStreamCtx *WorkStreamContext, currentToolCalls []ai.ToolCall) string {
	if workStreamCtx == nil {
		return "managed coding execution is investigating but has not generated patches yet; continue search-to-patch workflow"
	}

	// Check what's been accumulated in artifacts
	hasTestFailures := false
	hasFileReads := false
	hasSearchResults := false

	// Check artifacts for evidence
	if artifacts, ok := workStreamCtx.PhaseArtifacts["execution"]; ok {
		if evidence, ok := artifacts["evidence"]; ok {
			evidenceStr := fmt.Sprintf("%v", evidence)
			if strings.Contains(strings.ToLower(evidenceStr), "test") || strings.Contains(strings.ToLower(evidenceStr), "fail") {
				hasTestFailures = true
			}
		}
		if structuredData, ok := artifacts["structured_data"].(map[string]any); ok {
			if testResults, ok := structuredData["test_results"]; ok && testResults != nil {
				hasTestFailures = true
			}
		}
	}

	// Check current tool calls for file operations
	for _, toolCall := range currentToolCalls {
		name := strings.TrimSpace(strings.ToLower(toolCall.Name))
		if strings.Contains(name, "read_file") || strings.Contains(name, "view_file") {
			hasFileReads = true
		}
		if strings.Contains(name, "search") {
			hasSearchResults = true
		}
	}

	// Build specific guidance based on state
	if hasTestFailures && !hasFileReads && !hasSearchResults {
		return "test failures identified but source files not yet examined; READ the specific files mentioned in test failures to understand the root cause, then generate patches"
	}

	if hasTestFailures && hasSearchResults && !hasFileReads {
		return "test failures and search results available but source code not read; READ the complete source files (not just search snippets) to understand context, then generate patches"
	}

	if hasTestFailures && hasFileReads {
		return "test failures identified and source files examined; GENERATE specific patches to fix the identified issues based on the test failures and source code"
	}

	// Default: generic guidance for first iteration
	return "managed coding execution is investigating but has not generated patches yet; run tests to identify failures, then read source files and generate patches"
}
