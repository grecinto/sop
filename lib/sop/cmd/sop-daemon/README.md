# SOP Desktop Daemon

This directory contains the private host-side daemon for the overlay repo. It is the product-specific runtime that sits on top of the shared public SOP engine and provides host-side execution and local RAG behavior for the private deployment model.

## What it provides

- local command execution for approved UI actions,
- a local Ask endpoint for host-side RAG requests,
- direct command-prefix routing for fast local shell execution,
- and the local topology/transport glue that connects the private runtime to the higher-level product experience.

## Unified deployment model

The platform is designed around a unified codebase that can run in multiple deployment environments without changing the product experience.

- The same runtime can run on the user’s local host.
- The same runtime can run in HQ-managed infrastructure.
- The same runtime can run in a premise HQ or advanced self-hosted deployment.

This creates a strong topology because the product is implemented once and deployed where it is most appropriate.

### Local host mode

- the user’s machine runs the local daemon and local RAG runtime,
- Ask/LLM interactions and command-line tools run on the host device,
- STM/LTM-style local memory is retained on the host,
- and the experience remains fully functional even when HQ is not the active runtime.

### HQ mode

- the runtime operates in the shared HQ environment,
- shared policy, identity, and coordination remain centralized,
- and the same code path serves the shared product experience.

### Premise HQ / advanced mode

- the runtime is deployed behind the customer’s own HQ or premise infrastructure,
- more of the data ownership and policy enforcement can stay on-premise,
- and HQ can remain a control plane, coordination layer, or optional shared service.

This is a strong and efficient architecture because it avoids parallel implementations while still supporting local, cloud, and advanced enterprise deployment needs. It is also a clear advantage over database-specific stacks: a SQLite-backed deployment and a Postgres-backed deployment often need different backend endpoints and database-management code, while SOP keeps the product on one unified codebase and one runtime contract.

## Authentication model

The daemon currently supports a simple local-first flow for one trusted client:

- the client presents a signed bearer token,
- the daemon validates it,
- and the handshake accepts the renewal/HQ metadata without creating a daemon-owned session.

For advanced mode, the design should evolve toward a Copilot-style model:

- the client site authenticates with HQ or an identity provider,
- the client receives a short-lived access token and a renewal/refresh mechanism,
- the daemon validates that token and binds the request to a client identity and workspace context,
- and each client can be isolated by tenant, workspace, or runtime scope.

This keeps the daemon lightweight and stateless while still supporting multiple concurrent clients in a scalable way.

## Run locally

```bash
go run ./cmd/sop-daemon -port 9090 -config /path/to/agent-config.json
```

The daemon listens on 127.0.0.1 by default and exposes:

- POST /api/execute
- POST /api/ask

## Direct local command execution

The local-agent Ask flow accepts direct command prefixes so users can work with the host shell without going through the full reasoning loop:

- `cmd:...`
- `shell:...`
- `exec:...`
- `run:...`

Examples:

```text
cmd:pwd
cmd:go test ./...
cmd:git status
cmd:find . -name "*.go" | head
```

When local desktop automation is enabled, the same execution path can also trigger richer helpers for repo inspection and editing, including file reads/writes/patches, directory management, repo search/grep, test execution, and path inspection. These actions are still approval-aware and policy-gated by the automation layer.

### Command Execution Modes

Commands can execute in two modes: **Terminal** or **Background**.

**Terminal Mode** (🖥️):
- Opens a native terminal window (Terminal.app, gnome-terminal, etc.)
- Supports interactive commands requiring user input (sudo passwords, ssh prompts)
- User types directly in terminal, maintains full control
- Best for: `sudo`, `ssh`, `vim`, `top`, `tail -f`, text editors, monitors

**Background Mode** (⚡):
- Runs invisibly with stdout/stderr captured
- Results displayed in chat window
- Supports cancellation with partial output preservation
- Best for: `ls`, `pwd`, `git status`, `cat`, build commands

**Auto-Detection**: The daemon automatically suggests the appropriate mode based on command patterns (see `terminal.go:shouldUseTerminal()`). Users can override the suggestion in the approval modal.

**Cancellation Support**: Background commands can be aborted mid-execution. The system kills the process group (including child processes) and preserves any partial output captured before cancellation.

**Error Handling**: Failed commands preserve stdout/stderr output, allowing users to see what happened before the error. The UI distinguishes between cancelled (user-initiated abort) and failed (command error) states.

See [docs/TERMINAL_LAUNCH_FEATURE.md](../../../../docs/TERMINAL_LAUNCH_FEATURE.md) for complete documentation.

### Testing

Run all daemon tests:
```bash
go test ./cmd/sop-daemon/ -v
```

Test specific features:
```bash
# Terminal detection and suggestions
go test ./cmd/sop-daemon/ -run TestShouldUseTerminal
go test ./cmd/sop-daemon/ -run TestGetTerminalSuggestion

# Command execution and cancellation
go test ./cmd/sop-daemon/ -run TestExecuteHandler

# Cancellation response time (should complete in ~0.10s)
go test ./cmd/sop-daemon/ -run TestExecuteHandlerHandlesCancellation
```

**Test Coverage**:
- 96 test functions covering command execution, cancellation, error handling
- Terminal pattern detection (sudo, ssh, vim, editors, monitors)
- Process group termination on cancellation
- Partial output preservation on errors
- Cross-platform terminal launch (macOS, Linux, Windows)

**Key Test Files**:
- `main_test.go` - Command execution, cancellation, error handling
- `terminal_test.go` - Terminal detection patterns and suggestions

## Private overlay scope

The following pieces are the private overlay pieces that belong in this repository:

- [cmd/sop-daemon/main.go](cmd/sop-daemon/main.go): host-side daemon entrypoint and HTTP bridge
- [cmd/sop-daemon/rag.go](cmd/sop-daemon/rag.go): local Ask handling and local RAG bootstrap
- [cmd/sop-daemon/localrag/main.go](cmd/sop-daemon/localrag/main.go): local interactive RAG CLI
- [cmd/sop-daemon/topology/bundle.go](cmd/sop-daemon/topology/bundle.go): packaging logic for host installation
- [cmd/sop-daemon/topology/remote.go](cmd/sop-daemon/topology/remote.go): host/HQ transport for private product flows
- [cmd/sop-daemon/communication.go](cmd/sop-daemon/communication.go): session coordination and policy handling between host and HQ

These files stay local because they encode the behavior that is specific to this product layer rather than the generic upstream engine.

## Host/HQ coordination sketch

The HQ-side platform should remain the authority for entitlement and policy. The host daemon should establish a long-lived session with HQ and keep it alive for the lifetime of the local RAG runtime.

The initial flow should be:

1. Host daemon starts and registers itself with HQ using a subscription identifier.
2. HQ returns a session token and policy envelope (rate limits, feature flags, tenant context).
3. The daemon sends periodic heartbeats while the local runtime is active.
4. Each Ask request consumes budget from the session policy and is evaluated centrally by HQ.
5. If the subscription is revoked, rate-limited, or expired, HQ can instruct the host to stop serving local Ask traffic.

The current implementation in [cmd/sop-daemon/communication.go](cmd/sop-daemon/communication.go) gives a simple session registry and budget model for the host side, but the actual wire protocol, auth, and HQ enforcement logic should be added in the private repo.

## Security proposal

The private implementation should use signed tokens for both HQ and the local RAG runtime. The proposed model is documented in [cmd/sop-daemon/SECURITY_PROTOCOL.md](cmd/sop-daemon/SECURITY_PROTOCOL.md).

In short:

- HQ remains the authority for subscriptions, rate-limits, and revocation.
- The host daemon verifies signed tokens locally and enforces the latest policy when online.
- If HQ is unavailable, the host falls back to the last valid cached token and policy snapshot and continues enforcing them locally until the token or budget expires.

## Implementation checklist for the private repo

### Phase 1: packaging and deployment
- [ ] Build a host-installable binary for the daemon.
- [ ] Package the daemon with its local config and model-cache defaults.
- [ ] Add a minimal bootstrap flow for first-run host registration.

### Phase 2: HQ/host protocol
- [ ] Define the register/heartbeat/ask/policy-refresh/revoke wire protocol.
- [ ] Add signed-token issuance and verification.
- [ ] Add policy caching and offline fallback behavior.
- [ ] Add host-side revocation handling and stop conditions.

### Phase 3: local enforcement
- [ ] Enforce signed-token checks inside the local RAG runtime.
- [ ] Enforce cached policy limits locally when HQ is unreachable.
- [ ] Add audit logging for policy decisions and revocations.

### Phase 4: operations and governance
- [ ] Add private configuration for tenant IDs, signing keys, and model locations.
- [ ] Add rollout and rollback guidance for host deployments.
- [ ] Add incident handling for offline mode and token expiry.

## Handoff notes

This package is intentionally the boundary between:

- the generic SOP AI engine, which can remain in the public repo, and
- the proprietary host/HQ deployment runtime, which should stay private.

When returning to this work later, the main focus areas should be:

1. the secure session protocol between HQ and the host,
2. the offline enforcement model,
3. and the packaging flow for the downloadable daemon binary.

For the repo split planning itself, see [cmd/sop-daemon/PRIVATE_REPO_SPLIT.md](cmd/sop-daemon/PRIVATE_REPO_SPLIT.md).
