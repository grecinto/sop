package desktop

import (
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestDesktopCapabilityHandlesDesktopRouting(t *testing.T) {
	cap := NewCapability()
	if !cap.CanHandle("run tests", capabilities.RoutingState{Domain: "Desktop"}) {
		t.Fatal("expected desktop capability to handle desktop routing")
	}
}

func TestDesktopCapabilityBuildsContext(t *testing.T) {
	cap := NewCapability()
	ctx := cap.BuildContext("run tests", capabilities.RoutingState{Domain: "Desktop"})
	if ctx == "" {
		t.Fatal("expected desktop capability context")
	}
}
