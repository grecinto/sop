package agent

import (
	"strings"
	"testing"

	"github.com/sharedcode/sop"
)

// TestPhasePlanCreation verifies PhasePlan can be created and stored in WorkStream
func TestPhasePlanCreation(t *testing.T) {
	stm := NewShortTermMemory()

	// Create a thread with work stream
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test phase succession",
		Status:     "active",
	}
	thread.InitializeWorkStream("investigation")
	stm.AddThread(thread)

	// Create a PhasePlan
	plan := &PhasePlan{
		PlannedPhases:   []string{"investigation", "planning", "execution", "review", "delivery"},
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: []string{"investigation", "planning", "execution", "review", "delivery"},
		Checkpoints: map[string]bool{
			"after_investigation": true,
			"after_execution":     true,
		},
		Reasoning:  "Standard coding workflow",
		Confidence: "high",
	}

	thread.StreamContext.PhasePlan = plan

	// Verify plan is stored
	if thread.StreamContext.PhasePlan == nil {
		t.Fatal("PhasePlan not stored in StreamContext")
	}

	if len(thread.StreamContext.PhasePlan.PlannedPhases) != 5 {
		t.Errorf("Expected 5 planned phases, got %d", len(thread.StreamContext.PhasePlan.PlannedPhases))
	}

	if thread.StreamContext.PhasePlan.CurrentIndex != 0 {
		t.Errorf("Expected CurrentIndex 0, got %d", thread.StreamContext.PhasePlan.CurrentIndex)
	}

	// Verify checkpoints
	if !thread.StreamContext.PhasePlan.Checkpoints["after_investigation"] {
		t.Error("after_investigation checkpoint not set")
	}
	if !thread.StreamContext.PhasePlan.Checkpoints["after_execution"] {
		t.Error("after_execution checkpoint not set")
	}
}

// TestPhasePlanAdvancement verifies phase cursor advancement
func TestPhasePlanAdvancement(t *testing.T) {
	plan := &PhasePlan{
		PlannedPhases:   []string{"investigation", "planning", "execution"},
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: []string{"investigation", "planning", "execution"},
		Checkpoints:     map[string]bool{},
	}

	// First advancement: investigation -> planning
	plan.CurrentIndex++
	if plan.CurrentIndex != 1 {
		t.Errorf("Expected CurrentIndex 1, got %d", plan.CurrentIndex)
	}
	if plan.PlannedPhases[plan.CurrentIndex] != "planning" {
		t.Errorf("Expected planning phase, got %s", plan.PlannedPhases[plan.CurrentIndex])
	}

	// Second advancement: planning -> execution
	plan.CurrentIndex++
	if plan.CurrentIndex != 2 {
		t.Errorf("Expected CurrentIndex 2, got %d", plan.CurrentIndex)
	}
	if plan.PlannedPhases[plan.CurrentIndex] != "execution" {
		t.Errorf("Expected execution phase, got %s", plan.PlannedPhases[plan.CurrentIndex])
	}

	// Third advancement: execution -> end (no more phases)
	plan.CurrentIndex++
	if plan.CurrentIndex >= len(plan.PlannedPhases) {
		// Expected - no more phases
		t.Log("Successfully detected end of phase plan")
	} else {
		t.Error("Should have reached end of plan")
	}
}

// TestCheckpointDetection verifies checkpoint phase detection
func TestCheckpointDetection(t *testing.T) {
	plan := &PhasePlan{
		PlannedPhases: []string{"investigation", "planning", "execution", "review"},
		CurrentIndex:  0,
		Checkpoints: map[string]bool{
			"after_investigation": true,
			"after_execution":     true,
		},
	}

	// Investigation phase has checkpoint after it
	checkpointKey := "after_investigation"
	if !plan.Checkpoints[checkpointKey] {
		t.Errorf("Expected checkpoint after investigation")
	}

	// Planning phase has NO checkpoint
	checkpointKey = "after_planning"
	if plan.Checkpoints[checkpointKey] {
		t.Errorf("Should not have checkpoint after planning")
	}

	// Execution phase has checkpoint
	checkpointKey = "after_execution"
	if !plan.Checkpoints[checkpointKey] {
		t.Errorf("Expected checkpoint after execution")
	}
}

// TestPriorityRules verifies phase succession priority rules
func TestPriorityRules(t *testing.T) {
	tests := []struct {
		name               string
		status             string
		exitRequested      bool
		explicitTransition string
		plannedNext        string
		expectedTransition string
		expectedUsePlan    bool
	}{
		{
			name:               "Exit request - highest priority",
			status:             "complete",
			exitRequested:      true,
			explicitTransition: "",
			plannedNext:        "planning",
			expectedTransition: "", // Exit takes precedence
			expectedUsePlan:    false,
		},
		{
			name:               "Explicit transition - overrides plan",
			status:             "complete",
			exitRequested:      false,
			explicitTransition: "execution", // LLM chose to skip planning
			plannedNext:        "planning",
			expectedTransition: "execution",
			expectedUsePlan:    false,
		},
		{
			name:               "Automatic succession - uses plan",
			status:             "complete",
			exitRequested:      false,
			explicitTransition: "",
			plannedNext:        "planning",
			expectedTransition: "planning",
			expectedUsePlan:    true,
		},
		{
			name:               "Not complete - no transition",
			status:             "in_progress",
			exitRequested:      false,
			explicitTransition: "",
			plannedNext:        "planning",
			expectedTransition: "",
			expectedUsePlan:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the priority logic
			var finalTransition string
			usedPlan := false

			if strings.ToLower(tt.status) == "complete" {
				if tt.explicitTransition != "" {
					// Priority 2: Explicit transition
					finalTransition = tt.explicitTransition
				} else if !tt.exitRequested {
					// Priority 3: Use plan
					finalTransition = tt.plannedNext
					usedPlan = true
				}
				// Priority 1: Exit request (implicitly handled by not setting transition)
			}

			if finalTransition != tt.expectedTransition {
				t.Errorf("Expected transition %q, got %q", tt.expectedTransition, finalTransition)
			}

			if usedPlan != tt.expectedUsePlan {
				t.Errorf("Expected usePlan=%v, got %v", tt.expectedUsePlan, usedPlan)
			}
		})
	}
}

// TestWorkStreamPersistence verifies PhasePlan survives across pause/resume
func TestWorkStreamPersistence(t *testing.T) {
	stm := NewShortTermMemory()

	// Create thread with phase plan
	thread := &ConversationThread{
		ID:         sop.NewUUID(),
		RootPrompt: "Test persistence",
		Status:     "active",
	}
	thread.InitializeWorkStream("investigation")

	plan := &PhasePlan{
		PlannedPhases:   []string{"investigation", "planning", "execution"},
		CurrentIndex:    1, // Already completed investigation
		CompletedPhases: []string{"investigation"},
		RemainingPhases: []string{"planning", "execution"},
		Checkpoints: map[string]bool{
			"after_planning": true,
		},
		Reasoning:  "Test plan",
		Confidence: "high",
	}
	thread.StreamContext.PhasePlan = plan
	stm.AddThread(thread)

	// Simulate pause
	thread.PauseWorkStreamWithReason("User interjection", false)

	if thread.StreamState != WorkStreamPaused {
		t.Error("Thread should be paused")
	}

	// Verify plan is still intact
	if thread.StreamContext.PhasePlan.CurrentIndex != 1 {
		t.Error("CurrentIndex should be preserved during pause")
	}

	// Resume
	thread.ResumeWorkStream()

	if thread.StreamState != WorkStreamActive {
		t.Error("Thread should be active after resume")
	}

	// Verify plan is still intact after resume
	if thread.StreamContext.PhasePlan == nil {
		t.Fatal("PhasePlan lost after resume")
	}

	if thread.StreamContext.PhasePlan.CurrentIndex != 1 {
		t.Error("CurrentIndex should be preserved after resume")
	}

	if len(thread.StreamContext.PhasePlan.PlannedPhases) != 3 {
		t.Error("PlannedPhases should be preserved after resume")
	}
}
