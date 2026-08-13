# Tier 3 Escalation → Phase Succession Integration

**Status**: ✅ Complete  
**Date**: 2026-01-31  
**Lines Changed**: ~120 lines across 6 files  
**Test Status**: Package tests pass; the original pending assertion-update note is now resolved

Historical note: this document captures the original Tier 3 milestone. Later work extended this path with structured cognitive assessment and suggested-phase recovery so the Ask pipeline can influence backward recovery without breaking the participant pattern.

## Overview

Integrated progressive retry Tier 3 escalation with automatic backward phase transitions. When the compatible/shared ReAct path exhausts retry attempts (Tier 3: Clarify), capabilities can now automatically transition backward to a previous phase for error recovery.

## Architecture

### Data Flow Path

```
engine_compatible.go (compatible-path Tier 3 escalation)
    ↓ EscalationState map[string]any
ai.ReasoningResponse
    ↓ resp.EscalationState
copilot.go reactLoop()
    ↓ escalationState return value
copilot.go ask()
    ↓ CapabilityResult.Data["escalation_state"]
phase_capabilities.go OnAfter()
    ↓ handleEscalation()
WorkQueue.Enqueue(backward phase transition)
```

### Code Changes

#### 1. interfaces.go - Added EscalationState Field
**File**: `lib/sop/ai/interfaces.go`  
**Lines**: +7 lines

```go
type ReasoningResponse struct {
    FinalText       string
    ToolCalls       []ToolCall
    OutcomeFacts    []string
    OutcomeRecipes  []LearnedRecipe
    CarryoverState  *CarryoverState
    // NEW: Tier 3 escalation metadata for phase succession
    EscalationState map[string]any // Fields: tier, failed_tool, failure_count, strategy, suggested_action, error_type
}
```

#### 2. engine_compatible.go - Capture Escalation State
**File**: `lib/sop/ai/agent/engine_compatible.go`
**Lines**: +15 lines (1 declaration, 11 in escalation block, 3 in returns)

**Tracking variable**:
```go
var escalationState map[string]any // Track Tier 3 escalation for phase succession
```

**Capture at Tier 3**:
```go
if retryTier == TierClarify || shouldEscalateRepairToClarification(repairAttempts) {
    emitVerboseProgress(ctx, "Tier 3 (Clarify): Escalating to user after %d failures.", repairAttempts)
    
    // Capture escalation state for phase succession
    escalationState = map[string]any{
        "tier":             3,
        "failed_tool":      toolCall.Name,
        "failure_count":    repairAttempts,
        "strategy":         repairPlan.Strategy,
        "suggested_action": "backward_phase_transition",
        "error_type":       errorMessage,
    }
    // ... existing escalation code ...
}
```

**Return in all paths**:
```go
return ai.ReasoningResponse{
    FinalText:       finalText,
    ToolCalls:       executedToolCalls,
    OutcomeFacts:    summarizeOutcomeFacts(toolResults),
    OutcomeRecipes:  outcomeRecipes,
    EscalationState: escalationState, // NEW
}, nil
```

#### 3. copilot.go - Pass Escalation Through
**File**: `lib/sop/ai/agent/copilot.go`  
**Lines**: +5 lines (signature, return, ask call)

**Updated signature**:
```go
func (a *CopilotAgent) reactLoop(ctx context.Context, query string, gen ai.Generator, fullPrompt string) (
    string, []ai.ToolCall, []string, []ai.LearnedRecipe, *ai.CarryoverState, map[string]any, error) {
    // ... (added map[string]any return for escalationState)
}
```

**Extract and return**:
```go
return finalText, resp.ToolCalls, resp.OutcomeFacts, resp.OutcomeRecipes, resp.CarryoverState, resp.EscalationState, nil
```

**Pass to OnAfter**:
```go
finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState, escalationState, err := a.reactLoop(ctx, query, gen, fullPrompt)
// ...
capResult := capabilities.CapabilityResult{
    Summary: finalText,
    Data: map[string]any{
        "tool_calls":        toolCalls,
        "outcome_facts":     outcomeFacts,
        "outcome_recipes":   outcomeRecipes,
        "carryover_state":   carryoverState,
        "escalation_state":  escalationState, // NEW
    },
}
```

#### 4. copilot_v2.go - V2 Ask Pipeline Support
**File**: `lib/sop/ai/agent/copilot_v2.go`  
**Lines**: +2 lines

Same changes as copilot.go for V2 Ask pipeline compatibility.

#### 5. phase_capabilities.go - Handle Escalation
**File**: `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`  
**Lines**: +108 lines (10 in OnAfter, 98 in handleEscalation)

**OnAfter Priority Check**:
```go
func (c *PhaseCapability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
    // 0. Check for Tier 3 escalation FIRST (takes priority over normal phase succession)
    if escalation, ok := result.Data["escalation_state"].(map[string]any); ok && escalation != nil {
        tier, _ := escalation["tier"].(int)
        if tier >= 3 {
            return c.handleEscalation(ctx, query, escalation)
        }
    }
    
    // 1. Check if a capture tool was called (phase completion signal)
    // ... rest of existing logic ...
}
```

**Backward Transition Logic**:
```go
func (c *PhaseCapability) handleEscalation(ctx context.Context, query string, escalation map[string]any) error {
    // Determine backward phase transition
    switch c.phaseID {
    case "execution":   targetPhase = "planning"
    case "planning":    targetPhase = "investigation"
    case "review":      targetPhase = "execution"
    case "delivery":    targetPhase = "review"
    case "investigation": // First phase, stay in place
        return nil
    }
    
    // Enqueue with high priority (15 > normal 10)
    workItem := struct {
        Query    string
        Actor    string
        Priority int
        Context  map[string]any
    }{
        Query:    query,
        Actor:    "phase:" + targetPhase,
        Priority: 15, // Higher than normal phase succession
        Context: map[string]any{
            "transition_reason": "tier3_escalation",
            "transition_type":   "backward",
            "from_phase":        c.phaseID,
            "failed_tool":       failedTool,
            "failure_count":     failureCount,
            "error_type":        errorType,
            "escalation":        escalation,
            "explanation":       transitionReason,
        },
    }
    
    return queue.Enqueue(workItem)
}
```

#### 6. Test Updates
**Files**: 
- `lib/sop/ai/agent/copilot_pipeline_test.go` (+2 lines)
- `lib/sop/ai/agent/copilot_streaming_test.go` (+1 line)

Updated test files to handle new escalationState return value:
```go
finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState, escalationState, err := ag.reactLoop(...)
_ = escalationState // Test doesn't validate escalation state yet
```

## Participant Pattern Compliance

✅ **Verified Compliant**

- **Escalation capture**: Happens inside provider ReAct loop (correct - part of reasoning)
- **Phase succession**: Happens in OnAfter AFTER ReAct completes (correct - participant pattern)
- **Queue enqueue**: Single work item, not batch (correct - no controller loop)
- **Ask pipeline control**: Queue processing still owned by Ask() (correct - single orchestrator)

## Backward Transition Rules

| Current Phase    | Target Phase     | Reason                                          |
|-----------------|------------------|-------------------------------------------------|
| execution       | planning         | Revise approach based on execution failures     |
| planning        | investigation    | Gather more context for better planning         |
| review          | execution        | Fix issues found during review                  |
| delivery        | review           | Verify outputs before delivery retry            |
| investigation   | (stays in phase) | First phase - no backward transition available  |

## Priority System

- **Normal phase succession**: Priority 10
- **Tier 3 escalation**: Priority 15 (higher priority ensures immediate attention)

## Observability

**Log Entry Example**:
```
INFO Phase succession: Handling Tier 3 escalation
    current_phase=execution
    failed_tool=write_file
    failure_count=2
    error_type="permission denied: /protected/path"
    
INFO Phase succession: Enqueued backward phase transition due to Tier 3 escalation
    from_phase=execution
    to_phase=planning
    failed_tool=write_file
    failure_count=2
    reason="Execution phase encountered repeated failures - returning to planning to revise approach"
```

## Testing

### Compilation
✅ All code compiles without errors

### Test Results
```
ok      github.com/sharedcode/sop/ai/agent/codingworkflow       (cached)
ok      github.com/sharedcode/sop/ai/agent/capabilities (cached)
ok      github.com/sharedcode/sop/ai/agent/codegen      (cached)
ok      github.com/sharedcode/sop/ai/agent/planner      (cached)
```

### Historical Test Note
The original milestone left two Tier 2B-era assertions behind while the retry threshold was changing.
Those package-level expectations have since been updated, and the current package test suite passes.

## Benefits

1. **Automatic Error Recovery**: LLM can recover from repeated failures by revisiting previous phases
2. **Progressive Workflow**: Natural backward flow when execution gets stuck
3. **Provider Agnostic**: Works across ChatGPT, Anthropic, Gemini, and default ReAct loops
4. **Participant Pattern**: Maintains architecture - no controller loops in capabilities
5. **Rich Context**: Backward transitions carry full escalation metadata for informed recovery

## Example Scenario

```
User: "Deploy the updated API to production"

1. Investigation Phase
   → Gathers production config, checks permissions
   → Transitions forward to Planning

2. Planning Phase
   → Plans deployment steps: build, test, deploy
   → Transitions forward to Execution

3. Execution Phase - Attempt 1
   → Tool: write_file(/production/config.yaml)
   → Error: "permission denied"
   → Retry with research (Tier 2)

4. Execution Phase - Attempt 2
   → Tool: write_file(/production/config.yaml)
   → Error: "permission denied"
   → Escalate to Tier 3
   → EscalationState captured

5. OnAfter Hook
   → Detects Tier 3 escalation
   → Backward transition: execution → planning
   → Priority 15 (immediate)

6. Planning Phase (Retry)
   → LLM sees escalation context
   → Plans alternative: use deployment tool with elevated permissions
   → Transitions forward to Execution

7. Execution Phase - Attempt 3
   → Tool: deploy_with_permissions(config)
   → Success! ✅
```

## Implementation Stats

- **Total Lines Changed**: ~120 lines
- **Files Modified**: 6 files
- **New Functions**: 1 (handleEscalation)
- **Architecture Compliance**: ✅ Participant Pattern maintained
- **Test Regression**: 0 new failures
- **Compilation**: ✅ Clean build

## Follow-Through

1. ✅ **Core Implementation**: Complete
2. ✅ **Compilation**: Verified
3. ✅ **Test Validation**: Package tests pass
4. ✅ **Documentation**: Indexed and aligned with the current retry/cognition docs
5. 📋 **Optional Further Validation**: Exercise Tier 3→backward phase transitions in live workflows when doing end-to-end workflow testing

## Related Documentation

- [TIER2B_PROGRESSIVE_ESCALATION_IMPLEMENTATION.md](./TIER2B_PROGRESSIVE_ESCALATION_IMPLEMENTATION.md) - Progressive retry system
- [RETRY_ORCHESTRATION_STATE.md](./RETRY_ORCHESTRATION_STATE.md) - Current retry/cognition architecture
- [ASK_V2_ARCHITECTURE.md](./ASK_V2_ARCHITECTURE.md) - Current Ask pipeline architecture
- [ARCHITECTURE_PATTERNS.md](./ARCHITECTURE_PATTERNS.md) - Participant Pattern
- [PLANNING_FIRST_LLM_ORCHESTRATION.md](./PLANNING_FIRST_LLM_ORCHESTRATION.md) - Phase succession design
- [WORKFLOWS.md](./WORKFLOWS.md) - Workflow architecture
- [/memories/repo/participant-pattern-not-controller.md](/memories/repo/participant-pattern-not-controller.md) - Architecture constraints
