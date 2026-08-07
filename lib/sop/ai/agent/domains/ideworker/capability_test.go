package ideworker

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

type fakeBackend struct {
	inspectState     SessionState
	inspectErr       error
	lastRequest      TaskRequest
	result           string
	err              error
	delegationID     string
	delegationStatus DelegationStatus
	delegationErr    error
	lastDelegationID string
	cancelled        bool
}

func (f *fakeBackend) InspectSession(ctx context.Context, sessionID string) (SessionState, error) {
	_ = ctx
	if f.inspectState.SessionID == "" {
		f.inspectState.SessionID = sessionID
	}
	return f.inspectState, f.inspectErr
}

func (f *fakeBackend) ExecuteTask(ctx context.Context, req TaskRequest) (string, error) {
	_ = ctx
	f.lastRequest = req
	return f.result, f.err
}

func (f *fakeBackend) StartTask(ctx context.Context, req TaskRequest) (DelegationHandle, error) {
	_ = ctx
	f.lastRequest = req
	if f.delegationErr != nil {
		return DelegationHandle{}, f.delegationErr
	}
	if f.delegationID == "" {
		f.delegationID = "delegation-1"
	}
	f.lastDelegationID = f.delegationID
	return DelegationHandle{ID: f.delegationID}, nil
}

func (f *fakeBackend) GetTaskStatus(ctx context.Context, delegationID string) (DelegationStatus, error) {
	_ = ctx
	f.lastDelegationID = delegationID
	if f.delegationErr != nil {
		return DelegationStatus{}, f.delegationErr
	}
	status := f.delegationStatus
	if status.ID == "" {
		status.ID = delegationID
	}
	if status.Status == "" {
		status.Status = "running"
	}
	return status, nil
}

func (f *fakeBackend) CancelTask(ctx context.Context, delegationID string) (DelegationStatus, error) {
	_ = ctx
	f.lastDelegationID = delegationID
	f.cancelled = true
	if f.delegationErr != nil {
		return DelegationStatus{}, f.delegationErr
	}
	status := f.delegationStatus
	if status.ID == "" {
		status.ID = delegationID
	}
	status.Status = "cancelled"
	status.Cancelled = true
	return status, nil
}

func TestCapabilityInspectSessionIncludesStructuredContext(t *testing.T) {
	backend := &fakeBackend{inspectState: SessionState{
		Connected:          true,
		IDE:                "vscode",
		WorkerCapabilities: []string{"apply_patch", "read_context"},
	}}
	cap := NewCapability()
	cap.SetBackend(backend)

	payload := &ai.SessionPayload{
		SessionID: "sess-1",
		Variables: map[string]any{
			"continue_mode":           "apply",
			"coding_workflow_profile": "refactoring",
			"requested_phase":         "execution",
			"continue_supported_phases": []any{
				map[string]any{
					"id":              "execution",
					"label":           "Execution & Implementation",
					"continue_modes":  []any{"agent"},
					"available_tools": []any{"read_file", "multi_edit", "run_terminal_command"},
				},
			},
			"continue_available_tools":       []string{"read_file", "multi_edit", "get_problems"},
			"continue_phase_tools":           []string{"read_file", "multi_edit", "run_terminal_command"},
			"continue_phase_available_tools": []string{"read_file", "multi_edit"},
			"continue_tool_choice":           "multi_edit",
			"workspace_root":                 "/repo",
			"active_file":                    "payments/service.go",
			"selected_text":                  "func processPayment()",
			"open_files":                     []string{"payments/service.go", "payments/service_test.go"},
		},
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	result, err := cap.ExecuteTool(ctx, "inspect_continue_worker", nil, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("inspect_continue_worker failed: %v", err)
	}
	for _, want := range []string{
		"connected: yes",
		"ide: vscode",
		"workflow_profile: refactoring",
		"available_tools: read_file, multi_edit, get_problems",
		"phase_tools: read_file, multi_edit, run_terminal_command",
		"phase_available_tools: read_file, multi_edit",
		"tool_choice: multi_edit",
		"supported_phases:",
		"execution (Execution & Implementation) modes=agent tools=read_file,multi_edit,run_terminal_command",
		"active_file: payments/service.go",
		"open_files: payments/service.go, payments/service_test.go",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected inspect output to contain %q, got %q", want, result)
		}
	}
}

func TestCapabilityDelegateTaskUsesSessionDefaults(t *testing.T) {
	backend := &fakeBackend{result: "delegated"}
	cap := NewCapability()
	cap.SetBackend(backend)

	payload := &ai.SessionPayload{
		SessionID:        "sess-2",
		CurrentUserQuery: "Apply the prepared patch",
		Variables: map[string]any{
			"continue_mode":                   "apply",
			"llm_provider":                    "anthropic",
			"llm_model":                       "claude-sonnet-4-5",
			"requested_phase":                 "execution",
			"continue_available_tools":        []string{"read_file", "multi_edit"},
			"continue_phase_tools":            []string{"read_file", "multi_edit", "run_terminal_command"},
			"continue_phase_available_tools":  []string{"read_file", "multi_edit"},
			"continue_tool_choice":            "multi_edit",
			"workspace_root":                  "/repo",
			"active_file":                     "payments/service.go",
			"selected_text":                   "func processPayment()",
			"open_files":                      []string{"payments/service.go"},
			"continue_team_practices":         []string{"Preserve public interfaces", "Add targeted tests"},
			"continue_success_criteria":       []string{"Compilation stays green"},
			"continue_expected_result_format": "structured_execution_result",
		},
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	result, err := cap.ExecuteTool(ctx, "delegate_continue_task", map[string]any{
		"task":            "Apply the prepared patch",
		"requested_phase": "execution",
		"guidance": map[string]any{
			"team_practices":         []any{"Use existing logger"},
			"success_criteria":       []any{"Tests updated"},
			"constraints":            []any{"Do not widen API surface"},
			"implementation_notes":   []any{"Prefer repo conventions over generic style"},
			"expected_result_format": "structured_execution_result",
			"reference_context": []any{
				map[string]any{"kind": "ltm_note", "source": "Team Patterns/Payments", "title": "Payments practices", "content": "Prefer explicit error wrapping."},
			},
		},
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("delegate_continue_task failed: %v", err)
	}
	if !strings.Contains(result, "delegation started") {
		t.Fatalf("unexpected backend result: %q", result)
	}
	if backend.lastRequest.SessionID != "sess-2" {
		t.Fatalf("expected session id sess-2, got %q", backend.lastRequest.SessionID)
	}
	if backend.lastRequest.Mode != "apply" || backend.lastRequest.ActiveFile != "payments/service.go" || backend.lastRequest.WorkspaceRoot != "/repo" {
		t.Fatalf("unexpected delegated request: %#v", backend.lastRequest)
	}
	if backend.lastRequest.Provider != "anthropic" || backend.lastRequest.Model != "claude-sonnet-4-5" {
		t.Fatalf("expected provider/model to propagate from session payload, got %#v", backend.lastRequest)
	}
	if len(backend.lastRequest.AvailableTools) != 2 || backend.lastRequest.AvailableTools[0] != "read_file" || backend.lastRequest.AvailableTools[1] != "multi_edit" {
		t.Fatalf("expected available tool metadata to propagate, got %#v", backend.lastRequest.AvailableTools)
	}
	if backend.lastRequest.RequestedPhase != "execution" {
		t.Fatalf("expected requested phase to propagate, got %#v", backend.lastRequest)
	}
	if len(backend.lastRequest.AllowedTools) != 2 || backend.lastRequest.AllowedTools[0] != "read_file" || backend.lastRequest.AllowedTools[1] != "multi_edit" {
		t.Fatalf("expected bounded allowed tool metadata to propagate, got %#v", backend.lastRequest.AllowedTools)
	}
	if len(backend.lastRequest.PhaseTools) != 3 || backend.lastRequest.ToolChoice != "multi_edit" {
		t.Fatalf("expected phase tool metadata to propagate, got %#v", backend.lastRequest)
	}
	if len(backend.lastRequest.OpenFiles) != 1 || backend.lastRequest.OpenFiles[0] != "payments/service.go" {
		t.Fatalf("unexpected open files: %#v", backend.lastRequest.OpenFiles)
	}
	if len(backend.lastRequest.Guidance.TeamPractices) != 1 || backend.lastRequest.Guidance.TeamPractices[0] != "Use existing logger" {
		t.Fatalf("expected explicit team practices guidance to override session defaults, got %#v", backend.lastRequest.Guidance)
	}
	if len(backend.lastRequest.Guidance.SuccessCriteria) != 1 || backend.lastRequest.Guidance.SuccessCriteria[0] != "Tests updated" {
		t.Fatalf("expected success criteria guidance to propagate, got %#v", backend.lastRequest.Guidance)
	}
	if len(backend.lastRequest.Guidance.ReferenceContext) != 1 || backend.lastRequest.Guidance.ReferenceContext[0].Source != "Team Patterns/Payments" {
		t.Fatalf("expected reference context guidance to propagate, got %#v", backend.lastRequest.Guidance.ReferenceContext)
	}
}

func TestCapabilityExposeTools_ProvidesSingleDelegationTool(t *testing.T) {
	cap := NewCapability()
	tools := cap.ExposeTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 Ask-visible tool, got %d", len(tools))
	}
	if tools[0].Name != "delegate_continue_task" {
		t.Fatalf("expected delegate_continue_task as the only Ask-visible tool, got %#v", tools)
	}
}

func TestCapabilityDelegationLifecycleUsesBackend(t *testing.T) {
	backend := &fakeBackend{delegationStatus: DelegationStatus{Status: "running", PartialResult: "half done"}}
	cap := NewCapability()
	cap.SetBackend(backend)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{SessionID: "sess-3"})

	startResult, err := cap.ExecuteTool(ctx, "delegate_continue_task", map[string]any{"task": "Apply the patch"}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("delegate_continue_task failed: %v", err)
	}
	if !strings.Contains(startResult, "delegation_id") || !strings.Contains(startResult, "delegation-1") {
		t.Fatalf("expected delegation handle in result, got %q", startResult)
	}

	statusResult, err := cap.ExecuteTool(ctx, "get_continue_delegation_status", map[string]any{"delegation_id": "delegation-1"}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("get_continue_delegation_status failed: %v", err)
	}
	if !strings.Contains(statusResult, "half done") {
		t.Fatalf("expected partial result in status, got %q", statusResult)
	}

	cancelResult, err := cap.ExecuteTool(ctx, "cancel_continue_delegation", map[string]any{"delegation_id": "delegation-1"}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("cancel_continue_delegation failed: %v", err)
	}
	if !strings.Contains(cancelResult, "cancelled") {
		t.Fatalf("expected cancellation state in result, got %q", cancelResult)
	}
	if !backend.cancelled {
		t.Fatalf("expected backend cancel to be invoked")
	}
}

func TestCapabilityDelegateTaskGeneratesSOPTraceID(t *testing.T) {
	backend := &fakeBackend{}
	cap := NewCapability()
	cap.SetBackend(backend)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{SessionID: "sess-root"})

	if _, err := cap.ExecuteTool(ctx, "delegate_continue_task", map[string]any{"task": "Apply the patch"}, capabilities.RoutingState{}); err != nil {
		t.Fatalf("delegate_continue_task failed: %v", err)
	}
	if !ai.IsSOPParticipantTraceID(backend.lastRequest.TraceID) {
		t.Fatalf("expected SOP-generated trace_id, got %q", backend.lastRequest.TraceID)
	}
}

func TestCapabilityDelegateTaskPreservesExistingTraceID(t *testing.T) {
	backend := &fakeBackend{}
	cap := NewCapability()
	cap.SetBackend(backend)
	payload := &ai.SessionPayload{
		SessionID: "sess-root-2",
		Variables: map[string]any{ai.ParticipantTraceIDVariableKey: "external-trace-123"},
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	if _, err := cap.ExecuteTool(ctx, "delegate_continue_task", map[string]any{"task": "Apply the patch"}, capabilities.RoutingState{}); err != nil {
		t.Fatalf("delegate_continue_task failed: %v", err)
	}
	if backend.lastRequest.TraceID != "external-trace-123" {
		t.Fatalf("expected existing trace_id to propagate, got %q", backend.lastRequest.TraceID)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestCapabilityLegacyAliasesStillWork(t *testing.T) {
	backend := &fakeBackend{result: "delegated"}
	cap := NewCapability()
	cap.SetBackend(backend)

	payload := &ai.SessionPayload{SessionID: "sess-legacy"}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	if _, err := cap.ExecuteTool(ctx, "inspect_ide_worker", nil, capabilities.RoutingState{}); err != nil {
		t.Fatalf("inspect_ide_worker alias failed: %v", err)
	}
	if _, err := cap.ExecuteTool(ctx, "delegate_ide_coding_task", map[string]any{"task": "Apply the prepared patch"}, capabilities.RoutingState{}); err != nil {
		t.Fatalf("delegate_ide_coding_task alias failed: %v", err)
	}
}

func TestCapabilityClassifyContinueBridgePromptAsCodingSupport(t *testing.T) {
	cap := NewCapability()
	query := strings.Join([]string{
		"Implement the payment fix",
		"[continue_bridge=true]",
		"[continue_mode=apply]",
		"[coding_workflow_profile=debugging]",
		"[requested_phase=execution]",
		"[continue_phase_available_tools=read_file,multi_edit]",
	}, "\n")

	result := cap.Classify(query, capabilities.RoutingState{Domain: "coding", CapabilityHint: "execution"})
	if !result.Relevant {
		t.Fatalf("expected Continue bridge prompt to be relevant")
	}
	if result.Score < 32 {
		t.Fatalf("expected elevated score for Continue bridge prompt, got %d", result.Score)
	}
	if len(result.Hints) != 1 || result.Hints[0] != "execution" {
		t.Fatalf("expected execution hint from requested phase, got %#v", result.Hints)
	}
	if len(result.Domains) != 1 || result.Domains[0] != "coding" {
		t.Fatalf("expected coding domain for Continue bridge prompt, got %#v", result.Domains)
	}
	if result.Mode != capabilities.CapabilityModeSupporting {
		t.Fatalf("expected supporting classification mode for Continue bridge prompt, got %v", result.Mode)
	}
}

func TestCapabilityBuildInstructionsReflectsContinueBridgeMetadata(t *testing.T) {
	cap := NewCapability()
	query := strings.Join([]string{
		"Review the patch outcome",
		"[continue_bridge=true]",
		"[requested_phase=review]",
		"[continue_phase_tools=read_file,get_problems,run_terminal_command]",
		"[continue_phase_available_tools=read_file,get_problems]",
		"[continue_tool_choice=get_problems]",
	}, "\n")

	contextText := cap.BuildContext(query, capabilities.RoutingState{})
	for _, want := range []string{
		"requested_phase: review",
		"continue_allowed_tools: read_file, get_problems",
		"continue_tool_choice: get_problems",
		"delegation_contract:",
	} {
		if !strings.Contains(contextText, want) {
			t.Fatalf("expected dynamic context to contain %q, got %q", want, contextText)
		}
	}

	instructions := cap.BuildInstructions(query, capabilities.RoutingState{})
	for _, want := range []string{
		"normal Ask turns",
		"never as a workflow handoff",
		"Current Continue phase: review",
		"Bound Continue to these tools for this delegated turn when possible: read_file, get_problems.",
		"Preferred Continue tool for the delegated turn: get_problems.",
		"instructions for Continue's Ask pipeline",
		"After Continue returns the bounded result",
		"structured execution result",
		"team practices from LTM",
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("expected dynamic instructions to contain %q, got %q", want, instructions)
		}
	}
}

func TestCapabilityBuildContextEmphasizesAskControllerForContinueBridge(t *testing.T) {
	cap := NewCapability()
	query := strings.Join([]string{
		"Apply the prepared patch",
		"[continue_bridge=true]",
		"[requested_phase=execution]",
	}, "\n")

	contextText := cap.BuildContext(query, capabilities.RoutingState{Domain: "coding", CapabilityHint: "execution"})
	for _, want := range []string{
		"normal Ask turns",
		"never transfers workflow control",
		"delegate one bounded objective",
		"resume Ask-side orchestration",
	} {
		if !strings.Contains(contextText, want) {
			t.Fatalf("expected BuildContext to contain %q, got %q", want, contextText)
		}
	}
}
