package generator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

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
