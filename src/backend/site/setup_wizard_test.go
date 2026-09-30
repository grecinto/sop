package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestSetupWizardFirstRunRoutesInFreshProcess(t *testing.T) {
	if !runInFreshSiteTestProcess(t) {
		return
	}
	configureFreshFirstRunState(t)

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("write frontend fixture: %v", err)
	}
	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	for _, route := range []string{"/", "/site"} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route, nil))
		if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/app" {
			t.Errorf("first-run %s response = %d, Location=%q; want redirect to /app", route, recorder.Code, recorder.Header().Get("Location"))
		}
	}

	appRecorder := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(appRecorder, httptest.NewRequest(http.MethodGet, "/app", nil))
	if appRecorder.Code == http.StatusForbidden {
		t.Fatalf("first-run /app must not be forbidden; got %d", appRecorder.Code)
	}
}

func runInFreshSiteTestProcess(t *testing.T) bool {
	t.Helper()
	const childEnv = "SOP_ISOLATED_SITE_TEST_CHILD"
	if os.Getenv(childEnv) == "1" {
		return true
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated test %s failed: %v\n%s", t.Name(), err, output)
	}
	return false
}

func configureFreshFirstRunState(t *testing.T) {
	t.Helper()
	workDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	sopbackend.SetConfig(sopbackend.Config{})
	sopbackend.SetConfigFile(filepath.Join(workDir, "missing-config.json"))
}

func TestSetupWizardDoesNotRedirectAfterSetupComplete(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("write frontend fixture: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	sopbackend.SetConfigFile(configPath)
	if err := sopbackend.LoadConfig(configPath); err != nil {
		t.Fatalf("load config fixture: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("configured root returned %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestSetupWizardConfigDiscoveryDelegation(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	configPath := filepath.Join(workDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"databases":[{"name":"cwd"}]}`), 0o644); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	got := sopbackend.FindExistingConfigFile()
	resolvedGot, gotErr := filepath.EvalSymlinks(got)
	resolvedWant, wantErr := filepath.EvalSymlinks(configPath)
	if gotErr == nil {
		got = resolvedGot
	}
	if wantErr == nil {
		configPath = resolvedWant
	}
	if filepath.Clean(got) != filepath.Clean(configPath) {
		t.Fatalf("config discovery returned %q, want %q", got, configPath)
	}
}
