package spaces

import (
	"context"
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
	SharedInfra *codingworkflow.SharedInfrastructure
}

func NewCapability(sharedInfra ...*codingworkflow.SharedInfrastructure) *Capability {
	var infra *codingworkflow.SharedInfrastructure
	if len(sharedInfra) > 0 {
		infra = sharedInfra[0]
	}
	return &Capability{SharedInfra: infra}
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }
func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{EnabledByDefault: true, Tier: "base"}
}
func (c *Capability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if state.Domain == "" {
		return capabilities.ClassificationResult{}
	}
	if state.Domain == "Spaces" || state.Domain == CapabilityID || state.Domain == CapabilityName {
		return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
	}
	return capabilities.ClassificationResult{}
}
func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	domain := strings.ToLower(strings.TrimSpace(state.Domain))
	parts := []string{
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
	}
	if strings.Contains(lowerQuery, "test") || strings.Contains(lowerQuery, "testing") || strings.Contains(lowerQuery, "fail") || strings.Contains(lowerQuery, "failing") {
		parts = append(parts, "- For testing or validation requests, inspect the relevant Space behavior, validate the expected mutation or retrieval path, and preserve the failure context while fixing the issue.")
	}
	if domain != "" {
		parts = append(parts, "- Follow the current domain context so the workflow stays aligned with the active Space task rather than falling back to a generic coding path.")
	}
	return strings.Join(parts, "\n")
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
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(lowerQuery, "test") || strings.Contains(lowerQuery, "testing") || strings.Contains(lowerQuery, "fail") || strings.Contains(lowerQuery, "failing") {
		return "For testing or validation requests, investigate the relevant Space behavior, validate the expected mutation or retrieval path, and prefer targeted fixes that preserve the current knowledge contract."
	}
	return "Use the Spaces capability to execute the relevant Space operations and keep the workflow aligned with the current domain context."
}
func (c *Capability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (c *Capability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (c *Capability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	_ = ctx
	_ = query
	_ = state
	_ = result
	return nil
}
