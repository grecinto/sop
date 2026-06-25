# HQ/Host RAG Security Protocol Proposal

## Goal

Support signed-token enforcement for both HQ and the local RAG runtime, while preserving offline operation when HQ is unreachable.

This protocol also supports the new local RAG deployment model:

- a simple one-click standalone setup that defaults to a fully working local RAG runtime,
- an advanced setup path using the existing Setup Wizard for more customized deployments,
- and a governance model where HQ-authenticated sessions can still track usage and enforce limits while the local runtime remains autonomous and low-friction.

## Core model

Use asymmetric signing for all access tokens.

## Default vs advanced deployment split

### Default mode

The default deployment should keep the host device as the active execution surface:

- Ask/LLM flows run locally on the user’s machine,
- command-line tools execute on the host,
- local STM/LTM-style memory remains on the host,
- and Spaces/Stores remain centralized in HQ.

This gives the user a personal local runtime while keeping shared records and shared workspace state central.

### Advanced mode

Advanced mode can support a more self-hosted setup:

- the user can run a richer local or self-hosted RAG environment,
- Spaces and Stores can live on the user side,
- and HQ can act as a coordination plane, policy authority, or shared-service layer.

This makes the system flexible for users who want more local ownership while preserving the simpler default path for most users.

## Advanced mode: client-authenticated multi-client model

For advanced deployments, the daemon should not rely on a daemon-owned session lifecycle for each client. Instead, the client site should authenticate with HQ or a central identity layer and present a short-lived access token plus renewal metadata.

The daemon should then:

- validate the access token,
- extract the client identity, tenant context, and workspace scope,
- bind that identity to the request context,
- and route requests into a client-scoped runtime context.

This aligns with a Copilot-style architecture where authentication is handled centrally and the local daemon remains a lightweight enforcement point. It is more scalable than maintaining a separate session store inside each daemon instance, while still allowing per-client isolation for rate limits, workspace state, and policy enforcement.

In short:

- simple mode: one trusted local client, minimal handshake, no daemon-side session lifecycle
- advanced mode: multiple clients, client-side authentication, per-client identity and workspace isolation, centralized renewal and policy control


- HQ owns the private signing key.
- The host daemon only needs the public verification key.
- Each token is short-lived and carries policy and scope information.

## Token shape

Each token should include:

- `iss`: issuer, typically HQ
- `sub`: host or user identity
- `aud`: target service, for example `sop-host-rag`
- `jti`: unique token id for replay tracking
- `iat`, `nbf`, `exp`: validity window
- `scopes`: capabilities such as `ask`, `execute`, `heartbeat`
- `tenant_id`: tenant or subscription context
- `policy_version`: version of the last policy snapshot
- `rate_limit_budget`: remaining online budget if HQ is reachable
- `offline_until`: how long the host may continue using its cached policy
- `device_id`: optional host binding information

## Enforcement model

### 1. Online mode

When HQ and the host are both reachable:

- HQ is the authoritative policy engine.
- The host daemon verifies the token locally using the HQ public key.
- HQ also validates the token and enforces the latest policy.
- The host syncs policy, revocation state, and budget updates through periodic heartbeats.

This gives you dual enforcement:

- local enforcement for fast local decisions
- central enforcement for subscription, rate-limit, and entitlement control

### 2. Offline mode

When HQ is unreachable:

- the host daemon continues using the last valid signed token and cached policy snapshot
- the local RAG runtime enforces the cached policy locally
- the daemon only continues until one of these happens:
  - the token expires
  - the offline window ends
  - the local budget is exhausted
  - the host receives a revocation update later

## Recommended flow

1. Host starts and registers with HQ.
2. HQ issues a signed session token plus a policy envelope.
3. The host stores the token and policy envelope locally in encrypted storage.
4. For each Ask request, the host sends the signed token and a lightweight policy check.
5. If HQ is online, HQ can refresh the policy and revoke the session centrally.
6. If HQ is offline, the host relies on the cached signed token and the last synced policy snapshot.
7. For the simple setup path, this flow is the only required user-facing setup step beyond authentication; the runtime can self-bootstrap into a default standalone local RAG configuration.
8. For the advanced setup path, the user can still choose the richer Setup Wizard experience, but the token-based governance layer remains the same.

## Revocation strategy

Use a hybrid revocation approach:

- short-lived tokens for safety
- central revocation list or denylist in HQ
- local cached revocation state on the host for offline grace periods

A good default is:

- token lifetime: 5 to 15 minutes
- offline grace: 1 to 24 hours depending on business rules
- heartbeat interval: every 30 to 60 seconds

## Security recommendations

- Use Ed25519 or ECDSA signing rather than a shared secret.
- Rotate keys periodically and publish new public keys through a JWKS-style endpoint or a pinned bundle.
- Bind the token to the host identity where possible.
- Store cached policy and tokens encrypted at rest.
- Include a nonce or replay-safe challenge for sensitive operations.

## Suggested implementation stack

- HQ: sign and issue tokens, maintain policy and revocation state
- Host daemon: verify tokens, cache policy, send heartbeats, enforce locally when offline
- Local RAG runtime: consume the validated policy and refuse requests if the token or budget is invalid

## Recommended architecture decision

Treat HQ as the source of truth for subscription, entitlement, and policy, but let the local daemon remain an enforcement point when the network is unavailable. That gives you centralized control and resilient offline operation.
