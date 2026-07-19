package generator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
)

func SummarizeToolResultForLLM(toolName, resultText string, mode ai.ReActToolResultExecutionMode) string {
	normalizedMode := normalizeToolResultExecutionMode(mode)
	trimmedTool := strings.TrimSpace(toolName)
	trimmedResult := strings.TrimSpace(resultText)
	if trimmedTool == "" {
		trimmedTool = "tool"
	}

	if trimmedResult == "" {
		return buildToolResultContext(trimmedTool, "completed with no textual payload", "No payload was provided.", "Use this as the current evidence only if the absence of payload is meaningful for the task.")
	}

	if !strings.EqualFold(trimmedTool, "execute_script") {
		switch normalizedMode {
		case ai.ReActToolResultExecutionModeSummary:
			if len(trimmedResult) > 240 {
				return buildToolResultContext(trimmedTool, "completed with a large textual payload", fmt.Sprintf("The full payload was already streamed to the client. Summary: %s", trimmedResult[:240]), "Execution mode: summary. This was an investigatory tool call. A concise summary is sufficient for the next reasoning turn; do not restate the full payload unless the task specifically requires exact details.")
			}
			return trimmedResult
		case ai.ReActToolResultExecutionModeStructuredSummary:
			if len(trimmedResult) > 240 {
				return buildToolResultContext(trimmedTool, "completed with a large textual payload", fmt.Sprintf("The full payload was already streamed to the client. Preview: %s", trimmedResult[:240]), "Execution mode: structured_summary. This was an intermediate tool result. Preserve a compact, structured summary for the next reasoning turn rather than the full payload.")
			}
			return trimmedResult
		case ai.ReActToolResultExecutionModeFullSmallOnly:
			if len(trimmedResult) > 500 {
				return buildToolResultContext(trimmedTool, fmt.Sprintf("completed with a large textual payload (%d chars)", len(trimmedResult)), "The full payload was already streamed to the client. Do not restate it; provide at most a brief summary.", "Execution mode: full_small_only. This was a real execution that already surfaced the full details to the client. For the next reasoning turn, keep the payload compact and only mention the essential facts.")
			}
			return trimmedResult
		case ai.ReActToolResultExecutionModeFull:
			return trimmedResult
		default:
			return trimmedResult
		}
	}

	if strings.EqualFold(trimmedTool, "execute_script") {
		switch normalizedMode {
		case ai.ReActToolResultExecutionModeSummary:
			var rows []json.RawMessage
			if err := json.Unmarshal([]byte(trimmedResult), &rows); err == nil {
				return fmt.Sprintf("execute_script completed successfully and returned %d row(s). The full row payload was already streamed to the client. Do not restate the rows; provide at most a brief summary. Execution mode: summary. This was an investigatory tool call, so a concise summary is sufficient for the next reasoning turn.", len(rows))
			}

			var record map[string]any
			if err := json.Unmarshal([]byte(trimmedResult), &record); err == nil {
				return "execute_script completed successfully and returned one structured record. The full payload was already streamed to the client. Do not restate the record; provide at most a brief summary. Execution mode: summary. This was an investigatory tool call, so a concise summary is sufficient for the next reasoning turn."
			}

			if len(trimmedResult) > 1000 {
				return fmt.Sprintf("execute_script completed successfully and returned a large textual payload (%d chars). The full payload was already streamed to the client. Do not restate it; provide at most a brief summary. Execution mode: summary. This was an investigatory tool call, so a concise summary is sufficient for the next reasoning turn.", len(trimmedResult))
			}

			return fmt.Sprintf("execute_script completed successfully. The full payload was already streamed to the client. Do not restate it; provide at most a brief summary. Execution mode: summary. This was an investigatory tool call, so a concise summary is sufficient for the next reasoning turn.")
		case ai.ReActToolResultExecutionModeFullSmallOnly:
			if len(trimmedResult) > 500 {
				return fmt.Sprintf("execute_script completed successfully and returned a large textual payload (%d chars). The full payload was already streamed to the client. Do not restate it; provide at most a brief summary. Execution mode: full_small_only. This was a real execution, so keep the next turn compact but preserve the essential facts.", len(trimmedResult))
			}
			return trimmedResult
		case ai.ReActToolResultExecutionModeFull:
			return trimmedResult
		default:
			var rows []json.RawMessage
			if err := json.Unmarshal([]byte(trimmedResult), &rows); err == nil {
				previewRows := rows
				if len(previewRows) > 10 {
					previewRows = previewRows[:10]
				}
				preview, _ := json.Marshal(previewRows)
				return fmt.Sprintf("execute_script returned %d row(s). Preview (first %d rows): %s", len(rows), len(previewRows), string(preview))
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(trimmedResult), &record); err == nil {
				preview, _ := json.Marshal(record)
				if len(preview) > 400 {
					preview = preview[:400]
				}
				return fmt.Sprintf("execute_script returned one structured record. Preview: %s", string(preview))
			}
			if len(trimmedResult) > 500 {
				return fmt.Sprintf("execute_script returned a large textual payload (%d chars). Preview: %q", len(trimmedResult), trimmedResult[:500])
			}
			return fmt.Sprintf("execute_script completed successfully. The full payload was already streamed to the client. Do not restate it; provide at most a brief summary. Execution mode: structured_summary. This was an investigatory tool call, so a concise summary is sufficient for the next reasoning turn.")
		}
	}

	return trimmedResult
}

func normalizeToolResultExecutionMode(mode ai.ReActToolResultExecutionMode) ai.ReActToolResultExecutionMode {
	switch mode {
	case ai.ReActToolResultExecutionModeSummary,
		ai.ReActToolResultExecutionModeStructuredSummary,
		ai.ReActToolResultExecutionModeFullSmallOnly,
		ai.ReActToolResultExecutionModeFull:
		return mode
	default:
		return ai.ReActToolResultExecutionModeStructuredSummary
	}
}

func selectToolResultExecutionMode(toolName string, finalToolResult bool) ai.ReActToolResultExecutionMode {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if isInspectionTool(name) {
		return ai.ReActToolResultExecutionModeFull
	}
	if isExecutionTool(name) {
		if finalToolResult {
			return ai.ReActToolResultExecutionModeSummary
		}
		return ai.ReActToolResultExecutionModeStructuredSummary
	}
	if finalToolResult {
		return ai.ReActToolResultExecutionModeSummary
	}
	return ai.ReActToolResultExecutionModeStructuredSummary
}

func isInspectionTool(toolName string) bool {
	switch toolName {
	case "git_status", "git_diff", "git_log", "git_branch", "git_checkout":
		return true
	case "read_file", "list_directory", "path_exists", "stat_path", "list_stores", "list_space_items", "read_space_config":
		return true
	case "search_web", "search_repo_files", "grep_repo_text":
		return true
	default:
		return false
	}
}

func isExecutionTool(toolName string) bool {
	switch toolName {
	case "execute_script", "run_script", "patch_file", "write_file", "copy_file", "move_file", "delete_file", "delete_directory", "git_commit", "git_pull", "git_push":
		return true
	default:
		return false
	}
}

func buildToolResultContext(toolName, outcome, evidence, guidance string) string {
	var builder strings.Builder
	builder.WriteString("Tool result context:")
	builder.WriteString(fmt.Sprintf("\n- Tool: %s", strings.TrimSpace(toolName)))
	builder.WriteString(fmt.Sprintf("\n- Outcome: %s", strings.TrimSpace(outcome)))
	builder.WriteString(fmt.Sprintf("\n- Evidence: %s", strings.TrimSpace(evidence)))
	builder.WriteString(fmt.Sprintf("\n- Guidance: %s", strings.TrimSpace(guidance)))
	return builder.String()
}

func ExtractToolResultText(response any) string {
	switch v := response.(type) {
	case string:
		return v
	case map[string]any:
		if result, ok := v["result"].(string); ok {
			return result
		}
		if result, ok := v["tool_result"].(string); ok {
			return result
		}
		bytes, _ := json.Marshal(v)
		return string(bytes)
	default:
		bytes, _ := json.Marshal(response)
		return string(bytes)
	}
}
