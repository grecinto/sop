package agent

import (
	"time"
)

const (
	repoArtifactArchiveDays         = 90
	repoArtifactPruneDays           = 180
	repoArtifactArchivePressure     = 0.85
	repoArtifactForecastPressure    = 0.7
	repoArtifactPressureMinAge      = 30
	repoArtifactPressureArchiveDays = 60
)

// RepoArtifactRecord describes a generated repository artifact such as an AST or
// tree-sitter index/cache that should follow the same archive/prune lifecycle as
// repo-scoped LTM memory.
type RepoArtifactRecord struct {
	RepoID          string
	Kind            string
	Path            string
	CreatedAt       time.Time
	LastAccessedAt  time.Time
	UpdatedAt       time.Time
	ArchivedAt      time.Time
	LifecycleStatus string
	ArchiveReason   string
}

// EvaluateRepoArtifactLifecycle calculates whether a repo artifact should be
// archived, pruned, or left active based on age, access recency, and storage
// pressure. The contract mirrors the repo-scoped LTM retention policy.
func EvaluateRepoArtifactLifecycle(record RepoArtifactRecord, now time.Time, pressure float64) (archive bool, prune bool, next string, reason string) {
	if now.IsZero() {
		now = time.Now()
	}
	status := record.LifecycleStatus
	if status == "" {
		status = "active"
	}
	lastAccessAge := now.Sub(record.LastAccessedAt)
	age := now.Sub(record.CreatedAt)
	archiveAge := now.Sub(record.ArchivedAt)

	if stringsEqualFold(status, "archived") {
		if archiveAge >= repoArtifactPruneDays*24*time.Hour {
			return false, true, "pruned", "archive_age_exceeded"
		}
		return false, false, "archived", "retained_in_archive"
	}

	if pressure >= repoArtifactArchivePressure && lastAccessAge >= repoArtifactPressureArchiveDays*24*time.Hour && age >= repoArtifactPressureMinAge*24*time.Hour {
		return true, false, "archived", "storage_pressure_stale_repo_artifact"
	}
	if pressure >= repoArtifactForecastPressure && lastAccessAge >= repoArtifactArchiveDays*24*time.Hour && age >= repoArtifactArchiveDays*24*time.Hour {
		return true, false, "archived", "stale_repo_artifact"
	}
	if lastAccessAge >= repoArtifactArchiveDays*24*time.Hour && age >= repoArtifactArchiveDays*24*time.Hour {
		return true, false, "archived", "repo_artifact_stale"
	}
	if lastAccessAge >= repoArtifactPruneDays*24*time.Hour && archiveAge >= repoArtifactPruneDays*24*time.Hour {
		return false, true, "pruned", "artifact_archive_ttl_exceeded"
	}
	return false, false, "active", "recent_and_relevant"
}

// ArchiveStaleRepoArtifacts marks stale repo artifacts as archived, returning the
// updated slice and the number of records that were archived.
func ArchiveStaleRepoArtifacts(records []RepoArtifactRecord, now time.Time, pressure float64) ([]RepoArtifactRecord, int) {
	updated := make([]RepoArtifactRecord, 0, len(records))
	count := 0
	for i := range records {
		rec := records[i]
		archive, _, next, reason := EvaluateRepoArtifactLifecycle(rec, now, pressure)
		if archive {
			rec.LifecycleStatus = next
			rec.ArchiveReason = reason
			if rec.ArchivedAt.IsZero() {
				rec.ArchivedAt = now
			}
			count++
		}
		updated = append(updated, rec)
	}
	return updated, count
}

// PruneArchivedRepoArtifacts deletes archived repo artifacts once their archive
// TTL has expired. A pruned record is treated as final deletion and should be
// removed from the storage index.
func PruneArchivedRepoArtifacts(records []RepoArtifactRecord, now time.Time) ([]RepoArtifactRecord, int) {
	filtered := make([]RepoArtifactRecord, 0, len(records))
	count := 0
	for i := range records {
		rec := records[i]
		_, prune, _, _ := EvaluateRepoArtifactLifecycle(rec, now, 0)
		if prune {
			count++
			continue
		}
		filtered = append(filtered, rec)
	}
	return filtered, count
}

// TouchRepoArtifact marks the artifact as active again when it is accessed.
func TouchRepoArtifact(record *RepoArtifactRecord, now time.Time) {
	if record == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	record.LastAccessedAt = now
	record.UpdatedAt = now
	record.ArchiveReason = ""
	record.LifecycleStatus = "active"
}

func stringsEqualFold(a, b string) bool {
	return len(a) == len(b) && (a == b || (len(a) > 0 && (a == b || (len(a) == len(b) && (a == b)))))
}
