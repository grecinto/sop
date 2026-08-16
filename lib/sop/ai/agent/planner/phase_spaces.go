package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// SpacesPhase implements phase capability for spaces domain.
// Handles knowledge base/documentation operations.
type SpacesPhase struct {
	BasePhase
}

// newSpacesPhase creates a spaces domain phase
func newSpacesPhase(phaseID string, infra *SharedInfrastructure) *SpacesPhase {
	name, description := phaseMetadata(phaseID)
	base := newBasePhase(phaseID, name, description, "spaces", infra)
	base.setAuthoringProfile(PhaseAuthoringProfile{
		Domain:     "spaces",
		Principles: []string{"Treat persisted knowledge changes as high-impact operations"},
		Guardrails: []capabilities.GuidanceRule{{Description: "Do not refresh or delete large knowledge surfaces without explicit intent", AppliesTo: []string{"spaces"}}},
		Instructions: map[string]string{
			"investigation": "Use Investigation to inspect the relevant space content, configuration, and scope before changing anything.",
			"planning":      "Use Planning to define a narrow change scope and the expected knowledge impact before applying mutations.",
			"execution":     "Use Execution to apply the planned knowledge change with explicit validation and minimal blast radius.",
			"review":        "Use Review to confirm the final knowledge state and ensure the change stayed within the intended scope.",
		},
		PhaseRules: map[string][]capabilities.GuidanceRule{
			"investigation": {{Description: "Inspect the relevant space content and configuration before changing anything", AppliesTo: []string{"investigation"}}},
			"planning":      {{Description: "Define a narrow change scope and expected knowledge impact before applying mutations", AppliesTo: []string{"planning"}}},
			"execution":     {{Description: "Apply the planned change narrowly and validate the resulting knowledge state", AppliesTo: []string{"execution"}}},
			"review":        {{Description: "Confirm the final knowledge state stayed within the intended scope", AppliesTo: []string{"review"}}},
		},
	})
	return &SpacesPhase{BasePhase: base}
}

func (p *SpacesPhase) BuildContext(query string, state capabilities.RoutingState) string {
	return fmt.Sprintf("PHASE: %s for spaces/knowledge base domain", p.name)
}

func (p *SpacesPhase) ExposeTools() []capabilities.ToolDefinition {
	// Common capture/state tools
	tools := []capabilities.ToolDefinition{{
		Name:        p.captureTool,
		Description: fmt.Sprintf("Captures %s phase artifact to memory", p.name),
		Schema:      captureToolSchema(),
	}, {
		Name:        p.stateTool,
		Description: fmt.Sprintf("Returns current persisted state for %s phase", p.name),
		Schema:      stateToolSchema(),
	}}

	// Add phase-specific spaces tools
	tools = append(tools, p.getSpacesTools()...)
	return tools
}

func (p *SpacesPhase) getSpacesTools() []capabilities.ToolDefinition {
	spaceTaskSchema := `{
  "type": "object",
  "required": ["space_name"],
  "properties": {
    "space_name": {"type": "string", "description": "Target space/knowledge base name"},
    "category": {"type": "string", "description": "Optional category path"}
  }
}`

	switch p.phaseID {
	case "investigation":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("list_spaces"),
			Description: "Lists available spaces/knowledge bases. READ-ONLY.",
			Schema:      spaceTaskSchema,
		}, {
			Name:        p.phaseToolName("search_content"),
			Description: "Searches content within a space. READ-ONLY.",
			Schema:      spaceTaskSchema,
		}}

	case "planning":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("design_mutation"),
			Description: "Designs a knowledge mutation or retrieval-change plan before execution.",
			Schema:      spaceTaskSchema,
		}}

	case "execution":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("create_content"),
			Description: "Creates or updates content in a space.",
			Schema:      spaceTaskSchema,
		}}

	default:
		return nil
	}
}

func (p *SpacesPhase) BuildInstructions(query string, state capabilities.RoutingState) string {
	return fmt.Sprintf("Use %s capability to work with spaces/knowledge bases", p.name)
}

func (p *SpacesPhase) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state

	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return "", fmt.Errorf("empty tool name")
	}

	// Common tools
	if name == strings.ToLower(p.captureTool) {
		return p.runCapture(args)
	}
	if name == strings.ToLower(p.stateTool) {
		return p.runState(args)
	}

	// Spaces-specific tools
	switch name {
	case strings.ToLower(p.phaseToolName("list_spaces")):
		return p.runListSpaces(args)
	case strings.ToLower(p.phaseToolName("search_content")):
		return p.runSearchContent(args)
	case strings.ToLower(p.phaseToolName("design_mutation")):
		return p.runDesignMutation(args)
	case strings.ToLower(p.phaseToolName("create_content")):
		return p.runCreateContent(args)
	default:
		return "", fmt.Errorf("unsupported tool %q in spaces domain", toolName)
	}
}

// Spaces-specific tool execution methods (placeholders for now)

func (p *SpacesPhase) runListSpaces(args map[string]any) (string, error) {
	category := readPhaseString(args, "category", "")
	if category != "" {
		return fmt.Sprintf("Available spaces in category '%s':\n- %s/space_alpha\n- %s/space_beta", category, category, category), nil
	}
	return "Available spaces:\n- space_alpha\n- space_beta\n- space_gamma", nil
}

func (p *SpacesPhase) runSearchContent(args map[string]any) (string, error) {
	spaceName := readPhaseString(args, "space_name", "")
	if spaceName == "" {
		return "", fmt.Errorf("space_name is required")
	}

	return fmt.Sprintf("Search results in space '%s':\n- Document 1\n- Document 2", spaceName), nil
}

func (p *SpacesPhase) runDesignMutation(args map[string]any) (string, error) {
	spaceName := readPhaseString(args, "space_name", "")
	if spaceName == "" {
		return "", fmt.Errorf("space_name is required")
	}
	category := readPhaseString(args, "category", "")
	if p.sharedInfra != nil && p.sharedInfra.Planner != nil {
		task := cw.Task{
			Description: fmt.Sprintf("Design the knowledge change plan for space %q category %q", spaceName, category),
			TaskType:    "planning",
			State:       cw.RunState{Metadata: map[string]string{"domain": "spaces", "space_name": spaceName, "category": category, "phase": p.phaseID}},
		}
		pack := cw.ContextPack{RepositorySummary: fmt.Sprintf("space=%s category=%s", spaceName, category)}
		plan, err := p.sharedInfra.Planner.Plan(task, task.State, pack)
		if err == nil && len(plan.Steps) > 0 {
			basePlan := cw.BasePlanFromStructuredPlan(plan)
			structuredData := map[string]any{"structured_plan": plan, "base_plan": basePlan}
			_, _ = p.runCapture(map[string]any{
				"summary":         fmt.Sprintf("Space plan for %s", spaceName),
				"status":          "ready_for_execution",
				"plan":            toStringSlice(basePlan),
				"structured_data": structuredData,
			})
			if len(basePlan) > 0 {
				return fmt.Sprintf("Execution Plan:\n%s", strings.Join(basePlan, "\n")), nil
			}
		}
	}
	plan := fmt.Sprintf("Knowledge Change Plan for space '%s'", spaceName)
	if category != "" {
		plan += fmt.Sprintf(" (category: %s)", category)
	}
	plan += ":\n\nSteps:\n1. Inspect the target knowledge scope\n2. Define the intended mutation or retrieval change\n3. Validate the resulting knowledge state and semantic scope"
	return plan, nil
}

func (p *SpacesPhase) runCreateContent(args map[string]any) (string, error) {
	spaceName := readPhaseString(args, "space_name", "")
	if spaceName == "" {
		return "", fmt.Errorf("space_name is required")
	}

	category := readPhaseString(args, "category", "general")
	return fmt.Sprintf("Content created in space '%s' (category: %s)", spaceName, category), nil
}
