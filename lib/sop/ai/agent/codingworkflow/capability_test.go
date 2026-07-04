package codingworkflow

import (
	"os"
	"path/filepath"
	"testing"
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
