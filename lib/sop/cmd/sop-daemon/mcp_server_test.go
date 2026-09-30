package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
