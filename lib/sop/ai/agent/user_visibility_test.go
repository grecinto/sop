package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// TestPlanVisibilityFormatting verifies plan creation displays roadmap
func TestPlanVisibilityFormatting(t *testing.T) {
	stm := NewShortTermMemory()

	// Create thread with work stream
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test visibility",
		Status:     "active",
	}
	thread.InitializeWorkStream("planning")
	stm.AddThread(thread)

	// Create shared infrastructure with STM accessor
	sharedInfra := &codingworkflow.SharedInfrastructure{}
	sharedInfra.STMAccessor = func() codingworkflow.STMReader {
		return &stmTestAdapter{stm: stm, thread: thread}
	}

	// Create planning phase (succession path tool is only in planning phase)
	planningPhase := codingworkflow.NewPlanningPhaseCapability(sharedInfra)

	// Create succession path
	args := map[string]any{
		"phases": []string{"investigation", "planning", "execution"},
		"checkpoints": []string{
			"after_investigation",
		},
		"reasoning":  "Standard workflow for bug fix",
		"confidence": "high",
	}

	routingState := capabilities.RoutingState{Domain: "codingworkflow"}
	result, err := planningPhase.ExecuteTool(nil, "planning_create_succession_path", args, routingState)
	if err != nil {
		t.Fatalf("Failed to create succession path: %v", err)
	}

	// Debug: Print the actual result
	t.Logf("Result:\n%s", result)

	// Verify roadmap formatting in result
	if !strings.Contains(result, "📋 EXECUTION ROADMAP:") {
		t.Error("Expected roadmap section in plan output")
	}
	if !strings.Contains(result, "Investigation & Analysis") {
		t.Error("Expected formatted phase name in roadmap")
	}
	if !strings.Contains(result, "🔍 (checkpoint)") {
		t.Error("Expected checkpoint marker in roadmap")
	}
	if !strings.Contains(result, "1. Investigation & Analysis") {
		t.Error("Expected numbered phases in roadmap")
	}
	if !strings.Contains(result, "↓") {
		t.Error("Expected arrows between phases in roadmap")
	}
	if !strings.Contains(result, "🚀 Next: Begin execution with the first phase") {
		t.Error("Expected next step guidance")
	}
}

// TestProgressVisibility verifies progress display when completing phases
func TestProgressVisibility(t *testing.T) {
	stm := NewShortTermMemory()

	// Create thread with work stream
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test progress",
		Status:     "active",
	}
	thread.InitializeWorkStream("investigation")
	stm.AddThread(thread)

	// Create and store plan
	phases := []string{"investigation", "planning", "execution"}
	checkpoints := map[string]bool{"after_investigation": true}

	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   phases,
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: phases,
		Checkpoints:     checkpoints,
		Reasoning:       "Test workflow",
		Confidence:      "high",
	}

	// Create shared infrastructure
	sharedInfra := &codingworkflow.SharedInfrastructure{}
	sharedInfra.STMAccessor = func() codingworkflow.STMReader {
		return &stmTestAdapter{stm: stm, thread: thread}
	}

	// Create investigation phase
	investigationPhase := codingworkflow.NewInvestigationPhaseCapability(sharedInfra)

	// Complete investigation phase
	args := map[string]any{
		"status":               "complete",
		"summary":              "Investigation completed successfully",
		"transition_to":        "planning",
		"transition_from":      "investigation",
		"transition_direction": "forward",
	}

	routingState := capabilities.RoutingState{Domain: "codingworkflow"}
	result, err := investigationPhase.ExecuteTool(nil, "investigation_capture", args, routingState)
	if err != nil {
		t.Fatalf("Failed to capture investigation: %v", err)
	}

	// Debug: Print the actual result
	t.Logf("Result:\n%s", result)

	// Verify progress display
	if !strings.Contains(result, "📊 PROGRESS:") {
		t.Error("Expected progress indicator in completion message")
	}
	if !strings.Contains(result, "0/3 phases complete") {
		t.Error("Expected active-plan progress count to remain at the current cursor position")
	}
	if !strings.Contains(result, "(0%)") {
		t.Error("Expected percentage in progress for the current cursor position")
	}
	if !strings.Contains(result, "✅") {
		t.Error("Expected checkmark for completed phase")
	}
	if !strings.Contains(result, "⏭️") {
		t.Error("Expected next phase indicator")
	}
	if !strings.Contains(result, "→ Next phase: Planning & Design") {
		t.Error("Expected next phase announcement for the planned forward transition")
	}
}

// TestCheckpointVisibility verifies checkpoint notifications
func TestCheckpointVisibility(t *testing.T) {
	stm := NewShortTermMemory()

	// Create thread with work stream
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test checkpoint",
		Status:     "active",
	}
	thread.InitializeWorkStream("investigation")
	stm.AddThread(thread)

	// Create and store plan with checkpoint
	phases := []string{"investigation", "planning", "execution"}
	checkpoints := map[string]bool{"after_investigation": true}

	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   phases,
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: phases,
		Checkpoints:     checkpoints,
		Reasoning:       "Test workflow",
		Confidence:      "high",
	}

	// Create shared infrastructure
	sharedInfra := &codingworkflow.SharedInfrastructure{}
	sharedInfra.STMAccessor = func() codingworkflow.STMReader {
		return &stmTestAdapter{stm: stm, thread: thread}
	}

	// Create investigation phase
	investigationPhase := codingworkflow.NewInvestigationPhaseCapability(sharedInfra)

	// Complete investigation phase (checkpoint)
	args := map[string]any{
		"status":               "complete",
		"summary":              "Investigation completed",
		"transition_to":        "planning",
		"transition_from":      "investigation",
		"transition_direction": "forward",
	}

	routingState := capabilities.RoutingState{Domain: "codingworkflow"}
	result, err := investigationPhase.ExecuteTool(nil, "investigation_capture", args, routingState)
	if err != nil {
		t.Fatalf("Failed to capture investigation: %v", err)
	}

	// Verify checkpoint notification
	if !strings.Contains(result, "🔍 (CHECKPOINT PHASE)") {
		t.Error("Expected checkpoint phase indicator")
	}
	if !strings.Contains(result, "💡 TIP:") {
		t.Error("Expected tip about overriding at checkpoints")
	}
	if !strings.Contains(result, "override the planned path") {
		t.Error("Expected guidance about plan override capability")
	}
}

// TestFailureVisibility verifies blocked/failed phase displays
func TestFailureVisibility(t *testing.T) {
	stm := NewShortTermMemory()

	// Create thread with work stream
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test failure",
		Status:     "active",
	}
	thread.InitializeWorkStream("investigation")
	stm.AddThread(thread)

	// Create and store plan
	phases := []string{"investigation", "planning", "execution"}
	thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   phases,
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: phases,
		Checkpoints:     map[string]bool{},
		Reasoning:       "Test workflow",
		Confidence:      "high",
	}

	// Create shared infrastructure
	sharedInfra := &codingworkflow.SharedInfrastructure{}
	sharedInfra.STMAccessor = func() codingworkflow.STMReader {
		return &stmTestAdapter{stm: stm, thread: thread}
	}

	// Create investigation phase
	investigationPhase := codingworkflow.NewInvestigationPhaseCapability(sharedInfra)

	tests := []struct {
		name           string
		status         string
		expectedMarker string
	}{
		{
			name:           "blocked phase",
			status:         "blocked",
			expectedMarker: "⚠️",
		},
		{
			name:           "failed phase",
			status:         "failed",
			expectedMarker: "⚠️",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"status":  tt.status,
				"summary": "Encountered an issue",
			}

			routingState := capabilities.RoutingState{Domain: "codingworkflow"}
			result, err := investigationPhase.ExecuteTool(nil, "investigation_capture", args, routingState)
			if err != nil {
				t.Fatalf("Failed to capture investigation: %v", err)
			}

			// Verify failure visibility
			if !strings.Contains(result, tt.expectedMarker) {
				t.Errorf("Expected warning marker %s for %s status", tt.expectedMarker, tt.status)
			}
			if !strings.Contains(result, strings.ToUpper(tt.status)) {
				t.Errorf("Expected status %s to be highlighted", tt.status)
			}
			if !strings.Contains(result, "🔧 OPTIONS:") {
				t.Error("Expected options section for blocked/failed phase")
			}
			if !strings.Contains(result, "Retry this phase") {
				t.Error("Expected retry option")
			}
			if !strings.Contains(result, "Skip to a different phase") {
				t.Error("Expected skip option")
			}
			if !strings.Contains(result, "Request exit") {
				t.Error("Expected exit option")
			}
		})
	}
}

// TestPhaseNameFormatting verifies human-readable phase names
func TestPhaseNameFormatting(t *testing.T) {
	stm := NewShortTermMemory()

	// Create thread with work stream
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test names",
		Status:     "active",
	}
	thread.InitializeWorkStream("planning")
	stm.AddThread(thread)

	// Create shared infrastructure
	sharedInfra := &codingworkflow.SharedInfrastructure{}
	sharedInfra.STMAccessor = func() codingworkflow.STMReader {
		return &stmTestAdapter{stm: stm, thread: thread}
	}

	// Create planning phase (succession path tool is in planning phase)
	planningPhase := codingworkflow.NewPlanningPhaseCapability(sharedInfra)

	// Create plan with all standard phases
	args := map[string]any{
		"phases": []string{
			"intake",
			"investigation",
			"planning",
			"execution",
			"review",
			"delivery",
		},
		"checkpoints": []string{},
		"reasoning":   "Full workflow test",
		"confidence":  "high",
	}

	routingState := capabilities.RoutingState{Domain: "codingworkflow"}
	result, err := planningPhase.ExecuteTool(nil, "planning_create_succession_path", args, routingState)
	if err != nil {
		t.Fatalf("Failed to create succession path: %v", err)
	}

	// Verify all phases have human-readable names
	expectedNames := []string{
		"Intake & Context",
		"Investigation & Analysis",
		"Planning & Design",
		"Execution & Implementation",
		"Review & Validation",
		"Delivery & Handoff",
	}

	for _, name := range expectedNames {
		if !strings.Contains(result, name) {
			t.Errorf("Expected formatted phase name: %s", name)
		}
	}
}

// TestArtifactJSONFormat verifies artifact is still valid JSON despite added formatting
func TestArtifactJSONFormat(t *testing.T) {
	stm := NewShortTermMemory()

	// Create thread with work stream
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test JSON",
		Status:     "active",
	}
	thread.InitializeWorkStream("investigation")
	stm.AddThread(thread)

	// Create shared infrastructure
	sharedInfra := &codingworkflow.SharedInfrastructure{}
	sharedInfra.STMAccessor = func() codingworkflow.STMReader {
		return &stmTestAdapter{stm: stm, thread: thread}
	}

	// Create investigation phase
	investigationPhase := codingworkflow.NewInvestigationPhaseCapability(sharedInfra)

	// Capture artifact
	args := map[string]any{
		"status":  "complete",
		"summary": "Test summary",
	}

	routingState := capabilities.RoutingState{Domain: "codingworkflow"}
	result, err := investigationPhase.ExecuteTool(nil, "investigation_capture", args, routingState)
	if err != nil {
		t.Fatalf("Failed to capture: %v", err)
	}

	// Extract JSON from result (between "Recorded" and first "\n\n")
	lines := strings.Split(result, "\n")
	var jsonLines []string
	inJSON := false
	for _, line := range lines {
		if strings.HasPrefix(line, "{") {
			inJSON = true
		}
		if inJSON {
			jsonLines = append(jsonLines, line)
			if strings.HasPrefix(line, "}") {
				break
			}
		}
	}

	jsonStr := strings.Join(jsonLines, "\n")

	// Verify it's valid JSON
	var artifact map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &artifact); err != nil {
		t.Errorf("Artifact is not valid JSON: %v\nJSON: %s", err, jsonStr)
	}
}

// stmTestAdapter implements STMReader for testing
type stmTestAdapter struct {
	stm    *ShortTermMemory
	thread *ConversationThread
}

func (a *stmTestAdapter) GetCurrentThread() codingworkflow.WorkStreamReader {
	if a.thread == nil {
		return nil
	}
	return &threadTestAdapter{thread: a.thread}
}

func (a *stmTestAdapter) CreatePhasePlan(phases []string, checkpoints map[string]bool, reasoning, confidence string) error {
	if a.thread == nil || a.thread.StreamContext == nil {
		return nil
	}
	a.thread.StreamContext.PhasePlan = &PhasePlan{
		PlannedPhases:   phases,
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: phases,
		Checkpoints:     checkpoints,
		Reasoning:       reasoning,
		Confidence:      confidence,
	}
	return nil
}

func (a *stmTestAdapter) StoreMultiStepPlan(planData any) error {
	if a.thread == nil || a.thread.StreamContext == nil {
		return nil
	}
	// Store the plan data - in tests we can just ignore it or store it in a simple way
	// The real implementation would marshal and store it properly
	return nil
}

// threadTestAdapter implements WorkStreamReader for testing
type threadTestAdapter struct {
	thread *ConversationThread
}

func (a *threadTestAdapter) GetStreamContext() codingworkflow.WorkStreamContextReader {
	if a.thread == nil || a.thread.StreamContext == nil {
		return nil
	}
	return &contextTestAdapter{ctx: a.thread.StreamContext}
}

// contextTestAdapter implements WorkStreamContextReader for testing
type contextTestAdapter struct {
	ctx *WorkStreamContext
}

func (a *contextTestAdapter) GetPhasePlan() codingworkflow.PhasePlanReader {
	if a.ctx == nil || a.ctx.PhasePlan == nil {
		return nil
	}
	return &planTestAdapter{plan: a.ctx.PhasePlan}
}

func (a *contextTestAdapter) SetPhasePlan(plan codingworkflow.PhasePlanReader) {
	if a.ctx == nil {
		return
	}
	// For testing, we just update the existing plan reference
	// In production, this would properly copy the plan data
}

func (a *contextTestAdapter) GetMultiStepPlan() codingworkflow.MultiStepPlanReader {
	// Test stub - return nil
	return nil
}

func (a *contextTestAdapter) SetMultiStepPlan(plan codingworkflow.MultiStepPlanReader) {
	// Test stub - no-op
}

func (a *contextTestAdapter) GetPhaseArtifacts() map[string]map[string]any {
	if a.ctx == nil {
		return nil
	}
	return a.ctx.PhaseArtifacts
}

// planTestAdapter implements PhasePlanReader for testing
type planTestAdapter struct {
	plan *PhasePlan
}

func (a *planTestAdapter) GetPlannedPhases() []string {
	if a.plan == nil {
		return nil
	}
	return a.plan.PlannedPhases
}

func (a *planTestAdapter) GetCurrentIndex() int {
	if a.plan == nil {
		return -1
	}
	return a.plan.CurrentIndex
}

func (a *planTestAdapter) GetCheckpoints() map[string]bool {
	if a.plan == nil {
		return nil
	}
	return a.plan.Checkpoints
}

func (a *planTestAdapter) SetCurrentIndex(idx int) {
	if a.plan == nil {
		return
	}
	a.plan.CurrentIndex = idx
}

func (a *planTestAdapter) AdvancePhase() (nextPhase string, hasMore bool) {
	if a.plan == nil {
		return "", false
	}
	a.plan.CurrentIndex++
	if a.plan.CurrentIndex >= len(a.plan.PlannedPhases) {
		return "", false
	}
	return a.plan.PlannedPhases[a.plan.CurrentIndex], true
}
