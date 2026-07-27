package sopbackend

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
)

func TestShouldExposeStoreInUI_HidesInternalStores(t *testing.T) {
	cases := []struct {
		name          string
		store         string
		userID        string
		isAdmin       bool
		isSystemAdmin bool
		exposed       bool
	}{
		{name: "plain store", store: "my_store", userID: "alice", exposed: true},
		{name: "component store hidden", store: "ltm_omni/items", userID: "alice", exposed: false},
		{name: "vector store hidden", store: "documents_vecs", userID: "alice", exposed: false},
		{name: "stm time index hidden", store: "stm_alice_by_time", userID: "alice", exposed: false},
		{name: "own stm visible", store: "stm_alice", userID: "alice", exposed: true},
		{name: "own ltm visible", store: "ltm_alice", userID: "alice", exposed: true},
		{name: "other user stm hidden", store: "stm_bob", userID: "alice", exposed: false},
		{name: "other user ltm hidden", store: "ltm_bob", userID: "alice", exposed: false},
		{name: "sessions hidden for non-admin", store: "sessions", userID: "alice", exposed: false},
		{name: "sessions hidden for admin", store: "sessions", userID: "root", isAdmin: true, exposed: false},
		{name: "sessions visible for system admin", store: "sessions", userID: "root", isSystemAdmin: true, exposed: true},
		{name: "users hidden for regular user", store: "users", userID: "alice", exposed: false},
		{name: "users visible for admin", store: "users", userID: "root", isAdmin: true, exposed: true},
		{name: "users visible for system admin", store: "users", userID: "root", isSystemAdmin: true, exposed: true},
		{name: "admin own stm hidden", store: "stm_root", userID: "root", isAdmin: true, exposed: false},
		{name: "system admin own stm visible", store: "stm_root", userID: "root", isSystemAdmin: true, exposed: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.userID != "" {
				roles := []string{}
				if tc.isAdmin {
					roles = append(roles, sop.RoleAdmin)
				}
				if tc.isSystemAdmin {
					roles = append(roles, sop.RoleSystemAdmin)
				}
				ctx = sop.ContextWithAuth(ctx, sop.AuthContext{UserID: tc.userID, Roles: roles})
			}
			if got := shouldExposeStoreInRBAC(ctx, tc.store); got != tc.exposed {
				t.Fatalf("shouldExposeStoreInRBAC(%q) = %v, want %v", tc.store, got, tc.exposed)
			}
		})
	}
}
