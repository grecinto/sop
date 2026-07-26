package sopbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
)

func TestRunAdminCLI_ListUsers(t *testing.T) {
	config = Config{UsersStore: NewUserStore()}
	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleAdmin); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/admin/users", WithAuth(handleAdminUsersList))
	server := httptest.NewServer(mux)
	defer server.Close()

	var stdout, stderr bytes.Buffer
	if err := runAdminCLI([]string{"--server", server.URL, "--username", "alice", "--password", "pw123", "list-users"}, &stdout, &stderr); err != nil {
		t.Fatalf("runAdminCLI() error = %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "alice") {
		t.Fatalf("expected output to contain alice, got %q", stdout.String())
	}
}

func TestRunAdminCLI_UsesHTTPServerEndpoints(t *testing.T) {
	config = Config{UsersStore: NewUserStore()}
	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleAdmin); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/admin/users", WithAuth(handleAdminUsersList))
	mux.HandleFunc("/api/auth/users/role", WithAuth(handleUpdateUserRole))
	mux.HandleFunc("/api/auth/unregister", WithAuth(handleUnregister))
	server := httptest.NewServer(mux)
	defer server.Close()

	var stdout, stderr bytes.Buffer
	if err := runAdminCLI([]string{"--server", server.URL, "--username", "alice", "--password", "pw123", "list-users"}, &stdout, &stderr); err != nil {
		t.Fatalf("runAdminCLI() error = %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "alice") {
		t.Fatalf("expected output to contain alice, got %q", stdout.String())
	}
}

func TestHandleAdminUsersPage_RendersAdminUI(t *testing.T) {
	config = Config{UsersStore: NewUserStore()}
	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleAdmin}}))
	w := httptest.NewRecorder()

	handleAdminUsersPage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleAdminUsersPage() status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Admin Users") {
		t.Fatalf("expected admin UI markup, got %s", w.Body.String())
	}
}

func TestHandleAdminUsersList_ReturnsUsersForAdmin(t *testing.T) {
	config = Config{UsersStore: NewUserStore()}
	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleAdmin}}))
	w := httptest.NewRecorder()

	handleAdminUsersList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleAdminUsersList() status = %d, body=%s", w.Code, w.Body.String())
	}

	var payload []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload) == 0 {
		t.Fatalf("expected at least one user, got %s", w.Body.String())
	}
}
