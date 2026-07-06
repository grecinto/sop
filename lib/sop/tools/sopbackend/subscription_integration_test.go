package sopbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

func TestPaymentFlow_PutsAccountOnHoldAndRestoresCopilotAccess(t *testing.T) {
	setupSubscriptionIntegrationEnv(t)
	if _, err := config.RegisterUser(context.Background(), "payer", "pw123", "payer@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	prevLoadedAgents := loadedAgents
	stub := &stubSharedAskAgent{}
	loadedAgents = map[string]ai.Agent[map[string]any]{"copilot": stub}
	defer func() { loadedAgents = prevLoadedAgents }()

	if _, _, err := config.SubmitPayment(context.Background(), "payer", "starter", 5.0, "USD", "succeeded", "pay_001", "evt_001", time.Now().UTC()); err != nil {
		t.Fatalf("SubmitPayment(success) error = %v", err)
	}
	user, ok, err := config.FindUserRecord(context.Background(), "payer")
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || user == nil {
		t.Fatal("expected payer user after successful payment")
	}
	if user.BillingStatus != "active" {
		t.Fatalf("expected billing status active after success, got %q", user.BillingStatus)
	}
	authCtx := sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "payer", Roles: []string{sop.RoleUser}})
	if _, err := AskSharedAgent(authCtx, "hello from payer", nil); err != nil {
		t.Fatalf("AskSharedAgent() after success error = %v", err)
	}
	if len(stub.queries) != 1 {
		t.Fatalf("expected one Copilot ask after success, got %d", len(stub.queries))
	}

	if _, _, err := config.SubmitPayment(context.Background(), "payer", "starter", 5.0, "USD", "declined", "pay_002", "evt_002", time.Now().UTC()); err != nil {
		t.Fatalf("SubmitPayment(declined) error = %v", err)
	}
	user, ok, err = config.FindUserRecord(context.Background(), "payer")
	if err != nil {
		t.Fatalf("FindUserRecord() after decline error = %v", err)
	}
	if !ok || user == nil {
		t.Fatal("expected payer user after declined payment")
	}
	if user.BillingStatus != "on_hold" {
		t.Fatalf("expected billing status on_hold after decline, got %q", user.BillingStatus)
	}
	if !strings.Contains(strings.ToLower(user.BillingNotice), "on hold") {
		t.Fatalf("expected billing notice to mention on hold, got %q", user.BillingNotice)
	}
	if _, err := AskSharedAgent(authCtx, "this should be blocked", nil); err == nil || !strings.Contains(err.Error(), "on hold") {
		t.Fatalf("expected AskSharedAgent() to be blocked by on-hold billing, got %v", err)
	}
	if len(stub.queries) != 1 {
		t.Fatalf("expected no additional Copilot ask while account is on hold, got %d", len(stub.queries))
	}

	if _, _, err := config.SubmitPayment(context.Background(), "payer", "starter", 5.0, "USD", "succeeded", "pay_003", "evt_003", time.Now().UTC()); err != nil {
		t.Fatalf("SubmitPayment(recovery) error = %v", err)
	}
	user, ok, err = config.FindUserRecord(context.Background(), "payer")
	if err != nil {
		t.Fatalf("FindUserRecord() after recovery error = %v", err)
	}
	if !ok || user == nil {
		t.Fatal("expected payer user after recovery")
	}
	if user.BillingStatus != "active" {
		t.Fatalf("expected billing status active after recovery, got %q", user.BillingStatus)
	}
	if _, err := AskSharedAgent(authCtx, "restored access", nil); err != nil {
		t.Fatalf("AskSharedAgent() after recovery error = %v", err)
	}
	if len(stub.queries) != 2 {
		t.Fatalf("expected Copilot ask to resume after recovery, got %d", len(stub.queries))
	}
}

func TestHandleBillingPaymentAPI_PersistsSuccessAndDeclineLifecycle(t *testing.T) {
	setupSubscriptionIntegrationEnv(t)
	if _, err := config.RegisterUser(context.Background(), "billing-api", "pw123", "billing-api@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	body := strings.NewReader(`{"plan":"starter","amount":5,"currency":"USD","status":"succeeded","provider_payment_id":"pay_api_success","event_id":"evt_api_success"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/billing/payment", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "billing-api", Roles: []string{sop.RoleUser}}))
	w := httptest.NewRecorder()
	handleBillingPayment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleBillingPayment(success) status = %d, body=%s", w.Code, w.Body.String())
	}
	var successPayload struct {
		User struct {
			BillingStatus string `json:"billing_status"`
			BillingNotice string `json:"billing_notice"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &successPayload); err != nil {
		t.Fatalf("json.Unmarshal() success error = %v", err)
	}
	if successPayload.User.BillingStatus != "active" {
		t.Fatalf("expected active billing after success, got %q", successPayload.User.BillingStatus)
	}

	declineBody := strings.NewReader(`{"plan":"starter","amount":5,"currency":"USD","status":"declined","provider_payment_id":"pay_api_decline","event_id":"evt_api_decline"}`)
	declineReq := httptest.NewRequest(http.MethodPost, "/api/billing/payment", declineBody)
	declineReq.Header.Set("Content-Type", "application/json")
	declineReq = declineReq.WithContext(sop.ContextWithAuth(declineReq.Context(), sop.AuthContext{UserID: "billing-api", Roles: []string{sop.RoleUser}}))
	declineW := httptest.NewRecorder()
	handleBillingPayment(declineW, declineReq)
	if declineW.Code != http.StatusOK {
		t.Fatalf("handleBillingPayment(decline) status = %d, body=%s", declineW.Code, declineW.Body.String())
	}
	var declinePayload struct {
		User struct {
			BillingStatus string `json:"billing_status"`
			BillingNotice string `json:"billing_notice"`
		} `json:"user"`
	}
	if err := json.Unmarshal(declineW.Body.Bytes(), &declinePayload); err != nil {
		t.Fatalf("json.Unmarshal() decline error = %v", err)
	}
	if declinePayload.User.BillingStatus != "on_hold" {
		t.Fatalf("expected on_hold billing after decline, got %q", declinePayload.User.BillingStatus)
	}
	if !strings.Contains(declinePayload.User.BillingNotice, "on hold") {
		t.Fatalf("expected billing notice to mention on hold, got %q", declinePayload.User.BillingNotice)
	}
}

func TestHandleBillingDaemonAccess_ReportsHoldAndRecoveryState(t *testing.T) {
	setupSubscriptionIntegrationEnv(t)
	if _, err := config.RegisterUser(context.Background(), "daemon-access", "pw123", "daemon-access@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	if _, _, err := config.SubmitPayment(context.Background(), "daemon-access", "starter", 5.0, "USD", "declined", "pay_daemon_block", "evt_daemon_block", time.Now().UTC()); err != nil {
		t.Fatalf("SubmitPayment(declined) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/billing/daemon-access", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "daemon-access", Roles: []string{sop.RoleUser}}))
	w := httptest.NewRecorder()
	handleBillingDaemonAccess(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleBillingDaemonAccess() status = %d, body=%s", w.Code, w.Body.String())
	}
	var blockedPayload struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &blockedPayload); err != nil {
		t.Fatalf("json.Unmarshal() block error = %v", err)
	}
	if blockedPayload.Allowed {
		t.Fatalf("expected daemon access to be blocked while on hold, got %+v", blockedPayload)
	}
	if !strings.Contains(strings.ToLower(blockedPayload.Reason), "on hold") {
		t.Fatalf("expected reason to mention on hold, got %q", blockedPayload.Reason)
	}

	if _, _, err := config.SubmitPayment(context.Background(), "daemon-access", "starter", 5.0, "USD", "succeeded", "pay_daemon_restore", "evt_daemon_restore", time.Now().UTC()); err != nil {
		t.Fatalf("SubmitPayment(recovery) error = %v", err)
	}

	restoreReq := httptest.NewRequest(http.MethodGet, "/api/billing/daemon-access", nil)
	restoreReq = req.WithContext(sop.ContextWithAuth(restoreReq.Context(), sop.AuthContext{UserID: "daemon-access", Roles: []string{sop.RoleUser}}))
	restoreW := httptest.NewRecorder()
	handleBillingDaemonAccess(restoreW, restoreReq)
	if restoreW.Code != http.StatusOK {
		t.Fatalf("handleBillingDaemonAccess(recovery) status = %d, body=%s", restoreW.Code, restoreW.Body.String())
	}
	var restoredPayload struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
		User    struct {
			BillingNotice string `json:"billing_notice"`
		} `json:"user"`
	}
	if err := json.Unmarshal(restoreW.Body.Bytes(), &restoredPayload); err != nil {
		t.Fatalf("json.Unmarshal() restore error = %v", err)
	}
	if !restoredPayload.Allowed {
		t.Fatalf("expected daemon access to be allowed after recovery, got %+v", restoredPayload)
	}
	if !strings.Contains(strings.ToLower(restoredPayload.User.BillingNotice), "restored") {
		t.Fatalf("expected recovery notice in response, got %q", restoredPayload.User.BillingNotice)
	}
}

func TestHandleBillingStatus_ReturnsUserBillingStatusAndPayments(t *testing.T) {
	setupSubscriptionIntegrationEnv(t)
	if _, err := config.RegisterUser(context.Background(), "billing-status", "pw123", "billing-status@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}
	if _, _, err := config.SubmitPayment(context.Background(), "billing-status", "starter", 5.0, "USD", "declined", "pay_status", "evt_status", time.Now().UTC()); err != nil {
		t.Fatalf("SubmitPayment() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/billing/status", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "billing-status", Roles: []string{sop.RoleUser}}))
	w := httptest.NewRecorder()
	handleBillingStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleBillingStatus() status = %d, body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		User struct {
			BillingStatus string `json:"billing_status"`
			BillingNotice string `json:"billing_notice"`
		} `json:"user"`
		Payments []PaymentRecord `json:"payments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.User.BillingStatus != "on_hold" {
		t.Fatalf("expected on_hold billing status, got %q", payload.User.BillingStatus)
	}
	if len(payload.Payments) != 1 {
		t.Fatalf("expected 1 payment record, got %d", len(payload.Payments))
	}
}

func TestSubscriptionLifecycleIntegration_RegistersAuthenticatesActivatesAndUnsubscribes(t *testing.T) {
	env := setupSubscriptionIntegrationEnv(t)
	var loginRefreshToken string

	t.Run("register", func(t *testing.T) {
		body := strings.NewReader(`{"username":"subber","password":"pw123","email":"subber@example.com","role":"user","subscription_plan":"none"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/register", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handleRegister(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("handleRegister() status = %d, body=%s", w.Code, w.Body.String())
		}

		var payload struct {
			Status string `json:"status"`
			User   struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Status != "ok" || payload.User.Username != "subber" {
			t.Fatalf("unexpected registration payload: %+v", payload)
		}
	})

	t.Run("login_and_current_user", func(t *testing.T) {
		body := strings.NewReader(`{"username":"subber","password":"pw123"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handleLogin(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("handleLogin() status = %d, body=%s", w.Code, w.Body.String())
		}

		var loginPayload struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			User         struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &loginPayload); err != nil {
			t.Fatalf("json.Unmarshal() login error = %v", err)
		}
		if loginPayload.AccessToken == "" {
			t.Fatal("expected access token in login response")
		}
		if loginPayload.User.Username != "subber" {
			t.Fatalf("unexpected login username: %q", loginPayload.User.Username)
		}
		loginRefreshToken = loginPayload.RefreshToken

		meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		meReq = meReq.WithContext(sop.ContextWithAuth(meReq.Context(), sop.AuthContext{UserID: "subber", Roles: []string{sop.RoleUser}}))
		meW := httptest.NewRecorder()
		handleCurrentUser(meW, meReq)
		if meW.Code != http.StatusOK {
			t.Fatalf("handleCurrentUser() status = %d, body=%s", meW.Code, meW.Body.String())
		}
		var mePayload struct {
			User struct {
				SubscriptionPlan string `json:"subscription_plan"`
				BillingStatus    string `json:"billing_status"`
			} `json:"user"`
		}
		if err := json.Unmarshal(meW.Body.Bytes(), &mePayload); err != nil {
			t.Fatalf("json.Unmarshal() me error = %v", err)
		}
		if mePayload.User.SubscriptionPlan != "none" {
			t.Fatalf("expected initial subscription plan none, got %q", mePayload.User.SubscriptionPlan)
		}
		if mePayload.User.BillingStatus != "inactive" {
			t.Fatalf("expected initial billing status inactive, got %q", mePayload.User.BillingStatus)
		}
	})

	t.Run("checkout_and_webhook_activation", func(t *testing.T) {
		checkout, err := config.CreateCheckoutSession(context.Background(), "subber", "starter")
		if err != nil {
			t.Fatalf("CreateCheckoutSession() error = %v", err)
		}
		if checkout == nil || checkout.Status != "pending" {
			t.Fatalf("CreateCheckoutSession() returned %+v", checkout)
		}

		if err := config.HandleBillingWebhook(context.Background(), BillingWebhookEvent{
			EventType: "checkout.session.completed",
			EventID:   "evt_sub_1",
			UserID:    "subber",
			Plan:      "starter",
		}); err != nil {
			t.Fatalf("HandleBillingWebhook() error = %v", err)
		}

		user, ok, err := config.FindUserRecord(context.Background(), "subber")
		if err != nil {
			t.Fatalf("FindUserRecord() error = %v", err)
		}
		if !ok || user == nil {
			t.Fatal("expected user to exist after webhook")
		}
		if user.SubscriptionPlan != "starter" {
			t.Fatalf("expected plan starter after webhook, got %q", user.SubscriptionPlan)
		}
		if user.BillingStatus != "active" {
			t.Fatalf("expected billing status active after webhook, got %q", user.BillingStatus)
		}
	})

	t.Run("copilot_ask_and_refresh", func(t *testing.T) {
		prevLoadedAgents := loadedAgents
		stub := &stubSharedAskAgent{}
		loadedAgents = map[string]ai.Agent[map[string]any]{"copilot": stub}
		defer func() { loadedAgents = prevLoadedAgents }()

		out, err := AskSharedAgent(context.Background(), "plan my subscription lifecycle", nil)
		if err != nil {
			t.Fatalf("AskSharedAgent() error = %v", err)
		}
		if out != "shared-list-tools" {
			t.Fatalf("AskSharedAgent() = %q, want %q", out, "shared-list-tools")
		}
		if len(stub.queries) != 1 || stub.queries[0] != "plan my subscription lifecycle" {
			t.Fatalf("AskSharedAgent() forwarded %v, want [plan my subscription lifecycle]", stub.queries)
		}

		if err := config.HandleBillingWebhook(context.Background(), BillingWebhookEvent{
			EventType: "customer.subscription.deleted",
			EventID:   "evt_sub_cancel",
			UserID:    "subber",
			Plan:      "starter",
		}); err != nil {
			t.Fatalf("HandleBillingWebhook() cancellation error = %v", err)
		}
		user, ok, err := config.FindUserRecord(context.Background(), "subber")
		if err != nil {
			t.Fatalf("FindUserRecord() error = %v", err)
		}
		if !ok || user == nil {
			t.Fatal("expected user to exist after cancellation webhook")
		}
		if user.SubscriptionPlan != "none" {
			t.Fatalf("expected plan none after cancellation webhook, got %q", user.SubscriptionPlan)
		}
		if user.BillingStatus != "inactive" {
			t.Fatalf("expected billing status inactive after cancellation webhook, got %q", user.BillingStatus)
		}

		refreshPayload := strings.NewReader(`{"refresh_token":"` + loginRefreshToken + `"}`)
		refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", refreshPayload)
		refreshReq.Header.Set("Content-Type", "application/json")
		refreshW := httptest.NewRecorder()
		handleRefresh(refreshW, refreshReq)
		if refreshW.Code != http.StatusOK {
			t.Fatalf("handleRefresh() status = %d, body=%s", refreshW.Code, refreshW.Body.String())
		}
		var refreshedPayload struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.Unmarshal(refreshW.Body.Bytes(), &refreshedPayload); err != nil {
			t.Fatalf("json.Unmarshal() refresh error = %v", err)
		}
		if refreshedPayload.AccessToken == "" || refreshedPayload.RefreshToken == "" {
			t.Fatalf("expected rotated tokens in refresh response: %+v", refreshedPayload)
		}
	})

	t.Run("logout_relogin_and_unsubscribe", func(t *testing.T) {
		logoutW := httptest.NewRecorder()
		logoutReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
		(&UIRouter{}).handleLogout(logoutW, logoutReq)
		if logoutW.Code != http.StatusFound {
			t.Fatalf("handleLogout() status = %d, want %d", logoutW.Code, http.StatusFound)
		}

		loginBody := strings.NewReader(`{"username":"subber","password":"pw123"}`)
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
		loginReq.Header.Set("Content-Type", "application/json")
		loginW := httptest.NewRecorder()
		handleLogin(loginW, loginReq)
		if loginW.Code != http.StatusOK {
			t.Fatalf("handleLogin() status = %d, body=%s", loginW.Code, loginW.Body.String())
		}
		var reloginPayload struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(loginW.Body.Bytes(), &reloginPayload); err != nil {
			t.Fatalf("json.Unmarshal() relogin error = %v", err)
		}
		if reloginPayload.AccessToken == "" {
			t.Fatal("expected access token after relogin")
		}

		subscriptionReq := httptest.NewRequest(http.MethodPost, "/api/auth/subscription", strings.NewReader(`{"plan":"none"}`))
		subscriptionReq.Header.Set("Content-Type", "application/json")
		subscriptionReq = subscriptionReq.WithContext(sop.ContextWithAuth(subscriptionReq.Context(), sop.AuthContext{UserID: "subber", Roles: []string{sop.RoleUser}}))
		subscriptionW := httptest.NewRecorder()
		handleSubscriptionUpdate(subscriptionW, subscriptionReq)
		if subscriptionW.Code != http.StatusOK {
			t.Fatalf("handleSubscriptionUpdate() status = %d, body=%s", subscriptionW.Code, subscriptionW.Body.String())
		}

		meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		meReq = meReq.WithContext(sop.ContextWithAuth(meReq.Context(), sop.AuthContext{UserID: "subber", Roles: []string{sop.RoleUser}}))
		meW := httptest.NewRecorder()
		handleCurrentUser(meW, meReq)
		if meW.Code != http.StatusOK {
			t.Fatalf("handleCurrentUser() status = %d, body=%s", meW.Code, meW.Body.String())
		}
		var finalPayload struct {
			User struct {
				SubscriptionPlan string `json:"subscription_plan"`
				BillingStatus    string `json:"billing_status"`
			} `json:"user"`
		}
		if err := json.Unmarshal(meW.Body.Bytes(), &finalPayload); err != nil {
			t.Fatalf("json.Unmarshal() final me error = %v", err)
		}
		if finalPayload.User.SubscriptionPlan != "none" {
			t.Fatalf("expected final plan none after unsubscribe, got %q", finalPayload.User.SubscriptionPlan)
		}
		if finalPayload.User.BillingStatus != "inactive" {
			t.Fatalf("expected final billing status inactive after unsubscribe, got %q", finalPayload.User.BillingStatus)
		}
	})

	_ = env
}

type subscriptionIntegrationEnv struct {
	root string
}

func setupSubscriptionIntegrationEnv(t *testing.T) *subscriptionIntegrationEnv {
	t.Helper()

	oldConfig := config
	oldTokenFacade := tokenFacade
	oldTokenOnce := tokenFacadeOnce
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	config = Config{
		UsersStore:         NewUserStore(),
		SubscriptionsStore: NewSubscriptionStore(),
		PaymentsStore:      NewPaymentStore(),
		SystemDB:           &DatabaseConfig{Name: SystemDBName, Path: root, Mode: "standalone"},
	}
	tokenFacade = nil
	tokenFacadeOnce = sync.Once{}

	t.Cleanup(func() {
		config = oldConfig
		tokenFacade = oldTokenFacade
		tokenFacadeOnce = oldTokenOnce
		_ = os.Chdir(oldCwd)
	})

	return &subscriptionIntegrationEnv{root: root}
}
