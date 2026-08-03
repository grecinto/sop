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

That split is no longer ideal. First-boot provisioning should be standardized across hosts.

## Decision

The Setup Wizard must become reusable across hosts, including daemon.

That means `SetupWizard` is no longer only an HQ concern. It becomes the standard provisioning framework for any host that starts without a valid environment.

## Required model

Provisioning should support two modes:

1. `Simple` setup
2. `Advanced` setup

For all host kinds, the first-boot provisioner style should be the same:

- present the same provisioning chooser UI
- preselect `Simple`
- allow the user to switch to `Advanced`

This means HQ, daemon, and future Corporate server should not diverge on first-boot provisioning style. They may differ internally in runtime defaults and host capabilities, but the user-facing entry experience should be consistent.

### Simple setup

Simple setup should reuse the current daemon quick provisioner behavior:

- fast first boot
- minimal required inputs
- reasonable local defaults
- standalone/local system DB creation
- default knowledge bootstrap

This is the path for:

- local daemon first boot
- low-friction developer setup
- unattended or near-unattended local setup

### Advanced setup

Advanced setup should reuse the Setup Wizard flow:

- explicit path/database selection
- erasure coding and storage layout choices where relevant
- environment-specific auth/bootstrap configuration
- more guided provisioning and validation

This is the path for:

- HQ deployment
- future Corporate server deployment
- advanced daemon setup when the operator needs full control

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

The shared runtime should default that choice to `Simple` for all host kinds, while always keeping `Advanced` available from the same entry surface.

## Refactor implication

The daemon quick provisioner should not remain a standalone ad hoc path.

Instead:

- keep its behavior
- repackage it as the `Simple` provisioning mode under the shared provisioning framework
- let Setup Wizard infrastructure host both `Simple` and `Advanced` flows
- keep configured system DB repair in shared runtime bootstrap rather than a daemon-only startup branch

This provides one provisioning architecture with two operator experiences.

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
