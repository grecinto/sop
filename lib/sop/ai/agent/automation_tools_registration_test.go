package agent

import (
	"context"
	"strings"
	"testing"
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
	if out != "ok" {
		t.Fatalf("toolRunRepoTests() output = %q", out)
	}
	if !strings.Contains(received.Command, "go test") {
		t.Fatalf("expected go test command, got %q", received.Command)
	}
	if received.WorkingDir != "/tmp/repo" {
		t.Fatalf("expected working dir to be preserved, got %q", received.WorkingDir)
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

func TestListDirectoryUsesLocalCommandExecutor(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var received LocalCommandRequest
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		received = req
		return "ok", nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.toolListDirectory(context.Background(), map[string]any{"path": "/tmp/repo"})
	if err != nil {
		t.Fatalf("toolListDirectory() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("toolListDirectory() output = %q", out)
	}
	if !strings.Contains(received.Command, "ls") {
		t.Fatalf("expected ls command, got %q", received.Command)
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
