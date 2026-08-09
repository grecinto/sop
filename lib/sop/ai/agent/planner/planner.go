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

	architectureDesign := []string{}
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
		return []string{"intake", "investigation", "review", "delivery"}
	}
	return []string{"intake", "investigation", "planning", "execution", "review", "delivery"}
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
