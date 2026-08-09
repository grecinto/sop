package repo

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

type Client interface {
	Name() string
	Status(repoPath string, opts StatusOptions) ([]Change, error)
	Diff(repoPath string, opts DiffOptions) (string, error)
	Log(repoPath string, opts LogOptions) ([]Commit, error)
	Branches(repoPath string) ([]Branch, error)
	Checkout(repoPath, ref string) error
	Commit(repoPath, message string, opts CommitOptions) error
	Pull(repoPath string, opts PullOptions) error
	Push(repoPath string, opts PushOptions) error
}

type StatusOptions struct {
	Short  bool
	Staged bool
}

type DiffOptions struct {
	Staged bool
}

type LogOptions struct {
	MaxCount int
}

type CommitOptions struct {
	Files []string
	All   bool
}

type PullOptions struct {
	Rebase bool
}

type PushOptions struct {
	Remote string
	Branch string
}

type Change struct {
	Path   string
	Status string
	Staged bool
}

type Commit struct {
	Hash    string
	Message string
	Author  string
	Date    string
}

type Branch struct {
	Name   string
	Remote bool
}

type gitClient struct{}

func (g *gitClient) Name() string { return "git" }

func (g *gitClient) Status(repoPath string, opts StatusOptions) ([]Change, error) {
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
	changes := make([]Change, 0, len(status))
	for file, worktreeStatus := range status {
		staging := ""
		worktree := ""
		switch worktreeStatus.Staging {
		case git.Added:
			staging = "A"
		case git.Deleted:
			staging = "D"
		case git.Modified:
			staging = "M"
		case git.Renamed:
			staging = "R"
		case git.Copied:
			staging = "C"
		case git.Untracked:
			staging = "?"
		default:
			staging = ""
		}
		switch worktreeStatus.Worktree {
		case git.Added:
			worktree = "A"
		case git.Deleted:
			worktree = "D"
		case git.Modified:
			worktree = "M"
		case git.Renamed:
			worktree = "R"
		case git.Copied:
			worktree = "C"
		case git.Untracked:
			worktree = "?"
		default:
			worktree = ""
		}
		statusValue := strings.Trim(staging+":"+worktree, ":")
		if statusValue == "" {
			statusValue = "clean"
		}
		changes = append(changes, Change{Path: file, Status: statusValue, Staged: worktreeStatus.Staging != 0})
	}
	if opts.Short {
		return changes, nil
	}
	return changes, nil
}

func (g *gitClient) runGit(repoPath string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (g *gitClient) Diff(repoPath string, opts DiffOptions) (string, error) {
	args := []string{"--no-pager", "diff"}
	if opts.Staged {
		args = append(args, "--staged")
	}
	output, err := g.runGit(repoPath, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (g *gitClient) Log(repoPath string, opts LogOptions) ([]Commit, error) {
	args := []string{"--no-pager", "log", "--format=%H%x00%P%x00%an%x00%ad%x00%s", "--date=short"}
	if opts.MaxCount > 0 {
		args = append(args, fmt.Sprintf("-n%d", opts.MaxCount))
	}
	output, err := g.runGit(repoPath, args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	commits := make([]Commit, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\x00")
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, Commit{
			Hash:    parts[0],
			Message: strings.TrimSpace(parts[4]),
			Author:  parts[2],
			Date:    parts[3],
		})
	}
	return commits, nil
}

func (g *gitClient) Branches(repoPath string) ([]Branch, error) {
	output, err := g.runGit(repoPath, "branch", "--all")
	if err != nil {
		return nil, err
	}
	var branches []Branch
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		branchName := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		branchName = strings.TrimSpace(strings.TrimPrefix(branchName, "remotes/"))
		if branchName == "" {
			continue
		}
		branches = append(branches, Branch{Name: branchName, Remote: strings.HasPrefix(line, "remotes/")})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return branches, nil
}

func (g *gitClient) Checkout(repoPath, ref string) error {
	_, err := g.runGit(repoPath, "checkout", ref)
	return err
}

func (g *gitClient) Commit(repoPath, message string, opts CommitOptions) error {
	if len(opts.Files) > 0 {
		args := []string{"add"}
		args = append(args, opts.Files...)
		if _, err := g.runGit(repoPath, args...); err != nil {
			return err
		}
	} else if opts.All {
		if _, err := g.runGit(repoPath, "add", "-A"); err != nil {
			return err
		}
	}
	args := []string{"commit", "-m", message}
	_, err := g.runGit(repoPath, args...)
	return err
}

func (g *gitClient) Pull(repoPath string, opts PullOptions) error {
	args := []string{"pull"}
	if opts.Rebase {
		args = append(args, "--rebase")
	}
	_, err := g.runGit(repoPath, args...)
	return err
}

func (g *gitClient) Push(repoPath string, opts PushOptions) error {
	args := []string{"push"}
	if opts.Remote != "" {
		args = append(args, opts.Remote)
	}
	if opts.Branch != "" {
		args = append(args, opts.Branch)
	}
	_, err := g.runGit(repoPath, args...)
	return err
}

type svnClient struct{}

func (s *svnClient) Name() string { return "svn" }

func (s *svnClient) Status(repoPath string, opts StatusOptions) ([]Change, error) {
	return nil, fmt.Errorf("svn status not implemented yet")
}

func (s *svnClient) Diff(repoPath string, opts DiffOptions) (string, error) {
	return "", fmt.Errorf("svn diff not implemented yet")
}

func (s *svnClient) Log(repoPath string, opts LogOptions) ([]Commit, error) {
	return nil, fmt.Errorf("svn log not implemented yet")
}

func (s *svnClient) Branches(repoPath string) ([]Branch, error) {
	return nil, fmt.Errorf("svn branches not implemented yet")
}

func (s *svnClient) Checkout(repoPath, ref string) error {
	return fmt.Errorf("svn checkout not implemented yet")
}

func (s *svnClient) Commit(repoPath, message string, opts CommitOptions) error {
	return fmt.Errorf("svn commit not implemented yet")
}

func (s *svnClient) Pull(repoPath string, opts PullOptions) error {
	return fmt.Errorf("svn pull not implemented yet")
}

func (s *svnClient) Push(repoPath string, opts PushOptions) error {
	return fmt.Errorf("svn push not implemented yet")
}

type hgClient struct{}

func (h *hgClient) Name() string { return "hg" }

func (h *hgClient) Status(repoPath string, opts StatusOptions) ([]Change, error) {
	return nil, fmt.Errorf("hg status not implemented yet")
}

func (h *hgClient) Diff(repoPath string, opts DiffOptions) (string, error) {
	return "", fmt.Errorf("hg diff not implemented yet")
}

func (h *hgClient) Log(repoPath string, opts LogOptions) ([]Commit, error) {
	return nil, fmt.Errorf("hg log not implemented yet")
}

func (h *hgClient) Branches(repoPath string) ([]Branch, error) {
	return nil, fmt.Errorf("hg branches not implemented yet")
}

func (h *hgClient) Checkout(repoPath, ref string) error {
	return fmt.Errorf("hg checkout not implemented yet")
}

func (h *hgClient) Commit(repoPath, message string, opts CommitOptions) error {
	return fmt.Errorf("hg commit not implemented yet")
}

func (h *hgClient) Pull(repoPath string, opts PullOptions) error {
	return fmt.Errorf("hg pull not implemented yet")
}

func (h *hgClient) Push(repoPath string, opts PushOptions) error {
	return fmt.Errorf("hg push not implemented yet")
}

func NewClientForPath(path string) (Client, error) {
	candidate := path
	for {
		if info, err := os.Stat(filepath.Join(candidate, ".git")); err == nil && info.IsDir() {
			return &gitClient{}, nil
		}
		if info, err := os.Stat(filepath.Join(candidate, ".svn")); err == nil && info.IsDir() {
			return &svnClient{}, nil
		}
		if info, err := os.Stat(filepath.Join(candidate, ".hg")); err == nil && info.IsDir() {
			return &hgClient{}, nil
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
		candidate = parent
	}
	return nil, fmt.Errorf("no supported repository found for %s", path)
}

func ResolvePath(path string) (string, error) {
	candidate := path
	for {
		if info, err := os.Stat(filepath.Join(candidate, ".git")); err == nil && info.IsDir() {
			return candidate, nil
		}
		if info, err := os.Stat(filepath.Join(candidate, ".svn")); err == nil && info.IsDir() {
			return candidate, nil
		}
		if info, err := os.Stat(filepath.Join(candidate, ".hg")); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
		candidate = parent
	}
	return "", fmt.Errorf("no supported repository found for %s", path)
}
