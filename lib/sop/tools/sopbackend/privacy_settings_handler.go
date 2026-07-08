package sopbackend

import (
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"
)

var privacyTemplates *template.Template

// InitPrivacyTemplates loads privacy UI templates
func InitPrivacyTemplates(templatesDir string) error {
	var err error
	privacyTemplates, err = template.ParseGlob(filepath.Join(templatesDir, "privacy_*.html"))
	return err
}

// HandlePrivacySettingsPage serves the privacy settings UI page
func HandlePrivacySettingsPage(w http.ResponseWriter, r *http.Request) {
	if err := privacyTemplates.ExecuteTemplate(w, "privacy_settings.html", nil); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

// HandlePrivacyAuditLogPage serves the privacy audit log viewer
func HandlePrivacyAuditLogPage(w http.ResponseWriter, r *http.Request) {
	if err := privacyTemplates.ExecuteTemplate(w, "privacy_audit_log.html", nil); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

// HandlePrivacyApprovalQueuePage serves the approval queue UI page
func HandlePrivacyApprovalQueuePage(w http.ResponseWriter, r *http.Request) {
	if err := privacyTemplates.ExecuteTemplate(w, "privacy_approval_queue.html", nil); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

// HandlePrivacyDashboardPage serves the privacy dashboard UI page
func HandlePrivacyDashboardPage(w http.ResponseWriter, r *http.Request) {
	if err := privacyTemplates.ExecuteTemplate(w, "privacy_dashboard.html", nil); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

// HandlePrivacyAdvancedPage serves the advanced privacy controls UI page
func HandlePrivacyAdvancedPage(w http.ResponseWriter, r *http.Request) {
	if err := privacyTemplates.ExecuteTemplate(w, "privacy_advanced.html", nil); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

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
