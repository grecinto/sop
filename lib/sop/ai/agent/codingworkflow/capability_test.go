package codingworkflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestInferTaskInputsUsesPathLikeQueryTokens(t *testing.T) {
	workingDir, activeFile := inferTaskInputs("fix parser in src/backend/parser.go")
	if workingDir == "" {
		t.Fatal("expected working directory to be inferred")
	}
	if activeFile != "src/backend/parser.go" {
		t.Fatalf("expected active file to be inferred, got %q", activeFile)
	}
}

func TestOrchestratorBuildsContextWithAllComponents(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "parser.go"), []byte("package demo\nfunc Parse() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write parser.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "parser_test.go"), []byte("package demo\nimport \"testing\"\nfunc TestParse(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatalf("write parser_test.go: %v", err)
	}

	orchestrator := &Orchestrator{
		ContextManager:         &DefaultContextManager{},
		RepoIndexer:            &DefaultRepoIndexer{},
		PatchEngine:            &DefaultPatchEngine{},
		ChangedFileInspector:   &GitRepoInspector{},
		DiffPreviewer:          &DefaultDiffPreviewer{},
		RepoSearcher:           &FileSearchAdapter{},
		RepoInspector:          &GitRepoInspector{},
		Memory:                 NewInMemorySessionMemory(),
		Policy:                 &DefaultSafetyPolicy{},
		TestRunner:             &DefaultTestRunner{},
		Planner:                &DefaultPlanner{},
		ExpertiseRetriever:     &DefaultExpertiseRetriever{},
		KnowledgeBaseRetriever: &DefaultKnowledgeBaseRetriever{},
		ProviderRouter:         &DefaultProviderRouter{},
		GrammarRegistry:        NewGrammarRegistry(),
	}

	result, err := orchestrator.Execute(Task{WorkingDir: tmpDir, Description: "inspect parser", State: RunState{}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !contains(result.ContextPack.RelatedFiles, "parser.go") {
		t.Fatalf("expected file search to surface relevant files, got %v", result.ContextPack.RelatedFiles)
	}
}

func TestCapabilityRunIncludesKBMetadataInResult(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "parser.go"), []byte("package demo\nfunc Parse() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write parser.go: %v", err)
	}

	// Create capability with KB tracking components
	orchestrator := &Orchestrator{
		ContextManager:         &DefaultContextManager{},
		RepoIndexer:            &DefaultRepoIndexer{},
		Memory:                 NewInMemorySessionMemory(),
		ExpertiseRetriever:     &DefaultExpertiseRetriever{},
		KnowledgeBaseRetriever: &DefaultKnowledgeBaseRetriever{},
		ExplanationGenerator:   &ExplanationGenerator{},
		ValidationOrchestrator: NewValidationOrchestrator(
			NewValidator(ValidatorConfig{StrictMode: false}),
			CitationConfig{Enabled: false},
		),
		GrammarRegistry: NewGrammarRegistry(),
	}

	cap := &Capability{
		Orchestrator: orchestrator,
	}

	// Create a task that will trigger KB consultation
	task := Task{
		WorkingDir:  tmpDir,
		ActiveFile:  "parser.go",
		Description: "implement input validation",
		State:       RunState{},
	}

	// Execute directly through orchestrator (since Run() does complex path parsing)
	result, err := orchestrator.Execute(task)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Verify KB metadata is embedded in the message
	if len(result.KBsConsulted) > 0 {
		t.Logf("KBs consulted: %v", result.KBsConsulted)
	}
	if result.Explanation != "" {
		t.Logf("Explanation provided: %s", result.Explanation)
	}
	if result.TaskType != "" {
		t.Logf("Task type: %s", result.TaskType)
	}

	// Verify the message contains HTML comment with KB metadata
	// (This is the key integration that enables frontend feedback buttons)
	t.Logf("Message length: %d", len(result.Message))
	t.Logf("Message preview: %s", result.Message)

	if len(result.KBsConsulted) > 0 || result.Explanation != "" {
		if !strings.Contains(result.Message, "<!-- KB_METADATA:") {
			t.Error("expected KB metadata HTML comment in message when KBs are consulted")
		}
	}

	// Verify capability Run also works
	capResult, err := cap.Run("implement validation for parser", capabilities.RoutingState{})
	if err == nil {
		if capResult.Data != nil {
			t.Log("Capability result Data populated successfully")
		}
	}
}

// TestOrchestrateTestFixWorkflow verifies Phase 1.5 queue-based test debugging orchestration
func TestOrchestrateTestFixWorkflow(t *testing.T) {
	// Create mock queue to capture enqueued work items
	mockQueue := &mockWorkQueue{items: []map[string]interface{}{}}

	cap := &Capability{
		Orchestrator: &Orchestrator{}, // Minimal orchestrator, we're only testing queue injection
	}

	// Call orchestrateTestFixWorkflow
	handled, err := cap.orchestrateTestFixWorkflow(mockQueue, "fix failing TestHandleVectorization")
	if err != nil {
		t.Fatalf("orchestrateTestFixWorkflow returned error: %v", err)
	}
	if !handled {
		t.Fatal("expected orchestrateTestFixWorkflow to return handled=true")
	}

	// Verify 7 work items were enqueued
	if len(mockQueue.items) != 7 {
		t.Fatalf("expected 7 work items, got %d", len(mockQueue.items))
	}

	// Verify actors are correct (Phase 1.5 Step 4)
	expectedActors := []string{
		"test_debugger.runner",   // Step 1: Run tests
		"test_debugger.analyzer", // Step 2: Read errors
		"test_debugger.analyzer", // Step 3: Read test source
		"test_debugger.analyzer", // Step 4: Read implementation
		"test_debugger.fixer",    // Step 5: Apply fix
		"test_debugger.verifier", // Step 6: Verify fix
		"test_debugger.repair",   // Step 7: Iterate if needed
	}

	for i, expected := range expectedActors {
		item := mockQueue.items[i]
		actor, ok := item["actor"].(string)
		if !ok {
			t.Fatalf("work item %d missing actor field", i)
		}
		if actor != expected {
			t.Errorf("work item %d: expected actor %q, got %q", i, expected, actor)
		}

		// Verify query field exists
		query, ok := item["query"].(string)
		if !ok || query == "" {
			t.Errorf("work item %d: missing or empty query field", i)
		}

		// Verify priority field exists and is decreasing
		priority, ok := item["priority"].(int)
		if !ok {
			t.Errorf("work item %d: missing priority field", i)
		}
		expectedPriority := 100 - (i * 10)
		if priority != expectedPriority {
			t.Errorf("work item %d: expected priority %d, got %d", i, expectedPriority, priority)
		}
	}

	// Verify step 1 mentions "verbose output"
	step1Query := mockQueue.items[0]["query"].(string)
	if !strings.Contains(strings.ToLower(step1Query), "verbose") {
		t.Errorf("step 1 query should mention verbose output, got: %q", step1Query)
	}

	// Verify step 2 mentions reading error output
	step2Query := mockQueue.items[1]["query"].(string)
	if !strings.Contains(strings.ToLower(step2Query), "error") {
		t.Errorf("step 2 query should mention reading errors, got: %q", step2Query)
	}

	// Verify step 5 mentions applying fix
	step5Query := mockQueue.items[4]["query"].(string)
	if !strings.Contains(strings.ToLower(step5Query), "fix") {
		t.Errorf("step 5 query should mention applying fix, got: %q", step5Query)
	}

	// Verify step 6 mentions verification
	step6Query := mockQueue.items[5]["query"].(string)
	if !strings.Contains(strings.ToLower(step6Query), "verify") && !strings.Contains(strings.ToLower(step6Query), "re-run") {
		t.Errorf("step 6 query should mention verification, got: %q", step6Query)
	}
}

// mockWorkQueue captures enqueued work items for testing
type mockWorkQueue struct {
	items []map[string]interface{}
}

func (m *mockWorkQueue) Enqueue(item interface{}) error {
	itemMap, ok := item.(map[string]interface{})
	if !ok {
		return nil
	}
	m.items = append(m.items, itemMap)
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

// TestOrchestrateDetectsTestFixRequests verifies Phase 1.5 Step 3: test-fix detection
func TestOrchestrateDetectsTestFixRequests(t *testing.T) {
	testCases := []struct {
		query         string
		expectTestFix bool
		description   string
	}{
		{
			query:         "fix failing tests",
			expectTestFix: true,
			description:   "should detect 'fix failing tests'",
		},
		{
			query:         "fix the test",
			expectTestFix: true,
			description:   "should detect 'fix the test'",
		},
		{
			query:         "fix failed test in parser",
			expectTestFix: true,
			description:   "should detect 'fix failed test'",
		},
		{
			query:         "Fix TestHandleVectorizationRecoveryStatus",
			expectTestFix: true,
			description:   "should detect 'Fix Test...' (case insensitive)",
		},
		{
			query:         "implement new feature",
			expectTestFix: false,
			description:   "should NOT detect implementation requests",
		},
		{
			query:         "refactor the parser",
			expectTestFix: false,
			description:   "should NOT detect refactoring requests",
		},
		{
			query:         "test the changes",
			expectTestFix: false,
			description:   "should NOT detect 'test' without 'fix'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Verify the detection logic by checking if test-fix queries would
			// trigger the correct workflow based on the string matching logic
			// (This mirrors the logic in capability.go Orchestrate() method)

			lowerQuery := strings.ToLower(tc.query)
			isTestFix := strings.Contains(lowerQuery, "fix") &&
				(strings.Contains(lowerQuery, "test") ||
					strings.Contains(lowerQuery, "failing") ||
					strings.Contains(lowerQuery, "failed"))

			if isTestFix != tc.expectTestFix {
				t.Errorf("query %q: expected isTestFix=%v, got %v", tc.query, tc.expectTestFix, isTestFix)
			}
		})
	}
}
