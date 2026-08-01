package sopbackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sharedcode/sop"
)

func TestUIRouterRegistersLoginRoute(t *testing.T) {
	templateFS := fstest.MapFS{
		"login.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body><h1>SOP Login</h1></body></html>")},
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>Index</body></html>")},
	}
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

func TestLandingPageDataExposesModelCatalogForTemplates(t *testing.T) {
	router := NewUIRouter(nil, "*.html")
	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "root"}))

	data := router.landingPageData(req)
	catalog, ok := data["ModelCatalog"].(ModelCatalog)
	if !ok {
		t.Fatalf("expected landing page data to expose ModelCatalog, got %#v", data["ModelCatalog"])
	}

	var found bool
	for _, group := range catalog.Embedder {
		for _, option := range group.Options {
			value := strings.ToLower(option.Value)
			label := strings.ToLower(option.Label)
			if strings.Contains(value, "kelindar") || strings.Contains(value, "local") || strings.Contains(label, "kelindar") || strings.Contains(label, "local") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("expected landing page data to include a local embedder option, got %#v", catalog)
	}
}

func TestUIRouterRedirectsLogoutToLogin(t *testing.T) {
	templateFS := fstest.MapFS{
		"login.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body><h1>SOP Login</h1></body></html>")},
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>Index</body></html>")},
	}
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

func TestUninstallLocalDaemonServiceRunsSystemdCleanup(t *testing.T) {
	oldLookPath := lookPathForDaemonUninstall
	oldRunner := runDaemonUninstallCommand
	oldServiceUnitPaths := daemonUninstallServiceUnitPaths
	oldBinaryPaths := daemonUninstallBinaryPaths
	t.Cleanup(func() {
		lookPathForDaemonUninstall = oldLookPath
		runDaemonUninstallCommand = oldRunner
		daemonUninstallServiceUnitPaths = oldServiceUnitPaths
		daemonUninstallBinaryPaths = oldBinaryPaths
	})

	lookPathForDaemonUninstall = func(file string) (string, error) {
		if file != "systemctl" {
			t.Fatalf("unexpected lookup for %q", file)
		}
		return "/usr/bin/systemctl", nil
	}

	var calls [][]string
	runDaemonUninstallCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return exec.Command("sh", "-c", "exit 0")
	}

	tempDir := t.TempDir()
	daemonUninstallServiceUnitPaths = []string{filepath.Join(tempDir, "sop-daemon.service")}
	daemonUninstallBinaryPaths = []string{filepath.Join(tempDir, "sop-daemon")}
	if err := os.WriteFile(daemonUninstallServiceUnitPaths[0], []byte("unit"), 0o644); err != nil {
		t.Fatalf("failed to create fake service unit: %v", err)
	}
	if err := os.WriteFile(daemonUninstallBinaryPaths[0], []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to create fake daemon binary: %v", err)
	}

	if err := uninstallLocalDaemonService(); err != nil {
		t.Fatalf("uninstallLocalDaemonService() error = %v", err)
	}

	if _, err := os.Stat(daemonUninstallServiceUnitPaths[0]); !os.IsNotExist(err) {
		t.Fatalf("expected service unit to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(daemonUninstallBinaryPaths[0]); !os.IsNotExist(err) {
		t.Fatalf("expected daemon binary to be removed, stat err = %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 systemctl invocations, got %d", len(calls))
	}
	if got := calls[0]; len(got) != 3 || got[0] != "systemctl" || got[1] != "stop" || got[2] != "sop-daemon.service" {
		t.Fatalf("unexpected first command: %v", got)
	}
}

func TestUninstallLocalDaemonServiceIgnoresSystemdFailures(t *testing.T) {
	oldLookPath := lookPathForDaemonUninstall
	oldRunner := runDaemonUninstallCommand
	oldServiceUnitPaths := daemonUninstallServiceUnitPaths
	oldBinaryPaths := daemonUninstallBinaryPaths
	t.Cleanup(func() {
		lookPathForDaemonUninstall = oldLookPath
		runDaemonUninstallCommand = oldRunner
		daemonUninstallServiceUnitPaths = oldServiceUnitPaths
		daemonUninstallBinaryPaths = oldBinaryPaths
	})

	lookPathForDaemonUninstall = func(file string) (string, error) {
		if file != "systemctl" {
			t.Fatalf("unexpected lookup for %q", file)
		}
		return "/usr/bin/systemctl", nil
	}

	runDaemonUninstallCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 1")
	}

	tempDir := t.TempDir()
	daemonUninstallServiceUnitPaths = []string{filepath.Join(tempDir, "sop-daemon.service")}
	daemonUninstallBinaryPaths = []string{filepath.Join(tempDir, "sop-daemon")}
	if err := os.WriteFile(daemonUninstallServiceUnitPaths[0], []byte("unit"), 0o644); err != nil {
		t.Fatalf("failed to create fake service unit: %v", err)
	}
	if err := os.WriteFile(daemonUninstallBinaryPaths[0], []byte("binary"), 0o755); err != nil {
		t.Fatalf("failed to create fake daemon binary: %v", err)
	}

	if err := uninstallLocalDaemonService(); err != nil {
		t.Fatalf("expected systemd failures to be ignored, got %v", err)
	}
	if _, err := os.Stat(daemonUninstallServiceUnitPaths[0]); !os.IsNotExist(err) {
		t.Fatalf("expected service unit to be removed even when systemd fails, stat err = %v", err)
	}
	if _, err := os.Stat(daemonUninstallBinaryPaths[0]); !os.IsNotExist(err) {
		t.Fatalf("expected daemon binary to be removed even when systemd fails, stat err = %v", err)
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
	config = Config{UsersStore: NewUserStore()}
	_ = config.UsersStore.Save(context.Background(), &UserRecord{Username: "alice", Role: sop.RoleSystemAdmin})
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
