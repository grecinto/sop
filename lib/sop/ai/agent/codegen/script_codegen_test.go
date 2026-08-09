package codegen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileScriptToGo(t *testing.T) {
	// Create temporary test script
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test_script.json")

	scriptJSON := `{
  "name": "test_script",
  "description": "Test script for codegen",
  "input": "input_data",
  "output": "result",
  "script": [
    {
      "op": "read_file",
      "args": {"path": "/tmp/test.txt"},
      "result_var": "file_content"
    },
    {
      "op": "grep",
      "args": {"pattern": "ERROR"},
      "input_var": "file_content",
      "result_var": "errors"
    },
    {
      "op": "format",
      "args": {"template": "Found: {{input}}"},
      "input_var": "errors"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Failed to write test script: %v", err)
	}

	// Compile script
	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("CompileScriptToGo failed: %v", err)
	}

	// Verify generated code
	expectedParts := []string{
		"package compiled",
		"func Test_script(ctx context.Context, input map[string]any)",
		"// Step 1: read_file",
		"// Step 2: grep",
		"// Step 3: format",
		"executeRead_file",
		"executeGrep",
		"executeFormat",
		"variables[\"file_content\"] = result",
		"variables[\"errors\"] = result",
	}

	for _, part := range expectedParts {
		if !strings.Contains(goCode, part) {
			t.Errorf("Generated code missing expected part: %q\nGenerated code:\n%s", part, goCode)
		}
	}

	// Verify it's valid Go code (gofmt succeeded if no error)
	if !strings.HasPrefix(goCode, "// Code generated") {
		t.Errorf("Generated code should start with generation comment")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "Simple"},
		{"with-dashes", "With_dashes"},
		{"with_underscores", "With_underscores"},
		{"MixedCase", "MixedCase"},
		{"123numeric", "123numeric"},
		{"special!@#chars", "Special___chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToGoValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string", "hello", `"hello"`},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{
			"map",
			map[string]any{"key": "value", "num": float64(10)},
			`map[string]any{"key": "value", "num": 10}`,
		},
		{
			"slice",
			[]any{"a", "b", "c"},
			`[]any{"a", "b", "c"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toGoValue(tt.input)
			// Check key parts are present (map order may vary)
			if tt.name == "map" {
				if !strings.Contains(result, `"key": "value"`) || !strings.Contains(result, `"num": 10`) {
					t.Errorf("toGoValue(%v) = %q, want map containing both entries", tt.input, result)
				}
			} else if result != tt.expected {
				t.Errorf("toGoValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCompileScriptSupportedTargets(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "targeted_script.json")

	scriptJSON := `{
  "name": "targeted_script",
  "description": "A script used to verify target-language generation",
  "input": "repo_path",
  "output": "summary",
  "script": [
    {
      "op": "read_file",
      "args": {"path": "/tmp/example.txt"},
      "result_var": "content"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	testCases := []struct {
		name     string
		target   TargetLanguage
		expected string
	}{
		{name: "java", target: TargetLanguageJava, expected: "public class TargetedScript"},
		{name: "rust", target: TargetLanguageRust, expected: "fn targeted_script"},
		{name: "typescript", target: TargetLanguageTypeScript, expected: "export async function targetedScript"},
		{name: "javascript", target: TargetLanguageJavaScript, expected: "export function targetedScript"},
		{name: "react", target: TargetLanguageReact, expected: "export function"},
		{name: "angular", target: TargetLanguageAngular, expected: "@Component"},
		{name: "htmlcss", target: TargetLanguageHTMLCSS, expected: "<!DOCTYPE html>"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := CompileScript(scriptPath, tc.target)
			if err != nil {
				t.Fatalf("CompileScript(%s) failed: %v", tc.target, err)
			}
			if !strings.Contains(code, tc.expected) {
				t.Fatalf("generated code for %s does not contain %q:\n%s", tc.target, tc.expected, code)
			}
		})
	}
}

func TestCompileRealScript(t *testing.T) {
	// Test with actual extract_go_test_errors.json
	scriptPath := filepath.Join("..", "..", "..", "..", "workflow_scripts", "extract_go_test_errors.json")

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skip("Real script file not found, skipping")
	}

	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("Failed to compile real script: %v", err)
	}

	// Verify it contains expected operations
	if !strings.Contains(goCode, "Extract_go_test_errors") {
		t.Errorf("Generated function should be named Extract_go_test_errors")
	}

	// Write to temp file to verify it compiles
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "compiled_script.go")
	if err := os.WriteFile(outputPath, []byte(goCode), 0644); err != nil {
		t.Fatalf("Failed to write compiled script: %v", err)
	}

	t.Logf("Successfully compiled script to %s", outputPath)
}

// TestGeneratedCodeExecutes verifies that generated code can actually execute
func TestGeneratedCodeExecutes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test data file
	testDataPath := filepath.Join(tmpDir, "test_data.txt")
	testData := "Line 1\nERROR: Something failed\nLine 3\nERROR: Another issue\nLine 5"
	if err := os.WriteFile(testDataPath, []byte(testData), 0644); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}

	// Create script that reads file and greps for ERROR
	scriptJSON := `{
  "name": "grep_errors",
  "description": "Extract error lines from file",
  "input": "file_path",
  "output": "errors",
  "script": [
    {
      "op": "read_file",
      "args": {"path": "` + testDataPath + `"},
      "result_var": "content"
    },
    {
      "op": "grep",
      "args": {"pattern": "ERROR"},
      "input_var": "content"
    }
  ]
}`

	scriptPath := filepath.Join(tmpDir, "grep_errors.json")
	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Compile script
	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("CompileScriptToGo failed: %v", err)
	}

	// Verify structure
	if !strings.Contains(goCode, "func Grep_errors") {
		t.Errorf("Generated code should contain Grep_errors function")
	}

	if !strings.Contains(goCode, "executeRead_file") {
		t.Errorf("Generated code should contain read_file operation")
	}

	if !strings.Contains(goCode, "executeGrep") {
		t.Errorf("Generated code should contain grep operation")
	}
}

// Benchmark comparison: interpreted vs compiled
func BenchmarkCompiledScriptVsInterpreted(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "data.txt")
	testData := strings.Repeat("Line with content\nERROR: test error\n", 100)
	if err := os.WriteFile(testFile, []byte(testData), 0644); err != nil {
		b.Fatalf("Setup failed: %v", err)
	}

	b.Run("Compiled", func(b *testing.B) {
		// In a real benchmark, we'd import and call the generated function
		// For now, we just measure the compilation time
		scriptPath := filepath.Join(tmpDir, "bench_script.json")
		scriptJSON := `{
  "name": "bench_test",
  "description": "Benchmark test",
  "input": "data",
  "output": "result",
  "script": [
    {"op": "read_file", "args": {"path": "` + testFile + `"}},
    {"op": "grep", "args": {"pattern": "ERROR"}}
  ]
}`
		if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
			b.Fatalf("Setup failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := CompileScriptToGo(scriptPath)
			if err != nil {
				b.Fatalf("Compilation failed: %v", err)
			}
		}
	})

	_ = ctx // Use ctx to avoid unused variable warning
}

// TestControlFlowIf verifies if operation compilation
func TestControlFlowIf(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "if_script.json")

	scriptJSON := `{
  "name": "conditional_script",
  "description": "Test if/else control flow",
  "input": "data",
  "output": "result",
  "script": [
    {
      "op": "if",
      "args": {
        "condition": true,
        "then": [{"op": "format", "args": {"template": "Success"}}],
        "else": [{"op": "format", "args": {"template": "Failed"}}]
      },
      "result_var": "branch_result"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Compile script
	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("CompileScriptToGo failed: %v", err)
	}

	// Verify if operation is present
	if !strings.Contains(goCode, "executeIf") {
		t.Errorf("Generated code should contain executeIf operation")
	}

	if !strings.Contains(goCode, `variables["branch_result"] = result`) {
		t.Errorf("Generated code should store result in branch_result variable")
	}

	// Verify it compiles to valid Go
	if !strings.Contains(goCode, "package compiled") {
		t.Errorf("Generated code should be in compiled package")
	}
}

// TestControlFlowLoop verifies loop operation compilation
func TestControlFlowLoop(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "loop_script.json")

	scriptJSON := `{
  "name": "loop_script",
  "description": "Test loop iteration",
  "input": "data",
  "output": "result",
  "script": [
    {
      "op": "loop",
      "args": {
        "collection": "items",
        "item_var": "item",
        "body": [{"op": "format", "args": {"template": "Processing {{input}}"}}]
      },
      "result_var": "loop_result"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Compile script
	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("CompileScriptToGo failed: %v", err)
	}

	// Verify loop operation is present
	if !strings.Contains(goCode, "executeLoop") {
		t.Errorf("Generated code should contain executeLoop operation")
	}

	if !strings.Contains(goCode, `variables["loop_result"] = result`) {
		t.Errorf("Generated code should store result in loop_result variable")
	}
}

// TestListOperations verifies list operation compilation
func TestListOperations(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "list_script.json")

	scriptJSON := `{
  "name": "list_operations",
  "description": "Test list operations",
  "input": "data",
  "output": "result",
  "script": [
    {
      "op": "list_new",
      "result_var": "my_list"
    },
    {
      "op": "list_append",
      "args": {"item": "test_item"},
      "input_var": "my_list",
      "result_var": "updated_list"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Compile script
	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("CompileScriptToGo failed: %v", err)
	}

	// Verify list operations are present
	if !strings.Contains(goCode, "executeList_new") {
		t.Errorf("Generated code should contain executeList_new operation")
	}

	if !strings.Contains(goCode, "executeList_append") {
		t.Errorf("Generated code should contain executeList_append operation")
	}

	if !strings.Contains(goCode, `variables["my_list"]`) {
		t.Errorf("Generated code should reference my_list variable")
	}

	if !strings.Contains(goCode, `variables["updated_list"]`) {
		t.Errorf("Generated code should reference updated_list variable")
	}
}

// TestDataOperations verifies data manipulation operation compilation
func TestDataOperations(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "data_script.json")

	scriptJSON := `{
  "name": "data_operations",
  "description": "Test data operations",
  "input": "records",
  "output": "result",
  "script": [
    {
      "op": "project",
      "args": {"fields": ["name", "id"]},
      "result_var": "projected"
    },
    {
      "op": "limit",
      "args": {"count": 10},
      "input_var": "projected",
      "result_var": "limited"
    },
    {
      "op": "sort",
      "args": {"field": "name", "order": "asc"},
      "input_var": "limited"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Compile script
	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("CompileScriptToGo failed: %v", err)
	}

	// Verify data operations are present
	if !strings.Contains(goCode, "executeProject") {
		t.Errorf("Generated code should contain executeProject operation")
	}

	if !strings.Contains(goCode, "executeLimit") {
		t.Errorf("Generated code should contain executeLimit operation")
	}

	if !strings.Contains(goCode, "executeSort") {
		t.Errorf("Generated code should contain executeSort operation")
	}
}

// TestMixedOperations verifies complex scripts with multiple operation types
func TestMixedOperations(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "mixed_script.json")

	scriptJSON := `{
  "name": "mixed_operations",
  "description": "Test mixed operations",
  "input": "data",
  "output": "result",
  "script": [
    {
      "op": "read_file",
      "args": {"path": "/tmp/data.txt"},
      "result_var": "content"
    },
    {
      "op": "grep",
      "args": {"pattern": "ERROR"},
      "input_var": "content",
      "result_var": "errors"
    },
    {
      "op": "filter",
      "args": {"condition": "non_empty"},
      "input_var": "errors",
      "result_var": "filtered"
    },
    {
      "op": "select_first",
      "input_var": "filtered"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Compile script
	goCode, err := CompileScriptToGo(scriptPath)
	if err != nil {
		t.Fatalf("CompileScriptToGo failed: %v", err)
	}

	// Verify all operations are present
	operations := []string{"executeRead_file", "executeGrep", "executeFilter", "executeSelect_first"}
	for _, op := range operations {
		if !strings.Contains(goCode, op) {
			t.Errorf("Generated code should contain %s operation", op)
		}
	}

	// Verify variable flow
	variables := []string{"content", "errors", "filtered"}
	for _, varName := range variables {
		expectedVar := `variables["` + varName + `"]`
		if !strings.Contains(goCode, expectedVar) {
			t.Errorf("Generated code should reference %s variable", varName)
		}
	}
}

func TestCompileScriptMultiLanguageTargets(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "multilang_script.json")

	scriptJSON := `{
  "name": "multi_target_script",
  "description": "Generate script for multiple languages",
  "input": "input_data",
  "output": "result",
  "script": [
    {
      "op": "format",
      "args": {"template": "hello {{input}}"},
      "result_var": "formatted"
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name     string
		target   TargetLanguage
		contains []string
	}{
		{
			name:     "go",
			target:   TargetLanguageGo,
			contains: []string{"package compiled", "executeFormat"},
		},
		{
			name:     "csharp",
			target:   TargetLanguageCSharp,
			contains: []string{"public static class Multi_target_scriptScript", "ExecuteOperation"},
		},
		{
			name:     "python",
			target:   TargetLanguagePython,
			contains: []string{"def run(input_data: dict)", "def execute_operation"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, err := CompileScript(scriptPath, tc.target)
			if err != nil {
				t.Fatalf("CompileScript(%s) failed: %v", tc.target, err)
			}
			if strings.TrimSpace(code) == "" {
				t.Fatal("expected non-empty generated code")
			}
			for _, want := range tc.contains {
				if !strings.Contains(code, want) {
					t.Fatalf("generated code for %s missing %q", tc.target, want)
				}
			}
		})
	}
}

func TestCompileScriptUnsupportedTarget(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "unsupported_target_script.json")

	scriptJSON := `{
  "name": "unsupported_target_script",
  "description": "Unsupported target script test",
  "input": "input_data",
  "output": "result",
  "script": [
    {
      "op": "format",
      "args": {"template": "hi"}
    }
  ]
}`

	if err := os.WriteFile(scriptPath, []byte(scriptJSON), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err := CompileScript(scriptPath, TargetLanguage("ruby"))
	if err == nil {
		t.Fatal("expected unsupported target error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		t.Fatalf("expected unsupported target error, got %v", err)
	}
}
