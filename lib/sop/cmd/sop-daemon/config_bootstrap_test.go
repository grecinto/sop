package main

import (
	"bytes"
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

	var stdout bytes.Buffer
	printDaemonHelpTo(&stdout)

	resolvedPath, err := resolveDaemonConfigLocation("", nil)
	if err != nil {
		t.Fatalf("resolveDaemonConfigLocation() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Config file:") {
		t.Fatalf("expected help output to mention the config file, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), resolvedPath) {
		t.Fatalf("expected help output to include resolved config path %q, got %q", resolvedPath, stdout.String())
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
