# Phase-Aware Context Optimization

**Date**: 2026-08-31  
**Status**: Historical implementation snapshot; validate current budgets and composition against source and tests.
**Problem**: Execution phase context trimmed from 9704→2600 chars, removing critical instructions  

The recorded character counts, metrics, and manual scenarios below describe the original optimization pass, not current runtime measurements.

---

## Changes Made

### 1. Increased Execution Phase Budget

**File**: `lib/sop/ai/agent/copilot_prompt.go:415-424`

```go
// Phase-aware budget adjustment for execution phase
if isCodingWorkflowTaskClassification(taskClassification) {
    if classification := taskClassification.AskClassification; classification != nil {
        phase := normalizePhase(classification.Phase)
        if phase == "execution" {
            // Execution phase needs more room for phase instructions
            profile.ComponentCharBudgets[ComponentFocusedContext] = 6000
        }
    }
}
```

**Impact**: Execution phase now has 6000 chars (was 2600-3400) - enough room for critical instructions.

---

### 2. Phase-Aware Context Generation

**File**: `lib/sop/ai/agent/copilot_prompt.go:290-384`

#### Conditional Component Inclusion

Components now added based on phase relevance:

| Component | Investigation | Planning | Execution | Reason |
|-----------|--------------|----------|-----------|--------|
| `memoryContinuation` | ✅ | ✅ | ✅ | Always relevant |
| `askOutcome` | ✅ | ✅ | ✅ | Session continuity |
| `taskGuidanceSynthesis` | ✅ | ✅ | ❌ | Execution has phase.BuildContext() |
| `plannerContext` | ✅ | ✅ | ❌ | Planning metadata, not needed in execution |
| `caveatContext` | ✅ | ✅ | ✅ | Important corrections |
| `selectedSpaceGuidance` | ✅ | ✅ | ❌ | Research-specific |
| `researchGuidance` | ✅ | ✅ | ❌ | Discovery mode only |
| `memoryRecall` | ✅ | ✅ | ❌ | Investigation phase benefit |

**Code Pattern**:
```go
// Task guidance - Skip detailed guidance in execution phase (phase.BuildContext has it)
if !isExecutionPhase {
    if guidance := buildTaskGuidanceSynthesis(query, taskClassification); guidance != "" {
        // Add to focusedExecutionContext
    }
}
```

---

### 3. Instrumentation Logging

**File**: `lib/sop/ai/agent/copilot_prompt.go:393-405`

```go
slog.Info("Phase-aware context generation",
    "phase", currentPhase,
    "is_execution", isExecutionPhase,
    "focused_context_chars", len(focusedExecutionContext),
    "skipped_components", skippedComponents,
)
```

**Expected Log Output**:
```
Investigation: phase=investigation is_execution=false focused_context_chars=~5000 skipped_components=[]
Execution:     phase=execution is_execution=true focused_context_chars=~1500 skipped_components=[task_guidance, planner_context, research_guidance, memory_recall, selected_space_guidance]
```

---

## Expected Improvements

### Before (Aug 29)
```
Execution Phase:
  Generated: 9704 chars
  Trimmed to: 2600 chars (73% loss!)
  Result: Critical "IMMEDIATELY generate patches" instruction trimmed away
  Behavior: LLM keeps investigating instead of patching
```

### After (Aug 31)
```
Execution Phase:
  Generated: ~1500 chars (only relevant components)
  Budget: 6000 chars (4x headroom!)
  Result: All critical instructions preserved
  Behavior: LLM should generate patches immediately
```

---

## What This Fixes

1. ✅ **No more blind concatenation** - Generate only what's needed per phase
2. ✅ **Budget has headroom** - 1500 chars generated, 6000 budget = 4x safety margin
3. ✅ **Critical instructions preserved** - phase.BuildContext() output never trimmed
4. ✅ **Follows industry practice** - Like Cursor's mode-specific prompts

---

## Alignment with Copilot Best Practices

This matches what successful copilot products do:

- **GitHub Copilot**: Different system prompts for "chat" vs "edit" mode
- **Cursor**: Composer mode has focused context (implementation only)
- **Windsurf Cascade**: Stage hints guide LLM through workflow
- **Our approach**: Phase-aware context = mode-specific prompts

**We DON'T do** (correctly):
- ❌ Rigid workflow state machines (over-engineering)
- ❌ Auto-execute deterministic steps (removes LLM flexibility)
- ❌ Force tool sequences (too brittle)

**We DO** (correctly):
- ✅ Smart context budgets per phase
- ✅ Skip irrelevant components
- ✅ Let LLM choose tools (with better context)
- ✅ Stay within Participant Pattern

---

## Testing Plan

### Test 1: Fix Failing Tests Workflow

```bash
./cmd/sop-daemon/sop-daemon

# In browser
> fix failing tests
> current
```

**Watch for in logs**:
```
Phase-aware context generation phase=investigation is_execution=false focused_context_chars=~5000
Phase-aware context generation phase=execution is_execution=true focused_context_chars=~1500 skipped_components=[...]
Sending tool_call event payload=map[tool:preview_patch ...]  # ← SUCCESS!
```

**Success Criteria**:
- ✅ Execution phase generates `preview_patch` in iteration 1-2 (not iteration 3+)
- ✅ No more infinite investigation loops
- ✅ Tests get fixed

### Test 2: Compare Context Sizes

Log output should show:
```
Investigation phase: ~5000 chars (research guidance included)
Execution phase:     ~1500 chars (focused on patching only)
```

---

## Rollback Plan

If this breaks something:

```go
// copilot_prompt.go line 290
// Comment out phase detection, restore old behavior:
/*
currentPhase := ""
if classification := taskClassification.AskClassification; classification != nil {
    currentPhase = normalizePhase(classification.Phase)
}
isExecutionPhase := currentPhase == "execution"
*/
isExecutionPhase := false  // Force all components to be included
```

---

## Next Steps

1. ✅ **Implemented** - Phase-aware context generation
2. ✅ **Built** - Daemon rebuilt successfully
3. 🧪 **TODO** - Test "fix failing tests" workflow
4. 📊 **TODO** - Collect metrics (context size reduction, success rate)
5. 🔍 **TODO** - Validate other workflows (implement function, analyze feature)

---

## Files Changed

- `lib/sop/ai/agent/copilot_prompt.go`
  - Lines 415-424: Phase-aware budget increase
  - Lines 290-384: Conditional context generation
  - Lines 393-405: Instrumentation logging
