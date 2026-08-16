package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/domains/desktop"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
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
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	if ag.capabilityRegistry == nil {
		t.Fatal("expected capability registry to be initialized")
	}

	cap, ok := ag.capabilityRegistry.Resolve("", capabilities.RoutingState{Domain: "Desktop"})
	if !ok {
		t.Fatal("expected desktop capability to be registered when local automation is enabled")
	}
	if cap.ID() != desktop.CapabilityID {
		t.Fatalf("expected desktop capability id %q, got %q", desktop.CapabilityID, cap.ID())
	}
}

func TestNewCopilotAgentRegistersIDEWorkerCapability(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	var found bool
	for _, cap := range ag.capabilityRegistry.ListAll() {
		if cap != nil && cap.ID() == ideworker.CapabilityID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected IDE worker capability to be registered")
	}
}

func TestResolvePlanningContextForDomain_Coding(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	ag := NewCopilotAgent(Config{}, nil, nil)
	contextText := ag.resolvePlanningContextForDomain("fix failing tests in parser", "coding_workflow")
	if !strings.Contains(contextText, "Coding Planning Context") {
		t.Fatalf("expected coding planning context, got %q", contextText)
	}
}

func TestResolvePlanningContextForDomain_Desktop(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	ag := NewCopilotAgent(Config{}, nil, nil)
	contextText := ag.resolvePlanningContextForDomain("run git status", "desktop")
	if !strings.Contains(contextText, "Desktop Planning Context") {
		t.Fatalf("expected desktop planning context, got %q", contextText)
	}
}

func TestResolvePlanningContextForDomain_Stores(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	contextText := ag.resolvePlanningContextForDomain("fix store validation issue", "stores")
	if !strings.Contains(contextText, "Stores Planning Context") {
		t.Fatalf("expected stores planning context, got %q", contextText)
	}
}

func TestResolvePlanningContextForDomain_Spaces(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	contextText := ag.resolvePlanningContextForDomain("fix knowledge retrieval issue", "spaces")
	if !strings.Contains(contextText, "Spaces Planning Context") {
		t.Fatalf("expected spaces planning context, got %q", contextText)
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
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	var foundPhaseCapability bool
	for _, cap := range ag.capabilityRegistry.ListAll() {
		if cap != nil && strings.HasPrefix(cap.ID(), "phase_") {
			foundPhaseCapability = true
			break
		}
	}
	if !foundPhaseCapability {
		t.Fatal("expected local-only coding workflow phase capabilities to be registered when local automation is enabled")
	}
}

func TestNewCopilotAgentRegistersWorkStreamManagementCapability(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	var found bool
	for _, cap := range ag.capabilityRegistry.ListAll() {
		if cap != nil && cap.ID() == "workstream_management" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected WorkStreamManagement capability to be registered when local automation is enabled")
	}
}

func TestRegisterTools_ScopesCapabilityToolRegistrationForCodingWorkflowRouting(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{CurrentDB: "system", Variables: map[string]any{}}
	payload.Variables["RoutingState"] = &TaskContextClassification{
		Domain:         "coding_workflow",
		RoutingGate:    RoutingGateDiscovery,
		CapabilityHint: "execution",
		AskClassification: &AskTaskContext{
			Domains:       []string{"coding_workflow"},
			Phase:         "execution",
			PhasePlan:     []string{"investigation", "execution"},
			Intent:        "generate_code",
			CRUD:          []string{"R", "U"},
			OriginalQuery: "fix failing tests",
		},
	}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	ag.registerTools(ctx)

	found := map[string]bool{}
	for _, tool := range ag.registry.List() {
		found[tool.Name] = true
	}

	if found["create_phase_plan"] {
		t.Fatalf("did not expect planner tool to be globally registered for scoped coding workflow routing")
	}
	if found["decompose_multi_step_plan"] {
		t.Fatalf("did not expect multi-step planner tool to be globally registered for scoped coding workflow routing")
	}
	if found["investigation_capture"] || found["planning_capture"] || found["execution_capture"] {
		t.Fatalf("did not expect phase capability tools to be globally registered; phase factory should own them for the turn")
	}
	if !found["delegate_continue_task"] {
		t.Fatalf("expected scoped supporting capability tool delegate_continue_task to remain available")
	}
	if !found["list_workstreams"] {
		t.Fatalf("expected workstream management tool to remain available")
	}
}

func TestSessionSTMReaderAdapterPreservesWorkStreamState(t *testing.T) {
	stm := NewShortTermMemory()
	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "resume planning work",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")
	stm.AddThread(thread)

	reader := &sessionSTMReaderAdapter{stm: stm}
	current := reader.GetCurrentThread()
	if current == nil {
		t.Fatal("expected current thread adapter")
	}

	current.SetStreamState(capabilities.WorkStreamPaused)
	if thread.StreamState != WorkStreamPaused {
		t.Fatalf("expected thread state to update through adapter, got %q", thread.StreamState)
	}
	if got := current.GetStreamState(); got != capabilities.WorkStreamPaused {
		t.Fatalf("expected adapter to report paused state, got %q", got)
	}

	pauser, ok := current.(interface {
		PauseWorkStreamWithReason(string, bool)
	})
	if !ok {
		t.Fatal("expected thread adapter to support pause with reason")
	}
	pauser.PauseWorkStreamWithReason("Waiting for clarification: output shape", true)
	if thread.StreamContext == nil || thread.StreamContext.PauseReason != "Waiting for clarification: output shape" {
		t.Fatalf("expected pause reason to persist on thread, got %+v", thread.StreamContext)
	}
	if !thread.StreamContext.RequiresManualResume {
		t.Fatal("expected manual resume flag to persist on thread")
	}

	metadataReader, ok := current.(interface {
		GetPauseReason() string
		RequiresManualResume() bool
	})
	if !ok {
		t.Fatal("expected thread adapter to expose pause metadata")
	}
	if got := metadataReader.GetPauseReason(); got != "Waiting for clarification: output shape" {
		t.Fatalf("expected adapter pause reason, got %q", got)
	}
	if !metadataReader.RequiresManualResume() {
		t.Fatal("expected adapter manual resume flag to be true")
	}
}

// TestNewCopilotAgentRegistersServiceCapabilityByDefault removed (2026-07-30)
// ServiceCapability deprecated - phase capabilities provide all functionality
func TestNewCopilotAgentRegistersServiceCapabilityByDefault(t *testing.T) {
	t.Skip("ServiceCapability removed - phase capabilities provide all needed functionality")
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
func (s *stubCapability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	return nil
}
func (s *stubCapability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	return nil
}
