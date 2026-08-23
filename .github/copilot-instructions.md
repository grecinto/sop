# SOP Cloud Copilot Instructions

## Critical Architectural Patterns

### 1. Parameter Passing: Explicit, Not Context

**❌ DO NOT** use `context.Context` for application state or parameter passing:
```go
// WRONG - state in context
ctx = context.WithValue(ctx, "workstream", workstream)
func ExecuteTool(ctx context.Context) {
    ws := ctx.Value("workstream")  // ❌ Anti-pattern
}
```

**✅ DO** pass state explicitly as parameters:
```go
// CORRECT - explicit parameters
func ExecuteTool(ctx context.Context, workstream *WorkStreamContext, queue *WorkQueue) {
    // Direct access ✅
}
```

**Context is only for:**
- Cancellation signals (`ctx.Done()`)
- Request-scoped values (trace IDs, request IDs)
- Deadlines/timeouts
- **Infrastructure context** (event streamers, session payloads)

**⚠️ CRITICAL: NEVER create `context.Background()` in function implementations**
```go
// ❌ WRONG - loses event streamer and session context
func executeCommand(ctx context.Context, cmd string) {
    ctx = context.Background()  // ❌ Strips ai.CtxKeyEventStreamer!
    result, _ := localCommandExecutor(ctx, request)  // Command won't show in chat
}

// ✅ CORRECT - preserve the context chain
func executeCommand(ctx context.Context, cmd string) {
    result, _ := localCommandExecutor(ctx, request)  // Preserves event streamer
}
```

**Why this matters:**
- `ai.CtxKeyEventStreamer` carries the chat display streamer
- `emitTerminalCommand()` needs this to show commands to users
- Creating `context.Background()` breaks Terminal UI and chat history display

### 2. Ask Domain: Participant Pattern Only

**Core Principle:** The Ask pipeline (`copilot.go`'s `Ask()` function) is the **ONLY controller**. Capabilities are **passive participants**.

#### ✅ Correct: Participant Pattern

See `capabilities/capability.go` for the interface and `copilot.go`'s `Ask()` function for usage.

**Capabilities respond to requests via:**
- `Classify()` - Is this capability relevant?
- `BuildContext()` - Gather context
- `ExposeTools()` - Declare available tools
- `ExecuteTool()` - Execute a single tool

**Capabilities enqueue next work:**
```go
// In a tool like phase_capture
func (c *PhaseCapability) ExecuteTool(ctx, toolName, args, state) {
    // 1. Do work for THIS phase
    captureArtifact(args)
    
    // 2. Enqueue NEXT phase (one item only)
    queue.Enqueue(WorkItem{
        Actor: "phase:planning",
        Query: originalQuery,
    })
    
    // 3. Return - Ask loop will process next phase
    return "Phase complete", nil
}
```

**Ask pipeline controls execution:**
```go
// In copilot.go Ask()
func (a *CopilotAgent) Ask(ctx, query, cfg) {
    queue := a.getOrCreateSessionQueue()
    queue.Enqueue(WorkItem{Query: query, Actor: "user"})
    
    for !queue.IsEmpty() {
        item := queue.Dequeue()
        response := a.ask(ctx, item.Query, cfg)  // Full ReAct turn
        // Capabilities may have enqueued more work
    }
}
```

#### ❌ Anti-Pattern: Controller Pattern

**NEVER implement these patterns in capabilities:**

```go
// ❌ WRONG - Loop executing multiple steps
func Execute(phases []string) {
    for _, phase := range phases {
        executePhase(phase)  // Controller pattern!
    }
}

// ❌ WRONG - Run method that drives flow
func Run(workflow Workflow) {
    for _, step := range workflow.Steps {
        step.Execute()  // Controller pattern!
    }
}

// ❌ WRONG - Enqueueing all steps upfront
func Start(phases []string) {
    for _, phase := range phases {
        queue.Enqueue(phase)  // Batch enqueue!
    }
}
```

**Why this matters:**
- Ask pipeline is the single source of control
- Each phase = separate Ask turn with full LLM reasoning
- User can interject between phases
- State is clean between turns
- Capabilities can't deadlock or infinite loop

## Design Documentation

See these docs for architectural details:
- `docs/ARCHITECTURE_PATTERNS.md` - Complete guide to Participant Pattern
- `docs/PLANNING_FIRST_LLM_ORCHESTRATION.md` - Phase succession design
- `docs/WORKFLOWS.md` - Workflow architecture evolution
- `lib/sop/ai/agent/capabilities/capability.go` - Capability interface
- `lib/sop/ai/agent/copilot.go` - Ask pipeline implementation (lines 2664-2710)
