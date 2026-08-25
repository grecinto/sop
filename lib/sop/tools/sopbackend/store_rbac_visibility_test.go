package sopbackend

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
)

func TestStoreRBAC_VisibilityRules(t *testing.T) {
	cases := []struct {
		name    string
		ctx     context.Context
		store   string
		exposed bool
	}{
		{name: "plain store visible", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice"}), store: "my_custom_store", exposed: true},
		{name: "own stm visible", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice"}), store: "alice_stm", exposed: true},
		{name: "other user stm hidden", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice"}), store: "bob_stm", exposed: false},
		{name: "time index hidden", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice"}), store: "alice_stm_by_time", exposed: false},
		{name: "sessions hidden for non-admin", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice"}), store: "sessions", exposed: false},
		{name: "sessions hidden for admin", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{sop.RoleAdmin}, UserID: "root"}), store: "sessions", exposed: false},
		{name: "sessions visible for system admin", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{sop.RoleSystemAdmin}, UserID: "root"}), store: "sessions", exposed: true},
		{name: "own stm visible for admin", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{sop.RoleAdmin}, UserID: "root"}), store: "root_stm", exposed: true},
		{name: "own stm visible for system admin", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{Roles: []string{sop.RoleSystemAdmin}, UserID: "root"}), store: "root_stm", exposed: true},
		{name: "bootstrap metadata hidden", ctx: sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice"}), store: bootstrapStoreName, exposed: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldExposeStoreInRBAC(tc.ctx, tc.store); got != tc.exposed {
				t.Fatalf("shouldExposeStoreInRBAC(%q) = %v, want %v", tc.store, got, tc.exposed)
			}
		})
	}
}
