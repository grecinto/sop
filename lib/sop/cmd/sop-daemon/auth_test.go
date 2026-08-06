package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestResolveBootstrapPathForClientUsesDistinctFiles(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "daemon.bootstrap")

	if got := resolveBootstrapPathForClient(basePath, ""); got != "" {
		t.Fatalf("expected empty client to resolve to no path, got %s", got)
	}
	if got := resolveBootstrapPathForClient(basePath, "ui-a"); got != filepath.Join(filepath.Dir(basePath), "daemon.bootstrap.ui-a") {
		t.Fatalf("expected client-specific path, got %s", got)
	}
}

func TestNewDaemonAuthHandlerCreatesUIBootstrapSecretFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := NewDaemonAuthHandler(DaemonAuthConfig{AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour})
	if err != nil {
		t.Fatalf("new daemon auth handler: %v", err)
	}

	uiBootstrapPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sop", fmt.Sprintf("daemon.bootstrap.%s", uiBootstrapClientID))
	data, err := os.ReadFile(uiBootstrapPath)
	if err != nil {
		t.Fatalf("read ui bootstrap file: %v", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatalf("expected ui bootstrap file to contain a secret")
	}
}

func TestHandleBootstrapUsesInMemorySessionsWithoutSystemDB(t *testing.T) {
	oldCfg := sopbackend.GetConfig()
	sopbackend.SetConfig(sopbackend.Config{})
	t.Cleanup(func() {
		sopbackend.SetConfig(oldCfg)
	})

	h := &DaemonAuthHandler{
		bootstrapSecret: "shared-secret",
		sessionStore:    newInMemoryDaemonSessionStore(time.Minute, time.Hour),
	}

	challenge := h.issueBootstrapChallenge()
	if challenge == nil {
		t.Fatal("expected bootstrap challenge")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(signedBootstrapBodyWithClientID("shared-secret", challenge, "bootstrap-client")))
	rr := httptest.NewRecorder()

	h.HandleBootstrap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestDaemonRequireAuthPropagatesUserRoleToContext(t *testing.T) {
	withIsolatedSystemDB(t)

	h := &DaemonAuthHandler{sessionStore: sopbackend.NewSessionStore(time.Minute)}
	ctx := context.Background()
	token, _, err := h.sessionStore.CreateSession(ctx, "alice", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	var gotAuth sop.AuthContext
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = sop.GetAuthFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	h.RequireAuth(next)(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	if gotAuth.UserID != "alice" {
		t.Fatalf("expected user id alice, got %q", gotAuth.UserID)
	}
	if len(gotAuth.Roles) != 1 || gotAuth.Roles[0] != sop.RoleAdmin {
		t.Fatalf("expected admin role to be propagated, got %#v", gotAuth.Roles)
	}
}

func TestHandleBootstrapUsesCurrentBootstrapSecretFromDisk(t *testing.T) {
	withIsolatedSystemDB(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	h := &DaemonAuthHandler{
		bootstrapSecret: "stale-secret",
		bootstrapPath:   filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sop", "daemon.bootstrap.ui-client"),
		sessionStore:    sopbackend.NewSessionStore(time.Minute),
	}

	if err := os.MkdirAll(filepath.Dir(h.bootstrapPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(h.bootstrapPath, []byte("fresh-secret"), 0o600); err != nil {
		t.Fatalf("write bootstrap file: %v", err)
	}

	challenge := h.issueBootstrapChallenge()
	if challenge == nil {
		t.Fatal("expected bootstrap challenge")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(signedBootstrapBodyWithClientID("fresh-secret", challenge, "ui-client")))
	rr := httptest.NewRecorder()

	h.HandleBootstrap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestHandleBootstrapRejectsBootstrapTokenWithoutChallenge(t *testing.T) {
	withIsolatedSystemDB(t)

	h := &DaemonAuthHandler{sessionStore: sopbackend.NewSessionStore(time.Minute)}
	accessToken, _, err := h.sessionStore.CreateSession(context.Background(), "alice", "daemon-user")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(fmt.Sprintf(`{"bootstrap_token":"%s"}`, accessToken)))
	rr := httptest.NewRecorder()

	h.HandleBootstrap(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected bootstrap token without challenge to be rejected, got %d with body %s", rr.Code, rr.Body.String())
	}
}

func TestHandleBootstrapRequiresSignedChallengeOnce(t *testing.T) {
	h := &DaemonAuthHandler{
		bootstrapSecret: "shared-secret",
		sessionStore:    newInMemoryDaemonSessionStore(time.Minute, time.Hour),
	}
	challenge := h.issueBootstrapChallenge()
	if challenge == nil {
		t.Fatal("expected bootstrap challenge")
	}

	signed := signBootstrapChallenge("shared-secret", challenge)
	body := fmt.Sprintf(`{"bootstrap_secret":"shared-secret","bootstrap_challenge_id":"%s","bootstrap_challenge_signature":"%s","client_id":"challenge-client"}`, challenge.ChallengeID, signed)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(body))
	firstRR := httptest.NewRecorder()
	h.HandleBootstrap(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("expected first bootstrap to succeed, got %d with body %s", firstRR.Code, firstRR.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(body))
	secondRR := httptest.NewRecorder()
	h.HandleBootstrap(secondRR, secondReq)
	if secondRR.Code == http.StatusOK {
		t.Fatalf("expected replayed bootstrap challenge to be rejected, got %d with body %s", secondRR.Code, secondRR.Body.String())
	}
}

func TestHandleBootstrapRejectsSopbackendSessionTokenWithoutChallenge(t *testing.T) {
	withIsolatedSystemDB(t)

	h := &DaemonAuthHandler{sessionStore: newInMemoryDaemonSessionStore(time.Minute, time.Hour)}
	siteSessionStore := sopbackend.NewSessionStore(time.Minute)
	accessToken, _, err := siteSessionStore.CreateSession(context.Background(), "alice", "daemon-user")
	if err != nil {
		t.Fatalf("create site session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(fmt.Sprintf(`{"bootstrap_token":"%s"}`, accessToken)))
	rr := httptest.NewRecorder()

	h.HandleBootstrap(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected site session token without challenge to be rejected, got %d with body %s", rr.Code, rr.Body.String())
	}
}

func signBootstrapChallenge(secret string, challenge *bootstrapChallenge) string {
	digest := sha256.Sum256([]byte(secret + ":" + challenge.ChallengeID))
	return hex.EncodeToString(digest[:])
}

func signedBootstrapBody(secret string, challenge *bootstrapChallenge) string {
	return signedBootstrapBodyWithClientID(secret, challenge, "")
}

func signedBootstrapBodyWithClientID(secret string, challenge *bootstrapChallenge, clientID string) string {
	signed := signBootstrapChallenge(secret, challenge)
	if strings.TrimSpace(clientID) == "" {
		return fmt.Sprintf(`{"bootstrap_secret":"%s","bootstrap_challenge_id":"%s","bootstrap_challenge_signature":"%s"}`, secret, challenge.ChallengeID, signed)
	}
	return fmt.Sprintf(`{"bootstrap_secret":"%s","bootstrap_challenge_id":"%s","bootstrap_challenge_signature":"%s","client_id":"%s"}`, secret, challenge.ChallengeID, signed, clientID)
}

func TestHandleBootstrapSupportsMultipleAuthenticatedClients(t *testing.T) {
	withIsolatedSystemDB(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	h := &DaemonAuthHandler{
		bootstrapSecret: "shared-secret",
		bootstrapPath:   filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sop", "daemon.bootstrap.ai-studio-ui"),
		sessionStore:    sopbackend.NewSessionStore(time.Minute),
	}

	if err := os.MkdirAll(filepath.Dir(h.bootstrapPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(h.bootstrapPath, []byte(h.bootstrapSecret), 0o600); err != nil {
		t.Fatalf("write bootstrap file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(h.bootstrapPath), "daemon.bootstrap.continue-vscode"), []byte(h.bootstrapSecret), 0o600); err != nil {
		t.Fatalf("write second bootstrap file: %v", err)
	}

	firstChallenge := h.issueBootstrapChallenge()
	if firstChallenge == nil {
		t.Fatal("expected first bootstrap challenge")
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(fmt.Sprintf(`{"bootstrap_secret":"shared-secret","bootstrap_challenge_id":"%s","bootstrap_challenge_signature":"%s","client_id":"ai-studio-ui"}`, firstChallenge.ChallengeID, signBootstrapChallenge("shared-secret", firstChallenge))))
	firstRR := httptest.NewRecorder()
	h.HandleBootstrap(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("expected first bootstrap to succeed, got %d with body %s", firstRR.Code, firstRR.Body.String())
	}

	secondChallenge := h.issueBootstrapChallenge()
	if secondChallenge == nil {
		t.Fatal("expected second bootstrap challenge")
	}
	secondReq := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(fmt.Sprintf(`{"bootstrap_secret":"shared-secret","bootstrap_challenge_id":"%s","bootstrap_challenge_signature":"%s","client_id":"continue-vscode"}`, secondChallenge.ChallengeID, signBootstrapChallenge("shared-secret", secondChallenge))))
	secondRR := httptest.NewRecorder()
	h.HandleBootstrap(secondRR, secondReq)
	if secondRR.Code != http.StatusOK {
		t.Fatalf("expected second bootstrap to succeed, got %d with body %s", secondRR.Code, secondRR.Body.String())
	}

	if got := h.clientSessionRegistry().Count(); got != 2 {
		t.Fatalf("expected 2 authenticated clients in registry, got %d", got)
	}
}

func TestCleanupStaleClientSessionsRemovesExpiredEntries(t *testing.T) {
	h := &DaemonAuthHandler{clientRegistry: newDaemonClientRegistry()}
	registry := h.clientSessionRegistry()
	registry.Register("stale-client", "ui-client", "daemon-user", "token-1")

	entry, ok := registry.Get("stale-client")
	if !ok {
		t.Fatalf("expected stale client to be registered")
	}
	entry.LastSeen = time.Now().Add(-2 * time.Hour)
	registry.clients[entry.ClientID] = entry

	removed := h.cleanupStaleClientSessions(time.Hour)
	if removed != 1 {
		t.Fatalf("expected one stale client to be removed, got %d", removed)
	}
	if got := registry.Count(); got != 0 {
		t.Fatalf("expected stale client registry to be empty after cleanup, got %d", got)
	}
}

func TestRequireAuthAcceptsHQSignedTokens(t *testing.T) {
	withIsolatedSystemDB(t)

	h := &DaemonAuthHandler{sessionStore: sopbackend.NewSessionStore(time.Minute), hqSigningSecret: "hq-secret"}
	token, err := h.signHQAccessToken("hq-user", "daemon-user", []string{"ask", "memory"}, time.Now().Add(5*time.Minute), "hq-token-1")
	if err != nil {
		t.Fatalf("sign hq token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	var gotAuth sop.AuthContext
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = sop.GetAuthFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	h.RequireAuth(next)(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNoContent, rr.Code, rr.Body.String())
	}
	if gotAuth.UserID != "hq-user" {
		t.Fatalf("expected user id hq-user, got %q", gotAuth.UserID)
	}
	if len(gotAuth.Roles) != 1 || gotAuth.Roles[0] != "daemon-user" {
		t.Fatalf("expected role daemon-user, got %#v", gotAuth.Roles)
	}
}

func TestHandleHQAuthIssuesDaemonTokensForSignedHQToken(t *testing.T) {
	withIsolatedSystemDB(t)

	h := &DaemonAuthHandler{sessionStore: sopbackend.NewSessionStore(time.Minute), hqSigningSecret: "hq-secret"}
	token, err := h.signHQAccessToken("hq-user", "daemon-user", []string{"ask", "memory"}, time.Now().Add(5*time.Minute), "hq-token-2")
	if err != nil {
		t.Fatalf("sign hq token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-auth", strings.NewReader(fmt.Sprintf(`{"access_token":"%s"}`, token)))
	rr := httptest.NewRecorder()

	h.HandleHQAuth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["access_token"] == "" {
		t.Fatalf("expected daemon access token in response, got %#v", body)
	}
}

func TestSyncWithHQReturnsNilForValidHandler(t *testing.T) {
	withIsolatedSystemDB(t)

	h := &DaemonAuthHandler{sessionStore: sopbackend.NewSessionStore(time.Minute)}
	if err := h.syncWithHQ(context.Background()); err != nil {
		t.Fatalf("syncWithHQ() error = %v", err)
	}
}
