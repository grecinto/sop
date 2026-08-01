package agent

import "github.com/sharedcode/sop/ai"

type RetryCoordinator = ai.RetryCoordinator
type RetryTier = ai.RetryTier
type RepairStrategy = ai.RepairStrategy
type RepairPlan = ai.RepairPlan
type RetryPolicySurface = ai.RetryPolicySurface
type RetryDecisionInput = ai.RetryDecisionInput

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

func NewDefaultRetryPolicySurface() RetryPolicySurface {
	return ai.NewDefaultRetryPolicySurface()
}
