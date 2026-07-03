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
	"github.com/sharedcode/sop/tools/sopbackend"
)

type stubSharedAgent struct {
	name string
}

type recordingQueryAgent struct {
	queries []string
}

type executorAwareAgent struct {
	gotExecutor bool
}

func (a *stubSharedAgent) Open(ctx context.Context) error  { return nil }
func (a *stubSharedAgent) Close(ctx context.Context) error { return nil }
func (a *stubSharedAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *stubSharedAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	return "shared-agent", nil
}
func (a *stubSharedAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *stubSharedAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func (a *recordingQueryAgent) Open(ctx context.Context) error  { return nil }
func (a *recordingQueryAgent) Close(ctx context.Context) error { return nil }
func (a *recordingQueryAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *recordingQueryAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	a.queries = append(a.queries, query)
	return "ok", nil
}
func (a *recordingQueryAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *recordingQueryAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func (a *executorAwareAgent) Open(ctx context.Context) error  { return nil }
func (a *executorAwareAgent) Close(ctx context.Context) error { return nil }
func (a *executorAwareAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return nil, nil
}
func (a *executorAwareAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	_, a.gotExecutor = ctx.Value(ai.CtxKeyExecutor).(*sopbackend.DefaultToolExecutor)
	return "ok", nil
}
func (a *executorAwareAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *executorAwareAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}

func TestBuildListenAddr(t *testing.T) {
	if got, want := buildListenAddr("127.0.0.1", 9090), "127.0.0.1:9090"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}

	if got, want := buildListenAddr("0.0.0.0", 8080), "0.0.0.0:8080"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}
}

func TestTrySimpleShellCommandFallbackSkipsAgentCommandsWithoutSlash(t *testing.T) {
	svc := &localRAGService{shellSessions: newShellSessionState()}
	for _, query := range []string{"list_tools", "list_databases", "last-tool"} {
		got, handled, err := svc.trySimpleShellCommandFallback(query, "session")
		if err != nil {
			t.Fatalf("trySimpleShellCommandFallback(%q) returned error: %v", query, err)
		}
		if handled {
			t.Fatalf("trySimpleShellCommandFallback(%q) should bypass shell fallback for agent-style commands", query)
		}
		if got != "" {
			t.Fatalf("trySimpleShellCommandFallback(%q) returned unexpected output %q", query, got)
		}
	}
}

func TestTrySimpleShellCommandFallbackSkipsNaturalLanguageQuestions(t *testing.T) {
	svc := &localRAGService{shellSessions: newShellSessionState()}
	query := "show me list of changed files in this repo in git"

	got, handled, err := svc.trySimpleShellCommandFallback(query, "session")
	if err != nil {
		t.Fatalf("trySimpleShellCommandFallback(%q) returned error: %v", query, err)
	}
	if handled {
		t.Fatalf("trySimpleShellCommandFallback(%q) should bypass shell fallback for natural-language prompts", query)
	}
	if got != "" {
		t.Fatalf("trySimpleShellCommandFallback(%q) returned unexpected output %q", query, got)
	}
}

func TestHandleAskRoutesPlainListToolsToAgentWithSlashPrefix(t *testing.T) {
	agent := &recordingQueryAgent{}
	svc := &localRAGService{service: agent, shellSessions: newShellSessionState()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"list_tools"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.queries) != 1 {
		t.Fatalf("expected one agent query, got %d", len(agent.queries))
	}
	if agent.queries[0] != "/list_tools" {
		t.Fatalf("expected agent query to be /list_tools, got %q", agent.queries[0])
	}
}

func TestHandleAskInjectsBackendExecutorIntoContext(t *testing.T) {
	agent := &executorAwareAgent{}
	svc := &localRAGService{service: &stubSharedAgent{name: "shared"}, shellSessions: newShellSessionState()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"list_tools"}`))
	rr := httptest.NewRecorder()

	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": agent})
	defer sopbackend.SetLoadedAgents(nil)

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !agent.gotExecutor {
		t.Fatal("expected handleAsk to inject the shared backend DefaultToolExecutor into the shared agent context")
	}
}

func TestHandleAskUsesSharedBackendHandlerForSlashCommands(t *testing.T) {
	localAgent := &recordingQueryAgent{}
	sharedAgent := &recordingQueryAgent{}
	svc := &localRAGService{service: localAgent, shellSessions: newShellSessionState()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"list_tools"}`))
	rr := httptest.NewRecorder()

	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": sharedAgent})
	defer sopbackend.SetLoadedAgents(nil)

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(localAgent.queries) != 0 {
		t.Fatalf("expected local service agent to be bypassed for slash commands, got %v", localAgent.queries)
	}
	if len(sharedAgent.queries) != 1 || sharedAgent.queries[0] != "/list_tools" {
		t.Fatalf("expected shared backend handler to receive /list_tools, got %v", sharedAgent.queries)
	}
}

func TestHandleAskUsesSharedBackendHandlerForNaturalLanguageQueries(t *testing.T) {
	localAgent := &recordingQueryAgent{}
	sharedAgent := &recordingQueryAgent{}
	svc := &localRAGService{service: localAgent, shellSessions: newShellSessionState()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"show me list of changed files in this repo in git"}`))
	rr := httptest.NewRecorder()

	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": sharedAgent})
	defer sopbackend.SetLoadedAgents(nil)

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(localAgent.queries) != 0 {
		t.Fatalf("expected local service agent to be bypassed for natural-language queries, got %v", localAgent.queries)
	}
	if len(sharedAgent.queries) != 1 {
		t.Fatalf("expected shared backend handler to receive the natural-language query, got %v", sharedAgent.queries)
	}
	if sharedAgent.queries[0] != "show me list of changed files in this repo in git" {
		t.Fatalf("expected shared backend handler to receive the original query, got %q", sharedAgent.queries[0])
	}
}

func TestHandleAskNormalizesKelindarProviderUsingSharedBackendLogic(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newShellSessionState()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_provider":"kelindar","llm_model":"nomic-embed-text-v1.5-q8_0"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	if agent.calls[0].cfg == nil {
		t.Fatal("expected agent config to be populated")
	}
	value, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := value.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", value)
	}
	if providerDetails.Provider != "kelindar" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "kelindar")
	}
	if providerDetails.Model != "nomic-embed-text-v1.5-q8_0" {
		t.Fatalf("model = %q, want %q", providerDetails.Model, "nomic-embed-text-v1.5-q8_0")
	}
}

func TestHandleAskPassesProviderDetailsToAgentConfig(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newShellSessionState()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_provider":"gemini","llm_api_key":"secret-key","llm_model":"gemini-2.5-pro","llm_url":"https://example.test"}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	if agent.calls[0].cfg == nil {
		t.Fatal("expected agent config to be populated")
	}
	value, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := value.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", value)
	}
	if providerDetails.Provider != "gemini" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "gemini")
	}
	if providerDetails.APIKey != "secret-key" {
		t.Fatalf("api key = %q, want %q", providerDetails.APIKey, "secret-key")
	}
	if providerDetails.Model != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want %q", providerDetails.Model, "gemini-2.5-pro")
	}
	if providerDetails.BaseURL != "https://example.test" {
		t.Fatalf("base URL = %q, want %q", providerDetails.BaseURL, "https://example.test")
	}
}

func TestAutoProvisionSharedCopilotServiceUsesBackendLoadedAgents(t *testing.T) {
	oldAgents := sopbackend.GetLoadedAgents()
	defer sopbackend.SetLoadedAgents(oldAgents)

	backend := sopbackend.NewBackend(&sopbackend.Config{}, &sopbackend.ModelCatalog{})
	backend.LoadedAgents = map[string]ai.Agent[map[string]any]{"copilot": &stubSharedAgent{name: "shared"}}
	if err := backend.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	svc, err := autoProvisionSharedCopilotService(context.Background())
	if err != nil {
		t.Fatalf("autoProvisionSharedCopilotService() error = %v", err)
	}
	if _, ok := svc.(*stubSharedAgent); !ok {
		t.Fatalf("autoProvisionSharedCopilotService() = %T, want *stubSharedAgent", svc)
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
	if !strings.Contains(resp.WelcomeContext, "Shell-style commands") || !strings.Contains(resp.WelcomeContext, "ls") || !strings.Contains(resp.WelcomeContext, "/list_tools") {
		t.Fatalf("welcome context should include shell-command guidance, got %q", resp.WelcomeContext)
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

func TestLocalRAGAskUsesHandoffProviderDetailsWhenAskBodyOmitsThem(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent}
	ctxKey := "handoff-provider-session"

	handoffReq := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","auth":{"access_token":"token-123","refresh_token":"refresh-123"},"hq":{"base_url":"https://hq.example","user_id":"user-42","session_id":"handoff-provider-session"},"llm_provider":"anthropic","llm_api_key":"test-key","llm_model":"claude-3-5-sonnet-20241022","llm_url":"https://api.anthropic.com","thinking_level":"medium"}`))
	handoffRR := httptest.NewRecorder()
	handleUIHandoff(handoffRR, handoffReq)
	if handoffRR.Code != http.StatusOK {
		t.Fatalf("handoff status = %d, want %d", handoffRR.Code, http.StatusOK)
	}

	askReq := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","hq":{"base_url":"https://hq.example","user_id":"user-42","session_id":"handoff-provider-session"}}`))
	askRR := httptest.NewRecorder()
	svc.handleAsk(askRR, askReq)
	if askRR.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", askRR.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected 1 ask call, got %d", len(agent.calls))
	}

	if agent.calls[0].cfg == nil {
		t.Fatal("expected config map to be passed to agent ask")
	}
	val, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider details to be propagated from handoff")
	}
	providerDetails, ok := val.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected *sharedagent.ProviderDetails, got %T", val)
	}
	if providerDetails.Provider != "anthropic" || providerDetails.Model != "claude-3-5-sonnet-20241022" || providerDetails.APIKey != "test-key" || providerDetails.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("unexpected provider details: %+v", providerDetails)
	}
	if ctxVal, ok := agent.calls[0].ctx.Value(ai.CtxKeyProvider).(string); !ok || ctxVal != "anthropic:claude-3-5-sonnet-20241022" {
		t.Fatalf("expected provider context to be set from handoff, got %v", ctxVal)
	}
	if ctxKey != "" {
		_ = ctxKey
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
