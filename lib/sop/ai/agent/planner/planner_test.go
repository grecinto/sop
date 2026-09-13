package planner

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestPlannerCapability_RegistersAsPrimaryCodingPlanner(t *testing.T) {
	cap := NewPlannerCapability(nil)
	result := cap.Classify("fix the parser bug in the repository", capabilities.RoutingState{CapabilityHint: "planner"})

	if !result.Relevant {
		t.Fatalf("expected planner capability to match coding workflow requests")
	}
	if result.Mode != capabilities.CapabilityModePrimary {
		t.Fatalf("expected primary mode, got %v", result.Mode)
	}
}

func TestPlannerCapabilityBuildGuidanceProfileUsesLightGuidance(t *testing.T) {
	cap := NewPlannerCapability(nil)
	profile := cap.BuildGuidanceProfile("plan a parser refactor", capabilities.RoutingState{CapabilityHint: "planner"})
	if profile.Intensity != capabilities.GuidanceIntensityLight {
		t.Fatalf("expected light guidance intensity, got %q", profile.Intensity)
	}
	if len(profile.ToolSelection) == 0 {
		t.Fatal("expected planner capability to provide tool-selection guidance")
	}
}

func TestPlannerCapabilityBuildContext_UsesDiscoveryImplementationLanguage(t *testing.T) {
	cap := NewPlannerCapability(nil)
	contextText := cap.BuildContext("fix failing tests", capabilities.RoutingState{CapabilityHint: "planner"})
	for _, check := range []string{"discovery", "implementation", "continuous Ask/ReAct transaction"} {
		if !strings.Contains(contextText, check) {
			t.Fatalf("expected planner context to include %q, got %q", check, contextText)
		}
	}
	if strings.Contains(contextText, "full SDLC") {
		t.Fatalf("did not expect planner context to keep legacy full SDLC guidance, got %q", contextText)
	}
}

func TestCodingPhaseExecutionBuildContext_PromotesSameTurnPivotToPreviewPatch(t *testing.T) {
	phase := newCodingPhase("execution", nil)
	contextText := phase.BuildContext("fix failing tests", capabilities.RoutingState{CapabilityHint: "execution"})
	for _, check := range []string{"SAME-TURN PIVOT RULE", "preview_patch immediately", "Do not wait for a separate phase handoff or queue item"} {
		if !strings.Contains(contextText, check) {
			t.Fatalf("expected execution context to include %q, got %q", check, contextText)
		}
	}
}

func TestPlannerCapabilityBuildInstructions_AvoidsMandatoryPhasePlanForCoding(t *testing.T) {
	cap := NewPlannerCapability(nil)
	instructions := cap.BuildInstructions("fix failing tests", capabilities.RoutingState{CapabilityHint: "planner"})
	for _, check := range []string{"do not force a full SDLC phase plan", "two-mode runtime", "create_phase_plan only when"} {
		if !strings.Contains(instructions, check) {
			t.Fatalf("expected planner instructions to include %q, got %q", check, instructions)
		}
	}
}

func TestShouldPreferSemanticRouting_RepoCentricRepairQueries(t *testing.T) {
	for _, query := range []string{
		"fix failing tests in the parser package",
		"this repo is broken after the refactor",
		"repair the project regression",
		"debug the failing codebase",
	} {
		if !shouldPreferSemanticRouting(query, capabilities.RoutingState{}) {
			t.Fatalf("expected repo-centric repair query to prefer semantic routing: %q", query)
		}
	}
	if shouldPreferSemanticRouting("hello there", capabilities.RoutingState{}) {
		t.Fatal("expected benign greeting to avoid semantic-first routing")
	}
}

func TestBasePhaseBuildInstructionsUsesAuthoringProfileOverrides(t *testing.T) {
	base := newBasePhase("planning", "Planning", "Plan work", "coding_workflow", nil)
	base.setAuthoringProfile(PhaseAuthoringProfile{
		Domain: "coding_workflow",
		Instructions: map[string]string{
			"planning": "Use the authored planning instruction",
		},
	})

	if got := base.BuildInstructions("plan a parser refactor", capabilities.RoutingState{}); got != "Use the authored planning instruction" {
		t.Fatalf("expected authored planning instruction, got %q", got)
	}
}

func TestBasePhaseBuildGuidanceProfileUsesAuthoringProfileRules(t *testing.T) {
	base := newBasePhase("planning", "Planning", "Plan work", "coding_workflow", nil)
	base.setAuthoringProfile(PhaseAuthoringProfile{
		Domain:     "coding_workflow",
		Principles: []string{"Prefer authored principles"},
		PhaseRules: map[string][]capabilities.GuidanceRule{
			"planning": {{Description: "Prefer authored phase guidance", AppliesTo: []string{"planning"}}},
		},
	})

	profile := base.BuildGuidanceProfile("plan a parser refactor", capabilities.RoutingState{})
	if len(profile.PhaseRules) == 0 {
		t.Fatal("expected authored phase rules to be present")
	}
	if got := profile.PhaseRules[0].Description; got != "Prefer authored phase guidance" {
		t.Fatalf("expected authored phase guidance rule, got %q", got)
	}
	if !containsStringInTest(profile.Principles, "Prefer authored principles") {
		t.Fatalf("expected authored principle to be included, got %#v", profile.Principles)
	}
}

func TestDesktopPhaseRunExecuteCommand_UsesSharedInfraExecutor(t *testing.T) {
	var received map[string]any
	phase := newDesktopPhase("execution", &cw.SharedInfrastructure{LocalCommandExecutor: func(ctx context.Context, args map[string]any) (string, error) {
		received = args
		return "bounded output", nil
	}})
	output, err := phase.runExecuteCommand(context.Background(), map[string]any{"command": "go test ./..."})
	if err != nil {
		t.Fatalf("expected command execution to succeed, got %v", err)
	}
	if output != "bounded output" {
		t.Fatalf("expected shared executor result, got %q", output)
	}
	if got, _ := received["command"].(string); got != "go test ./..." {
		t.Fatalf("expected command to be forwarded to shared executor, got %#v", received)
	}
	if got, _ := received["reasoning"].(string); strings.TrimSpace(got) == "" {
		t.Fatalf("expected desktop phase to provide reasoning to shared executor, got %#v", received)
	}
}

func TestStoresPhaseRunDesignQuery_FormatsStorePlan(t *testing.T) {
	phase := newStoresPhase("planning", nil)
	output, err := phase.runDesignQuery(map[string]any{"database": "orders", "store": "customers"})
	if err != nil {
		t.Fatalf("expected query design to succeed, got %v", err)
	}
	if !strings.Contains(output, "orders") || !strings.Contains(output, "customers") {
		t.Fatalf("expected plan to mention the database and store, got %q", output)
	}
}

type staticStructuredPlanner struct {
	plan cw.StructuredPlan
}

func (p *staticStructuredPlanner) Plan(task cw.Task, state cw.RunState, pack cw.ContextPack) (cw.StructuredPlan, error) {
	return p.plan, nil
}

type captureStructuredPlanner struct {
	plan         cw.StructuredPlan
	observedPack cw.ContextPack
}

func (p *captureStructuredPlanner) Plan(task cw.Task, state cw.RunState, pack cw.ContextPack) (cw.StructuredPlan, error) {
	p.observedPack = pack
	return p.plan, nil
}

type plannerTestContextManager struct {
	pack cw.ContextPack
}

func (m plannerTestContextManager) BuildContext(task cw.Task, state cw.RunState) (cw.ContextPack, error) {
	pack := m.pack
	if pack.WorkingDir == "" {
		pack.WorkingDir = task.WorkingDir
	}
	if pack.ActiveFile == "" {
		pack.ActiveFile = task.ActiveFile
	}
	if state.Metadata != nil {
		if summary := strings.TrimSpace(state.Metadata["continuation_summary"]); summary != "" {
			pack.ContinuationSummary = summary
			pack.ResumedFromState = true
		}
	}
	return pack, nil
}

func TestStoresPhaseRunDesignQuery_UsesSharedStructuredPlannerWhenAvailable(t *testing.T) {
	phase := newStoresPhase("planning", &cw.SharedInfrastructure{Planner: &staticStructuredPlanner{plan: cw.StructuredPlan{
		Objective:     "Plan store repair",
		WorkflowShape: "repair",
		Domain:        "stores",
		Steps:         []cw.StructuredPlanStep{{StepID: "step_1", Goal: "Inspect store shape", ActionInstruction: "Inspect the target store and validate the expected query behavior."}},
	}}})
	output, err := phase.runDesignQuery(map[string]any{"database": "orders", "store": "customers"})
	if err != nil {
		t.Fatalf("expected structured planner path to succeed, got %v", err)
	}
	if !strings.Contains(output, "Inspect the target store and validate the expected query behavior.") {
		t.Fatalf("expected output to use shared structured planner, got %q", output)
	}
}

func TestCodingPhaseRunPlanChanges_UsesStoredPhaseContinuationSummary(t *testing.T) {
	planner := &captureStructuredPlanner{plan: cw.StructuredPlan{
		Objective:     "Plan repo repair",
		WorkflowShape: "repair",
		Domain:        "coding_workflow",
		Steps:         []cw.StructuredPlanStep{{StepID: "step_1", Goal: "Investigate failure", ActionInstruction: "Inspect the exact failing test, identify the root cause, and trace the dependency path through the responsible code path."}},
	}}
	infra := &cw.SharedInfrastructure{
		ContextManager: plannerTestContextManager{pack: cw.ContextPack{WorkingDir: "/tmp/demo", TestFiles: []string{"parser_test.go"}}},
		Planner:        planner,
		Memory:         cw.NewInMemorySessionMemory(),
	}
	if err := infra.Memory.Save("phase-handoff:planning", map[string]string{"continuation_summary": "=== Phase Transaction Handoff ===\nGrounded outcome facts:\n- --- FAIL: TestParserRejectsInvalidInput"}); err != nil {
		t.Fatalf("save continuation summary: %v", err)
	}
	phase := newCodingPhase("planning", infra)

	output, err := phase.runPlanChanges(context.Background(), map[string]any{"query": "plan parser validation improvements", "working_dir": "/tmp/demo"})
	if err != nil {
		t.Fatalf("expected planning to succeed, got %v", err)
	}
	if !strings.Contains(output, "identify the root cause") {
		t.Fatalf("expected structured plan output, got %q", output)
	}
	if !strings.Contains(planner.observedPack.ContinuationSummary, "TestParserRejectsInvalidInput") {
		t.Fatalf("expected planner to receive stored continuation summary, got %#v", planner.observedPack)
	}
	if !planner.observedPack.ResumedFromState {
		t.Fatalf("expected planner context pack to mark resumed state, got %#v", planner.observedPack)
	}
}

func TestDiagnosisInstrumentation_TracksRepairTasksWithoutDiagnosis(t *testing.T) {
	summary := "Fix failing parser regression caused by invalid token handling"
	plan := []string{"Investigate the failing parser test", "Patch the parser state machine"}
	evidence := []string{"TestParserRejectsInvalidInput failed with unexpected token"}

	if !isRepairTaskForDiagnosis(summary, plan, evidence) {
		t.Fatal("expected repair classification to trigger for failing-code tasks")
	}

	missing := diagnosisFieldsMissing(map[string]any{"fix_target": "code"}, nil)
	if len(missing) == 0 || !containsString(missing, "root_cause") || !containsString(missing, "evidence") {
		t.Fatalf("expected missing diagnosis fields to include root_cause and evidence, got %#v", missing)
	}
}

func TestDiagnosisFieldsMissing_TreatsUnresolvedPlaceholderAsMissing(t *testing.T) {
	structuredData := map[string]any{
		"diagnosis": map[string]any{
			"root_cause": "Auth-path tests are failing due to a previously identified local defect, but the exact file/function-level target is not recoverable from the currently visible state preview.",
			"fix_target": map[string]any{
				"note":   "A prior turn identified a concrete fix target, but it is not present in the recoverable preview returned by state.",
				"status": "unresolved_in_current_turn",
				"value":  nil,
			},
			"shared_with":  []any{"auth test cluster"},
			"blast_radius": map[string]any{"scope": "local to auth path"},
		},
	}

	missing := diagnosisFieldsMissing(structuredData, []string{"auth test failure evidence"})
	if !containsString(missing, "root_cause") {
		t.Fatalf("expected placeholder root_cause text to be treated as missing, got %#v", missing)
	}
	if !containsString(missing, "fix_target") {
		t.Fatalf("expected unresolved fix_target placeholder to be treated as missing, got %#v", missing)
	}
}

func TestPhaseBPreviewPatchWarnsWhenDiagnosisMissing(t *testing.T) {
	missing := diagnosisFieldsMissing(map[string]any{"fix_target": "code"}, nil)
	if len(missing) == 0 || !containsString(missing, "root_cause") || !containsString(missing, "evidence") {
		t.Fatalf("expected missing diagnosis fields to be reported by the soft-gate helper, got %#v", missing)
	}
	if !isRepairTaskForDiagnosis("fix the failing parser regression", nil, []string{"parser test failed"}) {
		t.Fatal("expected repair detection for failing task")
	}
}

func TestPhaseCPreviewPatchRequiresDiagnosisForRepairTasks(t *testing.T) {
	phase := newCodingPhase("execution", nil)
	_, err := phase.runPreviewPatch(context.Background(), map[string]any{
		"query":       "fix the failing parser regression",
		"working_dir": ".",
		"file_path":   "pkg/parser.go",
		"old_content": "func Parse() {}",
		"new_content": "func Parse() { return nil }",
	})
	if err == nil {
		t.Fatal("expected hard gate error when repair task lacks diagnosis metadata")
	}
	if !strings.Contains(err.Error(), "DIAGNOSIS REQUIRED") || !strings.Contains(err.Error(), "root_cause") {
		t.Fatalf("expected diagnosis requirement in error, got %v", err)
	}
}

func TestPhaseDRequiresSharedCauseAndBlastRadiusInDiagnosis(t *testing.T) {
	phase := newCodingPhase("execution", nil)
	_, err := phase.runPreviewPatch(context.Background(), map[string]any{
		"query":       "fix the failing parser regression",
		"working_dir": ".",
		"file_path":   "pkg/parser.go",
		"old_content": "func Parse() {}",
		"new_content": "func Parse() { return nil }",
		"structured_data": map[string]any{
			"diagnosis": map[string]any{
				"root_cause": "invalid token handling in parser",
				"evidence":   []string{"TestParserRejectsInvalidInput failed"},
				"fix_target": "code",
			},
		},
	})
	if err == nil {
		t.Fatal("expected Phase D check to require shared_with and blast_radius in diagnosis")
	}
	if !strings.Contains(err.Error(), "shared_with") || !strings.Contains(err.Error(), "blast_radius") {
		t.Fatalf("expected shared_with and blast_radius in hard gate error, got %v", err)
	}
}

func TestClusterFailureEvidenceGroupsSharedFailures(t *testing.T) {
	clusters := clusterFailureEvidence([]string{
		"TestParserRejectsInvalidInput failed with unexpected token",
		"TestParserRejectsNullInput failed with unexpected token",
		"TestConfigLoadsDefaults passed",
	})
	if len(clusters) == 0 {
		t.Fatal("expected at least one failure cluster")
	}
	if len(clusters[0]["members"].([]string)) < 2 {
		t.Fatalf("expected shared failure cluster to include multiple members, got %#v", clusters[0])
	}
}

func TestCodingPhaseBuildArtifactInventory_UsesContextSignals(t *testing.T) {
	phase := newCodingPhase("investigation", nil)
	pack := cw.ContextPack{
		ActiveFile:        "pkg/parser.go",
		RepositoryFiles:   []string{"pkg/parser.go", "pkg/parser_test.go", "pkg/config.yaml", "pkg/interfaces/parser_interface.go", "README.md"},
		RelatedFiles:      []string{"pkg/parser.go"},
		DependencyFiles:   []string{"pkg/parser_test.go"},
		TestFiles:         []string{"pkg/parser_test.go"},
		Symbols:           []string{"Parse"},
		ASTLeafNodes:      []string{"Parse"},
		StructuralPath:    "repo/pkg/Parse",
		RepositorySummary: "module=demo",
	}

	snapshot := phase.buildArtifactInventory(cw.Task{ActiveFile: "pkg/parser.go", Description: "analyze parser"}, pack)
	if snapshot == nil {
		t.Fatal("expected inventory snapshot")
	}
	if got := snapshot["inventory_type"]; got != "code_artifacts" {
		t.Fatalf("expected code_artifacts inventory, got %#v", got)
	}
	if got := snapshot["objective"]; got != "analyze parser" {
		t.Fatalf("expected objective to be captured on inventory, got %#v", got)
	}
	if got := snapshot["objective_profile"]; got == "" {
		t.Fatalf("expected objective profile to be set on inventory, got %#v", got)
	}
	items, ok := snapshot["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected items slice, got %#v", snapshot["items"])
	}
	if len(items) == 0 {
		t.Fatal("expected inventory items")
	}
	if activeID, _ := snapshot["active_item_id"].(string); activeID == "" {
		t.Fatal("expected active item id to be set")
	}
	var sawFile, sawTest, sawConfig, sawSymbol, sawCluster, sawInterface bool
	for _, item := range items {
		switch item["type"] {
		case "interface":
			if item["display_name"] == "pkg/interfaces/parser_interface.go" {
				sawInterface = true
			}
		case "file":
			if item["display_name"] == "pkg/parser.go" {
				sawFile = true
			}
		case "test":
			if item["display_name"] == "pkg/parser_test.go" {
				sawTest = true
			}
		case "config":
			if item["display_name"] == "pkg/config.yaml" {
				sawConfig = true
			}
		case "symbol":
			if item["display_name"] == "Parse" {
				sawSymbol = true
			}
		case "cluster":
			if item["display_name"] == "pkg" {
				sawCluster = true
			}
		}
	}
	if !sawFile || !sawTest || !sawConfig || !sawSymbol || !sawCluster || !sawInterface {
		t.Fatalf("expected file/test/config/symbol inventory items, got %#v", items)
	}
}

func TestCodingPhaseBuildArtifactInventory_AddsRelationshipGroups(t *testing.T) {
	phase := newCodingPhase("investigation", nil)
	pack := cw.ContextPack{
		ActiveFile:        "pkg/parser.go",
		RepositoryFiles:   []string{"pkg/parser.go", "pkg/interfaces/parser_interface.go", "pkg/parser_test.go"},
		RelatedFiles:      []string{"pkg/parser.go"},
		DependencyFiles:   []string{"pkg/interfaces/parser_interface.go"},
		TestFiles:         []string{"pkg/parser_test.go"},
		Symbols:           []string{"Parse"},
		ASTLeafNodes:      []string{"Parse"},
		StructuralPath:    "repo/pkg/Parse",
		RepositorySummary: "module=demo",
	}

	snapshot := phase.buildArtifactInventory(cw.Task{ActiveFile: "pkg/parser.go", Description: "analyze parser"}, pack)
	items := snapshotItems(snapshot)
	var sawRelationship bool
	for _, item := range items {
		if item["type"] == "relationship" && item["display_name"] == "parser" {
			sawRelationship = true
			if metadata, ok := item["metadata"].(map[string]any); ok {
				files, ok := metadata["related_files"].([]string)
				if !ok || len(files) < 2 {
					t.Fatalf("expected relationship item to include multiple related files, got %#v", metadata)
				}
			}
		}
	}
	if !sawRelationship {
		t.Fatalf("expected relationship inventory item for parser, got %#v", items)
	}
}

func TestCodingPhaseBuildArtifactInventory_PrioritizesPackageScope(t *testing.T) {
	phase := newCodingPhase("investigation", nil)
	pack := cw.ContextPack{
		ActiveFile:        "pkg/parser.go",
		RepositoryFiles:   []string{"pkg/parser.go", "pkg/parser_test.go", "pkg/parser_helper.go"},
		RelatedFiles:      []string{"pkg/parser.go"},
		DependencyFiles:   []string{"pkg/parser_helper.go"},
		TestFiles:         []string{"pkg/parser_test.go"},
		Symbols:           []string{"Parse"},
		ASTLeafNodes:      []string{"Parse"},
		StructuralPath:    "repo/pkg/Parse",
		RepositorySummary: "module=demo",
	}

	snapshot := phase.buildArtifactInventory(cw.Task{ActiveFile: "pkg/parser.go", Description: "analyze parser"}, pack)
	items := snapshotItems(snapshot)
	var sawPackageScope bool
	for _, item := range items {
		if item["type"] == "package" && item["display_name"] == "pkg" {
			sawPackageScope = true
			if metadata, ok := item["metadata"].(map[string]any); ok {
				files, ok := metadata["files"].([]string)
				if !ok || len(files) < 2 {
					t.Fatalf("expected package scope item to include multiple files, got %#v", metadata)
				}
			}
		}
	}
	if !sawPackageScope {
		t.Fatalf("expected package-scoped inventory item for pkg, got %#v", items)
	}
}

func TestCodingPhaseBuildArtifactInventory_TracksExecutionSurfaceEdges(t *testing.T) {
	phase := newCodingPhase("investigation", nil)
	pack := cw.ContextPack{
		ActiveFile:        "pkg/parser.go",
		RepositoryFiles:   []string{"pkg/parser.go", "pkg/parser_helper.go", "pkg/parser_test.go", "pkg/config.yaml"},
		RelatedFiles:      []string{"pkg/parser.go"},
		DependencyFiles:   []string{"pkg/parser_helper.go"},
		TestFiles:         []string{"pkg/parser_test.go"},
		Symbols:           []string{"Parse"},
		ASTLeafNodes:      []string{"Parse"},
		StructuralPath:    "repo/pkg/Parse",
		RepositorySummary: "module=demo",
	}

	snapshot := phase.buildArtifactInventory(cw.Task{ActiveFile: "pkg/parser.go", Description: "analyze parser"}, pack)
	items := snapshotItems(snapshot)
	var sawExecutionSurface bool
	for _, item := range items {
		if item["type"] == "execution_surface" {
			sawExecutionSurface = true
			metadata, ok := item["metadata"].(map[string]any)
			if !ok {
				t.Fatalf("expected execution surface metadata, got %#v", item)
			}
			deps, _ := metadata["dependency_files"].([]string)
			tests, _ := metadata["test_files"].([]string)
			if len(deps) == 0 || len(tests) == 0 {
				t.Fatalf("expected dependency and test files on execution surface, got %#v", metadata)
			}
		}
	}
	if !sawExecutionSurface {
		t.Fatalf("expected execution surface inventory item, got %#v", items)
	}
}

func TestResolveTaskActiveFileFromInventory_UsesSelectedArtifactWhenExplicitFileMissing(t *testing.T) {
	snapshot := map[string]any{
		"active_item_id": "test:pkg/parser_test.go",
		"items": []map[string]any{
			{"id": "file:pkg/parser.go", "type": "file", "display_name": "pkg/parser.go", "status": "pending", "metadata": map[string]any{"file_path": "pkg/parser.go"}},
			{"id": "test:pkg/parser_test.go", "type": "test", "display_name": "pkg/parser_test.go", "status": "active", "metadata": map[string]any{"file_path": "pkg/parser_test.go"}},
		},
	}

	got := resolveTaskActiveFileFromInventory(cw.Task{Description: "fix parser bug"}, snapshot)
	if got != "pkg/parser_test.go" {
		t.Fatalf("expected active inventory file to be used as target, got %q", got)
	}

	got = resolveTaskActiveFileFromInventory(cw.Task{ActiveFile: "pkg/other.go", Description: "fix parser bug"}, snapshot)
	if got != "pkg/other.go" {
		t.Fatalf("expected explicit task file to win over inventory, got %q", got)
	}
}

func TestCodingPhaseBuildTask_UsesActiveInventoryFileWhenNoActiveFileProvided(t *testing.T) {
	phaseArtifacts := map[string]map[string]any{
		"investigation": {
			"artifact_inventory": map[string]any{
				"active_item_id": "file:pkg/parser.go",
				"items": []map[string]any{
					{"id": "file:pkg/parser.go", "type": "file", "display_name": "pkg/parser.go", "status": "active", "metadata": map[string]any{"file_path": "pkg/parser.go"}},
					{"id": "test:pkg/parser_test.go", "type": "test", "display_name": "pkg/parser_test.go", "status": "pending", "metadata": map[string]any{"file_path": "pkg/parser_test.go"}},
				},
			},
		},
	}
	phase := newCodingPhase("investigation", &cw.SharedInfrastructure{STMAccessor: func() cw.STMReader {
		return &phaseSTMReader{thread: &phaseWorkStreamReader{ctx: &phaseWorkStreamContext{phaseArtifacts: phaseArtifacts}}}
	}})

	task := phase.buildTask(context.Background(), map[string]any{"query": "fix parser bug"})
	if task.ActiveFile != "pkg/parser.go" {
		t.Fatalf("expected buildTask to default to active inventory file, got %q", task.ActiveFile)
	}
}

type phaseSTMReader struct {
	thread cw.WorkStreamReader
}

func (r *phaseSTMReader) GetCurrentThread() cw.WorkStreamReader { return r.thread }
func (r *phaseSTMReader) CreatePhasePlan(phases []string, checkpoints map[string]bool, reasoning, confidence string) error {
	return nil
}
func (r *phaseSTMReader) StoreMultiStepPlan(planData any) error { return nil }

type phaseWorkStreamReader struct {
	ctx cw.WorkStreamContextReader
}

func (r *phaseWorkStreamReader) GetStreamContext() cw.WorkStreamContextReader { return r.ctx }

type phaseWorkStreamContext struct {
	phaseArtifacts map[string]map[string]any
}

func (c *phaseWorkStreamContext) GetPhasePlan() cw.PhasePlanReader           { return nil }
func (c *phaseWorkStreamContext) SetPhasePlan(cw.PhasePlanReader)            {}
func (c *phaseWorkStreamContext) GetStructuredPlan() cw.StructuredPlanReader { return nil }
func (c *phaseWorkStreamContext) SetStructuredPlan(cw.StructuredPlanReader)  {}
func (c *phaseWorkStreamContext) GetMultiStepPlan() cw.MultiStepPlanReader   { return nil }
func (c *phaseWorkStreamContext) SetMultiStepPlan(cw.MultiStepPlanReader)    {}
func (c *phaseWorkStreamContext) GetPhaseArtifacts() map[string]map[string]any {
	return c.phaseArtifacts
}

func TestArtifactInventorySelectionAndCompletion_AdvancesActiveItem(t *testing.T) {
	snapshot := map[string]any{
		"inventory_type": "code_artifacts",
		"active_item_id": "file:pkg/parser.go",
		"items": []map[string]any{
			{"id": "file:pkg/parser.go", "type": "file", "display_name": "pkg/parser.go", "status": "active", "metadata": map[string]any{"file_path": "pkg/parser.go"}},
			{"id": "test:pkg/parser_test.go", "type": "test", "display_name": "pkg/parser_test.go", "status": "pending", "metadata": map[string]any{"file_path": "pkg/parser_test.go"}},
			{"id": "config:pkg/config.yaml", "type": "config", "display_name": "pkg/config.yaml", "status": "pending", "metadata": map[string]any{"file_path": "pkg/config.yaml"}},
		},
	}

	if !selectArtifactInventoryItem(snapshot, "pkg/parser_test.go") {
		t.Fatal("expected selectArtifactInventoryItem to match by display name")
	}
	items := snapshotItems(snapshot)
	if got := snapshot["active_item_id"]; got != "test:pkg/parser_test.go" {
		t.Fatalf("expected active item id to move to test item, got %#v", got)
	}
	for _, item := range items {
		if item["id"] == "test:pkg/parser_test.go" && item["status"] != "active" {
			t.Fatalf("expected selected item to be active, got %#v", item)
		}
	}

	if !completeArtifactInventoryItem(snapshot, "test:pkg/parser_test.go") {
		t.Fatal("expected completeArtifactInventoryItem to mark the item complete")
	}
	if got := snapshot["active_item_id"]; got != "file:pkg/parser.go" {
		t.Fatalf("expected next pending item to become active, got %#v", got)
	}
	items = snapshotItems(snapshot)
	var sawCompleted, sawNextActive bool
	for _, item := range items {
		switch item["id"] {
		case "test:pkg/parser_test.go":
			if item["status"] != "completed" {
				t.Fatalf("expected completed item status, got %#v", item)
			}
			sawCompleted = true
		case "file:pkg/parser.go":
			if item["status"] != "active" {
				t.Fatalf("expected next item to be active, got %#v", item)
			}
			sawNextActive = true
		}
	}
	if !sawCompleted || !sawNextActive {
		t.Fatalf("expected completion to advance to next item, got %#v", items)
	}
}

func TestSyncWorkInventoryFromTestResult_CreatesItemsFromRealFailures(t *testing.T) {
	phaseArtifacts := map[string]map[string]any{}
	phase := newCodingPhase("execution", &cw.SharedInfrastructure{STMAccessor: func() cw.STMReader {
		return &phaseSTMReader{thread: &phaseWorkStreamReader{ctx: &phaseWorkStreamContext{phaseArtifacts: phaseArtifacts}}}
	}})

	output := "--- FAIL: TestAuthenticateUser_ValidCredentials (0.00s)\n" +
		"    auth_test.go:42: Authenticate() = false, want true\n" +
		"FAIL\tgithub.com/sharedcode/sop/tools/sopbackend\t0.012s\n"

	phase.syncWorkInventoryFromTestResult(cw.TestResult{Passed: false, Output: output})

	_, snapshot, err := phase.loadArtifactInventorySnapshot()
	if err != nil {
		t.Fatalf("expected snapshot load to succeed, got %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected a compiled inventory snapshot from real test failures")
	}
	items := snapshotItems(snapshot)
	if len(items) != 1 {
		t.Fatalf("expected exactly one failing test item, got %#v", items)
	}
	if items[0]["id"] != "test:TestAuthenticateUser_ValidCredentials" {
		t.Fatalf("expected item id to match failing test name, got %#v", items[0])
	}
	if items[0]["status"] != "pending" && items[0]["status"] != "active" {
		t.Fatalf("expected new failing test item to be pending or active, got %#v", items[0])
	}

	// Once the test passes, the item should be marked completed automatically.
	phase.syncWorkInventoryFromTestResult(cw.TestResult{Passed: true, Output: "ok  \tgithub.com/sharedcode/sop/tools/sopbackend\t0.010s\n"})
	_, snapshot, err = phase.loadArtifactInventorySnapshot()
	if err != nil {
		t.Fatalf("expected snapshot load to succeed, got %v", err)
	}
	items = snapshotItems(snapshot)
	if len(items) != 1 || items[0]["status"] != "completed" {
		t.Fatalf("expected the previously failing test item to be completed, got %#v", items)
	}
}

func TestSpacesPhaseRunDesignMutation_UsesSharedStructuredPlannerWhenAvailable(t *testing.T) {
	phase := newSpacesPhase("planning", &cw.SharedInfrastructure{Planner: &staticStructuredPlanner{plan: cw.StructuredPlan{
		Objective:     "Plan space mutation",
		WorkflowShape: "repair",
		Domain:        "spaces",
		Steps:         []cw.StructuredPlanStep{{StepID: "step_1", Goal: "Inspect knowledge scope", ActionInstruction: "Inspect the target space scope and define the intended knowledge change."}},
	}}})
	output, err := phase.runDesignMutation(map[string]any{"space_name": "products", "category": "pricing"})
	if err != nil {
		t.Fatalf("expected spaces structured planner path to succeed, got %v", err)
	}
	if !strings.Contains(output, "Inspect the target space scope and define the intended knowledge change.") {
		t.Fatalf("expected output to use shared structured planner, got %q", output)
	}
}

func containsStringInTest(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestPlanningPhaseCapability_UsesPlannerPackage(t *testing.T) {
	cap := NewPlanningPhaseCapability(nil)
	if cap == nil {
		t.Fatal("expected planning phase capability")
	}
	if cap.ID() != "phase_planning" {
		t.Fatalf("expected phase_planning id, got %q", cap.ID())
	}
}

func TestPlanningPhaseCapabilityBuildGuidanceProfileUsesPhaseRules(t *testing.T) {
	cap := NewPlanningPhaseCapability(nil)
	provider, ok := cap.(capabilities.StructuredGuidanceProvider)
	if !ok {
		t.Fatal("expected planning phase capability to expose structured guidance")
	}
	profile := provider.BuildGuidanceProfile("plan a parser refactor", capabilities.RoutingState{})
	if len(profile.PhaseRules) == 0 {
		t.Fatal("expected planning phase capability to provide phase rules")
	}
	if profile.Intensity != capabilities.GuidanceIntensityLight {
		t.Fatalf("expected light guidance intensity, got %q", profile.Intensity)
	}
}

func TestInferPreferredModelScheme_UsesStrongSchemeForPlanningDesignTasks(t *testing.T) {
	preference, taskType := inferPreferredModelScheme("design a new API and plan rollout", "planning", "hard", "medium")
	if preference != "strong" {
		t.Fatalf("expected strong preference for planning/design tasks, got %q", preference)
	}
	if taskType != "reasoning" {
		t.Fatalf("expected reasoning task type for planning/design tasks, got %q", taskType)
	}
}

func TestBuildDetailedExecutionPlan_AddsFilesValidationAndExitCriteria(t *testing.T) {
	steps := buildDetailedExecutionPlan(
		cw.Task{Description: "fix parser bug", ActiveFile: "lib/parser.go", TaskType: "implementation"},
		cw.ContextPack{ActiveFile: "lib/parser.go", RelatedFiles: []string{"lib/parser.go", "lib/parser_test.go"}, TestFiles: []string{"lib/parser_test.go"}, DependencyFiles: []string{"lib/errors.go"}},
		[]string{"inspect parser", "update parser", "validate change"},
	)

	if len(steps) != 3 {
		t.Fatalf("expected 3 detailed steps, got %d", len(steps))
	}
	if got := steps[0].Files; len(got) == 0 || got[0] != "lib/parser.go" {
		t.Fatalf("expected first step to include the active file, got %#v", got)
	}
	if len(steps[2].Validation) == 0 {
		t.Fatalf("expected validation guidance for the final step, got %#v", steps[2].Validation)
	}
	if joined := strings.Join(steps[2].ExitCriteria, " "); !strings.Contains(strings.ToLower(joined), "tests") {
		t.Fatalf("expected exit criteria to mention validation, got %#v", steps[2].ExitCriteria)
	}
}

func TestDefaultPlanner_PlanReturnsStructuredPlan(t *testing.T) {
	planner := &cw.DefaultPlanner{}
	plan, err := planner.Plan(
		cw.Task{Description: "fix failing tests in parser", ActiveFile: "lib/parser.go", TaskType: "implementation"},
		cw.RunState{},
		cw.ContextPack{ActiveFile: "lib/parser.go", RelatedFiles: []string{"lib/parser.go", "lib/parser_test.go"}, TestFiles: []string{"lib/parser_test.go"}, DependencyFiles: []string{"lib/errors.go"}, CodeStructureSummary: "parser uses a token stream", ModuleName: "parser"},
	)
	if err != nil {
		t.Fatalf("expected structured planner to succeed, got %v", err)
	}
	if plan.Domain != "coding_workflow" {
		t.Fatalf("expected coding_workflow domain, got %#v", plan)
	}
	if len(plan.Steps) == 0 {
		t.Fatalf("expected structured plan steps, got %#v", plan)
	}
	if len(cw.BasePlanFromStructuredPlan(plan)) == 0 {
		t.Fatalf("expected structured plan to project into legacy steps, got %#v", plan)
	}
	if got := plan.SystemContext.PrimaryTargets; len(got) == 0 || got[0] != "lib/parser.go" {
		t.Fatalf("expected primary targets in structured system context, got %#v", got)
	}
	if got := plan.CompletionGate.Requirements; len(got) == 0 {
		t.Fatalf("expected completion gate requirements, got %#v", got)
	}
}

func TestBuildExecutionGuidanceFromArtifact_UsesStructuredPlan(t *testing.T) {
	artifact := phaseArtifact{
		PhaseID: "planning",
		StructuredData: map[string]any{
			"detailed_execution_plan": []map[string]any{{
				"title":         "Step 1",
				"description":   "inspect parser",
				"files":         []string{"lib/parser.go"},
				"validation":    []string{"run parser tests"},
				"exit_criteria": []string{"tests pass"},
			}},
		},
	}

	guidance := buildExecutionGuidanceFromArtifact(artifact)
	if !strings.Contains(guidance, "inspect parser") {
		t.Fatalf("expected guidance to include the detailed step, got %q", guidance)
	}
	if !strings.Contains(guidance, "run parser tests") {
		t.Fatalf("expected guidance to include validation guidance, got %q", guidance)
	}
}

func TestBuildExecutionGuidanceFromArtifact_UsesMiniPlan(t *testing.T) {
	artifact := phaseArtifact{
		PhaseID: "planning",
		StructuredData: map[string]any{
			"phase_mini_plan": map[string]any{
				"objective":           "add parser validation",
				"architecture_design": []string{"keep the change localized"},
				"functional_features": []string{"inspect parser", "add validation"},
				"pseudo_logic":        []string{"read parser token stream"},
				"file_plan": []map[string]any{{
					"file": "lib/parser.go",
					"role": "primary implementation file",
				}},
			},
		},
	}

	guidance := buildExecutionGuidanceFromArtifact(artifact)
	if !strings.Contains(guidance, "add parser validation") {
		t.Fatalf("expected guidance to include objective, got %q", guidance)
	}
	if !strings.Contains(guidance, "primary implementation file") {
		t.Fatalf("expected guidance to include file plan role, got %q", guidance)
	}
}

func TestBuildPhasePlanSequence_DefaultsToDiscoveryPlanningExecution(t *testing.T) {
	plan := buildPhasePlanSequence("fix failing tests in the repository")
	joined := strings.Join(plan, ",")
	if joined != "investigation,planning,execution" {
		t.Fatalf("expected default coding phase sequence to be investigation,planning,execution, got %v", plan)
	}
}

func TestBuildPhasePlanSequence_ValidationOnlyUsesInvestigationExecution(t *testing.T) {
	plan := buildPhasePlanSequence("validate the parser behavior after the fix")
	joined := strings.Join(plan, ",")
	if joined != "investigation,execution" {
		t.Fatalf("expected validation-oriented sequence to be investigation,execution, got %v", plan)
	}
}

func TestPlanningPhaseBuildContext_UsesDiscoveryModeLanguage(t *testing.T) {
	cap := NewPlanningPhaseCapability(nil)
	contextBuilder, ok := cap.(interface {
		BuildContext(string, capabilities.RoutingState) string
	})
	if !ok {
		t.Fatal("expected planning phase capability to expose BuildContext")
	}
	contextText := contextBuilder.BuildContext("plan a parser fix", capabilities.RoutingState{})
	for _, check := range []string{"Discovery mode", "Continue directly into implementation"} {
		if !strings.Contains(contextText, check) {
			t.Fatalf("expected planning build context to include %q, got %q", check, contextText)
		}
	}
	if strings.Contains(contextText, "PLANNING-FIRST: Call planning_assess_and_route FIRST") {
		t.Fatalf("did not expect legacy planning-first instruction, got %q", contextText)
	}
}

func TestBuildPhaseMiniPlan_ProducesStructuredArtifacts(t *testing.T) {
	task := cw.Task{Description: "add parser validation for malformed input", ActiveFile: "lib/parser.go", TaskType: "implementation"}
	pack := cw.ContextPack{ActiveFile: "lib/parser.go", RelatedFiles: []string{"lib/parser.go", "lib/parser_test.go"}, TestFiles: []string{"lib/parser_test.go"}, DependencyFiles: []string{"lib/errors.go"}, CodeStructureSummary: "parser uses a token stream", RepositorySummary: "Go package"}

	miniPlan := buildPhaseMiniPlan(task, pack, []string{"inspect parser", "update parser", "validate change"})
	if got, ok := miniPlan["objective"].(string); !ok || !strings.Contains(strings.ToLower(got), "parser") {
		t.Fatalf("expected objective to mention the task, got %#v", miniPlan["objective"])
	}
	if design, ok := miniPlan["architecture_design"].([]string); !ok || len(design) == 0 {
		t.Fatalf("expected architecture design guidance, got %#v", miniPlan["architecture_design"])
	}
	if features, ok := miniPlan["functional_features"].([]string); !ok || len(features) == 0 {
		t.Fatalf("expected functional features, got %#v", miniPlan["functional_features"])
	}
	if pseudo, ok := miniPlan["pseudo_logic"].([]string); !ok || len(pseudo) == 0 {
		t.Fatalf("expected pseudo logic steps, got %#v", miniPlan["pseudo_logic"])
	}
}

func TestBuildTaskRoutingMetadata_AddsClassificationFields(t *testing.T) {
	routing := buildTaskRoutingMetadata("run go test ./...", "mechanical", "easy", "low")
	if routing["complexity"] != "easy" {
		t.Fatalf("expected easy complexity, got %#v", routing["complexity"])
	}
	if routing["task_type"] != "mechanical" {
		t.Fatalf("expected mechanical task type, got %#v", routing["task_type"])
	}
	preferred := routing["preferred_model"].(string)
	if preferred == "" || !strings.Contains(preferred, ":") {
		t.Fatalf("expected a concrete provider:model recommendation, got %#v", routing["preferred_model"])
	}
	if got, _ := routing["expected_verification"].(string); !strings.Contains(got, "go test") {
		t.Fatalf("expected verification guidance, got %#v", routing["expected_verification"])
	}
}

func TestBuildTaskRoutingMetadata_PrefersStrongerModelForMediaAnalysis(t *testing.T) {
	routing := buildTaskRoutingMetadata("analyze this video clip and summarize the key moments", "implementation", "hard", "high")
	if got, _ := routing["preferred_model"].(string); got == "" || !strings.Contains(got, ":") {
		t.Fatalf("expected a concrete provider:model recommendation for media analysis, got %#v", got)
	}
	if got, _ := routing["media_required"].(bool); !got {
		t.Fatalf("expected media_required to be true for media analysis, got %#v", routing["media_required"])
	}
	if got, _ := routing["media_types"].([]string); len(got) == 0 || got[0] != "video" {
		t.Fatalf("expected media_types to include video, got %#v", routing["media_types"])
	}
}

func TestInferCodingSituation_ClassifiesTaskFamilies(t *testing.T) {
	bugFix := inferCodingSituation("fix the parser bug that drops malformed input")
	if bugFix.ID != "bug_fix" {
		t.Fatalf("expected bug_fix situation, got %q", bugFix.ID)
	}
	if bugFix.InvestigationFocus == "" {
		t.Fatalf("expected investigation focus for bug fix, got %#v", bugFix)
	}

	feature := inferCodingSituation("add a new API endpoint for exports")
	if feature.ID != "new_feature" {
		t.Fatalf("expected new_feature situation, got %q", feature.ID)
	}
	if feature.DesignFocus == "" {
		t.Fatalf("expected design focus for new feature, got %#v", feature)
	}
}

func TestBuildPhaseMiniPlan_ProducesDesignBriefForDesignRequests(t *testing.T) {
	miniPlan := buildPhaseMiniPlan(cw.Task{Description: "design a new API for the platform", ActiveFile: "lib/api.go", TaskType: "implementation"}, cw.ContextPack{CodeStructureSummary: "service layer", RepositorySummary: "Go package"}, []string{"define request contract", "wire handler"})
	designBrief, ok := miniPlan["design_brief"].(map[string]any)
	if !ok || len(designBrief) == 0 {
		t.Fatalf("expected design brief in mini plan, got %#v", miniPlan["design_brief"])
	}
	if got, _ := designBrief["problem_statement"].(string); got == "" {
		t.Fatalf("expected problem_statement in design brief, got %#v", designBrief)
	}
	if got, _ := designBrief["recommended_approach"].(string); got == "" {
		t.Fatalf("expected recommended_approach in design brief, got %#v", designBrief)
	}
}

func TestBuildPhaseMiniPlan_ProducesInvestigationBriefForBugFixRequests(t *testing.T) {
	miniPlan := buildPhaseMiniPlan(cw.Task{Description: "fix the parser bug that drops malformed input", ActiveFile: "lib/parser.go", TaskType: "implementation"}, cw.ContextPack{CodeStructureSummary: "parser uses a token stream", RelatedFiles: []string{"lib/parser.go", "lib/parser_test.go"}, TestFiles: []string{"lib/parser_test.go"}, DependencyFiles: []string{"lib/errors.go"}}, []string{"inspect parser", "trace failure path", "patch bug"})
	investigationBrief, ok := miniPlan["investigation_brief"].(map[string]any)
	if !ok || len(investigationBrief) == 0 {
		t.Fatalf("expected investigation brief in mini plan, got %#v", miniPlan["investigation_brief"])
	}
	if got, _ := investigationBrief["investigation_focus"].(string); got == "" {
		t.Fatalf("expected investigation_focus in investigation brief, got %#v", investigationBrief)
	}
	if got, _ := investigationBrief["analysis_questions"].(string); got == "" {
		t.Fatalf("expected analysis_questions in investigation brief, got %#v", investigationBrief)
	}
}

func TestBuildPhaseMiniPlan_IncludesRoutingMetadata(t *testing.T) {
	miniPlan := buildPhaseMiniPlan(cw.Task{Description: "add parser validation", ActiveFile: "lib/parser.go", TaskType: "implementation"}, cw.ContextPack{}, []string{"add validation"})
	routing, ok := miniPlan["routing"].(map[string]any)
	if !ok || len(routing) == 0 {
		t.Fatalf("expected routing metadata in mini plan, got %#v", miniPlan["routing"])
	}
	if got, _ := routing["preferred_model"].(string); got == "" {
		t.Fatalf("expected preferred model to be populated, got %#v", routing["preferred_model"])
	}
}

func TestBuildPhasePlanSequence_UsesActiveCodingPhasesOnly(t *testing.T) {
	sequence := buildPhasePlanSequence("implement a parser fix with regression coverage")
	if len(sequence) != 3 {
		t.Fatalf("expected active coding sequence to contain only investigation, planning, execution, got %#v", sequence)
	}
	if sequence[0] != "investigation" || sequence[1] != "planning" || sequence[2] != "execution" {
		t.Fatalf("expected active coding sequence to be investigation,planning,execution, got %#v", sequence)
	}
	for _, phase := range sequence {
		if phase == "testing" || phase == "review" || phase == "delivery" {
			t.Fatalf("did not expect legacy phase %q in the active coding sequence, got %#v", phase, sequence)
		}
	}
}

func TestNewTestingPhaseCapability_ExposesTestingPhase(t *testing.T) {
	cap := NewTestingPhaseCapability(nil)
	if cap == nil {
		t.Fatal("expected testing phase capability")
	}
	if cap.ID() != "phase_testing" {
		t.Fatalf("expected phase_testing id, got %q", cap.ID())
	}
}
