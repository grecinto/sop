package sopbackend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sharedcode/sop"
)

// TestValidatePathConflicts_AdvancedModeWithSubfolders verifies that when using
// Advanced Mode (StoresFolders specified), subdirectories under a common parent
// do NOT trigger false path conflicts.
//
// Regression test for: Setup wizard incorrectly detecting ".sop-data/system" as
// conflicting with workspace root when RegistryPath was set.
func TestValidatePathConflicts_AdvancedModeWithSubfolders(t *testing.T) {
	// Simulate user's setup wizard configuration:
	// - System DB: store folders in ./.sop-data/system
	// - User DB: store folders in ./.sop-data/db
	// Both share a common parent (./.sop-data) but are in separate subdirectories.

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change to work dir: %v", err)
	}

	req := &SaveConfigRequest{
		RegistryPath: "", // Advanced mode: RegistryPath empty or ignored
		RootPassword: "test123",
		SystemOptions: sop.DatabaseOptions{
			StoresFolders: []string{
				"./.sop-data/system", // System DB subfolder
			},
		},
		Databases: []UserDBRequest{
			{
				Name: "users",
				Path: "", // Advanced mode: Path empty or ignored
				DatabaseOptions: sop.DatabaseOptions{
					StoresFolders: []string{
						"./.sop-data/db", // User DB subfolder
					},
				},
			},
		},
	}

	// This should NOT return an error
	err := validatePathConflictsAndPermissions(req)
	if err != nil {
		t.Fatalf("validatePathConflictsAndPermissions() failed with valid subdirectory paths: %v", err)
	}
}

// TestValidatePathConflicts_SimpleModeRegistryPathConflict verifies that when
// using Simple Mode (RegistryPath specified), actual conflicts are still detected.
func TestValidatePathConflicts_SimpleModeRegistryPathConflict(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change to work dir: %v", err)
	}

	samePath := filepath.Join(workDir, ".sop-data")

	req := &SaveConfigRequest{
		RegistryPath: samePath, // Simple mode: RegistryPath specified
		RootPassword: "test123",
		SystemOptions: sop.DatabaseOptions{
			StoresFolders: []string{}, // Empty in simple mode
		},
		Databases: []UserDBRequest{
			{
				Name: "users",
				Path: samePath, // CONFLICT: Same path as system DB
				DatabaseOptions: sop.DatabaseOptions{
					StoresFolders: []string{}, // Empty in simple mode
				},
			},
		},
	}

	// This SHOULD return an error due to identical paths
	err := validatePathConflictsAndPermissions(req)
	if err == nil {
		t.Fatal("validatePathConflictsAndPermissions() should have detected path conflict for identical paths")
	}
}

// TestValidatePathConflicts_NestedPathsDetected verifies that nested paths
// within the same database type are still detected as conflicts.
func TestValidatePathConflicts_NestedPathsDetected(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change to work dir: %v", err)
	}

	req := &SaveConfigRequest{
		RegistryPath: "", // Advanced mode
		RootPassword: "test123",
		SystemOptions: sop.DatabaseOptions{
			StoresFolders: []string{
				"./.sop-data",           // Parent folder
				"./.sop-data/subfolder", // CONFLICT: Nested inside parent
			},
		},
		Databases: []UserDBRequest{},
	}

	// This SHOULD return an error due to nested paths
	err := validatePathConflictsAndPermissions(req)
	if err == nil {
		t.Fatal("validatePathConflictsAndPermissions() should have detected nested path conflict")
	}
}

// TestValidatePathConflicts_SiblingFoldersAllowed verifies that sibling
// directories (same parent, different subdirs) do NOT conflict.
func TestValidatePathConflicts_SiblingFoldersAllowed(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change to work dir: %v", err)
	}

	req := &SaveConfigRequest{
		RegistryPath: "", // Advanced mode
		RootPassword: "test123",
		SystemOptions: sop.DatabaseOptions{
			StoresFolders: []string{
				"./.sop-data/system",
				"./.sop-data/cache",
			},
		},
		Databases: []UserDBRequest{
			{
				Name: "db1",
				Path: "", // Advanced mode
				DatabaseOptions: sop.DatabaseOptions{
					StoresFolders: []string{
						"./.sop-data/db1",
						"./.sop-data/db1-cache",
					},
				},
			},
			{
				Name: "db2",
				Path: "", // Advanced mode
				DatabaseOptions: sop.DatabaseOptions{
					StoresFolders: []string{
						"./.sop-data/db2",
					},
				},
			},
		},
	}

	// This should NOT return an error - all are siblings
	err := validatePathConflictsAndPermissions(req)
	if err != nil {
		t.Fatalf("validatePathConflictsAndPermissions() failed with valid sibling paths: %v", err)
	}
}
