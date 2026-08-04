# Continue Facade Architecture

AI Studio and the SOP daemon remain the runtime and orchestrator in the default bridge flow. Continue is the editor-side companion that surfaces coding workflows inside the IDE while delegating execution to the daemon.

The SDK now also supports the reverse composition: another agent can use SOP Ask as a participant for a bounded delegated turn while keeping its own outer controller loop.

## The separation of responsibilities

### AI Studio / SOP daemon
The daemon is the system of record for:

- Ask and ReAct orchestration
- workflow state and approvals
- memory, history, and undo
- long-running execution and tool coordination
- provider and credential handling for the runtime

### Continue
Continue provides the IDE experience:

- editor context and cursor-aware prompts
- code-aware actions such as diffs, patching, and test execution
- a familiar chat surface inside the editor
- per-request provider overrides when the user wants to route a specific request through a different model or endpoint

## Why the bridge is thin

The Continue integration is intentionally a facade rather than a second orchestrator. Continue does not own the workflow loop. It packages the request, sends it to the daemon bridge, and returns the streamed result.

That keeps the architecture simple:

1. Continue captures editor context and user intent.
2. The bridge forwards the request to the daemon.
3. The daemon runs the normal AI Studio workflow.
4. The daemon returns progress, tool results, and final content back to Continue.

## Request flow

A typical Continue request flows through:

- the Continue client in the IDE
- the local or remote bridge endpoint
- the SOP daemon runtime
- the existing Ask pipeline and tool system

In practice, this means the daemon remains the single place that decides which capability to use, how to sequence tools, and how to manage state.

## Reverse-role mode

The default Continue integration keeps SOP in control, but SDK consumers are not limited to that shape.

When you want another agent to be the outer orchestrator, you can mount SOP Ask as a participant instead:

- the outer controller decides when to delegate to SOP
- SOP Ask runs one bounded Ask turn
- SOP Ask still controls its own internal capability selection, tool usage, and memory access
- control returns to the outer controller after the delegated turn completes

This is useful when Continue, another IDE agent, or a custom orchestration service wants to reuse SOP's Ask/ReAct runtime without surrendering top-level flow control.

### SDK surfaces

The participant-mode surfaces live in `lib/sop/ai/agent`:

- `Service.AskWithRequest(ctx, AskRequest)` for in-process embedding
- `NewParticipantHandler(...)` for exposing SOP Ask as an HTTP participant endpoint
- `ParticipantClient` for invoking a remote SOP Ask participant endpoint
- `ParticipantClient.AskStream(...)` for NDJSON streaming progress, content, and final response events

The transport request intentionally uses a JSON-safe participant session shape rather than the full internal session object so external controllers can pass portable state cleanly.

Participant requests may also carry `request_id` and `root_request_id`. `request_id` identifies the current delegated call. `root_request_id` identifies the whole controller-to-controller Ask tree. If SOP sees a `root_request_id` that SOP itself minted for outbound delegation, it rejects the request instead of allowing recursive Ask-to-Ask re-entry.

When the participant endpoint is backed by the registered copilot agent, the delegated request runs through SOP's queue-aware Ask pipeline before the final response is emitted.

For end-to-end examples, see [Ask Participant Mode](./ask-participant-mode).

## Provider and credential model

Continue can supply provider metadata per request, including:

- provider name
- API key
- model name
- base URL or endpoint

The daemon treats these values as request-scoped overrides when present. If no override is supplied, it can fall back to the configured runtime defaults.

This keeps the UI flexible while preserving a clear runtime boundary.

## Practical guidance

Use Continue when you want:

- a fast editor-native coding companion
- code-aware actions without leaving the IDE
- a thin integration over the same runtime used by AI Studio

Use the daemon directly when you want:

- full workflow orchestration
- approvals, memory, undo, and history
- multi-step execution that should be controlled by the platform runtime

Use Ask-as-participant mode when you want:

- another controller to own the outer workflow
- SOP Ask to contribute one bounded reasoning and tool-execution turn
- a clean transport or embedding seam without breaking the participant pattern inside SOP

## API shape

The bridge exposes Continue-style endpoints such as:

- `/api/continue/chat`
- `/api/continue/tools/<tool>`

These endpoints are designed to be simple adapters for Continue's request model while preserving the daemon's native execution model.
