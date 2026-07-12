package database_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/ai/vector"
	"github.com/sharedcode/sop/btree"
	core "github.com/sharedcode/sop/database"
)

func TestAIDatabase_OpenKnowledgeBase_RehydratesCategorizerFromConfig(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	kb, err := db.OpenKnowledgeBase(ctx, "coding_kb", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}

	cfg := &memory.KnowledgeBaseConfig{
		CategorizerConfig: &memory.CategorizerConfig{
			Type: "rule",
			Default: map[string]any{
				"categories": []any{map[string]any{"path": "Coding Best Practices/Go", "keywords": []any{"go", "golang", "context"}}},
			},
			Fallback: "uncategorized",
		},
	}
	if err := kb.SetConfig(ctx, cfg); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	reopenTx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction reopen failed: %v", err)
	}
	defer reopenTx.Rollback(ctx)

	reopened, err := db.OpenKnowledgeBase(ctx, "coding_kb", reopenTx, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase reopen failed: %v", err)
	}
	if reopened.Manager == nil {
		t.Fatal("expected reopened KB manager")
	}
	category, err := reopened.Manager.Categorize("go context cancellation", memory.CategorizeOptions{Mode: memory.CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("expected categorizer to be active after reopen, got err %v", err)
	}
	if category != "Coding Best Practices/Go" {
		t.Fatalf("expected reopened KB to route to Go best practices, got %q", category)
	}
}

func TestAIDatabase_RemoveKnowledgeBase_RemovesAllDomainStores(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	if _, err := db.OpenKnowledgeBase(ctx, "tasks3", tx, nil, nil, false); err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}

	if _, err := db.OpenVectorStore(ctx, "tasks3", tx, vector.Config{UsageMode: ai.Dynamic}); err != nil {
		t.Fatalf("OpenVectorStore failed: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	before, err := db.GetDomains(ctx)
	if err != nil {
		t.Fatalf("GetDomains before delete failed: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected tasks3 domain to exist before deletion")
	}

	if err := db.RemoveKnowledgeBase(ctx, "tasks3"); err != nil {
		t.Fatalf("RemoveKnowledgeBase failed: %v", err)
	}

	after, err := db.GetDomains(ctx)
	if err != nil {
		t.Fatalf("GetDomains after delete failed: %v", err)
	}
	for _, name := range after {
		if name == "tasks3" {
			t.Fatalf("expected tasks3 to be removed from domains, got %v", after)
		}
	}

	verifyTx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction verify failed: %v", err)
	}
	stores, err := verifyTx.GetStores(ctx)
	_ = verifyTx.Rollback(ctx)
	if err != nil {
		t.Fatalf("GetStores verify failed: %v", err)
	}
	for _, store := range stores {
		if len(store) >= len("tasks3/") && store[:len("tasks3/")] == "tasks3/" {
			t.Fatalf("expected no remaining tasks3 stores, found %q", store)
		}
	}
}

func TestAIDatabase_RemoveKnowledgeBase_AcceptsDataSuffix(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}
	if _, err := db.OpenKnowledgeBase(ctx, "tasks4", tx, nil, nil, false); err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if err := db.RemoveKnowledgeBase(ctx, "tasks4/data"); err != nil {
		t.Fatalf("RemoveKnowledgeBase with /data suffix failed: %v", err)
	}
}

func TestAIDatabase_RemoveKnowledgeBase_SuggestsCorrectCase(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}
	if _, err := db.OpenKnowledgeBase(ctx, "Tasks5", tx, nil, nil, false); err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	err = db.RemoveKnowledgeBase(ctx, "tasks5")
	if err == nil {
		t.Fatal("expected case-mismatched delete to fail")
	}
	if !strings.Contains(err.Error(), "did you mean 'Tasks5'") {
		t.Fatalf("expected case suggestion, got %v", err)
	}

	after, err := db.GetDomains(ctx)
	if err != nil {
		t.Fatalf("GetDomains after failed delete failed: %v", err)
	}
	found := false
	for _, name := range after {
		if name == "Tasks5" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Tasks5 to remain after failed delete, got %v", after)
	}
}

func TestAIDatabase_RemoveKnowledgeBase_ReturnsNotFoundWhenMissing(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	err := db.RemoveKnowledgeBase(ctx, "missing_space")
	if err == nil {
		t.Fatal("expected missing knowledge base delete to fail")
	}
}

func TestAIDatabase_OpenKnowledgeBase_OpensMatchingStoreConfiguration(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	legacyStore := sop.ConfigureStore("kb_legacy/categories", true, btree.DefaultSlotLength, "legacy categories store", sop.SmallData, "")
	if _, err := core.NewBtree[sop.UUID, *memory.Category](ctx, db.Config(), legacyStore.Name, tx, nil, legacyStore); err != nil {
		t.Fatalf("pre-creating matching categories store failed: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	reopenTx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction reopen failed: %v", err)
	}
	if _, err := db.OpenKnowledgeBase(ctx, "kb_legacy", reopenTx, nil, nil, false); err != nil {
		t.Fatalf("expected OpenKnowledgeBase to reopen a matching store without failing, got: %v", err)
	}
	if err := reopenTx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
}

func TestAIDatabase_OpenKnowledgeBase_RespectsPersistedTextSearchSetting(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}
	kb, err := db.OpenKnowledgeBase(ctx, "kb_text_optional", tx, nil, nil, false, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}
	if err := kb.SetConfig(ctx, &memory.KnowledgeBaseConfig{TextSearchEnabled: false}); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	tx2, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction reopen failed: %v", err)
	}
	if _, err := db.OpenKnowledgeBase(ctx, "kb_text_optional", tx2, nil, nil, false, true); err != nil {
		t.Fatalf("OpenKnowledgeBase reopen failed: %v", err)
	}
	stores, err := tx2.GetStores(ctx)
	_ = tx2.Rollback(ctx)
	if err != nil {
		t.Fatalf("GetStores failed: %v", err)
	}
	for _, store := range stores {
		if store == "kb_text_optional/text" {
			t.Fatalf("expected persisted KB text-search setting to prevent creating text index store, found %q", store)
		}
	}
}

func TestAIDatabase_OpenKnowledgeBase_UsesFilesystemDocumentTreeForFilesystemReferences(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}
	kb, err := db.OpenKnowledgeBase(ctx, "filesystem_ref_kb", tx, nil, nil, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}

	tmpDir := t.TempDir()
	payload := []byte(strings.Repeat("filesystem-kb-bytes-", 4000))
	path := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	doc := memory.Document{
		ID:          sop.NewUUID(),
		Title:       "filesystem kb doc",
		StorageMode: memory.DocumentStorageModeFilesystemReference,
		PayloadRef:  path,
	}
	if err := kb.Store.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument returned error: %v", err)
	}

	docTree, err := kb.Store.Documents(ctx)
	if err != nil {
		t.Fatalf("Documents returned error: %v", err)
	}
	// Keep the API explicit; do not pass context.Context for ordinary document parameters.
	metadata, err := docTree.GetDocumentChunkMetadata(doc.ID)
	if err != nil {
		t.Fatalf("GetDocumentChunkMetadata returned error: %v", err)
	}
	if len(metadata) <= 1 {
		t.Fatalf("expected multiple chunks for filesystem-backed KB document, got %d", len(metadata))
	}

	var combined []byte
	for _, item := range metadata {
		chunk, err := docTree.GetDocumentChunk(doc.ID, item.Index)
		if err != nil {
			t.Fatalf("GetDocumentChunk(%d) returned error: %v", item.Index, err)
		}
		combined = append(combined, chunk...)
	}
	if !bytes.Equal(combined, payload) {
		t.Fatalf("chunk reconstruction mismatch: got %d bytes want %d", len(combined), len(payload))
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
}

func TestAIDatabase_Standalone_ModelStore(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	ms, err := db.OpenModelStore(ctx, "models", tx)
	if err != nil {
		t.Fatalf("OpenModelStore failed: %v", err)
	}

	if ms == nil {
		t.Error("ModelStore is nil")
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestAIDatabase_Standalone_VectorStore(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	cfg := vector.Config{
		UsageMode: ai.Dynamic,
	}
	vs, err := db.OpenVectorStore(ctx, "vectors", tx, cfg)
	if err != nil {
		t.Fatalf("OpenVectorStore failed: %v", err)
	}

	if vs == nil {
		t.Error("VectorStore is nil")
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestAIDatabase_Standalone_Search(t *testing.T) {
	storagePath := t.TempDir()

	db := database.NewDatabase(core.DatabaseOptions{
		StoresFolders: []string{storagePath},
	})

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	idx, err := db.OpenSearch(ctx, "search_index", tx)
	if err != nil {
		t.Fatalf("OpenSearch failed: %v", err)
	}

	if idx == nil {
		t.Error("Search Index is nil")
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}
