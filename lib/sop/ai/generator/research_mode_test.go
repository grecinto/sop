package generator

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestSelectToolResultExecutionMode_PreservesFullPayloadForInspectionTools(t *testing.T) {
	got := selectToolResultExecutionMode("git_status", true)
	if got != ai.ReActToolResultExecutionModeFull {
		t.Fatalf("expected full payload mode for inspection tools, got %q", got)
	}
}

func TestSelectToolResultExecutionMode_UsesCompactModeForWriteTools(t *testing.T) {
	got := selectToolResultExecutionMode("git_commit", true)
	if got != ai.ReActToolResultExecutionModeSummary {
		t.Fatalf("expected compact summary mode for write tools, got %q", got)
	}
}

func TestSummarizeChatGPTContinuationToolOutput_UsesPreviewForNonFinalResults(t *testing.T) {
	got := summarizeChatGPTContinuationToolOutput(ai.ReActToolResult{Name: "execute_script", Result: `[{"id":1},{"id":2},{"id":3}]`, ExecutionMode: ai.ReActToolResultExecutionModeStructuredSummary})
	if !strings.Contains(got, "Preview (first 3 rows)") {
		t.Fatalf("expected capped preview rows, got %q", got)
	}
	if !strings.Contains(got, "\"id\":1") {
		t.Fatalf("expected a sample row payload in the preview, got %q", got)
	}
}

func TestSummarizeChatGPTContinuationToolOutput_FinalResultUsesCompactSummary(t *testing.T) {
	got := summarizeChatGPTContinuationToolOutput(ai.ReActToolResult{Name: "execute_script", Result: `[{"id":1},{"id":2},{"id":3}]`, ExecutionMode: ai.ReActToolResultExecutionModeSummary})
	if !strings.Contains(got, "The full row payload was already streamed") {
		t.Fatalf("expected compact summary for final result, got %q", got)
	}
	if strings.Contains(got, "Preview (first") {
		t.Fatalf("did not expect preview rows for final result, got %q", got)
	}
}

func TestSummarizeChatGPTContinuationToolOutput_AppendsStructuredRetryGuidance(t *testing.T) {
	got := summarizeChatGPTContinuationToolOutput(ai.ReActToolResult{
		Name:   "execute_script",
		Result: "Tool execution error: missing required field\nRetry instruction: Call execute_script next with corrected arguments.",
		RetryGuidance: &ai.RetryGuidance{
			ToolName:        "execute_script",
			Tier:            ai.RetryTierQuickFix,
			Strategy:        ai.RepairStrategySameTool,
			AllowedTool:     "execute_script",
			NextAction:      ai.ReActNextActionRetrySameTool,
			FailureCount:    1,
			SoftRetryBudget: 2,
			HardRetryBudget: 4,
		},
	})
	if !strings.Contains(got, "Retry policy state:") {
		t.Fatalf("expected structured retry guidance state, got %q", got)
	}
}

func TestSummarizeChatGPTContinuationToolOutput_WrapsRetryGuidanceInStructuredEnvelope(t *testing.T) {
	got := summarizeChatGPTContinuationToolOutput(ai.ReActToolResult{
		Name:   "execute_script",
		Result: "Tool execution error: missing required field",
		RetryGuidance: &ai.RetryGuidance{
			ToolName:        "execute_script",
			Tier:            ai.RetryTierQuickFix,
			Strategy:        ai.RepairStrategySameTool,
			AllowedTool:     "execute_script",
			NextAction:      ai.ReActNextActionRetrySameTool,
			FailureCount:    1,
			SoftRetryBudget: 2,
			HardRetryBudget: 4,
		},
	})
	if !strings.Contains(got, "\"retry_guidance\"") {
		t.Fatalf("expected structured retry guidance envelope, got %q", got)
	}
	if !strings.Contains(got, "\"repair_directive\"") {
		t.Fatalf("expected repair directive in structured envelope, got %q", got)
	}
}

func TestSummarizeGeminiContinuationToolOutput_UsesPreviewForNonFinalResults(t *testing.T) {
	got := summarizeGeminiContinuationToolOutput("execute_script", map[string]any{"result": `[{"id":1},{"id":2},{"id":3}]`}, ai.ReActToolResultExecutionModeStructuredSummary)
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", got)
	}
	result, _ := payload["result"].(string)
	if !strings.Contains(result, "Preview (first 3 rows)") {
		t.Fatalf("expected capped preview rows, got %q", result)
	}
}

func TestSummarizeGeminiContinuationToolOutput_FinalResultUsesCompactSummary(t *testing.T) {
	got := summarizeGeminiContinuationToolOutput("execute_script", map[string]any{"result": `[{"id":1},{"id":2},{"id":3}]`}, ai.ReActToolResultExecutionModeSummary)
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", got)
	}
	result, _ := payload["result"].(string)
	if !strings.Contains(result, "The full row payload was already streamed") {
		t.Fatalf("expected compact summary in default mode, got %q", result)
	}
}

func TestSummarizeGeminiContinuationToolOutput_PreservesRetryGuidanceEnvelope(t *testing.T) {
	got := summarizeGeminiContinuationToolOutput("execute_script", map[string]any{
		"tool_result": map[string]any{"result": `[{"id":1},{"id":2},{"id":3}]`},
		"react_state": map[string]any{"retry_guidance": map[string]any{"strategy": "research_first"}},
	}, ai.ReActToolResultExecutionModeSummary)
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", got)
	}
	if payload["react_state"] == nil {
		t.Fatalf("expected react_state to be preserved, got %#v", payload)
	}
	toolResult, ok := payload["tool_result"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested tool_result to be preserved, got %#v", payload)
	}
	result, _ := toolResult["result"].(string)
	if !strings.Contains(result, "The full row payload was already streamed") {
		t.Fatalf("expected nested result summary, got %q", result)
	}
}

func TestSummarizeAnthropicContinuationToolOutput_UsesPreviewForNonFinalResults(t *testing.T) {
	got := summarizeAnthropicContinuationToolOutput("execute_script", map[string]any{"result": `[{"id":1},{"id":2},{"id":3}]`}, ai.ReActToolResultExecutionModeStructuredSummary)
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", got)
	}
	result, _ := payload["result"].(string)
	if !strings.Contains(result, "Preview (first 3 rows)") {
		t.Fatalf("expected capped preview rows, got %q", result)
	}
}

func TestSummarizeAnthropicContinuationToolOutput_FinalResultUsesCompactSummary(t *testing.T) {
	got := summarizeAnthropicContinuationToolOutput("execute_script", map[string]any{"result": `[{"id":1},{"id":2},{"id":3}]`}, ai.ReActToolResultExecutionModeSummary)
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", got)
	}
	result, _ := payload["result"].(string)
	if !strings.Contains(result, "The full row payload was already streamed") {
		t.Fatalf("expected compact summary in default mode, got %q", result)
	}
}

func TestSummarizeAnthropicContinuationToolOutput_PreservesRetryGuidanceEnvelope(t *testing.T) {
	got := summarizeAnthropicContinuationToolOutput("execute_script", map[string]any{
		"tool_error":     map[string]any{"message": "Tool execution error: invalid join\nRetry instruction: Call list_stores first."},
		"retry_guidance": map[string]any{"strategy": "research_first"},
	}, ai.ReActToolResultExecutionModeSummary)
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", got)
	}
	if payload["retry_guidance"] == nil {
		t.Fatalf("expected retry_guidance to be preserved, got %#v", payload)
	}
	toolErr, ok := payload["tool_error"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested tool_error to be preserved, got %#v", payload)
	}
	message, _ := toolErr["result"].(string)
	if message == "" {
		message, _ = toolErr["message"].(string)
	}
	if !strings.Contains(message, "Retry instruction:") {
		t.Fatalf("expected nested retry message to be preserved, got %#v", toolErr)
	}
}
func TestSummarizeToolResultForLLM_FullSmallOnlyKeepsShortPayloads(t *testing.T) {
	got := SummarizeToolResultForLLM("execute_script", `{"ok":true}`, ai.ReActToolResultExecutionModeFullSmallOnly)
	if got != `{"ok":true}` {
		t.Fatalf("expected full payload for short results in full-small-only mode, got %q", got)
	}
}

func TestSummarizeToolResultForLLM_FullSmallOnlySummarizesLargePayloads(t *testing.T) {
	payload := strings.Repeat("x", 600)
	got := SummarizeToolResultForLLM("execute_script", payload, ai.ReActToolResultExecutionModeFullSmallOnly)
	if !strings.Contains(got, "large textual payload") {
		t.Fatalf("expected a compact summary for large results in full-small-only mode, got %q", got)
	}
}

func TestSummarizeToolResultForLLM_FullModePreservesFullPayload(t *testing.T) {
	payload := strings.Repeat("x", 600)
	got := SummarizeToolResultForLLM("search_web", payload, ai.ReActToolResultExecutionModeFull)
	if got != payload {
		t.Fatalf("expected full payload to be preserved in full mode, got %q", got)
	}
}

func TestSummarizeToolResultForLLM_SummaryModePreservesShortGenericPayload(t *testing.T) {
	got := SummarizeToolResultForLLM("search_web", "found 3 results", ai.ReActToolResultExecutionModeSummary)
	if got != "found 3 results" {
		t.Fatalf("expected short generic payload to be preserved, got %q", got)
	}
}

func TestSummarizeToolResultForLLM_SummaryModeCompactsLargeGenericPayload(t *testing.T) {
	payload := strings.Repeat("x", 600)
	got := SummarizeToolResultForLLM("search_web", payload, ai.ReActToolResultExecutionModeSummary)
	if !strings.Contains(got, "large textual payload") {
		t.Fatalf("expected a compact summary for large generic payloads, got %q", got)
	}
}

func TestSummarizeToolResultForLLM_StructuredSummaryCompactsLargeGenericPayload(t *testing.T) {
	payload := strings.Repeat("x", 600)
	got := SummarizeToolResultForLLM("search_web", payload, ai.ReActToolResultExecutionModeStructuredSummary)
	if !strings.Contains(got, "Execution mode: structured_summary") {
		t.Fatalf("expected structured-summary guidance for large generic payloads, got %q", got)
	}
}

func TestSummarizeToolResultForLLM_EmptyModeFallsBackToStructuredSummary(t *testing.T) {
	payload := strings.Repeat("x", 600)
	got := SummarizeToolResultForLLM("search_web", payload, "")
	if !strings.Contains(got, "Execution mode: structured_summary") {
		t.Fatalf("expected empty mode to fall back to structured-summary guidance, got %q", got)
	}
}

func TestCloneGeminiReActToolResults_PreservesExecutionMode(t *testing.T) {
	input := []ai.ReActToolResult{{Name: "search_web", ExecutionMode: ai.ReActToolResultExecutionModeSummary}}
	got := cloneGeminiReActToolResults(input)
	if len(got) != 1 {
		t.Fatalf("expected one cloned result, got %d", len(got))
	}
	if got[0].ExecutionMode != ai.ReActToolResultExecutionModeSummary {
		t.Fatalf("expected execution mode to be preserved, got %q", got[0].ExecutionMode)
	}
}
