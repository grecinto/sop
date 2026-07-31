# Ask V2 Architecture - LLM-as-Orchestrator with Phase Factory

**Created**: 2026-07-30  
**Status**: Implementation In Progress  
**Purpose**: Pure LLM orchestration with domain-agnostic phase system

---

## Core Principle: LLM Makes ALL Decisions

The V2 pipeline removes ALL heuristics. The LLM:
1. **Classifies** domain(s) and phase(s) from user query
2. **Sees** ALL available tools for selected domain+phase combinations
3. **Decides** which tools to call based on descriptions and context
4. **Orchestrates** the entire workflow via tool calling

**Zero keyword matching. Zero pre-filtering. Pure LLM reasoning.**

---

## User Specification

### Step 1: LLM Classification
```
LLM analyzes query and returns:
{
  "domains": ["stores", "spaces"],      // Can be multiple
  "phase": "investigation",             // Current phase
  "phase_plan": ["investigation", "execution"],  // Full plan (can be single phase)
  "intent": "read_data",
  "confidence": "high"
}
```

**Examples**:
- `"List stores"` → Domains: ["stores"], Phase: "investigation", Plan: ["investigation"]
- `"Query stores and search spaces"` → Domains: ["stores", "spaces"], Phase: "investigation"  
- `"Fix parser bug"` → Domains: ["coding_workflow"], Phase: "investigation", Plan: ["investigation", "planning", "execution", "review"]

### Step 2: PhasePlan Resolution
```go
// Use LLM-generated plan or default to single phase
if len(classification.PhasePlan) > 0 {
    plan = classification.PhasePlan
} else {
    plan = []string{classification.Phase}
}

// Store in WorkStream
workstream.SetPhasePlan(plan, checkpoints, reasoning, confidence)

// Get current phase (first in plan or resume from workstream)
currentPhase = plan[0]
```

### Step 3: Phase Factory Creates Domain Proxies
```go
// For EACH domain, create a phase capability
phases := []Capability{}
for _, domain := range classification.Domains {
    phase := NewPhase(domain, currentPhase, sharedInfra)
    phases = append(phases, phase)
}

// Examples:
// Domains=["stores"], Phase="investigation"
//   → StoresPhase("investigation")
//   → Tools: [investigation_list_stores, investigation_scan, investigation_describe]
//
// Domains=["stores", "spaces"], Phase="investigation"  
//   → StoresPhase("investigation") + SpacesPhase("investigation")
//   → Tools: [stores tools...] + [spaces tools...]
```

### Step 4: Aggregate Tools from ALL Phases
```go
// Collect tools from ALL domain phases
allTools := []ToolDefinition{}
for _, phase := range phases {
    tools := phase.ExposeTools()
    allTools = append(allTools, tools...)
}

// Register ALL tools - no filtering
registerTools(allTools)
```

**Key Point**: LLM sees the FULL tool inventory. No pre-selection.

### Step 5: Build System Prompt
```go
// Get instructions from each phase
instructions := []string{}
for _, phase := range phases {
    inst := phase.BuildInstructions(query, state)
    instructions = append(instructions, inst)
}

// Assemble system prompt
systemPrompt := buildSystemPromptV2(
    persona,
    context,
    instructions,  // Phase-specific guidance
    conversationHistory,
    query
)
```

### Step 6: ReAct Loop (Pure LLM Orchestration)
```go
// LLM receives:
// - System prompt with phase instructions
// - FULL tool inventory (all domain+phase tools)
// - User query

response := reactLoop(ctx, query, gen, systemPrompt)

// LLM decides:
// - Which tool(s) to call
// - What arguments to pass
// - When to call another tool vs respond to user
// - Whether to jump to next phase
```

### Step 7: Phase Succession
```go
// After ReAct completes, check if phase is done
if isPhaseComplete(toolCalls, response) {
    nextPhaseIndex := getCurrentPhaseIndex() + 1
    if nextPhaseIndex < len(plan) {
        queue.Enqueue(WorkItem{
            Phase: plan[nextPhaseIndex],
            Query: originalQuery,
            Actor: "phase_transition"
        })
    }
}
```

---

## Flow Diagram

```
User: "List stores and search spaces for 'auth'"
    ↓
┌─────────────────────────────────────────┐
│ Step 1: LLM Classification              │
│ ClassifyTaskContextV2(query, gen)       │
└─────────────────────────────────────────┘
    ↓
Classification Result:
{
  "domains": ["stores", "spaces"],
  "phase": "investigation",
  "phase_plan": ["investigation"],
  "intent": "read_data"
}
    ↓
┌─────────────────────────────────────────┐
│ Step 2: PhasePlan Resolution            │
│ plan = ["investigation"]                │
│ currentPhase = "investigation"          │
└─────────────────────────────────────────┘
    ↓
┌─────────────────────────────────────────┐
│ Step 3: Phase Factory                   │
│ NewPhase("stores", "investigation")     │
│ NewPhase("spaces", "investigation")     │
└─────────────────────────────────────────┘
    ↓
StoresPhase("investigation") → Tools:
  - investigation_list_stores
  - investigation_scan
  - investigation_describe
  
SpacesPhase("investigation") → Tools:
  - investigation_read_config
  - investigation_search_space
  - investigation_list_items
    ↓
┌─────────────────────────────────────────┐
│ Step 4: Aggregate Tools                 │
│ allTools = stores_tools + spaces_tools  │
│ registerTools(allTools)                 │
└─────────────────────────────────────────┘
    ↓
┌─────────────────────────────────────────┐
│ Step 5: Build System Prompt             │
│ - StoresPhase.BuildInstructions()       │
│ - SpacesPhase.BuildInstructions()       │
│ - Conversation history                  │
└─────────────────────────────────────────┘
    ↓
SystemPrompt:
"""
PHASE: Investigation (Stores Domain)
- Use read-only store operations
- List stores, scan, describe

PHASE: Investigation (Spaces Domain)  
- Use read-only space operations
- Read config, search, list items

Available Tools:
- investigation_list_stores: Lists all stores...
- investigation_scan: Scans store data...
- investigation_read_config: Reads space config...
- investigation_search_space: Searches space content...
...

User: List stores and search spaces for 'auth'
"""
    ↓
┌─────────────────────────────────────────┐
│ Step 6: ReAct Loop                      │
│ LLM sees ALL tools, decides sequence    │
└─────────────────────────────────────────┘
    ↓
LLM Reasoning:
"I need to:
1. List stores first
2. Then search spaces for 'auth'
Let me call investigation_list_stores"
    ↓
Tool Call: investigation_list_stores(database="production")
    ↓
Result: "Stores: users, orders, products..."
    ↓
LLM: "Now search spaces"
    ↓
Tool Call: investigation_search_space(query="auth")
    ↓
Result: "Found in: authentication.md, oauth.md..."
    ↓
LLM Response to User:
"Found stores: users, orders, products
Found 'auth' in spaces: authentication.md, oauth.md"
    ↓
┌─────────────────────────────────────────┐
│ Step 7: Phase Succession                │
│ Investigation complete                  │
│ (Single-phase plan, so done)            │
└─────────────────────────────────────────┘
```

---

## Key Architectural Differences from V1

| Aspect | V1 (Current) | V2 (New) |
|--------|-------------|----------|
| **Domain Selection** | Heuristics (keyword matching) | LLM classification |
| **Phase Concept** | None | LLM classifies current phase |
| **Capability Selection** | Registry.Classify() with keywords | Phase Factory (no Classify) |
| **Tool Exposure** | Pre-filtered by Classify() | ALL domain+phase tools exposed |
| **Tool Orchestration** | Filtered subset → LLM | Full inventory → LLM decides |
| **Multi-Domain** | Single domain only | Native multi-domain support |
| **Prompt Building** | capability.BuildContext() | phase.BuildInstructions() |
| **Phase Succession** | Manual via capability ExecuteTool | Automatic via work queue |

---

## Implementation Files

1. **`lib/sop/ai/agent/classifier_v2.go`**
   - `ClassifyTaskContextV2()` - Enhanced LLM classification
   - Returns: domains[], phase, phase_plan[], intent, confidence

2. **`lib/sop/ai/agent/copilot_v2.go`**
   - `AskV2()` - New Ask pipeline
   - `createPhaseProxies()` - Phase Factory invocation
   - `aggregatePhaseTools()` - Tool collection
   - `buildSystemPromptV2()` - Unified prompt builder
   - `isPhaseComplete()` - Phase completion detection
   - `enqueueNextPhase()` - Phase succession

3. **`lib/sop/ai/agent/copilot.go`** (modified)
   - `Ask()` - Feature flag router to V1/V2

---

## Migration Strategy

**Week 1**: Implement V2, feature flag off by default  
**Week 2-3**: Internal testing, fix bugs, compare V1 vs V2 behavior  
**Week 4**: Enable V2 by default, keep V1 as fallback  
**Week 5**: Deprecate V1, remove heuristic code  

---

## Testing Strategy

### Test Cases

```go
// TC1: Single domain, single phase
Input: "List stores in production database"
Expected:
  - Domains: ["stores"]
  - Phase: "investigation"
  - PhasePlan: ["investigation"]
  - Tools: [investigation_list_stores, investigation_scan, ...]

// TC2: Multi-domain, single phase
Input: "Query stores and search spaces for 'authentication'"
Expected:
  - Domains: ["stores", "spaces"]
  - Phase: "investigation"
  - PhasePlan: ["investigation"]
  - Tools: [stores investigation tools] + [spaces investigation tools]

// TC3: Single domain, multi-phase plan
Input: "Refactor authentication to use OAuth2"
Expected:
  - Domains: ["coding_workflow"]
  - Phase: "intake"
  - PhasePlan: ["intake", "investigation", "planning", "execution", "review"]
  - Tools: [intake tools] (first phase only)

// TC4: Multi-domain, multi-phase plan
Input: "Migrate user data from stores to spaces with validation"
Expected:
  - Domains: ["stores", "spaces"]
  - Phase: "investigation"
  - PhasePlan: ["investigation", "planning", "execution", "review"]
  - Tools: [stores investigation] + [spaces investigation]
```

### Validation Points

✅ LLM correctly identifies multiple domains  
✅ Phase Factory creates domain-specific phases  
✅ Tools from all domain phases are registered  
✅ System prompt includes instructions from all phases  
✅ LLM can call tools from any domain  
✅ Phase succession works for multi-phase plans  
✅ Single-phase plans complete without enqueuing  

---

## Success Metrics

- **Zero Heuristics**: No keyword matching in routing/classification
- **LLM Autonomy**: LLM makes 100% of tool selection decisions
- **Multi-Domain Support**: Can handle queries spanning multiple domains
- **Phase Awareness**: LLM understands current phase context
- **Tool Completeness**: All relevant domain+phase tools exposed

---

## Architectural Pattern Compliance

### ✅ Pattern 1: Explicit Parameter Passing (No Context for State)

**Verification:**
```go
// ✅ State passed explicitly
func (a *CopilotAgent) createPhaseProxiesV2(domains []string, currentPhase string)
func (a *CopilotAgent) resolvePhasePlanV2(ctx, classification *TaskContextV2) (plan []string, currentPhase string)
func (a *CopilotAgent) registerPhaseToolsV2(ctx, allTools, phases, classification)

// ❌ NO state hidden in context
// We do NOT do: ctx = context.WithValue(ctx, "domains", domains)
// We do NOT do: domains := ctx.Value("domains").([]string)

// ✅ Context only for: cancellation, timeouts, request IDs
ctx = context.WithValue(ctx, "latest_user_query", query)  // Request-scoped value ✅
```

### ✅ Pattern 2: Participant Pattern (Phases are Passive)

**Verification:**
```go
// ✅ Phases expose capabilities via interface methods
type Capability interface {
    ExposeTools() []ToolDefinition          // Declare tools
    BuildInstructions() string              // Provide guidance
    ExecuteTool(toolName, args) string      // Execute ONE tool
}

// ✅ Phase.ExecuteTool() is passive dispatch
func (p *StoresPhase) ExecuteTool(ctx, toolName, args, state) (string, error) {
    switch toolName {
    case "investigation_list_stores":
        return p.listStores(args)  // Execute, return result
    }
}

// ❌ Phases do NOT:
// - Control flow (no for loops over phases)
// - Enqueue work (no queue.Enqueue() in ExecuteTool)
// - Call other tools (no recursive ExecuteTool calls)
// - Orchestrate (no multi-step workflows in ExecuteTool)
```

### ✅ Pattern 3: Ask/LLM is Orchestrator

**Verification:**
```go
// ✅ AskV2() is the single point of control
func (a *CopilotAgent) AskV2(ctx, query, cfg) {
    // 1. AskV2 controls classification
    classification := a.ClassifyTaskContextV2(ctx, query, gen)
    
    // 2. AskV2 controls phase creation
    phases := a.createPhaseProxiesV2(classification.Domains, currentPhase)
    
    // 3. AskV2 controls tool registration
    a.registerPhaseToolsV2(ctx, allTools, phases, classification)
    
    // 4. AskV2 delegates to LLM for tool selection
    response := a.reactLoop(ctx, query, gen, systemPrompt)
    
    // 5. AskV2 controls phase succession
    if a.isPhaseCompleteV2(...) {
        nextPhase := a.getNextPhase(plan, currentPhase)
        queue.Enqueue(WorkItem{Phase: nextPhase})  // ✅ Only Ask enqueues
    }
}

// ✅ LLM decides which tools to call
// LLM sees: [investigation_list_stores, investigation_scan, ...]
// LLM decides: "I need to list stores first, then scan"
// LLM calls: investigation_list_stores → gets result → calls investigation_scan

// ❌ Phases do NOT decide workflow
// Phases do NOT enqueue next work
```

### ✅ Pattern 4: No Controller in Phases

**Verification:**
```go
// ✅ ExecuteTool is simple dispatch
func (p *StoresPhase) ExecuteTool(ctx, toolName, args, state) (string, error) {
    switch toolName {
    case "investigation_list_stores":
        stores, err := p.db.ListStores(args["database"])
        return formatStoreList(stores), err
    }
    // Returns result immediately - no loops, no enqueuing
}

// ❌ NEVER in ExecuteTool:
func ExecuteTool(ctx, toolName, args, state) {
    // ❌ WRONG - controller pattern
    for _, step := range workflow.Steps {
        executeStep(step)  // Multiple steps
    }
    
    // ❌ WRONG - multiple enqueues
    queue.Enqueue(phase1)
    queue.Enqueue(phase2)
    queue.Enqueue(phase3)
    
    // ❌ WRONG - calling other tools
    result1 := ExecuteTool("tool1", args1)
    result2 := ExecuteTool("tool2", args2)
}
```

---

## Pattern Violations Examples (What NOT to Do)

### ❌ Anti-Pattern 1: State in Context
```go
// WRONG
func (a *CopilotAgent) AskV2(ctx, query, cfg) {
    classification := a.ClassifyTaskContextV2(ctx, query, gen)
    ctx = context.WithValue(ctx, "classification", classification)  // ❌
    ctx = context.WithValue(ctx, "domains", classification.Domains)  // ❌
    phases := a.createPhaseProxiesV2(ctx)  // ❌ No explicit params
}

// CORRECT
func (a *CopilotAgent) AskV2(ctx, query, cfg) {
    classification := a.ClassifyTaskContextV2(ctx, query, gen)
    // ✅ Pass explicitly as parameters
    phases := a.createPhaseProxiesV2(classification.Domains, currentPhase)
}
```

### ❌ Anti-Pattern 2: Phase as Controller
```go
// WRONG - Phase controlling workflow
func (p *PhaseCapability) ExecuteTool(ctx, toolName, args, state) {
    // ❌ Controller pattern - phase driving multiple steps
    phases := []string{"investigation", "planning", "execution"}
    for _, phase := range phases {
        executePhase(phase)  // ❌ Loop
    }
    
    // ❌ Enqueueing multiple items
    for _, phase := range phases {
        queue.Enqueue(WorkItem{Phase: phase})  // ❌ Batch enqueue
    }
}

// CORRECT - Phase as participant
func (p *PhaseCapability) ExecuteTool(ctx, toolName, args, state) {
    // ✅ Execute ONE tool, return result
    result := p.performInvestigation(args)
    return result, nil
    
    // ✅ Ask pipeline will decide if/when to move to next phase
}
```

### ❌ Anti-Pattern 3: ExecuteTool Orchestrating
```go
// WRONG - ExecuteTool calling other tools
func (p *PhaseCapability) ExecuteTool(ctx, toolName, args, state) {
    // ❌ Chained tool calls
    stores := p.ExecuteTool(ctx, "list_stores", args, state)
    for _, store := range stores {
        p.ExecuteTool(ctx, "scan", map[string]any{"store": store}, state)  // ❌
    }
}

// CORRECT - ExecuteTool does one thing
func (p *PhaseCapability) ExecuteTool(ctx, toolName, args, state) {
    switch toolName {
    case "list_stores":
        return p.listStores(args)  // ✅ Single operation
    case "scan":
        return p.scanStore(args)   // ✅ Single operation
    }
    // LLM will call list_stores, see result, then call scan - LLM orchestrates
}
```

---

## Summary: V2 Pattern Compliance

| Pattern | Compliance | Evidence |
|---------|-----------|----------|
| **Explicit Parameters** | ✅ | classification, plan, currentPhase passed explicitly |
| **Participant Pattern** | ✅ | Phases: ExposeTools(), BuildInstructions(), ExecuteTool() |
| **Ask is Orchestrator** | ✅ | AskV2() controls all flow, phases are passive |
| **No Phase Controllers** | ✅ | ExecuteTool() returns result, doesn't loop/enqueue |
| **LLM Orchestration** | ✅ | LLM sees all tools, decides which to call |

**Validation**: All architectural patterns from `.github/copilot-instructions.md` are followed in V2 implementation.
