package sopbackend

import (
	"testing"

	"github.com/sharedcode/sop"
)

func TestCanManageEnvironments(t *testing.T) {
	adminAuth := sop.AuthContext{Roles: []string{sop.RoleAdmin}, UserID: "root"}
	if !CanManageEnvironments(adminAuth) {
		t.Fatal("expected admin role to be allowed to manage environments")
	}

	userAuth := sop.AuthContext{Roles: []string{sop.RoleUser}, UserID: "alice"}
	if !CanManageEnvironments(userAuth) {
		t.Fatal("expected environment management to remain visible by default unless explicitly denied")
	}
}

func TestCanManageEnvironmentsHonorsExplicitPermissionDeny(t *testing.T) {
	auth := sop.AuthContext{
		Roles:       []string{sop.RoleAdmin},
		UserID:      "root",
		Permissions: map[string]bool{"manage_environments": false},
	}
	if CanManageEnvironments(auth) {
		t.Fatal("expected explicit deny permission to hide environment management")
	}
}

func TestCanManageEnvironmentsHonorsDisabledAuthMode(t *testing.T) {
	defer SetDisableAuth(false)
	SetDisableAuth(true)

	auth := sop.AuthContext{Roles: []string{sop.RoleUser}, UserID: "alice"}
	if CanManageEnvironments(auth) {
		t.Fatal("expected disabled auth mode to hide environment management")
	}
}
