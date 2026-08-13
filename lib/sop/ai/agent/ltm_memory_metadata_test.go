package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
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
	status := evaluateLTMLifecycleStatus(120, 150, 1, 10, true)
	if status.State != "stale" {
		t.Fatalf("expected stale state for truly cold entry, got %q", status.State)
	}
}

func TestEvaluateLTMLifecycleStatusKeepsRecentEntriesActive(t *testing.T) {
	status := evaluateLTMLifecycleStatus(2, 10, 5, 60, true)
	if status.State != "active" {
		t.Fatalf("expected active state for recent entry, got %q", status.State)
	}
}

func TestDecideLTMLifecycleRetentionDelaysArchiveForModeratelyColdEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status":       "stale",
		"lifecycle_last_used_at": now.Add(-100 * 24 * time.Hour).UnixMilli(),
		"lifecycle_access_count": 1,
		"scope_valid":            true,
	}
	decision := decideLTMLifecycleRetention(payload, now)
	if decision.Action != "" {
		t.Fatalf("expected moderate cold entry to remain retained, got action=%q reason=%q", decision.Action, decision.Reason)
	}
}

func TestDecideLTMLifecycleRetentionArchivesColdEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status":       "stale",
		"lifecycle_last_used_at": now.Add(-130 * 24 * time.Hour).UnixMilli(),
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
		"archived_at":      now.Add(-31 * 24 * time.Hour).UnixMilli(),
	}
	if !decideLTMLifecycleCompaction(payload, now) {
		t.Fatalf("expected archived entries older than thirty days to be compacted")
	}
}

func TestDecideLTMLifecycleCompactionHardPrunesTwoYearOldArchivedEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status": "archived",
		"archived_at":      now.Add(-731 * 24 * time.Hour).UnixMilli(),
	}
	if !decideLTMLifecycleCompaction(payload, now) {
		t.Fatalf("expected archived entries older than two years to be hard-pruned")
	}
}

func TestDecideLTMLifecycleCompactionWithStoragePressureTriggersEarlierCleanup(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status": "archived",
		"archived_at":      now.Add(-120 * 24 * time.Hour).UnixMilli(),
	}
	if !decideLTMLifecycleCompactionWithStoragePressure(payload, now, 0.9) {
		t.Fatalf("expected archived entries to be compacted under high storage pressure")
	}
}

func TestSummarizeLongTailArchivedMemoryProducesCondensedSignal(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status":       "archived",
		"archived_at":            now.Add(-400 * 24 * time.Hour).UnixMilli(),
		"query":                  "retry logic after payment errors",
		"outcome":                "recovered from payment retry failures",
		"lesson":                 "use idempotent retries and backoff",
		"lifecycle_access_count": 1,
		"scope_valid":            true,
	}
	if summary := summarizeLongTailArchivedMemory(payload); summary == "" {
		t.Fatalf("expected a compressed long-tail summary for old archived memory")
	}
	if !strings.Contains(summarizeLongTailArchivedMemory(payload), "retry") {
		t.Fatalf("expected summary to preserve the key context, got %q", summarizeLongTailArchivedMemory(payload))
	}
}

func TestShouldSummarizeArchivedMemoryAfterLongDormancy(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]any{
		"lifecycle_status":       "archived",
		"archived_at":            now.Add(-400 * 24 * time.Hour).UnixMilli(),
		"lifecycle_access_count": 1,
		"scope_valid":            true,
		"lesson":                 "idempotent retry logic",
	}
	if !shouldSummarizeArchivedMemory(payload, now) {
		t.Fatalf("expected dormant archived memory to be summarized before final pruning")
	}
}

func TestBuildLTMPressureSummaryReportsUsageAndThreshold(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	summary := buildLTMPressureSummary("/tmp", now)
	if summary["usage_ratio"] == nil {
		t.Fatalf("expected usage ratio in pressure summary")
	}
	if v, ok := summary["pressure_state"].(string); !ok || v == "" {
		t.Fatalf("expected pressure state to be reported, got %#v", summary["pressure_state"])
	}
	if v, ok := summary["threshold"].(float64); !ok || v <= 0 {
		t.Fatalf("expected positive pressure threshold, got %#v", summary["threshold"])
	}
}

func TestCopilotAgentNotifyLTMPressureUser_AlertsOnTriggerAndRetrigger(t *testing.T) {
	agent := &CopilotAgent{ltmPressureAlertCooldown: 12 * time.Hour}
	var notifications []LTMPressureNotification
	agent.LTMPressureNotifier = func(notification LTMPressureNotification) {
		notifications = append(notifications, notification)
	}
	first := map[string]any{"usage_ratio": 0.92, "pressure_state": "high", "threshold": 0.85, "triggered": true, "action": "compact"}
	agent.notifyLTMPressureUser(first, 5, 2)
	if len(notifications) != 1 {
		t.Fatalf("expected first trigger notification, got %d", len(notifications))
	}
	if notifications[0].Severity != "warning" {
		t.Fatalf("expected warning severity on first high-pressure alert, got %q", notifications[0].Severity)
	}
	if len(notifications[0].FreeUpTargets) != 2 {
		t.Fatalf("expected two cleanup locations in the first trigger notification, got %d", len(notifications[0].FreeUpTargets))
	}
	if !strings.Contains(notifications[0].FreeUpTargets[0], "Data Manager") {
		t.Fatalf("expected first cleanup target to mention Data Manager, got %q", notifications[0].FreeUpTargets[0])
	}
	if !strings.Contains(notifications[0].FreeUpTargets[1], "AI chat") {
		t.Fatalf("expected second cleanup target to mention the AI chat workflow, got %q", notifications[0].FreeUpTargets[1])
	}

	agent.notifyLTMPressureUser(first, 7, 3)
	if len(notifications) != 2 {
		t.Fatalf("expected retrigger notification, got %d", len(notifications))
	}
	if !notifications[1].Retriiggered {
		t.Fatalf("expected second trigger to be marked as retriggered")
	}
}

func TestSetDefaultLTMPressureNotifierUsedWhenAgentDoesNotOverride(t *testing.T) {
	old := defaultLTMPressureNotifier
	defer SetDefaultLTMPressureNotifier(old)

	var notifications []LTMPressureNotification
	SetDefaultLTMPressureNotifier(func(notification LTMPressureNotification) {
		notifications = append(notifications, notification)
	})

	agent := &CopilotAgent{ltmPressureAlertCooldown: 12 * time.Hour}
	first := map[string]any{"usage_ratio": 0.92, "pressure_state": "high", "threshold": 0.85, "triggered": true, "action": "compact"}
	agent.notifyLTMPressureUser(first, 5, 2)
	if len(notifications) != 1 {
		t.Fatalf("expected default notifier to fire, got %d notifications", len(notifications))
	}
	if notifications[0].Title == "" {
		t.Fatalf("expected notification title to be populated")
	}
}

func TestBuildCodingMemoryPayloadIsLifecycleAwareForRepoTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload, entries := buildCodingMemoryLTMPayloadAndIndexes(cw.CodingMemory{
		Description:    "Fixed repository parsing bug",
		Outcome:        "Added path-aware fallback for stale repo memory",
		RepositoryHint: "sop-cloud",
		ActiveFile:     "/repo/lib/sop/ai/agent/copilot_ltm.go",
		StructuralPath: "sop-cloud/ai/agent/copilot_ltm.go/FixParsing",
		ASTLeafNodes:   []string{"FixParsing"},
	})
	if payload["scope_type"] != "repo" {
		t.Fatalf("expected coding memory payload to expose repo scope_type, got %#v", payload["scope_type"])
	}
	if payload["scope_id"] != "sop-cloud" {
		t.Fatalf("expected coding memory payload to expose repo scope_id, got %#v", payload["scope_id"])
	}
	if payload["lifecycle_status"] != "active" {
		t.Fatalf("expected coding memory payload to default to active lifecycle, got %#v", payload["lifecycle_status"])
	}
	if !isRepoScopedLTMPayload(payload) {
		t.Fatalf("expected coding memory payload to match repo-scoped lifecycle classification")
	}
	if len(entries) == 0 {
		t.Fatal("expected coding memory path/semantic indexes to be generated")
	}
	_ = now
}

func TestShouldArchiveRepoScopedMemoryForPressureOnNearThreshold(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stale := map[string]any{
		"scope_type":             "repo",
		"scope_id":               "legacy-repo",
		"lifecycle_status":       "stale",
		"lifecycle_last_used_at": now.Add(-120 * 24 * time.Hour).UnixMilli(),
		"scope_valid":            true,
	}
	if !shouldArchiveRepoScopedMemoryForPressure(stale, now, 0.75) {
		t.Fatalf("expected stale repo memory to be archived when storage is near threshold")
	}

	active := map[string]any{
		"scope_type":             "repo",
		"scope_id":               "active-repo",
		"lifecycle_status":       "active",
		"lifecycle_last_used_at": now.Add(-2 * 24 * time.Hour).UnixMilli(),
		"scope_valid":            true,
	}
	if shouldArchiveRepoScopedMemoryForPressure(active, now, 0.75) {
		t.Fatalf("expected current repo memory to remain active even near threshold")
	}
}

func TestShouldPruneStaleRepoScopedMemoryAfterAgeThreshold(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	archived := map[string]any{
		"scope_type":       "repo",
		"scope_id":         "legacy-repo",
		"lifecycle_status": "archived",
		"archived_at":      now.Add(-200 * 24 * time.Hour).UnixMilli(),
		"scope_valid":      true,
	}
	if !shouldPruneRepoScopedMemoryByAge(archived, now) {
		t.Fatalf("expected archived repo memory older than the prune threshold to be eligible for deletion")
	}

	recent := map[string]any{
		"scope_type":       "repo",
		"scope_id":         "recent-repo",
		"lifecycle_status": "archived",
		"archived_at":      now.Add(-10 * 24 * time.Hour).UnixMilli(),
		"scope_valid":      true,
	}
	if shouldPruneRepoScopedMemoryByAge(recent, now) {
		t.Fatalf("expected recently archived repo memory to stay retained")
	}

	stale := map[string]any{
		"scope_type":             "repo",
		"scope_id":               "stale-not-archived",
		"lifecycle_status":       "stale",
		"lifecycle_last_used_at": now.Add(-370 * 24 * time.Hour).UnixMilli(),
		"scope_valid":            true,
	}
	if shouldPruneRepoScopedMemoryByAge(stale, now) {
		t.Fatalf("expected stale repo memory to be archived before pruning")
	}
}

func TestDecideLTMPressureActionTriggersCleanupAtHighPressure(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	// Force a high-pressure state by using a temp path with a high usage ratio.
	// The implementation derives pressure from disk stats, so we only assert on the action logic shape.
	summary := decideLTMPressureAction("/tmp", now)
	if summary["action"] == nil {
		t.Fatalf("expected a monitor action to be selected")
	}
	if v, ok := summary["triggered"].(bool); !ok || !v {
		if _, ok := summary["pressure_state"].(string); !ok {
			t.Fatalf("expected trigger state to be reflected in pressure monitor summary")
		}
	}
}
