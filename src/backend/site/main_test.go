package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestSiteBackendDoesNotRegisterLegacyUIRoutes(t *testing.T) {
	mux := http.NewServeMux()
	registerSiteHandlers(mux, t.TempDir())

	for _, path := range []string{"/login", "/app", "/app/subscribe"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %s to be unhandled, got %d", path, rec.Code)
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
