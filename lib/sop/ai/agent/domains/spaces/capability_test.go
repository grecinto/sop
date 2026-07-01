package spaces

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestSpacesCapabilityHandlesSpacesRouting(t *testing.T) {
	cap := NewCapability()
	if !cap.CanHandle("show spaces", capabilities.RoutingState{Domain: "Spaces"}) {
		t.Fatal("expected spaces capability to handle spaces routing")
	}
}

func TestSpacesCapabilityBuildsContext(t *testing.T) {
	cap := NewCapability()
	ctx := cap.BuildContext("show spaces", capabilities.RoutingState{Domain: "Spaces"})
	if ctx == "" {
		t.Fatal("expected spaces capability context")
	}
}

func TestSpacesCapabilityBuildsExecutionContext(t *testing.T) {
	cap := NewCapability()
	ctx := cap.BuildExecutionContext(ExecutionContextInput{CRUDFlags: map[string]bool{"R": true}, DBArtifacts: []string{"ops"}})
	if ctx == "" {
		t.Fatal("expected spaces capability execution context")
	}
	if !strings.Contains(ctx, "Domain: Spaces") {
		t.Fatalf("expected spaces domain header, got %q", ctx)
	}
}

func TestSpacesCapabilityRunUsesSharedWorkflow(t *testing.T) {
	cap := NewCapability(&codingworkflow.Orchestrator{})
	result, err := cap.Run("generate a JSON artifact for a space config", capabilities.RoutingState{Domain: "Spaces"})
	if err != nil {
		t.Fatalf("expected shared workflow execution to succeed: %v", err)
	}
	if result.Summary == "" {
		t.Fatal("expected spaces capability to return a summary")
	}
	if result.Data == nil {
		t.Fatal("expected spaces capability to return workflow data")
	}
	if got := result.Data["task_type"]; got != "generation" {
		t.Fatalf("expected generation task type, got %#v", got)
	}
}
