# Continue Integration Architecture

**Status**: Active

## Summary

Continue is the IDE surface. AI Studio and the daemon are the controller.

That is the governing rule for this integration.

Continue can contribute editor context, diff-aware execution, and local workflow affordances, but it does not own orchestration, memory, approvals, or long-running workflow state.

## Control Boundary

### Daemon responsibilities

- Ask/ReAct orchestration
- provider routing decisions
- workflow phase progression
- approvals and policy rails
- session continuity and durable state
- memory and retrieval
- fallback execution when no editor worker is available

### Continue responsibilities

- chat and editor interaction surface inside VS Code
- active file, selection, and open-file context
- editor-aware execution when explicitly delegated
- worker capability advertisement and callback handling

## Why the OpenAI Facade Was Wrong

Surfacing the daemon as an OpenAI endpoint was rejected because it creates the wrong long-term boundary.

Problems with that shape:

- it locks the daemon contract to OpenAI request semantics
- it makes provider-neutral orchestration harder to preserve
- it shifts product language toward provider-specific transaction shapes
- it encourages config-driven model spoofing instead of explicit provider/model intent
- it obscures the fact that AI Studio is the controller

The correct design is a native Continue transport with explicit provider/model attributes.

## Native Continue Contract

Continue should talk to the daemon through native `/api/continue/*` routes.

Current transport surface:

- `/api/continue/chat`
- `/api/continue/tools/{toolName}`
- `/api/continue/sessions/{sessionId}`
- `/api/continue/requests/{requestId}`
- `/api/continue/agent/execute`
- `/api/continue/worker/register`
- `/api/continue/workers/{sessionId}`
- `/api/continue/metadata`

Key transport properties:

- `request_id` is the per-execution identity
- `session_id` remains the workstream identity
- provider is an explicit request attribute
- model is an explicit request attribute
- workspace and editor context are structured request fields
- responses stream as NDJSON
- daemon-owned metadata declares AI Studio as controller and Continue as editor worker
- daemon-owned metadata declares the authoritative AI Studio provider/model catalog through `control_model.supported_llms`

## Ask-Side Abstraction

The integration is not just a set of HTTP handlers. Inside Ask, Continue must participate through the same LAO pipeline as every other daemon request.

### Core rule

Nothing about the daemon Ask pipeline changes for Continue:

- the incoming Continue request still goes through normal Ask classification
- the daemon LLM still determines domain and phase
- the normal Phase Factory still decides which phase proxies participate
- Continue is just one capability participant among the others

This means the daemon LLM can still orchestrate across other capabilities such as Spaces, Stores, Desktop, or workflow memory when the request needs that broader support.

### Continue capability target shape

The target Ask-side shape is a Continue-specific LAO participant, not a mirrored tool catalog.

- capability role: supporting participant for IDE-native coding execution
- primary responsibility: tell the daemon LLM when and how to delegate one bounded coding objective to Continue
- exposed tool count: one

Recommended tool shape:

- `delegate_continue_task`

That sole tool is the embodiment of Continue's own Ask pipeline. The daemon LLM uses it when a bounded slice of the request should be completed by Continue locally inside the editor.

### Dynamic context and instructions

The Continue capability should be dynamic per request.

- `Classify()` recognizes Continue-originated IDE coding asks and contributes relevance like any other capability
- `BuildContext()` tells the daemon LLM that Continue is the editor-native coding orchestrator for bounded subwork
- `BuildInstructions()` teaches the daemon LLM how to compose the delegated objective for Continue's own Ask loop

The delegated task contract should include:

- coding objective
- target phase intent
- workflow profile when available
- allowed tool subset when bounded by request metadata
- success criteria
- expected result shape returned to the daemon

### Transitional note

The current codebase may still contain a more generic `ide_worker` wrapper shape and compatibility aliases such as `inspect_ide_worker` and `delegate_ide_coding_task`.

Those should be treated as transitional implementation surfaces, not the target architecture. The implementation direction is to converge on the single-tool Continue LAO participant described above.

## Execution Model

### Normal Continue chat

```text
Continue user request
  -> /api/continue/chat
  -> build structured Ask payload
  -> daemon Ask turn
  -> daemon tools and capabilities
  -> stream result back to Continue
```

### Delegated editor-aware task

```text
daemon Ask turn
  -> classify normally across phases/domains/capabilities
  -> Continue capability contributes dynamic context/instructions
  -> daemon LLM decides that a bounded IDE coding step should be delegated
  -> delegate_continue_task
  -> Continue runs its own local Ask/tool loop
  -> Continue returns finished result to daemon
  -> daemon continues the larger workflow
```

Important rule:

- delegation is one bounded subtask, not a transfer of control

## Provider and Model Propagation

Provider/model intent now flows through both major paths.

### Chat path

- Continue sends `provider` and `model` on `/api/continue/chat`
- the daemon copies them into Ask config as `provider_details`
- the daemon also copies them into `SessionPayload.Variables` as `llm_provider` and `llm_model`

### Delegated worker path

- the delegated Continue capability tool reads `llm_provider` and `llm_model` from the current Ask payload
- the task request contract carries `Provider` and `Model`
- the task request contract carries `RequestID` as the canonical execution identifier
- the Continue backend writes those fields into `/api/continue/agent/execute`
- fallback execution preserves the same provider/model values
- delegated Ask2Ask execution should also preserve `mode` and `requested_phase` as separate fields
- the daemon should persist the final structured delegated result object, including `modified_files`, diff references, and metadata, after proxying streamed worker output

## Current State

Implemented now:

- native Continue transport and metadata
- daemon-owned session tracking
- daemon-owned request tracking for delegated execution
- capability-aware worker delegation
- Ask-side Continue capability scaffolding for single-tool delegated orchestration
- second backend implementation proving the abstraction is not Continue-specific
- provider/model propagation through chat and delegated execution
- hybrid Ask2Ask delegated-result contract with streamed progress and a final structured execution result
- Continue provider forwarding of `tools` and `tool_choice`
- translation of daemon `tool_call` events into native Continue tool calls
- surfaced IDE-native debugging and navigation tools through Continue's built-in tool layer
- AI Studio UI cleanup removing daemon-generated Continue config launch affordances

Removed:

- `/v1/chat/completions`
- `/v1/models`
- `/api/continue/config`
- daemon-owned shadow-model discovery as the canonical integration story

## Design Guardrails

- Ask remains the only controller.
- Capabilities remain passive participants.
- Continue remains an editor surface and delegated worker.
- Provider identity is explicit and transport-neutral.
- Do not reintroduce provider-specific facade contracts as the primary architecture.

## Workflow Surface Positioning

Continue is now strong enough to serve as the coding-workflow and IDE-execution substrate for AI Studio.

Current strengths:

- repository read and search
- file mutation and patch-style editing
- terminal execution
- diff inspection
- IDE diagnostics and navigation

Current boundary:

- strong engineering inner loop
- not yet the full SDLC surface for CI, deployment, rollback, release, and operations

That broader SDLC layer should be added above Continue at the AI Studio orchestration layer rather than by turning Continue into the workflow controller.

Those workflows are not primarily IDE or language-server responsibilities. They can be added later through AI Studio-native workflow tools, desktop automation, and command-line execution without changing Continue's role as the IDE substrate.

## Related Docs

- `docs/CONTINUE_BRIDGE_API_SPEC.md`
- `docs/CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md`
- `docs/CONTINUE_IDE_WORKFLOW_SURFACE.md`
- `docs/ASK_V2_ARCHITECTURE.md`
- `docs/DOMAIN_AGNOSTIC_PHASE_SYSTEM.md`
