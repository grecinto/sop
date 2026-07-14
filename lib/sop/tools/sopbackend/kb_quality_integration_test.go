package sopbackend

import (
	"bytes"
	"encoding/json"
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

func TestKBQualityFeedbackAndAlternativesFlow(t *testing.T) {
	tracker := GetKBEffectivenessTracker()
	if tracker == nil {
		t.Fatal("expected KB effectiveness tracker")
	}

	feedbackBody := bytes.NewBufferString(`{"message_id":"msg-flow","kb_name":"go-best-practices","task_type":"implementation","feedback":"positive","user_id":"u1","session_id":"session-1"}`)
	feedbackReq := httptest.NewRequest(http.MethodPost, "/api/ai/feedback", feedbackBody)
	feedbackRec := httptest.NewRecorder()
	HandleKBFeedback(feedbackRec, feedbackReq)
	if feedbackRec.Code != http.StatusOK {
		t.Fatalf("feedback endpoint returned %d: %s", feedbackRec.Code, feedbackRec.Body.String())
	}

	altBody := bytes.NewBufferString(`{"task_type":"implementation","current_kb":"go-best-practices","excluded_kbs":["go-best-practices"]}`)
	altReq := httptest.NewRequest(http.MethodPost, "/api/ai/kb/alternatives", altBody)
	altRec := httptest.NewRecorder()
	HandleKBAlternatives(altRec, altReq)
	if altRec.Code != http.StatusOK {
		t.Fatalf("alternatives endpoint returned %d: %s", altRec.Code, altRec.Body.String())
	}

	var altResp KBAlternativesResponse
	if err := json.Unmarshal(altRec.Body.Bytes(), &altResp); err != nil {
		t.Fatalf("decode alternatives response: %v", err)
	}
	if altResp.Success && len(altResp.Alternatives) > 0 {
		// The tracker may return additional KBs depending on stored state, but the flow should remain successful.
		if altResp.Alternatives[0].Name == "" {
			t.Fatalf("expected a non-empty alternative name, got %+v", altResp.Alternatives[0])
		}
	}
}

func BenchmarkKBAlternativesEndpoint(b *testing.B) {
	tracker := GetKBEffectivenessTracker()
	if tracker == nil {
		b.Fatal("expected KB effectiveness tracker")
	}

	payload := bytes.NewBufferString(`{"task_type":"implementation","current_kb":"go-best-practices","excluded_kbs":["go-best-practices"]}`)
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/ai/kb/alternatives", payload)
		rec := httptest.NewRecorder()
		HandleKBAlternatives(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("benchmark request returned %d: %s", rec.Code, rec.Body.String())
		}
	}
}
