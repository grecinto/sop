package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

const clineExecutableName = "cline"

const maxClineInlineDiffBytes = 32 * 1024

type clineWorkerBackend struct {
	shellSessions *ptyShellSessionManager
	delegationTracker
	sessions       map[string]string
	sessionLocksMu sync.Mutex
	sessionLocks   map[string]*sync.Mutex
}

type clineRunResult struct {
	FinishReason string         `json:"finishReason"`
	Text         string         `json:"text"`
	Iterations   int            `json:"iterations"`
	Usage        map[string]any `json:"usage"`
	Model        map[string]any `json:"model"`
}

func newClineIDEWorkerBackend(shellSessions *ptyShellSessionManager) ideworker.Backend {
	if shellSessions == nil {
		shellSessions = newPTYShellSessionManager()
	}
	return &clineWorkerBackend{shellSessions: shellSessions}
}

func (b *clineWorkerBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	_ = ctx
	state := ideworker.SessionState{SessionID: strings.TrimSpace(sessionID), IDE: "cline"}
	if _, _, err := b.shellSessions.Run(state.SessionID, "command -v cline", 2*time.Second); err == nil {
		state.Connected = true
		state.WorkerCapabilities = append([]string(nil), ideworker.StandardCapabilities...)
	}
	return state, nil
}

func (b *clineWorkerBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	return b.executeTask(ctx, req, nil)
}

func (b *clineWorkerBackend) executeTask(ctx context.Context, req ideworker.TaskRequest, progress func(string)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	command, cleanup, err := clineExecutionSpec(req)
	if err != nil {
		return "", err
	}
	defer cleanup()
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "cline-default"
	}
	sessionLock := b.sessionLock(sessionID)
	sessionLock.Lock()
	defer sessionLock.Unlock()
	if _, err := b.shellSessions.EnsureSession(sessionID, strings.TrimSpace(req.WorkspaceRoot)); err != nil {
		return "", fmt.Errorf("start Cline shell session: %w", err)
	}
	before := snapshotClineWorkspace(ctx, req.WorkspaceRoot)
	output, stillRunning, err := b.shellSessions.RunWithContext(ctx, sessionID, command, 12*time.Hour, func(chunk string) {
		if progress != nil {
			progress(strings.TrimSpace(chunk))
		}
	})
	if err != nil {
		return "", fmt.Errorf("run Cline task: %w", err)
	}
	if stillRunning {
		return "", fmt.Errorf("Cline task did not finish before the daemon timeout")
	}
	result, err := parseClineResult(output)
	if err != nil {
		return "", err
	}
	result.ModifiedFiles = collectClineWorkspaceChanges(ctx, req.WorkspaceRoot, before)
	if len(result.ModifiedFiles) > 0 {
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["modified_files_source"] = "daemon_workspace_diff"
	}
	if err := recordClineExecutionHistory(ctx, req, &result); err != nil {
		log.Printf("cline execution history: %v", err)
	}
	return marshalClineExecutionResult(result), nil
}

func (b *clineWorkerBackend) sessionLock(sessionID string) *sync.Mutex {
	b.sessionLocksMu.Lock()
	defer b.sessionLocksMu.Unlock()
	if b.sessionLocks == nil {
		b.sessionLocks = make(map[string]*sync.Mutex)
	}
	if lock := b.sessionLocks[sessionID]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	b.sessionLocks[sessionID] = lock
	return lock
}

func (b *clineWorkerBackend) StartTask(ctx context.Context, req ideworker.TaskRequest) (ideworker.DelegationHandle, error) {
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = newContinueRequestID()
	}
	id := req.RequestID
	now := time.Now()
	record := &delegationRecord{ID: id, Status: "running", Progress: "submitted", Message: "Cline task started", StartedAt: now, LastUpdated: now}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "cline-default"
	}
	b.mu.Lock()
	if b.delegations == nil {
		b.delegations = make(map[string]*delegationRecord)
	}
	if b.sessions == nil {
		b.sessions = make(map[string]string)
	}
	b.delegations[id] = record
	b.sessions[id] = sessionID
	b.mu.Unlock()
	go func() {
		result, err := b.executeTask(ctx, req, func(message string) {
			if message == "" {
				return
			}
			if len(message) > 512 {
				message = message[len(message)-512:]
			}
			b.mu.Lock()
			record.Progress = "running"
			record.Message = message
			record.LastUpdated = time.Now()
			b.mu.Unlock()
		})
		b.mu.Lock()
		defer b.mu.Unlock()
		if record.Cancelled {
			return
		}
		record.LastUpdated = time.Now()
		if err != nil {
			record.Status = "failed"
			record.Progress = "failed"
			record.Error = err.Error()
			record.Message = "Cline task failed"
			return
		}
		record.Status = "completed"
		record.Progress = "finished"
		record.PartialResult = result
		record.Message = "Cline task completed"
	}()
	return ideworker.DelegationHandle{ID: id, Status: "running", Message: "Cline task started"}, nil
}

func (b *clineWorkerBackend) GetTaskStatus(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	_ = ctx
	return b.delegationTracker.status(delegationID)
}

func (b *clineWorkerBackend) CancelTask(ctx context.Context, delegationID string) (ideworker.DelegationStatus, error) {
	_ = ctx
	b.mu.Lock()
	record, ok := b.delegations[delegationID]
	sessionID := b.sessions[delegationID]
	b.mu.Unlock()
	if !ok {
		return ideworker.DelegationStatus{}, fmt.Errorf("delegation %s not found", delegationID)
	}
	if sessionID != "" {
		_, _, _ = b.shellSessions.Run(sessionID, "stop", 2*time.Second)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record.Cancelled = true
	record.Status = "cancelled"
	record.Progress = "cancelled"
	record.Message = "Cline task cancelled"
	record.LastUpdated = time.Now()
	return ideworker.DelegationStatus{ID: record.ID, Status: record.Status, Progress: record.Progress, PartialResult: record.PartialResult, Message: record.Message, Error: record.Error, Cancelled: record.Cancelled}, nil
}

func clineCommand(req ideworker.TaskRequest) (string, error) {
	if err := validateClineWorkspaceRoot(req.WorkspaceRoot); err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Task) == "" {
		return "", fmt.Errorf("Cline task requires a task")
	}
	parts := []string{clineExecutableName, "--json", "--cwd", shellQuote(req.WorkspaceRoot)}
	if provider := strings.TrimSpace(req.Provider); provider != "" {
		parts = append(parts, "-P", shellQuote(provider))
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		parts = append(parts, "-m", shellQuote(model))
	}
	parts = append(parts, shellQuote(req.Task))
	return strings.Join(parts, " "), nil
}

func clineExecutionCommand(req ideworker.TaskRequest) (string, error) {
	command, cleanup, err := clineExecutionSpec(req)
	if cleanup != nil {
		defer cleanup()
	}
	return command, err
}

func clineExecutionSpec(req ideworker.TaskRequest) (string, func(), error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SOP_CLINE_EXECUTION_MODE")), "cli") {
		command, err := clineCommand(req)
		return command, func() {}, err
	}
	if err := validateClineWorkspaceRoot(req.WorkspaceRoot); err != nil {
		return "", func() {}, err
	}
	if strings.TrimSpace(req.Task) == "" {
		return "", func() {}, fmt.Errorf("Cline task requires a task")
	}
	hostPath := strings.TrimSpace(os.Getenv("SOP_CLINE_SDK_HOST_PATH"))
	if hostPath == "" {
		hostPath = "tools/cline-sdk-host/dist/host.js"
	}
	hostPath = resolveClineSDKHostPath(hostPath)
	if info, err := os.Stat(hostPath); err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("path is a directory")
		}
		return "", func() {}, fmt.Errorf("Cline SDK host is unavailable at %s: %w", hostPath, err)
	}
	payload := map[string]any{
		"type":              "run",
		"requestId":         req.RequestID,
		"sessionId":         req.SessionID,
		"task":              req.Task,
		"workspaceRoot":     req.WorkspaceRoot,
		"providerId":        req.Provider,
		"modelId":           req.Model,
		"apiKey":            req.APIKey,
		"baseUrl":           req.BaseURL,
		"mcpUrl":            req.MCPURL,
		"mcpBearerToken":    req.MCPBearerToken,
		"mode":              clineSDKMode(req.Mode),
		"guidance":          req.Guidance,
		"enableCheckpoints": true,
	}
	if apiKey := strings.TrimSpace(os.Getenv("SOP_CLINE_API_KEY")); apiKey != "" {
		payload["apiKey"] = apiKey
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", func() {}, fmt.Errorf("marshal Cline SDK request: %w", err)
	}
	requestFile, err := os.CreateTemp("", "sop-cline-sdk-request-*.json")
	if err != nil {
		return "", func() {}, fmt.Errorf("create Cline SDK request file: %w", err)
	}
	requestPath := requestFile.Name()
	if err := requestFile.Chmod(0o600); err != nil {
		requestFile.Close()
		_ = os.Remove(requestPath)
		return "", func() {}, fmt.Errorf("protect Cline SDK request file: %w", err)
	}
	if _, err := requestFile.Write(body); err != nil {
		requestFile.Close()
		_ = os.Remove(requestPath)
		return "", func() {}, fmt.Errorf("write Cline SDK request file: %w", err)
	}
	if err := requestFile.Close(); err != nil {
		_ = os.Remove(requestPath)
		return "", func() {}, fmt.Errorf("close Cline SDK request file: %w", err)
	}
	cleanup := func() { _ = os.Remove(requestPath) }
	return "node " + shellQuote(hostPath) + " < " + shellQuote(requestPath), cleanup, nil
}

func resolveClineSDKHostPath(configured string) string {
	if filepath.IsAbs(configured) {
		return configured
	}
	candidates := make([]string, 0, 8)
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, configured))
		for parent := cwd; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			candidates = append(candidates, filepath.Join(filepath.Dir(parent), configured))
		}
	}
	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(executableDir, configured))
		for parent := executableDir; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			candidates = append(candidates, filepath.Join(filepath.Dir(parent), configured))
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if resolved, err := filepath.Abs(candidate); err == nil {
				return resolved
			}
		}
	}
	return configured
}

func validateClineWorkspaceRoot(workspaceRoot string) error {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return fmt.Errorf("Cline task requires a workspace root")
	}
	info, err := os.Stat(workspaceRoot)
	if err != nil {
		return fmt.Errorf("Cline workspace root is unavailable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("Cline workspace root is not a directory")
	}
	return nil
}

func clineSDKMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "plan":
		return "plan"
	case "yolo":
		return "yolo"
	default:
		return "act"
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func parseClineRunResult(output string) (ideworker.ExecutionResult, error) {
	scanner := bufio.NewScanner(bytes.NewReader([]byte(output)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var terminal clineRunResult
	found := false
	for scanner.Scan() {
		var envelope struct {
			Type         string         `json:"type"`
			FinishReason string         `json:"finishReason"`
			Text         string         `json:"text"`
			Iterations   int            `json:"iterations"`
			Usage        map[string]any `json:"usage"`
			Model        map[string]any `json:"model"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			continue
		}
		if envelope.Type == "run_result" {
			terminal = clineRunResult{
				FinishReason: envelope.FinishReason,
				Text:         envelope.Text,
				Iterations:   envelope.Iterations,
				Usage:        envelope.Usage,
				Model:        envelope.Model,
			}
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return ideworker.ExecutionResult{}, fmt.Errorf("read Cline output: %w", err)
	}
	if !found {
		return ideworker.ExecutionResult{}, fmt.Errorf("Cline output did not contain a run_result event")
	}
	status := strings.ToLower(strings.TrimSpace(terminal.FinishReason))
	if status == "" {
		status = "completed"
	}
	if status != "completed" && status != "success" && status != "complete" {
		return ideworker.ExecutionResult{}, fmt.Errorf("Cline task finished with status %s: %s", status, strings.TrimSpace(terminal.Text))
	}
	metadata := map[string]any{"finish_reason": terminal.FinishReason, "iterations": terminal.Iterations}
	if len(terminal.Usage) > 0 {
		metadata["usage"] = terminal.Usage
	}
	if len(terminal.Model) > 0 {
		metadata["model"] = terminal.Model
	}
	return ideworker.ExecutionResult{Status: status, Summary: strings.TrimSpace(terminal.Text), Metadata: metadata}, nil
}

func parseClineSDKResult(output string) (ideworker.ExecutionResult, error) {
	var result struct {
		Status   string         `json:"status"`
		Text     string         `json:"text"`
		Summary  string         `json:"summary"`
		Metadata map[string]any `json:"metadata"`
	}
	for _, line := range strings.Split(output, "\n") {
		var envelope struct {
			Type    string          `json:"type"`
			Result  json.RawMessage `json:"result"`
			Message string          `json:"message"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &envelope); err != nil {
			continue
		}
		if envelope.Type == "error" {
			return ideworker.ExecutionResult{}, fmt.Errorf("Cline SDK task failed: %s", envelope.Message)
		}
		if envelope.Type != "result" {
			continue
		}
		if err := json.Unmarshal(envelope.Result, &result); err != nil {
			return ideworker.ExecutionResult{}, fmt.Errorf("decode Cline SDK result: %w", err)
		}
		status := strings.ToLower(strings.TrimSpace(result.Status))
		if status == "" {
			status = "completed"
		}
		if result.Summary == "" {
			result.Summary = result.Text
		}
		return ideworker.ExecutionResult{Status: status, Summary: strings.TrimSpace(result.Summary), Metadata: result.Metadata}, nil
	}
	return ideworker.ExecutionResult{}, fmt.Errorf("Cline SDK output did not contain a result event")
}

func snapshotClineWorkspace(ctx context.Context, workspaceRoot string) map[string]string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil
	}
	command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	files := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	snapshot := make(map[string]string, len(files))
	for _, relativePath := range files {
		relativePath = strings.TrimSpace(relativePath)
		if relativePath == "" {
			continue
		}
		snapshot[relativePath] = hashClineWorkspaceFile(filepath.Join(root, relativePath))
	}
	return snapshot
}

func hashClineWorkspaceFile(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	hash := sha256.Sum256(contents)
	return fmt.Sprintf("%x", hash)
}

func collectClineWorkspaceChanges(ctx context.Context, workspaceRoot string, before map[string]string) []ideworker.ModifiedFile {
	after := snapshotClineWorkspace(ctx, workspaceRoot)
	if before == nil || after == nil {
		return nil
	}
	paths := make(map[string]bool, len(before)+len(after))
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	orderedPaths := make([]string, 0, len(paths))
	for path := range paths {
		if before[path] != after[path] {
			orderedPaths = append(orderedPaths, path)
		}
	}
	sort.Strings(orderedPaths)
	modified := make([]ideworker.ModifiedFile, 0, len(orderedPaths))
	for _, path := range orderedPaths {
		operation := "update"
		if _, existedBefore := before[path]; !existedBefore {
			operation = "create"
		} else if _, existsAfter := after[path]; !existsAfter {
			operation = "delete"
		}
		diffDelta := clineWorkspaceDiff(ctx, workspaceRoot, path)
		metadata := map[string]any{"source": "daemon_workspace_diff"}
		if len(diffDelta) > maxClineInlineDiffBytes {
			metadata["diff_truncated"] = true
			metadata["diff_bytes"] = len(diffDelta)
			diffDelta = diffDelta[:maxClineInlineDiffBytes]
		}
		modified = append(modified, ideworker.ModifiedFile{
			FilePath:      path,
			CRUDOperation: operation,
			DiffDelta:     diffDelta,
			DiffFormat:    "unified",
			Metadata:      metadata,
		})
	}
	return modified
}

func clineWorkspaceDiff(ctx context.Context, workspaceRoot, relativePath string) string {
	command := exec.CommandContext(ctx, "git", "-C", workspaceRoot, "diff", "--no-ext-diff", "--unified=3", "--", relativePath)
	if output, err := command.Output(); err == nil && len(output) > 0 {
		return string(output)
	}
	command = exec.CommandContext(ctx, "git", "-C", workspaceRoot, "diff", "--no-index", "--unified=3", "/dev/null", filepath.Join(workspaceRoot, relativePath))
	output, _ := command.CombinedOutput()
	return string(output)
}

func recordClineExecutionHistory(ctx context.Context, req ideworker.TaskRequest, result *ideworker.ExecutionResult) error {
	if result == nil || len(result.ModifiedFiles) == 0 {
		return nil
	}
	bridgeRequest := continueAgentExecuteRequest{
		RequestID:      strings.TrimSpace(req.RequestID),
		TraceID:        strings.TrimSpace(req.TraceID),
		RootRequestID:  strings.TrimSpace(req.RootRequestID),
		SessionID:      strings.TrimSpace(req.SessionID),
		Task:           strings.TrimSpace(req.Task),
		Provider:       strings.TrimSpace(req.Provider),
		Model:          strings.TrimSpace(req.Model),
		RequestedPhase: strings.TrimSpace(req.RequestedPhase),
		Mode:           strings.TrimSpace(req.Mode),
	}
	bridgeRequest.Context.WorkspaceRoot = strings.TrimSpace(req.WorkspaceRoot)
	bridgeRequest.Context.ActiveFile = strings.TrimSpace(req.ActiveFile)
	bridgeRequest.Context.SelectedText = strings.TrimSpace(req.SelectedText)
	bridgeRequest.Context.OpenFiles = append([]string(nil), req.OpenFiles...)
	if err := recordContinueDelegatedUndoTransaction(ctx, bridgeRequest, result); err != nil {
		return err
	}
	return recordContinueExecutionMemoryBrief(ctx, bridgeRequest, result)
}

func parseClineResult(output string) (ideworker.ExecutionResult, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SOP_CLINE_EXECUTION_MODE")), "cli") {
		return parseClineRunResult(output)
	}
	return parseClineSDKResult(output)
}

func marshalClineExecutionResult(result ideworker.ExecutionResult) string {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("Cline task completed: %s", result.Summary)
	}
	return string(body)
}
