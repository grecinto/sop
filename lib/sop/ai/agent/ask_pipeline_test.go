package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

type askRoutingTestGenerator struct {
	Response string
}

func (m *askRoutingTestGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	return ai.GenOutput{Text: m.Response}, nil
}

func (m *askRoutingTestGenerator) Name() string { return "askRoutingTestGenerator" }

func (m *askRoutingTestGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0.0 }

func (m *askRoutingTestGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func containsDomain(domains []string, want string) bool {
	for _, domain := range domains {
		if domain == want {
			return true
		}
	}
	return false
}

// TestClassifyAskTaskContext_Fallback tests that Ask classification has fallback behavior.
func TestClassifyAskTaskContext_Fallback(t *testing.T) {
	agent := &CopilotAgent{}

	// Test fallback classification
	fallback := agent.buildFallbackClassification("List stores")

	if len(fallback.Domains) == 0 {
		t.Fatal("Fallback should return at least one domain")
	}

	if fallback.Phase == "" {
		t.Fatal("Fallback should return a phase")
	}

	if fallback.Intent == "" {
		t.Fatal("Fallback should return an intent")
	}

	t.Logf("✅ Fallback classification: domains=%v, phase=%s, intent=%s",
		fallback.Domains, fallback.Phase, fallback.Intent)
}

// TestAskPhaseFactoryIntegration tests that the Ask pipeline can create phase proxies.
func TestAskPhaseFactoryIntegration(t *testing.T) {
	agent := &CopilotAgent{}

	// Test phase proxy creation
	domains := []string{"stores", "spaces"}
	phase := "investigation"

	phases := agent.createPhaseProxies(domains, phase)

	if len(phases) != 2 {
		t.Fatalf("Expected 2 phases, got %d", len(phases))
	}

	t.Logf("✅ Phase Factory created %d phases for domains %v", len(phases), domains)

	// Test tool aggregation
	classification := &AskTaskContext{
		Domains:   domains,
		Phase:     phase,
		PhasePlan: []string{phase},
		CRUD:      []string{"R"}, // Read-only for test
	}

	allTools := agent.aggregatePhaseTools(phases, classification)

	if len(allTools) == 0 {
		t.Fatal("Should have aggregated tools from phases")
	}

	t.Logf("✅ Aggregated %d tools from phases", len(allTools))
}

func TestGetOrCreateSharedInfra_WiresLocalCommandExecutor(t *testing.T) {
	agent := &CopilotAgent{}
	infra := agent.getOrCreateSharedInfra()
	if infra == nil {
		t.Fatal("expected shared infrastructure")
	}
	if infra.LocalCommandExecutor == nil {
		t.Fatal("expected shared infrastructure to reuse the local command executor")
	}
}

func TestTryAskColdStartRouting_AttachesAskClassification(t *testing.T) {
	ag := &CopilotAgent{}
	taskCtx, err := ag.tryAskColdStartRouting(context.Background(), "List stores", &askRoutingTestGenerator{Response: `{"domains":["stores"],"phase":"investigation","phase_plan":["investigation"],"intent":"read_data","crud":["R"],"confidence":"high","reasoning":"store listing"}`}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskCtx == nil {
		t.Fatal("expected a routing classification")
	}
	if taskCtx.AskClassification == nil {
		t.Fatal("expected Ask classification metadata to be attached to the routing result")
	}
	if !containsDomain(taskCtx.AskClassification.Domains, "stores") {
		t.Fatalf("expected attached classification to preserve domains, got %+v", taskCtx.AskClassification)
	}
}

func TestAsk_PersistsPhasePlanForDiscoveryColdStart(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	ag.service.session.Memory.AddThread(&ConversationThread{ID: sop.NewUUID(), RootPrompt: "Investigate stores"})
	ag.SetGenerator(&askRoutingTestGenerator{Response: `{"domains":["stores"],"phase":"investigation","phase_plan":["investigation","planning","execution"],"intent":"read_data","crud":["R"],"confidence":"high","reasoning":"investigate before execution"}`})

	resp, err := ag.Ask(context.Background(), "Investigate the stores before editing anything", nil)
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	if strings.TrimSpace(resp) == "" {
		t.Fatal("expected Ask to return a response")
	}

	thread := ag.service.session.Memory.GetCurrentThread()
	if thread == nil || thread.StreamContext == nil || thread.StreamContext.PhasePlan == nil {
		t.Fatal("expected Ask to persist a phase plan for discovery cold start")
	}
	if got := thread.StreamContext.PhasePlan.PlannedPhases; len(got) != 3 || got[0] != "investigation" || got[1] != "planning" {
		t.Fatalf("expected persisted multi-phase plan, got %v", got)
	}
}

func TestShouldPauseAskQueue_WhenClarificationStateIsPending(t *testing.T) {
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		Variables: make(map[string]any),
		ClarificationState: &ai.ClarificationState{
			Status: "pending",
		},
	})
	if !shouldPauseAskQueue(ctx) {
		t.Fatal("expected pending clarification state to pause ask queue processing")
	}
}

func TestShouldPauseAskQueue_WhenClarificationStateIsResolved(t *testing.T) {
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		Variables: make(map[string]any),
		ClarificationState: &ai.ClarificationState{
			Status: "resolved",
		},
	})
	if shouldPauseAskQueue(ctx) {
		t.Fatal("did not expect resolved clarification state to pause ask queue processing")
	}
}

func TestAskCore_AsksForRepoContextBeforeCodingWorkflowClassification(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.SetGenerator(&askRoutingTestGenerator{Response: `{"domains":["coding_workflow"],"phase":"execution","phase_plan":["investigation","execution","review"],"intent":"generate_code","confidence":"high","reasoning":"fixing code"}`})

	resp, err := ag.askCore(context.Background(), "fix failing tests", nil)
	if err != nil {
		t.Fatalf("askCore failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp), "repository or working directory") {
		t.Fatalf("expected vague repo-fix ask to request repository or working directory before coding workflow routing, got: %s", resp)
	}
}

func TestAskCore_RejectsLocalOnlyWorkflowWhenDisabled(t *testing.T) {
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.SetGenerator(&askRoutingTestGenerator{Response: `{"domains":["coding_workflow"],"phase":"execution","phase_plan":["investigation","execution","review"],"intent":"generate_code","confidence":"high","reasoning":"fixing code"}`})

	resp, err := ag.askCore(context.Background(), "fix failing tests", nil)
	if err != nil {
		t.Fatalf("askCore failed: %v", err)
	}
	if !strings.Contains(resp, "Desktop automation and/or coding workflow is not available in this setup") {
		t.Fatalf("expected unsupported local-only capability message, got: %s", resp)
	}
}

func TestAskTaskContext_NormalizesRepoFixPathQueriesToCodingWorkflowOnly(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := ag.normalizeAskTaskContext(AskTaskContext{
		Domains:   []string{"coding_workflow", "desktop"},
		Phase:     "execution",
		PhasePlan: []string{"investigation", "execution", "review"},
		Intent:    "generate_code",
		CRUD:      []string{"R", "U"},
		Confidence: "high",
		Reasoning:  "The user wants failing tests fixed in a local code folder, which requires inspecting the code/tests and then modifying existing files.",
		OriginalQuery: "The current folder ../lib/sop, fix failing tests under this folder.",
	})
	if len(ctx.Domains) != 1 || ctx.Domains[0] != "coding_workflow" {
		t.Fatalf("expected repo-fix folder query to normalize to coding_workflow only, got %v", ctx.Domains)
	}
}

func TestBuildAskClassificationPrompt_UsesActiveCognitiveStateForContinuity(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any), CognitiveState: &ai.SessionCognitiveState{
		CurrentQuery: "fix failing tests",
		CurrentPhase: "execution",
		LastAssessment: &ai.SessionCognitiveAssessment{
			SuggestedPhase: "execution",
			Rationale:      "Keep the repo-fix task in coding_workflow while the user scopes it to a local folder.",
		},
	}}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	prompt := ag.buildAskClassificationPrompt(ctx, "The current folder ../lib/sop, fix failing tests under this folder.", nil, nil)
	if !strings.Contains(prompt, "coding_workflow") || !strings.Contains(prompt, "preserve the active coding_workflow lead") {
		t.Fatalf("expected active cognitive state to steer classification continuity, got prompt: %s", prompt)
	}
}

func TestAskCore_QueuedPhaseActorUsesPersistedAskClassification(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.registry = NewRegistry()
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}

	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Investigate stores"}
	thread.InitializeWorkStream("investigation")
	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   []string{"investigation", "planning", "execution"},
		CurrentIndex:    1,
		CompletedPhases: []string{"investigation"},
		RemainingPhases: []string{"planning", "execution"},
		Checkpoints:     map[string]bool{"after_investigation": true},
		Reasoning:       "needs planning after investigation",
		Confidence:      "high",
	}
	ag.service.session.Memory.AddThread(thread)
	ag.service.session.Memory.SetRoutingState(&TaskContextClassification{
		Domain: "stores",
		AskClassification: &AskTaskContext{
			Domains:    []string{"stores"},
			Phase:      "investigation",
			PhasePlan:  []string{"investigation", "planning", "execution"},
			Intent:     "read_data",
			CRUD:       []string{"R"},
			Confidence: "high",
		},
	})
	ag.SetGenerator(&askRoutingTestGenerator{Response: `{"domains":["general"],"phase":"investigation","phase_plan":["investigation"],"intent":"analyze","confidence":"low","reasoning":"fallback"}`})

	ctx := context.WithValue(context.Background(), ctxKeyAskWorkItemActor, "phase:planning")
	resp, err := ag.askCore(ctx, "continue the existing work", nil)
	if err != nil {
		t.Fatalf("askCore failed: %v", err)
	}
	if strings.TrimSpace(resp) == "" {
		t.Fatal("expected askCore to return a response")
	}
	if _, ok := ag.registry.Get("planning_design_query"); !ok {
		t.Fatal("expected queued planning phase to register stores planning tools from persisted Ask classification")
	}
	if _, ok := ag.registry.Get("investigation_list_stores"); ok {
		t.Fatal("did not expect investigation-phase stores tools when queued actor targets planning")
	}
}
