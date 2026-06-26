package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
)

func TestBuildListenAddr(t *testing.T) {
	if got, want := buildListenAddr("127.0.0.1", 9090), "127.0.0.1:9090"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}

	if got, want := buildListenAddr("0.0.0.0", 8080), "0.0.0.0:8080"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}
}

func TestExecuteHandlerRunsShellCommand(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(`{"command":"printf 'hello from daemon'"}`))
	rr := httptest.NewRecorder()

	executeHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executeHandler() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%q", resp.ExitCode, resp.Stderr)
	}
	if !strings.Contains(resp.Stdout, "hello from daemon") {
		t.Fatalf("stdout = %q, want it to contain command output", resp.Stdout)
	}
}

func TestLocalAgentExecuteToolUsesDaemonCommandRunner(t *testing.T) {
	sharedagent.SetLocalCommandExecutor(func(ctx context.Context, req sharedagent.LocalCommandRequest) (string, error) {
		if req.Command != "echo hello" {
			t.Fatalf("command = %q, want %q", req.Command, "echo hello")
		}
		if req.Reasoning != "show me the output" {
			t.Fatalf("reasoning = %q, want %q", req.Reasoning, "show me the output")
		}
		if req.WorkingDir != "" {
			t.Fatalf("working dir = %q, want empty", req.WorkingDir)
		}
		if req.TimeoutSeconds != 600 {
			t.Fatalf("timeout = %d, want 600", req.TimeoutSeconds)
		}
		return `{"stdout":"hello","stderr":"","exit_code":0}`, nil
	})
	sharedagent.SetLocalDesktopAutomationEnabled(true)
	defer sharedagent.SetLocalDesktopAutomationEnabled(false)
	defer sharedagent.SetLocalCommandExecutor(nil)

	ag := sharedagent.NewCopilotAgent(sharedagent.Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	out, err := ag.Execute(ctx, "execute_local_command", map[string]any{"command": "echo hello", "reasoning": "show me the output"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("Execute() output = %q, want it to contain command output", out)
	}
}

func TestLocalAgentExecuteToolPassesWorkingDirAndTimeout(t *testing.T) {
	sharedagent.SetLocalCommandExecutor(func(ctx context.Context, req sharedagent.LocalCommandRequest) (string, error) {
		if req.Command != "pwd" {
			t.Fatalf("command = %q, want %q", req.Command, "pwd")
		}
		if req.WorkingDir != "/tmp" {
			t.Fatalf("working dir = %q, want %q", req.WorkingDir, "/tmp")
		}
		if req.TimeoutSeconds != 45 {
			t.Fatalf("timeout = %d, want 45", req.TimeoutSeconds)
		}
		return `{"stdout":"/tmp","stderr":"","exit_code":0}`, nil
	})
	sharedagent.SetLocalDesktopAutomationEnabled(true)
	defer sharedagent.SetLocalDesktopAutomationEnabled(false)
	defer sharedagent.SetLocalCommandExecutor(nil)

	ag := sharedagent.NewCopilotAgent(sharedagent.Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{CurrentDB: "system"})

	out, err := ag.Execute(ctx, "execute_local_command", map[string]any{"command": "pwd", "reasoning": "inspect repo state", "working_dir": "/tmp", "timeout_seconds": 45})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "/tmp") {
		t.Fatalf("Execute() output = %q, want it to contain repo working directory output", out)
	}
}

func TestRunLocalCommandUsesSessionStateForFollowUpCommands(t *testing.T) {
	first, err := runLocalCommand(context.Background(), sharedagent.LocalCommandRequest{Command: "cd /tmp", SessionID: "repo-session", TimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("first runLocalCommand() error = %v", err)
	}
	var firstResp ExecuteResponse
	if err := json.Unmarshal([]byte(first), &firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstResp.Error != "" {
		t.Fatalf("first response error = %q", firstResp.Error)
	}

	second, err := runLocalCommand(context.Background(), sharedagent.LocalCommandRequest{Command: "pwd", SessionID: "repo-session", TimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("second runLocalCommand() error = %v", err)
	}
	var secondResp ExecuteResponse
	if err := json.Unmarshal([]byte(second), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if !strings.Contains(secondResp.Stdout, "/tmp") {
		t.Fatalf("expected second command output to reflect session cwd, got %q", secondResp.Stdout)
	}
}

func TestHandleUIHandoffReturnsWelcomeContextAndStoresSessionInfo(t *testing.T) {
	ctxKey := "handoff-test"
	daemonSessionStore.mu.Lock()
	delete(daemonSessionStore.data, ctxKey)
	daemonSessionStore.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","auth":{"access_token":"token-123","refresh_token":"refresh-123"},"hq":{"base_url":"https://hq.example","user_id":"user-1","session_id":"handoff-test"}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WelcomeContext == "" {
		t.Fatalf("expected welcome context in handoff response, got %+v", resp)
	}
	if !strings.Contains(strings.ToLower(resp.WelcomeContext), "local") {
		t.Fatalf("welcome context should describe the local RAG experience, got %q", resp.WelcomeContext)
	}

	daemonSessionStore.mu.Lock()
	ctx, ok := daemonSessionStore.data[ctxKey]
	daemonSessionStore.mu.Unlock()
	if !ok {
		t.Fatalf("expected handoff context to be stored for %q", ctxKey)
	}
	if ctx.AccessToken != "token-123" || ctx.RefreshToken != "refresh-123" {
		t.Fatalf("unexpected stored tokens: %+v", ctx)
	}
	if ctx.HQBaseURL != "https://hq.example" || ctx.HQUserID != "user-1" || ctx.HQSessionID != ctxKey {
		t.Fatalf("unexpected stored HQ context: %+v", ctx)
	}
}

func TestHandleUIHandoffAllowsMissingAuthTokensForLocalRuntime(t *testing.T) {
	ctxKey := "handoff-local-runtime"
	daemonSessionStore.mu.Lock()
	delete(daemonSessionStore.data, ctxKey)
	daemonSessionStore.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","auth":{"access_token":"","refresh_token":""},"hq":{"base_url":"https://hq.example","user_id":"user-2","session_id":"handoff-local-runtime"}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("expected handoff to be accepted without auth tokens, got %+v", resp)
	}
	if resp.Error != "" {
		t.Fatalf("expected no error, got %q", resp.Error)
	}
}

type recordingAskAgent struct {
	response string
	calls    []struct {
		ctx context.Context
		cfg *ai.ConfigMap
	}
}

func (a *recordingAskAgent) Open(ctx context.Context) error  { return nil }
func (a *recordingAskAgent) Close(ctx context.Context) error { return nil }
func (a *recordingAskAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *recordingAskAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	a.calls = append(a.calls, struct {
		ctx context.Context
		cfg *ai.ConfigMap
	}{ctx: ctx, cfg: cfg})
	if a.response != "" {
		return a.response, nil
	}
	return "ok", nil
}

func TestLocalRAGAskUsesShellFallbackForSimpleCommands(t *testing.T) {
	agent := &recordingAskAgent{response: "⚠️ **AI Copilot Disabled**: No valid API Key found."}
	svc := &localRAGService{service: agent}

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"echo hello"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp AskResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error response: %q", resp.Error)
	}
	if !strings.Contains(resp.Answer, "hello") {
		t.Fatalf("expected shell fallback output in answer, got %q", resp.Answer)
	}
}

func TestShellSessionManagerPersistsDirectoryAcrossCommands(t *testing.T) {
	mgr := newShellSessionManager()

	if _, _, err := mgr.run("shell-pty", "cd /tmp", 2*time.Second); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	out, stillRunning, err := mgr.run("shell-pty", "pwd", 2*time.Second)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if stillRunning {
		t.Fatalf("expected pwd to finish promptly, got still-running output")
	}
	if !strings.Contains(out, "/tmp") {
		t.Fatalf("expected pwd output to reflect /tmp, got %q", out)
	}
}

func TestShellSessionManagerSupportsLongRunningProcessAndStatus(t *testing.T) {
	mgr := newShellSessionManager()

	out, stillRunning, err := mgr.run("long-run", "sh -c 'printf start; sleep 1; printf end'", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if !stillRunning {
		t.Fatalf("expected first run to stay running beyond the timeout, got output %q", out)
	}
	if !strings.Contains(out, "start") {
		t.Fatalf("expected partial output from long-running process, got %q", out)
	}

	status, stillRunning, err := mgr.run("long-run", "status", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("status query failed: %v", err)
	}
	if !stillRunning {
		t.Fatalf("expected status query to report the process is still running, got %q", status)
	}
	if !strings.Contains(status, "start") {
		t.Fatalf("expected status output to include buffered output, got %q", status)
	}
}

func TestLocalRAGAskPersistsShellSessionDirectoryAcrossRequests(t *testing.T) {
	svc := &localRAGService{service: &recordingAskAgent{}}

	firstReq := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"cd /tmp"}`))
	firstReq.Header.Set("X-SOP-Session-ID", "shell-session")
	firstRR := httptest.NewRecorder()
	svc.handleAsk(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first handleAsk() status = %d, want %d", firstRR.Code, http.StatusOK)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"pwd"}`))
	secondReq.Header.Set("X-SOP-Session-ID", "shell-session")
	secondRR := httptest.NewRecorder()
	svc.handleAsk(secondRR, secondReq)
	if secondRR.Code != http.StatusOK {
		t.Fatalf("second handleAsk() status = %d, want %d", secondRR.Code, http.StatusOK)
	}

	var secondResp AskResponse
	if err := json.NewDecoder(secondRR.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if !strings.Contains(secondResp.Answer, "/tmp") {
		t.Fatalf("expected pwd output to reflect the updated directory, got %q", secondResp.Answer)
	}
}

func TestLocalRAGAskPropagatesHandoffContextIntoAgentRequest(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent}

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"sop:","auth":{"access_token":"token-123","refresh_token":"refresh-123"},"hq":{"base_url":"https://hq.example","user_id":"user-42","session_id":"sess-42"}}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected 1 ask call, got %d", len(agent.calls))
	}

	payload := ai.GetSessionPayload(agent.calls[0].ctx)
	if payload == nil {
		t.Fatal("expected session payload in ask context")
	}
	if payload.UserID != "user-42" || payload.SessionID != "sess-42" {
		t.Fatalf("unexpected session payload: %+v", payload)
	}
	if agent.calls[0].cfg == nil {
		t.Fatal("expected config map to be passed to agent ask")
	}
	if val, ok := agent.calls[0].cfg.Get("payload"); !ok {
		t.Fatal("expected payload in config map")
	} else if mappedPayload, ok := val.(*ai.SessionPayload); !ok || mappedPayload != payload {
		t.Fatalf("unexpected payload in config map: %v", val)
	}
}

func TestWithRateLimitRejectsRequestsAfterLocalBudgetIsExhausted(t *testing.T) {
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 1, Window: time.Minute})
	handler := withRateLimit(reg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("X-SOP-Session-ID", "session-a")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusNoContent {
		t.Fatalf("first request status = %d, want %d", rr1.Code, http.StatusNoContent)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-SOP-Session-ID", "session-a")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", rr2.Code, http.StatusTooManyRequests)
	}
}
