# Phase System Architecture

**Status**: ✅ Active  
**Last Updated**: 2026-07-30  
**Pattern**: LLM-as-Orchestrator with Implicit Interpretation

---

## Table of Contents

1. [Core Paradigm: LLM as Orchestrator](#core-paradigm-llm-as-orchestrator)
2. [Implicit Interpretation Design](#implicit-interpretation-design)
3. [Adaptive Phase Classification](#adaptive-phase-classification)
4. [Dynamic Phase Plan Mutation](#dynamic-phase-plan-mutation)
5. [Architecture Components](#architecture-components)
6. [How User Interjections Work](#how-user-interjections-work)
7. [Code Locations](#code-locations)

---

## Core Paradigm: LLM as Orchestrator

### The Paradigm Shift

**Previous**: Plans are automatic frameworks that execute → LLM works within rigid structure  
**New**: **LLM decides whether to plan at all** → Plans are tools LLM optionally uses

> "Phase plans and multi-step plans are **DEFAULTS for automation completion**, not requirements. The LLM being the orchestrator is absolute. Each capability is adaptive."

### What This Means

The LLM evaluates each query and decides:

```
Simple Query: "What's in this file?"
├─ LLM Decision: Execute directly (no plan needed)
├─ Action: Just read the file, return content
└─ Turns: 1

Moderate Query: "Fix the authentication bug"  
├─ LLM Decision: Create phase plan
├─ Action: investigation → planning → execution → review
└─ Turns: 4-6

Complex Query: "Refactor entire auth system across 3 services"
├─ LLM Decision: Create multi-step plan
├─ Action: Break into steps, each with phases
└─ Turns: 15-30
```

**The LLM chooses the appropriate automation level.**

---

## Implicit Interpretation Design

### Why No Explicit InterjectionInterpreter?

**Design Decision**: Simplify architecture by letting LLM interpret interjections naturally

**Key Insight**: Interpretation is inevitable, but abstraction level varies.

#### Comparison

```
Explicit (InterjectionInterpreter capability):
  User: "Skip investigation"
    ↓
  LLM: "This is an interjection, call interpret_user_intent"
    ↓
  Inner LLM: {"type": "jump_phase", "target": "planning"}
    ↓
  Outer LLM: "Ah, call mutate_phase_plan"
    ↓
  Tool: mutate_phase_plan(action="jump", phase="planning")

Implicit (Current Design):
  User: "Skip investigation"
    ↓
  LLM: "User wants to skip this phase, I'll jump to planning"
    ↓
  Tool: mutate_phase_plan(action="jump", phase="planning")
```

**Both do interpretation. The second is simpler.**

### The Guardrails (What Prevents Chaos)

Even though LLM orchestrates freely, these structures provide safety:

#### 1. PhasePlanMutator
**File**: `lib/sop/ai/agent/planner/phase_plan_mutator.go`

Provides structured mutation tools:
- `mutate_phase_plan(action, phase)` - Insert, remove, jump, clear phases
- `get_current_plan()` - Inspect plan state
- `set_checkpoint(phase)` - Mark recovery points

#### 2. Adaptive Classification
**File**: `lib/sop/ai/agent/planner/phase_classifier.go`

Learns from user corrections:
- Starts with fast heuristics
- Auto-escalates to LLM when heuristics fail
- Remembers successful routes for future queries

#### 3. Phase Capabilities
Domain-specific tools (coding, stores, spaces, desktop) expose relevant tools based on phase and domain.

#### 4. Artifact Capture
Phase execution creates structured artifacts (runCapture, runState) that survive across turns.

#### 5. STM State Management
Workstream state persists in Short-Term Memory, enabling recovery and continuation.

---

## Adaptive Phase Classification

### Problem Statement

**Challenge**: Fixed keyword-based classification fails when:
- User corrects routing ("no, I want planning phase")
- Query intent is ambiguous
- Conversational refinement changes requirements

> "Heuristics are good for 1st path, BUT once user rejects or adds improvement, heuristics will not be reliable enough. The planner has to be more adaptive. LLM stays on the driver's seat."

### Solution: Strategy Pattern with Learning

```go
type PhaseClassificationStrategy interface {
    Classify(ctx, phase, query, state, history) ClassificationResult
    Name() string
    CanEscalate() bool
}
```

#### Strategy Types

**a) HeuristicClassificationStrategy** (Default - Fast & Cheap)
- Keyword matching
- Phase detection patterns  
- Domain filtering
- **Learns from user corrections** via ClassificationHistory

**b) LLMClassificationStrategy** (Accurate but Slower)
- Sends query + phase context to LLM
- LLM evaluates: "Is this query relevant for this phase?"
- Returns: relevant, score, mode (primary/supporting), reasoning

**c) AdaptiveClassificationStrategy** (Auto-Escalating - Recommended)
- **Starts with heuristics** (fast first attempt)
- **Tracks failures** in ClassificationHistory
- **Auto-escalates to LLM** after 2+ heuristic failures for similar queries
- **Remembers successful LLM routes** for future queries

### Classification History & Learning

```go
type ClassificationHistory struct {
    // Tracks user corrections: "suggested X → user chose Y"
    UserCorrections map[string]string
    
    // Counts heuristic failures per query pattern
    FailedHeuristics map[string]int
    
    // Remembers queries where LLM succeeded
    SuccessfulLLM map[string]bool
}
```

**Learning Flow Example:**
1. User asks: "refactor the authentication code"
2. Heuristic suggests: `execution` phase (keyword: "refactor")
3. User corrects: "No, start with investigation"
4. System records: `RecordCorrection("refactor...", "execution", "investigation")`
5. Next similar query: **Heuristic immediately routes to `investigation`** (learned)

---

## Dynamic Phase Plan Mutation

### Problem

Rigid phase succession prevents:
- User interjection: "Actually, skip planning and go straight to execution"
- Dynamic discovery: "We need an extra investigation phase for database schema"
- Course correction: "Go back to planning, the requirements changed"

### Solution: WorkStream Management Tools

**Status**: ✅ Implemented (`lib/sop/ai/agent/capabilities/workstream.go`)

**Pattern**: LLM-as-Orchestrator with atomic tools (not parameter-heavy mutation tool)

#### Implemented Tools

**1. Phase Navigation**: `jump_to_phase(phase: string)`
```
Purpose: Skip forward or backward to specific phase
Parameter: phase (enum) - intake|investigation|planning|execution|review|delivery
Example: jump_to_phase(phase="execution") → Skips from investigation to execution
```

**2. State Management**: Atomic tools (no parameters)
```
pause_workstream()    → Pause current work (can resume later)
resume_workstream()   → Continue paused work from current phase
abandon_workstream()  → Permanently cancel work (cannot resume)
```

**3. Inspection**: View state (no parameters)
```
list_workstreams()         → Show active/paused streams with progress
get_workstream_details()   → Full JSON dump of WorkStream context
```

#### Why Atomic Tools (Not Mutation Tool)?

**Old Design** (rejected):
```json
{
  "name": "mutate_phase_plan",
  "parameters": {
    "action": "insert|append|remove|jump|checkpoint|show",
    "phase_id": "intake|investigation|...",
    "position": 0
  }
}
```

**Problems**:
- ❌ Too many parameters slow down LLM response
- ❌ Action parameter adds unnecessary overhead
- ❌ Combining multiple operations complicates schema

**Current Design** (implemented):
```go
// 6 separate atomic tools, each with clear purpose
jump_to_phase(phase)       // Only essential parameter
pause_workstream()         // No parameters - calling IS decision
resume_workstream()        // No parameters
abandon_workstream()       // No parameters
list_workstreams()         // No parameters
get_workstream_details()   // No parameters
```

**Benefits**:
- ✅ Faster LLM response (fewer tokens)
- ✅ Simpler tool schemas
- ✅ Tool name = intent (clear semantics)
- ✅ Lower cost (minimal parameters)

### PhasePlanMutator Interface

**File**: `lib/sop/ai/agent/capabilities/workstream.go:400+`

```go
type PhasePlanMutator interface {
    JumpToPhase(phaseID string) error         // Used by jump_to_phase tool
    InsertPhase(position int, phaseID string) error
    AppendPhase(phaseID string) error
    RemovePhase(phaseID string) error
    GetCurrentPlan() (phases []string, currentIndex int)
    SetCheckpoint(name string) error
}
```

**Access Pattern**:
```go
// In WorkStreamManagement capability
mutator := w.mutatorAccessor()  // Get from SharedInfrastructure
err := mutator.JumpToPhase(targetPhase)  // Update plan
```

**Access from Phase Capabilities** (`phase_base.go:105`):
```go
func (b *BasePhase) GetPhasePlanMutator() PhasePlanMutator {
    if b.sharedInfra != nil && b.sharedInfra.PhasePlanMutator != nil {
        if m, ok := b.sharedInfra.PhasePlanMutator.(PhasePlanMutator); ok {
            return m
        }
    }
    return nil
}
```

### Implementation: STMPhasePlanMutator

**Pattern**: Uses STM (Short-Term Memory) as source of truth
- Reads current plan from WorkStream context
- Applies mutations (JumpToPhase updates CurrentIndex)
- Writes back to STM
- State survives across turns, enabling pause/resume

**Key Advantage**: Persistent state across conversation turns

### Example: jump_to_phase Implementation

**File**: `lib/sop/ai/agent/capabilities/workstream.go:282-340`

```go
func (w *WorkStreamManagement) jumpToPhase(targetPhase string) (string, error) {
    // 1. Get STM and current WorkStream context
    stm := w.stmAccessor()
    thread := stm.GetCurrentThread()
    streamCtx := thread.GetStreamContext()
    plan := streamCtx.GetPhasePlan()
    
    // 2. Validate target phase exists in plan
    plannedPhases := plan.GetPlannedPhases()
    currentIndex := plan.GetCurrentIndex()
    
    targetIndex := -1
    for i, phase := range plannedPhases {
        if phase == targetPhase {
            targetIndex = i
            break
        }
    }
    
    if targetIndex == -1 {
        return fmt.Sprintf("Phase '%s' not found in current plan. Available: %v", 
            targetPhase, plannedPhases), nil
    }
    
    // 3. Jump using PhasePlanMutator
    mutator := w.mutatorAccessor()
    err := mutator.JumpToPhase(targetPhase)
    if err != nil {
        return "", fmt.Errorf("failed to jump to phase: %w", err)
    }
    
    // 4. Return success with context
    direction := "forward"
    if targetIndex < currentIndex {
        direction = "backward"
    }
    skipped := abs(targetIndex - currentIndex) - 1
    
    return fmt.Sprintf("✓ Jumped %s from phase '%s' to '%s' (skipped %d phases). Continuing from '%s'.", 
        direction, currentPhase, targetPhase, skipped, targetPhase), nil
}
```

---

## Architecture Components

### File Structure

```
lib/sop/ai/agent/planner/
├── phase_base.go              # Common capability interface methods
├── phase_factory.go           # NewPhase(domain, phaseID, infra)
├── phase_coding.go            # CodingPhase implementation (20+ tools)
├── phase_stores.go            # StoresPhase (domain-specific)
├── phase_spaces.go            # SpacesPhase (domain-specific)
├── phase_desktop.go           # DesktopPhase (domain-specific)
├── phase_classifier.go        # Adaptive classification strategies
├── phase_plan_mutator.go      # Dynamic plan mutation
├── interjection_interpreter.go # NOT REGISTERED (reference only)
├── types.go                   # Type aliases
└── helpers.go                 # Shared utilities
```

### Component Responsibilities

**BasePhase** (embedded in all domain phases):
- Artifact management (runCapture, runState, loadExistingArtifact)
- Classification delegation to strategy
- Common Classify() implementation
- PhasePlanMutator access

**Domain Phases** (CodingPhase, StoresPhase, etc.):
- BuildContext() - Gather phase-specific context
- ExposeTools() - Declare domain+phase tools (20+ for coding)
- ExecuteTool() - Execute tools and enqueue next work

**Factory Pattern**:
```go
func NewPhase(domain, phaseID string, infra *SharedInfrastructure) Capability {
    switch domain {
    case "coding_workflow": return newCodingPhase(phaseID, infra)
    case "stores":          return newStoresPhase(phaseID, infra)
    case "spaces":          return newSpacesPhase(phaseID, infra)
    case "desktop":         return newDesktopPhase(phaseID, infra)
    }
}
```

### Domains & Phases

**Domains**: `coding_workflow`, `stores`, `spaces`, `desktop`

**Phases**: `intake`, `investigation`, `planning`, `execution`, `review`, `delivery`

**Example**: `phase:coding_workflow:investigation` provides investigation tools for coding tasks.

---

## How User Interjections Work

### Pattern: LLM-as-Orchestrator (Not Automatic Detection)

**Core Principle**: LLM sees state, LLM decides action, LLM calls tools

- ❌ No automatic interjection detection
- ❌ No InterjectionInterpreter capability
- ✅ LLM sees WorkStream state in BuildContext()
- ✅ LLM decides when to pause/resume/jump
- ✅ LLM calls atomic tools to manage WorkStreams

### Flow Diagram (Implemented)

```
User Interjection: "Actually, skip investigation"
         ↓
Ask Pipeline receives query
         ↓
LLM Context (from WorkStreamManagement.BuildContext()):
  - User input: "Actually, skip investigation"
  - Current WorkStream: Active
  - Current phase: "investigation"  
  - Phase plan: ["investigation", "planning", "execution", "review"]
  - Available tools: jump_to_phase, pause_workstream, resume_workstream, ...
         ↓
LLM Natural Reasoning:
  "User wants to skip current phase and jump to planning"
         ↓
LLM Tool Call:
  jump_to_phase(phase="planning")
         ↓
Agent executes tool:
  1. Get STM accessor
  2. Get current WorkStream context
  3. Validate "planning" exists in plan
  4. Call mutator.JumpToPhase("planning")
  5. Update CurrentIndex in PhasePlan
  6. Return success message
         ↓
Response to user:
  "✓ Jumped forward from phase 'investigation' to 'planning' (skipped 0 phases). 
   Continuing from 'planning'."
         ↓
Next turn:
  Planning phase capability becomes active
  LLM continues with planning phase tools
```

### Example Scenarios

**Scenario 1: Skip Investigation**
```
[In investigation phase]
User: "Skip investigation, I know the issue is in auth.go"

LLM sees:
  - Active WorkStream
  - Current phase: investigation
  - Planned phases: [investigation, planning, execution, review]

LLM reasons: User wants to skip to execution (knows the problem)

Tool call: jump_to_phase(phase="execution")

Result: "✓ Jumped forward from 'investigation' to 'execution' (skipped 1 phases)"
```

**Scenario 2: Go Back to Requirements**
```
[In execution phase]
User: "Wait, let me review the requirements first"

LLM sees:
  - Active WorkStream
  - Current phase: execution
  - Completed phases: [intake, investigation, planning]

LLM reasons: User wants to revisit intake phase

Tool call: jump_to_phase(phase="intake")

Result: "✓ Jumped backward from 'execution' to 'intake' (skipped 2 phases)"
```

**Scenario 3: Pause for Urgent Work**
```
[In planning phase for Feature A]
User: "Actually, there's a critical bug. Work on that first."

LLM sees:
  - Active WorkStream: Feature A (planning phase)
  - User wants to switch tasks

LLM reasons: Should pause current work, not abandon

Tool call: pause_workstream()

Result: "✓ WorkStream paused. You can resume it anytime."

[Agent starts new WorkStream for bug fix]

[Later]
User: "Back to Feature A"

LLM sees:
  - Paused WorkStream: Feature A (planning phase, 2/6 complete)
  - User wants to continue

Tool call: resume_workstream()

Result: "✓ WorkStream resumed. Continuing from phase 'planning' (2/6 complete)."
```

**Scenario 4: Direct Execution (Skip Planning)**
```
User: "Add a config flag ENABLE_DEBUG=true"

[Agent starts intake]

User: "No planning needed, just add it to config.json"

LLM sees:
  - Active WorkStream
  - Current phase: intake
  - User provided exact implementation

LLM reasons: Skip to execution (no investigation/planning needed)

Tool call: jump_to_phase(phase="execution")

Result: "✓ Jumped forward from 'intake' to 'execution' (skipped 2 phases)"
```

### Why This Design Works

**1. LLM Has Full Context**
- WorkStreamManagement.BuildContext() shows active/paused streams
- Current phase and progress visible
- Phase plan shows what phases are available

**2. LLM Makes Decisions**
- No black box automatic detection
- LLM reasoning visible in tool calls
- User can see why decisions were made

**3. Tools Are Simple**
- Atomic operations (single purpose)
- Minimal parameters (faster LLM response)
- Clear semantics (tool name = intent)

**4. Agent Code Is Clean**
- No detection logic
- No interjection classification
- Just tool execution (participant pattern)

### Comparison: Automatic vs. Tool-Based

**Automatic Detection (Rejected)**:
```go
// Agent code making decisions ❌
if detectInterjection(query) {
    intent := classifyInterjection(query)
    if intent.Type == "skip_phase" {
        jumpToPhase(intent.TargetPhase)
    }
}
// LLM informed after the fact
```

**Tool-Based Orchestration (Implemented)**:
```go
// LLM sees state ✅
BuildContext() → "Current phase: investigation, Plan: [...]"
// LLM decides ✅
"User wants to skip, I'll call jump_to_phase"
// LLM calls tool ✅
ExecuteTool("jump_to_phase", {"phase": "execution"})
// Agent executes ✅
mutator.JumpToPhase("execution")
```

---
         ↓
LLM Calls Tool:
  mutate_phase_plan(action="jump", phase="planning")
         ↓
PhasePlanMutator Executes:
  - Updates STM workstream state
  - Returns: "Jumped to planning phase"
         ↓
Ask Pipeline Processes:
  - Queue now has: WorkItem{Actor: "phase:planning", Query: original_query}
  - Next iteration continues from planning phase
```

**Key Point**: The tool call IS the interpretation signal. No explicit classifier needed.

### Common Interjection Patterns

| User Says | LLM Interprets As | Tool Called |
|-----------|------------------|-------------|
| "Stop" | User wants to abort | `mutate_phase_plan(action="clear")` |
| "Actually, skip investigation" | User wants to jump phases | `mutate_phase_plan(action="jump", phase="planning")` |
| "Start over" | User wants restart | `mutate_phase_plan(action="clear")` + new plan |
| "Hold on, fix Z first" | User wants to pause and fork | (Future: pause_and_fork tool) |
| "By the way, Z is in login.go" | User providing clarification | Update artifact, continue |
| "What are you doing?" | User asking for status | `get_current_plan()` + explain current phase |
| "Go back to planning" | User wants to revisit phase | `mutate_phase_plan(action="jump", phase="planning")` |
| "Add a validation step" | User wants to modify plan | `mutate_phase_plan(action="insert", phase="validation", position=4)` |

---

## Code Locations

### Integration Points

**1. Capability Registration** - `copilot.go::initializeCapabilities()`
```go
// Phase capabilities for all domains
for _, phaseID := range []string{"intake", "investigation", "planning", "execution", "review", "delivery"} {
    caps = append(caps, plannerpkg.NewPhase("coding_workflow", phaseID, cwfInfra))
}

// NOTE: InterjectionInterpreter NOT registered (implicit interpretation)
// Comment in code:
// "No explicit InterjectionInterpreter needed - LLM interprets naturally 
//  and calls mutation tools directly"
```

**2. Adaptive Classification Setup** - `copilot.go::Ask()`
```go
// Create adaptive classification strategy
classificationHistory := planner.NewClassificationHistory()
wrappedGenerator := planner.WrapGenerator(a.brain)
adaptiveStrategy := planner.NewAdaptiveClassificationStrategy(
    planner.NewHeuristicClassificationStrategy(),
    planner.NewLLMClassificationStrategy(wrappedGenerator),
    classificationHistory,
)

// Inject into shared infrastructure
cwfInfra.PhaseClassificationStrategy = adaptiveStrategy
cwfInfra.ClassificationHistory = classificationHistory
```

**3. Phase Plan Mutator Setup** - `copilot.go::Ask()`
```go
// Create phase plan mutator
cwfInfra.PhasePlanMutator = planner.NewSTMPhasePlanMutator(cwfInfra.STMReader)
```

**4. Classification** - `phase_base.go::Classify()`
```go
func (p *BasePhase) Classify(ctx, query, state) ClassificationResult {
    strategy := p.getClassificationStrategy()
    history := p.getClassificationHistory()
    return strategy.Classify(ctx, p, query, state, history)
}
```

**5. Tool Execution** - Domain phases (e.g., `phase_coding.go::ExecuteTool()`)
```go
func (c *CodingPhase) ExecuteTool(ctx, toolName, args, state) (string, error) {
    switch toolName {
    case "phase_capture":
        return c.handlePhaseCapture(ctx, args, state)
    case "phase_state":
        return c.handlePhaseState(ctx, args, state)
    // ... 20+ other tools ...
    }
}
```

### Key Files

| File | Lines | Purpose |
|------|-------|---------|
| `planner/phase_base.go` | ~200 | Common capability methods, artifact management |
| `planner/phase_coding.go` | ~600 | Complete coding phase implementation |
| `planner/phase_factory.go` | ~50 | Domain selection factory |
| `planner/phase_classifier.go` | ~350 | Adaptive classification with learning |
| `planner/phase_plan_mutator.go` | ~380 | Dynamic plan mutation |
| `planner/interjection_interpreter.go` | ~430 | **NOT USED** (reference only) |
| `copilot.go` | ~3500 | Main Ask pipeline, capability registration |

---

## Summary

The phase system implements **LLM-as-Orchestrator** with:

1. **Optional Planning**: LLM decides if plans are needed
2. **Implicit Interpretation**: No explicit InterjectionInterpreter, LLM calls mutation tools directly
3. **Adaptive Classification**: Learns from user corrections, auto-escalates to LLM when needed
4. **Dynamic Mutation**: Phase plans can be modified mid-execution via mutation tools
5. **Domain Agnostic**: Factory pattern supports coding_workflow, stores, spaces, desktop

**Key Principle**: The LLM is always in control. Plans, phases, and tools are optional utilities that the LLM chooses to use based on query complexity and user intent.
