package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"regexp"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/tools/sopbackend"
)

type bridgeAskRequest struct {
	SessionID    string
	UserPrompt   string
	FullPrompt   string
	ProviderName string
	ModelName    string
	Variables    map[string]any
}

type inferredOpenAICompatibleEditorContext struct {
	WorkspaceRoot string
	ActiveFile    string
	SelectedText  string
	OpenFiles     []string
}

type bridgeStreamEvent struct {
	Type    string
	Payload any
}

type bridgeChatEnvelope struct {
	Ask           bridgeAskRequest
	Stream        bool
	ResponseModel string
	DefaultAnswer string
	InitialEvents []bridgeStreamEvent
}

type bridgeAskInvoker func(context.Context, bridgeAskRequest, func(string, any)) (string, error)

type bridgeChatTransport interface {
	DecodeChatRequest(r *http.Request) (bridgeChatEnvelope, error)
	StreamChat(w http.ResponseWriter, envelope bridgeChatEnvelope, ask bridgeAskInvoker)
	WriteChatResponse(w http.ResponseWriter, envelope bridgeChatEnvelope, answer string)
}

type continueToolHandler func(continueToolRequest) (any, error)

type bridgeRoute struct {
	Pattern         string
	RequiresService bool
	Handler         http.HandlerFunc
}

type bridgeClientAdapter interface {
	Routes(*localRAGService) []bridgeRoute
}

type bridgeModelDescriptor struct {
	ID       string
	OwnedBy  string
	Metadata map[string]any
}

type continueWorkflowProfile struct {
	ID               string
	Label            string
	Description      string
	TaskType         string
	DefaultPhasePlan []string
	ModeHints        []string
	Guidance         string
}

type continueAgentExecuteRequest struct {
	SessionID string `json:"session_id"`
	Task      string `json:"task"`
	Context   struct {
		WorkspaceRoot string   `json:"workspace_root"`
		ActiveFile    string   `json:"active_file"`
		SelectedText  string   `json:"selected_text"`
		OpenFiles     []string `json:"open_files"`
	} `json:"context"`
	Mode string `json:"mode"`
}

type continueSessionSnapshot struct {
	SessionID       string   `json:"session_id"`
	Status          string   `json:"status"`
	Phase           string   `json:"phase,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	WorkflowProfile string   `json:"workflow_profile,omitempty"`
	TaskType        string   `json:"task_type,omitempty"`
	Message         string   `json:"message,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	OpenFiles       []string `json:"open_files,omitempty"`
	ActiveFile      string   `json:"active_file,omitempty"`
	WorkspaceRoot   string   `json:"workspace_root,omitempty"`
}

type continueWorkerRegistrationRequest struct {
	SessionID    string   `json:"session_id"`
	CallbackURL  string   `json:"callback_url"`
	Capabilities []string `json:"capabilities,omitempty"`
	IDE          string   `json:"ide,omitempty"`
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

var continueWorkerCapabilityCatalog = []string{"read_context", "apply_patch", "edit_code", "diff_review", "plan_changes"}

type continueBridgeTransport struct{}

type openAICompatibleBridgeTransport struct{}

type continueBridgeAdapter struct {
	transport continueBridgeTransport
}

type openAICompatibleBridgeAdapter struct {
	transport openAICompatibleBridgeTransport
}

func newContinueBridgeSessionStore() *continueBridgeSessionStore {
	return &continueBridgeSessionStore{sessions: map[string]continueSessionSnapshot{}}
}

func newContinueBridgeWorkerStore() *continueBridgeWorkerStore {
	return &continueBridgeWorkerStore{workers: map[string]continueWorkerRegistration{}}
}

func (s *localRAGService) ensureContinueSessionStore() *continueBridgeSessionStore {
	if s == nil {
		return newContinueBridgeSessionStore()
	}
	if s.continueState == nil {
		s.continueState = newContinueBridgeSessionStore()
	}
	return s.continueState
}

func (s *localRAGService) ensureContinueWorkerStore() *continueBridgeWorkerStore {
	if s == nil {
		return newContinueBridgeWorkerStore()
	}
	if s.continueWorkers == nil {
		s.continueWorkers = newContinueBridgeWorkerStore()
	}
	return s.continueWorkers
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
	if s == nil || strings.TrimSpace(registration.SessionID) == "" || strings.TrimSpace(registration.CallbackURL) == "" {
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

func normalizeContinueWorkerCapabilities(capabilities []string) []string {
	if len(capabilities) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(capabilities))
	normalized := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" || seen[capability] {
			continue
		}
		seen[capability] = true
		normalized = append(normalized, capability)
	}
	sort.Strings(normalized)
	return normalized
}

func continueWorkerSupportsCapabilities(registration continueWorkerRegistration, required []string) bool {
	if len(required) == 0 {
		return true
	}
	available := make(map[string]bool, len(registration.Capabilities))
	for _, capability := range registration.Capabilities {
		available[strings.ToLower(strings.TrimSpace(capability))] = true
	}
	for _, capability := range required {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" {
			continue
		}
		if !available[capability] {
			return false
		}
	}
	return true
}

func requiredContinueWorkerCapabilities(req continueAgentExecuteRequest) []string {
	required := []string{}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	task := strings.ToLower(strings.TrimSpace(req.Task))
	combined := mode + " " + task

	if strings.TrimSpace(req.Context.ActiveFile) != "" || strings.TrimSpace(req.Context.SelectedText) != "" || len(req.Context.OpenFiles) > 0 {
		required = append(required, "read_context")
	}
	switch {
	case containsAnyWorkflowSignal(combined, "apply", "patch"):
		required = append(required, "apply_patch")
	case containsAnyWorkflowSignal(combined, "edit", "implement", "refactor"):
		required = append(required, "edit_code")
	case containsAnyWorkflowSignal(combined, "review", "diff"):
		required = append(required, "diff_review")
	case containsAnyWorkflowSignal(combined, "plan"):
		required = append(required, "plan_changes")
	}
	return normalizeContinueWorkerCapabilities(required)
}

func (s *localRAGService) handleBridgeChat(w http.ResponseWriter, r *http.Request, transport bridgeChatTransport) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	envelope, err := transport.DecodeChatRequest(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	ask := func(ctx context.Context, request bridgeAskRequest, eventStreamer func(string, any)) (string, error) {
		return s.askBridgeChat(ctx, request, eventStreamer)
	}

	if envelope.Stream {
		transport.StreamChat(w, envelope, ask)
		return
	}

	answer := envelope.DefaultAnswer
	if s != nil && s.service != nil {
		if resp, err := ask(context.Background(), envelope.Ask, nil); err == nil && strings.TrimSpace(resp) != "" {
			answer = resp
		}
	}
	transport.WriteChatResponse(w, envelope, answer)
}

func (s *localRAGService) continueToolHandlers() map[string]continueToolHandler {
	return map[string]continueToolHandler{
		"repo.read_file": func(req continueToolRequest) (any, error) {
			return readContinueFile(req.Path, req.StartLine, req.EndLine)
		},
		"repo.search_code": func(req continueToolRequest) (any, error) {
			return searchContinueCode(req.Query, req.Path, req.Limit)
		},
		"repo.list_files": func(req continueToolRequest) (any, error) {
			return listContinueFiles(req.Path, req.Recursive)
		},
		"code.apply_patch": func(req continueToolRequest) (any, error) {
			return applyContinuePatch(req.Path, req.Selection)
		},
		"code.preview_patch": func(req continueToolRequest) (any, error) {
			return previewContinuePatch(req.Path, req.Selection)
		},
		"test.run": func(req continueToolRequest) (any, error) {
			return runContinueTest(req.Query, req.SessionID)
		},
		"agent.ask": func(req continueToolRequest) (any, error) {
			return s.delegateContinueAgent(req.Query, req.SessionID)
		},
	}
}

func (s *localRAGService) bridgeClients() []bridgeClientAdapter {
	return []bridgeClientAdapter{
		continueBridgeAdapter{transport: continueBridgeTransport{}},
		openAICompatibleBridgeAdapter{transport: openAICompatibleBridgeTransport{}},
	}
}

func (s *localRAGService) registerBridgeRoutes(mux *http.ServeMux, reg *SessionRegistry) {
	if mux == nil || reg == nil {
		return
	}
	for _, client := range s.bridgeClients() {
		for _, route := range client.Routes(s) {
			handler := route.Handler
			requiresService := route.RequiresService
			wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requiresService && s == nil {
					respondJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "official RAG is not configured"})
					return
				}
				handler(w, r)
			})
			mux.HandleFunc(route.Pattern, handleOptionsAndCORS(withRateLimit(reg, wrapped)))
		}
	}
}

func (a continueBridgeAdapter) Routes(s *localRAGService) []bridgeRoute {
	return []bridgeRoute{
		{
			Pattern:         "/api/continue/chat",
			RequiresService: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleChat(s, w, r)
			},
		},
		{
			Pattern:         "/api/continue/tools/",
			RequiresService: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleTool(s, w, r)
			},
		},
		{
			Pattern: "/api/continue/metadata",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.writeMetadata(w, s)
			},
		},
		{
			Pattern:         "/api/continue/agent/execute",
			RequiresService: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleAgentExecute(s, w, r)
			},
		},
		{
			Pattern: "/api/continue/sessions/",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleSessionState(s, w, r)
			},
		},
		{
			Pattern:         "/api/continue/worker/register",
			RequiresService: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleWorkerRegister(s, w, r)
			},
		},
		{
			Pattern: "/api/continue/workers/",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleWorkerState(s, w, r)
			},
		},
	}
}

func (a openAICompatibleBridgeAdapter) Routes(s *localRAGService) []bridgeRoute {
	return []bridgeRoute{
		{
			Pattern:         "/v1/chat/completions",
			RequiresService: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleChat(s, w, r)
			},
		},
		{
			Pattern: "/v1/models",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.writeModels(w)
			},
		},
	}
}

func (continueBridgeTransport) handleChat(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req continueChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Invalid JSON body: " + err.Error()})
		return
	}
	envelope, err := buildContinueBridgeEnvelope(req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req.SessionID = envelope.Ask.SessionID
	continueBridgeTransport{}.streamContinueEnvelope(s, w, envelope, req, "")
}

func (continueBridgeTransport) handleTool(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	toolName := strings.TrimPrefix(r.URL.Path, "/api/continue/tools/")
	if toolName == "" {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "tool name is required"})
		return
	}
	s.handleContinueTool(w, r, toolName)
}

func (continueBridgeTransport) handleAgentExecute(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req continueAgentExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Invalid JSON body: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Task) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "task cannot be empty"})
		return
	}

	chatReq := continueChatRequest{SessionID: req.SessionID, Message: req.Task, Mode: req.Mode}
	chatReq.Workspace.Root = req.Context.WorkspaceRoot
	chatReq.Workspace.Active = req.Context.ActiveFile
	chatReq.Context.SelectedText = req.Context.SelectedText
	chatReq.Context.OpenFiles = req.Context.OpenFiles
	if strings.TrimSpace(chatReq.Mode) == "" {
		chatReq.Mode = "coding"
	}
	workflowProfile, requestedPhase := resolveContinueWorkflowProfile(chatReq.Mode, chatReq.Message)
	if registration, ok := s.ensureContinueWorkerStore().Get(chatReq.SessionID); ok {
		requiredCapabilities := requiredContinueWorkerCapabilities(req)
		if continueWorkerSupportsCapabilities(registration, requiredCapabilities) {
			s.ensureContinueSessionStore().Put(buildContinueSessionSnapshot(chatReq, workflowProfile, requestedPhase, "in_progress", chatReq.Message))
			statusCode, err := continueBridgeTransport{}.proxyAgentExecuteToWorker(r.Context(), w, req, registration)
			if err != nil {
				s.ensureContinueSessionStore().Put(buildContinueSessionSnapshot(chatReq, workflowProfile, requestedPhase, "failed", err.Error()))
				respondJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			status := "completed"
			if statusCode >= 400 {
				status = "failed"
			}
			s.ensureContinueSessionStore().Put(buildContinueSessionSnapshot(chatReq, workflowProfile, requestedPhase, status, chatReq.Message))
			return
		}
	}

	envelope, err := buildContinueBridgeEnvelope(chatReq)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	chatReq.SessionID = envelope.Ask.SessionID
	continueBridgeTransport{}.streamContinueEnvelope(s, w, envelope, chatReq, "Executing editor-aware coding task")
}

func (continueBridgeTransport) proxyAgentExecuteToWorker(ctx context.Context, w http.ResponseWriter, req continueAgentExecuteRequest, registration continueWorkerRegistration) (int, error) {
	body, statusCode, err := executeContinueWorkerCallback(ctx, req, registration)
	if err != nil {
		return 0, err
	}
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(statusCode)
	if _, err := w.Write(body); err != nil {
		return statusCode, fmt.Errorf("copy worker response: %w", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return statusCode, nil
}

func (continueBridgeTransport) handleSessionState(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/continue/sessions/"))
	if sessionID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "session id is required"})
		return
	}
	store := s.ensureContinueSessionStore()
	snapshot, ok := store.Get(sessionID)
	if !ok {
		respondJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "session not found"})
		return
	}
	respondJSON(w, http.StatusOK, snapshot)
}

func (continueBridgeTransport) handleWorkerRegister(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req continueWorkerRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Invalid JSON body: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.CallbackURL) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "session_id and callback_url are required"})
		return
	}
	registration := continueWorkerRegistration{
		SessionID:    strings.TrimSpace(req.SessionID),
		CallbackURL:  strings.TrimSpace(req.CallbackURL),
		Capabilities: normalizeContinueWorkerCapabilities(req.Capabilities),
		IDE:          strings.TrimSpace(req.IDE),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	s.ensureContinueWorkerStore().Put(registration)
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "worker": registration})
}

func (continueBridgeTransport) handleWorkerState(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/continue/workers/"))
	if sessionID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "session id is required"})
		return
	}
	registration, ok := s.ensureContinueWorkerStore().Get(sessionID)
	if !ok {
		respondJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "worker not found"})
		return
	}
	respondJSON(w, http.StatusOK, registration)
}

func (continueBridgeTransport) writeMetadata(w http.ResponseWriter, s *localRAGService) {
	if w == nil {
		return
	}
	toolNames := []string{}
	if s != nil {
		toolNames = s.continueToolNames()
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"object": "client.capabilities",
		"client": "continue",
		"chat": map[string]any{
			"path":      "/api/continue/chat",
			"streaming": "ndjson",
		},
		"session_state": map[string]any{
			"path_prefix": "/api/continue/sessions/",
			"method":      "GET",
		},
		"agent_execute": map[string]any{
			"path":      "/api/continue/agent/execute",
			"method":    "POST",
			"streaming": "ndjson",
		},
		"worker_register": map[string]any{
			"path":   "/api/continue/worker/register",
			"method": "POST",
		},
		"worker_state": map[string]any{
			"path_prefix": "/api/continue/workers/",
			"method":      "GET",
		},
		"worker_capabilities": continueWorkerCapabilityCatalog,
		"tools": map[string]any{
			"path_prefix": "/api/continue/tools/",
			"names":       toolNames,
		},
		"features": map[string]any{
			"workspace_context": true,
			"streaming":         true,
			"tool_dispatch":     true,
			"coding_workflows":  true,
			"session_tracking":  true,
			"remote_agent":      true,
			"worker_callback":   true,
		},
		"coding_workflows": continueWorkflowMetadata(),
		"control_model": map[string]any{
			"controller":            "ai_studio_daemon",
			"editor_worker":         "continue",
			"provider_is_attribute": true,
		},
	})
}

func continueWorkflowProfiles() []continueWorkflowProfile {
	return []continueWorkflowProfile{
		{
			ID:               "feature_implementation",
			Label:            "Feature Implementation",
			Description:      "Drive a feature from analysis and planning through implementation, validation, iteration, and delivery.",
			TaskType:         "implementation",
			DefaultPhasePlan: []string{"investigation", "planning", "execution", "review", "delivery"},
			ModeHints:        []string{"plan", "implement", "edit", "deliver"},
			Guidance:         "Ground the feature in repository evidence, produce a change plan, implement incrementally, validate with tests, and finish with a delivery summary.",
		},
		{
			ID:               "debugging",
			Label:            "Debugging",
			Description:      "Investigate failures, isolate the defect, apply a focused fix, and verify the repair.",
			TaskType:         "debugging",
			DefaultPhasePlan: []string{"investigation", "execution", "review"},
			ModeHints:        []string{"debug", "fix", "repair"},
			Guidance:         "Reproduce or inspect the failure first, patch the smallest responsible slice, and rerun the narrowest validation that can falsify the fix.",
		},
		{
			ID:               "testing_validation",
			Label:            "Testing & Validation",
			Description:      "Focus on test design, execution, failure analysis, and validation loops.",
			TaskType:         "testing",
			DefaultPhasePlan: []string{"investigation", "execution", "review"},
			ModeHints:        []string{"test", "validate", "verify"},
			Guidance:         "Prioritize failing or behavior-scoped tests, inspect failure output, and keep iterating until the touched slice is validated.",
		},
		{
			ID:               "refactoring",
			Label:            "Refactoring",
			Description:      "Restructure code safely while preserving behavior through planning and verification.",
			TaskType:         "refactoring",
			DefaultPhasePlan: []string{"investigation", "planning", "execution", "review"},
			ModeHints:        []string{"refactor", "cleanup", "simplify"},
			Guidance:         "Map the controlling abstraction first, make small behavior-preserving edits, and validate with targeted tests or type checks.",
		},
		{
			ID:               "code_review",
			Label:            "Code Review",
			Description:      "Review changes for correctness, regressions, risk, and missing validation.",
			TaskType:         "review",
			DefaultPhasePlan: []string{"investigation", "review", "delivery"},
			ModeHints:        []string{"review", "audit"},
			Guidance:         "Prioritize concrete findings with evidence, then summarize residual risks and validation gaps.",
		},
	}
}

func continueWorkflowMetadata() map[string]any {
	profiles := continueWorkflowProfiles()
	items := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, map[string]any{
			"id":                 profile.ID,
			"label":              profile.Label,
			"description":        profile.Description,
			"task_type":          profile.TaskType,
			"default_phase_plan": profile.DefaultPhasePlan,
			"mode_hints":         profile.ModeHints,
		})
	}
	return map[string]any{
		"phase_vocabulary":   []string{"intake", "investigation", "planning", "execution", "review", "delivery"},
		"supported_profiles": items,
	}
}

func buildContinueBridgeEnvelope(req continueChatRequest) (bridgeChatEnvelope, error) {
	if strings.TrimSpace(req.Message) == "" {
		return bridgeChatEnvelope{}, fmt.Errorf("message cannot be empty")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		req.SessionID = "continue-bridge"
	}

	variables := map[string]any{"continue_bridge": true, "workspace_root": req.Workspace.Root}
	workflowProfile, requestedPhase := resolveContinueWorkflowProfile(req.Mode, req.Message)
	if mode := strings.TrimSpace(req.Mode); mode != "" {
		variables["continue_mode"] = mode
	}
	if activeFile := strings.TrimSpace(req.Workspace.Active); activeFile != "" {
		variables["active_file"] = activeFile
	}
	if selectedText := strings.TrimSpace(req.Context.SelectedText); selectedText != "" {
		variables["selected_text"] = selectedText
	}
	if len(req.Context.OpenFiles) > 0 {
		variables["open_files"] = append([]string(nil), req.Context.OpenFiles...)
	}
	if workflowProfile.ID != "" {
		variables["coding_workflow_profile"] = workflowProfile.ID
		variables["coding_task_type"] = workflowProfile.TaskType
	}
	if requestedPhase != "" {
		variables["requested_phase"] = requestedPhase
	}
	return bridgeChatEnvelope{
		Ask: bridgeAskRequest{
			SessionID:  req.SessionID,
			UserPrompt: req.Message,
			FullPrompt: buildContinueBridgePrompt(req, workflowProfile, requestedPhase),
			Variables:  variables,
		},
		Stream: true,
		InitialEvents: []bridgeStreamEvent{
			{Type: "progress", Payload: "Preparing local workspace context"},
			{Type: "content", Payload: "I’m connecting your request to the local SOP runtime."},
		},
	}, nil
}

func resolveContinueWorkflowProfile(mode, message string) (continueWorkflowProfile, string) {
	normalizedMode := strings.ToLower(strings.TrimSpace(mode))
	signals := strings.ToLower(strings.TrimSpace(mode + " " + message))

	requestedPhase := ""
	switch {
	case containsAnyWorkflowSignal(normalizedMode, "plan", "planning", "analyze", "analysis", "design"):
		requestedPhase = "planning"
	case containsAnyWorkflowSignal(normalizedMode, "implement", "implementation", "edit", "apply", "execute", "code"):
		requestedPhase = "execution"
	case containsAnyWorkflowSignal(normalizedMode, "test", "validate", "verify", "review"):
		requestedPhase = "review"
	case containsAnyWorkflowSignal(normalizedMode, "deliver", "delivery", "ship", "handoff"):
		requestedPhase = "delivery"
	}

	profiles := continueWorkflowProfiles()
	switch {
	case containsAnyWorkflowSignal(signals, "debug", "bug", "fix", "failure", "failing test", "broken"):
		return profiles[1], requestedPhase
	case containsAnyWorkflowSignal(signals, "test", "testing", "validate", "verification", "coverage"):
		return profiles[2], requestedPhase
	case containsAnyWorkflowSignal(signals, "refactor", "cleanup", "simplify", "rename", "extract"):
		return profiles[3], requestedPhase
	case containsAnyWorkflowSignal(signals, "review", "audit", "regression risk"):
		return profiles[4], requestedPhase
	case containsAnyWorkflowSignal(signals, "feature", "implement", "implementation", "build", "add", "create"):
		return profiles[0], requestedPhase
	default:
		return continueWorkflowProfile{}, requestedPhase
	}
}

func containsAnyWorkflowSignal(text string, signals ...string) bool {
	for _, signal := range signals {
		if signal != "" && strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsAllWorkflowSignals(text string, signals ...string) bool {
	for _, signal := range signals {
		if signal == "" {
			continue
		}
		if !strings.Contains(text, signal) {
			return false
		}
	}
	return len(signals) > 0
}

func buildContinueSessionSnapshot(req continueChatRequest, workflowProfile continueWorkflowProfile, requestedPhase, status, message string) continueSessionSnapshot {
	phase := requestedPhase
	if phase == "" && len(workflowProfile.DefaultPhasePlan) > 0 {
		phase = workflowProfile.DefaultPhasePlan[0]
	}
	return continueSessionSnapshot{
		SessionID:       req.SessionID,
		Status:          status,
		Phase:           phase,
		Mode:            strings.TrimSpace(req.Mode),
		WorkflowProfile: workflowProfile.ID,
		TaskType:        workflowProfile.TaskType,
		Message:         message,
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
		OpenFiles:       append([]string(nil), req.Context.OpenFiles...),
		ActiveFile:      strings.TrimSpace(req.Workspace.Active),
		WorkspaceRoot:   strings.TrimSpace(req.Workspace.Root),
	}
}

func inferContinueOpenAIWorkflowContext(messages []openAICompatibleMessage) (string, continueWorkflowProfile, string) {
	textParts := make([]string, 0, len(messages))
	for _, message := range messages {
		text := strings.TrimSpace(extractOpenAICompatibleMessageText(message.Content))
		if text != "" {
			textParts = append(textParts, text)
		}
	}
	combined := strings.ToLower(strings.Join(textParts, "\n\n"))
	inferredMode := ""
	switch {
	case containsAllWorkflowSignals(combined, "original code:", "suggested edit:") || containsAllWorkflowSignals(combined, "original code:", "new code:"):
		inferredMode = "apply"
	case containsAnyWorkflowSignal(combined,
		"rewrite the above code",
		"edit the code to perfectly satisfy",
		"you will rewrite the above code",
		"rewrite the code to satisfy this request",
		"<start editing here>"):
		inferredMode = "edit"
	}
	profile, requestedPhase := resolveContinueWorkflowProfile(inferredMode, combined)
	return inferredMode, profile, requestedPhase
}

func inferOpenAICompatibleEditorContext(messages []openAICompatibleMessage) inferredOpenAICompatibleEditorContext {
	ctx := inferredOpenAICompatibleEditorContext{}
	texts := make([]string, 0, len(messages))
	pathHints := make([]string, 0, len(messages))
	for _, message := range messages {
		text := strings.TrimSpace(extractOpenAICompatibleMessageText(message.Content))
		if text == "" {
			continue
		}
		texts = append(texts, text)
		if ctx.SelectedText == "" {
			ctx.SelectedText = inferSelectedTextFromPrompt(text)
		}
		pathHints = append(pathHints, inferOpenAICompatiblePathHints(text)...)
	}

	combined := strings.Join(texts, "\n\n")
	if workspaceRoot := inferLabeledValue(combined, "workspace_root", "workspace root"); workspaceRoot != "" {
		ctx.WorkspaceRoot = workspaceRoot
	}
	if activeFile := inferLabeledValue(combined, "active_file", "active file", "current file"); activeFile != "" {
		ctx.ActiveFile = activeFile
	}
	if ctx.SelectedText == "" {
		ctx.SelectedText = inferLabeledCodeBlock(combined, "selected text", "selection")
	}
	ctx.OpenFiles = dedupeStrings(pathHints)
	if ctx.ActiveFile == "" && len(ctx.OpenFiles) > 0 {
		ctx.ActiveFile = ctx.OpenFiles[0]
	}
	if ctx.ActiveFile != "" && !containsString(ctx.OpenFiles, ctx.ActiveFile) {
		ctx.OpenFiles = append([]string{ctx.ActiveFile}, ctx.OpenFiles...)
	}
	return ctx
}

func buildOpenAICompatibleEditorContextText(ctx inferredOpenAICompatibleEditorContext) string {
	lines := make([]string, 0, 4)
	if ctx.WorkspaceRoot != "" {
		lines = append(lines, fmt.Sprintf("[workspace_root=%s]", ctx.WorkspaceRoot))
	}
	if ctx.ActiveFile != "" {
		lines = append(lines, fmt.Sprintf("[active_file=%s]", ctx.ActiveFile))
	}
	if ctx.SelectedText != "" {
		lines = append(lines, fmt.Sprintf("[selected_text=%s]", ctx.SelectedText))
	}
	if len(ctx.OpenFiles) > 0 {
		lines = append(lines, fmt.Sprintf("[open_files=%s]", strings.Join(ctx.OpenFiles, ",")))
	}
	return strings.Join(lines, "\n")
}

func inferSelectedTextFromPrompt(text string) string {
	if text == "" {
		return ""
	}
	if selectedText := inferLabeledCodeBlock(text, "original code", "code to edit"); selectedText != "" {
		return selectedText
	}
	trimmed := strings.TrimSpace(text)
	if containsAnyWorkflowSignal(strings.ToLower(trimmed), "rewrite the above code", "rewrite the code to satisfy", "you will rewrite the above code") {
		blocks := extractFencedCodeBlocks(trimmed)
		if len(blocks) > 0 {
			return blocks[0].Body
		}
	}
	return ""
}

type fencedCodeBlock struct {
	Info string
	Body string
}

func extractFencedCodeBlocks(text string) []fencedCodeBlock {
	all := regexp.MustCompile("(?s)```([^\n`]*)\\n(.*?)```").FindAllStringSubmatch(text, -1)
	blocks := make([]fencedCodeBlock, 0, len(all))
	for _, match := range all {
		if len(match) < 3 {
			continue
		}
		body := strings.TrimSpace(match[2])
		if body == "" {
			continue
		}
		blocks = append(blocks, fencedCodeBlock{Info: strings.TrimSpace(match[1]), Body: body})
	}
	return blocks
}

func inferLabeledCodeBlock(text string, labels ...string) string {
	lower := strings.ToLower(text)
	for _, label := range labels {
		needle := strings.ToLower(strings.TrimSpace(label))
		if needle == "" {
			continue
		}
		idx := strings.Index(lower, needle)
		if idx == -1 {
			continue
		}
		if blocks := extractFencedCodeBlocks(text[idx:]); len(blocks) > 0 {
			return blocks[0].Body
		}
	}
	return ""
}

func inferLabeledValue(text string, labels ...string) string {
	for _, label := range labels {
		if label == "" {
			continue
		}
		pattern := regexp.MustCompile(`(?im)(?:\[)?` + regexp.QuoteMeta(label) + `(?:\])?\s*[:=]\s*([^\n\]]+)`)
		if match := pattern.FindStringSubmatch(text); len(match) > 1 {
			value := strings.TrimSpace(strings.Trim(match[1], "`\""))
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func inferOpenAICompatiblePathHints(text string) []string {
	paths := []string{}
	for _, block := range extractFencedCodeBlocks(text) {
		if path := normalizeFileHint(block.Info); path != "" {
			paths = append(paths, path)
		}
	}
	if activeFile := inferLabeledValue(text, "active file", "current file"); activeFile != "" {
		paths = append(paths, activeFile)
	}
	return dedupeStrings(paths)
}

func normalizeFileHint(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "`"))
	if value == "" || !looksLikePathHint(value) {
		return ""
	}
	return value
}

func looksLikePathHint(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	knownLanguageTags := map[string]bool{
		"go": true, "js": true, "ts": true, "tsx": true, "jsx": true,
		"json": true, "yaml": true, "yml": true, "py": true, "python": true,
		"java": true, "rb": true, "ruby": true, "rs": true, "rust": true,
		"sh": true, "bash": true, "zsh": true, "md": true, "markdown": true,
		"sql": true, "xml": true, "html": true, "css": true,
	}
	if knownLanguageTags[lower] {
		return false
	}
	return strings.Contains(value, "/") || strings.Contains(value, "\\") || regexp.MustCompile(`^[A-Za-z0-9._-]+\.[A-Za-z0-9._-]+$`).MatchString(value)
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func (s *localRAGService) continueToolNames() []string {
	handlers := s.continueToolHandlers()
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (openAICompatibleBridgeTransport) handleChat(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	s.handleBridgeChat(w, r, openAICompatibleBridgeTransport{})
}

func (openAICompatibleBridgeTransport) modelCatalog() []bridgeModelDescriptor {
	return []bridgeModelDescriptor{
		{
			ID:      "daemon-bridge",
			OwnedBy: "sop-daemon",
			Metadata: map[string]any{
				"bridge":                    "openai-compatible",
				"supports_stream":           true,
				"supports_chat":             true,
				"provider_alias_format":     "provider:model",
				"supported_alias_providers": []string{"chatgpt", "gemini", "anthropic"},
				"provider_alias_examples":   []string{"gpt-5.4", "gpt-4o-mini", "gemini:gemini-2.5-pro", "anthropic:claude-3-5-sonnet-20241022"},
			},
		},
		{
			ID:      "gpt-5.4",
			OwnedBy: "sop-daemon",
			Metadata: map[string]any{
				"alias_of":            "daemon-bridge",
				"compatibility_alias": true,
				"supports_stream":     true,
			},
		},
		{
			ID:      "gpt-4o-mini",
			OwnedBy: "sop-daemon",
			Metadata: map[string]any{
				"alias_of":            "daemon-bridge",
				"compatibility_alias": true,
				"supports_stream":     true,
			},
		},
		{
			ID:      "gemini:gemini-2.5-pro",
			OwnedBy: "sop-daemon",
			Metadata: map[string]any{
				"alias_of":          "daemon-bridge",
				"provider_alias":    true,
				"provider":          "gemini",
				"routed_via_bridge": true,
				"supports_stream":   true,
			},
		},
		{
			ID:      "anthropic:claude-3-5-sonnet-20241022",
			OwnedBy: "sop-daemon",
			Metadata: map[string]any{
				"alias_of":          "daemon-bridge",
				"provider_alias":    true,
				"provider":          "anthropic",
				"routed_via_bridge": true,
				"supports_stream":   true,
			},
		},
	}
}

func (t openAICompatibleBridgeTransport) writeModels(w http.ResponseWriter) {
	if w == nil {
		return
	}
	models := t.modelCatalog()
	data := make([]map[string]any, 0, len(models))
	created := time.Now().Unix()
	for _, model := range models {
		data = append(data, map[string]any{
			"id":       model.ID,
			"object":   "model",
			"created":  created,
			"owned_by": model.OwnedBy,
			"metadata": model.Metadata,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (continueBridgeTransport) DecodeChatRequest(r *http.Request) (bridgeChatEnvelope, error) {
	var req continueChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return bridgeChatEnvelope{}, fmt.Errorf("Invalid JSON body: %w", err)
	}
	return buildContinueBridgeEnvelope(req)
}

func (continueBridgeTransport) StreamChat(w http.ResponseWriter, envelope bridgeChatEnvelope, ask bridgeAskInvoker) {
	sendEvent, _ := sopbackend.SetupStream(w)
	for _, event := range envelope.InitialEvents {
		sendEvent(event.Type, event.Payload)
	}

	ctx := sopbackend.InjectSharedRuntimeContext(context.Background(), sendEvent, w)
	ctx = context.WithValue(ctx, ai.CtxKeyWriter, sopbackend.NewDirectFlushingWriter(w))

	answer, err := ask(ctx, envelope.Ask, nil)
	if err != nil {
		sendEvent("error", err.Error())
		sendEvent("done", nil)
		return
	}
	if strings.TrimSpace(answer) != "" {
		sendEvent("content", answer)
	}
	sendEvent("done", nil)
}

func (continueBridgeTransport) WriteChatResponse(w http.ResponseWriter, envelope bridgeChatEnvelope, answer string) {
	respondJSON(w, http.StatusOK, AskResponse{Answer: answer})
}

func (continueBridgeTransport) streamContinueEnvelope(s *localRAGService, w http.ResponseWriter, envelope bridgeChatEnvelope, req continueChatRequest, startingMessage string) {
	store := s.ensureContinueSessionStore()
	workflowProfile, requestedPhase := resolveContinueWorkflowProfile(req.Mode, req.Message)
	store.Put(buildContinueSessionSnapshot(req, workflowProfile, requestedPhase, "in_progress", req.Message))

	sendEvent, _ := sopbackend.SetupStream(w)
	if strings.TrimSpace(startingMessage) != "" {
		sendEvent("progress", startingMessage)
	}
	for _, event := range envelope.InitialEvents {
		sendEvent(event.Type, event.Payload)
	}

	ctx := sopbackend.InjectSharedRuntimeContext(context.Background(), sendEvent, w)
	ctx = context.WithValue(ctx, ai.CtxKeyWriter, sopbackend.NewDirectFlushingWriter(w))

	answer, err := s.askBridgeChat(ctx, envelope.Ask, nil)
	if err != nil {
		store.Put(buildContinueSessionSnapshot(req, workflowProfile, requestedPhase, "failed", err.Error()))
		sendEvent("error", err.Error())
		sendEvent("done", nil)
		return
	}
	store.Put(buildContinueSessionSnapshot(req, workflowProfile, requestedPhase, "completed", req.Message))
	if strings.TrimSpace(answer) != "" {
		sendEvent("content", answer)
	}
	sendEvent("done", nil)
}

func buildContinueBridgePrompt(req continueChatRequest, workflowProfile continueWorkflowProfile, requestedPhase string) string {
	query := req.Message
	if mode := strings.TrimSpace(req.Mode); mode != "" {
		query += fmt.Sprintf("\n[continue_mode=%s]", mode)
	}
	if workflowProfile.ID != "" {
		query += fmt.Sprintf("\n[coding_workflow_profile=%s]", workflowProfile.ID)
		query += fmt.Sprintf("\n[coding_task_type=%s]", workflowProfile.TaskType)
		query += fmt.Sprintf("\n[recommended_phase_plan=%s]", strings.Join(workflowProfile.DefaultPhasePlan, " -> "))
		query += fmt.Sprintf("\n[workflow_guidance=%s]", workflowProfile.Guidance)
	}
	if requestedPhase != "" {
		query += fmt.Sprintf("\n[requested_phase=%s]", requestedPhase)
	}
	if strings.TrimSpace(req.Workspace.Root) != "" {
		query += fmt.Sprintf("\n[workspace_root=%s]", req.Workspace.Root)
	}
	if strings.TrimSpace(req.Workspace.Active) != "" {
		query += fmt.Sprintf("\n[active_file=%s]", req.Workspace.Active)
	}
	if strings.TrimSpace(req.Context.SelectedText) != "" {
		query += fmt.Sprintf("\n[selected_text=%s]", req.Context.SelectedText)
	}
	if len(req.Context.OpenFiles) > 0 {
		query += fmt.Sprintf("\n[open_files=%s]", strings.Join(req.Context.OpenFiles, ","))
	}
	return query
}

type openAICompatibleChatRequest struct {
	Model    string                    `json:"model"`
	User     string                    `json:"user,omitempty"`
	Stream   bool                      `json:"stream,omitempty"`
	Messages []openAICompatibleMessage `json:"messages"`
}

type openAICompatibleMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func (openAICompatibleBridgeTransport) DecodeChatRequest(r *http.Request) (bridgeChatEnvelope, error) {
	var req openAICompatibleChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return bridgeChatEnvelope{}, fmt.Errorf("Invalid JSON body: %w", err)
	}

	userPrompt, fullPrompt := buildOpenAICompatibleQuery(req.Messages)
	if strings.TrimSpace(userPrompt) == "" {
		return bridgeChatEnvelope{}, fmt.Errorf("user message is required")
	}
	inferredMode, workflowProfile, requestedPhase := inferContinueOpenAIWorkflowContext(req.Messages)
	editorContext := inferOpenAICompatibleEditorContext(req.Messages)
	if inferredMode != "" || workflowProfile.ID != "" || requestedPhase != "" {
		workflowContextLines := make([]string, 0, 4)
		if inferredMode != "" {
			workflowContextLines = append(workflowContextLines, fmt.Sprintf("[continue_mode=%s]", inferredMode))
		}
		if workflowProfile.ID != "" {
			workflowContextLines = append(workflowContextLines,
				fmt.Sprintf("[coding_workflow_profile=%s]", workflowProfile.ID),
				fmt.Sprintf("[coding_task_type=%s]", workflowProfile.TaskType),
				fmt.Sprintf("[recommended_phase_plan=%s]", strings.Join(workflowProfile.DefaultPhasePlan, " -> ")),
			)
		}
		if requestedPhase != "" {
			workflowContextLines = append(workflowContextLines, fmt.Sprintf("[requested_phase=%s]", requestedPhase))
		}
		if len(workflowContextLines) > 0 {
			fullPrompt += "\n\nContinue workflow context:\n" + strings.Join(workflowContextLines, "\n")
		}
	}
	if editorContextText := buildOpenAICompatibleEditorContextText(editorContext); editorContextText != "" {
		fullPrompt += "\n\nContinue editor context:\n" + editorContextText
	}

	sessionID := strings.TrimSpace(req.User)
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Continue-Session-Id"))
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-Id"))
	}
	if sessionID == "" {
		sessionID = "openai-compatible"
	}

	providerName, modelName := parseOpenAICompatibleModelAlias(req.Model)
	variables := map[string]any{"continue_bridge": true}
	if inferredMode != "" {
		variables["continue_mode"] = inferredMode
	}
	if workflowProfile.ID != "" {
		variables["coding_workflow_profile"] = workflowProfile.ID
		variables["coding_task_type"] = workflowProfile.TaskType
	}
	if requestedPhase != "" {
		variables["requested_phase"] = requestedPhase
	}
	if editorContext.WorkspaceRoot != "" {
		variables["workspace_root"] = editorContext.WorkspaceRoot
	}
	if editorContext.ActiveFile != "" {
		variables["active_file"] = editorContext.ActiveFile
	}
	if editorContext.SelectedText != "" {
		variables["selected_text"] = editorContext.SelectedText
	}
	if len(editorContext.OpenFiles) > 0 {
		variables["open_files"] = append([]string(nil), editorContext.OpenFiles...)
	}
	return bridgeChatEnvelope{
		Ask: bridgeAskRequest{
			SessionID:    sessionID,
			UserPrompt:   userPrompt,
			FullPrompt:   fullPrompt,
			ProviderName: providerName,
			ModelName:    modelName,
			Variables:    variables,
		},
		Stream:        req.Stream,
		ResponseModel: req.Model,
		DefaultAnswer: "Hello from the local SOP runtime. I received your request and I’m ready to help.",
	}, nil
}

func (openAICompatibleBridgeTransport) StreamChat(w http.ResponseWriter, envelope bridgeChatEnvelope, ask bridgeAskInvoker) {
	sse := newOpenAICompatibleSSEWriter(w, envelope.ResponseModel)
	eventStreamer := func(eventType string, payload any) {
		switch eventType {
		case ai.ReasoningEventAssistantMessage:
			if message, ok := payload.(map[string]any); ok {
				if source, _ := message["source"].(string); strings.EqualFold(strings.TrimSpace(source), "reasoning_summary") {
					return
				}
				if text, ok := message["text"].(string); ok {
					sse.writeContent(text)
				}
			}
		case "content":
			sse.writeContent(fmt.Sprintf("%v", payload))
		case "progress":
			sse.writeContent(fmt.Sprintf("Progress: %v", payload))
		case ai.ReasoningEventToolCall:
			if event, ok := payload.(map[string]any); ok {
				tool, _ := event["tool"].(string)
				if strings.TrimSpace(tool) != "" {
					sse.writeContent(fmt.Sprintf("Calling tool: %s", strings.TrimSpace(tool)))
				}
			}
		case ai.ReasoningEventToolResult:
			if event, ok := payload.(map[string]any); ok {
				if summary := summarizeOpenAICompatibleToolResult(event); summary != "" {
					sse.writeContent(summary)
				}
			}
		case ai.ReasoningEventToolError:
			if event, ok := payload.(map[string]any); ok {
				tool, _ := event["tool"].(string)
				errText, _ := event["error"].(string)
				if strings.TrimSpace(tool) != "" || strings.TrimSpace(errText) != "" {
					sse.writeContent(strings.TrimSpace(fmt.Sprintf("Tool error (%s): %s", strings.TrimSpace(tool), strings.TrimSpace(errText))))
				}
			}
		case "terminal_command":
			switch typed := payload.(type) {
			case map[string]any:
				if text, ok := typed["text"].(string); ok && strings.TrimSpace(text) != "" {
					sse.writeContent(text)
				}
			case string:
				if strings.TrimSpace(typed) != "" {
					sse.writeContent(typed)
				}
			}
		case "error":
			sse.writeContent(fmt.Sprintf("Error: %v", payload))
		}
	}

	answer, err := ask(context.Background(), envelope.Ask, eventStreamer)
	if err != nil {
		sse.finish("Error: " + err.Error())
		return
	}
	sse.finish(answer)
}

func (openAICompatibleBridgeTransport) WriteChatResponse(w http.ResponseWriter, envelope bridgeChatEnvelope, answer string) {
	response := map[string]any{
		"id":      "chatcmpl-sop-1",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   envelope.ResponseModel,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": answer,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	}
	respondJSON(w, http.StatusOK, response)
}

func buildOpenAICompatibleQuery(messages []openAICompatibleMessage) (latestUser string, fullPrompt string) {
	if len(messages) == 0 {
		return "", ""
	}
	lastUserIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			lastUserIndex = i
			break
		}
	}
	if lastUserIndex == -1 {
		return "", ""
	}
	latestUser = strings.TrimSpace(extractOpenAICompatibleMessageText(messages[lastUserIndex].Content))
	if latestUser == "" {
		return "", ""
	}
	sections := make([]string, 0, 3)
	systemParts := make([]string, 0, 2)
	historyParts := make([]string, 0, len(messages))
	for idx, msg := range messages {
		text := strings.TrimSpace(extractOpenAICompatibleMessageText(msg.Content))
		if text == "" {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "system", "developer":
			systemParts = append(systemParts, text)
		case "user", "assistant", "tool":
			if idx == lastUserIndex {
				continue
			}
			historyParts = append(historyParts, fmt.Sprintf("%s: %s", strings.Title(role), text))
		}
	}
	if len(systemParts) > 0 {
		sections = append(sections, "Continue system instructions:\n"+strings.Join(systemParts, "\n\n"))
	}
	if len(historyParts) > 0 {
		sections = append(sections, "Continue conversation history:\n"+strings.Join(historyParts, "\n"))
	}
	sections = append(sections, "Current Continue request:\n"+latestUser)
	return latestUser, strings.Join(sections, "\n\n")
}

func extractOpenAICompatibleMessageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			switch typed := part.(type) {
			case string:
				if strings.TrimSpace(typed) != "" {
					parts = append(parts, typed)
				}
			case map[string]any:
				if text, ok := typed["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func parseOpenAICompatibleModelAlias(model string) (provider string, resolvedModel string) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" || strings.EqualFold(trimmed, "daemon-bridge") {
		return "", ""
	}
	providerPart, modelPart := agent.NormalizeProviderAndModel(trimmed, "")
	if strings.TrimSpace(modelPart) != "" {
		return providerPart, modelPart
	}
	return agent.ProviderChatGPT, trimmed
}
