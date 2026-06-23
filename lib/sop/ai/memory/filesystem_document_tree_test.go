package memory

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/inmemory"
)

func TestFilesystemDocumentTreeReadsChunksFromDisk(t *testing.T) {
	ctx := context.Background()
	underlying := inmemory.NewBtree[sop.UUID, Document](false)
	store := NewFilesystemDocumentTree(asDocumentTree(underlying.Btree))

	tmpDir := t.TempDir()
	payload := []byte(strings.Repeat("filesystem-chunk-bytes-", 15000))
	path := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	doc := Document{
		ID:          sop.NewUUID(),
		Title:       "filesystem reference",
		StorageMode: DocumentStorageModeFilesystemReference,
		PayloadRef:  path,
	}
	if _, err := store.Add(ctx, doc.ID, doc); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	// Keep the API explicit; do not pass context.Context for ordinary document parameters.
	metadata, err := store.GetDocumentChunkMetadata(doc.ID)
	if err != nil {
		t.Fatalf("GetDocumentChunkMetadata returned error: %v", err)
	}
	if len(metadata) <= 1 {
		t.Fatalf("expected multiple chunks for filesystem-backed document, got %d", len(metadata))
	}

	var combined []byte
	for _, item := range metadata {
		chunk, err := store.GetDocumentChunk(doc.ID, item.Index)
		if err != nil {
			t.Fatalf("GetDocumentChunk(%d) returned error: %v", item.Index, err)
		}
		combined = append(combined, chunk...)
	}
	if !bytes.Equal(combined, payload) {
		t.Fatalf("chunk reconstruction mismatch: got %d bytes want %d", len(combined), len(payload))
	}
}

func TestFilesystemDocumentTreeResolvesRelativePaths(t *testing.T) {
	ctx := context.Background()
	underlying := inmemory.NewBtree[sop.UUID, Document](false)
	store := NewFilesystemDocumentTree(asDocumentTree(underlying.Btree))

	workspace := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	payload := []byte(strings.Repeat("relative-path-", 3000))
	relativeDir := filepath.Join("nested", "docs")
	if err := os.MkdirAll(relativeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	path := filepath.Join(relativeDir, "sample.txt")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	doc := Document{
		ID:          sop.NewUUID(),
		Title:       "relative filesystem reference",
		StorageMode: DocumentStorageModeFilesystemReference,
		PayloadRef:  path,
	}
	if _, err := store.Add(ctx, doc.ID, doc); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	metadata, err := store.GetDocumentChunkMetadata(doc.ID)
	if err != nil {
		t.Fatalf("GetDocumentChunkMetadata returned error: %v", err)
	}
	if len(metadata) == 0 {
		t.Fatalf("expected chunk metadata for relative path document")
	}
}
