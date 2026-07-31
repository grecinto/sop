# Multi-Step Workflow Design

**Created**: 2026-07-30  
**Status**: Design Phase - Prerequisites Complete  
**Purpose**: Support multi-goal queries with sequential step execution

**Prerequisites Status**:
- ✅ **Dual-PTY Support**: Daemon now supports 2 concurrent PTY processes (not 1 PTY + 1 non-PTY)
- ⏳ **Multi-Step Implementation**: Pending (design complete, ready to implement)

---

## Executive Summary

This document describes the **Multi-Step Workflow System** - an extension to the Phase Plan architecture that enables AI agents to handle complex queries with multiple distinct goals. Each goal becomes a "step" with its own Phase Plan, executed sequentially with artifact sharing.

**Important Note**: Before implementing multi-step workflows, we enhanced the daemon to support **2 concurrent PTY processes** (previously 1 PTY + 1 non-PTY). This ensures **identical command execution behavior** across all parallel threads.

**Example Use Case**:
```
User: "Create a new store with products AND generate a report on existing stores"

System Decomposes:
  Step 1: Create store
    - Phase Plan: [investigation, planning, execution, review]
    - Domain: stores
    - Goal: Create new store with products
  
  Step 2: Generate report
    - Phase Plan: [investigation, execution]
    - Domain: stores
    - Goal: Query and report on existing stores
```

**Key Benefits**:
1. **Complex Task Decomposition**: Break multi-goal queries into manageable steps
2. **Sequential Execution**: Steps execute in order, each completing before next starts
3. **Artifact Sharing**: Step 2 can access results from Step 1
4. **Progress Tracking**: User sees progress across steps and phases
5. **Domain Flexibility**: Each step can use different domain (stores, coding, spaces, etc.)

---

## Architecture Overview

### Current State (Single-Step)

```
User Query
    ↓
Planner creates PhasePlan
    ↓
Phase 1 → Phase 2 → Phase 3 → Phase 4
    ↓
Completion
```

**Data Structure**:
```go
type WorkStreamContext struct {
    PhasePlan      *PhasePlan  // Single plan
    CurrentPhase   string
    PhaseArtifacts map[string]map[string]any
}
```

### Target State (Multi-Step)

```
User Query (Multi-Goal)
    ↓
Orchestrator detects multiple goals
    ↓
Creates Multi-Step Plan
    ↓
Step 1: Goal A
    ├─ Phase 1 → Phase 2 → Phase 3
    └─ Complete → Store artifacts
    ↓
Step 2: Goal B (can access Step 1 artifacts)
    ├─ Phase 1 → Phase 2
    └─ Complete → Store artifacts
    ↓
Completion (all steps done)
```

**Enhanced Data Structure**:
```go
type WorkStreamContext struct {
    // NEW: Multi-step support
    MultiStepPlan  *MultiStepPlan  // Contains multiple steps
    CurrentStepIndex int            // Which step we're on
    
    // EXISTING: Single-step backward compatibility
    PhasePlan      *PhasePlan      // Current step's phase plan
    CurrentPhase   string
    PhaseArtifacts map[string]map[string]any
}

type MultiStepPlan struct {
    Steps            []StepDefinition  // Ordered list of steps
    CurrentStepIndex int               // Cursor: which step we're on
    CompletedSteps   []string          // Step IDs that completed
    RemainingSteps   []string          // Step IDs yet to execute
    Reasoning        string            // Why this decomposition
    TotalSteps       int               // Number of steps
    CreatedAt        int64             // Timestamp
}

type StepDefinition struct {
    StepID          string            // "step_1_create_store"
    StepNumber      int               // 1, 2, 3...
    Goal            string            // "Create new store with products"
    Domain          string            // "stores", "coding_workflow", "spaces"
    PhasePlan       *PhasePlan        // Phase sequence for THIS step
    Status          string            // "pending", "active", "complete", "failed"
    Prerequisites   []string          // Step IDs that must complete first
    Artifacts       map[string]any    // Results from this step
    StartedAt       int64             // Timestamp
    CompletedAt     int64             // Timestamp
}
```

---

## Design Principles

### 1. Backward Compatibility
**Single-step workflows continue to work unchanged**
- If no MultiStepPlan exists, use PhasePlan (current behavior)
- Orchestrator only activates for multi-goal queries
- All existing capabilities continue to work

### 2. Participant Pattern Preserved
**Orchestrator is a capability, not a controller**
- Follows same pattern as Planner capability
- Responds to Ask pipeline, doesn't control it
- Enqueues steps sequentially (one at a time)
- Never loops or drives execution flow

### 3. Sequential Execution (MVP)
**Steps execute in order for MVP**
- Step 2 doesn't start until Step 1 completes
- Future enhancement: parallel steps with dependency tracking
- Simple, predictable, easy to debug

### 4. Artifact Sharing
**Steps can access previous step results**
- Step 2's investigation can read Step 1's artifacts
- Passed via PhaseArtifacts map with step prefixes
- Example: `step_1_create_store.store_id` → `"store_123"`

### 5. Domain Per Step
**Each step can use different domain**
- Step 1: coding_workflow domain
- Step 2: stores domain
- Step 3: spaces domain
- Planner generates domain-appropriate phases for each step

---

## Core Components

### 1. Orchestrator Capability

**Purpose**: Detect multi-goal queries and decompose into steps

**Location**: `lib/sop/ai/agent/orchestrator/capability.go`

**Classification Logic**:
```go
func (c *OrchestratorCapability) Classify(query string, state RoutingState) ClassificationResult {
    // Detect multi-goal signals
    signals := []string{
        " and ", " then ", " after that",
        "first ... then", "step 1", "step 2",
        "both", "also", "additionally",
    }
    
    // Count goals using heuristics
    goalCount := countGoals(query)
    
    if goalCount >= 2 {
        return ClassificationResult{
            Relevant: true, 
            Score: 18,  // Higher than Planner (16) but lower than explicit hints (20)
            Mode: CapabilityModePrimary,
        }
    }
    
    return ClassificationResult{}
}
```

**Tools Exposed**:
- `orchestrator_decompose_steps`: Break query into N steps
- `orchestrator_step_status`: Check current step progress

**Example Tool Call**:
```json
{
  "tool": "orchestrator_decompose_steps",
  "args": {
    "original_query": "Create store AND generate report",
    "steps": [
      {
        "step_number": 1,
        "goal": "Create new store with products",
        "domain": "stores",
        "estimated_phases": ["investigation", "planning", "execution", "review"],
        "reasoning": "Need to inspect schema, design store structure, execute creation"
      },
      {
        "step_number": 2,
        "goal": "Generate report on existing stores",
        "domain": "stores",
        "estimated_phases": ["investigation", "execution"],
        "reasoning": "Query existing stores then format report"
      }
    ],
    "reasoning": "Query has two distinct goals separated by AND",
    "confidence": "high"
  }
}
```

**Execution Flow**:
```go
func (c *OrchestratorCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state RoutingState) (string, error) {
    if toolName == "orchestrator_decompose_steps" {
        // 1. Parse steps from args
        steps := parseSteps(args)
        
        // 2. Create MultiStepPlan
        multiStepPlan := &MultiStepPlan{
            Steps: steps,
            CurrentStepIndex: 0,
            Reasoning: args["reasoning"],
            TotalSteps: len(steps),
        }
        
        // 3. Store in WorkStreamContext via STM
        stm := c.sharedInfra.ShortTermMemory()
        ws := stm.GetWorkStreamContext()
        ws.MultiStepPlan = multiStepPlan
        stm.UpdateWorkStreamContext(ws)
        
        // 4. Enqueue FIRST step only (Participant Pattern)
        queue := c.sharedInfra.GetSessionQueue()
        firstStep := steps[0]
        
        queue.Enqueue(WorkItem{
            Actor: "step_executor",
            Query: fmt.Sprintf("Execute step %d: %s", firstStep.StepNumber, firstStep.Goal),
            Context: map[string]any{
                "step_id": firstStep.StepID,
                "step_number": firstStep.StepNumber,
            },
        })
        
        // 5. Return success (Ask pipeline will process enqueued work)
        return fmt.Sprintf("Created %d-step plan. Starting Step 1: %s", len(steps), firstStep.Goal), nil
    }
    
    return "", fmt.Errorf("unknown tool: %s", toolName)
}
```

### 2. Step Executor Capability

**Purpose**: Execute a single step's Phase Plan

**Location**: `lib/sop/ai/agent/orchestrator/step_executor.go`

**Classification Logic**:
```go
func (c *StepExecutorCapability) Classify(query string, state RoutingState) ClassificationResult {
    // Only activate when Actor="step_executor"
    if strings.Contains(strings.ToLower(query), "execute step") {
        return ClassificationResult{Relevant: true, Score: 20, Mode: CapabilityModePrimary}
    }
    return ClassificationResult{}
}
```

**Execution Flow**:
```go
func (c *StepExecutorCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state RoutingState) (string, error) {
    if toolName == "step_executor_begin_step" {
        // 1. Load MultiStepPlan from STM
        stm := c.sharedInfra.ShortTermMemory()
        ws := stm.GetWorkStreamContext()
        multiStepPlan := ws.MultiStepPlan
        
        if multiStepPlan == nil {
            return "", fmt.Errorf("no multi-step plan found")
        }
        
        // 2. Get current step
        currentStep := multiStepPlan.Steps[multiStepPlan.CurrentStepIndex]
        currentStep.Status = "active"
        currentStep.StartedAt = time.Now().Unix()
        
        // 3. Store step's PhasePlan in WorkStreamContext
        ws.PhasePlan = currentStep.PhasePlan
        ws.CurrentPhase = ""  // Will be set by first phase
        stm.UpdateWorkStreamContext(ws)
        
        // 4. Enqueue first phase of this step
        firstPhase := currentStep.PhasePlan.PlannedPhases[0]
        queue := c.sharedInfra.GetSessionQueue()
        
        queue.Enqueue(WorkItem{
            Actor: fmt.Sprintf("phase:%s", firstPhase),
            Query: currentStep.Goal,
            Context: map[string]any{
                "step_id": currentStep.StepID,
                "domain": currentStep.Domain,
            },
        })
        
        // 5. Return success
        return fmt.Sprintf("Started Step %d: %s (Domain: %s, Phases: %v)", 
            currentStep.StepNumber, currentStep.Goal, currentStep.Domain, 
            currentStep.PhasePlan.PlannedPhases), nil
    }
    
    return "", fmt.Errorf("unknown tool: %s", toolName)
}
```

### 3. Phase Completion Handler (Modified)

**Purpose**: Detect when all phases of a step complete, advance to next step

**Location**: Modify existing phase capabilities' completion logic

**Enhancement**:
```go
func (c *PhaseCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state RoutingState) (string, error) {
    if strings.HasSuffix(toolName, "_capture") {
        status := readPhaseString(args, "status", "in_progress")
        
        if status == "complete" {
            // Existing: Enqueue next phase in PhasePlan
            stm := c.sharedInfra.ShortTermMemory()
            ws := stm.GetWorkStreamContext()
            
            // NEW: Check if this is the last phase of a step
            if ws.MultiStepPlan != nil {
                currentStepIndex := ws.MultiStepPlan.CurrentStepIndex
                currentStep := ws.MultiStepPlan.Steps[currentStepIndex]
                
                // Is this the last phase of current step's plan?
                if ws.PhasePlan.CurrentIndex >= len(ws.PhasePlan.PlannedPhases) {
                    // Step complete! Mark it and advance
                    currentStep.Status = "complete"
                    currentStep.CompletedAt = time.Now().Unix()
                    
                    // Store step artifacts
                    currentStep.Artifacts = ws.PhaseArtifacts
                    
                    // Advance to next step
                    ws.MultiStepPlan.CurrentStepIndex++
                    ws.MultiStepPlan.CompletedSteps = append(
                        ws.MultiStepPlan.CompletedSteps, 
                        currentStep.StepID,
                    )
                    
                    // Are there more steps?
                    if ws.MultiStepPlan.CurrentStepIndex < len(ws.MultiStepPlan.Steps) {
                        // Enqueue next step
                        nextStep := ws.MultiStepPlan.Steps[ws.MultiStepPlan.CurrentStepIndex]
                        
                        queue := c.sharedInfra.GetSessionQueue()
                        queue.Enqueue(WorkItem{
                            Actor: "step_executor",
                            Query: fmt.Sprintf("Execute step %d: %s", nextStep.StepNumber, nextStep.Goal),
                            Context: map[string]any{
                                "step_id": nextStep.StepID,
                                "step_number": nextStep.StepNumber,
                            },
                        })
                        
                        return fmt.Sprintf("Step %d complete. Advancing to Step %d: %s", 
                            currentStep.StepNumber, nextStep.StepNumber, nextStep.Goal), nil
                    } else {
                        // All steps complete!
                        return fmt.Sprintf("All %d steps complete! Workflow finished.", 
                            ws.MultiStepPlan.TotalSteps), nil
                    }
                }
            }
            
            // EXISTING: Normal phase progression (no multi-step plan)
            // ... existing code to enqueue next phase ...
        }
    }
}
```

---

## Implementation Plan

### Phase 1: Core Data Structures (1 hour)

**Files**:
- `lib/sop/ai/agent/memory_shortterm.go` - Add MultiStepPlan structs
- `lib/sop/ai/agent/workqueue.go` - No changes (already supports Context field)

**Changes**:
```go
// Add to memory_shortterm.go after PhasePlan definition

// MultiStepPlan represents a workflow with multiple sequential steps.
// Each step has its own goal and Phase Plan.
type MultiStepPlan struct {
    Steps            []StepDefinition  `json:"steps"`              // Ordered list of steps
    CurrentStepIndex int               `json:"current_step_index"` // Cursor: which step we're on
    CompletedSteps   []string          `json:"completed_steps"`    // Step IDs completed
    RemainingSteps   []string          `json:"remaining_steps"`    // Step IDs yet to execute
    Reasoning        string            `json:"reasoning"`          // Why this decomposition
    TotalSteps       int               `json:"total_steps"`        // Number of steps
    CreatedAt        int64             `json:"created_at"`         // Timestamp
}

// StepDefinition represents a single step in a multi-step workflow.
type StepDefinition struct {
    StepID        string         `json:"step_id"`        // "step_1_create_store"
    StepNumber    int            `json:"step_number"`    // 1, 2, 3...
    Goal          string         `json:"goal"`           // "Create new store with products"
    Domain        string         `json:"domain"`         // "stores", "coding_workflow", "spaces"
    PhasePlan     *PhasePlan     `json:"phase_plan"`     // Phase sequence for THIS step
    Status        string         `json:"status"`         // "pending", "active", "complete", "failed"
    Prerequisites []string       `json:"prerequisites"`  // Step IDs that must complete first
    Artifacts     map[string]any `json:"artifacts"`      // Results from this step
    StartedAt     int64          `json:"started_at"`     // Timestamp
    CompletedAt   int64          `json:"completed_at"`   // Timestamp
}

// Add to WorkStreamContext struct
type WorkStreamContext struct {
    // ... existing fields ...
    
    // NEW: Multi-step workflow support
    MultiStepPlan    *MultiStepPlan `json:"multi_step_plan,omitempty"` // Multi-step plan (if applicable)
    CurrentStepIndex int            `json:"current_step_index"`        // Which step we're on
}
```

**Testing**:
```bash
cd lib/sop/ai/agent
go build ./...  # Should compile without errors
```

### Phase 2: Orchestrator Capability (2 hours)

**Files**:
- `lib/sop/ai/agent/orchestrator/capability.go` (NEW)
- `lib/sop/ai/agent/orchestrator/decomposer.go` (NEW) - Goal detection logic
- `lib/sop/ai/agent/orchestrator/capability_test.go` (NEW)

**Create Orchestrator**:
```bash
mkdir -p lib/sop/ai/agent/orchestrator
```

**capability.go**:
```go
package orchestrator

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/sharedcode/sop/ai/agent/capabilities"
    cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

const (
    OrchestratorCapabilityID   = "orchestrator"
    OrchestratorCapabilityName = "Multi-Step Orchestrator"
)

type OrchestratorCapability struct {
    sharedInfra *cw.SharedInfrastructure
}

func NewOrchestratorCapability(sharedInfra *cw.SharedInfrastructure) *OrchestratorCapability {
    return &OrchestratorCapability{sharedInfra: sharedInfra}
}

func (c *OrchestratorCapability) ID() string   { return OrchestratorCapabilityID }
func (c *OrchestratorCapability) Name() string { return OrchestratorCapabilityName }

func (c *OrchestratorCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
    // Detect multi-goal queries
    q := strings.ToLower(query)
    
    // Strong signals
    multiGoalSignals := []string{
        " and then ", " after that ", " also ", " additionally ",
        " both ", " first ", " second ", " step 1", " step 2",
    }
    
    for _, signal := range multiGoalSignals {
        if strings.Contains(q, signal) {
            return capabilities.ClassificationResult{
                Relevant: true,
                Score:    18, // Higher than Planner
                Mode:     capabilities.CapabilityModePrimary,
            }
        }
    }
    
    // Weaker signal: " and " with multiple verbs
    if strings.Contains(q, " and ") {
        verbCount := countVerbs(q)
        if verbCount >= 2 {
            return capabilities.ClassificationResult{
                Relevant: true,
                Score:    15,
                Mode:     capabilities.CapabilityModePrimary,
            }
        }
    }
    
    return capabilities.ClassificationResult{}
}

func (c *OrchestratorCapability) BuildContext(query string, state capabilities.RoutingState) string {
    return "Multi-Step Orchestrator: Decomposes complex queries with multiple goals into sequential steps. Each step gets its own Phase Plan."
}

func (c *OrchestratorCapability) ExposeTools() []capabilities.ToolDefinition {
    return []capabilities.ToolDefinition{
        {
            Name:        "orchestrator_decompose_steps",
            Description: "Break a multi-goal query into N sequential steps. Each step has a goal, domain, and phase plan.",
            Schema: `{
                "type": "object",
                "properties": {
                    "steps": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "step_number": {"type": "integer"},
                                "goal": {"type": "string"},
                                "domain": {"type": "string"},
                                "estimated_phases": {"type": "array", "items": {"type": "string"}},
                                "reasoning": {"type": "string"}
                            },
                            "required": ["step_number", "goal", "domain", "estimated_phases"]
                        }
                    },
                    "reasoning": {"type": "string"},
                    "confidence": {"type": "string"}
                },
                "required": ["steps", "reasoning"]
            }`,
        },
    }
}

func (c *OrchestratorCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
    if toolName == "orchestrator_decompose_steps" {
        return c.decomposeSteps(ctx, args)
    }
    return "", fmt.Errorf("unknown tool: %s", toolName)
}

func (c *OrchestratorCapability) decomposeSteps(ctx context.Context, args map[string]any) (string, error) {
    // Parse steps from args
    stepsRaw, ok := args["steps"].([]any)
    if !ok {
        return "", fmt.Errorf("steps must be an array")
    }
    
    steps := make([]StepDefinition, 0, len(stepsRaw))
    for _, stepRaw := range stepsRaw {
        stepMap := stepRaw.(map[string]any)
        
        step := StepDefinition{
            StepID:     fmt.Sprintf("step_%d", int(stepMap["step_number"].(float64))),
            StepNumber: int(stepMap["step_number"].(float64)),
            Goal:       stepMap["goal"].(string),
            Domain:     stepMap["domain"].(string),
            Status:     "pending",
        }
        
        // Parse estimated phases
        phasesRaw := stepMap["estimated_phases"].([]any)
        phases := make([]string, len(phasesRaw))
        for i, p := range phasesRaw {
            phases[i] = p.(string)
        }
        
        // Create PhasePlan for this step
        step.PhasePlan = &PhasePlan{
            PlannedPhases: phases,
            CurrentIndex:  0,
            Reasoning:     stepMap["reasoning"].(string),
            Confidence:    "medium",
            LastUpdated:   time.Now().Unix(),
        }
        
        steps = append(steps, step)
    }
    
    // Create MultiStepPlan
    multiStepPlan := &MultiStepPlan{
        Steps:            steps,
        CurrentStepIndex: 0,
        Reasoning:        args["reasoning"].(string),
        TotalSteps:       len(steps),
        CreatedAt:        time.Now().Unix(),
    }
    
    // Store in WorkStreamContext
    stm := c.sharedInfra.ShortTermMemory()
    ws := stm.GetWorkStreamContext()
    ws.MultiStepPlan = multiStepPlan
    stm.UpdateWorkStreamContext(ws)
    
    // Enqueue first step
    firstStep := steps[0]
    queue := c.sharedInfra.GetSessionQueue()
    
    queue.Enqueue(WorkItem{
        Actor: "step_executor",
        Query: fmt.Sprintf("Execute step %d: %s", firstStep.StepNumber, firstStep.Goal),
        Context: map[string]any{
            "step_id":     firstStep.StepID,
            "step_number": firstStep.StepNumber,
        },
    })
    
    return fmt.Sprintf("Created %d-step workflow:\n%s\n\nStarting Step 1: %s", 
        len(steps), formatStepSummary(steps), firstStep.Goal), nil
}

func formatStepSummary(steps []StepDefinition) string {
    var sb strings.Builder
    for _, step := range steps {
        sb.WriteString(fmt.Sprintf("  Step %d: %s (Domain: %s, Phases: %v)\n", 
            step.StepNumber, step.Goal, step.Domain, step.PhasePlan.PlannedPhases))
    }
    return sb.String()
}

func countVerbs(query string) int {
    // Simple heuristic: count action words
    verbs := []string{"create", "generate", "build", "make", "update", "delete", "query", "find", "list", "show", "fix", "debug", "implement", "design"}
    count := 0
    for _, verb := range verbs {
        if strings.Contains(query, verb) {
            count++
        }
    }
    return count
}
```

**Testing**:
```bash
cd lib/sop/ai/agent/orchestrator
go test ./...
```

### Phase 3: Step Executor Capability (1.5 hours)

**Files**:
- `lib/sop/ai/agent/orchestrator/step_executor.go` (NEW)
- `lib/sop/ai/agent/orchestrator/step_executor_test.go` (NEW)

**step_executor.go**: (See design above)

### Phase 4: Phase Completion Enhancement (1 hour)

**Files**:
- `lib/sop/ai/agent/planner/phase_capabilities.go` (MODIFY)

**Modify `{phase}_capture` tool execution**:
- After phase completion, check if MultiStepPlan exists
- If yes, check if all phases of current step complete
- If yes, mark step complete and enqueue next step
- If no more steps, declare workflow complete

### Phase 5: Registration & Integration (0.5 hours)

**Files**:
- `lib/sop/ai/agent/copilot.go` (MODIFY)

**Add Orchestrator registration**:
```go
// After Planner registration
caps = append(caps, orchestrator.NewOrchestratorCapability(cwfInfra))
caps = append(caps, orchestrator.NewStepExecutorCapability(cwfInfra))
```

### Phase 6: Testing & Documentation (1 hour)

**Integration Tests**:
```bash
# Test 1: Simple multi-step
# User: "Create store named 'Alpha' AND list all stores"

# Test 2: Cross-domain multi-step
# User: "Fix bug in auth.go AND add test to spaces knowledge base"

# Test 3: Complex 3-step
# User: "First create store, then add products, then generate report"
```

**Documentation**:
- Update PHASE_PLAN_GUIDE.md with multi-step examples
- Add section to WORKFLOWS.md
- Create MULTI_STEP_WORKFLOW_EXAMPLES.md

---

## Success Metrics

### Functional Requirements
- ✅ Detect multi-goal queries accurately (>90% for explicit signals)
- ✅ Decompose into N steps (2-5 steps typical)
- ✅ Execute steps sequentially without overlap
- ✅ Share artifacts between steps
- ✅ Support domain switching per step
- ✅ Maintain backward compatibility (single-step still works)

### User Experience
- ✅ Clear progress indicators ("Step 1 of 3: Creating store...")
- ✅ Step summaries before execution
- ✅ Ability to see full workflow plan upfront
- ✅ Graceful handling of step failures

### Technical Requirements
- ✅ No loops or controller patterns (Participant Pattern preserved)
- ✅ All state passed explicitly (no context.Context abuse)
- ✅ Build passes: `go build ./...`
- ✅ Tests pass: `go test ./...`
- ✅ No breaking changes to existing capabilities

---

## Example Workflows

### Example 1: Stores Domain (Same Domain, Multiple Goals)

**User Query**: 
```
Create a new store named "TechHub" with initial products, then generate a sales report for all stores.
```

**Orchestrator Decomposition**:
```json
{
  "steps": [
    {
      "step_number": 1,
      "goal": "Create new store named 'TechHub' with initial products",
      "domain": "stores",
      "estimated_phases": ["investigation", "planning", "execution", "review"],
      "reasoning": "Need to inspect schema, design store structure, create store, verify creation"
    },
    {
      "step_number": 2,
      "goal": "Generate sales report for all stores",
      "domain": "stores",
      "estimated_phases": ["investigation", "execution"],
      "reasoning": "Query all stores, aggregate data, format report"
    }
  ],
  "reasoning": "Query has two sequential goals separated by 'then'",
  "confidence": "high"
}
```

**Execution Timeline**:
```
1. Orchestrator creates 2-step plan
2. Step 1 begins
   - Investigation phase: list_stores, describe_schema
   - Planning phase: design_query for INSERT
   - Execution phase: execute_script (CREATE store)
   - Review phase: verify store exists
3. Step 1 completes → artifacts: {store_id: "store_123", store_name: "TechHub"}
4. Step 2 begins
   - Investigation phase: scan all stores, check sales data
   - Execution phase: aggregate_sales, format_report
5. Step 2 completes → workflow done
```

### Example 2: Cross-Domain Workflow

**User Query**: 
```
Fix the authentication bug in auth.go AND document the fix in our spaces knowledge base.
```

**Orchestrator Decomposition**:
```json
{
  "steps": [
    {
      "step_number": 1,
      "goal": "Fix authentication bug in auth.go",
      "domain": "coding_workflow",
      "estimated_phases": ["investigation", "planning", "execution", "review"],
      "reasoning": "Bug fix requires code analysis, patch design, implementation, testing"
    },
    {
      "step_number": 2,
      "goal": "Document fix in spaces knowledge base",
      "domain": "spaces",
      "estimated_phases": ["investigation", "execution", "review"],
      "reasoning": "Read existing docs, mint new content with fix details, verify"
    }
  ],
  "reasoning": "Two distinct goals: coding task + documentation task",
  "confidence": "high"
}
```

**Execution Timeline**:
```
1. Orchestrator creates 2-step plan (coding + spaces)
2. Step 1 (coding_workflow):
   - Investigation: inspect auth.go, search for bug
   - Planning: design patch
   - Execution: apply_patch, run_tests
   - Review: verify tests pass
3. Step 1 completes → artifacts: {patch_applied: true, fix_description: "Added null check..."}
4. Step 2 (spaces):
   - Investigation: read_config, list_items in docs space
   - Execution: mint_to_space with fix_description from Step 1 artifacts
   - Review: verify content exists
5. Step 2 completes → workflow done
```

### Example 3: Three-Step Sequential

**User Query**: 
```
First, create a store. Then add 5 products to it. Finally, generate a stock report.
```

**Orchestrator Decomposition**:
```json
{
  "steps": [
    {
      "step_number": 1,
      "goal": "Create a store",
      "domain": "stores",
      "estimated_phases": ["investigation", "planning", "execution", "review"]
    },
    {
      "step_number": 2,
      "goal": "Add 5 products to the store",
      "domain": "stores",
      "estimated_phases": ["planning", "execution", "review"],
      "prerequisites": ["step_1"]
    },
    {
      "step_number": 3,
      "goal": "Generate stock report",
      "domain": "stores",
      "estimated_phases": ["investigation", "execution"],
      "prerequisites": ["step_2"]
    }
  ],
  "reasoning": "Three sequential tasks with explicit 'first', 'then', 'finally' signals",
  "confidence": "high"
}
```

---

## Migration Path

### Phase 1: MVP (Sequential Steps Only)
- Steps execute in order (1 → 2 → 3)
- No parallel execution
- No complex dependencies
- **Target**: Support 80% of multi-goal queries

### Phase 2: Dependency Tracking (Future)
- StepDefinition.Prerequisites enables partial ordering
- Example: Step 3 depends on Step 1, but Step 2 is independent
- **Target**: Optimize execution time for independent steps

### Phase 3: Parallel Execution (Future)
- Execute independent steps in parallel
- Requires: AskParallel integration
- **Target**: Reduce latency for complex workflows

---

## Questions for Design Review

1. **Naming**: Is "Orchestrator" the right name? Alternatives: "WorkflowPlanner", "MultiStepPlanner", "GoalDecomposer"
2. **Step Limit**: Should we cap at N steps (e.g., 5)? Prevents runaway complexity
3. **Failure Handling**: If Step 2 fails, should we retry or abort entire workflow?
4. **Artifact Namespacing**: How should Step 2 access Step 1 artifacts? Via step prefixes or separate map?
5. **User Confirmation**: Should we ask user to confirm step decomposition before execution?

---

## Implementation Timeline

**Total Estimated Time**: 7 hours

1. Data Structures: 1 hour
2. Orchestrator Capability: 2 hours
3. Step Executor: 1.5 hours
4. Phase Enhancement: 1 hour
5. Registration: 0.5 hours
6. Testing & Docs: 1 hour

**Milestones**:
- Day 1: Data structures + Orchestrator (MVP)
- Day 2: Step executor + Phase enhancement
- Day 3: Integration testing + Documentation

---

## Next Steps

1. **Review Design**: User approves architecture approach
2. **Confirm Naming**: Orchestrator vs alternatives
3. **Begin Implementation**: Start with data structures
4. **Iterate**: Test with real queries, refine decomposition logic
5. **Document**: Update guides with multi-step examples
