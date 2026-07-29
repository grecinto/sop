package codingworkflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestServiceCapabilityBuildContextTool(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "parser.go"), []byte("package demo\nfunc Parse() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write parser.go: %v", err)
	}

	cap := NewServiceCapability(&Orchestrator{
		ContextManager: &DefaultContextManager{},
		RepoIndexer:    &DefaultRepoIndexer{},
	})

	out, err := cap.ExecuteTool(context.Background(), "build_context", map[string]any{
		"query":       "inspect parser",
		"working_dir": tmpDir,
	}, capabilities.RoutingState{Domain: "desktop", CapabilityHint: "repo_edit"})
	if err != nil {
		t.Fatalf("ExecuteTool returned error: %v", err)
	}
	if !strings.Contains(out, "Context Pack") {
		t.Fatalf("expected context summary in output, got %q", out)
	}
}

func TestServiceCapabilityParticipatesForCodingQueries(t *testing.T) {
	cap := NewServiceCapability(&Orchestrator{})
	result, handled, err := cap.Participate(context.Background(), "fix the failing parser test", capabilities.RoutingState{Domain: "desktop"})
	if err != nil {
		t.Fatalf("Participate returned error: %v", err)
	}
	if !handled {
		t.Fatal("expected capability to participate for coding query")
	}
	if !strings.Contains(result.Summary, "coding workflow services") {
		t.Fatalf("expected summary to mention coding workflow services, got %q", result.Summary)
	}
}
