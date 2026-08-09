package agent

import "github.com/sharedcode/sop/ai/agent/repo"

type RepoClient = repo.Client
type RepoStatusOptions = repo.StatusOptions
type RepoDiffOptions = repo.DiffOptions
type RepoLogOptions = repo.LogOptions
type RepoCommitOptions = repo.CommitOptions
type RepoPullOptions = repo.PullOptions
type RepoPushOptions = repo.PushOptions
type RepoChange = repo.Change
type RepoCommit = repo.Commit
type RepoBranch = repo.Branch

func newRepoClientForPath(path string) (RepoClient, error) {
	return repo.NewClientForPath(path)
}

func resolveRepoPath(path string) (string, error) {
	return repo.ResolvePath(path)
}
