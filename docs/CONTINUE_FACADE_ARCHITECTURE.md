# Continue Integration Architecture

**Status**: Active

## Summary

Continue is the IDE surface and the preferred execution backend. Continue owns the coding workflow execution loop, while AI Studio and the daemon remain thin recorders and policy/memory owners.

Design goal: keep the bridge thin and reuse Continue's own AST/tree-sitter, editor context, search, navigation, and local execution primitives rather than reimplementing them in the daemon.

That is the governing rule for this integration.

This design fits our pluggable capability system: the platform is composed of general capabilities, domain-specific capabilities, and runtime bridge capabilities. Continue is not a second architecture or a parallel capability framework. It is a specialized IDE-native worker that can be connected into the same capability-based Ask pipeline through a thin adapter or participant tool.

The capability stack therefore looks like this:

- General capabilities: routing, memory, retrieval, policy, session continuity, approvals, and common orchestration behaviors.
- Domain capabilities: coding workflow, desktop automation, research, and other task-specific execution surfaces.
- Bridge capabilities: Continue, editor workers, IDE adapters, and external automation surfaces that contribute local execution power without owning the authoritative Ask loop.

For SDK reuse, the same Ask runtime can also be mounted in reverse as a participant under another controller. That does not change the internal rule above for the default Continue bridge. It adds a second supported boundary: another agent may call SOP Ask as a delegated worker while SOP Ask still controls its own inner capability pipeline.

Continue can contribute editor context, diff-aware execution, and local workflow affordances. For the coding workflow itself, Continue owns orchestration inside the delegated execution slice, including investigation, test fixing, and feature implementation using its internal AST/tree-sitter and editor-native tools. AI Studio retains durable memory, KnowledgeBase guidance, approvals, and visible history recording.

In practical terms, Continue should be able to take a prompt and complete the coding workflow end to end, then return structured file diffs, deltas, and execution metadata to AI Studio for persistence and undo/history tracking.

The implementation boundary should be a small backend API that can be swapped between Continue and the daemon without changing the controller contract.

If Continue already provides a capability natively, the bridge should call that capability and return its structured result instead of duplicating the logic in daemon code.

## Capability-first integration model

Continue should be treated as a capability adapter rather than as a replacement for the platform model.

In practice, that means:

- general capabilities remain the shared foundation for all domains
- coding workflow remains a domain capability with its own phase logic and repository-aware behavior
- desktop remains a domain capability with OS and shell automation behavior
- Continue is an IDE bridge capability that exposes one bounded local execution channel for editor-native tasks
- the daemon Ask loop remains the controller for AI Studio-facing orchestration, unification, and durable memory, while Continue owns the delegated coding workflow loop

This keeps the architecture aligned with the project’s intent: a capability-pluggable design system where specialists can be added without rewriting the controller or the core orchestration model.

For the detailed behavior and interface contract, see [CONTINUE_COPILOT_INTEGRATION_CONTRACT.md](CONTINUE_COPILOT_INTEGRATION_CONTRACT.md).

## LLM interaction contract

The relevant lesson from Continue is not about replacing the orchestration model. It is about letting Continue own the delegated coding workflow turn while keeping AI Studio responsible for prompt capture, memory, approvals, and visible history.

The interaction contract for the Ask/ReAct system should therefore be:

- Ask remains the controller of platform-level workflow progression, memory, and phase transitions, but the coding workflow execution inside the delegated slice is owned by Continue.
- Capabilities remain passive participants that classify tasks, inject context, expose tools, and return bounded work.
- General, domain, and bridge capabilities each contribute within their own scope; they do not bypass the Ask loop.
- A delegated IDE worker such as Continue participates through an explicit bounded tool or participant boundary and owns the coding workflow inside that boundary.
- Before editing, the LLM must state the immediate objective and the expected scope in one sentence.
- Prefer the smallest valid change over a broad rewrite; do not expand scope unless the task requires it.
- If a task is multi-step, sequence it through the Ask workflow rather than collapsing it into one uncontrolled loop.
- Maintain the current user intent, workstream, and plan unless the user explicitly changes scope.
- If behavior changes, update the relevant tests or validation checks to match the new contract; stale tests are not assumed valid.
- Validate with the smallest relevant command or check that exercises the changed behavior.
- If a sub-task is editor-native or IDE-specific, delegate it to the bridge capability and return a result that the outer workflow can continue from.

This is the practical adaptation of Continue’s strengths: better local execution discipline, bounded steps, and ownership of the coding workflow loop, while retaining AI Studio’s stronger orchestration, memory, and history recording.

## AST vs. Continue semantic layer

The default workflow should keep using AST/tree-sitter and repo-indexing primitives as the primary source of structural understanding for planning and investigation.

Continue is the intended IDE-native bridge for richer semantic actions once the workflow needs them. The important distinction is:

- AST/tree-sitter remains the foundation for repo structure, symbols, imports, and lightweight structural reasoning.
- Continue is the preferred surface for deeper IDE-native actions such as go-to-definition, go-to-type-definition, find references, document symbols, rename-style refactors, and richer type-aware navigation/completion assistance.
- These features are optional enhancements, not a prerequisite for the initial planning/execution loop, but they should remain part of the long-term Continue integration plan.

## Control Boundary

### AI Studio and daemon responsibilities

- Ask/ReAct orchestration around the coding workflow
- provider routing decisions
- approvals and policy rails
- session continuity and durable state
- prompt servicing, prompt ledger, and visible change-history storage
- memory and retrieval
- fallback execution when no editor worker is available

### Continue responsibilities

- chat and editor interaction surface inside VS Code
- active file, selection, and open-file context
- editor-aware coding workflow execution when explicitly delegated
- prompt-level coding workflow execution, including investigation, test fixing, and feature implementation when explicitly delegated
- worker capability advertisement and callback handling
- structured file-change reporting for history and undo cards

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

The worker-facing result contract should remain structured enough for AI Studio to render a complete history entry. At minimum, the returned result should include modified files, diff summaries, change identifiers, optional undo references, validation metadata, and any knowledge or memory references needed to reconstruct the reasoning trail.

AI Studio is the place where KnowledgeBase, memory tracking, prompt recording, and undo history live. Continue is the place where the coding work itself is performed.

## Ask-Side Abstraction

The integration is not just a set of HTTP handlers. Inside Ask, Continue must participate through the same LAO pipeline as every other daemon request.

### Core rule

Nothing about the daemon Ask pipeline changes for Continue:

- the incoming Continue request still goes through normal Ask classification
- the daemon LLM still determines domain and phase
- the normal Phase Factory still decides which phase proxies participate
- Continue is just one capability participant among the others

This means the daemon LLM can still orchestrate across other capabilities such as Spaces, Stores, Desktop, or workflow memory when the request needs that broader support.

## Reverse-Role Support

The bridge architecture is now deliberately versatile enough to support the opposite composition as well.

### Supported reversal

- Continue or another agent may act as the outer controller.
- SOP Ask may act as a participant for one delegated turn.
- SOP still preserves LLM-as-Orchestrator inside that delegated turn.

### Boundary for reversal

The reversal is supported only at the service or transport boundary, not by turning capabilities into controllers.

- In-process embedding uses `Service.AskWithRequest(ctx, AskRequest)`.
- HTTP exposure uses `NewParticipantHandler(...)`.
- Remote invocation uses `ParticipantClient`.
- Live streaming remote invocation uses `ParticipantClient.AskStream(...)` with `Accept: application/x-ndjson`.

Participant-mode requests can also carry `request_id` and `root_request_id`. `request_id` identifies the current delegated participant call. `root_request_id` identifies the full Ask-to-Ask delegation tree. SOP generates a SOP-owned root when it delegates outward and rejects any later participant request that re-enters SOP carrying that SOP-owned root, which prevents controller-to-controller recursion by durable identity.

This keeps the architecture coherent:

- outer controller decides when to call SOP Ask
- SOP Ask decides how its own capabilities and tools are used
- capabilities inside SOP remain passive participants

### Continue capability target shape

The target Ask-side shape is a Continue-specific LAO participant, not a mirrored tool catalog.

- capability role: supporting participant for IDE-native coding execution
- primary responsibility: tell the daemon LLM when and how to delegate one bounded coding objective to Continue, which then owns that coding workflow slice
- exposed tool count: one

Recommended tool shape:

- `delegate_continue_task`

That sole tool is the embodiment of Continue's own Ask pipeline. The daemon LLM uses it when a bounded slice of the request should be completed by Continue locally inside the editor.

For the refactor plan, treat that worker behavior as one implementation of a broader workflow-backend interface. The controller still talks to one prompt-service contract, while Continue supplies the shipping worker implementation.

Recommended contract shape:

- `PromptService` records prompts, events, and completion results for AI Studio.
- `CodingWorkflowWorker` executes bounded investigation/test-fix work and returns structured change history.
- `WorkflowBackend` combines the two behind a configurable backend selector such as `continue` or `daemon`.

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
- In reverse-role mode, SOP Ask may be the participant of another controller, but SOP capabilities still never become controllers.
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
