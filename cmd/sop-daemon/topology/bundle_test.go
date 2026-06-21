package topology

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateBundle(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	storageDir := filepath.Join(tmpDir, "storage")
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		t.Fatalf("mkdir storage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageDir, "data.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write data: %v", err)
	}

	bundlePath := filepath.Join(tmpDir, "bundle.zip")
	manifest, err := CreateBundle(BundleOptions{
		ConfigPath:  configPath,
		StoragePath: storageDir,
		OutputPath:  bundlePath,
		Name:        "demo-rag",
		Version:     "1.0.0",
	})
	if err != nil {
		t.Fatalf("CreateBundle returned error: %v", err)
	}

	if manifest.Name != "demo-rag" {
		t.Fatalf("expected manifest name demo-rag, got %q", manifest.Name)
	}
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("expected bundle file to exist: %v", err)
	}
}
