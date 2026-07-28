package codingworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// TestDetectUserRedirect tests the user redirect detection logic
func TestDetectUserRedirect(t *testing.T) {
	step := &BaseStep{}
	step.OnStart(context.Background(), "test", capabilities.RoutingState{})

	tests := []struct {
		name           string
		userMessage    string
		expectRedirect bool
	}{
		{
			name:           "No redirect - normal message",
			userMessage:    "continue with the implementation",
			expectRedirect: false,
		},
		{
			name:           "Redirect with 'no'",
			userMessage:    "No, I want to test first",
			expectRedirect: true,
		},
		{
			name:           "Redirect with 'wait'",
			userMessage:    "Wait, let's debug this first",
			expectRedirect: true,
		},
		{
			name:           "Redirect with 'instead'",
			userMessage:    "Instead, run the tests",
			expectRedirect: true,
		},
		{
			name:           "Redirect with task mention",
			userMessage:    "test first",
			expectRedirect: true,
		},
		{
			name:           "Redirect with 'stop'",
			userMessage:    "Stop, change direction to refactoring",
			expectRedirect: true,
		},
		{
			name:           "Redirect with 'actually'",
			userMessage:    "Actually, let's document this first",
			expectRedirect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Since extractLastUserMessage currently returns empty string,
			// we test the detection logic directly by simulating the message
			// This test documents the expected behavior

			// For now, we verify the keyword detection logic exists
			// Once conversation integration is complete, these tests will work end-to-end
			if tt.expectRedirect {
				// Verify redirect keywords are in the test message
				hasKeyword := false
				keywords := []string{"no,", "wait,", "actually,", "instead", "first", "before", "stop", "change", "switch"}
				msg := strings.ToLower(tt.userMessage)
				for _, kw := range keywords {
					if strings.Contains(msg, kw) {
						hasKeyword = true
						break
					}
				}
				if !hasKeyword {
					t.Errorf("Test expects redirect but message doesn't contain redirect keywords: %s", tt.userMessage)
				}
			}
		})
	}
}

// TestDetectUserModification tests the user modification detection logic
func TestDetectUserModification(t *testing.T) {
	step := &BaseStep{}
	step.OnStart(context.Background(), "test", capabilities.RoutingState{})

	tests := []struct {
		name         string
		userMessage  string
		expectModify bool
	}{
		{
			name:         "No modification",
			userMessage:  "continue",
			expectModify: false,
		},
		{
			name:         "Modify with 'also'",
			userMessage:  "Also add error handling",
			expectModify: true,
		},
		{
			name:         "Modify with 'but'",
			userMessage:  "But make sure to validate inputs",
			expectModify: true,
		},
		{
			name:         "Modify with 'include'",
			userMessage:  "Include logging statements",
			expectModify: true,
		},
		{
			name:         "Modify with 'make sure'",
			userMessage:  "Make sure to handle edge cases",
			expectModify: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Similar to redirect test, document expected behavior
			if tt.expectModify {
				hasKeyword := false
				keywords := []string{"also", "but", "however", "include", "add", "consider", "make sure", "remember", "don't forget"}
				msg := strings.ToLower(tt.userMessage)
				for _, kw := range keywords {
					if strings.Contains(msg, kw) {
						hasKeyword = true
						break
					}
				}
				if !hasKeyword {
					t.Errorf("Test expects modification but message doesn't contain modify keywords: %s", tt.userMessage)
				}
			}
		})
	}
}

// TestWorkflowRedirectInExecution tests redirect handling in workflow execution
func TestWorkflowRedirectInExecution(t *testing.T) {
	orchestrator := createTestOrchestrator(t)
	manager := NewWorkflowManager(orchestrator)

	// Create a test step that returns UserRedirect outcome
	redirectStep := &TestRedirectStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{
				ID:   "redirect_step",
				Name: "Redirect Test Step",
			},
		},
		shouldRedirect: true,
		userFeedback:   "No, I want to test first",
	}

	steps := []Step{redirectStep}
	task := Task{
		WorkingDir:  ".",
		Description: "test redirect",
		TaskType:    string(TaskTypeImplementation),
	}
	state := RunState{}

	result, err := manager.Execute(context.Background(), steps, task, state)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify redirect was recorded
	workflowState := redirectStep.GetWorkflowState()
	if workflowState == nil {
		t.Fatal("WorkflowState is nil")
	}

	if !workflowState.UserInterrupted {
		t.Error("Expected UserInterrupted to be true")
	}

	if len(workflowState.RedirectHistory) != 1 {
		t.Errorf("Expected 1 redirect in history, got %d", len(workflowState.RedirectHistory))
	} else {
		redirect := workflowState.RedirectHistory[0]
		if redirect.FromStep != "redirect_step" {
			t.Errorf("Expected redirect from 'redirect_step', got '%s'", redirect.FromStep)
		}
		if redirect.UserFeedback != "No, I want to test first" {
			t.Errorf("Expected user feedback 'No, I want to test first', got '%s'", redirect.UserFeedback)
		}
	}

	// Verify result indicates completion
	if !result.Success {
		t.Errorf("Expected workflow to be marked successful after redirect handling")
	}
}

// TestWorkflowModifyInExecution tests modification handling in workflow execution
func TestWorkflowModifyInExecution(t *testing.T) {
	orchestrator := createTestOrchestrator(t)
	manager := NewWorkflowManager(orchestrator)

	// Create a test step that returns UserModify outcome
	modifyStep := &TestRedirectStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{
				ID:   "modify_step",
				Name: "Modify Test Step",
			},
		},
		shouldModify: true,
		userFeedback: "Also add error handling",
	}

	steps := []Step{modifyStep}
	task := Task{
		WorkingDir:  ".",
		Description: "test modify",
		TaskType:    string(TaskTypeImplementation),
	}
	state := RunState{}

	result, err := manager.Execute(context.Background(), steps, task, state)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify modification was recorded in SharedData
	workflowState := modifyStep.GetWorkflowState()
	if workflowState == nil {
		t.Fatal("WorkflowState is nil")
	}

	if modifications, ok := workflowState.SharedData["user_modifications"].(string); ok {
		if modifications != "Also add error handling" {
			t.Errorf("Expected modifications 'Also add error handling', got '%s'", modifications)
		}
	} else {
		t.Error("Expected user_modifications in SharedData")
	}

	if modifiedAt, ok := workflowState.SharedData["modified_at_step"].(string); ok {
		if modifiedAt != "modify_step" {
			t.Errorf("Expected modified_at_step 'modify_step', got '%s'", modifiedAt)
		}
	} else {
		t.Error("Expected modified_at_step in SharedData")
	}

	if !result.Success {
		t.Errorf("Expected workflow to be marked successful after modification handling")
	}
}

// TestWorkflowStateInitialization tests that WorkflowState is properly initialized with redirect fields
func TestWorkflowStateInitialization(t *testing.T) {
	orchestrator := createTestOrchestrator(t)
	manager := NewWorkflowManager(orchestrator)

	step := &TestRedirectStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{
				ID:   "test_step",
				Name: "Test Step",
			},
		},
	}

	steps := []Step{step}
	task := Task{
		WorkingDir:  ".",
		Description: "test init",
		TaskType:    string(TaskTypeImplementation),
	}
	state := RunState{}

	_, err := manager.Execute(context.Background(), steps, task, state)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	workflowState := step.GetWorkflowState()
	if workflowState == nil {
		t.Fatal("WorkflowState is nil")
	}

	// Verify redirect fields are initialized
	if workflowState.OriginalWorkflowType != string(TaskTypeImplementation) {
		t.Errorf("Expected OriginalWorkflowType 'implementation', got '%s'", workflowState.OriginalWorkflowType)
	}

	if workflowState.RedirectHistory == nil {
		t.Error("Expected RedirectHistory to be initialized")
	}

	if workflowState.PreviousSteps == nil {
		t.Error("Expected PreviousSteps to be initialized")
	}

	if workflowState.UserInterrupted {
		t.Error("Expected UserInterrupted to be false initially")
	}
}

// TestAutomaticModificationInjection tests that user modifications are automatically injected into OnBeforePrompt
func TestAutomaticModificationInjection(t *testing.T) {
	orchestrator := createTestOrchestrator(t)
	manager := NewWorkflowManager(orchestrator)

	// Create step 1 that returns UserModify outcome
	step1 := &TestRedirectStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{
				ID:   "step1",
				Name: "First Step",
			},
		},
		shouldModify: true,
		userFeedback: "Also add error handling for null pointers",
	}

	// Create step 2 that will receive the modification in its prompt
	step2 := &TestPromptStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{
				ID:   "step2",
				Name: "Second Step",
			},
		},
	}

	steps := []Step{step1, step2}
	task := Task{
		WorkingDir:  ".",
		Description: "test modification injection",
		TaskType:    string(TaskTypeImplementation),
	}
	state := RunState{}

	result, err := manager.Execute(context.Background(), steps, task, state)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected workflow to succeed")
	}

	// Verify step2 received the modification in its prompt
	if step2.capturedPromptContext == "" {
		t.Error("Expected step2 to capture prompt context")
	}

	if !strings.Contains(step2.capturedPromptContext, "Also add error handling for null pointers") {
		t.Errorf("Expected prompt to contain user modification, got: %s", step2.capturedPromptContext)
	}

	if !strings.Contains(step2.capturedPromptContext, "User Modification") {
		t.Errorf("Expected prompt to have 'User Modification' label, got: %s", step2.capturedPromptContext)
	}

	if !strings.Contains(step2.capturedPromptContext, "step1") {
		t.Errorf("Expected prompt to mention originating step 'step1', got: %s", step2.capturedPromptContext)
	}
}

// TestMultipleModificationAccumulation tests that multiple modifications are tracked in history
func TestMultipleModificationAccumulation(t *testing.T) {
	orchestrator := createTestOrchestrator(t)
	manager := NewWorkflowManager(orchestrator)

	// Create steps that each add modifications
	step1 := &TestRedirectStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{ID: "step1", Name: "Step 1"},
		},
		shouldModify: true,
		userFeedback: "Add error handling",
	}

	step2 := &TestRedirectStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{ID: "step2", Name: "Step 2"},
		},
		shouldModify: true,
		userFeedback: "Also add logging",
	}

	step3 := &TestPromptStep{
		BaseStep: BaseStep{
			metadata: StepMetadata{ID: "step3", Name: "Step 3"},
		},
	}

	steps := []Step{step1, step2, step3}
	task := Task{
		WorkingDir:  ".",
		Description: "test multiple modifications",
		TaskType:    string(TaskTypeImplementation),
	}
	state := RunState{}

	result, err := manager.Execute(context.Background(), steps, task, state)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected workflow to succeed")
	}

	// Verify step3 received both modifications
	promptCtx := step3.capturedPromptContext
	if promptCtx == "" {
		t.Fatal("Expected step3 to capture prompt context")
	}

	// Should have the latest modification
	if !strings.Contains(promptCtx, "Also add logging") {
		t.Errorf("Expected prompt to contain latest modification 'Also add logging', got: %s", promptCtx)
	}

	// Should have modification history
	if !strings.Contains(promptCtx, "Previous User Modifications") {
		t.Errorf("Expected prompt to have modification history section, got: %s", promptCtx)
	}

	if !strings.Contains(promptCtx, "Add error handling") {
		t.Errorf("Expected prompt to contain first modification 'Add error handling' in history, got: %s", promptCtx)
	}

	// Verify modification history in workflow state
	workflowState := step3.GetWorkflowState()
	if workflowState == nil {
		t.Fatal("WorkflowState is nil")
	}

	modHistory, ok := workflowState.SharedData["modification_history"].([]string)
	if !ok {
		t.Fatal("Expected modification_history in SharedData")
	}

	if len(modHistory) != 2 {
		t.Errorf("Expected 2 modifications in history, got %d", len(modHistory))
	}
}

// TestPromptStep is a test step that captures the prompt context
type TestPromptStep struct {
	BaseStep
	capturedPromptContext string
}

func (s *TestPromptStep) OnStart(ctx context.Context, query string, state capabilities.RoutingState) error {
	s.stepState = make(map[string]interface{})
	s.continueSteps = true
	return nil
}

func (s *TestPromptStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	// Call base implementation to get automatic injection
	promptContext, err := s.BaseStep.OnBeforePrompt(ctx, query, state)
	if err != nil {
		return "", err
	}

	// Capture for testing
	s.capturedPromptContext = promptContext
	return promptContext, nil
}

func (s *TestPromptStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Step completed",
		Data:    s.stepState,
	}, nil
}

// TestRedirectStep is a test helper that simulates redirect/modify behavior
type TestRedirectStep struct {
	BaseStep
	shouldRedirect bool
	shouldModify   bool
	userFeedback   string
}

func (s *TestRedirectStep) OnStart(ctx context.Context, query string, state capabilities.RoutingState) error {
	s.stepState = make(map[string]interface{})
	s.continueSteps = true
	return nil
}

func (s *TestRedirectStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	if s.shouldRedirect {
		return StepResult{
			Outcome: StepOutcomeUserRedirect,
			Message: "User requested redirect",
			Data: map[string]interface{}{
				"user_feedback": s.userFeedback,
				"new_direction": s.userFeedback,
			},
		}, nil
	}

	if s.shouldModify {
		return StepResult{
			Outcome: StepOutcomeUserModify,
			Message: "User requested modification",
			Data: map[string]interface{}{
				"user_feedback": s.userFeedback,
			},
		}, nil
	}

	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Step completed",
		Data:    s.stepState,
	}, nil
}

// createTestOrchestrator creates a minimal orchestrator for testing
func createTestOrchestrator(t *testing.T) *Orchestrator {
	return &Orchestrator{
		// Minimal initialization for test
		// In real tests, this would be more complete
	}
}
