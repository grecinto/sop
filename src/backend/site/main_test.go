package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestApplySopbackendPreloadPolicyDisablesPreloadByDefault(t *testing.T) {
	sopbackend.SetConfig(sopbackend.Config{})
	defer sopbackend.SetConfig(sopbackend.Config{})

	applySopbackendPreloadPolicy()

	if !sopbackend.GetConfig().DisableSOPKBPreload {
		t.Fatalf("expected DisableSOPKBPreload to be enabled")
	}
}

func TestApplySopbackendPreloadPolicyPreservesExistingFlag(t *testing.T) {
	sopbackend.SetConfig(sopbackend.Config{DisableSOPKBPreload: true})
	defer sopbackend.SetConfig(sopbackend.Config{})

	applySopbackendPreloadPolicy()

	if !sopbackend.GetConfig().DisableSOPKBPreload {
		t.Fatalf("expected existing DisableSOPKBPreload flag to remain enabled")
	}
}

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

func TestDefaultKnowledgeManifestPathUsesSourceTreePath(t *testing.T) {
	t.Setenv("SOP_KB_CURATION_MANIFEST_PATH", "")

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	want := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "lib", "sop", "ai", "KB_CURATION_MANIFEST.md"))
	if got := defaultKnowledgeManifestPath(); got != want {
		t.Fatalf("defaultKnowledgeManifestPath() = %q, want %q", got, want)
	}
}

func TestServeKnowledgeAssetServesMarkdownFromKnowledgeBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	manifestPath := filepath.Join(baseDir, "KB_CURATION_MANIFEST.md")
	if err := os.WriteFile(manifestPath, []byte("# Manifest\n\nThis is a test manifest."), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/kb/KB_CURATION_MANIFEST.md", nil)
	rec := httptest.NewRecorder()
	serveKnowledgeAsset(rec, req, baseDir)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "text/markdown; charset=utf-8" {
		t.Fatalf("expected markdown content type, got %q", contentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, "This is a test manifest") {
		t.Fatalf("expected manifest content in response body, got %q", body)
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

func TestResolveRuntimeConfigPathPrefersCwdConfigOverEnvOverride(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer os.Chdir(oldWd)

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	cwdConfigPath := filepath.Join(workDir, "config.json")
	if err := os.WriteFile(cwdConfigPath, []byte(`{"databases":[{"name":"cwd"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	envConfigPath := filepath.Join(t.TempDir(), "env-config.json")
	if err := os.WriteFile(envConfigPath, []byte(`{"databases":[{"name":"env"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("SOP_CONFIG_FILE", envConfigPath)

	got := resolveRuntimeConfigPath(filepath.Join(workDir, ".sop-data"))
	want := cwdConfigPath
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("resolveRuntimeConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveRuntimeConfigPathIgnoresRepoSharedConfigWhenNoRuntimeConfigExists(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer os.Chdir(oldWd)

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	workspaceConfigPath := filepath.Join(workDir, "lib", "sop", "tools", "httpserver", "config.json")
	if err := os.MkdirAll(filepath.Dir(workspaceConfigPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(workspaceConfigPath, []byte(`{"users":[{"username":"root"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := resolveRuntimeConfigPath(filepath.Join(workDir, ".sop-data"))
	want := filepath.Join(workDir, ".sop-data", "config.json")
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("resolveRuntimeConfigPath() = %q, want %q", got, want)
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

func TestStartSiteBackgroundServicesStartsCleanupLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startSiteBackgroundServices(ctx)
}

func TestSiteBackendDaemonBootstrapEndpointReturnsJsonErrorWhenBootstrapFileIsMissing(t *testing.T) {
	frontendDir := t.TempDir()
	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

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
	registerSiteHandlers(mux, frontendDir, t.TempDir())

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

func TestSiteBackendRedirectsRootToSetupWizardWhenNoConfigExists(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected root request to redirect to setup wizard, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/app" {
		t.Fatalf("expected root request to redirect to /app, got %q", got)
	}
}

func TestSiteBackendAllowsSetupWizardAtAppPathWhenNoConfigExists(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	rec := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("expected /app to be allowed during first-run setup, got forbidden")
	}
}

func TestSiteBackendServesFrontendAtRootEvenWhenConfigExists(t *testing.T) {
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
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected root request to serve the landing page even when config exists, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "site") {
		t.Fatalf("expected root response body to contain the site markup, got %q", body)
	}
}

func TestSiteBackendBlocksAppAccessForUnsubscribedUsers(t *testing.T) {
	frontendDir := t.TempDir()
	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "bob", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status for unsubscribed user, got %d", rec.Code)
	}
}

func TestSiteBackendRedirectsSubscribedUsersToLocalDaemon(t *testing.T) {
	frontendDir := t.TempDir()
	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}, SubscriptionPlan: "starter"}))
	rec := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect status for subscribed user, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "http://127.0.0.1:9090" {
		t.Fatalf("expected redirect to local daemon URL, got %q", got)
	}
}

func TestSiteBackendUsesRBACPermissionForAppAccess(t *testing.T) {
	frontendDir := t.TempDir()
	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{
		UserID: "alice",
		Roles:  []string{sop.RoleUser},
		Permissions: map[string]bool{
			siteAppPermissionName: true,
		},
	}))
	rec := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected RBAC-authorized app access to redirect, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "http://127.0.0.1:9090" {
		t.Fatalf("expected RBAC-authorized app access to redirect to local daemon URL, got %q", got)
	}
}

func TestSiteBackendServesSiteFrontendRoutes(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

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
	registerSiteHandlers(mux, t.TempDir(), t.TempDir())

	for _, path := range []string{"/login", "/app", "/app/subscribe"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound {
			t.Fatalf("expected %s to be handled by the shared UI routes, got 404", path)
		}
	}
}

func TestSiteBackendRegistersCheckoutEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	registerSiteHandlers(mux, t.TempDir(), t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/auth/checkout", strings.NewReader(`{"plan":"starter"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected /api/auth/checkout to be registered, got 404")
	}
}

func TestSiteBackendRegistersAccountCreationEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	registerSiteHandlers(mux, t.TempDir(), t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"tester","password":"secret","email":"tester@example.com","role":"User"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected /api/auth/register to be registered, got 404")
	}
}

func TestSiteBackendRejectsCheckoutForAnonymousUsers(t *testing.T) {
	mux := http.NewServeMux()
	registerSiteHandlers(mux, t.TempDir(), t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/auth/checkout", strings.NewReader(`{"plan":"starter"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous checkout to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "authentication required") {
		t.Fatalf("expected checkout response body to mention authentication requirement, got %q", body)
	}
}

func TestSiteBackendExposesSwaggerEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	registerSiteHandlers(mux, t.TempDir(), t.TempDir())

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
