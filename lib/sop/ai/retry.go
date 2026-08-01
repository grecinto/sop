package ai

import (
	"fmt"
	log "log/slog"
	"strings"
)

// RetryCoordinator implements progressive retry logic (Tier 1/2A/2B/3) shared across
// provider-owned and shared-compatible ReAct loops.
type RetryCoordinator struct{}

func NewRetryCoordinator() *RetryCoordinator {
	return &RetryCoordinator{}
}

type RetryTier int

const (
	RetryTierQuickFix RetryTier = 1
	RetryTierResearch RetryTier = 2
	RetryTierClarify  RetryTier = 3
)

type RepairStrategy string

const (
	RepairStrategySameTool      RepairStrategy = "same_tool"
	RepairStrategyResearchFirst RepairStrategy = "research_first"
)

type RepairPlan struct {
	ToolName       string
	Strategy       RepairStrategy
	Tier           RetryTier
	ResearchTool   string
	ResearchReason string
}

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

func (rc *RetryCoordinator) DetermineRetryTier(errorType string, failureCount int, hint *ToolProgressHint) RetryTier {
	if hint != nil && hint.SuggestedAction == "research_before_retry" {
		return RetryTierResearch
	}
	if hint != nil && hint.SuggestedAction == "give_up_and_clarify" {
		return RetryTierClarify
	}
	if failureCount == 0 {
		return RetryTierQuickFix
	}
	if failureCount == 1 {
		if rc.isResearchableError(errorType) {
			return RetryTierResearch
		}
		return RetryTierQuickFix
	}
	return RetryTierClarify
}

func (rc *RetryCoordinator) ClassifyRepair(toolName string, err error, hint *ToolProgressHint, recentlyResearched bool) RepairPlan {
	plan := RepairPlan{
		ToolName: toolName,
		Strategy: RepairStrategySameTool,
		Tier:     RetryTierQuickFix,
	}

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
			log.Info("Tier 2A: Tool suggests giving up", "tool", toolName)
		}
	}

	if toolName != "execute_script" || err == nil {
		return plan
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "relation") ||
		strings.Contains(message, "schema") ||
		strings.Contains(message, "join mapping") {
		if !recentlyResearched {
			plan.Strategy = RepairStrategyResearchFirst
			plan.ResearchTool = "list_stores"
			plan.ResearchReason = "The failure text points to unresolved schema or relation ambiguity."
		}
	}

	return plan
}

func (rc *RetryCoordinator) ShouldEscalateToClarification(repairAttempts int) bool {
	return repairAttempts >= 2
}

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
