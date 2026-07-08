package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	log "log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
)

var (
	cachedRootUser   *UserRecord
	cachedRootUserMu sync.RWMutex
	hqRateLimiter    *RateLimiter
	hqRateLimiterMu  sync.Mutex
)

// hqSyncBatch represents a sync payload from a local daemon to HQ.
type hqSyncBatch struct {
	Kind      string `json:"kind"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	DaemonID  string `json:"daemon_id"`
	DaemonURL string `json:"daemon_url"` // NEW: Daemon's callback URL for HQ→daemon communication
	Timestamp string `json:"timestamp"`

	SharedMeta struct {
		Enabled    bool   `json:"enabled"`
		Scope      string `json:"scope"`
		Mode       string `json:"mode"`
		LastSyncAt string `json:"last_sync_at"`
		Artifacts  []struct {
			Text     string         `json:"text"`
			Category string         `json:"category"`
			Metadata map[string]any `json:"metadata,omitempty"`
		} `json:"artifacts,omitempty"`
	} `json:"shared_memory,omitempty"`
}

const (
	// rootSAUsername is the service account username for central/shared memory storage.
	rootSAUsername = "ltm_root"
	// sharedMemoryKBName is the knowledge base name for central shared memory.
	sharedMemoryKBName = "shared_memory"
)

// getHQRateLimiter returns the global HQ rate limiter instance.
func getHQRateLimiter() *RateLimiter {
	hqRateLimiterMu.Lock()
	defer hqRateLimiterMu.Unlock()

	if hqRateLimiter == nil {
		hqRateLimiter = NewRateLimiter()
		log.Info("HQ rate limiter initialized",
			"per_daemon_rate", hqRateLimiter.perDaemonRate,
			"global_rate", hqRateLimiter.globalRate)
	}
	return hqRateLimiter
}

// HandleHQSync receives shared memory sync batches from local daemons and stores
// them into the central/root SA LTM for fleet-wide retrieval.
//
// Design:
// - V1 uses the root service-account LTM as the backing store for central memory
// - Only authenticated daemon requests with valid bearer tokens are accepted
// - Artifacts are stored in the "shared_memory" knowledge base under the ltm_root user
// - The feature is additive and degrades gracefully if the root SA or KB is unavailable
func HandleHQSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and parse the sync batch
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		log.Warn("HQ sync: failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var batch hqSyncBatch
	if err := json.Unmarshal(body, &batch); err != nil {
		log.Warn("HQ sync: invalid JSON payload", "error", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Validate the batch
	if strings.TrimSpace(batch.Kind) == "" {
		log.Warn("HQ sync: missing batch kind")
		http.Error(w, "Batch kind is required", http.StatusBadRequest)
		return
	}

	daemonID := strings.TrimSpace(batch.DaemonID)
	if daemonID == "" {
		daemonID = "unknown-daemon"
	}

	// Check rate limits
	rateLimiter := getHQRateLimiter()
	allowed, retryAfter, reason := rateLimiter.Allow(daemonID)
	if !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		w.Header().Set("X-RateLimit-Reason", reason)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)

		log.Warn("HQ sync: rate limit exceeded",
			"daemon_id", daemonID,
			"reason", reason,
			"retry_after", retryAfter)

		json.NewEncoder(w).Encode(map[string]any{
			"error":               "rate limit exceeded",
			"reason":              reason,
			"retry_after_seconds": int(retryAfter.Seconds()),
			"daemon_id":           daemonID,
		})
		return
	}

	userID := strings.TrimSpace(batch.UserID)
	if userID == "" {
		userID = "daemon"
	}

	// Register daemon in the fleet registry
	if batch.DaemonURL != "" {
		registry := GetDaemonRegistry()
		registry.Register(daemonID, batch.DaemonURL, userID)
		log.Debug("HQ sync: daemon registered",
			"daemon_id", daemonID,
			"daemon_url", batch.DaemonURL,
		)
	}

	log.Info("HQ sync: received batch",
		"kind", batch.Kind,
		"daemon_id", daemonID,
		"user_id", userID,
		"session_id", batch.SessionID,
		"timestamp", batch.Timestamp,
		"shared_memory_enabled", batch.SharedMeta.Enabled,
	)

	// Process shared memory artifacts if present
	if batch.SharedMeta.Enabled && len(batch.SharedMeta.Artifacts) > 0 {
		if err := storeSharedMemoryArtifacts(r.Context(), batch); err != nil {
			log.Error("HQ sync: failed to store shared memory artifacts",
				"error", err,
				"daemon_id", daemonID,
				"artifact_count", len(batch.SharedMeta.Artifacts),
			)
			// Don't fail the entire sync on storage errors - log and continue
		} else {
			log.Info("HQ sync: stored shared memory artifacts",
				"daemon_id", daemonID,
				"artifact_count", len(batch.SharedMeta.Artifacts),
			)
		}
	}

	// Get list of auto-shared Spaces for daemon to pull
	autoSharedSpaces := listAutoSharedSpacesForDaemon(r.Context())

	// Return success response with auto-shared Spaces list
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":             "accepted",
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
		"daemon_id":          daemonID,
		"auto_shared_spaces": autoSharedSpaces,
	})
}

// storeSharedMemoryArtifacts stores the artifacts from a sync batch into the root SA's LTM.
func storeSharedMemoryArtifacts(ctx context.Context, batch hqSyncBatch) error {
	if len(batch.SharedMeta.Artifacts) == 0 {
		return nil
	}

	// Get or cache the root SA user for shared memory operations
	rootUser, err := getRootUserForSharedMemory(ctx)
	if err != nil {
		return fmt.Errorf("get root SA user: %w", err)
	}

	// Create auth context as the root SA user
	authCtx := sop.AuthContext{
		UserID:      rootUser.Username,
		Roles:       []string{rootUser.Role},
		Permissions: map[string]bool{},
	}
	ctx = sop.ContextWithAuth(ctx, authCtx)

	// Get system database options
	sysOpts, err := GetSystemDBOptions(ctx)
	if err != nil {
		return fmt.Errorf("get system database options: %w", err)
	}

	db := aidb.NewDatabase(sysOpts)

	// Get configured embedder for vectorization
	embedder := GetConfiguredEmbedder(nil)

	// Store each artifact
	for i, artifact := range batch.SharedMeta.Artifacts {
		text := strings.TrimSpace(artifact.Text)
		if text == "" {
			log.Warn("HQ sync: skipping empty artifact", "index", i, "daemon_id", batch.DaemonID)
			continue
		}

		category := strings.TrimSpace(artifact.Category)
		if category == "" {
			category = "shared_memory"
		}

		// Enrich metadata with provenance information
		metadata := artifact.Metadata
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["daemon_id"] = strings.TrimSpace(batch.DaemonID)
		metadata["user_id"] = strings.TrimSpace(batch.UserID)
		metadata["session_id"] = strings.TrimSpace(batch.SessionID)
		metadata["sync_timestamp"] = strings.TrimSpace(batch.Timestamp)
		metadata["promoted_at"] = time.Now().UTC().Format(time.RFC3339)

		artifactRef := agent.ExtractArtifactReference(metadata["artifact_ref"])
		if artifactRef.Name != "" || artifactRef.DatabaseName != "" || artifactRef.Type != "" {
			if err := agent.PromoteKnowledgeArtifact(
				ctx,
				db,
				nil,      // LLM not required for ingestion
				embedder, // Use configured embedder for vectorization
				text,
				artifactRef,
				artifactRef.Name,
				category,
				"daemon",       // scope_type
				batch.DaemonID, // scope_id
				metadata,
			); err != nil {
				log.Error("HQ sync: failed to promote knowledge artifact",
					"error", err,
					"index", i,
					"daemon_id", batch.DaemonID,
					"artifact_name", artifactRef.Name,
				)
				continue
			}
		} else if err := agent.PromoteSharedMemoryArtifact(
			ctx,
			db,
			nil,      // LLM not required for ingestion
			embedder, // Use configured embedder for vectorization
			text,
			sharedMemoryKBName,
			category,
			"daemon",       // scope_type
			batch.DaemonID, // scope_id
			metadata,
		); err != nil {
			log.Error("HQ sync: failed to promote artifact",
				"error", err,
				"index", i,
				"daemon_id", batch.DaemonID,
				"text_length", len(text),
			)
			// Continue with remaining artifacts even if one fails
			continue
		}

		log.Debug("HQ sync: promoted artifact",
			"index", i,
			"daemon_id", batch.DaemonID,
			"category", category,
			"text_length", len(text),
		)
	}

	// Increment federation cursor after successful storage
	cursor := GetGlobalFederationCursor()
	newCursor := cursor.IncrementVersion()
	log.Debug("HQ sync: federation cursor incremented",
		"new_cursor", newCursor,
		"daemon_id", batch.DaemonID)

	return nil
}

// getRootUserForSharedMemory retrieves and caches the root SA user for shared memory operations.
func getRootUserForSharedMemory(ctx context.Context) (*UserRecord, error) {
	// Check cache first
	cachedRootUserMu.RLock()
	if cachedRootUser != nil {
		cachedRootUserMu.RUnlock()
		return cachedRootUser, nil
	}
	cachedRootUserMu.RUnlock()

	// Cache miss - fetch from config
	cachedRootUserMu.Lock()
	defer cachedRootUserMu.Unlock()

	// Double-check after acquiring write lock
	if cachedRootUser != nil {
		return cachedRootUser, nil
	}

	// Look up root user from config (seeded during initial setup)
	rootUser, ok, err := config.findUserRecord(ctx, rootSAUsername)
	if err != nil {
		return nil, fmt.Errorf("find root SA user: %w", err)
	}
	if !ok || rootUser == nil {
		// Fallback to "root" if ltm_root doesn't exist yet
		rootUser, ok, err = config.findUserRecord(ctx, "root")
		if err != nil {
			return nil, fmt.Errorf("find root user: %w", err)
		}
		if !ok || rootUser == nil {
			return nil, fmt.Errorf("root SA user not found in config")
		}
	}

	// Cache for future syncs
	cachedRootUser = rootUser
	log.Info("HQ sync: cached root SA user for shared memory", "username", rootUser.Username)

	return rootUser, nil
}

// hqRetrieveRequest represents a request from a daemon to retrieve artifacts from HQ.
type hqRetrieveRequest struct {
	DaemonID string `json:"daemon_id"`
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
}

// hqRetrieveResponse represents the response containing artifacts from HQ.
type hqRetrieveResponse struct {
	Artifacts []hqArtifact `json:"artifacts"`
	Timestamp string       `json:"timestamp"`
}

type hqArtifact struct {
	Text     string         `json:"text"`
	Category string         `json:"category"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Score    float64        `json:"score"`
}

// HandleHQRetrieve allows daemons to pull relevant artifacts from HQ's shared memory.
func HandleHQRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req hqRetrieveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn("HQ retrieve: invalid JSON payload", "error", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	daemonID := strings.TrimSpace(req.DaemonID)
	if daemonID == "" {
		daemonID = "unknown-daemon"
	}

	// Check rate limits
	rateLimiter := getHQRateLimiter()
	allowed, retryAfter, reason := rateLimiter.Allow(daemonID)
	if !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		w.Header().Set("X-RateLimit-Reason", reason)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)

		log.Warn("HQ retrieve: rate limit exceeded",
			"daemon_id", daemonID,
			"reason", reason,
			"retry_after", retryAfter)

		json.NewEncoder(w).Encode(map[string]any{
			"error":               "rate limit exceeded",
			"reason":              reason,
			"retry_after_seconds": int(retryAfter.Seconds()),
			"daemon_id":           daemonID,
		})
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		// Return empty result for empty query
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(hqRetrieveResponse{
			Artifacts: []hqArtifact{},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	log.Info("HQ retrieve: received request",
		"daemon_id", daemonID,
		"query", query,
		"limit", limit,
	)

	// Retrieve artifacts from shared memory
	artifacts, err := retrieveSharedMemoryArtifacts(r.Context(), query, limit)
	if err != nil {
		log.Error("HQ retrieve: failed to retrieve artifacts",
			"error", err,
			"daemon_id", daemonID,
		)
		http.Error(w, "Failed to retrieve artifacts", http.StatusInternalServerError)
		return
	}

	log.Info("HQ retrieve: returning artifacts",
		"daemon_id", daemonID,
		"artifact_count", len(artifacts),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(hqRetrieveResponse{
		Artifacts: artifacts,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// retrieveSharedMemoryArtifacts retrieves relevant artifacts from the root SA's LTM.
func retrieveSharedMemoryArtifacts(ctx context.Context, query string, limit int) ([]hqArtifact, error) {
	// Get or cache the root SA user. If it is unavailable in a fresh environment,
	// return an empty set rather than surfacing a 500 to the caller.
	rootUser, err := getRootUserForSharedMemory(ctx)
	if err != nil {
		log.Warn("HQ retrieve: root SA user unavailable, returning empty results", "error", err)
		return []hqArtifact{}, nil
	}

	// Create auth context as the root SA user
	authCtx := sop.AuthContext{
		UserID:      rootUser.Username,
		Roles:       []string{rootUser.Role},
		Permissions: map[string]bool{},
	}
	ctx = sop.ContextWithAuth(ctx, authCtx)

	// Get system database options
	sysOpts, err := GetSystemDBOptions(ctx)
	if err != nil {
		log.Warn("HQ retrieve: system database unavailable, returning empty results", "error", err)
		return []hqArtifact{}, nil
	}

	db := aidb.NewDatabase(sysOpts)

	// Get configured embedder for vectorization
	embedder := GetConfiguredEmbedder(nil)

	// Retrieve relevant artifacts
	hits, err := agent.RetrieveSharedMemory(
		ctx,
		db,
		nil,      // LLM not required for retrieval
		embedder, // Use configured embedder for vectorization
		query,
		sharedMemoryKBName,
		limit,
	)
	if err != nil {
		log.Warn("HQ retrieve: shared-memory retrieval failed, returning empty results", "error", err)
		return []hqArtifact{}, nil
	}

	// Load category auto-share settings to filter artifacts
	categoryAutoShare, err := loadCategoryAutoShareSettings(ctx, db, sharedMemoryKBName)
	if err != nil {
		log.Warn("HQ retrieve: failed to load category auto-share settings, defaulting to share all", "error", err)
		// Continue with all artifacts if we can't load settings
	}

	// Convert hits to artifacts, filtering by category auto-share settings
	artifacts := make([]hqArtifact, 0)
	for _, hit := range hits {
		text := ""
		category := "shared_memory"
		metadata := make(map[string]any)

		if hit.Payload != nil {
			if t, ok := hit.Payload["text"].(string); ok {
				text = t
			}
			if c, ok := hit.Payload["category"].(string); ok {
				category = c
			}
			// Copy metadata but exclude internal fields
			for k, v := range hit.Payload {
				if k != "text" && k != "category" {
					metadata[k] = v
				}
			}
		}

		// Check if this category should be shared to daemons
		if !shouldShareCategoryToDaemons(category, categoryAutoShare) {
			log.Debug("HQ retrieve: filtering category from daemon sync", "category", category)
			continue
		}

		if text != "" {
			artifacts = append(artifacts, hqArtifact{
				Text:     text,
				Category: category,
				Metadata: metadata,
				Score:    float64(hit.Score),
			})
		}
	}

	return artifacts, nil
}

// loadCategoryAutoShareSettings loads category auto-share settings from the knowledge base.
func loadCategoryAutoShareSettings(ctx context.Context, db *aidb.Database, kbName string) (map[string]bool, error) {
	tx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, kbName, tx, nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("open knowledge base: %w", err)
	}

	categories, err := kb.Store.Categories(ctx)
	if err != nil {
		return nil, fmt.Errorf("get categories: %w", err)
	}

	settings := make(map[string]bool)

	ok, err := categories.First(ctx)
	if err != nil || !ok {
		return settings, nil
	}

	for {
		cat, err := categories.GetCurrentValue(ctx)
		if err != nil {
			break
		}

		if cat != nil && cat.Path != "" {
			// If AutoShareToDaemons is nil, default to true (share by default)
			// If false, don't share. If true, share.
			if cat.AutoShareToDaemons != nil && !*cat.AutoShareToDaemons {
				settings[cat.Path] = false
			} else {
				settings[cat.Path] = true
			}
		}

		if ok, err := categories.Next(ctx); err != nil || !ok {
			break
		}
	}

	return settings, nil
}

// shouldShareCategoryToDaemons checks if a category should be shared to daemons.
// Returns true if category should be shared (default), false otherwise.
func shouldShareCategoryToDaemons(category string, settings map[string]bool) bool {
	if settings == nil {
		return true // Default to sharing if no settings loaded
	}

	// Check exact match first
	if share, found := settings[category]; found {
		return share
	}

	// Default to sharing if category not found in settings
	return true
}
