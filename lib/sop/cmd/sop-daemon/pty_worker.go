package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/creack/pty"
)

// PTYWorker is a standalone process that manages ONE PTY shell session
// Crashes in this process do NOT crash the parent daemon
// Communication happens via stdin/stdout using JSON protocol

type workerRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type workerResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
	Running  bool   `json:"running"` // true if command still executing
}

var ansiEscapeWorker = regexp.MustCompile(`\x1b\[[?]?[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[^\[]*|\r`)

func stripANSIWorker(s string) string {
	return ansiEscapeWorker.ReplaceAllString(s, "")
}

// removeCommandEcho removes the command echo from PTY output
// PTY terminals echo the command before showing output, even with stty -echo
// This happens because stty -echo hasn't taken effect for the first command
func removeCommandEcho(output, command string) string {
	lines := strings.Split(output, "\n")
	var result []string

	// Build exact pattern to match
	cmdTrimmed := strings.TrimSpace(command)

	for _, line := range lines {
		lineTrimmed := strings.TrimSpace(line)

		// Skip if this line is EXACTLY the command we sent
		if lineTrimmed == cmdTrimmed {
			continue
		}
		// Skip lines starting with "stty" (the echo disable command)
		if strings.HasPrefix(lineTrimmed, "stty") {
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// runPTYWorker is the main entry point when process is spawned as PTY worker
// Uses os.Stdout for JSON output (production mode)
func runPTYWorker() error {
	return runPTYWorkerWithOutput(os.Stdout)
}

// runPTYWorkerWithOutput is the worker implementation that accepts a specific output file
// This allows test mode to isolate JSON output from test framework pollution
func runPTYWorkerWithOutput(jsonOut *os.File) error {
	// CRITICAL: Redirect ALL non-JSON output away from stdout to prevent pollution
	// The test framework and other code may try to write to stdout, but we ONLY
	// want clean JSON on stdout for the parent daemon to parse
	// In test mode, os.Stdout is already redirected to stderr by TestMain,
	// so this is mainly for production mode
	if jsonOut == os.Stdout {
		// Production mode: redirect the global stdout to stderr
		// Save the real stdout for JSON protocol
		realStdout := os.Stdout
		os.Stdout = os.Stderr
		jsonOut = realStdout
	}

	// Initialize PTY shell - use bash for better terminal control
	shell := "/bin/bash"
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"PS1=<<<PROMPT_READY>>>"+string(rune(10)),
		"PS2=",
		"ENV=",
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("PTY worker failed to start shell: %w", err)
	}
	defer ptmx.Close()

	reader := bufio.NewReader(ptmx)

	// Wait for shell to initialize
	time.Sleep(100 * time.Millisecond)

	// Explicitly set prompt (ensures it's set correctly)
	io.WriteString(ptmx, "PS1='<<<PROMPT_READY>>>\n'\n")
	time.Sleep(50 * time.Millisecond)

	// Disable echo
	io.WriteString(ptmx, "stty -echo\n")
	time.Sleep(50 * time.Millisecond)

	// Drain initialization output using drainUntilPromptWorker
	// This will read until it sees our prompt marker
	drainUntilPromptWorker(reader)

	// Process commands from stdin, send responses to jsonOut (clean JSON only)
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(jsonOut)

	for scanner.Scan() {
		var req workerRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			// Send error response
			encoder.Encode(workerResponse{
				Error:    fmt.Sprintf("invalid request: %v", err),
				ExitCode: 1,
			})
			continue
		}

		// Execute command
		resp := executeCommandWorker(ptmx, reader, req)
		if err := encoder.Encode(resp); err != nil {
			// Cannot send response - worker is broken
			return fmt.Errorf("failed to send response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdin scanner error: %w", err)
	}

	return nil
}

func executeCommandWorker(ptmx *os.File, reader *bufio.Reader, req workerRequest) workerResponse {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return workerResponse{Output: "", ExitCode: 0}
	}

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 600 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// CRITICAL: Discard any leftover prompt from previous command
	// This prevents reading stale prompts instead of waiting for actual output
	const promptMarker = "<<<PROMPT_READY>>>"
	for reader.Buffered() > 0 {
		// Peek at buffered data
		peek, _ := reader.Peek(reader.Buffered())
		peekStr := string(peek)

		// If there's a prompt in the buffer, read and discard up to and including it
		if strings.Contains(peekStr, promptMarker) {
			// Read until we consume the prompt line
			for {
				line, err := reader.ReadString('\n')
				if err != nil || strings.Contains(line, promptMarker) {
					break
				}
			}
		} else {
			// No prompt in buffer, safe to proceed
			break
		}
	}

	// Send command
	if _, err := io.WriteString(ptmx, command+"\n"); err != nil {
		return workerResponse{
			Error:    fmt.Sprintf("failed to write command: %v", err),
			ExitCode: 1,
		}
	}

	// Small delay to allow command to start executing and produce output
	// This prevents reading a leftover prompt before the command executes
	// Use longer delay to be conservative about PTY timing
	time.Sleep(20 * time.Millisecond)

	// Read output until prompt or timeout
	var output strings.Builder
	done := make(chan error, 1)

	go func() {
		done <- readUntilPromptWorker(reader, &output)
	}()

	select {
	case err := <-done:
		if err != nil {
			return workerResponse{
				Output:   output.String(),
				Error:    fmt.Sprintf("read error: %v", err),
				ExitCode: 1,
			}
		}
		cleanOutput := stripANSIWorker(output.String())
		cleanOutput = removeCommandEcho(cleanOutput, command)
		return workerResponse{
			Output:   cleanOutput,
			ExitCode: 0,
		}

	case <-ctx.Done():
		// Command still running
		cleanOutput := stripANSIWorker(output.String())
		cleanOutput = removeCommandEcho(cleanOutput, command)
		return workerResponse{
			Output:  cleanOutput,
			Running: true,
		}
	}
}

func drainUntilPromptWorker(reader *bufio.Reader) error {
	var discarded strings.Builder
	return readUntilPromptWorker(reader, &discarded)
}

func readUntilPromptWorker(reader *bufio.Reader, output *strings.Builder) error {
	const promptMarker = "<<<PROMPT_READY>>>"
	var buffer strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("shell terminated unexpectedly")
			}
			return fmt.Errorf("read error: %w", err)
		}

		buffer.WriteString(line)

		// Check if we hit the prompt
		if strings.Contains(buffer.String(), promptMarker) {
			bufferStr := buffer.String()
			beforePrompt := strings.Split(bufferStr, promptMarker)[0]
			output.WriteString(beforePrompt)
			return nil
		}

		// Periodically flush accumulated lines to output
		if buffer.Len() > 4096 {
			output.WriteString(buffer.String())
			buffer.Reset()
		}
	}
}
