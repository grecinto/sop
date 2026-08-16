package codingworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type llmPlannerTestGenerator struct {
	response string
	err      error
	prompt   string
}

func (g *llmPlannerTestGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	g.prompt = prompt
	if g.err != nil {
		return ai.GenOutput{}, g.err
	}
	return ai.GenOutput{Text: g.response}, nil
}

func (g *llmPlannerTestGenerator) Name() string { return "llmPlannerTestGenerator" }
func (g *llmPlannerTestGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}
func (g *llmPlannerTestGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }

func TestLLMPlanner_PlanUsesStructuredJSONResponse(t *testing.T) {
	planner := NewLLMPlanner(
		func(ctx context.Context) (any, error) {
			return &llmPlannerTestGenerator{response: `{
				"objective": "Repair parser behavior",
				"workflow_shape": "repair",
				"domain": "coding_workflow",
				"system_context": {
					"subsystem": "parser",
					"primary_targets": ["lib/parser.go", "lib/parser_test.go"],
					"contracts": ["tests and implementation reflect the same parser contract"]
				},
				"steps": [
					{
						"step_id": "repair_1",
						"goal": "Assess the failing behavior",
						"action_instruction": "Compare the failing test to the implementation path.",
						"validation": ["repair direction is explicit"]
					},
					{
						"step_id": "repair_2",
						"goal": "Apply the smallest valid fix",
						"action_instruction": "Patch the parser or test based on the intended contract.",
						"validation": ["targeted parser tests pass"]
					}
				],
				"completion_gate": {
					"requirements": ["rerun the original failing test"]
				}
			}`}, nil
		},
		&DefaultPlanner{},
	)

	plan, err := planner.Plan(
		Task{Description: "fix failing tests in parser", ActiveFile: "lib/parser.go", TaskType: "implementation"},
		RunState{},
		ContextPack{ActiveFile: "lib/parser.go", TestFiles: []string{"lib/parser_test.go"}, RelatedFiles: []string{"lib/parser.go", "lib/parser_test.go"}},
	)
	if err != nil {
		t.Fatalf("expected planner to succeed, got %v", err)
	}
	if plan.Objective != "Repair parser behavior" {
		t.Fatalf("expected objective from LLM response, got %#v", plan)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected two structured steps, got %#v", plan.Steps)
	}
	if plan.Steps[0].Status != "pending" {
		t.Fatalf("expected planner normalization to default step status to pending, got %#v", plan.Steps[0])
	}
	if got := plan.CompletionGate.Requirements; len(got) != 1 || got[0] != "rerun the original failing test" {
		t.Fatalf("expected completion gate from LLM response, got %#v", got)
	}
}

func TestLLMPlanner_PlanFallsBackToDefaultPlannerOnBadJSON(t *testing.T) {
	planner := NewLLMPlanner(
		func(ctx context.Context) (any, error) {
			return &llmPlannerTestGenerator{response: `not-json`}, nil
		},
		&DefaultPlanner{},
	)

	plan, err := planner.Plan(
		Task{Description: "fix failing tests in parser", ActiveFile: "lib/parser.go", TaskType: "implementation"},
		RunState{},
		ContextPack{ActiveFile: "lib/parser.go", TestFiles: []string{"lib/parser_test.go"}, RelatedFiles: []string{"lib/parser.go", "lib/parser_test.go"}},
	)
	if err != nil {
		t.Fatalf("expected fallback planner to succeed, got %v", err)
	}
	if len(plan.Steps) == 0 {
		t.Fatalf("expected fallback structured plan steps, got %#v", plan)
	}
	if plan.Domain != "coding_workflow" {
		t.Fatalf("expected fallback domain, got %#v", plan)
	}
}

func TestLLMPlanner_PlanIncludesCapabilityPlanningContext(t *testing.T) {
	gen := &llmPlannerTestGenerator{response: `{
		"objective": "Plan store repair",
		"workflow_shape": "repair",
		"domain": "stores",
		"steps": [
			{"step_id":"step_1","goal":"Inspect store behavior","action_instruction":"Inspect store shape and failing behavior."}
		]
	}`}
	SetDefaultPlanningContextResolver(func(query string, domain string) string {
		if domain == "stores" {
			return "Stores Planning Context\n- Domain focus: persisted store operations and schema-aware query design."
		}
		return ""
	})
	defer SetDefaultPlanningContextResolver(nil)

	planner := NewLLMPlanner(
		func(ctx context.Context) (any, error) {
			return gen, nil
		},
		&DefaultPlanner{},
	)

	_, err := planner.Plan(
		Task{Description: "fix failing store validation", TaskType: "planning", State: RunState{Metadata: map[string]string{"domain": "stores"}}},
		RunState{Metadata: map[string]string{"domain": "stores"}},
		ContextPack{},
	)
	if err != nil {
		t.Fatalf("expected planner to succeed, got %v", err)
	}
	if !strings.Contains(gen.prompt, "Stores Planning Context") {
		t.Fatalf("expected planner prompt to include capability planning context, got %s", gen.prompt)
	}
	if !strings.Contains(gen.prompt, "Quality bar:") || !strings.Contains(gen.prompt, "decision rule") {
		t.Fatalf("expected richer planner quality guidance in prompt, got %s", gen.prompt)
	}
}

func TestLLMPlanner_NormalizeStructuredPlanEnrichesMissingStepFields(t *testing.T) {
	planner := NewLLMPlanner(
		func(ctx context.Context) (any, error) {
			return &llmPlannerTestGenerator{response: `{
				"objective": "Repair parser behavior",
				"workflow_shape": "repair",
				"steps": [
					{"goal": "Inspect the issue", "action_instruction": "Inspect the failing behavior."},
					{"goal": "Validate the result", "action_instruction": "Validate the repaired behavior."}
				]
			}`}, nil
		},
		&DefaultPlanner{},
	)

	plan, err := planner.Plan(
		Task{Description: "fix failing tests in parser", ActiveFile: "lib/parser.go", TaskType: "implementation"},
		RunState{},
		ContextPack{ActiveFile: "lib/parser.go", TestFiles: []string{"lib/parser_test.go"}, RelatedFiles: []string{"lib/parser.go", "lib/parser_test.go"}, CodeStructureSummary: "parser uses a token stream"},
	)
	if err != nil {
		t.Fatalf("expected planner to succeed, got %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected two steps, got %#v", plan.Steps)
	}
	if len(plan.Steps[0].EvidenceToCollect) == 0 {
		t.Fatalf("expected normalization to add evidence_to_collect, got %#v", plan.Steps[0])
	}
	if strings.TrimSpace(plan.Steps[0].DecisionRule) == "" {
		t.Fatalf("expected normalization to add decision_rule, got %#v", plan.Steps[0])
	}
	if strings.TrimSpace(plan.Steps[0].AllowedChangeScope) == "" {
		t.Fatalf("expected normalization to add allowed_change_scope, got %#v", plan.Steps[0])
	}
	if len(plan.Steps[0].Files) == 0 {
		t.Fatalf("expected normalization to add files, got %#v", plan.Steps[0])
	}
	if len(plan.Steps[0].Notes) == 0 {
		t.Fatalf("expected normalization to add notes, got %#v", plan.Steps[0])
	}
}
