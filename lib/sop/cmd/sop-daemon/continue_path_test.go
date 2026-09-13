package main

import (
	"path/filepath"
	"testing"
)

func TestResolveContinueRepoRootFromNestedWorkspace(t *testing.T) {
	root := resolveContinueRepoRootFrom(filepath.Join("/Volumes/T7/sop-cloud", "lib", "sop"))
	if root != "/Volumes/T7/sop-cloud" {
		t.Fatalf("expected repo root /Volumes/T7/sop-cloud, got %q", root)
	}
}

func TestResolveContinueEditablePathUsesRepoRootForContinueRepoFiles(t *testing.T) {
	got := resolveContinueEditablePath(filepath.Join(".continue-repo", "core", "util", "ideUtils.ts"))
	want := filepath.Clean(filepath.Join("/Volumes/T7/sop-cloud", ".continue-repo", "core", "util", "ideUtils.ts"))
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
