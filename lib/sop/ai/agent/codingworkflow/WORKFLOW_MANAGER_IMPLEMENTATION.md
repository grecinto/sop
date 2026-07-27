# Workflow Manager Implementation

## Overview

The Workflow Manager architecture enables clean separation of concerns for multi-step coding workflows. This implementation extracts reusable infrastructure from the coding workflow and provides a composable Step-based system.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      WorkflowManager                         │
│  • Orchestrates sequence of Steps                           │
│  • Injects SharedInfrastructure into each Step              │
│  • Tracks pass/fail for each Step                           │
│  • Handles retry logic (exponential backoff)                │
│  • Short-circuits on terminal failures                      │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ SetSharedInfrastructure()
                            ▼
        ┌───────────────────────────────────────┐
        │             Step (Interface)           │
        │  • One unit of work = one Ask txn     │
        │  • Stores infra reference as field    │
        │  • Implements lifecycle hooks         │
        │  • Accesses SharedInfrastructure      │
        └───────────────────────────────────────┘
                            │
                            │ Direct field access (s.infra)
                            ▼
        ┌───────────────────────────────────────┐
        │       SharedInfrastructure             │
        │  • AST operations (tree-sitter)       │
        │  • Patch operations                   │
        │  • Test operations                    │
        │  • Repository operations              │
        └───────────────────────────────────────┘
```

## Components

### 1. SharedInfrastructure (`shared_infrastructure.go`)

Wraps existing Orchestrator tools for reuse by Steps:

- **ASTOperations**: Tree-sitter parsing, symbol extraction, imports analysis
- **PatchOperations**: Patch application, diff preview
- **TestOperations**: Test execution, result parsing
- **RepoOperations**: Repository search, indexing, inspection
- **ContextPackOperations**: Context building, expertise enrichment

**Access Pattern:**
```go
func (s *MyStep) OnAfterToolResult(ctx, toolName, result, state) {
    // SharedInfrastructure injected directly into step
    astOps := s.infra.NewASTOperations()
    parser, root, err := astOps.ParseFile(path, content)
    
    // Use test operations
    testOps := s.infra.NewTestOperations()
    result, err := testOps.RunTests(task, state)
    
    return state, true, nil
}
```

### 2. WorkflowManager (`workflow_manager.go`)

Orchestrates Step execution with retry logic and failure handling:

**Core Methods:**
- `Execute(ctx, steps, task, state)` - Run a sequence of Steps
- `SetDefaultMaxRetry(n)` - Configure default retry count
- `SetRetryBackoff(duration)` - Configure backoff strategy

**Step Outcomes:**
- `StepOutcomeSuccess` - Step completed successfully, continue
- `StepOutcomeFailed` - Step failed permanently, retry if attempts remain
- `StepOutcomeRetry` - Explicit retry request
- `StepOutcomeNeedsClarify` - Needs user clarification
- `StepOutcomeShortCircuit` - Terminal failure, stop workflow immediately
- `StepOutcomeSkipped` - Step was skipped (condition not met)

**Retry Logic:**
- Exponential backoff: `baseBackoff * 2^(attempt-1)`
- Max retries configurable per-step via `StepMetadata.MaxRetries`
- Automatic retry on `StepOutcomeFailed` and `StepOutcomeRetry`

### 3. Step Interface & BaseStep (`step_capability.go`)

**Step Interface:**
```go
type Step interface {
    Metadata() StepMetadata
    SetSharedInfrastructure(infra *SharedInfrastructure)
    OnStart(ctx, query, state) error
    OnBeforePrompt(ctx, query, state) (string, error)
    OnAfterToolResult(ctx, toolName, result, state) (state, bool, error)
    OnAfterTurn(ctx, turnNumber, state) (bool, error)
    OnFinalize(ctx, state) (StepResult, error)
}
```

**BaseStep Implementation:**
Provides default implementations - subclasses override only what they need:
```go
type MyStep struct {
    BaseStep
    // Custom fields
}

func (s *MyStep) Metadata() StepMetadata {
    return StepMetadata{
        ID: "my_step",
        Name: "My Step",
        Description: "Does something",
        MaxRetries: 2,
        IsTerminal: false,
    }
}

func (s *MyStep) OnAfterToolResult(ctx, toolName, result, state) {
    // Custom logic using SharedInfrastructure
    // Infrastructure already injected as s.infra
    astOps := s.infra.NewASTOperations()
    // ...
    return state, true, nil
}
```

## Example: Test Fix Workflow

Complete example with 4 Steps:

```go
func ExampleTestFixWorkflow() {
    // 1. Create Orchestrator (existing infrastructure)
    orchestrator := &Orchestrator{
        GrammarRegistry: NewGrammarRegistry(),
        TestRunner: &DefaultTestRunner{},
        PatchEngine: &DefaultPatchEngine{},
        // ... other components
    }
    
    // 2. Create WorkflowManager
    wm := NewWorkflowManager(orchestrator)
    wm.SetDefaultMaxRetry(2)
    
    // 3. Define Steps
    steps := []Step{
        NewRunTestsStep("/repo", []string{"-v"}),
        NewAnalyzeErrorsStep(),
        NewApplyFixStep(&Patch{
            FilePath: "/repo/main.go",
            Description: "Fix test failure",
        }),
        NewVerifyFixStep("/repo"),  // Terminal step
    }
    
    // 4. Create Task
    task := Task{
        WorkingDir: "/repo",
        Description: "Fix test failures",
    }
    
    // 5. Execute Workflow
    ctx := context.Background()
    result, err := wm.Execute(ctx, steps, task, RunState{})
    
    // 6. Check Results
    if err != nil {
        log.Printf("Workflow failed: %v", err)
        log.Printf("Failed at step: %s", result.FailedStep)
    } else {
        log.Printf("Workflow succeeded: %s", result.Message)
        log.Printf("Completed steps: %v", result.CompletedSteps)
        log.Printf("Execution time: %v", result.ExecutionTime)
    }
}
```

## Provided Example Steps

### 1. RunTestsStep
- Executes tests using TestOperations
- Captures test output and pass/fail status
- Stores results in step state
- Stops workflow if all tests pass (no fixes needed)

### 2. AnalyzeErrorsStep
- Uses ASTOperations to parse error messages
- Uses tree-sitter to analyze code structure
- Identifies root causes of failures
- Generates fix suggestions

### 3. ApplyFixStep
- Uses PatchOperations to apply fixes
- Checks safety policy before modification
- Handles patch application errors

### 4. VerifyFixStep (Terminal)
- Re-runs tests using TestOperations
- Verifies fix resolved failures
- Marked as terminal (workflow ends here)

## Integration Points

### With Existing Orchestrator
- `NewSharedInfrastructure(orchestrator)` - Wraps existing tools
- No changes to Orchestrator required
- Existing tools remain unchanged

### With Ask Pipeline (Future)
Currently, `executeStepOnce` simulates lifecycle hooks. Full integration requires:

1. **Ask Pipeline Detection**: Detect when processing a Step vs regular capability
2. **Hook Injection**: Call Step lifecycle hooks at appropriate points:
   - After routing: `OnStart()`
   - During prompt building: `OnBeforePrompt()`
   - After tool execution: `OnAfterToolResult()`
   - After each turn: `OnAfterTurn()`
   - Before epilogue: `OnFinalize()`
3. **Context Injection**: Provide SharedInfrastructure via context

## Benefits

### 1. Clean Separation of Concerns
- **WorkflowManager**: Orchestration logic (sequence, retry, failure)
- **Step**: Task-specific logic (what to do)
- **Ask Pipeline**: Reasoning and tool execution (how to do it)

### 2. Reusable Infrastructure
- AST analysis shared across Steps
- Tree-sitter parsing available to all
- Patch engine, test runner, etc. accessible
- No duplication of infrastructure

### 3. Composability
- Steps are self-contained units
- Same Step used in different workflows
- New workflows = combine existing Steps

### 4. No Secondary Orchestration Loop
- Each Step is one Ask transaction
- WorkflowManager sequences, doesn't orchestrate internally
- Respects Ask pipeline boundaries

### 5. Scalability
- Add new Steps without modifying WorkflowManager
- Add new tools to SharedInfrastructure without breaking Steps
- Multiple workflows can coexist

## Testing

Run tests:
```bash
go test ./lib/sop/ai/agent/codingworkflow -v
```

Key tests:
- `TestSharedInfrastructureAccess` - Verify infrastructure wrapping
- `TestBaseStepLifecycle` - Verify Step interface works
- `TestStepStateManagement` - Verify step state handling
- `TestStepRetryLogic` - Verify retry with exponential backoff
- `TestTerminalStep` - Verify terminal steps stop workflow

## Phase 1 Complete ✅

Implemented:
- ✅ SharedInfrastructure wrapping existing tools
- ✅ WorkflowManager with retry/failure handling
- ✅ Step interface with lifecycle hooks
- ✅ BaseStep with default implementations
- ✅ Example Steps (RunTests, AnalyzeErrors, ApplyFix, VerifyFix)
- ✅ Context-based infrastructure access
- ✅ Comprehensive tests

Legacy Untouched:
- ✅ Orchestrator.Execute() unchanged
- ✅ Capability.Orchestrate() unchanged
- ✅ Existing tools unchanged
- ✅ No breaking changes

## Next Phase (Separate Task)

Integration with Ask Pipeline:
1. Detect Step processing in Ask pipeline
2. Call lifecycle hooks at appropriate points
3. Inject SharedInfrastructure into context
4. Fix BuildContext/BuildInstructions not being called

Cleanup (when Ask integration complete):
1. Remove or deprecate Orchestrate() method
2. Migrate workflows to WorkflowManager pattern
3. Phase out queue-based orchestration

## Files Created

1. `shared_infrastructure.go` - Infrastructure wrapper (481 lines)
2. `workflow_manager.go` - WorkflowManager implementation (459 lines)
3. `step_capability.go` - Step interface and example implementations (444 lines)
4. `workflow_manager_test.go` - Comprehensive tests (308 lines)
5. `WORKFLOW_MANAGER_IMPLEMENTATION.md` - This documentation

Total: ~1,700 lines of clean, tested code with zero breaking changes.
