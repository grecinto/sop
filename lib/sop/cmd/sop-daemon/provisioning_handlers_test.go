package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestHandleDaemonProvisionStatusRejectsNonGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/provision/status", nil)
	rr := httptest.NewRecorder()

	handleDaemonProvisionStatus(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("handleDaemonProvisionStatus() status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleDaemonProvisionStatusReturnsUnprovisionedWhenConfigMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = ""

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/provision/status", nil)
	rr := httptest.NewRecorder()

	handleDaemonProvisionStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleDaemonProvisionStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp daemonProvisionStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Required {
		t.Fatal("expected provisioning to be required")
	}
	if resp.State != daemonProvisionStateUnprovisioned {
		t.Fatalf("expected unprovisioned state, got %q", resp.State)
	}
}

func TestHandleDaemonProvisionSimpleCreatesConfigAndSystemDB(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "daemon-config.json")

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = configPath

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/provision/simple", strings.NewReader(`{"port":9091}`))
	rr := httptest.NewRecorder()

	handleDaemonProvisionSimple(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleDaemonProvisionSimple() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp daemonProvisionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
	if resp.Mode != "standalone" {
		t.Fatalf("mode = %q, want standalone", resp.Mode)
	}
	if resp.ConfigPath != configPath {
		t.Fatalf("config_path = %q, want %q", resp.ConfigPath, configPath)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file at %s: %v", configPath, err)
	}
	status := currentDaemonProvisionStatus()
	if status.State != daemonProvisionStateReady || !status.HasSystemDB || status.Required {
		t.Fatalf("expected ready status after simple provisioning, got %#v", status)
	}
}

func TestHandleDaemonProvisionSystemRejectsClusteredWithoutRedis(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "daemon-config.json")

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = configPath

	body := `{"system_db":{"path":"` + filepath.Join(t.TempDir(), "system") + `","mode":"clustered"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/provision/system", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handleDaemonProvisionSystem(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleDaemonProvisionSystem() status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "redis_url is required") {
		t.Fatalf("expected redis_url validation error, got %q", rr.Body.String())
	}
}

func TestHandleDaemonProvisionSystemStandaloneUsesRequestedPath(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "daemon-config.json")
	systemPath := filepath.Join(workdir, "custom-system")

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = configPath

	body := `{"port":9092,"system_db":{"path":"` + systemPath + `","mode":"standalone","stores_folders":["` + systemPath + `"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/provision/system", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handleDaemonProvisionSystem(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleDaemonProvisionSystem() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp daemonProvisionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
	if resp.SystemDBPath != systemPath {
		t.Fatalf("system_db_path = %q, want %q", resp.SystemDBPath, systemPath)
	}
	status := currentDaemonProvisionStatus()
	if status.State != daemonProvisionStateReady || status.DefaultSystemDBPath != systemPath {
		t.Fatalf("expected ready status at system path %q, got %#v", systemPath, status)
	}
}

func TestHandleDaemonProvisionSystemAddsDefaultErasureConfigWhenMissing(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "daemon-config.json")
	systemPath := filepath.Join(workdir, "default-system")

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = configPath

	body := `{"system_db":{"path":"` + systemPath + `","mode":"standalone"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/provision/system", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handleDaemonProvisionSystem(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleDaemonProvisionSystem() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	cfg, err := loadDaemonConfigSnapshot(configPath)
	if err != nil {
		t.Fatalf("load daemon config: %v", err)
	}
	if cfg.SystemDB == nil || len(cfg.SystemDB.ErasureConfigs) == 0 {
		t.Fatalf("expected system db to contain an erasure config, got %#v", cfg.SystemDB)
	}
	first := cfg.SystemDB.ErasureConfigs[0]
	if first.Key != "" {
		t.Fatalf("expected default erasure config key, got %q", first.Key)
	}
	if first.DataChunks != 1 || first.ParityChunks != 0 {
		t.Fatalf("expected default erasure config shape 1/0, got %d/%d", first.DataChunks, first.ParityChunks)
	}
}

func TestHandleDaemonProvisionSystemNormalizesDefaultErasureConfigKey(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "daemon-config.json")
	systemPath := filepath.Join(workdir, "default-system")

	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)
	oldPath := localRuntimeConfigPath
	defer func() { localRuntimeConfigPath = oldPath }()
	localRuntimeConfigPath = configPath

	body := `{"system_db":{"path":"` + systemPath + `","mode":"standalone","erasure_config":{"default":{"data_shards_count":1,"parity_shards_count":0,"base_folder_paths_across_drives":["` + systemPath + `"]}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/provision/system", strings.NewReader(body))
	rr := httptest.NewRecorder()

	handleDaemonProvisionSystem(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleDaemonProvisionSystem() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	cfg, err := loadDaemonConfigSnapshot(configPath)
	if err != nil {
		t.Fatalf("load daemon config: %v", err)
	}
	if cfg.SystemDB == nil || len(cfg.SystemDB.ErasureConfigs) == 0 {
		t.Fatalf("expected system db to contain an erasure config, got %#v", cfg.SystemDB)
	}
	if cfg.SystemDB.ErasureConfigs[0].Key != "" {
		t.Fatalf("expected erasure config key to be normalized to empty string, got %q", cfg.SystemDB.ErasureConfigs[0].Key)
	}
}
