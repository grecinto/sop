package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWorkflowManager_ScriptOptimization tests script-based step optimization.
func TestWorkflowManager_ScriptOptimization(t *testing.T) {
	ctx := context.Background()

	// Create KB
	kb, err := createTestKnowledgeBase(ctx)
	if err != nil {
		t.Fatalf("Failed to create test KB: %v", err)
	}

	// Create WorkflowManager
	wm := NewWorkflowManager(kb)

	// Create mock script executor
	executor := &mockScriptExecutor{
		results: map[string]map[string]any{
			"test_script": {
				"output":  "Script executed successfully",
				"time_ms": 100,
			},
		},
	}
	wm.SetScriptExecutor(executor)

	// Create temp script library
	tmpDir := t.TempDir()
	wm.SetScriptLibraryPath(tmpDir)

	// Create a test script
	testScript := map[string]any{
		"name":        "test_script",
		"description": "Test script",
		"script": []map[string]any{
			{"op": "noop", "result_var": "output"},
		},
	}
	scriptPath := filepath.Join(tmpDir, "test_script.json")
	data, _ := json.Marshal(testScript)
	if err := os.WriteFile(scriptPath, data, 0644); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	// Test step with optimization
	step := WorkflowStep{
		Step:          1,
		Actor:         "test.actor",
		QueryTemplate: "Test query",
		OptimizationProfile: &OptimizationProfile{
			PreferScript:  true,
			ScriptLibrary: []string{"test_script"},
			FallbackToLLM: false,
			MaxScriptTime: 5000,
		},
	}

	// Test optimization
	result, used := wm.optimizeWithScripts(ctx, step, map[string]any{"input": "test"})
	if !used {
		t.Error("Expected script to be used")
	}
	if result == nil {
		t.Fatal("Expected result from script")
	}
	if result["output"] != "Script executed successfully" {
		t.Errorf("Expected script output, got %v", result)
	}

	// Verify executor was called
	if executor.callCount != 1 {
		t.Errorf("Expected 1 executor call, got %d", executor.callCount)
	}
}

// TestWorkflowManager_ScriptFallback tests fallback to LLM when script fails.
func TestWorkflowManager_ScriptFallback(t *testing.T) {
	ctx := context.Background()

	// Create KB
	kb, err := createTestKnowledgeBase(ctx)
	if err != nil {
		t.Fatalf("Failed to create test KB: %v", err)
	}

	// Create WorkflowManager
	wm := NewWorkflowManager(kb)

	// Create mock script executor that fails
	executor := &mockScriptExecutor{
		shouldFail: true,
	}
	wm.SetScriptExecutor(executor)
	wm.SetScriptLibraryPath("/nonexistent")

	// Test step with fallback enabled
	step := WorkflowStep{
		Step:          1,
		Actor:         "test.actor",
		QueryTemplate: "Test query",
		OptimizationProfile: &OptimizationProfile{
			PreferScript:  true,
			ScriptLibrary: []string{"nonexistent_script"},
			FallbackToLLM: true,
		},
	}

	// Test optimization - should fall back
	result, used := wm.optimizeWithScripts(ctx, step, map[string]any{"input": "test"})
	if used {
		t.Error("Expected fallback to LLM, script should not be used")
	}
	if result != nil {
		t.Error("Expected nil result for LLM fallback")
	}
}

// TestWorkflowManager_ScriptTimeout tests script execution timeout.
func TestWorkflowManager_ScriptTimeout(t *testing.T) {
	ctx := context.Background()

	// Create KB
	kb, err := createTestKnowledgeBase(ctx)
	if err != nil {
		t.Fatalf("Failed to create test KB: %v", err)
	}

	// Create WorkflowManager
	wm := NewWorkflowManager(kb)

	// Create mock script executor that takes too long
	executor := &mockScriptExecutor{
		delay: 2 * time.Second,
	}
	wm.SetScriptExecutor(executor)

	// Create temp script library
	tmpDir := t.TempDir()
	wm.SetScriptLibraryPath(tmpDir)

	// Create a test script
	testScript := map[string]any{
		"name": "slow_script",
		"script": []map[string]any{
			{"op": "noop", "result_var": "output"},
		},
	}
	scriptPath := filepath.Join(tmpDir, "slow_script.json")
	data, _ := json.Marshal(testScript)
	if err := os.WriteFile(scriptPath, data, 0644); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	// Test step with short timeout
	step := WorkflowStep{
		Step:          1,
		Actor:         "test.actor",
		QueryTemplate: "Test query",
		OptimizationProfile: &OptimizationProfile{
			PreferScript:  true,
			ScriptLibrary: []string{"slow_script"},
			FallbackToLLM: true,
			MaxScriptTime: 100, // 100ms timeout
		},
	}

	// Test optimization - should timeout and fall back
	start := time.Now()
	result, used := wm.optimizeWithScripts(ctx, step, map[string]any{"input": "test"})
	elapsed := time.Since(start)

	if used {
		t.Error("Expected fallback after timeout")
	}
	if result != nil {
		t.Error("Expected nil result for timeout fallback")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Timeout should abort quickly, took %v", elapsed)
	}
}

// TestWorkflowManager_LoadScript tests script loading from library.
func TestWorkflowManager_LoadScript(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create test script
	testScript := map[string]any{
		"name":        "load_test",
		"description": "Test loading",
		"script": []map[string]any{
			{"op": "noop"},
		},
	}
	scriptPath := filepath.Join(tmpDir, "load_test.json")
	data, _ := json.Marshal(testScript)
	if err := os.WriteFile(scriptPath, data, 0644); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	// Create KB
	kb, _ := createTestKnowledgeBase(context.Background())
	wm := NewWorkflowManager(kb)
	wm.SetScriptLibraryPath(tmpDir)

	// Test loading
	script, err := wm.LoadScript("load_test")
	if err != nil {
		t.Fatalf("Failed to load script: %v", err)
	}

	if script["name"] != "load_test" {
		t.Errorf("Expected script name 'load_test', got %v", script["name"])
	}

	// Test loading nonexistent script
	_, err = wm.LoadScript("nonexistent")
	if err == nil {
		t.Error("Expected error loading nonexistent script")
	}
}

// mockScriptExecutor implements ScriptExecutor for testing.
type mockScriptExecutor struct {
	results    map[string]map[string]any
	shouldFail bool
	delay      time.Duration
	callCount  int
}

func (m *mockScriptExecutor) ExecuteScript(ctx context.Context, scriptPath string, input map[string]any) (map[string]any, error) {
	m.callCount++

	// Simulate delay
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.shouldFail {
		return nil, context.DeadlineExceeded
	}

	// Extract script name from path
	scriptName := filepath.Base(scriptPath)
	scriptName = scriptName[:len(scriptName)-5] // Remove .json

	if result, ok := m.results[scriptName]; ok {
		return result, nil
	}

	return map[string]any{"output": "default result"}, nil
}
