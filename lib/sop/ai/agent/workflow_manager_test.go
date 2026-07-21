package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWorkflowManager_New(t *testing.T) {
	wm := NewWorkflowManager(nil)
	if wm == nil {
		t.Fatal("NewWorkflowManager returned nil")
	}
	if wm.conditionRegistry == nil {
		t.Error("conditionRegistry is nil")
	}

	// Verify built-in conditions are registered
	builtInConditions := []string{"tests_still_failing", "has_error_output", "tests_passing"}
	for _, cond := range builtInConditions {
		if _, ok := wm.conditionRegistry[cond]; !ok {
			t.Errorf("Built-in condition %q not registered", cond)
		}
	}
}

func TestWorkflowManager_RegisterCondition(t *testing.T) {
	wm := NewWorkflowManager(nil)

	called := false
	wm.RegisterCondition("test_condition", func(result TurnResult) bool {
		called = true
		return true
	})

	evaluator, ok := wm.conditionRegistry["test_condition"]
	if !ok {
		t.Fatal("Condition not registered")
	}

	result := evaluator(TurnResult{})
	if !result {
		t.Error("Evaluator returned false, expected true")
	}
	if !called {
		t.Error("Evaluator was not called")
	}
}

func TestExpandTemplate(t *testing.T) {
	wm := NewWorkflowManager(nil)

	tests := []struct {
		template      string
		originalQuery string
		expected      string
	}{
		{
			template:      "Fix the tests",
			originalQuery: "broken test",
			expected:      "Fix the tests",
		},
		{
			template:      "{original_query}",
			originalQuery: "fix failing tests",
			expected:      "fix failing tests",
		},
		{
			template:      "Analyze {original_query} and report",
			originalQuery: "test failures",
			expected:      "Analyze test failures and report",
		},
		{
			template:      "{original_query} - please fix",
			originalQuery: "broken test",
			expected:      "broken test - please fix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			result := wm.expandTemplate(tt.template, tt.originalQuery)
			if result != tt.expected {
				t.Errorf("expandTemplate() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestWorkflowDefinition_JSONRoundTrip(t *testing.T) {
	workflow := &WorkflowDefinition{
		ID:           "test-workflow-v1",
		Name:         "Test Workflow",
		CategoryPath: "Workflows/Test",
		Description:  "A test workflow",
		Steps: []WorkflowStep{
			{
				Step:          1,
				Actor:         "test.runner",
				Priority:      100,
				QueryTemplate: "Run tests",
				ToolsExpected: []string{"run_repo_tests"},
			},
			{
				Step:          2,
				Actor:         "test.analyzer",
				Priority:      90,
				QueryTemplate: "Analyze results",
			},
		},
		ActorLimits: map[string]int{
			"test.runner": 3,
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("Failed to marshal workflow: %v", err)
	}

	// Unmarshal back
	var decoded WorkflowDefinition
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal workflow: %v", err)
	}

	// Verify key fields
	if decoded.ID != workflow.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, workflow.ID)
	}
	if len(decoded.Steps) != len(workflow.Steps) {
		t.Errorf("Steps length mismatch: got %d, want %d", len(decoded.Steps), len(workflow.Steps))
	}
	if decoded.Steps[0].Actor != workflow.Steps[0].Actor {
		t.Errorf("Step actor mismatch: got %q, want %q", decoded.Steps[0].Actor, workflow.Steps[0].Actor)
	}
}

func TestWorkflowManager_Execute(t *testing.T) {
	wm := NewWorkflowManager(nil)

	workflow := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Steps: []WorkflowStep{
			{
				Step:          1,
				Actor:         "test.runner",
				Priority:      100,
				QueryTemplate: "Run {original_query}",
			},
			{
				Step:          2,
				Actor:         "test.fixer",
				Priority:      90,
				QueryTemplate: "Fix failures",
				Condition: &StepCondition{
					Type:          "tests_still_failing",
					MaxIterations: 3,
				},
			},
		},
		ActorLimits: map[string]int{
			"test.fixer": 3,
		},
	}

	queue := newMockWorkQueue()

	err := wm.Execute(context.Background(), workflow, queue, "tests")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify work items were enqueued
	if len(queue.items) != 2 {
		t.Errorf("Expected 2 work items, got %d", len(queue.items))
	}
	if queue.items[0].Query != "Run tests" {
		t.Errorf("First item query = %q, want %q", queue.items[0].Query, "Run tests")
	}

	// Note: Actor limits can't be set through capabilities.WorkQueue interface
	// They would need to be set at queue construction time
}

// mockWorkQueue is a test implementation of capabilities.WorkQueue interface
type mockWorkQueue struct {
	items       []WorkItem
	actorLimits map[string]int
}

func newMockWorkQueue() *mockWorkQueue {
	return &mockWorkQueue{
		items:       []WorkItem{},
		actorLimits: make(map[string]int),
	}
}

func (m *mockWorkQueue) Enqueue(item interface{}) error {
	if wi, ok := item.(WorkItem); ok {
		m.items = append(m.items, wi)
	}
	return nil
}

func (m *mockWorkQueue) EnqueueConditional(item interface{}) error {
	return m.Enqueue(item)
}

func (m *mockWorkQueue) Depth() int {
	return len(m.items)
}

func (m *mockWorkQueue) IsEmpty() bool {
	return len(m.items) == 0
}

func (m *mockWorkQueue) SetMaxPerActor(actor string, limit int) {
	m.actorLimits[actor] = limit
}
