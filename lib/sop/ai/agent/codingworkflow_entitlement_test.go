package agent

import (
	"testing"

	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestCopilotAgentRetrievalFallsBackWhenEntitlementMissing(t *testing.T) {
	ag := &CopilotAgent{}
	capsule, err := ag.Retrieve(cw.Task{Description: "implement parser"}, cw.RunState{}, cw.ContextPack{TaskType: "code", ActiveFile: "parser.go"})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if capsule.Summary != "" {
		t.Fatalf("expected empty expertise summary when entitlement is missing, got %q", capsule.Summary)
	}
}
