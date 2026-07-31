# User Interjection Queue Design

**Status**: ✅ Implemented (LLM-as-Orchestrator pattern)  
**Last Updated**: 2026-07-30  
**Implementation**: Tool-based WorkStream management

## Problem Statement

**Original Behavior**: When a user sends a new prompt while an Ask request is being processed, it goes to event handlers but doesn't interrupt the current Ask flow.

**Implemented Solution**: User interjections are handled via **LLM-as-Orchestrator pattern**:
1. LLM sees WorkStream state in context (active/paused streams)
2. LLM decides when to pause/resume/jump phases based on user intent
3. LLM calls atomic tools (`pause_workstream`, `resume_workstream`, `jump_to_phase`)
4. No automatic detection - LLM makes all orchestration decisions

---

## Implemented Architecture (V2)

### Core Principle: LLM-as-Orchestrator

**Pattern**: Agent code provides **tools as infrastructure**, LLM sees state and **orchestrates via tool calls**.

- ❌ No automatic detection of interjections
- ❌ No automatic handling of pause/resume
- ❌ No InterjectionContext or detectInterjection() functions
- ✅ LLM sees WorkStream state in system prompt
- ✅ LLM decides when to pause/resume/jump based on user intent
- ✅ LLM calls atomic tools to manage WorkStreams

### Implementation Files

**WorkQueue with Priorities** (`lib/sop/ai/agent/workqueue.go`):
```go
const (
    PriorityUser           = 100  // User queries always processed first
    PrioritySystemResume   = 50   // System-triggered resume
    PriorityPhaseSuccession = 10  // Normal phase-to-phase transitions
)

type WorkItem struct {
    Query    string
    Actor    string
    Priority int    // Higher = processed first
    Context  map[string]any
    // ...
}
```

**WorkStream Management Capability** (`lib/sop/ai/agent/capabilities/workstream.go`):
```go
type WorkStreamManagement struct {
    stmAccessor     func() STMReader        // Access to short-term memory
    mutatorAccessor func() PhasePlanMutator // Access to phase plan mutator
}

// 6 atomic tools exposed to LLM:
// 1. list_workstreams()           - No parameters
// 2. get_workstream_details()     - No parameters  
// 3. jump_to_phase(phase: string) - phase enum: [intake, investigation, planning, execution, review, delivery]
// 4. pause_workstream()           - No parameters
// 5. resume_workstream()          - No parameters
// 6. abandon_workstream()         - No parameters
```

**V2 Ask Pipeline** (`lib/sop/ai/agent/copilot_v2.go`):
```go
func (a *CopilotAgent) AskV2(ctx, query, cfg) (string, error) {
    queue := a.getOrCreateSessionQueue()
    
    // Enqueue user query with highest priority
    queue.Enqueue(WorkItem{
        Query:    query,
        Actor:    "user",
        Priority: PriorityUser,
    })
    
    // Process until queue empty
    for !queue.IsEmpty() {
        item := queue.Dequeue()  // Highest priority first
        response := a.askV2Core(ctx, item.Query, cfg)
        // LLM may have called tools that enqueued more work
    }
    
    return lastResponse, nil
}
```

---

## WorkStream Tools Reference

### 1. `list_workstreams()`

**Purpose**: Show active/paused WorkStream state with progress indicators

**Parameters**: None (calling IS the decision)

**Example Output**:
```
Active WorkStream: Add Redis caching
  ✓ intake (complete)
  ✓ investigation (complete)
  → planning (current - 3/6 phases complete)
  - execution
  - review
  - delivery

Paused: Fix auth bug (investigation - 1/6 phases complete)
```

**When LLM uses**:
- User asks "what am I working on?"
- User says "show me the progress"
- Before pausing to show context

---

### 2. `get_workstream_details()`

**Purpose**: Get full JSON dump of current WorkStream context

**Parameters**: None

**Example Output**:
```json
{
  "stream_id": "uuid-123",
  "state": "active",
  "created_at": 1722288000,
  "phase_plan": {
    "phases": ["intake", "investigation", "planning", "execution", "review", "delivery"],
    "current_index": 2,
    "completed": ["intake", "investigation"],
    "remaining": ["execution", "review", "delivery"]
  },
  "artifacts": {
    "intake": { "summary": "...", "requirements": [...] },
    "investigation": { "summary": "...", "evidence": [...] }
  }
}
```

**When LLM uses**:
- Need detailed artifact data
- Debugging phase progression
- Before making phase plan mutations

---

### 3. `jump_to_phase(phase: string)` ⭐ NEW

**Purpose**: Skip forward or backward to a specific phase

**Parameters**:
- `phase` (required, enum): `intake`, `investigation`, `planning`, `execution`, `review`, `delivery`

**Example Output**:
```
✓ Jumped forward from phase 'investigation' to 'execution' (skipped 1 phases). Continuing from 'execution'.
```

**When LLM uses**:
- User says "skip investigation" → `jump_to_phase(phase="planning")`
- User says "just implement it now" → `jump_to_phase(phase="execution")`
- User says "go back to requirements" → `jump_to_phase(phase="intake")`
- User says "review first" → `jump_to_phase(phase="review")`

**Implementation**:
```go
func (w *WorkStreamManagement) jumpToPhase(targetPhase string) (string, error) {
    // 1. Get STM accessor and current thread
    stm := w.stmAccessor()
    thread := stm.GetCurrentThread()
    streamCtx := thread.GetStreamContext()
    
    // 2. Get phase plan mutator
    mutator := w.mutatorAccessor()
    
    // 3. Validate target phase exists in plan
    plan := streamCtx.GetPhasePlan()
    plannedPhases := plan.GetPlannedPhases()
    // ... validation logic ...
    
    // 4. Jump to target phase
    err := mutator.JumpToPhase(targetPhase)
    
    // 5. Return success message
    return "✓ Jumped to phase 'execution'. Continuing from 'execution'.", nil
}
```

---

### 4. `pause_workstream()`

**Purpose**: Pause current WorkStream (can be resumed later)

**Parameters**: None

**Example Output**:
```
✓ WorkStream paused. You can resume it anytime with resume_workstream.
```

**When LLM uses**:
- User starts new task: "Actually, work on the payment bug first"
- User explicitly says: "Pause this"
- User switches context: "Let me work on something else"

**State Transition**: `active` → `paused`

---

### 5. `resume_workstream()`

**Purpose**: Resume a paused WorkStream

**Parameters**: None

**Example Output**:
```
✓ WorkStream resumed. Continuing from phase 'planning' (3/6 complete).
```

**When LLM uses**:
- User says "resume", "continue", "back to the previous task"
- User asks "what was I working on?" and then wants to continue

**Validation**: Only works if current state is `paused`

---

### 6. `abandon_workstream()`

**Purpose**: Permanently cancel a WorkStream (cannot be resumed)

**Parameters**: None

**Example Output**:
```
✓ WorkStream abandoned. Previous work discarded.
```

**When LLM uses**:
- User says "cancel that", "never mind", "forget about it"
- User explicitly wants to discard work

**State Transition**: Any state → `abandoned` (terminal state)

---

## Example Scenarios

### Scenario 1: Skip Investigation Phase

```
[Agent in Investigation Phase]
Agent: "Let me analyze the test failures by reading each test file..."

User: "Skip investigation, I know what's wrong. Just fix it."

LLM Analysis:
  - User wants to skip current phase
  - User wants to jump to execution
  - Decision: Call jump_to_phase

Tool Call: jump_to_phase(phase="execution")

Agent: "✓ Jumped forward from phase 'investigation' to 'execution' (skipped 1 phases). 
        What would you like me to implement?"
```

### Scenario 2: Go Back to Requirements

```
[Agent in Execution Phase]
Agent: "I've implemented the caching layer. Let me run tests..."

User: "Wait, I need to review the requirements first. Go back."

LLM Analysis:
  - User wants to revisit earlier phase
  - Decision: Jump backward to intake

Tool Call: jump_to_phase(phase="intake")

Agent: "✓ Jumped backward from phase 'execution' to 'intake' (skipped 2 phases).
        Let's review the requirements together."
```

### Scenario 3: Pause for Urgent Work
### Scenario 3: Pause for Urgent Work

```
[Agent in Planning Phase for "Add Redis caching"]
Agent: "Here's my implementation plan: 1. Create cache service..."

User: "Actually, pause that. There's a critical payment bug I need to fix first."

LLM Analysis:
  - User wants to switch tasks
  - Current work should be paused, not abandoned
  - Decision: Pause current stream

Tool Call: pause_workstream()

Agent: "✓ WorkStream paused. The Redis caching work is saved.
        Let's focus on the payment bug. What's the issue?"

[Later, after fixing payment bug]
User: "Okay, back to the Redis work."

LLM Analysis:
  - User wants to resume paused work
  - Decision: Resume previous stream

Tool Call: resume_workstream()

Agent: "✓ WorkStream resumed. Continuing from phase 'planning' (2/6 complete).
        We were working on the Redis caching implementation plan..."
```

### Scenario 4: Direct Execution (Skip Planning)

```
User: "Add a config flag to disable rate limiting"

[Agent starts intake phase]
Agent: "Let me gather requirements..."

User: "No need, just add 'DISABLE_RATE_LIMIT=true' to the config"

LLM Analysis:
  - User provided exact implementation
  - No investigation/planning needed
  - Decision: Jump directly to execution

Tool Call: jump_to_phase(phase="execution")

Agent: "✓ Jumped forward from phase 'intake' to 'execution' (skipped 2 phases).
        Adding DISABLE_RATE_LIMIT flag to config..."
```

---

## Design Rationale

### Why Tool-Based Instead of Automatic?

**Original Design** (Rejected):
- Agent code detects interjections via `detectInterjection()`
- Agent code calls `handleInterjection()` automatically
- LLM is informed of decisions after the fact

**Problems**:
1. ❌ Violates **LLM-as-Orchestrator** pattern
2. ❌ Agent code making orchestration decisions
3. ❌ Removes LLM's control over workflow
4. ❌ No way for LLM to reason about interjections
5. ❌ Hard to debug (black box decisions)

**Current Design** (Implemented):
- Agent code provides tools (`pause_workstream`, `jump_to_phase`, etc.)
- LLM sees state in context
- LLM decides when to call tools
- Agent code executes tool (participant, not controller)

**Benefits**:
1. ✅ LLM has full orchestration control
2. ✅ LLM reasoning is visible in tool calls
3. ✅ User can see why decisions were made
4. ✅ Simpler agent code (no detection logic)
5. ✅ Easier to debug (tool calls are logged)

### Why Atomic Tools (No Parameters)?

**Original Design**: Tools with parameters
```go
pause_workstream(reason: string, confirm: bool)
resume_workstream(stream_id: string, phase: string)
```

**Problems**:
1. ❌ Slower LLM response (must generate parameters)
2. ❌ Parameter overhead (reason, confirm) unnecessary
3. ❌ Conversation context already shows "why"
4. ❌ More tokens = more cost

**Current Design**: Atomic tools (minimal parameters)
```go
pause_workstream()        // No parameters - calling IS the decision
resume_workstream()       // No parameters - operates on current stream
jump_to_phase(phase)      // Only essential parameter
```

**Benefits**:
1. ✅ Faster LLM response (fewer tokens to generate)
2. ✅ Lower cost (fewer tokens)
3. ✅ Simpler tool definitions
4. ✅ Calling the tool IS the decision
5. ✅ Context shows why (no need for reason parameter)

**Exception**: `jump_to_phase(phase)` needs the target phase as a parameter since there are multiple valid targets. All other tools operate on implicit context (current WorkStream).

---

## Implementation Status

### ✅ Completed

1. **Priority Queue** (`workqueue.go`)
   - Priority constants: `PriorityUser=100`, `PrioritySystemResume=50`, `PriorityPhaseSuccession=10`
   - Priority-based insertion in `Enqueue()`
   - FIFO within same priority

2. **V2 Ask Pipeline** (`copilot_v2.go`)
   - `AskV2()` processes work queue with priority ordering
   - User queries always dequeued first (`PriorityUser`)
   - No automatic interjection handling (removed `InterjectionContext`, `detectInterjection()`, `handleInterjection()`)

3. **WorkStream Management Capability** (`capabilities/workstream.go`)
   - 6 atomic tools exposed to LLM
   - `BuildContext()` shows WorkStream state in system prompt
   - `BuildInstructions()` guides LLM on when to use tools
   - All tools implemented and tested

4. **Phase Jump Implementation** (`workstream.go::jumpToPhase()`)
   - Validates target phase exists in plan
   - Uses `PhasePlanMutator.JumpToPhase()` to update current index
   - Returns success message with direction and skipped count
   - Supports forward and backward jumps

5. **STM Integration**
   - `STMReader` interface for accessing WorkStream context
   - `PhasePlanMutator` interface for phase plan mutations
   - Injected via `SharedInfrastructure` in `getOrCreateSharedInfra()`

### 📋 Pending

1. **WorkStreamManagement Registration**
   - Need to register capability with `CopilotAgent`
   - Wire `stmAccessor` and `mutatorAccessor` into constructor
   - Ensure tools are exposed in V2 pipeline

2. **End-to-End Testing**
   - Test jump_to_phase in real scenarios
   - Verify LLM can discover and use tools
   - Test pause → jump → resume workflows

3. **Documentation Updates**
   - ✅ This document updated
   - ⏸️ Update `PHASE_SYSTEM_ARCHITECTURE.md` with jump_to_phase details
   - ⏸️ Update `WORKFLOWS.md` with LLM-orchestrated interjection handling

---

## Code Locations

### Core Implementation

- **Priority Queue**: `/lib/sop/ai/agent/workqueue.go`
  - Lines 13-18: Priority constants
  - WorkItem struct with Priority field
  - Enqueue/Dequeue with priority sorting

- **V2 Ask Pipeline**: `/lib/sop/ai/agent/copilot_v2.go`
  - Lines 65-101: `AskV2()` priority queue loop
  - Lines 108-210: `askV2Core()` pure ReAct turn
  - Lines 721+: `getOrCreateSharedInfra()` STM/PhasePlanMutator injection

- **WorkStream Capability**: `/lib/sop/ai/agent/capabilities/workstream.go`
  - Lines 1-25: Struct and constructor
  - Lines 40-60: `Classify()` - keyword detection
  - Lines 62-80: `BuildContext()` - shows WorkStream state
  - Lines 89-103: `BuildInstructions()` - LLM guidance
  - Lines 105-165: `ExposeTools()` - 6 tool definitions
  - Lines 167-200: `ExecuteTool()` - routing switch
  - Lines 202-280: Tool implementations (list, details, pause, resume, abandon)
  - Lines 282-340: `jumpToPhase()` - NEW phase jumping implementation
  - Lines 400+: Interfaces (`STMReader`, `PhasePlanMutator`)

- **Phase Base**: `/lib/sop/ai/agent/planner/phase_base.go`
  - Line 105: `GetPhasePlanMutator()` - access from base phase

### Related Documentation

- `/docs/ARCHITECTURE_PATTERNS.md` - Participant Pattern details
- `/docs/PLANNING_FIRST_LLM_ORCHESTRATION.md` - Phase succession design
- `/docs/WORKFLOWS.md` - Workflow architecture evolution
- `/docs/PHASE_PLAN_IMPLEMENTATION_DESIGN.md` - PhasePlan data structures

---

## Migration Notes

### From Automatic Detection to LLM-Orchestrated

**Before** (Rejected Design):
```go
// Agent code made orchestration decisions
if detectInterjection(query, state) {
    handleInterjection(query, currentPhase)
    // LLM informed after the fact
}
```

**After** (Current Design):
```go
// LLM sees state and makes decisions
// Agent code provides tools
func (w *WorkStreamManagement) ExposeTools() []ToolDefinition {
    return []ToolDefinition{
        {Name: "jump_to_phase", Schema: "..."}, 
        {Name: "pause_workstream", Schema: "..."},
        // LLM decides when to call
    }
}
```

**Key Changes**:
1. Removed: `InterjectionContext`, `detectInterjection()`, `handleInterjection()`
2. Added: `WorkStreamManagement` capability with 6 tools
3. Shifted: Decision-making from agent code to LLM
4. Pattern: Controller → Participant (agent provides infrastructure, LLM orchestrates)

---

## Future Enhancements

### Phase 1: Current Implementation ✅
- Priority queue
- WorkStream management tools
- Phase jumping
- Pause/resume/abandon

### Phase 2: Planned Features 📋
- **Multi-WorkStream Support**: Handle multiple paused streams
- **WorkStream Naming**: User-friendly names for paused work
- **Stream Switching**: `switch_to_workstream(name)` tool
- **Progress Indicators**: Visual progress in list_workstreams
- **Phase Plan Customization**: `mutate_phase_plan` for insert/remove/reorder

### Phase 3: Advanced Features 💡
- **Checkpoint System**: Save/restore phase state
- **WorkStream Forking**: Branch from current phase
- **Undo/Redo**: Reverse phase jumps
- **Phase Templates**: Custom phase sequences per domain
- **WorkStream Analytics**: Time spent per phase, success metrics

```
[Agent in Planning Phase]
Agent: "Here's my plan to refactor the auth module..."

User: "Actually, let's fix the urgent bug in payments first"
↓
Interjection detected
↓
LLM Classification:
  type: "redirect"
  target_phase: "investigation"
  is_redirection: true
  reasoning: "User changed priority from refactoring to bug fix"
↓
Queue cleared of capability items
New interjection processed
↓
Agent: "Understood. Let me investigate the payments bug..."
```

### Scenario 3: Clarify Context

```
[Agent in Execution Phase]
Agent: "Applying patch to user_service.go..."

User: "By the way, make sure to update the tests too"
↓
Interjection detected
↓
LLM Classification:
  type: "clarify"
  is_clarification: true
  reasoning: "User adding requirement to include test updates"
↓
Context enriched, execution continues
↓
Agent: "Noted. I'll update the corresponding tests as well."
```

### Scenario 4: Cancel Operation

```
[Agent in Review Phase]
Agent: "Validating changes across 50 files..."

User: "Stop, I need to make some manual changes first"
↓
Interjection detected
↓
LLM Classification:
  type: "cancel"
  is_cancel: true
  reasoning: "User wants to abort current validation"
↓
Queue cleared, execution stopped
↓
Agent: "Operation cancelled. Let me know when you're ready to continue."
```

### Scenario 5: Status Question

```
[Agent in Investigation Phase]
Agent: [Running long analysis...]

User: "What's taking so long?"
↓
Interjection detected
↓
LLM Classification:
  type: "question"
  reasoning: "User asking for status update"
↓
Immediate response
↓
Agent: "I'm analyzing 15 test failures to identify patterns. Found 3 distinct root causes so far..."
```

---

## Implementation Plan

### Phase 1: Core Infrastructure (2-3 days)

**Files to create**:
- `lib/sop/ai/agent/interjection.go` - InterjectionManager
- `lib/sop/ai/agent/interjection_classifier.go` - LLM classification
- `lib/sop/ai/agent/copilot_interjection.go` - Intent handling

**Files to modify**:
- `lib/sop/ai/agent/workqueue.go` - Add Source, IsInterjection fields
- `lib/sop/ai/agent/copilot.go` - Enhance Ask loop

**Tasks**:
1. ✅ Add WorkItemSource enum
2. ✅ Add Priority constants (PriorityUserInterjection, etc.)
3. ✅ Create InterjectionManager with SetActive/SetInactive
4. ✅ Create LLMInterjectionClassifier with ClassifyInterjection
5. ✅ Add handleInterjectionIntent to CopilotAgent

### Phase 2: Queue Enhancement (1 day)

**Modifications**:
```go
// workqueue.go additions
type WorkItem struct {
    // ... existing fields
    Source         WorkItemSource
    IsInterjection bool
    Timestamp      time.Time
}

// New method to clear capability items (for redirections)
func (q *WorkQueue) ClearCapabilityItems() {
    q.mu.Lock()
    defer q.mu.Unlock()
    
    var userItems []WorkItem
    for _, item := range q.items {
        if item.Source == SourceUserInitial || item.Source == SourceUserInterjection {
            userItems = append(userItems, item)
        }
    }
    q.items = userItems
}
```

### Phase 3: HTTP Integration (1-2 days)

**Modify**: `lib/sop/cmd/sop-daemon/rag.go`

**Tasks**:
1. ✅ Track active Ask sessions (session ID → active Ask state)
2. ✅ Detect interjections in handleAsk
3. ✅ Call InterjectionManager.HandleUserInterjection
4. ✅ Return immediate response for interjections

### Phase 4: Testing (2 days)

**Test files to create**:
- `lib/sop/ai/agent/interjection_test.go`
- `lib/sop/ai/agent/interjection_classifier_test.go`
- `lib/sop/ai/agent/copilot_interjection_test.go`

**Test scenarios**:
1. ✅ Interjection enqueued with high priority
2. ✅ LLM classification for each intent type
3. ✅ Queue reprioritization
4. ✅ Capability items cleared on redirect
5. ✅ Cancel stops execution
6. ✅ Multiple interjections handled
7. ✅ HTTP integration end-to-end

---

## Total Implementation: 6-8 Days

**Breakdown**:
- Phase 1 (Core): 2-3 days
- Phase 2 (Queue): 1 day
- Phase 3 (HTTP): 1-2 days
- Phase 4 (Testing): 2 days

---

## Key Benefits

### 1. **Responsive UX**
- User can interrupt at any time
- No need to wait for long operations
- Immediate feedback on interjection

### 2. **Intelligent Routing**
- LLM understands user intent
- Automatic phase correction
- Context-aware responses

### 3. **Queue-Based Architecture**
- Single execution model (queue)
- No race conditions
- Predictable prioritization

### 4. **Minimal Code Changes**
- Leverages existing WorkQueue
- Extends, doesn't replace
- Backward compatible

---

## Related to Simple Adaptive Phase Routing

**This complements the adaptive routing system**:

1. **Heuristics** → Initial phase selection ✅
2. **User rejection at turn boundary** → Adaptive classification ✅
3. **User interjection DURING turn** → This design ⭐ NEW
4. **Natural handoff** → Phase transitions ✅

**All four mechanisms work together**:
```
Heuristics (fast, initial)
    ↓
User rejects at turn end → Adaptive LLM classification
    ↓
User interjects mid-turn → Interjection LLM classification (this design)
    ↓
Phase completes → Natural handoff
```

---

## LLM Intervention Opportunities Summary

**3 LLM Classification Points**:

1. **Adaptive Phase Classification** (turn boundary)
   - When: User rejects phase at end of turn
   - Input: User query, current phase, conversation history
   - Output: Correct target phase
   - Frequency: 5-10% of turns

2. **User Interjection Classification** (mid-turn) ⭐ **NEW**
   - When: User sends message during Ask execution
   - Input: User query, current phase, current action, conversation history
   - Output: InterjectionIntent (reject/redirect/clarify/cancel/continue/question)
   - Frequency: 2-5% of asks

3. **Frustration Detection** (optional, future)
   - When: Repeated failures or user dissatisfaction
   - Input: Query similarity, outcome patterns, sentiment
   - Output: Escalation decision
   - Frequency: <1% of asks

**Combined**: LLM invoked intelligently, not every turn, maximum responsiveness.

---

## Next Steps

1. **Review this design** - Confirm approach before implementation
2. **Phase 1 implementation** - Core interjection infrastructure
3. **Integration with adaptive routing** - Ensure both work together
4. **Testing with real scenarios** - Validate UX improvements

**Ready to start Phase 1 when approved!** 🚀
