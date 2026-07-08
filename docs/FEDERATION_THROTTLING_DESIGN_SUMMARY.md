# Federation Sync Throttling Design Summary

## Goal
Protect HQ from bursty daemon traffic while still letting daemons share useful federated artifacts efficiently.

## Design at a Glance
We use a two-sided approach:

1. Daemon-side queueing and adaptive throttling
2. HQ-side rate limiting and circuit breaking

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

## Sync Flow
1. A daemon promotes an artifact into local shared memory.
2. The artifact is also queued for HQ sync.
3. A periodic sync loop dequeues a batch and sends it to HQ.
4. If the batch is empty, the daemon sends a lightweight heartbeat.
5. After a successful push, the daemon may pull back relevant federated content.

## Why This Works Well
- Prevents overload on HQ.
- Avoids duplicate or noisy sync traffic.
- Preserves important artifacts through prioritization.
- Degrades gracefully under failures.
- Keeps the implementation simple and testable.

## When to Evolve Further
If federation sync becomes truly real-time or very high volume, the next evolution would be a worker-pool or channel-driven pipeline with backpressure. For the current use case, the existing queued batch design is the better fit.
