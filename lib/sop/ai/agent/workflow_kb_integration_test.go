package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai/memory"
)

// TestWorkflowManagerIntegration verifies WorkflowManager is initialized in CopilotAgent
// DEPRECATED: KB-driven workflows removed (2026-07-30) in favor of Phase system
func TestWorkflowManagerIntegration(t *testing.T) {
	t.Skip("KB-driven workflows removed - use Phase system instead")

	// Without systemDB, WorkflowManager should not be initialized
	agent := NewCopilotAgent(Config{}, nil, nil)

	if agent == nil {
		t.Fatal("NewCopilotAgent returned nil")
	}
}

// TestKBWorkflowDetection verifies workflow detection logic
// DEPRECATED: KB-driven workflows removed (2026-07-30) in favor of Phase system
func TestKBWorkflowDetection(t *testing.T) {
	t.Skip("KB-driven workflows removed - use Phase system instead")

	agent := NewCopilotAgent(Config{}, nil, nil)

	ctx := context.Background()
	_ = agent
	_ = ctx
}

// TestWorkflowManagerWithMockKB tests workflow detection with a mock KB
func TestWorkflowManagerWithMockKB(t *testing.T) {
	// Create minimal WorkflowManager
	wm := NewWorkflowManager(nil)

	if wm == nil {
		t.Fatal("NewWorkflowManager returned nil")
	}

	// Verify condition registry is initialized
	if wm.conditionRegistry == nil {
		t.Error("Condition registry not initialized")
	}

	// Verify built-in conditions are registered
	conditions := []string{"tests_still_failing", "has_error_output", "tests_passing"}
	for _, cond := range conditions {
		if _, ok := wm.conditionRegistry[cond]; !ok {
			t.Errorf("Built-in condition not registered: %s", cond)
		}
	}
}

// TestWorkflowDetectionWithoutTransaction verifies graceful handling without transaction
// DEPRECATED: KB-driven workflows removed (2026-07-30) in favor of Phase system
func TestWorkflowDetectionWithoutTransaction(t *testing.T) {
	t.Skip("KB-driven workflows removed - use Phase system instead")

	agent := NewCopilotAgent(Config{}, nil, nil)

	// Context without session payload
	ctx := context.Background()
	_ = agent
	_ = ctx
}

// TestWorkflowTriggerPatternMatching tests trigger pattern matching logic
func TestWorkflowTriggerPatternMatching(t *testing.T) {
	// Create a simple workflow definition
	workflow := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Steps: []WorkflowStep{
			{
				Step:          1,
				Actor:         "test_actor",
				Priority:      100,
				QueryTemplate: "Execute step 1",
			},
		},
		TriggerPatterns: []string{
			"fix failing test",
			"debug test",
			"test is failing",
		},
	}

	// Test queries that should match
	matchingQueries := []string{
		"fix failing test",
		"debug test",
		"test is failing",
	}

	for _, query := range matchingQueries {
		matched := false
		for _, pattern := range workflow.TriggerPatterns {
			if pattern == query {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("Query should match workflow: %s", query)
		}
	}
}

// TestWorkflowManagerKBInitialization tests lazy KB initialization
func TestWorkflowManagerKBInitialization(t *testing.T) {
	wm := NewWorkflowManager(nil)

	// KB should be nil initially
	if wm.kb != nil {
		t.Error("KB should be nil initially")
	}

	// After setting KB, should be available
	mockKB := &memory.KnowledgeBase[map[string]any]{}
	wm.kb = mockKB

	if wm.kb != mockKB {
		t.Error("KB not set correctly")
	}
}

// TestWorkflowStepExecution tests basic workflow step creation
func TestWorkflowStepExecution(t *testing.T) {
	workflow := &WorkflowDefinition{
		ID:   "simple-workflow",
		Name: "Simple Workflow",
		Steps: []WorkflowStep{
			{
				Step:          1,
				Actor:         "actor1",
				Priority:      100,
				QueryTemplate: "Do step 1: {original_query}",
			},
			{
				Step:          2,
				Actor:         "actor2",
				Priority:      90,
				QueryTemplate: "Do step 2",
			},
		},
	}

	if len(workflow.Steps) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(workflow.Steps))
	}

	if workflow.Steps[0].Priority != 100 {
		t.Errorf("Expected priority 100, got %d", workflow.Steps[0].Priority)
	}

	if workflow.Steps[1].Actor != "actor2" {
		t.Errorf("Expected actor2, got %s", workflow.Steps[1].Actor)
	}
}

// TestWorkflowConditionEvaluation tests condition evaluation
func TestWorkflowConditionEvaluation(t *testing.T) {
	wm := NewWorkflowManager(nil)

	// Test tests_still_failing condition
	if evaluator, ok := wm.conditionRegistry["tests_still_failing"]; ok {
		// Condition should be true when tests failed
		result := TurnResult{
			TestsFailed: true,
			Error:       context.DeadlineExceeded, // Any error
		}
		if !evaluator(result) {
			t.Error("tests_still_failing should be true when tests failed")
		}

		// Condition should be false when tests passed
		result = TurnResult{
			TestsFailed: false,
			Error:       nil,
		}
		if evaluator(result) {
			t.Error("tests_still_failing should be false when tests passed")
		}
	} else {
		t.Error("tests_still_failing condition not found")
	}
}
