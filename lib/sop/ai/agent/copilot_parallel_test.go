package agent

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestDetectParallelSubqueries tests query splitting logic
func TestDetectParallelSubqueries(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected []string
	}{
		{
			name:     "Simple and pattern",
			query:    "run tests and check formatting",
			expected: []string{"run tests", "check formatting"},
		},
		{
			name:     "Comma separated with and",
			query:    "run tests, check formatting, and build docs",
			expected: []string{"run tests", "check formatting", "build docs"},
		},
		{
			name:     "Explicit parallel marker",
			query:    "run tests || check formatting || build docs",
			expected: []string{"run tests", "check formatting", "build docs"},
		},
		{
			name:     "Non-parallelizable query",
			query:    "what is the weather today",
			expected: []string{"what is the weather today"},
		},
		{
			name:     "Single action verb",
			query:    "run tests",
			expected: []string{"run tests"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectParallelSubqueries(tt.query)

			if len(result) != len(tt.expected) {
				t.Errorf("detectParallelSubqueries() got %d queries, want %d", len(result), len(tt.expected))
				t.Logf("Got: %v", result)
				t.Logf("Want: %v", tt.expected)
				return
			}

			for i, q := range result {
				if q != tt.expected[i] {
					t.Errorf("detectParallelSubqueries()[%d] = %q, want %q", i, q, tt.expected[i])
				}
			}
		})
	}
}

// TestAggregateParallelResults tests result aggregation
func TestAggregateParallelResults(t *testing.T) {
	queries := []string{"run tests", "check formatting"}
	results := []string{"Tests passed ✓", "Formatting OK ✓"}
	errors := []error{nil, nil}

	combined := aggregateParallelResults(queries, results, errors)

	if !strings.Contains(combined, "Parallel Execution Complete") {
		t.Error("Expected header not found in combined results")
	}

	if !strings.Contains(combined, "Tests passed") {
		t.Error("Expected test result not found")
	}

	if !strings.Contains(combined, "Formatting OK") {
		t.Error("Expected formatting result not found")
	}
}

// TestIsParallelExecution tests context flag detection
func TestIsParallelExecution(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected bool
	}{
		{
			name:     "Regular context",
			ctx:      context.Background(),
			expected: false,
		},
		{
			name:     "Parallel execution context",
			ctx:      context.WithValue(context.Background(), "parallel_execution_mode", true),
			expected: true,
		},
		{
			name:     "Parallel execution false",
			ctx:      context.WithValue(context.Background(), "parallel_execution_mode", false),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isParallelExecution(tt.ctx)
			if result != tt.expected {
				t.Errorf("isParallelExecution() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestAskParallel_FallbackToSequential tests that single queries fall back to Ask
func TestAskParallel_FallbackToSequential(t *testing.T) {
	// This test verifies that AskParallel falls back to Ask for non-parallel queries
	// Full integration testing would require mocking the LLM, which is out of scope

	query := "what is the weather"
	subQueries := detectParallelSubqueries(query)

	if len(subQueries) != 1 {
		t.Errorf("Expected fallback for non-parallel query, got %d subqueries", len(subQueries))
	}

	if subQueries[0] != query {
		t.Errorf("Expected original query preserved, got %q", subQueries[0])
	}
}

func TestExtractParallelTaskWorkingDir(t *testing.T) {
	query, workingDir := extractParallelTaskWorkingDir("analyze the repo in /tmp/sop-project")
	if query != "analyze the repo" {
		t.Fatalf("expected query to be normalized, got %q", query)
	}
	if workingDir != "/tmp/sop-project" {
		t.Fatalf("expected extracted working dir %q, got %q", "/tmp/sop-project", workingDir)
	}
}

func TestResolveParallelWorkingDirUsesContextDefaultWhenUnset(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyWorkingDir, "/default/repo")
	if got := resolveParallelWorkingDir(ctx, "run tests"); got != "/default/repo" {
		t.Fatalf("expected inherited working dir %q, got %q", "/default/repo", got)
	}
}

func TestResolveParallelWorkingDirFallsBackToProcessCWD(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	if got := resolveParallelWorkingDir(context.Background(), "run tests"); got != cwd {
		t.Fatalf("expected fallback working dir %q, got %q", cwd, got)
	}
}
