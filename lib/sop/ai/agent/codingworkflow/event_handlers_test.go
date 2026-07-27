package codingworkflow

import (
	"context"
	"testing"
)

// TestEventHandlerRegistration verifies handler registration
func TestEventHandlerRegistration(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)

	// Register global handler
	handler := &BestPracticesHandler{Languages: []string{"go"}}
	wm.RegisterEventHandler(handler)

	if len(wm.eventHandlers) != 1 {
		t.Errorf("Expected 1 global handler, got %d", len(wm.eventHandlers))
	}

	// Register step-specific handler
	stepHandler := &CodePatternHandler{}
	wm.RegisterStepEventHandler("test_step", stepHandler)

	if len(wm.stepHandlers["test_step"]) != 1 {
		t.Errorf("Expected 1 step handler for test_step, got %d", len(wm.stepHandlers["test_step"]))
	}
}

// TestEventEmission verifies event emission and handler invocation
func TestEventEmission(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)

	// Create test step with code_generation tag
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:   "code_gen",
		Name: "Code Generation",
		Tags: []string{"code_generation"},
	})

	// Register handler
	handler := &BestPracticesHandler{Languages: []string{"go"}}
	wm.RegisterEventHandler(handler)

	// Create workflow state
	workflowState := &WorkflowState{
		SharedData:   make(map[string]interface{}),
		StepHistory:  []StepResult{},
		ResearchData: make(map[string]interface{}),
	}
	workflowState.SharedData["language"] = "go"

	// Emit event
	ctx := context.Background()
	signals := wm.emitEvent(ctx, StepEventBeforeExecution, step, workflowState)

	// Verify signal was emitted
	if len(signals) != 1 {
		t.Fatalf("Expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.Type != SignalResearchBestPractices {
		t.Errorf("Expected SignalResearchBestPractices, got %s", signal.Type)
	}
	if signal.Mode != SignalModeFunctionCall {
		t.Errorf("Expected SignalModeFunctionCall, got %s", signal.Mode)
	}
	if signal.Priority != SignalPriorityHigh {
		t.Errorf("Expected SignalPriorityHigh, got %d", signal.Priority)
	}
}

// TestBestPracticesHandler verifies BestPracticesHandler behavior
func TestBestPracticesHandler(t *testing.T) {
	handler := &BestPracticesHandler{Languages: []string{"go", "python"}}

	// Test CanHandle - should trigger for code_generation tag
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:   "code_gen",
		Tags: []string{"code_generation"},
	})

	event := StepEvent{
		Type:          StepEventBeforeExecution,
		Step:          step,
		WorkflowState: &WorkflowState{SharedData: map[string]interface{}{"language": "go"}},
	}

	if !handler.CanHandle(event) {
		t.Error("BestPracticesHandler should handle code_generation steps")
	}

	// Test HandleEvent
	signals, err := handler.HandleEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	if len(signals) != 1 {
		t.Fatalf("Expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.Type != SignalResearchBestPractices {
		t.Errorf("Expected SignalResearchBestPractices, got %s", signal.Type)
	}

	// Test language filtering
	event.WorkflowState.SharedData["language"] = "rust"
	signals, err = handler.HandleEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("Expected no signals for rust (not in filter list), got %d", len(signals))
	}
}

// TestComplexityAnalysisHandler verifies ComplexityAnalysisHandler behavior
func TestComplexityAnalysisHandler(t *testing.T) {
	handler := &ComplexityAnalysisHandler{Threshold: "high"}

	step := &BaseStep{}
	step.SetMetadata(StepMetadata{ID: "impl_step"})

	// Test with no previous step - should not trigger
	workflowState := &WorkflowState{
		SharedData:  make(map[string]interface{}),
		StepHistory: []StepResult{},
	}

	event := StepEvent{
		Type:          StepEventBeforeExecution,
		Step:          step,
		WorkflowState: workflowState,
	}

	if handler.CanHandle(event) {
		t.Error("Handler should not trigger without previous step")
	}

	// Add previous step with high complexity
	workflowState.StepHistory = append(workflowState.StepHistory, StepResult{
		Outcome: StepOutcomeSuccess,
		Data:    map[string]interface{}{"complexity": "high"},
	})

	if !handler.CanHandle(event) {
		t.Error("Handler should trigger after high complexity step")
	}

	// Test HandleEvent
	signals, err := handler.HandleEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	if len(signals) != 1 {
		t.Fatalf("Expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.Type != SignalAnalyzeComplexity {
		t.Errorf("Expected SignalAnalyzeComplexity, got %s", signal.Type)
	}
	if signal.Mode != SignalModeFullStep {
		t.Errorf("Expected SignalModeFullStep, got %s", signal.Mode)
	}
}

// TestSignalMerging verifies signal deduplication and priority handling
func TestSignalMerging(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)

	// Create signals with same type but different priorities
	signals1 := []StepSignal{{
		Type:     SignalResearchBestPractices,
		Priority: SignalPriorityLow,
	}}

	signals2 := []StepSignal{{
		Type:     SignalResearchBestPractices,
		Priority: SignalPriorityHigh,
	}}

	signals3 := []StepSignal{{
		Type:     SignalResearchCodePatterns,
		Priority: SignalPriorityMedium,
	}}

	// Merge signals
	merged := wm.mergeSignals(signals1, signals2, signals3)

	// Should have 2 unique signal types
	if len(merged) != 2 {
		t.Fatalf("Expected 2 merged signals, got %d", len(merged))
	}

	// Find SignalResearchBestPractices and verify highest priority was kept
	var foundBestPractices bool
	for _, signal := range merged {
		if signal.Type == SignalResearchBestPractices {
			foundBestPractices = true
			if signal.Priority != SignalPriorityHigh {
				t.Errorf("Expected merged signal to have priority High, got %d", signal.Priority)
			}
		}
	}

	if !foundBestPractices {
		t.Error("SignalResearchBestPractices not found in merged signals")
	}
}

// TestStepSpecificHandlers verifies step-specific handler execution
func TestStepSpecificHandlers(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)

	// Create two steps
	step1 := &BaseStep{}
	step1.SetMetadata(StepMetadata{
		ID:   "step1",
		Tags: []string{"code_generation"},
	})

	step2 := &BaseStep{}
	step2.SetMetadata(StepMetadata{
		ID:   "step2",
		Tags: []string{"testing"},
	})

	// Register step-specific handler for step1 only
	handler := &BestPracticesHandler{}
	wm.RegisterStepEventHandler("step1", handler)

	workflowState := &WorkflowState{
		SharedData: make(map[string]interface{}),
	}

	// Emit event for step1 - should trigger
	ctx := context.Background()
	signals1 := wm.emitEvent(ctx, StepEventBeforeExecution, step1, workflowState)
	if len(signals1) != 1 {
		t.Errorf("Expected 1 signal for step1, got %d", len(signals1))
	}

	// Emit event for step2 - should not trigger (handler only registered for step1)
	signals2 := wm.emitEvent(ctx, StepEventBeforeExecution, step2, workflowState)
	if len(signals2) != 0 {
		t.Errorf("Expected 0 signals for step2, got %d", len(signals2))
	}
}

// TestSecurityValidationHandler verifies SecurityValidationHandler behavior
func TestSecurityValidationHandler(t *testing.T) {
	handler := &SecurityValidationHandler{
		SensitiveTags: []string{"authentication", "payment"},
	}

	// Step with sensitive tag
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:   "auth_step",
		Tags: []string{"authentication", "implementation"},
	})

	event := StepEvent{
		Type: StepEventBeforeExecution,
		Step: step,
	}

	if !handler.CanHandle(event) {
		t.Error("Handler should trigger for authentication tag")
	}

	signals, err := handler.HandleEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	if len(signals) != 1 {
		t.Fatalf("Expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.Type != SignalValidateSecurity {
		t.Errorf("Expected SignalValidateSecurity, got %s", signal.Type)
	}
	if signal.Priority != SignalPriorityCritical {
		t.Errorf("Expected critical priority for security, got %d", signal.Priority)
	}
}
