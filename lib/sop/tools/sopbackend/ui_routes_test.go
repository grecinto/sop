package sopbackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
)

func TestUIRouterRegistersLoginRoute(t *testing.T) {
	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")

	mux := http.NewServeMux()
	router.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected login page to render with status 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "SOP Login") {
		t.Fatalf("expected login page body to contain SOP Login, got %q", got)
	}
}

func TestUIRouterFindsTemplatesWhenWorkingDirectoryChanges(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	router := NewUIRouter(nil, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected login page to render with status 200 when cwd changes, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "SOP Login") {
		t.Fatalf("expected login page body to contain SOP Login, got %q", got)
	}
}

func TestUIRouterRedirectsLogoutToLogin(t *testing.T) {
	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")

	mux := http.NewServeMux()
	router.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected logout to redirect with status 302, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Fatalf("expected logout redirect target to be /login, got %q", got)
	}
}

func TestRuntimeSwitchEnabledForContextAllowsSystemAdmin(t *testing.T) {
	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{sop.RoleSystemAdmin}})
	if !runtimeSwitchEnabledForContext(ctx) {
		t.Fatal("expected system admin to enable runtime switching")
	}
}

func TestRuntimeSwitchEnabledForContextUsesStoredAdminRole(t *testing.T) {
	oldConfig := config
	config = Config{Users: []UserRecord{{Username: "alice", Role: sop.RoleSystemAdmin}}}
	t.Cleanup(func() { config = oldConfig })

	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice"})
	if !runtimeSwitchEnabledForContext(ctx) {
		t.Fatal("expected stored system admin role to enable runtime switching")
	}
}

func TestRuntimeSwitchEnabledForContextRejectsRegularUser(t *testing.T) {
	ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{"User"}})
	if runtimeSwitchEnabledForContext(ctx) {
		t.Fatal("expected regular user to keep runtime switching disabled")
	}
}
