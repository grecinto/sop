package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// CodingPhase implements phase capability for coding_workflow domain.
type CodingPhase struct {
	BasePhase
}

// newCodingPhase creates a coding workflow phase
func newCodingPhase(phaseID string, infra *SharedInfrastructure) *CodingPhase {
	name, description := phaseMetadata(phaseID)
	base := newBasePhase(phaseID, name, description, "coding_workflow", infra)
	base.setAuthoringProfile(PhaseAuthoringProfile{
		Domain:     "coding_workflow",
		Principles: []string{"Use the most relevant tools and evidence for the task"},
		Instructions: map[string]string{
			"intake":        "Use Intake only when scope, constraints, or success criteria are genuinely missing.",
			"investigation": "Use Investigation as part of discovery mode to gather evidence from the repository, tests, and surrounding code before making changes.",
			"planning":      "Use Planning as part of discovery mode to create only the amount of design structure the task actually needs.",
			"testing":       "Use Testing as embedded validation behavior, not a separate runtime destination, unless an older workflow artifact explicitly resumes here.",
			"execution":     "Use Execution as implementation mode to inspect, patch, validate, and continue the same Ask/ReAct transaction until the task is done.",
			"review":        "Use Review only when resuming an older workflow artifact; otherwise validate inside implementation mode.",
			"delivery":      "Use Delivery only when resuming an older workflow artifact; otherwise summarize directly from implementation mode.",
		},
		PhaseRules: map[string][]capabilities.GuidanceRule{
			"investigation": {{Description: "Gather evidence before proposing a fix or change", AppliesTo: []string{"investigation"}}},
			"planning":      {{Description: "Create a concrete plan with constraints, dependencies, and validation steps", AppliesTo: []string{"planning"}}},
			"execution":     {{Description: "Implement only the scoped change and preserve the surrounding contract", AppliesTo: []string{"execution"}}},
			"review":        {{Description: "Validate the result against the evidence and surface remaining uncertainty", AppliesTo: []string{"review"}}},
			"delivery":      {{Description: "Summarize the outcome, decisions, and follow-up steps clearly", AppliesTo: []string{"delivery"}}},
		},
	})
	return &CodingPhase{BasePhase: base}
}

func (p *CodingPhase) BuildContext(query string, state capabilities.RoutingState) string {
	switch p.phaseID {
	case "intake":
		return `PHASE: Intake & Context Assembly
OBJECTIVE: Gather requirements, clarify scope
WORKFLOW: Ask questions → Build context → Document requirements → Capture artifact`
	case "investigation":
		return `PHASE: Investigation & Analysis
OBJECTIVE: Discovery mode for understanding current state and finding patterns
REQUIRED: investigation_build_context (call first)
WORKFLOW: Build context → Inspect repo → Search patterns → Document evidence → Continue directly into implementation when the next step is clear`
	case "planning":
		return `PHASE: Planning & Design
OBJECTIVE: Discovery mode design aid when a concrete plan adds value
WORKFLOW: Build context → Plan changes → Capture planning evidence → Continue directly into implementation when the next step is clear`
	case "testing":
		return `PHASE: Testing & Validation
OBJECTIVE: Legacy validation phase for older artifacts; prefer running tests from discovery or implementation in the current coding runtime
GUIDANCE: Use tests as a design aid when they clarify contracts, interfaces, or expected behavior; do not force TDD when the right abstraction is still being explored
WORKFLOW: Review requirements → Add/adjust tests or validation cases → Run tests → Capture results`
	case "execution":
		return `PHASE: Execution & Implementation
OBJECTIVE: Implementation mode for applying changes safely in a continuous Ask/ReAct transaction
SAFETY: Always preview before apply
MULTI-FILE: Use apply_multi_file_patch for multiple related files (faster than sequential apply_patch)
WORKFLOW: Preview → Approve → Apply → Test → Capture results → Summarize or continue implementation as needed`
	case "review":
		return `PHASE: Review & Validation
OBJECTIVE: Legacy review phase for older artifacts; prefer validating inside implementation mode in the current coding runtime
WORKFLOW: Build context → Inspect changes → Run tests → Validate → Capture results`
	case "delivery":
		return `PHASE: Delivery & Handoff
OBJECTIVE: Legacy delivery phase for older artifacts; prefer summarizing directly from implementation mode in the current coding runtime
WORKFLOW: Review artifacts → Summarize → Document → Capture → Signal exit`
	default:
		return fmt.Sprintf("Phase: %s for coding workflow", p.name)
	}
}

func (p *CodingPhase) BuildPlanningContext(query string, state capabilities.RoutingState) string {
	parts := []string{
		"Coding Planning Context",
		"- Domain focus: repository-aware implementation, repair, refactor, and validation work.",
		"- Primary planning concerns: active files, nearby tests, interfaces, symbols, dependency paths, and behavior contracts.",
		"- Preserve local correctness and existing contracts unless the user explicitly requests a behavior or architecture change.",
		"- Validation should prove the code, tests, and intended contract still align after the planned change.",
	}
	if hint := strings.TrimSpace(state.CapabilityHint); hint != "" {
		parts = append(parts, "- Active runtime hint: "+hint)
	}
	lower := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(lower, "fail") || strings.Contains(lower, "test") {
		parts = append(parts, "- Repair rule: determine whether the failing behavior comes from outdated tests, broken code, or both before choosing the repair path.")
	}
	if strings.Contains(lower, "design") || strings.Contains(lower, "architect") {
		parts = append(parts, "- Architecture rule: surface contracts, trade-offs, and slice boundaries before broad implementation work.")
	}
	return strings.Join(parts, "\n")
}

func (p *CodingPhase) EvaluateStepCompletion(query string, state capabilities.RoutingState, actionText string, validation []string, toolCalls []ai.ToolCall, outcomeFacts []string) bool {
	_ = query
	_ = state
	text := strings.ToLower(strings.TrimSpace(actionText + " " + strings.Join(validation, " ")))
	for _, toolCall := range toolCalls {
		toolName := strings.ToLower(strings.TrimSpace(toolCall.Name))
		switch {
		case strings.Contains(text, "inspect") || strings.Contains(text, "search") || strings.Contains(text, "trace") || strings.Contains(text, "identify"):
			if strings.Contains(toolName, "inspect_repo") || strings.Contains(toolName, "search_repo") || strings.Contains(toolName, "build_context") || strings.Contains(toolName, "list_changed_files") {
				return true
			}
		case strings.Contains(text, "plan") || strings.Contains(text, "design") || strings.Contains(text, "decide"):
			if strings.Contains(toolName, "plan_changes") || strings.Contains(toolName, "build_context") {
				return true
			}
		case strings.Contains(text, "validate") || strings.Contains(text, "test") || strings.Contains(text, "verify"):
			if strings.Contains(toolName, "run_tests") {
				return true
			}
		case strings.Contains(text, "patch") || strings.Contains(text, "apply") || strings.Contains(text, "implement") || strings.Contains(text, "fix") || strings.Contains(text, "edit"):
			if strings.Contains(toolName, "apply_patch") || strings.Contains(toolName, "apply_multi_file_patch") || strings.Contains(toolName, "preview_patch") {
				return true
			}
		}
	}
	return len(toolCalls) > 0 && len(outcomeFacts) > 0
}

func (p *CodingPhase) ExposeTools() []capabilities.ToolDefinition {
	tools := []capabilities.ToolDefinition{{
		Name:        p.captureTool,
		Description: fmt.Sprintf("Captures %s phase artifact to memory", p.name),
		Schema:      captureToolSchema(),
	}, {
		Name:        p.stateTool,
		Description: fmt.Sprintf("Returns current persisted state for %s phase", p.name),
		Schema:      stateToolSchema(),
	}}

	tools = append(tools, p.getCodingTools()...)
	return tools
}

func (p *CodingPhase) getCodingTools() []capabilities.ToolDefinition {
	taskSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string"},
    "working_dir": {"type": "string"},
    "active_file": {"type": "string"}
  }
}`

	searchSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string"},
    "search": {"type": "string"}
  }
}`

	patchSchema := `{
  "type": "object",
  "required": ["file_path", "old_content", "new_content"],
  "properties": {
    "file_path": {"type": "string"},
    "old_content": {"type": "string"},
    "new_content": {"type": "string"},
    "description": {"type": "string"}
  }
}`

	multiFilePatchSchema := `{
  "type": "object",
  "required": ["patches"],
  "properties": {
    "patches": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["file_path", "old_content", "new_content"],
        "properties": {
          "file_path": {"type": "string"},
          "old_content": {"type": "string"},
          "new_content": {"type": "string"},
          "description": {"type": "string"}
        }
      }
    },
    "description": {"type": "string"}
  }
}`

	successionSchema := `{
  "type": "object",
  "required": ["phases"],
  "properties": {
    "phases": {"type": "array", "items": {"type": "string"}},
    "checkpoints": {"type": "array", "items": {"type": "string"}},
    "reasoning": {"type": "string"},
    "confidence": {"type": "string"}
  }
}`

	switch p.phaseID {
	case "intake":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("build_context"),
			Description: "Builds repository context if task involves code",
			Schema:      taskSchema,
		}}

	case "investigation":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("build_context"),
			Description: "REQUIRED FIRST: Builds repository context",
			Schema:      taskSchema,
		}, {
			Name:        p.phaseToolName("inspect_repo"),
			Description: "Inspects current repository state",
			Schema:      taskSchema,
		}, {
			Name:        p.phaseToolName("search_repo"),
			Description: "Searches repository for patterns",
			Schema:      searchSchema,
		}, {
			Name:        p.phaseToolName("list_changed_files"),
			Description: "Lists files with uncommitted changes",
			Schema:      taskSchema,
		}}

	case "planning":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("assess_and_route"),
			Description: "PLANNING-FIRST: Assesses query and determines starting phase",
			Schema:      taskSchema,
		}, {
			Name:        p.phaseToolName("create_succession_path"),
			Description: "Creates phase succession path with checkpoints",
			Schema:      successionSchema,
		}, {
			Name:        p.phaseToolName("build_context"),
			Description: "REQUIRED: Builds repository context for planning",
			Schema:      taskSchema,
		}, {
			Name:        p.phaseToolName("plan_changes"),
			Description: "Generates execution plan. Use tests or validation cases as a design aid when they help specify contracts, interfaces, or expected behavior; avoid making TDD mandatory for exploratory work.",
			Schema:      taskSchema,
		}}

	case "testing":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("run_tests"),
			Description: "Runs tests and validation commands. WHEN TO CALL: After adding or updating tests, or to validate the current implementation. Use tests as a design aid and regression check, not as a rigid requirement for exploratory work.",
			Schema:      taskSchema,
		}, {
			Name:        p.phaseToolName("build_context"),
			Description: "Builds context for test planning and failure triage.",
			Schema:      taskSchema,
		}}

	case "execution":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("preview_patch"),
			Description: "SAFETY: Preview patch before applying",
			Schema:      patchSchema,
		}, {
			Name:        p.phaseToolName("apply_patch"),
			Description: "Applies patch to file (preview first)",
			Schema:      patchSchema,
		}, {
			Name:        p.phaseToolName("apply_multi_file_patch"),
			Description: "Applies patches to multiple files atomically. Use when generating/modifying multiple related files (e.g., adding feature with multiple components). More efficient than sequential apply_patch calls.",
			Schema:      multiFilePatchSchema,
		}, {
			Name:        p.phaseToolName("run_tests"),
			Description: "Runs tests after changes",
			Schema:      taskSchema,
		}}

	case "review":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("build_context"),
			Description: "Builds context for review",
			Schema:      taskSchema,
		}, {
			Name:        p.phaseToolName("inspect_repo"),
			Description: "Inspects changes for review",
			Schema:      taskSchema,
		}, {
			Name:        p.phaseToolName("run_tests"),
			Description: "Runs comprehensive tests",
			Schema:      taskSchema,
		}}

	case "delivery":
		return []capabilities.ToolDefinition{{
			Name:        p.phaseToolName("build_context"),
			Description: "Builds final context for delivery",
			Schema:      taskSchema,
		}}

	default:
		return nil
	}
}

func (p *CodingPhase) BuildInstructions(query string, state capabilities.RoutingState) string {
	return p.BasePhase.BuildInstructions(query, state)
}

func (p *CodingPhase) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state

	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return "", fmt.Errorf("empty tool name")
	}

	if name == strings.ToLower(p.captureTool) {
		return p.runCapture(args)
	}
	if name == strings.ToLower(p.stateTool) {
		return p.runState(args)
	}

	switch name {
	case strings.ToLower(p.phaseToolName("assess_and_route")):
		return p.runAssessAndRoute(args)
	case strings.ToLower(p.phaseToolName("create_succession_path")):
		return p.runCreateSuccessionPath(args)
	case strings.ToLower(p.phaseToolName("build_context")):
		return p.runBuildContext(args)
	case strings.ToLower(p.phaseToolName("inspect_repo")):
		return p.runInspectRepo(args)
	case strings.ToLower(p.phaseToolName("search_repo")):
		return p.runSearchRepo(args)
	case strings.ToLower(p.phaseToolName("list_changed_files")):
		return p.runListChangedFiles(args)
	case strings.ToLower(p.phaseToolName("plan_changes")):
		return p.runPlanChanges(args)
	case strings.ToLower(p.phaseToolName("run_tests")):
		return p.runTests(args)
	case strings.ToLower(p.phaseToolName("preview_patch")):
		return p.runPreviewPatch(args)
	case strings.ToLower(p.phaseToolName("apply_patch")):
		return p.runApplyPatch(args)
	case strings.ToLower(p.phaseToolName("apply_multi_file_patch")):
		return p.runApplyMultiFilePatch(args)
	case strings.ToLower(p.phaseToolName("run_tests")):
		return p.runTests(args)
	default:
		return "", fmt.Errorf("unsupported tool %q in coding workflow", toolName)
	}
}

// Tool execution methods

func (p *CodingPhase) buildTask(args map[string]any) Task {
	query := readPhaseString(args, "query", "")
	workingDir := readPhaseString(args, "working_dir", "")
	activeFile := readPhaseString(args, "active_file", "")

	state := RunState{
		Args:     map[string]string{},
		Metadata: map[string]string{"phase": p.phaseID},
	}
	if p.phaseID == "planning" {
		if continuationSummary := p.loadPhaseContinuationSummary(); continuationSummary != "" {
			state.Metadata["continuation_summary"] = continuationSummary
		}
	}

	return Task{
		WorkingDir:  workingDir,
		ActiveFile:  activeFile,
		Description: query,
		TaskType:    p.phaseID,
		State:       state,
	}
}

func (p *CodingPhase) loadPhaseContinuationSummary() string {
	infra := p.ensureSharedInfrastructure()
	if infra == nil || infra.Memory == nil {
		return ""
	}
	values, err := infra.Memory.Load("phase-handoff:planning")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values["continuation_summary"])
}

func (p *CodingPhase) runBuildContext(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	packOps := infra.NewContextPackOperations()
	pack, err := packOps.BuildContextForTask(task, task.State)
	if err != nil {
		return "", fmt.Errorf("build context: %w", err)
	}

	if infra.RepoIndexer != nil {
		_, _ = infra.RepoIndexer.Index(task, task.State)
	}

	return formatContextPack(pack), nil
}

func (p *CodingPhase) runInspectRepo(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	repoOps := infra.NewRepoOperations()
	summary, err := repoOps.InspectRepo(task, task.State)
	if err != nil {
		return "", fmt.Errorf("inspect repo: %w", err)
	}

	parts := []string{"Repository Inspection"}
	if len(summary.ChangedFiles) > 0 {
		parts = append(parts, fmt.Sprintf("Changed Files: %s", strings.Join(summary.ChangedFiles, ", ")))
	} else {
		parts = append(parts, "Changed Files: none")
	}
	if strings.TrimSpace(summary.DiffPreview) != "" {
		parts = append(parts, fmt.Sprintf("Diff Preview: %s", summary.DiffPreview))
	}
	return strings.Join(parts, "\n"), nil
}

func (p *CodingPhase) runSearchRepo(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	repoOps := infra.NewRepoOperations()
	searchQuery := readPhaseString(args, "search", "")
	if searchQuery == "" {
		searchQuery = task.Description
	}

	results, err := repoOps.SearchRepo(task, task.State, searchQuery)
	if err != nil {
		return "", fmt.Errorf("search repo: %w", err)
	}

	if len(results) == 0 {
		return "Search Results: none", nil
	}
	return fmt.Sprintf("Search Results:\n%s", strings.Join(results, "\n")), nil
}

func (p *CodingPhase) runListChangedFiles(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	repoOps := infra.NewRepoOperations()
	files, err := repoOps.ListChangedFiles(task, task.State)
	if err != nil {
		return "", fmt.Errorf("list changed files: %w", err)
	}

	if len(files) == 0 {
		return "Changed Files: none", nil
	}
	return fmt.Sprintf("Changed Files:\n%s", strings.Join(files, "\n")), nil
}

func (p *CodingPhase) runPlanChanges(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	packOps := infra.NewContextPackOperations()
	pack, err := packOps.BuildContextForTask(task, task.State)
	if err != nil {
		return "", fmt.Errorf("build context for planning: %w", err)
	}

	if infra.Planner == nil {
		return "", fmt.Errorf("planner not configured")
	}
	plan, err := infra.Planner.Plan(task, task.State, pack)
	if err != nil {
		return "", fmt.Errorf("plan changes: %w", err)
	}
	basePlan := cw.BasePlanFromStructuredPlan(plan)

	if len(basePlan) == 0 {
		return "Execution Plan: none", nil
	}

	detailedSteps := buildDetailedExecutionPlan(task, pack, basePlan)
	if len(detailedSteps) > 0 {
		miniPlan := buildPhaseMiniPlan(task, pack, basePlan)
		structuredData := map[string]any{
			"structured_plan":         plan,
			"phase_mini_plan":         miniPlan,
			"detailed_execution_plan": serializeDetailedExecutionPlan(detailedSteps),
			"base_plan":               basePlan,
		}
		if _, err := p.runCapture(map[string]any{
			"summary":         fmt.Sprintf("Execution plan for %s", strings.TrimSpace(task.Description)),
			"status":          "ready_for_execution",
			"plan":            toStringSlice(basePlan),
			"notes":           []any{fmt.Sprintf("Target files: %s", strings.Join(collectTargetFiles(task, pack), ", "))},
			"structured_data": structuredData,
		}); err != nil {
			return "", fmt.Errorf("capture planning artifact: %w", err)
		}
		return fmt.Sprintf("Execution Plan:\n%s", formatDetailedExecutionPlan(detailedSteps)), nil
	}
	return fmt.Sprintf("Execution Plan:\n%s", strings.Join(basePlan, "\n")), nil
}

func serializeDetailedExecutionPlan(steps []detailedExecutionStep) []map[string]any {
	if len(steps) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		out = append(out, map[string]any{
			"title":         step.Title,
			"description":   step.Description,
			"files":         step.Files,
			"validation":    step.Validation,
			"exit_criteria": step.ExitCriteria,
		})
	}
	return out
}

func formatDetailedExecutionPlan(steps []detailedExecutionStep) string {
	if len(steps) == 0 {
		return ""
	}
	lines := make([]string, 0, len(steps)*3+1)
	lines = append(lines, "Detailed execution plan:")
	for idx, step := range steps {
		line := fmt.Sprintf("%d. %s", idx+1, step.Description)
		if len(step.Files) > 0 {
			line += fmt.Sprintf(" [files: %s]", strings.Join(step.Files, ", "))
		}
		lines = append(lines, line)
		if len(step.Validation) > 0 {
			lines = append(lines, fmt.Sprintf("   validation: %s", strings.Join(step.Validation, "; ")))
		}
		if len(step.ExitCriteria) > 0 {
			lines = append(lines, fmt.Sprintf("   exit criteria: %s", strings.Join(step.ExitCriteria, "; ")))
		}
	}
	return strings.Join(lines, "\n")
}

func toStringSlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func collectTargetFiles(task cw.Task, pack cw.ContextPack) []string {
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
	return files
}

func (p *CodingPhase) runPreviewPatch(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	patch := readPatch(args)
	if patch == nil {
		return "", fmt.Errorf("missing patch inputs")
	}

	patchOps := infra.NewPatchOperations()
	preview, err := patchOps.PreviewPatch(task, patch, task.State)
	if err != nil {
		return "", fmt.Errorf("preview patch: %w", err)
	}
	return preview, nil
}

func (p *CodingPhase) runApplyPatch(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	patch := readPatch(args)
	if patch == nil {
		return "", fmt.Errorf("missing patch inputs")
	}

	if infra.Policy != nil {
		if err := infra.Policy.Allow(task, task.State); err != nil {
			return "", err
		}
	}

	patchOps := infra.NewPatchOperations()
	if err := patchOps.ApplyPatch(task, patch, task.State); err != nil {
		return "", fmt.Errorf("apply patch: %w", err)
	}
	return fmt.Sprintf("Applied patch to %s", patch.FilePath), nil
}

func (p *CodingPhase) runApplyMultiFilePatch(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	patches := readMultiFilePatch(args)
	if len(patches) == 0 {
		return "", fmt.Errorf("missing patches array")
	}

	// Policy check applies to all patches
	if infra.Policy != nil {
		if err := infra.Policy.Allow(task, task.State); err != nil {
			return "", err
		}
	}

	patchOps := infra.NewPatchOperations()
	appliedFiles := make([]string, 0, len(patches))
	failedFiles := make([]string, 0)

	// Apply all patches - collect both successes and failures
	for i, patch := range patches {
		if err := patchOps.ApplyPatch(task, patch, task.State); err != nil {
			failedFiles = append(failedFiles, fmt.Sprintf("%s: %v", patch.FilePath, err))
		} else {
			appliedFiles = append(appliedFiles, patch.FilePath)
		}
		_ = i // Track for potential rollback in future
	}

	// Build result message
	description := readPhaseString(args, "description", "")
	resultParts := make([]string, 0, 4)

	if description != "" {
		resultParts = append(resultParts, fmt.Sprintf("Multi-file patch: %s", description))
	} else {
		resultParts = append(resultParts, "Multi-file patch applied")
	}

	if len(appliedFiles) > 0 {
		resultParts = append(resultParts, fmt.Sprintf("✅ Applied %d files: %s", len(appliedFiles), strings.Join(appliedFiles, ", ")))
	}

	if len(failedFiles) > 0 {
		resultParts = append(resultParts, fmt.Sprintf("❌ Failed %d files:\n%s", len(failedFiles), strings.Join(failedFiles, "\n")))
		// Partial success is still reported, but with error context
		if len(appliedFiles) == 0 {
			return "", fmt.Errorf("all patches failed:\n%s", strings.Join(failedFiles, "\n"))
		}
	}

	return strings.Join(resultParts, "\n"), nil
}

func (p *CodingPhase) runTests(args map[string]any) (string, error) {
	task := p.buildTask(args)
	infra := p.ensureSharedInfrastructure()

	testOps := infra.NewTestOperations()
	result, err := testOps.RunTests(task, task.State)
	if err != nil {
		return "", fmt.Errorf("run tests: %w", err)
	}

	if result.Passed {
		return fmt.Sprintf("Tests passed\n%s", result.Output), nil
	}
	return fmt.Sprintf("Tests failed\n%s", result.Output), nil
}

func (p *CodingPhase) runAssessAndRoute(args map[string]any) (string, error) {
	query := readPhaseString(args, "query", "")
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	queryLower := strings.ToLower(strings.TrimSpace(query))
	detectedPhase := detectStartingPhase(queryLower)

	characteristics := []string{}
	if strings.Contains(queryLower, "fix") || strings.Contains(queryLower, "bug") {
		characteristics = append(characteristics, "🔍 Bug/error fixing")
	}
	if strings.Contains(queryLower, "add") || strings.Contains(queryLower, "implement") {
		characteristics = append(characteristics, "✨ Feature implementation")
	}
	if strings.Contains(queryLower, "review") || strings.Contains(queryLower, "validate") {
		characteristics = append(characteristics, "✅ Validation/review")
	}

	return fmt.Sprintf(`PHASE ASSESSMENT
Query: %s
Detected: %s
Characteristics: %s
Recommended: Start in %s phase`,
		query, detectedPhase, strings.Join(characteristics, ", "), detectedPhase), nil
}

func (p *CodingPhase) runCreateSuccessionPath(args map[string]any) (string, error) {
	phases := readPhaseStringSlice(args, "phases")
	if len(phases) == 0 {
		return "", fmt.Errorf("phases array required")
	}

	validPhases := map[string]bool{
		"intake": true, "investigation": true, "planning": true,
		"execution": true, "review": true, "delivery": true,
	}
	for _, phase := range phases {
		if !validPhases[phase] {
			return "", fmt.Errorf("invalid phase: %s", phase)
		}
	}

	checkpoints := readPhaseStringSlice(args, "checkpoints")
	reasoning := readPhaseString(args, "reasoning", "No reasoning")
	confidence := readPhaseString(args, "confidence", "medium")

	checkpointMap := make(map[string]bool)
	for _, cp := range checkpoints {
		checkpointMap[cp] = true
	}

	if p.sharedInfra != nil && p.sharedInfra.STMAccessor != nil {
		stm := p.sharedInfra.STMAccessor()
		if stm != nil {
			if err := stm.CreatePhasePlan(phases, checkpointMap, reasoning, confidence); err != nil {
				return "", fmt.Errorf("failed to store plan: %w", err)
			}
		}
	}

	return fmt.Sprintf(`✅ PHASE PLAN CREATED
Phases: %s
Reasoning: %s
Confidence: %s
Next: Begin with %s`,
		strings.Join(phases, " → "), reasoning, confidence, phases[0]), nil
}

// Helper functions

func formatContextPack(pack ContextPack) string {
	parts := []string{"Context Pack:"}
	if pack.WorkingDir != "" {
		parts = append(parts, fmt.Sprintf("Working Dir: %s", pack.WorkingDir))
	}
	if pack.ActiveFile != "" {
		parts = append(parts, fmt.Sprintf("Active File: %s", pack.ActiveFile))
	}
	if pack.RepositorySummary != "" {
		parts = append(parts, fmt.Sprintf("Repository: %s", pack.RepositorySummary))
	}
	return strings.Join(parts, "\n")
}

func readPatch(args map[string]any) *Patch {
	if args == nil {
		return nil
	}
	filePath := readPhaseString(args, "file_path", "")
	oldContent := readPhaseString(args, "old_content", "")
	newContent := readPhaseString(args, "new_content", "")
	description := readPhaseString(args, "description", "")

	if filePath == "" && oldContent == "" && newContent == "" {
		return nil
	}
	return &Patch{
		FilePath:    filePath,
		OldContent:  oldContent,
		NewContent:  newContent,
		Description: description,
	}
}

func readMultiFilePatch(args map[string]any) []*Patch {
	if args == nil {
		return nil
	}

	patchesRaw, ok := args["patches"]
	if !ok {
		return nil
	}

	// Handle both []any and []map[string]any
	var patchMaps []map[string]any
	switch v := patchesRaw.(type) {
	case []any:
		patchMaps = make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				patchMaps = append(patchMaps, m)
			}
		}
	case []map[string]any:
		patchMaps = v
	default:
		return nil
	}

	if len(patchMaps) == 0 {
		return nil
	}

	patches := make([]*Patch, 0, len(patchMaps))
	for _, patchArgs := range patchMaps {
		patch := readPatch(patchArgs)
		if patch != nil {
			patches = append(patches, patch)
		}
	}

	return patches
}
