# Autonomous Test Fixing - Debug Log & Solutions

**Created**: 2026-08-29  
**Status**: ⚠️ Workaround Applied - Context Trimming Issue Discovered  
**Related**: [CONTEXT_GENERATION_AUDIT.md](./CONTEXT_GENERATION_AUDIT.md), [CONTEXT_GENERATION_REFORM.md](./CONTEXT_GENERATION_REFORM.md), [CLI_CODING_WORKFLOW_IMPLEMENTATION.md](./CLI_CODING_WORKFLOW_IMPLEMENTATION.md)

---

## Latest Discovery: Context Budget Trimming (2026-08-29 23:45)

### Problem
LLM completes all investigation correctly but **never generates patches** in execution phase:
- ✅ Runs tests successfully (38 failures detected)
- ✅ Reads test files completely  
- ✅ Reads implementation files completely
- ✅ Searches for patterns correctly
- ❌ Never calls `preview_patch` or `apply_patch` tools
- ❌ Stays in investigation mode for all 3 execution iterations

### Root Cause: Aggressive Context Trimming

**Log Evidence**:
```
INFO LLM Context Budget
  routing_gate=discovery
  original_chars=14288
  final_chars=7184
  trimmed_components=focused_execution_context:9704->2600
```

**Analysis**:
- Execution phase `BuildContext()` returns ~650 chars with "CRITICAL: IMMEDIATELY generate patches"
- But that gets concatenated with 8+ other context sources → 9704 total chars
- Budget trimmer cuts to 2600 chars (73% loss)
- **Critical directive likely removed**, LLM never sees it
- LLM sees tools (not trimmed) but not phase instructions (trimmed)

**Why 9704 chars generated?** Blind concatenation of:
1. askOutcome (~500 chars)
2. taskGuidance (~300 chars)  
3. **plannerContext (~1200 chars)** ← Not needed in execution!
4. caveatContext (~800 chars)
5. generalContext (0 - has domain)
6. spaceGuidance (0 - not spaces)
7. **researchGuidance (~2000 chars)** ← Not needed for "fix tests"!
8. memoryRecall (~1500 chars)
9. **phase.BuildContext() (~650 chars)** ← THIS MATTERS!
10. Others (~2754 chars)

**Waste**: ~70% of generated context irrelevant to execution phase

### Workaround Applied

**Changed tool descriptions** (not trimmed) to include directives:
```go
// lib/sop/ai/agent/planner/phase_coding.go:320-330
preview_patch: "**REQUIRED FIRST STEP**: When you have test failures AND source 
context, generate a patch immediately. Use this NOW if you've read test files 
and implementation files."
```

Tool descriptions live in `system_tools` component (not aggressively trimmed), so LLM sees them.

### Proper Fix Required

**Documentation Created**:
- [CONTEXT_GENERATION_AUDIT.md](CONTEXT_GENERATION_AUDIT.md) - Current state analysis
- [CONTEXT_GENERATION_REFORM.md](CONTEXT_GENERATION_REFORM.md) - Refactoring plan

**Key Principles**:
1. **Generate only relevant context** - Don't concatenate everything
2. **No trimming needed** - Stay under budget naturally  
3. **Conditional generation** - Phase-aware context inclusion
4. **No duplication** - Single source of truth per concept

**Timeline**: 3-4 weeks for proper refactoring

---

## Overview

This document tracks the debugging journey for autonomous LLM-orchestrated test fixing through the Ask/ReAct pipeline with phase-based tool orchestration. The goal was to enable the LLM to autonomously progress through: **investigation → planning → execution** without manual intervention.

**Key Architecture**: See `.github/copilot-instructions.md` for Participant Pattern and explicit parameter passing rules.

---

## Critical Bugs Fixed (Aug 2026)

### 1. ✅ Query Parsing False Positives

**Date**: 2026-08-29  
**File**: `lib/sop/ai/agent/codingworkflow/default_components.go:1244`

#### Problem
```go
// ❌ TOO PERMISSIVE - extracts from English text
re := regexp.MustCompile(`\b([a-z][a-z0-9/_-]*(?:/[a-z][a-z0-9_-]*)+)\b`)
```

**Symptom**: 
- LLM query: "Run broad test to find failing **packages/tests**" (meaning "test cases")
- Regex extracts: `packages/tests` (interpreted as directory path)
- Result: `go test ./packages/tests` fails (directory doesn't exist)
- LLM hallucinates repository structure trying to read `packages/tests/package.json`

#### Solution
```go
// ✅ CONSERVATIVE - only matches explicit package indicators
patterns := []string{
    `(?:in|test|package)\s+([a-z][a-z0-9/_-]+(?:/[a-z][a-z0-9_-]+)+)`,  // "in package tools/sopbackend"
    `go\s+test\s+(?:\./)?([a-z][a-z0-9/_-]+(?:/[a-z][a-z0-9_-]+)+)`,     // "go test tools/sopbackend"
    `\./([a-z][a-z0-9/_-]+(?:/[a-z][a-z0-9_-]+)+)`,                      // "./tools/sopbackend"
    `\b([a-z]+/[a-z]+/[a-z0-9_-]+)\b`,                                    // "cmd/daemon/main" (3+ segments)
}
```

**Lesson**: Query parsing must distinguish between:
- ✅ Explicit package paths: `"in tools/sopbackend"`, `"./cmd/myapp"`
- ❌ English descriptions: `"failing packages/tests"`, `"report packages/tests"`

---

### 2. ✅ Path Doubling Bug

**Date**: 2026-08-29  
**File**: `lib/sop/ai/agent/planner/phase_coding.go:557`

#### Problem
```go
// ❌ NAIVE JOIN - causes path doubling
absPath = filepath.Join(task.WorkingDir, filePath)
// Example:
//   WorkingDir: "/Volumes/T7/sop-cloud/lib/sop"
//   filePath:   "lib/sop/tools/sopbackend/auth.go" (from search_repo)
//   Result:     "/Volumes/T7/sop-cloud/lib/sop/lib/sop/tools/sopbackend/auth.go" ❌
```

**Root Cause**: 
- `search_repo` returns **workspace-relative paths**: `lib/sop/tools/sopbackend/auth.go`
- LLM copies these paths to `read_file` tool
- `working_dir` already contains `lib/sop` → double prefix when joining

**Error Message**:
```
read lib/sop/tools/sopbackend/types.go: open /Volumes/T7/sop-cloud/lib/sop/lib/sop/tools/sopbackend/types.go: no such file or directory
```

#### Solution
```go
// ✅ STRIP WORKSPACE PREFIX - prevents doubling
workDirBase := filepath.Base(task.WorkingDir)  // "lib/sop"
if strings.HasPrefix(filePath, workDirBase+string(filepath.Separator)) {
    filePath = strings.TrimPrefix(filePath, workDirBase+string(filepath.Separator))
}
absPath = filepath.Join(task.WorkingDir, filePath)
// Result: "/Volumes/T7/sop-cloud/lib/sop/tools/sopbackend/auth.go" ✅
```

**Lesson**: Search results and path arguments may contain workspace-relative prefixes that duplicate working_dir. Always normalize before joining.

---

### 3. ✅ Phase Desynchronization

**Date**: 2026-08-29  
**File**: `lib/sop/ai/agent/ask_pipeline.go:419`

#### Problem
```go
// ❌ STALE CLASSIFICATION - transaction evaluator sees wrong phase
plan, currentPhase := a.resolvePhasePlan(ctx, askCtx)
// currentPhase correctly resolved to "execution" from queue actor
// BUT askCtx.Phase still contains "investigation" (stale value)
```

**Symptom** (from daemon logs):
```
INFO Resuming WorkStream from existing PhasePlan current_phase=execution
INFO Transaction evaluation start current_phase=investigation  ❌ WRONG!
INFO Phase transaction decision next_phase=execution           ❌ RE-ENQUEUES SAME PHASE
INFO Enqueued next phase target_actor=phase:execution         ❌ INFINITE LOOP RISK
```

**Root Cause**:
1. `resolvePhasePlan()` correctly reads queued phase actor: `currentPhase = "execution"`
2. Creates phase proxies with `currentPhase`
3. **But**: `taskContext.AskClassification.Phase` never updated
4. Transaction evaluator reads stale `classification.Phase = "investigation"`
5. Re-enqueues execution phase (thinking it's still in investigation)

#### Solution
```go
// ✅ SYNC CLASSIFICATION - keep transaction evaluator aligned
plan, currentPhase := a.resolvePhasePlan(ctx, askCtx)
log.Info("Phase plan resolved", "plan", plan, "current_phase", currentPhase)

// CRITICAL: Update classification with actual resolved phase
if currentPhase != "" && currentPhase != askCtx.Phase {
    log.Info("Updating classification phase for transaction evaluator",
        "old_phase", askCtx.Phase,
        "resolved_phase", currentPhase)
    askCtx.Phase = currentPhase
}
```

**Lesson**: When phase succession uses queue actors to override classification, **always sync the classification back** before transaction evaluation. Otherwise evaluator operates on stale state.

---

## Historical Bugs (Previously Fixed)

### ✅ Cognitive Layer Tuning (Aug 2026)
- **Problem**: Investigation phase required 3+ tool calls before LLM evaluation
- **Solution**: Modified `shouldEvaluate()`: `(currentPhase == "investigation" && len(toolCalls) >= 1) || len(toolCalls) >= 3`
- **File**: `lib/sop/ai/agent/cognitive_layer.go`

### ✅ Universal Tools Missing (Aug 2026)
- **Problem**: NEW CodingPhase missing run_tests, read_file in investigation
- **Solution**: Added `getCodingUniversalTools()` exposing 7 tools to ALL phases
- **File**: `lib/sop/ai/agent/planner/phase_coding.go:156`

### ✅ Phase Prefix Exposure (Aug 2026)
- **Problem**: LLM saw `investigation_run_tests` vs `execution_run_tests` (internal orchestration leaked)
- **Solution**: `phaseToolName()` returns clean suffix: `"run_tests"` (no prefix)
- **File**: `lib/sop/ai/agent/planner/phase_base.go:311`

### ✅ Registry Clearing (Aug 2026)
- **Problem**: Tool name collisions between phase transitions
- **Solution**: Call `registry.Clear()` before `registerPhaseTools()`
- **File**: `lib/sop/ai/agent/ask_pipeline.go:455`

### ✅ Test Result Preprocessing (Aug 2026)
- **Problem**: Raw test output lacked structure
- **Solution**: `ParseTestResult()` detects format, extracts failures, adds NextActions
- **File**: `lib/sop/ai/agent/codingworkflow/test_result_parser.go`

---

## Debugging Checklist

When autonomous orchestration fails, check:

### Phase Succession
- [ ] Queue actor respected? (logs: `queuedPhaseActor` vs `resolveActiveCodingRuntimePhase`)
- [ ] Classification phase synced? (logs: `current_phase` in transaction evaluation)
- [ ] No infinite re-enqueuing? (search logs for repeated `Enqueued next phase`)

### Path Resolution
- [ ] No path doubling? (error: `lib/sop/lib/sop/...`)
- [ ] Working dir correct? (logs: `working_dir=/Volumes/T7/sop-cloud/lib/sop`)
- [ ] Search results normalized? (check if paths start with workspace basename)

### Query Parsing
- [ ] Test pattern extraction valid? (logs: `extractGoTestPattern`)
- [ ] Package path extraction not false positive? (logs: `extractGoPackagePath`)
- [ ] Broad vs focused test selection appropriate?

### Tool Registration
- [ ] Registry cleared before phase? (logs: `Registry cleared before phase tool registration`)
- [ ] Correct tool count? (logs: `Registering phase tools tool_count=12`)
- [ ] No phase prefixes exposed to LLM? (logs: tool names should be clean: `run_tests` not `investigation_run_tests`)

### Context Propagation
- [ ] No `context.Background()` calls? (strips `ai.CtxKeyEventStreamer`)
- [ ] Session payload preserved? (working_dir variable available)
- [ ] Event streamer functional? (Terminal UI showing commands)

---

## Production Validation

**Test Case**: `"fix failing tests"` with `working_dir="current"`

**Expected Flow**:
1. Investigation: `run_tests` → 38 failures detected
2. Investigation: `read_file`, `search_repo` → gather context
3. Transaction: Enqueue `phase:execution`
4. Execution: `read_file` → inspect failing code (no path doubling)
5. Execution: `apply_patch` → fix implementation
6. Execution: `run_tests` → validate fix

**Success Criteria**:
- ✅ No path doubling errors (`lib/sop/lib/sop`)
- ✅ No false positive package extraction (`packages/tests` from query text)
- ✅ No phase desync (transaction evaluator sees correct phase)
- ✅ Autonomous progression (investigation → execution without manual intervention)
- ✅ Clean tool names in LLM context (no phase prefixes)

---

## Future Enhancements

### Query Understanding
- Add NLP-based intent detection to distinguish path references from natural language
- Support abbreviated paths (`tools/sopbackend` → `./lib/sop/tools/sopbackend`)
- Detect workspace root automatically from git/go.mod

### Phase Orchestration  
- Add phase transition validation (check prerequisites before advancing)
- Support phase rollback on validation failure
- Track phase iteration limits per phase (not just global)

### Path Normalization
- Centralized path resolution service
- Canonicalization of all search results
- Workspace-relative vs absolute path detection

### Monitoring
- Phase transition metrics (duration, tool calls, success rate)
- Query parsing accuracy tracking
- Path resolution error rates

---

## Related Documentation

- **Architecture**: [DOMAIN_AGNOSTIC_PHASE_SYSTEM.md](./DOMAIN_AGNOSTIC_PHASE_SYSTEM.md)
- **Patterns**: `.github/copilot-instructions.md` (Participant Pattern, context passing rules)
- **Implementation**: [CLI_CODING_WORKFLOW_IMPLEMENTATION.md](./CLI_CODING_WORKFLOW_IMPLEMENTATION.md)
- **Workflows**: [WORKFLOWS.md](./WORKFLOWS.md)
