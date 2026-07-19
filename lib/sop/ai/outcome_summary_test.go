package ai

import "testing"

func TestExtractListStoresFacts_FromStructuredPayload(t *testing.T) {
	raw := `{"database":"sales","stores":[{"name":"users","schema":{"key":"string","first_name":"string"},"relations":[{"source_fields":["key"],"target_store":"users_orders","target_fields":["user_id"]}]},{"name":"orders","schema":{"total_amount":"number"}}]}`
	facts := extractListStoresFacts(raw)
	if len(facts) != 3 {
		t.Fatalf("expected 3 facts, got %+v", facts)
	}
	if facts[0] != "list_stores confirmed users schema={first_name: string, key: string}" {
		t.Fatalf("unexpected first fact: %q", facts[0])
	}
	if facts[1] != "list_stores confirmed users relations=[users_orders(user_id->key)]" {
		t.Fatalf("unexpected relation fact: %q", facts[1])
	}
	if facts[2] != "list_stores confirmed orders schema={total_amount: number}" {
		t.Fatalf("unexpected orders fact: %q", facts[2])
	}
}

func TestExtractListStoresFacts_FromEnvelopePayload(t *testing.T) {
	raw := `{"tool_result":{"database":"sales","stores":[{"name":"users","schema":{"first_name":"string"}}]},"progress_hint":{"status":"progressing"}}`
	facts := extractListStoresFacts(raw)
	if len(facts) != 1 || facts[0] != "list_stores confirmed users schema={first_name: string}" {
		t.Fatalf("unexpected facts from envelope: %+v", facts)
	}
}

func TestSummarizeSuccessfulToolResult_CommandLineToolsSkipSummarization(t *testing.T) {
	// Command-line/automation tools should return nil (no summarization)
	// so their raw output is preserved for ToolResultStorage handling
	commandLineTools := []string{
		"execute_local_command",
		"search_repo_files",
		"grep_repo_text",
		"run_repo_tests",
		"git_status",
		"git_diff",
		"read_file",
		"write_file",
		"postgres_query",
		"splunk_search",
	}

	for _, toolName := range commandLineTools {
		result := ReActToolResult{
			Name:   toolName,
			Result: "This is a long command output that should NOT be truncated to 180 chars because command-line tools preserve raw output for temp file storage with grep/awk/tail analysis.",
			Args:   map[string]any{},
		}
		facts := SummarizeSuccessfulToolResult(result)
		if facts != nil {
			t.Fatalf("expected %s to skip summarization (return nil), got %+v", toolName, facts)
		}
	}
}

func TestSummarizeSuccessfulToolResult_StoreToolsGetSummarized(t *testing.T) {
	// Store/Space tools should still get semantic summarization
	result := ReActToolResult{
		Name:   "list_stores",
		Result: `{"database":"sales","stores":[{"name":"users","schema":{"first_name":"string"}}]}`,
		Args:   map[string]any{},
	}
	facts := SummarizeSuccessfulToolResult(result)
	if len(facts) != 1 {
		t.Fatalf("expected list_stores to be summarized, got %+v", facts)
	}
	if facts[0] != "list_stores confirmed users schema={first_name: string}" {
		t.Fatalf("unexpected summary: %q", facts[0])
	}
}
