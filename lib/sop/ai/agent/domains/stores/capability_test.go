package stores

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestStoresCapabilityHandlesStoresRouting(t *testing.T) {
	cap := NewCapability()
	result := cap.Classify("show inventory", capabilities.RoutingState{Domain: "Stores"})
	if !result.Relevant {
		t.Fatal("expected stores capability to classify stores routing as relevant")
	}
}

func TestStoresCapabilityBuildsContext(t *testing.T) {
	cap := NewCapability()
	ctx := cap.BuildContext("show inventory", capabilities.RoutingState{Domain: "Stores"})
	if ctx == "" {
		t.Fatal("expected stores capability context")
	}
}

func TestStoresCapabilityBuildsExecutionContext(t *testing.T) {
	cap := NewCapability()
	ctx := cap.BuildExecutionContext(ExecutionContextInput{CRUDFlags: map[string]bool{"R": true}, DBArtifacts: []string{"orders"}, ScriptAuthoring: true, ActiveDatabase: "dev_db"})
	if ctx == "" {
		t.Fatal("expected stores capability execution context")
	}
	if !strings.Contains(ctx, "Domain: Stores") {
		t.Fatalf("expected stores domain header, got %q", ctx)
	}
}

func TestStoresCapabilityBuildsGuidanceProfile(t *testing.T) {
	cap := NewCapability()
	profile := cap.BuildGuidanceProfile("show inventory", capabilities.RoutingState{Domain: "Stores"})
	if profile.Intensity != capabilities.GuidanceIntensityLight {
		t.Fatalf("expected light guidance intensity, got %q", profile.Intensity)
	}
	if len(profile.ToolSelection) == 0 {
		t.Fatal("expected stores capability to provide tool-selection guidance")
	}
}
