package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sharedcode/sop/ai"
)

const (
	// toolResultThreshold defines when LLM gets summary vs full output
	// ALL results are written to temp file, but LLM gets full result if <= threshold
	toolResultThreshold = 16 * 1024 // 16KB - most tool outputs fit inline, only massive results get file-backed

	// toolResultMaxInline defines the summary size for file-backed results
	// We show head + tail of this size total
	toolResultMaxInline = 2048 // 2KB preview (1KB head + 1KB tail) for better context
)

// ToolResultStorage manages temporary file storage for large tool outputs
// Results are stored in session-scoped directories for isolation and cleanup
type ToolResultStorage struct {
	sessionID    string
	baseDir      string // e.g., /tmp/sop-tool-results/{sessionID}/
	Preprocessor interface {
		PreprocessResult(toolName, rawOutput, filePath string, maxInlineSize int) string
	} // Explicit preprocessor, not from context
}

// NewToolResultStorage creates a new storage manager for a session
// Creates the session directory if it doesn't exist
// preprocessor is optional - pass nil if no domain-specific preprocessing needed
func NewToolResultStorage(sessionID string, preprocessor interface {
	PreprocessResult(toolName, rawOutput, filePath string, maxInlineSize int) string
}) (*ToolResultStorage, error) {
	if sessionID == "" {
		sessionID = "default"
	}

	baseDir := filepath.Join(os.TempDir(), "sop-tool-results", sessionID)
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create tool result directory: %w", err)
	}

	return &ToolResultStorage{
		sessionID:    sessionID,
		baseDir:      baseDir,
		Preprocessor: preprocessor,
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

	// Apply 4KB rule: return full result if small, summary if large
	// Note: Capability-specific preprocessing happens in wrapToolResultWithStorage
	// which has access to context and capabilities
	// Apply 4KB rule: return full result if small, summary if large
	if size <= toolResultThreshold {
		// Small - return full result inline
		return displayResult, filePath, size, nil
	}

	// Large - return summary with guidance for LLM
	summary := s.createSummary(toolName, displayResult, filePath, size)
	return summary, filePath, size, nil
}

func wrapToolResultWithStorage(ctx context.Context, toolName, rawResult string) (string, error) {
	storage := getOrCreateToolResultStorage(ctx)
	if storage == nil {
		return rawResult, nil
	}
	if shouldBypassToolResultStorage(toolName) {
		return rawResult, nil
	}

	hadEnvelope := isToolResultEnvelope(rawResult)
	resultText, hint, _, _ := unwrapToolResultEnvelopeFull(rawResult)

	// PHASE 3: Try capability-specific preprocessing before standard storage
	// This allows coding capability to convert test failures to structured JSON
	// Use explicit preprocessor from storage, not context (follows explicit parameter passing principle)
	preprocessed := tryPreprocessing(storage, toolName, resultText, "", 0)
	if preprocessed != "" {
		// Capability provided structured result - still store to file but return structured
		_, filePath, size, err := storage.StoreResult(toolName, resultText)
		if err != nil {
			return "", err
		}

		// Return the preprocessed structured result
		envelope := ai.ToolResultEnvelope{
			ToolResult:   json.RawMessage(preprocessed), // Structured result as raw JSON
			ProgressHint: hint,
		}
		if filePath != "" {
			envelope.FileMetadata = &ai.ToolFileMetadata{
				FilePath: filePath,
				FileSize: int64(size),
			}
		}
		wrapped, err := json.Marshal(envelope)
		if err != nil {
			return preprocessed, nil
		}
		return string(wrapped), nil
	}

	// FALLBACK: Standard storage flow for unknown formats
	inline, filePath, size, err := storage.StoreResult(toolName, resultText)
	if err != nil {
		return "", err
	}
	if size <= toolResultThreshold && hint == nil && !hadEnvelope {
		return inline, nil
	}

	envelope := ai.ToolResultEnvelope{
		ToolResult:   normalizeToolResultPayload(inline),
		ProgressHint: hint,
	}
	if filePath != "" && size > toolResultThreshold {
		envelope.FileMetadata = &ai.ToolFileMetadata{
			FilePath: filePath,
			FileSize: int64(size),
		}
	}

	wrapped, err := json.Marshal(envelope)
	if err != nil {
		return inline, nil
	}
	return string(wrapped), nil
}

func shouldBypassToolResultStorage(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "list_tool_results", "read_tool_result", "grep_tool_result", "analyze_tool_result":
		return true
	default:
		return false
	}
}

func isToolResultEnvelope(rawResult string) bool {
	trimmed := strings.TrimSpace(rawResult)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return false
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return false
	}
	_, ok := payload["tool_result"]
	return ok
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

	// Extract the most relevant failure block(s), even when the decisive failure
	// appears deep in the output after many passing tests.
	criticalSection := extractCriticalFailureHighlights(result, 50)
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
	sb.WriteString("2. Identify the failing test name/package from the failure block above\n")
	sb.WriteString("3. Read the failing test source code to understand what's being tested\n")
	sb.WriteString("4. Read the implementation code that the test is checking\n")
	sb.WriteString("5. Identify the root cause by matching error to code\n")
	sb.WriteString("6. Apply the fix using patch_file\n")
	sb.WriteString("7. Re-run the test to verify the fix works\n\n")

	sb.WriteString("If you need more context from the test output:\n")
	sb.WriteString(fmt.Sprintf("  • Read specific lines: read_tool_result(file_path=\"%s\", start_line=X, end_line=Y)\n", filePath))
	sb.WriteString(fmt.Sprintf("  • Search patterns: grep_tool_result(file_path=\"%s\", pattern=\"PATTERN\")\n", filePath))
	sb.WriteString("  • After finding a failing test name, inspect that test file and the implementation it exercises before concluding anything\n")

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

	// Smart, context-aware recommendation instead of generic menu
	sb.WriteString("\n---\n")
	sb.WriteString(fmt.Sprintf("Full output: %s (%.1fKB)\n", filePath, sizeKB))
	
	recommendation := s.getSmartRecommendation(toolName, head, filePath, sizeKB)
	if recommendation != "" {
		sb.WriteString(recommendation)
	}

	return sb.String()
}

// getSmartRecommendation provides context-aware next action based on tool and content
func (s *ToolResultStorage) getSmartRecommendation(toolName, preview, filePath string, sizeKB float64) string {
	lower := strings.ToLower(preview)
	
	// Repo inspection tools with massive output
	if toolName == "inspect_repo" || toolName == "list_changed_files" {
		if sizeKB > 100 {
			return fmt.Sprintf("⚠️  Large repository scan (%.0fKB). This output is too broad to analyze directly.\n"+
				"Next: Focus your query on specific files, directories, or patterns instead of processing all changes.\n", sizeKB)
		}
		return fmt.Sprintf("Review changed files in the preview above, then read specific files with read_file.\n")
	}
	
	// Git log - usually want to find specific commits
	if strings.Contains(toolName, "git_log") {
		return fmt.Sprintf("To find specific commits: grep_tool_result(file_path=\"%s\", pattern=\"your-search-term\")\n", filePath)
	}
	
	// Git diff - errors or specific changes
	if strings.Contains(toolName, "git_diff") || strings.Contains(toolName, "git_status") {
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") {
			return fmt.Sprintf("⚠️  Git operation encountered errors. Review the output above for details.\n")
		}
		return "Review the changes shown above. To see specific sections: read_tool_result with line ranges.\n"
	}
	
	// Build/compilation output - look for errors
	if strings.Contains(toolName, "build") || strings.Contains(toolName, "compile") {
		if strings.Contains(lower, "error") || strings.Contains(lower, "fail") {
			return fmt.Sprintf("Build errors detected. Find them: grep_tool_result(file_path=\"%s\", pattern=\"error|fail\")\n", filePath)
		}
		return "Build output captured. Review preview above for any warnings or errors.\n"
	}
	
	// Directory listings
	if strings.Contains(toolName, "list_directory") || strings.Contains(toolName, "ls") {
		lineCount := strings.Count(preview, "\n")
		if lineCount > 100 {
			return fmt.Sprintf("Large directory (%d+ items). Consider narrowing your search or filtering by file type.\n", lineCount)
		}
		return "Directory contents shown above.\n"
	}
	
	// Search results
	if strings.Contains(toolName, "search") || strings.Contains(toolName, "grep") || strings.Contains(toolName, "find") {
		matchCount := strings.Count(preview, "\n")
		if matchCount > 50 {
			return fmt.Sprintf("Found %d+ matches. Consider refining your search pattern for better results.\n", matchCount)
		}
		if matchCount == 0 {
			return "No matches found. Try a different search pattern or broaden your criteria.\n"
		}
		return fmt.Sprintf("Found %d match(es). Review results above.\n", matchCount)
	}
	
	// Command execution output
	if strings.Contains(toolName, "execute") || strings.Contains(toolName, "command") {
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "fail") {
			return fmt.Sprintf("Command failed. Find errors: grep_tool_result(file_path=\"%s\", pattern=\"error|fail|fatal\")\n", filePath)
		}
		return "Command output captured. Review the preview above for results.\n"
	}
	
	// Generic fallback - but still only ONE recommendation, not six
	if sizeKB > 50 {
		return fmt.Sprintf("Large output (%.0fKB). To analyze: grep_tool_result(file_path=\"%s\", pattern=\"your-search-term\")\n", sizeKB, filePath)
	}
	
	return fmt.Sprintf("Output saved. To analyze: grep_tool_result(file_path=\"%s\", pattern=\"PATTERN\") or read_tool_result for specific lines.\n", filePath)
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

// tryPreprocessing uses the storage's explicit preprocessor (not context)
// This follows the explicit parameter passing principle: never use context for application state
func tryPreprocessing(storage *ToolResultStorage, toolName, rawOutput, filePath string, size int) string {
	if storage == nil {
		return ""
	}
	if storage.Preprocessor == nil {
		return ""
	}

	// Call the explicit preprocessor
	return storage.Preprocessor.PreprocessResult(toolName, rawOutput, filePath, toolResultThreshold)
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

// extractCriticalFailureHighlights prefers explicit failure blocks over the first
// generic error-looking line so the model sees the decisive failing test even
// when it appears deep in large test logs.
func extractCriticalFailureHighlights(result string, maxLines int) string {
	lines := strings.Split(result, "\n")
	markers := []int{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--- FAIL:") || strings.HasPrefix(trimmed, "panic:") || strings.HasPrefix(trimmed, "FAIL\t") || trimmed == "FAIL" {
			markers = append(markers, i)
		}
	}
	if len(markers) == 0 {
		return extractCriticalErrorSection(result, maxLines)
	}

	startMarker := markers[len(markers)-1]
	for _, idx := range markers {
		if strings.HasPrefix(strings.TrimSpace(lines[idx]), "--- FAIL:") {
			startMarker = idx
			break
		}
	}

	startIdx := startMarker - 5
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + maxLines
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	for i := startMarker + 1; i < len(lines) && i < endIdx; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "FAIL\t") {
			endIdx = i + 1
			break
		}
		if strings.HasPrefix(trimmed, "ok\t") || (strings.HasPrefix(trimmed, "=== RUN") && i > startMarker+1) {
			endIdx = i
			break
		}
	}

	section := strings.TrimSpace(strings.Join(lines[startIdx:endIdx], "\n"))
	if section != "" {
		return section
	}
	return extractCriticalErrorSection(result, maxLines)
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

func (s *ToolResultStorage) resolveStoredResultPath(filePath string) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return "", fmt.Errorf("file path is required")
	}

	candidate := trimmed
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(s.baseDir, candidate)
	}

	if resolved, ok, err := s.resolveStoredResultExact(candidate); err != nil {
		return "", err
	} else if ok {
		return resolved, nil
	}

	if resolved, ok, err := s.resolveStoredResultGlob(candidate); err != nil {
		return "", err
	} else if ok {
		return resolved, nil
	}

	baseName := filepath.Base(trimmed)
	if resolved, ok, err := s.resolveStoredResultExact(filepath.Join(s.baseDir, baseName)); err != nil {
		return "", err
	} else if ok {
		return resolved, nil
	}

	if strings.HasSuffix(baseName, ".txt") {
		stem := strings.TrimSuffix(baseName, ".txt")
		if resolved, ok, err := s.resolveStoredResultGlob(filepath.Join(s.baseDir, stem+"-*.txt")); err != nil {
			return "", err
		} else if ok {
			return resolved, nil
		}
	}

	return "", nil
}

func (s *ToolResultStorage) resolveStoredResultExact(candidate string) (string, bool, error) {
	cleaned := filepath.Clean(candidate)
	if !strings.HasPrefix(cleaned, s.baseDir) {
		return "", false, fmt.Errorf("file path is outside session directory")
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to stat result file: %w", err)
	}
	if info.IsDir() {
		return "", false, nil
	}
	return cleaned, true, nil
}

func (s *ToolResultStorage) resolveStoredResultGlob(pattern string) (string, bool, error) {
	prefix := pattern
	if idx := strings.IndexAny(pattern, "*?["); idx >= 0 {
		prefix = pattern[:idx]
	}
	base := filepath.Clean(filepath.Dir(prefix))
	if !strings.HasPrefix(base, s.baseDir) {
		return "", false, fmt.Errorf("file path is outside session directory")
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", false, fmt.Errorf("invalid file path pattern: %w", err)
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	if len(matches) > 1 {
		return "", false, fmt.Errorf("file path pattern matched %d files; provide a more specific path", len(matches))
	}
	return filepath.Clean(matches[0]), true, nil
}

// ReadResultLines reads a line range from a stored result file
func (s *ToolResultStorage) ReadResultLines(filePath string, startLine, endLine int) (string, error) {
	resolvedPath, err := s.resolveStoredResultPath(filePath)
	if err != nil {
		return "", err
	}
	if resolvedPath == "" {
		return "", nil
	}

	content, err := os.ReadFile(resolvedPath)
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
	resolvedPath, err := s.resolveStoredResultPath(filePath)
	if err != nil {
		return nil, err
	}
	if resolvedPath == "" {
		return nil, nil
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read result file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var matches []GrepMatch
	trimmedPattern := strings.TrimSpace(pattern)
	matcher := func(line string) bool {
		return strings.Contains(strings.ToLower(line), strings.ToLower(trimmedPattern))
	}
	if re, compileErr := regexp.Compile("(?i)" + trimmedPattern); compileErr == nil {
		matcher = re.MatchString
	}

	for lineNum, line := range lines {
		if matcher(line) {
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
	resolvedPath, err := s.resolveStoredResultPath(filePath)
	if err != nil {
		return "", err
	}
	if resolvedPath == "" {
		return "", nil
	}

	content, err := os.ReadFile(resolvedPath)
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
			return fmt.Sprintf("No matches found for pattern '%s' in %s", pattern, filepath.Base(resolvedPath)), nil
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
