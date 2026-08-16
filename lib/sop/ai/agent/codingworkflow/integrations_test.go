package codingworkflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestGitRepoInspectorListsChangedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	filePath := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("hello.txt"); err != nil {
		t.Fatalf("add file: %v", err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "tester", Email: "tester@example.com", When: time.Now()}}); err != nil {
		t.Fatalf("commit file: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("hello again\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	inspector := &GitRepoInspector{}
	files, err := inspector.ListChangedFiles(Task{WorkingDir: tmpDir}, RunState{})
	if err != nil {
		t.Fatalf("ListChangedFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Fatalf("expected changed file hello.txt, got %v", files)
	}
}

func TestDefaultDiffPreviewerBuildsPreview(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	previewer := &DefaultDiffPreviewer{}
	preview, err := previewer.Preview(Task{WorkingDir: tmpDir}, &Patch{FilePath: "sample.txt", OldContent: "hello", NewContent: "hi"}, RunState{})
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}
	if preview == "" {
		t.Fatalf("expected diff preview to be non-empty")
	}
	if !strings.Contains(preview, "@@") {
		t.Fatalf("expected diff preview to contain diff header, got %q", preview)
	}
}

func TestGitRepoInspectorInspectBuildsSummary(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	filePath := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("hello.txt"); err != nil {
		t.Fatalf("add file: %v", err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "tester", Email: "tester@example.com", When: time.Now()}}); err != nil {
		t.Fatalf("commit file: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("hello again\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	inspector := &GitRepoInspector{}
	summary, err := inspector.Inspect(Task{WorkingDir: tmpDir}, RunState{})
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if len(summary.ChangedFiles) != 1 || summary.ChangedFiles[0] != "hello.txt" {
		t.Fatalf("expected changed file hello.txt, got %v", summary.ChangedFiles)
	}
	if !strings.Contains(summary.DiffPreview, "hello.txt") {
		t.Fatalf("expected diff preview to mention hello.txt, got %q", summary.DiffPreview)
	}
}

func TestGitRepoInspectorResolvesRepoFromNestedWorkingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	filePath := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("hello.txt"); err != nil {
		t.Fatalf("add file: %v", err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "tester", Email: "tester@example.com", When: time.Now()}}); err != nil {
		t.Fatalf("commit file: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("hello again\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	nestedDir := filepath.Join(tmpDir, "subdir", "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	inspector := &GitRepoInspector{}
	files, err := inspector.ListChangedFiles(Task{WorkingDir: nestedDir}, RunState{})
	if err != nil {
		t.Fatalf("ListChangedFiles returned error for nested dir: %v", err)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Fatalf("expected changed file hello.txt from nested dir, got %v", files)
	}
}

func TestResolveRepoPathUsesCurrentWorkingDirectoryForDot(t *testing.T) {
	tmpDir := t.TempDir()
	if _, err := git.PlainInit(tmpDir, false); err != nil {
		t.Fatalf("init repo: %v", err)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(prevWD)
	}()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	resolved, err := resolveRepoPath(".")
	if err != nil {
		t.Fatalf("resolveRepoPath returned error: %v", err)
	}
	resolvedReal, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		resolvedReal = resolved
	}
	tmpDirReal, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		tmpDirReal = tmpDir
	}
	if resolvedReal != tmpDirReal {
		t.Fatalf("expected repo path %q, got %q", tmpDirReal, resolvedReal)
	}
}
