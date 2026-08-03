package sopbackend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProvisioningDecision_RequiresSimpleProvisioningForDaemonWithoutConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	decision := ResolveProvisioningDecision(configPath, RuntimeOptions{HostKind: HostKindDaemon})

	if !decision.Required {
		t.Fatal("expected provisioning to be required for daemon without config")
	}
	if decision.Mode != ProvisioningModeSimple {
		t.Fatalf("expected daemon provisioning mode %q, got %q", ProvisioningModeSimple, decision.Mode)
	}
	if decision.Reason != ProvisioningDecisionReasonMissingConfig {
		t.Fatalf("expected reason %q, got %q", ProvisioningDecisionReasonMissingConfig, decision.Reason)
	}
	if decision.HostProfile.Kind != HostKindDaemon {
		t.Fatalf("expected host profile kind %q, got %q", HostKindDaemon, decision.HostProfile.Kind)
	}
	if decision.ResolvedPath != configPath {
		t.Fatalf("expected resolved path %q, got %q", configPath, decision.ResolvedPath)
	}
	if len(decision.AvailableModes) != 2 {
		t.Fatalf("expected 2 available provisioning modes, got %#v", decision.AvailableModes)
	}
}

func TestResolveProvisioningDecision_RequiresSimpleProvisioningChooserForHQWithoutConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	decision := ResolveProvisioningDecision(configPath, RuntimeOptions{HostKind: HostKindHQ})

	if !decision.Required {
		t.Fatal("expected provisioning to be required for HQ without config")
	}
	if decision.Mode != ProvisioningModeSimple {
		t.Fatalf("expected HQ provisioning mode %q, got %q", ProvisioningModeSimple, decision.Mode)
	}
	if decision.HostProfile.AuthMode != AuthModeSession {
		t.Fatalf("expected HQ auth mode %q, got %q", AuthModeSession, decision.HostProfile.AuthMode)
	}
	if len(decision.AvailableModes) != 2 || decision.AvailableModes[0] != ProvisioningModeSimple || decision.AvailableModes[1] != ProvisioningModeAdvanced {
		t.Fatalf("expected HQ provisioning chooser [simple advanced], got %#v", decision.AvailableModes)
	}
}

func TestResolveProvisioningDecision_NotRequiredWhenConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	decision := ResolveProvisioningDecision(configPath, RuntimeOptions{HostKind: HostKindDaemon})

	if !decision.Required {
		t.Fatal("expected provisioning to remain required when config exists but runtime state is empty")
	}
	if !decision.HasConfigOnDisk {
		t.Fatal("expected config-on-disk to be true when config exists")
	}
	if decision.Reason != ProvisioningDecisionReasonMissingRuntimeState {
		t.Fatalf("expected reason %q, got %q", ProvisioningDecisionReasonMissingRuntimeState, decision.Reason)
	}
}

func TestResolveProvisioningDecision_NotRequiredWhenProvisionedConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"system_db":{"name":"system","path":"/tmp/system","mode":"standalone"}}\n`), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	decision := ResolveProvisioningDecision(configPath, RuntimeOptions{HostKind: HostKindDaemon})

	if decision.Required {
		t.Fatal("expected provisioning to be skipped when config exists")
	}
	if !decision.HasConfigOnDisk {
		t.Fatal("expected config-on-disk to be true when config exists")
	}
	if decision.Reason != ProvisioningDecisionReasonReady {
		t.Fatalf("expected reason %q, got %q", ProvisioningDecisionReasonReady, decision.Reason)
	}
}

func TestResolveProvisioningDecision_UsesExplicitHostProfileOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	profile := HostProfile{
		Kind:                    HostKindDaemon,
		AuthMode:                AuthModeJWTBootstrap,
		ProvisioningModes:       []ProvisioningMode{ProvisioningModeAdvanced, ProvisioningModeSimple},
		DefaultProvisioningMode: ProvisioningModeAdvanced,
	}

	decision := ResolveProvisioningDecision(configPath, RuntimeOptions{HostProfile: profile})

	if decision.Mode != ProvisioningModeAdvanced {
		t.Fatalf("expected explicit provisioning mode override %q, got %q", ProvisioningModeAdvanced, decision.Mode)
	}
	if decision.HostProfile.DefaultProvisioningMode != ProvisioningModeAdvanced {
		t.Fatalf("expected decision to preserve explicit host profile default provisioning mode %q, got %q", ProvisioningModeAdvanced, decision.HostProfile.DefaultProvisioningMode)
	}
	if decision.HostProfile.Kind != HostKindDaemon {
		t.Fatalf("expected explicit host profile kind %q, got %q", HostKindDaemon, decision.HostProfile.Kind)
	}
}
