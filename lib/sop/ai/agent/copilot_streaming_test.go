package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

func TestCopilotAgent_DelegateToReasoningEngineStreamsToolEvents(t *testing.T) {
	a := &CopilotAgent{
		Config: Config{
			Verbose: true,
		},
		registry: NewRegistry(),
		databases: map[string]sop.DatabaseOptions{
			"dev": {},
		},
	}

	a.registry.Register("mock_tool", "run a mock operation", "{}", wrapStringTool(func(ctx context.Context, args map[string]any) (string, error) {
		return `{"result":"success"}`, nil
	}))

	var events []string
	ctx := context.WithValue(context.Background(), ai.CtxKeyEventStreamer, func(eventType string, data any) {
		events = append(events, eventType)
	})
	ctx = context.WithValue(ctx, ai.CtxKeyExecutor, a)
	ctx = context.WithValue(ctx, "session_payload", &ai.SessionPayload{CurrentDB: "dev"})

	generator := &mockToolStreamingGenerator{}
	_, _, _, _, _, err := a.reactLoop(ctx, "show me users", generator, "system prompt")
	if err != nil {
		t.Fatalf("delegateToReasoningEngine failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected streamed reasoning events, got none")
	}
}

type mockToolStreamingGenerator struct {
	calls int
}

func (m *mockToolStreamingGenerator) Name() string { return "mock_tool_streaming" }

func (m *mockToolStreamingGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }

func (m *mockToolStreamingGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func (m *mockToolStreamingGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.calls++
	if m.calls == 1 {
		return ai.GenOutput{ToolCalls: []ai.ToolCall{{
			Name: "mock_tool",
			Args: map[string]any{},
		}}}, nil
	}
	return ai.GenOutput{Text: "Final answer: Operation completed"}, nil
}
