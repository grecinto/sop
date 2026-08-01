# Team Patterns Integration Plan

**Status**: Implementation Ready  
**Created**: 2026-07-31  
**Goal**: Wire Team Patterns into production Ask pipeline for automatic code generation improvement

---

## Overview

**Current State**: Foundation exists (`lib/sop/ai/agent/teampatterns/`) but not integrated  
**Target State**: LLM automatically discovers and uses team patterns during code generation

**Architectural Compliance**:
- ✅ Participant Pattern - Phase exposes tool, LLM decides when to call
- ✅ LLM-as-Orchestrator - No automatic lookup, LLM must explicitly search
- ✅ Explicit Parameters - Pass LTM explicitly, not via context

---

## Phase 1: Core Integration (4 files, ~150 lines)

### Step 1.1: Add LTM Accessor to SharedInfrastructure

**File**: `lib/sop/ai/agent/codingworkflow/shared_infrastructure.go`

**Change**: Add LTM accessor field and method

```go
type SharedInfrastructure struct {
    // ... existing fields ...
    SystemDB        *database.Database  // Existing
    STMAccessor     func() *agent.ShortTermMemory  // Existing
    
    // NEW: Long-Term Memory accessor for team patterns
    LTMAccessor     func(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error)
}

// NEW: Accessor method
func (s *SharedInfrastructure) GetLTM(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error) {
    if s.LTMAccessor == nil {
        return nil, fmt.Errorf("LTM accessor not configured")
    }
    return s.LTMAccessor(ctx)
}
```

**Lines**: ~10 lines

---

### Step 1.2: Add Tool Definition (Investigation Phase)

**File**: `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`

**Location**: In `phaseWorkflowTools()` switch statement, `case "investigation"`

**Change**: Add new tool after existing investigation tools

```go
case "investigation":
    return []capabilities.ToolDefinition{{
        Name:        c.phaseToolName("build_context"),
        Description: "REQUIRED FIRST STEP: Builds repository context...",
        Schema:      taskSchema,
    }, {
        Name:        c.phaseToolName("inspect_repo"),
        Description: "Inspects current repository state...",
        Schema:      taskSchema,
    }, {
        Name:        c.phaseToolName("search_repo"),
        Description: "Searches repository for patterns...",
        Schema:      searchSchema,
    }, {
        Name:        c.phaseToolName("list_changed_files"),
        Description: "Lists files with uncommitted changes...",
        Schema:      taskSchema,
    }, {
        // NEW TOOL
        Name:        c.phaseToolName("search_team_patterns"),
        Description: "Searches team-specific coding conventions and patterns in Long-Term Memory. " +
                    "Use when planning code generation to find team's preferred helpers, anti-patterns, and conventions. " +
                    "Returns pattern details including convention, anti-pattern, examples, and file references. " +
                    "WHEN TO CALL: Before designing or implementing features to ensure code follows team standards.",
        Schema: `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string",
      "description": "Semantic search query for team patterns (e.g., 'authentication HTTP handlers', 'error handling', 'database queries', 'testing utilities')"
    },
    "pattern_type": {
      "type": "string",
      "description": "Optional filter by pattern type",
      "enum": ["authentication", "error_handling", "database", "testing", "api_design", "background_jobs"]
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of patterns to return (default 5)",
      "default": 5
    }
  }
}`,
    }}
```

**Lines**: ~30 lines

---

### Step 1.3: Add Tool Execution Handler

**File**: `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`

**Location**: In `ExecuteTool()` method, in the switch statement for investigation tools

**Change**: Add new case handler

```go
func (c *PhaseCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
    switch toolName {
    // ... existing cases ...
    
    case c.phaseToolName("search_team_patterns"):
        return c.runSearchTeamPatterns(ctx, args)
    
    // ... rest of cases ...
    }
}

// NEW: Handler implementation
func (c *PhaseCapability) runSearchTeamPatterns(ctx context.Context, args map[string]any) (string, error) {
    // Get LTM from shared infrastructure
    ltm, err := c.sharedInfra.GetLTM(ctx)
    if err != nil {
        return "Team patterns not available (LTM not configured)", nil
    }
    
    // Extract arguments
    query, ok := args["query"].(string)
    if !ok || strings.TrimSpace(query) == "" {
        return "", fmt.Errorf("query is required")
    }
    
    patternType := ""
    if pt, ok := args["pattern_type"].(string); ok {
        patternType = pt
    }
    
    limit := 5
    if l, ok := args["limit"].(float64); ok {
        limit = int(l)
    }
    
    // Call team patterns library
    patterns, err := teampatterns.SearchTeamPatterns(ctx, ltm, query, patternType, limit)
    if err != nil {
        return "", fmt.Errorf("team pattern search failed: %w", err)
    }
    
    if len(patterns) == 0 {
        return fmt.Sprintf("No team patterns found for: %s", query), nil
    }
    
    // Format results using existing formatter
    var output strings.Builder
    output.WriteString(fmt.Sprintf("Found %d team patterns:\n\n", len(patterns)))
    
    for i, pattern := range patterns {
        output.WriteString(teampatterns.FormatPatternForDisplay(pattern, i+1))
    }
    
    return output.String(), nil
}
```

**Lines**: ~50 lines

---

### Step 1.4: Inject Context in System Prompt

**File**: `lib/sop/ai/agent/copilot.go`

**Location**: In `buildSystemPrompt()` method, after persona building, before returning

**Change**: Add conditional team patterns context

```go
func (a *CopilotAgent) buildSystemPrompt(ctx context.Context, query string, taskContext TaskContextClassification) string {
    // ... existing persona building ...
    
    persona += "CRITICAL SYSTEM GUARDRAIL:\n" +
        "1. Autonomous Research: ..." +
        "..." // existing guardrails
    
    // NEW: Team Patterns context injection (conditional)
    if a.systemDB != nil {
        // Check if team patterns exist in LTM
        if ltm, err := a.getOrCreateLTM(ctx); err == nil {
            if hasPatterns, _ := teampatterns.HasTeamPatterns(ctx, ltm); hasPatterns {
                persona += "\n\n## Team-Specific Coding Conventions Available\n\n" +
                    "This user has team-specific coding conventions in Long-Term Memory.\n" +
                    "When generating or planning code, you SHOULD search for relevant patterns.\n\n" +
                    "Pattern types available:\n" +
                    "- Authentication patterns (JWT, OAuth, sessions)\n" +
                    "- Error handling conventions (wrappers, logging, response format)\n" +
                    "- Database patterns (query builders, transactions, migrations)\n" +
                    "- Testing utilities (fixtures, mocks, integration tests)\n" +
                    "- API design patterns (REST conventions, validation)\n" +
                    "- Background job patterns (async processing, queues)\n\n" +
                    "Use investigation_search_team_patterns tool to find relevant conventions.\n\n" +
                    "CRITICAL: Always prefer team conventions over generic approaches when patterns exist.\n" +
                    "Team patterns contain project-specific helpers, security requirements, and team decisions.\n\n"
            }
        }
    }
    
    // ... rest of method ...
}

// NEW: Helper method to get LTM instance
func (a *CopilotAgent) getOrCreateLTM(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error) {
    if a.systemDB == nil {
        return nil, fmt.Errorf("systemDB not available")
    }
    
    // Get user ID from context or session
    userID := a.getUserID(ctx)
    if userID == "" {
        userID = "default" // Fallback
    }
    
    kbName := fmt.Sprintf("ltm_%s", userID)
    
    tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback(ctx)
    
    var embedder ai.Embeddings
    if a.service != nil && a.service.Domain() != nil {
        embedder = a.service.Domain().Embedder()
    }
    
    return a.systemDB.OpenKnowledgeBase(ctx, kbName, tx, a.brain, embedder, false)
}
```

**Lines**: ~60 lines

---

## Phase 2: Wire LTM Accessor (2 files, ~30 lines)

### Step 2.1: Pass LTM Accessor to SharedInfrastructure

**File**: `lib/sop/ai/agent/copilot.go`

**Location**: Where SharedInfrastructure is created (search for `NewDefaultSharedInfrastructure` or where phases are initialized)

**Change**: Add LTM accessor when creating infrastructure

```go
// In the method that creates SharedInfrastructure
func (a *CopilotAgent) getOrCreateSharedInfra() *codingworkflow.SharedInfrastructure {
    if a.sharedInfra != nil {
        return a.sharedInfra
    }
    
    a.sharedInfra = &codingworkflow.SharedInfrastructure{
        SystemDB:    a.systemDB,
        STMAccessor: func() *agent.ShortTermMemory {
            if a.service != nil && a.service.session != nil {
                return a.service.session.Memory
            }
            return nil
        },
        // NEW: LTM accessor
        LTMAccessor: func(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error) {
            return a.getOrCreateLTM(ctx)
        },
    }
    
    return a.sharedInfra
}
```

**Lines**: ~15 lines

---

### Step 2.2: Import Team Patterns Package

**File**: `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`

**Location**: Top of file, in imports section

**Change**: Add import

```go
import (
    // ... existing imports ...
    "github.com/sharedcode/sop/ai/agent/teampatterns"
)
```

**Lines**: 1 line

---

## Phase 3: Testing (2 files, ~200 lines)

### Step 3.1: Unit Test for Tool Execution

**File**: `lib/sop/ai/agent/codingworkflow/phase_capabilities_test.go`

**Add**: New test function

```go
func TestInvestigationPhase_SearchTeamPatterns(t *testing.T) {
    // Setup mock LTM with test patterns
    mockLTM := setupMockLTMWithPatterns(t)
    
    infra := &SharedInfrastructure{
        LTMAccessor: func(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error) {
            return mockLTM, nil
        },
    }
    
    cap := NewInvestigationPhaseCapability(infra)
    
    // Test: Search for authentication patterns
    result, err := cap.ExecuteTool(context.Background(), "investigation_search_team_patterns", map[string]any{
        "query": "authentication HTTP handlers",
        "limit": 5,
    }, capabilities.RoutingState{})
    
    if err != nil {
        t.Fatalf("ExecuteTool failed: %v", err)
    }
    
    if !strings.Contains(result, "team patterns") {
        t.Errorf("Expected result to mention team patterns, got: %s", result)
    }
}

func TestInvestigationPhase_SearchTeamPatterns_NoResults(t *testing.T) {
    mockLTM := setupMockLTMEmpty(t)
    
    infra := &SharedInfrastructure{
        LTMAccessor: func(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error) {
            return mockLTM, nil
        },
    }
    
    cap := NewInvestigationPhaseCapability(infra)
    
    result, err := cap.ExecuteTool(context.Background(), "investigation_search_team_patterns", map[string]any{
        "query": "nonexistent pattern",
    }, capabilities.RoutingState{})
    
    if err != nil {
        t.Fatalf("ExecuteTool failed: %v", err)
    }
    
    if !strings.Contains(result, "No team patterns found") {
        t.Errorf("Expected 'No team patterns found', got: %s", result)
    }
}
```

**Lines**: ~60 lines

---

### Step 3.2: Integration Test for System Prompt

**File**: `lib/sop/ai/agent/copilot_test.go`

**Add**: Test for system prompt injection

```go
func TestBuildSystemPrompt_TeamPatternsContext(t *testing.T) {
    // Setup agent with mock LTM containing patterns
    agent := setupTestAgentWithTeamPatterns(t)
    
    ctx := context.Background()
    taskCtx := TaskContextClassification{
        Domain: "coding_workflow",
    }
    
    prompt := agent.buildSystemPrompt(ctx, "fix auth bug", taskCtx)
    
    // Verify team patterns context is included
    if !strings.Contains(prompt, "Team-Specific Coding Conventions Available") {
        t.Error("Expected team patterns context in system prompt")
    }
    
    if !strings.Contains(prompt, "investigation_search_team_patterns") {
        t.Error("Expected mention of search tool in system prompt")
    }
}

func TestBuildSystemPrompt_NoTeamPatterns(t *testing.T) {
    // Setup agent without team patterns
    agent := setupTestAgentEmptyLTM(t)
    
    ctx := context.Background()
    taskCtx := TaskContextClassification{
        Domain: "coding_workflow",
    }
    
    prompt := agent.buildSystemPrompt(ctx, "fix bug", taskCtx)
    
    // Verify team patterns context is NOT included
    if strings.Contains(prompt, "Team-Specific Coding Conventions") {
        t.Error("Did not expect team patterns context when LTM is empty")
    }
}
```

**Lines**: ~40 lines

---

## Phase 4: Documentation Updates (2 files, ~50 lines)

### Step 4.1: Update Implementation Checklist

**File**: `docs/TEAM_PATTERNS_LTM_DESIGN.md`

**Change**: Update Phase 2 checklist

```markdown
### Phase 2: Tool & Context Injection (Week 2)
- [x] Add `investigation_search_team_patterns` tool
- [x] Modify Ask pipeline to inject "Team Patterns Available" context
- [x] Add auto-lookup in Investigation phase
- [ ] Add auto-lookup in Coding phase based on task type (future)
- [x] Tests for tool execution and context injection
```

---

### Step 4.2: Add Usage Guide

**File**: `docs/TEAM_PATTERNS_LTM_DESIGN.md`

**Add**: New section after "Example: End-to-End Workflow"

```markdown
## Usage Guide: For Users

### Adding Your First Team Pattern

1. **Via Data Manager UI** (when available):
   - Navigate to Long-Term Memory
   - Click "Add Team Pattern"
   - Fill in convention, anti-pattern, example
   - Save

2. **Via Direct LTM Storage** (current):
   ```go
   pattern := &teampatterns.TeamPattern{
       PatternName: "JWT Validation",
       PatternType: "authentication",
       Convention:  "Use auth.ValidateRequestToken(r)",
       AntiPattern: "DO NOT use jwt.Parse()",
       Example:     "userID, err := auth.ValidateRequestToken(r)",
       References:  []string{"lib/auth/jwt.go:45"},
   }
   teampatterns.StoreTeamPattern(ctx, ltm, pattern)
   ```

### Using Team Patterns During Code Generation

The LLM will automatically:
1. See "Team Patterns Available" in system prompt
2. Call `investigation_search_team_patterns` tool when planning code
3. Generate code using team conventions

**Example conversation**:
```
User: "Add authentication to /api/users endpoint"

LLM: [Calls investigation_search_team_patterns(query="authentication")]
     [Receives: "Use auth.ValidateRequestToken()" pattern]
     [Generates code using team convention]

Generated Code:
func handleUsers(w http.ResponseWriter, r *http.Request) {
    userID, err := auth.ValidateRequestToken(r)  // ✅ Team pattern!
    if err != nil {
        return auth.ErrorResponse(w, err)
    }
    // ... rest of handler
}
```
```

---

## Implementation Order

### Week 1: Core Integration (Priority 1)

**Day 1-2**: Foundation
1. Step 1.1: Add LTM accessor to SharedInfrastructure
2. Step 2.1: Wire LTM accessor in copilot.go
3. Step 2.2: Add imports

**Day 3-4**: Tool Implementation
4. Step 1.2: Add tool definition
5. Step 1.3: Add tool execution handler

**Day 5**: Context Injection
6. Step 1.4: Inject system prompt context

---

### Week 2: Testing & Validation (Priority 2)

**Day 1-2**: Unit Tests
7. Step 3.1: Unit tests for tool execution

**Day 3**: Integration Tests
8. Step 3.2: System prompt integration tests

**Day 4-5**: End-to-End Testing
9. Manual testing with real LTM data
10. Documentation updates (Steps 4.1, 4.2)

---

## Validation Criteria

### ✅ Feature Complete When:

1. **Tool Registration**
   - Tool appears in `investigation` phase tool list
   - Tool has proper schema with required/optional fields
   - Tool description guides LLM when to use it

2. **Execution Works**
   - Tool can be called with valid arguments
   - Returns formatted team patterns
   - Handles "no results" gracefully
   - Error messages are clear

3. **Context Injection**
   - System prompt includes team patterns section (when patterns exist)
   - System prompt does NOT include section (when no patterns)
   - Prompt mentions the tool name correctly

4. **LLM Can Use It**
   - LLM sees tool in available tools
   - LLM can understand when to call it
   - LLM receives formatted results
   - LLM generates code using retrieved patterns

5. **Tests Pass**
   - Unit tests for tool execution
   - Integration tests for system prompt
   - No regressions in existing tests

---

## Rollout Strategy

### Stage 1: Internal Testing (Week 1-2)
- Deploy to development environment
- Test with sample team patterns
- Verify LLM behavior

### Stage 2: Beta Testing (Week 3)
- Enable for small group of users
- Collect feedback on pattern quality
- Measure pattern adoption rate

### Stage 3: General Availability (Week 4)
- Enable for all users
- Monitor usage metrics
- Iterate based on feedback

---

## Success Metrics

### Immediate (Week 1-2)
- [ ] Tool successfully registered in investigation phase
- [ ] Tool callable and returns results
- [ ] System prompt injection works
- [ ] All tests pass

### Short-term (Month 1)
- [ ] 5+ team patterns stored by early adopters
- [ ] LLM calls search_team_patterns tool in 50%+ of code generation tasks
- [ ] Generated code uses patterns in 70%+ of relevant cases

### Long-term (Quarter 1)
- [ ] 20+ patterns per active team
- [ ] Pattern adoption rate > 80%
- [ ] Reduced code review corrections by 30%
- [ ] Positive developer feedback on pattern relevance

---

## Risk Mitigation

### Risk 1: LLM Doesn't Call Tool
**Mitigation**: 
- Clear tool description with "WHEN TO CALL" guidance
- System prompt emphasizes importance
- Monitor tool usage metrics

### Risk 2: Poor Pattern Quality
**Mitigation**:
- Provide pattern template/examples
- Add validation on pattern storage
- Review patterns periodically

### Risk 3: Performance Impact
**Mitigation**:
- LTM search is already optimized (semantic index)
- Cache HasTeamPatterns() result per session
- Limit default search results to 5

---

## Future Enhancements (Post-Integration)

### Phase 3: Validation & Enforcement
- Auto-validate generated code against anti-patterns
- Suggest fixes when violations detected
- Track pattern compliance metrics

### Phase 4: UI & Management
- Data Manager UI for pattern CRUD
- Pattern usage analytics dashboard
- Pattern versioning and history

### Phase 5: Advanced Features
- Import patterns from Markdown docs
- Auto-capture patterns from approved code reviews
- Pattern sharing across team members
- Pattern effectiveness metrics

---

## Appendix: File Locations

### Files to Modify
1. `lib/sop/ai/agent/codingworkflow/shared_infrastructure.go` (~10 lines)
2. `lib/sop/ai/agent/codingworkflow/phase_capabilities.go` (~80 lines)
3. `lib/sop/ai/agent/copilot.go` (~75 lines)
4. `lib/sop/ai/agent/codingworkflow/phase_capabilities_test.go` (~60 lines)
5. `lib/sop/ai/agent/copilot_test.go` (~40 lines)
6. `docs/TEAM_PATTERNS_LTM_DESIGN.md` (~50 lines)

### Total Code Changes
- **New code**: ~265 lines
- **Modified files**: 6
- **New files**: 0 (reuses existing `teampatterns` package)

### Dependencies
- Existing: `lib/sop/ai/agent/teampatterns/` package
- Existing: `lib/sop/ai/memory/` package
- No new external dependencies

---

## Summary

This plan provides a **minimal, surgical integration** of Team Patterns into the existing Ask pipeline:

✅ **Follows architectural patterns** (Participant, LLM-as-Orchestrator)  
✅ **Reuses existing foundation** (teampatterns package)  
✅ **Minimal code changes** (~265 lines across 6 files)  
✅ **Clear validation criteria** (5 checkpoints)  
✅ **Low risk** (no breaking changes, additive only)  

**Next Step**: Begin implementation with Step 1.1 (SharedInfrastructure).
