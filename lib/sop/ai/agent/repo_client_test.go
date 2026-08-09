package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/repo"
)

func TestNewRepoClientForPathDetectsGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatalf("create git dir: %v", err)
	}

	client, err := newRepoClientForPath(tmpDir)
	if err != nil {
		t.Fatalf("newRepoClientForPath returned error: %v", err)
	}
	if client == nil || client.Name() != "git" {
		t.Fatalf("expected git client, got %#v", client)
	}
}

func TestNewRepoClientForPathDetectsSvnRepo(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".svn"), 0o755); err != nil {
		t.Fatalf("create svn dir: %v", err)
	}

	client, err := newRepoClientForPath(tmpDir)
	if err != nil {
		t.Fatalf("newRepoClientForPath returned error: %v", err)
	}
	if client == nil || client.Name() != "svn" {
		t.Fatalf("expected svn client, got %#v", client)
	}
}

func TestNewRepoClientForPathDetectsHgRepo(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".hg"), 0o755); err != nil {
		t.Fatalf("create hg dir: %v", err)
	}

	client, err := newRepoClientForPath(tmpDir)
	if err != nil {
		t.Fatalf("newRepoClientForPath returned error: %v", err)
	}
	if client == nil || client.Name() != "hg" {
		t.Fatalf("expected hg client, got %#v", client)
	}
}

func TestGitRepoClientSupportsDiffLogBranchesAndCommit(t *testing.T) {
	tmpDir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	runGit("init")
	runGit("config", "user.name", "Test User")
	runGit("config", "user.email", "test@example.com")

	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit("add", "README.md")
	runGit("commit", "-m", "initial")

	client, err := repo.NewClientForPath(tmpDir)
	if err != nil {
		t.Fatalf("NewClientForPath returned error: %v", err)
	}
	if diff, err := client.Diff(tmpDir, RepoDiffOptions{}); err != nil {
		t.Fatalf("Diff returned error: %v", err)
	} else if strings.Contains(diff, "diff --git") {
		t.Fatalf("expected empty diff, got %q", diff)
	}

	if err := os.WriteFile(readmePath, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("update README: %v", err)
	}
	if diff, err := client.Diff(tmpDir, RepoDiffOptions{}); err != nil {
		t.Fatalf("Diff after change returned error: %v", err)
	} else if !strings.Contains(diff, "README.md") {
		t.Fatalf("expected diff to mention README.md, got %q", diff)
	}

	if err := client.Commit(tmpDir, "second", RepoCommitOptions{All: true}); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}

	commits, err := client.Log(tmpDir, RepoLogOptions{MaxCount: 3})
	if err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	if len(commits) < 2 {
		t.Fatalf("expected at least two commits, got %d", len(commits))
	}

	branches, err := client.Branches(tmpDir)
	if err != nil {
		t.Fatalf("Branches returned error: %v", err)
	}
	if len(branches) == 0 {
		t.Fatalf("expected at least one branch")
	}
}
