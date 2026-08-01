package sopbackend

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
)

func listToolsTextForLoadedAgents(t *testing.T, agents map[string]ai.Agent[map[string]any]) string {
	t.Helper()
	if len(agents) == 0 {
		t.Fatal("expected backend agents to be initialized")
	}

	copilot, ok := agents["copilot"]
	if !ok {
		t.Fatal("expected backend to expose a copilot agent")
	}

	// The copilot agent should be a Service wrapping the CopilotAgent sub-agent.
	// Use the generic Agent.Ask interface which should handle /list_tools
	out, err := copilot.Ask(context.Background(), "/list_tools", nil)
	if err != nil {
		t.Fatalf("/list_tools ask failed: %v", err)
	}
	return out
}

func TestInitializeDaemonRuntimeExposesAutomationAndNativeToolsInListTools(t *testing.T) {
	sharedagent.SetLocalDesktopAutomationEnabled(false)
	sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())
	defer sharedagent.SetLocalDesktopAutomationEnabled(false)
	defer sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())

	policy := sharedagent.AutomationPolicy{
		Enabled:               true,
		AllowShellCommands:    true,
		AllowEmail:            false,
		RequireApproval:       true,
		DefaultTimeoutSeconds: 60,
		MaxTimeoutSeconds:     120,
	}
	if err := Initialize(context.Background(), RuntimeOptions{EnableLocalAutomation: true, AutomationPolicy: &policy}); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	out := listToolsTextForLoadedAgents(t, GetLoadedAgents())
	for _, expected := range []string{"/execute_script", "/list_stores", "/mint_to_space", "/read_space_config", "/execute_local_command", "/git_status", "/postgres_query", "/splunk_search"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected daemon tool list to include %s, got:\n%s", expected, out)
		}
	}
}

func TestInitializeHttpServerRuntimeExposesNativeToolsWithoutAutomation(t *testing.T) {
	sharedagent.SetLocalDesktopAutomationEnabled(false)
	sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())
	defer sharedagent.SetLocalDesktopAutomationEnabled(false)
	defer sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())

	if err := Initialize(context.Background(), RuntimeOptions{}); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	out := listToolsTextForLoadedAgents(t, GetLoadedAgents())
	for _, expected := range []string{"/list_tools", "/mint_to_space", "/read_space_config"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected httpserver tool list to include %s, got:\n%s", expected, out)
		}
	}
	for _, disallowed := range []string{"/execute_local_command", "/git_status", "/postgres_query", "/splunk_search"} {
		if strings.Contains(out, disallowed) {
			t.Fatalf("expected httpserver tool list to exclude %s, got:\n%s", disallowed, out)
		}
	}
}
