package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureConfigPath_CreatesDefaultConfigInUserConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, created, err := ensureConfigPath("")
	if err != nil {
		t.Fatalf("ensureConfigPath() error = %v", err)
	}
	if !created {
		t.Fatal("expected config file to be created")
	}
	if path == "" {
		t.Fatal("expected config path to be set")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to exist at %s: %v", path, err)
	}
	if !strings.HasSuffix(path, filepath.Join("sop", "config.json")) {
		t.Fatalf("expected config path to end with sop/config.json, got %s", path)
	}
}
