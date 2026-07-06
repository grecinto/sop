package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/common/mocks"
)

// TestReinstateFailedDrives_RepairsErasureCodingShards validates that missing EC shards
// are reconstructed during drive reinstatement by walking all stores and reading their blobs.
func TestReinstateFailedDrives_RepairsErasureCodingShards(t *testing.T) {
	ctx := context.Background()

	// Isolate global replication state
	globalReplicationDetailsLocker.Lock()
	prev := GlobalReplicationDetails
	GlobalReplicationDetails = &ReplicationTrackedDetails{}
	globalReplicationDetailsLocker.Unlock()
	t.Cleanup(func() {
		globalReplicationDetailsLocker.Lock()
		GlobalReplicationDetails = prev
		globalReplicationDetailsLocker.Unlock()
	})

	root := t.TempDir()
	activeBase := filepath.Join(root, "active")
	passiveBase := filepath.Join(root, "passive")

	// Set up 3-drive EC configuration (2 data + 1 parity)
	drives := []string{
		filepath.Join(root, "drive1"),
		filepath.Join(root, "drive2"),
		filepath.Join(root, "drive3"),
	}
	for _, d := range drives {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir drive: %v", err)
		}
	}

	ecConfig := sop.ErasureCodingConfig{
		DataShardsCount:             2,
		ParityShardsCount:           1,
		BaseFolderPathsAcrossDrives: drives,
		RepairCorruptedShards:       true, // Enable repair
	}

	// Save and restore global EC config
	prevEC := GetGlobalErasureConfig()
	SetGlobalErasureConfig(map[string]sop.ErasureCodingConfig{"": ecConfig})
	t.Cleanup(func() { SetGlobalErasureConfig(prevEC) })

	// Create replication tracker
	rt, err := NewReplicationTracker(ctx, []string{activeBase, passiveBase}, true, mocks.NewMockClient())
	if err != nil {
		t.Fatalf("NewReplicationTracker: %v", err)
	}

	// Create store repository and add a store
	sr, err := NewStoreRepository(ctx, rt, nil, mocks.NewMockClient(), 0)
	if err != nil {
		t.Fatalf("NewStoreRepository: %v", err)
	}

	storeName := "ec_test_store"
	storeInfo := sop.StoreInfo{
		Name:                     storeName,
		SlotLength:               8,
		IsUnique:                 true,
		BlobTable:                "", // Use default EC config
		RegistryTable:            sop.FormatRegistryTable(storeName),
		IsValueDataInNodeSegment: true,
		RootNodeID:               sop.NewUUID(),
	}

	if err := sr.Add(ctx, storeInfo); err != nil {
		t.Fatalf("Add store: %v", err)
	}

	// Create blob store with EC
	bs, err := NewBlobStoreWithEC(DefaultToFilePath, nil, map[string]sop.ErasureCodingConfig{"": ecConfig})
	if err != nil {
		t.Fatalf("NewBlobStoreWithEC: %v", err)
	}
	blobStore := bs.(*BlobStoreWithEC)

	// Write test blob
	blobID := sop.NewUUID()
	testData := []byte("test-data-for-ec-repair-on-reinstate")
	payload := []sop.BlobsPayload[sop.KeyValuePair[sop.UUID, []byte]]{{
		BlobTable: "",
		Blobs:     []sop.KeyValuePair[sop.UUID, []byte]{{Key: blobID, Value: testData}},
	}}

	if err := blobStore.Add(ctx, payload); err != nil {
		t.Fatalf("Add blob: %v", err)
	}

	// Verify all shards exist before simulated failure
	for i := 0; i < 3; i++ {
		fp := blobStore.toFilePath(filepath.Join(drives[i], ""), blobID)
		shardPath := fmt.Sprintf("%s%c%s_%d", fp, os.PathSeparator, blobID.String(), i)
		if _, err := os.Stat(shardPath); err != nil {
			t.Fatalf("expected shard %d to exist before failure: %v", i, err)
		}
	}

	// Simulate drive failure by removing one shard (drive3, index 2)
	targetDrive := drives[2]
	fp := blobStore.toFilePath(filepath.Join(targetDrive, ""), blobID)
	shardPath := fmt.Sprintf("%s%c%s_%d", fp, os.PathSeparator, blobID.String(), 2)

	if err := os.Remove(shardPath); err != nil {
		t.Fatalf("remove target shard: %v", err)
	}

	// Verify shard is missing
	if _, err := os.Stat(shardPath); !os.IsNotExist(err) {
		t.Fatalf("expected shard to be removed: stat err=%v", err)
	}

	// Mark replication as failed (prerequisite for ReinstateFailedDrives)
	rt.FailedToReplicate = true
	globalReplicationDetailsLocker.Lock()
	GlobalReplicationDetails.FailedToReplicate = true
	globalReplicationDetailsLocker.Unlock()

	// Act: Call ReinstateFailedDrives which should trigger EC repair
	if err := rt.ReinstateFailedDrives(ctx); err != nil {
		t.Fatalf("ReinstateFailedDrives: %v", err)
	}

	// Assert: Shard should be reconstructed
	if _, err := os.Stat(shardPath); err != nil {
		t.Fatalf("expected rebuilt shard at %s after reinstate: %v", shardPath, err)
	}

	// Verify we can still read the blob correctly
	result, err := blobStore.GetOne(ctx, "", blobID)
	if err != nil {
		t.Fatalf("GetOne after repair: %v", err)
	}
	if string(result) != string(testData) {
		t.Fatalf("data mismatch after repair: got %q, want %q", result, testData)
	}

	// Verify replication is no longer marked as failed
	if GlobalReplicationDetails.FailedToReplicate {
		t.Fatalf("expected FailedToReplicate to be cleared after successful reinstate")
	}
}
