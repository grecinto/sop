# VS Code and Continue facade architecture

**Status**: Active design / implementation in progress

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

## Architecture Rule

The daemon must remain the controller.

That is not just an implementation preference. It is a product requirement.

If Continue becomes the primary controller of the LLM conversation loop, the daemon stops being the system of record for:

- undo history
- memory and retrieval
- approvals and policy rails
- workstream continuity
- provider-owned ReAct state
- long-running workflow execution

Those features only remain first-class product capabilities when the daemon stays on the hot path for every meaningful request.

The control rule is therefore:

- Continue owns IDE UX and IDE services.
- The daemon owns orchestration, provider routing, memory, undo, approvals, and workflow state.

## Recommended architecture

The cleanest approach is a thin adapter layer between Continue and the existing daemon/backend Ask pipeline.

Continue should not need to understand the full coding workflow engine directly. Instead, it should consume a compact tool surface exposed by the shared runtime. The coding-focused tools should be backed by the existing AST, tree-sitter, repo-indexing, patching, and validation infrastructure so that Continue can act as an editor-facing client over a code-aware toolset while the daemon remains the controller.

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
   - Exposes editor-aware services back to the daemon when the daemon needs IDE execution.

2. **Adapter layer**
   - Maps Continue requests into the existing Ask payload.
   - Preserves streaming progress and session identity.
   - Converts editor context into the structured inputs expected by the workflow runtime.
   - Optionally presents daemon-owned shadow models or local provider facades as native-looking provider choices.

3. **Shared workflow runtime**
   - Owns orchestration, planning, execution, review, and validation.
   - Maintains approvals, tool policies, and state transitions.
   - Remains the single source of truth for workflow behavior.

## Transparent Provider Facades

The preferred long-term UX is that Continue should display familiar provider choices such as Anthropic, Gemini, and OpenAI without exposing a separate visible "SOP bridge" provider to the user.

In that model:

- Continue still shows normal provider/model choices.
- Those choices route to daemon-owned local endpoints.
- The daemon decides how to execute, route, persist, and enrich the request.

This means the bridge becomes transport, not product surface.

### Two implementation levels

#### 1. Shadow-model V1

Continue shows friendly model titles such as:

- Claude Sonnet
- Gemini 2.5
- GPT-4.1

But all of them still route through one daemon-owned protocol surface. This is sufficient for a strong V1 because it removes bridge branding while preserving daemon control.

#### 2. Provider-compatible local facades

The longer-term shape is to expose daemon-owned local endpoints that look provider-native to Continue:

- Anthropic-compatible local facade
- Gemini-compatible local facade
- OpenAI-compatible local facade

In that design, Continue believes it is using native provider transports, but the daemon remains the actual runtime and controller.

## Why this is the right shape

This arrangement avoids three common mistakes:

- treating Continue as a full replacement for the workflow engine
- embedding workflow orchestration logic inside the editor extension
- letting Continue own the main conversation loop and then trying to reconstruct daemon-only product features after the fact

The workflow runtime should stay in the backend/daemon layer because it already owns the important policy and execution concerns.

## Controlled-agent integration model

The integration can evolve beyond a simple chat bridge into a controlled-agent pattern.

- Continue remains the editor-facing surface and can expose a chat history, progress stream, and diff UI in VS Code.
- The daemon Ask/ReAct runtime remains the orchestrator for the broader workflow.
- When the workflow reaches a coding-focused subtask, the daemon can delegate to Continue through a small remote tool or RPC contract.
- Continue executes the edit-oriented action in VS Code, streams progress, and returns structured results such as file changes, patch previews, validation status, or an error.
- The daemon then consumes that result and continues the larger workflow.

This creates a clean control boundary:
- daemon Ask = supervisor
- Continue = subordinate coding agent for editor-aware tasks

This is the preferred model for a future bidirectional setup. The user may start from the SOP AI window or from Continue, but the execution handoff is explicit and controlled. If VS Code is not running, the daemon can fall back to a non-editor execution path; if VS Code is running, Continue provides the richer coding experience.

## Authentication and recovery UX

Continue should reuse the existing daemon/HQ bearer-token flow rather than introducing a separate auth system. Requests should carry an Authorization header with a short-lived access token, and the backend should validate that token through the existing auth middleware. The token lifecycle should follow the same login and renewal path used by the daemon/HQ experience so users do not need to manage a second identity model.

For a simple user experience, Continue should expose a small sign-in affordance in its UI, such as a compact entry or button that opens the existing HQ/daemon login flow. When a request fails with 401 or 403 because the session is expired or invalid, the Continue client should show a lightweight modal over the VS Code window prompting the user to sign in again. If renewal succeeds, the client should retry the pending request automatically; if renewal fails, it should stop the current workflow gracefully and guide the user back through the login flow.

## Current Status

The integration is no longer design-only.

Already present in the codebase:

- daemon chat bridge endpoint: `/api/continue/chat`
- daemon tool bridge endpoint: `/api/continue/tools/<tool>`
- daemon session-state endpoint: `/api/continue/sessions/<sessionId>`
- daemon OpenAI-compatible endpoint: `/v1/chat/completions`
- a working Continue config that currently points at the daemon through an OpenAI-compatible entry

What is not yet complete:

- removal of bridge branding from the Continue model picker
- generated shadow-model catalog for native-feeling model choices
- provider-compatible local facades for Anthropic and Gemini
- robust config sync between daemon-selected port/provider state and Continue model config
- clear resume-oriented implementation milestones in the design docs

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

## V1 Definition

V1 should not be defined as full provider-protocol emulation.

V1 should be defined as:

- daemon remains the controller and system of record
- Continue remains the IDE surface and IDE service layer
- the visible "SOP bridge" model disappears from the Continue picker
- Continue shows friendly shadow models for the providers users expect
- all requests still flow through daemon-owned transport surfaces
- memory, undo, approvals, workstreams, and provider-owned ReAct loops remain fully daemon-owned

This is the fastest path to a real product outcome.

## V2 Definition

V2 can add true provider-compatible local facades:

- OpenAI-compatible local surface refined beyond the current chat-only entry
- Anthropic-compatible local messages facade
- Gemini-compatible local generate/stream facade

That lets Continue think in more native provider terms internally while still keeping the daemon on the hot path.

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

## Resume Checklist

When resuming this work, inspect these surfaces first:

### Runtime and bridge surfaces

- daemon route wiring in `lib/sop/cmd/sop-daemon/main.go`
- bridge handlers in `lib/sop/cmd/sop-daemon/rag.go`
- authenticated backend equivalents in `lib/sop/tools/sopbackend/main.ai.go`

### Current Continue config surface

- `.continuerc.json`
- `continue-daemon-config.json`

### Design docs

- `docs/CONTINUE_FACADE_ARCHITECTURE.md`
- `docs/CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md`
- `docs/CONTINUE_BRIDGE_API_SPEC.md`

### First implementation questions to answer

- How will shadow models be generated and synced into Continue config?
- Which provider/model labels should V1 expose by default?
- How will the selected provider intent be passed to the daemon per request?
- How will daemon port and auth state be synchronized into Continue?
- Which bridge-visible tools should be enabled by default in V1?

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
- Do not let Continue become the effective controller of multi-turn workflow state.
- Do not make daemon-only product features depend on reconstructing state from Continue history.

## Outcome

If implemented this way, the system gains a polished VS Code coding experience without discarding the architecture that already supports phases, automation, memory, and validation. The result is a stronger coding workflow experience that still remains coherent, debuggable, and extensible.

For a concrete implementation blueprint with a proposed endpoint layout and tool catalog, see [CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md](CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md).
