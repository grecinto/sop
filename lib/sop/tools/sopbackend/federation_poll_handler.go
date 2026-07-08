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
	CurrentCursor  string                 `json:"current_cursor"`
	HasDelta       bool                   `json:"has_delta"`
	DeltaCount     int                    `json:"delta_count,omitempty"`
	StreamEndpoint string                 `json:"stream_endpoint,omitempty"`
	Throttle       map[string]interface{} `json:"throttle,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
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

	// Check rate limiting first
	rateLimiter := getHQRateLimiter()
	allowed, retryAfter, limitReason := rateLimiter.Allow(daemonID)

	// Get cached load metrics (lightweight, updated after token consumption)
	cachedMetrics := rateLimiter.GetCachedLoadMetrics()
	loadPercent := cachedMetrics["load_percent"].(float64)

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

	// Compute throttle hints based on rate limit status, cached load, and delta
	if !allowed {
		// Rate limited - tell daemon to back off
		retrySeconds := int(retryAfter.Seconds())
		if retrySeconds < 1 {
			retrySeconds = 1 // Minimum 1 second
		}
		response.Throttle = map[string]interface{}{
			"allowed":               false,
			"reason":                limitReason,
			"sync_interval_seconds": retrySeconds + 60, // Add buffer
			"batch_size":            5,
			"max_queue_depth":       500,
			"priority_boost":        false,
		}
		log.Info("Federation poll: rate limited",
			"daemon_id", daemonID,
			"reason", limitReason,
			"retry_after", retryAfter,
			"load_percent", loadPercent)
	} else if loadPercent > 0.9 {
		// System under very high load (>90% capacity used)
		response.Throttle = map[string]interface{}{
			"allowed":               false,
			"reason":                "high_load",
			"sync_interval_seconds": 600, // 10 minutes
			"batch_size":            5,
			"max_queue_depth":       500,
			"priority_boost":        false,
		}
		log.Info("Federation poll: high load throttle",
			"daemon_id", daemonID,
			"load_percent", loadPercent)
	} else if loadPercent > 0.75 {
		// System under moderate load (>75% capacity used)
		response.Throttle = map[string]interface{}{
			"allowed":               true,
			"reason":                "moderate_load",
			"sync_interval_seconds": 1800, // 30 minutes
			"batch_size":            10,
			"max_queue_depth":       800,
			"priority_boost":        false,
		}
	} else if hasDelta {
		// Delta exists and load is healthy - allow sync with normal settings
		response.Throttle = map[string]interface{}{
			"allowed":               true,
			"reason":                "healthy_with_delta",
			"sync_interval_seconds": 900, // 15 minutes
			"batch_size":            25,
			"max_queue_depth":       1000,
			"priority_boost":        false,
		}
	} else {
		// No delta and load is healthy - slow down cadence
		response.Throttle = map[string]interface{}{
			"allowed":               true,
			"reason":                "idle_no_delta",
			"sync_interval_seconds": 3600, // 1 hour
			"batch_size":            10,
			"max_queue_depth":       1000,
			"priority_boost":        false,
		}
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
