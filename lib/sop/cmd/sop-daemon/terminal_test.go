package main

import (
	"strings"
	"testing"
)

func TestShouldUseTerminal(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected bool
		reason   string
	}{
		// Interactive commands that should use terminal
		{
			name:     "sudo command",
			command:  "sudo apt update",
			expected: true,
			reason:   "needs password input",
		},
		{
			name:     "su command",
			command:  "su - username",
			expected: true,
			reason:   "switch user needs password",
		},
		{
			name:     "ssh connection",
			command:  "ssh user@server",
			expected: true,
			reason:   "may need password or interaction",
		},
		{
			name:     "scp file transfer",
			command:  "scp file.txt user@server:/path",
			expected: true,
			reason:   "may need password",
		},
		{
			name:     "vim editor",
			command:  "vim config.yaml",
			expected: true,
			reason:   "interactive text editor",
		},
		{
			name:     "vi editor",
			command:  "vi file.txt",
			expected: true,
			reason:   "interactive text editor",
		},
		{
			name:     "nano editor",
			command:  "nano readme.md",
			expected: true,
			reason:   "interactive text editor",
		},
		{
			name:     "emacs editor",
			command:  "emacs script.sh",
			expected: true,
			reason:   "interactive text editor",
		},
		{
			name:     "less pager",
			command:  "less /var/log/syslog",
			expected: true,
			reason:   "interactive pager",
		},
		{
			name:     "more pager",
			command:  "more output.txt",
			expected: true,
			reason:   "interactive pager",
		},
		{
			name:     "top process monitor",
			command:  "top",
			expected: true,
			reason:   "continuous process monitoring",
		},
		{
			name:     "htop process monitor",
			command:  "htop",
			expected: true,
			reason:   "interactive process monitoring",
		},
		{
			name:     "watch command",
			command:  "watch -n 1 df -h",
			expected: true,
			reason:   "continuous monitoring",
		},
		{
			name:     "tail follow",
			command:  "tail -f /var/log/app.log",
			expected: true,
			reason:   "continuous log following",
		},
		{
			name:     "docker interactive exec",
			command:  "docker exec -it container bash",
			expected: true,
			reason:   "interactive container shell",
		},
		{
			name:     "kubectl interactive exec",
			command:  "kubectl exec -it pod -- bash",
			expected: true,
			reason:   "interactive pod shell",
		},
		{
			name:     "python interactive",
			command:  "python -i script.py",
			expected: true,
			reason:   "interactive Python REPL",
		},
		{
			name:     "node REPL",
			command:  "node script.js",
			expected: true,
			reason:   "Node.js REPL",
		},
		{
			name:     "irb Ruby REPL",
			command:  "irb",
			expected: true,
			reason:   "interactive Ruby REPL",
		},
		{
			name:     "psql database shell",
			command:  "psql -U user database",
			expected: true,
			reason:   "interactive database shell",
		},
		{
			name:     "mysql database shell",
			command:  "mysql -u user -p",
			expected: true,
			reason:   "interactive database shell",
		},
		{
			name:     "redis-cli",
			command:  "redis-cli",
			expected: true,
			reason:   "interactive Redis CLI",
		},
		// Non-interactive commands that should use background
		{
			name:     "ls directory listing",
			command:  "ls -la",
			expected: false,
			reason:   "simple output command",
		},
		{
			name:     "pwd current directory",
			command:  "pwd",
			expected: false,
			reason:   "simple output command",
		},
		{
			name:     "cat file content",
			command:  "cat file.txt",
			expected: false,
			reason:   "simple file reading",
		},
		{
			name:     "echo output",
			command:  "echo 'hello world'",
			expected: false,
			reason:   "simple output command",
		},
		{
			name:     "git status",
			command:  "git status",
			expected: false,
			reason:   "simple git query",
		},
		{
			name:     "git log",
			command:  "git log --oneline -10",
			expected: false,
			reason:   "simple git query",
		},
		{
			name:     "tail without follow",
			command:  "tail -n 20 file.log",
			expected: false,
			reason:   "simple file reading (not following)",
		},
		{
			name:     "grep search",
			command:  "grep 'pattern' file.txt",
			expected: false,
			reason:   "simple text search",
		},
		{
			name:     "find files",
			command:  "find . -name '*.go'",
			expected: false,
			reason:   "simple file search",
		},
		{
			name:     "make build",
			command:  "make build",
			expected: false,
			reason:   "build command (non-interactive)",
		},
		// Edge cases
		{
			name:     "command with sudo in middle",
			command:  "echo 'sudo' && ls",
			expected: true,
			reason:   "contains sudo keyword",
		},
		{
			name:     "uppercase command",
			command:  "SUDO apt update",
			expected: true,
			reason:   "case-insensitive detection",
		},
		{
			name:     "command with extra whitespace",
			command:  "  vim   file.txt  ",
			expected: true,
			reason:   "handles whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldUseTerminal(tt.command)
			if got != tt.expected {
				t.Errorf("shouldUseTerminal(%q) = %v, want %v (reason: %s)",
					tt.command, got, tt.expected, tt.reason)
			}
		})
	}
}

func TestGetTerminalSuggestion(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		contains string // Expected substring in suggestion
	}{
		{
			name:     "sudo command",
			command:  "sudo apt update",
			contains: "password input",
		},
		{
			name:     "su command",
			command:  "su - admin",
			contains: "password input",
		},
		{
			name:     "ssh command",
			command:  "ssh user@server",
			contains: "SSH",
		},
		{
			name:     "scp command",
			command:  "scp file.txt server:/path",
			contains: "SSH",
		},
		{
			name:     "vim editor",
			command:  "vim config.yaml",
			contains: "editor",
		},
		{
			name:     "nano editor",
			command:  "nano readme.md",
			contains: "editor",
		},
		{
			name:     "emacs editor",
			command:  "emacs script.sh",
			contains: "editor",
		},
		{
			name:     "less pager",
			command:  "less file.txt",
			contains: "Pager",
		},
		{
			name:     "more pager",
			command:  "more output.log",
			contains: "Pager",
		},
		{
			name:     "top monitor",
			command:  "top",
			contains: "Monitoring",
		},
		{
			name:     "htop monitor",
			command:  "htop",
			contains: "Monitoring",
		},
		{
			name:     "watch monitor",
			command:  "watch df -h",
			contains: "Monitoring",
		},
		{
			name:     "tail follow",
			command:  "tail -f app.log",
			contains: "Log following",
		},
		{
			name:     "generic interactive",
			command:  "psql",
			contains: "interaction",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTerminalSuggestion(tt.command)
			if got == "" {
				t.Errorf("getTerminalSuggestion(%q) returned empty string", tt.command)
			}
			if tt.contains != "" && !strings.Contains(strings.ToLower(got), strings.ToLower(tt.contains)) {
				t.Errorf("getTerminalSuggestion(%q) = %q, want to contain %q",
					tt.command, got, tt.contains)
			}
		})
	}
}
