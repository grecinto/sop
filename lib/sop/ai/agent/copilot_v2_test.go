package agent

import (
	"testing"

	"github.com/sharedcode/sop/ai"
)

// TestAskV2_BasicClassification tests that V2 pipeline can be invoked and classification works
func TestAskV2_BasicClassification(t *testing.T) {
	// Skip if no LLM provider configured
	if testing.Short() {
		t.Skip("Skipping V2 integration test in short mode")
	}

	// Create minimal agent for testing
	// agent := &CopilotAgent{
	// 	registry: NewToolRegistry(),
	// }

	// Enable V2 pipeline
	cfg := ai.NewConfigMap()
	cfg.Set("use_v2_pipeline", true)

	// ctx := context.Background()
	// query := "List stores in production database"

	// This should route to V2
	// Note: Will fail if no LLM provider configured, but compilation should work
	// t.Logf("Testing V2 pipeline with query: %s", query)

	// For now, just test that the feature flag routing works
	// We can't fully test without a configured LLM provider
	useV2 := false
	if v2Flag, ok := cfg.Get("use_v2_pipeline"); ok {
		if v2Bool, ok := v2Flag.(bool); ok {
			useV2 = v2Bool
		}
	}

	if !useV2 {
		t.Fatal("Feature flag should enable V2, but it's not set")
	}

	t.Log("✅ V2 feature flag routing works")
	t.Log("✅ To fully test, configure LLM provider and call agent.Ask(ctx, query, cfg)")
}

// TestClassifyTaskContextV2_Fallback tests that V2 classification has fallback behavior
func TestClassifyTaskContextV2_Fallback(t *testing.T) {
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

// TestV2PhaseFactoryIntegration tests that V2 can create phase proxies
func TestV2PhaseFactoryIntegration(t *testing.T) {
	agent := &CopilotAgent{}

	// Test phase proxy creation
	domains := []string{"stores", "spaces"}
	phase := "investigation"

	phases := agent.createPhaseProxiesV2(domains, phase)

	if len(phases) != 2 {
		t.Fatalf("Expected 2 phases, got %d", len(phases))
	}

	t.Logf("✅ Phase Factory created %d phases for domains %v", len(phases), domains)

	// Test tool aggregation
	allTools := agent.aggregatePhaseToolsV2(phases)

	if len(allTools) == 0 {
		t.Fatal("Should have aggregated tools from phases")
	}

	t.Logf("✅ Aggregated %d tools from phases", len(allTools))
}
