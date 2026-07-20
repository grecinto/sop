package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

// ============================================================================
// CRITICAL FIRST-RUN SETUP WIZARD TESTS
// ============================================================================
//
// ⚠️  WARNING: DO NOT MODIFY WITHOUT READING src/backend/site/SETUP_WIZARD_CRITICAL.md
//
// These tests protect the first-run user experience. Breaking them means:
// - New installations cannot complete setup
// - Users see errors instead of the setup wizard
// - Data directory paths may be incorrectly configured
//
// RUN THESE TESTS BEFORE ANY PR THAT TOUCHES:
// - Route handlers for /, /site, /app
// - Config loading or discovery logic
// - Auth middleware or session handling
// - Landing page or guest access features
//
// Command: go test ./src/backend/site -run TestSetupWizard -v
// ============================================================================

// TestSetupWizardRedirectsFromRootWhenNoConfig verifies that when no config.json
// exists, requests to / are redirected to /app (the setup wizard).
//
// CRITICAL: This is the first-run experience. Breaking this means new users
// cannot complete initial setup.
func TestSetupWizardRedirectsFromRootWhenNoConfig(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	// Ensure no config is loaded
	sopbackend.SetConfig(sopbackend.Config{})

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("CRITICAL: Expected redirect (302) to setup wizard, got %d. First-run setup is BROKEN.", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/app" {
		t.Fatalf("CRITICAL: Expected redirect to /app, got %q. First-run setup is BROKEN.", location)
	}
}

// TestSetupWizardRedirectsFromSiteWhenNoConfig verifies that when no config.json
// exists, requests to /site are redirected to /app (the setup wizard).
func TestSetupWizardRedirectsFromSiteWhenNoConfig(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	// Ensure no config is loaded
	sopbackend.SetConfig(sopbackend.Config{})

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/site", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("CRITICAL: Expected redirect (302) to setup wizard, got %d. First-run setup is BROKEN.", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/app" {
		t.Fatalf("CRITICAL: Expected redirect to /app, got %q. First-run setup is BROKEN.", location)
	}
}

// TestSetupWizardAllowsAccessToAppPathWhenNoConfig verifies that when no
// config.json exists, requests to /app are allowed (not redirected).
//
// CRITICAL: This is where the setup wizard UI is served. Breaking this means
// the setup wizard is inaccessible and new installations cannot be configured.
func TestSetupWizardAllowsAccessToAppPathWhenNoConfig(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	// Ensure no config is loaded
	sopbackend.SetConfig(sopbackend.Config{})

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	mux := http.NewServeMux()
	registerSiteHandlers(mux, frontendDir, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	rec := httptest.NewRecorder()
	newSiteAppAccessHandler(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CRITICAL: Expected /app to be accessible (200), got %d. Setup wizard is BROKEN.", rec.Code)
	}
}

// TestSetupWizardDoesNotRedirectAfterSetupComplete verifies that after a
// config.json exists, requests to / serve the landing page instead of redirecting.
//
// This ensures the normal landing page works after initial setup is complete.
func TestSetupWizardDoesNotRedirectAfterSetupComplete(t *testing.T) {
	oldConfig := sopbackend.GetConfig()
	defer sopbackend.SetConfig(oldConfig)

	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatalf("failed to write test frontend asset: %v", err)
	}

	// Create and load a config so HasConfigFileOnDisk() returns true
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected landing page (200) after setup complete, got %d", rec.Code)
	}
}

// TestSetupWizardConfigDiscoveryDelegation verifies that the site backend
// delegates config discovery to sopbackend instead of reimplementing it.
//
// This prevents path duplication bugs like ".sop-data/.sop-data".
func TestSetupWizardConfigDiscoveryDelegation(t *testing.T) {
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
		t.Fatalf("CRITICAL: Config discovery not delegated properly. Expected %q, got %q. Path duplication bug may occur.", want, got)
	}
}
