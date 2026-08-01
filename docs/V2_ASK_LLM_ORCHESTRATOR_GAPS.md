# V2 Ask LLM-as-Orchestrator Gap Analysis

**Date**: 2026-07-31  
**Status**: Analysis Complete

---

## Executive Summary

The V2 Ask pipeline has made **significant progress** toward LLM-as-Orchestrator, but there are **2 critical gaps** and **1 design weakness** that prevent full implementation:

### Critical Gaps
1. ❌ **WorkStreamManagement NOT Registered** - Capability created but never wired into agent
2. ❌ **Phase Success Criteria Not Enforced** - Checkboxes shown to LLM but not validated

### Design Issue
3. ⚠️ **Phase Completion is Weak** - String-based status with no programmatic validation

---

## Gap 1: WorkStreamManagement Not Registered

### Problem

The `WorkStreamManagement` capability is fully implemented but **never registered** with the agent.

**Evidence**:

Location: `lib/sop/ai/agent/copilot.go:1639-1710` (`ensureCapabilityRegistry()`)

```go
func (a *CopilotAgent) ensureCapabilityRegistry() {
    // ... desktop, stores, spaces capabilities registered ...
    
    // Planner registered
    caps = append(caps, plannerpkg.NewPlannerCapability(cwfInfra))
    
    // Multi-Step Planner registered
    caps = append(caps, multistepplanner.NewMultiStepPlannerCapability(cwfInfra))
    
    // Phase capabilities registered
    for _, phaseID := range []string{"intake", "investigation", "planning", "execution", "review", "delivery"} {
        caps = append(caps, plannerpkg.NewPhase("coding_workflow", phaseID, cwfInfra))
    }
    
    // ❌ WorkStreamManagement NEVER registered!
    // Missing: caps = append(caps, capabilities.NewWorkStreamManagement(...))
}
```

**Impact**:
- LLM cannot see WorkStream management tools
- Cannot pause/resume/jump phases
- jump_to_phase, pause_workstream, etc. are invisible to LLM
- LLM-as-Orchestrator is incomplete

**Solution**:

Add to `ensureCapabilityRegistry()` after multi-step planner:

```go
// WorkStream Management (LLM orchestrates pause/resume/jump)
caps = append(caps, capabilities.NewWorkStreamManagement(
    a.getSTMForPhaseCapabilities,
    func() capabilities.PhasePlanMutator {
        return cwfInfra.PhasePlanMutator.(capabilities.PhasePlanMutator)
    },
))
```

**Files to Edit**:
- `/Volumes/T7/sop-cloud/lib/sop/ai/agent/copilot.go` (ensureCapabilityRegistry function)

---

## Gap 2: Phase Success Criteria Not Enforced

### Problem

Each phase shows **completion criteria** as checkboxes in BuildContext(), but these are **suggestions to LLM**, not enforced conditions.

**Evidence**:

Location: `lib/sop/ai/agent/codingworkflow/phase_capabilities.go:255+`

**Intake Phase Criteria**:
```go
COMPLETION CRITERIA:
□ User's intent is clearly documented
□ Requirements list has >= 1 concrete requirement
□ Scope is defined (what's included/excluded)
□ Success criteria or acceptance criteria documented
□ Artifact captured with status="complete"
□ Ready to transition to investigation or planning
```

**Investigation Phase Criteria**:
```go
COMPLETION CRITERIA:
□ Root cause identified (for bugs)
□ OR codebase structure understood (for features)
□ Evidence documented (file locations, patterns)
□ >= 3 pieces of evidence gathered
□ Artifact captured with status="complete"
□ Ready to transition to planning
```

**Execution Phase Criteria**:
```go
COMPLETION CRITERIA:
□ All planned changes applied
□ Tests passing (if automated tests exist)
□ No compilation/syntax errors
□ Code review ready
□ Artifact captured with status="complete"
□ Ready to transition to review
```

### The Weakness

**Current Implementation** (`lib/sop/ai/agent/planner/phase_base.go:195-280`):

```go
func (b *BasePhase) runCapture(args map[string]any) (string, error) {
    summary, _ := args["summary"].(string)
    status, _ := args["status"].(string)

    // ❌ Only validates that fields exist, not that criteria are met
    if strings.TrimSpace(summary) == "" {
        return "", fmt.Errorf("summary is required")
    }
    if strings.TrimSpace(status) == "" {
        return "", fmt.Errorf("status is required")
    }

    // ✅ LLM can set status="complete" even if:
    // - Requirements list is empty (intake)
    // - No evidence gathered (investigation)  
    // - No plan created (planning)
    // - No tests run (execution)
    // - No validation performed (review)
    
    artifact := phaseArtifact{
        PhaseID:   b.phaseID,
        Summary:   summary,
        Status:    status,  // ❌ Any string accepted
        // ...
    }
    
    // ❌ No validation of completion criteria
    
    return fmt.Sprintf("✓ %s artifact captured (status: %s)", b.name, status), nil
}
```

**Status Values** (from `captureToolSchema()`):
```
Phase status: exploring|drafting|refining|ready_for_execution|complete|blocked|approved|delivered
```

These are strings with **no validation** that the phase actually meets its criteria.

### Impact

**LLM can skip work**:
```
User: "Add Redis caching"

LLM (Intake):
  intake_capture(summary="Add caching", status="complete", requirements=[])
  ❌ No requirements documented, but marked complete!

LLM (Investigation):  
  investigation_capture(summary="Done", status="complete", evidence=[])
  ❌ No evidence gathered, but marked complete!

LLM (Planning):
  planning_capture(summary="Plan made", status="complete", plan=[])
  ❌ No plan created, but marked complete!

Result: Phase succession happens but no actual work done
```

**Real-World Failure Mode**:
1. LLM rushes through phases to appear productive
2. Marks status="complete" without doing the work
3. Phase succession continues (plan says next phase)
4. User gets "implementation complete" but nothing actually happened
5. Quality suffers, bugs introduced, requirements missed

### Why This is a Problem

The current design relies on **LLM honesty** and **prompt engineering** to ensure work is done. But:

- ❌ LLMs can hallucinate completion
- ❌ LLMs optimize for appearing helpful (fast completion)
- ❌ No guardrails prevent premature phase transitions
- ❌ User has no way to know if criteria were actually met

### Proposed Solutions

#### Option 1: Programmatic Validation (Recommended)

Add validation logic to `runCapture()`:

```go
func (b *BasePhase) runCapture(args map[string]any) (string, error) {
    // ... existing code ...
    
    // Validate phase-specific completion criteria
    if status == "complete" {
        if err := b.validateCompletionCriteria(artifact); err != nil {
            return "", fmt.Errorf("cannot mark phase complete: %w", err)
        }
    }
    
    // ... rest of function ...
}

func (b *BasePhase) validateCompletionCriteria(artifact phaseArtifact) error {
    switch b.phaseID {
    case "intake":
        if len(artifact.Requirements) == 0 {
            return fmt.Errorf("intake requires at least 1 requirement")
        }
        if artifact.Summary == "" {
            return fmt.Errorf("intake requires summary of user intent")
        }
    
    case "investigation":
        if len(artifact.Evidence) < 3 {
            return fmt.Errorf("investigation requires at least 3 pieces of evidence")
        }
        if artifact.Summary == "" {
            return fmt.Errorf("investigation requires root cause or findings summary")
        }
    
    case "planning":
        if len(artifact.Plan) == 0 {
            return fmt.Errorf("planning requires at least 1 implementation step")
        }
        if artifact.Summary == "" {
            return fmt.Errorf("planning requires summary of approach")
        }
    
    case "execution":
        // At minimum, must have summary of what was done
        if artifact.Summary == "" {
            return fmt.Errorf("execution requires summary of changes made")
        }
        // Could add: test results validation, file change verification
    
    case "review":
        if artifact.Status != "approved" && artifact.Status != "complete" {
            return fmt.Errorf("review must be 'approved' or have findings")
        }
    
    case "delivery":
        if artifact.Summary == "" {
            return fmt.Errorf("delivery requires final summary")
        }
    }
    
    return nil
}
```

**Benefits**:
- ✅ LLM cannot skip work
- ✅ Phases must meet minimum criteria
- ✅ Clear error messages guide LLM
- ✅ User confidence in quality

**Drawbacks**:
- ⚠️ More rigid (less LLM flexibility)
- ⚠️ Requires careful criteria design
- ⚠️ May block legitimate edge cases

#### Option 2: Completion Checklist Tool (Hybrid)

Add a validation tool LLM must call before marking complete:

```go
// New tool: validate_phase_completion
func (b *BasePhase) validatePhaseCompletion(args map[string]any) (string, error) {
    artifact := b.loadExistingArtifact()
    
    issues := []string{}
    
    switch b.phaseID {
    case "intake":
        if len(artifact.Requirements) == 0 {
            issues = append(issues, "❌ No requirements documented")
        } else {
            issues = append(issues, "✅ Requirements documented")
        }
    
    case "investigation":
        if len(artifact.Evidence) < 3 {
            issues = append(issues, fmt.Sprintf("❌ Only %d pieces of evidence (need 3)", len(artifact.Evidence)))
        } else {
            issues = append(issues, fmt.Sprintf("✅ %d pieces of evidence gathered", len(artifact.Evidence)))
        }
    }
    
    if len(issues) > 0 {
        return "Phase Completion Validation:\n" + strings.Join(issues, "\n"), nil
    }
    
    return "✅ All completion criteria met. Safe to mark phase complete.", nil
}
```

**Benefits**:
- ✅ LLM gets feedback before marking complete
- ✅ Less rigid than hard validation
- ✅ Educational (LLM learns what's required)
- ✅ Still allows flexibility

**Drawbacks**:
- ⚠️ LLM can ignore the tool
- ⚠️ Not enforced (just advisory)

#### Option 3: Status Enum with Required Fields

Make status an enum and require specific fields per status:

```go
type PhaseStatus string

const (
    StatusExploring          PhaseStatus = "exploring"
    StatusDrafting           PhaseStatus = "drafting"
    StatusRefining           PhaseStatus = "refining"
    StatusReadyForExecution  PhaseStatus = "ready_for_execution"
    StatusComplete           PhaseStatus = "complete"
    StatusBlocked            PhaseStatus = "blocked"
    StatusApproved           PhaseStatus = "approved"
    StatusDelivered          PhaseStatus = "delivered"
)

// Required fields per phase when status="complete"
var completionRequirements = map[string]map[string][]string{
    "intake": {
        "complete": {"summary", "requirements"}, // Must have both
    },
    "investigation": {
        "complete": {"summary", "evidence"}, // Must have both
    },
    "planning": {
        "complete": {"summary", "plan"}, // Must have both
    },
    // ...
}
```

---

## Gap 3: isPhaseCompleteV2() is Too Simplistic

### Problem

The V2 Ask pipeline uses `isPhaseCompleteV2()` to determine if a phase is done, but the logic is **weak heuristics**.

**Evidence**:

Location: `lib/sop/ai/agent/copilot_v2.go:632-670`

```go
func (a *CopilotAgent) isPhaseCompleteV2(ctx context.Context, currentPhase string, toolCalls []ai.ToolCall, response string) bool {
    // Simple heuristic: Phase is complete if:
    // 1. At least one tool was called
    // 2. Response doesn't indicate "in progress" or "next step"

    if len(toolCalls) == 0 {
        return false  // ❌ No work done
    }

    // Check for completion tool calls
    for _, tc := range toolCalls {
        // Tools with "capture" or "complete" in name signal phase end
        if strings.Contains(tc.Name, "capture") || strings.Contains(tc.Name, "complete") {
            return true  // ✅ Capture called = complete
        }
    }

    // Check response for continuation signals
    responseLower := strings.ToLower(response)
    continuationSignals := []string{
        "next step",
        "will now",
        "proceeding to",
        "let me",
        "i will",
    }

    for _, signal := range continuationSignals {
        if strings.Contains(responseLower, signal) {
            return false // Still in progress
        }
    }

    // Default: Phase complete if tools were called and response provided
    return len(toolCalls) > 0 && len(strings.TrimSpace(response)) > 0
}
```

### Weakness

**Problem 1: Keyword-based is brittle**
```go
// ❌ Fails if LLM uses different phrasing
LLM: "I'm going to implement the caching now"  // No "next step" → marked complete!
LLM: "Implementing changes..."  // No continuation signal → complete!
```

**Problem 2: Doesn't check artifact status**
```go
// ❌ Doesn't read the actual artifact status
// Should check: artifact.Status == "complete"
```

**Problem 3: Assumes capture = complete**
```go
// ❌ LLM can call capture with status="in_progress"
// But isPhaseCompleteV2 sees "capture" in tool name → marks complete!
```

### Recommended Solution

Check the actual artifact status instead of heuristics:

```go
func (a *CopilotAgent) isPhaseCompleteV2(ctx context.Context, currentPhase string, toolCalls []ai.ToolCall, response string) bool {
    // Check if capture tool was called
    captureToolCalled := false
    for _, tc := range toolCalls {
        if strings.Contains(tc.Name, "capture") {
            captureToolCalled = true
            break
        }
    }
    
    if !captureToolCalled {
        return false // Not complete until artifact captured
    }
    
    // Load the actual artifact and check its status
    stm := a.getSTMForPhaseCapabilities()
    if stm == nil {
        return false
    }
    
    thread := stm.GetCurrentThread()
    if thread == nil {
        return false
    }
    
    streamCtx := thread.GetStreamContext()
    if streamCtx == nil {
        return false
    }
    
    // Get artifact for current phase
    artifacts := streamCtx.GetArtifacts()
    if artifact, ok := artifacts[currentPhase]; ok {
        // Check actual status
        if artMap, ok := artifact.(map[string]any); ok {
            status, _ := artMap["status"].(string)
            return status == "complete" || status == "approved" || status == "delivered"
        }
    }
    
    // Fallback to heuristics if artifact not found
    return false
}
```

---

## Summary of Gaps

| Gap | Severity | Impact | Effort to Fix |
|-----|----------|--------|---------------|
| **WorkStreamManagement not registered** | 🔴 Critical | LLM cannot orchestrate WorkStreams | 10 minutes |
| **Phase criteria not enforced** | 🟠 High | LLM can skip work, quality suffers | 2-4 hours |
| **isPhaseCompleteV2 too simple** | 🟡 Medium | Inaccurate phase transitions | 30 minutes |

---

## Recommended Action Plan

### Phase 1: Critical (Today)

1. **Register WorkStreamManagement** (10 min)
   - Edit `copilot.go::ensureCapabilityRegistry()`
   - Add WorkStreamManagement after multi-step planner
   - Test: LLM can see and call jump_to_phase, pause_workstream, etc.

2. **Fix isPhaseCompleteV2** (30 min)
   - Read actual artifact status instead of heuristics
   - Test: Phase completion accurate

### Phase 2: Important (This Week)

3. **Add Completion Validation** (2-4 hours)
   - Option 1: Hard validation in runCapture (recommended)
   - Option 2: Validation tool LLM can call
   - Option 3: Required fields per status
   - Test: LLM cannot mark phase complete without meeting criteria

### Phase 3: Polish (Next Week)

4. **Enhanced Completion Feedback**
   - Show checklist status when LLM attempts to mark complete
   - Guide LLM toward meeting criteria
   - Better error messages

---

## Testing Checklist

After fixing gaps:

### WorkStreamManagement
- [ ] `list_workstreams()` visible in tool list
- [ ] `jump_to_phase(phase)` works forward/backward
- [ ] `pause_workstream()` / `resume_workstream()` work
- [ ] LLM discovers and uses tools appropriately

### Phase Completion
- [ ] Cannot mark intake complete without requirements
- [ ] Cannot mark investigation complete without evidence
- [ ] Cannot mark planning complete without plan
- [ ] Cannot mark execution complete without summary
- [ ] Error messages guide LLM to add missing fields

### isPhaseCompleteV2
- [ ] Reads actual artifact status
- [ ] Detects "complete", "approved", "delivered"
- [ ] Ignores "in_progress", "drafting", "refining"
- [ ] Handles missing artifacts gracefully

---

## Conclusion

The V2 Ask LLM-as-Orchestrator implementation is **80% complete** but has **3 critical gaps**:

1. ❌ **WorkStreamManagement not wired** → Fix immediately (10 min)
2. ❌ **No validation of phase criteria** → High priority (2-4 hours)
3. ⚠️ **Weak phase completion detection** → Medium priority (30 min)

**Total effort to close gaps**: ~4-5 hours

Once these are fixed, we'll have a **complete LLM-as-Orchestrator system** with:
- ✅ WorkStream management tools (pause/resume/jump)
- ✅ Enforced phase success criteria
- ✅ Accurate phase completion detection
- ✅ Full LLM orchestration control
