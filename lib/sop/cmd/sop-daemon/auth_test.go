package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestHandleBootstrapUsesCurrentBootstrapSecretFromDisk(t *testing.T) {
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
