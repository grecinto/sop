# Continue Facade Architecture

AI Studio provides the runtime and orchestration layer in the default bridge flow. Continue is the editor-side companion that surfaces coding assistance inside the IDE while delegating execution to AI Studio.

The SDK also supports the reverse composition: another agent can use AI Studio Ask as a participant for a bounded delegated turn while keeping its own outer control loop.

## The separation of responsibilities

### AI Studio runtime
AI Studio provides:

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

The Continue integration is a focused connection to the AI Studio runtime. Continue packages the request, sends it through the bridge, and returns the streamed result.

A useful architectural distinction is:

- AST/tree-sitter and repo indexing remain the core source of structural understanding for planning and investigation.
- Continue is the preferred IDE-native surface for richer semantic actions once the workflow needs them, such as go-to-definition, find references, rename-oriented refactors, type-aware navigation, and richer completion assistance.

That keeps the default workflow simple: AST-based repo understanding is available from the start, while Continue adds higher-value editor-native semantics later without changing the orchestration boundary.

That keeps the architecture simple:

1. Continue captures editor context and user intent.
2. The bridge forwards the request to AI Studio.
3. AI Studio runs the normal task flow.
4. AI Studio returns progress, tool results, and final content back to Continue.

## Request flow

A typical Continue request flows through:

- the Continue client in the IDE
- the local or remote bridge endpoint
- the AI Studio runtime
- the existing Ask pipeline and tool system

In practice, AI Studio decides which capability to use, how to sequence tools, and how to manage state.

## Reverse-role mode

The default Continue integration keeps AI Studio in control, while SDK consumers can also choose a participant arrangement.

When you want another agent to be the outer orchestrator, you can use AI Studio Ask as a participant instead:

- the outer controller decides when to delegate to AI Studio
- AI Studio Ask runs one bounded Ask turn
- AI Studio Ask still manages its own capability selection, tool usage, and memory access
- control returns to the outer controller after the delegated turn completes

This is useful when Continue, another IDE agent, or a custom orchestration service wants to reuse AI Studio's Ask/ReAct runtime while retaining top-level flow control.

### SDK surfaces

The participant-mode surfaces live in `lib/sop/ai/agent`:

- `Service.AskWithRequest(ctx, AskRequest)` for in-process embedding
- `NewParticipantHandler(...)` for exposing AI Studio Ask as an HTTP participant endpoint
- `ParticipantClient` for invoking a remote AI Studio Ask participant endpoint
- `ParticipantClient.AskStream(...)` for NDJSON streaming progress, content, and final response events

The transport request intentionally uses a JSON-safe participant session shape rather than the full internal session object so external controllers can pass portable state cleanly.

Participant requests may also carry `request_id` and `root_request_id`. `request_id` identifies the current delegated call. `root_request_id` identifies the whole controller-to-controller Ask tree. If AI Studio sees a `root_request_id` that it minted for outbound delegation, it rejects the request instead of allowing recursive Ask-to-Ask re-entry.

When the participant endpoint is backed by the registered copilot agent, the delegated request runs through AI Studio's queue-aware Ask pipeline before the final response is emitted.

For end-to-end examples, see [Ask Participant Mode](./ask-participant-mode).

## Provider and credential model

Continue can supply provider metadata per request, including:

- provider name
- API key
- model name
- base URL or endpoint

AI Studio treats these values as request-scoped overrides when present. If no override is supplied, it can fall back to the configured runtime defaults.

This keeps the UI flexible while preserving a clear runtime boundary.

## Practical guidance

Use Continue when you want:

- a fast editor-native coding companion
- code-aware actions without leaving the IDE
- a thin integration over the same runtime used by AI Studio

Use AI Studio directly when you want:

- full workflow orchestration
- approvals, memory, undo, and history
- multi-step execution that should be controlled by the platform runtime

Use Ask-as-participant mode when you want:

- another controller to own the outer workflow
- AI Studio Ask to contribute one bounded reasoning and tool-execution turn
- a clean transport or embedding boundary while preserving AI Studio's orchestration model

## API shape

The bridge exposes Continue-style endpoints such as:

- `/api/continue/chat`
- `/api/continue/tools/<tool>`

These endpoints are designed to be simple adapters for Continue's request model while preserving AI Studio's native execution model.
