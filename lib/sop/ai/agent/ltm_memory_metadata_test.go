package agent

import (
	"testing"
	"time"

	"github.com/sharedcode/sop/ai"
)

func TestBuildLTMMemoryCategoryPathUsesCategoryAndScope(t *testing.T) {
	path := buildLTMMemoryCategoryPath("coding", "coding_workflow", "repo", "sop-cloud")
	want := "Coding/Repo/sop-cloud"
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestBuildLTMMemoryMetadataAddsCapabilityAndScopeFields(t *testing.T) {
	meta := buildLTMMemoryMetadata("research", "research_workflow", "space", "legal-space")
	if meta["category"] != "research" {
		t.Fatalf("expected category metadata, got %#v", meta["category"])
	}
	if meta["capability"] != "research_workflow" {
		t.Fatalf("expected capability metadata, got %#v", meta["capability"])
	}
	if meta["scope_type"] != "space" {
		t.Fatalf("expected scope_type metadata, got %#v", meta["scope_type"])
	}
	if meta["scope_id"] != "legal-space" {
		t.Fatalf("expected scope_id metadata, got %#v", meta["scope_id"])
	}
	if meta["provenance"] != "agent" {
		t.Fatalf("expected provenance metadata, got %#v", meta["provenance"])
	}
}

func TestBuildLTMMemoryMetadataPersistsLifecycleStateFields(t *testing.T) {
	meta := buildLTMMemoryMetadata("coding", "coding_workflow", "repo", "sop-cloud")
	if meta["lifecycle_status"] != "active" {
		t.Fatalf("expected lifecycle_status metadata, got %#v", meta["lifecycle_status"])
	}
	if meta["lifecycle_access_count"] != 1 {
		t.Fatalf("expected lifecycle_access_count metadata to be 1, got %#v", meta["lifecycle_access_count"])
	}
	if meta["lifecycle_last_used_at"] == nil {
		t.Fatalf("expected lifecycle_last_used_at metadata to be present")
	}
}

func TestTouchLTMLifecycleForRepoAccessMarksRepoActive(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"scope_type":             "repo",
		"scope_id":               "sop-cloud",
		"lifecycle_status":       "stale",
		"status":                 "stale",
		"scope_valid":            false,
		"lifecycle_access_count": 1,
		"lifecycle_last_used_at": now.Add(-100 * 24 * time.Hour).UnixMilli(),
	}

	touched := touchLTMLifecycleForRepoAccess(payload, now)
	if got, ok := touched["lifecycle_status"].(string); !ok || got != "active" {
		t.Fatalf("expected lifecycle_status to be active, got %#v", touched["lifecycle_status"])
	}
	if got, ok := touched["status"].(string); !ok || got != "active" {
		t.Fatalf("expected status to be active, got %#v", touched["status"])
	}
	if got := touched["lifecycle_last_used_at"]; got != now.UnixMilli() {
		t.Fatalf("expected lifecycle_last_used_at to be refreshed, got %#v", got)
	}
	if got := touched["lifecycle_access_count"]; got != 2 {
		t.Fatalf("expected lifecycle_access_count to increment, got %#v", got)
	}
	if got := touched["scope_valid"]; got != true {
		t.Fatalf("expected scope_valid to be re-enabled, got %#v", got)
	}
}

func TestBuildLTMRetrievalTargetsPrefersResearchSpaceAndRepoScope(t *testing.T) {
	targets := buildLTMRetrievalTargets("fix parser", &ai.SessionPayload{SelectedKBs: []ai.ArtifactReference{{Name: "legal-space", Type: ai.ArtifactTypeSpace}}}, "sop-cloud")
	if len(targets) < 2 {
		t.Fatalf("expected multiple retrieval targets, got %d", len(targets))
	}
	if targets[0].Category != "research" {
		t.Fatalf("expected first target to be research scoped, got %#v", targets[0])
	}
	if targets[0].ScopeType != "space" || targets[0].ScopeID != "legal-space" {
		t.Fatalf("expected first target to use selected space scope, got %#v", targets[0])
	}
	if targets[1].Category != "coding" || targets[1].ScopeType != "repo" || targets[1].ScopeID != "sop-cloud" {
		t.Fatalf("expected second target to use repo scope, got %#v", targets[1])
	}
}

func TestBuildLTMRetrievalTargetsKeepsRepoAndSpaceScopesSeparate(t *testing.T) {
	targets := buildLTMRetrievalTargets("debug login for repo sop-cloud", &ai.SessionPayload{SelectedKBs: []ai.ArtifactReference{{Name: "finance-space", Type: ai.ArtifactTypeSpace}}}, "sop-cloud")
	if len(targets) != 2 {
		t.Fatalf("expected repo and space targets only, got %d", len(targets))
	}
	if targets[0].ScopeType != "space" || targets[0].ScopeID != "finance-space" {
		t.Fatalf("expected first target to preserve selected space scope, got %#v", targets[0])
	}
	if targets[1].ScopeType != "repo" || targets[1].ScopeID != "sop-cloud" {
		t.Fatalf("expected second target to preserve repo scope, got %#v", targets[1])
	}
}

func TestDetectLTMLifecycleSignalParsesRepoRetirementCue(t *testing.T) {
	signal := detectLTMLifecycleSignal("done with repo sop-cloud")
	if signal.Action != "retire" {
		t.Fatalf("expected retire action, got %q", signal.Action)
	}
	if signal.ScopeType != "repo" || signal.ScopeID != "sop-cloud" {
		t.Fatalf("expected repo scope signal, got %#v", signal)
	}
}

func TestDetectLTMLifecycleSignalParsesRepoReactivationCue(t *testing.T) {
	signal := detectLTMLifecycleSignal("continue with repo sop-cloud")
	if signal.Action != "reactivate" {
		t.Fatalf("expected reactivate action, got %q", signal.Action)
	}
	if signal.ScopeType != "repo" || signal.ScopeID != "sop-cloud" {
		t.Fatalf("expected repo scope signal, got %#v", signal)
	}
}

func TestFilterLTMRetrievalTargetsRemovesRetiredScope(t *testing.T) {
	targets := []ltmRetrievalTarget{{Category: "coding", Capability: "coding_workflow", ScopeType: "repo", ScopeID: "sop-cloud"}, {Category: "general", Capability: "general", ScopeType: "global", ScopeID: "global"}}
	filtered := filterLTMRetrievalTargets(targets, ltmLifecycleSignal{Action: "retire", ScopeType: "repo", ScopeID: "sop-cloud"})
	if len(filtered) != 1 {
		t.Fatalf("expected retired repo target to be removed, got %#v", filtered)
	}
	if filtered[0].ScopeType != "global" {
		t.Fatalf("expected general fallback to remain, got %#v", filtered[0])
	}
}

func TestEvaluateLTMLifecycleStatusTagsColdEntriesAsStale(t *testing.T) {
	status := evaluateLTMLifecycleStatus(90, 100, 1, 10, true)
	if status.State != "stale" {
		t.Fatalf("expected stale state for cold entry, got %q", status.State)
	}
}

func TestEvaluateLTMLifecycleStatusKeepsRecentEntriesActive(t *testing.T) {
	status := evaluateLTMLifecycleStatus(2, 10, 5, 60, true)
	if status.State != "active" {
		t.Fatalf("expected active state for recent entry, got %q", status.State)
	}
}

func TestDecideLTMLifecycleRetentionArchivesColdEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status":       "stale",
		"lifecycle_last_used_at": now.Add(-120 * 24 * time.Hour).UnixMilli(),
		"lifecycle_access_count": 1,
		"scope_valid":            true,
	}
	decision := decideLTMLifecycleRetention(payload, now)
	if decision.Action != "archive" {
		t.Fatalf("expected archive action for stale entry, got %q", decision.Action)
	}
	if decision.Reason == "" {
		t.Fatalf("expected a retention reason to be populated")
	}
}

func TestDecideLTMLifecycleCompactionPrunesArchivedEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status": "archived",
		"archived_at":      now.Add(-8 * 24 * time.Hour).UnixMilli(),
	}
	if !decideLTMLifecycleCompaction(payload, now) {
		t.Fatalf("expected archived entries older than seven days to be compacted")
	}
}
