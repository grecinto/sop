package codingworkflow

import (
	"context"
	log_slog "log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai"
)

// NormalizeRepoAlias converts common repository aliases to ".", but preserves actual paths.
// Exported for use by phase implementations.
func NormalizeRepoAlias(path string) string {
	trimmed := strings.TrimSpace(path)

	// If path contains separators (/, \) or relative indicators (..), treat as actual path
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") || strings.Contains(trimmed, "..") {
		return trimmed
	}

	// Only normalize bare aliases
	switch strings.ToLower(trimmed) {
	case "current", "current folder", "current directory", "repo", "current repo", "current repository", "workspace", "current workspace", "current working directory":
		return "."
	default:
		return trimmed
	}
}

// contextualBaseDir retrieves session working directory from context
func contextualBaseDir(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	payload := ai.GetSessionPayload(ctx)
	if payload == nil || payload.Variables == nil {
		return ""
	}

	// Try working_dir first, then fallback to active_repo
	if wd, ok := payload.Variables["working_dir"].(string); ok && wd != "" {
		return wd
	}

	if payload.ActiveRepo != "" {
		return payload.ActiveRepo
	}

	return ""
}

// ResolveContextWorkingDir resolves working_dir parameter using session context.
// Exported for use by phase implementations.
func ResolveContextWorkingDir(ctx context.Context, workingDir string) string {
	trimmed := strings.TrimSpace(workingDir)

	// Normalize "current repo" and similar aliases to "."
	normalized := NormalizeRepoAlias(trimmed)
	cleaned := filepath.Clean(normalized)

	// If explicit non-dot path provided, resolve it
	if normalized != "" && cleaned != "." && cleaned != string(filepath.Separator) {
		// Get session's base directory for validation
		base := contextualBaseDir(ctx)

		// If it's an absolute path, validate it against session context
		if filepath.IsAbs(cleaned) {
			// Reject absolute paths if we have no session context
			if base == "" {
				return "."
			}

			// Validate path is within session working dir
			if base != "" && !strings.HasPrefix(cleaned, base) {
				return "."
			}

			return cleaned
		}

		// Relative path - join with session base if available
		if base != "" {
			joined := filepath.Join(base, cleaned)
			return joined
		}

		return cleaned
	}

	// Use session context for default
	if base := contextualBaseDir(ctx); base != "" {
		return base
	}

	return "."
}

// ResolveRepoWorkingDir resolves task working directory and discovers git repositories.
// Exported for use by phase implementations.
func ResolveRepoWorkingDir(ctx context.Context, task Task) (Task, []string) {
	resolvedDir := task.WorkingDir

	// If working_dir is explicitly set and is an actual path, use it
	if resolvedDir != "" {
		cleaned := filepath.Clean(resolvedDir)
		if _, err := os.Stat(cleaned); err == nil {
			task.WorkingDir = cleaned

			// Check if it's a git repo
			if IsGitRepositoryDir(cleaned) {
				return task, []string{cleaned}
			}

			// Not a git repo, try to discover from here
			candidates := DiscoverWorkingDirCandidates(cleaned)
			if len(candidates) > 0 {
				// Use first discovered repo as working dir
				task.WorkingDir = candidates[0]
			}
			return task, candidates
		}
	}

	// No explicit working_dir or it doesn't exist - use session context
	base := contextualBaseDir(ctx)
	if base == "" {
		// No session context, use current directory
		if cwd, err := os.Getwd(); err == nil {
			base = cwd
		} else {
			base = "."
		}
	}

	task.WorkingDir = base

	// Discover git repositories from base
	candidates := DiscoverWorkingDirCandidates(base)
	if len(candidates) > 0 {
		task.WorkingDir = candidates[0]
	}

	return task, candidates
}

// IsGitRepositoryDir checks if the given directory contains a .git folder.
// Exported for use by phase implementations.
func IsGitRepositoryDir(dir string) bool {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(filepath.Clean(trimmed), ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// DiscoverWorkingDirCandidates finds git repositories starting from baseDir.
// Exported for use by phase implementations.
func DiscoverWorkingDirCandidates(baseDir string) []string {
	absBase := baseDir
	if !filepath.IsAbs(baseDir) {
		if cwd, err := os.Getwd(); err == nil {
			absBase = filepath.Join(cwd, baseDir)
		}
	}

	absBase = filepath.Clean(absBase)

	var candidates []string

	// Check if base itself is a git repo
	if IsGitRepositoryDir(absBase) {
		candidates = append(candidates, absBase)
	}

	// Search for git repos in subdirectories (one level deep)
	entries, err := os.ReadDir(absBase)
	if err != nil {
		return candidates
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subPath := filepath.Join(absBase, entry.Name())
		if IsGitRepositoryDir(subPath) {
			candidates = append(candidates, subPath)
		}
	}

	return candidates
}

// PreprocessToolResult preprocesses tool output for test results.
// Exported for use by phase implementations.
func PreprocessToolResult(toolName string, rawOutput string, filePath string, maxInlineSize int) string {
	log_slog.Info("[PreprocessToolResult] Called",
		"tool", toolName,
		"raw_len", len(rawOutput),
		"file_path", filePath,
		"max_inline", maxInlineSize)

	// Try to parse into structured format
	structured := ParseTestResult(toolName, rawOutput)
	if structured == nil {
		// Not a recognized format, return empty to use standard text flow
		log_slog.Info("[PreprocessToolResult] ParseTestResult returned nil - no structured format")
		return ""
	}

	log_slog.Info("[PreprocessToolResult] ParseTestResult succeeded", "type", structured.Type, "error_count", len(structured.ErrorDetails))

	// Convert to enriched JSON result
	result := CreateEnrichedTestResult(structured, rawOutput, filePath, maxInlineSize)
	log_slog.Info("[PreprocessToolResult] Returning", "result_len", len(result))
	return result
}
