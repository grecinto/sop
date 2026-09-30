# Team Patterns LTM Category Design

**Status**: Design Approved  
**Created**: 2026-07-30  
**Context**: Addresses gap in code generation - LLM knows generic best practices from training data, but doesn't know team-specific conventions.

## Problem Statement

**Current Gap**: LLMs are pre-trained on canonical texts (Clean Code, High Performance MySQL, Gang of Four, etc.) and automatically apply generic best practices. However, they don't know:

❌ **Team-specific patterns**: "We use `lib/auth/jwt.go:ValidateRequestToken()` not raw `jwt.Parse()`"  
❌ **Project conventions**: "All database queries go through `db/query_builder.go` helpers"  
❌ **Team decisions**: "Use Temporal for background jobs, NOT custom workers"  
❌ **Local helpers**: "Import validation uses `lib/validators/CustomValidator`"

**Why Generic KB Failed**: DAEMON_USER_KB.md contained ~1400 lines of generic best practices that duplicate LLM training:
- "Use gofmt" → Already in Claude's training
- "Avoid N+1 queries" → Already in Claude's training
- "Handle errors explicitly in Go" → Already in Claude's training

**Key Insight**: Retrieval ≠ Following. KB lookup doesn't make LLMs follow practices without explicit instruction or enforcement.

**The Solution**: Store TEAM-SPECIFIC patterns in LTM as "Team Patterns" category, make them semantically searchable during code generation.

---

## Architecture: Team Patterns as LTM Category

### Top-Level Category Structure

```
LTM Categories (per user):
├── Coding (existing - repo-specific memories)
│   ├── Workflow/Repo/{repo}/File/{file}
│   ├── Semantic/{topic}
│   └── Go/Package/.../Type/.../Method/...
│
├── Team Patterns (NEW - team-specific conventions)
│   ├── Authentication
│   │   ├── JWT Validation
│   │   ├── Session Management
│   │   └── OAuth Integration
│   ├── Error Handling
│   │   ├── Error Wrapper Pattern
│   │   ├── Logging Standards
│   │   └── Error Response Format
│   ├── Database Patterns
│   │   ├── Query Builder Usage
│   │   ├── Transaction Boundaries
│   │   └── Migration Standards
│   └── Testing Utilities
│       ├── Test Fixtures
│       ├── Mock Patterns
│       └── Integration Test Setup
│
├── Research (planned)
├── Planning (planned)
└── General (existing)
```

### Why LTM Over Spaces?

| Feature | Spaces | LTM Team Patterns |
|---------|--------|-------------------|
| **Purpose** | Product documentation Q&A | Code generation constraints |
| **Example** | "How do Spaces work?" | "Use auth.ValidateRequestToken()" |
| **Retrieval** | Explicit user query | Automatic during code generation |
| **Scope** | Global product knowledge | Per-user/team conventions |
| **Persistence** | Session-independent | Always available per user |
| **Context Injection** | Manual lookup | Automatic semantic search |

**Decision**: Use LTM because Team Patterns are **always-available user memory**, not product documentation.

---

## Pattern Item Schema

### Metadata Fields

```go
type TeamPattern struct {
    // Standard LTM fields
    DocumentID      string    `json:"document_id"`
    CategoryPath    string    `json:"category_path"`      // "Team Patterns/Authentication/JWT Validation"
    
    // Pattern identification
    PatternName     string    `json:"pattern_name"`       // "JWT Validation for HTTP Handlers"
    PatternType     string    `json:"pattern_type"`       // "authentication" | "error_handling" | "database" | "testing"
    
    // Pattern content
    Convention      string    `json:"convention"`         // "Use auth.ValidateRequestToken(r)"
    AntiPattern     string    `json:"anti_pattern"`       // "DO NOT use jwt.Parse() directly"
    Reasoning       string    `json:"reasoning"`          // "Our helper includes tenant validation + audit logging"
    
    // Code examples
    Example         string    `json:"example"`            // Full working code example
    References      []string  `json:"references"`         // ["lib/auth/jwt.go:45", "examples/api/handlers.go:23"]
    
    // Context
    WhenToUse       string    `json:"when_to_use"`        // "All authenticated HTTP endpoints"
    RelatedPatterns []string  `json:"related_patterns"`   // ["Error Response Format", "Auth Middleware"]
    
    // Scope metadata
    ScopeType       string    `json:"scope_type"`         // "team" | "project" | "repo"
    ScopeID         string    `json:"scope_id"`           // Team/project/repo identifier
    
    // Lifecycle
    Status          string    `json:"lifecycle_status"`   // "active" | "deprecated" | "archived"
    CreatedAt       int64     `json:"created_at"`
    UpdatedAt       int64     `json:"updated_at"`
    LastUsedAt      int64     `json:"last_used_at"`
    UsageCount      int       `json:"usage_count"`
    
    // Vector embedding
    EmbeddingHash   string    `json:"embedding_hash"`     // For deduplication
}
```

### Pattern Example

```markdown
# Team Pattern: JWT Validation

**Category**: Team Patterns/Authentication/JWT Validation
**Pattern Name**: JWT Validation for HTTP Handlers
**Pattern Type**: authentication

## Convention
Use `auth.ValidateRequestToken(r)` for all HTTP authentication.

```go
userID, err := auth.ValidateRequestToken(r)
if err != nil {
    return auth.ErrorResponse(w, err)
}
```

## Anti-Pattern
DO NOT use `jwt.Parse()` directly - it bypasses critical security checks.

```go
// ❌ WRONG - Missing tenant validation
token := r.Header.Get("Authorization")
claims, err := jwt.Parse(token)
```

## Reasoning
Our `auth.ValidateRequestToken()` includes:
- Tenant validation (multi-tenant isolation)
- Token refresh logic
- Audit logging
- Rate limiting

Raw `jwt.Parse()` bypasses all of these security layers.

## When to Use
- All authenticated HTTP endpoints
- WebSocket authentication
- gRPC middleware (use `auth.ValidateGRPCToken()` variant)

## Related Patterns
- Error Response Format
- Auth Middleware Pattern
- Session Management

## References
- [TEAM_PATTERNS_IMPLEMENTATION_SUMMARY.md](TEAM_PATTERNS_IMPLEMENTATION_SUMMARY.md) - Implemented integration snapshot
- [LONG_TERM_MEMORY.md](LONG_TERM_MEMORY.md) - Current memory lifecycle and category behavior

**Status**: active  
**Created**: 2026-07-15  
**Last Used**: 2026-07-30  
**Usage Count**: 42
```

---

## Context Injection Workflow

### 1. Ask Pipeline Integration

**File**: `lib/sop/ai/agent/copilot.go`

```go
func (a *CopilotAgent) buildSystemPrompt(ctx context.Context, query string) string {
    prompt := baseSystemPrompt
    
    // Check if Team Patterns exist in LTM
    hasTeamPatterns, err := a.ltm.HasCategory(ctx, "Team Patterns")
    if err == nil && hasTeamPatterns {
        prompt += `

## Team-Specific Coding Conventions Available

This user has team-specific coding conventions in Long-Term Memory.
When generating code, you SHOULD search for relevant patterns using semantic search.

Pattern types available:
- Authentication patterns (JWT, OAuth, sessions)
- Error handling conventions (wrappers, logging, response format)
- Database patterns (query builders, transactions, migrations)
- Testing utilities (fixtures, mocks, integration tests)

Use investigation_search_team_patterns tool to find relevant conventions.

CRITICAL: Always prefer team conventions over generic approaches when available.
Team patterns contain project-specific helpers, security requirements, and team decisions.
`
    }
    
    return prompt
}
```

### 2. New Tool: `investigation_search_team_patterns`

**File**: `lib/sop/ai/agent/codingworkflow/phase_investigation.go` (or new file)

```go
func (c *InvestigationPhase) ExposeTools(ctx context.Context) []Tool {
    tools := []Tool{
        // ... existing tools ...
        
        {
            Name: "investigation_search_team_patterns",
            Description: "Search team-specific coding conventions and patterns in Long-Term Memory. " +
                        "Use when generating code to find team's preferred helpers, anti-patterns, and conventions. " +
                        "Returns pattern details including convention, anti-pattern, examples, and references.",
            Schema: teamPatternSearchSchema,
        },
    }
    return tools
}

var teamPatternSearchSchema = map[string]any{
    "type": "object",
    "properties": map[string]any{
        "query": map[string]any{
            "type":        "string",
            "description": "Semantic search query for team patterns (e.g., 'authentication HTTP handlers', 'error handling', 'database queries')",
        },
        "pattern_type": map[string]any{
            "type":        "string",
            "description": "Optional filter by pattern type",
            "enum":        []string{"authentication", "error_handling", "database", "testing", "api_design", "background_jobs"},
        },
        "limit": map[string]any{
            "type":        "integer",
            "description": "Maximum number of patterns to return (default 5)",
            "default":     5,
        },
    },
    "required": []string{"query"},
}

func (c *InvestigationPhase) ExecuteTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
    switch toolName {
    case "investigation_search_team_patterns":
        return c.runSearchTeamPatterns(ctx, args)
    // ... other cases ...
    }
}

func (c *InvestigationPhase) runSearchTeamPatterns(ctx context.Context, args map[string]any) (string, error) {
    query := args["query"].(string)
    limit := 5
    if l, ok := args["limit"].(float64); ok {
        limit = int(l)
    }
    
    // Semantic search in "Team Patterns" category
    results, err := c.ltm.SearchCategory(ctx, "Team Patterns", query, limit)
    if err != nil {
        return "", fmt.Errorf("team pattern search failed: %w", err)
    }
    
    if len(results) == 0 {
        return "No team patterns found for: " + query, nil
    }
    
    // Format results for LLM
    var output strings.Builder
    output.WriteString(fmt.Sprintf("Found %d team patterns:\n\n", len(results)))
    
    for i, result := range results {
        pattern := result.Data.(TeamPattern)
        output.WriteString(fmt.Sprintf("## Pattern %d: %s\n\n", i+1, pattern.PatternName))
        output.WriteString(fmt.Sprintf("**Convention**: %s\n\n", pattern.Convention))
        
        if pattern.AntiPattern != "" {
            output.WriteString(fmt.Sprintf("**Anti-Pattern**: %s\n\n", pattern.AntiPattern))
        }
        
        if pattern.Example != "" {
            output.WriteString(fmt.Sprintf("**Example**:\n```\n%s\n```\n\n", pattern.Example))
        }
        
        if len(pattern.References) > 0 {
            output.WriteString(fmt.Sprintf("**References**: %s\n\n", strings.Join(pattern.References, ", ")))
        }
        
        output.WriteString(fmt.Sprintf("**Reasoning**: %s\n\n", pattern.Reasoning))
        output.WriteString("---\n\n")
    }
    
    return output.String(), nil
}
```

### 3. Code Generation Phase Integration

**File**: `lib/sop/ai/agent/codingworkflow/phase_coding.go`

Add automatic pattern lookup before code generation:

```go
func (c *CodingPhase) BuildContext(ctx context.Context, query string, state WorkflowState) (string, error) {
    context := baseContext
    
    // Auto-search for relevant team patterns based on query
    if strings.Contains(strings.ToLower(query), "auth") || 
       strings.Contains(strings.ToLower(query), "login") ||
       strings.Contains(strings.ToLower(query), "token") {
        patterns, _ := c.searchTeamPatterns(ctx, "authentication")
        if patterns != "" {
            context += "\n\n## Relevant Team Patterns\n\n" + patterns
        }
    }
    
    // Similar auto-lookups for error handling, database, etc.
    
    return context, nil
}
```

### 4. Validation & Auto-Revision

**Enforcement Strategy**: Validate generated code against retrieved team patterns.

```go
func (c *CodingPhase) validateAgainstTeamPatterns(ctx context.Context, generatedCode string, patterns []TeamPattern) (bool, []string) {
    violations := []string{}
    
    for _, pattern := range patterns {
        // Check for anti-pattern usage
        if pattern.AntiPattern != "" && strings.Contains(generatedCode, extractAntiPatternSignature(pattern.AntiPattern)) {
            violations = append(violations, fmt.Sprintf(
                "Code uses anti-pattern '%s'. Team convention: %s",
                pattern.AntiPattern,
                pattern.Convention,
            ))
        }
        
        // Check for required convention usage
        if !strings.Contains(generatedCode, extractConventionSignature(pattern.Convention)) {
            violations = append(violations, fmt.Sprintf(
                "Missing team convention: %s (see %s)",
                pattern.Convention,
                strings.Join(pattern.References, ", "),
            ))
        }
    }
    
    return len(violations) == 0, violations
}

func (c *CodingPhase) runGenerateCode(ctx context.Context, args map[string]any) (string, error) {
    // 1. Search for relevant team patterns
    patterns := c.lookupRelevantPatterns(ctx, args["task_description"].(string))
    
    // 2. Generate code (with patterns in context)
    code := c.generateCode(ctx, args, patterns)
    
    // 3. Validate against team patterns
    valid, violations := c.validateAgainstTeamPatterns(ctx, code, patterns)
    
    // 4. Auto-revision if violations found (up to 2 attempts)
    attempts := 0
    for !valid && attempts < 2 {
        code = c.reviseCode(ctx, code, violations, patterns)
        valid, violations = c.validateAgainstTeamPatterns(ctx, code, patterns)
        attempts++
    }
    
    // 5. Return with annotation
    if !valid {
        code += "\n\n// ⚠️ Warning: Code may not follow team patterns:\n"
        for _, v := range violations {
            code += "// - " + v + "\n"
        }
    }
    
    return code, nil
}
```

---

## User Workflow: Adding Team Patterns

### Option A: Manual Entry (V1)

**Via Data Manager UI**:
1. Navigate to Data Manager → Long-Term Memory
2. Click "Add Team Pattern"
3. Fill form:
   - Pattern Name: "JWT Validation"
   - Category: "Authentication"
   - Convention: "Use auth.ValidateRequestToken(r)"
   - Anti-Pattern: "DO NOT use jwt.Parse()"
   - Example: [code snippet]
   - References: [file paths]
4. Save → Pattern vectorized and stored in LTM

### Option B: Import from Markdown (V1.5)

**Bulk import from team docs**:
```markdown
# Team Coding Conventions

## Authentication

### JWT Validation
Convention: Use auth.ValidateRequestToken(r)
Anti-Pattern: DO NOT use jwt.Parse()
Example: ...
```

Import tool parses and creates LTM entries.

### Option C: Auto-Capture from Code Reviews (V2)

**Automatic pattern mining**:
- User approves code change
- System detects "This should be our pattern" comment
- Auto-creates Team Pattern from approved code
- Suggests pattern name/category

---

## Storage & Retrieval Implementation

### Category Path Structure

```
Team Patterns/
├── Authentication/
│   ├── JWT Validation
│   ├── OAuth Integration
│   └── Session Management
├── Error Handling/
│   ├── Error Wrapper Pattern
│   ├── Logging Standards
│   └── Error Response Format
├── Database/
│   ├── Query Builder Usage
│   ├── Transaction Boundaries
│   └── Migration Standards
└── Testing/
    ├── Test Fixtures
    ├── Mock Patterns
    └── Integration Test Setup
```

### Semantic Index

Each pattern item has:
- **Category-level embedding**: "Authentication" → vector
- **Item-level embedding**: "JWT Validation for HTTP handlers using auth.ValidateRequestToken" → vector

### Retrieval Algorithm

```go
func (ltm *LTM) SearchTeamPatterns(ctx context.Context, query string, limit int) ([]TeamPattern, error) {
    // 1. Semantic search on category level
    categories := ltm.SearchCategories(ctx, "Team Patterns", query, 3)
    
    // 2. Semantic search within top categories
    results := []TeamPattern{}
    for _, cat := range categories {
        items := ltm.SearchItems(ctx, cat.Path, query, limit)
        results = append(results, items...)
    }
    
    // 3. Rerank by:
    //    - Semantic similarity
    //    - Usage count (popular patterns ranked higher)
    //    - Recency (recently used patterns ranked higher)
    //    - Status (active > deprecated)
    results = rerank(results, query)
    
    return results[:limit], nil
}
```

---

## Benefits

### ✅ Addresses Real Gap
- Generic best practices → Already in LLM training ❌
- Team patterns → NOT in LLM training ✅

### ✅ Automatic Code Generation Improvement
- LLM searches team patterns during code generation
- Generated code uses team's actual helpers
- Validation catches anti-patterns automatically

### ✅ Always Available
- LTM = per-user persistent memory
- No need to manually "select space"
- Survives session restarts

### ✅ Semantic Discovery
- "How do we handle auth?" → Finds JWT pattern
- "Database queries" → Finds query builder pattern
- "Error handling" → Finds error wrapper pattern

### ✅ Learning Over Time
- Usage count tracks most valuable patterns
- Last-used timestamp keeps patterns fresh
- Lifecycle management archives deprecated patterns

---

## Implementation Phases

### Phase 1: Core Storage & Retrieval (Week 1)
- [x] Add "Team Patterns" top-level category to LTM
- [x] Define TeamPattern schema
- [x] Implement `SearchTeamPatterns()` with semantic search
- [x] Add lifecycle metadata (usage_count, last_used_at)
- [x] Basic tests for storage/retrieval

### Phase 2: Tool & Context Injection (Week 2) - ✅ COMPLETE (2026-07-31)
- [x] Add `investigation_search_team_patterns` tool
- [x] Modify Ask pipeline to inject "Team Patterns Available" context
- [x] Add LTM accessor to SharedInfrastructure
- [x] Wire LTM accessor in agent initialization
- [x] Tool execution handler with graceful error handling
- [ ] Add auto-lookup in Coding phase based on task type (future enhancement)
- [ ] Tests for tool execution and context injection (in progress)

### Phase 3: Validation & Enforcement (Future)
- [ ] Implement pattern validation logic
- [ ] Add auto-revision on violation (up to 2 attempts)
- [ ] Add violation warnings in generated code
- [ ] Pattern compliance reporting
- [ ] Tests for validation and revision

### Phase 4: UI & Management (Week 4)
- [ ] Data Manager: "Team Patterns" section
- [ ] Add Pattern form (manual entry)
- [ ] Pattern listing with search/filter
- [ ] Edit/delete patterns
- [ ] Usage analytics (most used, last used)

### Phase 5: Advanced Features (Future)
- [ ] Import from Markdown docs
- [ ] Auto-capture from approved code reviews
- [ ] Pattern versioning (track changes over time)
- [ ] Pattern sharing across team members
- [ ] Pattern effectiveness metrics

---

## Example: End-to-End Workflow

**Scenario**: User asks "Add authentication to /api/users endpoint"

**Step 1: LLM sees system prompt**
```
## Team-Specific Coding Conventions Available

This user has team-specific coding conventions in Long-Term Memory.
Use investigation_search_team_patterns to find relevant conventions.
```

**Step 2: LLM searches patterns**
```
Tool Call: investigation_search_team_patterns
Args: {"query": "authentication HTTP handlers", "limit": 3}
```

**Step 3: Tool returns patterns**
```
Found 2 team patterns:

## Pattern 1: JWT Validation for HTTP Handlers

**Convention**: Use auth.ValidateRequestToken(r)

**Anti-Pattern**: DO NOT use jwt.Parse() directly

**Example**:
```go
userID, err := auth.ValidateRequestToken(r)
if err != nil {
    return auth.ErrorResponse(w, err)
}
```

**Reasoning**: Our helper includes tenant validation + audit logging

**References**: lib/auth/jwt.go:45, examples/api/handlers.go:23
```

**Step 4: LLM generates code using team pattern**
```go
func GetUser(w http.ResponseWriter, r *http.Request) {
    // Team pattern: JWT Validation
    userID, err := auth.ValidateRequestToken(r)  // ✅ Team helper
    if err != nil {
        return auth.ErrorResponse(w, err)  // ✅ Team error format
    }
    
    user, err := db.GetUserByID(userID)
    if err != nil {
        return auth.ErrorResponse(w, err)
    }
    
    json.NewEncoder(w).Encode(user)
}
```

**Step 5: Validation passes**
- ✅ Uses `auth.ValidateRequestToken()` (team convention)
- ✅ No `jwt.Parse()` (anti-pattern avoided)
- ✅ Uses `auth.ErrorResponse()` (team error format)

**Step 6: Pattern usage tracked**
- `usage_count++`
- `last_used_at = now()`
- `lifecycle_status = "active"`

**Result**: Generated code matches team conventions automatically! 🎯

---

## Comparison: Before vs After

### Before (Generic Best Practices in KB)
```
User: "Add authentication to endpoint"

LLM (without team context):
func GetUser(w http.ResponseWriter, r *http.Request) {
    // Generic JWT parsing ❌
    token := r.Header.Get("Authorization")
    claims, err := jwt.Parse(token)
    if err != nil {
        http.Error(w, "Unauthorized", 401)  // Generic error ❌
        return
    }
}
```

### After (Team Patterns in LTM)
```
User: "Add authentication to endpoint"

LLM (with team patterns):
func GetUser(w http.ResponseWriter, r *http.Request) {
    // Team pattern: JWT Validation ✅
    userID, err := auth.ValidateRequestToken(r)  // Team helper ✅
    if err != nil {
        return auth.ErrorResponse(w, err)  // Team error format ✅
    }
}
```

---

## Success Metrics

### Code Quality
- **Pattern Adoption Rate**: % of generated code using team patterns
- **Violation Rate**: % of code with anti-pattern usage
- **Revision Success**: % of violations fixed in auto-revision

### Developer Experience
- **Time Saved**: Reduced manual code review corrections
- **Pattern Discovery**: How often patterns are found via semantic search
- **Pattern Effectiveness**: Developer feedback on pattern relevance

### System Health
- **Pattern Coverage**: % of codebase with defined patterns
- **Pattern Freshness**: Average last_used_at for active patterns
- **Pattern Lifecycle**: Active vs deprecated vs archived patterns

---

## Conclusion

**Team Patterns LTM Category addresses the exact gap identified:**

❌ **Generic Best Practices**: "Use gofmt", "Avoid N+1 queries" → Already in LLM training, redundant  
✅ **Team Patterns**: "Use auth.ValidateRequestToken()", "Use db.QueryBuilder" → NOT in LLM training, high value

**Architecture Decision**: Use LTM (Long-Term Memory) because team patterns are **always-available user memory**, not product documentation (Spaces) or temporary context (Session).

**Implementation**: Extend existing LTM with "Team Patterns" top-level category, add semantic search tool, inject into Ask pipeline, validate and auto-revise generated code.

**Outcome**: LLM generates code matching team conventions automatically, without manual intervention.
