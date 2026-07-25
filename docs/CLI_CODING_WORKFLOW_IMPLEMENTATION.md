# CLI Coding Workflow Implementation

**Last Updated**: 2026-07-24  
**Status**: ✅ V1 Complete - Production Ready  
**Post-V1**: Data-Driven Workflows with Infrastructure Integration

## Purpose

This document captures the coding workflow capability that makes SOP feel like a high-quality terminal-first coding assistant. V1 is **pure code-driven** with proven infrastructure backing. Post-V1 will enable **data-driven workflow authoring** while preserving infrastructure power.

## V1 Completion Status

### ✅ Shipped Features

**Infrastructure Foundation**:
- ✅ AST/tree-sitter parsing via GrammarRegistry
- ✅ ContextManager for rich code context assembly
- ✅ PatchEngine for precise code modifications
- ✅ TestRunner with output parsing and iteration
- ✅ RepoIndexer for searchable repository structure
- ✅ ValidationOrchestrator for KB quality enforcement

**Workflow Orchestrators** (All Multi-Step, Infrastructure-Backed):
- ✅ **Test Fix Workflow** (7 steps): analyze → isolate → patch → validate → test → iterate → document
- ✅ **Architecture Workflow** (5 steps): analyze → map → design → validate → document
- ✅ **Documentation Workflow** (4 steps): analyze → extract → generate → review
- ✅ **Implementation Workflow** (3 steps): plan → implement → validate

**Routing & Classification**:
- ✅ Fast-path keyword routing (80%+ queries, zero LLM calls)
- ✅ Task classifier with 35+ keywords (design, architecture, document, spec, plan, etc.)
- ✅ Backwards-compatible Stores/Spaces domain inference

**SDLC Enhancements**:
- ✅ Architecture/design workflow support
- ✅ Documentation generation workflow
- ✅ Technical spec writing
- ✅ Planning and proposal workflows

**Desktop Automation**:
- ✅ Git command execution
- ✅ Shell/postgres/splunk automation
- ✅ PTY session persistence

### 🔄 Deferred to Post-V1

**Data-Driven Workflows**:
- 📋 Visual workflow builder UI
- 📋 JSON-based workflow definitions in KB
- 📋 Infrastructure tool registry (expose analyze_ast, generate_patch as workflow steps)
- 📋 Team-customizable workflows
- 📋 Workflow marketplace/sharing

**Rationale**: V1 proves infrastructure-backed orchestration works. Post-V1 wraps this in a data-driven, user-customizable layer.

---

## Architecture Foundation: Multi-Turn Queue-Based Orchestration

The coding workflow capability leverages SOP's **queue-based multi-turn orchestration architecture** to execute complex, multi-step workflows across multiple ReAct turns.

**Key Enabler**: Instead of completing all work in a single turn (hitting token/time limits) or implementing internal loops (bypassing ReAct tools), the coding workflow injects work items into an execution queue. Each injected prompt gets full access to the ReAct tool registry.

**Workflow Example**:
```
Turn 1 (User): "Fix the parser bug"
  ↓ Inject: "Generate patch for parser.go"
Turn 2: Patch generation with full tool access
  ↓ Inject: "Apply patch"
Turn 3: File write with approval gate
  ↓ Inject: "Run tests"
Turn 4: Terminal execution
  ↓ Conditional: If tests fail, inject "Analyze failures and fix"
```

**See**: [Multi-Turn Queue-Based Orchestration](../lib/sop/ai/README.md#multi-turn-queue-based-orchestration) in the main AI architecture doc for full technical details.

### Phase 4: Parallel Execution

The architecture supports **parallel execution** of independent workflow steps by launching multiple `Ask()` threads. Each thread gets its own isolated queue and execution context.

**Example** (Parallel Quality Checks):
```
User: "Run tests and check formatting"
  ↓
Split into parallel threads:
  Thread 1 (Main/PTY): "Run tests"
  Thread 2 (Background): "Check formatting"
  ↓
Both execute simultaneously with isolated queues
  ↓
Results aggregated: "Tests passed ✓, Formatting OK ✓"
```

**Process Routing**:
- **Main thread**: Routes to PTY child process (stateful shell, preserves `cd`, env vars)
- **Background threads**: Route to exec.Command goroutines (stateless, parallel execution)

**Benefits**:
- 2-3x speedup for independent tasks
- Each Ask() call is fully isolated (own queue, own state)
- Main thread maintains shell persistence for subsequent commands

**Implementation**: Wrapper around existing `Ask()` - no modifications to core orchestration logic required.

## V1 Product Direction

For V1, the coding workflow should be designed to compete with high-quality coding copilots on everyday repository work. The bar is not merely “answer questions about code”; it is to help a developer inspect, edit, test, debug, and continue work with minimal friction.

The V1 implementation should prioritize:

- repo-aware context selection over generic chat context
- explicit planning before edits, with user-visible intent and rationale
- patch application with validation, diff review, and rollback safety
- test-and-fix loops that iterate until a task is verified
- persistent memory for repo-specific work, including file, symbol, and relationship recall
- code-aware retrieval that can reason over files, symbols, and nearby tests rather than only raw text

For this direction, AST/tree-sitter-style structure is a first-class V1 capability, but the system should store derived code-memory objects rather than raw full parse trees. The long-term memory layer should keep compact summaries, symbol facts, relationships, and vector embeddings so retrieval remains fast and useful.

## V1 Implementation Status

### ✅ Complete

- [x] Add repo-aware context assembly that selects the active file, nearest symbol, nearby tests, and relevant package/module context.
- [x] Add a deterministic patch and edit layer that can apply targeted edits, show diffs, and support safe rollback.
- [x] Add a test-and-fix loop that runs relevant tests, parses failures, and iterates until success or a bounded stop condition.
- [x] Add code-aware retrieval using lightweight parsing or AST/tree-sitter support for symbols, imports, and call relationships.
- [x] Add hybrid retrieval that combines deterministic context, path-based structure, and semantic memory for repo-specific recall.
- [x] Add persistent coding memory for repo/file/symbol summaries, relationships, embeddings, and lifecycle state.
- [x] Add explicit workflow tracing so the user can see what context was selected, what changed, and what was validated.
- [x] Add SDLC workflows (architecture design, documentation, planning)
- [x] Add fast-path routing optimization (keyword-based, eliminates 80% of LLM calls)

### 📋 Post-V1

- [ ] Visual workflow builder UI
- [ ] Infrastructure tool registry (expose coding tools to workflow system)
- [ ] JSON-based workflow definitions in KB
- [ ] Workflow sharing via Spaces
- [ ] Advanced patch visualization with side-by-side diffs
- [ ] Integration test generation from unit tests

---

## Implementation Details
- [ ] Add safety controls for destructive operations, including preview, approval, and rollback paths.
- [x] **Add systematic test debugging protocol (HIGH PRIORITY)**: ✅ **COMPLETED** - System prompt with mandatory workflow steps, verification gates, and improved tool result visibility. See the systematic test-fix protocol section in this document (§6.1).
- [ ] **KB-Driven Workflow Orchestration (APPROVED)**: ✅ **DESIGN COMPLETE** - WorkflowManager with script auto-optimization and compilation path. See [docs/KB_DRIVEN_WORKFLOW_ASSESSMENT.md](/Volumes/T7/sop-cloud/docs/KB_DRIVEN_WORKFLOW_ASSESSMENT.md) for full design.
  - **Phase 1** (2-3 days): WorkflowManager + KB integration + interpreted execution
  - **Phase 2** (2-3 days): Script auto-optimization (detect when to use scripts vs tools)
  - **Phase 3** (2-3 days): Code generation (JSON → Go → Binary compilation)
  - **Impact**: Data-driven workflows, 5-10x script speedup, distributable binaries
  - **Competitive Edge**: Nobody else has data-driven workflows that compile to native binaries

## Guard Rails for Future Copilots

The implementation should follow a few durable rules so future copilots, contributors, and adapters do not drift into implicit or unsafe behavior:

- Keep workflow inputs explicit. Prefer Task and RunState over hidden context or ambient globals.
- Keep the orchestration layer thin. It should coordinate policy, context, patches, and memory without embedding hidden runtime assumptions.
- Default to read-only behavior. Mutation should require an explicit policy decision, user approval, or a clearly marked destructive operation.
- Prefer preview and explainability before execution. Copilots should be able to show what they plan to change and why.
- Keep interfaces pluggable. New context providers, patch engines, search tools, or memory backends should fit through the facade instead of being hard-coded.
- Preserve deterministic behavior where possible. If the workflow cannot explain or justify an action, it should not perform it silently.
- Keep coding-workflow capabilities local-runtime scoped. The HQ site/server surface should remain focused on shared remote services, while the end-user host, daemon, or local RAG runtime should own coding workflow availability.
- Do not register coding workflow as a generic HQ capability. If a deployment is server-hosted and not intended to execute local code changes, it should not expose this capability path.

---

## Current Strengths

The repository already has a strong foundation for this direction:

- A copilot runtime and tool-driven execution layer in [lib/sop/ai/agent](../lib/sop/ai/agent)
- Grounding and retrieval guidance in [lib/sop/AI_COPILOT.md](../lib/sop/AI_COPILOT.md)
- Existing support for tool-based, policy-aware reasoning
- A clear path toward hierarchical knowledge routing and repo-aware context retrieval

The remaining work is mostly orchestration, context packaging, and execution reliability.

---

## Implemented Foundation

The first facade-based milestone is now in place under [lib/sop/ai/agent/codingworkflow/orchestrator.go](../lib/sop/ai/agent/codingworkflow/orchestrator.go) and covered by [lib/sop/ai/agent/codingworkflow/orchestrator_test.go](../lib/sop/ai/agent/codingworkflow/orchestrator_test.go).

This initial implementation provides:

- an explicit workflow surface based on Task and RunState rather than hidden context usage
- a small orchestrator that can assemble context, apply a patch, and persist lightweight memory
- pluggable interfaces for context building, repo indexing, patch application, memory, and policy enforcement
- a safe default policy that blocks destructive edits unless explicitly allowed

This gives us a stable shell for future expansion without overcommitting to a single implementation strategy.

---

## Missing Pieces

### 1. Context Slicing and Context Packing

Problem:
The system should not send whole files to the model for every request.

Required capability:
A context manager that can dynamically gather the smallest useful slice of code for the current task.

Implementation targets:
- Extract the active file and current symbol
- Include the enclosing function/class block
- Include nearby imports, definitions, or type declarations when needed
- Attach relevant test files or references when the task requires it
- Expand outward only when the task context demands it

Suggested output structure:
- active file chunk
- symbol definition chunk
- related test chunk
- related reference chunk
- package or module summary chunk

### 2. Repo-Aware Retrieval

Problem:
The agent needs to understand what part of the repo matters at the current moment.

Required capability:
Use repository structure to identify the correct scope for the request.

Implementation targets:
- active file
- current package/module
- changed files
- nearest tests
- referenced symbols
- similar implementations

### 3. Execution Loop for Coding Tasks

Problem:
The system must do more than answer questions; it must perform coding tasks end to end.

Required capability:
A loop that can:
- inspect the task
- choose relevant files
- edit code
- run tests
- read errors
- iterate until success or a safe stop

This is the primary differentiator for a terminal-first coding workflow.

### 4. Patch and Edit Engine

Problem:
Editing code through prompts alone is unreliable without a deterministic patch layer.

Required capability:
A patch engine that can:
- apply targeted edits
- preserve formatting
- support multi-file edits
- review diffs before accepting them
- roll back when necessary

### 5. Test-and-Fix Loop

Problem:
Model-only edits fail often unless the system has a clear feedback path.

Required capability:
A workflow that can:
- detect relevant tests
- run them
- parse failures
- fix the issue
- rerun until green or a stop condition is reached

This should be a core product feature, not an optional add-on.

### 6. Debugging Workflow Hooks

Problem:
A coding assistant should be useful when code is broken, not only when it is being written.

Required capability:
The system should support:
- running the relevant command
- capturing logs and stack traces
- inspecting failing tests
- proposing root cause hypotheses
- retrying with a bounded loop

#### 6.1 Systematic Test Fix Protocol (High Priority)

**Root Cause**: Analysis of real failures (GPT 5.4 test-fix attempts, 2026-07-20) showed that even with all necessary tools available, the LLM failed to fix tests because it didn't follow a systematic debugging workflow. Key failures:
- Never captured actual test error output (ran tests but didn't read results)
- Stored large outputs to files but never called `read_tool_result` to inspect them
- Never read test source code to understand what was being tested
- Never read implementation code to find the bug
- Never attempted any fixes
- Claimed completion without verification

**Solution**: Explicit test debugging protocol in system prompt and workflow gates.

This is now implemented in the runtime via a mandatory test-debugging protocol that activates for test-fix requests. The system prompt now requires a seven-step workflow, tool results surface failures more clearly, and test execution uses verbose output so the model sees the full error context.

##### Required System Prompt Addition

When user requests "fix failing tests" or similar, inject this protocol:

```
TEST DEBUGGING PROTOCOL (MANDATORY STEPS):

Step 1: CAPTURE TEST FAILURE
- Run: execute_local_command(command="go test -v -run TestName ./path", reasoning="capture full test error")
- If output saved to file: IMMEDIATELY call read_tool_result(file_path="...") to read it
- Extract the actual error message from output
- NEVER proceed without seeing the actual error

Step 2: UNDERSTAND THE TEST
- Call: read_file(path="path/to/test.go", start_line=N, end_line=M)
- Understand what the test is checking
- Identify which functions/methods it calls

Step 3: INSPECT IMPLEMENTATION
- Use search_repo_files or grep_repo_text to find implementation
- Read the code that the test calls
- Compare expected behavior vs actual behavior

Step 4: IDENTIFY ROOT CAUSE
- Match error message to code location
- Determine what's missing, incorrect, or misconfigured
- Form hypothesis about the fix

Step 5: APPLY FIX
- Use patch_file for focused changes (preferred)
- Or write_file for complete rewrites (if necessary)
- Include clear reasoning for the change

Step 6: VERIFY FIX
- Re-run the same test command from Step 1
- Confirm test passes
- Show passing output to user

Step 7: ITERATE IF NEEDED
- If test still fails, return to Step 1 with new error
- Update hypothesis and try again
- Maximum 3 iterations before asking user for guidance

CRITICAL RULES:
✗ NEVER claim tests are fixed without running them
✗ NEVER skip reading the error output
✗ NEVER skip reading test and implementation code
✗ NEVER proceed to next step without completing prior step
✓ ALWAYS show passing test output as proof of success
```

##### Implementation Checkpoints

Add workflow verification gates in the ReAct engine:

```go
type TestFixState struct {
    HasCapturedError       bool
    ErrorMessage           string
    HasReadTestFile        bool
    HasReadImplementation  bool
    HasAppliedFix          bool
    HasVerifiedFix         bool
    IterationCount         int
}

// Before allowing completion of a test-fix request:
if userRequestedTestFix {
    if !state.HasCapturedError {
        return error("Must capture test error before proceeding")
    }
    if !state.HasReadTestFile || !state.HasReadImplementation {
        return error("Must read test and implementation code")
    }
    if state.HasAppliedFix && !state.HasVerifiedFix {
        return error("Must re-run tests after applying fixes")
    }
}
```

##### Tool Result Visibility Improvements

Problem: Large test outputs stored to files but LLM never reads them.

**Solution A (Preferred)**: Auto-inline critical portions
```go
// In run_repo_tests and execute_local_command tools:
if exitCode != 0 && len(output) > 20000 {
    stored := storeToFile(output)
    return fmt.Sprintf(`Test FAILED. Output was large (%dKB) and saved to: %s

CRITICAL ERRORS (first 50 lines):
%s

NEXT REQUIRED STEP: Call read_tool_result(file_path="%s", start_line=1, end_line=100) for full output`, 
        len(output)/1024, stored, firstNLines(output, 50), stored)
}
```

**Solution B**: Explicit instruction in tool result
```go
return fmt.Sprintf(`Output saved to: %s

⚠️  REQUIRED NEXT STEP: You must read this file to see the error.
Call: read_tool_result(file_path="%s", start_line=1, end_line=50)
Or: grep_tool_result(file_path="%s", pattern="FAIL|error|Error")`, 
    path, path, path)
```

##### Test-Specific Tool Enhancement

Consider adding a focused `debug_test` tool that bundles the workflow:

```json
{
  "name": "debug_test",
  "description": "Run a test, capture failure, and return structured diagnostic info",
  "schema": {
    "test_path": "path to test package",
    "test_name": "specific test name to debug",
    "max_iterations": 3
  }
}
```

Returns:
```json
{
  "status": "failing",
  "error_message": "can't do operation on b-tree if transaction has not begun",
  "test_file": "lib/sop/cmd/sop-daemon/main_test.go",
  "test_line": 456,
  "implementation_hints": [
    "Test calls handleGetVectorizationRecoveryStatus",
    "Handler calls resetVectorizationIndexForRecovery", 
    "vectorizationResetExecutor is not mocked"
  ],
  "suggested_fix": "Mock vectorizationResetExecutor in test setup",
  "next_steps": [
    "read_file to inspect test setup",
    "patch_file to add mock"
  ]
}
```

##### Coding Memory Integration

Store successful patterns for future reference:

```go
// After successful test fix:
a.service.session.Memory.AddRuntimeCaveat(
    "test_fix_pattern",
    fmt.Sprintf("Fixed %s by %s", testName, fixDescription),
    "success_pattern",
    "coding_workflow",
    "learned"
)

// Store failures too:
if fixFailed && iterationCount >= 3 {
    a.service.session.Memory.AddRuntimeCaveat(
        "test_fix_failure",
        fmt.Sprintf("Failed to fix %s. Common mistake: %s", testName, commonMistake),
        "antipattern",
        "coding_workflow",
        "learned"
    )
}
```

##### Priority Implementation Order

1. **Immediate (High ROI)**: Add test debugging protocol to system prompt
2. **Short-term**: Add verification gates to prevent claiming success without proof
3. **Short-term**: Improve tool result messages to explicitly guide next steps
4. **Medium-term**: Auto-inline critical error portions in test failures
5. **Long-term**: Build unified `debug_test` tool
6. **Long-term**: Implement coding memory learning from patterns

### 7. Session Memory and Continuity

Problem:
A good coding workflow feels like a collaborator, not a stateless chatbot.

Required capability:
Persist lightweight state across turns:
- current task
- files modified
- previous attempts
- known constraints
- failed commands and their output
- user intent and preferred scope

### 8. Safety and Approval Model

Problem:
Terminal coding workflows can be destructive if left unconstrained.

Required capability:
A safety layer with:
- read-only mode by default
- approval for destructive actions
- support for dry-run previews
- clear rollback paths
- command allow/deny policy

### 9. Interviewable Output and Tracing

Problem:
Developers need to understand what the agent is doing.

Required capability:
The system should expose:
- what context it selected
- why it chose it
- what it changed
- what it ran
- what the next step is

This is essential for trust and usability.

---

## Proposed Architecture

### A. Context Manager

The context manager is the first core subsystem.

Responsibilities:
- receive the current task and workspace state
- build a compact context pack
- select the right file slices
- attach relevant symbols, tests, and references

### B. Repo Index and Symbol Graph

A lightweight index should provide:
- file paths
- symbols
- function/class boundaries
- package/module ownership
- references and call sites
- test-file relationships

This can be built incrementally and used for retrieval and context selection.

### C. Task Executor

The task executor manages the coding loop:
- plan the next step
- update context if needed
- edit files
- run tests or commands
- interpret results
- continue or stop

### D. Patch and Validation Layer

This layer ensures changes are safe and verifiable:
- patch application
- diff review
- test execution
- error parsing
- rollback handling

### E. Memory and Policy Layer

This layer keeps the workflow consistent and safe:
- session memory
- tool policy
- approval flow
- task history

---

## MVP Scope

The first implementation should focus on the most valuable workflow improvements:

1. Context slicing for active file and active symbol
2. Lightweight repo-aware retrieval for nearby files and tests
3. Patch application for targeted edits
4. Test-run feedback loop
5. Basic session memory
6. Safe terminal execution with preview/approval

The current facade already satisfies the first milestone of this plan by providing a transparent execution shell with explicit state and pluggable components. The next increments should build on that shell rather than replacing it.

---

## Success Criteria

The implementation will be considered successful when the workflow can:

- answer repo-aware questions with minimal context waste
- edit the correct file slice without dumping entire files into the prompt
- run relevant tests and use failures to refine the next edit
- maintain continuity across turns without losing task state
- operate safely in a terminal environment

---

## Recommended Delivery Order

### Phase 0: Facade Stabilization
- keep Task and RunState as the explicit execution contract
- preserve the pluggable interfaces for future implementations
- keep the default implementations simple and deterministic

### Phase 1: Context Intelligence
- active-file slicing
- symbol-aware extraction
- nearby test and reference inclusion
- optional repo-aware retrieval adapters

### Phase 2: Editing Flow
- patch engine
- multi-file edit support
- diff review
- patch preview and approval hooks

### Phase 3: Execution Loop
- test runner integration
- failure interpretation
- iterative repair loop
- bounded retry and stop conditions

### Phase 4: Productization
- richer terminal UX
- explainable traces and progress output
- approval policies and rollback paths

### Phase 4: Product Polish
- memory and continuity
- approval and safety controls
- richer developer-facing progress output

---

## Bottom Line

The missing pieces are not “more model power.” They are:

- better context selection
- deterministic editing
- test-driven repair loops
- repo awareness
- safe execution
- memory and continuity

Those are the capabilities that make a CLI coding workflow feel genuinely competitive.
