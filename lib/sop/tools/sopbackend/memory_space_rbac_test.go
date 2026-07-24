package sopbackend

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
)

// TestMemorySpaceRBAC_SystemAdminCanManage verifies SystemAdmin can write/delete memory spaces
func TestMemorySpaceRBAC_SystemAdminCanManage(t *testing.T) {
	tests := []struct {
		name       string
		spaceID    string
		action     sop.Action
		userRoles  []string
		isSystemDB bool
		allowed    bool
	}{
		// SystemAdmin can write to memory spaces
		{"SystemAdmin writes alice_ltm", "alice_ltm", sop.ActionWrite, []string{sop.RoleSystemAdmin}, true, true},
		{"SystemAdmin writes bob_stm", "bob_stm", sop.ActionWrite, []string{sop.RoleSystemAdmin}, true, true},
		{"SystemAdmin writes memory_alice", "memory_alice", sop.ActionWrite, []string{sop.RoleSystemAdmin}, true, true},
		{"SystemAdmin writes core_playbook", "core_playbook", sop.ActionWrite, []string{sop.RoleSystemAdmin}, true, true},
		{"SystemAdmin writes Core Playbook", "Core Playbook", sop.ActionWrite, []string{sop.RoleSystemAdmin}, true, true},

		// SystemAdmin can delete memory spaces
		{"SystemAdmin deletes alice_ltm", "alice_ltm", sop.ActionDelete, []string{sop.RoleSystemAdmin}, true, true},
		{"SystemAdmin deletes bob_stm", "bob_stm", sop.ActionDelete, []string{sop.RoleSystemAdmin}, true, true},

		// Regular users cannot write to memory spaces
		{"User cannot write alice_ltm", "alice_ltm", sop.ActionWrite, []string{sop.RoleUser}, true, false},
		{"User cannot write bob_stm", "bob_stm", sop.ActionWrite, []string{sop.RoleUser}, true, false},
		{"User cannot write memory_alice", "memory_alice", sop.ActionWrite, []string{sop.RoleUser}, true, false},
		{"User cannot write core_playbook", "core_playbook", sop.ActionWrite, []string{sop.RoleUser}, true, false},
		{"User cannot write Core Playbook", "Core Playbook", sop.ActionWrite, []string{sop.RoleUser}, true, false},

		// Regular users cannot delete memory spaces
		{"User cannot delete alice_ltm", "alice_ltm", sop.ActionDelete, []string{sop.RoleUser}, true, false},
		{"User cannot delete bob_stm", "bob_stm", sop.ActionDelete, []string{sop.RoleUser}, true, false},

		// Admin (non-SystemAdmin) cannot write to memory spaces
		{"Admin cannot write alice_ltm", "alice_ltm", sop.ActionWrite, []string{sop.RoleAdmin}, true, false},
		{"Admin cannot write bob_stm", "bob_stm", sop.ActionWrite, []string{sop.RoleAdmin}, true, false},

		// Everyone can read memory spaces
		{"User can read alice_ltm", "alice_ltm", sop.ActionRead, []string{sop.RoleUser}, true, true},
		{"SystemAdmin can read bob_stm", "bob_stm", sop.ActionRead, []string{sop.RoleSystemAdmin}, true, true},

		// Custom spaces are writable by all (not readonly)
		{"User can write custom_project", "custom_project", sop.ActionWrite, []string{sop.RoleUser}, true, true},
		{"Admin can write custom_project", "custom_project", sop.ActionWrite, []string{sop.RoleAdmin}, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{
				UserID: "test_user",
				Roles:  tt.userRoles,
			})

			entCtx := sop.EntitlementContext{
				AssetID:    tt.spaceID,
				IsSystemDB: tt.isSystemDB,
			}

			// Get the space RBAC blueprint
			registerBuiltinRBACBlueprints() // Ensure blueprints are registered
			blueprint, ok := sop.GetAssetBlueprint("space")
			if !ok {
				t.Fatal("space RBAC blueprint not found - did registerBuiltinRBACBlueprints() run?")
			}

			// Evaluate permission
			allowed := blueprint.Evaluator(ctx, entCtx, tt.action, nil)

			if allowed != tt.allowed {
				t.Errorf("%s: got %v, want %v (roles: %v, action: %v, space: %v)",
					tt.name, allowed, tt.allowed, tt.userRoles, tt.action, tt.spaceID)
			}
		})
	}
}

// TestMemorySpaceRBAC_AISelectBlocked verifies AI select is blocked for memory spaces
func TestMemorySpaceRBAC_AISelectBlocked(t *testing.T) {
	tests := []struct {
		name    string
		spaceID string
		blocked bool
	}{
		{"AI select blocked for alice_ltm", "alice_ltm", true},
		{"AI select blocked for bob_stm", "bob_stm", true},
		{"AI select blocked for memory_alice", "memory_alice", true},
		{"AI select blocked for shared_memory", "shared_memory", true},
		{"AI select blocked for Core Playbook", "Core Playbook", true},
		{"AI select allowed for custom_project", "custom_project", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sop.ContextWithAuth(context.Background(), sop.AuthContext{
				UserID: "test_user",
				Roles:  []string{sop.RoleUser},
			})

			entCtx := sop.EntitlementContext{
				AssetID:    tt.spaceID,
				IsSystemDB: true,
			}

			registerBuiltinRBACBlueprints() // Ensure blueprints are registered
			blueprint, ok := sop.GetAssetBlueprint("space")
			if !ok {
				t.Fatal("space RBAC blueprint not found")
			}

			allowed := blueprint.Evaluator(ctx, entCtx, sop.ActionAISelect, nil)
			expectedAllowed := !tt.blocked

			if allowed != expectedAllowed {
				t.Errorf("%s: got %v, want %v (space: %v)",
					tt.name, allowed, expectedAllowed, tt.spaceID)
			}
		})
	}
}
