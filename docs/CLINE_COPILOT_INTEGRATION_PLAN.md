# Cline SDK Integration

**Status (2026-09-29):** The daemon-managed Cline SDK backend, authenticated SOP MCP tools, workspace diff capture, delegation lifecycle, and packaging build checks are implemented. Go tests and the SDK-host TypeScript build pass. A real-provider, browser-driven end-to-end run and clean target-machine installation have not yet been validated; this is code-complete, not fully release-validated.

## Architecture

The standard AI Chat composer has one Send action. Requests go through SOP Ask; Ask classifies the request and may delegate a bounded coding task through the `ideworker` capability. Cline owns the inner coding agent loop, while SOP remains the outer controller.

```text
Data Manager Send
    -> /api/ask
    -> SOP Ask/ReAct and capability classification
    -> ideworker.Backend
    -> daemon-managed Cline SDK host
    -> workspace tools and validation
    -> workspace diff, SOP undo record, and result
```

The current Ask-visible worker tool is still named `delegate_continue_task` for compatibility. Its backend is not Continue-specific: the daemon selects Cline by default. Treat that name as legacy API terminology, not the product direction.

The React site is the account/onboarding surface. The Data Manager Chat UI is served by `httpserver` or the daemon from shared `sopbackend` templates. Direct Cline HTTP routes are for diagnostics and backend tests, not a second production chat controller or Send button.

## Implemented Components

| Component | Implementation |
|---|---|
| Shared worker contract | [`ideworker.Backend`](../lib/sop/ai/agent/domains/ideworker/capability.go) and canonical result types in [`result.go`](../lib/sop/ai/agent/domains/ideworker/result.go) |
| Cline backend | [`cline_worker_bridge.go`](../lib/sop/cmd/sop-daemon/cline_worker_bridge.go); SDK host is default, CLI fallback is selected with `SOP_CLINE_EXECUTION_MODE=cli` |
| SDK host | [`tools/cline-sdk-host`](../tools/cline-sdk-host); uses `@cline/core` and a JSON/NDJSON stdin/stdout protocol |
| Diagnostic task routes | `POST /api/cline/execute`, `POST /api/cline/tasks`, `GET /api/cline/tasks/{id}`, `POST /api/cline/tasks/{id}/cancel` |
| SOP MCP route | Authenticated `POST /mcp` using JSON-RPC 2.0 |
| Production chat route | `/api/ask`; Ask decides whether to delegate |

The SDK host currently exposes these SOP tools through authenticated MCP calls:

- `get_task_guidance`
- `search_memory`
- `search_undo_history`
- `record_validation`
- `restore_undo_transaction`

Restore requires explicit confirmation, a matching workspace/session, backup and hash verification, and invalidates the associated Cline session after success. Workspace roots are checked before execution or workspace-scoped memory/undo actions.

## Result, Undo, and Activity

The daemon compares workspace file hashes before and after a Cline task, computes created/updated/deleted files, captures diffs, and records the result through SOP's existing undo and execution-memory paths. Inline diffs are bounded to 32 KiB; larger results carry truncation metadata while file-level change metadata remains available.

SOP's `UndoManager` is the durable, searchable undo source of truth. Cline's task-local checkpoints remain a separate recovery layer. Do not import every Cline checkpoint into SOP.

The Chat surface shows one filtered activity message that updates in place and becomes the final result. Raw SDK/PTY output remains in the Terminal/daemon diagnostic surfaces. Structured status fields include phase, activity kind, label, and update time.

## Runtime and Packaging

- The SDK host requires Node.js 22 or newer.
- Build locally with `cd tools/cline-sdk-host && npm ci && npm run build`.
- `tools/cline-sdk-host/package-runtime.sh [output-directory]` runs the locked install and build and stages the package metadata, lockfile, TypeScript config, and compiled host.
- `SOP_CLINE_SDK_HOST_PATH` can point the daemon at a non-default compiled host path.
- `SOP_CLINE_API_KEY` and provider-specific environment variables are fallbacks; normal daemon requests pass provider credentials through a mode-0600 temporary request file, not command-line arguments.
- The package script does not install Node itself. Release/deployment packaging must provide Node 22+ and the host's runtime dependencies on the target machine.

Continue is not a shipping dependency. Its retained code and documentation are compatibility/reference material only; see the Continue section of the [documentation index](INDEX.md).

## Automated Validation

From the repository root:

```sh
go test ./...
go test ./lib/sop/...
cd tools/cline-sdk-host && npm ci && npm run build
```

Focused Cline coverage lives in `lib/sop/cmd/sop-daemon/*cline*test.go`, `lib/sop/cmd/sop-daemon/mcp_server_test.go`, and the `ideworker` package tests.

## Remaining Release Validation

A human with configured provider credentials still needs to verify the end-to-end path in the actual Data Manager:

1. Start the daemon with the SDK host available and open the Data Manager in daemon mode.
2. Send a bounded coding task using the normal Chat Send action; confirm SOP Ask delegates and the task edits the selected workspace.
3. Confirm completion, changed-file reporting, undo history, and a validation result appear as expected.
4. Cancel a long-running task and confirm the daemon-managed worker stops.
5. Send a non-coding question and confirm it remains an SOP Ask response without Cline delegation.
6. Repeat from a clean target-machine installation with Node 22+ and packaged host dependencies.

The automated protocol, daemon lifecycle, workspace-diff, MCP, and host-build checks do not replace this live provider/UI/install validation.