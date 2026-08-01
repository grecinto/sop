package agent

import (
	"context"
	"fmt"
	log "log/slog"
	"strings"

	"github.com/sharedcode/sop/ai"
)

// UnifiedReActEngine orchestrates the common ReAct loop pattern across all providers.
// It handles:
//   - Iteration control and loop lifecycle
//   - Tool execution orchestration
//   - Progressive retry policy evaluation via RetryPolicySurface
//   - Failure tracking and escalation state management
//   - Response building with EscalationState
//
// Provider-specific behavior (LLM invocation, continuation handling, etc) is delegated
// to the ReActLoopAdapter implementation.
type UnifiedReActEngine struct {
	Adapter ReActLoopAdapter
	// Candidate for interface capture toward LAO and/or heuristic policy enforcer:
	// the shared-compatible path should depend on policy contracts, not coordinator internals,
	// so this path can stay a fallback runtime instead of becoming a hidden control center.
	RetryPolicy       RetryPolicySurface
	EnableObfuscation bool
	MaxTokens         int
}

// NewUnifiedReActEngine creates a new unified engine with the given adapter.
func NewUnifiedReActEngine(adapter ReActLoopAdapter) *UnifiedReActEngine {
	return &UnifiedReActEngine{
		Adapter:     adapter,
		RetryPolicy: NewDefaultRetryPolicySurface(),
		MaxTokens:   4096,
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
	emitVerboseProgress(ctx, "Planning request with %s multi-step loop.", e.Adapter.Name())

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
	strategy := resolveReActTurnStrategy(req)

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

		emitVerboseProgress(ctx, "Reasoning iteration %d of %d.", state.Iteration, e.Adapter.MaxIterations())

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

		turn := ai.ReActTurn{
			Iteration:       state.Iteration,
			UserQuery:       req.UserQuery,
			RepairDirective: state.RepairDirective,
			LoopState: ai.ReActLoopState{
				Iteration:             state.Iteration,
				AllowedIterations:     e.Adapter.MaxIterations(),
				MaxIterations:         e.Adapter.MaxIterations(),
				RemainingToolCalls:    max(0, e.Adapter.MaxIterations()-state.Iteration+1),
				ToolResults:           append([]ai.ReActToolResult(nil), state.ToolResults...),
				ToolCallContinuations: append([]ai.ToolCallContinuation(nil), state.Continuations...),
			},
			Options:     opts,
			ToolResults: append([]ai.ReActToolResult(nil), state.ToolResults...),
			FinalTurn:   state.Iteration == e.Adapter.MaxIterations(),
		}
		if shouldBypassReActPrompt(strategy, turn) {
			prompt = ""
		}
		prompt, opts = prepareReActTurn(ctx, strategy, turn, prompt)

		emitVerboseProgress(ctx, "Waiting for model response.")

		// Invoke LLM (provider-specific)
		output, err := e.Adapter.InvokeLLM(ctx, prompt, opts, req)
		if err != nil {
			// Handle recoverable generation errors (malformed tool calls)
			if isRecoverableGenerationError(err) {
				emitVerboseProgress(ctx, "Model emitted a malformed native tool call; retrying.")
				malformedResult := ai.ReActToolResult{
					Name:   "native_tool_call",
					Result: formatRecoverableGenerationError(err),
				}
				state.ToolResults = append(state.ToolResults, malformedResult)
				state.RepairDirective = "Repair directive: The last model output produced an invalid native tool call. Your next step should be to call exactly one valid tool using arguments that conform to the provided tool schema and repair guidance below. Do not summarize the error as a final answer unless correction is impossible."
				log.Warn("Unified ReAct Engine Recoverable Generation Failure",
					"adapter", e.Adapter.Name(),
					"error", err)
				continue
			}
			return ai.ReasoningResponse{}, fmt.Errorf("LLM invocation failed: %w", err)
		}
		// Extract tool calls (provider-specific)
		toolCalls, finalText, err := e.Adapter.ExtractToolCalls(output)
		if err != nil {
			// Check if this is a recoverable generation error (e.g., truncated JSON)
			if isRecoverableGenerationError(err) {
				// Handle as a malformed generation error - add result and retry
				log.Warn("Unified ReAct Engine Recoverable Generation Error",
					"iteration", state.Iteration,
					"adapter", e.Adapter.Name(),
					"error", err.Error())

				emitVerboseProgress(ctx, "Recoverable generation error; retrying.")

				// Format the error for the model
				errResult := ai.ReActToolResult{
					Name:   "",
					Result: formatRecoverableGenerationError(err),
				}
				state.ToolResults = append(state.ToolResults, errResult)
				state.RepairDirective = "Repair directive: The last model output produced an invalid native tool call."
				pendingRepair = nil // Clear any pending repair
				continue
			}
			return ai.ReasoningResponse{}, fmt.Errorf("failed to extract tool calls: %w", err)
		}

		// No tool calls → final response
		if req.Executor == nil || len(toolCalls) == 0 {
			emitVerboseProgress(ctx, "No further tools required; preparing final answer.")

			log.Info("Unified ReAct Engine Final Response (no tool calls)",
				"iteration", state.Iteration,
				"adapter", e.Adapter.Name(),
				"text_chars", len(finalText))

			// Check if we should preserve structured result
			if e.shouldPreserveStructuredResult(ctx, state.ToolResults) {
				lastResult := state.ToolResults[len(state.ToolResults)-1]
				return e.Adapter.BuildFinalResponse(
					preserveStructuredToolResult(lastResult.Result, e.EnableObfuscation),
					state.ExecutedToolCalls,
					state.ToolResults,
					state.Continuations,
					state.EscalationState,
				), nil
			}

			return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
		}

		// Sanitize tool call arguments (remove markdown artifacts, deobfuscate)
		for i := range toolCalls {
			sanitizeToolCallArgs(toolCalls[i].Args, e.EnableObfuscation)
		}

		// Execute first tool call
		toolCall := toolCalls[0]

		// Validate tool call against pending repair
		// Candidate for interface capture toward LAO: repair gating is a valid hard rail,
		// but the user-facing continuation shape around that gate could move behind a
		// dedicated policy-enforcer interface if shared and owned loops diverge further.
		if pendingRepair != nil && !e.RetryPolicy.AllowsTool(*pendingRepair, toolCall.Name) {
			emitVerboseProgress(ctx, "Tool `%s` must be corrected before other actions.", pendingRepair.ToolName)

			log.Warn("Unified ReAct Engine Deferred Non-Repair Tool Call",
				"expected_tool", pendingRepair.ToolName,
				"received_tool", toolCall.Name,
				"adapter", e.Adapter.Name())

			reminderGuidance := ai.BuildRetryGuidance(pendingRepair.ToolName, *pendingRepair, nil, state.FailureCounts[pendingRepair.ToolName], e.Adapter.MaxIterations(), e.Adapter.MaxIterations(), false)
			// Add repair reminder to tool results
			reminderDirective := e.RetryPolicy.FormatRepairDirective(*pendingRepair, toolCall.Name)
			reminderResult := ai.ReActToolResult{
				Name:          pendingRepair.ToolName,
				Result:        reminderDirective,
				RetryGuidance: reminderGuidance,
			}
			state.ToolResults = append(state.ToolResults, reminderResult)
			state.RepairDirective = reminderDirective
			continue
		}

		// Execute tool
		emitVerboseProgress(ctx, "Calling tool `%s`.", toolCall.Name)

		log.Info("Unified ReAct Engine Tool Call",
			"iteration", state.Iteration,
			"adapter", e.Adapter.Name(),
			"tool", toolCall.Name)

		// Emit tool call event
		emitReasoningEvent(req, ai.ReasoningEventToolCall, ai.BuildToolCallEvent(toolCall.Name, cloneToolEventMap(toolCall.Args), state.Iteration))

		execCtx := context.WithValue(ctx, ai.CtxKeyNativeToolHints, true)
		priorRepair := pendingRepair
		rawResult, err := req.Executor.Execute(execCtx, toolCall.Name, toolCall.Args)

		if err != nil {
			// Emit tool error event
			emitReasoningEvent(req, ai.ReasoningEventToolError, ai.BuildToolErrorEvent(toolCall.Name, cloneToolEventMap(toolCall.Args), err, state.Iteration))

			// Check if error is recoverable
			if !e.RetryPolicy.IsRecoverable(err) {
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

			// Check if we recently did research for this tool
			recentlyResearched := e.wasResearchRecentlyDone(state.ToolResults, toolCall.Name)

			decision := e.RetryPolicy.EvaluateRetryDecision(RetryDecisionInput{
				ToolName:           toolCall.Name,
				Err:                err,
				FailureCount:       currentFailureCount,
				Hint:               lastHint,
				RecentlyResearched: recentlyResearched,
			})
			tier := decision.Tier
			repairPlan := decision.Plan
			retryGuidance := ai.BuildRetryGuidance(toolCall.Name, repairPlan, decision.Recommendation, currentFailureCount+1, e.Adapter.MaxIterations(), e.Adapter.MaxIterations(), decision.RequiresClarify || tier == RetryTierClarify)

			// Convert RepairPlan to the compatibility-path repair state for error formatting.
			compatiblePendingRepair := e.convertRepairPlanToCompatible(repairPlan)

			log.Info("Unified ReAct Engine Progressive Retry",
				"tool", toolCall.Name,
				"adapter", e.Adapter.Name(),
				"tier", int(tier),
				"failure_count", currentFailureCount,
				"strategy", repairPlan.Strategy)

			// Tier 3 (Clarify) → escalate to phase succession
			if decision.RequiresClarify || tier == RetryTierClarify {
				emitVerboseProgress(ctx, "Tier 3 (Clarify): Escalating to user after %d failures.", currentFailureCount)

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

				// Add clarification-required result with proper formatting
				clarificationResult := ai.ReActToolResult{
					Name:          toolCall.Name,
					Result:        formatClarificationRequiredToolError(compatiblePendingRepair, toolCall.Args, err),
					Args:          cloneToolEventMap(toolCall.Args),
					RetryGuidance: retryGuidance,
				}
				state.ToolResults = append(state.ToolResults, clarificationResult)
				state.RepairDirective = "Clarification directive: The last tool failure still remains unresolved after the first repair attempt. Do not call another tool yet. Ask the user one short, concrete clarification question that names the blocker and wait for the answer."
				pendingRepair = nil
				continue
			}

			// Tier 1/2 → continue retry
			state.FailureCounts[toolCall.Name] = currentFailureCount + 1
			pendingRepair = &repairPlan

			// Emit tier-specific progress message
			switch tier {
			case RetryTierQuickFix:
				emitVerboseProgress(ctx, "Tier 1 (Quick Fix): Tool `%s` needs corrected arguments; retrying.", toolCall.Name)
			case RetryTierResearch:
				emitVerboseProgress(ctx, "Tier 2 (Research): Tool `%s` requires research before retry.", toolCall.Name)
			default:
				emitVerboseProgress(ctx, "Tool `%s` needs corrected arguments; retrying.", toolCall.Name)
			}

			// Add repair result with proper formatting
			repairResult := ai.ReActToolResult{
				Name:          toolCall.Name,
				Result:        formatRecoverableToolError(compatiblePendingRepair, toolCall.Args, err),
				Args:          cloneToolEventMap(toolCall.Args),
				RetryGuidance: retryGuidance,
			}
			state.ToolResults = append(state.ToolResults, repairResult)
			state.RepairDirective = e.buildRepairDirective(state.ToolResults)

			log.Warn("Unified ReAct Engine Recoverable Tool Failure",
				"tool", toolCall.Name,
				"adapter", e.Adapter.Name(),
				"tier", int(tier),
				"error", err)
			continue
		}

		// Tool execution succeeded
		result, hint, resultFile, resultSize := unwrapToolResultEnvelopeFull(rawResult)

		emitVerboseProgress(ctx, "Tool `%s` completed.", toolCall.Name)

		log.Info("Unified ReAct Engine Tool Success",
			"iteration", state.Iteration,
			"adapter", e.Adapter.Name(),
			"tool", toolCall.Name,
			"result_chars", len(result),
			"file_backed", resultFile != "")

		// Emit tool result event
		emitReasoningEvent(req, ai.ReasoningEventToolResult, ai.BuildToolResultEvent(toolCall.Name, cloneToolEventMap(toolCall.Args), result, hint, state.Iteration, resultFile, resultSize))

		// Reset repair state on success
		if priorRepair != nil {
			// If we just completed the research tool for a research_first strategy, clear the repair
			// so the next attempt at the original tool is treated as a fresh retry
			if priorRepair.Strategy == RepairStrategyResearchFirst && toolCall.Name != priorRepair.ToolName {
				// Successfully completed research, clear the repair
				pendingRepair = nil
				state.RepairDirective = ""
			} else if toolCall.Name == priorRepair.ToolName {
				// Successfully completed the tool that was failing, reset failure count and clear repair
				state.FailureCounts[toolCall.Name] = 0
				pendingRepair = nil
				state.RepairDirective = ""
			}
		}

		// Record successful tool call
		state.ExecutedToolCalls = append(state.ExecutedToolCalls, toolCall)
		successResult := ai.ReActToolResult{
			Name:       toolCall.Name,
			Result:     result,
			ResultFile: resultFile,
			ResultSize: resultSize,
			Args:       cloneToolEventMap(toolCall.Args),
			Hint:       hint,
		}
		state.ToolResults = append(state.ToolResults, successResult)

		// Format continuation for next iteration (provider-specific)
		state.Continuations = e.Adapter.FormatContinuation(toolCall, successResult, state.Continuations)

		// Check for short-circuit on terminal hints
		if shouldShortCircuitAskLoopOnToolHint(hint) {
			emitVerboseProgress(ctx, "Tool `%s` reported a terminal hard error; short-circuiting the Ask loop.", toolCall.Name)
			return e.Adapter.BuildFinalResponse(
				preserveStructuredToolResult(result, e.EnableObfuscation),
				state.ExecutedToolCalls,
				state.ToolResults,
				state.Continuations,
				state.EscalationState,
			), nil
		}

		// Check if we should preserve structured result
		if e.shouldPreserveStructuredResult(ctx, state.ToolResults) {
			return e.Adapter.BuildFinalResponse(
				preserveStructuredToolResult(result, e.EnableObfuscation),
				state.ExecutedToolCalls,
				state.ToolResults,
				state.Continuations,
				state.EscalationState,
			), nil
		}
	}

	// Reached max iterations → invoke LLM one more time for clarification
	emitVerboseProgress(ctx, "Reached retry cap; switching to clarification.")

	log.Warn("Unified ReAct Engine Reached Max Iterations",
		"adapter", e.Adapter.Name(),
		"max_iterations", e.Adapter.MaxIterations())

	// Set repair directive for retry cap
	state.RepairDirective = fmt.Sprintf(
		"We have hit the retry cap. The query has reached the iteration limit (%d) without completing. "+
			"Please ask the user one concrete clarification question that identifies the missing input or blocker.",
		e.Adapter.MaxIterations())

	// Prepare LLM request with retry cap directive
	prompt, opts, err := e.Adapter.PrepareLLMRequest(ctx, state)
	if err != nil {
		// Fallback: return generic message if preparation fails
		finalText := fmt.Sprintf(
			"We have reached the maximum number of iterations (%d). "+
				"Please provide additional guidance to help complete the task.",
			e.Adapter.MaxIterations())
		return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
	}

	// Invoke LLM to generate clarification question
	output, err := e.Adapter.InvokeLLM(ctx, prompt, opts, req)
	if err != nil {
		// Fallback: return generic message if invocation fails
		finalText := fmt.Sprintf(
			"We have reached the maximum number of iterations (%d). "+
				"Please provide additional guidance to help complete the task.",
			e.Adapter.MaxIterations())
		return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
	}

	// Extract clarification text
	_, finalText, err := e.Adapter.ExtractToolCalls(output)
	if err != nil || finalText == "" {
		// Fallback: return generic message if extraction fails
		finalText = fmt.Sprintf(
			"We have reached the maximum number of iterations (%d). "+
				"Please provide additional guidance to help complete the task.",
			e.Adapter.MaxIterations())
	}

	return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
}

// getLatestToolHint returns the hint from the most recent tool result, if any.
func (e *UnifiedReActEngine) getLatestToolHint(toolResults []ai.ReActToolResult) *ai.ToolProgressHint {
	if len(toolResults) == 0 {
		return nil
	}
	return toolResults[len(toolResults)-1].Hint
}

// wasResearchRecentlyDone checks if research (list_stores) was recently performed
// after the last failure of the given tool.
func (e *UnifiedReActEngine) wasResearchRecentlyDone(toolResults []ai.ReActToolResult, failedTool string) bool {
	if len(toolResults) < 2 {
		return false
	}

	// Look backwards through tool results for the pattern:
	// 1. Failed tool with error
	// 2. Followed by successful list_stores
	for i := len(toolResults) - 1; i >= 0; i-- {
		result := toolResults[i]

		// Check if this is a successful list_stores result
		if result.Name == "list_stores" && !strings.Contains(strings.ToLower(result.Result), "error") {
			// Check if there was a failure of our tool before this research
			for j := i - 1; j >= 0 && j >= i-3; j-- {
				prevResult := toolResults[j]
				if prevResult.Name == failedTool &&
					(strings.Contains(prevResult.Result, "Retry instruction:") ||
						strings.Contains(prevResult.Result, "Tool execution error:")) {
					return true
				}
			}
		}

		// Only look back a limited number of results
		if i < len(toolResults)-6 {
			break
		}
	}

	return false
}

// shouldPreserveStructuredResult checks if the last tool result should be preserved as-is.
func (e *UnifiedReActEngine) shouldPreserveStructuredResult(ctx context.Context, toolResults []ai.ReActToolResult) bool {
	if len(toolResults) == 0 {
		return false
	}
	// Convert to compatibility-path format for the shared helper functions.
	compatibleResults := e.convertToCompatibleResults(toolResults)
	return shouldPreserveStructuredToolResult(ctx, compatibleResults)
}

// convertToCompatibleResults converts shared ReActToolResult to compatibleToolResult.
func (e *UnifiedReActEngine) convertToCompatibleResults(shared []ai.ReActToolResult) []compatibleToolResult {
	if len(shared) == 0 {
		return nil
	}
	compatible := make([]compatibleToolResult, 0, len(shared))
	for _, result := range shared {
		compatible = append(compatible, compatibleToolResult{
			Name:          result.Name,
			Result:        result.Result,
			ResultFile:    result.ResultFile,
			ResultSize:    result.ResultSize,
			Args:          result.Args,
			Hint:          result.Hint,
			RetryGuidance: result.RetryGuidance,
			ExecutionMode: result.ExecutionMode,
		})
	}
	return compatible
}

// convertRepairPlanToCompatible converts RepairPlan to compatibility-path repair state for error formatting.
func (e *UnifiedReActEngine) convertRepairPlanToCompatible(plan RepairPlan) compatiblePendingToolRepair {
	var strategy compatibleRepairStrategy
	switch plan.Strategy {
	case RepairStrategyResearchFirst:
		strategy = compatibleRepairStrategyResearchFirst
	default:
		strategy = compatibleRepairStrategySameTool
	}

	return compatiblePendingToolRepair{
		ToolName:       plan.ToolName,
		Strategy:       strategy,
		ResearchTool:   plan.ResearchTool,
		ResearchReason: plan.ResearchReason,
		Tier:           compatibleRetryTier(plan.Tier),
	}
}

// buildRepairDirective builds the repair directive string from tool results.
func (e *UnifiedReActEngine) buildRepairDirective(toolResults []ai.ReActToolResult) string {
	if len(toolResults) == 0 {
		return ""
	}
	// Convert to compatibility-path format for the shared helper function.
	compatibleResults := e.convertToCompatibleResults(toolResults)
	return buildCompatibleRepairDirective(compatibleResults[len(compatibleResults)-1])
}
