package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

type testVectorEmbedder struct{}

func (testVectorEmbedder) Name() string { return "test-embedder" }
func (testVectorEmbedder) Dim() int     { return 3 }
func (testVectorEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		text = strings.ToLower(text)
		switch {
		case strings.Contains(text, "finance"):
			out[i] = []float32{1, 0, 0}
		case strings.Contains(text, "budget"):
			out[i] = []float32{0, 1, 0}
		default:
			out[i] = []float32{0, 0, 1}
		}
	}
	return out, nil
}

var _ ai.Embeddings = testVectorEmbedder{}

func TestRefreshCategoryVectorsForMutation(t *testing.T) {
	ctx := context.Background()
	embedder := testVectorEmbedder{}
	db := database.NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{t.TempDir()}})

	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	kb, err := db.OpenKnowledgeBase(ctx, "vector_refresh_kb", tx, nil, embedder, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase failed: %v", err)
	}

	catID := sop.NewUUID()
	if err := kb.UpsertCategories(ctx, []memory.UpsertCategoryParam{{
		Category: &memory.Category{ID: catID, Name: "Finance", Path: "Finance"},
	}}); err != nil {
		t.Fatalf("UpsertCategories failed: %v", err)
	}

	if err := kb.UpsertItems(ctx, []memory.UpsertItemParam[map[string]any]{{
		CategoryID: catID,
		Item: &memory.Item[map[string]any]{
			ID:        sop.NewUUID(),
			Data:      map[string]any{"text": "Budget report"},
			Summaries: []string{"Budget report"},
		},
	}}); err != nil {
		t.Fatalf("UpsertItems failed: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	agent := NewCopilotAgent(Config{}, nil, db)
	if err := agent.refreshCategoryVectorsForMutation(ctx, db, "vector_refresh_kb", []sop.UUID{catID}, embedder); err != nil {
		t.Fatalf("refreshCategoryVectorsForMutation failed: %v", err)
	}

	tx2, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		t.Fatalf("BeginTransaction(read) failed: %v", err)
	}
	defer tx2.Rollback(ctx)

	kb2, err := db.OpenKnowledgeBase(ctx, "vector_refresh_kb", tx2, nil, embedder, false)
	if err != nil {
		t.Fatalf("OpenKnowledgeBase(read) failed: %v", err)
	}
	cats, err := kb2.Store.Categories(ctx)
	if err != nil {
		t.Fatalf("Categories failed: %v", err)
	}
	found, err := cats.Find(ctx, catID, false)
	if err != nil {
		t.Fatalf("Find category failed: %v", err)
	}
	if !found {
		t.Fatalf("expected category to exist")
	}
	cat, err := cats.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue failed: %v", err)
	}
	if len(cat.CenterVector) == 0 {
		t.Fatalf("expected category center vector to be populated")
	}
}
