package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// toolResultThreshold defines when outputs should be file-backed vs inline
	// Outputs larger than this are written to temp files
	toolResultThreshold = 20 * 1024 // 20KB

	// toolResultMaxInline defines the summary size for file-backed results
	// We show head + tail of this size total
	toolResultMaxInline = 2 * 1024 // 2KB
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
	size = len(result)

	// Small enough - return inline
	if size <= toolResultThreshold {
		return result, "", size, nil
	}

	// Large - write to file and return summary
	resultID := uuid.New().String()[:8] // Short UUID for readability
	fileName := fmt.Sprintf("%s-%s.txt", sanitizeToolName(toolName), resultID)
	filePath = filepath.Join(s.baseDir, fileName)

	if err := os.WriteFile(filePath, []byte(result), 0600); err != nil {
		return "", "", 0, fmt.Errorf("failed to write tool result to file: %w", err)
	}

	// Create summary with head/tail and tool recommendations
	summary := s.createSummary(toolName, result, filePath, size)
	return summary, filePath, size, nil
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

	// Header
	sb.WriteString(fmt.Sprintf("Tool %s produced large output (%.1fKB).\n", toolName, sizeKB))
	sb.WriteString(fmt.Sprintf("Full output saved to: %s\n\n", filePath))

	// Head content
	sb.WriteString(fmt.Sprintf("[First %d bytes]\n", len(head)))
	sb.WriteString(head)
	if !strings.HasSuffix(head, "\n") {
		sb.WriteString("\n")
	}

	// Omission marker
	if omittedBytes > 0 {
		sb.WriteString(fmt.Sprintf("\n... [%d bytes omitted - use text processing tools below] ...\n\n", omittedBytes))
	}

	// Tail content
	if tail != "" {
		sb.WriteString(fmt.Sprintf("[Last %d bytes]\n", len(tail)))
		sb.WriteString(tail)
		if !strings.HasSuffix(tail, "\n") {
			sb.WriteString("\n")
		}
	}

	// Tool recommendations
	sb.WriteString("\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString("📊 ANALYSIS TOOLS (Cross-Platform)\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Common patterns based on tool type
	if isTestCommand(toolName) {
		sb.WriteString("Finding failures:\n")
		sb.WriteString(fmt.Sprintf("  grep \"FAIL\" %s\n", filePath))
		sb.WriteString(fmt.Sprintf("  grep -n \"FAIL\" %s\n\n", filePath))

		sb.WriteString("Counting results:\n")
		sb.WriteString(fmt.Sprintf("  grep -c \"PASS\" %s\n", filePath))
		sb.WriteString(fmt.Sprintf("  grep -c \"FAIL\" %s\n\n", filePath))

		sb.WriteString("View test summary (last 50 lines):\n")
		sb.WriteString(fmt.Sprintf("  tail -50 %s\n\n", filePath))
	} else if isBuildCommand(toolName) {
		sb.WriteString("Finding errors:\n")
		sb.WriteString(fmt.Sprintf("  grep \"error:\" %s\n", filePath))
		sb.WriteString(fmt.Sprintf("  grep -n \"error:\" %s\n\n", filePath))

		sb.WriteString("View with context:\n")
		sb.WriteString(fmt.Sprintf("  grep -C 3 \"error:\" %s\n\n", filePath))
	} else {
		sb.WriteString("Search for patterns:\n")
		sb.WriteString(fmt.Sprintf("  grep \"pattern\" %s\n", filePath))
		sb.WriteString(fmt.Sprintf("  grep -n \"pattern\" %s\n\n", filePath))

		sb.WriteString("View sections:\n")
		sb.WriteString(fmt.Sprintf("  head -50 %s\n", filePath))
		sb.WriteString(fmt.Sprintf("  tail -50 %s\n\n", filePath))
	}

	// Generic tools
	sb.WriteString("Extract sections:\n")
	sb.WriteString(fmt.Sprintf("  sed -n '/start/,/end/p' %s\n", filePath))
	sb.WriteString(fmt.Sprintf("  awk '/pattern/,/pattern/' %s\n\n", filePath))

	sb.WriteString("Line ranges (after finding line numbers):\n")
	sb.WriteString(fmt.Sprintf("  read_file(path: \"%s\", startLine: N, endLine: M)\n\n", filePath))

	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

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

// isTestCommand checks if a tool name is related to testing
func isTestCommand(toolName string) bool {
	lower := strings.ToLower(toolName)
	return strings.Contains(lower, "test") || strings.Contains(lower, "spec")
}

// isBuildCommand checks if a tool name is related to building
func isBuildCommand(toolName string) bool {
	lower := strings.ToLower(toolName)
	return strings.Contains(lower, "build") || strings.Contains(lower, "compile") ||
		strings.Contains(lower, "make")
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
