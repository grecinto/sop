package siteapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
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

func TestHandleLogoutClearsSessionCookies(t *testing.T) {
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected logout to return %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("status = %q, want %q", payload.Status, "ok")
	}

	var foundAccess bool
	var foundRefresh bool
	for _, cookie := range rec.Result().Cookies() {
		switch cookie.Name {
		case "sop_access_token":
			foundAccess = true
			if cookie.MaxAge >= 0 {
				t.Fatalf("expected sop_access_token cookie to be cleared, got MaxAge=%d", cookie.MaxAge)
			}
		case "sop_refresh_token":
			foundRefresh = true
			if cookie.MaxAge >= 0 {
				t.Fatalf("expected sop_refresh_token cookie to be cleared, got MaxAge=%d", cookie.MaxAge)
			}
		}
	}
	if !foundAccess || !foundRefresh {
		t.Fatalf("expected both session cookies to be cleared, got %#v", rec.Result().Cookies())
	}
}

func TestHandleUnregisterClearsSessionCookies(t *testing.T) {
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"unregister-cookie-user","password":"secret123","email":"unregister-cookie@example.com","role":"user","subscription_plan":"starter"}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	mux.ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("expected register to return %d, got %d", http.StatusCreated, registerRec.Code)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/site/auth/login", strings.NewReader(`{"username":"unregister-cookie-user","password":"secret123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	mux.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login to return %d, got %d", http.StatusOK, loginRec.Code)
	}

	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	if loginPayload.AccessToken == "" {
		t.Fatal("expected login response to include access_token")
	}

	unregisterReq := httptest.NewRequest(http.MethodPost, "/api/auth/unregister", nil)
	unregisterReq = unregisterReq.WithContext(sop.ContextWithAuth(unregisterReq.Context(), sop.AuthContext{UserID: "unregister-cookie-user", Roles: []string{sop.RoleUser}}))
	for _, cookie := range loginRec.Result().Cookies() {
		unregisterReq.AddCookie(cookie)
	}
	unregisterRec := httptest.NewRecorder()
	mux.ServeHTTP(unregisterRec, unregisterReq)
	if unregisterRec.Code != http.StatusOK {
		t.Fatalf("expected unregister to return %d, got %d", http.StatusOK, unregisterRec.Code)
	}

	var foundAccess bool
	var foundRefresh bool
	for _, cookie := range unregisterRec.Result().Cookies() {
		switch cookie.Name {
		case "sop_access_token":
			foundAccess = true
			if cookie.MaxAge >= 0 {
				t.Fatalf("expected sop_access_token cookie to be cleared, got MaxAge=%d", cookie.MaxAge)
			}
		case "sop_refresh_token":
			foundRefresh = true
			if cookie.MaxAge >= 0 {
				t.Fatalf("expected sop_refresh_token cookie to be cleared, got MaxAge=%d", cookie.MaxAge)
			}
		}
	}
	if !foundAccess || !foundRefresh {
		t.Fatalf("expected both session cookies to be cleared after unregister, got %#v", unregisterRec.Result().Cookies())
	}
}

func TestHandleStatusIncludesPaymentsAndSubscriptions(t *testing.T) {
	sopbackend.SetDisableAuth(true)
	defer sopbackend.SetDisableAuth(false)

	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	cfg := sopbackend.GetConfig()
	_, err := cfg.RegisterUserWithRequest(context.Background(), sopbackend.RegisterUserRequest{
		Username:         "payment-history-user",
		Password:         "secret123",
		Email:            "payment-history@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	sopbackend.SetConfig(cfg)

	cfg = sopbackend.GetConfig()
	if _, err := cfg.CreateCheckoutSession(context.Background(), "payment-history-user", "starter"); err != nil {
		t.Fatalf("create checkout session: %v", err)
	}
	if err := cfg.HandleBillingWebhook(context.Background(), sopbackend.BillingWebhookEvent{EventType: "checkout.session.completed", EventID: "evt-payment-history", UserID: "payment-history-user", Plan: "starter"}); err != nil {
		t.Fatalf("handle billing webhook: %v", err)
	}
	sopbackend.SetConfig(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "payment-history-user", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status to return %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		User struct {
			Payments []struct {
				Status string  `json:"status"`
				Amount float64 `json:"amount"`
			} `json:"payments"`
			Subscriptions []struct {
				Status   string `json:"status"`
				PlanCode string `json:"plan_code"`
			} `json:"subscriptions"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.User.Payments) == 0 {
		t.Fatal("expected payments to be included in status response")
	}
	if len(payload.User.Subscriptions) == 0 {
		t.Fatal("expected subscriptions to be included in status response")
	}
	if payload.User.Payments[0].Status != "succeeded" {
		t.Fatalf("payment status = %q, want %q", payload.User.Payments[0].Status, "succeeded")
	}
}

func TestHandleLoginSetsSessionCookies(t *testing.T) {
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"site-login-user","password":"secret123","email":"login@example.com","role":"user","subscription_plan":"starter"}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	mux.ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("expected register to return %d, got %d", http.StatusCreated, registerRec.Code)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/site/auth/login", strings.NewReader(`{"username":"site-login-user","password":"secret123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	mux.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login to return %d, got %d", http.StatusOK, loginRec.Code)
	}

	var foundAccess bool
	var foundRefresh bool
	for _, cookie := range loginRec.Result().Cookies() {
		switch cookie.Name {
		case "sop_access_token":
			foundAccess = true
			if cookie.MaxAge <= 0 {
				t.Fatalf("expected sop_access_token cookie to be persistent, got MaxAge=%d", cookie.MaxAge)
			}
		case "sop_refresh_token":
			foundRefresh = true
			if cookie.MaxAge <= 0 {
				t.Fatalf("expected sop_refresh_token cookie to be persistent, got MaxAge=%d", cookie.MaxAge)
			}
		}
	}
	if !foundAccess || !foundRefresh {
		t.Fatalf("expected both session cookies to be set, got %#v", loginRec.Result().Cookies())
	}
}

func TestHandleDownloadReturnsSignedArtifactForActiveSubscribers(t *testing.T) {
	t.Setenv("SOP_DOWNLOAD_URL", "https://cdn.example.com/releases/sop-daemon.tar.gz")

	sopbackend.SetDisableAuth(false)
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"download-user","password":"secret123","email":"download@example.com","role":"user","subscription_plan":"starter"}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	mux.ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("expected setup register to return %d, got %d", http.StatusCreated, registerRec.Code)
	}

	cfg := sopbackend.GetConfig()
	if err := cfg.HandleBillingWebhook(context.Background(), sopbackend.BillingWebhookEvent{EventType: "checkout.session.completed", EventID: "evt-download", UserID: "download-user", Plan: "starter"}); err != nil {
		t.Fatalf("handle billing webhook: %v", err)
	}
	sopbackend.SetConfig(cfg)

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/auth/download", nil)
	downloadReq = downloadReq.WithContext(sop.ContextWithAuth(downloadReq.Context(), sop.AuthContext{UserID: "download-user", Roles: []string{sop.RoleUser}}))
	downloadRec := httptest.NewRecorder()
	mux.ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("expected download to return %d, got %d", http.StatusOK, downloadRec.Code)
	}

	var payload struct {
		DownloadURL string `json:"download_url"`
		Available   bool   `json:"available"`
	}
	if err := json.Unmarshal(downloadRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.Available {
		t.Fatalf("expected download to be available, got %#v", payload)
	}
	if payload.DownloadURL != "https://cdn.example.com/releases/sop-daemon.tar.gz" {
		t.Fatalf("download_url = %q, want %q", payload.DownloadURL, "https://cdn.example.com/releases/sop-daemon.tar.gz")
	}
}

func TestHandleRegisterReturnsBillingStatusInPayload(t *testing.T) {
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"billing-status-user","password":"secret123","email":"billing@example.com","role":"user","subscription_plan":"starter"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected register to return %d, got %d", http.StatusCreated, rec.Code)
	}

	var payload struct {
		User struct {
			BillingStatus string `json:"billing_status"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.User.BillingStatus != "pending" {
		t.Fatalf("billing status = %q, want %q", payload.User.BillingStatus, "pending")
	}
}

func TestHandleStatusIncludesBillingDetails(t *testing.T) {
	sopbackend.SetDisableAuth(true)
	defer sopbackend.SetDisableAuth(false)

	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	cfg := sopbackend.GetConfig()
	_, err := cfg.RegisterUserWithRequest(context.Background(), sopbackend.RegisterUserRequest{
		Username:         "billing-detail-user",
		Password:         "secret123",
		Email:            "billing-detail@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("register setup user: %v", err)
	}
	sopbackend.SetConfig(cfg)

	cfg = sopbackend.GetConfig()
	if _, err := cfg.CreateCheckoutSession(context.Background(), "billing-detail-user", "starter"); err != nil {
		t.Fatalf("create checkout session: %v", err)
	}
	sopbackend.SetConfig(cfg)

	cfg = sopbackend.GetConfig()
	if err := cfg.HandleBillingWebhook(context.Background(), sopbackend.BillingWebhookEvent{EventType: "checkout.session.expired", EventID: "evt-1", UserID: "billing-detail-user", Plan: "starter"}); err != nil {
		t.Fatalf("handle billing webhook: %v", err)
	}
	sopbackend.SetConfig(cfg)

	cfg = sopbackend.GetConfig()
	user, ok, err := cfg.FindUserRecord(context.Background(), "billing-detail-user")
	if err != nil || !ok || user == nil {
		t.Fatalf("lookup refreshed user: ok=%v err=%v", ok, err)
	}
	if user.CheckoutURL == "" {
		t.Fatalf("expected checkout URL on stored user record, got %#v", user)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "billing-detail-user", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status to return %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		User struct {
			Username string `json:"username"`
			Billing  struct {
				Status           string `json:"status"`
				CheckoutURL      string `json:"checkout_url"`
				LastWebhookAt    string `json:"last_webhook_at"`
				LastWebhookEvent string `json:"last_webhook_event"`
				FailureReason    string `json:"failure_reason"`
			} `json:"billing"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.User.Username != "billing-detail-user" {
		t.Fatalf("username = %q, want %q", payload.User.Username, "billing-detail-user")
	}
	if payload.User.Billing.Status != "inactive" {
		t.Fatalf("billing status = %q, want %q", payload.User.Billing.Status, "inactive")
	}
	if payload.User.Billing.CheckoutURL == "" {
		t.Fatal("expected checkout URL in billing payload")
	}
	if payload.User.Billing.LastWebhookEvent != "checkout.session.expired" {
		t.Fatalf("last webhook event = %q, want %q", payload.User.Billing.LastWebhookEvent, "checkout.session.expired")
	}
	if payload.User.Billing.FailureReason != "checkout session expired" {
		t.Fatalf("failure reason = %q, want %q", payload.User.Billing.FailureReason, "checkout session expired")
	}
}

func TestHandleStatusReturnsCurrentAccountState(t *testing.T) {
	sopbackend.SetDisableAuth(true)
	defer sopbackend.SetDisableAuth(false)

	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)

	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"status-user","password":"secret123","email":"status@example.com","role":"user","subscription_plan":"starter"}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	mux.ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("expected setup register to return %d, got %d", http.StatusCreated, registerRec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "status-user", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status to return %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		User struct {
			Username      string `json:"username"`
			BillingStatus string `json:"billing_status"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.User.Username != "status-user" {
		t.Fatalf("username = %q, want %q", payload.User.Username, "status-user")
	}
	if payload.User.BillingStatus != "pending" {
		t.Fatalf("billing status = %q, want %q", payload.User.BillingStatus, "pending")
	}
}
