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

// multiTurnAsk executes a multi-turn workflow using queue-based orchestration.
// This enables capabilities to inject work items that execute as separate ReAct turns.
func (a *CopilotAgent) multiTurnAsk(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	ctx = a.applyAskConfig(ctx, cfg)
	a.clearAskProgressMRU()

	// Initialize work queue
	queue := NewWorkQueue()
	queue.SetMaxPerActor("coding_workflow", 8) // Allow coding workflow more turns
	queue.SetMaxPerActor("test_runner", 5)

	// Enqueue initial user query
	initialItem := WorkItem{
		Query: query,
		Actor: "user",
		ID:    "turn-0",
	}
	if err := queue.Enqueue(initialItem); err != nil {
		return "", fmt.Errorf("failed to enqueue initial query: %w", err)
	}

	// Inject queue into context for capability access
	// Wrap the queue in an adapter to implement capabilities.WorkQueue interface
	queueAdapter := &workQueueAdapter{queue: queue}
	ctx = WithWorkQueue(ctx, queue)                          // For internal use
	ctx = context.WithValue(ctx, "work_queue", queueAdapter) // For capability access
	ctx = context.WithValue(ctx, "multi_turn_query", query)

	// Resolve capability and call Orchestrate() to inject work items
	if a.capabilityRegistry != nil {
		state := capabilities.RoutingState{
			Domain:         "",
			CapabilityHint: "coding_workflow",
		}
		cap, ok := a.capabilityRegistry.Resolve(query, state)
		if ok {
			if multiTurnCap, isMultiTurn := cap.(capabilities.MultiTurnCapability); isMultiTurn {
				log.Info("Calling capability Orchestrate", "capability", cap.Name())
				handled, err := multiTurnCap.Orchestrate(ctx, state)
				if err != nil {
					return "", fmt.Errorf("orchestration failed: %w", err)
				}
				if !handled {
					log.Warn("Capability declined to orchestrate")
					return "", fmt.Errorf("capability declined orchestration")
				}
			}
		}
	}

	// Track all turn results
	var turnResults []TurnResult
	turnNumber := 0
	maxTurns := 15 // Safety limit

	log.Info("Multi-turn Ask started", "initial_query", query, "max_turns", maxTurns, "queue_depth", queue.Depth())

	for !queue.IsEmpty() && turnNumber < maxTurns {
		item, err := queue.Dequeue()
		if err != nil {
			break
		}

		item.TurnNumber = turnNumber

		// Check conditional execution
		if item.Condition != nil && len(turnResults) > 0 {
			lastResult := turnResults[len(turnResults)-1]
			if !item.Condition(lastResult) {
				log.Info("Skipping turn due to unmet condition", "turn", turnNumber, "actor", item.Actor)
				turnNumber++
				continue
			}
		}

		// Execute single turn with timeout
		turnCtx, cancel := context.WithTimeout(ctx, queue.GetTurnTimeout())

		// Inject previous results into turn context
		turnCtx = a.injectTurnContext(turnCtx, item, turnResults)

		log.Info("Executing turn", "turn_number", turnNumber, "actor", item.Actor, "query_preview", truncateString(item.Query, 100))

		startTime := time.Now()
		result, err := a.processSingleTurn(turnCtx, item)
		result.Duration = time.Since(startTime)
		result.TurnNumber = turnNumber
		result.Actor = item.Actor
		result.Query = item.Query

		cancel()

		if err != nil {
			result.Error = err
			turnResults = append(turnResults, result)

			// Check if error recovery should be attempted
			if a.shouldAttemptRecovery(err, turnNumber, maxTurns) {
				log.Info("Injecting recovery turn", "error", err, "turn", turnNumber)
				recoveryItem := WorkItem{
					Query:    fmt.Sprintf("Recover from error: %v. Original query was: %s", err, item.Query),
					Actor:    "error_recovery",
					Priority: 100, // High priority
					Context:  map[string]any{"failed_turn": item, "error": err.Error()},
				}
				if queueErr := queue.Enqueue(recoveryItem); queueErr != nil {
					log.Warn("Failed to enqueue recovery turn", "error", queueErr)
				}
			} else {
				// Fatal error, terminate multi-turn execution
				log.Warn("Fatal error in multi-turn execution", "turn", turnNumber, "error", err)
				return a.aggregateMultiTurnResults(turnResults), err
			}
		} else {
			turnResults = append(turnResults, result)
			log.Info("Turn completed", "turn_number", turnNumber, "actor", item.Actor, "response_chars", len(result.Response))
		}

		turnNumber++

		// Update context with all results for next turn
		ctx = WithTurnResults(ctx, turnResults)
	}

	if turnNumber >= maxTurns {
		log.Warn("Multi-turn execution hit max turns limit", "turns", turnNumber, "queue_depth", queue.Depth())
	}

	log.Info("Multi-turn Ask completed", "total_turns", len(turnResults), "queue_remaining", queue.Depth())

	// Aggregate results and perform epilogue
	finalResponse := a.aggregateMultiTurnResults(turnResults)

	// Track outcome for MRU/memory (using last turn's data)
	if len(turnResults) > 0 {
		lastResult := turnResults[len(turnResults)-1]
		// Convert []any to []ai.ToolCall for epilogue
		var toolCalls []ai.ToolCall
		if lastResult.ToolCalls != nil {
			for _, tc := range lastResult.ToolCalls {
				if toolCall, ok := tc.(ai.ToolCall); ok {
					toolCalls = append(toolCalls, toolCall)
				}
			}
		}
		a.epilogueAndCleanup(ctx, query, ai.IntentOmni, finalResponse, toolCalls, nil, nil, nil)
	}

	return finalResponse, nil
}

// processSingleTurn executes a single turn in the multi-turn workflow.
func (a *CopilotAgent) processSingleTurn(ctx context.Context, item WorkItem) (TurnResult, error) {
	result := TurnResult{
		Data: make(map[string]any),
	}

	// Check for pending confirmations
	if handled, res, err := a.handlePendingUserConfirmation(ctx, item.Query); handled {
		result.Response = res
		result.Error = err
		return result, err
	}

	// Check for direct tool invocation (slash commands)
	if handled, res, err := a.handleDirectToolInvocation(ctx, item.Query, nil); handled {
		result.Response = res
		result.Error = err
		return result, err
	}

	// Resolve generator
	gen := a.resolveGeneratorWithOverride(ctx, nil)
	if gen == nil {
		err := fmt.Errorf("no valid AI generator configured")
		result.Error = err
		return result, err
	}

	// Classify intent
	intent := a.classifyIntent(ctx, item.Query, gen)

	// For multi-turn, we stay in Omni mode to maintain tool access
	if intent != ai.IntentOmni {
		log.Info("Turn routed to Avatar, converting to Omni for multi-turn consistency", "avatar", intent, "turn", item.TurnNumber)
		intent = ai.IntentOmni
	}

	// Evaluate routing gates
	taskContext, err := a.evaluateRoutingGates(ctx, item.Query, gen)
	if err != nil {
		result.Error = err
		return result, err
	}
	if taskContext == nil {
		taskContext = &TaskContextClassification{}
	}

	// Track episode metadata
	a.trackEpisodeMetadata(ctx, intent)

	// Build system prompt
	fullPrompt := a.buildSystemPrompt(ctx, item.Query, *taskContext)

	// Delegate to reasoning engine
	finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState, err := a.delegateToReasoningEngine(ctx, item.Query, gen, fullPrompt)

	result.Response = finalText
	// Convert []ai.ToolCall to []any for storage
	if toolCalls != nil {
		result.ToolCalls = make([]any, len(toolCalls))
		for i, tc := range toolCalls {
			result.ToolCalls[i] = tc
		}
	}
	result.Error = err

	// Extract any test failure indicators
	result.TestsFailed = detectTestFailures(finalText)

	// Store any data in result context
	if item.Context != nil {
		for k, v := range item.Context {
			result.Data[k] = v
		}
	}
	result.Data["outcome_facts"] = outcomeFacts
	result.Data["outcome_recipes"] = outcomeRecipes
	result.Data["carryover_state"] = carryoverState

	return result, err
}

// injectTurnContext creates enriched context for a turn with previous results.
func (a *CopilotAgent) injectTurnContext(ctx context.Context, item WorkItem, previousResults []TurnResult) context.Context {
	turnCtx := TurnContext{
		TurnNumber:    item.TurnNumber,
		ParentResults: make(map[string]TurnResult),
		SharedState:   make(map[string]any),
	}

	// Add parent result if specified
	if item.ParentItemID != "" {
		for _, result := range previousResults {
			if result.Data != nil {
				if itemID, ok := result.Data["item_id"].(string); ok && itemID == item.ParentItemID {
					turnCtx.ParentResults[item.ParentItemID] = result
					break
				}
			}
		}
	}

	// Add item context to shared state
	if item.Context != nil {
		for k, v := range item.Context {
			turnCtx.SharedState[k] = v
		}
	}

	// Add summary of previous turns for continuity
	if len(previousResults) > 0 {
		turnCtx.SharedState["previous_turns_summary"] = summarizePreviousTurns(previousResults)
	}

	return WithTurnContext(ctx, turnCtx)
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

// ===== Phase 4: Advanced Patterns =====

// MultiTurnOptions configures multi-turn execution behavior.
type MultiTurnOptions struct {
	EnableParallelExecution bool             // Execute independent items in parallel
	EnableCheckpoints       bool             // Periodically save queue state
	CheckpointInterval      int              // Save checkpoint every N turns
	ProgressCallback        ProgressCallback // Called when each turn completes
	MaxParallelTurns        int              // Max concurrent turns (default: 3)
}

// executeParallelTurns executes independent work items in parallel.
// Returns results from all parallel turns.
func (a *CopilotAgent) executeParallelTurns(ctx context.Context, items []WorkItem, queue *WorkQueue, turnResults []TurnResult) []TurnResult {
	if len(items) == 0 {
		return nil
	}

	type turnExecution struct {
		item   WorkItem
		result TurnResult
		err    error
	}

	resultsChan := make(chan turnExecution, len(items))

	// Execute each item in a goroutine
	for _, item := range items {
		go func(workItem WorkItem) {
			// Create isolated context for this turn
			turnCtx, cancel := context.WithTimeout(ctx, queue.GetTurnTimeout())
			defer cancel()

			// Inject turn context
			turnCtx = a.injectTurnContext(turnCtx, workItem, turnResults)

			log.Info("Executing parallel turn", "actor", workItem.Actor, "query_preview", truncateString(workItem.Query, 100))

			startTime := time.Now()
			result, err := a.processSingleTurn(turnCtx, workItem)
			result.Duration = time.Since(startTime)
			result.Actor = workItem.Actor
			result.Query = workItem.Query

			if err != nil {
				result.Error = err
			}

			resultsChan <- turnExecution{
				item:   workItem,
				result: result,
				err:    err,
			}
		}(item)
	}

	// Collect results
	var results []TurnResult
	for i := 0; i < len(items); i++ {
		execution := <-resultsChan
		results = append(results, execution.result)
		log.Info("Parallel turn completed", "actor", execution.item.Actor, "response_chars", len(execution.result.Response))
	}

	return results
}

// multiTurnAskWithOptions executes multi-turn workflow with advanced options (Phase 4).
func (a *CopilotAgent) multiTurnAskWithOptions(ctx context.Context, query string, cfg *ai.ConfigMap, opts MultiTurnOptions) (string, error) {
	ctx = a.applyAskConfig(ctx, cfg)
	a.clearAskProgressMRU()

	// Initialize work queue
	queue := NewWorkQueue()
	queue.SetMaxPerActor("coding_workflow", 8)
	queue.SetMaxPerActor("test_runner", 5)

	// Enqueue initial user query
	initialItem := WorkItem{
		Query: query,
		Actor: "user",
		ID:    "turn-0",
	}
	if err := queue.Enqueue(initialItem); err != nil {
		return "", fmt.Errorf("failed to enqueue initial query: %w", err)
	}

	// Inject queue into context
	queueAdapter := &workQueueAdapter{queue: queue}
	ctx = WithWorkQueue(ctx, queue)
	ctx = context.WithValue(ctx, "work_queue", queueAdapter)
	ctx = context.WithValue(ctx, "multi_turn_query", query)

	// Call capability Orchestrate() to inject work items
	if a.capabilityRegistry != nil {
		state := capabilities.RoutingState{
			Domain:         "",
			CapabilityHint: "coding_workflow",
		}
		cap, ok := a.capabilityRegistry.Resolve(query, state)
		if ok {
			if multiTurnCap, isMultiTurn := cap.(capabilities.MultiTurnCapability); isMultiTurn {
				log.Info("Calling capability Orchestrate", "capability", cap.Name())
				handled, err := multiTurnCap.Orchestrate(ctx, state)
				if err != nil {
					return "", fmt.Errorf("orchestration failed: %w", err)
				}
				if !handled {
					log.Warn("Capability declined to orchestrate")
					return "", fmt.Errorf("capability declined orchestration")
				}
			}
		}
	}

	var turnResults []TurnResult
	turnNumber := 0
	maxTurns := 15
	completedIDs := make(map[string]bool)

	// Set default max parallel turns if not specified
	if opts.MaxParallelTurns == 0 {
		opts.MaxParallelTurns = 3
	}

	log.Info("Multi-turn Ask started (Phase 4)", "initial_query", query, "max_turns", maxTurns,
		"parallel_enabled", opts.EnableParallelExecution, "checkpoints_enabled", opts.EnableCheckpoints)

	for !queue.IsEmpty() && turnNumber < maxTurns {
		// Phase 4: Checkpoint support
		if opts.EnableCheckpoints && opts.CheckpointInterval > 0 && turnNumber > 0 && turnNumber%opts.CheckpointInterval == 0 {
			checkpoint := queue.Checkpoint()
			checkpoint.TurnResults = turnResults
			log.Info("Queue checkpoint created", "checkpoint_id", checkpoint.CheckpointID, "turn", turnNumber)
			// In production, persist checkpoint to storage here
		}

		// Phase 4: Parallel execution
		if opts.EnableParallelExecution {
			readyItems := queue.GetReadyItems(completedIDs)

			// Limit parallel execution
			if len(readyItems) > opts.MaxParallelTurns {
				readyItems = readyItems[:opts.MaxParallelTurns]
			}

			if len(readyItems) > 1 {
				log.Info("Executing parallel turns", "count", len(readyItems))

				// Remove items from queue before executing
				var ids []string
				for _, item := range readyItems {
					ids = append(ids, item.ID)
					item.TurnNumber = turnNumber
					turnNumber++
				}
				queue.RemoveItems(ids)

				// Execute in parallel
				parallelResults := a.executeParallelTurns(ctx, readyItems, queue, turnResults)

				// Mark as completed and add to results
				for i, result := range parallelResults {
					completedIDs[readyItems[i].ID] = true
					turnResults = append(turnResults, result)

					// Phase 4: Progress callback
					if opts.ProgressCallback != nil {
						opts.ProgressCallback(result)
					}
				}

				ctx = WithTurnResults(ctx, turnResults)
				continue
			}
		}

		// Standard sequential execution (Phase 1-3)
		item, err := queue.Dequeue()
		if err != nil {
			break
		}

		item.TurnNumber = turnNumber

		// Check conditional execution
		if item.Condition != nil && len(turnResults) > 0 {
			lastResult := turnResults[len(turnResults)-1]
			if !item.Condition(lastResult) {
				log.Info("Skipping turn due to unmet condition", "turn", turnNumber, "actor", item.Actor)
				turnNumber++
				continue
			}
		}

		turnCtx, cancel := context.WithTimeout(ctx, queue.GetTurnTimeout())
		turnCtx = a.injectTurnContext(turnCtx, item, turnResults)

		log.Info("Executing turn", "turn_number", turnNumber, "actor", item.Actor, "query_preview", truncateString(item.Query, 100))

		startTime := time.Now()
		result, err := a.processSingleTurn(turnCtx, item)
		result.Duration = time.Since(startTime)
		result.TurnNumber = turnNumber
		result.Actor = item.Actor
		result.Query = item.Query

		cancel()

		if err != nil {
			result.Error = err
		}

		turnResults = append(turnResults, result)
		completedIDs[item.ID] = true

		// Phase 4: Progress callback
		if opts.ProgressCallback != nil {
			opts.ProgressCallback(result)
		}

		log.Info("Turn completed", "turn_number", turnNumber, "actor", item.Actor, "response_chars", len(result.Response))

		turnNumber++
		ctx = WithTurnResults(ctx, turnResults)
	}

	if turnNumber >= maxTurns {
		log.Warn("Multi-turn execution hit max turns limit", "turns", turnNumber, "queue_depth", queue.Depth())
	}

	log.Info("Multi-turn Ask completed (Phase 4)", "total_turns", len(turnResults), "queue_remaining", queue.Depth())

	return a.aggregateMultiTurnResults(turnResults), nil
}

// resumeFromCheckpoint resumes multi-turn execution from a saved checkpoint.
func (a *CopilotAgent) resumeFromCheckpoint(ctx context.Context, checkpoint QueueCheckpoint, cfg *ai.ConfigMap, opts MultiTurnOptions) (string, error) {
	log.Info("Resuming from checkpoint", "checkpoint_id", checkpoint.CheckpointID, "items", len(checkpoint.Items))

	queue := NewWorkQueue()
	if err := queue.RestoreFromCheckpoint(checkpoint); err != nil {
		return "", fmt.Errorf("failed to restore checkpoint: %w", err)
	}

	// Continue execution with restored queue
	// This is a simplified version - full implementation would restore all context
	ctx = WithWorkQueue(ctx, queue)
	ctx = WithTurnResults(ctx, checkpoint.TurnResults)

	return a.multiTurnAskWithOptions(ctx, "", cfg, opts)
}

// ===== Phase 5: Production Hardening =====

// ProductionOptions extends MultiTurnOptions with production features.
type ProductionOptions struct {
	MultiTurnOptions                   // Embed Phase 4 options
	RetryStrategy    RetryStrategy     // Retry configuration
	Persistence      *QueuePersistence // Optional persistence layer
	Metrics          *QueueMetrics     // Metrics tracker
	CircuitBreaker   *CircuitBreaker   // Circuit breaker for failing capabilities
	InterruptionCtx  context.Context   // Context for graceful cancellation
}

// multiTurnAskProduction executes multi-turn workflow with production hardening (Phase 5).
func (a *CopilotAgent) multiTurnAskProduction(ctx context.Context, query string, cfg *ai.ConfigMap, opts ProductionOptions) (string, error) {
	ctx = a.applyAskConfig(ctx, cfg)
	a.clearAskProgressMRU()

	// Initialize metrics if not provided
	if opts.Metrics == nil {
		opts.Metrics = NewQueueMetrics()
	}

	// Initialize work queue
	queue := NewWorkQueue()
	queue.SetMaxPerActor("coding_workflow", 8)
	queue.SetMaxPerActor("test_runner", 5)

	// Enqueue initial user query
	initialItem := WorkItem{
		Query: query,
		Actor: "user",
		ID:    "turn-0",
	}
	if err := queue.Enqueue(initialItem); err != nil {
		return "", fmt.Errorf("failed to enqueue initial query: %w", err)
	}

	// Inject queue into context
	queueAdapter := &workQueueAdapter{queue: queue}
	ctx = WithWorkQueue(ctx, queue)
	ctx = context.WithValue(ctx, "work_queue", queueAdapter)
	ctx = context.WithValue(ctx, "multi_turn_query", query)

	// Use interruption context if provided
	if opts.InterruptionCtx != nil {
		ctx = opts.InterruptionCtx
	}

	// Call capability Orchestrate() with circuit breaker protection
	if a.capabilityRegistry != nil {
		state := capabilities.RoutingState{
			Domain:         "",
			CapabilityHint: "coding_workflow",
		}

		orchestrateWithRetry := func() error {
			cap, ok := a.capabilityRegistry.Resolve(query, state)
			if !ok {
				return fmt.Errorf("capability not found")
			}

			multiTurnCap, isMultiTurn := cap.(capabilities.MultiTurnCapability)
			if !isMultiTurn {
				return fmt.Errorf("capability does not support multi-turn")
			}

			log.Info("Calling capability Orchestrate (Production)", "capability", cap.Name())

			// Apply circuit breaker if configured
			if opts.CircuitBreaker != nil {
				return opts.CircuitBreaker.Call(func() error {
					handled, err := multiTurnCap.Orchestrate(ctx, state)
					if err != nil {
						return err
					}
					if !handled {
						return fmt.Errorf("capability declined orchestration")
					}
					return nil
				})
			}

			// No circuit breaker - call directly
			handled, err := multiTurnCap.Orchestrate(ctx, state)
			if err != nil {
				return err
			}
			if !handled {
				return fmt.Errorf("capability declined orchestration")
			}
			return nil
		}

		// Retry orchestration with exponential backoff
		var lastErr error
		for attempt := 0; attempt < opts.RetryStrategy.MaxAttempts; attempt++ {
			if attempt > 0 {
				delay := opts.RetryStrategy.CalculateDelay(attempt)
				log.Info("Retrying orchestration", "attempt", attempt, "delay", delay)
				time.Sleep(delay)
			}

			if err := orchestrateWithRetry(); err != nil {
				lastErr = err
				log.Warn("Orchestration attempt failed", "attempt", attempt, "error", err)
				continue
			}

			lastErr = nil
			break
		}

		if lastErr != nil {
			return "", fmt.Errorf("orchestration failed after retries: %w", lastErr)
		}
	}

	var turnResults []TurnResult
	turnNumber := 0
	maxTurns := 15
	completedIDs := make(map[string]bool)

	if opts.MaxParallelTurns == 0 {
		opts.MaxParallelTurns = 3
	}

	log.Info("Multi-turn Ask started (Production)", "initial_query", query, "max_turns", maxTurns,
		"persistence", opts.Persistence != nil, "circuit_breaker", opts.CircuitBreaker != nil)

	for !queue.IsEmpty() && turnNumber < maxTurns {
		// Check for interruption
		select {
		case <-ctx.Done():
			log.Warn("Multi-turn execution interrupted", "turn", turnNumber, "reason", ctx.Err())

			// Save checkpoint before exiting
			if opts.Persistence != nil {
				checkpoint := queue.Checkpoint()
				checkpoint.TurnResults = turnResults
				if err := opts.Persistence.SaveCheckpoint(ctx, checkpoint); err != nil {
					log.Warn("Failed to save interruption checkpoint", "error", err)
				} else {
					log.Info("Interruption checkpoint saved", "checkpoint_id", checkpoint.CheckpointID)
				}
			}

			return a.aggregateMultiTurnResults(turnResults), ctx.Err()
		default:
			// Continue execution
		}

		// Phase 5: Checkpoint support with persistence
		if opts.EnableCheckpoints && opts.CheckpointInterval > 0 && turnNumber > 0 && turnNumber%opts.CheckpointInterval == 0 {
			checkpoint := queue.Checkpoint()
			checkpoint.TurnResults = turnResults
			opts.Metrics.RecordCheckpoint()

			if opts.Persistence != nil {
				if err := opts.Persistence.SaveCheckpoint(ctx, checkpoint); err != nil {
					log.Warn("Failed to save checkpoint", "error", err)
				} else {
					log.Info("Checkpoint persisted", "checkpoint_id", checkpoint.CheckpointID, "turn", turnNumber)
				}
			}
		}

		// Phase 4: Parallel execution
		if opts.EnableParallelExecution {
			readyItems := queue.GetReadyItems(completedIDs)

			if len(readyItems) > opts.MaxParallelTurns {
				readyItems = readyItems[:opts.MaxParallelTurns]
			}

			if len(readyItems) > 1 {
				log.Info("Executing parallel turns (Production)", "count", len(readyItems))

				var ids []string
				for _, item := range readyItems {
					ids = append(ids, item.ID)
					item.TurnNumber = turnNumber
					turnNumber++
				}
				queue.RemoveItems(ids)

				parallelResults := a.executeParallelTurns(ctx, readyItems, queue, turnResults)

				for i, result := range parallelResults {
					completedIDs[readyItems[i].ID] = true
					turnResults = append(turnResults, result)

					// Record metrics
					opts.Metrics.RecordTurn(result)

					// Persist metric if persistence enabled
					if opts.Persistence != nil {
						if err := opts.Persistence.SaveMetric(ctx, "current-session", result); err != nil {
							log.Warn("Failed to persist metric", "error", err)
						}
					}

					// Progress callback
					if opts.ProgressCallback != nil {
						opts.ProgressCallback(result)
					}
				}

				opts.Metrics.ParallelTurns += len(readyItems)
				ctx = WithTurnResults(ctx, turnResults)
				continue
			}
		}

		// Standard sequential execution
		item, err := queue.Dequeue()
		if err != nil {
			break
		}

		item.TurnNumber = turnNumber

		// Check conditional execution
		if item.Condition != nil && len(turnResults) > 0 {
			lastResult := turnResults[len(turnResults)-1]
			if !item.Condition(lastResult) {
				log.Info("Skipping turn due to unmet condition", "turn", turnNumber, "actor", item.Actor)
				turnNumber++
				continue
			}
		}

		// Execute turn with retry logic
		var result TurnResult
		var turnErr error

		for attempt := 0; attempt < opts.RetryStrategy.MaxAttempts; attempt++ {
			if attempt > 0 {
				delay := opts.RetryStrategy.CalculateDelay(attempt)
				log.Info("Retrying turn", "turn", turnNumber, "attempt", attempt, "delay", delay)
				time.Sleep(delay)
			}

			turnCtx, cancel := context.WithTimeout(ctx, queue.GetTurnTimeout())
			turnCtx = a.injectTurnContext(turnCtx, item, turnResults)

			log.Info("Executing turn (Production)", "turn_number", turnNumber, "actor", item.Actor, "attempt", attempt)

			startTime := time.Now()
			result, turnErr = a.processSingleTurn(turnCtx, item)
			result.Duration = time.Since(startTime)
			result.TurnNumber = turnNumber
			result.Actor = item.Actor
			result.Query = item.Query

			cancel()

			if turnErr == nil {
				break // Success
			}

			log.Warn("Turn execution failed", "turn", turnNumber, "attempt", attempt, "error", turnErr)
		}

		if turnErr != nil {
			result.Error = turnErr
		}

		turnResults = append(turnResults, result)
		completedIDs[item.ID] = true

		// Record metrics
		opts.Metrics.RecordTurn(result)

		// Persist metric
		if opts.Persistence != nil {
			if err := opts.Persistence.SaveMetric(ctx, "current-session", result); err != nil {
				log.Warn("Failed to persist metric", "error", err)
			}
		}

		// Progress callback
		if opts.ProgressCallback != nil {
			opts.ProgressCallback(result)
		}

		log.Info("Turn completed (Production)", "turn_number", turnNumber, "actor", item.Actor, "response_chars", len(result.Response))

		turnNumber++
		ctx = WithTurnResults(ctx, turnResults)
	}

	if turnNumber >= maxTurns {
		log.Warn("Multi-turn execution hit max turns limit", "turns", turnNumber, "queue_depth", queue.Depth())
	}

	// Log final metrics
	metricsMap := opts.Metrics.ToMap()
	log.Info("Multi-turn Ask completed (Production)", "total_turns", len(turnResults), "queue_remaining", queue.Depth(),
		"successful", metricsMap["successful_turns"], "failed", metricsMap["failed_turns"],
		"avg_duration_ms", metricsMap["avg_duration_ms"])

	return a.aggregateMultiTurnResults(turnResults), nil
}
