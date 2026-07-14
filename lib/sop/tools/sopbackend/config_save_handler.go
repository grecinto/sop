package sopbackend

import (
	"bytes"
	"cmp" // needed for cmp.Compare
	"context"
	"encoding/json"
	"fmt"
	"io"
	log "log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/model"
	"github.com/sharedcode/sop/database"
	"github.com/sharedcode/sop/fs"
)

// --- Helper Types for handleSaveConfig ---

type ErasureConfigRequest struct {
	Key    string                  `json:"key"`
	Config sop.ErasureCodingConfig `json:"config"`
}

type UserDBRequest struct {
	Name            string              `json:"name"`
	Path            string              `json:"path"`
	UseSharedDB     bool                `json:"use_shared_db"`
	PopulateDemo    bool                `json:"populate_demo"`
	DatabaseOptions sop.DatabaseOptions `json:"options"`
}

type SaveConfigRequest struct {
	RegistryPath string `json:"registry_path"`
	Port         int    `json:"port"`
	RootPassword string `json:"root_password,omitempty"`

	SystemAdminBootstrapSecret       string `json:"system_admin_bootstrap_secret,omitempty"`
	VectorizationRecoveryStoragePath string `json:"vectorization_recovery_storage_path,omitempty"`

	UseSharedBrain bool                `json:"use_shared_brain"`
	SystemOptions  sop.DatabaseOptions `json:"system_options"`
	Databases      []UserDBRequest     `json:"databases"`
}

func validateSetupWizardRootPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("root password is required")
	}
	return nil
}

func validateSetupWizardSystemAdminSecret(secret string) error {
	configuredSecret := strings.TrimSpace(os.Getenv("SOP_SYSTEM_ADMIN_BOOTSTRAP_SECRET"))
	if configuredSecret == "" {
		if strings.TrimSpace(secret) == "" {
			return nil
		}
		return fmt.Errorf("system admin bootstrap secret is not configured on this server")
	}

	if strings.TrimSpace(secret) == "" {
		return nil
	}
	if secret != configuredSecret {
		return fmt.Errorf("system admin bootstrap secret is invalid")
	}

	return nil
}

const bootstrapStoreName = "bootstrap_metadata"

func ensureBootstrapStore(ctx context.Context, db *DatabaseConfig) error {
	target := db
	if target == nil {
		target = config.SystemDB
	}
	if target == nil {
		if opts, err := GetSystemDBOptions(ctx); err == nil {
			return ensureBootstrapStoreInOptions(ctx, opts)
		}
		return nil
	}

	opts, err := GetDBOptionsFromConfig(ctx, target)
	if err != nil {
		return err
	}
	return ensureBootstrapStoreInOptions(ctx, opts)
}

func ensureBootstrapStoreInOptions(ctx context.Context, opts sop.DatabaseOptions) error {
	tx, err := database.BeginTransaction(ctx, opts, sop.ForWriting)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := database.NewBtree[string, string](ctx, opts, bootstrapStoreName, tx, cmp.Compare[string]); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already exists") || strings.Contains(strings.ToLower(err.Error()), "exists") {
			return nil
		}
		tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func ensureRootUserFromPassword(password string, role string, features []string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return nil
	}

	rootUsername := normalizeUsername("root")
	for i := range config.Users {
		if normalizeUsername(config.Users[i].Username) == rootUsername {
			config.Users = append(config.Users[:i], config.Users[i+1:]...)
			break
		}
	}

	if role == "" {
		role = sop.RoleAdmin
	}

	if len(features) == 0 {
		features = nil
	}

	if err := config.SetUserWithFeatures("root", password, role, features); err != nil {
		return err
	}
	if userStore := config.ensureUserStore(); userStore != nil {
		if user, ok, err := userStore.Get(context.Background(), rootUsername); err == nil && ok && user != nil {
			return nil
		}
	}
	return nil
}

func normalizeRedisConnection(cfg *sop.RedisCacheConfig) string {
	if cfg == nil {
		return ""
	}
	if strings.TrimSpace(cfg.URL) != "" {
		return strings.ToLower(strings.TrimSpace(cfg.URL))
	}
	return fmt.Sprintf("%s|%s|%d", strings.TrimSpace(cfg.Address), strings.TrimSpace(cfg.Password), cfg.DB)
}

func validateSharedDatabaseReuse(existingOpts, requestedOpts sop.DatabaseOptions) error {
	if existingOpts.GetDatabaseType() != sop.Clustered || requestedOpts.GetDatabaseType() != sop.Clustered {
		return fmt.Errorf("shared database reuse is only allowed when both configurations are clustered with Redis coordination")
	}
	if existingOpts.RedisConfig == nil || requestedOpts.RedisConfig == nil {
		return fmt.Errorf("shared database reuse requires both configurations to use the same Redis connection")
	}
	if normalizeRedisConnection(existingOpts.RedisConfig) != normalizeRedisConnection(requestedOpts.RedisConfig) {
		return fmt.Errorf("shared database reuse requires the same Redis connection")
	}
	return nil
}

func loadDatabaseOptionsForReuse(path string) (sop.DatabaseOptions, error) {
	if path == "" {
		return sop.DatabaseOptions{}, fmt.Errorf("database path is empty")
	}
	return database.GetOptions(context.Background(), path)
}

// handleSaveConfig writes the provided configuration to the specified file path.
// Broken down into modular steps for readability/maintainability.
func handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	log.Debug("TRACE: handleSaveConfig called")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Parse Request
	req, err := parseSaveConfigRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if err := validateSetupWizardRootPassword(req.RootPassword); err != nil {
		http.Error(w, fmt.Sprintf("Validation Failed: %v", err), http.StatusBadRequest)
		return
	}
	if err := validateSetupWizardSystemAdminSecret(req.SystemAdminBootstrapSecret); err != nil {
		http.Error(w, fmt.Sprintf("Validation Failed: %v", err), http.StatusBadRequest)
		return
	}

	// 2. Validate Safety (Paths, Conflicts, Permissions)
	if err := validatePathConflictsAndPermissions(req); err != nil {
		log.Error(fmt.Sprintf("TRACE: Validation Failed (SaveConfig): %v", err))
		http.Error(w, fmt.Sprintf("Safety Check Failed: %v", err), http.StatusBadRequest)
		return
	}

	// 3. Update global config settings (Memory)
	if req.Port > 0 {
		config.Port = req.Port
	}
	if strings.TrimSpace(req.VectorizationRecoveryStoragePath) != "" {
		config.VectorizationRecoveryStoragePath = sanitizePath(req.VectorizationRecoveryStoragePath)
	} else {
		config.VectorizationRecoveryStoragePath = ""
	}

	// 4. Setup System DB (I/O)
	ctx := r.Context()
	sysDBConfig, sysCreated, err := setupSystemDB(ctx, req)
	if err != nil {
		http.Error(w, fmt.Sprintf("System DB Setup Failed: %v", err), http.StatusInternalServerError)
		return
	}
	if sysDBConfig != nil {
		config.SystemDB = sysDBConfig
	}

	// 5. Setup User DBs (I/O)
	newDBs, err := setupUserDBs(ctx, req)
	if err != nil {
		// Orchestrate Global Rollback
		log.Warn(fmt.Sprintf("Transaction Failed. Rolling back SystemDB created? %v", sysCreated))
		if sysCreated {
			cleanupSystemDB(req)
		}
		http.Error(w, fmt.Sprintf("User DB Setup Failed: %v", err), http.StatusInternalServerError)
		return
	}
	// Append valid DBs
	config.Databases = append(config.Databases, newDBs...)

	// 6. Seed the root user into the now-configured system-backed store as part of provisioning.
	if req.RootPassword != "" {
		rootRole := sop.RoleAdmin
		rootFeatures := []string(nil)
		if strings.TrimSpace(req.SystemAdminBootstrapSecret) != "" {
			if err := validateSetupWizardSystemAdminSecret(req.SystemAdminBootstrapSecret); err == nil {
				rootRole = sop.RoleSystemAdmin
				rootFeatures = []string{"hq_runtime", "daemon_runtime"}
			}
		}
		if err := ensureRootUserFromPassword(req.RootPassword, rootRole, rootFeatures); err != nil {
			http.Error(w, fmt.Sprintf("Failed to seed root user: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if err := config.ensureProvisionedUsersStore(ctx); err != nil {
		http.Error(w, fmt.Sprintf("Failed to initialize users store: %v", err), http.StatusInternalServerError)
		return
	}
	if err := config.syncUsersStoreFromConfig(ctx); err != nil {
		http.Error(w, fmt.Sprintf("Failed to persist users store: %v", err), http.StatusInternalServerError)
		return
	}

	// 7. Save Config File (Disk)
	if err := saveConfig(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write config file: %v", err), http.StatusInternalServerError)
		return
	}

	markProvisioningComplete()

	// 8. Reload Agents
	if err := initAgents(context.Background()); err != nil {
		http.Error(w, fmt.Sprintf("Failed to initialize agents: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Configuration saved successfully"})
}

func parseSaveConfigRequest(r *http.Request) (*SaveConfigRequest, error) {
	var req SaveConfigRequest
	bodyBytes, _ := io.ReadAll(r.Body)
	log.Debug(fmt.Sprintf("RAW CONFIG PAYLOAD: %s", string(bodyBytes)))
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}

	// Sanitization
	req.RegistryPath = sanitizePath(req.RegistryPath)
	for i, sf := range req.SystemOptions.StoresFolders {
		req.SystemOptions.StoresFolders[i] = sanitizePath(sf)
	}
	// Sanitize System Erasure Configs
	for key, config := range req.SystemOptions.ErasureConfig {
		for i, p := range config.BaseFolderPathsAcrossDrives {
			config.BaseFolderPathsAcrossDrives[i] = sanitizePath(p)
		}
		req.SystemOptions.ErasureConfig[key] = config
	}

	for i := range req.Databases {
		req.Databases[i].Path = sanitizePath(req.Databases[i].Path)
		for j, sf := range req.Databases[i].DatabaseOptions.StoresFolders {
			req.Databases[i].DatabaseOptions.StoresFolders[j] = sanitizePath(sf)
		}
		// Sanitize User DB Erasure Configs
		for key, config := range req.Databases[i].DatabaseOptions.ErasureConfig {
			for k, p := range config.BaseFolderPathsAcrossDrives {
				config.BaseFolderPathsAcrossDrives[k] = sanitizePath(p)
			}
			req.Databases[i].DatabaseOptions.ErasureConfig[key] = config
		}
	}
	return &req, nil
}

func validatePathConflictsAndPermissions(req *SaveConfigRequest) error {
	// Lookup table for Global Collision Detection
	// Key: Absolute Path, Value: Who owns it
	pathOwners := make(map[string]string)

	// Helper to register path and check collision
	registerPath := func(rawPath, owner string) error {
		if rawPath == "" {
			return nil
		}
		absPath, err := filepath.Abs(rawPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path '%s': %v", rawPath, err)
		}
		if existingOwner, found := pathOwners[absPath]; found {
			// Check for benign collision (Same Entity)
			isSystem := strings.HasPrefix(existingOwner, "System") && strings.HasPrefix(owner, "System")
			sameUserDB := false
			if strings.HasPrefix(existingOwner, "UserDB[") && strings.HasPrefix(owner, "UserDB[") {
				// Check if they share the same index e.g. "UserDB[0]"
				endIdx1 := strings.Index(existingOwner, "]")
				endIdx2 := strings.Index(owner, "]")
				if endIdx1 > 0 && endIdx2 > 0 && existingOwner[:endIdx1+1] == owner[:endIdx2+1] {
					sameUserDB = true
				}
			}

			// CRITICAL: Erasure Coding paths must ALWAYS be unique for isolation (Data vs Parity vs Other Drives).
			// We disable "benign overlap" if either path is involved in an Erasure Config.
			if strings.Contains(existingOwner, "Erasure Key") || strings.Contains(owner, "Erasure Key") {
				isSystem = false
				sameUserDB = false
			}

			if isSystem || sameUserDB {
				// Benign overlap within the same logical entity config
				return nil
			}

			// Collision detected
			return fmt.Errorf("path conflict: '%s' is used by '%s' and '%s'. All paths must be unique.", rawPath, existingOwner, owner)
		}
		pathOwners[absPath] = owner
		return nil
	}

	// 1. Register System DB Paths
	// System Registry Path (ignore if empty/advanced mode fallback)
	if req.RegistryPath != "" {
		if err := registerPath(req.RegistryPath, "System RegistryPath"); err != nil {
			return err
		}
	}
	// System Stores Folders
	for i, sf := range req.SystemOptions.StoresFolders {
		if err := registerPath(sf, fmt.Sprintf("System StoresFolder[%d]", i)); err != nil {
			return err
		}
	}
	// System EC Configs
	for key, conf := range req.SystemOptions.ErasureConfig {
		if len(conf.BaseFolderPathsAcrossDrives) != conf.DataShardsCount+conf.ParityShardsCount {
			return fmt.Errorf("Erasure Config (System Key %s): BasePaths count must match Data+Parity", key)
		}
		for i, bp := range conf.BaseFolderPathsAcrossDrives {
			if err := registerPath(bp, fmt.Sprintf("System Erasure Key[%s][%d]", key, i)); err != nil {
				return err
			}
		}
	}

	// 2. Register User DB Paths
	for dbIdx, db := range req.Databases {
		dbLabel := fmt.Sprintf("UserDB[%d] '%s'", dbIdx, db.Name)
		if err := registerPath(db.Path, dbLabel+" Path"); err != nil {
			return err
		}
		for i, sf := range db.DatabaseOptions.StoresFolders {
			if err := registerPath(sf, fmt.Sprintf("%s StoresFolder[%d]", dbLabel, i)); err != nil {
				return err
			}
		}
		for key, conf := range db.DatabaseOptions.ErasureConfig {
			if len(conf.BaseFolderPathsAcrossDrives) != conf.DataShardsCount+conf.ParityShardsCount {
				return fmt.Errorf("Erasure Config (%s Key %s): BasePaths count must match Data+Parity", dbLabel, key)
			}
			for i, bp := range conf.BaseFolderPathsAcrossDrives {
				if err := registerPath(bp, fmt.Sprintf("%s Erasure Key[%s][%d]", dbLabel, key, i)); err != nil {
					return err
				}
			}
		}
	}

	// 3. Validate existing DBs and permissions
	// Re-construct list of unique paths for final safety checks
	allPaths := make([]string, 0, len(pathOwners))
	for p := range pathOwners {
		allPaths = append(allPaths, p)
	}

	alreadyConfigured := collectAllConfiguredPaths(SystemDBName)
	log.Debug("TRACE: Starting Safety Validation (Wizard)")

	if err := validatePathSafety(allPaths, alreadyConfigured); err != nil {
		return fmt.Errorf("path safety checking failed: %v", err)
	}
	if err := validateWritePermissions(allPaths); err != nil {
		return fmt.Errorf("write permissions check failed: %v", err)
	}

	log.Debug("TRACE: Safety Validation Passed (Wizard)")
	return nil
}

// normalizeDefaultEcConfig ensures that if a "default" erasure config exists,
// it is mapped to the empty string key "" which SOP uses as the fallback.
func normalizeDefaultEcConfig(opts *sop.DatabaseOptions) {
	if opts.ErasureConfig == nil {
		return
	}
	// If "default" exists ...
	if def, hasDefault := opts.ErasureConfig["default"]; hasDefault {
		// ... and "" does NOT exist ...
		if _, hasEmpty := opts.ErasureConfig[""]; !hasEmpty {
			// Move it to ""
			opts.ErasureConfig[""] = def
			delete(opts.ErasureConfig, "default")
		}
	}
}

func setupSystemDB(ctx context.Context, req *SaveConfigRequest) (*DatabaseConfig, bool, error) {
	// Normalize System DB Paths (Simple vs Advanced)
	targetFolders := req.SystemOptions.StoresFolders
	// Fallback for Simple Mode
	if len(targetFolders) == 0 {
		if req.RegistryPath != "" {
			targetFolders = []string{req.RegistryPath}
			req.SystemOptions.StoresFolders = targetFolders // Update the options as well
		} else {
			return nil, false, nil // Nothing to setup
		}
	}
	// Ensure Request Object is consistent for later steps
	req.RegistryPath = targetFolders[0]

	sysOpts := req.SystemOptions
	// Ensure defaults if not present
	if sysOpts.RegistryHashModValue == 0 {
		sysOpts.RegistryHashModValue = fs.MinimumModValue
	}

	// Normalize EC Config (default -> "")
	normalizeDefaultEcConfig(&sysOpts)

	// Shared Brain Detection
	hasDBOptions, hasRegHashMod := database.IsDatabasePath(req.RegistryPath)
	if !hasDBOptions {
		subPath := filepath.Join(req.RegistryPath, "system_db")
		if exists, mod := database.IsDatabasePath(subPath); exists {
			req.RegistryPath = subPath
			hasDBOptions = true
			hasRegHashMod = mod
		}
	}

	shouldSetup := true
	if hasDBOptions || hasRegHashMod {
		shouldSetup = false
		log.Info(fmt.Sprintf("Existing system database detected at '%s'. Reusing...", req.RegistryPath))
		existingOpts, err := loadDatabaseOptionsForReuse(req.RegistryPath)
		if err != nil {
			return nil, false, fmt.Errorf("system database exists but failed to read options in '%s': %w", req.RegistryPath, err)
		}
		if req.UseSharedBrain {
			if err := validateSharedDatabaseReuse(existingOpts, sysOpts); err != nil {
				return nil, false, fmt.Errorf("shared brain compatibility check failed: %w", err)
			}
		}
		sysOpts = existingOpts
	} else if req.UseSharedBrain {
		return nil, false, fmt.Errorf("shared brain selected but files missing in '%s'", req.RegistryPath)
	}

	if shouldSetup {
		log.Debug(fmt.Sprintf("TRACE: Executing database.Setup for SystemDB at '%s'", req.RegistryPath))
		if _, err := database.Setup(ctx, sysOpts); err != nil {
			// Local cleanup if failed new setup
			if !req.UseSharedBrain {
				cleanupSystemDB(req)
			}
			return nil, false, fmt.Errorf("setup system registry: %v", err)
		}
	}

	// Auto-Create Scripts

	func() {
		fmt.Println("DB_SETUP: before BeginTransaction")
		trans, err := database.BeginTransaction(ctx, sysOpts, sop.ForWriting)
		if err != nil {
			log.Error(fmt.Sprintf("Scripts store tx error: %v", err))
			return
		}
		ms := model.New("scripts", trans)
		fmt.Println("DB_SETUP: before ms.List")
		ms.List(ctx, "") // trigger init
		fmt.Println("DB_SETUP: after ms.List")

		// Seed demo
		demoLoop := ai.Script{
			Description: "Demonstrates loops",
			Steps: []ai.ScriptStep{
				{Type: "set", Variable: "items", Value: "apple\nbanana\ncherry"},
				{Type: "loop", List: "items", Iterator: "fruit", Steps: []ai.ScriptStep{
					{Type: "say", Message: "Processing {{.fruit}}..."},
					{Type: "ask", Prompt: "What color is {{.fruit}}? (Answer in 1 word)"},
				}},
			},
		}
		ms.Save(ctx, "general", "demo_loop", demoLoop)
		trans.Commit(ctx)
	}()

	// Construct Config
	mode := "standalone"
	if sysOpts.Type == sop.Clustered {
		mode = "clustered"
	}
	sysDB := &DatabaseConfig{
		Name:     SystemDBName,
		Path:     req.RegistryPath,
		IsSystem: true,
		Mode:     mode,
	}
	var ecs []ErasureConfigEntry
	for key, val := range sysOpts.ErasureConfig {
		ecs = append(ecs, ErasureConfigEntry{
			Key:          key,
			DataChunks:   val.DataShardsCount,
			ParityChunks: val.ParityShardsCount,
			BasePaths:    val.BaseFolderPathsAcrossDrives,
		})
	}
	sysDB.ErasureConfigs = ecs
	if mode == "clustered" && sysOpts.RedisConfig != nil {
		sysDB.RedisURL = sysOpts.RedisConfig.Address
	}

	if err := ensureBootstrapStore(ctx, sysDB); err != nil {
		return nil, false, fmt.Errorf("bootstrap metadata store: %w", err)
	}

	return sysDB, shouldSetup, nil
}

func setupUserDBs(ctx context.Context, req *SaveConfigRequest) ([]DatabaseConfig, error) {
	var results []DatabaseConfig
	var createdPaths []string // Aggregates all paths created for User DBs (Main path, stores folders, EC paths)

	for i, udb := range req.Databases {
		if udb.Path == "" {
			continue
		}

		// Dedupe Folders
		storeFolders := []string{udb.Path}
		seen := map[string]struct{}{udb.Path: {}}
		for _, f := range udb.DatabaseOptions.StoresFolders {
			if _, exists := seen[f]; !exists {
				storeFolders = append(storeFolders, f)
				seen[f] = struct{}{}
			}
		}

		// Use the embedded options
		uOpts := udb.DatabaseOptions
		uOpts.StoresFolders = storeFolders // Ensure path is included
		if uOpts.RegistryHashModValue == 0 {
			uOpts.RegistryHashModValue = fs.MinimumModValue
		}

		// Normalize EC Config (default -> "")
		normalizeDefaultEcConfig(&uOpts)

		if udb.UseSharedDB {
			existingOpts, err := loadDatabaseOptionsForReuse(udb.Path)
			if err != nil {
				return nil, fmt.Errorf("shared user database '%s' selected but failed to read existing options in '%s': %w", udb.Name, udb.Path, err)
			}
			if err := validateSharedDatabaseReuse(existingOpts, uOpts); err != nil {
				return nil, fmt.Errorf("shared user database '%s' compatibility check failed: %w", udb.Name, err)
			}
		}

		shouldSetupUser := !udb.UseSharedDB
		if shouldSetupUser {
			if entries, err := os.ReadDir(uOpts.StoresFolders[0]); err == nil && len(entries) > 0 {
				log.Error(fmt.Sprintf("Failed to setup User DB [%d] '%s': destination path '%s' is not empty. Cannot create a fresh database here, as it may corrupt existing data.", i, udb.Name, uOpts.StoresFolders[0]))
				for _, cp := range createdPaths {
					os.RemoveAll(cp)
				}
				return nil, fmt.Errorf("user destination path '%s' is not empty. Cannot create a fresh database here as it may corrupt existing data", uOpts.StoresFolders[0])
			}
			if _, err := database.Setup(ctx, uOpts); err != nil {
				log.Error(fmt.Sprintf("Failed to setup User DB [%d] '%s': %v. Rolling back user DBs...", i, udb.Name, err))
				// Rollback all user DB paths created so far
				for _, cp := range createdPaths {
					os.RemoveAll(cp)
				}
				// Also rollback current DB attempt partials
				for _, f := range storeFolders {
					os.RemoveAll(f)
				}
				for _, ec := range uOpts.ErasureConfig {
					for _, bp := range ec.BaseFolderPathsAcrossDrives {
						os.RemoveAll(bp)
					}
				}
				return nil, fmt.Errorf("user db setup failed: %v", err)
			}

			// Track paths for potential rollback on next iteration failure
			createdPaths = append(createdPaths, storeFolders...)
			for _, ec := range uOpts.ErasureConfig {
				createdPaths = append(createdPaths, ec.BaseFolderPathsAcrossDrives...)
			}

			// Init/Demo
			if udb.PopulateDemo {
				var wg sync.WaitGroup
				var errDemo error

				wg.Add(1)
				go func() {
					defer wg.Done()
					if e := PopulateDemoData(ctx, uOpts); e != nil {
						errDemo = e
					}
				}()
				wg.Wait()

				if errDemo != nil {
					log.Error(fmt.Sprintf("Failed to populate demo data for User DB '%s': %v", udb.Name, errDemo))
				} else {
					log.Info(fmt.Sprintf("Demo data populated for User DB '%s'", udb.Name))
				}
			}

		}

		// Config Entry
		var ecs []ErasureConfigEntry
		for key, val := range uOpts.ErasureConfig {
			ecs = append(ecs, ErasureConfigEntry{
				Key:          key,
				DataChunks:   val.DataShardsCount,
				ParityChunks: val.ParityShardsCount,
				BasePaths:    val.BaseFolderPathsAcrossDrives,
			})
		}
		mode := "standalone"
		if uOpts.Type == sop.Clustered {
			mode = "clustered"
		}
		newDB := DatabaseConfig{
			Name: udb.Name, Path: udb.Path, StoresFolders: uOpts.StoresFolders, Mode: mode, ErasureConfigs: ecs,
		}
		if mode == "clustered" && uOpts.RedisConfig != nil {
			newDB.RedisURL = uOpts.RedisConfig.Address
		}
		results = append(results, newDB)
	}
	return results, nil
}

// cleanupSystemDB performs a forceful rollback of all System DB related folders.
func cleanupSystemDB(req *SaveConfigRequest) {
	log.Warn(fmt.Sprintf("Cleaning up System DB at '%s'", req.RegistryPath))
	// 1. Registry Path
	if req.RegistryPath != "" {
		os.RemoveAll(req.RegistryPath)
	}
	// 2. Stores Folders
	for _, sf := range req.SystemOptions.StoresFolders {
		os.RemoveAll(sf)
	}
	// 3. Erasure Paths
	for _, config := range req.SystemOptions.ErasureConfig {
		for _, bp := range config.BaseFolderPathsAcrossDrives {
			os.RemoveAll(bp)
		}
	}
}
