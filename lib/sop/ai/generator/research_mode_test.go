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
	got := summarizeChatGPTContinuationToolOutput("execute_script", `[{"id":1},{"id":2},{"id":3}]`, ai.ReActToolResultExecutionModeStructuredSummary)
	if !strings.Contains(got, "Preview (first 3 rows)") {
		t.Fatalf("expected capped preview rows, got %q", got)
	}
	if !strings.Contains(got, "\"id\":1") {
		t.Fatalf("expected a sample row payload in the preview, got %q", got)
	}
}

func TestSummarizeChatGPTContinuationToolOutput_FinalResultUsesCompactSummary(t *testing.T) {
	got := summarizeChatGPTContinuationToolOutput("execute_script", `[{"id":1},{"id":2},{"id":3}]`, ai.ReActToolResultExecutionModeSummary)
	if !strings.Contains(got, "The full row payload was already streamed") {
		t.Fatalf("expected compact summary for final result, got %q", got)
	}
	if strings.Contains(got, "Preview (first") {
		t.Fatalf("did not expect preview rows for final result, got %q", got)
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
