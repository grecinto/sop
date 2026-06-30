package sopbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeRuntimeRequiresEncryptionKeyForConfigPersistence(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	_, err := InitializeRuntime(context.Background(), configPath, RuntimeOptions{}, 0, nil)
	if err == nil {
		t.Fatal("InitializeRuntime() error = nil, want missing encryption key error")
	}
	if !strings.Contains(err.Error(), "SOP_ENCRYPTION_KEY") {
		t.Fatalf("InitializeRuntime() error = %v, want message containing SOP_ENCRYPTION_KEY", err)
	}
}

func TestInitializeRuntimeRequiresSessionSecretInProductionMode(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SOP_SESSION_SECRET", "")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	_, err := InitializeRuntime(context.Background(), configPath, RuntimeOptions{}, 0, ptrBool(true))
	if err == nil {
		t.Fatal("InitializeRuntime() error = nil, want missing session secret error")
	}
	if !strings.Contains(err.Error(), "SOP_SESSION_SECRET") {
		t.Fatalf("InitializeRuntime() error = %v, want message containing SOP_SESSION_SECRET", err)
	}
}

func ptrBool(v bool) *bool {
	return &v
}

func TestInitializeRuntimeBootstrapsConfigAndReturnsResolvedPath(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SOP_SESSION_SECRET", "session-secret")

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
