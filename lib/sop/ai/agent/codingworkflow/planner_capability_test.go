package codingworkflow

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestPlannerCapabilityContextAndInstructions_AvoidWorkflowManagerGuidance(t *testing.T) {
	cap := NewPlannerCapability(NewDefaultSharedInfrastructure())

	contextText := cap.BuildContext("plan the parser refactor", capabilities.RoutingState{})
	if strings.Contains(contextText, "WorkflowManager") {
		t.Fatalf("expected planner context to avoid WorkflowManager guidance, got %q", contextText)
	}
	if !strings.Contains(contextText, "Ask-driven phase and workstream system") {
		t.Fatalf("expected planner context to describe the live execution path, got %q", contextText)
	}

	instructions := cap.BuildInstructions("plan the parser refactor", capabilities.RoutingState{})
	if strings.Contains(instructions, "WorkflowManager") {
		t.Fatalf("expected planner instructions to avoid WorkflowManager guidance, got %q", instructions)
	}
	if !strings.Contains(instructions, "phase and workstream system") {
		t.Fatalf("expected planner instructions to describe the live execution path, got %q", instructions)
	}
}
