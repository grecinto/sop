package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestResolveLLM_UsesRegistryModelWhenOverrideModelUnset(t *testing.T) {
	expected, ok := ai.GetModelCatalogRegistry().SelectBest(ai.ModelSelectionRequest{Provider: "anthropic"})
	if !ok {
		t.Fatal("expected catalog registry entry for anthropic")
	}

	agent := &CopilotAgent{}
	gen := agent.resolveLLM(context.Background(), &ProviderDetails{
		Provider: "anthropic",
		APIKey:   "test-key",
	})
	if gen == nil {
		t.Fatal("expected resolved generator")
	}

	type modelProfileProvider interface {
		ModelProfile() ai.ModelProfile
	}
	provider, ok := gen.(modelProfileProvider)
	if !ok {
		t.Fatalf("resolved generator type %T does not expose a model profile", gen)
	}
	if got := provider.ModelProfile().Model; got != expected.Model {
		t.Fatalf("resolved model = %q, want %q", got, expected.Model)
	}
}
