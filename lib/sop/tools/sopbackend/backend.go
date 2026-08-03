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

	registerSharedAPIHandlers(mux)
	if b.KBQualityAPI != nil {
		b.KBQualityAPI.RegisterRoutes(mux)
	}
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
