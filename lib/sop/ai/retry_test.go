package ai

import (
	"errors"
	"strings"
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

func TestDefaultRetryPolicySurface_EvaluateRetryDecision(t *testing.T) {
	rc := NewRetryCoordinator()
	policy := NewDefaultRetryPolicySurfaceWithCoordinator(rc)
	pattern := rc.ErrorPattern("execute_script", errors.New("join mapping unresolved"))
	decision := policy.EvaluateRetryDecision(RetryDecisionInput{
		ToolName:     "execute_script",
		Err:          errors.New("join mapping unresolved"),
		FailureCount: 0,
		Learnings: []RetryLearning{{
			ToolName:          "execute_script",
			ErrorPattern:      pattern,
			PreferredStrategy: RepairStrategyResearchFirst,
			SuccessCount:      3,
		}},
	})
	if decision.Plan.ToolName != "execute_script" {
		t.Fatalf("expected plan tool name to be preserved, got %#v", decision.Plan)
	}
	if decision.Tier != RetryTierResearch {
		t.Fatalf("expected research tier for join ambiguity, got %d", decision.Tier)
	}
	if decision.Recommendation == nil || decision.Recommendation.Strategy != RepairStrategyResearchFirst {
		t.Fatalf("expected learned recommendation to be surfaced, got %#v", decision.Recommendation)
	}
}

func TestDefaultRetryPolicySurface_BudgetDecisions(t *testing.T) {
	policy := NewDefaultRetryPolicySurfaceWithCoordinator(NewRetryCoordinator())
	budget := policy.InitialRetryBudget(RetryBudgetInput{BaseTurns: 2, Query: "refactor this workflow"})
	if budget.SoftLimit < 2 {
		t.Fatalf("expected soft limit to respect base turns, got %#v", budget)
	}
	if budget.HardLimit < budget.SoftLimit {
		t.Fatalf("expected hard limit to bound soft limit, got %#v", budget)
	}
	extension := policy.EvaluateRetryBudgetExtension(RetryBudgetExtensionInput{
		AllowedTurns:     2,
		MaxAllowedTurns:  4,
		CurrentIteration: 2,
		ProgressHint:     &ToolProgressHint{CompletionDelta: 0.5},
		ExplicitProgress: true,
	})
	if !extension.Allow || extension.AdditionalTurns != 2 {
		t.Fatalf("expected progress-driven extension, got %#v", extension)
	}
}

func TestBuildRetryGuidance_ResearchFirstSetsAllowedToolAndNextAction(t *testing.T) {
	guidance := BuildRetryGuidance("execute_script", RepairPlan{
		ToolName:     "execute_script",
		Strategy:     RepairStrategyResearchFirst,
		Tier:         RetryTierResearch,
		ResearchTool: "list_stores",
	}, &RetryLearningRecommendation{
		Strategy: RepairStrategyResearchFirst,
		Score:    2.5,
		Reason:   "repo-scoped",
	}, 2, 5, 7, false)
	if guidance == nil {
		t.Fatal("expected retry guidance")
	}
	if guidance.AllowedTool != "list_stores" || guidance.NextAction != ReActNextActionCallTool {
		t.Fatalf("expected research-first guidance to target list_stores, got %#v", guidance)
	}
	if guidance.RecommendedStrategy != RepairStrategyResearchFirst {
		t.Fatalf("expected learned recommendation to be preserved, got %#v", guidance)
	}
}

func TestAppendRetryGuidanceState_AppendsStructuredState(t *testing.T) {
	text := AppendRetryGuidanceState("Retry instruction: Call execute_script next.", &RetryGuidance{
		ToolName:        "execute_script",
		Tier:            RetryTierQuickFix,
		Strategy:        RepairStrategySameTool,
		AllowedTool:     "execute_script",
		NextAction:      ReActNextActionRetrySameTool,
		FailureCount:    1,
		SoftRetryBudget: 2,
		HardRetryBudget: 4,
	})
	if !strings.Contains(text, "Retry policy state:") {
		t.Fatalf("expected structured state to be appended, got %q", text)
	}
	if !strings.Contains(text, `"next_action":"retry_same_tool"`) {
		t.Fatalf("expected next_action in appended state, got %q", text)
	}
}

func TestBuildRepairDirectiveFromRetryGuidance_UsesStructuredStrategy(t *testing.T) {
	directive := BuildRepairDirectiveFromRetryGuidance("execute_script", &RetryGuidance{
		ToolName:     "execute_script",
		Strategy:     RepairStrategyResearchFirst,
		AllowedTool:  "list_stores",
		NextAction:   ReActNextActionCallTool,
		FailureCount: 2,
	})
	if !strings.Contains(directive, "call list_stores first") {
		t.Fatalf("expected research-first directive from structured guidance, got %q", directive)
	}
}

func TestDefaultCognitivePolicySurface_ShouldEvaluateOnRetryOrClarificationSignals(t *testing.T) {
	policy := NewDefaultCognitivePolicySurface()
	if !policy.ShouldEvaluateSessionCognition(SessionCognitiveInput{Observation: SessionCognitiveObservation{PendingClarification: true}}) {
		t.Fatal("expected pending clarification to trigger cognitive evaluation")
	}
	if !policy.ShouldEvaluateSessionCognition(SessionCognitiveInput{Observation: SessionCognitiveObservation{RetryTier: 1}}) {
		t.Fatal("expected retry tier to trigger cognitive evaluation")
	}
	if !policy.ShouldEvaluateSessionCognition(SessionCognitiveInput{Observation: SessionCognitiveObservation{Metadata: map[string]any{"repeated_continuation_prompt": true}}}) {
		t.Fatal("expected repeated continuation prompt metadata to trigger cognitive evaluation")
	}
	if policy.ShouldEvaluateSessionCognition(SessionCognitiveInput{Observation: SessionCognitiveObservation{CurrentPhase: "planning"}}) {
		t.Fatal("expected quiet state not to trigger cognitive evaluation")
	}
}

func TestDefaultCognitivePolicySurface_EvaluateSessionCognition(t *testing.T) {
	policy := NewDefaultCognitivePolicySurface()
	assessment := policy.EvaluateSessionCognition(SessionCognitiveInput{Observation: SessionCognitiveObservation{
		CurrentPhase:         "execution",
		EscalationTier:       3,
		RecentFailureClasses: []string{"implementation_bug", "implementation_bug"},
	}})
	if assessment.FailureClass != CognitiveFailureClassLooping {
		t.Fatalf("expected looping failure class, got %#v", assessment)
	}
	if assessment.PhaseAction != CognitivePhaseActionMoveBackward {
		t.Fatalf("expected backward phase action, got %#v", assessment)
	}
	if assessment.RetryAction != CognitiveRetryActionAskClarification {
		t.Fatalf("expected ask clarification retry action, got %#v", assessment)
	}

	assessment = policy.EvaluateSessionCognition(SessionCognitiveInput{Observation: SessionCognitiveObservation{
		CurrentPhase: "investigation",
		LatestHint:   &ToolProgressHint{SuggestedAction: "research_before_retry"},
	}})
	if assessment.RetryAction != CognitiveRetryActionResearchFirst {
		t.Fatalf("expected research-first retry action, got %#v", assessment)
	}
	if assessment.FailureClass != CognitiveFailureClassMissingFact {
		t.Fatalf("expected missing-fact failure class, got %#v", assessment)
	}

	assessment = policy.EvaluateSessionCognition(SessionCognitiveInput{Observation: SessionCognitiveObservation{
		CurrentPhase: "planning",
		Metadata: map[string]any{
			"repeated_continuation_prompt": true,
			"continuation_prompt_pattern":  "advance_request",
		},
	}})
	if assessment.FailureClass != CognitiveFailureClassLooping {
		t.Fatalf("expected looping failure class for repeated continuation prompts, got %#v", assessment)
	}
	if assessment.RetryAction != CognitiveRetryActionAskClarification {
		t.Fatalf("expected clarify retry action for repeated continuation prompts, got %#v", assessment)
	}
}
