package siteaccount

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
	"golang.org/x/crypto/bcrypt"
)

// RegisterHandlers wires the site account handlers to the site backend mux.
func RegisterHandlers(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/api/auth/register", HandleRegister)
	mux.HandleFunc("/api/auth/human-proof/challenge", HandleHumanProofChallenge)
	mux.HandleFunc("/api/site/auth/login", HandleLogin)
	mux.HandleFunc("/api/site/auth/logout", HandleLogout)
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
		Password:         req.Password,
		Email:            req.Email,
		Role:             req.Role,
		SubscriptionPlan: req.SubscriptionPlan,
		RequestOrigin:    req.RequestOrigin,
	})
	if err != nil {
		slog.Warn("siteaccount.register.failed", "username", req.Username, "error", err.Error())
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	slog.Info("siteaccount.register.success", "username", user.Username, "role", user.Role, "plan", user.SubscriptionPlan)
	SetConfig(cfg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(buildUserResponsePayload(user))
}

func parseLoginRequest(r *http.Request) (struct {
	Username string `json:"username"`
	Password string `json:"password"`
}, error) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return req, err
		}
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
		return req, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, err
	}
	return req, nil
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

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := parseLoginRequest(r)
	if err != nil {
		slog.Warn("siteaccount.login.invalid_body", "path", r.URL.Path, "error", err.Error())
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	slog.Info("siteaccount.login.request", "path", r.URL.Path, "username", req.Username, "remote_addr", r.RemoteAddr, "user_agent", r.UserAgent())
	cfg := GetConfig()
	if cfg == nil {
		writeJSONError(w, http.StatusInternalServerError, "auth config unavailable")
		return
	}

	user, found, findErr := cfg.FindUserRecord(r.Context(), normalizeUsername(req.Username))
	if findErr != nil {
		writeJSONError(w, http.StatusInternalServerError, findErr.Error())
		return
	}
	if !found || user == nil {
		slog.Warn("siteaccount.login.user_not_found", "username", normalizeUsername(req.Username))
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	passwordHash := strings.TrimSpace(user.PasswordHash)
	passwordMatches := false
	if passwordHash != "" {
		passwordMatches = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)) == nil
	}

	if !passwordMatches {
		sharedCfg := sopbackend.GetConfig()
		sharedUser, sharedFound, sharedErr := sharedCfg.FindUserRecord(r.Context(), normalizeUsername(req.Username))
		if sharedErr != nil {
			writeJSONError(w, http.StatusInternalServerError, sharedErr.Error())
			return
		}
		if sharedFound && sharedUser != nil {
			sharedPasswordHash := strings.TrimSpace(sharedUser.PasswordHash.Plaintext())
			if sharedPasswordHash != "" {
				passwordMatches = bcrypt.CompareHashAndPassword([]byte(sharedPasswordHash), []byte(req.Password)) == nil
			}
		}
	}

	if !passwordMatches {
		slog.Warn("siteaccount.login.password_mismatch", "username", normalizeUsername(req.Username), "site_password_hash_present", passwordHash != "")
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	accessToken, refreshToken, err := sopbackend.CreateSession(r.Context(), user.Username, user.Role)
	if err != nil {
		slog.Error("siteaccount.login.session_create_failed", "username", user.Username, "role", user.Role, "error", err.Error())
		writeJSONError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	slog.Info("siteaccount.login.session_created", "username", user.Username, "role", user.Role, "access_token_prefix", shortTokenPrefix(accessToken))
	setSessionCookies(w, accessToken, refreshToken, r.TLS != nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"user":          map[string]any{"username": user.Username, "role": user.Role},
	})
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
	clearSessionCookies(w)
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
			"request_origin":    user.RequestOrigin,
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
	slog.Info("siteaccount.status.request", "path", r.URL.Path, "username", account.Username, "auth_roles", strings.Join(account.Roles, ","), "remote_addr", r.RemoteAddr)
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
		Plan          string `json:"plan"`
		RequestOrigin string `json:"request_origin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := GetConfig()
	updated, err := cfg.UpdateSubscriptionPlanWithRequest(r.Context(), UpdateSubscriptionPlanRequest{Username: account.Username, SubscriptionPlan: req.Plan, RequestOrigin: req.RequestOrigin})
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
	username := strings.TrimSpace(account.Username)
	slog.Info("siteaccount.checkout.request", "path", r.URL.Path, "username", username, "auth_roles", strings.Join(account.Roles, ","), "remote_addr", r.RemoteAddr)
	if username == "" {
		slog.Warn("siteaccount.checkout.unauthenticated", "path", r.URL.Path)
		writeJSONError(w, http.StatusUnauthorized, "authentication required before checkout")
		return
	}
	var req struct {
		Plan                 string `json:"plan"`
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
		createdSession, err := cfg.CreateCheckoutSession(r.Context(), username, req.Plan)
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
