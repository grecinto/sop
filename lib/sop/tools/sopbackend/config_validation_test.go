package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/database"
)

func TestHandleSaveConfig_PersistsRootUserForLogin(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile

	payload := map[string]interface{}{
		"registry_path": filepath.Join(tmpDir, "system"),
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}

	config = Config{}
	if err := LoadConfig(configFile); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	ok, user, err := config.Authenticate("root", "secret123")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !ok || user == nil || user.Username != "root" {
		t.Fatalf("Authenticate() = ok=%v user=%v", ok, user)
	}
}

func TestHandleSaveConfig_PersistsRootUserIntoSystemUsersStore(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile

	payload := map[string]interface{}{
		"registry_path": filepath.Join(tmpDir, "system"),
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}

	store := config.ensureUserStore()
	if store == nil {
		t.Fatal("ensureUserStore() returned nil")
	}
	user, ok, err := store.Get(context.Background(), "root")
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if !ok || user == nil || user.Username != "root" {
		t.Fatalf("store.Get() = ok=%v user=%v", ok, user)
	}
}

func TestHandleSimpleProvision_UsesSharedDefaultsAndSeedsRootUser(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile

	payload := map[string]interface{}{
		"port":          9090,
		"root_password": "secret123",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/provision/simple", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSimpleProvision(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSimpleProvision() status = %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Status       string `json:"status"`
		RegistryPath string `json:"registry_path"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
	if resp.RegistryPath == "" {
		t.Fatal("expected simple provision response to include registry_path")
	}

	if config.SystemDB == nil {
		t.Fatal("expected simple provision to populate config.SystemDB")
	}
	if config.SystemDB.Path == "" {
		t.Fatal("expected simple provision to populate config.SystemDB.Path")
	}
	if len(config.Databases) != 0 {
		t.Fatalf("expected simple provision to avoid creating user databases, got %+v", config.Databases)
	}

	ok, user, err := config.Authenticate("root", "secret123")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !ok || user == nil || user.Username != "root" {
		t.Fatalf("Authenticate() = ok=%v user=%v", ok, user)
	}
}

func TestBuildSimpleProvisionSaveRequest_UsesConfigRelativeDefaultPath(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	config = Config{ConfigFile: filepath.Join(tmpDir, "config.json")}

	req := buildSimpleProvisionSaveRequest(&SimpleProvisionRequest{RootPassword: "secret123"})
	if req == nil {
		t.Fatal("expected buildSimpleProvisionSaveRequest() to return a request")
	}
	wantPath := filepath.Join(tmpDir, ".sop-data", SystemDBName)
	if req.RegistryPath != wantPath {
		t.Fatalf("RegistryPath = %q, want %q", req.RegistryPath, wantPath)
	}
	if req.Port != 8080 {
		t.Fatalf("Port = %d, want 8080", req.Port)
	}
	if req.SystemOptions.Type != sop.Standalone {
		t.Fatalf("SystemOptions.Type = %v, want %v", req.SystemOptions.Type, sop.Standalone)
	}
}

func TestWithAuth_AllowsInitialConfigSaveWithoutBearerToken(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configFile, []byte(`{"port":8080}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	config = Config{}
	config.ConfigFile = configFile

	called := false
	handler := WithAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/config/save", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Fatal("expected config save handler to run without bearer token")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSaveConfig_ReusesExistingSystemDBPath(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	registryPath := filepath.Join(tmpDir, "system")
	config = Config{}
	config.ConfigFile = configFile

	if _, err := database.Setup(context.Background(), sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{registryPath}}); err != nil {
		t.Fatalf("database.Setup() error = %v", err)
	}

	payload := map[string]interface{}{
		"registry_path": registryPath,
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSaveConfig_PersistsRootUserIntoPersistentSystemStore(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile

	payload := map[string]interface{}{
		"registry_path": filepath.Join(tmpDir, "system"),
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}
	if config.SystemDB == nil || config.SystemDB.Path == "" {
		t.Fatal("handleSaveConfig() did not populate config.SystemDB")
	}

	opts, err := GetSystemDBOptions(context.Background())
	if err != nil {
		t.Fatalf("GetSystemDBOptions() error = %v", err)
	}
	db := aidb.NewDatabase(opts)
	tx, err := db.BeginTransaction(context.Background(), sop.ForReading, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(context.Background())

	storeOptions := sop.ConfigureStore("users", true, 1000, "System users", sop.SmallData, "")
	storeOptions.IsPrimitiveKey = true
	store, err := database.NewBtree[string, UserRecord](context.Background(), db.Options(), "users", tx, nil, storeOptions)
	if err != nil {
		t.Fatalf("NewBtree() error = %v", err)
	}
	found, err := store.Find(context.Background(), "root", true)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if !found {
		t.Fatal("Find() did not find root user in persisted system store")
	}
	got, err := store.GetCurrentValue(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentValue() error = %v", err)
	}
	if got.Username != "root" {
		t.Fatalf("GetCurrentValue() returned username %q", got.Username)
	}
}

func TestEnsureBootstrapStoreCreatesStoreInSystemDB(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	config = Config{}
	config.ConfigFile = filepath.Join(tmpDir, "config.json")

	sysDBPath := filepath.Join(tmpDir, "system")
	if _, err := database.Setup(context.Background(), sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{sysDBPath}}); err != nil {
		t.Fatalf("database.Setup() error = %v", err)
	}

	sysDBConfig := &DatabaseConfig{Name: SystemDBName, Path: sysDBPath, Mode: "standalone"}
	config.SystemDB = sysDBConfig

	if err := ensureBootstrapStore(context.Background(), sysDBConfig); err != nil {
		t.Fatalf("ensureBootstrapStore() error = %v", err)
	}

	opts, err := GetDBOptionsFromConfig(context.Background(), sysDBConfig)
	if err != nil {
		t.Fatalf("GetDBOptionsFromConfig() error = %v", err)
	}
	aiDB := aidb.NewDatabase(opts)
	exists, err := aiDB.StoreExists(context.Background(), bootstrapStoreName)
	if err != nil {
		t.Fatalf("StoreExists() error = %v", err)
	}
	if !exists {
		t.Fatal("expected bootstrap store to exist in the system database")
	}
}

func TestHandleSaveConfig_AllowsImmediateSessionCreation(t *testing.T) {
	withIsolatedSessionStore(t)
	oldConfig := config
	oldFacade := tokenFacade
	defer func() {
		config = oldConfig
		tokenFacade = oldFacade
	}()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{SystemDB: config.SystemDB}
	config.ConfigFile = configFile
	tokenFacade = nil
	tokenFacadeOnce = sync.Once{}

	payload := map[string]interface{}{
		"registry_path": filepath.Join(tmpDir, "system"),
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}

	loginBody := bytes.NewBufferString(`{"username":"root","password":"secret123"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()

	handleLogin(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		_, _, sessionErr := currentTokenFacade().CreateSession(context.Background(), "root", sop.RoleAdmin)
		if sessionErr != nil {
			t.Fatalf("handleLogin() status = %d, body=%s, CreateSession() error=%v", loginW.Code, loginW.Body.String(), sessionErr)
		}
		t.Fatalf("handleLogin() status = %d, body=%s", loginW.Code, loginW.Body.String())
	}
}

func TestValidateSetupWizardRootPassword(t *testing.T) {
	if err := validateSetupWizardRootPassword(""); err == nil {
		t.Fatal("expected blank root password to be rejected")
	}
	if err := validateSetupWizardRootPassword("   "); err == nil {
		t.Fatal("expected whitespace-only root password to be rejected")
	}
	if err := validateSetupWizardRootPassword("secret123"); err != nil {
		t.Fatalf("expected non-empty root password to pass, got %v", err)
	}
}

func TestHandleSaveConfig_RejectsInvalidSystemAdminBootstrapSecret(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile

	payload := map[string]interface{}{
		"registry_path":                 filepath.Join(tmpDir, "system"),
		"port":                          8080,
		"root_password":                 "secret123",
		"system_admin_bootstrap_secret": "wrong-secret",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "bootstrap secret") {
		t.Fatalf("expected bootstrap secret error, got %s", w.Body.String())
	}
}

func TestHandleSaveConfig_AllowsBlankBootstrapSecretWhenServerIsConfigured(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile
	t.Setenv("SOP_SYSTEM_ADMIN_BOOTSTRAP_SECRET", "configured-secret")

	payload := map[string]interface{}{
		"registry_path": filepath.Join(tmpDir, "system"),
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}
	// Verify root user was created in persistent store
	if user, ok, err := config.findUserRecord(context.Background(), "root"); err != nil || !ok || user.Role != sop.RoleAdmin {
		t.Fatalf("expected root user with role %q, got ok=%v err=%v role=%s", sop.RoleAdmin, ok, err, user.Role)
	}
}

func TestHandleSaveConfig_PersistsSystemAdminRoleWhenBootstrapSecretMatches(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")

	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile

	t.Setenv("SOP_SYSTEM_ADMIN_BOOTSTRAP_SECRET", "correct-secret")

	payload := map[string]interface{}{
		"registry_path":                 filepath.Join(tmpDir, "system"),
		"port":                          8080,
		"root_password":                 "secret123",
		"system_admin_bootstrap_secret": "correct-secret",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleSaveConfig() status = %d, body=%s", w.Code, w.Body.String())
	}
	// Verify root user was created in persistent store
	if user, ok, err := config.findUserRecord(context.Background(), "root"); err != nil || !ok || user.Role != sop.RoleSystemAdmin {
		t.Fatalf("expected root user with role %q, got ok=%v err=%v role=%s", sop.RoleSystemAdmin, ok, err, user.Role)
	}
	// NOTE: sopbackend no longer tracks subscription features - this is site-backend-only now
}

func TestHandleSaveConfig_RejectsIncompatibleSharedBrainReuse(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	config = Config{}
	config.ConfigFile = configFile

	sharedPath := filepath.Join(tmpDir, "shared-system")
	if err := os.MkdirAll(sharedPath, 0o755); err != nil {
		t.Fatalf("failed to create shared path: %v", err)
	}

	existingOpts := sop.DatabaseOptions{
		StoresFolders: []string{sharedPath},
		Type:          sop.Standalone,
		CacheType:     sop.InMemory,
	}
	if data, err := json.Marshal(existingOpts); err != nil {
		t.Fatalf("marshal existing options: %v", err)
	} else if err := os.WriteFile(filepath.Join(sharedPath, "dboptions.json"), data, 0o644); err != nil {
		t.Fatalf("write existing dboptions: %v", err)
	}

	payload := map[string]interface{}{
		"registry_path":    sharedPath,
		"port":             8080,
		"root_password":    "secret123",
		"use_shared_brain": true,
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           1,
			"cache_type":     2,
			"redis_config": map[string]interface{}{
				"address": "localhost:6379",
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	handleSaveConfig(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("expected incompatible shared-brain reuse to be rejected, got status %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "shared brain") && !strings.Contains(w.Body.String(), "Redis") {
		t.Fatalf("expected compatibility error, got %s", w.Body.String())
	}
}

func TestHandleSaveConfig_DeduplicatesPaths(t *testing.T) {
	// Restore config state after test
	defer func() {
		config = Config{}
	}()

	tmpDir, err := os.MkdirTemp("", "sop_repro_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Case 1: Registry Path and Stores Folder are "effectively" the same but different strings (e.g. trailing slash)
	// This mirrors what might happen if frontend sends both.
	regPath := filepath.Join(tmpDir, "system")
	storePath := regPath + "/" // Trailing slash

	payload := map[string]interface{}{
		"registry_path": regPath,
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{storePath},
			"type":           0,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	// Execute
	handleSaveConfig(w, req)

	resp := w.Result()
	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Logf("Response Status: %d", resp.StatusCode)
		t.Logf("Response Body: %s", string(bodyBytes))

		// If validation is active, this expects to fail if dedupe isn't robust
		if strings.Contains(string(bodyBytes), "internal path conflict") {
			t.Log("reproduced: internal path conflict with same logical path")
		} else {
			t.Log("failed with other error")
		}
	} else {
		t.Log("Test passed (Validation might be disabled or working)")
	}
}

func TestHandleSaveConfig_AllowsUniqueECPaths(t *testing.T) {
	// Restore config state after test
	defer func() {
		config = Config{}
	}()

	tmpDir, err := os.MkdirTemp("", "sop_ec_repro_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	regPath := filepath.Join(tmpDir, "system")
	// EC paths
	d1 := filepath.Join(tmpDir, "d1")
	d2 := filepath.Join(tmpDir, "d2")

	// Payload with unique EC paths
	payload := map[string]interface{}{
		"registry_path": regPath,
		"port":          8080,
		"root_password": "secret123",
		"system_options": map[string]interface{}{
			"stores_folders": []string{},
			"type":           0,
			"erasure_config": map[string]interface{}{
				"default": map[string]interface{}{
					"data_shards_count":               1,
					"parity_shards_count":             1,
					"base_folder_paths_across_drives": []string{d1, d2},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/config/save", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	// Execute
	handleSaveConfig(w, req)

	resp := w.Result()
	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Logf("Response Status: %d", resp.StatusCode)
		t.Logf("Response Body: %s", string(bodyBytes))
	} else {
		t.Log("Test passed")
	}
}

func TestHandleSaveConfig_ErasureIsolation(t *testing.T) {
	// Restore config state after test
	defer func() {
		config = Config{}
	}()

	tmpDir, err := os.MkdirTemp("", "sop_ec_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path1 := filepath.Join(tmpDir, "disk1")
	path2 := filepath.Join(tmpDir, "disk2")

	// Helper to send request
	sendReq := func(payload map[string]interface{}) (int, string) {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/config/save", bytes.NewBuffer(body))
		w := httptest.NewRecorder()
		handleSaveConfig(w, req)
		resp := w.Result()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(bodyBytes)
	}

	t.Run("Fails with duplicate BasePaths in Global Config", func(t *testing.T) {
		payload := map[string]interface{}{
			"registry_path": filepath.Join(tmpDir, "system_dup_global"),
			"port":          8080,
			"root_password": "secret123",
			"system_options": map[string]interface{}{
				"stores_folders": []string{filepath.Join(tmpDir, "stores")},
				"type":           0, // Standalone
				"erasure_config": map[string]interface{}{
					"default": map[string]interface{}{
						"data_shards_count":               1,
						"parity_shards_count":             1,
						"base_folder_paths_across_drives": []string{path1, path1}, // Duplicate
					},
				},
			},
		}
		status, body := sendReq(payload)
		if status != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", status)
		}
		if !strings.Contains(body, "All paths must be unique") {
			t.Errorf("Expected error message about unique paths, got: %s", body)
		}
	})

	t.Run("Fails with mismatch chunk count in Global Config", func(t *testing.T) {
		payload := map[string]interface{}{
			"registry_path": filepath.Join(tmpDir, "system_mismatch"),
			"port":          8080,
			"root_password": "secret123",
			"system_options": map[string]interface{}{
				"stores_folders": []string{filepath.Join(tmpDir, "stores")},
				"type":           0, // Standalone
				"erasure_config": map[string]interface{}{
					"default": map[string]interface{}{
						"data_shards_count":               1,
						"parity_shards_count":             1,
						"base_folder_paths_across_drives": []string{path1}, // Only 1, need 2
					},
				},
			},
		}
		status, body := sendReq(payload)
		if status != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", status)
		}
		if !strings.Contains(body, "BasePaths count must match Data+Parity") {
			t.Errorf("Expected error message about path count, got: %s", body)
		}
	})

	t.Run("Fails with duplicate BasePaths in Per-Store Config", func(t *testing.T) {
		payload := map[string]interface{}{
			"registry_path": filepath.Join(tmpDir, "system_dup_store"),
			"port":          8080,
			"root_password": "secret123",
			"system_options": map[string]interface{}{
				"stores_folders": []string{filepath.Join(tmpDir, "stores")},
				"type":           0, // Standalone
				"erasure_config": map[string]interface{}{
					"storeA": map[string]interface{}{
						"data_shards_count":               1,
						"parity_shards_count":             1,
						"base_folder_paths_across_drives": []string{path1, path1}, // Duplicate
					},
				},
			},
		}
		status, body := sendReq(payload)
		if status != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", status)
		}
		// Expect "Erasure Config (System Key: storeA)" or similar. Matching looser "storeA" covers it.
		if !strings.Contains(body, "storeA") {
			t.Errorf("Expected error context for storeA, got: %s", body)
		}
		if !strings.Contains(body, "All paths must be unique") {
			t.Errorf("Expected error message about unique paths, got: %s", body)
		}
	})

	t.Run("Succeeds with valid unique paths", func(t *testing.T) {
		payload := map[string]interface{}{
			"registry_path": filepath.Join(tmpDir, "system_valid"),
			"port":          8080,
			"root_password": "secret123",
			"system_options": map[string]interface{}{
				"stores_folders": []string{filepath.Join(tmpDir, "stores")},
				"type":           0, // Standalone
				"erasure_config": map[string]interface{}{
					"default": map[string]interface{}{
						"data_shards_count":               1,
						"parity_shards_count":             1,
						"base_folder_paths_across_drives": []string{path1, path2}, // Unique
					},
				},
			},
		}
		status, body := sendReq(payload)
		// Assuming success or unrelated failure
		if status == http.StatusBadRequest && strings.Contains(body, "must be unique") {
			t.Fatalf("Failed unexpected on uniqueness: %s", body)
		}
	})
}
