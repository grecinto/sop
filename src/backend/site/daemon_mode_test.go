package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/tools/sopbackend"
)

// TestDaemonModeAccessControl verifies that daemon mode bypasses admin checks.
//
// Scenario: Regular authenticated user (non-admin) tries to access Data Manager.
// - Without ?mode=daemon → 403 Forbidden
// - With ?mode=daemon → 200 OK (allowed)
//
// This is CRITICAL for the "AI Studio" button feature.
// Breaking this will prevent regular users from accessing their local daemon.
func TestDaemonModeAccessControl(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		isAdmin        bool
		expectedStatus int
	}{
		{
			name:           "Admin accesses HQ mode",
			path:           "/app",
			isAdmin:        true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Admin accesses daemon mode",
			path:           "/app?mode=daemon",
			isAdmin:        true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Regular user accesses HQ mode (should be denied)",
			path:           "/app",
			isAdmin:        false,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "Regular user accesses daemon mode (MUST be allowed)",
			path:           "/app?mode=daemon",
			isAdmin:        false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Regular user with custom daemon URL",
			path:           "/app?mode=daemon&daemon_url=http://localhost:9091",
			isAdmin:        false,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock handler that returns 200 if request gets through
			mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Create access control wrapper
			accessHandler := &siteAppAccessHandler{
				next: mockHandler,
			}

			// Create test request
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			// Set up auth context
			roles := []string{"user"}
			if tt.isAdmin {
				roles = []string{sop.RoleSystemAdmin}
			}

			auth := sop.AuthContext{
				UserID:               "testuser",
				Roles:                roles,
				IsSystem:             tt.isAdmin,
				SubscriptionPlan:     "starter",
				SubscriptionFeatures: []string{},
			}

			// Enrich auth with site-specific fields
			auth = enrichSiteAppAuthContext(auth)

			// Add auth to request context
			ctx := sop.ContextWithAuth(req.Context(), auth)
			req = req.WithContext(ctx)

			// Execute request
			rr := httptest.NewRecorder()
			accessHandler.ServeHTTP(rr, req)

			// Verify status code
			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

// TestDaemonModeTemplateData verifies that template data is set correctly for daemon mode.
//
// This test ensures:
// 1. DaemonRuntimeEnabled is set when ?mode=daemon is present
// 2. DefaultRuntimeMode is set to "local" in daemon mode
// 3. Custom daemon URLs are passed through correctly
//
// Breaking this will cause the UI to connect to HQ instead of daemon.
func TestDaemonModeTemplateData(t *testing.T) {
	tests := []struct {
		name                  string
		path                  string
		expectedDaemonEnabled bool
		expectedDefaultMode   string
		expectedDaemonURL     string
	}{
		{
			name:                  "Default HQ mode",
			path:                  "/app",
			expectedDaemonEnabled: false,
			expectedDefaultMode:   "hq",
			expectedDaemonURL:     sopbackend.DefaultDaemonURL,
		},
		{
			name:                  "Daemon mode with default URL",
			path:                  "/app?mode=daemon",
			expectedDaemonEnabled: true,
			expectedDefaultMode:   "local",
			expectedDaemonURL:     sopbackend.DefaultDaemonURL,
		},
		{
			name:                  "Daemon mode with custom URL",
			path:                  "/app?mode=daemon&daemon_url=http://localhost:9091",
			expectedDaemonEnabled: true,
			expectedDefaultMode:   "local",
			expectedDaemonURL:     "http://localhost:9091",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			// Create UIRouter and get template data
			router := &sopbackend.UIRouter{}
			data := router.LandingPageData(req)

			// Verify DaemonRuntimeEnabled
			daemonEnabled, ok := data["DaemonRuntimeEnabled"].(bool)
			if !ok {
				t.Fatal("DaemonRuntimeEnabled not found in template data")
			}
			if daemonEnabled != tt.expectedDaemonEnabled {
				t.Errorf("Expected DaemonRuntimeEnabled=%v, got %v", tt.expectedDaemonEnabled, daemonEnabled)
			}

			// Verify DefaultRuntimeMode
			defaultMode, ok := data["DefaultRuntimeMode"].(string)
			if !ok {
				t.Fatal("DefaultRuntimeMode not found in template data")
			}
			if defaultMode != tt.expectedDefaultMode {
				t.Errorf("Expected DefaultRuntimeMode=%s, got %s", tt.expectedDefaultMode, defaultMode)
			}

			// Verify DaemonURL
			daemonURL, ok := data["DaemonURL"].(string)
			if !ok {
				t.Fatal("DaemonURL not found in template data")
			}
			if daemonURL != tt.expectedDaemonURL {
				t.Errorf("Expected DaemonURL=%s, got %s", tt.expectedDaemonURL, daemonURL)
			}
		})
	}
}

// TestDefaultDaemonURLConstant verifies the default daemon URL constant is correct.
//
// CRITICAL: This constant is referenced in multiple places:
// - Go: lib/sop/tools/sopbackend/ui_routes.go
// - JS: scripts_daemon.html, scripts_part01.html, scripts_part05.html
// - TS: src/frontend/src/App.tsx
//
// Changing this requires updating ALL references.
func TestDefaultDaemonURLConstant(t *testing.T) {
	expected := "http://127.0.0.1:9090"

	if sopbackend.DefaultDaemonURL != expected {
		t.Errorf("DefaultDaemonURL changed! Expected %s, got %s", expected, sopbackend.DefaultDaemonURL)
		t.Error("BREAKING CHANGE: You must update all JavaScript/TypeScript constants:")
		t.Error("  - lib/sop/tools/sopbackend/templates/scripts_daemon.html")
		t.Error("  - lib/sop/tools/sopbackend/templates/scripts_part01.html")
		t.Error("  - lib/sop/tools/sopbackend/templates/scripts_part05.html")
		t.Error("  - src/frontend/src/App.tsx")
	}
}
