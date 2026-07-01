package spaces

import (
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

const (
	CapabilityID   = "spaces"
	CapabilityName = "Spaces"
)

type ExecutionContextInput struct {
	CRUDFlags   map[string]bool
	DBArtifacts []string
}

type Capability struct {
	Orchestrator *codingworkflow.Orchestrator
}

func NewCapability(orchestrator ...*codingworkflow.Orchestrator) *Capability {
	var orch *codingworkflow.Orchestrator
	if len(orchestrator) > 0 {
		orch = orchestrator[0]
	}
	return &Capability{Orchestrator: orch}
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{EnabledByDefault: true, Tier: "base"}
}
func (c *Capability) CanHandle(query string, state capabilities.RoutingState) bool {
	if state.Domain == "" {
		return false
	}
	return state.Domain == "Spaces" || state.Domain == CapabilityID || state.Domain == CapabilityName
}
func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	return strings.Join([]string{
		"Structured Context: Spaces Tools",
		"- Space mutations are business-critical because they change persisted knowledge. The tool that performs the write also defines the durability boundary for that change.",
		"- When a Space tool manages its own transaction path, treat that tool call as the commit boundary for the requested knowledge mutation.",
		"- mint_to_space: Persist generated or discovered content in a Space. Use the exact kb_name requested by the user; this tool manages its own write transaction.",
		"- delete_space: Remove an entire Space only when the user explicitly wants full deletion; it manages its own deletion path.",
		"- enrich_space: Run the enrichment pipeline only when the user explicitly wants derived knowledge refreshed.",
		"- update_space_config: Change Space routing, persona, or tool settings with a grounded config object.",
		"- read_space_config: Inspect current Space configuration before changing it or when the user asks how it behaves.",
		"- vectorize_space: Refresh embeddings for the whole Space only when the user explicitly asks for vectorization or semantic refresh.",
		"- vectorize_space_categories: Refresh embeddings for specific categories when the request is narrower than full-space vectorization.",
		"- vectorize_space_items: Refresh embeddings for specific items when the user wants the tightest possible vectorization scope.",
	}, "\n")
}

func (c *Capability) BuildExecutionContext(input ExecutionContextInput) string {
	var sb strings.Builder
	sb.WriteString("Domain: Spaces\n")
	if ordered := orderedCRUDFlags(input.CRUDFlags); len(ordered) > 0 {
		sb.WriteString(fmt.Sprintf("CRUD Scope: %s\n", strings.Join(ordered, ", ")))
	}
	if len(input.DBArtifacts) > 0 {
		sb.WriteString(fmt.Sprintf("Target Spaces: %s\n", strings.Join(input.DBArtifacts, ", ")))
	}
	return strings.TrimSpace(sb.String())
}

func orderedCRUDFlags(flags map[string]bool) []string {
	order := []string{"C", "R", "U", "D"}
	ordered := make([]string, 0, len(flags))
	for _, crud := range order {
		if flags[crud] {
			ordered = append(ordered, crud)
		}
	}
	return ordered
}

func (c *Capability) ExposeTools() []capabilities.ToolDefinition { return nil }
func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return ""
}
func (c *Capability) Run(query string, state capabilities.RoutingState) (capabilities.CapabilityResult, error) {
	if c == nil || c.Orchestrator == nil {
		return capabilities.CapabilityResult{Summary: "spaces capability placeholder"}, nil
	}
	metadata := map[string]string{"query": query}
	if state.Domain != "" {
		metadata["domain"] = state.Domain
	}
	result, err := c.Orchestrator.Execute(codingworkflow.Task{Description: query, ActiveFile: "", State: codingworkflow.RunState{Args: map[string]string{"query": query}, Metadata: metadata}})
	if err != nil {
		return capabilities.CapabilityResult{}, err
	}
	data := map[string]any{"message": result.Message, "task_type": result.TaskType, "plan": result.Plan, "resumed_from_state": result.ResumedFromState}
	return capabilities.CapabilityResult{Summary: result.Message, Data: data}, nil
}
