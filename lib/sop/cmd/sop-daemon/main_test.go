package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
	"github.com/sharedcode/sop/ai/generator"
	sopfs "github.com/sharedcode/sop/fs"
	"github.com/sharedcode/sop/tools/sopbackend"
)

type stubSharedAgent struct {
	name string
}

type recordingQueryAgent struct {
	queries    []string
	queriesOut *[]string
}

type executorAwareAgent struct {
	gotExecutor bool
}

var workerModeEnabled bool

func init() {
	flag.BoolVar(&workerModeEnabled, "worker-mode", false, "run the PTY worker and exit")
}

func newLoopbackTestServer(handler http.Handler) *httptest.Server {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func TestRegisterBridgeRoutesSkipsDuplicateContinueRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/continue/chat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	service := &localRAGService{service: &recordingQueryAgent{}}
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})

	var panicked any
	func() {
		defer func() {
			panicked = recover()
		}()
		service.registerBridgeRoutes(mux, reg, nil)
	}()

	if panicked != nil {
		t.Fatalf("registerBridgeRoutes panicked on duplicate route registration: %v", panicked)
	}
}

func TestMain(m *testing.M) {
	// CRITICAL: Check for worker mode BEFORE any test framework initialization
	// We check os.Args directly (not flags) because flags haven't been parsed yet
	if len(os.Args) > 1 && os.Args[1] == "--worker-mode" {
		// Save real stdout for JSON protocol BEFORE any redirection
		realStdout := os.Stdout
		// Redirect global stdout to stderr to prevent test framework pollution
		os.Stdout = os.Stderr
		// Also redirect logs to stderr
		log.SetOutput(os.Stderr)

		// Run worker with the saved real stdout for JSON
		if err := runPTYWorkerWithOutput(realStdout); err != nil {
			log.Fatalf("[PTY Worker] Fatal error: %v", err)
		}
		os.Exit(0)
	}

	// Redirect logs to stderr (same as production main())
	// This prevents test framework logs from polluting worker stdout JSON protocol
	log.SetOutput(os.Stderr)

	os.Exit(m.Run())
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
	if a.queriesOut != nil {
		*a.queriesOut = append(*a.queriesOut, query)
	}
	return "ok", nil
}
func (a *recordingQueryAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return "", nil
}
func (a *recordingQueryAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	return nil, nil
}
func (a *recordingQueryAgent) Clone() ai.Agent[map[string]any] {
	queriesOut := a.queriesOut
	if queriesOut == nil {
		queriesOut = &a.queries
	}
	return &recordingQueryAgent{queriesOut: queriesOut}
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

func resetDefaultStorageHealthPolicyForTest(t *testing.T) {
	t.Helper()
	restore := sop.SetDefaultStorageHealthPolicy(sop.NewStorageHealthPolicy())
	t.Cleanup(restore)
}

func TestBuildListenAddr(t *testing.T) {
	if got, want := buildListenAddr("127.0.0.1", 9090), "127.0.0.1:9090"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}

	if got, want := buildListenAddr("0.0.0.0", 8080), "0.0.0.0:8080"; got != want {
		t.Fatalf("buildListenAddr() = %q, want %q", got, want)
	}
}

func TestShouldSeedFederatedContentSkipsWhenProvisioningIsRequired(t *testing.T) {
	ok, reason := shouldSeedFederatedContent(sopbackend.Config{}, sopbackend.ProvisioningDecision{
		Required: true,
		Reason:   sopbackend.ProvisioningDecisionReasonMissingConfig,
	})
	if ok {
		t.Fatal("expected seeding to be skipped while provisioning is required")
	}
	if !strings.Contains(reason, string(sopbackend.ProvisioningDecisionReasonMissingConfig)) {
		t.Fatalf("expected skip reason to mention provisioning reason, got %q", reason)
	}
}

func TestShouldSeedFederatedContentSkipsWithoutSystemDB(t *testing.T) {
	ok, reason := shouldSeedFederatedContent(sopbackend.Config{}, sopbackend.ProvisioningDecision{})
	if ok {
		t.Fatal("expected seeding to be skipped when no system database is configured")
	}
	if reason != "system database is not configured" {
		t.Fatalf("unexpected skip reason %q", reason)
	}
}

func TestShouldSeedFederatedContentAllowsReadySystemDB(t *testing.T) {
	ok, reason := shouldSeedFederatedContent(sopbackend.Config{
		SystemDB: &sopbackend.DatabaseConfig{Name: "system"},
	}, sopbackend.ProvisioningDecision{Required: false})
	if !ok {
		t.Fatalf("expected seeding to proceed, got skip reason %q", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty skip reason when seeding is allowed, got %q", reason)
	}
}

func TestContinueChatHandlerStreamsProgressAndContent(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}

	req := httptest.NewRequest(http.MethodPost, "/api/continue/chat", strings.NewReader(`{"session_id":"sess-1","message":"inspect this repo","workspace":{"root":"."},"context":{"open_files":["README.md"]}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueChat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueChat() status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "\"type\":\"content\"") {
		t.Fatalf("expected streamed content event, got %q", body)
	}
	if !strings.Contains(body, "\"type\":\"done\"") {
		t.Fatalf("expected done event, got %q", body)
	}
}

func TestContinueChatHandler_InjectsWorkflowProfileIntoAskQuery(t *testing.T) {
	agent := &recordingQueryAgent{}
	service := &localRAGService{service: agent}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/chat", strings.NewReader(`{"session_id":"sess-1","mode":"plan","message":"Implement a feature flag rollout for payments","workspace":{"root":".","active_file":"payments/service.go"},"context":{"open_files":["payments/service.go","payments/service_test.go"]}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueChat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueChat() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.queries) != 1 {
		t.Fatalf("expected one ask query, got %d", len(agent.queries))
	}
	query := agent.queries[0]
	checks := []string{
		"[continue_mode=plan]",
		"[coding_workflow_profile=feature_implementation]",
		"[coding_task_type=implementation]",
		"[recommended_phase_plan=investigation -> planning -> execution -> review -> delivery]",
		"[requested_phase=planning]",
		"[active_file=payments/service.go]",
	}
	for _, check := range checks {
		if !strings.Contains(query, check) {
			t.Fatalf("expected continue workflow context %q in query, got %q", check, query)
		}
	}
}

func TestContinueChatHandler_PropagatesProviderDetailsIntoAgentConfig(t *testing.T) {
	agent := &recordingAskAgent{}
	service := &localRAGService{service: agent}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/chat", strings.NewReader(`{"session_id":"continue-session-1","message":"Analyze the code base","provider":"gemini","api_key":"secret-key","model":"gemini-2.5-pro","url":"https://example.test","workspace":{"root":"."}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueChat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueChat() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	if agent.calls[0].cfg == nil {
		t.Fatal("expected agent config to be populated")
	}
	val, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := val.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", val)
	}
	if providerDetails.Provider != "gemini" || providerDetails.APIKey != "secret-key" || providerDetails.Model != "gemini-2.5-pro" || providerDetails.BaseURL != "https://example.test" {
		t.Fatalf("unexpected provider details: %+v", providerDetails)
	}
	payload := ai.GetSessionPayload(agent.calls[0].ctx)
	if payload == nil || payload.SessionID != "continue-session-1" {
		t.Fatalf("expected request user field to become session id, got %+v", payload)
	}
	if payload.Variables["llm_provider"] != "gemini" || payload.Variables["llm_model"] != "gemini-2.5-pro" {
		t.Fatalf("expected provider intent in payload variables, got %#v", payload.Variables)
	}
	if _, ok := payload.Variables["continue_routing_tier"]; ok {
		t.Fatalf("expected no inferred routing hint when provider/model are explicit, got %#v", payload.Variables)
	}
	if payload.Variables["llm_url"] != "https://example.test" {
		t.Fatalf("expected base URL intent in payload variables, got %#v", payload.Variables)
	}
	if ctxVal, ok := agent.calls[0].ctx.Value(ai.CtxKeyProvider).(string); !ok || ctxVal != "gemini:gemini-2.5-pro" {
		t.Fatalf("expected provider context override, got %#v", agent.calls[0].ctx.Value(ai.CtxKeyProvider))
	}
	if ctxVal, ok := agent.calls[0].ctx.Value(ai.CtxKeyAPIKey).(string); !ok || ctxVal != "secret-key" {
		t.Fatalf("expected API key context override, got %#v", agent.calls[0].ctx.Value(ai.CtxKeyAPIKey))
	}
	if ctxVal, ok := agent.calls[0].ctx.Value(ai.CtxKeyBaseURL).(string); !ok || ctxVal != "https://example.test" {
		t.Fatalf("expected base URL context override, got %#v", agent.calls[0].ctx.Value(ai.CtxKeyBaseURL))
	}
}

func TestContinueChatHandler_AutoEnrichesGuidanceVariablesForImplementationPlanning(t *testing.T) {
	agent := &recordingAskAgent{guidance: sharedagent.ContinueDelegationGuidance{
		TeamPractices:        []string{"Preserve public interfaces", "Add targeted tests"},
		Constraints:          []string{"Avoid direct SQL string concatenation"},
		ImplementationNotes:  []string{"Prefer repository helpers for retry logic"},
		ExpectedResultFormat: "structured_execution_result",
		ReferenceContext: []sharedagent.ContinueDelegationReference{{
			Kind:    "ltm_team_pattern",
			Source:  "Team Patterns/Payments/Retry Pattern",
			Title:   "Retry Pattern",
			Content: "Use the shared retry helper with jitter.",
		}},
		Metadata: map[string]any{"source": "ltm_team_patterns"},
	}}
	service := &localRAGService{service: agent}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/chat", strings.NewReader(`{"session_id":"continue-session-guidance","mode":"plan","message":"Implement retry handling for payment webhooks","workspace":{"root":".","active_file":"payments/webhook.go"},"context":{"open_files":["payments/webhook.go"]}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueChat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueChat() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	payload := ai.GetSessionPayload(agent.calls[0].ctx)
	if payload == nil {
		t.Fatal("expected session payload")
	}
	teamPractices, ok := payload.Variables["continue_team_practices"].([]string)
	if !ok || len(teamPractices) != 2 || teamPractices[0] != "Preserve public interfaces" {
		t.Fatalf("expected auto-enriched team practices, got %#v", payload.Variables["continue_team_practices"])
	}
	constraints, ok := payload.Variables["continue_constraints"].([]string)
	if !ok || len(constraints) != 1 {
		t.Fatalf("expected auto-enriched constraints, got %#v", payload.Variables["continue_constraints"])
	}
	if payload.Variables["continue_expected_result_format"] != "structured_execution_result" {
		t.Fatalf("expected expected result format guidance, got %#v", payload.Variables["continue_expected_result_format"])
	}
	refs, ok := payload.Variables["continue_reference_context"].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("expected auto-enriched reference context, got %#v", payload.Variables["continue_reference_context"])
	}
	metadata, ok := payload.Variables["continue_guidance_metadata"].(map[string]any)
	if !ok || metadata["source"] != "ltm_team_patterns" {
		t.Fatalf("expected guidance metadata, got %#v", payload.Variables["continue_guidance_metadata"])
	}
}

func TestAskBridgeChat_RemovesStaleContinueGuidanceVariablesWhenRefreshIsEmpty(t *testing.T) {
	agent := &recordingAskAgent{}
	service := &localRAGService{service: agent}
	_, err := service.askBridgeChat(context.Background(), bridgeAskRequest{
		SessionID:  "continue-session-stale-guidance",
		UserPrompt: "Implement retry handling for payment webhooks",
		FullPrompt: "Implement retry handling for payment webhooks",
		Variables: map[string]any{
			"requested_phase":                 "planning",
			"coding_task_type":                "implementation",
			"continue_team_practices":         []string{"OLD stale practice"},
			"continue_constraints":            []string{"OLD stale constraint"},
			"continue_guidance_metadata":      map[string]any{"source": "ltm_team_patterns"},
			"continue_expected_result_format": "structured_execution_result",
		},
	}, nil)
	if err != nil {
		t.Fatalf("askBridgeChat() error = %v", err)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	payload := ai.GetSessionPayload(agent.calls[0].ctx)
	if payload == nil {
		t.Fatal("expected session payload")
	}
	for _, key := range []string{
		"continue_team_practices",
		"continue_success_criteria",
		"continue_constraints",
		"continue_implementation_notes",
		"continue_expected_result_format",
		"continue_reference_context",
		"continue_guidance_metadata",
	} {
		if _, ok := payload.Variables[key]; ok {
			t.Fatalf("expected stale %s to be removed, got %#v", key, payload.Variables[key])
		}
	}
}

func TestContinueToolHandlerReadsFilesFromWorkspace(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("hello from bridge\nsecond line"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/repo.read_file", strings.NewReader(fmt.Sprintf(`{"path":"%s","start_line":1,"end_line":5}`, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "repo.read_file")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true response, got %#v", payload)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", payload["result"])
	}
	if got := result["content"]; got != "hello from bridge\nsecond line" {
		t.Fatalf("unexpected content: %#v", got)
	}
}

func TestContinueToolHandlerAppliesPatchToWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":"after"}`, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "code.apply_patch")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(data) != "after" {
		t.Fatalf("expected patch to overwrite file contents in replace mode, got %q", string(data))
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", payload["result"])
	}
	if result["mode"] != "replace" {
		t.Fatalf("expected replace mode for full-content patch, got %#v", result["mode"])
	}
}

func TestContinueToolHandlerDelegatesPatchToRegisteredWorkerForOpenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{continueState: newContinueBridgeSessionStore(), continueWorkers: newContinueBridgeWorkerStore()}
	service.ensureContinueSessionStore().Put(continueSessionSnapshot{
		SessionID:     "continue-patch-1",
		ActiveFile:    "payments/service.go",
		OpenFiles:     []string{"payments/service.go"},
		WorkspaceRoot: dir,
		Status:        "in_progress",
	})

	var received continueAgentExecuteRequest
	worker := newLoopbackTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"type\":\"done\",\"result\":{\"status\":\"completed\",\"summary\":\"Applied editor-native patch\",\"modified_files\":[{\"file_path\":\"payments/service.go\",\"crud_operation\":\"update\",\"diff_summary\":\"Applied patch\"}]}}\n"))
	}))
	defer worker.Close()

	service.ensureContinueWorkerStore().Put(continueWorkerRegistration{
		SessionID:    "continue-patch-1",
		CallbackURL:  worker.URL,
		Capabilities: []string{"apply_patch", "read_context"},
		IDE:          "vscode",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-patch-1","workspace":%q,"path":%q,"selection":"after"}`, dir, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "code.apply_patch")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file after delegated patch: %v", err)
	}
	if string(data) != "before" {
		t.Fatalf("expected delegated patch to avoid local disk mutation, got %q", string(data))
	}
	if received.ToolChoice != "apply_patch" || len(received.AllowedTools) != 2 || received.AllowedTools[1] != "apply_patch" {
		t.Fatalf("expected apply_patch delegation contract, got %#v", received)
	}
	if !strings.Contains(received.Task, "Patch content") || !strings.Contains(received.Task, "after") {
		t.Fatalf("expected focused delegated patch task, got %q", received.Task)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok || result["delegated"] != true || result["worker"] != "continue" {
		t.Fatalf("expected delegated worker result, got %#v", payload)
	}
	if result["summary"] != "Applied editor-native patch" {
		t.Fatalf("expected worker summary in response, got %#v", result)
	}
}

func TestContinueToolHandlerAppliesUnifiedHunkPatchToLargeFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	lines := make([]string, 0, 1200)
	for i := 1; i <= 1200; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	original := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	patch := strings.Join([]string{
		"@@ -600,3 +600,3 @@",
		" line 600",
		"-line 601",
		"+line 601 patched",
		" line 602",
	}, "\n")

	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":%q}`, filePath, patch)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "code.apply_patch")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "line 601 patched") || strings.Contains(text, "line 601\n") {
		t.Fatalf("expected targeted hunk update in large file")
	}
	if !strings.Contains(text, "line 1200\n") {
		t.Fatalf("expected non-target lines to remain intact")
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", payload["result"])
	}
	if result["mode"] != "unified_hunk" {
		t.Fatalf("expected unified_hunk mode, got %#v", result["mode"])
	}
}

func TestContinueToolHandlerFallsBackToLocalPatchWhenTargetFileIsNotOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{continueState: newContinueBridgeSessionStore(), continueWorkers: newContinueBridgeWorkerStore()}
	service.ensureContinueSessionStore().Put(continueSessionSnapshot{
		SessionID:     "continue-patch-2",
		ActiveFile:    "payments/other.go",
		OpenFiles:     []string{"payments/other.go"},
		WorkspaceRoot: dir,
		Status:        "in_progress",
	})
	service.ensureContinueWorkerStore().Put(continueWorkerRegistration{
		SessionID:    "continue-patch-2",
		CallbackURL:  "http://127.0.0.1:1",
		Capabilities: []string{"apply_patch", "read_context"},
		IDE:          "vscode",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-patch-2","workspace":%q,"path":%q,"selection":"after"}`, dir, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "code.apply_patch")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(data) != "after" {
		t.Fatalf("expected local fallback patch to update file, got %q", string(data))
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result payload, got %#v", payload)
	}
	if _, delegated := result["delegated"]; delegated {
		t.Fatalf("expected local fallback result, got %#v", result)
	}
	if result["mode"] != "replace" {
		t.Fatalf("expected local replace-mode patch result, got %#v", result)
	}
}

func TestContinueToolHandlerRunsCommandsForValidation(t *testing.T) {
	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/test.run", strings.NewReader(`{"query":"echo bridge-test"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "test.run")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", payload["result"])
	}
	if got := result["output"]; !strings.Contains(fmt.Sprint(got), "bridge-test") {
		t.Fatalf("expected command output to include bridge-test, got %#v", got)
	}
}

func TestContinueToolHandlerPreviewsPatchForWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.preview_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":"after"}`, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "code.preview_patch")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", payload["result"])
	}
	preview := fmt.Sprint(result["preview"])
	if !strings.Contains(preview, "-before") || !strings.Contains(preview, "+after") {
		t.Fatalf("expected preview to include old and new content, got %q", preview)
	}
	if result["mode"] != "replace" {
		t.Fatalf("expected replace mode in preview, got %#v", result["mode"])
	}
}

func TestContinueToolHandlerPreviewsUnifiedHunkPatchForWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	patch := strings.Join([]string{
		"@@ -1,3 +1,3 @@",
		" alpha",
		"-beta",
		"+beta patched",
		" gamma",
	}, "\n")

	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.preview_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":%q}`, filePath, patch)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "code.preview_patch")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", payload["result"])
	}
	preview := fmt.Sprint(result["preview"])
	if !strings.Contains(preview, "-alpha\nbeta\ngamma") || !strings.Contains(preview, "+alpha\nbeta patched\ngamma") {
		t.Fatalf("expected preview to include transformed content for hunk patch, got %q", preview)
	}
	if result["mode"] != "unified_hunk" {
		t.Fatalf("expected unified_hunk mode in preview, got %#v", result["mode"])
	}
}

func TestContinueToolHandlerAppliesSingleFindAndReplace(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/single_find_and_replace", strings.NewReader(fmt.Sprintf(`{"path":"%s","find":"beta","replace":"beta patched"}`, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "single_find_and_replace")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "alpha\nbeta patched\ngamma\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestContinueToolHandlerDelegatesSingleFindAndReplaceToRegisteredWorkerForOpenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{continueState: newContinueBridgeSessionStore(), continueWorkers: newContinueBridgeWorkerStore()}
	service.ensureContinueSessionStore().Put(continueSessionSnapshot{
		SessionID:     "continue-edit-1",
		ActiveFile:    "payments/service.go",
		OpenFiles:     []string{"payments/service.go"},
		WorkspaceRoot: dir,
		Status:        "in_progress",
	})

	var received continueAgentExecuteRequest
	worker := newLoopbackTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"type\":\"done\",\"result\":{\"status\":\"completed\",\"summary\":\"Applied editor-native replacement\",\"modified_files\":[{\"file_path\":\"payments/service.go\",\"crud_operation\":\"update\",\"diff_summary\":\"Replaced beta with beta patched\"}]}}\n"))
	}))
	defer worker.Close()

	service.ensureContinueWorkerStore().Put(continueWorkerRegistration{
		SessionID:    "continue-edit-1",
		CallbackURL:  worker.URL,
		Capabilities: []string{"apply_patch", "read_context"},
		IDE:          "vscode",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/single_find_and_replace", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-edit-1","workspace":%q,"path":%q,"find":"beta","replace":"beta patched"}`, dir, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "single_find_and_replace")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file after delegated edit: %v", err)
	}
	if string(data) != "alpha\nbeta\ngamma\n" {
		t.Fatalf("expected delegated edit to avoid local disk mutation, got %q", string(data))
	}
	if received.Context.ActiveFile != "payments/service.go" || len(received.Context.OpenFiles) != 1 || received.Context.OpenFiles[0] != "payments/service.go" {
		t.Fatalf("expected worker request to preserve open-file context, got %#v", received.Context)
	}
	if received.ToolChoice != "single_find_and_replace" || len(received.AllowedTools) != 2 || received.AllowedTools[1] != "single_find_and_replace" {
		t.Fatalf("expected delegated tool constraints, got %#v", received)
	}
	if !strings.Contains(received.Task, "Replace this exact snippet") || !strings.Contains(received.Task, "beta patched") {
		t.Fatalf("expected focused delegated task, got %q", received.Task)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok || result["delegated"] != true || result["worker"] != "continue" {
		t.Fatalf("expected delegated worker result, got %#v", payload)
	}
	if result["summary"] != "Applied editor-native replacement" {
		t.Fatalf("expected worker summary in response, got %#v", result)
	}
}

func TestContinueToolHandlerAppliesMultiEdit(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{}
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/multi_edit", strings.NewReader(fmt.Sprintf(`{"path":"%s","edits":[{"find":"alpha","replace":"alpha patched"},{"find":"gamma","replace":"gamma patched"}]}`, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "multi_edit")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "alpha patched\nbeta\ngamma patched\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestContinueToolHandlerFallsBackToLocalMultiEditWhenTargetFileIsNotOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	service := &localRAGService{continueState: newContinueBridgeSessionStore(), continueWorkers: newContinueBridgeWorkerStore()}
	service.ensureContinueSessionStore().Put(continueSessionSnapshot{
		SessionID:     "continue-edit-2",
		ActiveFile:    "payments/other.go",
		OpenFiles:     []string{"payments/other.go"},
		WorkspaceRoot: dir,
		Status:        "in_progress",
	})
	service.ensureContinueWorkerStore().Put(continueWorkerRegistration{
		SessionID:    "continue-edit-2",
		CallbackURL:  "http://127.0.0.1:1",
		Capabilities: []string{"apply_patch", "read_context"},
		IDE:          "vscode",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/multi_edit", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-edit-2","workspace":%q,"path":%q,"edits":[{"find":"alpha","replace":"alpha patched"},{"find":"gamma","replace":"gamma patched"}]}`, dir, filePath)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	service.handleContinueTool(rr, req, "multi_edit")

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "alpha patched\nbeta\ngamma patched\n" {
		t.Fatalf("expected local fallback edit, got %q", string(data))
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result payload, got %#v", payload)
	}
	if _, delegated := result["delegated"]; delegated {
		t.Fatalf("expected local fallback result, got %#v", result)
	}
	if result["mode"] != "multi_edit" {
		t.Fatalf("expected local multi_edit result, got %#v", result)
	}
}

func TestRegisterBridgeRoutes_MountsClientEndpoints(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	mux := http.NewServeMux()
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})
	service.registerBridgeRoutes(mux, reg, nil)

	chatReq := httptest.NewRequest(http.MethodPost, "/api/continue/chat", strings.NewReader(`{"session_id":"bridge-route-test-chat","message":"hello","provider":"anthropic","model":"claude-sonnet-4-5","workspace":{"root":"."}}`))
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("X-SOP-Session-ID", "bridge-route-test-chat")
	chatRR := httptest.NewRecorder()
	mux.ServeHTTP(chatRR, chatReq)
	if chatRR.Code != http.StatusOK {
		t.Fatalf("mounted continue chat route status = %d, want %d", chatRR.Code, http.StatusOK)
	}
	if !strings.Contains(chatRR.Body.String(), `"type":"content"`) || !strings.Contains(chatRR.Body.String(), `"type":"done"`) {
		t.Fatalf("expected mounted continue stream response, got %q", chatRR.Body.String())
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("hello from mounted route"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	toolReq := httptest.NewRequest(http.MethodPost, "/api/continue/tools/repo.read_file", strings.NewReader(fmt.Sprintf(`{"path":"%s","start_line":1,"end_line":1}`, filePath)))
	toolReq.Header.Set("Content-Type", "application/json")
	toolReq.Header.Set("X-SOP-Session-ID", "bridge-route-test-tool")
	toolRR := httptest.NewRecorder()
	mux.ServeHTTP(toolRR, toolReq)
	if toolRR.Code != http.StatusOK {
		t.Fatalf("mounted continue tool route status = %d, want %d", toolRR.Code, http.StatusOK)
	}
	if !strings.Contains(toolRR.Body.String(), "hello from mounted route") {
		t.Fatalf("expected mounted continue tool response, got %q", toolRR.Body.String())
	}
}

func TestRegisterBridgeRoutes_MountsClientMetadataEndpoints(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	mux := http.NewServeMux()
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})
	service.registerBridgeRoutes(mux, reg, nil)

	legacyModelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	legacyModelsRR := httptest.NewRecorder()
	mux.ServeHTTP(legacyModelsRR, legacyModelsReq)
	if legacyModelsRR.Code != http.StatusNotFound {
		t.Fatalf("expected removed legacy models route to return 404, got %d", legacyModelsRR.Code)
	}

	legacyChatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`))
	legacyChatReq.Header.Set("Content-Type", "application/json")
	legacyChatRR := httptest.NewRecorder()
	mux.ServeHTTP(legacyChatRR, legacyChatReq)
	if legacyChatRR.Code != http.StatusNotFound {
		t.Fatalf("expected removed legacy chat route to return 404, got %d", legacyChatRR.Code)
	}

	continueReq := httptest.NewRequest(http.MethodGet, "/api/continue/metadata", nil)
	continueReq.Header.Set("X-SOP-Session-ID", "bridge-route-continue-metadata")
	continueRR := httptest.NewRecorder()
	mux.ServeHTTP(continueRR, continueReq)
	if continueRR.Code != http.StatusOK {
		t.Fatalf("mounted continue metadata route status = %d, want %d", continueRR.Code, http.StatusOK)
	}
	var continuePayload map[string]any
	if err := json.Unmarshal(continueRR.Body.Bytes(), &continuePayload); err != nil {
		t.Fatalf("decode continue metadata: %v", err)
	}
	if continuePayload["client"] != "continue" {
		t.Fatalf("expected continue metadata client identifier, got %#v", continuePayload)
	}
	chatInfo, ok := continuePayload["chat"].(map[string]any)
	if !ok || chatInfo["path"] != "/api/continue/chat" || chatInfo["method"] != "POST" || chatInfo["provider_is_attribute"] != true || chatInfo["model_is_attribute"] != true {
		t.Fatalf("expected native continue chat metadata, got %#v", continuePayload)
	}
	if _, exists := continuePayload["config"]; exists {
		t.Fatalf("did not expect daemon-owned continue config metadata, got %#v", continuePayload)
	}
	controlModel, ok := continuePayload["control_model"].(map[string]any)
	if !ok || controlModel["controller"] != "ai_studio_daemon" || controlModel["editor_worker"] != "continue" || controlModel["provider_is_attribute"] != true || controlModel["model_is_attribute"] != true {
		t.Fatalf("expected continue control model metadata, got %#v", continuePayload)
	}
	poolPopulated, ok := controlModel["llm_pool_populated"].(bool)
	if !ok {
		t.Fatalf("expected llm_pool_populated boolean in control model metadata, got %#v", controlModel)
	}
	hideEditorPoolInputs, ok := controlModel["hide_editor_pool_inputs_when_populated"].(bool)
	if !ok {
		t.Fatalf("expected hide_editor_pool_inputs_when_populated boolean in control model metadata, got %#v", controlModel)
	}
	if hideEditorPoolInputs != poolPopulated {
		t.Fatalf("expected hide flag to mirror pool-populated state, got hide=%v populated=%v", hideEditorPoolInputs, poolPopulated)
	}
	if controlModel["selection_message"] == "" {
		t.Fatalf("expected continue control model selection message, got %#v", controlModel)
	}
	supportedLLMs, ok := controlModel["supported_llms"].([]any)
	if !ok || len(supportedLLMs) != 3 {
		t.Fatalf("expected daemon-advertised supported llms, got %#v", controlModel)
	}
	firstGroup, ok := supportedLLMs[0].(map[string]any)
	if !ok || firstGroup["label"] == "" {
		t.Fatalf("expected llm catalog groups in metadata, got %#v", supportedLLMs)
	}
	workerRegister, ok := continuePayload["worker_register"].(map[string]any)
	if !ok || workerRegister["path"] != "/api/continue/worker/register" || workerRegister["method"] != "POST" {
		t.Fatalf("expected continue worker register metadata, got %#v", continuePayload)
	}
	workerState, ok := continuePayload["worker_state"].(map[string]any)
	if !ok || workerState["path_prefix"] != "/api/continue/workers/" || workerState["method"] != "GET" {
		t.Fatalf("expected continue worker state metadata, got %#v", continuePayload)
	}
	workerCapabilities, ok := continuePayload["worker_capabilities"].([]any)
	if !ok || len(workerCapabilities) == 0 {
		t.Fatalf("expected continue worker capability catalog, got %#v", continuePayload)
	}
	foundApplyPatchCapability := false
	for _, item := range workerCapabilities {
		if item == "apply_patch" {
			foundApplyPatchCapability = true
			break
		}
	}
	if !foundApplyPatchCapability {
		t.Fatalf("expected apply_patch in worker capability catalog, got %#v", workerCapabilities)
	}
	tools, ok := continuePayload["tools"].(map[string]any)
	if !ok {
		t.Fatalf("expected continue tools metadata, got %#v", continuePayload)
	}
	names, ok := tools["names"].([]any)
	if !ok || len(names) == 0 {
		t.Fatalf("expected continue tool names, got %#v", tools)
	}
	foundReadFile := false
	for _, item := range names {
		if item == "repo.read_file" {
			foundReadFile = true
			break
		}
	}
	if !foundReadFile {
		t.Fatalf("expected repo.read_file in continue metadata, got %#v", names)
	}
	ideTools, ok := continuePayload["ide_tools"].(map[string]any)
	if !ok {
		t.Fatalf("expected continue IDE tool metadata, got %#v", continuePayload)
	}
	phaseItems, ok := ideTools["supported_phases"].([]any)
	if !ok || len(phaseItems) == 0 {
		t.Fatalf("expected supported phase metadata, got %#v", ideTools)
	}
	foundExecutionPhase := false
	for _, item := range phaseItems {
		phase, ok := item.(map[string]any)
		if !ok || phase["id"] != "execution" {
			continue
		}
		tools, ok := phase["available_tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Fatalf("expected execution phase tools, got %#v", phase)
		}
		var hasEdit, hasTerminal bool
		for _, tool := range tools {
			if tool == "edit_existing_file" {
				hasEdit = true
			}
			if tool == "run_terminal_command" {
				hasTerminal = true
			}
		}
		if !hasEdit || !hasTerminal {
			t.Fatalf("expected execution phase to expose edit_existing_file and run_terminal_command, got %#v", tools)
		}
		foundExecutionPhase = true
	}
	if !foundExecutionPhase {
		t.Fatalf("expected execution phase metadata, got %#v", phaseItems)
	}
	workflows, ok := continuePayload["coding_workflows"].(map[string]any)
	if !ok {
		t.Fatalf("expected coding workflow metadata, got %#v", continuePayload)
	}
	profiles, ok := workflows["supported_profiles"].([]any)
	if !ok || len(profiles) == 0 {
		t.Fatalf("expected supported workflow profiles, got %#v", workflows)
	}
	foundFeatureImplementation := false
	for _, item := range profiles {
		profile, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if profile["id"] == "feature_implementation" {
			foundFeatureImplementation = true
			plan, _ := profile["default_phase_plan"].([]any)
			if len(plan) < 5 || plan[0] != "investigation" || plan[1] != "planning" || plan[2] != "execution" || plan[3] != "review" || plan[4] != "delivery" {
				t.Fatalf("expected feature implementation phase plan, got %#v", profile)
			}
		}
	}
	if !foundFeatureImplementation {
		t.Fatalf("expected feature_implementation workflow profile, got %#v", profiles)
	}
}

func TestRegisterBridgeRoutes_TracksContinueAgentExecutionSessionState(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	mux := http.NewServeMux()
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})
	service.registerBridgeRoutes(mux, reg, nil)

	executeReq := httptest.NewRequest(http.MethodPost, "/api/continue/agent/execute", strings.NewReader(`{"request_id":"client-request-agent-1","session_id":"continue-agent-1","task":"Implement a feature flag rollout for billing","mode":"plan","context":{"workspace_root":".","active_file":"billing/service.go","selected_text":"func rollout()","open_files":["billing/service.go","billing/service_test.go"]}}`))
	executeReq.Header.Set("Content-Type", "application/json")
	executeReq.Header.Set("X-SOP-Session-ID", "bridge-route-agent-execute")
	executeRR := httptest.NewRecorder()
	mux.ServeHTTP(executeRR, executeReq)
	if executeRR.Code != http.StatusOK {
		t.Fatalf("mounted continue agent execute route status = %d, want %d", executeRR.Code, http.StatusOK)
	}
	if !strings.Contains(executeRR.Body.String(), "Executing editor-aware coding task") || !strings.Contains(executeRR.Body.String(), `"type":"done"`) {
		t.Fatalf("expected streamed continue agent execution response, got %q", executeRR.Body.String())
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/continue/sessions/continue-agent-1", nil)
	sessionReq.Header.Set("X-SOP-Session-ID", "bridge-route-agent-session")
	sessionRR := httptest.NewRecorder()
	mux.ServeHTTP(sessionRR, sessionReq)
	if sessionRR.Code != http.StatusOK {
		t.Fatalf("mounted continue session state route status = %d, want %d", sessionRR.Code, http.StatusOK)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(sessionRR.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode continue session snapshot: %v", err)
	}
	requestID, ok := snapshot["request_id"].(string)
	if snapshot["session_id"] != "continue-agent-1" || !ok || requestID == "" || requestID == "client-request-agent-1" || snapshot["status"] != "completed" {
		t.Fatalf("expected completed session snapshot, got %#v", snapshot)
	}
	if snapshot["phase"] != "planning" || snapshot["workflow_profile"] != "feature_implementation" || snapshot["task_type"] != "implementation" {
		t.Fatalf("expected planning feature-implementation session snapshot, got %#v", snapshot)
	}
	if snapshot["active_file"] != "billing/service.go" || snapshot["workspace_root"] != cwd() {
		t.Fatalf("expected workspace context in session snapshot, got %#v", snapshot)
	}

	requestReq := httptest.NewRequest(http.MethodGet, "/api/continue/requests/"+requestID, nil)
	requestReq.Header.Set("X-SOP-Session-ID", "bridge-route-agent-request")
	requestRR := httptest.NewRecorder()
	mux.ServeHTTP(requestRR, requestReq)
	if requestRR.Code != http.StatusOK {
		t.Fatalf("mounted continue request state route status = %d, want %d", requestRR.Code, http.StatusOK)
	}
}

func TestRegisterBridgeRoutes_ProxiesContinueAgentExecutionToRegisteredWorker(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	mux := http.NewServeMux()
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})
	service.registerBridgeRoutes(mux, reg, nil)

	var receivedBody map[string]any
	observedInProgress := false
	worker := newLoopbackTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("worker callback method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode worker callback body: %v", err)
		}
		if snapshot, ok := service.ensureContinueSessionStore().GetRequest(receivedBody["request_id"].(string)); ok {
			observedInProgress = snapshot.Status == "in_progress" && snapshot.Phase == "execution"
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"type\":\"progress\",\"message\":\"Continue worker preparing patch\"}\n"))
		_, _ = w.Write([]byte("{\"type\":\"file_change\",\"file_path\":\"payments/service.go\",\"crud_operation\":\"update\",\"diff_summary\":\"Applied payment patch\",\"metadata\":{\"phase\":\"execution\"}}\n"))
		_, _ = w.Write([]byte("{\"type\":\"done\",\"result\":{\"status\":\"completed\",\"summary\":\"Patched payments/service.go\",\"modified_files\":[{\"file_path\":\"payments/service.go\",\"crud_operation\":\"update\",\"diff_summary\":\"Applied payment patch\",\"diff_format\":\"unified\",\"metadata\":{\"phase\":\"execution\"}}],\"metadata\":{\"requested_phase\":\"execution\",\"mode\":\"apply\"}}}\n"))
	}))
	defer worker.Close()

	registerReq := httptest.NewRequest(http.MethodPost, "/api/continue/worker/register", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-worker-1","callback_url":"%s","capabilities":["apply_patch","read_context"],"ide":"vscode"}`, worker.URL)))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-register")
	registerRR := httptest.NewRecorder()
	mux.ServeHTTP(registerRR, registerReq)
	if registerRR.Code != http.StatusOK {
		t.Fatalf("mounted continue worker register route status = %d, want %d", registerRR.Code, http.StatusOK)
	}

	workerStateReq := httptest.NewRequest(http.MethodGet, "/api/continue/workers/continue-worker-1", nil)
	workerStateReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-state")
	workerStateRR := httptest.NewRecorder()
	mux.ServeHTTP(workerStateRR, workerStateReq)
	if workerStateRR.Code != http.StatusOK {
		t.Fatalf("mounted continue worker state route status = %d, want %d", workerStateRR.Code, http.StatusOK)
	}

	executeReq := httptest.NewRequest(http.MethodPost, "/api/continue/agent/execute", strings.NewReader(`{"request_id":"client-request-worker-1","session_id":"continue-worker-1","task":"Apply the prepared payment patch","mode":"apply","requested_phase":"execution","context":{"workspace_root":".","active_file":"payments/service.go","selected_text":"func processPayment()","open_files":["payments/service.go"]}}`))
	executeReq.Header.Set("Content-Type", "application/json")
	executeReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-execute")
	executeRR := httptest.NewRecorder()
	mux.ServeHTTP(executeRR, executeReq)
	if executeRR.Code != http.StatusOK {
		t.Fatalf("proxied continue agent execute status = %d, want %d", executeRR.Code, http.StatusOK)
	}
	if !strings.Contains(executeRR.Body.String(), "Continue worker preparing patch") || !strings.Contains(executeRR.Body.String(), `"type":"done"`) {
		t.Fatalf("expected proxied worker stream response, got %q", executeRR.Body.String())
	}
	if !observedInProgress {
		t.Fatalf("expected in-progress session snapshot to be visible during worker callback")
	}
	if receivedBody["task"] != "Apply the prepared payment patch" || receivedBody["mode"] != "apply" {
		t.Fatalf("unexpected proxied worker request body: %#v", receivedBody)
	}
	requestID, ok := receivedBody["request_id"].(string)
	if !ok || requestID == "" || requestID == "client-request-worker-1" {
		t.Fatalf("expected daemon-generated request_id, got %#v", receivedBody["request_id"])
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/continue/sessions/continue-worker-1", nil)
	sessionReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-session")
	sessionRR := httptest.NewRecorder()
	mux.ServeHTTP(sessionRR, sessionReq)
	if sessionRR.Code != http.StatusOK {
		t.Fatalf("mounted continue worker session route status = %d, want %d", sessionRR.Code, http.StatusOK)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(sessionRR.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode proxied continue session snapshot: %v", err)
	}
	if snapshot["status"] != "completed" || snapshot["phase"] != "execution" {
		t.Fatalf("expected completed execution snapshot for proxied worker path, got %#v", snapshot)
	}
	if snapshot["request_id"] != requestID {
		t.Fatalf("expected request-scoped execution snapshot, got %#v", snapshot)
	}
	if snapshot["mode"] != "apply" {
		t.Fatalf("expected mode to remain apply while phase is execution, got %#v", snapshot)
	}
	result, ok := snapshot["execution_result"].(map[string]any)
	if !ok {
		t.Fatalf("expected persisted execution_result in session snapshot, got %#v", snapshot["execution_result"])
	}
	if result["summary"] != "Patched payments/service.go" {
		t.Fatalf("expected persisted execution result summary, got %#v", result)
	}
	modifiedFiles, ok := result["modified_files"].([]any)
	if !ok || len(modifiedFiles) != 1 {
		t.Fatalf("expected one persisted modified file, got %#v", result["modified_files"])
	}
	events, ok := snapshot["execution_events"].([]any)
	if !ok || len(events) < 3 {
		t.Fatalf("expected persisted execution events on session snapshot, got %#v", snapshot["execution_events"])
	}
	progressEvent, ok := events[0].(map[string]any)
	if !ok || progressEvent["type"] != "progress" {
		t.Fatalf("expected progress event first, got %#v", events)
	}
	fileChangeEvent, ok := events[1].(map[string]any)
	if !ok || fileChangeEvent["type"] != "file_change" || fileChangeEvent["file_path"] != "payments/service.go" {
		t.Fatalf("expected file_change event with file path, got %#v", events)
	}

	requestReq := httptest.NewRequest(http.MethodGet, "/api/continue/requests/"+requestID, nil)
	requestReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-request")
	requestRR := httptest.NewRecorder()
	mux.ServeHTTP(requestRR, requestReq)
	if requestRR.Code != http.StatusOK {
		t.Fatalf("mounted continue worker request route status = %d, want %d", requestRR.Code, http.StatusOK)
	}
	var requestSnapshot map[string]any
	if err := json.Unmarshal(requestRR.Body.Bytes(), &requestSnapshot); err != nil {
		t.Fatalf("decode proxied continue request snapshot: %v", err)
	}
	requestEvents, ok := requestSnapshot["execution_events"].([]any)
	if !ok || len(requestEvents) < 3 {
		t.Fatalf("expected execution events in request snapshot, got %#v", requestSnapshot["execution_events"])
	}
}

func TestBuildContinueBridgeEnvelope_PopulatesStructuredIDEContextVariables(t *testing.T) {
	envelope, err := buildContinueBridgeEnvelope(continueChatRequest{
		SessionID: "sess-ctx",
		Message:   "Apply the prepared patch",
		Mode:      "apply",
		Workspace: struct {
			Root      string `json:"root"`
			Active    string `json:"active_file"`
			Selection struct {
				StartLine int `json:"start_line"`
				EndLine   int `json:"end_line"`
			} `json:"selection"`
		}{Root: ".", Active: "payments/service.go"},
		Context: struct {
			OpenFiles    []string `json:"open_files"`
			SelectedText string   `json:"selected_text"`
		}{OpenFiles: []string{"payments/service.go", "payments/service_test.go"}, SelectedText: "func processPayment()"},
	})
	if err != nil {
		t.Fatalf("buildContinueBridgeEnvelope failed: %v", err)
	}
	if envelope.Ask.Variables["active_file"] != "payments/service.go" {
		t.Fatalf("expected active_file variable, got %#v", envelope.Ask.Variables)
	}
	if envelope.Ask.Variables["selected_text"] != "func processPayment()" {
		t.Fatalf("expected selected_text variable, got %#v", envelope.Ask.Variables)
	}
	if envelope.Ask.Variables["workspace_root"] != cwd() {
		t.Fatalf("expected normalized workspace_root variable, got %#v", envelope.Ask.Variables["workspace_root"])
	}
	openFiles, ok := envelope.Ask.Variables["open_files"].([]string)
	if !ok || len(openFiles) != 2 {
		t.Fatalf("expected open_files variable, got %#v", envelope.Ask.Variables["open_files"])
	}
}

func TestBuildContinueBridgeEnvelope_PreservesContinueToolMetadata(t *testing.T) {
	envelope, err := buildContinueBridgeEnvelope(continueChatRequest{
		SessionID: "sess-tools",
		Message:   "Apply the prepared patch",
		Mode:      "apply",
		Tools: []continueChatTool{
			{Type: "function", Function: continueChatToolFunction{Name: "read_file"}},
			{Type: "function", Function: continueChatToolFunction{Name: "multi_edit"}},
			{Type: "function", Function: continueChatToolFunction{Name: "get_problems"}},
		},
		ToolChoice: continueChatToolChoice{
			Type:     "function",
			Function: continueChatToolFunction{Name: "multi_edit"},
		},
	})
	if err != nil {
		t.Fatalf("buildContinueBridgeEnvelope failed: %v", err)
	}
	availableTools, ok := envelope.Ask.Variables["continue_available_tools"].([]string)
	if !ok || len(availableTools) != 3 {
		t.Fatalf("expected continue_available_tools variable, got %#v", envelope.Ask.Variables["continue_available_tools"])
	}
	phaseTools, ok := envelope.Ask.Variables["continue_phase_tools"].([]string)
	if !ok || len(phaseTools) == 0 {
		t.Fatalf("expected continue_phase_tools variable, got %#v", envelope.Ask.Variables["continue_phase_tools"])
	}
	phaseAvailable, ok := envelope.Ask.Variables["continue_phase_available_tools"].([]string)
	if !ok || len(phaseAvailable) != 2 || phaseAvailable[0] != "multi_edit" || phaseAvailable[1] != "read_file" {
		t.Fatalf("expected execution-phase available tool intersection, got %#v", envelope.Ask.Variables["continue_phase_available_tools"])
	}
	if envelope.Ask.Variables["continue_tool_choice"] != "multi_edit" {
		t.Fatalf("expected continue_tool_choice variable, got %#v", envelope.Ask.Variables["continue_tool_choice"])
	}
	checks := []string{
		"[requested_phase=execution]",
		"[continue_available_tools=get_problems,multi_edit,read_file]",
		"[continue_phase_available_tools=multi_edit,read_file]",
		"[continue_tool_choice=multi_edit]",
	}
	for _, check := range checks {
		if !strings.Contains(envelope.Ask.FullPrompt, check) {
			t.Fatalf("expected %q in full prompt, got %q", check, envelope.Ask.FullPrompt)
		}
	}
}

func TestBuildContinueBridgeEnvelope_TrimsPromptOpenFilesButPreservesStructuredContext(t *testing.T) {
	envelope, err := buildContinueBridgeEnvelope(continueChatRequest{
		SessionID: "sess-open-files",
		Message:   "Review the payment flow",
		Mode:      "review",
		Context: struct {
			OpenFiles    []string `json:"open_files"`
			SelectedText string   `json:"selected_text"`
		}{OpenFiles: []string{
			"payments/service.go",
			"payments/service_test.go",
			"payments/retry.go",
			"payments/retry.go",
			"docs/PAYMENTS.md",
			"",
		}},
	})
	if err != nil {
		t.Fatalf("buildContinueBridgeEnvelope failed: %v", err)
	}
	openFiles, ok := envelope.Ask.Variables["open_files"].([]string)
	if !ok {
		t.Fatalf("expected structured open_files variable, got %#v", envelope.Ask.Variables["open_files"])
	}
	if len(openFiles) != 4 {
		t.Fatalf("expected four normalized open files, got %#v", openFiles)
	}
	if openFiles[0] != "payments/service.go" || openFiles[3] != "docs/PAYMENTS.md" {
		t.Fatalf("expected original ordering for normalized open files, got %#v", openFiles)
	}
	if !strings.Contains(envelope.Ask.FullPrompt, "[open_files=payments/service.go,payments/service_test.go,payments/retry.go,+1_more]") {
		t.Fatalf("expected trimmed open_files prompt, got %q", envelope.Ask.FullPrompt)
	}
	if strings.Contains(envelope.Ask.FullPrompt, "payments/retry.go,payments/retry.go") || strings.Contains(envelope.Ask.FullPrompt, "docs/PAYMENTS.md]") {
		t.Fatalf("expected prompt open files to be deduplicated and trimmed, got %q", envelope.Ask.FullPrompt)
	}
}

func TestBuildContinueBridgeEnvelope_TrimsPromptSelectedTextButPreservesStructuredContext(t *testing.T) {
	selectedText := strings.Repeat("func processPayment() error { return nil } ", 12)
	expectedSelectedText := strings.TrimSpace(selectedText)
	envelope, err := buildContinueBridgeEnvelope(continueChatRequest{
		SessionID: "sess-selected-text",
		Message:   "Explain this selection",
		Mode:      "review",
		Context: struct {
			OpenFiles    []string `json:"open_files"`
			SelectedText string   `json:"selected_text"`
		}{SelectedText: selectedText},
	})
	if err != nil {
		t.Fatalf("buildContinueBridgeEnvelope failed: %v", err)
	}
	if envelope.Ask.Variables["selected_text"] != expectedSelectedText {
		t.Fatalf("expected full selected_text in structured variables, got %#v", envelope.Ask.Variables["selected_text"])
	}
	if !strings.Contains(envelope.Ask.FullPrompt, "[selected_text=") {
		t.Fatalf("expected selected_text in prompt, got %q", envelope.Ask.FullPrompt)
	}
	if !strings.Contains(envelope.Ask.FullPrompt, "...") {
		t.Fatalf("expected trimmed selected_text prompt with ellipsis, got %q", envelope.Ask.FullPrompt)
	}
	if strings.Contains(envelope.Ask.FullPrompt, expectedSelectedText) {
		t.Fatalf("expected prompt selected_text to be trimmed, got %q", envelope.Ask.FullPrompt)
	}
}

func TestBuildContinueBridgeEnvelope_AddsCompactRoutingHintForNarrowSelectionRequests(t *testing.T) {
	envelope, err := buildContinueBridgeEnvelope(continueChatRequest{
		SessionID: "sess-routing-compact",
		Message:   "Explain this function",
		Mode:      "review",
		Context: struct {
			OpenFiles    []string `json:"open_files"`
			SelectedText string   `json:"selected_text"`
		}{
			OpenFiles:    []string{"payments/service.go"},
			SelectedText: "func processPayment() error { return nil }",
		},
	})
	if err != nil {
		t.Fatalf("buildContinueBridgeEnvelope failed: %v", err)
	}
	if envelope.Ask.Variables["continue_routing_tier"] != "compact" || envelope.Ask.Variables["continue_routing_reason"] != "narrow_selection_request" {
		t.Fatalf("expected compact routing hint, got %#v", envelope.Ask.Variables)
	}
	if envelope.Ask.Variables["continue_model_routing_source"] != "daemon_auto" || envelope.Ask.Variables["continue_model_routing_tier"] != "compact" {
		t.Fatalf("expected daemon auto model routing for compact tier, got %#v", envelope.Ask.Variables)
	}
	if strings.TrimSpace(envelope.Ask.ProviderName) == "" || strings.TrimSpace(envelope.Ask.ModelName) == "" {
		t.Fatalf("expected daemon auto provider/model for compact tier, got provider=%q model=%q", envelope.Ask.ProviderName, envelope.Ask.ModelName)
	}
	if envelope.Ask.Variables["llm_provider"] != envelope.Ask.ProviderName || envelope.Ask.Variables["llm_model"] != envelope.Ask.ModelName {
		t.Fatalf("expected llm provider/model variables to match auto-selected ask provider/model, got %#v", envelope.Ask.Variables)
	}
	if !strings.Contains(envelope.Ask.FullPrompt, "[continue_routing_tier=compact]") || !strings.Contains(envelope.Ask.FullPrompt, "[continue_routing_reason=narrow_selection_request]") {
		t.Fatalf("expected compact routing hint in prompt, got %q", envelope.Ask.FullPrompt)
	}
}

func TestBuildContinueBridgeEnvelope_AddsDeliberateRoutingHintForComplexRequests(t *testing.T) {
	envelope, err := buildContinueBridgeEnvelope(continueChatRequest{
		SessionID: "sess-routing-deliberate",
		Message:   "Debug the architecture issue across these files",
		Mode:      "plan",
		Context: struct {
			OpenFiles    []string `json:"open_files"`
			SelectedText string   `json:"selected_text"`
		}{
			OpenFiles: []string{"payments/service.go", "payments/service_test.go", "payments/retry.go", "payments/worker.go"},
		},
	})
	if err != nil {
		t.Fatalf("buildContinueBridgeEnvelope failed: %v", err)
	}
	if envelope.Ask.Variables["continue_routing_tier"] != "deliberate" || envelope.Ask.Variables["continue_routing_reason"] != "multi_file_or_complex_reasoning" {
		t.Fatalf("expected deliberate routing hint, got %#v", envelope.Ask.Variables)
	}
	if envelope.Ask.Variables["continue_model_routing_source"] != "daemon_auto" || envelope.Ask.Variables["continue_model_routing_tier"] != "deliberate" {
		t.Fatalf("expected daemon auto model routing for deliberate tier, got %#v", envelope.Ask.Variables)
	}
	if strings.TrimSpace(envelope.Ask.ProviderName) == "" || strings.TrimSpace(envelope.Ask.ModelName) == "" {
		t.Fatalf("expected daemon auto provider/model for deliberate tier, got provider=%q model=%q", envelope.Ask.ProviderName, envelope.Ask.ModelName)
	}
	if !strings.Contains(envelope.Ask.FullPrompt, "[continue_routing_tier=deliberate]") || !strings.Contains(envelope.Ask.FullPrompt, "[continue_routing_reason=multi_file_or_complex_reasoning]") {
		t.Fatalf("expected deliberate routing hint in prompt, got %q", envelope.Ask.FullPrompt)
	}
}

func TestBuildContinueBridgeEnvelope_PropagatesHQSessionContext(t *testing.T) {
	envelope, err := buildContinueBridgeEnvelope(continueChatRequest{
		SessionID: "sess-auth",
		Message:   "Apply the prepared patch",
		Mode:      "apply",
		Auth: struct {
			AccessToken  string `json:"access_token,omitempty"`
			RefreshToken string `json:"refresh_token,omitempty"`
		}{AccessToken: "hq-access", RefreshToken: "hq-refresh"},
		HQ: struct {
			BaseURL   string `json:"base_url,omitempty"`
			Port      string `json:"port,omitempty"`
			UserID    string `json:"user_id,omitempty"`
			SessionID string `json:"session_id,omitempty"`
		}{BaseURL: "https://northstar.example.com", UserID: "user-42", SessionID: "hq-session-42"},
	})
	if err != nil {
		t.Fatalf("buildContinueBridgeEnvelope failed: %v", err)
	}
	if envelope.Ask.AccessToken != "hq-access" || envelope.Ask.RefreshToken != "hq-refresh" {
		t.Fatalf("expected HQ tokens on bridge ask, got %#v", envelope.Ask)
	}
	if envelope.Ask.HQBaseURL != "https://northstar.example.com" || envelope.Ask.HQUserID != "user-42" || envelope.Ask.HQSessionID != "hq-session-42" {
		t.Fatalf("expected HQ session context on bridge ask, got %#v", envelope.Ask)
	}
}

func TestContinueIDEWorkerBackend_ExecuteTaskProxiesRegisteredWorker(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	daemonSessionStore.mu.Lock()
	oldSessionStore := daemonSessionStore.data
	daemonSessionStore.data = map[string]daemonSessionContext{}
	daemonSessionStore.mu.Unlock()
	t.Cleanup(func() {
		daemonSessionStore.mu.Lock()
		daemonSessionStore.data = oldSessionStore
		daemonSessionStore.mu.Unlock()
	})

	daemonSessionStore.mu.Lock()
	daemonSessionStore.data["continue-worker-2"] = daemonSessionContext{
		AccessToken:  "hq-access-token",
		RefreshToken: "hq-refresh-token",
		HQBaseURL:    "https://northstar.example.com",
		HQUserID:     "user-42",
		HQSessionID:  "hq-session-42",
	}
	daemonSessionStore.mu.Unlock()

	var received continueAgentExecuteRequest
	worker := newLoopbackTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"type\":\"progress\",\"message\":\"worker applying patch\"}\n{\"type\":\"done\",\"result\":{\"status\":\"completed\"}}\n"))
	}))
	defer worker.Close()

	service.ensureContinueWorkerStore().Put(continueWorkerRegistration{
		SessionID:    "continue-worker-2",
		CallbackURL:  worker.URL,
		Capabilities: []string{"apply_patch", "read_context"},
		IDE:          "vscode",
	})

	backend := newContinueIDEWorkerBackend(service)
	result, err := backend.ExecuteTask(context.Background(), ideworker.TaskRequest{
		RequestID:      "client-request-backend-1",
		SessionID:      "continue-worker-2",
		Task:           "Apply the prepared payment patch",
		Mode:           "apply",
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-5",
		RequestedPhase: "execution",
		AvailableTools: []string{"read_file", "multi_edit", "get_problems"},
		PhaseTools:     []string{"read_file", "multi_edit", "run_terminal_command"},
		AllowedTools:   []string{"read_file", "multi_edit"},
		ToolChoice:     "multi_edit",
		WorkspaceRoot:  ".",
		ActiveFile:     "payments/service.go",
		SelectedText:   "func processPayment()",
		OpenFiles:      []string{"payments/service.go"},
		Guidance: ideworker.DelegationGuidance{
			TeamPractices:        []string{"Preserve payment invariants", "Add targeted tests"},
			SuccessCriteria:      []string{"Return structured execution result"},
			ExpectedResultFormat: "structured_execution_result",
			ReferenceContext: []ideworker.DelegationReference{{
				Kind:    "ltm_note",
				Source:  "Team Patterns/Payments",
				Title:   "Payments practices",
				Content: "Prefer explicit error wrapping.",
			}},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}
	if received.Task != "Apply the prepared payment patch" || received.Context.ActiveFile != "payments/service.go" {
		t.Fatalf("unexpected worker request: %#v", received)
	}
	if received.RequestID == "" || received.RequestID == "client-request-backend-1" {
		t.Fatalf("expected daemon-generated request id on worker request, got %#v", received)
	}
	if received.Provider != "anthropic" || received.Model != "claude-sonnet-4-5" {
		t.Fatalf("expected provider/model on worker request, got %#v", received)
	}
	if received.Auth.AccessToken != "hq-access-token" || received.Auth.RefreshToken != "hq-refresh-token" {
		t.Fatalf("expected HQ auth handoff on worker request, got %#v", received.Auth)
	}
	if received.HQ.BaseURL != "https://northstar.example.com" || received.HQ.UserID != "user-42" || received.HQ.SessionID != "hq-session-42" {
		t.Fatalf("expected HQ connectivity handoff on worker request, got %#v", received.HQ)
	}
	if received.RequestedPhase != "execution" || received.ToolChoice != "multi_edit" {
		t.Fatalf("expected phase/tool choice metadata on worker request, got %#v", received)
	}
	if len(received.AllowedTools) != 2 || received.AllowedTools[0] != "read_file" || received.AllowedTools[1] != "multi_edit" {
		t.Fatalf("expected allowed tools on worker request, got %#v", received)
	}
	if !received.ResultContract.StreamProgress || !received.ResultContract.IncludeModifiedFiles || !received.ResultContract.IncludeDiffDeltas || !received.ResultContract.PreferArtifactRefs {
		t.Fatalf("expected hybrid result contract on worker request, got %#v", received.ResultContract)
	}
	if received.ResultContract.MaxInlineDiffBytes != 16384 {
		t.Fatalf("expected bounded inline diff size, got %#v", received.ResultContract)
	}
	if len(received.Guidance.TeamPractices) != 2 || received.Guidance.ExpectedResultFormat != "structured_execution_result" {
		t.Fatalf("expected structured guidance on worker request, got %#v", received.Guidance)
	}
	if !containsString(received.Guidance.ImplementationNotes, "Prefer patch-style or search/replace edits over whole-file rewrites.") {
		t.Fatalf("expected patch-first implementation note on worker request, got %#v", received.Guidance.ImplementationNotes)
	}
	if !containsString(received.Guidance.SuccessCriteria, "Return a structured execution result with modified_files metadata.") {
		t.Fatalf("expected structured diff success criteria on worker request, got %#v", received.Guidance.SuccessCriteria)
	}
	for _, want := range []string{"\"status\": \"completed\""} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected canonical JSON execution result to contain %q, got %q", want, result)
		}
	}
}

func TestContinueIDEWorkerBackend_ExecuteTaskAddsStructuredDefaultsForPatchStyleDelegation(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	var received continueAgentExecuteRequest
	worker := newLoopbackTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"type\":\"done\",\"result\":{\"status\":\"completed\"}}\n"))
	}))
	defer worker.Close()

	service.ensureContinueWorkerStore().Put(continueWorkerRegistration{
		SessionID:    "continue-worker-3",
		CallbackURL:  worker.URL,
		Capabilities: []string{"apply_patch", "read_context"},
		IDE:          "vscode",
	})

	backend := newContinueIDEWorkerBackend(service)
	_, err := backend.ExecuteTask(context.Background(), ideworker.TaskRequest{
		SessionID:      "continue-worker-3",
		Task:           "Apply the prepared patch",
		Mode:           "apply",
		RequestedPhase: "execution",
		ToolChoice:     "multi_edit",
		ActiveFile:     "payments/service.go",
	})
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}
	if received.Guidance.ExpectedResultFormat != "structured_execution_result" {
		t.Fatalf("expected default structured result format, got %#v", received.Guidance)
	}
	if !containsString(received.Guidance.ImplementationNotes, "Prefer patch-style or search/replace edits over whole-file rewrites.") {
		t.Fatalf("expected default patch-first implementation note, got %#v", received.Guidance.ImplementationNotes)
	}
	if !containsString(received.Guidance.SuccessCriteria, "Keep changes bounded to the requested slice.") {
		t.Fatalf("expected bounded-change success criteria, got %#v", received.Guidance.SuccessCriteria)
	}
}

func TestReadContinueWorkerResponse_AvoidsBufferingLargeStructuredDonePayload(t *testing.T) {
	largeDiff := strings.Repeat("diff-line\n", 12000)
	body := strings.NewReader("{\"type\":\"progress\",\"message\":\"worker applying patch\"}\n" +
		"{\"type\":\"file_change\",\"file\":{\"path\":\"payments/service.go\",\"operation\":\"update\",\"summary\":\"Applied payment patch\"}}\n" +
		fmt.Sprintf("{\"type\":\"done\",\"result\":{\"status\":\"completed\",\"summary\":\"Patched payments/service.go\",\"modified_files\":[{\"file_path\":\"payments/service.go\",\"diff_delta\":%q}]}}\n", largeDiff))

	outcome, err := readContinueWorkerResponse(body, nil, true)
	if err != nil {
		t.Fatalf("readContinueWorkerResponse failed: %v", err)
	}
	if outcome.FinalResult == nil || outcome.FinalResult.Summary != "Patched payments/service.go" {
		t.Fatalf("expected structured final result, got %#v", outcome.FinalResult)
	}
	if !strings.Contains(outcome.RawBody, "worker applying patch") {
		t.Fatalf("expected progress summary in raw body, got %q", outcome.RawBody)
	}
	if strings.Contains(outcome.RawBody, largeDiff) {
		t.Fatalf("expected raw body to exclude large structured done payload")
	}
	if len(outcome.RawBody) >= len(largeDiff) {
		t.Fatalf("expected raw body to stay bounded, got %d bytes", len(outcome.RawBody))
	}
	if len(outcome.Events) < 3 {
		t.Fatalf("expected typed stream events, got %#v", outcome.Events)
	}
	if outcome.Events[1].Type != "file_change" || outcome.Events[1].FilePath != "payments/service.go" || outcome.Events[1].CRUDOperation != "update" {
		t.Fatalf("expected typed file_change stream event, got %#v", outcome.Events[1])
	}
}

func TestDefaultIDEWorkerBackend_ExecuteTaskProxiesRegisteredContinueWorker(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}, shellSessions: newPTYShellSessionManager()}
	defer service.shellSessions.CloseAll()

	var received continueAgentExecuteRequest
	worker := newLoopbackTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("delegated by continue worker"))
	}))
	defer worker.Close()

	service.ensureContinueWorkerStore().Put(continueWorkerRegistration{
		SessionID:    "continue-worker-default",
		CallbackURL:  worker.URL,
		Capabilities: []string{"apply_patch", "read_context"},
		IDE:          "vscode",
	})

	backend := newDefaultIDEWorkerBackend(service)
	result, err := backend.ExecuteTask(context.Background(), ideworker.TaskRequest{
		RequestID:      "client-request-default-1",
		SessionID:      "continue-worker-default",
		Task:           "Apply the prepared payment patch",
		Mode:           "apply",
		Provider:       "gemini",
		Model:          "gemini-2.5-pro",
		RequestedPhase: "execution",
		AllowedTools:   []string{"read_file", "multi_edit"},
		ToolChoice:     "multi_edit",
		ActiveFile:     "payments/service.go",
	})
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}
	if received.Task != "Apply the prepared payment patch" || received.Context.ActiveFile != "payments/service.go" {
		t.Fatalf("unexpected worker request: %#v", received)
	}
	if received.RequestID == "" || received.RequestID == "client-request-default-1" {
		t.Fatalf("expected daemon-generated request id on default backend worker request, got %#v", received)
	}
	if received.Provider != "gemini" || received.Model != "gemini-2.5-pro" {
		t.Fatalf("expected provider/model on default backend worker request, got %#v", received)
	}
	if received.RequestedPhase != "execution" || received.ToolChoice != "multi_edit" {
		t.Fatalf("expected requested phase and tool choice on default backend worker request, got %#v", received)
	}
	if !received.ResultContract.StreamProgress || !received.ResultContract.PreferArtifactRefs {
		t.Fatalf("expected result contract to propagate on default backend worker request, got %#v", received.ResultContract)
	}
	if result != "delegated by continue worker" {
		t.Fatalf("unexpected default backend result: %q", result)
	}
}

func TestDefaultIDEWorkerBackend_InspectSessionFallsBackToDaemonShell(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer mgr.CloseAll()
	if _, _, err := mgr.Run("daemon-shell-worker", "cd /tmp", 2*time.Second); err != nil {
		t.Fatalf("seed shell session failed: %v", err)
	}

	service := &localRAGService{service: &recordingQueryAgent{}, shellSessions: mgr}
	backend := newDefaultIDEWorkerBackend(service)
	state, err := backend.InspectSession(context.Background(), "daemon-shell-worker")
	if err != nil {
		t.Fatalf("InspectSession failed: %v", err)
	}
	if !state.Connected {
		t.Fatalf("expected daemon shell session to be surfaced as connected: %#v", state)
	}
	if state.IDE != "daemon_shell" {
		t.Fatalf("expected daemon_shell IDE backend, got %#v", state)
	}
	if len(state.WorkerCapabilities) != 2 || state.WorkerCapabilities[0] != "terminal_execution" || state.WorkerCapabilities[1] != "workspace_context" {
		t.Fatalf("unexpected daemon shell capabilities: %#v", state.WorkerCapabilities)
	}
	if !strings.Contains(state.WorkspaceRoot, "/tmp") {
		t.Fatalf("expected daemon shell workspace root to reflect pwd, got %#v", state)
	}
}

func TestRegisterBridgeRoutes_FallsBackWhenRegisteredWorkerLacksRequiredCapabilities(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	mux := http.NewServeMux()
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})
	service.registerBridgeRoutes(mux, reg, nil)

	workerCalled := false
	worker := newLoopbackTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"type\":\"done\"}\n"))
	}))
	defer worker.Close()

	registerReq := httptest.NewRequest(http.MethodPost, "/api/continue/worker/register", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-worker-fallback","callback_url":"%s","capabilities":["plan_changes"],"ide":"vscode"}`, worker.URL)))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-register-fallback")
	registerRR := httptest.NewRecorder()
	mux.ServeHTTP(registerRR, registerReq)
	if registerRR.Code != http.StatusOK {
		t.Fatalf("mounted continue worker register route status = %d, want %d", registerRR.Code, http.StatusOK)
	}

	executeReq := httptest.NewRequest(http.MethodPost, "/api/continue/agent/execute", strings.NewReader(`{"session_id":"continue-worker-fallback","task":"Apply the prepared payment patch","mode":"apply","context":{"workspace_root":".","active_file":"payments/service.go","selected_text":"func processPayment()","open_files":["payments/service.go"]}}`))
	executeReq.Header.Set("Content-Type", "application/json")
	executeReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-fallback-execute")
	executeRR := httptest.NewRecorder()
	mux.ServeHTTP(executeRR, executeReq)
	if executeRR.Code != http.StatusOK {
		t.Fatalf("fallback continue agent execute status = %d, want %d", executeRR.Code, http.StatusOK)
	}
	if workerCalled {
		t.Fatalf("expected daemon-local fallback when worker lacks apply capability")
	}
	if !strings.Contains(executeRR.Body.String(), "Executing editor-aware coding task") || !strings.Contains(executeRR.Body.String(), `"type":"done"`) {
		t.Fatalf("expected daemon-local fallback stream response, got %q", executeRR.Body.String())
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/continue/sessions/continue-worker-fallback", nil)
	sessionReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-fallback-session")
	sessionRR := httptest.NewRecorder()
	mux.ServeHTTP(sessionRR, sessionReq)
	if sessionRR.Code != http.StatusOK {
		t.Fatalf("mounted continue fallback session route status = %d, want %d", sessionRR.Code, http.StatusOK)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(sessionRR.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode fallback continue session snapshot: %v", err)
	}
	if snapshot["status"] != "completed" || snapshot["phase"] != "execution" {
		t.Fatalf("expected completed execution snapshot for daemon fallback path, got %#v", snapshot)
	}
	requestID, ok := snapshot["request_id"].(string)
	if !ok || requestID == "" {
		t.Fatalf("expected daemon-generated request id on fallback snapshot, got %#v", snapshot)
	}

	requestReq := httptest.NewRequest(http.MethodGet, "/api/continue/requests/"+requestID, nil)
	requestReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-fallback-request")
	requestRR := httptest.NewRecorder()
	mux.ServeHTTP(requestRR, requestReq)
	if requestRR.Code != http.StatusOK {
		t.Fatalf("mounted continue fallback request route status = %d, want %d", requestRR.Code, http.StatusOK)
	}
	var requestSnapshot map[string]any
	if err := json.Unmarshal(requestRR.Body.Bytes(), &requestSnapshot); err != nil {
		t.Fatalf("decode fallback continue request snapshot: %v", err)
	}
	result, ok := requestSnapshot["execution_result"].(map[string]any)
	if !ok {
		t.Fatalf("expected typed execution_result in fallback request snapshot, got %#v", requestSnapshot["execution_result"])
	}
	if result["status"] != "completed" {
		t.Fatalf("expected completed typed execution_result in fallback request snapshot, got %#v", result)
	}
}

func TestRegisterBridgeRoutes_RejectsWorkerRegistrationWithNonLoopbackCallbackURL(t *testing.T) {
	service := &localRAGService{service: &recordingQueryAgent{}}
	mux := http.NewServeMux()
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})
	service.registerBridgeRoutes(mux, reg, nil)

	registerReq := httptest.NewRequest(http.MethodPost, "/api/continue/worker/register", strings.NewReader(`{"session_id":"continue-worker-remote","callback_url":"http://example.com/continue/worker","capabilities":["apply_patch"],"ide":"vscode"}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set("X-SOP-Session-ID", "bridge-route-worker-register-remote")
	registerRR := httptest.NewRecorder()
	mux.ServeHTTP(registerRR, registerReq)
	if registerRR.Code != http.StatusBadRequest {
		t.Fatalf("non-loopback worker registration status = %d, want %d", registerRR.Code, http.StatusBadRequest)
	}
	if !strings.Contains(registerRR.Body.String(), "loopback") {
		t.Fatalf("expected loopback validation error, got %q", registerRR.Body.String())
	}
}

func TestRegisterBridgeRoutes_RequiresBootstrapAuthenticatedBearerToken(t *testing.T) {
	withIsolatedSystemDB(t)
	xdgConfigHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

	authHandler := &DaemonAuthHandler{
		bootstrapSecret: "stale-secret",
		bootstrapPath:   filepath.Join(xdgConfigHome, "sop", "daemon.bootstrap.bridge-client"),
		sessionStore:    sopbackend.NewSessionStore(time.Minute),
		config:          defaultDaemonAuthConfig,
	}
	if err := os.MkdirAll(filepath.Dir(authHandler.bootstrapPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(authHandler.bootstrapPath, []byte("fresh-secret"), 0o600); err != nil {
		t.Fatalf("write bootstrap secret: %v", err)
	}

	service := &localRAGService{service: &recordingQueryAgent{}}
	mux := http.NewServeMux()
	reg := NewSessionRegistry(SubscriptionPolicy{MaxRequests: 20, Window: time.Minute})
	service.registerBridgeRoutes(mux, reg, authHandler)

	unauthMetadataReq := httptest.NewRequest(http.MethodGet, "/api/continue/metadata", nil)
	unauthMetadataRR := httptest.NewRecorder()
	mux.ServeHTTP(unauthMetadataRR, unauthMetadataReq)
	if unauthMetadataRR.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated metadata status = %d, want %d", unauthMetadataRR.Code, http.StatusUnauthorized)
	}
	unauthChatReq := httptest.NewRequest(http.MethodPost, "/api/continue/chat", strings.NewReader(`{"session_id":"continue-auth-unauth","message":"hello","workspace":{"root":"."}}`))
	unauthChatReq.Header.Set("Content-Type", "application/json")
	unauthChatRR := httptest.NewRecorder()
	mux.ServeHTTP(unauthChatRR, unauthChatReq)
	if unauthChatRR.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated continue chat status = %d, want %d", unauthChatRR.Code, http.StatusUnauthorized)
	}
	unauthRegisterReq := httptest.NewRequest(http.MethodPost, "/api/continue/worker/register", strings.NewReader(`{"session_id":"continue-auth-unauth","callback_url":"http://127.0.0.1:9999","capabilities":["read_context"],"ide":"vscode"}`))
	unauthRegisterReq.Header.Set("Content-Type", "application/json")
	unauthRegisterRR := httptest.NewRecorder()
	mux.ServeHTTP(unauthRegisterRR, unauthRegisterReq)
	if unauthRegisterRR.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated worker register status = %d, want %d", unauthRegisterRR.Code, http.StatusUnauthorized)
	}
	dir := t.TempDir()
	filePath := filepath.Join(dir, "auth-readme.txt")
	if err := os.WriteFile(filePath, []byte("auth check"), 0o644); err != nil {
		t.Fatalf("write auth temp file: %v", err)
	}
	unauthToolReq := httptest.NewRequest(http.MethodPost, "/api/continue/tools/repo.read_file", strings.NewReader(fmt.Sprintf(`{"path":"%s","start_line":1,"end_line":1}`, filePath)))
	unauthToolReq.Header.Set("Content-Type", "application/json")
	unauthToolRR := httptest.NewRecorder()
	mux.ServeHTTP(unauthToolRR, unauthToolReq)
	if unauthToolRR.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated continue tool status = %d, want %d", unauthToolRR.Code, http.StatusUnauthorized)
	}

	bootstrapChallenge := authHandler.issueBootstrapChallenge()
	if bootstrapChallenge == nil {
		t.Fatal("expected bootstrap challenge")
	}
	bootstrapBody := fmt.Sprintf(`{"bootstrap_secret":"fresh-secret","bootstrap_challenge_id":"%s","bootstrap_challenge_signature":"%s","client_id":"bridge-client"}`, bootstrapChallenge.ChallengeID, signBootstrapChallenge("fresh-secret", bootstrapChallenge))
	bootstrapReq := httptest.NewRequest(http.MethodPost, "/api/daemon/auth", strings.NewReader(bootstrapBody))
	bootstrapRR := httptest.NewRecorder()
	authHandler.HandleBootstrap(bootstrapRR, bootstrapReq)
	if bootstrapRR.Code != http.StatusOK {
		t.Fatalf("bootstrap auth status = %d, want %d with body %s", bootstrapRR.Code, http.StatusOK, bootstrapRR.Body.String())
	}
	var authPayload map[string]any
	if err := json.Unmarshal(bootstrapRR.Body.Bytes(), &authPayload); err != nil {
		t.Fatalf("decode bootstrap auth response: %v", err)
	}
	accessToken, _ := authPayload["access_token"].(string)
	if strings.TrimSpace(accessToken) == "" {
		t.Fatalf("expected bootstrap auth access token, got %#v", authPayload)
	}

	authMetadataReq := httptest.NewRequest(http.MethodGet, "/api/continue/metadata", nil)
	authMetadataReq.Header.Set("Authorization", "Bearer "+accessToken)
	authMetadataRR := httptest.NewRecorder()
	mux.ServeHTTP(authMetadataRR, authMetadataReq)
	if authMetadataRR.Code != http.StatusOK {
		t.Fatalf("authenticated metadata status = %d, want %d", authMetadataRR.Code, http.StatusOK)
	}
	if !strings.Contains(authMetadataRR.Body.String(), `"client":"continue"`) {
		t.Fatalf("expected continue metadata payload, got %q", authMetadataRR.Body.String())
	}

	authChatReq := httptest.NewRequest(http.MethodPost, "/api/continue/chat", strings.NewReader(`{"session_id":"continue-auth-2","message":"hello","workspace":{"root":"."}}`))
	authChatReq.Header.Set("Authorization", "Bearer "+accessToken)
	authChatReq.Header.Set("Content-Type", "application/json")
	authChatRR := httptest.NewRecorder()
	mux.ServeHTTP(authChatRR, authChatReq)
	if authChatRR.Code != http.StatusOK {
		t.Fatalf("authenticated continue chat status = %d, want %d", authChatRR.Code, http.StatusOK)
	}

	authRegisterReq := httptest.NewRequest(http.MethodPost, "/api/continue/worker/register", strings.NewReader(`{"session_id":"continue-auth-registered","callback_url":"http://127.0.0.1:9999","capabilities":["read_context"],"ide":"vscode"}`))
	authRegisterReq.Header.Set("Authorization", "Bearer "+accessToken)
	authRegisterReq.Header.Set("Content-Type", "application/json")
	authRegisterRR := httptest.NewRecorder()
	mux.ServeHTTP(authRegisterRR, authRegisterReq)
	if authRegisterRR.Code != http.StatusOK {
		t.Fatalf("authenticated worker register status = %d, want %d", authRegisterRR.Code, http.StatusOK)
	}

	authToolReq := httptest.NewRequest(http.MethodPost, "/api/continue/tools/repo.read_file", strings.NewReader(fmt.Sprintf(`{"path":"%s","start_line":1,"end_line":1}`, filePath)))
	authToolReq.Header.Set("Authorization", "Bearer "+accessToken)
	authToolReq.Header.Set("Content-Type", "application/json")
	authToolRR := httptest.NewRecorder()
	mux.ServeHTTP(authToolRR, authToolReq)
	if authToolRR.Code != http.StatusOK {
		t.Fatalf("authenticated continue tool status = %d, want %d", authToolRR.Code, http.StatusOK)
	}

	executeReq := httptest.NewRequest(http.MethodPost, "/api/continue/agent/execute", strings.NewReader(`{"request_id":"client-request-auth-1","session_id":"continue-auth-1","task":"Summarize the active file and report the workspace root.","mode":"apply","context":{"workspace_root":".","active_file":"README.md","selected_text":"","open_files":["README.md"]}}`))
	executeReq.Header.Set("Authorization", "Bearer "+accessToken)
	executeReq.Header.Set("Content-Type", "application/json")
	executeRR := httptest.NewRecorder()
	mux.ServeHTTP(executeRR, executeReq)
	if executeRR.Code != http.StatusOK {
		t.Fatalf("authenticated continue agent execute status = %d, want %d", executeRR.Code, http.StatusOK)
	}
	if !strings.Contains(executeRR.Body.String(), "Executing editor-aware coding task") || !strings.Contains(executeRR.Body.String(), `"type":"done"`) {
		t.Fatalf("expected streamed execute response, got %q", executeRR.Body.String())
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/continue/sessions/continue-auth-1", nil)
	sessionReq.Header.Set("Authorization", "Bearer "+accessToken)
	sessionRR := httptest.NewRecorder()
	mux.ServeHTTP(sessionRR, sessionReq)
	if sessionRR.Code != http.StatusOK {
		t.Fatalf("authenticated continue session status = %d, want %d", sessionRR.Code, http.StatusOK)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(sessionRR.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode continue session snapshot: %v", err)
	}
	if snapshot["session_id"] != "continue-auth-1" || snapshot["status"] != "completed" || snapshot["phase"] != "execution" {
		t.Fatalf("expected completed authenticated execution snapshot, got %#v", snapshot)
	}
	requestID, ok := snapshot["request_id"].(string)
	if !ok || requestID == "" || requestID == "client-request-auth-1" {
		t.Fatalf("expected daemon-generated request_id, got %#v", snapshot["request_id"])
	}
}

func TestDefaultDaemonAuthConfigUsesOneHourAndOneWeek(t *testing.T) {
	if got, want := defaultDaemonAuthConfig.AccessTokenTTL, time.Hour; got != want {
		t.Fatalf("defaultDaemonAuthConfig.AccessTokenTTL = %v, want %v", got, want)
	}
	if got, want := defaultDaemonAuthConfig.RefreshTokenTTL, 7*24*time.Hour; got != want {
		t.Fatalf("defaultDaemonAuthConfig.RefreshTokenTTL = %v, want %v", got, want)
	}
}

func TestParseDaemonLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    daemonLogLevel
		wantErr bool
	}{
		{name: "default", input: "", want: daemonLogLevelInfo},
		{name: "debug", input: "debug", want: daemonLogLevelDebug},
		{name: "info", input: "info", want: daemonLogLevelInfo},
		{name: "warn", input: "warn", want: daemonLogLevelWarn},
		{name: "error", input: "error", want: daemonLogLevelError},
		{name: "invalid", input: "verbosez", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDaemonLogLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDaemonLogLevel(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDaemonLogLevel(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseDaemonLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCurrentStorageHealthStateUsesSharedPolicyWhenRuntimePolicyIsUnavailable(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = nil
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	if !currentVectorizationNeedsRecovery() {
		t.Fatal("expected shared storage-health policy to surface recovery need")
	}

	state := currentStoragePathRecoveryState()
	if state.Status != "read_only" {
		t.Fatalf("expected shared storage-health policy status to be read_only, got %q", state.Status)
	}
	if state.Path != "/tmp/shared-store" {
		t.Fatalf("expected shared storage-health policy path to be preserved, got %q", state.Path)
	}
}

func TestCurrentStorageHealthStateFallsBackToSharedPolicyWhenRuntimePolicyHasNoState(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	runtimePolicy := newRuntimeVectorizationFailurePolicy(tmpDir)
	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = runtimePolicy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	if !currentVectorizationNeedsRecovery() {
		t.Fatal("expected shared storage-health policy to surface recovery need even when the runtime policy is present but empty")
	}

	state := currentStoragePathRecoveryState()
	if state.Status != "read_only" {
		t.Fatalf("expected shared storage-health policy status to be read_only when the runtime policy has no state, got %q", state.Status)
	}
	if state.Path != "/tmp/shared-store" {
		t.Fatalf("expected shared storage-health policy path to be preserved when the runtime policy has no state, got %q", state.Path)
	}
}

func TestHandleUIHandoffIncludesStoragePathRecoveryState(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.RecordStoragePathFailure("write", "/tmp/fake-store", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	body := strings.NewReader(`{"query":"hello","hq":{"base_url":"https://example.test"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/handoff", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload UIHandoffResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode handoff payload: %v", err)
	}
	if !payload.NeedsRecovery {
		t.Fatal("expected handoff payload to report storage-path recovery needs")
	}
	if payload.StoragePathRecovery.Status != "degraded" {
		t.Fatalf("expected storage path recovery status to be degraded, got %q", payload.StoragePathRecovery.Status)
	}
	if payload.StoragePathRecovery.Path != "/tmp/fake-store" {
		t.Fatalf("expected storage path recovery path to be preserved, got %q", payload.StoragePathRecovery.Path)
	}
}

func TestRetryPendingVectorizationRecoveryHonorsBackoff(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-backoff", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := 0
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called++
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	activeRuntimeVectorizationFailurePolicy.RecordRetryAttempt("kb", "category", "cat-backoff")

	if count := retryPendingVectorizationRecovery(context.Background(), false); count != 0 {
		t.Fatalf("expected backoff to suppress immediate retry, got %d", count)
	}
	if called != 0 {
		t.Fatalf("expected recovery executor to be skipped while backoff is active, got %d calls", called)
	}
}

func TestRetryPendingVectorizationRecoverySkipsWhenStorageHealthDisablesAutoRetry(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-storage-gate", os.ErrNotExist)
	}
	policy.RecordStoragePathFailure("write", "/tmp/fake-store", syscall.EROFS)

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := 0
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called++
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	if count := retryPendingVectorizationRecovery(context.Background(), false); count != 0 {
		t.Fatalf("expected storage-health policy to block auto-retry, got %d", count)
	}
	if called != 0 {
		t.Fatalf("expected recovery executor to be skipped when auto-retry is disabled, got %d calls", called)
	}
}

func TestStorageHealthAllowsHigherLevelAutoRetryUsesSharedPolicyState(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	tmpDir := t.TempDir()
	runtimePolicy := newRuntimeVectorizationFailurePolicy(tmpDir)
	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = runtimePolicy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	restore := sop.SetDefaultStorageHealthPolicy(sop.NewStorageHealthPolicy())
	defer restore()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	if storageHealthAllowsHigherLevelAutoRetry() {
		t.Fatal("expected shared storage-health policy to disable higher-level auto-retry for read-only storage")
	}
}

func TestResolveVectorizationRecoveryStateDirUsesConfiguredPathWhenWritable(t *testing.T) {
	configDir := t.TempDir()
	customDir := filepath.Join(configDir, "recovery-store")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom dir: %v", err)
	}

	cfg := sopbackend.Config{VectorizationRecoveryStoragePath: customDir}
	stateDir := resolveVectorizationRecoveryStateDir(filepath.Join(configDir, "config.json"), cfg)
	if stateDir != customDir {
		t.Fatalf("expected configured path %q, got %q", customDir, stateDir)
	}
}

func TestResolveVectorizationRecoveryStateDirFallsBackWhenConfiguredPathIsUnavailable(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")
	customDir := filepath.Join(configDir, "recovery-store")
	if err := os.WriteFile(filepath.Join(customDir), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg := sopbackend.Config{VectorizationRecoveryStoragePath: customDir}
	stateDir := resolveVectorizationRecoveryStateDir(configPath, cfg)
	want := filepath.Join(configDir, "state")
	if stateDir != want {
		t.Fatalf("expected fallback path %q, got %q", want, stateDir)
	}
}

func TestConfigureRecoveryStatePersistenceUpdatesConfigFile(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")
	initialCfg := sopbackend.Config{ConfigFile: configPath, VectorizationRecoveryStoragePath: filepath.Join(configDir, "old-recovery")}
	sopbackend.SetConfig(initialCfg)

	t.Cleanup(func() {
		sopfs.SetRecoveryStateConfigUpdater(nil)
		sopfs.SetRecoveryStateCleanupPath("")
		sopbackend.SetConfig(sopbackend.Config{})
	})

	updater := configureRecoveryStatePersistence(initialCfg)
	if updater == nil {
		t.Fatal("expected recovery-state persistence hook to be registered")
	}

	newPath := filepath.Join(configDir, "new-recovery")
	if err := updater(newPath); err != nil {
		t.Fatalf("update recovery path: %v", err)
	}

	cfg := sopbackend.GetConfig()
	if cfg.VectorizationRecoveryStoragePath != newPath {
		t.Fatalf("expected config to record %q, got %q", newPath, cfg.VectorizationRecoveryStoragePath)
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}

	var persisted sopbackend.Config
	if err := json.Unmarshal(contents, &persisted); err != nil {
		t.Fatalf("decode persisted config: %v", err)
	}
	if persisted.VectorizationRecoveryStoragePath != newPath {
		t.Fatalf("expected persisted config to record %q, got %q", newPath, persisted.VectorizationRecoveryStoragePath)
	}
}

func TestBuildLocalRAGWelcomeContextIncludesRecoveryActions(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-9", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	contextHTML := buildLocalRAGWelcomeContext()
	if !strings.Contains(contextHTML, "recovery-action-button") {
		t.Fatalf("expected recovery action buttons in welcome context, got %q", contextHTML)
	}
	if !strings.Contains(contextHTML, `data-action="recover"`) {
		t.Fatalf("expected recover action metadata in welcome context, got %q", contextHTML)
	}
	if !strings.Contains(contextHTML, `data-action="ignore"`) {
		t.Fatalf("expected ignore action metadata in welcome context, got %q", contextHTML)
	}
	if !strings.Contains(contextHTML, `data-action="clear_and_rebuild"`) {
		t.Fatalf("expected clear-and-rebuild action metadata in welcome context, got %q", contextHTML)
	}
}

func TestHandleVectorizationRecoveryStatusClearAndRebuildActionInvokesRecoveryHandler(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-clear", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := false
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called = true
		if kbName != "kb" || targetType != "category" || targetID != "cat-clear" {
			t.Fatalf("unexpected recovery target: kb=%q targetType=%q targetID=%q", kbName, targetType, targetID)
		}
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	body := strings.NewReader(`{"action":"clear_and_rebuild","kb_name":"kb","target_type":"category","target_id":"cat-clear"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Fatal("expected clear_and_rebuild action to invoke recovery handler")
	}

	var payload struct {
		Updated bool   `json:"updated"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode clear-and-rebuild payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if !strings.Contains(strings.ToLower(payload.Message), "clear") {
		t.Fatalf("expected clear-and-rebuild message, got %q", payload.Message)
	}
}

func TestHandleVectorizationRecoveryStatusReturnsPersistedState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-4", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		HasRecovery bool                                `json:"has_recovery"`
		Latest      runtimeVectorizationRecoveryState   `json:"latest"`
		States      []runtimeVectorizationRecoveryState `json:"states"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery status payload: %v", err)
	}
	if !payload.HasRecovery {
		t.Fatalf("expected recovery payload to report recovery")
	}
	if len(payload.States) == 0 {
		t.Fatalf("expected recovery states to be listed")
	}
	if payload.Latest.TargetID != "cat-4" {
		t.Fatalf("expected latest target id to be cat-4, got %q", payload.Latest.TargetID)
	}
	if payload.Latest.Prompt == "" {
		t.Fatalf("expected recovery prompt to be present")
	}
}

func TestHandleVectorizationRecoveryStatusIncludesNeedsRecoveryFlag(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-8", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		NeedsRecovery bool `json:"needs_recovery"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery needs payload: %v", err)
	}
	if !payload.NeedsRecovery {
		t.Fatalf("expected needs_recovery to be true")
	}
}

func TestHandleVectorizationRecoveryStatusUsesSharedStorageHealthPolicyWhenRuntimePolicyUnavailable(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = nil
	defer func() { activeRuntimeVectorizationFailurePolicy = previous }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/shared-store", syscall.EROFS)

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		HasRecovery         bool                         `json:"has_recovery"`
		NeedsRecovery       bool                         `json:"needs_recovery"`
		StoragePathRecovery runtimeDataPathRecoveryState `json:"storage_path_recovery"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode shared storage-health payload: %v", err)
	}
	if !payload.HasRecovery {
		t.Fatalf("expected shared storage-health policy to mark recovery as present")
	}
	if !payload.NeedsRecovery {
		t.Fatalf("expected shared storage-health policy to require recovery")
	}
	if payload.StoragePathRecovery.Status != "read_only" {
		t.Fatalf("expected shared storage-health policy status to be read_only, got %q", payload.StoragePathRecovery.Status)
	}
	if payload.StoragePathRecovery.Path != "/tmp/shared-store" {
		t.Fatalf("expected shared storage-health policy path to be preserved, got %q", payload.StoragePathRecovery.Path)
	}
}

func TestHandleVectorizationRecoveryStatusIncludesAllSharedStoragePathStates(t *testing.T) {
	resetDefaultStorageHealthPolicyForTest(t)

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = nil
	defer func() { activeRuntimeVectorizationFailurePolicy = previous }()

	policy := sop.DefaultStorageHealthPolicy()
	policy.RecordFailure("write", "/tmp/first-store", os.ErrNotExist)
	policy.RecordFailure("write", "/tmp/second-store", syscall.EROFS)

	req := httptest.NewRequest(http.MethodGet, "/api/daemon/vectorization/recovery", nil)
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		StoragePathStates []runtimeDataPathRecoveryState `json:"storage_path_states"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode shared storage-path state payload: %v", err)
	}
	if len(payload.StoragePathStates) != 2 {
		t.Fatalf("expected 2 shared storage-path states, got %d", len(payload.StoragePathStates))
	}
}

func TestHandleVectorizationRecoveryStatusAcceptsRecoveryAction(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-5", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	body := strings.NewReader(`{"action":"ignore","kb_name":"kb","target_type":"category","target_id":"cat-5"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		Latest runtimeVectorizationRecoveryState `json:"latest"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery action payload: %v", err)
	}
	if payload.Latest.Status != "ignored" {
		t.Fatalf("expected ignored status after action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusScheduleRetryActionMarksScheduledState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-schedule", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	body := strings.NewReader(`{"action":"schedule_retry","kb_name":"kb","target_type":"category","target_id":"cat-schedule"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		Latest  runtimeVectorizationRecoveryState `json:"latest"`
		Updated bool                              `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode schedule-retry payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if payload.Latest.Status != "scheduled" {
		t.Fatalf("expected scheduled status after schedule_retry action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusGiveUpActionMarksAbandonedState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-giveup", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	body := strings.NewReader(`{"action":"give_up","kb_name":"kb","target_type":"category","target_id":"cat-giveup"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var payload struct {
		Latest  runtimeVectorizationRecoveryState `json:"latest"`
		Updated bool                              `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode give-up payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if payload.Latest.Status != "abandoned" {
		t.Fatalf("expected abandoned status after give_up action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusRecoverActionInvokesRecoveryHandler(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-6", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := false
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called = true
		if kbName != "kb" || targetType != "category" || targetID != "cat-6" {
			t.Fatalf("unexpected recovery target: kb=%q targetType=%q targetID=%q", kbName, targetType, targetID)
		}
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	body := strings.NewReader(`{"action":"recover","kb_name":"kb","target_type":"category","target_id":"cat-6"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Fatal("expected recover action to invoke recovery handler")
	}

	var payload struct {
		Latest  runtimeVectorizationRecoveryState `json:"latest"`
		Updated bool                              `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery action payload: %v", err)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
	if payload.Latest.Status != "recovered" {
		t.Fatalf("expected recovered status after recovery action, got %q", payload.Latest.Status)
	}
}

func TestHandleVectorizationRecoveryStatusRecoverAllActionRebuildsAllDegradedTargets(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-7", os.ErrNotExist)
	}
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-8", os.ErrNotExist)
	}

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	var attempted []string
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		attempted = append(attempted, kbName+":"+targetType+":"+targetID)
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	body := strings.NewReader(`{"action":"recover_all"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/vectorization/recovery", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleGetVectorizationRecoveryStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleGetVectorizationRecoveryStatus() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(attempted) != 2 {
		t.Fatalf("expected recover_all to attempt 2 targets, got %d", len(attempted))
	}

	var payload struct {
		RecoveredCount int  `json:"recovered_count"`
		Updated        bool `json:"updated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode recovery-all payload: %v", err)
	}
	if payload.RecoveredCount != 2 {
		t.Fatalf("expected recovered_count to be 2, got %d", payload.RecoveredCount)
	}
	if !payload.Updated {
		t.Fatal("expected updated flag to be true")
	}
}

func TestTrySimpleShellCommandFallbackSkipsAgentCommandsWithoutSlash(t *testing.T) {
	t.Skip("Bare command execution removed to avoid ambiguity with natural language prompts. Commands must use slash prefixes like /c, /cmd")
	svc := &localRAGService{shellSessions: newPTYShellSessionManager()}
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
	t.Skip("Bare command execution removed to avoid ambiguity with natural language prompts. Commands must use slash prefixes like /c, /cmd")
	svc := &localRAGService{shellSessions: newPTYShellSessionManager()}
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
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
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
	svc := &localRAGService{service: &stubSharedAgent{name: "shared"}, shellSessions: newPTYShellSessionManager()}
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
	svc := &localRAGService{service: localAgent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"list_tools","hq":{"session_id":"shared-slash-test"}}`))
	rr := httptest.NewRecorder()

	prevSessions := sopbackend.GetLoadedAgents()
	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": sharedAgent})
	defer sopbackend.SetLoadedAgents(prevSessions)

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
	svc := &localRAGService{service: localAgent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"show me list of changed files in this repo in git","hq":{"session_id":"shared-natural-language-test"}}`))
	rr := httptest.NewRecorder()

	prevSessions := sopbackend.GetLoadedAgents()
	sopbackend.SetLoadedAgents(map[string]ai.Agent[map[string]any]{"copilot": sharedAgent})
	defer sopbackend.SetLoadedAgents(prevSessions)

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
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
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
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
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

func TestHandleEncryptDaemonSecretReturnsEncryptedBlob(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/secrets/encrypt", strings.NewReader(`{"value":"secret-key"}`))
	rr := httptest.NewRecorder()

	handleEncryptDaemonSecret(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleEncryptDaemonSecret() status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp daemonSecretEncryptResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(resp.EncryptedValue, "enc:v1:") {
		t.Fatalf("expected encrypted blob, got %q", resp.EncryptedValue)
	}
	decrypted, err := sopbackend.DecryptSecretValue(resp.EncryptedValue)
	if err != nil {
		t.Fatalf("DecryptSecretValue() error = %v", err)
	}
	if decrypted != "secret-key" {
		t.Fatalf("decrypted value = %q, want %q", decrypted, "secret-key")
	}
}

func TestHandleAskDecryptsEncryptedProviderAPIKey(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	encrypted, err := sopbackend.EncryptSecretValue("secret-key")
	if err != nil {
		t.Fatalf("EncryptSecretValue() error = %v", err)
	}
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(fmt.Sprintf(`{"query":"hello","llm_provider":"gemini","llm_api_key":%q,"llm_model":"gemini-2.5-pro","llm_url":"https://example.test"}`, encrypted)))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	value, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := value.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", value)
	}
	if providerDetails.APIKey != "secret-key" {
		t.Fatalf("api key = %q, want %q", providerDetails.APIKey, "secret-key")
	}
}

func TestHandleUIHandoffRejectsInvalidPoolCredentialSecret(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","hq":{"base_url":"https://hq.example","user_id":"user-1","session_id":"handoff-invalid-pool"},"llm_credentials":{"openai":{"api_key":"enc:v1:not-valid"}}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "invalid llm_credentials[openai].api_key") {
		t.Fatalf("expected invalid pool api key error, got %q", rr.Body.String())
	}
}

func TestHandleAskUsesProviderPoolWhenLegacyFieldsMissing(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	encAnthropic, err := sopbackend.EncryptSecretValue("anthropic-secret")
	if err != nil {
		t.Fatalf("EncryptSecretValue() error = %v", err)
	}
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(fmt.Sprintf(`{"query":"hello","llm_credentials":{"anthropic":{"api_key":%q,"model":"claude-3-5-sonnet-20241022","url":"https://api.anthropic.com"}}}`, encAnthropic)))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	value, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := value.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", value)
	}
	if providerDetails.Provider != "anthropic" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "anthropic")
	}
	if providerDetails.APIKey != "anthropic-secret" {
		t.Fatalf("api key = %q, want %q", providerDetails.APIKey, "anthropic-secret")
	}
	if providerDetails.Model != "claude-3-5-sonnet-20241022" {
		t.Fatalf("model = %q, want %q", providerDetails.Model, "claude-3-5-sonnet-20241022")
	}
	if providerDetails.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("base URL = %q, want %q", providerDetails.BaseURL, "https://api.anthropic.com")
	}
}

func TestHandleAskLegacyFieldsTakePrecedenceOverProviderPool(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	encPoolKey, err := sopbackend.EncryptSecretValue("pool-openai-secret")
	if err != nil {
		t.Fatalf("EncryptSecretValue() error = %v", err)
	}
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(fmt.Sprintf(`{"query":"hello","llm_provider":"gemini","llm_api_key":"legacy-secret","llm_model":"gemini-2.5-pro","llm_credentials":{"openai":{"api_key":%q,"model":"gpt-5.4"}}}`, encPoolKey)))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
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
	if providerDetails.APIKey != "legacy-secret" {
		t.Fatalf("api key = %q, want %q", providerDetails.APIKey, "legacy-secret")
	}
	if providerDetails.Model != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want %q", providerDetails.Model, "gemini-2.5-pro")
	}
}

func TestHandleAskRejectsInvalidPoolCredentialSecret(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_credentials":{"gemini":{"api_key":"enc:v1:not-valid"}}}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "invalid llm_credentials[gemini].api_key") {
		t.Fatalf("expected invalid pool api key error, got %q", rr.Body.String())
	}
}

func TestHandleAskConfiguresMultiFallbackGeneratorFromCredentialPool(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_credentials":{"openai":{"api_key":"openai-key","model":"gpt-5.4"},"anthropic":{"api_key":"anthropic-key","model":"claude-3-5-sonnet-20241022"}}}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}
	value, ok := agent.calls[0].cfg.Get("generator")
	if !ok {
		t.Fatal("expected generator in agent config")
	}
	if _, ok := value.(*generator.FallbackGenerator); !ok {
		t.Fatalf("expected fallback generator, got %T", value)
	}

	pdValue, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := pdValue.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", pdValue)
	}
	if providerDetails.Provider != "openai" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "openai")
	}
}

func TestHandleAskGeneratorChainSkipsInvalidPrimaryAndUsesPoolFallback(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_provider":"unsupported","llm_api_key":"bad-key","llm_credentials":{"anthropic":{"api_key":"anthropic-key","model":"claude-3-5-sonnet-20241022"}}}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}

	pdValue, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := pdValue.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", pdValue)
	}
	if providerDetails.Provider != "anthropic" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "anthropic")
	}
	if providerDetails.APIKey != "anthropic-key" {
		t.Fatalf("api key = %q, want %q", providerDetails.APIKey, "anthropic-key")
	}
}

func TestRecordProviderHistoryTracksSuccessAndFailure(t *testing.T) {
	base := map[string]llmCredential{
		"anthropic": {APIKey: "anthropic-secret", Model: "claude-3-5-sonnet-20241022", SuccessCount: 1, FailureCount: 0, LastSuccessAtMS: 1000},
	}
	updated := recordLLMProviderOutcome(base, "anthropic", true, 2000)
	if updated["anthropic"].SuccessCount != 2 {
		t.Fatalf("success_count = %d, want %d", updated["anthropic"].SuccessCount, 2)
	}
	if updated["anthropic"].LastSuccessAtMS != 2000 {
		t.Fatalf("last_success_at_ms = %d, want %d", updated["anthropic"].LastSuccessAtMS, 2000)
	}
	updated = recordLLMProviderOutcome(updated, "anthropic", false, 3000)
	if updated["anthropic"].FailureCount != 1 {
		t.Fatalf("failure_count = %d, want %d", updated["anthropic"].FailureCount, 1)
	}
	if updated["anthropic"].LastFailureAtMS != 3000 {
		t.Fatalf("last_failure_at_ms = %d, want %d", updated["anthropic"].LastFailureAtMS, 3000)
	}
}

func TestBuildRuntimeProviderChainPrefersProvenModelOverCheaperUnprovenModel(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	catalog := `{"llm":[{"label":"Test","options":[{"value":"test:cheap-low","label":"Cheap Low","capability_tier":"cheap","reasoning_strength":2,"max_output_tokens":1024,"max_tool_turns":3,"compact_prompt":true,"supported_task_types":["implementation"],"input_cost_per_1m_tokens":0.1,"output_cost_per_1m_tokens":0.2},{"value":"test:expensive-high","label":"Expensive High","capability_tier":"strong","reasoning_strength":5,"max_output_tokens":4096,"max_tool_turns":8,"compact_prompt":false,"supported_task_types":["implementation"],"input_cost_per_1m_tokens":3.0,"output_cost_per_1m_tokens":12.0}]}]}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	chain := buildRuntimeProviderChain("test:cheap-low", "primary-key", "cheap-low", "https://primary", map[string]llmCredential{
		"test": {APIKey: "pool-key", Model: "expensive-high", URL: "https://expensive", SuccessCount: 7, FailureCount: 1, LastSuccessAtMS: 2000},
	}, "medium")
	if len(chain) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(chain), chain)
	}
	if chain[0].Model != "cheap-low" {
		t.Fatalf("expected explicit primary model to stay first, got %+v", chain)
	}
	if chain[1].Model != "expensive-high" {
		t.Fatalf("expected proven model to outrank the cheaper but unproven model, got %+v", chain)
	}
}

func TestBuildRuntimeProviderChainRanksCheapestEligiblePoolModelByThinkingLevel(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "model_catalog.json")
	catalog := `{"llm":[{"label":"Test","options":[{"value":"test:cheap-low","label":"Cheap Low","capability_tier":"cheap","reasoning_strength":2,"max_output_tokens":1024,"max_tool_turns":3,"compact_prompt":true,"supported_task_types":["implementation"],"input_cost_per_1m_tokens":0.1,"output_cost_per_1m_tokens":0.2},{"value":"test:expensive-high","label":"Expensive High","capability_tier":"strong","reasoning_strength":5,"max_output_tokens":4096,"max_tool_turns":8,"compact_prompt":false,"supported_task_types":["implementation"],"input_cost_per_1m_tokens":3.0,"output_cost_per_1m_tokens":12.0}]}]}`
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	chain := buildRuntimeProviderChain("test:expensive-high", "primary-key", "expensive-high", "https://primary", map[string]llmCredential{
		"test": {APIKey: "pool-key", Model: "cheap-low", URL: "https://cheap"},
	}, "medium")
	if len(chain) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(chain), chain)
	}
	if chain[0].Model != "expensive-high" {
		t.Fatalf("expected explicit primary model to stay first, got %+v", chain)
	}
	if chain[1].Model != "cheap-low" {
		t.Fatalf("expected cheaper eligible model to rank next, got %+v", chain)
	}
}

func TestHandleAskLegacyPrimaryStaysFirstWhenPoolPresent(t *testing.T) {
	agent := &recordingAskAgent{}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"query":"hello","llm_provider":"gemini","llm_api_key":"legacy-gemini-key","llm_model":"gemini-2.5-pro","llm_credentials":{"openai":{"api_key":"openai-key","model":"gpt-5.4"},"anthropic":{"api_key":"anthropic-key","model":"claude-3-5-sonnet-20241022"}}}`))
	rr := httptest.NewRecorder()

	svc.handleAsk(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAsk() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("expected one agent call, got %d", len(agent.calls))
	}

	value, ok := agent.calls[0].cfg.Get("generator")
	if !ok {
		t.Fatal("expected generator in agent config")
	}
	if _, ok := value.(*generator.FallbackGenerator); !ok {
		t.Fatalf("expected fallback generator, got %T", value)
	}

	pdValue, ok := agent.calls[0].cfg.Get("provider_details")
	if !ok {
		t.Fatal("expected provider_details in agent config")
	}
	providerDetails, ok := pdValue.(*sharedagent.ProviderDetails)
	if !ok {
		t.Fatalf("expected provider_details of type *sharedagent.ProviderDetails, got %T", pdValue)
	}
	if providerDetails.Provider != "gemini" {
		t.Fatalf("provider = %q, want %q", providerDetails.Provider, "gemini")
	}
	if providerDetails.APIKey != "legacy-gemini-key" {
		t.Fatalf("api key = %q, want %q", providerDetails.APIKey, "legacy-gemini-key")
	}
}

func TestAskRequestDecodesLLMCredentials(t *testing.T) {
	var req AskRequest
	body := `{"query":"hello","llm_credentials":{"gemini":{"api_key":"enc:v1:not-valid","model":"gemini-2.5-pro"}}}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal ask request: %v", err)
	}
	if len(req.LLMCredentials) != 1 {
		t.Fatalf("expected one credential entry, got %#v", req.LLMCredentials)
	}
	cred, ok := req.LLMCredentials["gemini"]
	if !ok {
		t.Fatalf("expected gemini credential, got %#v", req.LLMCredentials)
	}
	if cred.APIKey != "enc:v1:not-valid" || cred.Model != "gemini-2.5-pro" {
		t.Fatalf("unexpected credential decode: %#v", cred)
	}
}

func TestNormalizeAndValidateLLMCredentialsRejectsMalformedEncryptedKey(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	_, err := normalizeAndValidateLLMCredentials(map[string]llmCredential{
		"gemini": {APIKey: "enc:v1:not-valid"},
	})
	if err == nil {
		t.Fatal("expected malformed encrypted key error")
	}
}

func TestMergeLLMPoolStatePrefersIncomingNewerVersion(t *testing.T) {
	existing := daemonSessionContext{
		LLMCredentials: map[string]llmCredential{
			"openai": {APIKey: "old-openai", UpdatedAtMS: 100},
		},
		LLMPoolVersion:     2,
		LLMPoolUpdatedAtMS: 100,
		LLMPoolUpdatedBy:   "daemon",
	}
	incoming := map[string]llmCredential{
		"openai": {APIKey: "new-openai", UpdatedAtMS: 200},
		"gemini": {APIKey: "gemini-key", UpdatedAtMS: 210},
	}

	merged, version, updatedAtMS, updatedBy, direction := mergeLLMPoolState(existing, incoming, 5, 250, "client-1")

	if direction != "client_to_daemon" {
		t.Fatalf("direction = %q, want client_to_daemon", direction)
	}
	if got := merged["openai"].APIKey; got != "new-openai" {
		t.Fatalf("merged openai api key = %q, want %q", got, "new-openai")
	}
	if got := merged["gemini"].APIKey; got != "gemini-key" {
		t.Fatalf("merged gemini api key = %q, want %q", got, "gemini-key")
	}
	if version <= 5 {
		t.Fatalf("version = %d, want > 5", version)
	}
	if updatedAtMS <= 0 {
		t.Fatalf("updatedAtMS = %d, want > 0", updatedAtMS)
	}
	if updatedBy != "daemon" {
		t.Fatalf("updatedBy = %q, want daemon", updatedBy)
	}
}

func TestMergeLLMPoolStateDaemonNewerVersionWinsDirection(t *testing.T) {
	existing := daemonSessionContext{
		LLMCredentials: map[string]llmCredential{
			"anthropic": {APIKey: "daemon-anthropic", UpdatedAtMS: 300},
		},
		LLMPoolVersion:     8,
		LLMPoolUpdatedAtMS: 400,
		LLMPoolUpdatedBy:   "daemon",
	}
	incoming := map[string]llmCredential{
		"anthropic": {APIKey: "stale-anthropic", UpdatedAtMS: 200},
	}

	merged, version, _, _, direction := mergeLLMPoolState(existing, incoming, 3, 210, "client-1")

	if direction != "daemon_to_client" {
		t.Fatalf("direction = %q, want daemon_to_client", direction)
	}
	if got := merged["anthropic"].APIKey; got != "daemon-anthropic" {
		t.Fatalf("merged anthropic api key = %q, want %q", got, "daemon-anthropic")
	}
	if version != 8 {
		t.Fatalf("version = %d, want %d", version, 8)
	}
}

func TestHandleUIHandoffReturnsCanonicalPoolSyncSnapshot(t *testing.T) {
	ctxKey := "handoff-sync-snapshot"
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	daemonSessionStore.mu.Lock()
	delete(daemonSessionStore.data, ctxKey)
	daemonSessionStore.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","hq":{"base_url":"https://hq.example","user_id":"user-sync","session_id":"handoff-sync-snapshot"},"llm_pool_version":1,"llm_pool_updated_at_ms":1234,"llm_pool_updated_by":"client-1","llm_credentials":{"openai":{"api_key":"openai-key","model":"gpt-5.4","updated_at_ms":1234}}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.LLMCredentials["openai"].APIKey != "openai-key" {
		t.Fatalf("expected canonical openai key in response, got %#v", resp.LLMCredentials)
	}
	if resp.LLMPoolVersion <= 1 {
		t.Fatalf("expected daemon-bumped pool version, got %d", resp.LLMPoolVersion)
	}
	if resp.LLMPoolSyncDirection != "client_to_daemon" {
		t.Fatalf("sync direction = %q, want %q", resp.LLMPoolSyncDirection, "client_to_daemon")
	}
}

func TestPersistDaemonLLMPoolStateWritesEncryptedDiskSnapshot(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")

	oldPath := daemonLLMPoolStatePath
	defer func() { daemonLLMPoolStatePath = oldPath }()
	daemonLLMPoolStatePath = filepath.Join(t.TempDir(), "state", "daemon_llm_pool_state.json")

	daemonSessionStore.mu.Lock()
	oldStore := daemonSessionStore.data
	daemonSessionStore.data = map[string]daemonSessionContext{
		"session-encrypted": {
			HQBaseURL:   "https://hq.example",
			LLMProvider: "openai",
			LLMAPIKey:   "legacy-secret",
			LLMModel:    "gpt-5.4",
			LLMCredentials: map[string]llmCredential{
				"openai": {APIKey: "openai-secret", Model: "gpt-5.4", UpdatedAtMS: 1234},
			},
			LLMPoolVersion:     3,
			LLMPoolUpdatedAtMS: 1234,
			LLMPoolUpdatedBy:   "daemon",
			LastUpdated:        time.UnixMilli(1234).UTC(),
		},
	}
	daemonSessionStore.mu.Unlock()
	defer func() {
		daemonSessionStore.mu.Lock()
		daemonSessionStore.data = oldStore
		daemonSessionStore.mu.Unlock()
	}()

	if err := persistDaemonLLMPoolState(); err != nil {
		t.Fatalf("persistDaemonLLMPoolState() error = %v", err)
	}

	raw, err := os.ReadFile(daemonLLMPoolStatePath)
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}
	if bytes.Contains(raw, []byte("openai-secret")) || bytes.Contains(raw, []byte("legacy-secret")) {
		t.Fatalf("expected no plaintext secrets in persisted file, got %q", string(raw))
	}

	var envelope persistedDaemonLLMEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode persisted envelope: %v", err)
	}
	if !sopbackend.IsEncryptedSecretValue(envelope.EncryptedPayload) {
		t.Fatalf("expected encrypted payload envelope, got %#v", envelope)
	}
}

func TestLoadDaemonLLMPoolStateSurvivesRestartAndRejectsOlderClientPool(t *testing.T) {
	t.Setenv("SOP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")

	oldPath := daemonLLMPoolStatePath
	defer func() { daemonLLMPoolStatePath = oldPath }()
	daemonLLMPoolStatePath = filepath.Join(t.TempDir(), "state", "daemon_llm_pool_state.json")

	ctxKey := "restart-pool-session"
	daemonSessionStore.mu.Lock()
	oldStore := daemonSessionStore.data
	daemonSessionStore.data = map[string]daemonSessionContext{
		ctxKey: {
			HQBaseURL:   "https://hq.example",
			HQSessionID: ctxKey,
			LLMProvider: "openai",
			LLMModel:    "gpt-5.4",
			LLMCredentials: map[string]llmCredential{
				"openai": {APIKey: "daemon-newer-openai", Model: "gpt-5.4", UpdatedAtMS: 2000},
			},
			LLMPoolVersion:     9,
			LLMPoolUpdatedAtMS: 2000,
			LLMPoolUpdatedBy:   "daemon",
			LastUpdated:        time.UnixMilli(2000).UTC(),
		},
	}
	daemonSessionStore.mu.Unlock()
	defer func() {
		daemonSessionStore.mu.Lock()
		daemonSessionStore.data = oldStore
		daemonSessionStore.mu.Unlock()
	}()

	if err := persistDaemonLLMPoolState(); err != nil {
		t.Fatalf("persistDaemonLLMPoolState() error = %v", err)
	}

	// Simulate daemon restart by clearing in-memory state before loading persisted state.
	daemonSessionStore.mu.Lock()
	daemonSessionStore.data = map[string]daemonSessionContext{}
	daemonSessionStore.mu.Unlock()

	if err := loadDaemonLLMPoolState(); err != nil {
		t.Fatalf("loadDaemonLLMPoolState() error = %v", err)
	}

	daemonSessionStore.mu.Lock()
	loaded := daemonSessionStore.data[ctxKey]
	daemonSessionStore.mu.Unlock()
	if loaded.LLMPoolVersion != 9 {
		t.Fatalf("loaded pool version = %d, want %d", loaded.LLMPoolVersion, 9)
	}
	if got := loaded.LLMCredentials["openai"].APIKey; got != "daemon-newer-openai" {
		t.Fatalf("loaded openai api key = %q, want %q", got, "daemon-newer-openai")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","hq":{"base_url":"https://hq.example","user_id":"user-restart","session_id":"restart-pool-session"},"llm_pool_version":1,"llm_pool_updated_at_ms":1000,"llm_pool_updated_by":"client-1","llm_credentials":{"openai":{"api_key":"client-stale-openai","model":"gpt-5.4","updated_at_ms":1000}}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.LLMPoolSyncDirection != "daemon_to_client" {
		t.Fatalf("sync direction = %q, want %q", resp.LLMPoolSyncDirection, "daemon_to_client")
	}
	if got := resp.LLMCredentials["openai"].APIKey; got != "daemon-newer-openai" {
		t.Fatalf("canonical openai api key = %q, want %q", got, "daemon-newer-openai")
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

func TestExecuteHandlerPassesStdinToCommand(t *testing.T) {
	reqBody := `{"command":"cat","stdin":"hello from stdin\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(reqBody))
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
	if !strings.Contains(resp.Stdout, "hello from stdin") {
		t.Fatalf("stdout = %q, want it to contain echoed stdin", resp.Stdout)
	}
}

func TestExecuteHandlerHandlesCancellation(t *testing.T) {
	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start a long-running command
	reqBody := `{"command":"sleep 10 && echo done"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(reqBody))
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	// Cancel the context after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	executeHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executeHandler() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp.Cancelled {
		t.Fatalf("expected command to be marked as cancelled, got cancelled=%v", resp.Cancelled)
	}

	if resp.ExitCode != -1 {
		t.Fatalf("expected exit code -1 for cancelled command, got %d", resp.ExitCode)
	}

	if !strings.Contains(resp.Error, "cancelled") {
		t.Fatalf("expected error to mention cancellation, got %q", resp.Error)
	}
}

func TestExecuteHandlerShowsPartialOutputOnError(t *testing.T) {
	// Command that produces output before failing
	reqBody := `{"command":"echo 'partial output' && exit 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	executeHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executeHandler() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", resp.ExitCode)
	}

	// Verify partial output is captured
	if !strings.Contains(resp.Stdout, "partial output") {
		t.Fatalf("expected stdout to contain partial output, got %q", resp.Stdout)
	}

	// Should not be marked as cancelled
	if resp.Cancelled {
		t.Fatalf("command failed naturally, should not be marked as cancelled")
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

func TestPTYShellSessionManagerGetsCanonicalWorkingDirectory(t *testing.T) {
	mgr := newPTYShellSessionManager()
	const sessionID = "canonical-cwd-session"

	if _, _, err := mgr.Run(sessionID, "cd /tmp", 10*time.Second); err != nil {
		t.Fatalf("cd /tmp in PTY session: %v", err)
	}

	cwd, err := mgr.GetWorkingDirectory(sessionID)
	if err != nil {
		t.Fatalf("GetWorkingDirectory(): %v", err)
	}
	if !strings.Contains(cwd, "/tmp") {
		t.Fatalf("expected PTY session CWD to resolve to /tmp, got %q", cwd)
	}
}

func TestRunLocalCommandExecutesRunnableScriptFromWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "hello.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello-from-script\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "hello.sh",
		Reasoning:      "run a local script",
		WorkingDir:     tmpDir,
		TimeoutSeconds: 10,
		SessionID:      "",
	})
	if err != nil {
		t.Fatalf("runLocalCommandWithWorkingDir() error = %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}
	if !strings.Contains(resp.Stdout, "hello-from-script") {
		t.Fatalf("stdout = %q, want runnable script output", resp.Stdout)
	}
}

func TestRunLocalCommandWithStdoutStreaming(t *testing.T) {
	var streamedLines []string
	var mu sync.Mutex

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo line1 && echo line2 && echo line3",
		Reasoning:      "test stdout streaming",
		TimeoutSeconds: 10,
		StreamStdout: func(line string) {
			mu.Lock()
			streamedLines = append(streamedLines, line)
			mu.Unlock()
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}

	// Verify streaming occurred
	mu.Lock()
	defer mu.Unlock()
	if len(streamedLines) == 0 {
		t.Fatalf("expected streaming to occur, but got 0 chunks")
	}

	// Verify all content was streamed
	allStreamed := strings.Join(streamedLines, "")
	if !strings.Contains(allStreamed, "line1") || !strings.Contains(allStreamed, "line2") || !strings.Contains(allStreamed, "line3") {
		t.Fatalf("streamed content = %q, missing expected lines", allStreamed)
	}

	// Verify final output contains all lines
	if !strings.Contains(resp.Stdout, "line1") || !strings.Contains(resp.Stdout, "line2") || !strings.Contains(resp.Stdout, "line3") {
		t.Fatalf("stdout = %q, missing expected lines", resp.Stdout)
	}
}

func TestRunLocalCommandWithStderrStreaming(t *testing.T) {
	var stderrLines []string
	var mu sync.Mutex

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo error1 >&2 && echo error2 >&2",
		Reasoning:      "test stderr streaming",
		TimeoutSeconds: 10,
		StreamStderr: func(line string) {
			mu.Lock()
			stderrLines = append(stderrLines, line)
			mu.Unlock()
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify stderr was captured
	mu.Lock()
	defer mu.Unlock()
	if len(stderrLines) == 0 {
		t.Fatalf("expected stderr streaming to occur, but got 0 chunks")
	}

	// Verify all stderr content was streamed
	allStreamed := strings.Join(stderrLines, "")
	if !strings.Contains(allStreamed, "error1") || !strings.Contains(allStreamed, "error2") {
		t.Fatalf("streamed stderr = %q, missing expected errors", allStreamed)
	}

	if !strings.Contains(resp.Stderr, "error1") || !strings.Contains(resp.Stderr, "error2") {
		t.Fatalf("stderr = %q, missing expected errors", resp.Stderr)
	}
}

func TestRunLocalCommandWithStdinInput(t *testing.T) {
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "cat",
		Reasoning:      "test stdin support",
		Stdin:          "hello from stdin\n",
		TimeoutSeconds: 5,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}

	if !strings.Contains(resp.Stdout, "hello from stdin") {
		t.Fatalf("stdout = %q, expected stdin to be echoed", resp.Stdout)
	}
}

func TestRunLocalCommandWithOnCompleteCallback(t *testing.T) {
	var completedStdout, completedStderr string
	var completedExitCode int

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:        "echo success && echo warning >&2",
		Reasoning:      "test completion callback",
		TimeoutSeconds: 10,
		OnComplete: func(stdout, stderr string, exitCode int) {
			completedStdout = stdout
			completedStderr = stderr
			completedExitCode = exitCode
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify callback was invoked
	if !strings.Contains(completedStdout, "success") {
		t.Fatalf("OnComplete stdout = %q, expected success message", completedStdout)
	}

	if !strings.Contains(completedStderr, "warning") {
		t.Fatalf("OnComplete stderr = %q, expected warning message", completedStderr)
	}

	if completedExitCode != 0 {
		t.Fatalf("OnComplete exitCode = %d, expected 0", completedExitCode)
	}
}

// Phase 2 Tests: Process Control
func TestRunLocalCommandWithProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Process groups not supported on Windows")
	}

	var capturedPID int

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:      "echo test",
		ProcessGroup: true,
		OnProcessStart: func(pid int) {
			capturedPID = pid
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if capturedPID == 0 {
		t.Fatalf("OnProcessStart not called or PID not captured")
	}

	if resp.PID != capturedPID {
		t.Fatalf("response PID = %d, callback PID = %d", resp.PID, capturedPID)
	}
}

func TestRunLocalCommandWithPIDCapture(t *testing.T) {
	var capturedPID int

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command: "echo test",
		OnProcessStart: func(pid int) {
			capturedPID = pid
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if capturedPID == 0 {
		t.Fatalf("OnProcessStart not called")
	}

	if resp.PID != capturedPID {
		t.Fatalf("response PID = %d, callback PID = %d", resp.PID, capturedPID)
	}
}

// Phase 3 Tests: Background Jobs
func TestRunLocalCommandInBackground(t *testing.T) {
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "sleep 1 && echo background-test",
		Background: true,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp.Background {
		t.Fatalf("Background flag not set in response")
	}

	if resp.JobID == "" {
		t.Fatalf("JobID not returned")
	}

	// Wait a bit for command to complete
	time.Sleep(2 * time.Second)

	// Check job status
	job, ok := getBackgroundJob(resp.JobID)
	if !ok {
		t.Fatalf("Job not found: %s", resp.JobID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if !job.done {
		t.Fatalf("Job should be done by now")
	}

	if job.exitCode == nil || *job.exitCode != 0 {
		t.Fatalf("Job exit code: %v, expected 0", job.exitCode)
	}

	if !strings.Contains(job.stdout.String(), "background-test") {
		t.Fatalf("Job stdout = %q, expected 'background-test'", job.stdout.String())
	}
}

func TestBackgroundJobWithCustomID(t *testing.T) {
	customID := "my-custom-job-123"

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "echo custom-job",
		Background: true,
		JobID:      customID,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.JobID != customID {
		t.Fatalf("JobID = %s, expected %s", resp.JobID, customID)
	}

	// Wait for completion
	time.Sleep(500 * time.Millisecond)

	job, ok := getBackgroundJob(customID)
	if !ok {
		t.Fatalf("Job not found: %s", customID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if !strings.Contains(job.stdout.String(), "custom-job") {
		t.Fatalf("Job output = %q, expected 'custom-job'", job.stdout.String())
	}
}

func TestKillBackgroundJob(t *testing.T) {
	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "sleep 30",
		Background: true,
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Wait a bit to ensure job started
	time.Sleep(200 * time.Millisecond)

	// Kill the job
	if err := killBackgroundJob(resp.JobID); err != nil {
		t.Fatalf("failed to kill job: %v", err)
	}

	// Wait for termination
	time.Sleep(500 * time.Millisecond)

	job, ok := getBackgroundJob(resp.JobID)
	if !ok {
		t.Fatalf("Job not found: %s", resp.JobID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if !job.done {
		t.Fatalf("Job should be terminated")
	}

	// Exit code should be non-zero (killed)
	if job.exitCode != nil && *job.exitCode == 0 {
		t.Fatalf("Job exit code = 0, expected non-zero (killed)")
	}
}

func TestBackgroundJobWithStreaming(t *testing.T) {
	var stdoutLines []string
	var mu sync.Mutex

	result, err := runLocalCommandWithWorkingDir(context.Background(), sharedagent.LocalCommandRequest{
		Command:    "echo line1 && echo line2 && echo line3",
		Background: true,
		StreamStdout: func(line string) {
			mu.Lock()
			stdoutLines = append(stdoutLines, line)
			mu.Unlock()
		},
	})

	if err != nil {
		t.Fatalf("command error: %v", err)
	}

	var resp ExecuteResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Wait for completion
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(stdoutLines) == 0 {
		t.Fatalf("Expected streaming to occur, but got 0 chunks")
	}

	// Verify all content was streamed
	allStreamed := strings.Join(stdoutLines, "")
	if !strings.Contains(allStreamed, "line1") || !strings.Contains(allStreamed, "line2") || !strings.Contains(allStreamed, "line3") {
		t.Fatalf("streamed content = %q, missing expected lines", allStreamed)
	}
}

func TestRetryPendingVectorizationRecoveryAttemptsPersistedDegradedTargets(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-9", os.ErrNotExist)
	}

	resetDefaultStorageHealthPolicyForTest(t)

	previousPolicy := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	defer func() { activeRuntimeVectorizationFailurePolicy = previousPolicy }()

	previousHandler := vectorizationRecoveryExecutor
	called := false
	vectorizationRecoveryExecutor = func(ctx context.Context, kbName, targetType, targetID string) error {
		called = true
		if kbName != "kb" || targetType != "category" || targetID != "cat-9" {
			t.Fatalf("unexpected recovery target: kb=%q targetType=%q targetID=%q", kbName, targetType, targetID)
		}
		return nil
	}
	defer func() { vectorizationRecoveryExecutor = previousHandler }()

	count := retryPendingVectorizationRecovery(context.Background(), true)
	if count != 1 {
		t.Fatalf("expected one pending recovery to be retried, got %d", count)
	}
	if !called {
		t.Fatal("expected pending recovery to invoke the recovery executor")
	}

	state := activeRuntimeVectorizationFailurePolicy.RecoveryState()
	if state.Status != "recovered" {
		t.Fatalf("expected recovered status after auto-retry, got %q", state.Status)
	}
}

func TestHandleUIHandoffIncludesVectorizationRecoveryState(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-6", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"hello","hq":{"base_url":"http://example.test"}}`))
	rr := httptest.NewRecorder()
	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode handoff response: %v", err)
	}
	if resp.Recovery.TargetID != "cat-6" {
		t.Fatalf("expected handoff recovery target id to be cat-6, got %q", resp.Recovery.TargetID)
	}
	if resp.Recovery.Prompt == "" {
		t.Fatalf("expected handoff recovery prompt to be present")
	}
}

func TestHandleUIHandoffIncludesRecoveryPromptInWelcomeContext(t *testing.T) {
	tmpDir := t.TempDir()
	policy := newRuntimeVectorizationFailurePolicy(tmpDir)
	for i := 0; i < 3; i++ {
		policy.HandleFailure("kb", "category", "cat-7", os.ErrNotExist)
	}

	previous := activeRuntimeVectorizationFailurePolicy
	activeRuntimeVectorizationFailurePolicy = policy.(runtimeVectorizationFailurePolicyHandle)
	t.Cleanup(func() {
		activeRuntimeVectorizationFailurePolicy = previous
	})

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"hello","hq":{"base_url":"http://example.test"}}`))
	rr := httptest.NewRecorder()
	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp UIHandoffResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode handoff response: %v", err)
	}
	if !strings.Contains(resp.WelcomeContext, "Vectorization") {
		t.Fatalf("expected welcome context to mention vectorization recovery, got %q", resp.WelcomeContext)
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

func TestHandleUIHandoffStoresExplicitHQPortAndNormalizesBaseURL(t *testing.T) {
	ctxKey := "handoff-port"
	daemonSessionStore.mu.Lock()
	delete(daemonSessionStore.data, ctxKey)
	daemonSessionStore.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/handoff", strings.NewReader(`{"query":"ping","auth":{"access_token":"token-123","refresh_token":"refresh-123"},"hq":{"base_url":"https://hq.example","port":"9443","user_id":"user-1","session_id":"handoff-port"}}`))
	rr := httptest.NewRecorder()

	handleUIHandoff(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleUIHandoff() status = %d, want %d", rr.Code, http.StatusOK)
	}

	daemonSessionStore.mu.Lock()
	ctx, ok := daemonSessionStore.data[ctxKey]
	daemonSessionStore.mu.Unlock()
	if !ok {
		t.Fatalf("expected handoff context to be stored for %q", ctxKey)
	}
	if ctx.HQBaseURL != "https://hq.example:9443" {
		t.Fatalf("expected normalized HQ base URL to include explicit port, got %q", ctx.HQBaseURL)
	}
	if ctx.HQPort != "9443" {
		t.Fatalf("expected stored HQ port to be preserved, got %q", ctx.HQPort)
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
	guidance sharedagent.ContinueDelegationGuidance
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

func (a *recordingAskAgent) BuildContinueDelegationGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (sharedagent.ContinueDelegationGuidance, error) {
	_ = ctx
	_ = query
	_ = requestedPhase
	_ = variables
	return a.guidance, nil
}

func TestLocalRAGAskUsesShellFallbackForSimpleCommands(t *testing.T) {
	t.Skip("Bare command execution removed - commands must use slash prefixes like /c, /cmd")
	agent := &recordingAskAgent{response: "⚠️ **AI Copilot Disabled**: No valid API Key found."}
	svc := &localRAGService{service: agent, shellSessions: newPTYShellSessionManager()}

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

	if _, _, err := mgr.Run("shell-pty", "cd /tmp", 2*time.Second); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	out, stillRunning, err := mgr.Run("shell-pty", "pwd", 2*time.Second)
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

	out, stillRunning, err := mgr.Run("long-run", "sh -c 'printf start; sleep 1; printf end'", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if !stillRunning {
		t.Fatalf("expected first run to stay running beyond the timeout, got output %q", out)
	}

	status, stillRunning, err := mgr.Run("long-run", "status", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("status query failed: %v", err)
	}
	if !stillRunning {
		t.Fatalf("expected status query to report the process is still running, got %q", status)
	}
}

func TestPTYShellSessionManagerSpawnsBackgroundSessions(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	// Spawn a background session
	bgSessionID, err := mgr.SpawnBackgroundSession("")
	if err != nil {
		t.Fatalf("SpawnBackgroundSession() failed: %v", err)
	}

	if bgSessionID == "" {
		t.Fatal("expected non-empty background session ID")
	}

	// Verify it's tracked as background
	mgr.mu.Lock()
	bgSession := mgr.sessions[bgSessionID]
	mgr.mu.Unlock()

	if bgSession == nil {
		t.Fatal("background session not found in manager")
	}

	if !bgSession.isBackground {
		t.Error("expected session to be marked as background")
	}

	// Can run commands in background session
	output, _, err := mgr.Run(bgSessionID, "echo 'background'", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() in background session failed: %v", err)
	}

	if !strings.Contains(output, "background") {
		t.Errorf("expected output to contain 'background', got %q", output)
	}
}

func TestPTYShellSessionManagerCleansUpIdleSessions(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer mgr.StopCleanup()

	// Create a session and mark it as old
	sessionID := "test-session"
	_, _, err := mgr.Run(sessionID, "echo test", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// Force session to appear idle
	mgr.mu.Lock()
	if sess, ok := mgr.sessions[sessionID]; ok {
		sess.lastUsed = time.Now().Add(-1 * time.Hour) // 1 hour ago
	}
	mgr.mu.Unlock()

	// Cleanup with 30 minute timeout should remove the session
	cleaned := mgr.cleanupIdleSessions(30 * time.Minute)
	if cleaned != 1 {
		t.Errorf("expected 1 session cleaned, got %d", cleaned)
	}

	// Verify session was removed
	mgr.mu.Lock()
	_, exists := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	if exists {
		t.Error("expected session to be removed after cleanup")
	}
}

func TestPTYShellSessionHealthCheckDetectsStuckSession(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	sessionID := "test-health"

	// Create a session and run a valid command
	_, _, err := mgr.Run(sessionID, "echo test", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	mgr.mu.Lock()
	session := mgr.sessions[sessionID]
	mgr.mu.Unlock()
	if session == nil {
		t.Fatal("expected session to be tracked")
	}

	// Closing the session should leave it unhealthy for subsequent checks.
	session.Close()
	if session.IsHealthy() {
		t.Error("expected closed session to report unhealthy")
	}
}

func TestPTYShellSessionAutoRecoveryFromStuckSession(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	sessionID := "test-recovery"

	// Create a session
	_, _, err := mgr.Run(sessionID, "echo first", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// Get the session and mark it as stuck
	mgr.mu.Lock()
	session := mgr.sessions[sessionID]
	session.stillRunning = true
	mgr.mu.Unlock()

	// Close the session to simulate unresponsive state
	session.Close()

	// Next command should recover via the worker manager and keep the session tracked.
	output, _, err := mgr.Run(sessionID, "echo recovered", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() after recovery failed: %v", err)
	}

	if !strings.Contains(output, "recovered") {
		t.Errorf("expected recovered session to work, got output: %q", output)
	}

	mgr.mu.Lock()
	newSession := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	if newSession == nil {
		t.Error("expected session to remain tracked after recovery")
	}
}

func TestPTYShellSessionForceReset(t *testing.T) {
	mgr := newPTYShellSessionManager()
	defer func() {
		mgr.mu.Lock()
		for _, sess := range mgr.sessions {
			sess.Close()
		}
		mgr.mu.Unlock()
	}()

	sessionID := "test-force-reset"

	// Create a session
	_, _, err := mgr.Run(sessionID, "echo before", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	mgr.mu.Lock()
	oldSession := mgr.sessions[sessionID]
	mgr.mu.Unlock()

	// Force reset removes the tracked session and the next command recreates it.
	if err := mgr.ForceResetSession(sessionID); err != nil {
		t.Fatalf("ForceResetSession() failed: %v", err)
	}

	mgr.mu.Lock()
	_, exists := mgr.sessions[sessionID]
	mgr.mu.Unlock()
	if exists {
		t.Fatal("expected tracked session to be removed after reset")
	}

	output, _, err := mgr.Run(sessionID, "echo after", 5*time.Second)
	if err != nil {
		t.Fatalf("Run() after force reset failed: %v", err)
	}

	if !strings.Contains(output, "after") {
		t.Errorf("expected reset session to work, got: %q", output)
	}

	mgr.mu.Lock()
	newSession := mgr.sessions[sessionID]
	mgr.mu.Unlock()
	if newSession == nil {
		t.Fatal("expected session to be recreated on the next command")
	}
	if newSession == oldSession {
		t.Error("expected a fresh session object after reset")
	}
}

func TestLocalRAGAskPersistsShellSessionDirectoryAcrossRequests(t *testing.T) {
	t.Skip("Bare command execution removed - commands must use slash prefixes like /c, /cmd")
	svc := &localRAGService{service: &recordingAskAgent{}, shellSessions: newPTYShellSessionManager()}

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
