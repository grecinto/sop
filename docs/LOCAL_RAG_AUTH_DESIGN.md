# Local RAG and HQ Authentication Design

## Overview

The UI remains the root controller for user identity. The user logs in through the UI, and that authenticated session is then used to bootstrap the local runtime. The local daemon and the local RAG process are not treated as independent clients with their own login flow; instead, they inherit the UI session through a secure handoff of a daemon-specific bootstrap credential.

This keeps the local experience aligned with the existing product model while preserving a secure boundary for policy enforcement and limits. The daemon receives a scoped bootstrap credential from the UI over localhost, exchanges it with HQ for a short-lived daemon sync token pair, and keeps those tokens in memory only.

## Goals

- Keep the UI as the single entry point for authentication.
- Let the local daemon and local RAG runtime operate under the same authenticated identity as the UI.
- Reuse the existing bearer-token and refresh-token model rather than introducing a separate trust model.
- Use a scoped bootstrap credential for the daemon handoff instead of passing the user’s full session token.
- Keep daemon sync credentials in memory only, with refresh through the UI when needed.
- Allow HQ to enforce policy, quotas, and revocation without becoming a second runtime.

## Core Trust Model

1. The user authenticates in the UI.
2. The UI requests a daemon-specific bootstrap credential from HQ. This credential is short-lived, scoped to daemon sync, and bound to the current user and daemon identity.
3. The UI passes that bootstrap credential to the local daemon over localhost.
4. The local daemon exchanges the bootstrap credential with HQ for a short-lived daemon access token and a refresh token.
5. The daemon keeps both tokens in memory only and uses the access token when calling HQ for policy or enforcement checks.
6. If the access token is near expiry, the daemon uses the refresh token to obtain a new access token, or asks the UI to fetch a fresh bootstrap credential and repeat the exchange.
7. HQ validates the token, checks its scope and revocation state, and returns policy decisions, limit state, or revocation signals.

This makes the local runtime a delegated extension of the UI session rather than an autonomous client.

## Architecture

### Components

- UI / HTTP server
  - Owns login, session issuance, and refresh handling.
  - Acts as the root controller for user identity.

- Local daemon
  - Runs locally on the user host.
  - Exposes local execution and local ask endpoints.
  - Receives the UI-hydrated access and refresh tokens.
  - Uses those tokens when communicating with HQ.

- Local RAG runtime
  - Uses the shared agent runtime and local configuration.
  - Executes local asks and tool-mediated work on the host.
  - Does not need its own independent user login.

- HQ / policy control plane
  - Receives authenticated requests from the local daemon.
  - Enforces policy, budget limits, and revocation decisions.
  - Does not replace the local runtime; it only governs it.

## Request Flow

```mermaid
sequenceDiagram
    participant User
    participant UI as UI / Browser
    participant Local as Local Daemon
    participant RAG as Local RAG Runtime
    participant HQ as HQ Policy Service

    User->>UI: Sign in
    UI->>UI: Issue access + refresh tokens
    UI->>Local: Hydrate tokens over localhost
    Local->>RAG: Start local ask / execution flow
    RAG->>Local: Request policy check or execution approval
    Local->>HQ: Send authenticated policy request
    HQ-->>Local: Allow / deny / limit state
    Local-->>RAG: Continue or block execution
    RAG-->>UI: Return result or policy response
```

## Security Properties

- No independent local login path is required.
- The local daemon inherits the UI session identity through a daemon-scoped bootstrap credential.
- The daemon never stores its sync credentials on disk; the UI only passes them over localhost and the daemon keeps them in memory.
- The bootstrap credential is short-lived and scope-limited; the daemon’s sync token pair uses the same expiry period for both access and refresh tokens, with a single shared renewal window.
- HQ can revoke or deny access centrally, including the bootstrap credential and the daemon token pair.
- Communication remains local-first for the agent runtime, with HQ only used for policy coordination and sync.

## MFA and session policy

The recommended policy is practical rather than overly rigid:

- Password-based sign-in remains the default path.
- MFA should be available for initial login, new-device sign-in, and sensitive or high-risk actions.
- Trusted devices may stay signed in for a limited period, but refresh tokens should expire on a weekly or monthly cadence and require a fresh sign-in when they expire.
- Re-authentication or an MFA challenge should be required every 60–90 days on trusted devices, and immediately for sensitive actions such as billing changes or account recovery.
- MFA should not be required on every routine refresh; that creates unnecessary friction for normal use.

This gives the product a clear security posture while keeping the day-to-day experience smooth.

## Practical API Shape

The initial HQ boundary can remain minimal:

- POST /api/daemon/bootstrap
  - Validate the active UI session.
  - Issue a short-lived, daemon-scoped bootstrap credential or authorization code for the current user and daemon instance.
  - Return it to the UI for localhost handoff.

- POST /api/daemon/token
  - Exchange the bootstrap credential for a daemon access token and refresh token.
  - Bind the token pair to the daemon identity and the requested sync scope.

- POST /api/policy/check
  - Validate the supplied daemon access token.
  - Return allow/deny and current policy state.

- POST /api/policy/heartbeat
  - Report local runtime activity and usage.
  - Let HQ update enforcement state.

- POST /api/daemon/refresh
  - Exchange the daemon refresh token for a new daemon access token when needed.

These endpoints can reuse the same bearer-token semantics already used by the UI session layer, but the daemon flow should use a scoped bootstrap token rather than the user’s full-long-lived session token.

## Implementation Notes

- The local daemon should accept a daemon bootstrap credential from the UI through a local bootstrap channel such as headers, environment variables, or a small local config payload.
- The daemon should exchange that bootstrap credential with HQ to obtain its own short-lived sync token pair and keep those in memory only.
- The daemon should not create a second identity model; it should reuse the same session-token validation logic already present in the UI auth layer.
- HQ should treat the local daemon as a delegated authenticated client, not as a standalone user.
- The UI remains the root of trust for authentication and renewal, including the bootstrap handoff and the refresh flow.

## Planned Product Workflow

The intended product flow is intentionally simple and host-centric:

1. The user signs in through the HQ UI and completes self-registration or onboarding.
2. The UI keeps the default path simple: the HQ experience remains the default until the user explicitly switches to local mode.
3. When local mode is selected, the UI uses the configured local agent URL as the active endpoint when HQ is offline or unavailable.
4. The local daemon is treated as a manually startable host process: the user or installer starts it explicitly, and the UI connects to it once it is available.
5. If the daemon is not reachable, the UI notifies the user that the local agent is unavailable and offers a "Reconnect to local agent" action so the user can retry the connection after fixing the local installation or starting the daemon manually.
6. The UI passes a daemon bootstrap credential and HQ connection context to the daemon over localhost so the daemon can request its own short-lived sync token pair without requiring a separate login flow.
7. In the simple/default mode, the daemon does not create its own independent user session; it relies on the bootstrap credential, its daemon access/refresh token pair, and the UI for renewal when needed.
8. The long-term host integration can optionally let the daemon be registered with the host OS so it can auto-start on login or boot, but the first version only needs reliable manual startup, explicit configuration, and graceful recovery.

This keeps the product straightforward for everyday users while leaving room for richer host-to-HQ integrations later.

## Current Status

The current implementation already has the essential building blocks:

- the UI auth layer supports signed access tokens and refresh tokens,
- the local daemon already exposes local execution and local ask endpoints,
- and the shared agent runtime can be used as the common execution substrate for both the UI and the local host experience.

The remaining work is to wire the UI-hydrated tokens through the daemon and add the authenticated HQ policy boundary.

## New Site / httpserver / local daemon handoff (2026-06-30)

This section captures the current implementation intent for the next pass: the new site experience should allow self-registration, subscription, payment, and onboarding, and then hand the user into the httpserver UI with a compatible, reusable auth session that continues to work with the local daemon on the host.

### Goals

- Reuse the shared sopbackend auth/session/token model rather than introduce a separate identity stack.
- Allow the new site to issue a compatible bearer token and access cookie that httpserver can consume.
- Preserve the user identity, role, subscription plan, and feature list as the session continues from the site into httpserver and then into the local daemon.
- Keep the site backend focused on product onboarding and account state while preserving shared auth and session handling in sopbackend.

### Current state

- The new site accounting and billing state is implemented in [src/backend/siteaccount/account.go](../src/backend/siteaccount/account.go).
- The site endpoints are registered in [src/backend/siteaccount/handlers.go](../src/backend/siteaccount/handlers.go).
- The standalone site backend is served from [src/backend/site/main.go](../src/backend/site/main.go).
- Shared auth/session/token handling remains in [lib/sop/tools/sopbackend/auth.go](../lib/sop/tools/sopbackend/auth.go) and [lib/sop/tools/sopbackend/scripts.go](../lib/sop/tools/sopbackend/scripts.go).

### Required implementation contract

1. Site registration and login create or reuse the same shared session token model used by sopbackend.
2. The token is exposed through the same bearer-token and cookie contract that httpserver expects.
3. When the site redirects to httpserver, the target app receives the same authenticated identity and entitlement context.
4. The httpserver runtime uses the same auth context and feature list to decide which UI and backend capabilities are available.
5. The local daemon receives the same token material over localhost and uses it to continue the authenticated session with the host-side runtime.

### Implementation sequence for the next pass

1. Add a site login path that issues a sopbackend-compatible access token.
2. Return the access token and refresh token through the same response structure expected by the existing session layer.
3. Add a redirect bridge from the site onboarding flow into the httpserver UI with the token preserved in the URL, cookie, or local storage handshake.
4. Ensure httpserver accepts the token through the existing auth middleware without a second login.
5. Add a simple local bootstrap path for the daemon to receive the same token context and continue the session on the host.
6. Add end-to-end tests for registration, login, redirect, httpserver access, and daemon bootstrap.

### Open questions to resolve before implementation

- Should the redirect handoff use a short-lived one-time token or the same long-lived access token?
- Should the daemon bootstrap be header-based, cookie-based, or environment-based for the first implementation?
- Should the site maintain a separate onboarding session state or rely entirely on the shared sopbackend session state?

### Pause/resume notes

When work pauses, the implementation should resume from the auth-handoff boundary described above. The expected shape is:

- site registration/login → shared session token issued,
- site → httpserver redirect with same auth context,
- httpserver → daemon bootstrap with same token material,
- all using the shared sopbackend auth/session and entitlement model.
