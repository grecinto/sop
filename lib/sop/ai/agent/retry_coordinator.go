package agent

import "github.com/sharedcode/sop/ai"

type RetryCoordinator = ai.RetryCoordinator
type RetryTier = ai.RetryTier
type RepairStrategy = ai.RepairStrategy
type RepairPlan = ai.RepairPlan

const (
	RetryTierQuickFix = ai.RetryTierQuickFix
	RetryTierResearch = ai.RetryTierResearch
	RetryTierClarify  = ai.RetryTierClarify

	RepairStrategySameTool      = ai.RepairStrategySameTool
	RepairStrategyResearchFirst = ai.RepairStrategyResearchFirst
)

func NewRetryCoordinator() *RetryCoordinator {
	return ai.NewRetryCoordinator()
}
