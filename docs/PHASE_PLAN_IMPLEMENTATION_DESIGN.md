# Phase Plan Implementation Design

**Created**: 2026-07-30  
**Status**: Phases 1-5 Complete  
**Purpose**: Design documentation reconstructed from implementation

---

## Executive Summary

This document describes the **Phase Plan System** implementation - a Planning-First LLM orchestration architecture that enables AI agents to automatically plan and execute multi-phase workflows. The system implements a cognitive pattern where complex tasks are decomposed into phases (intake, investigation, planning, execution, review, delivery) with automatic progression and checkpoint reassessment.

**Key Design Principles**:
1. **Planning-First**: Every task starts with planning phase (universal entry point)
2. **Participant Pattern**: Capabilities are passive participants, not controllers
3. **Cursor-Based Progression**: PhasePlan uses a cursor to track current phase
4. **Explicit State Management**: State passed as parameters, not through context
5. **User Visibility**: Rich feedback shows progress, checkpoints, and roadmap

---

## Architecture Overview

### The Universal Thinking Sequence

Every task follows this cognitive pattern:

```
User Query
    ↓
Planning Phase (ALWAYS - Universal Entry)
    ├─ Assess: Do we understand the problem?
    ├─ Assess: Do we have enough context?
    ├─ Create Plan: Which phases are needed?
    └─ Route: Jump to first phase or follow default sequence
    ↓
Phase Execution (with Plan Guidance)
    ├─ Intake: Gather requirements
    ├─ Investigation: Analyze code/bugs
    ├─ Planning: Design approach
    ├─ Execution: Implement changes
    ├─ Review: Validate results
    └─ Delivery: Summarize and handoff
    ↓
Completion
```

### Phase Capability Architecture

Each phase is implemented as a **passive capability** that responds to the Ask pipeline:

```go
type PhaseCapability struct {
    sharedInfra *SharedInfrastructure  // Access to STM, queues, memory
    phaseID     string                 // "investigation", "planning", etc.
    name        string                 // "Investigation & Analysis"
    description string                 // Human-readable description
    captureTool string                 // "investigation_capture"
    stateTool   string                 // "investigation_state"
}
```

**Capability Methods** (Participant Pattern):
- `Classify()`: Determine if this capability is relevant to the query
- `BuildContext()`: Provide context to the LLM
- `ExposeTools()`: Declare available tools
- `ExecuteTool()`: Execute a tool when LLM calls it
- `Handoff()`: Return transition signal when phase completes

---

## Phase 1: Core Memory Structures

**Goal**: Establish data structures for phase planning and work stream tracking

### PhasePlan Structure

The heart of the phase plan system - a cursor-based plan that persists across turns:

```go
type PhasePlan struct {
    PlannedPhases   []string        // ["investigation", "planning", "execution"]
    CurrentIndex    int             // CURSOR: which phase we're on (0-based)
    CompletedPhases []string        // Phases already done
    RemainingPhases []string        // Phases yet to execute
    Checkpoints     map[string]bool // {"after_investigation": true}
    Reasoning       string          // Why this path was chosen
    Confidence      string          // "high", "medium", "low"
    LastUpdated     int64           // Timestamp
}
```

**Design Decision: Cursor-Based Advancement**

Why a cursor instead of popping from a queue?
- ✅ **Non-destructive**: Can look back at completed phases
- ✅ **Predictable**: CurrentIndex points to next phase to execute
- ✅ **Inspectable**: Can see full plan at any time
- ✅ **Simple**: Increment to advance, no complex queue management

**CurrentIndex Semantics**:
- `CurrentIndex = 0`: Ready to execute first phase
- `CurrentIndex = 1`: First phase complete, ready for second
- `CurrentIndex = N`: N phases complete, ready for (N+1)th
- `CurrentIndex >= len(Phases)`: All phases complete

### WorkStreamContext

Work stream context holds all phase-related state:

```go
type WorkStreamContext struct {
    CurrentPhase   string                    // Current executing phase
    PhaseHistory   []string                  // All phases executed
    PhasePlan      *PhasePlan                // THE PLAN - cursor and checkpoints
    PhaseArtifacts map[string]map[string]any // Results from each phase
    
    // Loop Detection
    IterationCount     int                 // Total phase executions
    PhaseIterations    map[string]int      // Per-phase counters
    RecentPhasePattern []string            // Last 10 phases for pattern detection
    LoopDetectionState *LoopDetectionState // Loop configuration
}
```

**Design Decision: Artifacts as map[string]any**

Why flexible map instead of typed structs?
- ✅ **Phase-specific data**: Each phase captures different information
- ✅ **Evolution**: Can add new fields without breaking existing code
- ✅ **LLM-friendly**: JSON structure easy for LLM to read/write
- ❌ **Type safety**: Loss of compile-time checking (accepted tradeoff)

### Planning Tools

Two core tools for plan management:

#### 1. `planning_assess_and_route`

Analyzes query and recommends starting phase:

```go
func detectStartingPhase(queryLower string) string {
    // Review-oriented: "review code", "audit", "verify"
    // Investigation-oriented: "fix", "debug", "error", "failing"
    // Planning-oriented: "design", "architect", "approach"
    // Execution-oriented: "apply patch", "update version"
    // Default: "intake" (gather requirements)
}
```

**Design Decision: Semantic Routing**

Why semantic analysis instead of simple keywords?
- ✅ **Context-aware**: "review code for bugs" → review (not investigation)
- ✅ **Phrase-based**: "fix bug" not just "bug" (reduces false positives)
- ✅ **Priority order**: Check specific patterns before generic ones
- ✅ **Fallback safe**: Defaults to intake for unclear queries

#### 2. `planning_create_succession_path`

Creates the phase plan with checkpoints:

```json
{
  "phases": ["investigation", "planning", "execution", "review", "delivery"],
  "checkpoints": ["after_investigation", "after_execution"],
  "reasoning": "Bug fix requires: investigate → plan → implement → validate",
  "confidence": "high"
}
```

**Result**: Stores PhasePlan in WorkStreamContext via STM

---

## Phase 2: Phase Completion & Information Sharing

**Goal**: Enable phases to signal completion and share context with next phase

### Completion Detection

Every `{phase}_capture` tool accepts a `status` field:

```go
status := readPhaseString(args, "status", "in_progress")

// Status values:
// - "in_progress": Still working on this phase
// - "complete": Phase done, ready to advance
// - "blocked": Can't proceed, need help
// - "failed": Attempted but failed
```

**Design Decision: Explicit Status**

Why require LLM to declare completion?
- ✅ **Intentional**: LLM must consciously decide when done
- ✅ **Prevents premature transitions**: No automatic timeout
- ✅ **Enables retries**: "failed" allows retry without advancing
- ✅ **User control**: User can see phase isn't rushing

### Structured Data Support

Artifacts now support `structured_data` map for phase-specific information:

```json
{
  "status": "complete",
  "summary": "Found missing import in auth.go:42",
  "structured_data": {
    "findings": ["Missing crypto/sha256 import", "Removed in refactor"],
    "affected_files": ["lib/auth/hash.go"],
    "affected_tests": ["TestAuthHash", "TestPasswordVerify"],
    "root_cause": "Import removed during cleanup without updating tests"
  }
}
```

**Design Decision: Structured Data Map**

Why structured data instead of just summary text?
- ✅ **Machine-readable**: Next phase can parse and use findings
- ✅ **Rich context**: Evidence, metrics, file paths, etc.
- ✅ **Queryable**: Can filter/search artifacts by specific fields
- ✅ **LLM-friendly**: Natural JSON structure for LLM to create

### Context-Rich Handoffs

When transitioning, include previous phase context:

```go
func gatherPreviousPhaseContext(artifact *PhaseArtifact) string {
    context := fmt.Sprintf("Previous Phase: %s\n", artifact.PhaseName)
    context += fmt.Sprintf("Summary: %s\n", artifact.Summary)
    
    if len(artifact.StructuredData) > 0 {
        context += "\nKey Findings:\n"
        // Include relevant structured data
    }
    
    return context
}
```

**Design Decision: Explicit Context Passing**

Why pass context vs. having LLM query it?
- ✅ **Immediate**: Context available in first turn of new phase
- ✅ **Relevant**: Only include information next phase needs
- ✅ **Efficient**: No extra LLM calls to retrieve context
- ✅ **Consistent**: Guarantees context format

---

## Phase 2.5: Work Stream Persistence

**Goal**: Persist phase plans across application restarts and pauses

### B-tree Storage

Phase plans stored in STM's B-tree storage:

```go
func (thread *ConversationThread) Persist() ([]byte, error) {
    // Serialize thread including StreamContext and PhasePlan
    return json.Marshal(thread)
}

func (thread *ConversationThread) Rehydrate(data []byte) error {
    // Deserialize thread, restoring all phase state
    return json.Unmarshal(data, thread)
}
```

**Design Decision: Full State Serialization**

Why serialize everything instead of incremental updates?
- ✅ **Atomic**: All-or-nothing write prevents corruption
- ✅ **Simple**: No complex delta tracking
- ✅ **Reliable**: Easy to verify integrity
- ❌ **Performance**: Larger writes (acceptable for STM scale)

### TTL and Cleanup

Sessions expire after 7 days:

```go
const SessionTTL = 7 * 24 * time.Hour

func (stm *ShortTermMemory) PruneExpiredSessions() int {
    cutoff := time.Now().Unix() - int64(SessionTTL.Seconds())
    // Remove threads older than cutoff
}
```

**Design Decision: 7-Day TTL**

Why 7 days instead of longer/shorter?
- ✅ **Long enough**: Covers weeklong projects with interruptions
- ✅ **Short enough**: Prevents unbounded growth
- ✅ **User-friendly**: Users expect week-old work to persist
- ⚠️ **Configurable**: Could be made user-configurable later

---

## Phase 3: Phase Plan Logic (Participant Pattern)

**Goal**: Implement phase-to-phase transitions following Participant Pattern

### STM Access via Explicit Parameters

**Critical Design Constraint**: NO state through context.Context

```go
// ❌ WRONG - State in context
ctx = context.WithValue(ctx, "stm", stm)

// ✅ CORRECT - Explicit parameters
type SharedInfrastructure struct {
    STMAccessor func() STMReader  // Function to get STM
}
```

**Design Decision: STMAccessor as Function**

Why a function instead of direct STM reference?
- ✅ **Lazy**: STM created only when needed
- ✅ **Testable**: Easy to inject test doubles
- ✅ **Thread-safe**: Each call gets current STM
- ✅ **No circular deps**: Avoids import cycles

### STMReader Interfaces

Layered interfaces for controlled access:

```go
type STMReader interface {
    GetCurrentThread() WorkStreamReader
}

type WorkStreamReader interface {
    GetStreamContext() WorkStreamContextReader
}

type WorkStreamContextReader interface {
    GetPhasePlan() PhasePlanReader
}

type PhasePlanReader interface {
    GetPlannedPhases() []string
    GetCurrentIndex() int
    GetCheckpoints() map[string]bool
    AdvancePhase() (string, bool)  // Advances cursor, returns next phase
    SetCurrentIndex(int)
}
```

**Design Decision: Layered Interfaces**

Why multiple interfaces instead of one big interface?
- ✅ **Principle of Least Privilege**: Each layer sees only what it needs
- ✅ **Testability**: Mock only the layer you're testing
- ✅ **Clarity**: Makes dependencies explicit
- ✅ **Evolution**: Can add methods at appropriate layer

### Phase Advancement Logic

When a phase completes (`status="complete"`), determine next phase:

```go
func (c *PhaseCapability) determineNextPhaseFromPlan() (string, string) {
    stm := c.sharedInfra.STMAccessor()
    plan := stm.GetCurrentThread().GetStreamContext().GetPhasePlan()
    
    currentIdx := plan.GetCurrentIndex()
    plannedPhases := plan.GetPlannedPhases()
    
    // Verify we're at the right phase
    if plannedPhases[currentIdx] != c.phaseID {
        // Sync issue - find current phase and update cursor
    }
    
    // Advance cursor to next phase
    nextPhase, hasNext := plan.AdvancePhase()  // ← KEY: Advances cursor
    
    return nextPhase, "forward"
}
```

**Design Decision: Cursor Advancement**

Why advance cursor during determination (not during execution)?
- ✅ **Atomic**: Determination and advancement happen together
- ✅ **Consistent**: Cursor always points to next phase to execute
- ✅ **Recoverable**: If execution fails, cursor stays correct
- ⚠️ **Careful**: Must ensure phase is actually complete before calling

### Priority Rules for Transitions

Three levels of priority:

```go
// 1. EXIT: Always honored (highest priority)
if artifact.ExitRequested {
    return "exit", ""
}

// 2. EXPLICIT: LLM-provided transition overrides plan
if artifact.TransitionTo != "" {
    return artifact.TransitionTo, artifact.TransitionDirection
}

// 3. AUTOMATIC: Use plan if no explicit override
nextPhase, direction := c.determineNextPhaseFromPlan()
return nextPhase, direction
```

**Design Decision: Priority Hierarchy**

Why this specific order?
- ✅ **User control**: Exit always respected (emergency brake)
- ✅ **LLM override**: At checkpoints, LLM can reassess and change course
- ✅ **Plan fallback**: Default path is the planned one
- ✅ **Predictable**: Clear precedence rules avoid confusion

---

## Phase 4: Checkpoint Logic

**Goal**: Enable plan reassessment at key decision points

### Checkpoint Detection

Checkpoints defined as "after_{phase}" markers:

```go
func (c *PhaseCapability) isCheckpointPhase() bool {
    plan := /* get plan */
    checkpoints := plan.GetCheckpoints()
    checkpointKey := fmt.Sprintf("after_%s", c.phaseID)
    return checkpoints[checkpointKey]
}
```

**Design Decision: "after_X" Convention**

Why "after_investigation" instead of "at_planning"?
- ✅ **Clear**: "after" indicates completion trigger
- ✅ **Flexible**: Can mark any phase as needing reassessment
- ✅ **Readable**: Maps naturally to workflow language
- ✅ **Consistent**: Same pattern for all checkpoints

### Checkpoint Behavior

At checkpoints, LLM can override planned path:

```go
if artifact.Status == "complete" && c.isCheckpointPhase() {
    // LLM can provide transition_to to override plan
    // Otherwise, plan's next phase is used
}
```

**Design Decision: Override vs. Mandatory**

Why allow override instead of forcing reassessment?
- ✅ **Efficiency**: LLM doesn't have to reassess if path still correct
- ✅ **Flexibility**: Can continue or change direction
- ✅ **User-friendly**: Doesn't force extra reasoning steps
- ✅ **Observable**: Clear when LLM overrides vs. follows plan

### Checkpoint Notifications

User sees checkpoint markers in output:

```
✅ Phase Investigation & Analysis marked as COMPLETE 🔍 (CHECKPOINT PHASE)

💡 TIP: At checkpoints, you can override the planned path by specifying 
a different transition_to if the situation has changed.
```

**Design Decision: Visual Checkpoint Indicators**

Why show checkpoints explicitly?
- ✅ **User awareness**: User knows when reassessment is possible
- ✅ **LLM guidance**: Reminds LLM it can override at this point
- ✅ **Transparency**: Makes decision points visible
- ✅ **Trust**: User sees system isn't rigidly following plan

---

## Phase 5: User Visibility

**Goal**: Provide rich feedback showing progress, roadmap, and checkpoints

### Enhanced Plan Display

When plan is created, show execution roadmap:

```
✅ PHASE PLAN CREATED

📋 EXECUTION ROADMAP:
1. Investigation & Analysis 🔍 (checkpoint)
   ↓
2. Planning & Design
   ↓
3. Execution & Implementation 🔍 (checkpoint)
   ↓
4. Review & Validation
   ↓
5. Delivery & Handoff

🚀 Next: Begin execution with the first phase (Investigation & Analysis)
```

**Design Decision: Upfront Roadmap**

Why show entire plan at creation?
- ✅ **User confidence**: User sees agent has a plan
- ✅ **Expectations**: User knows what phases to expect
- ✅ **Checkpoints visible**: User sees where reassessment happens
- ✅ **Trust building**: Transparency about approach

### Progress Tracking

After each phase completion, show progress:

```go
func (c *PhaseCapability) getProgressInfo() string {
    plan := /* get plan */
    currentIdx := plan.GetCurrentIndex()  // Number of completed phases
    total := len(plan.GetPlannedPhases())
    percentage := (currentIdx * 100) / total
    
    // Build visual progress bar
    progressBar := ""
    for i, phase := range phases {
        if i < currentIdx {
            progressBar += "   ✅ " + formatPhaseName(phase) + "\n"
        } else if i == currentIdx {
            progressBar += "   ⏭️  " + formatPhaseName(phase) + "\n"
        } else {
            progressBar += "   ⬜ " + formatPhaseName(phase) + "\n"
        }
    }
    
    return fmt.Sprintf("📊 PROGRESS: %d/%d phases complete (%d%%)\n\n%s",
        currentIdx, total, percentage, progressBar)
}
```

**Design Decision: Visual Progress Bar**

Why use emojis and visual formatting?
- ✅ **Immediate understanding**: User sees progress at a glance
- ✅ **Current position clear**: ⏭️ shows what's executing now
- ✅ **Motivation**: Seeing progress is encouraging
- ✅ **LLM-friendly**: Structured format LLM can generate

### Phase Name Formatting

Convert phase IDs to human-readable names:

```go
func formatPhaseName(phaseID string) string {
    names := map[string]string{
        "intake":        "Intake & Context",
        "investigation": "Investigation & Analysis",
        "planning":      "Planning & Design",
        "execution":     "Execution & Implementation",
        "review":        "Review & Validation",
        "delivery":      "Delivery & Handoff",
    }
    return names[phaseID]
}
```

**Design Decision: Descriptive Names**

Why not just capitalize phase IDs?
- ✅ **Professional**: "Investigation & Analysis" > "Investigation"
- ✅ **Clarity**: Explains what phase does
- ✅ **Consistency**: All names follow same pattern (Action & Purpose)
- ✅ **User-friendly**: More conversational

### Failure Guidance

When phases fail or block, show recovery options:

```
⚠️  Phase Execution & Implementation status: FAILED

🔧 OPTIONS:
   1. Retry this phase after addressing the issue
   2. Skip to a different phase by setting transition_to
   3. Request exit by setting exit=true

📊 PROGRESS: 3/5 phases complete (60%)
```

**Design Decision: Explicit Recovery Options**

Why list options instead of letting LLM figure it out?
- ✅ **Guidance**: LLM knows what actions are available
- ✅ **User awareness**: User sees system isn't stuck
- ✅ **Consistency**: Same options every time
- ✅ **Discovery**: Makes features visible (like transition_to override)

---

## Implementation Patterns

### The Participant Pattern (Critical)

**Rule**: Capabilities NEVER control execution flow

```go
// ❌ WRONG - Controller Pattern
func (c *PhaseCapability) ExecutePhaseSequence(phases []string) {
    for _, phase := range phases {  // ← LOOP = Controller!
        c.executePhase(phase)
    }
}

// ✅ CORRECT - Participant Pattern
func (c *PhaseCapability) ExecuteTool(toolName string, args map[string]any) {
    // 1. Do work for THIS phase
    artifact := captureArtifact(args)
    
    // 2. Enqueue NEXT phase (one item only)
    if artifact.Status == "complete" {
        nextPhase := determineNextPhaseFromPlan()
        queue.Enqueue(WorkItem{Actor: "phase:" + nextPhase})
    }
    
    // 3. Return - Ask loop processes next phase
    return "Phase complete", nil
}
```

**Why This Matters**:
- ✅ Each phase = separate Ask turn with full LLM reasoning
- ✅ User can interject between phases
- ✅ State is clean between turns
- ✅ Capabilities can't deadlock or infinite loop
- ✅ Ask pipeline maintains single point of control

### State Management Pattern

**Rule**: Pass state explicitly as parameters

```go
// ❌ WRONG - Hidden state
ctx = context.WithValue(ctx, "plan", plan)
processPhase(ctx)

// ✅ CORRECT - Explicit parameters
func processPhase(ctx context.Context, plan *PhasePlan, queue *WorkQueue) {
    // All dependencies visible in signature
}
```

**Why This Matters**:
- ✅ Dependencies are explicit and visible
- ✅ Easy to test (inject mock dependencies)
- ✅ No hidden coupling through context
- ✅ Follows Go idioms

### Cursor Pattern for Plans

**Rule**: Use cursor (index) not destructive queue operations

```go
// ❌ WRONG - Destructive
nextPhase := plan.PopPhase()  // Loses history

// ✅ CORRECT - Cursor-based
plan.CurrentIndex++
nextPhase := plan.PlannedPhases[plan.CurrentIndex]
```

**Why This Matters**:
- ✅ Non-destructive (can look back)
- ✅ Inspectable (see full plan anytime)
- ✅ Recoverable (can reset cursor if needed)
- ✅ Predictable (index arithmetic is simple)

---

## Testing Strategy

### Unit Tests by Phase

**Phase 1**: Core structures
- `TestPhasePlanCreation`: Verify plan storage
- `TestPhasePlanAdvancement`: Verify cursor advancement

**Phase 2**: Completion & context
- `TestPhaseCompletionDetection`: Status handling
- `TestStructuredDataCapture`: Artifact data

**Phase 3**: Phase plan logic
- `TestNextPhaseFromPlan`: Plan reading
- `TestPriorityRules`: Exit > Explicit > Plan

**Phase 4**: Checkpoints
- `TestCheckpointDetection`: Checkpoint identification
- `TestCheckpointOverride`: LLM override at checkpoints

**Phase 5**: User visibility
- `TestPlanVisibilityFormatting`: Roadmap display
- `TestProgressVisibility`: Progress tracking
- `TestFailureVisibility`: Failure guidance

### Test Adapters Pattern

Use adapters to inject STM in tests:

```go
type testSTMAdapter struct {
    plan *PhasePlan
}

func (a *testSTMAdapter) GetCurrentThread() WorkStreamReader {
    return &testWorkStreamAdapter{plan: a.plan}
}
```

**Why Adapters**:
- ✅ No real STM needed in tests
- ✅ Control exact test conditions
- ✅ Fast (no I/O)
- ✅ Isolated (tests don't interfere)

---

## Design Decisions Summary

### What Worked Well

1. **Cursor-Based Plans**: Simple, predictable, inspectable
2. **Participant Pattern**: Clean separation of concerns
3. **Structured Artifacts**: Rich context sharing between phases
4. **Explicit Parameters**: No hidden dependencies
5. **Visual Feedback**: Users and LLM get clear progress info
6. **Priority Rules**: Clear precedence for transitions
7. **Checkpoint Pattern**: Flexible reassessment without rigidity

### Tradeoffs Made

1. **Flexibility vs. Type Safety**: map[string]any for artifacts
   - **Chose**: Flexibility (LLM needs evolving schemas)
   
2. **Performance vs. Simplicity**: Full serialization on persist
   - **Chose**: Simplicity (STM scale doesn't need optimization)
   
3. **User Visibility vs. Conciseness**: Verbose progress displays
   - **Chose**: Visibility (transparency builds trust)
   
4. **Immediate vs. Lazy**: STMAccessor as function
   - **Chose**: Lazy (better testability, no circular deps)

### Lessons Learned

1. **Controller Pattern is Tempting**: It's natural to want loops, but violates architecture
2. **Context.Context is for Cancellation**: Not for application state
3. **Visual Feedback Matters**: Emojis and progress bars improve UX significantly
4. **Checkpoints Need Visibility**: Users should know when reassessment is possible
5. **Test Adapters Essential**: STM interfaces must be mockable for testing

---

## Future Work (Phase 6)

### Integration Testing

- End-to-end tests with real LLM
- Multi-phase workflow scenarios
- Error recovery testing
- Pause/resume testing

### Performance Optimization

- Lazy artifact loading
- Incremental persistence (if needed)
- Checkpoint performance metrics

### Enhanced Features

- Parallel phase execution (when phases independent)
- Conditional branching (if-then-else in plans)
- Phase templates (common patterns)
- Plan visualization tools

---

## References

### Implementation Files

- `lib/sop/ai/agent/memory_shortterm.go`: PhasePlan and WorkStreamContext
- `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`: Phase implementations
- `lib/sop/ai/agent/codingworkflow/shared_infrastructure.go`: STM interfaces
- `lib/sop/ai/agent/copilot.go`: Ask pipeline and adapters

### Test Files

- `lib/sop/ai/agent/phase_succession_test.go`: Core phase plan tests
- `lib/sop/ai/agent/user_visibility_test.go`: Visibility feature tests
- `lib/sop/ai/agent/workstream_persistence_test.go`: Persistence tests

### Documentation

- `docs/PHASE_PLAN_GUIDE.md`: Comprehensive guide (merged doc)
- `.github/copilot-instructions.md`: Architecture constraints

---

**Document Status**: Reconstructed from Phases 1-5 implementation  
**Last Updated**: 2026-07-30  
**Implementation Complete**: 70% (Phases 1-5)  
**Remaining**: Phase 6 (Integration & Testing)
