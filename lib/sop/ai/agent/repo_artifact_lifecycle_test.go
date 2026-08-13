package agent

import (
	"testing"
	"time"
)

func TestEvaluateRepoArtifactLifecycleArchivesStaleRepoArtifacts(t *testing.T) {
	now := time.Now()
	record := RepoArtifactRecord{
		RepoID:          "repo-42",
		Kind:            "tree-sitter",
		Path:            "/tmp/repo-42/ast-cache",
		CreatedAt:       now.Add(-120 * 24 * time.Hour),
		LastAccessedAt:  now.Add(-100 * 24 * time.Hour),
		UpdatedAt:       now.Add(-100 * 24 * time.Hour),
		LifecycleStatus: "active",
	}

	archive, prune, next, reason := EvaluateRepoArtifactLifecycle(record, now, 0.2)
	if !archive {
		t.Fatalf("expected stale repo artifact to be archived, archive=%v prune=%v next=%q reason=%q", archive, prune, next, reason)
	}
	if next != "archived" {
		t.Fatalf("expected archived status, got %q", next)
	}
}

func TestArchiveStaleRepoArtifactsPrunesExpiredArchives(t *testing.T) {
	now := time.Now()
	records := []RepoArtifactRecord{
		{
			RepoID:          "repo-42",
			Kind:            "tree-sitter",
			Path:            "/tmp/repo-42/ast-cache",
			CreatedAt:       now.Add(-260 * 24 * time.Hour),
			LastAccessedAt:  now.Add(-220 * 24 * time.Hour),
			UpdatedAt:       now.Add(-220 * 24 * time.Hour),
			ArchivedAt:      now.Add(-200 * 24 * time.Hour),
			LifecycleStatus: "archived",
		},
		{
			RepoID:          "repo-99",
			Kind:            "ast",
			Path:            "/tmp/repo-99/index",
			CreatedAt:       now.Add(-40 * 24 * time.Hour),
			LastAccessedAt:  now.Add(-10 * 24 * time.Hour),
			UpdatedAt:       now.Add(-10 * 24 * time.Hour),
			ArchivedAt:      now.Add(-6 * 24 * time.Hour),
			LifecycleStatus: "archived",
		},
	}

	updated, archivedCount := ArchiveStaleRepoArtifacts(records, now, 0.2)
	if archivedCount != 0 {
		t.Fatalf("expected only expired archive to prune, archivedCount=%d", archivedCount)
	}
	if len(updated) != 2 {
		t.Fatalf("expected unchanged records, got %d", len(updated))
	}

	pruned, prunedCount := PruneArchivedRepoArtifacts(updated, now)
	if prunedCount != 1 {
		t.Fatalf("expected exactly one archived record to be pruned, got %d", prunedCount)
	}
	if len(pruned) != 1 {
		t.Fatalf("expected one retained record after pruning, got %d", len(pruned))
	}
	if pruned[0].RepoID != "repo-99" {
		t.Fatalf("expected remaining repo to be repo-99, got %q", pruned[0].RepoID)
	}
}

func TestTouchRepoArtifactMarksRepoArtifactActive(t *testing.T) {
	now := time.Now()
	record := RepoArtifactRecord{
		RepoID:          "repo-7",
		Kind:            "ast",
		Path:            "/tmp/repo-7/ast-index",
		CreatedAt:       now.Add(-90 * 24 * time.Hour),
		LastAccessedAt:  now.Add(-80 * 24 * time.Hour),
		UpdatedAt:       now.Add(-80 * 24 * time.Hour),
		LifecycleStatus: "archived",
	}

	TouchRepoArtifact(&record, now)
	if record.LifecycleStatus != "active" {
		t.Fatalf("expected touched repo artifact to become active, got %q", record.LifecycleStatus)
	}
	if record.LastAccessedAt.IsZero() {
		t.Fatalf("expected last access time to be set")
	}
}
