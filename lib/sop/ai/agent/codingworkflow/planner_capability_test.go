package codingworkflow

import (
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestPlannerCapability_Run(t *testing.T) {
	// Create orchestrator with minimal dependencies
	orchestrator := &Orchestrator{
		ContextManager: &DefaultContextManager{},
		RepoIndexer:    &DefaultRepoIndexer{},
		PatchEngine:    &DefaultPatchEngine{},
		Memory:         NewInMemorySessionMemory(),
		TestRunner:     &DefaultTestRunner{},
		Planner:        &DefaultPlanner{},
		ProviderRouter: &DefaultProviderRouter{},
	}

	// Create planner capability
	planner := NewPlannerCapability(orchestrator)

	// Test cases for different task types
	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "Debugging task",
			query: "fix the bug in parser.go",
		},
		{
			name:  "Implementation task",
			query: "implement a new feature for user authentication",
		},
		{
			name:  "Testing task",
			query: "write tests for the authentication module",
		},
		{
			name:  "Refactoring task",
			query: "refactor the database layer to use interfaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := capabilities.RoutingState{
				Domain:         "coding",
				Gate:           "write",
				CapabilityHint: "dynamic_planner",
			}

			// Call Run - should generate and return a plan
			result, err := planner.Run(tt.query, state)

			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if result.Summary == "" {
				t.Error("Run returned empty summary")
			}

			// Verify plan data is present
			if result.Data == nil {
				t.Fatal("Run returned nil Data")
			}

			if _, ok := result.Data["plan"]; !ok {
				t.Error("Run result missing 'plan' key in Data")
			}

			if _, ok := result.Data["steps"]; !ok {
				t.Error("Run result missing 'steps' key in Data")
			}

			t.Logf("Plan summary: %s", result.Summary)
		})
	}
}

func TestPlannerCapability_CanHandle(t *testing.T) {
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
			result := planner.CanHandle(tt.query, tt.state)
			if result != tt.expected {
				t.Errorf("CanHandle() = %v, expected %v", result, tt.expected)
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
