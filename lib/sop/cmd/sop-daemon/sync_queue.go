package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SyncPriority defines the urgency of an artifact sync.
type SyncPriority int

const (
	PriorityNormal SyncPriority = iota
	PriorityHigh
	PriorityCritical
)

// QueuedArtifact represents an artifact pending sync to HQ.
type QueuedArtifact struct {
	Text       string
	Category   string
	Metadata   map[string]any
	Priority   SyncPriority
	QueuedAt   time.Time
	Similarity float64 // For deduplication tracking
}

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// SyncThrottleControl is a hint from HQ about how aggressively the daemon should sync.
type SyncThrottleControl struct {
	Allowed             bool   `json:"allowed"`
	Reason              string `json:"reason,omitempty"`
	SyncIntervalSeconds int    `json:"sync_interval_seconds,omitempty"`
	BatchSize           int    `json:"batch_size,omitempty"`
	MaxQueueDepth       int    `json:"max_queue_depth,omitempty"`
	PriorityBoost       bool   `json:"priority_boost,omitempty"`
}

// SyncQueue manages artifact queueing, throttling, and circuit breaking for HQ sync.
type SyncQueue struct {
	mu                      sync.Mutex
	queue                   []*QueuedArtifact
	maxDepth                int
	consecutiveFailures     int
	consecutiveSuccesses    int
	circuitState            CircuitState
	circuitOpenedAt         time.Time
	lastSyncAttempt         time.Time
	lastSyncSuccess         time.Time
	currentBackoffDelay     time.Duration
	baseSyncInterval        time.Duration
	maxBackoffDuration      time.Duration
	circuitBreakerThreshold int
	throttleControl         *SyncThrottleControl

	// Metrics
	totalPromoted      int64
	totalSyncAttempts  int64
	totalSyncSuccesses int64
	totalSyncFailures  int64
	totalDropped       int64
}

// NewSyncQueue creates a new sync queue with configuration from environment variables.
func NewSyncQueue() *SyncQueue {
	return &SyncQueue{
		queue:                   make([]*QueuedArtifact, 0),
		maxDepth:                getEnvInt("SOP_MAX_QUEUE_DEPTH", 1000),
		circuitState:            CircuitClosed,
		baseSyncInterval:        getEnvDuration("SOP_BASE_SYNC_INTERVAL", 15*time.Minute),
		maxBackoffDuration:      getEnvDuration("SOP_MAX_BACKOFF_DURATION", 1*time.Hour),
		circuitBreakerThreshold: getEnvInt("SOP_CIRCUIT_BREAKER_THRESHOLD", 5),
		currentBackoffDelay:     0,
	}
}

// Enqueue adds an artifact to the sync queue with privacy filtering, deduplication and priority assignment.
func (q *SyncQueue) Enqueue(text, category string, metadata map[string]any) error {
	if !getEnvBool("SOP_SYNC_QUEUE_ENABLED", true) {
		// Queue disabled, skip
		return nil
	}

	// Apply privacy filter before queueing
	ctx := context.Background()
	userID := "daemon-user" // TODO: Get actual user ID from context

	filterResult, err := filterContentBeforeShare(ctx, category, text, userID)
	if err != nil {
		log.Printf("Privacy filter error: %v", err)
		return err
	}

	// Handle filter decision
	switch filterResult.Action {
	case "block":
		// Content blocked by privacy filter
		log.Printf("Content blocked by privacy filter: %s", filterResult.Reason)
		return nil // Not an error, just blocked

	case "pending":
		// Content requires manual approval
		log.Printf("Content pending approval: %s", filterResult.Reason)
		// Add to approval queue instead of sync queue
		globalApprovalQueue.AddToApprovalQueue(ApprovalItem{
			ID:              generateApprovalID(),
			Category:        category,
			OriginalContent: text,
			RedactedContent: filterResult.RedactedContent,
			PIIMatches:      filterResult.PIIMatches,
			Reason:          filterResult.Reason,
			CreatedAt:       time.Now().UTC(),
			UserID:          userID,
		})
		return nil

	case "redact":
		// Use redacted content for sharing
		if filterResult.RedactedContent != "" {
			text = filterResult.RedactedContent
			if metadata == nil {
				metadata = make(map[string]any)
			}
			metadata["privacy_redacted"] = true
			metadata["pii_count"] = len(filterResult.PIIMatches)
		}

	case "allow":
		// Content approved for sharing as-is
		// Continue with normal flow
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	artifact := &QueuedArtifact{
		Text:     text,
		Category: category,
		Metadata: metadata,
		Priority: determinePriority(category),
		QueuedAt: time.Now(),
	}

	// Lightweight deduplication: check if similar artifact already in queue
	if q.isDuplicate(artifact) {
		return nil // Skip duplicate
	}

	// Check queue depth and apply drop-oldest policy if full
	if len(q.queue) >= q.maxDepth {
		q.dropOldest()
		q.totalDropped++
	}

	q.queue = append(q.queue, artifact)
	q.totalPromoted++

	return nil
}

// isDuplicate checks if a very similar artifact is already in the queue.
func (q *SyncQueue) isDuplicate(artifact *QueuedArtifact) bool {
	for _, existing := range q.queue {
		if existing.Category == artifact.Category {
			similarity := textSimilarity(existing.Text, artifact.Text)
			if similarity > 0.95 {
				return true
			}
		}
	}
	return false
}

// dropOldest removes the oldest normal-priority item from the queue.
func (q *SyncQueue) dropOldest() {
	// Try to drop oldest normal priority first to preserve high/critical items
	for i := len(q.queue) - 1; i >= 0; i-- {
		if q.queue[i].Priority == PriorityNormal {
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			return
		}
	}
	// If no normal priority, drop oldest item
	if len(q.queue) > 0 {
		q.queue = q.queue[1:]
	}
}

// DequeueBatch returns a batch of artifacts for sync, respecting priority and category diversity.
func (q *SyncQueue) DequeueBatch(maxSize int) []*QueuedArtifact {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.queue) == 0 {
		return nil
	}

	// Sort by priority (critical first, then high, then normal), then by age
	q.sortQueue()

	// Build category-diverse batch
	batch := make([]*QueuedArtifact, 0, maxSize)
	categoryCount := make(map[string]int)
	maxPerCategory := maxSize / 5 // Allow up to 20% of batch per category

	// First pass: fill with critical/high priority across categories
	for _, artifact := range q.queue {
		if len(batch) >= maxSize {
			break
		}
		if artifact.Priority >= PriorityHigh {
			batch = append(batch, artifact)
			categoryCount[artifact.Category]++
		}
	}

	// Second pass: fill remaining with normal priority, respecting category limits
	for _, artifact := range q.queue {
		if len(batch) >= maxSize {
			break
		}
		if artifact.Priority == PriorityNormal && !contains(batch, artifact) {
			if categoryCount[artifact.Category] < maxPerCategory || maxPerCategory == 0 {
				batch = append(batch, artifact)
				categoryCount[artifact.Category]++
			}
		}
	}

	// Remove batched items from queue
	q.removeBatchFromQueue(batch)

	return batch
}

// sortQueue orders queue by priority (high to low) and then by age (oldest first).
func (q *SyncQueue) sortQueue() {
	// Simple bubble sort for small queues (efficient enough for ~1000 items)
	for i := 0; i < len(q.queue); i++ {
		for j := i + 1; j < len(q.queue); j++ {
			// Sort by priority first (critical > high > normal)
			if q.queue[i].Priority < q.queue[j].Priority {
				q.queue[i], q.queue[j] = q.queue[j], q.queue[i]
			} else if q.queue[i].Priority == q.queue[j].Priority {
				// Then by age (older first)
				if q.queue[i].QueuedAt.After(q.queue[j].QueuedAt) {
					q.queue[i], q.queue[j] = q.queue[j], q.queue[i]
				}
			}
		}
	}
}

// removeBatchFromQueue removes the batched artifacts from the queue.
func (q *SyncQueue) removeBatchFromQueue(batch []*QueuedArtifact) {
	remaining := make([]*QueuedArtifact, 0, len(q.queue))
	for _, artifact := range q.queue {
		if !contains(batch, artifact) {
			remaining = append(remaining, artifact)
		}
	}
	q.queue = remaining
}

// RecordSyncAttempt records a sync attempt.
func (q *SyncQueue) RecordSyncAttempt() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.totalSyncAttempts++
	q.lastSyncAttempt = time.Now()
}

// RecordSyncSuccess records a successful sync and updates circuit breaker state.
func (q *SyncQueue) RecordSyncSuccess() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.totalSyncSuccesses++
	q.consecutiveSuccesses++
	q.consecutiveFailures = 0
	q.lastSyncSuccess = time.Now()
	q.currentBackoffDelay = 0 // Reset backoff on success

	// Circuit breaker state transitions
	if q.circuitState == CircuitHalfOpen {
		// Half-open -> closed after successful test
		q.circuitState = CircuitClosed
		log.Printf("Circuit breaker closed after successful sync")
	} else if q.circuitState == CircuitOpen {
		// This shouldn't happen, but reset to closed if it does
		q.circuitState = CircuitClosed
	}
}

// RecordSyncFailure records a failed sync and updates circuit breaker state.
func (q *SyncQueue) RecordSyncFailure(statusCode int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.totalSyncFailures++
	q.consecutiveFailures++
	q.consecutiveSuccesses = 0

	// Calculate backoff with exponential growth and jitter
	if q.currentBackoffDelay == 0 {
		q.currentBackoffDelay = q.baseSyncInterval
	} else {
		q.currentBackoffDelay = time.Duration(float64(q.currentBackoffDelay) * 2.0)
	}

	// Add 25% jitter
	jitter := time.Duration(rand.Float64() * 0.25 * float64(q.currentBackoffDelay))
	q.currentBackoffDelay += jitter

	// Cap at max backoff
	if q.currentBackoffDelay > q.maxBackoffDuration {
		q.currentBackoffDelay = q.maxBackoffDuration
	}

	// Circuit breaker state transitions
	if !getEnvBool("SOP_CIRCUIT_BREAKER_ENABLED", true) {
		return // Circuit breaker disabled
	}

	if q.circuitState == CircuitClosed && q.consecutiveFailures >= q.circuitBreakerThreshold {
		// Closed -> open after threshold failures
		q.circuitState = CircuitOpen
		q.circuitOpenedAt = time.Now()
		log.Printf("Circuit breaker opened after %d consecutive failures", q.consecutiveFailures)
	} else if q.circuitState == CircuitHalfOpen {
		// Half-open -> open if test fails
		q.circuitState = CircuitOpen
		q.circuitOpenedAt = time.Now()
		log.Printf("Circuit breaker reopened after test failure")
	}
}

// ShouldAttemptSync checks if a sync attempt should be made based on circuit breaker and throttling.
func (q *SyncQueue) ShouldAttemptSync() (bool, string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.throttleControl != nil && !q.throttleControl.Allowed {
		reason := q.throttleControl.Reason
		if reason == "" {
			reason = "HQ denied sync"
		}
		return false, fmt.Sprintf("HQ throttle block: %s", reason)
	}

	// Check circuit breaker state
	switch q.circuitState {
	case CircuitOpen:
		// Check if enough time has passed to try half-open
		timeSinceOpen := time.Since(q.circuitOpenedAt)
		if timeSinceOpen < q.currentBackoffDelay {
			return false, fmt.Sprintf("circuit breaker open (retry in %s)", q.currentBackoffDelay-timeSinceOpen)
		}
		// Transition to half-open with random jitter to prevent thundering herd
		jitter := time.Duration(rand.Float64() * 30 * float64(time.Second))
		time.Sleep(jitter)
		q.circuitState = CircuitHalfOpen
		log.Printf("Circuit breaker half-open, testing with small batch")
		return true, "circuit breaker half-open"

	case CircuitHalfOpen:
		// Allow attempt but with reduced batch size
		return true, "circuit breaker half-open"

	case CircuitClosed:
		// Normal operation
		return true, "circuit breaker closed"
	}

	return true, "unknown state"
}

// ApplyThrottleControl applies an HQ-provided throttle hint to the daemon.
func (q *SyncQueue) ApplyThrottleControl(control *SyncThrottleControl) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.throttleControl = control
	if control == nil {
		return
	}
	if control.MaxQueueDepth > 0 {
		q.maxDepth = control.MaxQueueDepth
	}
	if control.SyncIntervalSeconds > 0 {
		q.baseSyncInterval = time.Duration(control.SyncIntervalSeconds) * time.Second
	}
	if control.BatchSize > 0 {
		q.maxDepth = max(q.maxDepth, control.BatchSize)
	}
}

// GetNextSyncDelay returns the delay until the next sync attempt.
func (q *SyncQueue) GetNextSyncDelay() time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.throttleControl != nil && q.throttleControl.SyncIntervalSeconds > 0 {
		return time.Duration(q.throttleControl.SyncIntervalSeconds) * time.Second
	}

	if !getEnvBool("SOP_ADAPTIVE_THROTTLING_ENABLED", true) {
		// Adaptive throttling disabled, use base interval with jitter
		return addJitter(q.baseSyncInterval, 0.25)
	}

	// Adaptive throttling based on recent success/failure
	if q.currentBackoffDelay > 0 {
		return q.currentBackoffDelay
	}

	// Scale down interval if recent successes
	if q.consecutiveSuccesses >= 3 {
		// Successful sync streak, slightly reduce interval
		return addJitter(q.baseSyncInterval*4/5, 0.25)
	}

	return addJitter(q.baseSyncInterval, 0.25)
}

// GetBatchSize returns the appropriate batch size based on circuit breaker state.
func (q *SyncQueue) GetBatchSize() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.throttleControl != nil && q.throttleControl.BatchSize > 0 {
		return q.throttleControl.BatchSize
	}

	maxBatchSize := getEnvInt("SOP_MAX_BATCH_SIZE", 50)

	if q.circuitState == CircuitHalfOpen {
		// Use small test batch in half-open state
		return min(5, maxBatchSize)
	}

	return maxBatchSize
}

// GetMetrics returns current metrics as a map for logging/observability.
func (q *SyncQueue) GetMetrics() map[string]interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	return map[string]interface{}{
		"queue_depth":           len(q.queue),
		"total_promoted":        q.totalPromoted,
		"total_sync_attempts":   q.totalSyncAttempts,
		"total_sync_successes":  q.totalSyncSuccesses,
		"total_sync_failures":   q.totalSyncFailures,
		"total_dropped":         q.totalDropped,
		"consecutive_failures":  q.consecutiveFailures,
		"consecutive_successes": q.consecutiveSuccesses,
		"circuit_state":         circuitStateString(q.circuitState),
		"current_backoff_delay": q.currentBackoffDelay.String(),
		"last_sync_attempt":     q.lastSyncAttempt.Format(time.RFC3339),
		"last_sync_success":     q.lastSyncSuccess.Format(time.RFC3339),
	}
}

// Helper functions

func determinePriority(category string) SyncPriority {
	switch category {
	case "security_pattern", "safety_heuristic":
		return PriorityCritical
	case "error_resolution", "repair_strategy", "anti_pattern":
		return PriorityHigh
	default:
		return PriorityNormal
	}
}

func textSimilarity(a, b string) float64 {
	// Simple similarity: ratio of common prefix length to average length
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))

	if a == b {
		return 1.0
	}

	minLen := min(len(a), len(b))
	if minLen == 0 {
		return 0.0
	}

	commonPrefix := 0
	for i := 0; i < minLen; i++ {
		if a[i] == b[i] {
			commonPrefix++
		} else {
			break
		}
	}

	avgLen := (len(a) + len(b)) / 2
	if avgLen == 0 {
		return 0.0
	}

	return float64(commonPrefix) / float64(avgLen)
}

func contains(batch []*QueuedArtifact, artifact *QueuedArtifact) bool {
	for _, item := range batch {
		if item == artifact {
			return true
		}
	}
	return false
}

func circuitStateString(state CircuitState) string {
	switch state {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

func addJitter(duration time.Duration, jitterFraction float64) time.Duration {
	jitter := time.Duration(rand.Float64() * jitterFraction * float64(duration))
	return duration + jitter
}

func generateApprovalID() string {
	return fmt.Sprintf("approval-%d-%d", time.Now().Unix(), rand.Intn(10000))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return defaultValue
	}
	duration, err := time.ParseDuration(val)
	if err != nil {
		return defaultValue
	}
	return duration
}

func getEnvBool(key string, defaultValue bool) bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if val == "" {
		return defaultValue
	}
	return val == "true" || val == "1" || val == "yes"
}
