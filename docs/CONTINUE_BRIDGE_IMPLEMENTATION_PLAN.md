# Continue bridge implementation plan

**Status**: Active plan / partial implementation already present

## Goal

Define the minimal bridge between VS Code + Continue and the existing daemon-backed Ask/ReAct runtime so Continue can act as the editor-facing client while the app remains the controller and execution engine.

## Controller Rule

The daemon must remain the controller.

Continue cannot own the main conversation loop if AI Studio is expected to provide:

- undo history
- memory and retrieval
- approvals and policy rails
- provider-owned ReAct behavior
- workstream continuity
- durable multi-step workflow state

If Continue becomes the controller, the daemon only receives partial downstream events and those features stop being native runtime behavior. That is not acceptable for the intended product shape.

## Recommended integration shape

Use a small local bridge service that Continue calls from VS Code. The bridge should:

- accept Continue-style chat requests
- attach workspace and editor context
- forward requests to the local daemon Ask endpoint
- stream progress and tool events back to Continue
- expose a small, code-aware tool surface for editing, inspection, validation, and workflow resumption

```text
VS Code + Continue
  -> local bridge
  -> SOP daemon /api/ask
  -> shared workflow engine
  -> tools: search, inspect, patch, test, resume
```

## Current status

Parts of this bridge already exist.

Implemented today:

- `/api/continue/chat`
- `/api/continue/tools/<tool>`
- `/api/continue/sessions/<sessionId>`
- `/v1/chat/completions` OpenAI-compatible entrypoint
- test coverage for the chat bridge, file-read tool bridge, patch preview/apply bridge, and OpenAI-compatible chat surface
- a working Continue config that currently points at the daemon via an OpenAI-compatible model entry

Still missing for the intended UX:

- removal of visible bridge branding from model selection
- shadow-model catalog generation and config sync
- provider-compatible Anthropic facade
- provider-compatible Gemini facade
- unified auth/config sync between Continue and daemon UI

## Initial transport choice

Start with a local HTTP bridge. It is the fastest path to a working integration and is sufficient for the first implementation milestone.

## Transparent provider strategy

There are two sensible rollout levels.

### V1: Shadow models over daemon-owned transport

Continue should display familiar model labels such as:

- Claude Sonnet
- Gemini 2.5
- GPT-4.1

But those labels should still route through daemon-owned transport surfaces. The user should not see `SOP Daemon Bridge` as an LLM choice.

This is sufficient for V1 because it preserves the UX goal while keeping implementation scope moderate.

### V2: Provider-compatible local facades

The long-term target is to expose provider-native local ingress surfaces:

- OpenAI-compatible facade
- Anthropic-compatible facade
- Gemini-compatible facade

Continue can then keep using native provider assumptions while the daemon remains the actual runtime.

## Controlled-agent delegation pattern

The first bridge is sufficient for chat and tool calls. The next step is to let daemon Ask delegate coding subtasks to Continue as a remote agent.

### Execution flow

1. daemon Ask identifies a coding subtask that requires editor-aware execution.
2. It invokes Continue over a narrow RPC/HTTP contract.
3. Continue executes the subtask in VS Code, streams progress to its chat UI, and returns structured output.
4. daemon Ask consumes that result and resumes the overarching workflow.

### Design rule

Continue should not be the controller of the workflow. It is a worker/agent that receives instructions and reports results. The daemon Ask remains the supervisor and the source of truth for workflow state, approvals, and session continuity.

This design gives us a seamless experience for the user while keeping the control graph simple and explicit.

### Authentication and recovery UX

The bridge should reuse the existing bearer-token auth path already used by the daemon/HQ APIs. Every Continue request should include an Authorization header with a short-lived access token, and the backend should validate it through the same auth middleware used elsewhere in the system.

The Continue client should offer a minimal sign-in experience, such as a compact entry/button in the Continue UI that launches the existing HQ/daemon login flow. If a request returns 401 or 403 because the token expired or was invalidated, the client should surface a lightweight modal over the VS Code window that asks the user to sign in again. After successful re-auth, the client should retry the pending request or reopen the affected session so the workflow can continue without a hard interruption.

### Suggested endpoints

1. **POST /api/continue/chat**
   - accepts a chat request and returns a stream of events
   - used for normal user prompts and workflow-driven turns

2. **POST /api/continue/tools/{toolName}**
   - invokes a single tool by name with structured arguments
   - used when Continue wants to inspect code, patch files, or run validation

3. **GET /api/continue/sessions/{sessionId}**
   - returns the current workflow state or metadata for a continuing task

## Chat request contract

A basic request should include:

```json
{
  "session_id": "workspace-42",
  "message": "fix the failing test in the parser module",
  "workspace": {
    "root": "/path/to/repo",
    "active_file": "src/parser/handler.go",
    "selection": {
      "start_line": 120,
      "end_line": 150
    }
  },
  "context": {
    "open_files": ["src/parser/handler.go", "src/parser/handler_test.go"],
    "selected_text": "..."
  },
  "mode": "chat"
}
```

## Streaming response contract

The bridge should stream NDJSON or a similar event stream so Continue can show progress immediately.

Example event types:

```json
{"type":"progress","message":"Inspecting the failing parser logic"}
{"type":"tool_call","name":"search_code","args":{"query":"parser"}}
{"type":"tool_result","name":"search_code","result":{"hits":[...]}}
{"type":"content","delta":"I found the likely failure point..."}
{"type":"done"}
```

## Minimal tool catalog

The first implementation should expose a compact tool set rather than a large API surface.

### 1. Agent delegation tools

- **agent.ask**
  - sends the request to the daemon Ask/ReAct pipeline
  - used when Continue wants the app to reason over the task

- **agent.resume**
  - resumes a previously started workflow or task
  - used when the user continues a prior coding task

### 2. Repository inspection tools

- **repo.list_files**
  - list workspace files or a directory tree

- **repo.search_code**
  - search text or symbol references across the repository

- **repo.read_file**
  - load file contents with optional line range

- **repo.get_symbols**
  - return relevant definitions, functions, classes, or methods

### 3. Code intelligence tools

These should be backed by AST/tree-sitter and repo indexing.

- **code.find_references**
  - find references to a symbol

- **code.find_definition**
  - locate the definition of a symbol

- **code.get_context**
  - build a compact context window around the active symbol or file

- **code.related_symbols**
  - find callers, implementations, and nearby dependent symbols

### 4. Editing tools

- **code.preview_patch**
  - show a proposed edit without applying it

- **code.apply_patch**
  - apply a structured patch to a file

- **code.insert_at_location**
  - insert content at a known location when the location is already known

### 5. Validation tools

- **test.run**
  - run relevant tests and return structured output

- **test.status**
  - report whether a recent test run passed or failed

- **lint.run**
  - run an optional targeted lint or static check

### 6. Workflow tools

- **workflow.get_state**
  - return the current task state, phase, and progress

- **workflow.update_state**
  - persist a progress update for multi-turn tasks

## Why this contract is sufficient

This contract is intentionally narrow. Continue does not need the full workflow engine exposed as a giant API. It needs a small set of tools that let it:

- inspect the codebase
- delegate reasoning to the daemon runtime
- edit files safely
- run validation
- continue work across turns

That is enough to support the coding workflow experience without coupling the editor plugin to the full internal architecture.

## V1 Scope

V1 should include:

- continue-to-daemon chat flow through the existing bridge
- daemon-owned session continuity and streaming
- friendly shadow models in Continue instead of bridge-branded entries
- provider/model intent passed through to the daemon
- default bridge tool catalog for repo inspection, patch preview/apply, test execution, and agent ask
- auth and daemon-port synchronization good enough for daily use

V1 should not require:

- full Anthropic protocol emulation
- full Gemini protocol emulation
- moving orchestration into Continue

## V2 Scope

V2 can add:

- provider-compatible local facades for Anthropic and Gemini
- richer Continue config synchronization and auto-repair
- daemon-to-Continue delegated coding subtasks with stronger editor-service contracts

## Implementation sequence

For a concrete endpoint and payload contract, see [CONTINUE_BRIDGE_API_SPEC.md](CONTINUE_BRIDGE_API_SPEC.md).

### Already complete

1. Local HTTP bridge shape exists.
2. Chat requests can be forwarded into the daemon Ask path.
3. Initial bridge tool set exists.
4. OpenAI-compatible chat surface exists.

### Remaining for V1

1. Replace bridge-branded Continue model entries with friendly shadow models.
2. Pass selected provider/model intent explicitly from Continue into daemon requests.
3. Sync daemon port and auth state into Continue config.
4. Validate that memory, undo, approvals, and workflow continuity remain daemon-owned in the Continue flow.
5. Tighten bridge tool defaults and error handling for daily use.

### Remaining for V2

1. Add Anthropic-compatible local facade.
2. Add Gemini-compatible local facade.
3. Expand protocol-specific streaming and tool-call compatibility tests.
4. Optionally evolve the bridge toward an MCP-style interface later if richer tool discovery becomes necessary.

## Effort estimate

For one engineer:

### V1

- approximately 5 to 10 working days for a usable version
- approximately 2 to 3 weeks for a hardened V1 with config sync and auth polish

### V2

- approximately 3 to 6 weeks for true provider-compatible local facades beyond the current OpenAI-compatible surface

## Resume checklist

When resuming implementation, start here:

### Code surfaces

- `lib/sop/cmd/sop-daemon/main.go`
- `lib/sop/cmd/sop-daemon/rag.go`
- `lib/sop/tools/sopbackend/main.ai.go`
- `.continuerc.json`
- `continue-daemon-config.json`

### Validate these first

- current bridge flow still works end-to-end
- OpenAI-compatible local model entry still points at daemon
- selected provider/model intent can be surfaced in daemon request context
- bridge sessions preserve daemon-owned state rather than Continue-owned state

## TODO tracker

- [ ] Replace visible `SOP Daemon Bridge` model entry with shadow-model entries for V1.
- [ ] Add config-sync logic so Continue model entries track daemon-selected port and runtime state.
- [ ] Pass provider/model intent per request from Continue to daemon.
- [ ] Validate daemon-owned undo, memory, approvals, and workflow continuity through the Continue flow.
- [ ] Add daemon port configurability to the UI install/setup flow so users can choose an available port.
- [ ] Persist the user-selected daemon port as a single source of truth (settings/state).
- [ ] On VSIX activation, read the persisted daemon port and update Continue model `apiBase` entries to route through that port.
- [ ] When user changes daemon port later, re-sync Continue model routing automatically without manual config edits.
- [ ] Add Anthropic-compatible local facade.
- [ ] Add Gemini-compatible local facade.

## Design guardrails

- Keep the bridge thin.
- Keep orchestration in the shared runtime.
- Keep tool results structured so Continue can reason over them.
- Use AST/tree-sitter and repo indexing as the foundation for code-aware tools.
- Prefer a small, composable tool surface over a sprawling one.
- Do not let Continue become the primary controller of workflow state.
- Do not let daemon-only product value depend on post-hoc reconstruction from Continue history.
