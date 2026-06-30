package main

import (
	"path/filepath"
	"testing"
)

func TestBuildSiteArgs(t *testing.T) {
	got := buildSiteArgs(nil)
	want := []string{"run", "./src/backend/site"}
	if len(got) != len(want) {
		t.Fatalf("buildSiteArgs() length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buildSiteArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRepoRootFindsWorkspaceRoot(t *testing.T) {
	got := repoRoot()
	if got == "" {
		t.Fatal("repoRoot() returned empty path")
	}
	if _, err := filepath.Abs(got); err != nil {
		t.Fatalf("repoRoot() returned invalid path %q: %v", got, err)
	}
}
