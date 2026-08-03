package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		state.WorkspaceRoot = strings.TrimSpace(snapshot.WorkspaceRoot)
		state.ActiveFile = strings.TrimSpace(snapshot.ActiveFile)
		state.OpenFiles = append([]string(nil), snapshot.OpenFiles...)
	}
	return state, nil
}

func (b continueIDEWorkerBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	if b.service == nil {
		return "", fmt.Errorf("IDE worker backend unavailable")
	}
	continueReq := continueAgentExecuteRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		Task:      strings.TrimSpace(req.Task),
		Mode:      strings.TrimSpace(req.Mode),
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

	body, statusCode, err := executeContinueWorkerCallback(ctx, continueReq, registration)
	if err != nil {
		return "", err
	}
	if statusCode >= http.StatusBadRequest {
		trimmedBody := strings.TrimSpace(string(body))
		if trimmedBody == "" {
			trimmedBody = http.StatusText(statusCode)
		}
		return "", fmt.Errorf("IDE worker returned status %d: %s", statusCode, trimmedBody)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "IDE worker task delegated successfully.", nil
	}
	return string(body), nil
}

func executeContinueWorkerCallback(ctx context.Context, req continueAgentExecuteRequest, registration continueWorkerRegistration) ([]byte, int, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal worker request: %w", err)
	}
	callbackReq, err := http.NewRequestWithContext(ctx, http.MethodPost, registration.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build worker callback request: %w", err)
	}
	callbackReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(callbackReq)
	if err != nil {
		return nil, 0, fmt.Errorf("call IDE worker: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read worker response: %w", err)
	}
	if resp.StatusCode == 0 {
		resp.StatusCode = http.StatusOK
	}
	return responseBody, resp.StatusCode, nil
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
