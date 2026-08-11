package planner

import (
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// NewPhase creates a phase capability dynamically based on domain and phase ID.
// This is the factory method that selects the appropriate domain-specific implementation.
//
// Domain values: "coding_workflow", "stores", "spaces", "desktop"
// Phase IDs: "intake", "investigation", "planning", "testing", "execution", "review", "delivery"
func NewPhase(domain, phaseID string, infra *SharedInfrastructure) capabilities.Capability {
	switch domain {
	case "coding_workflow", "coding", "codingworkflow":
		return newCodingPhase(phaseID, infra)
	case "stores":
		return newStoresPhase(phaseID, infra)
	case "spaces":
		return newSpacesPhase(phaseID, infra)
	case "desktop":
		return newDesktopPhase(phaseID, infra)
	default:
		// Fallback to coding workflow for backward compatibility
		return newCodingPhase(phaseID, infra)
	}
}

// Backward-compatible factory functions for coding_workflow domain
// These are kept for existing code that creates phases directly

func NewIntakePhaseCapability(infra *SharedInfrastructure) capabilities.Capability {
	return newCodingPhase("intake", infra)
}

func NewInvestigationPhaseCapability(infra *SharedInfrastructure) capabilities.Capability {
	return newCodingPhase("investigation", infra)
}

func NewPlanningPhaseCapability(infra *SharedInfrastructure) capabilities.Capability {
	return newCodingPhase("planning", infra)
}

func NewTestingPhaseCapability(infra *SharedInfrastructure) capabilities.Capability {
	return newCodingPhase("testing", infra)
}

func NewExecutionPhaseCapability(infra *SharedInfrastructure) capabilities.Capability {
	return newCodingPhase("execution", infra)
}

func NewReviewPhaseCapability(infra *SharedInfrastructure) capabilities.Capability {
	return newCodingPhase("review", infra)
}

func NewDeliveryPhaseCapability(infra *SharedInfrastructure) capabilities.Capability {
	return newCodingPhase("delivery", infra)
}

// NewDefaultSharedInfrastructure creates default shared infrastructure for phases
func NewDefaultSharedInfrastructure() *SharedInfrastructure {
	return cw.NewDefaultSharedInfrastructure()
}
