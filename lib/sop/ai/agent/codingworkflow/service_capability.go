package codingworkflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	ServiceCapabilityID   = "coding_workflow_services"
	ServiceCapabilityName = "Coding Workflow Services"
)

// ServiceCapability exposes the reusable coding-workflow primitives to the Ask loop
// through the capability contract. It participates lightly in routing and exposes
// explicit tools for context building and repo inspection without taking over the
// main orchestration path.
type ServiceCapability struct {
	SharedInfra *SharedInfrastructure
}

func NewServiceCapability(sharedInfra *SharedInfrastructure) *ServiceCapability {
	return &ServiceCapability{SharedInfra: sharedInfra}
}

func (c *ServiceCapability) ID() string   { return ServiceCapabilityID }
func (c *ServiceCapability) Name() string { return ServiceCapabilityName }

func (c *ServiceCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{
			{
				Resource: "filesystem",
				Actions:  []string{"read", "write", "create", "delete"},
				Gate:     capabilities.GateWriteAccess,
			},
			{
				Resource: "shell",
				Actions:  []string{"read"},
				Gate:     capabilities.GateReadAccess,
			},
		},
	}
}

func (c *ServiceCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil {
		return capabilities.ClassificationResult{}
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "coding_workflow_services" {
			return capabilities.ClassificationResult{Relevant: true, Score: 15, Mode: capabilities.CapabilityModePrimary}
		}
		if hint == "coding_workflow" || hint == "codingworkflow" || hint == "repo_edit" {
			return capabilities.ClassificationResult{Relevant: true, Score: 14, Mode: capabilities.CapabilityModeSupporting}
		}
		if hint == "desktop_automation" || hint == "local_ops" {
			return capabilities.ClassificationResult{}
		}
	}

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return capabilities.ClassificationResult{}
	}
	if strings.Contains(q, "fix") ||
		strings.Contains(q, "refactor") ||
		strings.Contains(q, "patch") ||
		strings.Contains(q, "implement") ||
		strings.Contains(q, "modify") ||
		strings.Contains(q, "edit") ||
		strings.Contains(q, "repo") ||
		strings.Contains(q, "repository") ||
		strings.Contains(q, "code") ||
		strings.Contains(q, "parser") ||
		strings.Contains(q, "test") ||
		strings.Contains(q, "debug") ||
		strings.Contains(q, "analy") ||
		strings.Contains(q, "design") ||
		strings.Contains(q, "plan") ||
		strings.Contains(q, "architect") ||
		strings.Contains(q, "bug") {
		return capabilities.ClassificationResult{Relevant: true, Score: 11, Mode: capabilities.CapabilityModeSupporting}
	}
	return capabilities.ClassificationResult{}
}

func (c *ServiceCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Coding Workflow Services\n- Expose reusable coding workflow helpers to the Ask loop for context building, planning, and repo inspection.\n- Use these services when the task needs repository context or a lightweight structured next step.\n- Keep the Ask loop anchored in explicit context, safe previews, and continuation state."
}

func (c *ServiceCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{
		{
			Name:        "build_context",
			Description: "Builds a lightweight repository context pack from the current task and working directory.",
			Schema:      "{}",
		},
		{
			Name:        "inspect_repo",
			Description: "Inspects the repository state and returns a concise summary relevant to the current task.",
			Schema:      "{}",
		},
	}
}

func (c *ServiceCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "Use the coding workflow services capability when the task needs repository context, repo inspection, or a structured next step. Prefer the exposed tools to gather evidence before acting, and preserve continuity across turns.\n\n" + CodeFormatInstructions
}

func (c *ServiceCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (c *ServiceCapability) Participate(ctx context.Context, query string, state capabilities.RoutingState) (capabilities.ParticipationResult, bool, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return capabilities.ParticipationResult{}, false, nil
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "fix") ||
		strings.Contains(lower, "debug") ||
		strings.Contains(lower, "test") ||
		strings.Contains(lower, "parser") ||
		strings.Contains(lower, "repo") ||
		strings.Contains(lower, "implement") ||
		strings.Contains(lower, "refactor") ||
		strings.Contains(lower, "patch") {
		return capabilities.ParticipationResult{
			Summary: "coding workflow services: gather repository context and shape the next coding step",
			Data: map[string]any{
				"capability": ServiceCapabilityID,
				"tool":       "build_context",
			},
		}, true, nil
	}
	return capabilities.ParticipationResult{}, false, nil
}

func (c *ServiceCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state
	name := strings.TrimSpace(strings.ToLower(toolName))
	if name == "" {
		return "", fmt.Errorf("empty tool name")
	}
	switch name {
	case "build_context":
		return c.runBuildContext(args)
	case "inspect_repo":
		return c.runInspectRepo(args)
	default:
		return "", fmt.Errorf("unsupported tool %q", toolName)
	}
}

func (c *ServiceCapability) runBuildContext(args map[string]any) (string, error) {
	infra := c.ensureSharedInfrastructure()
	task := c.buildTask(args)
	pack, err := infra.ContextManager.BuildContext(task, task.State)
	if err != nil {
		return "", fmt.Errorf("build context: %w", err)
	}
	if infra.RepoIndexer != nil {
		_, _ = infra.RepoIndexer.Index(task, task.State)
	}
	return formatContextPack(pack), nil
}

func (c *ServiceCapability) runInspectRepo(args map[string]any) (string, error) {
	infra := c.ensureSharedInfrastructure()
	task := c.buildTask(args)
	if infra.RepoInspector == nil {
		return "", fmt.Errorf("repo inspector not configured")
	}
	summary, err := infra.RepoInspector.Inspect(task, task.State)
	if err != nil {
		return "", fmt.Errorf("inspect repo: %w", err)
	}
	return fmt.Sprintf("Repository Inspection\nChangedFiles: %s\nDiffPreview: %s", strings.Join(summary.ChangedFiles, ", "), summary.DiffPreview), nil
}

func (c *ServiceCapability) ensureSharedInfrastructure() *SharedInfrastructure {
	if c == nil {
		return NewDefaultSharedInfrastructure()
	}
	if c.SharedInfra == nil {
		c.SharedInfra = NewDefaultSharedInfrastructure()
	}
	return c.SharedInfra.ensureDefaults()
}

func (c *ServiceCapability) buildTask(args map[string]any) Task {
	query := ""
	if raw, ok := args["query"].(string); ok {
		query = raw
	} else if raw, ok := args["task"].(string); ok {
		query = raw
	} else if raw, ok := args["description"].(string); ok {
		query = raw
	}

	workingDir := ""
	if raw, ok := args["working_dir"].(string); ok {
		workingDir = strings.TrimSpace(raw)
	}
	activeFile := ""
	if raw, ok := args["active_file"].(string); ok {
		activeFile = strings.TrimSpace(raw)
	}

	if workingDir == "" || activeFile == "" {
		inferredDir, inferredFile := inferTaskInputs(query)
		if workingDir == "" {
			workingDir = inferredDir
		}
		if activeFile == "" {
			activeFile = inferredFile
		}
	}

	return Task{
		WorkingDir:  workingDir,
		ActiveFile:  activeFile,
		Description: query,
		State: RunState{
			Args:     map[string]string{},
			Metadata: map[string]string{},
		},
	}
}

func formatContextPack(pack ContextPack) string {
	parts := []string{"Context Pack"}
	if pack.WorkingDir != "" {
		parts = append(parts, fmt.Sprintf("WorkingDir: %s", pack.WorkingDir))
	}
	if pack.ActiveFile != "" {
		parts = append(parts, fmt.Sprintf("ActiveFile: %s", pack.ActiveFile))
	}
	if len(pack.RelatedFiles) > 0 {
		parts = append(parts, fmt.Sprintf("RelatedFiles: %s", strings.Join(pack.RelatedFiles, ", ")))
	}
	if len(pack.TestFiles) > 0 {
		parts = append(parts, fmt.Sprintf("TestFiles: %s", strings.Join(pack.TestFiles, ", ")))
	}
	if pack.RepositorySummary != "" {
		parts = append(parts, fmt.Sprintf("Summary: %s", pack.RepositorySummary))
	}
	return strings.Join(parts, "\n")
}
