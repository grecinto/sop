# Domain-Agnostic Phase System - Quick Resume Guide

**Created**: 2026-07-30  
**Status**: Design Complete - Ready for Implementation  
**Full Design**: See [DOMAIN_AGNOSTIC_PHASE_SYSTEM.md](./DOMAIN_AGNOSTIC_PHASE_SYSTEM.md)

---

## TL;DR

**Goal**: Make Phase Plan System work for ALL domains (Stores, Spaces, Desktop, Coding) to improve success rate through methodical workflows.

**Key Insight**: Planner generates domain-agnostic PhasePlans. Phase capabilities surface domain-specific tools.

**Why This Matters**: Stores/Spaces currently expose tools randomly. Adding phases gives them Investigation → Planning → Execution structure, improving success rate.

---

## Current State (Blockers)

### 1. Domain Gates Block Non-Coding

**File**: `lib/sop/ai/agent/planner/planner.go:59`
```go
// ❌ THIS BLOCKS STORES/SPACES/DESKTOP
if state.Domain != "" && state.Domain != "coding_workflow" {
    return ClassificationResult{}
}
```

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go:108`
```go
// ❌ THIS BLOCKS NON-CODING PHASES
if state.Domain != "" && state.Domain != "coding_workflow" {
    return ClassificationResult{}
}
```

### 2. All Tools Are Coding-Specific

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go:1063`
```go
func phaseWorkflowTools() {
    case "investigation":
        return []ToolDefinition{
            {Name: "investigation_inspect_repo"},   // Git only
            {Name: "investigation_search_repo"},    // Code only
        }
}
```

### 3. Stores Uses CRUD, Not Phases

**File**: `lib/sop/ai/agent/domains/stores/capability.go`
```go
type ExecutionContextInput struct {
    CRUDFlags map[string]bool  // ❌ Less semantic than phases
}
```

---

## Target Architecture

```
Domain     | Investigation                    | Planning              | Execution
-----------|----------------------------------|-----------------------|---------------------------
Coding     | inspect_repo, search_repo        | plan_changes          | preview_patch, apply_patch
Stores     | list_stores, scan, describe      | design_query          | execute_script
Spaces     | read_config, list_items          | design_mutation       | mint_to_space, enrich
Desktop    | list_files, inspect_env          | create_script         | execute_command
```

**Key Principle**: Investigation = Read-only, Execution = Mutations. This replaces CRUD flags with clearer intent.

---

## Implementation Steps

### Step 1: Remove Domain Gates (15 min)

**File**: `lib/sop/ai/agent/planner/planner.go`
- Line ~59: Remove `if state.Domain != "coding_workflow"` gate
- Keep domain-agnostic classification logic (already exists)

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go`
- Line ~30: Add `domain string` field to PhaseCapability struct
- Line ~50: Update `NewPhaseCapability(phaseID, domain string, ...)` signature
- Line ~108: Replace gate with domain matching: `if c.domain != "" && c.domain != state.Domain { return }`

**Test**: Planner should accept stores domain queries

### Step 2: Create Tool Adapters (30 min)

**File**: `lib/sop/ai/agent/planner/phase_tool_adapters.go` (NEW)
- Define `PhaseToolAdapter` interface: `GetTools(phaseID string) []ToolDefinition`
- Implement `CodingPhaseToolAdapter` (move existing logic from `phaseWorkflowTools()`)
- Implement `StoresPhaseToolAdapter` (see DOMAIN_AGNOSTIC_PHASE_SYSTEM.md section 2.3)

**File**: `lib/sop/ai/agent/planner/phase_capabilities.go`
- Line ~1063: Replace `phaseWorkflowTools()` with adapter lookup:
```go
func (c *PhaseCapability) phaseWorkflowTools() []ToolDefinition {
    adapter := c.getToolAdapter()
    return adapter.GetTools(c.phaseID)
}
```

**Test**: Investigation phase should surface different tools per domain

### Step 3: Update Registration (10 min)

**File**: `lib/sop/ai/agent/copilot.go`
- Line ~1500-1700: Update capability registration:
```go
// Coding phases
for _, phaseID := range []string{"intake", "investigation", "planning", "execution", "review", "delivery"} {
    caps = append(caps, planner.NewPhaseCapability(phaseID, "coding_workflow", sharedInfra))
}

// Stores phases
for _, phaseID := range []string{"investigation", "planning", "execution", "review"} {
    caps = append(caps, planner.NewPhaseCapability(phaseID, "stores", sharedInfra))
}
```

**Test**: Registry should contain phase capabilities for multiple domains

### Step 4: Deprecate CRUD (15 min)

**File**: `lib/sop/ai/agent/domains/stores/capability.go`
- Replace `CRUDFlags map[string]bool` with `Phase string`
- Update `BuildExecutionContext()` to use phase semantics:
```go
switch input.Phase {
case "investigation":
    sb.WriteString("Operations: Read-only (list, scan, describe)\n")
case "execution":
    sb.WriteString("Operations: Mutations allowed (create, update, delete)\n")
}
```

**File**: `lib/sop/ai/agent/domains/spaces/capability.go`
- Same changes as Stores

**Test**: Phase-based context should be more semantic than CRUD

---

## Testing Checklist

```bash
# Step 1: Domain gates removed
cd lib/sop/ai/agent
go test -run TestPlannerAcceptsAllDomains ./planner
go test -run TestPhaseCapabilityDomainMatching ./planner

# Step 2: Tool adapters work
go test -run TestCodingToolAdapter ./planner
go test -run TestStoresToolAdapter ./planner
go test -run TestInvestigationToolsReadOnly ./planner

# Step 3: Registration correct
go test -run TestMultiDomainPhaseRegistration ./...

# Step 4: CRUD deprecated
go test -run TestPhaseContextSemantic ./domains/stores
go test -run TestPhaseContextSemantic ./domains/spaces

# Full build
go build ./...
```

---

## Example Workflow Change

### Before (Current - Random Tool Calls)

```
User: "Show stores with status=active"
→ Stores capability exposes: list_stores, scan, execute_script
→ LLM might call execute_script immediately (risky!)
→ No structure, no validation
```

### After (Methodical Workflow)

```
User: "Show stores with status=active"
→ Planner generates: ["investigation", "execution"]
→ Investigation phase surfaces: list_stores, scan (read-only)
→ LLM explores data structure
→ Execution phase surfaces: execute_script
→ LLM executes query with full context
→ Structured, safer
```

---

## Key Files Reference

**To Read** (Understand current state):
1. `lib/sop/ai/agent/planner/planner.go` - Planner capability
2. `lib/sop/ai/agent/planner/phase_capabilities.go` - Phase capabilities
3. `lib/sop/ai/agent/domains/stores/capability.go` - Stores capability
4. `lib/sop/ai/agent/capabilities/capability.go` - Capability interface

**To Modify** (Implementation):
1. `planner/planner.go` - Remove domain gate (line 59)
2. `planner/phase_capabilities.go` - Add domain field, update Classify (lines 30, 50, 108, 1063)
3. `planner/phase_tool_adapters.go` - NEW FILE - Create tool adapters
4. `copilot.go` - Update registration (lines 1500-1700)
5. `domains/stores/capability.go` - Replace CRUD with Phase
6. `domains/spaces/capability.go` - Replace CRUD with Phase

**To Create** (New files):
1. `lib/sop/ai/agent/planner/phase_tool_adapters.go`
2. `lib/sop/ai/agent/planner/phase_tool_adapters_test.go`

---

## Success Metrics

**Quantitative**:
- Stores success rate: ~60% → 85%+ (methodical workflows)
- Tool calls per task: Fewer, more targeted
- Error recovery rate: Higher (phases provide recovery path)

**Qualitative**:
- "Agent is more methodical"
- "Clearer phase progression"
- "Better explanations"

---

## Context from Conversation

**User's Key Insights**:
1. "The secret sauce is adaptive or LLM on the driver's seat. Not federation."
2. "The Workflow feature is a silly solution that has very low success rate, because it is STATIC."
3. "Planner can generate PhasePlan which is domain agnostic!"
4. "Based on Domain, the tools each Phase will 'surface' depends on the domain."
5. "Phase is a clearer signal [than CRUD]. Store domain for Investigation phase means turning on R (of CRUD)."
6. "This will solve or increase success rate for Stores and Spaces, because we are introducing a methodical approach."

**Recent Architecture Changes** (Build 55):
- Removed BaseCapability pattern (overengineered)
- Removed static workflow infrastructure (low success rate)
- Removed ServiceCapability (redundant with phases)
- Disabled Fleet Learning (designed for static workflows)
- Standardized lifecycle: OnBefore/OnAfter methods
- Core principle: LLM orchestration > static procedures

---

## Risk Mitigation

**Backward Compatibility**:
- Keep domain capabilities (Stores, Spaces, Desktop) for now
- Deprecate CRUD fields, don't remove (keep for 2-3 releases)
- Phase capabilities score higher (14-15 vs 10) so they take precedence

**Rollout Strategy**:
1. Week 1: Implement + test with coding domain (no behavior change)
2. Week 2: Add Stores adapter, test success rate improvement
3. Week 3: Add Spaces/Desktop adapters, deprecate CRUD
4. Week 4+: Monitor metrics, iterate

---

## Next Action When Resuming

1. **Read full design**: [DOMAIN_AGNOSTIC_PHASE_SYSTEM.md](./DOMAIN_AGNOSTIC_PHASE_SYSTEM.md)
2. **Review current code**: Read the 4 key files listed above
3. **Start with Step 1**: Remove domain gates (easiest, proves concept)
4. **Run tests**: Verify Planner accepts all domains
5. **Continue to Step 2**: Create tool adapters

**Estimated Time**: ~90 minutes total implementation + testing

---

## Questions to Resolve During Implementation

1. Should phase capabilities have shared tools? (e.g., capture tool)
2. How to handle phase skipping for simple commands? (e.g., "/cmd ls")
3. Should we create SpacesPhaseToolAdapter and DesktopPhaseToolAdapter in Phase 2, or defer to Phase 4?
4. What's the migration path for existing Stores queries using CRUD flags?

**Recommendation**: Start simple (Steps 1-2), prove the pattern with Stores, then expand.

---

**For Complete Details**: See [DOMAIN_AGNOSTIC_PHASE_SYSTEM.md](./DOMAIN_AGNOSTIC_PHASE_SYSTEM.md)
