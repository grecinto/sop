package sopbackend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterMonitorabilityHandlersServesStandardEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	RegisterMonitorabilityHandlers(mux)

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "healthz", path: "/healthz"},
		{name: "readyz", path: "/readyz"},
		{name: "info", path: "/info"},
		{name: "metrics", path: "/metrics"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected %s to return %d, got %d", tc.path, http.StatusOK, rec.Code)
			}
		})
	}
}

func TestObservabilityHandlerRecordsRequestsAndExposesMetrics(t *testing.T) {
	resetObservabilityStateForTesting()

	mux := http.NewServeMux()
	RegisterAllHandlers(mux)
	handler := NewObservabilityHandler(mux)

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected /healthz to return %d, got %d", http.StatusOK, healthRec.Code)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	handler.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected /readyz to return %d, got %d", http.StatusOK, readyRec.Code)
	}

	infoReq := httptest.NewRequest(http.MethodGet, "/info", nil)
	infoRec := httptest.NewRecorder()
	handler.ServeHTTP(infoRec, infoReq)
	if infoRec.Code != http.StatusOK {
		t.Fatalf("expected /info to return %d, got %d", http.StatusOK, infoRec.Code)
	}

	var infoPayload map[string]any
	if err := json.NewDecoder(infoRec.Body).Decode(&infoPayload); err != nil {
		t.Fatalf("expected /info to return valid JSON, got error: %v", err)
	}
	if infoPayload["status"] != "ok" {
		t.Fatalf("expected /info status to be ok, got %v", infoPayload["status"])
	}
	if _, ok := infoPayload["hostname"]; !ok {
		t.Fatalf("expected /info to include hostname, got payload %v", infoPayload)
	}
	if _, ok := infoPayload["environment"]; !ok {
		t.Fatalf("expected /info to include environment, got payload %v", infoPayload)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	handler.ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("expected /metrics to return %d, got %d", http.StatusOK, metricsRec.Code)
	}

	body := metricsRec.Body.String()
	if !strings.Contains(metricsRec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("expected metrics endpoint to advertise Prometheus text content type, got %q", metricsRec.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, "sop_http_requests_total") {
		t.Fatalf("expected metrics payload to include Prometheus counter, got %q", body)
	}
	if !strings.Contains(body, "path=\"/healthz\"") {
		t.Fatalf("expected metrics payload to include /healthz route label, got %q", body)
	}
	if !strings.Contains(body, "sop_ha_status") {
		t.Fatalf("expected metrics payload to include HA metric, got %q", body)
	}
}
