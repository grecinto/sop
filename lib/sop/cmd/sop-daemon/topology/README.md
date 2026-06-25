# Unified Topology for SOP Runtime

This package captures a unified deployment model for SOP in which the same runtime code can operate in multiple hosting environments without branching the product experience.

The design supports three first-class deployment surfaces:

1. Local host runtime on the user’s machine
2. HQ-managed runtime in the cloud
3. Premise HQ or self-hosted HQ runtime for advanced enterprise deployments

## Core design principle

SOP is designed around a single, shared runtime and configuration model that can be hosted in different places while preserving one product experience.

- The same engine, agent runtime, and API contracts power local, HQ, and premise deployments.
- The deployment target changes where the runtime lives, not how it behaves.
- The UI can switch between runtime modes while preserving the same core workflow and the same underlying knowledge and policy model.
- The local deployment is a full runtime, not a reduced or degraded mode.

## Why this topology is strong

This model is efficient because it avoids separate implementations for each environment.

- One code set reduces maintenance and rollout risk.
- One runtime contract keeps features and behavior consistent.
- Local execution gives low latency, privacy, and offline-friendly operation.
- HQ or premise HQ provides shared governance, central policy, and cross-user coordination.
- Advanced deployments can move more of the runtime and data ownership to the customer environment while keeping the same software foundation.
- This is a key advantage over database-specific stacks: in SQLite or Postgres, the backend endpoints, database management logic, and operational behavior often diverge, while SOP preserves a unified 100% code base across deployment surfaces.

## Deployment modes

### 1. Local host mode

This is the default experience for a single user or a personal workstation.

- The daemon runs on the user’s machine.
- Local RAG, local command execution, and local workflow help are handled directly on that host.
- The experience remains responsive and self-contained while still being able to hand off identity and context to HQ when needed.
- This mode is ideal for privacy-sensitive use, low-latency interaction, and offline-friendly operation.

### 2. HQ mode

This is the shared cloud-hosted experience.

- The runtime operates in HQ-managed infrastructure.
- Shared policy, identity, and central orchestration remain in the HQ environment.
- The same code path can serve the web experience and the connected runtime model without a separate implementation.
- This mode is ideal for multi-user, managed, and centrally governed deployments.

### 3. Premise HQ / advanced mode

This is the enterprise and self-hosted extension of the same architecture.

- The runtime is deployed behind the customer’s own HQ or premise infrastructure.
- The deployment can keep more data, policy, and runtime ownership on-premise while still using the same unified code base.
- HQ acts as the control plane, policy authority, or coordination layer when desired.
- This mode is ideal for regulated environments, private networks, or organizations that want tighter infrastructure control.

## Runtime model

The system should support a unified runtime model across all deployment targets:

- Same agent/config/runtime implementation
- Same request/response contracts
- Same authentication and policy hooks
- Same workspace and knowledge model
- Different hosting surfaces depending on the deployment

That makes the platform flexible while keeping the implementation simple and robust.

## Authentication and governance

The topology preserves a strong governance model without forcing a split implementation.

- Authentication and entitlement remain central even when the runtime is local.
- HQ or premise HQ can enforce policy, quotas, and identity checks.
- The local runtime can continue operating independently when connectivity is limited, with cached policy and local enforcement.
- This creates a strong balance between autonomy and centralized control.

## Components

- Unified runtime packaging for local, HQ, and premise deployments
- Shared transport and policy contracts between runtime surfaces
- Bootstrap and setup flow for simple local onboarding and advanced enterprise deployment
- Policy enforcement, identity continuity, and runtime switching across deployment modes
- Future extension points for signed bundles, version pinning, health checks, and policy refresh
