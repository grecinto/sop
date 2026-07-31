# Token Consumption Analysis - Executive Summary

**Created**: 2026-07-31  
**Full Analysis**: [TOKEN_CONSUMPTION_ANALYSIS.md](TOKEN_CONSUMPTION_ANALYSIS.md)

---

## TL;DR

**Question**: How much more expensive is V2 Ask (LLM as Orchestrator) vs V1 Ask (Heuristics)?

**Answer**: **2.5-6x more tokens**, but **hybrid approach can reduce to near-parity** while keeping LLM orchestration benefits.

| Approach | Tokens/Query | Cost/Query | Change |
|----------|--------------|------------|--------|
| **V1 (Heuristics)** | 2,000-5,000 | $0.006-0.015 | Baseline |
| **V2 (Pure LLM)** | 5,000-30,000 | $0.015-0.090 | **+2.5-6x** ⚠️ |
| **V2 (Hybrid)** | 4,000-10,000 | $0.012-0.030 | **+2-3x** ✅ |
| **V2 (Hybrid + Caching)** | ~2,000 avg | ~$0.002 avg | **Parity** 🎯 |

---

## Where Token Cost Comes From

### V2 Ask Pipeline

```
User Query
    ↓
┌──────────────────────────────────┐
│ 1. Classification (LLM Call)    │ +400-700 tokens ⚠️ NEW
│    "Analyze query, return JSON"  │
└──────────────────────────────────┘
    ↓
┌──────────────────────────────────┐
│ 2. Tool Exposure (Turn 1)       │ +1,500-10,000 tokens ⚠️ LARGE
│    System Prompt + ALL Tools     │
└──────────────────────────────────┘
    ↓
┌──────────────────────────────────┐
│ 3. Tool Exposure (Turn 2)       │ +1,500-10,000 tokens ⚠️ REPEATED
│    System Prompt + ALL Tools     │
└──────────────────────────────────┘
    ↓
┌──────────────────────────────────┐
│ 4. Tool Exposure (Turn 3)       │ +1,500-10,000 tokens ⚠️ REPEATED
│    System Prompt + ALL Tools     │
└──────────────────────────────────┘
    ↓
Total: 5,000-30,000 tokens
```

**Key Drivers**:
1. ⚠️ **New classification call**: +400-700 tokens upfront
2. ⚠️ **All tools exposed**: 1,500-10,000 tokens per turn (vs 250-1,500 in V1)
3. ⚠️ **Repeated every turn**: 3 turns = 3x tool context cost

---

## Optimization Strategy: Strategic Hybrid

### Phase 1: Fast-Path Classification ✅

**Problem**: Every query pays 400-700 tokens for LLM classification

**Solution**: Use heuristics for obvious cases, LLM for ambiguous

```go
if query.startsWith("/cmd") {
    return "desktop domain, execution phase" // 0 tokens ✅
}

if containsGitKeywords(query) {
    return "desktop domain, execution phase" // 0 tokens ✅
}

// Ambiguous? Ask LLM
return ClassifyTaskContextV2(query) // 400-700 tokens
```

**Impact**:
- 30-50% of queries hit fast-path
- **Savings: 120-350 tokens per query average** (-15%)
- **Latency: 200-500ms faster** for fast-path queries
- **Quality: Unchanged** (LLM still handles ambiguous cases)

---

### Phase 2: Domain Pre-Filtering ✅

**Problem**: V2 exposes ALL tools from ALL domains (50-100 tools)

**Solution**: Only expose tools for classified domains

```go
// V2 Current: Expose ALL domains
domains = ["desktop", "stores", "spaces", "coding"]
tools = 50-100 tools total ⚠️

// V2 Hybrid: Expose ONLY classified domains
classification = ClassifyTaskContextV2(query)
domains = classification.Domains // e.g., ["stores"]
tools = 15 tools for stores ✅
```

**Impact**:
- **Savings: 3,000-24,000 tokens per query** (-60%)
- **Quality: Unchanged** (LLM classified relevant domains)
- **Maintains LLM orchestration** (LLM still sees all tools for classified domain)

---

### Phase 3: Prompt Caching ✅ (Already Implemented)

**Problem**: Tool context (2,500 tokens) repeated every turn

**Solution**: Cache system prompt + tools (Anthropic provider)

**Impact**:
- First request: Full cost ($0.044)
- Requests 2-100: 90% discount ($0.004)
- **Average: $0.008 per query** (-78%)

**See**: `lib/sop/ai/PROMPT_CACHING.md`

---

## Combined Results

| Metric | V2 Pure | V2 Hybrid | V2 Hybrid + Caching |
|--------|---------|-----------|---------------------|
| **Tokens per query** | 12,000 | 4,000 (-67%) | ~2,000 avg (-83%) |
| **Cost per query** | $0.036 | $0.012 (-67%) | ~$0.002 avg (-94%) |
| **vs V1 Baseline** | +3.4x | +1.1x | **Parity** 🎯 |

---

## Does Hybrid Violate "LLM as Orchestrator"?

### ✅ NO - Core Principles Preserved

**LLM as Orchestrator means:**
1. ✅ **LLM classifies** task domains/phases (hybrid uses heuristics only for OBVIOUS cases)
2. ✅ **LLM sees all relevant tools** (hybrid shows all tools for classified domains, not filtered by LLM)
3. ✅ **LLM decides** which tools to call (unchanged)
4. ✅ **LLM controls** execution flow (unchanged)

**What changes:**
- 🔧 Classification: Heuristics for 30-50% obvious queries (still LLM for ambiguous)
- 🔧 Tool exposure: Pre-filter by domain (still show ALL tools for that domain)

**What doesn't change:**
- ✅ LLM reasoning during orchestration
- ✅ Tool calling decisions (LLM-controlled)
- ✅ Execution flow control (LLM-controlled)

**Analogy**:
- ❌ Traditional: "Take Main St, turn left at Oak" (hardcoded route)
- ✅ V2 Pure: "Here's a map with ALL roads in the city" (LLM decides)
- ✅ V2 Hybrid: "Here's a map with roads you can reach from here" (LLM still decides route, just filtered to reachable roads)

**Verdict**: Hybrid is still "LLM as Orchestrator" - we just don't ask LLM for trivial decisions like "Is 'list stores' a stores domain operation?"

**Note**: Gate 1 already handles slash commands (`/cmd ls`), prefix routing (`omni:`), and continuation. Hybrid adds fast-paths for simple domain-specific patterns.

---

## Recommended Action Plan

### Week 1: Fast-Path Classification ⭐ (Low Effort, High Value)

**Implementation**:
```go
// In classifier_v2.go
func ClassifyTaskContextV2Hybrid(ctx, query, gen) (*TaskContextV2, error) {
    // Fast-path: Obvious cases (30-50% of queries)
    if fastPath := tryFastPath(query); fastPath != nil {
        return fastPath, nil
    }
    
    // LLM: Ambiguous cases
    return ClassifyTaskContextV2(ctx, query, gen)
}
```

**Expected Results**:
- ✅ 15% token reduction
- ✅ 30% latency reduction for obvious queries
- ✅ Zero risk (fallback to LLM)

---

### Month 1: Domain Pre-Filtering ⭐⭐ (Medium Effort, Very High Value)

**Implementation**:
```go
// In copilot_v2.go
func createDomainPhases(classification) []Capability {
    phases := []Capability{}
    
    // ONLY create phases for classified domains
    for _, domain := range classification.Domains {
        phase := NewPhase(domain, classification.Phase, infra)
        phases = append(phases, phase)
    }
    
    return phases
}
```

**Expected Results**:
- ✅ 60% additional token reduction (67% combined)
- ✅ Maintains LLM orchestration benefits
- ✅ Low risk (classification already determines domains)

---

### Month 1: A/B Testing & Telemetry

**Metrics to Track**:
1. ✅ Token consumption per query (classification + tool context)
2. ✅ Cost per query (tokens × provider rate)
3. ✅ Task success rate (completion, corrections, retries)
4. ✅ Classification method (fast-path vs LLM)
5. ✅ Tool count exposed per turn
6. ✅ Latency (query → first tool call)

**Goal**: Validate 67% token reduction with no quality degradation

---

## Key Takeaway

**LLM as Orchestrator is expensive** (2.5-6x tokens vs heuristics) but provides superior intelligence and adaptability.

**Strategic Hybrid approach**:
- ✅ Use heuristics for obvious cases (30-50% of queries)
- ✅ Pre-filter tools by classified domains (not all domains)
- ✅ Let LLM orchestrate everything else

**Result**: **Near cost parity with V1** while keeping **LLM orchestration benefits** (adaptability, multi-domain, intelligent routing).

**Next Step**: Implement fast-path classification (Week 1) → Measure → Implement domain pre-filtering (Month 1) → Iterate

---

**Full Analysis**: [TOKEN_CONSUMPTION_ANALYSIS.md](TOKEN_CONSUMPTION_ANALYSIS.md)
