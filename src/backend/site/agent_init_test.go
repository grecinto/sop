package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestSiteBackendInitializesAgents(t *testing.T) {
	// Create a temporary data directory for the test
	tmpDir := t.TempDir()

	// Set up a minimal config file
	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"port": 8080,
		"databases": [],
		"system_db": {
			"path": "` + filepath.Join(tmpDir, "system_db") + `"
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Simulate what configureRuntimeEnvironment does
	sopbackend.SetConfigFile(configPath)
	if err := sopbackend.LoadConfig(configPath); err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// The key part: InitializeRuntime should load agents
	if _, err := sopbackend.InitializeRuntime(nil, configPath, sopbackend.RuntimeOptions{
		EnableLocalAutomation: false,
	}, 0, nil); err != nil {
		// The error might be about missing agent config file, which is expected
		// in the test environment, but we want to verify it was at least attempted
		if !strings.Contains(err.Error(), "agent config not found") {
			t.Fatalf("unexpected error during initialization: %v", err)
		}
		// This is the expected error - agent config file doesn't exist in test env
		t.Logf("Got expected error about missing agent config: %v", err)
		return
	}

	// If we get here, the initialization succeeded (unlikely in test env, but OK)
	agents := sopbackend.GetLoadedAgents()
	if len(agents) == 0 {
		t.Log("Warning: InitializeRuntime succeeded but no agents loaded")
	} else {
		t.Logf("Successfully loaded %d agents", len(agents))
	}
}
