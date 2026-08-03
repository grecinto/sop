package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
	sharedagent "github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

type continueIDEWorkerBackend struct {
	service *localRAGService
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
	return continueIDEWorkerBackend{service: service}
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

func (b continueIDEWorkerBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
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

func (b continueIDEWorkerBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	if b.service == nil {
		return "", fmt.Errorf("IDE worker backend unavailable")
	}
	continueReq := continueAgentExecuteRequest{
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
