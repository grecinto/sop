# Distributed RAG Topology

This package captures the rollout primitives for a multi-tier RAG architecture:

1. HQ RAG in a managed cluster (for example on GoDaddy or AWS)
2. Downloadable RAG for a user's host machine
3. A UI in HQ that can download the host bundle and switch to it as the active server-side RAG endpoint

## Core ideas

- The same agent/config runtime powers both HQ and host deployments.
- The host deployment is packaged as a portable bundle that contains the agent config and the local storage payload.
- The HQ UI can direct Ask traffic to either the HQ cluster RAG or the downloaded host RAG by swapping the endpoint target.

## Components

- Bundle export: create a portable archive for a host deployment.
- Remote Ask transport: send Ask requests to a remote endpoint.
- Future extension points: signed bundles, version pinning, manifest validation, and remote health checks.
