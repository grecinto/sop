package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/inmemory"
)

// TestWorkflowManager_Integration tests WorkflowManager with hardcoded workflow.
func TestWorkflowManager_Integration(t *testing.T) {
	ctx := context.Background()

	// Create KB
	kb, err := createTestKnowledgeBase(ctx)
	if err != nil {
		t.Fatalf("Failed to create test KB: %v", err)
	}

	// Create WorkflowManager
	wm := NewWorkflowManager(kb)
	if wm == nil {
		t.Fatal("NewWorkflowManager returned nil")
	}

	// Create a test workflow manually
	workflow := &WorkflowDefinition{
		ID:           "test-workflow",
		Name:         "Test Workflow",
		CategoryPath: "Test",
		Steps: []WorkflowStep{
			{
				Step:          1,
				Actor:         "test.actor",
				Priority:      100,
				QueryTemplate: "Test query: {original_query}",
			},
		},
	}

	// Test execution
	queue := newMockWorkQueue()
	if err := wm.Execute(ctx, workflow, queue, "original query text"); err != nil {
		t.Fatalf("Failed to execute workflow: %v", err)
	}

	// Verify work item was created
	if len(queue.items) != 1 {
		t.Errorf("Expected 1 work item, got %d", len(queue.items))
	}

	if len(queue.items) > 0 {
		item := queue.items[0]
		if item.Actor != "test.actor" {
			t.Errorf("Expected actor 'test.actor', got %q", item.Actor)
		}
		if item.Priority != 100 {
			t.Errorf("Expected priority 100, got %d", item.Priority)
		}
		expectedQuery := "Test query: original query text"
		if item.Query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, item.Query)
		}
	}
}

// createTestKnowledgeBase creates an in-memory KB for testing.
func createTestKnowledgeBase(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error) {
	// Create in-memory B-trees
	cats := inmemory.NewBtree[sop.UUID, *memory.Category](true)
	vecs := inmemory.NewBtree[memory.VectorKey, memory.Vector](true)
	items := inmemory.NewBtree[memory.ItemKey, memory.Item[map[string]any]](true)
	pathTree := inmemory.NewBtree[string, sop.UUID](false)
	distTree := inmemory.NewBtree[memory.DistanceKey, byte](false)
	docsTree := inmemory.NewBtree[sop.UUID, memory.Document](false)

	// Create store
	store := memory.NewStore[map[string]any](
		"test_workflow_kb",
		nil,
		cats.Btree,
		pathTree.Btree,
		distTree.Btree,
		vecs.Btree,
		items.Btree,
		docsTree.Btree,
	)

	// Create mock embedder
	embedder := &mockEmbedder{}

	// Create mock LLM
	llm := &mockLLM{embedder: embedder}

	// Create KB with manager
	kb := &memory.KnowledgeBase[map[string]any]{
		Store:   store,
		Manager: memory.NewMemoryManager[map[string]any](store, llm, embedder),
	}

	return kb, nil
}

// mockEmbedder implements ai.Embeddings for testing
type mockEmbedder struct{}

func (m *mockEmbedder) Name() string { return "mock-embedder" }
func (m *mockEmbedder) Dim() int     { return 768 }

func (m *mockEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	// Return simple mock embeddings
	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = make([]float32, 768)
		// Use text length as a simple hash
		if len(texts[i]) > 0 {
			vecs[i][0] = float32(len(texts[i])) / 100.0
		}
	}
	return vecs, nil
}

// mockLLM implements memory.LLM and ai.Generator for testing
type mockLLM struct {
	embedder *mockEmbedder
}

func (m *mockLLM) GenerateCategory(ctx context.Context, payload map[string]any) (*memory.Category, error) {
	// Return a simple mock category
	return &memory.Category{
		ID:   sop.NewUUID(),
		Name: "MockCategory",
	}, nil
}

func (m *mockLLM) Name() string                                               { return "mock-llm" }
func (m *mockLLM) EstimateCost(inTokens, outTokens int) float64               { return 0 }
func (m *mockLLM) PrewarmCache(ctx context.Context, opts ai.GenOptions) error { return nil }
func (m *mockLLM) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{Text: "mock response"}, nil
}
