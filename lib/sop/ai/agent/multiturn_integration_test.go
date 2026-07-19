package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/inmemory"
)

// Helper to create in-memory b-tree stores for testing
func createTestPersistence(ctx context.Context, t *testing.T) *QueuePersistence {
	t.Helper()
	checkpoints := inmemory.NewBtree[string, []byte](false).Btree
	metrics := inmemory.NewBtree[string, []byte](false).Btree
	byTimestamp := inmemory.NewBtree[string, string](false).Btree

	persistence, err := NewQueuePersistence(ctx, checkpoints, metrics, byTimestamp)
	if err != nil {
		t.Fatalf("failed to create persistence: %v", err)
	}
	return persistence
}

// ===== Integration Tests for Multi-Turn Orchestration =====

// TestIntegration_BasicWorkflow tests a complete multi-turn workflow end-to-end.
func TestIntegration_BasicWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	queue := NewWorkQueue()

	// Enqueue multiple work items
	_ = queue.Enqueue(WorkItem{
		Query:    "Analyze the codebase",
		Actor:    "analyzer",
		Priority: 10,
		ID:       "task-1",
	})

	_ = queue.Enqueue(WorkItem{
		Query:    "Generate implementation plan",
		Actor:    "planner",
		Priority: 5,
		ID:       "task-2",
	})

	_ = queue.Enqueue(WorkItem{
		Query:    "Execute implementation",
		Actor:    "executor",
		Priority: 1,
		ID:       "task-3",
	})

	if queue.Depth() != 3 {
		t.Fatalf("expected 3 items in queue, got %d", queue.Depth())
	}

	// Process items in priority order
	var processedActors []string
	for !queue.IsEmpty() {
		item, err := queue.Dequeue()
		if err != nil {
			t.Fatalf("failed to dequeue: %v", err)
		}
		processedActors = append(processedActors, item.Actor)
	}

	// Verify priority ordering
	if len(processedActors) != 3 {
		t.Errorf("expected 3 processed items, got %d", len(processedActors))
	}

	if processedActors[0] != "analyzer" {
		t.Errorf("expected analyzer first (priority 10), got %s", processedActors[0])
	}

	if processedActors[2] != "executor" {
		t.Errorf("expected executor last (priority 1), got %s", processedActors[2])
	}

	_ = ctx // Satisfy linter
}

// TestIntegration_ConditionalExecution tests conditional branching in workflows.
func TestIntegration_ConditionalExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	queue := NewWorkQueue()

	// Enqueue items with conditions
	_ = queue.Enqueue(WorkItem{
		Query:    "Run tests",
		Actor:    "test_runner",
		Priority: 10,
		ID:       "test-1",
	})

	_ = queue.EnqueueConditional(WorkItem{
		Query:    "Fix failing tests",
		Actor:    "fixer",
		Priority: 5,
		ID:       "fix-1",
		Condition: func(result TurnResult) bool {
			return result.TestsFailed
		},
	})

	_ = queue.EnqueueConditional(WorkItem{
		Query:    "Deploy to production",
		Actor:    "deployer",
		Priority: 1,
		ID:       "deploy-1",
		Condition: func(result TurnResult) bool {
			return !result.TestsFailed
		},
	})

	// Simulate test failure
	testResult := TurnResult{
		TurnNumber:  1,
		Actor:       "test_runner",
		TestsFailed: true,
	}

	// Verify conditional logic
	if queue.Depth() != 3 {
		t.Errorf("expected 3 items before conditions, got %d", queue.Depth())
	}

	// Process test task
	testItem, _ := queue.Dequeue()
	if testItem.Actor != "test_runner" {
		t.Errorf("expected test_runner, got %s", testItem.Actor)
	}

	// Check fix condition (should execute)
	fixItem, _ := queue.Dequeue()
	if fixItem.Condition != nil && !fixItem.Condition(testResult) {
		t.Error("fix task should execute when tests fail")
	}

	// Check deploy condition (should not execute)
	deployItem, _ := queue.Dequeue()
	if deployItem.Condition != nil && deployItem.Condition(testResult) {
		t.Error("deploy task should not execute when tests fail")
	}
}

// TestIntegration_ParallelExecution tests parallel turn execution.
func TestIntegration_ParallelExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	queue := NewWorkQueue()

	// Add independent parallel tasks
	tasks := []WorkItem{
		{Query: "Task A", Actor: "worker1", ID: "a", AllowParallel: true},
		{Query: "Task B", Actor: "worker2", ID: "b", AllowParallel: true},
		{Query: "Task C", Actor: "worker3", ID: "c", AllowParallel: true},
	}

	for _, task := range tasks {
		_ = queue.Enqueue(task)
	}

	// Add dependent task
	_ = queue.Enqueue(WorkItem{
		Query:         "Task D (depends on A, B, C)",
		Actor:         "worker4",
		ID:            "d",
		DependsOn:     []string{"a", "b", "c"},
		AllowParallel: true, // Allow to be returned by GetReadyItems
	})

	completedIDs := make(map[string]bool)

	// Get ready items (should be A, B, C)
	ready := queue.GetReadyItems(completedIDs)
	if len(ready) != 3 {
		t.Errorf("expected 3 ready items, got %d", len(ready))
	}

	// Mark as completed
	for _, item := range ready {
		completedIDs[item.ID] = true
	}

	// Now D should be ready
	ready = queue.GetReadyItems(completedIDs)
	hasTaskD := false
	for _, item := range ready {
		if item.ID == "d" {
			hasTaskD = true
		}
	}

	if !hasTaskD {
		t.Error("Task D should be ready after A, B, C complete")
	}
}

// TestIntegration_CheckpointResume tests checkpoint and resume functionality.
func TestIntegration_CheckpointResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create queue with work items
	queue := NewWorkQueue()
	queue.SetMaxPerActor("test_actor", 5)

	_ = queue.Enqueue(WorkItem{Query: "Task 1", Actor: "actor1", ID: "t1"})
	_ = queue.Enqueue(WorkItem{Query: "Task 2", Actor: "actor2", ID: "t2"})
	_ = queue.Enqueue(WorkItem{Query: "Task 3", Actor: "actor3", ID: "t3"})

	// Create checkpoint
	checkpoint := queue.Checkpoint()

	if len(checkpoint.Items) != 3 {
		t.Errorf("expected 3 items in checkpoint, got %d", len(checkpoint.Items))
	}

	// Simulate crash - create new queue
	newQueue := NewWorkQueue()

	// Restore from checkpoint
	err := newQueue.RestoreFromCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("failed to restore checkpoint: %v", err)
	}

	// Verify restored state
	if newQueue.Depth() != 3 {
		t.Errorf("expected depth 3 after restore, got %d", newQueue.Depth())
	}

	// Verify can continue processing
	item, err := newQueue.Dequeue()
	if err != nil {
		t.Fatalf("failed to dequeue after restore: %v", err)
	}

	if item.Query == "" {
		t.Error("restored item should have query")
	}
}

// TestIntegration_Persistence tests b-tree-backed persistence.
func TestIntegration_Persistence(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create in-memory persistence
	persistence := createTestPersistence(ctx, t)

	// Create checkpoint
	queue := NewWorkQueue()
	_ = queue.Enqueue(WorkItem{Query: "Test task", Actor: "actor1", ID: "t1"})

	checkpoint := queue.Checkpoint()
	checkpoint.TurnResults = []TurnResult{
		{TurnNumber: 1, Actor: "actor1", Response: "Response 1"},
	}

	// Save checkpoint
	err := persistence.SaveCheckpoint(ctx, checkpoint)
	if err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// Load checkpoint
	loaded, err := persistence.LoadCheckpoint(ctx, checkpoint.CheckpointID)
	if err != nil {
		t.Fatalf("failed to load checkpoint: %v", err)
	}

	if len(loaded.Items) != 1 {
		t.Errorf("expected 1 item in loaded checkpoint, got %d", len(loaded.Items))
	}

	if len(loaded.TurnResults) != 1 {
		t.Errorf("expected 1 turn result, got %d", len(loaded.TurnResults))
	}

	if loaded.TurnResults[0].Response != "Response 1" {
		t.Errorf("expected 'Response 1', got %s", loaded.TurnResults[0].Response)
	}

	// Test metrics persistence
	result := TurnResult{
		TurnNumber: 2,
		Actor:      "actor2",
		Duration:   100 * time.Millisecond,
	}

	err = persistence.SaveMetric(ctx, checkpoint.CheckpointID, result)
	if err != nil {
		t.Errorf("failed to save metric: %v", err)
	}
}

// TestIntegration_CircuitBreakerWithRetry tests circuit breaker with retry strategy.
func TestIntegration_CircuitBreakerWithRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cb := NewCircuitBreaker(3, 100*time.Millisecond)
	strategy := DefaultRetryStrategy()

	failCount := 0
	maxFails := 2

	operationFunc := func() error {
		failCount++
		if failCount <= maxFails {
			return fmt.Errorf("simulated failure %d", failCount)
		}
		return nil // Success on third attempt
	}

	// Execute with retries
	var lastErr error
	for attempt := 0; attempt < strategy.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := strategy.CalculateDelay(attempt)
			time.Sleep(delay)
		}

		err := cb.Call(operationFunc)
		if err == nil {
			lastErr = nil
			break
		}

		lastErr = err
	}

	if lastErr != nil {
		t.Errorf("expected success after retries, got error: %v", lastErr)
	}

	if cb.State() != "closed" {
		t.Errorf("expected circuit to be closed after success, got %s", cb.State())
	}
}

// TestIntegration_MetricsTracking tests comprehensive metrics tracking.
func TestIntegration_MetricsTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	metrics := NewQueueMetrics()

	// Simulate a workflow
	results := []TurnResult{
		{TurnNumber: 1, Actor: "analyzer", Duration: 100 * time.Millisecond},
		{TurnNumber: 2, Actor: "planner", Duration: 200 * time.Millisecond},
		{TurnNumber: 3, Actor: "executor", Duration: 150 * time.Millisecond, Error: fmt.Errorf("execution failed")},
		{TurnNumber: 4, Actor: "executor", Duration: 180 * time.Millisecond}, // Retry success
	}

	for _, result := range results {
		metrics.RecordTurn(result)
	}

	metrics.RecordCheckpoint()

	// Verify metrics
	if metrics.TotalTurns != 4 {
		t.Errorf("expected 4 total turns, got %d", metrics.TotalTurns)
	}

	if metrics.SuccessfulTurns != 3 {
		t.Errorf("expected 3 successful turns, got %d", metrics.SuccessfulTurns)
	}

	if metrics.FailedTurns != 1 {
		t.Errorf("expected 1 failed turn, got %d", metrics.FailedTurns)
	}

	if metrics.TurnsByActor["executor"] != 2 {
		t.Errorf("expected 2 turns by executor, got %d", metrics.TurnsByActor["executor"])
	}

	if metrics.CheckpointsCount != 1 {
		t.Errorf("expected 1 checkpoint, got %d", metrics.CheckpointsCount)
	}

	// Export and verify
	metricsMap := metrics.ToMap()
	if avgDuration, ok := metricsMap["avg_duration_ms"].(int64); !ok || avgDuration != 157 {
		t.Errorf("expected avg_duration_ms=157, got %v", metricsMap["avg_duration_ms"])
	}
}

// TestIntegration_ProgressStreaming tests real-time progress callbacks.
func TestIntegration_ProgressStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	var progressUpdates []string
	callback := func(result TurnResult) {
		msg := fmt.Sprintf("Turn %d (%s): %d chars", result.TurnNumber, result.Actor, len(result.Response))
		progressUpdates = append(progressUpdates, msg)
	}

	// Simulate progress updates
	results := []TurnResult{
		{TurnNumber: 1, Actor: "analyzer", Response: "Analysis complete"},
		{TurnNumber: 2, Actor: "planner", Response: "Plan generated"},
		{TurnNumber: 3, Actor: "executor", Response: "Execution done"},
	}

	for _, result := range results {
		callback(result)
	}

	if len(progressUpdates) != 3 {
		t.Errorf("expected 3 progress updates, got %d", len(progressUpdates))
	}

	if !strings.Contains(progressUpdates[0], "analyzer") {
		t.Errorf("expected analyzer in first update, got %s", progressUpdates[0])
	}
}

// TestIntegration_FullWorkflowWithAllFeatures tests all Phase 4 & 5 features together.
func TestIntegration_FullWorkflowWithAllFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup: Create queue, persistence, metrics, circuit breaker
	queue := NewWorkQueue()
	metrics := NewQueueMetrics()
	cb := NewCircuitBreaker(3, 1*time.Second)

	// Create in-memory persistence
	persistence := createTestPersistence(ctx, t)

	// Add work items with dependencies
	_ = queue.Enqueue(WorkItem{
		Query:         "Analyze code",
		Actor:         "analyzer",
		ID:            "analyze",
		Priority:      10,
		AllowParallel: true,
	})

	_ = queue.Enqueue(WorkItem{
		Query:         "Run tests",
		Actor:         "tester",
		ID:            "test",
		Priority:      10,
		AllowParallel: true,
	})

	_ = queue.Enqueue(WorkItem{
		Query:         "Generate report",
		Actor:         "reporter",
		ID:            "report",
		Priority:      5,
		DependsOn:     []string{"analyze", "test"},
		AllowParallel: false,
	})

	// Create checkpoint before processing
	checkpoint := queue.Checkpoint()
	err := persistence.SaveCheckpoint(ctx, checkpoint)
	if err != nil {
		t.Fatalf("failed to save initial checkpoint: %v", err)
	}

	// Process workflow
	completedIDs := make(map[string]bool)
	turnNumber := 0

	for !queue.IsEmpty() && turnNumber < 10 {
		// Get ready items for parallel execution
		ready := queue.GetReadyItems(completedIDs)

		if len(ready) == 0 {
			// Dequeue next item
			item, err := queue.Dequeue()
			if err != nil {
				break
			}

			// Simulate turn execution with circuit breaker
			err = cb.Call(func() error {
				// Simulate successful execution
				result := TurnResult{
					TurnNumber: turnNumber,
					Actor:      item.Actor,
					Response:   fmt.Sprintf("Completed %s", item.Query),
					Duration:   50 * time.Millisecond,
				}

				metrics.RecordTurn(result)
				completedIDs[item.ID] = true

				// Save metric
				return persistence.SaveMetric(ctx, checkpoint.CheckpointID, result)
			})

			if err != nil {
				t.Errorf("turn execution failed: %v", err)
			}
		} else {
			// Process ready items (parallel execution simulation)
			for _, item := range ready {
				queue.RemoveItems([]string{item.ID})
				completedIDs[item.ID] = true

				result := TurnResult{
					TurnNumber: turnNumber,
					Actor:      item.Actor,
					Response:   fmt.Sprintf("Parallel: %s", item.Query),
					Duration:   30 * time.Millisecond,
				}

				metrics.RecordTurn(result)
				_ = persistence.SaveMetric(ctx, checkpoint.CheckpointID, result)
			}
		}

		turnNumber++
	}

	// Verify all tasks completed
	if !completedIDs["analyze"] || !completedIDs["test"] || !completedIDs["report"] {
		t.Error("not all tasks completed")
	}

	// Verify metrics
	if metrics.TotalTurns != 3 {
		t.Errorf("expected 3 turns, got %d", metrics.TotalTurns)
	}

	// Verify circuit breaker state
	if cb.State() != "closed" {
		t.Errorf("expected circuit breaker closed, got %s", cb.State())
	}

	_ = ctx // Satisfy linter
}
