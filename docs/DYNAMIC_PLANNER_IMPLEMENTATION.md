# Dynamic Planner Capability Implementation

## Overview
Implemented `PlannerCapability` - a capability that analyzes coding tasks and dynamically generates custom workflow plans. The capability participates in the Ask flow by enqueueing work items for each planned step.

## Implementation Details

### Files Created
- **planner_capability.go**: Main capability implementation (420 lines)
- **planner_capability_test.go**: Comprehensive test suite with 4 test cases

### Architecture

#### Participant Pattern
The PlannerCapability uses the **participant pattern** to integrate with the Ask flow:

1. **Plan Generation**: Analyzes task using `TaskClassifier` and generates a `WorkflowPlan`
2. **Work Item Enqueueing**: For each step in the plan, enqueues a work item to the work queue
3. **Non-blocking**: Returns immediately after enqueueing, letting the Ask pipeline handle execution

```go
// Each step becomes a separate Ask transaction
for i, planStep := range plan.Steps {
    workItem := map[string]interface{}{
        "query":    stepQuery,
        "actor":    fmt.Sprintf("dynamic_planner.%s", planStep.Type),
        "priority": 100 - (i * 10),
    }
    queue.Enqueue(workItem)
}
```

#### Plan Structure
Each `WorkflowPlan` contains:
- **Name**: Workflow name (e.g., "Debugging Workflow")
- **Description**: Brief overview
- **Steps**: Array of `PlanStep` with:
  - Name, Description, Type (analyze/generate/apply/verify/research/design)
  - Dependencies on previous steps
  - Tags for categorization
- **Rationale**: Why this plan is appropriate

### Workflow Types Supported

1. **Debugging**: Analyze → Locate → Generate → Apply → Verify (5 steps)
2. **Testing**: Run → Analyze → Generate → Apply → Verify (5 steps)
3. **Implementation**: Design → Research → Generate → Apply → Verify (5 steps)
4. **Refactoring**: Analyze → Plan → Generate → Apply (4 steps)

### Capability Configuration

```go
func (c *PlannerCapability) CanHandle(query, state) bool {
    // Opt-in via explicit routing or capability hint
    return state.CapabilityHint == "dynamic_planner" || 
           state.CapabilityHint == "planner"
}
```

**Note**: PlannerCapability is opt-in to avoid conflicting with WorkflowCapability. Use capability hint "dynamic_planner" or "planner" to invoke it.

### Test Coverage

- ✅ TestPlannerCapability_Execute: Tests plan generation and work item enqueueing
- ✅ TestPlannerCapability_CanHandle: Tests capability routing logic
- ✅ TestPlannerCapability_PlanToSteps: Tests plan-to-steps conversion
- ✅ TestPlannerCapability_EmptyPlan: Tests error handling

All tests passing (0.4s runtime).

## Future Enhancements

### LLM-Based Dynamic Planning (TODO)
Currently uses heuristic-based planning with `TaskClassifier`. Future enhancement:

```go
// TODO in generatePlan():
// Use ai.Generator interface to call LLM for truly dynamic planning
// The LLM would analyze task + context and generate custom step sequences
```

This would enable:
- Truly custom workflows tailored to specific tasks
- Adaptive planning based on codebase structure
- Context-aware step generation

## Usage Example

```go
// Create capability
orchestrator := &Orchestrator{...}
planner := NewPlannerCapability(orchestrator)

// Invoke with capability hint
state := capabilities.RoutingState{
    CapabilityHint: "dynamic_planner",
}

// Participates in Ask flow
result, err := planner.Execute(ctx, "fix bug in parser.go", state)

// Result: 5 work items enqueued to Ask pipeline
// Each work item executed as separate Ask transaction
```

## Key Design Decisions

1. **Participant not Controller**: Capability enqueues work items and returns control, doesn't block on execution
2. **Work Queue Integration**: Uses `getWorkQueue(ctx)` to access pipeline's work queue
3. **Step-to-Query Conversion**: Each plan step converted to a detailed query with context
4. **Priority Ordering**: Earlier steps get higher priority (100, 90, 80, ...)
5. **Opt-in Activation**: Requires explicit capability hint to avoid conflicts

## Compilation & Testing

```bash
cd lib/sop/ai/agent/codingworkflow
go build               # ✅ Compiles successfully
go test                # ✅ All tests pass (0.525s)
go test -run Planner   # ✅ Planner tests pass (0.4s)
```

## Integration

To use PlannerCapability in the agent:
1. Register the capability in agent initialization
2. Invoke with capability hint "dynamic_planner"
3. Work items will be enqueued to Ask pipeline
4. Each step executes as separate Ask transaction
5. Pipeline handles coordination and state

---

**Status**: ✅ Implementation complete and tested
**Pattern**: Participant (enqueues work, non-blocking)
**Result**: Set of work items (Steps) enqueued to Ask pipeline
