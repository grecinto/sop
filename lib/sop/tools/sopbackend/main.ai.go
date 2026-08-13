package sopbackend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	log "log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/embed"

	_ "github.com/sharedcode/sop/ai/generator"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/ai/obfuscation"
	"github.com/sharedcode/sop/btree"
	"github.com/sharedcode/sop/database"
)

var unifiedHunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

var (
	ltmPressureUIHooksMu sync.Mutex
	ltmPressureUIHooks   []func(string, any)
)

const continueWorkerRawBodyLimit = 64 * 1024

func SetLTMPressureChatEventHook() {
	agent.SetLTMPressureEventHook(func(notification agent.LTMPressureNotification) {
		message := strings.TrimSpace(notification.Message)
		if message == "" {
			return
		}
		if strings.EqualFold(strings.TrimSpace(notification.PressureState), "normal") {
			return
		}
		if len(notification.FreeUpTargets) > 0 {
			message = fmt.Sprintf("%s\n\nStorage cleanup paths:\n- %s\n- %s", message, notification.FreeUpTargets[0], notification.FreeUpTargets[1])
		}
		event := ai.BuildAssistantMessageEvent("commentary", fmt.Sprintf("⚠️ %s", message))
		ltmPressureUIHooksMu.Lock()
		copyHooks := append([]func(string, any){}, ltmPressureUIHooks...)
		ltmPressureUIHooksMu.Unlock()
		for _, hook := range copyHooks {
			if hook != nil {
				hook(ai.ReasoningEventAssistantMessage, event)
			}
		}
	})
}

func registerLTMPressureUIHook(hook func(string, any)) {
	if hook == nil {
		return
	}
	ltmPressureUIHooksMu.Lock()
	defer ltmPressureUIHooksMu.Unlock()
	for _, existing := range ltmPressureUIHooks {
		if reflect.ValueOf(existing).Pointer() == reflect.ValueOf(hook).Pointer() {
			return
		}
	}
	ltmPressureUIHooks = append(ltmPressureUIHooks, hook)
}

func unregisterLTMPressureUIHook(hook func(string, any)) {
	if hook == nil {
		return
	}
	ltmPressureUIHooksMu.Lock()
	defer ltmPressureUIHooksMu.Unlock()
	filtered := ltmPressureUIHooks[:0]
	for _, existing := range ltmPressureUIHooks {
		if reflect.ValueOf(existing).Pointer() != reflect.ValueOf(hook).Pointer() {
			filtered = append(filtered, existing)
		}
	}
	ltmPressureUIHooks = filtered
}

type continueSessionSnapshot struct {
	SessionID     string
	Status        string
	ActiveFile    string
	OpenFiles     []string
	WorkspaceRoot string
	UpdatedAt     time.Time
}

type continueWorkerRegistration struct {
	SessionID    string   `json:"session_id"`
	CallbackURL  string   `json:"callback_url"`
	Capabilities []string `json:"capabilities,omitempty"`
	IDE          string   `json:"ide,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

type continueBridgeSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]continueSessionSnapshot
}

type continueBridgeWorkerStore struct {
	mu      sync.RWMutex
	workers map[string]continueWorkerRegistration
}

type continueExecutionResult struct {
	Status        string                          `json:"status,omitempty"`
	Summary       string                          `json:"summary,omitempty"`
	ModifiedFiles []continueExecutionModifiedFile `json:"modified_files,omitempty"`
	Metadata      map[string]any                  `json:"metadata,omitempty"`
}

type continueExecutionModifiedFile struct {
	FilePath      string         `json:"file_path,omitempty"`
	CRUDOperation string         `json:"crud_operation,omitempty"`
	DiffSummary   string         `json:"diff_summary,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type continueWorkerCallbackOutcome struct {
	StatusCode  int
	ContentType string
	RawBody     string
	FinalResult *continueExecutionResult
}

type continueAgentExecuteRequest struct {
	RequestID      string   `json:"request_id,omitempty"`
	SessionID      string   `json:"session_id"`
	Task           string   `json:"task"`
	Mode           string   `json:"mode,omitempty"`
	RequestedPhase string   `json:"requested_phase,omitempty"`
	AllowedTools   []string `json:"allowed_tools,omitempty"`
	ToolChoice     string   `json:"tool_choice,omitempty"`
	Context        struct {
		WorkspaceRoot string   `json:"workspace_root"`
		ActiveFile    string   `json:"active_file"`
		SelectedText  string   `json:"selected_text,omitempty"`
		OpenFiles     []string `json:"open_files,omitempty"`
	} `json:"context"`
}

type continueEditDelegationContext struct {
	SessionID     string
	WorkspaceRoot string
	ActiveFile    string
	OpenFiles     []string
}

var continueSessionState = newContinueBridgeSessionStore()
var continueWorkerState = newContinueBridgeWorkerStore()

// ObfuscationMode defines the global obfuscation policy.
type ObfuscationMode string

const (
	// ObfuscationDisabled means no obfuscation globally.
	ObfuscationDisabled ObfuscationMode = "disabled"
	// ObfuscationPerDatabase means we respect obfuscation flag per database.
	ObfuscationPerDatabase ObfuscationMode = "per_database"
	// ObfuscationAllDatabases means we enforce obfuscation globally.
	ObfuscationAllDatabases ObfuscationMode = "all_databases"
)

// DirectFlushingWriter writes directly to the http.ResponseWriter and flushes
type DirectFlushingWriter struct {
	w http.ResponseWriter
}

func (d *DirectFlushingWriter) Write(p []byte) (n int, err error) {
	d.w.Header().Set("Content-Type", "application/x-ndjson")
	n, err = d.w.Write(p)
	if f, ok := d.w.(http.Flusher); ok {
		f.Flush()
	}
	return
}

var seededKBs sync.Map

func seedMetaCognitionAsync(userID, kbName string, sysOpts sop.DatabaseOptions) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, loaded := seededKBs.LoadOrStore(kbName, true)
	if loaded {
		return // Already seeding or seeded this KB
	}

	sysDB := aidb.NewDatabase(sysOpts)

	trans, err := sysDB.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return
	}

	embedder := GetConfiguredEmbedder(nil)
	kb, err := sysDB.OpenKnowledgeBase(ctx, kbName, trans, nil, embedder, false, true)
	if err != nil {
		trans.Rollback(ctx)
		return
	}
	res, _ := kb.Search(ctx, []memory.SearchRequest[map[string]any]{{Text: "Meta_Cognition", Limit: 1}})
	trans.Rollback(ctx)

	if len(res) > 0 && len(res[0]) > 0 {
		return
	}

	wTrans, err := sysDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return
	}
	defer wTrans.Rollback(ctx)
	wKb, err := sysDB.OpenKnowledgeBase(ctx, kbName, wTrans, nil, embedder, false, true)
	if err != nil {
		return
	}

	thoughtText := "Meta-Memory Rules: 1) Generalize bugs instead of memorizing stack traces. 2) Never duplicate information available in SOP. 3) Prioritize specific user preferences over generic defaults."
	vecs, err := embed.DocumentTexts(ctx, embedder, []string{thoughtText})
	if err == nil && len(vecs) > 0 {
		data := map[string]any{
			"type":        "meta_rule",
			"description": "Base rules for Long Term Memory consolidation",
		}
		_ = wKb.IngestThought(ctx, thoughtText, "Meta_Cognition", "system", vecs[0], data)
		if wTrans.Commit(ctx) == nil {
			log.Info("Successfully seeded lightweight Meta_Cognition to LTM", "user_id", userID)
		}
	}
}

// handleAIChat manages the HTTP UI integration for the AI Agent.
//
// Step-by-Step Flow:
// 1. Request Initialization: Validates HTTP POST, reads raw body, and unmarshals JSON to extract Message, SessionID, UserID, Agent, etc.
// 2. Stream Setup: Initializes an NDJSON event writer (sendEvent) that flushes backend progress (logs, content, records) dynamically to the UI.
// 3. Context & Agent Resolution: Defaults to "copilot" if no agent specified, verifies agent exists, and handles zero-day session bootstrapping via UUID.
// 4. Session Mutex Locking: Isolates and locks the active session via activeSessions.GetOrCreate(), applying Agent.Clone() to ensure thread safety.
// 5. Payload Construction: Determines the 'AvatarScope' based on selected knowledge bases, bundles DB contexts, and anchors security via GlobalObfuscator.
// 6. Agent Execution Lifecycle: Uses `agentSvc.Open()` to bind physical memory contexts natively and asynchronously fires the `seedMetaCognitionAsync` task.
// 7. RAG Execution: Delegates the complex query processing to `agentSvc.Ask()`.
// 8. Output Interpretation (Heuristics): Scans response strings for required actions, valid JSON array/object structures, or NDJSON Script streams, rendering them gracefully as distinct UI records or standard MarkDown messages.
func handleContinueChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload continueChatRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	chatReq := buildAIChatRequestFromContinueChatRequest(payload)
	if chatReq == nil {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(chatReq.UserID) == "" {
		if authUserID := strings.TrimSpace(sop.GetAuthFromContext(r.Context()).UserID); authUserID != "" {
			chatReq.UserID = authUserID
		}
	}

	if strings.TrimSpace(chatReq.SessionID) == "" {
		chatReq.SessionID = uuid.NewString()
	}
	storeContinueSessionContext(payload)

	// Reuse the existing AI chat pipeline with NDJSON streaming.
	// The bridge is intentionally thin: the editor supplies context and the daemon handles execution.
	legacyReq := r.Clone(r.Context())
	legacyReq.Method = http.MethodPost
	legacyReq.Body = io.NopCloser(strings.NewReader(mustJSON(chatReq)))
	legacyReq.ContentLength = int64(len(mustJSON(chatReq)))
	legacyReq.Header.Set("Content-Type", "application/json")
	legacyReq.RequestURI = ""
	if payload.Provider != "" {
		legacyReq.Header.Set("X-LLM-Provider", payload.Provider)
	}
	if payload.APIKey != "" {
		legacyReq.Header.Set("X-LLM-API-Key", payload.APIKey)
	}
	if payload.URL != "" {
		legacyReq.Header.Set("X-LLM-URL", payload.URL)
	}
	if payload.Model != "" {
		legacyReq.Header.Set("X-LLM-Model", payload.Model)
	}

	handleAIChat(w, legacyReq)
}

func handleAIChat(w http.ResponseWriter, r *http.Request) {
	req, err := initializeRequest(w, r)
	if err != nil {
		return
	}

	sendEvent, hasStreamedContent := SetupStream(w)

	blueprint, err := resolveContextAndAgent(r.Context(), req, sendEvent)
	if err != nil {
		return
	}

	agentSvc, sessionMu := lockSession(req, blueprint)
	sessionMu.Lock()
	defer sessionMu.Unlock()
	log.Debug("HTTP AI Chat Request",
		"session_id", req.SessionID,
		"agent", req.Agent,
		"agent_type", fmt.Sprintf("%T", agentSvc),
		"provider", req.Provider,
		"database", req.Database,
		"domain", req.Domain,
		"selected_kb_count", len(req.SelectedKBs),
		"message_chars", len(req.Message),
	)

	sendEvent("session_id", req.SessionID)

	var rs *agent.RunnerSession
	if svc, ok := agentSvc.(*agent.Service); ok && svc != nil {
		rs = svc.RunnerSession()
	}

	llmSettings := resolveLLMSettings(r)
	ctx, payload, cfg, fullMessage := constructPayload(r.Context(), w, req, llmSettings, sendEvent, rs)
	defer unregisterLTMPressureUIHook(sendEvent)
	log.Debug("HTTP AI Chat Payload",
		"session_id", req.SessionID,
		"current_db", payload.CurrentDB,
		"active_domain", payload.ActiveDomain,
		"avatar_scope", payload.AvatarScope,
		"selected_kbs", len(payload.SelectedKBs),
		"full_message_chars", len(fullMessage),
	)

	// Inject streaming writer bypass
	ctx = context.WithValue(ctx, ai.CtxKeyWriter, &DirectFlushingWriter{w: w})
	ctx = context.WithValue(ctx, ai.CtxKeyAppBaseURL, buildAppBaseURL(r))

	if err := executeAgentLifecycle(ctx, r, agentSvc, req, sendEvent); err != nil {
		return
	}

	// Ensure Close is ALWAYS called, even on panic or context cancellation
	defer func() {
		if r := recover(); r != nil {
			log.Error("Panic during agent execution", "panic", r, "session_id", req.SessionID)
			// Use background context since original may be canceled
			if err := agentSvc.Close(context.Background()); err != nil {
				log.Error(fmt.Sprintf("Agent '%s' failed to close after panic: %v", req.Agent, err))
			}
			sendEvent("error", fmt.Sprintf("Internal error: %v", r))
			panic(r) // Re-throw after cleanup
		}
		// Normal close - use original context to allow proper commit/rollback decisions
		if err := agentSvc.Close(ctx); err != nil {
			log.Error(fmt.Sprintf("Agent '%s' failed to close session: %v", req.Agent, err))
		}
	}()

	response, err := executeRAG(ctx, agentSvc, req, fullMessage, cfg, sendEvent)
	if err != nil {
		// Check if error is due to context cancellation
		if ctx.Err() != nil {
			log.Warn("Request canceled by client", "session_id", req.SessionID, "error", ctx.Err())
			sendEvent("error", "Request canceled")
		}
		return
	}
	log.Debug("HTTP AI Chat Response",
		"session_id", req.SessionID,
		"response_chars", len(response),
	)

	// When the event stream already sent assistant content, calling interpretOutput
	// would duplicate the final answer. Only run it for the path where no content
	// has been flushed yet.
	if !hasStreamedContent() {
		interpretOutput(response, sendEvent)
	}
}

type aiChatRequest struct {
	Message       string                 `json:"message"`
	Database      string                 `json:"database"`
	StoreName     string                 `json:"store"`
	Agent         string                 `json:"agent"`
	Provider      string                 `json:"provider"`
	Format        string                 `json:"format"`
	ThinkingLevel string                 `json:"thinking_level"`
	SessionID     string                 `json:"session_id"`
	UserID        string                 `json:"user_id"`
	ClientID      string                 `json:"client_id"`
	Domain        string                 `json:"domain"`
	SelectedKBs   []ai.ArtifactReference `json:"selected_kbs"`
	InputParts    []ai.ContentPart       `json:"input_parts"`
}

type continueChatRequest struct {
	SessionID string                   `json:"session_id"`
	Message   string                   `json:"message"`
	Workspace continueWorkspaceContext `json:"workspace"`
	Context   continueChatContext      `json:"context"`
	Tools     []continueToolHint       `json:"tools"`
	Mode      string                   `json:"mode"`
	Provider  string                   `json:"provider"`
	APIKey    string                   `json:"api_key"`
	URL       string                   `json:"url"`
	Model     string                   `json:"model"`
}

type continueToolHint struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type continueToolRequest struct {
	Path       string             `json:"path"`
	Query      string             `json:"query"`
	StartLine  int                `json:"start_line"`
	EndLine    int                `json:"end_line"`
	Recursive  bool               `json:"recursive"`
	Limit      int                `json:"limit"`
	SessionID  string             `json:"session_id"`
	Workspace  string             `json:"workspace"`
	Selection  string             `json:"selection"`
	ActiveFile string             `json:"active_file"`
	Find       string             `json:"find"`
	Replace    string             `json:"replace"`
	Edits      []continueTextEdit `json:"edits"`
}

type continueTextEdit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

type continueWorkspaceContext struct {
	Root       string `json:"root"`
	ActiveFile string `json:"active_file"`
}

type continueChatContext struct {
	OpenFiles    []string `json:"open_files"`
	SelectedText string   `json:"selected_text"`
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func buildAIChatRequestFromContinueChatRequest(req continueChatRequest) *aiChatRequest {
	if req.Message == "" {
		return nil
	}

	messageParts := []string{strings.TrimSpace(req.Message)}
	if req.Workspace.ActiveFile != "" {
		messageParts = append(messageParts, fmt.Sprintf("\nActive file: %s", req.Workspace.ActiveFile))
	}
	if req.Context.SelectedText != "" {
		messageParts = append(messageParts, fmt.Sprintf("\nSelected text:\n%s", req.Context.SelectedText))
	}
	if len(req.Context.OpenFiles) > 0 {
		messageParts = append(messageParts, fmt.Sprintf("\nOpen files: %s", strings.Join(req.Context.OpenFiles, ", ")))
	}
	if req.Workspace.Root != "" {
		messageParts = append(messageParts, fmt.Sprintf("\nWorkspace root: %s", req.Workspace.Root))
	}
	if len(req.Tools) > 0 {
		toolLines := []string{"\nRequested tools:"}
		for _, tool := range req.Tools {
			if tool.Name == "" {
				continue
			}
			toolLines = append(toolLines, fmt.Sprintf("\n- %s", tool.Name))
			if len(tool.Args) > 0 {
				toolLines = append(toolLines, fmt.Sprintf(": %s", mustJSON(tool.Args)))
			}
		}
		messageParts = append(messageParts, strings.Join(toolLines, ""))
	}

	return &aiChatRequest{
		Message:   strings.Join(messageParts, ""),
		SessionID: strings.TrimSpace(req.SessionID),
		Agent:     "copilot",
		Provider:  strings.TrimSpace(req.Provider),
	}
}

func handleContinueSessionState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/continue/sessions/")
	if sessionID == "" {
		http.Error(w, "session id is required", http.StatusBadRequest)
		return
	}

	if snapshot, ok := continueSessionState.Get(sessionID); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":     snapshot.SessionID,
			"status":         snapshot.Status,
			"phase":          "investigation",
			"message":        "SOP bridge session is active",
			"active_file":    snapshot.ActiveFile,
			"open_files":     snapshot.OpenFiles,
			"workspace_root": snapshot.WorkspaceRoot,
		})
		return
	}

	if _, exists := activeSessions.Get(sessionID); !exists {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id": sessionID,
		"status":     "active",
		"phase":      "investigation",
		"message":    "SOP bridge session is active",
	})
}

func handleContinueWorkerRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var registration continueWorkerRegistration
	if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(registration.SessionID) == "" || strings.TrimSpace(registration.CallbackURL) == "" {
		http.Error(w, "session_id and callback_url are required", http.StatusBadRequest)
		return
	}
	if err := validateContinueWorkerCallbackURL(registration.CallbackURL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	registration.Capabilities = normalizeContinueWorkerCapabilities(registration.Capabilities)
	registration.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	continueWorkerState.Put(registration)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "worker": registration})
}

func handleContinueWorkerState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/continue/workers/"), "/api/sop/workers/"))
	if sessionID == "" {
		http.Error(w, "session id is required", http.StatusBadRequest)
		return
	}
	registration, ok := continueWorkerState.Get(sessionID)
	if !ok {
		http.Error(w, "worker not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(registration)
}

func handleContinueTool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	toolName := strings.TrimPrefix(r.URL.Path, "/api/continue/tools/")
	if toolName == "" {
		http.Error(w, "tool name is required", http.StatusBadRequest)
		return
	}

	var req continueToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	var result any
	var err error
	switch toolName {
	case "repo.read_file":
		result, err = readContinueFile(req.Path, req.StartLine, req.EndLine)
	case "repo.search_code":
		result, err = searchContinueCode(req.Query, req.Path, req.Limit)
	case "repo.list_files":
		result, err = listContinueFiles(req.Path, req.Recursive)
	case "code.apply_patch":
		result, err = handleContinuePatchTool(req)
	case "code.preview_patch":
		result, err = previewContinuePatch(req.Path, req.Selection)
	case "single_find_and_replace":
		result, err = handleContinueEditTool(req, "single_find_and_replace")
	case "multi_edit":
		result, err = handleContinueEditTool(req, "multi_edit")
	case "test.run":
		result, err = runContinueTest(req.Query, req.SessionID)
	case "agent.ask":
		result, err = delegateContinueAgent(req.Query, req.SessionID)
	default:
		http.Error(w, "unsupported tool", http.StatusNotImplemented)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "tool": toolName, "result": result})
}

func handleContinuePatchTool(req continueToolRequest) (any, error) {
	targetPath := resolveContinueEditPath(req.Path, req.ActiveFile)
	if delegatedResult, delegated, err := tryDelegateContinueEditTool(req, "code.apply_patch", targetPath); delegated {
		return delegatedResult, err
	}
	return applyContinuePatch(req.Path, req.Selection)
}

func handleContinueEditTool(req continueToolRequest, toolName string) (any, error) {
	targetPath := resolveContinueEditPath(req.Path, req.ActiveFile)
	if delegatedResult, delegated, err := tryDelegateContinueEditTool(req, toolName, targetPath); delegated {
		return delegatedResult, err
	}
	switch toolName {
	case "single_find_and_replace":
		return applyContinueSingleFindAndReplace(targetPath, req.Find, req.Replace)
	case "multi_edit":
		return applyContinueMultiEdit(targetPath, req.Edits)
	default:
		return nil, fmt.Errorf("unsupported edit tool: %s", toolName)
	}
}

func tryDelegateContinueEditTool(req continueToolRequest, toolName, targetPath string) (any, bool, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" || strings.TrimSpace(targetPath) == "" {
		return nil, false, nil
	}
	snapshot, ok := continueSessionState.Get(sessionID)
	if !ok {
		return nil, false, nil
	}
	editContext, ok := resolveContinueOpenFileContext(targetPath, req, snapshot)
	if !ok {
		return nil, false, nil
	}
	registration, ok := continueWorkerState.Get(sessionID)
	if !ok {
		return nil, false, nil
	}
	delegatedReq, err := buildContinueEditDelegationRequest(toolName, req, editContext)
	if err != nil {
		return nil, false, nil
	}
	if !continueWorkerSupportsCapabilities(registration, delegatedReq) {
		return nil, false, nil
	}
	outcome, err := executeContinueWorkerCallback(context.Background(), delegatedReq, registration)
	if err != nil {
		return nil, true, err
	}
	if outcome.StatusCode >= http.StatusBadRequest {
		trimmedBody := strings.TrimSpace(outcome.RawBody)
		if trimmedBody == "" {
			trimmedBody = http.StatusText(outcome.StatusCode)
		}
		return nil, true, fmt.Errorf("IDE worker returned status %d: %s", outcome.StatusCode, trimmedBody)
	}
	result := map[string]any{
		"path":      editContext.ActiveFile,
		"applied":   true,
		"delegated": true,
		"worker":    "continue",
		"mode":      continueDelegatedToolChoice(toolName),
	}
	if outcome.FinalResult != nil {
		result["execution_result"] = cloneContinueExecutionResult(*outcome.FinalResult)
		if summary := strings.TrimSpace(outcome.FinalResult.Summary); summary != "" {
			result["summary"] = summary
		}
	}
	if strings.TrimSpace(outcome.RawBody) != "" {
		result["raw_result"] = strings.TrimSpace(outcome.RawBody)
	}
	return result, true, nil
}

func readContinueFile(path string, startLine, endLine int) (map[string]any, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cwd(), cleanPath))
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine > len(lines) {
		endLine = len(lines)
	}
	selected := lines[startLine-1 : endLine]
	return map[string]any{
		"path":       cleanPath,
		"content":    strings.Join(selected, "\n"),
		"start_line": startLine,
		"end_line":   endLine,
	}, nil
}

func searchContinueCode(query, path string, limit int) (map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 20
	}
	root := strings.TrimSpace(path)
	if root == "" {
		root = cwd()
	}
	if !filepath.IsAbs(root) {
		root = filepath.Clean(filepath.Join(cwd(), root))
	}
	var hits []map[string]any
	err := filepath.WalkDir(root, func(currentPath string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(currentPath, ".git") || strings.HasSuffix(currentPath, ".png") || strings.HasSuffix(currentPath, ".jpg") || strings.HasSuffix(currentPath, ".jpeg") || strings.HasSuffix(currentPath, ".pdf") {
			return nil
		}
		data, readErr := os.ReadFile(currentPath)
		if readErr != nil {
			return nil
		}
		text := string(data)
		if strings.Contains(text, query) {
			hits = append(hits, map[string]any{"path": currentPath, "text": text[:min(200, len(text))]})
			if len(hits) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"hits": hits}, nil
}

func listContinueFiles(path string, recursive bool) (map[string]any, error) {
	root := strings.TrimSpace(path)
	if root == "" {
		root = cwd()
	}
	if !filepath.IsAbs(root) {
		root = filepath.Clean(filepath.Join(cwd(), root))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	return map[string]any{"path": root, "files": result, "recursive": recursive}, nil
}

func applyContinuePatch(path, patch string) (map[string]any, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cwd(), cleanPath))
	}
	if strings.TrimSpace(patch) == "" {
		return nil, fmt.Errorf("patch is required")
	}
	originalContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	updatedContent, mode, err := applyContinuePatchContent(string(originalContent), patch)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cleanPath, []byte(updatedContent), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": cleanPath, "applied": true, "content": updatedContent, "mode": mode}, nil
}

func previewContinuePatch(path, patch string) (map[string]any, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cwd(), cleanPath))
	}
	if strings.TrimSpace(patch) == "" {
		return nil, fmt.Errorf("patch is required")
	}
	oldContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	updatedContent, mode, err := applyContinuePatchContent(string(oldContent), patch)
	if err != nil {
		return nil, err
	}
	preview := fmt.Sprintf("--- %s\n+++ %s\n-%s\n+%s\n", cleanPath, cleanPath, string(oldContent), updatedContent)
	return map[string]any{"path": cleanPath, "preview": preview, "applied": false, "mode": mode}, nil
}

func applyContinuePatchContent(original, patch string) (string, string, error) {
	if !looksLikeUnifiedDiffPatch(patch) {
		return patch, "replace", nil
	}
	updated, err := applyUnifiedDiffHunks(original, patch)
	if err != nil {
		return "", "", err
	}
	return updated, "unified_hunk", nil
}

func storeContinueSessionContext(req continueChatRequest) {
	if strings.TrimSpace(req.SessionID) == "" {
		return
	}
	continueSessionState.Put(continueSessionSnapshot{
		SessionID:     strings.TrimSpace(req.SessionID),
		Status:        "active",
		ActiveFile:    strings.TrimSpace(req.Workspace.ActiveFile),
		OpenFiles:     normalizeContinueOpenFiles(req.Context.OpenFiles),
		WorkspaceRoot: strings.TrimSpace(req.Workspace.Root),
		UpdatedAt:     time.Now().UTC(),
	})
}

func normalizeContinueOpenFiles(openFiles []string) []string {
	if len(openFiles) == 0 {
		return nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(openFiles))
	for _, path := range openFiles {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func resolveContinueOpenFileContext(targetPath string, req continueToolRequest, snapshot continueSessionSnapshot) (continueEditDelegationContext, bool) {
	workspaceRoot := resolveContinueWorkspaceRoot(strings.TrimSpace(req.Workspace), strings.TrimSpace(snapshot.WorkspaceRoot))
	matchedFile := matchContinueOpenFile(targetPath, workspaceRoot, strings.TrimSpace(snapshot.ActiveFile), snapshot.OpenFiles)
	if matchedFile == "" {
		fallbackActive := strings.TrimSpace(req.ActiveFile)
		if fallbackActive == "" {
			fallbackActive = strings.TrimSpace(snapshot.ActiveFile)
		}
		matchedFile = matchContinueOpenFile(targetPath, workspaceRoot, fallbackActive, nil)
	}
	if matchedFile == "" {
		return continueEditDelegationContext{}, false
	}
	openFiles := normalizeContinueOpenFiles(snapshot.OpenFiles)
	if len(openFiles) == 0 {
		openFiles = []string{matchedFile}
	}
	return continueEditDelegationContext{
		SessionID:     strings.TrimSpace(req.SessionID),
		WorkspaceRoot: workspaceRoot,
		ActiveFile:    matchedFile,
		OpenFiles:     openFiles,
	}, true
}

func resolveContinueWorkspaceRoot(candidates ...string) string {
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if filepath.IsAbs(trimmed) {
			return filepath.Clean(trimmed)
		}
		return filepath.Clean(filepath.Join(cwd(), trimmed))
	}
	return cwd()
}

func matchContinueOpenFile(targetPath, workspaceRoot, activeFile string, openFiles []string) string {
	targetComparable := continueComparablePath(targetPath, workspaceRoot)
	if targetComparable == "" {
		return ""
	}
	if trimmedActive := strings.TrimSpace(activeFile); trimmedActive != "" && continueComparablePath(trimmedActive, workspaceRoot) == targetComparable {
		return trimmedActive
	}
	for _, openFile := range normalizeContinueOpenFiles(openFiles) {
		if continueComparablePath(openFile, workspaceRoot) == targetComparable {
			return openFile
		}
	}
	return ""
}

func continueComparablePath(path, workspaceRoot string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(filepath.Join(resolveContinueWorkspaceRoot(workspaceRoot), trimmed))
}

func resolveContinueEditPath(path, activeFile string) string {
	if trimmed := strings.TrimSpace(path); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(activeFile)
}

func applyContinueSingleFindAndReplace(path, find, replace string) (map[string]any, error) {
	cleanPath, originalContent, err := readContinueEditableFile(path)
	if err != nil {
		return nil, err
	}
	updatedContent, replacements, err := replaceOnceOrFail(string(originalContent), find, replace)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(cleanPath, []byte(updatedContent), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": cleanPath, "applied": true, "content": updatedContent, "mode": "single_find_and_replace", "replacements": replacements}, nil
}

func applyContinueMultiEdit(path string, edits []continueTextEdit) (map[string]any, error) {
	cleanPath, originalContent, err := readContinueEditableFile(path)
	if err != nil {
		return nil, err
	}
	updatedContent := string(originalContent)
	totalReplacements := 0
	for _, edit := range edits {
		nextContent, replacements, err := replaceOnceOrFail(updatedContent, edit.Find, edit.Replace)
		if err != nil {
			return nil, err
		}
		updatedContent = nextContent
		totalReplacements += replacements
	}
	if err := os.WriteFile(cleanPath, []byte(updatedContent), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": cleanPath, "applied": true, "content": updatedContent, "mode": "multi_edit", "replacements": totalReplacements}, nil
}

func readContinueEditableFile(path string) (string, []byte, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "", nil, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cwd(), cleanPath))
	}
	originalContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", nil, err
	}
	return cleanPath, originalContent, nil
}

func replaceOnceOrFail(content, find, replace string) (string, int, error) {
	needle := strings.TrimSpace(find)
	if needle == "" {
		return "", 0, fmt.Errorf("find is required")
	}
	count := strings.Count(content, find)
	if count == 0 {
		return "", 0, fmt.Errorf("find text not found")
	}
	if count > 1 {
		return "", 0, fmt.Errorf("find text matched %d locations; provide a more specific snippet or use multi_edit with narrower edits", count)
	}
	return strings.Replace(content, find, replace, 1), 1, nil
}

func buildContinueEditDelegationRequest(toolName string, req continueToolRequest, editContext continueEditDelegationContext) (continueAgentExecuteRequest, error) {
	workerToolName := continueDelegatedToolChoice(toolName)
	if workerToolName == "" {
		return continueAgentExecuteRequest{}, fmt.Errorf("unsupported edit tool: %s", toolName)
	}
	continueReq := continueAgentExecuteRequest{
		RequestID:      uuid.NewString(),
		SessionID:      editContext.SessionID,
		Mode:           "apply",
		RequestedPhase: "execution",
		AllowedTools:   []string{"read_file", workerToolName},
		ToolChoice:     workerToolName,
	}
	continueReq.Context.WorkspaceRoot = editContext.WorkspaceRoot
	continueReq.Context.ActiveFile = editContext.ActiveFile
	continueReq.Context.OpenFiles = append([]string(nil), editContext.OpenFiles...)

	switch toolName {
	case "code.apply_patch":
		if strings.TrimSpace(req.Selection) == "" {
			return continueAgentExecuteRequest{}, fmt.Errorf("selection is required")
		}
		continueReq.Task = fmt.Sprintf("Apply the provided patch to %s only. Use patch-style editing, keep the change bounded to this file, and do not make unrelated modifications.\n\nPatch content:\n\n%s", editContext.ActiveFile, req.Selection)
		continueReq.Context.SelectedText = req.Selection
	case "single_find_and_replace":
		if strings.TrimSpace(req.Find) == "" {
			return continueAgentExecuteRequest{}, fmt.Errorf("find is required")
		}
		continueReq.Task = fmt.Sprintf("Apply one exact find-and-replace edit to %s only. Replace this exact snippet:\n\n%s\n\nwith:\n\n%s\n\nDo not rewrite the whole file or make unrelated changes.", editContext.ActiveFile, req.Find, req.Replace)
		continueReq.Context.SelectedText = req.Find
	case "multi_edit":
		if len(req.Edits) == 0 {
			return continueAgentExecuteRequest{}, fmt.Errorf("edits are required")
		}
		var task strings.Builder
		task.WriteString(fmt.Sprintf("Apply the following exact edits to %s only. Do not rewrite the whole file or make unrelated changes.\n", editContext.ActiveFile))
		for index, edit := range req.Edits {
			if strings.TrimSpace(edit.Find) == "" {
				return continueAgentExecuteRequest{}, fmt.Errorf("edit %d find is required", index)
			}
			task.WriteString(fmt.Sprintf("\nEdit %d find:\n%s\n\nEdit %d replace:\n%s\n", index+1, edit.Find, index+1, edit.Replace))
		}
		continueReq.Task = task.String()
		continueReq.Context.SelectedText = req.Edits[0].Find
	}
	return continueReq, nil
}

func continueDelegatedToolChoice(toolName string) string {
	switch toolName {
	case "code.apply_patch":
		return "apply_patch"
	case "single_find_and_replace", "multi_edit":
		return toolName
	default:
		return ""
	}
}

func continueWorkerSupportsCapabilities(registration continueWorkerRegistration, req continueAgentExecuteRequest) bool {
	available := make(map[string]bool, len(registration.Capabilities))
	for _, capability := range registration.Capabilities {
		available[strings.ToLower(strings.TrimSpace(capability))] = true
	}
	for _, capability := range requiredContinueWorkerCapabilities(req) {
		if !available[capability] {
			return false
		}
	}
	return true
}

func requiredContinueWorkerCapabilities(req continueAgentExecuteRequest) []string {
	required := []string{}
	if strings.TrimSpace(req.Context.ActiveFile) != "" || len(req.Context.OpenFiles) > 0 || strings.TrimSpace(req.Context.SelectedText) != "" {
		required = append(required, "read_context")
	}
	if strings.TrimSpace(req.ToolChoice) != "" || strings.TrimSpace(req.Mode) == "apply" || strings.TrimSpace(req.RequestedPhase) == "execution" {
		required = append(required, "apply_patch")
	}
	return required
}

func normalizeContinueWorkerCapabilities(capabilities []string) []string {
	seen := make(map[string]bool, len(capabilities))
	result := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		trimmed := strings.ToLower(strings.TrimSpace(capability))
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func validateContinueWorkerCallbackURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("callback_url is required")
	}
	parsedURL, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("callback_url must be a valid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("callback_url must use http or https")
	}
	if parsedURL.Host == "" || parsedURL.User != nil {
		return fmt.Errorf("callback_url must include a loopback host without user info")
	}
	host := strings.TrimSpace(parsedURL.Hostname())
	if host == "localhost" {
		return nil
	}
	if parsedIP := net.ParseIP(host); parsedIP != nil && parsedIP.IsLoopback() {
		return nil
	}
	return fmt.Errorf("callback_url host must be loopback-only")
}

func executeContinueWorkerCallback(ctx context.Context, req continueAgentExecuteRequest, registration continueWorkerRegistration) (continueWorkerCallbackOutcome, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return continueWorkerCallbackOutcome{}, fmt.Errorf("marshal worker request: %w", err)
	}
	callbackReq, err := http.NewRequestWithContext(ctx, http.MethodPost, registration.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return continueWorkerCallbackOutcome{}, fmt.Errorf("build worker callback request: %w", err)
	}
	callbackReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(callbackReq)
	if err != nil {
		return continueWorkerCallbackOutcome{}, fmt.Errorf("call IDE worker: %w", err)
	}
	defer resp.Body.Close()
	outcome, err := readContinueWorkerResponse(resp.Body)
	if err != nil {
		return continueWorkerCallbackOutcome{}, fmt.Errorf("read worker response: %w", err)
	}
	outcome.StatusCode = resp.StatusCode
	outcome.ContentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	return outcome, nil
}

func readContinueWorkerResponse(body io.Reader) (continueWorkerCallbackOutcome, error) {
	reader := bufio.NewReader(body)
	var raw strings.Builder
	outcome := continueWorkerCallbackOutcome{}
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			captureContinueWorkerRaw(&raw, line)
			captureContinueWorkerResult(&outcome, line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return outcome, err
		}
	}
	outcome.RawBody = raw.String()
	return outcome, nil
}

func captureContinueWorkerRaw(raw *strings.Builder, rawLine []byte) {
	if raw == nil || raw.Len() >= continueWorkerRawBodyLimit {
		return
	}
	trimmed := strings.TrimSpace(string(rawLine))
	if trimmed == "" {
		return
	}
	var envelope struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Content string `json:"content"`
		Error   string `json:"error"`
		Result  any    `json:"result"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil {
		switch envelope.Type {
		case "done":
			if envelope.Result != nil {
				return
			}
		case "progress":
			trimmed = strings.TrimSpace(envelope.Message)
		case "content":
			trimmed = strings.TrimSpace(envelope.Content)
		case "error":
			trimmed = strings.TrimSpace(envelope.Error)
		}
	}
	if trimmed == "" {
		return
	}
	if raw.Len() > 0 {
		raw.WriteByte('\n')
	}
	remaining := continueWorkerRawBodyLimit - raw.Len()
	if len(trimmed) > remaining {
		raw.WriteString(trimmed[:remaining])
		return
	}
	raw.WriteString(trimmed)
}

func captureContinueWorkerResult(outcome *continueWorkerCallbackOutcome, rawLine []byte) {
	trimmed := strings.TrimSpace(string(rawLine))
	if trimmed == "" || outcome == nil {
		return
	}
	var envelope struct {
		Type   string                  `json:"type"`
		Result continueExecutionResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return
	}
	if envelope.Type == "done" {
		resultCopy := cloneContinueExecutionResult(envelope.Result)
		outcome.FinalResult = &resultCopy
	}
}

func cloneContinueExecutionResult(src continueExecutionResult) continueExecutionResult {
	result := continueExecutionResult{Status: src.Status, Summary: src.Summary}
	if len(src.ModifiedFiles) > 0 {
		result.ModifiedFiles = make([]continueExecutionModifiedFile, len(src.ModifiedFiles))
		copy(result.ModifiedFiles, src.ModifiedFiles)
	}
	if len(src.Metadata) > 0 {
		result.Metadata = cloneAnyMap(src.Metadata)
	}
	return result
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}

func newContinueBridgeSessionStore() *continueBridgeSessionStore {
	return &continueBridgeSessionStore{sessions: map[string]continueSessionSnapshot{}}
}

func newContinueBridgeWorkerStore() *continueBridgeWorkerStore {
	return &continueBridgeWorkerStore{workers: map[string]continueWorkerRegistration{}}
}

func (s *continueBridgeSessionStore) Put(snapshot continueSessionSnapshot) {
	if s == nil || strings.TrimSpace(snapshot.SessionID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[snapshot.SessionID] = snapshot
}

func (s *continueBridgeSessionStore) Get(sessionID string) (continueSessionSnapshot, bool) {
	if s == nil || strings.TrimSpace(sessionID) == "" {
		return continueSessionSnapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.sessions[sessionID]
	return snapshot, ok
}

func (s *continueBridgeWorkerStore) Put(registration continueWorkerRegistration) {
	if s == nil || strings.TrimSpace(registration.SessionID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers[registration.SessionID] = registration
}

func (s *continueBridgeWorkerStore) Get(sessionID string) (continueWorkerRegistration, bool) {
	if s == nil || strings.TrimSpace(sessionID) == "" {
		return continueWorkerRegistration{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	registration, ok := s.workers[sessionID]
	return registration, ok
}

func looksLikeUnifiedDiffPatch(patch string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "@@ ") {
			return true
		}
	}
	return false
}

func applyUnifiedDiffHunks(original, patch string) (string, error) {
	originalLines, trailingNewline := splitContentLines(original)
	patchLines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")

	result := make([]string, 0, len(originalLines))
	originalIdx := 0
	for i := 0; i < len(patchLines); i++ {
		line := patchLines[i]
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "@@ ") {
			continue
		}

		match := unifiedHunkHeaderPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) < 3 {
			return "", fmt.Errorf("invalid unified diff hunk header: %s", line)
		}
		oldStart, err := strconv.Atoi(match[1])
		if err != nil {
			return "", fmt.Errorf("invalid unified diff hunk start: %w", err)
		}
		copyUntil := oldStart - 1
		if copyUntil < 0 {
			copyUntil = 0
		}
		if copyUntil > len(originalLines) {
			return "", fmt.Errorf("hunk start %d is beyond file length %d", oldStart, len(originalLines))
		}
		for originalIdx < copyUntil {
			result = append(result, originalLines[originalIdx])
			originalIdx++
		}

		for i+1 < len(patchLines) {
			nextLine := patchLines[i+1]
			if strings.HasPrefix(strings.TrimSpace(nextLine), "@@ ") {
				break
			}
			i++
			if strings.HasPrefix(nextLine, "\\") {
				continue
			}
			if nextLine == "" {
				return "", fmt.Errorf("invalid unified diff payload line")
			}
			switch nextLine[0] {
			case ' ':
				if originalIdx >= len(originalLines) || originalLines[originalIdx] != nextLine[1:] {
					return "", fmt.Errorf("context mismatch while applying unified diff")
				}
				result = append(result, originalLines[originalIdx])
				originalIdx++
			case '-':
				if originalIdx >= len(originalLines) || originalLines[originalIdx] != nextLine[1:] {
					return "", fmt.Errorf("delete mismatch while applying unified diff")
				}
				originalIdx++
			case '+':
				result = append(result, nextLine[1:])
			default:
				return "", fmt.Errorf("invalid unified diff line prefix: %q", nextLine[:1])
			}
		}
	}

	for originalIdx < len(originalLines) {
		result = append(result, originalLines[originalIdx])
		originalIdx++
	}

	return joinContentLines(result, trailingNewline), nil
}

func splitContentLines(content string) ([]string, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	trailing := strings.HasSuffix(normalized, "\n")
	trimmed := normalized
	if trailing {
		trimmed = strings.TrimSuffix(trimmed, "\n")
	}
	if trimmed == "" {
		return nil, trailing
	}
	return strings.Split(trimmed, "\n"), trailing
}

func joinContentLines(lines []string, trailingNewline bool) string {
	content := strings.Join(lines, "\n")
	if trailingNewline {
		return content + "\n"
	}
	return content
}

func runContinueTest(command, sessionID string) (map[string]any, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil, fmt.Errorf("command is required")
	}
	if sessionID == "" {
		sessionID = "continue-tests"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, "sh", "-c", trimmed)
	cmd.Dir = cwd()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("command timed out")
		}
		return map[string]any{
			"command":       trimmed,
			"output":        stdout.String() + stderr.String(),
			"still_running": false,
			"exit_code":     0,
			"error":         err.Error(),
		}, nil
	}

	return map[string]any{
		"command":       trimmed,
		"output":        stdout.String() + stderr.String(),
		"still_running": false,
		"exit_code":     0,
	}, nil
}

func delegateContinueAgent(query, sessionID string) (map[string]any, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, fmt.Errorf("query is required")
	}
	agentSvc, exists := loadedAgents["copilot"]
	if !exists || agentSvc == nil {
		return map[string]any{
			"session_id": sessionID,
			"answer":     "",
			"error":      "copilot agent is not configured",
		}, nil
	}
	ctx := context.Background()
	if err := agentSvc.Open(ctx); err != nil {
		return map[string]any{
			"session_id": sessionID,
			"answer":     "",
			"error":      err.Error(),
		}, nil
	}
	defer func() {
		_ = agentSvc.Close(ctx)
	}()
	cfg := ai.NewConfigMap()
	answer, err := agentSvc.Ask(ctx, trimmed, cfg)
	if err != nil {
		return map[string]any{
			"session_id": sessionID,
			"answer":     "",
			"error":      err.Error(),
		}, nil
	}
	return map[string]any{"session_id": sessionID, "answer": answer}, nil
}

func cwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func buildAppBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}

	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}

	return strings.TrimRight(proto+"://"+host, "/")
}

func initializeRequest(w http.ResponseWriter, r *http.Request) (*aiChatRequest, error) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return nil, fmt.Errorf("method not allowed")
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	log.Debug("Received AIChat Request", "body", string(bodyBytes))

	var req aiChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("Invalid JSON body", "error", err)
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return nil, err
	}

	req.Database = strings.TrimSpace(req.Database)
	req.StoreName = strings.TrimSpace(req.StoreName)
	if authUserID := strings.TrimSpace(sop.GetAuthFromContext(r.Context()).UserID); authUserID != "" {
		req.UserID = authUserID
	}
	return &req, nil
}

func isStructuredToolResult(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "execute_script":
		return true
	default:
		return false
	}
}

func renderVisibleText(s string) string {
	if strings.Contains(s, `\u`) {
		var decoded string
		if err := json.Unmarshal([]byte("\""+strings.ReplaceAll(s, `"`, `\"`)+"\""), &decoded); err == nil {
			s = decoded
		}
	}
	return html.UnescapeString(s)
}

func shouldSuppressLogLikeChunk(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "\n") || strings.Contains(trimmed, "\r") {
		return false
	}

	prefixes := []string{
		"[INFO]",
		"[DEBUG]",
		"[WARN]",
		"[WARNING]",
		"[ERROR]",
		"INFO ",
		"DEBUG ",
		"WARN ",
		"WARNING ",
		"ERROR ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}

	timestampPattern := regexp.MustCompile(`^\d{4}[-/]\d{2}[-/]\d{2}([ T]\d{2}:\d{2}:\d{2}([.,]\d+)?(Z|[+-]\d{2}:\d{2})?)?`)
	if timestampPattern.MatchString(trimmed) {
		return true
	}

	if len(trimmed) >= 20 {
		prefix := trimmed[:10]
		if (strings.Count(prefix, ":") >= 1 && strings.Count(prefix, "/") >= 1) || strings.Contains(prefix, "T") {
			return true
		}
	}

	return false
}

func isReducerSamplePayload(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return false
	}

	switch v := decoded.(type) {
	case []any:
		for _, item := range v {
			if rec, ok := item.(map[string]any); ok {
				if _, hasNotice := rec["_result_reducer_notice"]; hasNotice {
					return true
				}
			}
		}
	case map[string]any:
		_, hasNotice := v["_result_reducer_notice"]
		return hasNotice
	}

	return false
}

// SetupStream initialises the NDJSON event writer for an AI chat response.
// It returns:
//   - sendEvent: the function to call for each event
//   - hasStreamedContent: returns true once any assistant text was sent via the
//     event stream (delta or non-streaming). Used by handleAIChat to decide
//     whether interpretOutput should re-emit the same text.
//
// This is used by both HQ (/api/ai/chat) and daemon (/api/ask).
func SetupStream(w http.ResponseWriter) (func(string, any), func() bool) {
	var contentStreamed bool

	// Set streaming headers immediately to prevent buffering
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	// CRITICAL: Write status header BEFORE first flush to initiate chunked encoding
	w.WriteHeader(http.StatusOK)

	// Flush headers immediately to establish streaming connection
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	writeEvent := func(eventType string, payload any) {
		switch eventType {
		case "record", "records", "preview", "result_stream":
			contentStreamed = true
		}

		if eventType == "records" || eventType == "record" || eventType == "preview" {
			b, _ := json.Marshal(payload)
			log.Debug("UI: streaming event", "type", eventType, "bytes", len(b))
		} else if eventType == "content" {
			s := fmt.Sprintf("%v", payload)
			log.Debug("UI: streaming content", "length", len(s))
		}

		log.Debug("UI: writing NDJSON event", "type", eventType, "payload_type", fmt.Sprintf("%T", payload))
		if err := json.NewEncoder(w).Encode(map[string]any{
			"type":    eventType,
			"payload": payload,
		}); err != nil {
			log.Error("Failed to encode NDJSON event", "error", err)
			return
		}

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	sendEvent := func(eventType string, payload any) {
		switch eventType {
		case "session_id":
			// Filter out session_id from client stream - it's for internal logging only
			return
		case "record", "records", "preview", "result_stream":
			contentStreamed = true
			writeEvent(eventType, payload)
			return
		case ai.ReasoningEventAssistantMessage:
			if message, ok := payload.(map[string]any); ok {
				if source, _ := message["source"].(string); strings.EqualFold(strings.TrimSpace(source), "reasoning_summary") {
					return
				}
				phase, _ := message["phase"].(string)
				streaming, _ := message["streaming"].(bool)
				if streaming && !strings.EqualFold(strings.TrimSpace(phase), "commentary") {
					return
				}
				if text, ok := message["text"].(string); ok && strings.TrimSpace(text) != "" {
					contentStreamed = true
					log.Info("UI: streaming assistant message", "length", len(text))
					writeEvent("content", text)
					return
				}
			}
			return
		case "content":
			contentStreamed = true
			log.Info("UI: streaming content", "length", len(fmt.Sprintf("%v", payload)))
			writeEvent(eventType, payload)
			return
		case "progress", "done":
			writeEvent(eventType, payload)
			return
		case "terminal_command":
			if payloadMap, ok := payload.(map[string]any); ok {
				if text, ok := payloadMap["text"].(string); ok && strings.TrimSpace(text) != "" {
					writeEvent(eventType, payloadMap)
					return
				}
			}
			if text, ok := payload.(string); ok && strings.TrimSpace(text) != "" {
				writeEvent(eventType, map[string]any{"text": text, "display_as_command": true})
				return
			}
			return
		case "terminal_output":
			if text, ok := payload.(string); ok && strings.TrimSpace(text) != "" {
				writeEvent(eventType, text)
				return
			}
			return
		case ai.ReasoningEventToolCall:
			// Send tool_call event to UI for spinner/loading indicator
			// But don't mark contentStreamed to avoid displaying raw event
			log.Info("Sending tool_call event", "payload", payload)
			writeEvent(eventType, payload)
			return
		case "tool_stream_chunk":
			// Stream command preview and output events separately so the UI can
			// show the command line with special styling before normal terminal output arrives.
			if chunkData, ok := payload.(map[string]any); ok {
				if text, ok := chunkData["text"].(string); ok && strings.TrimSpace(text) != "" {
					commandText := strings.TrimSpace(text)
					if strings.HasPrefix(commandText, "$ ") || strings.HasPrefix(commandText, "# ") {
						writeEvent("terminal_command", map[string]any{
							"text":               commandText,
							"display_as_command": true,
						})
						return
					}
					writeEvent("terminal_output", text)
					return
				}
			}
			return
		case "error":
			if errorText, ok := payload.(string); ok && strings.TrimSpace(errorText) != "" {
				contentStreamed = true
				writeEvent("content", fmt.Sprintf("⚠️ %s", strings.TrimSpace(errorText)))
				return
			}
			if errorText, ok := payload.(fmt.Stringer); ok {
				contentStreamed = true
				writeEvent("content", fmt.Sprintf("⚠️ %s", strings.TrimSpace(errorText.String())))
				return
			}
			if errorEvent, ok := payload.(map[string]any); ok {
				if errorMsg, _ := errorEvent["error"].(string); strings.TrimSpace(errorMsg) != "" {
					contentStreamed = true
					writeEvent("content", fmt.Sprintf("⚠️ %s", strings.TrimSpace(errorMsg)))
					return
				}
			}
		case ai.ReasoningEventToolError:
			// Stream tool errors immediately so user can see validation failures in real-time
			if errorEvent, ok := payload.(map[string]any); ok {
				if errorMsg, _ := errorEvent["error"].(string); strings.TrimSpace(errorMsg) != "" {
					contentStreamed = true
					message := sanitizeUserFacingErrorMessage(errorMsg)
					if message == "" {
						message = strings.TrimSpace(errorMsg)
					}
					// Format error for display
					tool, _ := errorEvent["tool"].(string)
					iteration, _ := errorEvent["iteration"].(int)
					var msg string
					if iteration > 0 {
						msg = fmt.Sprintf("⚠️ Error (iteration %d, tool: %s): %s\n", iteration, tool, message)
					} else {
						msg = fmt.Sprintf("⚠️ Error (tool: %s): %s\n", tool, message)
					}
					sanitizedEvent := map[string]any{}
					for key, value := range errorEvent {
						sanitizedEvent[key] = value
					}
					sanitizedEvent["error"] = message
					writeEvent("content", msg)
					// Also send the sanitized error event for UI to handle if needed
					writeEvent("tool_error", sanitizedEvent)
					return
				}
			}
		case ai.ReasoningEventToolResult:
			if result, ok := payload.(map[string]any); ok {
				writeEvent(eventType, payload)
				tool, _ := result["tool"].(string)
				if text, ok := result["result"].(string); ok && strings.TrimSpace(text) != "" {
					trimmed := strings.TrimSpace(text)
					if isReducerSamplePayload(trimmed) {
						return
					}
					if isStructuredToolResult(tool) {
						contentStreamed = true
						return
					}

					// For command execution tools, don't show output in chat - Terminal only
					// Let the LLM provide its own analysis instead of showing raw output
					isCommandTool := tool == "local_command" || tool == "execute_local_command"
					if isCommandTool {
						contentStreamed = true
						return
					}

					// For other tools, show first 5 lines
					lines := strings.Split(trimmed, "\n")
					highlight := lines
					truncated := false

					if len(lines) > 5 {
						highlight = lines[:5]
						truncated = true
					}

					contentStreamed = true
					summary := strings.Join(highlight, "\n")
					if truncated {
						summary += "\n... (output truncated)"
					}
					formatted := fmt.Sprintf("🛠️ %s:\n%s", tool, renderVisibleText(summary))
					writeEvent("content", formatted)
				}
			}
			return
		default:
			// Silently ignore unknown events - don't display raw event data in UI
			return
		}

		// This fallback should never be reached due to default case
		// writeEvent(eventType, payload)
	}

	return sendEvent, func() bool { return contentStreamed }
}

func resolveContextAndAgent(ctx context.Context, req *aiChatRequest, sendEvent func(string, any)) (ai.Agent[map[string]any], error) {
	if req.Database != "" {
		if _, err := GetDBOptions(ctx, req.Database); err != nil {
			msg := fmt.Sprintf("Invalid database '%s': %v", req.Database, err)
			log.Info("Response: Invalid Database", "error", msg)
			sendEvent("error", msg)
			return nil, err
		}
	}
	if req.Agent == "" {
		req.Agent = "copilot"
	}
	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}
	blueprint, exists := loadedAgents[req.Agent]
	if !exists {
		msg := fmt.Sprintf("Agent '%s' is not initialized or not found.", req.Agent)
		log.Info("Response: Agent Not Found", "error", msg)
		sendEvent("error", msg)
		return nil, fmt.Errorf("agent not found")
	}
	return blueprint, nil
}

func lockSession(req *aiChatRequest, blueprint ai.Agent[map[string]any]) (ai.Agent[map[string]any], *sync.Mutex) {
	agentSvc, sessionMu := activeSessions.GetOrCreate(req.SessionID, func() ai.Agent[map[string]any] {
		if cloneable, ok := blueprint.(interface {
			Clone() ai.Agent[map[string]any]
		}); ok {
			log.Debug("Cloned new pristine agent instance for session", "session_id", req.SessionID)
			return cloneable.Clone()
		}
		return blueprint
	})
	return agentSvc, sessionMu
}

func InjectSharedRuntimeContext(ctx context.Context, sendEvent func(string, any), w http.ResponseWriter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, ai.CtxKeyExecutor, &DefaultToolExecutor{Agents: loadedAgents})
	if sendEvent != nil {
		ctx = context.WithValue(ctx, ai.CtxKeyProgressSink, func(msg string) {
			sendEvent("content", msg+"\n")
		})
		ctx = context.WithValue(ctx, ai.CtxKeyEventStreamer, sendEvent)
		ctx = context.WithValue(ctx, ai.CtxKeyResultStreamer, agent.NewEventResultStreamer(sendEvent))
	} else {
		ctx = context.WithValue(ctx, ai.CtxKeyProgressSink, func(string) {})
		ctx = context.WithValue(ctx, ai.CtxKeyEventStreamer, func(string, any) {})
	}
	if w != nil {
		ctx = context.WithValue(ctx, agent.CtxKeyJSONStreamer, agent.NewNDJSONStreamer(&DirectFlushingWriter{w: w}))
	}
	if sendEvent != nil {
		registerLTMPressureUIHook(sendEvent)
	}
	return ctx
}

func constructPayload(ctx context.Context, w http.ResponseWriter, req *aiChatRequest, llmSettings llmSettings, sendEvent func(string, any), rs *agent.RunnerSession) (context.Context, *ai.SessionPayload, *ai.ConfigMap, string) {
	if req.Provider != "" {
		ctx = context.WithValue(ctx, ai.CtxKeyProvider, req.Provider)
	}
	ctx = context.WithValue(ctx, agent.RunnerSessionKey, rs)
	ctx = InjectSharedRuntimeContext(ctx, sendEvent, w)

	cfg := ai.NewConfigMap()

	// Add LLM settings as ProviderDetails struct for the agent
	if llmSettings.Provider != "" || llmSettings.APIKey != "" {
		providerDetails := &agent.ProviderDetails{
			Provider: llmSettings.Provider,
			Model:    llmSettings.Model,
			APIKey:   llmSettings.APIKey,
			BaseURL:  llmSettings.URL,
		}
		cfg.Set("provider_details", providerDetails)
	}

	if req.Database != "" {
		cfg.Set("database", req.Database)
	}
	if trimmedThinkingLevel := strings.TrimSpace(req.ThinkingLevel); trimmedThinkingLevel != "" {
		cfg.Set("thinking_level", trimmedThinkingLevel)
	}
	format := req.Format
	if format == "" {
		format = "csv"
	}
	cfg.Set("default_format", format)

	databases := make(map[string]any)
	for _, dbCfg := range config.Databases {
		if opts, err := GetDBOptions(ctx, dbCfg.Name); err == nil {
			databases[dbCfg.Name] = opts
		}
	}
	if req.Database != "" {
		if _, exists := databases[req.Database]; !exists {
			if opts, err := GetDBOptions(ctx, req.Database); err == nil {
				databases[req.Database] = opts
			}
		}
	}

	var avatarScope string
	for i, kbRef := range req.SelectedKBs {
		if req.SelectedKBs[i].DatabaseName == "" {
			// Fallback to request's main database if DBName is not provided in reference
			req.SelectedKBs[i].DatabaseName = req.Database
		}

		resolvedOpts, err := GetDBOptions(ctx, req.SelectedKBs[i].DatabaseName)
		if err == nil {
			// Use the resolved options to check for Avatar exclusivity
			if resolvedOpts.Type >= 0 {
				db := aidb.NewDatabase(resolvedOpts)
				trans, _ := db.BeginTransaction(ctx, sop.ForReading)
				if trans != nil {
					kb, err := db.OpenKnowledgeBase(ctx, kbRef.Name, trans, nil, nil, false, true)
					if err == nil {
						cfg, err := kb.GetConfig(ctx)
						if err == nil && cfg != nil && cfg.IsExclusive {
							avatarScope = kbRef.Name
							log.Info("[Copilot] Avatar Mode Active. LLM Sandboxed to: " + kbRef.Name)
						}
					}
					trans.Rollback(ctx)
					if avatarScope != "" {
						break
					}
				}
			}
		}
	}

	if strings.TrimSpace(req.UserID) == "" {
		req.UserID = strings.TrimSpace(sop.GetAuthFromContext(ctx).UserID)
	}

	payload := &ai.SessionPayload{
		CurrentDB:        req.Database,
		CurrentUserQuery: req.Message,
		ActiveDomain:     req.Domain,
		SelectedKBs:      req.SelectedKBs,
		UserID:           req.UserID,
		ClientID:         req.ClientID,
		SessionID:        req.SessionID,
		AvatarScope:      avatarScope,
		Variables:        map[string]any{},
	}
	if len(req.InputParts) > 0 {
		payload.Variables["input_parts"] = req.InputParts
	}
	cfg.Set("payload", payload)

	if req.Database != "" {
		obfuscation.GlobalObfuscator.RegisterResource(req.Database, "DB")
	}
	if req.StoreName != "" {
		obfuscation.GlobalObfuscator.RegisterResource(req.StoreName, "STORE")
	}

	fullMessage := strings.TrimSpace(req.Message)
	if fullMessage == "" {
		fullMessage = req.Message
	}

	ctx = context.WithValue(ctx, "session_payload", payload)

	return ctx, payload, cfg, fullMessage
}

func executeAgentLifecycle(ctx context.Context, r *http.Request, agentSvc ai.Agent[map[string]any], req *aiChatRequest, sendEvent func(string, any)) error {
	if sysOpts, err := GetSystemDBOptions(ctx); err == nil {
		sysDB := aidb.NewDatabase(sysOpts)
		kbName := memory.BuildLTMStoreName(ai.AgentIDOmni, req.UserID)
		trans, _ := sysDB.BeginTransaction(ctx, sop.ForWriting)
		if trans != nil {
			dbEmbedder := GetConfiguredEmbedder(nil)
			dbLLM := GetConfiguredLLM(r)
			sysDB.OpenKnowledgeBase(ctx, kbName, trans, dbLLM, dbEmbedder, false, true)
			trans.Commit(ctx)
			go seedMetaCognitionAsync(req.UserID, kbName, sysOpts)
		}
	}

	if err := agentSvc.Open(ctx); err != nil {
		msg := fmt.Sprintf("Agent '%s' failed to open session: %v", req.Agent, err)
		log.Error("Response: Session Open Failed", "error", msg)
		sendEvent("error", msg)
		return err
	}
	return nil
}

var uuidLikePattern = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)

func sanitizeUserFacingErrorMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}

	msg = uuidLikePattern.ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "unknown tool"), strings.Contains(lower, "not found in any registered agent"), strings.Contains(lower, "tool not found"), strings.Contains(lower, "no executor available"):
		return "could not find the requested tool"
	case strings.Contains(lower, "no session payload found"):
		return "session context is unavailable"
	case strings.Contains(lower, "database not found"):
		return "the requested database is not available"
	case strings.Contains(lower, "request canceled"):
		return "the request was canceled"
	default:
		return msg
	}
}

func formatUserFacingAgentError(agentName string, err error) string {
	if err == nil {
		return fmt.Sprintf("agent %q could not complete the request", agentName)
	}

	msg := sanitizeUserFacingErrorMessage(err.Error())
	if msg == "" {
		return fmt.Sprintf("agent %q could not complete the request", agentName)
	}
	return msg
}

func executeRAG(ctx context.Context, agentSvc ai.Agent[map[string]any], req *aiChatRequest, fullMessage string, cfg *ai.ConfigMap, sendEvent func(string, any)) (string, error) {
	response, err := agentSvc.Ask(ctx, fullMessage, cfg)
	if err != nil {
		msg := fmt.Sprintf("Agent '%s' failed: %s", req.Agent, formatUserFacingAgentError(req.Agent, err))
		log.Error("Response: Agent Ask Failed", "error", msg)
		sendEvent("error", msg)
		return "", err
	}
	return response, nil
}

func interpretOutput(response string, sendEvent func(string, any)) {
	text := strings.TrimSpace(response)

	if idxStart := strings.Index(text, "<<<ACTION_REQUIRED:"); idxStart >= 0 {
		if idxEnd := strings.LastIndex(text, ">>>"); idxEnd > idxStart {
			candidate := text[idxStart+len("<<<ACTION_REQUIRED:") : idxEnd]
			var actionPayload map[string]any
			if err := json.Unmarshal([]byte(candidate), &actionPayload); err == nil {
				log.Info("Response: Action Required payload detected")
				sendEvent("action_required", actionPayload)
				return
			}
		}
	}

	var toolCall struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	cleanText := strings.TrimPrefix(text, "```json")
	cleanText = strings.TrimPrefix(cleanText, "```")
	cleanText = strings.TrimSuffix(cleanText, "```")
	cleanText = strings.TrimSpace(cleanText)

	if err := json.Unmarshal([]byte(cleanText), &toolCall); err == nil && toolCall.Tool != "" {
		log.Debug(fmt.Sprintf("Agent returned raw tool call (unexpected): %s", toolCall.Tool))
		msg := fmt.Sprintf("Agent attempted to call tool '%s' but failed to execute it internally.", toolCall.Tool)
		log.Info("Response: Raw Tool Call", "response", msg)
		sendEvent("content", msg)
		return
	}

	var rawRecords []json.RawMessage
	var mapRecords []map[string]any
	if err := json.Unmarshal([]byte(cleanText), &mapRecords); err == nil && len(mapRecords) > 0 {
		if err := json.Unmarshal([]byte(cleanText), &rawRecords); err == nil {
			log.Debug("Response: Detected JSON Records", "count", len(rawRecords))
			for _, rec := range rawRecords {
				sendEvent("record", rec)
			}
			return
		}
	}

	if idxStart := strings.Index(cleanText, "["); idxStart >= 0 {
		if idxEnd := strings.LastIndex(cleanText, "]"); idxEnd > idxStart {
			candidate := cleanText[idxStart : idxEnd+1]
			var embeddedMapRecords []map[string]any
			if err := json.Unmarshal([]byte(candidate), &embeddedMapRecords); err == nil && len(embeddedMapRecords) > 0 {
				if err := json.Unmarshal([]byte(candidate), &rawRecords); err == nil {
					log.Debug("Response: Detected Embedded JSON Records", "count", len(rawRecords))
					for _, rec := range rawRecords {
						sendEvent("record", rec)
					}
					return
				}
			}
		}
	}

	var singleRecord map[string]any
	if strings.HasPrefix(cleanText, "{") {
		if err := json.Unmarshal([]byte(cleanText), &singleRecord); err == nil {
			log.Debug("Response: Detected Single JSON Record")
			sendEvent("record", singleRecord)
			return
		}
	}

	lines := strings.Split(cleanText, "\n")
	var ndjsonEvents []map[string]any
	isNDJSON := false

	if len(lines) > 0 {
		validCount := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var obj map[string]any
			if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &obj) == nil {
				ndjsonEvents = append(ndjsonEvents, obj)
				validCount++
			} else {
				isNDJSON = false
				break
			}
		}
		if validCount > 0 && len(ndjsonEvents) == validCount {
			isNDJSON = true
		}
	}

	if isNDJSON {
		log.Debug("Response: Detected NDJSON Stream", "count", len(ndjsonEvents))
		for _, evt := range ndjsonEvents {
			rawType, _ := evt["type"].(string)

			switch rawType {
			case "record":
				if rec, ok := evt["record"]; ok {
					sendEvent("record", rec)
				} else if rec, ok := evt["data"]; ok {
					sendEvent("record", rec)
				}
			case "log":
				msg, _ := evt["message"].(string)
				sendEvent("content", fmt.Sprintf("> *Log:* %s\n", msg))
			case "error":
				msg, _ := evt["error"].(string)
				sendEvent("error", msg)
			case "step_start":
				name, _ := evt["name"].(string)
				if name == "" {
					name, _ = evt["command"].(string)
				}
				stepIndexVal := evt["step_index"]

				var stepIndex int
				if f, ok := stepIndexVal.(float64); ok {
					stepIndex = int(f)
				} else if i, ok := stepIndexVal.(int); ok {
					stepIndex = i
				}

				if stepIndex > 0 {
					sendEvent("content", fmt.Sprintf("**Step %d:** `%s`\n", stepIndex, name))
				} else {
					sendEvent("content", fmt.Sprintf("**Step:** `%s`\n", name))
				}
			case "outputs":
				if list, ok := evt["outputs"].([]any); ok {
					for _, item := range list {
						sendEvent("record", item)
					}
				} else {
					b, _ := json.MarshalIndent(evt["outputs"], "", "  ")
					sendEvent("content", fmt.Sprintf("```json\n%s\n```", string(b)))
				}
			default:
				sendEvent("record", evt)
			}
		}
		return
	}

	if response != "" {
		sendEvent("content", response)
	}
}

func initAgents(ctx context.Context) error {
	return loadAgent(ctx, "copilot", "ai/data/copilot_pipeline.json")
}

func seedDefaultScripts(ctx context.Context, db *aidb.Database) {
	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		log.Error(fmt.Sprintf("Failed to begin transaction for seeding scripts: %v", err))
		return
	}
	store, err := db.OpenModelStore(ctx, "scripts", tx)
	if err != nil {
		tx.Rollback(ctx)
		log.Error(fmt.Sprintf("Failed to open scripts store: %v", err))
		return
	}

	// Check if demo_loop exists
	var existing ai.Script
	if err := store.Load(ctx, "general", "demo_loop", &existing); err == nil {
		tx.Rollback(ctx)
		return // Already exists
	}

	// Create demo_loop script
	demoLoop := ai.Script{
		Description: "Demonstrates loops and variables",
		Steps: []ai.ScriptStep{
			{
				Type:     "set",
				Variable: "items",
				Value:    "apple\nbanana\ncherry",
			},
			{
				Type:     "loop",
				List:     "items",
				Iterator: "fruit",
				Steps: []ai.ScriptStep{
					{
						Type:    "say",
						Message: "Processing {{.fruit}}...",
					},
					{
						Type:   "ask",
						Prompt: "What color is {{.fruit}}? (Answer in 1 word)",
					},
				},
			},
		},
	}

	if err := store.Save(ctx, "general", "demo_loop", demoLoop); err != nil {
		log.Error(fmt.Sprintf("Failed to save demo_loop script: %v", err))
		tx.Rollback(ctx)
		return
	}

	tx.Commit(ctx)
	log.Info("Seeded 'demo_loop' script.")
}

func loadAgent(ctx context.Context, key, configPath string) error {
	// ctx is passed in

	// Try to find the file in common locations
	cwd, _ := os.Getwd()
	pathsToTry := []string{
		configPath,
		filepath.Join(cwd, configPath),
		filepath.Join(cwd, "..", configPath),
		filepath.Join(cwd, "..", "..", configPath),
		filepath.Join(cwd, "..", "..", "..", configPath),
		filepath.Join(cwd, "..", "..", "..", "lib", "sop", configPath),
		filepath.Join("/Volumes/T7/sop-cloud/lib/sop", configPath),
		filepath.Join("/Volumes/T7/sop-cloud", configPath),
		filepath.Join("/Volumes/T7/sop", configPath),
		filepath.Join("/Users/grecinto/sop", configPath), // Absolute fallback
	}

	var foundPath string
	for _, p := range pathsToTry {
		if _, err := os.Stat(p); err == nil {
			foundPath = p
			break
		}
	}

	if foundPath == "" {
		return fmt.Errorf("agent config not found: %s (searched: %v)", configPath, pathsToTry)
	}

	cfg, err := agent.LoadConfigFromFile(foundPath)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to load agent config %s: %v", foundPath, err))
		return err
	}

	// Apply Global config (LLM API Key, text embedders, etc)
	injectGlobalConfig(cfg, &config)

	// Apply Global Obfuscation Mode if specified in HTTP Config
	// We do NOT update the agent config anymore, instead we calculate the per-database flag below
	globalObfMode := ObfuscationDisabled
	if config.ObfuscationMode != "" {
		log.Info("Applying Global Obfuscation Mode from HTTP config", "mode", config.ObfuscationMode)
		globalObfMode = ObfuscationMode(config.ObfuscationMode)
	}

	// Ensure absolute path for storage
	if cfg.StoragePath != "" {
		if !filepath.IsAbs(cfg.StoragePath) {
			configDir := filepath.Dir(foundPath)
			cfg.StoragePath = filepath.Join(configDir, cfg.StoragePath)
		}
		if absPath, err := filepath.Abs(cfg.StoragePath); err == nil {
			cfg.StoragePath = absPath
		}
	}

	log.Debug(fmt.Sprintf("Initializing AI Agent: %s (%s)...", cfg.Name, cfg.ID))

	// Initialize System DB
	sysOpts, err := GetSystemDBOptions(ctx)
	var sysDB *aidb.Database
	if err == nil {
		pathExists := true
		if len(sysOpts.StoresFolders) > 0 {
			if _, statErr := os.Stat(sysOpts.StoresFolders[0]); os.IsNotExist(statErr) {
				pathExists = false
			}
		}
		if pathExists {
			sysDB = aidb.NewDatabase(sysOpts)
			// Seed default scripts for testing
			seedDefaultScripts(ctx, sysDB)
		} else {
			log.Warn("System DB path does not exist. Skipping AI Agent seeding to prevent auto-creation.")
		}
	} else {
		log.Debug(fmt.Sprintf("System DB not available for agent %s: %v", cfg.ID, err))
	}

	// Prepare Databases map
	databases := make(map[string]sop.DatabaseOptions)

	// Add System DB if available
	if sysDB != nil {
		databases[agent.SystemDBName] = sysOpts
	}

	for _, dbCfg := range config.Databases {
		// We need to use a copy of dbCfg because getDBOptionsFromConfig takes a pointer
		// and loop variable reuse might be an issue in older Go versions, though fixed in 1.22.
		// Safe to just pass address.
		d := dbCfg
		opts, err := GetDBOptionsFromConfig(ctx, &d)
		if err == nil {
			// Calculate Obfuscation Flag based on Global Mode and Per-DB Config
			switch globalObfMode {
			case ObfuscationAllDatabases:
				opts.EnableObfuscation = true
			case ObfuscationDisabled:
				opts.EnableObfuscation = false
			case ObfuscationPerDatabase:
				opts.EnableObfuscation = dbCfg.EnableObfuscation
			default:
				// Fallback to Disabled if unknown
				opts.EnableObfuscation = false
			}

			databases[d.Name] = opts
		}
	}

	registry := make(map[string]ai.Agent[map[string]any])

	// Helper to initialize an agent from a config
	initAgent := func(agentCfg agent.Config) (ai.Agent[map[string]any], error) {
		if agentCfg.StoragePath != "" {
			if !filepath.IsAbs(agentCfg.StoragePath) {
				configDir := filepath.Dir(foundPath)
				agentCfg.StoragePath = filepath.Join(configDir, agentCfg.StoragePath)
			}
			if absPath, err := filepath.Abs(agentCfg.StoragePath); err == nil {
				agentCfg.StoragePath = absPath
			}
		}
		svc, err := agent.NewFromConfig(ctx, agentCfg, agent.Dependencies{
			AgentRegistry: registry,
			SystemDB:      sysDB,
			Databases:     databases,
		})
		return svc, err
	}

	// Pre-register internal policy agents
	for _, pCfg := range cfg.Policies {
		if pCfg.ID != "" {
			registry[pCfg.ID] = agent.NewPolicyAgent(pCfg.ID, nil, nil)
		}
	}

	// Register locally defined agents
	for _, localAgentCfg := range cfg.Agents {
		if localAgentCfg.ID == "" {
			continue
		}
		if _, exists := registry[localAgentCfg.ID]; exists {
			continue
		}

		// Dynamic Generator Override based on Environment Variables
		// This allows the "sql_core" agent to switch between Ollama/Gemini/GPT without changing the JSON file.
		if localAgentCfg.Generator.Type != "" {
			// Check for explicit override from UI (passed via context or global config?)
			// For now, we stick to Env Vars as the "Configuration" source.
			// But to allow runtime switching, we might need to re-initialize the agent or pass the provider in the Ask() call.
			// Since Ask() is generic, we can't easily pass it there without changing the interface.
			// However, we can make the Generator itself memory!

			provider := os.Getenv("AI_PROVIDER")
			if provider != "" {
				log.Debug(fmt.Sprintf("Overriding generator for agent %s to %s", localAgentCfg.ID, provider))
				localAgentCfg.Generator.Type = provider
				localAgentCfg.Generator.Options = make(map[string]any) // Clear options to rely on env vars
			}
		}

		log.Debug(fmt.Sprintf("Initializing local agent: %s...", localAgentCfg.ID))
		svc, err := initAgent(localAgentCfg)
		if err != nil {
			log.Debug(fmt.Sprintf("Failed to initialize local agent %s: %v", localAgentCfg.ID, err))
			continue
		}
		registry[localAgentCfg.ID] = svc
		loadedAgents[localAgentCfg.ID] = svc
	}

	// Initialize the main agent
	mainAgent, err := agent.NewFromConfig(ctx, *cfg, agent.Dependencies{
		AgentRegistry: registry,
		SystemDB:      sysDB,
		Databases:     databases,
	})
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to initialize main agent %s: %v", key, err))
		return err
	}

	// Enable History Injection (Manual Override as requested)
	if svc, ok := mainAgent.(*agent.Service); ok {
		log.Info("Enabling History Injection for agent", "agent", key)
		svc.SetFeature("history_injection", true)
	}

	loadedAgents[key] = mainAgent
	log.Debug(fmt.Sprintf("Agent '%s' initialized successfully.", key))
	return nil
}

// DefaultToolExecutor implements ai.ToolExecutor by delegating to registered agents.
type DefaultToolExecutor struct {
	Agents map[string]ai.Agent[map[string]any]
}

func (e *DefaultToolExecutor) Execute(ctx context.Context, tool string, args map[string]any) (string, error) {
	if e == nil {
		return "", fmt.Errorf("tool '%s' not found or no executor available", tool)
	}

	if agentSvc, ok := e.Agents["copilot"]; ok {
		if executor, ok := agentSvc.(interface {
			Execute(context.Context, string, map[string]any) (string, error)
		}); ok {
			if resp, err := executor.Execute(ctx, tool, args); err == nil || !strings.Contains(err.Error(), "unknown tool") {
				return resp, err
			}
		}
	}

	for _, agentSvc := range e.Agents {
		if executor, ok := agentSvc.(interface {
			Execute(context.Context, string, map[string]any) (string, error)
		}); ok {
			if resp, err := executor.Execute(ctx, tool, args); err == nil || !strings.Contains(err.Error(), "unknown tool") {
				return resp, err
			}
		}
	}

	return "", fmt.Errorf("tool '%s' not found or no executor available", tool)
}

func injectGlobalConfig(cfg *agent.Config, globalCfg *Config) {
	if cfg == nil || globalCfg == nil {
		return
	}

	// The global config no longer owns AI secret/provider fields. Keep runtime
	// configuration in the agent config itself and only recurse into nested agents.
	// Recursive injection
	for i := range cfg.Agents {
		// We need to take address of slice element to modify it
		// But cfg.Agents is []Config.
		// wait, Config struct has []Config.
		// We can't easily recurse efficiently on value type slices in Go without pointers or rewriting the slice.
		// cfg.Agents is []Config (values).
		subCfg := &cfg.Agents[i]
		injectGlobalConfig(subCfg, globalCfg)
	}
}

func (e *DefaultToolExecutor) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	if e == nil {
		return nil, nil
	}

	if agentSvc, ok := e.Agents["copilot"]; ok {
		if te, ok := agentSvc.(interface {
			ListTools(context.Context) ([]ai.ToolDefinition, error)
		}); ok {
			tools, err := te.ListTools(ctx)
			if err != nil {
				return nil, err
			}
			if len(tools) > 0 {
				return tools, nil
			}
		}
	}

	for _, agentSvc := range e.Agents {
		if te, ok := agentSvc.(interface {
			ListTools(context.Context) ([]ai.ToolDefinition, error)
		}); ok {
			tools, err := te.ListTools(ctx)
			if err != nil {
				return nil, err
			}
			if len(tools) > 0 {
				return tools, nil
			}
		}
	}

	log.Info("DefaultToolExecutor.ListTools: no tools returned", "agent_count", len(e.Agents))
	return nil, nil
}

func handleAIFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		MsgID       string `json:"msgId"`
		Type        string `json:"type"` // positive, negative
		AIContent   string `json:"ai_content"`
		UserContent string `json:"user_content"`
		Database    string `json:"database"`
		Agent       string `json:"agent"`
		UserID      string `json:"user_id"`
		SessionID   string `json:"session_id"`
		// KB Quality fields (optional)
		KBName   string `json:"kb_name"`
		TaskType string `json:"task_type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	log.Info("AI Feedback Received", "msgId", req.MsgID, "type", req.Type)

	ctx := r.Context()

	// 1. Get System DB Options
	// We store feedback in the 'system' database to be global across user sessions/databases
	opts, err := GetDBOptions(ctx, "system")
	if err != nil {
		log.Error("Failed to get system db options", "error", err)
		http.Error(w, "System DB error", http.StatusInternalServerError)
		return
	}

	// 2. Begin Transaction
	trans, err := database.BeginTransaction(ctx, opts, sop.ForWriting)
	if err != nil {
		log.Error("Failed to begin transaction", "error", err)
		http.Error(w, "DB trans error", http.StatusInternalServerError)
		return
	}
	// Ensure rollback if not committed
	defer trans.Rollback(ctx)

	kbName := memory.BuildLTMStoreName(ai.AgentIDOmni, req.UserID)
	if req.UserID == "" {
		kbName = "llm_feedback"
	}

	// 3. Open Store "llm_feedback"
	storeName := "llm_feedback"
	// Simple string comparer
	comparer := func(a, b string) int { return strings.Compare(a, b) }

	// Configure store (auto-create if missing)
	so := sop.ConfigureStore(storeName, true, btree.DefaultSlotLength, "LLM Feedback Store", sop.SmallData, "")

	store, err := database.NewBtree[string, string](ctx, opts, storeName, trans, comparer, so)
	if err != nil {
		log.Error("Failed to open feedback store", "error", err)
		http.Error(w, "Store open error", http.StatusInternalServerError)
		return
	}

	// 4. Create Payload
	id := uuid.New().String()
	payload := map[string]any{
		// "id" removed - it is the Key
		"msg_id":       req.MsgID,
		"type":         req.Type,
		"ai_content":   req.AIContent,
		"user_content": req.UserContent,
		"database":     req.Database,
		"agent":        req.Agent,
		"timestamp":    time.Now().Format(time.RFC3339),
	}

	valBytes, _ := json.Marshal(payload)
	val := string(valBytes)

	// 5. Save (Add new record) to B-Tree
	if ok, err := store.Add(ctx, id, val); err != nil {
		log.Error("Failed to save feedback", "error", err)
		http.Error(w, "Save error", http.StatusInternalServerError)
		return
	} else if !ok {
		// Collision? Retry or just fail (UUID collision unlikely)
		http.Error(w, "Save failed (collision)", http.StatusInternalServerError)
		return
	}

	// 6. Vectorize & Save to Memory KnowledgeBase (if user content is present)
	if req.UserContent != "" {
		// Create Embedder (Simple Hash for now, ideally use Agent's Embedder)
		// 384 dimensions is good balance
		embedder := GetConfiguredEmbedder(nil)

		vecs, err := embed.DocumentTexts(ctx, embedder, []string{req.UserContent})
		if err != nil {
			log.Warn("Failed to embed user content", "error", err)
			// Don't fail the request, just skip vectorization
		} else if len(vecs) > 0 {
			sysDB := aidb.NewDatabase(opts)
			kb, err := sysDB.OpenKnowledgeBase(ctx, kbName, trans, nil, embedder, false, true)
			if err != nil {
				log.Error("Failed to open physical knowledge base", "error", err)
				http.Error(w, "Knowledge base open error", http.StatusInternalServerError)
				return
			}

			if err := kb.IngestThought(ctx, req.UserContent, "Feedback", "System", vecs[0], map[string]any{"data": val}); err != nil {
				log.Error("Failed to ingest memory feedback", "error", err)
				http.Error(w, "Memory ingest error", http.StatusInternalServerError)
				return
			}
			log.Info("Feedback ingested to active memory", "id", id)
		}
	}

	// 6.5. Record KB Effectiveness (if KB metadata present)
	if req.KBName != "" {
		taskType := strings.ToLower(strings.TrimSpace(req.TaskType))
		if taskType == "" {
			taskType = "general"
		}
		success := req.Type == "positive"

		// Get KB effectiveness tracker
		if tracker := GetKBEffectivenessTracker(); tracker != nil {
			// Use compliance score of 1.0 for positive feedback, 0.0 for negative
			complianceScore := 0.0
			if success {
				complianceScore = 1.0
			}
			tracker.RecordOutcome(taskType, req.KBName, success, complianceScore)
			log.Info("KB effectiveness recorded", "kb", req.KBName, "task", taskType, "success", success)
		}
	}

	// 7. Commit
	if err := trans.Commit(ctx); err != nil {
		log.Error("Failed to commit feedback", "error", err)
		http.Error(w, "Commit error", http.StatusInternalServerError)
		return
	}

	log.Info("Feedback Saved", "id", id)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": id})
}

// handleCloseSession allows explicitly closing an active session.
func handleCloseSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "query param session_id is required", http.StatusBadRequest)
		return
	}

	activeSessions.Close(sessionID)
	log.Info("Closed session explicitly", "session_id", sessionID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success", "message":"Session closed successfully"}`))
}

func handleToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Tool    string `json:"tool"`
		Text    string `json:"text"`
		Intent  string `json:"intent"`
		Context string `json:"context"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("Invalid JSON body", "error", err)
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	agentSvc, exists := loadedAgents["copilot"]
	if !exists {
		http.Error(w, "Default AI agent not configured", http.StatusInternalServerError)
		return
	}

	switch req.Tool {
	case "enhance_text":
		prompt := fmt.Sprintf("Can you help me creatively enhance this text/prompt config for a %s?\n\n", strings.ReplaceAll(req.Intent, "_", " "))
		if req.Context != "" {
			prompt += fmt.Sprintf("Context / Details about the project:\n%s\n\n", req.Context)
		}
		if req.Intent == "system_prompt" {
			prompt += "IMPORTANT INSTRUCTION: The user is writing a System Prompt for an AI that interacts with a specific Knowledge Base.\n"
			prompt += "Please expand their text into a robust set of instructions that tells the AI exactly HOW to interact with, search, and synthesize the data in this knowledge base, ensuring it grounds its answers in the data.\n\n"
		}
		prompt += fmt.Sprintf("Here is what the user has drafted so far:\n%s\n\nPlease improve it into a professional, clear, and comprehensive format. Return ONLY the improved text, no conversational filler or markdown code block formatting.", req.Text)

		ctx := context.Background()

		if err := agentSvc.Open(ctx); err != nil {
			log.Error("Failed to open agent session", "error", err)
			http.Error(w, "Failed to initialize AI session", http.StatusInternalServerError)
			return
		}
		defer agentSvc.Close(ctx)

		cfg := ai.NewConfigMap()
		response, err := agentSvc.Ask(ctx, prompt, cfg)
		if err != nil {
			log.Error("Failed to ask agent", "error", err)
			http.Error(w, "Failed to generate text", http.StatusInternalServerError)
			return
		}

		response = strings.TrimSpace(response)
		response = strings.TrimPrefix(response, "```xml")
		response = strings.TrimPrefix(response, "```markdown")
		response = strings.TrimPrefix(response, "```json")
		response = strings.TrimPrefix(response, "```")
		response = strings.TrimSuffix(response, "```")
		response = strings.TrimSpace(response)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"enhanced_text": response,
		})
	default:
		http.Error(w, "Unknown tool", http.StatusBadRequest)
	}
}
