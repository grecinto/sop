package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/tools/sopbackend"
)

type bridgeAskRequest struct {
	SessionID    string
	UserPrompt   string
	FullPrompt   string
	ProviderName string
	APIKey       string
	BaseURL      string
	ModelName    string
	AccessToken  string
	RefreshToken string
	HQBaseURL    string
	HQPort       string
	HQUserID     string
	HQSessionID  string
	Variables    map[string]any
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

type continueWorkflowProfile struct {
	ID               string
	Label            string
	Description      string
	TaskType         string
	DefaultPhasePlan []string
	ModeHints        []string
	Guidance         string
}

type continueRoutingHint struct {
	Tier   string
	Reason string
}

type continueAutoModelSelection struct {
	Provider string
	Model    string
	Tier     string
	Reason   string
}

type continueIDEToolDefinition struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Readonly     bool     `json:"readonly"`
	Experimental bool     `json:"experimental,omitempty"`
	Modes        []string `json:"modes,omitempty"`
	Phases       []string `json:"phases,omitempty"`
	Workflows    []string `json:"workflows,omitempty"`
}

type continueIDEPhaseDefinition struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	ContinueModes  []string `json:"continue_modes,omitempty"`
	AvailableTools []string `json:"available_tools,omitempty"`
}

type continueExecutionResultContract struct {
	StreamProgress       bool `json:"stream_progress,omitempty"`
	IncludeModifiedFiles bool `json:"include_modified_files,omitempty"`
	IncludeDiffDeltas    bool `json:"include_diff_deltas,omitempty"`
	IncludeMetadata      bool `json:"include_metadata,omitempty"`
	PreferArtifactRefs   bool `json:"prefer_artifact_refs,omitempty"`
	MaxInlineDiffBytes   int  `json:"max_inline_diff_bytes,omitempty"`
}

type continueExecutionArtifact struct {
	ID          string         `json:"id,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	Path        string         `json:"path,omitempty"`
	Description string         `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type continueExecutionModifiedFile struct {
	FilePath        string         `json:"file_path,omitempty"`
	CRUDOperation   string         `json:"crud_operation,omitempty"`
	DiffSummary     string         `json:"diff_summary,omitempty"`
	DiffDelta       string         `json:"diff_delta,omitempty"`
	DiffFormat      string         `json:"diff_format,omitempty"`
	DiffArtifactRef string         `json:"diff_artifact_ref,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type continueExecutionResult struct {
	Status        string                          `json:"status,omitempty"`
	Summary       string                          `json:"summary,omitempty"`
	ModifiedFiles []continueExecutionModifiedFile `json:"modified_files,omitempty"`
	Artifacts     []continueExecutionArtifact     `json:"artifacts,omitempty"`
	Metadata      map[string]any                  `json:"metadata,omitempty"`
}

type continueExecutionEvent struct {
	Type          string         `json:"type,omitempty"`
	Status        string         `json:"status,omitempty"`
	Message       string         `json:"message,omitempty"`
	Content       string         `json:"content,omitempty"`
	Error         string         `json:"error,omitempty"`
	FilePath      string         `json:"file_path,omitempty"`
	CRUDOperation string         `json:"crud_operation,omitempty"`
	DiffSummary   string         `json:"diff_summary,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type continueDelegationReference struct {
	Kind     string         `json:"kind,omitempty"`
	Source   string         `json:"source,omitempty"`
	Title    string         `json:"title,omitempty"`
	Content  string         `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type continueDelegationGuidance struct {
	TeamPractices        []string                      `json:"team_practices,omitempty"`
	SuccessCriteria      []string                      `json:"success_criteria,omitempty"`
	Constraints          []string                      `json:"constraints,omitempty"`
	ImplementationNotes  []string                      `json:"implementation_notes,omitempty"`
	ExpectedResultFormat string                        `json:"expected_result_format,omitempty"`
	ReferenceContext     []continueDelegationReference `json:"reference_context,omitempty"`
	Metadata             map[string]any                `json:"metadata,omitempty"`
}

type continueAgentExecuteRequest struct {
	RequestID          string                          `json:"request_id,omitempty"`
	TraceID            string                          `json:"trace_id,omitempty"`
	RootRequestID      string                          `json:"root_request_id,omitempty"`
	SessionID          string                          `json:"session_id"`
	Task               string                          `json:"task"`
	Provider           string                          `json:"provider,omitempty"`
	APIKey             string                          `json:"api_key,omitempty"`
	Model              string                          `json:"model,omitempty"`
	URL                string                          `json:"url,omitempty"`
	LLMCredentials     map[string]llmCredential        `json:"llm_credentials,omitempty"`
	LLMPoolVersion     int64                           `json:"llm_pool_version,omitempty"`
	LLMPoolUpdatedAtMS int64                           `json:"llm_pool_updated_at_ms,omitempty"`
	LLMPoolUpdatedBy   string                          `json:"llm_pool_updated_by,omitempty"`
	RequestedPhase     string                          `json:"requested_phase,omitempty"`
	AvailableTools     []string                        `json:"available_tools,omitempty"`
	PhaseTools         []string                        `json:"phase_tools,omitempty"`
	AllowedTools       []string                        `json:"allowed_tools,omitempty"`
	ToolChoice         string                          `json:"tool_choice,omitempty"`
	Guidance           continueDelegationGuidance      `json:"guidance,omitempty"`
	ResultContract     continueExecutionResultContract `json:"result_contract,omitempty"`
	Auth               struct {
		AccessToken  string `json:"access_token,omitempty"`
		RefreshToken string `json:"refresh_token,omitempty"`
	} `json:"auth,omitempty"`
	HQ struct {
		BaseURL   string `json:"base_url,omitempty"`
		Port      string `json:"port,omitempty"`
		UserID    string `json:"user_id,omitempty"`
		SessionID string `json:"session_id,omitempty"`
	} `json:"hq,omitempty"`
	Context struct {
		WorkspaceRoot string   `json:"workspace_root"`
		ActiveFile    string   `json:"active_file"`
		SelectedText  string   `json:"selected_text"`
		OpenFiles     []string `json:"open_files"`
	} `json:"context"`
	Mode string `json:"mode"`
}

type continueSessionSnapshot struct {
	RequestID       string                   `json:"request_id,omitempty"`
	SessionID       string                   `json:"session_id"`
	Status          string                   `json:"status"`
	Phase           string                   `json:"phase,omitempty"`
	Mode            string                   `json:"mode,omitempty"`
	WorkflowProfile string                   `json:"workflow_profile,omitempty"`
	TaskType        string                   `json:"task_type,omitempty"`
	Message         string                   `json:"message,omitempty"`
	UpdatedAt       string                   `json:"updated_at,omitempty"`
	OpenFiles       []string                 `json:"open_files,omitempty"`
	ActiveFile      string                   `json:"active_file,omitempty"`
	WorkspaceRoot   string                   `json:"workspace_root,omitempty"`
	ExecutionResult *continueExecutionResult `json:"execution_result,omitempty"`
	ExecutionEvents []continueExecutionEvent `json:"execution_events,omitempty"`
}

type continueWorkerCallbackOutcome struct {
	StatusCode    int
	ContentType   string
	RawBody       string
	FinalResult   *continueExecutionResult
	Events        []continueExecutionEvent
	StreamedEvent bool
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
	requests map[string]continueSessionSnapshot
}

type continueBridgeWorkerStore struct {
	mu      sync.RWMutex
	workers map[string]continueWorkerRegistration
}

var continueWorkerCapabilityCatalog = []string{"read_context", "apply_patch", "edit_code", "diff_review", "plan_changes"}

const continueWorkerRawBodyLimit = 64 * 1024
const continueExecutionEventLimit = 64
const continueExecutionEventTextLimit = 2048

type continueBridgeTransport struct{}

type continueBridgeAdapter struct {
	transport continueBridgeTransport
}

func newContinueBridgeSessionStore() *continueBridgeSessionStore {
	return &continueBridgeSessionStore{
		sessions: map[string]continueSessionSnapshot{},
		requests: map[string]continueSessionSnapshot{},
	}
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
	if requestID := strings.TrimSpace(snapshot.RequestID); requestID != "" {
		s.requests[requestID] = snapshot
	}
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

func (s *continueBridgeSessionStore) GetRequest(requestID string) (continueSessionSnapshot, bool) {
	if s == nil || strings.TrimSpace(requestID) == "" {
		return continueSessionSnapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.requests[requestID]
	return snapshot, ok
}

func newContinueRequestID() string {
	return uuid.NewString()
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
	if parsedURL.User != nil {
		return fmt.Errorf("callback_url must not include user info")
	}
	if parsedURL.Host == "" {
		return fmt.Errorf("callback_url must include a host")
	}

	host := strings.TrimSpace(parsedURL.Hostname())
	if host == "" {
		return fmt.Errorf("callback_url must include a host")
	}
	if host == "localhost" {
		return nil
	}
	if parsedIP := net.ParseIP(host); parsedIP != nil {
		if parsedIP.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("callback_url host must be loopback-only")
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
			return s.handleContinuePatchTool(req)
		},
		"code.preview_patch": func(req continueToolRequest) (any, error) {
			return previewContinuePatch(req.Path, req.Selection)
		},
		"single_find_and_replace": func(req continueToolRequest) (any, error) {
			return s.handleContinueEditTool(req, "single_find_and_replace")
		},
		"multi_edit": func(req continueToolRequest) (any, error) {
			return s.handleContinueEditTool(req, "multi_edit")
		},
		"test.run": func(req continueToolRequest) (any, error) {
			return runContinueTest(req.Query, req.SessionID)
		},
		"agent.ask": func(req continueToolRequest) (any, error) {
			return s.delegateContinueAgent(req.Query, req.SessionID)
		},
	}
}

func (s *localRAGService) handleContinuePatchTool(req continueToolRequest) (any, error) {
	targetPath := resolveContinueEditPath(req.Path, req.ActiveFile)
	if delegatedResult, delegated, err := s.tryDelegateContinueEditTool(req, "code.apply_patch", targetPath); delegated {
		return delegatedResult, err
	}
	return applyContinuePatch(req.Path, req.Selection)
}

func (s *localRAGService) handleContinueEditTool(req continueToolRequest, toolName string) (any, error) {
	targetPath := resolveContinueEditPath(req.Path, req.ActiveFile)
	if delegatedResult, delegated, err := s.tryDelegateContinueEditTool(req, toolName, targetPath); delegated {
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

func (s *localRAGService) tryDelegateContinueEditTool(req continueToolRequest, toolName, targetPath string) (any, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" || strings.TrimSpace(targetPath) == "" {
		return nil, false, nil
	}
	snapshot, ok := s.ensureContinueSessionStore().Get(sessionID)
	if !ok {
		return nil, false, nil
	}
	editContext, ok := resolveContinueOpenFileContext(targetPath, req, snapshot)
	if !ok {
		return nil, false, nil
	}
	registration, ok := s.ensureContinueWorkerStore().Get(sessionID)
	if !ok {
		return nil, false, nil
	}
	delegatedReq, err := buildContinueEditDelegationRequest(toolName, req, editContext)
	if err != nil {
		return nil, false, nil
	}
	requiredCapabilities := requiredContinueWorkerCapabilities(delegatedReq)
	if !continueWorkerSupportsCapabilities(registration, requiredCapabilities) {
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
		"mode":      toolName,
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

type continueEditDelegationContext struct {
	SessionID     string
	WorkspaceRoot string
	ActiveFile    string
	OpenFiles     []string
	TargetPath    string
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
		TargetPath:    strings.TrimSpace(targetPath),
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
	if trimmedActive := strings.TrimSpace(activeFile); trimmedActive != "" {
		if continueComparablePath(trimmedActive, workspaceRoot) == targetComparable {
			return trimmedActive
		}
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
	root := resolveContinueWorkspaceRoot(workspaceRoot)
	return filepath.Clean(filepath.Join(root, trimmed))
}

func buildContinueEditDelegationRequest(toolName string, req continueToolRequest, editContext continueEditDelegationContext) (continueAgentExecuteRequest, error) {
	workerToolName := continueDelegatedToolChoice(toolName)
	if workerToolName == "" {
		return continueAgentExecuteRequest{}, fmt.Errorf("unsupported edit tool: %s", toolName)
	}
	continueReq := continueAgentExecuteRequest{
		RequestID:      newContinueRequestID(),
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
	default:
		return continueAgentExecuteRequest{}, fmt.Errorf("unsupported edit tool: %s", toolName)
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

func (s *localRAGService) bridgeClients() []bridgeClientAdapter {
	return []bridgeClientAdapter{
		continueBridgeAdapter{transport: continueBridgeTransport{}},
	}
}

func registerBridgeRoute(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	if mux == nil || pattern == "" {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if errText := fmt.Sprint(recovered); strings.Contains(errText, "conflicts with pattern") {
				return
			}
			panic(recovered)
		}
	}()
	mux.HandleFunc(pattern, handler)
}

func (s *localRAGService) registerBridgeRoutes(mux *http.ServeMux, reg *SessionRegistry, authHandler *DaemonAuthHandler) {
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
			protected := withRateLimit(reg, wrapped)
			if authHandler != nil {
				protected = authHandler.RequireAuth(protected)
			}
			registerBridgeRoute(mux, route.Pattern, handleOptionsAndCORS(protected))
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
			Pattern: "/api/continue/requests/",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				a.transport.handleRequestState(s, w, r)
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
	s.storeContinueSessionContext(req)
	envelope, err := buildContinueBridgeEnvelope(req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req.SessionID = envelope.Ask.SessionID
	continueBridgeTransport{}.streamContinueEnvelope(s, w, envelope, req, "", "")
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
	req.RequestID = newContinueRequestID()

	chatReq := continueChatRequest{
		SessionID:          req.SessionID,
		Message:            req.Task,
		Mode:               req.Mode,
		Provider:           strings.TrimSpace(req.Provider),
		APIKey:             strings.TrimSpace(req.APIKey),
		Model:              strings.TrimSpace(req.Model),
		URL:                strings.TrimSpace(req.URL),
		LLMCredentials:     cloneLLMCredentialsMap(req.LLMCredentials),
		LLMPoolVersion:     req.LLMPoolVersion,
		LLMPoolUpdatedAtMS: req.LLMPoolUpdatedAtMS,
		LLMPoolUpdatedBy:   strings.TrimSpace(req.LLMPoolUpdatedBy),
	}
	chatReq.Auth.AccessToken = strings.TrimSpace(req.Auth.AccessToken)
	chatReq.Auth.RefreshToken = strings.TrimSpace(req.Auth.RefreshToken)
	chatReq.HQ.BaseURL = strings.TrimSpace(req.HQ.BaseURL)
	chatReq.HQ.Port = strings.TrimSpace(req.HQ.Port)
	chatReq.HQ.UserID = strings.TrimSpace(req.HQ.UserID)
	chatReq.HQ.SessionID = strings.TrimSpace(req.HQ.SessionID)
	chatReq.Workspace.Root = req.Context.WorkspaceRoot
	chatReq.Workspace.Active = req.Context.ActiveFile
	chatReq.Context.SelectedText = req.Context.SelectedText
	chatReq.Context.OpenFiles = req.Context.OpenFiles
	if strings.TrimSpace(chatReq.Mode) == "" {
		chatReq.Mode = "coding"
	}
	s.storeContinueSessionContext(chatReq)
	workflowProfile, inferredPhase := resolveContinueWorkflowProfile(chatReq.Mode, chatReq.Message)
	requestedPhase := strings.TrimSpace(req.RequestedPhase)
	if requestedPhase == "" {
		requestedPhase = inferredPhase
	}
	if registration, ok := s.ensureContinueWorkerStore().Get(chatReq.SessionID); ok {
		requiredCapabilities := requiredContinueWorkerCapabilities(req)
		if continueWorkerSupportsCapabilities(registration, requiredCapabilities) {
			s.ensureContinueSessionStore().Put(buildContinueSessionSnapshot(req.RequestID, chatReq, workflowProfile, requestedPhase, "in_progress", chatReq.Message))
			outcome, err := continueBridgeTransport{}.proxyAgentExecuteToWorker(r.Context(), w, req, registration)
			if err != nil {
				s.ensureContinueSessionStore().Put(buildContinueSessionSnapshot(req.RequestID, chatReq, workflowProfile, requestedPhase, "failed", err.Error()))
				respondJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			status := "completed"
			if outcome.StatusCode >= 400 {
				status = "failed"
			}
			s.ensureContinueSessionStore().Put(buildContinueExecutionSnapshot(req.RequestID, chatReq, workflowProfile, requestedPhase, status, chatReq.Message, outcome.FinalResult, outcome.Events))
			return
		}
	}

	envelope, err := buildContinueBridgeEnvelope(chatReq)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	chatReq.SessionID = envelope.Ask.SessionID
	continueBridgeTransport{}.streamContinueEnvelope(s, w, envelope, chatReq, req.RequestID, "Executing editor-aware coding task")
}

func (continueBridgeTransport) proxyAgentExecuteToWorker(ctx context.Context, w http.ResponseWriter, req continueAgentExecuteRequest, registration continueWorkerRegistration) (continueWorkerCallbackOutcome, error) {
	if err := validateContinueWorkerCallbackURL(registration.CallbackURL); err != nil {
		return continueWorkerCallbackOutcome{}, fmt.Errorf("invalid worker callback URL: %w", err)
	}
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

	outcome := continueWorkerCallbackOutcome{StatusCode: resp.StatusCode}
	if outcome.StatusCode == 0 {
		outcome.StatusCode = http.StatusOK
	}
	outcome.ContentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	if outcome.ContentType == "" {
		outcome.ContentType = "application/x-ndjson"
	}
	w.Header().Set("Content-Type", outcome.ContentType)
	w.WriteHeader(outcome.StatusCode)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	streamOutcome, err := readContinueWorkerResponse(resp.Body, w, false)
	if err != nil {
		return outcome, fmt.Errorf("copy worker response: %w", err)
	}
	outcome.FinalResult = streamOutcome.FinalResult
	outcome.Events = streamOutcome.Events
	outcome.StreamedEvent = streamOutcome.StreamedEvent
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return outcome, nil
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

func (continueBridgeTransport) handleRequestState(s *localRAGService, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requestID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/continue/requests/"))
	if requestID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "request id is required"})
		return
	}
	store := s.ensureContinueSessionStore()
	snapshot, ok := store.GetRequest(requestID)
	if !ok {
		respondJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "request not found"})
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
	if err := validateContinueWorkerCallbackURL(req.CallbackURL); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
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
	llmPoolPopulated := daemonHasPopulatedLLMPoolState()
	respondJSON(w, http.StatusOK, map[string]any{
		"object": "client.capabilities",
		"client": "continue",
		"chat": map[string]any{
			"path":                  "/api/continue/chat",
			"method":                "POST",
			"streaming":             "ndjson",
			"api_key_is_attribute":  true,
			"provider_is_attribute": true,
			"model_is_attribute":    true,
			"url_is_attribute":      true,
		},
		"session_state": map[string]any{
			"path_prefix": "/api/continue/sessions/",
			"method":      "GET",
		},
		"request_state": map[string]any{
			"path_prefix": "/api/continue/requests/",
			"method":      "GET",
		},
		"agent_execute": map[string]any{
			"path":                    "/api/continue/agent/execute",
			"method":                  "POST",
			"streaming":               "ndjson",
			"provider_is_attribute":   true,
			"model_is_attribute":      true,
			"request_id_is_supported": true,
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
		"ide_tools": map[string]any{
			"source":                       "continue",
			"request_forwards_tools":       true,
			"request_supports_tool_choice": true,
			"names":                        continueIDEToolNames(),
			"catalog":                      continueIDEToolCatalog(),
			"supported_phases":             continueSupportedPhases(),
		},
		"features": map[string]any{
			"workspace_context": true,
			"streaming":         true,
			"tool_dispatch":     true,
			"coding_workflows":  true,
			"session_tracking":  true,
			"request_tracking":  true,
			"remote_agent":      true,
			"worker_callback":   true,
		},
		"coding_workflows": continueWorkflowMetadata(),
		"control_model": map[string]any{
			"controller":                             "ai_studio_daemon",
			"editor_worker":                          "continue",
			"provider_is_attribute":                  true,
			"model_is_attribute":                     true,
			"llm_pool_populated":                     llmPoolPopulated,
			"hide_editor_pool_inputs_when_populated": llmPoolPopulated,
			"selection_message":                      "Use the daemon-advertised supported LLM list for AI Studio routing. Continue and daemon should present the same provider/model choices.",
			"supported_llms":                         sopbackend.GetModelCatalog().LLM,
		},
	})
}

func daemonHasPopulatedLLMPoolState() bool {
	daemonSessionStore.mu.Lock()
	defer daemonSessionStore.mu.Unlock()

	for _, session := range daemonSessionStore.data {
		if strings.TrimSpace(session.LLMAPIKey) != "" {
			return true
		}
		if llmCredentialsPoolHasUsableKey(session.LLMCredentials) {
			return true
		}
	}

	return false
}

func llmCredentialsPoolHasUsableKey(pool map[string]llmCredential) bool {
	for _, provider := range []string{"openai", "anthropic", "gemini"} {
		cred, ok := pool[provider]
		if !ok {
			continue
		}
		updatedAt := normalizePoolTimestamp(cred.UpdatedAtMS)
		deletedAt := normalizePoolTimestamp(cred.DeletedAtMS)
		if deletedAt > 0 && deletedAt >= updatedAt {
			continue
		}
		if strings.TrimSpace(cred.APIKey) != "" {
			return true
		}
	}

	return false
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
		"supported_phases":   continueSupportedPhases(),
	}
}

func continueSupportedPhases() []continueIDEPhaseDefinition {
	return []continueIDEPhaseDefinition{
		{ID: "intake", Label: "Intake & Context", ContinueModes: []string{"chat", "plan"}, AvailableTools: []string{"read_file", "read_currently_open_file", "file_glob_search", "ls", "view_repo_map", "request_rule", "read_skill"}},
		{ID: "investigation", Label: "Investigation & Analysis", ContinueModes: []string{"plan", "agent"}, AvailableTools: []string{"read_file", "read_currently_open_file", "read_file_range", "file_glob_search", "grep_search", "ls", "codebase", "view_repo_map", "view_subdirectory", "get_problems", "goto_definition", "goto_type_definition", "get_references", "get_document_symbols", "get_terminal_contents", "fetch_url_content", "search_web"}},
		{ID: "planning", Label: "Planning & Design", ContinueModes: []string{"plan"}, AvailableTools: []string{"read_file", "read_currently_open_file", "read_file_range", "file_glob_search", "grep_search", "ls", "codebase", "view_repo_map", "view_subdirectory", "goto_definition", "goto_type_definition", "get_references", "get_document_symbols", "view_diff", "request_rule", "read_skill", "fetch_url_content", "search_web"}},
		{ID: "execution", Label: "Execution & Implementation", ContinueModes: []string{"agent"}, AvailableTools: []string{"read_file", "read_currently_open_file", "view_diff", "create_new_file", "edit_existing_file", "single_find_and_replace", "multi_edit", "run_terminal_command", "create_rule_block"}},
		{ID: "review", Label: "Review & Validation", ContinueModes: []string{"plan", "agent"}, AvailableTools: []string{"read_file", "read_currently_open_file", "read_file_range", "grep_search", "get_problems", "goto_definition", "goto_type_definition", "get_references", "get_document_symbols", "view_diff", "run_terminal_command", "get_terminal_contents", "request_rule", "read_skill"}},
		{ID: "delivery", Label: "Delivery & Handoff", ContinueModes: []string{"plan", "agent"}, AvailableTools: []string{"read_file", "view_diff", "run_terminal_command", "get_terminal_contents", "create_rule_block"}},
	}
}

func continueIDEToolCatalog() []continueIDEToolDefinition {
	return []continueIDEToolDefinition{
		{Name: "read_file", DisplayName: "Read File", Description: "Read an existing file from the workspace", Category: "read", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"intake", "investigation", "planning", "execution", "review", "delivery"}, Workflows: []string{"feature_implementation", "debugging", "testing_validation", "refactoring", "code_review"}},
		{Name: "read_currently_open_file", DisplayName: "Read Currently Open File", Description: "Read the active file in the IDE", Category: "read", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"intake", "investigation", "planning", "execution", "review"}, Workflows: []string{"feature_implementation", "debugging", "testing_validation", "refactoring", "code_review"}},
		{Name: "read_file_range", DisplayName: "Read File Range", Description: "Read a targeted line range from a file", Category: "read", Readonly: true, Experimental: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning", "review"}, Workflows: []string{"feature_implementation", "debugging", "testing_validation", "refactoring", "code_review"}},
		{Name: "file_glob_search", DisplayName: "Glob File Search", Description: "Find files by glob pattern", Category: "search", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"intake", "investigation", "planning"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "grep_search", DisplayName: "Grep Search", Description: "Search repository contents with ripgrep", Category: "search", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning", "review"}, Workflows: []string{"feature_implementation", "debugging", "testing_validation", "refactoring", "code_review"}},
		{Name: "ls", DisplayName: "List Directory", Description: "List files and folders in the workspace", Category: "search", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"intake", "investigation", "planning"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "codebase", DisplayName: "Codebase Search", Description: "Semantically search the codebase", Category: "search", Readonly: true, Experimental: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "view_repo_map", DisplayName: "View Repo Map", Description: "Inspect repository structure summaries", Category: "search", Readonly: true, Experimental: true, Modes: []string{"plan", "agent"}, Phases: []string{"intake", "investigation", "planning"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "view_subdirectory", DisplayName: "View Subdirectory", Description: "Inspect a subdirectory summary", Category: "search", Readonly: true, Experimental: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "get_problems", DisplayName: "Get Problems", Description: "Read IDE diagnostics for a file", Category: "diagnostics", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "review"}, Workflows: []string{"debugging", "testing_validation", "refactoring", "code_review"}},
		{Name: "goto_definition", DisplayName: "Go To Definition", Description: "Resolve symbol definitions", Category: "navigation", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning", "review"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "goto_type_definition", DisplayName: "Go To Type Definition", Description: "Resolve symbol type definitions", Category: "navigation", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning", "review"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "get_references", DisplayName: "Get References", Description: "Find symbol references", Category: "navigation", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning", "review"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "get_document_symbols", DisplayName: "Get Document Symbols", Description: "List document symbols for a file", Category: "navigation", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning", "review"}, Workflows: []string{"feature_implementation", "debugging", "refactoring", "code_review"}},
		{Name: "view_diff", DisplayName: "View Diff", Description: "Inspect the current working diff", Category: "review", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"planning", "execution", "review", "delivery"}, Workflows: []string{"feature_implementation", "debugging", "testing_validation", "refactoring", "code_review"}},
		{Name: "create_new_file", DisplayName: "Create New File", Description: "Create a new file in the workspace", Category: "edit", Readonly: false, Modes: []string{"agent"}, Phases: []string{"execution"}, Workflows: []string{"feature_implementation", "refactoring"}},
		{Name: "edit_existing_file", DisplayName: "Edit Existing File", Description: "Edit an existing file", Category: "edit", Readonly: false, Modes: []string{"agent"}, Phases: []string{"execution"}, Workflows: []string{"feature_implementation", "debugging", "refactoring"}},
		{Name: "single_find_and_replace", DisplayName: "Single Find and Replace", Description: "Apply one targeted replacement", Category: "edit", Readonly: false, Modes: []string{"agent"}, Phases: []string{"execution"}, Workflows: []string{"feature_implementation", "debugging", "refactoring"}},
		{Name: "multi_edit", DisplayName: "Multi Edit", Description: "Apply multi-location edits in one action", Category: "edit", Readonly: false, Modes: []string{"agent"}, Phases: []string{"execution"}, Workflows: []string{"feature_implementation", "debugging", "refactoring"}},
		{Name: "run_terminal_command", DisplayName: "Run Terminal Command", Description: "Run a terminal command in the workspace", Category: "execution", Readonly: false, Modes: []string{"agent"}, Phases: []string{"execution", "review", "delivery"}, Workflows: []string{"feature_implementation", "debugging", "testing_validation", "refactoring"}},
		{Name: "get_terminal_contents", DisplayName: "Get Terminal Contents", Description: "Inspect current terminal contents", Category: "execution", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "review", "delivery"}, Workflows: []string{"debugging", "testing_validation", "code_review"}},
		{Name: "request_rule", DisplayName: "Request Rule", Description: "Load rule or policy guidance", Category: "guidance", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"intake", "planning", "review"}, Workflows: []string{"feature_implementation", "refactoring", "code_review"}},
		{Name: "read_skill", DisplayName: "Read Skill", Description: "Read skill instructions", Category: "guidance", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"intake", "planning", "review"}, Workflows: []string{"feature_implementation", "refactoring", "code_review"}},
		{Name: "create_rule_block", DisplayName: "Create Rule Block", Description: "Create a new rule block", Category: "guidance", Readonly: false, Modes: []string{"agent"}, Phases: []string{"execution", "delivery"}, Workflows: []string{"feature_implementation", "documentation"}},
		{Name: "fetch_url_content", DisplayName: "Fetch URL Content", Description: "Read content from a URL", Category: "research", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning"}, Workflows: []string{"feature_implementation", "debugging", "code_review"}},
		{Name: "search_web", DisplayName: "Search Web", Description: "Search the web for supporting context", Category: "research", Readonly: true, Modes: []string{"plan", "agent"}, Phases: []string{"investigation", "planning"}, Workflows: []string{"feature_implementation", "debugging", "code_review"}},
	}
}

func continueIDEToolNames() []string {
	tools := continueIDEToolCatalog()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func continuePhaseToolBundles() map[string][]string {
	bundles := map[string][]string{}
	for _, phase := range continueSupportedPhases() {
		bundles[phase.ID] = append([]string(nil), phase.AvailableTools...)
	}
	return bundles
}

func continueToolsForPhase(phase string) []string {
	phase = strings.ToLower(strings.TrimSpace(phase))
	if phase == "" {
		return nil
	}
	for _, item := range continueSupportedPhases() {
		if item.ID == phase {
			return append([]string(nil), item.AvailableTools...)
		}
	}
	return nil
}

func continueRequestToolNames(tools []continueChatTool) []string {
	if len(tools) == 0 {
		return nil
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func intersectContinueToolNames(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(right))
	for _, name := range right {
		allowed[name] = true
	}
	matched := []string{}
	for _, name := range left {
		if allowed[name] {
			matched = append(matched, name)
		}
	}
	sort.Strings(matched)
	return matched
}

func normalizeContinueOpenFiles(openFiles []string) []string {
	if len(openFiles) == 0 {
		return nil
	}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(openFiles))
	for _, path := range openFiles {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func continuePromptOpenFiles(openFiles []string) string {
	normalized := normalizeContinueOpenFiles(openFiles)
	if len(normalized) == 0 {
		return ""
	}
	const maxPromptOpenFiles = 3
	if len(normalized) <= maxPromptOpenFiles {
		return strings.Join(normalized, ",")
	}
	return fmt.Sprintf("%s,+%d_more", strings.Join(normalized[:maxPromptOpenFiles], ","), len(normalized)-maxPromptOpenFiles)
}

func continuePromptSelectedText(selectedText string) string {
	trimmed := strings.TrimSpace(selectedText)
	if trimmed == "" {
		return ""
	}
	const maxPromptSelectedTextRunes = 280
	runes := []rune(trimmed)
	if len(runes) <= maxPromptSelectedTextRunes {
		return trimmed
	}
	return string(runes[:maxPromptSelectedTextRunes]) + "..."
}

func inferContinueRoutingHint(req continueChatRequest, workflowProfile continueWorkflowProfile, requestedPhase string) continueRoutingHint {
	if strings.TrimSpace(req.Provider) != "" || strings.TrimSpace(req.Model) != "" {
		return continueRoutingHint{}
	}

	message := strings.ToLower(strings.TrimSpace(req.Message))
	selectedText := strings.TrimSpace(req.Context.SelectedText)
	openFiles := normalizeContinueOpenFiles(req.Context.OpenFiles)
	hasSelection := selectedText != ""
	messageRunes := len([]rune(message))

	if hasSelection && len(openFiles) <= 2 && messageRunes > 0 && messageRunes <= 160 &&
		containsAnyWorkflowSignal(message, "explain", "summarize", "summary", "docstring", "comment", "rename") {
		return continueRoutingHint{Tier: "compact", Reason: "narrow_selection_request"}
	}

	if requestedPhase == "planning" || len(openFiles) >= 4 ||
		containsAnyWorkflowSignal(message, "architecture", "design", "debug", "broken", "failure", "root cause", "multi-file") ||
		containsAnyWorkflowSignal(strings.ToLower(workflowProfile.TaskType), "implementation", "debugging") {
		return continueRoutingHint{Tier: "deliberate", Reason: "multi_file_or_complex_reasoning"}
	}

	return continueRoutingHint{}
}

func inferAutoModelSelection(routingHint continueRoutingHint) continueAutoModelSelection {
	if strings.TrimSpace(routingHint.Tier) == "" {
		return continueAutoModelSelection{}
	}

	type candidate struct {
		provider      string
		model         string
		thinkingScore int
	}

	catalog := sopbackend.GetModelCatalog()
	candidates := make([]candidate, 0)
	for _, group := range catalog.LLM {
		for _, option := range group.Options {
			value := strings.TrimSpace(option.Value)
			if value == "" {
				continue
			}
			provider, model, ok := strings.Cut(value, ":")
			if !ok {
				continue
			}
			provider = strings.TrimSpace(provider)
			model = strings.TrimSpace(model)
			if provider == "" || model == "" {
				continue
			}
			candidates = append(candidates, candidate{
				provider:      provider,
				model:         model,
				thinkingScore: thinkingLevelScore(option.DefaultThinkingLevel),
			})
		}
	}
	if len(candidates) == 0 {
		return continueAutoModelSelection{}
	}

	pick := candidates[0]
	tier := strings.ToLower(strings.TrimSpace(routingHint.Tier))
	switch tier {
	case "compact":
		for _, option := range candidates[1:] {
			if option.thinkingScore < pick.thinkingScore {
				pick = option
			}
		}
		return continueAutoModelSelection{Provider: pick.provider, Model: pick.model, Tier: "compact", Reason: "auto_select_lower_thinking_model"}
	case "deliberate":
		for _, option := range candidates[1:] {
			if option.thinkingScore > pick.thinkingScore {
				pick = option
			}
		}
		return continueAutoModelSelection{Provider: pick.provider, Model: pick.model, Tier: "deliberate", Reason: "auto_select_higher_thinking_model"}
	default:
		return continueAutoModelSelection{}
	}
}

func thinkingLevelScore(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low":
		return 1
	case "high":
		return 3
	default:
		return 2
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
	availableToolNames := continueRequestToolNames(req.Tools)
	workflowProfile, requestedPhase := resolveContinueWorkflowProfile(req.Mode, req.Message)
	routingHint := inferContinueRoutingHint(req, workflowProfile, requestedPhase)
	if mode := strings.TrimSpace(req.Mode); mode != "" {
		variables["continue_mode"] = mode
	}
	if activeFile := strings.TrimSpace(req.Workspace.Active); activeFile != "" {
		variables["active_file"] = activeFile
	}
	if selectedText := strings.TrimSpace(req.Context.SelectedText); selectedText != "" {
		variables["selected_text"] = selectedText
	}
	if normalizedOpenFiles := normalizeContinueOpenFiles(req.Context.OpenFiles); len(normalizedOpenFiles) > 0 {
		variables["open_files"] = normalizedOpenFiles
	}
	if len(availableToolNames) > 0 {
		variables["continue_available_tools"] = availableToolNames
	}
	if workflowProfile.ID != "" {
		variables["coding_workflow_profile"] = workflowProfile.ID
		variables["coding_task_type"] = workflowProfile.TaskType
	}
	if requestedPhase != "" {
		variables["requested_phase"] = requestedPhase
		phaseTools := continueToolsForPhase(requestedPhase)
		if len(phaseTools) > 0 {
			variables["continue_phase_tools"] = phaseTools
			if len(availableToolNames) > 0 {
				variables["continue_phase_available_tools"] = intersectContinueToolNames(phaseTools, availableToolNames)
			}
		}
	}
	if toolChoice := strings.TrimSpace(req.ToolChoice.Function.Name); toolChoice != "" {
		variables["continue_tool_choice"] = toolChoice
	}
	if routingHint.Tier != "" {
		variables["continue_routing_tier"] = routingHint.Tier
		variables["continue_routing_reason"] = routingHint.Reason
	}
	providerName := strings.TrimSpace(req.Provider)
	apiKey := strings.TrimSpace(req.APIKey)
	modelName := strings.TrimSpace(req.Model)
	baseURL := strings.TrimSpace(req.URL)
	if providerName == "" && modelName == "" {
		autoSelection := inferAutoModelSelection(routingHint)
		if autoSelection.Provider != "" && autoSelection.Model != "" {
			providerName = autoSelection.Provider
			modelName = autoSelection.Model
			variables["continue_model_routing_source"] = "daemon_auto"
			variables["continue_model_routing_tier"] = autoSelection.Tier
			variables["continue_model_routing_reason"] = autoSelection.Reason
		}
	}
	if providerName != "" {
		variables["llm_provider"] = providerName
	}
	if modelName != "" {
		variables["llm_model"] = modelName
	}
	if baseURL != "" {
		variables["llm_url"] = baseURL
	}
	return bridgeChatEnvelope{
		Ask: bridgeAskRequest{
			SessionID:    req.SessionID,
			UserPrompt:   req.Message,
			FullPrompt:   buildContinueBridgePrompt(req, workflowProfile, requestedPhase, routingHint),
			ProviderName: providerName,
			APIKey:       apiKey,
			BaseURL:      baseURL,
			ModelName:    modelName,
			AccessToken:  strings.TrimSpace(req.Auth.AccessToken),
			RefreshToken: strings.TrimSpace(req.Auth.RefreshToken),
			HQBaseURL:    strings.TrimSpace(req.HQ.BaseURL),
			HQPort:       strings.TrimSpace(req.HQ.Port),
			HQUserID:     strings.TrimSpace(req.HQ.UserID),
			HQSessionID:  strings.TrimSpace(req.HQ.SessionID),
			Variables:    variables,
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

func buildContinueSessionSnapshot(requestID string, req continueChatRequest, workflowProfile continueWorkflowProfile, requestedPhase, status, message string) continueSessionSnapshot {
	phase := requestedPhase
	if phase == "" && len(workflowProfile.DefaultPhasePlan) > 0 {
		phase = workflowProfile.DefaultPhasePlan[0]
	}
	return continueSessionSnapshot{
		RequestID:       strings.TrimSpace(requestID),
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

func buildContinueExecutionSnapshot(requestID string, req continueChatRequest, workflowProfile continueWorkflowProfile, requestedPhase, status, message string, result *continueExecutionResult, events []continueExecutionEvent) continueSessionSnapshot {
	snapshot := buildContinueSessionSnapshot(requestID, req, workflowProfile, requestedPhase, status, message)
	if result != nil {
		resultCopy := cloneContinueExecutionResult(*result)
		snapshot.ExecutionResult = &resultCopy
	}
	if len(events) > 0 {
		snapshot.ExecutionEvents = cloneContinueExecutionEvents(events)
	}
	return snapshot
}

func cloneContinueExecutionEvents(src []continueExecutionEvent) []continueExecutionEvent {
	if len(src) == 0 {
		return nil
	}
	cloned := make([]continueExecutionEvent, 0, len(src))
	for _, event := range src {
		cloned = append(cloned, continueExecutionEvent{
			Type:          event.Type,
			Status:        event.Status,
			Message:       event.Message,
			Content:       event.Content,
			Error:         event.Error,
			FilePath:      event.FilePath,
			CRUDOperation: event.CRUDOperation,
			DiffSummary:   event.DiffSummary,
			Metadata:      cloneAnyMap(event.Metadata),
		})
	}
	return cloned
}

func cloneContinueExecutionResult(src continueExecutionResult) continueExecutionResult {
	cloned := continueExecutionResult{
		Status:   src.Status,
		Summary:  src.Summary,
		Metadata: cloneAnyMap(src.Metadata),
	}
	if len(src.ModifiedFiles) > 0 {
		cloned.ModifiedFiles = make([]continueExecutionModifiedFile, 0, len(src.ModifiedFiles))
		for _, file := range src.ModifiedFiles {
			cloned.ModifiedFiles = append(cloned.ModifiedFiles, continueExecutionModifiedFile{
				FilePath:        file.FilePath,
				CRUDOperation:   file.CRUDOperation,
				DiffSummary:     file.DiffSummary,
				DiffDelta:       file.DiffDelta,
				DiffFormat:      file.DiffFormat,
				DiffArtifactRef: file.DiffArtifactRef,
				Metadata:        cloneAnyMap(file.Metadata),
			})
		}
	}
	if len(src.Artifacts) > 0 {
		cloned.Artifacts = make([]continueExecutionArtifact, 0, len(src.Artifacts))
		for _, artifact := range src.Artifacts {
			cloned.Artifacts = append(cloned.Artifacts, continueExecutionArtifact{
				ID:          artifact.ID,
				Kind:        artifact.Kind,
				Path:        artifact.Path,
				Description: artifact.Description,
				Metadata:    cloneAnyMap(artifact.Metadata),
			})
		}
	}
	return cloned
}

func buildContinueExecutionResultFromAnswer(answer string) *continueExecutionResult {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return &continueExecutionResult{Status: "completed"}
	}
	if result, ok := parseContinueExecutionResult([]byte(trimmed)); ok {
		resultCopy := cloneContinueExecutionResult(result)
		return &resultCopy
	}
	return &continueExecutionResult{
		Status:  "completed",
		Summary: trimmed,
	}
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

func readContinueWorkerResponse(body io.Reader, sink io.Writer, collectRaw bool) (continueWorkerCallbackOutcome, error) {
	reader := bufio.NewReader(body)
	var raw strings.Builder
	outcome := continueWorkerCallbackOutcome{}
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			outcome.StreamedEvent = true
			if sink != nil {
				if _, writeErr := sink.Write(line); writeErr != nil {
					return outcome, writeErr
				}
			}
			if collectRaw {
				captureContinueWorkerRaw(&raw, line)
			}
			captureContinueWorkerEvent(&outcome, line)
			captureContinueWorkerResult(&outcome, line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return outcome, err
		}
	}
	if collectRaw {
		outcome.RawBody = raw.String()
		if outcome.FinalResult == nil {
			trimmed := strings.TrimSpace(outcome.RawBody)
			if result, ok := parseContinueExecutionResult([]byte(trimmed)); ok {
				resultCopy := cloneContinueExecutionResult(result)
				outcome.FinalResult = &resultCopy
			}
		}
	}
	return outcome, nil
}

func captureContinueWorkerRaw(raw *strings.Builder, rawLine []byte) {
	if raw == nil || len(rawLine) == 0 || raw.Len() >= continueWorkerRawBodyLimit {
		return
	}

	trimmed := strings.TrimSpace(string(rawLine))
	if trimmed == "" {
		return
	}
	if _, ok := parseContinueExecutionResult([]byte(trimmed)); ok {
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
			appendContinueWorkerRawChunk(raw, envelope.Message)
			return
		case "content":
			appendContinueWorkerRawChunk(raw, envelope.Content)
			return
		case "error":
			appendContinueWorkerRawChunk(raw, envelope.Error)
			if strings.TrimSpace(envelope.Error) == "" {
				appendContinueWorkerRawChunk(raw, envelope.Message)
			}
			return
		}
	}

	appendContinueWorkerRawChunk(raw, trimmed)
}

func appendContinueWorkerRawChunk(raw *strings.Builder, chunk string) {
	if raw == nil {
		return
	}
	chunk = strings.TrimSpace(chunk)
	if chunk == "" || raw.Len() >= continueWorkerRawBodyLimit {
		return
	}
	if raw.Len() > 0 {
		chunk = "\n" + chunk
	}
	remaining := continueWorkerRawBodyLimit - raw.Len()
	if len(chunk) > remaining {
		if remaining <= len("\n...[truncated]") {
			return
		}
		raw.WriteString(chunk[:remaining-len("\n...[truncated]")])
		raw.WriteString("\n...[truncated]")
		return
	}
	raw.WriteString(chunk)
}

func captureContinueWorkerEvent(outcome *continueWorkerCallbackOutcome, rawLine []byte) {
	if outcome == nil {
		return
	}
	trimmed := strings.TrimSpace(string(rawLine))
	if trimmed == "" {
		return
	}

	var envelope struct {
		Type          string                  `json:"type"`
		Status        string                  `json:"status"`
		Message       string                  `json:"message"`
		Content       string                  `json:"content"`
		Error         string                  `json:"error"`
		FilePath      string                  `json:"file_path"`
		CRUDOperation string                  `json:"crud_operation"`
		DiffSummary   string                  `json:"diff_summary"`
		Metadata      map[string]any          `json:"metadata"`
		Result        continueExecutionResult `json:"result"`
		File          struct {
			Path      string `json:"path"`
			Operation string `json:"operation"`
			Summary   string `json:"summary"`
		} `json:"file"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return
	}

	typ := strings.TrimSpace(envelope.Type)
	if typ == "" {
		return
	}
	event := continueExecutionEvent{
		Type:          typ,
		Status:        strings.TrimSpace(envelope.Status),
		Message:       strings.TrimSpace(envelope.Message),
		Content:       strings.TrimSpace(envelope.Content),
		Error:         strings.TrimSpace(envelope.Error),
		FilePath:      strings.TrimSpace(envelope.FilePath),
		CRUDOperation: strings.TrimSpace(envelope.CRUDOperation),
		DiffSummary:   strings.TrimSpace(envelope.DiffSummary),
		Metadata:      cloneAnyMap(envelope.Metadata),
	}

	if event.FilePath == "" {
		event.FilePath = strings.TrimSpace(envelope.File.Path)
	}
	if event.CRUDOperation == "" {
		event.CRUDOperation = strings.TrimSpace(envelope.File.Operation)
	}
	if event.DiffSummary == "" {
		event.DiffSummary = strings.TrimSpace(envelope.File.Summary)
	}

	if typ == "done" {
		if event.Status == "" {
			event.Status = strings.TrimSpace(envelope.Result.Status)
		}
		if event.Message == "" {
			event.Message = strings.TrimSpace(envelope.Result.Summary)
		}
	}

	event.Status = clampContinueEventText(event.Status)
	event.Message = clampContinueEventText(event.Message)
	event.Content = clampContinueEventText(event.Content)
	event.Error = clampContinueEventText(event.Error)
	event.FilePath = clampContinueEventText(event.FilePath)
	event.CRUDOperation = clampContinueEventText(event.CRUDOperation)
	event.DiffSummary = clampContinueEventText(event.DiffSummary)

	appendContinueExecutionEvent(outcome, event)
}

func appendContinueExecutionEvent(outcome *continueWorkerCallbackOutcome, event continueExecutionEvent) {
	if outcome == nil || strings.TrimSpace(event.Type) == "" {
		return
	}
	if len(outcome.Events) >= continueExecutionEventLimit {
		return
	}
	outcome.Events = append(outcome.Events, event)
}

func clampContinueEventText(value string) string {
	if len(value) <= continueExecutionEventTextLimit {
		return value
	}
	if continueExecutionEventTextLimit <= len("...[truncated]") {
		return value[:continueExecutionEventTextLimit]
	}
	return value[:continueExecutionEventTextLimit-len("...[truncated]")] + "...[truncated]"
}

func captureContinueWorkerResult(outcome *continueWorkerCallbackOutcome, rawLine []byte) {
	trimmed := strings.TrimSpace(string(rawLine))
	if trimmed == "" {
		return
	}
	if result, ok := parseContinueExecutionResult([]byte(trimmed)); ok {
		resultCopy := cloneContinueExecutionResult(result)
		outcome.FinalResult = &resultCopy
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

func (s *localRAGService) continueToolNames() []string {
	handlers := s.continueToolHandlers()
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

func (continueBridgeTransport) streamContinueEnvelope(s *localRAGService, w http.ResponseWriter, envelope bridgeChatEnvelope, req continueChatRequest, requestID, startingMessage string) {
	store := s.ensureContinueSessionStore()
	workflowProfile, requestedPhase := resolveContinueWorkflowProfile(req.Mode, req.Message)
	store.Put(buildContinueSessionSnapshot(requestID, req, workflowProfile, requestedPhase, "in_progress", req.Message))

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
		store.Put(buildContinueSessionSnapshot(requestID, req, workflowProfile, requestedPhase, "failed", err.Error()))
		sendEvent("error", err.Error())
		sendEvent("done", nil)
		return
	}
	finalResult := buildContinueExecutionResultFromAnswer(answer)
	localEvents := []continueExecutionEvent{{Type: "done", Status: "completed"}}
	if summary := strings.TrimSpace(answer); summary != "" {
		localEvents = append([]continueExecutionEvent{{Type: "content", Content: clampContinueEventText(summary)}}, localEvents...)
	}
	store.Put(buildContinueExecutionSnapshot(requestID, req, workflowProfile, requestedPhase, "completed", req.Message, finalResult, localEvents))
	if strings.TrimSpace(answer) != "" {
		sendEvent("content", answer)
	}
	sendEvent("done", nil)
}

func buildContinueBridgePrompt(req continueChatRequest, workflowProfile continueWorkflowProfile, requestedPhase string, routingHint continueRoutingHint) string {
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
		phaseTools := continueToolsForPhase(requestedPhase)
		if len(phaseTools) > 0 {
			query += fmt.Sprintf("\n[continue_phase_tools=%s]", strings.Join(phaseTools, ","))
		}
	}
	availableToolNames := continueRequestToolNames(req.Tools)
	if len(availableToolNames) > 0 {
		query += fmt.Sprintf("\n[continue_available_tools=%s]", strings.Join(availableToolNames, ","))
		if requestedPhase != "" {
			phaseAvailable := intersectContinueToolNames(continueToolsForPhase(requestedPhase), availableToolNames)
			if len(phaseAvailable) > 0 {
				query += fmt.Sprintf("\n[continue_phase_available_tools=%s]", strings.Join(phaseAvailable, ","))
			}
		}
	}
	if toolChoice := strings.TrimSpace(req.ToolChoice.Function.Name); toolChoice != "" {
		query += fmt.Sprintf("\n[continue_tool_choice=%s]", toolChoice)
	}
	if routingHint.Tier != "" {
		query += fmt.Sprintf("\n[continue_routing_tier=%s]", routingHint.Tier)
		query += fmt.Sprintf("\n[continue_routing_reason=%s]", routingHint.Reason)
	}
	if provider := strings.TrimSpace(req.Provider); provider != "" {
		query += fmt.Sprintf("\n[llm_provider=%s]", provider)
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		query += fmt.Sprintf("\n[llm_model=%s]", model)
	}
	if strings.TrimSpace(req.Workspace.Root) != "" {
		query += fmt.Sprintf("\n[workspace_root=%s]", req.Workspace.Root)
	}
	if strings.TrimSpace(req.Workspace.Active) != "" {
		query += fmt.Sprintf("\n[active_file=%s]", req.Workspace.Active)
	}
	if promptSelectedText := continuePromptSelectedText(req.Context.SelectedText); promptSelectedText != "" {
		query += fmt.Sprintf("\n[selected_text=%s]", promptSelectedText)
	}
	if promptOpenFiles := continuePromptOpenFiles(req.Context.OpenFiles); promptOpenFiles != "" {
		query += fmt.Sprintf("\n[open_files=%s]", promptOpenFiles)
	}
	return query
}
