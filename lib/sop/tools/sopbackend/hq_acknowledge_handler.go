package sopbackend

import (
	"encoding/json"
	"io"
	log "log/slog"
	"net/http"
	"time"
)

// handleAcknowledgeSpace receives acknowledgments from daemons when they successfully receive a Space.
func handleAcknowledgeSpace(w http.ResponseWriter, r *http.Request) {
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
		SpaceName    string `json:"space_name"`
		DatabaseName string `json:"database_name"`
		DaemonID     string `json:"daemon_id"`
		Status       string `json:"status"`
		Timestamp    string `json:"timestamp"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		log.Warn("HQ acknowledge: invalid JSON payload", "error", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.SpaceName == "" || req.DaemonID == "" {
		http.Error(w, "space_name and daemon_id are required", http.StatusBadRequest)
		return
	}

	log.Info("HQ acknowledge: received acknowledgment",
		"space_name", req.SpaceName,
		"daemon_id", req.DaemonID,
		"status", req.Status,
	)

	// Update federation state to acknowledged
	if state, found := GetSpaceFederationState(req.SpaceName, req.DaemonID); found {
		updateSpaceFederationState(req.SpaceName, req.DaemonID, "acknowledged", state.Version)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "recorded",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
