package agent

import (
	"context"
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
