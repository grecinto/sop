package sopbackend

import (
	"context"
	"net/http"
	"time"

	"github.com/sharedcode/sop/ai"
)

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

// RegisterHandlers registers all backend API handlers on the provided mux.
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

// StartSessionCleanup starts a background goroutine to clean up stale sessions.
func (b *Backend) StartSessionCleanup(interval, ttl time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			evicted := b.ActiveSessions.RemoveStale(ttl)
			if evicted > 0 {
				// Log cleanup if needed
			}
		}
	}()
}

// SyncState synchronizes the backend state with global package variables.
// This is needed because the backend logic uses package-level variables.
func (b *Backend) SyncState() {
	config = *b.Config
	modelCatalog = *b.ModelCatalog
	loadedAgents = b.LoadedAgents
	activeSessions = b.ActiveSessions
}
