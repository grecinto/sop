# Cline SDK Host Prototype

This is the daemon-managed TypeScript host for the Cline SDK. It does not require
VS Code or IntelliJ. The Go daemon sends newline-delimited JSON requests on stdin;
the host emits newline-delimited JSON lifecycle events on stdout.

## Protocol

Run a task:

```json
{"type":"run","requestId":"req-1","sessionId":"sop-session-1","clineSessionId":"cline-workstream-1","task":"Run the failing tests and fix the implementation","workspaceRoot":"/path/to/repo","providerId":"openai","modelId":"gpt-5","apiKey":"...","mode":"act","guidance":{"successCriteria":["Tests pass"]}}
```

Cancel a task:

```json
{"type":"cancel","requestId":"req-1"}
```

Stop the host:

```json
{"type":"shutdown"}
```

The host emits `started`, `progress`, `result`, `error`, `cancelled`,
`session_invalidated`, and `stopped` events. The first run for a `clineSessionId`
uses `core.start()`; later runs for that same session use `core.send()`. Keep the
host alive between same-workstream tasks. The daemon provides one host per
workstream and workspace/provider configuration, serializes requests to it,
expires idle hosts, and starts with fresh sessions after daemon restart. A
confirmed SOP restore emits `session_invalidated`; the daemon then retires that
host before accepting the next task. The result is normalized into a status/summary/metadata
payload; the daemon adapter converts it into `ideworker.ExecutionResult`, adds
workspace diff data, and records the task through SOP's shared undo and
execution-memory pipeline.

When `mcpUrl` is provided, or `SOP_AI_STUDIO_MCP_URL` is set, the host adds the
`ai_studio_get_task_guidance`, `ai_studio_search_memory`,
`ai_studio_search_undo_history`, and `ai_studio_record_validation` tools to the
Cline session. The host also exposes `ai_studio_restore_undo_transaction`, which
requires explicit confirmation and marks the related session stale. Those tools
call the authenticated AI Studio MCP endpoint during the
agent loop. Set
`SOP_AI_STUDIO_MCP_TOKEN` for the daemon bearer token, or provide
`mcpBearerToken` in the request.

The daemon endpoint is `POST http://127.0.0.1:9090/mcp` and currently supports
MCP `initialize`, `tools/list`, and `tools/call` for `get_task_guidance` and
`search_memory`, read-only `search_undo_history`, `record_validation`, and
confirmed `restore_undo_transaction`.

## Local development

This package targets Node 22 and the published `@cline/core` package. Install
and build it from this directory:

```sh
npm install
npm run build
printf '%s\n' '{"type":"run","requestId":"req-1","clineSessionId":"cline-workstream-1","task":"Inspect the tests and report the first failure","workspaceRoot":"/tmp/repo","providerId":"openai","modelId":"gpt-5"}' | npm start
```

Create a deployable host directory:

```sh
./package-runtime.sh /path/to/release/cline-sdk-host
```

The host boundary carries separate SOP session, Cline session, and request
identities so MCP calls are scoped to the same SOP workflow without sharing
Cline conversation state across unrelated workstreams. This host uses pipes;
PTY remains reserved for terminal interaction and the explicit CLI fallback.

An operator can opt in to one same-provider in-session model escalation by
setting `SOP_CLINE_ESCALATION_MODEL_ID` in the daemon environment to a manually
selected model ID. Cline may request that switch through
`ai_studio_escalate_cline_model` after concrete repeated reasoning or progress
failure; the tool is not offered unless a distinct target is configured. The
SDK changes the active session model while retaining its history, and the host
restores the original model before returning the task result. Provider,
network, authentication, and rate-limit failures do not trigger this path.
Successful escalation and any failed restoration are included in result
metadata. Cross-provider switching and automatic model ranking are not enabled.

Run focused host tests with `npm test`.

After each task, the host best-effort reads Cline's accumulated usage before and
after the turn. Result metadata reports task deltas and session totals for both
root and aggregate usage, including input/output/cache tokens and total cost.
Failure to read usage does not fail the task. Workspace diffs and completeness
metadata are added by the Go daemon adapter, not by this host.
