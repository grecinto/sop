package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestDiscoverWorkingDirCandidatesPrefersCurrentRepoAndNearbySiblings(t *testing.T) {
	root := t.TempDir()
	repoOne := filepath.Join(root, "repo-one")
	repoTwo := filepath.Join(root, "repo-two")
	workspaceDir := filepath.Join(root, "workspace")
	startDir := filepath.Join(repoOne, "src")

	for _, dir := range []string{repoOne, repoTwo, workspaceDir, startDir, filepath.Join(repoOne, ".git"), filepath.Join(repoTwo, ".git")} {
		if dir == filepath.Join(repoOne, ".git") || dir == filepath.Join(repoTwo, ".git") {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			continue
		}
		if dir == startDir {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(repoOne, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repoOne .git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoTwo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repoTwo .git: %v", err)
	}

	candidates := discoverWorkingDirCandidates(startDir)
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %v", candidates)
	}
	if candidates[0] != repoOne {
		t.Fatalf("expected first candidate %q, got %q", repoOne, candidates[0])
	}
	if !containsStringValue(candidates, repoTwo) {
		t.Fatalf("expected sibling repo %q in candidates, got %v", repoTwo, candidates)
	}
}

func TestRepoPathFromContext_UsesWorkingDirForCurrentRepoAlias(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{Variables: map[string]any{"working_dir": repoRoot}})

	for _, alias := range []string{"current repo", "current", "here", "pwd", "workspace"} {
		resolved := repoPathFromContext(ctx, map[string]any{"path": alias})
		if resolved != repoRoot {
			t.Fatalf("expected alias %q to resolve to %q, got %q", alias, repoRoot, resolved)
		}
	}
}

func TestListDirectory_ResolvesRelativePathAgainstWorkingDir(t *testing.T) {
	repoRoot := t.TempDir()
	targetDir := filepath.Join(repoRoot, "ai", "agent")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "sample.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{Variables: map[string]any{"working_dir": repoRoot}})
	ag := &CopilotAgent{}

	out, err := ag.toolListDirectory(ctx, map[string]any{"path": "ai/agent"})
	if err != nil {
		t.Fatalf("toolListDirectory() error = %v", err)
	}
	if !strings.Contains(out, "sample.txt") {
		t.Fatalf("expected resolved directory listing to include sample.txt, got %q", out)
	}
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
