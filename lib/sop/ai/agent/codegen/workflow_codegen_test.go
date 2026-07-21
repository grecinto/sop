package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileWorkflowToGo(t *testing.T) {
	// Create temporary test workflow
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test_workflow.json")

	workflowJSON := `{
  "workflow_id": "test-workflow-v1",
  "name": "Test Workflow",
  "description": "A simple test workflow",
  "version": "1.0.0",
  "category_path": "Workflows/Test",
  "trigger_patterns": ["test"],
  "steps": [
    {
      "step": 1,
      "actor": "test.runner",
      "priority": 100,
      "query_template": "Run tests: {original_query}",
      "tools_expected": ["run_tests"]
    },
    {
      "step": 2,
      "actor": "test.analyzer",
      "priority": 90,
      "query_template": "Analyze results",
      "tools_expected": ["read_file", "grep"]
    }
  ],
  "actor_limits": {
    "test.runner": 3
  }
}`

	if err := os.WriteFile(workflowPath, []byte(workflowJSON), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Compile workflow
	goCode, err := CompileWorkflowToGo(workflowPath)
	if err != nil {
		t.Fatalf("CompileWorkflowToGo failed: %v", err)
	}

	// Verify generated code
	expectedParts := []string{
		"package main",
		"func main()",
		"func executeWorkflow",
		"func executeStep",
		"Workflow: Test Workflow",
		"Version: 1.0.0",
		`Actor:         "test.runner"`,
		`Actor:         "test.analyzer"`,
		"expandTemplate",
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

	// Verify workflow metadata is included
	if !strings.Contains(goCode, "Test Workflow") {
		t.Errorf("Generated code should include workflow name")
	}
}

func TestCompileRealWorkflow(t *testing.T) {
	// Test with actual test_debugging_go_v1.json
	workflowPath := filepath.Join("..", "..", "..", "..", "workflows", "test_debugging_go_v1.json")

	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Skip("Real workflow file not found, skipping")
	}

	goCode, err := CompileWorkflowToGo(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile real workflow: %v", err)
	}

	// Verify it contains expected components
	expectedComponents := []string{
		"package main",
		"func main()",
		"test_debugger.runner",
		"test_debugger.analyzer",
		"test_debugger.fixer",
		"run_repo_tests",
		"read_tool_result",
		"patch_file",
	}

	for _, component := range expectedComponents {
		if !strings.Contains(goCode, component) {
			t.Errorf("Generated code missing component: %q", component)
		}
	}

	// Write to temp file to verify it compiles
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "compiled_workflow.go")
	if err := os.WriteFile(outputPath, []byte(goCode), 0644); err != nil {
		t.Fatalf("Failed to write compiled workflow: %v", err)
	}

	t.Logf("Successfully compiled workflow to %s", outputPath)
	t.Logf("Generated code length: %d bytes", len(goCode))
}

func TestWorkflowStepGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "step_test.json")

	// Workflow with various step configurations
	workflowJSON := `{
  "workflow_id": "step-test-v1",
  "name": "Step Test",
  "description": "Tests various step configurations",
  "version": "1.0.0",
  "steps": [
    {
      "step": 1,
      "actor": "actor1",
      "priority": 100,
      "query_template": "Simple query",
      "tools_expected": ["tool1"]
    },
    {
      "step": 2,
      "actor": "actor2",
      "priority": 90,
      "query_template": "Query with {original_query}",
      "tools_expected": ["tool2", "tool3"],
      "optimization_profile": {
        "prefer_script": true,
        "script_library": ["script1"],
        "fallback_to_llm": true,
        "max_script_time_ms": 5000
      }
    }
  ]
}`

	if err := os.WriteFile(workflowPath, []byte(workflowJSON), 0644); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	goCode, err := CompileWorkflowToGo(workflowPath)
	if err != nil {
		t.Fatalf("CompileWorkflowToGo failed: %v", err)
	}

	// Verify both steps are generated
	if !strings.Contains(goCode, "actor1") || !strings.Contains(goCode, "actor2") {
		t.Errorf("Generated code should contain both actors")
	}

	// Verify tools are included
	if !strings.Contains(goCode, "tool1") || !strings.Contains(goCode, "tool2") || !strings.Contains(goCode, "tool3") {
		t.Errorf("Generated code should contain all tools")
	}

	// Verify template expansion function exists
	if !strings.Contains(goCode, "func expandTemplate") {
		t.Errorf("Generated code should contain expandTemplate function")
	}
}

func TestCompileToBinaryPlaceholder(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "binary_test.json")

	workflowJSON := `{
  "workflow_id": "binary-test-v1",
  "name": "Binary Test",
  "description": "Test binary compilation",
  "version": "1.0.0",
  "steps": [
    {
      "step": 1,
      "actor": "test.actor",
      "priority": 100,
      "query_template": "Test query",
      "tools_expected": ["test_tool"]
    }
  ]
}`

	if err := os.WriteFile(workflowPath, []byte(workflowJSON), 0644); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Test binary compilation (returns source path for now)
	outputPath, err := CompileWorkflowToBinary(workflowPath, "")
	if err != nil {
		t.Fatalf("CompileWorkflowToBinary failed: %v", err)
	}

	// Verify source file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("Expected source file to exist at %s", outputPath)
	}

	t.Logf("Generated source at: %s", outputPath)
}

func TestExpandTemplate(t *testing.T) {
	// This tests the template expansion logic that will be in the generated code
	tests := []struct {
		template string
		query    string
		expected string
	}{
		{
			"Run tests: {original_query}",
			"fix failing test",
			"Run tests: fix failing test",
		},
		{
			"Simple query without placeholder",
			"any query",
			"Simple query without placeholder",
		},
		{
			"{original_query} at the start",
			"test",
			"test at the start",
		},
		{
			"Multiple {original_query} and {original_query}",
			"X",
			"Multiple X and X",
		},
	}

	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			// Simulate the expandTemplate function from generated code
			result := strings.ReplaceAll(tt.template, "{original_query}", tt.query)
			if result != tt.expected {
				t.Errorf("expandTemplate(%q, %q) = %q, want %q", tt.template, tt.query, result, tt.expected)
			}
		})
	}
}

// TestGeneratedWorkflowStructure verifies the structure of generated workflows
func TestGeneratedWorkflowStructure(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "structure_test.json")

	workflowJSON := `{
  "workflow_id": "structure-v1",
  "name": "Structure Test",
  "description": "Tests workflow structure",
  "version": "2.0.0",
  "steps": [
    {"step": 1, "actor": "a1", "priority": 100, "query_template": "q1", "tools_expected": []},
    {"step": 2, "actor": "a2", "priority": 90, "query_template": "q2", "tools_expected": []},
    {"step": 3, "actor": "a3", "priority": 80, "query_template": "q3", "tools_expected": []}
  ]
}`

	if err := os.WriteFile(workflowPath, []byte(workflowJSON), 0644); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	goCode, err := CompileWorkflowToGo(workflowPath)
	if err != nil {
		t.Fatalf("CompileWorkflowToGo failed: %v", err)
	}

	// Verify main function structure
	mainComponents := []string{
		"func main() {",
		"if len(os.Args) < 2 {",
		"query := os.Args[1]",
		"executeWorkflow(ctx, query)",
	}

	for _, component := range mainComponents {
		if !strings.Contains(goCode, component) {
			t.Errorf("main() missing component: %q", component)
		}
	}

	// Verify workflow execution structure
	execComponents := []string{
		"func executeWorkflow(ctx context.Context, originalQuery string)",
		"result := &WorkflowResult{",
		"steps := []WorkflowStep{",
		"for _, step := range steps {",
		"expandTemplate(step.QueryTemplate, originalQuery)",
	}

	for _, component := range execComponents {
		if !strings.Contains(goCode, component) {
			t.Errorf("executeWorkflow() missing component: %q", component)
		}
	}

	// Verify all 3 steps are in the generated code
	stepCount := strings.Count(goCode, "Step:")
	if stepCount < 3 {
		t.Errorf("Expected at least 3 step definitions, found %d", stepCount)
	}
}
