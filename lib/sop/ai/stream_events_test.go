package ai

import (
	"errors"
	"testing"
)

func TestBuildSharedReasoningEvents_ProviderCompatibleShape(t *testing.T) {
	args := map[string]any{"name": "John"}
	hint := &ToolProgressHint{Status: "progressing", CompletionDelta: 0.25}

	toolCall := BuildToolCallEvent("lookup_user", args, 2)
	if toolCall["tool"] != "lookup_user" || toolCall["args"].(map[string]any)["name"] != "John" || toolCall["iteration"] != 2 {
		t.Fatalf("unexpected tool_call payload: %#v", toolCall)
	}

	toolResult := BuildToolResultEvent("lookup_user", args, "John is in the users store.", hint, 2, "", 0)
	if toolResult["tool"] != "lookup_user" || toolResult["result"] != "John is in the users store." || toolResult["result_chars"] != len("John is in the users store.") {
		t.Fatalf("unexpected tool_result payload: %#v", toolResult)
	}
	if gotHint, ok := toolResult["progress_hint"].(*ToolProgressHint); !ok || gotHint.Status != "progressing" {
		t.Fatalf("expected progress hint in tool_result payload, got %#v", toolResult)
	}

	toolError := BuildToolErrorEvent("lookup_user", args, errors.New("network timeout"), 2)
	if toolError["tool"] != "lookup_user" || toolError["error"] != "network timeout" || toolError["iteration"] != 2 {
		t.Fatalf("unexpected tool_error payload: %#v", toolError)
	}

	assistant := BuildAssistantMessageEvent("commentary", "Checking the store.")
	if assistant["phase"] != "commentary" || assistant["text"] != "Checking the store." {
		t.Fatalf("unexpected assistant_message payload: %#v", assistant)
	}

	streamAssistant := BuildStreamingAssistantMessageEvent("commentary", "Checking", "msg_1", "assistant_output", "delta")
	if streamAssistant["streaming"] != true || streamAssistant["stream_state"] != "delta" || streamAssistant["item_id"] != "msg_1" {
		t.Fatalf("unexpected streaming assistant payload: %#v", streamAssistant)
	}

	streamTool := BuildStreamingToolCallEvent("lookup_user", "fc_1", "call_1", "done")
	if streamTool["streaming"] != true || streamTool["tool"] != "lookup_user" || streamTool["call_id"] != "call_1" || streamTool["stream_state"] != "done" {
		t.Fatalf("unexpected streaming tool payload: %#v", streamTool)
	}
}

// TestBuildToolResultEvent_FileBackedMetadata verifies Phase 2.2 file-backed metadata
func TestBuildToolResultEvent_FileBackedMetadata(t *testing.T) {
	// Test without file-backing (inline result)
	inlineResult := BuildToolResultEvent("execute_test", map[string]any{"path": "."}, "All tests passed", nil, 1, "", 0)
	if _, hasFile := inlineResult["result_file"]; hasFile {
		t.Error("inline result should not have result_file field")
	}
	if _, hasBacked := inlineResult["file_backed"]; hasBacked {
		t.Error("inline result should not have file_backed field")
	}

	// Test with file-backing (large result)
	fileBackedResult := BuildToolResultEvent("execute_test", map[string]any{"path": "."},
		"Test output summary...", nil, 1, "/tmp/sop-tool-results/session-123/execute_test-abc123.txt", 54321)

	if fileBackedResult["result_file"] != "/tmp/sop-tool-results/session-123/execute_test-abc123.txt" {
		t.Errorf("expected result_file in payload, got %v", fileBackedResult["result_file"])
	}
	if fileBackedResult["result_size"] != 54321 {
		t.Errorf("expected result_size=54321, got %v", fileBackedResult["result_size"])
	}
	if fileBackedResult["file_backed"] != true {
		t.Errorf("expected file_backed=true, got %v", fileBackedResult["file_backed"])
	}
	if fileBackedResult["result"] != "Test output summary..." {
		t.Errorf("expected summary in result field, got %v", fileBackedResult["result"])
	}
}
