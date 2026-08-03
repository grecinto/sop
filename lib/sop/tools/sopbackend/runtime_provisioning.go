package sopbackend

import (
	"os"
	"path/filepath"
	"strings"
)

type ProvisioningDecisionReason string

const (
	ProvisioningDecisionReasonReady               ProvisioningDecisionReason = "ready"
	ProvisioningDecisionReasonMissingConfig       ProvisioningDecisionReason = "missing_config"
	ProvisioningDecisionReasonMissingRuntimeState ProvisioningDecisionReason = "missing_runtime_state"
)

type ProvisioningDecision struct {
	Required        bool                       `json:"required"`
	Mode            ProvisioningMode           `json:"mode"`
	AvailableModes  []ProvisioningMode         `json:"available_modes,omitempty"`
	Reason          ProvisioningDecisionReason `json:"reason"`
	ResolvedPath    string                     `json:"resolved_path,omitempty"`
	HostProfile     HostProfile                `json:"host_profile"`
	HasConfigOnDisk bool                       `json:"has_config_on_disk"`
}

func CurrentProvisioningDecision() ProvisioningDecision {
	return ResolveProvisioningDecision(config.ConfigFile, RuntimeOptions{HostProfile: config.HostProfile, HostKind: config.HostKind})
}

func ResolveProvisioningDecision(configPath string, opts RuntimeOptions) ProvisioningDecision {
	profile := resolveHostProfile(opts)
	resolvedPath := strings.TrimSpace(configPath)
	if resolvedPath == "" {
		resolvedPath = FindExistingConfigFile()
	}
	if resolvedPath == "" {
		if cwd, err := os.Getwd(); err == nil {
			resolvedPath = filepath.Join(cwd, "config.json")
		} else {
			resolvedPath = "config.json"
		}
	}

	decision := ProvisioningDecision{
		Mode:           profile.DefaultProvisioningMode,
		AvailableModes: append([]ProvisioningMode(nil), profile.ProvisioningModes...),
		Reason:         ProvisioningDecisionReasonReady,
		ResolvedPath:   resolvedPath,
		HostProfile:    profile,
	}

	if _, err := os.Stat(resolvedPath); err == nil {
		decision.HasConfigOnDisk = true
		cfg, loadErr := loadConfigFromDisk(resolvedPath)
		if loadErr != nil {
			decision.Required = true
			decision.Reason = ProvisioningDecisionReasonMissingRuntimeState
			return decision
		}
		if len(cfg.Databases) == 0 && cfg.SystemDB == nil {
			decision.Required = true
			decision.Reason = ProvisioningDecisionReasonMissingRuntimeState
			return decision
		}
		return decision
	}

	decision.Required = true
	decision.Reason = ProvisioningDecisionReasonMissingConfig
	decision.HasConfigOnDisk = false
	if decision.Mode == ProvisioningModeUnknown && len(decision.AvailableModes) > 0 {
		decision.Mode = decision.AvailableModes[0]
	}
	return decision
}
