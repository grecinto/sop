package sopbackend

import (
	"net/http"
)

// HandleKBQualityDashboardPage serves the KB Quality Dashboard UI page.
func HandleKBQualityDashboardPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	router := NewUIRouter(SharedUIAssets(), "")
	if router.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title": "KB Quality Dashboard",
	}

	if err := router.Templates.ExecuteTemplate(w, "kb_quality.html", data); err != nil {
		http.Error(w, "Failed to render KB Quality dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
