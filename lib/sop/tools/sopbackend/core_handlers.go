package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	log "log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/btree"
	"github.com/sharedcode/sop/common"
	"github.com/sharedcode/sop/database"
	"github.com/sharedcode/sop/encoding"
	"github.com/sharedcode/sop/fs"
	"github.com/sharedcode/sop/jsondb"
)

// ErasureConfigEntry defines a single EC zone configuration

// DatabaseConfig holds configuration for a single SOP database

// Config holds the server configuration

var provisioningCompleteCache atomic.Bool

func markProvisioningComplete() {
	provisioningCompleteCache.Store(true)
}

func resetProvisioningCompletionCache() {
	provisioningCompleteCache.Store(false)
}

// normalizeStoreName converts spaces to underscores and lowercases the name for backend storage
// Example: "My Store" -> "my_store"
func normalizeStoreName(name string) string {
	name = strings.TrimSpace(name)
	// Convert spaces to underscores
	name = strings.ReplaceAll(name, " ", "_")
	// Convert to lowercase for consistency
	name = strings.ToLower(name)
	return name
}

func candidateConfigPaths() []string {
	return CandidateConfigPaths(config.ConfigFile)
}

func FindExistingConfigFile() string {
	return findExistingConfigFile(config.ConfigFile)
}

func init() {
	sop.RegisterAssetRBAC(sop.AssetBlueprint{
		AssetType:   "space",
		Description: "Space represents a domain or workspace in the system",
		Endpoints:   []string{string(sop.EndpointSpacesList)},
		Actions:     []sop.Action{sop.ActionRead, sop.ActionWrite, sop.ActionDelete, sop.ActionAISelect},
		Evaluator: func(ctx context.Context, entCtx sop.EntitlementContext, action sop.Action, evalState *sop.RBACEvaluationState) bool {
			if action == sop.ActionAISelect {
				if isReadOnlyContext(entCtx.AssetID) {
					return false
				}

				// Vectorization is no longer a requirement - all spaces are accessible
				return true
			}

			if isReadOnlyContext(entCtx.AssetID) && entCtx.IsSystemDB && (action == sop.ActionWrite || action == sop.ActionDelete) {
				// Allow SystemAdmin to manage memory spaces via AI
				auth := sop.GetAuthFromContext(ctx)
				for _, role := range auth.Roles {
					if strings.EqualFold(role, sop.RoleSystemAdmin) {
						return true
					}
				}
				return false
			}
			return true
		},
	})

	sop.RegisterAssetRBAC(sop.AssetBlueprint{
		AssetType:   "store",
		Description: "Store represents a schema-backed data collection in the current database",
		Endpoints:   []string{string(sop.EndpointStoresList)},
		Actions:     []sop.Action{sop.ActionRead, sop.ActionWrite, sop.ActionDelete},
		Evaluator: func(ctx context.Context, entCtx sop.EntitlementContext, action sop.Action, evalState *sop.RBACEvaluationState) bool {
			// WorkQueue stores (wfq_*) are read-only for all users (including SystemAdmin)
			if strings.HasPrefix(entCtx.AssetID, "wfq_") {
				if action == sop.ActionWrite || action == sop.ActionDelete {
					return false
				}
				// Only allow read access
				if !shouldExposeStoreInRBAC(ctx, entCtx.AssetID) {
					return false
				}
				return action == sop.ActionRead
			}
			if entCtx.IsSystemDB && (action == sop.ActionWrite || action == sop.ActionDelete) {
				return false
			}
			if !shouldExposeStoreInRBAC(ctx, entCtx.AssetID) {
				return false
			}
			return sop.Authorize(ctx, sop.ResourceAccess{Visibility: sop.VisibilityPublic}, action)
		},
	})
}

func migrateLegacyRootPassword() {
	if strings.TrimSpace(config.RootPassword.Plaintext()) == "" {
		return
	}

	// Check if root user already exists in persistent store
	if _, ok, _ := config.findUserRecord(context.Background(), "root"); ok {
		config.RootPassword = ""
		return
	}

	if err := config.SetUser("root", config.RootPassword.Plaintext(), sop.RoleAdmin); err != nil {
		log.Warn("failed to migrate legacy root password into users", "error", err)
		return
	}
	config.RootPassword = ""
}

func LoadConfig(path string) error {
	raw, err := loadConfigFromDisk(path)
	if err != nil {
		return err
	}

	// Reset data pointers to prevent pollution from previous active configuration
	config.Databases = nil
	config.SystemDB = nil

	payload, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		return err
	}
	if config.ConfigFile == "" {
		config.ConfigFile = path
	}

	migrateLegacyRootPassword()
	if err := config.syncUsersFromStore(context.Background()); err != nil {
		log.Warn("failed to hydrate users from backing store", "error", err)
	}

	// Default System DB logic
	// We ONLY populate SystemDB if it is explicitly defined in the config.
	// If it is nil, we leave it as nil. This allows us to distinguish between
	// "Legacy/Default" (handled by getters) and "New Empty Environment" (handled by UI Wizard).

	if config.SystemDB != nil {
		configDir := filepath.Dir(path)
		if config.SystemDB.Path == "" {
			config.SystemDB.Path = configDir
		}
		if config.SystemDB.Name == "" {
			config.SystemDB.Name = SystemDBName
		}
		if config.SystemDB.Mode == "" {
			config.SystemDB.Mode = config.Mode
		}
		if config.SystemDB.RedisURL == "" {
			config.SystemDB.RedisURL = config.RedisURL
		}

		// Resolve paths config-relative
		config.SystemDB.Path = resolvePathRelativeToConfig(config.SystemDB.Path)
		for i, p := range config.SystemDB.StoresFolders {
			config.SystemDB.StoresFolders[i] = resolvePathRelativeToConfig(p)
		}
		for i, ec := range config.SystemDB.ErasureConfigs {
			for j, bp := range ec.BasePaths {
				config.SystemDB.ErasureConfigs[i].BasePaths[j] = resolvePathRelativeToConfig(bp)
			}
		}
	}

	for i := range config.Databases {
		config.Databases[i].Path = resolvePathRelativeToConfig(config.Databases[i].Path)
		for j, p := range config.Databases[i].StoresFolders {
			config.Databases[i].StoresFolders[j] = resolvePathRelativeToConfig(p)
		}
		for j, ec := range config.Databases[i].ErasureConfigs {
			for k, bp := range ec.BasePaths {
				config.Databases[i].ErasureConfigs[j].BasePaths[k] = resolvePathRelativeToConfig(bp)
			}
		}
	}

	return nil
}

func GetSystemDBOptions(ctx context.Context) (sop.DatabaseOptions, error) {
	// 1. If explicitly configured in config.json, use it.
	if config.SystemDB != nil {
		return GetDBOptionsFromConfig(ctx, config.SystemDB)
	}

	// 2. Fallback to default (cwd)
	cwd, _ := os.Getwd()
	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{cwd},
	}
	if config.Mode == "clustered" {
		opts.Type = sop.Clustered
		opts.CacheType = sop.Redis
		opts.RedisConfig = &sop.RedisCacheConfig{
			Address: config.RedisURL,
		}
	}
	return opts, nil
}

// IsSystemDB checks if the given database name corresponds to the System Database.
func IsSystemDB(name string) bool {
	// Check against configured SystemDB name if available
	if config.SystemDB != nil && config.SystemDB.Name == name {
		return true
	}
	// Check known system name.
	return name == SystemDBName
}

func GetDBOptionsFromConfig(ctx context.Context, db *DatabaseConfig) (sop.DatabaseOptions, error) {
	// Try to load from disk first
	if loadedOpts, err := database.GetOptions(ctx, db.Path); err == nil {
		// Override with runtime config if necessary (e.g. Redis address from flags)
		if db.Mode == "clustered" {
			loadedOpts.Type = sop.Clustered
			loadedOpts.CacheType = sop.Redis
			loadedOpts.RedisConfig = &sop.RedisCacheConfig{
				Address: db.RedisURL,
			}
		}
		// Ensure we use the loaded options' StoresFolders if available, as they might contain the full list of folders
		// including those for erasure coding, which are critical for finding the data.
		return loadedOpts, nil
	}

	opts := sop.DatabaseOptions{
		Type:          sop.Standalone,
		StoresFolders: []string{db.Path},
	}
	// We rely on SOP to handle path resolution.
	// If the store uses Erasure Coding, the config in StoreInfo should guide SOP to the correct files.
	// If we override StoresFolders here, we risk breaking Database discovery.

	if db.Mode == "clustered" {
		opts.Type = sop.Clustered
		opts.CacheType = sop.Redis
		opts.RedisConfig = &sop.RedisCacheConfig{
			Address: db.RedisURL,
		}
	}
	return opts, nil
}

func GetDBOptions(ctx context.Context, dbName string) (sop.DatabaseOptions, error) {
	if dbName == "" {
		if len(config.Databases) > 0 {
			// Return options for the first database
			return GetDBOptionsFromConfig(ctx, &config.Databases[0])
		}
		// If no databases, falls through to error or should we return SystemDB?
		// Stick to current behavior: likely error if no default found here.
	} else {
		// 1. Check if it explicitly matches the Configured System DB Name
		if config.SystemDB != nil && config.SystemDB.Name == dbName {
			return GetDBOptionsFromConfig(ctx, config.SystemDB)
		}

		// 2. Check for System Database
		// Delegated to getSystemDBOptions to handle default/env configuration
		if dbName == SystemDBName {
			return GetSystemDBOptions(ctx)
		}

		// 3. Check User Databases
		for i := range config.Databases {
			if config.Databases[i].Name == dbName {
				return GetDBOptionsFromConfig(ctx, &config.Databases[i])
			}
		}
	}

	return sop.DatabaseOptions{}, fmt.Errorf("database '%s' not found", dbName)
}

func HasConfigFileOnDisk() bool {
	if provisioningCompleteCache.Load() {
		return true
	}
	path := FindExistingConfigFile()
	if path == "" {
		resetProvisioningCompletionCache()
		return false
	}
	cfg, err := loadConfigFromDisk(path)
	if err != nil {
		resetProvisioningCompletionCache()
		return false
	}
	provisioned := len(cfg.Databases) > 0 || cfg.SystemDB != nil
	if provisioned {
		markProvisioningComplete()
	} else {
		resetProvisioningCompletionCache()
	}
	return provisioned
}

type RuntimeRefreshResponse struct {
	Runtime             string              `json:"runtime"`
	Databases           []DatabaseConfig    `json:"databases"`
	SelectedDatabase    string              `json:"selected_database,omitempty"`
	Stores              map[string][]string `json:"stores,omitempty"`
	Spaces              map[string][]string `json:"spaces,omitempty"`
	DisableSOPKBPreload bool                `json:"disable_sop_kb_preload,omitempty"`
	StoresEnabled       bool                `json:"stores_enabled"`
}

func handleRuntimeRefresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	json.NewEncoder(w).Encode(buildRuntimeRefreshResponse(r.Context(), r, "hq"))
}

func buildRuntimeRefreshResponse(ctx context.Context, req *http.Request, runtime string) RuntimeRefreshResponse {
	response := RuntimeRefreshResponse{
		Runtime:             runtime,
		Databases:           []DatabaseConfig{},
		Stores:              map[string][]string{},
		Spaces:              map[string][]string{},
		DisableSOPKBPreload: config.DisableSOPKBPreload,
		StoresEnabled:       sharedagent.StoresFeatureEnabled(),
	}

	dbs := make([]DatabaseConfig, len(config.Databases))
	copy(dbs, config.Databases)

	for i := range dbs {
		if _, err := os.Stat(dbs[i].Path); os.IsNotExist(err) {
			msg := fmt.Sprintf("Database '%s' does not exist at path '%s'.", dbs[i].Name, dbs[i].Path)
			if !strings.Contains(dbs[i].Warning, msg) {
				if dbs[i].Warning == "" {
					dbs[i].Warning = msg
				} else {
					dbs[i].Warning += " " + msg
				}
			}
		}
	}

	if config.SystemDB != nil {
		sysDB := *config.SystemDB
		if sysDB.Name == "" {
			sysDB.Name = SystemDBName
		}
		sysDB.IsSystem = true
		if _, err := os.Stat(sysDB.Path); os.IsNotExist(err) {
			msg := fmt.Sprintf("System Database '%s' does not exist at path '%s'.", sysDB.Name, sysDB.Path)
			if !strings.Contains(sysDB.Warning, msg) {
				if sysDB.Warning == "" {
					sysDB.Warning = msg
				} else {
					sysDB.Warning += " " + msg
				}
			}
		}
		dbs = append(dbs, sysDB)
	} else if len(config.Databases) > 0 {
		implicitPath := "."
		if config.ConfigFile != "" {
			implicitPath = filepath.Dir(config.ConfigFile)
		}
		implDB := DatabaseConfig{Name: SystemDBName, Path: filepath.Join(implicitPath, SystemDBName), Mode: config.Mode, IsSystem: true}
		if _, err := os.Stat(implDB.Path); os.IsNotExist(err) {
			msg := fmt.Sprintf("System Database '%s' does not exist at path '%s'.", implDB.Name, implDB.Path)
			if !strings.Contains(implDB.Warning, msg) {
				if implDB.Warning == "" {
					implDB.Warning = msg
				} else {
					implDB.Warning += " " + msg
				}
			}
		}
		dbs = append(dbs, implDB)
	}

	response.Databases = dbs
	if len(dbs) > 0 {
		response.SelectedDatabase = dbs[0].Name
	}

	for _, db := range dbs {
		// Only populate Stores if feature is enabled
		if sharedagent.StoresFeatureEnabled() {
			if stores, err := runtimeStoresForDatabase(ctx, req, db.Name); err == nil {
				response.Stores[db.Name] = stores
			}
		}
		if spaces, err := runtimeSpacesForDatabase(ctx, req, db.Name); err == nil {
			response.Spaces[db.Name] = spaces
		}
	}

	return response
}

func runtimeStoresForDatabase(ctx context.Context, req *http.Request, dbName string) ([]string, error) {
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		return nil, err
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		return nil, err
	}
	defer trans.Rollback(ctx)

	stores, err := trans.GetStores(ctx)
	if err != nil {
		return nil, err
	}

	currentUserID := strings.TrimSpace(sop.GetAuthFromContext(ctx).UserID)
	if currentUserID == "" && req != nil {
		currentUserID = strings.TrimSpace(req.URL.Query().Get("user_id"))
	}

	var visibleStores []string
	for _, s := range stores {
		rbac := sop.ResolveRBACMap(ctx, "store", sop.EntitlementContext{AssetID: s, Database: dbName, IsSystemDB: IsSystemDB(dbName), UserID: currentUserID}, nil)
		if rbac[sop.UICapabilityRead] {
			visibleStores = append(visibleStores, s)
		}
	}
	if visibleStores == nil {
		visibleStores = make([]string, 0)
	}
	return visibleStores, nil
}

func runtimeSpacesForDatabase(ctx context.Context, req *http.Request, dbName string) ([]string, error) {
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		return nil, err
	}

	db := aidb.NewDatabase(dbOpts)
	result, err := db.GetDomains(ctx)
	if err != nil {
		return nil, err
	}

	userID := strings.TrimSpace(sop.GetAuthFromContext(ctx).UserID)
	if userID == "" && req != nil {
		userID = strings.TrimSpace(req.URL.Query().Get("user_id"))
	}
	var filtered []string
	expectedMemoryPrefix := "memory_"
	expectedMemoryFull := expectedMemoryPrefix + userID
	if result != nil {
		for _, name := range result {
			if strings.HasPrefix(name, expectedMemoryPrefix) {
				if name == expectedMemoryFull {
					filtered = append(filtered, name)
				}
			} else {
				filtered = append(filtered, name)
			}
		}
	}
	if filtered == nil {
		filtered = make([]string, 0)
	}
	return filtered, nil
}

func handleDatabases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Content-Type", "application/json")

	// GET: List Databases
	if r.Method == http.MethodGet {
		dbs := make([]DatabaseConfig, len(config.Databases))
		copy(dbs, config.Databases)

		for i := range dbs {
			if _, err := os.Stat(dbs[i].Path); os.IsNotExist(err) {
				msg := fmt.Sprintf("Database '%s' does not exist at path '%s'.", dbs[i].Name, dbs[i].Path)
				if !strings.Contains(dbs[i].Warning, msg) {
					if dbs[i].Warning == "" {
						dbs[i].Warning = msg
					} else {
						dbs[i].Warning += " " + msg
					}
				}
			}
		}

		if config.SystemDB != nil {
			sysDB := *config.SystemDB
			if sysDB.Name == "" {
				sysDB.Name = SystemDBName
			}
			sysDB.IsSystem = true
			if _, err := os.Stat(sysDB.Path); os.IsNotExist(err) {
				msg := fmt.Sprintf("System Database '%s' does not exist at path '%s'.", sysDB.Name, sysDB.Path)
				if !strings.Contains(sysDB.Warning, msg) {
					if sysDB.Warning == "" {
						sysDB.Warning = msg
					} else {
						sysDB.Warning += " " + msg
					}
				}
			}
			dbs = append(dbs, sysDB)
		} else if len(config.Databases) > 0 {
			// Backward Compatibility:
			// If we have user databases but no explicit SystemDB, we include the implicit one.
			// This prevents existing setups from "losing" their System DB in the UI.
			// But if Databases is EMPTY, we assume it's a fresh env and return nothing, triggering the Wizard.

			// We can reconstruct the implicit path logic or just ignore it for the list?
			// Generally, if it's implicit, it's in the CWD (or config dir).
			// Let's add it to key users happy.

			// Determine implicit path
			implicitPath := "."
			if config.ConfigFile != "" {
				implicitPath = filepath.Dir(config.ConfigFile)
			}

			implDB := DatabaseConfig{
				Name:     SystemDBName,
				Path:     filepath.Join(implicitPath, SystemDBName),
				Mode:     config.Mode,
				IsSystem: true,
			}
			if _, err := os.Stat(implDB.Path); os.IsNotExist(err) {
				msg := fmt.Sprintf("System Database '%s' does not exist at path '%s'.", implDB.Name, implDB.Path)
				if !strings.Contains(implDB.Warning, msg) {
					if implDB.Warning == "" {
						implDB.Warning = msg
					} else {
						implDB.Warning += " " + msg
					}
				}
			}

			dbs = append(dbs, implDB)
		}

		json.NewEncoder(w).Encode(dbs)
		return
	}

	// DELETE: Remove Database
	if r.Method == http.MethodDelete {
		name := r.URL.Query().Get("name")
		deleteDataStr := r.URL.Query().Get("delete_data")
		deleteData := deleteDataStr != "false" // Default to true if not specified or "true"

		if name == "" {
			http.Error(w, "Database name is required", http.StatusBadRequest)
			return
		}

		found := -1
		var dbPath string
		isSystem := false

		for i, db := range config.Databases {
			if db.Name == name {
				found = i
				dbPath = db.Path
				break
			}
		}

		if found == -1 && config.SystemDB != nil {
			sysName := config.SystemDB.Name
			if sysName == "" {
				sysName = SystemDBName
			}
			if sysName == name {
				isSystem = true
				dbPath = config.SystemDB.Path
			}
		}

		if found == -1 && !isSystem {
			http.Error(w, "Database not found", http.StatusNotFound)
			return
		}

		// Delete the database.
		if deleteData {
			if err := database.Remove(r.Context(), dbPath); err != nil {
				log.Warn(fmt.Sprintf("Cleanup: Failed to remove database (some metadata may persist): %v", err))
			}
		} else {
			log.Info(fmt.Sprintf("Database '%s' removed from config only. Data at '%s' preserved.", name, dbPath))
		}

		// Remove from config
		if found != -1 {
			config.Databases = append(config.Databases[:found], config.Databases[found+1:]...)
		} else if isSystem {
			// Clearing SystemDB means setting it to nil
			config.SystemDB = nil
		}

		// Save Config
		if config.ConfigFile != "" {
			saveConfigFile()
			// Re-initialize agents to drop the deleted database
			if err := initAgents(context.Background()); err != nil {
				http.Error(w, fmt.Sprintf("Failed to initialize agents: %v", err), http.StatusInternalServerError)
				return
			}
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// POST: Create Database
	if r.Method == http.MethodPost {
		var req struct {
			Name            string   `json:"name"`
			Path            string   `json:"path"`
			Type            string   `json:"type"`
			Connection      string   `json:"conn_url"`
			StoresFolders   []string `json:"stores_folders"`
			RegistryHashMod int      `json:"registry_hash_mod"`
			PopulateDemo    bool     `json:"populate_demo"`
			UseSharedDB     bool     `json:"use_shared_db"`
			ErasureConfigs  []struct {
				Key          string   `json:"key"`
				DataChunks   int      `json:"data_chunks"`
				ParityChunks int      `json:"parity_chunks"`
				BasePaths    []string `json:"base_paths"`
			} `json:"erasure_configs"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Name == "" || req.Path == "" {
			http.Error(w, "Name and Path are required", http.StatusBadRequest)
			return
		}

		if req.RegistryHashMod == 0 {
			req.RegistryHashMod = fs.MinimumModValue
		}

		// Sanitize paths
		req.Path = sanitizePath(req.Path)
		for i, sf := range req.StoresFolders {
			req.StoresFolders[i] = sanitizePath(sf)
		}

		// Check for existing DB files to determine state (Shared vs New)
		dbOptionsPath := filepath.Join(req.Path, "dboptions.json")
		_, errOpts := os.Stat(dbOptionsPath)
		hasDBOptions := !os.IsNotExist(errOpts)

		shouldSetup := true

		if req.UseSharedDB {
			// User wants to reuse existing DB
			if hasDBOptions {
				shouldSetup = false
				log.Info(fmt.Sprintf("Shared User DB detected at '%s'. Reusing...", req.Path))

				// Check Write Permissions
				testFile := filepath.Join(req.Path, ".sop_write_test")
				if f, err := os.Create(testFile); err != nil {
					http.Error(w, fmt.Sprintf("User DB path exists but is not writable: %v", err), http.StatusBadRequest)
					return
				} else {
					f.Close()
					os.Remove(testFile)
				}
			} else {
				http.Error(w, fmt.Sprintf("Shared User DB selected but 'dboptions.json' is missing in '%s'.", req.Path), http.StatusBadRequest)
				return
			}
		} else {
			// New DB requested
			if hasDBOptions {
				http.Error(w, fmt.Sprintf("Destination path '%s' already contains a 'dboptions.json'. Enable 'Shared Mode' to reuse it or choose a clean path.", req.Path), http.StatusBadRequest)
				return
			}
		}

		// Prepare Options
		// Prepare Options
		options := sop.DatabaseOptions{
			RegistryHashModValue: req.RegistryHashMod,
		}
		// If explicit Stores Folders provided (Advanced Mode), use them.
		// Otherwise use the Data Path (Simple Mode).
		if len(req.StoresFolders) > 0 {
			options.StoresFolders = req.StoresFolders
		} else {
			options.StoresFolders = []string{req.Path}
		}

		// Set Database Type and Cache Config
		if req.Type == "clustered" {
			options.Type = sop.Clustered
			options.CacheType = sop.Redis
			if req.Connection != "" {
				options.RedisConfig = &sop.RedisCacheConfig{
					URL: req.Connection,
				}
			} else {
				http.Error(w, "Redis Connection URL is required for Clustered mode", http.StatusBadRequest)
				return
			}
		} else {
			options.Type = sop.Standalone
			options.CacheType = sop.InMemory
		}

		// Set Erasure Config if provided
		if len(req.ErasureConfigs) > 0 {
			options.ErasureConfig = make(map[string]sop.ErasureCodingConfig)
			for _, ec := range req.ErasureConfigs {
				options.ErasureConfig[ec.Key] = sop.ErasureCodingConfig{
					DataShardsCount:             ec.DataChunks,
					ParityShardsCount:           ec.ParityChunks,
					BaseFolderPathsAcrossDrives: ec.BasePaths,
				}
			}
		}

		ctx := r.Context()

		if shouldSetup {
			// 1. Setup Database
			if _, err := database.Setup(ctx, options); err != nil {
				http.Error(w, "Failed to setup database: "+err.Error(), http.StatusInternalServerError)
				return
			}

			// 2. Initialize Registry (Create dummy store)
			tx, err := database.BeginTransaction(ctx, options, sop.ForWriting)
			if err != nil {
				http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
				return
			}

			if err := ensureBootstrapStore(ctx, config.SystemDB); err != nil {
				tx.Rollback(ctx)
				http.Error(w, "Failed to initialize registry: "+err.Error(), http.StatusInternalServerError)
				return
			}

			tx.Commit(ctx)
		}

		if req.PopulateDemo && shouldSetup {
			if err := PopulateDemoData(ctx, options); err != nil {
				log.Error("Failed to populate demo data: " + err.Error())
			}
		}

		// 3. Update Config
		newDB := DatabaseConfig{
			Name:     req.Name,
			Path:     req.Path,
			Mode:     "standalone",
			RedisURL: req.Connection,
		}
		if req.Type == "clustered" {
			newDB.Mode = "clustered"
		}

		// Copy Erasure Configs
		for _, ec := range req.ErasureConfigs {
			newDB.ErasureConfigs = append(newDB.ErasureConfigs, struct {
				Key          string   `json:"key"`
				DataChunks   int      `json:"data_chunks"`
				ParityChunks int      `json:"parity_chunks"`
				BasePaths    []string `json:"base_paths"`
			}{
				Key:          ec.Key,
				DataChunks:   ec.DataChunks,
				ParityChunks: ec.ParityChunks,
				BasePaths:    ec.BasePaths,
			})
		}

		// Check duplication
		for _, db := range config.Databases {
			if db.Name == newDB.Name {
				http.Error(w, "Database with this name already exists", http.StatusConflict)
				return
			}
		}

		config.Databases = append(config.Databases, newDB)

		if config.ConfigFile != "" {
			saveConfigFile()
			// Re-initialize agents to pick up the new database
			if err := initAgents(context.Background()); err != nil {
				http.Error(w, fmt.Sprintf("Failed to initialize agents: %v", err), http.StatusInternalServerError)
				return
			}
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}
}

func saveConfigFile() {
	if config.ConfigFile == "" {
		return
	}
	f, err := os.Create(config.ConfigFile + ".tmp")
	if err != nil {
		log.Error(fmt.Sprintf("Failed to save config: %v", err))
		return
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "    ")
	encoder.Encode(config)
	os.Rename(config.ConfigFile+".tmp", config.ConfigFile)
}

func handleUpdateDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name           string `json:"name"`
		ErasureConfigs []struct {
			Key          string   `json:"key"`
			DataChunks   int      `json:"data_chunks"`
			ParityChunks int      `json:"parity_chunks"`
			BasePaths    []string `json:"base_paths"`
		} `json:"erasure_configs"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dbIdx := -1
	for i, db := range config.Databases {
		if db.Name == req.Name {
			dbIdx = i
			break
		}
	}

	if dbIdx == -1 {
		http.Error(w, "Database not found", http.StatusNotFound)
		return
	}

	// Validate (No Deletions allowed for now)
	currentKeys := make(map[string]bool)
	for _, ec := range config.Databases[dbIdx].ErasureConfigs {
		currentKeys[ec.Key] = true
	}

	newKeys := make(map[string]bool)
	for _, ec := range req.ErasureConfigs {
		newKeys[ec.Key] = true
	}

	for k := range currentKeys {
		if !newKeys[k] {
			http.Error(w, fmt.Sprintf("Deletion of Data File entry '%s' is not allowed.", k), http.StatusForbidden)
			return
		}
	}

	// Apply Update
	var newConfigs []ErasureConfigEntry

	for _, ec := range req.ErasureConfigs {
		newConfigs = append(newConfigs, ErasureConfigEntry{
			Key:          ec.Key,
			DataChunks:   ec.DataChunks,
			ParityChunks: ec.ParityChunks,
			BasePaths:    ec.BasePaths,
		})
	}

	// VALIDATION START
	{
		targetDB := config.Databases[dbIdx]
		proposedPaths := []string{}
		if targetDB.Path != "" {
			proposedPaths = append(proposedPaths, targetDB.Path)
		}
		proposedPaths = append(proposedPaths, targetDB.StoresFolders...)
		for _, ec := range newConfigs {
			proposedPaths = append(proposedPaths, ec.BasePaths...)
		}

		if err := validatePathSafety(proposedPaths, collectAllConfiguredPaths(req.Name)); err != nil {
			http.Error(w, fmt.Sprintf("Path Safety Error: %v", err), http.StatusBadRequest)
			return
		}
	}
	// VALIDATION END

	config.Databases[dbIdx].ErasureConfigs = newConfigs

	if config.ConfigFile != "" {
		saveConfigFile()
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func shouldExposeStoreInRBAC(ctx context.Context, storeName string) bool {
	storeName = strings.TrimSpace(storeName)
	if storeName == "" {
		return false
	}
	if strings.Contains(storeName, "/") {
		return false
	}
	if strings.HasSuffix(storeName, "_vecs") || strings.HasSuffix(storeName, "_by_time") {
		return false
	}
	if strings.EqualFold(storeName, bootstrapStoreName) {
		return false
	}
	if strings.EqualFold(storeName, sessionStoreName) {
		return isSystemAdminRoleInContext(ctx)
	}
	if strings.EqualFold(storeName, "site_users") {
		return isSystemAdminRoleInContext(ctx)
	}

	if strings.HasPrefix(storeName, "stm_") {
		if isRegularAdminRoleInContext(ctx) {
			return false
		}
		currentUserID := strings.TrimSpace(sop.GetAuthFromContext(ctx).UserID)
		if currentUserID == "" {
			return false
		}
		ownerSTM := memory.BuildSTMStoreName(ai.AgentIDOmni, currentUserID)
		if storeName != ownerSTM {
			return false
		}
	}

	if strings.HasPrefix(storeName, "ltm_") {
		currentUserID := strings.TrimSpace(sop.GetAuthFromContext(ctx).UserID)
		if currentUserID == "" {
			return false
		}
		ownerLTM := memory.BuildLTMStoreName(ai.AgentIDOmni, currentUserID)
		if storeName != ownerLTM {
			return false
		}
	}

	// WorkQueue stores: wfq_checkpoints_{userID}, wfq_metrics_{userID}, wfq_bytimestamp_{userID}
	if strings.HasPrefix(storeName, "wfq_") {
		// SystemAdmin and Admin can see all users' WorkQueue stores
		if isSystemAdminRoleInContext(ctx) || isRegularAdminRoleInContext(ctx) {
			return true
		}
		currentUserID := strings.TrimSpace(sop.GetAuthFromContext(ctx).UserID)
		if currentUserID == "" {
			return false
		}
		// Extract userID from store name: wfq_checkpoints_{userID}
		parts := strings.SplitN(storeName, "_", 3)
		if len(parts) < 3 {
			return false
		}
		storeOwnerID := parts[2]
		// Users can only see their own WorkQueue stores
		if storeOwnerID != currentUserID {
			return false
		}
	}

	return true
}

func handleListStores(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	dbName := r.URL.Query().Get("database")
	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	aiDB := aidb.NewDatabase(dbOpts)
	stores, err := aiDB.GetStores(ctx)
	if err != nil {
		http.Error(w, "Failed to list stores: "+err.Error(), http.StatusInternalServerError)
		return
	}
	folderStores, err := aiDB.GetFolderStores(ctx)
	if err != nil {
		http.Error(w, "Failed to list folder stores: "+err.Error(), http.StatusInternalServerError)
		return
	}
	repoStores, err := aiDB.GetRepoStores(ctx)
	if err != nil {
		http.Error(w, "Failed to list repo stores: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Merge the primary transactional stores with the lightweight side-car source stores.
	// Folder and repo stores are manifest-backed placeholders for later digest-to-Space
	// curation, so they are surfaced in the Stores view without affecting the core ACID store model.
	combinedStores := make([]string, 0, len(stores)+len(folderStores)+len(repoStores))
	seen := make(map[string]struct{})
	appendUnique := func(names []string) {
		for _, name := range names {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			combinedStores = append(combinedStores, name)
		}
	}
	appendUnique(stores)
	appendUnique(folderStores)
	appendUnique(repoStores)

	currentUserID := strings.TrimSpace(sop.GetAuthFromContext(ctx).UserID)
	if currentUserID == "" {
		currentUserID = strings.TrimSpace(r.URL.Query().Get("user_id"))
	}

	var visibleStores []string
	for _, s := range combinedStores {
		rbac := sop.ResolveRBACMap(ctx, "store", sop.EntitlementContext{AssetID: s, Database: dbName, IsSystemDB: IsSystemDB(dbName), UserID: currentUserID}, nil)
		if rbac[sop.UICapabilityRead] {
			visibleStores = append(visibleStores, s)
		}
	}

	if visibleStores == nil {
		visibleStores = make([]string, 0)
	}

	// Generate user-friendly display names for stores (WorkQueue, memory, etc.)
	storeDisplayNames := make(map[string]string)
	for _, storeName := range visibleStores {
		storeDisplayNames[storeName] = resolveStoreDisplayName(storeName)
	}

	bundled := sop.BundledResponse{
		Data:              visibleStores,
		RBAC:              sop.ResolveRBACMap(ctx, "store", sop.EntitlementContext{AssetID: dbName, Database: dbName, IsSystemDB: IsSystemDB(dbName)}, nil),
		StoreDisplayNames: storeDisplayNames,
	}

	json.NewEncoder(w).Encode(bundled)
}

func resolveSpaceDisplayName(name string, cfg *memory.KnowledgeBaseConfig) string {
	_ = cfg

	lowerName := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(lowerName, "ltm_") || strings.HasPrefix(lowerName, "memory_") {
		return "memory"
	}

	// Convert underscore-separated names to title case with spaces
	// e.g., "shared_memory" → "Shared Memory", "my_custom_space" → "My Custom Space"
	if strings.Contains(name, "_") {
		parts := strings.Split(name, "_")
		for i, part := range parts {
			if len(part) > 0 {
				parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
			}
		}
		return strings.Join(parts, " ")
	}

	return name
}

func resolveStoreDisplayName(name string) string {
	// User-friendly names for WorkQueue stores in AI Studio
	if strings.HasPrefix(name, "wfq_checkpoints_") {
		return "AI Studio - Workflow Checkpoints"
	}
	if strings.HasPrefix(name, "wfq_metrics_") {
		return "AI Studio - Workflow Metrics"
	}
	if strings.HasPrefix(name, "wfq_bytimestamp_") {
		return "AI Studio - Workflow Timeline"
	}

	// User-friendly names for memory stores
	if strings.HasPrefix(name, "stm_") {
		return "Short-term Memory"
	}
	if strings.HasPrefix(name, "ltm_") {
		return "Long-term Memory"
	}

	return name
}

func handleListSpaces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	dbName := r.URL.Query().Get("database")
	userID := r.URL.Query().Get("user_id")
	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db := aidb.NewDatabase(dbOpts)

	result, err := db.GetDomains(ctx)
	if err != nil {
		http.Error(w, "Failed to list playbooks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter out other users' LTMs, STMs, and memory spaces (unless SystemAdmin)
	var filtered []string
	auth := sop.GetAuthFromContext(ctx)
	isSystemAdmin := false
	for _, role := range auth.Roles {
		if strings.EqualFold(role, sop.RoleSystemAdmin) {
			isSystemAdmin = true
			break
		}
	}

	expectedMemoryPrefix := "memory_"
	expectedMemoryFull := expectedMemoryPrefix + userID
	expectedLTMSuffix := userID + "_ltm"
	expectedSTMSuffix := userID + "_stm"

	if result != nil {
		for _, name := range result {
			trimmedName := strings.TrimSpace(name)
			if trimmedName == "" {
				continue
			}
			lowerName := strings.ToLower(trimmedName)

			// SystemAdmin can see everything
			if isSystemAdmin {
				filtered = append(filtered, trimmedName)
				continue
			}

			// Filter memory_ prefixed spaces - only show user's own
			if strings.HasPrefix(trimmedName, expectedMemoryPrefix) {
				if trimmedName == expectedMemoryFull {
					filtered = append(filtered, trimmedName)
				}
				continue
			}

			// Filter _ltm suffixed spaces - only show user's own
			if strings.HasSuffix(lowerName, "_ltm") {
				if strings.EqualFold(trimmedName, expectedLTMSuffix) {
					filtered = append(filtered, trimmedName)
				}
				continue
			}

			// Filter _stm suffixed spaces - only show user's own
			if strings.HasSuffix(lowerName, "_stm") {
				if strings.EqualFold(trimmedName, expectedSTMSuffix) {
					filtered = append(filtered, trimmedName)
				}
				continue
			}

			// All other spaces are visible
			filtered = append(filtered, trimmedName)
		}
	}

	if filtered == nil {
		// Ensure we output [] instead of null in JSON if no playbooks exist
		filtered = make([]string, 0)
	}

	// PRE-FETCH vectorization status and display labels in one transaction for performance.
	// Reuse the same transaction for the later RBAC evaluation so each space can consult the same open KB handles.
	// Keep batch-scoped state explicit here (transaction + config cache) and pass it into the RBAC evaluator
	// instead of relying on context values for hot-path state propagation.
	vectorizedMap := make(map[string]bool)
	displayNames := make(map[string]string, len(filtered))
	configCache := make(map[string]bool)
	tx, _ := db.BeginTransaction(ctx, sop.ForReading)
	if tx != nil {
		for _, name := range filtered {
			kb, errKB := db.OpenKnowledgeBase(ctx, name, tx, nil, nil, false)
			if errKB == nil && kb != nil {
				cfg, cfgErr := kb.GetConfig(ctx)
				if cfgErr == nil && cfg != nil {
					if cfg.LastVectorized > 0 {
						vectorizedMap[name] = true
					}
					displayNames[name] = resolveSpaceDisplayName(name, cfg)
				} else {
					displayNames[name] = resolveSpaceDisplayName(name, nil)
				}
			} else {
				displayNames[name] = resolveSpaceDisplayName(name, nil)
			}
		}
	}
	eligibleSpaces := make([]string, 0, len(filtered))
	for _, name := range filtered {
		// Include all spaces, not just vectorized ones
		// The vectorizedMap will indicate which are vectorized for UI purposes
		eligibleSpaces = append(eligibleSpaces, name)
	}

	evalState := &sop.RBACEvaluationState{
		Transaction:      tx,
		ConfigCache:      configCache,
		VectorizedSpaces: vectorizedMap,
	}

	itemRBAC := make(map[string]sop.ContextRBACMap)
	for _, name := range eligibleSpaces {
		itemMap := sop.ResolveRBACMapWithState(ctx, "space", sop.EntitlementContext{AssetID: name, Database: dbName, IsSystemDB: IsSystemDB(dbName)}, nil, evalState)
		// Default to allowed, only ship the exclusion rules over the wire
		exclusionsOnly := make(sop.ContextRBACMap)
		for k, v := range itemMap {
			if !v {
				exclusionsOnly[k] = false
			}
		}
		if len(exclusionsOnly) > 0 {
			itemRBAC[name] = exclusionsOnly
		}
	}

	if tx != nil {
		tx.Rollback(ctx)
	}

	bundled := sop.BundledResponse{
		Data:             eligibleSpaces,
		RBAC:             sop.ResolveRBACMapWithState(ctx, "space", sop.EntitlementContext{AssetID: dbName, Database: dbName, IsSystemDB: IsSystemDB(dbName)}, nil, evalState),
		ItemRBAC:         itemRBAC,
		DisplayNames:     displayNames,
		VectorizedSpaces: vectorizedMap,
	}

	json.NewEncoder(w).Encode(bundled)
}

func handleGetDBOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	dbName := r.URL.Query().Get("database")
	dbOpts, err := GetDBOptions(r.Context(), dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(dbOpts); err != nil {
		log.Error("db-options encode failed", "database", dbName, "error", err)
		return
	}
	log.Debug("db-options served", "database", dbName, "payload", dbOpts)
}

// handleGetStoreInfo handles retrieval of store metadata.
func handleGetStoreInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	if storeName == "" {
		http.Error(w, "Store name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	// We need to open the store to get its info.
	// We can use a dummy comparer since we are not doing any operations.
	comparer := func(a, b any) int { return 0 }
	store, err := database.OpenBtree[any, any](ctx, dbOpts, storeName, trans, comparer)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open store '%s': %v", storeName, err), http.StatusInternalServerError)
		return
	}

	si := store.GetStoreInfo()

	// Prepare response
	dataSize := si.ValueDataSize()

	response := map[string]any{
		"name":                     si.Name,
		"description":              si.Description,
		"count":                    si.Count,
		"isPrimitiveKey":           si.IsPrimitiveKey,
		"celExpression":            si.CELexpression,
		"slotLength":               si.SlotLength,
		"isUnique":                 si.IsUnique,
		"dataSize":                 dataSize,
		"isValueInNode":            si.IsValueDataInNodeSegment,
		"isValueActivelyPersisted": si.IsValueDataActivelyPersisted,
		"isValueGloballyCached":    si.IsValueDataGloballyCached,
		"cacheDuration":            int(si.CacheConfig.ValueDataCacheDuration.Minutes()),
		"relations":                append([]sop.Relation(nil), si.Relations...),
		"schema":                   cloneStringMap(si.Schema),
		"keyFields":                append([]string(nil), si.KeyFields...),
		"valueFields":              append([]string(nil), si.ValueFields...),
	}

	hasIndexSpec := si.MapKeyIndexSpecification != ""
	if hasIndexSpec {
		var is jsondb.IndexSpecification
		if err := encoding.DefaultMarshaler.Unmarshal([]byte(si.MapKeyIndexSpecification), &is); err == nil {
			response["indexSpec"] = is
		}
	}

	// Fetch a sample key to infer structure/type for UI if store is not empty
	keyType := "string"
	valueType := "string"
	keyIsArray := false
	valueIsArray := false

	hasSchemaMetadata := len(si.Schema) > 0
	declaresStructKey := hasIndexSpec || len(si.KeyFields) > 0 || hasSchemaMetadata
	declaresStructValue := len(si.ValueFields) > 0 || hasSchemaMetadata

	if declaresStructKey || !si.IsPrimitiveKey {
		keyType = "map"
	}
	if declaresStructValue {
		valueType = "map"
		valueIsArray = false
	}

	if si.Count > 0 && !hasSchemaMetadata {
		if ok, _ := store.First(ctx); ok {
			k := store.GetCurrentKey().Key
			response["sampleKey"] = k
			keyType, keyIsArray = sop.InferType(k)
			if declaresStructKey || !si.IsPrimitiveKey {
				keyType = "map"
				keyIsArray = false
			}

			if v, err := store.GetCurrentValue(ctx); err == nil {
				response["sampleValue"] = v
				if !declaresStructValue {
					valueType, valueIsArray = sop.InferType(v)
				}
			}
		}
	} else {
		// If primitive and empty, we default to "string" in UI,
		// but we don't really know if it was intended to be int.
		// StoreInfo doesn't persist "int" vs "string", only "IsPrimitiveKey".
		// So "string" default is acceptable.
	}

	response["keyType"] = keyType
	response["valueType"] = valueType
	response["keyIsArray"] = keyIsArray
	response["valueIsArray"] = valueIsArray

	// Add display name for the store (uses resolveStoreDisplayName to handle special cases)
	response["display_name"] = resolveStoreDisplayName(si.Name)

	json.NewEncoder(w).Encode(response)
}

func normalizeJSON(s string) string {
	var j interface{}
	if err := json.Unmarshal([]byte(s), &j); err != nil {
		return s
	}
	b, _ := json.Marshal(j)
	return string(b)
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

// handleUpdateStoreInfo handles updates to store metadata and structure.
//
// EDITING RULES:
// 1. Empty Store (Count == 0):
//   - Full structural rebuild is allowed.
//   - Can change Key Type, Value Type, Slot Length, IsUnique, etc.
//   - Can add Seed Data (which effectively sets the type for inference).
//   - No Admin Token required.
//
// 2. Non-Empty Store (Count > 0):
//   - Structural fields are LOCKED (Key/Value Type, Slot Length, IsUnique).
//   - EXCEPTION: Index Specification and CEL Expression can be updated IF AND ONLY IF they are currently empty (missing).
//     This allows "fixing" stores created via code that lack these definitions.
//   - Description and Cache Configuration are ALWAYS editable.
//   - Admin Token can override locks (though UI may not expose this).
//
// handleUpdateStoreInfo handles updates to store metadata and structure.
//
// EDITING RULES:
// 1. Empty Store (Count == 0):
//   - Rebuild allowed for: Key Type, Value Type, Index Spec, CEL, Seed Data.
//   - LOCKED: Slot Length, IsUnique, ValueInNode (require Admin Token).
//   - No Admin Token required for allowed fields.
//
// 2. Non-Empty Store (Count > 0):
//   - Structural fields are LOCKED (Key/Value Type, Slot Length, IsUnique).
//   - EXCEPTION: Index Specification and CEL Expression can be updated IF AND ONLY IF they are currently empty (missing).
//     This allows "fixing" stores created via code that lack these definitions.
//   - Description and Cache Configuration are ALWAYS editable.
//   - Admin Token can override locks (though UI may not expose this).
func handleUpdateStoreInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Database      string            `json:"database"`
		StoreName     string            `json:"storeName"`
		CelExpression string            `json:"celExpression"`
		Description   string            `json:"description"`
		IndexSpec     *string           `json:"indexSpec"`
		KeyType       string            `json:"keyType"` // "map" or "primitive" (string, int, etc)
		SeedKey       any               `json:"seedKey"`
		SeedValue     any               `json:"seedValue"`
		AdminToken    string            `json:"adminToken"`
		AdminUsername string            `json:"adminUsername"`
		AdminPassword string            `json:"adminPassword"`
		SlotLength    int               `json:"slotLength"`
		IsUnique      bool              `json:"isUnique"`
		DataSize      int               `json:"dataSize"` // 0=Small, 1=Medium, 2=Big
		CacheDuration int               `json:"cacheDuration"`
		IsCacheTTL    bool              `json:"isCacheTTL"`
		ValueType     string            `json:"valueType"`
		Relations     []sop.Relation    `json:"relations"`
		Schema        map[string]string `json:"schema"`
		KeyFields     []string          `json:"keyFields"`
		ValueFields   []string          `json:"valueFields"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("Failed to decode JSON", "error", err)
		http.Error(w, fmt.Sprintf("Invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}

	if authToken := extractBearerToken(r.Header.Get("Authorization")); authToken != "" && req.AdminToken == "" {
		req.AdminToken = authToken
	}
	if username, password := config.authFromAuthorization(r.Header.Get("Authorization")); username != "" || password != "" {
		req.AdminUsername = username
		req.AdminPassword = password
	}

	// Hardening: Prevent modifying stores in System DB
	if IsSystemDB(req.Database) {
		http.Error(w, "Access Denied: Modifying store configuration in the System DB is not allowed.", http.StatusForbidden)
		return
	}

	// We need to handle IndexSpec as a string (JSON) or object.
	// Since the frontend sends it as a JSON string (via JSON.stringify), we should decode it as string first,
	// OR fix the frontend to send it as an object.
	// The frontend sends: indexSpec: JSON.stringify(...) -> which is a string.
	// So req.IndexSpec should be *string.

	if req.StoreName == "" {
		http.Error(w, "Store name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, req.Database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Always rollback if not committed.
	defer trans.Rollback(ctx)

	// Open the store to get current info
	comparer := func(a, b any) int { return 0 }
	store, err := database.OpenBtree[any, any](ctx, dbOpts, req.StoreName, trans, comparer)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open store '%s': %v", req.StoreName, err), http.StatusInternalServerError)
		return
	}

	si := store.GetStoreInfo()

	// DEBUG LOGGING
	log.Info("UpdateStoreInfo", "Name", req.StoreName, "Desc", req.Description)
	log.Info("REQ", "Cel", req.CelExpression, "IndexSpec", req.IndexSpec, "SeedValue", req.SeedValue)
	log.Info("SI", "Cel", si.CELexpression, "IndexSpec", si.MapKeyIndexSpecification)
	if req.IndexSpec != nil {
		log.Info("IndexSpec Diff", "REQ", *req.IndexSpec, "SI", si.MapKeyIndexSpecification)
	}

	// Update Description and metadata hints (Allowed for all stores)
	si.Description = req.Description
	si.Relations = req.Relations
	if req.Schema != nil {
		si.Schema = req.Schema
	}
	if req.KeyFields != nil {
		si.KeyFields = append([]string(nil), req.KeyFields...)
	}
	if req.ValueFields != nil {
		si.ValueFields = append([]string(nil), req.ValueFields...)
	}

	// Normalize the requested structural values so stale/default UI fields do not
	// get misread as real structural changes on populated stores.
	requestedSlotLength := req.SlotLength
	requestedIsUnique := req.IsUnique
	requestedDataSize := sop.ValueDataSize(req.DataSize)
	if si.Count > 0 {
		if requestedSlotLength <= 0 {
			requestedSlotLength = si.SlotLength
		}
		if !requestedIsUnique && si.IsUnique {
			requestedIsUnique = si.IsUnique
		}
		if req.DataSize == 0 && si.ValueDataSize() != sop.SmallData {
			requestedDataSize = si.ValueDataSize()
		}
	}

	// Compute StoreOptions based on DataSize
	// 0=Small, 1=Medium, 2=Big
	computedOpts := sop.ConfigureStore(req.StoreName, requestedIsUnique, requestedSlotLength, req.Description, requestedDataSize, "")
	dataSize := requestedDataSize

	// Update Cache Config
	// IMPORTANT:
	// - UI only configures Value cache duration + TTL.
	// - Do NOT overwrite other cache settings; SOP defaults should remain intact.
	// - When Value data is stored in-node, Value caching is not applicable; ignore cache updates.
	isCacheTTL := req.IsCacheTTL
	newDuration := time.Duration(req.CacheDuration) * time.Minute

	// Handle "No Cache" (-1 from UI)
	if req.CacheDuration < 0 {
		newDuration = 0
		isCacheTTL = false
	} else if newDuration == 0 {
		// 0 means default, but here we might want to respect what ConfigureStore returned or keep existing?
		// Actually, UI sends 0 for "Use Default".
		// But wait, ConfigureStore sets defaults.
		// If user sends 0, we should probably stick to what ConfigureStore gave us OR what is currently there?
		// Let's assume 0 means "don't change" or "default".
		// If we are updating, we should probably respect the computedOpts defaults if it's a structural change,
		// or keep existing if not.
		// However, the requirement says: "When size is Medium... allow user to convey 'no caching' (-1)..."
		// So if 0, it implies default.
		isCacheTTL = false
	}

	// Validate that no structural changes are attempted, unless authorized as root
	var structuralChange bool
	var shouldAddSeed bool

	// Check for structural changes using normalized, current store values.
	slotLengthChanged := requestedSlotLength > 0 && requestedSlotLength != si.SlotLength
	isUniqueChanged := requestedIsUnique != si.IsUnique
	dataSizeChanged := requestedDataSize != si.ValueDataSize()

	if slotLengthChanged || isUniqueChanged || dataSizeChanged {
		if si.Count > 0 {
			http.Error(w, "Structural fields (SlotLength, IsUnique, Data Size) cannot be changed for non-empty stores.", http.StatusBadRequest)
			return
		}
		if slotLengthChanged {
			si.SlotLength = req.SlotLength
		}
		if isUniqueChanged {
			si.IsUnique = req.IsUnique
		}

		// Apply computed options
		si.IsValueDataInNodeSegment = computedOpts.IsValueDataInNodeSegment
		si.IsValueDataActivelyPersisted = computedOpts.IsValueDataActivelyPersisted
		si.IsValueDataGloballyCached = computedOpts.IsValueDataGloballyCached

		structuralChange = true
	}

	// Apply Cache Config if allowed (Medium Data)
	if dataSize == sop.MediumData {
		if req.CacheDuration == -1 {
			// User explicitly wants to disable caching
			si.CacheConfig.ValueDataCacheDuration = -1 * time.Minute
			si.CacheConfig.IsValueDataCacheTTL = false
		} else if req.CacheDuration > 0 {
			// User specified a duration
			si.CacheConfig.ValueDataCacheDuration = time.Duration(req.CacheDuration) * time.Minute
			si.CacheConfig.IsValueDataCacheTTL = isCacheTTL
		} else {
			// req.CacheDuration == 0. Use Default?
			// If we are updating, and user sends 0, maybe they mean "don't change" or "reset to default"?
			// In the context of "Advanced Mode" dropdowns, usually 0 is "Default".
			// If I want to reset to default:
			si.CacheConfig.ValueDataCacheDuration = computedOpts.CacheConfig.ValueDataCacheDuration
			si.CacheConfig.IsValueDataCacheTTL = computedOpts.CacheConfig.IsValueDataCacheTTL
		}
	} else {
		// For Small and Big, enforce computed defaults
		si.CacheConfig.ValueDataCacheDuration = computedOpts.CacheConfig.ValueDataCacheDuration
		si.CacheConfig.IsValueDataCacheTTL = computedOpts.CacheConfig.IsValueDataCacheTTL
	}

	// Check for other changes
	celChanged := req.CelExpression != si.CELexpression

	var indexSpecChanged bool
	if req.IndexSpec != nil {
		normalizedReq := normalizeJSON(*req.IndexSpec)
		normalizedSI := normalizeJSON(si.MapKeyIndexSpecification)
		indexSpecChanged = normalizedReq != normalizedSI
	}

	seedValueChanged := req.SeedValue != nil

	if celChanged || indexSpecChanged || seedValueChanged {
		// Allow setting Index/CEL if they are currently empty (fixing a store created without them)
		// Also allow updating Seed Value (Schema) as it's just metadata for the UI.
		// Otherwise, require Admin Token for structural changes.
		// One-time fix behavior:
		// - Non-empty store: IndexSpec may be set only if currently missing.
		// - CEL is also structural, and is only allowed when Index is editable.
		//   That means: for non-empty stores, CEL can only be set when IndexSpec is missing.
		isFixingMissingIndex := indexSpecChanged && si.MapKeyIndexSpecification == ""
		isFixingMissingCel := celChanged && si.CELexpression == "" && si.MapKeyIndexSpecification == ""
		isFixingMissingSpec := isFixingMissingIndex || isFixingMissingCel

		// We allow seed value changes freely as they don't affect the B-Tree structure, only UI hints.
		isSeedChangeOnly := seedValueChanged && !celChanged && !indexSpecChanged

		// STRICT RULE: If store is not empty, we ONLY allow fixing MISSING specs.
		// If a spec already exists, it CANNOT be changed, even with Admin Token.
		// This prevents corruption of existing B-Tree data.

		// Check if we are trying to change an EXISTING spec on a non-empty store
		isModifyingExistingSpec := false
		if si.Count > 0 {
			if celChanged && si.CELexpression != "" {
				isModifyingExistingSpec = true
			}
			if indexSpecChanged && si.MapKeyIndexSpecification != "" {
				isModifyingExistingSpec = true
			}
		}

		isAdmin := config.IsAdminOverride(ctx, req.AdminUsername, req.AdminPassword, req.AdminToken) || isAdminRoleInContext(r.Context())

		if isModifyingExistingSpec {
			if !isAdmin {
				http.Error(w, "Cannot modify existing Index Specification or CEL Expression on a non-empty store. This action risks data corruption. Please delete and recreate the store if you need to change its structure.", http.StatusBadRequest)
				return
			}
			log.Warn("Admin Token used to modify existing Index/CEL on non-empty store", "Store", req.StoreName)
		}

		// If IndexSpec already exists on a non-empty store, CEL is locked even if missing.
		if si.Count > 0 && celChanged && si.CELexpression == "" && si.MapKeyIndexSpecification != "" {
			if !isAdmin {
				http.Error(w, "Cannot set CEL Expression on a non-empty store that already has an Index Specification. This action is intentionally blocked to avoid structural mistakes. Use manual file edit (storeinfo.txt) or recreate the store.", http.StatusBadRequest)
				return
			}
		}

		authorized := (si.Count == 0) || isFixingMissingSpec || isSeedChangeOnly || isAdmin

		if !authorized {
			if celChanged {
				http.Error(w, "Cannot update existing CEL Expression on a non-empty store. Please delete and recreate the store if you need to change its structure.", http.StatusBadRequest)
				return
			}
			if indexSpecChanged {
				http.Error(w, "Cannot update existing Index Specification on a non-empty store. Please delete and recreate the store if you need to change its structure.", http.StatusBadRequest)
				return
			}
			// Seed value check removed as it is now allowed or covered by authorized
		}
		// Apply authorized changes
		if celChanged {
			// If fixing, ensure we don't overwrite existing if not authorized (though logic above handles it)
			si.CELexpression = req.CelExpression
			structuralChange = true
		}
		if indexSpecChanged {
			si.MapKeyIndexSpecification = *req.IndexSpec
			structuralChange = true
		}
		if seedValueChanged {
			// We don't persist SeedValue in StoreInfo directly as it's inferred from the first item.
			// So we update the first item if it exists, or add it if empty.
			if si.Count > 0 {
				if ok, _ := store.First(ctx); ok {
					key := store.GetCurrentKey().Key
					// Only update if value changed significantly?
					// For now, we trust the user wants to update the sample value.
					if _, err := store.Update(ctx, key, req.SeedValue); err != nil {
						http.Error(w, fmt.Sprintf("Failed to update sample item with new value: %v", err), http.StatusInternalServerError)
						return
					}
					structuralChange = true
				}
			} else {
				// If empty, we can't update an item.
				// But handleAddStore adds it. Here we are updating.
				// If the store is empty, we could add the seed item?
				// But we don't have the SeedKey here easily (it's in req but might be nil if not sent).
				// If req.SeedKey is provided, we can add.
				if req.SeedKey != nil {
					// Defer adding seed data until after metadata is saved to avoid overwrite issues
					shouldAddSeed = true
					structuralChange = true
				}
			}
		}
	}
	if req.KeyType != "" {
		isPrimitive := req.KeyType != "map"
		if si.IsPrimitiveKey != isPrimitive {
			// We allow Key Type metadata change even on non-empty stores as it is a hint for bindings.
			// It does not affect the physical B-Tree structure in Go.
			si.IsPrimitiveKey = isPrimitive
			structuralChange = true
		}
	}

	// Update StoreInfo via StoreRepository
	t := trans.GetPhasedTransaction()
	if ct, ok := t.(*common.Transaction); ok {
		// Tell engine this store needs meta data save.
		si.NeedsMetaDataSave = true
		if _, err := ct.StoreRepository.Update(ctx, []sop.StoreInfo{si}); err != nil {
			http.Error(w, "Failed to update store info: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "Transaction type not supported for update", http.StatusInternalServerError)
		return
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Failed to commit transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if shouldAddSeed {
		// Start new transaction for data
		trans, err = database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
		if err != nil {
			http.Error(w, "Failed to begin transaction for seed data: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer trans.Rollback(ctx)

		// Re-open store (will pick up new metadata)
		store, err = database.OpenBtree[any, any](ctx, dbOpts, req.StoreName, trans, comparer)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to open store for seed data: %v", err), http.StatusInternalServerError)
			return
		}

		if _, err := store.Add(ctx, req.SeedKey, req.SeedValue); err != nil {
			http.Error(w, fmt.Sprintf("Failed to add seed item: %v", err), http.StatusInternalServerError)
			return
		}

		if err := trans.Commit(ctx); err != nil {
			http.Error(w, "Failed to commit seed data: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	resp := map[string]string{"status": "ok"}
	if structuralChange {
		resp["warning"] = "Overriding store metadata is safe but you need to make sure the new metadata being saved captures the correct sorting characteristics of this store."
	}
	json.NewEncoder(w).Encode(resp)
}

func handleListItems(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	storeName := r.URL.Query().Get("name")
	dbName := r.URL.Query().Get("database")
	if storeName == "" {
		http.Error(w, "Store name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, dbName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForReading)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	// Variables to hold state for the closure
	var indexSpec *jsondb.IndexSpecification
	var isPrimitiveKey bool

	// Check if primitive key before opening
	if t2, ok := trans.GetPhasedTransaction().(*common.Transaction); ok {
		stores, err := t2.StoreRepository.Get(ctx, storeName)
		if err == nil && len(stores) > 0 {
			isPrimitiveKey = stores[0].IsPrimitiveKey
			if !isPrimitiveKey && stores[0].MapKeyIndexSpecification != "" {
				var is jsondb.IndexSpecification
				if err := encoding.DefaultMarshaler.Unmarshal([]byte(stores[0].MapKeyIndexSpecification), &is); err == nil {
					indexSpec = &is
				}
			}
		}
	}

	var comparer btree.ComparerFunc[any]

	if !isPrimitiveKey {
		// Proxy comparer
		comparer = func(a, b any) int {
			mapA, okA := a.(map[string]any)
			mapB, okB := b.(map[string]any)
			if !okA || !okB {
				return btree.Compare(a, b)
			}

			if indexSpec != nil {
				return indexSpec.Comparer(mapA, mapB)
			}

			// Default Map Comparer (Dynamic)
			// Collect all keys, sort them, compare values.
			keys := make([]string, 0, len(mapA)+len(mapB))
			seen := make(map[string]struct{})
			for k := range mapA {
				if _, exists := seen[k]; !exists {
					keys = append(keys, k)
					seen[k] = struct{}{}
				}
			}
			for k := range mapB {
				if _, exists := seen[k]; !exists {
					keys = append(keys, k)
					seen[k] = struct{}{}
				}
			}
			// We need to sort keys to be deterministic
			sort.Strings(keys)

			for _, k := range keys {
				valA, existsA := mapA[k]
				valB, existsB := mapB[k]

				if !existsA && !existsB {
					continue
				}
				if !existsA {
					return -1 // A is missing key, so A < B
				}
				if !existsB {
					return 1 // B is missing key, so A > B
				}

				res := btree.Compare(valA, valB)
				if res != 0 {
					return res
				}
			}
			return 0
		}
	}

	// Open the B-Tree using 'any' for Key and Value to support generic browsing.
	store, err := database.OpenBtree[any, any](ctx, dbOpts, storeName, trans, comparer)
	if err != nil {
		log.Error(fmt.Sprintf("Failed to open store '%s': %v.", storeName, err))
		http.Error(w, fmt.Sprintf("Failed to open store '%s': %v.", storeName, err), http.StatusInternalServerError)
		return
	}

	// Configure the comparer based on StoreInfo
	si := store.GetStoreInfo()
	if !si.IsPrimitiveKey {
		if si.MapKeyIndexSpecification != "" {
			var is jsondb.IndexSpecification
			if err := encoding.DefaultMarshaler.Unmarshal([]byte(si.MapKeyIndexSpecification), &is); err == nil {
				indexSpec = &is
			} else {
				log.Warn(fmt.Sprintf("Error unmarshaling index spec: %v", err))
			}
		}
	}

	// Detect sample key for primitive types to guide parsing
	var sampleKey any
	if si.IsPrimitiveKey && si.Count > 0 {
		ok, err := store.First(ctx)
		if err != nil {
			log.Error(fmt.Sprintf("Failed to get first item for sample key: %v", err))
			http.Error(w, fmt.Sprintf("Failed to fetch initial page: %v", err), http.StatusInternalServerError)
			return
		}
		if ok {
			sampleKey = store.GetCurrentKey().Key
		}
	}
	var items []map[string]any
	limit := config.PageSize
	if limit <= 0 {
		limit = 100
	}
	count := 0

	// Determine start position
	query := r.URL.Query().Get("q")
	action := r.URL.Query().Get("action")
	refKeyStr := r.URL.Query().Get("key")

	var ok bool

	// Helper to parse key
	parseKey := func(kStr string) any {
		var k any = kStr
		if !isPrimitiveKey {
			var mapKey map[string]any
			if err := json.Unmarshal([]byte(kStr), &mapKey); err == nil {
				k = mapKey
			}
			return k
		}

		// Primitive Key Parsing based on sample
		if sampleKey != nil {
			switch sampleKey.(type) {
			case int:
				if v, err := strconv.Atoi(kStr); err == nil {
					return v
				}
			case int8:
				if v, err := strconv.ParseInt(kStr, 10, 8); err == nil {
					return int8(v)
				}
			case int16:
				if v, err := strconv.ParseInt(kStr, 10, 16); err == nil {
					return int16(v)
				}
			case int32:
				if v, err := strconv.ParseInt(kStr, 10, 32); err == nil {
					return int32(v)
				}
			case int64:
				if v, err := strconv.ParseInt(kStr, 10, 64); err == nil {
					return v
				}
			case uint:
				if v, err := strconv.ParseUint(kStr, 10, 64); err == nil {
					return uint(v)
				}
			case uint8:
				if v, err := strconv.ParseUint(kStr, 10, 8); err == nil {
					return uint8(v)
				}
			case uint16:
				if v, err := strconv.ParseUint(kStr, 10, 16); err == nil {
					return uint16(v)
				}
			case uint32:
				if v, err := strconv.ParseUint(kStr, 10, 32); err == nil {
					return uint32(v)
				}
			case uint64:
				if v, err := strconv.ParseUint(kStr, 10, 64); err == nil {
					return v
				}
			case float32:
				if v, err := strconv.ParseFloat(kStr, 32); err == nil {
					return float32(v)
				}
			case float64:
				if v, err := strconv.ParseFloat(kStr, 64); err == nil {
					return v
				}
			case string:
				return kStr
			case sop.UUID:
				if v, err := sop.ParseUUID(kStr); err == nil {
					return v
				}
			case uuid.UUID:
				if v, err := uuid.Parse(kStr); err == nil {
					return v
				}
			}
		}

		// Fallback guessing if empty store or unknown type
		if _, err := uuid.Parse(kStr); err == nil {
			// It looks like a UUID, so return it as uuid.UUID
			// This matches jsondb behavior where we infer type from string format
			u, _ := uuid.Parse(kStr)
			return u
		}

		var i int
		if _, err := fmt.Sscanf(kStr, "%d", &i); err == nil {
			// Check if the original string was actually just digits
			// This prevents "123abc" being parsed as 123
			if fmt.Sprintf("%d", i) == kStr {
				k = i
			}
		}
		return k
	}

	switch action {
	case "first":
		ok, err = store.First(ctx)
	case "last":
		ok, err = store.Last(ctx)
		if ok {
			// Go back limit-1 items to find start of last page
			for i := 0; i < limit-1; i++ {
				if okPrev, _ := store.Previous(ctx); !okPrev {
					store.First(ctx)
					break
				}
			}
		}
	case "current":
		if refKeyStr != "" {
			k := parseKey(refKeyStr)
			// Find(ctx, k, true) positions cursor at k or next item if k is missing.
			// If the store is already empty, do not fall back to a stale cursor item.
			ok, err = store.Find(ctx, k, true)
			if !ok && err == nil && store.Count() > 0 {
				if _, err := store.GetCurrentItem(ctx); err == nil {
					ok = true
				}
			}
		} else {
			ok, err = store.First(ctx)
		}
	case "next":
		if refKeyStr != "" {
			k := parseKey(refKeyStr)
			if found, _ := store.Find(ctx, k, true); found {
				ok, err = store.Next(ctx)
			} else {
				// If key not found, fallback to first
				ok, err = store.First(ctx)
			}
		} else {
			ok, err = store.First(ctx)
		}
	case "prev":
		if refKeyStr != "" {
			k := parseKey(refKeyStr)
			if found, _ := store.Find(ctx, k, true); found {
				if ok, _ = store.Previous(ctx); ok {
					// Go back limit-1 more
					for i := 0; i < limit-1; i++ {
						if okPrev, _ := store.Previous(ctx); !okPrev {
							store.First(ctx)
							break
						}
					}
					// We are positioned at the start of the previous page
					ok = true
				} else {
					// Already at start
					ok, err = store.First(ctx)
				}
			} else {
				ok, err = store.First(ctx)
			}
		} else {
			ok, err = store.First(ctx)
		}
	default:
		if query != "" {
			// Try to find the item with the query key.
			searchKey := parseKey(query)
			fmt.Printf("searchKey: %v/n", searchKey)
			ok, err = store.Find(ctx, searchKey, false)
			if store.GetCurrentKey().Key != nil {
				ok = true
			}
		} else {
			// Iterate from the first item
			ok, err = store.First(ctx)
		}
	}

	// Check for NDJSON Streaming Mode
	isNDJSON := r.Header.Get("Accept") == "application/x-ndjson"
	if isNDJSON {
		w.Header().Set("Content-Type", "application/x-ndjson")
	}

	var encoder *json.Encoder
	if isNDJSON {
		encoder = json.NewEncoder(w)
	}

	for ok && err == nil && count < limit {
		kItem := store.GetCurrentKey()
		v, err := store.GetCurrentValue(ctx)
		if err != nil {
			log.Error(fmt.Sprintf("Error reading value for key %v: %v", kItem.Key, err))
		}

		itemMap := map[string]any{
			"key": kItem.Key,
		}
		if valueMap, ok := v.(map[string]any); ok {
			itemValue := make(map[string]any, len(valueMap))
			for k, val := range valueMap {
				itemValue[k] = val
			}
			if _, exists := itemValue["lifecycle_status"]; !exists {
				if lifecycleStatus, ok := itemValue["lifecycle_status"]; ok {
					itemValue["lifecycle_status"] = lifecycleStatus
				}
			}
			itemMap["value"] = itemValue
		} else {
			itemMap["value"] = v
		}

		if isNDJSON {
			if err := encoder.Encode(itemMap); err != nil {
				log.Error(fmt.Sprintf("Error streaming item: %v", err))
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		} else {
			items = append(items, itemMap)
		}

		ok, err = store.Next(ctx)
		count++
	}

	if err != nil {
		log.Error(fmt.Sprintf("Error during iteration: %v", err))
	}

	if !isNDJSON {
		if items == nil {
			items = make([]map[string]any, 0)
		}

		response := map[string]any{
			"data": items,
		}

		json.NewEncoder(w).Encode(response)
	}
}

func handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Database  string `json:"database"`
		StoreName string `json:"store"`
		Key       any    `json:"key"`
		Value     any    `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, req.Database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Open transaction for writing
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Ensure rollback in case of error, but we will Commit on success
	defer trans.Rollback(ctx)

	// We need to open the store to perform update
	// We need to determine if it's a primitive key store to set up the comparer correctly
	// and to cast the key from JSON (float64) to the correct type (int, etc).

	// Peek at store info first (requires a read transaction or just checking repository)
	// But we are already in a write transaction. We can access the store repository directly via the transaction.
	var isPrimitiveKey bool
	var indexSpec *jsondb.IndexSpecification

	if t2, ok := trans.GetPhasedTransaction().(*common.Transaction); ok {
		stores, err := t2.StoreRepository.Get(ctx, req.StoreName)
		if err == nil && len(stores) > 0 {
			isPrimitiveKey = stores[0].IsPrimitiveKey
			if !isPrimitiveKey && stores[0].MapKeyIndexSpecification != "" {
				var is jsondb.IndexSpecification
				if err := encoding.DefaultMarshaler.Unmarshal([]byte(stores[0].MapKeyIndexSpecification), &is); err == nil {
					indexSpec = &is
				}
			}
		}
	}

	var comparer btree.ComparerFunc[any]
	// If not primitive, we need a map comparer.
	// For update, we need to find the EXACT key.
	if !isPrimitiveKey {
		if indexSpec != nil {
			comparer = func(a, b any) int {
				return indexSpec.Comparer(a.(map[string]any), b.(map[string]any))
			}
		} else {
			// Default Map Comparer (Dynamic) for generic maps (e.g. ModelStore)
			comparer = func(a, b any) int {
				mapA, okA := a.(map[string]any)
				mapB, okB := b.(map[string]any)
				if !okA || !okB {
					return btree.Compare(a, b)
				}

				keys := make([]string, 0, len(mapA)+len(mapB))
				seen := make(map[string]struct{})
				for k := range mapA {
					if _, exists := seen[k]; !exists {
						keys = append(keys, k)
						seen[k] = struct{}{}
					}
				}
				for k := range mapB {
					if _, exists := seen[k]; !exists {
						keys = append(keys, k)
						seen[k] = struct{}{}
					}
				}
				sort.Strings(keys)

				for _, k := range keys {
					valA, existsA := mapA[k]
					valB, existsB := mapB[k]

					if !existsA && !existsB {
						continue
					}
					if !existsA {
						return -1
					}
					if !existsB {
						return 1
					}

					res := btree.Compare(valA, valB)
					if res != 0 {
						return res
					}
				}
				return 0
			}
		}
	}

	store, err := database.OpenBtree[any, any](ctx, dbOpts, req.StoreName, trans, comparer)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open store '%s': %v", req.StoreName, err), http.StatusInternalServerError)
		return
	}

	// Apply persisted schema coercion before writing the value.
	si := store.GetStoreInfo()
	finalKey, err := coerceRecordKey(req.Key, &si)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	finalValue, err := coerceRecordValue(req.Value, &si)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Detect sample key to cast req.Key
	var sampleKey any
	if isPrimitiveKey {
		if ok, _ := store.First(ctx); ok {
			sampleKey = store.GetCurrentKey().Key
		}
	}

	// Cast Key
	if sampleKey != nil {
		// Handle JSON number (float64) to Integer types
		if f, ok := req.Key.(float64); ok {
			switch sampleKey.(type) {
			case int:
				finalKey = int(f)
			case int8:
				finalKey = int8(f)
			case int16:
				finalKey = int16(f)
			case int32:
				finalKey = int32(f)
			case int64:
				finalKey = int64(f)
			case uint:
				finalKey = uint(f)
			case uint8:
				finalKey = uint8(f)
			case uint16:
				finalKey = uint16(f)
			case uint32:
				finalKey = uint32(f)
			case uint64:
				finalKey = uint64(f)
			case float32:
				finalKey = float32(f)
			}
		}
		// Handle String input for integer keys (UI compat)
		if s, ok := req.Key.(string); ok {
			switch sampleKey.(type) {
			case int:
				if v, err := strconv.Atoi(s); err == nil {
					finalKey = v
				}
			case int8:
				if v, err := strconv.ParseInt(s, 10, 8); err == nil {
					finalKey = int8(v)
				}
			case int16:
				if v, err := strconv.ParseInt(s, 10, 16); err == nil {
					finalKey = int16(v)
				}
			case int32:
				if v, err := strconv.ParseInt(s, 10, 32); err == nil {
					finalKey = int32(v)
				}
			case int64:
				if v, err := strconv.ParseInt(s, 10, 64); err == nil {
					finalKey = v
				}
			case uint:
				if v, err := strconv.ParseUint(s, 10, 64); err == nil {
					finalKey = uint(v)
				}
			case uint8:
				if v, err := strconv.ParseUint(s, 10, 8); err == nil {
					finalKey = uint8(v)
				}
			case uint16:
				if v, err := strconv.ParseUint(s, 10, 16); err == nil {
					finalKey = uint16(v)
				}
			case uint32:
				if v, err := strconv.ParseUint(s, 10, 32); err == nil {
					finalKey = uint32(v)
				}
			case uint64:
				if v, err := strconv.ParseUint(s, 10, 64); err == nil {
					finalKey = uint64(v)
				}
			case float32:
				if v, err := strconv.ParseFloat(s, 32); err == nil {
					finalKey = float32(v)
				}
			case float64:
				if v, err := strconv.ParseFloat(s, 64); err == nil {
					finalKey = v
				}
			case string:
				finalKey = s
			case sop.UUID:
				if v, err := sop.ParseUUID(s); err == nil {
					finalKey = v
				}
			case uuid.UUID:
				if v, err := uuid.Parse(s); err == nil {
					finalKey = v
				}
			}
		}
		// Handle String to UUID
		if s, ok := req.Key.(string); ok {
			switch sampleKey.(type) {
			case sop.UUID:
				if id, err := sop.ParseUUID(s); err == nil {
					finalKey = id
				}
			case uuid.UUID:
				if id, err := uuid.Parse(s); err == nil {
					finalKey = id
				}
			}
		}
	}

	// Special handling for "scripts" store (ModelStore validation)
	// ModelStore expects the value to be a JSON string, but the UI might send a JSON object.
	if req.StoreName == "scripts" {
		if _, isString := req.Value.(string); !isString {
			if b, err := json.Marshal(req.Value); err == nil {
				req.Value = string(b)
			}
		}
	}

	// Perform Update
	if ok, err := store.Update(ctx, finalKey, finalValue); err != nil {
		http.Error(w, "Update failed: "+err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "Update failed: Item not found", http.StatusNotFound)
		return
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleAddStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Database      string         `json:"database"`
		StoreName     string         `json:"store"`
		KeyType       string         `json:"key_type"` // string, int, uuid, map
		ValueType     string         `json:"value_type"`
		Description   string         `json:"description"`
		IndexSpec     string         `json:"index_spec"`     // Optional, for map keys
		CelExpression string         `json:"cel_expression"` // Optional, for custom sorting
		SeedKey       any            `json:"seed_key"`       // Optional, for seeding
		SeedValue     any            `json:"seed_value"`     // Optional, for seeding
		Relations     []sop.Relation `json:"relations"`

		// Store creation options.
		// NOTE: The UI may hide/show these behind an "Advanced" toggle, but the backend
		// must not depend on any UI-only concept like "advanced_mode".
		SlotLength    *int  `json:"slot_length"`
		IsUnique      *bool `json:"is_unique"`
		DataSize      *int  `json:"data_size"` // 0=Small, 1=Medium, 2=Big
		CacheDuration *int  `json:"cache_duration"`
		IsCacheTTL    *bool `json:"is_cache_ttl"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.StoreName == "" {
		http.Error(w, "Store name is required", http.StatusBadRequest)
		return
	}

	// Normalize Store name (convert spaces to underscores, lowercase)
	req.StoreName = normalizeStoreName(req.StoreName)

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, req.Database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Open transaction for writing
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	// Defaults (must not depend on any UI-only toggle)
	slotLength := 1000
	if req.SlotLength != nil {
		slotLength = *req.SlotLength
	}
	if slotLength < 2 {
		slotLength = 2
	}
	if slotLength > 10000 {
		slotLength = 10000
	}

	isUnique := true
	if req.IsUnique != nil {
		isUnique = *req.IsUnique
	}

	// Compute StoreOptions based on DataSize
	dataSize := sop.SmallData
	if req.DataSize != nil {
		dataSize = sop.ValueDataSize(*req.DataSize)
	}

	// Use ConfigureStore to get the correct structural flags and defaults
	computedOpts := sop.ConfigureStore(req.StoreName, isUnique, slotLength, req.Description, dataSize, "")

	cacheDuration := 0
	if req.CacheDuration != nil {
		cacheDuration = *req.CacheDuration
	}

	isCacheTTL := false
	if req.IsCacheTTL != nil {
		isCacheTTL = *req.IsCacheTTL
	}

	storeOpts := sop.StoreOptions{
		Name:                     req.StoreName,
		SlotLength:               slotLength,
		IsUnique:                 isUnique,
		Description:              req.Description,
		IsPrimitiveKey:           req.KeyType != "map",
		CELexpression:            req.CelExpression,
		MapKeyIndexSpecification: req.IndexSpec,
		Relations:                req.Relations,

		// Apply computed structural flags
		IsValueDataInNodeSegment:     computedOpts.IsValueDataInNodeSegment,
		IsValueDataActivelyPersisted: computedOpts.IsValueDataActivelyPersisted,
		IsValueDataGloballyCached:    computedOpts.IsValueDataGloballyCached,
	}

	// Apply Cache Config
	// If Medium Data, allow user override. Otherwise use computed defaults.
	if dataSize == sop.MediumData {
		// Start with computed defaults (which has correct global cache settings)
		storeOpts.CacheConfig = computedOpts.CacheConfig

		if cacheDuration == -1 {
			// User explicitly wants to disable caching
			storeOpts.CacheConfig.ValueDataCacheDuration = -1 * time.Minute
			storeOpts.CacheConfig.IsValueDataCacheTTL = false
		} else if cacheDuration > 0 {
			// User specified a duration
			storeOpts.CacheConfig.ValueDataCacheDuration = time.Duration(cacheDuration) * time.Minute
			storeOpts.CacheConfig.IsValueDataCacheTTL = isCacheTTL
		} else {
			// cacheDuration == 0. Use Default (already set from computedOpts)
		}
	} else {
		// For Small and Big, enforce computed defaults
		storeOpts.CacheConfig = computedOpts.CacheConfig
	}

	// Cast SeedValue based on ValueType
	finalSeedValue := req.SeedValue
	if req.SeedValue != nil {
		switch req.ValueType {
		case "int":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = int(f)
			}
		case "int8":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = int8(f)
			}
		case "int16":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = int16(f)
			}
		case "int32":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = int32(f)
			}
		case "int64":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = int64(f)
			}
		case "uint":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = uint(f)
			}
		case "uint8":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = uint8(f)
			}
		case "uint16":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = uint16(f)
			}
		case "uint32":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = uint32(f)
			}
		case "uint64":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = uint64(f)
			}
		case "float32":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = float32(f)
			}
		case "rune":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = rune(f)
			}
		case "uuid":
			if s, ok := req.SeedValue.(string); ok {
				if id, err := sop.ParseUUID(s); err == nil {
					finalSeedValue = id
				}
			}
		case "timestamp":
			if f, ok := req.SeedValue.(float64); ok {
				finalSeedValue = int64(f)
			}
		}
	}

	seedKeyToUse := req.SeedKey
	if req.SeedValue != nil && seedKeyToUse == nil {
		switch req.KeyType {
		case "string":
			seedKeyToUse = ""
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "rune":
			seedKeyToUse = 0
		case "float32", "float64":
			seedKeyToUse = 0.0
		case "bool":
			seedKeyToUse = false
		case "uuid":
			seedKeyToUse = sop.UUID{}
		case "map":
			seedKeyToUse = map[string]any{}
		case "array":
			seedKeyToUse = []any{}
		default:
			seedKeyToUse = ""
		}
	}

	var storeErr error
	switch req.KeyType {
	case "string":
		var s btree.BtreeInterface[string, any]
		s, storeErr = database.NewBtree[string, any](ctx, dbOpts, req.StoreName, trans, nil, storeOpts)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			if kStr, ok := seedKeyToUse.(string); ok {
				_, storeErr = s.Add(ctx, kStr, finalSeedValue)
			}
		}
	case "int":
		var s btree.BtreeInterface[int, any]
		s, storeErr = database.NewBtree[int, any](ctx, dbOpts, req.StoreName, trans, nil, storeOpts)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			if f, ok := seedKeyToUse.(float64); ok {
				_, storeErr = s.Add(ctx, int(f), finalSeedValue)
			} else if i, ok := seedKeyToUse.(int); ok {
				_, storeErr = s.Add(ctx, i, finalSeedValue)
			}
		}
	case "uuid":
		var s btree.BtreeInterface[sop.UUID, any]
		s, storeErr = database.NewBtree[sop.UUID, any](ctx, dbOpts, req.StoreName, trans, nil, storeOpts)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			if str, ok := seedKeyToUse.(string); ok {
				if id, err2 := sop.ParseUUID(str); err2 == nil {
					_, storeErr = s.Add(ctx, id, finalSeedValue)
				} else {
					storeErr = err2
				}
			} else if id, ok := seedKeyToUse.(sop.UUID); ok {
				_, storeErr = s.Add(ctx, id, finalSeedValue)
			}
		}
	case "map":
		var s *jsondb.JsonDBMapKey
		s, storeErr = jsondb.NewJsonBtreeMapKey(ctx, dbOpts, storeOpts, trans, req.IndexSpec)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			// JsonDBMapKey expects []jsondb.Item[map[string]any, any] for Add
			if kMap, ok := seedKeyToUse.(map[string]any); ok {
				item := jsondb.Item[map[string]any, any]{
					Key:   kMap,
					Value: &finalSeedValue,
				}
				_, storeErr = s.Add(ctx, []jsondb.Item[map[string]any, any]{item})
			}
		}
	case "array":
		var s btree.BtreeInterface[[]any, any]
		s, storeErr = database.NewBtree[[]any, any](ctx, dbOpts, req.StoreName, trans, nil, storeOpts)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			if kArr, ok := seedKeyToUse.([]any); ok {
				_, storeErr = s.Add(ctx, kArr, finalSeedValue)
			}
		}
	case "int64":
		var s btree.BtreeInterface[int64, any]
		s, storeErr = database.NewBtree[int64, any](ctx, dbOpts, req.StoreName, trans, nil, storeOpts)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			if f, ok := seedKeyToUse.(float64); ok {
				_, storeErr = s.Add(ctx, int64(f), finalSeedValue)
			} else if i64, ok := seedKeyToUse.(int64); ok {
				_, storeErr = s.Add(ctx, i64, finalSeedValue)
			}
		}
	case "float64":
		var s btree.BtreeInterface[float64, any]
		s, storeErr = database.NewBtree[float64, any](ctx, dbOpts, req.StoreName, trans, nil, storeOpts)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			if f, ok := seedKeyToUse.(float64); ok {
				_, storeErr = s.Add(ctx, f, finalSeedValue)
			}
		}
	case "bool":
		var s btree.BtreeInterface[bool, any]
		s, storeErr = database.NewBtree[bool, any](ctx, dbOpts, req.StoreName, trans, nil, storeOpts)
		if storeErr == nil && seedKeyToUse != nil && req.SeedValue != nil {
			if b, ok := seedKeyToUse.(bool); ok {
				_, storeErr = s.Add(ctx, b, finalSeedValue)
			}
		}
	default:
		http.Error(w, "Invalid key type. Supported: string, int, int64, float64, bool, uuid, map, array", http.StatusBadRequest)
		return
	}

	if storeErr != nil {
		http.Error(w, "Failed to create store: "+storeErr.Error(), http.StatusInternalServerError)
		return
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Store created successfully"})
}

func handleDeleteStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Database  string `json:"database"`
		StoreName string `json:"store"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Allow deleting non-script stores from the System DB while protecting the scripts store.
	if IsSystemDB(req.Database) && strings.EqualFold(req.StoreName, "scripts") {
		http.Error(w, "Access Denied: The scripts store is protected and cannot be deleted.", http.StatusForbidden)
		return
	}

	if req.StoreName == "" {
		http.Error(w, "Store name is required", http.StatusBadRequest)
		return
	}

	dbOpts, err := GetDBOptions(r.Context(), req.Database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	aiDB := aidb.NewDatabase(dbOpts)
	if err := aiDB.RemoveKnowledgeBase(r.Context(), req.StoreName); err != nil {
		if err := database.RemoveBtree(r.Context(), dbOpts, req.StoreName); err != nil {
			http.Error(w, "Failed to delete store: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Store deleted successfully"})
}

func handleDeleteSpace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Database string `json:"database"`
		Space    string `json:"space"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Space == "" {
		http.Error(w, "Space name is required", http.StatusBadRequest)
		return
	}

	dbOpts, err := GetDBOptions(r.Context(), req.Database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	aiDB := aidb.NewDatabase(dbOpts)
	if err := aiDB.RemoveKnowledgeBase(r.Context(), req.Space); err != nil {
		http.Error(w, "Failed to delete space: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Space deleted successfully"})
}

func handleAddItem(w http.ResponseWriter, r *http.Request) {
	handleWriteOperation(w, r, "add")
}

func handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	handleWriteOperation(w, r, "delete")
}

func handleDeleteItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Database  string `json:"database"`
		StoreName string `json:"store"`
		Keys      []any  `json:"keys"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.StoreName == "" {
		http.Error(w, "Store name is required", http.StatusBadRequest)
		return
	}
	if len(req.Keys) == 0 {
		http.Error(w, "At least one key is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, req.Database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	var isPrimitiveKey bool
	var indexSpec *jsondb.IndexSpecification
	if t2, ok := trans.GetPhasedTransaction().(*common.Transaction); ok {
		stores, err := t2.StoreRepository.Get(ctx, req.StoreName)
		if err == nil && len(stores) > 0 {
			isPrimitiveKey = stores[0].IsPrimitiveKey
			if !isPrimitiveKey && stores[0].MapKeyIndexSpecification != "" {
				var is jsondb.IndexSpecification
				if err := encoding.DefaultMarshaler.Unmarshal([]byte(stores[0].MapKeyIndexSpecification), &is); err == nil {
					indexSpec = &is
				}
			}
		}
	}

	var comparer btree.ComparerFunc[any]
	if !isPrimitiveKey {
		if indexSpec != nil {
			comparer = func(a, b any) int {
				return indexSpec.Comparer(a.(map[string]any), b.(map[string]any))
			}
		} else {
			comparer = func(a, b any) int {
				mapA, okA := a.(map[string]any)
				mapB, okB := b.(map[string]any)
				if !okA || !okB {
					return btree.Compare(a, b)
				}

				keys := make([]string, 0, len(mapA)+len(mapB))
				seen := make(map[string]struct{})
				for k := range mapA {
					if _, exists := seen[k]; !exists {
						keys = append(keys, k)
						seen[k] = struct{}{}
					}
				}
				for k := range mapB {
					if _, exists := seen[k]; !exists {
						keys = append(keys, k)
						seen[k] = struct{}{}
					}
				}
				sort.Strings(keys)

				for _, k := range keys {
					valA, existsA := mapA[k]
					valB, existsB := mapB[k]
					if !existsA && !existsB {
						continue
					}
					if !existsA {
						return -1
					}
					if !existsB {
						return 1
					}
					res := btree.Compare(valA, valB)
					if res != 0 {
						return res
					}
				}
				return 0
			}
		}
	}

	store, err := database.OpenBtree[any, any](ctx, dbOpts, req.StoreName, trans, comparer)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open store '%s': %v", req.StoreName, err), http.StatusInternalServerError)
		return
	}

	var sampleKey any
	if isPrimitiveKey {
		if ok, _ := store.First(ctx); ok {
			sampleKey = store.GetCurrentKey().Key
		}
	}

	resolvedKeys := make([]any, 0, len(req.Keys))
	preservedCount := 0
	preserveOne := int(store.Count()) <= len(req.Keys)
	for _, rawKey := range req.Keys {
		finalKey := rawKey
		if sampleKey != nil {
			if f, ok := rawKey.(float64); ok {
				switch sampleKey.(type) {
				case int:
					finalKey = int(f)
				case int8:
					finalKey = int8(f)
				case int16:
					finalKey = int16(f)
				case int32:
					finalKey = int32(f)
				case int64:
					finalKey = int64(f)
				case uint:
					finalKey = uint(f)
				case uint8:
					finalKey = uint8(f)
				case uint16:
					finalKey = uint16(f)
				case uint32:
					finalKey = uint32(f)
				case uint64:
					finalKey = uint64(f)
				case float32:
					finalKey = float32(f)
				}
			}
			if s, ok := rawKey.(string); ok {
				switch sampleKey.(type) {
				case sop.UUID:
					if id, err := sop.ParseUUID(s); err == nil {
						finalKey = id
					}
				case uuid.UUID:
					if id, err := uuid.Parse(s); err == nil {
						finalKey = id
					}
				}
			}
		}

		if sampleKey != nil {
			sT := fmt.Sprintf("%T", sampleKey)
			fT := fmt.Sprintf("%T", finalKey)
			if sT != fT {
				http.Error(w, fmt.Sprintf("Key type mismatch: expected %s, got %s", sT, fT), http.StatusBadRequest)
				return
			}
		}

		if preserveOne && preservedCount == 0 {
			preservedCount++
			continue
		}
		resolvedKeys = append(resolvedKeys, finalKey)
	}

	if store.Count()-int64(len(resolvedKeys)) < 1 {
		http.Error(w, "At least one row must remain in the store for transaction merging.", http.StatusBadRequest)
		return
	}

	deletedCount := 0
	for _, key := range resolvedKeys {
		removed, err := store.Remove(ctx, key)
		if err != nil {
			http.Error(w, "Failed to delete key: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !removed {
			http.Error(w, "One or more keys were not found", http.StatusNotFound)
			return
		}
		deletedCount++
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "deleted": deletedCount, "preserved": preservedCount})
}

func handleWriteOperation(w http.ResponseWriter, r *http.Request, op string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Database  string `json:"database"`
		StoreName string `json:"store"`
		Key       any    `json:"key"`
		Value     any    `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Hardening: Prevent manual modification of critical System DB stores
	// Users should not manually add items to memory as it is managed by the AI/System
	if IsSystemDB(req.Database) && req.StoreName == "memory" {
		if op == "add" {
			http.Error(w, "Access Denied: The 'memory' store is managed by the system. Manual additions are restricted.", http.StatusForbidden)
			return
		}
	}

	ctx := r.Context()
	dbOpts, err := GetDBOptions(ctx, req.Database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Open transaction for writing
	trans, err := database.BeginTransaction(ctx, dbOpts, sop.ForWriting)
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer trans.Rollback(ctx)

	// Determine if primitive key
	var isPrimitiveKey bool
	var indexSpec *jsondb.IndexSpecification

	if t2, ok := trans.GetPhasedTransaction().(*common.Transaction); ok {
		stores, err := t2.StoreRepository.Get(ctx, req.StoreName)
		if err == nil && len(stores) > 0 {
			isPrimitiveKey = stores[0].IsPrimitiveKey
			if !isPrimitiveKey && stores[0].MapKeyIndexSpecification != "" {
				var is jsondb.IndexSpecification
				if err := encoding.DefaultMarshaler.Unmarshal([]byte(stores[0].MapKeyIndexSpecification), &is); err == nil {
					indexSpec = &is
				}
			}
		}
	}

	var comparer btree.ComparerFunc[any]
	if !isPrimitiveKey {
		if indexSpec != nil {
			comparer = func(a, b any) int {
				return indexSpec.Comparer(a.(map[string]any), b.(map[string]any))
			}
		} else {
			// Default Map Comparer (Dynamic) for generic maps (e.g. ModelStore)
			comparer = func(a, b any) int {
				mapA, okA := a.(map[string]any)
				mapB, okB := b.(map[string]any)
				if !okA || !okB {
					return btree.Compare(a, b)
				}

				keys := make([]string, 0, len(mapA)+len(mapB))
				seen := make(map[string]struct{})
				for k := range mapA {
					if _, exists := seen[k]; !exists {
						keys = append(keys, k)
						seen[k] = struct{}{}
					}
				}
				for k := range mapB {
					if _, exists := seen[k]; !exists {
						keys = append(keys, k)
						seen[k] = struct{}{}
					}
				}
				sort.Strings(keys)

				for _, k := range keys {
					valA, existsA := mapA[k]
					valB, existsB := mapB[k]

					if !existsA && !existsB {
						continue
					}
					if !existsA {
						return -1
					}
					if !existsB {
						return 1
					}

					res := btree.Compare(valA, valB)
					if res != 0 {
						return res
					}
				}
				return 0
			}
		}
	}

	store, err := database.OpenBtree[any, any](ctx, dbOpts, req.StoreName, trans, comparer)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to open store '%s': %v", req.StoreName, err), http.StatusInternalServerError)
		return
	}

	// Apply persisted schema coercion before writing the item.
	si := store.GetStoreInfo()
	finalKey, err := coerceRecordKey(req.Key, &si)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	finalValue, err := coerceRecordValue(req.Value, &si)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Detect sample key to cast req.Key
	var sampleKey any
	if isPrimitiveKey {
		if ok, _ := store.First(ctx); ok {
			sampleKey = store.GetCurrentKey().Key
		}
	}

	// Cast Key
	if sampleKey != nil {
		if f, ok := req.Key.(float64); ok {
			switch sampleKey.(type) {
			case int:
				finalKey = int(f)
			case int8:
				finalKey = int8(f)
			case int16:
				finalKey = int16(f)
			case int32:
				finalKey = int32(f)
			case int64:
				finalKey = int64(f)
			case uint:
				finalKey = uint(f)
			case uint8:
				finalKey = uint8(f)
			case uint16:
				finalKey = uint16(f)
			case uint32:
				finalKey = uint32(f)
			case uint64:
				finalKey = uint64(f)
			case float32:
				finalKey = float32(f)
			}
		}
		if s, ok := req.Key.(string); ok {
			switch sampleKey.(type) {
			case sop.UUID:
				if id, err := sop.ParseUUID(s); err == nil {
					finalKey = id
				}
			case uuid.UUID:
				if id, err := uuid.Parse(s); err == nil {
					finalKey = id
				}
			}
		}
	}

	// Validate Key Type if possible
	if sampleKey != nil {
		sT := fmt.Sprintf("%T", sampleKey)
		fT := fmt.Sprintf("%T", finalKey)
		if sT != fT {
			http.Error(w, fmt.Sprintf("Key type mismatch: expected %s, got %s", sT, fT), http.StatusBadRequest)
			return
		}
	}

	// Perform Operation
	var opErr error
	switch op {
	case "add":
		var added bool
		added, opErr = store.Add(ctx, finalKey, finalValue)
		if opErr == nil && !added {
			opErr = fmt.Errorf("key already exists or is invalid")
		}
	case "delete":
		var removed bool
		removed, opErr = store.Remove(ctx, finalKey)
		if opErr == nil && !removed {
			opErr = fmt.Errorf("key not found")
		}
	}

	if opErr != nil {
		http.Error(w, opErr.Error(), http.StatusInternalServerError)
		return
	}

	if err := trans.Commit(ctx); err != nil {
		http.Error(w, "Commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func coerceRecordKey(value any, info *sop.StoreInfo) (any, error) {
	return coerceRecordField(value, info)
}

func coerceRecordValue(value any, info *sop.StoreInfo) (any, error) {
	return coerceRecordField(value, info)
}

func coerceRecordField(value any, info *sop.StoreInfo) (any, error) {
	if value == nil || info == nil || len(info.Schema) == 0 {
		return value, nil
	}

	if m, ok := value.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, v := range m {
			targetType := strings.ToLower(strings.TrimSpace(info.Schema[k]))
			if targetType == "" {
				out[k] = v
				continue
			}
			coerced, err := coerceScalarValue(v, targetType)
			if err != nil {
				return nil, fmt.Errorf("field %q must be %s: %w", k, targetType, err)
			}
			out[k] = coerced
		}
		return out, nil
	}

	return value, nil
}

func coerceScalarValue(value any, targetType string) (any, error) {
	switch targetType {
	case "string":
		switch v := value.(type) {
		case string:
			return v, nil
		case nil:
			return "", nil
		default:
			return fmt.Sprint(v), nil
		}
	case "number", "numeric", "integer", "int", "float", "float64":
		switch v := value.(type) {
		case float64:
			return v, nil
		case float32:
			return float64(v), nil
		case int:
			return float64(v), nil
		case int8:
			return float64(v), nil
		case int16:
			return float64(v), nil
		case int32:
			return float64(v), nil
		case int64:
			return float64(v), nil
		case uint:
			return float64(v), nil
		case uint8:
			return float64(v), nil
		case uint16:
			return float64(v), nil
		case uint32:
			return float64(v), nil
		case uint64:
			return float64(v), nil
		case bool:
			if v {
				return 1.0, nil
			}
			return 0.0, nil
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f, nil
			}
			return nil, fmt.Errorf("invalid number value %q", v)
		default:
			return nil, fmt.Errorf("invalid number value %T", value)
		}
	case "boolean", "bool":
		switch v := value.(type) {
		case bool:
			return v, nil
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				return b, nil
			}
			return nil, fmt.Errorf("invalid boolean value %q", v)
		case float64:
			return v != 0, nil
		case float32:
			return v != 0, nil
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return fmt.Sprint(v) != "0", nil
		default:
			return nil, fmt.Errorf("invalid boolean value %T", value)
		}
	case "datetime", "date", "timestamp":
		switch v := value.(type) {
		case time.Time:
			return v, nil
		case string:
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t, nil
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t, nil
			}
			if t, err := time.Parse("2006-01-02", v); err == nil {
				return t, nil
			}
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				return time.Unix(0, n), nil
			}
			return nil, fmt.Errorf("invalid date/datetime value %q", v)
		default:
			return nil, fmt.Errorf("invalid date/datetime value %T", value)
		}
	}
	return value, nil
}

func handleValidateAdminToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AdminToken    string `json:"adminToken"`
		AdminUsername string `json:"adminUsername"`
		AdminPassword string `json:"adminPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if authToken := extractBearerToken(r.Header.Get("Authorization")); authToken != "" && req.AdminToken == "" {
		req.AdminToken = authToken
	}
	if username, password := config.authFromAuthorization(r.Header.Get("Authorization")); username != "" || password != "" {
		req.AdminUsername = username
		req.AdminPassword = password
	}

	if !config.IsAdminOverride(r.Context(), req.AdminUsername, req.AdminPassword, req.AdminToken) && !isAdminRoleInContext(r.Context()) {
		http.Error(w, "Only admins are allowed", http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
