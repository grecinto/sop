package main

import (
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop/ai/agent"
)

func TestApplyOverrides(t *testing.T) {
	cfg := &agent.Config{
		StoragePath: "./local-store",
		Agents: []agent.Config{{
			ID:          "child",
			StoragePath: "./child-store",
		}},
	}

	if err := applyOverrides(cfg, "/tmp/override-store", true); err != nil {
		t.Fatalf("applyOverrides returned error: %v", err)
	}

	if !cfg.StubMode {
		t.Fatal("expected stub mode to be enabled")
	}

	if cfg.StoragePath != "/tmp/override-store" {
		t.Fatalf("expected storage path override, got %q", cfg.StoragePath)
	}

	if !cfg.Agents[0].StubMode {
		t.Fatal("expected child agent stub mode to be enabled")
	}

	absPath, err := filepath.Abs("./child-store")
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	if cfg.Agents[0].StoragePath != absPath {
		t.Fatalf("expected child storage path to be absolute, got %q", cfg.Agents[0].StoragePath)
	}
}
