package agent

import (
	"context"
	"fmt"
	"strings"

	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// WorkflowGitToolAdapter routes workflow git operations through the existing desktop automation tools.
type WorkflowGitToolAdapter struct {
	agent *CopilotAgent
}

func NewWorkflowGitToolAdapter(agent *CopilotAgent) *WorkflowGitToolAdapter {
	return &WorkflowGitToolAdapter{agent: agent}
}

func (a *WorkflowGitToolAdapter) Status(repoPath string) ([]string, error) {
	if a == nil || a.agent == nil {
		return nil, fmt.Errorf("workflow git adapter requires an agent")
	}
	out, err := a.agent.toolGitStatus(context.Background(), map[string]any{"path": repoPath, "short": true})
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) == 0 {
			continue
		}
		name := strings.TrimSpace(parts[len(parts)-1])
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

func (a *WorkflowGitToolAdapter) Diff(repoPath string) (string, error) {
	if a == nil || a.agent == nil {
		return "", fmt.Errorf("workflow git adapter requires an agent")
	}
	return a.agent.toolGitDiff(context.Background(), map[string]any{"path": repoPath})
}

func (a *WorkflowGitToolAdapter) Commit(repoPath, message string) error {
	if a == nil || a.agent == nil {
		return fmt.Errorf("workflow git adapter requires an agent")
	}
	_, err := a.agent.toolGitCommit(context.Background(), map[string]any{"path": repoPath, "message": message, "all": true})
	return err
}

var _ cw.GitClient = (*WorkflowGitToolAdapter)(nil)
