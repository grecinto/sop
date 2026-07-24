# Category Hierarchy Integrity Requirements

**Date**: 2026-07-23  
**Status**: Critical Architecture Requirement  
**Related**: `DYNAMIC_VECTOR_STORE_DESIGN.md` Section 12 - Semantic Path Search

## The Problem

The Semantic Path Search algorithm (Section 12 of DYNAMIC_VECTOR_STORE_DESIGN.md) requires **complete hierarchical category structures** for proper operation. However, the current `EnsureCategory` implementation creates only leaf categories without intermediate parents, breaking semantic triangulation.

## Semantic Triangulation Requirements

For a path like `"Playbooks/TestDebugging/Go/UnitTests"`, the algorithm expects:

### Level 0: Root Anchor (DomainReference)
```
Query: "Playbooks"
Anchor: DomainReference (global KB anchor)
Expected: Category{Name: "Playbooks", Path: "Playbooks", ParentID: NilUUID}
```

### Level 1: Parent as Anchor
```
Query: "TestDebugging"
Anchor: "Playbooks".CenterVector
Expected: Category{Name: "TestDebugging", Path: "Playbooks/TestDebugging", ParentID: "Playbooks".ID}
```

### Level 2: Grandparent as Anchor
```
Query: "Go"
Anchor: "TestDebugging".CenterVector
Expected: Category{Name: "Go", Path: "Playbooks/TestDebugging/Go", ParentID: "TestDebugging".ID}
```

### Level 3: Leaf
```
Query: "UnitTests"
Anchor: "Go".CenterVector
Expected: Category{Name: "UnitTests", Path: "Playbooks/TestDebugging/Go/UnitTests", ParentID: "Go".ID}
```

## Why Intermediates Are Critical

The `SemanticCategoryByPath` algorithm performs hierarchical drill-down:

```go
// Level i uses parent's CenterVector and ID as anchor
func semanticCategoryByPartRecursive(ctx, parentID, anchor, pathVectors, level) {
    candidates := semanticCategoryByPart(ctx, parentID, anchor, pathVectors[level])
    
    // Recursively drill down using candidate as next parent
    for candidate in candidates {
        subCandidates := semanticCategoryByPartRecursive(
            ctx, 
            candidate.ID,           // ← Requires actual category ID
            candidate.CenterVector, // ← Requires actual vector
            pathVectors, 
            level+1
        )
    }
}
```

**Without intermediate categories:**
- ❌ No `ParentID` to scope child searches
- ❌ No `CenterVector` to use as spatial anchor
- ❌ No entries in `CategoriesByDistance` B-Tree for navigation
- ❌ Algorithm returns empty or fails silently

## Current Implementation Gap

### `EnsureCategory` Behavior (manager.go:1289)

```go
func EnsureCategory(ctx, categoryPath) (UUID, error) {
    // Scans for existing full path
    // If not found, creates ONLY leaf:
    
    parts := strings.Split(categoryPath, "/")
    leafName := parts[len(parts)-1]  // Last component only
    
    anchor := &Category{
        ID:        sop.NewUUID(),
        Name:      leafName,           // "UnitTests"
        Path:      categoryPath,       // Full: "Playbooks/TestDebugging/Go/UnitTests"
        ParentIDs: nil,                // ❌ NO PARENT SET
    }
    
    return m.store.AddCategory(ctx, anchor)  // Creates only leaf
}
```

**Problem:** Calling `EnsureCategory("Playbooks/TestDebugging/Go/UnitTests")` creates:
- ✅ Category: `Path="Playbooks/TestDebugging/Go/UnitTests"`
- ❌ Missing: `"Playbooks"`, `"Playbooks/TestDebugging"`, `"Playbooks/TestDebugging/Go"`

## When This Breaks

### 1. Direct KB API Usage
```go
kb.IngestThoughts(ctx, []Thought{
    {CategoryPath: "Playbooks/TestDebugging/Go/UnitTests", ...}
})
// → Calls EnsureCategory → Creates only leaf
```

### 2. Workflow Ingestion
```go
agent.IngestWorkflow(ctx, kb, "workflows/test_debugging_go_v1.json")
// → Calls UpsertItems with CategoryPath from JSON → Calls EnsureCategory
```

### 3. Any Runtime Category Creation
Outside of preload JSON (which has full hierarchy), any dynamic category path creation skips intermediates.

## Why Preload Works

The KB compiler and preload JSON (`ai/sop_base_knowledge.json`) explicitly define each level:

```json
{
  "categories": [
    {"id": "...", "name": "Playbooks", "path": "Playbooks"},
    {"id": "...", "name": "Coding", "path": "Playbooks/Coding", "parents": [{"parent_id": "..."}]},
    {"id": "...", "name": "Go", "path": "Coding Best Practices/Go", "parents": [{"parent_id": "..."}]},
    ...
  ]
}
```

**This creates the full hierarchy at ingest time.**

## The Solution: Auto-Create Hierarchy

Modify `EnsureCategory` to create all intermediate categories:

```go
func EnsureCategory(ctx, categoryPath) (leafID UUID, error) {
    parts := strings.Split(categoryPath, "/")
    
    var parentID UUID = sop.NilUUID
    for i := 0; i < len(parts); i++ {
        currentPath := strings.Join(parts[0:i+1], "/")
        currentName := parts[i]
        
        // Check if category exists
        catID, err := findCategoryByExactPath(ctx, currentPath)
        if err != nil {
            return NilUUID, err
        }
        
        if catID.IsNil() {
            // Create category with proper parent link
            cat := &Category{
                ID:   sop.NewUUID(),
                Name: currentName,
                Path: currentPath,
            }
            
            if !parentID.IsNil() {
                cat.ParentIDs = []CategoryParent{{ParentID: parentID}}
            }
            
            catID, err = m.store.AddCategory(ctx, cat)
            if err != nil {
                return NilUUID, err
            }
        }
        
        parentID = catID  // Use as parent for next level
    }
    
    return parentID, nil  // Return leaf ID
}
```

## Backward Compatibility

The fix is **backward compatible** because:

1. **Existing preload paths**: Already have full hierarchy, so `findCategoryByExactPath` will find them and skip creation
2. **No schema changes**: Same Category struct, same B-Tree layout
3. **Same return value**: Still returns leaf category ID
4. **Idempotent**: Multiple calls create hierarchy only once
5. **Transaction safe**: All creates in same transaction, rollback if any fails

## Case Handling Convention

**Implementation**: Case-insensitive lookup with original case preservation ("first-wins" strategy).

```go
// Lookup uses strings.EqualFold for case-insensitive comparison
existingID := findCategoryByExactPath(ctx, categoryPath)

// Storage preserves the exact case provided in the first creation
cat := &Category{
    Name: currentName,        // Preserves original case
    Path: currentPath,        // Preserves original case
}
```

**Behavior:**
- ✅ Case-insensitive lookups prevent duplicates
- ✅ First-created version determines stored casing
- ✅ Later calls with different casing return existing category
- ✅ UI can apply display transformations independently

**Example:**
```go
// First call establishes the canonical casing
id1 := EnsureCategory(ctx, "Coding Best Practices/Go")
// Stored as: "Coding Best Practices/Go"

// Later calls with different casing find the same category
id2 := EnsureCategory(ctx, "coding best practices/go")
// Returns: same id1, no duplicate created

// All variations map to the first version's casing
id3 := EnsureCategory(ctx, "CODING BEST PRACTICES/GO")
// Returns: same id1, stored path remains "Coding Best Practices/Go"
```

**Best Practice Convention:**

Follow the title case pattern established in preload JSON files:
```json
{
  "path": "Coding Best Practices/Go/Error Handling",
  "name": "Error Handling"
}
```

**Recommended casing for common patterns:**
- ✅ `"Coding Best Practices/Go/Error Handling"` (Title Case)
- ✅ `"Playbooks/Debugging/Unit Tests"` (Title Case)
- ✅ `"API/REST/Authentication"` (Preserve acronyms in uppercase)
- ⚠️ Avoid all-lowercase unless intentional: `"api/rest/auth"`
- ⚠️ Avoid all-uppercase unless semantic: `"URGENT/CRITICAL"`

**Rationale:**
- Preserves flexibility for special cases (acronyms, semantic all-caps)
- Maintains consistency with existing preload data
- Allows UI to handle display styling independently
- Avoids forced normalization that loses semantic information

## Benefits

- ✅ Semantic triangulation works correctly for all paths
- ✅ Runtime category creation matches preload behavior
- ✅ Developer API usage (IngestThoughts, UpsertItems) works reliably
- ✅ Workflow ingestion creates proper hierarchy
- ✅ No breaking changes to existing code
- ✅ Maintains ACID transactionality
- ✅ Case-insensitive lookups prevent duplicates while preserving original casing
- ✅ Flexible convention supports special cases (acronyms, semantic capitalization)

## Testing Requirements

After implementing the fix:

1. **Test direct IngestThoughts** with deep category paths
2. **Test workflow ingestion** with hierarchical category_path
3. **Test SemanticCategoryByPath** with dynamically created categories
4. **Verify CategoriesByDistance** entries for all levels
5. **Test ParentIDs** are correctly set for each level
6. **Verify vectorization** updates CenterVector for intermediates

## References

- `lib/sop/ai/DYNAMIC_VECTOR_STORE_DESIGN.md` - Section 12: Semantic Path Search
- `lib/sop/ai/memory/store.go` - `SemanticCategoryByPath` implementation
- `lib/sop/ai/memory/manager.go` - `EnsureCategory` current implementation
- `lib/sop/ai/memory/knowledge_base.go` - `IngestThoughts` usage
