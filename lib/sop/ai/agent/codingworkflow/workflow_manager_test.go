package codingworkflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// TestWorkflowManagerBasicFlow demonstrates the basic workflow execution.
func TestWorkflowManagerBasicFlow(t *testing.T) {
	// Create orchestrator with test infrastructure
	orchestrator := &Orchestrator{
		GrammarRegistry: NewGrammarRegistry(),
		// Note: TestRunner and PatchEngine would need to be initialized
		// for a full end-to-end test. This test focuses on the orchestration flow.
	}

	// Create WorkflowManager
	wm := NewWorkflowManager(orchestrator)
	wm.SetDefaultMaxRetry(2)
	wm.SetRetryBackoff(100 * time.Millisecond)

	// Define simple workflow steps (BaseStep just succeeds by default)
	step1 := &BaseStep{}
	step1.SetMetadata(StepMetadata{ID: "step1", Name: "Step 1"})

	step2 := &BaseStep{}
	step2.SetMetadata(StepMetadata{ID: "step2", Name: "Step 2"})

	steps := []Step{step1, step2}

	// Create task
	task := Task{
		WorkingDir:  "/test/repo",
		Description: "Test workflow",
	}

	// Execute workflow through the shared step runner.
	ctx := context.Background()
	result, err := wm.runWorkflow(ctx, steps, task, RunState{})

	// Verify results
	if err != nil {
		t.Errorf("Workflow failed unexpectedly: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected workflow to succeed")
	}

	// Check that steps were executed
	if len(result.StepResults) != len(steps) {
		t.Logf("Expected %d step results, got %d", len(steps), len(result.StepResults))
	}

	// Verify step outcomes
	for stepID, stepResult := range result.StepResults {
		t.Logf("Step %s: %s - %s", stepID, stepResult.Outcome, stepResult.Message)
		if stepResult.Outcome != StepOutcomeSuccess {
			t.Errorf("Expected step %s to succeed, got %s", stepID, stepResult.Outcome)
		}
	}
}

// TestSharedInfrastructureAccess demonstrates accessing SharedInfrastructure from a Step.
func TestSharedInfrastructureAccess(t *testing.T) {
	// Create orchestrator
	orchestrator := &Orchestrator{
		GrammarRegistry: NewGrammarRegistry(),
	}

	// Create SharedInfrastructure
	infra := NewSharedInfrastructure(orchestrator)
	if infra == nil {
		t.Fatal("Failed to create SharedInfrastructure")
	}

	// Create a step and inject infrastructure
	step := &BaseStep{}
	step.SetSharedInfrastructure(infra)

	// Verify infrastructure was injected
	if step.infra != infra {
		t.Error("Infrastructure not properly injected into step")
	}

	// Test AST operations
	astOps := infra.NewASTOperations()
	if astOps == nil {
		t.Error("Failed to create AST operations")
	}

	// Test other operations
	contextOps := infra.NewContextPackOperations()
	if contextOps == nil {
		t.Error("Failed to create context pack operations")
	}

	patchOps := infra.NewPatchOperations()
	if patchOps == nil {
		t.Error("Failed to create patch operations")
	}

	testOps := infra.NewTestOperations()
	if testOps == nil {
		t.Error("Failed to create test operations")
	}

	repoOps := infra.NewRepoOperations()
	if repoOps == nil {
		t.Error("Failed to create repo operations")
	}
}

// TestBaseStepLifecycle demonstrates the Step lifecycle.
func TestBaseStepLifecycle(t *testing.T) {
	// Create a base step
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "test_step",
		Name:        "Test Step",
		Description: "A test step",
	})

	ctx := context.Background()
	state := capabilities.RoutingState{
		Domain:         "testing",
		CapabilityHint: "coding_workflow",
	}

	// 1. OnStart
	err := step.OnStart(ctx, "test query", state)
	if err != nil {
		t.Errorf("OnStart failed: %v", err)
	}

	// 2. OnBeforePrompt
	context, err := step.OnBeforePrompt(ctx, "test query", state)
	if err != nil {
		t.Errorf("OnBeforePrompt failed: %v", err)
	}
	_ = context

	// 3. OnAfterToolResult
	updatedState, shouldContinue, err := step.OnAfterToolResult(ctx, "test_tool", map[string]interface{}{}, state)
	if err != nil {
		t.Errorf("OnAfterToolResult failed: %v", err)
	}
	if !shouldContinue {
		t.Error("Expected step to continue")
	}
	_ = updatedState

	// 4. OnAfterTurn
	shouldContinue, err = step.OnAfterTurn(ctx, 1, state)
	if err != nil {
		t.Errorf("OnAfterTurn failed: %v", err)
	}
	if !shouldContinue {
		t.Error("Expected step to continue")
	}

	// 5. OnFinalize
	result, err := step.OnFinalize(ctx, state)
	if err != nil {
		t.Errorf("OnFinalize failed: %v", err)
	}
	if result.Outcome != StepOutcomeSuccess {
		t.Errorf("Expected success outcome, got %s", result.Outcome)
	}
}

// TestStepStateManagement demonstrates step state management.
func TestStepStateManagement(t *testing.T) {
	step := &BaseStep{}

	// Set state values
	step.SetStepState("key1", "value1")
	step.SetStepState("key2", 42)
	step.SetStepState("key3", []string{"a", "b", "c"})

	// Get state values
	val1, ok := step.GetStepStateValue("key1")
	if !ok || val1 != "value1" {
		t.Errorf("Expected 'value1', got %v", val1)
	}

	val2, ok := step.GetStepStateValue("key2")
	if !ok || val2 != 42 {
		t.Errorf("Expected 42, got %v", val2)
	}

	val3, ok := step.GetStepStateValue("key3")
	if !ok {
		t.Error("Expected to find key3")
	}
	slice, ok := val3.([]string)
	if !ok || len(slice) != 3 {
		t.Errorf("Expected []string with 3 elements, got %v", val3)
	}

	// Get entire state
	state := step.GetStepState()
	if len(state) != 3 {
		t.Errorf("Expected 3 state entries, got %d", len(state))
	}
}

// TestStepRetryLogic demonstrates retry behavior.
func TestStepRetryLogic(t *testing.T) {
	orchestrator := &Orchestrator{
		GrammarRegistry: NewGrammarRegistry(),
	}

	wm := NewWorkflowManager(orchestrator)
	wm.SetDefaultMaxRetry(3)
	wm.SetRetryBackoff(10 * time.Millisecond)

	// Create a step that always fails
	failingStep := &failingTestStep{}
	failingStep.SetMetadata(StepMetadata{
		ID:         "failing_step",
		Name:       "Failing Step",
		MaxRetries: 2,
	})

	task := Task{Description: "Test retry"}
	ctx := context.Background()

	// Execute with retries through the shared step runner.
	startTime := time.Now()
	steps := []Step{failingStep}
	result, err := wm.runWorkflow(ctx, steps, task, RunState{})

	elapsed := time.Since(startTime)

	// Should have failed after retries
	if err == nil {
		t.Error("Expected error from failing step")
	} else {
		t.Logf("Got expected error: %v", err)
	}

	if result.Success {
		t.Error("Expected workflow to fail")
	}

	// Should have taken time for retries with backoff
	// With 2 retries: 0ms + 10ms + 20ms = 30ms minimum
	if elapsed < 25*time.Millisecond {
		t.Errorf("Expected retry backoff, elapsed: %v", elapsed)
	}

	t.Logf("Retry test completed in %v", elapsed)
}

func TestWorkflowManagerExecuteIsDisabled(t *testing.T) {
	wm := NewWorkflowManager(&Orchestrator{})
	_, err := wm.Execute(context.Background(), nil, Task{}, RunState{})
	if err == nil {
		t.Fatal("expected disabled controller-style execution to return an error")
	}
	if !strings.Contains(err.Error(), "controller-style workflow execution is disabled") {
		t.Fatalf("expected disabled guidance in error, got %v", err)
	}
}

// TestTerminalStep demonstrates terminal step behavior.
func TestTerminalStep(t *testing.T) {
	orchestrator := &Orchestrator{
		GrammarRegistry: NewGrammarRegistry(),
	}

	wm := NewWorkflowManager(orchestrator)

	// Create steps, with second one being terminal
	step1 := &BaseStep{}
	step1.SetMetadata(StepMetadata{ID: "step1", Name: "Step 1"})

	step2 := &BaseStep{}
	step2.SetMetadata(StepMetadata{ID: "step2", Name: "Step 2", IsTerminal: true})

	step3 := &BaseStep{}
	step3.SetMetadata(StepMetadata{ID: "step3", Name: "Step 3"})

	steps := []Step{step1, step2, step3}

	task := Task{Description: "Test terminal"}
	ctx := context.Background()

	result, err := wm.runWorkflow(ctx, steps, task, RunState{})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Expected workflow to succeed")
	}

	// Should have executed only first 2 steps (terminal stops workflow)
	if len(result.CompletedSteps) != 2 {
		t.Errorf("Expected 2 completed steps, got %d", len(result.CompletedSteps))
	}

	// Should NOT have result for step3
	if _, exists := result.StepResults["step3"]; exists {
		t.Error("Step 3 should not have been executed")
	}

	t.Logf("Terminal step test: %s", result.Message)
}

// Helper types for testing

// failingTestStep always returns a failure outcome
type failingTestStep struct {
	BaseStep
}

func (s *failingTestStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	failErr := fmt.Errorf("intentional failure for testing")
	return StepResult{
		Outcome: StepOutcomeFailed,
		Message: "Intentional failure for testing",
		Error:   failErr,
	}, failErr
}
