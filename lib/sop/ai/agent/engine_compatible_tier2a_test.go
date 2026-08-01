package agent

import (
	"fmt"
	"testing"

	"github.com/sharedcode/sop/ai"
)

// Tier 2A: Test hint-driven retry strategy
func TestCompatibleReActEngine_Tier2A_HintDrivenRetryStrategy(t *testing.T) {
	// Test that hint with SuggestedAction overrides default strategy
	hint := &ai.ToolProgressHint{
		SuggestedAction:    "research_before_retry",
		SuggestedNextTools: []string{"describe_store_schema"},
		Tips:               []string{"Schema validation failed, need to verify column names"},
	}

	repair := classifyRecoverableToolRepair("execute_script", fmt.Errorf("column not found"), hint)

	if repair.Strategy != compatibleRepairStrategyResearchFirst {
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

	if repairNoHint.Strategy != compatibleRepairStrategySameTool {
		t.Fatalf("expected error without hint to use default same-tool strategy, got %v", repairNoHint.Strategy)
	}
}

// Test getLatestToolHint helper
func TestCompatibleReActEngine_Tier2A_GetLatestToolHint(t *testing.T) {
	// Empty results should return nil
	if hint := getLatestToolHint(nil); hint != nil {
		t.Fatal("expected nil hint from empty results")
	}

	if hint := getLatestToolHint([]compatibleToolResult{}); hint != nil {
		t.Fatal("expected nil hint from empty results")
	}

	// Should return hint from latest result
	results := []compatibleToolResult{
		{Name: "tool1", Hint: &ai.ToolProgressHint{Tips: []string{"old hint"}}},
		{Name: "tool2", Hint: &ai.ToolProgressHint{Tips: []string{"latest hint"}}},
	}

	hint := getLatestToolHint(results)
	if hint == nil || len(hint.Tips) == 0 || hint.Tips[0] != "latest hint" {
		t.Fatalf("expected latest hint, got %+v", hint)
	}
}

// Tier 2B: Test progressive retry tier determination
func TestCompatibleReActEngine_Tier2B_DetermineRetryTier(t *testing.T) {
	// Test progressive escalation based on failure count
	
	// First failure (0) -> Quick Fix
	tier := determineRetryTier("table not found", 0, nil)
	if tier != TierQuickFix {
		t.Fatalf("expected TierQuickFix for first failure, got %v", tier)
	}
	
	// Second failure (1) with researchable error -> Research
	tier = determineRetryTier("table 'users' not found", 1, nil)
	if tier != TierResearch {
		t.Fatalf("expected TierResearch for second failure with researchable error, got %v", tier)
	}
	
	// Second failure (1) with non-researchable error -> Quick Fix
	tier = determineRetryTier("invalid syntax", 1, nil)
	if tier != TierQuickFix {
		t.Fatalf("expected TierQuickFix for second failure with non-researchable error, got %v", tier)
	}
	
	// Third failure (2) -> Clarify
	tier = determineRetryTier("any error", 2, nil)
	if tier != TierClarify {
		t.Fatalf("expected TierClarify for third failure, got %v", tier)
	}
	
	// Hint override: research_before_retry (any failure count)
	hint := &ai.ToolProgressHint{SuggestedAction: "research_before_retry"}
	tier = determineRetryTier("any error", 0, hint)
	if tier != TierResearch {
		t.Fatalf("expected TierResearch when hint suggests research, got %v", tier)
	}
	
	// Hint override: give_up_and_clarify (any failure count)
	hint = &ai.ToolProgressHint{SuggestedAction: "give_up_and_clarify"}
	tier = determineRetryTier("any error", 0, hint)
	if tier != TierClarify {
		t.Fatalf("expected TierClarify when hint suggests giving up, got %v", tier)
	}
}

// Test researchable error classification
func TestCompatibleReActEngine_Tier2B_IsResearchableError(t *testing.T) {
	researchable := []string{
		"table 'users' not found",
		"relation does not exist",
		"schema validation failed",
		"store not found",
		"column 'id' does not exist",
		"join mapping invalid",
	}
	
	for _, errorMsg := range researchable {
		if !isResearchableError(errorMsg) {
			t.Fatalf("expected '%s' to be researchable", errorMsg)
		}
	}
	
	nonResearchable := []string{
		"syntax error",
		"invalid query",
		"connection timeout",
		"permission denied",
	}
	
	for _, errorMsg := range nonResearchable {
		if isResearchableError(errorMsg) {
			t.Fatalf("expected '%s' to NOT be researchable", errorMsg)
		}
	}
}

// Test escalation threshold
func TestCompatibleReActEngine_Tier2B_EscalationThreshold(t *testing.T) {
	// Should not escalate on first two failures
	if shouldEscalateRepairToClarification(0) {
		t.Fatal("should not escalate on first failure")
	}
	
	if shouldEscalateRepairToClarification(1) {
		t.Fatal("should not escalate on second failure")
	}
	
	// Should escalate on third failure
	if !shouldEscalateRepairToClarification(2) {
		t.Fatal("should escalate on third failure")
	}
	
	if !shouldEscalateRepairToClarification(3) {
		t.Fatal("should escalate on fourth+ failures")
	}
}
