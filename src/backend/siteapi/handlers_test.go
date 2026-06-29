package siteapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestHandleRegisterExposesSiteFlowMetric(t *testing.T) {
	sopbackend.ResetObservabilityStateForTesting()

	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)
	handler := sopbackend.NewObservabilityHandler(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"site-metric-user","password":"secret123","email":"site@example.com","role":"user","subscription_plan":"starter"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected register to return %d, got %d", http.StatusCreated, rec.Code)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	handler.ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("expected /metrics to return %d, got %d", http.StatusOK, metricsRec.Code)
	}

	body := metricsRec.Body.String()
	if !strings.Contains(body, `name="site_auth_register_requests"`) {
		t.Fatalf("expected metrics payload to include site registration metric, got %q", body)
	}
}
