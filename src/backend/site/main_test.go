package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	siteapi "github.com/grecinto/sop-cloud/src/backend/siteapi"
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

func TestSiteBackendExposesSwaggerEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	sopbackend.RegisterAllHandlers(mux)
	siteapi.RegisterHandlers(mux)

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
