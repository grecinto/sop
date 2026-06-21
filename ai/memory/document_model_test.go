package memory

import "testing"

func TestDocumentStorageModeHelpers(t *testing.T) {
	t.Run("defaults to local streaming for backward-compatible documents", func(t *testing.T) {
		doc := Document{}
		if got := doc.NormalizedStorageMode(); got != DocumentStorageModeSpaceLocalStreaming {
			t.Fatalf("expected default storage mode %q, got %q", DocumentStorageModeSpaceLocalStreaming, got)
		}
		if !doc.IsSpaceLocalStreaming() {
			t.Fatalf("expected zero-value document to be treated as space-local streaming")
		}
	})

	t.Run("recognizes streaming store docid mode", func(t *testing.T) {
		doc := Document{StorageMode: DocumentStorageModeStreamingStoreDocID, PayloadRef: "doc-123"}
		if !doc.IsStreamingStoreDocID() {
			t.Fatalf("expected streaming store docid mode to be recognized")
		}
		if doc.IsExternalMetadataOnly() {
			t.Fatalf("expected streaming store docid mode not to be treated as external metadata")
		}
	})

	t.Run("recognizes external metadata-only mode", func(t *testing.T) {
		doc := Document{StorageMode: DocumentStorageModeExternalMetadataOnly, PayloadRef: "gs://bucket/file.pdf"}
		if !doc.IsExternalMetadataOnly() {
			t.Fatalf("expected external metadata-only mode to be recognized")
		}
		if doc.IsSpaceLocalStreaming() {
			t.Fatalf("expected external metadata-only mode not to be treated as space-local streaming")
		}
	})

	t.Run("recognizes filesystem reference mode", func(t *testing.T) {
		doc := Document{StorageMode: DocumentStorageModeFilesystemReference, PayloadRef: "/tmp/inbox/report.pdf"}
		if !doc.IsFilesystemReference() {
			t.Fatalf("expected filesystem reference mode to be recognized")
		}
		if doc.IsExternalMetadataOnly() {
			t.Fatalf("expected filesystem reference mode not to be treated as external metadata-only")
		}
	})

	t.Run("recognizes apple cloud references", func(t *testing.T) {
		doc := Document{
			StorageMode: DocumentStorageModeExternalMetadataOnly,
			PayloadRef:  "applecloud://container/file.pdf",
			CloudSource: DocumentCloudSource{Provider: "apple_cloud", ObjectKey: "file.pdf"},
		}
		if !doc.IsAppleCloud() {
			t.Fatalf("expected apple cloud reference to be recognized")
		}
		if got := doc.NormalizedCloudProvider(); got != "apple_cloud" {
			t.Fatalf("expected normalized provider %q, got %q", "apple_cloud", got)
		}
	})
}
