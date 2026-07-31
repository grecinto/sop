package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// SpacesPhase implements phase capability for spaces domain.
// Handles knowledge base/documentation operations.
type SpacesPhase struct {
	BasePhase
}

// newSpacesPhase creates a spaces domain phase
func newSpacesPhase(phaseID string, infra *SharedInfrastructure) *SpacesPhase {
	name, description := phaseMetadata(phaseID)
	return &SpacesPhase{
		BasePhase: newBasePhase(phaseID, name, description, "spaces", infra),
	}
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
	case strings.ToLower(p.phaseToolName("create_content")):
		return p.runCreateContent(args)
	default:
		return "", fmt.Errorf("unsupported tool %q in spaces domain", toolName)
	}
}

// Spaces-specific tool execution methods (placeholders for now)

func (p *SpacesPhase) runListSpaces(args map[string]any) (string, error) {
	// TODO: Implement actual spaces listing
	return "Available spaces:\n- space_alpha\n- space_beta\n- space_gamma", nil
}

func (p *SpacesPhase) runSearchContent(args map[string]any) (string, error) {
	spaceName := readPhaseString(args, "space_name", "")
	if spaceName == "" {
		return "", fmt.Errorf("space_name is required")
	}

	// TODO: Implement actual content search
	return fmt.Sprintf("Search results in space '%s':\n- Document 1\n- Document 2", spaceName), nil
}

func (p *SpacesPhase) runCreateContent(args map[string]any) (string, error) {
	spaceName := readPhaseString(args, "space_name", "")
	if spaceName == "" {
		return "", fmt.Errorf("space_name is required")
	}

	// TODO: Implement actual content creation
	return fmt.Sprintf("Content created in space '%s'", spaceName), nil
}
