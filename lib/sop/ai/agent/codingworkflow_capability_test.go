package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestWithCodingWorkflowCapabilityRegistersCapability(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	orchestrator := &cw.Orchestrator{}

	WithCodingWorkflowCapability(orchestrator)(agent)

	cap, ok := agent.capabilityRegistry.Resolve("fix this bug in the repository", capabilities.RoutingState{Domain: "Desktop"})
	if !ok {
		t.Fatal("expected coding workflow capability to be registered")
	}
	if cap.ID() != cw.CapabilityID {
		t.Fatalf("expected capability id %q, got %q", cw.CapabilityID, cap.ID())
	}
}

func TestWithCodingWorkflowCapabilityWiresGitAdapter(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	orchestrator := &cw.Orchestrator{}

	WithCodingWorkflowCapability(orchestrator)(agent)

	inspector, ok := orchestrator.ChangedFileInspector.(*cw.GitRepoInspector)
	if !ok {
		t.Fatalf("expected changed file inspector to be a git inspector, got %T", orchestrator.ChangedFileInspector)
	}
	if inspector.Client == nil {
		t.Fatal("expected git inspector to be wired with a git client")
	}
}

func TestWithCodingWorkflowCapabilityWiresMemorySinkToAgentSession(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.service = &Service{session: NewRunnerSession()}
	orchestrator := &cw.Orchestrator{}

	WithCodingWorkflowCapability(orchestrator)(agent)
	if orchestrator.MemorySink == nil {
		t.Fatal("expected coding workflow capability to wire a memory sink")
	}

	orchestrator.MemorySink(cw.CodingMemory{Description: "fix parser", Outcome: "patched parser", FilesTouched: []string{"parser.go"}, TestStatus: "passed"})
	if agent.service.session.Memory == nil {
		t.Fatal("expected coding workflow result to create session memory")
	}
	if len(agent.service.session.Memory.RuntimeCaveats) == 0 {
		t.Fatal("expected coding workflow result to add a runtime caveat")
	}
	if _, ok := agent.GetMRUCategory("CODING_WORKFLOW_LAST"); !ok {
		t.Fatal("expected coding workflow result to populate MRU")
	}
}

func TestBuildSystemPromptIncludesCodingWorkflowMemory(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.service = &Service{session: NewRunnerSession()}
	agent.recordCodingMemory(cw.CodingMemory{Description: "fix parser", Outcome: "patched parser", FilesTouched: []string{"parser.go"}, TestStatus: "passed"})

	prompt := agent.buildSystemPrompt(context.Background(), "continue the fix", TaskContextClassification{})
	if !strings.Contains(prompt, "Coding Workflow Memory") {
		t.Fatalf("expected prompt to include explicit coding workflow memory section, got %s", prompt)
	}
	if !strings.Contains(prompt, "patched parser") {
		t.Fatalf("expected prompt to include coding workflow outcome, got %s", prompt)
	}
}

func TestRecordCodingMemoryStoresPlanInSTMAndMRU(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.service = &Service{session: NewRunnerSession()}
	mem := cw.CodingMemory{
		Description:  "fix parser",
		Outcome:      "patched parser",
		FilesTouched: []string{"parser.go"},
		TestStatus:   "passed",
		Plan: []string{
			"edit active file parser.go",
			"focus on symbols Parse",
			"inspect dependency file(s): helper.go",
			"review related tests: parser_test.go",
		},
	}

	agent.recordCodingMemory(mem)
	if agent.service.session.Memory == nil {
		t.Fatal("expected coding workflow memory to create short-term memory")
	}
	if len(agent.service.session.Memory.RuntimeCaveats) == 0 {
		t.Fatal("expected coding workflow memory to create a runtime caveat")
	}

	var details string
	for _, caveat := range agent.service.session.Memory.RuntimeCaveats {
		if caveat.Summary == "fix parser" {
			details = caveat.Details
			break
		}
	}
	if !strings.Contains(details, "edit active file parser.go") {
		t.Fatalf("expected STM details to retain the full plan, got %q", details)
	}
	if !strings.Contains(details, "review related tests: parser_test.go") {
		t.Fatalf("expected STM details to retain the full checklist, got %q", details)
	}

	ctx, ok := agent.GetMRUCategory("CODING_WORKFLOW_LAST")
	if !ok {
		t.Fatal("expected coding workflow MRU category to be populated")
	}
	if !strings.Contains(ctx, "edit active file parser.go") {
		t.Fatalf("expected MRU context to include the leading plan item, got %q", ctx)
	}
	if !strings.Contains(ctx, "focus on symbols Parse") {
		t.Fatalf("expected MRU context to include a compact plan summary, got %q", ctx)
	}
}

func TestRehydrateMRUFromMemoryRestoresCodingWorkflowContext(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.service = &Service{session: NewRunnerSession()}
	agent.service.session.Memory = NewShortTermMemory()
	agent.service.session.Memory.AddRuntimeCaveat("fix parser", "plan=edit active file parser.go | focus on symbols Parse", "coding_workflow", "coding_workflow", "coding_workflow")

	agent.rehydrateMRUFromMemory(context.Background())
	ctx, ok := agent.GetMRUCategory("CODING_WORKFLOW_LAST")
	if !ok {
		t.Fatal("expected coding workflow context to be rehydrated from STM")
	}
	if !strings.Contains(ctx, "edit active file parser.go") {
		t.Fatalf("expected rehydrated MRU context to include the stored plan, got %q", ctx)
	}
}

func TestCodingWorkflowExecutionStatePersistsInSTM(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.service = &Service{session: NewRunnerSession()}
	state := &CodingWorkflowExecutionState{ActiveFile: "parser.go", Description: "fix parser", Plan: []string{"edit active file parser.go"}, Symbols: []string{"Parse"}}
	agent.service.session.Memory.SetCodingWorkflowState(state)

	restored := agent.service.session.Memory.GetCodingWorkflowState()
	if restored == nil {
		t.Fatal("expected coding workflow state to be stored")
	}
	if restored.ActiveFile != "parser.go" {
		t.Fatalf("expected active file to be restored, got %q", restored.ActiveFile)
	}
	if !containsStringSlice(restored.Plan, "edit active file parser.go") {
		t.Fatalf("expected plan to be restored, got %v", restored.Plan)
	}
}

func containsStringSlice(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestCodingWorkflowCapabilityExposesContinuitySignals(t *testing.T) {
	orchestrator := &cw.Orchestrator{}
	cap := cw.NewCapability(orchestrator)

	result, err := cap.Run("fix parser", capabilities.RoutingState{Domain: "Desktop"})
	if err != nil {
		t.Fatalf("expected capability run to succeed in the current working directory context: %v", err)
	}
	if result.Summary == "" {
		t.Fatal("expected capability run to return a summary")
	}
	if got, ok := result.Data["continuity_signal"]; !ok || got != "task_outcome" {
		t.Fatalf("expected continuity signal payload to be exposed, got %#v", got)
	}
}

func TestCodingWorkflowCapabilityHandlesGenerationAndAnalysisQueries(t *testing.T) {
	cap := cw.NewCapability(&cw.Orchestrator{})
	if !cap.CanHandle("generate a JSON script for a store workflow", capabilities.RoutingState{Domain: "Desktop"}) {
		t.Fatal("expected capability to handle generation-style requests")
	}
	if !cap.CanHandle("analyze table records to find anomalies", capabilities.RoutingState{Domain: "Desktop"}) {
		t.Fatal("expected capability to handle analysis-style requests")
	}
}

func TestOrchestratorExecutionResultIncludesTaskClassificationAndPlan(t *testing.T) {
	tmpDir := t.TempDir()
	orchestrator := &cw.Orchestrator{ContextManager: &cw.DefaultContextManager{}, Planner: &cw.DefaultPlanner{}, RepoIndexer: &cw.DefaultRepoIndexer{}, PatchEngine: &cw.DefaultPatchEngine{}, ChangedFileInspector: &cw.GitRepoInspector{}, DiffPreviewer: &cw.DefaultDiffPreviewer{}, RepoSearcher: &cw.FileSearchAdapter{}, RepoInspector: &cw.GitRepoInspector{}, Memory: cw.NewInMemorySessionMemory(), Policy: &cw.DefaultSafetyPolicy{}, TestRunner: &cw.DefaultTestRunner{}}
	result, err := orchestrator.Execute(cw.Task{WorkingDir: tmpDir, Description: "analyze store inventory and propose a SQL-based cleanup plan", State: cw.RunState{Metadata: map[string]string{"continuation_summary": "continue from prior analysis"}}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.TaskType != "analysis" {
		t.Fatalf("expected analysis task type, got %q", result.TaskType)
	}
	if !result.ResumedFromState {
		t.Fatal("expected resumed-from-state flag to be set when continuation metadata is present")
	}
	if len(result.Plan) == 0 {
		t.Fatal("expected execution result to include a plan")
	}
}
