# Context Generation Audit - Current State

**Date**: 2026-08-29  
**Purpose**: Document current context generation before refactoring  
**Related**: CONTEXT_GENERATION_REFORM.md (design plan)

---

## Current Flow Analysis

### Entry Point: `copilot_prompt.go`

**Function**: `buildCopilotSystemPrompt()`  
**Location**: `lib/sop/ai/agent/copilot_prompt.go:~250-380`

```go
// Current implementation (documented, not copied code)
func (a *CopilotAgent) buildCopilotSystemPrompt(ctx, query, taskClassification) string {
    var focusedExecutionContext string
    
    // Step 1: Concatenate multiple sources (ORDER MATTERS)
    focusedExecutionContext = concat(
        1. askOutcome                              // Line ~305
        2. buildTaskGuidanceSynthesis()            // Line ~308
        3. getPlannerContext()                     // Line ~313
        4. getCaveatContext()                      // Line ~318
        5. isGeneralTaskContext()                  // Line ~323
        6. selectedSpaceGuidance                   // Line ~330
        7. buildResearchWorkflowGuidance()         // Line ~335
        8. buildVirtualMemoryRecallContext()       // Line ~344
        9. phase.BuildContext() ???                // MISSING in current view?
    )
    
    // Step 2: Trim if Stores domain
    if leanStoresAssembly && domain == StoresDomain {
        focusedExecutionContext = trim(focusedExecutionContext, 1000)  // Line ~351
    }
    
    // Step 3: Add to prompt builder
    builder.With(ComponentFocusedContext, focusedExecutionContext)     // Line ~354
    
    // Step 4: Apply budget with trimming
    fullPrompt, budgetReport = builder.ToJSONWithBudgetReport(
        a.promptBudgetProfile(taskClassification)
    )
    
    return fullPrompt
}
```

---

## Component Sources

### 1. askOutcome
**Source**: `getAskOutcomeContext()`  
**When Generated**: After ask completion  
**Typical Size**: Unknown (needs instrumentation)  
**Contains**: Previous ask outcome metadata  
**Relevance**: 
- Investigation: ✅ Yes (shows progress)
- Planning: ✅ Yes (context from investigation)
- Execution: ⚠️ Maybe (only if iterating)
- Review/Delivery: ❌ Likely not needed

### 2. buildTaskGuidanceSynthesis()
**Source**: `buildTaskGuidanceSynthesis(query, taskClassification)`  
**When Generated**: Always  
**Typical Size**: Unknown  
**Contains**: Query-specific guidance  
**Relevance**: ✅ Always (task-specific)

### 3. getPlannerContext()
**Source**: `a.getPlannerContext()`  
**When Generated**: Always concatenated  
**Typical Size**: Unknown  
**Contains**: Planning-phase specific guidance  
**Relevance**:
- Investigation: ❌ No
- Planning: ✅ Yes
- Execution: ❌ No (already past planning!)
- Review: ❌ No
- Delivery: ❌ No

**ISSUE**: Generated for ALL phases but only relevant in planning

### 4. getCaveatContext()
**Source**: `a.getCaveatContext()`  
**When Generated**: Always concatenated  
**Typical Size**: Unknown  
**Contains**: General warnings/caveats  
**Relevance**: ⚠️ Conditional (may not need in every turn)

### 5. General Context (isGeneralTaskContext)
**Source**: Hardcoded string  
**When Generated**: When `isGeneralTaskContext()` returns true  
**Size**: ~250 chars
```
"General Ask Context:
- This is an open-ended request that may cover any topic...
- Use conversation history, memory continuity, and available tools...
- Ask clarification question only if genuinely ambiguous."
```
**Relevance**: 
- Only when no specific domain classified
- Not needed for coding_workflow domain

### 6. selectedSpaceGuidance
**Source**: `a.getSelectedSpaceGuidance()`  
**When Generated**: If space is selected  
**Typical Size**: Unknown  
**Contains**: Space-specific context  
**Relevance**:
- Only for Spaces domain operations
- Not needed for coding_workflow

### 7. buildResearchWorkflowGuidance()
**Source**: `a.buildResearchWorkflowGuidance(ctx, query, taskClassification)`  
**When Generated**: If `taskClassification.ResearchRecommended == true`  
**Typical Size**: Unknown  
**Contains**: Research workflow patterns  
**Relevance**:
- Investigation: ✅ Yes (if complex task)
- Planning: ⚠️ Maybe (if research-heavy)
- Execution: ❌ No (past research phase)
- Review/Delivery: ❌ No

**ISSUE**: May be generated for "fix failing tests" which is NOT research

### 8. buildVirtualMemoryRecallContext()
**Source**: `a.buildVirtualMemoryRecallContext(ctx, query)`  
**When Generated**: Always attempted  
**Typical Size**: Unknown  
**Contains**: Previous session context for resume  
**Relevance**: ✅ Only when resuming prior work

### 9. phase.BuildContext()
**Source**: `phase.BuildContext(query, routingState)` (from focused_context.go?)  
**Location**: Called elsewhere, not visible in copilot_prompt.go excerpt  
**Typical Size**: 400-800 chars per phase  
**Contains**: Phase-specific workflow instructions  
**Relevance**: ✅ ALWAYS - Core phase instructions  
**Examples**:
```go
// execution phase (phase_coding.go:73-86)
`PHASE: Execution & Implementation
OBJECTIVE: Implementation mode...
CRITICAL: If you have test failures and source context, IMMEDIATELY generate patches.
WORKFLOW: [steps]
SAFETY: Always preview before apply
...`  // ~650 chars
```

---

## Budget Configuration

### Source: `promptBudgetProfile()`
**Location**: `lib/sop/ai/agent/copilot_prompt.go:~387-460`

```go
// Default Profile
profile := PromptBudgetProfile{
    TotalChars: 14000,
    ComponentCharBudgets: map[PromptComponent]int{
        ComponentPersona:        2800,
        ComponentSemanticMemory: 1400,
        ComponentRecipes:        3200,
        ComponentPlaybooks:      1600,
        ComponentFocusedContext: 2600,  // ⚠️ DEFAULT BUDGET
        ComponentSchema:         1800,
        ComponentHistory:        1800,
        ComponentUserQuery:      1200,
    },
    TrimPriorityLowToHigh: []PromptComponent{
        ComponentHistory,         // Trim first
        ComponentSchema,
        ComponentPlaybooks,
        ComponentSemanticMemory,
        ComponentSystemTools,
        ComponentRecipes,
        ComponentPersona,
        ComponentFocusedContext,  // Trim 8th (nearly last)
        ComponentUserQuery,       // Never trim (last)
    },
}

// RoutingGateFocused adjustments
case RoutingGateFocused:
    profile.ComponentCharBudgets[ComponentFocusedContext] = 3400  // +800 chars

// Cross-domain adjustments
if isCrossDomain(taskClassification.Layers) {
    profile.ComponentCharBudgets[ComponentFocusedContext] = 3200  // +600 chars
}
```

---

## Observed Behavior (From Logs)

### Scenario: "fix failing tests" - Execution Phase (Iteration 2)

```
INFO LLM Context Budget 
  routing_gate=discovery 
  original_chars=14288 
  final_chars=7184 
  components_present=persona,system_tools,focused_execution_context,conversation_history,user_query 
  trimmed_components=focused_execution_context:9704->2600
```

**Analysis**:
- **Generated**: 9704 chars for focused_execution_context
- **Budget**: 2600 chars (default, routing_gate=discovery)
- **Trimmed**: 7104 chars removed (73% loss)
- **Result**: Unknown which 2600 chars survived

**Why 9704 chars?**
Likely concatenation of:
1. askOutcome: ~500 chars (previous iteration outcome)
2. taskGuidance: ~300 chars
3. plannerContext: ~1200 chars ← NOT NEEDED in execution!
4. caveatContext: ~800 chars
5. generalContext: 0 (has domain)
6. spaceGuidance: 0 (not spaces domain)
7. researchGuidance: ~2000 chars? ← NOT NEEDED for "fix tests"!
8. memoryRecall: ~1500 chars?
9. phase.BuildContext(): ~650 chars ← THIS IS WHAT MATTERS!
10. Other sources: ~2754 chars

**Estimated Breakdown** (needs verification):
```
Component                   | Chars | Needed in Execution?
----------------------------|-------|---------------------
phase.BuildContext()        |   650 | ✅ YES - Critical
taskGuidance                |   300 | ✅ YES - Task-specific
askOutcome                  |   500 | ⚠️ MAYBE - Shows progress
memoryRecall                |  1500 | ⚠️ MAYBE - If resuming
caveatContext               |   800 | ⚠️ MAYBE - General warnings
plannerContext              |  1200 | ❌ NO - Past planning!
researchGuidance            |  2000 | ❌ NO - Not research task!
spaceGuidance               |     0 | ❌ NO - Not spaces
generalContext              |     0 | ❌ NO - Has domain
Others/padding              |  2754 | ❓ UNKNOWN
----------------------------|-------|---------------------
TOTAL                       |  9704 |
RELEVANT                    | ~2950 | (650+300+500+1500)
WASTE                       | ~6754 | (70% waste!)
```

---

## Trim Priority Issues

### Current Order (TrimPriorityLowToHigh):
1. History (trim first)
2. Schema
3. Playbooks
4. SemanticMemory
5. SystemTools
6. Recipes
7. Persona
8. **FocusedContext** ← Trimmed 8th
9. UserQuery (never trim)

**Problem**: FocusedContext is trimmed LATE, meaning:
- Other components trimmed first
- By the time we trim FocusedContext, we're desperate for space
- Trim 73% to fit budget
- **No intelligence** - doesn't know which parts are critical

### Example Trimming Scenario:
```
1. Generate all components: 18,000 chars
2. Budget: 14,000 chars (need to cut 4,000)
3. Trim History: -1,200 chars → 16,800 remaining
4. Trim Schema: -800 chars → 16,000 remaining
5. Trim Playbooks: -600 chars → 15,400 remaining
6. Trim SemanticMemory: -400 chars → 15,000 remaining
7. Still need -1,000 chars...
8. Trim FocusedContext: -7,104 chars → 7,896 remaining
   ↳ ⚠️ Cut from 9,704 to 2,600 (73% loss)
   ↳ ⚠️ May have lost "CRITICAL: generate patches immediately"
```

---

## Data Needed (Instrumentation Required)

### 1. Component Size Distribution
```go
// For each ask, log:
type ContextGenerationMetrics struct {
    SessionID          string
    Phase              string
    QueryType          string
    RoutingGate        string
    
    // Component sizes BEFORE trimming
    AskOutcomeChars           int
    TaskGuidanceChars         int
    PlannerContextChars       int
    CaveatContextChars        int
    GeneralContextChars       int
    SpaceGuidanceChars        int
    ResearchGuidanceChars     int
    MemoryRecallChars         int
    PhaseInstructionsChars    int
    
    // Total and budget
    TotalGenerated            int
    BudgetAllowed             int
    FinalAfterTrim            int
    TrimmedAmount             int
    TrimmedPercent            float64
    
    // Outcome
    TaskSucceeded             bool
    PatchesGenerated          bool
    IterationsNeeded          int
}
```

### 2. Relevance Analysis
For 100 recent tasks, manually evaluate:
- Was each component actually used by LLM?
- Did trimming remove critical content?
- Which components were unnecessary?

### 3. Correlation Study
```sql
-- Does excessive trimming correlate with failure?
SELECT 
    phase,
    AVG(trimmed_percent) as avg_trim,
    AVG(CASE WHEN task_succeeded THEN 1 ELSE 0 END) as success_rate
FROM context_metrics
GROUP BY phase
ORDER BY avg_trim DESC;

-- Which components get trimmed most?
SELECT 
    component_name,
    AVG(chars_generated) as avg_generated,
    AVG(chars_after_trim) as avg_kept,
    AVG(chars_generated - chars_after_trim) as avg_wasted
FROM component_metrics
GROUP BY component_name
ORDER BY avg_wasted DESC;
```

---

## Waste Patterns (Hypotheses to Test)

### Pattern 1: Cross-Phase Contamination
- **Hypothesis**: plannerContext included in execution/review phases
- **Expected Waste**: ~1,200 chars per execution turn
- **Fix**: Conditional generation based on phase

### Pattern 2: Over-Eager Research
- **Hypothesis**: researchGuidance generated for simple tasks like "fix tests"
- **Expected Waste**: ~2,000 chars when not needed
- **Fix**: Better heuristic for when research is truly needed

### Pattern 3: Stale Memory Recall
- **Hypothesis**: memoryRecall included when not resuming
- **Expected Waste**: ~1,500 chars when not relevant
- **Fix**: Only include if detecting resume intent

### Pattern 4: Redundant Guidance
- **Hypothesis**: phase.BuildContext() + taskGuidance + researchGuidance all say similar things
- **Example**: All three might mention "run tests to validate"
- **Expected Waste**: ~500-1000 chars duplication
- **Fix**: Deduplicate or consolidate sources

### Pattern 5: Universal Concatenation
- **Hypothesis**: All components always concatenated regardless of relevance
- **Expected Waste**: 50-70% of generated content unused
- **Fix**: Relevance-based conditional generation

---

## Critical Questions to Answer

### Q1: Where does phase.BuildContext() get added?
- Not visible in copilot_prompt.go excerpt (lines 300-380)
- Likely called in focused_context.go or ask_pipeline.go?
- **Action**: Find where phase context is integrated

### Q2: What's the actual trimming algorithm?
- `builder.ToJSONWithBudgetReport()` does the trimming
- Located where? prompt_builder.go?
- **Action**: Document exact trimming logic

### Q3: How often does trimming cause failures?
- No current metrics
- **Action**: Add correlation tracking

### Q4: What % of generated content is actually used?
- Unknown - need LLM attention analysis
- **Action**: Add telemetry or sample manual review

### Q5: Which components are most frequently irrelevant?
- Unknown - need relevance scoring
- **Action**: Phase-by-phase audit with scoring

---

## Next Steps

### Step 1: Instrument Current Flow (Week 1)
```go
// Add to copilot_prompt.go
func (a *CopilotAgent) buildCopilotSystemPrompt(...) string {
    metrics := &ContextGenerationMetrics{
        SessionID: getSessionID(ctx),
        Phase: getCurrentPhase(ctx),
        // ... populate ...
    }
    
    // Before each concatenation, record size
    if askOutcome != "" {
        metrics.AskOutcomeChars = len(askOutcome)
    }
    // ... for each component ...
    
    // After trimming
    metrics.TotalGenerated = len(focusedExecutionContext)
    metrics.FinalAfterTrim = len(trimmedContext)
    
    // Log to metrics store
    recordContextMetrics(ctx, metrics)
    
    return fullPrompt
}
```

### Step 2: Collect Data (Week 1-2)
- Deploy instrumented version
- Run for 1 week in production
- Collect 1000+ data points

### Step 3: Analyze Patterns (Week 2)
- Generate waste report
- Identify high-waste components
- Validate hypotheses

### Step 4: Design Targeted Fixes (Week 2)
- Use data to prioritize which components to make conditional
- Calculate expected savings
- Design relevance predicates

### Step 5: Implement Conditional Generator (Week 3)
- Based on actual data, not guesses
- Start with highest-waste components first

---

## Acceptance Criteria for "Done with Audit"

- [ ] All 9+ context component sources documented with size ranges
- [ ] Trimming algorithm fully understood and documented
- [ ] 1000+ production context generations logged with metrics
- [ ] Waste patterns identified with data backing
- [ ] Relevance matrix validated against actual usage
- [ ] Cost/benefit analysis for conditional generation completed
- [ ] Design decisions can be made with confidence from data

---

## References

**Code Locations**:
- Main prompt builder: `lib/sop/ai/agent/copilot_prompt.go:250-460`
- Phase context: `lib/sop/ai/agent/planner/phase_coding.go:49-90`
- Budget config: `lib/sop/ai/agent/copilot_prompt.go:387-460`
- Trim logic: `lib/sop/ai/agent/prompt_builder.go` (needs review)

**Related Docs**:
- [CONTEXT_GENERATION_REFORM.md](CONTEXT_GENERATION_REFORM.md) - Refactoring plan
- [PHASE_DEBUGGING_QUICK_REF.md](PHASE_DEBUGGING_QUICK_REF.md) - Phase system reference
- [AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md](AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md) - Context trimming discovery

**Logs Referenced**:
- 2026-08-29 23:44:20 - Execution iteration 2, 73% trim observed
