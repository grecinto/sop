package planner

import (
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

func TestPlanningPhaseCapability_UsesPlannerPackage(t *testing.T) {
	cap := NewPlanningPhaseCapability(nil)
	if cap == nil {
		t.Fatal("expected planning phase capability")
	}
	if cap.ID() != "phase_planning" {
		t.Fatalf("expected phase_planning id, got %q", cap.ID())
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

func TestBuildPhasePlanSequence_IncludesTestingPhase(t *testing.T) {
	sequence := buildPhasePlanSequence("implement a parser fix with regression coverage")
	if len(sequence) < 7 {
		t.Fatalf("expected at least 7 phases, got %d", len(sequence))
	}
	if sequence[3] != "testing" {
		t.Fatalf("expected testing to appear before execution, got %#v", sequence)
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
