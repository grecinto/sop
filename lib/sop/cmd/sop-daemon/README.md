# SOP Daemon

The daemon is the local runtime for workspace-aware APIs, AI Studio Ask, terminal workers, and bounded coding delegation. It is a separate process from the React site and from `httpserver`, which serves the shared Data Manager UI.

## Runtime Boundary

- Data Manager Chat requests enter through `/api/ask`; SOP Ask/ReAct owns the outer controller.
- The daemon's `ideworker.Backend` defaults to Cline SDK, hosted by `tools/cline-sdk-host`. `SOP_CLINE_EXECUTION_MODE=cli` selects the compatibility CLI fallback.
- The Cline host requires Node.js 22 or newer and must be built/packaged with its runtime dependencies available. See the [Cline SDK host README](../../../../tools/cline-sdk-host/README.md) and [integration guide](../../../../docs/CLINE_COPILOT_INTEGRATION_PLAN.md).
- Direct `/api/cline/*` routes are diagnostic/task-lifecycle APIs, not the production chat controller.
- `POST /mcp` exposes authenticated SOP guidance, memory, undo-history, validation, and confirmed restore tools to the SDK host.

## Authentication and Network Binding

The daemon is not authless. By default it binds to `127.0.0.1`, uses a one-time bootstrap challenge/secret to issue daemon JWT access and refresh tokens, and validates bearer/cookie credentials on protected routes. Keep the bootstrap secret private and do not expose the listener publicly without a separate deployment/security design.

See [`SECURITY_PROTOCOL.md`](SECURITY_PROTOCOL.md), [`DAEMON_AUTH_USAGE.md`](../../DAEMON_AUTH_USAGE.md), and the repository [authentication guide](../../../../docs/AUTHENTICATION.md).

## HQ Synchronization

When HQ connectivity is configured, the daemon runs a periodic sync loop for poll/throttle/queue exchange and pulls after successful batch sync. Startup also seeds initial federation content after the system database is provisioned. The shared-memory auto-enqueuer and Ask-time automatic promotion hooks are currently disabled; see [FEDERATION.md](../../../../docs/FEDERATION.md) for the exact boundary.

## Run and Validate

From the repository root:

```sh
go run ./lib/sop/cmd/sop-daemon --port 9090
```

Build the Cline host with Node 22+:

```sh
cd tools/cline-sdk-host
npm ci
npm run build
```

Focused Go checks:

```sh
go test ./lib/sop/cmd/sop-daemon
go test ./lib/sop/ai/agent/domains/ideworker/...
```
