package main

import (
	"bytes"
	"net"
	"net/http"
	"testing"
)

type testHTTPServer struct {
	URL      string
	listener net.Listener
	server   *http.Server
}

func newTestServer(t *testing.T, handler http.Handler) *testHTTPServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}

	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	return &testHTTPServer{URL: "http://" + listener.Addr().String(), listener: listener, server: server}
}

func (s *testHTTPServer) Close() error {
	if s == nil {
		return nil
	}
	_ = s.server.Close()
	return s.listener.Close()
}

func TestRunAdminCLI_WrapperDelegatesToBackend(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"token"}`))
	})
	mux.HandleFunc("/api/admin/users", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"username":"alice","role":"user","status":"active"}]`))
	})
	server := newTestServer(t, mux)
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
