# Phase + Domain + Operation Based Tool Filtering

**Created**: 2026-07-31  
**Status**: Proposal  
**Purpose**: Further optimize V2 token consumption by filtering tools based on Phase + Domain + Operation (CRUD) intent

---

## Executive Summary

**Current State**: V2 hybrid strategy (from TOKEN_CONSUMPTION_ANALYSIS.md) reduces tokens by 67% through:
- Phase 1: Fast-path classification (obvious queries)
- Phase 2: Domain-based tool pre-filtering (only show tools for classified domains)

**Existing Infrastructure**: ✅ CRUD grouping **already implemented** in V1 Ask:
- `focused_context.go`: Collects CRUD flags from classification (`collectCRUDFlags`)
- `buildStoresCRUDOperationsContext()`: Shows CRUD-specific guidance in prompt
- `stores.ExecutionContextInput{CRUDFlags: map[string]bool}`: Passes CRUD to capabilities
- **Gap**: CRUD flags are shown in prompt but **NOT used to filter tools**

**This Proposal**: **Extend existing CRUD infrastructure** to actually filter tools in V2 Ask for **40-60% additional token reduction** within domains.

**Example**:
- Current V1/V2: "list stores" → Shows CRUD guidance + Exposes ALL 15 stores tools
- Proposed Phase 3: "list stores" → Phase=investigation + Domain=stores + CRUD=R → Expose ONLY 5 read-only store tools (reuse existing CRUDFlags)

**Expected Impact**:
- **Additional 30-50% token reduction** (stacks on Phase 1+2)
- **Combined 75-85% total token reduction** vs V2 pure
- **Near parity with V1** token consumption while maintaining LLM orchestration

---

## Existing Infrastructure (Already Implemented in V1)

### ✅ CRUD Classification & Collection

**File**: `lib/sop/ai/agent/classifier.go`
```go
type LayerInfo struct {
    Name string   `json:"name"`
    CRUD []string `json:"crud"`  // ✅ Already captures ["C", "R", "U", "D"]
}
```

**File**: `lib/sop/ai/agent/focused_context.go`
```go
func collectCRUDFlags(layers []LayerInfo) map[string]bool {
    flags := make(map[string]bool)
    for _, layer := range layers {
        for _, crud := range layer.CRUD {
            crud = strings.ToUpper(strings.TrimSpace(crud))
            if crud != "" {
                flags[crud] = true  // ✅ Aggregates CRUD flags
            }
        }
    }
    return flags
}
```

### ✅ Domain Capability Integration

**File**: `lib/sop/ai/agent/domains/stores/capability.go`
```go
type ExecutionContextInput struct {
    CRUDFlags map[string]bool  // ✅ Receives CRUD from classification
    DBArtifacts []string
    ScriptAuthoring bool
    ActiveDatabase string
}

func (c *Capability) BuildExecutionContext(input ExecutionContextInput) string {
    if ordered := orderedCRUDFlags(input.CRUDFlags); len(ordered) > 0 {
        sb.WriteString(fmt.Sprintf("CRUD Scope: %s\n", strings.Join(ordered, ", ")))
    }
    // ⚠️ Shows CRUD in prompt but doesn't filter tools
}
```

### ✅ CRUD-Specific Guidance

**File**: `lib/sop/ai/agent/focused_context.go`
```go
func buildStoresCRUDOperationsContext(flags map[string]bool) string {
    if flags["R"] {
        sections = append(sections,
            "- R = Read. Flow: begin_tx(mode=read) -> open_store -> scan/filter/project...",
        )
    }
    if flags["C"] {
        sections = append(sections,
            "- C = Create. Use write transactions...",
        )
    }
    // ✅ CRUD-specific guidance already built based on flags
}
```

### ⚠️ Gap: CRUD Not Used for Tool Filtering

**Current Behavior**:
1. ✅ Classification captures CRUD in `LayerInfo.CRUD[]`
2. ✅ CRUD flags collected via `collectCRUDFlags()`
3. ✅ CRUD passed to capabilities via `ExecutionContextInput{CRUDFlags}`
4. ✅ CRUD guidance shown in prompt via `buildStoresCRUDOperationsContext()`
5. ⚠️ **Tools NOT filtered** - `ExposeTools()` returns ALL tools regardless of CRUD

**Result**: LLM sees CRUD guidance but is still exposed to all 15 store tools (including mutations for read-only queries).

**This Proposal**: Wire existing CRUD flags to filter tools in `ExposeTools()`.

---

## Problem Statement

### Current V2 Hybrid Behavior (After Phase 1+2)

When classifying "list all stores":
1. ✅ Fast-path classification determines: Domain=stores, Phase=investigation
2. ✅ Domain pre-filtering exposes ONLY stores tools (not desktop, not spaces)
3. ⚠️ **Problem**: Exposes ALL 15 stores tools, including mutations

**Tools Exposed**:
```json
{
  "investigation_tools": [
    "list_stores",           // ✅ Read - relevant
    "get_store_config",      // ✅ Read - relevant
    "scan_store_items",      // ✅ Read - relevant
    "query_store",           // ✅ Read - relevant
    "describe_store_schema", // ✅ Read - relevant
    "create_store",          // ❌ Mutation - NOT relevant
    "delete_store",          // ❌ Mutation - NOT relevant
    "update_store_config",   // ❌ Mutation - NOT relevant
    "insert_item",           // ❌ Mutation - NOT relevant
    "update_item",           // ❌ Mutation - NOT relevant
    "delete_item",           // ❌ Mutation - NOT relevant
    "bulk_insert",           // ❌ Mutation - NOT relevant
    "bulk_update",           // ❌ Mutation - NOT relevant
    "bulk_delete",           // ❌ Mutation - NOT relevant
    "migrate_store"          // ❌ DDL - NOT relevant
  ]
}
```

**Token Cost**: 15 tools × 80 tokens each = **1,200 tokens** per turn

**Waste**: 10 irrelevant tools × 80 tokens each = **800 tokens wasted** (67% of tool context)

---

## Proposed Solution

### Extend Existing CRUD Infrastructure for Tool Filtering

**Existing Infrastructure** (already in V1 Ask):

1. **Classification** (`classifier.go`):
```go
type LayerInfo struct {
    Name string   `json:"name"`
    CRUD []string `json:"crud"`  // ✅ Already captures C/R/U/D flags
}
```

2. **CRUD Collection** (`focused_context.go`):
```go
func collectCRUDFlags(layers []LayerInfo) map[string]bool {
    flags := make(map[string]bool)
    for _, layer := range layers {
        for _, crud := range layer.CRUD {
            crud = strings.ToUpper(strings.TrimSpace(crud))
            if crud != "" {
                flags[crud] = true  // ✅ Already collects CRUD flags
            }
        }
    }
    return flags
}
```

3. **Domain Context** (`focused_context.go`):
```go
func (a *CopilotAgent) storesExecutionContextInputFromTask(...) stores.ExecutionContextInput {
    return stores.ExecutionContextInput{
        CRUDFlags: collectCRUDFlags(taskClassification.Layers),  // ✅ Already passes CRUD
        // ...
    }
}
```

4. **Capability Context** (`domains/stores/capability.go`):
```go
type ExecutionContextInput struct {
    CRUDFlags map[string]bool  // ✅ Already receives CRUD flags
    // ...
}

func (c *Capability) BuildExecutionContext(input ExecutionContextInput) string {
    if ordered := orderedCRUDFlags(input.CRUDFlags); len(ordered) > 0 {
        sb.WriteString(fmt.Sprintf("CRUD Scope: %s\n", strings.Join(ordered, ", ")))
    }
    // ⚠️ Shows CRUD in prompt but doesn't filter tools!
}
```

**Gap**: CRUD flags are collected and shown in prompt, but **NOT used to filter tools in `ExposeTools()`**.

**Proposed Enhancement**: Use existing CRUD flags to filter tools

**Operation Types**:
- `read`: Read-only operations (list, get, scan, query, describe)
- `create`: Create new resources (create_store, insert_item, bulk_insert)
- `update`: Modify existing resources (update_config, update_item, bulk_update)
- `delete`: Remove resources (delete_store, delete_item, bulk_delete)
- `ddl`: Schema/structure changes (migrate_store, create_index, alter_schema)
- `mixed`: Ambiguous or multi-operation queries (requires all tools)

#### Tool Metadata Enhancement

**Extend existing tool definitions with CRUD tags**:

```go
// In stores.go capability - extend ExposeTools to use existing CRUD flags
func (c *Capability) ExposeTools(ctx context.Context, routingState capabilities.RoutingState) []ai.ToolDefinition {
    allTools := []ai.ToolDefinition{
        {
            Name:        "list_stores",
            Description: "Lists all available stores",
            Parameters:  listStoresSchema,
            Tags:        []string{"R"},  // ✅ Maps to existing CRUD flag "R"
        },
        {
            Name:        "create_store",
            Description: "Creates a new store",
            Parameters:  createStoreSchema,
            Tags:        []string{"C"},  // ✅ Maps to existing CRUD flag "C"
        },
        {
            Name:        "delete_store",
            Description: "Deletes an existing store",
            Parameters:  deleteStoreSchema,
            Tags:        []string{"D"},  // ✅ Maps to existing CRUD flag "D"
        },
        // ... more tools with CRUD tags
    }
    
    // ✅ NEW: Filter by CRUD flags from routing state
    crudFlags := extractCRUDFromRoutingState(routingState)
    if len(crudFlags) == 0 {
        return allTools  // No filtering - expose all tools
    }
    
    return filterToolsByCRUD(allTools, crudFlags)
}
```

#### Filtering Logic

**New function in `copilot_v2.go`**:

```go
// filterToolsByOperation filters tools based on Phase + Domain + Operation
func filterToolsByOperation(
    tools []ai.ToolDefinition,
    phase string,
    domain string,
    operation string,
) []ai.ToolDefinition {
    // If operation is "mixed" or empty, expose all tools (ambiguous case)
    if operation == "" || operation == "mixed" {
        return tools
    }
    
    filtered := make([]ai.ToolDefinition, 0, len(tools))
    for _, tool := range tools {
        // Check if tool tags match the operation
        if hasMatchingTag(tool.Tags, operation) {
            filtered = append(filtered, tool)
        }
    }
    
    // Safety: If filtering results in 0 tools, return all tools
    // (prevents over-filtering edge cases)
    if len(filtered) == 0 {
        log.Warn("Operation filtering resulted in 0 tools, exposing all",
            "phase", phase,
            "domain", domain,
            "operation", operation,
        )
        return tools
    }
    
    return filtered
}

func hasMatchingTag(tags []string, operation string) bool {
    for _, tag := range tags {
        if strings.EqualFold(tag, operation) {
            return true
        }
    }
    return false
}
```

#### Integration into V2 Ask Pipeline

**Update `buildSystemPrompt()` in `copilot_v2.go`**:

```go
func (a *CopilotAgent) buildSystemPrompt(
    ctx context.Context,
    query string,
    taskContext TaskContextClassification,
) string {
    // ... existing code ...
    
    // Phase 2: Domain-based filtering (existing)
    relevantCapabilities := a.capabilityRegistry.FilterByDomain(taskContext.Domain)
    
    // Phase 3: Operation-based filtering (NEW)
    for _, cap := range relevantCapabilities {
        allTools := cap.ExposeTools(ctx, routingState)
        
        // Apply operation filtering
        filteredTools := filterToolsByOperation(
            allTools,
            taskContext.Phase,
            taskContext.Domain,
            taskContext.Operation,  // NEW field from classification
        )
        
        // Add filtered tools to system prompt
        toolContext.WriteString(formatToolsForPrompt(filteredTools))
    }
    
    // ... rest of prompt construction ...
}
```

---

## Classification Examples

### Example 1: Read-Only Query

**Query**: "list all stores"

**Classification**:
```json
{
  "domains": ["stores"],
  "phase": "investigation",
  "operation": "read",
  "intent": "read_data"
}
```

**Tools Exposed** (5 read-only tools):
```
- list_stores
- get_store_config
- scan_store_items
- query_store
- describe_store_schema
```

**Token Savings**: 10 filtered tools × 80 tokens = **800 tokens saved** (67% reduction)

---

### Example 2: Mutation Query

**Query**: "create a new store called 'production'"

**Classification**:
```json
{
  "domains": ["stores"],
  "phase": "execution",
  "operation": "create",
  "intent": "mutate_data"
}
```

**Tools Exposed** (3 create tools):
```
- create_store
- insert_item
- bulk_insert
```

**Token Savings**: 12 filtered tools × 80 tokens = **960 tokens saved** (80% reduction)

---

### Example 3: Mixed Operations (Ambiguous)

**Query**: "analyze store health and fix any issues"

**Classification**:
```json
{
  "domains": ["stores"],
  "phase": "investigation",
  "phase_plan": ["investigation", "execution"],
  "operation": "mixed",
  "intent": "analyze"
}
```

**Tools Exposed**: ALL 15 stores tools (ambiguous operation, needs flexibility)

**Token Savings**: 0 tokens (safety fallback - LLM needs all options)

---

## Operation Classification Logic

### Keyword-Based Classification (Fast-Path)

Add operation detection to existing fast-path classification:

```go
func inferOperationFromQuery(query string) string {
    q := strings.ToLower(strings.TrimSpace(query))
    
    // Read operations
    readKeywords := []string{
        "list", "show", "get", "display", "view", "read",
        "scan", "query", "search", "find", "describe", "check",
    }
    for _, keyword := range readKeywords {
        if strings.Contains(q, keyword) {
            return "read"
        }
    }
    
    // Create operations
    createKeywords := []string{
        "create", "add", "insert", "new", "make", "initialize",
    }
    for _, keyword := range createKeywords {
        if strings.Contains(q, keyword) {
            return "create"
        }
    }
    
    // Update operations
    updateKeywords := []string{
        "update", "modify", "change", "edit", "set", "configure",
    }
    for _, keyword := range updateKeywords {
        if strings.Contains(q, keyword) {
            return "update"
        }
    }
    
    // Delete operations
    deleteKeywords := []string{
        "delete", "remove", "drop", "clear", "destroy",
    }
    for _, keyword := range deleteKeywords {
        if strings.Contains(q, keyword) {
            return "delete"
        }
    }
    
    // DDL operations
    ddlKeywords := []string{
        "migrate", "schema", "alter", "index", "restructure",
    }
    for _, keyword := range ddlKeywords {
        if strings.Contains(q, keyword) {
            return "ddl"
        }
    }
    
    // Mixed or ambiguous
    return "mixed"
}
```

### LLM-Based Classification (Fallback)

For ambiguous cases, LLM classification includes operation in JSON output:

```go
func buildClassificationPromptV2(query string) string {
    return `Classify the user's query:

Query: "` + query + `"

Return JSON with:
{
  "domains": ["stores", "spaces", "desktop", "coding_workflow"],
  "phase": "investigation|planning|execution|review|delivery",
  "operation": "read|create|update|delete|ddl|mixed",
  "intent": "read_data|mutate_data|analyze|automate",
  "confidence": "high|medium|low"
}

Operation guidelines:
- "read": Read-only operations (list, get, scan, query)
- "create": Create new resources (create, insert, add)
- "update": Modify existing resources (update, edit, configure)
- "delete": Remove resources (delete, remove, drop)
- "ddl": Schema/structure changes (migrate, alter, create_index)
- "mixed": Multiple operation types or ambiguous intent`
}
```

---

## Tool Tagging Strategy

### Tagging Guidelines

Every tool should have 2 types of tags:
1. **Operation tag**: `read`, `create`, `update`, `delete`, `ddl`
2. **Phase tag**: `investigation`, `planning`, `execution`, `review`

**Example**:

```go
{
    Name: "scan_store_items",
    Description: "Scans all items in a store with optional filtering",
    Tags: []string{"read", "investigation"},  // Read-only, for investigation phase
}

{
    Name: "update_item",
    Description: "Updates a single item in a store",
    Tags: []string{"update", "execution"},  // Mutation, for execution phase
}

{
    Name: "migrate_store",
    Description: "Migrates a store to a new schema version",
    Tags: []string{"ddl", "execution"},  // DDL, for execution phase
}
```

### Tool Inventory by Domain

#### Stores Domain (15 tools)

**Read Operations** (5 tools, 400 tokens):
- `list_stores` - Lists all stores
- `get_store_config` - Gets store configuration
- `scan_store_items` - Scans all items
- `query_store` - Queries items with filters
- `describe_store_schema` - Describes store schema

**Create Operations** (3 tools, 240 tokens):
- `create_store` - Creates a new store
- `insert_item` - Inserts a single item
- `bulk_insert` - Bulk inserts items

**Update Operations** (3 tools, 240 tokens):
- `update_store_config` - Updates store configuration
- `update_item` - Updates a single item
- `bulk_update` - Bulk updates items

**Delete Operations** (3 tools, 240 tokens):
- `delete_store` - Deletes a store
- `delete_item` - Deletes a single item
- `bulk_delete` - Bulk deletes items

**DDL Operations** (1 tool, 80 tokens):
- `migrate_store` - Migrates store schema

---

#### Spaces Domain (12 tools)

**Read Operations** (4 tools, 320 tokens):
- `read_space_config` - Reads space configuration
- `list_space_items` - Lists all items in a space
- `search_space` - Searches space content
- `navigate_space_category` - Navigates category hierarchy

**Create Operations** (3 tools, 240 tokens):
- `mint_space_item` - Creates a new space item
- `create_space_category` - Creates a new category
- `initialize_space` - Initializes a new space

**Update Operations** (3 tools, 240 tokens):
- `enrich_space_item` - Enriches an existing item
- `update_space_config` - Updates space configuration
- `move_space_item` - Moves item to different category

**Delete Operations** (2 tools, 160 tokens):
- `delete_space_item` - Deletes a space item
- `delete_space_category` - Deletes a category

---

#### Desktop Domain (10 tools)

**Read Operations** (4 tools, 320 tokens):
- `read_file` - Reads file contents
- `list_directory` - Lists directory contents
- `check_file_exists` - Checks if file exists
- `get_working_directory` - Gets current directory

**Create Operations** (2 tools, 160 tokens):
- `create_file` - Creates a new file
- `create_directory` - Creates a new directory

**Update Operations** (2 tools, 160 tokens):
- `write_file` - Writes to a file
- `execute_command` - Executes shell command (mixed)

**Delete Operations** (2 tools, 160 tokens):
- `delete_file` - Deletes a file
- `delete_directory` - Deletes a directory

---

#### Coding Workflow Domain (20+ tools)

**Read Operations** (8 tools, 640 tokens):
- `read_file` - Reads file contents
- `search_code` - Searches codebase
- `find_references` - Finds symbol references
- `analyze_structure` - Analyzes code structure
- `get_diagnostics` - Gets linter/compiler errors
- `run_tests_read_only` - Runs tests in read-only mode
- `trace_dependencies` - Traces dependencies
- `check_types` - Checks type correctness

**Create Operations** (3 tools, 240 tokens):
- `create_file` - Creates a new file
- `generate_code` - Generates code from template
- `scaffold_project` - Scaffolds project structure

**Update Operations** (5 tools, 400 tokens):
- `edit_file` - Edits existing file
- `refactor_code` - Refactors code
- `apply_fix` - Applies automated fix
- `format_code` - Formats code
- `update_imports` - Updates import statements

**Delete Operations** (2 tools, 160 tokens):
- `delete_file` - Deletes a file
- `remove_code` - Removes code block

**Mixed Operations** (3 tools, 240 tokens):
- `run_tests` - Runs tests (reads + may create artifacts)
- `build_project` - Builds project (reads + creates output)
- `debug_program` - Debugs program (reads + steps)

---

## Token Savings Analysis

### Per-Query Savings

**Scenario**: "list all stores" (Read operation)

| Filtering Level | Tools Exposed | Tokens | Savings |
|----------------|---------------|--------|---------|
| None (V2 Pure) | 15 (all stores) + 10 (desktop) + 12 (spaces) = 37 tools | 2,960 | Baseline |
| Phase 2 (Domain) | 15 (all stores only) | 1,200 | -60% |
| **Phase 3 (Operation)** | **5 (read-only stores)** | **400** | **-87%** |

**Additional Savings from Phase 3**: 800 tokens per turn

---

### Multi-Turn Savings

**Average query**: 3 ReAct turns

**Phase 2 Only** (Domain filtering):
- Turn 1: 1,200 tokens (15 tools)
- Turn 2: 1,200 tokens (15 tools)
- Turn 3: 1,200 tokens (15 tools)
- **Total: 3,600 tokens**

**Phase 3** (Operation filtering):
- Turn 1: 400 tokens (5 tools)
- Turn 2: 400 tokens (5 tools)
- Turn 3: 400 tokens (5 tools)
- **Total: 1,200 tokens**

**Multi-turn savings**: 2,400 tokens per query (-67% additional)

---

### Combined Strategy Savings

**V2 Pure** (no optimization):
- Classification: 700 tokens
- Tool context (3 turns × 2,960 tokens): 8,880 tokens
- **Total: 9,580 tokens per query**

**Phase 1+2** (Fast-path + Domain filtering):
- Classification: 0 tokens (fast-path)
- Tool context (3 turns × 1,200 tokens): 3,600 tokens
- **Total: 3,600 tokens per query** (-62%)

**Phase 1+2+3** (Fast-path + Domain + Operation filtering):
- Classification: 0 tokens (fast-path)
- Tool context (3 turns × 400 tokens): 1,200 tokens
- **Total: 1,200 tokens per query** (-87%)

**Combined savings**: **8,380 tokens per query** (87% reduction)

---

## Cost Analysis

### Per-Query Cost

**Model**: Claude Sonnet 3.5
- Input: $3.00 per 1M tokens
- Output: $15.00 per 1M tokens
- Average: ~$0.003 per 1K input tokens

**V2 Pure**:
- 9,580 tokens × $0.003 = **$0.029 per query**

**Phase 1+2** (Domain filtering):
- 3,600 tokens × $0.003 = **$0.011 per query** (-62%)

**Phase 1+2+3** (Operation filtering):
- 1,200 tokens × $0.003 = **$0.004 per query** (-87%)

**With Prompt Caching** (90% reduction on repeated context):
- Phase 1+2+3: **$0.0004 per query** (87% + 90% = 98.7% total reduction)

---

### Monthly Cost Projection

**Assumptions**:
- 10,000 queries/day
- 300,000 queries/month
- 50% cache hit rate

**V2 Pure**:
- $0.029 × 300,000 = **$8,700/month**

**Phase 1+2** (Domain filtering):
- $0.011 × 150,000 (cold) = $1,650
- $0.0011 × 150,000 (cached) = $165
- **Total: $1,815/month** (-79%)

**Phase 1+2+3** (Operation filtering):
- $0.004 × 150,000 (cold) = $600
- $0.0004 × 150,000 (cached) = $60
- **Total: $660/month** (-92%)

**Monthly savings**: **$8,040** vs V2 Pure

---

## Implementation Plan

### Phase 1: Tool CRUD Tagging (Week 1)

**Tasks**:
1. ✅ **Reuse existing infrastructure**: `LayerInfo.CRUD` and `collectCRUDFlags()` already exist
2. Add `Tags []string` field to `ai.ToolDefinition` struct (or reuse existing)
3. Tag all existing tools in capabilities with CRUD flags:
   - `stores.go`: Tag 15 store tools with C/R/U/D
   - `spaces.go`: Tag 12 space tools with C/R/U/D
   - `desktop.go`: Tag 10 desktop tools with R/U (mostly read, some updates)
   - `coding_workflow.go`: Tag 20+ coding tools with C/R/U/D
4. Write tests for CRUD tag matching logic

**Files**:
- `lib/sop/ai/types.go`: Add Tags field (if not exists)
- `lib/sop/ai/agent/capabilities/stores/stores.go`: Add CRUD tags
- `lib/sop/ai/agent/capabilities/spaces/spaces.go`: Add CRUD tags
- `lib/sop/ai/agent/capabilities/desktop/desktop.go`: Add CRUD tags
- `lib/sop/ai/agent/capabilities/codingworkflow/*.go`: Add CRUD tags

**Effort**: 1-2 days (infrastructure already exists, just extend it)

---

### Phase 2: Routing State Enhancement (Week 2)

**Tasks**:
1. ✅ **Reuse existing**: `TaskContextClassification.Layers[].CRUD` already captures CRUD
2. Extend `capabilities.RoutingState` to include `CRUDFlags map[string]bool`
3. Propagate CRUD flags from classification to routing state
4. Add CRUD flags to V2 classification output (already in V1)

**Files**:
- `lib/sop/ai/agent/capabilities/types.go`: Extend RoutingState with CRUDFlags
- `lib/sop/ai/agent/copilot_v2.go`: Propagate CRUD to routing state
- `lib/sop/ai/agent/classifier_v2.go`: Ensure V2 classification includes CRUD (already in V1)
- `lib/sop/ai/agent/copilot_v2_test.go`: Add CRUD propagation tests

**Effort**: 1-2 days (mostly wiring existing data through V2 pipeline)

---

### Phase 3: Tool Filtering Logic (Week 3)

**Tasks**:
1. Implement `filterToolsByCRUD()` function (reuses existing CRUD flags)
2. Update capability `ExposeTools()` to accept and use CRUD flags from routing state
3. Add safety fallback (return all tools if filtering results in 0 tools)
4. Add telemetry for filtered tool counts

**Files**:
- `lib/sop/ai/agent/capabilities/stores/stores.go`: Implement CRUD filtering in ExposeTools
- `lib/sop/ai/agent/capabilities/spaces/spaces.go`: Implement CRUD filtering in ExposeTools
- `lib/sop/ai/agent/capabilities/types.go`: Add helper `filterToolsByCRUD()`
- `lib/sop/ai/agent/copilot_v2.go`: Wire CRUD flags to ExposeTools calls
- `lib/sop/ai/agent/copilot_v2_test.go`: Add filtering tests

**Effort**: 2-3 days (reusing existing CRUD infrastructure)

---

### Phase 4: Testing & Validation (Week 4)

**Tasks**:
1. Unit tests for operation inference
2. Unit tests for tool filtering
3. Integration tests for E2E flow
4. A/B test: Phase 1+2 vs Phase 1+2+3
5. Measure: token reduction, cost, quality, latency

**Files**:
- `lib/sop/ai/agent/copilot_v2_test.go`: Add comprehensive tests
- `test/integration/token_consumption_test.go`: Add integration tests

**Effort**: 3-4 days

---

### Total Timeline: 2-3 weeks (Reduced from 3-4 weeks)

**Why Faster**: Existing CRUD infrastructure means 50% of the work is already done. This is primarily wiring existing data through V2 pipeline, not building new classification/collection logic.

---

## Risks & Mitigations

### Risk 1: Over-Filtering

**Problem**: Operation classification incorrectly filters out needed tools

**Example**: Query "list stores and create one if none exist"
- Operation: "read" (from "list" keyword)
- Filtered tools: Only read tools exposed
- **Problem**: LLM can't execute "create" part

**Mitigation**:
1. Use "mixed" operation for multi-operation queries
2. Implement safety fallback: If filtering results in 0 tools, expose all
3. Add telemetry to detect over-filtering cases
4. LLM can request tool re-exposure if needed

**Severity**: Medium  
**Likelihood**: Low (LLM classification detects mixed operations)

---

### Risk 2: Keyword-Based Classification Errors

**Problem**: Fast-path keyword matching misclassifies operation

**Example**: "show me how to delete a store" (educational query, not actual deletion)
- Operation: "delete" (from "delete" keyword)
- Filtered tools: Only delete tools exposed
- **Problem**: User wanted explanation, not actual deletion

**Mitigation**:
1. Add educational query detection in fast-path
2. Fall back to LLM classification for ambiguous queries
3. Use "mixed" operation for educational queries
4. Add confidence scoring

**Severity**: Low (educational queries are rare in operational context)  
**Likelihood**: Low

---

### Risk 3: Reduced LLM Flexibility

**Problem**: Over-filtering limits LLM's ability to adapt

**Example**: User asks "list stores" → LLM discovers issue → Wants to fix but tools aren't exposed

**Mitigation**:
1. LLM can request additional tools via special tool: `request_additional_tools(reason)`
2. Multi-phase queries expose different tools per phase
3. "mixed" operation fallback for ambiguous cases
4. Telemetry to detect cases where LLM requested additional tools

**Severity**: Medium  
**Likelihood**: Medium (LLM adaptation scenarios are common)

**Recommendation**: Implement `request_additional_tools` escape hatch

---

### Risk 4: Implementation Complexity

**Problem**: Tagging all tools is tedious and error-prone

**Mitigation**:
1. Create tool tagging guidelines document
2. Use consistent tag vocabulary (read/create/update/delete/ddl)
3. Add validation tests for tag completeness
4. Generate tag suggestions from tool names/descriptions

**Severity**: Low  
**Likelihood**: High

---

## Success Metrics

### Token Reduction Metrics

**Target**: 40-60% additional reduction over Phase 1+2

**Measure**:
- Tokens per turn (before vs after)
- Tools exposed per turn (before vs after)
- Filtering effectiveness (% queries with reduced tool count)

**Goal**:
- Phase 1+2: 3,600 tokens per query
- Phase 1+2+3: 1,200 tokens per query (-67%)

---

### Cost Reduction Metrics

**Target**: 67% additional cost reduction over Phase 1+2

**Measure**:
- Cost per query (before vs after)
- Monthly cost projection
- ROI (implementation cost vs savings)

**Goal**:
- Phase 1+2: $0.011 per query
- Phase 1+2+3: $0.004 per query (-64%)

---

### Quality Metrics

**Target**: Maintain or improve task success rate (≥95%)

**Measure**:
- Task completion rate (did LLM complete task successfully?)
- User corrections (did user have to correct LLM?)
- Tool re-exposure requests (did LLM request additional tools?)
- Retry rate (did user have to retry query?)

**Goal**: No degradation in success rate vs Phase 1+2

---

### Latency Metrics

**Target**: No latency increase (operation classification is fast-path)

**Measure**:
- Time to first tool call
- Total query time
- Prompt construction time

**Goal**: Same or better latency vs Phase 1+2

---

## Recommendation

### ✅ Implement CRUD-Based Tool Filtering

**Justification**:
1. **High value**: 40-60% additional token reduction (stacks on Phase 1+2)
2. **Low risk**: CRUD infrastructure already proven in V1 production
3. **Minimal new code**: Primarily wiring existing CRUD to ExposeTools()
4. **Incremental**: Builds on existing V1 infrastructure (50% done)
5. **Measurable**: Clear metrics for success
6. **Reversible**: Can disable filtering if issues arise

**Timeline**: 2-3 weeks (reduced from 3-4 due to existing infrastructure)

**ROI**:
- Implementation cost: ~$10,000 (2-3 weeks × 1 engineer, reduced from $15k)
- Monthly savings: $8,040 (vs V2 Pure)
- **Payback period**: 1.2 months (improved from 1.8 months)

---

### Implementation Order

**Week 1-2**: Implement Phase 1+2 (Fast-path + Domain filtering)
- Lower complexity, higher confidence
- Establish baseline metrics
- Validate hybrid approach

**Week 3-6**: Implement Phase 3 (Operation filtering)
- Build on Phase 1+2 infrastructure
- Tag tools incrementally by domain
- A/B test each domain independently

**Month 2**: Production rollout
- Monitor telemetry for over-filtering
- Iterate based on metrics
- Expand to all domains

---

## Alternatives Considered

### Alternative 1: Phase-Only Filtering (No Operation)

**Concept**: Filter tools by Phase only (investigation, execution, etc.)

**Pros**:
- Simpler than operation filtering
- Less risk of over-filtering

**Cons**:
- Lower token savings (20-30% vs 40-60%)
- Still exposes many irrelevant tools within phase

**Verdict**: ❌ Not recommended - operation filtering provides significantly more value for similar complexity

---

### Alternative 2: Intent-Based Filtering (Not CRUD)

**Concept**: Filter by high-level intent (read_data, mutate_data, analyze) instead of CRUD operations

**Pros**:
- More semantic than CRUD
- Aligns with existing classification

**Cons**:
- Less granular than CRUD (mutate_data includes create + update + delete)
- Lower token savings (30-40% vs 40-60%)

**Verdict**: ❌ Not recommended - CRUD provides better granularity for similar complexity

---

### Alternative 3: LLM-Only Classification (No Fast-Path Operation)

**Concept**: Always use LLM to classify operation (no keyword-based fast-path)

**Pros**:
- More accurate classification
- Handles ambiguous cases better

**Cons**:
- Adds classification latency (200-500ms)
- Negates Phase 1 fast-path benefits

**Verdict**: ❌ Not recommended - fast-path keyword matching is sufficient for most queries

---

## Next Steps

### Immediate Actions (This Week)

1. ✅ **Review and approve proposal**
2. ✅ **Create implementation plan**
3. ✅ **Set up telemetry for baseline metrics**

### Week 1-2: Phase 1+2 Implementation

4. ✅ Implement fast-path classification
5. ✅ Implement domain pre-filtering
6. ✅ A/B test Phase 1+2 vs V2 Pure
7. ✅ Measure token/cost reduction

### Week 3-4: Phase 3 Design

8. ✅ Design tool tagging strategy
9. ✅ Create tagging guidelines
10. ✅ Prototype operation classification

### Week 5-8: Phase 3 Implementation

11. ✅ Tag all tools by domain
12. ✅ Implement operation filtering
13. ✅ A/B test Phase 1+2+3 vs Phase 1+2
14. ✅ Measure token/cost reduction

### Month 2: Production Rollout

15. ✅ Monitor telemetry for issues
16. ✅ Iterate based on metrics
17. ✅ Expand to all domains

---

## Conclusion

**CRUD-based tool filtering** is a natural extension of **existing V1 infrastructure** that provides **40-60% additional token reduction** with **low risk** and **high confidence**.

**Existing Infrastructure** (already working in V1):
- ✅ `LayerInfo.CRUD[]` - Classification captures CRUD flags
- ✅ `collectCRUDFlags()` - Aggregates CRUD from layers
- ✅ `ExecutionContextInput{CRUDFlags}` - Passes CRUD to capabilities
- ✅ `buildStoresCRUDOperationsContext()` - Shows CRUD guidance
- ⚠️ **Gap**: CRUD flags shown in prompt but NOT used to filter tools

**This Proposal**: **Extend existing CRUD to filter tools** in V2 Ask

**Key Benefits**:
- ✅ **87% total token reduction** vs V2 Pure (combined with Phase 1+2)
- ✅ **92% cost reduction** (with prompt caching)
- ✅ **Maintains LLM orchestration** (LLM still decides which tools to call)
- ✅ **Low risk** (safety fallbacks prevent over-filtering)
- ✅ **Reuses existing infrastructure** (CRUD already classified and collected)
- ✅ **Minimal new code** (just wire existing CRUD to ExposeTools)

**Recommendation**: ✅ **Approve and implement** - This is primarily wiring existing CRUD data to tool filtering, not building new infrastructure.

**Expected Results**:
- **1,200 tokens per query** (down from 9,580 in V2 Pure)
- **$0.004 per query** (down from $0.029 in V2 Pure)
- **$660/month** (down from $8,700 in V2 Pure)
- **98.7% total cost reduction** with prompt caching

---

## Appendix: Tool Tagging Reference

### Complete Tag Vocabulary

**CRUD Tags** (reuse existing V1 flags):
- `C`: Create operations (insert, add, create)
- `R`: Read operations (list, get, scan, query, describe)
- `U`: Update operations (update, edit, modify, configure)
- `D`: Delete operations (delete, remove, drop)

**Note**: V1 already uses these exact CRUD flags in `LayerInfo.CRUD[]`. This proposal simply extends them to filter tools.

**Example Tool Tags** (map to existing CRUD):
```go
// Read-only tool
{
    Name: "list_stores",
    Tags: []string{"R"},  // Maps to existing "R" CRUD flag
}

// Mutation tool
{
    Name: "create_store",
    Tags: []string{"C"},  // Maps to existing "C" CRUD flag
}

// Multiple operations
{
    Name: "upsert_item",
    Tags: []string{"C", "U"},  // Both create and update
}
```

---

**End of Proposal**
