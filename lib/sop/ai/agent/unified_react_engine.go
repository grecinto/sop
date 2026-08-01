package agent

import (
	"context"
	"fmt"
	log "log/slog"

	"github.com/sharedcode/sop/ai"
)

// UnifiedReActEngine orchestrates the common ReAct loop pattern across all providers.
// It handles:
//   - Iteration control and loop lifecycle
//   - Tool execution orchestration
//   - Progressive retry logic (Tier 1/2A/2B/3) via RetryCoordinator
//   - Failure tracking and escalation state management
//   - Response building with EscalationState
//
// Provider-specific behavior (LLM invocation, continuation handling, etc) is delegated
// to the ReActLoopAdapter implementation.
type UnifiedReActEngine struct {
	Adapter          ReActLoopAdapter
	RetryCoordinator *RetryCoordinator
	EnableObfuscation bool
	MaxTokens        int
}

// NewUnifiedReActEngine creates a new unified engine with the given adapter.
func NewUnifiedReActEngine(adapter ReActLoopAdapter) *UnifiedReActEngine {
	return &UnifiedReActEngine{
		Adapter:          adapter,
		RetryCoordinator: NewRetryCoordinator(),
		MaxTokens:        4096,
	}
}

// Run executes the unified ReAct loop.
func (e *UnifiedReActEngine) Run(ctx context.Context, req ai.ReasoningRequest) (ai.ReasoningResponse, error) {
	if req.Generator == nil {
		return ai.ReasoningResponse{FinalText: req.ContextText}, nil
	}

	var tools []ai.ToolDefinition
	var err error
	if req.Executor != nil {
		tools, err = req.Executor.ListTools(ctx)
		if err != nil {
			return ai.ReasoningResponse{}, fmt.Errorf("failed to list tools: %w", err)
		}
	}

	log.Info("Unified ReAct Engine Start",
		"adapter", e.Adapter.Name(),
		"generator", req.Generator.Name(),
		"tool_count", len(tools),
		"max_iterations", e.Adapter.MaxIterations(),
	)

	// Restore continuations from carryover state (provider-specific)
	restoredContinuations, _ := e.Adapter.RestoreContinuations(req.CarryoverState)

	// Initialize loop state
	state := ReActLoopState{
		Iteration:         0,
		Request:           req,
		ToolResults:       make([]ai.ReActToolResult, 0),
		ExecutedToolCalls: make([]ai.ToolCall, 0),
		Continuations:     restoredContinuations,
		FailureCounts:     make(map[string]int),
		RepairDirective:   "",
		EscalationState:   nil,
	}

	var pendingRepair *RepairPlan

	// Main ReAct iteration loop
	for state.Iteration = 1; state.Iteration <= e.Adapter.MaxIterations(); state.Iteration++ {
		// Check for cancellation
		select {
		case <-ctx.Done():
			log.Warn("Unified ReAct Engine: Request canceled",
				"iteration", state.Iteration,
				"adapter", e.Adapter.Name(),
				"error", ctx.Err())
			return ai.ReasoningResponse{}, fmt.Errorf("request canceled during iteration %d: %w", state.Iteration, ctx.Err())
		default:
		}

		log.Info("Unified ReAct Iteration",
			"iteration", state.Iteration,
			"adapter", e.Adapter.Name(),
			"tool_results_count", len(state.ToolResults),
			"pending_repair", pendingRepair != nil)

		// Check provider-specific early termination
		if shouldStop, reason := e.Adapter.ShouldTerminateEarly(state); shouldStop {
			log.Info("Unified ReAct Engine Early Termination",
				"iteration", state.Iteration,
				"adapter", e.Adapter.Name(),
				"reason", reason)
			return e.Adapter.BuildFinalResponse("", state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
		}

		// Prepare LLM request (provider-specific)
		prompt, opts, err := e.Adapter.PrepareLLMRequest(ctx, state)
		if err != nil {
			return ai.ReasoningResponse{}, fmt.Errorf("failed to prepare LLM request: %w", err)
		}

		// Invoke LLM (provider-specific)
		output, err := e.Adapter.InvokeLLM(ctx, prompt, opts, req)
		if err != nil {
			// TODO: Handle recoverable generation errors (malformed tool calls)
			return ai.ReasoningResponse{}, fmt.Errorf("LLM invocation failed: %w", err)
		}

		// Extract tool calls (provider-specific)
		toolCalls, finalText, err := e.Adapter.ExtractToolCalls(output)
		if err != nil {
			return ai.ReasoningResponse{}, fmt.Errorf("failed to extract tool calls: %w", err)
		}

		// No tool calls → final response
		if req.Executor == nil || len(toolCalls) == 0 {
			log.Info("Unified ReAct Engine Final Response (no tool calls)",
				"iteration", state.Iteration,
				"adapter", e.Adapter.Name(),
				"text_chars", len(finalText))
			return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
		}

		// Execute first tool call
		toolCall := toolCalls[0]

		// Validate tool call against pending repair
		if pendingRepair != nil && !e.RetryCoordinator.AllowsTool(*pendingRepair, toolCall.Name) {
			log.Warn("Unified ReAct Engine Deferred Non-Repair Tool Call",
				"expected_tool", pendingRepair.ToolName,
				"received_tool", toolCall.Name,
				"adapter", e.Adapter.Name())

			// Add repair reminder to tool results
			reminderResult := ai.ReActToolResult{
				Name:   pendingRepair.ToolName,
				Result: e.RetryCoordinator.FormatRepairDirective(*pendingRepair, toolCall.Name),
			}
			state.ToolResults = append(state.ToolResults, reminderResult)
			state.RepairDirective = e.RetryCoordinator.FormatRepairDirective(*pendingRepair, toolCall.Name)
			continue
		}

		// Execute tool
		log.Info("Unified ReAct Engine Tool Call",
			"iteration", state.Iteration,
			"adapter", e.Adapter.Name(),
			"tool", toolCall.Name)

		execCtx := context.WithValue(ctx, ai.CtxKeyNativeToolHints, true)
		rawResult, err := req.Executor.Execute(execCtx, toolCall.Name, toolCall.Args)

		if err != nil {
			// Check if error is recoverable
			if !e.RetryCoordinator.IsRecoverableToolError(err) {
				log.Error("Unified ReAct Engine Unrecoverable Tool Failure",
					"tool", toolCall.Name,
					"adapter", e.Adapter.Name(),
					"error", err)
				return ai.ReasoningResponse{}, fmt.Errorf("tool execution failed: %w", err)
			}

			// Recoverable error → progressive retry
			currentFailureCount := state.FailureCounts[toolCall.Name]
			lastHint := e.getLatestToolHint(state.ToolResults)
			errorMessage := err.Error()

			// Determine retry tier (Tier 2B)
			tier := e.RetryCoordinator.DetermineRetryTier(errorMessage, currentFailureCount, lastHint)

			// Classify repair strategy (Tier 2A)
			repairPlan := e.RetryCoordinator.ClassifyRepair(toolCall.Name, err, lastHint)
			repairPlan.Tier = tier

			log.Info("Unified ReAct Engine Progressive Retry",
				"tool", toolCall.Name,
				"adapter", e.Adapter.Name(),
				"tier", int(tier),
				"failure_count", currentFailureCount,
				"strategy", repairPlan.Strategy)

			// Tier 3 (Clarify) → escalate to phase succession
			if tier == RetryTierClarify {
				log.Warn("Unified ReAct Engine Tier 3 Escalation",
					"tool", toolCall.Name,
					"adapter", e.Adapter.Name(),
					"failure_count", currentFailureCount)

				// Capture escalation state for phase succession
				state.EscalationState = map[string]any{
					"tier":             3,
					"failed_tool":      toolCall.Name,
					"failure_count":    currentFailureCount,
					"strategy":         string(repairPlan.Strategy),
					"suggested_action": "backward_phase_transition",
					"error_type":       errorMessage,
				}

				// Add clarification-required result
				clarificationResult := ai.ReActToolResult{
					Name:   toolCall.Name,
					Result: fmt.Sprintf("ERROR (Tier 3 - Clarification Required): %s", err.Error()),
				}
				state.ToolResults = append(state.ToolResults, clarificationResult)
				pendingRepair = nil
				continue
			}

			// Tier 1/2 → continue retry
			state.FailureCounts[toolCall.Name] = currentFailureCount + 1
			pendingRepair = &repairPlan
			state.RepairDirective = e.RetryCoordinator.FormatRepairDirective(repairPlan, toolCall.Name)

			// Add repair result
			repairResult := ai.ReActToolResult{
				Name:   toolCall.Name,
				Result: fmt.Sprintf("ERROR (Tier %d - %s): %s\n\n%s", int(tier), repairPlan.Strategy, err.Error(), state.RepairDirective),
			}
			state.ToolResults = append(state.ToolResults, repairResult)

			log.Warn("Unified ReAct Engine Recoverable Tool Failure",
				"tool", toolCall.Name,
				"adapter", e.Adapter.Name(),
				"tier", int(tier),
				"error", err)
			continue
		}

		// Tool execution succeeded
		result, hint, resultFile, resultSize := unwrapToolResultEnvelopeFull(rawResult)

		log.Info("Unified ReAct Engine Tool Success",
			"iteration", state.Iteration,
			"adapter", e.Adapter.Name(),
			"tool", toolCall.Name,
			"result_chars", len(result),
			"file_backed", resultFile != "")

		// Reset repair state on success
		if pendingRepair != nil && pendingRepair.ToolName == toolCall.Name {
			state.FailureCounts[toolCall.Name] = 0
			pendingRepair = nil
			state.RepairDirective = ""
		}

		// Record successful tool call
		state.ExecutedToolCalls = append(state.ExecutedToolCalls, toolCall)
		successResult := ai.ReActToolResult{
			Name:       toolCall.Name,
			Result:     result,
			ResultFile: resultFile,
			ResultSize: resultSize,
			Hint:       hint,
		}
		state.ToolResults = append(state.ToolResults, successResult)

		// Format continuation for next iteration (provider-specific)
		state.Continuations = e.Adapter.FormatContinuation(toolCall, successResult, state.Continuations)
	}

	// Reached max iterations → final clarification request
	log.Warn("Unified ReAct Engine Reached Max Iterations",
		"adapter", e.Adapter.Name(),
		"max_iterations", e.Adapter.MaxIterations())

	// Build final response asking for clarification
	finalText := fmt.Sprintf(
		"We have reached the maximum number of iterations (%d). "+
			"Please provide additional guidance to help complete the task.",
		e.Adapter.MaxIterations())

	return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
}

// getLatestToolHint returns the hint from the most recent tool result, if any.
func (e *UnifiedReActEngine) getLatestToolHint(toolResults []ai.ReActToolResult) *ai.ToolProgressHint {
	if len(toolResults) == 0 {
		return nil
	}
	return toolResults[len(toolResults)-1].Hint
}
