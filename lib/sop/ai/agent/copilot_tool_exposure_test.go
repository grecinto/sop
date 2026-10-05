package agent

import "testing"

func TestNativeToolExposureDelegatesCodingButKeepsSpecializedAutomation(t *testing.T) {
	state := &TaskContextClassification{
		Domain:             "coding_workflow",
		AutomationFamilies: []string{"postgres", "splunk", "git", "shell"},
		AskClassification:  &AskTaskContext{Domains: []string{"coding_workflow"}},
	}

	delegated := buildNativeToolExposure(state, true)
	for _, tool := range []string{"git_status", "git_commit", "execute_local_command", "read_file", "write_file"} {
		if delegated.allowed[tool] {
			t.Errorf("coding-owned tool %q should be hidden while Cline owns coding execution", tool)
		}
	}
	for _, tool := range []string{"postgres_query", "postgres_execute", "splunk_search", "splunk_create_saved_search"} {
		if !delegated.allowed[tool] {
			t.Errorf("specialized automation tool %q should remain available", tool)
		}
	}

	native := buildNativeToolExposure(state, false)
	if !native.allowed["git_status"] || !native.allowed["execute_local_command"] || !native.allowed["write_file"] {
		t.Fatal("native coding fallback should retain Git and command-line tools")
	}
}
