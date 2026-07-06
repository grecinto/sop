package main

import (
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func withIsolatedSystemDB(t *testing.T) {
	t.Helper()

	oldCfg := sopbackend.GetConfig()
	tmpDir := t.TempDir()
	config := sopbackend.Config{
		SystemDB: &sopbackend.DatabaseConfig{
			Name: sopbackend.SystemDBName,
			Path: tmpDir,
			Mode: "standalone",
		},
	}
	sopbackend.SetConfig(config)

	t.Cleanup(func() {
		sopbackend.SetConfig(oldCfg)
	})
}
