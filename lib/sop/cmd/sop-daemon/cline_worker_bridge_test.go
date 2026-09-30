package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

func TestClineSDKCommandIncludesTaskAndGuidance(t *testing.T) {
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	workspace := t.TempDir()
	command, err := clineExecutionCommand(ideworker.TaskRequest{
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
	if err != nil {
		t.Fatalf("clineExecutionCommand failed: %v", err)
	}
	if !strings.Contains(command, "node '") || !strings.Contains(command, "tools/cline-sdk-host/dist/host.js'") || !strings.Contains(command, " < '") {
		t.Fatalf("expected SDK host command to use a request file: %q", command)
	}
	if strings.Contains(command, "run the failing tests") || strings.Contains(command, "tests pass") {
		t.Fatalf("expected task and guidance to stay out of the shell command: %q", command)
	}
}

func TestParseClineSDKResult(t *testing.T) {
	t.Setenv("SOP_CLINE_EXECUTION_MODE", "sdk")
	result, err := parseClineResult(strings.Join([]string{
		`{"type":"started","requestId":"req-1"}`,
		`{"type":"result","requestId":"req-1","result":{"status":"completed","text":"Tests pass","metadata":{"iterations":2}}}`,
	}, "\n"))
	if err != nil {
		t.Fatalf("parseClineResult failed: %v", err)
	}
	if result.Status != "completed" || result.Summary != "Tests pass" {
		t.Fatalf("unexpected SDK result: %#v", result)
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
	changes := collectClineWorkspaceChanges(context.Background(), workspace, before)
	if len(changes) != 1 || changes[0].FilePath != "app.py" || changes[0].CRUDOperation != "update" {
		t.Fatalf("unexpected workspace changes: %#v", changes)
	}
	if !strings.Contains(changes[0].DiffDelta, "-return 1") || !strings.Contains(changes[0].DiffDelta, "+return 2") {
		t.Fatalf("expected unified diff, got %q", changes[0].DiffDelta)
	}
}
