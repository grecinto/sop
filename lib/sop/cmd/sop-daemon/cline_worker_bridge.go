package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

const clineExecutableName = "cline"

const maxClineInlineDiffBytes = 32 * 1024

const maxClineSnapshotFiles = 2000

const maxClineSnapshotListingBytes = 2 * 1024 * 1024

const maxClineSnapshotFileBytes = 1024 * 1024

const maxClineSnapshotTotalBytes = 16 * 1024 * 1024

const clineAsyncTaskTimeout = 12 * time.Hour

type clineWorkerBackend struct {
	shellSessions *ptyShellSessionManager
	sdkHosts      *clineSDKHostManager
	taskContexts  *clineTaskContextStore
	delegationTracker
	sessions       map[string]string
	eventStreamers map[string]func(ideworker.DelegationStatus)
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

type clineWorkspaceFile struct {
	Hash    string
	Content string
}

type clineWorkspaceSnapshot struct {
	Files     map[string]clineWorkspaceFile
	Skipped   map[string]bool
	Complete  bool
	Truncated bool
	Source    string
}

type clineSnapshotListing struct {
	bytes.Buffer
	limit int
}

func (b *clineSnapshotListing) Write(contents []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return 0, fmt.Errorf("workspace file listing exceeds limit")
	}
	if len(contents) > remaining {
		written, _ := b.Buffer.Write(contents[:remaining])
		return written, fmt.Errorf("workspace file listing exceeds limit")
	}
	return b.Buffer.Write(contents)
}

func newClineIDEWorkerBackend(shellSessions *ptyShellSessionManager) ideworker.Backend {
	if shellSessions == nil {
		shellSessions = newPTYShellSessionManager()
	}
	return &clineWorkerBackend{shellSessions: shellSessions, sdkHosts: newClineSDKHostManager()}
}

func (b *clineWorkerBackend) Close() error {
	if b == nil || b.sdkHosts == nil {
		return nil
	}
	return b.sdkHosts.Close()
}

func (b *clineWorkerBackend) BackendAvailable() bool {
	return clineRuntimeAvailable()
}

func (b *clineWorkerBackend) OwnsCodingWorkflow() bool {
	return true
}

func (b *clineWorkerBackend) CodingCapabilityName() string {
	return "Cline Coding"
}

func (b *clineWorkerBackend) InspectSession(ctx context.Context, sessionID string) (ideworker.SessionState, error) {
	_ = ctx
	state := ideworker.SessionState{SessionID: strings.TrimSpace(sessionID), IDE: "cline"}
	if clineRuntimeAvailable() {
		state.Connected = true
		state.WorkerCapabilities = append([]string(nil), ideworker.StandardCapabilities...)
	}
	return state, nil
}

func clineRuntimeAvailable() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SOP_CLINE_EXECUTION_MODE")), "cli") {
		_, err := exec.LookPath(clineExecutableName)
		return err == nil
	}
	hostPath := resolveClineSDKHostPath(strings.TrimSpace(os.Getenv("SOP_CLINE_SDK_HOST_PATH")))
	if strings.TrimSpace(hostPath) == "" || hostPath == "tools/cline-sdk-host/dist/host.js" {
		return false
	}
	if _, err := os.Stat(hostPath); err != nil {
		return false
	}
	_, err := exec.LookPath("node")
	return err == nil
}

func (b *clineWorkerBackend) ExecuteTask(ctx context.Context, req ideworker.TaskRequest) (string, error) {
	if !b.BackendAvailable() {
		return "", clineBackendUnavailableError()
	}
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = newContinueRequestID()
	}
	sendEvent, _ := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any))
	emit := func(status, progress, message string) {
		if sendEvent == nil {
			return
		}
		sendEvent("progress", map[string]any{
			"source":         "ide_worker",
			"backend":        "cline",
			"event":          "delegation_status",
			"delegation_id":  req.RequestID,
			"status":         status,
			"progress":       progress,
			"message":        message,
			"phase":          "execution",
			"activity_kind":  clineActivityKind(message),
			"activity_label": clineActivityLabel(message),
			"updated_at":     time.Now(),
		})
	}
	emit("running", "submitted", "Cline coding task started")
	result, err := b.executeTask(ctx, req, func(message string) {
		if message = strings.TrimSpace(message); message != "" {
			emit("running", "running", message)
		}
	})
	if err != nil {
		emit("failed", "failed", "Cline coding task failed")
		return "", err
	}
	if failure := clineExecutionResultFailure(result); failure != "" {
		emit("failed", "failed", failure)
		return result, nil
	}
	emit("completed", "finished", "Cline coding task completed")
	return result, nil
}

func clineExecutionResultFailure(raw string) string {
	result, ok := ideworker.ParseExecutionResult(raw)
	if !ok {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "", "completed", "complete", "success", "succeeded":
		return ""
	default:
		message := strings.TrimSpace(result.Summary)
		if message == "" {
			message = "Cline returned task status " + strings.TrimSpace(result.Status)
		}
		return message
	}
}

func (b *clineWorkerBackend) executeTask(ctx context.Context, req ideworker.TaskRequest, progress func(string)) (output string, retErr error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if b.taskContexts != nil {
		b.taskContexts.PutFromRequest(req, "in_progress", req.Task)
		defer func() {
			status, message := "completed", strings.TrimSpace(output)
			if retErr != nil {
				status, message = "failed", retErr.Error()
			} else if failure := clineExecutionResultFailure(output); failure != "" {
				status, message = "failed", failure
			}
			b.taskContexts.PutFromRequest(req, status, message)
		}()
	}
	workstreamID := strings.TrimSpace(req.WorkstreamID)
	if workstreamID == "" {
		workstreamID = strings.TrimSpace(req.SessionID)
	}
	if workstreamID == "" {
		workstreamID = strings.TrimSpace(req.RequestID)
	}
	sessionLock := b.sessionLock(workstreamID)
	sessionLock.Lock()
	defer sessionLock.Unlock()
	if abs, err := filepath.Abs(strings.TrimSpace(req.WorkspaceRoot)); err == nil && abs != "" {
		req.WorkspaceRoot = abs
	}
	before := snapshotClineWorkspace(ctx, req.WorkspaceRoot)
	var result ideworker.ExecutionResult
	var err error
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SOP_CLINE_EXECUTION_MODE")), "cli") {
		command, err := clineCommand(req)
		if err != nil {
			return "", err
		}
		if _, err := b.shellSessions.EnsureSession(workstreamID, strings.TrimSpace(req.WorkspaceRoot)); err != nil {
			return "", fmt.Errorf("start Cline CLI shell session: %w", err)
		}
		output, stillRunning, err := b.shellSessions.RunWithContext(ctx, workstreamID, command, 12*time.Hour, func(chunk string) {
			if progress != nil {
				progress(strings.TrimSpace(chunk))
			}
		})
		if err != nil {
			return "", fmt.Errorf("run Cline CLI task: %w", err)
		}
		if stillRunning {
			return "", fmt.Errorf("Cline CLI task did not finish before the daemon timeout")
		}
		result, err = parseClineRunResult(output)
	} else {
		hostPath := strings.TrimSpace(os.Getenv("SOP_CLINE_SDK_HOST_PATH"))
		if hostPath == "" {
			hostPath = "tools/cline-sdk-host/dist/host.js"
		}
		hostPath = resolveClineSDKHostPath(hostPath)
		output, runErr := b.sdkHosts.Run(ctx, hostPath, req, progress)
		if runErr != nil {
			return "", fmt.Errorf("run Cline SDK task: %w", runErr)
		}
		result, err = parseClineSDKResult(output)
	}
	if err != nil {
		return "", err
	}
	after := snapshotClineWorkspace(ctx, req.WorkspaceRoot)
	result.ModifiedFiles = collectClineWorkspaceChanges(before, after)
	if result.Metadata == nil {
		result.Metadata = make(map[string]any)
	}
	result.Metadata["workspace_snapshot"] = map[string]any{
		"before_complete": before != nil && before.Complete,
		"after_complete":  after != nil && after.Complete,
		"source":          snapshotSource(before, after),
	}
	if len(result.ModifiedFiles) > 0 {
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
	if !b.BackendAvailable() {
		return ideworker.DelegationHandle{}, clineBackendUnavailableError()
	}
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = newContinueRequestID()
	}
	id := req.RequestID
	now := time.Now()
	record := &delegationRecord{ID: id, Status: "running", Progress: "submitted", Message: "Cline task started", Phase: "intake", ActivityKind: "started", ActivityLabel: "Cline SDK started", StartedAt: now, LastUpdated: now}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "cline-default"
	}
	var emitStatus func(ideworker.DelegationStatus)
	if sendEvent, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && sendEvent != nil {
		emitStatus = func(status ideworker.DelegationStatus) {
			if ctx.Err() != nil {
				return
			}
			sendEvent("progress", clineDelegationProgressPayload(status))
		}
	}
	b.mu.Lock()
	if b.delegations == nil {
		b.delegations = make(map[string]*delegationRecord)
	}
	if b.sessions == nil {
		b.sessions = make(map[string]string)
	}
	if emitStatus != nil {
		if b.eventStreamers == nil {
			b.eventStreamers = make(map[string]func(ideworker.DelegationStatus))
		}
		b.eventStreamers[id] = emitStatus
	}
	b.delegations[id] = record
	b.sessions[id] = sessionID
	initialStatus := clineDelegationStatus(record)
	b.mu.Unlock()
	if emitStatus != nil {
		emitStatus(initialStatus)
	}
	workerCtx, cancelWorker := context.WithTimeout(context.WithoutCancel(ctx), clineAsyncTaskTimeout)
	go func() {
		defer cancelWorker()
		result, err := b.executeTask(workerCtx, req, func(message string) {
			if message == "" {
				return
			}
			if len(message) > 512 {
				message = message[len(message)-512:]
			}
			b.mu.Lock()
			record.Progress = "running"
			record.Message = message
			record.Phase = "execution"
			record.ActivityKind = clineActivityKind(message)
			record.ActivityLabel = clineActivityLabel(message)
			record.LastUpdated = time.Now()
			status := clineDelegationStatus(record)
			b.mu.Unlock()
			if emitStatus != nil {
				emitStatus(status)
			}
		})
		b.mu.Lock()
		if record.Cancelled {
			delete(b.eventStreamers, id)
			b.mu.Unlock()
			return
		}
		record.LastUpdated = time.Now()
		resultFailure := clineExecutionResultFailure(result)
		if err != nil || resultFailure != "" {
			record.Status = "failed"
			record.Progress = "failed"
			record.ActivityKind = "failed"
			record.ActivityLabel = "Cline task failed"
			if err != nil {
				record.Error = err.Error()
			} else {
				record.Error = resultFailure
				record.PartialResult = result
			}
			record.Message = "Cline task failed"
		} else {
			record.Status = "completed"
			record.Progress = "finished"
			record.Phase = "delivery"
			record.ActivityKind = "completed"
			record.ActivityLabel = "Cline task completed"
			record.PartialResult = result
			record.Message = "Cline task completed"
		}
		status := clineDelegationStatus(record)
		delete(b.eventStreamers, id)
		b.mu.Unlock()
		if emitStatus != nil {
			emitStatus(status)
		}
	}()
	return ideworker.DelegationHandle{ID: id, Status: "running", Message: "Cline task started"}, nil
}

func clineBackendUnavailableError() error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SOP_CLINE_EXECUTION_MODE")), "cli") {
		return fmt.Errorf("Cline backend is selected but unavailable: install the Cline CLI and ensure it is on PATH")
	}
	return fmt.Errorf("Cline backend is selected but unavailable: build/package tools/cline-sdk-host and provide Node.js 22 or newer")
}

func clineDelegationStatus(record *delegationRecord) ideworker.DelegationStatus {
	return ideworker.DelegationStatus{
		ID:            record.ID,
		Status:        record.Status,
		Progress:      record.Progress,
		Message:       record.Message,
		Error:         record.Error,
		Cancelled:     record.Cancelled,
		Phase:         record.Phase,
		ActivityKind:  record.ActivityKind,
		ActivityLabel: record.ActivityLabel,
		UpdatedAt:     record.LastUpdated,
	}
}

func clineDelegationProgressPayload(status ideworker.DelegationStatus) map[string]any {
	return map[string]any{
		"source":         "ide_worker",
		"backend":        "cline",
		"event":          "delegation_status",
		"delegation_id":  status.ID,
		"status":         status.Status,
		"progress":       status.Progress,
		"message":        status.Message,
		"error":          status.Error,
		"cancelled":      status.Cancelled,
		"phase":          status.Phase,
		"activity_kind":  status.ActivityKind,
		"activity_label": status.ActivityLabel,
		"updated_at":     status.UpdatedAt,
	}
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
	emitStatus := b.eventStreamers[delegationID]
	if !ok {
		b.mu.Unlock()
		return ideworker.DelegationStatus{}, fmt.Errorf("delegation %s not found", delegationID)
	}
	if record.Status != "running" {
		status := clineDelegationStatus(record)
		b.mu.Unlock()
		return status, nil
	}
	record.Cancelled = true
	record.Status = "cancelled"
	record.Progress = "cancelled"
	record.Message = "Cline task cancelled"
	record.ActivityKind = "cancelled"
	record.ActivityLabel = "Cline task cancelled"
	record.LastUpdated = time.Now()
	status := clineDelegationStatus(record)
	delete(b.eventStreamers, delegationID)
	b.mu.Unlock()
	if emitStatus != nil {
		emitStatus(status)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SOP_CLINE_EXECUTION_MODE")), "cli") {
		if sessionID != "" {
			_, _, _ = b.shellSessions.Run(sessionID, "stop", 2*time.Second)
		}
	} else if b.sdkHosts != nil {
		if err := b.sdkHosts.Cancel(delegationID); err != nil {
			log.Printf("cancel Cline SDK request %s: %v", delegationID, err)
		}
	}
	return status, nil
}

func clineActivityKind(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "test"), strings.Contains(lower, "validate"):
		return "validation"
	case strings.Contains(lower, "read"), strings.Contains(lower, "grep"), strings.Contains(lower, "search"), strings.Contains(lower, "inspect"):
		return "investigating"
	case strings.Contains(lower, "edit"), strings.Contains(lower, "write"), strings.Contains(lower, "patch"), strings.Contains(lower, "file"):
		return "tool"
	case strings.Contains(lower, "checkpoint"):
		return "checkpoint"
	case strings.Contains(lower, "plan"), strings.Contains(lower, "reason"):
		return "planning"
	default:
		return "tool"
	}
}

func clineActivityLabel(message string) string {
	switch clineActivityKind(message) {
	case "validation":
		return "Running validation..."
	case "investigating":
		return "Investigating workspace..."
	case "tool":
		return "Updating workspace..."
	case "checkpoint":
		return "Checkpoint created"
	case "planning":
		return "Planning task..."
	default:
		return "Working in workspace..."
	}
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
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("SOP_CLINE_EXECUTION_MODE")), "cli") {
		return "", fmt.Errorf("Cline SDK tasks run through the persistent SDK host, not a shell command")
	}
	return clineCommand(req)
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

func snapshotClineWorkspace(ctx context.Context, workspaceRoot string) *clineWorkspaceSnapshot {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil
	}
	listing := &clineSnapshotListing{limit: maxClineSnapshotListingBytes}
	command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z")
	command.Stdout = listing
	err := command.Run()
	if err != nil {
		return snapshotClineFilesystem(ctx, root)
	}
	files := strings.Split(strings.TrimSuffix(listing.String(), "\x00"), "\x00")
	snapshot := &clineWorkspaceSnapshot{Files: make(map[string]clineWorkspaceFile), Skipped: make(map[string]bool), Complete: true, Source: "git"}
	var totalBytes int64
	entriesSeen := 0
	for _, relativePath := range files {
		if ctx.Err() != nil {
			snapshot.Complete = false
			break
		}
		relativePath = strings.TrimSpace(relativePath)
		if relativePath == "" {
			continue
		}
		if entriesSeen >= maxClineSnapshotFiles {
			snapshot.Complete = false
			snapshot.Truncated = true
			break
		}
		entriesSeen++
		contents, err := readClineSnapshotFile(root, relativePath)
		if err != nil {
			if !os.IsNotExist(err) {
				snapshot.Skipped[relativePath] = true
				snapshot.Complete = false
			}
			continue
		}
		if len(contents) > maxClineSnapshotFileBytes || totalBytes+int64(len(contents)) > maxClineSnapshotTotalBytes {
			snapshot.Complete = false
			snapshot.Skipped[relativePath] = true
			continue
		}
		var hash [32]byte
		hash = sha256.Sum256(contents)
		snapshot.Files[relativePath] = clineWorkspaceFile{Hash: fmt.Sprintf("%x", hash), Content: string(contents)}
		totalBytes += int64(len(contents))
	}
	return snapshot
}

func snapshotClineFilesystem(ctx context.Context, root string) *clineWorkspaceSnapshot {
	snapshot := &clineWorkspaceSnapshot{Files: make(map[string]clineWorkspaceFile), Skipped: make(map[string]bool), Complete: true, Source: "filesystem"}
	var totalBytes int64
	entriesSeen := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			snapshot.Complete = false
			return err
		}
		if walkErr != nil {
			snapshot.Complete = false
			return nil
		}
		if path == root {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			snapshot.Complete = false
			return nil
		}
		if entry.IsDir() {
			if clineSnapshotSkippedDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		relativePath = filepath.ToSlash(relativePath)
		if entriesSeen >= maxClineSnapshotFiles {
			snapshot.Complete = false
			snapshot.Truncated = true
			return filepath.SkipAll
		}
		entriesSeen++
		contents, err := readClineSnapshotFile(root, relativePath)
		if err != nil {
			snapshot.Skipped[relativePath] = true
			snapshot.Complete = false
			return nil
		}
		if len(contents) > maxClineSnapshotFileBytes || totalBytes+int64(len(contents)) > maxClineSnapshotTotalBytes {
			snapshot.Complete = false
			snapshot.Skipped[relativePath] = true
			return nil
		}
		hash := sha256.Sum256(contents)
		snapshot.Files[relativePath] = clineWorkspaceFile{Hash: fmt.Sprintf("%x", hash), Content: string(contents)}
		totalBytes += int64(len(contents))
		return nil
	})
	if err != nil {
		snapshot.Complete = false
	}
	return snapshot
}

func clineSnapshotSkippedDirectory(name string) bool {
	switch name {
	case ".git", ".sop", "node_modules", "vendor", "dist", "build", "target", ".next", ".cache", ".turbo", ".tox", "coverage", "__pycache__", ".venv", "venv", "Pods":
		return true
	default:
		return false
	}
}

func readClineSnapshotFile(root, relativePath string) ([]byte, error) {
	clean := filepath.Clean(relativePath)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("workspace path escapes root")
	}
	path := filepath.Join(root, clean)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxClineSnapshotFileBytes {
		return nil, fmt.Errorf("unsupported or oversized workspace file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("workspace file changed while snapshotting")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxClineSnapshotFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxClineSnapshotFileBytes {
		return nil, fmt.Errorf("oversized workspace file")
	}
	return contents, nil
}

func collectClineWorkspaceChanges(before, after *clineWorkspaceSnapshot) []ideworker.ModifiedFile {
	if before == nil || after == nil || before.Truncated || after.Truncated {
		return nil
	}
	paths := make(map[string]bool, len(before.Files)+len(after.Files))
	for path := range before.Files {
		paths[path] = true
	}
	for path := range after.Files {
		paths[path] = true
	}
	orderedPaths := make([]string, 0, len(paths))
	for path := range paths {
		if before.Skipped[path] || after.Skipped[path] {
			continue
		}
		beforeFile, existedBefore := before.Files[path]
		afterFile, existsAfter := after.Files[path]
		if existedBefore != existsAfter || beforeFile.Hash != afterFile.Hash {
			orderedPaths = append(orderedPaths, path)
		}
	}
	sort.Strings(orderedPaths)
	modified := make([]ideworker.ModifiedFile, 0, len(orderedPaths))
	for _, path := range orderedPaths {
		operation := "update"
		if _, existedBefore := before.Files[path]; !existedBefore {
			operation = "create"
		} else if _, existsAfter := after.Files[path]; !existsAfter {
			operation = "delete"
		}
		beforeFile, existedBefore := before.Files[path]
		afterFile, existsAfter := after.Files[path]
		diffDelta := clineWorkspaceDiff(path, beforeFile.Content, existedBefore, afterFile.Content, existsAfter)
		metadata := map[string]any{"source": "daemon_workspace_diff", "snapshot_complete": before.Complete && after.Complete}
		if strings.ContainsRune(diffDelta, '\x00') {
			metadata["diff_omitted"] = "binary file"
			diffDelta = ""
		}
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

func clineWorkspaceDiff(path, before string, existedBefore bool, after string, existsAfter bool) string {
	if strings.ContainsRune(before, '\x00') || strings.ContainsRune(after, '\x00') {
		return ""
	}
	oldName, newName := "/dev/null", "/dev/null"
	if existedBefore {
		oldName = "a/" + path
	}
	if existsAfter {
		newName = "b/" + path
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(before), B: difflib.SplitLines(after),
		FromFile: oldName, ToFile: newName, Context: 3,
	})
	if err != nil {
		return ""
	}
	return diff
}

func snapshotSource(before, after *clineWorkspaceSnapshot) string {
	if before == nil || after == nil {
		return "unavailable"
	}
	if before.Source == after.Source {
		return before.Source
	}
	return before.Source + "+" + after.Source
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
