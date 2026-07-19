package siteapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/grecinto/sop-cloud/src/backend/siteaccount"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

var siteMux *http.ServeMux

// RegisterHandlers registers the product-facing site account endpoints for registration, current-user lookup, subscription updates, and unregister flows.
func RegisterHandlers(mux *http.ServeMux) {
	siteMux = mux
	mux.HandleFunc("/api/auth/register", HandleRegister)
	mux.HandleFunc("/api/site/auth/login", HandleLogin)
	mux.HandleFunc("/api/site/auth/logout", HandleLogout)
	mux.HandleFunc("/api/auth/logout", HandleLogout)
	mux.HandleFunc("/api/auth/me", sopbackend.WithAuth(HandleCurrentUser))
	mux.HandleFunc("/api/auth/status", sopbackend.WithAuth(HandleStatus))
	mux.HandleFunc("/api/auth/subscription", sopbackend.WithAuth(HandleSubscriptionUpdate))
	mux.HandleFunc("/api/auth/checkout", sopbackend.WithAuth(HandleCheckoutSession))
	mux.HandleFunc("/api/auth/download", sopbackend.WithAuth(HandleDownloadAsset))
	mux.HandleFunc("/api/auth/ping", sopbackend.WithAuth(siteaccount.HandlePing))
	mux.HandleFunc("/api/auth/pending-checkout/recover", sopbackend.WithAuth(siteaccount.HandleRecoverPendingCheckout))
	mux.HandleFunc("/api/auth/webhooks/billing", HandleBillingWebhook)
	mux.HandleFunc("/api/auth/unregister", sopbackend.WithAuth(HandleUnregister))
}

type RegisterUserRequest struct {
	Username         string
	Password         string
	Email            string
	Role             string
	SubscriptionPlan string
}

type UnregisterUserRequest struct {
	Username string
}

type UpdateSubscriptionPlanRequest struct {
	Username         string
	SubscriptionPlan string
}

type AuthenticatedAccount struct {
	Username string
	Roles    []string
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "message": message})
}

func parseRegisterRequest(r *http.Request) (RegisterUserRequest, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return RegisterUserRequest{}, err
		}
		return RegisterUserRequest{
			Username:         r.FormValue("username"),
			Password:         r.FormValue("password"),
			Email:            r.FormValue("email"),
			Role:             r.FormValue("role"),
			SubscriptionPlan: r.FormValue("subscription_plan"),
		}, nil
	}

	var req struct {
		Username         string `json:"username"`
		Password         string `json:"password"`
		Email            string `json:"email"`
		Role             string `json:"role"`
		SubscriptionPlan string `json:"subscription_plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return RegisterUserRequest{}, err
	}
	return RegisterUserRequest{
		Username:         req.Username,
		Password:         req.Password,
		Email:            req.Email,
		Role:             req.Role,
		SubscriptionPlan: req.SubscriptionPlan,
	}, nil
}

func currentAccountFromRequest(r *http.Request) AuthenticatedAccount {
	authCtx := sop.GetAuthFromContext(r.Context())
	username := strings.TrimSpace(authCtx.UserID)
	roles := make([]string, 0, len(authCtx.Roles))
	for _, role := range authCtx.Roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		roles = append(roles, role)
	}
	return AuthenticatedAccount{Username: username, Roles: roles}
}

func HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := parseRegisterRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := sopbackend.GetConfig()
	user, err := cfg.RegisterUserWithRequest(r.Context(), sopbackend.RegisterUserRequest{
		Username:         req.Username,
		Password:         req.Password,
		Email:            req.Email,
		Role:             req.Role,
		SubscriptionPlan: req.SubscriptionPlan,
	})
	if err != nil {
		sopbackend.RecordProductMetric("site_auth_register_failures")
		status := http.StatusInternalServerError
		message := err.Error()
		if strings.Contains(strings.ToLower(err.Error()), "already exists") || strings.Contains(strings.ToLower(err.Error()), "already taken") {
			status = http.StatusConflict
			message = "username is not available (already taken)."
		} else if strings.Contains(strings.ToLower(err.Error()), "early-access seats") {
			status = http.StatusTooManyRequests
		}
		writeJSONError(w, status, message)
		return
	}
	sopbackend.RecordProductMetric("site_auth_register_requests")
	sopbackend.SetConfig(cfg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(r, user))
}

func clearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "sop_access_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "sop_refresh_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func traceLoginRequest(r *http.Request, body []byte) {
	if r == nil {
		return
	}

	username := ""
	if len(body) > 0 {
		if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			var payload struct {
				Username string `json:"username"`
			}
			if err := json.Unmarshal(body, &payload); err == nil {
				username = strings.TrimSpace(payload.Username)
			}
		} else if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
			if form, err := url.ParseQuery(string(body)); err == nil {
				username = strings.TrimSpace(form.Get("username"))
			}
		}
	}

	log.Printf("site-auth.trace path=%s method=%s remote_addr=%s content_type=%s username=%q body_bytes=%d", r.URL.Path, r.Method, r.RemoteAddr, r.Header.Get("Content-Type"), username, len(body))
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	traceLoginRequest(r, bodyBytes)

	proxyReq := httptest.NewRequest(r.Method, "/api/auth/login", bytes.NewReader(bodyBytes))
	proxyReq.Header = r.Header.Clone()
	proxyReq = proxyReq.WithContext(r.Context())

	recorder := httptest.NewRecorder()
	if siteMux != nil {
		siteMux.ServeHTTP(recorder, proxyReq)
	} else {
		sopbackend.RecordProductMetric("site_auth_login_failures")
		writeJSONError(w, http.StatusInternalServerError, "login handler is unavailable")
		return
	}

	for _, cookie := range recorder.Result().Cookies() {
		http.SetCookie(w, cookie)
	}
	for key, values := range recorder.Header() {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(recorder.Code)
	_, _ = w.Write(recorder.Body.Bytes())
	log.Printf("site-auth.trace result path=%s method=%s status=%d", r.URL.Path, r.Method, recorder.Code)

	sopbackend.RecordProductMetric("site_auth_login_requests")
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokens := make([]string, 0, 2)
	if token := strings.TrimSpace(sopbackend.AuthTokenFromRequest(r)); token != "" {
		tokens = append(tokens, token)
	}
	if cookie, err := r.Cookie("sop_refresh_token"); err == nil {
		if refreshToken := strings.TrimSpace(cookie.Value); refreshToken != "" {
			tokens = append(tokens, refreshToken)
		}
	}
	fmt.Printf("siteapi.logout.revoking_tokens path=%s token_count=%d remote_addr=%s\n", r.URL.Path, len(tokens), r.RemoteAddr)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		sopbackend.RevokeToken(r.Context(), token)
		fmt.Printf("siteapi.logout.revoked_token path=%s token_prefix=%s remote_addr=%s\n", r.URL.Path, token[:min(len(token), 8)], r.RemoteAddr)
	}

	clearSessionCookies(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "logged out"})
}

func buildUserResponsePayload(r *http.Request, user *sopbackend.UserRecord) map[string]any {
	if user == nil {
		return map[string]any{"status": "ok", "user": map[string]any{}}
	}

	lastWebhookAt := ""
	if user != nil && !user.LastWebhookAt.IsZero() {
		lastWebhookAt = user.LastWebhookAt.UTC().Format(time.RFC3339Nano)
	}
	billing := map[string]any{
		"status":             user.BillingStatus,
		"checkout_url":       user.CheckoutURL,
		"last_webhook_at":    lastWebhookAt,
		"last_webhook_event": user.LastWebhookEvent,
		"failure_reason":     user.FailureReason,
	}

	cfg := sopbackend.GetConfig()
	payments := make([]map[string]any, 0, len(cfg.Payments))
	for _, payment := range cfg.Payments {
		if !strings.EqualFold(payment.UserID, user.Username) {
			continue
		}
		createdAt := ""
		if !payment.CreatedAt.IsZero() {
			createdAt = payment.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		updatedAt := ""
		if !payment.UpdatedAt.IsZero() {
			updatedAt = payment.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		payments = append(payments, map[string]any{
			"id":                  payment.ID,
			"status":              payment.Status,
			"amount":              payment.Amount,
			"currency":            payment.Currency,
			"subscription_id":     payment.SubscriptionID,
			"provider_payment_id": payment.ProviderPaymentID,
			"created_at":          createdAt,
			"updated_at":          updatedAt,
		})
	}

	subscriptions := make([]map[string]any, 0, len(cfg.Subscriptions))
	for _, subscription := range cfg.Subscriptions {
		if !strings.EqualFold(subscription.UserID, user.Username) {
			continue
		}
		createdAt := ""
		if !subscription.CreatedAt.IsZero() {
			createdAt = subscription.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		updatedAt := ""
		if !subscription.UpdatedAt.IsZero() {
			updatedAt = subscription.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		subscriptions = append(subscriptions, map[string]any{
			"id":                       subscription.ID,
			"plan_code":                subscription.PlanCode,
			"status":                   subscription.Status,
			"provider_subscription_id": subscription.ProviderSubscription,
			"cancel_at_period_end":     subscription.CancelAtPeriodEnd,
			"created_at":               createdAt,
			"updated_at":               updatedAt,
		})
	}

	authCtx := sop.AuthContext{}
	if r != nil {
		authCtx = sop.GetAuthFromContext(r.Context())
	}
	canAccessDataManager := authCtx.IsSystem || strings.EqualFold(user.Role, sop.RoleSystemAdmin) || strings.EqualFold(user.Username, "root")

	return map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":                user.Username,
			"role":                    user.Role,
			"email":                   user.Email.Plaintext(),
			"subscription_plan":       user.SubscriptionPlan,
			"billing_status":          user.BillingStatus,
			"billing":                 billing,
			"payments":                payments,
			"subscriptions":           subscriptions,
			"status":                  user.Status,
			"created_at":              user.CreatedAt,
			"updated_at":              user.UpdatedAt,
			"can_access_data_manager": canAccessDataManager,
		},
	}
}

func HandleCurrentUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := currentAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	cfg := sopbackend.GetConfig()
	user, ok, err := cfg.FindUserRecord(r.Context(), authAccount.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(r, user))
}

func HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := currentAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	cfg := sopbackend.GetConfig()
	user, ok, err := cfg.FindUserRecord(r.Context(), authAccount.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(r, user))
}

func HandleSubscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := currentAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Plan       string `json:"plan"`
		Unregister bool   `json:"unregister"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := sopbackend.GetConfig()
	if req.Unregister && strings.EqualFold(strings.TrimSpace(req.Plan), "none") {
		if err := cfg.UnregisterUserWithRequest(r.Context(), sopbackend.UnregisterUserRequest{Username: authAccount.Username}); err != nil {
			sopbackend.RecordProductMetric("site_auth_subscription_failures")
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sopbackend.RecordProductMetric("site_auth_subscription_requests")
		sopbackend.SetConfig(cfg)
		clearSessionCookies(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "subscription canceled and account removed", "unregistered": true})
		return
	}
	updated, err := cfg.UpdateSubscriptionPlanWithRequest(r.Context(), sopbackend.UpdateSubscriptionPlanRequest{
		Username:         authAccount.Username,
		SubscriptionPlan: req.Plan,
	})
	if err != nil {
		sopbackend.RecordProductMetric("site_auth_subscription_failures")
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		writeJSONError(w, status, err.Error())
		return
	}
	sopbackend.RecordProductMetric("site_auth_subscription_requests")
	sopbackend.SetConfig(cfg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(r, updated))
}

func HandleCheckoutSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := currentAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := sopbackend.GetConfig()
	session, err := cfg.CreateCheckoutSession(r.Context(), authAccount.Username, req.Plan)
	if err != nil {
		sopbackend.RecordProductMetric("site_auth_checkout_failures")
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sopbackend.RecordProductMetric("site_auth_checkout_requests")
	sopbackend.SetConfig(cfg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"checkout": map[string]any{
			"id":     session.ID,
			"url":    session.URL,
			"status": session.Status,
		},
	})
}

func HandleDownloadAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := currentAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	cfg := sopbackend.GetConfig()
	user, ok, err := cfg.FindUserRecord(r.Context(), authAccount.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	isEntitled := strings.EqualFold(user.SubscriptionPlan, "starter") || strings.EqualFold(user.SubscriptionPlan, "hq") || strings.EqualFold(user.BillingStatus, "active")
	if !isEntitled {
		writeJSONError(w, http.StatusForbidden, "active subscription required")
		return
	}

	downloadURL := strings.TrimSpace(os.Getenv("SOP_DOWNLOAD_URL"))
	if downloadURL == "" {
		// TODO: Replace this fallback with the Cloudflare R2 object URL or a short-lived signed URL
		// once the daemon release is published and the Cloudflare asset is available.
		downloadURL = "https://github.com/sharedcode/sop/releases/latest"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       "ok",
		"available":    true,
		"download_url": downloadURL,
		"plan":         user.SubscriptionPlan,
	})
}

func HandleBillingWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		EventType string `json:"event_type"`
		EventID   string `json:"event_id"`
		UserID    string `json:"user_id"`
		Plan      string `json:"plan"`
		ID        string `json:"id"`
		Type      string `json:"type"`
		Data      struct {
			Object map[string]any `json:"object"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	eventType := strings.TrimSpace(payload.EventType)
	if eventType == "" {
		eventType = strings.TrimSpace(payload.Type)
	}
	eventID := strings.TrimSpace(payload.EventID)
	if eventID == "" {
		eventID = strings.TrimSpace(payload.ID)
	}
	userID := strings.TrimSpace(payload.UserID)
	plan := strings.TrimSpace(payload.Plan)
	if userID == "" {
		if metadata, ok := payload.Data.Object["metadata"].(map[string]any); ok {
			if usernameValue, ok := metadata["username"].(string); ok {
				userID = strings.TrimSpace(usernameValue)
			}
		}
	}
	if plan == "" {
		if metadata, ok := payload.Data.Object["metadata"].(map[string]any); ok {
			if planValue, ok := metadata["plan"].(string); ok {
				plan = strings.TrimSpace(planValue)
			}
		}
	}

	cfg := sopbackend.GetConfig()
	if err := cfg.HandleBillingWebhook(r.Context(), sopbackend.BillingWebhookEvent{EventType: eventType, EventID: eventID, UserID: userID, Plan: plan}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sopbackend.SetConfig(cfg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func HandleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authAccount := currentAccountFromRequest(r)
	if authAccount.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	cfg := sopbackend.GetConfig()
	if err := cfg.UnregisterUserWithRequest(r.Context(), sopbackend.UnregisterUserRequest{Username: authAccount.Username}); err != nil {
		sopbackend.RecordProductMetric("site_auth_unregister_failures")
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sopbackend.RecordProductMetric("site_auth_unregister_requests")
	sopbackend.SetConfig(cfg)

	clearSessionCookies(w)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "account removed"})
}

func init() {
	_ = context.Background
	_ = fmt.Stringer(nil)
}
