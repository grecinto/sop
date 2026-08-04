package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
	"github.com/sharedcode/sop/tools/sopbackend"
)

const (
	defaultDelegationPollInterval = 5 * time.Second
	defaultDelegationTTL          = 12 * time.Hour
	defaultDelegationProgressTTL  = 1 * time.Hour
)

var (
	continueDelegationSupervisorHookOnce sync.Once
	continueDelegationSupervisorMu       sync.Mutex
	continueDelegationBackends           = make(map[*continueIDEWorkerBackend]struct{})
)

type continueIDEWorkerBackend struct {
	service      *localRAGService
	mu           sync.Mutex
	delegations  map[string]*delegationRecord
	pollInterval time.Duration
	timeout      time.Duration
}

type delegationRecord struct {
	ID            string
	Status        string
	Progress      string
	PartialResult string
	Message       string
	Error         string
	Cancelled     bool
	StartedAt     time.Time
	LastUpdated   time.Time
	Timeout       time.Duration
}

type daemonShellIDEWorkerBackend struct {
	shellSessions *ptyShellSessionManager
}

type compositeIDEWorkerBackend struct {
	backends []ideworker.Backend
}

func newDefaultIDEWorkerBackend(service *localRAGService) ideworker.Backend {
	if service == nil {
		return compositeIDEWorkerBackend{}
	}
	return newCompositeIDEWorkerBackend(
		newContinueIDEWorkerBackend(service),
		newDaemonShellIDEWorkerBackend(service.shellSessions),
	)
}

func newCompositeIDEWorkerBackend(backends ...ideworker.Backend) ideworker.Backend {
	filtered := make([]ideworker.Backend, 0, len(backends))
	for _, backend := range backends {
		if backend != nil {
			filtered = append(filtered, backend)
		}
	}
	return compositeIDEWorkerBackend{backends: filtered}
}

func newDaemonShellIDEWorkerBackend(shellSessions *ptyShellSessionManager) ideworker.Backend {
	return daemonShellIDEWorkerBackend{shellSessions: shellSessions}
}

func newContinueIDEWorkerBackend(service *localRAGService) ideworker.Backend {
	backend := &continueIDEWorkerBackend{service: service}
	backend.registerBackgroundSupervisorHook()
	registerContinueDelegationBackend(backend)
	return backend
}

func (b compositeIDEWorkerBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	state := ideworker.SessionState{SessionID: strings.TrimSpace(sessionID)}
	var lastErr error
	mergedAny := false
	for _, backend := range b.backends {
		if backend == nil {
			continue
		}
		backendState, err := backend.InspectSession(ctx, sessionID)
		if err != nil {
			lastErr = err
			continue
		}
		state = mergeIDEWorkerSessionState(state, backendState)
		mergedAny = true
	}
	if lastErr != nil && !mergedAny {
		return state, lastErr
	}
	return state, nil
}

func (b compositeIDEWorkerBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	var errs []string
	for _, backend := range b.backends {
		if backend == nil {
			continue
		}
		result, err := backend.ExecuteTask(ctx, req)
		if err == nil {
			return result, nil
		}
		errMsg := strings.TrimSpace(err.Error())
		if errMsg != "" {
			errs = append(errs, errMsg)
		}
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("IDE worker backend unavailable")
	}
	if len(errs) == 1 {
		return "", fmt.Errorf("%s", errs[0])
	}
	return "", fmt.Errorf("no IDE worker backend could execute task: %s", strings.Join(errs, "; "))
}

func (b compositeIDEWorkerBackend) StartTask(ctx context.Context, req ideworker.TaskRequest) (ideworker.DelegationHandle, error) {
	var errs []string
	for _, backend := range b.backends {
		if backend == nil {
			continue
		}
		handle, err := backend.StartTask(ctx, req)
		if err == nil {
			return handle, nil
		}
		errMsg := strings.TrimSpace(err.Error())
		if errMsg != "" {
			errs = append(errs, errMsg)
		}
	}
	if len(errs) == 0 {
		return ideworker.DelegationHandle{}, fmt.Errorf("IDE worker backend unavailable")
	}
	if len(errs) == 1 {
		return ideworker.DelegationHandle{}, fmt.Errorf("%s", errs[0])
	}
	return ideworker.DelegationHandle{}, fmt.Errorf("no IDE worker backend could start task: %s", strings.Join(errs, "; "))
}

func (b compositeIDEWorkerBackend) GetTaskStatus(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	var errs []string
	for _, backend := range b.backends {
		if backend == nil {
			continue
		}
		status, err := backend.GetTaskStatus(ctx, delegationID)
		if err == nil {
			return status, nil
		}
		errMsg := strings.TrimSpace(err.Error())
		if errMsg != "" {
			errs = append(errs, errMsg)
		}
	}
	if len(errs) == 0 {
		return ideworker.DelegationStatus{}, fmt.Errorf("IDE worker backend unavailable")
	}
	if len(errs) == 1 {
		return ideworker.DelegationStatus{}, fmt.Errorf("%s", errs[0])
	}
	return ideworker.DelegationStatus{}, fmt.Errorf("no IDE worker backend could report task status: %s", strings.Join(errs, "; "))
}

func (b compositeIDEWorkerBackend) CancelTask(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	var errs []string
	for _, backend := range b.backends {
		if backend == nil {
			continue
		}
		status, err := backend.CancelTask(ctx, delegationID)
		if err == nil {
			return status, nil
		}
		errMsg := strings.TrimSpace(err.Error())
		if errMsg != "" {
			errs = append(errs, errMsg)
		}
	}
	if len(errs) == 0 {
		return ideworker.DelegationStatus{}, fmt.Errorf("IDE worker backend unavailable")
	}
	if len(errs) == 1 {
		return ideworker.DelegationStatus{}, fmt.Errorf("%s", errs[0])
	}
	return ideworker.DelegationStatus{}, fmt.Errorf("no IDE worker backend could cancel task: %s", strings.Join(errs, "; "))
}

func (b daemonShellIDEWorkerBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	_ = ctx
	state := ideworker.SessionState{SessionID: strings.TrimSpace(sessionID)}
	if b.shellSessions == nil || state.SessionID == "" {
		return state, nil
	}

	b.shellSessions.mu.Lock()
	_, ok := b.shellSessions.sessions[state.SessionID]
	b.shellSessions.mu.Unlock()
	if !ok {
		return state, nil
	}

	state.Connected = true
	state.IDE = "daemon_shell"
	state.WorkerCapabilities = []string{"terminal_execution", "workspace_context"}
	if pwdOutput, _, err := b.shellSessions.Run(state.SessionID, "pwd", 2*time.Second); err == nil {
		state.WorkspaceRoot = strings.TrimSpace(pwdOutput)
	}
	return state, nil
}

func (b daemonShellIDEWorkerBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	_ = ctx
	_ = req
	return "", fmt.Errorf("daemon shell backend does not support editor-native delegation")
}

func (b daemonShellIDEWorkerBackend) StartTask(ctx context.Context, req ideworker.TaskRequest) (ideworker.DelegationHandle, error) {
	_ = ctx
	_ = req
	return ideworker.DelegationHandle{}, fmt.Errorf("daemon shell backend does not support editor-native delegation")
}

func (b daemonShellIDEWorkerBackend) GetTaskStatus(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	_ = ctx
	_ = delegationID
	return ideworker.DelegationStatus{}, fmt.Errorf("daemon shell backend does not support editor-native delegation")
}

func (b daemonShellIDEWorkerBackend) CancelTask(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	_ = ctx
	_ = delegationID
	return ideworker.DelegationStatus{}, fmt.Errorf("daemon shell backend does not support editor-native delegation")
}

func (b *continueIDEWorkerBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	_ = ctx
	state := ideworker.SessionState{SessionID: strings.TrimSpace(sessionID)}
	if b.service == nil || state.SessionID == "" {
		return state, nil
	}
	state.SupportedPhases = convertContinueSupportedPhases()
	if registration, ok := b.service.ensureContinueWorkerStore().Get(state.SessionID); ok {
		state.Connected = true
		state.IDE = strings.TrimSpace(registration.IDE)
		state.WorkerCapabilities = append([]string(nil), registration.Capabilities...)
	}
	if snapshot, ok := b.service.ensureContinueSessionStore().Get(state.SessionID); ok {
		if state.SessionID == "" {
			state.SessionID = snapshot.SessionID
		}
		state.Mode = strings.TrimSpace(snapshot.Mode)
		state.WorkflowProfile = strings.TrimSpace(snapshot.WorkflowProfile)
		state.RequestedPhase = strings.TrimSpace(snapshot.Phase)
		state.PhaseTools = continueToolsForPhase(state.RequestedPhase)
		state.WorkspaceRoot = strings.TrimSpace(snapshot.WorkspaceRoot)
		state.ActiveFile = strings.TrimSpace(snapshot.ActiveFile)
		state.OpenFiles = append([]string(nil), snapshot.OpenFiles...)
	}
	return state, nil
}

func convertContinueSupportedPhases() []ideworker.PhaseDefinition {
	phases := continueSupportedPhases()
	converted := make([]ideworker.PhaseDefinition, 0, len(phases))
	for _, phase := range phases {
		converted = append(converted, ideworker.PhaseDefinition{
			ID:             phase.ID,
			Label:          phase.Label,
			ContinueModes:  append([]string(nil), phase.ContinueModes...),
			AvailableTools: append([]string(nil), phase.AvailableTools...),
		})
	}
	return converted
}

func (b *continueIDEWorkerBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	return b.executeDelegatedTask(ctx, req)
}

func (b *continueIDEWorkerBackend) StartTask(ctx context.Context, req ideworker.TaskRequest) (ideworker.DelegationHandle, error) {
	if b.service == nil {
		return ideworker.DelegationHandle{}, fmt.Errorf("IDE worker backend unavailable")
	}
	req.RequestID = newContinueRequestID()
	id := req.RequestID
	now := time.Now()
	record := &delegationRecord{
		ID:          id,
		Status:      "running",
		Progress:    "submitted",
		Message:     "delegation started",
		StartedAt:   now,
		LastUpdated: now,
		Timeout:     b.getTTL(),
	}
	b.mu.Lock()
	if b.delegations == nil {
		b.delegations = make(map[string]*delegationRecord)
	}
	b.delegations[id] = record
	b.mu.Unlock()

	go func() {
		result, err := b.executeDelegatedTask(ctx, req)
		b.mu.Lock()
		defer b.mu.Unlock()
		if record.Cancelled {
			record.Status = "cancelled"
			record.Progress = "cancelled"
			record.Message = "delegation cancelled"
			record.LastUpdated = time.Now()
			return
		}
		if err != nil {
			record.Status = "failed"
			record.Progress = "failed"
			record.Error = err.Error()
			record.Message = "delegation failed"
			record.LastUpdated = time.Now()
			return
		}
		record.Status = "completed"
		record.Progress = "finished"
		record.PartialResult = result
		record.Message = "delegation completed"
		record.LastUpdated = time.Now()
	}()

	return ideworker.DelegationHandle{ID: id, Status: "running", Message: "delegation started"}, nil
}

func (b *continueIDEWorkerBackend) GetTaskStatus(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.delegations == nil {
		return ideworker.DelegationStatus{}, fmt.Errorf("delegation %s not found", delegationID)
	}
	record, ok := b.delegations[delegationID]
	if !ok {
		return ideworker.DelegationStatus{}, fmt.Errorf("delegation %s not found", delegationID)
	}
	return ideworker.DelegationStatus{
		ID:            record.ID,
		Status:        record.Status,
		Progress:      record.Progress,
		PartialResult: record.PartialResult,
		Message:       record.Message,
		Error:         record.Error,
		Cancelled:     record.Cancelled,
	}, nil
}

func (b *continueIDEWorkerBackend) CancelTask(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.delegations == nil {
		return ideworker.DelegationStatus{}, fmt.Errorf("delegation %s not found", delegationID)
	}
	record, ok := b.delegations[delegationID]
	if !ok {
		return ideworker.DelegationStatus{}, fmt.Errorf("delegation %s not found", delegationID)
	}
	record.Cancelled = true
	record.Status = "cancelled"
	record.Progress = "cancelled"
	record.Message = "delegation cancelled"
	record.LastUpdated = time.Now()
	return ideworker.DelegationStatus{
		ID:            record.ID,
		Status:        record.Status,
		Progress:      record.Progress,
		PartialResult: record.PartialResult,
		Message:       record.Message,
		Error:         record.Error,
		Cancelled:     record.Cancelled,
	}, nil
}

func (b *continueIDEWorkerBackend) registerBackgroundSupervisorHook() {
	continueDelegationSupervisorHookOnce.Do(func() {
		sopbackend.RegisterBackgroundServiceHook(func(context.Context) {
			runContinueDelegationSupervisorTick()
		})
	})
}

func registerContinueDelegationBackend(backend *continueIDEWorkerBackend) {
	if backend == nil {
		return
	}
	continueDelegationSupervisorMu.Lock()
	defer continueDelegationSupervisorMu.Unlock()
	continueDelegationBackends[backend] = struct{}{}
}

func runContinueDelegationSupervisorTick() {
	continueDelegationSupervisorMu.Lock()
	backends := make([]*continueIDEWorkerBackend, 0, len(continueDelegationBackends))
	for backend := range continueDelegationBackends {
		backends = append(backends, backend)
	}
	continueDelegationSupervisorMu.Unlock()

	for _, backend := range backends {
		if backend != nil {
			backend.tickDelegationSupervisor()
		}
	}
}

func (b *continueIDEWorkerBackend) tickDelegationSupervisor() {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.delegations == nil {
		return
	}
	for _, record := range b.delegations {
		if record.Cancelled || record.Status == "completed" || record.Status == "failed" || record.Status == "cancelled" {
			continue
		}
		if record.StartedAt.IsZero() {
			record.StartedAt = now
		}
		if record.LastUpdated.IsZero() {
			record.LastUpdated = record.StartedAt
		}
		if record.Timeout <= 0 {
			record.Timeout = b.getTTL()
		}
		if now.Sub(record.StartedAt) >= record.Timeout {
			record.Cancelled = true
			record.Status = "cancelled"
			record.Progress = "timed_out"
			record.Message = fmt.Sprintf("delegation timed out after %s", record.Timeout)
			record.Error = record.Message
			record.LastUpdated = now
			continue
		}
		if now.Sub(record.LastUpdated) >= b.getProgressTTL() {
			record.Cancelled = true
			record.Status = "cancelled"
			record.Progress = "stalled"
			record.Message = fmt.Sprintf("delegation stalled for %s without progress", b.getProgressTTL())
			record.Error = record.Message
			record.LastUpdated = now
			continue
		}
		if now.Sub(record.LastUpdated) >= b.getPollInterval() {
			record.Progress = "polling"
			record.Message = "delegation still running"
			record.LastUpdated = now
		}
	}
}

func (b *continueIDEWorkerBackend) getPollInterval() time.Duration {
	if b.pollInterval > 0 {
		return b.pollInterval
	}
	return defaultDelegationPollInterval
}

func (b *continueIDEWorkerBackend) getTTL() time.Duration {
	if b.timeout > 0 {
		return b.timeout
	}
	return defaultDelegationTTL
}

func (b *continueIDEWorkerBackend) getProgressTTL() time.Duration {
	return defaultDelegationProgressTTL
}

func (b *continueIDEWorkerBackend) executeDelegatedTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	if b.service == nil {
		return "", fmt.Errorf("IDE worker backend unavailable")
	}
	continueReq := continueAgentExecuteRequest{
		RequestID:      newContinueRequestID(),
		TraceID:        strings.TrimSpace(req.TraceID),
		RootRequestID:  strings.TrimSpace(req.RootRequestID),
		SessionID:      strings.TrimSpace(req.SessionID),
		Task:           strings.TrimSpace(req.Task),
		Mode:           strings.TrimSpace(req.Mode),
		Provider:       strings.TrimSpace(req.Provider),
		Model:          strings.TrimSpace(req.Model),
		RequestedPhase: strings.TrimSpace(req.RequestedPhase),
		AvailableTools: append([]string(nil), req.AvailableTools...),
		PhaseTools:     append([]string(nil), req.PhaseTools...),
		AllowedTools:   append([]string(nil), req.AllowedTools...),
		ToolChoice:     strings.TrimSpace(req.ToolChoice),
		Guidance:       convertDelegationGuidance(req.Guidance),
	}
	continueReq.ResultContract = continueExecutionResultContract{
		StreamProgress:       true,
		IncludeModifiedFiles: true,
		IncludeDiffDeltas:    true,
		IncludeMetadata:      true,
		PreferArtifactRefs:   true,
		MaxInlineDiffBytes:   16384,
	}
	continueReq.Context.WorkspaceRoot = strings.TrimSpace(req.WorkspaceRoot)
	continueReq.Context.ActiveFile = strings.TrimSpace(req.ActiveFile)
	continueReq.Context.SelectedText = strings.TrimSpace(req.SelectedText)
	continueReq.Context.OpenFiles = append([]string(nil), req.OpenFiles...)

	sessionKey := continueSessionContextKey(continueReq.SessionID, "", "")
	daemonSessionStore.mu.Lock()
	storedSessionContext, ok := daemonSessionStore.data[sessionKey]
	daemonSessionStore.mu.Unlock()
	if ok {
		continueReq.Auth.AccessToken = strings.TrimSpace(storedSessionContext.AccessToken)
		continueReq.Auth.RefreshToken = strings.TrimSpace(storedSessionContext.RefreshToken)
		continueReq.HQ.BaseURL = strings.TrimSpace(storedSessionContext.HQBaseURL)
		continueReq.HQ.Port = strings.TrimSpace(storedSessionContext.HQPort)
		continueReq.HQ.UserID = strings.TrimSpace(storedSessionContext.HQUserID)
		continueReq.HQ.SessionID = strings.TrimSpace(storedSessionContext.HQSessionID)
	}

	if continueReq.SessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if continueReq.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	registration, ok := b.service.ensureContinueWorkerStore().Get(continueReq.SessionID)
	if !ok {
		return "", fmt.Errorf("no IDE worker is registered for session %s", continueReq.SessionID)
	}
	requiredCapabilities := requiredContinueWorkerCapabilities(continueReq)
	if !continueWorkerSupportsCapabilities(registration, requiredCapabilities) {
		return "", fmt.Errorf("registered IDE worker lacks required capabilities: need %s, have %s", strings.Join(requiredCapabilities, ", "), strings.Join(registration.Capabilities, ", "))
	}

	outcome, err := executeContinueWorkerCallback(ctx, continueReq, registration)
	if err != nil {
		return "", err
	}
	if outcome.StatusCode >= http.StatusBadRequest {
		trimmedBody := strings.TrimSpace(outcome.RawBody)
		if trimmedBody == "" {
			trimmedBody = http.StatusText(outcome.StatusCode)
		}
		return "", fmt.Errorf("IDE worker returned status %d: %s", outcome.StatusCode, trimmedBody)
	}
	if strings.TrimSpace(outcome.RawBody) == "" && outcome.FinalResult == nil {
		return "IDE worker task delegated successfully.", nil
	}
	if outcome.FinalResult != nil {
		if body, ok := marshalContinueExecutionResult(*outcome.FinalResult); ok {
			return body, nil
		}
		return "", fmt.Errorf("marshal delegated execution result")
	}
	if normalized, ok := normalizeContinueWorkerResult([]byte(outcome.RawBody)); ok {
		return normalized, nil
	}
	return outcome.RawBody, nil
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
	outcome, err := readContinueWorkerResponse(resp.Body, nil, true)
	if err != nil {
		return continueWorkerCallbackOutcome{}, fmt.Errorf("read worker response: %w", err)
	}
	outcome.StatusCode = resp.StatusCode
	if outcome.StatusCode == 0 {
		outcome.StatusCode = http.StatusOK
	}
	outcome.ContentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	if outcome.ContentType == "" {
		outcome.ContentType = "application/x-ndjson"
	}
	return outcome, nil
}

func convertDelegationGuidance(src ideworker.DelegationGuidance) continueDelegationGuidance {
	guidance := continueDelegationGuidance{
		TeamPractices:        append([]string(nil), src.TeamPractices...),
		SuccessCriteria:      append([]string(nil), src.SuccessCriteria...),
		Constraints:          append([]string(nil), src.Constraints...),
		ImplementationNotes:  append([]string(nil), src.ImplementationNotes...),
		ExpectedResultFormat: strings.TrimSpace(src.ExpectedResultFormat),
		Metadata:             cloneAnyMap(src.Metadata),
	}
	if len(src.ReferenceContext) > 0 {
		guidance.ReferenceContext = make([]continueDelegationReference, 0, len(src.ReferenceContext))
		for _, ref := range src.ReferenceContext {
			guidance.ReferenceContext = append(guidance.ReferenceContext, continueDelegationReference{
				Kind:     strings.TrimSpace(ref.Kind),
				Source:   strings.TrimSpace(ref.Source),
				Title:    strings.TrimSpace(ref.Title),
				Content:  strings.TrimSpace(ref.Content),
				Metadata: cloneAnyMap(ref.Metadata),
			})
		}
	}
	return guidance
}

func normalizeContinueWorkerResult(body []byte) (string, bool) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", false
	}

	if result, ok := parseContinueExecutionResult([]byte(trimmed)); ok {
		return marshalContinueExecutionResult(result)
	}

	lines := strings.Split(trimmed, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var envelope struct {
			Type   string                  `json:"type"`
			Result continueExecutionResult `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		if envelope.Type == "done" {
			return marshalContinueExecutionResult(envelope.Result)
		}
	}

	return "", false
}

func marshalContinueExecutionResult(result continueExecutionResult) (string, bool) {
	if strings.TrimSpace(result.Status) == "" {
		result.Status = "completed"
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", false
	}
	return string(body), true
}

func parseContinueExecutionResult(body []byte) (continueExecutionResult, bool) {
	var result continueExecutionResult
	if err := json.Unmarshal(body, &result); err != nil {
		return continueExecutionResult{}, false
	}
	if strings.TrimSpace(result.Status) == "" && strings.TrimSpace(result.Summary) == "" && len(result.ModifiedFiles) == 0 && len(result.Artifacts) == 0 && len(result.Metadata) == 0 {
		return continueExecutionResult{}, false
	}
	return result, true
}

func injectIDEWorkerBackend(agentSvc ai.Agent[map[string]any], backend ideworker.Backend) {
	if copilotAgent, ok := agentSvc.(*sharedagent.CopilotAgent); ok {
		copilotAgent.InjectIDEWorkerBackend(backend)
	}
}

func mergeIDEWorkerSessionState(base ideworker.SessionState, overlay ideworker.SessionState) ideworker.SessionState {
	if base.SessionID == "" {
		base.SessionID = strings.TrimSpace(overlay.SessionID)
	}
	if !base.Connected && overlay.Connected {
		base.Connected = true
	}
	if strings.TrimSpace(base.IDE) == "" {
		base.IDE = strings.TrimSpace(overlay.IDE)
	}
	if len(base.WorkerCapabilities) == 0 && len(overlay.WorkerCapabilities) > 0 {
		base.WorkerCapabilities = append([]string(nil), overlay.WorkerCapabilities...)
	}
	if strings.TrimSpace(base.Mode) == "" {
		base.Mode = strings.TrimSpace(overlay.Mode)
	}
	if strings.TrimSpace(base.WorkflowProfile) == "" {
		base.WorkflowProfile = strings.TrimSpace(overlay.WorkflowProfile)
	}
	if strings.TrimSpace(base.RequestedPhase) == "" {
		base.RequestedPhase = strings.TrimSpace(overlay.RequestedPhase)
	}
	if strings.TrimSpace(base.WorkspaceRoot) == "" {
		base.WorkspaceRoot = strings.TrimSpace(overlay.WorkspaceRoot)
	}
	if strings.TrimSpace(base.ActiveFile) == "" {
		base.ActiveFile = strings.TrimSpace(overlay.ActiveFile)
	}
	if strings.TrimSpace(base.SelectedText) == "" {
		base.SelectedText = strings.TrimSpace(overlay.SelectedText)
	}
	if len(base.OpenFiles) == 0 && len(overlay.OpenFiles) > 0 {
		base.OpenFiles = append([]string(nil), overlay.OpenFiles...)
	}
	return base
}
