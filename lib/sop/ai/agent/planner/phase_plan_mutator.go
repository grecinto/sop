package planner

import (
	"context"
	"fmt"
	"strings"
)

// PhasePlanMutator provides dynamic phase plan modification capabilities
// This allows LLM to add, skip, or reorder phases mid-conversation based on:
// - User feedback/corrections
// - Discovery of new requirements
// - Validation that certain phases are unnecessary
type PhasePlanMutator interface {
	// InsertPhase adds a new phase at a specific position (0-based index)
	InsertPhase(ctx context.Context, phaseID string, position int) error

	// AppendPhase adds a new phase at the end
	AppendPhase(ctx context.Context, phaseID string) error

	// RemovePhase removes a phase from the plan
	RemovePhase(ctx context.Context, phaseID string) error

	// JumpToPhase immediately transitions to a specific phase (bypassing succession)
	JumpToPhase(ctx context.Context, phaseID string) error

	// GetCurrentPlan returns the current phase plan
	GetCurrentPlan(ctx context.Context) ([]string, int, error)

	// SetCheckpoint marks a phase as a validation checkpoint
	SetCheckpoint(ctx context.Context, phaseID string) error
}

// STMPhasePlanMutator implements PhasePlanMutator using STMReader/WorkStreamContext
type STMPhasePlanMutator struct {
	stmAccessor func() STMReader
}

// NewSTMPhasePlanMutator creates a mutator backed by STM
func NewSTMPhasePlanMutator(stmAccessor func() STMReader) *STMPhasePlanMutator {
	return &STMPhasePlanMutator{stmAccessor: stmAccessor}
}

func (m *STMPhasePlanMutator) InsertPhase(ctx context.Context, phaseID string, position int) error {
	_ = ctx

	stm := m.stmAccessor()
	if stm == nil {
		return fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active conversation thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return fmt.Errorf("no stream context")
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return fmt.Errorf("no phase plan")
	}

	phases := plan.GetPlannedPhases()
	if position < 0 || position > len(phases) {
		return fmt.Errorf("invalid position %d (plan has %d phases)", position, len(phases))
	}

	// Insert phase at position
	newPhases := make([]string, 0, len(phases)+1)
	newPhases = append(newPhases, phases[:position]...)
	newPhases = append(newPhases, phaseID)
	newPhases = append(newPhases, phases[position:]...)

	// Create new plan (this is a bit hacky - need better mutation API)
	return stm.CreatePhasePlan(newPhases, plan.GetCheckpoints(), "Phase inserted dynamically", "high")
}

func (m *STMPhasePlanMutator) AppendPhase(ctx context.Context, phaseID string) error {
	_ = ctx

	stm := m.stmAccessor()
	if stm == nil {
		return fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active conversation thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return fmt.Errorf("no stream context")
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return fmt.Errorf("no phase plan")
	}

	phases := plan.GetPlannedPhases()
	newPhases := append(phases, phaseID)

	return stm.CreatePhasePlan(newPhases, plan.GetCheckpoints(), "Phase appended dynamically", "high")
}

func (m *STMPhasePlanMutator) RemovePhase(ctx context.Context, phaseID string) error {
	_ = ctx

	stm := m.stmAccessor()
	if stm == nil {
		return fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active conversation thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return fmt.Errorf("no stream context")
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return fmt.Errorf("no phase plan")
	}

	phases := plan.GetPlannedPhases()
	newPhases := make([]string, 0, len(phases))
	for _, p := range phases {
		if p != phaseID {
			newPhases = append(newPhases, p)
		}
	}

	if len(newPhases) == len(phases) {
		return fmt.Errorf("phase %s not found in plan", phaseID)
	}

	return stm.CreatePhasePlan(newPhases, plan.GetCheckpoints(), "Phase removed dynamically", "high")
}

func (m *STMPhasePlanMutator) JumpToPhase(ctx context.Context, phaseID string) error {
	_ = ctx

	stm := m.stmAccessor()
	if stm == nil {
		return fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active conversation thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return fmt.Errorf("no stream context")
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return fmt.Errorf("no phase plan")
	}

	phases := plan.GetPlannedPhases()

	// Find target phase index
	targetIndex := -1
	for i, p := range phases {
		if p == phaseID {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 {
		return fmt.Errorf("phase %s not found in plan", phaseID)
	}

	// Jump to phase by setting current index
	plan.SetCurrentIndex(targetIndex)
	return nil
}

func (m *STMPhasePlanMutator) GetCurrentPlan(ctx context.Context) ([]string, int, error) {
	_ = ctx

	stm := m.stmAccessor()
	if stm == nil {
		return nil, 0, fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return nil, 0, fmt.Errorf("no active conversation thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return nil, 0, fmt.Errorf("no stream context")
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return nil, 0, fmt.Errorf("no phase plan")
	}

	return plan.GetPlannedPhases(), plan.GetCurrentIndex(), nil
}

func (m *STMPhasePlanMutator) SetCheckpoint(ctx context.Context, phaseID string) error {
	_ = ctx

	stm := m.stmAccessor()
	if stm == nil {
		return fmt.Errorf("STM not available")
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active conversation thread")
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return fmt.Errorf("no stream context")
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return fmt.Errorf("no phase plan")
	}

	checkpoints := plan.GetCheckpoints()
	if checkpoints == nil {
		checkpoints = make(map[string]bool)
	}
	checkpoints[phaseID] = true

	// Re-create plan with updated checkpoints
	return stm.CreatePhasePlan(plan.GetPlannedPhases(), checkpoints, "Checkpoint added", "high")
}

// PhasePlanMutationTool exposes phase plan mutation as an LLM tool
type PhasePlanMutationTool struct {
	mutator PhasePlanMutator
}

func NewPhasePlanMutationTool(mutator PhasePlanMutator) *PhasePlanMutationTool {
	return &PhasePlanMutationTool{mutator: mutator}
}

// ToolDefinition returns the tool schema for LLM
func (t *PhasePlanMutationTool) ToolDefinition() string {
	return `{
  "name": "mutate_phase_plan",
  "description": "Dynamically modify the phase plan during conversation. Use when user requests changes or when you discover the current plan doesn't match requirements. For refinement interjections or added requirements, merge the new requirements into the current plan/design instead of replacing the existing plan; preserve prior decisions unless the user explicitly changes them and call out conflicts clearly.",
  "parameters": {
    "type": "object",
    "required": ["action"],
    "properties": {
      "action": {
        "type": "string",
        "enum": ["insert", "append", "remove", "jump", "checkpoint", "show"],
        "description": "Action to perform: insert=add at position, append=add at end, remove=delete phase, jump=skip to phase, checkpoint=mark validation point, show=display current plan"
      },
      "phase_id": {
        "type": "string",
        "description": "Phase identifier (intake, investigation, planning, execution, review, delivery)"
      },
      "position": {
        "type": "integer",
        "description": "Position for insert action (0-based index)"
      }
    }
  }
}`
}

// Execute handles the tool invocation
func (t *PhasePlanMutationTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := strings.ToLower(strings.TrimSpace(readStringArg(args, "action")))
	phaseID := strings.TrimSpace(readStringArg(args, "phase_id"))

	switch action {
	case "show":
		phases, currentIdx, err := t.mutator.GetCurrentPlan(ctx)
		if err != nil {
			return "", fmt.Errorf("get current plan: %w", err)
		}

		var sb strings.Builder
		sb.WriteString("📋 Current Phase Plan:\n\n")
		for i, p := range phases {
			marker := "  "
			if i == currentIdx {
				marker = "▶️"
			} else if i < currentIdx {
				marker = "✅"
			}
			sb.WriteString(fmt.Sprintf("%s %d. %s\n", marker, i+1, p))
		}
		return sb.String(), nil

	case "insert":
		if phaseID == "" {
			return "", fmt.Errorf("phase_id required for insert")
		}
		position := readIntArg(args, "position")
		if err := t.mutator.InsertPhase(ctx, phaseID, position); err != nil {
			return "", fmt.Errorf("insert phase: %w", err)
		}
		return fmt.Sprintf("✅ Inserted '%s' at position %d", phaseID, position), nil

	case "append":
		if phaseID == "" {
			return "", fmt.Errorf("phase_id required for append")
		}
		if err := t.mutator.AppendPhase(ctx, phaseID); err != nil {
			return "", fmt.Errorf("append phase: %w", err)
		}
		return fmt.Sprintf("✅ Appended '%s' to plan", phaseID), nil

	case "remove":
		if phaseID == "" {
			return "", fmt.Errorf("phase_id required for remove")
		}
		if err := t.mutator.RemovePhase(ctx, phaseID); err != nil {
			return "", fmt.Errorf("remove phase: %w", err)
		}
		return fmt.Sprintf("✅ Removed '%s' from plan", phaseID), nil

	case "jump":
		if phaseID == "" {
			return "", fmt.Errorf("phase_id required for jump")
		}
		if err := t.mutator.JumpToPhase(ctx, phaseID); err != nil {
			return "", fmt.Errorf("jump to phase: %w", err)
		}
		return fmt.Sprintf("✅ Jumped to '%s' phase", phaseID), nil

	case "checkpoint":
		if phaseID == "" {
			return "", fmt.Errorf("phase_id required for checkpoint")
		}
		if err := t.mutator.SetCheckpoint(ctx, phaseID); err != nil {
			return "", fmt.Errorf("set checkpoint: %w", err)
		}
		return fmt.Sprintf("✅ Set checkpoint at '%s'", phaseID), nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func readStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func readIntArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	if v, ok := args[key]; ok {
		switch typed := v.(type) {
		case int:
			return typed
		case float64:
			return int(typed)
		}
	}
	return 0
}
