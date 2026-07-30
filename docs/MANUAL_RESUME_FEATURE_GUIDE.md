# Manual Resume for Paused Work Streams - Feature Guide

**Status**: ✅ Implemented (with known limitation)  
**Last Updated**: 2026-07-29

---

## ⚠️ Known Limitation: Session Persistence

**Current State**: Work streams are **in-memory only** and **lost on restart**

**Impact**:
- ❌ Paused work streams don't survive application/IDE restart
- ❌ Manual resume only works within active session
- ❌ If you close VS Code, all paused work is lost
- ❌ Multi-day features lose context on restart

**Planned Fix**: Phase 2.5 (Work Stream Persistence) - See [PLANNING_FIRST_IMPLEMENTATION_STATUS.md](./PLANNING_FIRST_IMPLEMENTATION_STATUS.md#phase-25-work-stream-persistence--not-started-2-days)

**Workaround**: Use manual resume for within-session pause/resume only. For long-term work, capture context in comments or notes before closing session.

---

## Overview

When a user interjects and pauses current work, the system can now mark that work stream to require **manual resume**. This prevents auto-resume when nested streams complete, giving users full control over when paused work continues.

**⚠️ Important**: This feature currently works **only within an active session**. Paused streams are lost when the application restarts.

---

## Key Features

### 1. Manual Resume Marker

When pausing a work stream, you can specify if manual resume is required:

```go
thread.PauseWorkStreamWithReason("Working on payment bug first", true)
//                                                                 ^^^^
//                                                   requireManualResume = true
```

**What it does**:
- Sets `RequiresManualResume` flag in WorkStreamContext
- Prevents auto-resume when child streams complete
- User must explicitly say "resume" to continue

### 2. Auto-Resume Detection

Check if a paused stream can be automatically resumed:

```go
if thread.CanAutoResume() {
    // This stream can auto-resume when nested work completes
} else {
    // User must manually resume this stream
}
```

**Decision logic**:
- ✅ **Auto-resume allowed**: `RequiresManualResume = false`
- ❌ **Manual resume required**: `RequiresManualResume = true`

### 3. Paused Stream Queries

Get all paused streams or just those needing manual resume:

```go
// All paused streams (auto + manual)
allPaused := stm.GetPausedWorkStreams()

// Only streams requiring manual resume
manualOnly := stm.GetManualResumeWorkStreams()
```

### 4. User-Friendly Reminders

Generate a formatted reminder about paused work:

```go
reminder := stm.FormatPausedWorkStreamsReminder()
// Returns:
// 📌 **Paused Work Streams** (require manual resume):
//    1. **Auth Bug Fix** (paused in investigation phase) - User requested to work on payments first 2m ago
//    2. **Redis Cache** (paused in planning phase) - Waiting for design review 1h ago
//
// 💡 Use 'resume [number]' to continue a paused task
```

---

## Usage Patterns

### Pattern 1: User Explicitly Pauses Work

```go
// User says: "pause this and let me work on the payment bug"

// Mark current stream as paused with manual resume required
currentStream.PauseWorkStreamWithReason(
    "User requested to work on payment bug first",
    true, // Requires manual resume
)

// Create new stream for payment bug work
newStream := createNewWorkStream("Fix payment processing bug")
newStream.StreamContext.ParentStreamID = currentStream.ID
newStream.StreamContext.InterjectionType = InterjectionPauseNew
```

### Pattern 2: Quick Interruption (Auto-Resume OK)

```go
// User says: "quick question about the database schema"

// Pause current stream but allow auto-resume
currentStream.PauseWorkStreamWithReason(
    "Quick schema question",
    false, // Auto-resume allowed
)

// Answer question
// When done, automatically resume paused stream
if currentStream.CanAutoResume() {
    currentStream.ResumeWorkStream()
}
```

### Pattern 3: User Asks About Paused Work

```go
// User says: "what was I working on?"

reminder := stm.FormatPausedWorkStreamsReminder()
if reminder != "" {
    return reminder // Show paused streams
} else {
    return "No paused work streams"
}
```

### Pattern 4: User Resumes Specific Work

```go
// User says: "resume the auth bug fix"

manualStreams := stm.GetManualResumeWorkStreams()

// Find matching stream (by label or number)
for _, stream := range manualStreams {
    if strings.Contains(strings.ToLower(stream.Label), "auth bug") {
        stream.ResumeWorkStream()
        // Continue execution from where it left off
        break
    }
}
```

---

## API Reference

### ConversationThread Methods

#### `PauseWorkStreamWithReason(reason string, requireManualResume bool)`
Pauses the work stream with a reason and manual resume control.

**Parameters**:
- `reason` - Why the stream was paused (shown in reminders)
- `requireManualResume` - If true, prevents auto-resume

**Effects**:
- Sets `StreamState = WorkStreamPaused`
- Records `PausedAt` timestamp
- Sets `RequiresManualResume` flag
- Stores `PauseReason`

---

#### `CanAutoResume() bool`
Checks if a paused stream can be automatically resumed.

**Returns**:
- `true` - If paused and auto-resume allowed
- `false` - If not paused or requires manual resume

---

#### `IsWaitingForManualResume() bool`
Checks if stream is paused and waiting for explicit user resume.

**Returns**:
- `true` - If paused AND requires manual resume
- `false` - Otherwise

---

#### `ResumeWorkStream()`
Marks a paused work stream as active again.

**Effects**:
- Sets `StreamState = WorkStreamActive`
- Clears `PausedAt` timestamp
- Clears `RequiresManualResume` flag
- Clears `PauseReason`

---

### ShortTermMemory Methods

#### `GetPausedWorkStreams() []*ConversationThread`
Returns ALL paused threads (both auto-resume and manual-resume).

**Returns**: Array of paused ConversationThread objects

---

#### `GetManualResumeWorkStreams() []*ConversationThread`
Returns ONLY threads that are paused and require manual resume.

**Returns**: Array of ConversationThread objects waiting for manual resume

---

#### `FormatPausedWorkStreamsReminder() string`
Creates a user-friendly formatted reminder about paused work streams.

**Returns**: 
- Formatted reminder string with stream details
- Empty string if no paused streams requiring manual resume

**Format includes**:
- Stream label or root prompt
- Current phase (where it was paused)
- Pause reason
- Time since pause (e.g., "2m ago", "1h ago", "3d ago")
- Resume instructions

---

## WorkStreamContext Fields

### New Fields

```go
type WorkStreamContext struct {
    // ... existing fields ...
    
    // Manual Resume Control
    RequiresManualResume bool   `json:"requires_manual_resume"` // Prevents auto-resume
    PauseReason          string `json:"pause_reason"`           // Why paused (for reminders)
}
```

---

## Integration Example

```go
// In the Ask pipeline when detecting user interjection

func handleUserInterjection(query string, currentStream *ConversationThread) {
    // Classify interjection type
    interjectionType := classifyInterjection(query)
    
    switch interjectionType {
    case "pause_new":
        // User explicitly wants to pause
        // Examples: "pause this", "hold on", "let me work on X first"
        
        reason := extractPauseReason(query)
        currentStream.PauseWorkStreamWithReason(reason, true) // Manual resume required
        
        // Create new work stream
        newStream := createWorkStreamFromQuery(query)
        newStream.StreamContext.ParentStreamID = currentStream.ID
        newStream.StreamContext.InterjectionType = InterjectionPauseNew
        
        return "Paused current work. Starting new task. I'll remind you about the paused work when you're ready."
        
    case "clear_restart":
        // User wants fresh start
        // Examples: "forget that", "start over", "new task"
        
        currentStream.PauseWorkStreamWithReason("User started fresh task", true)
        return "Cleared previous work. Starting fresh."
        
    case "refine":
        // User wants to stay in current stream but adjust
        // Examples: "wait, let's do X first", "actually investigate before planning"
        
        // Don't pause, just adjust phase
        return "Adjusting current approach..."
        
    case "query":
        // Quick question - auto-resume OK
        
        currentStream.PauseWorkStreamWithReason("Quick question", false) // Auto-resume allowed
        // Answer question
        // Auto-resume happens when done
    }
}

// Completion handler for child streams
func onStreamComplete(childStream *ConversationThread) {
    if childStream.StreamContext.ParentStreamID != nil {
        parentStream := findStream(childStream.StreamContext.ParentStreamID)
        
        if parentStream != nil && parentStream.CanAutoResume() {
            // Automatically resume parent
            parentStream.ResumeWorkStream()
            continueExecution(parentStream)
        } else if parentStream != nil && parentStream.IsWaitingForManualResume() {
            // Show reminder about paused work
            reminder := stm.FormatPausedWorkStreamsReminder()
            showToUser(reminder)
        }
    }
}
```

---

## User Experience Flow

### Scenario 1: Explicit Pause

```
User: "I'm working on the auth bug, but pause this and let me fix the payment bug first"

Agent: 
✅ Paused: Auth Bug Fix (investigation phase)
📝 Reason: Working on payment bug first
🔄 Starting: Payment Bug Fix

[... work on payment bug ...]

Agent (after payment bug complete):
📌 **Paused Work Streams** (require manual resume):
   1. **Auth Bug Fix** (paused in investigation phase) - Working on payment bug first 15m ago

💡 Use 'resume [number]' to continue a paused task

User: "resume 1"

Agent:
✅ Resuming: Auth Bug Fix (investigation phase)
📍 Continuing from where we left off...
```

### Scenario 2: Quick Interruption

```
User: "Wait, what's the database schema for users?"

Agent:
[Answers question about schema]
✅ Automatically resuming previous work (auth bug investigation)
```

---

## Testing

Run the comprehensive test suite:

```bash
cd /Volumes/T7/sop-cloud/lib/sop/ai/agent
go test -v -run 'TestPause|TestCanAutoResume|TestIsWaiting|TestResume|TestGetManual|TestGetPaused|TestFormat' .
```

**Test Coverage**:
- ✅ Pause with manual resume
- ✅ Pause with auto-resume
- ✅ CanAutoResume logic
- ✅ IsWaitingForManualResume detection
- ✅ ResumeWorkStream clearing
- ✅ GetManualResumeWorkStreams filtering
- ✅ GetPausedWorkStreams retrieval
- ✅ FormatPausedWorkStreamsReminder formatting
- ✅ Time-since-pause display

**All tests passing** ✅

---

## Future Enhancement: Work Stream Persistence

**Planned**: Phase 2.5 (2 days of work)

### What Will Be Added

**B-tree Persistence**:
```go
// Save session state periodically
stm.Persist(ctx, btree)  // Saves all threads, order, states

// Restore on startup
stm.Rehydrate(ctx, btree)  // Loads saved work streams
```

**When persisted**:
- Phase transitions (investigation → planning)
- Work stream pause/resume
- Phase artifact updates
- Every 30 seconds (debounced)

**What will be preserved**:
- ✅ Paused work streams with manual resume flags
- ✅ Phase progression and succession paths
- ✅ Phase artifacts (findings, plans, test results)
- ✅ Parent-child work stream relationships
- ✅ Loop detection state
- ✅ Pause reasons and timestamps

**TTL**: 7 days (configurable)

### Benefits

Once implemented:
- ✅ Resume work after restart - "Continue auth bug investigation from yesterday"
- ✅ Multi-day features - Long-running work preserves context
- ✅ Crash recovery - Work-in-progress isn't lost
- ✅ Manual resume works across sessions - Paused work survives restarts
- ✅ Industry-standard UX - Matches VS Code, IntelliJ behavior

---

## Related Documentation

- [PLANNING_FIRST_LLM_ORCHESTRATION.md](./PLANNING_FIRST_LLM_ORCHESTRATION.md) - Core architecture
- [STM_WORKSTREAM_STACK_DESIGN.md](./STM_WORKSTREAM_STACK_DESIGN.md) - Work stream stack design
- [PLANNING_FIRST_IMPLEMENTATION_STATUS.md](./PLANNING_FIRST_IMPLEMENTATION_STATUS.md) - Implementation progress

---

## Summary

**What was added**:
- ✅ `RequiresManualResume` flag in WorkStreamContext
- ✅ `PauseReason` field for user reminders
- ✅ `PauseWorkStreamWithReason()` - Pause with control
- ✅ `CanAutoResume()` - Check auto-resume eligibility
- ✅ `IsWaitingForManualResume()` - Check manual resume state
- ✅ `ResumeWorkStream()` - Explicit resume
- ✅ `GetManualResumeWorkStreams()` - Query paused work
- ✅ `FormatPausedWorkStreamsReminder()` - User-friendly reminders

**Benefits**:
- 🎯 User has full control over work resumption
- 📌 System remembers WHY work was paused
- ⏰ Users see how long work has been paused
- 🔄 Auto-resume still works for quick interruptions
- 💡 Clear reminders help users remember paused tasks
