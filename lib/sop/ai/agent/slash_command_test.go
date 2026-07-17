package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/parser"
)

func TestCopilotAgent_Ask_SlashCommand(t *testing.T) {
	// Setup Agent with NO databases
	cfg := Config{}
	agent := NewCopilotAgent(cfg, nil, nil)
	ctx := context.Background()
	agent.Open(ctx)

	// Inject session payload which is required by Execute
	payload := &ai.SessionPayload{
		CurrentDB: "system",
	}
	ctx = context.WithValue(ctx, "session_payload", payload)

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

func TestCapabilityPrefixRouting_RequiresSlashPrefixes(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	defer SetLocalCommandExecutor(nil)

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		return `{"stdout":"test-ok","stderr":"","exit_code":0}`, nil
	})

	ctx := context.Background()
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.Open(ctx)

	// Test unprefixed forms - should NOT be handled by capability routing
	for _, input := range []string{"echo direct-ok", "cmd:echo direct-ok", "cmd echo direct-ok", "shell:echo direct-ok", "exec:echo direct-ok", "run:echo direct-ok"} {
		if handled, _, _ := agent.capabilityRegistry.TryPrefixRouting(ctx, input); handled {
			t.Fatalf("TryPrefixRouting(%q) = handled, want not handled for unprefixed command", input)
		}
	}

	// Test slash-prefixed forms - should be handled by capability routing
	for _, input := range []string{"/cmd echo direct-ok", "/cmd:echo direct-ok", "/c echo direct-ok", "/c:echo direct-ok", "/shell echo test", "/exec pwd"} {
		if handled, _, _ := agent.capabilityRegistry.TryPrefixRouting(ctx, input); !handled {
			t.Fatalf("TryPrefixRouting(%q) = not handled, want handled for slash-prefixed command", input)
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
