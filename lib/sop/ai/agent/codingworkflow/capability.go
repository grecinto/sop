package codingworkflow

import (
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	CapabilityID   = "coding_workflow"
	CapabilityName = "Coding Workflow"
)

// Capability adapts the coding workflow orchestrator into the agent capability plugin contract.
type Capability struct {
	Orchestrator *Orchestrator
}

func NewCapability(orchestrator *Orchestrator) *Capability {
	return &Capability{Orchestrator: orchestrator}
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{EnabledByDefault: false, Tier: "local"}
}

func (c *Capability) CanHandle(query string, state capabilities.RoutingState) bool {
	if c == nil || c.Orchestrator == nil {
		return false
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "coding_workflow" || hint == "codingworkflow" || hint == "repo_edit" {
			return true
		}
		if hint == "desktop_automation" || hint == "local_ops" {
			return false
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	return strings.Contains(q, "fix") ||
		strings.Contains(q, "refactor") ||
		strings.Contains(q, "patch") ||
		strings.Contains(q, "implement") ||
		strings.Contains(q, "modify") ||
		strings.Contains(q, "search") ||
		strings.Contains(q, "edit") ||
		strings.Contains(q, "repo") ||
		strings.Contains(q, "code") ||
		strings.Contains(q, "parser") ||
		strings.Contains(q, "function") ||
		strings.Contains(q, "class") ||
		strings.Contains(q, "module") ||
		strings.Contains(q, "generate") ||
		strings.Contains(q, "json") ||
		strings.Contains(q, "script") ||
		strings.Contains(q, "lego") ||
		strings.Contains(q, "analy") ||
		strings.Contains(q, "analysis") ||
		strings.Contains(q, "sql") ||
		strings.Contains(q, "record") ||
		strings.Contains(q, "table")
}

func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Coding Workflow\n- Use the local workflow for code edits, structured generation, and analysis tasks.\n- Prefer a design-first approach: understand the requirement, shape the change, and make the implementation plan explicit.\n- Use TDD as a lightweight companion to the design: draft or review a focused test case when a code change is underway, without turning tests into a mandatory gate.\n- Preserve continuity, plan steps explicitly, and keep actions previewable and safe."
}

func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{Name: "coding_workflow", Description: "Runs the local workflow for repository inspection, patch preview, safe edits, artifact generation, and analysis.", Schema: "{}"}}
}

func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "Use the coding workflow capability for local-runtime code, generation, and analysis tasks. Start from the design intent, make the plan explicit, and use TDD as an assistive practice by drafting or reviewing focused tests while implementing. Keep actions explicit, previewable, and safe, and preserve continuation context across turns."
}

func (c *Capability) Run(query string, state capabilities.RoutingState) (capabilities.CapabilityResult, error) {
	if c == nil || c.Orchestrator == nil {
		return capabilities.CapabilityResult{}, fmt.Errorf("coding workflow capability requires an orchestrator")
	}
	workingDir, activeFile := inferTaskInputs(query)
	metadata := map[string]string{"query": query}
	if state.Domain != "" {
		metadata["domain"] = state.Domain
	}
	if workingDir != "" {
		metadata["working_dir"] = workingDir
	}
	if activeFile != "" {
		metadata["active_file"] = activeFile
	}
	task := Task{Description: query, ActiveFile: activeFile, State: RunState{Args: map[string]string{"query": query}, Metadata: metadata}}
	if workingDir != "" {
		task.WorkingDir = workingDir
	}
	result, err := c.Orchestrator.Execute(task)
	if err != nil {
		return capabilities.CapabilityResult{}, err
	}
	data := map[string]any{"message": result.Message, "applied_patches": len(result.AppliedPatches), "task_type": result.TaskType, "plan": result.Plan, "resumed_from_state": result.ResumedFromState, "active_file": result.ContextPack.ActiveFile, "working_dir": result.ContextPack.WorkingDir}
	if c.Orchestrator != nil {
		data["continuity_signal"] = "task_outcome"
	}
	return capabilities.CapabilityResult{Summary: result.Message, Data: data}, nil
}

func inferTaskInputs(query string) (string, string) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", ""
	}
	if workingDir := inferWorkingDir(trimmed); workingDir != "" {
		return workingDir, inferActiveFile(trimmed)
	}
	return "", inferActiveFile(trimmed)
}

func inferWorkingDir(query string) string {
	patterns := []string{"in ", "under ", "from ", "for ", "at ", "repo ", "workspace "}
	for _, pattern := range patterns {
		if idx := strings.Index(strings.ToLower(query), pattern); idx >= 0 {
			candidate := strings.TrimSpace(query[idx+len(pattern):])
			if candidate == "" {
				continue
			}
			parts := strings.Fields(candidate)
			if len(parts) > 0 {
				first := strings.Trim(parts[0], "./")
				if first != "" && strings.Contains(first, "/") {
					return first
				}
			}
		}
	}
	return ""
}

func inferActiveFile(query string) string {
	for _, token := range strings.Fields(query) {
		candidate := strings.TrimSpace(token)
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "http") || strings.HasPrefix(candidate, "#") {
			continue
		}
		if strings.Contains(candidate, "/") || strings.HasSuffix(candidate, ".go") || strings.HasSuffix(candidate, ".ts") || strings.HasSuffix(candidate, ".js") || strings.HasSuffix(candidate, ".py") || strings.HasSuffix(candidate, ".md") || strings.HasSuffix(candidate, ".json") {
			return strings.Trim(candidate, ".,")
		}
	}
	return ""
}
