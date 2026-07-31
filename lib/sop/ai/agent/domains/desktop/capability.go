package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	CapabilityID   = "desktop"
	CapabilityName = "Desktop Automation"
)

// Capability provides desktop automation through local command execution.
//
// This capability implements PrefixHandler to enable fast-path command execution via
// prefixes like /c, /cmd, /shell, /exec. When users type these prefixes, commands execute
// immediately without entering the ReAct reasoning loop.
//
// Architecture: See ARCHITECTURE.md § 9. Capability Prefix Routing
//
// Prefix Routing Examples:
//
//	/cmd ls -la          → Executes "ls -la" directly
//	/shell pwd           → Executes "pwd" directly
//	/exec ./build.sh     → Executes "./build.sh" directly
//
// The capability requires daemon mode (local agent) for command execution.
type Capability struct {
	// commandExecutor is injected to execute local commands (for LLM tool calls)
	commandExecutor func(context.Context, map[string]any) (string, error)
	// rawCommandExecutor is injected for direct /c commands (bypasses tool result events)
	rawCommandExecutor func(context.Context, map[string]any) (string, error)
	// shellSessionManager provides persistent shell sessions per conversation
	shellSessionManager ShellSessionManager
}

// ShellSessionManager interface for persistent shell sessions
type ShellSessionManager interface {
	Run(sessionID, command string, timeout time.Duration) (string, bool, error)
}

func NewCapability() *Capability { return &Capability{} }

// SetCommandExecutor injects the command execution function from the agent (for LLM tool calls)
func (c *Capability) SetCommandExecutor(executor func(context.Context, map[string]any) (string, error)) {
	c.commandExecutor = executor
}

// SetRawCommandExecutor injects the raw command executor for direct /c commands
func (c *Capability) SetRawCommandExecutor(executor func(context.Context, map[string]any) (string, error)) {
	c.rawCommandExecutor = executor
}

// SetShellSessionManager injects the shell session manager
func (c *Capability) SetShellSessionManager(mgr ShellSessionManager) {
	c.shellSessionManager = mgr
}

// getSessionIDFromContext extracts the SessionID from context
func getSessionIDFromContext(ctx context.Context) string {
	// Use string literal to avoid circular import with agent package
	if payload, ok := ctx.Value("session_payload").(*ai.SessionPayload); ok && payload != nil {
		if payload.SessionID != "" {
			return payload.SessionID
		}
	}
	return "default"
}

// isStatefulCommand returns true if the command modifies shell state (cd, export, etc.)
func isStatefulCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false
	}
	// Check for state-changing commands
	if strings.HasPrefix(trimmed, "cd ") || trimmed == "cd" {
		return true
	}
	if strings.HasPrefix(trimmed, "export ") {
		return true
	}
	// Session management commands
	if strings.EqualFold(trimmed, "status") || strings.EqualFold(trimmed, "stop") {
		return true
	}
	return false
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: true,
		Tier:             "base",
		RequiredGate:     capabilities.GateWriteAccess, // Gap 1: Shell execution needs write
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalShellExec, // Shell execution needs approval
		},
		Permissions: []capabilities.Permission{
			{
				Resource: "shell",
				Actions:  []string{"execute", "read"},
				Gate:     capabilities.GateWriteAccess,
			},
			{
				Resource: "git",
				Actions:  []string{"commit", "push", "fetch"},
				Gate:     capabilities.GateWriteAccess,
			},
			{
				Resource: "filesystem",
				Actions:  []string{"read"},
				Gate:     capabilities.GateReadAccess,
			},
		},
	}
}
func (c *Capability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if state.Domain == "" {
		return capabilities.ClassificationResult{}
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "desktop_automation" || hint == "local_ops" {
			return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
		}
		if hint == "coding_workflow" || hint == "repo_edit" {
			return capabilities.ClassificationResult{}
		}
	}
	if state.Domain == "Desktop" || state.Domain == "Desktop Automation" || state.Domain == CapabilityID || state.Domain == CapabilityName {
		return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
	}
	return capabilities.ClassificationResult{}
}
func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Desktop Automation\n- Use local automation tools for shell, git, file, and workspace tasks when the request involves repository work, local file editing, or developer workflow operations."
}
func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{
		Name:        "execute_local_command",
		Description: "Executes a shell command on the user's local machine after approval.",
		Schema:      "{}",
		Tags:        []string{"R", "U"}, // Can read (ls, cat) or update (write, modify files)
	}}
}
func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return `Desktop Automation Workflow Guide:

State Persistence:
- Commands run in a persistent shell session per conversation
- Working directory (cd) persists across commands
- Environment variables (export) persist across commands
- Use this for multi-step workflows: cd into directory, then run commands there

Background Execution (Use Sparingly):
- Default: Run commands SEQUENTIALLY (safe, predictable, no race conditions)
- Set background=true ONLY when user explicitly requests parallel execution
- Clear user intent signals:
  * "in the background"
  * "in parallel"
  * "while I..."
  * "simultaneously"
- When user intent is unclear: ASK before using background=true
- Safe background examples (when user confirms):
  * Dev servers: npm run dev, python -m http.server, rails server
  * File watchers: webpack --watch, tsc --watch, nodemon
  * Long-running servers: postgres, redis-server, mongod

Sequential vs Parallel Decision:
- Sequential (default): "build and test" → build finishes, then test runs
- Parallel (user requested): "start dev server in background and run tests" → both run simultaneously
- Unclear: "run dev server and tests" → ASK: "Should I run the dev server in the background (parallel) or wait for it to complete first (sequential)?"

Coding Workflow Pattern:
1. Navigate to correct directory: execute_local_command(command="cd /path/to/project")
2. Build/install (sequential): execute_local_command(command="npm install")
3. Start dev server (only if user wants background): execute_local_command(command="npm run dev", background=true)
4. Run tests (sequential): execute_local_command(command="npm test")

Session Recovery:
- If a command times out or hangs, the next command will auto-recover
- Stuck sessions are automatically detected and reset
- No manual intervention needed`
}
func (c *Capability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (c *Capability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (c *Capability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	_ = ctx
	_ = query
	_ = state
	_ = result
	return nil
}

// GetHandledPrefixes returns the supported command prefixes this capability handles.
// Only explicit slash-prefixed shortcuts are exposed to avoid ambiguity with natural-language prompts.
func (c *Capability) GetHandledPrefixes() []string {
	return []string{"/c", "/cmd", "/shell", "/exec"}
}

// HandlePrefix executes a command that starts with a registered prefix
func (c *Capability) HandlePrefix(ctx context.Context, query string, normalizedCommand string) (string, error) {
	if normalizedCommand == "" {
		return "", fmt.Errorf("empty command: provide a command to execute")
	}

	// Validate command is not just whitespace
	if len(strings.TrimSpace(normalizedCommand)) == 0 {
		return "", fmt.Errorf("command contains only whitespace")
	}

	// Route stateful commands through persistent shell session
	if c.shellSessionManager != nil && isStatefulCommand(normalizedCommand) {
		sessionID := getSessionIDFromContext(ctx)
		output, stillRunning, err := c.shellSessionManager.Run(sessionID, normalizedCommand, 30*time.Second)

		if err != nil {
			return "", fmt.Errorf("shell session error: %w", err)
		}

		// Handle cd command - return working directory feedback
		if strings.HasPrefix(strings.TrimSpace(normalizedCommand), "cd ") {
			// Get current directory by running pwd in the same session
			pwdOutput, _, pwdErr := c.shellSessionManager.Run(sessionID, "pwd", 5*time.Second)
			if pwdErr == nil && strings.TrimSpace(pwdOutput) != "" {
				return fmt.Sprintf("📁 Changed directory to: %s", strings.TrimSpace(pwdOutput)), nil
			}
			return "📁 Directory changed", nil
		}

		// Return status for long-running or session commands
		if stillRunning {
			return fmt.Sprintf("⏳ Still running...\n%s", output), nil
		}

		return output, nil
	}

	// One-shot execution for simple commands
	// Use rawCommandExecutor if available (for direct /c commands) to avoid double-emission
	executor := c.rawCommandExecutor
	if executor == nil {
		executor = c.commandExecutor
	}
	if executor == nil {
		return "", fmt.Errorf("desktop automation command executor not available")
	}

	// Normalize ls commands to use multi-column output
	normalizedCommand = normalizeLsCommand(normalizedCommand)

	// Execute the command via the injected executor
	result, err := executor(ctx, map[string]any{
		"command":   normalizedCommand,
		"reasoning": "Direct local command execution requested by user.",
	})

	if err != nil {
		return result, err
	}

	// Parse the JSON response from daemon
	var response struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}

	if jsonErr := json.Unmarshal([]byte(result), &response); jsonErr == nil {
		// Successfully parsed JSON - extract stdout
		output := response.Stdout

		// Include stderr if present
		if response.Stderr != "" {
			output += "\n⚠️ Errors:\n" + response.Stderr
		}

		// Emit formatted result for direct commands (rawCommandExecutor path)
		if c.rawCommandExecutor != nil {
			if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
				streamer("content", fmt.Sprintf("🛠️ Result from local_command:\n%s", output))
			}
		}

		// Return raw output - let the frontend renderer handle display formatting
		return output, nil
	}

	// Not JSON or parse failed - return as-is (might be ACTION_REQUIRED payload)
	return result, nil
}

// normalizeLsCommand adds -C flag to ls commands to force multi-column output.
// Without -C, ls defaults to single-column when output is piped (no TTY).
// The -C flag forces columnar display using the COLUMNS env variable.
func normalizeLsCommand(command string) string {
	cmdLower := strings.ToLower(strings.TrimSpace(command))

	// Check if it's an ls command
	if !strings.HasPrefix(cmdLower, "ls") && !strings.HasPrefix(cmdLower, "dir") {
		return command
	}

	// Don't modify if already has formatting flags (-1, -l, -C, -x)
	if strings.Contains(command, " -1") || strings.Contains(command, " -l") ||
		strings.Contains(command, " -C") || strings.Contains(command, " -x") {
		return command
	}

	// Add -C flag after the command name
	parts := strings.SplitN(command, " ", 2)
	if len(parts) == 1 {
		// Just "ls" with no args
		return command + " -C"
	}
	// "ls [args]" - insert -C before other args
	return parts[0] + " -C " + parts[1]
}

// formatCommandOutputForDisplay formats command output for better readability.
// For list-like outputs (e.g., ls), formats into two aligned columns with truncation.
func formatCommandOutputForDisplay(command, output string) string {
	// Only format if it's a list command (ls, dir, etc.) and output looks like a simple list
	cmdLower := strings.ToLower(strings.TrimSpace(command))
	isListCommand := strings.HasPrefix(cmdLower, "ls") ||
		strings.HasPrefix(cmdLower, "dir") ||
		strings.HasPrefix(cmdLower, "ll")

	if !isListCommand {
		return output
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Need at least 4 items to make two columns worthwhile
	if len(lines) < 4 {
		return output
	}

	// Check if lines are simple (no tabs, not overly complex formatting)
	for _, line := range lines {
		// Skip already formatted output (with multiple spaces, tabs)
		if strings.Contains(line, "\t") || strings.Contains(line, "  ") {
			return output
		}
	}

	// Maximum column width to keep output readable
	const maxColumnWidth = 45

	// Format into two columns
	var sb strings.Builder

	// Calculate max length of LEFT column items only (even indices)
	maxLen := 0
	for i := 0; i < len(lines); i += 2 {
		itemLen := len(lines[i])
		if itemLen > maxColumnWidth {
			itemLen = maxColumnWidth // Will be truncated
		}
		if itemLen > maxLen {
			maxLen = itemLen
		}
	}

	// Add padding for column alignment
	colWidth := maxLen + 4

	for i := 0; i < len(lines); i += 2 {
		left := truncateFilename(lines[i], maxColumnWidth)
		if i+1 < len(lines) {
			right := truncateFilename(lines[i+1], maxColumnWidth)
			sb.WriteString(fmt.Sprintf("%-*s  %s\n", colWidth, left, right))
		} else {
			// Odd number of items, last one goes on its own line
			sb.WriteString(left + "\n")
		}
	}

	return sb.String()
}

// truncateFilename truncates a filename to maxLen characters, appending ".." if truncated.
// Note: This is optimized for chat-based copilot interface, not terminal emulation.
// Real shells display full filenames with dynamic column widths - we use fixed widths
// with truncation for better readability in chat output.
func truncateFilename(filename string, maxLen int) string {
	if len(filename) <= maxLen {
		return filename
	}
	// Always return at least ".." for any truncation
	if maxLen <= 2 {
		return ".."
	}
	return filename[:maxLen-2] + ".."
}
