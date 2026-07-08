package sopbackend

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstAndBlocksOverLimit(t *testing.T) {
	t.Setenv("SOP_HQ_RATE_LIMITING_ENABLED", "true")
	t.Setenv("SOP_HQ_PER_DAEMON_RATE", "60")
	t.Setenv("SOP_HQ_PER_DAEMON_BURST", "2")
	t.Setenv("SOP_HQ_GLOBAL_RATE", "1000")
	t.Setenv("SOP_HQ_GLOBAL_BURST", "2")

	rl := NewRateLimiter()

	for i := 0; i < 2; i++ {
		allowed, _, reason := rl.Allow("daemon-a")
		if !allowed {
			t.Fatalf("expected request %d to be allowed, got reason=%s", i+1, reason)
		}
	}

	allowed, retryAfter, reason := rl.Allow("daemon-a")
	if allowed {
		t.Fatalf("expected third request to be rejected, got allowed=true reason=%s retryAfter=%s", reason, retryAfter)
	}
	if retryAfter <= 0 {
		t.Fatalf("expected retryAfter to be positive, got %s", retryAfter)
	}
}

func TestRateLimiterMetricsIncludeDaemonStats(t *testing.T) {
	rl := NewRateLimiter()
	allowed, _, _ := rl.Allow("daemon-b")
	if !allowed {
		t.Fatal("expected first request to be allowed")
	}

	metrics := rl.GetMetrics()
	stats, ok := metrics["per_daemon_stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected per_daemon_stats map, got %T", metrics["per_daemon_stats"])
	}
	if _, exists := stats["daemon-b"]; !exists {
		t.Fatalf("expected daemon-b stats to be present, got %v", stats)
	}
}

func TestTokenBucketTimeUntilTokenIsPositiveWhenEmpty(t *testing.T) {
	bucket := &tokenBucket{tokens: 0, capacity: 2, refillRate: 1}
	delay := bucket.timeUntilToken()
	if delay <= 0 {
		t.Fatalf("expected positive delay for an empty bucket, got %s", delay)
	}
	if delay > time.Second {
		t.Fatalf("expected delay close to 1s, got %s", delay)
	}
}
