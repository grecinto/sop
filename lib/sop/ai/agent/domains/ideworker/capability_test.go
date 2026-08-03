package ideworker

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

type fakeBackend struct {
	inspectState SessionState
	inspectErr   error
	lastRequest  TaskRequest
	result       string
	err          error
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
			"workspace_root":          "/repo",
			"active_file":             "payments/service.go",
			"selected_text":           "func processPayment()",
			"open_files":              []string{"payments/service.go", "payments/service_test.go"},
		},
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	result, err := cap.ExecuteTool(ctx, "inspect_ide_worker", nil, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("inspect_ide_worker failed: %v", err)
	}
	for _, want := range []string{
		"connected: yes",
		"ide: vscode",
		"workflow_profile: refactoring",
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
			"continue_mode":  "apply",
			"workspace_root": "/repo",
			"active_file":    "payments/service.go",
			"selected_text":  "func processPayment()",
			"open_files":     []string{"payments/service.go"},
		},
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	result, err := cap.ExecuteTool(ctx, "delegate_ide_coding_task", map[string]any{
		"task": "Apply the prepared patch",
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("delegate_ide_coding_task failed: %v", err)
	}
	if result != "delegated" {
		t.Fatalf("unexpected backend result: %q", result)
	}
	if backend.lastRequest.SessionID != "sess-2" {
		t.Fatalf("expected session id sess-2, got %q", backend.lastRequest.SessionID)
	}
	if backend.lastRequest.Mode != "apply" || backend.lastRequest.ActiveFile != "payments/service.go" || backend.lastRequest.WorkspaceRoot != "/repo" {
		t.Fatalf("unexpected delegated request: %#v", backend.lastRequest)
	}
	if len(backend.lastRequest.OpenFiles) != 1 || backend.lastRequest.OpenFiles[0] != "payments/service.go" {
		t.Fatalf("unexpected open files: %#v", backend.lastRequest.OpenFiles)
	}
}
