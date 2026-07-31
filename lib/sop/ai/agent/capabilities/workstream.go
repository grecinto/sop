package capabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// WorkStreamManagement provides tools for LLM to orchestrate WorkStreams.
// Follows LLM-as-Orchestrator pattern: LLM sees state, decides action, calls tools.
// Agent code provides tools (participant), LLM makes decisions (orchestrator).
type WorkStreamManagement struct {
	stmAccessor     func() STMReader        // Access to short-term memory
	mutatorAccessor func() PhasePlanMutator // Access to phase plan mutator
}

// NewWorkStreamManagement creates a new WorkStream management capability.
func NewWorkStreamManagement(stmAccessor func() STMReader, mutatorAccessor func() PhasePlanMutator) *WorkStreamManagement {
	return &WorkStreamManagement{
		stmAccessor:     stmAccessor,
		mutatorAccessor: mutatorAccessor,
	}
}

func (w *WorkStreamManagement) ID() string {
	return "workstream_management"
}

func (w *WorkStreamManagement) Name() string {
	return "WorkStream Management"
}

func (w *WorkStreamManagement) Entitlement() Entitlement {
	return Entitlement{
		EnabledByDefault: true,
		Tier:             "local",
		RequiredGate:     GateReadAccess,
		Permissions: []Permission{{
			Resource: "memory",
			Actions:  []string{"read", "write"},
			Gate:     GateReadAccess,
		}},
	}
}

func (w *WorkStreamManagement) Classify(query string, state RoutingState) ClassificationResult {
	queryLower := strings.ToLower(query)

	// High relevance for explicit WorkStream management
	explicitSignals := []string{
		"pause", "resume", "workstream", "work stream",
		"abandon", "cancel task", "switch to", "back to",
		"list tasks", "show tasks", "active tasks",
	}
	for _, signal := range explicitSignals {
		if strings.Contains(queryLower, signal) {
			return ClassificationResult{
				Relevant: true,
				Score:    90,
				Mode:     CapabilityModePrimary,
				Hints:    []string{"Explicit WorkStream management command"},
			}
		}
	}

	// Low relevance - always available but not primary capability
	return ClassificationResult{
		Relevant: true,
		Score:    10,
		Mode:     CapabilityModeSupporting,
		Hints:    []string{"WorkStream management always available for LLM orchestration"},
	}
}

func (w *WorkStreamManagement) BuildContext(query string, state RoutingState) string {
	stm := w.stmAccessor()
	if stm == nil {
		return ""
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return ""
	}

	streamCtx := thread.GetStreamContext()
	streamState := thread.GetStreamState()

	if streamCtx != nil && streamState != "" {
		plan := streamCtx.GetPhasePlan()
		if plan != nil {
			plannedPhases := plan.GetPlannedPhases()
			currentIndex := plan.GetCurrentIndex()
			return fmt.Sprintf(
				"Active WorkStream: %s (state: %s, phase: %s, progress: %d/%d)",
				streamCtx.GetCurrentPhase(), streamState, streamCtx.GetCurrentPhase(),
				currentIndex+1, len(plannedPhases),
			)
		}
		return fmt.Sprintf("Active WorkStream: %s (state: %s)", streamCtx.GetCurrentPhase(), streamState)
	}

	return "No active WorkStream"
}

func (w *WorkStreamManagement) BuildInstructions(query string, state RoutingState) string {
	return `WorkStream Management: You can pause, resume, list, jump phases, and abandon WorkStreams.

Use these tools to manage multi-phase tasks:
- list_workstreams: See current WorkStream state and progress
- get_workstream_details: Get detailed phase plan information
- jump_to_phase: Skip to a different phase (forward or backward)
- pause_workstream: Pause current task (can be resumed later)
- resume_workstream: Continue a paused task
- abandon_workstream: Permanently cancel a task

When to use:
- User wants to skip ahead → jump_to_phase (e.g., "just implement it now")
- User wants to go back → jump_to_phase (e.g., "review requirements first")
- User starts new task while another is active → Consider pausing current WorkStream
- User explicitly says "pause", "stop", "switch to" → Pause current WorkStream
- User says "resume", "continue", "back to" → Resume paused WorkStream
- User says "cancel", "abandon", "forget about" → Abandon WorkStream

You decide when to pause/resume/jump based on context - no automatic handling.`
}

func (w *WorkStreamManagement) ExposeTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_workstreams",
			Description: "List all active and paused WorkStreams with their current state and progress. Use this to see what tasks are in progress.",
			Schema:      `{"type": "object", "properties": {}}`,
			Tags:        []string{"R"}, // Read-only operation
		},
		{
			Name:        "get_workstream_details",
			Description: "Get detailed information about the current WorkStream including phase plan, current phase, completed phases, and remaining work.",
			Schema:      `{"type": "object", "properties": {}}`,
			Tags:        []string{"R"}, // Read-only operation
		},
		{
			Name:        "jump_to_phase",
			Description: "Jump to a specific phase in the current WorkStream. Use when user wants to skip ahead (e.g., 'just implement it now') or go back (e.g., 'review requirements first'). Updates the phase plan and continues from the target phase.",
			Schema:      `{"type": "object", "properties": {"phase": {"type": "string", "enum": ["intake", "investigation", "planning", "execution", "review", "delivery"], "description": "Target phase to jump to"}}, "required": ["phase"]}`,
			Tags:        []string{"U"}, // Updates WorkStream phase
		},
		{
			Name:        "pause_workstream",
			Description: "Pause the current active WorkStream. Use when the user wants to switch to a different task. The WorkStream can be resumed later. Calling this tool IS the decision to pause.",
			Schema:      `{"type": "object", "properties": {}}`,
			Tags:        []string{"U"}, // Updates WorkStream status
		},
		{
			Name:        "resume_workstream",
			Description: "Resume a paused WorkStream. Use when user wants to continue a previously paused task. Calling this tool IS the decision to resume.",
			Schema:      `{"type": "object", "properties": {}}`,
			Tags:        []string{"U"}, // Updates WorkStream status
		},
		{
			Name:        "abandon_workstream",
			Description: "Permanently abandon the current WorkStream. Use when user wants to cancel a task entirely, not just pause it. This cannot be undone. Calling this tool IS the decision to abandon.",
			Schema:      `{"type": "object", "properties": {}}`,
			Tags:        []string{"D"}, // Deletes/removes WorkStream
		},
	}
}

func (w *WorkStreamManagement) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state RoutingState) (string, error) {
	switch toolName {
	case "list_workstreams":
		return w.listWorkStreams()
	case "get_workstream_details":
		return w.getWorkStreamDetails()
	case "jump_to_phase":
		phase, _ := args["phase"].(string)
		return w.jumpToPhase(phase)
	case "pause_workstream":
		return w.pauseWorkStream()
	case "resume_workstream":
		return w.resumeWorkStream()
	case "abandon_workstream":
		return w.abandonWorkStream()
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (w *WorkStreamManagement) OnBefore(ctx context.Context, query string, state RoutingState) error {
	return nil
}

func (w *WorkStreamManagement) OnAfter(ctx context.Context, query string, state RoutingState, result CapabilityResult) error {
	return nil
}

func (w *WorkStreamManagement) Handoff(ctx context.Context, query string, state RoutingState) *Handoff {
	return nil
}

// Tool implementations

func (w *WorkStreamManagement) listWorkStreams() (string, error) {
	stm := w.stmAccessor()
	if stm == nil {
		return "No WorkStreams available (STM not initialized)", nil
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return "No active conversation thread", nil
	}

	streamCtx := thread.GetStreamContext()
	streamState := thread.GetStreamState()

	if streamCtx == nil {
		return "No WorkStreams found. This is a fresh conversation with no active tasks.", nil
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return fmt.Sprintf("WorkStream exists but has no phase plan (state: %s)", streamState), nil
	}

	plannedPhases := plan.GetPlannedPhases()
	currentIndex := plan.GetCurrentIndex()
	completedPhases := plan.GetCompletedPhases()

	result := strings.Builder{}
	result.WriteString(fmt.Sprintf("Current WorkStream (state: %s):\n", streamState))
	result.WriteString(fmt.Sprintf("- Current Phase: %s (%d/%d)\n", streamCtx.GetCurrentPhase(), currentIndex+1, len(plannedPhases)))
	result.WriteString(fmt.Sprintf("- Completed: %d phases\n", len(completedPhases)))
	result.WriteString(fmt.Sprintf("- Remaining: %d phases\n", len(plannedPhases)-currentIndex-1))
	result.WriteString(fmt.Sprintf("\nPhase Plan:\n"))
	for i, phase := range plannedPhases {
		marker := "  "
		if i < currentIndex {
			marker = "✓ "
		} else if i == currentIndex {
			marker = "→ "
		}
		result.WriteString(fmt.Sprintf("%s%d. %s\n", marker, i+1, phase))
	}

	return result.String(), nil
}

func (w *WorkStreamManagement) getWorkStreamDetails() (string, error) {
	stm := w.stmAccessor()
	if stm == nil {
		return "", fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return "", fmt.Errorf("no active thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return "No active WorkStream", nil
	}

	plan := streamCtx.GetPhasePlan()
	history := streamCtx.GetPhaseHistory()
	artifacts := streamCtx.GetPhaseArtifacts()

	details := map[string]any{
		"current_phase":    streamCtx.GetCurrentPhase(),
		"phase_history":    history,
		"phase_artifacts":  artifacts,
		"iteration_count":  streamCtx.GetIterationCount(),
		"phase_iterations": streamCtx.GetPhaseIterations(),
	}

	if plan != nil {
		details["planned_phases"] = plan.GetPlannedPhases()
		details["current_index"] = plan.GetCurrentIndex()
		details["completed_phases"] = plan.GetCompletedPhases()
		details["remaining_phases"] = plan.GetRemainingPhases()
	}

	detailsJSON, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to serialize details: %w", err)
	}

	return string(detailsJSON), nil
}

func (w *WorkStreamManagement) jumpToPhase(targetPhase string) (string, error) {
	if targetPhase == "" {
		return "", fmt.Errorf("phase parameter is required")
	}

	stm := w.stmAccessor()
	if stm == nil {
		return "", fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return "", fmt.Errorf("no active thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return "No active WorkStream to jump phases in", nil
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return "No phase plan available", nil
	}

	// Get mutator to update the phase plan
	mutator := w.mutatorAccessor()
	if mutator == nil {
		return "", fmt.Errorf("PhasePlanMutator not available")
	}

	// Validate target phase exists in plan
	plannedPhases := plan.GetPlannedPhases()
	currentIndex := plan.GetCurrentIndex()
	currentPhase := ""
	if currentIndex >= 0 && currentIndex < len(plannedPhases) {
		currentPhase = plannedPhases[currentIndex]
	}

	targetIndex := -1
	for i, phase := range plannedPhases {
		if phase == targetPhase {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 {
		return fmt.Sprintf("Phase '%s' not found in current plan. Available phases: %v", targetPhase, plannedPhases), nil
	}

	if targetIndex == currentIndex {
		return fmt.Sprintf("Already at phase '%s'. No jump needed.", targetPhase), nil
	}

	// Jump to target phase using mutator
	err := mutator.JumpToPhase(targetPhase)
	if err != nil {
		return "", fmt.Errorf("failed to jump to phase: %w", err)
	}

	direction := "forward"
	if targetIndex < currentIndex {
		direction = "backward"
	}
	skipped := targetIndex - currentIndex
	if skipped < 0 {
		skipped = -skipped
	}

	return fmt.Sprintf("✓ Jumped %s from phase '%s' to '%s' (skipped %d phases). Continuing from '%s'.",
		direction, currentPhase, targetPhase, skipped-1, targetPhase), nil
}

func (w *WorkStreamManagement) pauseWorkStream() (string, error) {
	stm := w.stmAccessor()
	if stm == nil {
		return "", fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return "", fmt.Errorf("no active thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return "No active WorkStream to pause", nil
	}

	// Set state to paused
	thread.SetStreamState(WorkStreamPaused)

	currentPhase := streamCtx.GetCurrentPhase()
	plan := streamCtx.GetPhasePlan()
	var progress string
	if plan != nil {
		progress = fmt.Sprintf(" (phase %d/%d)", plan.GetCurrentIndex()+1, len(plan.GetPlannedPhases()))
	}
	return fmt.Sprintf("✓ Paused WorkStream at phase '%s'%s. You can resume later with resume_workstream().", currentPhase, progress), nil
}

func (w *WorkStreamManagement) resumeWorkStream() (string, error) {
	stm := w.stmAccessor()
	if stm == nil {
		return "", fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return "", fmt.Errorf("no active thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return "No WorkStream to resume", nil
	}

	streamState := thread.GetStreamState()
	if streamState != WorkStreamPaused {
		return fmt.Sprintf("WorkStream is not paused (current state: %s). Cannot resume.", streamState), nil
	}

	// Set state to active
	thread.SetStreamState(WorkStreamActive)

	currentPhase := streamCtx.GetCurrentPhase()
	plan := streamCtx.GetPhasePlan()
	var remaining int
	if plan != nil {
		remaining = len(plan.GetRemainingPhases())
	}

	return fmt.Sprintf("✓ Resumed WorkStream at phase '%s'. %d phases remaining.", currentPhase, remaining), nil
}

func (w *WorkStreamManagement) abandonWorkStream() (string, error) {
	stm := w.stmAccessor()
	if stm == nil {
		return "", fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return "", fmt.Errorf("no active thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return "No WorkStream to abandon", nil
	}

	// Set state to abandoned
	thread.SetStreamState(WorkStreamAbandoned)

	currentPhase := streamCtx.GetCurrentPhase()
	plan := streamCtx.GetPhasePlan()
	var abandonedWork string
	if plan != nil {
		abandonedWork = fmt.Sprintf(" (%d/%d phases completed)", plan.GetCurrentIndex(), len(plan.GetPlannedPhases()))
	}
	return fmt.Sprintf("✓ Abandoned WorkStream at phase '%s'%s. This WorkStream cannot be resumed.", currentPhase, abandonedWork), nil
}

// STMReader interface for accessing WorkStream state
type STMReader interface {
	GetCurrentThread() ThreadReader
}

type ThreadReader interface {
	GetStreamContext() WorkStreamContextReader
	GetStreamState() WorkStreamState
	SetStreamState(WorkStreamState)
}

type WorkStreamContextReader interface {
	GetCurrentPhase() string
	GetPhaseHistory() []string
	GetPhasePlan() PhasePlanReader
	GetPhaseArtifacts() map[string]any
	GetIterationCount() int
	GetPhaseIterations() map[string]int
}

type PhasePlanReader interface {
	GetPlannedPhases() []string
	GetCurrentIndex() int
	GetCompletedPhases() []string
	GetRemainingPhases() []string
}

// PhasePlanMutator provides methods to modify the phase plan
type PhasePlanMutator interface {
	InsertPhase(position int, phaseID string) error
	AppendPhase(phaseID string) error
	RemovePhase(phaseID string) error
	JumpToPhase(phaseID string) error
	GetCurrentPlan() (phases []string, currentIndex int)
	SetCheckpoint(name string) error
}

// WorkStreamState represents the state of a WorkStream
type WorkStreamState string

const (
	WorkStreamActive    WorkStreamState = "active"
	WorkStreamPaused    WorkStreamState = "paused"
	WorkStreamCompleted WorkStreamState = "completed"
	WorkStreamAbandoned WorkStreamState = "abandoned"
)
