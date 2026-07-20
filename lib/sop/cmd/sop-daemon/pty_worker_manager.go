package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// workerProcess represents a running PTY worker process
type workerProcess struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	sessionID string
	pid       int
	mu        sync.Mutex
	dead      bool
	lastUsed  time.Time
}

// workerManager manages the lifecycle of PTY worker processes
type workerManager struct {
	workers map[string]*workerProcess
	mu      sync.Mutex
}

// newWorkerManager creates a new worker manager
func newWorkerManager() *workerManager {
	return &workerManager{
		workers: make(map[string]*workerProcess),
	}
}

// spawnWorker spawns a new PTY worker process for the given sessionID
func (m *workerManager) spawnWorker(sessionID string) (*workerProcess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if worker already exists
	if existing, ok := m.workers[sessionID]; ok && !existing.dead {
		existing.lastUsed = time.Now()
		return existing, nil
	}

	// Spawn new worker process (same binary with --worker-mode flag)
	cmd := exec.Command(os.Args[0], "--worker-mode")
	cmd.Env = os.Environ()

	// Setup pipes for communication
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the worker process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("failed to start worker process: %w", err)
	}

	worker := &workerProcess{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		sessionID: sessionID,
		pid:       cmd.Process.Pid,
		dead:      false,
		lastUsed:  time.Now(),
	}

	m.workers[sessionID] = worker

	// Start monitoring the worker in a goroutine
	go m.monitorWorker(worker)

	// Start logging stderr in background
	go m.logWorkerStderr(worker)

	if shouldLogVerbose() {
		log.Printf("[Worker Manager] Spawned worker process PID=%d for session=%s", worker.pid, sessionID)
	}

	return worker, nil
}

// monitorWorker monitors a worker process and cleans up when it exits
func (m *workerManager) monitorWorker(worker *workerProcess) {
	err := worker.cmd.Wait()

	worker.mu.Lock()
	worker.dead = true
	exitCode := -1
	if worker.cmd.ProcessState != nil {
		exitCode = worker.cmd.ProcessState.ExitCode()
	}
	worker.mu.Unlock()

	m.mu.Lock()
	delete(m.workers, worker.sessionID)
	m.mu.Unlock()

	if shouldLogVerbose() {
		if err != nil {
			log.Printf("[Worker Manager] Worker PID=%d session=%s died with error (exit=%d): %v",
				worker.pid, worker.sessionID, exitCode, err)
		} else {
			log.Printf("[Worker Manager] Worker PID=%d session=%s exited cleanly (exit=%d)",
				worker.pid, worker.sessionID, exitCode)
		}
	}
}

// logWorkerStderr logs stderr output from the worker process
func (m *workerManager) logWorkerStderr(worker *workerProcess) {
	scanner := bufio.NewScanner(worker.stderr)
	for scanner.Scan() {
		if shouldLogVerbose() {
			log.Printf("[Worker PID=%d stderr] %s", worker.pid, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Printf("[Worker Manager] Error reading stderr from PID=%d: %v", worker.pid, err)
	}
}

// sendCommand sends a command to a worker process and returns the response
func (m *workerManager) sendCommand(ctx context.Context, sessionID, command string, timeoutSeconds int) (*workerResponse, error) {
	return m.sendCommandWithStreaming(ctx, sessionID, command, timeoutSeconds, false, nil)
}

// sendCommandWithStreaming sends a command with optional NDJSON streaming
func (m *workerManager) sendCommandWithStreaming(ctx context.Context, sessionID, command string, timeoutSeconds int, streaming bool, chunkCallback func(string)) (*workerResponse, error) {
	// Get or spawn worker
	worker, err := m.spawnWorker(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get worker: %w", err)
	}

	worker.mu.Lock()
	if worker.dead {
		worker.mu.Unlock()
		// Worker is dead, try to spawn a new one
		return m.sendCommandWithStreaming(ctx, sessionID, command, timeoutSeconds, streaming, chunkCallback)
	}
	worker.lastUsed = time.Now()
	worker.mu.Unlock()

	// Prepare request
	req := workerRequest{
		Command:        command,
		TimeoutSeconds: timeoutSeconds,
		Streaming:      streaming,
	}

	// Send request to worker via stdin
	encoder := json.NewEncoder(worker.stdin)
	if err := encoder.Encode(req); err != nil {
		worker.mu.Lock()
		worker.dead = true
		worker.mu.Unlock()
		return nil, fmt.Errorf("failed to send command to worker: %w", err)
	}

	// Read response(s) from worker stdout
	decoder := json.NewDecoder(worker.stdout)
	var finalResp workerResponse
	var fullOutput strings.Builder

	for {
		var resp workerResponse
		if err := decoder.Decode(&resp); err != nil {
			worker.mu.Lock()
			worker.dead = true
			worker.mu.Unlock()
			return nil, fmt.Errorf("failed to read response from worker: %w", err)
		}

		// Handle chunk
		if resp.Chunk != "" {
			fullOutput.WriteString(resp.Chunk)
			if chunkCallback != nil {
				chunkCallback(resp.Chunk)
			}
		}

		// Check if this is the final response
		if !streaming || resp.Complete {
			finalResp = resp
			// Include accumulated output in non-streaming mode
			if !streaming && resp.Output != "" {
				fullOutput.WriteString(resp.Output)
			}
			break
		}
	}

	// Return final response with accumulated output
	finalResp.Output = fullOutput.String()
	return &finalResp, nil
}

// killWorker kills a worker process for the given sessionID
func (m *workerManager) killWorker(sessionID string) error {
	m.mu.Lock()
	worker, ok := m.workers[sessionID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("worker not found for session: %s", sessionID)
	}
	delete(m.workers, sessionID)
	m.mu.Unlock()

	worker.mu.Lock()
	defer worker.mu.Unlock()

	if worker.dead {
		return nil
	}

	worker.dead = true

	// Try graceful shutdown first (close stdin)
	if worker.stdin != nil {
		if err := worker.stdin.Close(); err != nil {
			if shouldLogVerbose() {
				log.Printf("[Worker Manager] Failed to close stdin for PID=%d: %v", worker.pid, err)
			}
		}
	}

	// Check if cmd is nil before waiting
	if worker.cmd == nil {
		return nil
	}

	// Wait a bit for graceful exit
	done := make(chan struct{})
	go func() {
		worker.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		if shouldLogVerbose() {
			log.Printf("[Worker Manager] Worker PID=%d exited gracefully", worker.pid)
		}
		return nil
	case <-time.After(2 * time.Second):
		// Force kill if not exited
		if shouldLogVerbose() {
			log.Printf("[Worker Manager] Force killing worker PID=%d", worker.pid)
		}
		if worker.cmd.Process != nil {
			if err := worker.cmd.Process.Kill(); err != nil {
				return fmt.Errorf("failed to kill worker: %w", err)
			}
		}
		return nil
	}
}

// cleanup kills all active workers (called on daemon shutdown)
func (m *workerManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if shouldLogVerbose() {
		log.Printf("[Worker Manager] Cleaning up %d active workers", len(m.workers))
	}

	for sessionID, worker := range m.workers {
		worker.mu.Lock()
		if !worker.dead {
			worker.dead = true
			// Close stdin only if it's not nil
			if worker.stdin != nil {
				if err := worker.stdin.Close(); err != nil {
					if shouldLogVerbose() {
						log.Printf("[Worker Manager] Failed to close stdin for session=%s: %v", sessionID, err)
					}
				}
			}
			// Force kill after short wait
			go func(w *workerProcess) {
				time.Sleep(1 * time.Second)
				if w.cmd != nil && w.cmd.Process != nil {
					w.cmd.Process.Kill()
				}
			}(worker)
		}
		worker.mu.Unlock()
	}

	m.workers = make(map[string]*workerProcess)
}

// getWorkerCount returns the number of active workers
func (m *workerManager) getWorkerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.workers)
}

var globalWorkerManager *workerManager
var workerManagerOnce sync.Once

// getGlobalWorkerManager returns the global worker manager instance
func getGlobalWorkerManager() *workerManager {
	workerManagerOnce.Do(func() {
		globalWorkerManager = newWorkerManager()
	})
	return globalWorkerManager
}
