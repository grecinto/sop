package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

func TestClineSDKSessionScopeSeparatesWorkstreamsAndConfig(t *testing.T) {
	workspace := t.TempDir()
	base := ideworker.TaskRequest{
		WorkstreamID:  "workstream-a",
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
	}
	baseScope := clineSDKSessionScope(base)

	otherWorkstream := base
	otherWorkstream.WorkstreamID = "workstream-b"
	if clineSDKSessionScope(otherWorkstream) == baseScope {
		t.Fatal("different workstreams must not share a Cline host")
	}
	otherWorkspace := base
	otherWorkspace.WorkspaceRoot = t.TempDir()
	if clineSDKSessionScope(otherWorkspace) == baseScope {
		t.Fatal("different workspaces must not share a Cline host")
	}
	otherProvider := base
	otherProvider.Provider = "anthropic"
	if clineSDKSessionScope(otherProvider) == baseScope {
		t.Fatal("different providers must not share a Cline host")
	}
}

func TestClineSDKHostManagerReusesWorkstreamSession(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	first := runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-a", "req-1", "first")
	second := runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-a", "req-2", "second")
	otherStream := runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-b", "req-3", "third")

	firstSession := eventSessionID(t, first)
	if secondSession := eventSessionID(t, second); secondSession != firstSession {
		t.Fatalf("same workstream should retain its Cline session: %q != %q", secondSession, firstSession)
	}
	if otherSession := eventSessionID(t, otherStream); otherSession == firstSession {
		t.Fatal("separate workstreams must receive different Cline sessions")
	}
	if !strings.Contains(first, `"summary":"turn-1"`) || !strings.Contains(second, `"summary":"turn-2"`) || !strings.Contains(otherStream, `"summary":"turn-1"`) {
		t.Fatalf("host did not preserve only the same-workstream turn count:\nfirst: %s\nsecond: %s\nother: %s", first, second, otherStream)
	}
}

func TestClineSDKHostManagerCancelsOnlyActiveRequest(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	progress := make(chan struct{}, 1)
	result := make(chan error, 1)
	go func() {
		_, err := manager.Run(context.Background(), hostPath, ideworker.TaskRequest{
			RequestID:     "req-cancel",
			WorkstreamID:  "stream-cancel",
			WorkspaceRoot: workspace,
			Provider:      "openai-native",
			Model:         "model-a",
			Task:          "wait-cancel",
		}, func(string) {
			select {
			case progress <- struct{}{}:
			default:
			}
		})
		result <- err
	}()

	select {
	case <-progress:
	case <-time.After(5 * time.Second):
		t.Fatal("fake host did not start the cancellable request")
	}
	unrelated := runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-unrelated", "req-unrelated", "unrelated")
	if !strings.Contains(unrelated, `"summary":"turn-1"`) {
		t.Fatalf("unrelated workstream should complete independently: %s", unrelated)
	}
	if err := manager.Cancel("req-cancel"); err != nil {
		t.Fatalf("cancel active request: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("cancelled request returned an error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request did not finish")
	}

	followUp := runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-cancel", "req-after-cancel", "after cancel")
	if !strings.Contains(followUp, `"summary":"turn-2"`) {
		t.Fatalf("cancel should leave the workstream session reusable: %s", followUp)
	}
}

func TestClineSDKHostManagerStartsFreshSessionAfterRestore(t *testing.T) {
	manager, hostPath, workspace := newFakeClineSDKHostManager(t)
	beforeRestore := runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-restore", "req-before-restore", "before")
	_ = runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-restore", "req-restore", "restore")
	afterRestore := runFakeClineSDKTask(t, manager, hostPath, workspace, "stream-restore", "req-after-restore", "after")
	if eventSessionID(t, beforeRestore) == eventSessionID(t, afterRestore) {
		t.Fatal("a restored workstream must not reuse stale Cline session context")
	}
	if !strings.Contains(afterRestore, `"summary":"turn-1"`) {
		t.Fatalf("expected a fresh Cline host after restore: %s", afterRestore)
	}
}

func newFakeClineSDKHostManager(t *testing.T) (*clineSDKHostManager, string, string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for the fake host integration test")
	}
	workspace := t.TempDir()
	hostPath := filepath.Join(t.TempDir(), "fake-host.js")
	script := `const readline = require("node:readline");
const sessions = new Map();
const active = new Map();
const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
function emit(event) { process.stdout.write(JSON.stringify(event) + "\n"); }
input.on("line", line => {
  const request = JSON.parse(line);
  if (request.type === "run") {
    const turn = (sessions.get(request.clineSessionId) || 0) + 1;
    sessions.set(request.clineSessionId, turn);
    emit({ type: "started", requestId: request.requestId, sessionId: request.clineSessionId });
	emit({ type: "progress", requestId: request.requestId, sessionId: request.clineSessionId, message: "working on " + request.task });
    if (request.task === "wait-cancel") {
      active.set(request.requestId, request.clineSessionId);
      emit({ type: "progress", requestId: request.requestId, sessionId: request.clineSessionId, message: "waiting" });
		} else if (request.task === "restore") {
			emit({ type: "session_invalidated", requestId: request.requestId, sessionId: request.clineSessionId, message: "workspace restored" });
			emit({ type: "result", requestId: request.requestId, sessionId: request.clineSessionId, result: { status: "completed", summary: "restored" } });
		} else if (request.task === "fail-result") {
			emit({ type: "result", requestId: request.requestId, sessionId: request.clineSessionId, result: { status: "failed", summary: "validation failed" } });
    } else {
      emit({ type: "result", requestId: request.requestId, sessionId: request.clineSessionId, result: { status: "completed", summary: "turn-" + turn } });
    }
  } else if (request.type === "cancel") {
    const sessionId = active.get(request.requestId);
    if (sessionId) {
      active.delete(request.requestId);
      emit({ type: "cancelled", requestId: request.requestId, sessionId });
    }
  } else if (request.type === "shutdown") {
    emit({ type: "stopped" });
    input.close();
  }
});
`
	if err := os.WriteFile(hostPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write fake host: %v", err)
	}
	manager := newClineSDKHostManager()
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("close fake Cline host manager: %v", err)
		}
	})
	return manager, hostPath, workspace
}

func runFakeClineSDKTask(t *testing.T, manager *clineSDKHostManager, hostPath, workspace, workstreamID, requestID, task string) string {
	t.Helper()
	output, err := manager.Run(context.Background(), hostPath, ideworker.TaskRequest{
		RequestID:     requestID,
		WorkstreamID:  workstreamID,
		WorkspaceRoot: workspace,
		Provider:      "openai-native",
		Model:         "model-a",
		Task:          task,
	}, nil)
	if err != nil {
		t.Fatalf("run fake Cline task %s: %v", requestID, err)
	}
	return output
}

func eventSessionID(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		var event struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "started" {
			return event.SessionID
		}
	}
	t.Fatalf("started event missing from host output: %s", output)
	return ""
}
