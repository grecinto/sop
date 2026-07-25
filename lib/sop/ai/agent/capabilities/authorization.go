package capabilities

import (
	"time"
)

// AuthorizationGate represents progressive access control levels (Gate 0-4)
type AuthorizationGate int

const (
	// GateNone: No authorization required (public tools)
	// Examples: help, documentation, read-only queries
	GateNone AuthorizationGate = 0

	// GateBasicAccess: Requires authenticated user
	// Examples: basic searches, knowledge base queries
	GateBasicAccess AuthorizationGate = 1

	// GateReadAccess: Read-only operations
	// Examples: file reads, git status, database queries
	GateReadAccess AuthorizationGate = 2

	// GateWriteAccess: Write operations (requires approval)
	// Examples: file writes, database mutations, git commits
	GateWriteAccess AuthorizationGate = 3

	// GateSystemAccess: System-level operations (admin only)
	// Examples: system commands, configuration changes, user management
	GateSystemAccess AuthorizationGate = 4
)

// String returns the string representation of the authorization gate
func (g AuthorizationGate) String() string {
	switch g {
	case GateNone:
		return "none"
	case GateBasicAccess:
		return "basic_access"
	case GateReadAccess:
		return "read_access"
	case GateWriteAccess:
		return "write_access"
	case GateSystemAccess:
		return "system_access"
	default:
		return "unknown"
	}
}

// ApprovalType defines what kind of approval is required for an operation
type ApprovalType string

const (
	ApprovalNone        ApprovalType = "none"
	ApprovalFileWrite   ApprovalType = "file_write"
	ApprovalShellExec   ApprovalType = "shell_exec"
	ApprovalAPICall     ApprovalType = "api_call"
	ApprovalDBMutation  ApprovalType = "db_mutation"
	ApprovalGitCommit   ApprovalType = "git_commit"
	ApprovalSecureInput ApprovalType = "secure_input" // Passwords, tokens, API keys
)

// Permission defines fine-grained operation permissions
type Permission struct {
	Resource string            // "file", "shell", "database", "api", "git"
	Actions  []string          // ["read", "write", "delete", "execute"]
	Gate     AuthorizationGate // Required gate level for this permission
}

// OperationContext captures the context of an operation requiring authorization
type OperationContext struct {
	OperationType string
	Resource      string
	Action        string
	Metadata      map[string]any

	// Privacy flags for secure data handling
	// When SecureInputRequired=true:
	//   - User will be prompted via chat UI with privacy notice
	//   - Input will be sent DIRECTLY to the tool/shell (bypasses LLM)
	//   - LLM will NOT see the sensitive data (passwords, tokens, keys)
	//   - Event streamer should display privacy warning to user
	SecureInputRequired bool
	SecureInputPrompt   string // Prompt text to show user (e.g., "Enter database password:")
}

// AuthorizationResult captures the outcome of an authorization check
type AuthorizationResult struct {
	Allowed       bool
	Gate          AuthorizationGate
	ApprovalGiven bool
	Reason        string
	Timestamp     time.Time
}
