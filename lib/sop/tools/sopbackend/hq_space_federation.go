package sopbackend

import (
	"bytes"
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
	"github.com/sharedcode/sop/ai/database"
)

// SpaceFederationState tracks which daemons have which Spaces.
type SpaceFederationState struct {
	SpaceName    string         `json:"space_name"`
	DatabaseName string         `json:"database_name"`
	DaemonID     string         `json:"daemon_id"`
	Status       string         `json:"status"` // pending, delivered, acknowledged, failed
	PushedAt     time.Time      `json:"pushed_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Version      int            `json:"version"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

var (
	spaceFederationStates   = make(map[string]SpaceFederationState) // key: spaceName:daemonID
	spaceFederationStatesMu sync.RWMutex
)

// hqPushSpaceRequest represents a request from HQ to push a Space to a daemon.
type hqPushSpaceRequest struct {
	SpaceName    string `json:"space_name"`
	DatabaseName string `json:"database_name"`
	DaemonID     string `json:"daemon_id"`
	Version      int    `json:"version"`
}

// hqPushSpaceResponse represents the response after pushing a Space.
type hqPushSpaceResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// HandleHQPushSpace allows HQ to push a Space to a specific daemon.
// This is the HQ-side handler that initiates the push.
func HandleHQPushSpace(w http.ResponseWriter, r *http.Request) {
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

	var req hqPushSpaceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn("HQ push space: invalid JSON payload", "error", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" || req.DaemonID == "" {
		http.Error(w, "space_name, database_name, and daemon_id are required", http.StatusBadRequest)
		return
	}

	log.Info("HQ push space: received request",
		"space_name", req.SpaceName,
		"database_name", req.DatabaseName,
		"daemon_id", req.DaemonID,
		"version", req.Version,
	)

	// Export the Space from HQ
	spaceJSON, err := exportSpaceFromHQ(r.Context(), req.DatabaseName, req.SpaceName)
	if err != nil {
		log.Error("HQ push space: failed to export Space",
			"error", err,
			"space_name", req.SpaceName,
			"database_name", req.DatabaseName,
		)
		http.Error(w, "Failed to export Space", http.StatusInternalServerError)
		return
	}

	// Push the Space to the daemon
	daemonURL, err := getDaemonURL(req.DaemonID)
	if err != nil {
		log.Error("HQ push space: failed to get daemon URL",
			"error", err,
			"daemon_id", req.DaemonID,
		)
		http.Error(w, "Failed to get daemon URL", http.StatusInternalServerError)
		return
	}

	if err := pushSpaceToDaemon(r.Context(), daemonURL, req.SpaceName, req.DatabaseName, spaceJSON); err != nil {
		log.Error("HQ push space: failed to push to daemon",
			"error", err,
			"daemon_id", req.DaemonID,
			"space_name", req.SpaceName,
		)

		// Update federation state to failed
		updateSpaceFederationState(req.SpaceName, req.DaemonID, "failed", req.Version)

		http.Error(w, "Failed to push Space to daemon", http.StatusInternalServerError)
		return
	}

	// Update federation state to delivered
	updateSpaceFederationState(req.SpaceName, req.DaemonID, "delivered", req.Version)

	log.Info("HQ push space: successfully pushed",
		"space_name", req.SpaceName,
		"daemon_id", req.DaemonID,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(hqPushSpaceResponse{
		Status:    "delivered",
		Message:   fmt.Sprintf("Space %s pushed to daemon %s", req.SpaceName, req.DaemonID),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// HandleHQReshareSpace allows HQ to reshare a Space to multiple daemons.
func HandleHQReshareSpace(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		SpaceName    string   `json:"space_name"`
		DatabaseName string   `json:"database_name"`
		DaemonIDs    []string `json:"daemon_ids"`
		Version      int      `json:"version"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn("HQ reshare space: invalid JSON payload", "error", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DatabaseName == "" || len(req.DaemonIDs) == 0 {
		http.Error(w, "space_name, database_name, and daemon_ids are required", http.StatusBadRequest)
		return
	}

	log.Info("HQ reshare space: received request",
		"space_name", req.SpaceName,
		"database_name", req.DatabaseName,
		"daemon_count", len(req.DaemonIDs),
	)

	// Export the Space once
	spaceJSON, err := exportSpaceFromHQ(r.Context(), req.DatabaseName, req.SpaceName)
	if err != nil {
		log.Error("HQ reshare space: failed to export Space",
			"error", err,
			"space_name", req.SpaceName,
		)
		http.Error(w, "Failed to export Space", http.StatusInternalServerError)
		return
	}

	// Push to all daemons concurrently
	results := make(map[string]string) // daemonID -> status
	var resultsMu sync.Mutex
	var wg sync.WaitGroup

	for _, daemonID := range req.DaemonIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			daemonURL, err := getDaemonURL(id)
			if err != nil {
				log.Error("HQ reshare: failed to get daemon URL",
					"error", err,
					"daemon_id", id,
				)
				resultsMu.Lock()
				results[id] = "failed: " + err.Error()
				resultsMu.Unlock()
				updateSpaceFederationState(req.SpaceName, id, "failed", req.Version)
				return
			}

			if err := pushSpaceToDaemon(r.Context(), daemonURL, req.SpaceName, req.DatabaseName, spaceJSON); err != nil {
				log.Error("HQ reshare: failed to push to daemon",
					"error", err,
					"daemon_id", id,
				)
				resultsMu.Lock()
				results[id] = "failed: " + err.Error()
				resultsMu.Unlock()
				updateSpaceFederationState(req.SpaceName, id, "failed", req.Version)
				return
			}

			resultsMu.Lock()
			results[id] = "delivered"
			resultsMu.Unlock()
			updateSpaceFederationState(req.SpaceName, id, "delivered", req.Version)

			log.Info("HQ reshare: successfully pushed to daemon",
				"space_name", req.SpaceName,
				"daemon_id", id,
			)
		}(daemonID)
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "completed",
		"results":   results,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// exportSpaceFromHQ exports a Space from HQ's database.
func exportSpaceFromHQ(ctx context.Context, dbName, spaceName string) ([]byte, error) {
	dbOpt, err := GetDBOptions(ctx, dbName)
	if err != nil {
		return nil, fmt.Errorf("get database options: %w", err)
	}

	db := database.NewDatabase(dbOpt)

	trans, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer trans.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, spaceName, trans, nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("open knowledge base: %w", err)
	}

	var buf bytes.Buffer
	if err := kb.ExportJSON(ctx, &buf); err != nil {
		return nil, fmt.Errorf("export JSON: %w", err)
	}

	return buf.Bytes(), nil
}

// pushSpaceToDaemon sends a Space to a daemon's receive endpoint.
func pushSpaceToDaemon(ctx context.Context, daemonURL, spaceName, dbName string, spaceJSON []byte) error {
	endpoint := strings.TrimRight(daemonURL, "/") + "/api/daemon/receive-space"

	payload := map[string]any{
		"space_name":    spaceName,
		"database_name": dbName,
		"space_data":    string(spaceJSON),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// getDaemonURL returns the URL for a given daemon ID from the daemon registry.
func getDaemonURL(daemonID string) (string, error) {
	registry := GetDaemonRegistry()
	url, found := registry.GetURL(daemonID)
	if !found {
		return "", fmt.Errorf("daemon %s not found in registry (may not have synced yet)", daemonID)
	}
	return url, nil
}

// updateSpaceFederationState updates the federation state for a Space-daemon pair.
func updateSpaceFederationState(spaceName, daemonID, status string, version int) {
	spaceFederationStatesMu.Lock()
	defer spaceFederationStatesMu.Unlock()

	key := spaceName + ":" + daemonID
	state := spaceFederationStates[key]

	state.SpaceName = spaceName
	state.DaemonID = daemonID
	state.Status = status
	state.Version = version
	state.UpdatedAt = time.Now().UTC()

	if state.PushedAt.IsZero() {
		state.PushedAt = time.Now().UTC()
	}

	spaceFederationStates[key] = state

	log.Debug("Updated space federation state",
		"space_name", spaceName,
		"daemon_id", daemonID,
		"status", status,
		"version", version,
	)
}

// GetSpaceFederationState returns the federation state for a Space-daemon pair.
func GetSpaceFederationState(spaceName, daemonID string) (SpaceFederationState, bool) {
	spaceFederationStatesMu.RLock()
	defer spaceFederationStatesMu.RUnlock()

	key := spaceName + ":" + daemonID
	state, found := spaceFederationStates[key]
	return state, found
}

// ListSpaceFederationStates returns all federation states.
func ListSpaceFederationStates() []SpaceFederationState {
	spaceFederationStatesMu.RLock()
	defer spaceFederationStatesMu.RUnlock()

	states := make([]SpaceFederationState, 0, len(spaceFederationStates))
	for _, state := range spaceFederationStates {
		states = append(states, state)
	}
	return states
}
