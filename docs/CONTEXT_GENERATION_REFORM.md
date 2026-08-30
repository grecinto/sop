# Context Generation Reform: Eliminate Budget Trimming

**Date**: 2026-08-29  
**Status**: Design Phase  
**Goal**: Deprecate budget trimmer by generating only relevant, non-duplicated, valuable context

---

## Problem Statement

### Current Flow (Wasteful)
```
1. Generate everything blindly
   - askOutcome
   - taskGuidance
   - plannerContext ← Why in execution?
   - caveatContext
   - generalContext
   - spaceGuidance
   - researchGuidance ← Why for "fix tests"?
   - memoryRecall
   - phase.BuildContext()
   
2. Concatenate all pieces: 9704 chars

3. Trim to budget: 2600 chars (73% loss!)

4. Hope critical instructions survived
```

### Issues
- **Data Loss**: 73% of generated content discarded
- **No Guarantees**: Critical phase instructions might be trimmed
- **Wasteful**: Generate 9KB to use 2.6KB
- **Blind**: Trimmer doesn't understand semantic importance
- **Dangerous**: Lost context = wrong LLM behavior

---

## Design Principles

### 1. Generate Only What's Needed
```go
// BEFORE: Generate everything, trim later
context = concat(memory, research, planner, spaces, general, caveats, phase)
trim(context, 2600)

// AFTER: Generate conditionally
if needsMemoryRecall(query, phase) {
    context += memoryRecall()
}
if needsResearchGuidance(taskType, phase) {
    context += researchGuidance()
}
context += phase.BuildContext() // Always include phase instructions
```

### 2. Context Relevance Matrix

| Context Component | Investigation | Planning | Execution | Review | Delivery |
|------------------|---------------|----------|-----------|--------|----------|
| Phase Instructions | ✅ Always | ✅ Always | ✅ Always | ✅ Always | ✅ Always |
| Memory Recall | ✅ If resuming | ✅ If resuming | ✅ If resuming | ⚠️ Rarely | ⚠️ Rarely |
| Task Guidance | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes |
| Planner Context | ❌ No | ✅ Yes | ❌ No | ❌ No | ❌ No |
| Research Guidance | ✅ If complex | ⚠️ If design | ❌ No | ❌ No | ❌ No |
| Space Guidance | ✅ If Space domain | ❌ No | ❌ No | ❌ No | ❌ No |
| General Context | ✅ If no domain | ❌ No | ❌ No | ❌ No | ❌ No |
| Caveats | ✅ Yes | ✅ Yes | ✅ Yes | ⚠️ Rarely | ⚠️ Rarely |

### 3. No Duplication
```go
// BEFORE: Multiple sources might provide same info
focusedContext = concat(
    phase.BuildContext(),      // "Run tests to validate"
    taskGuidance(),            // "You should run tests"
    researchGuidance(),        // "Validate with tests"
)

// AFTER: Single source of truth per concept
focusedContext = phase.BuildContext() // Already includes validation workflow
```

### 4. Semantic Importance
- **Critical** (MUST include): Phase instructions, active task requirements
- **High** (Include if relevant): Memory recall for resume, domain-specific guidance  
- **Medium** (Include conditionally): Research patterns, caveats
- **Low** (Usually skip): Generic boilerplate, unrelated domain context

---

## Implementation Plan

### Phase 1: Audit Current Generation (Week 1)

**1.1 Trace Context Sources**
```bash
# For each component, document:
- What generates it
- When it's relevant
- How large it typically is
- What % gets trimmed
```

**1.2 Analyze Actual Usage**
```sql
-- Add logging to track:
SELECT 
    phase,
    component,
    generated_chars,
    post_trim_chars,
    (generated_chars - post_trim_chars) as wasted_chars,
    COUNT(*) as occurrences
FROM context_generation_log
GROUP BY phase, component
ORDER BY wasted_chars DESC;
```

**1.3 Document Current Flows**
- Map: Query Type → Phase → Context Components → Trim Amount
- Identify: Which components are frequently trimmed to zero
- Find: Patterns where critical content was lost

### Phase 2: Design Conditional Generator (Week 1-2)

**2.1 Context Relevance Predicate**
```go
type ContextRelevance struct {
    Phase           string
    Domain          string
    TaskType        string
    ResearchNeeded  bool
    IsResume        bool
    IsCrossDomain   bool
}

type ContextComponent string

func (r ContextRelevance) ShouldInclude(component ContextComponent) bool {
    switch component {
    case ComponentPhaseInstructions:
        return true // Always
    
    case ComponentMemoryRecall:
        return r.IsResume
    
    case ComponentPlannerContext:
        return r.Phase == "planning"
    
    case ComponentResearchGuidance:
        return r.ResearchNeeded && (r.Phase == "investigation" || r.Phase == "planning")
    
    case ComponentSpaceGuidance:
        return r.Domain == "spaces"
    
    case ComponentGeneralContext:
        return r.Domain == "" // Only if no specific domain
    
    default:
        return false
    }
}
```

**2.2 Refactor Prompt Builder**
```go
// copilot_prompt.go - NEW DESIGN

func (a *CopilotAgent) buildFocusedExecutionContext(
    ctx context.Context,
    query string,
    phase string,
    taskClassification TaskContextClassification,
) string {
    relevance := ContextRelevance{
        Phase:          phase,
        Domain:         taskClassification.Domain,
        TaskType:       inferTaskType(query, taskClassification),
        ResearchNeeded: taskClassification.ResearchRecommended,
        IsResume:       detectResume(ctx, query),
        IsCrossDomain:  isCrossDomain(taskClassification.Layers),
    }
    
    var sections []string
    
    // 1. Phase instructions (ALWAYS - core workflow)
    if phaseContext := a.getPhaseContext(phase); phaseContext != "" {
        sections = append(sections, phaseContext)
    }
    
    // 2. Task-specific guidance (ALWAYS - for current query)
    if taskGuidance := buildTaskGuidanceSynthesis(query, taskClassification); taskGuidance != "" {
        sections = append(sections, taskGuidance)
    }
    
    // 3. Memory recall (CONDITIONAL - only if resuming)
    if relevance.ShouldInclude(ComponentMemoryRecall) {
        if memoryRecall := a.buildVirtualMemoryRecallContext(ctx, query); memoryRecall != "" {
            sections = append(sections, memoryRecall)
        }
    }
    
    // 4. Domain-specific context (CONDITIONAL)
    if relevance.ShouldInclude(ComponentSpaceGuidance) {
        if spaceGuidance := a.getSelectedSpaceGuidance(); spaceGuidance != "" {
            sections = append(sections, spaceGuidance)
        }
    }
    
    // 5. Research patterns (CONDITIONAL - complex tasks only)
    if relevance.ShouldInclude(ComponentResearchGuidance) {
        if researchGuidance := a.buildResearchWorkflowGuidance(ctx, query, taskClassification); researchGuidance != "" {
            sections = append(sections, researchGuidance)
        }
    }
    
    // 6. Planning context (CONDITIONAL - planning phase only)
    if relevance.ShouldInclude(ComponentPlannerContext) {
        if plannerContext := a.getPlannerContext(); plannerContext != "" {
            sections = append(sections, plannerContext)
        }
    }
    
    // 7. Caveats (CONDITIONAL - active phases only)
    if relevance.Phase == "investigation" || relevance.Phase == "planning" || relevance.Phase == "execution" {
        if caveats := a.getCaveatContext(); caveats != "" {
            sections = append(sections, caveats)
        }
    }
    
    return strings.Join(sections, "\n\n")
}
```

**2.3 Remove Duplication**
```go
// BEFORE: Multiple overlapping sources
phase.BuildContext()           // "Run tests to validate fixes"
taskGuidance()                 // "Use run_tests tool to validate"
researchGuidance()             // "Test-driven validation recommended"

// AFTER: Single authoritative source
phase.BuildContext()           // Contains complete workflow
// taskGuidance only adds query-specific context, not workflow
// researchGuidance only for genuinely complex research tasks
```

### Phase 3: Add Validation (Week 2)

**3.1 Size Assertions**
```go
func TestContextGenerationStaysWithinBudget(t *testing.T) {
    scenarios := []struct{
        query string
        phase string
        maxChars int
    }{
        {"fix failing tests", "execution", 3000},
        {"design new feature", "planning", 4000},
        {"investigate bug", "investigation", 3500},
    }
    
    for _, sc := range scenarios {
        context := buildFocusedExecutionContext(ctx, sc.query, sc.phase, ...)
        if len(context) > sc.maxChars {
            t.Errorf("Context for %q in %q: %d chars (max: %d)",
                sc.query, sc.phase, len(context), sc.maxChars)
        }
    }
}
```

**3.2 Relevance Checks**
```go
func TestExecutionPhaseOmitsIrrelevantContext(t *testing.T) {
    context := buildFocusedExecutionContext(ctx, "fix tests", "execution", ...)
    
    // Should NOT contain:
    if strings.Contains(context, "Planning Context") {
        t.Error("Execution phase should not include planning context")
    }
    if strings.Contains(context, "Research Workflow") {
        t.Error("Simple fix shouldn't include research guidance")
    }
    
    // MUST contain:
    if !strings.Contains(context, "PHASE: Execution") {
        t.Error("Execution phase must include phase instructions")
    }
}
```

**3.3 No Duplication Check**
```go
func TestContextHasNoDuplication(t *testing.T) {
    context := buildFocusedExecutionContext(ctx, "fix tests", "execution", ...)
    
    // Check for repeated phrases
    phrases := extractKeyPhrases(context)
    seen := make(map[string]int)
    for _, phrase := range phrases {
        seen[phrase]++
        if seen[phrase] > 1 {
            t.Errorf("Phrase appears %d times: %q", seen[phrase], phrase)
        }
    }
}
```

### Phase 4: Migrate & Deprecate Trimmer (Week 3)

**4.1 Add Feature Flag**
```go
// copilot_prompt.go
const useConditionalGeneration = true // Feature flag

if useConditionalGeneration {
    focusedContext = a.buildFocusedExecutionContextV2(...)
    // No trimming needed
} else {
    focusedContext = a.buildFocusedExecutionContextLegacy(...)
    focusedContext = trimPromptComponentContent(...) // Old behavior
}
```

**4.2 A/B Test**
```go
// Log both approaches, compare results
v1 := buildLegacyWithTrim(...)
v2 := buildConditionalNoTrim(...)

log.Info("Context comparison",
    "v1_chars", len(v1),
    "v2_chars", len(v2),
    "v1_trimmed", wasTrimmed(v1),
    "v2_trimmed", false,
)

return v2 // Use new approach
```

**4.3 Remove Trimmer**
```go
// Delete after validation:
- trimPromptComponentContent()
- PromptBudgetProfile.ComponentCharBudgets
- TrimPriorityLowToHigh
- All trimming logic
```

### Phase 5: Monitor & Tune (Ongoing)

**5.1 Metrics**
```go
type ContextGenerationMetrics struct {
    Phase              string
    QueryType          string
    ComponentsIncluded []string
    TotalChars         int
    ExceededBudget     bool
    MissingCritical    bool
}

// Dashboard tracking:
- Avg context size by phase
- % queries exceeding soft limits (3000 chars)
- Components included frequency
- Task success rate correlation
```

**5.2 Tuning Knobs**
```go
// Per-phase target sizes (guidelines, not hard limits)
var phaseContextTargets = map[string]int{
    "investigation": 2800,
    "planning":      3200,
    "execution":     2200, // Lean - just instructions
    "review":        2400,
    "delivery":      2000,
}

// Alert if consistently exceeding by >50%
```

---

## Success Criteria

### Must Achieve
- ✅ Zero trimming occurs in production
- ✅ Phase instructions ALWAYS fully included
- ✅ Generated context ≤ 3500 chars per phase (95th percentile)
- ✅ No duplicate guidance across components
- ✅ Task success rate maintained or improved

### Should Achieve  
- ✅ Avg context size reduced 40-60% vs current
- ✅ Token cost reduced proportionally
- ✅ Context relevance score >90% (manual evaluation)
- ✅ Zero reports of missing critical instructions

### Nice to Have
- ✅ Dynamic adjustment based on task complexity
- ✅ Learning: improve relevance predictions over time
- ✅ Per-domain context templates

---

## Risk Mitigation

### Risk: Under-generate context
**Mitigation**: 
- Start conservative (include more)
- Remove components incrementally
- Monitor task success rates
- Easy rollback with feature flag

### Risk: Break existing workflows
**Mitigation**:
- A/B test with production traffic
- Gradual rollout by phase
- Keep legacy code until validation complete

### Risk: Phase-specific needs missed
**Mitigation**:
- Phase owners review relevance matrix
- Add phase-specific hooks for custom context
- Extensive testing per phase

---

## Timeline

| Week | Activities | Deliverables |
|------|-----------|--------------|
| 1 | Audit current generation, analyze trim logs | Context waste report, relevance matrix |
| 1-2 | Design conditional generator, refactor buildFocusedExecutionContext | PR: Conditional generation with feature flag |
| 2 | Add validation tests, size assertions | Test suite with >90% coverage |
| 3 | Deploy with feature flag, A/B test, monitor | Production deployment, metrics dashboard |
| 4+ | Remove trimmer, cleanup dead code | Trimmer deprecated, documentation updated |

---

## Next Steps

1. **Approve Design**: Review this plan with team
2. **Instrument Logging**: Add context generation metrics to production
3. **Analyze Data**: Run for 1 week to understand current waste patterns
4. **Implement V2**: Build conditional generator
5. **Validate**: Test thoroughly before rollout
6. **Deploy**: Feature flag → A/B test → Full migration
7. **Deprecate**: Remove trimmer once stable

---

## Open Questions

1. Should we make phase.BuildContext() return structured sections instead of flat string?
   - Pro: Better control over what's included
   - Con: More refactoring needed

2. Dynamic context budget based on model context window?
   - GPT-4: 8K budget
   - GPT-5.4: Maybe higher?
   - Claude: Different limits?

3. Should phase instructions be in system prompt instead of user context?
   - Pro: Never gets trimmed, applies to whole session
   - Con: Less flexible per-turn adaptation

4. Learning mechanism to improve relevance predictions?
   - Track: which context correlated with successful outcomes
   - Adjust: relevance rules based on data
