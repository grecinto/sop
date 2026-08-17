package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMultiTurnAskBasicWorkflow(t *testing.T) {
	// This test verifies basic multi-turn workflow execution
	// Note: This is a structural test - actual execution would require full agent setup

	queue := NewWorkQueue()

	// Simulate initial user query
	queue.Enqueue(WorkItem{
		Query: "Fix the parser bug",
		Actor: "user",
	})

	// Simulate capability injecting follow-up work
	queue.Enqueue(WorkItem{
		Query: "Generate patch for parser.go",
		Actor: "coding_workflow.patcher",
	})

	queue.Enqueue(WorkItem{
		Query: "Run tests",
		Actor: "coding_workflow.tester",
	})

	if queue.Depth() != 3 {
		t.Errorf("expected 3 items in queue, got %d", queue.Depth())
	}

	// Verify items dequeue in order
	item1, _ := queue.Dequeue()
	if item1.Actor != "user" {
		t.Errorf("expected first item from user, got %s", item1.Actor)
	}

	item2, _ := queue.Dequeue()
	if item2.Actor != "coding_workflow.patcher" {
		t.Errorf("expected second item from patcher, got %s", item2.Actor)
	}

	item3, _ := queue.Dequeue()
	if item3.Actor != "coding_workflow.tester" {
		t.Errorf("expected third item from tester, got %s", item3.Actor)
	}
}

func TestMultiTurnContextPropagation(t *testing.T) {
	ctx := context.Background()
	queue := NewWorkQueue()

	// Test context injection
	ctx = WithWorkQueue(ctx, queue)

	retrieved, ok := GetWorkQueue(ctx)
	if !ok {
		t.Fatal("failed to retrieve work queue from context")
	}

	if retrieved != queue {
		t.Error("retrieved queue doesn't match injected queue")
	}

	// Test turn context injection
	turnCtx := TurnContext{
		TurnNumber:  1,
		SharedState: map[string]any{"test_key": "test_value"},
	}

	ctx = WithTurnContext(ctx, turnCtx)

	retrievedTurnCtx, ok := GetTurnContext(ctx)
	if !ok {
		t.Fatal("failed to retrieve turn context from context")
	}

	if retrievedTurnCtx.TurnNumber != 1 {
		t.Errorf("expected turn number 1, got %d", retrievedTurnCtx.TurnNumber)
	}

	if retrievedTurnCtx.SharedState["test_key"] != "test_value" {
		t.Error("shared state not preserved in turn context")
	}
}

func TestIsQueueExecutionTurn(t *testing.T) {
	if isQueueExecutionTurn(context.Background()) {
		t.Fatal("expected normal context to not be marked as queue execution")
	}

	ctx := context.WithValue(context.Background(), ctxKeyQueueExecutionTurn, true)
	if !isQueueExecutionTurn(ctx) {
		t.Fatal("expected queue execution context to be detected")
	}
	if !shouldExecuteQueuedTurnDirectly(ctx) {
		t.Fatal("expected queued turns to run directly")
	}
}

func TestBuildEffectiveQueueQueryPreservesOriginalRequestContext(t *testing.T) {
	item := WorkItem{
		Query: "Run tests",
		Context: map[string]any{
			"parent_query": "fix failing tests",
			"working_dir":  "/tmp/repo",
		},
	}

	effective := buildEffectiveQueueQuery(item)
	if !strings.Contains(effective, "Original request: fix failing tests") {
		t.Fatalf("expected original request context to be preserved, got %q", effective)
	}
	if !strings.Contains(effective, "Current step: Run tests") {
		t.Fatalf("expected queued step to be preserved, got %q", effective)
	}
}

func TestBuildEffectiveQueueQueryIncludesControllerGuidance(t *testing.T) {
	item := WorkItem{
		Query: "Controller guidance: continue in planning and produce the execution-ready plan artifact.",
		Context: map[string]any{
			"original_query":      "fix failing tests",
			"controller_guidance": "Controller guidance: continue in planning and produce the execution-ready plan artifact.",
		},
	}

	effective := buildEffectiveQueueQuery(item)
	if !strings.Contains(effective, "Original request: fix failing tests") {
		t.Fatalf("expected original request context to be preserved, got %q", effective)
	}
	if !strings.Contains(effective, "Controller guidance: continue in planning") {
		t.Fatalf("expected controller guidance to be included, got %q", effective)
	}
}

func TestConditionalWorkItemExecution(t *testing.T) {
	queue := NewWorkQueue()

	// Create conditional work item
	conditionalItem := WorkItem{
		Query: "Fix test failures",
		Actor: "coding_workflow.repair",
		Condition: func(result TurnResult) bool {
			return result.TestsFailed
		},
	}

	queue.Enqueue(conditionalItem)

	dequeued, _ := queue.Dequeue()

	// Test condition with failed tests
	resultWithFailure := TurnResult{
		TestsFailed: true,
	}

	if !dequeued.Condition(resultWithFailure) {
		t.Error("condition should return true for failed tests")
	}

	// Test condition with passing tests
	resultWithSuccess := TurnResult{
		TestsFailed: false,
	}

	if dequeued.Condition(resultWithSuccess) {
		t.Error("condition should return false for passing tests")
	}
}

func TestTurnResultAggregation(t *testing.T) {
	results := []TurnResult{
		{TurnNumber: 0, Actor: "user", Response: "Analyzed code", Duration: 100 * time.Millisecond},
		{TurnNumber: 1, Actor: "coding_workflow", Response: "Generated patch", Duration: 200 * time.Millisecond},
		{TurnNumber: 2, Actor: "test_runner", Response: "Tests passed", Duration: 300 * time.Millisecond},
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Verify results maintain turn order
	for i, result := range results {
		if result.TurnNumber != i {
			t.Errorf("turn %d has incorrect turn number %d", i, result.TurnNumber)
		}
	}

	// Verify total duration is sum of all turns
	var totalDuration time.Duration
	for _, result := range results {
		totalDuration += result.Duration
	}

	expectedDuration := 600 * time.Millisecond
	if totalDuration != expectedDuration {
		t.Errorf("expected total duration %v, got %v", expectedDuration, totalDuration)
	}
}

func TestDetectTestFailures(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     bool
	}{
		{
			name:     "explicit failure message",
			response: "Tests failed with 3 failures",
			want:     true,
		},
		{
			name:     "error indicator",
			response: "Error: assertion failed",
			want:     true,
		},
		{
			name:     "success message",
			response: "All tests passed successfully",
			want:     false,
		},
		{
			name:     "neutral message",
			response: "Completed test execution",
			want:     false,
		},
		{
			name:     "failure symbol",
			response: "Test result: ✗",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTestFailures(tt.response)
			if got != tt.want {
				t.Errorf("detectTestFailures() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string truncated",
			input:  "this is a very long string",
			maxLen: 10,
			want:   "this is...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 5,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizePreviousTurns(t *testing.T) {
	results := []TurnResult{
		{TurnNumber: 0, Actor: "user", Response: "Long response that should be truncated to a reasonable length for summary purposes"},
		{TurnNumber: 1, Actor: "coding_workflow", Response: "Short response"},
		{TurnNumber: 2, Actor: "test_runner", Error: fmt.Errorf("generator not found")},
	}

	summary := summarizePreviousTurns(results)

	if summary == "" {
		t.Error("expected non-empty summary")
	}

	// Summary should include turn indicators
	if !strings.Contains(summary, "Turn 1") {
		t.Error("summary should contain turn indicators")
	}

	// Summary should indicate errors
	if !strings.Contains(summary, "Error") {
		t.Error("summary should indicate errors for failed turns")
	}
}

func TestWorkQueueActorLimitsInMultiTurn(t *testing.T) {
	queue := NewWorkQueue()
	queue.SetMaxPerActor("test_actor", 2)

	// Should allow 2 items
	err1 := queue.Enqueue(WorkItem{Actor: "test_actor", Query: "1"})
	err2 := queue.Enqueue(WorkItem{Actor: "test_actor", Query: "2"})

	if err1 != nil || err2 != nil {
		t.Fatal("should allow up to 2 items per actor")
	}

	// Third should fail
	err3 := queue.Enqueue(WorkItem{Actor: "test_actor", Query: "3"})
	if err3 == nil {
		t.Error("should reject third item from same actor")
	}

	// After dequeue, should allow another
	queue.Dequeue()
	err4 := queue.Enqueue(WorkItem{Actor: "test_actor", Query: "4"})
	if err4 != nil {
		t.Error("should allow item after dequeue frees slot")
	}
}

func TestMultiTurnPriorityOrdering(t *testing.T) {
	queue := NewWorkQueue()

	// Enqueue items with different priorities
	queue.Enqueue(WorkItem{Query: "low priority", Actor: "user", Priority: 1})
	queue.Enqueue(WorkItem{Query: "urgent", Actor: "error_recovery", Priority: 100})
	queue.Enqueue(WorkItem{Query: "medium priority", Actor: "coding_workflow", Priority: 50})

	// Should dequeue in priority order
	item1, _ := queue.Dequeue()
	if item1.Priority != 100 {
		t.Errorf("expected highest priority (100) first, got %d", item1.Priority)
	}

	item2, _ := queue.Dequeue()
	if item2.Priority != 50 {
		t.Errorf("expected medium priority (50) second, got %d", item2.Priority)
	}

	item3, _ := queue.Dequeue()
	if item3.Priority != 1 {
		t.Errorf("expected lowest priority (1) third, got %d", item3.Priority)
	}
}

func TestShouldAttemptRecovery(t *testing.T) {
	agent := &CopilotAgent{}

	tests := []struct {
		name        string
		err         error
		currentTurn int
		maxTurns    int
		want        bool
	}{
		{
			name:        "recoverable error early in execution",
			err:         fmt.Errorf("temporary network error"),
			currentTurn: 2,
			maxTurns:    10,
			want:        true,
		},
		{
			name:        "fatal error - no api key",
			err:         fmt.Errorf("no valid API key found"),
			currentTurn: 2,
			maxTurns:    10,
			want:        false,
		},
		{
			name:        "near max turns limit",
			err:         fmt.Errorf("some error"),
			currentTurn: 9,
			maxTurns:    10,
			want:        false,
		},
		{
			name:        "context canceled",
			err:         context.Canceled,
			currentTurn: 2,
			maxTurns:    10,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.shouldAttemptRecovery(tt.err, tt.currentTurn, tt.maxTurns)
			if got != tt.want {
				t.Errorf("shouldAttemptRecovery() = %v, want %v", got, tt.want)
			}
		})
	}
}
