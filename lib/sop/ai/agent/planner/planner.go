package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

const (
	PlannerCapabilityID   = "planner"
	PlannerCapabilityName = "Planner"
)

// PlannerCapability is the cold-start planner entry point for coding tasks.
// It creates a persisted PhasePlan that the phase capabilities execute.
type PlannerCapability struct {
	sharedInfra *cw.SharedInfrastructure
}

func NewPlannerCapability(sharedInfra *cw.SharedInfrastructure) *PlannerCapability {
	if sharedInfra == nil {
		sharedInfra = cw.NewDefaultSharedInfrastructure()
	}
	return &PlannerCapability{sharedInfra: sharedInfra}
}

func (c *PlannerCapability) ID() string   { return PlannerCapabilityID }
func (c *PlannerCapability) Name() string { return PlannerCapabilityName }

func (c *PlannerCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{{
			Resource: "memory",
			Actions:  []string{"read", "write"},
			Gate:     capabilities.GateReadAccess,
		}},
	}
}

func (c *PlannerCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil {
		return capabilities.ClassificationResult{}
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		switch hint {
		case "phase_plan_planner", "coding_workflow", "codingworkflow", "repo_edit", "planner":
			return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModePrimary}
		}
	}
	// Domain-agnostic: Planner works for all domains (coding, stores, spaces, desktop, etc.)
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return capabilities.ClassificationResult{}
	}
	// Planning signals across all domains
	for _, signal := range []string{"fix", "debug", "bug", "refactor", "patch", "implement", "modify", "edit", "repo", "repository", "code", "parser", "test", "feature", "design", "plan", "architect", "workflow", "phases", "steps"} {
		if strings.Contains(q, signal) {
			return capabilities.ClassificationResult{Relevant: true, Score: 16, Mode: capabilities.CapabilityModePrimary}
		}
	}
	return capabilities.ClassificationResult{}
}

func (c *PlannerCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Universal phase planner context:\n- Use this planner for tasks that benefit from structured workflow (coding, stores, spaces, desktop automation, etc.).\n- Create a persistent PhasePlan that adapts to task complexity: simple queries may need only 1-2 phases, complex tasks need full workflow.\n- Phase sequence examples: [investigation, execution] for queries, [intake, investigation, planning, execution, review, delivery] for full SDLC.\n- The domain determines which tools are available in each phase."
}

func (c *PlannerCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "For new coding tasks, start by creating a PhasePlan with the create_phase_plan tool before taking implementation actions. Persist the plan into the current work stream so the later phase capabilities can continue the workflow.\n\n" + cw.CodeFormatInstructions
}

func (c *PlannerCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{
		Name:        "create_phase_plan",
		Description: "Creates a phase-based plan for a coding task and stores it in the current work stream.",
		Schema:      `{"type":"object","properties":{"query":{"type":"string","description":"The user request or task to plan"},"reasoning":{"type":"string","description":"Why this phase sequence was chosen"},"confidence":{"type":"string","description":"Confidence level for the plan"},"checkpoints":{"type":"array","items":{"type":"string"},"description":"Optional checkpoint names such as after_investigation"}},"required":["query"]}`,
	}}
}

func (c *PlannerCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	return nil
}

func (c *PlannerCapability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (c *PlannerCapability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	_ = ctx
	_ = query
	_ = state
	_ = result
	return nil
}

func (c *PlannerCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name != "create_phase_plan" {
		return "", fmt.Errorf("unsupported tool %q", toolName)
	}
	query := firstStringArg(args, "query", "task", "description")
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	reasoning := firstStringArg(args, "reasoning")
	if strings.TrimSpace(reasoning) == "" {
		reasoning = "Cold-start coding requests benefit from a structured progression through intake, investigation, planning, execution, review, and delivery."
	}
	confidence := firstStringArg(args, "confidence")
	if strings.TrimSpace(confidence) == "" {
		confidence = "medium"
	}
	checkpoints := collectCheckpointNames(args)
	if len(checkpoints) == 0 {
		checkpoints = []string{"after_investigation", "after_execution"}
	}
	checkpointMap := make(map[string]bool, len(checkpoints))
	for _, checkpoint := range checkpoints {
		checkpointMap[checkpoint] = true
	}
	phases := buildPhasePlanSequence(query)
	if c.sharedInfra != nil && c.sharedInfra.STMAccessor != nil {
		stm := c.sharedInfra.STMAccessor()
		if stm != nil {
			if err := stm.CreatePhasePlan(phases, checkpointMap, reasoning, confidence); err != nil {
				return "", fmt.Errorf("create phase plan: %w", err)
			}
		}
	}
	return fmt.Sprintf("✅ PHASE PLAN CREATED\n\nPlanned phases: %s\nCheckpoints: %s\nReasoning: %s\nConfidence: %s",
		strings.Join(phases, " → "),
		strings.Join(checkpoints, ", "),
		reasoning,
		confidence), nil
}

func buildPhasePlanSequence(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(q, "review") || strings.Contains(q, "validate") || strings.Contains(q, "check") {
		return []string{"intake", "investigation", "review", "delivery"}
	}
	return []string{"intake", "investigation", "planning", "execution", "review", "delivery"}
}

func collectCheckpointNames(args map[string]any) []string {
	if args == nil {
		return nil
	}
	if raw, ok := args["checkpoints"].([]string); ok {
		return raw
	}
	if raw, ok := args["checkpoints"].([]any); ok {
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func firstStringArg(args map[string]any, keys ...string) string {
	if args == nil {
		return ""
	}
	for _, key := range keys {
		if raw, ok := args[key]; ok {
			if s, ok := raw.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
