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

	if !shouldShowCategoryAutoShareControls(authCtx, "shared_memory", "workflow_pattern", false) {
		t.Fatal("expected controls to be visible in local daemon/UI for eligible categories")
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
