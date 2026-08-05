package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/tools/sopbackend"
)

type daemonProvisionState string

const (
	daemonProvisionStateUnprovisioned daemonProvisionState = "unprovisioned"
	daemonProvisionStatePartial       daemonProvisionState = "partial"
	daemonProvisionStateReady         daemonProvisionState = "ready"
)

type daemonProvisionStatusResponse struct {
	Required            bool                 `json:"required"`
	State               daemonProvisionState `json:"state"`
	ConfigPath          string               `json:"config_path,omitempty"`
	HasConfigOnDisk     bool                 `json:"has_config_on_disk"`
	HasSystemDB         bool                 `json:"has_system_db"`
	DefaultSystemDBPath string               `json:"default_system_db_path,omitempty"`
	DefaultPort         int                  `json:"default_port"`
	AdvancedSupported   bool                 `json:"advanced_supported"`
	Mode                string               `json:"mode,omitempty"`
	Reason              string               `json:"reason,omitempty"`
}

type daemonProvisionSimpleRequest struct {
	Port int `json:"port,omitempty"`
}

type daemonProvisionSystemRequest struct {
	Port     int                         `json:"port,omitempty"`
	SystemDB daemonProvisionSystemConfig `json:"system_db"`
}

type daemonProvisionSystemConfig struct {
	Path            string                          `json:"path"`
	Mode            string                          `json:"mode,omitempty"`
	RedisURL        string                          `json:"redis_url,omitempty"`
	RegistryHashMod int                             `json:"registry_hash_mod,omitempty"`
	StoresFolders   []string                        `json:"stores_folders,omitempty"`
	ErasureConfig   map[string]daemonErasureRequest `json:"erasure_config,omitempty"`
}

type daemonErasureRequest struct {
	DataShardsCount             int      `json:"data_shards_count"`
	ParityShardsCount           int      `json:"parity_shards_count"`
	BaseFolderPathsAcrossDrives []string `json:"base_folder_paths_across_drives"`
}

type daemonProvisionResponse struct {
	Status        string `json:"status"`
	ConfigPath    string `json:"config_path,omitempty"`
	SystemDBPath  string `json:"system_db_path,omitempty"`
	Mode          string `json:"mode,omitempty"`
	RestartNeeded bool   `json:"restart_required"`
}

func handleDaemonProvisionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	respondJSON(w, http.StatusOK, currentDaemonProvisionStatus())
}

func handleDaemonProvisionSimple(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req daemonProvisionSimpleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body: " + err.Error()})
		return
	}
	configPath, err := provisionDaemonSimple(r.Context(), req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status := currentDaemonProvisionStatus()
	respondJSON(w, http.StatusOK, daemonProvisionResponse{
		Status:        "ok",
		ConfigPath:    configPath,
		SystemDBPath:  status.DefaultSystemDBPath,
		Mode:          "standalone",
		RestartNeeded: false,
	})
}

func handleDaemonProvisionSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req daemonProvisionSystemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body: " + err.Error()})
		return
	}
	configPath, mode, systemPath, err := provisionDaemonSystem(r.Context(), req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, daemonProvisionResponse{
		Status:        "ok",
		ConfigPath:    configPath,
		SystemDBPath:  systemPath,
		Mode:          mode,
		RestartNeeded: false,
	})
}

func currentDaemonProvisionStatus() daemonProvisionStatusResponse {
	configPath, err := currentDaemonConfigPath()
	if err != nil {
		return daemonProvisionStatusResponse{
			Required:          true,
			State:             daemonProvisionStateUnprovisioned,
			DefaultPort:       9090,
			AdvancedSupported: true,
			Reason:            err.Error(),
		}
	}
	defaultSystemDBPath := filepath.Join(filepath.Dir(configPath), "system")
	status := daemonProvisionStatusResponse{
		Required:            true,
		State:               daemonProvisionStateUnprovisioned,
		ConfigPath:          configPath,
		DefaultSystemDBPath: defaultSystemDBPath,
		DefaultPort:         daemonDefaultPort(),
		AdvancedSupported:   true,
	}
	info, statErr := os.Stat(configPath)
	if statErr != nil || info.IsDir() {
		if statErr != nil && !os.IsNotExist(statErr) {
			status.Reason = statErr.Error()
		}
		return status
	}
	status.HasConfigOnDisk = true
	daemonCfg, err := loadDaemonConfigSnapshot(configPath)
	if err != nil {
		status.State = daemonProvisionStatePartial
		status.Reason = err.Error()
		return status
	}
	if daemonCfg.SystemDB == nil || strings.TrimSpace(daemonCfg.SystemDB.Path) == "" {
		status.State = daemonProvisionStatePartial
		status.Reason = "system_db is missing from config"
		return status
	}
	status.DefaultSystemDBPath = daemonCfg.SystemDB.Path
	status.Mode = strings.TrimSpace(daemonCfg.SystemDB.Mode)
	dbOptionsPath := filepath.Join(daemonCfg.SystemDB.Path, "dboptions.json")
	if _, err := os.Stat(dbOptionsPath); err == nil {
		status.HasSystemDB = true
		status.Required = false
		status.State = daemonProvisionStateReady
		status.Reason = ""
		return status
	}
	status.State = daemonProvisionStatePartial
	status.Reason = "system database files are missing"
	return status
}

func provisionDaemonSimple(ctx context.Context, req daemonProvisionSimpleRequest) (string, error) {
	configPath, err := currentDaemonConfigPath()
	if err != nil {
		return "", err
	}
	defaultSystemDBPath := filepath.Join(filepath.Dir(configPath), "system")
	port := req.Port
	if port <= 0 {
		port = daemonDefaultPort()
	}
	if err := validateDaemonProvisionPaths([]string{defaultSystemDBPath}); err != nil {
		return "", err
	}
	dbCfg := sopbackend.DatabaseConfig{
		Name:     sopbackend.SystemDBName,
		Path:     defaultSystemDBPath,
		Mode:     "standalone",
		IsSystem: true,
	}
	if err := writeAndActivateDaemonConfig(ctx, configPath, dbCfg, port); err != nil {
		return "", err
	}
	return configPath, nil
}

func provisionDaemonSystem(ctx context.Context, req daemonProvisionSystemRequest) (string, string, string, error) {
	configPath, err := currentDaemonConfigPath()
	if err != nil {
		return "", "", "", err
	}
	mode := strings.ToLower(strings.TrimSpace(req.SystemDB.Mode))
	if mode == "" {
		mode = "standalone"
	}
	if mode != "standalone" && mode != "clustered" {
		return "", "", "", fmt.Errorf("system_db.mode must be 'standalone' or 'clustered'")
	}
	systemPath := sanitizeDaemonPath(req.SystemDB.Path)
	if systemPath == "" {
		return "", "", "", fmt.Errorf("system_db.path is required")
	}
	storesFolders := sanitizeDaemonPathList(req.SystemDB.StoresFolders)
	if len(storesFolders) == 0 {
		storesFolders = []string{systemPath}
	}
	if mode == "clustered" && strings.TrimSpace(req.SystemDB.RedisURL) == "" {
		return "", "", "", fmt.Errorf("system_db.redis_url is required for clustered mode")
	}
	erasureEntries, err := convertDaemonErasureConfig(req.SystemDB.ErasureConfig)
	if err != nil {
		return "", "", "", err
	}
	paths := append([]string{}, storesFolders...)
	for _, ec := range erasureEntries {
		paths = append(paths, ec.BasePaths...)
	}
	if err := validateDaemonProvisionPaths(paths); err != nil {
		return "", "", "", err
	}
	dbCfg := sopbackend.DatabaseConfig{
		Name:           sopbackend.SystemDBName,
		Path:           systemPath,
		StoresFolders:  storesFolders,
		Mode:           mode,
		RedisURL:       strings.TrimSpace(req.SystemDB.RedisURL),
		IsSystem:       true,
		ErasureConfigs: erasureEntries,
	}
	port := req.Port
	if port <= 0 {
		port = daemonDefaultPort()
	}
	if err := writeAndActivateDaemonConfig(ctx, configPath, dbCfg, port); err != nil {
		return "", "", "", err
	}
	return configPath, mode, systemPath, nil
}

func writeAndActivateDaemonConfig(ctx context.Context, configPath string, systemDB sopbackend.DatabaseConfig, port int) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if systemDB.Name == "" {
		systemDB.Name = sopbackend.SystemDBName
	}
	if port <= 0 {
		port = daemonDefaultPort()
	}
	cfg := sopbackend.Config{
		Port:           port,
		Databases:      []sopbackend.DatabaseConfig{},
		PageSize:       40,
		SystemDB:       &systemDB,
		ProductionMode: true,
	}
	if err := sopbackend.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("write daemon config: %w", err)
	}
	if err := sopbackend.LoadConfig(configPath); err != nil {
		return fmt.Errorf("load daemon config: %w", err)
	}
	sopbackend.SetConfigFile(configPath)
	if _, err := sopbackend.LoadModelCatalog(configPath); err != nil {
		return fmt.Errorf("load model catalog: %w", err)
	}
	if _, err := sopbackend.GetSystemDBOptions(ctx); err != nil {
		return fmt.Errorf("provision system db: %w", err)
	}
	if cfg := sopbackend.GetConfig(); cfg.SystemDB != nil {
		if err := bootstrapSOPKnowledgeBaseIfNeeded(
			ctx,
			cfg.SystemDB.Name,
			func(ctx context.Context, spaceName string) (bool, error) {
				dbOpts, err := sopbackend.GetDBOptions(ctx, cfg.SystemDB.Name)
				if err != nil {
					return false, fmt.Errorf("resolve DB options for bootstrap: %w", err)
				}
				db := aidb.NewDatabase(dbOpts)
				return db.KnowledgeBaseExists(ctx, spaceName)
			},
			func(ctx context.Context, dbName string) error {
				return sopbackend.PreloadSpaceFromTemplate(ctx, ai.DefaultKBName, dbName, nil)
			},
		); err != nil {
			return fmt.Errorf("bootstrap built-in SOP knowledge base: %w", err)
		}
	}
	return nil

}

func currentDaemonConfigPath() (string, error) {
	if strings.TrimSpace(localRuntimeConfigPath) != "" {
		return localRuntimeConfigPath, nil
	}
	return resolveDaemonConfigLocation("", nil)
}

func loadDaemonConfigSnapshot(path string) (*sopbackend.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var cfg sopbackend.Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func daemonDefaultPort() int {
	if cfg := sopbackend.GetConfig(); cfg.Port > 0 {
		return cfg.Port
	}
	return 9090
}

func sanitizeDaemonPath(p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func sanitizeDaemonPathList(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		cleaned := sanitizeDaemonPath(path)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

func convertDaemonErasureConfig(input map[string]daemonErasureRequest) ([]sopbackend.ErasureConfigEntry, error) {
	if len(input) == 0 {
		return nil, nil
	}
	entries := make([]sopbackend.ErasureConfigEntry, 0, len(input))
	for key, value := range input {
		paths := sanitizeDaemonPathList(value.BaseFolderPathsAcrossDrives)
		if value.DataShardsCount <= 0 {
			return nil, fmt.Errorf("erasure config %q requires data_shards_count > 0", key)
		}
		if value.ParityShardsCount < 0 {
			return nil, fmt.Errorf("erasure config %q requires parity_shards_count >= 0", key)
		}
		if len(paths) != value.DataShardsCount+value.ParityShardsCount {
			return nil, fmt.Errorf("erasure config %q requires %d base paths", key, value.DataShardsCount+value.ParityShardsCount)
		}
		entries = append(entries, sopbackend.ErasureConfigEntry{
			Key:          key,
			DataChunks:   value.DataShardsCount,
			ParityChunks: value.ParityShardsCount,
			BasePaths:    paths,
		})
	}
	return entries, nil
}

func validateDaemonProvisionPaths(paths []string) error {
	cleaned := sanitizeDaemonPathList(paths)
	if len(cleaned) == 0 {
		return fmt.Errorf("at least one writable path is required")
	}
	seenAbs := map[string]struct{}{}
	for _, path := range cleaned {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve path %q: %w", path, err)
		}
		if _, ok := seenAbs[absPath]; ok {
			return fmt.Errorf("path conflict: %q is duplicated", path)
		}
		seenAbs[absPath] = struct{}{}
	}
	for _, path := range cleaned {
		dir := path
		for {
			info, err := os.Stat(dir)
			if err == nil {
				if !info.IsDir() {
					return fmt.Errorf("path %q exists but is not a directory", dir)
				}
				testFile, err := os.CreateTemp(dir, ".sop_daemon_perm_")
				if err != nil {
					return fmt.Errorf("write permission denied for path %q via %q: %w", path, dir, err)
				}
				testFile.Close()
				_ = os.Remove(testFile.Name())
				break
			}
			if !os.IsNotExist(err) {
				return fmt.Errorf("cannot access path %q: %w", dir, err)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return fmt.Errorf("path %q or its ancestors do not exist or are inaccessible", path)
			}
			dir = parent
		}
	}
	return nil
}
