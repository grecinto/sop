# Continue bridge API spec

**Status**: Draft for implementation

## Overview

This document defines the first-pass API shape for the local bridge that connects Continue in VS Code to the daemon-backed Ask/ReAct runtime.

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

```json
{"type":"progress","message":"Preparing the patch"}
{"type":"tool_call","name":"code.apply_patch","args":{"path":"src/parser/handler.go"}}
{"type":"tool_result","name":"code.apply_patch","result":{"ok":true}}
{"type":"done","result":{"status":"completed"}}
```

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
