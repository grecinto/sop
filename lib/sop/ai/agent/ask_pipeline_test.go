package agent

import (
	"context"
	"os"
	"path/filepath"
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

type askRoutingClarificationGenerator struct {
	calls int
}

func (m *askRoutingClarificationGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.calls++
	switch m.calls {
	case 1:
		return ai.GenOutput{Text: `{"domains":["coding_workflow"],"phase":"execution","phase_plan":["investigation","execution","review"],"intent":"generate_code","confidence":"high","reasoning":"fixing code"}`}, nil
	default:
		return ai.GenOutput{ToolCalls: []ai.ToolCall{{
			Name: compatibleClarificationToolName,
			Args: map[string]any{
				"question":     "Which repository or working directory should I inspect for the failing tests?",
				"missing_fact": "repository or working directory",
				"reason":       "I need the repo/workdir before I can run tests or inspect code.",
				"target_query": "fix failing tests",
			},
		}}}, nil
	}
}

func (m *askRoutingClarificationGenerator) Name() string { return "askRoutingClarificationGenerator" }

func (m *askRoutingClarificationGenerator) EstimateCost(inTokens, outTokens int) float64 { return 0.0 }

func (m *askRoutingClarificationGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

type askRoutingClarificationResumeGenerator struct {
	calls      int
	workingDir string
}

func (m *askRoutingClarificationResumeGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.calls++
	switch m.calls {
	case 1, 3:
		return ai.GenOutput{Text: `{"domains":["coding_workflow"],"phase":"investigation","phase_plan":["investigation","execution","review"],"intent":"generate_code","crud":["R","U"],"confidence":"high","reasoning":"repo fix workflow"}`}, nil
	case 2:
		return ai.GenOutput{ToolCalls: []ai.ToolCall{{
			Name: compatibleClarificationToolName,
			Args: map[string]any{
				"question":     "Which repository or working directory should I inspect for the failing tests?",
				"missing_fact": "repository or working directory",
				"reason":       "I need the repo/workdir before I can run tests or inspect code.",
				"target_query": "fix failing tests",
			},
		}}}, nil
	case 4:
		return ai.GenOutput{ToolCalls: []ai.ToolCall{{
			Name: "execution_run_tests",
			Args: map[string]any{
				"query":       "fix failing tests",
				"working_dir": m.workingDir,
			},
		}}}, nil
	default:
		return ai.GenOutput{Text: "I ran the tests in the current folder and used that result to continue the repo fix workflow."}, nil
	}
}

func (m *askRoutingClarificationResumeGenerator) Name() string {
	return "askRoutingClarificationResumeGenerator"
}

func (m *askRoutingClarificationResumeGenerator) EstimateCost(inTokens, outTokens int) float64 {
	return 0.0
}

func (m *askRoutingClarificationResumeGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
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

	phases := agent.createPhaseProxies(domains, phase, nil)

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

func TestCreatePhaseProxies_CodingWorkflowImplementationBundleIncludesExecutionTools(t *testing.T) {
	agent := &CopilotAgent{}
	classification := &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "investigation",
		PhasePlan:     []string{"investigation", "execution", "review"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		OriginalQuery: "fix failing tests in the current repository",
	}

	phases := agent.createPhaseProxies([]string{"coding_workflow"}, "investigation", classification)
	tools := agent.aggregatePhaseTools(phases, classification)
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	if !toolNames["investigation_inspect_repo"] {
		t.Fatalf("expected implementation bundle to keep investigation tools, got %v", toolNames)
	}
	if !toolNames["execution_run_tests"] {
		t.Fatalf("expected implementation bundle to expose execution_run_tests, got %v", toolNames)
	}
	if !toolNames["execution_apply_patch"] {
		t.Fatalf("expected implementation bundle to expose execution_apply_patch, got %v", toolNames)
	}
	if toolNames["planning_assess_and_route"] {
		t.Fatalf("did not expect implementation bundle to expose planning_assess_and_route, got %v", toolNames)
	}
	if toolNames["planning_create_succession_path"] {
		t.Fatalf("did not expect implementation bundle to expose planning_create_succession_path, got %v", toolNames)
	}
	if toolNames["review_build_context"] {
		t.Fatalf("did not expect implementation bundle to expose review_build_context, got %v", toolNames)
	}
	if toolNames["delivery_build_context"] {
		t.Fatalf("did not expect implementation bundle to expose delivery_build_context, got %v", toolNames)
	}
}

func TestCreatePhaseProxies_CodingWorkflowDiscoveryBundleAvoidsMutationTools(t *testing.T) {
	agent := &CopilotAgent{}
	classification := &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "investigation",
		PhasePlan:     []string{"investigation", "planning"},
		Intent:        "analyze",
		CRUD:          []string{"R"},
		OriginalQuery: "investigate why the parser behaves this way",
	}

	phases := agent.createPhaseProxies([]string{"coding_workflow"}, "investigation", classification)
	tools := agent.aggregatePhaseTools(phases, classification)
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	if !toolNames["investigation_search_repo"] {
		t.Fatalf("expected discovery bundle to expose investigation tools, got %v", toolNames)
	}
	if !toolNames["planning_plan_changes"] {
		t.Fatalf("expected discovery bundle to expose planning tools, got %v", toolNames)
	}
	if toolNames["planning_assess_and_route"] {
		t.Fatalf("did not expect discovery bundle to expose planning_assess_and_route, got %v", toolNames)
	}
	if toolNames["planning_create_succession_path"] {
		t.Fatalf("did not expect discovery bundle to expose planning_create_succession_path, got %v", toolNames)
	}
	if toolNames["execution_apply_patch"] {
		t.Fatalf("did not expect discovery bundle to expose execution_apply_patch, got %v", toolNames)
	}
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

func TestNormalizeCodingRuntimePlanForClassification_CollapsesLegacyCodingPhases(t *testing.T) {
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}}
	plan := normalizeCodingRuntimePlanForClassification(classification, []string{"intake", "investigation", "planning", "testing", "execution", "review", "delivery"})
	joined := strings.Join(plan, ",")
	if joined != "investigation,planning,execution" {
		t.Fatalf("expected legacy coding plan to collapse to investigation,planning,execution, got %v", plan)
	}
}

func TestResolvePhasePlan_NormalizesLegacyCodingWorkStreamPlanOnResume(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	thread.InitializeWorkStream("investigation")
	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   []string{"intake", "investigation", "planning", "testing", "execution", "review", "delivery"},
		CurrentIndex:    5,
		CompletedPhases: []string{"intake", "investigation", "planning", "testing", "execution"},
		RemainingPhases: []string{"review", "delivery"},
		Checkpoints:     map[string]bool{"after_investigation": true, "after_execution": true},
	}
	ag.service.session.Memory.AddThread(thread)

	plan, currentPhase := ag.resolvePhasePlan(context.Background(), &AskTaskContext{
		Domains:   []string{"coding_workflow"},
		Phase:     "investigation",
		PhasePlan: []string{"investigation", "planning", "execution"},
	})
	if strings.Join(plan, ",") != "investigation,planning,execution" {
		t.Fatalf("expected resumed coding plan to normalize to investigation,planning,execution, got %v", plan)
	}
	if currentPhase != "execution" {
		t.Fatalf("expected legacy review phase to normalize to execution, got %q", currentPhase)
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

func TestAskCore_UsesReactClarificationForVagueRepoTask(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.SetGenerator(&askRoutingClarificationGenerator{})
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	resp, err := ag.askCore(ctx, "fix failing tests", nil)
	if err != nil {
		t.Fatalf("askCore failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp), "repository or working directory") {
		t.Fatalf("expected ReAct clarification to request repository or working directory, got: %s", resp)
	}
	if payload.ClarificationState == nil {
		t.Fatal("expected clarification state to be recorded from the ReAct clarification")
	}
	if payload.ClarificationState.TargetQuery != "fix failing tests" || payload.ClarificationState.Status != "pending" {
		t.Fatalf("unexpected clarification state after ReAct clarification: %+v", payload.ClarificationState)
	}
	if payload.ClarificationState.Source != compatibleClarificationToolName {
		t.Fatalf("expected clarification source %q, got %+v", compatibleClarificationToolName, payload.ClarificationState)
	}
}

func TestAskCore_ResumesClarificationAnswerAndRunsTestsInSameWorkflow(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)

	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module example.com/resume\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "resume_test.go"), []byte("package resume\n\nimport \"testing\"\n\nfunc TestResume(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatalf("write resume_test.go: %v", err)
	}

	ag := NewCopilotAgent(Config{}, nil, nil)
	gen := &askRoutingClarificationResumeGenerator{workingDir: tempDir}
	ag.SetGenerator(gen)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	firstResp, err := ag.askCore(ctx, "fix failing tests", nil)
	if err != nil {
		t.Fatalf("first askCore failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(firstResp), "repository or working directory") {
		t.Fatalf("expected first turn to ask for repo/workdir, got: %s", firstResp)
	}
	if payload.ClarificationState == nil || payload.ClarificationState.Status != "pending" {
		t.Fatalf("expected pending clarification after first turn, got %+v", payload.ClarificationState)
	}

	secondResp, err := ag.askCore(ctx, "this current folder", nil)
	if err != nil {
		t.Fatalf("second askCore failed: %v", err)
	}
	if !strings.Contains(secondResp, "I ran the tests in the current folder") {
		t.Fatalf("expected resumed turn to continue after running tests, got: %s", secondResp)
	}
	if payload.ClarificationState != nil {
		t.Fatalf("expected clarification state to clear after resumed ask completes, got %+v", payload.ClarificationState)
	}
	if gen.calls < 5 {
		t.Fatalf("expected resumed workflow to reach test tool execution and final response, got %d generator calls", gen.calls)
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
		Domains:       []string{"coding_workflow", "desktop"},
		Phase:         "execution",
		PhasePlan:     []string{"investigation", "execution", "review"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		Confidence:    "high",
		Reasoning:     "The user wants failing tests fixed in a local code folder, which requires inspecting the code/tests and then modifying existing files.",
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

func TestApplyQueuedPhaseActorContext_NormalizesLegacyCodingQueuedActor(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), ctxKeyAskWorkItemActor, "phase:review")
	persisted := &AskTaskContext{
		Domains:   []string{"coding_workflow"},
		Phase:     "investigation",
		PhasePlan: []string{"investigation", "planning", "execution"},
	}
	taskCtx := ag.applyQueuedPhaseActorContext(ctx, &TaskContextClassification{}, persisted)
	if taskCtx.CapabilityHint != "execution" {
		t.Fatalf("expected legacy coding queued actor review to normalize to execution, got %q", taskCtx.CapabilityHint)
	}
	if taskCtx.AskClassification == nil || taskCtx.AskClassification.Phase != "execution" {
		t.Fatalf("expected persisted classification phase to normalize to execution, got %+v", taskCtx.AskClassification)
	}
}
