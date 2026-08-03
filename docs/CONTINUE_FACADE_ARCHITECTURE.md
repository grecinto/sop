# VS Code and Continue facade architecture

**Status**: Proposed / implementation-ready

## Summary

Yes — the shared coding workflow framework can be exposed through VS Code by using Continue as the editor-facing experience while keeping the existing shared runtime as the authoritative execution engine.

The recommended model is a facade, not a replacement:

- Continue provides the IDE interaction surface:
  - chat UI
  - code context selection
  - file and diff handling
  - editor-aware actions
- The shared workflow framework remains the engine:
  - phase orchestration
  - tool execution
  - approvals and safety rails
  - session continuity and memory
  - long-running automation

## Recommended architecture

The cleanest approach is a thin adapter layer between Continue and the existing daemon/backend Ask pipeline.

Continue should not need to understand the full coding workflow engine directly. Instead, it should consume a compact tool surface exposed by the shared runtime. The coding-focused tools should be backed by the existing AST, tree-sitter, repo-indexing, patching, and validation infrastructure so that Continue can act as a controller over a code-aware toolset.

```text
VS Code + Continue
  -> local adapter / bridge
  -> SOP daemon /api/ask
  -> shared workflow engine
  -> automation, shell, patching, validation, memory
```

### Layer responsibilities

1. **Editor surface (VS Code + Continue)**
   - Presents chat, context selection, and edit/diff actions.
   - Keeps the developer experience familiar and IDE-native.

2. **Adapter layer**
   - Maps Continue requests into the existing Ask payload.
   - Preserves streaming progress and session identity.
   - Converts editor context into the structured inputs expected by the workflow runtime.

3. **Shared workflow runtime**
   - Owns orchestration, planning, execution, review, and validation.
   - Maintains approvals, tool policies, and state transitions.
   - Remains the single source of truth for workflow behavior.

## Why this is the right shape

This arrangement avoids two common mistakes:

- treating Continue as a full replacement for the workflow engine
- embedding workflow orchestration logic inside the editor extension

The workflow runtime should stay in the backend/daemon layer because it already owns the important policy and execution concerns.

## Integration pattern

A minimal implementation can follow this contract:

- Continue sends a chat request with workspace context and selected code.
- The adapter forwards it to the local daemon Ask endpoint.
- The daemon routes it through the existing shared agent and workflow stack.
- The adapter streams responses back into the Continue UI as they are produced.

### Tool surface Continue should use

To support real coding workflows, Continue should call a focused set of tools exposed by the app rather than relying on raw chat-only interaction. The most valuable tools are:

- repo inspection tools
  - list files
  - search code
  - inspect symbols
  - summarize relevant files
- code intelligence tools
  - resolve definitions and references
  - find related symbols and callers
  - build local context from AST/tree-sitter data
- editing tools
  - apply a structured patch
  - preview a diff
  - insert or replace code at a known location
- validation tools
  - run tests
  - run linters or targeted checks
  - return structured failure output
- workflow tools
  - start or continue a task
  - resume prior work
  - hand off results between phases

These tools should be implemented on top of the existing coding infrastructure rather than reimplemented in the editor layer.

This keeps the editor experience lightweight while preserving the richer runtime behavior already built into the platform.

## Recommended implementation steps

1. **Expose a local bridge endpoint**
   - Accept Continue-style chat requests.
   - Map them to the existing Ask payload and session context.

2. **Reuse NDJSON streaming**
   - Surface progress and intermediate reasoning in the UI as the workflow runs.

3. **Preserve session continuity**
   - Carry session identifiers and durable context across turns.

4. **Keep tool execution in the shared runtime**
   - Avoid moving shell execution, patching, approvals, or workflow state into the editor plugin.

5. **Layer editor-specific features on top**
   - diff preview, file selection, workspace context, and apply actions can live in the VS Code/Continue layer.

## Guardrails

The following principles should govern the integration:

- Keep coding intelligence in the app runtime, not in the editor wrapper.
- Use AST/tree-sitter and repo indexing as the foundation for symbol-aware tools.
- Prefer structured tool results over free-form text where possible so Continue can make decisions reliably.
- Keep the tool surface compact and composable rather than exposing a sprawling, hard-to-manage API.

- Keep orchestration in the shared runtime.
- Keep approvals, policy decisions, and workflow state explicit.
- Treat Continue as a front-end surface, not as the source of truth for execution.
- Preserve a clear boundary between editor UX and backend automation.

## Outcome

If implemented this way, the system gains a polished VS Code coding experience without discarding the architecture that already supports phases, automation, memory, and validation. The result is a stronger coding workflow experience that still remains coherent, debuggable, and extensible.

For a concrete implementation blueprint with a proposed endpoint layout and tool catalog, see [CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md](CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md).
