package siteapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestHandleRegisterExposesSiteFlowMetric(t *testing.T) {
	sopbackend.ResetObservabilityStateForTesting()
	mux := newSiteAPIContractMux(t)
	handler := sopbackend.NewObservabilityHandler(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"site-metric-user","password":"secret123","email":"site@example.com","role":"user"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("registration returned %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	metricsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("/metrics returned %d, want %d", metricsRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(metricsRecorder.Body.String(), `name="site_auth_register_requests"`) {
		t.Fatalf("metrics omitted site registration metric: %s", metricsRecorder.Body.String())
	}
}

func TestPendingCheckoutRecoverRouteIsRegistered(t *testing.T) {
	mux := newSiteAPIContractMux(t)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/pending-checkout/recover", strings.NewReader(`{"checkout_session_id":""}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("pending checkout recovery returned %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestHandleLogoutClearsSessionCookies(t *testing.T) {
	mux := newSiteAPIContractMux(t)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("logout returned %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode logout response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("logout status = %q, want %q", payload.Status, "ok")
	}

	cleared := map[string]bool{}
	for _, cookie := range recorder.Result().Cookies() {
		if (cookie.Name == "sop_access_token" || cookie.Name == "sop_refresh_token") && cookie.MaxAge < 0 {
			cleared[cookie.Name] = true
		}
	}
	if !cleared["sop_access_token"] || !cleared["sop_refresh_token"] {
		t.Fatalf("logout did not clear both session cookies: %#v", recorder.Result().Cookies())
	}
}
