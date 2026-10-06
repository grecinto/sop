package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/parser"
)

func TestCopilotAgent_Ask_SlashCommand(t *testing.T) {
	// Setup Agent with NO databases
	cfg := Config{}
	agent := NewCopilotAgent(cfg, nil, nil)
	payload := &ai.SessionPayload{
		CurrentDB: "system",
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)
	agent.Open(ctx)

	// Case 1: Slash Command with simple args
	// /list_databases
	resp, err := agent.Ask(ctx, "/list_databases", nil)
	if err != nil {
		t.Fatalf("Ask with /list_databases failed: %v", err)
	}

	if strings.Contains(resp, "No valid API Key found") {
		t.Errorf("Slash command triggered 'No API Key' error. Logic failed.")
	}
}

func TestCopilotAgent_Ask_WorkspaceCommandShowsCurrentCodingRoot(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: NewRunnerSession()}
	ag.service.session.Memory = NewShortTermMemory()
	thread := &ConversationThread{ID: sop.NewUUID(), StreamState: WorkStreamActive}
	thread.InitializeWorkStream("investigation")
	thread.StreamContext.WorkspaceRoot = "/workspace/project"
	ag.service.session.Memory.AddThread(thread)

	resp, err := ag.Ask(context.Background(), "/workspace", nil)
	if err != nil {
		t.Fatalf("/workspace failed: %v", err)
	}
	if resp != "Current coding workspace: /workspace/project" {
		t.Fatalf("unexpected /workspace response: %q", resp)
	}
}

func TestCopilotAgent_Ask_WorkspaceCommandReportsNoRoot(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: NewRunnerSession()}
	ag.service.session.Memory = NewShortTermMemory()

	resp, err := ag.Ask(context.Background(), "/workspace", nil)
	if err != nil {
		t.Fatalf("/workspace failed: %v", err)
	}
	if resp != "Current coding workspace: (none set)." {
		t.Fatalf("unexpected /workspace response for unset scope: %q", resp)
	}
}

func TestCopilotAgent_Ask_RepoAliasShowsCurrentCodingRoot(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: NewRunnerSession()}
	ag.service.session.Memory = NewShortTermMemory()
	thread := &ConversationThread{ID: sop.NewUUID(), StreamState: WorkStreamActive}
	thread.InitializeWorkStream("investigation")
	thread.StreamContext.WorkspaceRoot = "/workspace/project"
	ag.service.session.Memory.AddThread(thread)

	resp, err := ag.Ask(context.Background(), "/repo", nil)
	if err != nil {
		t.Fatalf("/repo failed: %v", err)
	}
	if resp != "Current coding workspace: /workspace/project" {
		t.Fatalf("unexpected /repo response: %q", resp)
	}
}

func TestCopilotAgent_Ask_CommandPrefixExecutesLocalAutomationDirectly(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetLocalCommandExecutor(nil)

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		if req.Command != "echo direct-ok" {
			t.Fatalf("command = %q, want %q", req.Command, "echo direct-ok")
		}
		return `{"stdout":"direct-ok","stderr":"","exit_code":0}`, nil
	})

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{CurrentDB: "system"}
	ctx = context.WithValue(ctx, "session_payload", payload)

	resp, err := agent.Ask(ctx, "/c echo direct-ok", nil)
	if err != nil {
		t.Fatalf("Ask with direct command prefix failed: %v", err)
	}
	if !strings.Contains(resp, "direct-ok") {
		t.Fatalf("direct command response = %q, want it to contain command output", resp)
	}
}

func TestCopilotAgent_Ask_SlashPrefixedCommandExecutesLocalAutomationDirectly(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetLocalCommandExecutor(nil)

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		if req.Command != "echo slash-ok" {
			t.Fatalf("command = %q, want %q", req.Command, "echo slash-ok")
		}
		return `{"stdout":"slash-ok","stderr":"","exit_code":0}`, nil
	})

	agent := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.Background()
	payload := &ai.SessionPayload{CurrentDB: "system"}
	ctx = context.WithValue(ctx, "session_payload", payload)

	resp, err := agent.Ask(ctx, "/cmd echo slash-ok", nil)
	if err != nil {
		t.Fatalf("Ask with slash-prefixed direct command failed: %v", err)
	}
	if !strings.Contains(resp, "slash-ok") {
		t.Fatalf("slash-prefixed direct command response = %q, want it to contain command output", resp)
	}
}

func TestCopilotAgent_Clone_Ask_CommandPrefixExecutesLocalAutomationDirectly(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetLocalCommandExecutor(nil)

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		if req.Command != "cd .." {
			t.Fatalf("command = %q, want %q", req.Command, "cd ..")
		}
		return `{"stdout":"","stderr":"","exit_code":0}`, nil
	})

	blueprint := NewCopilotAgent(Config{}, nil, nil)
	cloned, ok := blueprint.Clone().(*CopilotAgent)
	if !ok {
		t.Fatal("expected CopilotAgent clone")
	}

	ctx := context.Background()
	payload := &ai.SessionPayload{CurrentDB: "system", SessionID: "clone-slash", UserID: "alice"}
	ctx = context.WithValue(ctx, "session_payload", payload)

	resp, err := cloned.Ask(ctx, "/c cd ..", nil)
	if err != nil {
		t.Fatalf("Ask on cloned agent with direct command prefix failed: %v", err)
	}
	if strings.Contains(strings.ToLower(resp), "tool fallback failed") {
		t.Fatalf("cloned direct command response = %q, did not expect tool fallback failure", resp)
	}
}

func TestCapabilityPrefixRouting_RequiresSlashPrefixes(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetLocalCommandExecutor(nil)

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		return `{"stdout":"test-ok","stderr":"","exit_code":0}`, nil
	})

	ctx := context.WithValue(context.Background(), SessionPayloadKey, &ai.SessionPayload{Variables: map[string]any{}})
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.Open(ctx)

	// Test unprefixed forms - should NOT be handled by capability routing
	for _, input := range []string{"echo direct-ok", "cmd:echo direct-ok", "cmd echo direct-ok", "shell:echo direct-ok", "exec:echo direct-ok", "run:echo direct-ok"} {
		if handled, _, _ := agent.capabilityRegistry.TryPrefixRouting(ctx, input); handled {
			t.Fatalf("TryPrefixRouting(%q) = handled, want not handled for unprefixed command", input)
		}
	}

	// Test the supported explicit slash prefixes
	for _, input := range []string{"/cmd echo direct-ok", "/cmd:echo direct-ok", "/c echo direct-ok", "/c:echo direct-ok", "/shell echo test", "/exec pwd"} {
		if handled, _, _ := agent.capabilityRegistry.TryPrefixRouting(ctx, input); !handled {
			t.Fatalf("TryPrefixRouting(%q) = not handled, want handled for supported slash-prefixed command", input)
		}
	}
}

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantTool string
		wantArgs map[string]string
	}{
		{
			input:    "tool key=value",
			wantTool: "tool",
			wantArgs: map[string]string{"key": "value"},
		},
		{
			input:    "tool key=\"value with spaces\"",
			wantTool: "tool",
			wantArgs: map[string]string{"key": "value with spaces"},
		},
		{
			input:    "tool key=\"nested \\\"quote\\\"\"",
			wantTool: "tool",
			wantArgs: map[string]string{"key": "nested \"quote\""},
		},
		{
			input:    "create_script \"My New Script\"",
			wantTool: "create_script",
			wantArgs: map[string]string{"name": "My New Script"},
		},
		{
			input:    "save_last_step \"MyScript\"",
			wantTool: "save_last_step",
			wantArgs: map[string]string{"script": "MyScript"},
		},
	}

	for _, tt := range tests {
		gotTool, gotArgs, err := parser.ParseSlashCommand(tt.input)
		if err != nil {
			t.Errorf("parseSlashCommand(%q) returned error: %v", tt.input, err)
			continue
		}
		if gotTool != tt.wantTool {
			t.Errorf("parseSlashCommand(%q) tool = %q, want %q", tt.input, gotTool, tt.wantTool)
		}

		for k, v := range tt.wantArgs {
			valStr, _ := gotArgs[k].(string)
			if valStr != v {
				t.Errorf("parseSlashCommand(%q) arg[%q] = %v, want %q", tt.input, k, gotArgs[k], v)
			}
		}
	}
}
