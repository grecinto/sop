package stores

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestStoresCapabilityHandlesStoresRouting(t *testing.T) {
	cap := NewCapability()
	if !cap.CanHandle("show inventory", capabilities.RoutingState{Domain: "Stores"}) {
		t.Fatal("expected stores capability to handle stores routing")
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
