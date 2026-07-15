package siteaccount

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/grecinto/sop-cloud/src/backend/siteapp"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

// RegisterHandlers wires the site account handlers to the site backend mux.
func RegisterHandlers(mux *http.ServeMux) {
	if mux == nil {
		return
	}

	mux.HandleFunc("/api/auth/register", HandleRegister)
	mux.HandleFunc("/api/auth/human-proof/challenge", HandleHumanProofChallenge)
	// Login is handled by sopbackend at /api/auth/login - no site-specific login endpoint needed
	mux.HandleFunc("/api/site/auth/logout", HandleLogout)
	mux.HandleFunc("/api/auth/me", withSiteAuth(HandleCurrentUser))
	// Status endpoint checks config existence BEFORE requiring auth
	mux.HandleFunc("/api/auth/status", HandleStatus)
	mux.HandleFunc("/api/auth/subscription", withSiteAuth(HandleSubscriptionUpdate))
	mux.HandleFunc("/api/auth/password", withSiteAuth(HandleChangePassword))
	mux.HandleFunc("/api/auth/checkout", withSiteAuth(HandleCheckoutSession))
	mux.HandleFunc("/api/auth/download", withSiteAuth(HandleDownloadAsset))
	mux.HandleFunc("/api/auth/ping", withSiteAuth(HandlePing))
	mux.HandleFunc("/api/auth/pending-checkout/recover", HandleRecoverPendingCheckout)
	mux.HandleFunc("/api/auth/webhooks/billing", HandleBillingWebhook)
	mux.HandleFunc("/api/auth/unregister", withSiteAuth(HandleUnregister))
}

type registerUserRequest struct {
	Username         string `json:"username"`
	FullName         string `json:"full_name,omitempty"`
	Password         string `json:"password"`
	Email            string `json:"email"`
	Role             string `json:"role"`
	SubscriptionPlan string `json:"subscription_plan"`
	RequestOrigin    string `json:"request_origin"`
	HumanProof       string `json:"human_proof"`
}

type authAccount struct {
	Username string
	Roles    []string
}

var humanProofChallengesMu sync.RWMutex
var humanProofChallenges = map[string]time.Time{}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "message": message})
}

func shortTokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
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

func withSiteAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next(w, r)
			return
		}

		token := sopbackend.AuthTokenFromRequest(r)
		if strings.TrimSpace(token) == "" {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		cfg := sopbackend.GetConfig()
		ok, user, err := cfg.AuthenticateBearerToken(r.Context(), token)
		if err != nil || !ok || user == nil {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		auth := sop.AuthContext{
			IsSystem:             strings.EqualFold(user.Role, sop.RoleSystemAdmin) || strings.EqualFold(user.Username, "root"),
			Roles:                []string{user.Role},
			UserID:               user.Username,
			SubscriptionPlan:     user.SubscriptionPlan,
			SubscriptionFeatures: append([]string(nil), user.SubscriptionFeatures...),
		}
		auth.Permissions = map[string]bool{siteapp.PermissionSiteAppAccess: siteapp.HasSiteAppAccess(auth)}
		next(w, r.WithContext(sop.ContextWithAuth(r.Context(), auth)))
	}
}

func generateChallengeToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

func issueHumanProofChallenge(w http.ResponseWriter) string {
	challenge := generateChallengeToken()
	if challenge == "" {
		return ""
	}
	humanProofChallengesMu.Lock()
	humanProofChallenges[challenge] = time.Now().Add(5 * time.Minute)
	humanProofChallengesMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "sop_human_challenge", Value: challenge, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	return challenge
}

func validateHumanProof(r *http.Request, supplied string) bool {
	supplied = strings.TrimSpace(supplied)
	if supplied == "" {
		return false
	}
	cookie, err := r.Cookie("sop_human_challenge")
	if err != nil {
		return false
	}
	challenge := strings.TrimSpace(cookie.Value)
	if challenge == "" || challenge != supplied {
		return false
	}
	humanProofChallengesMu.RLock()
	expiresAt, ok := humanProofChallenges[challenge]
	humanProofChallengesMu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiresAt) {
		humanProofChallengesMu.Lock()
		delete(humanProofChallenges, challenge)
		humanProofChallengesMu.Unlock()
		return false
	}
	return true
}

func HandleHumanProofChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	challenge := issueHumanProofChallenge(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"challenge": challenge,
		"prompt":    "Type the number shown in the captcha image.",
		"type":      "captcha",
	})
}

func HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slog.Info("siteaccount.register.request", "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr, "user_agent", r.UserAgent())
	var req registerUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateHumanProof(r, req.HumanProof) {
		slog.Warn("siteaccount.register.human_proof_missing", "username", req.Username, "path", r.URL.Path)
		writeJSONError(w, http.StatusForbidden, "please confirm you are human before continuing")
		return
	}
	cfg := GetConfig()
	user, err := cfg.RegisterUserWithRequest(r.Context(), RegisterUserRequest{
		Username:         req.Username,
		FullName:         req.FullName,
		Password:         req.Password,
		Email:            req.Email,
		Role:             req.Role,
		SubscriptionPlan: req.SubscriptionPlan,
		RequestOrigin:    req.RequestOrigin,
	})
	if err != nil {
		slog.Warn("siteaccount.register.failed", "username", req.Username, "error", err.Error())
		status := http.StatusConflict
		if strings.Contains(strings.ToLower(err.Error()), "early-access seats") {
			status = http.StatusTooManyRequests
		}
		writeJSONError(w, status, err.Error())
		return
	}
	slog.Info("siteaccount.register.success", "username", user.Username, "role", user.Role, "plan", user.SubscriptionPlan)
	SetConfig(cfg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(user, r.Context()))
}

func setSessionCookies(w http.ResponseWriter, accessToken, refreshToken string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sop_access_token",
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((30 * time.Minute).Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "sop_refresh_token",
		Value:    refreshToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	})
}

func clearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "sop_access_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "sop_refresh_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func revokeSessionTokensFromRequest(ctx context.Context, r *http.Request) {
	tokens := make([]string, 0, 2)
	if token := strings.TrimSpace(sopbackend.AuthTokenFromRequest(r)); token != "" {
		tokens = append(tokens, token)
	}
	if cookie, err := r.Cookie("sop_refresh_token"); err == nil {
		if refreshToken := strings.TrimSpace(cookie.Value); refreshToken != "" {
			tokens = append(tokens, refreshToken)
		}
	}
	slog.Info("siteaccount.logout.revoking_tokens", "path", r.URL.Path, "token_count", len(tokens), "remote_addr", r.RemoteAddr)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		sopbackend.RevokeToken(ctx, token)
		slog.Info("siteaccount.logout.revoked_token", "path", r.URL.Path, "token_prefix", shortTokenPrefix(token), "remote_addr", r.RemoteAddr)
	}
}

func HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req.RefreshToken = r.FormValue("refresh_token")
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if cookie, errCookie := r.Cookie("sop_refresh_token"); errCookie == nil && strings.TrimSpace(cookie.Value) != "" {
			req.RefreshToken = strings.TrimSpace(cookie.Value)
		} else {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		if cookie, errCookie := r.Cookie("sop_refresh_token"); errCookie == nil && strings.TrimSpace(cookie.Value) != "" {
			req.RefreshToken = strings.TrimSpace(cookie.Value)
		}
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		writeJSONError(w, http.StatusBadRequest, "refresh token required")
		return
	}
	accessToken, refreshToken, err := sopbackend.RefreshSession(r.Context(), req.RefreshToken)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	setSessionCookies(w, accessToken, refreshToken, r.TLS != nil)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "access_token": accessToken, "refresh_token": refreshToken, "token_type": "Bearer"})
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	revokeSessionTokensFromRequest(r.Context(), r)
	clearSessionCookies(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "logged out"})
}

func buildUserResponsePayload(user *UserRecord, ctx context.Context) map[string]any {
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
	authCtx := sop.GetAuthFromContext(ctx)
	canAccessDataManager := authCtx.IsSystem
	for _, role := range authCtx.Roles {
		if sop.IsAdminRole(role) || strings.EqualFold(role, sop.RoleSystemAdmin) {
			canAccessDataManager = true
			break
		}
	}
	if user != nil && strings.EqualFold(strings.TrimSpace(user.Username), "root") {
		canAccessDataManager = true
	}
	permissions := map[string]bool{
		siteapp.PermissionSiteAppAccess: siteapp.HasSiteAppAccess(authCtx),
	}
	return map[string]any{
		"status": "ok",
		"user": map[string]any{
			"username":                user.Username,
			"full_name":               user.FullName,
			"role":                    user.Role,
			"email":                   user.Email,
			"subscription_plan":       user.SubscriptionPlan,
			"billing_status":          user.BillingStatus,
			"billing":                 billing,
			"request_origin":          user.RequestOrigin,
			"status":                  user.Status,
			"created_at":              user.CreatedAt,
			"updated_at":              user.UpdatedAt,
			"can_access_data_manager": canAccessDataManager,
			"permissions":             permissions,
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
	// Sync billing status from Stripe if pending (fallback for missed webhooks)
	if user.BillingStatus == "pending" {
		_ = cfg.SyncSubscriptionStatusFromStripe(r.Context(), account.Username)
		// Re-fetch user after potential sync
		user, ok, err = cfg.FindUserRecord(r.Context(), account.Username)
		if err != nil || !ok || user == nil {
			writeJSONError(w, http.StatusInternalServerError, "unable to refresh user state")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(user, r.Context()))
}

func HandleStatus(w http.ResponseWriter, r *http.Request) {
	// This must happen BEFORE checking authentication since no users exist yet
	if !sopbackend.HasConfigFileOnDisk() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"config_provisioned": false,
		})
		return
	}

	// Config exists, now require authentication	}

	account := currentAccountFromRequest(r)
	slog.Debug("siteaccount.status.request", "path", r.URL.Path, "username", account.Username, "auth_roles", strings.Join(account.Roles, ","), "remote_addr", r.RemoteAddr)
	if account.Username == "" {
		slog.Warn("siteaccount.status.unauthenticated", "path", r.URL.Path)
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
	slog.Info("siteaccount.status.user_found", "username", account.Username, "billing_status_before_sync", user.BillingStatus)
	// Sync billing status from Stripe if pending (fallback for missed webhooks)
	if user.BillingStatus == "pending" {
		_ = cfg.SyncSubscriptionStatusFromStripe(r.Context(), account.Username)
		// Re-fetch user after potential sync
		user, ok, err = cfg.FindUserRecord(r.Context(), account.Username)
		if err != nil || !ok || user == nil {
			writeJSONError(w, http.StatusInternalServerError, "unable to refresh user state")
			return
		}
		slog.Info("siteaccount.status.user_after_sync", "username", account.Username, "billing_status_after_sync", user.BillingStatus)
	}
	slog.Info("siteaccount.status.response", "username", account.Username, "final_billing_status", user.BillingStatus)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	payload := buildUserResponsePayload(user, r.Context())
	// Add config provisioning status so frontend can detect if Data Manager setup is needed
	payload["config_provisioned"] = sopbackend.HasConfigFileOnDisk()
	_ = json.NewEncoder(w).Encode(payload)
}

func HandlePing(w http.ResponseWriter, r *http.Request) {
	account := currentAccountFromRequest(r)
	slog.Info("siteaccount.ping.request", "username", account.Username, "path", r.URL.Path, "remote_addr", r.RemoteAddr)
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
	slog.Info("siteaccount.ping.user_found", "username", account.Username, "billing_status", user.BillingStatus)
	if strings.EqualFold(user.BillingStatus, "pending") {
		slog.Info("siteaccount.ping.sync_pending_billing", "username", account.Username, "checkout_session_id", user.StripeCheckoutSessionID, "pending_checkout_id", user.PendingCheckoutSessionID)
		_ = cfg.SyncSubscriptionStatusFromStripe(r.Context(), account.Username)
		refreshedUser, ok, err := cfg.FindUserRecord(r.Context(), account.Username)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok || refreshedUser == nil {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		user = refreshedUser
		slog.Info("siteaccount.ping.user_after_sync", "username", account.Username, "billing_status_after_sync", user.BillingStatus)
		if !strings.EqualFold(user.BillingStatus, "pending") {
			slog.Info("siteaccount.ping.status_changed_to_active", "username", account.Username, "new_status", user.BillingStatus)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "billing_status": user.BillingStatus})
			return
		}
		now := time.Now().UTC()
		if user.PendingCheckoutPingDeadline.IsZero() {
			user.PendingCheckoutPingDeadline = now.Add(30 * time.Second)
		} else {
			user.PendingCheckoutPingDeadline = user.PendingCheckoutPingDeadline.Add(15 * time.Second)
			if user.PendingCheckoutPingDeadline.Before(now.Add(30 * time.Second)) {
				user.PendingCheckoutPingDeadline = now.Add(30 * time.Second)
			}
		}
		user.LastPingAt = now
		user.UpdatedAt = now
		if err := cfg.UpsertUserRecord(r.Context(), user); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	slog.Info("siteaccount.ping.final_response", "username", account.Username, "billing_status", user.BillingStatus)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "billing_status": user.BillingStatus})
}

func HandleRecoverPendingCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CheckoutSessionID string `json:"checkout_session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	checkoutSessionID := strings.TrimSpace(req.CheckoutSessionID)
	if checkoutSessionID == "" {
		checkoutSessionID = strings.TrimSpace(r.URL.Query().Get("checkout_session_id"))
	}
	if checkoutSessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "checkout session id is required")
		return
	}

	cfg := GetConfig()
	user, ok, err := cfg.FindUserRecordByCheckoutSession(r.Context(), checkoutSessionID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "user": nil})
		return
	}

	if strings.EqualFold(user.BillingStatus, "pending") {
		slog.Info("siteaccount.recover.sync_pending_billing", "username", user.Username, "checkout_session_id", user.StripeCheckoutSessionID, "pending_checkout_id", user.PendingCheckoutSessionID)
		_ = cfg.SyncSubscriptionStatusFromStripe(r.Context(), user.Username)
		refreshedUser, ok, err := cfg.FindUserRecord(r.Context(), user.Username)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok || refreshedUser == nil {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		user = refreshedUser
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "user": map[string]any{
		"username":          user.Username,
		"role":              user.Role,
		"email":             user.Email,
		"subscription_plan": user.SubscriptionPlan,
		"billing_status":    user.BillingStatus,
		"billing": map[string]any{
			"checkout_url":                   user.CheckoutURL,
			"last_webhook_event":             user.LastWebhookEvent,
			"failure_reason":                 user.FailureReason,
			"last_webhook_at":                user.LastWebhookAt,
			"last_ping_at":                   user.LastPingAt,
			"pending_checkout_ping_deadline": user.PendingCheckoutPingDeadline,
		},
	}})
}

func HandleSubscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	account := currentAccountFromRequest(r)
	if account.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Plan          string `json:"plan"`
		RequestOrigin string `json:"request_origin"`
		Unregister    bool   `json:"unregister"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := GetConfig()
	if req.Unregister && strings.EqualFold(strings.TrimSpace(req.Plan), "none") {
		if err := cfg.UnregisterUserWithRequest(r.Context(), UnregisterUserRequest{Username: account.Username}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		revokeSessionTokensFromRequest(r.Context(), r)
		clearSessionCookies(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "subscription canceled and account removed", "unregistered": true})
		return
	}
	updated, err := cfg.UpdateSubscriptionPlanWithRequest(r.Context(), UpdateSubscriptionPlanRequest{Username: account.Username, SubscriptionPlan: req.Plan, RequestOrigin: req.RequestOrigin})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(updated, r.Context()))
}

func HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	account := currentAccountFromRequest(r)
	if account.Username == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := GetConfig()
	updated, err := cfg.ChangePasswordWithRequest(r.Context(), ChangePasswordRequest{Username: account.Username, CurrentPassword: req.CurrentPassword, NewPassword: req.NewPassword})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(updated, r.Context()))
}

func HandleCheckoutSession(w http.ResponseWriter, r *http.Request) {
	account := currentAccountFromRequest(r)
	username := strings.TrimSpace(account.Username)
	slog.Info("siteaccount.checkout.request", "path", r.URL.Path, "username", username, "auth_roles", strings.Join(account.Roles, ","), "remote_addr", r.RemoteAddr)
	if username == "" {
		slog.Warn("siteaccount.checkout.unauthenticated", "path", r.URL.Path)
		writeJSONError(w, http.StatusUnauthorized, "authentication required before checkout")
		return
	}
	var req struct {
		Plan                 string `json:"plan"`
		Name                 string `json:"name"`
		Email                string `json:"email"`
		RequestOrigin        string `json:"request_origin"`
		HumanProof           string `json:"human_proof"`
		StripePaymentID      string `json:"stripe_payment_id"`
		StripeSessionID      string `json:"stripe_session_id"`
		StripeCustomerID     string `json:"stripe_customer_id"`
		StripeSubscriptionID string `json:"stripe_subscription_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := GetConfig()
	user, ok, err := cfg.FindUserRecord(r.Context(), username)
	if err != nil {
		slog.Error("siteaccount.checkout.find_user_failed", "username", username, "error", err.Error())
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || user == nil {
		slog.Warn("siteaccount.checkout.user_not_found", "username", username)
		writeJSONError(w, http.StatusNotFound, "account not found")
		return
	}

	// Prevent creating a new checkout session if user already has an active subscription
	if req.StripePaymentID == "" && req.StripeSessionID == "" && user.BillingStatus == "active" {
		slog.Warn("siteaccount.checkout.already_active", "username", username, "current_plan", user.SubscriptionPlan, "requested_plan", req.Plan)
		writeJSONError(w, http.StatusConflict, "You already have an active subscription. Please cancel your current subscription before starting a new one.")
		return
	}

	if req.StripePaymentID == "" && req.StripeSessionID == "" {
		if !validateHumanProof(r, req.HumanProof) {
			slog.Warn("siteaccount.checkout.human_proof_missing", "username", username, "plan", req.Plan)
			writeJSONError(w, http.StatusForbidden, "please confirm you are human before continuing")
			return
		}
	}
	if req.RequestOrigin != "" {
		if normalized := normalizeRequestOrigin(req.RequestOrigin); normalized != "" {
			user.RequestOrigin = normalized
			_, _ = cfg.UpdateSubscriptionPlanWithRequest(r.Context(), UpdateSubscriptionPlanRequest{Username: username, SubscriptionPlan: req.Plan, RequestOrigin: req.RequestOrigin})
		}
	}
	session := &BillingCheckoutSession{}
	if req.StripePaymentID == "" && req.StripeSessionID == "" {
		slog.Info("siteaccount.checkout.creating_session", "username", username, "plan", req.Plan)
		createdSession, err := cfg.CreateCheckoutSession(r.Context(), username, req.Plan, req.Email)
		if err != nil {
			slog.Error("siteaccount.checkout.create_session_failed", "username", username, "plan", req.Plan, "error", err.Error())
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		session = createdSession
		slog.Info("siteaccount.checkout.session_created", "username", username, "plan", req.Plan, "session_id", session.ID, "url", session.URL)
	} else {
		slog.Info("siteaccount.checkout.billing_callback", "username", username, "session_id", req.StripeSessionID, "payment_id", req.StripePaymentID, "subscription_id", req.StripeSubscriptionID)
		user.StripeCustomerID = req.StripeCustomerID
		user.StripeSubID = req.StripeSubscriptionID
		user.BillingStatus = "active"
		user.SubscriptionPlan = req.Plan
		user.UpdatedAt = time.Now().UTC()
		if _, err := cfg.UpdateSubscriptionPlanWithRequest(r.Context(), UpdateSubscriptionPlanRequest{Username: username, SubscriptionPlan: req.Plan, RequestOrigin: req.RequestOrigin}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	accessToken, refreshToken, sessionErr := sopbackend.CreateSession(r.Context(), username, sop.RoleUser)
	if sessionErr != nil {
		slog.Error("siteaccount.checkout.session_create_failed", "username", username, "error", sessionErr.Error())
		writeJSONError(w, http.StatusInternalServerError, sessionErr.Error())
		return
	}
	slog.Info("siteaccount.checkout.session_cookie_set", "username", username, "access_token_prefix", shortTokenPrefix(accessToken))
	setSessionCookies(w, accessToken, refreshToken, r.TLS != nil)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "checkout": map[string]any{"id": session.ID, "url": session.URL, "status": session.Status, "payment_id": req.StripePaymentID, "session_id": req.StripeSessionID}})
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
	cfg := GetConfig()
	if err := cfg.HandleBillingWebhook(r.Context(), BillingWebhookEvent{EventType: eventType, EventID: eventID, UserID: userID, Plan: plan}); err != nil {
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
