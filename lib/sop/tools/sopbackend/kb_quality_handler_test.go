package sopbackend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestKBQualityDashboardEndpoint verifies the KB Quality dashboard is accessible.
func TestKBQualityDashboardEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAllHandlers(mux)

	// Test GET request (should work or require auth)
	req := httptest.NewRequest(http.MethodGet, "/kb-quality", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	// Should not return 404 (endpoint exists)
	// May return 401 (auth required) which is fine, or 200 if no auth
	if w.Code == http.StatusNotFound {
		t.Errorf("/kb-quality endpoint not found (got 404)")
	}

	t.Logf("KB Quality endpoint returned status %d", w.Code)
}

// TestKBQualityDashboardHandler tests the handler directly.
func TestKBQualityDashboardHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/kb-quality", nil)
	w := httptest.NewRecorder()

	HandleKBQualityDashboardPage(w, req)

	// Should return 200 and HTML content
	if w.Code != http.StatusOK {
		t.Logf("KB Quality handler returned status %d (may need template setup)", w.Code)
	}

	// Check if response contains HTML indicators
	body := w.Body.String()
	if w.Code == http.StatusOK {
		if len(body) == 0 {
			t.Error("KB Quality handler returned empty body")
		}
	}
}

func TestKBQualityDashboardHandlerRendersTemplate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/kb-quality", nil)
	w := httptest.NewRecorder()

	HandleKBQualityDashboardPage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected KB quality dashboard to render with status 200, got %d and body %q", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !strings.Contains(got, "Space Quality Dashboard") {
		t.Fatalf("expected response body to contain dashboard title, got %q", got)
	}
}
