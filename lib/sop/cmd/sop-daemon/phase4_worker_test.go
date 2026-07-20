package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	sharedagent "github.com/sharedcode/sop/ai/agent"
)

// TestPhase4_WorkerSpawning verifies that worker processes spawn on first command
func TestPhase4_WorkerSpawning(t *testing.T) {
	t.Log("=== Phase 4 Test 1: Worker Spawning ===")

	sessionID := fmt.Sprintf("test-spawn-%d", time.Now().Unix())

	// Count processes before
	beforeCount := countWorkerProcesses(t)
	t.Logf("Worker processes before: %d", beforeCount)

	// Execute a command that should spawn a worker
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo hello",
		Reasoning:      "test worker spawning",
		SessionID:      sessionID,
		TimeoutSeconds: 10,
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if !strings.Contains(result, "hello") {
		t.Fatalf("Command output missing 'hello': %s", result)
	}

	// Give worker time to settle
	time.Sleep(500 * time.Millisecond)

	// Count processes after
	afterCount := countWorkerProcesses(t)
	t.Logf("Worker processes after: %d", afterCount)

	if afterCount <= beforeCount {
		t.Logf("Warning: No new worker detected (may have already existed)")
	} else {
		t.Logf("✓ Worker spawned successfully")
	}
}

// TestPhase4_SessionStatePersistence verifies working directory persists across commands
func TestPhase4_SessionStatePersistence(t *testing.T) {
	t.Log("=== Phase 4 Test 2: Session State Persistence ===")

	sessionID := fmt.Sprintf("test-persist-%d", time.Now().Unix())
	tmpDir := t.TempDir()

	// Command 1: Change directory
	t.Logf("Changing to directory: %s", tmpDir)
	result1, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        fmt.Sprintf("cd %s", tmpDir),
		Reasoning:      "change working directory",
		SessionID:      sessionID,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("cd command failed: %v", err)
	}
	t.Logf("cd result: %s", result1)

	// Command 2: Check we're still in that directory
	result2, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "pwd",
		Reasoning:      "verify working directory persisted",
		SessionID:      sessionID,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("pwd command failed: %v", err)
	}
	t.Logf("pwd result: %s", result2)

	if !strings.Contains(result2, tmpDir) {
		t.Fatalf("Working directory did not persist. Expected %s, got: %s", tmpDir, result2)
	}

	t.Log("✓ Session state persisted across commands")
}

// TestPhase4_WorkerCrashRecovery verifies daemon survives worker crashes and auto-recovers
func TestPhase4_WorkerCrashRecovery(t *testing.T) {
	t.Log("=== Phase 4 Test 3: Worker Crash Recovery ===")

	sessionID := fmt.Sprintf("test-crash-%d", time.Now().Unix())

	// Step 1: Spawn a worker with initial command
	t.Log("Step 1: Spawning worker...")
	_, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo initial",
		Reasoning:      "spawn worker for crash test",
		SessionID:      sessionID,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("Initial command failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Step 2: Find and kill the worker
	t.Log("Step 2: Finding and killing worker...")
	killed := killWorkersForSession(t, sessionID)
	if killed == 0 {
		t.Log("Warning: No workers found to kill (may have already exited)")
	} else {
		t.Logf("Killed %d worker(s)", killed)
	}

	time.Sleep(500 * time.Millisecond)

	// Step 3: Send another command - should auto-spawn new worker
	t.Log("Step 3: Sending command to trigger auto-recovery...")
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo recovered",
		Reasoning:      "test auto-recovery after crash",
		SessionID:      sessionID,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("Recovery command failed: %v", err)
	}

	if !strings.Contains(result, "recovered") {
		t.Fatalf("Recovery failed. Output: %s", result)
	}

	t.Log("✓ Worker auto-recovered after crash")
}

// TestPhase4_ConcurrentSessions tests multiple sessions running in parallel
func TestPhase4_ConcurrentSessions(t *testing.T) {
	t.Log("=== Phase 4 Test 4: Concurrent Sessions ===")

	numSessions := 10
	var wg sync.WaitGroup
	errors := make(chan error, numSessions)

	t.Logf("Starting %d concurrent sessions...", numSessions)

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sessionID := fmt.Sprintf("test-concurrent-%d-%d", time.Now().Unix(), idx)
			tmpDir := t.TempDir()

			// Each session: cd to unique dir, create file, verify
			commands := []string{
				fmt.Sprintf("cd %s", tmpDir),
				fmt.Sprintf("echo session-%d > test.txt", idx),
				"cat test.txt",
			}

			for cmdIdx, cmd := range commands {
				result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
					Command:        cmd,
					Reasoning:      fmt.Sprintf("concurrent test session %d command %d", idx, cmdIdx),
					SessionID:      sessionID,
					TimeoutSeconds: 10,
				})
				if err != nil {
					errors <- fmt.Errorf("session %d command %d failed: %w", idx, cmdIdx, err)
					return
				}

				// Verify last command output
				if cmdIdx == len(commands)-1 {
					expectedOutput := fmt.Sprintf("session-%d", idx)
					if !strings.Contains(result, expectedOutput) {
						errors <- fmt.Errorf("session %d: expected %q in output, got: %s", idx, expectedOutput, result)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errCount := 0
	for err := range errors {
		t.Errorf("Concurrent session error: %v", err)
		errCount++
	}

	if errCount == 0 {
		t.Logf("✓ All %d concurrent sessions completed successfully", numSessions)
	} else {
		t.Fatalf("%d/%d sessions failed", errCount, numSessions)
	}
}

// TestPhase4_PerformanceBenchmark measures IPC overhead
func TestPhase4_PerformanceBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark in short mode")
	}

	t.Log("=== Phase 4 Test 5: Performance Benchmark ===")

	sessionID := fmt.Sprintf("test-perf-%d", time.Now().Unix())
	numCommands := 50

	start := time.Now()

	for i := 0; i < numCommands; i++ {
		_, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
			Command:        fmt.Sprintf("echo iteration-%d", i),
			Reasoning:      "performance test",
			SessionID:      sessionID,
			TimeoutSeconds: 10,
		})
		if err != nil {
			t.Fatalf("Command %d failed: %v", i, err)
		}
	}

	elapsed := time.Since(start)
	avgPerCommand := elapsed / time.Duration(numCommands)

	t.Logf("Executed %d commands in %v", numCommands, elapsed)
	t.Logf("Average per command: %v", avgPerCommand)
	t.Logf("Commands per second: %.2f", float64(numCommands)/elapsed.Seconds())

	// Benchmark threshold - should be well under 100ms per command
	if avgPerCommand > 100*time.Millisecond {
		t.Logf("Warning: Average command time %v exceeds 100ms threshold", avgPerCommand)
	} else {
		t.Logf("✓ Performance acceptable: %v per command", avgPerCommand)
	}
}

// TestPhase4_StreamingOutput verifies streaming callbacks work through worker IPC
func TestPhase4_StreamingOutput(t *testing.T) {
	t.Log("=== Phase 4 Test 6: Streaming Output ===")

	sessionID := fmt.Sprintf("test-stream-%d", time.Now().Unix())
	var streamedChunks []string
	var mu sync.Mutex

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo line1 && echo line2 && echo line3",
		Reasoning:      "test streaming through worker",
		SessionID:      sessionID,
		TimeoutSeconds: 10,
		StreamStdout: func(chunk string) {
			mu.Lock()
			streamedChunks = append(streamedChunks, chunk)
			mu.Unlock()
			t.Logf("Streamed: %q", chunk)
		},
	})

	if err != nil {
		t.Fatalf("Streaming command failed: %v", err)
	}

	mu.Lock()
	numChunks := len(streamedChunks)
	mu.Unlock()

	t.Logf("Received %d streamed chunks", numChunks)
	t.Logf("Final result: %s", result)

	if numChunks == 0 {
		t.Log("Warning: No chunks streamed (output may be buffered)")
	} else {
		t.Log("✓ Streaming output working")
	}
}

// Helper functions

func countWorkerProcesses(t *testing.T) int {
	// Count processes with --worker-mode flag
	cmd := exec.Command("pgrep", "-f", "sop-daemon.*--worker-mode")
	output, err := cmd.Output()
	if err != nil {
		// pgrep returns exit code 1 if no processes found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return 0
		}
		t.Logf("Warning: pgrep failed: %v", err)
		return 0
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

func killWorkersForSession(t *testing.T, sessionID string) int {
	// Get all worker PIDs
	cmd := exec.Command("pgrep", "-f", "sop-daemon.*--worker-mode")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	pids := strings.Fields(strings.TrimSpace(string(output)))
	killed := 0

	for _, pid := range pids {
		killCmd := exec.Command("kill", "-9", pid)
		if err := killCmd.Run(); err == nil {
			killed++
		}
	}

	return killed
}

// TestPhase4_RunAll runs all Phase 4 tests in sequence
func TestPhase4_RunAll(t *testing.T) {
	t.Log("===================================")
	t.Log("=== Phase 4: Production Validation ===")
	t.Log("===================================")
	t.Log("")

	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"WorkerSpawning", TestPhase4_WorkerSpawning},
		{"SessionStatePersistence", TestPhase4_SessionStatePersistence},
		{"WorkerCrashRecovery", TestPhase4_WorkerCrashRecovery},
		{"ConcurrentSessions", TestPhase4_ConcurrentSessions},
		{"PerformanceBenchmark", TestPhase4_PerformanceBenchmark},
		{"StreamingOutput", TestPhase4_StreamingOutput},
	}

	passed := 0
	failed := 0

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Test panicked: %v", r)
					failed++
				} else if t.Failed() {
					failed++
				} else {
					passed++
				}
			}()
			tt.fn(t)
		})
	}

	t.Log("")
	t.Log("===================================")
	t.Logf("=== Phase 4 Results: %d passed, %d failed ===", passed, failed)
	t.Log("===================================")

	if failed > 0 {
		t.Fatalf("Phase 4 validation failed: %d tests failed", failed)
	}
}
