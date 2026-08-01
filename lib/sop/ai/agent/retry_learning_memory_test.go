package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
)

func TestPersistAndLoadRetryLearnings_RoundTripsScopedAndGlobal(t *testing.T) {
	ctx := context.Background()
	db := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	payload := &ai.SessionPayload{UserID: "user-42", AgentID: ai.AgentIDOmni, ActiveDomain: "coding_workflow", ActiveRepo: "sop-cloud"}
	learnings := []ai.RetryLearning{{
		ToolName:          "execute_script",
		ErrorPattern:      "execute_script:join mapping unresolved",
		PreferredStrategy: ai.RepairStrategyResearchFirst,
		SuccessCount:      3,
		FailureCount:      0,
	}}
	if err := persistRetryLearnings(ctx, db, payload, "chatgpt", "gpt-5.4", learnings); err != nil {
		t.Fatalf("persistRetryLearnings() error = %v", err)
	}
	loaded, err := loadPersistentRetryLearnings(ctx, db, payload, "chatgpt", "gpt-5.4")
	if err != nil {
		t.Fatalf("loadPersistentRetryLearnings() error = %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("expected retry learnings to round-trip")
	}
	if loaded[0].RepoScope != "sop-cloud" || loaded[0].DomainScope != "coding_workflow" {
		t.Fatalf("expected scoped retry learning metadata, got %+v", loaded[0])
	}
	globalPayload := &ai.SessionPayload{UserID: "user-42", AgentID: ai.AgentIDOmni}
	globalLoaded, err := loadPersistentRetryLearnings(ctx, db, globalPayload, "chatgpt", "gpt-5.4")
	if err != nil {
		t.Fatalf("loadPersistentRetryLearnings() global error = %v", err)
	}
	if len(globalLoaded) == 0 {
		t.Fatal("expected global fallback retry learnings to load")
	}
}

func TestHydrateRetryLearningsIntoCarryover_PopulatesMissingCarryover(t *testing.T) {
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{UserID: "user-42", AgentID: ai.AgentIDOmni, ActiveDomain: "coding_workflow", ActiveRepo: "sop-cloud"})
	db := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})
	payload := ai.GetSessionPayload(ctx)
	learnings := []ai.RetryLearning{{
		ToolName:          "execute_script",
		ErrorPattern:      "execute_script:join mapping unresolved",
		PreferredStrategy: ai.RepairStrategyResearchFirst,
		SuccessCount:      3,
	}}
	if err := persistRetryLearnings(ctx, db, payload, "chatgpt", "gpt-5.4", learnings); err != nil {
		t.Fatalf("persistRetryLearnings() error = %v", err)
	}
	gen := capabilityOnlyGenerator{provider: "chatgpt", model: "gpt-5.4", supportsCompact: true, supportsLive: true}
	carryover := hydrateRetryLearningsIntoCarryover(ctx, db, payload, gen, nil)
	if carryover == nil || len(carryover.RetryLearnings) != 1 {
		t.Fatalf("expected carryover retry learnings to be hydrated, got %+v", carryover)
	}
}
