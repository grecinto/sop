package generator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type stubRetryPolicySurface struct {
	recoverable bool
	decision    ai.RetryDecision
	budget      ai.RetryBudgetDecision
	extension   ai.RetryBudgetExtensionDecision
}

func (p stubRetryPolicySurface) IsRecoverable(err error) bool {
	return p.recoverable && err != nil
}

func (p stubRetryPolicySurface) EvaluateRetryDecision(input ai.RetryDecisionInput) ai.RetryDecision {
	return p.decision
}

func (p stubRetryPolicySurface) InitialRetryBudget(input ai.RetryBudgetInput) ai.RetryBudgetDecision {
	if p.budget.SoftLimit == 0 {
		return ai.RetryBudgetDecision{SoftLimit: input.BaseTurns, HardLimit: input.BaseTurns + 1}
	}
	return p.budget
}

func (p stubRetryPolicySurface) EvaluateRetryBudgetExtension(input ai.RetryBudgetExtensionInput) ai.RetryBudgetExtensionDecision {
	return p.extension
}

func (p stubRetryPolicySurface) AllowsTool(plan ai.RepairPlan, toolName string) bool {
	return toolName == plan.ToolName
}

func (p stubRetryPolicySurface) FormatRepairDirective(plan ai.RepairPlan, attemptedToolName string) string {
	return "repair directive"
}

func TestOwnedLoopRetryState_UsesCarryoverLearnedStrategy(t *testing.T) {
	state := newOwnedLoopRetryState(2, "join users and orders", &ai.CarryoverState{
		RetryLearnings: []ai.RetryLearning{{
			ToolName:          "execute_script",
			ErrorPattern:      ai.NewRetryCoordinator().ErrorPattern("execute_script", errors.New("join mapping unresolved")),
			PreferredStrategy: ai.RepairStrategyResearchFirst,
			SuccessCount:      3,
			FailureCount:      0,
		}},
	})

	result, escalated := state.handleRecoverableFailure(
		ai.ToolCall{Name: "execute_script", Args: map[string]any{"script": []any{map[string]any{"op": "join"}}}},
		errors.New("join mapping unresolved"),
		nil,
	)
	if escalated {
		t.Fatal("did not expect immediate escalation")
	}
	if state.pendingRepair == nil || state.pendingRepair.Strategy != ai.RepairStrategyResearchFirst {
		t.Fatalf("expected learned research-first strategy, got %#v", state.pendingRepair)
	}
	if !containsString(result.Result, "Repair strategy: research_first") {
		t.Fatalf("expected retry result to include learned research strategy, got %q", result.Result)
	}
	if result.RetryGuidance == nil || result.RetryGuidance.Strategy != ai.RepairStrategyResearchFirst {
		t.Fatalf("expected structured retry guidance for research-first repair, got %#v", result.RetryGuidance)
	}
}

func TestOwnedLoopRetryState_ExtendsBudgetOnProgress(t *testing.T) {
	state := newOwnedLoopRetryState(1, "inspect repo", nil)
	if state.allowedTurns != 1 {
		t.Fatalf("expected initial budget 1, got %d", state.allowedTurns)
	}
	extended := state.maybeExtendBudget([]ai.ReActToolResult{{
		Name: "list_stores",
		Hint: &ai.ToolProgressHint{Status: "progressing", CompletionDelta: 0.5},
	}}, 1)
	if !extended {
		t.Fatal("expected budget extension on progress")
	}
	if state.allowedTurns <= 1 {
		t.Fatalf("expected budget to increase, got %d", state.allowedTurns)
	}
}

func TestOwnedLoopRetryState_RecordsSuccessLearning(t *testing.T) {
	state := newOwnedLoopRetryState(2, "fix task", nil)
	toolCall := ai.ToolCall{Name: "mint_to_space", Args: map[string]any{"space": "Tasks"}}
	_, _ = state.handleRecoverableFailure(toolCall, errors.New("argument 'content' is missing or not a string"), nil)
	state.completeSuccessfulTool("mint_to_space")
	if len(state.exportedLearnings()) == 0 {
		t.Fatal("expected successful retry learning to be recorded")
	}
	learning := state.exportedLearnings()[0]
	if learning.SuccessCount != 1 {
		t.Fatalf("expected success count 1, got %#v", learning)
	}
}

func TestOwnedLoopRetryState_PrefersScopedLearningContext(t *testing.T) {
	pattern := ai.NewRetryCoordinator().ErrorPattern("execute_script", errors.New("join mapping unresolved"))
	state := newOwnedLoopRetryState(2, "join users and orders", &ai.CarryoverState{
		RetryLearnings: []ai.RetryLearning{
			{ToolName: "execute_script", ErrorPattern: pattern, PreferredStrategy: ai.RepairStrategySameTool, SuccessCount: 5, RepoScope: "other-repo", DomainScope: "coding_workflow", TaskClass: "data"},
			{ToolName: "execute_script", ErrorPattern: pattern, PreferredStrategy: ai.RepairStrategyResearchFirst, SuccessCount: 3, RepoScope: "sop-cloud", DomainScope: "coding_workflow", TaskClass: "data"},
		},
	})
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{ActiveRepo: "sop-cloud", ActiveDomain: "coding_workflow"})
	state.configureLearningContext(deriveRetryLearningContext(ctx, ai.ReasoningRequest{UserQuery: "join users and orders"}, "chatgpt", "gpt-5.4"))
	result, escalated := state.handleRecoverableFailure(ai.ToolCall{Name: "execute_script", Args: map[string]any{}}, errors.New("join mapping unresolved"), nil)
	if escalated {
		t.Fatal("did not expect immediate escalation")
	}
	if state.pendingRepair == nil || state.pendingRepair.Strategy != ai.RepairStrategyResearchFirst {
		t.Fatalf("expected scoped research-first learning to win, got %#v", state.pendingRepair)
	}
	if !strings.Contains(result.Result, "Repair strategy: research_first") {
		t.Fatalf("expected retry result to mention research_first, got %q", result.Result)
	}
}

func TestOwnedLoopRetryState_UsesPolicySurfaceForBudget(t *testing.T) {
	state := newOwnedLoopRetryStateWithPolicy(2, "fix task", nil, stubRetryPolicySurface{
		budget: ai.RetryBudgetDecision{SoftLimit: 4, HardLimit: 6},
	})
	if state.allowedTurns != 4 {
		t.Fatalf("expected soft limit from policy, got %d", state.allowedTurns)
	}
	if state.maxAllowedTurns != 6 {
		t.Fatalf("expected hard limit from policy, got %d", state.maxAllowedTurns)
	}
}

func TestOwnedLoopRetryState_UsesPolicySurfaceForRetryDecision(t *testing.T) {
	decision := ai.RetryDecision{
		Tier: ai.RetryTierResearch,
		Plan: ai.RepairPlan{
			ToolName:       "execute_script",
			Strategy:       ai.RepairStrategyResearchFirst,
			Tier:           ai.RetryTierResearch,
			ResearchTool:   "list_stores",
			ResearchReason: "policy selected research",
		},
		Recommendation: &ai.RetryLearningRecommendation{Strategy: ai.RepairStrategyResearchFirst, Score: 1.0, Reason: "policy test"},
	}
	state := newOwnedLoopRetryStateWithPolicy(2, "join users and orders", nil, stubRetryPolicySurface{
		recoverable: true,
		decision:    decision,
		budget:      ai.RetryBudgetDecision{SoftLimit: 2, HardLimit: 4},
	})
	result, escalated := state.handleRecoverableFailure(ai.ToolCall{Name: "execute_script", Args: map[string]any{}}, errors.New("join mapping unresolved"), nil)
	if escalated {
		t.Fatal("did not expect clarification escalation")
	}
	if state.pendingRepair == nil || state.pendingRepair.Strategy != ai.RepairStrategyResearchFirst {
		t.Fatalf("expected policy-selected repair strategy, got %#v", state.pendingRepair)
	}
	if !strings.Contains(result.Result, "Repair strategy: research_first") {
		t.Fatalf("expected retry result to include policy-selected strategy, got %q", result.Result)
	}
	if !strings.Contains(result.Result, "policy test") {
		t.Fatalf("expected retry result to include policy recommendation details, got %q", result.Result)
	}
}

func TestOwnedLoopRetryState_UsesPolicySurfaceForRecoverability(t *testing.T) {
	state := newOwnedLoopRetryStateWithPolicy(1, "fix task", nil, stubRetryPolicySurface{recoverable: true})
	if !state.isRecoverableFailure(errors.New("recoverable")) {
		t.Fatal("expected policy surface to classify error as recoverable")
	}
	state = newOwnedLoopRetryStateWithPolicy(1, "fix task", nil, stubRetryPolicySurface{recoverable: false})
	if state.isRecoverableFailure(errors.New("recoverable")) {
		t.Fatal("expected policy surface to classify error as non-recoverable")
	}
}

func containsString(text, needle string) bool {
	return len(needle) == 0 || (len(text) >= len(needle) && (text == needle || stringContainsAt(text, needle)))
}

func stringContainsAt(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
