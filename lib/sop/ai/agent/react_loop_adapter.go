package agent

import (
	"context"

	"github.com/sharedcode/sop/ai"
)

// ReActLoopAdapter allows providers that fit the shared loop model to customize
// specific parts of the ReAct loop while inheriting common orchestration, retry
// logic, and state management.
//
// The unified engine handles:
//   - Iteration control and loop lifecycle
//   - Tool execution orchestration
//   - Progressive retry logic (Tier 1/2A/2B/3)
//   - Failure tracking and escalation state management
//   - Response building with EscalationState
//
// Adapters customize:
//   - LLM invocation (Generator.Generate vs custom APIs)
//   - Request/prompt formatting (provider-specific)
//   - Continuation handling (previous_response_id, message threads, etc)
//   - Carryover state formatting
//   - Early termination conditions
//
// This interface is intentionally for providers compatible with the shared loop.
// Providers with transport-native outer-loop semantics can stay on owned loops
// via ai.ReActLoopProvider plus ai.ReActLoopModeProvider.
type ReActLoopAdapter interface {
	// Name returns the adapter identifier (e.g., "chatgpt", "anthropic", "gemini", "native")
	Name() string

	// MaxIterations returns the iteration limit for this provider.
	MaxIterations() int

	// PrepareLLMRequest builds the provider-specific prompt and options for the current iteration.
	// The engine provides common state (iteration, tool results, repair directive, continuations).
	//
	// Returns:
	//   - prompt: The user prompt/query for this iteration
	//   - opts: Generation options (tools, temperature, continuations, etc)
	//   - error: If request preparation fails
	PrepareLLMRequest(ctx context.Context, state ReActLoopState) (prompt string, opts ai.GenOptions, err error)

	// InvokeLLM calls the provider's LLM API and returns the output.
	// Most providers use req.Generator.Generate(), but some (ChatGPT) use custom APIs.
	//
	// Returns:
	//   - output: LLM generation output (text, tool calls, metadata)
	//   - error: If LLM invocation fails
	InvokeLLM(ctx context.Context, prompt string, opts ai.GenOptions, req ai.ReasoningRequest) (ai.GenOutput, error)

	// ExtractToolCalls parses tool calls from the LLM response.
	// If no tool calls are present, returns final text for response completion.
	//
	// Returns:
	//   - toolCalls: List of tools the LLM wants to call
	//   - finalText: Text to return if no tool calls (loop termination)
	//   - error: If extraction fails
	ExtractToolCalls(output ai.GenOutput) (toolCalls []ai.ToolCall, finalText string, err error)

	// FormatContinuation builds the continuation payload for the next iteration.
	// This is provider-specific:
	//   - ChatGPT: tracks previous_response_id
	//   - Shared-compatible providers: append provider-specific replay state
	//   - Native: typically empty (stateless per Ask)
	//
	// Returns:
	//   - Updated continuations list for next iteration
	FormatContinuation(
		toolCall ai.ToolCall,
		result ai.ReActToolResult,
		existingContinuations []ai.ToolCallContinuation,
	) []ai.ToolCallContinuation

	// BuildFinalResponse assembles the final ReasoningResponse with provider-specific
	// carryover state. The engine provides common fields (tool calls, escalation state).
	//
	// Returns:
	//   - Complete ReasoningResponse ready to return to caller
	BuildFinalResponse(
		finalText string,
		toolCalls []ai.ToolCall,
		toolResults []ai.ReActToolResult,
		continuations []ai.ToolCallContinuation,
		escalationState map[string]any,
		clarificationRequest *ai.ClarificationRequest,
	) ai.ReasoningResponse

	// ShouldTerminateEarly checks provider-specific termination conditions.
	// Examples:
	//   - Gemini: checks for terminal hints in tool results
	//   - Native: checks budget exhaustion
	//   - Most: return false (no early termination)
	//
	// Returns:
	//   - shouldStop: true if loop should terminate before max iterations
	//   - reason: human-readable reason for termination
	ShouldTerminateEarly(state ReActLoopState) (shouldStop bool, reason string)

	// RestoreContinuations deserializes continuations from carryover state.
	// Each provider stores continuations differently in CarryoverState.
	//
	// Returns:
	//   - Restored continuations for this Ask turn
	//   - error: If deserialization fails (non-fatal, logs warning)
	RestoreContinuations(carryover *ai.CarryoverState) ([]ai.ToolCallContinuation, error)
}

// ReActLoopState captures the current state of the loop, provided to adapters.
// This gives adapters full context to make decisions about prompt formatting,
// early termination, etc.
type ReActLoopState struct {
	// Iteration is the current iteration number (1-based)
	Iteration int

	// Request is the original reasoning request from the Ask pipeline
	Request ai.ReasoningRequest

	// ToolResults contains all tool execution results so far this Ask turn
	ToolResults []ai.ReActToolResult

	// ExecutedToolCalls contains all tool calls executed so far this Ask turn
	ExecutedToolCalls []ai.ToolCall

	// Continuations contains provider-specific continuation state
	// (e.g., ChatGPT previous_response_id, Anthropic message thread)
	Continuations []ai.ToolCallContinuation

	// FailureCounts tracks per-tool failure counts for retry logic
	// Map key: tool name, value: failure count
	FailureCounts map[string]int

	// RepairDirective contains the repair instruction for the current iteration
	// (e.g., "Repair directive: Call list_stores first before retrying execute_script")
	RepairDirective string

	// EscalationState contains Tier 3 escalation metadata if escalation occurred
	// Fields: tier, failed_tool, failure_count, strategy, suggested_action, error_type
	EscalationState map[string]any
}
