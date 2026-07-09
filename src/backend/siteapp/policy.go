package siteapp

import (
	"strings"

	"github.com/sharedcode/sop"
)

const PermissionSiteAppAccess = "site_app_access"

var permissionEvaluators = map[string]func(sop.AuthContext) bool{
	PermissionSiteAppAccess: evaluateSiteAppAccess,
}

func EvaluatePermission(auth sop.AuthContext, permission string) bool {
	if auth.Permissions != nil {
		if allowed, ok := auth.Permissions[permission]; ok {
			return allowed
		}
	}
	if evaluator, ok := permissionEvaluators[permission]; ok {
		return evaluator(auth)
	}
	return false
}

func HasSiteAppAccess(auth sop.AuthContext) bool {
	return EvaluatePermission(auth, PermissionSiteAppAccess)
}

func evaluateSiteAppAccess(auth sop.AuthContext) bool {
	normalizedPlan := strings.ToLower(strings.TrimSpace(auth.SubscriptionPlan))
	return normalizedPlan != "" && normalizedPlan != "none"
}
