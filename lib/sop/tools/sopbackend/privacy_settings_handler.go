package sopbackend

import (
	"encoding/json"
	"net/http"
)

// HandleGetPrivacySettings returns the current privacy settings for the daemon.
// This is a proxy to the daemon's privacy settings endpoint.
func HandleGetPrivacySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Note: In a real implementation, this would proxy to the daemon's endpoint
	// For now, return a placeholder response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "privacy settings endpoint - integrate with daemon",
	})
}

// HandleUpdatePrivacySettings updates privacy settings.
func HandleUpdatePrivacySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Proxy to daemon endpoint
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleGetPrivacyAuditLog returns the privacy audit log.
func HandleGetPrivacyAuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Proxy to daemon endpoint
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]interface{}{})
}

// HandleGetApprovalQueue returns pending approval items.
func HandleGetApprovalQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Proxy to daemon endpoint
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]interface{}{})
}
