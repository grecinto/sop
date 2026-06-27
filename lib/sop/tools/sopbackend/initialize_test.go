package sopbackend

import (
	"context"
	"testing"

	sharedagent "github.com/sharedcode/sop/ai/agent"
)

func TestInitializeAppliesAutomationRuntimeOptions(t *testing.T) {
	sharedagent.SetLocalDesktopAutomationEnabled(false)
	sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())
	defer sharedagent.SetLocalDesktopAutomationEnabled(false)
	defer sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())

	policy := sharedagent.AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: true, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120}
	if err := Initialize(context.Background(), RuntimeOptions{EnableLocalAutomation: true, AutomationPolicy: &policy}); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	activePolicy := sharedagent.GetAutomationPolicy()
	if !activePolicy.Enabled {
		t.Fatalf("expected automation to be enabled after Initialize, got %#v", activePolicy)
	}
	if !activePolicy.AllowShellCommands {
		t.Fatalf("expected shell automation to be enabled after Initialize, got %#v", activePolicy)
	}
}
