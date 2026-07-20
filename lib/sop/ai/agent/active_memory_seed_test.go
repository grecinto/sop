package agent

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/memory"
)

func TestBuildSemanticBaseKnowledgeThoughts(t *testing.T) {
	chunks := []KnowledgeChunk{
		{Category: "alpha", Title: "First", Content: "one"},
		{Category: "beta", Title: "Second", Content: "two"},
	}

	thoughts := buildSemanticBaseKnowledgeThoughts(chunks)
	if len(thoughts) != len(chunks) {
		t.Fatalf("expected %d thoughts, got %d", len(chunks), len(thoughts))
	}

	for i, thought := range thoughts {
		if thought.CategoryPath != chunks[i].Category {
			t.Fatalf("expected category %q, got %q", chunks[i].Category, thought.CategoryPath)
		}
		if len(thought.Summaries) != 1 || !strings.Contains(thought.Summaries[0], chunks[i].Title) {
			t.Fatalf("expected summary to contain title %q, got %#v", chunks[i].Title, thought.Summaries)
		}
		payload := thought.Data
		if payload["title"] != chunks[i].Title {
			t.Fatalf("expected title payload %q, got %#v", chunks[i].Title, payload["title"])
		}
	}
}

func TestBatchSemanticBaseKnowledgeThoughts(t *testing.T) {
	thoughts := []memory.Thought[map[string]any]{{}, {}, {}, {}}
	batches := batchSemanticBaseKnowledgeThoughts(thoughts, 2)
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}
	if len(batches[0]) != 2 || len(batches[1]) != 2 {
		t.Fatalf("expected batch sizes 2 and 2, got %d and %d", len(batches[0]), len(batches[1]))
	}
}
