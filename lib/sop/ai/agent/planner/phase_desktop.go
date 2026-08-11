package planner

import (
	"context"
	"fmt"
	"runtime"
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
	base := newBasePhase(phaseID, name, description, "desktop", infra)
	base.setAuthoringProfile(PhaseAuthoringProfile{
		Domain:     "desktop",
		Principles: []string{"Keep shell actions scoped to the requested workspace or repository"},
		Guardrails: []capabilities.GuidanceRule{{Description: "Do not execute destructive commands without explicit user intent", AppliesTo: []string{"desktop"}}},
		Instructions: map[string]string{
			"investigation": "Use Investigation to inspect the workspace, environment, and relevant automation context before taking action.",
			"planning":      "Use Planning to outline the shell or automation steps explicitly before executing them.",
			"execution":     "Use Execution to run the planned automation step-by-step and retain the output for review.",
			"review":        "Use Review to confirm the automation outcome and capture any follow-up risk.",
		},
		PhaseRules: map[string][]capabilities.GuidanceRule{
			"investigation": {{Description: "Inspect the workspace and environment before executing automation", AppliesTo: []string{"investigation"}}},
			"planning":      {{Description: "Outline the shell or automation steps explicitly before execution", AppliesTo: []string{"planning"}}},
			"execution":     {{Description: "Run the planned automation step-by-step and preserve output for review", AppliesTo: []string{"execution"}}},
			"review":        {{Description: "Confirm the automation outcome and capture any follow-up risk", AppliesTo: []string{"review"}}},
		},
	})
	return &DesktopPhase{BasePhase: base}
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
		return p.runExecuteCommand(ctx, args)
	default:
		return "", fmt.Errorf("unsupported tool %q in desktop domain", toolName)
	}
}

// Desktop-specific tool execution methods (placeholders for now)

func (p *DesktopPhase) runListWindows(args map[string]any) (string, error) {
	_ = args
	return "Open windows:\n- Terminal (active)\n- VS Code\n- Chrome", nil
}

func (p *DesktopPhase) runGetScreenInfo(args map[string]any) (string, error) {
	_ = args
	return fmt.Sprintf("Screen info (platform: %s):\n- Display 1: 1920x1080\n- Display 2: 2560x1440", runtime.GOOS), nil
}

func (p *DesktopPhase) runExecuteCommand(ctx context.Context, args map[string]any) (string, error) {
	command := readPhaseString(args, "command", "")
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	infra := p.ensureSharedInfrastructure()
	if infra.LocalCommandExecutor == nil {
		return "", fmt.Errorf("desktop phase command executor not configured")
	}

	execArgs := map[string]any{}
	for key, value := range args {
		execArgs[key] = value
	}
	if _, ok := execArgs["reasoning"]; !ok {
		execArgs["reasoning"] = "Execute the requested desktop command through the existing local automation path."
	}

	return infra.LocalCommandExecutor(ctx, execArgs)
}
