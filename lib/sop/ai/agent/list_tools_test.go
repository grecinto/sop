package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestListTools(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()

	// Inject session payload
	payload := &ai.SessionPayload{
		CurrentDB: "system",
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)

	// Invoke valid tool
	output, err := agent.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed: %v", err)
	}

	// Unwrap ToolResultEnvelope if present
	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err == nil && len(envelope.ToolResult) > 0 {
		var unwrapped string
		if err := json.Unmarshal(envelope.ToolResult, &unwrapped); err == nil {
			output = unwrapped
		}
	}

	// Output format is a simple list, not a table
	if strings.Contains(output, "| Command | Arguments | Description |") {
		t.Errorf("Output should NOT contain table header (format changed to list). Got: %s", output)
	}

	expected := "/list_databases: Lists all available databases."
	if !strings.Contains(output, expected) {
		t.Errorf("Output should contain list_databases row.\nExpected to contain: %s\nGot:\n%s", expected, output)
	}
}

func TestListToolsIncludesClearCommand(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	agent.registerTools(ctx)

	output, err := agent.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed: %v", err)
	}

	// Unwrap ToolResultEnvelope if present
	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err == nil && len(envelope.ToolResult) > 0 {
		var unwrapped string
		if err := json.Unmarshal(envelope.ToolResult, &unwrapped); err == nil {
			output = unwrapped
		}
	}

	if !strings.Contains(output, "/clear") {
		t.Fatalf("expected list_tools output to include /clear, got:\n%s", output)
	}
}

func TestListToolsFormatterIsSharedAcrossServiceAndToolPaths(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{CurrentDB: "system"}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)

	toolOutput, err := agent.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools tool execution failed: %v", err)
	}

	// Unwrap ToolResultEnvelope if present
	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(toolOutput), &envelope); err == nil && len(envelope.ToolResult) > 0 {
		var unwrapped string
		if err := json.Unmarshal(envelope.ToolResult, &unwrapped); err == nil {
			toolOutput = unwrapped
		}
	}

	svc := &Service{registry: map[string]ai.Agent[map[string]any]{"copilot": agent}, session: &RunnerSession{}}
	serviceOutput, handled, err := svc.handleSessionCommand(ctx, "/list_tools", nil)
	if err != nil {
		t.Fatalf("service list_tools failed: %v", err)
	}
	if !handled {
		t.Fatal("service list_tools should be handled")
	}

	if !strings.Contains(toolOutput, "### Available Tools") || !strings.Contains(serviceOutput, "### Available Tools") {
		t.Fatalf("both list_tools outputs should include the shared header, got tool=%q service=%q", toolOutput, serviceOutput)
	}
	if !strings.Contains(toolOutput, "/list_databases") || !strings.Contains(serviceOutput, "/list_databases") {
		t.Fatalf("both list_tools outputs should share the same slash-prefixed entry, got tool=%q service=%q", toolOutput, serviceOutput)
	}
	if strings.Contains(toolOutput, "\n- `/") || strings.Contains(serviceOutput, "\n- `/") {
		t.Fatalf("list_tools output should not use bullet prefixes in front of slash commands, got tool=%q service=%q", toolOutput, serviceOutput)
	}
}

func TestListToolsFormatterUsesPlainSlashCommandLines(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})
	agent.registerTools(ctx)

	output, err := agent.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed: %v", err)
	}

	// Unwrap ToolResultEnvelope if present
	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err == nil && len(envelope.ToolResult) > 0 {
		var unwrapped string
		if err := json.Unmarshal(envelope.ToolResult, &unwrapped); err == nil {
			output = unwrapped
		}
	}

	if strings.Contains(output, "\n- `/") {
		t.Fatalf("expected slash-prefixed tool lines without bullet prefixes, got:\n%s", output)
	}
	if !strings.Contains(output, "\n/list_databases:") {
		t.Fatalf("expected /list_databases line in output, got:\n%s", output)
	}
}

func TestServiceListToolsStreamsContentWhenEventStreamerIsPresent(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	ctx = context.WithValue(ctx, "session_payload", &ai.SessionPayload{CurrentDB: "system"})
	agent.registerTools(ctx)

	var streamed []string
	ctx = context.WithValue(ctx, ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventType != "content" {
			return
		}
		if msg, ok := payload.(string); ok {
			streamed = append(streamed, msg)
		}
	})

	svc := &Service{registry: map[string]ai.Agent[map[string]any]{"copilot": agent}, session: &RunnerSession{}}
	out, handled, err := svc.handleSessionCommand(ctx, "/list_tools", nil)
	if err != nil {
		t.Fatalf("service list_tools failed: %v", err)
	}
	if !handled {
		t.Fatal("service list_tools should be handled")
	}
	if len(streamed) == 0 {
		t.Fatalf("expected list_tools content to be streamed, got output=%q", out)
	}
	if !strings.Contains(streamed[0], "### Available Tools") {
		t.Fatalf("expected streamed content to include the tool inventory header, got %q", streamed[0])
	}
	if !strings.Contains(out, "### Available Tools") {
		t.Fatalf("expected the handler to return the tool inventory text alongside the stream, got %q", out)
	}
}

func TestListTools_ReRegistersAutomationToolsAfterPolicyEnablement(t *testing.T) {
	SetLocalDesktopAutomationEnabled(false)
	SetAutomationPolicy(DefaultAutomationPolicy())
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	SetLocalDesktopAutomationEnabled(true)
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})

	// Re-register tools after policy change to pick up automation tools
	agent.registerTools(ctx)

	output, err := agent.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed after enabling automation: %v", err)
	}

	// Unwrap ToolResultEnvelope if present
	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err == nil && len(envelope.ToolResult) > 0 {
		// If result was stored to file, read the file
		if envelope.FileMetadata != nil && envelope.FileMetadata.FilePath != "" {
			fileContent, err := os.ReadFile(envelope.FileMetadata.FilePath)
			if err == nil {
				output = string(fileContent)
			}
		} else {
			var unwrapped string
			if err := json.Unmarshal(envelope.ToolResult, &unwrapped); err == nil {
				output = unwrapped
			}
		}
	}

	if !strings.Contains(output, "/execute_local_command") {
		t.Fatalf("expected automation tools to appear in list_tools output after policy enablement, got:\n%s", output)
	}
}

func TestListTools_StoresRoutingExposesStoresProtocolTools(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{
		CurrentDB: "system",
		Variables: map[string]any{
			"RoutingState": &TaskContextClassification{
				Domain: StoresDomain,
				Layers: []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
			},
		},
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)
	tools, err := agent.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["execute_script"] || !names["list_stores"] {
		t.Fatalf("expected Stores routing to expose execute_script and list_stores, got %#v", names)
	}
	if !names["select"] || !names["explain_join"] {
		t.Fatalf("expected Stores routing to expose registered Stores read tools, got %#v", names)
	}
	for _, required := range []string{"begin_tx", "open_store", "scan", "filter", "project", "join_right", "commit_tx"} {
		if !names[required] {
			t.Fatalf("expected Stores routing to expose %s for native read pipelines, got %#v", required, names)
		}
	}
}

func TestListTools_CodingWorkflowRouting_HidesStoresSpacesPostgresAndSplunkTools(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, AllowPostgresRead: true, AllowPostgresWrite: true, AllowSplunkRead: true, AllowSplunkWrite: true, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{
		CurrentDB: "system",
		Variables: map[string]any{
			"RoutingState": &TaskContextClassification{
				Domain:         "coding_workflow",
				RoutingGate:    RoutingGateDiscovery,
				CapabilityHint: "investigation",
				AskClassification: &AskTaskContext{
					Domains:       []string{"coding_workflow"},
					Phase:         "investigation",
					PhasePlan:     []string{"investigation", "planning", "execution"},
					Intent:        "generate_code",
					CRUD:          []string{"R", "U"},
					OriginalQuery: "fix failing tests",
				},
			},
		},
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)
	tools, err := agent.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}

	for _, forbidden := range []string{"postgres_query", "postgres_list_tables", "postgres_execute", "splunk_search", "splunk_list_saved_searches", "splunk_create_saved_search", "splunk_update_saved_search", "splunk_delete_saved_search", "list_stores", "execute_script", "mint_to_space", "read_space_config"} {
		if names[forbidden] {
			t.Fatalf("did not expect coding workflow routing to expose %s, got %#v", forbidden, names)
		}
	}
	for _, required := range []string{"execute_local_command", "search_repo_files", "grep_repo_text", "run_repo_tests", "git_status", "read_tool_result", "read_tool_result_page", "grep_tool_result"} {
		if !names[required] {
			t.Fatalf("expected coding workflow routing to expose %s, got %#v", required, names)
		}
	}
}

func TestListTools_SpacesRoutingHidesStoresProtocolTools(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{
		CurrentDB: "system",
		Variables: map[string]any{
			"RoutingState": &TaskContextClassification{
				Domain: SpacesDomain,
				Layers: []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
			},
		},
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)
	tools, err := agent.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if names["execute_script"] || names["list_stores"] || names["scan"] {
		t.Fatalf("expected Spaces routing to hide Stores protocol tools, got %#v", names)
	}
	if !names["read_space_config"] {
		t.Fatalf("expected Spaces routing to expose registered Spaces read tools, got %#v", names)
	}
}

func TestListTools_StoresReadRoutingHidesMutationTools(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{
		CurrentDB: "system",
		Variables: map[string]any{
			"RoutingState": &TaskContextClassification{
				Domain: StoresDomain,
				Layers: []LayerInfo{{Name: "Single-Domain", CRUD: []string{"R"}}},
			},
		},
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)
	tools, err := agent.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if names["add"] || names["update"] || names["delete"] {
		t.Fatalf("expected read-only Stores routing to hide mutation tools, got %#v", names)
	}
	if !names["manage_transaction"] {
		t.Fatalf("expected foundational Stores transaction tool to remain available, got %#v", names)
	}
}

func TestListTools_StoresUpdateRoutingExposesOnlyUpdateMutation(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{
		CurrentDB: "system",
		Variables: map[string]any{
			"RoutingState": &TaskContextClassification{
				Domain: StoresDomain,
				Layers: []LayerInfo{{Name: "Single-Domain", CRUD: []string{"U"}}},
			},
		},
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)
	tools, err := agent.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["update"] || names["add"] || names["delete"] {
		t.Fatalf("expected update-only Stores routing to expose only update among store mutation tools, got %#v", names)
	}
	if !names["execute_script"] || !names["list_stores"] {
		t.Fatalf("expected foundational Stores protocol tools to remain available, got %#v", names)
	}
}

func TestListTools_SpacesUpdateRoutingExposesSpaceMutationTools(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{
		CurrentDB: "system",
		Variables: map[string]any{
			"RoutingState": &TaskContextClassification{
				Domain: SpacesDomain,
				Layers: []LayerInfo{{Name: "Single-Domain", CRUD: []string{"U"}}},
			},
		},
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.registerTools(ctx)
	tools, err := agent.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["mint_to_space"] || !names["update_space_config"] {
		t.Fatalf("expected Spaces update routing to expose mutation tools, got %#v", names)
	}
	if names["execute_script"] || names["list_stores"] {
		t.Fatalf("expected Spaces update routing to keep Stores protocol tools hidden, got %#v", names)
	}
}

func TestListTools_LowRiskExposedToolsUseJSONSchemas(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: true, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	ctx = context.WithValue(ctx, "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	agent.registerTools(ctx)
	tools, err := agent.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	expectedJSON := map[string]bool{
		"list_databases":             false,
		"list_tools":                 false,
		"list_stores":                false,
		"begin_tx":                   false,
		"commit_tx":                  false,
		"rollback_tx":                false,
		"open_store":                 false,
		"select":                     false,
		"scan":                       false,
		"filter":                     false,
		"sort":                       false,
		"project":                    false,
		"limit":                      false,
		"join_right":                 false,
		"explain_join":               false,
		"add":                        false,
		"update":                     false,
		"delete":                     false,
		"manage_transaction":         false,
		"list_scripts":               false,
		"create_script":              false,
		"save_script":                false,
		"get_script_details":         false,
		"save_step":                  false,
		"insert_step":                false,
		"delete_step":                false,
		"update_step":                false,
		"reorder_steps":              false,
		"save_last_step":             false,
		"refactor_last_interaction":  false,
		"mint_to_space":              false,
		"delete_space":               false,
		"enrich_space":               false,
		"update_space_config":        false,
		"read_space_config":          false,
		"vectorize_space":            false,
		"vectorize_space_categories": false,
		"vectorize_space_items":      false,
		"execute_local_command":      false,
		"send_email":                 false,
		"search_space":               false,
		"handoff_to_avatar":          false,
		"conclude_topic":             false,
	}

	for _, tool := range tools {
		if _, ok := expectedJSON[tool.Name]; !ok {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(tool.Schema), "{") {
			t.Fatalf("expected %s schema to be JSON, got %q", tool.Name, tool.Schema)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(tool.Schema), &decoded); err != nil {
			t.Fatalf("expected %s schema to decode as JSON: %v\nSchema: %s", tool.Name, err, tool.Schema)
		}
		expectedJSON[tool.Name] = true
	}

	for name, found := range expectedJSON {
		if !found {
			t.Fatalf("expected tool %s to be exposed with a JSON schema", name)
		}
	}
}

func TestListToolsIncludesCommandShortcuts(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{CurrentDB: "system"}
	ctx = context.WithValue(ctx, "session_payload", payload)

	agent.ensureCapabilityRegistry()
	agent.registerTools(ctx)

	output, err := agent.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed: %v", err)
	}

	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err == nil && len(envelope.ToolResult) > 0 {
		var unwrapped string
		if err := json.Unmarshal(envelope.ToolResult, &unwrapped); err == nil {
			output = unwrapped
		}
	}

	if !strings.Contains(output, "### Command Shortcuts") {
		t.Errorf("list_tools output should include Command Shortcuts section.\nGot:\n%s", output)
	}

	expectedShortcuts := []string{"/c", "/cmd", "/shell", "/exec"}
	for _, shortcut := range expectedShortcuts {
		if !strings.Contains(output, shortcut+" <command>:") {
			t.Errorf("list_tools output should include shortcut %s.\nGot:\n%s", shortcut, output)
		}
	}

	if !strings.Contains(output, "Example:") && !strings.Contains(output, "/cmd ls") {
		t.Errorf("list_tools output should include example usage.\nGot:\n%s", output)
	}
}

func TestEnsureCapabilityRegistryDoesNotRegisterPhaseInstancesGlobally(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.ensureCapabilityRegistry()
	caps := agent.capabilityRegistry.ListAll()
	ids := make(map[string]bool, len(caps))
	for _, cap := range caps {
		ids[cap.ID()] = true
	}

	for id := range ids {
		if strings.HasPrefix(id, "phase_") {
			t.Fatalf("phase capability %s should be created per Ask turn, got global registry IDs %v", id, ids)
		}
	}
}
