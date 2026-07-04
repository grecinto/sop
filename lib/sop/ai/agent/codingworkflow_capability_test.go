package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/inmemory"
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

func TestShouldFlushLTMBufferedWritesUsesWindowOrCount(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if shouldFlushLTMBufferedWrites(now, time.Time{}, 1, 0) {
		t.Fatal("expected empty last-flush time to defer flushing")
	}
	if !shouldFlushLTMBufferedWrites(now, now.Add(-6*time.Minute), 1, 0) {
		t.Fatal("expected time-window flush after the configured interval")
	}
	if !shouldFlushLTMBufferedWrites(now, now.Add(-1*time.Minute), LTMFlushMaxCUDItems, 0) {
		t.Fatal("expected count-threshold flush when the pending CUD count reaches the max")
	}
	if !shouldFlushLTMBufferedWrites(now, now.Add(-1*time.Minute), 0, LTMFlushMaxReadItems) {
		t.Fatal("expected count-threshold flush when the pending read count reaches the max")
	}
}

func TestQueueLTMBufferedWriteFlushesAtThreshold(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)
	agent.ltmFlushLastFlush = time.Now().Add(-time.Minute)

	var executed int
	for i := 0; i < LTMFlushMaxCUDItems; i++ {
		if err := agent.queueLTMBufferedWrite("cud", func(context.Context) error {
			executed++
			return nil
		}); err != nil {
			t.Fatalf("queue returned error: %v", err)
		}
	}
	if executed == 0 {
		t.Fatal("expected buffered LTM write to flush when the count threshold is reached")
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

func TestFormatLearnedKnowledgeHitUsesCanonicalDocumentSourceLabel(t *testing.T) {
	ctx := context.Background()
	cats := inmemory.NewBtree[sop.UUID, *memory.Category](true)
	vecs := inmemory.NewBtree[memory.VectorKey, memory.Vector](true)
	items := inmemory.NewBtree[memory.ItemKey, memory.Item[map[string]any]](true)

	store := memory.NewStore[map[string]any](
		"learned_knowledge_label_kb",
		nil,
		cats.Btree,
		inmemory.NewBtree[string, sop.UUID](false).Btree,
		inmemory.NewBtree[memory.DistanceKey, byte](false).Btree,
		vecs.Btree,
		items.Btree,
		inmemory.NewBtree[sop.UUID, memory.Document](false).Btree,
	)

	docID := sop.NewUUID()
	if err := store.UpsertDocument(ctx, memory.Document{ID: docID, Title: "parser regression fix", Source: "coding_workflow", ContentType: "text/plain", Content: "canonical content"}); err != nil {
		t.Fatalf("failed to store canonical document: %v", err)
	}

	kb := &memory.KnowledgeBase[map[string]any]{Store: store}
	hit := memory.KBDigestHit{Score: 0.9, Category: "Architecture", Text: "patched parser", DocID: memory.DocIDs{docID.String()}}
	formatted := formatLearnedKnowledgeHit(ctx, kb, hit)
	if !strings.Contains(formatted, "Source: parser regression fix") {
		t.Fatalf("expected learned knowledge prompt to use canonical document title as source, got %q", formatted)
	}
}

func TestBuildCodingMemoryLTMPayloadBuildsCanonicalDocumentAndTwoIndexes(t *testing.T) {
	mem := cw.CodingMemory{
		Description:    "fix parser",
		ActiveFile:     "parser.go",
		FilesTouched:   []string{"parser.go"},
		Outcome:        "patched parser",
		TestStatus:     "passed",
		Lesson:         "keep the parser behavior focused",
		NextStep:       "add a regression test",
		RepositoryHint: "sop-cloud",
		StructuralPath: "sop-cloud/ai/parser/Parse",
		ASTLeafNodes:   []string{"Parse", "ParseExpression"},
	}

	payload, entries := buildCodingMemoryLTMPayloadAndIndexes(mem)
	if payload["document_id"] == "" {
		t.Fatal("expected canonical document id to be populated")
	}
	if got := payload["path_index"]; got == "" {
		t.Fatal("expected path index to be populated")
	}
	if got := payload["semantic_index"]; got == "" {
		t.Fatal("expected semantic index to be populated")
	}
	if got := payload["code_structure_index"]; got == "" {
		t.Fatal("expected code_structure index to be populated")
	}
	if len(entries) != 3 {
		t.Fatalf("expected three index entries (path, code_structure, semantic), got %d", len(entries))
	}
	if entries[0].CategoryPath == entries[1].CategoryPath && entries[1].CategoryPath == entries[2].CategoryPath {
		t.Fatal("expected the three index routes to differ")
	}
	if entries[0].Payload["document_id"] != entries[1].Payload["document_id"] {
		t.Fatal("expected both index entries to point at the same canonical document")
	}
	canonical, ok := entries[0].Payload["canonical_payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected canonical payload map, got %T", entries[0].Payload["canonical_payload"])
	}
	if canonical["description"] != "fix parser" {
		t.Fatalf("expected canonical payload to preserve description, got %#v", canonical["description"])
	}
}

func TestBuildCodingMemoryLTMThoughtsAttachesCanonicalDocumentReference(t *testing.T) {
	mem := cw.CodingMemory{
		Description:  "fix parser",
		Outcome:      "patched parser",
		FilesTouched: []string{"parser.go"},
		TestStatus:   "passed",
	}
	payload, entries := buildCodingMemoryLTMPayloadAndIndexes(mem)
	thoughts := buildCodingMemoryLTMThoughts(mem, payload["document_id"].(string), entries)
	if len(thoughts) != 3 {
		t.Fatalf("expected three thoughts (path, code_structure, semantic), got %d", len(thoughts))
	}
	for _, thought := range thoughts {
		if len(thought.DocID) != 1 || thought.DocID[0] != payload["document_id"].(string) {
			t.Fatalf("expected thought to carry canonical document id, got %+v", thought.DocID)
		}
		if thought.Data["document_id"] != payload["document_id"].(string) {
			t.Fatalf("expected thought data to preserve document id, got %#v", thought.Data["document_id"])
		}
	}
}

func TestShouldVectorizeLTMIndexEntryOnlyForSemanticKind(t *testing.T) {
	tests := []struct {
		name  string
		entry codingMemoryLTMIndexEntry
		want  bool
	}{
		{name: "semantic", entry: codingMemoryLTMIndexEntry{Kind: "semantic"}, want: true},
		{name: "path", entry: codingMemoryLTMIndexEntry{Kind: "path"}, want: false},
		{name: "code_structure", entry: codingMemoryLTMIndexEntry{Kind: "code_structure"}, want: false},
		{name: "case-insensitive", entry: codingMemoryLTMIndexEntry{Kind: "SEMANTIC"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldVectorizeLTMIndexEntry(tt.entry); got != tt.want {
				t.Fatalf("shouldVectorizeLTMIndexEntry(%q) = %v, want %v", tt.entry.Kind, got, tt.want)
			}
		})
	}
}

func TestBuildCodingMemoryLTMPayloadStoresLifecycleMetadataForRepoState(t *testing.T) {
	mem := cw.CodingMemory{Description: "fix parser", Outcome: "patched parser", RepositoryHint: "sop-cloud"}
	payload, entries := buildCodingMemoryLTMPayloadAndIndexes(mem)
	thoughts := buildCodingMemoryLTMThoughts(mem, payload["document_id"].(string), entries)
	if len(thoughts) == 0 {
		t.Fatal("expected thoughts to be generated")
	}
	for _, key := range []string{"lifecycle_status", "status", "scope_type", "scope_id"} {
		if _, ok := thoughts[0].Data[key]; ok {
			t.Fatalf("expected item payload to omit %s, got %#v", key, thoughts[0].Data[key])
		}
	}
	canonical, ok := entries[0].Payload["canonical_payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected canonical payload map, got %T", entries[0].Payload["canonical_payload"])
	}
	if canonical["lifecycle_status"] != "active" {
		t.Fatalf("expected canonical repo payload to persist lifecycle_status, got %#v", canonical["lifecycle_status"])
	}
	if canonical["scope_type"] != "repo" {
		t.Fatalf("expected canonical repo payload to persist scope_type, got %#v", canonical["scope_type"])
	}
	if canonical["scope_id"] != "sop-cloud" {
		t.Fatalf("expected canonical repo payload to persist scope_id, got %#v", canonical["scope_id"])
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
