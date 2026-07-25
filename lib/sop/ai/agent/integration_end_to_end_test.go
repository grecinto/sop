package agent

import (
	"testing"

	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// TestEndToEnd_CodingWorkflow_SDLC verifies the complete pipeline:
// Query → Routing → Capability Selection → Task Classification → Workflow Selection
func TestEndToEnd_CodingWorkflow_SDLC(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectRouting  string // Expected capability hint from routing
		expectTaskType codingworkflow.TaskType
		expectWorkflow string // Which workflow should be selected
	}{
		// === SDLC Enhancement Queries ===
		{
			name:           "Design system architecture",
			query:          "design the authentication system architecture",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeArchitecture,
			expectWorkflow: "orchestrateArchitectureWorkflow",
		},
		{
			name:           "Plan feature implementation",
			query:          "plan how to implement user permissions",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeArchitecture,
			expectWorkflow: "orchestrateArchitectureWorkflow",
		},
		{
			name:           "Write documentation",
			query:          "document the API endpoints in the README",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeDocumentation,
			expectWorkflow: "orchestrateDocumentationWorkflow",
		},
		{
			name:           "Create technical spec",
			query:          "write a spec for the payment processing module",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeDocumentation,
			expectWorkflow: "orchestrateDocumentationWorkflow",
		},
		{
			name:           "Propose solution",
			query:          "propose a solution for handling rate limiting",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeArchitecture,
			expectWorkflow: "orchestrateArchitectureWorkflow",
		},

		// === Traditional Coding Workflow Queries ===
		{
			name:           "Fix failing tests",
			query:          "fix the failing unit tests in auth module",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeDebugging,
			expectWorkflow: "orchestrateTestFixWorkflow",
		},
		{
			name:           "Implement feature",
			query:          "implement user login functionality",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeImplementation,
			expectWorkflow: "orchestrateImplementationWorkflow",
		},
		{
			name:           "Refactor code",
			query:          "refactor the database connection handling",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeRefactoring,
			expectWorkflow: "orchestrateImplementationWorkflow",
		},
		{
			name:           "Add tests",
			query:          "add unit tests for the parser module",
			expectRouting:  capabilityHintCodingWorkflow,
			expectTaskType: codingworkflow.TaskTypeTesting,
			expectWorkflow: "orchestrateImplementationWorkflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// === Step 1: Test Routing Layer ===
			intentFamily, capabilityHint, _ := inferCapabilityRoutingSignal(tt.query, "")

			if capabilityHint != tt.expectRouting {
				t.Errorf("Routing failed: got capability=%q, want %q", capabilityHint, tt.expectRouting)
			}

			if intentFamily != intentFamilyRepoEdit {
				t.Errorf("Routing failed: got intent=%q, want %q", intentFamily, intentFamilyRepoEdit)
			}

			// === Step 2: Test Task Classification ===
			classifier := codingworkflow.DefaultTaskClassifier()
			classification := classifier.Classify(tt.query, "", "/test/repo")

			if classification.TaskType != tt.expectTaskType {
				t.Errorf("Task classification failed: got type=%q, want %q",
					classification.TaskType, tt.expectTaskType)
			}

			// === Step 3: Verify Workflow Selection Logic ===
			// This verifies the task type maps to the correct workflow orchestrator
			workflowName := getExpectedWorkflowForTaskType(classification.TaskType)
			if workflowName != tt.expectWorkflow {
				t.Errorf("Workflow selection failed: got workflow=%q, want %q",
					workflowName, tt.expectWorkflow)
			}

			t.Logf("✓ End-to-end success: %q → %s → %s → %s",
				tt.query, capabilityHint, classification.TaskType, workflowName)
		})
	}
}

// TestEndToEnd_DesktopAutomation verifies desktop automation routing
func TestEndToEnd_DesktopAutomation(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expectRouting string
		expectIntent  string
	}{
		{
			name:          "Git command",
			query:         "run git status",
			expectRouting: capabilityHintDesktopAutomation,
			expectIntent:  intentFamilyLocalOps,
		},
		{
			name:          "Shell command",
			query:         "execute shell command to check disk space",
			expectRouting: capabilityHintDesktopAutomation,
			expectIntent:  intentFamilyLocalOps,
		},
		{
			name:          "Postgres query",
			query:         "query postgres database for user records",
			expectRouting: capabilityHintDesktopAutomation,
			expectIntent:  intentFamilyLocalOps,
		},
		{
			name:          "File system operation",
			query:         "check the filesystem for large files",
			expectRouting: capabilityHintDesktopAutomation,
			expectIntent:  intentFamilyLocalOps,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intentFamily, capabilityHint, _ := inferCapabilityRoutingSignal(tt.query, "")

			if capabilityHint != tt.expectRouting {
				t.Errorf("Routing failed: got capability=%q, want %q", capabilityHint, tt.expectRouting)
			}

			if intentFamily != tt.expectIntent {
				t.Errorf("Routing failed: got intent=%q, want %q", intentFamily, tt.expectIntent)
			}

			t.Logf("✓ Desktop automation routing: %q → %s (%s)",
				tt.query, capabilityHint, intentFamily)
		})
	}
}

// TestEndToEnd_FastPathOptimization verifies LLM calls are avoided when possible
func TestEndToEnd_FastPathOptimization(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		expectFastPath  bool
		expectLLMNeeded bool
	}{
		{
			name:            "Clear coding intent - fast path",
			query:           "fix the authentication bug",
			expectFastPath:  true,
			expectLLMNeeded: false,
		},
		{
			name:            "Clear desktop intent - fast path",
			query:           "run git log",
			expectFastPath:  true,
			expectLLMNeeded: false,
		},
		{
			name:            "Clear design intent - fast path",
			query:           "design the database schema",
			expectFastPath:  true,
			expectLLMNeeded: false,
		},
		{
			name:            "Ambiguous query - LLM needed",
			query:           "help me with this",
			expectFastPath:  false,
			expectLLMNeeded: true,
		},
		{
			name:            "Generic question - LLM needed",
			query:           "what should I do?",
			expectFastPath:  false,
			expectLLMNeeded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, capabilityHint, _ := inferCapabilityRoutingSignal(tt.query, "")

			usesFastPath := capabilityHint != capabilityHintGeneral
			needsLLM := capabilityHint == capabilityHintGeneral

			if usesFastPath != tt.expectFastPath {
				t.Errorf("Fast path detection failed: got %v, want %v (hint=%s)",
					usesFastPath, tt.expectFastPath, capabilityHint)
			}

			if needsLLM != tt.expectLLMNeeded {
				t.Errorf("LLM necessity detection failed: got %v, want %v (hint=%s)",
					needsLLM, tt.expectLLMNeeded, capabilityHint)
			}

			pathType := "fast path (no LLM)"
			if needsLLM {
				pathType = "slow path (LLM)"
			}
			t.Logf("✓ Optimization: %q → %s", tt.query, pathType)
		})
	}
}

// getExpectedWorkflowForTaskType maps task types to workflow orchestrators
// This matches the switch statement in capability.go Orchestrate method
func getExpectedWorkflowForTaskType(taskType codingworkflow.TaskType) string {
	switch taskType {
	case codingworkflow.TaskTypeDebugging:
		return "orchestrateTestFixWorkflow"
	case codingworkflow.TaskTypeArchitecture:
		return "orchestrateArchitectureWorkflow"
	case codingworkflow.TaskTypeDocumentation:
		return "orchestrateDocumentationWorkflow"
	case codingworkflow.TaskTypeImplementation,
		codingworkflow.TaskTypeTesting,
		codingworkflow.TaskTypeRefactoring:
		return "orchestrateImplementationWorkflow"
	default:
		return "orchestrateImplementationWorkflow" // default fallback
	}
}

// TestEndToEnd_StoresSpacesBackwardsCompatibility verifies domain routing still works
func TestEndToEnd_StoresSpacesBackwardsCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expectDomain  string
		expectRouting string
	}{
		{
			name:          "Stores query with fix",
			query:         "fix the users table in Stores",
			expectDomain:  StoresDomain,
			expectRouting: capabilityHintCodingWorkflow,
		},
		{
			name:          "Spaces query with document",
			query:         "document the API in Spaces",
			expectDomain:  SpacesDomain,
			expectRouting: capabilityHintCodingWorkflow,
		},
		{
			name:          "Store query with shell",
			query:         "run shell command to query store database",
			expectDomain:  StoresDomain,
			expectRouting: capabilityHintDesktopAutomation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, capabilityHint, domain := inferCapabilityRoutingSignal(tt.query, "")

			if domain != tt.expectDomain {
				t.Errorf("Domain inference failed: got %q, want %q", domain, tt.expectDomain)
			}

			if capabilityHint != tt.expectRouting {
				t.Errorf("Routing failed: got %q, want %q", capabilityHint, tt.expectRouting)
			}

			t.Logf("✓ Domain routing: %q → domain=%s, capability=%s",
				tt.query, domain, capabilityHint)
		})
	}
}
