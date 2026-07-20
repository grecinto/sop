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
	toolResultThreshold = 5 * 1024 // 5KB - triggers summarization for most test outputs

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
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n") {
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
func (s *ToolResultStorage) createSummary(toolName, result, filePath string, size int) string {
	sizeKB := float64(size) / 1024.0

	// Extract head and tail
	headSize := toolResultMaxInline / 2
	tailSize := toolResultMaxInline / 2

	head := result
	tail := ""
	omittedBytes := 0

	if len(result) > toolResultMaxInline {
		head = result[:headSize]
		tail = result[len(result)-tailSize:]
		omittedBytes = size - headSize - tailSize
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
	sb.WriteString("To analyze based on user's request:\n")
	sb.WriteString("  • Tests/results: grep \"PASS\\|FAIL\\|error\" <file>\n")
	sb.WriteString("  • Find specific: grep \"<keyword>\" <file>\n")
	sb.WriteString("  • Count: grep -c \"<pattern>\" <file> or wc -l <file>\n")
	sb.WriteString("  • Extract: sed -n '10,50p' <file> or awk 'NR>=10 && NR<=50' <file>\n")

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
