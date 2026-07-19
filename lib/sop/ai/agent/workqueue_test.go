package agent

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewWorkQueue(t *testing.T) {
	q := NewWorkQueue()
	if q == nil {
		t.Fatal("expected non-nil queue")
	}
	if q.maxDepth != 10 {
		t.Errorf("expected maxDepth=10, got %d", q.maxDepth)
	}
	if q.IsEmpty() != true {
		t.Error("expected new queue to be empty")
	}
}

func TestWorkQueueEnqueueDequeue(t *testing.T) {
	q := NewWorkQueue()

	item := WorkItem{
		Query: "test query",
		Actor: "user",
	}

	if err := q.Enqueue(item); err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	if q.Depth() != 1 {
		t.Errorf("expected depth=1, got %d", q.Depth())
	}

	dequeued, err := q.Dequeue()
	if err != nil {
		t.Fatalf("failed to dequeue: %v", err)
	}

	if dequeued.Query != "test query" {
		t.Errorf("expected query='test query', got '%s'", dequeued.Query)
	}

	if !q.IsEmpty() {
		t.Error("expected queue to be empty after dequeue")
	}
}

func TestWorkQueuePriority(t *testing.T) {
	q := NewWorkQueue()

	// Enqueue items with different priorities
	q.Enqueue(WorkItem{Query: "low", Actor: "user", Priority: 1})
	q.Enqueue(WorkItem{Query: "high", Actor: "user", Priority: 10})
	q.Enqueue(WorkItem{Query: "medium", Actor: "user", Priority: 5})

	// Should dequeue in priority order
	item1, _ := q.Dequeue()
	if item1.Query != "high" {
		t.Errorf("expected 'high' first, got '%s'", item1.Query)
	}

	item2, _ := q.Dequeue()
	if item2.Query != "medium" {
		t.Errorf("expected 'medium' second, got '%s'", item2.Query)
	}

	item3, _ := q.Dequeue()
	if item3.Query != "low" {
		t.Errorf("expected 'low' third, got '%s'", item3.Query)
	}
}

func TestWorkQueueDepthLimit(t *testing.T) {
	q := NewWorkQueueWithConfig(3, time.Minute)

	// Fill queue to limit
	for i := 0; i < 3; i++ {
		if err := q.Enqueue(WorkItem{Query: "test", Actor: "user"}); err != nil {
			t.Fatalf("failed to enqueue item %d: %v", i, err)
		}
	}

	// Next enqueue should fail
	err := q.Enqueue(WorkItem{Query: "overflow", Actor: "user"})
	if err != ErrQueueDepthExceeded {
		t.Errorf("expected ErrQueueDepthExceeded, got %v", err)
	}
}

func TestWorkQueueActorLimit(t *testing.T) {
	q := NewWorkQueue()
	q.SetMaxPerActor("test_actor", 2)

	// Should allow 2 items from test_actor
	if err := q.Enqueue(WorkItem{Query: "1", Actor: "test_actor"}); err != nil {
		t.Fatalf("failed first enqueue: %v", err)
	}
	if err := q.Enqueue(WorkItem{Query: "2", Actor: "test_actor"}); err != nil {
		t.Fatalf("failed second enqueue: %v", err)
	}

	// Third should fail
	err := q.Enqueue(WorkItem{Query: "3", Actor: "test_actor"})
	if err != ErrActorLimitExceeded {
		t.Errorf("expected ErrActorLimitExceeded, got %v", err)
	}

	// Different actor should still work
	if err := q.Enqueue(WorkItem{Query: "4", Actor: "other_actor"}); err != nil {
		t.Fatalf("failed enqueue from different actor: %v", err)
	}
}

func TestWorkQueueClear(t *testing.T) {
	q := NewWorkQueue()

	for i := 0; i < 5; i++ {
		q.Enqueue(WorkItem{Query: "test", Actor: "user"})
	}

	if q.Depth() != 5 {
		t.Errorf("expected depth=5, got %d", q.Depth())
	}

	q.Clear()

	if !q.IsEmpty() {
		t.Error("expected queue to be empty after clear")
	}
}

func TestWorkQueueSnapshot(t *testing.T) {
	q := NewWorkQueue()

	q.Enqueue(WorkItem{Query: "1", Actor: "user"})
	q.Enqueue(WorkItem{Query: "2", Actor: "user"})

	snapshot := q.Snapshot()

	if len(snapshot) != 2 {
		t.Errorf("expected snapshot length=2, got %d", len(snapshot))
	}

	// Modifying snapshot shouldn't affect queue
	snapshot[0].Query = "modified"

	item, _ := q.Dequeue()
	if item.Query == "modified" {
		t.Error("snapshot modification affected queue")
	}
}

func TestWorkQueueContextInjection(t *testing.T) {
	ctx := context.Background()
	q := NewWorkQueue()

	ctx = WithWorkQueue(ctx, q)

	retrieved, ok := GetWorkQueue(ctx)
	if !ok {
		t.Fatal("failed to retrieve queue from context")
	}

	if retrieved != q {
		t.Error("retrieved queue doesn't match injected queue")
	}
}

func TestTurnContextInjection(t *testing.T) {
	ctx := context.Background()
	turnCtx := TurnContext{
		TurnNumber:  5,
		SharedState: map[string]any{"key": "value"},
	}

	ctx = WithTurnContext(ctx, turnCtx)

	retrieved, ok := GetTurnContext(ctx)
	if !ok {
		t.Fatal("failed to retrieve turn context from context")
	}

	if retrieved.TurnNumber != 5 {
		t.Errorf("expected turn number=5, got %d", retrieved.TurnNumber)
	}
}

func TestWorkItemIDGeneration(t *testing.T) {
	q := NewWorkQueue()

	item := WorkItem{
		Query: "test",
		Actor: "user",
	}

	if err := q.Enqueue(item); err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	dequeued, _ := q.Dequeue()
	if dequeued.ID == "" {
		t.Error("expected auto-generated ID, got empty string")
	}
}

func TestWorkQueuePeek(t *testing.T) {
	q := NewWorkQueue()

	item := WorkItem{Query: "test", Actor: "user"}
	q.Enqueue(item)

	peeked, err := q.Peek()
	if err != nil {
		t.Fatalf("failed to peek: %v", err)
	}

	if peeked.Query != "test" {
		t.Errorf("expected query='test', got '%s'", peeked.Query)
	}

	// Peek shouldn't remove item
	if q.Depth() != 1 {
		t.Error("peek modified queue depth")
	}
}

func TestWorkQueueEmptyErrors(t *testing.T) {
	q := NewWorkQueue()

	_, err := q.Dequeue()
	if err != ErrQueueEmpty {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}

	_, err = q.Peek()
	if err != ErrQueueEmpty {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestTurnResultStorage(t *testing.T) {
	ctx := context.Background()

	results := []TurnResult{
		{TurnNumber: 0, Actor: "user", Response: "response1"},
		{TurnNumber: 1, Actor: "coding_workflow", Response: "response2"},
	}

	ctx = WithTurnResults(ctx, results)

	retrieved, ok := GetTurnResults(ctx)
	if !ok {
		t.Fatal("failed to retrieve turn results from context")
	}

	if len(retrieved) != 2 {
		t.Errorf("expected 2 results, got %d", len(retrieved))
	}

	if retrieved[0].Response != "response1" {
		t.Errorf("expected response1, got %s", retrieved[0].Response)
	}
}

// ===== Phase 4 Tests =====

func TestWorkQueue_Checkpoint(t *testing.T) {
	queue := NewWorkQueue()
	queue.SetMaxPerActor("test_actor", 3)

	// Add some items
	_ = queue.Enqueue(WorkItem{Query: "task1", Actor: "actor1", Priority: 1})
	_ = queue.Enqueue(WorkItem{Query: "task2", Actor: "actor2", Priority: 2})

	// Create checkpoint
	checkpoint := queue.Checkpoint()

	if len(checkpoint.Items) != 2 {
		t.Errorf("expected 2 items in checkpoint, got %d", len(checkpoint.Items))
	}

	if checkpoint.MaxDepth != 10 {
		t.Errorf("expected maxDepth 10, got %d", checkpoint.MaxDepth)
	}

	if checkpoint.MaxPerActor["test_actor"] != 3 {
		t.Errorf("expected max_per_actor[test_actor]=3, got %d", checkpoint.MaxPerActor["test_actor"])
	}

	if checkpoint.CheckpointID == "" {
		t.Error("checkpoint ID should not be empty")
	}
}

func TestWorkQueue_RestoreFromCheckpoint(t *testing.T) {
	// Create original queue
	original := NewWorkQueue()
	original.SetMaxPerActor("test_actor", 5)
	_ = original.Enqueue(WorkItem{Query: "task1", Actor: "actor1", Priority: 1, ID: "id1"})
	_ = original.Enqueue(WorkItem{Query: "task2", Actor: "actor2", Priority: 2, ID: "id2"})

	// Create checkpoint
	checkpoint := original.Checkpoint()

	// Create new queue and restore
	restored := NewWorkQueue()
	err := restored.RestoreFromCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("failed to restore from checkpoint: %v", err)
	}

	// Verify restored state
	if restored.Depth() != 2 {
		t.Errorf("expected depth 2, got %d", restored.Depth())
	}

	item, err := restored.Dequeue()
	if err != nil {
		t.Fatalf("failed to dequeue: %v", err)
	}

	if item.Query != "task2" { // Higher priority should come first
		t.Errorf("expected task2, got %s", item.Query)
	}
}

func TestWorkQueue_GetReadyItems(t *testing.T) {
	queue := NewWorkQueue()

	// Add items with dependencies
	_ = queue.Enqueue(WorkItem{
		Query:         "independent1",
		Actor:         "actor1",
		ID:            "id1",
		AllowParallel: true,
	})
	_ = queue.Enqueue(WorkItem{
		Query:         "dependent1",
		Actor:         "actor2",
		ID:            "id2",
		DependsOn:     []string{"id1"},
		AllowParallel: true,
	})
	_ = queue.Enqueue(WorkItem{
		Query:         "independent2",
		Actor:         "actor3",
		ID:            "id3",
		AllowParallel: true,
	})

	// No items completed yet - only independent items should be ready
	completedIDs := make(map[string]bool)
	ready := queue.GetReadyItems(completedIDs)

	if len(ready) != 2 {
		t.Errorf("expected 2 ready items, got %d", len(ready))
	}

	// Complete id1
	completedIDs["id1"] = true
	ready = queue.GetReadyItems(completedIDs)

	// Now dependent1 should also be ready
	if len(ready) != 3 {
		t.Errorf("expected 3 ready items after completing id1, got %d", len(ready))
	}
}

func TestWorkQueue_RemoveItems(t *testing.T) {
	queue := NewWorkQueue()

	_ = queue.Enqueue(WorkItem{Query: "task1", Actor: "actor1", ID: "id1"})
	_ = queue.Enqueue(WorkItem{Query: "task2", Actor: "actor2", ID: "id2"})
	_ = queue.Enqueue(WorkItem{Query: "task3", Actor: "actor3", ID: "id3"})

	if queue.Depth() != 3 {
		t.Fatalf("expected depth 3, got %d", queue.Depth())
	}

	// Remove id2
	queue.RemoveItems([]string{"id2"})

	if queue.Depth() != 2 {
		t.Errorf("expected depth 2 after removal, got %d", queue.Depth())
	}

	// Verify id2 is gone
	snapshot := queue.Snapshot()
	for _, item := range snapshot {
		if item.ID == "id2" {
			t.Error("id2 should have been removed")
		}
	}
}

func TestWorkQueue_ToJSON(t *testing.T) {
	queue := NewWorkQueue()
	queue.SetMaxPerActor("test_actor", 5)

	_ = queue.Enqueue(WorkItem{
		Query:         "task1",
		Actor:         "actor1",
		ID:            "id1",
		Priority:      10,
		AllowParallel: true,
		DependsOn:     []string{"id0"},
	})

	jsonData, err := queue.ToJSON()
	if err != nil {
		t.Fatalf("failed to export to JSON: %v", err)
	}

	if depth, ok := jsonData["depth"].(int); !ok || depth != 1 {
		t.Errorf("expected depth 1 in JSON, got %v", jsonData["depth"])
	}

	if maxDepth, ok := jsonData["max_depth"].(int); !ok || maxDepth != 10 {
		t.Errorf("expected max_depth 10 in JSON, got %v", jsonData["max_depth"])
	}

	items, ok := jsonData["items"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected items to be array of maps")
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item in JSON, got %d", len(items))
	}

	if items[0]["query"] != "task1" {
		t.Errorf("expected query 'task1', got %v", items[0]["query"])
	}

	if items[0]["allow_parallel"] != true {
		t.Errorf("expected allow_parallel true, got %v", items[0]["allow_parallel"])
	}
}

func TestWorkQueue_DependenciesWithoutParallel(t *testing.T) {
	queue := NewWorkQueue()

	// Add item with dependencies but AllowParallel = false
	_ = queue.Enqueue(WorkItem{
		Query:         "dependent",
		Actor:         "actor1",
		ID:            "id1",
		DependsOn:     []string{"id0"},
		AllowParallel: false, // Explicitly not parallel
	})

	completedIDs := map[string]bool{"id0": true}
	ready := queue.GetReadyItems(completedIDs)

	// Should not be ready even though dependency is satisfied because AllowParallel=false
	if len(ready) != 0 {
		t.Errorf("expected 0 ready items (AllowParallel=false), got %d", len(ready))
	}
}

// ===== Phase 5 Tests =====

func TestRetryStrategy_CalculateDelay(t *testing.T) {
	strategy := RetryStrategy{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
	}

	// First retry
	delay1 := strategy.CalculateDelay(1)
	if delay1 != 1*time.Second {
		t.Errorf("expected 1s delay for attempt 1, got %v", delay1)
	}

	// Second retry (exponential backoff: 1s * 2 = 2s)
	delay2 := strategy.CalculateDelay(2)
	if delay2 != 2*time.Second {
		t.Errorf("expected 2s delay for attempt 2, got %v", delay2)
	}

	// Third retry (exponential backoff: 2s * 2 = 4s)
	delay3 := strategy.CalculateDelay(3)
	if delay3 != 4*time.Second {
		t.Errorf("expected 4s delay for attempt 3, got %v", delay3)
	}
}

func TestRetryStrategy_MaxDelay(t *testing.T) {
	strategy := RetryStrategy{
		InitialDelay:  5 * time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 3.0,
	}

	// Should cap at MaxDelay
	delay := strategy.CalculateDelay(3) // Would be 45s without cap
	if delay > strategy.MaxDelay {
		t.Errorf("delay %v exceeds max delay %v", delay, strategy.MaxDelay)
	}
}

func TestCircuitBreaker_OpenOnFailures(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Second)

	failingFunc := func() error {
		return fmt.Errorf("simulated failure")
	}

	// First 3 failures should work (circuit still closed)
	for i := 0; i < 3; i++ {
		err := cb.Call(failingFunc)
		if err == nil {
			t.Error("expected error from failing function")
		}
	}

	// Circuit should now be open
	if cb.State() != "open" {
		t.Errorf("expected circuit to be open, got %s", cb.State())
	}

	// Next call should fail fast
	err := cb.Call(failingFunc)
	if err == nil || err.Error() != "circuit breaker open: too many failures" {
		t.Errorf("expected circuit breaker error, got %v", err)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	failingFunc := func() error {
		return fmt.Errorf("failure")
	}

	// Trigger circuit breaker
	cb.Call(failingFunc)
	cb.Call(failingFunc)

	if cb.State() != "open" {
		t.Fatalf("expected circuit to be open")
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Should transition to half-open
	successFunc := func() error {
		return nil
	}

	err := cb.Call(successFunc)
	if err != nil {
		t.Errorf("expected success after reset, got %v", err)
	}

	// Should be closed now
	if cb.State() != "closed" {
		t.Errorf("expected circuit to be closed after success, got %s", cb.State())
	}
}

func TestQueueMetrics_RecordTurn(t *testing.T) {
	metrics := NewQueueMetrics()

	// Record successful turn
	metrics.RecordTurn(TurnResult{
		TurnNumber: 1,
		Actor:      "actor1",
		Duration:   100 * time.Millisecond,
		Error:      nil,
	})

	// Record failed turn
	metrics.RecordTurn(TurnResult{
		TurnNumber: 2,
		Actor:      "actor2",
		Duration:   200 * time.Millisecond,
		Error:      fmt.Errorf("test error"),
	})

	if metrics.TotalTurns != 2 {
		t.Errorf("expected 2 total turns, got %d", metrics.TotalTurns)
	}

	if metrics.SuccessfulTurns != 1 {
		t.Errorf("expected 1 successful turn, got %d", metrics.SuccessfulTurns)
	}

	if metrics.FailedTurns != 1 {
		t.Errorf("expected 1 failed turn, got %d", metrics.FailedTurns)
	}

	if metrics.TurnsByActor["actor1"] != 1 {
		t.Errorf("expected 1 turn by actor1, got %d", metrics.TurnsByActor["actor1"])
	}

	if metrics.AverageDuration != 150*time.Millisecond {
		t.Errorf("expected 150ms average duration, got %v", metrics.AverageDuration)
	}
}

func TestQueueMetrics_ToMap(t *testing.T) {
	metrics := NewQueueMetrics()

	metrics.RecordTurn(TurnResult{
		TurnNumber: 1,
		Actor:      "test_actor",
		Duration:   100 * time.Millisecond,
	})

	metrics.RecordCheckpoint()

	metricsMap := metrics.ToMap()

	if metricsMap["total_turns"] != 1 {
		t.Errorf("expected total_turns=1, got %v", metricsMap["total_turns"])
	}

	if metricsMap["checkpoints"] != 1 {
		t.Errorf("expected checkpoints=1, got %v", metricsMap["checkpoints"])
	}

	if avgDuration, ok := metricsMap["avg_duration_ms"].(int64); !ok || avgDuration != 100 {
		t.Errorf("expected avg_duration_ms=100, got %v", metricsMap["avg_duration_ms"])
	}
}
