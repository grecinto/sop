package codingworkflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSearchAdapterFindsMatchingFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	adapter := &FileSearchAdapter{}
	matches, err := adapter.Search(Task{WorkingDir: tmpDir}, RunState{}, "Hello")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "sample.go") {
		t.Fatalf("expected sample.go match, got %v", matches)
	}
}

func TestFileSearchAdapterFindsHiddenTypeScriptFile(t *testing.T) {
	tmpDir := t.TempDir()
	hiddenDir := filepath.Join(tmpDir, ".continue-repo", "core", "util")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatalf("mkdir hidden dir: %v", err)
	}
	filePath := filepath.Join(hiddenDir, "ideUtils.ts")
	if err := os.WriteFile(filePath, []byte("export const ideUtils = true\n"), 0o644); err != nil {
		t.Fatalf("write hidden sample file: %v", err)
	}

	adapter := &FileSearchAdapter{}
	matches, err := adapter.Search(Task{WorkingDir: tmpDir}, RunState{}, "ideUtils")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected hidden ideUtils.ts match, got none")
	}
	if !strings.Contains(matches[0], "ideUtils.ts") {
		t.Fatalf("expected match output to mention ideUtils.ts, got %v", matches)
	}
}
