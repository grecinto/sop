package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewToolResultStorage(t *testing.T) {
	sessionID := "test-session-123"
	storage, err := NewToolResultStorage(sessionID)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	// Verify directory was created
	if _, err := os.Stat(storage.GetSessionDir()); os.IsNotExist(err) {
		t.Fatalf("Session directory was not created: %s", storage.GetSessionDir())
	}

	// Verify session ID
	if storage.sessionID != sessionID {
		t.Errorf("Expected sessionID %s, got %s", sessionID, storage.sessionID)
	}
}

func TestStoreResult_SmallInline(t *testing.T) {
	storage, err := NewToolResultStorage("test-inline")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	smallResult := strings.Repeat("a", 1024) // 1KB - below threshold
	inline, filePath, size, err := storage.StoreResult("test_tool", smallResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	if filePath != "" {
		t.Errorf("Expected no file path for small result, got: %s", filePath)
	}

	if inline != smallResult {
		t.Errorf("Expected inline result to match original")
	}

	if size != 1024 {
		t.Errorf("Expected size 1024, got %d", size)
	}
}

func TestStoreResult_LargeFileBacked(t *testing.T) {
	storage, err := NewToolResultStorage("test-large")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	largeResult := strings.Repeat("test output line\n", 2000) // ~30KB - above threshold
	inline, filePath, size, err := storage.StoreResult("run_tests", largeResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Should have file path
	if filePath == "" {
		t.Errorf("Expected file path for large result")
	}

	// File should exist
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("File was not created: %s", filePath)
	}

	// File should contain full result
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != largeResult {
		t.Errorf("File content doesn't match original result")
	}

	// Inline should be a summary
	if len(inline) >= len(largeResult) {
		t.Errorf("Expected inline to be summary (shorter than original), got %d >= %d",
			len(inline), len(largeResult))
	}

	// Summary should include file path
	if !strings.Contains(inline, filePath) {
		t.Errorf("Summary should include file path")
	}

	// Summary should include tool recommendations
	if !strings.Contains(inline, "ANALYSIS TOOLS") {
		t.Errorf("Summary should include analysis tools section")
	}

	if size != len(largeResult) {
		t.Errorf("Expected size %d, got %d", len(largeResult), size)
	}
}

func TestStoreResult_TestToolRecommendations(t *testing.T) {
	storage, err := NewToolResultStorage("test-recommendations")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	largeResult := strings.Repeat("test output\n", 2000)
	inline, _, _, err := storage.StoreResult("run_repo_tests", largeResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Should include test-specific recommendations
	if !strings.Contains(inline, "Finding failures") {
		t.Errorf("Expected test-specific recommendations for test tool")
	}

	if !strings.Contains(inline, "grep \"FAIL\"") {
		t.Errorf("Expected FAIL grep recommendation for test tool")
	}

	if !strings.Contains(inline, "tail -50") {
		t.Errorf("Expected tail recommendation for test tool")
	}
}

func TestStoreResult_BuildToolRecommendations(t *testing.T) {
	storage, err := NewToolResultStorage("test-build")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	largeResult := strings.Repeat("build output\n", 2000)
	inline, _, _, err := storage.StoreResult("compile_project", largeResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Should include build-specific recommendations
	if !strings.Contains(inline, "Finding errors") {
		t.Errorf("Expected build-specific recommendations for build tool")
	}

	if !strings.Contains(inline, "error:") {
		t.Errorf("Expected error grep recommendation for build tool")
	}
}

func TestStoreResult_HeadAndTail(t *testing.T) {
	storage, err := NewToolResultStorage("test-head-tail")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	// Create result with distinctive head and tail
	head := "START_MARKER\n"
	middle := strings.Repeat("middle content\n", 2000)
	tail := "END_MARKER\n"
	largeResult := head + middle + tail

	inline, _, _, err := storage.StoreResult("test_tool", largeResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Summary should include head
	if !strings.Contains(inline, "START_MARKER") {
		t.Errorf("Summary should include head of output")
	}

	// Summary should include tail
	if !strings.Contains(inline, "END_MARKER") {
		t.Errorf("Summary should include tail of output")
	}

	// Summary should indicate omitted bytes
	if !strings.Contains(inline, "bytes omitted") {
		t.Errorf("Summary should indicate omitted bytes")
	}
}

func TestCleanup(t *testing.T) {
	sessionID := "test-cleanup"
	storage, err := NewToolResultStorage(sessionID)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Store a large file (over 20KB threshold)
	largeResult := strings.Repeat("test output line\n", 2000) // ~30KB
	_, filePath, _, err := storage.StoreResult("test_tool", largeResult)
	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Verify file path was returned (result is file-backed)
	if filePath == "" {
		t.Fatalf("Expected file path for large result")
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("File should exist before cleanup")
	}

	// Cleanup
	if err := storage.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify directory is gone
	if _, err := os.Stat(storage.GetSessionDir()); !os.IsNotExist(err) {
		t.Errorf("Session directory should be removed after cleanup")
	}
}

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple_tool", "simple_tool"},
		{"tool-with-dash", "tool-with-dash"},
		{"tool/with/slashes", "tool_with_slashes"},
		{"tool:with:colons", "tool_with_colons"},
		{"tool with spaces", "tool_with_spaces"},
		{strings.Repeat("a", 100), strings.Repeat("a", 50)}, // Length limit
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeToolName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeToolName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanupOldToolResults(t *testing.T) {
	baseDir := filepath.Join(os.TempDir(), "test-cleanup-old")
	defer os.RemoveAll(baseDir)

	// Create old session directory
	oldDir := filepath.Join(baseDir, "old-session")
	if err := os.MkdirAll(oldDir, 0700); err != nil {
		t.Fatalf("Failed to create old directory: %v", err)
	}

	// Create recent session directory
	recentDir := filepath.Join(baseDir, "recent-session")
	if err := os.MkdirAll(recentDir, 0700); err != nil {
		t.Fatalf("Failed to create recent directory: %v", err)
	}

	// Make old directory appear old by modifying its timestamp
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatalf("Failed to modify old directory timestamp: %v", err)
	}

	// Run cleanup with 24 hour TTL
	if err := CleanupOldToolResults(baseDir, 24*time.Hour); err != nil {
		t.Fatalf("CleanupOldToolResults failed: %v", err)
	}

	// Old directory should be gone
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("Old directory should be removed")
	}

	// Recent directory should still exist
	if _, err := os.Stat(recentDir); os.IsNotExist(err) {
		t.Errorf("Recent directory should still exist")
	}
}

func TestIsTestCommand(t *testing.T) {
	tests := []struct {
		toolName string
		expected bool
	}{
		{"run_tests", true},
		{"run_repo_tests", true},
		{"test_suite", true},
		{"execute_spec", true},
		{"build_project", false},
		{"compile_code", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			result := isTestCommand(tt.toolName)
			if result != tt.expected {
				t.Errorf("isTestCommand(%q) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

func TestIsBuildCommand(t *testing.T) {
	tests := []struct {
		toolName string
		expected bool
	}{
		{"build_project", true},
		{"compile_code", true},
		{"make_binary", true},
		{"run_tests", false},
		{"execute_command", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			result := isBuildCommand(tt.toolName)
			if result != tt.expected {
				t.Errorf("isBuildCommand(%q) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}
}
