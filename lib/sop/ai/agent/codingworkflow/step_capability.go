package codingworkflow

import (
	"context"
	"fmt"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// BaseStep provides a default implementation of the Step interface.
// Concrete steps can embed BaseStep and override only the methods they need.
//
// Example:
//
//	type RunTestsStep struct {
//	    BaseStep
//	    testFile string
//	}
//
//	func (s *RunTestsStep) Metadata() StepMetadata {
//	    return StepMetadata{
//	        ID: "run_tests",
//	        Name: "Run Tests",
//	        Description: "Execute repository tests",
//	    }
//	}
//
//	func (s *RunTestsStep) OnAfterToolResult(ctx, toolName, result, state) {
//	    // Custom logic using SharedInfrastructure
//	    testOps := s.infra.NewTestOperations()
//	    // ...
//	    return state, true, nil
//	}
type BaseStep struct {
	metadata      StepMetadata
	infra         *SharedInfrastructure  // Injected by WorkflowManager
	workflowState *WorkflowState         // Injected by WorkflowManager
	stepState     map[string]interface{} // Step-specific state
	continueSteps bool                   // Whether to continue after current turn
}

// Metadata returns the step metadata.
// Subclasses should override this to provide their own metadata.
func (b *BaseStep) Metadata() StepMetadata {
	return b.metadata
}

// SetMetadata sets the step metadata.
func (b *BaseStep) SetMetadata(metadata StepMetadata) {
	b.metadata = metadata
}

// SetSharedInfrastructure injects the shared infrastructure.
func (b *BaseStep) SetSharedInfrastructure(infra *SharedInfrastructure) {
	b.infra = infra
}

// SetWorkflowState injects the workflow state for cross-step data access.
func (b *BaseStep) SetWorkflowState(state *WorkflowState) {
	b.workflowState = state
}

// GetWorkflowState retrieves the workflow state.
func (b *BaseStep) GetWorkflowState() *WorkflowState {
	return b.workflowState
}

// GetSignals returns signals for pre-step research.
// Default implementation returns no signals.
// Subclasses can override to emit signals based on runtime conditions.
func (b *BaseStep) GetSignals(ctx context.Context, workflowState *WorkflowState) ([]StepSignal, error) {
	return nil, nil
}

// OnStart initializes the step.
// Default implementation creates empty step state.
func (b *BaseStep) OnStart(ctx context.Context, query string, state capabilities.RoutingState) error {
	b.stepState = make(map[string]interface{})
	b.continueSteps = true
	return nil
}

// OnBeforePrompt returns additional context for the prompt.
// Default implementation returns empty string.
func (b *BaseStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return "", nil
}

// OnAfterToolResult handles tool execution results.
// Default implementation does nothing, just returns state unchanged.
func (b *BaseStep) OnAfterToolResult(
	ctx context.Context,
	toolName string,
	toolResult interface{},
	state capabilities.RoutingState,
) (capabilities.RoutingState, bool, error) {
	return state, b.continueSteps, nil
}

// OnAfterTurn decides whether to continue the step.
// Default implementation returns true (continue).
func (b *BaseStep) OnAfterTurn(ctx context.Context, turnNumber int, state capabilities.RoutingState) (bool, error) {
	return b.continueSteps, nil
}

// OnFinalize returns the final step result.
// Default implementation returns success.
// Subclasses should override this to provide meaningful results.
func (b *BaseStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Step completed",
		Data:    b.stepState,
	}, nil
}

// GetStepState returns the current step state.
func (b *BaseStep) GetStepState() map[string]interface{} {
	return b.stepState
}

// SetStepState sets a value in step state.
func (b *BaseStep) SetStepState(key string, value interface{}) {
	if b.stepState == nil {
		b.stepState = make(map[string]interface{})
	}
	b.stepState[key] = value
}

// GetStepStateValue gets a value from step state.
func (b *BaseStep) GetStepStateValue(key string) (interface{}, bool) {
	if b.stepState == nil {
		return nil, false
	}
	val, ok := b.stepState[key]
	return val, ok
}

// StopContinuation signals that the step should stop after current turn.
func (b *BaseStep) StopContinuation() {
	b.continueSteps = false
}

// Example concrete steps for test fix workflow

// RunTestsStep executes tests and captures output.
type RunTestsStep struct {
	BaseStep
	workingDir string
	testArgs   []string
}

// NewRunTestsStep creates a new RunTestsStep.
func NewRunTestsStep(workingDir string, testArgs []string) *RunTestsStep {
	step := &RunTestsStep{
		workingDir: workingDir,
		testArgs:   testArgs,
	}
	step.SetMetadata(StepMetadata{
		ID:          "run_tests",
		Name:        "Run Tests",
		Description: "Execute repository tests and capture failures",
		MaxRetries:  0, // Don't retry test execution
		Tags:        []string{"testing", "validation"},
	})
	return step
}

// OnBeforePrompt injects test execution context.
func (s *RunTestsStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return fmt.Sprintf("Execute tests in %s with verbose output to capture all failures.", s.workingDir), nil
}

// OnAfterToolResult processes test execution results.
func (s *RunTestsStep) OnAfterToolResult(
	ctx context.Context,
	toolName string,
	toolResult interface{},
	state capabilities.RoutingState,
) (capabilities.RoutingState, bool, error) {
	// Check infrastructure is available
	if s.infra == nil {
		return state, false, fmt.Errorf("shared infrastructure not initialized")
	}

	// Use TestOperations to run tests
	testOps := s.infra.NewTestOperations()
	task := Task{
		WorkingDir:  s.workingDir,
		Description: "Run tests",
	}
	runState := RunState{
		Args: map[string]string{
			"test_args": fmt.Sprintf("%v", s.testArgs),
		},
	}

	testResult, err := testOps.RunTests(task, runState)
	if err != nil {
		s.SetStepState("error", err.Error())
		return state, false, err
	}

	// Store results in step state
	s.SetStepState("test_passed", testResult.Passed)
	s.SetStepState("test_output", testResult.Output)

	// Stop continuation if tests passed (no fixes needed)
	if testResult.Passed {
		s.StopContinuation()
	}

	return state, s.continueSteps, nil
}

// OnFinalize returns test results.
func (s *RunTestsStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	passed, _ := s.GetStepStateValue("test_passed")
	output, _ := s.GetStepStateValue("test_output")

	if passed == true {
		return StepResult{
			Outcome: StepOutcomeSuccess,
			Message: "All tests passed",
			Data: map[string]interface{}{
				"test_output": output,
				"test_passed": true,
			},
		}, nil
	}

	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Tests failed, continuing to analysis",
		Data: map[string]interface{}{
			"test_output": output,
			"test_passed": false,
		},
	}, nil
}

// AnalyzeErrorsStep analyzes test failures using AST.
type AnalyzeErrorsStep struct {
	BaseStep
}

// NewAnalyzeErrorsStep creates a new AnalyzeErrorsStep.
func NewAnalyzeErrorsStep() *AnalyzeErrorsStep {
	step := &AnalyzeErrorsStep{}
	step.SetMetadata(StepMetadata{
		ID:          "analyze_errors",
		Name:        "Analyze Errors",
		Description: "Analyze test failures using AST and tree-sitter",
		MaxRetries:  1,
		Tags:        []string{"analysis", "ast"},
	})
	return step
}

// OnBeforePrompt injects analysis context.
func (s *AnalyzeErrorsStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return "Analyze test failures using AST analysis to identify root causes and affected code.", nil
}

// OnAfterToolResult performs AST-based error analysis.
func (s *AnalyzeErrorsStep) OnAfterToolResult(
	ctx context.Context,
	toolName string,
	toolResult interface{},
	state capabilities.RoutingState,
) (capabilities.RoutingState, bool, error) {
	// Check infrastructure is available
	if s.infra == nil {
		return state, false, fmt.Errorf("shared infrastructure not initialized")
	}

	// Use AST operations to analyze errors
	astOps := s.infra.NewASTOperations()
	_ = astOps // Use AST operations for analysis

	// TODO: Implement actual error analysis logic
	// This would:
	// 1. Parse error messages from test output
	// 2. Use AST to locate failing code
	// 3. Use tree-sitter to understand code structure
	// 4. Identify root cause and generate fix suggestions

	s.SetStepState("analysis_complete", true)
	s.StopContinuation()

	return state, false, nil
}

// OnFinalize returns analysis results.
func (s *AnalyzeErrorsStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Error analysis complete",
		Data:    s.GetStepState(),
	}, nil
}

// ApplyFixStep applies a patch to fix identified issues.
type ApplyFixStep struct {
	BaseStep
	patch *Patch
}

// NewApplyFixStep creates a new ApplyFixStep.
func NewApplyFixStep(patch *Patch) *ApplyFixStep {
	step := &ApplyFixStep{
		patch: patch,
	}
	step.SetMetadata(StepMetadata{
		ID:          "apply_fix",
		Name:        "Apply Fix",
		Description: "Apply patch to fix identified issues",
		MaxRetries:  0, // Don't retry patch application
		Tags:        []string{"patching", "modification"},
	})
	return step
}

// OnBeforePrompt injects patching context.
func (s *ApplyFixStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return "Apply the generated patch to fix the identified issues.", nil
}

// OnAfterToolResult applies the patch using PatchEngine.
func (s *ApplyFixStep) OnAfterToolResult(
	ctx context.Context,
	toolName string,
	toolResult interface{},
	state capabilities.RoutingState,
) (capabilities.RoutingState, bool, error) {
	// Check infrastructure is available
	if s.infra == nil {
		return state, false, fmt.Errorf("shared infrastructure not initialized")
	}

	// Use PatchOperations to apply patch
	patchOps := s.infra.NewPatchOperations()
	task := Task{
		Description: "Apply fix",
		Patch:       s.patch,
	}
	runState := RunState{}

	err := patchOps.ApplyPatch(task, s.patch, runState)
	if err != nil {
		s.SetStepState("error", err.Error())
		return state, false, err
	}

	s.SetStepState("patch_applied", true)
	s.StopContinuation()

	return state, false, nil
}

// OnFinalize returns patch application results.
func (s *ApplyFixStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	applied, _ := s.GetStepStateValue("patch_applied")

	if applied == true {
		return StepResult{
			Outcome: StepOutcomeSuccess,
			Message: "Patch applied successfully",
			Data:    s.GetStepState(),
		}, nil
	}

	return StepResult{
		Outcome: StepOutcomeFailed,
		Message: "Failed to apply patch",
		Error:   fmt.Errorf("patch application failed"),
		Data:    s.GetStepState(),
	}, fmt.Errorf("patch application failed")
}

// VerifyFixStep re-runs tests to verify the fix.
type VerifyFixStep struct {
	BaseStep
	workingDir string
}

// NewVerifyFixStep creates a new VerifyFixStep.
func NewVerifyFixStep(workingDir string) *VerifyFixStep {
	step := &VerifyFixStep{
		workingDir: workingDir,
	}
	step.SetMetadata(StepMetadata{
		ID:          "verify_fix",
		Name:        "Verify Fix",
		Description: "Re-run tests to verify the fix works",
		IsTerminal:  true, // This is a terminal step
		MaxRetries:  0,
		Tags:        []string{"testing", "verification"},
	})
	return step
}

// OnBeforePrompt injects verification context.
func (s *VerifyFixStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return "Re-run tests to verify the fix resolved the failures.", nil
}

// OnAfterToolResult runs tests to verify fix.
func (s *VerifyFixStep) OnAfterToolResult(
	ctx context.Context,
	toolName string,
	toolResult interface{},
	state capabilities.RoutingState,
) (capabilities.RoutingState, bool, error) {
	// Check infrastructure is available
	if s.infra == nil {
		return state, false, fmt.Errorf("shared infrastructure not initialized")
	}

	// Use TestOperations to run tests
	testOps := s.infra.NewTestOperations()
	task := Task{
		WorkingDir:  s.workingDir,
		Description: "Verify fix",
	}
	runState := RunState{}

	testResult, err := testOps.RunTests(task, runState)
	if err != nil {
		s.SetStepState("error", err.Error())
		return state, false, err
	}

	// Store results
	s.SetStepState("test_passed", testResult.Passed)
	s.SetStepState("test_output", testResult.Output)
	s.StopContinuation()

	return state, false, nil
}

// OnFinalize returns verification results.
func (s *VerifyFixStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	passed, _ := s.GetStepStateValue("test_passed")
	output, _ := s.GetStepStateValue("test_output")

	if passed == true {
		return StepResult{
			Outcome: StepOutcomeSuccess,
			Message: "Fix verified: all tests pass",
			Data: map[string]interface{}{
				"test_output": output,
				"test_passed": true,
			},
		}, nil
	}

	return StepResult{
		Outcome: StepOutcomeFailed,
		Message: "Fix verification failed: tests still failing",
		Error:   fmt.Errorf("tests still failing after fix"),
		Data: map[string]interface{}{
			"test_output": output,
			"test_passed": false,
		},
	}, fmt.Errorf("tests still failing after fix")
}
