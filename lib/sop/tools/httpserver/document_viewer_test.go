package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleViewer_ExternalDocIDRedirectsDirectly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/viewer?docID=https://github.com/SharedCode/sop/README.md&text=hello", nil)
	rec := httptest.NewRecorder()

	handleViewer(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect status 302, got %d", rec.Code)
	}

	got := rec.Header().Get("Location")
	if got != "https://github.com/SharedCode/sop/README.md" {
		t.Fatalf("expected direct external redirect without highlight text suffix, got %q", got)
	}
}

func TestHandleFilePreviewResolvesHomeDirectoryPaths(t *testing.T) {
	tempDir := t.TempDir()
	imagePath := filepath.Join(tempDir, "preview.png")
	if err := os.WriteFile(imagePath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	defer os.Setenv("HOME", oldHome)

	req := httptest.NewRequest(http.MethodGet, "/file-preview?path=~/preview.png", nil)
	rec := httptest.NewRecorder()

	handleFilePreview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("expected image/png content type, got %q", got)
	}
}
