# Continue Facade Architecture

AI Studio and the SOP daemon remain the runtime and orchestrator. Continue is the editor-side companion that surfaces coding workflows inside the IDE while delegating execution to the daemon.

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

## API shape

The bridge exposes Continue-style endpoints such as:

- `/api/continue/chat`
- `/api/continue/tools/<tool>`

These endpoints are designed to be simple adapters for Continue's request model while preserving the daemon's native execution model.
