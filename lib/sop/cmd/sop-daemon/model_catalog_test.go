package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModelCatalogReadsCatalogFromWorkingDirectory(t *testing.T) {
	tempDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	contents := `{"llm":[{"label":"Test","options":[{"value":"vendor:model-a","label":"Vendor A"}]}],"embedder":[{"label":"Test","options":[{"value":"vendor:embed-a","label":"Embed A"}]}]}`
	if err := os.WriteFile(catalogPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	catalog, err := loadModelCatalog("")
	if err != nil {
		t.Fatalf("loadModelCatalog: %v", err)
	}
	if len(catalog.LLM) == 0 || len(catalog.LLM[0].Options) == 0 {
		t.Fatalf("expected llm options from loaded catalog, got %#v", catalog.LLM)
	}
	if got := catalog.LLM[0].Options[0].Value; got != "vendor:model-a" {
		t.Fatalf("unexpected llm option: %q", got)
	}
	if len(catalog.Embedder) == 0 || len(catalog.Embedder[0].Options) == 0 {
		t.Fatalf("expected embedder options from loaded catalog, got %#v", catalog.Embedder)
	}
	if got := catalog.Embedder[0].Options[0].Value; got != "vendor:embed-a" {
		t.Fatalf("unexpected embedder option: %q", got)
	}
}
