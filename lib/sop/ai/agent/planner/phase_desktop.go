package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// DesktopPhase implements phase capability for desktop automation domain.
// Handles UI automation, command execution, file operations.
type DesktopPhase struct {
	BasePhase
}

// newDesktopPhase creates a desktop domain phase
func newDesktopPhase(phaseID string, infra *SharedInfrastructure) *DesktopPhase {
	name, description := phaseMetadata(phaseID)
	return &DesktopPhase{
		BasePhase: newBasePhase(phaseID, name, description, "desktop", infra),
	}
}

func (p *DesktopPhase) BuildContext(query string, state capabilities.RoutingState) string {
	return fmt.Sprintf("PHASE: %s for desktop automation domain", p.name)
}

func (p *DesktopPhase) ExposeTools() []capabilities.ToolDefinition {
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

	// Add phase-specific desktop tools
	tools = append(tools, p.getDesktopTools()...)
	return tools
}

func (p *DesktopPhase) getDesktopTools() []capabilities.ToolDefinition {
	desktopTaskSchema := `{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {"type": "string", "description": "Command or action to execute"},
    "target": {"type": "string", "description": "Target application or window"}
  }
}`

	switch p.phaseID {
	case "investigation":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("list_windows"),
			Description: "Lists open windows and applications. READ-ONLY.",
			Schema:      desktopTaskSchema,
		}, {
			Name:        p.phaseToolName("get_screen_info"),
			Description: "Gets screen/display information. READ-ONLY.",
			Schema:      desktopTaskSchema,
		}}

	case "execution":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("execute_command"),
			Description: "Executes desktop command or automation.",
			Schema:      desktopTaskSchema,
		}}

	default:
		return nil
	}
}

func (p *DesktopPhase) BuildInstructions(query string, state capabilities.RoutingState) string {
	return fmt.Sprintf("Use %s capability to work with desktop automation", p.name)
}

func (p *DesktopPhase) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
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

	// Desktop-specific tools
	switch name {
	case strings.ToLower(p.phaseToolName("list_windows")):
		return p.runListWindows(args)
	case strings.ToLower(p.phaseToolName("get_screen_info")):
		return p.runGetScreenInfo(args)
	case strings.ToLower(p.phaseToolName("execute_command")):
		return p.runExecuteCommand(args)
	default:
		return "", fmt.Errorf("unsupported tool %q in desktop domain", toolName)
	}
}

// Desktop-specific tool execution methods (placeholders for now)

func (p *DesktopPhase) runListWindows(args map[string]any) (string, error) {
	// TODO: Implement actual window listing
	return "Open windows:\n- Terminal (active)\n- VS Code\n- Chrome", nil
}

func (p *DesktopPhase) runGetScreenInfo(args map[string]any) (string, error) {
	// TODO: Implement actual screen info retrieval
	return "Screen info:\n- Display 1: 1920x1080\n- Display 2: 2560x1440", nil
}

func (p *DesktopPhase) runExecuteCommand(args map[string]any) (string, error) {
	command := readPhaseString(args, "command", "")
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	// TODO: Implement actual command execution
	return fmt.Sprintf("Executed desktop command: %s", command), nil
}
