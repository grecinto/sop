# Continue Native Transport API Spec

**Status**: Active

## Purpose

This document defines the native transport between Continue and AI Studio from Northstar AI.

The AI Studio daemon remains the controller. Continue is an editor-facing participant that sends provider/model intent, workspace context, and bounded coding tasks into the daemon-owned Ask/ReAct runtime.

## Design Rules

- Do not surface the daemon as an OpenAI-compatible provider.
- Do not use `/v1/chat/completions`, `/v1/models`, or `/api/continue/config` as canonical integration points.
- Provider and model are explicit request attributes on native Continue requests.
- The daemon owns orchestration, workflow state, approvals, memory, and fallback behavior.
- Continue may execute editor-aware subtasks, but only as a delegated worker.
- The current Continue extension implements the Ask2Ask-style continuation flow: it persists HQ and daemon auth state in VS Code secret storage, rehydrates that state on startup, and refreshes or re-handshakes the session so restarts do not immediately break delegated AI Studio work.
- Delegated Continue requests are intentionally task-scoped and single-turn. They should carry the current IDE subtask payload rather than depending on Continue conversation history, because the daemon and AI Studio remain the authoritative memory layer for prior Ask prompts and workflow state.

## Base URL

```text
http://127.0.0.1:9090
```

## Authentication

Use the existing daemon/HQ bearer-token flow.

- Continue owns two distinct authenticated sessions:
  - an HQ session for Northstar HQ access, renewal, and periodic re-authentication
  - a daemon-local session for `http://127.0.0.1:9090` bridge calls
- Requests should include `Authorization: Bearer <daemon-access-token>` when auth is enabled.
- `401` and `403` from the daemon should be handled through the daemon-local sign-in and token refresh flow.
- No Continue-specific daemon credential scheme should be introduced.
- Local helper validation may bootstrap through `POST /api/daemon/auth` using the daemon bootstrap secret or an existing bootstrap token, then cache and refresh daemon bearer tokens through `POST /api/daemon/refresh`.
- Continue must authenticate to HQ separately and manage its own HQ access token, HQ refresh token, and re-authentication cadence independently from the UI session.
- Continue persists both the HQ token set and the daemon token set in VS Code secret storage. On activation, Continue rehydrates those values, refreshes them when stale, and can re-establish the daemon handoff after a restart.
- When Continue calls daemon endpoints that may later trigger federation work, Continue must also provide its current HQ connectivity context so daemon can reuse the same HQ identity for outbound sync work.

In the common handoff case, daemon may also provide HQ connectivity and token context back to Continue after the user has already logged in through AI Studio UI. That lets the user move from AI Studio into IDE coding without performing a second HQ login immediately.

### Dual-token model

Continue keeps two independent token sets:

1. HQ token set
  - `hq_access_token`
  - `hq_refresh_token`
  - expiry metadata and re-authentication state
2. Daemon token set
  - `daemon_access_token`
  - `daemon_refresh_token`
  - expiry metadata

These token sets must never be substituted for one another. The HQ token set authenticates Continue with Northstar HQ. The daemon token set authenticates Continue with the local daemon.

For user experience, the HQ token set may be initialized from daemon-provided handoff state when the user already authenticated through AI Studio UI. After handoff, Continue treats that HQ session as its active HQ context and is responsible for continuing refresh and re-authentication behavior from there.

### HQ context propagation into daemon

Continue must pass its active HQ connectivity context into daemon-owned flows whenever daemon may need to perform later HQ federation work on behalf of the same Continue session.

The minimum propagated fields are:

- `hq_base_url`
- `hq_user_id`
- `hq_session_id`
- Continue-owned HQ `access_token`
- Continue-owned HQ `refresh_token` when daemon-side renewal or long-lived federation work needs to survive beyond the current request

The daemon treats this HQ context as session-scoped metadata, not as a replacement for daemon bearer auth.

The same path also works in reverse: if daemon already holds fresh HQ connectivity for the current user session, daemon may include that HQ context in delegated worker callbacks so Continue can adopt it and avoid a redundant login prompt.

## Endpoints

### POST /api/continue/chat

Primary Continue-to-daemon request path.

Responsibilities:

- accepts the current user prompt
- preserves provider/model intent explicitly
- carries structured workspace/editor context
- may carry Continue-native tool definitions and tool-choice hints for IDE-side workflow execution
- streams daemon-owned progress and final content back to Continue
- records daemon-owned session snapshots

Continue is expected to use its normal tool system as the IDE execution surface. AI Studio remains the controller and may use `tools` and `tool_choice` to constrain or steer which Continue tools are callable for a given request.

The daemon should treat this as metadata, not just transport. The integration contract includes the ability to describe:

- supported Continue phases
- phase-to-tool availability
- mode constraints per phase
- request-scoped forwarded tool availability

#### Request body

```json
{
  "session_id": "workspace-42",
  "message": "Refactor the parser error handling",
  "provider": "anthropic",
  "model": "claude-sonnet-4-5",
  "api_key": "provider-key-from-continue",
  "url": "https://api.anthropic.com",
  "workspace": {
    "root": "/path/to/repo",
    "active_file": "src/parser/handler.go",
    "selection": {
      "start_line": 40,
      "end_line": 90
    }
  },
  "context": {
    "open_files": ["src/parser/handler.go", "src/parser/handler_test.go"],
    "selected_text": "func parseHandler(...)"
  },
  "tools": [
    {
      "type": "function",
      "displayTitle": "Edit File",
      "group": "AI Studio Workflows",
      "readonly": false,
      "function": {
        "name": "edit_file",
        "description": "Edit an existing file",
        "parameters": {
          "type": "object"
        }
      }
    }
  ],
  "tool_choice": {
    "type": "function",
    "function": {
      "name": "edit_file"
    }
  },
  "mode": "edit"
}
```

#### Ask payload mapping

The daemon maps the request into shared Ask state.

Structured variables currently propagated into `SessionPayload.Variables` include:

```json
{
  "continue_bridge": true,
  "continue_mode": "edit",
  "coding_workflow_profile": "refactoring",
  "coding_task_type": "refactoring",
  "requested_phase": "execution",
  "workspace_root": "/path/to/repo",
  "active_file": "src/parser/handler.go",
  "selected_text": "func parseHandler(...)",
  "open_files": ["src/parser/handler.go", "src/parser/handler_test.go"],
  "continue_available_tools": ["read_file", "multi_edit", "get_problems"],
  "continue_phase_tools": ["read_file", "view_diff", "edit_existing_file", "single_find_and_replace", "multi_edit", "run_terminal_command"],
  "continue_phase_available_tools": ["read_file", "multi_edit"],
  "continue_tool_choice": "multi_edit",
  "llm_provider": "anthropic",
  "llm_model": "claude-sonnet-4-5",
  "llm_url": "https://api.anthropic.com"
}
```

This allows AI Studio capabilities to consume Continue availability as structured data rather than relying on special-case integration logic.

#### Response format

The endpoint streams NDJSON events.

Supported event types:

- `progress`
- `tool_call`
- `tool_result`
- `content`
- `error`
- `done`

When `tool_call` events are emitted, the Continue AI Studio provider translates them into native Continue `assistant.toolCalls` messages so IDE-side tools such as file edit, multi-edit, diff review, and terminal execution can be executed through Continue's normal tool pipeline.

This pipeline now also supports surfaced IDE-native diagnostics and navigation tools including problems, definitions, type definitions, references, document symbols, and terminal contents.

See [docs/CONTINUE_IDE_WORKFLOW_SURFACE.md](docs/CONTINUE_IDE_WORKFLOW_SURFACE.md) for the current workflow inventory and SDLC coverage boundary.

#### Example response

```json
{"type":"progress","message":"Preparing local workspace context"}
{"type":"content","delta":"I'm connecting your request to the local AI Studio runtime."}
{"type":"content","delta":"I found the parser branch that needs refactoring."}
{"type":"done"}
```

### POST /api/continue/tools/{toolName}

Invokes one daemon-owned tool with structured input.

Current tool surface:

- `repo.read_file`
- `repo.search_code`
- `repo.list_files`
- `code.apply_patch`
- `code.preview_patch`
- `test.run`
- `agent.ask`

#### Example request

```http
POST /api/continue/tools/repo.search_code
Content-Type: application/json
```

```json
{
  "query": "parse_error",
  "path": "src/",
  "limit": 20
}
```

#### Example response

```json
{
  "ok": true,
  "tool": "repo.search_code",
  "result": {
    "hits": [
      {
        "path": "src/parser/handler.go",
        "line": 42,
        "text": "return parseError(err)"
      }
    ]
  }
}
```

### GET /api/continue/sessions/{sessionId}

Returns daemon-owned workflow state for the Continue session.

Identity rules:

- `session_id` is the workstream or conversation identity
- `request_id` is the bounded execution identity
- the session view returns the latest daemon-owned snapshot for that session

#### Example response

```json
{
  "request_id": "request-123",
  "session_id": "workspace-42",
  "status": "in_progress",
  "phase": "execution",
  "mode": "edit",
  "workflow_profile": "refactoring",
  "task_type": "refactoring",
  "message": "Refactor the parser error handling",
  "updated_at": "2026-08-03T12:34:56Z",
  "open_files": ["src/parser/handler.go", "src/parser/handler_test.go"],
  "active_file": "src/parser/handler.go",
  "workspace_root": "/path/to/repo"
}
```

### GET /api/continue/requests/{requestId}

Returns daemon-owned workflow state for one bounded Continue execution.

Use this endpoint when multiple delegated executions may happen under the same `session_id` and the caller needs a definitive handle for progress, timeout, cancellation, or result correlation.

#### Example response

```json
{
  "request_id": "request-123",
  "session_id": "workspace-42",
  "status": "completed",
  "phase": "execution",
  "mode": "apply",
  "workflow_profile": "refactoring",
  "task_type": "refactoring",
  "message": "Refactor the parser error handling",
  "updated_at": "2026-08-03T12:34:56Z",
  "open_files": ["src/parser/handler.go", "src/parser/handler_test.go"],
  "active_file": "src/parser/handler.go",
  "workspace_root": "/path/to/repo"
}
```

### POST /api/continue/agent/execute

Allows the daemon to delegate one bounded editor-aware coding task to Continue.

This is the transport behind the daemon Ask capability's delegated Continue task tool.

#### Request body

```json
{
  "request_id": "request-123",
  "session_id": "workspace-42",
  "task": "Apply the prepared patch to the parser handler",
  "provider": "gemini",
  "model": "gemini-2.5-pro",
  "api_key": "provider-key-from-continue",
  "url": "https://generativelanguage.googleapis.com",
  "requested_phase": "execution",
  "allowed_tools": ["read_file", "apply_patch", "get_problems"],
  "tool_choice": "apply_patch",
  "guidance": {
    "team_practices": ["Preserve public interfaces", "Add targeted tests"],
    "success_criteria": ["Return a structured execution result", "Keep changes bounded to the selected file"],
    "constraints": ["Do not widen the public API"],
    "implementation_notes": ["Prefer existing repository logging/error patterns"],
    "expected_result_format": "structured_execution_result",
    "reference_context": [
      {"kind": "ltm_note", "source": "Team Patterns/Payments", "title": "Payments practices", "content": "Prefer explicit error wrapping for service-layer failures."}
    ]
  },
  "result_contract": {
    "stream_progress": true,
    "include_modified_files": true,
    "include_diff_deltas": true,
    "include_metadata": true,
    "prefer_artifact_refs": true,
    "max_inline_diff_bytes": 16384
  },
  "context": {
    "workspace_root": "/path/to/repo",
    "active_file": "src/parser/handler.go",
    "selected_text": "func parseHandler(...)",
    "open_files": ["src/parser/handler.go"]
  },
  "mode": "apply"
}
```

#### Execution rules

- the daemon owns the authoritative `request_id` for each bounded delegated execution; Continue may send a caller-provided value, but the daemon uses its own generated identity for lifecycle tracking and correlation
- `request_id` is the canonical identifier for correlation, timeout, cancellation, and concurrent delegated work
- `session_id` remains the conversation or workstream identity and may span multiple delegated executions
- if a Continue worker is registered for the session and advertises the required capability set, the daemon proxies the task to that worker
- if no worker is registered, the daemon falls back to local Ask execution
- if a worker is registered but lacks the required capabilities, the daemon falls back to local Ask execution
- provider/model intent must be preserved in both the proxy path and the fallback path
- rich guidance such as LTM-derived team practices, success criteria, and constraints should be passed as structured parameters rather than hidden only in the freeform task string
- the daemon remains the system of record for session state in all cases

#### Response format

The endpoint returns NDJSON in the same event shape used by chat. During long-running delegated work, progress and file-level change events should stream incrementally. The final `done.result` is the canonical structured execution result.

```json
{"type":"progress","message":"Executing editor-aware coding task"}
{"type":"continue_progress","message":"Continue Ask2Ask handling phase=execution file=src/parser/handler.go"}
{"type":"tool_call","name":"code.apply_patch","args":{"path":"src/parser/handler.go"}}
{"type":"continue_modified_file","file":{"file_path":"src/parser/handler.go","crud_operation":"update","diff_summary":"Applied delegated execution change to src/parser/handler.go","diff_format":"unified","diff_delta":"@@ ...","metadata":{"phase":"execution"}}}
{"type":"done","result":{"status":"completed","summary":"Continue completed delegated execution work for src/parser/handler.go","modified_files":[{"file_path":"src/parser/handler.go","crud_operation":"update","diff_summary":"Applied delegated execution change to src/parser/handler.go","diff_format":"unified","diff_delta":"@@ ...","metadata":{"phase":"execution"}}],"artifacts":[{"id":"diff::handler.go","kind":"diff","path":"artifacts/diffs/handler.go.patch","description":"Full diff for src/parser/handler.go","metadata":{"diff_format":"unified"}}],"metadata":{"requested_phase":"execution","tool_choice":"apply_patch","allowed_tools":["read_file","apply_patch","get_problems"]}}}
```

Execution result rules:

- stream progress while Continue is still working
- emit file-level change events as modifications become known
- always finish with one canonical structured result object
- inline compact diffs when practical
- prefer artifact references for large diffs when requested by `result_contract`

### POST /api/continue/worker/register

Registers a Continue-side worker callback for a daemon session.

Registration remains session-scoped. The worker callback handles many per-request delegated executions, each identified by `request_id`.

The callback URL must be an absolute http(s) URL whose host resolves to loopback only (for example `http://127.0.0.1:65432/continue/worker` or `http://localhost:65432/continue/worker`). Remote hosts are rejected to prevent SSRF-style callback abuse.

#### Request body

```json
{
  "session_id": "workspace-42",
  "callback_url": "http://127.0.0.1:65432/continue/worker",
  "capabilities": ["apply_patch", "read_context"],
  "ide": "vscode"
}
```

#### Response

```json
{
  "ok": true,
  "worker": {
    "session_id": "workspace-42",
    "callback_url": "http://127.0.0.1:65432/continue/worker",
    "capabilities": ["apply_patch", "read_context"],
    "ide": "vscode",
    "updated_at": "2026-08-03T12:34:56Z"
  }
}
```

### GET /api/continue/workers/{sessionId}

Returns the registered worker callback for the session when available.

### GET /api/continue/metadata

Returns native Continue transport metadata.

Current metadata guarantees:

- chat path is `/api/continue/chat`
- chat method is `POST`
- request state path prefix is `/api/continue/requests/`
- delegated execution supports an optional caller-provided `request_id`, but the daemon owns the authoritative execution identity for correlation and lifecycle tracking
- `control_model.supported_llms` is the authoritative daemon-advertised AI Studio model catalog for the supported Big 3 providers
- `control_model.selection_message` tells Continue to present the same daemon-advertised provider/model choices
- chat streaming is `ndjson`
- `provider_is_attribute` is `true`
- `model_is_attribute` is `true`
- control model declares `controller=ai_studio_daemon` and `editor_worker=continue`
- control model advertises the daemon-owned `supported_llms` catalog and a `selection_message`
- no daemon-owned config endpoint is advertised

The metadata may also advertise:

- `ide_tools.catalog`
- `ide_tools.supported_phases`
- request support for `tools` forwarding
- request support for `tool_choice`

This makes the Continue integration consumable as a daemon-side capability participant from AI Studio.

## Ask-visible Continue Capability

The HTTP transport is only one layer of the integration. Inside Ask, the daemon should expose Continue through the same LAO pipeline used for all other requests.

Core rule:

- Continue-originated prompts still go through normal Ask classification
- the daemon LLM still determines phase and domain
- Phase Factory and other capabilities still participate normally
- Continue is represented as one passive capability participant, not as an alternate controller

Target tool surface:

- `delegate_continue_task`

That tool represents Continue's own local Ask/tool loop. The daemon LLM uses it to delegate one bounded IDE coding objective to Continue and waits for the finished result.

Dynamic context and instruction requirements:

- `Classify()` recognizes that Continue-originated coding requests have an editor-native participant available
- `BuildContext()` tells the daemon LLM when Continue is the right bounded coding executor for the slice
- `BuildInstructions()` tells the daemon LLM how to compose the delegated objective, including phase bounds, allowed tools, success criteria, and expected result shape

Transitional note:

- current compatibility aliases such as `inspect_ide_worker` and `delegate_ide_coding_task` may still exist during migration
- they should not be treated as the target architecture

## Continue Workflow Surface Notes

The Continue integration should be understood as a layered workflow surface:

- Continue provides IDE-native execution tools and context capture.
- AI Studio decides when those tools are available and how they fit into the larger workflow.
- The current surface is strong for coding, testing, debugging, and repository-local documentation work.
- It is not yet a full SDLC abstraction for CI, deployment, release, rollback, and operational workflows.

Mode-aware tool expectations:

- `chat` mode: no active tools
- `plan` mode: readonly tools only
- `agent` mode: mutation-capable tools when enabled

This mode gating should be treated as part of the API contract even though it is implemented inside Continue rather than the daemon.

## Removed Surfaces

The following surfaces are intentionally not part of the current contract:

- `/v1/chat/completions`
- `/v1/models`
- `/api/continue/config`
- daemon-owned shadow-model discovery
- daemon-owned OpenAI-compatible request semantics

## Validation Checklist

When changing this integration, validate these behaviors first:

- `/api/continue/chat` preserves `provider` and `model`
- chat requests populate structured editor context in `SessionPayload.Variables`
- `/api/continue/agent/execute` preserves provider/model through both worker-proxy and fallback paths
- worker proxying only occurs when advertised capabilities match the requested subtask
- `/api/continue/metadata` advertises the native contract and does not reintroduce daemon-owned config surfaces
