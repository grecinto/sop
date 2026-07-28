# Workflow Undo System Architecture

This document covers the technical architecture of AI Studio's workflow undo system, including transaction management, multi-dimensional indexing, semantic search integration, and rollback mechanisms.

## Overview

The Workflow Undo System treats each coding workflow run as an **atomic transaction** that can be intelligently searched, queried, and restored. This provides semantic, domain-aware undo that understands your codebase's application architecture.

### Key Differentiators

**vs. Git Undo:**
- No git history pollution from experimental workflows
- Workflow-scoped transactions (not commit-level)
- Semantic search: "undo auth middleware changes" works naturally
- Separates experimentation from semantic versioning

**vs. IDE Local History:**
- Application-domain aware (understands components like authentication, database, parser)
- Multi-day history with intelligent querying
- Workflow context preserved (why changes were made, not just what)
- Natural language queries powered by vector search

**vs. Enterprise Workflow Systems (Temporal, Step Functions):**
- No complex compensation logic required
- Simple snapshot-based approach (reliable, fast)
- Developer-friendly UX (no YAML/JSON configuration)
- Built-in semantic intelligence

## Architecture

### Three-Layer Storage Design

The undo system uses a three-layer architecture for optimal performance and reliability:

```
Layer 1: Knowledge Base Space (Semantic Index)
├── Multi-dimensional categories
│   ├── Date hierarchy (exact, fast lookup)
│   ├── Code artifacts (semantic, vectorized)
│   └── Workflow types (tagged, searchable)
├── Rich searchable content (descriptions + code context)
└── Time-ranked results (recency + relevance scoring)

Layer 2: Filesystem (Backup Storage)
├── .sop/undo/transactions/<txnID>/
│   ├── backups/ (full file copies)
│   ├── metadata.json (transaction details)
│   └── checksums.json (SHA256 hashes)
└── Fast, reliable restore operations

Layer 3: Index (Fast Lookups)
└── .sop/undo/index.json
    ├── Date-based index for temporal queries
    ├── Transaction ID to path mapping
    └── Quick access without full KB scan
```

### Multi-Dimensional Indexing

Each workflow transaction is indexed along **three dimensions simultaneously**:

#### 1. Date Dimension (Non-Semantic, Exact)

```
Category Path: 2026/07/27 → Transaction Item
```

- Fast temporal filtering via B-tree path lookup
- No vectorization overhead
- O(log N) exact date queries
- Stored as path-only references (no vectors in `CategoriesByDistance`)

#### 2. Code Artifacts Dimension (Semantic, Vectorized)

```
Category Path: Authentication System/Middleware → Transaction Item
               Database Layer/Query Builder → Transaction Item
               Parser Engine/Tokenizer → Transaction Item
```

- Extracted via AST analysis using tree-sitter
- Hierarchical categories: Component → Sub-component
- Fully vectorized for semantic similarity search
- Enables queries like "authentication middleware changes"

#### 3. Workflow Metadata Dimension (Hybrid)

```
Category Path: workflow_type/debugging → Transaction Item
               workflow_type/refactoring → Transaction Item
```

- Type classifications: debugging, testing, refactoring, implementation
- Tags: domain-specific labels
- Description: Natural language (vectorized)
- Combined with code artifacts for rich search

### Multi-Path Category Storage

**Key Innovation**: Each transaction exists in **multiple category paths simultaneously** without data duplication:

```go
// Same item, multiple access paths
Path 1 (Date):     "2026-07-27" → txn_item
Path 2 (Code):     "Authentication System/Middleware" → txn_item  
Path 3 (Type):     "workflow_type/security_enhancement" → txn_item

// All paths reference the same ItemID in the KB
```

**Implementation Detail**: Date paths are stored as path-only references (no vectors), while code artifact and workflow type paths are fully vectorized. Categories without vectors have no entry in the `CategoriesByDistance` B-Tree, automatically excluding them from semantic search triangulation. This keeps date filtering exact and fast while enabling rich semantic queries.

**Benefits:**
- No data duplication (same item, multiple indices)
- Fast date queries (direct category path lookup)
- Rich semantic search (vector similarity across code artifacts)
- Flexible ranking (combine temporal + semantic relevance)
- System-controlled vectorization (zero user error)

## Transaction Model

### Transaction Lifecycle

```go
// 1. Begin Transaction
func BeginTransaction(workingDir, description string) (TransactionID, error) {
    txnID := generateTransactionID()
    backupDir := fmt.Sprintf(".sop/undo/transactions/%s/backups", txnID)
    os.MkdirAll(backupDir, 0755)
    
    metadata := TransactionMetadata{
        ID:          txnID,
        Timestamp:   time.Now(),
        WorkingDir:  workingDir,
        Description: description,
        Status:      "in_progress",
        Files:       []FileModification{},
    }
    
    saveMetadata(txnID, metadata)
    return txnID, nil
}

// 2. Record File Modifications
func BackupAndModify(txnID, filePath string, patch Patch) error {
    // Backup original file
    originalContent, err := os.ReadFile(filePath)
    if err != nil && !os.IsNotExist(err) {
        return err
    }
    
    backupPath := fmt.Sprintf(".sop/undo/transactions/%s/backups/%s", 
        txnID, sanitizeFilePath(filePath))
    os.MkdirAll(filepath.Dir(backupPath), 0755)
    os.WriteFile(backupPath, originalContent, 0644)
    
    // Calculate checksum
    checksum := sha256.Sum256(originalContent)
    saveChecksum(txnID, filePath, checksum)
    
    // Apply patch
    if err := patch.Apply(filePath); err != nil {
        return err
    }
    
    // Record modification
    recordFileModification(txnID, filePath, patch.Stats())
    return nil
}

// 3. Commit Transaction
func CommitTransaction(txnID string, success bool) error {
    metadata := loadMetadata(txnID)
    
    if success {
        metadata.Status = "completed"
        
        // Extract code artifacts via AST
        artifacts, err := extractCodeArtifacts(metadata.Files)
        if err != nil {
            log.Warn("Failed to extract code artifacts", "error", err)
        }
        metadata.CodeArtifacts = artifacts
        
        // Store in KB with multiple category paths
        if err := storeInKnowledgeBase(metadata); err != nil {
            return err
        }
        
        // Update index
        updateIndex(txnID, metadata)
    } else {
        metadata.Status = "rolled_back"
        
        // Auto-rollback: restore all files
        if err := rollbackTransaction(txnID); err != nil {
            return err
        }
    }
    
    saveMetadata(txnID, metadata)
    return nil
}
```

### Transaction Metadata Structure

```go
type TransactionMetadata struct {
    ID              string    `json:"id"`
    Timestamp       time.Time `json:"timestamp"`
    WorkflowType    string    `json:"workflow_type"`    // debugging, testing, refactoring, etc.
    Description     string    `json:"description"`
    Status          string    `json:"status"`           // in_progress, completed, rolled_back
    
    // Triggering information
    TriggeredBy     string    `json:"triggered_by"`     // user, automated
    UserName        string    `json:"user_name"`
    
    // Code artifacts extracted via AST
    CodeArtifacts   []CodeArtifact `json:"code_artifacts"`
    
    // File changes
    Files           []FileModification `json:"files"`
    
    // Categorization
    Tags            []string          `json:"tags"`
    Component       string            `json:"component"`        // e.g., "Authentication System"
    SubComponent    string            `json:"sub_component"`    // e.g., "Middleware"
}

type CodeArtifact struct {
    Namespace  string   `json:"namespace"`   // e.g., "github.com/myapp/parser"
    Package    string   `json:"package"`     // e.g., "tokenizer"
    File       string   `json:"file"`        // relative path
    Functions  []string `json:"functions"`   // function names
    Types      []string `json:"types"`       // struct/class/interface names
}

type FileModification struct {
    Path          string `json:"path"`
    BackupPath    string `json:"backup_path"`
    Checksum      string `json:"checksum"`      // SHA256 of original
    LinesAdded    int    `json:"lines_added"`
    LinesRemoved  int    `json:"lines_removed"`
}
```

## Code Artifact Extraction

### AST-Based Analysis

The system uses tree-sitter parsers to extract code artifacts for semantic categorization:

```go
func extractCodeArtifacts(files []FileModification) ([]CodeArtifact, error) {
    artifacts := []CodeArtifact{}
    
    for _, file := range files {
        lang := detectLanguage(file.Path)
        parser := getParser(lang)
        
        sourceCode, err := os.ReadFile(file.Path)
        if err != nil {
            continue
        }
        
        tree, err := parser.Parse(sourceCode)
        if err != nil {
            continue
        }
        
        artifact := CodeArtifact{
            File:      file.Path,
            Package:   extractPackageName(tree, lang),
            Functions: extractFunctions(tree, lang),
            Types:     extractTypes(tree, lang),
        }
        
        artifacts = append(artifacts, artifact)
    }
    
    return artifacts, nil
}
```

### Multi-Language Support

| Language | Detection | Functions | Types | Packages |
|----------|-----------|-----------|-------|----------|
| **Go** | `go.mod` | `function_declaration`, `method_declaration` | `struct`, `interface`, `type` | `package` |
| **Python** | `*.py` | `function_definition`, methods in classes | `class` | module inference |
| **Java** | `*.java` | `method_declaration`, `constructor` | `class`, `interface`, `enum` | `package` |
| **Rust** | `Cargo.toml` | `function_item`, methods in impl | `struct`, `enum`, `trait` | `mod`, `crate` |
| **JavaScript/TypeScript** | `*.js`, `*.ts` | `function_declaration`, `method_definition`, arrows | `class`, `interface`, `type` | directory-based |
| **C#** | `*.cs` | `method_declaration`, `constructor` | `class`, `interface`, `struct` | `namespace` |

### Component Categorization

Components are inferred from package structure and file paths:

```go
func categorizeComponent(artifact CodeArtifact) (component, subComponent string) {
    // Examples:
    // "auth/middleware.go" → ("Authentication System", "Middleware")
    // "db/query_builder.go" → ("Database Layer", "Query Builder")
    // "parser/tokenizer.go" → ("Parser Engine", "Tokenizer")
    
    parts := strings.Split(artifact.File, "/")
    if len(parts) >= 2 {
        component = humanize(parts[0])
        subComponent = humanize(parts[1])
    }
    
    return component, subComponent
}
```

## Semantic Search with Time Ranking

### Ranking Strategies

```go
type RankingStrategy string

const (
    RankingHybrid         RankingStrategy = "hybrid"           // Default
    RankingTimeDescending RankingStrategy = "time_desc"
    RankingTimeAscending  RankingStrategy = "time_asc"
    RankingPureRelevance  RankingStrategy = "relevance"
)

type SearchOptions struct {
    Query           string
    Strategy        RankingStrategy
    RecencyWeight   float64  // Default: 0.3
    RelevanceWeight float64  // Default: 0.7
    DateAfter       *time.Time
    DateBefore      *time.Time
    Component       string
    Limit           int
}
```

### Hybrid Scoring Algorithm

```go
func computeHybridScore(txn TransactionMetadata, query string, opts SearchOptions) float64 {
    // Semantic relevance from vector search
    relevance := vectorSimilarity(txn.Description, query)
    
    // Time decay function (exponential)
    daysSince := time.Since(txn.Timestamp).Hours() / 24
    recency := math.Exp(-0.1 * daysSince)
    
    // Weighted combination
    score := (recency * opts.RecencyWeight) + (relevance * opts.RelevanceWeight)
    
    return score
}

// Examples:
// Today (0 days):    recency = 1.0    → strong boost
// 1 week (7 days):   recency = 0.5    → moderate boost
// 1 month (30 days): recency = 0.05   → minimal boost
```

### Search Implementation

```go
func SearchTransactions(kb *KnowledgeBase, opts SearchOptions) ([]TransactionResult, error) {
    var results []TransactionResult
    
    switch opts.Strategy {
    case RankingHybrid:
        // Semantic search in code artifacts
        items, err := kb.SemanticSearch(opts.Query, &memory.SearchOptions{
            MaxResults: opts.Limit * 2,  // Over-fetch for re-ranking
        })
        if err != nil {
            return nil, err
        }
        
        // Re-rank with time decay
        for _, item := range items {
            txn := item.Data.(TransactionMetadata)
            score := computeHybridScore(txn, opts.Query, opts)
            results = append(results, TransactionResult{
                Transaction: txn,
                Score:       score,
            })
        }
        
        sort.Slice(results, func(i, j int) bool {
            return results[i].Score > results[j].Score
        })
        
    case RankingTimeDescending:
        // Fast date-based lookup (no semantic search)
        results, err = listByDate(kb, opts.DateAfter, opts.DateBefore, true)
        
    case RankingTimeAscending:
        results, err = listByDate(kb, opts.DateAfter, opts.DateBefore, false)
        
    case RankingPureRelevance:
        // Pure vector similarity (no time boost)
        items, err := kb.SemanticSearch(opts.Query, &memory.SearchOptions{
            MaxResults: opts.Limit,
        })
        for _, item := range items {
            results = append(results, TransactionResult{
                Transaction: item.Data.(TransactionMetadata),
                Score:       item.Score,
            })
        }
    }
    
    // Apply date filters if specified
    if opts.DateAfter != nil || opts.DateBefore != nil {
        results = filterByDateRange(results, opts.DateAfter, opts.DateBefore)
    }
    
    // Apply component filter
    if opts.Component != "" {
        results = filterByComponent(results, opts.Component)
    }
    
    return results[:min(len(results), opts.Limit)], nil
}
```

## Rollback Operations

### Full Transaction Rollback

```go
func RollbackTransaction(txnID string) error {
    metadata := loadMetadata(txnID)
    
    if metadata.Status == "rolled_back" {
        return fmt.Errorf("transaction already rolled back")
    }
    
    // Restore each modified file
    for _, file := range metadata.Files {
        backupPath := file.BackupPath
        
        // Verify backup integrity
        backupContent, err := os.ReadFile(backupPath)
        if err != nil {
            return fmt.Errorf("backup missing for %s: %w", file.Path, err)
        }
        
        checksum := sha256.Sum256(backupContent)
        if hex.EncodeToString(checksum[:]) != file.Checksum {
            return fmt.Errorf("backup corrupted for %s", file.Path)
        }
        
        // Restore file
        if err := os.WriteFile(file.Path, backupContent, 0644); err != nil {
            return fmt.Errorf("failed to restore %s: %w", file.Path, err)
        }
    }
    
    // Update status
    metadata.Status = "rolled_back"
    metadata.RolledBackAt = time.Now()
    saveMetadata(txnID, metadata)
    
    return nil
}
```

### Selective File Restore

```go
func RestoreFiles(txnID string, filePaths []string) error {
    metadata := loadMetadata(txnID)
    
    for _, targetPath := range filePaths {
        // Find file in transaction
        var filemod *FileModification
        for _, f := range metadata.Files {
            if f.Path == targetPath {
                filemod = &f
                break
            }
        }
        
        if filemod == nil {
            return fmt.Errorf("file %s not part of transaction", targetPath)
        }
        
        // Restore from backup
        backupContent, err := os.ReadFile(filemod.BackupPath)
        if err != nil {
            return err
        }
        
        if err := os.WriteFile(targetPath, backupContent, 0644); err != nil {
            return err
        }
    }
    
    return nil
}
```

### Preview Before Restore

```go
func PreviewRestore(txnID string) (RestorePreview, error) {
    metadata := loadMetadata(txnID)
    
    preview := RestorePreview{
        TransactionID: txnID,
        Description:   metadata.Description,
        Files:         []FilePreview{},
    }
    
    for _, file := range metadata.Files {
        preview.Files = append(preview.Files, FilePreview{
            Path:         file.Path,
            LinesAdded:   file.LinesRemoved,  // Reversed
            LinesRemoved: file.LinesAdded,    // Reversed
        })
    }
    
    return preview, nil
}

type RestorePreview struct {
    TransactionID string
    Description   string
    Timestamp     time.Time
    Files         []FilePreview
}

type FilePreview struct {
    Path         string
    LinesAdded   int  // Will be added when restoring
    LinesRemoved int  // Will be removed when restoring
}
```

## Integration with Workflows

### Automatic Transaction Wrapping

Workflows automatically wrap their execution in undo transactions:

```go
func (wm *WorkflowManager) Execute(ctx context.Context, workflow Workflow) error {
    // Begin transaction
    txnID, err := BeginTransaction(workflow.WorkingDir, workflow.Description)
    if err != nil {
        return err
    }
    
    // Execute workflow with transaction context
    ctx = context.WithValue(ctx, "transaction_id", txnID)
    
    success := false
    defer func() {
        // Commit or rollback based on success
        CommitTransaction(txnID, success)
    }()
    
    // Execute workflow steps
    for _, step := range workflow.Steps {
        if err := step.Execute(ctx); err != nil {
            log.Error("Step failed", "step", step.Title, "error", err)
            return err  // Triggers rollback in defer
        }
    }
    
    success = true
    return nil
}
```

### File Operations Integration

All file modifications go through the transaction system:

```go
func ApplyPatch(ctx context.Context, filePath string, patch Patch) error {
    txnID, ok := ctx.Value("transaction_id").(string)
    if !ok {
        // No transaction active - apply without backup
        return patch.Apply(filePath)
    }
    
    // Transaction active - backup and record
    return BackupAndModify(txnID, filePath, patch)
}
```

## Storage and Performance

### Storage Requirements

Per transaction:
- Metadata: ~2-5 KB (JSON)
- File backups: Actual file sizes
- Checksums: 32 bytes per file
- KB index entries: ~1 KB per transaction

**Example**: A workflow modifying 3 files (average 10 KB each):
- Total: ~35-40 KB per transaction
- 100 transactions: ~3.5-4 MB
- 1000 transactions: ~35-40 MB

### Retention and Cleanup

```go
type RetentionPolicy struct {
    MaxAge           time.Duration  // Default: 30 days
    MaxTransactions  int            // Default: 1000
    KeepMilestones   bool           // Default: true
}

func CleanupOldTransactions(policy RetentionPolicy) error {
    cutoffDate := time.Now().Add(-policy.MaxAge)
    
    transactions := listAllTransactions()
    
    // Sort by date
    sort.Slice(transactions, func(i, j int) bool {
        return transactions[i].Timestamp.Before(transactions[j].Timestamp)
    })
    
    // Keep recent or milestone transactions
    for _, txn := range transactions {
        if txn.Timestamp.Before(cutoffDate) && !isMilestone(txn) {
            deleteTransaction(txn.ID)
        }
    }
    
    return nil
}

func isMilestone(txn TransactionMetadata) bool {
    // Keep transactions with milestone tags
    for _, tag := range txn.Tags {
        if tag == "milestone" || tag == "production_release" {
            return true
        }
    }
    return false
}
```

### Performance Optimizations

1. **Index Caching**: Keep frequently accessed transactions in memory
2. **Lazy Loading**: Load metadata only when needed
3. **Batch Operations**: Group multiple file operations in single transaction
4. **Compressed Backups**: Compress older transaction backups
5. **Date Index**: Fast date-based lookups without KB scan

```go
type TransactionCache struct {
    cache map[string]*TransactionMetadata
    mu    sync.RWMutex
    ttl   time.Duration
}

func (c *TransactionCache) Get(txnID string) (*TransactionMetadata, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    if txn, ok := c.cache[txnID]; ok {
        return txn, true
    }
    return nil, false
}

func (c *TransactionCache) Set(txnID string, metadata *TransactionMetadata) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.cache[txnID] = metadata
    
    // Auto-evict after TTL
    time.AfterFunc(c.ttl, func() {
        c.mu.Lock()
        delete(c.cache, txnID)
        c.mu.Unlock()
    })
}
```

## CLI Integration

### Command Structure

```bash
# Search transactions
sop workflow find <query> [options]
  --after <date>           # Filter by date range
  --before <date>
  --component <name>       # Filter by component
  --rank-by <strategy>     # Ranking strategy
  --recency-weight <float> # Time weight (0-1)
  --relevance-weight <float> # Relevance weight (0-1)
  --limit <int>           # Max results

# List transactions
sop workflow list [options]
  --date <date>           # Specific date
  --component <name>      # Browse by component

# Restore operations
sop workflow restore <txn-id> [options]
  --dry-run              # Preview without applying
  --files-only <paths>   # Restore specific files

# Transaction details
sop workflow show <txn-id>

# Cleanup
sop workflow cleanup [options]
  --older-than <duration>
  --keep-milestones
```

### Example CLI Output

```bash
$ sop workflow find "authentication middleware"

Found 3 matching workflows (ranked by recency + relevance):

1. [Jul 27, 14:30] ⭐ Add JWT validation                    (score: 0.95)
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   Component: Authentication System → Middleware
   Type: security_enhancement
   Files: auth/middleware.go (+45 -12), api/handler.go (+8 -3)
   
   Changes:
   • Added JWT token validation
   • Implemented refresh token flow
   • Added rate limiting middleware
   
   Restore: sop workflow restore txn_20260727_143022

2. [Jul 26, 10:15] ⭐ Fix session timeout bug               (score: 0.88)
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   Component: Authentication System → Middleware
   Type: debugging
   Files: auth/middleware.go (+8 -3)
   
   Changes:
   • Fixed session timeout calculation
   • Added timeout configuration
   
   Restore: sop workflow restore txn_20260726_101522

Enter transaction number to restore, or 'q' to quit: _
```

## Testing

### Unit Tests

```go
func TestTransactionLifecycle(t *testing.T) {
    // Begin transaction
    txnID, err := BeginTransaction("/test/workspace", "Test workflow")
    assert.NoError(t, err)
    
    // Modify files
    err = BackupAndModify(txnID, "test.go", mockPatch())
    assert.NoError(t, err)
    
    // Commit
    err = CommitTransaction(txnID, true)
    assert.NoError(t, err)
    
    // Verify stored in KB
    metadata := loadMetadata(txnID)
    assert.Equal(t, "completed", metadata.Status)
}

func TestRollback(t *testing.T) {
    // Create file
    os.WriteFile("test.go", []byte("original"), 0644)
    
    // Begin transaction and modify
    txnID, _ := BeginTransaction("/test", "Test")
    BackupAndModify(txnID, "test.go", mockPatch())
    
    // Rollback
    err := RollbackTransaction(txnID)
    assert.NoError(t, err)
    
    // Verify file restored
    content, _ := os.ReadFile("test.go")
    assert.Equal(t, "original", string(content))
}
```

### Integration Tests

```go
func TestSemanticSearch(t *testing.T) {
    kb := setupTestKB()
    
    // Create test transactions
    createTestTransaction(kb, "Add JWT validation", "Authentication System/Middleware")
    createTestTransaction(kb, "Fix parser bug", "Parser Engine/Tokenizer")
    
    // Search
    results, err := SearchTransactions(kb, SearchOptions{
        Query:    "authentication",
        Strategy: RankingHybrid,
        Limit:    10,
    })
    
    assert.NoError(t, err)
    assert.Greater(t, len(results), 0)
    assert.Contains(t, results[0].Transaction.Description, "JWT")
}
```

## Best Practices for SDK Integration

### When to Create Transactions

✅ **Do create transactions for:**
- Workflow executions that modify files
- Experimental code changes
- Refactoring operations
- Bug fixes with multiple file changes
- Code generation workflows

❌ **Don't create transactions for:**
- Read-only operations
- Single-file trivial edits (< 5 lines)
- Configuration file updates
- Log file modifications

### Transaction Granularity

**One transaction per logical unit of work:**
```go
// ✅ Good: One transaction for entire refactoring
txnID := BeginTransaction(dir, "Refactor authentication module")
refactorAuthMiddleware(txnID)
refactorAuthHandlers(txnID)
refactorAuthTests(txnID)
CommitTransaction(txnID, true)

// ❌ Bad: Separate transactions for each file
txnID1 := BeginTransaction(dir, "Refactor middleware")
refactorAuthMiddleware(txnID1)
CommitTransaction(txnID1, true)

txnID2 := BeginTransaction(dir, "Refactor handlers")
refactorAuthHandlers(txnID2)
CommitTransaction(txnID2, true)
```

### Error Handling

Always use defer to ensure commit/rollback:
```go
func executeWorkflow(workflow Workflow) error {
    txnID, err := BeginTransaction(workflow.Dir, workflow.Description)
    if err != nil {
        return err
    }
    
    success := false
    defer func() {
        if err := CommitTransaction(txnID, success); err != nil {
            log.Error("Transaction commit failed", "error", err)
        }
    }()
    
    // Execute workflow
    if err := workflow.Execute(); err != nil {
        return err  // Triggers rollback via defer
    }
    
    success = true
    return nil
}
```

### Component Naming Conventions

Use consistent, hierarchical component names:
```
Authentication System
├── Middleware
├── User Service
└── Token Manager

Database Layer
├── Connection Pool
├── Query Builder
└── Migration System

Parser Engine
├── Tokenizer
├── AST Builder
└── Code Generator
```

This enables effective component-based browsing and filtering.

## Summary

The Workflow Undo System provides:

1. **Atomic transactions** - Workflow runs as rollback-able units
2. **Multi-dimensional indexing** - Date, code artifacts, workflow types
3. **Semantic search** - Natural language queries with time ranking
4. **AST-based categorization** - Automatic component extraction
5. **Fast rollback** - File-based snapshots with integrity checks
6. **Rich context** - Know why changes were made, not just what
7. **Multi-language support** - Works across Go, Python, Java, Rust, JS/TS, C#

The system integrates seamlessly with workflows, providing confidence to experiment while maintaining a complete, searchable history of code evolution.
