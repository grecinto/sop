package capabilities

import "testing"

func TestRegistryResolvesCapabilityByID(t *testing.T) {
	reg := NewRegistry()
	cap := &testCapability{id: "stores", name: "Stores"}
	reg.Register(cap)

	resolved, ok := reg.Resolve("show stores", RoutingState{Domain: "Stores"})
	if !ok {
		t.Fatal("expected capability to resolve")
	}
	if resolved.ID() != "stores" {
		t.Fatalf("expected stores capability, got %s", resolved.ID())
	}
}

func TestRegistryComposeReturnsEnabledCapabilitiesInOrder(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&testCapability{id: "stores", name: "Stores"})
	reg.Register(&testCapability{id: "spaces", name: "Spaces"})
	reg.Register(&testCapability{id: "web", name: "Web", entitlement: Entitlement{Premium: true, EnabledByDefault: false}})

	composed := reg.Compose("search", RoutingState{Domain: "Stores"}, nil)
	if len(composed) != 1 {
		t.Fatalf("expected one default capability match, got %d", len(composed))
	}
	if composed[0].ID() != "stores" {
		t.Fatalf("expected stores capability first, got %s", composed[0].ID())
	}
}

func TestCapabilityRunUsesExplicitInputs(t *testing.T) {
	cap := &testCapability{id: "stores", name: "Stores"}
	result, err := cap.Run("show inventory", RoutingState{Domain: "Stores"})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if result.Summary != "ok" {
		t.Fatalf("expected summary ok, got %q", result.Summary)
	}
}

func TestNewRegistryRegistersCapabilitiesAtConstruction(t *testing.T) {
	reg := NewRegistry(&testCapability{id: "spaces", name: "Spaces"})
	cap, ok := reg.Resolve("", RoutingState{Domain: "Spaces"})
	if !ok {
		t.Fatal("expected capability to be registered via constructor")
	}
	if cap.ID() != "spaces" {
		t.Fatalf("expected spaces capability, got %q", cap.ID())
	}
}

type testCapability struct {
	id           string
	name         string
	entitlement  Entitlement
	canHandleFn  func(query string, state RoutingState) bool
	buildContext func(query string, state RoutingState) string
}

func (c *testCapability) ID() string               { return c.id }
func (c *testCapability) Name() string             { return c.name }
func (c *testCapability) Entitlement() Entitlement { return c.entitlement }
func (c *testCapability) CanHandle(query string, state RoutingState) bool {
	if c.canHandleFn != nil {
		return c.canHandleFn(query, state)
	}
	return state.Domain == c.name || state.Domain == c.id
}
func (c *testCapability) BuildContext(query string, state RoutingState) string {
	if c.buildContext != nil {
		return c.buildContext(query, state)
	}
	return ""
}
func (c *testCapability) ExposeTools() []ToolDefinition                             { return nil }
func (c *testCapability) BuildInstructions(query string, state RoutingState) string { return "" }
func (c *testCapability) Run(query string, state RoutingState) (CapabilityResult, error) {
	return CapabilityResult{Summary: "ok"}, nil
}
