package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

func TestAppendAskCompletionSummary_PrefixesRepoRelativePaths(t *testing.T) {
	tmpDir := t.TempDir()

	repo, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("open worktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add README: %v", err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}}); err != nil {
		t.Fatalf("commit initial: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("hello updated"), 0o644); err != nil {
		t.Fatalf("update README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte("new file"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	ctx := context.WithValue(context.Background(), ctxKeyWorkingDir, tmpDir)
	finalText := appendAskCompletionSummary(ctx, "Done", []ai.ToolCall{{Name: "write_file"}}, nil)

	if !strings.Contains(finalText, "### What happened") {
		t.Fatalf("expected summary section, got %q", finalText)
	}
	if !strings.Contains(finalText, "./README.md") {
		t.Fatalf("expected relative README path in summary, got %q", finalText)
	}
	if !strings.Contains(finalText, "./notes.txt") {
		t.Fatalf("expected relative notes path in summary, got %q", finalText)
	}
	if strings.Contains(finalText, tmpDir) {
		t.Fatalf("expected repo-relative paths, got %q", finalText)
	}
}

func TestPersistAskCompletionState_StoresWorkstreamMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.WithValue(context.Background(), ctxKeyWorkingDir, tmpDir)

	repo, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("open worktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add README: %v", err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}}); err != nil {
		t.Fatalf("commit initial: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("updated"), 0o644); err != nil {
		t.Fatalf("update README: %v", err)
	}

	thread := &ConversationThread{ID: sop.NewUUID()}
	persistAskCompletionState(ctx, thread, "Done", []ai.ToolCall{{Name: "write_file"}}, nil)

	if thread.StreamContext == nil {
		t.Fatal("expected stream context to be initialized")
	}
	if thread.StreamContext.CompletionSummary != "Done" {
		t.Fatalf("expected completion summary to be stored, got %q", thread.StreamContext.CompletionSummary)
	}
	if !reflect.DeepEqual(thread.StreamContext.UpdatedFiles, []string{"README.md"}) {
		t.Fatalf("expected updated files to be stored, got %v", thread.StreamContext.UpdatedFiles)
	}
}
