package codingworkflow

import (
	"testing"
)

// TestTieBreaking_DeterministicFallback verifies deterministic tie-breaking
func TestTieBreaking_DeterministicFallback(t *testing.T) {
	tc := DefaultTaskClassifier()

	// Task that creates a tie between testing and debugging
	// "fix" adds 0.4 to debugging
	// "test" adds 0.4 to testing
	// They should tie at 0.4 each
	result := tc.Classify("fix test handler", "", "/project")

	// With deterministic fallback, debugging should win (higher priority)
	if result.TaskType != TaskTypeDebugging {
		t.Errorf("Expected debugging to win tie-break, got %s", result.TaskType)
	}

	// Confidence should be reduced to indicate tie-breaking
	if result.Confidence >= 0.4 {
		t.Errorf("Expected reduced confidence after tie-break, got %.2f", result.Confidence)
	}
}

// TestTieBreaking_NoTie verifies behavior when there's a clear winner
func TestTieBreaking_NoTie(t *testing.T) {
	tc := DefaultTaskClassifier()

	// Clear debugging task (no tie)
	result := tc.Classify("fix error in connection handling", "server.go", "/project")

	if result.TaskType != TaskTypeDebugging {
		t.Errorf("Expected debugging, got %s", result.TaskType)
	}

	// No tie-breaking, so confidence should be higher than 0.4
	if result.Confidence < 0.4 {
		t.Errorf("Expected higher confidence (no tie), got %.2f", result.Confidence)
	}
}

// TestTieBreaking_PriorityOrder verifies deterministic priority ordering
func TestTieBreaking_PriorityOrder(t *testing.T) {
	tests := []struct {
		name           string
		task           string
		expectedWinner TaskType
		description    string
	}{
		{
			name:           "Debugging_vs_Testing",
			task:           "fix test", // fix=0.4 debug, test=0.4 testing → tie at 0.4
			expectedWinner: TaskTypeDebugging,
			description:    "Debugging has higher priority than testing",
		},
		{
			name:           "Testing_vs_Refactoring",
			task:           "check improve", // check=0.4 testing, improve=0.3 refactor
			expectedWinner: TaskTypeTesting,
			description:    "Testing has higher priority than refactoring",
		},
		{
			name:           "Implementation_vs_Refactoring",
			task:           "add cleanup", // add=0.3 impl, cleanup=0.3 refactor → tie at 0.3
			expectedWinner: TaskTypeImplementation,
			description:    "Implementation has higher priority than refactoring",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := DefaultTaskClassifier()
			result := tc.Classify(tt.task, "", "/project")

			if result.TaskType != tt.expectedWinner {
				t.Errorf("%s: expected %s, got %s", tt.description, tt.expectedWinner, result.TaskType)
			}
		})
	}
}

// TestTieBreaking_ConfidenceReduction verifies confidence adjustment
func TestTieBreaking_ConfidenceReduction(t *testing.T) {
	tc := DefaultTaskClassifier()

	// Task creating a tie at 0.4 score
	result := tc.Classify("fix test", "", "/project")

	// After deterministic tie-breaking, confidence should be 0.4 * 0.9 = 0.36
	expectedConfidence := 0.4 * 0.9
	if result.Confidence < expectedConfidence-0.01 || result.Confidence > expectedConfidence+0.01 {
		t.Errorf("Expected confidence ~%.2f, got %.2f", expectedConfidence, result.Confidence)
	}
}

// TestTieBreaking_MultipleTies verifies handling when several types tie
func TestTieBreaking_MultipleTies(t *testing.T) {
	tc := DefaultTaskClassifier()

	// Even with multiple ties, the priority order should resolve it
	result := tc.Classify("fix test", "", "/project")

	// Debugging should win based on priority
	if result.TaskType != TaskTypeDebugging {
		t.Errorf("Expected debugging to win priority, got %s", result.TaskType)
	}

	// Confidence should be reduced due to tie
	expectedMax := 0.4 // Original max score before reduction
	if result.Confidence >= expectedMax {
		t.Errorf("Expected reduced confidence (< %.2f), got %.2f", expectedMax, result.Confidence)
	}
}
