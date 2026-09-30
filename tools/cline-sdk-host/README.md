# Cline SDK Host Prototype

This is the daemon-managed TypeScript host for the Cline SDK. It does not require
VS Code or IntelliJ. The Go daemon sends newline-delimited JSON requests on stdin;
the host emits newline-delimited JSON lifecycle events on stdout.

## Protocol

Run a task:

```json
{"type":"run","requestId":"req-1","sessionId":"session-1","task":"Run the failing tests and fix the implementation","workspaceRoot":"/path/to/repo","providerId":"openai","modelId":"gpt-5","apiKey":"...","mode":"act","guidance":{"successCriteria":["Tests pass"]}}
```

Cancel a task:

```json
{"type":"cancel","requestId":"req-1"}
```

Stop the host:

```json
{"type":"shutdown"}
```

The host emits `started`, `progress`, `result`, `error`, `cancelled`, and
`stopped` events. The result is normalized into a status/summary/metadata
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
printf '%s\n' '{"type":"run","requestId":"req-1","task":"Inspect the tests and report the first failure","workspaceRoot":"/tmp/repo","providerId":"openai","modelId":"gpt-5"}' | npm start
```

The host boundary carries session and request identity so MCP calls are scoped
to the same SOP workflow.
