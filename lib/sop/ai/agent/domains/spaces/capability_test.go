package spaces

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestSpacesCapabilityHandlesSpacesRouting(t *testing.T) {
	cap := NewCapability()
	result := cap.Classify("show spaces", capabilities.RoutingState{Domain: "Spaces"})
	if !result.Relevant {
		t.Fatal("expected spaces capability to classify spaces routing as relevant")
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
