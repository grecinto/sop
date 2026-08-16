package ideworker

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	CapabilityID                = "ide_worker"
	CapabilityName              = "Continue Delegation"
	inspectContinueToolName     = "inspect_continue_worker"
	delegateContinueToolName    = "delegate_continue_task"
	getDelegationStatusToolName = "get_continue_delegation_status"
	cancelDelegationToolName    = "cancel_continue_delegation"
	legacyInspectToolName       = "inspect_ide_worker"
	legacyDelegateToolName      = "delegate_ide_coding_task"
)

type SessionState struct {
	SessionID           string
	Connected           bool
	IDE                 string
	WorkerCapabilities  []string
	Mode                string
	WorkflowProfile     string
	RequestedPhase      string
	SupportedPhases     []PhaseDefinition
	AvailableTools      []string
	PhaseTools          []string
	PhaseAvailableTools []string
	ToolChoice          string
	WorkspaceRoot       string
	ActiveFile          string
	SelectedText        string
	OpenFiles           []string
}

type PhaseDefinition struct {
	ID             string
	Label          string
	ContinueModes  []string
	AvailableTools []string
}

type DelegationReference struct {
	Kind     string
	Source   string
	Title    string
	Content  string
	Metadata map[string]any
}

type DelegationGuidance struct {
	TeamPractices        []string
	SuccessCriteria      []string
	Constraints          []string
	ImplementationNotes  []string
	ExpectedResultFormat string
	ReferenceContext     []DelegationReference
	Metadata             map[string]any
}

type TaskRequest struct {
	RequestID      string
	TraceID        string
	RootRequestID  string
	SessionID      string
	Task           string
	Mode           string
	Provider       string
	Model          string
	RequestedPhase string
	AvailableTools []string
	PhaseTools     []string
	AllowedTools   []string
	ToolChoice     string
	WorkspaceRoot  string
	ActiveFile     string
	SelectedText   string
	OpenFiles      []string
	Guidance       DelegationGuidance
}

type DelegationHandle struct {
	ID      string `json:"delegation_id"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

type DelegationStatus struct {
	ID            string `json:"delegation_id,omitempty"`
	Status        string `json:"status,omitempty"`
	Progress      string `json:"progress,omitempty"`
	PartialResult string `json:"partial_result,omitempty"`
	Message       string `json:"message,omitempty"`
	Error         string `json:"error,omitempty"`
	Cancelled     bool   `json:"cancelled,omitempty"`
}

type Backend interface {
	InspectSession(ctx context.Context, sessionID string) (SessionState, error)
	ExecuteTask(ctx context.Context, req TaskRequest) (string, error)
	StartTask(ctx context.Context, req TaskRequest) (DelegationHandle, error)
	GetTaskStatus(ctx context.Context, delegationID string) (DelegationStatus, error)
	CancelTask(ctx context.Context, delegationID string) (DelegationStatus, error)
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
	metadata := parseContinuePromptMetadata(query)
	combined := strings.ToLower(strings.TrimSpace(strings.Join([]string{state.Domain, state.CapabilityHint, query}, " ")))
	if combined == "" {
		return capabilities.ClassificationResult{}
	}

	if metadata.HasBridgeContext {
		result := capabilities.ClassificationResult{
			Relevant: true,
			Score:    32,
			Mode:     capabilities.CapabilityModeSupporting,
			Domains:  []string{"coding"},
		}
		if metadata.RequestedPhase != "" {
			result.Hints = []string{metadata.RequestedPhase}
		}
		if containsAny(combined, "implement", "patch", "edit", "refactor", "review", "test", "debug") {
			result.Score = 40
		}
		return result
	}

	if containsAny(combined,
		"[continue_mode=",
		"[continue_bridge=",
		"coding_workflow_profile",
		"workspace_root",
		"active_file",
		"selected_text",
		"open_files",
		"continue",
		"editor",
		"ide worker",
		"ide delegation",
	) {
		return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModeSupporting}
	}

	stateDomain := strings.ToLower(strings.TrimSpace(state.Domain))
	stateHint := strings.ToLower(strings.TrimSpace(state.CapabilityHint))
	if containsAny(stateDomain, "continue", "ide_worker", "editor") || containsAny(stateHint, "continue", "ide_worker", "editor") {
		return capabilities.ClassificationResult{Relevant: true, Score: 12, Mode: capabilities.CapabilityModeSupporting}
	}

	return capabilities.ClassificationResult{}
}

func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
	metadata := parseContinuePromptMetadata(query)
	_ = state
	b := strings.Builder{}
	b.WriteString("Structured Context: Continue Delegation\n")
	b.WriteString("- Continue-originated requests are still normal Ask turns. Bridge metadata provides delegation constraints and context, but it never transfers workflow control away from the daemon Ask pipeline.\n")
	b.WriteString("- When a Continue-backed IDE worker is available for this session, Ask may delegate one bounded IDE coding subtask to Continue's own Ask/tool loop without giving up daemon control.\n")
	b.WriteString("- This capability is LAO-oriented: the daemon LLM classifies and orchestrates the broader request, and Continue performs only the delegated coding slice for that step.\n")
	b.WriteString("- Ask-visible tool: delegate_continue_task. Use other daemon capabilities normally before or after delegation when the request needs broader workflow support.\n")
	b.WriteString("- Delegation discipline: delegate one bounded objective, consume the returned result, then resume Ask-side orchestration for the next decision.\n")
	b.WriteString("- Expected Continue result: a final structured execution report covering modified_files, CRUD operation per file, diff deltas or artifact references, and execution metadata.\n")
	b.WriteString("- Rich delegation parameters are supported: team practices, success criteria, constraints, implementation notes, expected result format, and reference context can be passed as structured guidance.\n")
	if metadata.HasBridgeContext {
		if metadata.Mode != "" {
			b.WriteString(fmt.Sprintf("- continue_mode: %s\n", metadata.Mode))
		}
		if metadata.WorkflowProfile != "" {
			b.WriteString(fmt.Sprintf("- workflow_profile: %s\n", metadata.WorkflowProfile))
		}
		if metadata.RequestedPhase != "" {
			b.WriteString(fmt.Sprintf("- requested_phase: %s\n", metadata.RequestedPhase))
		}
		if len(metadata.AvailableTools) > 0 {
			b.WriteString(fmt.Sprintf("- continue_available_tools: %s\n", strings.Join(metadata.AvailableTools, ", ")))
		}
		if len(metadata.AllowedTools) > 0 {
			b.WriteString(fmt.Sprintf("- continue_allowed_tools: %s\n", strings.Join(metadata.AllowedTools, ", ")))
		}
		if metadata.ToolChoice != "" {
			b.WriteString(fmt.Sprintf("- continue_tool_choice: %s\n", metadata.ToolChoice))
		}
		b.WriteString("- delegation_contract: compose a bounded Continue task, pass continuity/tool constraints through delegate_continue_task, accept streamed progress during execution, then continue daemon task-mode orchestration with the final structured result.\n")
	}
	return strings.TrimSpace(b.String())
}

func (c *Capability) BuildGuidanceProfile(query string, state capabilities.RoutingState) capabilities.GuidanceProfile {
	base := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityLight, "Guide bounded IDE delegation and preserve Ask-side orchestration")
	base.Principles = []string{"Keep Ask as the controller for the larger workflow", "Preserve structured success criteria and constraints for the delegated turn"}
	base.Guardrails = []capabilities.GuidanceRule{{Description: "Do not hand off the entire workflow to Continue when the daemon still needs to orchestrate the next step"}}
	base.ToolSelection = []capabilities.GuidanceRule{{Description: "Prefer delegation when the task is editor-native and bounded, otherwise use the daemon-native capability path"}}

	profile := capabilities.GuidanceProfile{
		Principles: []string{"Delegate only one bounded coding slice at a time"},
	}.ApplyDefaults(base)

	return profile
}

func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{
		{
			Name:        delegateContinueToolName,
			Description: "Delegate one bounded coding subtask to the connected Continue worker so Continue can run its own Ask/tool loop locally and return the finished result to AI Studio.",
			Schema:      `{"type":"object","properties":{"task":{"type":"string","description":"Focused coding task to delegate to the IDE worker"},"mode":{"type":"string","description":"Worker mode hint such as plan, edit, apply, review, or test"},"requested_phase":{"type":"string","description":"Optional explicit workflow phase such as investigation, planning, execution, review, or delivery"},"allowed_tools":{"type":"array","items":{"type":"string"},"description":"Optional bounded tool subset that the remote Continue Ask turn should be allowed to use"},"tool_choice":{"type":"string","description":"Optional preferred Continue tool name for the delegated turn"},"provider":{"type":"string","description":"Optional provider override to preserve the selected LLM family during delegation"},"model":{"type":"string","description":"Optional model override to preserve the selected model during delegation"},"workspace_root":{"type":"string","description":"Workspace root for the delegated task"},"active_file":{"type":"string","description":"Primary file for the delegated task"},"selected_text":{"type":"string","description":"Selected code snippet or region for the task"},"open_files":{"type":"array","items":{"type":"string"},"description":"Open files that provide extra editor context"},"target_file":{"type":"string","description":"Specific file target for edit-style delegation"},"target_symbol":{"type":"string","description":"Symbol or function name to focus the edit on"},"surrounding_snippet":{"type":"string","description":"Snippet around the edit target to provide context to the IDE worker"},"change_intent":{"type":"string","description":"Intent or expected outcome for the requested edit"},"guidance":{"type":"object","description":"Structured guidance for Continue's Ask pipeline.","properties":{"team_practices":{"type":"array","items":{"type":"string"}},"success_criteria":{"type":"array","items":{"type":"string"}},"constraints":{"type":"array","items":{"type":"string"}},"implementation_notes":{"type":"array","items":{"type":"string"}},"expected_result_format":{"type":"string"},"reference_context":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string"},"source":{"type":"string"},"title":{"type":"string"},"content":{"type":"string"}}}},"metadata":{"type":"object"}}}},"required":["task"]}`,
			Tags:        []string{"R", "U"},
		},
	}
}

func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
	metadata := parseContinuePromptMetadata(query)
	_ = state
	instructions := `Continue Delegation Guidance:
- This capability exposes one Ask-visible delegation tool: delegate_continue_task.
- Continue-originated requests remain normal Ask turns. Treat Continue bridge metadata as delegation constraints, never as a workflow handoff.
- Before any edit, state the immediate objective and expected scope in one sentence. Prefer the smallest valid change over a broad rewrite.
- Use delegate_continue_task only for one bounded coding subtask that Continue should complete through its own local Ask/tool loop.
- Keep Ask as the controller. Classify and orchestrate the larger workflow in the daemon, delegate the IDE coding slice to Continue, consume the result, then continue with the remaining work.
- Continue is the coding executor for the delegated slice, not the outer workflow controller.
- Do not choreograph Continue with low-level tool sequences or delegation lifecycle loops. Delegate one objective, let Continue execute it, then continue orchestration in Ask.
- Expect Continue to finish with a structured execution result that lists modified_files, CRUD operations, diff deltas or diff artifact references, and relevant metadata.
- When available, pass structured guidance such as team practices from LTM, success criteria, constraints, implementation notes, expected result format, and reference context as parameters to Continue's Ask.
- If Continue is not available or the request does not benefit from editor-native coding execution, continue with daemon-native tools or other capabilities instead.`
	if !metadata.HasBridgeContext {
		return instructions
	}
	boundedTools := metadata.AllowedTools
	if len(boundedTools) == 0 {
		boundedTools = metadata.PhaseTools
	}
	parts := []string{instructions}
	if metadata.RequestedPhase != "" {
		parts = append(parts, fmt.Sprintf("Current Continue continuity hint: %s. Keep delegation scoped to that bounded slice unless the user changes the ask.", metadata.RequestedPhase))
	}
	if len(boundedTools) > 0 {
		parts = append(parts, fmt.Sprintf("Bound Continue to these tools for this delegated turn when possible: %s.", strings.Join(boundedTools, ", ")))
	}
	if metadata.ToolChoice != "" {
		parts = append(parts, fmt.Sprintf("Preferred Continue tool for the delegated turn: %s.", metadata.ToolChoice))
	}
	parts = append(parts, "When delegating, write the task as instructions for Continue's Ask pipeline, not as a sequence of low-level tool calls for AI Studio to imitate.")
	parts = append(parts, "Delegate the coding objective, expected scope, and success criteria. Let Continue decide how to use its internal IDE tools.")
	parts = append(parts, "After Continue returns the bounded result, continue with Ask-side task-mode orchestration and decide the next step in the daemon.")
	return strings.Join(parts, "\n")
}

func (c *Capability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = state
	switch toolName {
	case inspectContinueToolName, legacyInspectToolName:
		return c.inspectSession(ctx)
	case delegateContinueToolName, legacyDelegateToolName:
		return c.delegateTask(ctx, args)
	case getDelegationStatusToolName:
		return c.getDelegationStatus(ctx, args)
	case cancelDelegationToolName:
		return c.cancelDelegation(ctx, args)
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
		state.SupportedPhases = readPhaseDefinitions(payload.Variables["continue_supported_phases"])
		state.AvailableTools = readStringSlice(payload.Variables["continue_available_tools"])
		state.PhaseTools = readStringSlice(payload.Variables["continue_phase_tools"])
		state.PhaseAvailableTools = readStringSlice(payload.Variables["continue_phase_available_tools"])
		state.ToolChoice = readString(payload.Variables, "continue_tool_choice")
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
			req.RequestID = readString(payload.Variables, ai.ParticipantRequestIDVariableKey)
			req.TraceID = readString(payload.Variables, ai.ParticipantTraceIDVariableKey)
			if req.TraceID == "" {
				req.TraceID = readString(payload.Variables, "root_request_id")
			}
			req.RootRequestID = req.TraceID
			req.Mode = readString(payload.Variables, "continue_mode")
			req.Provider = readString(payload.Variables, "llm_provider")
			req.Model = readString(payload.Variables, "llm_model")
			req.RequestedPhase = readString(payload.Variables, "requested_phase")
			req.AvailableTools = readStringSlice(payload.Variables["continue_available_tools"])
			req.PhaseTools = readStringSlice(payload.Variables["continue_phase_tools"])
			req.AllowedTools = readStringSlice(payload.Variables["continue_phase_available_tools"])
			req.ToolChoice = readString(payload.Variables, "continue_tool_choice")
			req.WorkspaceRoot = readString(payload.Variables, "workspace_root")
			req.ActiveFile = readString(payload.Variables, "active_file")
			req.SelectedText = readString(payload.Variables, "selected_text")
			req.OpenFiles = readStringSlice(payload.Variables["open_files"])
			req.Guidance = readDelegationGuidanceFromVariables(payload.Variables)
		}
	}

	if task, ok := args["task"].(string); ok {
		req.Task = strings.TrimSpace(task)
	}
	if mode, ok := args["mode"].(string); ok && strings.TrimSpace(mode) != "" {
		req.Mode = strings.TrimSpace(mode)
	}
	if requestedPhase, ok := args["requested_phase"].(string); ok && strings.TrimSpace(requestedPhase) != "" {
		req.RequestedPhase = strings.TrimSpace(requestedPhase)
	}
	if allowedTools, ok := args["allowed_tools"]; ok {
		req.AllowedTools = readStringSlice(allowedTools)
	}
	if toolChoice, ok := args["tool_choice"].(string); ok && strings.TrimSpace(toolChoice) != "" {
		req.ToolChoice = strings.TrimSpace(toolChoice)
	}
	if provider, ok := args["provider"].(string); ok && strings.TrimSpace(provider) != "" {
		req.Provider = strings.TrimSpace(provider)
	}
	if model, ok := args["model"].(string); ok && strings.TrimSpace(model) != "" {
		req.Model = strings.TrimSpace(model)
	}
	if workspaceRoot, ok := args["workspace_root"].(string); ok && strings.TrimSpace(workspaceRoot) != "" {
		req.WorkspaceRoot = strings.TrimSpace(workspaceRoot)
	}
	if activeFile, ok := args["active_file"].(string); ok && strings.TrimSpace(activeFile) != "" {
		req.ActiveFile = strings.TrimSpace(activeFile)
	}
	if targetFile, ok := args["target_file"].(string); ok && strings.TrimSpace(targetFile) != "" {
		req.ActiveFile = strings.TrimSpace(targetFile)
	}
	if selectedText, ok := args["selected_text"].(string); ok && strings.TrimSpace(selectedText) != "" {
		req.SelectedText = strings.TrimSpace(selectedText)
	}
	if surroundingSnippet, ok := args["surrounding_snippet"].(string); ok && strings.TrimSpace(surroundingSnippet) != "" {
		req.SelectedText = strings.TrimSpace(surroundingSnippet)
	}
	if targetSymbol, ok := args["target_symbol"].(string); ok && strings.TrimSpace(targetSymbol) != "" {
		if req.Guidance.Metadata == nil {
			req.Guidance.Metadata = map[string]any{}
		}
		req.Guidance.Metadata["target_symbol"] = strings.TrimSpace(targetSymbol)
	}
	if changeIntent, ok := args["change_intent"].(string); ok && strings.TrimSpace(changeIntent) != "" {
		if req.Guidance.Metadata == nil {
			req.Guidance.Metadata = map[string]any{}
		}
		req.Guidance.Metadata["change_intent"] = strings.TrimSpace(changeIntent)
	}
	if req.ActiveFile != "" {
		if req.Guidance.Metadata == nil {
			req.Guidance.Metadata = map[string]any{}
		}
		if _, exists := req.Guidance.Metadata["target_file"]; !exists {
			req.Guidance.Metadata["target_file"] = req.ActiveFile
		}
	}
	if openFiles, ok := args["open_files"]; ok {
		req.OpenFiles = readStringSlice(openFiles)
	}
	if guidance, ok := args["guidance"]; ok {
		req.Guidance = mergeDelegationGuidance(req.Guidance, readDelegationGuidance(guidance))
	}
	if teamPractices, ok := args["team_practices"]; ok {
		req.Guidance.TeamPractices = readStringSlice(teamPractices)
	}
	if successCriteria, ok := args["success_criteria"]; ok {
		req.Guidance.SuccessCriteria = readStringSlice(successCriteria)
	}
	if constraints, ok := args["constraints"]; ok {
		req.Guidance.Constraints = readStringSlice(constraints)
	}
	if implementationNotes, ok := args["implementation_notes"]; ok {
		req.Guidance.ImplementationNotes = readStringSlice(implementationNotes)
	}
	if expectedResultFormat, ok := args["expected_result_format"].(string); ok && strings.TrimSpace(expectedResultFormat) != "" {
		req.Guidance.ExpectedResultFormat = strings.TrimSpace(expectedResultFormat)
	}
	if referenceContext, ok := args["reference_context"]; ok {
		req.Guidance.ReferenceContext = readDelegationReferences(referenceContext)
	}
	if req.Mode == "" {
		req.Mode = "coding"
	}
	if req.TraceID == "" {
		req.TraceID = ai.NewParticipantTraceID()
		req.RootRequestID = req.TraceID
		if payload != nil {
			if payload.Variables == nil {
				payload.Variables = make(map[string]any)
			}
			payload.Variables[ai.ParticipantTraceIDVariableKey] = req.TraceID
			payload.Variables["root_request_id"] = req.TraceID
		}
	}
	if req.Task == "" {
		return "", fmt.Errorf("task is required")
	}
	if req.SessionID == "" {
		return "", fmt.Errorf("session id is required to delegate an IDE worker task")
	}

	handle, err := c.backend.StartTask(ctx, req)
	if err != nil {
		return "", err
	}
	return formatDelegationHandle(handle), nil
}

func (c *Capability) getDelegationStatus(ctx context.Context, args map[string]any) (string, error) {
	if c.backend == nil {
		return "", fmt.Errorf("IDE worker backend is not available")
	}
	delegationID := readDelegationID(args)
	if strings.TrimSpace(delegationID) == "" {
		return "", fmt.Errorf("delegation_id is required")
	}
	status, err := c.backend.GetTaskStatus(ctx, delegationID)
	if err != nil {
		return "", err
	}
	return formatDelegationStatus(status), nil
}

func (c *Capability) cancelDelegation(ctx context.Context, args map[string]any) (string, error) {
	if c.backend == nil {
		return "", fmt.Errorf("IDE worker backend is not available")
	}
	delegationID := readDelegationID(args)
	if strings.TrimSpace(delegationID) == "" {
		return "", fmt.Errorf("delegation_id is required")
	}
	status, err := c.backend.CancelTask(ctx, delegationID)
	if err != nil {
		return "", err
	}
	return formatDelegationStatus(status), nil
}

func readDelegationID(args map[string]any) string {
	if args == nil {
		return ""
	}
	if raw, ok := args["delegation_id"].(string); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}

func formatDelegationHandle(handle DelegationHandle) string {
	b := strings.Builder{}
	b.WriteString("delegation started\n")
	b.WriteString(fmt.Sprintf("- delegation_id: %s\n", defaultString(handle.ID, "unknown")))
	if handle.Status != "" {
		b.WriteString(fmt.Sprintf("- status: %s\n", handle.Status))
	}
	if handle.Message != "" {
		b.WriteString(fmt.Sprintf("- message: %s\n", handle.Message))
	}
	return strings.TrimSpace(b.String())
}

func formatDelegationStatus(status DelegationStatus) string {
	b := strings.Builder{}
	b.WriteString("delegation status\n")
	b.WriteString(fmt.Sprintf("- delegation_id: %s\n", defaultString(status.ID, "unknown")))
	if status.Status != "" {
		b.WriteString(fmt.Sprintf("- status: %s\n", status.Status))
	}
	if status.Progress != "" {
		b.WriteString(fmt.Sprintf("- progress: %s\n", status.Progress))
	}
	if status.PartialResult != "" {
		b.WriteString(fmt.Sprintf("- partial_result: %s\n", status.PartialResult))
	}
	if status.Message != "" {
		b.WriteString(fmt.Sprintf("- message: %s\n", status.Message))
	}
	if status.Error != "" {
		b.WriteString(fmt.Sprintf("- error: %s\n", status.Error))
	}
	if status.Cancelled {
		b.WriteString("- cancelled: yes\n")
	}
	return strings.TrimSpace(b.String())
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
	if len(overlay.SupportedPhases) > 0 {
		base.SupportedPhases = append([]PhaseDefinition(nil), overlay.SupportedPhases...)
	}
	if len(overlay.AvailableTools) > 0 {
		base.AvailableTools = append([]string(nil), overlay.AvailableTools...)
	}
	if len(overlay.PhaseTools) > 0 {
		base.PhaseTools = append([]string(nil), overlay.PhaseTools...)
	}
	if len(overlay.PhaseAvailableTools) > 0 {
		base.PhaseAvailableTools = append([]string(nil), overlay.PhaseAvailableTools...)
	}
	if overlay.ToolChoice != "" {
		base.ToolChoice = overlay.ToolChoice
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
	if len(state.AvailableTools) > 0 {
		b.WriteString(fmt.Sprintf("- available_tools: %s\n", strings.Join(state.AvailableTools, ", ")))
	}
	if len(state.PhaseTools) > 0 {
		b.WriteString(fmt.Sprintf("- phase_tools: %s\n", strings.Join(state.PhaseTools, ", ")))
	}
	if len(state.PhaseAvailableTools) > 0 {
		b.WriteString(fmt.Sprintf("- phase_available_tools: %s\n", strings.Join(state.PhaseAvailableTools, ", ")))
	}
	if state.ToolChoice != "" {
		b.WriteString(fmt.Sprintf("- tool_choice: %s\n", state.ToolChoice))
	}
	if len(state.SupportedPhases) > 0 {
		b.WriteString("- supported_phases:\n")
		for _, phase := range state.SupportedPhases {
			line := fmt.Sprintf("  - %s", phase.ID)
			if phase.Label != "" {
				line += fmt.Sprintf(" (%s)", phase.Label)
			}
			if len(phase.ContinueModes) > 0 {
				line += fmt.Sprintf(" modes=%s", strings.Join(phase.ContinueModes, ","))
			}
			if len(phase.AvailableTools) > 0 {
				line += fmt.Sprintf(" tools=%s", strings.Join(phase.AvailableTools, ","))
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
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

func readPhaseDefinitions(value any) []PhaseDefinition {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	phases := make([]PhaseDefinition, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		phase := PhaseDefinition{
			ID:             readString(entry, "id"),
			Label:          readString(entry, "label"),
			ContinueModes:  readStringSlice(entry["continue_modes"]),
			AvailableTools: readStringSlice(entry["available_tools"]),
		}
		if phase.ID == "" {
			continue
		}
		phases = append(phases, phase)
	}
	if len(phases) == 0 {
		return nil
	}
	return phases
}

func readDelegationGuidanceFromVariables(values map[string]any) DelegationGuidance {
	if values == nil {
		return DelegationGuidance{}
	}
	guidance := DelegationGuidance{
		TeamPractices:        readStringSlice(values["continue_team_practices"]),
		SuccessCriteria:      readStringSlice(values["continue_success_criteria"]),
		Constraints:          readStringSlice(values["continue_constraints"]),
		ImplementationNotes:  readStringSlice(values["continue_implementation_notes"]),
		ExpectedResultFormat: readString(values, "continue_expected_result_format"),
		ReferenceContext:     readDelegationReferences(values["continue_reference_context"]),
	}
	if metadata, ok := values["continue_guidance_metadata"].(map[string]any); ok {
		guidance.Metadata = cloneAnyMap(metadata)
	}
	return guidance
}

func readDelegationGuidance(value any) DelegationGuidance {
	entry, ok := value.(map[string]any)
	if !ok {
		return DelegationGuidance{}
	}
	guidance := DelegationGuidance{
		TeamPractices:        readStringSlice(entry["team_practices"]),
		SuccessCriteria:      readStringSlice(entry["success_criteria"]),
		Constraints:          readStringSlice(entry["constraints"]),
		ImplementationNotes:  readStringSlice(entry["implementation_notes"]),
		ExpectedResultFormat: readString(entry, "expected_result_format"),
		ReferenceContext:     readDelegationReferences(entry["reference_context"]),
	}
	if metadata, ok := entry["metadata"].(map[string]any); ok {
		guidance.Metadata = cloneAnyMap(metadata)
	}
	return guidance
}

func mergeDelegationGuidance(base DelegationGuidance, overlay DelegationGuidance) DelegationGuidance {
	if len(overlay.TeamPractices) > 0 {
		base.TeamPractices = append([]string(nil), overlay.TeamPractices...)
	}
	if len(overlay.SuccessCriteria) > 0 {
		base.SuccessCriteria = append([]string(nil), overlay.SuccessCriteria...)
	}
	if len(overlay.Constraints) > 0 {
		base.Constraints = append([]string(nil), overlay.Constraints...)
	}
	if len(overlay.ImplementationNotes) > 0 {
		base.ImplementationNotes = append([]string(nil), overlay.ImplementationNotes...)
	}
	if strings.TrimSpace(overlay.ExpectedResultFormat) != "" {
		base.ExpectedResultFormat = strings.TrimSpace(overlay.ExpectedResultFormat)
	}
	if len(overlay.ReferenceContext) > 0 {
		base.ReferenceContext = append([]DelegationReference(nil), overlay.ReferenceContext...)
	}
	if len(overlay.Metadata) > 0 {
		base.Metadata = cloneAnyMap(overlay.Metadata)
	}
	return base
}

func readDelegationReferences(value any) []DelegationReference {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	refs := make([]DelegationReference, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ref := DelegationReference{
			Kind:    readString(entry, "kind"),
			Source:  readString(entry, "source"),
			Title:   readString(entry, "title"),
			Content: readString(entry, "content"),
		}
		if metadata, ok := entry["metadata"].(map[string]any); ok {
			ref.Metadata = cloneAnyMap(metadata)
		}
		if ref.Kind == "" && ref.Source == "" && ref.Title == "" && ref.Content == "" && len(ref.Metadata) == 0 {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
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

type continuePromptMetadata struct {
	HasBridgeContext bool
	Mode             string
	WorkflowProfile  string
	RequestedPhase   string
	AvailableTools   []string
	PhaseTools       []string
	AllowedTools     []string
	ToolChoice       string
}

func parseContinuePromptMetadata(query string) continuePromptMetadata {
	metadata := continuePromptMetadata{}
	for _, line := range strings.Split(query, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
		key, value, found := strings.Cut(body, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "continue_bridge":
			metadata.HasBridgeContext = strings.EqualFold(value, "true")
		case "continue_mode":
			metadata.Mode = value
			metadata.HasBridgeContext = true
		case "coding_workflow_profile":
			metadata.WorkflowProfile = value
			metadata.HasBridgeContext = true
		case "requested_phase":
			metadata.RequestedPhase = value
			metadata.HasBridgeContext = true
		case "continue_available_tools":
			metadata.AvailableTools = splitCSV(value)
			metadata.HasBridgeContext = true
		case "continue_phase_tools":
			metadata.PhaseTools = splitCSV(value)
			metadata.HasBridgeContext = true
		case "continue_phase_available_tools":
			metadata.AllowedTools = splitCSV(value)
			metadata.HasBridgeContext = true
		case "continue_tool_choice":
			metadata.ToolChoice = value
			metadata.HasBridgeContext = true
		}
	}
	return metadata
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}
