package sopbackend

import (
	"context"
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
	normalized := strings.ReplaceAll(strings.ReplaceAll(lower, "_", " "), "-", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	return normalized == "core playbook" || normalized == "shared memory" || strings.HasPrefix(lower, "memory_") || strings.HasSuffix(lower, "_ltm") || strings.HasSuffix(lower, "_stm")
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

			// STM and LTM stores are read-only for regular users (prevent accidents)
			// SystemAdmin can write/delete for maintenance
			if strings.HasSuffix(entCtx.AssetID, "_stm") || strings.HasSuffix(entCtx.AssetID, "_ltm") {
				if action == sop.ActionWrite || action == sop.ActionDelete {
					// Only SystemAdmin can write/delete memory stores
					auth := sop.GetAuthFromContext(ctx)
					for _, role := range auth.Roles {
						if strings.EqualFold(role, sop.RoleSystemAdmin) {
							return true
						}
					}
					return false
				}
				// Regular users can read their own STM/LTM
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

// Backend represents the reusable SOP backend that can be used by both httpserver and sop-daemon.
type Backend struct {
	Config         *Config
	ModelCatalog   *ModelCatalog
	LoadedAgents   map[string]ai.Agent[map[string]any]
	ActiveSessions *SessionManager
	shutdownCh     chan struct{}
	shutdownOnce   sync.Once

	// KB Quality Management
	KBEffectivenessTracker *memory.KBEffectivenessTracker
	KBQualityAPI           *memory.KBQualityAPI
	KBQualityJob           *memory.QualityCheckJob
}

// NewBackend creates a new backend instance.
func NewBackend(cfg *Config, catalog *ModelCatalog) *Backend {
	b := &Backend{
		Config:         cfg,
		ModelCatalog:   catalog,
		LoadedAgents:   make(map[string]ai.Agent[map[string]any]),
		ActiveSessions: NewSessionManager(100),
		shutdownCh:     make(chan struct{}),
	}

	// Initialize KB Quality Management
	b.initKBQuality()

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

// initKBQuality initializes the KB Quality Management system.
func (b *Backend) initKBQuality() {
	// Determine storage path for KB effectiveness data
	dataPath := b.getKBEffectivenessPath()

	// Initialize tracker with auto-save enabled
	tracker, err := memory.NewKBEffectivenessTracker(dataPath, true)
	if err != nil {
		log.Warn("Failed to initialize KB effectiveness tracker", "error", err, "path", dataPath)
		return
	}

	b.KBEffectivenessTracker = tracker

	// Initialize quality analyzer with default thresholds
	thresholds := memory.DefaultPruningThresholds()
	b.KBQualityAPI = memory.NewKBQualityAPI(tracker, thresholds)

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

	b.KBQualityJob = memory.NewQualityCheckJob(tracker, jobConfig)
	b.KBQualityAPI.SetQualityJob(b.KBQualityJob)

	// Start background job
	b.KBQualityJob.Start()

	log.Info("KB Quality Management initialized", "path", dataPath)
}

// getKBEffectivenessPath determines where to store KB effectiveness data.
func (b *Backend) getKBEffectivenessPath() string {
	// Prefer SystemDB path if available
	if b.Config.SystemDB != nil && b.Config.SystemDB.Path != "" {
		return filepath.Join(b.Config.SystemDB.Path, "kb_effectiveness.json")
	}

	// Fall back to first database path
	if len(b.Config.Databases) > 0 && b.Config.Databases[0].Path != "" {
		baseDir := filepath.Dir(b.Config.Databases[0].Path)
		return filepath.Join(baseDir, "kb_effectiveness.json")
	}

	// Last resort: temp directory
	return filepath.Join(os.TempDir(), "sop_kb_effectiveness.json")
}

// StopKBQuality gracefully stops the KB quality background job.
func (b *Backend) StopKBQuality() {
	if b.KBQualityJob != nil {
		b.KBQualityJob.Stop()
		log.Info("KB Quality Management stopped")
	}

	if b.KBEffectivenessTracker != nil {
		if err := b.KBEffectivenessTracker.Close(); err != nil {
			log.Warn("Failed to close KB effectiveness tracker", "error", err)
		}
	}
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

	// Stores API endpoints (disabled by default for v1, hidden via feature flag)
	if sharedagent.StoresFeatureEnabled() {
		mux.HandleFunc("/api/stores", WithAuth(handleListStores))
		mux.HandleFunc("/api/store/info", WithAuth(handleGetStoreInfo))
		mux.HandleFunc("/api/store/update", WithAuth(handleUpdateStoreInfo))
		mux.HandleFunc("/api/store/items", WithAuth(handleListItems))
		mux.HandleFunc("/api/store/item/update", WithAuth(handleUpdateItem))
		mux.HandleFunc("/api/store/item/add", WithAuth(handleAddItem))
		mux.HandleFunc("/api/store/item/delete", WithAuth(handleDeleteItem))
		mux.HandleFunc("/api/store/items/delete", WithAuth(handleDeleteItems))
		mux.HandleFunc("/api/store/add", WithAuth(handleAddStore))
		mux.HandleFunc("/api/store/delete", WithAuth(handleDeleteStore))
	}

	mux.HandleFunc("/api/spaces", WithAuth(handleListSpaces))
	mux.HandleFunc("/api/db/options", WithAuth(handleGetDBOptions))
	mux.HandleFunc("/api/admin/validate", WithAuth(handleValidateAdminToken))

	// AI Endpoints
	//
	// NOTE: /api/ai/chat vs /api/ask (daemon)
	// This endpoint (/api/ai/chat) is for HQ mode with SESSION auth.
	// The daemon provides /api/ask with JWT auth for localhost (see daemon/main.go).
	// Both share underlying agent logic but maintain separate endpoints for:
	// - Different auth models (session vs JWT)
	// - Different payload structures (rich HQ vs simpler daemon)
	// - Independent evolution (HQ features vs local-first features)
	mux.HandleFunc("/api/ai/chat", WithAuth(handleAIChat))
	mux.HandleFunc("/api/continue/chat", WithAuth(handleContinueChat))
	mux.HandleFunc("/api/sop/chat", WithAuth(handleContinueChat))
	mux.HandleFunc("/api/continue/tools/", WithAuth(handleContinueTool))
	mux.HandleFunc("/api/sop/tools/", WithAuth(handleContinueTool))
	mux.HandleFunc("/api/continue/sessions/", WithAuth(handleContinueSessionState))
	mux.HandleFunc("/api/sop/sessions/", WithAuth(handleContinueSessionState))
	mux.HandleFunc("/api/continue/worker/register", WithAuth(handleContinueWorkerRegister))
	mux.HandleFunc("/api/sop/worker/register", WithAuth(handleContinueWorkerRegister))
	mux.HandleFunc("/api/continue/workers/", WithAuth(handleContinueWorkerState))
	mux.HandleFunc("/api/sop/workers/", WithAuth(handleContinueWorkerState))
	mux.HandleFunc("/api/ai/test-connection", WithAuth(handleTestLLMConnection))
	mux.HandleFunc("/api/ai/test-embedder-connection", WithAuth(handleTestEmbedderConnection))
	mux.HandleFunc("/api/ai/summarize", WithAuth(handleAISummarize))
	mux.HandleFunc("/api/tool/execute", WithAuth(handleToolExecute))
	mux.HandleFunc("/api/ai/session/close", WithAuth(handleCloseSession))
	mux.HandleFunc("/api/ai/feedback", WithAuth(handleAIFeedback))
	mux.HandleFunc("/api/scripts/execute", WithAuth(handleExecuteScript))

	// KB Refinement Endpoints
	mux.HandleFunc("/api/ai/chat/revalidate", WithAuth(HandleKBRevalidate))
	mux.HandleFunc("/api/ai/kb/alternatives", WithAuth(HandleKBAlternatives))

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
	mux.HandleFunc("/api/space/pull", WithAuth(HandleDaemonPullSpace)) // Daemon pulls Space from HQ
	mux.HandleFunc("/api/federation/model-catalog", WithAuth(HandleGetModelCatalogFederation))
	mux.HandleFunc("/api/federation/model-catalog/refresh", WithAuth(HandleRefreshModelCatalogFederation))
	mux.HandleFunc("/api/federation/model-catalog/share", WithAuth(HandleShareModelCatalogFederation))

	// LTM Category Auto-Share Endpoints (SA only)
	mux.HandleFunc("/api/ltm/category/auto-share", WithAuth(HandleSetCategoryAutoShare))
	mux.HandleFunc("/api/ltm/category/auto-share/list", WithAuth(HandleListCategoryAutoShare))
	mux.HandleFunc("/api/ltm/category/auto-share/bulk", WithAuth(HandleBulkSetCategoryAutoShare))

	// Workflow Management Endpoints (SA only)
	mux.HandleFunc("/api/workflow/save", WithAuth(handleSaveWorkflow))
	mux.HandleFunc("/api/workflow/publish", WithAuth(handlePublishWorkflow))
	mux.HandleFunc("/api/workflow/list", WithAuth(handleListWorkflows))
	mux.HandleFunc("/api/workflow/get", WithAuth(handleGetWorkflow))

	// Workflow Undo Endpoints
	mux.HandleFunc("/api/workflow/undo/recent", WithAuth(handleUndoRecent))
	mux.HandleFunc("/api/workflow/undo/restore/", WithAuth(handleUndoRestore)) // Trailing slash for path param
	mux.HandleFunc("/api/workflow/undo/search", WithAuth(handleUndoSearch))
	mux.HandleFunc("/api/workflow/undo/stats", WithAuth(handleUndoStats))

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

	// KB Quality Management Endpoints
	if b.KBQualityAPI != nil {
		b.KBQualityAPI.RegisterRoutes(mux)
	}
}

// StartBackgroundService starts the shared background service loop for the backend.
// The main loop acts as a lightweight dispatcher and hands off heavier work to dedicated worker goroutines.
func (b *Backend) StartBackgroundService(interval, ttl time.Duration) {
	b.StartBackgroundServiceWithContext(context.Background(), interval, ttl)
}

// StartBackgroundServiceWithContext starts the shared background service loop and stops it when the provided context is canceled.
func (b *Backend) StartBackgroundServiceWithContext(ctx context.Context, interval, ttl time.Duration) {
	if b == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				b.StopBackgroundService()
				return
			case <-ticker.C:
			}
			if b.ActiveSessions != nil {
				evicted := b.ActiveSessions.RemoveStale(ttl)
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
		}
	}()
}

func (b *Backend) StopBackgroundService() {
	if b == nil {
		return
	}
	b.shutdownOnce.Do(func() {
		close(b.shutdownCh)
		b.StopKBQuality()
	})
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
