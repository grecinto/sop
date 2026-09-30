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

func TestApplySopbackendPreloadPolicyPreservesDefaultPreloadSetting(t *testing.T) {
	sopbackend.SetConfig(sopbackend.Config{})
	defer sopbackend.SetConfig(sopbackend.Config{})

	applySopbackendPreloadPolicy()

	if sopbackend.GetConfig().DisableSOPKBPreload {
		t.Fatalf("expected DisableSOPKBPreload to remain disabled by default")
	}
}

func TestShouldPrintHelpRecognizesHelpFlags(t *testing.T) {
	if !shouldPrintHelp([]string{"--h"}) {
		t.Fatal("expected --h to trigger help output")
	}
	if !shouldPrintHelp([]string{"--help"}) {
		t.Fatal("expected --help to trigger help output")
	}
	if shouldPrintHelp([]string{"--port", "8081"}) {
		t.Fatal("expected non-help args to not trigger help output")
	}
}

func TestPrintSiteHelpIncludesResolvedConfigPath(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Set the config file path so sopbackend.FindExistingConfigFile can locate it
	sopbackend.SetConfigFile(configPath)

	var stdout strings.Builder
	printSiteHelpTo(&stdout)

	got := stdout.String()
	if !strings.Contains(got, "Config file:") {
		t.Fatalf("expected help output to include config file section, got %q", got)
	}
	if !strings.Contains(got, configPath) {
		t.Fatalf("expected help output to include resolved config path, got %q", got)
	}
}

func TestPrintSiteHelpOmitsConfigPathWhenConfigFileMissing(t *testing.T) {
	t.Setenv("SOP_CLOUD_DATA_DIR", t.TempDir())
	t.Setenv("SOP_DATA_DIR", t.TempDir())
	t.Setenv("SOP_CONFIG_FILE", "")

	var stdout strings.Builder
	printSiteHelpTo(&stdout)

	if strings.Contains(stdout.String(), "config.json") {
		t.Fatalf("expected help output to omit config path when config file is missing, got %q", stdout.String())
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

func TestSopbackendConfigDiscoveryPrefersCwdConfig(t *testing.T) {
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

	// sopbackend.FindExistingConfigFile should find the cwd config.json
	got := sopbackend.FindExistingConfigFile()
	want := cwdConfigPath
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("sopbackend.FindExistingConfigFile() = %q, want %q", got, want)
	}
}

func TestSopbackendConfigDiscoveryReturnsEmptyWhenNoConfigExists(t *testing.T) {
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

	// sopbackend.FindExistingConfigFile should return empty when no config.json is in cwd
	got := sopbackend.FindExistingConfigFile()
	if got != "" {
		t.Fatalf("sopbackend.FindExistingConfigFile() = %q, want empty string when no config in cwd", got)
	}
}

func TestConfigureRuntimeEnvironmentSetsDataDirEnvVars(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer os.Chdir(oldWd)

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	dataDir := filepath.Join(workDir, ".sop-data")
	configureRuntimeEnvironment(dataDir)

	// Verify environment variables are set
	if got := os.Getenv("SOP_DATA_DIR"); !strings.Contains(got, ".sop-data") {
		t.Fatalf("expected SOP_DATA_DIR to contain .sop-data, got %q", got)
	}
	if got := os.Getenv("SOP_CLOUD_DATA_DIR"); !strings.Contains(got, ".sop-data") {
		t.Fatalf("expected SOP_CLOUD_DATA_DIR to contain .sop-data, got %q", got)
	}
}

func TestStartSiteBackgroundServicesStartsCleanupLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startSiteBackgroundServices(ctx)
}

func TestSiteBackendDoesNotExposeDaemonBootstrapSecret(t *testing.T) {
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

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/daemon/bootstrap-secret", nil))
	if strings.Contains(recorder.Body.String(), "secret-value") {
		t.Fatal("site backend exposed the daemon bootstrap secret")
	}
	if recorder.Code == http.StatusOK {
		t.Fatalf("site backend must not serve the daemon bootstrap endpoint, got %d", recorder.Code)
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

func TestSiteBackendAcceptsSharedAuthSessionForAppRoute(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	systemDBPath := filepath.Join(t.TempDir(), "system")
	cfg := sopbackend.Config{
		UsersStore:    sopbackend.NewUserStore(),
		SystemDB:      &sopbackend.DatabaseConfig{Name: sopbackend.SystemDBName, Path: systemDBPath, Mode: "standalone"},
		SessionSecret: sopbackend.SecuredString("shared-auth-test-secret"),
	}
	sopbackend.SetConfig(cfg)
	if _, err := (&cfg).RegisterUser(context.Background(), "shared-auth-admin", "secret", "admin@example.com", sop.RoleAdmin); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}
	sopbackend.SetConfig(cfg)

	oldFacade := sopbackend.GetConfig().UsersStore
	_ = oldFacade
	if tokenFacade := sopbackend.GetConfig().UsersStore; tokenFacade == nil {
		// no-op
	}

	accessToken, _, err := sopbackend.CreateSession(context.Background(), "shared-auth-admin", sop.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.AddCookie(&http.Cookie{Name: "sop_access_token", Value: accessToken})
	rec := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin with shared auth session to access Data Manager, got %d", rec.Code)
	}
}

func TestSiteBackendServesFrontendAtRootEvenWhenConfigExists(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	// Load the config so sopbackend.HasConfigFileOnDisk() returns true
	sopbackend.SetConfigFile(configPath)
	if err := sopbackend.LoadConfig(configPath); err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

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

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status for subscribed non-admin user, got %d", rec.Code)
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

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected RBAC-authorized non-admin to be denied Data Manager access, got %d", rec.Code)
	}
}

func TestSiteBackendServesSiteFrontendRoutes(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	// Create a config so the frontend is served instead of redirecting to setup wizard
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"databases":[{"name":"default","path":"/tmp/default"}]}`), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	sopbackend.SetConfigFile(configPath)
	if err := sopbackend.LoadConfig(configPath); err != nil {
		t.Fatalf("failed to load config: %v", err)
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
