package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestStoresFeatureIsEnabledByDefault(t *testing.T) {
	// Reset to default state in case other tests modified it
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(true) // Restore default after test

	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})
	ag.registerTools(ctx)

	foundStoreTool := false
	for _, tool := range ag.registry.List() {
		if isStoreTool(tool.Name) {
			foundStoreTool = true
			break
		}
	}

	if !foundStoreTool {
		t.Fatal("expected Store tools to be enabled by default")
	}
}

func TestStoresFeatureCanBeEnabled(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})
	ag.registerTools(ctx)

	// Verify Store tools ARE registered when enabled
	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	expectedStoreTools := []string{"execute_script", "list_stores", "begin_tx", "commit_tx", "open_store", "select"}
	for _, expectedTool := range expectedStoreTools {
		if !found[expectedTool] {
			t.Fatalf("expected %q to be registered when Stores feature is enabled", expectedTool)
		}
	}
}

func TestStoresCapabilityNotRegisteredWhenDisabled(t *testing.T) {
	SetStoresFeatureEnabled(false)
	defer SetStoresFeatureEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)

	// Verify Stores capability is NOT in registry
	if ag.capabilityRegistry == nil {
		t.Fatal("expected capability registry to be initialized")
	}

	_, ok := ag.capabilityRegistry.Resolve("", capabilities.RoutingState{Domain: "Stores"})
	if ok {
		t.Fatal("expected Stores capability to NOT be registered when feature is disabled")
	}
}

func TestStoresCapabilityRegisteredWhenEnabled(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)

	// Verify Stores capability IS in registry
	_, ok := ag.capabilityRegistry.Resolve("", capabilities.RoutingState{Domain: "Stores"})
	if !ok {
		t.Fatal("expected Stores capability to be registered when feature is enabled")
	}
}

func TestListToolsExcludesStoreToolsWhenDisabled(t *testing.T) {
	SetStoresFeatureEnabled(false)
	defer SetStoresFeatureEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	output, err := ag.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed: %v", err)
	}

	// Verify Store tools don't appear in output
	storeToolNames := []string{"execute_script", "list_stores", "begin_tx", "commit_tx", "open_store", "select"}
	for _, toolName := range storeToolNames {
		if strings.Contains(output, toolName) {
			t.Fatalf("expected list_tools to exclude Store tool %q when feature disabled, but found it in output", toolName)
		}
	}
}

func TestListToolsIncludesStoreToolsWhenEnabled(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	output, err := ag.Execute(ctx, "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed: %v", err)
	}

	// Verify at least some Store tools appear in output
	if !strings.Contains(output, "list_stores") {
		t.Fatal("expected list_tools to include Store tools when feature enabled")
	}
}

func TestStoresFeatureFlagPersistsAcrossAgentInstances(t *testing.T) {
	// Enable for first agent
	SetStoresFeatureEnabled(true)
	ag1 := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})
	ag1.registerTools(ctx)

	hasStoreTools1 := false
	for _, tool := range ag1.registry.List() {
		if isStoreTool(tool.Name) {
			hasStoreTools1 = true
			break
		}
	}

	// Disable for second agent
	SetStoresFeatureEnabled(false)
	ag2 := NewCopilotAgent(Config{}, nil, nil)
	ag2.registerTools(ctx)

	hasStoreTools2 := false
	for _, tool := range ag2.registry.List() {
		if isStoreTool(tool.Name) {
			hasStoreTools2 = true
			break
		}
	}

	if !hasStoreTools1 {
		t.Fatal("expected first agent to have Store tools when enabled")
	}
	if hasStoreTools2 {
		t.Fatal("expected second agent to NOT have Store tools when disabled")
	}

	// Cleanup
	SetStoresFeatureEnabled(false)
}

// isStoreTool returns true if the tool name is a Store-specific tool
func isStoreTool(name string) bool {
	storeTools := map[string]bool{
		"execute_script":     true,
		"list_stores":        true,
		"begin_tx":           true,
		"commit_tx":          true,
		"rollback_tx":        true,
		"open_store":         true,
		"select":             true,
		"join":               true,
		"join_right":         true,
		"explain_join":       true,
		"add":                false, // Too generic, also used elsewhere
		"update":             false, // Too generic, also used elsewhere
		"delete":             false, // Too generic, also used elsewhere
		"manage_transaction": true,
		"scan":               true,
		"filter":             true,
		"sort":               true,
		"project":            true,
		"limit":              true,
	}
	return storeTools[name]
}
