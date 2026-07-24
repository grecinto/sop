package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sharedcode/sop/ai/memory"
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
		CategoryPath: "Playbooks/Test",
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

func TestWorkflowExecutionContext_RenderStepEventLabel(t *testing.T) {
	ctx := WorkflowExecutionContext{StepNumber: 5, StepTitle: "Review and polish"}
	if got := ctx.RenderStepEventLabel(); got != "#Review and polish" {
		t.Fatalf("expected event label %q, got %q", "#Review and polish", got)
	}

	fallback := WorkflowExecutionContext{StepNumber: 2}
	if got := fallback.RenderStepEventLabel(); got != "#Step 2" {
		t.Fatalf("expected fallback event label %q, got %q", "#Step 2", got)
	}
}

func TestWorkflowManager_ParseWorkflowFromItem_PreservesStepTitle(t *testing.T) {
	wm := NewWorkflowManager(nil)

	item := memory.Item[map[string]any]{
		Data: map[string]any{
			"workflow_name": "Coding",
			"workflow_template": map[string]any{
				"name":        "Coding",
				"description": "Plan, implement, verify, and review",
				"steps": []any{
					map[string]any{
						"step":         5,
						"title":        "Review and polish",
						"instructions": "Review the final diff for clarity and correctness.",
					},
				},
			},
		},
	}

	workflow, err := wm.parseWorkflowFromItem(&item)
	if err != nil {
		t.Fatalf("parseWorkflowFromItem failed: %v", err)
	}
	if len(workflow.Steps) != 1 {
		t.Fatalf("expected 1 workflow step, got %d", len(workflow.Steps))
	}
	if workflow.Steps[0].Title != "Review and polish" {
		t.Fatalf("expected step title %q, got %q", "Review and polish", workflow.Steps[0].Title)
	}
	if workflow.Steps[0].QueryTemplate != "Review and polish" {
		t.Fatalf("expected fallback query template %q, got %q", "Review and polish", workflow.Steps[0].QueryTemplate)
	}
	ctx := WorkflowExecutionContext{StepNumber: 5, StepTitle: workflow.Steps[0].Title}
	if got := ctx.RenderStepStatus(); got != "Step 5: Review and polish" {
		t.Fatalf("expected rendered step status %q, got %q", "Step 5: Review and polish", got)
	}
}

func TestWorkflowManager_ParseWorkflowFromItem_UsesWorkflowTemplatePayload(t *testing.T) {
	wm := NewWorkflowManager(nil)

	item := memory.Item[map[string]any]{
		Data: map[string]any{
			"description":   "Fix failing tests",
			"workflow_name": "Fix failing tests",
			"workflow_template": map[string]any{
				"name":              "Fix failing tests",
				"description":       "Fix failing tests",
				"protocol_guidance": "Inspect the failing output before changing code.",
				"steps": []any{
					map[string]any{
						"step":             1,
						"title":            "Inspect the failing output",
						"instructions":     "Read the test output first.",
						"required_context": []any{"test output", "stack trace"},
						"success_criteria": []any{"Root cause identified"},
						"iteration_policy": "Retry up to 3 times if the failure persists.",
						"tools_expected":   []any{"run_tests", "read_file"},
					},
				},
			},
		},
	}

	workflow, err := wm.parseWorkflowFromItem(&item)
	if err != nil {
		t.Fatalf("parseWorkflowFromItem failed: %v", err)
	}
	if workflow.Name != "Fix failing tests" {
		t.Fatalf("expected workflow name %q, got %q", "Fix failing tests", workflow.Name)
	}
	if workflow.Description != "Fix failing tests" {
		t.Fatalf("expected workflow description %q, got %q", "Fix failing tests", workflow.Description)
	}
	if workflow.ProtocolGuidance != "Inspect the failing output before changing code." {
		t.Fatalf("expected protocol guidance %q, got %q", "Inspect the failing output before changing code.", workflow.ProtocolGuidance)
	}
	if len(workflow.Steps) != 1 {
		t.Fatalf("expected 1 workflow step, got %d", len(workflow.Steps))
	}
	step := workflow.Steps[0]
	if step.QueryTemplate != "Inspect the failing output" {
		t.Fatalf("expected query template %q, got %q", "Inspect the failing output", step.QueryTemplate)
	}
	if step.Instructions != "Read the test output first." {
		t.Fatalf("expected instructions %q, got %q", "Read the test output first.", step.Instructions)
	}
	if len(step.RequiredContext) != 2 {
		t.Fatalf("expected 2 required contexts, got %d", len(step.RequiredContext))
	}
	if len(step.SuccessCriteria) != 1 {
		t.Fatalf("expected 1 success criterion, got %d", len(step.SuccessCriteria))
	}
	if step.IterationPolicy != "Retry up to 3 times if the failure persists." {
		t.Fatalf("expected iteration policy %q, got %q", "Retry up to 3 times if the failure persists.", step.IterationPolicy)
	}
	if len(step.ToolsExpected) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(step.ToolsExpected))
	}
}

func TestWorkflowManager_GetWorkflow_FallsBackToStoredWorkflowTemplateItems(t *testing.T) {
	ctx := context.Background()
	kb, err := createTestKnowledgeBase(ctx)
	if err != nil {
		t.Fatalf("createTestKnowledgeBase failed: %v", err)
	}

	wm := NewWorkflowManager(kb)
	if err := kb.UpsertItems(ctx, []memory.UpsertItemParam[map[string]any]{
		{
			CategoryPath: "Playbooks/TestDebugging",
			Item: &memory.Item[map[string]any]{
				Data: map[string]any{
					"workflow_name": "Fix failing tests",
					"description":   "Fix failing tests in the repository",
					"workflow_template": map[string]any{
						"name":        "Fix failing tests",
						"description": "Fix failing tests in the repository",
						"steps": []any{
							map[string]any{
								"step":  1,
								"title": "Inspect the failing output",
							},
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("UpsertItems failed: %v", err)
	}

	workflowIface, err := wm.GetWorkflow(ctx, "fix failing tests")
	if err != nil {
		t.Fatalf("expected workflow lookup to succeed, got error: %v", err)
	}
	if workflowIface == nil {
		t.Fatal("expected workflow, got nil")
	}
	workflow, ok := workflowIface.(*WorkflowDefinition)
	if !ok {
		t.Fatalf("expected *WorkflowDefinition, got %T", workflowIface)
	}
	if workflow.Name != "Fix failing tests" {
		t.Fatalf("expected workflow name %q, got %q", "Fix failing tests", workflow.Name)
	}
	if len(workflow.Steps) != 1 {
		t.Fatalf("expected 1 workflow step, got %d", len(workflow.Steps))
	}
}

func TestWorkflowManager_Execute_PopulatesWorkflowStepContext(t *testing.T) {
	wm := NewWorkflowManager(nil)

	workflow := &WorkflowDefinition{
		ID:               "test-workflow-context",
		Name:             "Test Debugging",
		Description:      "Fix failing tests",
		ProtocolGuidance: "Always inspect test output before changing code.",
		Steps: []WorkflowStep{{
			Step:            1,
			Actor:           "test.runner",
			Priority:        100,
			QueryTemplate:   "Run {original_query}",
			Instructions:    "Inspect the failing test output before making changes.",
			RequiredContext: []string{"test output", "failing stack trace"},
			SuccessCriteria: []string{"A failing test has been reproduced and explained."},
			IterationPolicy: "Retry up to 3 times if the failure persists.",
			ToolsExpected:   []string{"run_tests", "read_file"},
		}},
	}

	queue := newMockWorkQueue()
	if err := wm.Execute(context.Background(), workflow, queue, "fix failing tests"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(queue.items) != 1 {
		t.Fatalf("Expected 1 work item, got %d", len(queue.items))
	}

	item := queue.items[0]
	payload, ok := item.Context["workflow_step"]
	if !ok {
		t.Fatalf("expected workflow_step context in work item, got %#v", item.Context)
	}

	ctxPayload, ok := payload.(WorkflowExecutionContext)
	if !ok {
		t.Fatalf("expected WorkflowExecutionContext payload, got %T", payload)
	}

	if ctxPayload.StepInstructions != workflow.Steps[0].Instructions {
		t.Fatalf("expected step instructions %q, got %q", workflow.Steps[0].Instructions, ctxPayload.StepInstructions)
	}
	if ctxPayload.WorkflowName != workflow.Name {
		t.Fatalf("expected workflow name %q, got %q", workflow.Name, ctxPayload.WorkflowName)
	}
	if ctxPayload.IterationPolicy != workflow.Steps[0].IterationPolicy {
		t.Fatalf("expected iteration policy %q, got %q", workflow.Steps[0].IterationPolicy, ctxPayload.IterationPolicy)
	}
	if len(ctxPayload.RequiredContext) != 2 {
		t.Fatalf("expected 2 required contexts, got %d", len(ctxPayload.RequiredContext))
	}
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
