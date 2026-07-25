package capabilities

import (
	"context"
	"fmt"
	log "log/slog"
	"sync"
	"time"
)

// AuthorizationAudit captures authorization decision for audit trail
type AuthorizationAudit struct {
	Timestamp      time.Time
	UserID         string
	CapabilityID   string
	Operation      OperationContext
	Result         AuthorizationResult
	ApprovalNeeded bool
	ApprovalGiven  bool
}

// ApprovalHandler is called when an operation requires user approval
type ApprovalHandler func(ctx context.Context, op OperationContext) (bool, error)

// AuthorizationManager handles capability authorization checks with Gate 0-4 enforcement
type AuthorizationManager struct {
	mu               sync.RWMutex
	auditLog         []AuthorizationAudit
	approvalHandlers map[ApprovalType]ApprovalHandler
	userGate         AuthorizationGate // Current user's maximum gate level
}

// NewAuthorizationManager creates a new authorization manager with the specified user gate level
func NewAuthorizationManager(userGate AuthorizationGate) *AuthorizationManager {
	return &AuthorizationManager{
		auditLog:         make([]AuthorizationAudit, 0, 1000),
		approvalHandlers: make(map[ApprovalType]ApprovalHandler),
		userGate:         userGate,
	}
}

// RegisterApprovalHandler registers a handler for a specific approval type
func (am *AuthorizationManager) RegisterApprovalHandler(approvalType ApprovalType, handler ApprovalHandler) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.approvalHandlers[approvalType] = handler
}

// Authorize checks if a capability operation is authorized based on Gate 0-4 levels
func (am *AuthorizationManager) Authorize(ctx context.Context, capability Capability, op OperationContext) (AuthorizationResult, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	ent := capability.Entitlement()
	result := AuthorizationResult{
		Timestamp: time.Now(),
		Gate:      ent.RequiredGate,
	}

	// Check if user's gate level is sufficient
	if am.userGate < ent.RequiredGate {
		result.Allowed = false
		result.Reason = fmt.Sprintf("Insufficient authorization: user gate %d (%s) < required gate %d (%s)",
			am.userGate, am.userGate.String(),
			ent.RequiredGate, ent.RequiredGate.String())
		am.recordAudit(ctx, capability, op, result, false, false)
		return result, nil
	}

	// Check if approval is needed
	if len(ent.Approvals) > 0 {
		for _, approvalType := range ent.Approvals {
			if approvalType == ApprovalNone {
				continue
			}

			handler, ok := am.approvalHandlers[approvalType]
			if !ok {
				result.Allowed = false
				result.Reason = fmt.Sprintf("No approval handler registered for type: %s", approvalType)
				am.recordAudit(ctx, capability, op, result, true, false)
				return result, nil
			}

			approved, err := handler(ctx, op)
			if err != nil {
				result.Allowed = false
				result.Reason = fmt.Sprintf("Approval handler error: %v", err)
				am.recordAudit(ctx, capability, op, result, true, false)
				return result, err
			}

			if !approved {
				result.Allowed = false
				result.Reason = fmt.Sprintf("User denied approval for: %s", approvalType)
				am.recordAudit(ctx, capability, op, result, true, false)
				return result, nil
			}

			result.ApprovalGiven = true
		}
	}

	// Check fine-grained permissions
	if len(ent.Permissions) > 0 {
		if !am.checkPermissions(op, ent.Permissions) {
			result.Allowed = false
			result.Reason = fmt.Sprintf("Permission denied for operation: %s on resource: %s", op.Action, op.Resource)
			am.recordAudit(ctx, capability, op, result, false, result.ApprovalGiven)
			return result, nil
		}
	}

	// Authorization succeeded
	result.Allowed = true
	result.Reason = "Authorized"
	am.recordAudit(ctx, capability, op, result, len(ent.Approvals) > 0, result.ApprovalGiven)

	log.Info("Authorization granted",
		"capability", capability.ID(),
		"gate", ent.RequiredGate.String(),
		"approval_needed", len(ent.Approvals) > 0,
		"approval_given", result.ApprovalGiven)

	return result, nil
}

// checkPermissions verifies if operation is allowed by fine-grained permissions
func (am *AuthorizationManager) checkPermissions(op OperationContext, permissions []Permission) bool {
	for _, perm := range permissions {
		if perm.Resource == op.Resource {
			// Check if user gate meets permission requirement
			if am.userGate < perm.Gate {
				return false
			}
			// Check if action is allowed
			for _, action := range perm.Actions {
				if action == op.Action {
					return true
				}
			}
		}
	}
	// If no matching permission found, deny by default
	return false
}

// recordAudit adds an entry to the audit log
func (am *AuthorizationManager) recordAudit(ctx context.Context, capability Capability, op OperationContext, result AuthorizationResult, approvalNeeded, approvalGiven bool) {
	userID := "unknown"
	// Try to extract user ID from context
	if v := ctx.Value("user_id"); v != nil {
		if uid, ok := v.(string); ok {
			userID = uid
		}
	}

	audit := AuthorizationAudit{
		Timestamp:      result.Timestamp,
		UserID:         userID,
		CapabilityID:   capability.ID(),
		Operation:      op,
		Result:         result,
		ApprovalNeeded: approvalNeeded,
		ApprovalGiven:  approvalGiven,
	}

	am.auditLog = append(am.auditLog, audit)

	// Keep audit log size bounded (last 1000 entries)
	if len(am.auditLog) > 1000 {
		am.auditLog = am.auditLog[len(am.auditLog)-1000:]
	}
}

// GetAuditLog returns the authorization audit log (last 1000 entries)
func (am *AuthorizationManager) GetAuditLog() []AuthorizationAudit {
	am.mu.RLock()
	defer am.mu.RUnlock()

	// Return copy to prevent external mutation
	result := make([]AuthorizationAudit, len(am.auditLog))
	copy(result, am.auditLog)
	return result
}

// SetUserGate updates the current user's maximum gate level
func (am *AuthorizationManager) SetUserGate(gate AuthorizationGate) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.userGate = gate
	log.Info("User gate level updated", "gate", gate.String())
}

// GetUserGate returns the current user's maximum gate level
func (am *AuthorizationManager) GetUserGate() AuthorizationGate {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.userGate
}
