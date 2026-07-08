# Federation Poll+Stream Implementation Summary

## What was implemented

This enhancement adds a hybrid poll+stream architecture for daemon-to-HQ federation sync, enabling efficient hourly polling with fast follow-up when changes are detected.

## Core components added

### 1. HQ-side infrastructure

**Federation cursor tracking** ([lib/sop/tools/sopbackend/federation_cursor.go](lib/sop/tools/sopbackend/federation_cursor.go))
- Global versioned cursor that increments when new federation content arrives
- Deterministic hash generation for cursor comparison
- Delta detection against daemon's last known cursor

**Poll endpoint** ([lib/sop/tools/sopbackend/federation_poll_handler.go](lib/sop/tools/sopbackend/federation_poll_handler.go))
- `POST /api/daemon/federation/poll`
- Lightweight change detection
- Returns current cursor and delta status
- Returns a federation whisper payload that can throttle sync frequency and batch size
- Whisper decisions are computed dynamically from cached rate limiter metrics (load percentage >90% blocks sync, >75% moderates it)
- Cached metrics are updated after token consumption for zero-overhead external monitoring
- Avoids transferring full payloads during polling

**Cursor integration** ([lib/sop/tools/sopbackend/hq_sync_handler.go](lib/sop/tools/sopbackend/hq_sync_handler.go))
- Cursor automatically increments when artifacts are stored
- All daemons can detect changes by comparing cursors

### 2. Daemon-side infrastructure

**Poll client** ([lib/sop/cmd/sop-daemon/federation_poll.go](lib/sop/cmd/sop-daemon/federation_poll.go))
- Periodic polling to HQ
- Cursor tracking and comparison
- Configurable poll interval (default: 1 hour)

**Sync flow integration** ([lib/sop/cmd/sop-daemon/auth.go](lib/sop/cmd/sop-daemon/auth.go))
- Poll first before batch sync
- Skip sync if no changes and queue is empty
- Proceed with batch sync if delta exists or local queue has items

**Main loop update** ([lib/sop/cmd/sop-daemon/main.go](lib/sop/cmd/sop-daemon/main.go))
- Uses poll interval when poll client is available
- Falls back to adaptive delay if polling is disabled

### 3. Testing

**Cursor tests** ([lib/sop/tools/sopbackend/federation_cursor_test.go](lib/sop/tools/sopbackend/federation_cursor_test.go))
- Cursor increment and tracking
- Delta detection logic
- Deterministic hash generation

**Poll handler tests** ([lib/sop/tools/sopbackend/federation_poll_handler_test.go](lib/sop/tools/sopbackend/federation_poll_handler_test.go))
- Endpoint behavior
- Delta detection across cursor changes
- Empty cursor handling

## How it works

### Monitoring and CloudWatch follow-up
The poll endpoint now contributes to the same observability surface as the rest of the backend. The immediate next step is to visualize the new federation values in CloudWatch:

- `sop_federation_load_percent` for HQ saturation
- `sop_federation_active_daemons` for fleet size and concurrency
- `sop_federation_global_tokens` for available global rate-limiter headroom
- HTTP request and status metrics to correlate spikes with throttling

Suggested alarm thresholds are:

- load percent above 0.90 for 5 minutes
- global tokens below 10 for 5 minutes
- sustained throttling above 20% of poll requests for 10 minutes

### Normal flow (no changes)
1. Daemon wakes up after 1 hour
2. Polls HQ with its last known cursor
3. HQ responds: `has_delta=false`
4. Daemon checks local queue depth
5. If queue is empty, daemon sleeps for another hour
6. **Result: Zero batch sync overhead when nothing changed**

### Change flow (new content at HQ)
1. Another daemon pushes new artifacts to HQ
2. HQ increments global federation cursor
3. Next daemon polls HQ with old cursor
4. HQ responds: `has_delta=true, stream_endpoint=/api/daemon/federation/stream`
5. Daemon proceeds with batch sync to both push local artifacts and pull new ones
6. Daemon updates its cursor after successful sync

### Federation whisper behavior
The daemon does not treat the poll response as a hard command. Instead it applies a simple decision algorithm:

1. If HQ marks sync as disallowed, the daemon skips the sync attempt and preserves the local queue.
2. If HQ supplies a suggested interval, that value becomes the next sync delay; otherwise the daemon falls back to its adaptive delay.
3. If HQ supplies a batch size, that value becomes the effective batch size; otherwise the daemon uses its existing adaptive batching logic.
4. If HQ supplies a queue-depth hint, the daemon uses it to bound local buffering; otherwise the default queue limit remains in effect.

This gives HQ a lightweight control plane while keeping the system daemon-initiated, conservative, and non-blocking.

### Backward compatibility
- Poll is best-effort, not required
- If poll fails, daemon falls back to normal batch sync
- Daemons without poll support continue using adaptive periodic sync
- HQ serves both polling and non-polling daemons

## Configuration

### Environment variables

**Daemon side:**
- `SOP_FEDERATION_POLL_ENABLED` - Enable/disable polling (default: `true`)
- `SOP_FEDERATION_POLL_INTERVAL` - Poll interval (default: `1h`)
- `SOP_HQ_BASE_URL` - HQ endpoint URL (required for polling)
- `SOP_DAEMON_ID` - Unique daemon identifier

**HQ side:**
- No new configuration required (cursor tracking is automatic)

## Benefits

### Efficiency
- Eliminates unnecessary sync attempts when nothing changed
- Reduces HQ load from 200 daemons × 4 syncs/hour to 200 polls/hour
- Polls are lightweight (< 1KB payload vs. full batch sync)

### Scalability
- 200 daemons polling hourly = ~0.056 req/sec (negligible)
- Batch sync only happens when needed
- HQ can handle thousands of daemons with this model

### Best-effort design
- Federation sync remains non-blocking
- Never interferes with AI Copilot workflows
- Degrades gracefully under failures

### V1 scaling posture
The design targets up to 500 daemons for V1. That limit is intentionally conservative and appropriate for the current daemon-led, throttled model. Beyond that threshold, expansion should follow observed demand and production telemetry rather than speculative over-provisioning.

## Current limitations and future work

### What's not implemented yet
- **Streaming protocol** - poll detects changes, but full streaming delivery is not yet implemented
- **Delta calculation** - `delta_count` is a placeholder (always returns 1 if delta exists)
- **Cursor persistence** - daemon cursor resets on restart (could add file/DB persistence)
- **Replay window** - no cursor history retention or replay capability

### Phase 2 opportunities
- Implement SSE/WebSocket streaming for fast delta delivery after poll
- Add cursor persistence so daemons survive restarts
- Track per-artifact versions for fine-grained delta calculation
- Add observability metrics for poll success rate and cursor tracking

## Verification

All tests passing:
- Daemon package: ✅ `ok github.com/sharedcode/sop/cmd/sop-daemon 22.307s`
- Backend package: ✅ `ok github.com/sharedcode/sop/tools/sopbackend 5.381s`
- Federation cursor tests: ✅
- Federation poll handler tests: ✅

## Integration status

This enhancement is **production-ready as Phase 1**:
- ✅ Cursor tracking works
- ✅ Poll endpoint functional
- ✅ Daemon integration complete
- ✅ Backward compatible
- ✅ Tests passing
- ⏳ Streaming (Phase 2, not yet implemented)
