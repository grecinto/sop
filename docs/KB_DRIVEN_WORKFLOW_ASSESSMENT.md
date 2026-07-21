# KB-Driven Workflow Automation: Assessment & Proposal

**Status**: FEASIBILITY ASSESSMENT  
**Date**: 2026-07-20  
**Context**: Phase 2 auto-inline paused to assess KB-driven workflow approach

---

## Executive Summary

**Feasibility**: ✅ **HIGHLY FEASIBLE** - All required components already exist  
**Impact**: 🚀 **TRANSFORMATIVE** - Moves from code-driven to data-driven workflows  
**Effort**: 📊 **MODERATE** (2-3 days implementation)

### Key Insight
Instead of hardcoding workflows like `orchestrateTestFixWorkflow()`, we can:
1. **Store workflow expertise in KnowledgeBase** (semantic search-enabled)
2. **Use a generic WorkflowManager** to retrieve and execute workflows
3. **Leverage existing Queue + Tools** for orchestration

This makes workflows **learnable**, **updatable**, and **discoverable** without code changes.

---

## Current Architecture Analysis

### ✅ What We Already Have

#### 1. **KnowledgeBase System** (lib/sop/ai/memory/knowledge_base.go)
- **Hierarchical semantic categories**: M-way tree with O(D log N) search
- **SearchByPath()**: Lexical + semantic fallback navigation
- **Search()**: Hybrid vector + text search with RRF ranking
- **Already integrated**: retrieveKnowledge() in Ask() pipeline
- **Proven**: Powers SOP KB, coding workflow expertise retrieval

**Example CategoryPath structure**:
```
Workflows/
├── TestDebugging/
│   ├── Go/
│   │   ├── UnitTests/
│   │   └── IntegrationTests/
│   └── Python/
├── Implementation/
│   ├── FeatureDevelopment/
│   └── BugFixes/
├── Refactoring/
│   ├── CodeCleanup/
│   └── ArchitectureChanges/
└── Investigation/
    ├── ErrorAnalysis/
    └── PerformanceDebugging/
```

#### 2. **Tool Registry** (lib/sop/ai/agent/registry.go)
- **ToolDefinition**: name, description, schema, handler
- **Dynamic registration**: RegisterWithUI(), Register()
- **Capability integration**: registerCapabilityTools()
- **Already exposed to LLM**: GeneratePrompt()

**Available tools** (all tested and working):
- `execute_local_command` - Run shell commands
- `read_file` - Read file contents
- `write_file` - Create/overwrite files
- `patch_file` - Apply focused changes
- `run_repo_tests` - Execute test suite with verbose output
- `search_repo_files` - Find files by pattern
- `grep_repo_text` - Search code for patterns
- `read_tool_result` - Read large tool outputs from files
- `grep_tool_result` - Search stored tool results

#### 3. **Queue Orchestration** (lib/sop/ai/agent/copilot_multiturn.go)
- **WorkQueue**: Priority-based, actor-limited execution
- **WorkItem**: Query, Actor, Priority, Condition
- **Actor limits**: SetMaxPerActor() for iteration control
- **Conditional execution**: Condition(TurnResult) bool
- **Already working**: Phase 1.5 test debugging workflow

#### 4. **Coding Workflow Capability** (lib/sop/ai/agent/codingworkflow/capability.go)
- **Orchestrate()**: Entry point for multi-turn workflows
- **Task classification**: Debugging, Implementation, Testing, Refactoring
- **Workflow injection**: Currently hardcoded in orchestrateTestFixWorkflow()
- **Context access**: getWorkQueue(ctx) and getQuery(ctx)

---

## Proposed Architecture

### 🎯 Core Concept: KB as Workflow Repository

Instead of this (hardcoded):
```go
func (c *Capability) orchestrateTestFixWorkflow(queue WorkQueue, query string) {
    queue.Enqueue(map[string]interface{}{
        "query":    "Run tests with verbose output",
        "actor":    "test_debugger.runner",
        "priority": 100,
    })
    // ... 6 more hardcoded steps
}
```

We do this (data-driven):
```go
func (c *Capability) Orchestrate(ctx context.Context, state RoutingState) (bool, error) {
    // 1. Query KB for matching workflow
    workflow, err := c.workflowManager.GetWorkflow(ctx, query, state)
    
    // 2. Execute workflow from KB definition
    return c.workflowManager.Execute(ctx, workflow, queue)
}
```

### 📊 Workflow Definition Schema

**KB Entry Structure**:
```json
{
  "category_path": "Workflows/TestDebugging/Go/UnitTests",
  "summary": "Systematic test debugging protocol: run → analyze → fix → verify",
  "search_text": "fix failing tests, debug test failures, test not passing",
  "data": {
    "workflow_id": "test-debugging-v1",
    "version": "1.0",
    "trigger_patterns": ["fix.*test", "failing.*test", "test.*failed"],
    "steps": [
      {
        "step": 1,
        "actor": "test_debugger.runner",
        "priority": 100,
        "query_template": "Run tests with verbose output: {original_query}",
        "tools_expected": ["run_repo_tests"],
        "verification": "test_output_captured"
      },
      {
        "step": 2,
        "actor": "test_debugger.analyzer",
        "priority": 90,
        "query_template": "Read test error output. Extract actual error message.",
        "tools_expected": ["read_tool_result", "grep_tool_result"],
        "depends_on": ["step:1"],
        "condition": "has_failure_indicators"
      },
      {
        "step": 3,
        "actor": "test_debugger.analyzer",
        "priority": 80,
        "query_template": "Read failing test source to understand what's tested",
        "tools_expected": ["read_file", "search_repo_files"]
      },
      {
        "step": 4,
        "actor": "test_debugger.analyzer",
        "priority": 70,
        "query_template": "Find and read implementation code the test checks",
        "tools_expected": ["read_file", "search_repo_files", "grep_repo_text"]
      },
      {
        "step": 5,
        "actor": "test_debugger.fixer",
        "priority": 60,
        "query_template": "Based on error + test + impl, apply fix",
        "tools_expected": ["patch_file", "write_file"]
      },
      {
        "step": 6,
        "actor": "test_debugger.verifier",
        "priority": 50,
        "query_template": "Re-run tests to verify fix works",
        "tools_expected": ["run_repo_tests"],
        "verification": "tests_passing"
      },
      {
        "step": 7,
        "actor": "test_debugger.repair",
        "priority": 40,
        "max_iterations": 3,
        "query_template": "Test still failed. Analyze new error and iterate.",
        "condition": "tests_still_failing",
        "loop_to": "step:2"
      }
    ],
    "actor_limits": {
      "test_debugger.repair": 3,
      "test_debugger.fixer": 5
    },
    "success_criteria": {
      "required_tools_used": ["run_repo_tests", "read_file", "patch_file"],
      "final_state": "tests_passing"
    },
    "failure_recovery": {
      "max_turns": 15,
      "timeout_per_turn": "5m",
      "on_timeout": "report_and_continue"
    }
  }
}
```

**Benefits of JSON Schema**:
- ✅ **Updatable**: Change workflow without code changes
- ✅ **Versionable**: Track workflow evolution (v1 → v2)
- ✅ **Queryable**: Search "how to fix tests?" returns workflow
- ✅ **Learnable**: Add new workflows via KB ingestion
- ✅ **Debuggable**: Inspect workflow execution at runtime
- ✅ **Testable**: Validate workflow definitions independently

---

## Implementation Plan

### Phase 1: Generic Workflow Manager (2-3 days)

#### 1.1 WorkflowDefinition Types
**File**: `lib/sop/ai/agent/codingworkflow/workflow_definition.go`

```go
package codingworkflow

// WorkflowDefinition represents a KB-stored workflow
type WorkflowDefinition struct {
    WorkflowID      string                 `json:"workflow_id"`
    Version         string                 `json:"version"`
    TriggerPatterns []string               `json:"trigger_patterns"`
    Steps           []WorkflowStep         `json:"steps"`
    ActorLimits     map[string]int         `json:"actor_limits"`
    SuccessCriteria SuccessCriteria        `json:"success_criteria"`
    FailureRecovery FailureRecovery        `json:"failure_recovery"`
}

type WorkflowStep struct {
    Step           int      `json:"step"`
    Actor          string   `json:"actor"`
    Priority       int      `json:"priority"`
    QueryTemplate  string   `json:"query_template"`
    ToolsExpected  []string `json:"tools_expected"`
    DependsOn      []string `json:"depends_on"`
    Condition      string   `json:"condition"`      // Named condition
    MaxIterations  int      `json:"max_iterations"`
    LoopTo         string   `json:"loop_to"`
}

type SuccessCriteria struct {
    RequiredToolsUsed []string `json:"required_tools_used"`
    FinalState        string   `json:"final_state"`
}

type FailureRecovery struct {
    MaxTurns      int    `json:"max_turns"`
    TimeoutPerTurn string `json:"timeout_per_turn"`
    OnTimeout     string `json:"on_timeout"`
}
```

#### 1.2 WorkflowManager Implementation
**File**: `lib/sop/ai/agent/codingworkflow/workflow_manager.go`

```go
package codingworkflow

type WorkflowManager struct {
    kb              KnowledgeBaseRetriever
    conditionRegistry map[string]ConditionFunc
}

// ConditionFunc evaluates whether a work item should execute
type ConditionFunc func(result TurnResult) bool

func NewWorkflowManager(kb KnowledgeBaseRetriever) *WorkflowManager {
    wm := &WorkflowManager{
        kb: kb,
        conditionRegistry: make(map[string]ConditionFunc),
    }
    wm.registerBuiltinConditions()
    return wm
}

// GetWorkflow retrieves a matching workflow from KB
func (wm *WorkflowManager) GetWorkflow(ctx context.Context, query string, state RoutingState) (*WorkflowDefinition, error) {
    // Step 1: Classify task type (debugging, implementation, etc.)
    taskType := classifyTask(query)
    
    // Step 2: Build category path
    categoryPath := fmt.Sprintf("Workflows/%s", taskType)
    
    // Step 3: Search KB with hybrid approach
    results, err := wm.kb.SearchByPath(ctx, []PathSearchParam{{
        CategoryPath: categoryPath,
        SearchText: query,
    }})
    
    if err != nil || len(results) == 0 {
        return nil, fmt.Errorf("no matching workflow found")
    }
    
    // Step 4: Parse workflow definition from KB data
    var workflow WorkflowDefinition
    if err := json.Unmarshal(results[0].Data, &workflow); err != nil {
        return nil, fmt.Errorf("invalid workflow definition: %w", err)
    }
    
    return &workflow, nil
}

// Execute executes a workflow by injecting work items into queue
func (wm *WorkflowManager) Execute(ctx context.Context, workflow *WorkflowDefinition, queue WorkQueue, originalQuery string) error {
    // Step 1: Apply actor limits
    for actor, limit := range workflow.ActorLimits {
        queue.SetMaxPerActor(actor, limit)
    }
    
    // Step 2: Inject work items for each step
    for _, step := range workflow.Steps {
        // Expand query template
        query := expandTemplate(step.QueryTemplate, map[string]string{
            "original_query": originalQuery,
        })
        
        // Build work item
        item := map[string]interface{}{
            "query":    query,
            "actor":    step.Actor,
            "priority": step.Priority,
        }
        
        // Add condition if specified
        if step.Condition != "" {
            condFunc, ok := wm.conditionRegistry[step.Condition]
            if ok {
                item["condition"] = condFunc
            }
        }
        
        // Enqueue
        if err := queue.Enqueue(item); err != nil {
            return fmt.Errorf("failed to enqueue step %d: %w", step.Step, err)
        }
    }
    
    return nil
}

// registerBuiltinConditions registers common condition functions
func (wm *WorkflowManager) registerBuiltinConditions() {
    wm.conditionRegistry["has_failure_indicators"] = func(result TurnResult) bool {
        return containsFailureIndicators(result.Text)
    }
    
    wm.conditionRegistry["tests_passing"] = func(result TurnResult) bool {
        return !containsFailureIndicators(result.Text)
    }
    
    wm.conditionRegistry["tests_still_failing"] = func(result TurnResult) bool {
        return len(result.TestsFailed) > 0
    }
}
```

#### 1.3 Capability Integration
**File**: `lib/sop/ai/agent/codingworkflow/capability.go` (modify)

```go
type Capability struct {
    Orchestrator    *Orchestrator
    WorkflowManager *WorkflowManager  // NEW
}

func (c *Capability) Orchestrate(ctx context.Context, state RoutingState) (bool, error) {
    queue, ok := getWorkQueue(ctx)
    if !ok {
        return false, fmt.Errorf("work queue not found")
    }
    
    query, ok := ctx.Value("multi_turn_query").(string)
    if !ok || query == "" {
        return false, fmt.Errorf("query not found")
    }
    
    // NEW: Use KB-driven workflow manager
    if c.WorkflowManager != nil {
        workflow, err := c.WorkflowManager.GetWorkflow(ctx, query, state)
        if err == nil && workflow != nil {
            return c.WorkflowManager.Execute(ctx, workflow, queue, query) == nil, nil
        }
        // Fallback to classification-based approach if no KB workflow found
    }
    
    // EXISTING: Classification-based workflows (fallback)
    classifier := DefaultTaskClassifier()
    classification := classifier.Classify(query, "", "")
    
    switch classification.TaskType {
    case TaskTypeDebugging:
        return c.orchestrateDebuggingWorkflow(queue, query)
    // ... other cases
    }
}
```

### Phase 2: KB Population with Workflows (1 day)

#### 2.1 Workflow Ingestion Tool
**File**: `lib/sop/ai/agent/codingworkflow/workflow_ingestion.go`

```go
// IngestWorkflow adds a workflow definition to KB
func IngestWorkflow(ctx context.Context, kb *memory.KnowledgeBase, workflow WorkflowDefinition) error {
    // Serialize workflow to JSON
    data, err := json.Marshal(workflow)
    if err != nil {
        return err
    }
    
    // Build category path from workflow ID
    categoryPath := fmt.Sprintf("Workflows/%s", inferCategoryFromID(workflow.WorkflowID))
    
    // Build search text from trigger patterns
    searchText := strings.Join(workflow.TriggerPatterns, " ")
    
    // Ingest into KB
    return kb.IngestThought(ctx, searchText, categoryPath, "system", nil, string(data))
}
```

#### 2.2 Workflow Library Creation
**File**: `lib/sop/ai/agent/codingworkflow/workflows/test_debugging_v1.json`

Store the JSON schema shown earlier as a file, then ingest:
```go
func SeedWorkflowKB(ctx context.Context, kb *memory.KnowledgeBase) error {
    workflows := []string{
        "workflows/test_debugging_v1.json",
        "workflows/feature_implementation_v1.json",
        "workflows/bug_investigation_v1.json",
        "workflows/refactoring_v1.json",
    }
    
    for _, path := range workflows {
        data, _ := os.ReadFile(path)
        var workflow WorkflowDefinition
        json.Unmarshal(data, &workflow)
        IngestWorkflow(ctx, kb, workflow)
    }
    return nil
}
```

### Phase 3: Testing & Validation (1 day)

#### 3.1 Unit Tests
```go
func TestWorkflowManager_GetWorkflow(t *testing.T) {
    // Test KB retrieval of workflow definition
}

func TestWorkflowManager_Execute(t *testing.T) {
    // Test queue injection from workflow steps
}

func TestWorkflowDefinition_JSONRoundTrip(t *testing.T) {
    // Test serialization/deserialization
}
```

#### 3.2 Integration Tests
```go
func TestEndToEnd_TestDebuggingWorkflow(t *testing.T) {
    // 1. Seed KB with test debugging workflow
    // 2. Trigger with "fix failing tests"
    // 3. Verify correct work items enqueued
    // 4. Validate actor limits applied
}
```

---

## Benefits Analysis

### 🚀 Immediate Benefits

1. **No Code Changes for Workflow Updates**
   - Edit JSON workflow definition
   - Re-ingest into KB
   - Instantly available to all agents

2. **Discoverability**
   - User asks: "How should I fix failing tests?"
   - KB returns: Test debugging workflow with 7 steps
   - LLM can explain the approach before executing

3. **Learning from History**
   - Track which workflows succeed/fail
   - Update workflows based on observed patterns
   - Add new workflows without touching code

4. **Semantic Workflow Matching**
   - "debug crash" → matches "Investigation/ErrorAnalysis"
   - "speed up code" → matches "Refactoring/PerformanceOptimization"
   - Handles variations naturally via vector search

5. **Multi-Language Support**
   - Chinese query "修复测试" → finds "TestDebugging" workflow
   - Cross-lingual matching via embeddings

### 📊 Long-Term Benefits

1. **Workflow Marketplace**
   - Share workflows between teams
   - Import community-contributed workflows
   - Rate and review workflows

2. **A/B Testing**
   - Run workflow v1 vs v2
   - Measure success rates
   - Promote better version automatically

3. **Adaptive Workflows**
   - Analyze TurnResult history
   - Identify common failure patterns
   - Suggest workflow improvements

4. **Tool Recommendation**
   - Workflow specifies expected tools per step
   - If tool missing, suggest installation
   - If tool fails, suggest alternatives

5. **Workflow Composition**
   - "Implement feature + write tests + deploy"
   - Chain 3 workflows together
   - Conditional branching between workflows

---

## Comparison: Current vs Proposed

| Aspect | Current (Hardcoded) | Proposed (KB-Driven) |
|--------|---------------------|----------------------|
| **Add new workflow** | Write Go code, recompile | Create JSON, ingest to KB |
| **Update workflow** | Code change + deploy | Update KB entry |
| **Workflow discovery** | Not possible | "Show me test fix workflows" |
| **Multi-language** | English only | Cross-lingual via embeddings |
| **Version control** | Git history | KB versioning + Git |
| **A/B testing** | Deploy separate builds | Query v1 or v2 from KB |
| **Learning** | Manual analysis | Automated pattern detection |
| **Sharing** | Copy code files | Export/import KB entries |
| **Runtime inspection** | Debug logs | Query KB for active workflow |
| **Tool validation** | Manual code review | Workflow schema validation |

---

## Risk Mitigation

### Risk 1: KB Unavailable
**Mitigation**: Fallback to hardcoded workflows (current approach)
```go
workflow, err := wm.GetWorkflow(ctx, query, state)
if err != nil {
    // Fallback to classification-based approach
    return c.orchestrateTestFixWorkflow(queue, query)
}
```

### Risk 2: Invalid Workflow JSON
**Mitigation**: Schema validation on ingestion + runtime validation
```go
func ValidateWorkflow(workflow WorkflowDefinition) error {
    if workflow.WorkflowID == "" {
        return fmt.Errorf("workflow_id required")
    }
    for _, step := range workflow.Steps {
        if step.Actor == "" {
            return fmt.Errorf("step %d missing actor", step.Step)
        }
    }
    return nil
}
```

### Risk 3: Performance Overhead
**Mitigation**: Cache workflow definitions in memory
```go
type WorkflowManager struct {
    kb    KnowledgeBaseRetriever
    cache map[string]*WorkflowDefinition  // In-memory cache
}
```

### Risk 4: Security (Malicious Workflows)
**Mitigation**: Workflow sandboxing + tool allowlist
```go
func (wm *WorkflowManager) ValidateSecurity(workflow WorkflowDefinition) error {
    for _, step := range workflow.Steps {
        for _, tool := range step.ToolsExpected {
            if !isAllowedTool(tool) {
                return fmt.Errorf("tool %s not allowed", tool)
            }
        }
    }
    return nil
}
```

---

## Success Metrics

### Implementation Success
- ✅ WorkflowManager can retrieve workflows from KB
- ✅ Execute() correctly injects work items into queue
- ✅ Condition evaluation works correctly
- ✅ Actor limits applied as specified
- ✅ All existing tests still pass

### Functional Success
- ✅ "Fix failing tests" triggers KB workflow (not hardcoded)
- ✅ Workflow update without code change takes effect
- ✅ Query "how to debug tests" returns workflow definition
- ✅ Cross-lingual matching works (e.g., Chinese → English workflow)

### Quality Success
- ✅ Test debugging success rate ≥ Phase 1.5 baseline
- ✅ Workflow execution time within 10% of hardcoded version
- ✅ KB query latency < 100ms (cached)
- ✅ Zero workflow definition errors in production

---

## Recommendation

### ✅ **PROCEED with KB-Driven Workflow Approach**

**Rationale**:
1. All required infrastructure exists (KB, Queue, Tools)
2. Implementation effort is moderate (2-3 days)
3. Benefits are transformative (data-driven, learnable, discoverable)
4. Risks are manageable with fallback strategy
5. Aligns with SOP's knowledge-centric architecture

**Phased Rollout**:
1. **Week 1**: Implement WorkflowManager + basic KB integration
2. **Week 2**: Migrate test debugging workflow to KB format
3. **Week 3**: Add 3-4 more workflows (implementation, investigation, refactoring)
4. **Week 4**: Production validation + performance tuning

**Next Immediate Steps**:
1. ✅ Approve this proposal
2. Create `workflow_definition.go` with types
3. Implement `WorkflowManager.GetWorkflow()`
4. Convert existing test debugging workflow to JSON
5. Write unit tests for workflow retrieval + execution
6. Run integration test comparing hardcoded vs KB-driven

---

## Appendix: Example Workflows

### A.1 Feature Implementation Workflow
```json
{
  "workflow_id": "feature-implementation-v1",
  "category_path": "Workflows/Implementation/FeatureDevelopment",
  "trigger_patterns": ["implement.*feature", "add.*functionality", "build.*component"],
  "steps": [
    {"step": 1, "actor": "implementation.analyzer", "priority": 100, "query_template": "Analyze requirements: {original_query}"},
    {"step": 2, "actor": "implementation.designer", "priority": 90, "query_template": "Design approach and identify files to modify"},
    {"step": 3, "actor": "implementation.coder", "priority": 80, "query_template": "Implement feature based on design"},
    {"step": 4, "actor": "implementation.tester", "priority": 70, "query_template": "Write tests for new functionality"},
    {"step": 5, "actor": "implementation.verifier", "priority": 60, "query_template": "Run tests and verify implementation"}
  ]
}
```

### A.2 Bug Investigation Workflow
```json
{
  "workflow_id": "bug-investigation-v1",
  "category_path": "Workflows/Investigation/ErrorAnalysis",
  "trigger_patterns": ["investigate.*bug", "debug.*crash", "analyze.*error"],
  "steps": [
    {"step": 1, "actor": "investigation.reproducer", "priority": 100, "query_template": "Reproduce the bug: {original_query}"},
    {"step": 2, "actor": "investigation.analyzer", "priority": 90, "query_template": "Analyze error logs and stack traces"},
    {"step": 3, "actor": "investigation.tracer", "priority": 80, "query_template": "Trace execution path to root cause"},
    {"step": 4, "actor": "investigation.reporter", "priority": 70, "query_template": "Document findings and suggest fix"}
  ]
}
```

---

**Prepared by**: GitHub Copilot (Claude Sonnet 4.5)  
**Review requested from**: User  
**Dependencies**: Phase 2 test fixes (paused), existing KB/Queue/Tools infrastructure  
**Estimated Timeline**: 2-3 days implementation + 1 week validation
