package codingworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// PhaseCapability exposes a phase of the coding workflow as a capability that the
// LLM can invoke dynamically during conversation. Each phase capability owns a
// lightweight artifact contract and can persist its state into shared memory so
// later Ask turns continue from the same context. The phase flow stays simple:
// the conversation can transition to a successor phase or exit the current phase
// whenever the user or the agent decides that is appropriate.
type PhaseCapability struct {
	sharedInfra *SharedInfrastructure
	phaseID     string
	name        string
	description string
	captureTool string
	stateTool   string
}

func NewIntakePhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "intake", "Intake & Context", "requirements gathering and context assembly")
}

func NewInvestigationPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "investigation", "Investigation & Analysis", "repository investigation and technical analysis")
}

func NewPlanningPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "planning", "Planning & Design", "planning and design for implementation")
}

func NewExecutionPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "execution", "Execution & Implementation", "implementation and patch application")
}

func NewReviewPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "review", "Review & Validation", "validation, verification, and review")
}

func NewDeliveryPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "delivery", "Delivery & Handoff", "delivery summarization and handoff")
}

func newPhaseCapability(sharedInfra *SharedInfrastructure, phaseID, name, description string) *PhaseCapability {
	return &PhaseCapability{
		sharedInfra: sharedInfra,
		phaseID:     phaseID,
		name:        name,
		description: description,
		captureTool: phaseID + "_capture",
		stateTool:   phaseID + "_state",
	}
}

func (c *PhaseCapability) ID() string { return "phase_" + c.phaseID }

func (c *PhaseCapability) Name() string { return c.name }

func (c *PhaseCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{{
			Resource: "filesystem",
			Actions:  []string{"read", "write", "create", "delete"},
			Gate:     capabilities.GateWriteAccess,
		}, {
			Resource: "memory",
			Actions:  []string{"read", "write"},
			Gate:     capabilities.GateReadAccess,
		}},
	}
}

func (c *PhaseCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil {
		return capabilities.ClassificationResult{}
	}
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if strings.Contains(hint, c.phaseID) || strings.Contains(hint, strings.ReplaceAll(c.name, " ", "_")) {
			return capabilities.ClassificationResult{Relevant: true, Score: 13, Mode: capabilities.CapabilityModePrimary}
		}
	}

	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return capabilities.ClassificationResult{}
	}
	for _, keyword := range phaseKeywords(c.phaseID) {
		if strings.Contains(lower, keyword) {
			return capabilities.ClassificationResult{Relevant: true, Score: 12, Mode: capabilities.CapabilityModeSupporting}
		}
	}
	return capabilities.ClassificationResult{}
}

func phaseKeywords(phaseID string) []string {
	switch phaseID {
	case "intake":
		return []string{"requirement", "requirements", "context", "goal", "scope", "clarify", "ask", "understand"}
	case "investigation":
		return []string{"inspect", "investigate", "analy", "debug", "trace", "error", "repo", "search"}
	case "planning":
		return []string{"plan", "design", "architecture", "approach", "proposal", "strategy"}
	case "execution":
		return []string{"implement", "execute", "patch", "modify", "write", "change", "fix"}
	case "review":
		return []string{"review", "validate", "verify", "check", "test", "quality"}
	case "delivery":
		return []string{"deliver", "handoff", "summarize", "summary", "report", "complete"}
	default:
		return nil
	}
}

func (c *PhaseCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return fmt.Sprintf("Phase capability: %s\n- Use this capability when the conversation is in the %s phase.\n- Capture the current artifact into memory so later Ask turns can continue from the same context.\n- If the user wants to move on, record a successor phase or signal an exit so the workflow can proceed fluidly.", c.name, c.description)
}

func (c *PhaseCapability) ExposeTools() []capabilities.ToolDefinition {
	tools := []capabilities.ToolDefinition{{
		Name:        c.captureTool,
		Description: fmt.Sprintf("Records a phase artifact for %s.", c.name),
		Schema:      "{}",
	}, {
		Name:        c.stateTool,
		Description: fmt.Sprintf("Returns the latest persisted state for %s.", c.name),
		Schema:      "{}",
	}}
	for _, tool := range c.phaseWorkflowTools() {
		tools = append(tools, tool)
	}
	return tools
}

func (c *PhaseCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return fmt.Sprintf("Use the %s capability to gather or record the current phase state, preserve a meaningful artifact in memory, and hand the conversation forward with a clear summary for the next phase when the user chooses to move on.", c.name)
}

func (c *PhaseCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = query
	_ = state
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
	if c == nil {
		return nil
	}
	artifact, err := c.loadExistingArtifact()
	if err != nil || artifact.PhaseID == "" {
		return nil
	}
	if artifact.ExitRequested {
		return &capabilities.Handoff{Direction: "exit", Prompt: fmt.Sprintf("Complete the %s phase and return to the main workflow.", c.name)}
	}
	direction := strings.ToLower(strings.TrimSpace(artifact.TransitionDirection))
	target := strings.TrimSpace(artifact.TransitionTo)
	if direction == "backward" {
		target = strings.TrimSpace(artifact.TransitionFrom)
	}
	if direction == "forward" && target != "" {
		prompt := fmt.Sprintf("Continue the workflow by moving to the %s phase.", target)
		if artifact.Summary != "" {
			prompt = fmt.Sprintf("%s %s", prompt, strings.TrimSpace(artifact.Summary))
		}
		return &capabilities.Handoff{Direction: "forward", Target: target, Prompt: prompt}
	}
	if direction == "backward" && target != "" {
		prompt := fmt.Sprintf("Return to the %s phase and continue the workflow.", target)
		if artifact.Summary != "" {
			prompt = fmt.Sprintf("%s %s", prompt, strings.TrimSpace(artifact.Summary))
		}
		return &capabilities.Handoff{Direction: "backward", Target: target, Prompt: prompt}
	}
	return nil
}

func (c *PhaseCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state
	name := strings.TrimSpace(strings.ToLower(toolName))
	if name == "" {
		return "", fmt.Errorf("empty tool name")
	}
	switch name {
	case strings.ToLower(c.captureTool):
		return c.runCapture(args)
	case strings.ToLower(c.stateTool):
		return c.runState(args)
	case strings.ToLower(c.phaseToolName("build_context")):
		return c.runBuildContext(args)
	case strings.ToLower(c.phaseToolName("inspect_repo")):
		return c.runInspectRepo(args)
	case strings.ToLower(c.phaseToolName("search_repo")):
		return c.runSearchRepo(args)
	case strings.ToLower(c.phaseToolName("list_changed_files")):
		return c.runListChangedFiles(args)
	case strings.ToLower(c.phaseToolName("plan_changes")):
		return c.runPlanChanges(args)
	case strings.ToLower(c.phaseToolName("preview_patch")):
		return c.runPreviewPatch(args)
	case strings.ToLower(c.phaseToolName("apply_patch")):
		return c.runApplyPatch(args)
	case strings.ToLower(c.phaseToolName("run_tests")):
		return c.runTests(args)
	default:
		return "", fmt.Errorf("unsupported tool %q", toolName)
	}
}

func (c *PhaseCapability) phaseWorkflowTools() []capabilities.ToolDefinition {
	switch c.phaseID {
	case "intake":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("build_context"),
			Description: "Builds repository context for the current intake phase request.",
			Schema:      "{}",
		}}
	case "investigation":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("build_context"),
			Description: "Builds repository context for the investigation phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("inspect_repo"),
			Description: "Inspects repository state and changed files for the investigation phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("search_repo"),
			Description: "Searches repository content for evidence relevant to the investigation phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("list_changed_files"),
			Description: "Lists changed files that may affect the current investigation.",
			Schema:      "{}",
		}}
	case "planning":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("build_context"),
			Description: "Builds repository context for the planning phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("inspect_repo"),
			Description: "Inspects repository state for the planning phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("search_repo"),
			Description: "Searches the repository for relevant implementation patterns before planning.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("list_changed_files"),
			Description: "Lists changed files that should be considered in the current plan.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("plan_changes"),
			Description: "Generates a concrete execution plan from the assembled repository context.",
			Schema:      "{}",
		}}
	case "execution":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("preview_patch"),
			Description: "Previews a patch before applying it in the execution phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("apply_patch"),
			Description: "Applies a patch in the execution phase with workflow safety checks.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("run_tests"),
			Description: "Runs tests for the current execution phase task.",
			Schema:      "{}",
		}}
	case "review":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("build_context"),
			Description: "Builds repository context for the review phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("inspect_repo"),
			Description: "Inspects the repository state during the review phase.",
			Schema:      "{}",
		}, {
			Name:        c.phaseToolName("run_tests"),
			Description: "Runs tests during the review phase to validate the current state.",
			Schema:      "{}",
		}}
	default:
		return nil
	}
}

func (c *PhaseCapability) phaseToolName(suffix string) string {
	if c == nil {
		return suffix
	}
	return c.phaseID + "_" + suffix
}

func (c *PhaseCapability) ensureSharedInfrastructure() *SharedInfrastructure {
	if c == nil {
		return NewDefaultSharedInfrastructure()
	}
	if c.sharedInfra == nil {
		c.sharedInfra = NewDefaultSharedInfrastructure()
	}
	return c.sharedInfra.ensureDefaults()
}

func (c *PhaseCapability) buildTask(args map[string]any) Task {
	query := readPhaseString(args, "query", "")
	if query == "" {
		query = readPhaseString(args, "task", "")
	}
	if query == "" {
		query = readPhaseString(args, "description", "")
	}

	workingDir := readPhaseString(args, "working_dir", "")
	activeFile := readPhaseString(args, "active_file", "")
	if workingDir == "" || activeFile == "" {
		inferredDir, inferredFile := inferTaskInputs(query)
		if workingDir == "" {
			workingDir = inferredDir
		}
		if activeFile == "" {
			activeFile = inferredFile
		}
	}

	state := RunState{
		Args: map[string]string{},
		Metadata: map[string]string{
			"phase": c.phaseID,
		},
	}
	return Task{
		WorkingDir:  workingDir,
		ActiveFile:  activeFile,
		Description: query,
		TaskType:    c.phaseID,
		State:       state,
	}
}

func (c *PhaseCapability) sharedInfrastructure() *SharedInfrastructure {
	return c.ensureSharedInfrastructure()
}

func (c *PhaseCapability) runBuildContext(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
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

func (c *PhaseCapability) runInspectRepo(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
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

func (c *PhaseCapability) runSearchRepo(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
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

func (c *PhaseCapability) runListChangedFiles(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
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

func (c *PhaseCapability) runPlanChanges(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
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
	return fmt.Sprintf("Execution Plan:\n%s", strings.Join(plan, "\n")), nil
}

func (c *PhaseCapability) runPreviewPatch(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	patch := c.readPatch(args)
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

func (c *PhaseCapability) runApplyPatch(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	patch := c.readPatch(args)
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

func (c *PhaseCapability) runTests(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
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

func (c *PhaseCapability) readPatch(args map[string]any) *Patch {
	if args == nil {
		return nil
	}
	if raw, ok := args["patch"]; ok {
		switch typed := raw.(type) {
		case *Patch:
			return typed
		case Patch:
			return &typed
		case map[string]any:
			return c.readPatch(typed)
		}
	}
	filePath := readPhaseString(args, "file_path", "")
	oldContent := readPhaseString(args, "old_content", "")
	newContent := readPhaseString(args, "new_content", "")
	description := readPhaseString(args, "description", "")
	if filePath == "" && oldContent == "" && newContent == "" && description == "" {
		return nil
	}
	return &Patch{FilePath: filePath, OldContent: oldContent, NewContent: newContent, Description: description}
}

func (c *PhaseCapability) runCapture(args map[string]any) (string, error) {
	previous, err := c.loadExistingArtifact()
	if err != nil {
		return "", err
	}

	artifact := phaseArtifact{
		PhaseID:             c.phaseID,
		Summary:             readPhaseString(args, "summary", previous.Summary),
		Status:              readPhaseString(args, "status", previous.Status),
		Requirements:        mergePhaseStringSlice(readPhaseStringSlice(args, "requirements"), previous.Requirements),
		Plan:                mergePhaseStringSlice(readPhaseStringSlice(args, "plan"), previous.Plan),
		Notes:               mergePhaseStringSlice(readPhaseStringSlice(args, "notes"), previous.Notes),
		Evidence:            mergePhaseStringSlice(readPhaseStringSlice(args, "evidence"), previous.Evidence),
		NextSteps:           mergePhaseStringSlice(readPhaseStringSlice(args, "next_steps"), previous.NextSteps),
		TransitionTo:        readPhaseString(args, "transition_to", previous.TransitionTo),
		TransitionFrom:      readPhaseString(args, "transition_from", previous.TransitionFrom),
		TransitionDirection: readPhaseString(args, "transition_direction", previous.TransitionDirection),
		ExitRequested:       readPhaseBool(args, "exit") || previous.ExitRequested,
		Iteration:           previous.Iteration + 1,
		UpdatedAt:           time.Now().UTC(),
	}
	if artifact.Iteration <= 0 {
		artifact.Iteration = 1
	}
	if artifact.Status == "" {
		artifact.Status = "in_progress"
	}
	if artifact.Summary == "" {
		artifact.Summary = readPhaseString(args, "description", "")
	}
	if artifact.Summary == "" {
		artifact.Summary = fmt.Sprintf("%s phase artifact", c.name)
	}
	if artifact.TransitionTo == "" && previous.TransitionTo != "" {
		artifact.TransitionTo = previous.TransitionTo
	}
	if artifact.TransitionFrom == "" && previous.TransitionFrom != "" {
		artifact.TransitionFrom = previous.TransitionFrom
	}
	if artifact.TransitionDirection == "" && previous.TransitionDirection != "" {
		artifact.TransitionDirection = previous.TransitionDirection
	}
	if err := c.persistArtifact(artifact); err != nil {
		return "", err
	}
	payload, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal artifact: %w", err)
	}
	return fmt.Sprintf("Recorded %s phase artifact:\n%s", c.name, string(payload)), nil
}

func (c *PhaseCapability) runState(args map[string]any) (string, error) {
	storage := c.ensureSharedInfrastructure()
	if storage.Memory == nil {
		storage.Memory = NewInMemorySessionMemory()
	}
	values, err := storage.Memory.Load("phase-artifact:" + c.phaseID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "memory not found") {
			return fmt.Sprintf("No persisted state found for %s yet.", c.name), nil
		}
		return "", fmt.Errorf("load phase state: %w", err)
	}
	artifactJSON := values["artifact_json"]
	if artifactJSON == "" {
		return fmt.Sprintf("No persisted state found for %s yet.", c.name), nil
	}
	return fmt.Sprintf("Current %s phase state:\n%s", c.name, artifactJSON), nil
}

func (c *PhaseCapability) loadExistingArtifact() (phaseArtifact, error) {
	storage := c.ensureSharedInfrastructure()
	if storage.Memory == nil {
		storage.Memory = NewInMemorySessionMemory()
	}
	values, err := storage.Memory.Load("phase-artifact:" + c.phaseID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "memory not found") {
			return phaseArtifact{}, nil
		}
		return phaseArtifact{}, fmt.Errorf("load phase artifact: %w", err)
	}
	artifactJSON := values["artifact_json"]
	if artifactJSON == "" {
		return phaseArtifact{}, nil
	}
	var artifact phaseArtifact
	if err := json.Unmarshal([]byte(artifactJSON), &artifact); err != nil {
		return phaseArtifact{}, fmt.Errorf("decode phase artifact: %w", err)
	}
	return artifact, nil
}

func (c *PhaseCapability) persistArtifact(artifact phaseArtifact) error {
	storage := c.ensureSharedInfrastructure()
	if storage.Memory == nil {
		storage.Memory = NewInMemorySessionMemory()
	}
	payload, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("marshal phase artifact: %w", err)
	}
	values := map[string]string{
		"artifact_json": string(payload),
		"phase":         artifact.PhaseID,
		"summary":       artifact.Summary,
		"status":        artifact.Status,
		"updated_at":    artifact.UpdatedAt.Format(time.RFC3339),
	}
	if err := storage.Memory.Save("phase-artifact:"+artifact.PhaseID, values); err != nil {
		return fmt.Errorf("persist phase artifact: %w", err)
	}
	if storage != nil {
		_ = storage
	}
	return nil
}

func readPhaseString(args map[string]any, key, fallback string) string {
	if args == nil {
		return fallback
	}
	if value, ok := args[key]; ok {
		if str, ok := value.(string); ok {
			return strings.TrimSpace(str)
		}
	}
	return fallback
}

func readPhaseStringSlice(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	if value, ok := args[key]; ok {
		switch typed := value.(type) {
		case []string:
			return sanitizePhaseStrings(typed)
		case []any:
			items := make([]string, 0, len(typed))
			for _, item := range typed {
				if str, ok := item.(string); ok {
					items = append(items, strings.TrimSpace(str))
				}
			}
			return sanitizePhaseStrings(items)
		case string:
			parts := strings.Split(typed, ",")
			items := make([]string, 0, len(parts))
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					items = append(items, trimmed)
				}
			}
			return sanitizePhaseStrings(items)
		}
	}
	return nil
}

func sanitizePhaseStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered
}

func mergePhaseStringSlice(current, previous []string) []string {
	if len(current) > 0 {
		return current
	}
	if len(previous) > 0 {
		return previous
	}
	return nil
}

func readPhaseBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	if value, ok := args[key]; ok {
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(typed, "true") || strings.EqualFold(typed, "yes") || strings.EqualFold(typed, "1")
		case int:
			return typed != 0
		case int64:
			return typed != 0
		case float64:
			return typed != 0
		}
	}
	return false
}

type phaseArtifact struct {
	PhaseID             string    `json:"phase_id"`
	Summary             string    `json:"summary"`
	Status              string    `json:"status"`
	Requirements        []string  `json:"requirements,omitempty"`
	Plan                []string  `json:"plan,omitempty"`
	Notes               []string  `json:"notes,omitempty"`
	Evidence            []string  `json:"evidence,omitempty"`
	NextSteps           []string  `json:"next_steps,omitempty"`
	TransitionTo        string    `json:"transition_to,omitempty"`
	TransitionFrom      string    `json:"transition_from,omitempty"`
	TransitionDirection string    `json:"transition_direction,omitempty"`
	ExitRequested       bool      `json:"exit_requested,omitempty"`
	Iteration           int       `json:"iteration"`
	UpdatedAt           time.Time `json:"updated_at"`
}
