package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type cognitiveAssessmentMockGenerator struct {
	response string
	prompt   string
	opts     ai.GenOptions
}

func (m *cognitiveAssessmentMockGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.prompt = prompt
	m.opts = opts
	return ai.GenOutput{Text: m.response}, nil
}

func (m *cognitiveAssessmentMockGenerator) Name() string { return "cognitive-assessment-mock" }

func (m *cognitiveAssessmentMockGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }

func (m *cognitiveAssessmentMockGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func TestEvaluateSessionCognitivePolicy_PausesForPendingClarification(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any), ClarificationState: &ai.ClarificationState{Status: "pending"}}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "continue the workflow", "phase:planning", 3, "planning", "tool_error", "clarification needed")
	if decision.Action != "pause_for_clarification" {
		t.Fatalf("expected pause_for_clarification action, got %q", decision.Action)
	}
	if decision.Reason == "" {
		t.Fatal("expected a non-empty reason")
	}
}

func TestEvaluateSessionCognitivePolicy_EscalatesRetryWhenTierIsHigh(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	payload.CognitiveState = &ai.SessionCognitiveState{EscalationTier: 3}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "retry the same ask", "phase:execution", 2, "execution", "retry_error", "tier 3 escalation")
	if decision.Action != "escalate_retry" {
		t.Fatalf("expected escalate_retry action, got %q", decision.Action)
	}
}

func TestEvaluateSessionCognitivePolicy_UsesAssessmentToPauseForClarification(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	payload.CognitiveState = &ai.SessionCognitiveState{LastAssessment: &ai.SessionCognitiveAssessment{
		FailureClass:        ai.CognitiveFailureClassMissingFact,
		RetryAction:         ai.CognitiveRetryActionAskClarification,
		ClarificationNeeded: true,
		Rationale:           "A missing requirement blocks safe continuation.",
	}}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "continue the workflow", "phase:planning", 3, "planning", "tool_error", "clarification needed")
	if decision.Action != "pause_for_clarification" {
		t.Fatalf("expected pause_for_clarification action, got %q", decision.Action)
	}
	if decision.Reason != "A missing requirement blocks safe continuation." {
		t.Fatalf("expected cognitive rationale to be preserved, got %q", decision.Reason)
	}
	if payload.CognitiveState == nil || !payload.CognitiveState.QueuePaused {
		t.Fatalf("expected queue to be paused, got %#v", payload.CognitiveState)
	}
}

func TestEvaluateSessionCognitivePolicy_UsesAssessmentToStopOnTerminalBlock(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	payload.CognitiveState = &ai.SessionCognitiveState{LastAssessment: &ai.SessionCognitiveAssessment{
		FailureClass:   ai.CognitiveFailureClassTerminalFailure,
		RetryAction:    ai.CognitiveRetryActionStop,
		SuggestedPhase: "delivery",
		Rationale:      "The environment block requires user control before continuing.",
	}}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "deploy the change", "phase:execution", 4, "execution", "tool_error", "terminal environment block")
	if decision.Action != "stop" {
		t.Fatalf("expected stop action, got %q", decision.Action)
	}
	if decision.SuggestedPhase != "delivery" {
		t.Fatalf("expected suggested phase to be preserved, got %q", decision.SuggestedPhase)
	}
	if decision.Reason != "The environment block requires user control before continuing." {
		t.Fatalf("expected cognitive rationale to be preserved, got %q", decision.Reason)
	}
}

func TestEvaluateSessionCognitivePolicy_UsesAssessmentToMoveBackwardToSuggestedPhase(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	payload.CognitiveState = &ai.SessionCognitiveState{LastAssessment: &ai.SessionCognitiveAssessment{
		FailureClass:   ai.CognitiveFailureClassWrongPhase,
		PhaseAction:    ai.CognitivePhaseActionMoveBackward,
		RetryAction:    ai.CognitiveRetryActionResearchFirst,
		SuggestedPhase: "investigation",
		Rationale:      "Execution is looping because the task needs more investigation first.",
	}}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "fix failing tests", "phase:execution", 5, "execution", "tool_error", "looping failures")
	if decision.Action != "escalate_retry" {
		t.Fatalf("expected escalate_retry action, got %q", decision.Action)
	}
	if decision.SuggestedPhase != "investigation" {
		t.Fatalf("expected suggested phase investigation, got %q", decision.SuggestedPhase)
	}
	if decision.Reason != "Execution is looping because the task needs more investigation first." {
		t.Fatalf("expected cognitive rationale to be preserved, got %q", decision.Reason)
	}
}

func TestEvaluateSessionCognitivePolicy_EscalatesRepeatedContinuationPromptPattern(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	payload.CognitiveState = &ai.SessionCognitiveState{
		CurrentPhase:  "planning",
		RecentQueries: []string{"proceed"},
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "go ahead", "phase:planning", 4, "planning", "", "")
	if decision.Action != "escalate_retry" {
		t.Fatalf("expected repeated continuation prompt pattern to trigger cognitive escalation, got %q", decision.Action)
	}
	if !strings.Contains(decision.Reason, "repeated unresolved continuation prompt") {
		t.Fatalf("expected escalation reason to explain repeated continuation prompt, got %q", decision.Reason)
	}
	if !strings.Contains(decision.Reason, "advance_request") {
		t.Fatalf("expected escalation reason to include continuation pattern class, got %q", decision.Reason)
	}
	if decision.SuggestedPhase != "planning" {
		t.Fatalf("expected current phase to be preserved as suggested phase, got %q", decision.SuggestedPhase)
	}
}

func TestRefreshSessionCognitiveAssessment_AnnotatesRepeatedContinuationPromptPattern(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any), CognitiveState: &ai.SessionCognitiveState{RecentQueries: []string{"proceed"}}}
	ctx := context.WithValue(context.Background(), "session_payload", payload)
	ctx = context.WithValue(ctx, ctxKeyAskWorkItemActor, "phase:planning")
	gen := &cognitiveAssessmentMockGenerator{response: `{"failure_class":"looping","progress_state":"looping","phase_action":"stay_current","retry_action":"ask_clarification","suggested_phase":"planning","clarification_needed":true,"loop_risk":"high","confidence":"high","rationale":"Repeated continuation nudges without progress indicate the workflow should explain the blocker before continuing."}`}

	assessment := ag.refreshSessionCognitiveAssessment(ctx, "go ahead", "planning", "", nil, nil, nil, gen)
	if assessment == nil {
		t.Fatal("expected cognitive assessment")
	}
	if !strings.Contains(gen.prompt, "repeated_continuation_prompt") {
		t.Fatalf("expected cognitive prompt to include repeated continuation metadata, got %q", gen.prompt)
	}
	if !strings.Contains(gen.prompt, "advance_request") {
		t.Fatalf("expected cognitive prompt to include continuation pattern class, got %q", gen.prompt)
	}
}

func TestRepeatedContinuationPrompt_GroupsDifferentAdvancePhrases(t *testing.T) {
	repeated, count, descriptor := repeatedContinuationPrompt([]string{"proceed", "go ahead"})
	if !repeated {
		t.Fatal("expected semantically equivalent advance prompts to count as repeated continuation pattern")
	}
	if count != 2 {
		t.Fatalf("expected repeat count 2, got %d", count)
	}
	if descriptor.Pattern != "advance_request" {
		t.Fatalf("expected advance_request pattern, got %#v", descriptor)
	}
	if len(descriptor.Examples) != 2 {
		t.Fatalf("expected examples to be preserved, got %#v", descriptor)
	}
}

func TestBuildSessionCognitiveState_UsesMemorySignals(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: NewRunnerSession()}
	ag.service.session.MRU = []MRUItem{{Category: "ask_outcome", Context: "previous task failed", Source: MRUSourceAskOutcome, Scope: MRUScopeSession}}
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	state := ag.buildSessionCognitiveState(ctx, "continue the workflow", "phase:planning", 1, "planning")
	if len(state.MemorySignals) == 0 {
		t.Fatal("expected memory signals to be populated")
	}
	if state.CurrentActor != "phase:planning" {
		t.Fatalf("expected current actor to be preserved, got %q", state.CurrentActor)
	}
}

func TestRefreshSessionCognitiveAssessment_UsesLLMRecommendationForEscalation(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)
	ctx = context.WithValue(ctx, ai.CtxKeyProvider, "openai:gpt-5.4")
	ctx = context.WithValue(ctx, ctxKeyAskWorkItemActor, "phase:execution")
	gen := &cognitiveAssessmentMockGenerator{response: `{"failure_class":"wrong_phase","progress_state":"looping","phase_action":"move_backward","retry_action":"research_first","suggested_phase":"investigation","clarification_needed":false,"loop_risk":"high","confidence":"high","rationale":"Repeated execution failures show the task needs renewed investigation before more implementation."}`}
	escalation := map[string]any{"tier": 3, "failed_tool": "execution_apply_patch", "failure_count": 2, "error_type": "tests keep failing"}

	assessment := ag.refreshSessionCognitiveAssessment(ctx, "fix failing tests", "execution", "tool failure", nil, escalation, nil, gen)
	if assessment == nil {
		t.Fatal("expected cognitive assessment")
	}
	if assessment.SuggestedPhase != "investigation" {
		t.Fatalf("expected suggested phase investigation, got %#v", assessment)
	}
	if escalation["suggested_phase"] != "investigation" {
		t.Fatalf("expected escalation to carry suggested phase, got %#v", escalation)
	}
	if payload.CognitiveState == nil || payload.CognitiveState.LastAssessment == nil {
		t.Fatalf("expected last assessment to be stored in payload state, got %#v", payload.CognitiveState)
	}
	if gen.opts.ThinkingLevel == "" {
		t.Fatal("expected strict-json thinking level to be set")
	}
	if gen.prompt == "" || !strings.Contains(gen.prompt, "suggested_phase") {
		t.Fatalf("expected cognitive assessment prompt to include strict schema, got %q", gen.prompt)
	}
}

func TestEvaluatePhaseTransactionCognitiveDecision_QueuesPlanningAfterFailingTestInvestigation(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "investigation",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransactionCognitiveDecision(ctx, classification.OriginalQuery, classification, []ai.ToolCall{{Name: "run_repo_tests"}}, []string{"--- FAIL: TestAuthenticateUser_ValidCredentials"}, "", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected failing-test investigation to enqueue planning")
	}
	if decision.NextPhase != "planning" {
		t.Fatalf("expected suggested next phase planning, got %q", decision.NextPhase)
	}
	if payload.CognitiveState == nil || payload.CognitiveState.LastAssessment == nil {
		t.Fatalf("expected cognitive state assessment to be stored, got %#v", payload.CognitiveState)
	}
	if payload.CognitiveState.LastAssessment.SuggestedPhase != "planning" {
		t.Fatalf("expected cognitive suggested phase planning, got %#v", payload.CognitiveState.LastAssessment)
	}
	if payload.CognitiveState.LastDecision != "enqueue_phase" {
		t.Fatalf("expected cognitive last decision to record enqueue_phase, got %#v", payload.CognitiveState)
	}
}

func TestEvaluatePhaseTransactionCognitiveDecision_QueuesPlanningWhenExecutionLacksReadyArtifact(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "execution",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransactionCognitiveDecision(ctx, classification.OriginalQuery, classification, nil, nil, "", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected missing ready-for-execution artifact to enqueue planning")
	}
	if decision.NextPhase != "planning" {
		t.Fatalf("expected suggested next phase planning, got %q", decision.NextPhase)
	}
	if payload.CognitiveState == nil || payload.CognitiveState.LastAssessment == nil {
		t.Fatalf("expected cognitive state assessment to be stored, got %#v", payload.CognitiveState)
	}
	if payload.CognitiveState.LastAssessment.SuggestedPhase != "planning" {
		t.Fatalf("expected cognitive suggested phase planning, got %#v", payload.CognitiveState.LastAssessment)
	}
}

func TestEvaluatePhaseTransactionCognitiveDecision_RequeuesExecutionWhenValidationEvidenceIsMissing(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "execution",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		ExecutionReady:     true,
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransactionCognitiveDecision(ctx, classification.OriginalQuery, classification, []ai.ToolCall{{Name: "execution_apply_patch"}}, []string{"Patch applied"}, "", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected missing validation evidence to continue execution")
	}
	if decision.NextPhase != "execution" {
		t.Fatalf("expected suggested next phase execution, got %q", decision.NextPhase)
	}
	if payload.CognitiveState == nil || payload.CognitiveState.LastAssessment == nil {
		t.Fatalf("expected cognitive state assessment to be stored, got %#v", payload.CognitiveState)
	}
	if payload.CognitiveState.LastAssessment.SuggestedPhase != "execution" {
		t.Fatalf("expected cognitive suggested phase execution, got %#v", payload.CognitiveState.LastAssessment)
	}
}

func TestEvaluatePhaseTransactionCognitiveDecision_QueuesExecutionWhenPlanningArtifactIsReady(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	artifact := `{"summary":"Execution plan for fix failing tests","status":"ready_for_execution","plan":["inspect failing tests","patch root cause","rerun focused tests"]}`
	if err := ag.service.session.Memory.SaveWorkflowMemory("phase-artifact:planning", map[string]string{"artifact_json": artifact}); err != nil {
		t.Fatalf("failed to seed planning artifact: %v", err)
	}
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "planning",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransactionCognitiveDecision(ctx, classification.OriginalQuery, classification, []ai.ToolCall{{Name: "planning_plan_changes"}}, []string{"Execution plan captured"}, "Execution plan captured with ready_for_execution status.", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected ready planning artifact to enqueue execution")
	}
	if decision.NextPhase != "execution" {
		t.Fatalf("expected suggested next phase execution, got %q", decision.NextPhase)
	}
	if payload.CognitiveState == nil || payload.CognitiveState.LastAssessment == nil {
		t.Fatalf("expected cognitive state assessment to be stored, got %#v", payload.CognitiveState)
	}
	if payload.CognitiveState.LastAssessment.SuggestedPhase != "execution" {
		t.Fatalf("expected cognitive suggested phase execution, got %#v", payload.CognitiveState.LastAssessment)
	}
	if payload.CognitiveState.LastDecision != "enqueue_phase" {
		t.Fatalf("expected cognitive last decision to record enqueue_phase, got %#v", payload.CognitiveState)
	}
}
