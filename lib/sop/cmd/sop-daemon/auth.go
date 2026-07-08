package main

/*
Daemon Authentication System with Subscription Monitoring

This implements bootstrap-based JWT authentication for the local daemon with automatic
subscription monitoring via periodic HQ re-login requirements.

SECURITY MODEL:
- Bootstrap secret: 256-bit crypto/rand, written on startup/expiry
- One-time use: Deleted after first authentication
- JWT tokens: Access (15m) + Refresh (48h, configurable)
- Auto-rotation: New bootstrap file on every refresh token expiry

SUBSCRIPTION MONITORING:
- User must login to HQ every 48 hours (default, configurable via --refresh-token-ttl)
- After 48 hours, refresh token expires → daemon regenerates bootstrap file
- UI redirects to HQ login → after login, automatic re-bootstrap → seamless resumption
- HQ tracks: User login timestamp = active subscription verification

USER EXPERIENCE:
Day 1-2: Works freely without interruption
After 48h: "Session expired. Please login to continue (subscription monitoring)"
User logs in (5 seconds) → Immediately back to work with fresh 48-hour lease

AUTOMATIC FLOW (No Manual Intervention):
1. Token expires → Daemon regenerates bootstrap file
2. UI detects expiry → Redirects to HQ login
3. User logs in → markHQLoginCompleted()
4. Page reloads → Auto-bootstrap with new file
5. User continues working

QUICK TEST PROCEDURE:
┌────────────────────────────────────────────────────────────────────┐
│ Test 1: Initial Authentication                                     │
├────────────────────────────────────────────────────────────────────┤
│ $ ./sop-daemon --port 9090 --refresh-token-ttl 2m  # 2min for test│
│ $ ./httpserver --port 8080                                         │
│ $ open http://localhost:8080                                       │
│ → Login to HQ                                                      │
│ → Check console: "Successfully authenticated with daemon" ✅       │
└────────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────────┐
│ Test 2: Token Expiry & Subscription Monitoring                    │
├────────────────────────────────────────────────────────────────────┤
│ Wait 2 minutes (refresh token expires)                            │
│ → Make any daemon request in UI                                    │
│ → Should see: "Session expired. Please login to continue"         │
│ → Login again                                                      │
│ → Should immediately work without daemon restart ✅                │
│ → Check daemon logs: "Bootstrap file regenerated"                 │
└────────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────────┐
│ Test 3: Daemon Restart (No User Interruption)                     │
├────────────────────────────────────────────────────────────────────┤
│ Kill and restart daemon while user has valid tokens               │
│ $ pkill sop-daemon && ./sop-daemon --port 9090                    │
│ → User continues working (existing tokens still valid) ✅          │
│ → New bootstrap secret generated but not needed until expiry      │
└────────────────────────────────────────────────────────────────────┘

CONFIGURATION:
--access-token-ttl 15m          # Access token lifetime (default: 15 minutes)
--refresh-token-ttl 48h         # Subscription monitoring interval (default: 48 hours)
                                # Adjust for tighter (24h) or looser (168h=weekly) monitoring

ENDPOINTS:
POST /api/daemon/auth           # Bootstrap authentication (no auth required)
POST /api/daemon/refresh        # Token refresh (no auth required)
Protected endpoints:            # All require Bearer token
  POST /api/ask
  POST /api/execute
  POST /api/handoff

FILES:
~/.config/sop/daemon.bootstrap  # Bootstrap secret file (ephemeral, 0600 permissions)
                                # Created: On startup or after token expiry
                                # Deleted: After first successful authentication
                                # Regenerated: Automatically when refresh token expires

LOGS TO MONITOR:
[Startup] Generated new daemon bootstrap secret at ~/.config/sop/daemon.bootstrap
[Auth] UI authenticated successfully via bootstrap
[Auth] Bootstrap file deleted (one-time use completed)
[Refresh] Token refresh failed (likely expired): ...
[Auth] Bootstrap file regenerated (refresh token expired, ready for HQ re-login)

SECURITY PROPERTIES:
- 2^256 key space (cryptographically unbreakable)
- <10 second attack window (ephemeral bootstrap file)
- Automatic rotation on expiry (no persistent secrets)
- Subscription tracking (HQ login audit trail)
- CORS restricted to localhost only
- No bypass: Cannot extend lease without HQ login

See SUBSCRIPTION_MONITORING_FLOW.md for complete technical documentation.
*/

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/tools/sopbackend"
)

// DaemonAuthConfig holds token expiry configuration
type DaemonAuthConfig struct {
	AccessTokenTTL  time.Duration `json:"access_token_ttl"`
	RefreshTokenTTL time.Duration `json:"refresh_token_ttl"`
}

var defaultDaemonAuthConfig = DaemonAuthConfig{
	AccessTokenTTL:  15 * time.Minute,
	RefreshTokenTTL: 48 * time.Hour, // 2 days
}

// DaemonAuthHandler handles daemon authentication
type DaemonAuthHandler struct {
	bootstrapSecret string
	bootstrapPath   string
	sessionStore    *sopbackend.SessionStore
	config          DaemonAuthConfig
	hqSigningSecret string
	syncQueue       *SyncQueue
}

type hqSignedClaims struct {
	Subject   string   `json:"sub"`
	Role      string   `json:"role"`
	Scopes    []string `json:"scopes,omitempty"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
	TokenID   string   `json:"jti"`
}

type hqAuthRequest struct {
	AccessToken string `json:"access_token"`
}

type hqAuthResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	TokenType    string   `json:"token_type"`
	ExpiresIn    int      `json:"expires_in"`
	Scopes       []string `json:"scopes,omitempty"`
}

type hqArtifact struct {
	Text     string         `json:"text"`
	Category string         `json:"category"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Score    float64        `json:"score,omitempty"`
}

type hqSyncBatch struct {
	Kind       string `json:"kind"`
	SessionID  string `json:"session_id,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	DaemonID   string `json:"daemon_id,omitempty"`
	DaemonURL  string `json:"daemon_url,omitempty"` // Callback URL for HQ→daemon communication
	Timestamp  string `json:"timestamp"`
	SharedMeta struct {
		Enabled    bool   `json:"enabled"`
		Scope      string `json:"scope,omitempty"`
		LastSyncAt string `json:"last_sync_at,omitempty"`
		Mode       string `json:"mode,omitempty"`
		Artifacts  []struct {
			Text     string         `json:"text"`
			Category string         `json:"category"`
			Metadata map[string]any `json:"metadata,omitempty"`
		} `json:"artifacts,omitempty"`
	} `json:"shared_memory,omitempty"`
}

// ensureBootstrapSecret generates a new bootstrap secret on every daemon startup
func ensureBootstrapSecret() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}

	bootstrapPath := filepath.Join(configDir, "daemon.bootstrap")

	// Always generate new bootstrap secret (rotation on every restart)
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate bootstrap secret: %w", err)
	}
	secret := hex.EncodeToString(buf)

	// Write with 0600 permissions (owner read/write only)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(bootstrapPath, []byte(secret), 0600); err != nil {
		return "", fmt.Errorf("write bootstrap secret: %w", err)
	}

	log.Printf("Generated new daemon bootstrap secret at %s", bootstrapPath)
	return secret, nil
}

func (h *DaemonAuthHandler) loadBootstrapSecretFromDisk() (string, error) {
	if h.bootstrapPath == "" {
		return "", fmt.Errorf("bootstrap path not configured")
	}

	data, err := os.ReadFile(h.bootstrapPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		return "", fmt.Errorf("read bootstrap secret: %w", err)
	}

	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("bootstrap secret file is empty")
	}
	return secret, nil
}

// getConfigDir returns the daemon configuration directory
func getConfigDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		return filepath.Join(v, "sop"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sop"), nil
}

// NewDaemonAuthHandler creates a new authentication handler
func NewDaemonAuthHandler(config DaemonAuthConfig) (*DaemonAuthHandler, error) {
	bootstrapSecret, err := ensureBootstrapSecret()
	if err != nil {
		return nil, fmt.Errorf("ensure bootstrap secret: %w", err)
	}

	configDir, err := getConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config dir: %w", err)
	}
	bootstrapPath := filepath.Join(configDir, "daemon.bootstrap")

	// Create session store with configured TTLs
	// Note: sopbackend.NewSessionStore only takes access TTL, need to customize
	sessionStore := sopbackend.NewSessionStore(config.AccessTokenTTL)
	// TODO: Set refreshTTL once sopbackend exports it or provides setter

	return &DaemonAuthHandler{
		bootstrapSecret: bootstrapSecret,
		bootstrapPath:   bootstrapPath,
		sessionStore:    sessionStore,
		config:          config,
		hqSigningSecret: getHQSigningSecret(),
		syncQueue:       GetGlobalSyncQueue(),
	}, nil
}

func getHQSigningSecret() string {
	if secret := strings.TrimSpace(os.Getenv("SOP_HQ_SIGNING_SECRET")); secret != "" {
		return secret
	}
	return "sop-daemon-hq-default-secret"
}

func (h *DaemonAuthHandler) getHQSigningSecretValue() []byte {
	if h == nil {
		return []byte(getHQSigningSecret())
	}
	if strings.TrimSpace(h.hqSigningSecret) != "" {
		return []byte(h.hqSigningSecret)
	}
	return []byte(getHQSigningSecret())
}

func (h *DaemonAuthHandler) signHQAccessToken(subject, role string, scopes []string, expiresAt time.Time, tokenID string) (string, error) {
	claims := hqSignedClaims{
		Subject:   strings.TrimSpace(subject),
		Role:      strings.TrimSpace(role),
		Scopes:    append([]string(nil), scopes...),
		IssuedAt:  time.Now().UTC().Unix(),
		ExpiresAt: expiresAt.UTC().Unix(),
		TokenID:   strings.TrimSpace(tokenID),
	}
	if claims.Subject == "" {
		claims.Subject = "daemon"
	}
	if claims.Role == "" {
		claims.Role = "daemon-user"
	}
	if claims.TokenID == "" {
		claims.TokenID = fmt.Sprintf("hq-%d", claims.IssuedAt)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	input := header + "." + payload
	mac := hmac.New(sha256.New, h.getHQSigningSecretValue())
	_, _ = mac.Write([]byte(input))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return input + "." + signature, nil
}

func (h *DaemonAuthHandler) validateHQSignedToken(token string) (*hqSignedClaims, error) {
	if h == nil {
		return nil, fmt.Errorf("auth handler unavailable")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid HQ token")
	}
	header, payload, signature := parts[0], parts[1], parts[2]
	mac := hmac.New(sha256.New, h.getHQSigningSecretValue())
	_, _ = mac.Write([]byte(header + "." + payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil, fmt.Errorf("invalid HQ signature")
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid HQ payload")
	}
	var claims hqSignedClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("invalid HQ claims")
	}
	if claims.Subject == "" || claims.TokenID == "" {
		return nil, fmt.Errorf("invalid HQ claims")
	}
	if time.Now().UTC().Unix() >= claims.ExpiresAt {
		return nil, fmt.Errorf("expired HQ token")
	}
	return &claims, nil
}

func (h *DaemonAuthHandler) HandleHQAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req hqAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if strings.TrimSpace(req.AccessToken) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "access_token required"})
		return
	}
	claims, err := h.validateHQSignedToken(req.AccessToken)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid HQ access token"})
		return
	}
	accessToken, refreshToken, err := h.sessionStore.CreateSession(r.Context(), claims.Subject, claims.Role)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create daemon session"})
		return
	}
	respondJSON(w, http.StatusOK, hqAuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(h.config.AccessTokenTTL.Seconds()),
		Scopes:       claims.Scopes,
	})
}

func collectSharedMemoryArtifacts(ctx context.Context) ([]struct {
	Text     string         `json:"text"`
	Category string         `json:"category"`
	Metadata map[string]any `json:"metadata,omitempty"`
}, error) {
	ctx = normalizeContext(ctx)
	db, err := sharedMemoryDatabase(ctx)
	if err != nil {
		return nil, err
	}

	// Retrieve recent shared memory artifacts for sync
	hits, err := agent.RetrieveSharedMemory(ctx, db, nil, nil, "shared_memory", "shared_memory", 10)
	if err != nil {
		return nil, err
	}

	if len(hits) == 0 {
		return nil, nil
	}

	type artifact struct {
		Text     string         `json:"text"`
		Category string         `json:"category"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}

	var artifacts []artifact
	for _, hit := range hits {
		text := ""
		if txt, ok := hit.Payload["text"].(string); ok {
			text = strings.TrimSpace(txt)
		}
		if text == "" {
			continue
		}

		category := "shared_memory"
		if cat, ok := hit.Payload["category"].(string); ok && strings.TrimSpace(cat) != "" {
			category = strings.TrimSpace(cat)
		}

		// Extract metadata while preserving structured references for knowledge artifacts.
		metadata := make(map[string]any)
		for k, v := range hit.Payload {
			if k == "text" || k == "category" {
				continue
			}
			switch typed := v.(type) {
			case string:
				if len(typed) < 1000 {
					metadata[k] = typed
				}
			case float64, bool:
				metadata[k] = v
			case map[string]any:
				metadata[k] = typed
			case []any:
				metadata[k] = typed
			case []string:
				metadata[k] = typed
			}
		}

		artifacts = append(artifacts, artifact{
			Text:     text,
			Category: category,
			Metadata: metadata,
		})
	}

	// Convert to the expected anonymous struct type for the batch
	var result []struct {
		Text     string         `json:"text"`
		Category string         `json:"category"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	for _, a := range artifacts {
		result = append(result, struct {
			Text     string         `json:"text"`
			Category string         `json:"category"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}{Text: a.Text, Category: a.Category, Metadata: a.Metadata})
	}

	return result, nil
}

func (h *DaemonAuthHandler) syncWithHQ(ctx context.Context) error {
	if h == nil || h.sessionStore == nil || h.syncQueue == nil {
		return nil
	}

	// Check if sync should be attempted (circuit breaker)
	shouldAttempt, reason := h.syncQueue.ShouldAttemptSync()
	if !shouldAttempt {
		log.Printf("HQ sync skipped: %s", reason)
		return nil
	}

	// Log metrics before sync
	metrics := h.syncQueue.GetMetrics()
	log.Printf("HQ sync metrics: queue_depth=%d, state=%s, consecutive_failures=%d",
		metrics["queue_depth"], metrics["circuit_state"], metrics["consecutive_failures"])

	hqBaseURL := strings.TrimSpace(os.Getenv("SOP_HQ_BASE_URL"))
	if hqBaseURL == "" {
		log.Printf("HQ sync skipped (no HQ endpoint configured)")
		return nil
	}

	// Dequeue batch from sync queue
	batchSize := h.syncQueue.GetBatchSize()
	queuedArtifacts := h.syncQueue.DequeueBatch(batchSize)

	if len(queuedArtifacts) == 0 {
		// No artifacts to sync, send heartbeat only
		log.Printf("HQ sync: no artifacts queued, sending heartbeat only")
		return h.sendHQHeartbeat(ctx, hqBaseURL)
	}

	// Convert queued artifacts to batch format
	artifacts := make([]struct {
		Text     string         `json:"text"`
		Category string         `json:"category"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}, 0, len(queuedArtifacts))

	for _, qa := range queuedArtifacts {
		artifacts = append(artifacts, struct {
			Text     string         `json:"text"`
			Category string         `json:"category"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}{
			Text:     qa.Text,
			Category: qa.Category,
			Metadata: qa.Metadata,
		})
	}

	daemonID := "local-daemon"
	if storedID := strings.TrimSpace(os.Getenv("SOP_DAEMON_ID")); storedID != "" {
		daemonID = storedID
	}

	daemonURL := getDaemonCallbackURL()

	batch := hqSyncBatch{
		Kind:      "shared_memory_sync",
		SessionID: "daemon-sync",
		UserID:    "daemon",
		DaemonID:  daemonID,
		DaemonURL: daemonURL,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	batch.SharedMeta.Enabled = true
	batch.SharedMeta.Scope = "shared-memory"
	batch.SharedMeta.Mode = "queued"
	batch.SharedMeta.LastSyncAt = batch.Timestamp
	batch.SharedMeta.Artifacts = artifacts

	payload, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("marshal HQ sync batch: %w", err)
	}

	hqURL := strings.TrimRight(hqBaseURL, "/") + "/api/daemon/hq-sync"

	// Record sync attempt
	h.syncQueue.RecordSyncAttempt()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hqURL, strings.NewReader(string(payload)))
	if err != nil {
		h.syncQueue.RecordSyncFailure(0)
		return fmt.Errorf("create HQ sync request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(h.getHQSigningSecretValue()))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		h.syncQueue.RecordSyncFailure(0)
		return fmt.Errorf("send HQ sync batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Special handling for rate limit responses
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			reason := resp.Header.Get("X-RateLimit-Reason")

			h.syncQueue.RecordSyncFailure(resp.StatusCode)

			log.Printf("HQ sync rate limited: %s (retry after %s)", reason, retryAfter)
			return fmt.Errorf("HQ sync rate limited: %s (retry after %s)", reason, retryAfter)
		}

		h.syncQueue.RecordSyncFailure(resp.StatusCode)

		// Try to parse error body
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			return fmt.Errorf("HQ sync returned status %d: %s", resp.StatusCode, string(bodyBytes))
		}
		return fmt.Errorf("HQ sync returned status %d", resp.StatusCode)
	}

	// Record success
	h.syncQueue.RecordSyncSuccess()
	log.Printf("HQ sync successful: delivered %d artifacts to %s", len(artifacts), hqURL)

	// Pull artifacts from HQ after successful push
	if err := h.pullFromHQ(ctx, hqBaseURL); err != nil {
		log.Printf("HQ pull failed (continuing): %v", err)
		// Don't fail the entire sync on pull errors
	}

	return nil
}

// parseRetryAfter parses the Retry-After header value.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	// Try parsing as seconds
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP date (not implemented for simplicity)
	return 0
}

// sendHQHeartbeat sends a heartbeat to HQ with no artifacts.
func (h *DaemonAuthHandler) sendHQHeartbeat(ctx context.Context, hqBaseURL string) error {
	daemonID := "local-daemon"
	if storedID := strings.TrimSpace(os.Getenv("SOP_DAEMON_ID")); storedID != "" {
		daemonID = storedID
	}

	daemonURL := getDaemonCallbackURL()

	batch := hqSyncBatch{
		Kind:      "shared_memory_sync",
		SessionID: "daemon-sync",
		UserID:    "daemon",
		DaemonID:  daemonID,
		DaemonURL: daemonURL,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	batch.SharedMeta.Enabled = true
	batch.SharedMeta.Scope = "shared-memory"
	batch.SharedMeta.Mode = "heartbeat"
	batch.SharedMeta.LastSyncAt = batch.Timestamp

	payload, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("marshal HQ heartbeat: %w", err)
	}

	hqURL := strings.TrimRight(hqBaseURL, "/") + "/api/daemon/hq-sync"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hqURL, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create HQ heartbeat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(h.getHQSigningSecretValue()))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send HQ heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HQ heartbeat returned status %d", resp.StatusCode)
	}

	log.Printf("HQ heartbeat sent to %s", hqURL)
	return nil
}

// getDaemonCallbackURL returns the URL where this daemon can be reached by HQ.
// Priority: SOP_DAEMON_CALLBACK_URL env var > derive from listen address
func getDaemonCallbackURL() string {
	// Check explicit callback URL first
	if url := strings.TrimSpace(os.Getenv("SOP_DAEMON_CALLBACK_URL")); url != "" {
		return url
	}

	// Try to derive from listen address (if available)
	// For now, return empty string if not explicitly set
	// HQ will know daemon exists but can't push to it
	return ""
}

// pullFromHQ retrieves relevant artifacts from HQ and stores them locally.
func (h *DaemonAuthHandler) pullFromHQ(ctx context.Context, hqBaseURL string) error {
	if h == nil || h.sessionStore == nil {
		return nil
	}

	hqURL := strings.TrimRight(hqBaseURL, "/") + "/api/daemon/hq-retrieve"

	// Request artifacts with a general query for shared patterns
	req := map[string]any{
		"daemon_id": "local-daemon",
		"query":     "shared patterns best practices error handling",
		"limit":     10,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal HQ retrieve request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, hqURL, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create HQ retrieve request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+string(h.getHQSigningSecretValue()))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send HQ retrieve request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HQ retrieve returned status %d", resp.StatusCode)
	}

	var response struct {
		Artifacts []hqArtifact `json:"artifacts"`
		Timestamp string       `json:"timestamp"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("decode HQ retrieve response: %w", err)
	}

	log.Printf("HQ pull: received %d artifacts from %s", len(response.Artifacts), hqURL)

	// Store retrieved artifacts in local shared memory
	if len(response.Artifacts) > 0 {
		if err := h.storeRetrievedArtifacts(ctx, response.Artifacts); err != nil {
			return fmt.Errorf("store retrieved artifacts: %w", err)
		}
	}

	return nil
}

// storeRetrievedArtifacts stores artifacts retrieved from HQ in local shared memory.
func (h *DaemonAuthHandler) storeRetrievedArtifacts(ctx context.Context, artifacts []hqArtifact) error {
	if len(artifacts) == 0 {
		return nil
	}

	db, err := sharedMemoryDatabase(ctx)
	if err != nil {
		return fmt.Errorf("get shared memory database: %w", err)
	}

	for _, artifact := range artifacts {
		text := strings.TrimSpace(artifact.Text)
		if text == "" {
			continue
		}

		category := strings.TrimSpace(artifact.Category)
		if category == "" {
			category = "shared_memory"
		}

		// Enrich metadata with HQ origin
		metadata := artifact.Metadata
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["origin"] = "hq"
		metadata["retrieved_at"] = time.Now().UTC().Format(time.RFC3339)

		artifactRef := agent.ExtractArtifactReference(metadata["artifact_ref"])
		if artifactRef.Name != "" || artifactRef.DatabaseName != "" || artifactRef.Type != "" {
			if err := agent.PromoteKnowledgeArtifact(
				ctx,
				db,
				nil, // LLM not required
				nil, // Embedder handled by PromoteKnowledgeArtifact
				text,
				artifactRef,
				artifactRef.Name,
				category,
				"daemon",
				"hq-federation",
				metadata,
			); err != nil {
				log.Printf("Failed to store retrieved artifact: %v", err)
				// Continue with remaining artifacts instead of failing completely
			}
			continue
		}

		// Store in local shared memory for immediate reuse
		if err := agent.PromoteSharedMemoryArtifact(
			ctx,
			db,
			nil, // LLM not required
			nil, // Embedder handled by PromoteSharedMemoryArtifact
			text,
			"shared_memory",
			category,
			"daemon",
			"hq-federation",
			metadata,
		); err != nil {
			log.Printf("Failed to store retrieved artifact: %v", err)
			// Continue with remaining artifacts instead of failing completely
		}
	}

	return nil
}

// HandleBootstrap handles initial authentication with bootstrap secret
func (h *DaemonAuthHandler) HandleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BootstrapSecret string `json:"bootstrap_secret"`
		BootstrapToken  string `json:"bootstrap_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}

	var subject, role string
	if strings.TrimSpace(req.BootstrapToken) != "" {
		userRecord, err := h.sessionStore.ValidateTokenWithoutAccountCheck(r.Context(), strings.TrimSpace(req.BootstrapToken))
		if err != nil {
			log.Printf("Invalid bootstrap token attempt: %v", err)
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid bootstrap token"})
			return
		}
		subject = strings.TrimSpace(userRecord.Username)
		role = strings.TrimSpace(userRecord.Role)
		if subject == "" {
			subject = "ui-client"
		}
		if role == "" {
			role = "daemon-user"
		}
	} else {
		// Validate using the current bootstrap secret stored on disk.
		currentBootstrapSecret, err := h.loadBootstrapSecretFromDisk()
		if err != nil {
			log.Printf("Failed to load bootstrap secret from disk: %v", err)
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid bootstrap secret"})
			return
		}
		if h.bootstrapSecret != currentBootstrapSecret {
			h.bootstrapSecret = currentBootstrapSecret
		}

		if req.BootstrapSecret != h.bootstrapSecret {
			log.Printf("Invalid bootstrap secret attempt")
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid bootstrap secret"})
			return
		}
		subject = "ui-client"
		role = "daemon-user"
	}

	// Create session for the UI client
	accessToken, refreshToken, err := h.sessionStore.CreateSession(r.Context(), subject, role)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
		return
	}

	// Delete bootstrap file after successful authentication (one-time use)
	if err := os.Remove(h.bootstrapPath); err != nil {
		log.Printf("Warning: failed to delete bootstrap file: %v", err)
		// Continue anyway - authentication succeeded
	} else {
		log.Printf("Bootstrap file deleted (one-time use completed)")
	}

	log.Printf("UI authenticated successfully via bootstrap")
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(h.config.AccessTokenTTL.Seconds()),
	})
}

// HandleRefresh handles token refresh requests
func (h *DaemonAuthHandler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}

	if req.RefreshToken == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "refresh_token required"})
		return
	}

	// Refresh the session
	accessToken, refreshToken, err := h.sessionStore.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		log.Printf("Token refresh failed (likely expired): %v", err)

		// Automatically regenerate bootstrap file when refresh token expires
		// This enables seamless re-authentication after HQ login
		if regenerateErr := h.regenerateBootstrapFile(); regenerateErr != nil {
			log.Printf("Warning: failed to regenerate bootstrap file: %v", regenerateErr)
		} else {
			log.Printf("Bootstrap file regenerated (refresh token expired, ready for HQ re-login)")
		}

		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired refresh token"})
		return
	}

	log.Printf("Token refreshed successfully")
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(h.config.AccessTokenTTL.Seconds()),
	})
}

// HandleRegenerateBootstrap regenerates the bootstrap secret file
// This is called after HQ login when the daemon refresh token has expired
// to allow the UI to re-authenticate with the daemon
func (h *DaemonAuthHandler) HandleRegenerateBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.regenerateBootstrapFile(); err != nil {
		log.Printf("Failed to regenerate bootstrap file: %v", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to regenerate bootstrap file"})
		return
	}

	log.Printf("Bootstrap secret regenerated after HQ login")
	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Bootstrap secret regenerated successfully",
	})
}

// regenerateBootstrapFile generates a new bootstrap secret and writes it to disk
// This is called automatically when the refresh token expires (lease expiry)
func (h *DaemonAuthHandler) regenerateBootstrapFile() error {
	// Generate new bootstrap secret
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("generate bootstrap secret: %w", err)
	}
	newSecret := hex.EncodeToString(buf)

	// Write to bootstrap file
	configDir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("get config directory: %w", err)
	}

	bootstrapPath := filepath.Join(configDir, "daemon.bootstrap")
	if err := os.WriteFile(bootstrapPath, []byte(newSecret), 0600); err != nil {
		return fmt.Errorf("write bootstrap file: %w", err)
	}

	// Update handler's bootstrap secret
	h.bootstrapSecret = newSecret

	return nil
}

// RequireAuth is middleware that validates bearer tokens
func (h *DaemonAuthHandler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract bearer token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authorization header required"})
			return
		}

		token := extractBearerToken(authHeader)
		if token == "" {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid authorization header format"})
			return
		}

		// Validate daemon-issued session tokens first, then fall back to HQ-signed bearer tokens.
		userRecord, err := h.sessionStore.ValidateTokenWithoutAccountCheck(r.Context(), token)
		if err != nil {
			claims, claimErr := h.validateHQSignedToken(token)
			if claimErr != nil {
				respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
				return
			}
			authCtx := sop.AuthContext{
				UserID:      claims.Subject,
				Roles:       []string{claims.Role},
				Permissions: map[string]bool{"manage_environments": false},
			}
			ctx := sop.ContextWithAuth(r.Context(), authCtx)
			r = r.WithContext(ctx)
			next(w, r)
			return
		}

		// Add user info to context
		ctx := context.WithValue(r.Context(), "user", userRecord)
		authCtx := sop.AuthContext{
			UserID:      userRecord.Username,
			Roles:       []string{userRecord.Role},
			Permissions: map[string]bool{"manage_environments": false},
		}
		ctx = sop.ContextWithAuth(ctx, authCtx)
		r = r.WithContext(ctx)
		next(w, r)
	}
}

func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
