package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type sharedModeOwnedLoopMockGenerator struct {
	calls int
}

func (m *sharedModeOwnedLoopMockGenerator) Name() string { return "shared_mode_owned_loop_mock" }

func (m *sharedModeOwnedLoopMockGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }

func (m *sharedModeOwnedLoopMockGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func (m *sharedModeOwnedLoopMockGenerator) ReActLoopMode() ai.ReActLoopMode {
	return ai.ReActLoopModeShared
}

func (m *sharedModeOwnedLoopMockGenerator) ReActLoop() ai.ReActLoop {
	return ownedReActLoopMock{}
}

func (m *sharedModeOwnedLoopMockGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	_ = ctx
	_ = prompt
	_ = opts
	m.calls++
	if m.calls == 1 {
		return ai.GenOutput{ToolCalls: []ai.ToolCall{{
			Name: "execute_script",
			Args: map[string]any{"script": []any{map[string]any{"op": "scan"}}},
		}}}, nil
	}
	return ai.GenOutput{Text: "Final answer: shared loop used"}, nil
}

func TestCompatibleReActEngine_SharedModeOverridesOwnedLoopProvider(t *testing.T) {
	engine := &CompatibleReActEngine{}
	gen := &sharedModeOwnedLoopMockGenerator{}

	resp, err := engine.Run(context.Background(), ai.ReasoningRequest{
		SystemPrompt: "You are a test assistant.",
		UserQuery:    "Find users",
		Executor:     &loopMockExecutor{},
		Generator:    gen,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if resp.FinalText != "Final answer: shared loop used" {
		t.Fatalf("expected shared loop response, got %q", resp.FinalText)
	}
	if gen.calls != 2 {
		t.Fatalf("expected shared loop generator to be called twice, got %d", gen.calls)
	}
}

type explicitOwnedLoopMockGenerator struct{}

func (m *explicitOwnedLoopMockGenerator) Name() string { return "explicit_owned_loop_mock" }

func (m *explicitOwnedLoopMockGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0 }

func (m *explicitOwnedLoopMockGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func (m *explicitOwnedLoopMockGenerator) ReActLoopMode() ai.ReActLoopMode {
	return ai.ReActLoopModeOwned
}

func (m *explicitOwnedLoopMockGenerator) ReActLoop() ai.ReActLoop {
	return ownedReActLoopMock{}
}

func (m *explicitOwnedLoopMockGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	_ = ctx
	_ = prompt
	_ = opts
	return ai.GenOutput{Text: "unexpected shared loop"}, nil
}

func TestCompatibleReActEngine_ExplicitOwnedModeUsesProviderLoop(t *testing.T) {
	engine := &CompatibleReActEngine{}
	gen := &explicitOwnedLoopMockGenerator{}

	resp, err := engine.Run(context.Background(), ai.ReasoningRequest{
		SystemPrompt: "You are a test assistant.",
		UserQuery:    "Find users",
		Generator:    gen,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if resp.FinalText != "Final answer: provider owned loop used" {
		t.Fatalf("expected provider owned loop response, got %q", resp.FinalText)
	}
}

type runtimeCapabilityLoopModeMockGenerator struct {
	calls int
	mode  ai.ReActLoopMode
}

func (m *runtimeCapabilityLoopModeMockGenerator) Name() string {
	return "runtime_capability_loop_mode_mock"
}

func (m *runtimeCapabilityLoopModeMockGenerator) EstimateCost(inTokens, outTokens int) float64 {
	return 0
}

func (m *runtimeCapabilityLoopModeMockGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func (m *runtimeCapabilityLoopModeMockGenerator) ProviderRuntimeCapability() ai.ProviderRuntimeCapability {
	return ai.ProviderRuntimeCapability{Provider: m.Name(), LoopMode: m.mode}
}

func (m *runtimeCapabilityLoopModeMockGenerator) ReActLoop() ai.ReActLoop {
	return ownedReActLoopMock{}
}

func (m *runtimeCapabilityLoopModeMockGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	_ = ctx
	_ = prompt
	_ = opts
	m.calls++
	if m.calls == 1 {
		return ai.GenOutput{ToolCalls: []ai.ToolCall{{
			Name: "execute_script",
			Args: map[string]any{
				"script": []any{map[string]any{"op": "scan"}},
			},
		}}}, nil
	}
	return ai.GenOutput{Text: "Final answer: runtime capability shared loop used"}, nil
}

func TestCompatibleReActEngine_RuntimeCapabilitySharedModeOverridesOwnedLoopProvider(t *testing.T) {
	engine := &CompatibleReActEngine{}
	gen := &runtimeCapabilityLoopModeMockGenerator{mode: ai.ReActLoopModeShared}

	resp, err := engine.Run(context.Background(), ai.ReasoningRequest{
		SystemPrompt: "You are a test assistant.",
		UserQuery:    "Find users",
		Executor:     &loopMockExecutor{},
		Generator:    gen,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if resp.FinalText != "Final answer: runtime capability shared loop used" {
		t.Fatalf("expected shared loop response, got %q", resp.FinalText)
	}
	if gen.calls != 2 {
		t.Fatalf("expected shared loop generator to be called twice, got %d", gen.calls)
	}
}
