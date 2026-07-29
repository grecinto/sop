package desktop

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestDesktopCapabilityHandlesDesktopRouting(t *testing.T) {
	cap := NewCapability()
	result := cap.Classify("run tests", capabilities.RoutingState{Domain: "Desktop"})
	if !result.Relevant {
		t.Fatal("expected desktop capability to classify desktop routing as relevant")
	}
}

func TestDesktopCapabilityBuildsContext(t *testing.T) {
	cap := NewCapability()
	ctx := cap.BuildContext("run tests", capabilities.RoutingState{Domain: "Desktop"})
	if ctx == "" {
		t.Fatal("expected desktop capability context")
	}
}

func TestDesktopCapabilityBuildsInstructions(t *testing.T) {
	cap := NewCapability()
	instructions := cap.BuildInstructions("run dev server", capabilities.RoutingState{Domain: "Desktop"})

	// Verify key workflow concepts are present
	if instructions == "" {
		t.Fatal("expected non-empty instructions")
	}
	if !strings.Contains(instructions, "State Persistence") {
		t.Error("expected instructions to mention State Persistence")
	}
	if !strings.Contains(instructions, "Background Execution") {
		t.Error("expected instructions to mention Background Execution")
	}
	if !strings.Contains(instructions, "background=true") {
		t.Error("expected instructions to show background parameter usage")
	}
	if !strings.Contains(instructions, "npm run dev") {
		t.Error("expected instructions to include dev server example")
	}
	if !strings.Contains(instructions, "Coding Workflow Pattern") {
		t.Error("expected instructions to include workflow pattern")
	}
	if !strings.Contains(instructions, "Session Recovery") {
		t.Error("expected instructions to mention session recovery")
	}
}
