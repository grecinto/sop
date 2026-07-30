package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	plannerpkg "github.com/sharedcode/sop/ai/agent/planner"
)

type recordingSTMReader struct {
	phases      []string
	checkpoints map[string]bool
	reasoning   string
	confidence  string
}

func (r *recordingSTMReader) GetCurrentThread() cw.WorkStreamReader { return nil }

func (r *recordingSTMReader) CreatePhasePlan(phases []string, checkpoints map[string]bool, reasoning, confidence string) error {
	r.phases = append([]string(nil), phases...)
	r.checkpoints = make(map[string]bool, len(checkpoints))
	for k, v := range checkpoints {
		r.checkpoints[k] = v
	}
	r.reasoning = reasoning
	r.confidence = confidence
	return nil
}

func TestPlannerCapability_ClassifiesCodingWorkflowRequests(t *testing.T) {
	cap := plannerpkg.NewPlannerCapability(nil)
	result := cap.Classify("fix the parser bug in the repository", capabilities.RoutingState{CapabilityHint: "planner"})

	if !result.Relevant {
		t.Fatalf("expected planner capability to match coding workflow requests")
	}
	if result.Mode != capabilities.CapabilityModePrimary {
		t.Fatalf("expected primary mode, got %v", result.Mode)
	}
	if cap.ID() != "planner" {
		t.Fatalf("expected planner capability id to be planner, got %q", cap.ID())
	}
	if cap.Name() != "Planner" {
		t.Fatalf("expected planner capability name to be Planner, got %q", cap.Name())
	}
}

func TestPlannerCapability_ExecuteToolCreatesPhasePlan(t *testing.T) {
	stm := &recordingSTMReader{}
	infra := cw.NewDefaultSharedInfrastructure()
	infra.STMAccessor = func() cw.STMReader { return stm }

	cap := plannerpkg.NewPlannerCapability(infra)
	output, err := cap.ExecuteTool(context.Background(), "create_phase_plan", map[string]any{
		"query": "fix the parser bug in the repo",
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("ExecuteTool returned error: %v", err)
	}
	if output == "" {
		t.Fatal("expected non-empty output")
	}
	if len(stm.phases) == 0 {
		t.Fatal("expected phase plan to be stored")
	}
	if stm.phases[0] != "intake" {
		t.Fatalf("expected intake to be first, got %q", stm.phases[0])
	}
	if stm.checkpoints["after_investigation"] != true {
		t.Fatalf("expected investigation checkpoint to be recorded, got %+v", stm.checkpoints)
	}
}
