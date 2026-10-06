package agent

import (
	"context"
	"testing"
	"time"

	"github.com/sharedcode/sop"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/inmemory"
)

// TestShortTermMemory_PersistAndRehydrate tests the basic persist/rehydrate cycle
func TestShortTermMemory_PersistAndRehydrate(t *testing.T) {
	ctx := context.Background()

	// Create in-memory B-tree for testing
	btreeInstance := inmemory.NewBtree[string, any](false).Btree

	// Create a ShortTermMemory with work streams
	stm := NewShortTermMemory()

	// Add a thread with work stream context
	threadID := sop.NewUUID()
	thread := &ConversationThread{
		ID:          threadID,
		Label:       "Test Feature Implementation",
		StreamState: WorkStreamActive,
		StreamContext: &WorkStreamContext{
			CurrentPhase:  "execution",
			WorkspaceRoot: "/workspace/project",
			StructuredPlan: &cw.StructuredPlan{
				Objective:     "Repair and validate parser behavior",
				WorkflowShape: "repair",
				Domain:        "coding_workflow",
				SystemContext: cw.StructuredSystemContext{
					Subsystem:         "parser",
					PrimaryTargets:    []string{"lib/parser.go", "lib/parser_test.go"},
					SupportingTargets: []string{"lib/errors.go"},
					Contracts:         []string{"parser errors stay stable for malformed input"},
				},
				Steps: []cw.StructuredPlanStep{{
					StepID:            "repair_1",
					Goal:              "Localize the failing behavior",
					ActionInstruction: "Read test and implementation together",
					Status:            "active",
				}},
				CurrentStep:    0,
				CompletedSteps: []string{},
			},
			PhaseHistory:   []string{"planning", "intake", "execution"},
			IterationCount: 3,
			PhaseIterations: map[string]int{
				"planning":  1,
				"intake":    1,
				"execution": 1,
			},
			RequiresManualResume: false,
		},
		Exchanges: []Interaction{
			{Role: RoleUser, Content: "Implement feature X"},
			{Role: RoleAssistant, Content: "I'll implement feature X"},
		},
	}

	stm.Threads[threadID] = thread
	stm.Order = []sop.UUID{threadID}
	stm.CurrentThreadID = threadID

	// Persist the state
	if err := stm.Persist(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to persist STM: %v", err)
	}

	// Rehydrate into a new STM
	stm2 := NewShortTermMemory()
	if err := stm2.Rehydrate(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to rehydrate STM: %v", err)
	}

	// Verify the state was restored
	if len(stm2.Threads) != 1 {
		t.Errorf("Expected 1 thread, got %d", len(stm2.Threads))
	}

	if len(stm2.Order) != 1 {
		t.Errorf("Expected 1 item in order, got %d", len(stm2.Order))
	}

	if stm2.CurrentThreadID != threadID {
		t.Errorf("Expected CurrentThreadID %v, got %v", threadID, stm2.CurrentThreadID)
	}

	// Verify thread details
	restoredThread, exists := stm2.Threads[threadID]
	if !exists {
		t.Fatal("Thread not restored")
	}

	if restoredThread.Label != thread.Label {
		t.Errorf("Expected label %q, got %q", thread.Label, restoredThread.Label)
	}

	if restoredThread.StreamState != WorkStreamActive {
		t.Errorf("Expected StreamState %v, got %v", WorkStreamActive, restoredThread.StreamState)
	}

	if restoredThread.StreamContext == nil {
		t.Fatal("StreamContext not restored")
	}

	if restoredThread.StreamContext.CurrentPhase != "execution" {
		t.Errorf("Expected CurrentPhase %s, got %s", "execution", restoredThread.StreamContext.CurrentPhase)
	}
	if restoredThread.StreamContext.WorkspaceRoot != "/workspace/project" {
		t.Fatalf("expected workspace root to persist, got %q", restoredThread.StreamContext.WorkspaceRoot)
	}
	if restoredThread.StreamContext.StructuredPlan == nil {
		t.Fatal("StructuredPlan not restored")
	}
	if restoredThread.StreamContext.StructuredPlan.Objective != "Repair and validate parser behavior" {
		t.Fatalf("expected structured plan objective to persist, got %#v", restoredThread.StreamContext.StructuredPlan)
	}
	if got := restoredThread.StreamContext.StructuredPlan.SystemContext.PrimaryTargets; len(got) != 2 || got[0] != "lib/parser.go" {
		t.Fatalf("expected structured plan primary targets to persist, got %#v", got)
	}

	if len(restoredThread.StreamContext.PhaseHistory) != 3 {
		t.Errorf("Expected 3 phases in history, got %d", len(restoredThread.StreamContext.PhaseHistory))
	}

	if len(restoredThread.Exchanges) != 2 {
		t.Errorf("Expected 2 exchanges, got %d", len(restoredThread.Exchanges))
	}
}

func TestWorkStreamContextAdapter_GetStructuredPlan(t *testing.T) {
	ctx := &WorkStreamContext{
		StructuredPlan: &cw.StructuredPlan{
			Objective:      "Investigate and repair failing tests",
			WorkflowShape:  "repair",
			Domain:         "coding_workflow",
			SystemContext:  cw.StructuredSystemContext{Subsystem: "parser", PrimaryTargets: []string{"lib/parser.go", "lib/parser_test.go"}},
			Steps:          []cw.StructuredPlanStep{{StepID: "s1", Goal: "Localize failure", Status: "active"}},
			CurrentStep:    0,
			CompletedSteps: []string{},
			CompletionGate: cw.StructuredCompletionGate{Requirements: []string{"rerun the failing test"}},
		},
	}
	adapter := &workStreamContextAdapter{ctx: ctx}
	plan := adapter.GetStructuredPlan()
	if plan == nil {
		t.Fatal("expected structured plan reader")
	}
	if got := plan.GetObjective(); got != "Investigate and repair failing tests" {
		t.Fatalf("expected objective to round-trip, got %q", got)
	}
	if got := plan.GetSystemContext().Subsystem; got != "parser" {
		t.Fatalf("expected subsystem to round-trip, got %q", got)
	}
	if got := plan.GetCompletionGate().Requirements; len(got) != 1 || got[0] != "rerun the failing test" {
		t.Fatalf("expected completion gate to round-trip, got %#v", got)
	}
}

// TestShortTermMemory_RehydrateExpiredSession tests that expired sessions are handled gracefully
func TestShortTermMemory_RehydrateExpiredSession(t *testing.T) {
	ctx := context.Background()

	btreeInstance := inmemory.NewBtree[string, any](false).Btree

	// Create a STM with expired state
	stm := NewShortTermMemory()
	threadID := sop.NewUUID()
	thread := &ConversationThread{
		ID:          threadID,
		Label:       "Old Work Stream",
		StreamState: WorkStreamPaused,
	}
	stm.Threads[threadID] = thread
	stm.Order = []sop.UUID{threadID}

	// Persist the state first
	if err := stm.Persist(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to persist: %v", err)
	}

	// Manually corrupt the expiration time to make it expired
	found, _ := btreeInstance.Find(ctx, "stm_session_state", false)
	if !found {
		t.Fatal("Session state not found after persist")
	}

	val, _ := btreeInstance.GetCurrentValue(ctx)
	if payload, ok := val.(map[string]any); ok {
		payload["expires_at"] = time.Now().Add(-24 * time.Hour).Unix() // Expire it
		btreeInstance.Add(ctx, "stm_session_state", payload)           // Update with expired time
	}

	// Try to rehydrate - should gracefully return nil (empty state)
	stm2 := NewShortTermMemory()
	err := stm2.Rehydrate(ctx, btreeInstance)

	// Should not error, just start with empty state
	if err != nil {
		t.Errorf("Rehydrate should not error on expired session, got: %v", err)
	}

	// State should remain empty (expired session ignored)
	if len(stm2.Threads) != 0 {
		t.Errorf("Expected 0 threads after rehydrating expired session, got %d", len(stm2.Threads))
	}
}

// TestShortTermMemory_PersistPreservesPhaseArtifacts tests that phase artifacts are preserved
func TestShortTermMemory_PersistPreservesPhaseArtifacts(t *testing.T) {
	ctx := context.Background()

	btreeInstance := inmemory.NewBtree[string, any](false).Btree

	stm := NewShortTermMemory()
	threadID := sop.NewUUID()

	// Create artifacts
	artifacts := map[string]map[string]any{
		"planning": {
			"approach":        "Test-driven development",
			"files":           []string{"main.go", "main_test.go"},
			"estimated_hours": 4,
		},
		"execution": {
			"tests_written": 5,
			"tests_passing": 3,
		},
	}

	thread := &ConversationThread{
		ID:          threadID,
		Label:       "Feature with Artifacts",
		StreamState: WorkStreamActive,
		StreamContext: &WorkStreamContext{
			CurrentPhase:   "execution",
			PhaseArtifacts: artifacts,
		},
	}

	stm.Threads[threadID] = thread
	stm.Order = []sop.UUID{threadID}
	stm.CurrentThreadID = threadID

	// Persist
	if err := stm.Persist(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to persist: %v", err)
	}

	// Rehydrate
	stm2 := NewShortTermMemory()
	if err := stm2.Rehydrate(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to rehydrate: %v", err)
	}

	// Verify artifacts restored
	restoredThread := stm2.Threads[threadID]
	if restoredThread.StreamContext == nil {
		t.Fatal("StreamContext not restored")
	}

	if len(restoredThread.StreamContext.PhaseArtifacts) != 2 {
		t.Errorf("Expected 2 phase artifacts, got %d", len(restoredThread.StreamContext.PhaseArtifacts))
	}

	planningArtifact, exists := restoredThread.StreamContext.PhaseArtifacts["planning"]
	if !exists {
		t.Fatal("Planning artifact not restored")
	}

	if planningArtifact["approach"] != "Test-driven development" {
		t.Errorf("Expected approach 'Test-driven development', got %v", planningArtifact["approach"])
	}
}

// TestShortTermMemory_PersistPreservesManualResumeState tests that RequiresManualResume is preserved
func TestShortTermMemory_PersistPreservesManualResumeState(t *testing.T) {
	ctx := context.Background()

	btreeInstance := inmemory.NewBtree[string, any](false).Btree

	stm := NewShortTermMemory()
	threadID := sop.NewUUID()

	thread := &ConversationThread{
		ID:          threadID,
		Label:       "Paused Work Stream",
		StreamState: WorkStreamPaused,
		StreamContext: &WorkStreamContext{
			CurrentPhase:         "execution",
			RequiresManualResume: true,
			PauseReason:          "User requested to work on something else",
		},
	}

	stm.Threads[threadID] = thread
	stm.Order = []sop.UUID{threadID}

	// Persist
	if err := stm.Persist(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to persist: %v", err)
	}

	// Rehydrate
	stm2 := NewShortTermMemory()
	if err := stm2.Rehydrate(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to rehydrate: %v", err)
	}

	// Verify manual resume state
	restoredThread := stm2.Threads[threadID]
	if restoredThread.StreamState != WorkStreamPaused {
		t.Errorf("Expected StreamState %v, got %v", WorkStreamPaused, restoredThread.StreamState)
	}

	if restoredThread.StreamContext == nil {
		t.Fatal("StreamContext not restored")
	}

	if !restoredThread.StreamContext.RequiresManualResume {
		t.Error("Expected RequiresManualResume to be true")
	}

	if restoredThread.StreamContext.PauseReason != "User requested to work on something else" {
		t.Errorf("Expected PauseReason to be preserved, got %q", restoredThread.StreamContext.PauseReason)
	}

	// Verify helper methods still work
	if restoredThread.StreamContext.RequiresManualResume != true {
		t.Error("RequiresManualResume should be true")
	}

	if !restoredThread.IsWaitingForManualResume() {
		t.Error("IsWaitingForManualResume should return true")
	}

	if restoredThread.CanAutoResume() {
		t.Error("CanAutoResume should return false")
	}
}

// TestShortTermMemory_PersistPreservesParentChildRelationships tests nested work streams
func TestShortTermMemory_PersistPreservesParentChildRelationships(t *testing.T) {
	ctx := context.Background()

	btreeInstance := inmemory.NewBtree[string, any](false).Btree

	stm := NewShortTermMemory()

	// Create parent work stream
	parentID := sop.NewUUID()
	parentThread := &ConversationThread{
		ID:          parentID,
		Label:       "Parent Feature",
		StreamState: WorkStreamPaused,
		StreamContext: &WorkStreamContext{
			CurrentPhase: "execution",
		},
	}

	// Create child work stream
	childID := sop.NewUUID()
	childThread := &ConversationThread{
		ID:          childID,
		Label:       "Child Investigation",
		StreamState: WorkStreamActive,
		StreamContext: &WorkStreamContext{
			CurrentPhase:     "investigation",
			ParentStreamID:   parentID,
			InterjectionType: InterjectionPauseNew,
		},
	}

	stm.Threads[parentID] = parentThread
	stm.Threads[childID] = childThread
	stm.Order = []sop.UUID{parentID, childID}
	stm.CurrentThreadID = childID

	// Persist
	if err := stm.Persist(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to persist: %v", err)
	}

	// Rehydrate
	stm2 := NewShortTermMemory()
	if err := stm2.Rehydrate(ctx, btreeInstance); err != nil {
		t.Fatalf("Failed to rehydrate: %v", err)
	}

	// Verify parent-child relationship
	restoredChild := stm2.Threads[childID]
	if restoredChild.StreamContext == nil {
		t.Fatal("Child StreamContext not restored")
	}

	if restoredChild.StreamContext.ParentStreamID != parentID {
		t.Errorf("Expected ParentStreamID %v, got %v", parentID, restoredChild.StreamContext.ParentStreamID)
	}

	if restoredChild.StreamContext.InterjectionType != InterjectionPauseNew {
		t.Errorf("Expected InterjectionType %v, got %v", InterjectionPauseNew, restoredChild.StreamContext.InterjectionType)
	}

	// Verify both threads restored
	if len(stm2.Threads) != 2 {
		t.Errorf("Expected 2 threads, got %d", len(stm2.Threads))
	}

	// Verify order preserved
	if len(stm2.Order) != 2 || stm2.Order[0] != parentID || stm2.Order[1] != childID {
		t.Error("Thread order not preserved correctly")
	}
}

// TestPruneExpiredSessions tests the cleanup of expired sessions
func TestPruneExpiredSessions(t *testing.T) {
	ctx := context.Background()

	btreeInstance := inmemory.NewBtree[string, any](false).Btree

	// Add an expired session
	expiredPayload := map[string]any{
		"id":           "stm_session_state",
		"expires_at":   time.Now().Add(-24 * time.Hour).Unix(),
		"threads_json": "{}",
		"order_json":   "[]",
		"current_id":   "",
	}

	_, err := btreeInstance.Add(ctx, "stm_session_state", expiredPayload)
	if err != nil {
		t.Fatalf("Failed to add expired session: %v", err)
	}

	// Prune expired sessions
	removedCount, err := PruneExpiredSessions(ctx, btreeInstance, time.Now())
	if err != nil {
		t.Fatalf("PruneExpiredSessions failed: %v", err)
	}

	if removedCount != 1 {
		t.Errorf("Expected 1 session pruned, got %d", removedCount)
	}

	// Verify session was removed
	found, _ := btreeInstance.Find(ctx, "stm_session_state", false)
	if found {
		t.Error("Expired session should have been removed")
	}
}
