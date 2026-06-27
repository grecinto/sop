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
		Content:     strings.Repeat("chunked-document-content-", 50000),
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

	if underlying.Btree.Count() != 1 {
		t.Fatalf("expected the current streaming implementation to persist as a single underlying chunk, got %d", underlying.Btree.Count())
	}
}

func TestDocumentChunkSizeForErasureConfigScalesWithShardCount(t *testing.T) {
	if got := DocumentChunkSizeForErasureConfig(nil); got != DefaultDocumentChunkSize {
		t.Fatalf("expected default chunk size %d, got %d", DefaultDocumentChunkSize, got)
	}

	if got := DocumentChunkSizeForErasureConfig(map[string]sop.ErasureCodingConfig{"default": {DataShardsCount: 2, ParityShardsCount: 1}}); got != DefaultDocumentChunkSize {
		t.Fatalf("expected baseline chunk size %d for a few shards, got %d", DefaultDocumentChunkSize, got)
	}

	if got := DocumentChunkSizeForErasureConfig(map[string]sop.ErasureCodingConfig{"default": {DataShardsCount: 4, ParityShardsCount: 2}}); got != 2*DefaultDocumentChunkSize {
		t.Fatalf("expected doubled chunk size %d for a medium EC layout, got %d", 2*DefaultDocumentChunkSize, got)
	}

	if got := DocumentChunkSizeForErasureConfig(map[string]sop.ErasureCodingConfig{"default": {DataShardsCount: 8, ParityShardsCount: 0}}); got != 4*DefaultDocumentChunkSize {
		t.Fatalf("expected quadrupled chunk size %d for a large EC layout, got %d", 4*DefaultDocumentChunkSize, got)
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
		Content:     strings.Repeat("chunk-metadata-payload-", 50000),
	}

	if _, err := store.Add(ctx, doc.ID, doc); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	metadata, err := store.GetDocumentChunkMetadata(doc.ID)
	if err != nil {
		t.Fatalf("GetDocumentChunkMetadata returned error: %v", err)
	}
	if len(metadata) != 1 {
		t.Fatalf("expected a single chunk for the current implementation, got %d", len(metadata))
	}
	if metadata[0].Index != 0 {
		t.Fatalf("expected first chunk index 0, got %d", metadata[0].Index)
	}

	chunk, err := store.GetDocumentChunk(doc.ID, 0)
	if err != nil {
		t.Fatalf("GetDocumentChunk returned error: %v", err)
	}
	if len(chunk) == 0 {
		t.Fatalf("expected chunk payload bytes")
	}
}
