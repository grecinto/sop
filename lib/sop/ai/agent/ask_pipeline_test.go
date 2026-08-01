package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai"
)

type askRoutingTestGenerator struct {
	Response string
}

func (m *askRoutingTestGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{Text: m.Response}, nil
}

func (m *askRoutingTestGenerator) Name() string { return "askRoutingTestGenerator" }

func (m *askRoutingTestGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0.0 }

func (m *askRoutingTestGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func containsDomain(domains []string, want string) bool {
	for _, domain := range domains {
		if domain == want {
			return true
		}
	}
	return false
}

// TestClassifyAskTaskContext_Fallback tests that Ask classification has fallback behavior.
func TestClassifyAskTaskContext_Fallback(t *testing.T) {
	agent := &CopilotAgent{}

	// Test fallback classification
	fallback := agent.buildFallbackClassification("List stores")

	if len(fallback.Domains) == 0 {
		t.Fatal("Fallback should return at least one domain")
	}

	if fallback.Phase == "" {
		t.Fatal("Fallback should return a phase")
	}

	if fallback.Intent == "" {
		t.Fatal("Fallback should return an intent")
	}

	t.Logf("✅ Fallback classification: domains=%v, phase=%s, intent=%s",
		fallback.Domains, fallback.Phase, fallback.Intent)
}

// TestAskPhaseFactoryIntegration tests that the Ask pipeline can create phase proxies.
func TestAskPhaseFactoryIntegration(t *testing.T) {
	agent := &CopilotAgent{}

	// Test phase proxy creation
	domains := []string{"stores", "spaces"}
	phase := "investigation"

	phases := agent.createPhaseProxies(domains, phase)

	if len(phases) != 2 {
		t.Fatalf("Expected 2 phases, got %d", len(phases))
	}

	t.Logf("✅ Phase Factory created %d phases for domains %v", len(phases), domains)

	// Test tool aggregation
	classification := &AskTaskContext{
		Domains:   domains,
		Phase:     phase,
		PhasePlan: []string{phase},
		CRUD:      []string{"R"}, // Read-only for test
	}

	allTools := agent.aggregatePhaseTools(phases, classification)

	if len(allTools) == 0 {
		t.Fatal("Should have aggregated tools from phases")
	}

	t.Logf("✅ Aggregated %d tools from phases", len(allTools))
}
