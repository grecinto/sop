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
)

// hqSyncBatch represents a sync payload from a local daemon to HQ.
type hqSyncBatch struct {
	Kind      string `json:"kind"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	DaemonID  string `json:"daemon_id"`
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

	userID := strings.TrimSpace(batch.UserID)
	if userID == "" {
		userID = "daemon"
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

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "accepted",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"daemon_id": daemonID,
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

		// Promote to central shared memory
		if err := agent.PromoteSharedMemoryArtifact(
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
	// Get or cache the root SA user
	rootUser, err := getRootUserForSharedMemory(ctx)
	if err != nil {
		return nil, fmt.Errorf("get root SA user: %w", err)
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
		return nil, fmt.Errorf("get system database options: %w", err)
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
		return nil, fmt.Errorf("retrieve shared memory: %w", err)
	}

	// Convert hits to artifacts
	var artifacts []hqArtifact
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
