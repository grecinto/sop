package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

func resetContinueBridgeStateForTest() {
	continueSessionState = newContinueBridgeSessionStore()
	continueWorkerState = newContinueBridgeWorkerStore()
}

func TestFormatUserFacingAgentErrorRedactsToolFailureNoise(t *testing.T) {
	msg := formatUserFacingAgentError("copilot", fmt.Errorf("tool 'list_directory' not found in any registered agent for request 123e4567-e89b-12d3-a456-426614174000"))
	if !strings.Contains(msg, "could not find the requested tool") {
		t.Fatalf("expected concise tool-unavailable message, got %q", msg)
	}
	if strings.Contains(msg, "123e4567-e89b-12d3-a456-426614174000") {
		t.Fatalf("expected UUID-like noise to be removed, got %q", msg)
	}
}

func TestInitializeRequest_UsesAuthenticatedUserID(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/chat", strings.NewReader(`{"message":"hi","user_id":"client-random"}`))
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice"}))

	chatReq, err := initializeRequest(nil, req)
	if err != nil {
		t.Fatalf("initializeRequest failed: %v", err)
	}
	if got := chatReq.UserID; got != "alice" {
		t.Fatalf("expected authenticated user ID to override request payload, got %q", got)
	}
}

func TestConstructPayload_PreservesRawUserMessage(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	config = Config{Databases: []DatabaseConfig{{Name: "demo", Path: t.TempDir()}}}

	req := &aiChatRequest{
		Message:  "show me orders",
		Database: "demo",
	}

	ctx, payload, _, fullMessage := constructPayload(context.Background(), httptest.NewRecorder(), req, llmSettings{}, func(string, any) {}, nil)
	if ctx == nil || payload == nil {
		t.Fatal("expected constructPayload to return context and payload")
	}
	if got := strings.TrimSpace(fullMessage); got != req.Message {
		t.Fatalf("expected raw user message to stay untouched, got %q want %q", got, req.Message)
	}
}

func TestConstructPayload_PropagatesInputPartsIntoSessionVariables(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	config = Config{Databases: []DatabaseConfig{{Name: "demo", Path: t.TempDir()}}}

	req := &aiChatRequest{
		Message:    "inspect screenshot",
		Database:   "demo",
		InputParts: []ai.ContentPart{{Type: "image", MimeType: "image/png", Data: "abc123"}},
	}

	_, payload, _, _ := constructPayload(context.Background(), httptest.NewRecorder(), req, llmSettings{}, func(string, any) {}, nil)
	if payload == nil {
		t.Fatal("expected constructPayload to return payload")
	}
	if payload.Variables == nil {
		t.Fatal("expected session variables to be initialized")
	}
	gotParts, ok := payload.Variables["input_parts"].([]ai.ContentPart)
	if !ok || len(gotParts) != 1 || gotParts[0].Data != "abc123" {
		t.Fatalf("expected image input parts to be propagated, got %#v", payload.Variables["input_parts"])
	}
}

func TestBuildAIChatRequestFromContinueChatRequestAddsContext(t *testing.T) {
	payload := continueChatRequest{
		Message:   "fix the failing test",
		SessionID: "continue-session",
		Workspace: continueWorkspaceContext{Root: "/repo", ActiveFile: "src/app.go"},
		Context:   continueChatContext{SelectedText: "fmt.Println(\"hi\")", OpenFiles: []string{"src/app.go", "src/app_test.go"}},
	}

	chatReq := buildAIChatRequestFromContinueChatRequest(payload)
	if chatReq == nil {
		t.Fatal("expected chat request payload")
	}
	if chatReq.SessionID != "continue-session" {
		t.Fatalf("expected session ID to be preserved, got %q", chatReq.SessionID)
	}
	if !strings.Contains(chatReq.Message, "fix the failing test") {
		t.Fatalf("expected original message to be preserved, got %q", chatReq.Message)
	}
	if !strings.Contains(chatReq.Message, "src/app.go") {
		t.Fatalf("expected active file context to be included, got %q", chatReq.Message)
	}
	if !strings.Contains(chatReq.Message, "fmt.Println(\"hi\")") {
		t.Fatalf("expected selected text context to be included, got %q", chatReq.Message)
	}
}

func TestBuildAIChatRequestFromContinueChatRequestAddsToolContext(t *testing.T) {
	payload := continueChatRequest{
		Message: "inspect the parser error",
		Tools:   []continueToolHint{{Name: "search_code", Args: map[string]any{"query": "ParseError"}}},
	}

	chatReq := buildAIChatRequestFromContinueChatRequest(payload)
	if chatReq == nil {
		t.Fatal("expected chat request payload")
	}
	if !strings.Contains(chatReq.Message, "search_code") {
		t.Fatalf("expected tool intent to be included, got %q", chatReq.Message)
	}
	if !strings.Contains(chatReq.Message, "ParseError") {
		t.Fatalf("expected tool arguments to be included, got %q", chatReq.Message)
	}
}

func TestBuildAIChatRequestFromContinueChatRequestPreservesProviderMetadata(t *testing.T) {
	payload := continueChatRequest{
		Message:  "use the supplied credentials",
		Provider: "openai",
		APIKey:   "sk-test",
		URL:      "https://example.test/v1",
		Model:    "gpt-4.1",
	}

	chatReq := buildAIChatRequestFromContinueChatRequest(payload)
	if chatReq == nil {
		t.Fatal("expected chat request payload")
	}
	if chatReq.Provider != "openai" {
		t.Fatalf("expected provider metadata to be preserved, got %q", chatReq.Provider)
	}
}

func TestHandleContinueToolReadsFilesFromWorkspace(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("hello from bridge\nsecond line"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/repo.read_file", strings.NewReader(fmt.Sprintf(`{"path":"%s","start_line":1,"end_line":5}`, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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

func TestHandleContinueToolAppliesPatchToWorkspaceFile(t *testing.T) {
	resetContinueBridgeStateForTest()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":"after"}`, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleContinueTool() status = %d, want %d", rr.Code, http.StatusOK)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(data) != "after" {
		t.Fatalf("expected patch to overwrite file contents, got %q", string(data))
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

func TestHandleContinueToolDelegatesPatchToRegisteredWorkerForOpenFile(t *testing.T) {
	resetContinueBridgeStateForTest()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	continueSessionState.Put(continueSessionSnapshot{SessionID: "continue-patch-1", ActiveFile: "payments/service.go", OpenFiles: []string{"payments/service.go"}, WorkspaceRoot: dir, Status: "active"})

	var received continueAgentExecuteRequest
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"type\":\"done\",\"result\":{\"status\":\"completed\",\"summary\":\"Applied editor-native patch\"}}\n"))
	}))
	defer worker.Close()
	continueWorkerState.Put(continueWorkerRegistration{SessionID: "continue-patch-1", CallbackURL: worker.URL, Capabilities: []string{"apply_patch", "read_context"}, IDE: "vscode"})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-patch-1","workspace":%q,"path":%q,"selection":"after"}`, dir, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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
}

func TestHandleContinueToolAppliesUnifiedHunkPatchToLargeFile(t *testing.T) {
	resetContinueBridgeStateForTest()
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

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":%q}`, filePath, patch)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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

func TestHandleContinueToolFallsBackToLocalPatchWhenTargetFileIsNotOpen(t *testing.T) {
	resetContinueBridgeStateForTest()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	continueSessionState.Put(continueSessionSnapshot{SessionID: "continue-patch-2", ActiveFile: "payments/other.go", OpenFiles: []string{"payments/other.go"}, WorkspaceRoot: dir, Status: "active"})
	continueWorkerState.Put(continueWorkerRegistration{SessionID: "continue-patch-2", CallbackURL: "http://127.0.0.1:1", Capabilities: []string{"apply_patch", "read_context"}, IDE: "vscode"})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.apply_patch", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-patch-2","workspace":%q,"path":%q,"selection":"after"}`, dir, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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
}

func TestHandleContinueToolRunsCommandsForValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/test.run", strings.NewReader(`{"query":"echo bridge-test"}`))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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
	if got := result["output"]; !strings.Contains(got.(string), "bridge-test") {
		t.Fatalf("expected command output to include bridge-test, got %#v", got)
	}
}

func TestHandleContinueToolPreviewsPatchForWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.preview_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":"after"}`, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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

func TestHandleContinueToolPreviewsUnifiedHunkPatchForWorkspaceFile(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/code.preview_patch", strings.NewReader(fmt.Sprintf(`{"path":"%s","selection":%q}`, filePath, patch)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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

func TestHandleContinueToolAppliesSingleFindAndReplace(t *testing.T) {
	resetContinueBridgeStateForTest()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/single_find_and_replace", strings.NewReader(fmt.Sprintf(`{"path":"%s","find":"beta","replace":"beta patched"}`, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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

func TestHandleContinueToolDelegatesSingleFindAndReplaceToRegisteredWorkerForOpenFile(t *testing.T) {
	resetContinueBridgeStateForTest()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	continueSessionState.Put(continueSessionSnapshot{SessionID: "continue-edit-1", ActiveFile: "payments/service.go", OpenFiles: []string{"payments/service.go"}, WorkspaceRoot: dir, Status: "active"})

	var received continueAgentExecuteRequest
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"type\":\"done\",\"result\":{\"status\":\"completed\",\"summary\":\"Applied editor-native replacement\"}}\n"))
	}))
	defer worker.Close()
	continueWorkerState.Put(continueWorkerRegistration{SessionID: "continue-edit-1", CallbackURL: worker.URL, Capabilities: []string{"apply_patch", "read_context"}, IDE: "vscode"})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/single_find_and_replace", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-edit-1","workspace":%q,"path":%q,"find":"beta","replace":"beta patched"}`, dir, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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
	if received.ToolChoice != "single_find_and_replace" || received.Context.ActiveFile != "payments/service.go" {
		t.Fatalf("expected focused delegated edit request, got %#v", received)
	}
}

func TestHandleContinueToolAppliesMultiEdit(t *testing.T) {
	resetContinueBridgeStateForTest()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/multi_edit", strings.NewReader(fmt.Sprintf(`{"path":"%s","edits":[{"find":"alpha","replace":"alpha patched"},{"find":"gamma","replace":"gamma patched"}]}`, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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

func TestHandleContinueToolFallsBackToLocalMultiEditWhenTargetFileIsNotOpen(t *testing.T) {
	resetContinueBridgeStateForTest()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o755); err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	filePath := filepath.Join(dir, "payments", "service.go")
	if err := os.WriteFile(filePath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	continueSessionState.Put(continueSessionSnapshot{SessionID: "continue-edit-2", ActiveFile: "payments/other.go", OpenFiles: []string{"payments/other.go"}, WorkspaceRoot: dir, Status: "active"})
	continueWorkerState.Put(continueWorkerRegistration{SessionID: "continue-edit-2", CallbackURL: "http://127.0.0.1:1", Capabilities: []string{"apply_patch", "read_context"}, IDE: "vscode"})

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/multi_edit", strings.NewReader(fmt.Sprintf(`{"session_id":"continue-edit-2","workspace":%q,"path":%q,"edits":[{"find":"alpha","replace":"alpha patched"},{"find":"gamma","replace":"gamma patched"}]}`, dir, filePath)))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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
}

func TestHandleContinueToolDelegatesToAgentRuntime(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/agent.ask", strings.NewReader(`{"query":"reply with hello"}`))
	rr := httptest.NewRecorder()

	handleContinueTool(rr, req)

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
}

func TestContinueToolRouteIsRegisteredOnBackendMux(t *testing.T) {
	resetContinueBridgeStateForTest()
	oldConfig := config
	config = Config{DisableAuth: true}
	defer func() { config = oldConfig }()

	mux := http.NewServeMux()
	backend := &Backend{Config: &config, ModelCatalog: &ModelCatalog{}, LoadedAgents: loadedAgents, ActiveSessions: activeSessions}
	backend.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/continue/tools/repo.read_file", strings.NewReader(`{"path":"/tmp/does-not-matter.txt"}`))
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected router to reach tool handler, got status %d body %q", rr.Code, rr.Body.String())
	}
}

func TestContinueWorkerRegisterRouteIsRegisteredOnBackendMux(t *testing.T) {
	resetContinueBridgeStateForTest()
	oldConfig := config
	config = Config{DisableAuth: true}
	defer func() { config = oldConfig }()

	mux := http.NewServeMux()
	backend := &Backend{Config: &config, ModelCatalog: &ModelCatalog{}, LoadedAgents: loadedAgents, ActiveSessions: activeSessions}
	backend.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/continue/worker/register", strings.NewReader(`{"session_id":"worker-session-1","callback_url":"http://127.0.0.1:4321/callback","capabilities":["apply_patch","read_context"],"ide":"vscode"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected worker register route to succeed, got status %d body %q", rr.Code, rr.Body.String())
	}
}

func TestContinueSessionStateRouteReturnsActiveSessionInfo(t *testing.T) {
	oldConfig := config
	config = Config{DisableAuth: true}
	defer func() { config = oldConfig }()

	oldSessions := activeSessions
	activeSessions = NewSessionManager(10)
	defer func() { activeSessions = oldSessions }()

	activeSessions.Put("session-42", nil)

	mux := http.NewServeMux()
	backend := &Backend{Config: &config, ModelCatalog: &ModelCatalog{}, LoadedAgents: loadedAgents, ActiveSessions: activeSessions}
	backend.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/continue/sessions/session-42", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected session state route to succeed, got status %d body %q", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["session_id"] != "session-42" {
		t.Fatalf("expected session_id to be echoed, got %#v", payload["session_id"])
	}
	if payload["status"] != "active" {
		t.Fatalf("expected status active, got %#v", payload["status"])
	}
}

func TestSOPBridgeAliasRoutesAreRegisteredOnBackendMux(t *testing.T) {
	oldConfig := config
	config = Config{DisableAuth: true}
	defer func() { config = oldConfig }()

	mux := http.NewServeMux()
	backend := &Backend{Config: &config, ModelCatalog: &ModelCatalog{}, LoadedAgents: loadedAgents, ActiveSessions: activeSessions}
	backend.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/sop/tools/repo.read_file", strings.NewReader(`{"path":"/tmp/does-not-matter.txt"}`))
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatalf("expected SOP bridge alias route to reach tool handler, got status %d body %q", rr.Code, rr.Body.String())
	}
}

func TestBackendRegisterHandlers_ProtectsHQSyncRoute(t *testing.T) {
	oldConfig := config
	config = Config{}
	defer func() { config = oldConfig }()

	mux := http.NewServeMux()
	backend := &Backend{Config: &config, ModelCatalog: &ModelCatalog{}, LoadedAgents: loadedAgents, ActiveSessions: activeSessions}
	backend.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/hq-sync", strings.NewReader(`{"daemon_id":"daemon-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected /api/daemon/hq-sync to require auth on backend mux, got status %d body %q", rr.Code, rr.Body.String())
	}
}

func TestLoadAgentFindsCopilotConfigFromToolsWorkingDirectory(t *testing.T) {
	oldLoadedAgents := loadedAgents
	loadedAgents = make(map[string]ai.Agent[map[string]any])
	defer func() { loadedAgents = oldLoadedAgents }()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	toolsDir := filepath.Join(oldWd, "..")
	if err := os.Chdir(toolsDir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", toolsDir, err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	if err := loadAgent(context.Background(), "copilot", "ai/data/copilot_pipeline.json"); err != nil {
		t.Fatalf("loadAgent() error = %v", err)
	}
	if _, ok := loadedAgents["copilot"]; !ok {
		t.Fatal("expected copilot agent to be registered")
	}
}
