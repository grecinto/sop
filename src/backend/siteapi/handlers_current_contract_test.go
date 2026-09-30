package siteapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grecinto/sop-cloud/src/backend/siteaccount"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

var siteAPITestDataDir string

func TestMain(m *testing.M) {
	var err error
	siteAPITestDataDir, err = os.MkdirTemp("", "sop-siteapi-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(siteAPITestDataDir)

	systemDBPath := filepath.Join(siteAPITestDataDir, "system-db")
	userDBPath := filepath.Join(siteAPITestDataDir, "user-db")
	if err := os.MkdirAll(systemDBPath, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(userDBPath, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sopbackend.SetConfig(sopbackend.Config{
		Mode: "standalone",
		SystemDB: &sopbackend.DatabaseConfig{
			Name: sopbackend.SystemDBName,
			Path: systemDBPath,
			Mode: "standalone",
		},
		Databases: []sopbackend.DatabaseConfig{{
			Name: "userdb",
			Path: userDBPath,
			Mode: "standalone",
		}},
		UsersStore:    sopbackend.NewUserStore(),
		SessionSecret: sopbackend.SecuredString("siteapi-test-session-secret"),
	})
	siteaccount.SetConfig(siteaccount.NewConfig())
	exitCode := m.Run()
	_ = os.RemoveAll(siteAPITestDataDir)
	os.Exit(exitCode)
}

func newSiteAPIContractMux(t *testing.T) *http.ServeMux {
	t.Helper()
	previousConfig := sopbackend.GetConfig()
	previousSiteConfig := siteaccount.GetConfig()
	previousSiteMux := siteMux

	config := previousConfig
	config.UsersStore = sopbackend.NewUserStore()
	config.SessionSecret = sopbackend.SecuredString("siteapi-test-session-secret")
	config.DisableAuth = false
	sopbackend.SetConfig(config)
	siteaccount.SetConfig(siteaccount.NewConfig())

	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	RegisterHandlers(mux)
	t.Cleanup(func() {
		siteMux = previousSiteMux
		siteaccount.SetConfig(previousSiteConfig)
		sopbackend.SetConfig(previousConfig)
	})
	return mux
}

func registerSiteAPIUser(t *testing.T, mux *http.ServeMux, username string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"secret123","email":"%s@example.com","role":"user"}`, username, username)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register returned %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
}

func registerSiteAccount(t *testing.T, username string) *siteaccount.UserRecord {
	t.Helper()
	user, err := siteaccount.GetConfig().RegisterUserWithRequest(context.Background(), siteaccount.RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            username + "@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("register site account: %v", err)
	}
	return user
}

func testUsername(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("siteapi-%s-%d", strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-"), time.Now().UnixNano())
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	mux := newSiteAPIContractMux(t)
	username := testUsername(t)
	registerSiteAPIUser(t, mux, username)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"secret123","email":"duplicate@example.com","role":"user"}`, username)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate registration returned %d, want %d: %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}

	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode duplicate registration response: %v", err)
	}
	want := "username is not available (already taken)."
	if payload.Error != want || payload.Message != want {
		t.Fatalf("duplicate response = %#v, want error and message %q", payload, want)
	}
}

func TestLoginSetsSessionCookiesAndUnregisterClearsThem(t *testing.T) {
	mux := newSiteAPIContractMux(t)
	username := testUsername(t)
	registerSiteAPIUser(t, mux, username)

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/site/auth/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"secret123"}`, username)))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	mux.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login returned %d, want %d: %s", loginRecorder.Code, http.StatusOK, loginRecorder.Body.String())
	}

	cookies := loginRecorder.Result().Cookies()
	if len(cookies) < 2 {
		t.Fatalf("login returned %d cookies, want access and refresh cookies", len(cookies))
	}
	for _, name := range []string{"sop_access_token", "sop_refresh_token"} {
		found := false
		for _, cookie := range cookies {
			if cookie.Name == name && cookie.MaxAge > 0 {
				found = true
			}
		}
		if !found {
			t.Errorf("login did not set persistent %s cookie: %#v", name, cookies)
		}
	}

	unregisterRequest := httptest.NewRequest(http.MethodPost, "/api/auth/unregister", nil)
	unregisterRequest = unregisterRequest.WithContext(sop.ContextWithAuth(unregisterRequest.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	for _, cookie := range cookies {
		unregisterRequest.AddCookie(cookie)
	}
	unregisterRecorder := httptest.NewRecorder()
	mux.ServeHTTP(unregisterRecorder, unregisterRequest)
	if unregisterRecorder.Code != http.StatusOK {
		t.Fatalf("unregister returned %d, want %d: %s", unregisterRecorder.Code, http.StatusOK, unregisterRecorder.Body.String())
	}
	cleared := map[string]bool{}
	for _, cookie := range unregisterRecorder.Result().Cookies() {
		if (cookie.Name == "sop_access_token" || cookie.Name == "sop_refresh_token") && cookie.MaxAge < 0 {
			cleared[cookie.Name] = true
		}
	}
	if !cleared["sop_access_token"] || !cleared["sop_refresh_token"] {
		t.Fatalf("unregister did not clear both session cookies: %#v", unregisterRecorder.Result().Cookies())
	}
}

func TestStatusReturnsCompositeSiteAccountState(t *testing.T) {
	mux := newSiteAPIContractMux(t)
	username := testUsername(t)
	registerSiteAccount(t, username)

	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	request = request.WithContext(sop.ContextWithAuth(request.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status returned %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload struct {
		Status string `json:"status"`
		User   struct {
			Username         string `json:"username"`
			SubscriptionPlan string `json:"subscription_plan"`
			BillingStatus    string `json:"billing_status"`
			Billing          struct {
				Status string `json:"status"`
			} `json:"billing"`
			Payments      []json.RawMessage `json:"payments"`
			Subscriptions []json.RawMessage `json:"subscriptions"`
		} `json:"user"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if payload.Status != "ok" || payload.User.Username != username {
		t.Fatalf("unexpected status response: %#v", payload)
	}
	if payload.User.SubscriptionPlan != "starter" || payload.User.BillingStatus != "pending" || payload.User.Billing.Status != "pending" {
		t.Fatalf("status did not include current site billing state: %#v", payload.User)
	}
	if payload.User.Payments == nil || payload.User.Subscriptions == nil {
		t.Fatalf("status omitted payment/subscription collections: %#v", payload.User)
	}
}

func TestBillingWebhookUpdatesSiteAccountAndDownloadEntitlement(t *testing.T) {
	t.Setenv("SOP_DOWNLOAD_URL", "https://cdn.example.com/releases/sop-daemon.tar.gz")
	mux := newSiteAPIContractMux(t)
	username := testUsername(t)
	registerSiteAccount(t, username)

	webhookRequest := httptest.NewRequest(http.MethodPost, "/api/auth/webhooks/billing", strings.NewReader(fmt.Sprintf(`{"id":"evt-%s","type":"checkout.session.completed","data":{"object":{"metadata":{"username":%q,"plan":"starter"}}}}`, username, username)))
	webhookRequest.Header.Set("Content-Type", "application/json")
	webhookRecorder := httptest.NewRecorder()
	mux.ServeHTTP(webhookRecorder, webhookRequest)
	if webhookRecorder.Code != http.StatusOK {
		t.Fatalf("billing webhook returned %d, want %d: %s", webhookRecorder.Code, http.StatusOK, webhookRecorder.Body.String())
	}

	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/auth/download", nil)
	downloadRequest = downloadRequest.WithContext(sop.ContextWithAuth(downloadRequest.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	downloadRecorder := httptest.NewRecorder()
	mux.ServeHTTP(downloadRecorder, downloadRequest)
	if downloadRecorder.Code != http.StatusOK {
		t.Fatalf("entitled download returned %d, want %d: %s", downloadRecorder.Code, http.StatusOK, downloadRecorder.Body.String())
	}

	var payload struct {
		Available   bool   `json:"available"`
		DownloadURL string `json:"download_url"`
		Plan        string `json:"plan"`
	}
	if err := json.Unmarshal(downloadRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode download response: %v", err)
	}
	if !payload.Available || payload.DownloadURL != "https://cdn.example.com/releases/sop-daemon.tar.gz" || payload.Plan != "starter" {
		t.Fatalf("unexpected entitled download payload: %#v", payload)
	}
}
