package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestServiceListTools(t *testing.T) {
	// Setup
	registry := make(map[string]ai.Agent[map[string]any])

	// Create a CopilotAgent and add to registry with arbitrary key
	da := NewCopilotAgent(Config{}, nil, nil)
	// We need to register tools on the agent so list_tools works
	da.registerTools(context.Background())

	registry["random_key_name"] = da

	// Create Service
	svc := &Service{
		registry: registry,
		session:  &RunnerSession{},
	}

	// Test /list_tools
	handler := svc.handleSessionCommand

	// Execution
	// Setup context with payload
	payload := &ai.SessionPayload{
		CurrentDB: "system",
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	out, handled, err := handler(ctx, "/list_tools", nil)

	// Verification
	if err != nil {
		t.Fatalf("Handler failed: %v", err)
	}
	if !handled {
		t.Error("Handler should have handled the command")
	}

	// Check content
	if len(out) == 0 {
		t.Error("Output should not be empty")
	}

	// Modern output format is a flat list
	// Check for a specific command with backticks
	if !strings.Contains(out, "/list_tools") {
		t.Error("Output should contain /list_tools command")
	}

	// Check for an agent tool
	if !strings.Contains(out, "/add") {
		t.Error("Output should contain /add command")
	}
}

type listToolsStubAgent struct {
	tools []ai.ToolDefinition
}

func (a *listToolsStubAgent) Open(ctx context.Context) error  { return nil }
func (a *listToolsStubAgent) Close(ctx context.Context) error { return nil }
func (a *listToolsStubAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *listToolsStubAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	return "", nil
}
func (a *listToolsStubAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *listToolsStubAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return a.tools, nil
}

type directSlashStubAgent struct{}

func (a *directSlashStubAgent) Open(ctx context.Context) error  { return nil }
func (a *directSlashStubAgent) Close(ctx context.Context) error { return nil }
func (a *directSlashStubAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *directSlashStubAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	return "", nil
}
func (a *directSlashStubAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	if toolName == "list_directory" {
		return "dir1\nfile2", nil
	}
	return "", fmt.Errorf("unknown tool: %s", toolName)
}
func (a *directSlashStubAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return []ai.ToolDefinition{{Name: "list_directory", Description: "list directory"}}, nil
}

func TestServiceAskBypassesPipelineForDirectSlashCommands(t *testing.T) {
	t.Skip("Slash command handling moved to Agent layer (handleDirectToolInvocation) via capability routing")
	svc := &Service{
		registry: map[string]ai.Agent[map[string]any]{"stub": &directSlashStubAgent{}},
		session:  &RunnerSession{},
		pipeline: []PipelineStep{{Agent: PipelineAgent{ID: "broken"}}},
	}

	resp, err := svc.ask(context.Background(), AskRequest{Query: "/list_directory", Session: &ai.SessionPayload{}})
	if err != nil {
		t.Fatalf("ask() error = %v", err)
	}
	if resp.FinalText != "dir1\nfile2" {
		t.Fatalf("ask() final text = %q, want %q", resp.FinalText, "dir1\nfile2")
	}
}

func TestServiceAskRegistersToolsBeforeDirectSlashCommands(t *testing.T) {
	t.Skip("Slash command handling moved to Agent layer (handleDirectToolInvocation) via capability routing")
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	svc := &Service{
		registry: map[string]ai.Agent[map[string]any]{"copilot": NewCopilotAgent(Config{}, nil, nil)},
		session:  &RunnerSession{},
		pipeline: []PipelineStep{{Agent: PipelineAgent{ID: "broken"}}},
	}

	resp, err := svc.ask(context.Background(), AskRequest{Query: "/list_directory", Session: &ai.SessionPayload{}})
	if err != nil {
		t.Fatalf("ask() error = %v", err)
	}
	if resp.FinalText == "" {
		t.Fatal("expected non-empty directory listing")
	}
	if strings.Contains(resp.FinalText, "not found") || strings.Contains(resp.FinalText, "failed") {
		t.Fatalf("unexpected tool failure output: %q", resp.FinalText)
	}
}

func TestListToolsUsesRegisteredToolExecutorInventory(t *testing.T) {
	registry := make(map[string]ai.Agent[map[string]any])
	registry["stub"] = &listToolsStubAgent{tools: []ai.ToolDefinition{{Name: "alpha", Description: "Alpha tool", Schema: "{}"}, {Name: "beta", Description: "Beta tool", Schema: "{}"}}}

	svc := &Service{registry: registry, session: &RunnerSession{}}
	out, handled, err := svc.handleSessionCommand(context.Background(), "/list_tools", nil)
	if err != nil {
		t.Fatalf("Handler failed: %v", err)
	}
	if !handled {
		t.Fatal("Handler should have handled the command")
	}
	if !strings.Contains(out, "/alpha") || !strings.Contains(out, "/beta") {
		t.Fatalf("Output should include registered agent tools, got: %s", out)
	}
}

func TestServiceListToolsDoesNotInterceptListDirectory(t *testing.T) {
	svc := &Service{registry: map[string]ai.Agent[map[string]any]{}, session: &RunnerSession{}}
	out, handled, err := svc.handleSessionCommand(context.Background(), "/list_directory", nil)
	if err != nil {
		t.Fatalf("Handler should not error for /list_directory: %v", err)
	}
	if handled {
		t.Fatalf("Handler should not intercept /list_directory, got handled=%t output=%q", handled, out)
	}
	if out != "" {
		t.Fatalf("Handler should not produce output for /list_directory, got %q", out)
	}
}

func TestServiceListToolsWithoutSessionPayload(t *testing.T) {
	registry := make(map[string]ai.Agent[map[string]any])

	da := NewCopilotAgent(Config{}, nil, nil)
	da.registerTools(context.Background())
	registry["random_key_name"] = da

	svc := &Service{
		registry: registry,
		session:  &RunnerSession{},
	}

	out, handled, err := svc.handleSessionCommand(context.Background(), "/list_tools", nil)
	if err != nil {
		t.Fatalf("Handler failed without session payload: %v", err)
	}
	if !handled {
		t.Fatal("Handler should have handled the command without session payload")
	}
	if len(out) == 0 {
		t.Fatal("Output should not be empty without session payload")
	}
	if strings.Contains(out, "Error listing agent tools") {
		t.Fatalf("Output should not contain an agent-listing error, got: %s", out)
	}
	if !strings.Contains(out, "/add") {
		t.Fatalf("Output should contain /add even without session payload, got: %s", out)
	}
}
