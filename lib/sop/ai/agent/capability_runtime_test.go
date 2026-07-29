package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/domains/desktop"
	"github.com/sharedcode/sop/ai/agent/domains/spaces"
	"github.com/sharedcode/sop/ai/agent/domains/stores"
)

func TestBuildFocusedToolContextUsesCapabilityRegistry(t *testing.T) {
	ag := &CopilotAgent{}
	reg := capabilities.NewRegistry()
	reg.Register(&stubCapability{id: "stores", name: "Stores", context: "capability context"})
	ag.capabilityRegistry = reg

	taskCtx := &TaskContextClassification{Domain: StoresDomain, RoutingGate: RoutingGateFocused}
	got := ag.buildFocusedToolContext(context.Background(), "", taskCtx)
	if !strings.Contains(got, "capability context") {
		t.Fatalf("expected focused tool context to include capability context, got %q", got)
	}
}

func TestNewCopilotAgentRegistersDesktopCapability(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	if ag.capabilityRegistry == nil {
		t.Fatal("expected capability registry to be initialized")
	}

	cap, ok := ag.capabilityRegistry.Resolve("", capabilities.RoutingState{Domain: "Desktop"})
	if !ok {
		t.Fatal("expected desktop capability to be registered")
	}
	if cap.ID() != desktop.CapabilityID {
		t.Fatalf("expected desktop capability id %q, got %q", desktop.CapabilityID, cap.ID())
	}
}

func TestNewCopilotAgentRegistersStoresCapability(t *testing.T) {
	SetStoresFeatureEnabled(true)
	defer SetStoresFeatureEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	cap, ok := ag.capabilityRegistry.Resolve("", capabilities.RoutingState{Domain: "Stores"})
	if !ok {
		t.Fatal("expected stores capability to be registered")
	}
	if cap.ID() != stores.CapabilityID {
		t.Fatalf("expected stores capability id %q, got %q", stores.CapabilityID, cap.ID())
	}
}

func TestNewCopilotAgentRegistersSpacesCapability(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	cap, ok := ag.capabilityRegistry.Resolve("", capabilities.RoutingState{Domain: "Spaces"})
	if !ok {
		t.Fatal("expected spaces capability to be registered")
	}
	if cap.ID() != spaces.CapabilityID {
		t.Fatalf("expected spaces capability id %q, got %q", spaces.CapabilityID, cap.ID())
	}
}

func TestNewCopilotAgentRegistersCodingWorkflowCapabilityByDefault(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	// Phase capabilities should be registered by default
	var foundPhaseCapability bool
	for _, cap := range ag.capabilityRegistry.ListAll() {
		if cap != nil && strings.HasPrefix(cap.ID(), "phase_") {
			foundPhaseCapability = true
			break
		}
	}
	if !foundPhaseCapability {
		t.Fatal("expected phase capabilities to be registered by default")
	}
}

func TestNewCopilotAgentRegistersServiceCapabilityByDefault(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	var found bool
	for _, cap := range ag.capabilityRegistry.ListAll() {
		if cap != nil && cap.ID() == "coding_workflow_services" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected coding workflow services capability to be registered by default")
	}
}

func TestNewCopilotAgentAppliesCapabilityOptions(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil, WithCapability(&stubCapability{id: "stores", name: "Stores", context: "option context"}))
	cap, ok := ag.capabilityRegistry.Resolve("", capabilities.RoutingState{Domain: "Stores"})
	if !ok {
		t.Fatal("expected option-registered capability to be available")
	}
	if cap.ID() != "stores" {
		t.Fatalf("expected stores capability, got %q", cap.ID())
	}
}

func TestRegisterToolsSkipsLegacyDomainToolsWithoutCapabilities(t *testing.T) {
	ag := &CopilotAgent{registry: NewRegistry(), capabilityRegistry: capabilities.NewRegistry()}
	ag.registerTools(context.Background())

	if _, ok := ag.registry.Get("select"); ok {
		t.Fatal("expected stores tool registration to be skipped without stores capability")
	}
	if _, ok := ag.registry.Get("mint_to_space"); ok {
		t.Fatal("expected spaces tool registration to be skipped without spaces capability")
	}
}

type stubCapability struct {
	id      string
	name    string
	context string
}

func (s *stubCapability) ID() string   { return s.id }
func (s *stubCapability) Name() string { return s.name }
func (s *stubCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{EnabledByDefault: true}
}
func (s *stubCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if state.Domain == StoresDomain || state.Domain == s.id {
		return capabilities.ClassificationResult{Relevant: true, Score: 1, Mode: capabilities.CapabilityModePrimary}
	}
	return capabilities.ClassificationResult{}
}
func (s *stubCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return s.context
}
func (s *stubCapability) ExposeTools() []capabilities.ToolDefinition { return nil }
func (s *stubCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return ""
}
func (s *stubCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = ctx
	return nil
}
