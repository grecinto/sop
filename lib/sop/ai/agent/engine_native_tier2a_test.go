package agent

import (
	"fmt"
	"testing"

	"github.com/sharedcode/sop/ai"
)

// Tier 2A: Test hint-driven retry strategy
func TestEngineNative_Tier2A_HintDrivenRetryStrategy(t *testing.T) {
	// Test that hint with SuggestedAction overrides default strategy
	hint := &ai.ToolProgressHint{
		SuggestedAction:    "research_before_retry",
		SuggestedNextTools: []string{"describe_store_schema"},
		Tips:               []string{"Schema validation failed, need to verify column names"},
	}

	repair := classifyRecoverableToolRepair("execute_script", fmt.Errorf("column not found"), hint)

	if repair.Strategy != nativeRepairStrategyResearchFirst {
		t.Fatalf("expected hint to trigger research-first strategy, got %v", repair.Strategy)
	}

	if repair.ResearchTool != "describe_store_schema" {
		t.Fatalf("expected research tool from hint, got %v", repair.ResearchTool)
	}

	if repair.ResearchReason != "Schema validation failed, need to verify column names" {
		t.Fatalf("expected research reason from hint, got %v", repair.ResearchReason)
	}

	// Test that without hint, falls back to error-based classification
	repairNoHint := classifyRecoverableToolRepair("execute_script", fmt.Errorf("column not found"), nil)

	if repairNoHint.Strategy != nativeRepairStrategySameTool {
		t.Fatalf("expected error without hint to use default same-tool strategy, got %v", repairNoHint.Strategy)
	}
}

// Test getLatestToolHint helper
func TestEngineNative_Tier2A_GetLatestToolHint(t *testing.T) {
	// Empty results should return nil
	if hint := getLatestToolHint(nil); hint != nil {
		t.Fatal("expected nil hint from empty results")
	}

	if hint := getLatestToolHint([]nativeToolResult{}); hint != nil {
		t.Fatal("expected nil hint from empty results")
	}

	// Should return hint from latest result
	results := []nativeToolResult{
		{Name: "tool1", Hint: &ai.ToolProgressHint{Tips: []string{"old hint"}}},
		{Name: "tool2", Hint: &ai.ToolProgressHint{Tips: []string{"latest hint"}}},
	}

	hint := getLatestToolHint(results)
	if hint == nil || len(hint.Tips) == 0 || hint.Tips[0] != "latest hint" {
		t.Fatalf("expected latest hint, got %+v", hint)
	}
}
