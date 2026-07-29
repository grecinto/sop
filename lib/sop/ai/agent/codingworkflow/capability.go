package codingworkflow

import (
	"encoding/json"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	CapabilityID   = "coding_workflow"
	CapabilityName = "Coding Workflow"
)

// Capability adapts the coding workflow orchestrator into the agent capability plugin contract.
// This capability is code-driven and executes hardcoded workflows.
// It is separate from KB-driven workflows handled by WorkflowManager.
type Capability struct {
	Orchestrator *Orchestrator
}

func NewCapability(orchestrator *Orchestrator) *Capability {
	return &Capability{
		Orchestrator: orchestrator,
	}
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess, // Gap 1: Requires write access
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite, // File modifications need approval
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

func (c *Capability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil || c.Orchestrator == nil {
		return capabilities.ClassificationResult{}
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "coding_workflow" || hint == "codingworkflow" || hint == "repo_edit" {
			return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
		}
		if hint == "desktop_automation" || hint == "local_ops" {
			return capabilities.ClassificationResult{}
		}
	}
	hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint))
	if domain := strings.ToLower(strings.TrimSpace(state.Domain)); domain != "" {
		switch domain {
		case "stores", "spaces":
			return capabilities.ClassificationResult{}
		case "desktop", "desktop automation", "commandline", "automation", "general", "sop":
			// Keep coding workflow available for desktop and general coding-oriented requests.
		default:
			if hint == "coding_workflow" || hint == "repo_edit" {
				return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
			}
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
		strings.Contains(q, "table") ||
		strings.Contains(q, "design") ||
		strings.Contains(q, "plan") ||
		strings.Contains(q, "architect") ||
		strings.Contains(q, "propose") ||
		strings.Contains(q, "spec") ||
		strings.Contains(q, "requirement") ||
		strings.Contains(q, "bug") ||
		strings.Contains(q, "test") {
		return capabilities.ClassificationResult{Relevant: true, Score: 12, Mode: capabilities.CapabilityModePrimary}
	}
	return capabilities.ClassificationResult{}
}

func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Coding Workflow\n- Use the local workflow for code edits, structured generation, and analysis tasks.\n- Prefer a design-first approach: understand the requirement, shape the change, and make the implementation plan explicit.\n- Use TDD as a lightweight companion to the design: draft or review a focused test case when a code change is underway, without turning tests into a mandatory gate.\n- Preserve continuity, plan steps explicitly, and keep actions previewable and safe."
}

func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{Name: "coding_workflow", Description: "Runs the local workflow for repository inspection, patch preview, safe edits, artifact generation, and analysis.", Schema: "{}"}}
}

func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "Use the coding workflow capability for local-runtime code, generation, and analysis tasks. Start from the design intent, make the plan explicit, and use TDD as an assistive practice by drafting or reviewing focused tests while implementing. Keep actions explicit, previewable, and safe, and preserve continuation context across turns.\n\n" + CodeFormatInstructions
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

func mustMarshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
