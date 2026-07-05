package sopbackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	log "log/slog"
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

// SetConfig replaces the current configuration.
func SetConfig(cfg Config) {
	config = cfg
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

// InitializeRuntime loads the shared config, applies runtime defaults, and initializes the backend once.
func InitializeRuntime(ctx context.Context, configPath string, opts RuntimeOptions, port int, productionMode *bool) (string, error) {
	isProduction := true
	if productionMode != nil {
		isProduction = *productionMode
	}
	if _, err := validateRuntimeSecretConfiguration(isProduction); err != nil {
		return "", err
	}

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

// BootstrapRuntime loads the shared config, applies runtime defaults, and initializes the backend once.
func BootstrapRuntime(ctx context.Context, configPath string, opts RuntimeOptions, port int, productionMode *bool) (string, error) {
	return InitializeRuntime(ctx, configPath, opts, port, productionMode)
}

func validateRuntimeSecretConfiguration(isProduction bool) (bool, error) {
	var missing []string
	if configEncryptionKey() == nil {
		missing = append(missing, "SOP_ENCRYPTION_KEY")
	}
	if isProduction {
		if strings.TrimSpace(os.Getenv("SOP_SESSION_SECRET")) == "" && strings.TrimSpace(config.SessionSecret.Plaintext()) == "" {
			missing = append(missing, "SOP_SESSION_SECRET")
		}
	}
	if len(missing) == 0 {
		return true, nil
	}
	return false, fmt.Errorf("missing required runtime secrets: %s", strings.Join(missing, ", "))
}

// InitAgents initializes the agents with the current configuration.
func InitAgents(ctx context.Context) error {
	return Initialize(ctx, RuntimeOptions{})
}

// RegisterSwaggerHandlers registers a shared Swagger UI and OpenAPI document on the provided mux.
func RegisterSwaggerHandlers(mux *http.ServeMux, basePath string) {
	if mux == nil {
		return
	}
	basePath = strings.TrimRight(basePath, "/")
	if basePath == "" {
		basePath = ""
	}
	if req, err := http.NewRequest(http.MethodGet, basePath+"/swagger.json", nil); err == nil {
		if _, pattern := mux.Handler(req); pattern == basePath+"/swagger.json" {
			return
		}
	}

	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "SOP Backend API",
			"version":     "1.0.0",
			"description": "Shared API surface for the SOP Data Manager, daemon, and new site experience.",
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"AuthLoginRequest": map[string]any{
					"type":     "object",
					"required": []string{"username", "password"},
					"properties": map[string]any{
						"username": map[string]any{"type": "string"},
						"password": map[string]any{"type": "string"},
					},
				},
				"AuthUserResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"username":          map[string]any{"type": "string"},
						"role":              map[string]any{"type": "string"},
						"email":             map[string]any{"type": "string"},
						"subscription_plan": map[string]any{"type": "string"},
						"status":            map[string]any{"type": "string"},
					},
				},
				"MonitorabilityInfoResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status":         map[string]any{"type": "string"},
						"timestamp":      map[string]any{"type": "string"},
						"service":        map[string]any{"type": "string"},
						"version":        map[string]any{"type": "string"},
						"environment":    map[string]any{"type": "string"},
						"hostname":       map[string]any{"type": "string"},
						"uptime_seconds": map[string]any{"type": "integer"},
					},
				},
			},
		},
		"paths": map[string]any{
			"/api/auth/login": map[string]any{"post": map[string]any{
				"summary":     "Authenticate a user",
				"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/AuthLoginRequest"}}}},
				"responses":   map[string]any{"200": map[string]any{"description": "Authentication succeeded"}},
			}},
			"/api/auth/me": map[string]any{"get": map[string]any{
				"summary":   "Get the current authenticated user",
				"responses": map[string]any{"200": map[string]any{"description": "Current user", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/AuthUserResponse"}}}}},
			}},
			"/healthz": map[string]any{"get": map[string]any{"summary": "Liveness endpoint for the runtime instance"}},
			"/readyz":  map[string]any{"get": map[string]any{"summary": "Readiness endpoint for the runtime instance"}},
			"/info": map[string]any{"get": map[string]any{
				"summary":   "Instance metadata for the runtime instance",
				"responses": map[string]any{"200": map[string]any{"description": "Instance metadata", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/MonitorabilityInfoResponse"}}}}},
			}},
			"/metrics":       map[string]any{"get": map[string]any{"summary": "Prometheus-compatible metrics for the runtime instance"}},
			"/api/databases": map[string]any{"get": map[string]any{"summary": "List databases"}},
			"/api/stores":    map[string]any{"get": map[string]any{"summary": "List stores"}},
			"/api/spaces":    map[string]any{"get": map[string]any{"summary": "List spaces"}},
		},
	}

	mux.HandleFunc(basePath+"/swagger.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(spec)
	})
	mux.HandleFunc(basePath+"/swagger/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>SOP Swagger UI</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
    <style>
      :root { color-scheme: light dark; }
      body {
        margin: 0;
        background: #f5f8ff;
        color: #08111f;
      }
      @media (prefers-color-scheme: dark) {
        body {
          background: #07111f;
          color: #f7f9fc;
        }
      }
      #swagger-ui { padding: 1rem; }
    </style>
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.onload = () => {
        window.ui = SwaggerUIBundle({
          url: './swagger.json',
          dom_id: '#swagger-ui',
          deepLinking: true,
          presets: [SwaggerUIBundle.presets.apis],
        });
      };
    </script>
  </body>
</html>`))
	})
	mux.HandleFunc(basePath+"/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, basePath+"/swagger/", http.StatusFound)
	})
}

// RegisterAllHandlers registers all backend API handlers on the provided mux.
func RegisterAllHandlers(mux *http.ServeMux) {
	RegisterAllHandlersWithOptions(mux, true)
}

// RegisterAllHandlersWithoutRoot registers all backend API handlers without mounting the shared UI root redirect.
func RegisterAllHandlersWithoutRoot(mux *http.ServeMux) {
	RegisterAllHandlersWithOptions(mux, false)
}

// RegisterAllHandlersWithOptions registers all backend API handlers with optional root routing support.
func RegisterAllHandlersWithOptions(mux *http.ServeMux, includeRoot bool) {
	if includeRoot {
		RegisterUIRoutes(mux)
	} else {
		RegisterUIRoutesWithoutRoot(mux)
	}
	RegisterSwaggerHandlers(mux, "")

	mux.HandleFunc("/admin/users", WithAuth(handleAdminUsersPage))
	mux.HandleFunc("/api/admin/users", WithAuth(handleAdminUsersList))
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
	mux.HandleFunc("/api/source-store/digest", WithAuth(handleDigestSourceStore))
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
	mux.HandleFunc("/api/auth/users/role", WithAuth(handleUpdateUserRole))
}

// StartBackgroundService starts the shared background service loop for the backend.
// It currently handles session cleanup and can host additional periodic work over time.
func StartBackgroundService(interval, ttl time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			if activeSessions != nil {
				evicted := activeSessions.RemoveStale(ttl)
				if evicted > 0 {
					// Log cleanup if needed
				}
			}
			if currentTokenFacade() != nil {
				if store, ok := currentTokenFacade().(*SessionStore); ok && store != nil {
					if _, err := store.collectStaleSessions(context.Background(), 10*time.Minute, DefaultSessionGCBatchSize()); err != nil {
						log.Warn("background service: failed to collect stale sessions", "error", err)
					}
				}
			}
		}
	}()
}
