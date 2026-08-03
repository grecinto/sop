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

For the first iteration, the bridge may reuse the existing local daemon authentication flow and session headers already used by the daemon endpoints.

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
