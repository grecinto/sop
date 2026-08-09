package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/repo"
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

	client, err := repo.NewClientForPath(workingDir)
	if err != nil || client == nil {
		return nil, nil
	}
	changes, err := client.Status(workingDir, repo.StatusOptions{})
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, nil
	}

	files := make([]string, 0, len(changes))
	for _, change := range changes {
		files = append(files, change.Path)
	}
	return files, nil
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
