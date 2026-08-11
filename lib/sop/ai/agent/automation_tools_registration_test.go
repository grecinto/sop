package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestAutomationToolsAreDisabledByDefault(t *testing.T) {
	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	for _, tool := range ag.registry.List() {
		if tool.Name == "execute_local_command" || tool.Name == "send_email" {
			t.Fatalf("expected automation tools to be disabled by default, but saw %q", tool.Name)
		}
	}
}

func TestAutomationToolsCanBeEnabledForDaemonMode(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["execute_local_command"] {
		t.Fatalf("expected execute_local_command to be registered when local desktop automation is enabled")
	}
	if !found["send_email"] {
		t.Fatalf("expected send_email to be registered when local desktop automation is enabled")
	}
}

func TestAutomationPolicyAllowsScopedShellAndEmailTools(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: true, RequireApproval: true, DefaultTimeoutSeconds: 300, MaxTimeoutSeconds: 900})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["execute_local_command"] {
		t.Fatalf("expected shell automation tool to be registered when policy allows it")
	}
	if !found["send_email"] {
		t.Fatalf("expected email automation tool to be registered when policy allows it")
	}
}

func TestAutomationPolicyRejectsUnsignedShellExecution(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	_, err := ag.toolExecuteLocalCommand(context.Background(), map[string]any{"command": "pwd", "timeout_seconds": 300})
	if err == nil {
		t.Fatal("expected tool execution to be blocked by automation policy when timeout exceeds policy")
	}
}

func TestSensitiveCommandsRequireApprovalAndPasswordPrompt(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: true, DefaultTimeoutSeconds: 600, MaxTimeoutSeconds: 1800})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	out, err := ag.toolExecuteLocalCommand(context.Background(), map[string]any{"command": "sudo rm -rf /tmp/demo", "reasoning": "clean up temporary directory"})
	if err != nil {
		t.Fatalf("toolExecuteLocalCommand() error = %v", err)
	}
	if !strings.Contains(out, "<<<ACTION_REQUIRED:") {
		t.Fatalf("expected action-required marker, got %q", out)
	}
	if !strings.Contains(out, `"requires_approval":true`) {
		t.Fatalf("expected approval flag in action payload, got %q", out)
	}
	if !strings.Contains(out, `"requires_password_prompt":true`) {
		t.Fatalf("expected password prompt flag in action payload, got %q", out)
	}
	if !strings.Contains(out, `"approval_reason":`) {
		t.Fatalf("expected approval reason in action payload, got %q", out)
	}
}

func TestAutomationToolsRegisterRepoSearchHelpers(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["search_repo_files"] {
		t.Fatalf("expected search_repo_files to be registered")
	}
	if !found["grep_repo_text"] {
		t.Fatalf("expected grep_repo_text to be registered")
	}
}

func TestAutomationToolsRegisterGitHelpers(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	for _, toolName := range []string{"git_status", "git_diff", "git_log", "git_branch", "git_checkout", "git_commit", "git_pull", "git_push"} {
		if !found[toolName] {
			t.Fatalf("expected %s to be registered", toolName)
		}
	}
}

func TestAutomationToolsRegisterPostgresAndSplunkAdapters(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["postgres_query"] {
		t.Fatalf("expected postgres_query to be registered")
	}
	if !found["splunk_search"] {
		t.Fatalf("expected splunk_search to be registered")
	}
}

func TestAutomationToolResultsStreamToEventStreamer(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		return `{"stdout":"one\ntwo\n","stderr":"","exit_code":0}`, nil
	})
	defer SetLocalCommandExecutor(nil)

	var gotEvents []struct {
		typ     string
		payload any
	}
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		gotEvents = append(gotEvents, struct {
			typ     string
			payload any
		}{typ: eventType, payload: payload})
	})

	ag := &CopilotAgent{}
	out, err := ag.toolExecuteLocalCommand(ctx, map[string]any{"command": "printf 'one\ntwo'", "reasoning": "show output", "timeout_seconds": 60})
	if err != nil {
		t.Fatalf("toolExecuteLocalCommand() error = %v", err)
	}
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Fatalf("toolExecuteLocalCommand() output = %q", out)
	}

	// Command output should NOT be streamed to chat history
	// It should only go to the Terminal window pane
	foundContent := false
	for _, event := range gotEvents {
		if event.typ == "content" {
			foundContent = true
			t.Errorf("unexpected content event in chat history: automation tool execution should not emit content events; payload=%#v", event.payload)
		}
	}
	if foundContent {
		t.Fatal("automation tool execution should not emit content events (output goes to Terminal pane only)")
	}
}

func TestPostgresQueryUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolPostgresQuery(context.Background(), map[string]any{"query": "select 1", "connection_string": "postgres://user:pass@localhost:5432/app"})
	if err != nil {
		t.Fatalf("toolPostgresQuery() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolPostgresQuery() output = %q", out)
	}
	if !strings.Contains(received.Command, "psql") {
		t.Fatalf("expected psql command, got %q", received.Command)
	}
	if !strings.Contains(received.Command, "select 1") {
		t.Fatalf("expected query in command, got %q", received.Command)
	}
}

func TestExecute_WrapsLargeCommandResultsWithStoredEnvelope(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		return `{"stdout":"` + strings.Repeat("large command output line\n", 400) + `","stderr":"","exit_code":0}`, nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{registry: NewRegistry()}
	ag.registerTools(context.Background())

	storage, err := NewToolResultStorage("execute-large-command")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	ctx = context.WithValue(ctx, SessionPayloadKey, &ai.SessionPayload{SessionID: "execute-large-command"})

	out, err := ag.Execute(ctx, "execute_local_command", map[string]any{"command": "printf 'x'", "timeout_seconds": 60})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("expected file-backed tool result envelope, got %q: %v", out, err)
	}
	if envelope.FileMetadata == nil || strings.TrimSpace(envelope.FileMetadata.FilePath) == "" {
		t.Fatalf("expected file metadata for large command result, got %+v", envelope.FileMetadata)
	}
	if !strings.Contains(string(envelope.ToolResult), "Use the saved file to confirm the outcome based on your request") {
		t.Fatalf("expected stored summary guidance, got %s", string(envelope.ToolResult))
	}
}

func TestPostgresQueryUsesNativeAPIWhenEnabled(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	previous := postgresQueryExecutor
	postgresQueryExecutor = func(ctx context.Context, cfg postgresConfig, query string, allowWrite bool) (string, error) {
		if query != "select 1" {
			t.Fatalf("expected select query, got %q", query)
		}
		if allowWrite {
			t.Fatal("expected read-only mode")
		}
		if cfg.Database != "appdb" {
			t.Fatalf("expected appdb database, got %q", cfg.Database)
		}
		return `{"status":"ok","rows":[],"mode":"read"}`, nil
	}
	defer func() { postgresQueryExecutor = previous }()

	ag := &CopilotAgent{}
	out, err := ag.toolPostgresQuery(context.Background(), map[string]any{"query": "select 1", "use_native_api": true, "host": "db.internal", "port": "5433", "database": "appdb", "user": "appuser", "password": "secret"})
	if err != nil {
		t.Fatalf("toolPostgresQuery() error = %v", err)
	}
	if !strings.Contains(out, `"status":"ok"`) {
		t.Fatalf("expected native API result, got %q", out)
	}
}

func TestPostgresQueryAppliesQueryLimit(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	previous := postgresQueryExecutor
	postgresQueryExecutor = func(ctx context.Context, cfg postgresConfig, query string, allowWrite bool) (string, error) {
		if !strings.Contains(query, "LIMIT 25") {
			t.Fatalf("expected query limit to be appended, got %q", query)
		}
		return `{"status":"ok","rows":[],"mode":"read"}`, nil
	}
	defer func() { postgresQueryExecutor = previous }()

	ag := &CopilotAgent{}
	_, err := ag.toolPostgresQuery(context.Background(), map[string]any{"query": "select 1", "use_native_api": true, "limit": 25})
	if err != nil {
		t.Fatalf("toolPostgresQuery() error = %v", err)
	}
}

func TestPostgresDescribeTableUsesNativeAPIWhenEnabled(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	previous := postgresQueryExecutor
	postgresQueryExecutor = func(ctx context.Context, cfg postgresConfig, query string, allowWrite bool) (string, error) {
		if !strings.Contains(query, "information_schema.columns") {
			t.Fatalf("expected schema inspection query, got %q", query)
		}
		if !strings.Contains(query, "users") {
			t.Fatalf("expected table name in schema inspection query, got %q", query)
		}
		return `{"status":"ok","rows":[],"mode":"read"}`, nil
	}
	defer func() { postgresQueryExecutor = previous }()

	ag := &CopilotAgent{}
	_, err := ag.toolPostgresDescribeTable(context.Background(), map[string]any{"table": "users", "use_native_api": true})
	if err != nil {
		t.Fatalf("toolPostgresDescribeTable() error = %v", err)
	}
}

func TestSplunkSearchUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolSplunkSearch(context.Background(), map[string]any{"query": "index=main error", "host": "https://splunk.example", "port": "8089", "username": "admin", "password": "secret", "max_results": 5})
	if err != nil {
		t.Fatalf("toolSplunkSearch() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolSplunkSearch() output = %q", out)
	}
	if !strings.Contains(received.Command, "splunk search") {
		t.Fatalf("expected splunk search command, got %q", received.Command)
	}
	if !strings.Contains(received.Command, "index=main") {
		t.Fatalf("expected index in command, got %q", received.Command)
	}
}

func TestPostgresQueryUsesEnvironmentConfigWhenArgsOmitted(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	t.Setenv("POSTGRES_HOST", "db.internal")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_DATABASE", "appdb")
	t.Setenv("POSTGRES_USER", "appuser")
	t.Setenv("POSTGRES_PASSWORD", "secret")

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolPostgresQuery(context.Background(), map[string]any{"query": "select 1"})
	if err != nil {
		t.Fatalf("toolPostgresQuery() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolPostgresQuery() output = %q", out)
	}
	if !strings.Contains(received.Command, "postgresql://appuser:secret@db.internal:5433/appdb") {
		t.Fatalf("expected environment-based connection string, got %q", received.Command)
	}
}

func TestPostgresQueryRejectsWriteQueries(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	_, err := ag.toolPostgresQuery(context.Background(), map[string]any{"query": "DELETE FROM users"})
	if err == nil {
		t.Fatal("expected write query to be rejected")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got %v", err)
	}
}

func TestSplunkSearchUsesEnvironmentConfigWhenArgsOmitted(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	t.Setenv("SPLUNK_HOST", "splunk.internal")
	t.Setenv("SPLUNK_PORT", "8089")
	t.Setenv("SPLUNK_USERNAME", "admin")
	t.Setenv("SPLUNK_PASSWORD", "secret")
	t.Setenv("SPLUNK_INDEX", "main")

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolSplunkSearch(context.Background(), map[string]any{"query": "error"})
	if err != nil {
		t.Fatalf("toolSplunkSearch() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolSplunkSearch() output = %q", out)
	}
	if !strings.Contains(received.Command, "splunk search") {
		t.Fatalf("expected splunk search command, got %q", received.Command)
	}
	if !strings.Contains(received.Command, "splunk.internal") {
		t.Fatalf("expected environment-based host, got %q", received.Command)
	}
	if !strings.Contains(received.Command, "main") {
		t.Fatalf("expected environment-based index, got %q", received.Command)
	}
}

func TestSplunkSearchUsesNativeAPIWhenEnabled(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	previous := splunkSearchExecutor
	splunkSearchExecutor = func(ctx context.Context, cfg splunkConfig, query string) (string, error) {
		if query != "index=main error" {
			t.Fatalf("expected search query, got %q", query)
		}
		if cfg.Index != "main" {
			t.Fatalf("expected main index, got %q", cfg.Index)
		}
		return `{"status":"ok","results":[]}`, nil
	}
	defer func() { splunkSearchExecutor = previous }()

	ag := &CopilotAgent{}
	out, err := ag.toolSplunkSearch(context.Background(), map[string]any{"query": "index=main error", "use_native_api": true, "host": "https://splunk.example", "index": "main", "max_results": 5})
	if err != nil {
		t.Fatalf("toolSplunkSearch() error = %v", err)
	}
	if !strings.Contains(out, `"status":"ok"`) {
		t.Fatalf("expected native API result, got %q", out)
	}
}

func TestSplunkSearchSupportsSavedSearchSemantics(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	previous := splunkSearchExecutor
	splunkSearchExecutor = func(ctx context.Context, cfg splunkConfig, query string) (string, error) {
		if !strings.Contains(query, "savedsearch") {
			t.Fatalf("expected saved-search semantics, got %q", query)
		}
		return `{"status":"ok","results":[]}`, nil
	}
	defer func() { splunkSearchExecutor = previous }()

	ag := &CopilotAgent{}
	_, err := ag.toolSplunkSearch(context.Background(), map[string]any{"query": "errors", "use_native_api": true, "saved_search_name": "errors"})
	if err != nil {
		t.Fatalf("toolSplunkSearch() error = %v", err)
	}
}

func TestPostgresExecuteRequiresWritePermission(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, AllowPostgresRead: true, AllowPostgresWrite: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	_, err := ag.toolPostgresExecute(context.Background(), map[string]any{"statement": "INSERT INTO users (id) VALUES (1)"})
	if err == nil {
		t.Fatal("expected postgres write operation to be rejected without write permission")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Fatalf("expected write-permission error, got %v", err)
	}
}

func TestSplunkCreateSavedSearchUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, AllowSplunkRead: true, AllowSplunkWrite: true, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolSplunkCreateSavedSearch(context.Background(), map[string]any{"name": "errors", "search": "index=main error", "host": "splunk.internal", "port": "8089", "username": "admin", "password": "secret"})
	if err != nil {
		t.Fatalf("toolSplunkCreateSavedSearch() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolSplunkCreateSavedSearch() output = %q", out)
	}
	if !strings.Contains(received.Command, "saved/searches") {
		t.Fatalf("expected saved-search management command, got %q", received.Command)
	}
	if !strings.Contains(received.Command, "errors") {
		t.Fatalf("expected saved search name in command, got %q", received.Command)
	}
}

func TestGitStatusUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolGitStatus(context.Background(), map[string]any{"path": "/tmp/repo"})
	if err != nil {
		t.Fatalf("toolGitStatus() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolGitStatus() output = %q", out)
	}
	if !strings.Contains(received.Command, "git --no-pager") || !strings.Contains(received.Command, "status") {
		t.Fatalf("expected git status command, got %q", received.Command)
	}
}

func TestSearchRepoFilesUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolSearchRepoFiles(context.Background(), map[string]any{"path": "/tmp/repo", "pattern": "*.go", "max_results": 3})
	if err != nil {
		t.Fatalf("toolSearchRepoFiles() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolSearchRepoFiles() output = %q", out)
	}
	if !strings.Contains(received.Command, "find") {
		t.Fatalf("expected find command, got %q", received.Command)
	}
	if received.WorkingDir != "" {
		t.Fatalf("expected empty working dir, got %q", received.WorkingDir)
	}
}

func TestGrepRepoTextUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolGrepRepoText(context.Background(), map[string]any{"path": "/tmp/repo", "query": "TODO", "max_results": 2})
	if err != nil {
		t.Fatalf("toolGrepRepoText() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolGrepRepoText() output = %q", out)
	}
	if !strings.Contains(received.Command, "grep") && !strings.Contains(received.Command, "rg") {
		t.Fatalf("expected grep command, got %q", received.Command)
	}
}

func TestAutomationToolsRegisterTestRunner(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["run_repo_tests"] {
		t.Fatalf("expected run_repo_tests to be registered")
	}
}

func TestRunRepoTestsUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolRunRepoTests(context.Background(), map[string]any{"path": "/tmp/repo", "package": "./...", "pattern": "TestFoo", "timeout_seconds": 90})
	if err != nil {
		t.Fatalf("toolRunRepoTests() error = %v", err)
	}
	if !strings.Contains(out, "Command: go test -v ./... -run \"TestFoo\"") {
		t.Fatalf("toolRunRepoTests() output = %q", out)
	}
	if !strings.Contains(received.Command, "go test") {
		t.Fatalf("expected go test command, got %q", received.Command)
	}
	if received.WorkingDir != "/tmp/repo" {
		t.Fatalf("expected working dir to be preserved, got %q", received.WorkingDir)
	}
	if !strings.Contains(received.Command, "go test") || !strings.Contains(received.Command, "./...") {
		t.Fatalf("expected command to include go test with ./... package, got %q", received.Command)
	}
	if !strings.Contains(out, "go test") || !strings.Contains(out, "./...") {
		t.Fatalf("expected tool result to include go test with ./... package, got %q", out)
	}
}

func TestAutomationToolsRegisterReadFileHelper(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["read_file"] {
		t.Fatalf("expected read_file to be registered")
	}
}

func TestReadFileUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolReadFile(context.Background(), map[string]any{"path": "/tmp/repo/file.go", "start_line": 1, "end_line": 40})
	if err != nil {
		t.Fatalf("toolReadFile() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolReadFile() output = %q", out)
	}
	if !strings.Contains(received.Command, "sed") {
		t.Fatalf("expected sed command, got %q", received.Command)
	}
	if received.WorkingDir != "" {
		t.Fatalf("expected empty working dir, got %q", received.WorkingDir)
	}
}

func TestAutomationToolsRegisterWriteFileHelper(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["write_file"] {
		t.Fatalf("expected write_file to be registered")
	}
}

func TestWriteFileUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolWriteFile(context.Background(), map[string]any{"path": "/tmp/repo/file.go", "content": "package main\n"})
	if err != nil {
		t.Fatalf("toolWriteFile() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolWriteFile() output = %q", out)
	}
	if !strings.Contains(received.Command, "cat <<'EOF' >") {
		t.Fatalf("expected write command, got %q", received.Command)
	}
}

func TestAutomationToolsRegisterPatchFileHelper(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["patch_file"] {
		t.Fatalf("expected patch_file to be registered")
	}
}

func TestPatchFileUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolPatchFile(context.Background(), map[string]any{"path": "/tmp/repo/file.go", "old_string": "old", "new_string": "new"})
	if err != nil {
		t.Fatalf("toolPatchFile() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolPatchFile() output = %q", out)
	}
	if !strings.Contains(received.Command, "python3") {
		t.Fatalf("expected python3 patch command, got %q", received.Command)
	}
}

func TestAutomationToolsRegisterStructureHelpers(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["create_directory"] {
		t.Fatalf("expected create_directory to be registered")
	}
	if !found["rename_file"] {
		t.Fatalf("expected rename_file to be registered")
	}
}

func TestCreateDirectoryUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolCreateDirectory(context.Background(), map[string]any{"path": "/tmp/repo/pkg"})
	if err != nil {
		t.Fatalf("toolCreateDirectory() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolCreateDirectory() output = %q", out)
	}
	if !strings.Contains(received.Command, "mkdir") {
		t.Fatalf("expected mkdir command, got %q", received.Command)
	}
}

func TestRenameFileUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolRenameFile(context.Background(), map[string]any{"old_path": "/tmp/repo/a.go", "new_path": "/tmp/repo/b.go"})
	if err != nil {
		t.Fatalf("toolRenameFile() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolRenameFile() output = %q", out)
	}
	if !strings.Contains(received.Command, "mv") {
		t.Fatalf("expected mv command, got %q", received.Command)
	}
}

func TestAutomationToolsRegisterDeleteHelpers(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["delete_file"] {
		t.Fatalf("expected delete_file to be registered")
	}
	if !found["delete_directory"] {
		t.Fatalf("expected delete_directory to be registered")
	}
}

func TestDeleteFileUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolDeleteFile(context.Background(), map[string]any{"path": "/tmp/repo/old.go"})
	if err != nil {
		t.Fatalf("toolDeleteFile() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolDeleteFile() output = %q", out)
	}
	if !strings.Contains(received.Command, "rm") {
		t.Fatalf("expected rm command, got %q", received.Command)
	}
}

func TestDeleteDirectoryUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolDeleteDirectory(context.Background(), map[string]any{"path": "/tmp/repo/olddir"})
	if err != nil {
		t.Fatalf("toolDeleteDirectory() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolDeleteDirectory() output = %q", out)
	}
	if !strings.Contains(received.Command, "rm") {
		t.Fatalf("expected rm command, got %q", received.Command)
	}
}

func TestAutomationToolsRegisterCopyHelpers(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["copy_file"] {
		t.Fatalf("expected copy_file to be registered")
	}
}

func TestCopyFileUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolCopyFile(context.Background(), map[string]any{"source_path": "/tmp/repo/a.go", "destination_path": "/tmp/repo/b.go"})
	if err != nil {
		t.Fatalf("toolCopyFile() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolCopyFile() output = %q", out)
	}
	if !strings.Contains(received.Command, "cp") {
		t.Fatalf("expected cp command, got %q", received.Command)
	}
}

func TestAutomationToolsRegisterListDirectoryHelper(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["list_directory"] {
		t.Fatalf("expected list_directory to be registered")
	}
}

func TestListDirectoryListsDirectoryContentsDirectly(t *testing.T) {
	SetAutomationPolicy(DefaultAutomationPolicy())
	defer SetAutomationPolicy(DefaultAutomationPolicy())
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		t.Fatalf("list_directory should not route through the local command executor")
		return "", nil
	})
	defer SetLocalCommandExecutor(nil)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("one"), 0o644); err != nil {
		t.Fatalf("write alpha.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.txt"), []byte("two"), 0o644); err != nil {
		t.Fatalf("write beta.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	ag := &CopilotAgent{}
	out, err := ag.toolListDirectory(context.Background(), map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("toolListDirectory() error = %v", err)
	}
	if !strings.Contains(out, "alpha.txt") || !strings.Contains(out, "beta.txt") || !strings.Contains(out, "nested/") {
		t.Fatalf("toolListDirectory() output = %q", out)
	}
}

func TestListDirectoryEmitsFormattedContentThroughEventStreamer(t *testing.T) {
	SetAutomationPolicy(DefaultAutomationPolicy())
	defer SetAutomationPolicy(DefaultAutomationPolicy())
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		t.Fatalf("list_directory should not route through the local command executor")
		return "", nil
	})
	defer SetLocalCommandExecutor(nil)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("one"), 0o644); err != nil {
		t.Fatalf("write alpha.txt: %v", err)
	}

	var got []string
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventType == "content" {
			if content, ok := payload.(string); ok {
				got = append(got, content)
			}
		}
	})

	ag := &CopilotAgent{}
	out, err := ag.toolListDirectory(ctx, map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("toolListDirectory() error = %v", err)
	}
	if !strings.Contains(out, "- alpha.txt") {
		t.Fatalf("toolListDirectory() output = %q", out)
	}
	// Directory listing should NOT stream to chat history
	if len(got) > 0 {
		t.Fatalf("expected list_directory to NOT emit content chunks to chat, got %d chunks", len(got))
	}
}

func TestPostgresQueryStreamsNativeResults(t *testing.T) {
	SetAutomationPolicy(DefaultAutomationPolicy())
	defer SetAutomationPolicy(DefaultAutomationPolicy())
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	postgresQueryExecutor = func(ctx context.Context, cfg postgresConfig, query string, allowWrite bool) (string, error) {
		return "row1", nil
	}
	defer func() { postgresQueryExecutor = nil }()

	var got []string
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventType == "content" {
			if content, ok := payload.(string); ok {
				got = append(got, content)
			}
		}
	})

	ag := &CopilotAgent{}
	out, err := ag.toolPostgresQuery(ctx, map[string]any{"query": "select 1", "use_native_api": true})
	if err != nil {
		t.Fatalf("toolPostgresQuery() error = %v", err)
	}
	if out != "row1" {
		t.Fatalf("toolPostgresQuery() output = %q", out)
	}
	if len(got) == 0 {
		t.Fatal("expected postgres query results to emit content events")
	}
	if !strings.Contains(got[0], "row1") {
		t.Fatalf("streamed content = %q", got[0])
	}
}

func TestSplunkCreateSavedSearchStreamsNativeResults(t *testing.T) {
	SetAutomationPolicy(DefaultAutomationPolicy())
	defer SetAutomationPolicy(DefaultAutomationPolicy())
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	splunkSearchExecutor = func(ctx context.Context, cfg splunkConfig, query string) (string, error) {
		return "saved", nil
	}
	defer func() { splunkSearchExecutor = nil }()

	var got []string
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventType == "content" {
			if content, ok := payload.(string); ok {
				got = append(got, content)
			}
		}
	})

	ag := &CopilotAgent{}
	out, err := ag.toolSplunkCreateSavedSearch(ctx, map[string]any{"name": "demo", "search": "index=main", "use_native_api": true})
	if err != nil {
		t.Fatalf("toolSplunkCreateSavedSearch() error = %v", err)
	}
	if out != "saved" {
		t.Fatalf("toolSplunkCreateSavedSearch() output = %q", out)
	}
	if len(got) == 0 {
		t.Fatal("expected splunk create results to emit content events")
	}
	if !strings.Contains(got[0], "saved") {
		t.Fatalf("streamed content = %q", got[0])
	}
}

func TestListDirectoryDefaultsToCurrentWorkingDirectoryWhenPathMissing(t *testing.T) {
	SetAutomationPolicy(DefaultAutomationPolicy())
	defer SetAutomationPolicy(DefaultAutomationPolicy())
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		t.Fatalf("list_directory should not route through the local command executor")
		return "", nil
	})
	defer SetLocalCommandExecutor(nil)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "listdir-default-test.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write default test file: %v", err)
	}
	defer os.Remove(filepath.Join(cwd, "listdir-default-test.txt"))

	ag := &CopilotAgent{}
	out, err := ag.toolListDirectory(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("toolListDirectory() error = %v", err)
	}
	if !strings.Contains(out, "listdir-default-test.txt") {
		t.Fatalf("toolListDirectory() output = %q", out)
	}
}

func TestAutomationToolsRegisterPathExistsHelper(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["path_exists"] {
		t.Fatalf("expected path_exists to be registered")
	}
}

func TestPathExistsUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolPathExists(context.Background(), map[string]any{"path": "/tmp/repo"})
	if err != nil {
		t.Fatalf("toolPathExists() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolPathExists() output = %q", out)
	}
	if !strings.Contains(received.Command, "test") {
		t.Fatalf("expected test command, got %q", received.Command)
	}
}

func TestAutomationToolsRegisterStatPathHelper(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	ag := &CopilotAgent{}
	ag.registerTools(context.Background())

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if !found["stat_path"] {
		t.Fatalf("expected stat_path to be registered")
	}
}

func TestStatPathUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolStatPath(context.Background(), map[string]any{"path": "/tmp/repo"})
	if err != nil {
		t.Fatalf("toolStatPath() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolStatPath() output = %q", out)
	}
	if !strings.Contains(received.Command, "stat") {
		t.Fatalf("expected stat command, got %q", received.Command)
	}
}
