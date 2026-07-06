package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop"
)

func TestBlobStoreWithEC_RepairsMissingShardOnRead(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	drives := []string{filepath.Join(root, "drive1"), filepath.Join(root, "drive2"), filepath.Join(root, "drive3")}
	for _, d := range drives {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir drive: %v", err)
		}
	}

	ec := map[string]sop.ErasureCodingConfig{
		"": {
			DataShardsCount:             2,
			ParityShardsCount:           1,
			BaseFolderPathsAcrossDrives: drives,
			RepairCorruptedShards:       true,
		},
	}

	bs, err := NewBlobStoreWithEC(DefaultToFilePath, nil, ec)
	if err != nil {
		t.Fatalf("new blob store: %v", err)
	}
	blobStore := bs.(*BlobStoreWithEC)
	blobID := sop.NewUUID()
	payload := []sop.BlobsPayload[sop.KeyValuePair[sop.UUID, []byte]]{{
		BlobTable: "table",
		Blobs:     []sop.KeyValuePair[sop.UUID, []byte]{{Key: blobID, Value: []byte("rebuild-me")}},
	}}
	if err := blobStore.Add(ctx, payload); err != nil {
		t.Fatalf("add blob: %v", err)
	}

	targetDrive := drives[2]
	fp := blobStore.toFilePath(filepath.Join(targetDrive, "table"), blobID)
	shardPath := fmt.Sprintf("%s%c%s_%d", fp, os.PathSeparator, blobID.String(), 2)
	if err := os.Remove(shardPath); err != nil {
		t.Fatalf("remove target shard: %v", err)
	}

	got, err := blobStore.GetOne(ctx, "table", blobID)
	if err != nil {
		t.Fatalf("get blob after repair: %v", err)
	}
	if string(got) != "rebuild-me" {
		t.Fatalf("unexpected blob contents: %q", string(got))
	}

	if _, err := os.Stat(shardPath); err != nil {
		t.Fatalf("expected rebuilt shard at %s: %v", shardPath, err)
	}

	got, err = blobStore.GetOne(ctx, "table", blobID)
	if err != nil {
		t.Fatalf("get blob after rebuild: %v", err)
	}
	if string(got) != "rebuild-me" {
		t.Fatalf("unexpected blob contents: %q", string(got))
	}
}
