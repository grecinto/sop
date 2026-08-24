# Phase Tool Availability Assessment

## Current Problem

The phase system restricts tools by phase, but **context-gathering tools should be universal**.

### Current Tool Distribution

**Investigation Phase:**
- `investigation_build_context` (R)
- `investigation_inspect_repo` (R)
- `investigation_search_repo` (R)
- `investigation_list_changed_files` (R)
- `investigation_run_tests` (R)
- `investigation_read_file` (R)

**Planning Phase:**
- `planning_build_context` (R) ← **DUPLICATE!**
- `planning_plan_changes` (C/U)
- `planning_assess_and_route` (C)
- `planning_create_succession_path` (C)
- `planning_read_file` (R) ← **DUPLICATE!**

**Execution Phase:**
- `execution_build_context` (R) ← **DUPLICATE!**
- `execution_preview_patch` (R)
- `execution_apply_patch` (U)
- `execution_apply_multi_file_patch` (U)
- `execution_run_tests` (R/U)
- `execution_read_file` (R) ← **DUPLICATE!**

**Review/Testing Phases:**
- Similar read-only tools duplicated

### The Core Issue

**Scenario: Planning needs to search code**
```
LLM in planning phase: "I need to search for authentication patterns"
Current: ❌ No access to investigation_search_repo
Workaround: Has to call planning_build_context and hope it includes the pattern
Problem: Artificial restriction forces heuristic context gathering
```

**Scenario: Execution needs to verify before applying patch**
```
LLM in execution: "Let me search for other usages of this function"
Current: ❌ No access to investigation_search_repo  
Workaround: Blindly apply patch without full context
Problem: Reduces code quality and safety
```

## Root Cause Analysis

### Why Phases Exist
1. ✅ **Mental framework** - helps LLM organize thinking
2. ✅ **Progress tracking** - WorkStreamContext knows where we are
3. ✅ **Artifact organization** - findings in investigation, plans in planning
4. ✅ **Tool organization** - groups related tools together

### What Phases Restrict (Incorrectly)
1. ❌ **Context gathering** - search/read should be universal
2. ❌ **LLM autonomy** - forces planning to guess without full context
3. ❌ **Natural workflows** - real work requires searching at any phase

## Proposed Architecture

### Principle: Separate Tool **Access** from Tool **Organization**

Phases should organize tools conceptually but NOT restrict access to context-gathering operations.

### Tool Classification

#### Universal Tools (Available to ALL phases)
These are **read-only context-gathering** - any phase should be able to call them:

```go
// Context Gathering - CRUD: R
- search_repo (universal)
- read_file (universal)
- inspect_repo (universal)  
- build_context (universal)
- list_changed_files (universal)
- run_tests (universal) // Read-only when just observing results
```

**Rationale:** You can't make informed decisions without context. Planning needs to search. Execution needs to verify. Review needs to inspect.

#### Phase-Specific Tools (Restricted by phase)
These are **state-changing actions** - should be restricted:

```go
// Planning Tools - CRUD: C
- planning_plan_changes
- planning_assess_and_route
- planning_create_succession_path

// Execution Tools - CRUD: U, C
- execution_apply_patch
- execution_apply_multi_file_patch  
- execution_run_command

// Capture Tools - CRUD: C, U (per phase)
- investigation_capture
- planning_capture
- execution_capture
```

### Implementation Options

#### Option 1: Universal Context Bundle (Recommended)
Create a shared "context" capability that all phases include:

```go
type ContextCapability struct {
    // Provides universal read-only tools
}

func (c *ContextCapability) ExposeTools() []ToolDefinition {
    return []ToolDefinition{
        {Name: "search_repo", Tags: []string{"R"}},
        {Name: "read_file", Tags: []string{"R"}},
        {Name: "inspect_repo", Tags: []string{"R"}},
        {Name: "build_context", Tags: []string{"R"}},
        {Name: "list_changed_files", Tags: []string{"R"}},
        {Name: "run_tests", Tags: []string{"R"}}, // Observer mode
    }
}

// Each phase includes context tools
func (p *InvestigationPhase) ExposeTools() []ToolDefinition {
    tools := UniversalContextTools() // Get universal tools
    tools = append(tools, p.phaseSpecificTools()...) // Add phase tools
    return tools
}
```

**Benefits:**
- ✅ No duplication (single source of truth)
- ✅ Clear separation (universal vs. phase-specific)
- ✅ Easy to extend (add new universal tools in one place)
- ✅ Maintains phase organization for phase-specific actions

#### Option 2: CRUD-Based Filtering Only
Remove phase-based tool restriction entirely, rely on CRUD:

```go
// All phases expose ALL tools
// CRUD filtering reduces the set based on intent

classification.CRUD = ["R"] // Read-only query
// Result: Only read tools exposed (search, read, inspect)

classification.CRUD = ["R", "U"] // Planning task
// Result: Read tools + update tools (planning, execution)
```

**Benefits:**
- ✅ Simplest implementation
- ✅ Maximum flexibility
- ❌ Loses conceptual phase organization
- ❌ Might expose execution tools during planning

#### Option 3: Hybrid (Current + Relaxed)
Keep phase organization but allow cross-phase tool access via CRUD:

```go
// Investigation exposes investigation tools
// Planning exposes planning tools
// BUT: All phases ALSO get universal context tools

// CRUD filtering further reduces based on intent
```

**Benefits:**
- ✅ Maintains phase mental model
- ✅ Allows cross-phase context gathering
- ✅ Still uses CRUD for fine-grained control

## Recommendation

### ✅ Implement Option 1: Universal Context Bundle

**Why:**
1. Clearest separation of concerns
2. No tool duplication
3. Maintains phase organization benefits
4. Enables natural workflows (planning can search, execution can verify)
5. Easy to audit (universal tools in one place)

### Implementation Plan

**Phase 1: Create Universal Context Capability**
```go
// lib/sop/ai/agent/capabilities/context/context.go
package context

type Capability struct {
    searchAdapter FileSearchAdapter
    fileReader    FileReader
    testRunner    TestRunner
}

func (c *Capability) ExposeTools() []ToolDefinition {
    return []ToolDefinition{
        {
            Name: "search_repo",
            Description: "Search repository for code patterns, functions, types, or text. Available in ALL phases for context gathering.",
            Tags: []string{"R"},
            Schema: searchSchema,
        },
        {
            Name: "read_file",
            Description: "Read file contents. Available in ALL phases for context gathering.",
            Tags: []string{"R"},
            Schema: readFileSchema,
        },
        // ... more universal tools
    }
}
```

**Phase 2: Update Phase Capabilities**
```go
func (p *InvestigationPhase) ExposeTools() []ToolDefinition {
    tools := []ToolDefinition{}
    
    // Add universal context tools
    tools = append(tools, UniversalContextTools()...)
    
    // Add phase-specific tools
    tools = append(tools, ToolDefinition{
        Name: "investigation_capture",
        Description: "Capture investigation findings",
        Tags: []string{"C", "U"},
    })
    
    return tools
}
```

**Phase 3: Remove Duplicates**
- Remove `*_build_context` from individual phases
- Remove `*_read_file` from individual phases  
- Remove `*_search_repo` duplicates
- Keep phase-specific actions (capture, plan, apply_patch)

### Expected Results

**Before (Current):**
```
Planning phase needs to search
→ No search_repo tool available
→ Calls planning_build_context hoping it returns relevant code
→ Gets partial context, makes suboptimal plan
```

**After (Universal Context):**
```
Planning phase needs to search  
→ search_repo available via universal context
→ Directly searches for pattern
→ Gets precise context, makes informed plan
```

**Token Impact:**
- Slightly more tools per phase (+6 universal tools)
- But CRUD filtering still reduces based on intent
- Net: Similar or better (no forced heuristic context building)

**Workflow Impact:**
- ✅ Planning can search when needed
- ✅ Execution can verify before applying
- ✅ All phases can gather context naturally
- ✅ Phases still provide organization and progress tracking

## Conclusion

**Phases are valuable** for:
- Organizing work conceptually
- Tracking progress (WorkStreamContext)
- Grouping related actions
- Providing mental framework

**Phases should NOT restrict** :
- Context gathering (search, read, inspect)
- LLM autonomy (self-correcting by gathering more context)
- Natural workflows (iterative refinement)

**Solution:** Universal Context Bundle
- Context-gathering tools available everywhere
- Phase-specific actions remain restricted  
- Best of both worlds: organization + flexibility
