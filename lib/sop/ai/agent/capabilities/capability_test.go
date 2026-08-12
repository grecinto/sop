package capabilities

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestRenderCapabilityGuidance_RendersStructuredProfile(t *testing.T) {
	cap := &guidanceCapability{}
	rendered := RenderCapabilityGuidance(cap, "fix a parser bug", RoutingState{})
	if !strings.Contains(rendered, "ROLE") {
		t.Fatalf("expected guidance role heading, got %q", rendered)
	}
	if !strings.Contains(rendered, "Investigate before acting") {
		t.Fatalf("expected structured guidance content, got %q", rendered)
	}
}

func TestProgressiveGuidanceSupport(t *testing.T) {
	minimal := NewGuidanceProfile(GuidanceIntensityMinimal, "Help with simple tasks")
	if minimal.Intensity != GuidanceIntensityMinimal {
		t.Fatalf("expected minimal guidance intensity, got %q", minimal.Intensity)
	}
	if minimal.IsEmpty() {
		t.Fatal("expected minimal guidance profile to be considered non-empty")
	}

	merged := MergeGuidanceProfiles(minimal, GuidanceProfile{Role: "Handle domain-specific workflows"})
	if merged.Role == "" {
		t.Fatal("expected merged guidance profile to preserve role information")
	}
	if merged.Intensity != GuidanceIntensityMinimal {
		t.Fatalf("expected merged intensity to stay minimal, got %q", merged.Intensity)
	}
}

func TestGuidanceProfileApplyDefaultsInheritsBaseGuidance(t *testing.T) {
	defaults := GuidanceProfile{
		Intensity:  GuidanceIntensityLight,
		Role:       "Handle common tasks",
		Principles: []string{"Prefer safety"},
		Guardrails: []GuidanceRule{{Description: "Do not mutate state without confirmation"}},
	}
	override := GuidanceProfile{
		Role:       "Handle domain-specific workflows",
		Principles: []string{"Prefer evidence over assumption"},
	}

	resolved := override.ApplyDefaults(defaults)
	if resolved.Role != "Handle domain-specific workflows" {
		t.Fatalf("expected child role to override the base role, got %q", resolved.Role)
	}
	if resolved.Intensity != GuidanceIntensityLight {
		t.Fatalf("expected inherited intensity to be preserved, got %q", resolved.Intensity)
	}
	if len(resolved.Principles) != 2 {
		t.Fatalf("expected inherited and child principles to be combined, got %d", len(resolved.Principles))
	}
	if len(resolved.Guardrails) != 1 {
		t.Fatalf("expected base guardrails to be preserved, got %d", len(resolved.Guardrails))
	}
}

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

type guidanceCapability struct{}

func (c *guidanceCapability) ID() string               { return "guidance" }
func (c *guidanceCapability) Name() string             { return "Guidance" }
func (c *guidanceCapability) Entitlement() Entitlement { return Entitlement{} }
func (c *guidanceCapability) Classify(query string, state RoutingState) ClassificationResult {
	return ClassificationResult{Relevant: true, Score: 1, Mode: CapabilityModePrimary}
}
func (c *guidanceCapability) BuildContext(query string, state RoutingState) string      { return "" }
func (c *guidanceCapability) ExposeTools() []ToolDefinition                             { return nil }
func (c *guidanceCapability) BuildInstructions(query string, state RoutingState) string { return "" }
func (c *guidanceCapability) Handoff(ctx context.Context, query string, state RoutingState) *Handoff {
	return nil
}
func (c *guidanceCapability) OnBefore(ctx context.Context, query string, state RoutingState) error {
	return nil
}
func (c *guidanceCapability) OnAfter(ctx context.Context, query string, state RoutingState, result CapabilityResult) error {
	return nil
}
func (c *guidanceCapability) BuildGuidanceProfile(query string, state RoutingState) GuidanceProfile {
	return GuidanceProfile{
		Role:       "Investigate before acting",
		Principles: []string{"Prefer evidence over assumption"},
		Guardrails: []GuidanceRule{{Description: "Do not patch without tracing the failing path"}},
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
func (c *testCapability) Handoff(ctx context.Context, query string, state RoutingState) *Handoff {
	return nil
}
func (c *testCapability) OnBefore(ctx context.Context, query string, state RoutingState) error {
	return nil
}
func (c *testCapability) OnAfter(ctx context.Context, query string, state RoutingState, result CapabilityResult) error {
	return nil
}

// Mock prefix capability for testing
type mockPrefixCapability struct {
	id       string
	prefixes []string
	handler  func(context.Context, string, string) (string, error)
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
func (m *mockPrefixCapability) Handoff(ctx context.Context, query string, state RoutingState) *Handoff {
	return nil
}
func (m *mockPrefixCapability) OnBefore(ctx context.Context, query string, state RoutingState) error {
	return nil
}
func (m *mockPrefixCapability) OnAfter(ctx context.Context, query string, state RoutingState, result CapabilityResult) error {
	return nil
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

type stubSTMReader struct {
	thread ThreadReader
}

func (s *stubSTMReader) GetCurrentThread() ThreadReader {
	if s == nil {
		return nil
	}
	return s.thread
}

type stubWorkStreamThread struct {
	ctx                WorkStreamContextReader
	state              WorkStreamState
	pauseReason        string
	manualResume       bool
	pauseWithReasonHit int
	resumeHit          int
}

func (s *stubWorkStreamThread) GetStreamContext() WorkStreamContextReader {
	return s.ctx
}

func (s *stubWorkStreamThread) GetStreamState() WorkStreamState {
	return s.state
}

func (s *stubWorkStreamThread) SetStreamState(state WorkStreamState) {
	s.state = state
}

func (s *stubWorkStreamThread) PauseWorkStreamWithReason(reason string, requireManualResume bool) {
	s.state = WorkStreamPaused
	s.pauseReason = reason
	s.manualResume = requireManualResume
	s.pauseWithReasonHit++
}

func (s *stubWorkStreamThread) ResumeWorkStream() {
	s.state = WorkStreamActive
	s.pauseReason = ""
	s.manualResume = false
	s.resumeHit++
}

func (s *stubWorkStreamThread) GetPauseReason() string {
	return s.pauseReason
}

func (s *stubWorkStreamThread) RequiresManualResume() bool {
	return s.manualResume
}

type stubWorkStreamContext struct {
	currentPhase string
}

func (s *stubWorkStreamContext) GetCurrentPhase() string {
	return s.currentPhase
}

func (s *stubWorkStreamContext) GetPhaseHistory() []string {
	return nil
}

func (s *stubWorkStreamContext) GetPhasePlan() PhasePlanReader {
	return nil
}

func (s *stubWorkStreamContext) GetPhaseArtifacts() map[string]any {
	return nil
}

func (s *stubWorkStreamContext) GetIterationCount() int {
	return 0
}

func (s *stubWorkStreamContext) GetPhaseIterations() map[string]int {
	return nil
}

func TestWorkStreamManagementOnAfter_PausesCurrentStreamForClarification(t *testing.T) {
	thread := &stubWorkStreamThread{
		ctx:   &stubWorkStreamContext{currentPhase: "planning"},
		state: WorkStreamActive,
	}
	cap := NewWorkStreamManagement(func() STMReader {
		return &stubSTMReader{thread: thread}
	}, nil)

	err := cap.OnAfter(context.Background(), "continue", RoutingState{}, CapabilityResult{
		Data: map[string]any{
			"clarification_request": &ai.ClarificationRequest{
				AssistantQuestion: "Which output shape do you want?",
				MissingFact:       "output shape",
				Reason:            "retry_policy_requested_clarification",
			},
		},
	})
	if err != nil {
		t.Fatalf("OnAfter returned error: %v", err)
	}
	if thread.state != WorkStreamPaused {
		t.Fatalf("expected workstream to be paused, got %q", thread.state)
	}
	if thread.pauseWithReasonHit != 1 {
		t.Fatalf("expected pause with reason to be used once, got %d", thread.pauseWithReasonHit)
	}
	if !thread.manualResume {
		t.Fatal("expected clarification pause to require manual resume")
	}
	if !strings.Contains(thread.pauseReason, "output shape") {
		t.Fatalf("expected pause reason to include missing fact, got %q", thread.pauseReason)
	}
}

func TestWorkStreamManagementBuildContext_IncludesPauseReason(t *testing.T) {
	thread := &stubWorkStreamThread{
		ctx:          &stubWorkStreamContext{currentPhase: "planning"},
		state:        WorkStreamPaused,
		pauseReason:  "Waiting for clarification: output shape",
		manualResume: true,
	}
	cap := NewWorkStreamManagement(func() STMReader {
		return &stubSTMReader{thread: thread}
	}, nil)

	result := cap.BuildContext("continue", RoutingState{})
	checks := []string{
		"state: paused",
		"pause_reason: Waiting for clarification: output shape",
		"resume_mode: manual",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Fatalf("expected build context to include %q, got %q", check, result)
		}
	}
}

func TestWorkStreamManagementBuildInstructions_RefinePreservesCurrentWorkStream(t *testing.T) {
	cap := NewWorkStreamManagement(nil, nil)
	result := cap.BuildInstructions("refine the current plan with a new requirement", RoutingState{})
	for _, check := range []string{
		"refine",
		"merge the new requirements into the current plan",
		"preserve the current design",
		"pause_new",
		"clear_restart",
	} {
		if !strings.Contains(strings.ToLower(result), strings.ToLower(check)) {
			t.Fatalf("expected workstream instructions to include %q, got %q", check, result)
		}
	}
}

func TestWorkStreamManagementListWorkStreams_IncludesPauseReason(t *testing.T) {
	thread := &stubWorkStreamThread{
		ctx:          &stubWorkStreamContext{currentPhase: "planning"},
		state:        WorkStreamPaused,
		pauseReason:  "Waiting for clarification: output shape",
		manualResume: true,
	}
	cap := NewWorkStreamManagement(func() STMReader {
		return &stubSTMReader{thread: thread}
	}, nil)

	result, err := cap.listWorkStreams()
	if err != nil {
		t.Fatalf("listWorkStreams returned error: %v", err)
	}
	checks := []string{
		"Current WorkStream (state: paused):",
		"- Pause Reason: Waiting for clarification: output shape",
		"- Resume Mode: manual",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Fatalf("expected workstream list to include %q, got %q", check, result)
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
