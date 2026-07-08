package sopbackend

import (
	"encoding/json"
	"net/http"
	"time"

	log "log/slog"
)

// FederationPollRequest represents a daemon's poll request.
type FederationPollRequest struct {
	DaemonID          string `json:"daemon_id"`
	LastAppliedCursor string `json:"last_applied_cursor"`
}

// FederationPollResponse tells the daemon whether new content exists.
type FederationPollResponse struct {
	CurrentCursor  string    `json:"current_cursor"`
	HasDelta       bool      `json:"has_delta"`
	DeltaCount     int       `json:"delta_count,omitempty"`
	StreamEndpoint string    `json:"stream_endpoint,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// HandleFederationPoll provides lightweight change detection for daemons.
// This endpoint is called once per hour by each daemon to check if federation
// content has changed since their last sync.
func HandleFederationPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FederationPollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn("Federation poll: invalid request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	daemonID := req.DaemonID
	if daemonID == "" {
		daemonID = "unknown-daemon"
	}

	log.Debug("Federation poll received",
		"daemon_id", daemonID,
		"last_cursor", req.LastAppliedCursor)

	// Get current federation cursor
	cursor := GetGlobalFederationCursor()
	currentCursor, version, lastUpdate := cursor.GetCurrentCursor()

	// Check if daemon needs an update
	hasDelta := cursor.HasDeltaSince(req.LastAppliedCursor)

	response := FederationPollResponse{
		CurrentCursor: currentCursor,
		HasDelta:      hasDelta,
		Timestamp:     time.Now().UTC(),
	}

	if hasDelta {
		// TODO: In a more sophisticated version, calculate actual delta count
		// For now, signal that streaming is available
		response.DeltaCount = 1 // Placeholder
		response.StreamEndpoint = "/api/daemon/federation/stream"

		log.Info("Federation poll: delta detected",
			"daemon_id", daemonID,
			"current_cursor", currentCursor,
			"daemon_cursor", req.LastAppliedCursor,
			"version", version,
			"last_update", lastUpdate)
	} else {
		log.Debug("Federation poll: no changes",
			"daemon_id", daemonID,
			"cursor", currentCursor)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("Federation poll: failed to encode response", "error", err)
	}
}
