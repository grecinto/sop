# Federation Poll+Stream Design

## Overview
This design combines efficient hourly polling with opportunistic streaming to balance low overhead with fast propagation when federation content changes.

## Architecture

### Three-layer model
1. **Hourly poll** - lightweight change detection
2. **Opportunistic stream** - fast delta delivery when changes exist
3. **Fallback batch** - resilient full sync if streaming fails

### Design principles
- Federation sync is best-effort, not critical path
- Never block AI Copilot workflows
- Stream only when daemon is idle enough
- Degrade gracefully under load

## State model

Each daemon tracks:
- `lastPollTime` - when it last polled HQ
- `lastSeenCursor` - latest cursor returned by HQ
- `lastAppliedCursor` - latest cursor successfully applied locally
- `streamActive` - whether a stream session is open

HQ tracks:
- Global federation cursor/version
- Per-artifact timestamps or version numbers
- Active stream sessions per daemon

## Poll flow

Every hour:
1. Daemon sends lightweight poll to `/api/daemon/federation/poll`
2. Request includes daemon's `lastAppliedCursor`
3. HQ responds with:
   - `currentCursor` - latest federation state version
   - `hasDelta` - boolean indicating if new content exists
   - `deltaCount` - approximate number of new artifacts
   - `streamEndpoint` - URL to open stream if delta exists

If `hasDelta` is false, daemon sleeps until next poll.

## Stream flow

When delta is detected:
1. Daemon checks if it's idle enough (no active Copilot requests)
2. If idle, opens SSE stream to `/api/daemon/federation/stream?from_cursor=X`
3. HQ streams events:
   ```json
   {
     "event_id": "evt_123",
     "cursor": "c_456",
     "kind": "artifact_created",
     "artifact": {...},
     "timestamp": "2026-07-07T12:00:00Z"
   }
   ```
4. Daemon applies each event locally
5. Daemon updates `lastAppliedCursor` after successful application
6. Stream closes naturally when caught up, or daemon cancels if it becomes busy

## Fallback strategy

If streaming fails or is suppressed:
1. Fall back to existing batch sync mechanism
2. Use normal adaptive backoff and circuit breaker
3. Retry poll at next interval

## Resource constraints

To protect the main AI workflow:
- Stream runs in separate goroutine with low priority
- Cancelled immediately if Copilot request arrives
- Max bandwidth: 1MB/sec per daemon stream
- Max concurrent streams per HQ: 50
- Stream idle timeout: 5 minutes
- Replay window: 7 days of cursors

## Scale considerations

For 200 daemons:
- Poll traffic: ~200 requests/hour = 0.056 req/sec (negligible)
- Stream traffic: burst when changes occur, then idle
- HQ can batch-notify multiple daemons of same cursor efficiently

## Implementation phases

### Phase 1: Core infrastructure
- Add cursor tracking state
- Implement poll endpoint
- Add tests

### Phase 2: Streaming
- Implement SSE stream endpoint
- Add daemon stream manager
- Add cancellation logic

### Phase 3: Production hardening
- Add metrics and observability
- Tune resource limits
- Load test with 200 daemons

## Backward compatibility

The enhancement is additive:
- Existing batch sync remains functional
- Daemons without poll support continue using periodic batch
- HQ serves both protocols simultaneously
