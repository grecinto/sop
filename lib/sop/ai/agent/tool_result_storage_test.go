package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai"
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
	storage, err := NewToolResultStorage("test-inline", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	smallResult := strings.Repeat("a", 1024) // 1KB - below threshold
	inline, filePath, size, err := storage.StoreResult("test_tool", smallResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Phase 2: Files are created even for small results (for debugging/auditing)
	if filePath == "" {
		t.Errorf("Expected file path even for small result")
	}

	// But small results are returned inline (not summarized)
	if inline != smallResult {
		t.Errorf("Expected inline result to match original for small result")
	}

	if size != 1024 {
		t.Errorf("Expected size 1024, got %d", size)
	}

	// Verify file was created
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("File should be created even for small results: %s", filePath)
	}
}

func TestWrapToolResultWithStorage_SmallResultReturnsRawInlineContent(t *testing.T) {
	storage, err := NewToolResultStorage("test-inline-envelope", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	ctx = context.WithValue(ctx, SessionPayloadKey, &ai.SessionPayload{SessionID: "test-inline-envelope"})

	wrapped, err := wrapToolResultWithStorage(ctx, "list_directory", "small inline result")
	if err != nil {
		t.Fatalf("wrapToolResultWithStorage() error = %v", err)
	}
	if wrapped != "small inline result" {
		t.Fatalf("expected raw inline result, got %q", wrapped)
	}
}

func TestWrapToolResultWithStorage_BypassesAnalysisTools(t *testing.T) {
	storage, err := NewToolResultStorage("test-analysis-bypass", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	ctx = context.WithValue(ctx, SessionPayloadKey, &ai.SessionPayload{SessionID: "test-analysis-bypass"})

	_, _, _, err = storage.StoreResult("run_repo_tests", strings.Repeat("x", 5000))
	if err != nil {
		t.Fatalf("failed to seed stored result: %v", err)
	}
	before, err := storage.ListStoredResults()
	if err != nil {
		t.Fatalf("ListStoredResults before failed: %v", err)
	}

	raw := strings.Repeat("listed result\n", 300)
	wrapped, err := wrapToolResultWithStorage(ctx, "list_tool_results", raw)
	if err != nil {
		t.Fatalf("wrapToolResultWithStorage() error = %v", err)
	}
	if wrapped != raw {
		t.Fatalf("expected raw result for analysis tool, got %q", wrapped)
	}
	after, err := storage.ListStoredResults()
	if err != nil {
		t.Fatalf("ListStoredResults after failed: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected analysis tool output to avoid recursive storage, before=%d after=%d", len(before), len(after))
	}
}

func TestStoreResult_LargeFileBacked(t *testing.T) {
	storage, err := NewToolResultStorage("test-large", nil)
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

	// Phase 2: Passing tests use standard summary format with explicit file-backed guidance.
	if !strings.Contains(inline, "Use the saved file to confirm the outcome based on your request") {
		t.Errorf("Summary should include standard analysis guidance")
	}

	if size != len(largeResult) {
		t.Errorf("Expected size %d, got %d", len(largeResult), size)
	}
}

func TestStoreResult_TestToolRecommendations(t *testing.T) {
	storage, err := NewToolResultStorage("test-recommendations", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	largeResult := strings.Repeat("test output\n", 2000)
	inline, _, _, err := storage.StoreResult("run_repo_tests", largeResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Phase 2: Passing tests (no failure indicators) use standard summary format
	if !strings.Contains(inline, "Use the saved file to confirm the outcome based on your request") {
		t.Errorf("Expected standard analysis guidance for passing tests")
	}
}

func TestStoreResult_BuildToolRecommendations(t *testing.T) {
	storage, err := NewToolResultStorage("test-build", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	largeResult := strings.Repeat("build output\n", 2000)
	inline, filePath, _, err := storage.StoreResult("compile_project", largeResult)

	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	// Phase 2: Build tools use standard summary format
	if !strings.Contains(inline, "Use the saved file to confirm the outcome based on your request") {
		t.Errorf("Expected standard analysis guidance for build tools")
	}
	if !strings.Contains(inline, filePath) {
		t.Errorf("Expected concrete file path in analysis guidance, got: %s", inline)
	}
	if !strings.Contains(inline, "Do not infer the final outcome from the truncated preview alone") {
		t.Errorf("Expected explicit warning against inferring from preview, got: %s", inline)
	}
	if !strings.Contains(inline, "grep_tool_result(file_path=") {
		t.Errorf("Expected grep_tool_result recommendation for known-signal analysis, got: %s", inline)
	}
}

func TestStoreResult_TestFailureSummarySurfacesLateFailingTestBlock(t *testing.T) {
	storage, err := NewToolResultStorage("test-late-failure", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	var output strings.Builder
	for i := 0; i < 300; i++ {
		output.WriteString("=== RUN   TestPassingCase\n")
		output.WriteString("ok\tgithub.com/sharedcode/sop/pkg\t0.010s\n")
	}
	output.WriteString("=== RUN   TestFloaterUsesSharedRuntimeResponseHandler\n")
	output.WriteString("--- FAIL: TestFloaterUsesSharedRuntimeResponseHandler (0.00s)\n")
	output.WriteString("FAIL\n")
	output.WriteString("FAIL\tgithub.com/sharedcode/sop/tools/httpserver\t1.862s\n")

	inline, _, _, err := storage.StoreResult("run_repo_tests", output.String())
	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}
	if !strings.Contains(inline, "TestFloaterUsesSharedRuntimeResponseHandler") {
		t.Fatalf("expected late failing test name to be surfaced, got: %s", inline)
	}
	if !strings.Contains(inline, "github.com/sharedcode/sop/tools/httpserver") {
		t.Fatalf("expected failing package to be surfaced, got: %s", inline)
	}
	if !strings.Contains(inline, "Read the failing test source code") {
		t.Fatalf("expected next-step guidance to inspect the test source, got: %s", inline)
	}
	if !strings.Contains(inline, "After finding a failing test name") {
		t.Fatalf("expected explicit handoff to source inspection, got: %s", inline)
	}
}

func TestStoreResult_HeadAndTail(t *testing.T) {
	storage, err := NewToolResultStorage("test-head-tail", nil)
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

	// Phase 2: Standard summary format includes head and tail with analysis guidance
	// The exact format may vary, but should have analysis instructions
	if !strings.Contains(inline, "Use the saved file to confirm the outcome based on your request") && !strings.Contains(inline, "Full output") {
		t.Errorf("Summary should include analysis guidance")
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

func TestReadResultLines_MissingFileReturnsEmpty(t *testing.T) {
	storage, err := NewToolResultStorage("test-missing-file", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	content, err := storage.ReadResultLines(filepath.Join(storage.GetSessionDir(), "missing.txt"), 1, 10)
	if err != nil {
		t.Fatalf("ReadResultLines returned unexpected error: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content for missing file, got %q", content)
	}
}

func TestGrepResult_MissingFileReturnsEmpty(t *testing.T) {
	storage, err := NewToolResultStorage("test-missing-grep", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	matches, err := storage.GrepResult(filepath.Join(storage.GetSessionDir(), "missing.txt"), "needle", 10)
	if err != nil {
		t.Fatalf("GrepResult returned unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no matches for missing file, got %+v", matches)
	}
}

func TestGrepResult_SupportsCaseInsensitiveAlternation(t *testing.T) {
	storage, err := NewToolResultStorage("test-grep-regex", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	output := "ok\nERROR: first failure\npanic: boom\n--- FAIL: TestSomething\n"
	_, filePath, _, err := storage.StoreResult("run_repo_tests", output)
	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	matches, err := storage.GrepResult(filePath, "fail|error|panic", 10)
	if err != nil {
		t.Fatalf("GrepResult failed: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 regex matches, got %d: %#v", len(matches), matches)
	}
	if !strings.Contains(matches[0].Content, "ERROR") {
		t.Fatalf("expected first match to include ERROR line, got %#v", matches[0])
	}
	if !strings.Contains(matches[2].Content, "--- FAIL") {
		t.Fatalf("expected fail block match, got %#v", matches[2])
	}
}

func TestReadResultLines_ResolvesWildcardAndBasenamePaths(t *testing.T) {
	storage, err := NewToolResultStorage("test-path-resolution", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	output := "line one\nline two\nline three\n"
	_, filePath, _, err := storage.StoreResult("run_repo_tests", output)
	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	wildcardPath := filepath.Join(storage.GetSessionDir(), "run_repo_tests-*.txt")
	content, err := storage.ReadResultLines(wildcardPath, 1, 2)
	if err != nil {
		t.Fatalf("ReadResultLines wildcard failed: %v", err)
	}
	if !strings.Contains(content, "line one") || !strings.Contains(content, "line two") {
		t.Fatalf("expected wildcard resolution to read stored file, got %q", content)
	}

	baseNamePath := "run_repo_tests.txt"
	content, err = storage.ReadResultLines(baseNamePath, 2, 3)
	if err != nil {
		t.Fatalf("ReadResultLines basename failed: %v", err)
	}
	if !strings.Contains(content, "line two") || !strings.Contains(content, "line three") {
		t.Fatalf("expected basename resolution to read stored file, got %q", content)
	}

	if filepath.Base(filePath) == baseNamePath {
		t.Fatalf("test setup invalid: expected stored file to include unique suffix")
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

// Phase 2 Tests: Auto-inline critical error portions

func TestPhase2_TestFailureAutoInline(t *testing.T) {
	storage, err := NewToolResultStorage("test-failure-inline", nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	// Simulate large test output with failure
	testOutput := `=== RUN   TestHandleVectorizationRecoveryStatus
Running vectorization recovery test...
--- FAIL: TestHandleVectorizationRecoveryStatus (0.05s)
    main_test.go:430: 
        Error: can't do operation on b-tree if transaction has not begun
FAIL
` + strings.Repeat("Additional output\n", 500)

	inline, filePath, _, err := storage.StoreResult("run_repo_tests", testOutput)
	if err != nil {
		t.Fatalf("StoreResult failed: %v", err)
	}

	if !strings.Contains(inline, "⚠️  TEST FAILURE DETECTED") {
		t.Errorf("Summary should include failure warning")
	}

	if !strings.Contains(inline, "CRITICAL ERROR OUTPUT") {
		t.Errorf("Summary should include critical error section")
	}

	if !strings.Contains(inline, "can't do operation on b-tree") {
		t.Errorf("Summary should inline the actual error message")
	}

	if !strings.Contains(inline, filePath) {
		t.Errorf("Summary should include file path")
	}
}

func TestPhase2_ExtractCriticalErrorSection(t *testing.T) {
	input := `Setup line
--- FAIL: TestSomething
Error occurred
Stack trace`

	result := extractCriticalErrorSection(input, 50)

	if !strings.Contains(result, "--- FAIL: TestSomething") {
		t.Errorf("Should extract error section")
	}
	if !strings.Contains(result, "Setup line") {
		t.Errorf("Should include context before error")
	}
}
