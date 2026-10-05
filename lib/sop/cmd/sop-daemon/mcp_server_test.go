package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

func TestMCPServerListsGuidanceTool(t *testing.T) {
	service := &localRAGService{continueState: newContinueBridgeSessionStore()}
	mux := http.NewServeMux()
	registerMCPRoute(mux, nil, service)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["jsonrpc"] != "2.0" {
		t.Fatalf("unexpected response: %#v", response)
	}
	result := response["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 5 || tools[0].(map[string]any)["name"] != "get_task_guidance" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}

func TestMCPServerReturnsScopedTaskGuidance(t *testing.T) {
	service := &localRAGService{continueState: newContinueBridgeSessionStore()}
	service.ensureContinueSessionStore().Put(continueSessionSnapshot{
		SessionID:       "session-1",
		RequestID:       "request-1",
		Status:          "in_progress",
		Phase:           "execution",
		Mode:            "act",
		WorkflowProfile: "test_fix",
		Message:         "Fix the failing tests",
		WorkspaceRoot:   "/tmp/workspace",
		ActiveFile:      "app.py",
		OpenFiles:       []string{"app.py", "test_app.py"},
	})
	result, err := mcpTaskGuidance(service, map[string]any{"session_id": "session-1"})
	if err != nil {
		t.Fatalf("mcpTaskGuidance failed: %v", err)
	}
	if !strings.Contains(result, "Fix the failing tests") || !strings.Contains(result, "test_fix") {
		t.Fatalf("unexpected guidance: %s", result)
	}
}

func TestMCPServerReturnsClineWorkstreamGuidanceWithoutContinueSnapshot(t *testing.T) {
	service := &localRAGService{clineTaskContexts: &clineTaskContextStore{contexts: make(map[string]clineTaskContext)}}
	service.ensureClineTaskContextStore().PutFromRequest(ideworker.TaskRequest{
		RequestID:      "request-cline-1",
		SessionID:      "sop-session-1",
		WorkstreamID:   "workstream-cline-1",
		Task:           "Fix the parser tests",
		WorkspaceRoot:  "/tmp/workspace-cline",
		ActiveFile:     "parser.go",
		OpenFiles:      []string{"parser.go", "parser_test.go"},
		RequestedPhase: "execution",
	}, "in_progress", "Fix the parser tests")

	result, err := mcpTaskGuidance(service, map[string]any{
		"session_id":    "sop-session-1",
		"workstream_id": "workstream-cline-1",
	})
	if err != nil {
		t.Fatalf("mcpTaskGuidance for Cline workstream failed: %v", err)
	}
	for _, want := range []string{"Fix the parser tests", "parser.go", "workstream-cline-1", "execution"} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected Cline task guidance to contain %q, got %s", want, result)
		}
	}
}

func TestMCPServerRecordsValidationInSessionContext(t *testing.T) {
	service := &localRAGService{continueState: newContinueBridgeSessionStore()}
	service.ensureContinueSessionStore().Put(continueSessionSnapshot{SessionID: "session-1"})
	result, err := mcpRecordValidation(service, map[string]any{
		"session_id": "session-1",
		"status":     "passed",
		"summary":    "go test ./... passed",
		"command":    "go test ./...",
	})
	if err != nil {
		t.Fatalf("mcpRecordValidation failed: %v", err)
	}
	if !strings.Contains(result, "passed") {
		t.Fatalf("unexpected validation result: %s", result)
	}
	snapshot, ok := service.ensureContinueSessionStore().Get("session-1")
	if !ok || snapshot.Validation["status"] != "passed" {
		t.Fatalf("validation was not stored: %#v", snapshot)
	}
}

func TestMCPServerRecordsValidationInClineWorkstream(t *testing.T) {
	service := &localRAGService{clineTaskContexts: &clineTaskContextStore{contexts: make(map[string]clineTaskContext)}}
	service.ensureClineTaskContextStore().PutFromRequest(ideworker.TaskRequest{
		RequestID: "request-cline-validation", SessionID: "sop-session-validation", WorkstreamID: "workstream-validation", Task: "run tests",
	}, "in_progress", "run tests")
	_, err := mcpRecordValidation(service, map[string]any{
		"session_id": "sop-session-validation", "workstream_id": "workstream-validation",
		"status": "passed", "summary": "go test passed", "command": "go test ./...",
	})
	if err != nil {
		t.Fatalf("record Cline validation: %v", err)
	}
	taskContext, ok := service.ensureClineTaskContextStore().Get("workstream-validation", "sop-session-validation")
	if !ok || taskContext.Validation["status"] != "passed" || taskContext.Validation["command"] != "go test ./..." {
		t.Fatalf("Cline validation missing from workstream context: %#v", taskContext)
	}
	if _, ok := service.ensureContinueSessionStore().Get("sop-session-validation"); ok {
		t.Fatal("Cline validation must not create or depend on a Continue session snapshot")
	}
}

func TestMCPRestoreRejectsMismatchedClineWorkstreamSession(t *testing.T) {
	workspace := t.TempDir()
	service := &localRAGService{clineTaskContexts: &clineTaskContextStore{contexts: make(map[string]clineTaskContext)}}
	service.ensureClineTaskContextStore().PutFromRequest(ideworker.TaskRequest{
		SessionID: "sop-session-authorized", WorkstreamID: "workstream-authorized", WorkspaceRoot: workspace,
	}, "in_progress", "task")
	_, err := mcpRestoreUndoTransaction(context.Background(), service, map[string]any{
		"workspace_root": workspace, "session_id": "sop-session-other", "workstream_id": "workstream-authorized",
		"transaction_id": "txn-1", "confirm": true,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected mismatched SOP session to fail authorization, got %v", err)
	}
}

func TestMCPRestoreRequiresExplicitConfirmation(t *testing.T) {
	workspace := t.TempDir()
	_, err := mcpRestoreUndoTransaction(context.Background(), &localRAGService{}, map[string]any{
		"workspace_root": workspace,
		"transaction_id": "txn-1",
		"confirm":        false,
	})
	if err == nil || !strings.Contains(err.Error(), "confirm=true") {
		t.Fatalf("expected explicit confirmation error, got %v", err)
	}
}
