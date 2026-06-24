# Distributed RAG Topology

This package captures the rollout primitives for a multi-tier RAG architecture:

1. HQ RAG in a managed cluster (for example on GoDaddy or AWS)
2. Downloadable RAG for a user's host machine
3. A UI in HQ that can download the host bundle and switch to it as the active server-side RAG endpoint

## Core ideas

- The same agent/config runtime powers both HQ and host deployments.
- The host deployment is packaged as a portable bundle that contains the agent config and the local storage payload.
- The HQ UI can direct Ask traffic to either the HQ cluster RAG or the downloaded host RAG by swapping the endpoint target.
- Local RAG is intended to be a full-featured RAG runtime, not a reduced or degraded experience compared with HQ.

## Local RAG deployment modes

### 1. Simple / one-click setup

This is the default experience for most users.

- The system defaults to a standalone local deployment.
- All key settings are pre-selected or defaulted so the user can complete setup in a single click.
- The runtime should bootstrap the local RAG environment with minimal user interaction.
- This path is optimized for speed, ease, and low-friction onboarding.

### 2. Advanced setup

This path preserves the richer configuration experience already available through the existing Setup Wizard.

- Users can choose the full advanced flow when they want more control.
- The setup wizard can still configure the full local RAG environment, including advanced options and custom deployment choices.
- This path is intended for power users, enterprise deployments, or cases where specific tuning is needed.

## Deployment model

The system should support both deployment styles:

- Standalone local RAG for simple, single-user, or low-friction deployments.
- Clustered or more advanced local RAG deployments when the user explicitly chooses them through the advanced setup flow.

The important principle is that the platform remains flexible while defaulting toward a simple, autonomous onboarding experience.

## Authentication and governance

Since each UI/RAG session is governed and protected by HQ authentication, the platform can track usage and enforce limits centrally.

- Authentication is the primary requirement for setup and runtime access.
- A signed token flow is sufficient to protect the local runtime without requiring intrusive manual setup.
- HQ can enforce quotas, entitlement checks, and policy boundaries even when the local RAG instance is running independently.
- The system should preserve minimal intrusion: users should not need to perform complicated manual configuration beyond authenticating and optionally selecting the advanced setup path.

## Components

- Bundle export: create a portable archive for a host deployment.
- Remote Ask transport: send Ask requests to a remote endpoint.
- Setup flow: support a one-click default standalone startup path plus the existing advanced Setup Wizard.
- Policy enforcement: use authenticated token flow to gate access, track usage, and support future rate limiting or quota enforcement.
- Future extension points: signed bundles, version pinning, manifest validation, remote health checks, and policy refresh.
