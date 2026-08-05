package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
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

func TestPrintConfigPathFlagPrintsResolvedConfigLocation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout bytes.Buffer
	path, err := resolveDaemonConfigLocation("", &stdout)
	if err != nil {
		t.Fatalf("resolveDaemonConfigLocation() error = %v", err)
	}
	if path == "" {
		t.Fatal("expected config path to be set")
	}
	if !strings.Contains(stdout.String(), path) {
		t.Fatalf("expected stdout to include resolved config path %q, got %q", path, stdout.String())
	}
}

func TestPrintDaemonHelpIncludesResolvedConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	resolvedPath, err := resolveDaemonConfigLocation("", nil)
	if err != nil {
		t.Fatalf("resolveDaemonConfigLocation() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(resolvedPath, []byte(`{"port":9090}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var stdout bytes.Buffer
	printDaemonHelpTo(&stdout)

	if !strings.Contains(stdout.String(), "Config file:") {
		t.Fatalf("expected help output to mention the config file, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), resolvedPath) {
		t.Fatalf("expected help output to include resolved config path %q, got %q", resolvedPath, stdout.String())
	}
}

func TestPrintDaemonHelpOmitsConfigPathWhenConfigFileMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout bytes.Buffer
	printDaemonHelpTo(&stdout)

	if strings.Contains(stdout.String(), "config.json") {
		t.Fatalf("expected help output to omit config path when config file is missing, got %q", stdout.String())
	}
}

func TestShouldPrintHelpRecognizesDoubleDashH(t *testing.T) {
	if !shouldPrintHelp([]string{"--h"}) {
		t.Fatal("expected --h to trigger help output")
	}
	if !shouldPrintHelp([]string{"--help"}) {
		t.Fatal("expected --help to trigger help output")
	}
	if shouldPrintHelp([]string{"--port", "9090"}) {
		t.Fatal("expected ordinary flags to keep help disabled")
	}
}

func TestCurrentDaemonProvisionStatusReportsUnprovisionedWhenConfigMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = ""

	status := currentDaemonProvisionStatus()
	if !status.Required {
		t.Fatal("expected daemon provisioning to be required")
	}
	if status.State != daemonProvisionStateUnprovisioned {
		t.Fatalf("expected unprovisioned state, got %q", status.State)
	}
	if status.HasConfigOnDisk {
		t.Fatal("expected no config file on disk")
	}
	if !strings.HasSuffix(status.ConfigPath, filepath.Join("sop", "config.json")) {
		t.Fatalf("expected resolved config path to end with sop/config.json, got %q", status.ConfigPath)
	}
	if !strings.HasSuffix(status.DefaultSystemDBPath, filepath.Join("sop", "system")) {
		t.Fatalf("expected default system db path to end with sop/system, got %q", status.DefaultSystemDBPath)
	}
}

func TestProvisionDaemonSimpleWritesConfigAndTransitionsToReady(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = ""

	configPath, err := provisionDaemonSimple(context.Background(), daemonProvisionSimpleRequest{Port: 9090})
	if err != nil {
		t.Fatalf("provisionDaemonSimple() error = %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to exist at %s: %v", configPath, err)
	}

	status := currentDaemonProvisionStatus()
	if status.Required {
		t.Fatalf("expected provisioning to be complete, got %#v", status)
	}
	if status.State != daemonProvisionStateReady {
		t.Fatalf("expected ready state, got %q", status.State)
	}
	if !status.HasSystemDB {
		t.Fatal("expected simple provisioning to create the system db")
	}
}
