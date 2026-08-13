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
	"sync"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/ai/memory"
)

var ErrAgentNotInitialized = errors.New("agent not initialized")

func daemonBootstrapConfigDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		return filepath.Join(v, "sop"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sop"), nil
}

func sanitizeDaemonBootstrapClientID(clientID string) string {
	clientKey := strings.ToLower(strings.TrimSpace(clientID))
	clientKey = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, clientKey)
	clientKey = strings.Trim(clientKey, "-")
	return clientKey
}

func HandleDaemonBootstrapSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configDir, err := daemonBootstrapConfigDir()
	if err != nil {
		log.Error("failed to resolve daemon bootstrap config directory", "error", err)
		http.Error(w, "Failed to get config directory", http.StatusInternalServerError)
		return
	}

	clientID := sanitizeDaemonBootstrapClientID(r.URL.Query().Get("client_id"))
	bootstrapFileName := "daemon.bootstrap"
	if clientID != "" {
		bootstrapFileName = fmt.Sprintf("daemon.bootstrap.%s", clientID)
	}
	bootstrapPath := filepath.Join(configDir, bootstrapFileName)

	secretBytes, err := os.ReadFile(bootstrapPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Bootstrap file not found. Daemon may not be running."})
			return
		}
		log.Error("failed to read daemon bootstrap file", "path", bootstrapPath, "error", err)
		http.Error(w, "Failed to read bootstrap file", http.StatusInternalServerError)
		return
	}

	secret := strings.TrimSpace(string(secretBytes))
	if secret == "" {
		log.Error("daemon bootstrap file is empty", "path", bootstrapPath)
		http.Error(w, "Bootstrap file is empty", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"bootstrap_secret": secret})
}

var (
	backgroundServiceHookMu sync.RWMutex
	backgroundServiceHooks  []func(context.Context)
)

// RegisterBackgroundServiceHook registers a callback that runs on each shared background service tick.
func RegisterBackgroundServiceHook(hook func(context.Context)) {
	if hook == nil {
		return
	}
	backgroundServiceHookMu.Lock()
	defer backgroundServiceHookMu.Unlock()
	backgroundServiceHooks = append(backgroundServiceHooks, hook)
}

func runBackgroundServiceHooks(ctx context.Context) {
	backgroundServiceHookMu.RLock()
	hooks := append([]func(context.Context){}, backgroundServiceHooks...)
	backgroundServiceHookMu.RUnlock()
	for _, hook := range hooks {
		hook(ctx)
	}
}

// CleanupStaleSessions removes stale tracked sessions from the shared backend runtime.
// It targets the in-memory runtime session manager used by the backend, while persistent
// stores such as the B-tree-backed session store are handled separately by their own cleanup logic.
func CleanupStaleSessions(ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	if activeSessions != nil {
		return activeSessions.RemoveStale(ttl)
	}
	return 0
}

// CleanupStalePersistentSessions removes stale sessions from the shared persistent session store.
// This is the HQ-style behavior for a B-tree-backed session store and mirrors the daemon's TTL-based cleanup semantics.
func CleanupStalePersistentSessions(ctx context.Context, ttl time.Duration) (int, error) {
	if ttl <= 0 {
		return 0, nil
	}
	if currentTokenFacade() == nil {
		return 0, nil
	}
	store, ok := currentTokenFacade().(*SessionStore)
	if !ok || store == nil {
		return 0, nil
	}
	return store.collectStaleSessions(ctx, 10*time.Minute, DefaultSessionGCBatchSize())
}

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
type parallelAskAgent interface {
	AskParallel(context.Context, string, *ai.ConfigMap) (string, error)
}

func sharedAskPayload(ctx context.Context, cfg *ai.ConfigMap) *ai.SessionPayload {
	if payload := ai.GetSessionPayload(ctx); payload != nil {
		return payload
	}
	if cfg == nil {
		return nil
	}
	value, ok := cfg.Get("payload")
	if !ok || value == nil {
		return nil
	}
	payload, _ := value.(*ai.SessionPayload)
	return payload
}

func deriveSharedAskSessionID(ctx context.Context, cfg *ai.ConfigMap) string {
	if payload := sharedAskPayload(ctx, cfg); payload != nil {
		if sessionID := strings.TrimSpace(payload.SessionID); sessionID != "" {
			return sessionID
		}
		if userID := strings.TrimSpace(payload.UserID); userID != "" {
			if clientID := strings.TrimSpace(payload.ClientID); clientID != "" {
				return fmt.Sprintf("user:%s:client:%s", userID, clientID)
			}
			return fmt.Sprintf("user:%s", userID)
		}
	}
	if auth := sop.GetAuthFromContext(ctx); strings.TrimSpace(auth.UserID) != "" {
		return fmt.Sprintf("user:%s", strings.TrimSpace(auth.UserID))
	}
	return "shared-anonymous"
}

func resolveSharedAskBlueprint() ai.Agent[map[string]any] {
	for _, agentID := range []string{"copilot", "sql_core"} {
		if agentSvc, ok := loadedAgents[agentID]; ok && agentSvc != nil {
			return agentSvc
		}
	}
	for _, agentSvc := range loadedAgents {
		if agentSvc != nil {
			return agentSvc
		}
	}
	return nil
}

func lockSharedAskSession(ctx context.Context, cfg *ai.ConfigMap, blueprint ai.Agent[map[string]any]) (context.Context, ai.Agent[map[string]any], *sync.Mutex, error) {
	if blueprint == nil {
		return ctx, nil, nil, ErrAgentNotInitialized
	}
	payload := sharedAskPayload(ctx, cfg)
	if payload != nil && ai.GetSessionPayload(ctx) == nil {
		ctx = context.WithValue(ctx, "session_payload", payload)
	}
	sessionID := deriveSharedAskSessionID(ctx, cfg)
	agentSvc, sessionMu := activeSessions.GetOrCreate(sessionID, func() ai.Agent[map[string]any] {
		if cloneable, ok := blueprint.(interface {
			Clone() ai.Agent[map[string]any]
		}); ok {
			return cloneable.Clone()
		}
		return blueprint
	})
	return ctx, agentSvc, sessionMu, nil
}

func AskSharedAgent(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		cfg = ai.NewConfigMap()
	}
	// Preserve existing streaming context if already injected (daemon mode)
	// Only inject defaults if context doesn't already have streaming support
	existingStreamer := ctx.Value(ai.CtxKeyEventStreamer)
	if existingStreamer == nil {
		ctx = InjectSharedRuntimeContext(ctx, nil, nil)
	}

	if config.DisableAuth {
		// Auth disabled for daemon mode - skip user-store lookup
	} else if auth := sop.GetAuthFromContext(ctx); auth.UserID != "" {
		username := normalizeUsername(auth.UserID)
		if username != "" && username != "root" && username != "ui-client" {
			if _, ok, err := config.findUserRecord(ctx, username); err == nil && !ok {
				return "", fmt.Errorf("user not found")
			} else if err != nil {
				return "", err
			}
		}
	}

	ctx, agentSvc, sessionMu, err := lockSharedAskSession(ctx, cfg, resolveSharedAskBlueprint())
	if err != nil {
		return "", err
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()

	if parallelAgent, ok := agentSvc.(parallelAskAgent); ok && len(sharedagent.DetectParallelSubqueries(query)) > 1 {
		return parallelAgent.AskParallel(ctx, query, cfg)
	}
	return agentSvc.Ask(ctx, query, cfg)
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
	EnableStoresFeature   bool // Enable Stores capability, tools, and API endpoints (enabled by default - provides DB management UI)
	HostKind              HostKind
	HostProfile           HostProfile
}

func normalizeHostKind(kind HostKind) HostKind {
	switch kind {
	case HostKindHQ, HostKindDaemon, HostKindCorporate:
		return kind
	default:
		return HostKindUnknown
	}
}

func normalizeAuthMode(mode AuthMode) AuthMode {
	switch mode {
	case AuthModeSession, AuthModeJWTBootstrap, AuthModeEnterpriseSSO:
		return mode
	default:
		return AuthModeUnknown
	}
}

func normalizeProvisioningMode(mode ProvisioningMode) ProvisioningMode {
	switch mode {
	case ProvisioningModeSimple, ProvisioningModeAdvanced:
		return mode
	default:
		return ProvisioningModeUnknown
	}
}

func normalizeProvisioningModes(modes []ProvisioningMode) []ProvisioningMode {
	if len(modes) == 0 {
		return nil
	}
	normalized := make([]ProvisioningMode, 0, len(modes))
	for _, mode := range modes {
		mode = normalizeProvisioningMode(mode)
		if mode == ProvisioningModeUnknown {
			continue
		}
		seen := false
		for _, existing := range normalized {
			if existing == mode {
				seen = true
				break
			}
		}
		if !seen {
			normalized = append(normalized, mode)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func DefaultHostProfile(kind HostKind) HostProfile {
	kind = normalizeHostKind(kind)
	switch kind {
	case HostKindHQ:
		return HostProfile{
			Kind:                    HostKindHQ,
			AuthMode:                AuthModeSession,
			ProvisioningModes:       []ProvisioningMode{ProvisioningModeSimple, ProvisioningModeAdvanced},
			DefaultProvisioningMode: ProvisioningModeSimple,
		}
	case HostKindDaemon:
		return HostProfile{
			Kind:                    HostKindDaemon,
			AuthMode:                AuthModeJWTBootstrap,
			ProvisioningModes:       []ProvisioningMode{ProvisioningModeSimple, ProvisioningModeAdvanced},
			DefaultProvisioningMode: ProvisioningModeSimple,
		}
	case HostKindCorporate:
		return HostProfile{
			Kind:                    HostKindCorporate,
			AuthMode:                AuthModeEnterpriseSSO,
			ProvisioningModes:       []ProvisioningMode{ProvisioningModeSimple, ProvisioningModeAdvanced},
			DefaultProvisioningMode: ProvisioningModeSimple,
		}
	default:
		return HostProfile{
			Kind:                    HostKindUnknown,
			AuthMode:                AuthModeUnknown,
			DefaultProvisioningMode: ProvisioningModeUnknown,
		}
	}
}

func resolveHostProfile(opts RuntimeOptions) HostProfile {
	profile := opts.HostProfile
	if profile.Kind == "" {
		profile = DefaultHostProfile(opts.HostKind)
	} else {
		profile.Kind = normalizeHostKind(profile.Kind)
		profile.AuthMode = normalizeAuthMode(profile.AuthMode)
		profile.ProvisioningModes = normalizeProvisioningModes(profile.ProvisioningModes)
		profile.DefaultProvisioningMode = normalizeProvisioningMode(profile.DefaultProvisioningMode)
		if profile.Kind == HostKindUnknown && opts.HostKind != "" {
			profile = DefaultHostProfile(opts.HostKind)
		}
		if profile.DefaultProvisioningMode == ProvisioningModeUnknown && len(profile.ProvisioningModes) > 0 {
			profile.DefaultProvisioningMode = profile.ProvisioningModes[0]
		}
	}
	if profile.Kind == HostKindUnknown && opts.HostKind != "" {
		profile = DefaultHostProfile(opts.HostKind)
	}
	return profile
}

// Initialize performs shared backend initialization for a specific runtime.
func Initialize(ctx context.Context, opts RuntimeOptions) error {
	config.HostProfile = resolveHostProfile(opts)
	config.HostKind = config.HostProfile.Kind

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

	// Stores feature flag (enabled by default - provides DB management UI for SOP developers)
	sharedagent.SetStoresFeatureEnabled(opts.EnableStoresFeature)

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

		if config.SystemDB != nil {
			if _, err := GetSystemDBOptions(ctx); err != nil {
				log.Warn("failed to repair configured system database on startup", "error", err)
			}
		}

		// Auto-provision users store if config exists but store is missing
		// This handles the case where .sop-data was deleted/recreated after initial setup
		if err := config.ensureProvisionedUsersStore(ctx); err != nil {
			log.Warn("failed to auto-provision users store on startup", "error", err)
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
			"description": "Shared API surface for the AI Studio, daemon, and new site experience.",
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

	// HQ Federation / Central Memory Endpoints
	mux.HandleFunc("/api/daemon/hq-sync", WithAuth(HandleHQSync))
	mux.HandleFunc("/api/daemon/hq-retrieve", WithAuth(HandleHQRetrieve))
	mux.HandleFunc("/api/daemon/pull-space", WithAuth(HandleDaemonPullSpace)) // Daemon pulls Space from HQ
	mux.HandleFunc("/api/daemon/acknowledge-space", WithAuth(handleAcknowledgeSpace))
	mux.HandleFunc("/api/federation/model-catalog", WithAuth(HandleGetModelCatalogFederation))
	mux.HandleFunc("/api/federation/model-catalog/refresh", WithAuth(HandleRefreshModelCatalogFederation))
	mux.HandleFunc("/api/federation/model-catalog/share", WithAuth(HandleShareModelCatalogFederation))

	// KB Quality Management Endpoints
	initKBQuality()
	if kbQualityAPI != nil {
		kbQualityAPI.RegisterRoutes(mux)
	}
}

var (
	backgroundShutdownCh      = make(chan struct{})
	backgroundShutdownOnce    sync.Once
	backgroundServiceHookOnce sync.Once
)

var (
	// KB Quality Management package-level variables
	kbEffectivenessTracker *memory.KBEffectivenessTracker
	kbQualityAPI           *memory.KBQualityAPI
	kbQualityJob           *memory.QualityCheckJob
	kbQualityInitOnce      sync.Once
)

// StartBackgroundService starts the shared background service loop for the backend.
// It currently handles session cleanup and delegates billing work to a separate worker goroutine.
func StartBackgroundService(interval, ttl time.Duration) {
	StartBackgroundServiceWithContext(context.Background(), interval, ttl)
}

// StartBackgroundServiceWithContext starts the shared background service loop and stops it when the provided context is canceled.
func StartBackgroundServiceWithContext(ctx context.Context, interval, ttl time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	backgroundServiceHookOnce.Do(func() {
		RegisterBackgroundServiceHook(func(ctx context.Context) {
			if err := memory.FlushExpiredShortTermMemoryStores(ctx); err != nil {
				log.Warn("background service: failed to flush STM episodes", "error", err)
			}
		})
	})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				StopBackgroundService()
				return
			case <-backgroundShutdownCh:
				return
			case <-ticker.C:
			}
			if activeSessions != nil {
				evicted := activeSessions.RemoveStale(ttl)
				if evicted > 0 {
					// Log cleanup if needed
				}
			}
			if currentTokenFacade() != nil {
				if store, ok := currentTokenFacade().(*SessionStore); ok && store != nil {
					if _, err := store.collectStaleSessions(ctx, 10*time.Minute, DefaultSessionGCBatchSize()); err != nil {
						log.Warn("background service: failed to collect stale sessions", "error", err)
					}
				}
			}
			runBackgroundServiceHooks(ctx)
		}
	}()
}

func StopBackgroundService() {
	backgroundShutdownOnce.Do(func() {
		close(backgroundShutdownCh)
		stopKBQuality()
	})
}

// initKBQuality initializes the KB Quality Management system for package-level usage.
func initKBQuality() {
	kbQualityInitOnce.Do(func() {
		// Determine storage path for KB effectiveness data
		dataPath := getKBEffectivenessPath()

		// Initialize tracker with auto-save enabled
		tracker, err := memory.NewKBEffectivenessTracker(dataPath, true)
		if err != nil {
			log.Warn("Failed to initialize KB effectiveness tracker", "error", err, "path", dataPath)
			return
		}

		kbEffectivenessTracker = tracker

		// Initialize quality analyzer with default thresholds
		thresholds := memory.DefaultPruningThresholds()
		kbQualityAPI = memory.NewKBQualityAPI(tracker, thresholds)

		// Initialize background quality check job (24h interval, no auto-prune)
		jobConfig := memory.DefaultQualityCheckConfig()
		jobConfig.Interval = 24 * time.Hour
		jobConfig.AutoPrune = false // Safety: require manual pruning
		jobConfig.NotifyOnCritical = true
		jobConfig.NotifyCallback = func(notification memory.QualityNotification) {
			// Log critical KB quality issues
			log.Warn("KB Quality Alert",
				"title", notification.Title,
				"message", notification.Message,
				"severity", notification.Severity,
				"kb_name", notification.KBName,
				"task_type", notification.TaskType)
		}

		kbQualityJob = memory.NewQualityCheckJob(tracker, jobConfig)
		kbQualityAPI.SetQualityJob(kbQualityJob)

		// Start background job
		kbQualityJob.Start()

		log.Info("KB Quality Management initialized", "path", dataPath)
	})
}

// getKBEffectivenessPath determines where to store KB effectiveness data.
func getKBEffectivenessPath() string {
	// Prefer SystemDB path if available
	if config.SystemDB != nil && config.SystemDB.Path != "" {
		return filepath.Join(config.SystemDB.Path, "kb_effectiveness.json")
	}

	// Fall back to first database path
	if len(config.Databases) > 0 && config.Databases[0].Path != "" {
		baseDir := filepath.Dir(config.Databases[0].Path)
		return filepath.Join(baseDir, "kb_effectiveness.json")
	}

	// Last resort: temp directory
	return filepath.Join(os.TempDir(), "sop_kb_effectiveness.json")
}

// stopKBQuality gracefully stops the KB quality background job.
func stopKBQuality() {
	if kbQualityJob != nil {
		kbQualityJob.Stop()
		log.Info("KB Quality Management stopped")
	}

	if kbEffectivenessTracker != nil {
		if err := kbEffectivenessTracker.Close(); err != nil {
			log.Warn("Failed to close KB effectiveness tracker", "error", err)
		}
	}
}

// NewDirectFlushingWriter creates a writer that flushes after each write for real-time streaming
func NewDirectFlushingWriter(w http.ResponseWriter) *DirectFlushingWriter {
	return &DirectFlushingWriter{w: w}
}
