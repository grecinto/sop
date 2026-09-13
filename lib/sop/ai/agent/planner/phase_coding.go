package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	log "log/slog"

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
WORKFLOW: Build context → Inspect repo → Search patterns → Document evidence → Continue directly into implementation when the next step is clear
INVENTORY RULE: If the user asks to analyze a codebase, map the repo, or fix failing tests, call compile_artifact_inventory first, then operate on a selected inventory item rather than rediscovering the whole repo each turn
FILE RULE: If you need adjacent files or companion tests, list the directory first instead of guessing sibling filenames.`
	case "planning":
		return `PHASE: Planning & Design
OBJECTIVE: Discovery mode design aid when a concrete plan adds value
WORKFLOW: Build context → Plan changes → Capture planning evidence → Continue directly into implementation when the next step is clear
INVENTORY RULE: If a worklist is needed or the task is framed as fixing failing tests, compile the artifact inventory first, then pick the next item to plan or design around`
	case "testing":
		return `PHASE: Testing & Validation
OBJECTIVE: Legacy validation phase for older artifacts; prefer running tests from discovery or implementation in the current coding runtime
GUIDANCE: Use tests as a design aid when they clarify contracts, interfaces, or expected behavior; do not force TDD when the right abstraction is still being explored
WORKFLOW: Review requirements → Add/adjust tests or validation cases → Run tests → Capture results`
	case "execution":
		return `PHASE: Execution & Implementation
OBJECTIVE: Implementation mode for applying changes safely in a continuous Ask/ReAct transaction

CRITICAL: If you have test failures and source context, IMMEDIATELY generate patches. DO NOT continue investigating.

		When you need adjacent files or companion tests, use read_file for a known filename. Use list_directory only when you need to discover an unknown directory or sibling filenames first; do not feed list_directory a filename.

SAME-TURN PIVOT RULE: If failing tests, relevant file reads, and the likely repair target are already known, transition to preview_patch immediately in this same Ask/ReAct turn. Do not wait for a separate phase handoff or queue item.

WORKFLOW:
1. IF test failures identified AND source files examined → GENERATE patches NOW (use preview_patch)
2. IF patches previewed → Apply them (use apply_patch or apply_multi_file_patch)
3. IF patches applied → Run tests to validate (use run_tests)
4. IF validation complete → Capture results

SAFETY: Always preview before apply
MULTI-FILE: Use apply_multi_file_patch for multiple related files (faster than sequential apply_patch)
INVESTIGATION: Only gather MORE context if patches fail validation and you need additional information
		ITEMS LIST RULE: If the task was compiled into an artifact inventory, treat that workstream item list as the source of steps/tasks to fix. Work the current active item first, then mark it complete and advance to the next pending item`
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
		Description: fmt.Sprintf("Captures %s phase artifact to workstream state", p.name),
		Schema:      captureToolSchema(),
	}, {
		Name:        p.stateTool,
		Description: fmt.Sprintf("Returns current persisted state for %s phase", p.name),
		Schema:      stateToolSchema(),
	}}

	// Add universal tools available to ALL phases
	tools = append(tools, p.getCodingUniversalTools()...)

	// Add phase-specific tools
	tools = append(tools, p.getCodingTools()...)
	return tools
}

// getCodingUniversalTools returns tools available to ALL coding phases.
// These enable core operations like running tests, reading files, and searching repos.
func (p *CodingPhase) getCodingUniversalTools() []capabilities.ToolDefinition {
	taskSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Task description or query"},
    "working_dir": {"type": "string", "description": "Use 'current' for session working directory (default if omitted). Never pass absolute paths."},
    "active_file": {"type": "string", "description": "Primary file for changes (optional, inferred if omitted)"}
  }
}`

	readFileSchema := `{
  "type": "object",
  "required": ["file_path"],
  "properties": {
    "file_path": {"type": "string", "description": "Path to file to read"},
    "start_line": {"type": "integer", "minimum": 1, "description": "First line to read (default 1)"},
    "end_line": {"type": "integer", "minimum": 1, "description": "Last line to read (default 200)"},
    "working_dir": {"type": "string", "description": "Use 'current' or '.' for session working directory (default if omitted)"},
    "query": {"type": "string", "description": "Optional context about why you're reading this file"}
  }
}`

	listDirectorySchema := `{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "Directory path to inspect. Defaults to the current working directory when omitted."},
		"max_items": {"type": "integer", "minimum": 1, "description": "Maximum number of entries to return before truncating the listing."},
		"working_dir": {"type": "string", "description": "Use 'current' or '.' for session working directory (default if omitted)"},
		"query": {"type": "string", "description": "Optional context about why you're listing this directory"}
	}
}`

	searchSchema := `{
  "type": "object",
  "required": ["query", "working_dir"],
  "properties": {
    "query": {"type": "string", "description": "Task description"},
    "search": {"type": "string", "description": "Search query for patterns or code (optional)"},
    "working_dir": {"type": "string", "description": "Use 'current' or '.' for session working directory. Never pass absolute paths."}
  }
}`
	allowDiscoveryTools := p.shouldExposeInvestigationTools()
	tools := []capabilities.ToolDefinition{{
		Name:        p.phaseToolName("run_tests"),
		Description: "Runs tests and validation commands. Returns structured test results with error details.",
		Schema:      taskSchema,
	}, {
		// read_file and search_repo stay available even after diagnosis is complete:
		// preview_patch/apply_patch require exact current file content for old_content, and the
		// captured diagnosis often doesn't include an exact line number - without search_repo the
		// model has no cheap way to locate it and gets stuck re-guessing read_file ranges or
		// stalling to ask the user. Only list_directory and whole-repo scans get hidden once
		// diagnosis is captured, since those don't help pinpoint an already-known file's content.
		Name:        p.phaseToolName("read_file"),
		Description: "Reads file content from repository. For files longer than ~200 lines, call search_repo FIRST to find the exact symbol/line number, then read a small range around it. Do not page through a large file sequentially with repeated calls.",
		Schema:      readFileSchema,
	}, {
		Name:        p.phaseToolName("search_repo"),
		Description: "Searches repository for code patterns using regex. Returns matching code snippets with line numbers.",
		Schema:      searchSchema,
	}}

	if !allowDiscoveryTools {
		return tools
	}

	// Narrow, targeted tool: safe to keep even in execution, since it lists a specific
	// directory instead of scanning the whole repository.
	tools = append(tools, capabilities.ToolDefinition{
		Name:        p.phaseToolName("list_directory"),
		Description: "Lists files and folders in a repository directory path only. Use this to discover unknown sibling files or directories; do not pass a filename.",
		Schema:      listDirectorySchema,
	})

	// Whole-repo scan tools are expensive (can return hundreds of KB) and belong to
	// investigation/planning. Execution already has the investigation context; re-running
	// these here just burns the tool budget without moving toward a patch.
	if p.phaseID == "execution" {
		return tools
	}

	return append(tools, []capabilities.ToolDefinition{{
		Name:        p.phaseToolName("build_context"),
		Description: "Builds comprehensive repository context including structure, files, and dependencies.",
		Schema:      taskSchema,
	}, {
		Name:        p.phaseToolName("inspect_repo"),
		Description: "Inspects current repository state including git status, uncommitted changes, and diffs.",
		Schema:      taskSchema,
	}, {
		Name:        p.phaseToolName("list_changed_files"),
		Description: "Lists files with uncommitted changes in the repository.",
		Schema:      taskSchema,
	}, {
		Name:        p.phaseToolName("index_repo_structure"),
		Description: "Indexes repository structure using AST analysis. Returns function names, imports, and call relationships.",
		Schema:      taskSchema,
	}, {
		Name:        p.phaseToolName("compile_artifact_inventory"),
		Description: "Compiles a structured inventory of code artifacts from repository context and stores it in the active workstream. Returns files, symbols, tests, configs, and dependency clusters.",
		Schema:      taskSchema,
	}, {
		Name:        p.phaseToolName("select_inventory_item"),
		Description: "Selects a single inventory item as the current active target so later turns can work on that item only.",
		Schema: `{
  "type": "object",
  "required": ["item_id"],
  "properties": {
    "item_id": {"type": "string", "description": "Inventory item id or display name"}
  }
}`,
	}, {
		Name:        p.phaseToolName("complete_inventory_item"),
		Description: "Marks a single inventory item as completed and advances to the next pending item when available.",
		Schema: `{
  "type": "object",
  "required": ["item_id"],
  "properties": {
    "item_id": {"type": "string", "description": "Inventory item id or display name"}
  }
}`,
	}}...)
}

// shouldExposeInvestigationTools reports whether list_directory and whole-repo scan tools
// should stay exposed. read_file and search_repo are intentionally NOT gated by this: patch
// generation needs exact current file content for old_content, and the captured diagnosis
// often lacks a precise line number, so hiding either once diagnosis is captured leaves the
// model unable to locate or fetch the exact patch region and it stalls instead of patching.
func (p *CodingPhase) shouldExposeInvestigationTools() bool {
	if p == nil || p.phaseID != "execution" {
		return true
	}
	// The capture tool persists via SharedInfrastructure.Memory under "phase-artifact:<phaseID>"
	// (see runCapture/loadExistingArtifact); WorkStreamContext.PhaseArtifacts (below) is a
	// separate map that capture never writes to, so checking it alone always sees it as empty
	// and never hides these tools. Check the real persisted artifact first.
	if diagnosisComplete, ok := p.diagnosisCompleteFromPersistedArtifact(); ok {
		return !diagnosisComplete
	}
	if p.sharedInfra == nil || p.sharedInfra.STMAccessor == nil {
		return true
	}
	stm := p.sharedInfra.STMAccessor()
	if stm == nil {
		return true
	}
	thread := stm.GetCurrentThread()
	if thread == nil {
		return true
	}
	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return true
	}
	phaseArtifacts := streamCtx.GetPhaseArtifacts()
	if len(phaseArtifacts) == 0 {
		return true
	}
	artifact, ok := phaseArtifacts[p.phaseID]
	if !ok || len(artifact) == 0 {
		return true
	}
	structuredData, _ := artifact["structured_data"].(map[string]any)
	if diagnosis, ok := structuredData["diagnosis"].(map[string]any); ok {
		structuredData = diagnosis
	}
	if len(diagnosisFieldsMissing(structuredData, readPhaseStringSliceAsAny(artifact["evidence"]))) > 0 {
		return true
	}
	return false
}

// diagnosisCompleteFromPersistedArtifact loads the phase's real persisted artifact (the same
// store runCapture writes to) and reports whether it carries a complete diagnosis. The second
// return value is false when no persisted artifact is available, so callers can fall back to
// other sources instead of treating "no artifact" as "diagnosis complete".
func (p *CodingPhase) diagnosisCompleteFromPersistedArtifact() (complete bool, found bool) {
	artifact, err := p.loadExistingArtifact()
	if err != nil || artifact.PhaseID == "" {
		return false, false
	}
	structuredData := artifact.StructuredData
	if diagnosis, ok := structuredData["diagnosis"].(map[string]any); ok {
		structuredData = diagnosis
	}
	return len(diagnosisFieldsMissing(structuredData, artifact.Evidence)) == 0, true
}

func readPhaseStringSliceAsAny(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
		return out
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	default:
		trimmed := strings.TrimSpace(fmt.Sprint(v))
		if trimmed == "" || trimmed == "<nil>" {
			return nil
		}
		return []string{trimmed}
	}
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
		// No phase-specific tools (universal tools available)
		return nil

	case "investigation":
		// All investigation tools now exposed as universal tools
		return nil

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
			Name:        p.phaseToolName("plan_changes"),
			Description: "Generates execution plan. Use tests or validation cases as a design aid when they help specify contracts, interfaces, or expected behavior; avoid making TDD mandatory for exploratory work.",
			Schema:      taskSchema,
		}}

	case "testing":
		// All testing tools now exposed as universal tools
		return nil

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
		}}

	case "review":
		// All review tools now exposed as universal tools
		return nil

	case "delivery":
		// All delivery tools now exposed as universal tools
		return nil

	default:
		return nil
	}
}

func (p *CodingPhase) BuildInstructions(query string, state capabilities.RoutingState) string {
	return p.BasePhase.BuildInstructions(query, state)
}

func (p *CodingPhase) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = state

	log.Debug("[PHASE EXECUTE] CodingPhase tool execution started",
		"tool_name", toolName,
		"phase", p.phaseID,
		"args_keys", func() []string {
			keys := make([]string, 0, len(args))
			for k := range args {
				keys = append(keys, k)
			}
			return keys
		}())

	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		log.Error("[PHASE EXECUTE] Empty tool name")
		return "", fmt.Errorf("empty tool name")
	}

	if name == strings.ToLower(p.captureTool) {
		log.Debug("[PHASE EXECUTE] Executing capture tool")
		return p.runCapture(args)
	}
	if name == strings.ToLower(p.stateTool) {
		log.Debug("[PHASE EXECUTE] Executing state tool")
		return p.runState(args)
	}

	log.Debug("[PHASE EXECUTE] Matching tool name",
		"normalized_name", name,
		"expected_inspect_repo", strings.ToLower(p.phaseToolName("inspect_repo")),
		"expected_run_tests", strings.ToLower(p.phaseToolName("run_tests")))

	switch name {
	case strings.ToLower(p.phaseToolName("assess_and_route")):
		log.Debug("[PHASE EXECUTE] Executing assess_and_route")
		return p.runAssessAndRoute(args)
	case strings.ToLower(p.phaseToolName("create_succession_path")):
		log.Debug("[PHASE EXECUTE] Executing create_succession_path")
		return p.runCreateSuccessionPath(args)
	case strings.ToLower(p.phaseToolName("build_context")):
		log.Debug("[PHASE EXECUTE] Executing build_context")
		return p.runBuildContext(ctx, args)
	case strings.ToLower(p.phaseToolName("inspect_repo")):
		log.Debug("[PHASE EXECUTE] Executing inspect_repo")
		return p.runInspectRepo(ctx, args)
	case strings.ToLower(p.phaseToolName("search_repo")):
		return p.runSearchRepo(ctx, args)
	case strings.ToLower(p.phaseToolName("list_changed_files")):
		return p.runListChangedFiles(ctx, args)
	case strings.ToLower(p.phaseToolName("read_file")):
		return p.runReadFile(ctx, args)
	case strings.ToLower(p.phaseToolName("list_directory")):
		return p.runListDirectory(ctx, args)
	case strings.ToLower(p.phaseToolName("index_repo_structure")):
		return p.runIndexRepoStructure(ctx, args)
	case strings.ToLower(p.phaseToolName("compile_artifact_inventory")):
		return p.runCompileArtifactInventory(ctx, args)
	case strings.ToLower(p.phaseToolName("select_inventory_item")):
		return p.runSelectInventoryItem(ctx, args)
	case strings.ToLower(p.phaseToolName("complete_inventory_item")):
		return p.runCompleteInventoryItem(ctx, args)
	case strings.ToLower(p.phaseToolName("plan_changes")):
		return p.runPlanChanges(ctx, args)
	case strings.ToLower(p.phaseToolName("run_tests")):
		log.Debug("[PHASE EXECUTE] Executing run_tests")
		return p.runTests(ctx, args)
	case strings.ToLower(p.phaseToolName("preview_patch")):
		log.Debug("[PHASE EXECUTE] Executing preview_patch")
		// Metrics recorded via copilot_prompt.go logging (Aug 31, 2026)
		return p.runPreviewPatch(ctx, args)
	case strings.ToLower(p.phaseToolName("apply_patch")):
		log.Debug("[PHASE EXECUTE] Executing apply_patch")
		return p.runApplyPatch(ctx, args)
	case strings.ToLower(p.phaseToolName("apply_multi_file_patch")):
		log.Debug("[PHASE EXECUTE] Executing apply_multi_file_patch")
		return p.runApplyMultiFilePatch(ctx, args)
	default:
		log.Error("[PHASE EXECUTE] Unsupported tool",
			"tool_name", toolName,
			"normalized_name", name)
		return "", fmt.Errorf("unsupported tool %q in coding workflow", toolName)
	}
}

// Tool execution methods

func (p *CodingPhase) buildTask(ctx context.Context, args map[string]any) Task {
	query := readPhaseString(args, "query", "")
	workingDir := readPhaseString(args, "working_dir", "")
	activeFile := readPhaseString(args, "active_file", "")

	if activeFile == "" {
		if _, snapshot, err := p.loadArtifactInventorySnapshot(); err == nil && snapshot != nil {
			if resolved := resolveTaskActiveFileFromInventory(cw.Task{ActiveFile: activeFile, Description: query}, snapshot); resolved != "" {
				activeFile = resolved
			}
		}
	}

	// Normalize and resolve working directory using session context
	workingDir = cw.NormalizeRepoAlias(workingDir)
	if ctx != nil {
		workingDir = cw.ResolveContextWorkingDir(ctx, workingDir)
	}

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

func (p *CodingPhase) runBuildContext(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
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

func (p *CodingPhase) runInspectRepo(ctx context.Context, args map[string]any) (string, error) {
	log.Info("[runInspectRepo] Starting inspection", "args", args)
	task := p.buildTask(ctx, args)
	log.Info("[runInspectRepo] Task built",
		"working_dir", task.WorkingDir,
		"description", task.Description)

	infra := p.ensureSharedInfrastructure()
	if infra == nil {
		log.Error("[runInspectRepo] Infrastructure is nil")
		return "", fmt.Errorf("infrastructure not initialized")
	}

	repoOps := infra.NewRepoOperations()
	if repoOps == nil {
		log.Error("[runInspectRepo] RepoOperations is nil")
		return "", fmt.Errorf("repo operations not initialized")
	}

	log.Info("[runInspectRepo] Calling InspectRepo",
		"working_dir", task.WorkingDir)
	summary, err := repoOps.InspectRepo(task, task.State)
	if err != nil {
		log.Error("[runInspectRepo] InspectRepo failed",
			"error", err,
			"working_dir", task.WorkingDir)
		return "", fmt.Errorf("inspect repo: %w", err)
	}
	log.Info("[runInspectRepo] InspectRepo succeeded",
		"changed_files_count", len(summary.ChangedFiles))

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

func (p *CodingPhase) runSearchRepo(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
	infra := p.ensureSharedInfrastructure()

	// Resolve repo working directory using exported helper
	task, _ = cw.ResolveRepoWorkingDir(ctx, task)

	repoOps := infra.NewRepoOperations()
	searchQuery := readPhaseString(args, "search", "")
	if searchQuery == "" {
		searchQuery = task.Description
	}

	results, err := repoOps.SearchRepo(task, task.State, searchQuery)
	if err != nil {
		return "", fmt.Errorf("search repo: %w", err)
	}

	output := "Search Results: none"
	if len(results) > 0 {
		output = fmt.Sprintf("Search Results:\n%s", strings.Join(results, "\n"))
	}
	if history := p.recordAndFormatSearchHistory(searchQuery); history != "" {
		output += "\n\n" + history
	}
	return output, nil
}

// searchHistoryMemoryKey scopes prior search queries to this phase so repeated
// searches in the same execution can be flagged instead of silently repeating.
func (p *CodingPhase) searchHistoryMemoryKey() string {
	return "search-repo-history:" + p.phaseID
}

// recordAndFormatSearchHistory persists the just-run search query and warns when
// three or more searches have run in this phase without reaching a patch, since
// that pattern burns the tool budget on exploration instead of converging on a fix.
func (p *CodingPhase) recordAndFormatSearchHistory(searchQuery string) string {
	infra := p.ensureSharedInfrastructure()
	if infra.Memory == nil {
		return ""
	}

	key := p.searchHistoryMemoryKey()
	var queries []string
	if stored, err := infra.Memory.Load(key); err == nil {
		if raw, ok := stored["queries_json"]; ok && raw != "" {
			_ = json.Unmarshal([]byte(raw), &queries)
		}
	}

	queries = append(queries, searchQuery)

	if encoded, err := json.Marshal(queries); err == nil {
		_ = infra.Memory.Save(key, map[string]string{"queries_json": string(encoded)})
	}

	if len(queries) < 3 {
		return ""
	}
	return fmt.Sprintf("This phase has already run %d searches this turn cycle: %s. If the target symbol/line is known, stop searching and generate the patch now.", len(queries), strings.Join(queries, " | "))
}

func (p *CodingPhase) runListChangedFiles(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
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

func (p *CodingPhase) runReadFile(ctx context.Context, args map[string]any) (string, error) {
	task, _ := cw.ResolveRepoWorkingDir(ctx, p.buildTask(ctx, args))

	// Extract file_path
	filePath := readPhaseString(args, "file_path", "")
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	// Strip workspace-relative prefix if present to prevent path doubling
	// Search results return paths like "lib/sop/tools/sopbackend/auth.go" (relative to workspace root)
	// but working_dir is already "/Volumes/T7/sop-cloud/lib/sop"
	// Joining creates: /Volumes/T7/sop-cloud/lib/sop + lib/sop/tools/... = /lib/sop/lib/sop/tools/... (DOUBLED!)
	//
	// Solution: Find how many trailing components of WorkingDir match the beginning of filePath
	// Example: WorkingDir="/a/b/c/d", filePath="c/d/file.go" → strip "c/d/" → result="file.go"
	workDirParts := strings.Split(filepath.Clean(task.WorkingDir), string(filepath.Separator))
	pathParts := strings.Split(filepath.Clean(filePath), string(filepath.Separator))

	// Find longest suffix of WorkingDir that matches prefix of filePath
	stripCount := 0
	for i := 1; i <= len(workDirParts) && i <= len(pathParts); i++ {
		// Check if last i parts of WorkingDir match first i parts of filePath
		match := true
		for j := 0; j < i; j++ {
			if workDirParts[len(workDirParts)-i+j] != pathParts[j] {
				match = false
				break
			}
		}
		if match {
			stripCount = i
		}
	}

	// Strip the matching prefix from filePath
	if stripCount > 0 {
		filePath = filepath.Join(pathParts[stripCount:]...)
	}

	// Build absolute path. For bare filenames, prefer the active file's
	// directory first so sibling files like auth.go/auth_test.go resolve there
	// before falling back to the repo root join.
	absPath := resolveReadFilePath(task, filePath)

	// Read entire file
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filePath, err)
	}

	// Parse line range
	startLine := 1
	if start, ok := args["start_line"]; ok {
		switch v := start.(type) {
		case int:
			startLine = v
		case float64:
			startLine = int(v)
		}
	}
	if startLine < 1 {
		startLine = 1
	}

	endLine := 200
	if end, ok := args["end_line"]; ok {
		switch v := end.(type) {
		case int:
			endLine = v
		case float64:
			endLine = int(v)
		}
	}

	// Split into lines
	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// Adjust end_line if not specified or exceeds file
	if endLine == 200 && totalLines < 200 {
		endLine = totalLines
	} else if endLine > totalLines {
		endLine = totalLines
	}

	if endLine < startLine {
		endLine = startLine
	}

	// Build output with line numbers
	var result strings.Builder
	result.WriteString(fmt.Sprintf("File: %s (lines %d-%d of %d)\n", filePath, startLine, endLine, totalLines))

	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		result.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
	}

	if endLine < totalLines {
		result.WriteString(fmt.Sprintf("\n(File continues to line %d. Use read_file with end_line=%d to see more)\n", totalLines, totalLines))
	}

	if history := p.recordAndFormatReadHistory(filePath, startLine, endLine); history != "" {
		result.WriteString("\n")
		result.WriteString(history)
	}

	return result.String(), nil
}

// readHistoryMemoryKey scopes already-read line ranges to this phase so later turns
// in the same execution can see what was covered instead of re-reading from scratch.
func (p *CodingPhase) readHistoryMemoryKey() string {
	return "read-file-history:" + p.phaseID
}

// recordAndFormatReadHistory persists the just-read range (system-tracked, not
// dependent on the model remembering to report it) and returns a summary of every
// range read so far for this file in this phase, so the model can see gaps instead
// of paging sequentially through a large file.
func (p *CodingPhase) recordAndFormatReadHistory(filePath string, startLine, endLine int) string {
	infra := p.ensureSharedInfrastructure()
	if infra.Memory == nil {
		return ""
	}

	key := p.readHistoryMemoryKey()
	history := map[string][]string{}
	if stored, err := infra.Memory.Load(key); err == nil {
		if raw, ok := stored["history_json"]; ok && raw != "" {
			_ = json.Unmarshal([]byte(raw), &history)
		}
	}

	history[filePath] = append(history[filePath], fmt.Sprintf("%d-%d", startLine, endLine))

	if encoded, err := json.Marshal(history); err == nil {
		_ = infra.Memory.Save(key, map[string]string{"history_json": string(encoded)})
	}

	ranges, ok := history[filePath]
	if !ok || len(ranges) <= 1 {
		return ""
	}
	return fmt.Sprintf("Already read in %s this phase: lines %s. Do not re-read these ranges; use search_repo to jump to the remaining symbol/lines.", filePath, strings.Join(ranges, ", "))
}

func (p *CodingPhase) runListDirectory(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
	task, _ = cw.ResolveRepoWorkingDir(ctx, task)

	dirPath := readPhaseString(args, "path", "")
	maxItems := 200
	if raw, ok := args["max_items"]; ok {
		switch v := raw.(type) {
		case int:
			maxItems = v
		case float64:
			maxItems = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				maxItems = int(parsed)
			}
		}
	}
	if maxItems <= 0 {
		maxItems = 200
	}
	resolvedPath := resolveListDirectoryPath(task, dirPath)
	if resolvedPath == "" {
		return "", fmt.Errorf("list directory: empty path")
	}

	entries, err := os.ReadDir(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("list directory %s: %w", dirPath, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	truncated := len(names) > maxItems
	if truncated {
		names = names[:maxItems]
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Directory contents for %s (%d item(s) shown)\n", resolvedPath, len(names)))
	for _, name := range names {
		result.WriteString(fmt.Sprintf("- %s\n", name))
	}
	if truncated {
		result.WriteString(fmt.Sprintf("\n(Output truncated at %d items. Use a narrower path or inspect subdirectories.)\n", maxItems))
	}

	return strings.TrimSpace(result.String()), nil
}

func stripRepeatedWorkspacePrefix(workingDir, candidate string) string {
	workingDir = filepath.Clean(strings.TrimSpace(workingDir))
	candidate = strings.TrimSpace(candidate)
	if workingDir == "" || candidate == "" {
		return candidate
	}
	if filepath.IsAbs(candidate) {
		return candidate
	}

	workingParts := splitPathParts(workingDir)
	candidateParts := splitPathParts(candidate)
	if len(workingParts) == 0 || len(candidateParts) == 0 {
		return candidate
	}

	maxMatch := min(len(workingParts), len(candidateParts))
	for i := maxMatch; i > 0; i-- {
		matched := true
		for j := 0; j < i; j++ {
			if workingParts[len(workingParts)-i+j] != candidateParts[j] {
				matched = false
				break
			}
		}
		if matched {
			if i == len(candidateParts) {
				return "."
			}
			return filepath.Join(candidateParts[i:]...)
		}
	}
	return candidate
}

func splitPathParts(value string) []string {
	cleaned := filepath.Clean(strings.TrimSpace(value))
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return nil
	}
	parts := strings.Split(cleaned, string(filepath.Separator))
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "." {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func resolveReadFilePath(task cw.Task, filePath string) string {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}

	candidatePath := stripRepeatedWorkspacePrefix(task.WorkingDir, trimmed)
	if !strings.Contains(candidatePath, string(filepath.Separator)) && strings.TrimSpace(task.ActiveFile) != "" {
		activeDir := filepath.Dir(strings.TrimSpace(task.ActiveFile))
		if activeDir != "" && activeDir != "." {
			activeDir = stripRepeatedWorkspacePrefix(task.WorkingDir, activeDir)
			candidate := filepath.Clean(filepath.Join(task.WorkingDir, activeDir, candidatePath))
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	if task.WorkingDir != "" {
		return filepath.Clean(filepath.Join(task.WorkingDir, candidatePath))
	}
	return filepath.Clean(candidatePath)
}

func resolveListDirectoryPath(task cw.Task, dirPath string) string {
	trimmed := strings.TrimSpace(dirPath)
	if trimmed == "" || trimmed == "." {
		if strings.TrimSpace(task.WorkingDir) != "" {
			return filepath.Clean(task.WorkingDir)
		}
		return "."
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	trimmed = stripRepeatedWorkspacePrefix(task.WorkingDir, trimmed)
	if strings.TrimSpace(task.WorkingDir) != "" {
		return filepath.Clean(filepath.Join(task.WorkingDir, trimmed))
	}
	return filepath.Clean(trimmed)
}

func (p *CodingPhase) runIndexRepoStructure(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
	resolvedTask, _ := cw.ResolveRepoWorkingDir(ctx, task)
	if resolvedTask.WorkingDir == "" {
		resolvedTask.WorkingDir = task.WorkingDir
	}

	// Use context pack operations for repo structure
	infra := p.ensureSharedInfrastructure()
	packOps := infra.NewContextPackOperations()
	pack, err := packOps.BuildContextForTask(resolvedTask, resolvedTask.State)
	if err != nil {
		return "", fmt.Errorf("index repository: %w", err)
	}

	return formatContextPack(pack), nil
}

func (p *CodingPhase) runCompileArtifactInventory(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
	infra := p.ensureSharedInfrastructure()

	packOps := infra.NewContextPackOperations()
	pack, err := packOps.BuildContextForTask(task, task.State)
	if err != nil {
		return "", fmt.Errorf("build context for inventory: %w", err)
	}

	inventory := p.buildArtifactInventory(task, pack)
	if err := p.persistArtifactInventorySnapshot(inventory); err != nil {
		log.Warn("[runCompileArtifactInventory] failed to persist inventory snapshot", "error", err)
	}

	return p.formatArtifactInventoryResponse(inventory), nil
}

func (p *CodingPhase) runSelectInventoryItem(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	itemID := strings.TrimSpace(readPhaseString(args, "item_id", ""))
	if itemID == "" {
		return "", fmt.Errorf("item_id is required")
	}

	phaseKey, inventory, err := p.loadArtifactInventorySnapshot()
	if err != nil {
		return "", err
	}
	if inventory == nil {
		return "", fmt.Errorf("no artifact inventory has been compiled yet")
	}

	if !selectArtifactInventoryItem(inventory, itemID) {
		return "", fmt.Errorf("inventory item %q not found", itemID)
	}
	if err := p.persistArtifactInventorySnapshotForPhase(phaseKey, inventory); err != nil {
		log.Warn("[runSelectInventoryItem] failed to persist inventory snapshot", "error", err)
	}

	return p.formatArtifactInventoryResponse(inventory), nil
}

func (p *CodingPhase) runCompleteInventoryItem(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	itemID := strings.TrimSpace(readPhaseString(args, "item_id", ""))
	if itemID == "" {
		return "", fmt.Errorf("item_id is required")
	}

	phaseKey, inventory, err := p.loadArtifactInventorySnapshot()
	if err != nil {
		return "", err
	}
	if inventory == nil {
		return "", fmt.Errorf("no artifact inventory has been compiled yet")
	}

	if !completeArtifactInventoryItem(inventory, itemID) {
		return "", fmt.Errorf("inventory item %q not found", itemID)
	}
	if err := p.persistArtifactInventorySnapshotForPhase(phaseKey, inventory); err != nil {
		log.Warn("[runCompleteInventoryItem] failed to persist inventory snapshot", "error", err)
	}

	return p.formatArtifactInventoryResponse(inventory), nil
}

func (p *CodingPhase) runPlanChanges(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
	if task.ActiveFile == "" {
		if _, snapshot, err := p.loadArtifactInventorySnapshot(); err == nil && snapshot != nil {
			if activeFile := resolveTaskActiveFileFromInventory(task, snapshot); activeFile != "" {
				task.ActiveFile = activeFile
			}
		}
	}
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

func resolveTaskActiveFileFromInventory(task cw.Task, snapshot map[string]any) string {
	if task.ActiveFile != "" {
		return normalizeInventoryPath(task.ActiveFile)
	}
	if len(snapshot) == 0 {
		return ""
	}
	for _, item := range snapshotItems(snapshot) {
		if activeID, _ := snapshot["active_item_id"].(string); activeID != "" && inventoryItemID(item) == activeID {
			if path := inventoryItemFilePath(item); path != "" {
				return normalizeInventoryPath(path)
			}
		}
		if status, _ := item["status"].(string); status == "active" {
			if path := inventoryItemFilePath(item); path != "" {
				return normalizeInventoryPath(path)
			}
		}
	}
	return ""
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

func diagnosisEvidenceStrings(structuredData map[string]any) []string {
	if structuredData == nil {
		return nil
	}
	candidate := map[string]any{}
	if nested, ok := structuredData["diagnosis"].(map[string]any); ok {
		candidate = nested
	} else {
		candidate = structuredData
	}
	if evidence, ok := candidate["evidence"]; ok {
		switch v := evidence.(type) {
		case []string:
			return v
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if item == nil {
					continue
				}
				out = append(out, fmt.Sprint(item))
			}
			return out
		case string:
			if strings.TrimSpace(v) != "" {
				return []string{v}
			}
		}
	}
	return nil
}

func (p *CodingPhase) runPreviewPatch(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
	infra := p.ensureSharedInfrastructure()

	var diagnosisData map[string]any
	if structData, ok := args["structured_data"].(map[string]any); ok {
		diagnosisData = structData
	}
	var topLevelEvidence []string
	if artifact, err := p.loadExistingArtifact(); err == nil {
		topLevelEvidence = artifact.Evidence
		if diagnosisData == nil && artifact.StructuredData != nil {
			diagnosisData = artifact.StructuredData
		}
	}

	if isRepairTaskForDiagnosis(task.Description, nil, nil) {
		if missing := diagnosisFieldsMissing(diagnosisData, topLevelEvidence); len(missing) > 0 {
			message := fmt.Sprintf("DIAGNOSIS REQUIRED\nRepair task detected: %s\nMissing diagnosis fields: %s\nAdd structured_data.diagnosis with root_cause, evidence, fix_target, shared_with, and blast_radius before previewing the patch.",
				task.Description,
				strings.Join(missing, ", "))
			log.Warn("Phase D hard gate: repair task preview_patch missing diagnosis values",
				"phase", p.phaseID,
				"task", task.Description,
				"missing_fields", missing)
			return "", fmt.Errorf("%s", message)
		}
		if clusters := clusterFailureEvidence(diagnosisEvidenceStrings(diagnosisData)); len(clusters) == 0 {
			message := fmt.Sprintf("DIAGNOSIS REQUIRED\nRepair task detected: %s\nMissing shared failure clustering: root cause must describe which sibling failures are affected (shared_with) and the blast radius to assess downstream impact.", task.Description)
			log.Warn("Phase D hard gate: repair task missing shared failure evidence",
				"phase", p.phaseID,
				"task", task.Description)
			return "", fmt.Errorf("%s", message)
		}
	}

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

func (p *CodingPhase) runApplyPatch(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
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

func (p *CodingPhase) runApplyMultiFilePatch(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
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

func (p *CodingPhase) runTests(ctx context.Context, args map[string]any) (string, error) {
	task := p.buildTask(ctx, args)
	infra := p.ensureSharedInfrastructure()

	// Resolve repo working directory using exported helper
	task, _ = cw.ResolveRepoWorkingDir(ctx, task)

	testOps := infra.NewTestOperations()
	result, err := testOps.RunTests(ctx, task, task.State)
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

func (p *CodingPhase) buildArtifactInventory(task cw.Task, pack cw.ContextPack) map[string]any {
	items := make([]map[string]any, 0, 32)
	seen := make(map[string]struct{})
	fileBuckets := make(map[string][]string)
	counts := map[string]int{}
	activeFile := normalizeInventoryPath(task.ActiveFile)
	objective := strings.TrimSpace(task.Description)
	objectiveProfile := inferObjectiveProfile(objective)

	addItem := func(itemType, id, displayName, description string, metadata map[string]any) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		item := map[string]any{
			"id":           id,
			"type":         itemType,
			"display_name": displayName,
			"status":       "pending",
		}
		if description != "" {
			item["description"] = description
		}
		if len(metadata) > 0 {
			item["metadata"] = metadata
		}
		items = append(items, item)
		seen[id] = struct{}{}
		counts[itemType]++
		if filePath, ok := metadata["file_path"].(string); ok && filePath != "" {
			bucket := filepath.Dir(filePath)
			bucket = strings.TrimSpace(filepath.ToSlash(bucket))
			if bucket == "." {
				bucket = ""
			}
			if !containsString(fileBuckets[bucket], filePath) {
				fileBuckets[bucket] = append(fileBuckets[bucket], filePath)
			}
		}
	}

	for _, path := range taskInventoryFiles(task, pack) {
		if isInventoryInterfaceFile(path) {
			metadata := map[string]any{
				"file_path": path,
				"extension": strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
			}
			if activeFile != "" && normalizeInventoryPath(path) == activeFile {
				metadata["role"] = "active_file"
			}
			addItem("interface", "interface:"+path, path, inventoryDescriptionForFile(path, "interface"), metadata)
		}
		itemType := inventoryFileType(path)
		if itemType == "" {
			continue
		}
		metadata := map[string]any{
			"file_path": path,
			"extension": strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		}
		if activeFile != "" && normalizeInventoryPath(path) == activeFile {
			metadata["role"] = "active_file"
		}
		addItem(itemType, itemType+":"+path, path, inventoryDescriptionForFile(path, itemType), metadata)
	}

	for _, symbol := range uniqueStrings(append(append([]string{}, pack.Symbols...), pack.ASTLeafNodes...)) {
		symbolName := strings.TrimSpace(symbol)
		if symbolName == "" {
			continue
		}
		metadata := map[string]any{"symbol": symbolName}
		if activeFile != "" {
			metadata["file_path"] = activeFile
		}
		if strings.TrimSpace(pack.StructuralPath) != "" {
			metadata["structural_path"] = pack.StructuralPath
		}
		addItem("symbol", "symbol:"+normalizeInventoryPath(pack.ActiveFile)+":"+symbolName, symbolName, "Code symbol discovered in repository context", metadata)
	}

	if relationshipItem := buildArtifactRelationshipItem(task, pack); relationshipItem != nil {
		addItem("relationship", inventoryItemID(relationshipItem), inventoryItemDisplayName(relationshipItem), "Repository relationship grouping for the active artifact", relationshipItem["metadata"].(map[string]any))
	}

	if executionSurface := buildArtifactExecutionSurface(task, pack); executionSurface != nil {
		addItem("execution_surface", inventoryItemID(executionSurface), inventoryItemDisplayName(executionSurface), "Execution surface for the active artifact and its validation path", executionSurface["metadata"].(map[string]any))
	}

	for bucket, files := range fileBuckets {
		if len(files) >= 2 {
			packageName := bucket
			if packageName == "" {
				packageName = "."
			}
			metadata := map[string]any{
				"files":         append([]string(nil), files...),
				"file_count":    len(files),
				"package_name":  packageName,
				"package_scope": packageName,
			}
			addItem("package", "package:"+packageName, packageName, "Package-scoped work area for related repository files", metadata)
		}
	}

	for bucket, files := range fileBuckets {
		if len(files) < 2 {
			continue
		}
		sortedFiles := append([]string(nil), files...)
		sort.Strings(sortedFiles)
		displayName := bucket
		if displayName == "" {
			displayName = "."
		}
		metadata := map[string]any{
			"files":       sortedFiles,
			"file_count":  len(sortedFiles),
			"bucket_path": bucket,
		}
		addItem("cluster", "cluster:"+displayName, displayName, "Dependency cluster for related files", metadata)
	}

	activeID := ""
	if activeFile != "" {
		for _, item := range items {
			if itemPath := inventoryItemFilePath(item); itemPath != "" && normalizeInventoryPath(itemPath) == activeFile {
				activeID = inventoryItemID(item)
				break
			}
		}
	}
	if activeID == "" && len(items) > 0 {
		activeID = inventoryItemID(items[0])
	}
	setActiveInventoryItem(items, activeID)

	return map[string]any{
		"inventory_type":    "code_artifacts",
		"objective":         objective,
		"objective_profile": objectiveProfile,
		"active_item_id":    activeID,
		"captured_at":       time.Now().UnixMilli(),
		"items":             items,
		"summary": map[string]any{
			"total_items": len(items),
			"counts":      counts,
		},
	}
}

func inferObjectiveProfile(objective string) string {
	lower := strings.ToLower(strings.TrimSpace(objective))
	switch {
	case lower == "":
		return "analysis"
	case strings.Contains(lower, "fix failing tests"), strings.Contains(lower, "failing tests"), strings.Contains(lower, "test failure"), strings.Contains(lower, "repair"), strings.Contains(lower, "regression"), strings.Contains(lower, "bug"), strings.Contains(lower, "error"):
		return "repair"
	case strings.Contains(lower, "design"), strings.Contains(lower, "architect"), strings.Contains(lower, "plan"), strings.Contains(lower, "proposal"):
		return "design"
	case strings.Contains(lower, "refactor"), strings.Contains(lower, "cleanup"), strings.Contains(lower, "simplify"):
		return "refactor"
	case strings.Contains(lower, "analyze"), strings.Contains(lower, "inspect"), strings.Contains(lower, "understand"), strings.Contains(lower, "map the repo"), strings.Contains(lower, "repository overview"):
		return "analysis"
	default:
		return "implementation"
	}
}

func buildArtifactRelationshipItem(task cw.Task, pack cw.ContextPack) map[string]any {
	activeFile := normalizeInventoryPath(task.ActiveFile)
	if activeFile == "" {
		activeFile = normalizeInventoryPath(pack.ActiveFile)
	}
	if activeFile == "" {
		return nil
	}

	related := uniqueStrings(append([]string{}, pack.RelatedFiles...))
	related = append(related, uniqueStrings(pack.DependencyFiles)...)
	related = append(related, uniqueStrings(pack.TestFiles)...)
	related = append(related, activeFile)
	related = uniqueStrings(related)
	sort.Strings(related)
	if len(related) < 2 {
		return nil
	}

	displayName := strings.TrimSuffix(filepath.Base(activeFile), filepath.Ext(activeFile))
	if displayName == "" {
		displayName = "artifact"
	}
	baseScope := filepath.Dir(activeFile)
	if baseScope == "." || baseScope == "" {
		baseScope = "root"
	}
	metadata := map[string]any{
		"file_path":      activeFile,
		"related_files":  related,
		"scope":          filepath.ToSlash(baseScope),
		"relation_count": len(related),
	}
	return map[string]any{
		"id":           "relationship:" + activeFile,
		"type":         "relationship",
		"display_name": displayName,
		"status":       "pending",
		"metadata":     metadata,
	}
}

func buildArtifactExecutionSurface(task cw.Task, pack cw.ContextPack) map[string]any {
	activeFile := normalizeInventoryPath(task.ActiveFile)
	if activeFile == "" {
		activeFile = normalizeInventoryPath(pack.ActiveFile)
	}
	if activeFile == "" {
		return nil
	}

	deps := append([]string(nil), uniqueStrings(pack.DependencyFiles)...)
	tests := append([]string(nil), uniqueStrings(pack.TestFiles)...)
	if len(deps) == 0 && len(tests) == 0 {
		return nil
	}

	displayName := strings.TrimSuffix(filepath.Base(activeFile), filepath.Ext(activeFile))
	if displayName == "" {
		displayName = "execution surface"
	}
	metadata := map[string]any{
		"file_path":        activeFile,
		"dependency_files": deps,
		"test_files":       tests,
		"related_files":    uniqueStrings(append(append([]string{}, deps...), tests...)),
		"execution_scope":  filepath.ToSlash(filepath.Dir(activeFile)),
		"dependency_count": len(deps),
		"test_count":       len(tests),
	}
	return map[string]any{
		"id":           "execution_surface:" + activeFile,
		"type":         "execution_surface",
		"display_name": displayName,
		"status":       "pending",
		"metadata":     metadata,
	}
}

func (p *CodingPhase) formatArtifactInventoryResponse(snapshot map[string]any) string {
	if len(snapshot) == 0 {
		return "Artifact Inventory: none"
	}
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Sprintf("Artifact Inventory: %v", snapshot)
	}
	return fmt.Sprintf("Artifact Inventory:\n%s", string(encoded))
}

func (p *CodingPhase) loadArtifactInventorySnapshot() (string, map[string]any, error) {
	infra := p.ensureSharedInfrastructure()
	if infra == nil || infra.STMAccessor == nil {
		return "", nil, fmt.Errorf("stream context not available")
	}
	stm := infra.STMAccessor()
	if stm == nil {
		return "", nil, fmt.Errorf("stream context not available")
	}
	thread := stm.GetCurrentThread()
	if thread == nil {
		return "", nil, fmt.Errorf("no active workstream")
	}
	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return "", nil, fmt.Errorf("no stream context")
	}
	phaseArtifacts := streamCtx.GetPhaseArtifacts()
	if len(phaseArtifacts) == 0 {
		return "", nil, nil
	}
	if snapshot, ok := extractArtifactInventorySnapshot(phaseArtifacts[p.phaseID]); ok {
		return p.phaseID, snapshot, nil
	}
	for phaseKey, artifact := range phaseArtifacts {
		if phaseKey == p.phaseID {
			continue
		}
		if snapshot, ok := extractArtifactInventorySnapshot(artifact); ok {
			return phaseKey, snapshot, nil
		}
	}
	return "", nil, nil
}

func (p *CodingPhase) persistArtifactInventorySnapshot(snapshot map[string]any) error {
	return p.persistArtifactInventorySnapshotForPhase(p.phaseID, snapshot)
}

func (p *CodingPhase) persistArtifactInventorySnapshotForPhase(phaseKey string, snapshot map[string]any) error {
	if len(snapshot) == 0 {
		return nil
	}
	infra := p.ensureSharedInfrastructure()
	if infra == nil || infra.STMAccessor == nil {
		return fmt.Errorf("stream context not available")
	}
	stm := infra.STMAccessor()
	if stm == nil {
		return fmt.Errorf("stream context not available")
	}
	thread := stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active workstream")
	}
	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return fmt.Errorf("no stream context")
	}
	phaseArtifacts := streamCtx.GetPhaseArtifacts()
	if phaseArtifacts == nil {
		return fmt.Errorf("phase artifacts are not initialized")
	}
	if phaseKey == "" {
		phaseKey = p.phaseID
	}
	artifact, ok := phaseArtifacts[phaseKey]
	if !ok || artifact == nil {
		artifact = map[string]any{}
		phaseArtifacts[phaseKey] = artifact
	}
	artifact["artifact_inventory"] = snapshot
	return nil
}

func extractArtifactInventorySnapshot(artifact map[string]any) (map[string]any, bool) {
	if len(artifact) == 0 {
		return nil, false
	}
	for _, key := range []string{"artifact_inventory", "work_inventory"} {
		if raw, ok := artifact[key]; ok {
			if snapshot, ok := raw.(map[string]any); ok {
				return snapshot, true
			}
			if converted := convertToStringAnyMap(raw); len(converted) > 0 {
				return converted, true
			}
		}
	}
	if _, ok := artifact["items"]; ok {
		return artifact, true
	}
	return nil, false
}

func convertToStringAnyMap(value any) map[string]any {
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var snapshot map[string]any
	if err := json.Unmarshal(bytes, &snapshot); err != nil {
		return nil
	}
	return snapshot
}

func taskInventoryFiles(task cw.Task, pack cw.ContextPack) []string {
	files := []string{}
	for _, candidate := range []string{task.ActiveFile} {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			files = append(files, candidate)
		}
	}
	for _, candidate := range pack.RelatedFiles {
		files = append(files, candidate)
	}
	for _, candidate := range pack.DependencyFiles {
		files = append(files, candidate)
	}
	for _, candidate := range pack.TestFiles {
		files = append(files, candidate)
	}
	for _, candidate := range pack.RepositoryFiles {
		files = append(files, candidate)
	}
	return uniqueStrings(files)
}

func inventoryFileType(filePath string) string {
	path := strings.ToLower(strings.TrimSpace(filePath))
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if isInventoryTestFile(base) {
		return "test"
	}
	if isInventoryConfigFile(base) {
		return "config"
	}
	if isInventorySourceFile(base) {
		return "file"
	}
	return ""
}

func isInventorySourceFile(filePath string) bool {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".java", ".rs", ".cs", ".sh":
		return true
	default:
		return false
	}
}

func isInventoryConfigFile(filePath string) bool {
	base := strings.ToLower(filepath.Base(filePath))
	switch {
	case base == "go.mod", base == "go.sum", base == "package.json", base == "package-lock.json", base == "pnpm-lock.yaml", base == "yarn.lock", base == "Cargo.toml", base == "Cargo.lock", base == "pom.xml", base == "build.gradle", base == "build.gradle.kts", base == "pyproject.toml", base == "requirements.txt":
		return true
	case strings.HasSuffix(base, ".yaml"), strings.HasSuffix(base, ".yml"), strings.HasSuffix(base, ".json"), strings.HasSuffix(base, ".toml"), strings.HasSuffix(base, ".xml"), strings.HasSuffix(base, ".ini"), strings.HasSuffix(base, ".env"), strings.HasSuffix(base, ".properties"):
		return true
	default:
		return false
	}
}

func isInventoryTestFile(filePath string) bool {
	base := strings.ToLower(filepath.Base(filePath))
	return strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".test.js") || strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".spec.js") || strings.HasSuffix(base, ".spec.ts") || strings.HasSuffix(base, ".spec.tsx") || strings.HasSuffix(base, ".test.py")
}

func inventoryDescriptionForFile(filePath, itemType string) string {
	switch itemType {
	case "test":
		return "Test artifact relevant to the current codebase inventory"
	case "config":
		return "Configuration artifact relevant to the current codebase inventory"
	case "interface":
		return "Interface artifact relevant to the current codebase inventory"
	default:
		return "Source artifact relevant to the current codebase inventory"
	}
}

func isInventoryInterfaceFile(filePath string) bool {
	base := strings.ToLower(filepath.Base(filePath))
	if strings.Contains(base, "interface") || strings.Contains(base, "contract") {
		return true
	}
	if strings.Contains(strings.ToLower(filepath.ToSlash(filePath)), "/interfaces/") {
		return true
	}
	return false
}

func inventoryItemID(item map[string]any) string {
	if item == nil {
		return ""
	}
	if id, ok := item["id"].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

func inventoryItemFilePath(item map[string]any) string {
	if item == nil {
		return ""
	}
	if metadata, ok := item["metadata"].(map[string]any); ok {
		if path, ok := metadata["file_path"].(string); ok {
			return strings.TrimSpace(path)
		}
	}
	return ""
}

func inventoryItemDisplayName(item map[string]any) string {
	if item == nil {
		return ""
	}
	if displayName, ok := item["display_name"].(string); ok {
		return strings.TrimSpace(displayName)
	}
	return ""
}

func selectArtifactInventoryItem(snapshot map[string]any, itemID string) bool {
	items := snapshotItems(snapshot)
	if len(items) == 0 {
		return false
	}
	selectedIndex := -1
	for idx, item := range items {
		if matchesInventorySelection(item, itemID) {
			selectedIndex = idx
			break
		}
	}
	if selectedIndex < 0 {
		return false
	}
	selectedID := inventoryItemID(items[selectedIndex])
	setActiveInventoryItem(items, selectedID)
	snapshot["active_item_id"] = selectedID
	snapshot["items"] = items
	return true
}

func completeArtifactInventoryItem(snapshot map[string]any, itemID string) bool {
	items := snapshotItems(snapshot)
	if len(items) == 0 {
		return false
	}
	selectedIndex := -1
	for idx, item := range items {
		if matchesInventorySelection(item, itemID) {
			selectedIndex = idx
			break
		}
	}
	if selectedIndex < 0 {
		return false
	}
	selectedID := inventoryItemID(items[selectedIndex])
	items[selectedIndex]["status"] = "completed"
	items[selectedIndex]["completed_at"] = time.Now().UnixMilli()
	if activeID, _ := snapshot["active_item_id"].(string); activeID == selectedID {
		snapshot["active_item_id"] = ""
	}
	for idx, item := range items {
		if idx == selectedIndex {
			continue
		}
		if status, _ := item["status"].(string); status == "active" {
			item["status"] = "pending"
		}
	}
	advanceArtifactInventoryActiveItem(snapshot, items)
	snapshot["items"] = items
	return true
}

func advanceArtifactInventoryActiveItem(snapshot map[string]any, items []map[string]any) {
	for _, item := range items {
		if status, _ := item["status"].(string); status == "pending" {
			item["status"] = "active"
			item["started_at"] = time.Now().UnixMilli()
			snapshot["active_item_id"] = inventoryItemID(item)
			return
		}
	}
	snapshot["active_item_id"] = ""
}

func snapshotItems(snapshot map[string]any) []map[string]any {
	if len(snapshot) == 0 {
		return nil
	}
	rawItems, ok := snapshot["items"]
	if !ok {
		return nil
	}
	var items []map[string]any
	switch v := rawItems.(type) {
	case []map[string]any:
		items = make([]map[string]any, 0, len(v))
		for _, item := range v {
			if item == nil {
				continue
			}
			items = append(items, item)
		}
	case []any:
		items = make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if item, ok := raw.(map[string]any); ok {
				items = append(items, item)
			}
		}
	default:
		converted := convertToStringAnyMap(v)
		if len(converted) > 0 {
			if nested, ok := converted["items"].([]any); ok {
				items = make([]map[string]any, 0, len(nested))
				for _, raw := range nested {
					if item, ok := raw.(map[string]any); ok {
						items = append(items, item)
					}
				}
			}
		}
	}
	return items
}

func matchesInventorySelection(item map[string]any, selector string) bool {
	selector = strings.TrimSpace(strings.ToLower(selector))
	if selector == "" {
		return false
	}
	if strings.ToLower(inventoryItemID(item)) == selector {
		return true
	}
	if strings.ToLower(inventoryItemDisplayName(item)) == selector {
		return true
	}
	if path := strings.ToLower(inventoryItemFilePath(item)); path != "" && path == selector {
		return true
	}
	return false
}

func setActiveInventoryItem(items []map[string]any, activeID string) {
	for _, item := range items {
		itemID := inventoryItemID(item)
		if itemID == "" {
			continue
		}
		if itemID == activeID {
			if status, _ := item["status"].(string); status != "completed" {
				item["status"] = "active"
				item["started_at"] = time.Now().UnixMilli()
			}
			continue
		}
		if status, _ := item["status"].(string); status == "active" {
			item["status"] = "pending"
		}
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeInventoryPath(path string) string {
	return strings.TrimSpace(filepath.ToSlash(path))
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

// PreprocessResult implements result preprocessing for coding workflow.
// Delegates to codingworkflow test result parser for structured test output.
func (p *CodingPhase) PreprocessResult(toolName string, rawOutput string, filePath string, maxInlineSize int) string {
	return cw.PreprocessToolResult(toolName, rawOutput, filePath, maxInlineSize)
}
