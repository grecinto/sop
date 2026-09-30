# Federation and HQ Sync

**Status checked:** 2026-09-29. Federation is not a single on/off feature; local retrieval, startup seeding, queue-based HQ sync, and automatic learning promotion have different activation states.

## Current Runtime

- **Startup seeding:** the daemon seeds initial federated content after provisioning establishes a usable system database.
- **Local retrieval:** Ask context can retrieve local shared-memory/federated content and federated Space context.
- **HQ sync loop:** the daemon starts a periodic `startHQSyncLoop`. When an HQ endpoint is configured, it polls for deltas when a poll client is available, respects throttle/circuit controls, sends queued shared-memory batches or a heartbeat, and pulls from HQ after successful batch exchange.
- **Queue integration:** a global sync queue exists, but automatic registration of the shared-memory sync enqueuer is commented out in `GetGlobalSyncQueue`.
- **Ask-derived promotion:** the `promoteSharedMemoryArtifactForAsk` helper remains in `rag.go`, but its Ask completion call sites are commented out. Do not claim that each successful Ask is automatically promoted or shared to HQ.
- **HQ ingestion:** the shared backend still has handlers for queued sync payloads, including versioned V2 workflow artifacts.
- **Space federation:** Space import/retrieval and derived-content flows are separate from the shared-memory queue; see `lib/sop/cmd/sop-daemon/space_federation.go` and `rag.go`.

Therefore: HQ sync infrastructure is present and can exchange configured/queued data, but automatic Ask-to-fleet learning enqueue/promotion is not active in the current path. Actual exchange depends on endpoint, credentials, poll-client configuration, and queued data.

## Data and Privacy Boundaries

Federated content is distinct from raw conversation history. Do not infer from the presence of a category or sync endpoint that its content is currently queued or shared. Daemon privacy configuration/filtering is initialized at startup; sharing behavior must be checked at the active enqueue and transmission call sites.

For privacy-input handling see [SECURE_INPUT_HANDLING.md](SECURE_INPUT_HANDLING.md). For memory retention and retrieval see [LONG_TERM_MEMORY.md](LONG_TERM_MEMORY.md). Older category and seed-pattern plans are summarized as history in [FEDERATED_CONTENT_STRATEGY.md](FEDERATED_CONTENT_STRATEGY.md).

## Verification Pointers

- Daemon loop and startup seeding: `lib/sop/cmd/sop-daemon/main.go`
- HQ push/pull and queue behavior: `lib/sop/cmd/sop-daemon/auth.go`
- Ask retrieval/promotion call sites: `lib/sop/cmd/sop-daemon/rag.go`
- HQ payload ingestion: `lib/sop/tools/sopbackend/hq_sync_handler.go`
- Space federation: `lib/sop/cmd/sop-daemon/space_federation.go`

Check these files and tests before describing any behavior as automatically shared or production-enabled; the older plans below are not a live rollout tracker.
