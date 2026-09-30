# Startup Rehydration Implementation Summary

**Date:** 2026-08-30  
**Feature:** Automatic workstream persistence and startup rehydration  
**Status:** ✅ Implemented and Tested

## Overview

The SOP daemon now automatically:
1. **Persists** workstream state every 30 seconds (piggybacked on STM batch commits)
2. **Rehydrates** paused workstreams on daemon startup
3. **Shows reminder** about paused work while **preserving user choice**

## User Experience

### Scenario: Multi-Day Task with Restart

**Day 1 (4:00 PM):**
```
User: "Implement user authentication system"
Agent: [Starts investigation phase...]

User: "Pause that, I need to fix a bug first"
Agent: ✓ Work paused in planning phase
```

**Daemon automatically persists state (no user action needed)**

**Day 2 (9:00 AM) - After daemon restart:**
```
[Daemon logs on startup]
INFO: Paused workstreams available
📌 Paused Work Streams (require manual resume):
   1. **User authentication system** (paused in planning phase) 
      - User requested to work on something else 17h ago

💡 Use 'resume [number]' to continue a paused task
```

**User has full choice:**
- `"continue the authentication work"` → Agent resumes from planning phase
- `"work on the homepage redesign"` → Starts new task, paused work stays available
- `"abandon that authentication task"` → Marks work as abandoned

## Architecture

### 1. Persistence Hook
**File:** [lib/sop/ai/agent/copilot_executor.go](../lib/sop/ai/agent/copilot_executor.go#L309-L314)

```go
// Hook up automatic persistence on STM batch commits
stmStore.SetSessionStatePersister(func(persistCtx context.Context, btreeInstance btree.BtreeInterface[string, any]) error {
    if a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
        return nil
    }
    return a.service.session.Memory.Persist(persistCtx, btreeInstance)
})
```

**Trigger:** Every 30 seconds when STM batches episodes ([lib/sop/ai/memory/stm_store.go:254](../lib/sop/ai/memory/stm_store.go#L254))

### 2. Startup Rehydration
**File:** [lib/sop/ai/agent/copilot_executor.go](../lib/sop/ai/agent/copilot_executor.go#L291-L307)

```go
// Rehydrate session memory from persisted workstream state
if a.service != nil && a.service.session != nil && a.service.session.Memory != nil && a.Memory != nil {
    stmStore := a.Memory.STMStore()
    if stmStore != nil && stmStore.Primary() != nil {
        // Load persisted workstreams (if any)
        if err := a.service.session.Memory.Rehydrate(ctx, stmStore.Primary()); err != nil {
            log.Warn("CopilotAgent: Failed to rehydrate session memory, starting fresh", "error", err)
        } else {
            // Show reminder about paused workstreams
            reminder := a.service.session.Memory.FormatPausedWorkStreamsReminder()
            if reminder != "" {
                log.Info("CopilotAgent: Paused workstreams available", "reminder", reminder)
                if sendEvent, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && sendEvent != nil {
                    sendEvent("content", reminder)
                }
            }
        }
    }
}
```

**Lifecycle:** Runs in `Initialize()` method, called during `Open()` phase

**Chat UI Integration:** ✅ Implemented (2026-08-30)
- Reminder automatically sent via event streamer if available
- Falls back to logging if no active chat session yet
- User can always access via `list_workstreams` tool

### 3. Persistence Implementation
**File:** [lib/sop/ai/agent/memory_shortterm.go](../lib/sop/ai/agent/memory_shortterm.go#L672-L725)

**Persist:**
- Serializes threads, phase state, artifacts to B-tree
- Sets 7-day expiration timestamp
- Stores under key `"stm_session_state"`

**Rehydrate:**
- Loads from B-tree
- Checks expiration (ignores > 7 days old)
- Reconstructs thread structure
- Validates manual resume flags

## What Gets Preserved

✅ **Phase Progression:**
- Phase history (e.g., `["investigation", "planning", "execution"]`)
- Current phase
- Phase iteration counts

✅ **Structured Plans:**
- Objective, workflow shape
- System context (subsystem, targets, contracts)
- Steps and completion status

✅ **Phase Artifacts:**
- Planning decisions
- Investigation findings
- Execution progress

✅ **Pause State:**
- Pause reason ("user requested to work on something else")
- Timestamp when paused
- Manual resume flag

✅ **Conversation Context:**
- Thread exchanges
- Root prompt
- Labels and categories

## Testing

**Test File:** [lib/sop/ai/agent/startup_rehydration_test.go](../lib/sop/ai/agent/startup_rehydration_test.go)

### Test Coverage:
1. ✅ `TestStartupRehydrationFlow` - Full persist → restart → rehydrate → resume cycle
2. ✅ `TestRehydrationWithEmptyBtree` - Graceful handling of fresh daemon (no persisted state)
3. ✅ `TestRehydrationPreservesStructuredPlan` - Complex structured plan preservation

**All tests passing:** ✅

### Test Output:
```
=== RUN   TestStartupRehydrationFlow
✓ Persisted state found in B-tree
✓ Thread count restored
✓ Workstream query preserved
✓ Phase progression restored (3 phases)
✓ Pause state preserved
✓ Phase artifacts restored
✓ Reminder formatted for user display
✓ Helper methods functional
✅ Startup rehydration works correctly
✅ User choice mechanism functional
✅ Phase state preserved across restarts
--- PASS: TestStartupRehydrationFlow (0.10s)
```

## Design Principles

### ✅ User Agency First
- **No forced resumption** - System informs, user chooses
- Natural language control ("continue work" vs "work on X")
- Clear reminder with pause reason and time elapsed

### ✅ Participant Pattern Compliance
- Rehydration happens **before Ask pipeline starts**
- No controller loops - just state restoration
- LLM sees paused workstreams via `list_workstreams` tool
- Resume triggered by `resume_workstream` tool (user-initiated)

### ✅ Crash Recovery
- State survives daemon restarts
- Power loss doesn't lose work
- 30-second persistence cadence balances safety vs performance

### ✅ No Manual Save Commands
- Fully automatic persistence
- Ze// sendToChat(ctx, reminder, MessageTypeInfo)
    log.Info("CopilotAgent: Paused workstreams available", "reminder", reminder)
}
```

### Expiration Policy Tuning
**Current:** 7-day hard cutoff  
**Future:** Consider:
- Configurable TTL per workspace
- "Archive" state for 7-30 days (read-only)
- Smart suggestions: "You paused this 28 days ago, abandon it?"
- TODO: When ship-quality UX is ready, prompt the user on restart to explicitly resume or abandon/trash a paused workstream before starting a new "fix failing tests" request.

### Multi-Session Awareness
**Current:** Single session per daemon  
**Future:** If supporting multiple concurrent sessions:
- Session-scoped B-tree keys
- Per-session rehydration
- Session ID in persistence key

## Files Modified

1. ✅ [lib/sop/ai/agent/copilot_executor.go](../lib/sop/ai/agent/copilot_executor.go) - Startup rehydration + persistence hook
2. ✅ [lib/sop/ai/agent/startup_rehydration_test.go](../lib/sop/ai/agent/startup_rehydration_test.go) - Comprehensive test suite (NEW)

## Files Already Implemented (Used)

1. ✅ [lib/sop/ai/agent/memory_shortterm.go](../lib/sop/ai/agent/memory_shortterm.go) - Persist/Rehydrate methods (672-727)
2. ✅ [lib/sop/ai/memory/stm_store.go](../lib/sop/ai/memory/stm_store.go) - SetSessionStatePersister hook (281-288)

## Verification Steps

1. ✅ Code compiles without errors
2. ✅ All unit tests pass
3. ✅ Persistence writes to B-tree
4. ✅ Rehydration loads threads correctly
5. ✅ Phase state preserved
6. ✅ Reminder formats correctly
7. ✅ Empty B-tree handled gracefully
8. ✅ Structured plans preserved

## Summary

**Implementation Status:** ✅ Complete and Working

The system now provides **automatic continuity** across daemon restarts while **preserving user autonomy**. Users can:
- Pause work naturally ("I need to do something else")
- See what work is paused on next startup
- Choose to resume or start new work
- Not worry about manual saves

**This solves the original ask:** "LLM should be able to continue from its memory system, no need for user to ask LLM to read a doc to document state."

The answer is: **Yes, with user choice.** State persists automatically, rehydrates on startup, and user can choose whether to continue or do something else.
Production-Ready