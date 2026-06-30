package siteaccount

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

// RegisterHandlers wires the site account handlers to the site backend mux.
func RegisterHandlers(mux *http.ServeMux) {
	if mux == nil {
		return
	}
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

type registerUserRequest struct {
	Username         string `json:"username"`
	Password         string `json:"password"`
	Email            string `json:"email"`
	Role             string `json:"role"`
	SubscriptionPlan string `json:"subscription_plan"`
}

type authAccount struct {
	Username string
	Roles    []string
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "message": message})
}

func currentAccountFromRequest(r *http.Request) authAccount {
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
	return authAccount{Username: username, Roles: roles}
}

func HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req registerUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := GetConfig()
	user, err := cfg.RegisterUserWithRequest(r.Context(), RegisterUserRequest{
		Username:         req.Username,
		Password:         req.Password,
		Email:            req.Email,
		Role:             req.Role,
		SubscriptionPlan: req.SubscriptionPlan,
	})
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	SetConfig(cfg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(user))
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := GetConfig()
	if cfg == nil {
		writeJSONError(w, http.StatusInternalServerError, "auth config unavailable")
		return
	}
	user, ok, err := cfg.FindUserRecord(r.Context(), req.Username)
	if err != nil || !ok || user == nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "user": map[string]any{"username": user.Username}})
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "logged out"})
}

func buildUserResponsePayload(user *UserRecord) map[string]any {
	if user == nil {
		return map[string]any{"status": "ok", "user": map[string]any{}}
	}
	billing := map[string]any{
		"status":             user.BillingStatus,
		"checkout_url":       user.CheckoutURL,
		"last_webhook_at":    user.LastWebhookAt.UTC().Format(time.RFC3339Nano),
		"last_webhook_event": user.LastWebhookEvent,
		"failure_reason":     user.FailureReason,
	}
	return map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":          user.Username,
			"role":              user.Role,
			"email":             user.Email,
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
	account := currentAccountFromRequest(r)
	if account.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	cfg := GetConfig()
	user, ok, err := cfg.FindUserRecord(r.Context(), account.Username)
	if err != nil {
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
	account := currentAccountFromRequest(r)
	if account.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	cfg := GetConfig()
	user, ok, err := cfg.FindUserRecord(r.Context(), account.Username)
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
	account := currentAccountFromRequest(r)
	if account.Username == "" {
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
	cfg := GetConfig()
	updated, err := cfg.UpdateSubscriptionPlanWithRequest(r.Context(), UpdateSubscriptionPlanRequest{Username: account.Username, SubscriptionPlan: req.Plan})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(updated))
}

func HandleCheckoutSession(w http.ResponseWriter, r *http.Request) {
	account := currentAccountFromRequest(r)
	if account.Username == "" {
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
	cfg := GetConfig()
	session, err := cfg.CreateCheckoutSession(r.Context(), account.Username, req.Plan)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "checkout": map[string]any{"id": session.ID, "url": session.URL, "status": session.Status}})
}

func HandleDownloadAsset(w http.ResponseWriter, r *http.Request) {
	account := currentAccountFromRequest(r)
	if account.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	cfg := GetConfig()
	user, ok, err := cfg.FindUserRecord(r.Context(), account.Username)
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
		downloadURL = "https://github.com/sharedcode/sop/releases/latest"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "available": true, "download_url": downloadURL, "plan": user.SubscriptionPlan})
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
	cfg := GetConfig()
	if err := cfg.HandleBillingWebhook(r.Context(), BillingWebhookEvent{EventType: req.EventType, EventID: req.EventID, UserID: req.UserID, Plan: req.Plan}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func HandleUnregister(w http.ResponseWriter, r *http.Request) {
	account := currentAccountFromRequest(r)
	if account.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	cfg := GetConfig()
	if err := cfg.UnregisterUserWithRequest(r.Context(), UnregisterUserRequest{Username: account.Username}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "account removed"})
}

var _ = context.Background
var _ = fmt.Stringer(nil)
