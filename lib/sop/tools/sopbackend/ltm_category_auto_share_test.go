package sopbackend

import (
	"testing"

	"github.com/sharedcode/sop"
)

func TestShouldShowCategoryAutoShareControls(t *testing.T) {
	authCtx := sop.AuthContext{Roles: []string{sop.RoleSystemAdmin}}

	if shouldShowCategoryAutoShareControls(authCtx, "core_playbook", "Fleet Learning/workflow_pattern", true) {
		t.Fatal("expected controls to remain hidden for eligible LTM categories on HQ with the current allowlist matching")
	}

	if shouldShowCategoryAutoShareControls(authCtx, "core_playbook", "debug_logs", true) {
		t.Fatal("expected controls to be hidden for non-federation categories")
	}

	if shouldShowCategoryAutoShareControls(authCtx, "regular_space", "workflow_pattern", true) {
		t.Fatal("expected controls to be hidden for non-LTM spaces")
	}

	if shouldShowCategoryAutoShareControls(sop.AuthContext{Roles: []string{sop.RoleUser}}, "core_playbook", "Fleet Learning/workflow_pattern", true) {
		t.Fatal("expected controls to be hidden for non-admin users")
	}

	if shouldShowCategoryAutoShareControls(authCtx, "core_playbook", "Fleet Learning/workflow_pattern", false) {
		t.Fatal("expected controls to remain hidden in local runtime/UI with the current allowlist matching")
	}
}

func TestAnnotateCategoryAutoShareVisibility(t *testing.T) {
	authCtx := sop.AuthContext{Roles: []string{sop.RoleSystemAdmin}}
	category := map[string]any{"path": "workflow_pattern"}

	annotateCategoryAutoShareVisibility(category, authCtx, "shared_memory", true)

	visible, ok := category["show_category_auto_share_controls"].(bool)
	if !ok || !visible {
		t.Fatal("expected eligible category to expose auto-share controls")
	}

	category["path"] = "debug_logs"
	annotateCategoryAutoShareVisibility(category, authCtx, "shared_memory", true)

	visible, ok = category["show_category_auto_share_controls"].(bool)
	if !ok || visible {
		t.Fatal("expected non-eligible category to hide auto-share controls")
	}
}
