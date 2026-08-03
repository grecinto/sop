# Continue bridge API spec

**Status**: Active and partially implemented

## Overview

This document defines the bridge contract that connects Continue in VS Code to the daemon-backed Ask/ReAct runtime.

The current implementation uses two layers:

- transport layer: Continue/OpenAI-compatible HTTP endpoints in the daemon
- Ask-visible wrapper layer: a generic `ide_worker` capability that can inspect session state and delegate a bounded coding task through a backend adapter

Continue is currently the first backend for that wrapper capability.

## Base URL

All endpoints are local to the host running the app:

```text
http://127.0.0.1:9090
```

## Authentication

The bridge should reuse the existing daemon/HQ bearer-token flow rather than introducing a separate Continue-only credential scheme. Every request should include an Authorization header in the form Bearer <access-token>.

Recommended behavior:
- access tokens should be short-lived
- renewal should go through the existing HQ login/session refresh path
- the backend should validate tokens using the same auth middleware as the rest of the platform

If a token is expired or invalid, the backend should return 401 Unauthorized or 403 Forbidden with a structured error such as auth_expired or login_required. The Continue client should respond by showing a VS Code modal prompting the user to sign in again and then retrying the pending operation after auth is restored.

## 1. Chat endpoint

### POST /api/continue/chat

Accepts a chat request and returns a streamed response.

Current Ask-side mapping:

- request metadata is transformed into the shared Ask payload
- structured editor context is copied into `SessionPayload.Variables`
- selected Continue model routing intent is copied into `SessionPayload.Variables` as `llm_provider` and `llm_model`
- the OpenAI-compatible chat surface also infers editor context from Continue-style prompt history when explicit editor fields are missing
- workflow hints such as `continue_mode`, `coding_workflow_profile`, and `requested_phase` are preserved for Ask-side tool selection

### Request body

```json
{
  "session_id": "string",
  "message": "string",
  "workspace": {
    "root": "string",
    "active_file": "string",
    "selection": {
      "start_line": 1,
      "end_line": 50
    }
  },
  "context": {
    "open_files": ["string"],
    "selected_text": "string"
  },
  "mode": "chat"
}
```

### Response

The endpoint should stream NDJSON events.

#### Event types

- `progress`
- `tool_call`
- `tool_result`
- `content`
- `done`
- `error`

#### Example

```json
{"type":"progress","message":"Inspecting the target module"}
{"type":"tool_call","name":"repo.search_code","args":{"query":"parser"}}
{"type":"tool_result","name":"repo.search_code","result":{"hits":[{"path":"src/parser/handler.go"}]}}
{"type":"content","delta":"I found the relevant parser code."}
{"type":"done"}
```

### Current structured session variables

When present, the bridge now copies these fields into `SessionPayload.Variables` so Ask-side capabilities can consume them without scraping prompt text:

```json
{
  "continue_bridge": true,
  "workspace_root": "/path/to/repo",
  "active_file": "src/parser/handler.go",
  "selected_text": "func parseHandler(...)",
  "open_files": ["src/parser/handler.go", "src/parser/handler_test.go"],
  "continue_mode": "apply",
  "coding_workflow_profile": "debugging",
  "coding_task_type": "debugging",
  "requested_phase": "execution"
}
```

## 2. Tool invocation endpoint

### POST /api/continue/tools/{toolName}

Invokes one tool with structured input.

### Example: repo.search_code

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

### Example response

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

## 3. Session state endpoint

### GET /api/continue/sessions/{sessionId}

Returns the current workflow state and metadata for a task.

### Example response

```json
{
  "session_id": "workspace-42",
  "status": "in_progress",
  "phase": "investigation",
  "message": "Inspecting failing parser logic"
}
```

## 4. Agent execution endpoint

### POST /api/continue/agent/execute

Invokes Continue as a remote coding worker for a specific subtask. This is the bridge used when daemon Ask needs editor-aware execution such as patching, diff review, or workspace-guided edits.

Current internal caller:

- generic Ask capability tool: `delegate_ide_coding_task`
- capability implementation: `lib/sop/ai/agent/domains/ideworker/capability.go`
- Continue backend adapter: `lib/sop/cmd/sop-daemon/ide_worker_bridge.go`

### Request body

```json
{
  "session_id": "workspace-42",
  "task": "patch the failing parser handler",
  "context": {
    "workspace_root": "/path/to/repo",
    "active_file": "src/parser/handler.go",
    "selected_text": "..."
  },
  "mode": "coding"
}
```

### Response

The endpoint should stream NDJSON events with the same progress/tool/content structure used by the chat endpoint, followed by a terminal result event.

Execution behavior:
- if no Continue worker is registered for the session, the daemon handles the request locally through the shared workflow runtime
- if a Continue worker is registered for the session, the daemon proxies the request to that registered worker and streams the worker response back to the caller
- if a Continue worker is registered but lacks the capabilities required by the subtask, the daemon falls back to local Ask execution instead of proxying blindly
- in both cases, the daemon remains the controller and records daemon-owned session snapshots

```json
{"type":"progress","message":"Preparing the patch"}
{"type":"tool_call","name":"code.apply_patch","args":{"path":"src/parser/handler.go"}}
{"type":"tool_result","name":"code.apply_patch","result":{"ok":true}}
{"type":"done","result":{"status":"completed"}}
```

## 5. Continue worker registration endpoint

### POST /api/continue/worker/register

Registers a live Continue-side worker callback for a session. This is the contract that allows the daemon to delegate editor-aware subtasks back to Continue while remaining the supervisor.

### Request body

```json
{
  "session_id": "workspace-42",
  "callback_url": "http://127.0.0.1:65432/continue/worker",
  "capabilities": ["apply_patch", "read_context"],
  "ide": "vscode"
}
```

### Response

```json
{
  "ok": true,
  "worker": {
    "session_id": "workspace-42",
    "callback_url": "http://127.0.0.1:65432/continue/worker",
    "capabilities": ["apply_patch", "read_context"],
    "ide": "vscode",
    "updated_at": "2026-08-02T12:34:56Z"
  }
}
```

## 6. Continue worker state endpoint

### GET /api/continue/workers/{sessionId}

Returns the registered Continue worker callback for the session when available.

### Example response

```json
{
  "session_id": "workspace-42",
  "callback_url": "http://127.0.0.1:65432/continue/worker",
  "capabilities": ["apply_patch", "read_context"],
  "ide": "vscode",
  "updated_at": "2026-08-02T12:34:56Z"
}
```

## 7. Ask-visible IDE worker tools

These are not external HTTP endpoints. They are the Ask-side capability tools that make the bridge visible inside the shared orchestration runtime.

### inspect_ide_worker

Purpose:
- inspect whether a session has a connected IDE worker
- expose advertised worker capabilities and current structured editor context

### delegate_ide_coding_task

Purpose:
- delegate one bounded editor-aware subtask back to the registered IDE worker
- preserve Ask as the controller and consume the worker result in the same workflow

Key rule:
- these tools are generic to the `ide_worker` capability; Continue is only the first backend implementation

## Tool schema

### repo.list_files

```json
{
  "path": "string",
  "recursive": false
}
```

### repo.search_code

```json
{
  "query": "string",
  "path": "string",
  "limit": 20
}
```

### repo.read_file

```json
{
  "path": "string",
  "start_line": 1,
  "end_line": 200
}
```

### repo.get_symbols

```json
{
  "path": "string"
}
```

### code.find_references

```json
{
  "symbol": "string",
  "path": "string"
}
```

### code.find_definition

```json
{
  "symbol": "string",
  "path": "string"
}
```

### code.get_context

```json
{
  "path": "string",
  "line": 1,
  "window": 30
}
```

### code.related_symbols

```json
{
  "symbol": "string",
  "path": "string"
}
```

### code.preview_patch

```json
{
  "path": "string",
  "patch": "string"
}
```

### code.apply_patch

```json
{
  "path": "string",
  "patch": "string"
}
```

### test.run

```json
{
  "target": "string",
  "cwd": "string"
}
```

### workflow.get_state

```json
{
  "session_id": "string"
}
```

### workflow.update_state

```json
{
  "session_id": "string",
  "phase": "string",
  "message": "string"
}
```

## Implementation notes

- The first milestone should implement only the core inspection, delegation, patch preview, patch apply, and test-run tools.
- The bridge should reuse the existing daemon Ask endpoint rather than introducing a second orchestration engine.
- Tool results should remain structured so Continue can make decisions from them.
- The bridge should preserve session continuity across turns.
- The wrapper capability must stay passive: no phase orchestration, no controller loop, and no Continue-owned workflow state.

## Resume checklist

When resuming this work, inspect these code surfaces first:

- `lib/sop/cmd/sop-daemon/client_bridge.go`
- `lib/sop/cmd/sop-daemon/ide_worker_bridge.go`
- `lib/sop/cmd/sop-daemon/rag.go`
- `lib/sop/ai/agent/domains/ideworker/capability.go`
- `lib/sop/ai/agent/copilot.go`

Validate these behaviors before extending the design:

- Continue chat populates structured editor context variables
- OpenAI-compatible prompt-shaped edit/apply requests still recover `selected_text`, `active_file`, and `open_files` into structured payload variables
- `inspect_ide_worker` can see session/editor state for the current Ask turn
- `delegate_ide_coding_task` only proxies when the registered worker advertises the required capability set
- daemon-local fallback remains the behavior when no worker is registered or capability matching fails
