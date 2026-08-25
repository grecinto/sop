package codingworkflow

import (
	"encoding/json"
	"fmt"
	log_slog "log/slog"
	"regexp"
	"strings"
)

// StructuredToolResult represents preprocessed, LLM-friendly tool output
type StructuredToolResult struct {
	Type         string                 `json:"type"` // "test_failure", "build_error", "lint_warning", "success"
	Summary      string                 `json:"summary"`
	Data         map[string]interface{} `json:"data,omitempty"`
	RawOutput    string                 `json:"raw_output,omitempty"`    // Truncated or full if small
	FilePath     string                 `json:"file_path,omitempty"`     // Full output location
	NextActions  []string               `json:"next_actions,omitempty"`  // Suggested tools/steps
	ErrorDetails []ErrorDetail          `json:"error_details,omitempty"` // Structured errors
}

// ErrorDetail represents a single failure/error/warning
type ErrorDetail struct {
	Type       string `json:"type"` // "test_failure", "compile_error", "lint_warning"
	Name       string `json:"name,omitempty"`
	Location   string `json:"location,omitempty"` // File path
	Line       int    `json:"line,omitempty"`
	Message    string `json:"message"`
	StackTrace string `json:"stack_trace,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	SourceLink string `json:"source_link,omitempty"` // Link to implementation
}

// ParseTestResult converts raw test output into structured data
// This is called by coding capability tools to preprocess test results
func ParseTestResult(toolName, rawOutput string) *StructuredToolResult {
	// Determine output type and apply appropriate parser
	switch {
	case isTestCommand(toolName):
		log_slog.Info("[ParseTestResult] Detected test command", "tool", toolName, "output_len", len(rawOutput))
		return parseTestOutput(rawOutput)
	case isBuildCommand(toolName):
		log_slog.Info("[ParseTestResult] Detected build command", "tool", toolName)
		return parseBuildOutput(rawOutput)
	case isLintCommand(toolName):
		log_slog.Info("[ParseTestResult] Detected lint command", "tool", toolName)
		return parseLintOutput(rawOutput)
	default:
		log_slog.Info("[ParseTestResult] No parser for tool", "tool", toolName)
		return nil // No structured parsing available
	}
}

// parseTestOutput extracts structured data from test failures
func parseTestOutput(rawOutput string) *StructuredToolResult {
	hasFailures := containsFailureIndicators(rawOutput)
	preview := rawOutput
	if len(preview) > 100 {
		preview = preview[:100]
	}
	log_slog.Info("[parseTestOutput] Checking for failures", "has_failures", hasFailures, "output_preview", preview)

	if !hasFailures {
		// Tests passed - simple success result
		log_slog.Info("[parseTestOutput] No failures detected - returning success")
		return &StructuredToolResult{
			Type:    "test_success",
			Summary: "All tests passed",
			Data: map[string]interface{}{
				"status": "passed",
			},
		}
	}

	// Detect language and route to appropriate parser
	// Each language has unique output patterns we can detect
	var failures []ErrorDetail
	var detectedFormat string

	switch {
	case detectGoTest(rawOutput):
		detectedFormat = "go"
		failures = extractGoTestFailures(rawOutput)
	case detectPytest(rawOutput):
		detectedFormat = "pytest"
		failures = extractPytestFailures(rawOutput)
	case detectJest(rawOutput):
		detectedFormat = "jest"
		failures = extractJestFailures(rawOutput)
	case detectRustTest(rawOutput):
		detectedFormat = "rust"
		failures = extractRustTestFailures(rawOutput)
	default:
		// Unknown format - return nil to use text fallback
		log_slog.Info("[parseTestOutput] Unknown test format - using text fallback")
		return nil
	}

	log_slog.Info("[parseTestOutput] Detected test format", "format", detectedFormat, "failures_found", len(failures))

	if len(failures) == 0 {
		// Has failure indicators but couldn't parse - return nil to use text fallback
		log_slog.Info("[parseTestOutput] Could not extract failures from output - using text fallback")
		return nil
	}

	result := &StructuredToolResult{
		Type:         "test_failure",
		Summary:      formatTestFailureSummary(failures),
		ErrorDetails: failures,
		NextActions: []string{
			"READ the test file and implementation files using the file paths in error_details",
			"Analyze the error messages to understand what's failing",
			"Identify the root cause in the implementation code",
			"Generate a patch to fix the issue",
			"Apply the patch and re-run tests to verify",
		},
	}

	// Add aggregate data
	result.Data = map[string]interface{}{
		"failed_count":  len(failures),
		"first_failure": failures[0].Name,
		"first_package": failures[0].Location,
	}

	return result
}

// extractGoTestFailures parses Go test output for failing tests
func extractGoTestFailures(output string) []ErrorDetail {
	var failures []ErrorDetail
	lines := strings.Split(output, "\n")

	// Regex patterns for Go test failures
	failTestPattern := regexp.MustCompile(`^\s*--- FAIL: (\S+) \(`)
	failPackagePattern := regexp.MustCompile(`^FAIL\s+(\S+)`)
	errorLinePattern := regexp.MustCompile(`^\s+(\S+):(\d+):`)

	var currentTest string
	var currentPackage string
	var currentFilePath string
	var currentError strings.Builder
	var currentLine int

	for i, line := range lines {
		// Detect "--- FAIL: TestName"
		if matches := failTestPattern.FindStringSubmatch(line); matches != nil {
			// Save previous failure if exists
			if currentTest != "" {
				errorMsg := strings.TrimSpace(currentError.String())
				if errorMsg == "" {
					errorMsg = "Test failed (no detailed error message captured)"
				}
				location := currentFilePath
				if location == "" {
					location = currentPackage // Fallback to package if no file found
				}
				failures = append(failures, ErrorDetail{
					Type:     "test_failure",
					Name:     currentTest,
					Location: location,
					Line:     currentLine,
					Message:  errorMsg,
				})
			}

			// Start new failure
			currentTest = matches[1]
			currentError.Reset()
			currentFilePath = ""
			currentLine = 0
			continue
		}

		// Detect "FAIL	package/path"
		if matches := failPackagePattern.FindStringSubmatch(line); matches != nil {
			currentPackage = matches[1]
			continue
		}

		// Detect error location "    file.go:123:"
		if currentTest != "" {
			if matches := errorLinePattern.FindStringSubmatch(line); matches != nil {
				currentFilePath = matches[1] // Capture file path
				currentLine, _ = parseInt(matches[2])
			}

			// Collect error message lines (indented lines under --- FAIL)
			if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
				if currentError.Len() > 0 {
					currentError.WriteString("\n")
				}
				currentError.WriteString(strings.TrimSpace(line))
			}

			// Stop collecting when we hit next test or end
			if i+1 < len(lines) {
				nextLine := lines[i+1]
				if strings.HasPrefix(nextLine, "--- FAIL:") || strings.HasPrefix(nextLine, "--- PASS:") || strings.HasPrefix(nextLine, "=== RUN") {
					if currentTest != "" {
						errorMsg := strings.TrimSpace(currentError.String())
						if errorMsg == "" {
							errorMsg = "Test failed (no detailed error message captured)"
						}
						location := currentFilePath
						if location == "" {
							location = currentPackage // Fallback to package if no file found
						}
						failures = append(failures, ErrorDetail{
							Type:     "test_failure",
							Name:     currentTest,
							Location: location,
							Line:     currentLine,
							Message:  errorMsg,
						})
						currentTest = ""
						currentError.Reset()
					}
				}
			}
		}
	}

	// Save final failure
	if currentTest != "" {
		errorMsg := strings.TrimSpace(currentError.String())
		if errorMsg == "" {
			errorMsg = "Test failed (no detailed error message captured)"
		}
		location := currentFilePath
		if location == "" {
			location = currentPackage // Fallback to package if no file found
		}
		failures = append(failures, ErrorDetail{
			Type:     "test_failure",
			Name:     currentTest,
			Location: location,
			Line:     currentLine,
			Message:  errorMsg,
		})
	}

	return failures
}

// formatTestFailureSummary creates a one-line summary
func formatTestFailureSummary(failures []ErrorDetail) string {
	if len(failures) == 0 {
		return "No test failures"
	}
	if len(failures) == 1 {
		return "1 test failed: " + failures[0].Name
	}
	return fmt.Sprintf("%d tests failed, first failure: %s", len(failures), failures[0].Name)
}

// parseBuildOutput extracts compilation errors
func parseBuildOutput(rawOutput string) *StructuredToolResult {
	// TODO: Parse compilation errors
	// Look for patterns like "file.go:123:45: error: ..."
	return nil
}

// parseLintOutput extracts lint warnings
func parseLintOutput(rawOutput string) *StructuredToolResult {
	// TODO: Parse lint output
	// Different linters have different formats (golangci-lint, eslint, etc.)
	return nil
}

// Helper: Check if tool is a build command
func isBuildCommand(toolName string) bool {
	lower := strings.ToLower(toolName)
	return strings.Contains(lower, "build") || strings.Contains(lower, "compile")
}

// Helper: Check if tool is a test command
func isTestCommand(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return false
	}
	return strings.Contains(name, "test") || strings.Contains(name, "spec")
}

// Helper: Check if tool is a lint command
func isLintCommand(toolName string) bool {
	lower := strings.ToLower(toolName)
	return strings.Contains(lower, "lint") || strings.Contains(lower, "check")
}

// Helper: Check if output contains failure indicators
func containsFailureIndicators(result string) bool {
	lower := strings.ToLower(result)
	return strings.Contains(lower, "fail") ||
		strings.Contains(lower, "error:") ||
		strings.Contains(lower, "panic:") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(result, "exit status 1") ||
		strings.Contains(result, "exit code: 1")
}

// Helper: Parse integer safely
func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// ============================================================================
// Language Detection - Identify test framework from output patterns
// ============================================================================

// detectGoTest checks if output is from Go's testing package
func detectGoTest(output string) bool {
	return strings.Contains(output, "--- FAIL:") ||
		strings.Contains(output, "=== RUN") ||
		(strings.Contains(output, "FAIL\t") && strings.Contains(output, "coverage:"))
}

// detectPytest checks if output is from Python pytest
func detectPytest(output string) bool {
	return strings.Contains(output, "FAILED ") && strings.Contains(output, ".py::") ||
		strings.Contains(output, "test session starts") ||
		strings.Contains(output, "collected ") && strings.Contains(output, "items")
}

// detectJest checks if output is from JavaScript Jest
func detectJest(output string) bool {
	return strings.Contains(output, "FAIL ") && (strings.Contains(output, ".test.js") || strings.Contains(output, ".test.ts")) ||
		strings.Contains(output, "● ") || // Jest failure marker
		strings.Contains(output, "Tests:") && strings.Contains(output, "failed,")
}

// detectRustTest checks if output is from Rust's test framework
func detectRustTest(output string) bool {
	return strings.Contains(output, "test result: FAILED.") ||
		strings.Contains(output, "---- ") && strings.Contains(output, " stdout ----")
}

// ============================================================================
// Language-Specific Parsers - Extract failures for each test framework
// ============================================================================

// extractPytestFailures parses Python pytest output
// Format: "FAILED tests/test_foo.py::test_bar - AssertionError: ..."
func extractPytestFailures(output string) []ErrorDetail {
	var failures []ErrorDetail
	lines := strings.Split(output, "\n")

	failedPattern := regexp.MustCompile(`^FAILED\s+(.+?)::(.+?)\s+-\s+(.+)$`)

	for _, line := range lines {
		if matches := failedPattern.FindStringSubmatch(line); matches != nil {
			failures = append(failures, ErrorDetail{
				Type:     "test_failure",
				Name:     matches[2], // test function name
				Location: matches[1], // file path
				Message:  matches[3], // error message
			})
		}
	}

	return failures
}

// extractJestFailures parses JavaScript Jest output
// Format: "● TestSuite › test name\n    Error: expected..."
func extractJestFailures(output string) []ErrorDetail {
	var failures []ErrorDetail
	lines := strings.Split(output, "\n")

	testPattern := regexp.MustCompile(`^  ● (.+?) › (.+)$`)

	var currentSuite, currentTest string
	var errorMessage strings.Builder

	for i, line := range lines {
		if matches := testPattern.FindStringSubmatch(line); matches != nil {
			// Save previous failure
			if currentTest != "" && errorMessage.Len() > 0 {
				failures = append(failures, ErrorDetail{
					Type:     "test_failure",
					Name:     currentTest,
					Location: currentSuite,
					Message:  strings.TrimSpace(errorMessage.String()),
				})
			}

			currentSuite = matches[1]
			currentTest = matches[2]
			errorMessage.Reset()
		} else if currentTest != "" && strings.HasPrefix(line, "    ") {
			// Collect error lines (indented under test name)
			if errorMessage.Len() > 0 {
				errorMessage.WriteString("\n")
			}
			errorMessage.WriteString(strings.TrimSpace(line))
		} else if currentTest != "" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "  ●") {
			// Next test starting - save current
			if errorMessage.Len() > 0 {
				failures = append(failures, ErrorDetail{
					Type:     "test_failure",
					Name:     currentTest,
					Location: currentSuite,
					Message:  strings.TrimSpace(errorMessage.String()),
				})
			}
			currentTest = ""
			errorMessage.Reset()
		}
	}

	// Save final failure
	if currentTest != "" && errorMessage.Len() > 0 {
		failures = append(failures, ErrorDetail{
			Type:     "test_failure",
			Name:     currentTest,
			Location: currentSuite,
			Message:  strings.TrimSpace(errorMessage.String()),
		})
	}

	return failures
}

// extractRustTestFailures parses Rust test output
// Format: "test tests::test_foo ... FAIL"
func extractRustTestFailures(output string) []ErrorDetail {
	var failures []ErrorDetail
	lines := strings.Split(output, "\n")

	failPattern := regexp.MustCompile(`^test (.+?) \.\.\. FAIL$`)

	for _, line := range lines {
		if matches := failPattern.FindStringSubmatch(line); matches != nil {
			failures = append(failures, ErrorDetail{
				Type:    "test_failure",
				Name:    matches[1],
				Message: "Test failed - see full output for details",
			})
		}
	}

	return failures
}

// CreateEnrichedTestResult combines structured data with raw output
func CreateEnrichedTestResult(structured *StructuredToolResult, rawOutput, filePath string, maxInlineSize int) string {
	log_slog.Info("[CreateEnrichedTestResult] Called",
		"structured_type", structured.Type,
		"error_count", len(structured.ErrorDetails),
		"file_path", filePath,
		"raw_len", len(rawOutput),
		"max_inline", maxInlineSize)

	// Add file path and raw output to structured result
	structured.FilePath = filePath
	if len(rawOutput) <= maxInlineSize {
		structured.RawOutput = rawOutput
	} else {
		// Truncate raw output in structured result, point to file
		structured.RawOutput = rawOutput[:1024] + "\n...[truncated, see file_path for full output]..."
	}

	// Return as pretty JSON
	jsonBytes, err := json.MarshalIndent(structured, "", "  ")
	if err != nil {
		// Fallback to raw output if JSON serialization fails
		log_slog.Info("[CreateEnrichedTestResult] JSON marshal failed", "error", err)
		return rawOutput
	}

	jsonStr := string(jsonBytes)
	log_slog.Info("[CreateEnrichedTestResult] JSON created", "json_len", len(jsonStr))
	return jsonStr
}
