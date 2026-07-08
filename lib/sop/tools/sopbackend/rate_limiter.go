package sopbackend

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter provides token-bucket rate limiting for HQ endpoints.
type RateLimiter struct {
	mu               sync.Mutex
	perDaemonBuckets map[string]*tokenBucket
	globalBucket     *tokenBucket
	perDaemonRate    int // requests per minute per daemon
	perDaemonBurst   int // max burst per daemon
	globalRate       int // requests per minute globally
	globalBurst      int // max burst globally
	cleanupInterval  time.Duration
	lastCleanup      time.Time
}

// tokenBucket implements a simple token bucket algorithm.
type tokenBucket struct {
	tokens       float64
	capacity     float64
	refillRate   float64 // tokens per second
	lastRefill   time.Time
	requestCount int64
	lastRequest  time.Time
}

// NewRateLimiter creates a rate limiter with configuration from environment variables.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		perDaemonBuckets: make(map[string]*tokenBucket),
		perDaemonRate:    getEnvInt("SOP_HQ_PER_DAEMON_RATE", 60),  // 60 req/min per daemon
		perDaemonBurst:   getEnvInt("SOP_HQ_PER_DAEMON_BURST", 10), // 10 req burst
		globalRate:       getEnvInt("SOP_HQ_GLOBAL_RATE", 1000),    // 1000 req/min globally
		globalBurst:      getEnvInt("SOP_HQ_GLOBAL_BURST", 100),    // 100 req burst
		cleanupInterval:  5 * time.Minute,
		lastCleanup:      time.Now(),
	}
}

// Allow checks if a request from the given daemon should be allowed.
// Returns (allowed bool, retryAfter time.Duration, reason string).
func (rl *RateLimiter) Allow(daemonID string) (bool, time.Duration, string) {
	if !getEnvBool("SOP_HQ_RATE_LIMITING_ENABLED", true) {
		return true, 0, "rate limiting disabled"
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Cleanup old buckets periodically
	if time.Since(rl.lastCleanup) > rl.cleanupInterval {
		rl.cleanup()
	}

	// Initialize global bucket if needed
	if rl.globalBucket == nil {
		rl.globalBucket = &tokenBucket{
			tokens:     float64(rl.globalBurst),
			capacity:   float64(rl.globalBurst),
			refillRate: float64(rl.globalRate) / 60.0, // convert per-minute to per-second
			lastRefill: time.Now(),
		}
	}

	// Check global rate limit first
	rl.globalBucket.refill()
	if rl.globalBucket.tokens < 1.0 {
		retryAfter := rl.globalBucket.timeUntilToken()
		return false, retryAfter, "global rate limit exceeded"
	}

	// Get or create per-daemon bucket
	bucket, exists := rl.perDaemonBuckets[daemonID]
	if !exists {
		bucket = &tokenBucket{
			tokens:     float64(rl.perDaemonBurst),
			capacity:   float64(rl.perDaemonBurst),
			refillRate: float64(rl.perDaemonRate) / 60.0, // convert per-minute to per-second
			lastRefill: time.Now(),
		}
		rl.perDaemonBuckets[daemonID] = bucket
	}

	// Check per-daemon rate limit
	bucket.refill()
	if bucket.tokens < 1.0 {
		retryAfter := bucket.timeUntilToken()
		return false, retryAfter, fmt.Sprintf("per-daemon rate limit exceeded for %s", daemonID)
	}

	// Consume tokens
	rl.globalBucket.tokens -= 1.0
	bucket.tokens -= 1.0
	bucket.requestCount++
	bucket.lastRequest = time.Now()

	return true, 0, "allowed"
}

// refill adds tokens to the bucket based on time elapsed.
func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()

	// Add tokens based on elapsed time
	tb.tokens += elapsed * tb.refillRate

	// Cap at capacity
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	tb.lastRefill = now
}

// timeUntilToken calculates how long until one token is available.
func (tb *tokenBucket) timeUntilToken() time.Duration {
	if tb.refillRate <= 0 {
		return time.Hour // fallback
	}
	tokensNeeded := 1.0 - tb.tokens
	if tokensNeeded <= 0 {
		return 0
	}
	seconds := tokensNeeded / tb.refillRate
	return time.Duration(seconds * float64(time.Second))
}

// cleanup removes stale per-daemon buckets.
func (rl *RateLimiter) cleanup() {
	now := time.Now()
	staleThreshold := 30 * time.Minute

	for daemonID, bucket := range rl.perDaemonBuckets {
		if now.Sub(bucket.lastRequest) > staleThreshold {
			delete(rl.perDaemonBuckets, daemonID)
		}
	}

	rl.lastCleanup = now
}

// GetMetrics returns current rate limiter metrics.
func (rl *RateLimiter) GetMetrics() map[string]interface{} {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	metrics := map[string]interface{}{
		"active_daemons":   len(rl.perDaemonBuckets),
		"per_daemon_rate":  rl.perDaemonRate,
		"per_daemon_burst": rl.perDaemonBurst,
		"global_rate":      rl.globalRate,
		"global_burst":     rl.globalBurst,
	}

	if rl.globalBucket != nil {
		metrics["global_tokens_available"] = rl.globalBucket.tokens
	}

	// Per-daemon stats
	perDaemonStats := make(map[string]interface{})
	for daemonID, bucket := range rl.perDaemonBuckets {
		perDaemonStats[daemonID] = map[string]interface{}{
			"tokens_available": bucket.tokens,
			"request_count":    bucket.requestCount,
			"last_request":     bucket.lastRequest.Format(time.RFC3339),
		}
	}
	metrics["per_daemon_stats"] = perDaemonStats

	return metrics
}

// RateLimitMiddleware returns an HTTP middleware that enforces rate limits.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract daemon ID from request (could be from JSON body, header, or query param)
			daemonID := extractDaemonID(r)
			if daemonID == "" {
				daemonID = "unknown-daemon"
			}

			allowed, retryAfter, reason := limiter.Allow(daemonID)
			if !allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
				w.Header().Set("X-RateLimit-Reason", reason)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)

				response := fmt.Sprintf(`{
					"error": "rate limit exceeded",
					"reason": "%s",
					"retry_after_seconds": %d,
					"daemon_id": "%s"
				}`, reason, int(retryAfter.Seconds()), daemonID)

				w.Write([]byte(response))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractDaemonID attempts to extract the daemon ID from the request.
func extractDaemonID(r *http.Request) string {
	// Try X-Daemon-ID header first
	if daemonID := r.Header.Get("X-Daemon-ID"); daemonID != "" {
		return strings.TrimSpace(daemonID)
	}

	// Try query parameter
	if daemonID := r.URL.Query().Get("daemon_id"); daemonID != "" {
		return strings.TrimSpace(daemonID)
	}

	// For sync/retrieve endpoints, daemon_id is in the JSON body
	// We'll parse it in the handler and store it in context or accept "unknown"
	// For now, use remote addr as fallback identifier
	return r.RemoteAddr
}

// Helper functions

func getEnvInt(key string, defaultValue int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return defaultValue
	}
	intVal, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return intVal
}

func getEnvBool(key string, defaultValue bool) bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if val == "" {
		return defaultValue
	}
	return val == "true" || val == "1" || val == "yes"
}
