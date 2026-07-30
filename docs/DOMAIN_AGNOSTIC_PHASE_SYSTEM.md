# Domain-Agnostic Phase System - Implementation Design

**Created**: 2026-07-30  
**Status**: Design Complete - Implementation Pending  
**Purpose**: Extend Phase Plan System to work across ALL domains (Stores, Spaces, Desktop, Coding)

---

## Executive Summary

The Phase Plan System currently works only for `coding_workflow` domain due to hardcoded domain gates in `PlannerCapability` and `PhaseCapability`. This document describes the architectural evolution to make Phases universal while surfacing domain-specific tools.

**Key Insight**: The Planner generates domain-agnostic PhasePlans. The Phase capabilities surface domain-appropriate tools when they execute.

### Current State (Limitations)

❌ **Planner rejects non-coding domains**
```go
// lib/sop/ai/agent/planner/planner.go:59
if state.Domain != "" && state.Domain != "coding_workflow" {
    return ClassificationResult{}  // Blocks Stores, Spaces, Desktop
}
```

❌ **Phase capabilities reject non-coding domains**
```go
// lib/sop/ai/agent/planner/phase_capabilities.go:108
if state.Domain != "" && state.Domain != "coding_workflow" {
    return ClassificationResult{}  // Blocks all non-coding phases
}
```

❌ **All phase tools are coding-specific**
```go
// phase_capabilities.go:1063 - phaseWorkflowTools()
case "investigation":
    return []ToolDefinition{
        {Name: "investigation_inspect_repo"},   // Git/repo only
        {Name: "investigation_search_repo"},    // Code search only
    }
```

❌ **Stores/Spaces use CRUD flags instead of phases**
```go
// domains/stores/capability.go
BuildExecutionContext(ExecutionContextInput{
    CRUDFlags: map[string]bool{"R": true},  // Less semantic than phases
})
```

### Target State (Benefits)

✅ **Universal Planner** - Works for coding, stores, spaces, desktop, ANY domain  
✅ **Methodical Workflows** - Investigation → Planning → Execution for ALL domains  
✅ **Domain-Specific Tools** - Investigation in Stores ≠ Investigation in Coding  
✅ **Better Success Rate** - Structured approach vs random tool calls  
✅ **Flexible Plans** - 1-phase to 6-phase based on task complexity  
✅ **CRUD Retirement** - Phase is clearer intent signal than C/R/U/D flags  

---

## Architecture Design

### Principle: Separation of Concerns

**Planner** (Universal):
- Generates PhasePlan sequences based on query semantics
- Decides: ["investigation", "execution"] vs full SDLC
- Domain-agnostic logic already exists in `buildPhasePlanSequence()`

**Phase Capabilities** (Domain-Aware):
- Match specific (Domain, Phase) combinations
- Surface appropriate tools for that domain+phase
- Example: (Stores, Investigation) → `list_stores`, `scan`

**Domain × Phase Tool Matrix**:

```
Domain     | Investigation                    | Planning              | Execution
-----------|----------------------------------|-----------------------|---------------------------
Coding     | inspect_repo, search_repo        | plan_changes          | preview_patch, apply_patch
Stores     | list_stores, scan, describe      | design_query          | execute_script
Spaces     | read_config, list_items          | design_mutation       | mint_to_space, enrich
Desktop    | list_files, inspect_env          | create_script         | execute_command
```

### Planner Thickness Variation

The Planner adapts plan complexity to task complexity:

**Zero Planning** (Direct Execution):
```
Query: "/cmd ls -la"
Domain: desktop
Plan: ["execution"]  # No investigation/planning needed
```

**Thin Planning** (Quick Path):
```
Query: "Show stores where status=active"
Domain: stores
Plan: ["investigation", "execution"]  # Minimal workflow
```

**Thick Planning** (Full SDLC):
```
Query: "Refactor authentication to use OAuth2"
Domain: coding_workflow
Plan: ["intake", "investigation", "planning", "execution", "review", "delivery"]
```

**Patch Only**:
```
Query: "Apply this patch: [content]"
Domain: coding_workflow
Plan: ["execution", "review"]  # Skip investigation/planning
```

---

## Implementation Plan

### Phase 1: Remove Domain Gates ⭐ **START HERE**

**Goal**: Make Planner and Phase capabilities accept ANY domain

#### 1.1 Update PlannerCapability.Classify()

**File**: `lib/sop/ai/agent/planner/planner.go`  
**Line**: ~50-70

**Current**:
```go
func (c *PlannerCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
    if c == nil {
        return capabilities.ClassificationResult{}
    }
    if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
        switch hint {
        case "phase_plan_planner", "coding_workflow", "codingworkflow", "repo_edit", "planner":
            return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModePrimary}
        }
    }
    // ❌ REMOVE THIS DOMAIN GATE:
    if state.Domain != "" && state.Domain != "coding_workflow" && !strings.EqualFold(state.Domain, "CodingWorkflow") {
        return capabilities.ClassificationResult{}
    }
    // ... rest of classification
}
```

**Target**:
```go
func (c *PlannerCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
    if c == nil {
        return capabilities.ClassificationResult{}
    }
    
    // Explicit hint takes highest priority
    if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
        if hint == "planner" || hint == "phase_plan" {
            return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModePrimary}
        }
    }
    
    // ✅ NO DOMAIN GATE - Accept all domains
    // Planner is universal - it generates PhasePlans for ANY domain
    
    q := strings.ToLower(strings.TrimSpace(query))
    if q == "" {
        return capabilities.ClassificationResult{}
    }
    
    // Detect planning keywords (domain-agnostic)
    planningSignals := []string{
        "plan", "design", "workflow", "phases", "steps",
        "how should", "approach", "strategy",
    }
    for _, signal := range planningSignals {
        if strings.Contains(q, signal) {
            return capabilities.ClassificationResult{Relevant: true, Score: 16, Mode: capabilities.CapabilityModePrimary}
        }
    }
    
    return capabilities.ClassificationResult{}
}
```

**Note**: `buildPhasePlanSequence()` already uses query semantics (not domain), so no changes needed there.

#### 1.2 Add Domain Field to PhaseCapability

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go`  
**Line**: ~30-50

**Current**:
```go
type PhaseCapability struct {
    sharedInfra *cw.SharedInfrastructure
    phaseID     string
    name        string
    description string
    captureTool string
    stateTool   string
}
```

**Target**:
```go
type PhaseCapability struct {
    sharedInfra *cw.SharedInfrastructure
    domain      string  // NEW: "coding", "stores", "spaces", "desktop", "" (universal)
    phaseID     string
    name        string
    description string
    captureTool string
    stateTool   string
}
```

#### 1.3 Update PhaseCapability Constructor

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go`  
**Line**: ~50-90

**Current**:
```go
func NewPhaseCapability(phaseID string, sharedInfra *cw.SharedInfrastructure) *PhaseCapability {
    name, desc := phaseMetadata(phaseID)
    return &PhaseCapability{
        sharedInfra: sharedInfra,
        phaseID:     phaseID,
        name:        name,
        description: desc,
        captureTool: phaseID + "_capture",
        stateTool:   phaseID + "_state",
    }
}
```

**Target**:
```go
func NewPhaseCapability(phaseID, domain string, sharedInfra *cw.SharedInfrastructure) *PhaseCapability {
    name, desc := phaseMetadata(phaseID, domain)
    return &PhaseCapability{
        sharedInfra: sharedInfra,
        domain:      domain,  // Store domain
        phaseID:     phaseID,
        name:        name,
        description: desc,
        captureTool: phaseID + "_capture",
        stateTool:   phaseID + "_state",
    }
}

func phaseMetadata(phaseID, domain string) (string, string) {
    // Add domain suffix if specified
    suffix := ""
    if domain != "" && domain != "coding" && domain != "coding_workflow" {
        suffix = " (" + domain + ")"
    }
    
    switch phaseID {
    case "intake":
        return "Intake & Requirements" + suffix, "Gather requirements and clarify scope"
    case "investigation":
        return "Investigation & Analysis" + suffix, "Analyze current state and gather evidence"
    case "planning":
        return "Planning & Design" + suffix, "Design solution and create execution plan"
    case "execution":
        return "Execution & Implementation" + suffix, "Apply changes and implement solution"
    case "review":
        return "Review & Validation" + suffix, "Validate results and check quality"
    case "delivery":
        return "Delivery & Handoff" + suffix, "Summarize work and hand off"
    default:
        return "Phase: " + phaseID + suffix, "Execute phase " + phaseID
    }
}
```

#### 1.4 Update PhaseCapability.Classify()

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go`  
**Line**: ~100-140

**Current**:
```go
func (c *PhaseCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
    if c == nil {
        return capabilities.ClassificationResult{}
    }

    // ❌ REMOVE THIS DOMAIN GATE:
    if state.Domain != "" && state.Domain != "coding_workflow" && state.Domain != "CodingWorkflow" {
        return capabilities.ClassificationResult{}
    }

    // Explicit capability hint takes highest priority
    if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
        if strings.Contains(hint, c.phaseID) || strings.Contains(hint, strings.ReplaceAll(c.name, " ", "_")) {
            return capabilities.ClassificationResult{Relevant: true, Score: 15, Mode: capabilities.CapabilityModePrimary}
        }
    }
    // ... rest of classification
}
```

**Target**:
```go
func (c *PhaseCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
    if c == nil {
        return capabilities.ClassificationResult{}
    }

    // ✅ DOMAIN MATCHING (not a gate - it's a filter)
    // If this phase is domain-specific, only match that domain
    // If domain is "" (universal), match any domain
    if c.domain != "" && state.Domain != "" {
        // Normalize domain names for comparison
        capDomain := strings.ToLower(strings.TrimSpace(c.domain))
        stateDomain := strings.ToLower(strings.TrimSpace(state.Domain))
        
        // Handle aliases
        if capDomain == "coding" || capDomain == "coding_workflow" || capDomain == "codingworkflow" {
            capDomain = "coding"
        }
        if stateDomain == "coding_workflow" || stateDomain == "codingworkflow" {
            stateDomain = "coding"
        }
        
        // If domains don't match, this phase capability is not relevant
        if capDomain != stateDomain {
            return capabilities.ClassificationResult{}
        }
    }

    // Explicit capability hint takes highest priority
    if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
        if strings.Contains(hint, c.phaseID) || strings.Contains(hint, strings.ReplaceAll(c.name, " ", "_")) {
            return capabilities.ClassificationResult{Relevant: true, Score: 15, Mode: capabilities.CapabilityModePrimary}
        }
    }

    lower := strings.ToLower(strings.TrimSpace(query))
    if lower == "" {
        return capabilities.ClassificationResult{}
    }

    // Detect the appropriate starting phase based on task semantics
    startingPhase := detectStartingPhase(lower)
    if startingPhase == c.phaseID {
        return capabilities.ClassificationResult{Relevant: true, Score: 14, Mode: capabilities.CapabilityModePrimary}
    }

    // Check for phase-specific keywords (supporting role)
    for _, keyword := range phaseKeywords(c.phaseID) {
        if strings.Contains(lower, keyword) {
            return capabilities.ClassificationResult{Relevant: true, Score: 12, Mode: capabilities.CapabilityModeSupporting}
        }
    }
    return capabilities.ClassificationResult{}
}
```

**Key Change**: Domain matching is no longer a gate (reject all). It's a filter (only match my domain). Universal phases (domain="") match all domains.

---

### Phase 2: Create Domain Tool Adapters

**Goal**: Separate domain-specific tool knowledge from phase logic

#### 2.1 Define Tool Adapter Interface

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go` (add to top)  
**Location**: After imports, before PhaseCapability struct

**Add**:
```go
// PhaseToolAdapter provides domain-specific tools for each phase.
// Each domain implements this interface to surface appropriate tools.
type PhaseToolAdapter interface {
    GetTools(phaseID string) []capabilities.ToolDefinition
}
```

#### 2.2 Implement CodingPhaseToolAdapter

**File**: `lib/sop/ai/agent/planner/phase_tool_adapters.go` (NEW FILE)

**Content**:
```go
package planner

import (
    "github.com/sharedcode/sop/ai/agent/capabilities"
    cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// CodingPhaseToolAdapter provides coding workflow tools for each phase
type CodingPhaseToolAdapter struct {
    sharedInfra *cw.SharedInfrastructure
}

func NewCodingPhaseToolAdapter(sharedInfra *cw.SharedInfrastructure) *CodingPhaseToolAdapter {
    return &CodingPhaseToolAdapter{sharedInfra: sharedInfra}
}

func (a *CodingPhaseToolAdapter) GetTools(phaseID string) []capabilities.ToolDefinition {
    taskSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Task description or query"},
    "working_dir": {"type": "string", "description": "Repository root path (optional, inferred if omitted)"},
    "active_file": {"type": "string", "description": "Primary file for changes (optional, inferred if omitted)"}
  }
}`

    searchSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Task description"},
    "search": {"type": "string", "description": "Search query for patterns or code (optional)"}
  }
}`

    patchSchema := `{
  "type": "object",
  "required": ["file_path", "old_content", "new_content"],
  "properties": {
    "file_path": {"type": "string", "description": "Path to file to modify"},
    "old_content": {"type": "string", "description": "Content to replace"},
    "new_content": {"type": "string", "description": "New content"},
    "description": {"type": "string", "description": "Description of change"}
  }
}`

    switch phaseID {
    case "intake":
        return []capabilities.ToolDefinition{{
            Name:        "intake_build_context",
            Description: "OPTIONAL: Builds repository context if task involves code. Call when user mentions files, repos, or code locations. Returns repository structure, files, dependencies.",
            Schema:      taskSchema,
        }}
        
    case "investigation":
        return []capabilities.ToolDefinition{{
            Name:        "investigation_build_context",
            Description: "REQUIRED FIRST STEP: Builds repository context (structure, files, dependencies). Must call before other investigation tools. Returns ContextPack with working directory, active file, repository summary.",
            Schema:      taskSchema,
        }, {
            Name:        "investigation_inspect_repo",
            Description: "Inspects current repository state and changed files. WHEN TO CALL: After build_context, to understand current state before analysis. Returns changed files list and diff preview.",
            Schema:      taskSchema,
        }, {
            Name:        "investigation_search_repo",
            Description: "Searches repository for patterns, implementations, or code. WHEN TO CALL: To find similar code, error patterns, or existing implementations before concluding something is novel. Returns matching code locations.",
            Schema:      searchSchema,
        }, {
            Name:        "investigation_list_changed_files",
            Description: "Lists files with uncommitted changes. WHEN TO CALL: To identify in-progress work and avoid conflicts. Returns list of changed file paths.",
            Schema:      taskSchema,
        }}
        
    case "planning":
        return []capabilities.ToolDefinition{{
            Name:        "planning_build_context",
            Description: "REQUIRED FIRST STEP: Builds repository context for planning. Must call before planning_plan_changes. Returns ContextPack with repository structure needed for concrete plan generation.",
            Schema:      taskSchema,
        }, {
            Name:        "planning_inspect_repo",
            Description: "Inspects current repository state. WHEN TO CALL: Before planning, to understand existing code structure and patterns. Returns changed files and current state.",
            Schema:      taskSchema,
        }, {
            Name:        "planning_search_repo",
            Description: "Searches repository for implementation patterns. WHEN TO CALL: Before designing, to find similar implementations and follow established patterns rather than inventing new approaches. Returns matching patterns and code locations.",
            Schema:      searchSchema,
        }, {
            Name:        "planning_list_changed_files",
            Description: "Lists changed files to avoid conflicts. WHEN TO CALL: Before finalizing plan, to ensure plan doesn't conflict with in-progress work. Returns list of changed files.",
            Schema:      taskSchema,
        }, {
            Name:        "planning_plan_changes",
            Description: "Generates concrete execution plan from gathered context. WHEN TO CALL: After build_context, search_repo, and inspect_repo complete. This is the core planning step. Returns ordered list of implementation steps with file/function specifics. IF EMPTY: Requirements may be unclear - ask clarifying question.",
            Schema:      taskSchema,
        }}
        
    case "execution":
        return []capabilities.ToolDefinition{{
            Name:        "execution_preview_patch",
            Description: "SAFETY REQUIRED: Previews patch before applying. WHEN TO CALL: Before every apply_patch call. Shows user what will change. Returns patch preview with old/new content comparison.",
            Schema:      patchSchema,
        }, {
            Name:        "execution_apply_patch",
            Description: "Applies patch to codebase. SAFETY RULE: Must call preview_patch and get user approval BEFORE calling this. WHEN TO CALL: Only after preview shown and approved. Returns confirmation of applied changes.",
            Schema:      patchSchema,
        }, {
            Name:        "execution_run_tests",
            Description: "Runs tests to validate changes. WHEN TO CALL: After each apply_patch call, to ensure changes work correctly and don't introduce regressions. Returns test results (pass/fail) and output.",
            Schema:      taskSchema,
        }}
        
    case "review":
        return []capabilities.ToolDefinition{{
            Name:        "review_build_context",
            Description: "Builds repository context for review. WHEN TO CALL: At review start, to understand current state after execution. Returns repository structure and current state.",
            Schema:      taskSchema,
        }, {
            Name:        "review_inspect_repo",
            Description: "Inspects repository state to see applied changes. WHEN TO CALL: To review what was changed during execution phase. Returns changed files and diff.",
            Schema:      taskSchema,
        }, {
            Name:        "review_run_tests",
            Description: "Runs comprehensive tests for validation. WHEN TO CALL: To validate all requirements are met and no regressions introduced. Returns test results and coverage.",
            Schema:      taskSchema,
        }}
        
    default:
        return nil
    }
}
```

**Note**: This is a straight extraction of existing `phaseWorkflowTools()` logic into an adapter.

#### 2.3 Implement StoresPhaseToolAdapter

**File**: `lib/sop/ai/agent/planner/phase_tool_adapters.go` (add to same file)

**Add**:
```go
// StoresPhaseToolAdapter provides Stores domain tools for each phase
type StoresPhaseToolAdapter struct{}

func NewStoresPhaseToolAdapter() *StoresPhaseToolAdapter {
    return &StoresPhaseToolAdapter{}
}

func (a *StoresPhaseToolAdapter) GetTools(phaseID string) []capabilities.ToolDefinition {
    storeTaskSchema := `{
  "type": "object",
  "required": ["database"],
  "properties": {
    "database": {"type": "string", "description": "Target database name"},
    "store": {"type": "string", "description": "Store name (optional if listing all)"}
  }
}`

    storeScanSchema := `{
  "type": "object",
  "required": ["database", "store"],
  "properties": {
    "database": {"type": "string", "description": "Target database name"},
    "store": {"type": "string", "description": "Store name to scan"},
    "prefix": {"type": "string", "description": "Optional key prefix filter"},
    "limit": {"type": "number", "description": "Max records to return (default 100)"}
  }
}`

    storeScriptSchema := `{
  "type": "object",
  "required": ["database", "script"],
  "properties": {
    "database": {"type": "string", "description": "Target database name"},
    "script": {"type": "string", "description": "Multi-step operation script"},
    "dry_run": {"type": "boolean", "description": "Preview without executing (default false)"}
  }
}`

    switch phaseID {
    case "investigation":
        // Read-only operations for understanding store structure and content
        return []capabilities.ToolDefinition{{
            Name:        "stores_investigation_list_stores",
            Description: "Lists all stores in the database. WHEN TO CALL: First step to discover available stores before querying. Returns store names and metadata.",
            Schema:      storeTaskSchema,
        }, {
            Name:        "stores_investigation_scan",
            Description: "Scans store records with optional filters. WHEN TO CALL: To understand data shape, content, and patterns. Returns sample records. READ-ONLY operation.",
            Schema:      storeScanSchema,
        }, {
            Name:        "stores_investigation_describe",
            Description: "Gets store schema (key structure, value types). WHEN TO CALL: Before query design, to understand data structure. Returns schema information.",
            Schema:      storeTaskSchema,
        }}
        
    case "planning":
        // Query design and strategy
        return []capabilities.ToolDefinition{{
            Name:        "stores_planning_design_query",
            Description: "Designs multi-step query plan. WHEN TO CALL: After investigation, before execution. Analyzes requirements and creates execution strategy. Returns query plan with steps.",
            Schema:      storeTaskSchema,
        }}
        
    case "execution":
        // Write operations (Create, Update, Delete)
        return []capabilities.ToolDefinition{{
            Name:        "stores_execution_execute_script",
            Description: "Executes multi-step store operations. SAFETY RULE: Use dry_run=true first to preview changes. WHEN TO CALL: Only after planning phase and user approval. Returns execution results.",
            Schema:      storeScriptSchema,
        }}
        
    case "review":
        // Validation after execution
        return []capabilities.ToolDefinition{{
            Name:        "stores_review_verify_results",
            Description: "Validates execution results against requirements. WHEN TO CALL: After execution, to ensure data integrity and requirements met. Returns validation report.",
            Schema:      storeTaskSchema,
        }}
        
    default:
        return nil
    }
}
```

**Note**: Investigation = Read-only (replaces CRUD "R" flag), Execution = Mutations (replaces CRUD "C/U/D" flags).

#### 2.4 Update PhaseCapability to Use Adapters

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go`  
**Line**: ~1063 (phaseWorkflowTools method)

**Current**:
```go
func (c *PhaseCapability) phaseWorkflowTools() []capabilities.ToolDefinition {
    // Giant switch statement with hardcoded coding tools
    switch c.phaseID {
    case "intake":
        return []capabilities.ToolDefinition{{Name: "intake_build_context", ...}}
    // ... 200+ lines of tool definitions
    }
}
```

**Target**:
```go
func (c *PhaseCapability) phaseWorkflowTools() []capabilities.ToolDefinition {
    if c.domain == "" {
        // Universal phase (no domain-specific tools)
        return nil
    }
    
    // Get domain-specific adapter
    adapter := c.getToolAdapter()
    if adapter == nil {
        return nil
    }
    
    return adapter.GetTools(c.phaseID)
}

func (c *PhaseCapability) getToolAdapter() PhaseToolAdapter {
    switch strings.ToLower(strings.TrimSpace(c.domain)) {
    case "coding", "coding_workflow", "codingworkflow":
        return NewCodingPhaseToolAdapter(c.sharedInfra)
    case "stores":
        return NewStoresPhaseToolAdapter()
    case "spaces":
        return NewSpacesPhaseToolAdapter()
    case "desktop":
        return NewDesktopPhaseToolAdapter()
    default:
        return nil
    }
}
```

**CRITICAL**: Move existing tool definitions from `phaseWorkflowTools()` into `CodingPhaseToolAdapter.GetTools()`. This is code motion, not rewrite.

---

### Phase 3: Update Capability Registration

**Goal**: Register phase capabilities per domain

**File**: `lib/sop/ai/agent/copilot.go`  
**Line**: ~1500-1700 (capability registration section)

**Current**:
```go
// Register phase capabilities (coding only)
for _, phaseID := range []string{"intake", "investigation", "planning", "execution", "review", "delivery"} {
    caps = append(caps, planner.NewPhaseCapability(phaseID, sharedInfra))
}
```

**Target**:
```go
// Planner (universal - no domain restriction)
caps = append(caps, planner.NewPlannerCapability(sharedInfra))

// Coding domain phases (full SDLC)
for _, phaseID := range []string{"intake", "investigation", "planning", "execution", "review", "delivery"} {
    caps = append(caps, planner.NewPhaseCapability(phaseID, "coding_workflow", sharedInfra))
}

// Stores domain phases (subset - intake/delivery less critical for queries)
for _, phaseID := range []string{"investigation", "planning", "execution", "review"} {
    caps = append(caps, planner.NewPhaseCapability(phaseID, "stores", sharedInfra))
}

// Spaces domain phases (minimal - often single-step operations)
for _, phaseID := range []string{"investigation", "execution", "review"} {
    caps = append(caps, planner.NewPhaseCapability(phaseID, "spaces", sharedInfra))
}

// Desktop domain phases (execution-focused)
for _, phaseID := range []string{"planning", "execution"} {
    caps = append(caps, planner.NewPhaseCapability(phaseID, "desktop", sharedInfra))
}

// Domain capabilities (backward compatibility - lower priority than phases)
caps = append(caps,
    stores.NewCapability(),
    spaces.NewCapability(),
    desktop.NewCapability(),
)
```

**Note**: Domain capabilities stay for backward compatibility but will score lower (10) than phase capabilities (14-15).

---

### Phase 4: Deprecate CRUD Flags

**Goal**: Replace CRUD-based permissions with phase-based permissions

#### 4.1 Update Stores Capability

**File**: `lib/sop/ai/agent/domains/stores/capability.go`

**Current**:
```go
type ExecutionContextInput struct {
    CRUDFlags       map[string]bool  // ❌ CRUD is less semantic than phases
    DBArtifacts     []string
    ScriptAuthoring bool
    ActiveDatabase  string
}

func (c *Capability) BuildExecutionContext(input ExecutionContextInput) string {
    var sb strings.Builder
    sb.WriteString("Domain: Stores\n")
    if ordered := orderedCRUDFlags(input.CRUDFlags); len(ordered) > 0 {
        sb.WriteString(fmt.Sprintf("CRUD Scope: %s\n", strings.Join(ordered, ", ")))
    }
    // ...
}
```

**Target**:
```go
type ExecutionContextInput struct {
    Phase           string   // ✅ "investigation", "planning", "execution", "review"
    DBArtifacts     []string
    ScriptAuthoring bool
    ActiveDatabase  string
}

func (c *Capability) BuildExecutionContext(input ExecutionContextInput) string {
    var sb strings.Builder
    sb.WriteString("Domain: Stores\n")
    
    // Phase-based context (more semantic than CRUD)
    if input.Phase != "" {
        sb.WriteString(fmt.Sprintf("Phase: %s\n", input.Phase))
        switch input.Phase {
        case "investigation":
            sb.WriteString("Operations: Read-only (list, scan, describe)\n")
        case "planning":
            sb.WriteString("Operations: Query design (no mutations)\n")
        case "execution":
            sb.WriteString("Operations: Mutations allowed (create, update, delete)\n")
        case "review":
            sb.WriteString("Operations: Validation (verify results)\n")
        }
    }
    
    if len(input.DBArtifacts) > 0 {
        sb.WriteString(fmt.Sprintf("Target Stores: %s\n", strings.Join(input.DBArtifacts, ", ")))
    }
    return strings.TrimSpace(sb.String())
}

// DEPRECATED: Keep for backward compatibility
func orderedCRUDFlags(flags map[string]bool) []string {
    order := []string{"C", "R", "U", "D"}
    ordered := make([]string, 0, len(flags))
    for _, crud := range order {
        if flags[crud] {
            ordered = append(ordered, crud)
        }
    }
    return ordered
}
```

**Migration Note**: Keep `orderedCRUDFlags()` for backward compatibility but mark as deprecated.

#### 4.2 Update Spaces Capability (Same Pattern)

**File**: `lib/sop/ai/agent/domains/spaces/capability.go`

Apply same changes as Stores:
- Replace `CRUDFlags map[string]bool` with `Phase string`
- Update `BuildExecutionContext()` to use phase semantics
- Keep `orderedCRUDFlags()` for backward compatibility

---

## Benefits Analysis

### Why This Improves Success Rate

**Current State** (Random Tool Calls):
```
User: "Show me stores with status=active"
→ Stores capability exposes: list_stores, scan, execute_script
→ LLM might call execute_script immediately (risky!)
→ No structured workflow, no validation
```

**New State** (Methodical Workflow):
```
User: "Show me stores with status=active"
→ Planner generates: ["investigation", "execution"]
→ Investigation phase surfaces: list_stores, scan, describe (read-only)
→ LLM calls list_stores, scan to understand data
→ Execution phase surfaces: execute_script
→ LLM executes query with full context
→ Structured progression, safer mutations
```

### Methodical Approach Benefits

1. **Investigation Before Action**
   - Forces LLM to gather context before mutations
   - Reduces "shoot first, ask questions later" errors

2. **Planning Before Execution**
   - Complex tasks get design phase
   - Multi-step operations are planned, not ad-hoc

3. **Review After Changes**
   - Validation is explicit, not skipped
   - Results are checked against requirements

4. **Clear Phase Transitions**
   - LLM can reason: "I'm done investigating, time to plan"
   - User can interject between phases

5. **Better Error Recovery**
   - Execution failed? Go back to planning
   - Requirements unclear? Go back to intake

---

## Testing Plan

### Phase 1 Testing (Domain Gates Removed)

**Test 1: Planner Accepts Stores Domain**
```go
state := RoutingState{Domain: "stores"}
result := planner.Classify("design a query for active stores", state)
assert.True(result.Relevant)  // Should NOT be rejected
```

**Test 2: Phase Capability Matches Domain**
```go
investigationCap := NewPhaseCapability("investigation", "stores", nil)
state := RoutingState{Domain: "stores"}
result := investigationCap.Classify("find active stores", state)
assert.True(result.Relevant)  // Should match
```

**Test 3: Phase Capability Rejects Wrong Domain**
```go
investigationCap := NewPhaseCapability("investigation", "stores", nil)
state := RoutingState{Domain: "coding_workflow"}
result := investigationCap.Classify("fix bug", state)
assert.False(result.Relevant)  // Should NOT match
```

### Phase 2 Testing (Tool Adapters)

**Test 4: Coding Adapter Returns Correct Tools**
```go
adapter := NewCodingPhaseToolAdapter(nil)
tools := adapter.GetTools("investigation")
assert.Contains(toolNames(tools), "investigation_inspect_repo")
assert.Contains(toolNames(tools), "investigation_search_repo")
```

**Test 5: Stores Adapter Returns Correct Tools**
```go
adapter := NewStoresPhaseToolAdapter()
tools := adapter.GetTools("investigation")
assert.Contains(toolNames(tools), "stores_investigation_list_stores")
assert.Contains(toolNames(tools), "stores_investigation_scan")
assert.NotContains(toolNames(tools), "stores_execution_execute_script")  // Execution only
```

**Test 6: Investigation Tools Are Read-Only**
```go
adapter := NewStoresPhaseToolAdapter()
investigationTools := adapter.GetTools("investigation")
executionTools := adapter.GetTools("execution")

// Investigation = read-only
for _, tool := range investigationTools {
    assert.NotContains(tool.Name, "execute")
    assert.NotContains(tool.Name, "write")
    assert.NotContains(tool.Name, "delete")
}

// Execution = mutations allowed
assert.Contains(toolNames(executionTools), "stores_execution_execute_script")
```

### Phase 3 Testing (Registration)

**Test 7: Multiple Domain Phase Capabilities Registered**
```go
agent := NewCopilotAgent(config)
caps := agent.GetCapabilities()

// Should have coding phases
assert.HasCapability(caps, "investigation", "coding_workflow")

// Should have stores phases
assert.HasCapability(caps, "investigation", "stores")

// Should have spaces phases
assert.HasCapability(caps, "investigation", "spaces")
```

### Phase 4 Testing (CRUD Deprecation)

**Test 8: Phase Context Is Semantic**
```go
input := ExecutionContextInput{Phase: "investigation"}
ctx := capability.BuildExecutionContext(input)
assert.Contains(ctx, "Phase: investigation")
assert.Contains(ctx, "Read-only")
assert.NotContains(ctx, "CRUD")  // No CRUD terminology
```

---

## Migration Strategy

### Backward Compatibility

**Domain Capabilities Stay** (for now):
- Keep Stores, Spaces, Desktop capabilities
- Lower classification score (10 vs 14-15 for phases)
- Phase capabilities take precedence
- Allows gradual migration

**CRUD Fields Deprecated** (not removed):
- Keep `CRUDFlags` field for backward compatibility
- Mark as deprecated in comments
- New code uses `Phase` field
- Old code continues to work

### Rollout Plan

1. **Week 1**: Implement Phase 1 (domain gates removed)
   - Test with coding domain (no behavior change)
   - Deploy to staging

2. **Week 2**: Implement Phase 2 (tool adapters)
   - Create Stores adapter
   - Test stores workflows
   - Compare success rate vs current implementation

3. **Week 3**: Implement Phase 3-4 (registration + CRUD deprecation)
   - Register all domain phases
   - Update Stores/Spaces to use Phase field
   - Deploy to production

4. **Week 4+**: Monitor and iterate
   - Track success rates by domain
   - Gather user feedback
   - Refine phase transitions

---

## Code Locations Reference

### Files to Modify

1. **lib/sop/ai/agent/planner/planner.go**
   - Line ~50-70: `Classify()` - Remove domain gate

2. **lib/sop/ai/agent/planner/phase_capabilities.go**
   - Line ~30-50: Add `domain` field to struct
   - Line ~50-90: Update constructor signature
   - Line ~100-140: Update `Classify()` with domain matching
   - Line ~1063: Replace `phaseWorkflowTools()` with adapter pattern

3. **lib/sop/ai/agent/planner/phase_tool_adapters.go** (NEW FILE)
   - Create tool adapter interface
   - Implement CodingPhaseToolAdapter (move existing logic)
   - Implement StoresPhaseToolAdapter (new)
   - Implement SpacesPhaseToolAdapter (new)
   - Implement DesktopPhaseToolAdapter (new)

4. **lib/sop/ai/agent/copilot.go**
   - Line ~1500-1700: Update capability registration

5. **lib/sop/ai/agent/domains/stores/capability.go**
   - Replace `CRUDFlags` with `Phase` in ExecutionContextInput
   - Update `BuildExecutionContext()` to use phase semantics

6. **lib/sop/ai/agent/domains/spaces/capability.go**
   - Same changes as Stores

### Files to Create

1. **lib/sop/ai/agent/planner/phase_tool_adapters.go**
   - Tool adapter interface
   - All domain-specific tool adapters

2. **lib/sop/ai/agent/planner/phase_tool_adapters_test.go**
   - Unit tests for adapters

### Tests to Add

1. **lib/sop/ai/agent/planner/planner_test.go**
   - TestPlannerAcceptsAllDomains
   - TestPlannerGeneratesPlanForStores
   - TestPlannerGeneratesPlanForSpaces

2. **lib/sop/ai/agent/planner/phase_capabilities_test.go**
   - TestPhaseCapabilityDomainMatching
   - TestPhaseCapabilityToolSurfacing
   - TestInvestigationToolsReadOnly

---

## Success Metrics

### Quantitative Metrics

1. **Success Rate by Domain**
   - Current Stores: ~60% success (estimate)
   - Target Stores: 85%+ success with phases
   
2. **Tool Call Efficiency**
   - Measure: Avg tools called per task
   - Current: Random, often excessive
   - Target: Structured, minimal necessary

3. **Error Recovery Rate**
   - Measure: Tasks recovered after initial failure
   - Current: Low (no structure to guide recovery)
   - Target: High (phases provide recovery path)

### Qualitative Metrics

1. **User Feedback**
   - "Agent is more methodical"
   - "Fewer random tool calls"
   - "Better explanations of what it's doing"

2. **Code Review Quality**
   - Fewer "why did it do that?" questions
   - Clearer phase progression in logs
   - Better error messages

---

## Open Questions

1. **Do all domains need all phases?**
   - Answer: No. Desktop might skip investigation/planning for simple commands
   - Resolution: Register only necessary phases per domain

2. **Should phase capabilities have shared tools?**
   - Example: "capture" tool is same across phases
   - Current: Each phase has its own capture tool
   - Consider: Shared capture capability?

3. **How to handle phase skipping?**
   - User: "Just execute this query" (skip investigation/planning)
   - Planner should generate: ["execution"] for simple tasks
   - Needs: Better detection of "direct execution" tasks

4. **Should CRUD be completely removed?**
   - Risk: Breaking changes for existing code
   - Recommendation: Deprecate but keep for 2-3 releases

---

## Future Enhancements

1. **Dynamic Phase Selection**
   - LLM decides which phases needed mid-workflow
   - Example: Investigation reveals need for more intake

2. **Phase Checkpoints with User Approval**
   - "Investigation complete. Should I proceed to execution?"
   - Configurable per domain/phase

3. **Cross-Domain Workflows**
   - Task involves both Stores and Coding
   - Need: Phase capabilities that work across domains

4. **Phase Templates per Task Type**
   - Bug fix: Investigation → Execution → Review
   - Feature: Full SDLC
   - Query: Investigation → Execution
   - Refactor: Investigation → Planning → Execution → Review

---

## Appendix: Domain × Phase Matrix (Full)

```
Domain   | Intake | Investigation      | Planning       | Execution          | Review         | Delivery
---------|--------|-------------------|----------------|-------------------|----------------|----------
Coding   | ✓      | ✓ inspect, search | ✓ plan_changes | ✓ patch, test     | ✓ test         | ✓
Stores   | –      | ✓ list, scan      | ✓ design_query | ✓ execute_script  | ✓ verify       | –
Spaces   | –      | ✓ read_config     | – (optional)   | ✓ mint, enrich    | ✓ check        | –
Desktop  | –      | – (optional)      | ✓ script       | ✓ execute_command | – (optional)   | –

Legend:
✓ = Phase implemented with tools
– = Phase not needed or very rare
(optional) = Available but often skipped
```

**Phase Usage Frequency by Domain**:

- **Coding**: High usage of all phases (full SDLC common)
- **Stores**: Investigation → Planning → Execution (methodical queries)
- **Spaces**: Investigation → Execution (often 2-phase)
- **Desktop**: Planning → Execution or just Execution (simple commands)

---

**End of Document**

For questions or clarifications, refer to:
- [PHASE_PLAN_IMPLEMENTATION_DESIGN.md](./PHASE_PLAN_IMPLEMENTATION_DESIGN.md) - Current phase system
- [ARCHITECTURE_PATTERNS.md](../lib/sop/ai/agent/ARCHITECTURE_PATTERNS.md) - Participant Pattern
- [PLANNING_FIRST_LLM_ORCHESTRATION.md](./PLANNING_FIRST_LLM_ORCHESTRATION.md) - Phase succession design
