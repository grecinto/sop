package agent

import (
	"context"
	"fmt"
	log "log/slog"

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
	return "native"
}

// MaxIterations returns the iteration limit for native loops.
func (a *CompatibleReActAdapter) MaxIterations() int {
	return nativeReActMaxToolIterations
}

// PrepareLLMRequest builds the native prompt with MRU state and budget management.
func (a *CompatibleReActAdapter) PrepareLLMRequest(ctx context.Context, state ReActLoopState) (string, ai.GenOptions, error) {
	// Convert ReActToolResult back to nativeToolResult for legacy functions
	nativeResults := a.convertToNativeResults(state.ToolResults)

	// Determine temperature and thinking level
	temperature := nativeReActToolCallTemperature
	thinkingLevel := "low" // Default: strict schema adherence for tool calls
	if state.RepairDirective != "" {
		temperature = nativeReActRepairTemperature
		thinkingLevel = "medium" // More reasoning for repair strategies
	}
	thinkingLevel = NormalizeThinkingLevelForGenerator(state.Request.Generator, "", "", thinkingLevel)

	// Build prompt using existing native functions
	promptProfile := nativeReActPromptBudgetProfile()
	askState := buildAskAnchoredMRUState(nativeResults)
	mainPrompt, promptReport := buildNativeReActPromptWithReport(state.Request, nativeResults, askState, promptProfile)

	// If there's a repair directive from unified engine, append it to the prompt
	// This handles special cases like retry cap that aren't part of tool results
	if state.RepairDirective != "" {
		mainPrompt = mainPrompt + "\n\n" + state.RepairDirective
	}

	// Log prompt budget application
	if firstTrimmed := firstTrimmedPromptComponent(promptProfile, promptReport); firstTrimmed != "" {
		log.Info("Native Adapter Prompt Budget Applied",
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
		ToolCallContinuations: state.Continuations,
	}

	log.Info("Native Adapter Prepared Request",
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
	// Check for native tool calls
	if len(output.ToolCalls) > 0 {
		return output.ToolCalls, "", nil
	}

	// Try to recover embedded textual tool call
	if recoveredCall, ok, recoverErr := extractEmbeddedNativeToolCall(output.Text); ok {
		if recoverErr != nil {
			// Malformed textual tool call
			return nil, "", recoverErr
		}

		log.Info("Native Adapter Recovered Textual Tool Call",
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
// Native adapter uses single-continuation stateless pattern.
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

// BuildFinalResponse assembles the final ReasoningResponse with outcome facts/recipes.
func (a *CompatibleReActAdapter) BuildFinalResponse(
	finalText string,
	toolCalls []ai.ToolCall,
	toolResults []ai.ReActToolResult,
	continuations []ai.ToolCallContinuation,
	escalationState map[string]any,
) ai.ReasoningResponse {
	// Convert to native results for legacy functions
	nativeResults := a.convertToNativeResults(toolResults)

	// Deobfuscate final text if needed
	if a.EnableObfuscation && finalText != "" {
		finalText = obfuscation.GlobalObfuscator.DeobfuscateText(finalText)
	}

	return ai.ReasoningResponse{
		FinalText:       finalText,
		ToolCalls:       toolCalls,
		OutcomeFacts:    summarizeOutcomeFacts(nativeResults),
		OutcomeRecipes:  summarizeOutcomeRecipes(nativeResults),
		EscalationState: escalationState,
	}
}

// ShouldTerminateEarly checks native-specific termination conditions.
func (a *CompatibleReActAdapter) ShouldTerminateEarly(state ReActLoopState) (bool, string) {
	// Check for terminal hints (anti_success, blocked, fatal, etc)
	if len(state.ToolResults) > 0 {
		lastResult := state.ToolResults[len(state.ToolResults)-1]
		if shouldShortCircuitAskLoopOnToolHint(lastResult.Hint) {
			return true, fmt.Sprintf("Tool '%s' reported terminal error", lastResult.Name)
		}
	}

	// Native adapter doesn't have budget-based early termination
	// (that's handled by MaxIterations)
	return false, ""
}

// RestoreContinuations deserializes continuations from carryover state.
// Native adapter typically doesn't preserve continuations across Ask turns.
func (a *CompatibleReActAdapter) RestoreContinuations(carryover *ai.CarryoverState) ([]ai.ToolCallContinuation, error) {
	// Native loop is stateless per Ask - no continuations to restore
	return nil, nil
}

// convertToNativeResults converts shared ReActToolResult to native nativeToolResult.
// This is needed to reuse existing helper functions during migration.
func (a *CompatibleReActAdapter) convertToNativeResults(shared []ai.ReActToolResult) []nativeToolResult {
	if len(shared) == 0 {
		return nil
	}
	native := make([]nativeToolResult, 0, len(shared))
	for _, result := range shared {
		native = append(native, nativeToolResult{
			Name:          result.Name,
			Result:        result.Result,
			ResultFile:    result.ResultFile,
			ResultSize:    result.ResultSize,
			Args:          result.Args,
			Hint:          result.Hint,
			ExecutionMode: result.ExecutionMode,
		})
	}
	return native
}
