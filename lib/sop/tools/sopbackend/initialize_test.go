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
	if cfg := GetConfig(); cfg.HostKind != HostKindUnknown {
		t.Fatalf("expected unspecified host kind to default to %q, got %q", HostKindUnknown, cfg.HostKind)
	}
}

func TestInitializeAppliesHostKindRuntimeOption(t *testing.T) {
	if err := Initialize(context.Background(), RuntimeOptions{HostKind: HostKindCorporate}); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if cfg := GetConfig(); cfg.HostKind != HostKindCorporate {
		t.Fatalf("expected host kind %q after Initialize, got %q", HostKindCorporate, cfg.HostKind)
	}
	if cfg := GetConfig(); cfg.HostProfile.AuthMode != AuthModeEnterpriseSSO {
		t.Fatalf("expected corporate host profile auth mode %q, got %q", AuthModeEnterpriseSSO, cfg.HostProfile.AuthMode)
	}
	if cfg := GetConfig(); cfg.HostProfile.DefaultProvisioningMode != ProvisioningModeSimple {
		t.Fatalf("expected corporate default provisioning mode %q, got %q", ProvisioningModeSimple, cfg.HostProfile.DefaultProvisioningMode)
	}
}

func TestInitializeAppliesExplicitHostProfile(t *testing.T) {
	profile := HostProfile{
		Kind:                    HostKindDaemon,
		AuthMode:                AuthModeJWTBootstrap,
		ProvisioningModes:       []ProvisioningMode{ProvisioningModeAdvanced, ProvisioningModeSimple},
		DefaultProvisioningMode: ProvisioningModeAdvanced,
	}
	if err := Initialize(context.Background(), RuntimeOptions{HostProfile: profile}); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	cfg := GetConfig()
	if cfg.HostKind != HostKindDaemon {
		t.Fatalf("expected host kind %q after Initialize, got %q", HostKindDaemon, cfg.HostKind)
	}
	if cfg.HostProfile.DefaultProvisioningMode != ProvisioningModeAdvanced {
		t.Fatalf("expected explicit default provisioning mode %q, got %q", ProvisioningModeAdvanced, cfg.HostProfile.DefaultProvisioningMode)
	}
	if len(cfg.HostProfile.ProvisioningModes) != 2 {
		t.Fatalf("expected 2 provisioning modes, got %#v", cfg.HostProfile.ProvisioningModes)
	}
}
