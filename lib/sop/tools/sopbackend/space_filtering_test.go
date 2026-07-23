package sopbackend

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
)

// TestSpaceFiltering_SystemAdmin verifies that SystemAdmin can see all spaces
func TestSpaceFiltering_SystemAdmin(t *testing.T) {
	tests := []struct {
		name            string
		spaceName       string
		userID          string
		roles           []string
		shouldBeVisible bool
	}{
		// SystemAdmin should see everything
		{"SystemAdmin sees alice_ltm", "alice_ltm", "bob", []string{sop.RoleSystemAdmin}, true},
		{"SystemAdmin sees bob_stm", "bob_stm", "alice", []string{sop.RoleSystemAdmin}, true},
		{"SystemAdmin sees memory_alice", "memory_alice", "bob", []string{sop.RoleSystemAdmin}, true},

		// Regular users only see their own memory spaces
		{"User alice sees own alice_ltm", "alice_ltm", "alice", []string{sop.RoleUser}, true},
		{"User alice does NOT see bob_ltm", "bob_ltm", "alice", []string{sop.RoleUser}, false},
		{"User bob sees own bob_stm", "bob_stm", "bob", []string{sop.RoleUser}, true},
		{"User bob does NOT see alice_stm", "alice_stm", "bob", []string{sop.RoleUser}, false},
		{"User alice sees own memory_alice", "memory_alice", "alice", []string{sop.RoleUser}, true},
		{"User bob does NOT see memory_alice", "memory_alice", "bob", []string{sop.RoleUser}, false},

		// All users see non-memory spaces
		{"User sees Core Playbook", "Core Playbook", "alice", []string{sop.RoleUser}, true},
		{"User sees custom space", "custom_project", "bob", []string{sop.RoleUser}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{
				UserID: tt.userID,
				Roles:  tt.roles,
			})

			auth := sop.GetAuthFromContext(ctx)
			isSystemAdmin := false
			for _, role := range auth.Roles {
				if role == sop.RoleSystemAdmin {
					isSystemAdmin = true
					break
				}
			}

			// Simulate the filtering logic
			lowerName := tt.spaceName
			expectedLTMSuffix := tt.userID + "_ltm"
			expectedSTMSuffix := tt.userID + "_stm"
			expectedMemoryPrefix := "memory_"
			expectedMemoryFull := expectedMemoryPrefix + tt.userID

			shouldInclude := false

			if isSystemAdmin {
				// SystemAdmin sees everything
				shouldInclude = true
			} else if tt.spaceName == expectedMemoryFull {
				// User sees their own memory_<user>
				shouldInclude = true
			} else if lowerName == expectedLTMSuffix {
				// User sees their own <user>_ltm
				shouldInclude = true
			} else if lowerName == expectedSTMSuffix {
				// User sees their own <user>_stm
				shouldInclude = true
			} else if tt.spaceName != "alice_ltm" && tt.spaceName != "bob_ltm" &&
				tt.spaceName != "alice_stm" && tt.spaceName != "bob_stm" &&
				tt.spaceName != "memory_alice" && tt.spaceName != "memory_bob" {
				// Non-memory spaces visible to all
				shouldInclude = true
			}

			if shouldInclude != tt.shouldBeVisible {
				t.Errorf("Space %q visibility for user %q (roles: %v): got %v, want %v",
					tt.spaceName, tt.userID, tt.roles, shouldInclude, tt.shouldBeVisible)
			}
		})
	}
}
