package agent

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

// PlannerCapability is a cold-start planner entry point for coding tasks.
// It is intentionally separate from the existing step-based workflow planner and
// focuses on creating a persisted PhasePlan that the phase capabilities can execute.
type PlannerCapability struct {
	sharedInfra *cw.SharedInfrastructure
}

func NewPlannerCapability(sharedInfra *cw.SharedInfrastructure) *PlannerCapability {
	if sharedInfra == nil {
		sharedInfra = cw.NewDefaultSharedInfrastructure()
	}
	return &PlannerCapability{sharedInfra: sharedInfra}
}

func (c *PlannerCapability) ID() string { return PlannerCapabilityID }

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

	// ✅ Domain-based classification only (LLM-classified domain via ClassifyTaskContext)
	// NO keyword matching - let LLM decide via tool descriptions

	// 1. Explicit capability hint (from session continuity or LLM classification)
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		switch hint {
		case "phase_plan_planner", "coding_workflow", "codingworkflow", "repo_edit", "planner":
			return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModePrimary}
		}
	}

	// 2. Domain-based relevance (domain was LLM-classified in tryColdStartBasedRouting)
	domain := strings.TrimSpace(strings.ToLower(state.Domain))
	if domain == "coding_workflow" || domain == "codingworkflow" {
		// LLM determined this is coding domain - planner is relevant for new coding tasks
		return capabilities.ClassificationResult{Relevant: true, Score: 15, Mode: capabilities.CapabilityModePrimary}
	}

	// 3. Not relevant for other domains - LLM will classify domain correctly
	return capabilities.ClassificationResult{}
}

func (c *PlannerCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Cold-start coding planner context:\n- Use this planner first for new repository or code-editing requests.\n- Create a persistent PhasePlan before the workflow begins.\n- The plan should include intake, investigation, planning, execution, review, and delivery phases with checkpoints."
}

func (c *PlannerCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "For new coding tasks, start by creating a PhasePlan with the create_phase_plan tool before taking implementation actions. Persist the plan into the current work stream so the later phase capabilities can continue the workflow.\n\n" + cw.CodeFormatInstructions
}

func (c *PlannerCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{
		Name:        "create_phase_plan",
		Description: "Creates a phase-based plan for a coding task and stores it in the current work stream.",
		Schema:      `{"type":"object","properties":{"query":{"type":"string","description":"The user request or task to plan"},"reasoning":{"type":"string","description":"Why this phase sequence was chosen"},"confidence":{"type":"string","description":"Confidence level for the plan"},"checkpoints":{"type":"array","items":{"type":"string"},"description":"Optional checkpoint names such as after_investigation"}},"required":["query"]}`,
		Tags:        []string{"C"}, // Creates a new phase plan
	}}
}

func (c *PlannerCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
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
