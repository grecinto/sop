package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[?]?[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[^\[]*|\r`)

// stripANSI removes ANSI escape sequences and carriage returns from output
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// StreamCallback is called with output chunks as they arrive
type StreamCallback func(chunk string)

// ptyShellSession represents a persistent shell session using PTY
type ptyShellSession struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	pty           *os.File
	reader        *bufio.Reader
	sessionID     string
	shell         string
	lastOutput    string
	promptReady   chan struct{}
	closed        bool
	stillRunning  bool            // tracks if previous command is still running
	lastUsed      time.Time       // tracks last activity for idle cleanup
	isBackground  bool            // marks background/detached sessions
	outputHistory strings.Builder // accumulated output for terminal display
	historyLines  int             // track line count for size management
}

// newPTYShellSession creates a new persistent shell session with PTY
func newPTYShellSession(sessionID string) (*ptyShellSession, error) {
	// Use /bin/sh for simplicity and portability instead of user's shell
	// Most shells are sh-compatible and sh has simpler initialization
	shell := "/bin/sh"

	// Start shell - interactive
	cmd := exec.Command(shell, "-i")
	cmd.Env = append(os.Environ(),
		"PS1=<<<PROMPT_READY>>>"+string(rune(10)), // Simple prompt with newline
		"PS2=",
		"ENV=", // Disable sourcing of .profile or similar
	)

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start shell with PTY: %w", err)
	}

	session := &ptyShellSession{
		cmd:         cmd,
		pty:         ptmx,
		reader:      bufio.NewReader(ptmx),
		sessionID:   sessionID,
		shell:       shell,
		promptReady: make(chan struct{}, 1),
		lastUsed:    time.Now(),
	}

	// Disable echo
	if _, err := io.WriteString(ptmx, "stty -echo\n"); err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("failed to disable echo: %w", err)
	}

	// Drain all initialization output
	time.Sleep(200 * time.Millisecond)
	_, _ = session.drainUntilPrompt(1 * time.Second)

	return session, nil
}

// drainUntilPrompt reads output until the prompt marker appears or timeout
// Returns the output and whether the prompt was found
func (s *ptyShellSession) drainUntilPrompt(timeout time.Duration) (string, bool) {
	return s.drainUntilPromptWithCallback(timeout, nil)
}

// drainUntilPromptWithCallback reads output with optional streaming callback
func (s *ptyShellSession) drainUntilPromptWithCallback(timeout time.Duration, callback StreamCallback) (string, bool) {
	var output bytes.Buffer
	deadline := time.Now().Add(timeout)
	foundPrompt := false

	for time.Now().Before(deadline) {
		s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 4096)
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])

			// Stream raw chunk to callback before processing
			if callback != nil {
				// Strip ANSI for clean streaming output
				cleanChunk := stripANSI(chunk)
				if cleanChunk != "" {
					callback(cleanChunk)
				}
			}

			// Accumulate output for return value
			output.WriteString(chunk)

			// Check if we hit the prompt marker
			if strings.Contains(chunk, "<<<PROMPT_READY>>>") {
				foundPrompt = true
				result := output.String()
				// Remove the prompt marker and any trailing output after it
				if idx := strings.Index(result, "<<<PROMPT_READY>>>"); idx >= 0 {
					result = result[:idx]
				}
				// Strip ANSI codes and clean up
				result = stripANSI(result)
				result = strings.TrimSpace(result)
				return result, true
			}
		}
		if err != nil && err != io.EOF {
			break
		}
	}

	// Timeout reached - return what we have
	result := stripANSI(output.String())
	return strings.TrimSpace(result), foundPrompt
}

// Run executes a command in the persistent shell
func (s *ptyShellSession) Run(command string, timeout time.Duration) (string, bool, error) {
	return s.RunWithStreaming(command, timeout, nil)
}

// RunWithStreaming executes a command with optional real-time output streaming
func (s *ptyShellSession) RunWithStreaming(command string, timeout time.Duration, streamCallback StreamCallback) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update last used timestamp
	s.lastUsed = time.Now()

	if s.closed {
		return "", false, fmt.Errorf("shell session is closed")
	}

	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return "", false, nil
	}

	// Handle special status command
	if strings.EqualFold(trimmed, "status") {
		// Return buffered output and check if we can read more
		if s.stillRunning && streamCallback != nil {
			// Try to read any new output without blocking
			output, foundPrompt := s.drainUntilPromptWithCallback(100*time.Millisecond, streamCallback)
			if output != "" {
				s.lastOutput = output
			}
			s.stillRunning = !foundPrompt
		}
		return s.lastOutput, s.stillRunning, nil
	}

	// Handle special stop command
	if strings.EqualFold(trimmed, "stop") {
		if s.stillRunning {
			// Send Ctrl+C to interrupt
			if _, err := s.pty.Write([]byte{3}); err != nil {
				return "", false, fmt.Errorf("failed to send interrupt: %w", err)
			}
			time.Sleep(50 * time.Millisecond)
			// Drain output
			output, _ := s.drainUntilPromptWithCallback(500*time.Millisecond, streamCallback)
			s.lastOutput = output
			s.stillRunning = false
			return s.lastOutput, false, nil
		}
		return "no command running", false, nil
	}

	// Regular command execution
	// Write command to PTY
	if _, err := io.WriteString(s.pty, trimmed+"\n"); err != nil {
		return "", false, fmt.Errorf("failed to write command: %w", err)
	}

	// Read output until prompt, streaming chunks to callback
	output, foundPrompt := s.drainUntilPromptWithCallback(timeout, streamCallback)
	s.lastOutput = output
	s.stillRunning = !foundPrompt

	// Accumulate output in history buffer for terminal display (limit growth)
	if output != "" {
		// Prefix with command for context
		s.outputHistory.WriteString("$ ")
		s.outputHistory.WriteString(trimmed)
		s.outputHistory.WriteString("\n")
		s.outputHistory.WriteString(output)
		if !strings.HasSuffix(output, "\n") {
			s.outputHistory.WriteString("\n")
		}
		s.historyLines += strings.Count(trimmed, "\n") + 1 + strings.Count(output, "\n")

		// Keep last 1000 lines to prevent memory issues
		if s.historyLines > 1000 {
			history := s.outputHistory.String()
			lines := strings.Split(history, "\n")
			if len(lines) > 500 {
				keptLines := lines[len(lines)-500:]
				s.outputHistory.Reset()
				s.outputHistory.WriteString(strings.Join(keptLines, "\n"))
				s.historyLines = len(keptLines)
			}
		}
	}

	// If we found the prompt, command completed
	// If we didn't find prompt, command is still running
	return output, s.stillRunning, nil
}

// GetWorkingDirectory returns the current working directory of the shell
func (s *ptyShellSession) GetWorkingDirectory() (string, error) {
	output, _, err := s.Run("pwd", 2*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// GetOutputHistory returns the accumulated terminal output
func (s *ptyShellSession) GetOutputHistory() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outputHistory.String()
}

// ClearOutputHistory clears the accumulated terminal output
func (s *ptyShellSession) ClearOutputHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outputHistory.Reset()
	s.historyLines = 0
}

// SendInterrupt sends Ctrl+C to stop the currently running command
func (s *ptyShellSession) SendInterrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.pty == nil {
		return fmt.Errorf("session is closed")
	}

	// Send Ctrl+C (ASCII 0x03)
	if _, err := s.pty.Write([]byte{0x03}); err != nil {
		return fmt.Errorf("failed to send interrupt: %w", err)
	}

	// Reset running state
	s.stillRunning = false

	return nil
}

// Close terminates the shell session
func (s *ptyShellSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	// Send exit command
	if s.pty != nil {
		io.WriteString(s.pty, "exit\n")
		time.Sleep(100 * time.Millisecond)
		s.pty.Close()
	}

	// Kill process if still running
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}

	return nil
}

// IsHealthy checks if the shell session is responsive by sending a simple echo command
func (s *ptyShellSession) IsHealthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false
	}

	// Quick health check - try echo with 2 second timeout
	if _, err := io.WriteString(s.pty, "echo __HEALTH_CHECK__\n"); err != nil {
		return false
	}

	// Look for the health check response
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 4096)
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			if strings.Contains(chunk, "__HEALTH_CHECK__") || strings.Contains(chunk, "<<<PROMPT_READY>>>") {
				return true
			}
		}
		if err != nil && err != io.EOF && err.Error() != "i/o timeout" {
			return false
		}
	}

	return false
}

// ptyShellSessionManager manages multiple PTY shell sessions
type ptyShellSessionManager struct {
	mu             sync.Mutex
	sessions       map[string]*ptyShellSession
	cleanupRunning bool
	cleanupCancel  context.CancelFunc
}

// newPTYShellSessionManager creates a new PTY shell session manager
func newPTYShellSessionManager() *ptyShellSessionManager {
	return &ptyShellSessionManager{
		sessions: make(map[string]*ptyShellSession),
	}
}

// Run executes a command in a persistent PTY shell session
func (m *ptyShellSessionManager) Run(sessionID, command string, timeout time.Duration) (string, bool, error) {
	return m.RunWithStreaming(sessionID, command, timeout, nil)
}

// RunWithStreaming executes a command with optional real-time output streaming
func (m *ptyShellSessionManager) RunWithStreaming(sessionID, command string, timeout time.Duration, streamCallback StreamCallback) (string, bool, error) {
	if sessionID == "" {
		sessionID = "default"
	}

	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if !ok {
		var err error
		session, err = newPTYShellSession(sessionID)
		if err != nil {
			m.mu.Unlock()
			return "", false, fmt.Errorf("failed to create shell session: %w", err)
		}
		m.sessions[sessionID] = session
	} else {
		// Check if existing session is stuck and needs recovery
		if session.stillRunning {
			log.Printf("PTY session %s marked as stuck, checking health...", sessionID)
			if !session.IsHealthy() {
				log.Printf("PTY session %s unresponsive, forcing reset", sessionID)
				// Close stuck session
				session.Close()
				// Create fresh session
				var err error
				session, err = newPTYShellSession(sessionID)
				if err != nil {
					m.mu.Unlock()
					return "", false, fmt.Errorf("failed to recreate shell session after reset: %w", err)
				}
				m.sessions[sessionID] = session
			}
		}
		// Update last used time for existing session
		session.lastUsed = time.Now()
	}
	m.mu.Unlock()

	return session.RunWithStreaming(command, timeout, streamCallback)
}

// SpawnBackgroundSession creates a new background PTY session with a unique ID
// Use this to run long-running commands (servers, watchers) in parallel with main session
func (m *ptyShellSessionManager) SpawnBackgroundSession(workingDir string) (string, error) {
	// Generate unique session ID for background work
	sessionID := fmt.Sprintf("bg-%d-%d", time.Now().Unix(), len(m.sessions))

	m.mu.Lock()
	session, err := newPTYShellSession(sessionID)
	if err != nil {
		m.mu.Unlock()
		return "", fmt.Errorf("failed to create background shell session: %w", err)
	}
	session.isBackground = true
	m.sessions[sessionID] = session
	m.mu.Unlock()

	// Change to working directory if specified
	if workingDir != "" {
		_, _, err := session.Run("cd "+workingDir, 5*time.Second)
		if err != nil {
			session.Close()
			return "", fmt.Errorf("failed to change to working directory: %w", err)
		}
	}

	return sessionID, nil
}

// ForceResetSession forcibly closes and recreates a session
// Use this when a session is stuck and auto-recovery didn't work
func (m *ptyShellSessionManager) ForceResetSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[sessionID]; ok {
		// Close the stuck session
		if err := session.Close(); err != nil {
			log.Printf("Error closing stuck session %s: %v", sessionID, err)
		}
		delete(m.sessions, sessionID)

		// Create fresh session
		newSession, err := newPTYShellSession(sessionID)
		if err != nil {
			return fmt.Errorf("failed to recreate session %s: %w", sessionID, err)
		}
		m.sessions[sessionID] = newSession
		log.Printf("Forcibly reset session %s", sessionID)
		return nil
	}

	return fmt.Errorf("session %s not found", sessionID)
}

// StartCleanup begins background cleanup of idle sessions
func (m *ptyShellSessionManager) StartCleanup(ctx context.Context, interval, idleTimeout time.Duration) {
	m.mu.Lock()
	if m.cleanupRunning {
		m.mu.Unlock()
		return
	}

	cleanupCtx, cancel := context.WithCancel(ctx)
	m.cleanupCancel = cancel
	m.cleanupRunning = true
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				cleaned := m.cleanupIdleSessions(idleTimeout)
				if cleaned > 0 {
					log.Printf("PTY cleanup: removed %d idle sessions", cleaned)
				}
			}
		}
	}()
}

// StopCleanup stops the background cleanup goroutine
func (m *ptyShellSessionManager) StopCleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cleanupCancel != nil {
		m.cleanupCancel()
		m.cleanupCancel = nil
	}
	m.cleanupRunning = false
}

// cleanupIdleSessions removes sessions that haven't been used within idleTimeout
func (m *ptyShellSessionManager) cleanupIdleSessions(idleTimeout time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for sessionID, session := range m.sessions {
		session.mu.Lock()
		idle := now.Sub(session.lastUsed)
		isIdle := idle > idleTimeout
		session.mu.Unlock()

		if isIdle {
			// Close the session
			if err := session.Close(); err != nil {
				log.Printf("PTY cleanup: error closing session %s: %v", sessionID, err)
			}
			delete(m.sessions, sessionID)
			cleaned++
		}
	}

	return cleaned
}

// CloseSession closes a specific shell session
func (m *ptyShellSessionManager) CloseSession(sessionID string) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if session != nil {
		return session.Close()
	}
	return nil
}

// GetSessionOutput retrieves the accumulated output for a session
func (m *ptyShellSessionManager) GetSessionOutput(sessionID string) (string, bool) {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	m.mu.Unlock()

	if !ok {
		return "", false
	}

	return session.GetOutputHistory(), true
}

// GetSessionStatus returns output, exists, and whether a command is currently running
func (m *ptyShellSessionManager) GetSessionStatus(sessionID string) (output string, exists bool, isRunning bool) {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	m.mu.Unlock()

	if !ok {
		return "", false, false
	}

	session.mu.Lock()
	running := session.stillRunning
	session.mu.Unlock()

	return session.GetOutputHistory(), true, running
}

// ClearSessionOutput clears the accumulated output for a session
func (m *ptyShellSessionManager) ClearSessionOutput(sessionID string) bool {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	m.mu.Unlock()

	if !ok {
		return false
	}

	session.ClearOutputHistory()
	return true
}

// SendSessionInterrupt sends Ctrl+C to stop the currently running command in a session
func (m *ptyShellSessionManager) SendSessionInterrupt(sessionID string) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	return session.SendInterrupt()
}

// CloseAll closes all shell sessions
func (m *ptyShellSessionManager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*ptyShellSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*ptyShellSession)
	m.mu.Unlock()

	for _, session := range sessions {
		session.Close()
	}
}
