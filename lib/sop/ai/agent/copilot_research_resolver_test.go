package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
	coredb "github.com/sharedcode/sop/database"
)

func TestResolveResearchSources_PreservesExplicitSelection(t *testing.T) {
	ctx := context.Background()
	storagePath := t.TempDir()

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{storagePath},
	}
	if _, err := coredb.Setup(ctx, opts); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	sysDB := aidb.NewDatabase(opts)
	agent := NewCopilotAgent(Config{}, nil, sysDB)

	// Explicit user selection should always be preserved
	explicitKB := ai.ArtifactReference{Name: "medical-kb", Type: ai.ArtifactTypeSpace, DatabaseName: "system"}
	payload := &ai.SessionPayload{
		SelectedKBs: []ai.ArtifactReference{explicitKB},
		ActiveRepo:  "user/go-project", // Even with Go context
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	resolved := agent.ResolveResearchSources(ctx, "explain error handling", TaskContextClassification{})

	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved source, got %d", len(resolved))
	}
	if resolved[0].Name != "medical-kb" {
		t.Fatalf("expected explicit selection to be preserved, got %q", resolved[0].Name)
	}
}

func TestResolveResearchSources_DetectsGoFromRepo(t *testing.T) {
	ctx := context.Background()
	storagePath := t.TempDir()

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{storagePath},
	}
	if _, err := coredb.Setup(ctx, opts); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	sysDB := aidb.NewDatabase(opts)

	// Create a "go-best-practices" knowledge base to be discoverable
	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	kb, err := sysDB.OpenKnowledgeBase(ctx, "go-best-practices", tx, nil, nil, false, true)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	if err := kb.IngestThought(ctx, "Use short variable names", "best_practices", "system", nil, map[string]any{}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	agent := NewCopilotAgent(Config{}, nil, sysDB)

	// No explicit selection, but Go repo context
	payload := &ai.SessionPayload{
		ActiveRepo: "user/go-microservice",
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	resolved := agent.ResolveResearchSources(ctx, "explain error handling in Go", TaskContextClassification{})

	if len(resolved) == 0 {
		t.Fatal("expected auto-detected source, got none")
	}

	foundGoSource := false
	for _, ref := range resolved {
		if strings.Contains(strings.ToLower(ref.Name), "go") {
			foundGoSource = true
			break
		}
	}

	if !foundGoSource {
		t.Fatalf("expected Go-related source, got %v", resolved)
	}
}

func TestResolveResearchSources_DetectsLanguageFromQuery(t *testing.T) {
	ctx := context.Background()
	storagePath := t.TempDir()

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{storagePath},
	}
	if _, err := coredb.Setup(ctx, opts); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	sysDB := aidb.NewDatabase(opts)

	// Create a "python-best-practices" knowledge base
	tx, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	kb, err := sysDB.OpenKnowledgeBase(ctx, "python-best-practices", tx, nil, nil, false, true)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	if err := kb.IngestThought(ctx, "Use list comprehensions", "best_practices", "system", nil, map[string]any{}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	agent := NewCopilotAgent(Config{}, nil, sysDB)

	payload := &ai.SessionPayload{}
	ctx = context.WithValue(ctx, "session_payload", payload)

	resolved := agent.ResolveResearchSources(ctx, "how do I handle exceptions in Python?", TaskContextClassification{})

	if len(resolved) == 0 {
		t.Fatal("expected auto-detected source from query, got none")
	}

	foundPythonSource := false
	for _, ref := range resolved {
		if strings.Contains(strings.ToLower(ref.Name), "python") {
			foundPythonSource = true
			break
		}
	}

	if !foundPythonSource {
		t.Fatalf("expected Python-related source, got %v", resolved)
	}
}

func TestResolveResearchSources_ReturnsNilWhenNoMatch(t *testing.T) {
	ctx := context.Background()
	storagePath := t.TempDir()

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{storagePath},
	}
	if _, err := coredb.Setup(ctx, opts); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	sysDB := aidb.NewDatabase(opts)
	agent := NewCopilotAgent(Config{}, nil, sysDB)

	// No selection, no language context, generic query
	payload := &ai.SessionPayload{}
	ctx = context.WithValue(ctx, "session_payload", payload)

	resolved := agent.ResolveResearchSources(ctx, "what is the weather?", TaskContextClassification{})

	// Should return nil/empty when no relevant source is found
	if len(resolved) > 0 {
		t.Fatalf("expected no sources for generic query, got %v", resolved)
	}
}

func TestDetectLanguageFromContext_RepoName(t *testing.T) {
	agent := &CopilotAgent{}

	tests := []struct {
		repo     string
		expected string
	}{
		{"user/go-microservice", "Go"},
		{"org/python-api", "Python"},
		{"team/typescript-frontend", "TypeScript"},
		{"user/rust-compiler", "Rust"},
		{"company/java-backend", "Java"},
		{"dev/javascript-app", "JavaScript"},
		{"team/ruby-on-rails", "Ruby"},
		{"org/dotnet-service", "CSharp"},
		{"user/random-project", ""}, // No language match
	}

	for _, tt := range tests {
		payload := &ai.SessionPayload{ActiveRepo: tt.repo}
		detected := agent.detectLanguageFromContext(context.Background(), "", payload)
		if detected != tt.expected {
			t.Errorf("detectLanguageFromContext(%q) = %q, want %q", tt.repo, detected, tt.expected)
		}
	}
}

func TestDetectFileLanguage(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"main.go", "Go"},
		{"app.py", "Python"},
		{"index.ts", "TypeScript"},
		{"component.tsx", "TypeScript"},
		{"script.js", "JavaScript"},
		{"view.jsx", "JavaScript"},
		{"Main.java", "Java"},
		{"lib.rs", "Rust"},
		{"app.rb", "Ruby"},
		{"Program.cs", "CSharp"},
		{"README.md", ""}, // No language match
	}

	for _, tt := range tests {
		detected := detectFileLanguage(tt.filename)
		if detected != tt.expected {
			t.Errorf("detectFileLanguage(%q) = %q, want %q", tt.filename, detected, tt.expected)
		}
	}
}
