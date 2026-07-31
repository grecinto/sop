package agent

import (
	"testing"
)

// TestInferCapabilityRoutingSignal_LLMOrchestrator verifies
// that domain inference has been removed in favor of LLM-driven classification.
// The LLM should see all capabilities and use their Classify() methods to determine relevance.
func TestInferCapabilityRoutingSignal_LLMOrchestrator(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectIntent   string
		expectHint     string
		expectDomain   string // Should always be "" - no keyword pre-routing
		expectFastPath bool  // True if should use keyword routing, not LLM
	}{
		{
			name:           "Fix tests with Stores keyword - NO pre-routing",
			query:          "fix failing tests in Stores",
			expectIntent:   intentFamilyRepoEdit,
			expectHint:     capabilityHintCodingWorkflow,
			expectDomain:   "", // LLM will decide via Classify()
			expectFastPath: true,
		},
		{
			name:           "Update users table with Stores keyword - NO pre-routing",
			query:          "update the users table in Stores",
			expectIntent:   intentFamilyRepoEdit,
			expectHint:     capabilityHintCodingWorkflow,
			expectDomain:   "", // LLM will decide via Classify()
			expectFastPath: true,
		},
		{
			name:           "Query with 'store' keyword (singular) - NO pre-routing",
			query:          "fix the store database connection",
			expectIntent:   intentFamilyRepoEdit,
			expectHint:     capabilityHintCodingWorkflow,
			expectDomain:   "", // LLM will decide via Classify()
			expectFastPath: true,
		},
		{
			name:           "Spaces keyword - NO pre-routing",
			query:          "refactor code in Spaces",
			expectIntent:   intentFamilyRepoEdit,
			expectHint:     capabilityHintCodingWorkflow,
			expectDomain:   "", // LLM will decide via Classify()
			expectFastPath: true,
		},
		{
			name:           "Query with 'space' keyword (singular) - NO pre-routing",
			query:          "add tests for space management",
			expectIntent:   intentFamilyRepoEdit,
			expectHint:     capabilityHintCodingWorkflow,
			expectDomain:   "", // LLM will decide via Classify()
			expectFastPath: true,
		},
		{
			name:           "Desktop automation with Stores - NO pre-routing",
			query:          "run shell command to query Stores",
			expectIntent:   intentFamilyLocalOps,
			expectHint:     capabilityHintDesktopAutomation,
			expectDomain:   "", // LLM will decide via Classify()
			expectFastPath: true,
		},
		{
			name:           "Knowledge query without domain",
			query:          "explain how the architecture works",
			expectIntent:   intentFamilyKnowledge,
			expectHint:     capabilityHintGeneral,
			expectDomain:   "",
			expectFastPath: false, // General hint triggers LLM fallback
		},
		{
			name:           "Clear capability without domain",
			query:          "fix the parser bug",
			expectIntent:   intentFamilyRepoEdit,
			expectHint:     capabilityHintCodingWorkflow,
			expectDomain:   "",
			expectFastPath: true,
		},
		{
			name:           "Ambiguous query",
			query:          "I need help with something",
			expectIntent:   intentFamilyGeneral,
			expectHint:     capabilityHintGeneral,
			expectDomain:   "",
			expectFastPath: false, // General hint triggers LLM fallback
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, hint, domain := inferCapabilityRoutingSignal(tt.query, "")

			if intent != tt.expectIntent {
				t.Errorf("Intent mismatch: got %q, want %q", intent, tt.expectIntent)
			}
			if hint != tt.expectHint {
				t.Errorf("Capability hint mismatch: got %q, want %q", hint, tt.expectHint)
			}
			if domain != tt.expectDomain {
				t.Errorf("Domain mismatch: got %q, want %q", domain, tt.expectDomain)
			}

			// Verify fast path behavior
			usesFastPath := hint != capabilityHintGeneral
			if usesFastPath != tt.expectFastPath {
				t.Errorf("Fast path mismatch: got %v, want %v (hint=%q)", usesFastPath, tt.expectFastPath, hint)
			}
		})
	}
}

// TestColdStartRouting_FastPathVsLLMFallback verifies that cold-start routing
// uses keyword fast path when possible and LLM fallback when needed.
func TestColdStartRouting_FastPathVsLLMFallback(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		expectFastPath  bool
		expectLLMNeeded bool
	}{
		{
			name:            "Fix tests - fast path",
			query:           "fix failing tests",
			expectFastPath:  true,
			expectLLMNeeded: false,
		},
		{
			name:            "Git command - fast path",
			query:           "run git status",
			expectFastPath:  true,
			expectLLMNeeded: false,
		},
		{
			name:            "Stores query - fast path with domain",
			query:           "fix users table in Stores",
			expectFastPath:  true,
			expectLLMNeeded: false,
		},
		{
			name:            "Ambiguous - LLM fallback",
			query:           "help me",
			expectFastPath:  false,
			expectLLMNeeded: true,
		},
		{
			name:            "Generic question - LLM fallback",
			query:           "what should I do next?",
			expectFastPath:  false,
			expectLLMNeeded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, hint, _ := inferCapabilityRoutingSignal(tt.query, "")

			usesFastPath := hint != capabilityHintGeneral
			needsLLM := hint == capabilityHintGeneral

			if usesFastPath != tt.expectFastPath {
				t.Errorf("Fast path mismatch: got %v, want %v (intent=%q, hint=%q)",
					usesFastPath, tt.expectFastPath, intent, hint)
			}
			if needsLLM != tt.expectLLMNeeded {
				t.Errorf("LLM needed mismatch: got %v, want %v (intent=%q, hint=%q)",
					needsLLM, tt.expectLLMNeeded, intent, hint)
			}
		})
	}
}
