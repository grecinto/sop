package agent

import (
	"context"
	"fmt"
	log "log/slog"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// EnablePhase5 activates production hardening features (retry, circuit breaker, metrics, persistence).
// Call this on your CopilotAgent instance to enable Phase 5.
func (a *CopilotAgent) EnablePhase5(retryStrategy *RetryStrategy, circuitBreaker *CircuitBreaker, metrics *QueueMetrics, persistence *QueuePersistence) {
	a.enablePhase5 = true

	if retryStrategy != nil {
		a.retryStrategy = retryStrategy
	} else {
		// Use default retry strategy
		defaultStrategy := DefaultRetryStrategy()
		a.retryStrategy = &defaultStrategy
	}

	if circuitBreaker != nil {
		a.circuitBreaker = circuitBreaker
	} else {
		// Default: 5 failures before opening circuit, 1 minute reset
		a.circuitBreaker = NewCircuitBreaker(5, 1*time.Minute)
	}

	if metrics != nil {
		a.queueMetrics = metrics
	} else {
		a.queueMetrics = NewQueueMetrics()
	}

	a.queuePersist = persistence // Can be nil if persistence not needed

	log.Info("Phase 5 production hardening enabled",
		"retry_max_attempts", a.retryStrategy.MaxAttempts,
		"circuit_breaker_threshold", a.circuitBreaker.maxFailures,
		"persistence_enabled", a.queuePersist != nil)
}

// DisablePhase5 turns off production hardening features.
func (a *CopilotAgent) DisablePhase5() {
	a.enablePhase5 = false
	log.Info("Phase 5 production hardening disabled")
}

// GetPhase5Metrics returns current Phase 5 metrics as a map.
func (a *CopilotAgent) GetPhase5Metrics() map[string]interface{} {
	if !a.enablePhase5 || a.queueMetrics == nil {
		return map[string]interface{}{"phase5_enabled": false}
	}

	metrics := a.queueMetrics.ToMap()
	metrics["phase5_enabled"] = true
	metrics["circuit_breaker_state"] = a.circuitBreaker.State()
	return metrics
}

// workQueueAdapter adapts WorkQueue to implement capabilities.WorkQueue interface
type workQueueAdapter struct {
	queue *WorkQueue
}

func (a *workQueueAdapter) Enqueue(item interface{}) error {
	// Convert interface{} to WorkItem
	workItem, ok := convertToWorkItem(item)
	if !ok {
		return fmt.Errorf("invalid work item type")
	}
	return a.queue.Enqueue(workItem)
}

func (a *workQueueAdapter) EnqueueConditional(item interface{}) error {
	workItem, ok := convertToWorkItem(item)
	if !ok {
		return fmt.Errorf("invalid work item type")
	}
	return a.queue.EnqueueConditional(workItem)
}

func (a *workQueueAdapter) Depth() int {
	return a.queue.Depth()
}

func (a *workQueueAdapter) IsEmpty() bool {
	return a.queue.IsEmpty()
}

// convertToWorkItem converts interface{} to WorkItem
// Accepts map[string]interface{} or WorkItem
func convertToWorkItem(item interface{}) (WorkItem, bool) {
	switch v := item.(type) {
	case WorkItem:
		return v, true
	case map[string]interface{}:
		workItem := WorkItem{}
		if query, ok := v["query"].(string); ok {
			workItem.Query = query
		}
		if actor, ok := v["actor"].(string); ok {
			workItem.Actor = actor
		}
		if priority, ok := v["priority"].(int); ok {
			workItem.Priority = priority
		}
		if cond, ok := v["condition"].(func(TurnResult) bool); ok {
			workItem.Condition = cond
		}
		return workItem, true
	default:
		return WorkItem{}, false
	}
}

// processQueueLoop is the SINGLE queue processor - dequeues items and calls Ask().
// This is the only place that should loop over queue items.
func (a *CopilotAgent) processQueueLoop(ctx context.Context, queue *WorkQueue, cfg *ai.ConfigMap, maxTurns int) []TurnResult {
	var turnResults []TurnResult
	turnNumber := 0

	// Phase 5: Initialize session checkpoint ID
	var checkpointID string
	if a.enablePhase5 && a.queuePersist != nil {
		checkpointID = fmt.Sprintf("session-%d", time.Now().Unix())
	}

	log.Info("Queue processing started", "max_turns", maxTurns, "queue_depth", queue.Depth(), "phase5_enabled", a.enablePhase5)

	for !queue.IsEmpty() && turnNumber < maxTurns {
		item, err := queue.Dequeue()
		if err != nil {
			break
		}

		// Check conditional execution
		if item.Condition != nil && len(turnResults) > 0 {
			lastResult := turnResults[len(turnResults)-1]
			if !item.Condition(lastResult) {
				log.Info("Skipping turn due to unmet condition", "turn", turnNumber, "actor", item.Actor)
				turnNumber++
				continue
			}
		}

		// Execute turn by calling Ask() - queue as prompt source!
		turnCtx, cancel := context.WithTimeout(ctx, queue.GetTurnTimeout())

		log.Info("Executing turn", "turn_number", turnNumber, "actor", item.Actor, "query_preview", truncateString(item.Query, 100))

		startTime := time.Now()

		// Phase 5: Execute with retry and circuit breaker
		var response string
		var execErr error

		if a.enablePhase5 && a.retryStrategy != nil {
			response, execErr = a.executeWithRetry(turnCtx, item.Query, cfg, queue)
		} else {
			response, execErr = a.askWithQueue(turnCtx, item.Query, cfg, queue)
		}

		result := TurnResult{
			Duration:   time.Since(startTime),
			TurnNumber: turnNumber,
			Actor:      item.Actor,
			Query:      item.Query,
			Response:   response,
			Error:      execErr,
			Data:       make(map[string]any),
		}
		cancel()

		// Phase 5: Record metrics and save checkpoint
		if a.enablePhase5 {
			if a.queueMetrics != nil {
				a.queueMetrics.RecordTurn(result)
			}
			if a.queuePersist != nil && checkpointID != "" {
				checkpoint := QueueCheckpoint{
					CheckpointID: checkpointID,
					Timestamp:    time.Now(),
					Items:        queue.Snapshot(),
					TurnResults:  turnResults,
					ActorCount:   make(map[string]int),
					MaxDepth:     queue.maxDepth,
					MaxPerActor:  queue.maxPerActor,
				}
				if saveErr := a.queuePersist.SaveCheckpoint(ctx, checkpoint); saveErr != nil {
					log.Warn("Failed to save checkpoint", "error", saveErr)
				} else {
					a.queueMetrics.RecordCheckpoint()
				}
				if saveErr := a.queuePersist.SaveMetric(ctx, checkpointID, result); saveErr != nil {
					log.Warn("Failed to save metric", "error", saveErr)
				}
			}
		}

		if execErr != nil && a.shouldAttemptRecovery(execErr, turnNumber, maxTurns) {
			log.Info("Injecting recovery turn", "error", execErr, "turn", turnNumber)
			recoveryItem := WorkItem{
				Query:    fmt.Sprintf("Recover from error: %v. Original query was: %s", execErr, item.Query),
				Actor:    "error_recovery",
				Priority: 100,
				Context:  map[string]any{"failed_turn": item, "error": execErr.Error()},
			}
			queue.Enqueue(recoveryItem)
		}

		turnResults = append(turnResults, result)
		log.Info("Turn completed", "turn_number", turnNumber, "actor", item.Actor, "response_chars", len(result.Response))

		turnNumber++
	}

	if turnNumber >= maxTurns {
		log.Warn("Queue processing hit max turns limit", "turns", turnNumber, "queue_depth", queue.Depth())
	}

	log.Info("Queue processing completed", "total_turns", len(turnResults), "queue_remaining", queue.Depth())

	// Phase 5: Log final metrics
	if a.enablePhase5 && a.queueMetrics != nil {
		metrics := a.queueMetrics.ToMap()
		log.Info("Phase 5 metrics", "metrics", metrics)
	}

	return turnResults
}

// executeWithRetry wraps askWithQueue with retry logic and circuit breaker.
// Phase 5: Production hardening for unreliable LLM APIs.
func (a *CopilotAgent) executeWithRetry(ctx context.Context, query string, cfg *ai.ConfigMap, queue *WorkQueue) (string, error) {
	if a.retryStrategy == nil {
		return a.askWithQueue(ctx, query, cfg, queue)
	}

	var lastErr error
	for attempt := 0; attempt < a.retryStrategy.MaxAttempts; attempt++ {
		// Check circuit breaker if configured
		if a.circuitBreaker != nil && a.circuitBreaker.State() == "open" {
			return "", fmt.Errorf("circuit breaker open: too many failures")
		}

		// Execute with circuit breaker protection if available
		var response string
		var err error

		if a.circuitBreaker != nil {
			err = a.circuitBreaker.Call(func() error {
				var callErr error
				response, callErr = a.askWithQueue(ctx, query, cfg, queue)
				return callErr
			})
		} else {
			response, err = a.askWithQueue(ctx, query, cfg, queue)
		}

		if err == nil {
			if attempt > 0 {
				log.Info("Retry succeeded", "attempt", attempt+1)
			}
			return response, nil
		}

		lastErr = err

		// Check if error is retryable
		if !a.isRetryableError(err) {
			log.Warn("Non-retryable error encountered", "error", err)
			return response, err
		}

		// Don't retry on last attempt
		if attempt >= a.retryStrategy.MaxAttempts-1 {
			break
		}

		// Calculate backoff delay
		delay := a.retryStrategy.CalculateDelay(attempt + 1)
		log.Warn("Retrying after error", "attempt", attempt+1, "max_attempts", a.retryStrategy.MaxAttempts, "delay", delay, "error", err)

		// Wait before retry
		select {
		case <-time.After(delay):
			// Continue to next attempt
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	return "", fmt.Errorf("max retry attempts exceeded: %w", lastErr)
}

// isRetryableError checks if an error should trigger a retry.
// Phase 5: Identifies transient failures vs fatal errors.
func (a *CopilotAgent) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Check against configured retryable errors
	if a.retryStrategy != nil {
		for _, pattern := range a.retryStrategy.RetryableErrors {
			if strings.Contains(errStr, strings.ToLower(pattern)) {
				return true
			}
		}
	}

	// Default retryable patterns
	retryablePatterns := []string{
		"timeout",
		"rate limit",
		"429",
		"503",
		"service unavailable",
		"temporary",
		"connection reset",
		"eof",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// aggregateMultiTurnResults combines results from all turns into a final response.
func (a *CopilotAgent) aggregateMultiTurnResults(results []TurnResult) string {
	if len(results) == 0 {
		return "Multi-turn execution completed with no results"
	}

	if len(results) == 1 {
		return results[0].Response
	}

	// Build comprehensive summary
	var sb strings.Builder

	// Use last turn's response as primary output (most relevant to user)
	lastResult := results[len(results)-1]
	sb.WriteString(lastResult.Response)

	// Add workflow summary if multiple turns executed
	if len(results) > 1 {
		sb.WriteString("\n\n---\n\n**Workflow Summary:**\n")
		for i, result := range results {
			if result.Error != nil {
				sb.WriteString(fmt.Sprintf("\n- Turn %d (%s): ❌ Error: %v", i+1, result.Actor, result.Error))
			} else {
				summary := truncateString(result.Response, 150)
				sb.WriteString(fmt.Sprintf("\n- Turn %d (%s): ✓ %s", i+1, result.Actor, summary))
			}
		}
	}

	return sb.String()
}

// shouldAttemptRecovery determines if error recovery should be attempted.
func (a *CopilotAgent) shouldAttemptRecovery(err error, currentTurn, maxTurns int) bool {
	// Don't attempt recovery if we're close to max turns
	if currentTurn >= maxTurns-2 {
		return false
	}

	// Check error type - some errors are fatal
	errStr := strings.ToLower(err.Error())
	fatalPatterns := []string{
		"no valid api key",
		"context canceled",
		"context deadline exceeded",
		"permission denied",
	}

	for _, pattern := range fatalPatterns {
		if strings.Contains(errStr, pattern) {
			return false
		}
	}

	return true
}

// detectTestFailures checks if the response indicates test failures.
func detectTestFailures(response string) bool {
	lowerResponse := strings.ToLower(response)
	failureIndicators := []string{
		"test failed",
		"tests failed",
		"failure",
		"error:",
		"✗",
		"failed:",
	}

	for _, indicator := range failureIndicators {
		if strings.Contains(lowerResponse, indicator) {
			return true
		}
	}

	return false
}

// summarizePreviousTurns creates a compact summary of previous turn results.
func summarizePreviousTurns(results []TurnResult) string {
	if len(results) == 0 {
		return ""
	}

	var parts []string
	for i, result := range results {
		if result.Error != nil {
			parts = append(parts, fmt.Sprintf("Turn %d (%s): Error", i+1, result.Actor))
		} else {
			summary := truncateString(result.Response, 100)
			parts = append(parts, fmt.Sprintf("Turn %d (%s): %s", i+1, result.Actor, summary))
		}
	}

	return strings.Join(parts, " | ")
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// shouldUseMultiTurn determines if a capability should use multi-turn orchestration.
func (a *CopilotAgent) shouldUseMultiTurn(ctx context.Context, query string, taskContext *TaskContextClassification) bool {
	if a.capabilityRegistry == nil {
		return false
	}

	// Multi-turn orchestration is currently only for coding tasks, not data queries
	// Skip if domain is Stores, Spaces, or other data-oriented domains
	if taskContext.Domain == "Stores" || taskContext.Domain == "Spaces" {
		return false
	}

	// Check if capability is multi-turn capable
	state := capabilities.RoutingState{
		Domain:         taskContext.Domain,
		Gate:           taskContext.RoutingGate,
		CapabilityHint: taskContext.CapabilityHint,
	}

	cap, ok := a.capabilityRegistry.Resolve(query, state)
	if !ok {
		return false
	}

	// Check if capability implements MultiTurnCapability
	_, isMultiTurn := cap.(capabilities.MultiTurnCapability)
	return isMultiTurn
}
