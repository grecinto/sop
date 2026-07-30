package planner

import (
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestPlannerCapability_RegistersAsPrimaryCodingPlanner(t *testing.T) {
	cap := NewPlannerCapability(nil)
	result := cap.Classify("fix the parser bug in the repository", capabilities.RoutingState{CapabilityHint: "planner"})

	if !result.Relevant {
		t.Fatalf("expected planner capability to match coding workflow requests")
	}
	if result.Mode != capabilities.CapabilityModePrimary {
		t.Fatalf("expected primary mode, got %v", result.Mode)
	}
}

func TestPlanningPhaseCapability_UsesPlannerPackage(t *testing.T) {
	cap := NewPlanningPhaseCapability(nil)
	if cap == nil {
		t.Fatal("expected planning phase capability")
	}
	if cap.ID() != "phase_planning" {
		t.Fatalf("expected phase_planning id, got %q", cap.ID())
	}
}
