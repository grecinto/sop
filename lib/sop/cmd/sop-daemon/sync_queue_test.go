package main

import (
	"testing"
	"time"
)

func TestSyncQueueEnqueueDeduplicatesAndTracksDepth(t *testing.T) {
	t.Setenv("SOP_SYNC_QUEUE_ENABLED", "true")

	q := NewSyncQueue()
	q.maxDepth = 2

	if err := q.Enqueue("Shared pattern A", "workflow_pattern", nil); err != nil {
		t.Fatalf("enqueue first artifact: %v", err)
	}
	if err := q.Enqueue("Shared pattern A", "workflow_pattern", nil); err != nil {
		t.Fatalf("enqueue duplicate artifact: %v", err)
	}
	if err := q.Enqueue("Shared pattern B", "repair_strategy", nil); err != nil {
		t.Fatalf("enqueue third artifact: %v", err)
	}

	if got := len(q.queue); got != 2 {
		t.Fatalf("expected queue depth 2 after deduplication and overflow handling, got %d", got)
	}

	if q.totalPromoted != 2 {
		t.Fatalf("expected total_promoted=2, got %d", q.totalPromoted)
	}
}

func TestSyncQueueCircuitBreakerTransitions(t *testing.T) {
	q := NewSyncQueue()
	q.circuitBreakerThreshold = 2
	q.circuitState = CircuitClosed

	q.RecordSyncFailure(500)
	q.RecordSyncFailure(500)

	if q.circuitState != CircuitOpen {
		t.Fatalf("expected circuit state open after threshold failures, got %v", q.circuitState)
	}

	allowed, reason := q.ShouldAttemptSync()
	if allowed {
		t.Fatalf("expected sync attempt to be blocked while circuit is open, got allowed=%v reason=%s", allowed, reason)
	}

	q.circuitOpenedAt = time.Now().Add(-time.Hour)
	q.currentBackoffDelay = 0

	allowed, reason = q.ShouldAttemptSync()
	if !allowed {
		t.Fatalf("expected half-open recovery to allow a test attempt, got allowed=%v reason=%s", allowed, reason)
	}
	if q.circuitState != CircuitHalfOpen {
		t.Fatalf("expected circuit state half-open after recovery window, got %v", q.circuitState)
	}

	q.RecordSyncSuccess()
	if q.circuitState != CircuitClosed {
		t.Fatalf("expected circuit state closed after successful recovery attempt, got %v", q.circuitState)
	}
}

func TestSyncQueueAdaptiveDelayShortensAfterSuccesses(t *testing.T) {
	q := NewSyncQueue()
	q.consecutiveSuccesses = 3
	q.currentBackoffDelay = 0

	delay := q.GetNextSyncDelay()
	if delay >= q.baseSyncInterval {
		t.Fatalf("expected adaptive delay to shrink after consecutive successes, got %s", delay)
	}
}

func TestSyncQueueAppliesThrottleControlFromHQ(t *testing.T) {
	q := NewSyncQueue()
	q.ApplyThrottleControl(&SyncThrottleControl{
		Allowed:             false,
		Reason:              "maintenance",
		SyncIntervalSeconds: 1800,
		BatchSize:           3,
	})

	allowed, reason := q.ShouldAttemptSync()
	if allowed {
		t.Fatalf("expected sync to be blocked by HQ throttle hint")
	}
	if reason == "" {
		t.Fatalf("expected throttle reason to be returned")
	}

	if got := q.GetBatchSize(); got != 3 {
		t.Fatalf("expected batch size 3 from HQ throttle hint, got %d", got)
	}

	if got := q.GetNextSyncDelay(); got != 30*time.Minute {
		t.Fatalf("expected sync delay 30m from HQ throttle hint, got %s", got)
	}
}
