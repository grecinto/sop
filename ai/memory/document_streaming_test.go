package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/inmemory"
	sd "github.com/sharedcode/sop/streamingdata"
)

func TestStreamingDocumentStoreChunksLargePayloads(t *testing.T) {
	ctx := context.Background()
	underlying := inmemory.NewBtree[sd.StreamingDataKey[sop.UUID], []byte](false)
	store := NewStreamingDocumentBtree(underlying.Btree)

	doc := Document{
		ID:          sop.NewUUID(),
		Title:       "large payload",
		ContentType: "text/plain",
		Content:     strings.Repeat("chunked-document-content-", 4000),
	}

	if _, err := store.Add(ctx, doc.ID, doc); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	found, err := store.Find(ctx, doc.ID, false)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if !found {
		t.Fatalf("expected document to be found")
	}

	got, err := store.GetCurrentValue(ctx)
	if err != nil {
		t.Fatalf("GetCurrentValue returned error: %v", err)
	}
	if got.Content != doc.Content {
		t.Fatalf("content mismatch: got %d bytes want %d", len(got.Content), len(doc.Content))
	}

	if underlying.Btree.Count() <= 1 {
		t.Fatalf("expected chunked persistence to create multiple underlying chunks, got %d", underlying.Btree.Count())
	}
}

func TestStreamingDocumentStoreExposesChunkMetadata(t *testing.T) {
	ctx := context.Background()
	underlying := inmemory.NewBtree[sd.StreamingDataKey[sop.UUID], []byte](false)
	store := NewStreamingDocumentBtree(underlying.Btree)

	doc := Document{
		ID:          sop.NewUUID(),
		Title:       "chunk metadata",
		ContentType: "text/plain",
		Content:     strings.Repeat("chunk-metadata-payload-", 8000),
	}

	if _, err := store.Add(ctx, doc.ID, doc); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	metadata, err := store.GetDocumentChunkMetadata(doc.ID)
	if err != nil {
		t.Fatalf("GetDocumentChunkMetadata returned error: %v", err)
	}
	if len(metadata) <= 1 {
		t.Fatalf("expected multiple chunks, got %d", len(metadata))
	}
	if metadata[0].Index != 0 {
		t.Fatalf("expected first chunk index 0, got %d", metadata[0].Index)
	}
	if metadata[len(metadata)-1].Index != len(metadata)-1 {
		t.Fatalf("expected final chunk index %d, got %d", len(metadata)-1, metadata[len(metadata)-1].Index)
	}

	chunk, err := store.GetDocumentChunk(doc.ID, 0)
	if err != nil {
		t.Fatalf("GetDocumentChunk returned error: %v", err)
	}
	if len(chunk) == 0 {
		t.Fatalf("expected chunk payload bytes")
	}
}
