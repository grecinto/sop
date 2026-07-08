# Federation Sync Throttling Design Summary

## Goal
Protect HQ from bursty daemon traffic while still letting daemons share useful federated artifacts efficiently.

## Design at a Glance
We use a two-sided approach:

1. Daemon-side queueing and adaptive throttling
2. HQ-side rate limiting and circuit breaking
3. A lightweight HQ-originated control whisper that nudges daemon sync cadence and batch size

## Daemon Side
The daemon keeps a local sync queue for promoted artifacts.

- Artifacts are deduplicated before enqueueing.
- Priority is assigned by category (critical, high, normal).
- The queue enforces a max depth and drops older low-priority items when full.
- The daemon sends batches rather than one artifact at a time.
- Batch size is adaptive:
  - smaller during circuit-breaker half-open testing
  - larger under normal operation
- Sync timing is adaptive:
  - success shortens the delay
  - failures increase backoff
  - repeated failures can open the circuit breaker

## HQ Side
HQ applies a token-bucket limiter to protect shared services.

- Limits are enforced per daemon and globally.
- Burst allowance is supported, but sustained traffic is capped.
- Excess traffic returns HTTP 429 with Retry-After and a reason header.

### Cached Load Metrics
The rate limiter maintains cached load state that is updated automatically after every token consumption. This enables lightweight external monitoring without computation overhead:

- **`cachedLoadPercent`**: Current load (0.0 = idle, 1.0 = fully loaded), computed as `1.0 - (available_tokens / burst_capacity)`
- **`cachedActiveDaemons`**: Number of active daemon clients currently tracked
- **`cachedGlobalTokens`**: Available tokens in the HQ-wide global rate limiter bucket
- **`lastMetricsUpdate`**: Timestamp of the last cache refresh

These metrics are the single source of truth for both internal throttle decisions and external monitoring. The poll handler reads from this cache when computing dynamic throttle hints, and external monitoring tools (such as Prometheus scrapers) can call `GetCachedLoadMetrics()` for instant access without triggering recomputation.

## Sync Flow
1. A daemon promotes an artifact into local shared memory.
2. The artifact is also queued for HQ sync.
3. A periodic sync loop dequeues a batch and sends it to HQ.
4. If the batch is empty, the daemon sends a lightweight heartbeat.
5. After a successful push, the daemon may pull back relevant federated content.

## Federation Whisper
The federation whisper is a compact control payload returned by HQ during the poll step. It is intentionally simple and non-blocking: the daemon remains the initiator of sync, but HQ can steer how aggressively that sync runs.

### Whisper fields
- `allowed`: whether the daemon should attempt sync at all
- `reason`: why HQ is allowing or suppressing sync
- `sync_interval_seconds`: suggested delay before the next attempt
- `batch_size`: suggested batch size for the next sync cycle
- `max_queue_depth`: suggested queue depth ceiling for this daemon
- `priority_boost`: whether the daemon should prefer urgent items

### Whisper algorithm
1. The daemon polls HQ with its last known federation cursor.
2. If HQ includes a whisper, the daemon applies it before scheduling the next sync attempt.
3. If `allowed=false`, the daemon skips the sync attempt entirely and leaves the queue intact.
4. If `allowed=true`, the daemon computes the effective delay as:
   - the whisper interval when provided,
   - otherwise the normal adaptive interval derived from recent success/failure history.
5. The batch size is taken from the whisper when present; otherwise the daemon uses its normal adaptive size, with smaller batches during circuit-breaker half-open testing.
6. Queue depth is bounded by the whisper's `max_queue_depth` when supplied, otherwise the daemon uses its default queue limit.
7. On sync success, backoff is reset and the daemon can become more aggressive again; on failure, backoff grows and the circuit breaker may open.

This keeps the control plane explicit and easy to reason about while preserving the daemon-initiated, best-effort nature of federation sync.

## Why This Works Well
- Prevents overload on HQ.
- Avoids duplicate or noisy sync traffic.
- Preserves important artifacts through prioritization.
- Degrades gracefully under failures.
- Keeps the implementation simple and testable.

## V1 Scaling Posture
For V1, the system is sized for up to 500 daemons. That is a comfortable operating envelope for the current design because the traffic is naturally tapered over time and can be safely throttled by HQ when load rises.

Beyond that point, scaling should be driven by observed demand and actual fleet behavior. If the system proves to need more throughput, the next step is not to switch the architecture wholesale, but to scale HQ capacity and tune the control policy using real metrics.

## When to Evolve Further
If federation sync becomes truly real-time or very high volume, the next evolution would be a worker-pool or channel-driven pipeline with backpressure. For the current use case, the existing queued batch design is the better fit.

## Monitoring and CloudWatch next steps
The throttling logic is now instrumented with cached load state and exposed through the shared /metrics endpoint. The next operational step is to wire those values into CloudWatch dashboards and alarms:

- `sop_federation_load_percent`: line chart for HQ saturation; alert when it stays above 0.90 for 5 minutes.
- `sop_federation_active_daemons`: line chart for fleet concurrency; alert on sudden growth spikes.
- `sop_federation_global_tokens`: line chart for global token headroom; alert when it drops below 10 for 5 minutes.
- `sop_http_requests_total` and `sop_http_response_status_total`: support panels for correlating traffic spikes with throttling behavior.
- Future throttle decision counters: add a counter such as `sop_federation_throttle_decisions_total{reason="..."}` so dashboards can show blocked vs allowed sync decisions over time.
