package agent

import (
	"context"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/inmemory"
)

// TestStartupRehydrationFlow validates the complete startup rehydration flow:
// 1. Create workstream with phase progression
// 2. Persist to B-tree
// 3. Create new memory instance (simulates daemon restart)
// 4. Rehydrate from B-tree
// 5. Verify paused workstreams are accessible
// 6. User can choose to resume or start new work
func TestStartupRehydrationFlow(t *testing.T) {
	ctx := context.Background()

	// Create B-tree for persistence
	btreeStore := inmemory.NewBtree[string, any](false).Btree

	// === Simulate First Session ===
	t.Log("Creating first session with paused workstream...")
	memory1 := NewShortTermMemory()

	// Create a conversation thread with phase progression
	threadID := sop.NewUUID()
	thread := &ConversationThread{
		ID:          threadID,
		RootPrompt:  "Create a test script",
		Label:       "Test Script Creation",
		StreamState: WorkStreamPaused,
		StreamContext: &WorkStreamContext{
			CurrentPhase: "execution",
			StructuredPlan: &cw.StructuredPlan{
				Objective:     "Create hello world test script",
				WorkflowShape: "create",
				Domain:        "coding_workflow",
				Steps: []cw.StructuredPlanStep{
					{StepID: "create_1", Goal: "Write test.sh", Status: "in_progress"},
				},
				CurrentStep:    0,
				CompletedSteps: []string{},
			},
			PhaseHistory:   []string{"investigation", "planning", "execution"},
			IterationCount: 3,
			PhaseIterations: map[string]int{
				"investigation": 1,
				"planning":      1,
				"execution":     1,
			},
			PhaseArtifacts: map[string]map[string]any{
				"planning": {
					"approach": "Create bash script with echo",
					"files":    []string{"test.sh"},
				},
				"execution": {
					"status": "in_progress",
				},
			},
			RequiresManualResume: true,
			PauseReason:          "user requested to work on something else",
			StartedAt:            time.Now().Unix(),
			PausedAt:             time.Now().Unix(),
		},
		Exchanges: []Interaction{
			{Role: RoleUser, Content: "Create a test script that prints hello world", Timestamp: time.Now().Unix()},
			{Role: RoleAssistant, Content: "I'll create a test script for you", Timestamp: time.Now().Unix()},
		},
	}

	memory1.Threads[threadID] = thread
	memory1.Order = []sop.UUID{threadID}
	memory1.CurrentThreadID = threadID

	// Persist to B-tree
	t.Log("Persisting workstream state...")
	if err := memory1.Persist(ctx, btreeStore); err != nil {
		t.Fatalf("Failed to persist: %v", err)
	}

	// Verify B-tree has the data
	found, _ := btreeStore.Find(ctx, "stm_session_state", false)
	if !found {
		t.Fatal("Persistence did not write to B-tree")
	}
	t.Log("✓ Persisted state found in B-tree")

	// === Simulate Daemon Restart ===
	t.Log("\nSimulating daemon restart...")
	time.Sleep(100 * time.Millisecond) // Simulate time passing

	// Create new memory instance (simulates fresh daemon start)
	memory2 := NewShortTermMemory()

	// Rehydrate from B-tree (this is what copilot_executor.Initialize() does)
	t.Log("Rehydrating session memory from persisted state...")
	if err := memory2.Rehydrate(ctx, btreeStore); err != nil {
		t.Fatalf("Failed to rehydrate: %v", err)
	}

	// === Verify Rehydration ===
	t.Log("\nVerifying rehydrated state...")

	// Check that thread was restored
	if len(memory2.Threads) != 1 {
		t.Fatalf("Expected 1 thread, got %d", len(memory2.Threads))
	}
	t.Log("✓ Thread count restored")

	// Get the rehydrated thread
	rehydratedThread, exists := memory2.Threads[threadID]
	if !exists {
		t.Fatal("Thread not found after rehydration")
	}

	// Verify thread details
	if rehydratedThread.RootPrompt != "Create a test script" {
		t.Errorf("RootPrompt mismatch: expected 'Create a test script', got '%s'", rehydratedThread.RootPrompt)
	}
	t.Log("✓ Workstream query preserved")

	// Verify phase progression was restored
	if rehydratedThread.StreamContext == nil {
		t.Fatal("StreamContext not restored")
	}
	if len(rehydratedThread.StreamContext.PhaseHistory) != 3 {
		t.Errorf("Expected 3 phases in history, got %d", len(rehydratedThread.StreamContext.PhaseHistory))
	}
	t.Logf("✓ Phase progression restored (%d phases)", len(rehydratedThread.StreamContext.PhaseHistory))

	// Verify pause state
	if rehydratedThread.StreamState != WorkStreamPaused {
		t.Error("Workstream should be paused")
	}
	if !rehydratedThread.StreamContext.RequiresManualResume {
		t.Error("RequiresManualResume should be true")
	}
	if rehydratedThread.StreamContext.PauseReason != "user requested to work on something else" {
		t.Errorf("Pause reason mismatch: got '%s'", rehydratedThread.StreamContext.PauseReason)
	}
	t.Log("✓ Pause state preserved")

	// Verify phase artifacts were restored
	if rehydratedThread.StreamContext.PhaseArtifacts == nil {
		t.Fatal("PhaseArtifacts not restored")
	}
	planningArtifact, exists := rehydratedThread.StreamContext.PhaseArtifacts["planning"]
	if !exists {
		t.Error("Planning artifacts not restored")
	} else if planningArtifact["approach"] != "Create bash script with echo" {
		t.Errorf("Artifact data not preserved correctly")
	}
	t.Log("✓ Phase artifacts restored")

	// === Test User Choice: Reminder Display ===
	t.Log("\nTesting user choice mechanism...")
	reminder := memory2.FormatPausedWorkStreamsReminder()
	if reminder == "" {
		t.Fatal("Expected non-empty reminder")
	}
	t.Logf("Reminder text:\n%s", reminder)
	t.Log("✓ Reminder formatted for user display")

	// Verify helper methods work
	if !rehydratedThread.IsWaitingForManualResume() {
		t.Error("IsWaitingForManualResume should return true")
	}
	if rehydratedThread.CanAutoResume() {
		t.Error("CanAutoResume should return false for manually paused work")
	}
	t.Log("✓ Helper methods functional")

	t.Log("\n=== ALL TESTS PASSED ===")
	t.Log("✅ Startup rehydration works correctly")
	t.Log("✅ User choice mechanism functional")
	t.Log("✅ Phase state preserved across restarts")
	t.Log("✅ User can see reminder and choose to resume or start new work")
}

// TestRehydrationWithEmptyBtree verifies graceful handling when no persisted state exists
func TestRehydrationWithEmptyBtree(t *testing.T) {
	ctx := context.Background()

	// Create empty B-tree
	btreeStore := inmemory.NewBtree[string, any](false).Btree

	// Create new memory and try to rehydrate
	mem := NewShortTermMemory()
	err := mem.Rehydrate(ctx, btreeStore)

	// Should not error on empty state
	if err != nil {
		t.Errorf("Rehydration should not error on empty B-tree: %v", err)
	}

	// Should have no threads
	if len(mem.Threads) != 0 {
		t.Errorf("Expected 0 threads with empty B-tree, got %d", len(mem.Threads))
	}

	// Reminder should be empty
	reminder := mem.FormatPausedWorkStreamsReminder()
	if reminder != "" {
		t.Error("Expected empty reminder with no paused workstreams")
	}

	t.Log("✓ Graceful handling of empty persisted state")
}

// TestRehydrationPreservesStructuredPlan validates that the structured plan is fully restored
func TestRehydrationPreservesStructuredPlan(t *testing.T) {
	ctx := context.Background()
	btreeStore := inmemory.NewBtree[string, any](false).Btree

	memory1 := NewShortTermMemory()
	threadID := sop.NewUUID()

	// Create thread with detailed structured plan
	thread := &ConversationThread{
		ID:          threadID,
		RootPrompt:  "Fix failing tests",
		Label:       "Test Repair",
		StreamState: WorkStreamPaused,
		StreamContext: &WorkStreamContext{
			CurrentPhase: "execution",
			StructuredPlan: &cw.StructuredPlan{
				Objective:     "Repair and validate parser behavior",
				WorkflowShape: "repair",
				Domain:        "coding_workflow",
				SystemContext: cw.StructuredSystemContext{
					Subsystem:         "parser",
					PrimaryTargets:    []string{"lib/parser.go", "lib/parser_test.go"},
					SupportingTargets: []string{"lib/errors.go"},
					Contracts:         []string{"parser errors stay stable"},
				},
				Steps: []cw.StructuredPlanStep{
					{StepID: "repair_1", Goal: "Localize the failing behavior", Status: "in_progress"},
				},
				CurrentStep:    0,
				CompletedSteps: []string{},
			},
			RequiresManualResume: true,
			PauseReason:          "testing persistence",
		},
	}

	memory1.Threads[threadID] = thread
	memory1.Order = []sop.UUID{threadID}

	// Persist and rehydrate
	if err := memory1.Persist(ctx, btreeStore); err != nil {
		t.Fatalf("Persist failed: %v", err)
	}

	memory2 := NewShortTermMemory()
	if err := memory2.Rehydrate(ctx, btreeStore); err != nil {
		t.Fatalf("Rehydrate failed: %v", err)
	}

	// Verify structured plan was restored
	rehydratedThread := memory2.Threads[threadID]
	if rehydratedThread.StreamContext == nil || rehydratedThread.StreamContext.StructuredPlan == nil {
		t.Fatal("StructuredPlan not restored")
	}

	plan := rehydratedThread.StreamContext.StructuredPlan
	if plan.Objective != "Repair and validate parser behavior" {
		t.Errorf("Objective not preserved: got %s", plan.Objective)
	}
	if plan.WorkflowShape != "repair" {
		t.Errorf("WorkflowShape not preserved: got %s", plan.WorkflowShape)
	}
	if plan.SystemContext.Subsystem != "parser" {
		t.Errorf("SystemContext not preserved: got %s", plan.SystemContext.Subsystem)
	}
	if len(plan.SystemContext.PrimaryTargets) != 2 {
		t.Errorf("PrimaryTargets not preserved: got %d", len(plan.SystemContext.PrimaryTargets))
	}

	t.Log("✓ StructuredPlan fully preserved across restart")
}
