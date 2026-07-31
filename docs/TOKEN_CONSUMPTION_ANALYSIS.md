# Token Consumption Analysis: LLM as Orchestrator vs Heuristics

**Created**: 2026-07-31  
**Status**: Analysis Complete  
**Purpose**: Assess token costs of V2 Ask (LLM Orchestrator) vs V1 Ask (Heuristic Routing) and identify hybrid opportunities

---

## Executive Summary

**Key Findings**:
- ✅ **V2 (LLM Orchestrator) uses 2.5-6x more tokens** than V1 (Heuristics) per query
- ✅ **Primary drivers**: Upfront classification call + full tool exposure
- ✅ **Hybrid opportunities exist** to reduce cost by 40-70% while maintaining LLM orchestration benefits
- ✅ **Prompt caching already mitigates** 90% of repeated context costs

**Recommendation**: **Implement Strategic Hybrid** - use heuristics for fast-path classification, LLM for ambiguous cases and orchestration.

---

## Detailed Breakdown

### V1 Ask: Heuristic-Based Routing

#### Classification Cost: **0 tokens** 🎯

**Implementation**: `copilot_routing.go`
```go
func inferCapabilityRoutingSignal(query string, domain string) (string, string, string) {
    q := strings.ToLower(strings.TrimSpace(query))
    
    // Zero LLM cost - pure keyword matching
    if strings.Contains(q, "git") || strings.Contains(q, "cmd") {
        return intentFamilyLocalOps, capabilityHintDesktopAutomation, ""
    }
    if strings.Contains(q, "fix") || strings.Contains(q, "refactor") {
        return intentFamilyRepoEdit, capabilityHintCodingWorkflow, ""
    }
    return inferIntentFamily(domain, q), capabilityHintGeneral, ""
}
```

**Cost**: 0 tokens (instant classification)

#### Tool Exposure: **250-1,500 tokens** per turn

**Pattern**: Pre-filtered tools from single capability
- Desktop capability: ~8-12 tools (git, shell, file ops)
- Stores capability: ~10-15 tools (list, scan, query, insert, update, delete)
- Coding capability: ~15-25 tools (read, edit, create, delete files)

**Calculation**:
- Tools: 5-15 tools
- Token cost per tool: ~50-100 tokens (name + description + parameters)
- **Total: 250-1,500 tokens**

#### Per-Query Total: **2,000-5,000 tokens**

**Breakdown** (3 ReAct turns average):
- Classification: 0 tokens ✅
- System prompt (base): ~500 tokens
- Tool context: ~250-1,500 tokens per turn × 3 turns = 750-4,500 tokens
- **Total: ~2,000-5,000 tokens per query**

---

### V2 Ask: LLM as Orchestrator

#### Classification Cost: **400-700 tokens** ⚠️

**Implementation**: `classifier_v2.go`
```go
func (a *CopilotAgent) ClassifyTaskContextV2(ctx context.Context, query string, gen ai.Generator) (*TaskContextV2, error) {
    prompt := a.buildClassificationPromptV2(query, storesList, spacesList)
    
    resp, err := gen.Generate(ctx, prompt, ai.GenOptions{
        Temperature: 0.3,
        MaxTokens:   500,  // ⚠️ New LLM call
    })
    
    // Returns: domains, phase, phase_plan, intent, confidence, reasoning
}
```

**Classification Prompt Structure**:
- Instructions: ~200 tokens
- Domain definitions: ~150 tokens
- Phase definitions: ~100 tokens
- Guidelines & examples: ~200 tokens
- Query: 10-50 tokens
- **Input: ~660-700 tokens**

**Classification Response**:
- JSON output: ~50-100 tokens
- **Output: ~50-100 tokens**

**Total Classification Cost**: ~400-700 tokens per query (input + output)

#### Tool Exposure: **1,500-10,000 tokens** per turn 📈

**Pattern**: ALL tools from ALL domain+phase combinations
- Multi-domain support: ["stores", "spaces"] → both domains' tools
- Phase factory: All phase-specific tools exposed
- No pre-filtering (LLM-as-Orchestrator principle)

**Example** (coding_workflow domain, investigation phase):
```go
// Phase Factory creates: coding_workflow_investigation
// Exposes ALL investigation tools:
- investigate_read_file
- investigate_list_directory
- investigate_search_code
- investigate_get_git_status
- investigate_analyze_structure
- investigate_find_references
- investigate_read_multiple_files
- investigate_scan_imports
// + 10-20 more investigation tools
```

**Multi-Domain Example** (["stores", "spaces"]):
```go
// TWO phase proxies created:
// 1. stores_investigation (15 tools)
// 2. spaces_investigation (12 tools)
// Total: 27 tools exposed simultaneously
```

**Calculation**:
- Single domain: 15-30 tools
- Multi-domain: 25-50 tools
- Complex phase (execution): 30-100 tools
- Token cost per tool: ~50-100 tokens
- **Total: 1,500-10,000 tokens per turn**

#### Per-Query Total: **5,000-30,000 tokens** 📊

**Breakdown** (3 ReAct turns average):
- **Classification: 400-700 tokens** ⚠️ (NEW in V2)
- System prompt (base): ~500 tokens
- **Tool context: 1,500-10,000 tokens per turn × 3 turns = 4,500-30,000 tokens** 📈
- **Total: ~5,000-30,000 tokens per query**

---

## Token Cost Comparison

| Component | V1 (Heuristics) | V2 (LLM Orchestrator) | Difference |
|-----------|-----------------|------------------------|------------|
| **Classification** | 0 tokens ✅ | 400-700 tokens ⚠️ | +400-700 |
| **Tool Context (per turn)** | 250-1,500 tokens | 1,500-10,000 tokens 📈 | +1,000-8,500 |
| **3-Turn Query Total** | 2,000-5,000 tokens | 5,000-30,000 tokens | **+2.5-6x** |
| **Cost per Query** | $0.006-0.015 | $0.015-0.090 | **+2.5-6x** |

**Real-World Example** (typical coding query with 3 turns):
- V1: ~3,500 tokens × $0.000003 = **$0.0105**
- V2: ~12,000 tokens × $0.000003 = **$0.0360**
- **Cost increase: 3.4x** 📊

---

## Where Token Consumption Happens

### V2 Ask Pipeline Flow

```
User Query: "Fix the parser bug in src/parser.go"
    ↓
┌─────────────────────────────────────────────┐
│ 1. ClassifyTaskContextV2() LLM Call        │ ⚠️ NEW COST
│    Input: ~700 tokens (prompt + examples)   │
│    Output: ~100 tokens (JSON response)      │
│    COST: ~800 tokens                        │
└─────────────────────────────────────────────┘
    ↓
    LLM Returns:
    {
      "domains": ["coding_workflow"],
      "phase": "investigation",
      "phase_plan": ["investigation", "planning", "execution", "review"],
      "intent": "mutate_data",
      "confidence": "high"
    }
    ↓
┌─────────────────────────────────────────────┐
│ 2. Phase Factory Creates Phases            │
│    coding_workflow_investigation            │
│    Exposes: 25 investigation tools          │
└─────────────────────────────────────────────┘
    ↓
┌─────────────────────────────────────────────┐
│ 3. System Prompt + Tools (Turn 1)          │ 📈 LARGE CONTEXT
│    Base: ~500 tokens                        │
│    Tools: ~2,500 tokens (25 tools × 100)    │
│    History: 0 tokens (first turn)           │
│    COST: ~3,000 tokens                      │
└─────────────────────────────────────────────┘
    ↓
    LLM calls: investigate_read_file
    ↓
┌─────────────────────────────────────────────┐
│ 4. System Prompt + Tools (Turn 2)          │ 📈 REPEATED
│    Base: ~500 tokens                        │
│    Tools: ~2,500 tokens (same 25 tools)     │
│    History: ~500 tokens (turn 1 result)     │
│    COST: ~3,500 tokens                      │
└─────────────────────────────────────────────┘
    ↓
    LLM calls: investigate_analyze_structure
    ↓
┌─────────────────────────────────────────────┐
│ 5. System Prompt + Tools (Turn 3)          │ 📈 REPEATED AGAIN
│    Base: ~500 tokens                        │
│    Tools: ~2,500 tokens (same 25 tools)     │
│    History: ~1,000 tokens (turn 1+2 results)│
│    COST: ~4,000 tokens                      │
└─────────────────────────────────────────────┘
    ↓
    Total Query Cost: ~11,300 tokens
```

**Key Observation**: Tool context (2,500 tokens) is **repeated every turn** and **dominates cost**.

---

## Mitigation: Prompt Caching (Already Implemented) ✅

### Anthropic Prompt Caching

**Status**: ✅ Implemented in `ai/generator/anthropic.go`

**How It Works**:
```go
// First request: Full cost
Request 1: System prompt + tools = 3,000 tokens
  - Cache write: 3,000 × $0.0000037 = $0.0111

// Subsequent requests: 90% savings
Request 2-100: System prompt + tools (cached)
  - Cache read: 3,000 × $0.0000003 = $0.0009 (each)
  - 99 requests: 99 × $0.0009 = $0.0891
  
Total (100 requests): $0.0111 + $0.0891 = $0.1002
Without caching: 100 × $0.0111 = $1.11
SAVINGS: 90% 🎯
```

**See**: `lib/sop/ai/PROMPT_CACHING.md`

**Impact on Token Analysis**:
- ✅ Tool context cost (1,500-10,000 tokens) reduced by 90% after first request
- ⚠️ Classification cost (400-700 tokens) NOT cached (dynamic per query)
- ⚠️ First request still incurs full cost

**Revised V2 Cost** (with caching, 100 requests):
- First request: ~12,000 tokens × $0.0000037 = $0.0444
- Requests 2-100: ~12,000 × 99 × $0.0000003 = $0.3564
- Average per request: $0.0444/100 + $0.3564/100 = **$0.004** 🎯

**Result**: With prompt caching, V2 cost approaches V1 cost for repeated queries in same session.

---

## Hybrid Opportunities

### Strategy 1: Fast-Path Classification for Obvious Cases

**Opportunity**: Use heuristics for "obvious" queries, LLM for ambiguous ones.

#### Fast-Path Candidates (Heuristic Classification)

**Note**: Gate 1 already handles slash commands (`/cmd`, `/c`, `/shell`), prefix routing (`omni:`, `SOP:`), and continuation queries. These examples are for patterns NOT yet covered.

**1. Simple Store Operations**: Clear domain + CRUD keywords
```go
if matches := regexp.MustCompile(`^(list|show|get) (all )?stores?`).FindString(query); matches != "" {
    return &TaskContextV2{
        Domains: []string{"stores"},
        Phase: "investigation",
        PhasePlan: []string{"investigation"},
        Intent: "read_data",
    }
}
```
**Example queries**: "list stores", "show all stores", "get store production"
**Savings**: 400-700 tokens per query

**2. Simple Space Operations**: Clear domain + search keywords
```go
if matches := regexp.MustCompile(`^(search|find in|query) spaces?`).FindString(query); matches != "" {
    return &TaskContextV2{
        Domains: []string{"spaces"},
        Phase: "investigation",
        PhasePlan: []string{"investigation"},
        Intent: "read_data",
    }
}
```
**Example queries**: "search spaces for authentication", "find in spaces kubernetes"
**Savings**: 400-700 tokens per query

**3. Clear File Operations**: "read", "show", "list" + file paths
```go
if isSimpleReadQuery(query) { // "list stores", "show spaces", "get store X"
    domain := extractDomain(query) // stores/spaces
    return &TaskContextV2{
        Domains: []string{domain},
        Phase: "investigation",
        PhasePlan: []string{"investigation"},
        Intent: "read_data",
    }
}
```
**Savings**: 400-700 tokens per query

**4. Natural Language Git Operations** (WITHOUT `/cmd` prefix)
```go
if matches := regexp.MustCompile(`^(commit|push|pull|checkout|branch|merge) `).FindString(query); matches != "" {
    return &TaskContextV2{
        Domains: []string{"desktop"},
        Phase: "execution",
        PhasePlan: []string{"execution"},
        Intent: "mutate_data",
    }
}
```
**Example queries**: "commit the changes", "push to main", "checkout feature-branch"
**Note**: `/cmd git commit` already handled by Gate 1 slash command routing
**Savings**: 400-700 tokens per query

#### LLM Classification (Ambiguous Cases)

**Use LLM when**:
- Multi-domain detection needed ("query stores AND search spaces")
- Complex phase planning needed ("fix bug" → full SDLC)
- Ambiguous intent ("improve performance" → investigation + planning + execution?)
- New/unknown query patterns

**Estimated Fast-Path Hit Rate**: 30-50% of queries
**Potential Savings**: 120-350 tokens per query on average

---

### Strategy 2: Domain-Based Tool Pre-Filtering

**Current V2 Behavior**: Expose ALL tools from ALL domains+phases

**Opportunity**: Pre-filter tools by domain AFTER classification

#### Implementation
```go
// After classification (LLM or heuristic)
classification := getClassification(query) // domains=["stores"], phase="investigation"

// Create ONLY relevant phase proxies (not all domains)
for _, domain := range classification.Domains { // ONLY classified domains
    phase := NewPhase(domain, classification.Phase, infra)
    phases = append(phases, phase)
}

// Expose ONLY domain-specific tools
allTools := []ToolDefinition{}
for _, phase := range phases {
    allTools = append(allTools, phase.ExposeTools()...)
}
// Result: 15 tools instead of 50+
```

**Savings**:
- Single domain: 50-80% tool reduction (15 tools vs 50+)
- Multi-domain: 30-50% tool reduction (25 tools vs 50+)
- Token savings: 1,000-8,000 tokens per turn (3,000-24,000 per query)

**Does This Violate LLM as Orchestrator?**
**NO** ✅ - The LLM still:
1. **Classifies** which domains are relevant (via ClassifyTaskContextV2 or heuristic)
2. **Sees** ALL tools for those classified domains (no further filtering)
3. **Decides** which tools to call
4. **Orchestrates** the execution flow

**Key Difference**: Pre-filtering happens BEFORE LLM sees tools (based on domain classification), not DURING orchestration.

**Example**:
```
Query: "List stores in production"

V2 Current (No Pre-filtering):
  - Domains: ["stores"]
  - Tools shown: stores (15) + spaces (12) + desktop (20) + coding (25) = 72 tools ⚠️
  - LLM must wade through 57 irrelevant tools

V2 Hybrid (Domain Pre-filtering):
  - Domains: ["stores"] ✅
  - Tools shown: stores (15) only ✅
  - LLM focuses on relevant tools
  - Savings: 57 tools × 100 tokens = 5,700 tokens per turn
```

---

### Strategy 3: Phase-Specific Tool Exposure

**Current V2 Behavior**: Phase factory creates phase-specific proxies (investigation, execution, etc.), but exposes ALL tools for that phase

**Opportunity**: Further narrow tools by operation type within phase

#### Example: Investigation Phase Sub-filtering
```go
// Investigation phase tools (current: ALL exposed)
investigation_tools = [
    "read_file",           // read operations
    "list_directory",      // read operations
    "search_code",         // read operations
    "analyze_structure",   // analysis operations
    "find_references",     // analysis operations
    "trace_dependencies",  // analysis operations
    "run_linter",          // validation operations
    "check_types",         // validation operations
]

// Sub-filter by intent from classification
if classification.Intent == "read_data" {
    // Only show read operations (not analysis/validation)
    tools = filterByTag(investigation_tools, "read")
    // Result: 3 tools instead of 8
}
```

**Savings**:
- Intent-based filtering: 40-60% tool reduction within phase
- Token savings: 500-2,000 tokens per turn (1,500-6,000 per query)

**Trade-off**: More rigid, less flexible than full LLM orchestration
**Recommendation**: Avoid for MVP, consider for optimization phase

---

## Recommended Hybrid Approach

### Phase 1: Fast-Path Classification (Easy Win) ✅

**What**: Use heuristics for obvious cases, LLM for ambiguous

**Implementation**:
```go
func (a *CopilotAgent) ClassifyTaskContextV2Hybrid(ctx context.Context, query string, gen ai.Generator) (*TaskContextV2, error) {
    // Fast-path: Direct commands
    if strings.HasPrefix(query, "/cmd") || strings.HasPrefix(query, "@terminal") {
        return &TaskContextV2{
            Domains: []string{"desktop"},
            Phase: "execution",
            PhasePlan: []string{"execution"},
            Confidence: "high",
            Reasoning: "Direct command syntax (fast-path)",
        }, nil
    }
    
    // Fast-path: Git operations
    if isGitOperation(query) {
        return &TaskContextV2{
            Domains: []string{"desktop"},
            Phase: "execution",
            PhasePlan: []string{"execution"},
            Intent: "mutate_data",
            Confidence: "high",
            Reasoning: "Git keyword detected (fast-path)",
        }, nil
    }
    
    // Fast-path: Simple reads
    if isSimpleReadQuery(query) {
        domain := extractDomainKeyword(query)
        if domain != "" {
            return &TaskContextV2{
                Domains: []string{domain},
                Phase: "investigation",
                PhasePlan: []string{"investigation"},
                Intent: "read_data",
                Confidence: "high",
                Reasoning: "Simple read pattern (fast-path)",
            }, nil
        }
    }
    
    // Fallback: LLM classification for ambiguous cases
    return a.ClassifyTaskContextV2(ctx, query, gen)
}
```

**Benefits**:
- ✅ Zero breaking changes (drop-in replacement)
- ✅ 30-50% of queries hit fast-path
- ✅ Savings: 120-350 tokens per query average
- ✅ Faster response (no classification latency)
- ✅ Maintains LLM orchestration for ambiguous cases

**Estimated Impact**:
- Token reduction: 10-15% overall
- Cost reduction: $0.0360 → $0.0306 per query (-15%)
- Latency reduction: 200-500ms for fast-path queries

---

### Phase 2: Domain-Based Tool Pre-Filtering (Medium Win) ✅

**What**: Only expose tools for classified domains

**Implementation**:
```go
func (a *CopilotAgent) createDomainPhases(classification *TaskContextV2) []Capability {
    phases := []Capability{}
    
    // ONLY create phases for classified domains
    for _, domain := range classification.Domains {
        phase := NewPhase(domain, classification.Phase, a.infra)
        phases = append(phases, phase)
    }
    
    return phases // Returns 1-3 phases, not 4+ (all domains)
}
```

**Benefits**:
- ✅ Maintains LLM orchestration (LLM sees all tools for classified domains)
- ✅ Reduces tool context by 50-80%
- ✅ Savings: 3,000-24,000 tokens per query
- ✅ No loss of functionality (LLM already classified domains)

**Estimated Impact**:
- Token reduction: 40-60% overall
- Cost reduction: $0.0360 → $0.0144 per query (-60%)
- Combined with Phase 1: $0.0360 → $0.0122 per query (-66%)

---

### Phase 3: Prompt Caching Optimization (Already Implemented) ✅

**Status**: ✅ Already mitigates 90% of repeated context costs
**See**: `lib/sop/ai/PROMPT_CACHING.md`

**No Action Needed**: System already optimized for repeated queries in same session

---

## Cost-Benefit Analysis

### Baseline (V2 Pure LLM Orchestrator)

| Metric | Value |
|--------|-------|
| Average tokens per query | 12,000 |
| Cost per query | $0.036 |
| Classification latency | 200-500ms |
| Tool context size | 2,500 tokens |

### With Hybrid Phase 1 (Fast-Path Classification)

| Metric | Value | Change |
|--------|-------|--------|
| Average tokens per query | 10,200 | -15% ✅ |
| Cost per query | $0.031 | -15% ✅ |
| Classification latency | 50-500ms | -30% avg ✅ |
| Tool context size | 2,500 tokens | (unchanged) |

### With Hybrid Phase 1 + 2 (Domain Pre-Filtering)

| Metric | Value | Change |
|--------|-------|--------|
| Average tokens per query | 4,000 | **-67%** 🎯 |
| Cost per query | $0.012 | **-67%** 🎯 |
| Classification latency | 50-500ms | -30% avg ✅ |
| Tool context size | 800 tokens | **-68%** 🎯 |

### With Prompt Caching (Already Active)

| Metric | First Query | Subsequent Queries (99) | Average |
|--------|-------------|--------------------------|---------|
| Tokens per query | 12,000 | 1,200 (90% cached) | 2,280 |
| Cost per query | $0.044 | $0.004 | $0.008 |

**Combined (Hybrid + Caching)**:
- First query: $0.012 (hybrid)
- Subsequent: $0.001 (hybrid + caching)
- **Average: $0.002 per query** 🎯
- **Savings vs V2 pure: 94%**
- **Parity with V1: Achieved** ✅

---

## Implementation Complexity

### Fast-Path Classification (Phase 1)

**Complexity**: ⭐ Low
**Effort**: 1-2 days
**Risk**: Low (fallback to LLM for ambiguous cases)
**Files**:
- `classifier_v2.go`: Add fast-path checks before LLM call
- `classifier_v2_test.go`: Add tests for fast-path patterns

### Domain Pre-Filtering (Phase 2)

**Complexity**: ⭐⭐ Medium
**Effort**: 2-3 days
**Risk**: Low (classification already determines domains)
**Files**:
- `copilot_v2.go`: Filter phase creation by classified domains
- `copilot_v2_test.go`: Add tests for domain filtering

---

## Architectural Purity: Does Hybrid Violate LLM as Orchestrator?

### ✅ NO - Hybrid Maintains Core Principles

**LLM as Orchestrator Means**:
1. ✅ **LLM classifies task domains/phases** (Phase 1 preserves this - heuristics only for OBVIOUS cases)
2. ✅ **LLM sees ALL relevant tools** (Phase 2 preserves this - shows all tools for classified domains)
3. ✅ **LLM decides which tools to call** (Preserved - no tool-level pre-filtering)
4. ✅ **LLM controls execution flow** (Preserved - full ReAct orchestration)

**What Changes**:
- 🔧 Classification: Heuristics for 30-50% of obvious queries (fast-path)
- 🔧 Tool exposure: Pre-filter by domain (still show ALL tools for classified domain)

**What Does NOT Change**:
- ✅ LLM reasoning during orchestration
- ✅ Tool calling decisions (LLM-controlled)
- ✅ Execution flow control (LLM-controlled)
- ✅ Multi-turn ReAct loop (LLM-controlled)

### Comparison to Traditional Rule-Based Systems

| Aspect | Traditional | V2 Pure | V2 Hybrid | Preserves Orchestrator? |
|--------|-------------|---------|-----------|------------------------|
| **Classification** | Hardcoded rules | LLM always | LLM (with fast-path) | ✅ YES (LLM for ambiguous) |
| **Tool Selection** | Pre-programmed | LLM decides | LLM decides | ✅ YES |
| **Execution Flow** | Fixed workflow | LLM orchestrates | LLM orchestrates | ✅ YES |
| **Adaptation** | Cannot adapt | Adaptive | Adaptive | ✅ YES |

**Verdict**: Hybrid approach is still "LLM as Orchestrator" - it just avoids asking LLM for trivial decisions.

**Analogy**:
- ❌ Traditional: "Take Main St, turn left at Oak, then right at Elm" (hardcoded route)
- ✅ LLM Pure: "You're at A, destination is B, here's a map with all roads" (LLM decides everything)
- ✅ LLM Hybrid: "You're at A, destination is B, here's a map with roads you can reach" (LLM still decides route, just filtered to reachable roads)

---

## Recommendations

### Immediate Actions (Week 1)

1. ✅ **Implement Fast-Path Classification (Phase 1)**
   - Low complexity, high value
   - 15% token savings
   - 30% latency reduction for obvious queries
   - Zero risk (fallback to LLM)

2. ✅ **Add Telemetry**
   - Track: classification method (fast-path vs LLM)
   - Track: token consumption per query
   - Track: tool count exposed per turn
   - Track: cost per query

### Near-Term Actions (Month 1)

3. ✅ **Implement Domain Pre-Filtering (Phase 2)**
   - Medium complexity, very high value
   - 60% additional token savings (67% combined)
   - Maintains LLM orchestration benefits
   - Low risk (classification already determines domains)

4. ✅ **A/B Test Hybrid vs Pure**
   - Measure: token consumption
   - Measure: user satisfaction (quality of results)
   - Measure: task completion rate
   - Measure: cost per query

### Long-Term Considerations (Month 2+)

5. ⚠️ **Consider Phase-Specific Tool Filtering (Phase 3)**
   - Higher complexity
   - Trade-off: token savings vs flexibility
   - Risk: over-filtering limits LLM's options
   - **Recommendation**: Monitor telemetry first, implement only if needed

6. ✅ **Continue Leveraging Prompt Caching**
   - Already implemented, 90% savings for repeated queries
   - Ensure caching works across domains/phases
   - Monitor cache hit rates

---

## Metrics for Success

### Token Metrics
- ✅ **Target**: Reduce average tokens per query from 12,000 → 4,000 (-67%)
- ✅ **Measure**: Track tokens per query (classification + tool context + turns)
- ✅ **Baseline**: Collect 1 week of pure V2 data before hybrid rollout

### Cost Metrics
- ✅ **Target**: Reduce cost per query from $0.036 → $0.012 (-67%)
- ✅ **Measure**: Track estimated cost per query (tokens × provider rate)
- ✅ **Goal**: Achieve parity with V1 cost when combined with caching

### Quality Metrics
- ✅ **Target**: Maintain or improve task success rate (≥95%)
- ✅ **Measure**: Track task completion, user corrections, retry rate
- ✅ **Baseline**: Compare V2 hybrid vs V2 pure for 1 month

### Latency Metrics
- ✅ **Target**: Reduce classification latency by 30% (500ms → 350ms avg)
- ✅ **Measure**: Time from query to first tool call
- ✅ **Fast-path goal**: <50ms for obvious queries

---

## Conclusion

**LLM as Orchestrator (V2)** provides superior flexibility and intelligence but at **2.5-6x higher token cost** than heuristic routing (V1).

**Strategic Hybrid Approach**:
1. ✅ Use heuristics for obvious fast-path queries (30-50% of queries)
2. ✅ Pre-filter tools by classified domains (not all domains)
3. ✅ Let LLM orchestrate everything else (ambiguous cases, tool selection, flow control)

**Expected Results**:
- **67% token reduction** (12,000 → 4,000 tokens per query)
- **67% cost reduction** ($0.036 → $0.012 per query)
- **94% cost reduction with caching** ($0.036 → $0.002 per query average)
- **Maintains LLM orchestration benefits** (adaptability, intelligence, flexibility)

**Recommendation**: ✅ **Implement hybrid approach** - start with Phase 1 (fast-path), then Phase 2 (domain filtering), measure impact, iterate.

**Next Steps**:
1. Implement fast-path classification in `classifier_v2.go`
2. Add telemetry for token/cost tracking
3. A/B test for 1 week
4. Implement domain pre-filtering if results are positive
5. Monitor and iterate based on metrics
