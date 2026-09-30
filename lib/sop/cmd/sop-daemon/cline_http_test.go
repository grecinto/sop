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
}

func (b *recordingClineHTTPBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	return ideworker.SessionState{SessionID: sessionID}, nil
}
func (b *recordingClineHTTPBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	b.request = req
	return b.result, nil
}
func (b *recordingClineHTTPBackend) StartTask(ctx context.Context, req ideworker.TaskRequest) (ideworker.DelegationHandle, error) {
	return ideworker.DelegationHandle{}, nil
}
func (b *recordingClineHTTPBackend) GetTaskStatus(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	return ideworker.DelegationStatus{}, nil
}
func (b *recordingClineHTTPBackend) CancelTask(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	return ideworker.DelegationStatus{}, nil
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
