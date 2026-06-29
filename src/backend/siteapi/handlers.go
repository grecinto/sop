package siteapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

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
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			status = http.StatusConflict
		}
		writeJSONError(w, status, err.Error())
		return
	}
	sopbackend.RecordProductMetric("site_auth_register_requests")
	sopbackend.SetConfig(cfg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          user.Username,
			"role":              user.Role,
			"email":             user.Email.Plaintext(),
			"subscription_plan": user.SubscriptionPlan,
			"billing_status":    user.BillingStatus,
		},
	})
}

func clearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "sop_access_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "sop_refresh_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
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

	sopbackend.RecordProductMetric("site_auth_login_requests")
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clearSessionCookies(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "logged out"})
}

func buildUserResponsePayload(user *sopbackend.UserRecord) map[string]any {
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
	return map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          user.Username,
			"role":              user.Role,
			"email":             user.Email.Plaintext(),
			"subscription_plan": user.SubscriptionPlan,
			"billing_status":    user.BillingStatus,
			"billing":           billing,
			"status":            user.Status,
			"created_at":        user.CreatedAt,
			"updated_at":        user.UpdatedAt,
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
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(user))
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
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(user))
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
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := sopbackend.GetConfig()
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
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          updated.Username,
			"subscription_plan": updated.SubscriptionPlan,
			"billing_status":    updated.BillingStatus,
			"updated_at":        updated.UpdatedAt,
		},
	})
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

	var req struct {
		EventType string `json:"event_type"`
		EventID   string `json:"event_id"`
		UserID    string `json:"user_id"`
		Plan      string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := sopbackend.GetConfig()
	if err := cfg.HandleBillingWebhook(r.Context(), sopbackend.BillingWebhookEvent{EventType: req.EventType, EventID: req.EventID, UserID: req.UserID, Plan: req.Plan}); err != nil {
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "account removed"})
}

func init() {
	_ = context.Background
	_ = fmt.Stringer(nil)
}
