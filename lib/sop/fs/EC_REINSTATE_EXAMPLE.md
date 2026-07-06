# Erasure Coding Shard Repair During Drive Reinstatement

## Overview

When a drive fails and is replaced, the reinstate process now automatically repairs missing EC shards by:
1. **Registry files**: Copied and fast-forwarded using existing replication logs
2. **Blob EC shards** (NEW): Reconstructed by reading all blobs through the EC repair-on-read path

## What Gets Repaired

The EC repair process handles:
- **B-tree node blobs**: All EC-sharded node data
- **Value segment blobs**: All EC-sharded value data (when `IsValueDataInNodeSegment=false`)

Both use the same `BlobTable` directory, so a single directory walk repairs everything.

## Usage

```go
import (
    "context"
    "github.com/sharedcode/sop"
    "github.com/sharedcode/sop/infs"
)

func reinstateFailedDrive(activePath, passivePath string) error {
    ctx := context.Background()
    
    // After replacing the failed drive, call reinstate
    err := infs.ReinstateFailedDrives(
        ctx,
        []string{activePath, passivePath},
        sop.Redis, // or sop.InMemory
    )
    if err != nil {
        return fmt.Errorf("reinstate failed: %w", err)
    }
    
    // All EC shards are now repaired and replication is back online
    return nil
}
```

## Implementation Details

### Separation of Concerns

1. **Registry Reinstatement** (existing code):
   - Handled by `copyStores()` → copies registry segment files
   - Handled by `fastForward()` → applies registry changes from commit logs

2. **Blob EC Repair** (new code in this feature):
   - Handled by `repairErasureCodingShards()` 
   - Walks `BlobTable` directories across all stores
   - Reads each blob → triggers EC decode → reconstructs missing shards → rewrites them

### Algorithm

```
ReinstateFailedDrives():
  1. Enable commit-change logging
  2. copyStores() ← Registry files copied here
  3. repairErasureCodingShards() ← NEW: EC blob repair
     - Enumerate all stores
     - For each store:
       - Walk BlobTable directory
       - Collect all blob UUIDs from shard filenames
       - Read each blob (triggers EC repair in GetOne())
  4. fastForward() ← Registry changes applied here
  5. Turn replication back on
  6. Final fastForward()
```

### Key Design Decisions

- **Reuses existing repair path**: No new EC reconstruction logic, leverages battle-tested `GetOne()` + `RepairCorruptedShards`
- **Single directory scan**: Both nodes and values live under `BlobTable`, handled together
- **Ignores read errors**: If blob is unrecoverable, it was already lost (not a reinstate failure)
- **Handles empty stores**: Gracefully skips stores with no blob data yet

## Testing

Run the comprehensive test:
```bash
cd lib/sop/fs
go test -v -run TestReinstateFailedDrives_RepairsErasureCodingShards
```

Run the public API tests:
```bash
cd lib/sop/infs
go test -v -run Reinstate
```
