package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// launchInTerminal opens a system terminal window and executes the command.
// The terminal remains visible so users can see output and provide input (passwords, etc).
func launchInTerminal(command, workingDir string) error {
	switch runtime.GOOS {
	case "darwin":
		return launchMacTerminal(command, workingDir)
	case "linux":
		return launchLinuxTerminal(command, workingDir)
	case "windows":
		return launchWindowsTerminal(command, workingDir)
	default:
		return fmt.Errorf("terminal launch not supported on %s", runtime.GOOS)
	}
}

// launchMacTerminal uses AppleScript to open Terminal.app
func launchMacTerminal(command, workingDir string) error {
	if workingDir == "" {
		workingDir = "."
	}

	// Escape single quotes in command for AppleScript
	escapedCmd := strings.ReplaceAll(command, `\`, `\\`)
	escapedCmd = strings.ReplaceAll(escapedCmd, `"`, `\"`)

	// Build AppleScript that changes directory and runs command
	script := fmt.Sprintf(`
		tell application "Terminal"
			activate
			do script "cd '%s' && %s; echo ''; echo 'Press any key to close...'; read -n 1"
		end tell
	`, workingDir, escapedCmd)

	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// launchLinuxTerminal tries common terminal emulators
func launchLinuxTerminal(command, workingDir string) error {
	if workingDir == "" {
		workingDir = "."
	}

	// Try terminal emulators in order of preference
	terminals := []struct {
		name string
		args []string
	}{
		{"gnome-terminal", []string{"--", "bash", "-c"}},
		{"konsole", []string{"-e", "bash", "-c"}},
		{"xfce4-terminal", []string{"-e", "bash", "-c"}},
		{"xterm", []string{"-e", "bash", "-c"}},
		{"x-terminal-emulator", []string{"-e", "bash", "-c"}},
	}

	// Build command string with directory change
	fullCommand := fmt.Sprintf("cd '%s' && %s; echo ''; echo 'Press any key to close...'; read -n 1", workingDir, command)

	var lastErr error
	for _, term := range terminals {
		// Check if terminal exists
		if _, err := exec.LookPath(term.name); err != nil {
			continue
		}

		// Try to launch
		args := append(term.args, fullCommand)
		cmd := exec.Command(term.name, args...)
		if err := cmd.Start(); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("failed to launch terminal: %w", lastErr)
	}
	return fmt.Errorf("no suitable terminal emulator found")
}

// launchWindowsTerminal tries Windows Terminal, then falls back to cmd.exe
func launchWindowsTerminal(command, workingDir string) error {
	if workingDir == "" {
		workingDir = "."
	}

	// Build command string
	fullCommand := fmt.Sprintf(`cd /d "%s" && %s && echo. && echo Press any key to close... && pause > nul`, workingDir, command)

	// Try Windows Terminal first (modern)
	if _, err := exec.LookPath("wt.exe"); err == nil {
		cmd := exec.Command("wt.exe", "new-tab", "cmd", "/k", fullCommand)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}

	// Fall back to cmd.exe
	cmd := exec.Command("cmd", "/c", "start", "cmd", "/k", fullCommand)
	return cmd.Start()
}

// shouldUseTerminal detects if a command needs terminal interaction
func shouldUseTerminal(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))

	// Interactive commands that need terminal
	interactivePatterns := []string{
		"sudo",             // Needs password
		"su ",              // Switch user
		"ssh",              // Remote shell
		"scp",              // May need password
		"vim",              // Text editor
		"vi ",              // Text editor
		"nano",             // Text editor
		"emacs",            // Text editor
		"less",             // Pager
		"more",             // Pager
		"top",              // Process monitor
		"htop",             // Process monitor
		"tail -f",          // Follow logs
		"watch",            // Continuous monitoring
		"docker exec -it",  // Interactive container
		"kubectl exec -it", // Interactive pod
		"python -i",        // Interactive Python
		"node ",            // REPL
		"irb",              // Ruby REPL
		"psql",             // PostgreSQL shell
		"mysql",            // MySQL shell
		"redis-cli",        // Redis CLI
	}

	for _, pattern := range interactivePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// getTerminalSuggestion returns a user-friendly explanation for why terminal is suggested
func getTerminalSuggestion(command string) string {
	lower := strings.ToLower(command)

	if strings.Contains(lower, "sudo") || strings.Contains(lower, "su ") {
		return "This command requires password input"
	}
	if strings.Contains(lower, "ssh") || strings.Contains(lower, "scp") {
		return "SSH commands may require password or interaction"
	}
	if strings.Contains(lower, "vim") || strings.Contains(lower, "nano") || strings.Contains(lower, "emacs") {
		return "Text editors require interactive terminal"
	}
	if strings.Contains(lower, "less") || strings.Contains(lower, "more") {
		return "Pagers require interactive terminal"
	}
	if strings.Contains(lower, "top") || strings.Contains(lower, "htop") || strings.Contains(lower, "watch") {
		return "Monitoring tools work best in terminal window"
	}
	if strings.Contains(lower, "tail -f") {
		return "Log following is easier to view in terminal"
	}

	return "This command may require interaction"
}
