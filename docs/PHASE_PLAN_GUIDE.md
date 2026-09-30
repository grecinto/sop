# Phase Plan System - Complete Guide

**Last Updated**: 2026-08-09  
**Status**: Phase planning is evolving toward layered, per-phase mini-planning  
**Architecture**: Planning-First LLM Orchestration with layered planner artifacts and participant pattern

---

## Table of Contents

1. [Overview](#overview)
2. [Core Architecture Patterns](#core-architecture-patterns)
3. [Planning-First Design](#planning-first-design)
4. [Quick Start Guide](#quick-start-guide)
5. [Implementation Status](#implementation-status)
6. [Reference & Examples](#reference--examples)

---

## Overview

### What Is the Phase Plan System?

### Design-oriented planning guidance

The system is being strengthened so that design-oriented requests (feature design, architecture design, API/module design) are handled with a more explicit architecture mindset. In practice, that means planning should:

- require a short design brief before implementation is proposed
- surface constraints, non-goals, and assumptions
- discuss trade-offs and likely risks
- consult Team Practice from the Core Playbook when relevant
- use the appropriate domain toolset for the task

A working plan for this hardening effort is documented in [docs/ARCHITECTURE_DESIGN_PROMPT_PLAN.md](ARCHITECTURE_DESIGN_PROMPT_PLAN.md).

The Phase Plan system is a cognitive pattern for multi-step problem-solving where an AI agent automatically plans and executes a sequence of phases (investigation, planning, execution, review, delivery) to complete complex tasks.

### Key Concepts

**Phase**: A distinct stage of work with specific tools and purpose
- **Intake**: Requirements gathering and clarification
- **Investigation**: Code analysis, bug hunting, context gathering
- **Planning**: Strategy design, execution plan creation
- **Execution**: Implementation, code changes, test running
- **Review**: Validation, testing, quality checks
- **Delivery**: Summarization, documentation, handoff

**PhasePlan**: A top-level planned sequence of phases with checkpoints for reassessment

**Phase Mini-Plan**: A per-phase planning artifact that turns a phase objective into implementation guidance. It can include architecture/design interpretation, functional feature breakdowns, pseudo-logic trees, file-level implementation tasks, and validation guidance.

**Participant Pattern**: Architecture where capabilities are passive participants, not controllers

**WorkStream**: A session of related work that persists across interruptions

---

## Core Architecture Patterns

### The Golden Rule: Ask Pipeline is the ONLY Controller

The Copilot agent uses the **Ask Domain** architecture where:

- ✅ **Ask pipeline** (`copilot.go`'s `Ask()` function) is the single, authoritative execution engine
- ✅ **Capabilities** are passive participants that respond to requests
- ❌ **NO controllers** - capabilities never drive execution flow
- ❌ **NO loops** - capabilities don't execute multiple steps themselves

### The Participant Pattern

Capabilities participate in the Ask pipeline but never control it. They:

1. **Respond** to capability selection (via `Classify()`)
2. **Provide context** to the LLM (via `BuildContext()`)
3. **Expose tools** for the LLM to use (via `ExposeTools()`)
4. **Execute tools** when LLM calls them (via `ExecuteTool()`)
5. **Enqueue work** for future Ask turns (via `WorkQueue`)
6. **Never loop** or control execution flow

### Correct Flow

```
User Query
    ↓
Ask Pipeline (Controller)
    ├─ Routes to Capability
    ├─ Gets context from Capability
    ├─ LLM reasons with Capability's tools
    ├─ LLM calls tools via ExecuteTool()
    └─ Capability returns result
    ↓
Ask Pipeline detects work items in queue
    ↓
Next Ask Turn processes work item
    (Repeat - each turn is independent)
```

### ❌ Anti-Pattern: Controller Pattern

**NEVER** implement these patterns in capabilities:

```go
// ❌ WRONG - This is a controller!
func (c *MyCapability) Execute(ctx context.Context, steps []Step) error {
    for _, step := range steps {  // ← LOOP controlling execution
        result := c.executeStep(step)
        if result.Failed {
            return c.retry(step)  // ← Retry logic
        }
    }
    return nil
}

// ❌ WRONG - This drives execution!
func executePhasePlan(phases []string) error {
    for i, phase := range phases {  // ← Controller loop
        executePhase(phase)
        if isCheckpoint(phase) {
            reassess()
        }
        advanceToNext()
    }
    return nil
}
```

### ✅ Correct Pattern: Enqueue Next Work

Instead of looping, **enqueue ONE work item at a time**:

```go
// ✅ CORRECT - Tool enqueues next step
func (c *PhaseCapability) ExecuteTool(ctx, "phase_capture", args, state) {
    // 1. Complete current phase work
    artifact := capturePhaseArtifact(args)
    
    // 2. Determine next phase from PhasePlan
    workStream := getWorkStream(ctx)
    plan := workStream.PhasePlan
    nextPhase, hasNext := plan.AdvancePhase()
    
    // 3. Enqueue NEXT phase only (not all remaining!)
    if hasNext {
        queue := GetWorkQueue(ctx)
        queue.Enqueue(WorkItem{
            Actor: "phase:" + nextPhase,
            Query: state.Query,
        })
    }
    
    return "Phase complete. Next phase enqueued.", nil
}
```

### Parameter Passing: Explicit, Not Context

**DO NOT use `context.Context` to pass state or parameters.**

Context is for:
- ✅ Cancellation signals
- ✅ Request-scoped values (trace IDs, user identity)
- ✅ Deadlines and timeouts

Context is NOT for:
- ❌ Application state (WorkStream, PhasePlan, etc.)
- ❌ Function parameters (query, config, options)
- ❌ Dependencies (queues, databases, capabilities)

**❌ Wrong:**
```go
ctx = context.WithValue(ctx, "workstream", workStream)
processPhase(ctx)  // Hidden dependency!
```

**✅ Correct:**
```go
processPhase(ctx, workStream, plan, queue)  // Explicit parameters
```

---

## Planning-First Design

### The Universal Thinking Sequence

Every task, regardless of domain, follows this thinking sequence:
1. **Planning**: Understand what's needed, assess readiness
2. **Assessment**: Do I have enough information?
3. **Strategy**: What's my approach?
4. **Execution**: Do the work
5. **Review**: Validate results
6. **Delivery**: Communicate outcome

### Planning-First Approach

The system now uses a layered planning model:

1. **Global phase planner** decides the high-level phase sequence.
2. **Per-phase mini-planner** converts the phase goal into a task breakdown suitable for implementation.
3. **Execution handoff** uses that detailed plan to generate or guide code changes per file.
4. **Task routing classification** tags each phase and subtask with routing metadata so cheaper or faster models can be selected when appropriate.

**Every query starts at Planning Phase** (not heuristic detection):

```
User Query: "Fix failing tests"
   ↓
ALWAYS → Planning Phase (Entry Point)
   ↓
Planning Phase (LLM Orchestration):
│
├─ LLM Decision: "Do I understand the problem?"
│  ├─ NO → "Need clarification" → Jump to INTAKE
│  └─ YES → Continue assessment
│
├─ LLM Decision: "Do I have enough context?"
│  ├─ NO → "Need to investigate" → Jump to INVESTIGATION
│  └─ YES → Continue assessment
│
├─ LLM Decision: "Can I create an execution plan?"
│  ├─ YES → Create plan → Continue to EXECUTION (default sequence)
│  └─ NO → Loop back to INVESTIGATION
│
└─ Result: Phase routing decision with reasoning
```

### Phase Jump Opportunities

The same layered model also supports phase-specific replanning. If a phase reveals a better approach, the current phase mini-plan can be refined before implementation continues.

**Two main opportunities to jump phases:**

#### 1. Planning Phase (Primary Router)

Planning phase can jump to **ANY** phase:

```go
// Planning phase tool: planning_assess_and_route
{
  "target_phase": "investigation|intake|review|execution|delivery",
  "reasoning": "Why this phase is needed",
  "default_sequence": false  // true = follow normal flow
}
```

**Example decisions**:
- "Need to understand error first" → Jump to **Investigation**
- "User query unclear" → Jump to **Intake**
- "Found existing PR to review" → Jump to **Review**
- "Have all info, ready to plan" → Create plan, go to **Execution** (default)

#### 2. Checkpoint Re-assessment (Secondary Router)

At checkpoint phases, LLM can override the planned path:

```go
// After completing investigation (checkpoint)
investigation_capture({
  "status": "complete",
  "transition_to": "execution"  // Override planned "planning" phase
})
```

### Default Sequential Flow

**If no jump is needed**, follow natural progression:

```
Planning → Execution → Review → Delivery
```

Within each phase, the planner should produce a more granular breakdown such as:

```
phase objective
  → architecture/design interpretation
  → functional feature decomposition
  → pseudo-logic tree
  → implementation plan per file
  → validation and acceptance criteria
  → task classification metadata
```

Each task or phase should be tagged with structured routing fields such as:

- complexity: easy / medium / hard
- task_type: mechanical / reasoning / implementation / validation / review
- risk: low / medium / high
- expected_verification: command or check to run
- preferred_model: tool-only / cheap / balanced / strong

Routing policy:
- Deterministic mechanical tasks such as reading a file, running a known test command, grepping output, or applying a straightforward patch should prefer direct tool execution and generally bypass the LLM.
- Reasoning-heavy tasks such as architecture synthesis, debugging root cause, or making tradeoff-based implementation decisions should use a stronger model.
- If a task is ambiguous, partially tool-driven, or requires interpretation of results, a lightweight model may be used before handing off to tools.

### Model capability profiles instead of per-model difficulty labels

The preferred model should be selected from a model capability profile, not by attaching a fixed difficulty label to each model entry. A model catalog entry should describe what the model is good at and how it behaves:

- capability_tier: cheap / standard / strong
- reasoning_strength: 1-5
- max_tool_turns: integer budget
- max_output_tokens: output cap
- compact_prompt: boolean
- supported_task_types: optional list such as mechanical, reasoning, implementation

Then the router maps task metadata to the best available model profile. In practice:
- easy + mechanical → cheap or tool-only
- medium + implementation → standard
- hard + architecture / debugging / tradeoff-heavy reasoning → strong

This keeps task difficulty and model capability separate, which makes the policy easier to evolve as the catalog changes.

### Priority Rules for Phase Transitions

1. **Exit requests**: Always honored (highest priority) - `artifact.ExitRequested = true`
2. **Explicit transitions**: LLM-provided `transition_to` overrides plan (at checkpoints or user requests)
3. **Automatic progression**: Use plan's next phase if no explicit transition provided

---

## Quick Start Guide

### Core Data Structures

#### PhasePlan

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

#### WorkStreamContext

```go
type WorkStreamContext struct {
    CurrentPhase         string
    PhaseHistory         []string
    PhasePlan           *PhasePlan        // ⭐ THE PLAN - survives pause/resume
    PhaseArtifacts      map[string]any    // Artifacts from each phase
    IterationCount      int               // Total phase executions
    PhaseIterations     map[string]int    // Per-phase repeat counts
    RecentPhasePattern  []string          // Last 10 phases for loop detection
    LoopDetectionState  *LoopDetectionState
}
```

### Available Tools

#### 1. `planning_assess_and_route`

**Purpose**: Analyzes user query and recommends starting phase

**Arguments**:
```json
{
  "query": "string - user's request/question"
}
```

**Example**:
```javascript
planning_assess_and_route({
  query: "fix the failing authentication tests"
})

// Returns:
// PHASE ASSESSMENT
// Query: fix the failing authentication tests
// Detected Starting Phase: investigation
// Characteristics:
//   🔍 Bug/error fixing - requires investigation
// Recommended Action:
//   - Start in investigation phase
```

#### 2. `planning_create_succession_path`

**Purpose**: Creates a phase plan with checkpoints

**Arguments**:
```json
{
  "phases": ["investigation", "planning", "execution", "review", "delivery"],
  "checkpoints": ["after_investigation", "after_execution"],
  "reasoning": "Why this path was chosen",
  "confidence": "high|medium|low"
}
```

**Example**:
```javascript
planning_create_succession_path({
  phases: ["investigation", "planning", "execution", "review", "delivery"],
  checkpoints: ["after_investigation", "after_execution"],
  reasoning: "Bug fix requires: investigate root cause → plan fix → implement → validate",
  confidence: "high"
})

// Returns:
// ✅ PHASE PLAN CREATED
// 
// Planned Phases: investigation → planning → execution → review → delivery
// Total Phases: 5
// 
// Checkpoints:
//   ✓ after_investigation
//   ✓ after_execution
// 
// 📋 EXECUTION ROADMAP:
// 1. Investigation & Analysis 🔍 (checkpoint)
//    ↓
// 2. Planning & Design
//    ↓
// 3. Execution & Implementation 🔍 (checkpoint)
//    ↓
// 4. Review & Validation
//    ↓
// 5. Delivery & Handoff
```

#### 3. `{phase}_capture` Tools

**Purpose**: Capture phase completion and determine next phase

**Arguments**:
```json
{
  "status": "complete|in_progress|blocked|failed",
  "summary": "What was accomplished",
  "structured_data": {
    "findings": ["key", "insights"],
    "evidence": ["file:line", "references"]
  },
  "transition_to": "next_phase_name",  // Optional override
  "exit": false  // Set true to end workflow
}
```

**Example**:
```javascript
investigation_capture({
  "status": "complete",
  "summary": "Found missing import in auth.go:42",
  "structured_data": {
    "findings": ["Missing crypto/sha256 import", "Removed in refactor"],
    "affected_tests": ["TestAuthHash", "TestPasswordVerify"]
  }
})

// Returns:
// ✅ Phase Investigation & Analysis marked as COMPLETE 🔍 (CHECKPOINT PHASE)
// 
// 📊 PROGRESS: 1/5 phases complete (20%)
// 
//    ✅ Investigation & Analysis
//    ⏭️  Planning & Design
//    ⬜ Execution & Implementation
//    ⬜ Review & Validation
//    ⬜ Delivery & Handoff
// 
// → Next phase: Planning & Design
```

### User Visibility Features

Phase 5 adds rich user feedback:

**1. Plan Display Upfront**:
- Execution roadmap showing all phases
- Checkpoint markers (🔍)
- Human-readable phase names

**2. Progress Tracking**:
- X/Y phases complete with percentage
- Visual progress bar (✅ ⏭️ ⬜)

**3. Checkpoint Notifications**:
- Checkpoint phase indicator
- Override guidance at checkpoints

**4. Failure Guidance**:
- Warning display (⚠️)
- Recovery options (retry, skip, exit)

---

## Implementation Status

### ✅ Phase 1: Planning Router & Phase Plans (COMPLETE)

**Implemented**:
- ✅ Core memory structures (PhasePlan, WorkStreamContext, LoopDetectionState)
- ✅ Planning tools (`planning_assess_and_route`, `planning_create_succession_path`)
- ✅ Work stream helper functions
- ✅ 5 comprehensive tests, all passing

**Files**:
- `lib/sop/ai/agent/memory_shortterm.go`
- `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`
- `lib/sop/ai/agent/phase_succession_test.go` - Phase plan tests

### ✅ Phase 2: Phase Completion & Information Sharing (COMPLETE)

**Implemented**:
- ✅ Structured data support in phase artifacts
- ✅ Completion detection (`status="complete"`)
- ✅ Context-rich handoff prompts
- ✅ Cross-phase information sharing
- ✅ 6 comprehensive tests, all passing

**Features**:
- Phase artifacts with structured data (findings, metrics, evidence)
- Previous phase context available in handoffs
- Completion signals for phase transitions

### ✅ Phase 2.5: Work Stream Persistence (COMPLETE)

**Implemented**:
- ✅ Session state persistence to B-tree storage
- ✅ Automatic rehydration on agent startup
- ✅ 7-day TTL with cleanup during sleep cycles
- ✅ Preserves all work stream state
- ✅ 6 comprehensive tests, all passing

**Benefits**:
- Work streams survive application restart
- Manual resume feature works across sessions
- Multi-day features maintain context

### ✅ Phase 3: Phase Plan Logic (COMPLETE - Participant Pattern)

**Implemented**:
- ✅ STM access via explicit parameters (STMAccessor in SharedInfrastructure)
- ✅ Adapter classes for STMReader interfaces
- ✅ `runCreateSuccessionPath` stores PhasePlan via STM
- ✅ `runCapture` determines next phase from plan when status="complete"
- ✅ `determineNextPhaseFromPlan` reads plan, advances cursor, returns next phase
- ✅ 5 comprehensive tests, all passing

**Pattern**: Participant Pattern (NOT Controller)
- Each capture tool reads plan, advances cursor, enqueues next phase
- No loops, no controllers
- Ask pipeline controls all execution

### ✅ Phase 4: Checkpoint Logic (COMPLETE)

**Implemented**:
- ✅ Checkpoint detection (`isCheckpointPhase` method)
- ✅ Priority rules (exit > explicit > plan)
- ✅ Enhanced completion messages showing checkpoint status
- ✅ Tests for checkpoint detection and priority rules

**Priority Rules**:
1. Exit requests always honored
2. Explicit transitions override plan (at checkpoints)
3. Automatic progression uses plan as default

### ✅ Phase 5: User Visibility (COMPLETE)

**Implemented**:
- ✅ Enhanced plan creation display with execution roadmap
- ✅ Progress tracking with visual progress bar
- ✅ Checkpoint notifications with override guidance
- ✅ Failure/blocked state guidance with recovery options
- ✅ Helper functions (formatPhaseRoadmap, formatPhaseName, getProgressInfo)
- ✅ 6 comprehensive tests, all passing

**User Experience**:
- Clear visibility into phase progress
- Checkpoint indicators and override tips
- Failure recovery options
- Human-readable phase names

### ⏳ Phase 6: Integration & End-to-End Testing (PENDING)

**Planned Work**:
- Integration with Ask pipeline for automatic phase routing
- End-to-end testing with real LLM
- Comprehensive scenario testing
- Documentation updates for users

**Estimated Time**: 3-4 days

---

## Reference & Examples

### Example Flow: Bug Fix with Checkpoint Override

```
User: "Fix failing authentication tests"

1. Planning Phase:
   planning_assess_and_route({query: "Fix failing authentication tests"})
   → Recommends: investigation
   
   planning_create_succession_path({
     phases: ["investigation", "planning", "execution", "review", "delivery"],
     checkpoints: ["after_investigation", "after_execution"]
   })
   → Plan created and stored in WorkStream

2. Investigation Phase (Turn 1):
   Ask Pipeline processes WorkItem{Actor: "phase:investigation"}
   → LLM uses investigation tools
   → LLM calls investigation_capture(status="complete")
   → Tool advances cursor: 0 → 1
   → Tool enqueues WorkItem{Actor: "phase:planning"}
   
3. Planning Phase (Turn 2):
   Ask Pipeline processes WorkItem{Actor: "phase:planning"}
   → LLM creates execution plan
   → LLM calls planning_capture(status="complete")
   → Tool advances cursor: 1 → 2
   → Tool enqueues WorkItem{Actor: "phase:execution"}

4. Execution Phase (Turn 3):
   Ask Pipeline processes WorkItem{Actor: "phase:execution"}
   → LLM applies fix, runs tests
   → Tests fail!
   → LLM calls execution_capture(status="failed", transition_to="investigation")
   → CHECKPOINT: Explicit transition overrides plan
   → Tool enqueues WorkItem{Actor: "phase:investigation"}  // Back to investigation

5. Investigation Phase (Turn 4):
   Ask Pipeline processes WorkItem{Actor: "phase:investigation"}
   → LLM finds second issue
   → ... continues until all tests pass
```

### Loop Detection Example

```
Execution → Investigation → Planning → Execution (fail)
  → Investigation → Planning → Execution (fail)
  → Investigation → Planning → Execution (fail)

Loop detected: [execution, investigation, planning] repeated 3 times

Response:
⚠️  Phase Execution & Implementation status: FAILED

🔧 OPTIONS:
   1. Retry this phase after addressing the issue
   2. Skip to a different phase by setting transition_to
   3. Request exit by setting exit=true

📊 PROGRESS: 9/12 phases attempted
```

### Key Differences: Controller vs. Participant

| Aspect | ❌ Controller Pattern | ✅ Participant Pattern |
|--------|----------------------|------------------------|
| **Execution** | Capability loops through steps | Ask pipeline processes work queue |
| **Control** | Capability drives flow | Ask pipeline drives flow |
| **Steps** | All steps in one turn | Each step is separate Ask turn |
| **LLM** | Capability decides next step | LLM reasons in each turn |
| **State** | Local variables in loop | WorkStream between turns |
| **Enqueuing** | Enqueue all steps upfront | Enqueue ONE step at a time |
| **User Control** | Can't interject mid-workflow | Can interject between turns |

---

## Summary: The Golden Rules

### DO:
1. ✅ Implement the `Capability` interface
2. ✅ Use `ExecuteTool()` to handle LLM tool calls
3. ✅ Use `WorkQueue.Enqueue()` for next steps
4. ✅ Store state in `WorkStreamContext`
5. ✅ Let Ask pipeline control execution
6. ✅ Pass state as explicit parameters, not via context
7. ✅ Use PhasePlan cursor to track progress

### DON'T:
1. ❌ Create `Execute()` or `Run()` methods that loop
2. ❌ Write `for` loops that process multiple steps
3. ❌ Enqueue all steps upfront
4. ❌ Implement retry logic in capabilities
5. ❌ Control execution flow
6. ❌ Pass application state through context.Context

---

## Related Documentation

- [ARCHITECTURE_PATTERNS.md](WORKFLOWS.md) - Participant pattern details
- [PLANNING_FIRST_LLM_ORCHESTRATION.md](ASK_V2_ARCHITECTURE.md) - Full design specs
- [PHASE_PLAN_IMPLEMENTATION_DESIGN.md](./PHASE_PLAN_IMPLEMENTATION_DESIGN.md) - Phase-plan implementation reference
- [STM_WORKSTREAM_STACK_DESIGN.md](./STM_WORKSTREAM_STACK_DESIGN.md) - Work stream management
- [WORKFLOWS.md](./WORKFLOWS.md) - Complete participant pattern explanation

---

**Last Updated**: 2026-07-29  
**Status**: Phases 1-5 Complete, Phase 6 Pending (~70% complete)  
**Architecture**: Planning-First LLM Orchestration with Participant Pattern

**READ THIS DOCUMENT BEFORE IMPLEMENTING ANY PHASE-RELATED FEATURES**
