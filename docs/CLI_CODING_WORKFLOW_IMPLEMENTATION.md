# CLI Coding Workflow Implementation Plan

## Purpose

This document captures the missing capabilities required to make SOP's coding workflow feel like a high-quality terminal-first coding assistant rather than a generic chat interface.

The focus is on workflow execution, not just answer generation.

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
