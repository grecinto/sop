# Work Stream Persistence Design

**Status**: 📋 Planned (Phase 2.5)  
**Priority**: HIGH  
**Estimated Effort**: 2 days  
**Last Updated**: 2026-07-29

---

## Problem Statement

### Current Architecture: In-Memory Only

Work streams are currently **ephemeral** and **lost on restart**:

```go
// service.go - Fresh session every startup
func NewService(...) *Service {
    return &Service{
        session: NewRunnerSession(),  // Empty Memory
        // ...
    }
}

// copilot.go - No rehydration from disk
if sess.Memory == nil {
    sess.Memory = NewShortTermMemory()  // Lost on restart
}
```

**What's lost**:
- ❌ All active/paused work streams (up to 20 threads)
- ❌ Phase progression (investigation → planning → execution)
- ❌ Phase artifacts (findings, plans, test results, handoffs)
- ❌ Parent-child work stream relationships (nested interjections)
- ❌ Manual resume state (paused streams requiring user action)
- ❌ Loop detection state (iteration counts, phase patterns)
- ❌ Runtime caveats and session boundaries

### User Impact

**Scenario 1**: Multi-day feature development
```
Day 1, 5pm: "Implement Redis caching - investigating design patterns..."
[Close IDE for the day]
Day 2, 9am: "Continue work"
Agent: ❌ "I don't have context about Redis caching work"
```

**Scenario 2**: Crash during execution
```
User: "Fix auth bug and add tests"
[Agent reaches execution phase with 3 files analyzed]
[VS Code crashes]
User: Restart VS Code, "Continue"
Agent: ❌ Lost all investigation findings and execution plan
```

**Scenario 3**: Paused work streams
```
User: "Pause this and work on payment bug"
[Fixes payment bug, closes IDE]
Next day: "What was I working on?"
Agent: ❌ No reminder about paused auth bug investigation
```

### Industry Standards

All major development tools persist session state:

**VS Code**:
- ✅ Persists open files, cursor positions, unsaved changes
- ✅ Restores debugging sessions, breakpoints
- ✅ Rehydrates terminal state, watch expressions

**IntelliJ IDEA**:
- ✅ Persists run configurations, tool window layouts
- ✅ Restores editor tabs, search history
- ✅ Rehydrates local history, shelved changes

**ChatGPT/Claude**:
- ✅ Persists conversation threads across sessions
- ✅ Can resume conversations from days/weeks ago
- ✅ Maintains context within conversations

**GitHub Copilot Chat**:
- ✅ Some conversation state persists in VS Code workspace storage
- ✅ Recent context maintained between IDE restarts

---

## Solution Architecture

### Storage Layer: STM B-tree

Use existing `ShortTermMemoryStore` infrastructure:

```
Storage Location: {user_id}_stm B-tree
Key: "stm_session_state"
Value: {
    id: "stm_session_state",
    version: 1,
    threads: {
        "uuid-1": ConversationThread { ... },
        "uuid-2": ConversationThread { ... }
    },
    order: ["uuid-1", "uuid-2"],
    current_id: "uuid-1",
    caveats: [...],
    boundaries: [...],
    updated_at: 1722288000,
    expires_at: 1722892800  // 7 days later
}
```

### Persistence Triggers

**Automatic (Piggybacked on STM Commits)**:
- ✅ **Every 30 seconds** - During episode batch commits (existing infrastructure)
- ✅ **Same transaction** - Session state persists with episodes atomically

**What triggers episode commits** (which trigger session state persistence):
1. Tool executions (via `LogEpisodeToSTM`)
2. Phase transitions (if episodes are logged)
3. Coding memory events
4. Any `recordCodingMemory()` calls

**No explicit calls needed** - Session state automatically persists whenever episodes are batched.

**On shutdown**:
- If episodes are pending in queue, they flush with session state
- No special shutdown hook needed (existing committer handles it)

### Rehydration Flow

```go
// On copilot initialization
func (a *CopilotAgent) Initialize(ctx) error {
    // ... open STM B-tree ...
    
    if sess.Memory == nil {
        sess.Memory = NewShortTermMemory()
    }
    
    // Try to load saved session state
    if err := sess.Memory.Rehydrate(ctx, btreeSTM); err != nil {
        log.Warn("Failed to rehydrate session, starting fresh", err)
        // Continue with empty memory (graceful degradation)
    }
    
    // Show user what was restored
    if len(sess.Memory.Threads) > 0 {
        reminder := sess.Memory.FormatPausedWorkStreamsReminder()
        if reminder != "" {
            showToUser(reminder)
        }
    }
}
```

---

## Implementation Plan

### 1. Core Persistence Methods

**File**: `lib/sop/ai/agent/memory_shortterm.go`

```go
const WorkStreamSessionTTL = 7 * 24 * time.Hour

// Persist saves the entire ShortTermMemory state to B-tree
func (stm *ShortTermMemory) Persist(ctx context.Context, btree btree.BtreeInterface[string, any]) error {
    if stm == nil {
        return nil
    }
    
    // Serialize threads (handle nested structs)
    threadsJSON, err := json.Marshal(stm.Threads)
    if err != nil {
        return fmt.Errorf("failed to marshal threads: %w", err)
    }
    
    orderJSON, err := json.Marshal(stm.Order)
    if err != nil {
        return fmt.Errorf("failed to marshal order: %w", err)
    }
    
    payload := map[string]any{
        "id":           "stm_session_state",
        "version":      1,
        "threads_json": string(threadsJSON),
        "order_json":   string(orderJSON),
        "current_id":   stm.CurrentThreadID.String(),
        "caveats":      stm.RuntimeCaveats,
        "boundaries":   stm.SessionBoundaries,
        "updated_at":   time.Now().Unix(),
        "expires_at":   time.Now().Add(WorkStreamSessionTTL).Unix(),
    }
    
    _, err = btree.Add(ctx, "stm_session_state", payload)
    if err != nil {
        return fmt.Errorf("failed to persist session state: %w", err)
    }
    
    log.Debug("Persisted ShortTermMemory session state", 
        "threads", len(stm.Threads),
        "current", stm.CurrentThreadID)
    
    return nil
}

// Rehydrate restores ShortTermMemory state from B-tree
func (stm *ShortTermMemory) Rehydrate(ctx context.Context, btree btree.BtreeInterface[string, any]) error {
    if stm == nil {
        return nil
    }
    
    // Try to load saved state
    val, err := btree.Get(ctx, "stm_session_state")
    if err != nil {
        // No saved state is OK (fresh session)
        return nil
    }
    
    payload, ok := val.(map[string]any)
    if !ok {
        return fmt.Errorf("invalid session state payload type")
    }
    
    // Check expiration
    if expiresAt, ok := payload["expires_at"].(int64); ok {
        if time.Now().Unix() > expiresAt {
            log.Info("Session state expired, starting fresh")
            return nil
        }
    }
    
    // Deserialize threads
    if threadsJSON, ok := payload["threads_json"].(string); ok {
        threads := make(map[sop.UUID]*ConversationThread)
        if err := json.Unmarshal([]byte(threadsJSON), &threads); err != nil {
            return fmt.Errorf("failed to unmarshal threads: %w", err)
        }
        stm.Threads = threads
    }
    
    // Deserialize order
    if orderJSON, ok := payload["order_json"].(string); ok {
        order := []sop.UUID{}
        if err := json.Unmarshal([]byte(orderJSON), &order); err != nil {
            return fmt.Errorf("failed to unmarshal order: %w", err)
        }
        stm.Order = order
    }
    
    // Restore current thread ID
    if currentIDStr, ok := payload["current_id"].(string); ok && currentIDStr != "" {
        stm.CurrentThreadID = sop.UUID(currentIDStr)
    }
    
    // Restore caveats and boundaries (if present)
    if caveats, ok := payload["caveats"].([]RuntimeCaveat); ok {
        stm.RuntimeCaveats = caveats
    }
    if boundaries, ok := payload["boundaries"].([]SessionBoundary); ok {
        stm.SessionBoundaries = boundaries
    }
    
    log.Info("Rehydrated ShortTermMemory session state",
        "threads", len(stm.Threads),
        "current", stm.CurrentThreadID)
    
    return nil
}

// PruneExpiredSessions removes old session states (called during sleep cycle)
func PruneExpiredSessions(ctx context.Context, btree btree.BtreeInterface[string, any], now time.Time) (int, error) {
    val, err := btree.Get(ctx, "stm_session_state")
    if err != nil {
        return 0, nil // No session state to prune
    }
    
    payload, ok := val.(map[string]any)
    if !ok {
        return 0, nil
    }
    
    if expiresAt, ok := payload["expires_at"].(int64); ok {
        if now.Unix() > expiresAt {
            if _, err := btree.Remove(ctx, "stm_session_state"); err != nil {
                return 0, err
            }
            return 1, nil
        }
    }
    
    return 0, nil
}
```

### 2. Piggyback on Existing STM Batch Commits

**Approach**: Reuse the existing 30-second periodic committer instead of creating a separate worker.

**File**: `lib/sop/ai/memory/stm_store.go`

```go
// Add callback field to ShortTermMemoryStore
type ShortTermMemoryStore struct {
    agentID string
    userID  string
    primary btree.BtreeInterface[string, any]
    byTime  btree.BtreeInterface[string, any]
    maxAge  time.Duration
    
    // NEW: Hook for session state persistence
    sessionStatePersister func(context.Context, btree.BtreeInterface[string, any]) error
}

// SetSessionStatePersister allows external code to hook into batch commits
func (s *ShortTermMemoryStore) SetSessionStatePersister(fn func(context.Context, btree.BtreeInterface[string, any]) error) {
    if s == nil {
        return
    }
    s.sessionStatePersister = fn
}

// In StartPeriodicCommitter - modify batch commit to include session state
func (s *ShortTermMemoryStore) StartPeriodicCommitter(ctx, systemDB, queue) error {
    // ... existing code ...
    
    go func() {
        for {
            select {
            case firstPayload := <-queue:
                // ... existing batch logic ...
                
                // Batch all episodes
                batchCount := 0
                for batching && batchCount < stmBatchMaxItems {
                    // ... existing batching code ...
                }
                
                // NEW: Persist session state in the same transaction
                if s.sessionStatePersister != nil {
                    if err := s.sessionStatePersister(ctx, s.primary); err != nil {
                        log.Warn("ShortTermMemoryStore: failed to persist session state", 
                            "agent_id", s.agentID, "error", err)
                        // Don't rollback - episodes are more important than session state
                    }
                }
                
                // Commit transaction (episodes + session state)
                if err := tx.Commit(ctx); err != nil {
                    log.Warn("ShortTermMemoryStore: commit failed", 
                        "agent_id", s.agentID, "count", batchCount, "error", err)
                } else {
                    log.Debug("ShortTermMemoryStore: batch committed to STM successfully", 
                        "agent_id", s.agentID, "count", batchCount)
                }
                s.Close()
            }
        }
    }()
}
```

### 3. Integration with CopilotAgent

**File**: `lib/sop/ai/agent/copilot.go`

```go
// Initialize - Add rehydration + hook setup
func (a *CopilotAgent) Initialize(ctx context.Context) error {
    // ... existing initialization ...
    
    // Open STM for session state rehydration
    tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)
    
    btreeSTM, err := a.Memory.OpenShortTermMemory(ctx, a.systemDB, tx)
    if err != nil {
        return err
    }
    
    // Initialize session memory if needed
    if a.service != nil && a.service.session != nil {
        if a.service.session.Memory == nil {
            a.service.session.Memory = NewShortTermMemory()
        }
        
        // Try to rehydrate saved session
        if err := a.service.session.Memory.Rehydrate(ctx, btreeSTM.(btree.BtreeInterface[string, any])); err != nil {
            log.Warn("Failed to rehydrate session state", "error", err)
        }
        
        // Show reminder about paused work
        reminder := a.service.session.Memory.FormatPausedWorkStreamsReminder()
        if reminder != "" {
            log.Info("Restored paused work streams", "reminder", reminder)
            // TODO: Show to user via system message
        }
        
        // NEW: Hook into periodic STM commits
        stmStore := a.Memory.STMStore()
        if stmStore != nil {
            stmStore.SetSessionStatePersister(func(ctx context.Context, btree btree.BtreeInterface[string, any]) error {
                if a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
                    return nil
                }
                return a.service.session.Memory.Persist(ctx, btree)
            })
        }
    }
    
    return nil
}
```

### 4. Sleep Cycle Integration

**File**: `lib/sop/ai/agent/copilot_memory_workers.go`

```go
// In runSleepCycle() - Add session state cleanup
func runSleepCycle() {
    // ... existing STM → LTM consolidation ...
    
    // After consolidating episodes to LTM, prune expired session states
    if removed, err := PruneExpiredSessions(ctx, stm, time.Now()); err != nil {
        log.Warn("Failed to prune expired sessions", "error", err)
    } else if removed > 0 {
        log.Debug("Pruned expired session states", "count", removed)
    }
}
```

**Note**: Session state pruning happens during sleep cycle, but session state **is NOT moved to LTM**. It stays in STM with a 7-day TTL. Only episodes (tool executions) get consolidated to LTM.

### 5. Testing

**File**: `lib/sop/ai/agent/workstream_persistence_test.go` (new)

```go
func TestShortTermMemory_PersistAndRehydrate(t *testing.T) {
    // Test basic persist/rehydrate cycle
}

func TestShortTermMemory_RehydrateExpiredSession(t *testing.T) {
    // Test that expired sessions are ignored
}

func TestShortTermMemory_PersistPreservesPhaseArtifacts(t *testing.T) {
    // Test that phase artifacts survive persist/rehydrate
}

func TestShortTermMemory_PersistPreservesManualResumeState(t *testing.T) {
    // Test that manual resume flags survive
}

func TestShortTermMemory_PersistPreservesParentChildRelationships(t *testing.T) {
    // Test nested work streams
}

func TestPruneExpiredSessions(t *testing.T) {
    // Test cleanup of old sessions
}
```

---

## Benefits

### User Experience

✅ **Resume work after restart**
```
User (after restart): "Continue the auth bug investigation"
Agent: "Resuming investigation phase. I found 3 authentication flows in the codebase..."
```

✅ **Multi-day features**
```
Day 1: Investigate Redis patterns → Pause
Day 2: "Continue Redis work"
Agent: "Resuming planning phase. Based on yesterday's investigation, I recommend..."
```

✅ **Crash recovery**
```
[IDE crashes during execution]
User (after restart): "What was I working on?"
Agent: "You were in the execution phase of 'Add user profile API'. 
        I had analyzed 3 files and was implementing the handler..."
```

✅ **Manual resume across sessions**
```
User: "Pause this for now, work on payment bug"
[Next day after restart]
User: "What tasks did I pause?"
Agent: "You have 1 paused task: 'Auth bug investigation' (paused 1d ago)"
```

### Technical Benefits

✅ **Industry-standard behavior** - Matches VS Code, IntelliJ, ChatGPT  
✅ **Minimal performance impact** - Persist only on state changes (debounced)  
✅ **Graceful degradation** - Falls back to fresh session if rehydration fails  
✅ **Automatic cleanup** - Expired sessions pruned during sleep cycle  
✅ **Small storage footprint** - ~10-50 KB per session  
✅ **No LTM pollution** - Session state stays in STM, not moved to semantic memory  

---

## Migration Strategy

### Phase 1: Implement Core (1 day)
- Add `Persist()` and `Rehydrate()` methods to `ShortTermMemory`
- Add `PruneExpiredSessions()` function
- Add `sessionStatePersister` callback field to `ShortTermMemoryStore`
- Add `SetSessionStatePersister()` method
- Unit tests for serialize/deserialize

### Phase 2: Integration (0.5 days)
- Wire into `CopilotAgent.Initialize()` for rehydration
- Hook into `StartPeriodicCommitter()` for automatic persistence
- Modify periodic committer to call persister before commit

### Phase 3: Sleep Cycle Integration (0.25 days)
- Add `PruneExpiredSessions()` call to sleep cycle
- Ensure session state is NOT moved to LTM (episodes only)

### Phase 4: Testing & Validation (0.25 days)
- Integration tests
- Manual testing with VS Code restarts
- Performance validation (persist adds < 10ms to batch commit)

**Total Effort**: 2 days (unchanged, but simpler implementation)

---

## Advantages of Piggybacking on STM Commits

✅ **Reuses existing infrastructure** - No duplicate batching logic  
✅ **Same transaction** - Episodes + session state commit atomically  
✅ **No separate worker** - One less goroutine to manage  
✅ **Same 30-second cadence** - Consistent timing with episodes  
✅ **Simpler code** - Single commit point instead of multiple  
✅ **Better atomicity** - Session state and episodes always in sync  
✅ **Lower overhead** - One transaction instead of two  

---

## Storage Characteristics

**Key**: `"stm_session_state"`  
**Size**: 
- **Typical**: ~2 KB (1 work stream)
- **Heavy**: ~5-10 KB (3-5 streams)
- **Max**: ~20-50 KB (20 streams, rare)
**Write frequency**: Every 30 seconds (piggybacked on episode commits)  
**Read frequency**: Once per startup  
**TTL**: 7 days (configurable via `WorkStreamSessionTTL`)  
**Cleanup**: Automatic during sleep cycle  
**Transaction**: Shared with episode batch (atomic commit)  
**Overhead**: < 10ms added to episode batch commit

**No index needed**: Single key lookup, no range queries

---

## Risks & Mitigations

### Risk 1: Serialization failures
**Mitigation**: Graceful fallback to fresh session, log warning

### Risk 2: State corruption
**Mitigation**: Version field for migration, validation on rehydration

### Risk 3: Stale state confusion
**Mitigation**: Show timestamp in reminders ("paused 2d ago"), automatic expiration

### Risk 4: Performance impact
**Mitigation**: Debouncing (30s), async persistence, size limit checks

---

## Related Documentation

- [PLANNING_FIRST_IMPLEMENTATION_STATUS.md](./PLANNING_FIRST_IMPLEMENTATION_STATUS.md) - Phase 2.5
- [MANUAL_RESUME_FEATURE_GUIDE.md](./MANUAL_RESUME_FEATURE_GUIDE.md) - Manual resume UX
- [STM_WORKSTREAM_STACK_DESIGN.md](./STM_WORKSTREAM_STACK_DESIGN.md) - Work stream architecture
- [PLANNING_FIRST_LLM_ORCHESTRATION.md](./PLANNING_FIRST_LLM_ORCHESTRATION.md) - Core architecture

---

## Summary

**Problem**: Work streams lost on restart (not industry standard)  
**Solution**: Persist `ShortTermMemory` to STM B-tree  
**Effort**: 2 days  
**Impact**: Major UX improvement, enables multi-day features, crash recovery  
**Priority**: HIGH - Required for manual resume to work properly  
