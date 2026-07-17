package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	// commandExecutor is injected to execute local commands
	commandExecutor func(context.Context, map[string]any) (string, error)
}

func NewCapability() *Capability { return &Capability{} }

// SetCommandExecutor injects the command execution function from the agent
func (c *Capability) SetCommandExecutor(executor func(context.Context, map[string]any) (string, error)) {
	c.commandExecutor = executor
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{EnabledByDefault: true, Tier: "base"}
}
func (c *Capability) CanHandle(query string, state capabilities.RoutingState) bool {
	if state.Domain == "" {
		return false
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "desktop_automation" || hint == "local_ops" {
			return true
		}
		if hint == "coding_workflow" || hint == "repo_edit" {
			return false
		}
	}
	return state.Domain == "Desktop" || state.Domain == "Desktop Automation" || state.Domain == CapabilityID || state.Domain == CapabilityName
}
func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Desktop Automation\n- Use local automation tools for shell, git, file, and workspace tasks when the request involves repository work, local file editing, or developer workflow operations."
}
func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{Name: "execute_local_command", Description: "Executes a shell command on the user's local machine after approval.", Schema: "{}"}}
}
func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return ""
}
func (c *Capability) Run(query string, state capabilities.RoutingState) (capabilities.CapabilityResult, error) {
	return capabilities.CapabilityResult{Summary: "desktop automation capability placeholder"}, nil
}

// GetHandledPrefixes returns the command prefixes this capability handles
func (c *Capability) GetHandledPrefixes() []string {
	return []string{"/c", "/cmd", "/shell", "/exec"}
}

// HandlePrefix executes a command that starts with a registered prefix
func (c *Capability) HandlePrefix(ctx context.Context, query string, normalizedCommand string) (string, error) {
	if c.commandExecutor == nil {
		return "", fmt.Errorf("desktop automation command executor not available")
	}

	if normalizedCommand == "" {
		return "", fmt.Errorf("empty command: provide a command to execute")
	}

	// Validate command is not just whitespace
	if len(strings.TrimSpace(normalizedCommand)) == 0 {
		return "", fmt.Errorf("command contains only whitespace")
	}

	// Normalize ls commands to use multi-column output
	normalizedCommand = normalizeLsCommand(normalizedCommand)

	// Execute the command via the injected executor
	result, err := c.commandExecutor(ctx, map[string]any{
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
