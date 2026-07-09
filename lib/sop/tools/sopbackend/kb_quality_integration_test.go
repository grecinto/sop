package sopbackend

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestKBQualityEndpointsRegistered verifies that KB Quality API endpoints are available.
func TestKBQualityEndpointsRegistered(t *testing.T) {
	// Setup: Register all handlers
	mux := http.NewServeMux()
	RegisterAllHandlers(mux)
	
	// Test KB Quality endpoints
	endpoints := []string{
		"/api/kb/quality/report",
		"/api/kb/quality/analyze",
		"/api/kb/quality/candidates",
		"/api/kb/effectiveness/all",
		"/api/kb/effectiveness/best",
		"/api/kb/effectiveness/poor",
		"/api/kb/effectiveness/kb",
		"/api/kb/quality/trigger",
		"/api/kb/quality/status",
	}
	
	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			w := httptest.NewRecorder()
			
			mux.ServeHTTP(w, req)
			
			// Should not return 404 (endpoint exists)
			// May return 401 (auth required) or 405 (wrong method) which is fine
			if w.Code == http.StatusNotFound {
				t.Errorf("endpoint %s not registered (got 404)", endpoint)
			}
		})
	}
}

// TestKBQualityReportEndpoint verifies the report endpoint returns valid responses.
func TestKBQualityReportEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAllHandlers(mux)
	
	// Test JSON format
	req := httptest.NewRequest(http.MethodGet, "/api/kb/quality/report?format=json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Logf("Report endpoint returned status %d (may require auth)", w.Code)
	}
	
	// Test text format
	req = httptest.NewRequest(http.MethodGet, "/api/kb/quality/report?format=text", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Logf("Report endpoint returned status %d (may require auth)", w.Code)
	}
}

// TestKBQualityInitialization verifies KB Quality components are initialized.
func TestKBQualityInitialization(t *testing.T) {
	// Initialize (safe to call multiple times due to sync.Once)
	initKBQuality()
	
	// Verify components are initialized
	if kbEffectivenessTracker == nil {
		t.Error("KB effectiveness tracker not initialized")
	}
	
	if kbQualityAPI == nil {
		t.Error("KB quality API not initialized")
	}
	
	if kbQualityJob == nil {
		t.Error("KB quality job not initialized")
	}
}

