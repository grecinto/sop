package codingworkflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSharedInfrastructureInitializesPatchEngine(t *testing.T) {
	infra := NewDefaultSharedInfrastructure()
	if infra.PatchEngine == nil {
		t.Fatal("expected default shared infrastructure to initialize PatchEngine")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.go")
	original := "package main\n\nfunc Hello() {}\n"
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	patchOps := infra.NewPatchOperations()
	patch := &Patch{
		FilePath:    "sample.go",
		OldContent:  "func Hello() {}",
		NewContent:  "func Hello() { println(\"hi\") }",
		Description: "update hello function",
	}

	if err := patchOps.ApplyPatch(Task{WorkingDir: tmpDir}, patch, RunState{}); err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	updated, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}
	if !strings.Contains(string(updated), "println(\"hi\")") {
		t.Fatalf("expected file to be patched, got %q", string(updated))
	}
}
