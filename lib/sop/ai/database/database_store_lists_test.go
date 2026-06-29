package database

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sharedcode/sop"
)

func TestDatabaseStoreListsReadCurrentFolderAndRepoLists(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "storelist.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write current store list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "folderstorelist.txt"), []byte("folder-one\n"), 0o644); err != nil {
		t.Fatalf("write folder store list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "repostorelist.txt"), []byte("repo-one\nrepo-two\n"), 0o644); err != nil {
		t.Fatalf("write repo store list: %v", err)
	}

	db := NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{tempDir}})

	current, err := db.GetStores(context.Background())
	if err != nil {
		t.Fatalf("GetStores returned error: %v", err)
	}
	folder, err := db.GetFolderStores(context.Background())
	if err != nil {
		t.Fatalf("GetFolderStores returned error: %v", err)
	}
	repo, err := db.GetRepoStores(context.Background())
	if err != nil {
		t.Fatalf("GetRepoStores returned error: %v", err)
	}

	if !reflect.DeepEqual(current, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected current stores: %#v", current)
	}
	if !reflect.DeepEqual(folder, []string{"folder-one"}) {
		t.Fatalf("unexpected folder stores: %#v", folder)
	}
	if !reflect.DeepEqual(repo, []string{"repo-one", "repo-two"}) {
		t.Fatalf("unexpected repo stores: %#v", repo)
	}
}

func TestDatabaseSourceStoresCreateAndPersistManifestData(t *testing.T) {
	tempDir := t.TempDir()
	db := NewDatabase(sop.DatabaseOptions{Type: sop.Standalone, StoresFolders: []string{tempDir}})

	if err := db.AddFolderStore(context.Background(), "folder-source"); err != nil {
		t.Fatalf("AddFolderStore returned error: %v", err)
	}

	manifest := RepoSourceStoreManifest{
		Name:            "repo-source",
		Description:     "Repository intake",
		Repositories:    []RepoSource{{URL: "https://example.com/repo", Branch: "main"}},
		CredentialsRefs: []string{"github-token"},
	}
	if err := db.AddRepoStore(context.Background(), manifest.Name, manifest); err != nil {
		t.Fatalf("AddRepoStore returned error: %v", err)
	}

	folderStores, err := db.GetFolderStores(context.Background())
	if err != nil {
		t.Fatalf("GetFolderStores returned error: %v", err)
	}
	repoStores, err := db.GetRepoStores(context.Background())
	if err != nil {
		t.Fatalf("GetRepoStores returned error: %v", err)
	}

	if !reflect.DeepEqual(folderStores, []string{"folder-source"}) {
		t.Fatalf("unexpected folder stores: %#v", folderStores)
	}
	if !reflect.DeepEqual(repoStores, []string{"repo-source"}) {
		t.Fatalf("unexpected repo stores: %#v", repoStores)
	}

	persisted, err := db.ReadRepoStoreManifest(context.Background(), "repo-source")
	if err != nil {
		t.Fatalf("ReadRepoStoreManifest returned error: %v", err)
	}
	if persisted.Description != manifest.Description {
		t.Fatalf("unexpected description: %q", persisted.Description)
	}
	if len(persisted.Repositories) != 1 || persisted.Repositories[0].URL != manifest.Repositories[0].URL {
		t.Fatalf("unexpected repository manifest: %#v", persisted.Repositories)
	}
	if !reflect.DeepEqual(persisted.CredentialsRefs, manifest.CredentialsRefs) {
		t.Fatalf("unexpected credentials refs: %#v", persisted.CredentialsRefs)
	}
}
