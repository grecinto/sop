package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestParseAndEmitTestStatusIncludesFailureContext(t *testing.T) {
	output := `=== RUN   TestSomething
--- FAIL: TestSomething (0.00s)
panic: expected value
FAIL
`

	summary := parseAndEmitTestStatus(context.Background(), "go test ./...", output)
	if !strings.Contains(summary, "TEST STATUS: FAIL") {
		t.Fatalf("expected failure status, got %q", summary)
	}
	if !strings.Contains(summary, "--- FAIL: TestSomething") {
		t.Fatalf("expected failure context in summary, got %q", summary)
	}
	if !strings.Contains(summary, "panic: expected value") {
		t.Fatalf("expected panic detail in summary, got %q", summary)
	}
}

func TestParseAndEmitTestStatusIncludesPassContext(t *testing.T) {
	output := `ok   github.com/sharedcode/sop/agent  0.123s
`

	summary := parseAndEmitTestStatus(context.Background(), "go test ./...", output)
	if !strings.Contains(summary, "TEST STATUS: PASS") {
		t.Fatalf("expected pass status, got %q", summary)
	}
	if !strings.Contains(summary, "ok   github.com/sharedcode/sop/agent") {
		t.Fatalf("expected package result in summary, got %q", summary)
	}
}

func TestParseAndEmitTestStatusIncludesStopAndReportInstruction(t *testing.T) {
	output := `=== RUN   TestSomething
--- FAIL: TestSomething (0.00s)
panic: expected value
FAIL
`

	summary := parseAndEmitTestStatus(context.Background(), "go test ./...", output)
	if !strings.Contains(summary, "Stop and report the outcome") {
		t.Fatalf("expected stop-and-report guidance in summary, got %q", summary)
	}
}

func TestRunLocalAutomationCommandUsesStreamedOutputWhenExecutorPayloadIsEmpty(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		// Streaming callbacks are intentionally nil - no streaming should occur
		// Return the actual output in the JSON payload instead
		return `{"stdout":"--- FAIL: TestExample\npanic: boom\n","stderr":"","exit_code":1}`, nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	out, err := ag.runLocalAutomationCommand(context.Background(), map[string]any{"command": "go test ./..."}, "go test ./...", "", "", 60)
	if err != nil {
		t.Fatalf("runLocalAutomationCommand() error = %v", err)
	}
	if !strings.Contains(out, "--- FAIL: TestExample") {
		t.Fatalf("expected failure output from payload, got %q", out)
	}
	if !strings.Contains(out, "panic: boom") {
		t.Fatalf("expected stderr from payload, got %q", out)
	}
}

func TestRunLocalAutomationCommandWrapsKnownCommandsForTextAnalysis(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	var seenCommand string
	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		seenCommand = req.Command
		return `{"stdout":"--- FAIL: TestExample\npanic: boom\n","stderr":"","exit_code":1}`, nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	_, err := ag.runLocalAutomationCommand(context.Background(), map[string]any{"command": "go test ."}, "go test .", "", "", 60)
	if err != nil {
		t.Fatalf("runLocalAutomationCommand() error = %v", err)
	}
	if !strings.Contains(seenCommand, "go test . 2>&1 | grep -E") {
		t.Fatalf("expected go test command to be wrapped for text analysis, got %q", seenCommand)
	}
	if !strings.Contains(seenCommand, "--- FAIL") {
		t.Fatalf("expected wrapped command to preserve the original command, got %q", seenCommand)
	}
}

func TestWrapKnownCommandForTextAnalysisDoesNotDoubleWrap(t *testing.T) {
	wrapped, shouldWrap := wrapKnownCommandForTextAnalysis("go test . 2>&1 | grep -E 'FAIL|panic' || true")
	if shouldWrap {
		t.Fatalf("expected already-wrapped command to be left unchanged, got %q", wrapped)
	}
	if wrapped != "go test . 2>&1 | grep -E 'FAIL|panic' || true" {
		t.Fatalf("expected command to remain unchanged, got %q", wrapped)
	}
}

func TestRunLocalAutomationCommandEmitsProgressCommentaryForGoTests(t *testing.T) {
	SetAutomationPolicy(AutomationPolicy{Enabled: true, AllowShellCommands: true, AllowEmail: false, RequireApproval: false, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 120})
	defer SetAutomationPolicy(DefaultAutomationPolicy())

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var commentary []string
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, payload any) {
		if eventType != ai.ReasoningEventAssistantMessage {
			return
		}
		if msg, ok := payload.(map[string]any); ok {
			if text, ok := msg["text"].(string); ok {
				commentary = append(commentary, text)
			}
		}
	})

	SetLocalCommandExecutor(func(ctx context.Context, req LocalCommandRequest) (string, error) {
		return `{"stdout":"ok","stderr":"","exit_code":0}`, nil
	})
	defer SetLocalCommandExecutor(nil)

	ag := &CopilotAgent{}
	_, err := ag.runLocalAutomationCommand(ctx, map[string]any{"command": "go test ./..."}, "go test ./...", "", tmpDir, 60)
	if err != nil {
		t.Fatalf("runLocalAutomationCommand() error = %v", err)
	}
	if len(commentary) == 0 {
		t.Fatalf("expected progress commentary to be emitted, got none")
	}
	if !strings.Contains(strings.Join(commentary, "\n"), "predominantly Go-based") {
		t.Fatalf("expected Go-based progress commentary, got %q", strings.Join(commentary, "\n"))
	}
}
