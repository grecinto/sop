# Continue Native Setup Guide

## Purpose

This guide documents the supported Continue integration pattern for AI Studio from Northstar AI.

- The daemon is the controller and orchestrator.
- Continue is an editor-facing client and optional delegated worker.
- Continue sends request-scoped provider configuration to the daemon.
- The daemon owns workflow state, orchestration, fallback behavior, and session tracking.

The open-source SOP runtime remains the underlying engine, but the product and integration surface documented here are branded as AI Studio from Northstar AI.

In the rest of this guide, the short product name is AI Studio.

Do not configure the daemon as an OpenAI-compatible provider.

## Supported transport

Use the native Continue endpoints under `/api/continue/*`.

Primary routes:

- `POST /api/continue/chat`
- `POST /api/continue/agent/execute`
- `POST /api/continue/worker/register`
- `GET /api/continue/metadata`
- `GET /api/continue/sessions/{sessionId}`
- `GET /api/continue/workers/{sessionId}`

Reference API details live in [docs/CONTINUE_BRIDGE_API_SPEC.md](docs/CONTINUE_BRIDGE_API_SPEC.md).

## Request attributes

Continue should send model configuration as explicit request fields rather than relying on daemon-side provider defaults.

Current native request attributes:

- `provider`
- `model`
- `api_key`
- `url`

The daemon preserves these values in request-scoped provider details for Ask execution and delegated worker execution.

Continue may also send Continue-native `tools` and `tool_choice` attributes on chat requests so AI Studio can deliberately route bounded IDE-side workflow execution back through Continue's tool pipeline.

Example chat request:

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
    "active_file": "src/parser/handler.go"
  },
  "context": {
    "open_files": ["src/parser/handler.go", "src/parser/handler_test.go"],
    "selected_text": "func parseHandler(...)"
  },
  "mode": "edit"
}
```

## Continue IDE workflow surface

Continue is the current IDE execution substrate for AI Studio. The practical workflow surface now includes:

- read and discovery tools
- edit and file-creation tools
- terminal execution
- diff inspection
- IDE diagnostics, references, definitions, symbols, and terminal inspection

Mode gating still matters:

- `chat` mode has no active tools
- `plan` mode is readonly only
- `agent` mode exposes the active mutation-capable tool set

The detailed inventory, SDLC coverage assessment, and remaining gaps are documented in [docs/CONTINUE_IDE_WORKFLOW_SURFACE.md](docs/CONTINUE_IDE_WORKFLOW_SURFACE.md).

Example delegated execute request:

```json
{
  "session_id": "workspace-42",
  "task": "Apply the prepared patch to the parser handler",
  "provider": "openai",
  "model": "gpt-5.4",
  "api_key": "provider-key-from-continue",
  "url": "https://api.openai.com",
  "context": {
    "workspace_root": "/path/to/repo",
    "active_file": "src/parser/handler.go",
    "selected_text": "func parseHandler(...)",
    "open_files": ["src/parser/handler.go"]
  },
  "mode": "apply"
}
```

## Authentication

When daemon auth is enabled, native Continue calls require the existing daemon bearer-token flow.

Supported paths:

1. Direct bearer token injection.
2. Refresh-token based renewal.
3. Local bootstrap-secret authentication against `POST /api/daemon/auth`.

For local validation, the helper script now supports all three without going through the browser login screen.

Relevant environment variables:

- `SOP_CONTINUE_BASE_URL`
- `SOP_CONTINUE_BEARER_TOKEN`
- `SOP_CONTINUE_REFRESH_TOKEN`
- `SOP_CONTINUE_BOOTSTRAP_PATH`
- `SOP_CONTINUE_TOKEN_CACHE`

Default bootstrap path:

```text
~/.config/sop/daemon.bootstrap
```

Default helper token cache:

```text
~/.config/sop/continue-helper-session.json
```

## Local validation helper

The repository includes [continue-daemon-bridge.py](../continue-daemon-bridge.py) for local validation and worker simulation.

Supported modes:

- `auth`
- `metadata`
- `chat`
- `execute`
- `read`
- `patch`
- `test`
- `session`
- `worker-state`
- `register-worker`
- `serve-worker`

### Quick validation against a daemon on port 9090

```bash
cd /Volumes/T7/sop-cloud

export SOP_CONTINUE_BASE_URL='http://127.0.0.1:9090'
export SOP_CONTINUE_PROVIDER='openai'
export SOP_CONTINUE_MODEL='gpt-5.4'
export SOP_CONTINUE_API_KEY='your-provider-key'
export SOP_CONTINUE_LLM_URL='https://api.openai.com'

python3 continue-daemon-bridge.py auth
python3 continue-daemon-bridge.py metadata
python3 continue-daemon-bridge.py chat
```

### Helper unit test

The helper also has a focused local unit test that validates token-cache behavior and delegated execute payload shaping without starting a daemon:

```bash
cd /Volumes/T7/sop-cloud
python3 -m unittest test_continue_daemon_bridge.py
```

### Worker registration and delegated execution

Terminal 1:

```bash
cd /Volumes/T7/sop-cloud

export SOP_CONTINUE_BASE_URL='http://127.0.0.1:9090'
python3 continue-daemon-bridge.py serve-worker 65432 --register
```

Terminal 2:

```bash
cd /Volumes/T7/sop-cloud

export SOP_CONTINUE_BASE_URL='http://127.0.0.1:9090'
export SOP_CONTINUE_ACTIVE_FILE='README.md'

python3 continue-daemon-bridge.py execute 'Summarize the active file and report the workspace root.'
python3 continue-daemon-bridge.py worker-state
python3 continue-daemon-bridge.py session
```

Expected result:

- `/api/continue/agent/execute` streams NDJSON events.
- the registered worker receives a callback on `/continue/worker`.
- worker state resolves through `/api/continue/workers/{sessionId}`.
- session state resolves through `/api/continue/sessions/{sessionId}`.

## Isolated source-tree validation

The current repository was validated end to end against an isolated daemon instance started from this source tree on port `9191` with a separate `XDG_CONFIG_HOME`.

Validated flow:

1. bootstrap-authenticate the helper with `auth`
2. fetch native metadata JSON with `metadata`
3. register a local worker with `serve-worker --register`
4. invoke delegated execution with `execute`
5. confirm session and worker state with `session` and `worker-state`

This confirms the native Continue transport works from the current daemon source tree without reintroducing an OpenAI-compatible bridge.

## Integration expectations for a real Continue client

If a dedicated Continue-side adapter is added later, it should follow these rules:

- send `provider`, `model`, `api_key`, and `url` as request attributes
- forward `tools` and `tool_choice` when AI Studio needs Continue to execute IDE-native workflow steps through the normal Continue tool system
- use the daemon bearer-token flow instead of inventing a separate auth scheme
- treat `/api/continue/chat` as the primary prompt path
- treat `/api/continue/agent/execute` as daemon-owned delegated execution
- treat the daemon as the only orchestrator and source of workflow/session truth

## Troubleshooting

If `metadata` returns HTML instead of JSON, the request is still hitting an unauthenticated route path or a different frontend/runtime surface.

Checks:

1. Run `python3 continue-daemon-bridge.py auth` first.
2. Confirm `SOP_CONTINUE_BASE_URL` points at the daemon instance you intend to test.
3. If reusing an existing local daemon, confirm the bearer token is accepted by that exact runtime.
4. If needed, start an isolated daemon instance with a separate `XDG_CONFIG_HOME` and validate against that instance first.