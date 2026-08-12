# STM Work Stream Stack Design

## Core Insight: STM Over Queue

**User's Architecture**: Not a simple queue, but **Short-Term Memory (STM) tracking multiple concurrent work streams** with pause/resume capability.

**Why STM is Perfect**:
- ✅ Already has `ConversationThread` infrastructure
- ✅ Supports multiple concurrent threads (map + order)
- ✅ Has `CurrentThreadID` for active tracking
- ✅ Has `PromoteThread()` for switching
- ✅ Captures streaming thoughts in time

---

## Enhanced STM Structure

### 1. Work Stream Thread Extension

**Location**: Extend `lib/sop/ai/agent/memory_shortterm.go`

```go
// WorkStreamState tracks the execution state of a work stream
type WorkStreamState string

const (
    WorkStreamActive    WorkStreamState = "active"     // Currently executing
    WorkStreamPaused    WorkStreamState = "paused"     // Interrupted, waiting
    WorkStreamCompleted WorkStreamState = "completed"  // Reached delivery phase
    WorkStreamAbandoned WorkStreamState = "abandoned"  // User cleared/canceled
)

// PhasePlan defines the planned sequence of phases for a work stream
type PhasePlan struct {
    PlannedPhases    []string          // Full planned path: ["investigation", "planning", "execution", "review", "delivery"]
    CurrentIndex     int               // Which phase we're on (0-based)
    CompletedPhases  []string          // Phases already completed
    RemainingPhases  []string          // Phases yet to execute
    Checkpoints      map[string]bool   // Re-assess triggers: {"after_investigation": true}
    Reasoning        string            // Why this path was chosen
    Confidence       string            // "high", "medium", "low"
    LastUpdated      int64             // Timestamp of last path modification
}

// LoopDetectionState tracks iteration patterns to prevent infinite loops
type LoopDetectionState struct {
    MaxIterations      int      // e.g., 20 total phases (prevents runaway)
    MaxPhaseRepeats    int      // e.g., 3 times in same phase (prevents stuck phases)
    PatternWindow      int      // e.g., detect loops in last 10 phases
    DetectedLoops      []string // Patterns detected (for logging/debugging)
    EscalationLevel    int      // 0=none, 1=warning, 2=user prompt, 3=abort
}

// WorkStreamContext captures the phase progression and artifacts
type WorkStreamContext struct {
    CurrentPhase         string                // Current executing phase
    PhaseHistory         []string              // All phases executed: [intake, investigation, planning, ...]
    PhasePlan  *PhasePlan  // THE PLAN - persisted across pause/resume
    PhaseArtifacts       map[string]any        // Artifacts from each phase
    LastToolCalls        []string              // Tools called in this stream
    
    // Loop Detection & Iteration Tracking
    IterationCount       int                   // Total phase executions (for loop detection)
    PhaseIterations      map[string]int        // Per-phase counters: {"investigation": 3, "execution": 2}
    RecentPhasePattern   []string              // Last 10 phases for pattern detection
    LoopDetectionState   *LoopDetectionState   // Loop tracking configuration
    
    // Timestamps
    StartedAt            int64                 // When stream created
    PausedAt             int64                 // When paused (0 if active)
    CompletedAt          int64                 // When completed (0 if not done)
    
    // Work Stream Relationships
    ParentStreamID       sop.UUID              // The stream that was paused to start this
    InterjectionType     InterjectionType      // How this stream was created
}

// ConversationThread (ENHANCED)
type ConversationThread struct {
    ID         sop.UUID `json:"id"`
    RootPrompt string   `json:"root_prompt"`
    
    // Transcribed Context (Managed by LLM)
    Label        string `json:"label"`
    Category     string `json:"category"`
    ContextNotes string `json:"context_notes"`
    
    // The linear exchange of Q&A within this topic
    Exchanges []Interaction `json:"exchanges"`
    
    // Termination
    Conclusion string `json:"conclusion"`
    Status     string `json:"status"` // "active", "concluded"
    
    // WORK STREAM ADDITIONS
    StreamState   WorkStreamState    `json:"stream_state"`   // NEW
    StreamContext *WorkStreamContext `json:"stream_context"` // NEW
}

// InterjectionType defines how a work stream was created
type InterjectionType string

const (
    InterjectionNone        InterjectionType = "none"         // Initial user query
    InterjectionPauseNew    InterjectionType = "pause_new"    // Pause current, start new
    InterjectionClearRestart InterjectionType = "clear_restart" // Clear all, restart
    InterjectionRefine      InterjectionType = "refine"       // Refine current stream
)
```

### 2. Work Stream Stack Manager

**Location**: New file `lib/sop/ai/agent/workstream_stack.go`

```go
package agent

import (
    "context"
    "fmt"
    "sync"
    
    "github.com/sharedcode/sop"
    log "log/slog"
)

const (
    MaxConcurrentWorkStreams = 5 // User requirement
)

// WorkStreamStack manages multiple concurrent work streams via STM
type WorkStreamStack struct {
    stm           *ShortTermMemory
    mu            sync.RWMutex
    activeStack   []sop.UUID // Stack of paused stream IDs (LIFO for backtracking)
    maxDepth      int
}

// NewWorkStreamStack creates a work stream stack manager
func NewWorkStreamStack(stm *ShortTermMemory) *WorkStreamStack {
    return &WorkStreamStack{
        stm:         stm,
        activeStack: make([]sop.UUID, 0, MaxConcurrentWorkStreams),
        maxDepth:    MaxConcurrentWorkStreams,
    }
}

// GetActiveStream returns the currently active work stream
func (ws *WorkStreamStack) GetActiveStream() *ConversationThread {
    ws.mu.RLock()
    defer ws.mu.RUnlock()
    
    thread := ws.stm.GetCurrentThread()
    if thread != nil && thread.StreamState == WorkStreamActive {
        return thread
    }
    return nil
}

// GetPausedStreams returns all paused streams in stack order
func (ws *WorkStreamStack) GetPausedStreams() []*ConversationThread {
    ws.mu.RLock()
    defer ws.mu.RUnlock()
    
    var paused []*ConversationThread
    for _, id := range ws.activeStack {
        if thread := ws.stm.Threads[id]; thread != nil {
            if thread.StreamState == WorkStreamPaused {
                paused = append(paused, thread)
            }
        }
    }
    return paused
}

// PauseCurrentAndStartNew pauses the current stream and starts a new one
// Type 1 Interjection: Pause & New Stream
func (ws *WorkStreamStack) PauseCurrentAndStartNew(
    ctx context.Context,
    userQuery string,
    interjectionIntent *InterjectionIntent,
) (*ConversationThread, error) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    // Check depth limit
    if len(ws.activeStack) >= ws.maxDepth {
        return nil, fmt.Errorf("work stream stack full (max %d concurrent streams)", ws.maxDepth)
    }
    
    // Pause current stream
    currentThread := ws.stm.GetCurrentThread()
    if currentThread != nil && currentThread.StreamState == WorkStreamActive {
        currentThread.StreamState = WorkStreamPaused
        currentThread.StreamContext.PausedAt = time.Now().UnixMilli()
        
        // Push to stack
        ws.activeStack = append(ws.activeStack, currentThread.ID)
        
        log.Info("Paused work stream",
            "stream_id", currentThread.ID,
            "current_phase", currentThread.StreamContext.CurrentPhase,
            "stack_depth", len(ws.activeStack))
    }
    
    // Create new stream
    newStreamID := sop.NewUUID()
    newStream := &ConversationThread{
        ID:         newStreamID,
        RootPrompt: userQuery,
        Label:      extractLabel(userQuery),
        Category:   "Work Stream",
        Exchanges:  make([]Interaction, 0),
        Status:     "active",
        StreamState: WorkStreamActive,
        StreamContext: &WorkStreamContext{
            CurrentPhase:     "intake", // Start at intake
            PhaseHistory:     []string{"intake"},
            PhaseArtifacts:   make(map[string]any),
            StartedAt:        time.Now().UnixMilli(),
            ParentStreamID:   currentThread.ID, // Link to parent
            InterjectionType: InterjectionPauseNew,
        },
    }
    
    // Add to STM
    ws.stm.AddThread(newStream)
    ws.stm.CurrentThreadID = newStreamID
    
    log.Info("Started new work stream from interjection",
        "stream_id", newStreamID,
        "parent_stream", currentThread.ID,
        "stack_depth", len(ws.activeStack))
    
    return newStream, nil
}

// ClearAllAndRestart clears all streams and starts fresh
// Type 2 Interjection: Clear & Restart
func (ws *WorkStreamStack) ClearAllAndRestart(
    ctx context.Context,
    userQuery string,
    interjectionIntent *InterjectionIntent,
) (*ConversationThread, error) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    // Mark all streams as abandoned
    for _, id := range ws.activeStack {
        if thread := ws.stm.Threads[id]; thread != nil {
            thread.StreamState = WorkStreamAbandoned
            thread.Status = "concluded"
            thread.Conclusion = "Abandoned due to user restart"
        }
    }
    
    // Mark current as abandoned
    currentThread := ws.stm.GetCurrentThread()
    if currentThread != nil {
        currentThread.StreamState = WorkStreamAbandoned
        currentThread.Status = "concluded"
        currentThread.Conclusion = "Abandoned due to user restart"
    }
    
    // Clear stack
    abandonedCount := len(ws.activeStack) + 1 // +1 for current
    ws.activeStack = make([]sop.UUID, 0, ws.maxDepth)
    
    log.Info("Cleared all work streams for restart",
        "abandoned_count", abandonedCount)
    
    // Create fresh stream
    newStreamID := sop.NewUUID()
    newStream := &ConversationThread{
        ID:         newStreamID,
        RootPrompt: userQuery,
        Label:      extractLabel(userQuery),
        Category:   "Work Stream",
        Exchanges:  make([]Interaction, 0),
        Status:     "active",
        StreamState: WorkStreamActive,
        StreamContext: &WorkStreamContext{
            CurrentPhase:     "intake",
            PhaseHistory:     []string{"intake"},
            PhaseArtifacts:   make(map[string]any),
            StartedAt:        time.Now().UnixMilli(),
            InterjectionType: InterjectionClearRestart,
        },
    }
    
    ws.stm.AddThread(newStream)
    ws.stm.CurrentThreadID = newStreamID
    
    log.Info("Started fresh work stream after clear",
        "stream_id", newStreamID)
    
    return newStream, nil
}

// RefineCurrentStream modifies current stream (phase jump, context update)
// Type 3 Interjection: Refine Current
func (ws *WorkStreamStack) RefineCurrentStream(
    ctx context.Context,
    userQuery string,
    targetPhase string,
    interjectionIntent *InterjectionIntent,
) (*ConversationThread, error) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    currentThread := ws.stm.GetCurrentThread()
    if currentThread == nil {
        return nil, fmt.Errorf("no active work stream to refine")
    }
    
    if currentThread.StreamState != WorkStreamActive {
        return nil, fmt.Errorf("cannot refine non-active stream (state: %s)", currentThread.StreamState)
    }
    
    oldPhase := currentThread.StreamContext.CurrentPhase
    
    // Update phase if target specified
    if targetPhase != "" && targetPhase != oldPhase {
        currentThread.StreamContext.CurrentPhase = targetPhase
        currentThread.StreamContext.PhaseHistory = append(
            currentThread.StreamContext.PhaseHistory,
            targetPhase,
        )
        
        log.Info("Refined work stream with phase jump",
            "stream_id", currentThread.ID,
            "from_phase", oldPhase,
            "to_phase", targetPhase,
            "reason", interjectionIntent.Reasoning)
    }
    
    // Add user refinement to exchanges
    currentThread.Exchanges = append(currentThread.Exchanges, Interaction{
        Role:      RoleUser,
        Content:   userQuery,
        Timestamp: time.Now().Unix(),
        Entity:    "refinement",
    })
    
    return currentThread, nil
}

// CompleteCurrentAndResumePrevious completes active stream, backtracks to paused
func (ws *WorkStreamStack) CompleteCurrentAndResumePrevious(
    ctx context.Context,
    completionSummary string,
) (*ConversationThread, error) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    // Mark current as completed
    currentThread := ws.stm.GetCurrentThread()
    if currentThread != nil && currentThread.StreamState == WorkStreamActive {
        currentThread.StreamState = WorkStreamCompleted
        currentThread.Status = "concluded"
        currentThread.Conclusion = completionSummary
        currentThread.StreamContext.CompletedAt = time.Now().UnixMilli()
        
        log.Info("Completed work stream",
            "stream_id", currentThread.ID,
            "final_phase", currentThread.StreamContext.CurrentPhase,
            "summary", completionSummary)
    }
    
    // Check if there's a paused stream to resume
    if len(ws.activeStack) == 0 {
        log.Info("No paused streams to resume, work complete")
        return nil, nil // All work complete
    }
    
    // Pop from stack (LIFO - most recently paused)
    pausedStreamID := ws.activeStack[len(ws.activeStack)-1]
    ws.activeStack = ws.activeStack[:len(ws.activeStack)-1]
    
    pausedThread := ws.stm.Threads[pausedStreamID]
    if pausedThread == nil {
        return nil, fmt.Errorf("paused stream not found: %s", pausedStreamID)
    }
    
    // Resume paused stream
    pausedThread.StreamState = WorkStreamActive
    pausedThread.StreamContext.PausedAt = 0
    ws.stm.CurrentThreadID = pausedStreamID
    
    log.Info("Resumed paused work stream",
        "stream_id", pausedStreamID,
        "resume_phase", pausedThread.StreamContext.CurrentPhase,
        "stack_depth", len(ws.activeStack))
    
    // Add resumption marker to exchanges
    pausedThread.Exchanges = append(pausedThread.Exchanges, Interaction{
        Role:      RoleSystem,
        Content:   fmt.Sprintf("Resuming after completing nested stream: %s", completionSummary),
        Timestamp: time.Now().Unix(),
        Entity:    "work_stream_resume",
    })
    
    return pausedThread, nil
}

// UpdatePhase records phase transition in current stream
func (ws *WorkStreamStack) UpdatePhase(newPhase string) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    currentThread := ws.stm.GetCurrentThread()
    if currentThread == nil || currentThread.StreamContext == nil {
        return
    }
    
    oldPhase := currentThread.StreamContext.CurrentPhase
    if newPhase != oldPhase {
        currentThread.StreamContext.CurrentPhase = newPhase
        currentThread.StreamContext.PhaseHistory = append(
            currentThread.StreamContext.PhaseHistory,
            newPhase,
        )
        
        log.Info("Phase transition in work stream",
            "stream_id", currentThread.ID,
            "from", oldPhase,
            "to", newPhase)
    }
}

// RecordPhaseArtifact stores artifact from phase
func (ws *WorkStreamStack) RecordPhaseArtifact(phase string, artifact any) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    currentThread := ws.stm.GetCurrentThread()
    if currentThread == nil || currentThread.StreamContext == nil {
        return
    }
    
    currentThread.StreamContext.PhaseArtifacts[phase] = artifact
    log.Debug("Recorded phase artifact",
        "stream_id", currentThread.ID,
        "phase", phase)
}

// GetStackDepth returns current depth of paused streams
func (ws *WorkStreamStack) GetStackDepth() int {
    ws.mu.RLock()
    defer ws.mu.RUnlock()
    return len(ws.activeStack)
}

// GetStreamSummary returns summary for debugging/monitoring
func (ws *WorkStreamStack) GetStreamSummary() map[string]any {
    ws.mu.RLock()
    defer ws.mu.RUnlock()
    
    current := ws.stm.GetCurrentThread()
    summary := map[string]any{
        "stack_depth": len(ws.activeStack),
        "max_depth":   ws.maxDepth,
    }
    
    if current != nil {
        summary["active_stream"] = map[string]any{
            "id":            current.ID,
            "phase":         current.StreamContext.CurrentPhase,
            "state":         current.StreamState,
            "phase_history": current.StreamContext.PhaseHistory,
        }
    }
    
    pausedStreams := make([]map[string]any, 0, len(ws.activeStack))
    for _, id := range ws.activeStack {
        if thread := ws.stm.Threads[id]; thread != nil {
            pausedStreams = append(pausedStreams, map[string]any{
                "id":    thread.ID,
                "phase": thread.StreamContext.CurrentPhase,
                "label": thread.Label,
            })
        }
    }
    summary["paused_streams"] = pausedStreams
    
    return summary
}

// Helper
func extractLabel(query string) string {
    if len(query) <= 50 {
        return query
    }
    return query[:47] + "..."
}
```

---

## Phase Succession Path Persistence

### Why Persistence Matters

**Work streams can be paused/resumed**, so the phase succession plan MUST survive interruptions:

```
Stream 1: "Fix failing tests"
  Planning creates path: [investigation, planning, execution, review, delivery]
  Currently on: investigation (index 1)
  ↓
  [USER INTERJECTS]
  ↓
Stream 1: PAUSED (path saved in WorkStreamContext)
Stream 2: "Check auth bug" starts...
  ↓
  [Stream 2 completes]
  ↓
Stream 1: RESUMED
  ✅ Path still intact: [investigation, planning, execution, review, delivery]
  ✅ Current index: 1 (investigation)
  ✅ Continue from where we left off!
```

**Without persistence**: Plan would be lost, agent wouldn't know what to do next.

### Creating the Succession Path

**When**: Planning phase assesses the query and creates the plan.

```go
// Planning phase tool: planning_create_succession_path
func (c *PlanningPhaseCapability) createSuccessionPath(ctx, args) (string, error) {
    plannedPhases := args["phase_path"].([]string)
    checkpoints := args["checkpoints"].(map[string]bool)
    reasoning := args["reasoning"].(string)
    confidence := args["confidence"].(string)
    
    // Create succession path
    path := &PhasePlan{
        PlannedPhases:   plannedPhases,
        CurrentIndex:    0,  // Start at first phase
        CompletedPhases: []string{},
        RemainingPhases: plannedPhases,
        Checkpoints:     checkpoints,
        Reasoning:       reasoning,
        Confidence:      confidence,
        LastUpdated:     time.Now().UnixMilli(),
    }
    
    // Store in active work stream context
    workStreamStack := getWorkStreamStack(ctx)
    currentStream := workStreamStack.GetActiveStream()
    
    if currentStream != nil && currentStream.StreamContext != nil {
        currentStream.StreamContext.PhasePlan = path
        
        log.Info("Created phase succession path for work stream",
            "stream_id", currentStream.ID,
            "planned_phases", plannedPhases,
            "checkpoints", getCheckpointPhases(checkpoints),
            "reasoning", reasoning)
    }
    
    return fmt.Sprintf("Created execution plan: %s\nReasoning: %s", 
        formatPhasePath(plannedPhases), reasoning), nil
}
```

### Advancing Through the Path

**When**: After each phase completes successfully.

```go
// Advance to next phase in succession path
func (ws *WorkStreamStack) AdvanceToNextPhase(ctx context.Context) (string, bool, error) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    currentStream := ws.stm.GetCurrentThread()
    if currentStream == nil || currentStream.StreamContext == nil {
        return "", false, fmt.Errorf("no active work stream")
    }
    
    path := currentStream.StreamContext.PhasePlan
    if path == nil {
        return "", false, fmt.Errorf("no phase succession path defined")
    }
    
    // Mark current phase as completed
    if path.CurrentIndex < len(path.PlannedPhases) {
        completedPhase := path.PlannedPhases[path.CurrentIndex]
        path.CompletedPhases = append(path.CompletedPhases, completedPhase)
    }
    
    // Move to next phase
    path.CurrentIndex++
    
    // Check if we've completed all phases
    if path.CurrentIndex >= len(path.PlannedPhases) {
        log.Info("Phase succession path completed",
            "stream_id", currentStream.ID,
            "completed_phases", path.CompletedPhases)
        return "", true, nil  // true = path complete
    }
    
    // Get next phase
    nextPhase := path.PlannedPhases[path.CurrentIndex]
    path.RemainingPhases = path.PlannedPhases[path.CurrentIndex:]
    
    // Update current phase
    currentStream.StreamContext.CurrentPhase = nextPhase
    currentStream.StreamContext.PhaseHistory = append(
        currentStream.StreamContext.PhaseHistory,
        nextPhase,
    )
    
    log.Info("Advanced to next phase in succession path",
        "stream_id", currentStream.ID,
        "next_phase", nextPhase,
        "progress", fmt.Sprintf("%d/%d", path.CurrentIndex+1, len(path.PlannedPhases)),
        "remaining", path.RemainingPhases)
    
    return nextPhase, false, nil
}
```

### Re-Planning at Checkpoints

**When**: After a phase completes, if it's a checkpoint.

```go
// Check if current phase is a checkpoint, re-plan if needed
func (ws *WorkStreamStack) CheckpointReassess(
    ctx context.Context,
    phaseResult string,
) (*PhasePlan, bool, error) {
    
    currentStream := ws.stm.GetCurrentThread()
    path := currentStream.StreamContext.PhasePlan
    
    if path == nil {
        return nil, false, nil  // No path, no checkpoint
    }
    
    currentPhase := path.PlannedPhases[path.CurrentIndex]
    checkpointKey := fmt.Sprintf("after_%s", currentPhase)
    
    // Is this a checkpoint?
    if !path.Checkpoints[checkpointKey] {
        return nil, false, nil  // Not a checkpoint, continue
    }
    
    log.Info("Reached checkpoint, re-assessing",
        "stream_id", currentStream.ID,
        "phase", currentPhase)
    
    // Invoke Planning LLM to re-assess
    reassessment := invokePlanningReassess(ctx, currentPhase, phaseResult, path)
    
    if !reassessment.RerouteNeeded {
        // Continue with existing path
        log.Info("Checkpoint passed, continuing with plan",
            "stream_id", currentStream.ID)
        return path, false, nil
    }
    
    // Re-route needed! Update path
    newPath := &PhasePlan{
        PlannedPhases:   reassessment.NewPhases,
        CurrentIndex:    0,  // Restart from beginning of new path
        CompletedPhases: path.CompletedPhases,  // Keep history
        RemainingPhases: reassessment.NewPhases,
        Checkpoints:     reassessment.NewCheckpoints,
        Reasoning:       reassessment.Reasoning,
        Confidence:      reassessment.Confidence,
        LastUpdated:     time.Now().UnixMilli(),
    }
    
    currentStream.StreamContext.PhasePlan = newPath
    
    log.Info("Re-routed at checkpoint",
        "stream_id", currentStream.ID,
        "old_path", path.PlannedPhases,
        "new_path", newPath.PlannedPhases,
        "reason", reassessment.Reasoning)
    
    return newPath, true, nil  // true = re-routed
}
```

### Resuming a Paused Stream

**When**: Completing a nested stream, backtracking to paused parent.

```go
// Modified CompleteCurrentAndResumePrevious to restore succession path
func (ws *WorkStreamStack) CompleteCurrentAndResumePrevious(
    ctx context.Context,
    completionSummary string,
) (*ConversationThread, error) {
    ws.mu.Lock()
    defer ws.mu.Unlock()
    
    // Mark current as completed
    currentThread := ws.stm.GetCurrentThread()
    if currentThread != nil && currentThread.StreamState == WorkStreamActive {
        currentThread.StreamState = WorkStreamCompleted
        currentThread.Status = "concluded"
        currentThread.Conclusion = completionSummary
        currentThread.StreamContext.CompletedAt = time.Now().UnixMilli()
        
        log.Info("Completed work stream",
            "stream_id", currentThread.ID,
            "final_phase", currentThread.StreamContext.CurrentPhase,
            "summary", completionSummary)
    }
    
    // Check if there's a paused stream to resume
    if len(ws.activeStack) == 0 {
        log.Info("No paused streams to resume, work complete")
        return nil, nil
    }
    
    // Pop from stack (LIFO)
    pausedStreamID := ws.activeStack[len(ws.activeStack)-1]
    ws.activeStack = ws.activeStack[:len(ws.activeStack)-1]
    
    pausedThread := ws.stm.Threads[pausedStreamID]
    if pausedThread == nil {
        return nil, fmt.Errorf("paused stream not found: %s", pausedStreamID)
    }
    
    // Resume paused stream
    pausedThread.StreamState = WorkStreamActive
    pausedThread.StreamContext.PausedAt = 0
    ws.stm.CurrentThreadID = pausedStreamID
    
    // ✅ PHASE SUCCESSION PATH IS INTACT!
    path := pausedThread.StreamContext.PhasePlan
    if path != nil {
        log.Info("Resumed paused work stream with succession path",
            "stream_id", pausedStreamID,
            "current_phase", pausedThread.StreamContext.CurrentPhase,
            "planned_path", path.PlannedPhases,
            "progress", fmt.Sprintf("%d/%d phases complete", 
                path.CurrentIndex, len(path.PlannedPhases)),
            "remaining", path.RemainingPhases,
            "stack_depth", len(ws.activeStack))
    } else {
        log.Info("Resumed paused work stream (no succession path)",
            "stream_id", pausedStreamID,
            "current_phase", pausedThread.StreamContext.CurrentPhase,
            "stack_depth", len(ws.activeStack))
    }
    
    // Add resumption marker
    pausedThread.Exchanges = append(pausedThread.Exchanges, Interaction{
        Role:      RoleSystem,
        Content:   fmt.Sprintf("Resuming after completing nested stream: %s", completionSummary),
        Timestamp: time.Now().Unix(),
        Entity:    "work_stream_resume",
    })
    
    return pausedThread, nil
}
```

---

## Example: Phase Succession Path with Pause/Resume

### Scenario: Nested Work Streams with Plans

```
User: "Fix failing tests in auth module"
↓
Stream 1 created
↓
Planning Phase:
  LLM Assessment: "Need to investigate failures, plan fix, execute, verify"
  Creates succession path:
    planned_phases: [investigation, planning, execution, review, delivery]
    checkpoints: {after_investigation: true, after_execution: true}
    current_index: 0
    remaining: [investigation, planning, execution, review, delivery]
↓
Execute: Investigation phase (index 0)
  [Analyzing test failures...]
  
  USER INTERJECTS: "Also check the payment timeout bug"
  ↓
  Interjection: pause_new
  ↓
  PAUSE Stream 1:
    ✅ Succession path saved:
       planned_phases: [investigation, planning, execution, review, delivery]
       current_index: 0 (investigation not complete yet)
       completed_phases: []
    ✅ Current phase: investigation
    ✅ State: paused
  ↓
  CREATE Stream 2: "Check payment timeout bug"
    Stack: [Stream 1]
    ↓
    Planning Phase (Stream 2):
      Creates NEW succession path:
        planned_phases: [investigation, review, delivery]
        current_index: 0
    ↓
    Execute: Investigation (Stream 2)
      [Checking payment code...]
      Completes investigation
    ↓
    Advance: current_index = 1
    ↓
    Execute: Review (Stream 2)
      [Reviewing payment logic...]
      Completes review
    ↓
    Advance: current_index = 2
    ↓
    Execute: Delivery (Stream 2)
      "Payment timeout is expected behavior, not a bug."
    ↓
    Stream 2 COMPLETE!
    ↓
    BACKTRACK: Resume Stream 1
      ✅ Succession path RESTORED:
         planned_phases: [investigation, planning, execution, review, delivery]
         current_index: 0 (still on investigation!)
         completed_phases: []
      ✅ Current phase: investigation (where we left off)
      ✅ State: active
    ↓
    CONTINUE Stream 1: Investigation phase
      [Finishes analyzing auth test failures...]
      Investigation completes
    ↓
    Checkpoint: after_investigation
      Re-assess? LLM: "Findings as expected, continue"
    ↓
    Advance: current_index = 1
    ↓
    Execute: Planning phase (index 1)
      [Creates fix plan...]
      Planning completes
    ↓
    Advance: current_index = 2
    ↓
    Execute: Execution phase (index 2)
      [Applies fix, runs tests...]
      Tests PASS!
    ↓
    Checkpoint: after_execution
      Re-assess? LLM: "Tests pass, continue to review"
    ↓
    Advance: current_index = 3
    ↓
    Execute: Review phase (index 3)
      [Validates fix...]
      Review complete
    ↓
    Advance: current_index = 4
    ↓
    Execute: Delivery phase (index 4)
      "Auth tests fixed successfully!"
    ↓
    Stream 1 COMPLETE!
    Stack: []
    ↓
    ALL WORK DONE ✅
```

### Key Points

**Stream 1 Plan Survived**:
- Created before pause: `[investigation, planning, execution, review, delivery]`
- Saved during pause (in WorkStreamContext.PhasePlan)
- Restored on resume (exact same plan, same index!)
- Continued from where it left off

**Stream 2 Had Its Own Plan**:
- Different plan: `[investigation, review, delivery]`
- Executed independently
- Completed, then Stack popped

**No Plan Lost Through Cracks**:
- Each work stream maintains its own succession path
- Paths persist across pause/resume
- Progress tracked (CurrentIndex, CompletedPhases, RemainingPhases)

---

## Three Interjection Types

### Type 1: Pause & New Stream

**When**: User starts unrelated task while current stream active

**Example**:
```
[Active Stream: Fixing auth bug - Investigation Phase]
Agent: "Analyzing auth module code..."

User: "Can you also check the payments timeout issue?"
↓
Interjection Classification: pause_new
↓
Actions:
1. Pause current stream (auth bug investigation)
2. Push to stack: [auth_bug_stream]
3. Create new stream (payments timeout)
4. Set phase: intake
5. Continue with new stream
```

**Result**:
```
Stack: [auth_bug_stream]
Active: payments_timeout_stream (intake → investigation → ...)
```

### Type 2: Clear & Restart

**When**: User abandons all work, starts completely new goal

**Example**:
```
[Active Stream: Refactoring auth - Planning Phase]
[Paused Stream: Fixing tests - Investigation Phase]

User: "Nevermind all that, let's design the payments API"
↓
Interjection Classification: clear_restart
↓
Actions:
1. Mark all streams as abandoned
2. Clear stack: []
3. Create fresh stream (payments API design)
4. Set phase: intake
5. Start clean
```

**Result**:
```
Stack: []
Active: payments_api_design_stream (intake → ...)
Abandoned: [refactoring_auth_stream, fixing_tests_stream]
```

### Type 3: Refine Current Stream

**When**: User redirects/clarifies within current task

**Example**:
```
[Active Stream: Fixing tests - Investigation Phase]
Agent: "Reading test file line by line..."

User: "Actually, jump to planning - I know the issue, it's missing mock"
↓
Interjection Classification: refine (target_phase: planning)
↓
Actions:
1. Keep current stream active
2. Jump phase: investigation → planning
3. Update phase history: [intake, investigation, planning]
4. Continue in planning phase
```

**Result**:
```
Stack: (unchanged)
Active: fixing_tests_stream (now in planning phase)
```

---

## Integration with Ask Pipeline

**Location**: Modify `lib/sop/ai/agent/copilot.go`

```go
type CopilotAgent struct {
    // ... existing fields
    workStreamStack *WorkStreamStack // NEW
}

func (a *CopilotAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
    // Initialize work stream stack if needed
    if a.workStreamStack == nil {
        if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
            a.workStreamStack = NewWorkStreamStack(a.service.session.Memory)
        }
    }
    
    // Check for interjection
    activeStream := a.workStreamStack.GetActiveStream()
    if activeStream != nil && activeStream.StreamState == WorkStreamActive {
        // This might be an interjection!
        intent, isInterjection := a.classifyInterjection(ctx, query, activeStream)
        
        if isInterjection {
            return a.handleInterjectionWithStreams(ctx, query, intent, cfg)
        }
    }
    
    // Normal Ask flow with work stream tracking
    return a.askWithWorkStreams(ctx, query, cfg)
}

func (a *CopilotAgent) handleInterjectionWithStreams(
    ctx context.Context,
    query string,
    intent *InterjectionIntent,
    cfg *ai.ConfigMap,
) (string, error) {
    
    switch intent.Type {
    case InterjectionPauseNew:
        // Type 1: Pause current, start new
        newStream, err := a.workStreamStack.PauseCurrentAndStartNew(ctx, query, intent)
        if err != nil {
            return "", fmt.Errorf("failed to pause and start new stream: %w", err)
        }
        
        // Continue Ask with new stream
        response, err := a.ask(ctx, query, cfg)
        if err != nil {
            return response, err
        }
        
        // Check if new stream completed
        if newStream.StreamContext.CurrentPhase == "delivery" {
            // Backtrack to previous stream
            resumedStream, err := a.workStreamStack.CompleteCurrentAndResumePrevious(
                ctx,
                response,
            )
            if resumedStream != nil {
                // Continue with resumed stream
                return a.continueWorkStream(ctx, resumedStream, cfg)
            }
        }
        
        return response, nil
        
    case InterjectionClearRestart:
        // Type 2: Clear all, restart
        _, err := a.workStreamStack.ClearAllAndRestart(ctx, query, intent)
        if err != nil {
            return "", fmt.Errorf("failed to clear and restart: %w", err)
        }
        
        // Continue Ask with fresh stream
        return a.ask(ctx, query, cfg)
        
    case InterjectionRefine:
        // Type 3: Refine current stream
        _, err := a.workStreamStack.RefineCurrentStream(
            ctx,
            query,
            intent.TargetPhase,
            intent,
        )
        if err != nil {
            return "", fmt.Errorf("failed to refine stream: %w", err)
        }
        
        // Continue Ask in updated phase
        return a.ask(ctx, query, cfg)
        
    default:
        // Unknown intent, treat as normal query
        return a.ask(ctx, query, cfg)
    }
}

func (a *CopilotAgent) continueWorkStream(
    ctx context.Context,
    stream *ConversationThread,
    cfg *ai.ConfigMap,
) (string, error) {
    // Build continuation prompt
    prompt := fmt.Sprintf(
        "Continuing work stream: %s\nCurrent phase: %s\nContext: %s",
        stream.Label,
        stream.StreamContext.CurrentPhase,
        stream.ContextNotes,
    )
    
    return a.ask(ctx, prompt, cfg)
}
```

---

## Phase Integration

**Location**: Modify `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`

```go
// Each phase capture tool updates work stream
func (c *PhaseCapability) ExecuteTool(ctx, toolName, args) (string, error) {
    // ... existing logic
    
    // Update work stream phase
    if workStreamStack := GetWorkStreamStack(ctx); workStreamStack != nil {
        workStreamStack.UpdatePhase(c.id) // investigation, planning, etc.
    }
    
    // Handle transitions
    if artifact.TransitionTo != "" {
        // Record phase transition in work stream
        if workStreamStack := GetWorkStreamStack(ctx); workStreamStack != nil {
            workStreamStack.UpdatePhase(artifact.TransitionTo)
        }
    }
    
    // Check for delivery phase completion
    if c.id == "delivery" && artifact.TransitionDirection == "exit" {
        // Work stream complete, trigger backtracking
        if workStreamStack := GetWorkStreamStack(ctx); workStreamStack != nil {
            _, err := workStreamStack.CompleteCurrentAndResumePrevious(
                ctx,
                artifact.Summary,
            )
            // Signal Ask pipeline to resume previous stream
        }
    }
    
    return result, nil
}
```

---

## Example: Nested Interjections

**Scenario**: User creates 3 nested work streams

```
User: "Fix failing tests"
→ Create Stream 1: fix_tests
→ Phase: intake → investigation

[Stream 1 active, investigating test failures]
Agent: "Analyzing test code..."

User: "Also check the auth module dependencies"
→ Interjection Type 1: pause_new
→ Pause Stream 1
→ Create Stream 2: check_auth_deps
→ Stack: [Stream 1]
→ Phase: intake → investigation

[Stream 2 active, checking dependencies]
Agent: "Scanning package.json..."

User: "Wait, can you review the API documentation first?"
→ Interjection Type 1: pause_new
→ Pause Stream 2
→ Create Stream 3: review_api_docs
→ Stack: [Stream 1, Stream 2]
→ Phase: intake → investigation → review

[Stream 3 active, reviewing docs]
Agent: "Reading API docs... Found inconsistencies. Here's my review."
→ Phase: delivery (complete!)
→ Backtrack: Resume Stream 2

[Stream 2 resumed, checking dependencies]
Agent: "Continuing dependency check... Dependencies are up to date."
→ Phase: delivery (complete!)
→ Backtrack: Resume Stream 1

[Stream 1 resumed, fixing tests]
Agent: "Back to fixing tests. The auth dependency issue was resolved. Now applying fix..."
→ Phase: planning → execution → delivery (complete!)
→ Stack: []
→ All work streams complete!
```

**Final State**:
- Stream 3: Completed (API docs reviewed)
- Stream 2: Completed (Dependencies checked)
- Stream 1: Completed (Tests fixed)
- Stack: [] (empty)

---

## LLM Classification for Interjections

**Prompt**:
```
You are a work stream interjection classifier.

Current Work Stream:
- ID: {stream_id}
- Phase: {current_phase}
- Task: {root_prompt}
- Recent Activity: {last_exchanges}

User Interjection: "{user_query}"

Classify the interjection type:

1. **pause_new**: User starts unrelated task (pause current, new stream)
   - Signals: "also", "can you", "switch to", "let's look at"
   - Example: "Can you also check the payments bug?"

2. **clear_restart**: User abandons all work (clear stack, fresh start)
   - Signals: "nevermind", "forget that", "let's start over", "new task"
   - Example: "Forget all that, let's redesign the database"

3. **refine**: User redirects/clarifies current task (stay in stream, maybe jump phase)
   - Signals: "actually", "instead", "jump to", "skip to", "also add", "and also", "update the plan"
   - Example: "Actually, jump to planning - I know the fix"
   - Strict rule: this is not a fresh reset. Preserve the current workstream, merge new requirements into the existing plan/design, and update only the affected parts. Do not discard prior requirements unless the user explicitly says to replace them. If new requirements conflict with prior decisions, surface the conflict and resolve it explicitly.

Respond with JSON:
{
  "type": "pause_new|clear_restart|refine",
  "target_phase": "investigation|planning|execution|review|delivery|null",
  "confidence": 0.0-1.0,
  "reasoning": "Brief explanation, including whether new requirements should be merged with the current plan/design"
}
```

---

## Implementation Plan

### Phase 1: Core Work Stream Infrastructure (3-4 days)

**Files to create**:
- `lib/sop/ai/agent/workstream_stack.go` - WorkStreamStack manager

**Files to modify**:
- `lib/sop/ai/agent/memory_shortterm.go` - Add WorkStreamState, WorkStreamContext

**Tasks**:
1. ✅ Extend ConversationThread with stream fields
2. ✅ Implement WorkStreamStack manager
3. ✅ Implement three interjection handlers
4. ✅ Implement backtracking (CompleteCurrentAndResumePrevious)
5. ✅ Add phase tracking (UpdatePhase, RecordPhaseArtifact)

### Phase 2: Interjection Classification (2 days)

**Files to create**:
- `lib/sop/ai/agent/interjection_stream_classifier.go` - LLM classification for streams

**Tasks**:
1. ✅ Create LLM prompt for 3-type classification
2. ✅ Implement classifyInterjection()
3. ✅ Add tests for each interjection type

### Phase 3: Ask Pipeline Integration (2-3 days)

**Files to modify**:
- `lib/sop/ai/agent/copilot.go` - Integrate WorkStreamStack into Ask

**Tasks**:
1. ✅ Add workStreamStack field to CopilotAgent
2. ✅ Detect interjections in Ask pipeline
3. ✅ Route to handleInterjectionWithStreams()
4. ✅ Implement continueWorkStream() for resumption

### Phase 4: Phase Integration (2 days)

**Files to modify**:
- `lib/sop/ai/agent/codingworkflow/phase_capabilities.go` - Update work stream on phase transitions

**Tasks**:
1. ✅ Call UpdatePhase() in phase tools
2. ✅ Detect delivery completion
3. ✅ Trigger backtracking on completion

### Phase 5: Testing (3 days)

**Files to create**:
- `lib/sop/ai/agent/workstream_stack_test.go`
- `lib/sop/ai/agent/workstream_integration_test.go`

**Test scenarios**:
1. ✅ Single stream (no interjections)
2. ✅ Pause & new stream (Type 1)
3. ✅ Clear & restart (Type 2)
4. ✅ Refine current (Type 3)
5. ✅ Nested interjections (3 deep)
6. ✅ Backtracking after completion
7. ✅ Max depth enforcement
8. ✅ Phase transitions tracked

---

## Total Implementation: 12-14 Days

**Breakdown**:
- Phase 1 (Core): 3-4 days
- Phase 2 (Classification): 2 days
- Phase 3 (Ask Integration): 2-3 days
- Phase 4 (Phase Integration): 2 days
- Phase 5 (Testing): 3 days

---

## Work Stream Execution: Error Handling & Loop Detection

### Overview

Work streams must handle failures gracefully and detect infinite loops. See [PLANNING_FIRST_LLM_ORCHESTRATION.md](./PLANNING_FIRST_LLM_ORCHESTRATION.md#implementation-details-handling-critical-gaps) for complete implementation details covering all 7 critical gaps:

1. **Phase Completion Criteria** - Phase tools signal completion
2. **Handoff vs. Path Priority** - Checkpoint-based priority
3. **User Visibility** - Plan + Progress updates
4. **Error Handling** - Auto re-route with escalation
5. **Phase Information Sharing** - PhaseArtifacts + Handoff prompts  
6. **Loop Detection** - Pattern tracking & iteration limits
7. **Execution Flow** - Loop-based with pause points

### Loop Detection in Work Streams

Loop detection is critical for work streams since they can pause/resume, potentially repeating failed patterns across multiple streams.

**Key tracking metrics** (stored in WorkStreamContext):
- `IterationCount`: Total phases executed
- `PhaseIterations`: Per-phase repeat counts
- `RecentPhasePattern`: Last 10 phases for pattern detection
- `LoopDetectionState`: Configuration & escalation level

**Example**: Stream gets stuck in execution → investigation → planning loop three times. Loop detector escalates to user: "I'm stuck in a loop. Try a different approach?"

### Error Recovery in Work Streams

Phase failures are handled with automatic retry (2x), handoff-suggested re-routing, or user escalation:

```
Phase execution fails
  ↓
Check handoff for re-route suggestion
  ↓ (suggests investigation)
Auto re-route to investigation
  OR
  ↓ (no suggestion)
Retry in same phase (max 2 times)
  ↓ (still failing)
Escalate to user:
  "Phase failed 3 times. Continue, investigate, or cancel?"
```

Failures are recorded in `PhaseArtifacts` and iteration counts updated for loop detection.

### User Visibility for Work Streams

Each work stream shows:
- **Plan upfront**: "I'll investigate → plan → execute → review → deliver"
- **Progress updates**: "Phase 2/5: planning"
- **Completion summaries**: "✅ investigation complete - Found root cause"
- **Stack status**: Shows active + paused streams
- **Failure prompts**: "Execution failed. Retry, investigate, or cancel?"

This keeps users informed across pause/resume cycles.

### Checkpoint-Based Re-assessment

At checkpoints (e.g., after_execution), work stream may update succession path:

```
Planned path: [investigation, planning, execution, review, delivery]
Checkpoint: after_execution

Execution completes → Tests FAIL
  ↓
Checkpoint triggers re-assessment
  ↓
LLM: "Tests failed, need more investigation"
  ↓
New path: [investigation, planning, execution, review, delivery]
  ↓
CurrentIndex reset to 0 (investigation)
```

Path updates are saved in WorkStreamContext, surviving pause/resume.

### Cross-Stream Loop Awareness

When creating nested streams, loop patterns should carry over to avoid repeating failed approaches:

```go
// New stream inherits loop knowledge from parent
func PauseCurrentAndStartNew(...) {
    parentStream := ws.GetActiveStream()
    
    newStream.StreamContext.LoopDetectionState.DetectedLoops = 
        parentStream.StreamContext.LoopDetectionState.DetectedLoops
        
    // If parent was escalated, new stream starts with awareness
}
```

This prevents trying the same failing pattern in multiple streams.

---

## Key Benefits

### 1. **STM-Native**
- Leverages existing ConversationThread infrastructure
- Natural fit for streaming thoughts in time
- Already handles multiple threads

### 2. **True Backtracking**
- LIFO stack for nested work streams
- Automatic resumption after completion
- Maintains full context for each stream

### 3. **Phase-Aware with Succession Path Persistence** ⭐
- Tracks phase progression per stream
- **Stores complete phase succession plan** (planned phases, progress, checkpoints)
- **Plan survives pause/resume** - no work falls through cracks
- Stores artifacts per phase
- Supports phase jumping within stream
- Checkpoint-based re-assessment at critical phases

### 4. **Max 5 Concurrent**
- Enforces user requirement
- Prevents infinite nesting
- Clear error when limit reached

### 5. **Three Interjection Types**
- **Pause & New**: Independent tasks
- **Clear & Restart**: Fresh start
- **Refine**: Phase jump within current

---

## Comparison: Queue vs. STM Work Streams

| Aspect | Simple Queue | STM Work Streams ⭐ |
|--------|--------------|---------------------|
| **Foundation** | WorkQueue data structure | STM ConversationThreads |
| **Concurrency** | Flat queue | Nested stack (max 5) |
| **Backtracking** | No | Yes (LIFO resume) |
| **Phase Tracking** | No | Yes (per stream) |
| **Phase Succession Plan** | No | ✅ Persisted across pause/resume |
| **Artifacts** | No | Yes (per phase) |
| **Streaming Thoughts** | No | Yes (exchanges) |
| **History** | Lost on dequeue | Preserved in STM |
| **Plan Persistence** | N/A | ✅ Plans don't fall through cracks |
| **Complexity** | Simple | More sophisticated |

**User's Choice**: STM Work Streams - the sophisticated solution.

---

## Related Documentation

- [SIMPLE_ADAPTIVE_PHASE_ROUTING.md](./SIMPLE_ADAPTIVE_PHASE_ROUTING.md) - Phase routing with heuristics
- [USER_INTERJECTION_QUEUE_DESIGN.md](./USER_INTERJECTION_QUEUE_DESIGN.md) - Initial queue design (superseded by this)
- [PHASE_ROUTING_ANALYSIS.md](./PHASE_ROUTING_ANALYSIS.md) - Handoff vs. adaptive classification

---

**Last Updated**: 2026-07-29  
**Status**: Ready for implementation  
**Foundation**: STM ConversationThreads + Work Stream Stack
