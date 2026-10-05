package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

func TestClineCommandQuotesWorkspaceAndTask(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "work space")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	command, err := clineCommand(ideworker.TaskRequest{
		WorkspaceRoot: workspace,
		Provider:      "openai",
		Model:         "gpt-5",
		Task:          "edit John's file",
	})
	if err != nil {
		t.Fatalf("clineCommand failed: %v", err)
	}
	if !strings.Contains(command, "cline --json --cwd '") || !strings.Contains(command, "-P 'openai' -m 'gpt-5' 'edit John'\\''s file'") {
		t.Fatalf("unexpected Cline command: %q", command)
	}
}

func TestParseClineRunResult(t *testing.T) {
	result, err := parseClineRunResult(strings.Join([]string{
		`{"type":"agent_event","event":{"type":"iteration_start","iteration":1}}`,
		`{"type":"run_result","finishReason":"completed","iterations":2,"text":"Updated the file","usage":{"inputTokens":10}}`,
	}, "\n"))
	if err != nil {
		t.Fatalf("parseClineRunResult failed: %v", err)
	}
	if result.Status != "completed" || result.Summary != "Updated the file" {
		t.Fatalf("unexpected parsed result: %#v", result)
	}
	if result.Metadata["iterations"] != 2 {
		t.Fatalf("expected iteration metadata, got %#v", result.Metadata)
	}
}

func TestParseClineRunResultRejectsFailure(t *testing.T) {
	_, err := parseClineRunResult(`{"type":"run_result","finishReason":"error","text":"Unauthorized"}`)
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("expected terminal Cline error, got %v", err)
	}
}

func TestClineSDKExecutionDoesNotReturnShellCommand(t *testing.T) {
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	workspace := t.TempDir()
	_, err := clineExecutionCommand(ideworker.TaskRequest{
		RequestID:     "req-1",
		SessionID:     "session-1",
		WorkspaceRoot: workspace,
		Provider:      "openai",
		Model:         "gpt-5",
		Task:          "run the failing tests",
		Guidance: ideworker.DelegationGuidance{
			SuccessCriteria: []string{"tests pass"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "persistent SDK host") {
		t.Fatalf("expected SDK execution to bypass shell commands, got %v", err)
	}
}

func TestParseClineSDKResult(t *testing.T) {
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	result, err := parseClineResult(strings.Join([]string{
		`{"type":"started","requestId":"req-1"}`,
		`{"type":"result","requestId":"req-1","result":{"status":"completed","text":"Tests pass","metadata":{"iterations":2,"task_usage":{"inputTokens":12,"outputTokens":4,"totalCost":0.003},"aggregate_session_usage":{"inputTokens":100,"outputTokens":40,"totalCost":0.03}}}}`,
	}, "\n"))
	if err != nil {
		t.Fatalf("parseClineResult failed: %v", err)
	}
	if result.Status != "completed" || result.Summary != "Tests pass" {
		t.Fatalf("unexpected SDK result: %#v", result)
	}
	if result.Metadata["task_usage"] == nil || result.Metadata["aggregate_session_usage"] == nil {
		t.Fatalf("expected Cline usage and cost metadata to survive parsing, got %#v", result.Metadata)
	}
}

func TestCollectClineWorkspaceChanges(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "app.py"), []byte("return 1\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "Cline Test"},
		{"config", "user.email", "cline-test@example.invalid"},
		{"add", "app.py"},
		{"commit", "-m", "initial"},
	} {
		command := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, output)
		}
	}
	before := snapshotClineWorkspace(context.Background(), workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.py"), []byte("return 2\n"), 0o644); err != nil {
		t.Fatalf("update fixture: %v", err)
	}
	after := snapshotClineWorkspace(context.Background(), workspace)
	changes := collectClineWorkspaceChanges(before, after)
	if len(changes) != 1 || changes[0].FilePath != "app.py" || changes[0].CRUDOperation != "update" {
		t.Fatalf("unexpected workspace changes: %#v", changes)
	}
	if !strings.Contains(changes[0].DiffDelta, "-return 1") || !strings.Contains(changes[0].DiffDelta, "+return 2") {
		t.Fatalf("expected unified diff, got %q", changes[0].DiffDelta)
	}
}

func TestCollectClineWorkspaceChangesOutsideGit(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "updated.txt"), []byte("old value\n"), 0o644); err != nil {
		t.Fatalf("write original fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "deleted.txt"), []byte("remove me\n"), 0o644); err != nil {
		t.Fatalf("write delete fixture: %v", err)
	}
	before := snapshotClineWorkspace(context.Background(), workspace)
	if before == nil || before.Source != "filesystem" || !before.Complete {
		t.Fatalf("expected complete filesystem snapshot, got %#v", before)
	}
	if err := os.WriteFile(filepath.Join(workspace, "updated.txt"), []byte("new value\n"), 0o644); err != nil {
		t.Fatalf("update fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "created.txt"), []byte("new file\n"), 0o644); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := os.Remove(filepath.Join(workspace, "deleted.txt")); err != nil {
		t.Fatalf("delete fixture: %v", err)
	}
	after := snapshotClineWorkspace(context.Background(), workspace)
	changes := collectClineWorkspaceChanges(before, after)
	operations := make(map[string]string, len(changes))
	for _, change := range changes {
		operations[change.FilePath] = change.CRUDOperation
	}
	if len(operations) != 3 || operations["updated.txt"] != "update" || operations["created.txt"] != "create" || operations["deleted.txt"] != "delete" {
		t.Fatalf("unexpected non-Git workspace changes: %#v", changes)
	}
	if !strings.Contains(changes[0].DiffDelta, "---") {
		t.Fatalf("expected unified diff from filesystem snapshots, got %#v", changes)
	}
}

func TestClineFilesystemSnapshotMarksLimitsIncomplete(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "oversized.txt"), make([]byte, maxClineSnapshotFileBytes+1), 0o644); err != nil {
		t.Fatalf("write oversized fixture: %v", err)
	}
	snapshot := snapshotClineFilesystem(context.Background(), workspace)
	if snapshot.Complete || !snapshot.Skipped["oversized.txt"] {
		t.Fatalf("expected oversized file to be skipped and snapshot marked incomplete: %#v", snapshot)
	}
}

func TestClineFilesystemSnapshotStopsAtEntryLimit(t *testing.T) {
	workspace := t.TempDir()
	for index := 0; index <= maxClineSnapshotFiles; index++ {
		name := filepath.Join(workspace, fmt.Sprintf("file-%04d.txt", index))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("write fixture %d: %v", index, err)
		}
	}
	snapshot := snapshotClineFilesystem(context.Background(), workspace)
	if snapshot.Complete || !snapshot.Truncated || len(snapshot.Files) != maxClineSnapshotFiles {
		t.Fatalf("expected a bounded, truncated snapshot, got files=%d snapshot=%#v", len(snapshot.Files), snapshot)
	}
	if changes := collectClineWorkspaceChanges(snapshot, snapshot); len(changes) != 0 {
		t.Fatalf("must not infer changes from truncated snapshots: %#v", changes)
	}
}

func TestClineRuntimeAvailableUsesSDKHostByDefault(t *testing.T) {
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	t.Setenv("SOP_CLINE_SDK_HOST_PATH", filepath.Join(t.TempDir(), "host.js"))
	backend := &clineWorkerBackend{}
	if clineRuntimeAvailable() || backend.BackendAvailable() {
		t.Fatal("expected unavailable SDK host before the host file exists")
	}
	workerCapability := ideworker.NewCapability()
	workerCapability.SetBackend(backend)
	if !workerCapability.HasBackend() || !workerCapability.OwnsCodingWorkflow() {
		t.Fatal("selected Cline must retain coding ownership when its runtime is misconfigured")
	}
	if _, err := backend.ExecuteTask(context.Background(), ideworker.TaskRequest{}); err == nil || !strings.Contains(err.Error(), "Cline backend is selected but unavailable") {
		t.Fatalf("expected actionable unavailable-backend error, got %v", err)
	}
}

func TestDelegationStatusIncludesStructuredClineActivity(t *testing.T) {
	backend := &clineWorkerBackend{delegationTracker: delegationTracker{delegations: map[string]*delegationRecord{
		"task-1": {ID: "task-1", Status: "running", Phase: "execution", ActivityKind: "validation", ActivityLabel: "Running validation...", LastUpdated: time.Unix(10, 0)},
	}}}
	status, err := backend.GetTaskStatus(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("GetTaskStatus failed: %v", err)
	}
	if status.Phase != "execution" || status.ActivityKind != "validation" || status.ActivityLabel != "Running validation..." || status.UpdatedAt.IsZero() {
		t.Fatalf("structured activity missing: %#v", status)
	}
}

func TestClineWorkerBackendStreamsProgressAndCompletion(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	t.Setenv("SOP_CLINE_SDK_HOST_PATH", hostPath)
	backend := &clineWorkerBackend{sdkHosts: manager}
	type capturedEvent struct {
		eventType string
		payload   map[string]any
	}
	events := make(chan capturedEvent, 8)
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventPayload, ok := payload.(map[string]any); ok {
			events <- capturedEvent{eventType: eventType, payload: eventPayload}
		}
	})

	handle, err := backend.StartTask(ctx, ideworker.TaskRequest{
		RequestID:     "req-stream-progress",
		SessionID:     "session-stream-progress",
		WorkstreamID:  "workstream-stream-progress",
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
		Task:          "stream this task",
	})
	if err != nil {
		t.Fatalf("StartTask failed: %v", err)
	}
	if handle.ID != "req-stream-progress" {
		t.Fatalf("unexpected delegation handle: %#v", handle)
	}

	sawLiveProgress := false
	for {
		select {
		case event := <-events:
			if event.eventType != "progress" || event.payload["source"] != "ide_worker" || event.payload["backend"] != "cline" || event.payload["delegation_id"] != handle.ID {
				t.Fatalf("unexpected streamed delegation event: %#v", event)
			}
			if event.payload["status"] == "running" && event.payload["message"] == "working on stream this task" {
				sawLiveProgress = true
			}
			if event.payload["status"] == "completed" {
				if !sawLiveProgress {
					t.Fatal("completion was streamed without a live progress event")
				}
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for streamed Cline progress and completion")
		}
	}
}

func TestClineExecuteTaskStreamsProgressAndReturnsFinalResult(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	t.Setenv("SOP_CLINE_SDK_HOST_PATH", hostPath)
	backend := &clineWorkerBackend{sdkHosts: manager}
	events := make(chan map[string]any, 8)
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventType == "progress" {
			if event, ok := payload.(map[string]any); ok {
				events <- event
			}
		}
	})

	result, err := backend.ExecuteTask(ctx, ideworker.TaskRequest{
		RequestID:     "req-sync-cline",
		SessionID:     "session-sync-cline",
		WorkstreamID:  "workstream-sync-cline",
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
		Task:          "synchronous coding task",
	})
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}
	if !strings.Contains(result, `"summary":"turn-1"`) {
		t.Fatalf("expected final result from Cline, got %s", result)
	}
	sawRunning, sawCompleted := false, false
	for range 3 {
		select {
		case event := <-events:
			if event["backend"] != "cline" || event["delegation_id"] != "req-sync-cline" {
				t.Fatalf("unexpected Cline event: %#v", event)
			}
			switch event["status"] {
			case "running":
				sawRunning = true
			case "completed":
				sawCompleted = true
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for Cline progress lifecycle events")
		}
	}
	if !sawRunning || !sawCompleted {
		t.Fatalf("expected running and completed progress events, got running=%t completed=%t", sawRunning, sawCompleted)
	}
}

func TestClineExecuteTaskReportsStructuredFailure(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	t.Setenv("SOP_CLINE_SDK_HOST_PATH", hostPath)
	backend := &clineWorkerBackend{sdkHosts: manager}
	events := make(chan map[string]any, 8)
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventType == "progress" {
			if event, ok := payload.(map[string]any); ok {
				events <- event
			}
		}
	})

	result, err := backend.ExecuteTask(ctx, ideworker.TaskRequest{
		RequestID:     "req-failed-result",
		SessionID:     "session-failed-result",
		WorkstreamID:  "workstream-failed-result",
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
		Task:          "fail-result",
	})
	if err != nil {
		t.Fatalf("structured task failure should be returned to Ask as a result: %v", err)
	}
	if !strings.Contains(result, `"status":"failed"`) || !strings.Contains(result, "validation failed") {
		t.Fatalf("expected failed structured result, got %s", result)
	}
	sawFailed := false
	for range 3 {
		select {
		case event := <-events:
			if event["status"] == "completed" {
				t.Fatalf("failed result must not emit completed progress: %#v", event)
			}
			if event["status"] == "failed" {
				sawFailed = true
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for failed-result lifecycle events")
		}
	}
	if !sawFailed {
		t.Fatal("expected a failed terminal progress event")
	}
}

func TestClineStartTaskMarksStructuredFailureFailed(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	t.Setenv("SOP_CLINE_SDK_HOST_PATH", hostPath)
	backend := &clineWorkerBackend{sdkHosts: manager}
	handle, err := backend.StartTask(context.Background(), ideworker.TaskRequest{
		RequestID:     "req-async-failed-result",
		SessionID:     "session-async-failed-result",
		WorkstreamID:  "workstream-async-failed-result",
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
		Task:          "fail-result",
	})
	if err != nil {
		t.Fatalf("StartTask failed: %v", err)
	}
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := backend.GetTaskStatus(context.Background(), handle.ID)
		if err != nil {
			t.Fatalf("GetTaskStatus failed: %v", err)
		}
		if status.Status == "failed" {
			if status.PartialResult == "" || !strings.Contains(status.Error, "validation failed") {
				t.Fatalf("failed task must preserve structured result and failure reason: %#v", status)
			}
			return
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("task did not reach failed terminal status: %#v", status)
		}
	}
}

func TestClineWorkerBackendStreamsCancellation(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	t.Setenv("SOP_CLINE_SDK_HOST_PATH", hostPath)
	backend := &clineWorkerBackend{sdkHosts: manager}
	type capturedEvent struct {
		eventType string
		payload   map[string]any
	}
	events := make(chan capturedEvent, 8)
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventPayload, ok := payload.(map[string]any); ok {
			events <- capturedEvent{eventType: eventType, payload: eventPayload}
		}
	})

	handle, err := backend.StartTask(ctx, ideworker.TaskRequest{
		RequestID:     "req-stream-cancel",
		SessionID:     "session-stream-cancel",
		WorkstreamID:  "workstream-stream-cancel",
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
		Task:          "wait-cancel",
	})
	if err != nil {
		t.Fatalf("StartTask failed: %v", err)
	}
	for {
		select {
		case event := <-events:
			if event.payload["message"] == "waiting" {
				status, err := backend.CancelTask(context.Background(), handle.ID)
				if err != nil {
					t.Fatalf("CancelTask failed: %v", err)
				}
				if status.Status != "cancelled" {
					t.Fatalf("expected cancelled status, got %#v", status)
				}
				select {
				case cancelled := <-events:
					if cancelled.eventType != "progress" || cancelled.payload["status"] != "cancelled" || cancelled.payload["delegation_id"] != handle.ID {
						t.Fatalf("unexpected cancellation event: %#v", cancelled)
					}
					return
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for streamed cancellation event")
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for Cline progress before cancellation")
		}
	}
}

func TestClineStartTaskOutlivesRequestContextUntilExplicitCancel(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	t.Setenv("SOP_CLINE_SDK_HOST_PATH", hostPath)
	backend := &clineWorkerBackend{sdkHosts: manager}
	type capturedEvent struct {
		eventType string
		payload   map[string]any
	}
	events := make(chan capturedEvent, 8)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	requestCtx = context.WithValue(requestCtx, ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventPayload, ok := payload.(map[string]any); ok {
			events <- capturedEvent{eventType: eventType, payload: eventPayload}
		}
	})

	handle, err := backend.StartTask(requestCtx, ideworker.TaskRequest{
		RequestID:     "req-http-async",
		SessionID:     "session-http-async",
		WorkstreamID:  "workstream-http-async",
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
		Task:          "wait-cancel",
	})
	if err != nil {
		t.Fatalf("StartTask failed: %v", err)
	}
	started := false
	progressDeadline := time.After(5 * time.Second)
	for !started {
		select {
		case event := <-events:
			started = event.payload["message"] == "waiting"
		case <-progressDeadline:
			t.Fatal("fake host did not start the asynchronous task")
		}
	}
	cancelRequest()

	deadline := time.After(250 * time.Millisecond)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		manager.mu.Lock()
		active := manager.requests[handle.ID] != nil
		manager.mu.Unlock()
		if !active {
			t.Fatal("Cline task stopped when its HTTP request context ended")
		}
		select {
		case <-ticker.C:
		case <-deadline:
			goto stillRunning
		}
	}

stillRunning:
	status, err := backend.CancelTask(context.Background(), handle.ID)
	if err != nil {
		t.Fatalf("explicit cancel failed: %v", err)
	}
	if status.Status != "cancelled" || !status.Cancelled {
		t.Fatalf("expected explicit cancellation status, got %#v", status)
	}
}
