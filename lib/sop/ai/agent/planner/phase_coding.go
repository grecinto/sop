package planner

import (
	"context"
	"fmt"
	"strings"

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
	return &CodingPhase{
		BasePhase: newBasePhase(phaseID, name, description, "coding_workflow", infra),
	}
}

func (p *CodingPhase) BuildContext(query string, state capabilities.RoutingState) string {
	switch p.phaseID {
	case "intake":
		return `PHASE: Intake & Context Assembly
OBJECTIVE: Gather requirements, clarify scope
WORKFLOW: Ask questions → Build context → Document requirements → Capture artifact`
	case "investigation":
		return `PHASE: Investigation & Analysis
OBJECTIVE: Understand current state, find patterns
REQUIRED: investigation_build_context (call first)
WORKFLOW: Build context → Inspect repo → Search patterns → Document evidence`
	case "planning":
		return `PHASE: Planning & Design
OBJECTIVE: Create execution strategy
PLANNING-FIRST: Call planning_assess_and_route FIRST
WORKFLOW: Assess → Create succession path → Build context → Plan changes → Capture artifact`
	case "execution":
		return `PHASE: Execution & Implementation
OBJECTIVE: Apply changes safely
SAFETY: Always preview before apply
MULTI-FILE: Use apply_multi_file_patch for multiple related files (faster than sequential apply_patch)
WORKFLOW: Preview → Approve → Apply → Test → Capture results`
	case "review":
		return `PHASE: Review & Validation
OBJECTIVE: Validate changes
WORKFLOW: Build context → Inspect changes → Run tests → Validate → Capture results`
	case "delivery":
		return `PHASE: Delivery & Handoff
OBJECTIVE: Summarize and complete
WORKFLOW: Review artifacts → Summarize → Document → Capture → Signal exit`
	default:
		return fmt.Sprintf("Phase: %s for coding workflow", p.name)
	}
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
			Description: "Generates execution plan",
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
	switch p.phaseID {
	case "intake":
		return "Use Intake: Ask questions → Gather requirements → Capture artifact → Transition"
	case "investigation":
		return "Use Investigation: Build context → Inspect → Search → Document evidence → Transition"
	case "planning":
		return "Use Planning: Assess → Create path → Plan changes → Capture → Transition"
	case "execution":
		return "Use Execution: Preview → Approve → Apply → Test → Capture → Transition. SAFETY: Always preview first. MULTI-FILE: Use apply_multi_file_patch for batch operations"
	case "review":
		return "Use Review: Build context → Inspect → Test → Validate → Capture → Transition"
	case "delivery":
		return "Use Delivery: Review artifacts → Summarize → Document → Capture → Exit"
	default:
		return fmt.Sprintf("Use %s capability", p.name)
	}
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

	return Task{
		WorkingDir:  workingDir,
		ActiveFile:  activeFile,
		Description: query,
		TaskType:    p.phaseID,
		State:       state,
	}
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

	if len(plan) == 0 {
		return "Execution Plan: none", nil
	}

	detailedSteps := buildDetailedExecutionPlan(task, pack, plan)
	if len(detailedSteps) > 0 {
		miniPlan := buildPhaseMiniPlan(task, pack, plan)
		structuredData := map[string]any{
			"phase_mini_plan":         miniPlan,
			"detailed_execution_plan": serializeDetailedExecutionPlan(detailedSteps),
			"base_plan":               plan,
		}
		if _, err := p.runCapture(map[string]any{
			"summary":         fmt.Sprintf("Execution plan for %s", strings.TrimSpace(task.Description)),
			"status":          "ready_for_execution",
			"plan":            toStringSlice(plan),
			"notes":           []any{fmt.Sprintf("Target files: %s", strings.Join(collectTargetFiles(task, pack), ", "))},
			"structured_data": structuredData,
		}); err != nil {
			return "", fmt.Errorf("capture planning artifact: %w", err)
		}
		return fmt.Sprintf("Execution Plan:\n%s", formatDetailedExecutionPlan(detailedSteps)), nil
	}
	return fmt.Sprintf("Execution Plan:\n%s", strings.Join(plan, "\n")), nil
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
