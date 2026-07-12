package memory

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/inmemory"
)

func TestExportImportJSON(t *testing.T) {
	ctx := context.Background()
	llm := &MockLLM{}
	embedder := &MockEmbedder{}
	// Create a new Knowledge Base
	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[string]](true)
	st := NewStore[string]("test_kb", nil, cats.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs.Btree, items.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[string])
	st.SetTextIndex(&MockTextIndex{})
	kb := &KnowledgeBase[string]{
		Store:   st,
		Manager: NewMemoryManager[string](st, llm, embedder),
	}

	// Ingest some thoughts
	thoughts := []Thought[string]{
		{Summaries: []string{"Apple is a fruit"}, CategoryPath: "Food", Data: "apple_data", Vectors: [][]float32{{0.1, 0.2}}},
		{Summaries: []string{"Car is a vehicle"}, CategoryPath: "Vehicles", Data: "car_data", Vectors: [][]float32{{0.9, 0.8}}},
	}
	err := kb.IngestThoughts(ctx, thoughts, "test")
	if err != nil {
		t.Fatalf("IngestThoughts failed: %v", err)
	}

	// Export to JSON
	var buf bytes.Buffer
	err = kb.ExportJSON(ctx, &buf)
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}

	jsonStr := buf.String()
	if !strings.Contains(jsonStr, "Food") || !strings.Contains(jsonStr, "Vehicles") {
		t.Errorf("ExportJSON did not contain expected categories: %v", jsonStr)
	}

	if !strings.Contains(jsonStr, "apple_data") || !strings.Contains(jsonStr, "car_data") {
		t.Errorf("ExportJSON did not contain expected data: %v", jsonStr)
	}

	// Create a new empty KnowledgeBase and Import the JSON
	cats2 := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs2 := inmemory.NewBtree[VectorKey, Vector](true)
	items2 := inmemory.NewBtree[ItemKey, Item[string]](true)
	st2 := NewStore[string]("test_kb", nil, cats2.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs2.Btree, items2.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[string])
	st2.SetTextIndex(&MockTextIndex{})
	kb2 := &KnowledgeBase[string]{
		Store:   st2,
		Manager: NewMemoryManager[string](st2, llm, embedder),
	}

	err = kb2.ImportJSON(ctx, &buf, "test")
	if err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}

	// Verify imported data
	c2, err := st2.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if c2 != 2 {
		t.Errorf("Expected 2 items in imported store, got %d", c2)
	}
}

func TestImportJSON_PersistsConfigDescription(t *testing.T) {
	ctx := context.Background()
	llm := &MockLLM{}
	embedder := &MockEmbedder{}

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)
	st := NewStore[map[string]any]("test_kb", nil, cats.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs.Btree, items.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[map[string]any])
	st.SetTextIndex(&MockTextIndex{})
	kb := &KnowledgeBase[map[string]any]{
		Store:   st,
		Manager: NewMemoryManager[map[string]any](st, llm, embedder),
	}

	payload := []byte(`{"config":{"description":"Scalable Objects Persistence (SOP)","system_prompt":"persona","document_mode":true},"categories":[],"items":[]}`)
	if err := kb.ImportJSON(ctx, bytes.NewReader(payload), "test"); err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config after import")
	}
	if got := cfg.Description; got != "Scalable Objects Persistence (SOP)" {
		t.Fatalf("expected Description to be persisted, got %q", got)
	}
}

func TestImportJSON_ReappliesCategorizerConfigToManager(t *testing.T) {
	ctx := context.Background()
	llm := &MockLLM{}
	embedder := &MockEmbedder{}

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)
	st := NewStore[map[string]any]("test_kb", nil, cats.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs.Btree, items.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[map[string]any])
	st.SetTextIndex(&MockTextIndex{})
	kb := &KnowledgeBase[map[string]any]{
		Store:   st,
		Manager: NewMemoryManager[map[string]any](st, llm, embedder),
	}

	payload := []byte(`{"config":{"categorizer_config":{"type":"rule","default":{"categories":[{"path":"Coding Best Practices/Go","keywords":["go","golang","context"]}]},"fallback":"uncategorized"}},"categories":[],"items":[]}`)
	if err := kb.ImportJSON(ctx, bytes.NewReader(payload), "test"); err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg == nil || cfg.CategorizerConfig == nil {
		t.Fatal("expected categorizer config after import")
	}
	if kb.Manager == nil {
		t.Fatal("expected manager to be available")
	}
	if kb.Manager.categorizer == nil {
		t.Fatal("expected imported categorizer config to configure the manager")
	}

	category, err := kb.Manager.Categorize("how to handle go context cancellation", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("Categorize failed: %v", err)
	}
	if category != "Coding Best Practices/Go" {
		t.Fatalf("expected imported categorizer to route to Go best practices, got %q", category)
	}
}

func TestImportJSONWithOptions_BootstrapsCategorizerWhenMissing(t *testing.T) {
	ctx := context.Background()
	llm := &MockLLM{}
	embedder := &MockEmbedder{}

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)
	st := NewStore[map[string]any]("test_kb_bootstrap", nil, cats.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs.Btree, items.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[map[string]any])
	st.SetTextIndex(&MockTextIndex{})
	kb := &KnowledgeBase[map[string]any]{
		Store:   st,
		Manager: NewMemoryManager[map[string]any](st, llm, embedder),
	}

	payload := []byte(`{"config":{"description":"bootstrap test"},"categories":[{"id":"00000000-0000-0000-0000-000000000001","name":"Go","path":"Engineering/Go","center_vector":[]}],"items":[]}`)
	if err := kb.ImportJSONWithOptions(ctx, bytes.NewReader(payload), "test", ImportJSONOptions{BootstrapCategorizerIfMissing: true}); err != nil {
		t.Fatalf("ImportJSONWithOptions failed: %v", err)
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg == nil || cfg.CategorizerConfig == nil {
		t.Fatal("expected imported knowledge base to receive a generated categorizer config")
	}
	if kb.Manager == nil || kb.Manager.categorizer == nil {
		t.Fatal("expected bootstrap categorizer to configure the manager")
	}

	category, err := kb.Manager.Categorize("how to handle go context cancellation", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("Categorize failed: %v", err)
	}
	if category != "Engineering/Go" {
		t.Fatalf("expected bootstrap categorizer to infer Engineering/Go, got %q", category)
	}
}

func TestExportImportJSON_PreservesCategorizerBundle(t *testing.T) {
	ctx := context.Background()
	llm := &MockLLM{}
	embedder := &MockEmbedder{}

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)
	st := NewStore[map[string]any]("test_kb_bundle", nil, cats.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs.Btree, items.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[map[string]any])
	st.SetTextIndex(&MockTextIndex{})
	kb := &KnowledgeBase[map[string]any]{
		Store:   st,
		Manager: NewMemoryManager[map[string]any](st, llm, embedder),
	}

	if err := kb.SetConfig(ctx, &KnowledgeBaseConfig{
		CategorizerRef: "demo-go-bundle",
		CategorizerConfig: &CategorizerConfig{
			Type:     "rule",
			Default:  map[string]any{"categories": []any{map[string]any{"path": "Coding Best Practices/Go", "keywords": []any{"go", "golang", "context"}}}},
			Fallback: "uncategorized",
		},
	}); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	var buf bytes.Buffer
	if err := kb.ExportJSON(ctx, &buf); err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}
	jsonStr := buf.String()
	if !strings.Contains(jsonStr, "categorizer_bundle") || !strings.Contains(jsonStr, "demo-go-bundle") {
		t.Fatalf("expected export to include categorizer bundle metadata, got %s", jsonStr)
	}

	cats2 := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs2 := inmemory.NewBtree[VectorKey, Vector](true)
	items2 := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)
	st2 := NewStore[map[string]any]("test_kb_bundle_import", nil, cats2.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs2.Btree, items2.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[map[string]any])
	st2.SetTextIndex(&MockTextIndex{})
	kb2 := &KnowledgeBase[map[string]any]{
		Store:   st2,
		Manager: NewMemoryManager[map[string]any](st2, llm, embedder),
	}

	if err := kb2.ImportJSON(ctx, bytes.NewReader(buf.Bytes()), "test"); err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}

	cfg, err := kb2.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg == nil || cfg.CategorizerConfig == nil {
		t.Fatal("expected imported categorizer config")
	}
	if cfg.CategorizerRef != "demo-go-bundle" {
		t.Fatalf("expected imported categorizer ref to be preserved, got %q", cfg.CategorizerRef)
	}
	if kb2.Manager == nil || kb2.Manager.categorizer == nil {
		t.Fatal("expected imported categorizer bundle to configure the manager")
	}
	category, err := kb2.Manager.Categorize("how to handle go context cancellation", CategorizeOptions{Mode: CategorizerModeRetrieval})
	if err != nil {
		t.Fatalf("Categorize failed: %v", err)
	}
	if category != "Coding Best Practices/Go" {
		t.Fatalf("expected imported bundle categorizer to route to Go best practices, got %q", category)
	}
}

func TestExportImportJSONPreservesMultipleDocIDs(t *testing.T) {
	ctx := context.Background()
	llm := &MockLLM{}
	embedder := &MockEmbedder{}

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[string]](true)
	st := NewStore[string]("test_kb", nil, cats.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs.Btree, items.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[string])
	st.SetTextIndex(&MockTextIndex{})
	kb := &KnowledgeBase[string]{
		Store:   st,
		Manager: NewMemoryManager[string](st, llm, embedder),
	}

	if err := kb.IngestThoughts(ctx, []Thought[string]{{
		Summaries:    []string{"Source doc test"},
		CategoryPath: "Docs",
		DocID:        DocIDs{"https://example.test/a", "https://example.test/b"},
		Data:         "doc_payload",
		Vectors:      [][]float32{{0.1, 0.2}},
	}}, "test"); err != nil {
		t.Fatalf("IngestThoughts failed: %v", err)
	}

	var buf bytes.Buffer
	if err := kb.ExportJSON(ctx, &buf); err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}

	if !strings.Contains(buf.String(), "\"doc_id\":[\"https://example.test/a\",\"https://example.test/b\"]") {
		t.Fatalf("ExportJSON did not preserve multiple doc ids: %s", buf.String())
	}

	cats2 := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs2 := inmemory.NewBtree[VectorKey, Vector](true)
	items2 := inmemory.NewBtree[ItemKey, Item[string]](true)
	st2 := NewStore[string]("test_kb", nil, cats2.Btree, inmemory.NewBtree[string, sop.UUID](false).Btree, inmemory.NewBtree[DistanceKey, byte](false).Btree, vecs2.Btree, items2.Btree, inmemory.NewBtree[sop.UUID, Document](false).Btree).(*store[string])
	st2.SetTextIndex(&MockTextIndex{})
	kb2 := &KnowledgeBase[string]{
		Store:   st2,
		Manager: NewMemoryManager[string](st2, llm, embedder),
	}

	if err := kb2.ImportJSON(ctx, bytes.NewReader(buf.Bytes()), "test"); err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}

	itemsBtree, err := st2.Items(ctx)
	if err != nil {
		t.Fatalf("Items failed: %v", err)
	}
	ok, _ := itemsBtree.First(ctx)
	if !ok {
		t.Fatal("expected imported items")
	}
	item, err := itemsBtree.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue failed: %v", err)
	}
	if got := len(item.DocID); got != 2 || item.DocID[0] != "https://example.test/a" || item.DocID[1] != "https://example.test/b" {
		t.Fatalf("expected two doc ids after import, got %#v", item.DocID)
	}
}
