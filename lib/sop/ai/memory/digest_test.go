package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/inmemory"
)

type mapDigestLLM struct{}

func (m *mapDigestLLM) Name() string { return "map-digest-llm" }

func (m *mapDigestLLM) EstimateCost(inTokens, outTokens int) float64 {
	return 0.0
}

func (m *mapDigestLLM) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func (m *mapDigestLLM) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	if strings.Contains(strings.ToLower(prompt), "sdlc") {
		return ai.GenOutput{Text: "SDLC"}, nil
	}
	return ai.GenOutput{Text: "Architecture"}, nil
}

func (m *mapDigestLLM) GenerateCategory(ctx context.Context, payload map[string]any) (*Category, error) {
	name := "General"
	if content, ok := payload["content"].(string); ok {
		switch {
		case strings.Contains(strings.ToLower(content), "architecture"):
			name = "Architecture"
		case strings.Contains(strings.ToLower(content), "sdlc"):
			name = "SDLC"
		}
	}
	return &Category{ID: sop.NewUUID(), Name: name}, nil
}

func TestDigestKnowledgeBase_ReturnsRelevantHits(t *testing.T) {
	ctx := context.Background()

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)

	store := NewStore[map[string]any](
		"digest_kb",
		nil,
		cats.Btree,
		inmemory.NewBtree[string, sop.UUID](false).Btree,
		inmemory.NewBtree[DistanceKey, byte](false).Btree,
		vecs.Btree,
		items.Btree,
		inmemory.NewBtree[sop.UUID, Document](false).Btree,
	).(*store[map[string]any])
	store.SetTextIndex(&MockTextIndex{})

	embedder := &MockPlaybookEmbedder{Rules: []PlaybookRule{
		{Keywords: []string{"architecture", "system design"}, CategoryName: "Architecture", Vector: []float32{1, 0, 0}},
		{Keywords: []string{"sdlc", "release"}, CategoryName: "SDLC", Vector: []float32{0, 1, 0}},
	}}
	llm := &mapDigestLLM{}
	store.SetLLM(llm)

	kb := &KnowledgeBase[map[string]any]{
		Store:   store,
		Manager: NewMemoryManager[map[string]any](store, llm, embedder),
	}

	err := kb.IngestThoughts(ctx, []Thought[map[string]any]{
		{
			Summaries:    []string{"SOP architecture guide"},
			CategoryPath: "Architecture",
			Vectors:      [][]float32{{1, 0, 0}},
			Data:         map[string]any{"content": "SOP architecture explains package layout and system boundaries."},
		},
		{
			Summaries:    []string{"SOP SDLC guide"},
			CategoryPath: "SDLC",
			Vectors:      [][]float32{{0, 1, 0}},
			Data:         map[string]any{"content": "SOP SDLC covers tests, release discipline, and onboarding expectations."},
		},
	}, "test")
	if err != nil {
		t.Fatalf("failed to ingest thoughts: %v", err)
	}

	hits, err := DigestKnowledgeBase(ctx, kb, embedder, KBDigestRequest{
		Queries:            []string{"architecture", "SOP architecture", "architecture"},
		PerQueryLimit:      3,
		MaxResults:         5,
		MinScore:           0.1,
		UseClosestCategory: true,
		KeywordFallback:    true,
	})
	if err != nil {
		t.Fatalf("DigestKnowledgeBase failed: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected at least one digest hit, got none")
	}

	joined := hits[0].Text
	if !strings.Contains(joined, "SOP architecture") {
		t.Fatalf("expected architecture content in digest, got %q", joined)
	}
	if hits[0].SearchType == "" {
		t.Fatalf("expected search metadata to be populated, got %+v", hits)
	}
}

func TestDigestKnowledgeBase_UsesDocumentContentWhenHitReferencesInternalDocument(t *testing.T) {
	ctx := context.Background()

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)

	store := NewStore[map[string]any](
		"digest_doc_kb",
		nil,
		cats.Btree,
		inmemory.NewBtree[string, sop.UUID](false).Btree,
		inmemory.NewBtree[DistanceKey, byte](false).Btree,
		vecs.Btree,
		items.Btree,
		inmemory.NewBtree[sop.UUID, Document](false).Btree,
	).(*store[map[string]any])
	store.SetTextIndex(&MockTextIndex{})

	embedder := &MockPlaybookEmbedder{Rules: []PlaybookRule{{Keywords: []string{"parser"}, CategoryName: "Architecture", Vector: []float32{1, 0, 0}}}}
	llm := &mapDigestLLM{}
	store.SetLLM(llm)

	kb := &KnowledgeBase[map[string]any]{
		Store:   store,
		Manager: NewMemoryManager[map[string]any](store, llm, embedder),
	}

	docID := sop.NewUUID()
	if err := store.UpsertDocument(ctx, Document{ID: docID, Title: "canonical parser memory", ContentType: "text/plain", Content: "Canonical parser repair guidance for the regression fix."}); err != nil {
		t.Fatalf("failed to store canonical document: %v", err)
	}

	if err := kb.IngestThoughts(ctx, []Thought[map[string]any]{
		{
			Summaries:    []string{"parser repair summary"},
			CategoryPath: "Architecture",
			DocID:        DocIDs{docID.String()},
			Vectors:      [][]float32{{1, 0, 0}},
			Data:         map[string]any{"content": "SOP architecture guide for parser repair and regression fixes."},
		},
	}, "test"); err != nil {
		t.Fatalf("failed to ingest thought: %v", err)
	}

	hits, err := DigestKnowledgeBase(ctx, kb, embedder, KBDigestRequest{
		Queries:            []string{"parser repair"},
		PerQueryLimit:      3,
		MaxResults:         5,
		MinScore:           0.1,
		UseClosestCategory: true,
		KeywordFallback:    true,
	})
	if err != nil {
		t.Fatalf("DigestKnowledgeBase failed: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected a digest hit for the document-backed thought")
	}
	if got := strings.TrimSpace(hits[0].Text); got != "Canonical parser repair guidance for the regression fix." {
		t.Fatalf("expected digest to prefer canonical document content, got %q", got)
	}
}

func TestResolveHitText_UsesCanonicalDocumentContentForInternalDocuments(t *testing.T) {
	ctx := context.Background()

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)

	store := NewStore[map[string]any](
		"resolve_hit_text_kb",
		nil,
		cats.Btree,
		inmemory.NewBtree[string, sop.UUID](false).Btree,
		inmemory.NewBtree[DistanceKey, byte](false).Btree,
		vecs.Btree,
		items.Btree,
		inmemory.NewBtree[sop.UUID, Document](false).Btree,
	).(*store[map[string]any])
	store.SetTextIndex(&MockTextIndex{})

	docID := sop.NewUUID()
	if err := store.UpsertDocument(ctx, Document{ID: docID, Title: "canonical parser memory", ContentType: "text/plain", Content: "Canonical parser repair guidance for the regression fix."}); err != nil {
		t.Fatalf("failed to store canonical document: %v", err)
	}

	kb := &KnowledgeBase[map[string]any]{Store: store}
	got := ResolveHitText(ctx, kb, map[string]any{"content": "fallback summary"}, []string{docID.String()})
	if got != "Canonical parser repair guidance for the regression fix." {
		t.Fatalf("expected canonical document content, got %q", got)
	}
}

func TestResolveHitText_RendersReadableTextFromStructuredDocumentContent(t *testing.T) {
	ctx := context.Background()

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)

	store := NewStore[map[string]any](
		"structured_doc_resolver_kb",
		nil,
		cats.Btree,
		inmemory.NewBtree[string, sop.UUID](false).Btree,
		inmemory.NewBtree[DistanceKey, byte](false).Btree,
		vecs.Btree,
		items.Btree,
		inmemory.NewBtree[sop.UUID, Document](false).Btree,
	).(*store[map[string]any])
	store.SetTextIndex(&MockTextIndex{})

	docID := sop.NewUUID()
	canonicalJSON := `{"description":"fix parser","outcome":"patched parser","lesson":"keep the parser behavior focused","next_step":"add a regression test"}`
	if err := store.UpsertDocument(ctx, Document{ID: docID, Title: "coding_workflow_memory", ContentType: "application/json", Content: canonicalJSON}); err != nil {
		t.Fatalf("failed to store structured canonical document: %v", err)
	}

	kb := &KnowledgeBase[map[string]any]{Store: store}
	got := ResolveHitText(ctx, kb, map[string]any{"content": "fallback summary"}, []string{docID.String()})
	if !strings.Contains(got, "fix parser") || !strings.Contains(got, "patched parser") {
		t.Fatalf("expected structured canonical document content to render readably, got %q", got)
	}
}

func TestResolveHitSourceLabel_UsesCanonicalDocumentMetadata(t *testing.T) {
	ctx := context.Background()

	cats := inmemory.NewBtree[sop.UUID, *Category](true)
	vecs := inmemory.NewBtree[VectorKey, Vector](true)
	items := inmemory.NewBtree[ItemKey, Item[map[string]any]](true)

	store := NewStore[map[string]any](
		"source_label_resolver_kb",
		nil,
		cats.Btree,
		inmemory.NewBtree[string, sop.UUID](false).Btree,
		inmemory.NewBtree[DistanceKey, byte](false).Btree,
		vecs.Btree,
		items.Btree,
		inmemory.NewBtree[sop.UUID, Document](false).Btree,
	).(*store[map[string]any])
	store.SetTextIndex(&MockTextIndex{})

	docID := sop.NewUUID()
	if err := store.UpsertDocument(ctx, Document{ID: docID, Title: "parser regression fix", Source: "coding_workflow", ContentType: "text/plain", Content: "canonical content"}); err != nil {
		t.Fatalf("failed to store canonical document: %v", err)
	}

	kb := &KnowledgeBase[map[string]any]{Store: store}
	got := ResolveHitSourceLabel(ctx, kb, []string{docID.String()})
	if got != "parser regression fix" {
		t.Fatalf("expected canonical document title to be used as source label, got %q", got)
	}
}

func TestResolveHitSourceLabel_FallsBackToRawDocIDWhenDocumentIsUnavailable(t *testing.T) {
	ctx := context.Background()
	kb := &KnowledgeBase[map[string]any]{Store: nil}
	if got := ResolveHitSourceLabel(ctx, kb, []string{"https://example.test/doc-2"}); got != "https://example.test/doc-2" {
		t.Fatalf("expected raw doc id fallback, got %q", got)
	}
}

func TestMergeDigestHit_DeduplicatesByDocIDOrContent(t *testing.T) {
	merged := map[string]KBDigestHit{}

	mergeDigestHit(merged, KBDigestHit{
		DocID:      DocIDs{"doc-1"},
		Score:      0.6,
		Category:   "Architecture",
		Text:       "Architecture basics",
		Query:      "architecture",
		SearchType: "semantic",
	})
	mergeDigestHit(merged, KBDigestHit{
		DocID:      DocIDs{"doc-1"},
		Score:      0.9,
		Category:   "Architecture",
		Text:       "Architecture basics",
		Query:      "SOP architecture",
		SearchType: "keyword",
	})
	mergeDigestHit(merged, KBDigestHit{
		Score:      0.7,
		Category:   "SDLC",
		Text:       "Release discipline",
		Query:      "sdlc",
		SearchType: "semantic",
	})
	mergeDigestHit(merged, KBDigestHit{
		Score:      0.5,
		Category:   "SDLC",
		Text:       "Release discipline",
		Query:      "release",
		SearchType: "keyword",
	})

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged digest hits, got %d: %+v", len(merged), merged)
	}
	if hit := merged["doc-1"]; hit.Score != 0.9 || hit.SearchType != "keyword" {
		t.Fatalf("expected highest scoring doc-id hit to win, got %+v", hit)
	}
	contentKey := "SDLC|Release discipline"
	if hit := merged[contentKey]; hit.Score != 0.7 || hit.Query != "sdlc" {
		t.Fatalf("expected content-based dedupe to retain strongest hit, got %+v", hit)
	}
}
