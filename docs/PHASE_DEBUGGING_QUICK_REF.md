# Phase System Debugging - Quick Reference

**For Full Details**: See [AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md](./AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md)

---

## Common Error Patterns

### Path Doubling: `lib/sop/lib/sop/...`

```
❌ Error: no such file or directory: /Volumes/T7/sop-cloud/lib/sop/lib/sop/tools/sopbackend/auth.go
```

**Check**: `lib/sop/ai/agent/planner/phase_coding.go:557` (runReadFile)  
**Fix**: Strip workspace basename prefix before joining with working_dir

---

### Phase Desync: Wrong Phase in Logs

```
❌ Logs show:
INFO Resuming WorkStream current_phase=execution
INFO Transaction evaluation start current_phase=investigation  ← WRONG!
```

**Check**: `lib/sop/ai/agent/ask_pipeline.go:419` (phase resolution)  
**Fix**: Sync `askCtx.Phase = currentPhase` after `resolvePhasePlan()`

---

### Query Parsing False Positives

```
❌ LLM query: "find failing packages/tests"
✅ Extracts: "packages/tests" (WRONG - this is English, not a path)
✅ Runs: go test ./packages/tests (FAILS - directory doesn't exist)
```

**Check**: `lib/sop/ai/agent/codingworkflow/default_components.go:1244` (extractGoPackagePath)  
**Fix**: Use conservative patterns requiring explicit path indicators

---

### Tool Name Collisions

```
❌ Error: investigation_run_tests called but execution_run_tests registered
```

**Check**: `lib/sop/ai/agent/ask_pipeline.go:455` (registry clearing)  
**Fix**: Call `registry.Clear()` before `registerPhaseTools()`

---

## Debug Grep Commands

```bash
# Find path doubling
grep -r "lib/sop/lib/sop" translogs/

# Find phase desync
grep "Transaction evaluation start" translogs/ | grep -B5 "current_phase"

# Find query parsing issues
grep "extractGoPackagePath\|extractGoTestPattern" translogs/

# Find tool registration issues
grep "Registry cleared\|Registering phase tools" translogs/

# Find infinite re-enqueuing
grep "Enqueued next phase" translogs/ | uniq -c
```

---

## Quick Validation

**After fixing bugs, validate with**:

```bash
# 1. Rebuild daemon
cd /Volumes/T7/sop-cloud/lib/sop/cmd/sop-daemon
go build

# 2. Test with "fix failing tests"
# Check logs for:
✅ No path doubling errors
✅ Phase matches in transaction evaluation
✅ No false positive package extraction
✅ Registry cleared before tool registration
✅ Clean tool names (no phase prefixes)
✅ Autonomous progression (investigation → execution)
```

---

## Key Architecture Rules

From `.github/copilot-instructions.md`:

### 1. NEVER Use context.Background()
```go
// ❌ WRONG - strips ai.CtxKeyEventStreamer
func executeCommand(ctx context.Context, cmd string) {
    ctx = context.Background()  // Terminal UI breaks!
    result, _ := localCommandExecutor(ctx, request)
}

// ✅ CORRECT - preserve context chain
func executeCommand(ctx context.Context, cmd string) {
    result, _ := localCommandExecutor(ctx, request)
}
```

### 2. Participant Pattern Only
```go
// ❌ WRONG - capability as controller
func Execute(phases []string) {
    for _, phase := range phases {
        executePhase(phase)  // DON'T LOOP!
    }
}

// ✅ CORRECT - capability as participant
func ExecuteTool(ctx, toolName, args, state) {
    // 1. Do work for THIS tool
    captureArtifact(args)
    
    // 2. Enqueue NEXT phase (one item only)
    queue.Enqueue(WorkItem{Actor: "phase:execution"})
    
    // 3. Return - Ask loop processes next
    return "Complete", nil
}
```

### 3. Explicit Parameters
```go
// ❌ WRONG - state in context
ctx = context.WithValue(ctx, "workstream", workstream)
func ExecuteTool(ctx context.Context) {
    ws := ctx.Value("workstream")  // Anti-pattern!
}

// ✅ CORRECT - explicit parameters
func ExecuteTool(ctx context.Context, workstream *WorkStreamContext, queue *WorkQueue) {
    // Direct access
}
```

---

## Debugging Workflow

1. **Reproduce** with `"fix failing tests"` query
2. **Check daemon logs** in `translogs/` for error patterns above
3. **Identify bug** using grep commands
4. **Apply fix** from Debug Log
5. **Rebuild** daemon: `go build`
6. **Validate** with Quick Validation checklist
7. **Document** any new patterns in Debug Log

---

## Related Files

**Core Implementation**:
- `lib/sop/ai/agent/ask_pipeline.go` - Phase resolution & tool registration
- `lib/sop/ai/agent/planner/phase_coding.go` - Coding workflow phase implementation
- `lib/sop/ai/agent/codingworkflow/default_components.go` - Test runner & query parsing
- `lib/sop/ai/agent/cognitive_layer.go` - Transaction evaluation

**Configuration**:
- `.github/copilot-instructions.md` - Architectural patterns & rules

**Documentation**:
- [AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md](./AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md) - Full bug history
- [PHASE_SYSTEM_ARCHITECTURE.md](./PHASE_SYSTEM_ARCHITECTURE.md) - Phase system overview
- [CLI_CODING_WORKFLOW_IMPLEMENTATION.md](./CLI_CODING_WORKFLOW_IMPLEMENTATION.md) - Coding workflow details
