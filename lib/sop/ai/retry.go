package ai

import (
	"fmt"
	log "log/slog"
	"strings"
	"time"
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

type RetryLearning struct {
	ToolName            string         `json:"tool_name,omitempty"`
	ErrorPattern        string         `json:"error_pattern,omitempty"`
	PreferredStrategy   RepairStrategy `json:"preferred_strategy,omitempty"`
	SuccessCount        int            `json:"success_count,omitempty"`
	FailureCount        int            `json:"failure_count,omitempty"`
	LastUsedAtUnixMilli int64          `json:"last_used_at_unix_milli,omitempty"`
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
		"relation",
		"schema",
		"join mapping",
		"not found",
		"does not exist",
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

func (rc *RetryCoordinator) ApplyLearnedStrategy(plan RepairPlan, toolName string, err error, learnings []RetryLearning) RepairPlan {
	strategy, ok := rc.RecommendStrategy(toolName, err, learnings)
	if !ok {
		return plan
	}
	plan.Strategy = strategy
	if strategy == RepairStrategyResearchFirst {
		if plan.ResearchTool == "" {
			plan.ResearchTool = "list_stores"
		}
		if plan.ResearchReason == "" {
			plan.ResearchReason = "Learned from prior successful retries for a similar failure."
		}
		if plan.Tier < RetryTierResearch {
			plan.Tier = RetryTierResearch
		}
	}
	return plan
}

func (rc *RetryCoordinator) RecommendStrategy(toolName string, err error, learnings []RetryLearning) (RepairStrategy, bool) {
	pattern := rc.ErrorPattern(toolName, err)
	bestScore := -1
	var bestStrategy RepairStrategy
	for _, learning := range learnings {
		if learning.ToolName != toolName || learning.ErrorPattern != pattern {
			continue
		}
		score := learning.SuccessCount - learning.FailureCount
		if score > bestScore && learning.SuccessCount > 0 {
			bestScore = score
			bestStrategy = learning.PreferredStrategy
		}
	}
	if bestScore <= 0 || bestStrategy == "" {
		return "", false
	}
	return bestStrategy, true
}

func (rc *RetryCoordinator) RecordOutcome(learnings []RetryLearning, toolName string, err error, plan RepairPlan, success bool) []RetryLearning {
	pattern := rc.ErrorPattern(toolName, err)
	now := time.Now().UnixMilli()
	for i := range learnings {
		if learnings[i].ToolName != toolName || learnings[i].ErrorPattern != pattern || learnings[i].PreferredStrategy != plan.Strategy {
			continue
		}
		if success {
			learnings[i].SuccessCount++
		} else {
			learnings[i].FailureCount++
		}
		learnings[i].LastUsedAtUnixMilli = now
		return learnings
	}
	learning := RetryLearning{
		ToolName:            toolName,
		ErrorPattern:        pattern,
		PreferredStrategy:   plan.Strategy,
		LastUsedAtUnixMilli: now,
	}
	if success {
		learning.SuccessCount = 1
	} else {
		learning.FailureCount = 1
	}
	learnings = append([]RetryLearning{learning}, learnings...)
	if len(learnings) > 16 {
		learnings = learnings[:16]
	}
	return learnings
}

func (rc *RetryCoordinator) CalculateAdaptiveBudget(base int, query string, learnings []RetryLearning) int {
	if base <= 0 {
		base = 1
	}
	budget := base
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(queryLower, "workflow") ||
		strings.Contains(queryLower, "implement") ||
		strings.Contains(queryLower, "refactor") ||
		strings.Contains(queryLower, "fix") ||
		strings.Contains(queryLower, "commit") {
		budget++
	}
	for _, learning := range learnings {
		if learning.SuccessCount > learning.FailureCount {
			budget++
			break
		}
	}
	if budget > base+3 {
		budget = base + 3
	}
	return budget
}

func (rc *RetryCoordinator) ErrorPattern(toolName string, err error) string {
	if err == nil {
		return strings.TrimSpace(toolName) + ":unknown"
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	message = strings.Join(strings.Fields(message), " ")
	if idx := strings.Index(message, " example fix:"); idx >= 0 {
		message = strings.TrimSpace(message[:idx])
	}
	if len(message) > 160 {
		message = message[:160]
	}
	return strings.TrimSpace(toolName) + ":" + message
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
