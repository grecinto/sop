# Operations Guide

This guide covers the operational aspects of running SOP in production, including failover handling, connection management, and backup strategies.

## Connection Management

SOP relies on long-lived (pooled) connections to Redis and Cassandra (for `incfs`).

> **Note**: If running in **Standalone Mode** (using `sop.InMemory` cache), Redis is not required, and this section can be ignored.

> **Note**: SOP does **not** require Redis data persistence (RDB/AOF). Redis is used for ephemeral locking and caching. If Redis restarts, SOP detects the change and recovers safely.

### Redis
*   **Pool**: SOP uses the standard `go-redis` client.
*   **Configuration**: Ensure your `redis.Options` includes:
    *   `PoolSize`: Set according to your concurrency needs (e.g., 10 * CPU cores).
    *   `MinIdleConns`: Keep some connections warm to avoid latency spikes.
    *   `ReadTimeout` / `WriteTimeout`: Tune these to avoid premature timeouts during heavy load.

### Cassandra (`incfs`)
*   **Consistency**: SOP typically uses `LOCAL_QUORUM` for strong consistency.
*   **Keyspace**: Ensure the keyspace is created with an appropriate replication strategy (e.g., `NetworkTopologyStrategy`) for your cluster.

## Failover Logic

SOP includes sophisticated logic to handle storage failures transparently.

### "Failover Qualified" I/O Errors

Not all errors trigger a failover. SOP distinguishes between transient errors (retryable) and permanent hardware/filesystem failures.

**Triggers for Failover:**
*   `syscall.EIO` (Input/output error)
*   `syscall.EROFS` (Read-only file system)
*   `syscall.ENOSPC` (No space left on device)
*   Specific Linux error codes for media failure (e.g., `EUCLEAN`).

**Behavior:**
1.  **Detection**: When a "Qualified" error occurs during a write.
2.  **Switch**: The system automatically marks the current storage path as "Passive" and switches to the configured "Active" standby path.
3.  **Recovery**: Operations continue on the new path. The failed path requires manual intervention or auto-repair (if configured).

### Monitoring

*   **Logs**: Watch for `[Failover]` tagged logs.
*   **Metrics**: Monitor the `sop_failover_count` metric (if you have instrumented your application).

### CRUD vs. recomputable workflows

The storage-health policy is intentionally not an automatic repair policy for primary CRUD writes.

- Primary CRUD operations (create/update/delete for categories, items, and other data-path writes) should fail fast and surface the error to the caller.
- Repeated failover-qualified errors for a path should still be counted and used to mark that path degraded or read-only.
- Higher-level workflows that are recomputable, such as semantic vectorization, may opt into a bounded retry loop only when the shared storage-health policy says auto-retry is still allowed.
- Read-only or write-protected storage should disable those higher-level retries immediately.

In practice, this means the data layer owns the storage-health state machine, while higher-level workflows decide whether a retry is appropriate for their own semantics.

### Recent failover and recovery-state refinements

The current implementation uses the shared storage-health policy for more than just status reporting:

- Failover-qualified I/O errors are no longer treated as an immediate failover signal on the first occurrence. The storage layer records repeated failures for the affected path and only escalates to failover once the shared threshold is crossed.
- Irreparable conditions such as read-only, no-device, no-medium, no-space, quota, and similar media or filesystem failures bypass that threshold and fail over immediately because staying on the broken path is unsafe.
- Recovery-state directories used for vectorization repair are treated as lifecycle-sensitive. During failover they are deleted only when the recovery path clearly belongs to the same drive as the passive folder; otherwise the dedicated recovery location is preserved.
- During reinstate, SOP can re-base the recovery-state path to the replacement passive drive, update the runtime config, and persist the new location so later recovery and retry flows continue to use the correct folder.

This keeps the policy conservative for primary data-path writes while still allowing recomputable workflows such as semantic vectorization to recover automatically when the storage layer says it is safe to do so.

## Drive Failure Handling and Reinstatement

When a drive fails, the recovery flow depends on whether the failure affected the Active/Passive metadata path or the EC-backed blob path.

### General degraded-path policy

The shared policy should apply across single-drive, replicated, and EC-backed deployments, and it belongs in the storage/data layer rather than in the UI or agent layer.

1. Treat failover-qualified I/O errors as a first-class degraded-path signal rather than an endless retry loop.
2. Count repeated failures against the affected path.
3. Mark the path as degraded once the threshold is crossed.
4. If the storage layer reports write-protection or read-only behavior, escalate to a read-only state immediately because writes are no longer safe.
5. Surface the storage-health transition through the daemon recovery/status flow so operators can see it promptly.
6. Let the storage topology decide the recovery action: replication and EC can use the underlying failover or repair path, while single-drive setups require conservative write-stopping and operator intervention.

The data layer owns the policy and the state machine. Higher layers, including RAG/ReAct flows, the daemon, and application-facing APIs, consume that state and translate it into prompts, warnings, or structured errors. That means a RAG loop can ask the user to intervene, while a B-tree or SQL-style caller can receive an appropriate error and health signal without re-implementing any storage policy logic.

A useful architectural boundary is:
- **Data layer**: owns storage semantics, degraded/read-only policy, health-state transitions, metadata, and recovery bookkeeping.
- **Application layer**: owns retries, recovery lifecycle orchestration, end-user alerting/prompting, operator guidance, and action selection for the current workflow.

This split keeps the policy consistent across all data access paths while letting the application layer tailor the experience for background services, UI workflows, and AI-assisted interactions.

The threshold should remain conservative and shared:

- **General threshold**: trigger degraded-state handling after **three consecutive failover-qualified errors** (or three within a short window) for a path.
- **Read-only escalation**: if the storage layer explicitly indicates read-only or write-protected behavior, transition to read-only immediately regardless of the previous count.

This keeps the policy consistent across all layouts while still respecting the difference in recovery options. A single failure should not be treated as a degraded condition by itself; that is too aggressive for replication and unnecessary for a single drive. The goal is to avoid both flapping and silent data risk while making the recovery decision only once the evidence is strong enough.

### 1) Replace the failed drive

1. Replace or reformat the failed disk and mount it at the same filesystem path that SOP expects.
2. Ensure the mount point has the correct ownership and permissions so SOP can read and write the store directories.
3. If the failed drive was part of an EC-protected blob set, keep the service running in degraded mode if parity is still sufficient. SOP can continue serving reads and writes while data is reconstructed.

### 2) Reinstatement after replacement

After the replacement drive is back online, run reinstate so the passive side is brought back into replication.

The reinstate flow will:
- copy the registry and StoreRepository metadata into the reinstated passive side,
- walk the blob data and force reads so Erasure Coding repairs any missing or corrupted shards on the replacement drive, and
- fast-forward any deltas that arrived while the reinstatement was in progress.

> No manual shard copy is required. The reinstate path reuses the existing EC repair logic rather than copying raw shards.

### 3) Trigger reinstate from the command line

There is no separate `sop-admin` subcommand for this yet. The simplest operational approach is to invoke the public Go API from a small helper binary or from your application process.

```go
package main

import (
    "context"
    "fmt"

    "github.com/sharedcode/sop"
    "github.com/sharedcode/sop/database"
)

func main() {
    err := database.ReinstateFailedDrives(context.Background(), sop.DatabaseOptions{
        StoresFolders: []string{"/srv/sop/drive-a", "/srv/sop/drive-b"},
        CacheType:     sop.Redis,
    })
    if err != nil {
        panic(err)
    }
    fmt.Println("reinstate complete")
}
```

Run it with:

```bash
go run ./reinstatement.go
```

If you are already running an application that owns the SOP configuration, call the same API from that process instead of creating a separate helper.

### 4) What to do after reinstate completes

1. Watch the replication status until the `FailedToReplicate` flag is cleared.
2. Restart or reload the relevant SOP service/process if it had to be restarted during the failure window, so it reopens the restored paths cleanly.
3. Continue monitoring logs and metrics for follow-up failures, especially if a second drive fails before redundancy is fully restored.

> TODO: Before going live, run a quick non-production recovery rehearsal for a real drive replacement/reinstate cycle so the operational steps are proven end to end.

## Backup & Restore

### Hybrid Backend (`incfs`)

Backing up a hybrid system requires coordination.

1.  **Snapshot Registry (Cassandra)**:
    *   Use `nodetool snapshot` to capture the state of the Cassandra keyspace.
2.  **Snapshot Blob Store (Filesystem)**:
    *   Use filesystem snapshots (e.g., ZFS, LVM, or cloud volume snapshots) to capture the data directory.
3.  **Consistency**:
    *   If possible, pause writes during the snapshot window to ensure the Registry and Blob Store are aligned.
    *   If zero-downtime is required, snapshot Cassandra *first*, then the Filesystem. SOP's Copy-On-Write nature means old blobs (referenced by the older Cassandra snapshot) will still exist on disk, ensuring a consistent point-in-time restore.

### Restore Procedure

1.  Stop the application.
2.  Restore the Cassandra keyspace.
3.  Restore the Filesystem data.
4.  Start the application.
5.  **Run Integrity Check**: Use SOP's internal tools to verify that all Registry entries point to valid blobs.

## Data Management Suite (SOP Web UI)

SOP includes an HTTP Server and Web UI that functions as a database management suite. It allows you to:
*   **Perform Full CRUD**: Create, Read, Update, and Delete records in any B-Tree.
*   **Manage Any Database**: Works with any SOP store, regardless of the language or data type used.
*   **Leverage Rich Indexing**: Utilize `IndexSpecification` to define and search on compound indexes with multiple fields and custom sort orders, giving you RDBMS-like power.
*   **Debug & Inspect**: Visually verify data integrity and structure.

> **Note on Architecture**: This tool is **not** a central database server. In SOP's masterless architecture, this UI is another client node. You can run it locally to manage a remote production cluster, or deploy it as a sidecar. It connects directly to the storage layer and respects the same ACID guarantees as other SOP clients.

### Running the Management Suite

You can run the tool directly from the source:

```bash
# Point to your SOP registry folder
go run ./tools/httpserver -registry /path/to/your/sop/data
```

Access the UI at `http://localhost:8080`.

### Key Features
*   **Store Listing**: See all B-Trees in your registry.
*   **Rich Search**: Search by key prefix or complex multi-field queries (if using `IndexSpecification`).
*   **JSON Editor**: View and edit complex value objects as formatted JSON.
*   **Bulk Friendly**: Pagination and efficient loading for large datasets.
*   **Safe Schema Updates**: Admins can securely unlock and modify Index/CEL expressions on live stores.

For more details, see the [SOP Data Manager Documentation](tools/httpserver/README.md).

## Troubleshooting & Best Practices

### Cluster reboot procedure
When rebooting an entire cluster running applications that use SOP, follow this order to avoid stale locks and ensure clean recovery:

1) Gracefully stop all apps that use SOP across the cluster.
2) Stop the Redis service(s) used by these SOP apps.
3) Reboot hosts if needed (or proceed directly if not).
4) Start the Redis service(s) first and verify they are healthy.
5) Start the apps that use SOP.

Notes:
- SOP relies on Redis for coordination (locks and recovery bookkeeping). Bringing Redis up before SOP apps prevents unnecessary failovers or stale-lock handling during app startup.
- If any node was force-killed, SOP’s stale-lock and rollback paths will repair on next write; starting Redis first ensures the required state is available.

### Clustered Mode: Data Deletion

When running SOP in **Clustered Mode** (using Redis + Disk Storage), it is critical to maintain synchronization between the persistent data on disk and the ephemeral locks/cache in Redis.

**Recommended Practice:**
*   Always use the SOP API (e.g., `RemoveBtree`, `DeleteDatabase`, or `RemoveStore`) to delete data.
*   These methods automatically handle the cleanup of both the physical files on disk and the corresponding entries in Redis (cache keys, locks, registry entries).

**Manual Deletion (Development Only):**
If you must delete the data files manually (e.g., during local development or a hard reset):
1.  **Stop all Applications**: Ensure no SOP processes (services, CLI, Data Manager) are running.
2.  **Delete Files**: Remove the store folders/files from the disk.
3.  **Flush Redis**: You **MUST** run `FLUSHALL` on your Redis instance immediately after deleting the files.
    ```bash
    redis-cli flushall
    ```

**Why this is necessary**:
Redis maintains locks and cached metadata with a default timeout (typically 15 minutes). If you delete the files but leave the Redis keys active, any new application instance you start will see "ghost" locks or stale metadata, preventing it from recreating the stores or acquiring locks until the timeout expires.
