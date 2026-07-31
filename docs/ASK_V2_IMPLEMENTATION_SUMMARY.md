# AskV2 Implementation Summary

**Created**: 2026-07-30  
**Status**: ✅ Implementation Complete - Ready for Testing  
**Location**: `/Volumes/T7/sop-cloud/lib/sop/ai/agent/`

---

## Files Created

### 1. `classifier_v2.go` (348 lines)
**Purpose**: Enhanced LLM classification for multi-domain + phase detection

**Key Functions**:
- `ClassifyTaskContextV2()` - LLM analyzes query, returns:
  - `Domains`: One or more domains (e.g., ["stores", "spaces"])
  - `Phase`: Current phase (e.g., "investigation")
  - `PhasePlan`: Full sequence (e.g., ["investigation", "execution"])
  - `Intent`: High-level intent ("read_data", "mutate_data", etc.)
  - `Confidence`: LLM's confidence level

**No Heuristics**: Pure LLM reasoning, no keyword matching

### 2. `copilot_v2.go` (469 lines)
**Purpose**: New Ask pipeline with LLM-as-Orchestrator + Phase Factory

**Key Functions**:
- `AskV2()` - Main entry point (7-step pipeline)
- `resolvePhasePlanV2()` - PhasePlan resolution
- `createPhaseProxiesV2()` - Phase Factory invocation
- `aggregatePhaseToolsV2()` - Tool collection
- `registerPhaseToolsV2()` - Tool registration
- `buildSystemPromptV2()` - Unified prompt builder
- `isPhaseCompleteV2()` - Phase completion detection
- `getNextPhase()` - Phase succession logic

**Architectural Patterns Enforced**:
✅ Explicit parameter passing (no context for state)
✅ Participant pattern (phases are passive)
✅ Ask is orchestrator (not phases)
✅ No controller in ExecuteTool

### 3. `docs/ASK_V2_ARCHITECTURE.md` (500+ lines)
**Purpose**: Complete architectural documentation

**Contents**:
- User specification flow diagram
- Step-by-step execution examples
- Architectural pattern compliance validation
- Anti-pattern examples (what NOT to do)
- Test cases and success metrics

### 4. Modified: `copilot.go`
**Changes**: Added feature flag routing

```go
func (a *CopilotAgent) Ask(ctx, query, cfg) {
    // Feature flag check
    if cfg["use_v2_pipeline"] == true {
        return a.AskV2(ctx, query, cfg)  // New pipeline
    }
    return a.askV1(ctx, query, cfg)  // Legacy pipeline
}
```

---

## How to Use V2

### Enable V2 Pipeline

```go
// In your code calling Ask()
cfg := &ai.ConfigMap{
    "use_v2_pipeline": true,  // Enable V2
}

response, err := agent.Ask(ctx, "List stores and search spaces for 'auth'", cfg)
```

### Test V2 Classification

```go
classification, err := agent.ClassifyTaskContextV2(ctx, query, gen)
fmt.Printf("Domains: %v\n", classification.Domains)
fmt.Printf("Phase: %s\n", classification.Phase)
fmt.Printf("Plan: %v\n", classification.PhasePlan)
```

### Verify Phase Factory Integration

```go
// V2 automatically creates phase proxies
phases := agent.createPhaseProxiesV2(["stores", "spaces"], "investigation")
// → StoresPhase("investigation") + SpacesPhase("investigation")

// Each phase exposes domain+phase tools
tools := agent.aggregatePhaseToolsV2(phases)
// → [investigation_list_stores, investigation_scan, investigation_read_config, ...]
```

---

## Architecture Validation

### ✅ Pattern 1: Explicit Parameters
```go
// State passed explicitly, not hidden in context
classification, plan, currentPhase := ...
phases := createPhaseProxiesV2(domains, currentPhase)  // ✅
registerPhaseToolsV2(ctx, allTools, phases, classification)  // ✅
```

### ✅ Pattern 2: Participant Pattern
```go
// Phases are passive participants
phase.ExposeTools()           // Declare tools
phase.BuildInstructions()     // Provide guidance
phase.ExecuteTool(tool, args) // Execute ONE tool, return result
// NO loops, NO enqueuing, NO orchestration in phase
```

### ✅ Pattern 3: Ask is Orchestrator
```go
AskV2() {
    classification := ClassifyTaskContextV2()  // Ask controls
    phases := createPhaseProxies()             // Ask controls
    registerTools()                            // Ask controls
    reactLoop()                                // LLM controls tools
    if phaseComplete {
        queue.Enqueue(nextPhase)               // Ask controls succession
    }
}
```

### ✅ Pattern 4: No Controller in Phases
```go
// ExecuteTool is simple dispatch
func (p *Phase) ExecuteTool(toolName, args) {
    return p.performOperation(args)  // Execute, return
    // NO for loops, NO multiple enqueues
}
```

---

## Migration Path

### Week 1: Testing (Current)
```bash
# Test V2 with feature flag
go test -v -run TestAskV2Classification
go test -v -run TestAskV2SingleDomain
go test -v -run TestAskV2MultiDomain
go test -v -run TestAskV2MultiPhase
```

### Week 2-3: Validation
- Compare V1 vs V2 behavior on same queries
- Validate multi-domain support
- Test phase succession
- Verify LLM tool orchestration

### Week 4: Default Switch
```go
// Change default to V2
if cfg["use_v1_pipeline"] == true {
    return a.askV1()  // Opt-in to legacy
}
return a.AskV2()  // Default to V2
```

### Week 5: Deprecation
- Remove V1 pipeline
- Remove heuristic routing code
- Clean up capability.Classify() methods

---

## Test Cases

### TC1: Single Domain, Single Phase
```go
Query: "List stores in production database"
Expected Classification:
  Domains: ["stores"]
  Phase: "investigation"
  PhasePlan: ["investigation"]

Expected Behavior:
  1. StoresPhase("investigation") created
  2. Tools: [investigation_list_stores, investigation_scan, ...]
  3. LLM sees all stores investigation tools
  4. LLM calls investigation_list_stores
  5. Result returned to user
```

### TC2: Multi-Domain, Single Phase
```go
Query: "Query stores and search spaces for 'authentication'"
Expected Classification:
  Domains: ["stores", "spaces"]
  Phase: "investigation"
  PhasePlan: ["investigation"]

Expected Behavior:
  1. StoresPhase("investigation") + SpacesPhase("investigation") created
  2. Tools: [stores tools] + [spaces tools]
  3. LLM sees ALL tools from both domains
  4. LLM orchestrates: list_stores → search_space
  5. Unified result to user
```

### TC3: Single Domain, Multi-Phase
```go
Query: "Refactor authentication to use OAuth2"
Expected Classification:
  Domains: ["coding_workflow"]
  Phase: "intake"
  PhasePlan: ["intake", "investigation", "planning", "execution", "review"]

Expected Behavior:
  1. CodingPhase("intake") created
  2. Intake tools exposed
  3. Intake phase completes
  4. AskV2 enqueues next: CodingPhase("investigation")
  5. Work queue processes phases sequentially
```

### TC4: Multi-Domain, Multi-Phase
```go
Query: "Migrate user data from stores to spaces with validation"
Expected Classification:
  Domains: ["stores", "spaces"]
  Phase: "investigation"
  PhasePlan: ["investigation", "planning", "execution", "review"]

Expected Behavior:
  1. Phase 1: Investigate both stores and spaces
  2. Phase 2: Plan migration strategy
  3. Phase 3: Execute migration
  4. Phase 4: Review/validate results
```

---

## Key Advantages of V2

### 1. No Heuristics
**V1**: Keywords decide domains/capabilities
**V2**: LLM classifies based on query semantics

### 2. Multi-Domain Support
**V1**: Single domain only
**V2**: Native multi-domain (stores + spaces + desktop)

### 3. Phase Awareness
**V1**: No phase concept
**V2**: LLM-classified phases with domain-specific tools

### 4. Pure LLM Orchestration
**V1**: Pre-filtered tool set → LLM
**V2**: Full tool inventory → LLM decides

### 5. Cleaner Architecture
**V1**: Heuristics in 4 layers (routing, classify, capability, tools)
**V2**: Single LLM classification → Phase Factory → Tools

---

## Next Steps

### Immediate (Day 1)
1. ✅ Review V2 implementation
2. ✅ Verify architectural patterns
3. Run basic sanity tests
4. Test single-domain queries

### Short-term (Week 1)
1. Write comprehensive unit tests
2. Test multi-domain queries
3. Test multi-phase plans
4. Compare V1 vs V2 outputs

### Medium-term (Weeks 2-3)
1. Integration testing
2. Performance benchmarking
3. Bug fixes and refinements
4. Documentation improvements

### Long-term (Weeks 4-5)
1. Enable V2 by default
2. Deprecate V1
3. Remove heuristic code
4. Clean up capability system

### Proposed Enhancements

**Conversational Feedback System** (Documented in [ASK_V2_ARCHITECTURE.md](ASK_V2_ARCHITECTURE.md#conversational-feedback-system)):
- **Grounding Echo**: Confirm LLM understanding after classification, before execution
- **Plan Description**: Show execution plan before ReAct loop starts
- **Findings Summary**: Highlight significant discoveries during tool execution
- **Collapsible UI**: User-controlled visibility for all conversational chatter

**Benefits**:
- Transparency in LLM reasoning
- Opportunity for user interjection before execution
- Better debugging visibility
- Improved user confidence in agent understanding

**Status**: Architecture designed, ready for implementation

---

## Files Modified Summary

| File | Type | Lines | Purpose |
|------|------|-------|---------|
| `classifier_v2.go` | New | 348 | Enhanced LLM classification |
| `copilot_v2.go` | New | 469 | V2 Ask pipeline |
| `docs/ASK_V2_ARCHITECTURE.md` | New | 500+ | Architecture docs |
| `copilot.go` | Modified | +30 | Feature flag routing |

**Total New Code**: ~817 lines  
**Documentation**: 500+ lines  

---

## Status: ✅ Ready for Testing

The V2 implementation is complete and follows all architectural patterns:
- ✅ Explicit parameter passing
- ✅ Participant pattern
- ✅ Ask is orchestrator
- ✅ No controller in phases
- ✅ Pure LLM orchestration
- ✅ Phase Factory integration
- ✅ Multi-domain support

**No heuristics. No keyword matching. Pure LLM reasoning.**
