# SOPBackend Host Framework Refactor Plan

Last updated: 2026-08-03.

## Purpose

This document captures the refactor direction for shaping `sopbackend` as the shared host framework for three federating server types:

- HQ / HTTPServer
- Local daemon
- Future Corporate server

The goal is not to force identical storage implementations. The goal is to make host composition, provisioning, federation, auth wiring, and lifecycle management consistent enough that a new host type can be added without duplicating framework code.

## Current Assessment

`daemon` and `httpserver` already share a substantial amount of behavior through `sopbackend`:

- runtime initialization
- shared API handlers
- shared session and token semantics
- shared federation handlers
- shared background cleanup and maintenance
- shared knowledge-base bootstrap behavior

That is the correct architectural center.

The remaining shaping gaps are:

1. `sopbackend` still relies heavily on package-global state.
2. Background service hooks and shutdown state are global, not host-instance-owned.
3. Shared federation primitives still use daemon-specific names even though the next architecture step is peer/server federation.
4. Host bootstrapping is still partly manual in each app.
5. First-boot provisioning is inconsistent between HQ and daemon.

## Target Shape

`Sopbackend` should become a reusable host runtime framework with explicit host profiles.

### Host profiles

Add an explicit host kind or profile layer such as:

- `hq`
- `daemon`
- `corporate`

Each profile should declare:

- auth mode
- route groups
- provisioning mode
- background services
- federation role and capabilities
- optional host-only features

This keeps the shared runtime common while allowing host-specific edges.

### Shared framework responsibilities

The shared layer should own:

- runtime initialization
- common route registration
- background service lifecycle
- common auth/session wiring contracts
- federation peer abstractions
- first-boot provisioning contracts
- knowledge-base bootstrap and post-provision preload hooks

### Host-specific responsibilities

Each host should own:

- UI shell and transport specifics
- auth entry UX
- host-only routes
- local environment constraints
- storage choices where they truly differ

## Provisioning Direction

## Problem

HQ already has a Setup Wizard-oriented first-boot model.

The daemon currently has a simpler quick provisioner path and auto-provisions pieces when local configuration or system DB state is missing.

That split is still undesirable from a UX perspective, but the provisioning implementation should not be standardized all the way down to shared backend semantics.

## Decision

The Setup Wizard visual language must become reusable across hosts, including daemon.

That does not mean daemon should reuse HQ/httpserver provisioning handlers. Daemon will have its own provisioning implementation and request contracts, while matching the same general style and mode chooser UX.

## Required model

Provisioning should support two modes:

1. `Simple` setup
2. `Advanced` setup

For all host kinds, the first-boot provisioner style should be the same:

- present the same provisioning chooser UI
- preselect `Simple`
- allow the user to switch to `Advanced`

This means HQ, daemon, and future Corporate server should not diverge on first-boot provisioning style. They may differ internally in runtime defaults and host capabilities, but the user-facing entry experience should be consistent.
They may also differ in provisioning payloads, backend side effects, and post-provision initialization.

### Simple setup

Daemon simple setup should reuse the current daemon quick provisioner behavior:

- fast first boot
- minimal required inputs
- reasonable local defaults
- standalone/local system DB creation
- default knowledge bootstrap

This is the path for:

- local daemon first boot
- low-friction developer setup
- unattended or near-unattended local setup

For HQ/httpserver, simple setup remains the existing shared Setup Wizard path backed by `sopbackend` config-save semantics.

### Advanced setup

Advanced setup should be host-specific behind a shared visual shell:

- HQ/httpserver advanced setup keeps the full Setup Wizard flow
- daemon advanced setup exposes only System DB provisioning controls
- daemon advanced setup should only provision a more advanced System DB shape:
	- explicit system DB path
	- standalone vs clustered mode
	- Redis URL when clustered
	- stores folders
	- registry hash mod
	- erasure coding / partitioning
- daemon advanced setup must not include HQ-only steps such as root-password seeding, users-store bootstrap, or page-3 LLM/embedder setup

This is the path for:

- HQ deployment
- future Corporate server deployment
- advanced daemon setup when the operator needs explicit control over local System DB topology

## Standard trigger

The standard trigger should be:

- environment is not found
- `config.json` is missing
- required system DB / runtime configuration is not available

When this happens, the host should enter a common provisioning decision path:

- choose simple setup
- choose advanced setup
- complete provisioning
- continue normal runtime bootstrap

Each host should default that choice to `Simple`, while always keeping `Advanced` available from the same entry surface.

## Daemon-specific implementation contract

The daemon must implement its own provisioning contract under `cmd/sop-daemon`.

Required daemon endpoints:

1. `GET /api/daemon/provision/status`
2. `POST /api/daemon/provision/simple`
3. `POST /api/daemon/provision/system`

Expected daemon provisioning states:

- `unprovisioned`: no valid daemon config exists yet
- `partial`: config exists but required System DB/runtime state is incomplete
- `ready`: daemon config and System DB are present and runtime can continue normal bootstrap

`Simple` provisioning contract:

- explicit user-triggered replacement for today's daemon quick-provision startup behavior
- create daemon config if missing
- create default standalone System DB
- use local defaults
- avoid HQ/httpserver concepts such as root-user creation, users-store bootstrap, or page-3 configuration

`Advanced` provisioning contract:

- provision only the daemon System DB
- allow:
	- path
	- standalone vs clustered mode
	- Redis URL
	- stores folders
	- registry hash mod
	- erasure coding / partitioning
- do not expose user DB creation
- do not expose HQ/httpserver-specific account bootstrap

Daemon UI contract:

- reuse the Setup Wizard design language only
- default to `Simple`
- expose an `Advanced` button
- in advanced mode, show only the System DB page equivalent
- do not reuse HQ provisioning handlers or payload contracts from `sopbackend`

Lifecycle constraint:

- daemon startup should no longer auto-create missing `config.json`
- daemon startup should no longer silently auto-provision a missing System DB
- daemon should stay alive in an unprovisioned state and wait for the UI client to trigger one of the daemon provisioning endpoints

## Refactor implication

The daemon quick provisioner should not remain a standalone startup side effect.

Instead:

- keep its behavior
- move it behind explicit daemon provisioning endpoints
- let daemon UI host both `Simple` and `Advanced` flows using daemon-only request/response contracts
- keep HQ/httpserver provisioning in `sopbackend`
- stop treating missing daemon config or missing daemon system DB state as something startup should auto-repair silently

This provides one design language with separate provisioning architectures for separate host responsibilities.

## Federation Direction

To support HQ, daemon, and Corporate server cleanly, shared federation concepts should move from daemon-specific naming toward peer/server naming.

### Preferred direction

Examples:

- `DaemonRegistry` -> `FederationPeerRegistry` or `ServerRegistry`
- `daemon_id` -> `peer_id` or `server_id`
- shared federation route group named by federation purpose rather than by daemon identity

Compatibility aliases can remain during migration, but the shared model should stop assuming that every federating participant is a daemon.

## Session and cleanup model

The session cleanup policy should stay shared.

The removal mechanism should stay storage-specific.

That means:

- in-memory daemon session structures can prune map-backed entries
- HQ or Corporate persistent stores can remove B-tree-backed session records transactionally
- both still use the same stale-session policy, timing, and background-service semantics

## Recommended refactor sequence

### Phase 1: Stabilize shared host wiring

- keep one canonical shared route registration path
- keep one canonical background-service registration path
- remove route drift between host entrypoints
- centralize common host bootstrap steps

Status:

- route registration parity between `RegisterAllHandlers` and `Backend.RegisterHandlers` has been tightened

### Phase 2: Introduce host profile abstraction

Add a host profile/config shape in `sopbackend` that describes:

- host kind
- auth style
- enabled route groups
- enabled feature groups
- provisioning mode availability
- federation role

This is the minimum shape needed before a third server type is added.

### Phase 3: Unify provisioning

Build a shared provisioning framework that:

- detects missing environment/config
- launches Setup Wizard-compatible provisioning flow
- supports `Simple` and `Advanced` modes
- allows daemon to use the same framework as HQ
- keeps current daemon quick-provisioning behavior as the `Simple` mode implementation

### Phase 4: Rename shared federation primitives

Move shared names from daemon-specific to peer/server-specific while keeping compatibility shims.

### Phase 5: Reduce package-global state

Move these toward backend-instance-owned or host-runtime-owned state where practical:

- loaded agents
- active sessions
- background hook registry
- background shutdown channels
- host service lifecycle state

This is the step that makes `sopbackend` truly reusable as a host framework instead of a shared singleton package.

## Proposed next steps

1. Define a `HostProfile` or `ServerKind` type in `sopbackend` and document the first three concrete values: HQ, daemon, corporate.
2. Introduce a shared provisioning contract that chooses `Simple` or `Advanced` setup when `config.json` or equivalent runtime state is missing.
3. Reuse the current daemon quick provisioner as the implementation of `Simple` setup instead of maintaining it as a separate bootstrap path.
4. Move Setup Wizard logic so it can be hosted by daemon as well as HQ.
5. Rename shared federation concepts away from daemon-only naming in the shared layer.
6. Continue reducing global state so backend lifecycle becomes host-instance-aware.

## Outcome we want

After this refactor:

- HQ, daemon, and Corporate server all bootstrap through the same host framework
- provisioning is consistent across first boot scenarios
- simple and advanced setup are both first-class
- federation treats participants as peers, not as special-cased daemon clients
- host-specific code becomes thinner and more intentional
- adding a third server type becomes mostly configuration and host-feature wiring rather than backend duplication
