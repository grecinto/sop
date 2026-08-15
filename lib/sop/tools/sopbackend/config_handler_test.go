package sopbackend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleUninstallSystem_RejectsSystemDBDeletion(t *testing.T) {
	defer func() { config = Config{} }()

	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system-db")
	if err := os.MkdirAll(systemDBPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	config = Config{
		ConfigFile: filepath.Join(tmpDir, "config.json"),
		SystemDB:   &DatabaseConfig{Path: systemDBPath},
	}

	reqBody := map[string]any{"delete_system_db": true}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/system/uninstall", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleUninstallSystem(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleUninstallSystem() status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	if _, err := os.Stat(systemDBPath); err != nil {
		t.Fatalf("system DB path was removed unexpectedly: %v", err)
	}
}

func TestHandleListEnvironments_OnlyListsValidAIStudioConfigFiles(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(oldWd)

	validCfg := `{
		"port": 8080,
		"databases": [],
		"system_db": {
			"name": "system",
			"path": "./.sop-data/system",
			"mode": "standalone"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "valid-env.json"), []byte(validCfg), 0o600); err != nil {
		t.Fatalf("WriteFile(valid-env.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "random.json"), []byte(`{"notes":"not an AI Studio config"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(random.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "not-config.json"), []byte(`[]`), 0o600); err != nil {
		t.Fatalf("WriteFile(not-config.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "other.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile(other.txt) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config/environments", nil)
	rec := httptest.NewRecorder()

	handleListEnvironments(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handleListEnvironments() status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body=%s", err, rec.Body.String())
	}

	if len(names) != 1 || names[0] != "valid-env.json" {
		t.Fatalf("expected only valid-ai-studio config file to be listed, got %#v", names)
	}
}
