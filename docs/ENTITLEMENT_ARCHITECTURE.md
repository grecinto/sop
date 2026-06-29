# Internal entitlement and licensing architecture

This document captures the internal design for making the new site the authority for account validity, subscription state, and licensed feature access.

## Goals

- Keep the new site as the control plane for user accounts, subscription state, and entitlement decisions.
- Make the bundled Data Manager behave as a licensed client that must remain tied to a valid account.
- Make future language bindings enforce the same rule for Memory, KnowledgeBase, and other premium capabilities.
- Fail closed when entitlement is missing, expired, revoked, or invalid.
- Use periodic heartbeat checks so the client remains tied to the current subscription state and cannot continue operating after the subscription is no longer valid.
- Use a modest cadence such as every two days for the periodic subscription check so the design stays lightweight and scalable.
- If the client cannot reach the new site because the user has no internet access, allow continued local use for a grace period rather than shutting the product down immediately; the client should resume enforcement once connectivity is restored.

## Core model

The new site owns the truth for:

- account existence
- account status
- subscription plan
- plan features and feature flags
- revocation or suspension state

The bundled Data Manager and future language bindings do not independently decide whether a user is allowed to use premium capabilities. They rely on the new site for a current entitlement decision.

## Proposed flow

### 1. Account and subscription live in the new site

The new site remains the place where users:

- register
- authenticate
- manage their subscription
- update or cancel their plan
- are suspended or revoked

### 2. Bundled Data Manager checks the new site on startup and on a periodic cadence

When the bundled Data Manager starts, it performs an entitlement check against the new site before continuing.

After startup, it should continue to ping the new site on a periodic interval such as every two days to confirm that:

- the account still exists
- the subscription is still active
- the site is reachable over the network
- the client still has a valid entitlement context

Required behavior:

- if the account exists and the subscription is active, the Data Manager continues to run normally
- if the account is missing, disabled, expired, or otherwise invalid, the Data Manager exits with a clear error message and a non-zero exit code
- if the entitlement check cannot be completed because the new site is unreachable, the Data Manager should continue to operate locally for a grace period rather than shutting down immediately; once connectivity returns, a fresh validation should be performed and any invalid state should then be enforced
- if connectivity is restored and the account is still valid, the client may resume after a fresh verification cycle

This means the Data Manager becomes a dependent client of the new site rather than an independently usable product surface.

### 3. Future language bindings use short-lived entitlement tokens

The upcoming language bindings should not depend on a static license key or a local copy of entitlement state. Instead, they should receive a short-lived, signed entitlement token from the new site.

That token should carry claims such as:

- issuer
- subject or account identifier
- audience for the specific client type
- permitted feature scope
- plan or entitlement tier
- expiration time
- unique token identifier

The binding validates the token locally and refuses to perform Memory or KnowledgeBase operations when the token is invalid, expired, revoked, or missing the required scope.

## Recommended API surface

### New site endpoints

- POST /api/entitlements/issue
  - issues a signed token for a validated account and requested features
- GET /api/entitlements/verify
  - verifies that a user or client currently has a valid entitlement
- POST /api/entitlements/revoke
  - revokes a previously issued token or disables a user’s entitlement

### Daemon and bundled Data Manager behavior

- on startup, call /api/entitlements/verify using the authenticated account context or a bootstrap credential
- on a periodic interval such as every two days, call /api/entitlements/heartbeat or a similar lightweight endpoint to confirm entitlement remains valid
- if verification fails, exit immediately
- if the heartbeat cannot be completed because the site is unavailable, allow the service to continue locally for a grace period rather than shutting down immediately; when connectivity is restored, enforce the next validation cycle and stop if the subscription is no longer valid
- log the reason clearly for supportability and operations

### Binding behavior

- on first use, request a token from the site or receive one from the application layer
- attach the token to the feature request or session
- validate the token locally before enabling premium features
- fail with a clear licensing error if the token is invalid or lacks the required feature scope

## Security model

### Token properties

The entitlement token should be:

- short-lived
- signed
- scoped to the feature set that is allowed
- bound to an audience such as daemon, SDK, or microservice
- revocable through the site control plane

### Security requirements

- never rely on a static client secret as the sole entitlement mechanism
- support revocation and denylisting for suspended or compromised accounts
- use HTTPS for all entitlement exchange paths
- keep the signing key server-side and rotate it periodically
- include a unique token ID to make revocation and audit easier

## Failure behavior

The system should fail closed in all of the following cases:

- no entitlement token is present
- the token is expired
- the token signature is invalid
- the token audience does not match the current client
- the requested feature is not covered by the token
- the account has been suspended, canceled, or otherwise invalidated
- the heartbeat or verification request fails because the new site is unreachable, and the grace period has expired
- the site reports that the subscription is no longer active

The user-facing error can be simple, but internally the implementation should preserve the exact reason code for telemetry and support.

## Operational notes

- log entitlement checks with account ID, plan, feature scope, and outcome
- record revocation and denial events for support and auditing
- include a small metrics surface for entitlement checks, token issuance, and failed authorization attempts
- keep a clear distinction between account existence and account entitlement; both matter
- for the release-ready billing model, store subscriptions and payments separately from the user identity record so multiple products and plans can be supported cleanly

## Rollout plan

### Phase 1

- add the new site entitlement endpoints
- add a lightweight heartbeat endpoint for periodic client validation on a two-day cadence
- enforce startup validation in the bundled Data Manager
- make the Data Manager exit when the account cannot be validated
- make the Data Manager stop if the site is unreachable and the heartbeat cannot be completed

### Phase 2

- add signed token issuance for language bindings
- enforce token validation for Memory and KnowledgeBase features
- add revocation support for invalid or canceled accounts

### Phase 3

- expand the same model to microservice deployments and higher-value premium capabilities
- add compliance and audit reporting for entitlement decisions
