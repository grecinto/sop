package agent

import (
	"context"
	"fmt"
	log "log/slog"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/obfuscation"
)

// CompatibleReActAdapter implements ReActLoopAdapter for the reserved shared/default
// serial ReAct path.
//
// It is maintained for providers that naturally fit the compatible one-tool-per-turn
// shared behavior. The currently supported provider-owned loops do not use this as
// their active outer-loop shape.
type CompatibleReActAdapter struct {
	EnableObfuscation bool
	MaxTokens         int
}

// NewCompatibleReActAdapter creates a new shared compatible adapter instance.
func NewCompatibleReActAdapter(enableObfuscation bool, maxTokens int) *CompatibleReActAdapter {
	return &CompatibleReActAdapter{
		EnableObfuscation: enableObfuscation,
		MaxTokens:         maxTokens,
	}
}

// NewNativeAdapter is kept as a compatibility alias while older references are phased out.
func NewNativeAdapter(enableObfuscation bool, maxTokens int) *CompatibleReActAdapter {
	return NewCompatibleReActAdapter(enableObfuscation, maxTokens)
}

// Name returns the adapter identifier.
func (a *CompatibleReActAdapter) Name() string {
	return "compatible"
}

// MaxIterations returns the iteration limit for the compatible shared loop.
func (a *CompatibleReActAdapter) MaxIterations() int {
	return compatibleReActMaxToolIterations
}

// PrepareLLMRequest builds the compatible-loop prompt with MRU state and budget management.
func (a *CompatibleReActAdapter) PrepareLLMRequest(ctx context.Context, state ReActLoopState) (string, ai.GenOptions, error) {
	// Convert ReActToolResult back to compatibleToolResult for compatibility-path helpers.
	compatibleResults := a.convertToCompatibleResults(state.ToolResults)

	// Determine temperature and thinking level
	temperature := compatibleReActToolCallTemperature
	thinkingLevel := "low" // Default: strict schema adherence for tool calls
	if state.RepairDirective != "" {
		temperature = compatibleReActRepairTemperature
		thinkingLevel = "medium" // More reasoning for repair strategies
	}
	thinkingLevel = NormalizeThinkingLevelForGenerator(state.Request.Generator, "", "", thinkingLevel)

	// Build prompt using the reserved compatible-path helpers.
	promptProfile := compatibleReActPromptBudgetProfile()
	askState := buildAskAnchoredMRUState(compatibleResults)
	mainPrompt, promptReport := buildCompatibleReActPromptWithReport(state.Request, compatibleResults, askState, promptProfile)

	// If there's a repair directive from unified engine, append it to the prompt
	// This handles special cases like retry cap that aren't part of tool results
	if state.RepairDirective != "" {
		mainPrompt = mainPrompt + "\n\n" + state.RepairDirective
	}

	// Log prompt budget application
	if firstTrimmed := firstTrimmedPromptComponent(promptProfile, promptReport); firstTrimmed != "" {
		log.Info("Compatible Adapter Prompt Budget Applied",
			"iteration", state.Iteration,
			"trimmed_first", firstTrimmed,
			"trimmed", summarizePromptBudgetTrim(promptReport),
			"final_chars", len(mainPrompt))
	}

	// Build generation options
	var tools []ai.ToolDefinition
	if state.Request.Executor != nil {
		var err error
		tools, err = state.Request.Executor.ListTools(ctx)
		if err != nil {
			return "", ai.GenOptions{}, fmt.Errorf("failed to list tools: %w", err)
		}
	}

	opts := ai.GenOptions{
		SystemPrompt:          state.Request.SystemPrompt,
		MaxTokens:             a.MaxTokens,
		Temperature:           temperature,
		ThinkingLevel:         thinkingLevel,
		Tools:                 tools,
		ToolCallContinuations: a.wrapToolCallContinuations(state),
	}

	log.Info("Compatible Adapter Prepared Request",
		"iteration", state.Iteration,
		"prompt_chars", len(mainPrompt),
		"tool_count", len(tools),
		"continuations", len(state.Continuations))

	return mainPrompt, opts, nil
}

// InvokeLLM calls the standard Generator.Generate() method.
func (a *CompatibleReActAdapter) InvokeLLM(ctx context.Context, prompt string, opts ai.GenOptions, req ai.ReasoningRequest) (ai.GenOutput, error) {
	return req.Generator.Generate(ctx, prompt, opts)
}

// ExtractToolCalls parses tool calls from the LLM response, including textual recovery.
func (a *CompatibleReActAdapter) ExtractToolCalls(output ai.GenOutput) ([]ai.ToolCall, string, error) {
	// Check for direct tool calls first.
	if len(output.ToolCalls) > 0 {
		return output.ToolCalls, "", nil
	}

	// Try to recover embedded textual tool call using the native tool-call format.
	if recoveredCall, ok, recoverErr := extractEmbeddedNativeToolCall(output.Text); ok {
		if recoverErr != nil {
			// Malformed textual tool call
			return nil, "", recoverErr
		}

		log.Info("Compatible Adapter Recovered Textual Tool Call",
			"tool", recoveredCall.Name)
		return []ai.ToolCall{recoveredCall}, "", nil
	}

	// No tool calls → final text
	finalText := output.Text
	if a.EnableObfuscation {
		finalText = obfuscation.GlobalObfuscator.DeobfuscateText(output.Text)
	}

	return nil, finalText, nil
}

// FormatContinuation builds the continuation for the next iteration.
// Compatible adapter uses a single-continuation stateless pattern.
func (a *CompatibleReActAdapter) FormatContinuation(
	toolCall ai.ToolCall,
	result ai.ReActToolResult,
	existingContinuations []ai.ToolCallContinuation,
) []ai.ToolCallContinuation {
	return []ai.ToolCallContinuation{
		{
			ToolCall: toolCall,
			Response: coerceToolCallContinuationResponse(result.Result),
		},
	}
}

func (a *CompatibleReActAdapter) wrapToolCallContinuations(state ReActLoopState) []ai.ToolCallContinuation {
	if len(state.Continuations) == 0 {
		return nil
	}
	wrapped := make([]ai.ToolCallContinuation, 0, len(state.Continuations))
	for idx, continuation := range state.Continuations {
		response := continuation.Response
		if idx == len(state.Continuations)-1 {
			response = map[string]any{
				"tool_result": continuation.Response,
				"react_state": map[string]any{
					"user_query":           strings.TrimSpace(state.Request.UserQuery),
					"latest_tool":          strings.TrimSpace(continuation.ToolCall.Name),
					"repair_directive":     strings.TrimSpace(state.RepairDirective),
					"retry_guidance":       sharedLatestRetryGuidance(state.ToolResults),
					"iteration":            state.Iteration,
					"task_status":          sharedContinuationTaskStatus(state),
					"phase":                string(sharedContinuationPhase(state)),
					"allowed_next_actions": sharedAllowedNextActions(state),
					"has_more_tool_work":   true,
				},
			}
		}
		wrapped = append(wrapped, ai.ToolCallContinuation{ToolCall: continuation.ToolCall, Response: response})
	}
	return wrapped
}

func sharedLatestRetryGuidance(results []ai.ReActToolResult) *ai.RetryGuidance {
	for idx := len(results) - 1; idx >= 0; idx-- {
		if results[idx].RetryGuidance != nil {
			return results[idx].RetryGuidance
		}
	}
	return nil
}

func sharedContinuationPhase(state ReActLoopState) ai.ReActLoopPhase {
	if sharedContinuationHasRepairState(state) {
		return ai.ReActLoopPhaseRepair
	}
	return ai.ReActLoopPhaseActive
}

func sharedContinuationTaskStatus(state ReActLoopState) string {
	if sharedContinuationHasRepairState(state) {
		return "repair_required"
	}
	if len(state.ToolResults) > 0 {
		return "tool_progressed"
	}
	return "active"
}

func sharedAllowedNextActions(state ReActLoopState) []string {
	if guidance := sharedLatestRetryGuidance(state.ToolResults); guidance != nil {
		actions := make([]string, 0, 2)
		next := guidance.NextAction
		if next == "" {
			next = ai.ReActNextActionRetrySameTool
		}
		actions = append(actions, string(next))
		if next != ai.ReActNextActionAskClarification {
			actions = append(actions, string(ai.ReActNextActionAskClarification))
		}
		return actions
	}
	if strings.TrimSpace(state.RepairDirective) != "" {
		return []string{string(ai.ReActNextActionRetrySameTool), string(ai.ReActNextActionAskClarification)}
	}
	return []string{string(ai.ReActNextActionCallTool), string(ai.ReActNextActionAnswerUser)}
}

func sharedContinuationHasRepairState(state ReActLoopState) bool {
	return sharedLatestRetryGuidance(state.ToolResults) != nil || strings.TrimSpace(state.RepairDirective) != ""
}

// BuildFinalResponse assembles the final ReasoningResponse with outcome facts/recipes.
func (a *CompatibleReActAdapter) BuildFinalResponse(
	finalText string,
	toolCalls []ai.ToolCall,
	toolResults []ai.ReActToolResult,
	continuations []ai.ToolCallContinuation,
	escalationState map[string]any,
	clarificationRequest *ai.ClarificationRequest,
) ai.ReasoningResponse {
	// Convert to compatible results for compatibility-path helpers.
	compatibleResults := a.convertToCompatibleResults(toolResults)

	// Deobfuscate final text if needed
	if a.EnableObfuscation && finalText != "" {
		finalText = obfuscation.GlobalObfuscator.DeobfuscateText(finalText)
	}
	if clarificationRequest != nil && strings.TrimSpace(finalText) != "" {
		clarificationRequest = &ai.ClarificationRequest{
			TargetQuery:       clarificationRequest.TargetQuery,
			AssistantQuestion: strings.TrimSpace(finalText),
			MissingFact:       clarificationRequest.MissingFact,
			Reason:            clarificationRequest.Reason,
			Source:            clarificationRequest.Source,
			Status:            clarificationRequest.Status,
		}
	}

	return ai.ReasoningResponse{
		FinalText:             finalText,
		ToolCalls:             toolCalls,
		OutcomeFacts:          summarizeOutcomeFacts(compatibleResults),
		OutcomeRecipes:        summarizeOutcomeRecipes(compatibleResults),
		EscalationState:       escalationState,
		ClarificationRequest:  clarificationRequest,
	}
}

// ShouldTerminateEarly checks compatible-loop termination conditions.
func (a *CompatibleReActAdapter) ShouldTerminateEarly(state ReActLoopState) (bool, string) {
	// Check for terminal hints (anti_success, blocked, fatal, etc)
	if len(state.ToolResults) > 0 {
		lastResult := state.ToolResults[len(state.ToolResults)-1]
		if shouldShortCircuitAskLoopOnToolHint(lastResult.Hint) {
			return true, fmt.Sprintf("Tool '%s' reported terminal error", lastResult.Name)
		}
	}

	// Compatible adapter doesn't have budget-based early termination
	// (that's handled by MaxIterations)
	return false, ""
}

// RestoreContinuations deserializes continuations from carryover state.
// Compatible adapter typically doesn't preserve continuations across Ask turns.
func (a *CompatibleReActAdapter) RestoreContinuations(carryover *ai.CarryoverState) ([]ai.ToolCallContinuation, error) {
	// Compatible loop is stateless per Ask - no continuations to restore.
	return nil, nil
}

// convertToCompatibleResults converts shared ReActToolResult to compatibleToolResult.
// This is needed to reuse the compatibility-path helper functions.
func (a *CompatibleReActAdapter) convertToCompatibleResults(shared []ai.ReActToolResult) []compatibleToolResult {
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
