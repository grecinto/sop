package stores

import (
	"fmt"
	"strings"

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
	return strings.Join([]string{
		"Structured Context: Stores Tools",
		"You are a Stores Database Expert Agent. Translate requests into executable store operations.",
		"CRITICAL RULES:",
		"1. Never guess store names, field names, or join mappings. Use list_stores first whenever schema, relations, or field paths are uncertain.",
		"2. Think through the read/join/filter plan before writing the operation.",
		"3. SOP store queries are not SQL. When the user asks for a query or filter, produce SOP-native JSON tool/script operations, not SQL syntax.",
		"4. When you generate a multi-step store plan using steps such as begin_tx, open_store, scan, filter, sort, project, commit_tx, rollback_tx or return, place those steps inside execute_script.script and call execute_script to submit the plan to the executor. The last step's result is returned automatically or use return to explicitly specify the return value.",
		"5. If execution fails, analyze the error, rewrite the operation, and retry once. If it still fails, ask the user a short clarification question.",
	}, "\n")
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
