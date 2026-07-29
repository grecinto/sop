package capabilities

import (
	"context"
	"strings"
	"testing"
)

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

func TestRegistryClassifySupportsAdditiveCapabilityComposition(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&testCapability{
		id:   "stores",
		name: "Stores",
		classifyFn: func(query string, state RoutingState) ClassificationResult {
			if strings.Contains(strings.ToLower(query), "inventory") {
				return ClassificationResult{Relevant: true, Score: 10, Mode: CapabilityModePrimary}
			}
			return ClassificationResult{}
		},
	})
	reg.Register(&testCapability{
		id:   "spaces",
		name: "Spaces",
		classifyFn: func(query string, state RoutingState) ClassificationResult {
			if strings.Contains(strings.ToLower(query), "inventory") {
				return ClassificationResult{Relevant: true, Score: 8, Mode: CapabilityModeSupporting}
			}
			return ClassificationResult{}
		},
	})

	classified := reg.Classify("show inventory", RoutingState{Domain: "Stores"})
	if len(classified) != 2 {
		t.Fatalf("expected two classified capabilities, got %d", len(classified))
	}
	if classified[0].Capability.ID() != "stores" {
		t.Fatalf("expected primary capability stores first, got %s", classified[0].Capability.ID())
	}
	if classified[1].Capability.ID() != "spaces" {
		t.Fatalf("expected supporting capability spaces second, got %s", classified[1].Capability.ID())
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
	classifyFn   func(query string, state RoutingState) ClassificationResult
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
func (c *testCapability) Classify(query string, state RoutingState) ClassificationResult {
	if c.classifyFn != nil {
		return c.classifyFn(query, state)
	}
	if c.canHandleFn != nil && c.canHandleFn(query, state) {
		return ClassificationResult{Relevant: true, Score: 1, Mode: CapabilityModePrimary}
	}
	if state.Domain == c.name || state.Domain == c.id {
		return ClassificationResult{Relevant: true, Score: 1, Mode: CapabilityModePrimary}
	}
	return ClassificationResult{}
}
func (c *testCapability) BuildContext(query string, state RoutingState) string {
	if c.buildContext != nil {
		return c.buildContext(query, state)
	}
	return ""
}
func (c *testCapability) ExposeTools() []ToolDefinition                             { return nil }
func (c *testCapability) BuildInstructions(query string, state RoutingState) string { return "" }

// Mock prefix capability for testing
type mockPrefixCapability struct {
	id       string
	prefixes []string
	handler  func(context.Context, string, string) (string, error)
}

type participatingCapability struct {
	id      string
	summary string
}

func (p *participatingCapability) ID() string   { return p.id }
func (p *participatingCapability) Name() string { return "participating" }
func (p *participatingCapability) Entitlement() Entitlement {
	return Entitlement{EnabledByDefault: true, Tier: "test"}
}
func (p *participatingCapability) Classify(query string, state RoutingState) ClassificationResult {
	if state.Domain == "Desktop" {
		return ClassificationResult{Relevant: true, Score: 1, Mode: CapabilityModePrimary}
	}
	return ClassificationResult{}
}
func (p *participatingCapability) BuildContext(query string, state RoutingState) string { return "" }
func (p *participatingCapability) ExposeTools() []ToolDefinition                        { return nil }
func (p *participatingCapability) BuildInstructions(query string, state RoutingState) string {
	return ""
}
func (p *participatingCapability) Participate(ctx context.Context, query string, state RoutingState) (ParticipationResult, bool, error) {
	return ParticipationResult{Summary: p.summary}, true, nil
}

func (m *mockPrefixCapability) ID() string   { return m.id }
func (m *mockPrefixCapability) Name() string { return "Mock " + m.id }
func (m *mockPrefixCapability) Entitlement() Entitlement {
	return Entitlement{EnabledByDefault: true, Tier: "test"}
}
func (m *mockPrefixCapability) Classify(query string, state RoutingState) ClassificationResult {
	return ClassificationResult{}
}
func (m *mockPrefixCapability) BuildContext(query string, state RoutingState) string {
	return ""
}
func (m *mockPrefixCapability) ExposeTools() []ToolDefinition { return nil }
func (m *mockPrefixCapability) BuildInstructions(query string, state RoutingState) string {
	return ""
}
func (m *mockPrefixCapability) GetHandledPrefixes() []string { return m.prefixes }
func (m *mockPrefixCapability) HandlePrefix(ctx context.Context, query string, normalized string) (string, error) {
	if m.handler != nil {
		return m.handler(ctx, query, normalized)
	}
	return "executed: " + normalized, nil
}

func TestRegistry_PrefixConflictDetection(t *testing.T) {
	reg := NewRegistry()

	cap1 := &mockPrefixCapability{id: "capability1", prefixes: []string{"/test", "/demo"}}
	cap2 := &mockPrefixCapability{id: "capability2", prefixes: []string{"/test", "/other"}}

	// Register first capability
	reg.Register(cap1)
	if len(reg.prefixHandlers) != 2 {
		t.Fatalf("Expected 2 prefixes registered, got %d", len(reg.prefixHandlers))
	}

	// Register second capability with conflicting prefix - should log warning
	reg.Register(cap2)
	if len(reg.prefixHandlers) != 3 {
		t.Fatalf("Expected 3 prefixes registered (1 overwritten), got %d", len(reg.prefixHandlers))
	}

	// Verify /test is now handled by cap2 (last registered wins)
	if reg.prefixHandlers["/test"].ID() != "capability2" {
		t.Errorf("Expected /test to be handled by capability2, got %s", reg.prefixHandlers["/test"].ID())
	}
}

func TestRegistry_EmptyPrefixRejection(t *testing.T) {
	reg := NewRegistry()

	cap := &mockPrefixCapability{id: "bad_capability", prefixes: []string{"", "/valid"}}
	reg.Register(cap)

	if len(reg.prefixHandlers) != 1 {
		t.Fatalf("Expected 1 prefix registered (empty prefix rejected), got %d", len(reg.prefixHandlers))
	}
	if _, exists := reg.prefixHandlers["/valid"]; !exists {
		t.Error("Valid prefix /valid should be registered")
	}
	if _, exists := reg.prefixHandlers[""]; exists {
		t.Error("Empty prefix should not be registered")
	}
}

func TestTryPrefixRouting_EmptyQuery(t *testing.T) {
	reg := NewRegistry()
	cap := &mockPrefixCapability{id: "test", prefixes: []string{"/cmd"}}
	reg.Register(cap)

	handled, result, err := reg.TryPrefixRouting(context.Background(), "")
	if handled {
		t.Error("Empty query should not be handled")
	}
	if result != "" {
		t.Errorf("Empty query should return empty result, got %q", result)
	}
	if err != nil {
		t.Errorf("Empty query should not return error, got %v", err)
	}
}

func TestRegistryTryParticipateUsesResolvedCapability(t *testing.T) {
	reg := NewRegistry()
	unrelated := &participatingCapability{id: "stores", summary: "stores participation"}
	selected := &participatingCapability{id: "desktop", summary: "desktop participation"}
	reg.Register(unrelated)
	reg.Register(selected)

	result, handled, err := reg.TryParticipateFor(context.Background(), "pwd", RoutingState{Domain: "Desktop"}, selected)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handled {
		t.Fatal("expected participation hook to handle the query")
	}
	if result.Summary != "desktop participation" {
		t.Fatalf("expected participation summary %q, got %q", "desktop participation", result.Summary)
	}
}

func TestRegistryTryParticipateUsesParticipationHook(t *testing.T) {
	reg := NewRegistry()
	cap := &participatingCapability{id: "desktop", summary: "shell output"}
	reg.Register(cap)

	result, handled, err := reg.TryParticipate(context.Background(), "pwd", RoutingState{Domain: "Desktop"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handled {
		t.Fatal("expected participation hook to handle the query")
	}
	if result.Summary != "shell output" {
		t.Fatalf("expected participation summary %q, got %q", "shell output", result.Summary)
	}
}

func TestTryPrefixRouting_EmptyCommand(t *testing.T) {
	reg := NewRegistry()
	cap := &mockPrefixCapability{id: "test", prefixes: []string{"/cmd"}}
	reg.Register(cap)

	// Test various forms of empty commands
	testCases := []string{
		"/cmd",
		"/cmd ",
		"/cmd  ",
		"/cmd\t",
		"/cmd:",
		"/cmd: ",
	}

	for _, tc := range testCases {
		handled, result, err := reg.TryPrefixRouting(context.Background(), tc)
		if !handled {
			t.Errorf("Query %q should be handled (to return error)", tc)
		}
		if err == nil {
			t.Errorf("Query %q should return error for empty command, got result: %q", tc, result)
		}
		if err != nil && !strings.Contains(err.Error(), "empty command") {
			t.Errorf("Query %q error should mention 'empty command', got: %v", tc, err)
		}
	}
}

func TestTryPrefixRouting_LongestPrefixFirst(t *testing.T) {
	reg := NewRegistry()

	executedBy := ""
	cap1 := &mockPrefixCapability{
		id:       "short",
		prefixes: []string{"/c"},
		handler: func(ctx context.Context, query, normalized string) (string, error) {
			executedBy = "short"
			return "short: " + normalized, nil
		},
	}
	cap2 := &mockPrefixCapability{
		id:       "long",
		prefixes: []string{"/cmd"},
		handler: func(ctx context.Context, query, normalized string) (string, error) {
			executedBy = "long"
			return "long: " + normalized, nil
		},
	}

	reg.Register(cap1)
	reg.Register(cap2)

	// /cmd should match longest prefix (/cmd, not /c)
	handled, result, err := reg.TryPrefixRouting(context.Background(), "/cmd ls")
	if err != nil {
		t.Fatalf("TryPrefixRouting error: %v", err)
	}
	if !handled {
		t.Fatal("Expected /cmd to be handled")
	}
	if executedBy != "long" {
		t.Errorf("Expected /cmd to be handled by 'long' capability, got %s", executedBy)
	}
	if result != "long: ls" {
		t.Errorf("Expected result 'long: ls', got %q", result)
	}

	// /c should match short prefix
	executedBy = ""
	handled, result, err = reg.TryPrefixRouting(context.Background(), "/c pwd")
	if err != nil {
		t.Fatalf("TryPrefixRouting error: %v", err)
	}
	if !handled {
		t.Fatal("Expected /c to be handled")
	}
	if executedBy != "short" {
		t.Errorf("Expected /c to be handled by 'short' capability, got %s", executedBy)
	}
	if result != "short: pwd" {
		t.Errorf("Expected result 'short: pwd', got %q", result)
	}
}

func TestTryPrefixRouting_SpaceAndColonSeparators(t *testing.T) {
	reg := NewRegistry()
	cap := &mockPrefixCapability{id: "test", prefixes: []string{"/cmd"}}
	reg.Register(cap)

	testCases := []struct {
		query    string
		expected string
	}{
		{"/cmd echo test", "executed: echo test"},
		{"/cmd:echo test", "executed: echo test"},
		{"/cmd: echo test", "executed: echo test"},
		{"/cmd  echo test", "executed: echo test"}, // Multiple spaces
	}

	for _, tc := range testCases {
		handled, result, err := reg.TryPrefixRouting(context.Background(), tc.query)
		if err != nil {
			t.Errorf("Query %q returned error: %v", tc.query, err)
			continue
		}
		if !handled {
			t.Errorf("Query %q should be handled", tc.query)
			continue
		}
		if result != tc.expected {
			t.Errorf("Query %q: expected %q, got %q", tc.query, tc.expected, result)
		}
	}
}

func TestTryPrefixRouting_NilRegistry(t *testing.T) {
	var reg *Registry = nil
	handled, result, err := reg.TryPrefixRouting(context.Background(), "/cmd ls")
	if handled {
		t.Error("Nil registry should not handle anything")
	}
	if result != "" {
		t.Errorf("Nil registry should return empty result, got %q", result)
	}
	if err != nil {
		t.Errorf("Nil registry should not return error, got %v", err)
	}
}

func TestTryPrefixRouting_HandlerError(t *testing.T) {
	reg := NewRegistry()
	cap := &mockPrefixCapability{
		id:       "error_test",
		prefixes: []string{"/fail"},
		handler: func(ctx context.Context, query, normalized string) (string, error) {
			return "", context.Canceled
		},
	}
	reg.Register(cap)

	handled, result, err := reg.TryPrefixRouting(context.Background(), "/fail command")
	if !handled {
		t.Error("Query should be handled even if handler returns error")
	}
	if err == nil {
		t.Error("Expected error from handler to be propagated")
	}
	if result != "" {
		t.Errorf("Expected empty result on error, got %q", result)
	}
}

func TestTryPrefixRouting_NoPrefixBoundary(t *testing.T) {
	reg := NewRegistry()
	cap := &mockPrefixCapability{id: "test", prefixes: []string{"/cmd"}}
	reg.Register(cap)

	// These should NOT match because there's no space or colon separator
	testCases := []string{
		"/cmdline",     // No separator
		"/commandline", // No separator
		"/cmdfoobar",   // No separator
	}

	for _, tc := range testCases {
		handled, _, _ := reg.TryPrefixRouting(context.Background(), tc)
		if handled {
			t.Errorf("Query %q should NOT be handled (no prefix boundary)", tc)
		}
	}
}
