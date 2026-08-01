package ai

import (
	"errors"
	"testing"
)

func TestRetryCoordinator_ErrorPattern_NormalizesValidationAndArgumentErrors(t *testing.T) {
	rc := NewRetryCoordinator()
	if got := rc.ErrorPattern("execute_script", errors.New("execute_script validation error [invalid_join_on_placeholder]: join mapping unresolved Example fix: {...}")); got != "execute_script:validation:invalid_join_on_placeholder" {
		t.Fatalf("unexpected validation error pattern: %q", got)
	}
	if got := rc.ErrorPattern("mint_to_space", errors.New("argument 'content' is missing or not a string")); got != "mint_to_space:argument_missing:content" {
		t.Fatalf("unexpected argument error pattern: %q", got)
	}
}

func TestRetryCoordinator_RecommendStrategy_UsesScopeContext(t *testing.T) {
	rc := NewRetryCoordinator()
	pattern := rc.ErrorPattern("execute_script", errors.New("join mapping unresolved"))
	strategy, ok := rc.RecommendStrategy("execute_script", errors.New("join mapping unresolved"), []RetryLearning{
		{ToolName: "execute_script", ErrorPattern: pattern, PreferredStrategy: RepairStrategySameTool, SuccessCount: 5, RepoScope: "other-repo", DomainScope: "coding_workflow", TaskClass: "data"},
		{ToolName: "execute_script", ErrorPattern: pattern, PreferredStrategy: RepairStrategyResearchFirst, SuccessCount: 3, RepoScope: "sop-cloud", DomainScope: "coding_workflow", TaskClass: "data"},
	}, RetryLearningContext{Provider: "chatgpt", Model: "gpt-5.4", RepoScope: "sop-cloud", DomainScope: "coding_workflow", TaskClass: "data"})
	if !ok {
		t.Fatal("expected a recommended strategy")
	}
	if strategy != RepairStrategyResearchFirst {
		t.Fatalf("expected scoped research-first strategy, got %q", strategy)
	}
}
