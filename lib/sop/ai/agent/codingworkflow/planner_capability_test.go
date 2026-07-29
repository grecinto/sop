package codingworkflow

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestPlannerCapability_Classify(t *testing.T) {
	orchestrator := &Orchestrator{}
	planner := NewPlannerCapability(orchestrator)

	tests := []struct {
		name     string
		query    string
		state    capabilities.RoutingState
		expected bool
	}{
		{
			name:  "Explicit capability hint",
			query: "fix the bug",
			state: capabilities.RoutingState{
				CapabilityHint: "dynamic_planner",
			},
			expected: true,
		},
		{
			name:  "Planner hint",
			query: "implement feature",
			state: capabilities.RoutingState{
				CapabilityHint: "planner",
			},
			expected: true,
		},
		{
			name:  "No hint - should not handle",
			query: "fix the bug",
			state: capabilities.RoutingState{
				CapabilityHint: "",
			},
			expected: false,
		},
		{
			name:  "Different capability hint",
			query: "fix the bug",
			state: capabilities.RoutingState{
				CapabilityHint: "workflow_manager",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := planner.Classify(tt.query, tt.state)
			if result.Relevant != tt.expected {
				t.Errorf("Classify() relevant = %v, expected %v", result.Relevant, tt.expected)
			}
		})
	}
}

func TestPlannerCapability_PlanToSteps(t *testing.T) {
	orchestrator := &Orchestrator{
		ContextManager: &DefaultContextManager{},
		RepoIndexer:    &DefaultRepoIndexer{},
		Memory:         NewInMemorySessionMemory(),
	}
	planner := NewPlannerCapability(orchestrator)

	plan := &WorkflowPlan{
		Name:        "Test Plan",
		Description: "A test plan",
		Steps: []PlanStep{
			{
				Name:        "Analyze Code",
				Description: "Analyze the code structure",
				Type:        "analyze",
				Tags:        []string{"analysis"},
			},
			{
				Name:         "Generate Patch",
				Description:  "Generate a patch",
				Type:         "generate",
				Dependencies: []string{"Analyze Code"},
				Tags:         []string{"patch"},
			},
			{
				Name:         "Apply Changes",
				Description:  "Apply the patch",
				Type:         "apply",
				Dependencies: []string{"Generate Patch"},
				Tags:         []string{"file_write"},
			},
			{
				Name:         "Verify Tests",
				Description:  "Run tests to verify",
				Type:         "verify",
				Dependencies: []string{"Apply Changes"},
				Tags:         []string{"test"},
			},
		},
		Rationale: "Test rationale",
	}

	steps, err := planner.planToSteps(plan)
	if err != nil {
		t.Fatalf("planToSteps failed: %v", err)
	}

	if len(steps) != len(plan.Steps) {
		t.Errorf("expected %d steps, got %d", len(plan.Steps), len(steps))
	}

	// Verify each step was created correctly
	for i, step := range steps {
		if step == nil {
			t.Errorf("step %d is nil", i)
			continue
		}

		metadata := step.Metadata()
		if metadata.Name == "" {
			t.Errorf("step %d has empty name", i)
		}
	}
}

func TestPlannerCapability_EmptyPlan(t *testing.T) {
	orchestrator := &Orchestrator{}
	planner := NewPlannerCapability(orchestrator)

	// Test with nil plan
	_, err := planner.planToSteps(nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}

	// Test with empty plan
	emptyPlan := &WorkflowPlan{
		Name:  "Empty",
		Steps: []PlanStep{},
	}
	_, err = planner.planToSteps(emptyPlan)
	if err == nil {
		t.Error("expected error for empty plan")
	}
}

func TestPlannerCapability_FormatPlanSummary(t *testing.T) {
	planner := NewPlannerCapability(&Orchestrator{})

	plan := &WorkflowPlan{
		Name:        "Plan",
		Description: "A plan for the user",
		Steps: []PlanStep{
			{Name: "Inspect", Description: "Inspect the repository", Type: "analyze"},
			{Name: "Implement", Description: "Implement the change", Type: "generate"},
		},
		Rationale: "This keeps the work focused",
	}

	summary := planner.formatPlanSummary(plan)
	if !strings.Contains(summary, "Inspect") {
		t.Fatalf("expected summary to include step names, got %q", summary)
	}
	if !strings.Contains(summary, "This keeps the work focused") {
		t.Fatalf("expected summary to include rationale, got %q", summary)
	}
}
