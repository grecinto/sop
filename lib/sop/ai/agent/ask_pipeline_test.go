package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
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
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
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

func TestBuildFallbackClassification_UsesDaemonDefaultDomains(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	fallback := agent.buildFallbackClassification("do something")
	if len(fallback.Domains) != 2 || fallback.Domains[0] != "coding_workflow" || fallback.Domains[1] != "desktop" {
		t.Fatalf("expected daemon fallback domains coding_workflow+desktop, got %#v", fallback.Domains)
	}
}

func TestBuildFallbackClassification_UsesHQDefaultDomains(t *testing.T) {
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	fallback := agent.buildFallbackClassification("do something")
	if len(fallback.Domains) != 2 || fallback.Domains[0] != "stores" || fallback.Domains[1] != "spaces" {
		t.Fatalf("expected HQ fallback domains stores+spaces, got %#v", fallback.Domains)
	}
}

func TestNormalizeAskTaskContext_UsesDaemonDefaultDomainsForGeneral(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	ctx := agent.normalizeAskTaskContext(AskTaskContext{Domains: []string{"general"}, OriginalQuery: "help me"})
	if len(ctx.Domains) != 2 || ctx.Domains[0] != "coding_workflow" || ctx.Domains[1] != "desktop" {
		t.Fatalf("expected general ambiguous ask to lean daemon defaults, got %#v", ctx.Domains)
	}
	if !ctx.DomainOfferNeeded {
		t.Fatal("expected daemon default domains to be treated as an offer")
	}
}

func TestNormalizeAskTaskContext_UsesHQDefaultDomainsForGeneral(t *testing.T) {
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	ctx := agent.normalizeAskTaskContext(AskTaskContext{Domains: []string{"general"}, OriginalQuery: "help me"})
	if len(ctx.Domains) != 2 || ctx.Domains[0] != "stores" || ctx.Domains[1] != "spaces" {
		t.Fatalf("expected general ambiguous ask to lean HQ defaults, got %#v", ctx.Domains)
	}
	if !ctx.DomainOfferNeeded {
		t.Fatal("expected HQ default domains to be treated as an offer")
	}
}

func TestNormalizeAskTaskContext_ClearStoresQueryDoesNotNeedOffer(t *testing.T) {
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	ctx := agent.normalizeAskTaskContext(AskTaskContext{Domains: []string{"general"}, OriginalQuery: "list stores in production database"})
	if len(ctx.Domains) != 1 || ctx.Domains[0] != "stores" {
		t.Fatalf("expected clear stores query to infer stores domain, got %#v", ctx.Domains)
	}
	if ctx.DomainOfferNeeded {
		t.Fatal("did not expect clear stores query to require a domain offer")
	}
}

func TestNormalizeAskTaskContext_ClearSpacesQueryDoesNotNeedOffer(t *testing.T) {
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	ctx := agent.normalizeAskTaskContext(AskTaskContext{Domains: []string{"general"}, OriginalQuery: "search the pricing knowledge base"})
	if len(ctx.Domains) != 1 || ctx.Domains[0] != "spaces" {
		t.Fatalf("expected clear spaces query to infer spaces domain, got %#v", ctx.Domains)
	}
	if ctx.DomainOfferNeeded {
		t.Fatal("did not expect clear spaces query to require a domain offer")
	}
}

func TestNormalizeAskTaskContext_ClearCodingQueryDoesNotNeedOffer(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	ctx := agent.normalizeAskTaskContext(AskTaskContext{Domains: []string{"general"}, OriginalQuery: "fix failing tests in the parser package"})
	if len(ctx.Domains) != 1 || ctx.Domains[0] != "coding_workflow" {
		t.Fatalf("expected clear coding query to infer coding_workflow domain, got %#v", ctx.Domains)
	}
	if ctx.DomainOfferNeeded {
		t.Fatal("did not expect clear coding query to require a domain offer")
	}
	if ctx.TaskShape != taskShapeIssueInvestigation {
		t.Fatalf("expected repo-fix ask to classify as issue investigation, got %q", ctx.TaskShape)
	}
	if ctx.OrchestrationLevel != orchestrationManagedObjective {
		t.Fatalf("expected repo-fix ask to require managed objective, got %q", ctx.OrchestrationLevel)
	}
	if ctx.Phase != "investigation" {
		t.Fatalf("expected repo-fix ask to start in investigation, got %q", ctx.Phase)
	}
	if got := strings.Join(ctx.PhasePlan, ","); got != "investigation,planning,execution" {
		t.Fatalf("expected repo-fix plan to include investigation, planning, execution, got %s", got)
	}
}

func TestNormalizeAskTaskContext_ClearDesktopQueryDoesNotNeedOffer(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	agent := &CopilotAgent{}
	ctx := agent.normalizeAskTaskContext(AskTaskContext{Domains: []string{"general"}, OriginalQuery: "run git status"})
	if len(ctx.Domains) != 1 || ctx.Domains[0] != "desktop" {
		t.Fatalf("expected clear desktop query to infer desktop domain, got %#v", ctx.Domains)
	}
	if ctx.DomainOfferNeeded {
		t.Fatal("did not expect clear desktop query to require a domain offer")
	}
}

func TestNormalizeAskTaskContext_AcceptsManagedObjectiveLevel(t *testing.T) {
	agent := &CopilotAgent{}
	ctx := agent.normalizeAskTaskContext(AskTaskContext{
		Domains:            []string{"coding_workflow"},
		OriginalQuery:      "fix failing tests in the parser package",
		OrchestrationLevel: "managed_objective",
		Phase:              "investigation",
	})
	if ctx.OrchestrationLevel != orchestrationManagedObjective {
		t.Fatalf("expected managed_objective to remain canonical, got %q", ctx.OrchestrationLevel)
	}
	if ctx.Phase != "investigation" {
		t.Fatalf("expected repo-fix ask to start in investigation, got %q", ctx.Phase)
	}
	if got := strings.Join(ctx.PhasePlan, ","); got != "investigation,planning,execution" {
		t.Fatalf("expected objective-driven repo-fix plan to include investigation, planning, execution, got %s", got)
	}
}

func TestAskCore_AmbiguousHQAskOffersStoresOrSpaces(t *testing.T) {
	SetLocalDesktopAutomationEnabled(false)
	defer SetLocalDesktopAutomationEnabled(false)
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.SetGenerator(&askRoutingTestGenerator{Response: `{"domains":["general"],"phase":"investigation","phase_plan":["investigation"],"intent":"general","confidence":"low","reasoning":"ambiguous ask"}`})
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	resp, err := ag.askCore(ctx, "help me with this", nil)
	if err != nil {
		t.Fatalf("askCore failed: %v", err)
	}
	if !strings.Contains(resp, "Stores or Spaces") {
		t.Fatalf("expected HQ ambiguous ask to offer Stores or Spaces, got: %s", resp)
	}
	if payload.ClarificationState == nil || payload.ClarificationState.Status != "pending" {
		t.Fatalf("expected pending clarification state, got %+v", payload.ClarificationState)
	}
}

func TestAskCore_AmbiguousDaemonAskOffersCodingOrDesktop(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.SetGenerator(&askRoutingTestGenerator{Response: `{"domains":["general"],"phase":"investigation","phase_plan":["investigation"],"intent":"general","confidence":"low","reasoning":"ambiguous ask"}`})
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	resp, err := ag.askCore(ctx, "help me with this", nil)
	if err != nil {
		t.Fatalf("askCore failed: %v", err)
	}
	if !strings.Contains(resp, "coding/repo changes or desktop/local commands") {
		t.Fatalf("expected daemon ambiguous ask to offer coding or desktop, got: %s", resp)
	}
	if payload.ClarificationState == nil || payload.ClarificationState.Status != "pending" {
		t.Fatalf("expected pending clarification state, got %+v", payload.ClarificationState)
	}
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

func TestCreatePhaseProxies_CodingWorkflowRespectsExecutionPhase(t *testing.T) {
	agent := &CopilotAgent{}
	classification := &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "execution",
		PhasePlan:     []string{"investigation", "execution", "review"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		OriginalQuery: "fix failing tests in the current repository",
	}

	phases := agent.createPhaseProxies([]string{"coding_workflow"}, "execution", classification)
	tools := agent.aggregatePhaseTools(phases, classification)
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	if !toolNames["run_tests"] {
		t.Fatalf("expected execution phase to expose run_tests, got %v", toolNames)
	}
	if !toolNames["apply_patch"] {
		t.Fatalf("expected execution phase to expose apply_patch, got %v", toolNames)
	}
	if toolNames["investigation_inspect_repo"] {
		t.Fatalf("did not expect execution phase to expose investigation tools, got %v", toolNames)
	}
	if toolNames["planning_plan_changes"] {
		t.Fatalf("did not expect execution phase to expose planning tools, got %v", toolNames)
	}
	if toolNames["review_build_context"] {
		t.Fatalf("did not expect execution phase to expose review_build_context, got %v", toolNames)
	}
	if toolNames["delivery_build_context"] {
		t.Fatalf("did not expect execution phase to expose delivery_build_context, got %v", toolNames)
	}
}

func TestCreatePhaseProxies_CodingWorkflowRespectsInvestigationPhase(t *testing.T) {
	agent := &CopilotAgent{}
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "investigation",
		PhasePlan:          []string{"investigation", "planning"},
		Intent:             "analyze",
		CRUD:               []string{"R"},
		OriginalQuery:      "investigate why the parser behaves this way",
	}

	phases := agent.createPhaseProxies([]string{"coding_workflow"}, "investigation", classification)
	tools := agent.aggregatePhaseTools(phases, classification)
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	if !toolNames["search_repo"] {
		t.Fatalf("expected investigation phase to expose search_repo, got %v", toolNames)
	}
	if !toolNames["run_tests"] {
		t.Fatalf("expected investigation phase to expose run_tests, got %v", toolNames)
	}
	if !toolNames["compile_artifact_inventory"] {
		t.Fatalf("expected investigation phase to expose compile_artifact_inventory, got %v", toolNames)
	}
	// After simplification: Only current phase tools are exposed (no look-ahead to planning/execution)
	if toolNames["planning_plan_changes"] {
		t.Fatalf("did not expect investigation phase to expose planning tools, got %v", toolNames)
	}
	if toolNames["execution_apply_patch"] {
		t.Fatalf("did not expect investigation phase to expose execution_apply_patch, got %v", toolNames)
	}
}

func TestResolveActiveCodingRuntimePhase_ManagedWorkflowRepoFixStaysInInvestigation(t *testing.T) {
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "execution",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	if got := resolveActiveCodingRuntimePhase(classification, classification.Phase); got != "investigation" {
		t.Fatalf("expected managed repo-fix ask to stay in investigation until execution-ready, got %q", got)
	}
}

func TestResolvePhasePlan_PromotesManagedCodingExecutionWhenPlanningArtifactIsReady(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	ag.service.session.Memory.AddThread(thread)
	artifact := `{"summary":"Execution plan for fix failing tests","status":"ready_for_execution","plan":["inspect failing tests","patch root cause","rerun focused tests"],"structured_data":{"detailed_execution_plan":[{"title":"Step 1","description":"Identify the exact failing tests and root cause.","validation":["root cause stated"]}]}}`
	if err := ag.service.session.Memory.SaveWorkflowMemory("phase-artifact:planning", map[string]string{"artifact_json": artifact}); err != nil {
		t.Fatalf("failed to seed planning artifact: %v", err)
	}
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "execution",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	plan, currentPhase := ag.resolvePhasePlan(context.Background(), classification)
	if !classification.ExecutionReady {
		t.Fatal("expected ready_for_execution planning artifact to promote execution readiness")
	}
	if currentPhase != "execution" {
		t.Fatalf("expected managed coding ask to enter execution when planning artifact is ready, got %q", currentPhase)
	}
	if got := strings.Join(plan, ","); got != "investigation,planning,execution" {
		t.Fatalf("expected canonical coding phase plan, got %s", got)
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
	EnableTestModeForTest(t) // Test uses context.Background() without SessionPayload
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

func TestCanonicalizeCodingRuntimePlan_UsesActiveCodingPhasesOnly(t *testing.T) {
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}}
	plan := canonicalizeCodingRuntimePlan(classification, []string{"intake", "investigation", "planning", "testing", "execution", "review", "delivery"})
	joined := strings.Join(plan, ",")
	if joined != "investigation,planning,execution" {
		t.Fatalf("expected coding plan to use only active runtime phases investigation,planning,execution, got %v", plan)
	}
}

func TestResolvePhasePlan_CanonicalizesCodingWorkStreamPlanOnResume(t *testing.T) {
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
		t.Fatalf("expected persisted non-active coding phase to canonicalize to execution, got %q", currentPhase)
	}
	if thread.StreamContext.PhasePlan.ExecutionBrief == nil || len(thread.StreamContext.PhasePlan.ExecutionBrief.Steps) == 0 {
		t.Fatal("expected resumed coding plan to gain an execution brief")
	}
}

func TestResolvePhasePlan_SyncsCurrentIndexFromQueuedPhaseActor(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	thread.InitializeWorkStream("investigation")
	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   []string{"investigation", "planning", "execution"},
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: []string{"investigation", "planning", "execution"},
		Checkpoints:     map[string]bool{"after_investigation": true, "after_planning": true},
	}
	ag.service.session.Memory.AddThread(thread)

	ctx := context.WithValue(context.Background(), ctxKeyAskWorkItemActor, "phase:planning")
	plan, currentPhase := ag.resolvePhasePlan(ctx, &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		Phase:              "planning",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		OrchestrationLevel: orchestrationManagedObjective,
		OriginalQuery:      "fix failing tests",
	})

	if strings.Join(plan, ",") != "investigation,planning,execution" {
		t.Fatalf("expected canonical coding plan, got %v", plan)
	}
	if currentPhase != "planning" {
		t.Fatalf("expected queued planning actor to resume planning, got %q", currentPhase)
	}
	if thread.StreamContext.PhasePlan.CurrentIndex != 1 {
		t.Fatalf("expected queued planning actor to sync current index to 1, got %d", thread.StreamContext.PhasePlan.CurrentIndex)
	}
}

func TestResolvePhasePlan_AttachesExecutionBriefForFailingTests(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	ag.service.session.Memory.AddThread(thread)

	plan, currentPhase := ag.resolvePhasePlan(context.Background(), &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "execution",
		PhasePlan:     []string{"investigation", "planning", "execution"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		Confidence:    "high",
		Reasoning:     "repo repair requires evidence then implementation",
		OriginalQuery: "fix failing tests in the parser package",
	})

	if strings.Join(plan, ",") != "investigation,planning,execution" {
		t.Fatalf("expected canonical coding plan, got %v", plan)
	}
	if currentPhase != "execution" {
		t.Fatalf("expected execution runtime phase, got %q", currentPhase)
	}
	if thread.StreamContext == nil || thread.StreamContext.PhasePlan == nil || thread.StreamContext.PhasePlan.ExecutionBrief == nil {
		t.Fatal("expected execution brief to be attached to the persisted phase plan")
	}
	brief := thread.StreamContext.PhasePlan.ExecutionBrief
	if brief.WorkflowShape != "checkpointed_fix" {
		t.Fatalf("expected checkpointed_fix workflow shape, got %q", brief.WorkflowShape)
	}
	if len(brief.Steps) < 5 {
		t.Fatalf("expected multiple execution steps, got %#v", brief.Steps)
	}
	if !strings.Contains(strings.ToLower(brief.Steps[0].Description), "failing tests") {
		t.Fatalf("expected first step to target failing tests, got %#v", brief.Steps[0])
	}
	if !strings.Contains(strings.ToLower(brief.Steps[0].Description), "compile the full initial failure inventory") {
		t.Fatalf("expected first step to require compiling the full initial failure inventory, got %#v", brief.Steps[0])
	}
	if got := strings.Join(brief.Steps[0].ContextInstructions, " "); !strings.Contains(got, "broad recursive test command only for the initial failure inventory") {
		t.Fatalf("expected first step to limit broad suite runs to initial discovery, got %#v", brief.Steps[0].ContextInstructions)
	}
	if got := strings.Join(brief.Steps[0].ContextInstructions, " "); !strings.Contains(got, "compile the full set of failing packages, folders, or named tests") {
		t.Fatalf("expected first step to require compiling the full failure inventory before selecting a cluster, got %#v", brief.Steps[0].ContextInstructions)
	}
}

func TestBuildCodingExecutionBriefSteps_CheckpointedFixStartsWithFullFailureInventory(t *testing.T) {
	steps := buildCodingExecutionBriefSteps("fix failing tests in the parser package", "checkpointed_fix")
	if len(steps) == 0 {
		t.Fatal("expected checkpointed_fix steps")
	}
	if !strings.Contains(strings.ToLower(steps[0].Description), "compile the full initial failure inventory") {
		t.Fatalf("expected first checkpointed_fix step to compile the full initial failure inventory, got %#v", steps[0])
	}
	contextText := strings.Join(steps[0].ContextInstructions, " ")
	if !strings.Contains(contextText, "compile the full set of failing packages, folders, or named tests") {
		t.Fatalf("expected first checkpointed_fix step to require compiling the full failure inventory before cluster selection, got %#v", steps[0].ContextInstructions)
	}
}

func TestCurrentExecutionBrief_IncludesStoredFailureInventoryContext(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.FailureInventoryCaptured = true
	thread.StreamContext.FailureClusters = []string{"package:github.com/sharedcode/sop/tools/sopbackend", "package:github.com/sharedcode/sop/src/backend/site"}
	thread.StreamContext.ActiveFailureCluster = "package:github.com/sharedcode/sop/tools/sopbackend"
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}, OriginalQuery: "fix failing tests in the parser package"}

	brief := ag.currentExecutionBrief(classification, classification.OriginalQuery)
	if brief == nil {
		t.Fatal("expected execution brief")
	}
	if !strings.Contains(brief.CurrentFocus, "Active failure cluster: package:github.com/sharedcode/sop/tools/sopbackend") {
		t.Fatalf("expected current focus to include active failure cluster, got %#v", brief.CurrentFocus)
	}
	if len(brief.Steps) == 0 {
		t.Fatal("expected execution brief steps")
	}
	joined := strings.Join(brief.Steps[0].ContextInstructions, " ")
	if !strings.Contains(joined, "Compiled failure inventory: package:github.com/sharedcode/sop/tools/sopbackend; package:github.com/sharedcode/sop/src/backend/site") {
		t.Fatalf("expected first step to include compiled failure inventory, got %#v", brief.Steps[0].ContextInstructions)
	}
	if !strings.Contains(joined, "Active failure cluster: package:github.com/sharedcode/sop/tools/sopbackend") {
		t.Fatalf("expected first step to include active failure cluster, got %#v", brief.Steps[0].ContextInstructions)
	}
}

func TestCurrentExecutionBrief_IncludesWorkInventoryContext(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.WorkInventory = &WorkInventory{
		InventoryType: "test_failures",
		ActiveItemID:  "test:TestAuthenticateUser_ValidCredentials",
		Items: []InventoryItem{
			{ID: "test:TestAuthenticateUser_ValidCredentials", Type: "test", DisplayName: "TestAuthenticateUser_ValidCredentials", Status: "active"},
			{ID: "test:TestSessionTokenTTL", Type: "test", DisplayName: "TestSessionTokenTTL", Status: "pending"},
		},
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}, OriginalQuery: "fix failing tests in the parser package"}

	brief := ag.currentExecutionBrief(classification, classification.OriginalQuery)
	if brief == nil {
		t.Fatal("expected execution brief")
	}
	if !strings.Contains(brief.CurrentFocus, "Active work item: TestAuthenticateUser_ValidCredentials") {
		t.Fatalf("expected current focus to include active work item, got %#v", brief.CurrentFocus)
	}
	if len(brief.Steps) == 0 {
		t.Fatal("expected execution brief steps")
	}
	joined := strings.Join(brief.Steps[0].ContextInstructions, " ")
	if !strings.Contains(joined, "Compiled work inventory: TestAuthenticateUser_ValidCredentials (active); TestSessionTokenTTL (pending)") {
		t.Fatalf("expected first step to include compiled work inventory, got %#v", brief.Steps[0].ContextInstructions)
	}
	if !strings.Contains(joined, "Active work item: TestAuthenticateUser_ValidCredentials") {
		t.Fatalf("expected first step to include active work item, got %#v", brief.Steps[0].ContextInstructions)
	}
}

func TestBuildPhasePrompt_IncludesExecutionBrief(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases: []string{"investigation", "planning", "execution"},
		CurrentIndex:  2,
		ExecutionBrief: &ExecutionBrief{
			WorkflowShape: "checkpointed_fix",
			Objective:     "fix failing tests in the parser package",
			CurrentFocus:  "Work through the current implementation steps in order, validate after each meaningful change, and summarize remaining risk before concluding.",
			Steps: []ExecutionBriefStep{
				{Title: "Step 1", Phase: "investigation", Description: "Identify the exact failing tests and capture the current failure output."},
				{Title: "Step 2", Phase: "planning", Description: "Choose the smallest repair and the narrowest validation target before editing files."},
				{Title: "Step 3", Phase: "execution", Description: "Implement the smallest plausible fix that explains the observed failure.", Validation: []string{"change stays within the observed repair boundary"}},
			},
			CompletionCriteria: []string{"The most relevant validation step has been rerun or the blocker is recorded."},
		},
	}
	ag.service.session.Memory.AddThread(thread)

	classification := &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "execution",
		PhasePlan:     []string{"investigation", "planning", "execution"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		Confidence:    "high",
		Reasoning:     "fixing code",
		OriginalQuery: "fix failing tests in the parser package",
	}
	phases := ag.createPhaseProxies([]string{"coding_workflow"}, "execution", classification)
	prompt := ag.buildPhasePrompt(context.Background(), classification.OriginalQuery, classification, phases)

	if !strings.Contains(prompt, "=== Active Execution Brief ===") {
		t.Fatalf("expected prompt to include execution brief, got %s", prompt)
	}
	if !strings.Contains(prompt, "Implement the smallest plausible fix") {
		t.Fatalf("expected prompt to include active execution step, got %s", prompt)
	}
	if !strings.Contains(prompt, "Workflow Shape: checkpointed_fix") {
		t.Fatalf("expected prompt to include workflow shape, got %s", prompt)
	}
}

func TestBuildPhasePrompt_IncludesPhaseTransactionHandoff(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	thread.InitializeWorkStream("planning")
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "planning",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}
	decision := phaseTransactionDecision{EnqueueNextPhase: true, NextPhase: "planning", Reason: "managed coding issue workflow requires planning after failing-test investigation"}
	ag.persistPhaseTransactionHandoff(classification.OriginalQuery, classification, decision, []ai.ToolCall{{Name: "run_repo_tests", Args: map[string]any{"package": "./..."}}}, []string{"--- FAIL: TestAuthenticateUser_ValidCredentials"}, "Observed a failing authentication test after running the focused repo test command.")

	phases := ag.createPhaseProxies([]string{"coding_workflow"}, "planning", classification)
	prompt := ag.buildPhasePrompt(context.Background(), classification.OriginalQuery, classification, phases)

	if !strings.Contains(prompt, "=== Phase Transaction Handoff ===") {
		t.Fatalf("expected prompt to include phase transaction handoff, got %s", prompt)
	}
	if !strings.Contains(prompt, "run_repo_tests") {
		t.Fatalf("expected prompt to include observed tool call, got %s", prompt)
	}
	if !strings.Contains(prompt, "--- FAIL: TestAuthenticateUser_ValidCredentials") {
		t.Fatalf("expected prompt to include failing test evidence, got %s", prompt)
	}
	if !strings.Contains(prompt, "Prior-turn analysis:") {
		t.Fatalf("expected prompt to include prior-turn analysis, got %s", prompt)
	}
	values, err := ag.service.session.Memory.LoadWorkflowMemory("phase-handoff:planning")
	if err != nil {
		t.Fatalf("expected stored planning handoff, got %v", err)
	}
	if strings.TrimSpace(values["handoff_json"]) == "" {
		t.Fatalf("expected non-empty handoff payload, got %#v", values)
	}
}

func TestResolvePhasePlan_PrefersAuthoredPlanningArtifactForExecutionBrief(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Implement parser validation"}
	ag.service.session.Memory.AddThread(thread)
	artifact := `{"summary":"Execution plan for add parser validation","status":"ready_for_execution","plan":["inspect parser","add validation","run parser tests"],"structured_data":{"detailed_execution_plan":[{"title":"Step 1","description":"Inspect parser error handling and malformed-input path.","validation":["affected code path identified"]},{"title":"Step 2","description":"Implement parser validation for malformed input.","validation":["change remains localized"]},{"title":"Step 3","description":"Run parser tests and confirm malformed input behavior.","validation":["targeted parser tests pass"]}]}}`
	if err := ag.service.session.Memory.SaveWorkflowMemory("phase-artifact:planning", map[string]string{"artifact_json": artifact}); err != nil {
		t.Fatalf("failed to seed planning artifact: %v", err)
	}

	_, _ = ag.resolvePhasePlan(context.Background(), &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "execution",
		PhasePlan:     []string{"investigation", "planning", "execution"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		Confidence:    "high",
		Reasoning:     "feature implementation",
		OriginalQuery: "implement parser validation for malformed input",
	})

	brief := thread.StreamContext.PhasePlan.ExecutionBrief
	if brief == nil {
		t.Fatal("expected execution brief to be attached")
	}
	if len(brief.Steps) != 3 {
		t.Fatalf("expected authored execution brief to keep 3 steps, got %#v", brief.Steps)
	}
	if brief.Steps[1].Description != "Implement parser validation for malformed input." {
		t.Fatalf("expected authored step description, got %#v", brief.Steps[1])
	}
	if brief.WorkflowShape != "feature_plan" {
		t.Fatalf("expected feature_plan workflow shape from authored plan, got %q", brief.WorkflowShape)
	}
	if thread.StreamContext.StructuredPlan == nil {
		t.Fatal("expected authored planning artifact to be promoted to a live structured plan")
	}
	if len(thread.StreamContext.StructuredPlan.Steps) != 3 {
		t.Fatalf("expected promoted structured plan to keep 3 steps, got %#v", thread.StreamContext.StructuredPlan)
	}
	if thread.StreamContext.StructuredPlan.CurrentStep != 0 || thread.StreamContext.StructuredPlan.Steps[0].Status != "active" {
		t.Fatalf("expected promoted structured plan to start at active step 0, got %#v", thread.StreamContext.StructuredPlan)
	}
}

func TestAuthoredPlanningArtifactStructuredPlan_AdvancesAndQueuesNextStep(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix parser validation"}
	ag.service.session.Memory.AddThread(thread)
	ag.service.session.Memory.SetRoutingState(&TaskContextClassification{AskClassification: &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}, OriginalQuery: "implement parser validation for malformed input"}})
	artifact := `{"summary":"Execution plan for add parser validation","status":"ready_for_execution","plan":["inspect parser","add validation","run parser tests"],"structured_data":{"detailed_execution_plan":[{"title":"Step 1","description":"Inspect parser error handling and malformed-input path.","validation":["affected code path identified"]},{"title":"Step 2","description":"Implement parser validation for malformed input.","validation":["change remains localized"]},{"title":"Step 3","description":"Run parser tests and confirm malformed input behavior.","validation":["targeted parser tests pass"]}]}}`
	if err := ag.service.session.Memory.SaveWorkflowMemory("phase-artifact:planning", map[string]string{"artifact_json": artifact}); err != nil {
		t.Fatalf("failed to seed planning artifact: %v", err)
	}
	classification := &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "execution",
		PhasePlan:     []string{"investigation", "planning", "execution"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		Confidence:    "high",
		Reasoning:     "feature implementation",
		OriginalQuery: "implement parser validation for malformed input",
	}

	_, _ = ag.resolvePhasePlan(context.Background(), classification)
	plan := thread.StreamContext.StructuredPlan
	if plan == nil || len(plan.Steps) != 3 {
		t.Fatalf("expected structured plan promotion, got %#v", plan)
	}

	ag.updateStructuredPlanProgressAfterTurn(classification, []ai.ToolCall{{Name: "investigation_inspect_repo"}}, []string{"affected code path identified"}, nil)
	if plan.CurrentStep != 1 {
		t.Fatalf("expected current step to advance to 1, got %#v", plan)
	}
	if plan.Steps[0].Status != "complete" || plan.Steps[1].Status != "active" {
		t.Fatalf("expected first step complete and second active, got %#v", plan.Steps)
	}

	ag.enqueueStructuredPlanContinuationAfterTurn(classification.OriginalQuery, classification, 0, nil)
	snapshot := ag.getOrCreateSessionQueue().Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected one queued continuation, got %#v", snapshot)
	}
	if snapshot[0].Actor != "phase:execution" {
		t.Fatalf("expected next authored step to queue execution phase for the repair step, got %#v", snapshot[0])
	}
	if step, _ := snapshot[0].Context["structured_plan_step"].(int); step != 1 {
		t.Fatalf("expected queued continuation to point at step 1, got %#v", snapshot[0].Context)
	}
}

func TestInferStepPhaseForText_PrefersRepairAndValidationOverIndexHeuristics(t *testing.T) {
	if got := inferStepPhaseForText("Implement parser validation for malformed input.", 1, 3); got != "execution" {
		t.Fatalf("expected repair step to map to execution, got %q", got)
	}
	if got := inferStepPhaseForText("Run parser tests and confirm malformed input behavior.", 2, 3); got != "execution" {
		t.Fatalf("expected validation step to stay in execution for coding workflow runtime, got %q", got)
	}
}

func TestCurrentExecutionBrief_UsesPersistedStructuredPlan(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix parser behavior"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Fix parser failing tests",
		WorkflowShape: "repair",
		Domain:        "coding_workflow",
		Steps: []cw.StructuredPlanStep{
			{StepID: "repair_1", Goal: "Localize failing behavior", ActionInstruction: "Inspect the failing test and parser implementation together.", EvidenceToCollect: []string{"failing assertion", "implicated code path"}, Validation: []string{"repair direction is explicit"}},
			{StepID: "repair_2", Goal: "Apply repair", ActionInstruction: "Patch the parser or test based on the chosen contract.", AllowedChangeScope: "localized patch", Files: []string{"lib/parser.go", "lib/parser_test.go"}, Validation: []string{"targeted parser tests pass"}},
		},
		CompletionGate: cw.StructuredCompletionGate{Requirements: []string{"rerun the original failing test"}},
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}, OriginalQuery: "fix parser failing tests"}

	brief := ag.currentExecutionBrief(classification, "fix parser failing tests")
	if brief == nil {
		t.Fatal("expected execution brief")
	}
	if len(brief.Steps) != 2 {
		t.Fatalf("expected projected structured plan steps, got %#v", brief.Steps)
	}
	if brief.Steps[0].Description != "Inspect the failing test and parser implementation together." {
		t.Fatalf("expected first structured plan instruction to drive description, got %#v", brief.Steps[0])
	}
	if got := strings.Join(brief.Steps[1].ContextInstructions, " "); !strings.Contains(got, "Allowed scope: localized patch") {
		t.Fatalf("expected structured plan context instructions to include allowed scope, got %#v", brief.Steps[1].ContextInstructions)
	}
	if got := brief.CompletionCriteria; len(got) != 1 || got[0] != "rerun the original failing test" {
		t.Fatalf("expected structured plan completion gate, got %#v", got)
	}
}

func TestUpdateStructuredPlanProgressAfterTurn_ActivatesAndAdvancesStep(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix parser behavior"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Fix parser failing tests",
		WorkflowShape: "repair",
		Domain:        "coding_workflow",
		Steps: []cw.StructuredPlanStep{
			{StepID: "repair_1", Goal: "Inspect failing behavior", ActionInstruction: "Inspect the failing test and parser implementation together.", Status: "pending"},
			{StepID: "repair_2", Goal: "Apply fix", ActionInstruction: "Apply the smallest valid patch.", Status: "pending"},
		},
		CurrentStep: 0,
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "investigation", PhasePlan: []string{"investigation", "planning", "execution"}}

	ag.updateStructuredPlanProgressAfterTurn(classification, []ai.ToolCall{{Name: "investigation_inspect_repo"}}, []string{"Found failing parser path"}, nil)

	plan := thread.StreamContext.StructuredPlan
	if plan.Steps[0].Status != "complete" {
		t.Fatalf("expected first step complete, got %#v", plan.Steps[0])
	}
	if plan.CurrentStep != 1 {
		t.Fatalf("expected current step to advance, got %#v", plan)
	}
	if plan.Steps[1].Status != "active" {
		t.Fatalf("expected next step active, got %#v", plan.Steps[1])
	}
	if len(plan.CompletedSteps) != 1 || plan.CompletedSteps[0] != "repair_1" {
		t.Fatalf("expected completed steps to include repair_1, got %#v", plan.CompletedSteps)
	}
}

func TestUpdateStructuredPlanProgressAfterTurn_DoesNotAdvanceOnClarification(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix parser behavior"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Fix parser failing tests",
		WorkflowShape: "repair",
		Domain:        "coding_workflow",
		Steps: []cw.StructuredPlanStep{{
			StepID:            "repair_1",
			Goal:              "Run targeted tests",
			ActionInstruction: "Run the original failing test.",
			Status:            "pending",
		}},
		CurrentStep: 0,
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}}

	ag.updateStructuredPlanProgressAfterTurn(classification, []ai.ToolCall{{Name: compatibleClarificationToolName}}, nil, &ai.ClarificationRequest{Status: "pending"})

	plan := thread.StreamContext.StructuredPlan
	if plan.CurrentStep != 0 {
		t.Fatalf("expected current step to remain unchanged, got %#v", plan)
	}
	if plan.Steps[0].Status != "active" {
		t.Fatalf("expected pending step to become active but not complete, got %#v", plan.Steps[0])
	}
	if len(plan.CompletedSteps) != 0 {
		t.Fatalf("expected no completed steps, got %#v", plan.CompletedSteps)
	}
}

func TestEnqueueStructuredPlanContinuationAfterTurn_QueuesNextStepOnce(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix parser behavior"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Fix parser failing tests",
		WorkflowShape: "repair",
		Domain:        "coding_workflow",
		Steps: []cw.StructuredPlanStep{
			{StepID: "repair_1", Goal: "Inspect failing behavior", ActionInstruction: "Inspect the failing test and parser implementation together.", Status: "complete"},
			{StepID: "repair_2", Goal: "Apply fix", ActionInstruction: "Apply the smallest valid patch.", Status: "active"},
		},
		CurrentStep:    1,
		CompletedSteps: []string{"repair_1"},
	}
	ag.service.session.Memory.AddThread(thread)
	ag.service.session.Memory.SetRoutingState(&TaskContextClassification{AskClassification: &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}}})

	ag.enqueueStructuredPlanContinuationAfterTurn("fix parser failing tests", &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}}, 0, nil)
	queue := ag.getOrCreateSessionQueue()
	snapshot := queue.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected one continuation item, got %#v", snapshot)
	}
	if snapshot[0].Actor != "phase:planning" {
		t.Fatalf("expected queued continuation to target planning phase, got %#v", snapshot[0])
	}
	if flag, _ := snapshot[0].Context["structured_plan_continuation"].(bool); !flag {
		t.Fatalf("expected structured plan continuation flag, got %#v", snapshot[0].Context)
	}
	if step, _ := snapshot[0].Context["structured_plan_step"].(int); step != 1 {
		t.Fatalf("expected queued continuation to point at current step 1, got %#v", snapshot[0].Context)
	}

	ag.enqueueStructuredPlanContinuationAfterTurn("fix parser failing tests", &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", PhasePlan: []string{"investigation", "planning", "execution"}}, 0, nil)
	if got := len(queue.Snapshot()); got != 1 {
		t.Fatalf("expected duplicate continuation to be ignored, got %d items", got)
	}

	routing := ag.service.session.Memory.GetRoutingState()
	if routing == nil || routing.AskClassification == nil || routing.AskClassification.Phase != "planning" {
		t.Fatalf("expected routing state phase to move to planning, got %+v", routing)
	}
}

func TestUpdateStructuredPlanProgressAfterTurn_UsesStoresCapabilityCompletion(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Validate store results"}
	thread.InitializeWorkStream("review")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Validate store results",
		WorkflowShape: "repair",
		Domain:        "stores",
		Steps: []cw.StructuredPlanStep{{
			StepID:            "store_1",
			Goal:              "Verify the expected store results",
			ActionInstruction: "Validate the expected store result contract.",
			Status:            "pending",
		}},
		CurrentStep: 0,
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"stores"}, Phase: "review", OriginalQuery: "validate store results"}

	ag.updateStructuredPlanProgressAfterTurn(classification, []ai.ToolCall{{Name: "review_verify_results"}}, []string{"Store validation succeeded"}, nil)

	if thread.StreamContext.StructuredPlan.Steps[0].Status != "complete" {
		t.Fatalf("expected stores capability completion to complete step, got %#v", thread.StreamContext.StructuredPlan.Steps[0])
	}
}

func TestUpdateStructuredPlanProgressAfterTurn_UsesSpacesCapabilityCompletion(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Plan space mutation"}
	thread.InitializeWorkStream("planning")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Plan a space mutation",
		WorkflowShape: "repair",
		Domain:        "spaces",
		Steps: []cw.StructuredPlanStep{{
			StepID:            "space_1",
			Goal:              "Design the knowledge mutation",
			ActionInstruction: "Design the target knowledge mutation.",
			Status:            "pending",
		}},
		CurrentStep: 0,
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"spaces"}, Phase: "planning", OriginalQuery: "plan space mutation"}

	ag.updateStructuredPlanProgressAfterTurn(classification, []ai.ToolCall{{Name: "planning_design_mutation"}}, []string{"Mutation plan ready"}, nil)

	if thread.StreamContext.StructuredPlan.Steps[0].Status != "complete" {
		t.Fatalf("expected spaces capability completion to complete step, got %#v", thread.StreamContext.StructuredPlan.Steps[0])
	}
}

func TestUpdateStructuredPlanProgressAfterTurn_UsesCodingCapabilityCompletion(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix parser behavior"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Fix parser failing tests",
		WorkflowShape: "repair",
		Domain:        "coding_workflow",
		Steps: []cw.StructuredPlanStep{{
			StepID:            "repair_1",
			Goal:              "Apply the smallest valid patch",
			ActionInstruction: "Apply the smallest valid patch.",
			Status:            "pending",
		}},
		CurrentStep: 0,
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"coding_workflow"}, Phase: "execution", OriginalQuery: "fix parser behavior"}

	ag.updateStructuredPlanProgressAfterTurn(classification, []ai.ToolCall{{Name: "execution_apply_patch"}}, []string{"Patch applied"}, nil)

	if thread.StreamContext.StructuredPlan.Steps[0].Status != "complete" {
		t.Fatalf("expected coding capability completion to complete step, got %#v", thread.StreamContext.StructuredPlan.Steps[0])
	}
}

func TestUpdateStructuredPlanProgressAfterTurn_UsesDesktopCapabilityCompletion(t *testing.T) {
	SetLocalDesktopAutomationEnabled(true)
	defer SetLocalDesktopAutomationEnabled(false)
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Run git status"}
	thread.InitializeWorkStream("execution")
	thread.StreamContext.StructuredPlan = &cw.StructuredPlan{
		Objective:     "Inspect repository state",
		WorkflowShape: "repair",
		Domain:        "desktop",
		Steps: []cw.StructuredPlanStep{{
			StepID:            "desktop_1",
			Goal:              "Run the repository inspection command",
			ActionInstruction: "Run git status in the current workspace.",
			Status:            "pending",
		}},
		CurrentStep: 0,
	}
	ag.service.session.Memory.AddThread(thread)
	classification := &AskTaskContext{Domains: []string{"desktop"}, Phase: "execution", OriginalQuery: "run git status"}

	ag.updateStructuredPlanProgressAfterTurn(classification, []ai.ToolCall{{Name: "execute_local_command"}}, []string{"git status completed"}, nil)

	if thread.StreamContext.StructuredPlan.Steps[0].Status != "complete" {
		t.Fatalf("expected desktop capability completion to complete step, got %#v", thread.StreamContext.StructuredPlan.Steps[0])
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

	secondResp, err := ag.askCore(ctx, "current repo", nil)
	if err != nil {
		t.Fatalf("second askCore failed: %v", err)
	}
	if !strings.Contains(secondResp, "I ran the tests in the current folder") {
		t.Fatalf("expected resumed turn to continue after running tests, got: %s", secondResp)
	}
	if payload.ClarificationState != nil {
		t.Fatalf("expected clarification state to clear after resumed ask completes, got %+v", payload.ClarificationState)
	}
	routing := payload.Variables["RoutingState"]
	if routing == nil {
		t.Fatal("expected resumed clarification flow to preserve routing state")
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

func TestApplyQueuedPhaseActorContext_CanonicalizesNonActiveCodingQueuedActor(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ctx := context.WithValue(context.Background(), ctxKeyAskWorkItemActor, "phase:review")
	persisted := &AskTaskContext{
		Domains:   []string{"coding_workflow"},
		Phase:     "investigation",
		PhasePlan: []string{"investigation", "planning", "execution"},
	}
	taskCtx := ag.applyQueuedPhaseActorContext(ctx, &TaskContextClassification{}, persisted)
	if taskCtx.CapabilityHint != "execution" {
		t.Fatalf("expected non-active coding queued actor review to canonicalize to execution phase, got %q", taskCtx.CapabilityHint)
	}
	if taskCtx.AskClassification == nil || taskCtx.AskClassification.Phase != "investigation" {
		t.Fatalf("expected persisted classification phase to remain investigation until execution is ready, got %+v", taskCtx.AskClassification)
	}
}

func TestApplyQueuedPhaseActorContext_PromotesExecutionReadyFromPlanningArtifact(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	artifact := `{"summary":"Execution plan for fix failing tests","status":"ready_for_execution","plan":["inspect failing tests","patch root cause","rerun focused tests"]}`
	if err := ag.service.session.Memory.SaveWorkflowMemory("phase-artifact:planning", map[string]string{"artifact_json": artifact}); err != nil {
		t.Fatalf("failed to seed planning artifact: %v", err)
	}
	ctx := context.WithValue(context.Background(), ctxKeyAskWorkItemActor, "phase:execution")
	persisted := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "investigation",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		OriginalQuery:      "fix failing tests in the repository",
	}

	taskCtx := ag.applyQueuedPhaseActorContext(ctx, &TaskContextClassification{}, persisted)
	if taskCtx.AskClassification == nil {
		t.Fatal("expected ask classification to be attached")
	}
	if !taskCtx.AskClassification.ExecutionReady {
		t.Fatal("expected queued actor context to promote execution readiness from ready_for_execution artifact")
	}
	if taskCtx.AskClassification.Phase != "execution" {
		t.Fatalf("expected queued actor classification phase to resolve to execution, got %+v", taskCtx.AskClassification)
	}
	if taskCtx.CapabilityHint != "execution" {
		t.Fatalf("expected queued execution actor to keep execution phase surface, got %q", taskCtx.CapabilityHint)
	}
}

func TestNormalizeCodingRuntimeRouting_UsesCanonicalPlanningPhaseForActionOrientedCodingAsk(t *testing.T) {
	taskCtx := normalizeCodingRuntimeRouting(&TaskContextClassification{
		Domain:         "coding_workflow",
		CapabilityHint: "investigation",
		AskClassification: &AskTaskContext{
			Domains:       []string{"coding_workflow"},
			Phase:         "planning",
			PhasePlan:     []string{"investigation", "planning", "execution"},
			Intent:        "generate_code",
			CRUD:          []string{"R", "U"},
			OriginalQuery: "fix failing tests in the repository",
		},
	})
	if taskCtx.CapabilityHint != "planning" {
		t.Fatalf("expected action-oriented coding ask to preserve the canonical planning phase, got %q", taskCtx.CapabilityHint)
	}
	if taskCtx.AskClassification == nil || taskCtx.AskClassification.Phase != "planning" {
		t.Fatalf("expected Ask classification to preserve entry-phase semantics, got %+v", taskCtx.AskClassification)
	}
}

func TestResolvePhasePlan_UsesImplementationSurfaceWhenPersistedCodingPlanIsStillAtPlanning(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	thread := &ConversationThread{ID: sop.NewUUID(), RootPrompt: "Fix failing tests"}
	thread.InitializeWorkStream("planning")
	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   []string{"investigation", "planning", "execution"},
		CurrentIndex:    1,
		CompletedPhases: []string{"investigation"},
		RemainingPhases: []string{"planning", "execution"},
	}
	ag.service.session.Memory.AddThread(thread)

	plan, currentPhase := ag.resolvePhasePlan(context.Background(), &AskTaskContext{
		Domains:       []string{"coding_workflow"},
		Phase:         "planning",
		PhasePlan:     []string{"investigation", "planning", "execution"},
		Intent:        "generate_code",
		CRUD:          []string{"R", "U"},
		OriginalQuery: "fix failing tests in the repository",
	})
	if strings.Join(plan, ",") != "investigation,planning,execution" {
		t.Fatalf("expected canonical coding runtime plan, got %v", plan)
	}
	if currentPhase != "execution" {
		t.Fatalf("expected persisted planning cursor for action-oriented coding ask to expose implementation surface, got %q", currentPhase)
	}
}

func TestEvaluatePhaseTransaction_QueuesPlanningAfterFailingTestInvestigation(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "investigation",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}
	toolCalls := []ai.ToolCall{{Name: "run_repo_tests"}}
	outcomeFacts := []string{"--- FAIL: TestAuthenticateUser_ValidCredentials"}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, toolCalls, outcomeFacts, "", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected failing-test investigation to enqueue planning")
	}
	if decision.NextPhase != "planning" {
		t.Fatalf("expected next phase planning, got %q", decision.NextPhase)
	}
	queue := ag.getOrCreateSessionQueue()
	ag.enqueuePhaseTransactionDecision(classification.OriginalQuery, decision)
	snapshot := queue.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected exactly one queued continuation, got %d", len(snapshot))
	}
	if snapshot[0].Actor != "phase:planning" {
		t.Fatalf("expected queued actor phase:planning, got %q", snapshot[0].Actor)
	}
	if got, _ := snapshot[0].Context["controller_guidance"].(string); !strings.Contains(got, "continue in planning") {
		t.Fatalf("expected controller guidance to describe the missing planning work, got %q", got)
	}
	if got, _ := snapshot[0].Context["reason"].(string); !strings.Contains(strings.ToLower(got), "failing-test investigation") {
		t.Fatalf("expected reason to describe the investigation-to-planning handoff, got %q", got)
	}
}

func TestEvaluatePhaseTransaction_DoesNotQueuePlanningWithoutFailingEvidence(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "investigation",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, []ai.ToolCall{{Name: "run_repo_tests"}}, []string{"tests passed"}, "All tests passed.", nil)
	if decision.EnqueueNextPhase {
		t.Fatalf("expected no planning continuation when failing evidence is absent, got %+v", decision)
	}
}

func TestEvaluatePhaseTransaction_QueuesPlanningWhenExecutionLacksReadyArtifact(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "execution",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, nil, nil, "", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected execution without a ready planning artifact to enqueue planning")
	}
	if decision.NextPhase != "planning" {
		t.Fatalf("expected next phase planning, got %q", decision.NextPhase)
	}
}

func TestEvaluatePhaseTransaction_QueuesExecutionWhenPlanningArtifactIsReady(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	artifact := `{"summary":"Execution plan for fix failing tests","status":"ready_for_execution","plan":["inspect failing tests","patch root cause","rerun focused tests"]}`
	if err := ag.service.session.Memory.SaveWorkflowMemory("phase-artifact:planning", map[string]string{"artifact_json": artifact}); err != nil {
		t.Fatalf("failed to seed planning artifact: %v", err)
	}
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "planning",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, []ai.ToolCall{{Name: "planning_plan_changes"}}, []string{"Execution plan captured"}, "Execution plan captured with ready_for_execution status.", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected ready planning artifact to enqueue execution")
	}
	if decision.NextPhase != "execution" {
		t.Fatalf("expected next phase execution, got %q", decision.NextPhase)
	}
	queue := ag.getOrCreateSessionQueue()
	ag.enqueuePhaseTransactionDecision(classification.OriginalQuery, decision)
	snapshot := queue.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected exactly one queued continuation, got %d", len(snapshot))
	}
	if snapshot[0].Actor != "phase:execution" {
		t.Fatalf("expected queued actor phase:execution, got %q", snapshot[0].Actor)
	}
	if got, _ := snapshot[0].Context["controller_guidance"].(string); !strings.Contains(got, "persisted execution-ready plan artifact") {
		t.Fatalf("expected controller guidance to describe the persisted plan handoff, got %q", got)
	}
	if got, _ := snapshot[0].Context["reason"].(string); !strings.Contains(strings.ToLower(got), "ready_for_execution") {
		t.Fatalf("expected reason to mention ready planning artifact, got %q", got)
	}
}

func TestMaybePersistPlanningArtifactFromFinalText_EnablesExecutionTransition(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: &RunnerSession{Memory: NewShortTermMemory()}}
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "planning",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests in the repository",
	}
	finalText := strings.Join([]string{
		"Execution-ready plan to fix the current repo's failing tests:",
		"1. Reproduce and isolate the failure",
		"2. Inspect the implicated source and tests",
		"3. Apply the smallest correct fix",
	}, "\n")

	ag.maybePersistPlanningArtifactFromFinalText(classification, finalText, nil)
	if !ag.hasExecutionReadyPlanningArtifact() {
		t.Fatal("expected plain-text execution-ready plan to persist a ready_for_execution planning artifact")
	}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, nil, nil, finalText, nil)
	if !decision.EnqueueNextPhase || decision.NextPhase != "execution" {
		t.Fatalf("expected persisted planning artifact inferred from final text to enqueue execution, got %+v", decision)
	}
}

func TestEvaluatePhaseTransaction_RequeuesExecutionWhenValidationEvidenceIsMissing(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "execution",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		ExecutionReady:     true,
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, []ai.ToolCall{{Name: "execution_apply_patch"}}, []string{"Patch applied"}, "", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected execution without validation evidence to continue execution")
	}
	if decision.NextPhase != "execution" {
		t.Fatalf("expected next phase execution, got %q", decision.NextPhase)
	}
}

func TestEvaluatePhaseTransaction_UsesCapabilityCompletionForNonCodingDomain(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	classification := &AskTaskContext{
		Domains:       []string{"stores"},
		TaskShape:     "read_data",
		Phase:         "execution",
		PhasePlan:     []string{"investigation", "execution"},
		Intent:        "read_data",
		OriginalQuery: "list inventory for the active store",
	}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, []ai.ToolCall{{Name: "list_stores"}}, nil, "", nil)
	if !decision.EnqueueNextPhase {
		t.Fatal("expected non-coding execution to continue until the capability confirms completion")
	}
	if decision.NextPhase != "execution" {
		t.Fatalf("expected next phase execution, got %q", decision.NextPhase)
	}
	if !strings.Contains(strings.ToLower(decision.Reason), "incomplete") && !strings.Contains(strings.ToLower(decision.Reason), "continue") {
		t.Fatalf("expected generic continuation reason for incomplete non-coding step, got %q", decision.Reason)
	}
}

func TestEvaluatePhaseTransaction_AllowsExecutionCompletionWhenValidationEvidenceExists(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "execution",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		ExecutionReady:     true,
		OriginalQuery:      "fix failing tests in the repository",
	}

	decision := ag.evaluatePhaseTransaction(context.Background(), classification.OriginalQuery, classification, []ai.ToolCall{{Name: "execution_apply_patch"}, {Name: "execution_run_tests"}}, []string{"Patch applied", "tests passed"}, "All tests passed.", nil)
	if decision.EnqueueNextPhase {
		t.Fatalf("expected validation evidence to allow execution completion, got %+v", decision)
	}
}

func TestEvaluatePhaseTransaction_DoesNotUseClarificationReplyAsContinuationReason(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	classification := &AskTaskContext{
		Domains:            []string{"coding_workflow"},
		TaskShape:          taskShapeIssueInvestigation,
		OrchestrationLevel: orchestrationManagedObjective,
		Phase:              "investigation",
		PhasePlan:          []string{"investigation", "planning", "execution"},
		Intent:             "generate_code",
		OriginalQuery:      "fix failing tests",
	}
	ctx := context.WithValue(context.Background(), "session_payload", &ai.SessionPayload{
		ClarificationState: &ai.ClarificationState{
			TargetQuery:       "fix failing tests",
			AssistantQuestion: "Which repository or working directory should I inspect for the failing tests?",
			UserClarification: "current repo",
			Status:            "resolved",
		},
	})

	decision := ag.evaluatePhaseTransaction(ctx, "current repo", classification, []ai.ToolCall{{Name: "run_repo_tests"}}, []string{"--- FAIL: TestAuthenticateUser_ValidCredentials"}, "", nil)
	if !decision.EnqueueNextPhase || decision.NextPhase != "planning" {
		t.Fatalf("expected failing-test investigation to enqueue planning, got %+v", decision)
	}
	if strings.Contains(strings.ToLower(decision.Reason), "current repo") {
		t.Fatalf("expected continuation reason to describe the phase handoff, got %q", decision.Reason)
	}
	if !strings.Contains(strings.ToLower(decision.Reason), "failing-test investigation") {
		t.Fatalf("expected explanatory continuation reason, got %q", decision.Reason)
	}
}
