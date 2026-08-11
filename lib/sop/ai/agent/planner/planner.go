package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

const (
	PlannerCapabilityID   = "planner"
	PlannerCapabilityName = "Planner"
)

// PlannerCapability is the cold-start planner entry point for coding tasks.
// It creates a persisted PhasePlan that the phase capabilities execute.
type PlannerCapability struct {
	sharedInfra *cw.SharedInfrastructure
}

func NewPlannerCapability(sharedInfra *cw.SharedInfrastructure) *PlannerCapability {
	if sharedInfra == nil {
		sharedInfra = cw.NewDefaultSharedInfrastructure()
	}
	return &PlannerCapability{sharedInfra: sharedInfra}
}

func (c *PlannerCapability) ID() string   { return PlannerCapabilityID }
func (c *PlannerCapability) Name() string { return PlannerCapabilityName }

func (c *PlannerCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{{
			Resource: "memory",
			Actions:  []string{"read", "write"},
			Gate:     capabilities.GateReadAccess,
		}},
	}
}

func (c *PlannerCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil {
		return capabilities.ClassificationResult{}
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		switch hint {
		case "phase_plan_planner", "coding_workflow", "codingworkflow", "repo_edit", "planner":
			return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModePrimary}
		}
	}
	// Domain-agnostic: Planner works for all domains (coding, stores, spaces, desktop, etc.)
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return capabilities.ClassificationResult{}
	}
	// Planning signals across all domains
	for _, signal := range []string{"fix", "debug", "bug", "refactor", "patch", "implement", "modify", "edit", "repo", "repository", "code", "parser", "test", "feature", "design", "plan", "architect", "workflow", "phases", "steps"} {
		if strings.Contains(q, signal) {
			return capabilities.ClassificationResult{Relevant: true, Score: 16, Mode: capabilities.CapabilityModePrimary}
		}
	}
	return capabilities.ClassificationResult{}
}

func (c *PlannerCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Universal phase planner context:\n- Use this planner for tasks that benefit from structured workflow (coding, stores, spaces, desktop automation, etc.).\n- Create a persistent PhasePlan that adapts to task complexity: simple queries may need only 1-2 phases, complex tasks need full workflow.\n- Phase sequence examples: [investigation, execution] for queries, [intake, investigation, planning, execution, review, delivery] for full SDLC.\n- The domain determines which tools are available in each phase."
}

func (c *PlannerCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "For new coding tasks, start by creating a PhasePlan with the create_phase_plan tool before taking implementation actions. Persist the plan into the current work stream so the later phase capabilities can continue the workflow.\n\n" + cw.CodeFormatInstructions
}

func (c *PlannerCapability) BuildGuidanceProfile(query string, state capabilities.RoutingState) capabilities.GuidanceProfile {
	base := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityLight, "Guide workflow planning safely and clearly")
	base.Principles = []string{"Prefer phased planning for work that spans multiple steps or validation checkpoints"}
	base.Guardrails = []capabilities.GuidanceRule{{Description: "Do not skip planning for architecture changes, significant validation, or multi-file edits"}}

	profile := capabilities.GuidanceProfile{
		Role:          "Guide structured workflow planning for multi-step tasks",
		Principles:    []string{"Use the simplest plan that still preserves the key checkpoints and risks"},
		ToolSelection: []capabilities.GuidanceRule{{Description: "Prefer create_phase_plan for high-level workflow orchestration and leave implementation detail to the later execution phases"}},
	}.ApplyDefaults(base)

	return profile
}

func (c *PlannerCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{
		Name:        "create_phase_plan",
		Description: "Creates a phase-based plan for a coding task and stores it in the current work stream.",
		Schema:      `{"type":"object","properties":{"query":{"type":"string","description":"The user request or task to plan"},"reasoning":{"type":"string","description":"Why this phase sequence was chosen"},"confidence":{"type":"string","description":"Confidence level for the plan"},"checkpoints":{"type":"array","items":{"type":"string"},"description":"Optional checkpoint names such as after_investigation"}},"required":["query"]}`,
	}}
}

func (c *PlannerCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	return nil
}

func (c *PlannerCapability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (c *PlannerCapability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	_ = ctx
	_ = query
	_ = state
	_ = result
	return nil
}

func (c *PlannerCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name != "create_phase_plan" {
		return "", fmt.Errorf("unsupported tool %q", toolName)
	}
	query := firstStringArg(args, "query", "task", "description")
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	reasoning := firstStringArg(args, "reasoning")
	if strings.TrimSpace(reasoning) == "" {
		reasoning = "Cold-start coding requests benefit from a structured progression through intake, investigation, planning, execution, review, and delivery."
	}
	confidence := firstStringArg(args, "confidence")
	if strings.TrimSpace(confidence) == "" {
		confidence = "medium"
	}
	checkpoints := collectCheckpointNames(args)
	if len(checkpoints) == 0 {
		checkpoints = []string{"after_investigation", "after_execution"}
	}
	checkpointMap := make(map[string]bool, len(checkpoints))
	for _, checkpoint := range checkpoints {
		checkpointMap[checkpoint] = true
	}
	phases := buildPhasePlanSequence(query)
	if c.sharedInfra != nil && c.sharedInfra.STMAccessor != nil {
		stm := c.sharedInfra.STMAccessor()
		if stm != nil {
			if err := stm.CreatePhasePlan(phases, checkpointMap, reasoning, confidence); err != nil {
				return "", fmt.Errorf("create phase plan: %w", err)
			}
		}
	}
	return fmt.Sprintf("✅ PHASE PLAN CREATED\n\nPlanned phases: %s\nCheckpoints: %s\nReasoning: %s\nConfidence: %s",
		strings.Join(phases, " → "),
		strings.Join(checkpoints, ", "),
		reasoning,
		confidence), nil
}

type detailedExecutionStep struct {
	Title        string
	Description  string
	Files        []string
	Validation   []string
	ExitCriteria []string
	Routing      map[string]any
}

func buildDetailedExecutionPlan(task cw.Task, pack cw.ContextPack, baseSteps []string) []detailedExecutionStep {
	if len(baseSteps) == 0 {
		return nil
	}
	steps := make([]detailedExecutionStep, 0, len(baseSteps))
	for idx, step := range baseSteps {
		if strings.TrimSpace(step) == "" {
			continue
		}
		files := make([]string, 0, 4)
		if task.ActiveFile != "" {
			files = append(files, task.ActiveFile)
		}
		for _, file := range pack.RelatedFiles {
			if file != "" && !containsString(files, file) {
				files = append(files, file)
			}
		}
		for _, file := range pack.DependencyFiles {
			if file != "" && !containsString(files, file) {
				files = append(files, file)
			}
		}
		if len(files) == 0 && task.ActiveFile != "" {
			files = append(files, task.ActiveFile)
		}

		validation := []string{"review the resulting diff"}
		if len(pack.TestFiles) > 0 {
			validation = append(validation, fmt.Sprintf("run or inspect relevant tests: %s", strings.Join(pack.TestFiles, ", ")))
		}
		if idx == len(baseSteps)-1 {
			validation = append(validation, "confirm the change satisfies the original request")
		}

		exitCriteria := []string{"change is documented in the plan artifact"}
		if len(pack.TestFiles) > 0 {
			exitCriteria = append(exitCriteria, "relevant tests pass or are explicitly accounted for")
		}
		if idx == len(baseSteps)-1 {
			exitCriteria = append(exitCriteria, "execution can hand off cleanly to review")
		}

		complexity := inferTaskComplexity(step)
		risk := inferTaskRisk(step)
		steps = append(steps, detailedExecutionStep{
			Title:        fmt.Sprintf("Step %d", idx+1),
			Description:  step,
			Files:        files,
			Validation:   validation,
			ExitCriteria: exitCriteria,
			Routing:      buildTaskRoutingMetadata(step, task.TaskType, complexity, risk),
		})
	}
	return steps
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func buildExecutionGuidanceFromArtifact(artifact phaseArtifact) string {
	if artifact.StructuredData == nil {
		return ""
	}

	lines := []string{"Execution guidance from planning artifact:"}

	if raw, ok := artifact.StructuredData["phase_mini_plan"]; ok && raw != nil {
		if miniPlan, ok := raw.(map[string]any); ok {
			if objective, _ := miniPlan["objective"].(string); strings.TrimSpace(objective) != "" {
				lines = append(lines, "- Objective: "+objective)
			}
			if designBrief, ok := miniPlan["design_brief"].(map[string]any); ok && len(designBrief) > 0 {
				if problem, _ := designBrief["problem_statement"].(string); strings.TrimSpace(problem) != "" {
					lines = append(lines, "- Problem: "+problem)
				}
				if constraints, _ := designBrief["constraints"].(string); strings.TrimSpace(constraints) != "" {
					lines = append(lines, "- Constraints: "+constraints)
				}
				if tradeoffs, _ := designBrief["trade_offs"].(string); strings.TrimSpace(tradeoffs) != "" {
					lines = append(lines, "- Trade-offs: "+tradeoffs)
				}
				if approach, _ := designBrief["recommended_approach"].(string); strings.TrimSpace(approach) != "" {
					lines = append(lines, "- Recommended approach: "+approach)
				}
			}
			if investigationBrief, ok := miniPlan["investigation_brief"].(map[string]any); ok && len(investigationBrief) > 0 {
				if focus, _ := investigationBrief["investigation_focus"].(string); strings.TrimSpace(focus) != "" {
					lines = append(lines, "- Investigation focus: "+focus)
				}
				if questions, _ := investigationBrief["analysis_questions"].(string); strings.TrimSpace(questions) != "" {
					lines = append(lines, "- Analysis questions: "+questions)
				}
				if evidence, _ := investigationBrief["evidence_to_collect"].(string); strings.TrimSpace(evidence) != "" {
					lines = append(lines, "- Evidence to collect: "+evidence)
				}
			}
			if design, ok := miniPlan["architecture_design"].([]string); ok && len(design) > 0 {
				lines = append(lines, "- Architecture/design: "+strings.Join(design, " | "))
			}
			if features, ok := miniPlan["functional_features"].([]string); ok && len(features) > 0 {
				lines = append(lines, "- Functional features: "+strings.Join(features, " | "))
			}
			if pseudo, ok := miniPlan["pseudo_logic"].([]string); ok && len(pseudo) > 0 {
				lines = append(lines, "- Pseudo logic: "+strings.Join(pseudo, " | "))
			}
			if filePlan, ok := miniPlan["file_plan"].([]map[string]any); ok && len(filePlan) > 0 {
				parts := make([]string, 0, len(filePlan))
				for _, entry := range filePlan {
					file, _ := entry["file"].(string)
					role, _ := entry["role"].(string)
					if strings.TrimSpace(file) != "" {
						parts = append(parts, fmt.Sprintf("%s (%s)", file, role))
					}
				}
				if len(parts) > 0 {
					lines = append(lines, "- File plan: "+strings.Join(parts, "; "))
				}
			}
			if routing, ok := miniPlan["routing"].(map[string]any); ok && len(routing) > 0 {
				lines = append(lines, formatRoutingGuidance(routing))
			}
			return strings.Join(lines, "\n")
		}
	}

	raw, ok := artifact.StructuredData["detailed_execution_plan"]
	if !ok || raw == nil {
		return ""
	}

	steps, ok := raw.([]map[string]any)
	if !ok {
		return ""
	}
	if len(steps) == 0 {
		return ""
	}

	for _, step := range steps {
		description, _ := step["description"].(string)
		if strings.TrimSpace(description) == "" {
			continue
		}
		lines = append(lines, "- "+description)
		if routing, ok := step["routing"].(map[string]any); ok && len(routing) > 0 {
			lines = append(lines, formatRoutingGuidance(routing))
		}
		if validation, ok := step["validation"].([]string); ok && len(validation) > 0 {
			lines = append(lines, "  validation: "+strings.Join(validation, "; "))
		}
		if exitCriteria, ok := step["exit_criteria"].([]string); ok && len(exitCriteria) > 0 {
			lines = append(lines, "  exit criteria: "+strings.Join(exitCriteria, "; "))
		}
	}
	return strings.Join(lines, "\n")
}

func buildPhaseMiniPlan(task cw.Task, pack cw.ContextPack, baseSteps []string) map[string]any {
	objective := fmt.Sprintf("Implement the requested change for %s", strings.TrimSpace(task.Description))
	if task.ActiveFile != "" {
		objective += fmt.Sprintf(" in %s", task.ActiveFile)
	}

	situation := inferCodingSituation(task.Description)

	architectureDesign := []string{}
	if situation.DesignFocus != "" {
		architectureDesign = append(architectureDesign, situation.DesignFocus)
	}
	if pack.CodeStructureSummary != "" {
		architectureDesign = append(architectureDesign, fmt.Sprintf("Align the change with the existing structure: %s", pack.CodeStructureSummary))
	}
	if pack.RepositorySummary != "" {
		architectureDesign = append(architectureDesign, fmt.Sprintf("Follow repository conventions: %s", pack.RepositorySummary))
	}
	if len(pack.DependencyFiles) > 0 {
		architectureDesign = append(architectureDesign, fmt.Sprintf("Review dependencies in %s", strings.Join(pack.DependencyFiles, ", ")))
	}
	if len(architectureDesign) == 0 {
		architectureDesign = append(architectureDesign, "Keep the change localized and testable")
	}

	functionalFeatures := []string{}
	for _, step := range baseSteps {
		if strings.TrimSpace(step) == "" {
			continue
		}
		functionalFeatures = append(functionalFeatures, step)
	}
	if len(functionalFeatures) == 0 {
		functionalFeatures = append(functionalFeatures, "inspect the affected code path")
	}

	pseudoLogic := []string{}
	if situation.InvestigationFocus != "" {
		pseudoLogic = append(pseudoLogic, situation.InvestigationFocus)
	}
	if task.ActiveFile != "" {
		pseudoLogic = append(pseudoLogic, fmt.Sprintf("Open %s and inspect the relevant logic", task.ActiveFile))
	}
	if len(pack.RelatedFiles) > 0 {
		pseudoLogic = append(pseudoLogic, fmt.Sprintf("Check related files: %s", strings.Join(pack.RelatedFiles, ", ")))
	}
	if len(pack.TestFiles) > 0 {
		pseudoLogic = append(pseudoLogic, fmt.Sprintf("Validate with tests: %s", strings.Join(pack.TestFiles, ", ")))
	}
	if len(pseudoLogic) == 0 {
		pseudoLogic = append(pseudoLogic, "Implement the smallest change that satisfies the request")
	}

	return map[string]any{
		"objective":           objective,
		"situation":           map[string]any{"id": situation.ID, "label": situation.Label, "description": situation.Description},
		"design_brief":        buildDesignBrief(task, pack, baseSteps),
		"investigation_brief": buildInvestigationBrief(task, pack, baseSteps),
		"architecture_design": architectureDesign,
		"functional_features": functionalFeatures,
		"pseudo_logic":        pseudoLogic,
		"file_plan":           buildFilePlan(task, pack, baseSteps),
		"routing":             buildTaskRoutingMetadata(task.Description, task.TaskType, inferTaskComplexity(strings.Join(baseSteps, " ")), inferTaskRisk(strings.Join(baseSteps, " "))),
	}
}

func formatRoutingGuidance(routing map[string]any) string {
	if len(routing) == 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	if taskType, _ := routing["task_type"].(string); strings.TrimSpace(taskType) != "" {
		parts = append(parts, "task_type="+taskType)
	}
	if complexity, _ := routing["complexity"].(string); strings.TrimSpace(complexity) != "" {
		parts = append(parts, "complexity="+complexity)
	}
	if preferredModel, _ := routing["preferred_model"].(string); strings.TrimSpace(preferredModel) != "" {
		parts = append(parts, "preferred_model="+preferredModel)
	}
	if verification, _ := routing["expected_verification"].(string); strings.TrimSpace(verification) != "" {
		parts = append(parts, "verification="+verification)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  routing: " + strings.Join(parts, "; ")
}

func inferTaskComplexity(description string) string {
	lower := strings.ToLower(strings.TrimSpace(description))
	switch {
	case strings.Contains(lower, "design"), strings.Contains(lower, "architecture"), strings.Contains(lower, "debug"), strings.Contains(lower, "refactor"):
		return "hard"
	case strings.Contains(lower, "test"), strings.Contains(lower, "validate"), strings.Contains(lower, "review"), strings.Contains(lower, "check"):
		return "easy"
	default:
		return "medium"
	}
}

func inferTaskRisk(description string) string {
	lower := strings.ToLower(strings.TrimSpace(description))
	switch {
	case strings.Contains(lower, "debug"), strings.Contains(lower, "fix"), strings.Contains(lower, "refactor"), strings.Contains(lower, "migration"):
		return "high"
	case strings.Contains(lower, "test"), strings.Contains(lower, "validate"), strings.Contains(lower, "review"):
		return "low"
	default:
		return "medium"
	}
}

type codingSituation struct {
	ID                 string
	Label              string
	Description        string
	InvestigationFocus string
	DesignFocus        string
	VerificationFocus  string
	PreferredModelHint string
	Complexity         string
	Risk               string
}

func inferCodingSituation(description string) codingSituation {
	lower := strings.ToLower(strings.TrimSpace(description))
	switch {
	case strings.Contains(lower, "bug") || strings.Contains(lower, "debug") || strings.Contains(lower, "fix"):
		return codingSituation{
			ID:                 "bug_fix",
			Label:              "Bug fix",
			Description:        "Investigate a defect, trace the failing path, and isolate the root cause before changing behavior",
			InvestigationFocus: "Trace the failing execution path, reproduce the symptom, and identify the minimal root cause",
			DesignFocus:        "Preserve existing behavior while narrowing the fix to the affected contract or flow",
			VerificationFocus:  "Reproduce the bug, run the relevant tests, and confirm the fix with regression coverage",
			PreferredModelHint: "strong",
			Complexity:         "hard",
			Risk:               "high",
		}
	case strings.Contains(lower, "design") || strings.Contains(lower, "architecture") || strings.Contains(lower, "api") || strings.Contains(lower, "contract"):
		return codingSituation{
			ID:                 "new_feature",
			Label:              "New feature or design",
			Description:        "Define the shape of a capability, interface, or workflow before implementation",
			InvestigationFocus: "Clarify the user need, boundaries, and constraints before choosing the implementation shape",
			DesignFocus:        "Frame the core abstraction, responsibilities, interfaces, and trade-offs",
			VerificationFocus:  "Validate the proposed design against constraints, risks, and likely integration points",
			PreferredModelHint: "strong",
			Complexity:         "hard",
			Risk:               "medium",
		}
	case strings.Contains(lower, "refactor") || strings.Contains(lower, "cleanup") || strings.Contains(lower, "simplify"):
		return codingSituation{
			ID:                 "refactor",
			Label:              "Refactor",
			Description:        "Improve structure or maintainability without changing external behavior",
			InvestigationFocus: "Map the current responsibilities and identify stable seams before changing structure",
			DesignFocus:        "Preserve behavior while improving cohesion, boundaries, and readability",
			VerificationFocus:  "Run existing tests and confirm behavior remains stable through the refactor",
			PreferredModelHint: "balanced",
			Complexity:         "hard",
			Risk:               "high",
		}
	case strings.Contains(lower, "test") || strings.Contains(lower, "validate") || strings.Contains(lower, "coverage"):
		return codingSituation{
			ID:                 "test_and_validation",
			Label:              "Testing and validation",
			Description:        "Strengthen test coverage, validation logic, or reliability checks",
			InvestigationFocus: "Inspect the current failure mode, coverage gaps, or validation assumptions",
			DesignFocus:        "Choose the smallest assertion or validation strategy that improves confidence",
			VerificationFocus:  "Execute the relevant test suite and capture evidence of the improvement",
			PreferredModelHint: "tool-only",
			Complexity:         "easy",
			Risk:               "low",
		}
	case strings.Contains(lower, "integrat") || strings.Contains(lower, "pipeline") || strings.Contains(lower, "deploy") || strings.Contains(lower, "migration"):
		return codingSituation{
			ID:                 "integration_or_migration",
			Label:              "Integration or migration",
			Description:        "Move behavior across systems, interfaces, or deployment boundaries",
			InvestigationFocus: "Map the source and destination contracts, dependencies, and compatibility risks",
			DesignFocus:        "Define the compatibility strategy, transitional steps, and rollback approach",
			VerificationFocus:  "Exercise the end-to-end path and validate compatibility at the boundary",
			PreferredModelHint: "strong",
			Complexity:         "hard",
			Risk:               "high",
		}
	default:
		return codingSituation{
			ID:                 "implementation",
			Label:              "Implementation",
			Description:        "Deliver a focused change or feature with clear scope and verification",
			InvestigationFocus: "Inspect the relevant code path and supporting context before implementing",
			DesignFocus:        "Keep the change localized and aligned with the surrounding architecture",
			VerificationFocus:  "Validate the change with the most relevant tests or checks",
			PreferredModelHint: "balanced",
			Complexity:         "medium",
			Risk:               "medium",
		}
	}
}

func buildDesignBrief(task cw.Task, pack cw.ContextPack, baseSteps []string) map[string]any {
	description := strings.TrimSpace(task.Description)
	problemStatement := description
	if problemStatement == "" {
		problemStatement = "Design the requested capability or feature with clear boundaries and a practical implementation path"
	}

	constraints := []string{}
	if pack.CodeStructureSummary != "" {
		constraints = append(constraints, "fit the existing structure: "+pack.CodeStructureSummary)
	}
	if pack.RepositorySummary != "" {
		constraints = append(constraints, "match repository conventions: "+pack.RepositorySummary)
	}
	if len(constraints) == 0 {
		constraints = append(constraints, "keep the design scoped to the stated requirement")
	}

	nonGoals := []string{"avoid premature implementation detail", "avoid introducing unrelated abstractions"}
	if len(baseSteps) > 0 {
		nonGoals = append(nonGoals, "avoid solving problems outside the stated scope")
	}

	recommendedApproach := "Describe the planned structure, interfaces, and responsibilities first, then align implementation steps to that design"
	if len(baseSteps) > 0 {
		recommendedApproach += fmt.Sprintf(" with the implementation path: %s", strings.Join(baseSteps, " → "))
	}

	return map[string]any{
		"problem_statement":    problemStatement,
		"constraints":          strings.Join(constraints, " | "),
		"non_goals":            strings.Join(nonGoals, " | "),
		"recommended_approach": recommendedApproach,
		"trade_offs":           "Prefer clarity of interfaces and maintainability over premature optimization; call out the main risk if the design is uncertain",
		"risks":                "Flag unclear requirements, missing context, or significant cross-cutting changes before implementation begins",
	}
}

func buildInvestigationBrief(task cw.Task, pack cw.ContextPack, baseSteps []string) map[string]any {
	description := strings.TrimSpace(task.Description)
	investigationFocus := description
	if investigationFocus == "" {
		investigationFocus = "Investigate the failing path and identify the root cause before proposing a fix"
	}

	analysisQuestions := []string{"What code path triggers the bug?", "Where does the current behavior diverge from the expected behavior?"}
	if len(pack.RelatedFiles) > 0 {
		analysisQuestions = append(analysisQuestions, fmt.Sprintf("Which related files or modules participate in this flow: %s?", strings.Join(pack.RelatedFiles, ", ")))
	}
	if len(pack.TestFiles) > 0 {
		analysisQuestions = append(analysisQuestions, fmt.Sprintf("What existing tests or coverage should be used as evidence: %s?", strings.Join(pack.TestFiles, ", ")))
	}
	if len(baseSteps) > 0 {
		analysisQuestions = append(analysisQuestions, fmt.Sprintf("What evidence should be gathered before changing the flow: %s?", strings.Join(baseSteps, " → ")))
	}

	evidenceToCollect := []string{"trace the relevant execution path", "capture the failing behavior or error", "inspect nearby code and surrounding tests"}
	if pack.CodeStructureSummary != "" {
		evidenceToCollect = append(evidenceToCollect, "map the flow against the current structure: "+pack.CodeStructureSummary)
	}
	if pack.DependencyFiles != nil && len(pack.DependencyFiles) > 0 {
		evidenceToCollect = append(evidenceToCollect, "review dependent modules: "+strings.Join(pack.DependencyFiles, ", "))
	}

	return map[string]any{
		"investigation_focus": investigationFocus,
		"analysis_questions":  strings.Join(analysisQuestions, " | "),
		"evidence_to_collect": strings.Join(evidenceToCollect, " | "),
		"expected_outcome":    "Identify the root cause, confirm the scope of the bug, and define a minimal fix strategy",
	}
}

func buildFilePlan(task cw.Task, pack cw.ContextPack, baseSteps []string) []map[string]any {
	files := make([]string, 0, 8)
	if task.ActiveFile != "" {
		files = append(files, task.ActiveFile)
	}
	for _, file := range pack.RelatedFiles {
		if file != "" && !containsString(files, file) {
			files = append(files, file)
		}
	}
	for _, file := range pack.DependencyFiles {
		if file != "" && !containsString(files, file) {
			files = append(files, file)
		}
	}
	for _, file := range pack.TestFiles {
		if file != "" && !containsString(files, file) {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		files = append(files, task.ActiveFile)
	}

	plan := make([]map[string]any, 0, len(files))
	for idx, file := range files {
		plan = append(plan, map[string]any{
			"file":                file,
			"role":                describeFileRole(file, idx, len(files), task, pack, baseSteps),
			"implementation_hint": fmt.Sprintf("Implement the change for %s", strings.TrimSpace(task.Description)),
		})
	}
	return plan
}

func describeFileRole(file string, idx int, total int, task cw.Task, pack cw.ContextPack, baseSteps []string) string {
	if idx == 0 && task.ActiveFile != "" && file == task.ActiveFile {
		return "primary implementation file"
	}
	if strings.HasSuffix(file, "_test.go") || strings.Contains(file, "test") {
		return "verification and regression coverage"
	}
	if idx < total-1 {
		return "supporting implementation file"
	}
	return "supporting validation file"
}

func buildTaskRoutingMetadata(taskDescription, taskType, complexity, risk string) map[string]any {
	if strings.TrimSpace(taskDescription) == "" {
		return map[string]any{}
	}
	if strings.TrimSpace(taskType) == "" {
		taskType = "implementation"
	}
	if strings.TrimSpace(complexity) == "" {
		complexity = "medium"
	}
	if strings.TrimSpace(risk) == "" {
		risk = "medium"
	}

	mediaTypes := inferMediaTypes(taskDescription)
	mediaRequired := len(mediaTypes) > 0

	preferredModel, selectionTaskType := inferPreferredModelScheme(taskDescription, taskType, complexity, risk)
	estimatedInputTokens, estimatedOutputTokens, maxCostUSD := inferTaskBudget(taskDescription, taskType, complexity, risk, mediaRequired)
	if strings.TrimSpace(selectionTaskType) == "" {
		selectionTaskType = strings.ToLower(strings.TrimSpace(taskType))
	}

	verification := "review the resulting diff"
	if strings.Contains(strings.ToLower(taskDescription), "test") || strings.Contains(strings.ToLower(taskDescription), "validate") || strings.Contains(strings.ToLower(taskDescription), "check") {
		verification = "run the relevant test or validation command"
	}
	if strings.Contains(strings.ToLower(taskDescription), "go test") {
		verification = "run go test for the relevant package or target"
	}

	selection := ai.ModelSelectionRequest{
		TaskType:              selectionTaskType,
		NeedMultimodal:        mediaRequired,
		RequiredMediaTypes:    mediaTypes,
		PreferCheap:           strings.EqualFold(preferredModel, "cheap") || strings.EqualFold(preferredModel, "tool-only"),
		PreferStrong:          strings.EqualFold(preferredModel, "strong"),
		EstimatedInputTokens:  estimatedInputTokens,
		EstimatedOutputTokens: estimatedOutputTokens,
		MaxCostUSD:            maxCostUSD,
	}
	if entry, ok := ai.GetModelCatalogRegistry().SelectBest(selection); ok {
		preferredModel = fmt.Sprintf("%s:%s", entry.Provider, entry.Model)
	}

	return map[string]any{
		"complexity":            complexity,
		"task_type":             taskType,
		"risk":                  risk,
		"expected_verification": verification,
		"preferred_model":       preferredModel,
		"media_required":        mediaRequired,
		"media_types":           mediaTypes,
	}
}

func inferPreferredModelScheme(taskDescription, taskType, complexity, risk string) (string, string) {
	lower := strings.ToLower(strings.TrimSpace(taskDescription))
	selectionTaskType := strings.ToLower(strings.TrimSpace(taskType))
	if selectionTaskType == "" {
		selectionTaskType = "implementation"
	}

	if strings.Contains(lower, "design") || strings.Contains(lower, "architecture") || strings.Contains(lower, "plan") || strings.Contains(lower, "planning") {
		return "strong", "reasoning"
	}
	if strings.Contains(lower, "debug") || strings.Contains(lower, "fix") || strings.Contains(lower, "refactor") || strings.Contains(lower, "migration") {
		return "strong", "reasoning"
	}
	if strings.EqualFold(selectionTaskType, "mechanical") || strings.EqualFold(selectionTaskType, "validation") {
		return "tool-only", selectionTaskType
	}
	if strings.EqualFold(complexity, "easy") {
		return "cheap", selectionTaskType
	}
	return "balanced", selectionTaskType
}

func inferTaskBudget(taskDescription, taskType, complexity, risk string, mediaRequired bool) (int, int, float64) {
	lower := strings.ToLower(strings.TrimSpace(taskDescription))
	selectionTaskType := strings.ToLower(strings.TrimSpace(taskType))
	if selectionTaskType == "" {
		selectionTaskType = "implementation"
	}

	inputTokens := 4000
	outputTokens := 800
	maxCostUSD := 0.08

	switch {
	case strings.Contains(lower, "design") || strings.Contains(lower, "architecture") || strings.Contains(lower, "plan") || strings.Contains(lower, "planning"):
		inputTokens = 8000
		outputTokens = 1200
		maxCostUSD = 0.18
	case strings.Contains(lower, "debug") || strings.Contains(lower, "fix") || strings.Contains(lower, "refactor") || strings.Contains(lower, "migration"):
		inputTokens = 7000
		outputTokens = 1200
		maxCostUSD = 0.16
	case strings.EqualFold(selectionTaskType, "mechanical") || strings.EqualFold(selectionTaskType, "validation"):
		inputTokens = 2500
		outputTokens = 400
		maxCostUSD = 0.01
	case strings.EqualFold(complexity, "easy"):
		inputTokens = 3000
		outputTokens = 600
		maxCostUSD = 0.03
	default:
		inputTokens = 5000
		outputTokens = 900
		maxCostUSD = 0.1
	}

	if mediaRequired {
		inputTokens += 2000
		maxCostUSD += 0.04
	}
	if strings.EqualFold(risk, "high") {
		inputTokens += 1000
		maxCostUSD += 0.02
	}
	return inputTokens, outputTokens, maxCostUSD
}

func inferMediaTypes(description string) []string {
	lower := strings.ToLower(strings.TrimSpace(description))
	if lower == "" {
		return nil
	}

	var mediaTypes []string
	if strings.Contains(lower, "video") || strings.Contains(lower, "clip") || strings.Contains(lower, "movie") {
		mediaTypes = append(mediaTypes, "video")
	}
	if strings.Contains(lower, "audio") || strings.Contains(lower, "sound") || strings.Contains(lower, "speech") || strings.Contains(lower, "transcrib") {
		mediaTypes = append(mediaTypes, "audio")
	}
	if strings.Contains(lower, "image") || strings.Contains(lower, "picture") || strings.Contains(lower, "photo") || strings.Contains(lower, "screenshot") || strings.Contains(lower, "diagram") {
		mediaTypes = append(mediaTypes, "image")
	}
	if len(mediaTypes) == 0 && strings.Contains(lower, "media") {
		mediaTypes = append(mediaTypes, "image")
	}
	return mediaTypes
}

func buildPhasePlanSequence(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(q, "review") || strings.Contains(q, "validate") || strings.Contains(q, "check") {
		return []string{"intake", "investigation", "testing", "review", "delivery"}
	}
	return []string{"intake", "investigation", "planning", "testing", "execution", "review", "delivery"}
}

func collectCheckpointNames(args map[string]any) []string {
	if args == nil {
		return nil
	}
	if raw, ok := args["checkpoints"].([]string); ok {
		return raw
	}
	if raw, ok := args["checkpoints"].([]any); ok {
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func firstStringArg(args map[string]any, keys ...string) string {
	if args == nil {
		return ""
	}
	for _, key := range keys {
		if raw, ok := args[key]; ok {
			if s, ok := raw.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
