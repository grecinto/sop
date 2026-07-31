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

### Solution: PhasePlanMutator Interface

```go
type PhasePlanMutator interface {
    InsertPhase(ctx, phaseID, position int) error  // Add phase at index
    AppendPhase(ctx, phaseID) error                 // Add to end
    RemovePhase(ctx, phaseID) error                 // Delete phase
    JumpToPhase(ctx, phaseID) error                 // Skip to phase immediately
    GetCurrentPlan(ctx) ([]string, int, error)      // Show plan
    SetCheckpoint(ctx, phaseID) error               // Mark validation point
}
```

### Exposed as LLM Tool

```json
{
  "name": "mutate_phase_plan",
  "description": "Dynamically modify the phase plan during conversation",
  "parameters": {
    "action": "insert|append|remove|jump|checkpoint|show",
    "phase_id": "intake|investigation|planning|execution|review|delivery",
    "position": 0
  }
}
```

### Implementation: STMPhasePlanMutator

Uses STM (Short-Term Memory) as source of truth:
- Reads current plan from workstream state
- Applies mutations
- Writes back to STM
- Enqueues next phase as WorkItem

**Key Advantage**: State survives across turns, enabling pause/resume.

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

### Flow Diagram

```
User Interjection: "Actually, skip investigation"
         ↓
Ask Pipeline receives query
         ↓
LLM Context:
  - User input: "Actually, skip investigation"
  - Current phase: "investigation"  
  - Current plan: ["investigation", "planning", "execution", "review"]
  - Available tools: [..., mutate_phase_plan, get_current_plan, ...]
         ↓
LLM Natural Reasoning:
  "User wants to skip current phase and jump to planning"
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
