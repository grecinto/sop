package sopbackend

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if got := rec.Body.String(); got == "" {
		t.Fatalf("expected login page body to be rendered")
	}
}

func TestUIRouterRegistersHQLoginRoute(t *testing.T) {
	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")

	mux := http.NewServeMux()
	router.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/hq-login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HQ login page to render with status 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "HQ") {
		t.Fatalf("expected HQ login page body to mention HQ, got %q", got)
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
