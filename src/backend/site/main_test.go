package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestResolveFrontendAssetPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantPath string
		wantSPA  bool
	}{
		{name: "root", path: "/", wantPath: "index.html", wantSPA: true},
		{name: "site root", path: "/site", wantPath: "index.html", wantSPA: true},
		{name: "subroute", path: "/site/subscribe", wantPath: "index.html", wantSPA: true},
		{name: "asset under site prefix", path: "/site/assets/app.js", wantPath: "assets/app.js", wantSPA: false},
		{name: "plain asset", path: "/assets/app.js", wantPath: "assets/app.js", wantSPA: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotSPA := resolveFrontendAssetPath(tt.path)
			if gotPath != tt.wantPath || gotSPA != tt.wantSPA {
				t.Fatalf("resolveFrontendAssetPath(%q) = (%q, %t), want (%q, %t)", tt.path, gotPath, gotSPA, tt.wantPath, tt.wantSPA)
			}
		})
	}
}

func TestDefaultFrontendDirUsesSourceTreePath(t *testing.T) {
	t.Setenv("FRONTEND_DIR", "")

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	want := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "frontend", "dist"))
	if got := defaultFrontendDir(); got != want {
		t.Fatalf("defaultFrontendDir() = %q, want %q", got, want)
	}
}

func TestDefaultDataDirUsesContainerOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "site-data")
	t.Setenv("SOP_CLOUD_DATA_DIR", want)
	if got := defaultDataDir(); got != want {
		t.Fatalf("defaultDataDir() = %q, want %q", got, want)
	}
}

func TestConfigureRuntimeEnvironmentSkipsSeedingConfigOnFirstRun(t *testing.T) {
	dataDir := t.TempDir()
	configureRuntimeEnvironment(dataDir)

	configPath := filepath.Join(dataDir, "config.json")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected no config.json to be created on first run, stat returned err=%v", err)
	}
}

func TestConfigureRuntimeEnvironmentUsesExistingDataDirConfigWhenPresent(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer os.Chdir(oldWd)

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	t.Setenv("SOP_CONFIG_FILE", "")

	configDir := filepath.Join(workDir, ".sop-data")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	configureRuntimeEnvironment(".sop-data")

	cfg := sopbackend.GetConfig()
	wantPath := configPath
	if resolved, err := filepath.EvalSymlinks(configPath); err == nil {
		wantPath = resolved
	}
	gotPath := cfg.ConfigFile
	if resolved, err := filepath.EvalSymlinks(cfg.ConfigFile); err == nil {
		gotPath = resolved
	}
	if filepath.Clean(gotPath) != filepath.Clean(wantPath) {
		t.Fatalf("expected runtime to use existing config at %q, got %q", filepath.Clean(wantPath), filepath.Clean(gotPath))
	}
}

func TestSiteBackendDaemonBootstrapEndpointReturnsJsonErrorWhenBootstrapFileIsMissing(t *testing.T) {
	frontendDir := t.TempDir()
	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir)

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/bootstrap-secret", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected bootstrap endpoint to return 404 when bootstrap file is missing, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Bootstrap file not found") {
		t.Fatalf("expected bootstrap error body to mention missing bootstrap file, got %q", body)
	}
}

func TestSiteBackendDaemonBootstrapEndpointReturnsBootstrapSecret(t *testing.T) {
	frontendDir := t.TempDir()
	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir)

	configRoot := filepath.Join(t.TempDir(), "config", "sop")
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "daemon.bootstrap"), []byte("secret-value"), 0o600); err != nil {
		t.Fatalf("failed to write bootstrap secret: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(configRoot))

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/bootstrap-secret", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected bootstrap endpoint to return 200, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, "secret-value") {
		t.Fatalf("expected bootstrap secret response body to contain secret value, got %q", body)
	}
}

func TestSiteBackendRedirectsRootToSharedUIWhenNoConfigExists(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected root request to redirect to shared UI, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/app" {
		t.Fatalf("expected redirect to /app, got %q", got)
	}
}

func TestSiteBackendRedirectsRootToSharedUIEvenWhenConfigExists(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	if err := os.Setenv("SOP_CLOUD_DATA_DIR", dataDir); err != nil {
		t.Fatalf("failed to set SOP_CLOUD_DATA_DIR: %v", err)
	}
	defer os.Unsetenv("SOP_CLOUD_DATA_DIR")

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected root request to redirect to shared UI even when config exists, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/app" {
		t.Fatalf("expected redirect to /app, got %q", got)
	}
}

func TestSiteBackendServesSiteFrontendRoutes(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir)

	for _, path := range []string{"/site/", "/site/subscribe"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %s to serve the site frontend, got %d", path, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, "site") {
			t.Fatalf("expected %s response body to contain the site markup, got %q", path, body)
		}
	}
}

func TestSiteBackendRegistersSharedUIRoutes(t *testing.T) {
	mux := http.NewServeMux()
	registerSiteHandlers(mux, t.TempDir())

	for _, path := range []string{"/login", "/app", "/app/subscribe"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound {
			t.Fatalf("expected %s to be handled by the shared UI routes, got 404", path)
		}
	}
}

func TestSiteBackendExposesSwaggerEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	registerSiteHandlers(mux, t.TempDir())

	t.Run("swagger json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected /swagger.json to return %d, got %d", http.StatusOK, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, "\"openapi\"") {
			t.Fatalf("expected /swagger.json payload to contain openapi spec, got %q", body)
		}
	})

	t.Run("swagger ui", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/swagger/", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected /swagger/ to return %d, got %d", http.StatusOK, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, "Swagger UI") {
			t.Fatalf("expected /swagger/ page to contain Swagger UI markup, got %q", body)
		}
	})
}
