package sopbackend

import (
	"context"
	log "log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	aidb "github.com/sharedcode/sop/ai/database"
)

const (
	defaultSessionCleanupInterval = 2 * time.Hour
	defaultSessionGCBatchSize     = 50
)

func DefaultSessionCleanupInterval() time.Duration {
	return defaultSessionCleanupInterval
}

func DefaultSessionGCBatchSize() int {
	return defaultSessionGCBatchSize
}

func isReadOnlyContext(id string) bool {
	lower := strings.ToLower(id)
	return lower == "sop" || strings.HasPrefix(lower, "memory_")
}

func registerBuiltinRBACBlueprints() {
	// Re-register the built-in RBAC blueprints so reused backend instances inherit
	// the same policy rules as a fresh sopbackend startup.
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

				if evalState != nil && evalState.VectorizedSpaces != nil {
					if allowed, exists := evalState.VectorizedSpaces[entCtx.AssetID]; exists {
						return allowed
					}
					return false
				}

				if evalState != nil && evalState.ConfigCache != nil {
					if allowed, exists := evalState.ConfigCache[entCtx.AssetID]; exists {
						return allowed
					}
				}

				if evalState != nil && evalState.Transaction != nil {
					dbOpts, err := GetDBOptions(ctx, entCtx.Database)
					if err == nil {
						db := aidb.NewDatabase(dbOpts)
						kb, errKB := db.OpenKnowledgeBase(ctx, entCtx.AssetID, evalState.Transaction, nil, nil, false)
						if errKB == nil && kb != nil {
							cfg, cfgErr := kb.GetConfig(ctx)
							if cfgErr == nil && cfg != nil {
								allowed := cfg.LastVectorized > 0
								if evalState.ConfigCache != nil {
									evalState.ConfigCache[entCtx.AssetID] = allowed
								}
								return allowed
							}
						}
					}
					return false
				}

				// Fallback slow path (if evaluated outside of handleListSpaces)
				dbOpts, err := GetDBOptions(ctx, entCtx.Database)
				if err == nil {
					db := aidb.NewDatabase(dbOpts)
					tx, _ := db.BeginTransaction(ctx, sop.ForReading)
					if tx != nil {
						kb, errKB := db.OpenKnowledgeBase(ctx, entCtx.AssetID, tx, nil, nil, false)
						if errKB == nil && kb != nil {
							cfg, cfgErr := kb.GetConfig(ctx)
							if cfgErr == nil && cfg != nil {
								tx.Rollback(ctx)
								return cfg.LastVectorized > 0
							}
						}
						tx.Rollback(ctx)
					}
				}

				return true
			}

			if isReadOnlyContext(entCtx.AssetID) && entCtx.IsSystemDB && (action == sop.ActionWrite || action == sop.ActionDelete) {
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

// Backend represents the reusable SOP backend that can be used by both httpserver and sop-daemon.
type Backend struct {
	Config         *Config
	ModelCatalog   *ModelCatalog
	LoadedAgents   map[string]ai.Agent[map[string]any]
	ActiveSessions *SessionManager
}

// NewBackend creates a new backend instance.
func NewBackend(cfg *Config, catalog *ModelCatalog) *Backend {
	b := &Backend{
		Config:         cfg,
		ModelCatalog:   catalog,
		LoadedAgents:   make(map[string]ai.Agent[map[string]any]),
		ActiveSessions: NewSessionManager(100),
	}
	return b
}

// Initialize sets up the backend state (agents, RBAC, sessions).
func (b *Backend) Initialize(ctx context.Context) error {
	registerBuiltinRBACBlueprints()

	// Sync global state
	config = *b.Config
	modelCatalog = *b.ModelCatalog
	loadedAgents = b.LoadedAgents
	activeSessions = b.ActiveSessions

	// Initialize agents if databases are configured
	if len(b.Config.Databases) > 0 || b.Config.SystemDB != nil {
		if err := initAgents(ctx); err != nil {
			return err
		}
		// Sync back
		b.LoadedAgents = loadedAgents
	}

	return nil
}

// RegisterHandlers registers the shared runtime API handlers for the platform on the provided mux.
func (b *Backend) RegisterHandlers(mux *http.ServeMux) {
	// Sync global state before registering
	config = *b.Config
	modelCatalog = *b.ModelCatalog
	loadedAgents = b.LoadedAgents
	activeSessions = b.ActiveSessions

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

	// Space Federation Endpoints
	mux.HandleFunc("/api/space/auto-share", WithAuth(HandleSetSpaceAutoShare))
	mux.HandleFunc("/api/space/auto-shared", WithAuth(HandleListAutoSharedSpaces))
	mux.HandleFunc("/api/space/push", WithAuth(HandleHQPushSpace))
	mux.HandleFunc("/api/space/reshare", WithAuth(HandleHQReshareSpace))

	// LTM Category Auto-Share Endpoints (SA only)
	mux.HandleFunc("/api/ltm/category/auto-share", WithAuth(HandleSetCategoryAutoShare))
	mux.HandleFunc("/api/ltm/category/auto-share/list", WithAuth(HandleListCategoryAutoShare))
	mux.HandleFunc("/api/ltm/category/auto-share/bulk", WithAuth(HandleBulkSetCategoryAutoShare))

	// Daemon Sync Endpoints
	mux.HandleFunc("/api/daemon/hq-sync", HandleHQSync)

	// Configuration Endpoints
	mux.HandleFunc("/api/config/save", WithAuth(handleSaveConfig))
	mux.HandleFunc("/api/db/init", WithAuth(handleInitDatabase))
	mux.HandleFunc("/api/config/validate-path", WithAuth(handleValidatePath))
	mux.HandleFunc("/api/system/uninstall", WithAuth(handleUninstallSystem))
	mux.HandleFunc("/api/config/environments", WithAuth(handleListEnvironments))
	mux.HandleFunc("/api/config/environments/create", WithAuth(handleCreateEnvironment))
	mux.HandleFunc("/api/config/environments/switch", WithAuth(handleSwitchEnvironment))
	mux.HandleFunc("/api/config/environments/delete", WithAuth(handleDeleteEnvironment))

	// Admin UI / Auth Endpoints
	mux.HandleFunc("/admin/users", WithAuth(handleAdminUsersPage))
	mux.HandleFunc("/api/admin/users", WithAuth(handleAdminUsersList))
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/auth/refresh", handleRefresh)
	mux.HandleFunc("/api/auth/users/role", WithAuth(handleUpdateUserRole))
}

// StartBackgroundService starts the shared background service loop for the backend.
// It currently handles session cleanup and can host additional periodic work over time.
func (b *Backend) StartBackgroundService(interval, ttl time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			if b.ActiveSessions != nil {
				evicted := b.ActiveSessions.RemoveStale(ttl)
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

// SyncState synchronizes the backend state with global package variables.
// This is needed because the backend logic uses package-level variables.
func (b *Backend) SyncState() {
	registerBuiltinRBACBlueprints()
	config = *b.Config
	modelCatalog = *b.ModelCatalog
	loadedAgents = b.LoadedAgents
	activeSessions = b.ActiveSessions
}
