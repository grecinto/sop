package ideworker

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	CapabilityID   = "ide_worker"
	CapabilityName = "IDE Worker Integration"
)

type SessionState struct {
	SessionID          string
	Connected          bool
	IDE                string
	WorkerCapabilities []string
	Mode               string
	WorkflowProfile    string
	RequestedPhase     string
	WorkspaceRoot      string
	ActiveFile         string
	SelectedText       string
	OpenFiles          []string
}

type TaskRequest struct {
	SessionID     string
	Task          string
	Mode          string
	WorkspaceRoot string
	ActiveFile    string
	SelectedText  string
	OpenFiles     []string
}

type Backend interface {
	InspectSession(ctx context.Context, sessionID string) (SessionState, error)
	ExecuteTask(ctx context.Context, req TaskRequest) (string, error)
}

type Capability struct {
	backend Backend
}

func NewCapability() *Capability { return &Capability{} }

func (c *Capability) SetBackend(backend Backend) {
	c.backend = backend
}

func (c *Capability) ID() string   { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }

func (c *Capability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: true,
		Tier:             "base",
		RequiredGate:     capabilities.GateReadAccess,
		Permissions: []capabilities.Permission{{
			Resource: "ide_worker",
			Actions:  []string{"inspect", "delegate"},
			Gate:     capabilities.GateReadAccess,
		}},
	}
}

func (c *Capability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	combined := strings.ToLower(strings.TrimSpace(strings.Join([]string{state.Domain, state.CapabilityHint, query}, " ")))
	if combined == "" {
		return capabilities.ClassificationResult{}
	}

	if containsAny(combined,
		"[continue_mode=",
		"coding_workflow_profile",
		"workspace_root",
		"active_file",
		"selected_text",
		"open_files",
		"continue",
		"editor",
		"ide worker",
		"patch",
		"diff",
		"refactor",
		"implement",
		"review",
		"test",
		"workspace",
	) {
		return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModeSupporting}
	}

	if containsAny(strings.ToLower(strings.TrimSpace(state.Domain)), "desktop", "coding") || containsAny(strings.ToLower(strings.TrimSpace(state.CapabilityHint)), "planning", "execution", "review", "delivery") {
		return capabilities.ClassificationResult{Relevant: true, Score: 12, Mode: capabilities.CapabilityModeSupporting}
	}

	return capabilities.ClassificationResult{}
}

func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	_ = query
	_ = state
	return "Structured Context: IDE Worker Integration\n- When an IDE-native worker is connected for this session, Ask can inspect editor context and delegate a focused coding subtask without giving up daemon control."
}

func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{
		{
			Name:        "inspect_ide_worker",
			Description: "Inspect whether the current Ask session has an IDE-native worker attached, including editor type, advertised worker capabilities, and structured workspace context.",
			Schema:      `{"type":"object","properties":{}}`,
			Tags:        []string{"R"},
		},
		{
			Name:        "delegate_ide_coding_task",
			Description: "Delegate a focused coding subtask to the connected IDE worker for editor-native execution such as patch application, context-aware editing, or diff-oriented review.",
			Schema:      `{"type":"object","properties":{"task":{"type":"string","description":"Focused coding task to delegate to the IDE worker"},"mode":{"type":"string","description":"Worker mode hint such as plan, edit, apply, review, or test"},"workspace_root":{"type":"string","description":"Workspace root for the delegated task"},"active_file":{"type":"string","description":"Primary file for the delegated task"},"selected_text":{"type":"string","description":"Selected code snippet or region for the task"},"open_files":{"type":"array","items":{"type":"string"},"description":"Open files that provide extra editor context"}},"required":["task"]}`,
			Tags:        []string{"R", "U"},
		},
	}
}

func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	_ = query
	_ = state
	return `IDE Worker Guidance:
- Use inspect_ide_worker to see whether this session has a connected editor worker and what capabilities it supports.
- Use delegate_ide_coding_task only for focused editor-native subtasks such as applying a prepared patch, performing a local diff-aware edit, or running an IDE-scoped coding step.
- Keep Ask as the controller. Delegate one bounded subtask, consume the result, and then continue the larger workflow in the daemon.
- If no worker is connected or required capabilities are missing, continue with daemon-native tools instead.`
}

func (c *Capability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = state
	switch toolName {
	case "inspect_ide_worker":
		return c.inspectSession(ctx)
	case "delegate_ide_coding_task":
		return c.delegateTask(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
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

func (c *Capability) inspectSession(ctx context.Context) (string, error) {
	payload := ai.GetSessionPayload(ctx)
	if payload == nil || strings.TrimSpace(payload.SessionID) == "" {
		return "No IDE worker session context is available for this Ask turn.", nil
	}

	state := SessionState{SessionID: strings.TrimSpace(payload.SessionID)}
	if payload.Variables != nil {
		state.Mode = readString(payload.Variables, "continue_mode")
		state.WorkflowProfile = readString(payload.Variables, "coding_workflow_profile")
		state.RequestedPhase = readString(payload.Variables, "requested_phase")
		state.WorkspaceRoot = readString(payload.Variables, "workspace_root")
		state.ActiveFile = readString(payload.Variables, "active_file")
		state.SelectedText = readString(payload.Variables, "selected_text")
		state.OpenFiles = readStringSlice(payload.Variables["open_files"])
	}

	if c.backend == nil {
		return formatSessionState(state), nil
	}

	backendState, err := c.backend.InspectSession(ctx, state.SessionID)
	if err != nil {
		return "", err
	}
	state = mergeSessionState(state, backendState)
	return formatSessionState(state), nil
}

func (c *Capability) delegateTask(ctx context.Context, args map[string]any) (string, error) {
	if c.backend == nil {
		return "", fmt.Errorf("IDE worker backend is not available")
	}

	payload := ai.GetSessionPayload(ctx)
	req := TaskRequest{}
	if payload != nil {
		req.SessionID = strings.TrimSpace(payload.SessionID)
		if payload.Variables != nil {
			req.Mode = readString(payload.Variables, "continue_mode")
			req.WorkspaceRoot = readString(payload.Variables, "workspace_root")
			req.ActiveFile = readString(payload.Variables, "active_file")
			req.SelectedText = readString(payload.Variables, "selected_text")
			req.OpenFiles = readStringSlice(payload.Variables["open_files"])
		}
	}

	if task, ok := args["task"].(string); ok {
		req.Task = strings.TrimSpace(task)
	}
	if mode, ok := args["mode"].(string); ok && strings.TrimSpace(mode) != "" {
		req.Mode = strings.TrimSpace(mode)
	}
	if workspaceRoot, ok := args["workspace_root"].(string); ok && strings.TrimSpace(workspaceRoot) != "" {
		req.WorkspaceRoot = strings.TrimSpace(workspaceRoot)
	}
	if activeFile, ok := args["active_file"].(string); ok && strings.TrimSpace(activeFile) != "" {
		req.ActiveFile = strings.TrimSpace(activeFile)
	}
	if selectedText, ok := args["selected_text"].(string); ok && strings.TrimSpace(selectedText) != "" {
		req.SelectedText = strings.TrimSpace(selectedText)
	}
	if openFiles, ok := args["open_files"]; ok {
		req.OpenFiles = readStringSlice(openFiles)
	}
	if req.Mode == "" {
		req.Mode = "coding"
	}
	if req.Task == "" {
		return "", fmt.Errorf("task is required")
	}
	if req.SessionID == "" {
		return "", fmt.Errorf("session id is required to delegate an IDE worker task")
	}

	return c.backend.ExecuteTask(ctx, req)
}

func mergeSessionState(base SessionState, overlay SessionState) SessionState {
	if overlay.SessionID != "" {
		base.SessionID = overlay.SessionID
	}
	if overlay.Connected {
		base.Connected = true
	}
	if overlay.IDE != "" {
		base.IDE = overlay.IDE
	}
	if len(overlay.WorkerCapabilities) > 0 {
		base.WorkerCapabilities = append([]string(nil), overlay.WorkerCapabilities...)
	}
	if overlay.Mode != "" {
		base.Mode = overlay.Mode
	}
	if overlay.WorkflowProfile != "" {
		base.WorkflowProfile = overlay.WorkflowProfile
	}
	if overlay.RequestedPhase != "" {
		base.RequestedPhase = overlay.RequestedPhase
	}
	if overlay.WorkspaceRoot != "" {
		base.WorkspaceRoot = overlay.WorkspaceRoot
	}
	if overlay.ActiveFile != "" {
		base.ActiveFile = overlay.ActiveFile
	}
	if overlay.SelectedText != "" {
		base.SelectedText = overlay.SelectedText
	}
	if len(overlay.OpenFiles) > 0 {
		base.OpenFiles = append([]string(nil), overlay.OpenFiles...)
	}
	return base
}

func formatSessionState(state SessionState) string {
	b := strings.Builder{}
	b.WriteString("IDE worker session:\n")
	b.WriteString(fmt.Sprintf("- session_id: %s\n", defaultString(state.SessionID, "unknown")))
	if state.Connected {
		b.WriteString("- connected: yes\n")
	} else {
		b.WriteString("- connected: no\n")
	}
	if state.IDE != "" {
		b.WriteString(fmt.Sprintf("- ide: %s\n", state.IDE))
	}
	if len(state.WorkerCapabilities) > 0 {
		b.WriteString(fmt.Sprintf("- worker_capabilities: %s\n", strings.Join(state.WorkerCapabilities, ", ")))
	}
	if state.Mode != "" {
		b.WriteString(fmt.Sprintf("- mode: %s\n", state.Mode))
	}
	if state.WorkflowProfile != "" {
		b.WriteString(fmt.Sprintf("- workflow_profile: %s\n", state.WorkflowProfile))
	}
	if state.RequestedPhase != "" {
		b.WriteString(fmt.Sprintf("- requested_phase: %s\n", state.RequestedPhase))
	}
	if state.WorkspaceRoot != "" {
		b.WriteString(fmt.Sprintf("- workspace_root: %s\n", state.WorkspaceRoot))
	}
	if state.ActiveFile != "" {
		b.WriteString(fmt.Sprintf("- active_file: %s\n", state.ActiveFile))
	}
	if state.SelectedText != "" {
		b.WriteString(fmt.Sprintf("- selected_text: %s\n", state.SelectedText))
	}
	if len(state.OpenFiles) > 0 {
		b.WriteString(fmt.Sprintf("- open_files: %s\n", strings.Join(state.OpenFiles, ", ")))
	}
	if !state.Connected {
		b.WriteString("- guidance: no IDE worker is attached right now; continue with daemon-native tools unless a worker registers later.\n")
	}
	return strings.TrimSpace(b.String())
}

func readString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func readStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				items = append(items, strings.TrimSpace(text))
			}
		}
		return items
	default:
		return nil
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func containsAny(text string, signals ...string) bool {
	for _, signal := range signals {
		if signal != "" && strings.Contains(text, signal) {
			return true
		}
	}
	return false
}
