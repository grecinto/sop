package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolResultStorage_ListReadGrep(t *testing.T) {
	// Create temporary storage
	tempDir := t.TempDir()
	storage := &ToolResultStorage{
		sessionID: "test-session",
		baseDir:   tempDir,
	}

	// Store some test results large enough to trigger file-backed storage
	testOutput1 := strings.Repeat("line content with more data\n", 1000) +
		"ERROR: Test failed\n" +
		strings.Repeat("more line content here\n", 1000)
	testOutput2 := "PASS: Test passed\n" + strings.Repeat("ok test passed\n", 2000)

	_, file1, _, err := storage.StoreResult("run_tests", testOutput1)
	if err != nil {
		t.Fatalf("failed to store result 1: %v", err)
	}
	if file1 == "" {
		t.Fatal("expected file path for large result")
	}

	_, _, _, err = storage.StoreResult("execute_command", testOutput2)
	if err != nil {
		t.Fatalf("failed to store result 2: %v", err)
	}

	// Test ListStoredResults
	results, err := storage.ListStoredResults()
	if err != nil {
		t.Fatalf("ListStoredResults failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Test ReadResultLines - full file
	content, err := storage.ReadResultLines(file1, 1, -1)
	if err != nil {
		t.Fatalf("ReadResultLines failed: %v", err)
	}
	if !strings.Contains(content, "ERROR: Test failed") {
		t.Error("expected error message in content")
	}

	// Test ReadResultLines - specific range
	content, err = storage.ReadResultLines(file1, 100, 105)
	if err != nil {
		t.Fatalf("ReadResultLines range failed: %v", err)
	}
	lines := strings.Split(content, "\n")
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines, got %d", len(lines))
	}

	// Test GrepResult
	matches, err := storage.GrepResult(file1, "ERROR", 10)
	if err != nil {
		t.Fatalf("GrepResult failed: %v", err)
	}
	if len(matches) == 0 {
		t.Error("expected at least one match for 'ERROR'")
	}
	if matches[0].LineNumber != 1001 {
		t.Errorf("expected error on line 1001, got line %d", matches[0].LineNumber)
	}

	// Test GrepResult with max matches
	matches, err = storage.GrepResult(file1, "line", 5)
	if err != nil {
		t.Fatalf("GrepResult with limit failed: %v", err)
	}
	if len(matches) != 5 {
		t.Errorf("expected exactly 5 matches, got %d", len(matches))
	}

	// Test security: accessing file outside session dir
	outsideFile := filepath.Join(os.TempDir(), "outside.txt")
	_, err = storage.ReadResultLines(outsideFile, 1, 10)
	if err == nil || !strings.Contains(err.Error(), "outside session directory") {
		t.Error("expected security error for file outside session directory")
	}
}

func TestToolResultAnalysis_AnalyzeToolResultOperations(t *testing.T) {
	storage, err := NewToolResultStorage("test-analysis-ops", nil)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	output := "INFO: start\nERROR: first\nWARN: caution\nERROR: second\nINFO: end\n"
	_, filePath, _, err := storage.StoreResult("integration_test", output)
	if err != nil {
		t.Fatalf("failed to store test result: %v", err)
	}

	agent := &CopilotAgent{}

	result, err := agent.toolAnalyzeToolResult(ctx, map[string]any{
		"file_path": filePath,
		"operation": "grep",
		"pattern":   "ERROR",
	})
	if err != nil {
		t.Fatalf("toolAnalyzeToolResult grep failed: %v", err)
	}
	if !strings.Contains(result, "ERROR: first") || !strings.Contains(result, "ERROR: second") {
		t.Fatalf("expected grep output to include both error lines, got: %s", result)
	}

	result, err = agent.toolAnalyzeToolResult(ctx, map[string]any{
		"file_path":  filePath,
		"operation":  "sed",
		"start_line": 2,
		"end_line":   4,
	})
	if err != nil {
		t.Fatalf("toolAnalyzeToolResult sed failed: %v", err)
	}
	if !strings.Contains(result, "ERROR: first") || !strings.Contains(result, "WARN: caution") {
		t.Fatalf("expected sed output to include the requested range, got: %s", result)
	}

	result, err = agent.toolAnalyzeToolResult(ctx, map[string]any{
		"file_path":   filePath,
		"operation":   "tail",
		"max_matches": 2,
	})
	if err != nil {
		t.Fatalf("toolAnalyzeToolResult tail failed: %v", err)
	}
	if !strings.Contains(result, "ERROR: second") || !strings.Contains(result, "INFO: end") {
		t.Fatalf("expected tail output to include the last lines, got: %s", result)
	}
}

func TestToolResultAnalysis_AgentTools(t *testing.T) {
	// Create agent with storage
	agent := &CopilotAgent{}

	storage, err := NewToolResultStorage("test-session-123", nil)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)

	// Store a test result file large enough to trigger file-backed storage
	testOutput := strings.Repeat("INFO: Starting test run\n", 500) +
		"ERROR: Connection timeout\n" +
		"ERROR: Retry failed\n" +
		strings.Repeat("INFO: Cleanup and teardown\n", 500)

	_, filePath, _, err := storage.StoreResult("integration_test", testOutput)
	if err != nil {
		t.Fatalf("failed to store test result: %v", err)
	}

	// Test list_tool_results
	result, err := agent.toolListToolResults(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("toolListToolResults failed: %v", err)
	}
	if !strings.Contains(result, "integration_test") {
		t.Errorf("expected result to mention test name, got: %s", result)
	}

	// Test grep_tool_result
	result, err = agent.toolGrepToolResult(ctx, map[string]any{
		"file_path": filePath,
		"pattern":   "ERROR",
	})
	if err != nil {
		t.Fatalf("toolGrepToolResult failed: %v", err)
	}
	if !strings.Contains(result, "Connection timeout") {
		t.Errorf("expected grep to find error, got: %s", result)
	}
	if !strings.Contains(result, "Line 501:") {
		t.Error("expected line numbers in grep output")
	}
	if !strings.Contains(result, "Suggested next step:") || !strings.Contains(result, "read_tool_result(file_path=") {
		t.Errorf("expected grep output to suggest follow-up reads, got: %s", result)
	}

	result, err = agent.toolGrepToolResult(ctx, map[string]any{
		"file_path": filePath,
		"pattern":   "fail|error|panic",
	})
	if err != nil {
		t.Fatalf("toolGrepToolResult regex failed: %v", err)
	}
	if !strings.Contains(result, "Connection timeout") || !strings.Contains(result, "Retry failed") {
		t.Errorf("expected alternation pattern to find error lines, got: %s", result)
	}

	// Test read_tool_result
	result, err = agent.toolReadToolResult(ctx, map[string]any{
		"file_path":  filePath,
		"start_line": 501,
		"end_line":   502,
	})
	if err != nil {
		t.Fatalf("toolReadToolResult failed: %v", err)
	}
	if !strings.Contains(result, "Connection timeout") || !strings.Contains(result, "Retry failed") {
		t.Errorf("expected both error lines, got: %s", result)
	}
}

func TestToolResultAnalysis_GrepToolResultSuggestsSourceLookupForFailingTests(t *testing.T) {
	agent := &CopilotAgent{}
	storage, err := NewToolResultStorage("test-session-fail-block", nil)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	testOutput := strings.Join([]string{
		"=== RUN   TestRepositoryConfigAllowsSOPKBPreloadByDefault",
		"--- FAIL: TestRepositoryConfigAllowsSOPKBPreloadByDefault (0.00s)",
		"FAIL\tgithub.com/sharedcode/sop/tools/sopbackend\t4.591s",
	}, "\n")
	_, filePath, _, err := storage.StoreResult("run_repo_tests", testOutput)
	if err != nil {
		t.Fatalf("failed to store test result: %v", err)
	}

	result, err := agent.toolGrepToolResult(ctx, map[string]any{
		"file_path": filePath,
		"pattern":   "FAIL|--- FAIL",
	})
	if err != nil {
		t.Fatalf("toolGrepToolResult failed: %v", err)
	}
	if !strings.Contains(result, "grep_repo_text(path=\".\", query=\"func TestRepositoryConfigAllowsSOPKBPreloadByDefault\")") {
		t.Fatalf("expected grep output to suggest source lookup for failing test, got: %s", result)
	}
	if !strings.Contains(result, "inspect files under package github.com/sharedcode/sop/tools/sopbackend") {
		t.Fatalf("expected grep output to mention failing package, got: %s", result)
	}
}

func TestToolResultAnalysis_ReadAndAnalyzeSuggestSourceLookupForFailingTests(t *testing.T) {
	agent := &CopilotAgent{}
	storage, err := NewToolResultStorage("test-session-read-fail-block", nil)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Cleanup()

	ctx := context.WithValue(context.Background(), ctxKeyToolResultStorage, storage)
	testOutput := strings.Join([]string{
		"=== RUN   TestRepositoryConfigAllowsSOPKBPreloadByDefault",
		"--- FAIL: TestRepositoryConfigAllowsSOPKBPreloadByDefault (0.00s)",
		"FAIL\tgithub.com/sharedcode/sop/tools/sopbackend\t4.591s",
	}, "\n")
	_, filePath, _, err := storage.StoreResult("run_repo_tests", testOutput)
	if err != nil {
		t.Fatalf("failed to store test result: %v", err)
	}

	readResult, err := agent.toolReadToolResult(ctx, map[string]any{
		"file_path":  filePath,
		"start_line": 1,
		"end_line":   3,
	})
	if err != nil {
		t.Fatalf("toolReadToolResult failed: %v", err)
	}
	if !strings.Contains(readResult, "grep_repo_text(path=\".\", query=\"func TestRepositoryConfigAllowsSOPKBPreloadByDefault\")") {
		t.Fatalf("expected read_tool_result to suggest source lookup, got: %s", readResult)
	}

	analyzeResult, err := agent.toolAnalyzeToolResult(ctx, map[string]any{
		"file_path":   filePath,
		"operation":   "tail",
		"max_matches": 10,
	})
	if err != nil {
		t.Fatalf("toolAnalyzeToolResult failed: %v", err)
	}
	if !strings.Contains(analyzeResult, "inspect files under package github.com/sharedcode/sop/tools/sopbackend") {
		t.Fatalf("expected analyze_tool_result to suggest package inspection, got: %s", analyzeResult)
	}
}
