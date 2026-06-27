package sopbackend

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
)

var ErrAgentNotInitialized = errors.New("agent not initialized")

// GetConfig returns a copy of the current configuration
func GetConfig() Config {
	return config
}

// GetLoadedAgents returns the currently initialized backend agents.
func GetLoadedAgents() map[string]ai.Agent[map[string]any] {
	return loadedAgents
}

// SetLoadedAgents replaces the currently initialized backend agents.
func SetLoadedAgents(agents map[string]ai.Agent[map[string]any]) {
	loadedAgents = agents
}

// AskSharedAgent routes a query through the shared backend agent runtime.
// It prefers the initialized backend agents so daemon and httpserver use the same
// handler/formatting path for slash-style requests such as /list_tools.
func AskSharedAgent(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		cfg = ai.NewConfigMap()
	}
	ctx = InjectSharedRuntimeContext(ctx, nil, nil)

	for _, agentID := range []string{"copilot", "sql_core"} {
		if agentSvc, ok := loadedAgents[agentID]; ok && agentSvc != nil {
			return agentSvc.Ask(ctx, query, cfg)
		}
	}
	for _, agentSvc := range loadedAgents {
		if agentSvc != nil {
			return agentSvc.Ask(ctx, query, cfg)
		}
	}
	return "", ErrAgentNotInitialized
}

// SetConfigFile sets the config file path
func SetConfigFile(path string) {
	config.ConfigFile = path
}

// SetPort sets the server port
func SetPort(port int) {
	config.Port = port
}

// SetProductionMode sets the production mode flag
func SetProductionMode(production bool) {
	config.ProductionMode = production
}

// SetDisableAuth disables authentication (for local daemon mode)
func SetDisableAuth(disable bool) {
	config.DisableAuth = disable
}

// GetModelCatalog returns a copy of the current model catalog
func GetModelCatalog() ModelCatalog {
	return modelCatalog
}

// RuntimeOptions customizes backend initialization for a specific app/runtime.
type RuntimeOptions struct {
	EnableLocalAutomation bool
	AutomationPolicy      *sharedagent.AutomationPolicy
}

// Initialize performs shared backend initialization for a specific runtime.
func Initialize(ctx context.Context, opts RuntimeOptions) error {
	if opts.EnableLocalAutomation {
		sharedagent.SetLocalDesktopAutomationEnabled(true)
		if opts.AutomationPolicy != nil {
			sharedagent.SetAutomationPolicy(*opts.AutomationPolicy)
		} else {
			sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())
		}
	} else {
		sharedagent.SetLocalDesktopAutomationEnabled(false)
		sharedagent.SetAutomationPolicy(sharedagent.DefaultAutomationPolicy())
	}

	if err := initAgents(ctx); err != nil {
		return err
	}
	return nil
}

// BootstrapRuntime loads the shared config, applies runtime defaults, and initializes the backend once.
func BootstrapRuntime(ctx context.Context, configPath string, opts RuntimeOptions, port int, productionMode *bool) (string, error) {
	targetConfigPath := strings.TrimSpace(configPath)
	if targetConfigPath == "" {
		targetConfigPath = FindExistingConfigFile()
	}
	if targetConfigPath == "" {
		if cwd, err := os.Getwd(); err == nil {
			targetConfigPath = filepath.Join(cwd, "config.json")
		} else {
			targetConfigPath = "config.json"
		}
	}

	if _, err := os.Stat(targetConfigPath); err == nil {
		if err := LoadConfig(targetConfigPath); err != nil {
			return "", err
		}
		SetConfigFile(targetConfigPath)
		if _, err := LoadModelCatalog(targetConfigPath); err != nil {
			return "", err
		}
	} else if _, err := LoadModelCatalog(""); err != nil {
		return "", err
	}

	if port != 0 {
		SetPort(port)
	}
	if productionMode != nil {
		SetProductionMode(*productionMode)
	} else {
		SetProductionMode(true)
	}

	if err := Initialize(ctx, opts); err != nil {
		return "", err
	}
	return targetConfigPath, nil
}

// InitAgents initializes the agents with the current configuration.
func InitAgents(ctx context.Context) error {
	return Initialize(ctx, RuntimeOptions{})
}

// RegisterAllHandlers registers all backend API handlers on the provided mux
func RegisterAllHandlers(mux *http.ServeMux) {
	// Runtime & Database Endpoints
	mux.HandleFunc("/api/runtime/refresh", WithAuth(handleRuntimeRefresh))
	mux.HandleFunc("/api/databases", WithAuth(handleDatabases))
	mux.HandleFunc("/api/databases/update", WithAuth(handleUpdateDatabase))
	mux.HandleFunc("/api/stores", WithAuth(handleListStores))
	mux.HandleFunc("/api/spaces", WithAuth(handleListSpaces))
	mux.HandleFunc("/api/db/options", WithAuth(handleGetDBOptions))
	mux.HandleFunc("/api/store/info", WithAuth(handleGetStoreInfo))
	mux.HandleFunc("/api/store/update", WithAuth(handleUpdateStoreInfo))
	mux.HandleFunc("/api/store/items", WithAuth(handleListItems))
	mux.HandleFunc("/api/store/item/update", WithAuth(handleUpdateItem))
	mux.HandleFunc("/api/store/item/add", WithAuth(handleAddItem))
	mux.HandleFunc("/api/store/item/delete", WithAuth(handleDeleteItem))
	mux.HandleFunc("/api/store/items/delete", WithAuth(handleDeleteItems))
	mux.HandleFunc("/api/store/add", WithAuth(handleAddStore))
	mux.HandleFunc("/api/store/delete", WithAuth(handleDeleteStore))
	mux.HandleFunc("/api/admin/validate", WithAuth(handleValidateAdminToken))

	// AI Endpoints
	mux.HandleFunc("/api/ai/chat", WithAuth(handleAIChat))
	mux.HandleFunc("/api/ai/test-connection", WithAuth(handleTestLLMConnection))
	mux.HandleFunc("/api/ai/test-embedder-connection", WithAuth(handleTestEmbedderConnection))
	mux.HandleFunc("/api/ai/summarize", WithAuth(handleAISummarize))
	mux.HandleFunc("/api/tool/execute", WithAuth(handleToolExecute))
	mux.HandleFunc("/api/ai/session/close", WithAuth(handleCloseSession))
	mux.HandleFunc("/api/ai/feedback", WithAuth(handleAIFeedback))
	mux.HandleFunc("/api/scripts/execute", WithAuth(handleExecuteScript))

	// Knowledge Base Endpoints
	mux.HandleFunc("/api/spaces/categories", WithAuth(handleListSpaceCategories))
	mux.HandleFunc("/api/spaces/category/add", WithAuth(handleAddSpaceCategory))
	mux.HandleFunc("/api/spaces/category/delete", WithAuth(handleDeleteSpaceCategory))
	mux.HandleFunc("/api/spaces/items", WithAuth(handleListSpaceItems))
	mux.HandleFunc("/api/spaces/item/add", WithAuth(handleAddSpaceItem))
	mux.HandleFunc("/api/spaces/item/update", WithAuth(handleUpdateSpaceItem))
	mux.HandleFunc("/api/spaces/item/delete", WithAuth(handleDeleteSpaceItem))
	mux.HandleFunc("/api/spaces/create", WithAuth(handleCreateSpace))
	mux.HandleFunc("/api/spaces/preload", WithAuth(handlePreloadSpace))
	mux.HandleFunc("/api/spaces/ingest", WithAuth(handleIngestSpace))
	mux.HandleFunc("/api/spaces/ingest/import", WithAuth(handleIngestImportSpace))
	mux.HandleFunc("/api/spaces/vectorize", WithAuth(handleVectorizeSpace))
	mux.HandleFunc("/api/spaces/delete", WithAuth(handleDeleteSpace))
	mux.HandleFunc("/api/spaces/export", WithAuth(handleExportSpace))
	mux.HandleFunc("/api/spaces/import", WithAuth(handleImportSpace))
	mux.HandleFunc("/api/spaces/config/get", WithAuth(handleGetSpaceConfig))
	mux.HandleFunc("/api/spaces/config", WithAuth(handleSaveSpaceConfig))
	mux.HandleFunc("/api/tasks/status", WithAuth(handleGetTaskStatus))

	// Configuration Endpoints
	mux.HandleFunc("/api/config/save", WithAuth(handleSaveConfig))
	mux.HandleFunc("/api/db/init", WithAuth(handleInitDatabase))
	mux.HandleFunc("/api/config/validate-path", WithAuth(handleValidatePath))
	mux.HandleFunc("/api/system/uninstall", WithAuth(handleUninstallSystem))
	mux.HandleFunc("/api/config/environments", WithAuth(handleListEnvironments))
	mux.HandleFunc("/api/config/environments/create", WithAuth(handleCreateEnvironment))
	mux.HandleFunc("/api/config/environments/switch", WithAuth(handleSwitchEnvironment))
	mux.HandleFunc("/api/config/environments/delete", WithAuth(handleDeleteEnvironment))

	// Auth Endpoints
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/auth/refresh", handleRefresh)
}

// StartSessionCleanup starts a background goroutine to clean up stale sessions
func StartSessionCleanup(interval, ttl time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			evicted := activeSessions.RemoveStale(ttl)
			if evicted > 0 {
				// Log cleanup if needed
			}
		}
	}()
}
