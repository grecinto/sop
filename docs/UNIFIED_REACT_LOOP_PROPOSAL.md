# Unified ReAct Loop Architecture Proposal

**Date**: 2026-07-31  
**Status**: PROPOSAL - Awaiting approval  
**Goal**: Unify Native and Provider-specific ReAct loops via common engine with adapter interface

---

## Executive Summary

**Proposal**: Extract common ReAct loop orchestration into a **Unified ReAct Engine** that delegates provider-specific behavior to a **ReActLoopAdapter** interface. This eliminates code duplication, ensures all providers get progressive retry improvements automatically, and reduces maintenance burden.

**Current State**: 4 separate loop implementations (~2000 lines duplicated logic)  
**Proposed State**: 1 unified engine + 4 lightweight adapters (~800 lines total)  
**Effort**: ~500-600 lines (engine + interfaces)  
**Timeline**: 2 weeks  
**Risk**: MEDIUM - requires careful migration, but patterns are well-understood

---

## Current Architecture Analysis

### Loop Comparison

| Aspect | Native Loop | ChatGPT Loop | Anthropic Loop | Gemini Loop |
|--------|-------------|--------------|----------------|-------------|
| **Lines of Code** | ~500 | ~600 | ~400 | ~500 |
| **Iteration Control** | Budget-based (5 iters) | Count-based (8 iters) | Count-based (5 iters) | Count-based (5 iters) |
| **LLM Invocation** | `Generator.Generate()` | `createResponsesStream()` (custom) | `Generator.Generate()` | `Generator.Generate()` |
| **Tool Execution** | With retry logic | Pass-through | Pass-through | Pass-through |
| **Continuation** | ToolCallContinuation | previous_response_id | Message thread | ToolCallContinuation |
| **Retry Logic** | ✅ Tier 1/2A/2B/3 | ❌ None | ❌ Basic directives | ❌ Basic directives |
| **Response Building** | EscalationState | No escalation | No escalation | No escalation |
| **State Tracking** | repairAttempts, budgetState | None | None | None |

### Common Pattern (All 4 Loops)

```
1. Setup:
   - Load tools from executor
   - Restore continuations from carryover state
   - Initialize tracking (tool results, executed calls)

2. Iteration Loop (for N iterations):
   a. Prepare prompt/request with continuations
   b. Call LLM (Generator or custom API)
   c. Extract tool calls from response
   d. IF no tool calls → return final response
   e. FOR each tool call:
      - Execute tool
      - Collect result
      - Build continuation
   f. Check termination conditions

3. Completion:
   - Build ReasoningResponse
   - Include carryover state
   - Return
```

**Observation**: 80% of logic is identical. The 20% that differs:
1. How to invoke LLM (3 use Generator, 1 uses custom API)
2. How to format continuations
3. How to handle tool execution errors (Native has retry, others don't)
4. How to prepare next iteration request

---

## Proposed Unified Architecture

### Core Concept

**Unified ReAct Engine** orchestrates the common loop. **ReActLoopAdapter** interface allows providers to customize behavior at specific hook points.

```
┌─────────────────────────────────────────────────────────┐
│           Unified ReAct Engine (New)                    │
│  - Iteration control                                     │
│  - Tool execution orchestration                          │
│  - Retry logic (Tier 1/2A/2B/3) ✅                       │
│  - State tracking (failures, budget)                     │
│  - Response building with EscalationState                │
└────────────────┬────────────────────────────────────────┘
                 │ Uses
                 ▼
┌─────────────────────────────────────────────────────────┐
│        ReActLoopAdapter Interface (New)                  │
│  - PrepareLLMRequest()    → How to call LLM             │
│  - ExtractToolCalls()     → Parse response               │
│  - FormatContinuation()   → Build continuation           │
│  - BuildFinalResponse()   → Assemble result              │
└────────────────┬────────────────────────────────────────┘
                 │ Implemented by
                 ▼
┌──────────────────┬──────────────────┬──────────────────┬──────────────────┐
│  NativeAdapter   │ ChatGPTAdapter   │ AnthropicAdapter │  GeminiAdapter   │
│  (~100 lines)    │  (~150 lines)    │   (~100 lines)   │  (~120 lines)    │
└──────────────────┴──────────────────┴──────────────────┴──────────────────┘
```

---

## Interface Design

### 1. ReActLoopAdapter Interface

```go
// ReActLoopAdapter allows providers to customize specific parts of the ReAct loop
// while inheriting common orchestration, retry logic, and state management.
type ReActLoopAdapter interface {
    // Name returns the adapter identifier (e.g., "chatgpt", "anthropic", "gemini", "native")
    Name() string
    
    // MaxIterations returns the iteration limit for this provider
    MaxIterations() int
    
    // PrepareLLMRequest builds the provider-specific request for the current iteration.
    // The engine provides common state (iteration, tool results, repair directive).
    // Returns: prompt/request, generation options, and optional error.
    PrepareLLMRequest(ctx context.Context, state ReActLoopState) (string, GenOptions, error)
    
    // InvokeLLM calls the provider's LLM API and returns the raw output.
    // Most providers use req.Generator.Generate(), but ChatGPT uses Responses API.
    InvokeLLM(ctx context.Context, prompt string, opts GenOptions, req ReasoningRequest) (GenOutput, error)
    
    // ExtractToolCalls parses tool calls from the LLM response.
    // Returns: tool calls, final text (if no tools), and optional error.
    ExtractToolCalls(output GenOutput) ([]ToolCall, string, error)
    
    // FormatContinuation builds the continuation payload for the next iteration.
    // Returns: updated continuations list.
    FormatContinuation(
        toolCall ToolCall,
        result ReActToolResult,
        existingContinuations []ToolCallContinuation,
    ) []ToolCallContinuation
    
    // BuildFinalResponse assembles the final ReasoningResponse with provider-specific
    // carryover state. The engine provides common fields (tool calls, escalation state).
    BuildFinalResponse(
        finalText string,
        toolCalls []ToolCall,
        toolResults []ReActToolResult,
        continuations []ToolCallContinuation,
        escalationState map[string]any,
    ) ReasoningResponse
    
    // ShouldTerminateEarly checks provider-specific termination conditions.
    // Example: Gemini checks for terminal hints, Native checks budget exhaustion.
    ShouldTerminateEarly(state ReActLoopState) (bool, string)
}

// ReActLoopState captures the current state of the loop, provided to adapters.
type ReActLoopState struct {
    Iteration         int
    Request           ReasoningRequest
    ToolResults       []ReActToolResult
    ExecutedToolCalls []ToolCall
    Continuations     []ToolCallContinuation
    FailureCounts     map[string]int // Tool name → failure count (for retry logic)
    RepairDirective   string
    EscalationState   map[string]any
}
```

---

### 2. Unified ReAct Engine

```go
// UnifiedReActEngine implements the common ReAct loop orchestration.
type UnifiedReActEngine struct {
    Adapter           ReActLoopAdapter
    EnableObfuscation bool
    RetryCoordinator  *RetryCoordinator // Shared retry logic (Tier 2B)
}

func (e *UnifiedReActEngine) Run(ctx context.Context, req ReasoningRequest) (ReasoningResponse, error) {
    // 1. Setup
    tools, err := req.Executor.ListTools(ctx)
    if err != nil {
        return ReasoningResponse{}, fmt.Errorf("failed to list tools: %w", err)
    }
    
    state := ReActLoopState{
        Request:           req,
        ToolResults:       make([]ReActToolResult, 0),
        ExecutedToolCalls: make([]ToolCall, 0),
        Continuations:     make([]ToolCallContinuation, 0),
        FailureCounts:     make(map[string]int),
    }
    
    // Restore continuations from carryover
    if req.CarryoverState != nil {
        state.Continuations = e.Adapter.RestoreContinuations(req.CarryoverState)
    }
    
    // 2. Iteration Loop
    maxIters := e.Adapter.MaxIterations()
    for state.Iteration = 1; state.Iteration <= maxIters; state.Iteration++ {
        if err := ctx.Err(); err != nil {
            return ReasoningResponse{}, err
        }
        
        // Provider prepares request
        prompt, opts, err := e.Adapter.PrepareLLMRequest(ctx, state)
        if err != nil {
            return ReasoningResponse{}, err
        }
        
        // Provider invokes LLM
        output, err := e.Adapter.InvokeLLM(ctx, prompt, opts, req)
        if err != nil {
            return ReasoningResponse{}, fmt.Errorf("LLM invocation failed: %w", err)
        }
        
        // Provider extracts tool calls
        toolCalls, finalText, err := e.Adapter.ExtractToolCalls(output)
        if err != nil {
            return ReasoningResponse{}, err
        }
        
        // No tool calls → done
        if len(toolCalls) == 0 {
            return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
        }
        
        // Execute tools with shared retry logic
        for _, toolCall := range toolCalls {
            result, decision := e.RetryCoordinator.ExecuteWithRetry(
                ctx, 
                toolCall, 
                state.Iteration, 
                maxIters, 
                state.ToolResults,
            )
            
            state.ExecutedToolCalls = append(state.ExecutedToolCalls, toolCall)
            state.ToolResults = append(state.ToolResults, result)
            
            // Handle escalation
            if decision != nil && decision.Escalate {
                state.EscalationState = decision.State
            }
            
            // Update failure tracking
            if decision != nil && !decision.Success {
                state.FailureCounts[toolCall.Name]++
            }
            
            // Provider formats continuation
            state.Continuations = e.Adapter.FormatContinuation(toolCall, result, state.Continuations)
        }
        
        // Check early termination
        if shouldStop, reason := e.Adapter.ShouldTerminateEarly(state); shouldStop {
            log.Info("ReAct loop terminating early", "adapter", e.Adapter.Name(), "reason", reason)
            break
        }
    }
    
    // 3. Build final response
    finalText := "Reached iteration limit"
    return e.Adapter.BuildFinalResponse(finalText, state.ExecutedToolCalls, state.ToolResults, state.Continuations, state.EscalationState), nil
}
```

---

### 3. Retry Coordinator (Shared Module)

```go
// RetryCoordinator encapsulates Tier 1/2A/2B/3 retry logic, shared by all adapters.
type RetryCoordinator struct {
    Executor ai.Executor
}

type RetryDecision struct {
    Success         bool
    Escalate        bool
    Tier            int
    State           map[string]any // EscalationState for Tier 3
    RepairDirective string
}

func (rc *RetryCoordinator) ExecuteWithRetry(
    ctx context.Context,
    toolCall ToolCall,
    iteration int,
    maxIterations int,
    pastResults []ReActToolResult,
) (ReActToolResult, *RetryDecision) {
    // Get last hint for Tier 2A
    lastHint := getLatestHint(pastResults)
    
    // Count failures for this tool
    failureCount := countToolFailures(pastResults, toolCall.Name)
    
    // Execute tool
    rawResult, err := rc.Executor.Execute(ctx, toolCall.Name, toolCall.Args)
    
    if err == nil {
        // Success
        return buildSuccessResult(rawResult), &RetryDecision{Success: true}
    }
    
    // Check if recoverable
    if !isRecoverableToolExecutionError(err) {
        return buildErrorResult(err), &RetryDecision{Success: false}
    }
    
    // Tier 2B: Determine retry tier
    tier := determineRetryTier(err.Error(), failureCount, lastHint)
    
    // Tier 2A: Classify repair strategy
    repairPlan := classifyRecoverableToolRepair(toolCall.Name, err, lastHint)
    repairPlan.Tier = tier
    
    // Tier 3: Escalate if threshold reached
    if tier == TierClarify || shouldEscalateRepairToClarification(failureCount) {
        escalationState := map[string]any{
            "tier":             3,
            "failed_tool":      toolCall.Name,
            "failure_count":    failureCount,
            "strategy":         repairPlan.Strategy,
            "suggested_action": "backward_phase_transition",
            "error_type":       err.Error(),
        }
        
        return buildClarificationResult(repairPlan, err), &RetryDecision{
            Success:  false,
            Escalate: true,
            Tier:     tier,
            State:    escalationState,
        }
    }
    
    // Tier 1/2: Return retry directive
    return buildRetryResult(repairPlan, err), &RetryDecision{
        Success:         false,
        Tier:            tier,
        RepairDirective: formatRepairDirective(repairPlan),
    }
}
```

---

## Adapter Implementations

### Native Adapter (~100 lines)

```go
type NativeAdapter struct {
    generator         ai.Generator
    enableObfuscation bool
}

func (a *NativeAdapter) Name() string { return "native" }
func (a *NativeAdapter) MaxIterations() int { return 5 }

func (a *NativeAdapter) PrepareLLMRequest(ctx context.Context, state ReActLoopState) (string, GenOptions, error) {
    // Build Native-style prompt with MRU state
    prompt := buildNativeReActPrompt(state.Request, state.ToolResults)
    
    opts := GenOptions{
        SystemPrompt:          state.Request.SystemPrompt,
        Temperature:           0.1,
        Tools:                 state.Request.Executor.ListTools(ctx),
        ToolCallContinuations: state.Continuations,
    }
    
    return prompt, opts, nil
}

func (a *NativeAdapter) InvokeLLM(ctx context.Context, prompt string, opts GenOptions, req ReasoningRequest) (GenOutput, error) {
    // Standard Generator.Generate() call
    return req.Generator.Generate(ctx, prompt, opts)
}

func (a *NativeAdapter) ExtractToolCalls(output GenOutput) ([]ToolCall, string, error) {
    // Native uses API-level tool calls
    return output.ToolCalls, output.Text, nil
}

func (a *NativeAdapter) FormatContinuation(toolCall ToolCall, result ReActToolResult, existing []ToolCallContinuation) []ToolCallContinuation {
    // Native doesn't use continuations (stateless per Ask)
    return existing
}

func (a *NativeAdapter) BuildFinalResponse(finalText string, toolCalls []ToolCall, toolResults []ReActToolResult, continuations []ToolCallContinuation, escalationState map[string]any) ReasoningResponse {
    return ReasoningResponse{
        FinalText:       finalText,
        ToolCalls:       toolCalls,
        OutcomeFacts:    SummarizeOutcomeFacts(toolResults),
        OutcomeRecipes:  SummarizeOutcomeRecipes(toolResults),
        EscalationState: escalationState, // ✅ Tier 3 integration
    }
}

func (a *NativeAdapter) ShouldTerminateEarly(state ReActLoopState) (bool, string) {
    // Native uses budget exhaustion (handled by engine)
    return false, ""
}
```

---

### ChatGPT Adapter (~150 lines)

```go
type ChatGPTAdapter struct {
    generator         *chatgpt
    responseIDTracker map[string]string // Tracks previous_response_id
}

func (a *ChatGPTAdapter) Name() string { return "chatgpt" }
func (a *ChatGPTAdapter) MaxIterations() int { return 8 }

func (a *ChatGPTAdapter) PrepareLLMRequest(ctx context.Context, state ReActLoopState) (string, GenOptions, error) {
    // ChatGPT uses structured request, not just prompt
    // Store request in context or state for InvokeLLM
    return state.Request.UserQuery, GenOptions{}, nil
}

func (a *ChatGPTAdapter) InvokeLLM(ctx context.Context, prompt string, opts GenOptions, req ReasoningRequest) (GenOutput, error) {
    // Build Responses API request
    request := buildChatGPTResponsesRequest(req, a.generator.model, opts.Tools)
    
    // Call custom API (not Generator interface)
    response, err := a.generator.createResponsesStream(ctx, request, req.Streamer)
    if err != nil {
        return GenOutput{}, err
    }
    
    // Convert to GenOutput
    toolCalls := openAIResponseToolCalls(response)
    return GenOutput{
        Text:      openAIResponseFinalText(response),
        ToolCalls: toolCalls,
    }, nil
}

func (a *ChatGPTAdapter) FormatContinuation(toolCall ToolCall, result ReActToolResult, existing []ToolCallContinuation) []ToolCallContinuation {
    // ChatGPT uses previous_response_id in request building (handled separately)
    return existing
}

func (a *ChatGPTAdapter) BuildFinalResponse(...) ReasoningResponse {
    resp := ReasoningResponse{
        FinalText:       finalText,
        ToolCalls:       toolCalls,
        OutcomeFacts:    SummarizeOutcomeFacts(toolResults),
        OutcomeRecipes:  SummarizeOutcomeRecipes(toolResults),
        EscalationState: escalationState, // ✅ Now included
    }
    
    // ChatGPT-specific carryover state
    if carryState := buildChatGPTCarryoverState(continuations); carryState != nil {
        carryState.Provider = "chatgpt"
        carryState.Model = a.generator.model
        resp.CarryoverState = carryState
    }
    
    return resp
}
```

---

### Anthropic/Gemini Adapters (~100-120 lines each)

Similar pattern:
- `PrepareLLMRequest()` builds prompt with repair directives
- `InvokeLLM()` uses `Generator.Generate()` with continuations
- `FormatContinuation()` appends to message thread
- `BuildFinalResponse()` includes provider-specific carryover

---

## Benefits of Unified Architecture

### 1. Code Reduction

| Component | Current | Proposed | Savings |
|-----------|---------|----------|---------|
| Native loop | 500 lines | 100 (adapter) | 400 lines |
| ChatGPT loop | 600 lines | 150 (adapter) | 450 lines |
| Anthropic loop | 400 lines | 100 (adapter) | 300 lines |
| Gemini loop | 500 lines | 120 (adapter) | 380 lines |
| **Shared engine** | 0 lines | 400 lines | -400 lines |
| **Retry coordinator** | 0 lines | 200 lines | -200 lines |
| **TOTAL** | **2000 lines** | **1070 lines** | **930 lines (47% reduction)** |

### 2. Automatic Feature Propagation

**Current**: Tier 1/2A/2B/3 only in Native loop  
**Proposed**: All providers get improvements automatically via shared engine

| Feature | Native | ChatGPT | Anthropic | Gemini |
|---------|--------|---------|-----------|--------|
| **Current State** |
| Progressive retry | ✅ | ❌ | ❌ | ❌ |
| Escalation state | ✅ | ❌ | ❌ | ❌ |
| Failure tracking | ✅ | ❌ | ❌ | ❌ |
| **After Unification** |
| Progressive retry | ✅ | ✅ | ✅ | ✅ |
| Escalation state | ✅ | ✅ | ✅ | ✅ |
| Failure tracking | ✅ | ✅ | ✅ | ✅ |

### 3. Single Maintenance Point

**Current**: Bug fixes/improvements need 4× implementation  
**Proposed**: Fix once in engine, all providers benefit

**Example**: Tier 2C adaptive learning (future)
- Current: ~200 lines × 4 loops = 800 lines
- Proposed: ~200 lines in engine = 200 lines

### 4. Easier Testing

**Current**: Test each loop independently (4 test suites)  
**Proposed**: Test engine once + minimal adapter tests

```
Engine tests:        ~500 test cases (covers 95% of logic)
Adapter tests:       ~50 test cases each × 4 = 200 test cases
TOTAL:              ~700 test cases vs ~2000 current
```

### 5. Consistent Behavior

**Current**: Providers have different retry thresholds, different termination logic  
**Proposed**: Unified behavior with provider-specific customization only where needed

---

## Migration Strategy

### Implementation Status

✅ **Phase 1 COMPLETE** (Week 1) - Commit 8d0467b85
- ReActLoopAdapter interface (~140 lines)
- RetryCoordinator (~250 lines)
- UnifiedReActEngine (~270 lines)
- Interface updates (ResultFile/ResultSize)
- All tests passing, no regressions

🔄 **Phase 2 IN PROGRESS** (Week 2)
- Native adapter implementation
- Migration of Native loop

⏳ **Phases 3-5 PENDING**

---

### Phase 1: Extract Shared Engine (Week 1) ✅ COMPLETE

**Goal**: Create UnifiedReActEngine without breaking existing code

1. Create `lib/sop/ai/agent/unified_react_engine.go`
2. Create `lib/sop/ai/agent/react_loop_adapter.go` (interface)
3. Create `lib/sop/ai/agent/retry_coordinator.go` (shared retry logic)
4. Extract common orchestration from Native loop
5. Unit test engine with mock adapter

**Deliverable**: Working engine + tests, no production impact

---

### Phase 2: Implement Native Adapter (Week 2)

**Goal**: Migrate Native loop to use unified engine

1. Create `lib/sop/ai/agent/native_adapter.go`
2. Implement ReActLoopAdapter interface
3. Update `engine_native.go` to delegate to unified engine
4. Run full test suite
5. Verify no regression

**Deliverable**: Native loop using unified engine, all tests passing

---

### Phase 3: Implement ChatGPT Adapter (Week 3)

**Goal**: Migrate ChatGPT loop to unified engine (60% of traffic)

1. Create `lib/sop/ai/generator/chatgpt_adapter.go`
2. Implement ReActLoopAdapter interface
3. Handle Responses API special case
4. Update `chatgpt_react_loop.go` to use engine
5. Run ChatGPT-specific tests

**Deliverable**: ChatGPT using unified engine with retry logic

---

### Phase 4: Implement Anthropic & Gemini Adapters (Week 4)

**Goal**: Complete migration for all providers

1. Create `lib/sop/ai/generator/anthropic_adapter.go`
2. Create `lib/sop/ai/generator/gemini_adapter.go`
3. Migrate both loops
4. Full integration testing

**Deliverable**: All 4 loops using unified engine

---

### Phase 5: Cleanup & Documentation (Week 5)

**Goal**: Remove old code, document new architecture

1. Delete old loop implementations
2. Update documentation
3. Performance benchmarking
4. Production rollout monitoring

---

## Risk Assessment

### Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| **Breaking conversation continuation** | HIGH | Careful adapter testing, preserve continuation formats exactly |
| **Performance regression** | MEDIUM | Benchmark before/after, optimize hot paths |
| **Increased complexity** | LOW | Interface is cleaner than 4 separate implementations |
| **Migration bugs** | MEDIUM | Gradual rollout, feature flags per provider |

### Rollback Plan

Each phase can be rolled back independently:
- Phase 2: Keep old `engine_native.go` runDefaultLoop(), switch via flag
- Phase 3-4: Keep old provider loops, switch via `ReActLoop()` return value
- Full rollback: Remove unified engine, restore old code

---

## Open Questions

### 1. Should adapters own LLM invocation?

**Current Proposal**: Yes - `InvokeLLM()` in adapter  
**Alternative**: Engine calls `req.Generator.Generate()`, adapters only format requests

**Recommendation**: Keep in adapter - ChatGPT needs custom API access

---

### 2. How to handle provider-specific streaming?

**Current Proposal**: Adapter's `InvokeLLM()` handles streaming  
**Alternative**: Engine streams, adapter formats events

**Recommendation**: Keep in adapter - streaming formats differ (ChatGPT SSE, Anthropic NDJSON)

---

### 3. Should we extract prompt building to adapters?

**Current Proposal**: Yes - `PrepareLLMRequest()` builds full prompt  
**Alternative**: Engine builds prompt, adapter decorates

**Recommendation**: Keep in adapter - Native has complex MRU prompt logic

---

### 4. How to handle iteration budget vs count?

**Current Proposal**: Adapter returns `MaxIterations()`, engine enforces  
**Alternative**: Adapter provides budget calculation function

**Recommendation**: Keep simple - max iterations is sufficient, budget logic can stay in Native adapter

---

## Performance Considerations

### Potential Overhead

1. **Interface dispatch**: ~1-2% overhead (negligible for LLM calls)
2. **State copying**: ReActLoopState passed to adapter (~100 bytes)
3. **Additional function calls**: ~5-10 extra calls per iteration

**Estimated Impact**: <1% total latency increase (LLM calls dominate at ~500ms-2s per iteration)

### Optimization Opportunities

1. **Shared prompt building**: Cache common prompt components
2. **Shared tool listing**: List tools once, pass to all adapters
3. **Parallel tool execution**: Engine can parallelize tool calls (future)

---

## Success Metrics

### Code Quality
- ✅ 47% code reduction (2000 → 1070 lines)
- ✅ Single source of truth for retry logic
- ✅ All providers get Tier 1/2A/2B/3 automatically

### Functionality
- ✅ No regression in existing behavior
- ✅ All 95% of users get progressive retry
- ✅ Automatic phase succession for all providers

### Maintenance
- ✅ 4× reduction in future improvement effort
- ✅ 65% reduction in test code
- ✅ Consistent behavior across providers

---

## Recommendation

**✅ PROCEED with unified architecture.**

**Why**:
1. **Clear win**: 47% code reduction + automatic feature propagation
2. **Low risk**: Gradual migration with rollback per phase
3. **High value**: Future improvements benefit all providers automatically
4. **Proven pattern**: Adapter pattern is well-understood and testable

**Next Steps**:
1. Review and approve this proposal
2. Begin Phase 1 (extract shared engine)
3. Weekly check-ins during 5-week migration
4. Success criteria: All tests passing, no performance regression, all providers using engine

---

## Related Documentation

- [REACT_LOOP_SYNC_GAP_ASSESSMENT.md](./REACT_LOOP_SYNC_GAP_ASSESSMENT.md) - Current gap analysis
- [TIER2B_PROGRESSIVE_ESCALATION_IMPLEMENTATION.md](./TIER2B_PROGRESSIVE_ESCALATION_IMPLEMENTATION.md) - Retry logic to unify
- [ARCHITECTURE_PATTERNS.md](./ARCHITECTURE_PATTERNS.md) - Architecture principles
- [ASK_V2_ARCHITECTURE.md](./ASK_V2_ARCHITECTURE.md) - Ask pipeline context

---

**Status**: ✅ **VIABLE** - Recommend proceeding with 5-phase migration  
**Effort**: 5 weeks  
**Value**: High (code reduction + automatic feature propagation)  
**Risk**: Medium (mitigated by gradual rollout)
