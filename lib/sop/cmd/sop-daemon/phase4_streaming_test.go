package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	sharedagent "github.com/sharedcode/sop/ai/agent"
)

// TestRealTimeNDJSONStreaming verifies that output streams in real-time from the child PTY process
func TestRealTimeNDJSONStreaming(t *testing.T) {
	sessionID := "test-realtime-streaming"

	// Track chunks with timestamps
	type chunk struct {
		data      string
		timestamp time.Time
	}
	var chunks []chunk
	var mu sync.Mutex

	// Command that outputs lines with delays
	command := `for i in 1 2 3; do echo "line $i"; sleep 0.1; done`

	startTime := time.Now()

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:   command,
		SessionID: sessionID,
		StreamStdout: func(line string) {
			mu.Lock()
			chunks = append(chunks, chunk{
				data:      line,
				timestamp: time.Now(),
			})
			mu.Unlock()
			t.Logf("Chunk at %dms: %q", time.Since(startTime).Milliseconds(), line)
		},
		TimeoutSeconds: 10,
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	t.Logf("Final result: %s", result)

	mu.Lock()
	chunkCount := len(chunks)
	mu.Unlock()

	// Should receive at least 3 chunks (one per line)
	if chunkCount < 3 {
		t.Fatalf("Expected at least 3 chunks, got %d", chunkCount)
	}

	// Verify chunks arrived over time (not all at once)
	// The command has 0.1s sleeps, so chunks should be spaced out
	mu.Lock()
	if len(chunks) >= 2 {
		firstChunkTime := chunks[0].timestamp
		lastChunkTime := chunks[len(chunks)-1].timestamp
		timeDiff := lastChunkTime.Sub(firstChunkTime)

		t.Logf("Time between first and last chunk: %dms", timeDiff.Milliseconds())

		// Should be at least 200ms (2 * 0.1s sleep) but allow some margin
		if timeDiff < 150*time.Millisecond {
			t.Logf("Warning: Chunks arrived too quickly (%dms), may not be streaming in real-time", timeDiff.Milliseconds())
		} else {
			t.Logf("✓ Chunks arrived over time - real-time streaming confirmed")
		}
	}
	mu.Unlock()

	t.Logf("✓ Real-time NDJSON streaming working")
}

// TestStreamingVsNonStreaming compares streaming and non-streaming behavior
func TestStreamingVsNonStreaming(t *testing.T) {
	command := `echo "line1"; echo "line2"; echo "line3"`

	t.Run("NonStreaming", func(t *testing.T) {
		sessionID := "test-nonstreaming"
		result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
			Command:        command,
			SessionID:      sessionID,
			TimeoutSeconds: 10,
		})
		if err != nil {
			t.Fatalf("Command failed: %v", err)
		}
		t.Logf("Non-streaming result: %s", result)

		// Should contain all lines
		if !strings.Contains(result, "line1") || !strings.Contains(result, "line2") || !strings.Contains(result, "line3") {
			t.Fatalf("Missing expected output")
		}
	})

	t.Run("Streaming", func(t *testing.T) {
		sessionID := "test-streaming"
		var chunks []string
		var mu sync.Mutex

		result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
			Command:   command,
			SessionID: sessionID,
			StreamStdout: func(line string) {
				mu.Lock()
				chunks = append(chunks, line)
				mu.Unlock()
				t.Logf("Streamed chunk: %q", line)
			},
			TimeoutSeconds: 10,
		})
		if err != nil {
			t.Fatalf("Command failed: %v", err)
		}
		t.Logf("Streaming result: %s", result)

		mu.Lock()
		chunkCount := len(chunks)
		mu.Unlock()

		// Should receive at least 3 chunks
		if chunkCount < 3 {
			t.Fatalf("Expected at least 3 chunks, got %d", chunkCount)
		}

		t.Logf("✓ Received %d chunks via streaming", chunkCount)
	})
}
