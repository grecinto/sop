package codingworkflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// GitClient exposes repository operations that can be implemented either by the local
// automation tool layer or by a lightweight in-process adapter.
type GitClient interface {
	Status(repoPath string) ([]string, error)
	Diff(repoPath string) (string, error)
	Commit(repoPath, message string) error
}

// GitRepoInspector uses the workflow's git client to inspect repository changes.
// Guard rail: use repository inspection adapters for read-only discovery so future
// copilots can reason about diffs and changed files without mutating workspace state.
type GitRepoInspector struct {
	Client GitClient
}

func resolveRepoPath(start string) (string, error) {
	if strings.TrimSpace(start) == "" {
		return "", fmt.Errorf("working directory is required")
	}
	current := start
	for {
		_, err := git.PlainOpen(current)
		if err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no git repository found for %q", start)
		}
		current = parent
	}
}

func (g *GitRepoInspector) ListChangedFiles(task Task, state RunState) ([]string, error) {
	_ = state
	repoPath, err := resolveRepoPath(task.WorkingDir)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}
	if g != nil && g.Client != nil {
		return g.Client.Status(repoPath)
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("open worktree: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	files := make([]string, 0, len(status))
	for file := range status {
		files = append(files, file)
	}
	return files, nil
}

// DefaultDiffPreviewer builds a simple unified diff preview using go-diff.
// Guard rail: expose previews before applying changes so the workflow can explain what
// will change and ask for approval when the operation is not obviously safe.
type DefaultDiffPreviewer struct{}

func (d *DefaultDiffPreviewer) Preview(task Task, patch *Patch, state RunState) (string, error) {
	_ = state
	if patch == nil || patch.FilePath == "" {
		return "", fmt.Errorf("patch is required")
	}
	fullPath := filepath.Join(task.WorkingDir, patch.FilePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read target file: %w", err)
	}
	oldText := string(content)
	newText := strings.Replace(oldText, patch.OldContent, patch.NewContent, 1)
	if patch.OldContent == "" {
		newText = patch.NewContent
	}
	dmp := diffmatchpatch.New()
	patches := dmp.PatchMake(oldText, newText)
	preview := dmp.PatchToText(patches)
	if preview == "" {
		preview = fmt.Sprintf("--- old\n+++ new\n-%s\n+%s\n", oldText, newText)
	}
	return preview, nil
}

// CommitSummary is a lightweight wrapper around the workflow's git client.
type RepoInspectionSummary struct {
	ChangedFiles []string
	DiffPreview  string
}

type CommitSummary struct {
	Message string
	Client  GitClient
}

func (g *GitRepoInspector) Inspect(task Task, state RunState) (RepoInspectionSummary, error) {
	_ = state
	changedFiles, err := g.ListChangedFiles(task, state)
	if err != nil {
		return RepoInspectionSummary{}, err
	}
	preview := ""
	if len(changedFiles) > 0 {
		preview = fmt.Sprintf("changed files: %s", strings.Join(changedFiles, ", "))
	}
	return RepoInspectionSummary{ChangedFiles: changedFiles, DiffPreview: preview}, nil
}

func (c *CommitSummary) CreateCommit(task Task, state RunState) error {
	_ = state
	repoPath, err := resolveRepoPath(task.WorkingDir)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	if c != nil && c.Client != nil {
		return c.Client.Commit(repoPath, c.Message)
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("open worktree: %w", err)
	}
	if _, err := wt.Add("."); err != nil {
		return fmt.Errorf("add changes: %w", err)
	}
	if _, err := wt.Commit(c.Message, &git.CommitOptions{Author: &object.Signature{Name: "sop-coding-workflow", Email: "sop@example.com", When: time.Now()}}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
