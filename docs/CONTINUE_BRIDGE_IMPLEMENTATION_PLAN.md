# Continue Native Transport Implementation Plan

**Status**: Active

## Goal

Keep AI Studio and the daemon as the controller while exposing Continue as an IDE-native participant for coding workflows.

The integration target is the native Continue transport already implemented under `/api/continue/*`, with explicit provider/model attributes on each request.

## Non-Goals

- Do not restore daemon-as-OpenAI transport.
- Do not add `/v1/chat/completions`, `/v1/models`, or `/api/continue/config` back as canonical surfaces.
- Do not let Continue become the workflow controller.
- Do not move workflow state, approvals, or memory ownership into the editor client.

## Current Architecture

### Controller path

```text
Continue chat or AI Studio handoff
  -> /api/continue/chat
  -> daemon Ask/ReAct runtime
  -> normal classification, Phase Factory, passive capabilities, daemon-owned tools
  -> streamed result back to Continue
```

### Delegated editor-execution path

```text
daemon Ask turn
  -> Continue capability participates like any other capability
  -> daemon LLM decides whether bounded IDE coding work belongs in Continue
  -> delegate_continue_task
  -> /api/continue/agent/execute
  -> registered Continue worker callback when capability-matched
  -> Continue completes the bounded subtask through its own Ask/tool loop
  -> daemon resumes control with the result
```

## Target Ask-Side Design

### LAO rule

Continue does not get a special side-channel around Ask.

When the prompt comes in from Continue, it should still flow through the same daemon Ask pipeline:

1. normal Ask classification
2. normal domain/phase detection
3. normal Phase Factory participation
4. normal capability/tool orchestration

Nothing special happens to the pipeline itself. The special behavior lives only in the Continue capability.

### Continue capability target

The target capability should expose one tool only:

- `delegate_continue_task`

That tool is not a low-level proxy for Continue's internal read/search/edit/navigation primitives.

Instead, it represents Continue's own local Ask pipeline as a bounded execution substrate. The daemon LLM uses it when the current slice of work is IDE-native coding that Continue should complete locally.

### BuildContext and BuildInstructions requirements

The Continue capability should be dynamic and metadata-aware.

`BuildContext()` should tell the daemon LLM:

- Continue is available as the editor-native coding orchestrator
- the current request originated from Continue when bridge metadata says so
- what phase/tool bounds apply to this request
- that the daemon remains the outer orchestrator

`BuildInstructions()` should tell the daemon LLM how to use the sole tool:

- delegate one bounded coding objective
- include phase intent and scope
- include allowed tools and preferred tool choice when present
- ask Continue to fully perform the IDE coding work and return the result
- resume daemon orchestration after the returned result

### Why this is the target

The goal is not for AI Studio to re-create Continue by composing Continue's low-level tools from the daemon side.

The goal is for Continue to successfully complete the IDE coding work it is already best at, then return the outcome so AI Studio can continue the broader platform, workflow, and multi-capability orchestration.

## Implemented

- native Continue chat transport at `/api/continue/chat`
- native Continue tool transport at `/api/continue/tools/{toolName}`
- daemon-owned session snapshots at `/api/continue/sessions/{sessionId}`
- daemon-owned request snapshots at `/api/continue/requests/{requestId}`
- worker registration at `/api/continue/worker/register`
- worker inspection at `/api/continue/workers/{sessionId}`
- metadata at `/api/continue/metadata`
- daemon-owned delegated execution at `/api/continue/agent/execute`
- transport and backend foundations for daemon-owned Continue delegation
- request-scoped execution correlation through `request_id` while keeping `session_id` as workstream identity
- current Ask-side capability scaffolding that can be evolved into the single-tool Continue participant
- provider/model propagation from native Continue chat into Ask payload variables
- provider/model propagation from the delegated Continue task contract into Continue worker execution payloads
- metadata flags declaring that provider/model are explicit transport attributes
- Continue provider forwarding of `tools` and `tool_choice`
- translation of daemon `tool_call` events into native Continue `assistant.toolCalls`
- surfaced Continue IDE-native tools for problems, definitions, type definitions, references, document symbols, and terminal contents
- removal of daemon route tests and code for `/v1/chat/completions`, `/v1/models`, and `/api/continue/config`
- removal of the AI Studio UI control that opened daemon-generated Continue config

## Remaining Work

Detailed workflow-surface analysis and SDLC boundary are documented in [docs/CONTINUE_IDE_WORKFLOW_SURFACE.md](docs/CONTINUE_IDE_WORKFLOW_SURFACE.md).

### 1. LAO Continue capability convergence

- simplify the Ask-visible Continue capability to one delegated tool: `delegate_continue_task`
- move session/metadata inspection into dynamic context and instructions instead of treating inspection as a primary long-term tool surface
- ensure `Classify()` treats Continue-originated IDE coding asks as normal Ask inputs that happen to have a Continue participant available
- ensure the daemon LLM receives enough context to compose a Continue-facing objective instead of imitating Continue's lower-level tools

### 2. Continue client wiring and workflow shaping

- ensure the Continue-side integration always calls the native `/api/continue/*` endpoints
- ensure the Continue-side request builder sends provider and model explicitly on chat and delegated execution requests
- ensure worker registration advertises only capabilities the editor callback truly supports
- define AI Studio workflow profiles that deliberately decide which Continue tools are exposed per request

### 2a. Ask2Ask contract hardening

- persist the final delegated worker result object in daemon-owned session/workflow state, not only transport status
- keep the final delegated worker snapshot addressable by `request_id`, not only by `session_id`
- keep `phase` and `mode` separate across the delegated execution contract; `phase` is workflow position, `mode` is Continue execution posture
- treat the final delegated result as a typed daemon object even when the Ask-visible tool also renders it as JSON/text for the model
- incrementally parse worker NDJSON streams so large delegated results do not require whole-body buffering on the proxy path
- define the streamed event schema alongside the final result schema so worker progress and file-change events remain stable across integrations

### 3. Authentication hardening

- validate bearer-token behavior across chat, tools, worker registration, and delegated execution
- finalize recovery UX for expired sessions without introducing a Continue-specific auth scheme

### 4. Session and workflow polish

- expand daemon-owned session snapshots when more editor state is useful
- add explicit request TTL and cancellation reconciliation across daemon restarts when a durable execution ledger is introduced
- keep Continue delegation steering focused on bounded execution and review slices
- extend tests around worker capability mismatch and local fallback behavior as needed
- consider surfacing low-risk remaining IDE context primitives such as open files, pinned files, and signature help

### 5. Broader SDLC layers above Continue

- add Git and review-specific workflow tools
- add CI/build visibility tools
- add deploy, rollback, logs, and environment-targeting workflows
- add documentation validation workflows such as build, preview, and link-check support

These are intentionally described as layers above Continue rather than Continue gaps. They are not primarily IDE-native or language-server-native capabilities, and they can be introduced later through AI Studio-native tools, desktop automation, and command-line execution patterns.

### 6. Continue-side setup guidance

- document the native participant flow for the VS Code/Continue integration
- avoid shipping workspace examples that imply daemon-owned OpenAI model entries are still the supported path

## Validation Commands

Primary daemon package validation:

```bash
export GOTELEMETRY=off && go test ./lib/sop/cmd/sop-daemon
```

Focused IDE worker validation:

```bash
export GOTELEMETRY=off && go test ./lib/sop/ai/agent/domains/ideworker
```

## Resume Checklist

Inspect these code surfaces first:

- `lib/sop/cmd/sop-daemon/client_bridge.go`
- `lib/sop/cmd/sop-daemon/rag.go`
- `lib/sop/cmd/sop-daemon/ide_worker_bridge.go`
- `lib/sop/ai/agent/domains/ideworker/capability.go`
- `lib/sop/cmd/sop-daemon/main_test.go`

Validate these behaviors before extending the design:

- `/api/continue/chat` still sends provider/model through Ask context and payload variables
- `/api/continue/agent/execute` still preserves provider/model through worker proxying and fallback execution
- delegated Continue execution remains correlated by `request_id` even when multiple requests share one `session_id`
- the Continue capability remains a passive participant and does not orchestrate phases
- the daemon Ask pipeline remains unchanged for Continue-originated requests
- the daemon LLM uses a single delegated Continue Ask tool instead of trying to mirror Continue's internal tool choreography
- capability matching is required before proxying to a registered Continue worker
- `/api/continue/metadata` still advertises the native contract and no daemon-owned config endpoint
- AI Studio does not expose a launcher for daemon-generated Continue config

## Guardrails

- Keep the daemon as the single workflow controller.
- Treat Continue as an editor surface and delegated worker.
- Prefer explicit request attributes over provider-specific transport semantics.
- Preserve provider neutrality at the daemon boundary.
- Avoid adding compatibility layers that lock the product into one provider's request shape.
