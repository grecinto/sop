# Work Inventory Generalization - Aug 31, 2026

## What Was Done

Generalized the test-failure-specific `FailureClusters` pattern to work across ALL domains, and then extended it into a repo-aware inventory compiler for coding tasks.

The important refinement is that this is not a task-specific workflow primitive. The runtime remains generic, and the inventory is the objective-scoped work model that runtime operates on.

The current architecture now treats the phase plan as the macro controller and the work inventory as the per-phase execution substrate:

- Phase plan answers: "What is the current objective progression?"
- Work inventory answers: "What concrete work items are active for the current objective?"
- The Ask/ReAct loop registers the active phase tools, and the coding phase emits a persisted inventory snapshot that later turns can hydrate and continue.

## Design Refinement

The inventory model should be understood as a generic objective work graph, not a coding-only artifact list and not a disguised workflow.

Architecture rule:

- A user request selects an objective, not a workflow.
- The Ask/ReAct loop remains the only controller.
- AST, tree-sitter, search, diagnostics, and test discovery are reusable signal sources.
- Inventory compilation, ranking, item selection, and completion are generic runtime operations.
- Variations such as test repair, codebase analysis, feature implementation, or design work are profile-level differences in ranking and completion criteria, not separate orchestrators.

This means an inventory item may represent a file, symbol, test failure, dependency boundary, design decision, validation task, or implementation target. Repo artifact inventories are one important compiler for this work model, not the definition of the model itself.

## Changes Made

### 1. **New Generic Types** (`memory_shortterm.go`)

```go
type WorkInventory struct {
    Items          []InventoryItem  // All work items
    ActiveItemID   string           // Current item being worked on
    CompletedItems []string         // Completed item IDs
    InventoryType  string           // "test_failures", "functions", "bugs", etc.
}

type InventoryItem struct {
    ID          string              // "test:TestAuth", "function:handleLogin"
    Type        string              // "test", "function", "bug", "file"
    DisplayName string              // Human-readable (stripped prefixes)
    Status      string              // "pending", "active", "completed", "failed"
    Metadata    map[string]any      // Domain-specific data
}
```

### 2. **Generic Progress Tracker** (`focused_context.go`)

**New function:** `formatWorkInventoryProgress(inventory *WorkInventory)`
- Works for ANY domain (tests, functions, bugs, files, endpoints, refactoring)
- Visual format with status markers: ◉ (active), ☐ (pending), ✓ (completed), ✗ (failed), ⊗ (blocked)
- Dynamic titles based on inventory type

**New function:** `getProgressTrackerTitle(inventoryType string)`
- Returns appropriate title: "TEST FAILURE PROGRESS TRACKER", "FUNCTION IMPLEMENTATION PROGRESS", etc.

**Legacy function:** `formatFailureClusterProgress()` - Kept for backward compatibility

### 3. **Backward Compatible Migration** (`memory_shortterm.go`)

```go
type WorkStreamContext struct {
    // NEW: Generic work inventory
    WorkInventory *WorkInventory
    
    // DEPRECATED: Legacy test-specific fields (kept for compatibility)
    FailureClusters          []string
    ActiveFailureCluster     string
    FailureInventoryCaptured bool
}
```

### 4. **Helper Methods** (`memory_shortterm.go`)

```go
func (wi *WorkInventory) GetActiveItem() *InventoryItem
func (wi *WorkInventory) GetPendingItems() []InventoryItem
func (wi *WorkInventory) MarkItemCompleted(itemID string) bool
```

### 5. **Comprehensive Tests** (`focused_context_test.go`)

**6 new generic tests:**
- `TestFormatWorkInventoryProgress_Empty` - Nil and empty handling
- `TestFormatWorkInventoryProgress_SingleItem` - Single item display
- `TestFormatWorkInventoryProgress_MultipleItems` - Multiple items with active marker
- `TestFormatWorkInventoryProgress_CompletedItems` - Completed items tracking
- `TestFormatWorkInventoryProgress_DifferentInventoryTypes` - All domain titles
- `TestFormatWorkInventoryProgress_StatusMarkers` - All status markers (◉☐✓✗⊗)

### 6. **Structural Inputs Already Available for Inventory Compilation**

The current coding workflow already produces the raw signals needed to compile a richer artifact inventory:

- `ContextPack` carries `RepositoryFiles`, `RelatedFiles`, `TestFiles`, `DependencyFiles`, `Symbols`, `ASTLeafNodes`, and `StructuralPath`
- `ContextManager` uses AST/tree-sitter to extract symbols and structure from active files
- `CodeArtifactExtractor` extracts file-level code artifacts from modified files for undo and restore flows
- `RepoIndexer` and repository search provide broader discovery beyond the active file

What is still missing is a more fully generalized turn-facing compiler layer that can turn those signals into a canonical worklist the LLM can consume and update across multiple objective types.

**7 legacy tests:** Unchanged, still passing for backward compatibility

## What This Enables

### Immediate Benefits

**Test Failures (existing):**
```go
inventory := &WorkInventory{
    InventoryType: "test_failures",
    Items: []InventoryItem{
        {ID: "test:TestAuth", DisplayName: "TestAuth", Status: "active"},
        {ID: "test:TestUser", DisplayName: "TestUser", Status: "pending"},
    },
}
// Visual tracker shows: ◉ TestAuth ← 🎯 CURRENT TARGET
```

**Function Implementation (new):**
```go
inventory := &WorkInventory{
    InventoryType: "functions",
    Items: []InventoryItem{
        {ID: "function:handleLogin", DisplayName: "handleLogin", Status: "active"},
        {ID: "function:handleLogout", DisplayName: "handleLogout", Status: "pending"},
    },
}
// Tracker title: "FUNCTION IMPLEMENTATION PROGRESS"
```

**Bug Fixes (new):**
```go
inventory := &WorkInventory{
    InventoryType: "bugs",
    Items: []InventoryItem{
        {ID: "bug:AUTH-123", DisplayName: "Login timeout", Status: "completed"},
        {ID: "bug:AUTH-124", DisplayName: "Token refresh", Status: "active"},
    },
}
// Shows: ✓ Login timeout (completed), ◉ Token refresh ← 🎯 CURRENT TARGET
```

**File Updates (new):**
```go
inventory := &WorkInventory{
    InventoryType: "files",
    Items: []InventoryItem{
        {ID: "file:auth.go", DisplayName: "auth.go", Status: "active"},
        {ID: "file:user.go", DisplayName: "user.go", Status: "pending"},
    },
}
// Tracker title: "FILE UPDATE PROGRESS TRACKER"
```

**API Endpoints (new):**
```go
inventory := &WorkInventory{
    InventoryType: "endpoints",
    Items: []InventoryItem{
        {ID: "endpoint:POST /auth/login", DisplayName: "POST /auth/login", Status: "active"},
        {ID: "endpoint:POST /auth/logout", DisplayName: "POST /auth/logout", Status: "pending"},
    },
}
// Tracker title: "ENDPOINT IMPLEMENTATION PROGRESS"
```

### Universal Pattern

**Any domain can now use itemized progress tracking through the same runtime loop:**

1. **Investigation phase:** Compile inventory
   ```go
   items := []InventoryItem{}
   // ... discover work units ...
   thread.StreamContext.WorkInventory = &WorkInventory{Items: items, InventoryType: "domain"}
   ```

2. **Execution phase:** Visual tracker displays automatically
   ```
   ═══════════════════════════════════════════
   [DOMAIN-SPECIFIC TITLE]
   ═══════════════════════════════════════════
   Total items: 5 | Completed: 2 | Working: 1 at a time
   
     1. ✓ ItemA (completed)
     2. ✓ ItemB (completed)
     3. ◉ ItemC ← 🎯 CURRENT TARGET - WORK ON THIS NOW
     4. ☐ ItemD (pending)
     5. ☐ ItemE (pending)
   
   ⚠️  CRITICAL: Fix ONLY the item marked ◉ CURRENT TARGET
   ⚠️  After fixing: Run TARGETED validation for that item only
   ⚠️  System automatically moves to next item after validation
   ═══════════════════════════════════════════
   ```

3. **Automatic progression:** `MarkItemCompleted()` moves to next

## Migration Path

### Phase 1: Dual Support (Current)
- New code uses `WorkInventory`
- Legacy code uses `FailureClusters` (still works)
- Progress tracker checks both paths

### Phase 2: Populate Both (Next)
- Update `copilottools.automation.go` to populate both old and new fields
- All workflows benefit from visual tracking

### Phase 3: Full Migration (Future)
- Deprecate `FailureClusters`, `ActiveFailureCluster`, `FailureInventoryCaptured`
- All domains use `WorkInventory`

### Phase 4: Artifact Inventory Compiler (Implemented)
- Add a dedicated inventory tool that compiles files, symbols, tests, interfaces, configs, dependency clusters, package scopes, and execution-surface groupings into `WorkInventory`
- Persist the compiled inventory so later turns can resume from the same worklist instead of rebuilding it from scratch
- Add selection/update operations so the model can mark a single item active, complete it, and move to the next target
- Use AST/tree-sitter as structural signals for item boundaries and dependency grouping, with repo search, diagnostics, and filesystem scans as supporting signals
- Treat this compiler as one reusable provider of work items inside a generic runtime, not as a special-purpose coding workflow controller

### Runtime Integration Pattern

The inventory is deliberately layered under the phase plan, not instead of it:

1. Ask resolves the current objective context and picks the active phase or runtime mode.
2. The active capability exposes inventory tools to ReAct.
3. The LLM runs `compile_artifact_inventory` or another inventory compiler to build the objective-scoped work inventory.
4. The LLM selects the active item with `select_inventory_item`.
5. The active item becomes the execution boundary for reads, edits, and validation.
6. `complete_inventory_item` marks the item done and advances the list.

This preserves the current Ask/ReAct flow without hiding workflow state inside `context.Context`; state is explicit in task/workstream data, phase artifacts, and the active inventory snapshot.

## Architecture Validation

✅ **Industry Aligned:** Cursor/Devin/Copilot track work items generically, not domain-specifically

✅ **DRY Principle:** One implementation serves infinite domains

✅ **Extensible:** New domains just define `InventoryType` and populate items

✅ **Backward Compatible:** Legacy code continues working during transition

## Test Results

```bash
$ go build ./cmd/sop-daemon
✓ BUILD SUCCESS

$ go test -v -run FormatWorkInventoryProgress ./ai/agent/
=== RUN   TestFormatWorkInventoryProgress_Empty
--- PASS: TestFormatWorkInventoryProgress_Empty (0.00s)
=== RUN   TestFormatWorkInventoryProgress_SingleItem
--- PASS: TestFormatWorkInventoryProgress_SingleItem (0.00s)
=== RUN   TestFormatWorkInventoryProgress_MultipleItems
--- PASS: TestFormatWorkInventoryProgress_MultipleItems (0.00s)
=== RUN   TestFormatWorkInventoryProgress_CompletedItems
--- PASS: TestFormatWorkInventoryProgress_CompletedItems (0.00s)
=== RUN   TestFormatWorkInventoryProgress_DifferentInventoryTypes
--- PASS: TestFormatWorkInventoryProgress_DifferentInventoryTypes (0.00s)
=== RUN   TestFormatWorkInventoryProgress_StatusMarkers
--- PASS: TestFormatWorkInventoryProgress_StatusMarkers (0.00s)
PASS (6/6 tests)

$ go test -v -run FormatFailureClusterProgress ./ai/agent/
PASS (7/7 legacy tests - backward compatibility preserved)
```

## What's Next

1. Define the canonical inventory schema for objective work items so the tool output is stable across domains and repositories.
2. Decide how compiler providers contribute inventory items for different objectives without creating scenario-specific controllers.
3. Add tests for inventory compilation, ranking, selection, and persistence so the worklist behavior is explicit and resumable.

1. **Update `copilottools.automation.go`** - Populate `WorkInventory` from test results
2. **Add domain helpers** - Functions to create inventories for functions, bugs, files, etc.
3. **Enable in other domains** - Stores, spaces, desktop can now use itemized progress
4. **Update documentation** - Show examples for each domain

---

**Summary:** Single refactor enables itemized progress tracking across ALL domains. Code is cleaner, more maintainable, and infinitely extensible. ✅
