package agent

import (
	"context"

	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
)

type ContinueDelegationReference = ideworker.DelegationReference

type ContinueDelegationGuidance = ideworker.DelegationGuidance

func (a *CopilotAgent) BuildContinueDelegationGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (ContinueDelegationGuidance, error) {
	if a == nil || a.systemDB == nil {
		return ContinueDelegationGuidance{}, nil
	}
	ltm, err := a.getOrCreateLTM(ctx)
	if err != nil {
		return ContinueDelegationGuidance{}, nil
	}
	return ideworker.BuildDelegationGuidance(ctx, ltm, query, requestedPhase, variables)
}

func shouldEnrichContinueGuidance(requestedPhase string, variables map[string]any) bool {
	return ideworker.ShouldEnrichContinueGuidance(requestedPhase, variables)
}
