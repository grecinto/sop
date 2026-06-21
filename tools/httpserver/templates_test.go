package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaPreviewRendererIsDefinedInTemplate(t *testing.T) {
	path := filepath.Join("templates", "scripts_part01.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "window.getItemMediaPreviewHtml = function") {
		t.Fatalf("expected media preview helper to be defined in %s", path)
	}
	if !strings.Contains(content, "<img") || !strings.Contains(content, "<video") {
		t.Fatalf("expected media preview helper to render image and video markup in %s", path)
	}
}
