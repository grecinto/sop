package sopbackend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadModelCatalogUsesDefaultCatalogWithoutWritingFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	modelCatalogPath := filepath.Join(tempDir, modelCatalogFilename)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	seededDefaults, err := LoadModelCatalog(configPath)
	if err != nil {
		t.Fatalf("loadModelCatalog returned error: %v", err)
	}
	if seededDefaults {
		t.Fatal("expected loadModelCatalog to avoid writing fallback catalog files")
	}
	if len(modelCatalog.LLM) == 0 {
		t.Fatal("expected default llm catalog entries")
	}
	if len(modelCatalog.Embedder) == 0 {
		t.Fatal("expected default embedder catalog entries")
	}
	if modelCatalog.LLM[0].Options[0].Value != "gemini:gemini-2.5-pro" {
		t.Fatalf("unexpected first default llm option: %q", modelCatalog.LLM[0].Options[0].Value)
	}
	if _, err := os.Stat(modelCatalogPath); !os.IsNotExist(err) {
		t.Fatalf("expected no model catalog file to be created, got err=%v", err)
	}
}

func TestDefaultModelCatalogIncludesFallbackLocalKelindarEmbedder(t *testing.T) {
	catalog := DefaultModelCatalog()
	var found bool

	for _, group := range catalog.Embedder {
		for _, option := range group.Options {
			value := strings.ToLower(option.Value)
			label := strings.ToLower(option.Label)
			if strings.Contains(value, "kelindar") || strings.Contains(value, "local") || strings.Contains(label, "kelindar") || strings.Contains(label, "local") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		t.Fatal("expected default embedder catalog to include a local embedder option")
	}
}

func TestLoadModelCatalogPreservesConfiguredFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	modelCatalogPath := filepath.Join(tempDir, modelCatalogFilename)

	contents := `{
		"llm": [{"label": "Vendor", "options": [{"value": "vendor:model-a", "label": "Model A"}]}],
		"embedder": [{"label": "Vendor", "options": [{"value": "vendor:embed-a", "label": "Embed A"}]}]
	}`
	if err := os.WriteFile(modelCatalogPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write model catalog: %v", err)
	}

	seededDefaults, err := LoadModelCatalog(configPath)
	if err != nil {
		t.Fatalf("loadModelCatalog returned error: %v", err)
	}
	if seededDefaults {
		t.Fatal("expected configured model catalog to remain source of truth")
	}
	if got := modelCatalog.LLM[0].Options[0].Value; got != "vendor:model-a" {
		t.Fatalf("expected configured llm option, got %q", got)
	}
	if got := modelCatalog.Embedder[0].Options[0].Value; got != "vendor:embed-a" {
		t.Fatalf("expected configured embedder option, got %q", got)
	}
}

func TestSaveConfigFileDoesNotCreateModelCatalogSidecar(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	modelCatalogPath := filepath.Join(tempDir, modelCatalogFilename)

	config = Config{ConfigFile: configPath, Port: 8080}
	modelCatalog = DefaultModelCatalog()

	saveConfigFile()

	if _, err := os.Stat(modelCatalogPath); !os.IsNotExist(err) {
		t.Fatalf("expected no model catalog sidecar to be created, got err=%v", err)
	}
}

func TestLoadModelCatalogFindsCatalogInParentOfConfigDirectory(t *testing.T) {
	tempDir := t.TempDir()
	toolsDir := filepath.Join(tempDir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir tools dir: %v", err)
	}

	configPath := filepath.Join(toolsDir, "config.json")
	catalogPath := filepath.Join(tempDir, modelCatalogFilename)
	contents := `{
		"llm": [{"label": "Vendor", "options": [{"value": "vendor:model-parent", "label": "Model Parent"}]}],
		"embedder": [{"label": "Vendor", "options": [{"value": "vendor:embed-parent", "label": "Embed Parent"}]}]
	}`
	if err := os.WriteFile(catalogPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write model catalog: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"port": 8080}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	seededDefaults, err := LoadModelCatalog(configPath)
	if err != nil {
		t.Fatalf("loadModelCatalog returned error: %v", err)
	}
	if seededDefaults {
		t.Fatal("expected existing parent catalog to remain source of truth")
	}
	if got := modelCatalog.LLM[0].Options[0].Value; got != "vendor:model-parent" {
		t.Fatalf("expected parent catalog llm option, got %q", got)
	}
	if got := modelCatalog.Embedder[0].Options[0].Value; got != "vendor:embed-parent" {
		t.Fatalf("expected parent catalog embedder option, got %q", got)
	}
}

func TestLoadModelCatalogFallsBackToParentOfWorkingDirectory(t *testing.T) {
	tempDir := t.TempDir()
	toolsDir := filepath.Join(tempDir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir tools dir: %v", err)
	}

	catalogPath := filepath.Join(tempDir, modelCatalogFilename)
	contents := `{
		"llm": [{"label": "Vendor", "options": [{"value": "vendor:model-parent", "label": "Model Parent"}]}],
		"embedder": [{"label": "Vendor", "options": [{"value": "vendor:embed-parent", "label": "Embed Parent"}]}]
	}`
	if err := os.WriteFile(catalogPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write model catalog: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)

	if err := os.Chdir(toolsDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	seededDefaults, err := LoadModelCatalog("")
	if err != nil {
		t.Fatalf("loadModelCatalog returned error: %v", err)
	}
	if seededDefaults {
		t.Fatal("expected existing parent catalog to remain source of truth")
	}
	if got := modelCatalog.LLM[0].Options[0].Value; got != "vendor:model-parent" {
		t.Fatalf("expected parent catalog llm option, got %q", got)
	}
	if got := modelCatalog.Embedder[0].Options[0].Value; got != "vendor:embed-parent" {
		t.Fatalf("expected parent catalog embedder option, got %q", got)
	}
}

func TestLoadModelCatalogFindsCatalogInLibSopDirectory(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	libSopDir := filepath.Join(repoRoot, "lib", "sop")
	if err := os.MkdirAll(libSopDir, 0o755); err != nil {
		t.Fatalf("mkdir lib/sop dir: %v", err)
	}

	catalogPath := filepath.Join(libSopDir, modelCatalogFilename)
	contents := `{
		"llm": [{"label": "Vendor", "options": [{"value": "vendor:model-lib-sop", "label": "Model Lib Sop"}]}],
		"embedder": [{"label": "Vendor", "options": [{"value": "vendor:embed-lib-sop", "label": "Embed Lib Sop"}]}]
	}`
	if err := os.WriteFile(catalogPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write model catalog: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)

	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	seededDefaults, err := LoadModelCatalog(filepath.Join(repoRoot, "config.json"))
	if err != nil {
		t.Fatalf("loadModelCatalog returned error: %v", err)
	}
	if seededDefaults {
		t.Fatal("expected existing lib/sop catalog to remain source of truth")
	}
	if got := modelCatalog.LLM[0].Options[0].Value; got != "vendor:model-lib-sop" {
		t.Fatalf("expected lib/sop catalog llm option, got %q", got)
	}
	if got := modelCatalog.Embedder[0].Options[0].Value; got != "vendor:embed-lib-sop" {
		t.Fatalf("expected lib/sop catalog embedder option, got %q", got)
	}
}
