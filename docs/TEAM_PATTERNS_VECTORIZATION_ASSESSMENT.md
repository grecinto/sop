# Team Patterns Vectorization Assessment

**Date**: 2026-07-31  
**Context**: After user stores team patterns, we need to vectorize them for semantic search

---

## Three Vectorization Functions in `ai/database`

### 1. `Vectorize()` - Full Space Vectorization ⚠️ **EXPENSIVE**
```go
func (db *Database) Vectorize(
    ctx context.Context,
    kbName string,
    llm ai.Generator,
    embedder ai.Embeddings,
    batchSize int,
) error
```

**What it does**:
- Vectorizes **ENTIRE knowledge base**
- Processes ALL categories from root
- Processes ALL items in ALL categories
- Scans `CategoriesByPath` B-tree completely

**Use cases**:
- Initial setup of a new knowledge base
- Bulk re-vectorization after embedder upgrade
- Full index rebuild

**Performance**:
- O(C + I) where C = all categories, I = all items
- For LTM with 100s of categories: **Very expensive** (minutes)
- **NOT appropriate for Team Patterns**

---

### 2. `VectorizeCategories()` - Targeted Category Vectorization ✅ **RECOMMENDED**
```go
func (db *Database) VectorizeCategories(
    ctx context.Context,
    kbName string,
    llm ai.Generator,
    embedder ai.Embeddings,
    batchSize int,
    categoryIDs []sop.UUID,  // ← SPECIFIC CATEGORIES ONLY
) error
```

**What it does**:
- Vectorizes **ONLY specified categories** (by UUID)
- Recursively vectorizes parent categories if needed
- Processes all items within specified categories
- Updates `CategoriesByDistance` index

**Key features**:
- **Targeted**: Only processes specified categories
- **Recursive parent handling**: Ensures parent categories are vectorized
- **Efficient**: Skips unrelated categories
- **Transactional**: Single transaction, atomic operation

**Use cases**:
- ✅ **Team Patterns** - vectorize "Team Patterns" category after user adds patterns
- Category-specific updates
- Selective re-vectorization

**Performance**:
- O(P + C + I) where P = parent chain, C = target categories, I = items in those categories
- For "Team Patterns" with 20 patterns: **Fast** (seconds)
- **PERFECT for Team Patterns**

---

### 3. `VectorizeItems()` - Granular Item Vectorization 🔧 **FINE-GRAINED**
```go
func (db *Database) VectorizeItems(
    ctx context.Context,
    kbName string,
    llm ai.Generator,
    embedder ai.Embeddings,
    batchSize int,
    categoryID sop.UUID,
    itemIDs []sop.UUID,  // ← SPECIFIC ITEMS ONLY
) error
```

**What it does**:
- Vectorizes **specific items** within a category
- If `itemIDs` is empty: falls back to `VectorizeCategories()` for the whole category
- If category vector changed: falls back to `VectorizeCategories()` (spatial consistency)
- Real-time, granular updates

**Use cases**:
- Real-time item updates in web handlers
- Single pattern updates (user edits one pattern)
- Incremental vectorization hooks

**Performance**:
- O(N) where N = number of items specified
- For 1 pattern: **Very fast** (milliseconds)
- **Could be used for single-pattern updates**

---

## Recommendation for Team Patterns

### ✅ Use `VectorizeCategories()` for Batch Vectorization

**Scenario**: User finishes adding multiple patterns and wants to vectorize them all.

**Why**:
1. **Efficient**: Only vectorizes "Team Patterns" subtree (not entire LTM)
2. **Complete**: Handles all patterns in category at once
3. **Recursive**: Automatically handles subcategories (Authentication, Error Handling, etc.)
4. **Atomic**: Single transaction, all-or-nothing

**Implementation**:
```go
// In teampatterns package
func VectorizeTeamPatterns(ctx context.Context, db *database.Database, ltmName string, llm ai.Generator, embedder ai.Embeddings) error {
    // 1. Get "Team Patterns" category ID
    categoryID, err := getTeamPatternsCategoryID(ctx, db, ltmName)
    if err != nil {
        return err
    }
    
    // 2. Vectorize the category and all its items
    return db.VectorizeCategories(ctx, ltmName, llm, embedder, 50, []sop.UUID{categoryID})
}

// Helper to find "Team Patterns" category ID
func getTeamPatternsCategoryID(ctx context.Context, db *database.Database, ltmName string) (sop.UUID, error) {
    tx, err := db.BeginTransaction(ctx, sop.ForReading)
    if err != nil {
        return sop.NilUUID, err
    }
    defer tx.Rollback(ctx)
    
    kb, err := db.OpenKnowledgeBase(ctx, ltmName, tx, nil, nil, false, true)
    if err != nil {
        return sop.NilUUID, err
    }
    
    catsByPath, err := kb.Store.CategoriesByPath(ctx)
    if err != nil {
        return sop.NilUUID, err
    }
    
    found, err := catsByPath.Find(ctx, "Team Patterns", false)
    if err != nil || !found {
        return sop.NilUUID, fmt.Errorf("Team Patterns category not found")
    }
    
    return catsByPath.GetCurrentValue(ctx)
}
```

---

## Tool Integration: Expose as LLM Tool

### Add Tool to Investigation Phase

**Tool Name**: `investigation_vectorize_team_patterns`

**When to expose**:
- After user stores patterns via separate tool (future: `investigation_store_team_pattern`)
- When LLM detects "I'm done adding patterns" signal

**Tool Definition**:
```go
{
    Name: "investigation_vectorize_team_patterns",
    Description: "Vectorizes all team patterns for semantic search. Call this AFTER adding/updating multiple patterns to enable fast semantic discovery. This is a one-time operation after pattern setup. Returns confirmation when vectorization completes.",
    Schema: `{
  "type": "object",
  "properties": {},
  "description": "No arguments required. Vectorizes the entire Team Patterns category."
}`,
}
```

**Tool Handler**:
```go
func (c *PhaseCapability) runVectorizeTeamPatterns(ctx context.Context, args map[string]any) (string, error) {
    // Get database and LTM name
    if c.sharedInfra == nil || c.sharedInfra.LTMAccessor == nil {
        return "Cannot vectorize: LTM not configured", nil
    }
    
    ltmInterface, err := c.sharedInfra.LTMAccessor(ctx)
    if err != nil {
        return "Cannot vectorize: LTM access failed", nil
    }
    
    ltm, ok := ltmInterface.(*memory.KnowledgeBase[map[string]any])
    if !ok {
        return "Cannot vectorize: LTM type mismatch", nil
    }
    
    // Get database from agent context (needs to be passed via SharedInfrastructure)
    // Get embedder from domain
    
    err = teampatterns.VectorizeTeamPatterns(ctx, db, ltmName, llm, embedder)
    if err != nil {
        return "", fmt.Errorf("vectorization failed: %w", err)
    }
    
    return "✅ Team Patterns vectorized successfully. All patterns are now semantically searchable.", nil
}
```

---

## User Workflow

### Scenario 1: Bulk Pattern Setup
```
User: "Add these 5 authentication patterns to the team conventions"

LLM: [Stores patterns using StoreTeamPattern() for each]
     
User: "Done adding patterns"

LLM: [Calls investigation_vectorize_team_patterns]
     "✅ All 5 patterns have been vectorized and are now semantically searchable."
```

### Scenario 2: Incremental Pattern Addition
```
User: "Add JWT validation pattern"

LLM: [Stores pattern]
     [Auto-detects this is a single pattern]
     [Could use VectorizeItems() for immediate availability]
     "✅ Pattern added. For best performance, vectorize all patterns together after adding multiple patterns."
```

### Scenario 3: Explicit Vectorization Request
```
User: "Vectorize my team patterns"

LLM: [Calls investigation_vectorize_team_patterns]
     "✅ Vectorized 15 team patterns in 3 categories (Authentication, Error Handling, Database). Patterns are now semantically searchable."
```

---

## Implementation Checklist

### Phase 1: Core Vectorization Function
- [ ] Add `VectorizeTeamPatterns()` to teampatterns package
- [ ] Add `getTeamPatternsCategoryID()` helper
- [ ] Handle "Team Patterns" category not found gracefully
- [ ] Handle subcategories (Authentication, Error Handling, etc.)
- [ ] Add unit tests

### Phase 2: Tool Registration
- [ ] Add `investigation_vectorize_team_patterns` tool to investigation phase
- [ ] Add tool handler `runVectorizeTeamPatterns()`
- [ ] Wire database accessor to SharedInfrastructure
- [ ] Wire embedder accessor to SharedInfrastructure
- [ ] Add LLM accessor for VectorizeCategories

### Phase 3: System Prompt Guidance
- [ ] Update system prompt to mention vectorization
- [ ] Guide LLM when to call vectorize (after bulk additions)
- [ ] Clarify vectorization is optional but recommended

---

## Performance Analysis

### For "Team Patterns" with 20 patterns across 3 subcategories:

**VectorizeCategories()**:
```
Category vectorization:
  - "Team Patterns" root: 1 embedding
  - "Authentication" subcategory: 1 embedding
  - "Error Handling" subcategory: 1 embedding
  - "Database" subcategory: 1 embedding
  = 4 category embeddings (~200ms)

Item vectorization:
  - 20 patterns × 1 embedding each = 20 embeddings
  - Batch size 50, so single batch
  = 20 item embeddings (~1 second)

Distance index updates:
  - 4 categories in CategoriesByDistance
  - 20 items in Vectors B-tree
  = ~100ms

Total: ~1.3 seconds ✅
```

**Vectorize()** (full space):
```
ALL categories in LTM:
  - "General" category tree: 50+ embeddings
  - "Repository Insights" tree: 30+ embeddings
  - "Coding Patterns" tree: 40+ embeddings
  - "Team Patterns" tree: 4 embeddings
  = 124+ category embeddings (~15 seconds)

ALL items in LTM:
  - 200+ items across all categories
  = 200+ item embeddings (~30 seconds)

Total: ~45 seconds ❌ (35x slower!)
```

---

## Alternative Approach: Auto-Vectorize on Store

### Option: Vectorize per-pattern as stored

**Pros**:
- No explicit vectorize step needed
- Patterns immediately searchable
- Simpler UX

**Cons**:
- Many small transactions (inefficient)
- 20 patterns = 20 separate vectorization calls
- No batching optimization
- Higher latency per pattern store

**Recommendation**: Keep explicit vectorization
- Better batching efficiency
- User control over when to vectorize
- Clear separation of concerns (store vs index)

---

## Summary

| Function | Use Case | Team Patterns? | Performance |
|----------|----------|----------------|-------------|
| `Vectorize()` | Full space vectorization | ❌ Too expensive | ~45s for typical LTM |
| `VectorizeCategories()` | Targeted category vectorization | ✅ **RECOMMENDED** | ~1.3s for 20 patterns |
| `VectorizeItems()` | Single item updates | ⚠️ For incremental updates | ~50ms per pattern |

**Final Recommendation**: 
- Use `VectorizeCategories()` with "Team Patterns" category ID
- Expose as `investigation_vectorize_team_patterns` tool
- LLM calls after user finishes adding patterns
- Single transaction, ~1-2 seconds, handles all patterns efficiently

**Next Steps**:
1. Implement `VectorizeTeamPatterns()` in teampatterns package
2. Add database/embedder accessors to SharedInfrastructure
3. Register vectorization tool in investigation phase
4. Update system prompt with guidance on when to vectorize
