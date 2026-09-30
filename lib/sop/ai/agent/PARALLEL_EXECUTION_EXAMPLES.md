# Phase 4: Parallel Execution - Usage Examples

## Basic Usage

### Example 1: Simple Parallel Tasks

```go
import (
    "context"
    "github.com/sharedcode/sop/ai"
    "github.com/sharedcode/sop/ai/agent"
)

func main() {
    agent := agent.NewCopilotAgent()
    cfg := ai.NewConfigMap()
    
    // This query will be split into 2 parallel threads
    result, err := agent.AskParallel(
        context.Background(),
        "run tests and check formatting",
        cfg,
    )
    
    // Output:
    // ✅ **Parallel Execution Complete**
    //
    // ### Task 1: run tests
    // Tests passed ✓
    //
    // ---
    //
    // ### Task 2: check formatting
    // Formatting OK ✓
}
```

### Example 2: Multiple Parallel Tasks

```go
// Comma-separated tasks with "and"
result, err := agent.AskParallel(
    ctx,
    "run tests, check formatting, and build docs",
    cfg,
)

// This splits into 3 parallel threads:
// - Thread 0 (Main/PTY): "run tests"
// - Thread 1 (Background): "check formatting"
// - Thread 2 (Background): "build docs"
```

### Example 3: Explicit Parallel Marker

```go
// Using || as explicit parallel marker
result, err := agent.AskParallel(
    ctx,
    "run tests || lint code || build production",
    cfg,
)

// Splits into 3 parallel threads
```

### Example 4: Fallback to Sequential

```go
// Non-parallelizable queries fall back to regular Ask()
result, err := agent.AskParallel(
    ctx,
    "what is the weather today",
    cfg,
)

// Executes as single-thread (sequential)
```

## Thread Routing

### Main Thread (Index 0)
- **Process**: PTY child process
- **Characteristics**:
  - Preserves shell state (cd, env vars)
  - SessionID maintained
  - Stateful execution
  - Background flag: false

### Background Threads (Index > 0)
- **Process**: exec.Command goroutines
- **Characteristics**:
  - Independent execution
  - SessionID stripped (empty)
  - Stateless execution
  - Background flag: true (forced)

## Query Splitting Patterns

### Pattern 1: Comma + "and"
```
"run tests, check formatting, and build docs"
→ ["run tests", "check formatting", "build docs"]
```

### Pattern 2: Simple "and"
```
"run tests and check formatting"
→ ["run tests", "check formatting"]
```

### Pattern 3: Explicit Marker ||
```
"run tests || check formatting"
→ ["run tests", "check formatting"]
```

### Pattern 4: Non-parallelizable
```
"what is the weather"
→ ["what is the weather"]  // Falls back to Ask()
```

## Context-Based Routing

### Implementation in Tool Execution

```go
// In runLocalAutomationCommand()
sessionID := sessionIDFromContext(ctx)

// Check if parallel execution mode
if isParallelExecution(ctx) {
    background = true
    sessionID = "" // Strip session to route to exec.Command
}

request := LocalCommandRequest{
    SessionID:  sessionID,
    Background: background,
    Command:    command,
}
```

### Daemon Routing Logic

```go
// In runLocalCommandWithWorkingDir()
if !req.Background && sessionID != "" {
    // Main thread → PTY child process
    return shellMgr.Run(sessionID, command, timeout)
}

if req.Background {
    // Background thread → exec.Command goroutine
    go runBackgroundJob(ctx, job, req)
    return jobID, nil
}
```

## Result Aggregation

Results from all parallel threads are combined with headers:

```
✅ **Parallel Execution Complete**

### Task 1: run tests
Tests passed ✓
All 42 tests successful

---

### Task 2: check formatting
Formatting OK ✓
All files properly formatted

---

### Task 3: build docs
Documentation built successfully ✓
Generated 156 pages
```

### Error Handling

If any thread fails, the error is included in the aggregated result:

```
✅ **Parallel Execution Complete**

### Task 1: run tests
Tests passed ✓

---

### Task 2: check formatting
❌ **Error**: formatting check failed: 3 files need formatting

---

### Task 3: build docs
Documentation built successfully ✓

⚠️ *Some tasks failed during parallel execution*
```

## Queue Isolation

Each `Ask()` call creates its own isolated queue:

```go
// Thread 1 (Main)
Ask(ctx, "run tests")
  → queue1 := NewWorkQueue()  // NEW queue
  → processQueueLoop(queue1)

// Thread 2 (Background)
Ask(bgCtx, "check formatting")
  → queue2 := NewWorkQueue()  // DIFFERENT queue
  → processQueueLoop(queue2)
```

**No shared state:**
- ✅ Each queue has its own mutex
- ✅ Queues never interact
- ✅ No race conditions
- ✅ Results returned independently

## Performance Benefits

### Sequential Execution (Before)
```
run tests          → 30 seconds
check formatting   → 10 seconds
build docs         → 20 seconds
─────────────────────────────────
Total:               60 seconds
```

### Parallel Execution (After)
```
run tests          → 30 seconds  ┐
check formatting   → 10 seconds  │ Parallel
build docs         → 20 seconds  ┘
─────────────────────────────────
Total:               30 seconds (2x speedup)
```

## Implementation Details

- **Code Added**: ~170 lines
  - `AskParallel()`: ~40 lines
  - `detectParallelSubqueries()`: ~60 lines
  - `aggregateParallelResults()`: ~40 lines
  - `isParallelExecution()`: ~5 lines
  - Context routing: ~10 lines

- **Tests**: 140 lines
  - Query splitting tests
  - Result aggregation tests
  - Context flag tests
  - Fallback behavior tests

- **Zero Modifications**: 
  - ✅ `Ask()` unchanged
  - ✅ `processQueueLoop()` unchanged
  - ✅ Queue infrastructure unchanged
  - ✅ Tool execution unchanged (except routing)

## Future Enhancements

### Phase 4.2: KB Workflow Integration
```yaml
# Workflow with parallel steps
workflow:
  name: code_quality
  parallel: true
  steps:
    - query: "run tests"
    - query: "check formatting"
    - query: "run linter"
```

### Phase 4.3: Dependency Resolution
```go
// Use existing WorkItem.DependsOn field
queue.Enqueue(WorkItem{
    ID: "task-c",
    Query: "build production",
    DependsOn: []string{"task-a", "task-b"},
})
```

### Phase 4.4: Production Hardening
- Rate limiting (max concurrent LLM calls)
- Timeout per thread
- Partial failure handling
- Metrics collection

## References

- [Parallel PTY Design](../../../../docs/DUAL_PTY_PARALLEL_EXECUTION.md)
- [Ask/agent architecture](../README.md)
- [Parallel PTY execution](../../../../docs/DUAL_PTY_PARALLEL_EXECUTION.md)
- [Shell Session Persistence](../../../../docs/SHELL_SESSION_PERSISTENCE.md)
