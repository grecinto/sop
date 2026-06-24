package agent

import (
	"strings"
	"testing"
)

func TestServiceBiomimicryFeatureToggle(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, false)
	if !svc.BiomimicryEnabled() {
		t.Fatalf("expected biomimicry to be enabled by default")
	}

	svc.SetFeature("biomimicry", false)
	if svc.BiomimicryEnabled() {
		t.Fatalf("expected biomimicry to be disabled after feature toggle")
	}
}

func TestCopilotAgentIncludesStoredCaveatsInPromptContext(t *testing.T) {
	ag := &CopilotAgent{service: &Service{session: NewRunnerSession()}}
	ag.service.session.Memory = NewShortTermMemory()
	ag.service.session.Memory.AddCaveat("Do not mention PHI")
	ag.service.session.Memory.AddCaveat("Use concise answers")

	got := ag.getCaveatContext()
	if !strings.Contains(got, "Do not mention PHI") || !strings.Contains(got, "Use concise answers") {
		t.Fatalf("expected caveats to be injected into prompt context, got %q", got)
	}
}
