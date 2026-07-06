package sopbackend

import (
	"testing"

	"github.com/sharedcode/sop"
)

func TestShouldShowCategoryAutoShareControls(t *testing.T) {
	authCtx := sop.AuthContext{Roles: []string{sop.RoleSystemAdmin}}

	if !shouldShowCategoryAutoShareControls(authCtx, "shared_memory", "workflow_pattern", true) {
		t.Fatal("expected controls to be visible for eligible LTM category on HQ")
	}

	if shouldShowCategoryAutoShareControls(authCtx, "shared_memory", "debug_logs", true) {
		t.Fatal("expected controls to be hidden for non-federation categories")
	}

	if shouldShowCategoryAutoShareControls(authCtx, "regular_space", "workflow_pattern", true) {
		t.Fatal("expected controls to be hidden for non-LTM spaces")
	}

	if shouldShowCategoryAutoShareControls(sop.AuthContext{Roles: []string{sop.RoleUser}}, "shared_memory", "workflow_pattern", true) {
		t.Fatal("expected controls to be hidden for non-admin users")
	}

	if shouldShowCategoryAutoShareControls(authCtx, "shared_memory", "workflow_pattern", false) {
		t.Fatal("expected controls to be hidden outside HQ")
	}
}
