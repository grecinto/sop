package sopbackend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeRuntimeBootstrapsConfigAndReturnsResolvedPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	systemPath := filepath.Join(tmpDir, "system")
	configContents := []byte("{\n  \"port\": 9090,\n  \"production_mode\": true,\n  \"system_db\": {\n    \"name\": \"system\",\n    \"path\": \"" + systemPath + "\",\n    \"mode\": \"standalone\"\n  },\n  \"databases\": []\n}\n")
	if err := os.WriteFile(configPath, configContents, 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	resolvedPath, err := InitializeRuntime(context.Background(), configPath, RuntimeOptions{}, 0, nil)
	if err != nil {
		t.Fatalf("InitializeRuntime() error = %v", err)
	}
	if resolvedPath != configPath {
		t.Fatalf("InitializeRuntime() resolved path = %q, want %q", resolvedPath, configPath)
	}

	cfg := GetConfig()
	if cfg.ConfigFile != configPath {
		t.Fatalf("GetConfig().ConfigFile = %q, want %q", cfg.ConfigFile, configPath)
	}
	if cfg.Port != 9090 {
		t.Fatalf("GetConfig().Port = %d, want 9090", cfg.Port)
	}
}
