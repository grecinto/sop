package agent

import (
	"fmt"
	log "log/slog"
	"strings"

	"github.com/sharedcode/sop/ai"
)

// RetryCoordinator implements progressive retry logic (Tier 1/2A/2B/3) shared across
// all ReAct loop adapters. This ensures consistent retry behavior regardless of
// which LLM provider is in use.
//
// Progressive Retry Tiers:
//   - Tier 1 (Quick Fix): First failure - retry same tool with corrections
//   - Tier 2A (Hint-Driven): LLM-controlled strategy via ToolProgressHint.SuggestedAction
//   - Tier 2B (Research): Second failure on researchable errors - call research tool first
//   - Tier 3 (Clarify): Third+ failure - escalate to user clarification (backward phase transition)
//
// The coordinator is stateless - it doesn't track failure counts itself. The caller
// (unified engine) maintains failure tracking and passes current state to each method.
type RetryCoordinator struct{}

// NewRetryCoordinator creates a new retry coordinator instance.
func NewRetryCoordinator() *RetryCoordinator {
	return &RetryCoordinator{}
}

// RetryTier represents the progressive escalation level
type RetryTier int

const (
	// RetryTierQuickFix: First failure - retry same tool with corrections
	RetryTierQuickFix RetryTier = 1
	// RetryTierResearch: Second failure - research before retry
	RetryTierResearch RetryTier = 2
	// RetryTierClarify: Third+ failure - escalate to user clarification
	RetryTierClarify RetryTier = 3
)

// RepairStrategy defines how to recover from a tool execution failure
type RepairStrategy string

const (
	// RepairStrategySameTool retries the same tool with corrected arguments
	RepairStrategySameTool RepairStrategy = "same_tool"
	// RepairStrategyResearchFirst calls a research tool before retrying the failed tool
	RepairStrategyResearchFirst RepairStrategy = "research_first"
)

// RepairPlan describes how to recover from a tool failure
type RepairPlan struct {
	ToolName       string         // The tool that failed
	Strategy       RepairStrategy // How to repair (same_tool or research_first)
	Tier           RetryTier      // Escalation tier (1=quick_fix, 2=research, 3=clarify)
	ResearchTool   string         // Tool to call first if strategy=research_first
	ResearchReason string         // Why research is needed
}

// IsRecoverableToolError determines if a tool execution error can be retried.
// Recoverable errors typically indicate validation issues or missing parameters.
func (rc *RetryCoordinator) IsRecoverableToolError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	recoverableHints := []string{
		"missing",
		"required",
		"not a string",
		"invalid type",
		"must be",
		"expected",
		"validation failed",
		"grammar validation",
		"unnecessary nested joins",
	}
	for _, hint := range recoverableHints {
		if strings.Contains(message, hint) {
			return true
		}
	}
	return false
}

// DetermineRetryTier determines the escalation tier based on failure count,
// error type, and tool hints. This implements Tier 2B progressive escalation.
//
// Tier progression:
//   - failureCount=0: TierQuickFix (retry with corrections)
//   - failureCount=1: TierResearch if error is researchable, else TierQuickFix
//   - failureCount>=2: TierClarify (escalate to user)
//
// Tier 2A overrides:
//   - hint.SuggestedAction="research_before_retry" → TierResearch immediately
//   - hint.SuggestedAction="give_up_and_clarify" → TierClarify immediately
func (rc *RetryCoordinator) DetermineRetryTier(
	errorType string,
	failureCount int,
	hint *ai.ToolProgressHint,
) RetryTier {
	// Tier 2A: Tool explicitly suggests research
	if hint != nil && hint.SuggestedAction == "research_before_retry" {
		return RetryTierResearch
	}

	// Tier 2A: Tool explicitly suggests giving up
	if hint != nil && hint.SuggestedAction == "give_up_and_clarify" {
		return RetryTierClarify
	}

	// Tier 2B: Progressive escalation based on failure count
	if failureCount == 0 {
		// First failure - try quick fix
		return RetryTierQuickFix
	}

	if failureCount == 1 {
		// Second failure - escalate to research if error is researchable
		if rc.isResearchableError(errorType) {
			return RetryTierResearch
		}
		// Otherwise stay in quick fix
		return RetryTierQuickFix
	}

	// Third+ failure - escalate to clarification
	return RetryTierClarify
}

// ClassifyRepair analyzes a tool failure and determines the repair strategy.
// This implements Tier 2A (hint-driven) and heuristic-based repair classification.
func (rc *RetryCoordinator) ClassifyRepair(
	toolName string,
	err error,
	hint *ai.ToolProgressHint,
) RepairPlan {
	plan := RepairPlan{
		ToolName: toolName,
		Strategy: RepairStrategySameTool,
		Tier:     RetryTierQuickFix, // Default, caller updates via DetermineRetryTier
	}

	// Tier 2A: Respect hint's suggested action if present
	if hint != nil && hint.SuggestedAction != "" {
		switch hint.SuggestedAction {
		case "research_before_retry":
			plan.Strategy = RepairStrategyResearchFirst
			plan.ResearchTool = "list_stores"
			if len(hint.SuggestedNextTools) > 0 {
				plan.ResearchTool = hint.SuggestedNextTools[0]
			}
			if len(hint.Tips) > 0 {
				plan.ResearchReason = hint.Tips[0]
			} else {
				plan.ResearchReason = "Tool suggested research before retry"
			}
			log.Info("Tier 2A: Using hint-driven retry strategy",
				"tool", toolName,
				"strategy", "research_first",
				"research_tool", plan.ResearchTool,
				"reason", plan.ResearchReason)
			return plan
		case "give_up_and_clarify":
			// Tool explicitly says this is unrecoverable - escalate immediately
			log.Info("Tier 2A: Tool suggests giving up", "tool", toolName)
			// Fall through to default logic for now
		}
	}

	// Heuristic-based repair for execute_script
	if toolName != "execute_script" || err == nil {
		return plan
	}

	// Check for schema/relation-related errors
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "relation") ||
		strings.Contains(message, "schema") ||
		strings.Contains(message, "join mapping") {
		plan.Strategy = RepairStrategyResearchFirst
		plan.ResearchTool = "list_stores"
		plan.ResearchReason = "The failure text points to unresolved schema or relation ambiguity."
	}

	return plan
}

// ShouldEscalateToClarification checks if we've exceeded the retry threshold
// and should escalate to Tier 3 (user clarification).
func (rc *RetryCoordinator) ShouldEscalateToClarification(repairAttempts int) bool {
	// Tier 2B: Escalate after 2 failures (0 and 1 = quick fix/research, 2+ = clarify)
	return repairAttempts >= 2
}

// FormatRepairDirective builds a repair prompt for the LLM based on the repair plan.
func (rc *RetryCoordinator) FormatRepairDirective(plan RepairPlan, attemptedToolName string) string {
	if plan.Strategy == RepairStrategyResearchFirst {
		allowedTool := plan.ResearchTool
		if allowedTool == "" {
			allowedTool = "list_stores"
		}
		return fmt.Sprintf(
			"Repair required before continuing.\nTool: %s\nRepair note: The model attempted %s instead.\nRetry instruction: Call %s next, preferably with scoped stores:[...] for the likely targets, use its schema/relations output as source of truth, then return to %s. Do not switch to unrelated tools or provide a final answer until the research attempt is made.",
			plan.ToolName,
			attemptedToolName,
			allowedTool,
			plan.ToolName,
		)
	}
	return fmt.Sprintf(
		"Repair required before continuing.\nTool: %s\nRepair note: The model attempted %s instead.\nRetry instruction: Call %s next with corrected arguments. Do not switch tools or provide a final answer until the repair attempt is made.",
		plan.ToolName,
		attemptedToolName,
		plan.ToolName,
	)
}

// AllowsTool checks if a tool is allowed under the current repair plan.
// For research_first strategy, only the research tool is allowed.
// For same_tool strategy, only the failed tool is allowed.
func (rc *RetryCoordinator) AllowsTool(plan RepairPlan, toolName string) bool {
	if plan.Strategy == RepairStrategyResearchFirst {
		researchTool := plan.ResearchTool
		if researchTool == "" {
			researchTool = "list_stores"
		}
		return toolName == researchTool
	}
	return toolName == plan.ToolName
}

// isResearchableError determines if an error type benefits from research.
// Errors related to missing knowledge (schema, relations, stores) are researchable.
func (rc *RetryCoordinator) isResearchableError(errorType string) bool {
	researchable := []string{
		"table", "relation", "schema", "store", "column",
		"join", "mapping", "not found", "does not exist",
	}
	errorLower := strings.ToLower(errorType)
	for _, keyword := range researchable {
		if strings.Contains(errorLower, keyword) {
			return true
		}
	}
	return false
}
