package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestListDirectoryAppliesStorageForLargeResults(t *testing.T) {
	// Create a temp directory with many files to exceed 2KB threshold
	dir := t.TempDir()

	// Create 1000 files to ensure output exceeds 2KB
	for i := 0; i < 1000; i++ {
		fileName := strings.Repeat("very-long-file-name-with-many-chars-", 3) + string(rune('a'+(i%26))) + ".txt"
		filePath := filepath.Join(dir, fileName)
		if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Set up agent
	ag := &CopilotAgent{}

	sessionID := "test-session-large-dir"
	storage, err := NewToolResultStorage(sessionID)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	ctx = context.WithValue(ctx, SessionPayloadKey, &ai.SessionPayload{SessionID: sessionID})

	// Call tool directly
	rawResult, err := ag.toolListDirectory(ctx, map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("toolListDirectory() error = %v", err)
	}

	// Apply storage wrapper (simulating what Execute() does)
	wrappedResult, err := wrapToolResultWithStorage(ctx, "list_directory", rawResult)
	if err != nil {
		t.Fatalf("wrapToolResultWithStorage() error = %v", err)
	}

	// For large directory listing, result should be wrapped with file path
	if len(rawResult) > 2048 {
		// Raw result is large, wrapped result should be a summary
		if !strings.Contains(wrappedResult, "omitted") && !strings.Contains(wrappedResult, "Full output:") {
			t.Fatalf("Expected wrapped result to be a summary for large directory, got: %s", wrappedResult[:200])
		}
		// Verify file path is mentioned
		if !strings.Contains(wrappedResult, "sop-tool-results") {
			t.Fatalf("Summary should mention file path, got: %s", wrappedResult[:500])
		}
		t.Logf("Large result: raw=%d bytes, wrapped=%d bytes", len(rawResult), len(wrappedResult))
	} else {
		t.Fatalf("Test setup error: created directory listing is only %d bytes, need > 2048 for test", len(rawResult))
	}
}

func TestListDirectoryReturnsInlineForSmallResults(t *testing.T) {
	// Create a temp directory with just a few files (under 2KB)
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		filePath := filepath.Join(dir, "small"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Set up agent
	ag := &CopilotAgent{}

	sessionID := "test-session-small-dir"
	storage, err := NewToolResultStorage(sessionID)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	ctx = context.WithValue(ctx, SessionPayloadKey, &ai.SessionPayload{SessionID: sessionID})

	// Call tool directly
	rawResult, err := ag.toolListDirectory(ctx, map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("toolListDirectory() error = %v", err)
	}

	// Apply storage wrapper (simulating what Execute() does)
	wrappedResult, err := wrapToolResultWithStorage(ctx, "list_directory", rawResult)
	if err != nil {
		t.Fatalf("wrapToolResultWithStorage() error = %v", err)
	}

	// For small directory listing, wrapped result contains full content inline
	// The result will be a JSON envelope, but the tool_result field should have the full content
	if !strings.Contains(wrappedResult, "Directory contents") {
		t.Fatalf("Expected inline result to contain directory header, got: %s", wrappedResult)
	}

	// Should contain all files inline
	for i := 0; i < 5; i++ {
		fileName := "small" + string(rune('a'+i)) + ".txt"
		if !strings.Contains(wrappedResult, fileName) {
			t.Fatalf("Expected result to contain %s, got: %s", fileName, wrappedResult)
		}
	}

	// For small results, the full content should be in the envelope (not a summary)
	// This means it should NOT contain "omitted" indicators
	if strings.Contains(wrappedResult, "omitted") || strings.Contains(wrappedResult, "Full output:") {
		t.Fatalf("Small result should contain full content, not summary, got: %s", wrappedResult)
	}

	t.Logf("Small result: raw=%d bytes, wrapped=%d bytes", len(rawResult), len(wrappedResult))
}
