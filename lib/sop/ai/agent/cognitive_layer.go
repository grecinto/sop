package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/planner"
)

type sessionCognitiveDecision struct {
	Action         string
	Reason         string
	SuggestedPhase string
}

// diagnosisMissingForPhase checks whether the current phase's persisted artifact carries a
// repair task without a complete diagnosis (root_cause, evidence, fix_target, shared_with,
// blast_radius). It reuses the same detection rules the patch-preview gate enforces so the
// transition decision and the tool-level gate never disagree about diagnosis readiness.
func (a *CopilotAgent) diagnosisMissingForPhase(phaseID string, query string) (missing []string, isRepair bool) {
	if a == nil {
		return nil, false
	}
	artifact, ok := a.loadStoredPhaseArtifact(phaseID)
	if !ok {
		return nil, false
	}
	isRepair = planner.IsRepairTaskForDiagnosis(artifact.Summary, artifact.Plan, nil) || planner.IsRepairTaskForDiagnosis(query, nil, nil)
	if !isRepair {
		return nil, false
	}
	return planner.DiagnosisFieldsMissing(artifact.StructuredData, artifact.Evidence), true
}

func (a *CopilotAgent) evaluatePhaseTransactionCognitiveDecision(ctx context.Context, query string, classification *AskTaskContext, toolCalls []ai.ToolCall, outcomeFacts []string, finalText string, clarificationRequest *ai.ClarificationRequest) phaseTransactionDecision {
	if a == nil || classification == nil {
		log.Debug("Transaction evaluation: nil agent or classification")
		return phaseTransactionDecision{}
	}
	if clarificationRequest != nil {
		log.Debug("Transaction evaluation: clarification pending")
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

	log.Debug("Transaction evaluation start",
		"current_phase", currentPhase,
		"orchestration_level", classification.OrchestrationLevel,
		"execution_ready", classification.ExecutionReady,
		"domains", classification.Domains,
		"tool_count", len(toolCalls),
		"phase_iteration", getPhaseIterationCount(workStreamCtx, currentPhase))

	if !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		log.Debug("Transaction evaluation: not coding_workflow domain", "domains", classification.Domains)
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
	if !isManagedOrchestrationLevel(classification.OrchestrationLevel) {
		log.Debug("Transaction evaluation: not managed objective",
			"orchestration_level", classification.OrchestrationLevel,
			"expected", orchestrationManagedObjective)
		return phaseTransactionDecision{}
	}

	// GENERAL COGNITIVE GATE: LLM-driven assessment for all phases
	// Ask the LLM what to do next as soon as there is any tool activity, rather than
	// waiting for 3+ tool calls to accumulate before consulting it.
	if len(toolCalls) >= 1 {
		iterationCount := getPhaseIterationCount(workStreamCtx, currentPhase)
		if iterationCount < 2 { // Prevent infinite loops
			directQuery, nextPhase, reason := a.buildDirectActionFromToolResults(ctx, toolCalls)
			if directQuery != "" {
				// LLM recommended specific action
				log.Debug("Cognitive gate: direct action detected",
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
			// Diagnosis is a stronger, more specific signal than generic iteration counting: a
			// repair task missing root_cause/evidence/fix_target/blast_radius should be told to
			// capture diagnosis, not just nudged to keep searching.
			if missing, isRepair := a.diagnosisMissingForPhase(currentPhase, query); isRepair && len(missing) > 0 {
				reason := fmt.Sprintf("repair task diagnosis is incomplete (missing: %s); capture root_cause, evidence, fix_target, shared_with, and blast_radius before continuing", strings.Join(missing, ", "))
				a.recordPhaseTransactionCognitiveState(ctx, currentPhase, reason)
				return phaseTransactionDecision{
					EnqueueNextPhase: true,
					NextPhase:        currentPhase,
					Reason:           reason,
					DirectQuery:      "Capture structured diagnosis (root_cause, evidence, fix_target, shared_with, blast_radius) for the failures found so far before generating a patch.",
				}
			}

			// Loop detection: use the purpose-built WorkStreamContext.CheckForLoop() detector
			// (same-phase repeats, total iteration cap, A-B-C-A-B-C pattern) instead of a local
			// iteration-count check, so a genuine stall is reported consistently everywhere.
			if looping, loopReason := workStreamCtx.CheckForLoop(); looping {
				log.Debug("Execution phase loop detected - stopping requeue",
					"phase", currentPhase,
					"loop_reason", loopReason)
				a.recordPhaseTransactionCognitiveState(ctx, currentPhase, loopReason)
				return phaseTransactionDecision{}
			}

			// Try to build a direct, actionable query from accumulated tool results
			directQuery, nextPhase, reason := a.buildDirectActionFromToolResults(ctx, toolCalls)
			if directQuery != "" {
				// Found specific action - enqueue direct query
				log.Debug("Cognitive gate: direct action detected (execution fallback)",
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
			reason = a.buildExecutionGuidanceFromState(currentPhase, workStreamCtx, toolCalls)
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

		// Cold-start execution phase: no tool calls yet and no ready planning artifact means
		// execution has nothing to work from. This used to live in an unreachable second
		// `currentPhase == "execution"` block below this one (dead code, since this block
		// always returns) - moved here so it actually executes.
		if len(toolCalls) == 0 && !a.hasExecutionReadyPlanningArtifact() {
			reason := "managed coding execution requires a ready_for_execution planning artifact before implementation can proceed"
			a.recordPhaseTransactionCognitiveState(ctx, "planning", reason)
			return phaseTransactionDecision{
				EnqueueNextPhase: true,
				NextPhase:        "planning",
				Reason:           reason,
			}
		}

		// A repair task that captured a complete diagnosis but did not mutate any code this
		// turn is not actually fixed yet - without this, the diagnosis gate above can be
		// satisfied and the turn silently ends without ever generating a patch.
		if !phaseTransactionMutatedCode(toolCalls) {
			if missing, isRepair := a.diagnosisMissingForPhase(currentPhase, query); isRepair && len(missing) == 0 {
				// A diagnosis captured on a PRIOR execution turn (this is at least the 2nd time
				// this phase has run) that still hasn't produced a patch usually means the
				// hypothesis was wrong and the model kept re-diagnosing instead of patching (e.g.
				// a "fix_target" that turns out to already be correct once read). Requeuing again
				// just repeats the same wasted search/read cycle, so stop here and let the stall
				// surface to the user instead of waiting for the generic 3-repeat loop detector.
				if getPhaseIterationCount(workStreamCtx, currentPhase) >= 2 {
					reason := "repair task has completed diagnosis on a prior turn without ever generating a patch; stopping auto-requeue to avoid repeating a wrong hypothesis"
					log.Debug("Execution phase re-diagnosed without patching - stopping requeue",
						"phase", currentPhase, "iteration_count", getPhaseIterationCount(workStreamCtx, currentPhase))
					a.recordPhaseTransactionCognitiveState(ctx, currentPhase, reason)
					return phaseTransactionDecision{}
				}
				if looping, loopReason := workStreamCtx.CheckForLoop(); looping {
					log.Debug("Execution phase loop detected after diagnosis capture - stopping requeue",
						"phase", currentPhase,
						"loop_reason", loopReason)
					a.recordPhaseTransactionCognitiveState(ctx, currentPhase, loopReason)
					return phaseTransactionDecision{}
				}
				reason := "repair task diagnosis is complete; proceed directly to patch generation and validation instead of stopping"
				a.recordPhaseTransactionCognitiveState(ctx, currentPhase, reason)
				return phaseTransactionDecision{
					EnqueueNextPhase: true,
					NextPhase:        currentPhase,
					Reason:           reason,
					DirectQuery:      a.buildExecutionPatchQueryFromState(currentPhase),
				}
			}
		}

		return phaseTransactionDecision{}
	}

	// Note: the "execution requires a ready planning artifact when no tool calls have run yet"
	// check now lives inside the currentPhase == "execution" block above (it was previously
	// unreachable dead code here, since that block always returns before reaching this one).

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
func (a *CopilotAgent) buildExecutionGuidanceFromState(currentPhase string, workStreamCtx *WorkStreamContext, currentToolCalls []ai.ToolCall) string {
	if workStreamCtx == nil {
		return "managed coding execution is investigating but has not generated patches yet; continue search-to-patch workflow"
	}

	// Check what's been accumulated in artifacts. Investigation often stores the failing
	// tests and evidence before execution starts, so scan the full phase-artifact set.
	hasTestFailures := hasTestFailureEvidenceInPhaseArtifacts(workStreamCtx.PhaseArtifacts)
	// WorkStreamContext.PhaseArtifacts is never populated by the capture tool in production
	// (it only writes the raw "phase-artifact:<phase>" memory key), so also check the real
	// persisted artifact directly to avoid missing evidence that was actually captured.
	if stored, ok := a.loadStoredPhaseArtifact(currentPhase); ok {
		if hasStoredTestFailureEvidence(stored) {
			hasTestFailures = true
		}
	}
	if stored, ok := a.loadStoredPhaseArtifact("investigation"); ok {
		if hasStoredTestFailureEvidence(stored) {
			hasTestFailures = true
		}
	}
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
		return "test failures identified but source files not yet examined; READ the specific files mentioned in test failures to understand the root cause, then generate patches with execution_preview_patch"
	}

	if hasTestFailures && hasSearchResults && !hasFileReads {
		return "test failures and search results available but source code not read; READ the complete source files (not just search snippets) to understand context, then generate patches with execution_preview_patch"
	}

	if hasTestFailures && hasFileReads {
		return a.buildExecutionPatchQueryFromState(currentPhase)
	}

	// Default: generic guidance for first iteration
	return "managed coding execution is investigating but has not generated patches yet; run tests to identify failures, then read source files and generate patches with execution_preview_patch"
}

// hasStoredTestFailureEvidence checks a persisted phase artifact (loaded via
// loadStoredPhaseArtifact) for test-failure evidence in its summary/evidence/structured data.
func hasStoredTestFailureEvidence(artifact storedPlanningArtifact) bool {
	candidates := append(append([]string{artifact.Summary}, artifact.Evidence...), artifact.NextSteps...)
	for _, candidate := range candidates {
		lower := strings.ToLower(candidate)
		if strings.Contains(lower, "test") || strings.Contains(lower, "fail") {
			return true
		}
	}
	if artifact.StructuredData != nil {
		if testResults, ok := artifact.StructuredData["test_results"]; ok && testResults != nil {
			return true
		}
		if diagnosis, ok := artifact.StructuredData["diagnosis"].(map[string]any); ok {
			if rootCause, ok := diagnosis["root_cause"].(string); ok {
				lower := strings.ToLower(rootCause)
				if strings.Contains(lower, "test") || strings.Contains(lower, "fail") {
					return true
				}
			}
		}
	}
	return false
}

func hasTestFailureEvidenceInPhaseArtifacts(phaseArtifacts map[string]map[string]any) bool {
	for _, artifact := range phaseArtifacts {
		if len(artifact) == 0 {
			continue
		}
		if evidence, ok := artifact["evidence"]; ok {
			evidenceStr := strings.ToLower(fmt.Sprint(evidence))
			if strings.Contains(evidenceStr, "test") || strings.Contains(evidenceStr, "fail") {
				return true
			}
		}
		if structuredData, ok := artifact["structured_data"].(map[string]any); ok {
			if testResults, ok := structuredData["test_results"]; ok && testResults != nil {
				return true
			}
			if evidence, ok := structuredData["evidence"]; ok {
				evidenceStr := strings.ToLower(fmt.Sprint(evidence))
				if strings.Contains(evidenceStr, "test") || strings.Contains(evidenceStr, "fail") {
					return true
				}
			}
		}
	}
	return false
}

type executionDiagnosisDetails struct {
	RootCause   string
	FixTarget   string
	FocusSymbol string
	SharedWith  string
	BlastRadius string
	Evidence    []string
}

func (a *CopilotAgent) buildExecutionPatchQueryFromState(phaseID string) string {
	base := "Diagnosis is captured. Proceed directly to execution_preview_patch and execution_apply_patch for the identified fix_target, then run execution_run_tests to validate the fix. If you do not already know the exact line range, call execution_search_repo ONCE for the focus symbol to locate it, then execution_read_file ONCE for that exact region to get precise old_content, then generate the patch immediately - do not re-read the file from the top in sequential windows or repeat either call for the same target."
	details := a.extractExecutionDiagnosisDetails(phaseID)
	if details.isEmpty() {
		return base
	}
	lines := []string{base}
	if details.RootCause != "" {
		lines = append(lines, "Root cause: "+details.RootCause)
	}
	if details.FixTarget != "" {
		lines = append(lines, "Fix target: "+details.FixTarget)
	}
	if details.FocusSymbol != "" {
		lines = append(lines, "Focus symbol: "+details.FocusSymbol)
	}
	if len(details.Evidence) > 0 {
		lines = append(lines, "Evidence: "+strings.Join(details.Evidence, "; "))
	}
	if details.SharedWith != "" {
		lines = append(lines, "Shared with: "+details.SharedWith)
	}
	if details.BlastRadius != "" {
		lines = append(lines, "Blast radius: "+details.BlastRadius)
	}
	lines = append(lines, "execution_read_file and execution_search_repo remain available; avoid redundant re-reads of the same already-fetched region, but if you still need more content to build an exact patch, call the tool again yourself instead of stopping to ask the user for permission.")
	return strings.Join(lines, "\n")
}

// extractExecutionDiagnosisDetails reads the same persisted phase artifact that
// diagnosisMissingForPhase checks (loadStoredPhaseArtifact), not WorkStreamContext.PhaseArtifacts,
// which nothing in the capture path ever populates.
func (a *CopilotAgent) extractExecutionDiagnosisDetails(phaseID string) executionDiagnosisDetails {
	if a == nil {
		return executionDiagnosisDetails{}
	}
	stored, ok := a.loadStoredPhaseArtifact(phaseID)
	if !ok {
		return executionDiagnosisDetails{}
	}
	details := executionDiagnosisDetails{}
	structuredData := stored.StructuredData
	if structuredData != nil {
		if diagnosis, ok := structuredData["diagnosis"].(map[string]any); ok {
			structuredData = diagnosis
		}
		details.RootCause = formatExecutionDiagnosisValue(structuredData["root_cause"])
		details.FixTarget = formatExecutionDiagnosisValue(structuredData["fix_target"])
		details.FocusSymbol = formatExecutionDiagnosisValue(structuredData["focus_symbol"])
		details.SharedWith = formatExecutionDiagnosisValue(structuredData["shared_with"])
		details.BlastRadius = formatExecutionDiagnosisValue(structuredData["blast_radius"])
	}
	if len(stored.Evidence) > 0 {
		details.Evidence = stored.Evidence
	}
	return details
}

func (d executionDiagnosisDetails) isEmpty() bool {
	return strings.TrimSpace(d.RootCause) == "" && strings.TrimSpace(d.FixTarget) == "" && strings.TrimSpace(d.FocusSymbol) == "" && strings.TrimSpace(d.SharedWith) == "" && strings.TrimSpace(d.BlastRadius) == "" && len(d.Evidence) == 0
}

func formatExecutionDiagnosisValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []string:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "; ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, key := range []string{"focus_symbol", "primary", "secondary_if_needed", "path", "file_path", "symbol"} {
			if item, ok := v[key]; ok {
				if text := formatExecutionDiagnosisValue(item); text != "" {
					parts = append(parts, fmt.Sprintf("%s=%s", key, text))
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
		fallback := make([]string, 0, len(v))
		for key, item := range v {
			if text := formatExecutionDiagnosisValue(item); text != "" {
				fallback = append(fallback, fmt.Sprintf("%s=%s", key, text))
			}
		}
		slices := fallback
		if len(slices) == 0 {
			return ""
		}
		return strings.Join(slices, "; ")
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func formatExecutionDiagnosisEvidence(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(item); text != "" {
				result = append(result, text)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				result = append(result, text)
			}
		}
		return result
	default:
		if text := strings.TrimSpace(fmt.Sprint(v)); text != "" && text != "<nil>" {
			return []string{text}
		}
		return nil
	}
}
