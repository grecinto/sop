package sopbackend

import (
	"testing"
)

// TestIsPathConflict_SiblingFolders verifies that folders with a shared parent
// but different leaf names do NOT conflict.
//
// This addresses the concern: "validator is too narrow that being in same parent
// folder is causing perceived collision despite the folders don't collide"
func TestIsPathConflict_SiblingFolders(t *testing.T) {
	tests := []struct {
		name           string
		pathA          string
		pathB          string
		shouldConflict bool
		description    string
	}{
		{
			name:           "Simple siblings",
			pathA:          "/data/system",
			pathB:          "/data/db",
			shouldConflict: false,
			description:    "Same parent /data, different leaf folders",
		},
		{
			name:           "Multiple level siblings",
			pathA:          "/var/lib/sop/system",
			pathB:          "/var/lib/sop/users",
			shouldConflict: false,
			description:    "Same parent /var/lib/sop, different leaf folders",
		},
		{
			name:           "Relative path siblings",
			pathA:          "./.sop-data/system",
			pathB:          "./.sop-data/db",
			shouldConflict: false,
			description:    "User's exact scenario - siblings under .sop-data",
		},
		{
			name:           "Sibling with similar prefix",
			pathA:          "/data/sys",
			pathB:          "/data/system",
			shouldConflict: false,
			description:    "Different folders, one has similar prefix",
		},
		{
			name:           "Parent contains child - should conflict",
			pathA:          "/data/system",
			pathB:          "/data/system/subfolder",
			shouldConflict: true,
			description:    "pathB is inside pathA",
		},
		{
			name:           "Child contains parent - should conflict",
			pathA:          "/data/system/subfolder",
			pathB:          "/data/system",
			shouldConflict: true,
			description:    "pathA is inside pathB",
		},
		{
			name:           "Identical paths - should conflict",
			pathA:          "/data/system",
			pathB:          "/data/system",
			shouldConflict: true,
			description:    "Same path used twice",
		},
		{
			name:           "Deep siblings",
			pathA:          "/a/b/c/d/system",
			pathB:          "/a/b/c/d/users",
			shouldConflict: false,
			description:    "Deep nesting, still siblings",
		},
		{
			name:           "Root and subfolder - should conflict",
			pathA:          "/data",
			pathB:          "/data/system",
			shouldConflict: true,
			description:    "Parent folder contains subfolder",
		},
		{
			name:           "Completely different paths",
			pathA:          "/var/lib/system",
			pathB:          "/opt/users",
			shouldConflict: false,
			description:    "Totally different directory trees",
		},
		{
			name:           "Similar names but different parents",
			pathA:          "/var/system",
			pathB:          "/opt/system",
			shouldConflict: false,
			description:    "Same leaf name, different parents",
		},
		{
			name:           "User scenario with data files",
			pathA:          "./.sop-data/db/db1",
			pathB:          "./.sop-data/db/db2",
			shouldConflict: false,
			description:    "Data files as siblings in same folder",
		},
		{
			name:           "System scenario with data files",
			pathA:          "./.sop-data/system/sdb1",
			pathB:          "./.sop-data/system/sdb2",
			shouldConflict: false,
			description:    "System data files as siblings",
		},
		{
			name:           "Cross-database siblings",
			pathA:          "./.sop-data/system/stores",
			pathB:          "./.sop-data/db/stores",
			shouldConflict: false,
			description:    "Same folder name in different database dirs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conflict, msg := isPathConflict(tt.pathA, tt.pathB)

			if conflict != tt.shouldConflict {
				if tt.shouldConflict {
					t.Errorf("Expected conflict but got none.\n  Path A: %s\n  Path B: %s\n  Description: %s",
						tt.pathA, tt.pathB, tt.description)
				} else {
					t.Errorf("CRITICAL: False conflict detected!\n  Path A: %s\n  Path B: %s\n  Conflict message: %s\n  Description: %s",
						tt.pathA, tt.pathB, msg, tt.description)
				}
			}

			// Verify symmetric behavior (A vs B should be same as B vs A)
			conflictReverse, _ := isPathConflict(tt.pathB, tt.pathA)
			if conflict != conflictReverse {
				t.Errorf("Asymmetric conflict detection!\n  A vs B: %v\n  B vs A: %v\n  Path A: %s\n  Path B: %s",
					conflict, conflictReverse, tt.pathA, tt.pathB)
			}
		})
	}
}

// TestIsPathConflict_TrailingSeparatorHandling verifies that the trailing
// separator logic correctly prevents false positives like /tmp matching /tmp2.
func TestIsPathConflict_TrailingSeparatorHandling(t *testing.T) {
	tests := []struct {
		pathA          string
		pathB          string
		shouldConflict bool
	}{
		{"/tmp", "/tmp2", false},         // Classic false positive case
		{"/data1", "/data10", false},     // Similar names, different folders
		{"/data", "/data/", true},        // Same path with/without trailing slash - identical
		{"/var/log", "/var/log2", false}, // Common prefix but different folders
		{"/a/b/c", "/a/b/cd", false},     // Folder name starts with same letters
	}

	for _, tt := range tests {
		conflict, msg := isPathConflict(tt.pathA, tt.pathB)
		if conflict != tt.shouldConflict {
			if tt.shouldConflict {
				t.Errorf("Expected conflict for '%s' vs '%s'", tt.pathA, tt.pathB)
			} else {
				t.Errorf("FALSE POSITIVE: '%s' vs '%s' incorrectly detected as conflict: %s",
					tt.pathA, tt.pathB, msg)
			}
		}
	}
}

// TestValidatePathSafety_RealWorldScenarios tests the full validatePathSafety
// function with realistic database configurations.
func TestValidatePathSafety_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name          string
		newPaths      []string
		existingPaths []string
		shouldPass    bool
		description   string
	}{
		{
			name: "User's exact setup - system and user DBs as siblings",
			newPaths: []string{
				"./.sop-data/system",
				"./.sop-data/db",
			},
			existingPaths: []string{},
			shouldPass:    true,
			description:   "System DB at .sop-data/system, User DB at .sop-data/db - should NOT conflict",
		},
		{
			name: "Multiple user DBs as siblings",
			newPaths: []string{
				"./.sop-data/system",
				"./.sop-data/db1",
				"./.sop-data/db2",
				"./.sop-data/db3",
			},
			existingPaths: []string{},
			shouldPass:    true,
			description:   "Multiple databases all as siblings under .sop-data",
		},
		{
			name: "Data files within DB folders",
			newPaths: []string{
				"./.sop-data/system",
				"./.sop-data/db/db1",
				"./.sop-data/db/db2",
			},
			existingPaths: []string{},
			shouldPass:    true,
			description:   "System folder + multiple data files in user DB folder",
		},
		{
			name: "Conflicting nested path",
			newPaths: []string{
				"./.sop-data",
				"./.sop-data/system", // CONFLICT: inside .sop-data
			},
			existingPaths: []string{},
			shouldPass:    false,
			description:   "Should detect parent/child conflict",
		},
		{
			name: "New path conflicts with existing",
			newPaths: []string{
				"./.sop-data/new-db",
			},
			existingPaths: []string{
				"./.sop-data/new-db", // Already exists
			},
			shouldPass:  false,
			description: "Should detect conflict with existing path",
		},
		{
			name: "New path nested inside existing",
			newPaths: []string{
				"./.sop-data/system/subfolder",
			},
			existingPaths: []string{
				"./.sop-data/system", // Parent already exists
			},
			shouldPass:  false,
			description: "Should detect new path inside existing",
		},
		{
			name: "Complex multi-level sibling structure",
			newPaths: []string{
				"/var/lib/sop/prod/system",
				"/var/lib/sop/prod/users",
				"/var/lib/sop/prod/cache",
				"/var/lib/sop/staging/system",
				"/var/lib/sop/staging/users",
			},
			existingPaths: []string{},
			shouldPass:    true,
			description:   "Multi-level hierarchy with siblings at each level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePathSafety(tt.newPaths, tt.existingPaths)

			if tt.shouldPass && err != nil {
				t.Errorf("CRITICAL: validatePathSafety failed for valid configuration!\n  Error: %v\n  Description: %s\n  New paths: %v\n  Existing paths: %v",
					err, tt.description, tt.newPaths, tt.existingPaths)
			}

			if !tt.shouldPass && err == nil {
				t.Errorf("validatePathSafety should have detected conflict!\n  Description: %s\n  New paths: %v\n  Existing paths: %v",
					tt.description, tt.newPaths, tt.existingPaths)
			}
		})
	}
}
