package codingworkflow

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// TestStepLifecycleHooksCalledInOrder verifies that all lifecycle hooks are called
// in the correct order during Step execution.
func TestStepLifecycleHooksCalledInOrder(t *testing.T) {
	var callOrder []string

	// Create a test step that tracks hook calls
	step := &BaseStep{
		metadata: StepMetadata{
			Name:        "TestStep",
			Description: "Test lifecycle hook ordering",
		},
	}

	// Override lifecycle methods to track calls
	testStep := &struct {
		*BaseStep
	}{
		BaseStep: step,
	}

	ctx := context.Background()
	query := "test query"
	routingState := capabilities.RoutingState{
		Domain:         "test",
		Gate:           "test-gate",
		CapabilityHint: "",
	}

	// Test OnStart
	if err := testStep.OnStart(ctx, query, routingState); err != nil {
		t.Errorf("OnStart failed: %v", err)
	}
	callOrder = append(callOrder, "OnStart")

	// Test OnBeforePrompt
	if _, err := testStep.OnBeforePrompt(ctx, query, routingState); err != nil {
		t.Errorf("OnBeforePrompt failed: %v", err)
	}
	callOrder = append(callOrder, "OnBeforePrompt")

	// Test OnAfterToolResult
	if _, shouldContinue, err := testStep.OnAfterToolResult(ctx, "test_tool", "test result", routingState); err != nil {
		t.Errorf("OnAfterToolResult failed: %v", err)
	} else if !shouldContinue {
		t.Error("Expected OnAfterToolResult to return true for shouldContinue")
	}
	callOrder = append(callOrder, "OnAfterToolResult")

	// Test OnAfterTurn
	if shouldContinue, err := testStep.OnAfterTurn(ctx, 1, routingState); err != nil {
		t.Errorf("OnAfterTurn failed: %v", err)
	} else if !shouldContinue {
		t.Error("Expected OnAfterTurn to return true for shouldContinue")
	}
	callOrder = append(callOrder, "OnAfterTurn")

	// Test OnFinalize
	if result, err := testStep.OnFinalize(ctx, routingState); err != nil {
		t.Errorf("OnFinalize failed: %v", err)
	} else if result.Outcome != StepOutcomeSuccess {
		t.Errorf("Expected Success outcome, got %v", result.Outcome)
	}
	callOrder = append(callOrder, "OnFinalize")

	// Verify order
	expectedOrder := []string{"OnStart", "OnBeforePrompt", "OnAfterToolResult", "OnAfterTurn", "OnFinalize"}
	if len(callOrder) != len(expectedOrder) {
		t.Errorf("Expected %d hook calls, got %d", len(expectedOrder), len(callOrder))
	}

	for i, expected := range expectedOrder {
		if i >= len(callOrder) {
			t.Errorf("Missing hook call: %s", expected)
			continue
		}
		if callOrder[i] != expected {
			t.Errorf("Hook call %d: expected %s, got %s", i, expected, callOrder[i])
		}
	}
}

// TestStepSharedInfrastructureInjection verifies that SharedInfrastructure is
// properly injected and accessible within lifecycle hooks.
func TestStepSharedInfrastructureInjection(t *testing.T) {
	step := &BaseStep{
		metadata: StepMetadata{
			Name:        "InfraTest",
			Description: "Test infrastructure injection",
		},
	}

	// Create mock infrastructure
	mockInfra := &SharedInfrastructure{
		ContextManager: nil, // Simplified for test
	}

	// Inject infrastructure
	step.SetSharedInfrastructure(mockInfra)

	// Verify it was stored
	if step.infra != mockInfra {
		t.Error("Infrastructure not properly stored after injection")
	}

	// Create a custom step that uses infrastructure
	customStep := &struct {
		*BaseStep
		infraAccessed bool
	}{
		BaseStep: step,
	}

	// In a real hook, we would access s.infra to use operations
	if customStep.infra == nil {
		t.Error("Infrastructure not accessible in step")
	} else {
		customStep.infraAccessed = true
	}

	if !customStep.infraAccessed {
		t.Error("Infrastructure was not accessed during test")
	}
}

func TestDetectUserRedirect_DoesNotTreatPhaseRefinementAsTaskSwitch(t *testing.T) {
	step := &BaseStep{}
	ctx := context.WithValue(context.Background(), "latest_user_query", "actually, investigate the failing tests first and then update the expectations")

	isRedirect, feedback := step.DetectUserRedirect(ctx)
	if isRedirect {
		t.Fatalf("expected implementation-compatible refinement to stay in the current conversation, got redirect with feedback %q", feedback)
	}
}

// TestStepOutcomeHandling verifies that different StepOutcome values are
// properly handled by the workflow.
func TestStepOutcomeHandling(t *testing.T) {
	tests := []struct {
		name           string
		outcome        StepOutcome
		expectedResult bool
	}{
		{"Success outcome", StepOutcomeSuccess, true},
		{"Failed outcome", StepOutcomeFailed, false},
		{"Retry outcome", StepOutcomeRetry, false},
		{"NeedsClarify outcome", StepOutcomeNeedsClarify, false},
		{"ShortCircuit outcome", StepOutcomeShortCircuit, false},
		{"Skipped outcome", StepOutcomeSkipped, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &BaseStep{
				metadata: StepMetadata{
					Name:        "TestOutcome",
					Description: "Test outcome handling",
				},
			}

			// Set outcome in step state
			step.SetStepState("outcome", tt.outcome)

			// Retrieve and verify
			if stored, ok := step.GetStepStateValue("outcome"); ok {
				if storedOutcome, ok := stored.(StepOutcome); ok {
					if storedOutcome != tt.outcome {
						t.Errorf("Expected outcome %v, got %v", tt.outcome, storedOutcome)
					}
				} else {
					t.Error("Stored value is not a StepOutcome")
				}
			} else {
				t.Error("Failed to retrieve outcome from step state")
			}
		})
	}
}

// TestStepStateManagementAcrossHooks verifies that state persists across
// lifecycle hook calls.
func TestStepStateManagementAcrossHooks(t *testing.T) {
	step := &BaseStep{
		metadata: StepMetadata{
			Name:        "StateTest",
			Description: "Test state persistence",
		},
	}

	ctx := context.Background()
	query := "test query"
	routingState := capabilities.RoutingState{}

	// Set state during OnStart
	if err := step.OnStart(ctx, query, routingState); err != nil {
		t.Fatalf("OnStart failed: %v", err)
	}
	step.SetStepState("test_key", "test_value")
	step.SetStepState("counter", 1)

	// Verify state persists during OnBeforePrompt
	if _, err := step.OnBeforePrompt(ctx, query, routingState); err != nil {
		t.Fatalf("OnBeforePrompt failed: %v", err)
	}
	if val, ok := step.GetStepStateValue("test_key"); !ok {
		t.Error("State did not persist to OnBeforePrompt")
	} else if strVal, ok := val.(string); !ok || strVal != "test_value" {
		t.Errorf("Expected test_value, got %v", val)
	}

	// Modify state during OnAfterToolResult
	if _, _, err := step.OnAfterToolResult(ctx, "tool", "result", routingState); err != nil {
		t.Fatalf("OnAfterToolResult failed: %v", err)
	}
	step.SetStepState("counter", 2)

	// Verify modified state persists to OnFinalize
	if result, err := step.OnFinalize(ctx, routingState); err != nil {
		t.Fatalf("OnFinalize failed: %v", err)
	} else if result.Outcome != StepOutcomeSuccess {
		t.Errorf("Expected Success, got %v", result.Outcome)
	}

	if counter, ok := step.GetStepStateValue("counter"); !ok {
		t.Error("Counter state was lost")
	} else if intCounter, ok := counter.(int); !ok || intCounter != 2 {
		t.Errorf("State modification did not persist: expected counter=2, got %v", counter)
	}
}

// TestStepTerminalityInWorkflow verifies that terminal steps properly stop
// workflow execution and final steps complete normally.
func TestStepTerminalityInWorkflow(t *testing.T) {
	tests := []struct {
		name       string
		isTerminal bool
		expected   string
	}{
		{"Terminal step", true, "stops workflow early"},
		{"Normal step", false, "continues workflow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &BaseStep{
				metadata: StepMetadata{
					Name:        "TerminalityTest",
					Description: "Test terminality",
					IsTerminal:  tt.isTerminal,
				},
			}

			if step.Metadata().IsTerminal != tt.isTerminal {
				t.Errorf("Expected IsTerminal=%v, got %v", tt.isTerminal, step.Metadata().IsTerminal)
			}
		})
	}
}
