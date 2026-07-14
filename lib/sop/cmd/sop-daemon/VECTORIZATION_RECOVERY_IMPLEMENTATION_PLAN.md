# Vectorization Recovery and Self-Healing Plan

Status: Implemented for V1 (including passive-drive recovery-state rebasing and persisted recovery paths)
Priority: Complete
Area: Runtime-agnostic vectorization recovery

## Objective

Semantic vectorization is now resilient, non-blocking, and recoverable across deployments, including HQ-hosted and local daemon environments. The runtime continues to serve retrieval and workflow use when vectorization fails, records degraded targets, and offers user-driven recovery actions.

## Problem Solved

A failed semantic vectorization path can leave any deployment with incomplete or stale semantic indexes, which hurts retrieval quality and makes the experience feel inconsistent. The V1 implementation addresses that by treating vectorization as a recoverable follow-up step rather than a hard gate on content use.

## V1 Design Principles Applied

1. Persist content first.
   - Category and item writes remain available even if semantic vectorization later fails.

2. Do not block the runtime on vectorization.
   - Vectorization is handled as a follow-up capability rather than a hard gate for local or hosted workflows.

3. Degrade gracefully on first failure.
   - Failed targets are skipped for semantic vectorization so the runtime can continue serving retrieval.

4. Track repeated failures.
   - The runtime counts failures over time and marks the semantic index as degraded when the threshold is crossed.

5. Escalate after repeated failures.
   - Repeated issues surface a recovery prompt to the user or operator.

6. Offer explicit recovery choices.
   - The user can ignore, recover, or clear-and-rebuild.

## Implementation Summary

Runtime-specific behavior is expressed through a policy interface so the core vectorization pipeline stays shared and different deployments can inject their own recovery behavior.

- The shared vectorization pipeline owns the core flow.
- A small policy interface decides how failures are handled.
- The daemon ships with a daemon-specific policy that skips failed targets, records degradation, and escalates after repeated failures.
- The same pattern can be applied in HQ or other runtimes by registering a different policy implementation.

## Scope for V1 (Completed)

### Completed work

- Runtime-agnostic recovery flow for semantic vectorization failures.
- Persisted local state for failed categories/items and recovery status.
- Automatic retry loop with backoff.
- Threshold-based recovery prompt and user actions.
- Logging and regression tests for skip-on-failure, escalation, and recovery behavior.
- SetupWizard-backed recovery storage path selection, defaulting to the passive folder when available, and persistence through config.
- Passive-drive recovery-state cleanup and rebasing during failover and reinstate.

### Remaining follow-up / out-of-scope work

- Cross-runtime or fleet-managed repair queues.
- Advanced index repair heuristics beyond rebuild and clear-and-rebuild.

## Implemented Architecture

### 1. Failure tracking state

The runtime maintains lightweight state for vectorization failures.

Each recorded failure includes:

- KB name
- target type: category, item, or batch
- target ID
- current state: degraded, ignored, or recovered
- failure count
- last error
- last failure timestamp
- retry metadata

This state lives in the runtime storage path and is used to decide when to escalate.

### 2. Failure handling in the write path

When vectorization fails during normal write or category-batch processing:

- the primary content write is preserved,
- the target is marked as degraded,
- the runtime continues processing other work,
- and the failure is recorded for later recovery rather than treated as fatal.

### 3. Degradation and retry policy

The runtime now degrades gracefully on first failure and evaluates repeated failures.

Retry behavior:

- the first failure skips semantic vectorization and keeps the workflow running,
- repeated failures increase the failure count,
- and the runtime avoids repeated immediate retries once degradation is active.

### 4. Recovery prompt and user actions

After repeated failures, the runtime raises a recovery state and presents the user or operator with three recovery choices:

- Ignore for now
- Recover semantic index
- Clear stale indexes and rebuild

## Completion Checklist

### Track 1: Failure-state model and storage

- [x] Define the vectorization-failure state schema.
- [x] Add a persisted store for failure state in the runtime storage path.
- [x] Add state transition helpers for degraded, ignored, and recovered.

### Track 2: Integration into the write path

- [x] Hook vectorization failure handling into the main vectorization flow.
- [x] Ensure content writes are preserved even if vectorization fails.
- [x] Record degraded-state instead of aborting the flow.

### Track 3: Degradation and escalation logic

- [x] Add failure-count tracking and threshold evaluation.
- [x] Mark targets as degraded after repeated failures.
- [x] Avoid repeated immediate retries once degraded.

### Track 4: Recovery escalation and user actions

- [x] Add a user-visible recovery prompt.
- [x] Implement actions for ignore, recover, and clear-and-rebuild.
- [x] Track the user’s recovery choice and update the state.

### Track 5: Observability and validation

- [x] Add logs for skipped vectorization, degraded targets, and escalations.
- [x] Expose degraded-state information for debugging.
- [x] Add regression tests for skip-on-failure, threshold escalation, and recovery prompt behavior.

## Acceptance Criteria

The V1 implementation is complete when:

- a failed semantic vectorization does not block workflows,
- failed targets are tracked for recovery,
- the repair loop retries them later automatically,
- the runtime escalates to a user-visible recovery prompt after repeated failures,
- and the user can choose to ignore, recover, or clear-and-rebuild.

## Notes

The remaining items are future enhancements rather than pending V1 work.
