package memory

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/inmemory"
)

func TestCloudDocumentTreePreservesCloudReferenceMetadata(t *testing.T) {
	ctx := context.Background()
	underlying := inmemory.NewBtree[sop.UUID, Document](false)
	store := NewCloudDocumentTree(asDocumentTree(underlying.Btree))

	doc := Document{
		ID:          sop.NewUUID(),
		Title:       "apple cloud document",
		StorageMode: DocumentStorageModeExternalMetadataOnly,
		PayloadRef:  "applecloud://container/file.pdf",
		CloudSource: DocumentCloudSource{Provider: "apple_cloud", ObjectKey: "file.pdf"},
	}
	if _, err := store.Add(ctx, doc.ID, doc); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	metadata, err := store.GetDocumentChunkMetadata(doc.ID)
	if err != nil {
		t.Fatalf("GetDocumentChunkMetadata returned error: %v", err)
	}
	if len(metadata) != 1 {
		t.Fatalf("expected a single cloud-reference metadata item, got %d", len(metadata))
	}

	chunk, err := store.GetDocumentChunk(doc.ID, 0)
	if err != nil {
		t.Fatalf("GetDocumentChunk returned error: %v", err)
	}
	if len(chunk) != 0 {
		t.Fatalf("expected no local bytes for cloud-backed document, got %d", len(chunk))
	}
}
