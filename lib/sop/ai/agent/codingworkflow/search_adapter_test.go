package codingworkflow

import (
	"os"
	"path/filepath"
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
	if len(matches) != 1 || matches[0] != "sample.go" {
		t.Fatalf("expected sample.go match, got %v", matches)
	}
}
