package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

func TestStoreContinueTeamPracticeWritesCanonicalTeamPatternsEntry(t *testing.T) {
	tmpDir := t.TempDir()
	sysDB := database.NewDatabase(sop.DatabaseOptions{StoresFolders: []string{tmpDir}, CacheType: sop.InMemory})
	agent := &CopilotAgent{
		systemDB: sysDB,
		Memory:   memory.NewMemoryUnit("continue-agent"),
		brain:    &MockGenerator{},
		service:  &Service{domain: &MockDomain{}},
	}

	ctx := context.Background()
	pattern, err := agent.StoreContinueTeamPractice(ctx, "Preserve public interfaces and add targeted tests")
	if err != nil {
		t.Fatalf("StoreContinueTeamPractice failed: %v", err)
	}
	if pattern == nil {
		t.Fatal("expected stored pattern")
	}
	if got := pattern.BuildCategoryPath(); got != "Team Patterns/Continue/Memory" {
		t.Fatalf("category path = %q, want %q", got, "Team Patterns/Continue/Memory")
	}

	readTx, err := sysDB.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("begin read transaction: %v", err)
	}
	defer readTx.Rollback(ctx)

	ltm, err := sysDB.OpenKnowledgeBase(ctx, agent.Memory.LongTermMemoryName(), readTx, nil, nil, false, true)
	if err != nil {
		t.Fatalf("open ltm: %v", err)
	}

	count, err := ltm.Store.Count(ctx)
	if err != nil {
		t.Fatalf("store count failed: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one stored team pattern item")
	}
	catsByPath, err := ltm.Store.CategoriesByPath(ctx)
	if err != nil {
		t.Fatalf("categories by path failed: %v", err)
	}
	found, err := catsByPath.Find(ctx, pattern.BuildCategoryPath(), false)
	if err != nil {
		t.Fatalf("category path lookup failed: %v", err)
	}
	if !found {
		t.Fatalf("expected category path %q to exist", pattern.BuildCategoryPath())
	}
}
