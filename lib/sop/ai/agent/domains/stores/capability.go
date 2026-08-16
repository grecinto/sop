package stores

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

const (
	CapabilityID   = "stores"
	CapabilityName = "Stores"
)

type ExecutionContextInput struct {
	CRUDFlags       map[string]bool
	DBArtifacts     []string
	ScriptAuthoring bool
	ActiveDatabase  string
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
	return capabilities.Entitlement{
		EnabledByDefault: true,
		Tier:             "base",
		RequiredGate:     capabilities.GateWriteAccess, // Gap 1: DB mutations need write
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalDBMutation, // DB write operations need approval
		},
		Permissions: []capabilities.Permission{
			{
				Resource: "database",
				Actions:  []string{"read", "query"},
				Gate:     capabilities.GateReadAccess,
			},
			{
				Resource: "database",
				Actions:  []string{"write", "create", "update", "delete"},
				Gate:     capabilities.GateWriteAccess,
			},
		},
	}
}
func (c *Capability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if state.Domain == "" {
		return capabilities.ClassificationResult{}
	}
	if state.Domain == "Stores" || state.Domain == CapabilityID || state.Domain == CapabilityName {
		return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
	}
	return capabilities.ClassificationResult{}
}
func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	domain := strings.ToLower(strings.TrimSpace(state.Domain))
	parts := []string{
		"Structured Context: Stores Tools",
		"You are a Stores Database Expert Agent. Translate requests into executable store operations.",
		"CRITICAL RULES:",
		"1. Never guess store names, field names, or join mappings. Use list_stores first whenever schema, relations, or field paths are uncertain.",
		"2. Think through the read/join/filter plan before writing the operation.",
		"3. SOP store queries are not SQL. When the user asks for a query or filter, produce SOP-native JSON tool/script operations, not SQL syntax.",
		"4. When you generate a multi-step store plan using steps such as begin_tx, open_store, scan, filter, sort, project, commit_tx, rollback_tx or return, place those steps inside execute_script.script and call execute_script to submit the plan to the executor. The last step's result is returned automatically or use return to explicitly specify the return value.",
		"5. If execution fails, analyze the error, rewrite the operation, and retry once. If it still fails, ask the user a short clarification question.",
	}
	if strings.Contains(lowerQuery, "test") || strings.Contains(lowerQuery, "testing") || strings.Contains(lowerQuery, "fail") || strings.Contains(lowerQuery, "failing") {
		parts = append(parts, "6. For testing or validation requests, inspect the relevant store operations, validate the expected behavior, and preserve the failure context while fixing the issue.")
	}
	if domain != "" {
		parts = append(parts, "7. Follow the current domain context so the workflow stays aligned with the active store task rather than falling back to a generic coding path.")
	}
	return strings.Join(parts, "\n")
}

func (c *Capability) BuildExecutionContext(input ExecutionContextInput) string {
	var sb strings.Builder
	sb.WriteString("Domain: Stores\n")
	if ordered := orderedCRUDFlags(input.CRUDFlags); len(ordered) > 0 {
		sb.WriteString(fmt.Sprintf("CRUD Scope: %s\n", strings.Join(ordered, ", ")))
	}
	if input.ActiveDatabase != "" {
		sb.WriteString(fmt.Sprintf("Active Database: %s\n", input.ActiveDatabase))
	}
	if len(input.DBArtifacts) > 0 {
		sb.WriteString("Target Stores:\n")
		for _, artifact := range input.DBArtifacts {
			sb.WriteString(fmt.Sprintf("- %s\n", artifact))
		}
	}
	if input.ScriptAuthoring {
		sb.WriteString("\nRelevant Script Authoring Operations:\n")
		sb.WriteString("- Use execute_script for multi-step store workflows when the request requires a concrete plan or mutation.")
	}
	return strings.TrimSpace(sb.String())
}

func (c *Capability) BuildPlanningContext(query string, state capabilities.RoutingState) string {
	parts := []string{
		"Stores Planning Context",
		"- Domain focus: persisted store operations and schema-aware query design.",
		"- Primary planning concerns: target database, store names, field paths, relation assumptions, and mutation safety.",
		"- Preserve data-contract compatibility unless the user explicitly requests a schema or behavior change.",
		"- Validation should prove the query or mutation against observed store shape and expected results.",
	}
	if domain := strings.TrimSpace(state.Domain); domain != "" {
		parts = append(parts, "- Active domain routing: "+domain)
	}
	if lower := strings.ToLower(strings.TrimSpace(query)); strings.Contains(lower, "fail") || strings.Contains(lower, "test") || strings.Contains(lower, "validate") {
		parts = append(parts, "- Repair rule: determine whether the issue is incorrect store logic, stale expectation, or missing evidence before changing the operation.")
	}
	return strings.Join(parts, "\n")
}

func (c *Capability) EvaluateStepCompletion(query string, state capabilities.RoutingState, actionText string, validation []string, toolCalls []ai.ToolCall, outcomeFacts []string) bool {
	_ = query
	_ = state
	text := strings.ToLower(strings.TrimSpace(actionText + " " + strings.Join(validation, " ")))
	for _, toolCall := range toolCalls {
		toolName := strings.ToLower(strings.TrimSpace(toolCall.Name))
		switch {
		case strings.Contains(text, "inspect") || strings.Contains(text, "schema") || strings.Contains(text, "describe"):
			if strings.Contains(toolName, "list_stores") || strings.Contains(toolName, "scan") || strings.Contains(toolName, "describe") {
				return true
			}
		case strings.Contains(text, "plan") || strings.Contains(text, "design"):
			if strings.Contains(toolName, "design_query") {
				return true
			}
		case strings.Contains(text, "execute") || strings.Contains(text, "query") || strings.Contains(text, "verify") || strings.Contains(text, "validate"):
			if strings.Contains(toolName, "execute_script") || strings.Contains(toolName, "verify_results") {
				return true
			}
		}
	}
	return len(toolCalls) > 0 && len(outcomeFacts) > 0
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
		return "For testing or validation requests, investigate the relevant store behavior, validate the expected operation, and prefer targeted fixes that preserve the current data contract."
	}
	return "Use the Stores capability to translate requests into executable store operations and keep the workflow aligned with the current domain context."
}

func (c *Capability) BuildGuidanceProfile(query string, state capabilities.RoutingState) capabilities.GuidanceProfile {
	base := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityLight, "Guide store operations with schema awareness and safe validation")
	base.Principles = []string{"Validate behavior before changing persistence logic"}
	base.ToolSelection = []capabilities.GuidanceRule{{Description: "Prefer inspection and validation tooling before writing or mutating store operations"}}

	profile := capabilities.GuidanceProfile{
		Principles: []string{"Never guess store names, field paths, or join mappings"},
	}.ApplyDefaults(base)

	return profile
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
