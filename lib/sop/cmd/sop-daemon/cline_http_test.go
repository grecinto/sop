package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

type clineHTTPBackendStub struct {
	result  string
	request ideworker.TaskRequest
}

func (b clineHTTPBackendStub) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	return ideworker.SessionState{SessionID: sessionID}, nil
}
func (b clineHTTPBackendStub) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	b.request = req
	return b.result, nil
}
func (b clineHTTPBackendStub) StartTask(ctx context.Context, req ideworker.TaskRequest) (ideworker.DelegationHandle, error) {
	return ideworker.DelegationHandle{}, nil
}

type recordingClineHTTPBackend struct {
	result  string
	request ideworker.TaskRequest
	status  ideworker.DelegationStatus
}

func (b *recordingClineHTTPBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	return ideworker.SessionState{SessionID: sessionID}, nil
}
func (b *recordingClineHTTPBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	b.request = req
	return b.result, nil
}
func (b *recordingClineHTTPBackend) StartTask(ctx context.Context, req ideworker.TaskRequest) (ideworker.DelegationHandle, error) {
	b.request = req
	return ideworker.DelegationHandle{ID: "task-1", Status: "running"}, nil
}
func (b *recordingClineHTTPBackend) GetTaskStatus(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	return b.status, nil
}
func (b *recordingClineHTTPBackend) CancelTask(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	b.status = ideworker.DelegationStatus{ID: delegationID, Status: "cancelled", Cancelled: true}
	return b.status, nil
}
func (b clineHTTPBackendStub) GetTaskStatus(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	return ideworker.DelegationStatus{}, nil
}
func (b clineHTTPBackendStub) CancelTask(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	return ideworker.DelegationStatus{}, nil
}

func TestClineHTTPExecuteRejectsMissingFields(t *testing.T) {
	service := &localRAGService{clineBackend: clineHTTPBackendStub{result: `{"status":"completed"}`}}
	req := httptest.NewRequest(http.MethodPost, "/api/cline/execute", strings.NewReader(`{"task":"fix"}`))
	recorder := httptest.NewRecorder()
	service.handleClineExecute(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func TestClineHTTPExecuteForwardsTaskContract(t *testing.T) {
	backend := &recordingClineHTTPBackend{result: `{"status":"completed","summary":"fixed","modified_files":[]}`}
	service := &localRAGService{clineBackend: backend}
	req := httptest.NewRequest(http.MethodPost, "/api/cline/execute", strings.NewReader(`{"request_id":"req-1","session_id":"session-1","task":"fix tests","provider":"openai-native","model":"gpt-5","api_key":"secret","base_url":"https://example.invalid/v1","workspace_root":"/tmp","active_file":"app.py"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	service.handleClineExecute(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"completed"`) {
		t.Fatalf("unexpected response: %d %s", recorder.Code, recorder.Body.String())
	}
	if backend.request.Task != "fix tests" || backend.request.APIKey != "secret" || backend.request.BaseURL != "https://example.invalid/v1" || backend.request.ActiveFile != "app.py" {
		t.Fatalf("request contract was not forwarded: %#v", backend.request)
	}
}

func TestClineHTTPExecuteRejectsUnknownFields(t *testing.T) {
	service := &localRAGService{clineBackend: &recordingClineHTTPBackend{result: `{"status":"completed"}`}}
	req := httptest.NewRequest(http.MethodPost, "/api/cline/execute", strings.NewReader(`{"session_id":"session-1","task":"fix","workspace_root":"/tmp","unexpected":true}`))
	recorder := httptest.NewRecorder()
	service.handleClineExecute(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", recorder.Code)
	}
}

func TestClineHTTPTaskLifecycle(t *testing.T) {
	backend := &recordingClineHTTPBackend{status: ideworker.DelegationStatus{ID: "task-1", Status: "completed", PartialResult: `{"status":"completed","summary":"done"}`}}
	service := &localRAGService{clineBackend: backend}
	start := httptest.NewRequest(http.MethodPost, "/api/cline/tasks", strings.NewReader(`{"session_id":"session-1","task":"fix tests","workspace_root":"/tmp"}`))
	startRecorder := httptest.NewRecorder()
	service.handleClineTaskStart(startRecorder, start)
	if startRecorder.Code != http.StatusAccepted || !strings.Contains(startRecorder.Body.String(), "task-1") {
		t.Fatalf("unexpected start response: %d %s", startRecorder.Code, startRecorder.Body.String())
	}
	status := httptest.NewRequest(http.MethodGet, "/api/cline/tasks/task-1", nil)
	statusRecorder := httptest.NewRecorder()
	service.handleClineTaskStatus(statusRecorder, status)
	if statusRecorder.Code != http.StatusOK || !strings.Contains(statusRecorder.Body.String(), "completed") {
		t.Fatalf("unexpected status response: %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	cancel := httptest.NewRequest(http.MethodPost, "/api/cline/tasks/task-1/cancel", nil)
	cancelRecorder := httptest.NewRecorder()
	service.handleClineTaskCancel(cancelRecorder, cancel)
	if cancelRecorder.Code != http.StatusOK || !strings.Contains(cancelRecorder.Body.String(), "cancelled") {
		t.Fatalf("unexpected cancel response: %d %s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
}

func TestClineMCPContextDoesNotTrustArbitraryHostHeader(t *testing.T) {
	t.Setenv("SOP_AI_STUDIO_MCP_URL", "")
	t.Setenv("SOP_AI_STUDIO_MCP_TOKEN", "")
	request := httptest.NewRequest(http.MethodPost, "http://attacker.invalid/api/ask", nil)
	request.Header.Set("Authorization", "Bearer daemon-token")
	url, token := clineMCPContext(request)
	if url != "http://127.0.0.1:9090/mcp" {
		t.Fatalf("unexpected MCP URL from untrusted Host header: %q", url)
	}
	if token != "daemon-token" {
		t.Fatalf("expected request bearer token, got %q", token)
	}
}

func TestClineMCPContextUsesConfiguredEndpointAndToken(t *testing.T) {
	t.Setenv("SOP_AI_STUDIO_MCP_URL", "https://sop.internal/mcp")
	t.Setenv("SOP_AI_STUDIO_MCP_TOKEN", "configured-token")
	request := httptest.NewRequest(http.MethodPost, "http://localhost:9090/api/ask", nil)
	url, token := clineMCPContext(request)
	if url != "https://sop.internal/mcp" || token != "configured-token" {
		t.Fatalf("unexpected configured MCP context: url=%q token=%q", url, token)
	}
}
