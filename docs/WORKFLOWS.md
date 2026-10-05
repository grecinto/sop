# Northstar AI Workflows - Complete Documentation

**Last Updated**: 2026-07-28  
**Status**: ✅ Capability-Based Coding Workflow Services + Participant Pattern  
**Post-V1 Vision**: Data-Driven Workflows with Infrastructure Integration

**Recent Changes (2026-07-28)**:
- Added a Coding Workflow Services capability that exposes reusable coding-workflow helpers through the capability contract
- Kept Ask/ReAct as the authoritative execution loop while allowing coding-specific primitives to participate lightly
- Implemented participant pattern for workflow execution and workflow setup hooks
- Added Dynamic Planner capability with structured guidance and phase-planning support
- Refactored WorkflowManagerCapability for single-step enqueueing
- Deprecated controller-style execution entry points for the capability layer
- Achieved 100% Ask pipeline reuse for workflow steps and structured coding assistance
- Note: `WORKFLOW_CAPABILITY_REFACTORING.md` has been superseded by this document's Architecture Evolution section

---

## Table of Contents

1. [V1 Status & Direction](#v1-status--direction)
2. [Post-V1 Vision](#post-v1-vision)
3. [Architecture & Implementation](#architecture--implementation)
4. [Migration Path](#migration-path)

---

## V1 Status & Direction

### Current State: Dual Workflow System (Code-Driven + AI-Generated)

**Strategic Evolution**: V1 now ships with **two complementary workflow approaches**, both executing through the unified Ask pipeline:

1. **Code-Driven Capabilities** - Proven, infrastructure-backed workflows
2. **Dynamic Planner** - AI-generated workflows tailored to specific tasks

**Why This Hybrid Approach:**
1. **Infrastructure Power**: Code-driven workflows leverage AST parsing, tree-sitter integration, patch engines, and test runners
2. **Adaptive Intelligence**: Dynamic Planner generates custom workflows for unique or complex scenarios
3. **Best of Both Worlds**: Proven templates for common tasks, AI flexibility for novel situations
4. **Unified Execution**: Both approaches use 100% Ask pipeline with provider continuity

**V1 Capabilities**:
- ✅ **Dynamic Planner**: AI-generated workflows with OnFinish hook pattern
- ✅ **KB Workflows**: Template-based workflows from knowledge bases (participant pattern)
- ✅ **Test Fix Workflow**: 7-step orchestrated process (analyze → isolate → patch → validate → test → iterate → document)
- ✅ **Architecture Workflow**: 5-step design process (analyze → map dependencies → design → validate → document)
- ✅ **Documentation Workflow**: 4-step generation process (analyze → extract structure → generate → review)
- ✅ **Implementation Workflow**: 3-step build process (plan → implement → validate)
- ✅ **Desktop Automation**: Shell/git/postgres command execution with PTY persistence
- 🚀 **Workflow Undo System**: Multi-dimensional semantic undo with time-ranked search (see [WORKFLOW_UNDO_SYSTEM.md](WORKFLOW_UNDO_SYSTEM.md))

**Routing Optimization**: 80%+ of queries use keyword-based fast path (zero LLM calls), falling back to LLM only for ambiguous queries.

---

## Architecture Evolution (2026-07-27 & 2026-07-28)

### Participant Pattern Implementation (July 2026)

The workflow stack has evolved to a **pure participant pattern** where capabilities are passive responders and the Ask pipeline maintains complete control of execution flow.

**Key Architectural Principles:**
- **Ask Pipeline is the Controller**: The Ask/ReAct loop is the single authoritative execution engine
- **Capabilities are Participants**: Workflows respond to requests but never control flow
- **100% Pipeline Reuse**: Every workflow step executes through the full Ask pipeline with provider reasoning
- **OnFinish Hook Pattern**: Post-execution hooks enable workflow setup without bypassing the pipeline

### Two Workflow Approaches (Both Participant Pattern)

#### 1. Phase Capabilities (Comprehensive Workflow API)

Phase capabilities expose the full coding workflow API through phase-specific tools:
- `investigation_build_context`, `planning_build_context` - Repository context building
- `investigation_inspect_repo`, `review_inspect_repo` - Repository inspection
- `investigation_search_repo`, `planning_search_repo` - Code search
- `execution_preview_patch`, `execution_apply_patch` - Patch operations
- `execution_run_tests`, `review_run_tests` - Test execution

**Key Characteristics:**
- **Phase-aware participation**: Each phase exposes relevant tools for that stage
- **Higher priority**: Phase capabilities win classification with scores 14-15
- **Complete API**: All coding workflow primitives available through phase tools
- **Better organization**: Tool names indicate phase context (investigation_*, planning_*, execution_*, etc.)

**Key Components:**
- **PhaseCapability** (`lib/sop/ai/agent/planner/phase_capabilities.go`)
  - Implements the capability interface for Ask integration
  - Exposes phase-specific tools from shared infrastructure
  - Manages phase state, transitions, and artifacts

#### 2. Dynamic Planner (AI-Generated Workflows)

The **PlannerCapability** generates custom workflows based on task analysis:

```
User Query
    ↓
Ask Pipeline Routing → PlannerCapability selected
    ↓
PlannerCapability.Run()
    ├─ Analyzes task type (debugging, implementation, testing, etc.)
    ├─ Generates WorkflowPlan with phases
    ├─ Returns CapabilityResult with plan in Data
    └─ Marks as "workflow_ready"
    ↓
Ask/ReAct Loop Completes (provider handles conversation)
    ↓
OnFinish() Hook Called (Post-Execution)
    ├─ Extract WorkflowPlan from result.Data
    ├─ Convert phases to concrete Steps via planToSteps()
    ├─ Store steps in result.Data["workflow_steps_ready"]
    └─ Mark "workflow_trigger_needed" = true
    ↓
Ask Pipeline Detects Trigger Needed
    ├─ Creates WorkItem with actor="workflow_manager"
    └─ Enqueues to WorkQueue
    ↓
Next Ask Iteration Processes Workflow Steps
    └─ Each step executes through full Ask pipeline
```

**Key Components:**
- **PlannerCapability** (`lib/sop/ai/agent/planner/planner.go`)
  - Implements the standard capability interface and contributes structured guidance for planning
  - Generates phase plans using task classification and heuristics
  - Converts plans to executable execution guidance for later phases
  
- **WorkflowPlanner Interface** (`lib/sop/ai/agent/capabilities/capability.go`)
  ```go
  type WorkflowPlanner interface {
      Capability
      OnFinish(ctx context.Context, result CapabilityResult) error
  }
  ```

- **Step Conversion** - Reuses coding workflow public functions:
  - `NewAnalyzeCodeStep()` - Code analysis and context gathering
  - `NewGeneratePatchStep()` - Generate code changes
  - `NewApplyFixStep()` - Apply patches to files
  - `NewVerifyFixStep()` - Run tests and verify
  - `NewDesignStep()` - Architecture and design work

#### 3. Knowledge Base Workflows (Pre-Defined Templates)

**WorkflowManager** handles KB-driven workflows using the same participant pattern:

```
User Query
    ↓
tryKBWorkflowExecution detects matching workflow
    ↓
WorkflowManagerCapability.RunWithQueue()
    ├─ Initializes workflow state
    ├─ Sets currentStepIndex = 0
    └─ Enqueues FIRST STEP ONLY ← Participant Pattern
    ↓
processQueueLoop processes steps
    ↓
For each step completion:
    ├─ Detects workflow_step in context
    ├─ Calls AdvanceStep(result) if successful
    └─ Enqueues NEXT STEP ONLY ← One at a time
    ↓
Workflow completes naturally
```

**Key Components:**
- **WorkflowManagerCapability** (`lib/sop/ai/agent/workflow_capability.go`)
  - Wraps WorkflowManager as a standard Capability
  - Tracks state: current workflow, step index, turn results
  - `RunWithQueue()` - Enqueues single step (participant pattern)
  - `AdvanceStep()` - Advances after successful completion
  
- **WorkflowManager** (`lib/sop/ai/agent/workflow_manager.go`)
  - `GetWorkflow()` - Retrieves workflow from KB via semantic search
  - `EnqueueNextStep()` - Enqueues one step at a time
  - Deprecated: `Execute()` - Old controller pattern (kept for compatibility)

### Participant vs Controller Pattern

**❌ Controller Pattern (Anti-Pattern - Removed):**
```
Capability.Orchestrate() {
    for each step {
        queue.Enqueue(step)  // Enqueues ALL steps upfront
    }
}
→ Capability controls execution flow
→ Bypasses Ask pipeline for step execution
→ Loses provider continuity and memory
```

**✅ Participant Pattern (Current):**
```
Capability.Run() {
    // Generate plan/result
    return CapabilityResult
}

Capability.OnFinish(result) {
    // Set up workflow trigger
    result.Data["workflow_steps_ready"] = steps
    result.Data["workflow_trigger_needed"] = true
}

Ask Pipeline {
    → Calls Run()
    → Calls OnFinish() after reasoning
    → Detects workflow_trigger_needed
    → Enqueues trigger to queue
    → Processes queue naturally
    → Each step goes through full Ask iteration
}
```

### Benefits of Participant Pattern

1. **100% Ask Pipeline Reuse**: Every workflow step inherits:
   - Provider conversation continuity (Gemini, Claude, GPT-4)
   - Memory integration (short-term + long-term)
   - Tool framework with authorization
   - Observable execution with metrics
   - Battle-tested ReAct loops

2. **Adaptive Execution**:
   - Steps execute one at a time
   - Workflow adapts based on intermediate results
   - Conditions evaluated after execution
   - Natural error handling and recovery

3. **Architectural Integrity**:
   - Ask pipeline remains single source of control
   - No separate execution engine needed
   - Capabilities are passive plugins
   - Consistent with all other capabilities

4. **Developer Experience**:
   - Clear separation of concerns
   - Easy to debug (single execution path)
   - Maintainable and extensible
   - Reusable workflow components

### Implementation Timeline

- **2026-07-24**: Queue-based multi-turn model evaluated
- **2026-07-27**: Workflow Undo System implemented with multi-dimensional indexing
- **2026-07-28**: Participant pattern implemented
  - Dynamic Planner with OnFinish hook
  - WorkflowManagerCapability refactoring
  - Controller pattern deprecated
  - 100% Ask pipeline reuse achieved

This consolidates the earlier notes on queue refactoring, transaction boundaries, lifecycle assessment, and workflow-manager design into a coherent participant pattern architecture.

### Workflow Undo System (2026-07-27)

A **premium feature** that treats each workflow run as an atomic transaction with intelligent search and restore capabilities.

**Key Innovation**: Multi-dimensional indexing with Space-backed semantic search:
- **Date dimension**: Exact temporal filtering (fast metadata lookups)
- **Code artifact dimension**: Semantic search by application component (AST-extracted, vectorized)
- **Time-ranked results**: Hybrid scoring combines recency (30%) + relevance (70%)

**Architecture**:
```
Storage Layers:
├─ Space: Multi-path categories (date + code artifacts), vectorized semantic search
├─ Filesystem: File backups with SHA256 verification
└─ Index: Fast temporal lookups

Query Flow:
User query → Semantic search in Space → Date filtering → Hybrid time ranking → Results
```

**Example Queries**:
- `sop workflow find "authentication middleware"` → Recent auth changes with semantic ranking
- `sop workflow find "parser fixes" --after 2026-07-20` → Date + semantic combined
- `sop workflow restore txn_xxx --dry-run` → Preview before restore

**Value Proposition**:
- vs. Git: No history pollution, workflow-scoped transactions, semantic search
- vs. IDE undo: Application-domain aware, multi-day history, natural language queries
- vs. Enterprise systems: Simple snapshot-based, no compensation logic, developer UX

See complete documentation: [WORKFLOW_UNDO_SYSTEM.md](WORKFLOW_UNDO_SYSTEM.md)

---

## Post-V1 Vision

### Vision: Data-Driven Workflows WITH Infrastructure Integration

**Post-V1 Goal**: Enable **visual, data-driven workflow authoring** while preserving the power of code-driven infrastructure.

**The Hybrid Model**:

```
Workflow Definition (JSON/KB)          Infrastructure Layer (Go Code)
├─ Visual editing                      ├─ AST parsing (tree-sitter)
├─ User customizable                   ├─ Patch engine
├─ Space-specific                      ├─ Test runner
├─ Trigger patterns                    ├─ Context manager
└─ Step sequences                      └─ Validation orchestrator
         ↓                                      ↓
         └───────── Integration ────────────────┘
                         ↓
           Rich Visual/Data-Driven Workflows
           with Code-Level Tool Integration
```

**What This Enables**:
1. **Visual Workflow Builder**: Users can define custom workflows in UI/KB
2. **Tool Integration**: Workflows can invoke coding infrastructure tools (analyze_ast, generate_patch, run_tests)
3. **Best of Both Worlds**: Flexibility of data-driven + power of infrastructure
4. **Team Customization**: Organizations can define team-specific workflows that leverage proven tools

**Example Post-V1 Workflow**:
```json
{
  "id": "custom-api-generation",
  "name": "Generate REST API Endpoint",
  "steps": [
    {"tool": "analyze_existing_patterns", "type": "infrastructure"},
    {"tool": "generate_code_with_ast", "type": "infrastructure"},
    {"tool": "apply_patch", "type": "infrastructure"},
    {"tool": "run_tests", "type": "infrastructure"},
    {"prompt": "Document the new endpoint", "type": "agent"}
  ]
}
```

### Core Playbook as Workflow Standard Library

**Strategy**: Core Playbook Space becomes the **shipping vehicle** for both curated workflows and workflow infrastructure documentation.

```
Core Playbook Space (Read-Only Internal Space)
├── Categories/
│   ├── Go Best Practices/          ← Existing documentation
│   ├── Architecture Guidelines/    ← Existing documentation
│   └── Playbooks/                  ← NEW: Physical workflow definitions
│       ├── Coding/
│       │   ├── Debug Test Failure
│       │   ├── Add Unit Tests
│       │   ├── Refactor Code
│       │   └── Generate Docs
│       ├── Testing/
│       │   ├── Run Test Suite
│       │   ├── Coverage Analysis
│       │   └── Performance Tests
│       └── Deployment/
│           ├── Safe Production Release
│           └── Rollback Procedure
```

**Key Point**: Workflows are **data**, not code. They live in the KB, version-controlled, and ship with every installation.

### Value Proposition

#### **Before** (Current State)
❌ User asks: *"fix failing tests"*  
❌ Copilot improvises based on system prompt  
❌ Inconsistent results, no repeatable process  
❌ User has to guide every step manually  

#### **After** (With Workflows)
✅ User asks: *"fix failing tests"*  
✅ Copilot detects → "Debug Test Failure" workflow  
✅ Executes 4-step proven process automatically  
✅ Consistent, repeatable, high-quality results

---

## Architecture & Implementation

### V1 Architecture: Code-Driven Capabilities

**Current System** (`lib/sop/ai/agent/codingworkflow/`):

```
Query: "fix failing tests"
  ↓
Keyword Routing (Fast Path - No LLM)
  ↓
Coding Workflow Capability Selected
  ↓
Task Classifier (Keyword-Based)
  ↓
TaskType = Debugging
  ↓
orchestrateTestFixWorkflow() [7 steps]
  ↓
Multi-Turn Queue Execution
  1. Analyze test failures (TestRunner)
  2. Isolate failing cases (AST analysis)
  3. Generate patches (PatchEngine)
  4. Validate syntax (tree-sitter)
  5. Run tests (TestRunner)
  6. Iterate if needed
  7. Document resolution
```

**Key Components** (All Infrastructure-Backed):
1. **ContextManager**: Builds rich context with AST, imports, dependencies
2. **PatchEngine**: Generates and applies precise code patches
3. **TestRunner**: Executes tests, parses output, tracks coverage
4. **GrammarRegistry**: Tree-sitter AST parsing for all languages
5. **ValidationOrchestrator**: Enforces KB quality standards
6. **RepoIndexer**: Maintains searchable repository index

**Routing Optimization**:
- **Fast Path** (80%+ queries): Keyword matching → Zero LLM calls → Instant routing
- **Slow Path** (ambiguous only): LLM classification → Fallback for complex queries

### Post-V1 Architecture: Infrastructure-Backed Data-Driven Workflows

### Follow KB/Space Loading Pattern

Workflows follow the **EXACT same pattern** as KB/Space loading:

```
┌─────────────────────────────────────────────────────────────────┐
│                  WORKFLOW LIFECYCLE                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  1. SOURCE FILES (Version Controlled)                            │
│     workflows/*.json → Workflow definitions                      │
│                                                                   │
│  2. PRELOAD (At Startup)                                        │
│     space_manager.go::ingestWorkflowsIntoKB()                   │
│     ├─ User creates "Core Playbook" space                       │
│     ├─ System loads sop_base_knowledge.json                     │
│     └─ Auto-ingests workflows/ directory into KB                │
│                                                                   │
│  3. RUNTIME ACCESS                                              │
│     WorkflowManager searches KB for workflows                    │
│     ├─ Semantic search (embedding-based)                        │
│     ├─ Trigger pattern matching                                 │
│     └─ Category path lookup                                     │
│                                                                   │
│  4. EXECUTION                                                   │
│     WorkQueue orchestration                                      │
│     ├─ Multi-turn step execution                                │
│     ├─ Actor-based routing                                      │
│     └─ Condition evaluation                                     │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

### Current System Architecture

```
User Query → tryDetectWorkflow() → WorkflowManager.GetWorkflow()
                                    ↓
                                KB Search (semantic + path)
                                    ↓
                                WorkflowDefinition
                                    ↓
                                WorkQueue Execution
                                    ↓
                                Multi-turn Results
```

**Key Components**:
1. **WorkflowManager** (`lib/sop/ai/agent/workflow_manager.go`)
   - Retrieves workflows from KB via semantic search or category path
   - Executes workflows by injecting steps into WorkQueue
   - Condition registry for conditional step execution

2. **WorkflowDefinition** (`lib/sop/ai/agent/workflow_types.go`)
   - Structured workflow schema with steps, actor limits, metadata
   - Trigger patterns for automatic matching
   - Protocol guidance for LLM behavior

3. **Integration Points** (`lib/sop/ai/agent/copilot.go`)
   - `tryDetectWorkflow()` - Check if query matches any workflow
   - `tryKBWorkflowExecution()` - Execute matched workflow
   - Lazy KB initialization on first workflow access

---

## Implementation Status

### ✅ Phase 1 Complete - Multi-KB Workflow Search

**Summary**: Workflow system is now fully wired and operational. Users can type "fix failing tests" and the system will search Core Playbook and selected Spaces for matching workflows.

**Files Modified:**
- `lib/sop/ai/agent/workflow_manager.go` - Added `selectedKBs` field and multi-KB search
- `lib/sop/ai/agent/copilot.go` - Enhanced workflow detection to search Core Playbook + selected Spaces
- `lib/sop/tools/sopbackend/space_manager.go` - Added workflow ingestion during Space loading

**Architecture:**
```
User Query: "fix failing tests"
    ↓
WorkflowManager searches:
  1. Core Playbook (always)        → "test-debugging-go-v1"
  2. Selected Space: "Team Guide"  → "team-specific-debug"
    ↓
Best match selected via semantic similarity
    ↓
Workflow executed via WorkQueue
```

**Post-V1 Architecture**:

```
User Query OR Visual Workflow Builder
  ↓
Workflow Definition (JSON in KB)
  {
    "steps": [
      {"tool": "analyze_ast", "params": {...}},
      {"tool": "generate_patch", "params": {...}},
      {"prompt": "Custom LLM step", "context": [...]}
    ]
  }
  ↓
Workflow Execution Engine
  ├─ Infrastructure Tools (Go functions)
  │  ├─ analyze_ast()
  │  ├─ generate_patch()
  │  ├─ run_tests()
  │  └─ validate_syntax()
  ├─ Agent Steps (LLM prompts)
  │  └─ Custom reasoning/generation
  └─ Hybrid Steps (Tool + Agent)
  ↓
Multi-Turn Queue Execution
  ↓
Results + Traceability
```

**Key Innovation**: Workflows become **orchestration scripts** that call infrastructure tools as first-class steps, not just text prompts.

---

## Migration Path

### For V1 → Post-V1 Transition

**What Stays**:
- ✅ All infrastructure tools (AST, patches, tests)
- ✅ Multi-turn queue orchestration
- ✅ Capability registry and routing
- ✅ Fast-path keyword optimization

**What Changes**:
- 📝 Workflow definitions move from code → KB (JSON)
- 📝 Add tool registry for infrastructure functions
- 📝 Enable visual workflow builder UI
- 📝 Re-enable `kbWorkflowsEnabled` feature flag

**Backward Compatibility**:
- Code-driven capabilities continue to work as fallback
- Existing workflows (test fix, architecture) become templates
- Users can choose: use curated workflows OR define custom ones

**Timeline**:
- **V1**: Ship with proven, code-driven capabilities
- **V1.1+**: Gradually expose infrastructure tools to workflow system
- **V2**: Full visual workflow builder with tool integration

---

## Technical Implementation Notes

### Current KB Workflow Infrastructure (Disabled in V1)

**Location**: `workflows/test_debugging_go_v1.json`

**Ingestion Flow:**
1. User creates/preloads Core Playbook Space
2. `runIngestImportSpace()` or `runIngestSpace()` executes
3. After KB populated with `sop_base_knowledge.json`
4. `ingestWorkflowsIntoKB()` loads all workflows from `workflows/` directory
5. Workflows stored in KB with embeddings for semantic search

**Helper Function** (`space_manager.go`):
```go
func ingestWorkflowsIntoKB(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], 
    onProgress func(progress int, total int, msg string)) error {
    
    workflowDir := "workflows"
    files, err := os.ReadDir(workflowDir)
    if err != nil {
        return fmt.Errorf("read workflow directory: %w", err)
    }
    
    for i, file := range files {
        if !strings.HasSuffix(file.Name(), ".json") {
            continue
        }
        
        data, err := os.ReadFile(filepath.Join(workflowDir, file.Name()))
        if err != nil {
            continue
        }
        
        var workflow agent.WorkflowDefinition
        if err := json.Unmarshal(data, &workflow); err != nil {
            continue
        }
        
        // Store workflow in KB
        thought := memory.Thought[map[string]any]{
            CategoryPath: workflow.CategoryPath,
            Data: map[string]any{
                "workflow_id": workflow.ID,
                "name": workflow.Name,
                "description": workflow.Description,
                "steps": workflow.Steps,
                "trigger_patterns": workflow.TriggerPatterns,
                // ... full workflow data
            },
            Summaries: []string{workflow.Description},
        }
        
        if err := kb.IngestThoughts(ctx, []memory.Thought[map[string]any]{thought}, "system"); err != nil {
            return fmt.Errorf("ingest workflow %s: %w", workflow.Name, err)
        }
        
        if onProgress != nil {
            onProgress(i+1, len(files), fmt.Sprintf("Ingested workflow: %s", workflow.Name))
        }
    }
    
    return nil
}
```

### Example Workflow: Test Debugging

**File**: `workflows/test_debugging_go_v1.json`

```json
{
  "id": "test-debugging-go-v1",
  "name": "Debug Test Failure",
  "description": "Systematically diagnose and fix failing Go unit tests",
  "category_path": "Playbooks/Testing/Debug",
  "version": "1.0",
  "trigger_patterns": [
    "fix failing tests",
    "test debug",
    "why is test failing",
    "debug test failure"
  ],
  "steps": [
    {
      "step": 1,
      "title": "Run Tests",
      "actor": "test_runner",
      "query_template": "Run the failing test suite",
      "tools_expected": ["execute_local_command", "run_repo_tests"],
      "success_criteria": "Test output captured with full error details"
    },
    {
      "step": 2,
      "title": "Analyze Failures",
      "actor": "coding_workflow",
      "query_template": "Analyze test failure output and identify root cause",
      "tools_expected": ["search_repo_files", "read_file", "grep_repo_text"]
    },
    {
      "step": 3,
      "title": "Generate Fix",
      "actor": "coding_workflow",
      "query_template": "Generate patch to fix identified issue",
      "tools_expected": ["patch_file", "write_file"]
    },
    {
      "step": 4,
      "title": "Verify Fix",
      "actor": "test_runner",
      "query_template": "Re-run tests to verify fix",
      "tools_expected": ["run_repo_tests"],
      "success_criteria": "All tests passing"
    }
  ],
  "actor_limits": {
    "test_runner": 5,
    "coding_workflow": 8
  },
  "protocol_guidance": "NEVER skip verification step. ALWAYS run tests after applying fix."
}
```

---

## Runtime & Orchestration

### WorkQueue Execution Model

```go
// Workflow detection and execution (copilot.go)
func (a *CopilotAgent) tryDetectWorkflow(ctx context.Context, query string) (bool, error) {
    if a.workflowManager == nil {
        return false, nil
    }
    
    // Search Core Playbook + selected Spaces
    workflow, err := a.workflowManager.GetWorkflow(ctx, query)
    if err != nil || workflow == nil {
        return false, nil
    }
    
    // Execute workflow via queue
    return a.tryKBWorkflowExecution(ctx, workflow, query)
}

// Multi-turn workflow execution
func (a *CopilotAgent) tryKBWorkflowExecution(ctx context.Context, 
    workflow *WorkflowDefinition, userQuery string) (bool, error) {
    
    // Inject workflow steps into work queue
    for _, step := range workflow.Steps {
        // Evaluate conditions
        if !a.evaluateStepCondition(ctx, step) {
            continue
        }
        
        // Create work item
        workItem := &WorkItem{
            Query: a.renderQueryTemplate(step.QueryTemplate, ctx),
            Actor: step.Actor,
            MaxIterations: a.getActorLimit(workflow, step.Actor),
            ExpectedTools: step.ToolsExpected,
            Instructions: step.Instructions,
        }
        
        // Add to queue
        a.workQueue.Add(workItem)
    }
    
    return true, nil
}
```

### Condition Evaluation

```go
// Evaluate step conditions (e.g., "tests_still_failing == true")
func (a *CopilotAgent) evaluateStepCondition(ctx context.Context, step WorkflowStep) bool {
    if step.Condition == "" {
        return true // No condition, always execute
    }
    
    // Parse condition expression
    condition := a.workflowManager.conditions[step.Condition]
    if condition == nil {
        return true // Unknown condition, default to execute
    }
    
    // Evaluate against context
    return condition.Evaluate(ctx)
}
```

---

## Tool Specifications

### Proposed Workflow Management Tools

**Goal**: Enable users to create, manage, and execute workflows through natural conversation.

**Current State**:
- ✅ WorkflowManager implemented with KB-backed storage
- ✅ Queue-based execution engine operational  
- ✅ Workflow detection and execution integrated into `askWithQueue()`
- ❌ **No user-facing tools for workflow management**
- ❌ **No discoverability mechanism for available workflows**
- ❌ **No authoring assistance via chat**

**Proposed Solution**: Register 7 new workflow management tools.

### 1. **list_workflows** - Discovery

**Purpose**: Show all available workflows or filter by domain/trigger pattern.

**Example Queries**:
- *"What workflows are available?"*
- *"Show me all testing workflows"*
- *"Which workflow handles 'fix failing tests'?"*

**Output Format**:
```
📋 **Available Workflows** (12 found)

### Testing Workflows
1. **Debug Test Failure** (`Playbooks/Testing/Debug`)
   - Trigger: "fix failing tests", "test debug"
   - Steps: 4 (Run tests → Analyze → Fix → Verify)
   
2. **Add Unit Tests** (`Playbooks/Testing/AddCoverage`)
   - Trigger: "add tests for", "test coverage"
   - Steps: 3 (Analyze code → Generate tests → Run)
```

### 2. **get_workflow** - Inspection

**Purpose**: Show detailed workflow definition including all steps, conditions, and metadata.

**Example Query**: *"Show me the 'Debug Test Failure' workflow"*

**Output**: Complete workflow details with steps, actors, tools, and conditions.

### 3. **create_workflow** - Authoring

**Purpose**: Create a new workflow from natural language description or structured JSON.

**Two Modes**:

**Mode A: LLM-Assisted Generation**
```
User: "Create a workflow for debugging parser errors"

LLM: [Generates structured workflow automatically]
     [Presents for approval]
     [Stores in KB on confirmation]
```

**Mode B: Structured JSON** (Advanced users)
```json
{
  "name": "Debug Parser Error",
  "category_path": "Playbooks/Debugging/Parser",
  "trigger_patterns": ["parser fails", "fix parser bug"],
  "steps": [...]
}
```

### 4. **update_workflow** - Editing

Modify existing workflow (steps, triggers, metadata).

### 5. **delete_workflow** - Cleanup

Remove a workflow. Requires explicit confirmation.

### 6. **test_workflow** - Validation

Dry-run a workflow without executing side effects.

**Output**:
```
🧪 **Workflow Test: Debug Test Failure**

✅ **Match**: Workflow triggered successfully
   
📋 **Execution Plan** (4 steps):
Step 1: Run Tests → Expected tools: execute_local_command ✓
Step 2: Analyze Failures → Condition: tests_still_failing == true ✓
Step 3: Generate Fix → Expected tools: patch_file ✓
Step 4: Verify Fix → Condition: has_error_output == false ✓

⏱️ **Estimated Duration**: 3-5 minutes
💾 **Side Effects**: File modifications
```

### 7. **explain_workflow** - Documentation

Generate human-readable explanation with Mermaid diagrams.

---

## Migration & Best Practices

### Workflow Authoring Guidelines

1. **Category Organization**
   - Use hierarchical paths: `Playbooks/Testing/Debug`
   - Group by domain: Testing, Debugging, Deployment, CodeReview

2. **Trigger Patterns**
   - Include 3-5 natural language variations
   - Cover common user phrasings
   - Example: "fix failing tests", "test debug", "why is test failing"

3. **Step Design**
   - Keep steps focused (single responsibility)
   - 3-7 steps per workflow (optimal)
   - Include success criteria
   - Specify expected tools

4. **Actor Selection**
   - `test_runner` - Test execution
   - `coding_workflow` - Code analysis/modification
   - `test_debugger.repair` - Test-specific debugging
   - Set reasonable iteration limits (5-8 turns)

5. **Protocol Guidance**
   - Add critical instructions LLM must follow
   - Example: "NEVER skip verification step"

### Example Workflow Template

```json
{
  "id": "unique-id-v1",
  "name": "Workflow Name",
  "description": "Brief description of what this accomplishes",
  "category_path": "Playbooks/Domain/Subdomain",
  "version": "1.0",
  "trigger_patterns": [
    "user phrase 1",
    "user phrase 2",
    "user phrase 3"
  ],
  "steps": [
    {
      "step": 1,
      "title": "Step Name",
      "actor": "coding_workflow",
      "query_template": "Action with {{.variable}} placeholder",
      "tools_expected": ["tool1", "tool2"],
      "instructions": "Specific guidance for LLM",
      "success_criteria": "Definition of success",
      "condition": "optional_condition_key"
    }
  ],
  "actor_limits": {
    "coding_workflow": 8,
    "test_runner": 5
  },
  "protocol_guidance": "Critical instructions the LLM must follow"
}
```

### Migrating Existing Workflows

**From hard-coded capabilities**:
1. Extract workflow logic
2. Convert to WorkflowDefinition JSON
3. Place in `workflows/` directory
4. Will auto-ingest on next Core Playbook load

**From markdown documentation**:
1. Identify documented procedures
2. Structure as workflow steps
3. Add trigger patterns based on doc headings
4. Store as JSON in `workflows/`

---

## Success Metrics

### User Adoption
- **Target**: 50% of users create at least 1 custom workflow within 30 days
- **Measure**: Track `create_workflow` tool usage

### Workflow Effectiveness
- **Target**: 80% of triggered workflows complete successfully
- **Measure**: Track workflow execution success/failure rates

### Time Savings
- **Target**: 30% reduction in multi-step task completion time
- **Measure**: Compare workflow execution vs manual step-by-step

---

## Next Steps

### Immediate (Phase 2)
1. ✅ Multi-KB workflow search - **COMPLETE**
2. ⏳ Implement workflow management tools (7 tools)
3. ⏳ Add LLM-assisted workflow generation
4. ⏳ Enhance trigger pattern matching

### Future (Phase 3)
1. Workflow versioning system
2. Export/import workflows (sharing)
3. Advanced conditional logic (if/else, loops)
4. Workflow composition (sub-workflows)
5. Approval gates for destructive operations

---

## References

- `lib/sop/ai/agent/workflow_manager.go` - Core workflow engine
- `lib/sop/ai/agent/workflow_types.go` - Data structures
- `lib/sop/ai/agent/copilot.go` - Integration points
- `lib/sop/tools/sopbackend/space_manager.go` - Workflow ingestion
- `workflows/*.json` - Workflow source files

---

**Status Summary**:
- ✅ Workflow execution engine operational
- ✅ Multi-KB search implemented
- ✅ Automatic workflow ingestion from JSON
- ✅ Core Playbook integration complete
- ⏳ User-facing management tools pending
- ⏳ LLM-assisted authoring pending

**Total Implementation Effort**: 5-7 days remaining for Phase 2
