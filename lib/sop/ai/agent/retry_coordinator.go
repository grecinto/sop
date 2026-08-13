package agent

import "github.com/sharedcode/sop/ai"

type RetryCoordinator = ai.RetryCoordinator
type RetryTier = ai.RetryTier
type RepairStrategy = ai.RepairStrategy
type RepairPlan = ai.RepairPlan
type RetryPolicySurface = ai.RetryPolicySurface
type RetryDecisionInput = ai.RetryDecisionInput
type CognitivePolicySurface = ai.CognitivePolicySurface
type SessionCognitiveInput = ai.SessionCognitiveInput
type SessionCognitiveObservation = ai.SessionCognitiveObservation
type SessionCognitiveAssessment = ai.SessionCognitiveAssessment

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

func NewDefaultCognitivePolicySurface() CognitivePolicySurface {
	return ai.NewDefaultCognitivePolicySurface()
}
