# Workflow Architecture

This document covers the technical architecture of AI Studio's intelligent workflow system, including the Dynamic Planner, Knowledge Base workflows, and the Ask pipeline execution model.

## Overview

AI Studio provides two complementary workflow approaches that both execute through a unified Ask pipeline:

1. **Dynamic Planner** - AI-generated workflows using task analysis and LLM reasoning
2. **Knowledge Base Workflows** - Pre-defined templates stored in knowledge bases

Both approaches implement a **participant pattern** where workflows are passive responders and the Ask pipeline maintains control of execution flow.

## The Ask Pipeline: Core Architecture

The Ask pipeline (`CopilotAgent.ask()`) is the central execution engine for all AI interactions in AI Studio. It provides:

### Pipeline Stages

1. **Routing & Classification**
   - Three-gate routing: prefix, continuation, cold-start
   - Task classification for workflow selection
   - Capability hint detection

2. **Capability Selection**
   - Registry lookup and capability matching via `CanHandle()`
   - Participant hook invocation for Ask continuity
   - Context building via `BuildContext()` and `BuildInstructions()`

3. **System Prompt Construction**
   - Assembles LLM context from capabilities
   - Includes memory (STM + LTM), tools, and instructions
   - KB search integration for enriched context

4. **Reasoning Engine Delegation**
   - Provider-specific ReAct loops (Gemini, Claude, GPT-4)
   - Tool call execution through capability handlers
   - Multi-turn conversation continuity

5. **Post-Execution Hooks**
   - `OnAfterTurn()` for step lifecycle management
   - `OnFinish()` for workflow setup (WorkflowPlanner interface)
   - Epilogue and cleanup

### Why This Matters

The Ask pipeline is "our moat" because it provides:

✅ **Battle-tested provider reasoning** - Years of refinement in multi-turn conversations
✅ **Memory integration** - Automatic STM/LTM access for every capability
✅ **Tool framework** - Unified tool registry with authorization and observability
✅ **Conversation continuity** - Provider maintains context across turns
✅ **Authorization gates** - Entitlement checks built into the pipeline
✅ **Observable execution** - Metrics, logs, and tracing at every step

By executing workflows *through* the Ask pipeline rather than *bypassing* it, every workflow step inherits these capabilities.

## Dynamic Planner Architecture

### Component Overview

The Dynamic Planner (`PlannerCapability`) generates custom workflows using task analysis and converts them to executable steps.

**Key Components:**

```go
type PlannerCapability struct {
    orchestrator     *Orchestrator
    workflowManager  *WorkflowManager
}

// Core interfaces
type Capability interface {
    ID() string
    Name() string
    CanHandle(ctx, query, state) bool
    Run(ctx, query, state) (CapabilityResult, error)
    // ... other methods
}

type WorkflowPlanner interface {
    Capability
    OnFinish(ctx context.Context, result CapabilityResult) error
}
```

### Execution Flow

1. **Task Classification**
   ```
   User query → TaskClassifier.Classify()
     → Determines: debugging, implementation, testing, refactoring, etc.
   ```

2. **Plan Generation**
   ```
   PlannerCapability.Run()
     → generatePlan() creates WorkflowPlan
     → Fallback heuristics or LLM-based planning
     → Returns CapabilityResult with plan in Data map
   ```

3. **OnFinish Hook (Post-Execution)**
   ```
   Ask pipeline calls OnFinish() after Run() completes
     → Extract WorkflowPlan from result.Data
     → Convert to Steps via planToSteps()
     → Store steps in result.Data["workflow_steps_ready"]
     → Mark result.Data["workflow_trigger_needed"] = true
   ```

4. **Workflow Trigger Enqueue**
   ```
   Ask pipeline detects workflow_trigger_needed
     → Creates WorkItem with actor="workflow_manager"
     → Enqueues to WorkQueue
     → Next Ask iteration processes trigger
   ```

5. **Step-by-Step Execution**
   ```
   Each workflow step → Full Ask pipeline iteration
     → Capability routing → Run() → ReAct loop → Tool calls
     → Provider maintains conversation context
     → Memory integration throughout
   ```

### Step Conversion

The `planToSteps()` function converts abstract workflow phases to concrete Step implementations:

```go
// Mapping from plan phase types to Step constructors
"analyze"    → NewAnalyzeCodeStep(description)
"generate"   → NewGeneratePatchStep(description)
"apply"      → NewApplyFixStep(nil)
"verify"     → NewVerifyFixStep(workingDir)
"research"   → NewAnalyzeCodeStep(description)
"design"     → NewDesignStep(description)
```

These Step implementations are from the coding workflow public API and provide:
- Lifecycle hooks: `OnBeforeExecute`, `OnAfterExecute`, `OnAfterTurn`, `OnAfterToolResult`
- Shared infrastructure access: file system, test runners, code analysis
- Retry logic and error handling
- Signal processing for user feedback

## Knowledge Base Workflows

### Architecture

KB workflows are pre-defined templates stored in knowledge bases (Core Playbook or Spaces):

```go
type WorkflowDefinition struct {
    ID              string
    Name            string
    CategoryPath    string   // e.g., "Debugging/Authentication"
    Description     string
    Steps           []WorkflowStep
    TriggerPatterns []string
    Metadata        map[string]any
}

type WorkflowStep struct {
    Step               int
    Title              string
    Actor              string
    Priority           int
    QueryTemplate      string  // "{original_query}" expansion
    ToolsExpected      []string
    Instructions       string
    RequiredContext    map[string]any
    SuccessCriteria    []string
    Condition          string  // e.g., "tests_passed", "code_changed"
}
```

### Workflow Manager

The `WorkflowManager` handles KB workflow discovery and execution:

```go
type WorkflowManager struct {
    kb                  *memory.KnowledgeBase
    selectedKBs         []*memory.KnowledgeBase
    conditionRegistry   map[string]ConditionFunc
    scriptExecutor      *ScriptExecutor
    workflowCache       *WorkflowCache
}

// Participant pattern method - enqueues one step at a time
func (wm *WorkflowManager) EnqueueNextStep(
    ctx context.Context,
    queue *WorkQueue,
    workflow *WorkflowDefinition,
    stepIndex int,
    originalQuery string,
) error {
    // Expands template, builds condition, creates WorkItem
    // Enqueues single step - Ask pipeline calls back for next
}
```

### Discovery

KB workflows are matched via:

1. **Semantic search** - Query vector matches workflow description
2. **Trigger patterns** - Regex patterns in workflow metadata
3. **Category path** - Hierarchical organization (e.g., "Testing/Go")

Example:
```
Query: "debug authentication timeout"
  → Semantic search in Core Playbook
  → Finds: "Authentication/Debugging/Session Timeout"
  → Returns WorkflowDefinition
```

## Participant vs Controller Pattern

### ❌ Controller Pattern (Anti-Pattern)

In a controller pattern, the capability controls execution flow:

```
Capability.Orchestrate() {
    for each step {
        queue.Enqueue(step)  // Enqueues ALL steps upfront
    }
}
→ Capability decides when each step runs
→ Bypasses Ask pipeline for step execution
→ Loses provider continuity and memory integration
```

**Problems:**
- Separate execution engine needed
- No provider conversation context between steps
- Memory and tool access must be reimplemented
- Defeats the purpose of the Ask pipeline

### ✅ Participant Pattern (Correct)

In a participant pattern, the Ask pipeline controls flow and capabilities respond:

```
Capability.Run() {
    // Generate plan/result
    return CapabilityResult
}

Capability.OnFinish(result) {
    // Set up workflow trigger
    result.Data["workflow_steps_ready"] = steps
    result.Data["workflow_trigger_needed"] = true
}

Ask Pipeline {
    → Calls Run()
    → Calls OnFinish() after reasoning completes
    → Detects workflow_trigger_needed
    → Enqueues trigger to queue
    → Processes queue naturally
    → Each step goes through full Ask iteration
}
```

**Benefits:**
- 100% Ask pipeline reuse
- Full provider reasoning for every step
- Memory integration automatic
- Tool framework access built-in
- Authorization and observability included

## WorkQueue Architecture

The `WorkQueue` enables multi-turn orchestration:

```go
type WorkQueue struct {
    items      []WorkItem
    maxDepth   int
    maxPerActor map[string]int
    actorCount map[string]int
}

type WorkItem struct {
    Query        string
    Actor        string
    Priority     int
    Context      map[string]any
    ParentItemID string
    TurnNumber   int
    ID           string
    Condition    func(TurnResult) bool
}

type TurnResult struct {
    TurnNumber  int
    Actor       string
    Response    string
    ToolCalls   []any
    Error       error
    Data        map[string]any
}
```

### Context Injection

WorkQueue is injected into the Ask pipeline context:

```go
// In Ask()
queue := NewWorkQueue()
ctx = WithWorkQueue(ctx, queue)

// Capabilities can access it
queue := GetWorkQueue(ctx)
if queue != nil {
    queue.Enqueue(workItem)
}
```

This allows capabilities to enqueue work without breaking encapsulation or bypassing the Ask pipeline.

## OnFinish Hook Implementation

The `OnFinish()` hook is called by the Ask pipeline after capability execution completes:

```go
// In CopilotAgent.ask() - after delegateToReasoningEngine()
if selectedCap, ok := ctx.Value(ctxKeySelectedCapability).(capabilities.Capability); ok {
    if planner, ok := selectedCap.(capabilities.WorkflowPlanner); ok {
        if capResult, ok := ctx.Value(ctxKeyCapabilityResult).(capabilities.CapabilityResult); ok {
            if err := planner.OnFinish(ctx, capResult); err != nil {
                log.Warn("WorkflowPlanner OnFinish failed", "error", err)
            }
        }
    }
}
```

### Why OnFinish Instead of Orchestrate

**OnFinish advantages:**
1. Called *after* reasoning completes - plan is fully formed
2. Doesn't control execution - just prepares next step
3. Maintains participant pattern - Ask pipeline remains controller
4. No import cycles - clean separation of concerns
5. Capability result is available - can use LLM output

**Old Orchestrate problems:**
1. Called *during* capability selection - premature
2. Controlled entire workflow - violated participant pattern
3. Required accessing work queue directly - tight coupling
4. Created import cycles between packages
5. Bypassed Ask pipeline benefits

## Tool Execution Chain

When workflow steps use tools:

```
1. Step instructions mention tool (e.g., "use read_file")
2. Provider ReAct loop decides to call tool
3. Provider calls req.Executor.Execute(toolName, args)
4. CopilotAgent.Execute() dispatches to registry
5. registry.Get(toolName) returns toolDef
6. toolDef.Handler() executes (may call capability.Run())
7. Result returns to provider ReAct loop
8. Provider continues reasoning with result
```

This ensures tools are called *by the provider* based on LLM reasoning, not hard-coded in capability logic.

## Example: Dynamic Planner Flow

Complete end-to-end flow for AI-generated workflow:

```
1. User: "Debug authentication timeout"

2. Ask pipeline routing
   → TaskContext created
   → No capability hint, evaluates CanHandle() for all
   → PlannerCapability.CanHandle() returns false (opt-in via hint)
   
3. User: "Use planner to debug authentication timeout"

4. Ask pipeline routing with hint
   → TaskContext.CapabilityHint = "planner"
   → PlannerCapability.CanHandle() returns true
   → ctx = WithValue(ctx, ctxKeySelectedCapability, plannerCap)

5. Capability execution
   → PlannerCapability.Run() called
   → generatePlan() analyzes task
   → Creates WorkflowPlan with phases: analyze, diagnose, fix, verify
   → Returns CapabilityResult with plan in Data

6. Reasoning engine delegation
   → delegateToReasoningEngine() called
   → Provider generates natural language response about plan
   → Returns finalText to user

7. OnFinish hook
   → Ask pipeline calls plannerCap.OnFinish(ctx, result)
   → Extract plan from result.Data["plan"]
   → planToSteps() converts phases to concrete Steps
   → Store steps in result.Data["workflow_steps_ready"]
   → Mark result.Data["workflow_trigger_needed"] = true
   → Returns nil

8. Ask pipeline detects trigger needed
   → Creates WorkItem with actor="workflow_manager"
   → WorkItem.Context contains workflow_steps, original_query
   → Enqueues to work queue
   → Returns response to user

9. Next Ask iteration
   → queue.Dequeue() returns workflow_manager trigger
   → New ask() call with trigger query
   → WorkflowManager detects trigger context
   → Enqueues first step to queue
   → Processes first step through full Ask iteration

10. Each subsequent step
    → Step completes → condition evaluated
    → If success, enqueue next step
    → Full Ask pipeline for each step
    → Provider maintains conversation context
```

## Code Workflow Public API

The Dynamic Planner reuses the public Step API from `lib/sop/ai/agent/codingworkflow`:

```go
// Public constructors for reusable steps
func NewAnalyzeCodeStep(description string) Step
func NewGeneratePatchStep(description string) Step
func NewApplyFixStep(editableFiles []string) Step
func NewVerifyFixStep(workingDir string) Step
func NewDesignStep(description string) Step
func NewReadTestSourceStep() Step
func NewReadImplementationStep() Step
```

These Steps provide:
- Shared infrastructure (`SharedInfrastructure`) for file access, test running
- Lifecycle hooks for extensibility
- Retry logic and error handling
- Signal processing for interruptions

By reusing these, the Dynamic Planner inherits proven workflow components without reimplementing core functionality.

## Best Practices for SDK Integration

### When to Use Dynamic Planner

- Complex, unique tasks that don't fit templates
- Investigation scenarios requiring custom analysis steps
- Tasks where optimal approach isn't predetermined
- Situations requiring LLM reasoning about workflow structure

### When to Use KB Workflows

- Standardized team processes (e.g., code review checklist)
- Compliance workflows with required steps
- Domain-specific patterns (e.g., "Deploy to Production")
- Reproducible workflows that should be consistent

### Extending the System

**Adding new Step types:**
```go
// In lib/sop/ai/agent/codingworkflow/workflow_steps.go
func NewCustomStep(params) Step {
    return &step{
        description: "...",
        execute: func(ctx, exec, infra, state) error {
            // Implementation
        },
    }
}
```

**Adding new workflow phases:**
```go
// In planner_capability.go planToSteps()
case "custom_phase":
    return NewCustomStep(phase.Description)
```

**Creating KB workflows:**
```yaml
# In knowledge base
workflow:
  id: custom_workflow_id
  name: My Custom Workflow
  category_path: MyTeam/CustomProcess
  trigger_patterns:
    - "follow team process for.*"
  steps:
    - step: 1
      title: Analyze Requirements
      actor: analyst
      query_template: "Analyze requirements for: {original_query}"
      tools_expected: ["read_file", "search_space"]
```

## Performance Considerations

### Queue Depth Limits

```go
queue.SetMaxPerActor("workflow_manager", 10)  // Prevent infinite loops
queue.SetMaxDepth(50)                          // Global limit
```

### Capability Result Caching

Store large data in separate storage:
```go
result.Data["plan_summary"] = "Short description"
result.Data["full_plan_path"] = "/tmp/plan_12345.json"
```

### Memory Footprint

- Each WorkItem: ~1-2 KB
- Plan with 5 steps: ~5-10 KB
- TurnResult history: ~10-50 KB per workflow

Total memory impact is minimal for typical workflows (<100 KB).

## Testing and Observability

### Logging

The system provides structured logging at key points:

```go
log.Info("Workflow trigger enqueued", 
    "workflow", plan.Name,
    "steps", len(steps),
    "queue_depth", queue.Depth())

log.Warn("WorkflowPlanner OnFinish failed",
    "capability", cap.ID(),
    "error", err)
```

### Metrics

Track workflow execution:
- Workflow start/completion times
- Step success/failure rates
- Queue depth over time
- Capability selection distribution

### Testing

Unit test capabilities independently:
```go
func TestPlannerCapability(t *testing.T) {
    cap := NewPlannerCapability(orchestrator, wfManager)
    result, err := cap.Run(ctx, "debug issue", state)
    assert.NoError(t, err)
    assert.NotNil(t, result.Data["plan"])
}
```

Integration test through Ask pipeline:
```go
func TestWorkflowExecution(t *testing.T) {
    agent := NewCopilotAgent(...)
    response, err := agent.Ask(ctx, "use planner to fix bug")
    // Verify workflow triggered and executed
}
```

## Summary

The AI Studio workflow architecture provides:

1. **Two workflow approaches** - Dynamic AI generation + KB templates
2. **Unified execution** - Both use Ask pipeline for 100% consistency
3. **Participant pattern** - Capabilities respond, Ask pipeline controls
4. **OnFinish hook** - Clean post-execution workflow setup
5. **WorkQueue orchestration** - Multi-turn coordination without tight coupling
6. **Reusable components** - Public Step API for workflow construction
7. **Battle-tested foundation** - Provider reasoning, memory, tools built-in

This architecture ensures every workflow benefits from years of refinement in the Ask pipeline while maintaining clean separation of concerns and extensibility.
