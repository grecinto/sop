package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/common/mocks"
	sopfs "github.com/sharedcode/sop/fs"
)

type controlledEmbedder struct {
	failAfterCalls int
	calls          int
	err            error
}

func (e *controlledEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if e.failAfterCalls > 0 && e.calls >= e.failAfterCalls {
		return nil, e.err
	}

	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = []float32{float32(i + 1), 0.5, 1.0}
	}
	return vecs, nil
}

func (e *controlledEmbedder) Dim() int     { return 3 }
func (e *controlledEmbedder) Name() string { return "controlled-embedder" }

func TestRealIntegrationKBHarness_FullLifecycle(t *testing.T) {
	ctx := context.Background()
	baseDir := filepath.Join(t.TempDir(), "kb-harness")
	activeDir := filepath.Join(baseDir, "active")
	passiveDir := filepath.Join(baseDir, "passive")
	for _, dir := range []string{activeDir, passiveDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	previousGlobalReplicationDetails := sopfs.GlobalReplicationDetails
	sopfs.GlobalReplicationDetails = nil
	t.Cleanup(func() {
		sopfs.GlobalReplicationDetails = previousGlobalReplicationDetails
		sopfs.SetRecoveryStateCleanupPath("")
		sopfs.SetRecoveryStateConfigUpdater(nil)
	})

	stateDir := filepath.Join(baseDir, "recovery-state")
	policy := newRuntimeVectorizationFailurePolicy(stateDir).(runtimeVectorizationFailurePolicyHandle)
	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy
	unregisterPolicy := aidb.RegisterVectorizationFailurePolicy(policy)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previousPolicy
		unregisterPolicy()
	})

	db := aidb.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{activeDir}})
	kbName := "kb-harness"
	catID := sop.NewUUID()

	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin write tx: %v", err)
	}
	kb, err := db.OpenKnowledgeBase(ctx, kbName, tx, nil, &controlledEmbedder{}, false)
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg == nil {
		cfg = &memory.KnowledgeBaseConfig{}
	}
	cfg.Description = "knowledge for integration harness"
	cfg.SystemPrompt = "integration harness"
	if err := kb.SetConfig(ctx, cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}

	cats, err := kb.Store.Categories(ctx)
	if err != nil {
		t.Fatalf("categories: %v", err)
	}
	cat := &memory.Category{ID: catID, Name: "Harness Category", Description: "Category used for integration harness"}
	cats.Add(ctx, catID, cat)

	catsByPath, err := kb.Store.CategoriesByPath(ctx)
	if err != nil {
		t.Fatalf("categories by path: %v", err)
	}
	catsByPath.Add(ctx, cat.Name, catID)

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	for i := 0; i < 3; i++ {
		itemID := sop.NewUUID()
		item := memory.Item[map[string]any]{ID: itemID, CategoryID: catID, Summaries: []string{fmt.Sprintf("knowledge item %d", i)}}
		itemsTree.Add(ctx, memory.ItemKey{CategoryID: catID, ItemID: itemID}, item)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit initial kb setup: %v", err)
	}

	// Seed a domain reference so the vectorization pipeline reaches the category/item loops,
	// which are the call sites that hand failures to the runtime policy.
	txSeed, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin config seed tx: %v", err)
	}
	kbSeed, err := db.OpenKnowledgeBase(ctx, kbName, txSeed, nil, &controlledEmbedder{}, false)
	if err != nil {
		t.Fatalf("open kb for config seed: %v", err)
	}
	cfgSeed, err := kbSeed.GetConfig(ctx)
	if err != nil {
		t.Fatalf("get config for seed: %v", err)
	}
	if cfgSeed == nil {
		cfgSeed = &memory.KnowledgeBaseConfig{}
	}
	cfgSeed.DomainReference = []float32{0.1, 0.2, 0.3}
	if err := kbSeed.SetConfig(ctx, cfgSeed); err != nil {
		t.Fatalf("set config for seed: %v", err)
	}
	if err := txSeed.Commit(ctx); err != nil {
		t.Fatalf("commit config seed: %v", err)
	}

	failingEmbedder := &controlledEmbedder{failAfterCalls: 1, err: errors.New("simulated vectorization failure")}
	if err := db.Vectorize(ctx, kbName, nil, failingEmbedder, 2); err != nil {
		t.Fatalf("vectorize with injected failure should be tolerated by the policy, got %v", err)
	}

	recoveryState := policy.RecoveryState()
	if recoveryState.Count == 0 {
		t.Fatalf("expected runtime vectorization policy to record a failure, got %+v", recoveryState)
	}

	previousRecoveryExecutor := vectorizationRecoveryExecutor
	var recoveryCalls int
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		recoveryCalls++
		return db.Vectorize(ctx, kbName, nil, &controlledEmbedder{}, 2)
	}
	t.Cleanup(func() {
		vectorizationRecoveryExecutor = previousRecoveryExecutor
	})

	if got := retryPendingVectorizationRecovery(ctx, true); got < 1 {
		t.Fatalf("expected at least one background recovery retry, got %d", got)
	}
	if recoveryCalls < 1 {
		t.Fatalf("expected vectorization recovery executor to run at least once, got %d", recoveryCalls)
	}

	sop.ReportStoragePathFailure("write", activeDir, syscall.EROFS)
	if storageHealthAllowsHigherLevelAutoRetry() {
		t.Fatal("expected storage-path health policy to block higher-level auto-retry while the path is unhealthy")
	}

	cache := mocks.NewMockClient()
	tracker, err := sopfs.NewReplicationTracker(ctx, []string{activeDir, passiveDir}, true, cache)
	if err != nil {
		t.Fatalf("new replication tracker: %v", err)
	}
	tracker.HandleReplicationRelatedError(ctx, sop.Error{Code: sop.FailoverQualifiedError + 1, Err: syscall.EROFS}, nil, false)
	if !tracker.FailedToReplicate {
		t.Fatal("expected tracker to enter failed-to-replicate state after failover-qualified error")
	}
	if err := tracker.ReinstateFailedDrives(ctx); err != nil {
		t.Fatalf("reinstate after failover returned unexpected error: %v", err)
	}

	newItemID := sop.NewUUID()
	tx2, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("begin follow-up write tx: %v", err)
	}
	kb2, err := db.OpenKnowledgeBase(ctx, kbName, tx2, nil, &controlledEmbedder{}, false)
	if err != nil {
		t.Fatalf("open kb for follow-up write: %v", err)
	}
	itemsTree2, err := kb2.Store.Items(ctx)
	if err != nil {
		t.Fatalf("open items tree for follow-up write: %v", err)
	}
	itemsTree2.Add(ctx, memory.ItemKey{CategoryID: catID, ItemID: newItemID}, memory.Item[map[string]any]{ID: newItemID, CategoryID: catID, Summaries: []string{"recovered item after failover"}})
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit follow-up write: %v", err)
	}

	tx3, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("begin follow-up read tx: %v", err)
	}
	defer tx3.Commit(ctx)
	kb3, err := db.OpenKnowledgeBase(ctx, kbName, tx3, nil, &controlledEmbedder{}, false)
	if err != nil {
		t.Fatalf("open kb for follow-up read: %v", err)
	}
	itemsTree3, err := kb3.Store.Items(ctx)
	if err != nil {
		t.Fatalf("open items tree for follow-up read: %v", err)
	}
	found, err := itemsTree3.Find(ctx, memory.ItemKey{CategoryID: catID, ItemID: newItemID}, false)
	if err != nil {
		t.Fatalf("lookup follow-up item: %v", err)
	}
	if !found {
		t.Fatal("expected follow-up item to be readable after failover and reinstate")
	}
	if err := db.Vectorize(ctx, kbName, nil, &controlledEmbedder{}, 2); err != nil {
		t.Fatalf("vectorize after failover/reinstate failed: %v", err)
	}
}
