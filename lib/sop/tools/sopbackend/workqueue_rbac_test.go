package sopbackend

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
)

// TestWorkQueueRBAC_ReadOnly verifies that WorkQueue stores are read-only for all users
func TestWorkQueueRBAC_ReadOnly(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		store    string
		wantRead bool
		wantEdit bool
	}{
		{
			name:     "regular user can read own queue",
			ctx:      sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}),
			store:    "wfq_checkpoints_alice",
			wantRead: true,
			wantEdit: false,
		},
		{
			name:     "system admin can read any queue",
			ctx:      sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}),
			store:    "wfq_metrics_alice",
			wantRead: true,
			wantEdit: false,
		},
		{
			name:     "admin can read any queue",
			ctx:      sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "admin", Roles: []string{sop.RoleAdmin}}),
			store:    "wfq_bytimestamp_bob",
			wantRead: true,
			wantEdit: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rbac := sop.ResolveRBACMap(tc.ctx, "store", sop.EntitlementContext{AssetID: tc.store, Database: "demo"}, nil)

			if got := rbac[sop.UICapabilityRead]; got != tc.wantRead {
				t.Errorf("can_read = %v, want %v", got, tc.wantRead)
			}
			if got := rbac[sop.UICapabilityEdit]; got != tc.wantEdit {
				t.Errorf("can_edit = %v, want %v (WorkQueue stores should be read-only)", got, tc.wantEdit)
			}
			if got := rbac[sop.UICapabilityDelete]; got != false {
				t.Errorf("can_delete = %v, want false (WorkQueue stores should be read-only)", got)
			}
		})
	}
}

// TestWorkQueueRBAC_VisibilityIsolation verifies that users can only see their own WorkQueue stores
func TestWorkQueueRBAC_VisibilityIsolation(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		store   string
		exposed bool
	}{
		{
			name:    "user can see own checkpoints",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}),
			store:   "wfq_checkpoints_alice",
			exposed: true,
		},
		{
			name:    "user cannot see other user's checkpoints",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}),
			store:   "wfq_checkpoints_bob",
			exposed: false,
		},
		{
			name:    "user can see own metrics",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "bob", Roles: []string{sop.RoleUser}}),
			store:   "wfq_metrics_bob",
			exposed: true,
		},
		{
			name:    "user cannot see other user's metrics",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "bob", Roles: []string{sop.RoleUser}}),
			store:   "wfq_metrics_alice",
			exposed: false,
		},
		{
			name:    "system admin can see all queues",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}),
			store:   "wfq_checkpoints_alice",
			exposed: true,
		},
		{
			name:    "system admin can see other user's queues",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}),
			store:   "wfq_bytimestamp_bob",
			exposed: true,
		},
		{
			name:    "regular admin can see all queues",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "admin", Roles: []string{sop.RoleAdmin}}),
			store:   "wfq_checkpoints_alice",
			exposed: true,
		},
		{
			name:    "regular admin can see other user's queues",
			ctx:     sop.ContextWithAuth(context.Background(), sop.AuthContext{UserID: "admin", Roles: []string{sop.RoleAdmin}}),
			store:   "wfq_metrics_bob",
			exposed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldExposeStoreInRBAC(tc.ctx, tc.store)
			if got != tc.exposed {
				t.Errorf("shouldExposeStoreInRBAC(%q) = %v, want %v", tc.store, got, tc.exposed)
			}
		})
	}
}

// TestWorkQueueRBAC_DisplayNames verifies that WorkQueue stores get user-friendly display names
func TestWorkQueueRBAC_DisplayNames(t *testing.T) {
	tests := []struct {
		storeName   string
		wantDisplay string
	}{
		{"wfq_checkpoints_alice", "AI Studio - Workflow Checkpoints"},
		{"wfq_metrics_bob", "AI Studio - Workflow Metrics"},
		{"wfq_bytimestamp_charlie", "AI Studio - Workflow Timeline"},
		{"stm_alice", "Short-term Memory"},
		{"ltm_bob", "Long-term Memory"},
		{"users", "users"}, // Regular stores keep their name
	}

	for _, tc := range tests {
		t.Run(tc.storeName, func(t *testing.T) {
			got := resolveStoreDisplayName(tc.storeName)
			if got != tc.wantDisplay {
				t.Errorf("resolveStoreDisplayName(%q) = %q, want %q", tc.storeName, got, tc.wantDisplay)
			}
		})
	}
}
