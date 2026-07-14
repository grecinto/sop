package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/encoding"
)

// ReinstateFailedDrives can be invoked after replacing the failed drives so they can be synchronized with
// the current Active targets' Stores' contents.
//
// High-level flow:
//  1. Enable commit-change logging and persist replication status
//  2. Copy store repositories and registry segment files to passive targets
//  3. Fast-forward by reading commit logs and applying to passive stores/registries until none remain
//  4. Turn replication back on and sync replication status to L2 cache
//  5. Run fast-forward again to catch any late logs
func (r *replicationTracker) ReinstateFailedDrives(ctx context.Context) error {
	if !r.replicate {
		return fmt.Errorf("replicationTracker.replicate flag is off, ReinstateFailedDrives is valid only if this is on")
	}
	if !r.ReplicationTrackedDetails.FailedToReplicate {
		return fmt.Errorf("replicationTracker.FailedToReplicate is false, ReinstateFailedDrives is valid only if this is true")
	}

	if err := ensureRecoveryStatePathForPassiveDrive(r.getPassiveBaseFolder()); err != nil {
		return err
	}
	if err := r.startLoggingCommitChanges(ctx); err != nil {
		return err
	}
	if err := r.copyStores(ctx); err != nil {
		return err
	}
	if err := r.repairErasureCodingShards(ctx); err != nil {
		return err
	}
	// Consume all Commit logs.
	for {
		if fileFound, err := r.fastForward(ctx); err != nil {
			return err
		} else if !fileFound {
			break
		}
	}
	// Turn on Replication.
	if err := r.turnOnReplication(ctx); err != nil {
		return err
	}
	// Check fast forward log one last time.
	for {
		if fileFound, err := r.fastForward(ctx); err != nil {
			return err
		} else if !fileFound {
			return nil
		}
	}
}

// startLoggingCommitChanges enables logging of commit changes and persists the replication status
// both on disk and in the L2 cache.
func (r *replicationTracker) startLoggingCommitChanges(ctx context.Context) error {
	globalReplicationDetailsLocker.Lock()
	defer globalReplicationDetailsLocker.Unlock()

	GlobalReplicationDetails.LogCommitChanges = true
	r.LogCommitChanges = true

	// Update the replication status details.
	if err := r.writeReplicationStatus(ctx, r.formatActiveFolderEntity(replicationStatusFilename)); err != nil {
		return err
	}
	return r.syncWithL2Cache(ctx, true)
}

// copyStores copies the active store repository and registry files to the passive folders.
func (r *replicationTracker) copyStores(ctx context.Context) error {
	if sr, err := NewStoreRepository(ctx, r, nil, r.l2Cache, 0); err != nil {
		return err
	} else {
		// Copy Store Repositories & Registries to passive folders being reinstated for replication.
		return sr.CopyToPassiveFolders(ctx)
	}
}

// repairErasureCodingShards walks all stores and reads their blob data to trigger EC repair on reinstated drives.
// This ensures that missing shards are reconstructed using the existing RepairCorruptedShards logic.
func (r *replicationTracker) repairErasureCodingShards(ctx context.Context) error {
	// Check if EC is configured
	ecConfigs := GetGlobalErasureConfig()
	if len(ecConfigs) == 0 {
		return nil
	}

	// Get store repository to enumerate stores
	sr, err := NewStoreRepository(ctx, r, nil, r.l2Cache, 0)
	if err != nil {
		return err
	}

	stores, err := sr.GetAll(ctx)
	if err != nil {
		return err
	}

	// For each store, walk its blob data to trigger repair
	for _, storeName := range stores {
		storeInfo, err := sr.Get(ctx, storeName)
		if err != nil || len(storeInfo) == 0 {
			continue
		}

		si := storeInfo[0]
		// Get EC config for this store's blob table
		ecConfig, hasEC := ecConfigs[si.BlobTable]
		if !hasEC {
			// Try default EC config
			ecConfig, hasEC = ecConfigs[""]
			if !hasEC {
				continue
			}
		}

		// Ensure repair flag is enabled for this sweep
		if !ecConfig.RepairCorruptedShards {
			// Temporarily enable repair for this reinstate operation
			ecConfig.RepairCorruptedShards = true
		}

		// Create blob store with repair enabled
		bs, err := NewBlobStoreWithEC(DefaultToFilePath, nil, map[string]sop.ErasureCodingConfig{si.BlobTable: ecConfig})
		if err != nil {
			return err
		}
		blobStore := bs.(*BlobStoreWithEC)

		// Walk all blob files and read them to trigger repair
		if err := r.walkAndRepairBlobs(ctx, blobStore, si.BlobTable, ecConfig.BaseFolderPathsAcrossDrives); err != nil {
			return err
		}
	}

	return nil
}

// walkAndRepairBlobs walks all blob shard files and reads each blob through the EC GetOne path
// to trigger reconstruction of missing shards on the reinstated drive.
func (r *replicationTracker) walkAndRepairBlobs(ctx context.Context, blobStore *BlobStoreWithEC, blobTable string, drives []string) error {
	// Pick first available drive to scan for blob IDs
	var scanDrive string
	for _, drive := range drives {
		scanDir := filepath.Join(drive, blobTable)
		if fio := NewFileIO(); fio.Exists(ctx, scanDir) {
			scanDrive = scanDir
			break
		}
	}
	if scanDrive == "" {
		return nil // No blobs to repair
	}

	// Walk the directory tree and collect blob IDs
	blobIDs := make(map[sop.UUID]bool)
	if err := r.collectBlobIDs(ctx, scanDrive, blobIDs); err != nil {
		return err
	}

	// Read each blob to trigger EC repair
	for blobID := range blobIDs {
		_, _ = blobStore.GetOne(ctx, blobTable, blobID)
		// Ignore read errors - repair happens on successful decode
	}

	return nil
}

// collectBlobIDs recursively walks a directory tree and extracts blob UUIDs from shard filenames.
func (r *replicationTracker) collectBlobIDs(ctx context.Context, dir string, blobIDs map[sop.UUID]bool) error {
	fio := NewFileIO()
	entries, err := fio.ReadDir(ctx, dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			if err := r.collectBlobIDs(ctx, filepath.Join(dir, entry.Name()), blobIDs); err != nil {
				return err
			}
			continue
		}

		// Parse blob UUID from shard filename (format: <uuid>_<shardIndex>)
		name := entry.Name()
		if idx := strings.Index(name, "_"); idx > 0 {
			if blobID, err := sop.ParseUUID(name[:idx]); err == nil {
				blobIDs[blobID] = true
			}
		}
	}

	return nil
}

// fastForward reads commit logs, applies changes to passive targets, and deletes each processed log.
// Returns true if any logs were found and processed.
func (r *replicationTracker) fastForward(ctx context.Context) (bool, error) {
	// Read the transaction commit logs then sync the passive stores/registries w/ the values from active stores/regs.
	//   - In case StoreRepository exists in target, use the StoreRepository timestamp to determine if target needs to get updated.
	//     If missing in passive target then add one from active.
	//   - In case Handle exists in target, use Handle version to determine if target needs to get updated.
	//     If missing in passive target then add one from active.
	// Delete after the log file is completely processed.
	// Repeat this until all commit logs files are done processing (all deleted).

	fn := r.formatActiveFolderEntity(commitChangesLogFolder)

	files, err := getFilesSortedDescByModifiedTime(ctx, fn, logFileExtension, nil)
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, nil
	}

	// Set to false the FailedToReplicate so we can issue a successful Replicate call on StoreRepository & Registry.
	r.ReplicationTrackedDetails.FailedToReplicate = false
	fio := NewFileIO()
	ms := NewManageStoreFolder(fio)
	sr, err := NewStoreRepository(ctx, r, ms, r.l2Cache, 0)
	if err != nil {
		return false, err
	}

	regHashMod := 0
	if regHashMod, err = sr.GetRegistryHashModValue(ctx); err != nil {
		return false, err
	}

	reg := NewRegistry(true, regHashMod, r, r.l2Cache)
	var foundAndProcessed bool
	// Get the oldest first.
	for i := range files {
		filename := files[i].Name()
		foundAndProcessed = true

		ffn := r.formatActiveFolderEntity(fmt.Sprintf("%s%c%s", commitChangesLogFolder, os.PathSeparator, filename))
		ba, err := fio.ReadFile(ctx, ffn)
		if err != nil {
			return false, err
		}

		var logData sop.Tuple[[]sop.StoreInfo, [][]sop.RegistryPayload[sop.Handle]]

		err = encoding.DefaultMarshaler.Unmarshal(ba, &logData)
		if err != nil {
			return false, err
		}

		if logData.First != nil {
			// Ensure Store Repo has the latest Count so we don't need to worry about potential race condition
			// between transaction doing commit logs vs. replication to the reinstated drives.
			for i := range logData.First {
				if sis, _ := sr.getFromCache(ctx, logData.First[i].Name); len(sis) == 1 {
					logData.First[i].Count = sis[0].Count
				}
			}
			if err := sr.Replicate(ctx, logData.First); err != nil {
				return false, err
			}
		}
		if err := reg.Replicate(ctx, logData.Second[0], logData.Second[1], logData.Second[2], logData.Second[3]); err != nil {
			return false, err
		}

		if err := fio.Remove(ctx, ffn); err != nil {
			return false, err
		}
	}

	return foundAndProcessed, nil
}

// turnOnReplication finalizes reinstatement: clears failure flags, updates status on disk and in L2 cache.
func (r *replicationTracker) turnOnReplication(ctx context.Context) error {
	globalReplicationDetailsLocker.Lock()
	defer globalReplicationDetailsLocker.Unlock()

	GlobalReplicationDetails.FailedToReplicate = false
	GlobalReplicationDetails.LogCommitChanges = false

	r.ReplicationTrackedDetails = *GlobalReplicationDetails
	// Update the replication status details.
	if err := r.writeReplicationStatus(ctx, r.formatActiveFolderEntity(replicationStatusFilename)); err != nil {
		return err
	}
	return r.syncWithL2Cache(ctx, true)
}
