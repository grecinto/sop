# Phase 2: Ask Pipeline Integration

## Overview

This document describes the integration of the Step lifecycle hooks with the CopilotAgent Ask pipeline, enabling Steps to leverage memory, MRU, multi-turn reasoning, and other Ask pipeline features.

## Architecture

The integration connects three layers:
1. **WorkflowManager** - Orchestrates Step sequence, injects SharedInfrastructure
2. **Step** - Implements lifecycle hooks to interact with Ask pipeline
3. **Ask Pipeline** - Processes user requests through CopilotAgent.ask()

## Lifecycle Hook Integration Points

### 1. OnStart Hook
**Location**: `lib/sop/ai/agent/copilot.go:2912-2916`  
**Integration**: Called immediately after capability classification and composition

```go
if step, isStep := cap.(cw.Step); isStep {
    ctx = context.WithValue(ctx, ctxKeyActiveStep, step)
    if err := step.OnStart(ctx, query, routingState); err != nil {
        return "", fmt.Errorf("step OnStart failed: %w", err)
    }
}
```

**Purpose**: Initialize step-specific state, set up tracking variables  
**Context**: Called once per Ask transaction, before any prompts are built

### 2. OnBeforePrompt Hook
**Location**: `lib/sop/ai/agent/copilot_prompt.go:163`  
**Integration**: Called during system prompt construction

```go
if isStep {
    if additionalContext, err := step.OnBeforePrompt(ctx, query, routingState); err != nil {
        log.Warn("Step OnBeforePrompt failed", "error", err)
    } else if additionalContext != "" {
        sections = append(sections, additionalContext)
    }
}
```

**Purpose**: Inject step-specific context into the system prompt  
**Context**: Called after capability classification and after BuildContext/BuildInstructions have been assembled, but before prompt finalization

### 3. OnAfterToolResult Hook
**Location**: `lib/sop/ai/agent/copilot.go:5375-5391`  
**Integration**: Called after each tool execution completes

```go
// Step OnAfterToolResult hook
if step, ok := ctx.Value(ctxKeyActiveStep).(cw.Step); ok && step != nil {
    if cap, ok := ctx.Value(ctxKeySelectedCapability).(capabilities.Capability); ok && cap != nil {
        routingState := capabilities.RoutingState{
            Domain: "",  // Would need to be passed through context if needed
            Gate: "",
            CapabilityHint: "",
        }
        if updatedState, shouldContinue, stepErr := step.OnAfterToolResult(ctx, toolName, res, routingState); stepErr != nil {
            log.Warn("Step OnAfterToolResult failed", "error", stepErr)
        } else if !shouldContinue {
            log.Info("Step requested early termination", "tool", toolName)
            // Update context with new state
            _ = updatedState
        }
    }
}
```

**Purpose**: React to tool execution results, access SharedInfrastructure operations  
**Context**: Called during ReAct loop, after each tool Handler returns  
**Note**: This is where Steps perform their primary work (e.g., test analysis, patch generation)

### 4. OnAfterTurn Hook
**Location**: `lib/sop/ai/agent/copilot.go:2939-2960`  
**Integration**: Called after reasoning engine completes (all turns in ReAct loop)

```go
// Step OnAfterTurn hook (called after reasoning completes)
if step, ok := ctx.Value(ctxKeyActiveStep).(cw.Step); ok && step != nil {
    if cap, ok := ctx.Value(ctxKeySelectedCapability).(capabilities.Capability); ok && cap != nil {
        routingState := capabilities.RoutingState{
            Domain: taskContext.Domain,
            Gate: taskContext.RoutingGate,
            CapabilityHint: taskContext.CapabilityHint,
        }
        if shouldContinue, stepErr := step.OnAfterTurn(ctx, 1, routingState); stepErr != nil {
            log.Warn("Step OnAfterTurn failed", "error", stepErr)
        } else if !shouldContinue {
            log.Info("Step requested workflow termination")
        }
    }
}
```

**Purpose**: Decide whether the step should continue or complete  
**Context**: Called once per Ask transaction, after delegateToReasoningEngine returns  
**Note**: Currently treats entire Ask as single turn; could be enhanced to track individual ReAct iterations

### 5. OnFinalize Hook
**Location**: `lib/sop/ai/agent/copilot_mru.go:201-220`  
**Integration**: Called before memory/MRU persistence (epilogueAndCleanup)

```go
func (a *CopilotAgent) epilogueAndCleanup(...) {
    // Step OnFinalize hook
    if step, ok := ctx.Value(ctxKeyActiveStep).(cw.Step); ok && step != nil {
        if cap, ok := ctx.Value(ctxKeySelectedCapability).(capabilities.Capability); ok && cap != nil {
            routingState := capabilities.RoutingState{
                Domain: "",
                Gate: "",
                CapabilityHint: "",
            }
            if stepResult, stepErr := step.OnFinalize(ctx, routingState); stepErr != nil {
                log.Warn("Step OnFinalize failed", "error", stepErr)
            } else {
                log.Info("Step finalized", "outcome", stepResult.Outcome, "message", stepResult.Message)
                // Could store stepResult in context or return it somehow
                _ = stepResult
            }
        }
    }
    
    if a.service != nil && a.service.session != nil {
        // ... existing epilogue logic
    }
}
```

**Purpose**: Return final StepResult, perform cleanup  
**Context**: Called after all reasoning completes, before conversation history persistence

## Context Keys

Two context keys are used to pass Step and Capability through the Ask pipeline:

```go
ctxKeyActiveStep        // Stores current Step instance
ctxKeySelectedCapability // Stores selected Capability
```

These are set in `copilot.go:ask()` and read by hook integration points.

## Integration Benefits

With Phase 2 complete, Steps now have access to:

1. **Memory System** - Steps can persist and retrieve conversation state
2. **MRU Tracking** - Steps can mark categories as recently used
3. **Multi-Turn Reasoning** - Steps participate in ReAct loops with tool calls
4. **Capability Registry** - Steps can leverage 34+ existing capabilities
5. **SharedInfrastructure** - Steps access 16 components for AST, patches, tests, repo operations

## Execution Flow

```
1. User submits task
2. WorkflowManager.Execute() called
   └─> For each Step:
       a. WorkflowManager.SetSharedInfrastructure(step)
       b. WorkflowManager.executeStepWithRetry(step)
           └─> WorkflowManager.executeStepOnce(step)
               └─> [INTEGRATION POINT] Ask Pipeline
                   
3. Ask Pipeline (CopilotAgent.ask)
   └─> evaluateRoutingGates()
   └─> [Detect Step capability]
   └─> OnStart() hook called ✓
   └─> buildSystemPrompt()
       └─> Capability.BuildContext() ✓
       └─> Capability.BuildInstructions() ✓
       └─> OnBeforePrompt() hook called ✓
   └─> delegateToReasoningEngine()
       └─> NativeReActEngine.Run()
           └─> ReAct loop iterations
               └─> For each tool:
                   └─> Execute(toolName, args)
                       └─> toolDef.Handler()
                       └─> OnAfterToolResult() hook called ✓
   └─> OnAfterTurn() hook called ✓
   └─> epilogueAndCleanup()
       └─> OnFinalize() hook called ✓
       └─> Persist memory/MRU
       
4. WorkflowManager receives StepResult
   └─> Handle outcome (success/failed/retry/etc.)
   └─> Continue to next Step or complete workflow
```

## Example: RunTestsStep Flow

Here's how a concrete Step uses the integrated lifecycle hooks:

```go
type RunTestsStep struct {
    BaseStep
    workingDir string
}

// 1. OnStart - Initialize
func (s *RunTestsStep) OnStart(ctx, query, state) error {
    s.SetStepState("start_time", time.Now())
    return nil
}

// 2. OnBeforePrompt - Inject context
func (s *RunTestsStep) OnBeforePrompt(ctx, query, state) (string, error) {
    return fmt.Sprintf("Working directory: %s\nGoal: Execute tests and capture failures", s.workingDir), nil
}

// 3. OnAfterToolResult - Access SharedInfrastructure
func (s *RunTestsStep) OnAfterToolResult(ctx, toolName, result, state) (state, bool, error) {
    if toolName == "run_tests" {
        testOps := s.infra.NewTestOperations()  // ← Uses SharedInfrastructure
        testResult, err := testOps.RunTests(...)
        
        if testResult.Failed {
            s.SetStepState("failed_tests", testResult.Failures)
        }
        
        return state, true, nil
    }
    return state, true, nil
}

// 4. OnAfterTurn - Decide continuation
func (s *RunTestsStep) OnAfterTurn(ctx, turnNumber, state) (bool, error) {
    if failures, ok := s.GetStepStateValue("failed_tests"); ok && failures != nil {
        return false, nil  // Stop, we have failures to report
    }
    return true, nil  // Continue for more turns
}

// 5. OnFinalize - Return result
func (s *RunTestsStep) OnFinalize(ctx, state) (StepResult, error) {
    duration := time.Since(s.GetStepState()["start_time"].(time.Time))
    
    if failures, ok := s.GetStepStateValue("failed_tests"); ok {
        return StepResult{
            Outcome: StepOutcomeFailed,
            Message: fmt.Sprintf("Found %d test failures", len(failures.([]string))),
            Data: map[string]interface{}{
                "failures": failures,
                "duration": duration,
            },
        }, nil
    }
    
    return StepResult{
        Outcome: StepOutcomeSuccess,
        Message: "All tests passed",
    }, nil
}
```

## Testing

Integration tests verify that hooks are called correctly:

- `TestStepLifecycleHooksCalledInOrder` - Verifies all 5 hooks can be called in sequence
- `TestStepSharedInfrastructureInjection` - Verifies infra field is accessible
- `TestStepOutcomeHandling` - Verifies StepOutcome enum handling
- `TestStepStateManagementAcrossHooks` - Verifies state persists across hooks
- `TestStepTerminalityInWorkflow` - Verifies terminal step metadata

Run tests:
```bash
go test ./lib/sop/ai/agent/codingworkflow -v
```

## Future Enhancements

### 1. Per-Iteration OnAfterTurn
Currently OnAfterTurn is called once per Ask transaction. Could be enhanced to call after each ReAct iteration by integrating into `engine_native.go:runDefaultLoop()`.

### 2. RoutingState Propagation
RoutingState fields (Domain, Gate, CapabilityHint) are partially populated. Could be improved by passing these through context or reconstructing from TaskContext.

### 3. StepResult Integration
OnFinalize returns StepResult but it's currently logged and discarded. Could be:
- Stored in context for later retrieval
- Returned to WorkflowManager via callback
- Used to modify final Ask response

### 4. Error Recovery
Hook errors are logged but don't stop execution. Could add:
- Retry logic for transient hook failures
- Error accumulation across hooks
- Circuit breaker for repeated hook failures

### 5. Streaming Hook Events
For long-running Steps, could stream hook events to UI:
- Tool execution progress
- Turn completion notifications
- Intermediate results

## See Also

- [ARCHITECTURE.md](./ARCHITECTURE.md) - Overall WorkflowManager design
- [README.md](./README.md) - Usage guide and examples
- [shared_infrastructure.go](./shared_infrastructure.go) - 16 component operations
- [step_capability.go](./step_capability.go) - BaseStep and example Steps
- [workflow_manager.go](./workflow_manager.go) - Orchestration logic
