package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunAdminCLI_WrapperDelegatesToBackend(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"token"}`))
	})
	mux.HandleFunc("/api/admin/users", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"username":"alice","role":"user","status":"active"}]`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	var stdout, stderr bytes.Buffer
	if err := runAdminCLI([]string{"--server", server.URL, "--username", "alice", "--password", "pw123", "list-users"}, &stdout, &stderr); err != nil {
		t.Fatalf("runAdminCLI() error = %v", err)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	if got := stdout.String(); got == "" {
		t.Fatalf("expected stdout to contain user output")
	}
}
