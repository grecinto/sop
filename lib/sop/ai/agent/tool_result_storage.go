package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// toolResultThreshold defines when LLM gets summary vs full output
	// ALL results are written to temp file, but LLM gets full result if <= threshold
	toolResultThreshold = 2 * 1024 // 5KB - triggers summarization for most test outputs

	// toolResultMaxInline defines the summary size for file-backed results
	// We show head + tail of this size total
	toolResultMaxInline = 1536 // 1.5KB (750 bytes head + 750 bytes tail)
)

// ToolResultStorage manages temporary file storage for large tool outputs
// Results are stored in session-scoped directories for isolation and cleanup
type ToolResultStorage struct {
	sessionID string
	baseDir   string // e.g., /tmp/sop-tool-results/{sessionID}/
}

// NewToolResultStorage creates a new storage manager for a session
// Creates the session directory if it doesn't exist
func NewToolResultStorage(sessionID string) (*ToolResultStorage, error) {
	if sessionID == "" {
		sessionID = "default"
	}

	baseDir := filepath.Join(os.TempDir(), "sop-tool-results", sessionID)
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create tool result directory: %w", err)
	}

	return &ToolResultStorage{
		sessionID: sessionID,
		baseDir:   baseDir,
	}, nil
}

// StoreResult stores a tool result, either inline or in a file based on size
// Returns:
//   - inline: The result string (full if small, summary if large)
//   - filePath: Path to full output file (empty if inline)
//   - size: Byte count of original result
//   - error: Any error during storage
func (s *ToolResultStorage) StoreResult(toolName, result string) (inline string, filePath string, size int, err error) {
	// FIRST: Unwrap JSON responses to get the actual content
	// Commands like execute_local_command return {"stdout":"...", "stderr":"", "exit_code":0}
	// We want to store and display the actual stdout content, not the JSON wrapper
	displayResult := unwrapJSONCommandResult(result)
	size = len(displayResult)

	// ALWAYS write to file first (regardless of size)
	resultID := uuid.New().String()[:8] // Short UUID for readability
	fileName := fmt.Sprintf("%s-%s.txt", sanitizeToolName(toolName), resultID)
	filePath = filepath.Join(s.baseDir, fileName)

	if err := os.WriteFile(filePath, []byte(displayResult), 0600); err != nil {
		return "", "", 0, fmt.Errorf("failed to write tool result to file: %w", err)
	}

	// THEN apply 5KB rule: return full result if small, summary if large
	if size <= toolResultThreshold {
		// Small - return full result inline
		return displayResult, filePath, size, nil
	}

	// Large - return summary with guidance for LLM
	summary := s.createSummary(toolName, displayResult, filePath, size)
	return summary, filePath, size, nil
}

// unwrapJSONCommandResult extracts stdout from JSON command responses
// If result is {"stdout":"...", "stderr":"...", "exit_code":0}, extract the stdout
// Otherwise return result as-is
func unwrapJSONCommandResult(result string) string {
	// Quick check - does it look like a JSON command response?
	if !strings.HasPrefix(strings.TrimSpace(result), "{") {
		return result
	}

	var cmdResult struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}

	if err := json.Unmarshal([]byte(result), &cmdResult); err != nil {
		// Not a command result, return as-is
		return result
	}

	// Build readable output from stdout/stderr
	var output strings.Builder
	if cmdResult.Stdout != "" {
		output.WriteString(cmdResult.Stdout)
	}
	if cmdResult.Stderr != "" {
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString(cmdResult.Stderr)
	}

	// If we got something, return it; otherwise return original
	if output.Len() > 0 {
		return output.String()
	}
	return result
}

// createSummary generates a summary for large outputs
// Includes: head + tail content + file path + tool usage recommendations
// Phase 2: For test failures, auto-inlines critical error portions
func (s *ToolResultStorage) createSummary(toolName, result, filePath string, size int) string {
	sizeKB := float64(size) / 1024.0

	// Phase 2: For test failures, extract and inline critical error portions
	if isTestCommand(toolName) && containsFailureIndicators(result) {
		return s.createTestFailureSummary(result, filePath, sizeKB)
	}

	// Standard summary for non-test or passing results
	return s.createStandardSummary(result, filePath, sizeKB, toolName)
}

// createTestFailureSummary creates an enhanced summary for test failures
// that auto-inlines critical error portions (Phase 2)
func (s *ToolResultStorage) createTestFailureSummary(result, filePath string, sizeKB float64) string {
	var sb strings.Builder

	sb.WriteString("⚠️  TEST FAILURE DETECTED\n\n")

	// Extract critical error section (first 50 lines or until first blank line after error)
	criticalSection := extractCriticalErrorSection(result, 50)
	sb.WriteString("CRITICAL ERROR OUTPUT:\n")
	sb.WriteString(strings.Repeat("=", 80))
	sb.WriteString("\n")
	sb.WriteString(criticalSection)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", 80))
	sb.WriteString("\n\n")

	// Show how much was omitted
	omittedBytes := len(result) - len(criticalSection)
	if omittedBytes > 0 {
		sb.WriteString(fmt.Sprintf("[%.1fKB of additional output omitted - full output available in file]\n\n", float64(omittedBytes)/1024.0))
	}

	sb.WriteString(fmt.Sprintf("Full output saved to: %s (%.1fKB)\n\n", filePath, sizeKB))

	sb.WriteString("NEXT STEPS (DO NOT SKIP):\n")
	sb.WriteString("1. ✓ Error captured above - analyze the failure message\n")
	sb.WriteString("2. Read the failing test source code to understand what's being tested\n")
	sb.WriteString("3. Read the implementation code that the test is checking\n")
	sb.WriteString("4. Identify the root cause by matching error to code\n")
	sb.WriteString("5. Apply the fix using patch_file\n")
	sb.WriteString("6. Re-run the test to verify the fix works\n\n")

	sb.WriteString("If you need more context from the test output:\n")
	sb.WriteString(fmt.Sprintf("  • Read specific lines: read_tool_result(file_path=\"%s\", start_line=X, end_line=Y)\n", filePath))
	sb.WriteString(fmt.Sprintf("  • Search patterns: grep_tool_result(file_path=\"%s\", pattern=\"PATTERN\")\n", filePath))

	return sb.String()
}

// createStandardSummary creates a standard head+tail summary for non-test outputs
func (s *ToolResultStorage) createStandardSummary(result, filePath string, sizeKB float64, toolName string) string {
	// Extract head and tail
	headSize := toolResultMaxInline / 2
	tailSize := toolResultMaxInline / 2

	head := result
	tail := ""
	omittedBytes := 0

	if len(result) > toolResultMaxInline {
		head = result[:headSize]
		tail = result[len(result)-tailSize:]
		omittedBytes = len(result) - headSize - tailSize
	}

	var sb strings.Builder

	// Show sample output
	sb.WriteString(head)
	if !strings.HasSuffix(head, "\n") {
		sb.WriteString("\n")
	}

	if omittedBytes > 0 {
		sb.WriteString(fmt.Sprintf("\n... [%.1fKB omitted] ...\n\n", float64(omittedBytes)/1024.0))
	}

	if tail != "" {
		sb.WriteString(tail)
		if !strings.HasSuffix(tail, "\n") {
			sb.WriteString("\n")
		}
	}

	// Guide the LLM on how to analyze the full output
	sb.WriteString("\n---\n")
	sb.WriteString(fmt.Sprintf("Full output: %s (%.1fKB)\n", filePath, sizeKB))
	sb.WriteString("To analyze based on your request:\n")
	sb.WriteString("  • Targeted search: analyze_tool_result(file_path=\"\", operation=\"grep\", pattern=\"PATTERN\")\n")
	sb.WriteString("  • Slice lines: analyze_tool_result(file_path=\"\", operation=\"sed\", start_line=X, end_line=Y)\n")
	sb.WriteString("  • Inspect boundaries: analyze_tool_result(file_path=\"\", operation=\"head\" or \"tail\", max_matches=N)\n")
	sb.WriteString("  • Count content: analyze_tool_result(file_path=\"\", operation=\"wc\")\n")

	return sb.String()
}

// Cleanup removes all stored files for this session
func (s *ToolResultStorage) Cleanup() error {
	return os.RemoveAll(s.baseDir)
}

// GetSessionDir returns the base directory for this session
func (s *ToolResultStorage) GetSessionDir() string {
	return s.baseDir
}

// sanitizeToolName removes characters that might be problematic in filenames
func sanitizeToolName(name string) string {
	// Replace non-alphanumeric with underscore
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)

	// Limit length
	if len(result) > 50 {
		result = result[:50]
	}

	return result
}

func isTestCommand(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return false
	}
	return strings.Contains(name, "test") || strings.Contains(name, "spec")
}

func isBuildCommand(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return false
	}
	return strings.Contains(name, "build") || strings.Contains(name, "compile") || strings.Contains(name, "make")
}

// containsFailureIndicators checks if output contains test/build failure patterns
func containsFailureIndicators(result string) bool {
	lower := strings.ToLower(result)
	// Check for common failure patterns
	return strings.Contains(lower, "fail") ||
		strings.Contains(lower, "error:") ||
		strings.Contains(lower, "panic:") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(result, "exit status 1") ||
		strings.Contains(result, "exit code: 1")
}

// extractCriticalErrorSection extracts the critical error portion from test output
// Returns the first maxLines or until we hit a natural break after the error
func extractCriticalErrorSection(result string, maxLines int) string {
	lines := strings.Split(result, "\n")

	// Find the first line with an error indicator
	errorStartIdx := -1
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fail") ||
			strings.Contains(lower, "error") ||
			strings.Contains(lower, "panic") ||
			strings.Contains(line, "--- FAIL:") ||
			strings.Contains(line, "FAIL\t") {
			errorStartIdx = i
			break
		}
	}

	// If no error found, just return first maxLines
	if errorStartIdx == -1 {
		if len(lines) <= maxLines {
			return result
		}
		return strings.Join(lines[:maxLines], "\n")
	}

	// Include some context before the error (up to 5 lines)
	startIdx := errorStartIdx - 5
	if startIdx < 0 {
		startIdx = 0
	}

	// Include lines until maxLines or we hit a natural break
	endIdx := startIdx + maxLines
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	// Try to find a natural stopping point (blank line after error context)
	blankCount := 0
	for i := errorStartIdx + 1; i < endIdx && i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			blankCount++
			if blankCount >= 2 {
				// Stop after 2 consecutive blank lines
				endIdx = i
				break
			}
		} else {
			blankCount = 0
		}
	}

	return strings.Join(lines[startIdx:endIdx], "\n")
}

// CleanupOldToolResults removes tool result directories older than the given TTL
// Used for background cleanup of abandoned sessions
func CleanupOldToolResults(baseDir string, ttl time.Duration) error {
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "sop-tool-results")
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to clean
		}
		return err
	}

	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(baseDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > ttl {
			_ = os.RemoveAll(dirPath) // Best effort cleanup
		}
	}

	return nil
}

// Phase 2.3: Analysis Tools for Stored Results

// ListStoredResults lists all tool result files in this session
func (s *ToolResultStorage) ListStoredResults() ([]StoredResult, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []StoredResult{}, nil
		}
		return nil, fmt.Errorf("failed to list tool results: %w", err)
	}

	var results []StoredResult
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		filePath := filepath.Join(s.baseDir, entry.Name())
		results = append(results, StoredResult{
			FileName:  entry.Name(),
			FilePath:  filePath,
			Size:      info.Size(),
			Timestamp: info.ModTime(),
		})
	}

	return results, nil
}

// ReadResultLines reads a line range from a stored result file
func (s *ToolResultStorage) ReadResultLines(filePath string, startLine, endLine int) (string, error) {
	// Validate file is in session directory
	if !strings.HasPrefix(filePath, s.baseDir) {
		return "", fmt.Errorf("file path is outside session directory")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read result file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// Adjust line numbers (1-indexed to 0-indexed)
	if startLine < 1 {
		startLine = 1
	}
	if endLine < 1 || endLine > totalLines {
		endLine = totalLines
	}
	if startLine > endLine {
		return "", fmt.Errorf("start line (%d) must be <= end line (%d)", startLine, endLine)
	}

	startIdx := startLine - 1
	endIdx := endLine

	selectedLines := lines[startIdx:endIdx]
	return strings.Join(selectedLines, "\n"), nil
}

// GrepResult searches for a pattern in a stored result file
func (s *ToolResultStorage) GrepResult(filePath, pattern string, maxMatches int) ([]GrepMatch, error) {
	// Validate file is in session directory
	if !strings.HasPrefix(filePath, s.baseDir) {
		return nil, fmt.Errorf("file path is outside session directory")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read result file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var matches []GrepMatch

	for lineNum, line := range lines {
		if strings.Contains(line, pattern) {
			matches = append(matches, GrepMatch{
				LineNumber: lineNum + 1, // 1-indexed
				Content:    line,
			})

			if maxMatches > 0 && len(matches) >= maxMatches {
				break
			}
		}
	}

	return matches, nil
}

// AnalyzeResult performs lightweight line-oriented analysis on a stored result file.
// Supported operations: grep, awk, sed, head, tail, wc, cat.
func (s *ToolResultStorage) AnalyzeResult(filePath, operation, pattern string, startLine, endLine, maxMatches int) (string, error) {
	if !strings.HasPrefix(filePath, s.baseDir) {
		return "", fmt.Errorf("file path is outside session directory")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read result file: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	op := strings.ToLower(strings.TrimSpace(operation))
	switch op {
	case "grep", "awk":
		if strings.TrimSpace(pattern) == "" {
			return "", fmt.Errorf("pattern is required for %s operation", op)
		}
		var matches []string
		for idx, line := range lines {
			if strings.Contains(line, pattern) {
				matches = append(matches, fmt.Sprintf("Line %d: %s", idx+1, line))
				if maxMatches > 0 && len(matches) >= maxMatches {
					break
				}
			}
		}
		if len(matches) == 0 {
			return fmt.Sprintf("No matches found for pattern '%s' in %s", pattern, filepath.Base(filePath)), nil
		}
		return strings.Join(matches, "\n"), nil
	case "sed":
		if startLine < 1 {
			startLine = 1
		}
		if endLine < 1 || endLine > len(lines) {
			endLine = len(lines)
		}
		if startLine > endLine {
			return "", fmt.Errorf("start line (%d) must be <= end line (%d)", startLine, endLine)
		}
		if startLine > len(lines) {
			return "", nil
		}
		selected := lines[startLine-1 : endLine]
		if len(selected) == 0 {
			return "", nil
		}
		return strings.Join(selected, "\n"), nil
	case "head":
		if maxMatches < 1 {
			maxMatches = 20
		}
		if maxMatches > len(lines) {
			maxMatches = len(lines)
		}
		if maxMatches <= 0 {
			return "", nil
		}
		return strings.Join(lines[:maxMatches], "\n"), nil
	case "tail":
		if maxMatches < 1 {
			maxMatches = 20
		}
		if maxMatches > len(lines) {
			maxMatches = len(lines)
		}
		if maxMatches <= 0 {
			return "", nil
		}
		return strings.Join(lines[len(lines)-maxMatches:], "\n"), nil
	case "wc":
		lineCount := len(lines)
		wordCount := 0
		charCount := 0
		for _, line := range lines {
			wordCount += len(strings.Fields(line))
			charCount += len(line)
		}
		return fmt.Sprintf("lines=%d words=%d chars=%d", lineCount, wordCount, charCount), nil
	case "cat":
		return strings.Join(lines, "\n"), nil
	default:
		return "", fmt.Errorf("unsupported operation %q", operation)
	}
}

// StoredResult represents metadata about a stored tool result file
type StoredResult struct {
	FileName  string
	FilePath  string
	Size      int64
	Timestamp time.Time
}

// GrepMatch represents a single matching line from grep search
type GrepMatch struct {
	LineNumber int
	Content    string
}
