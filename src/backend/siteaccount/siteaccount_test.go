package siteaccount

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	// Disable seat limit for tests to allow unlimited user registrations
	os.Setenv("SOP_SITE_SEAT_LIMIT", "999999")
	sopbackend.SetConfig(sopbackend.Config{
		UsersStore:    sopbackend.NewUserStore(),
		SessionSecret: sopbackend.SecuredString("siteaccount-test-secret"),
	})
	os.Exit(m.Run())
}

func uniqueTestUsername(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func countTransactionBackends(t *testing.T, tx sop.Transaction) int {
	t.Helper()
	if tx == nil {
		return 0
	}
	ph := tx.GetPhasedTransaction()
	if ph == nil {
		return 0
	}
	val := reflect.ValueOf(ph)
	if !val.IsValid() || val.Kind() != reflect.Ptr || val.IsNil() {
		return 0
	}
	field := val.Elem().FieldByName("btreesBackend")
	if !field.IsValid() || field.Kind() != reflect.Slice {
		return 0
	}
	return field.Len()
}

func resetSiteAccountDBForTest(t *testing.T) {
	t.Helper()

	siteAccountDBMu.Lock()
	siteAccountDBInst = nil
	siteAccountDBMu.Unlock()

	previousSopbackendCfg := sopbackend.GetConfig()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	systemDBPath := filepath.Join(t.TempDir(), "system-db")
	userDBPath := filepath.Join(t.TempDir(), "user-db")
	if err := os.MkdirAll(systemDBPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(systemDBPath) error = %v", err)
	}
	if err := os.MkdirAll(userDBPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(userDBPath) error = %v", err)
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
		SessionSecret: sopbackend.SecuredString("siteaccount-test-secret"),
	})

	t.Cleanup(func() {
		siteAccountDBMu.Lock()
		siteAccountDBInst = nil
		siteAccountDBMu.Unlock()
		_ = os.Chdir(wd)
		sopbackend.SetConfig(previousSopbackendCfg)
	})
}

func TestResetSiteAccountDBForTestUsesStandaloneStorage(t *testing.T) {
	resetSiteAccountDBForTest(t)

	db, err := getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}

	opts := db.Options()
	if opts.Type != sop.Standalone {
		t.Fatalf("db type = %v, want %v", opts.Type, sop.Standalone)
	}
}

func TestGetSiteAccountDBUsesConfiguredUserDatabase(t *testing.T) {
	resetSiteAccountDBForTest(t)

	systemDBPath := filepath.Join(t.TempDir(), "system-db")
	userDBPath := filepath.Join(t.TempDir(), "user-db")
	if err := os.MkdirAll(systemDBPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(systemDBPath) error = %v", err)
	}
	if err := os.MkdirAll(userDBPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(userDBPath) error = %v", err)
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
		SessionSecret: sopbackend.SecuredString("siteaccount-test-secret"),
	})

	db, err := getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}

	opts := db.Options()
	if len(opts.StoresFolders) == 0 {
		t.Fatal("expected site account database options to contain a store folder")
	}
	if got := opts.StoresFolders[0]; got != userDBPath {
		t.Fatalf("expected site account DB to use user database path %q, got %q", userDBPath, got)
	}
}

func TestHandleRegisterRequiresHumanProof(t *testing.T) {
	resetSiteAccountDBForTest(t)
	SetConfig(NewConfig())
	body := `{"username":"human-proof-user","password":"secret123","email":"human-proof@example.com","role":"user","subscription_plan":"starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	HandleRegister(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("HandleRegister() status = %d, want %d, body=%s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
}

func TestHandleRegisterStoresFullName(t *testing.T) {
	resetSiteAccountDBForTest(t)
	SetConfig(NewConfig())

	challengeRecorder := httptest.NewRecorder()
	challenge := issueHumanProofChallenge(challengeRecorder)
	if challenge == "" {
		t.Fatal("issueHumanProofChallenge() returned an empty challenge")
	}
	cookies := challengeRecorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("issueHumanProofChallenge() did not set a challenge cookie")
	}

	body := fmt.Sprintf(`{"username":"full-name-user","password":"secret123","email":"full-name@example.com","role":"user","subscription_plan":"starter","full_name":"Ada Lovelace","human_proof":"%s"}`, challenge)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookies[0])
	rr := httptest.NewRecorder()

	HandleRegister(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("HandleRegister() status = %d, want %d, body=%s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	cfg := GetConfig()
	user, ok, err := cfg.FindUserRecord(context.Background(), "full-name-user")
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || user == nil {
		t.Fatal("expected registered user to be retrievable")
	}
	if user.FullName != "Ada Lovelace" {
		t.Fatalf("FullName = %q, want %q", user.FullName, "Ada Lovelace")
	}
}

func TestHandleHumanProofChallengeReturnsCaptchaPrompt(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/human-proof/challenge", nil)
	rr := httptest.NewRecorder()

	HandleHumanProofChallenge(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleHumanProofChallenge() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var payload struct {
		Challenge string `json:"challenge"`
		Prompt    string `json:"prompt"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode challenge payload: %v", err)
	}
	if payload.Challenge == "" {
		t.Fatal("expected a challenge token in the response")
	}
	if payload.Prompt == "" {
		t.Fatal("expected a captcha prompt in the response")
	}
}

func TestValidateHumanProofAllowsReuseBeforeExpiry(t *testing.T) {
	rr := httptest.NewRecorder()
	challenge := issueHumanProofChallenge(rr)
	if challenge == "" {
		t.Fatal("issueHumanProofChallenge() returned an empty challenge")
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("issueHumanProofChallenge() did not set a challenge cookie")
	}

	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/register", nil)
	req1.AddCookie(cookies[0])
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/register", nil)
	req2.AddCookie(cookies[0])

	if !validateHumanProof(req1, challenge) {
		t.Fatal("first validation should succeed")
	}
	if !validateHumanProof(req2, challenge) {
		t.Fatal("second validation should also succeed before the challenge expires")
	}
}

func TestHandleChangePasswordUpdatesStoredPassword(t *testing.T) {
	resetSiteAccountDBForTest(t)
	SetConfig(NewConfig())
	cfg := GetConfig()
	username := uniqueTestUsername(t, "site-password-change")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "old-secret-123",
		Email:            "password-change@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	body := `{"current_password":"old-secret-123","new_password":"new-secret-456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	rr := httptest.NewRecorder()

	HandleChangePassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleChangePassword() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	updated, ok, err := cfg.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || updated == nil {
		t.Fatal("FindUserRecord() did not find updated user")
	}
	if updated.PasswordHash == "" {
		t.Fatal("expected password hash to be stored")
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("new-secret-456")) != nil {
		t.Fatal("expected new password to be accepted")
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("old-secret-123")) == nil {
		t.Fatal("expected old password to be rejected after change")
	}
}

func TestHandleStatusDoesNotFailWhenPendingBillingSyncIsUnavailable(t *testing.T) {
	resetSiteAccountDBForTest(t)
	t.Setenv("STRIPE_SECRET_KEY", "")
	cfg := NewConfig()
	SetConfig(cfg)
	defer SetConfig(nil)

	username := uniqueTestUsername(t, "site-status-pending-sync")
	user := UserRecord{
		Username:         username,
		Role:             "user",
		Email:            "pending-sync@example.com",
		SubscriptionPlan: "starter",
		BillingStatus:    "pending",
	}
	if err := cfg.UpsertUserRecord(context.Background(), &user); err != nil {
		t.Fatalf("UpsertUserRecord() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	rr := httptest.NewRecorder()

	HandleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleStatus() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "pending") {
		t.Fatalf("expected pending billing status to remain visible, got %s", rr.Body.String())
	}
}

func TestHandleStatusFallsBackGracefullyWhenUserStoreCannotBeOpened(t *testing.T) {
	resetSiteAccountDBForTest(t)
	invalidPath := filepath.Join(t.TempDir(), "invalid-db-path")
	if err := os.WriteFile(invalidPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	previousCfg := sopbackend.GetConfig()
	defer sopbackend.SetConfig(previousCfg)

	sopbackend.SetConfig(sopbackend.Config{
		Mode: "standalone",
		SystemDB: &sopbackend.DatabaseConfig{
			Name: sopbackend.SystemDBName,
			Path: filepath.Join(t.TempDir(), "system-db"),
			Mode: "standalone",
		},
		Databases: []sopbackend.DatabaseConfig{{
			Name: "userdb",
			Path: invalidPath,
			Mode: "standalone",
		}},
		UsersStore:    sopbackend.NewUserStore(),
		SessionSecret: sopbackend.SecuredString("siteaccount-test-secret"),
	})

	cfg := NewConfig()
	SetConfig(cfg)
	defer SetConfig(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "some-user", Roles: []string{sop.RoleUser}}))
	rr := httptest.NewRecorder()

	HandleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleStatus() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "config_provisioned") {
		t.Fatalf("expected fallback status payload, got %s", rr.Body.String())
	}
}

func TestHandleStatusAcceptsSessionCookieWithoutConfigFile(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-status-cookie")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "status-cookie@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	accessToken, _, err := sopbackend.CreateSession(context.Background(), username, "user")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "sop_access_token", Value: accessToken})
	rr := httptest.NewRecorder()

	withSiteAuth(HandleStatus)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleStatus() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestHandleStatusFindsSharedAuthUserWhenUsersStoreIsNotPreinitialized(t *testing.T) {
	resetSiteAccountDBForTest(t)

	sharedDir := t.TempDir()
	systemDBPath := filepath.Join(sharedDir, "system-db")
	userDBPath := filepath.Join(sharedDir, "user-db")
	if err := os.MkdirAll(systemDBPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(systemDBPath) error = %v", err)
	}
	if err := os.MkdirAll(userDBPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(userDBPath) error = %v", err)
	}

	sharedCfg := sopbackend.Config{
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
		SessionSecret: sopbackend.SecuredString("siteaccount-test-secret"),
	}
	sopbackend.SetConfig(sharedCfg)

	sharedCfgValue := sopbackend.GetConfig()
	if _, err := (&sharedCfgValue).RegisterUserWithRequest(context.Background(), sopbackend.RegisterUserRequest{
		Username: "root",
		Password: "secret123",
		Email:    "root@example.com",
		Role:     sop.RoleSystemAdmin,
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}
	sopbackend.SetConfig(sharedCfgValue)

	cfg := NewConfig()
	SetConfig(cfg)
	defer SetConfig(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}))
	rr := httptest.NewRecorder()

	HandleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleStatus() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"username":"root"`) {
		t.Fatalf("expected authenticated username in status response, got %s", rr.Body.String())
	}
}

func TestHandlePingExtendsPendingBillingHeartbeat(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-ping")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "ping@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/ping", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	rr := httptest.NewRecorder()

	HandlePing(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandlePing() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	updated, ok, err := cfg.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || updated == nil {
		t.Fatal("FindUserRecord() did not find updated user")
	}
	if updated.LastPingAt.IsZero() {
		t.Fatal("expected ping timestamp to be recorded")
	}
	if updated.PendingCheckoutPingDeadline.IsZero() {
		t.Fatal("expected pending checkout ping deadline to be extended")
	}
	if updated.PendingCheckoutPingDeadline.Before(updated.LastPingAt) {
		t.Fatalf("expected deadline %s to be after last ping %s", updated.PendingCheckoutPingDeadline, updated.LastPingAt)
	}
}

func TestHandleRecoverPendingCheckoutUsesCheckoutSessionID(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-recover")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "recover@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	user, ok, err := cfg.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || user == nil {
		t.Fatal("FindUserRecord() did not find created user")
	}
	user.BillingStatus = "pending"
	user.PendingCheckoutSessionID = "pending-checkout-123"
	user.StripeCheckoutSessionID = "cs_test_recover"
	user.CheckoutURL = "https://example.com/checkout"
	if err := cfg.UpsertUserRecord(context.Background(), user); err != nil {
		t.Fatalf("UpsertUserRecord() error = %v", err)
	}

	body := `{"checkout_session_id":"pending-checkout-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/pending-checkout/recover", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	HandleRecoverPendingCheckout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleRecoverPendingCheckout() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestHandleRecoverPendingCheckoutReturnsOkForUnknownCheckoutSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/pending-checkout/recover", strings.NewReader(`{"checkout_session_id":"missing-checkout"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	HandleRecoverPendingCheckout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleRecoverPendingCheckout() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected success response body, got %s", rr.Body.String())
	}
}

func TestHandleCheckoutSessionAcceptsStripeProofWithoutHumanProof(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-checkout-stripe")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "stripe-checkout@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	body := `{"plan":"starter","stripe_payment_id":"pi_123","stripe_session_id":"cs_123","stripe_customer_id":"cus_123","stripe_subscription_id":"sub_123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/checkout", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	rr := httptest.NewRecorder()

	HandleCheckoutSession(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleCheckoutSession() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestHandleCheckoutSessionRejectsActiveSubscriptionUser(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-active-sub")
	user, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "active-sub@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	// Activate the user's subscription
	user.BillingStatus = "active"
	user.StripeSubID = "sub_existing_123"
	if err := cfg.UpsertUserRecord(context.Background(), user); err != nil {
		t.Fatalf("UpsertUserRecord() error = %v", err)
	}

	// Attempt to create a new checkout session
	body := `{"plan":"starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/checkout", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: username, Roles: []string{sop.RoleUser}}))
	rr := httptest.NewRecorder()

	HandleCheckoutSession(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("HandleCheckoutSession() status = %d, want %d (Conflict), body=%s", rr.Code, http.StatusConflict, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "already have an active subscription") {
		t.Fatalf("expected 'already have an active subscription' error, got %s", rr.Body.String())
	}
}

func TestCheckoutRouteAllowsGuestIdentityWithoutAuthToken(t *testing.T) {
	resetSiteAccountDBForTest(t)
	cfg := NewConfig()
	username := uniqueTestUsername(t, "guest-checkout")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "guest-checkout@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	challenge := "test-guest-checkout-challenge"
	humanProofChallengesMu.Lock()
	humanProofChallenges[challenge] = time.Now().Add(5 * time.Minute)
	humanProofChallengesMu.Unlock()

	body := fmt.Sprintf(`{"plan":"starter","name":"%s","email":"guest-checkout@example.com","human_proof":"%s"}`, username, challenge)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/checkout", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "sop_human_challenge", Value: challenge})
	rr := httptest.NewRecorder()

	withSiteAuthOptional(HandleCheckoutSession)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("checkout route status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected success response body, got %s", rr.Body.String())
	}
}

func TestHandleStatusReturnsAuthenticatedUserEvenWithoutConfigFile(t *testing.T) {
	resetSiteAccountDBForTest(t)
	cfg := NewConfig()
	username := uniqueTestUsername(t, "status-without-config")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "status-without-config@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	accessToken, _, err := sopbackend.CreateSession(context.Background(), username, sop.RoleUser)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "sop_access_token", Value: accessToken})
	rr := httptest.NewRecorder()

	HandleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleStatus() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"username":"`+username+`"`) {
		t.Fatalf("expected authenticated username in status response, got %s", rr.Body.String())
	}
}

func TestHandleStatusMarksSystemAdminAliasAsAdmin(t *testing.T) {
	resetSiteAccountDBForTest(t)
	cfg := NewConfig()
	username := uniqueTestUsername(t, "status-system-admin")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "status-system-admin@example.com",
		Role:             "system-admin",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	accessToken, _, err := sopbackend.CreateSession(context.Background(), username, "system-admin")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "sop_access_token", Value: accessToken})
	rr := httptest.NewRecorder()

	HandleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleStatus() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var payload struct {
		User struct {
			CanAccessDataManager bool   `json:"can_access_data_manager"`
			Role                 string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode status payload: %v", err)
	}
	if !payload.User.CanAccessDataManager {
		t.Fatalf("expected system-admin alias to be surfaced as admin, got payload=%s", rr.Body.String())
	}
	if payload.User.Role != sop.RoleSystemAdmin {
		t.Fatalf("expected normalized role %q, got %q", sop.RoleSystemAdmin, payload.User.Role)
	}
}

func TestHandleLogoutRevokesAccessAndRefreshTokens(t *testing.T) {
	resetSiteAccountDBForTest(t)
	accessToken, refreshToken, err := sopbackend.CreateSession(context.Background(), "logout-user", sop.RoleUser)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "sop_access_token", Value: accessToken})
	req.AddCookie(&http.Cookie{Name: "sop_refresh_token", Value: refreshToken})
	rr := httptest.NewRecorder()

	HandleLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HandleLogout() status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if _, err := sopbackend.ValidateSessionToken(context.Background(), accessToken); err == nil {
		t.Fatal("expected access token to be invalid after logout")
	}
	if _, _, err := sopbackend.RefreshSession(context.Background(), refreshToken); err == nil {
		t.Fatal("expected refresh token to be invalid after logout")
	}
}

// Note: Login is now handled by sopbackend at /api/auth/login.
// The site no longer has a duplicate login handler - sopbackend's handler
// automatically uses the site's btree user store via the installed override.

func TestRegisterUserWithRequestRejectsWhenSeatLimitReached(t *testing.T) {
	t.Setenv("SOP_SITE_SEAT_LIMIT", "1")
	resetSiteAccountDBForTest(t)

	cfg := NewConfig()
	firstUsername := uniqueTestUsername(t, "site-seat-limit-first")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         firstUsername,
		Password:         "secret123",
		Email:            "seat-limit-first@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("first RegisterUserWithRequest() error = %v", err)
	}

	secondUsername := uniqueTestUsername(t, "site-seat-limit-second")
	_, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         secondUsername,
		Password:         "secret123",
		Email:            "seat-limit-second@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err == nil {
		t.Fatal("expected second registration to fail when the seat limit is reached")
	}
	if !strings.Contains(err.Error(), "early-access seats") {
		t.Fatalf("expected seat-limit message, got %v", err)
	}
}

func TestRegisterUserWithRequestRecoversWhenSharedAuthUserAlreadyExists(t *testing.T) {
	resetSiteAccountDBForTest(t)

	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-recover-user")
	sharedCfg := sopbackend.GetConfig()
	if _, err := sharedCfg.RegisterUserWithRequest(context.Background(), sopbackend.RegisterUserRequest{
		Username: username,
		Password: "secret123",
		Email:    "recover@example.com",
		Role:     "user",
	}); err != nil {
		t.Fatalf("pre-seeding shared auth user failed: %v", err)
	}

	user, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "recover@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}
	if user == nil {
		t.Fatal("RegisterUserWithRequest() returned nil user")
	}
	if _, ok, err := cfg.FindUserRecord(context.Background(), username); err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	} else if !ok {
		t.Fatal("expected site account to be present after recovering from shared-auth duplication")
	}
}

func TestRegisterUserWithRequestRepairsExistingAccountWithoutHash(t *testing.T) {
	resetSiteAccountDBForTest(t)
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-repair-user")

	// Seed a shared-auth user without a password hash so RegisterUserWithRequest can repair it.
	sharedCfg := sopbackend.GetConfig()
	_, err := sharedCfg.RegisterUserWithRequest(context.Background(), sopbackend.RegisterUserRequest{
		Username: username,
		Password: "secret123",
		Email:    "repair@example.com",
		Role:     "user",
	})
	if err != nil {
		t.Fatalf("pre-seeding shared auth user failed: %v", err)
	}
	sopbackend.SetConfig(sharedCfg)

	user, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "repair@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}
	if user == nil {
		t.Fatal("RegisterUserWithRequest() returned nil user")
	}
	if strings.TrimSpace(user.PasswordHash) == "" {
		t.Fatal("expected RegisterUserWithRequest() to repair the missing password hash")
	}
}

func TestUnregisterUserWithRequestRemovesBillingRecordsAndUserStore(t *testing.T) {
	resetSiteAccountDBForTest(t)
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-unregister-billing")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "billing-unregister@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	db, err := getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(context.Background(), sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	subscriptionStore, err := cfg.ensureSubscriptionStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	paymentStore, err := cfg.ensurePaymentStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	if err := subscriptionStore.Upsert(context.Background(), []SubscriptionRecord{{ID: "sub-1", UserID: username, PlanCode: "starter", Status: "active"}}); err != nil {
		t.Fatalf("subscriptionStore.Upsert() error = %v", err)
	}
	if err := paymentStore.Upsert(context.Background(), []PaymentRecord{{ID: "pay-1", UserID: username, SubscriptionID: "sub-1", Amount: 500, Currency: "USD", Status: "succeeded"}}); err != nil {
		t.Fatalf("paymentStore.Upsert() error = %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}

	if err := cfg.UnregisterUserWithRequest(context.Background(), UnregisterUserRequest{Username: username}); err != nil {
		t.Fatalf("UnregisterUserWithRequest() error = %v", err)
	}

	if _, ok, err := cfg.FindUserRecord(context.Background(), username); err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	} else if ok {
		t.Fatalf("expected user %q to be removed", username)
	}
	if len(cfg.Subscriptions) != 0 {
		t.Fatalf("cfg.Subscriptions = %v, want empty slice", cfg.Subscriptions)
	}
	if len(cfg.Payments) != 0 {
		t.Fatalf("cfg.Payments = %v, want empty slice", cfg.Payments)
	}

	db, err = getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB(second) error = %v", err)
	}
	tx, err = db.BeginTransaction(context.Background(), sop.ForReading, -1)
	if err != nil {
		t.Fatalf("BeginTransaction(read) error = %v", err)
	}
	subscriptionStore, err = cfg.ensureSubscriptionStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore(read) error = %v", err)
	}
	paymentStore, err = cfg.ensurePaymentStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensurePaymentStore(read) error = %v", err)
	}
	if subscriptions, err := subscriptionStore.List(context.Background()); err != nil {
		t.Fatalf("subscriptionStore.List() error = %v", err)
	} else if len(subscriptions) != 0 {
		t.Fatalf("subscriptionStore.List() = %v, want empty slice", subscriptions)
	}
	if payments, err := paymentStore.List(context.Background()); err != nil {
		t.Fatalf("paymentStore.List() error = %v", err)
	} else if len(payments) != 0 {
		t.Fatalf("paymentStore.List() = %v, want empty slice", payments)
	}
	_ = tx.Rollback(context.Background())
}

func TestUpdateSubscriptionPlanSyncsToSharedBackendAuthUser(t *testing.T) {
	resetSiteAccountDBForTest(t)
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-shared-auth-sync")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "shared-auth-sync@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	updated, err := cfg.UpdateSubscriptionPlanWithRequest(context.Background(), UpdateSubscriptionPlanRequest{
		Username:             username,
		SubscriptionPlan:     "hq",
		SubscriptionFeatures: []string{"copilot", "shared-memory"},
		RequestOrigin:        "site",
	})
	if err != nil {
		t.Fatalf("UpdateSubscriptionPlanWithRequest() error = %v", err)
	}
	if updated == nil || updated.SubscriptionPlan != "hq" {
		t.Fatalf("UpdateSubscriptionPlanWithRequest() returned %+v", updated)
	}

	sharedCfg := sopbackend.GetConfig()
	sharedUser, ok, err := sharedCfg.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("shared cfg FindUserRecord() error = %v", err)
	}
	if !ok || sharedUser == nil {
		t.Fatalf("shared cfg FindUserRecord() did not find %q", username)
	}
	// NOTE: sopbackend only tracks auth fields now; billing/subscription is site-backend-only.
	// Verify basic auth fields only.
	if sharedUser.Username != username {
		t.Fatalf("shared user username = %q, want %q", sharedUser.Username, username)
	}
}

func TestCreateCheckoutSessionUsesStripeHostedCheckoutURL(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-stripe-checkout")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "stripe-url@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	var gotBody string
	stripeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cs_test_123","url":"https://checkout.stripe.com/c/pay/test_session","status":"open"}`))
	}))
	defer stripeServer.Close()

	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_PRICE_ID", "price_test_123")
	t.Setenv("STRIPE_SUCCESS_URL", "https://example.com/success")
	t.Setenv("STRIPE_CANCEL_URL", "https://example.com/cancel")
	t.Setenv("STRIPE_API_BASE_URL", stripeServer.URL)

	session, err := cfg.CreateCheckoutSession(context.Background(), username, "starter")
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if session == nil || session.URL == "" {
		t.Fatal("CreateCheckoutSession() returned empty checkout URL")
	}
	if !strings.HasPrefix(session.URL, "https://checkout.stripe.com/") {
		t.Fatalf("CreateCheckoutSession() URL = %q, want Stripe-hosted checkout URL", session.URL)
	}
	if !strings.Contains(gotBody, "/v1/checkout/sessions") {
		t.Fatalf("expected Stripe checkout endpoint to be called, got %q", gotBody)
	}
}

func TestCreateCheckoutSessionReturnsConfigurationErrorWhenStripeIsMissing(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-stripe-missing")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "stripe-missing@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	t.Setenv("STRIPE_SECRET_KEY", "")
	t.Setenv("STRIPE_PRICE_ID", "")

	_, err := cfg.CreateCheckoutSession(context.Background(), username, "starter")
	if err == nil {
		t.Fatal("CreateCheckoutSession() error = nil, want configuration error")
	}
	if !strings.Contains(err.Error(), "stripe secret key not configured") {
		t.Fatalf("CreateCheckoutSession() error = %v, want configuration error", err)
	}
}

func TestCreateCheckoutSessionUsesProvidedEmailForStripeCheckout(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-stripe-email")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	var payloadBody string
	stripeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		payloadBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cs_test_123","url":"https://checkout.stripe.com/c/pay/test_session","status":"open"}`))
	}))
	defer stripeServer.Close()

	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_PRICE_ID", "price_test_123")
	t.Setenv("STRIPE_SUCCESS_URL", "https://example.com/success")
	t.Setenv("STRIPE_CANCEL_URL", "https://example.com/cancel")
	t.Setenv("STRIPE_API_BASE_URL", stripeServer.URL)

	_, err := cfg.CreateCheckoutSession(context.Background(), username, "starter", "stripe-user@example.com")
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if !strings.Contains(payloadBody, "customer_email=stripe-user%40example.com") {
		t.Fatalf("expected Stripe checkout request to use the provided email, got %q", payloadBody)
	}
}

func TestCreateCheckoutSessionRejectsProductIDInStripePriceConfig(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-stripe-product-id")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "stripe-product@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_PRICE_ID", "prod_UpNZqXLxm9mHMv")
	t.Setenv("STRIPE_SUCCESS_URL", "https://example.com/success")
	t.Setenv("STRIPE_CANCEL_URL", "https://example.com/cancel")

	_, err := cfg.CreateCheckoutSession(context.Background(), username, "starter")
	if err == nil {
		t.Fatal("CreateCheckoutSession() error = nil, want price-id validation error")
	}
	if !strings.Contains(err.Error(), "price id") || !strings.Contains(err.Error(), "price_") {
		t.Fatalf("CreateCheckoutSession() error = %v, want Stripe price-id validation error", err)
	}
}

func TestCreateCheckoutSessionUsesConfiguredStripeAPI(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-stripe-api")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "stripe-api@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	var gotAuthHeader, gotBody string
	stripeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotBody = r.URL.String()
		payload, _ := io.ReadAll(r.Body)
		if strings.Contains(string(payload), "customer_email") {
			gotBody += "|" + string(payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cs_test_123","url":"https://checkout.stripe.com/c/pay/test_session","status":"open"}`))
	}))
	defer stripeServer.Close()

	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_PRICE_ID", "price_test_123")
	t.Setenv("STRIPE_SUCCESS_URL", "https://example.com/success")
	t.Setenv("STRIPE_CANCEL_URL", "https://example.com/cancel")
	t.Setenv("STRIPE_API_BASE_URL", stripeServer.URL)

	session, err := cfg.CreateCheckoutSession(context.Background(), username, "starter")
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if session == nil || session.URL != "https://checkout.stripe.com/c/pay/test_session" {
		t.Fatalf("CreateCheckoutSession() URL = %q, want Stripe API provided URL", session.URL)
	}
	if !strings.HasPrefix(gotAuthHeader, "Bearer ") {
		t.Fatalf("expected Stripe Authorization header to be set, got %q", gotAuthHeader)
	}
	if !strings.Contains(gotBody, "/v1/checkout/sessions") {
		t.Fatalf("expected Stripe checkout endpoint to be called, got %q", gotBody)
	}
	if !strings.Contains(gotBody, "customer_email=stripe-api%40example.com") {
		t.Fatalf("expected Stripe checkout request to use the account email, got %q", gotBody)
	}
}

func TestRegisterUserWithRequestPersistsUserAndBillingState(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-account-user")
	user, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "site@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}
	if user == nil {
		t.Fatal("RegisterUserWithRequest() returned nil user")
	}
	if user.BillingStatus != "pending" {
		t.Fatalf("BillingStatus = %q, want %q", user.BillingStatus, "pending")
	}

	found, ok, err := cfg.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || found == nil {
		t.Fatal("FindUserRecord() did not find created user")
	}
	if found.Username != username {
		t.Fatalf("FindUserRecord() username = %q, want %q", found.Username, username)
	}
}

func TestRegisterUserWithRequestPersistsHumanOrigin(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-account-human")
	user, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "human@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
		RequestOrigin:    "human",
	})
	if err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}
	if user == nil {
		t.Fatal("RegisterUserWithRequest() returned nil user")
	}
	if user.RequestOrigin != "human" {
		t.Fatalf("RequestOrigin = %q, want %q", user.RequestOrigin, "human")
	}
}

func TestRegisterUserWithRequestSeedsDefaultProductCatalog(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "site-account-products")
	_, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "products@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	})
	if err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	db, err := getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(context.Background(), sop.ForReading, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(context.Background())

	store, err := newProductStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("newProductStore() error = %v", err)
	}
	products, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("store.List() error = %v", err)
	}
	if len(products) == 0 {
		t.Fatal("expected a default product to be seeded")
	}
	if products[0].Code != "ai-copilot-starter" {
		t.Fatalf("unexpected seeded product code = %q", products[0].Code)
	}
}

func TestPrimaryStoresReuseSameTreeHandlePerTransaction(t *testing.T) {
	cfg := NewConfig()
	ctx := context.Background()

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(ctx)

	before := countTransactionBackends(t, tx)
	if _, err := cfg.ensureBillingInfoStore(ctx, tx); err != nil {
		t.Fatalf("ensureBillingInfoStore() error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the first billing-info store open to add one backend entry, got %d", got)
	}
	if _, err := cfg.ensureBillingInfoStore(ctx, tx); err != nil {
		t.Fatalf("ensureBillingInfoStore(second) error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the billing-info store to be reused without adding a second backend entry, got %d", got)
	}

	before = countTransactionBackends(t, tx)
	if _, err := cfg.ensureSubscriptionStore(ctx, tx); err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the first subscription-store open to add one backend entry, got %d", got)
	}
	if _, err := cfg.ensureSubscriptionStore(ctx, tx); err != nil {
		t.Fatalf("ensureSubscriptionStore(second) error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the subscription store to be reused without adding a second backend entry, got %d", got)
	}

	before = countTransactionBackends(t, tx)
	if _, err := cfg.ensurePaymentStore(ctx, tx); err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the first payment-store open to add one backend entry, got %d", got)
	}
	if _, err := cfg.ensurePaymentStore(ctx, tx); err != nil {
		t.Fatalf("ensurePaymentStore(second) error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the payment store to be reused without adding a second backend entry, got %d", got)
	}
}

func TestIndexStoreReusesSameTreeHandlePerTransaction(t *testing.T) {
	cfg := NewConfig()
	ctx := context.Background()

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(ctx)

	before := countTransactionBackends(t, tx)
	if _, err := cfg.ensureUserSubscriptionIndexStore(ctx, tx); err != nil {
		t.Fatalf("ensureUserSubscriptionIndexStore() error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the first subscription-index open to add one backend entry, got %d", got)
	}
	if _, err := cfg.ensureUserSubscriptionIndexStore(ctx, tx); err != nil {
		t.Fatalf("ensureUserSubscriptionIndexStore(second) error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the subscription index store to be reused without adding a second backend entry, got %d", got)
	}

	before = countTransactionBackends(t, tx)
	if _, err := cfg.ensureUserPaymentIndexStore(ctx, tx); err != nil {
		t.Fatalf("ensureUserPaymentIndexStore() error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the first payment-index open to add one backend entry, got %d", got)
	}
	if _, err := cfg.ensureUserPaymentIndexStore(ctx, tx); err != nil {
		t.Fatalf("ensureUserPaymentIndexStore(second) error = %v", err)
	}
	if got := countTransactionBackends(t, tx); got != before+1 {
		t.Fatalf("expected the payment index store to be reused without adding a second backend entry, got %d", got)
	}
}

func TestSimpleRelationshipIndexStoreHelperMaintainsLinks(t *testing.T) {
	ctx := context.Background()
	db, err := getSiteAccountDB(ctx)
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(ctx)

	store, err := newSimpleRelationshipIndexStore(ctx, tx, "simple-bridge-helper")
	if err != nil {
		t.Fatalf("newSimpleRelationshipIndexStore() error = %v", err)
	}
	if err := store.upsert(ctx, []relationshipIndexEntry{{username: "ALICE", entityID: "sub-1"}, {username: "alice", entityID: "sub-2"}, {username: "alice", entityID: "sub-1"}}); err != nil {
		t.Fatalf("upsert() error = %v", err)
	}

	ids, err := store.listIDs(ctx, "alice")
	if err != nil {
		t.Fatalf("listIDs() error = %v", err)
	}
	if len(ids) != 2 || !containsString(ids, "sub-1") || !containsString(ids, "sub-2") {
		t.Fatalf("listIDs() = %v, want a deduplicated pair containing sub-1 and sub-2", ids)
	}

	if err := store.delete(ctx, []UserEntityIDs{{Username: "alice", EntityIDs: []string{"sub-2"}}}); err != nil {
		t.Fatalf("delete() error = %v", err)
	}
	ids, err = store.listIDs(ctx, "alice")
	if err != nil {
		t.Fatalf("listIDs(after delete) error = %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"sub-1"}) {
		t.Fatalf("listIDs(after delete) = %v, want %v", ids, []string{"sub-1"})
	}
}

func TestStoreBatchGetAndDeleteSupportSlices(t *testing.T) {
	cfg := NewConfig()
	ctx := context.Background()

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(ctx)

	username := uniqueTestUsername(t, "batch-store-user")
	billingStore, err := cfg.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureBillingInfoStore() error = %v", err)
	}
	if err := billingStore.Upsert(ctx, []BillingInfo{{Username: username, Status: "active"}}); err != nil {
		t.Fatalf("UpsertBatch billing info error = %v", err)
	}
	billingInfos, err := billingStore.Fetch(ctx, []string{username, "missing-user"})
	if err != nil {
		t.Fatalf("Get(billingInfos) error = %v", err)
	}
	if len(billingInfos) != 1 || billingInfos[0].Username != username {
		t.Fatalf("Get(billingInfos) = %+v, want a single record for %q", billingInfos, username)
	}

	subscriptionStore, err := cfg.ensureSubscriptionStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	subAID := fmt.Sprintf("sub-batch-a-%d", time.Now().UnixNano())
	subBID := fmt.Sprintf("sub-batch-b-%d", time.Now().UnixNano())
	if err := subscriptionStore.Upsert(ctx, []SubscriptionRecord{{ID: subAID, UserID: username, PlanCode: "starter", Status: "active"}, {ID: subBID, UserID: username, PlanCode: "pro", Status: "active"}}); err != nil {
		t.Fatalf("UpsertBatch subscriptions error = %v", err)
	}
	foundSubscriptions, err := subscriptionStore.Fetch(ctx, []string{subAID, "missing-sub", subBID})
	if err != nil {
		t.Fatalf("Get(subscriptions) error = %v", err)
	}
	if len(foundSubscriptions) != 2 {
		t.Fatalf("Get(subscriptions) len = %d, want 2", len(foundSubscriptions))
	}
	if err := subscriptionStore.Delete(ctx, []string{subAID, "missing-sub"}); err != nil {
		t.Fatalf("Delete(subscriptions) error = %v", err)
	}
	remainingSubscriptions, err := subscriptionStore.Fetch(ctx, []string{subAID, subBID})
	if err != nil {
		t.Fatalf("Get(subscriptions after delete) error = %v", err)
	}
	if len(remainingSubscriptions) != 1 || remainingSubscriptions[0].ID != subBID {
		t.Fatalf("Get(subscriptions after delete) = %+v, want only %q", remainingSubscriptions, subBID)
	}
}

func TestUserBillingAndPaymentIndexStores(t *testing.T) {
	cfg := NewConfig()
	ctx := context.Background()

	db, err := getSiteAccountDB(ctx)
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(ctx, sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(ctx)

	username := uniqueTestUsername(t, "indexed-user")
	billingInfo := &BillingInfo{Username: username, FullName: "Indexed User", BillingStatus: "active", Status: "active"}
	billingStore, err := cfg.ensureBillingInfoStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureBillingInfoStore() error = %v", err)
	}
	if err := billingStore.Upsert(ctx, []BillingInfo{*billingInfo}); err != nil {
		t.Fatalf("UpsertBatch billing info error = %v", err)
	}

	subscriptionID := fmt.Sprintf("sub-idx-%d", time.Now().UnixNano())
	subscription := &SubscriptionRecord{ID: subscriptionID, UserID: username, PlanCode: "starter", Status: "active"}
	subscriptionStore, err := cfg.ensureSubscriptionStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	if err := subscriptionStore.Upsert(ctx, []SubscriptionRecord{*subscription}); err != nil {
		t.Fatalf("UpsertBatch subscription error = %v", err)
	}

	paymentID := fmt.Sprintf("pay-idx-%d", time.Now().UnixNano())
	payment := &PaymentRecord{ID: paymentID, UserID: username, SubscriptionID: subscriptionID, Amount: 100, Currency: "USD", Status: "succeeded"}
	paymentStore, err := cfg.ensurePaymentStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	if err := paymentStore.Upsert(ctx, []PaymentRecord{*payment}); err != nil {
		t.Fatalf("UpsertBatch payment error = %v", err)
	}

	userSubscriptionIndexStore, err := cfg.ensureUserSubscriptionIndexStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureUserSubscriptionIndexStore() error = %v", err)
	}
	secondarySubscriptionID := fmt.Sprintf("sub-idx-secondary-%d", time.Now().UnixNano())
	if err := userSubscriptionIndexStore.Upsert(ctx, []UserSubscriptionEntry{{Username: username, SubscriptionID: subscriptionID}, {Username: username, SubscriptionID: secondarySubscriptionID}}); err != nil {
		t.Fatalf("UpsertBatch user subscription index error = %v", err)
	}
	if err := userSubscriptionIndexStore.Delete(ctx, []UserEntityIDs{{Username: username, EntityIDs: []string{subscriptionID, "missing-subscription"}}}); err != nil {
		t.Fatalf("Delete(user subscription index) error = %v", err)
	}
	remainingSubscriptionIDs, err := userSubscriptionIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("ListIDs(user subscription index) error = %v", err)
	}
	if !reflect.DeepEqual(remainingSubscriptionIDs, []string{secondarySubscriptionID}) {
		t.Fatalf("remaining subscription IDs = %v, want %v", remainingSubscriptionIDs, []string{secondarySubscriptionID})
	}
	userPaymentIndexStore, err := cfg.ensureUserPaymentIndexStore(ctx, tx)
	if err != nil {
		t.Fatalf("ensureUserPaymentIndexStore() error = %v", err)
	}
	secondaryPaymentID := fmt.Sprintf("pay-idx-secondary-%d", time.Now().UnixNano())
	if err := userPaymentIndexStore.Upsert(ctx, []UserPaymentEntry{{Username: username, PaymentID: paymentID}, {Username: username, PaymentID: secondaryPaymentID}}); err != nil {
		t.Fatalf("UpsertBatch user payment index error = %v", err)
	}
	if err := userPaymentIndexStore.Delete(ctx, []UserEntityIDs{{Username: username, EntityIDs: []string{paymentID, "missing-payment"}}}); err != nil {
		t.Fatalf("Delete(user payment index) error = %v", err)
	}
	remainingPaymentIDs, err := userPaymentIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("ListIDs(user payment index) error = %v", err)
	}
	if !reflect.DeepEqual(remainingPaymentIDs, []string{secondaryPaymentID}) {
		t.Fatalf("remaining payment IDs = %v, want %v", remainingPaymentIDs, []string{secondaryPaymentID})
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	rtx, err := db.BeginTransaction(ctx, sop.ForReading, -1)
	if err != nil {
		t.Fatalf("BeginTransaction(read) error = %v", err)
	}
	defer rtx.Rollback(ctx)

	ruserSubscriptionIndexStore, err := cfg.ensureUserSubscriptionIndexStore(ctx, rtx)
	if err != nil {
		t.Fatalf("ensureUserSubscriptionIndexStore(read) error = %v", err)
	}
	subIDs, err := ruserSubscriptionIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("List subscription IDs error = %v", err)
	}
	if !reflect.DeepEqual(subIDs, []string{secondarySubscriptionID}) {
		t.Fatalf("subscription IDs = %v, want %v", subIDs, []string{secondarySubscriptionID})
	}

	ruserPaymentIndexStore, err := cfg.ensureUserPaymentIndexStore(ctx, rtx)
	if err != nil {
		t.Fatalf("ensureUserPaymentIndexStore(read) error = %v", err)
	}
	payIDs, err := ruserPaymentIndexStore.ListIDs(ctx, username)
	if err != nil {
		t.Fatalf("List payment IDs error = %v", err)
	}
	if !reflect.DeepEqual(payIDs, []string{secondaryPaymentID}) {
		t.Fatalf("payment IDs = %v, want %v", payIDs, []string{secondaryPaymentID})
	}
}

func TestSiteAccountStoresPersistAcrossFreshConfigInstances(t *testing.T) {
	cfg := NewConfig()
	username := uniqueTestUsername(t, "persisted-site-user")
	if _, err := cfg.RegisterUserWithRequest(context.Background(), RegisterUserRequest{
		Username:         username,
		Password:         "secret123",
		Email:            "persisted@example.com",
		Role:             "user",
		SubscriptionPlan: "starter",
	}); err != nil {
		t.Fatalf("RegisterUserWithRequest() error = %v", err)
	}

	db, err := getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB() error = %v", err)
	}
	tx, err := db.BeginTransaction(context.Background(), sop.ForWriting, -1)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback(context.Background())

	subscriptionID := fmt.Sprintf("sub-%d", time.Now().UnixNano())
	paymentID := fmt.Sprintf("pay-%d", time.Now().UnixNano())
	subscriptionStore, err := cfg.ensureSubscriptionStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	if err := subscriptionStore.Upsert(context.Background(), []SubscriptionRecord{{
		ID:                   subscriptionID,
		UserID:               username,
		PlanCode:             "starter",
		Status:               "active",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
		CurrentPeriodStart:   time.Now().UTC(),
		CurrentPeriodEnd:     time.Now().UTC().Add(24 * time.Hour),
		CancelAtPeriodEnd:    false,
		ProviderSubscription: "sub_provider_1",
	}}); err != nil {
		t.Fatalf("Save subscription error = %v", err)
	}

	paymentStore, err := cfg.ensurePaymentStore(context.Background(), tx)
	if err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	if err := paymentStore.Upsert(context.Background(), []PaymentRecord{{
		ID:                paymentID,
		UserID:            username,
		SubscriptionID:    subscriptionID,
		ProviderPaymentID: "pay_provider_1",
		Amount:            500,
		Currency:          "USD",
		Status:            "succeeded",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("UpsertBatch payment error = %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	reloaded := NewConfig()
	found, ok, err := reloaded.FindUserRecord(context.Background(), username)
	if err != nil {
		t.Fatalf("FindUserRecord() after reload error = %v", err)
	}
	if !ok || found == nil {
		t.Fatal("expected persisted user record to be available after reload")
	}

	db, err = getSiteAccountDB(context.Background())
	if err != nil {
		t.Fatalf("getSiteAccountDB(read) error = %v", err)
	}
	rtx, err := db.BeginTransaction(context.Background(), sop.ForReading, -1)
	if err != nil {
		t.Fatalf("BeginTransaction(read) error = %v", err)
	}
	defer rtx.Rollback(context.Background())

	subscriptionStore, err = reloaded.ensureSubscriptionStore(context.Background(), rtx)
	if err != nil {
		t.Fatalf("ensureSubscriptionStore() error = %v", err)
	}
	subscriptionsByUser, err := subscriptionStore.FetchByUser(context.Background(), []string{username})
	if err != nil {
		t.Fatalf("ListByUser subscriptions error = %v", err)
	}
	if len(subscriptionsByUser) != 1 {
		t.Fatalf("expected 1 subscription group after reload, got %d", len(subscriptionsByUser))
	}
	if len(subscriptionsByUser[0].Value) != 1 {
		t.Fatalf("expected 1 subscription after reload, got %d", len(subscriptionsByUser[0].Value))
	}

	paymentStore, err = reloaded.ensurePaymentStore(context.Background(), rtx)
	if err != nil {
		t.Fatalf("ensurePaymentStore() error = %v", err)
	}
	paymentsByUser, err := paymentStore.FetchByUser(context.Background(), []string{username})
	if err != nil {
		t.Fatalf("ListByUser payments error = %v", err)
	}
	if len(paymentsByUser) != 1 {
		t.Fatalf("expected 1 payment group after reload, got %d", len(paymentsByUser))
	}
	if len(paymentsByUser[0].Value) != 1 {
		t.Fatalf("expected 1 payment after reload, got %d", len(paymentsByUser[0].Value))
	}
}
