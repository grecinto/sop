package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop"

	"github.com/go-git/go-git/v5"
	"github.com/sharedcode/sop/ai"
)

func appendAskCompletionSummary(ctx context.Context, finalText string, toolCalls []ai.ToolCall, outcomeFacts []string) string {
	trimmed := strings.TrimSpace(finalText)
	if trimmed == "" {
		return finalText
	}

	changedFiles, err := findChangedFilesForAskSummary(ctx)
	if err != nil || len(changedFiles) == 0 {
		return finalText
	}

	var summary strings.Builder
	summary.WriteString(trimmed)
	summary.WriteString("\n\n### What happened")

	if len(toolCalls) > 0 {
		summary.WriteString(fmt.Sprintf("\n- Executed %d tool action(s) to fulfill the request.", len(toolCalls)))
	} else if len(outcomeFacts) > 0 {
		summary.WriteString("\n- Completed the requested task using the available reasoning tools.")
	} else {
		summary.WriteString("\n- Completed the requested task.")
	}

	summary.WriteString("\n- Files updated:")
	for _, file := range changedFiles {
		summary.WriteString(fmt.Sprintf("\n  - ./%s", normalizeAskSummaryPath(file)))
	}

	return summary.String()
}

func persistAskCompletionState(ctx context.Context, thread *ConversationThread, finalText string, toolCalls []ai.ToolCall, outcomeFacts []string) {
	if thread == nil {
		return
	}

	changedFiles, err := findChangedFilesForAskSummary(ctx)
	if err != nil || len(changedFiles) == 0 {
		if thread.StreamContext != nil {
			thread.StreamContext.CompletionSummary = ""
			thread.StreamContext.UpdatedFiles = nil
		}
		return
	}

	normalizedFiles := make([]string, 0, len(changedFiles))
	for _, file := range changedFiles {
		normalizedFiles = append(normalizedFiles, normalizeAskSummaryPath(file))
	}

	if thread.StreamContext == nil {
		thread.StreamContext = NewWorkStreamContext("completion", sop.UUID{}, InterjectionNone)
	}

	thread.StreamContext.CompletionSummary = strings.TrimSpace(finalText)
	thread.StreamContext.UpdatedFiles = normalizedFiles
}

func findChangedFilesForAskSummary(ctx context.Context) ([]string, error) {
	workingDir := resolveAskSummaryWorkingDir(ctx)
	if strings.TrimSpace(workingDir) == "" {
		return nil, nil
	}

	candidate := workingDir
	for {
		repo, err := git.PlainOpen(candidate)
		if err == nil {
			wt, err := repo.Worktree()
			if err != nil {
				return nil, err
			}
			status, err := wt.Status()
			if err != nil {
				return nil, err
			}

			files := make([]string, 0, len(status))
			for file := range status {
				files = append(files, file)
			}
			if len(files) == 0 {
				return nil, nil
			}
			return files, nil
		}

		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
		candidate = parent
	}

	return nil, nil
}

func resolveAskSummaryWorkingDir(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if workingDir, ok := ctx.Value(ctxKeyWorkingDir).(string); ok && strings.TrimSpace(workingDir) != "" {
		return strings.TrimSpace(workingDir)
	}

	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func normalizeAskSummaryPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	trimmed = filepath.ToSlash(trimmed)
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	return strings.TrimSpace(trimmed)
}
