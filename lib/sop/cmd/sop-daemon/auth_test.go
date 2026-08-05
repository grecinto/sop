package main

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

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

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

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(`{"bootstrap_secret":"shared-secret"}`))
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
		bootstrapPath:   filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sop", "daemon.bootstrap"),
		sessionStore:    sopbackend.NewSessionStore(time.Minute),
	}

	if err := os.MkdirAll(filepath.Dir(h.bootstrapPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(h.bootstrapPath, []byte("fresh-secret"), 0o600); err != nil {
		t.Fatalf("write bootstrap file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(`{"bootstrap_secret":"fresh-secret"}`))
	rr := httptest.NewRecorder()

	h.HandleBootstrap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestHandleBootstrapAcceptsSignedBootstrapToken(t *testing.T) {
	withIsolatedSystemDB(t)

	h := &DaemonAuthHandler{sessionStore: sopbackend.NewSessionStore(time.Minute)}
	accessToken, _, err := h.sessionStore.CreateSession(context.Background(), "alice", "daemon-user")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(fmt.Sprintf(`{"bootstrap_token":"%s"}`, accessToken)))
	rr := httptest.NewRecorder()

	h.HandleBootstrap(rr, req)

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

func TestHandleBootstrapSupportsMultipleAuthenticatedClients(t *testing.T) {
	withIsolatedSystemDB(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	h := &DaemonAuthHandler{
		bootstrapSecret: "shared-secret",
		bootstrapPath:   filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sop", "daemon.bootstrap"),
		sessionStore:    sopbackend.NewSessionStore(time.Minute),
	}

	if err := os.MkdirAll(filepath.Dir(h.bootstrapPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(h.bootstrapPath, []byte(h.bootstrapSecret), 0o600); err != nil {
		t.Fatalf("write bootstrap file: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(`{"bootstrap_secret":"shared-secret","client_id":"ai-studio-ui"}`))
	firstRR := httptest.NewRecorder()
	h.HandleBootstrap(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("expected first bootstrap to succeed, got %d with body %s", firstRR.Code, firstRR.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(`{"bootstrap_secret":"shared-secret","client_id":"continue-vscode"}`))
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
