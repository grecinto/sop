package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sharedcode/sop/btree"
)

// WorkQueue manages the execution queue for multi-turn orchestration.
// It enables capabilities to inject work items that execute as separate ReAct turns.
type WorkQueue struct {
	items       []WorkItem
	mu          sync.Mutex
	maxDepth    int
	maxPerActor map[string]int
	turnTimeout time.Duration
	actorCount  map[string]int
}

// WorkItem represents a single unit of work to be executed in the ReAct loop.
type WorkItem struct {
	Query         string                `json:"query"`
	Actor         string                `json:"actor"`
	Priority      int                   `json:"priority"`
	Context       map[string]any        `json:"context,omitempty"`
	ParentItemID  string                `json:"parent_item_id,omitempty"`
	TurnNumber    int                   `json:"turn_number"`
	ID            string                `json:"id"`
	Condition     func(TurnResult) bool `json:"-"` // Skip JSON marshaling
	DependsOn     []string              `json:"depends_on,omitempty"`
	AllowParallel bool                  `json:"allow_parallel"`
}

// TurnResult captures the outcome of a single turn execution.
type TurnResult struct {
	TurnNumber  int
	Actor       string
	Query       string
	Response    string
	ToolCalls   []any
	Error       error
	Data        map[string]any
	Duration    time.Duration
	TestsFailed bool // Helper flag for conditional logic
}

// TurnContext holds state passed between turns.
type TurnContext struct {
	TurnNumber    int
	ParentResults map[string]TurnResult
	SharedState   map[string]any
}

// WorkflowExecutionState tracks the state of an active workflow execution.
// This is passed explicitly rather than through context to avoid anti-patterns.
type WorkflowExecutionState struct {
	InWorkflowMode bool         // Prevents recursive workflow detection
	Queue          *WorkQueue   // The active work queue
	TurnResults    []TurnResult // Results from previous turns
	OriginalQuery  string       // The initial user query that triggered the workflow
}

// WorkQueueError represents queue-specific errors.
type WorkQueueError struct {
	Code    string
	Message string
}

func (e *WorkQueueError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

var (
	ErrQueueDepthExceeded = &WorkQueueError{Code: "DEPTH_EXCEEDED", Message: "queue depth limit exceeded"}
	ErrActorLimitExceeded = &WorkQueueError{Code: "ACTOR_LIMIT", Message: "per-actor enqueue limit exceeded"}
	ErrQueueEmpty         = &WorkQueueError{Code: "QUEUE_EMPTY", Message: "queue is empty"}
)

// NewWorkQueue creates a new work queue with default settings.
func NewWorkQueue() *WorkQueue {
	return &WorkQueue{
		items:       make([]WorkItem, 0, 10),
		maxDepth:    10,
		maxPerActor: make(map[string]int),
		turnTimeout: 5 * time.Minute,
		actorCount:  make(map[string]int),
	}
}

// NewWorkQueueWithConfig creates a work queue with custom configuration.
func NewWorkQueueWithConfig(maxDepth int, turnTimeout time.Duration) *WorkQueue {
	return &WorkQueue{
		items:       make([]WorkItem, 0, maxDepth),
		maxDepth:    maxDepth,
		maxPerActor: make(map[string]int),
		turnTimeout: turnTimeout,
		actorCount:  make(map[string]int),
	}
}

// SetMaxPerActor sets the maximum number of items allowed per actor.
func (q *WorkQueue) SetMaxPerActor(actor string, max int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maxPerActor[actor] = max
}

// Enqueue adds a work item to the queue.
func (q *WorkQueue) Enqueue(item WorkItem) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check depth limit
	if len(q.items) >= q.maxDepth {
		return ErrQueueDepthExceeded
	}

	// Check per-actor limit
	if maxForActor, ok := q.maxPerActor[item.Actor]; ok {
		if q.actorCount[item.Actor] >= maxForActor {
			return ErrActorLimitExceeded
		}
	}

	// Generate ID if not set
	if item.ID == "" {
		item.ID = fmt.Sprintf("%s-%d-%d", item.Actor, time.Now().UnixNano(), len(q.items))
	}

	// Insert based on priority (higher priority first)
	inserted := false
	for i, existing := range q.items {
		if item.Priority > existing.Priority {
			q.items = append(q.items[:i], append([]WorkItem{item}, q.items[i:]...)...)
			inserted = true
			break
		}
	}
	if !inserted {
		q.items = append(q.items, item)
	}

	q.actorCount[item.Actor]++
	return nil
}

// EnqueueConditional adds a work item that only executes if the condition is met.
func (q *WorkQueue) EnqueueConditional(item WorkItem) error {
	return q.Enqueue(item)
}

// Dequeue removes and returns the next work item.
func (q *WorkQueue) Dequeue() (WorkItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return WorkItem{}, ErrQueueEmpty
	}

	item := q.items[0]
	q.items = q.items[1:]
	q.actorCount[item.Actor]--
	return item, nil
}

// Peek returns the next work item without removing it.
func (q *WorkQueue) Peek() (WorkItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return WorkItem{}, ErrQueueEmpty
	}

	return q.items[0], nil
}

// IsEmpty returns true if the queue has no items.
func (q *WorkQueue) IsEmpty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) == 0
}

// Depth returns the current number of items in the queue.
func (q *WorkQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Clear removes all items from the queue.
func (q *WorkQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = make([]WorkItem, 0, q.maxDepth)
	q.actorCount = make(map[string]int)
}

// Snapshot returns a copy of the current queue state.
func (q *WorkQueue) Snapshot() []WorkItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	snapshot := make([]WorkItem, len(q.items))
	copy(snapshot, q.items)
	return snapshot
}

// GetTurnTimeout returns the timeout for a single turn.
func (q *WorkQueue) GetTurnTimeout() time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.turnTimeout
}

// Context keys for queue and turn context injection
type workQueueContextKey string

const (
	ctxKeyWorkQueue   workQueueContextKey = "work_queue"
	ctxKeyTurnContext workQueueContextKey = "turn_context"
	ctxKeyTurnResults workQueueContextKey = "turn_results"
)

// WithWorkQueue injects the work queue into the context.
func WithWorkQueue(ctx context.Context, queue *WorkQueue) context.Context {
	return context.WithValue(ctx, ctxKeyWorkQueue, queue)
}

// GetWorkQueue retrieves the work queue from the context.
func GetWorkQueue(ctx context.Context) (*WorkQueue, bool) {
	queue, ok := ctx.Value(ctxKeyWorkQueue).(*WorkQueue)
	return queue, ok
}

// WithTurnContext injects turn context into the context.
func WithTurnContext(ctx context.Context, turnCtx TurnContext) context.Context {
	return context.WithValue(ctx, ctxKeyTurnContext, turnCtx)
}

// GetTurnContext retrieves turn context from the context.
func GetTurnContext(ctx context.Context) (TurnContext, bool) {
	turnCtx, ok := ctx.Value(ctxKeyTurnContext).(TurnContext)
	return turnCtx, ok
}

// WithTurnResults injects all turn results into the context.
func WithTurnResults(ctx context.Context, results []TurnResult) context.Context {
	return context.WithValue(ctx, ctxKeyTurnResults, results)
}

// GetTurnResults retrieves all turn results from the context.
func GetTurnResults(ctx context.Context) ([]TurnResult, bool) {
	results, ok := ctx.Value(ctxKeyTurnResults).([]TurnResult)
	return results, ok
}

// ===== Phase 4: Advanced Patterns =====

// ProgressCallback is called when a turn completes.
type ProgressCallback func(result TurnResult)

// QueueCheckpoint represents a serializable snapshot of queue state.
type QueueCheckpoint struct {
	Items        []WorkItem     `json:"items"`
	TurnResults  []TurnResult   `json:"turn_results"`
	ActorCount   map[string]int `json:"actor_count"`
	MaxDepth     int            `json:"max_depth"`
	MaxPerActor  map[string]int `json:"max_per_actor"`
	Timestamp    time.Time      `json:"timestamp"`
	CheckpointID string         `json:"checkpoint_id"`
}

// Checkpoint creates a serializable snapshot of the current queue state.
func (q *WorkQueue) Checkpoint() QueueCheckpoint {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Create items without Condition field (not serializable)
	items := make([]WorkItem, len(q.items))
	for i, item := range q.items {
		items[i] = WorkItem{
			Query:         item.Query,
			Actor:         item.Actor,
			Priority:      item.Priority,
			Context:       item.Context,
			ParentItemID:  item.ParentItemID,
			TurnNumber:    item.TurnNumber,
			ID:            item.ID,
			DependsOn:     item.DependsOn,
			AllowParallel: item.AllowParallel,
			// Condition omitted - not serializable
		}
	}

	actorCount := make(map[string]int)
	for k, v := range q.actorCount {
		actorCount[k] = v
	}

	maxPerActor := make(map[string]int)
	for k, v := range q.maxPerActor {
		maxPerActor[k] = v
	}

	return QueueCheckpoint{
		Items:        items,
		ActorCount:   actorCount,
		MaxDepth:     q.maxDepth,
		MaxPerActor:  maxPerActor,
		Timestamp:    time.Now(),
		CheckpointID: fmt.Sprintf("checkpoint-%d", time.Now().Unix()),
	}
}

// RestoreFromCheckpoint restores queue state from a checkpoint.
func (q *WorkQueue) RestoreFromCheckpoint(checkpoint QueueCheckpoint) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = make([]WorkItem, len(checkpoint.Items))
	copy(q.items, checkpoint.Items)

	q.actorCount = make(map[string]int)
	for k, v := range checkpoint.ActorCount {
		q.actorCount[k] = v
	}

	q.maxDepth = checkpoint.MaxDepth
	q.maxPerActor = make(map[string]int)
	for k, v := range checkpoint.MaxPerActor {
		q.maxPerActor[k] = v
	}

	return nil
}

// GetReadyItems returns items that have no pending dependencies.
// Used for parallel execution - items with no dependencies or whose dependencies
// are complete can be executed in parallel.
func (q *WorkQueue) GetReadyItems(completedIDs map[string]bool) []WorkItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	var ready []WorkItem
	for _, item := range q.items {
		if len(item.DependsOn) == 0 {
			ready = append(ready, item)
			continue
		}

		// Check if all dependencies are satisfied
		allSatisfied := true
		for _, depID := range item.DependsOn {
			if !completedIDs[depID] {
				allSatisfied = false
				break
			}
		}

		if allSatisfied && item.AllowParallel {
			ready = append(ready, item)
		}
	}

	return ready
}

// RemoveItems removes specific items from the queue by ID.
func (q *WorkQueue) RemoveItems(ids []string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}

	var remaining []WorkItem
	for _, item := range q.items {
		if !idSet[item.ID] {
			remaining = append(remaining, item)
		}
	}

	q.items = remaining
}

// ToJSON exports the queue state as JSON for visualization/debugging.
func (q *WorkQueue) ToJSON() (map[string]interface{}, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]map[string]interface{}, len(q.items))
	for i, item := range q.items {
		items[i] = map[string]interface{}{
			"id":             item.ID,
			"query":          item.Query,
			"actor":          item.Actor,
			"priority":       item.Priority,
			"turn_number":    item.TurnNumber,
			"parent_id":      item.ParentItemID,
			"depends_on":     item.DependsOn,
			"allow_parallel": item.AllowParallel,
		}
	}

	return map[string]interface{}{
		"depth":         len(q.items),
		"max_depth":     q.maxDepth,
		"max_per_actor": q.maxPerActor,
		"actor_count":   q.actorCount,
		"items":         items,
	}, nil
}

// ===== Phase 5: Production Hardening =====

// RetryStrategy defines how to retry failed operations.
type RetryStrategy struct {
	MaxAttempts     int           // Maximum retry attempts
	InitialDelay    time.Duration // Initial delay before first retry
	MaxDelay        time.Duration // Maximum delay between retries
	BackoffFactor   float64       // Exponential backoff multiplier (e.g., 2.0)
	RetryableErrors []string      // Error codes/messages that should trigger retry
}

// DefaultRetryStrategy returns a sensible default retry configuration.
func DefaultRetryStrategy() RetryStrategy {
	return RetryStrategy{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		RetryableErrors: []string{
			"timeout",
			"rate_limit",
			"service_unavailable",
			"temporary_failure",
		},
	}
}

// CalculateDelay calculates the delay for a given attempt using exponential backoff.
func (rs *RetryStrategy) CalculateDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	delay := float64(rs.InitialDelay)
	for i := 0; i < attempt-1; i++ {
		delay *= rs.BackoffFactor
	}

	if time.Duration(delay) > rs.MaxDelay {
		return rs.MaxDelay
	}

	return time.Duration(delay)
}

// QueuePersistenceStore is the storage contract for queue checkpoint/metric persistence.
// Concrete implementations can use SOP B-trees or any other backend.
type QueuePersistenceStore interface {
	SaveCheckpoint(ctx context.Context, checkpoint QueueCheckpoint) error
	LoadCheckpoint(ctx context.Context, checkpointID string) (*QueueCheckpoint, error)
	ListCheckpoints(ctx context.Context) ([]string, error)
	SaveMetric(ctx context.Context, checkpointID string, result TurnResult) error
	Close(ctx context.Context) error
}

type btreeQueuePersistence struct {
	checkpoints btree.BtreeInterface[string, []byte] // checkpoint_id -> serialized QueueCheckpoint
	metrics     btree.BtreeInterface[string, []byte] // metric_id -> serialized TurnResult
	byTimestamp btree.BtreeInterface[string, string] // timestamp:checkpoint_id -> checkpoint_id
}

// QueuePersistence is the facade used by the agent layer.
type QueuePersistence struct {
	backend QueuePersistenceStore
	mu      sync.Mutex
}

// NewQueuePersistence creates a new persistence facade backed by an SOP B-tree adapter.
func NewQueuePersistence(
	ctx context.Context,
	checkpoints btree.BtreeInterface[string, []byte],
	metrics btree.BtreeInterface[string, []byte],
	byTimestamp btree.BtreeInterface[string, string],
) (*QueuePersistence, error) {
	if checkpoints == nil || metrics == nil || byTimestamp == nil {
		return nil, fmt.Errorf("all b-tree stores must be non-nil")
	}

	backend := &btreeQueuePersistence{
		checkpoints: checkpoints,
		metrics:     metrics,
		byTimestamp: byTimestamp,
	}

	return &QueuePersistence{backend: backend}, nil
}

// NewQueuePersistenceWithBackend creates a facade around a custom persistence backend.
func NewQueuePersistenceWithBackend(backend QueuePersistenceStore) *QueuePersistence {
	if backend == nil {
		return &QueuePersistence{}
	}
	return &QueuePersistence{backend: backend}
}

// SaveCheckpoint persists a queue checkpoint to storage.
func (qp *QueuePersistence) SaveCheckpoint(ctx context.Context, checkpoint QueueCheckpoint) error {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.backend == nil {
		return fmt.Errorf("queue persistence backend not initialized")
	}
	return qp.backend.SaveCheckpoint(ctx, checkpoint)
}

// LoadCheckpoint loads a checkpoint from persistent storage.
func (qp *QueuePersistence) LoadCheckpoint(ctx context.Context, checkpointID string) (*QueueCheckpoint, error) {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.backend == nil {
		return nil, fmt.Errorf("queue persistence backend not initialized")
	}
	return qp.backend.LoadCheckpoint(ctx, checkpointID)
}

// ListCheckpoints returns all available checkpoint IDs ordered by timestamp (newest first).
func (qp *QueuePersistence) ListCheckpoints(ctx context.Context) ([]string, error) {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.backend == nil {
		return nil, fmt.Errorf("queue persistence backend not initialized")
	}
	return qp.backend.ListCheckpoints(ctx)
}

// SaveMetric records a turn execution metric.
func (qp *QueuePersistence) SaveMetric(ctx context.Context, checkpointID string, result TurnResult) error {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.backend == nil {
		return fmt.Errorf("queue persistence backend not initialized")
	}
	return qp.backend.SaveMetric(ctx, checkpointID, result)
}

// Close closes the persistence layer (commits the transaction).
func (qp *QueuePersistence) Close(ctx context.Context) error {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.backend == nil {
		return nil
	}
	return qp.backend.Close(ctx)
}

// SaveCheckpoint persists a queue checkpoint to storage.
func (bp *btreeQueuePersistence) SaveCheckpoint(ctx context.Context, checkpoint QueueCheckpoint) error {
	// Marshal checkpoint to JSON
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	// Save checkpoint by ID
	if _, err := bp.checkpoints.Add(ctx, checkpoint.CheckpointID, data); err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	// Create timestamp index key for ordering: timestamp:checkpoint_id
	timestampKey := fmt.Sprintf("%020d:%s", checkpoint.Timestamp.Unix(), checkpoint.CheckpointID)
	if _, err := bp.byTimestamp.Add(ctx, timestampKey, checkpoint.CheckpointID); err != nil {
		return fmt.Errorf("failed to save timestamp index: %w", err)
	}

	return nil
}

// LoadCheckpoint loads a checkpoint from persistent storage.
func (bp *btreeQueuePersistence) LoadCheckpoint(ctx context.Context, checkpointID string) (*QueueCheckpoint, error) {
	// Find checkpoint by ID
	ok, err := bp.checkpoints.Find(ctx, checkpointID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to find checkpoint: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("checkpoint not found: %s", checkpointID)
	}

	// Get checkpoint data
	data, err := bp.checkpoints.GetCurrentValue(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint data: %w", err)
	}

	// Unmarshal checkpoint
	var checkpoint QueueCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	return &checkpoint, nil
}

// ListCheckpoints returns all available checkpoint IDs ordered by timestamp (newest first).
func (bp *btreeQueuePersistence) ListCheckpoints(ctx context.Context) ([]string, error) {
	var checkpoints []string

	// Iterate through timestamp index in reverse order (newest first)
	ok, err := bp.byTimestamp.Last(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate checkpoints: %w", err)
	}

	for ok {
		checkpointID, err := bp.byTimestamp.GetCurrentValue(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get checkpoint ID: %w", err)
		}
		checkpoints = append(checkpoints, checkpointID)

		ok, err = bp.byTimestamp.Previous(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to iterate previous: %w", err)
		}
	}

	return checkpoints, nil
}

// SaveMetric records a turn execution metric.
func (bp *btreeQueuePersistence) SaveMetric(ctx context.Context, checkpointID string, result TurnResult) error {
	// Create metric record
	metric := struct {
		CheckpointID string `json:"checkpoint_id"`
		TurnNumber   int    `json:"turn_number"`
		Actor        string `json:"actor"`
		DurationMs   int64  `json:"duration_ms"`
		Error        string `json:"error,omitempty"`
		Timestamp    int64  `json:"timestamp"`
	}{
		CheckpointID: checkpointID,
		TurnNumber:   result.TurnNumber,
		Actor:        result.Actor,
		DurationMs:   result.Duration.Milliseconds(),
		Timestamp:    time.Now().Unix(),
	}

	if result.Error != nil {
		metric.Error = result.Error.Error()
	}

	// Marshal metric
	data, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("failed to marshal metric: %w", err)
	}

	// Generate unique metric ID: checkpoint_id:turn_number:timestamp
	metricID := fmt.Sprintf("%s:%d:%d", checkpointID, result.TurnNumber, time.Now().UnixNano())

	// Save metric
	if _, err := bp.metrics.Add(ctx, metricID, data); err != nil {
		return fmt.Errorf("failed to save metric: %w", err)
	}

	return nil
}

// Close closes the persistence layer (commits the transaction).
func (bp *btreeQueuePersistence) Close(ctx context.Context) error {
	// B-trees are managed by the transaction, just return nil
	// The caller should commit the transaction
	return nil
}

// CircuitBreaker prevents repeated calls to failing operations.
type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration
	failures     int
	lastFailTime time.Time
	state        string // "closed", "open", "half-open"
	mu           sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        "closed",
	}
}

// Call executes a function with circuit breaker protection.
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()

	// Check if circuit should transition from open to half-open
	if cb.state == "open" && time.Since(cb.lastFailTime) > cb.resetTimeout {
		cb.state = "half-open"
		cb.failures = 0
	}

	// Fail fast if circuit is open
	if cb.state == "open" {
		cb.mu.Unlock()
		return fmt.Errorf("circuit breaker open: too many failures")
	}

	cb.mu.Unlock()

	// Execute function
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFailTime = time.Now()

		if cb.failures >= cb.maxFailures {
			cb.state = "open"
		}

		return err
	}

	// Success - reset circuit
	if cb.state == "half-open" {
		cb.state = "closed"
	}
	cb.failures = 0

	return nil
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// QueueMetrics tracks performance metrics for queue operations.
type QueueMetrics struct {
	TotalTurns       int
	SuccessfulTurns  int
	FailedTurns      int
	TotalDuration    time.Duration
	AverageDuration  time.Duration
	TurnsByActor     map[string]int
	ErrorsByType     map[string]int
	ParallelTurns    int
	CheckpointsCount int
	mu               sync.Mutex
}

// NewQueueMetrics creates a new metrics tracker.
func NewQueueMetrics() *QueueMetrics {
	return &QueueMetrics{
		TurnsByActor: make(map[string]int),
		ErrorsByType: make(map[string]int),
	}
}

// RecordTurn records metrics for a completed turn.
func (qm *QueueMetrics) RecordTurn(result TurnResult) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.TotalTurns++
	qm.TotalDuration += result.Duration
	qm.TurnsByActor[result.Actor]++

	if result.Error != nil {
		qm.FailedTurns++
		qm.ErrorsByType[result.Error.Error()]++
	} else {
		qm.SuccessfulTurns++
	}

	qm.AverageDuration = qm.TotalDuration / time.Duration(qm.TotalTurns)
}

// RecordCheckpoint increments checkpoint counter.
func (qm *QueueMetrics) RecordCheckpoint() {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.CheckpointsCount++
}

// ToMap exports metrics as a map for reporting.
func (qm *QueueMetrics) ToMap() map[string]interface{} {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	return map[string]interface{}{
		"total_turns":       qm.TotalTurns,
		"successful_turns":  qm.SuccessfulTurns,
		"failed_turns":      qm.FailedTurns,
		"total_duration_ms": qm.TotalDuration.Milliseconds(),
		"avg_duration_ms":   qm.AverageDuration.Milliseconds(),
		"turns_by_actor":    qm.TurnsByActor,
		"errors_by_type":    qm.ErrorsByType,
		"parallel_turns":    qm.ParallelTurns,
		"checkpoints":       qm.CheckpointsCount,
	}
}
