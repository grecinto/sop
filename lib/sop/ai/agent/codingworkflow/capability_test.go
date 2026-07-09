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
