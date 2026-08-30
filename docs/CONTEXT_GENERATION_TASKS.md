# Context Generation Investigation - Action Items

**Created**: 2026-08-29  
**Purpose**: Track specific tasks for documenting and fixing context generation  
**Related**: [CONTEXT_GENERATION_AUDIT.md](CONTEXT_GENERATION_AUDIT.md), [CONTEXT_GENERATION_REFORM.md](CONTEXT_GENERATION_REFORM.md)

---

## Phase 1: Document Current State ✅

### 1.1 Trace Context Flow ✅
- [x] Document entry point: `buildCopilotSystemPrompt()`
- [x] List all 9+ component sources
- [x] Document concatenation order
- [x] Identify budget configuration
- [x] Map trim priority order
- **Deliverable**: [CONTEXT_GENERATION_AUDIT.md](CONTEXT_GENERATION_AUDIT.md) created

### 1.2 Identify Known Waste Patterns ✅
- [x] Pattern 1: Cross-phase contamination (plannerContext in execution)
- [x] Pattern 2: Over-eager research (researchGuidance for simple tasks)
- [x] Pattern 3: Stale memory recall (when not resuming)
- [x] Pattern 4: Redundant guidance (duplication across sources)
- [x] Pattern 5: Universal concatenation (all components always added)
- **Deliverable**: Waste patterns documented with hypotheses

### 1.3 Document Budget Configuration ✅
- [x] Default budgets per component
- [x] Routing gate adjustments
- [x] Trim priority order
- [x] Observed trim amounts (73% for execution)
- **Deliverable**: Budget section in audit doc

---

## Phase 2: Instrument & Collect Data 🔄

### 2.1 Add Metrics Logging 📋 TODO
```go
// Target: lib/sop/ai/agent/copilot_prompt.go
type ContextGenerationMetrics struct {
    SessionID              string
    Phase                  string
    QueryType              string
    RoutingGate            string
    
    // Component sizes BEFORE trimming
    AskOutcomeChars        int
    TaskGuidanceChars      int
    PlannerContextChars    int
    CaveatContextChars     int
    GeneralContextChars    int
    SpaceGuidanceChars     int
    ResearchGuidanceChars  int
    MemoryRecallChars      int
    PhaseInstructionsChars int
    OtherChars             int
    
    // Budget and outcome
    TotalGenerated         int
    BudgetAllowed          int
    FinalAfterTrim         int
    TrimmedAmount          int
    TrimmedPercent         float64
    
    // Task outcome
    TaskSucceeded          bool
    PatchesGenerated       bool
    IterationsNeeded       int
}

func (a *CopilotAgent) buildCopilotSystemPrompt(...) string {
    metrics := &ContextGenerationMetrics{...}
    
    // Before each concat
    if askOutcome != "" {
        metrics.AskOutcomeChars = len(askOutcome)
        focusedExecutionContext += askOutcome
    }
    // ... repeat for each component ...
    
    // After trimming
    metrics.TotalGenerated = len(focusedExecutionContext)
    metrics.FinalAfterTrim = len(trimmedContext)
    metrics.TrimmedPercent = float64(metrics.TrimmedAmount) / float64(metrics.TotalGenerated)
    
    // Async log to not block LLM
    go recordContextMetrics(ctx, metrics)
    
    return fullPrompt
}
```

**Tasks**:
- [ ] Create `ContextGenerationMetrics` struct
- [ ] Add size tracking before each concatenation
- [ ] Capture trim amounts from budget report
- [ ] Add metrics storage (file or DB)
- [ ] Deploy instrumented version

**Deliverable**: Instrumented daemon with metrics collection

### 2.2 Collect Production Data 📋 TODO
**Timeline**: Run for 1 week

**Targets**:
- Minimum 1000 context generations
- Cover all phases (investigation, planning, execution, review, delivery)
- Various query types (fix tests, add feature, debug, refactor)
- Different domains (coding, stores, spaces)

**Validation**:
```sql
-- Check data coverage
SELECT phase, COUNT(*) as count
FROM context_metrics
GROUP BY phase;

SELECT query_type, COUNT(*) as count
FROM context_metrics  
GROUP BY query_type
LIMIT 20;
```

**Deliverable**: `context_metrics.jsonl` or database with 1000+ entries

### 2.3 Analyze Data 📋 TODO
**Queries to Run**:

```sql
-- 1. Component size distribution
SELECT 
    component_name,
    AVG(chars_generated) as avg_size,
    STDDEV(chars_generated) as stddev_size,
    MAX(chars_generated) as max_size
FROM component_metrics
GROUP BY component_name
ORDER BY avg_size DESC;

-- 2. Waste by phase
SELECT 
    phase,
    AVG(total_generated) as avg_generated,
    AVG(final_after_trim) as avg_kept,
    AVG(trimmed_percent) as avg_trim_pct,
    COUNT(*) as samples
FROM context_metrics
GROUP BY phase
ORDER BY avg_trim_pct DESC;

-- 3. Correlation: trim amount vs success
SELECT 
    CASE 
        WHEN trimmed_percent < 0.2 THEN 'Low (0-20%)'
        WHEN trimmed_percent < 0.5 THEN 'Medium (20-50%)'
        ELSE 'High (50%+)'
    END as trim_category,
    AVG(CASE WHEN task_succeeded THEN 1.0 ELSE 0.0 END) as success_rate,
    COUNT(*) as samples
FROM context_metrics
GROUP BY trim_category;

-- 4. Component relevance by phase
SELECT 
    phase,
    component_name,
    SUM(chars_generated) as total_chars,
    AVG(CASE WHEN chars_generated > 0 THEN 1 ELSE 0 END) as inclusion_rate
FROM component_phase_metrics
GROUP BY phase, component_name
ORDER BY phase, total_chars DESC;
```

**Deliverable**: Analysis report with:
- Size distributions per component
- Waste patterns quantified
- Success correlation data
- Relevance matrix validated

---

## Phase 3: Design Phase-Aware Generator 📋 TODO

### 3.1 Create Relevance Rules
Based on data analysis, formalize:

```go
type ContextRelevance struct {
    Phase          string
    Domain         string
    TaskType       string
    ResearchNeeded bool
    IsResume       bool
}

func (r ContextRelevance) ShouldInclude(component ContextComponent) bool {
    // Data-driven rules
}
```

**Tasks**:
- [ ] Convert waste patterns into if/else rules
- [ ] Add heuristics for detecting task types
- [ ] Define component inclusion matrix per phase
- [ ] Add tests for relevance logic

**Deliverable**: `context_relevance.go` with rules

### 3.2 Refactor Builder Function
```go
func (a *CopilotAgent) buildFocusedExecutionContextV2(
    ctx context.Context,
    query string,
    phase string,
    taskClassification TaskContextClassification,
) string {
    relevance := computeRelevance(query, phase, taskClassification)
    
    var sections []string
    
    // Conditional generation - only relevant components
    if relevance.ShouldInclude(ComponentPhaseInstructions) {
        sections = append(sections, a.getPhaseContext(phase))
    }
    // ... repeat for each component with condition ...
    
    return strings.Join(sections, "\n\n")
}
```

**Tasks**:
- [ ] Create V2 function with conditional logic
- [ ] Add feature flag to toggle V1/V2
- [ ] Update budget config (higher limits since no trim needed)
- [ ] Add logging to compare V1 vs V2 sizes

**Deliverable**: PR with conditional generator behind feature flag

---

## Phase 4: Validate & Migrate 📋 TODO

### 4.1 A/B Testing
```go
const useConditionalGeneration = false // Feature flag

if useConditionalGeneration {
    focusedContext = a.buildFocusedExecutionContextV2(...)
} else {
    focusedContext = a.buildFocusedExecutionContextLegacy(...)
}

log.Info("Context comparison",
    "v1_chars", len(v1Context),
    "v2_chars", len(v2Context),
    "v1_trimmed", wasV1Trimmed,
    "v2_trimmed", false,
)
```

**Tasks**:
- [ ] Deploy with feature flag off
- [ ] Enable for 10% of traffic
- [ ] Monitor metrics for 3 days
- [ ] Compare success rates V1 vs V2
- [ ] Gradually increase to 100%

**Deliverable**: Validated V2 running in 100% of traffic

### 4.2 Remove Trimmer
After V2 stable:
- [ ] Delete `trimPromptComponentContent()`
- [ ] Remove `PromptBudgetProfile.ComponentCharBudgets`
- [ ] Remove `TrimPriorityLowToHigh`
- [ ] Delete legacy `buildFocusedExecutionContextLegacy()`
- [ ] Update docs to remove trimming references

**Deliverable**: Trimmer completely removed from codebase

---

## Phase 5: Monitor & Tune 📋 TODO

### 5.1 Ongoing Metrics
```go
type ContextEfficiencyMetrics struct {
    Date                    string
    PhaseDistribution       map[string]int
    AvgContextSize          map[string]int  // By phase
    TaskSuccessRate         map[string]float64
    ComponentInclusionRate  map[string]float64
    ExceededSoftLimit       int  // Count of >3500 char contexts
}

// Dashboard tracking:
// - Weekly context size trends
// - Success rate correlation  
// - Component usage patterns
// - Phase-specific efficiency
```

**Tasks**:
- [ ] Create monitoring dashboard
- [ ] Set alerts for anomalies
- [ ] Weekly review of metrics
- [ ] Tune relevance rules based on patterns

**Deliverable**: Self-maintaining context system with metrics

---

## Critical Path

```
Week 1: Instrument [2.1] → Deploy → Collect Data [2.2]
          ↓
Week 2: Analyze [2.3] → Design Rules [3.1]
          ↓
Week 3: Implement V2 [3.2] → Deploy with Flag [4.1]
          ↓  
Week 4: A/B Test [4.1] → Migrate [4.2] → Remove Trimmer
          ↓
Ongoing: Monitor [5.1] → Tune
```

---

## Success Metrics

**Must Achieve** (before declaring "done"):
- [ ] Zero trimming in production (confirmed by metrics)
- [ ] Phase instructions 100% included
- [ ] Avg context ≤3000 chars per phase (down from 9704)
- [ ] Task success rate ≥ baseline (maintained or improved)

**Should Achieve**:
- [ ] 50%+ reduction in context generation cost
- [ ] 90%+ relevance score (manual evaluation)
- [ ] Zero reports of missing critical instructions

---

## Current Status: Phase 1 Complete ✅

**What's Done**:
- ✅ Audit document created
- ✅ Reform plan designed
- ✅ Waste patterns hypothesized
- ✅ Budget configuration documented
- ✅ Critical questions identified

**Next Steps** (Week of 2026-09-02):
1. Implement metrics logging [2.1]
2. Deploy instrumented daemon
3. Start data collection [2.2]

**Blockers**: None - ready to proceed

**Owner**: TBD - needs assignment
