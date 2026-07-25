package agent

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

func TestCopilotAgent_Registry(t *testing.T) {
	cfg := Config{}
	agent := NewCopilotAgent(cfg, nil, nil)
	agent.registerTools(context.Background())

	// Test Registry Listing
	tools := agent.registry.List()
	if len(tools) == 0 {
		t.Fatal("Registry should not be empty")
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "list_databases" {
			found = true
			break
		}
	}
	if !found {
		t.Error("list_databases tool not found in registry")
	}
}

func TestCopilotAgent_Execute(t *testing.T) {
	cfg := Config{}
	dbs := map[string]sop.DatabaseOptions{
		"test_db": {},
	}
	agent := NewCopilotAgent(cfg, dbs, nil)
	agent.registerTools(context.Background())

	// Setup Context with Payload
	payload := &ai.SessionPayload{
		CurrentDB: "test_db",
	}
	ctx := context.Background()
	ctx = context.WithValue(ctx, "session_payload", payload)

	// Test list_databases
	resp, err := agent.Execute(ctx, "list_databases", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Unwrap ToolResultEnvelope if present
	var envelope ai.ToolResultEnvelope
	if err := json.Unmarshal([]byte(resp), &envelope); err == nil && len(envelope.ToolResult) > 0 {
		if envelope.FileMetadata != nil && envelope.FileMetadata.FilePath != "" {
			fileContent, err := os.ReadFile(envelope.FileMetadata.FilePath)
			if err == nil {
				resp = string(fileContent)
			}
		} else {
			var unwrapped string
			if err := json.Unmarshal(envelope.ToolResult, &unwrapped); err == nil {
				resp = unwrapped
			}
		}
	}

	if resp != "Databases: test_db" {
		t.Errorf("Expected 'Databases: test_db', got '%s'", resp)
	}
}
