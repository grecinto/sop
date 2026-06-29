package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	aidb "github.com/sharedcode/sop/ai/database"
)

func TestToolListFolderStoreFilesAndReadFolderStoreFile(t *testing.T) {
	tempDir := t.TempDir()
	dbOpts := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{tempDir}}
	if err := os.MkdirAll(filepath.Join(tempDir, "folder-source", "nested"), 0o755); err != nil {
		t.Fatalf("create folder store dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "folder-source", "nested", "notes.txt"), []byte("Distributed systems fundamentals"), 0o644); err != nil {
		t.Fatalf("write folder store file: %v", err)
	}

	db := aidb.NewDatabase(dbOpts)
	if err := db.AddFolderStore(context.Background(), "folder-source"); err != nil {
		t.Fatalf("AddFolderStore returned error: %v", err)
	}

	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{"appdb": dbOpts}, nil)
	files, err := ag.toolListFolderStoreFiles(context.Background(), map[string]any{"database": "appdb", "store": "folder-source"})
	if err != nil {
		t.Fatalf("toolListFolderStoreFiles returned error: %v", err)
	}
	if !strings.Contains(files, "nested/notes.txt") {
		t.Fatalf("expected listed files to include nested/notes.txt, got %q", files)
	}

	content, err := ag.toolReadFolderStoreFile(context.Background(), map[string]any{"database": "appdb", "store": "folder-source", "path": "nested/notes.txt"})
	if err != nil {
		t.Fatalf("toolReadFolderStoreFile returned error: %v", err)
	}
	if !strings.Contains(content, "Distributed systems") {
		t.Fatalf("expected file content to be returned, got %q", content)
	}
}

func TestToolListRepoStoreFilesAndReadRepoStoreFile(t *testing.T) {
	tempDir := t.TempDir()
	dbOpts := sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{tempDir}}
	repoDir := filepath.Join(tempDir, "repo-source")
	if err := os.MkdirAll(filepath.Join(repoDir, "docs"), 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "docs", "readme.md"), []byte("Repository guidance"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}

	db := aidb.NewDatabase(dbOpts)
	manifest := aidb.RepoSourceStoreManifest{
		Name: "repo-source",
		Repositories: []aidb.RepoSource{{
			URL: repoDir,
		}},
	}
	if err := db.AddRepoStore(context.Background(), manifest.Name, manifest); err != nil {
		t.Fatalf("AddRepoStore returned error: %v", err)
	}

	ag := NewCopilotAgent(Config{}, map[string]sop.DatabaseOptions{"appdb": dbOpts}, nil)
	files, err := ag.toolListRepoStoreFiles(context.Background(), map[string]any{"database": "appdb", "store": "repo-source"})
	if err != nil {
		t.Fatalf("toolListRepoStoreFiles returned error: %v", err)
	}
	if !strings.Contains(files, "docs/readme.md") {
		t.Fatalf("expected listed repo files to include docs/readme.md, got %q", files)
	}

	content, err := ag.toolReadRepoStoreFile(context.Background(), map[string]any{"database": "appdb", "store": "repo-source", "path": "docs/readme.md"})
	if err != nil {
		t.Fatalf("toolReadRepoStoreFile returned error: %v", err)
	}
	if !strings.Contains(content, "Repository guidance") {
		t.Fatalf("expected repo file content to be returned, got %q", content)
	}
}
