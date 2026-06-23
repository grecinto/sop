# Private Repo Split Plan

This document captures the intended boundary between the public SOP engine and the private overlay repository. The public repository stays focused on the generic AI engine and runtime primitives; this repository carries the deployment-specific runtime, auth, policy, packaging, and host/HQ integration logic.

## Goal

Treat this repository as a continuation layer over the public repository.

That means:

- the public repository remains the base engine,
- this repository depends on that engine,
- and this repository adds the product-specific daemon and orchestration logic on top.

## Suggested repository boundary

### Keep in the public repository

Keep code that is broadly reusable and not tied to this private deployment pattern:

- generic AI runtime packages
- agent abstractions
- embedder and generator interfaces
- shared core data structures
- public examples and documentation

### Keep in this private repository

Keep the code that encodes this deployment topology, entitlement model, and host-side behavior:

- daemon entrypoints
- local RAG bootstrap
- HQ/host communication layer
- signed-token enforcement and policy handling
- local model/cache workflow
- private config and deployment assets
- packaging logic for the downloadable host runtime

## Recommended private overlay layout

A practical layout is:

```text
gerardo/
  cmd/
    sop-daemon/
  private/
  deployment/
  docs/
  vendor/
    sop/   <- public repo as submodule or dependency
```

## Integration options

### Option 1: Git submodule

Best when you want a stable anchor to the public repository.

Workflow:

1. Add the public repo as a submodule inside the private repo.
2. Put proprietary code beside it.
3. Pull public updates when needed.

Example:

```bash
git submodule add https://github.com/your-org/sop.git vendor/sop
```

### Option 2: Dependency overlay

Best when you want the private repo to feel like a product overlay over the public repo.

Workflow:

1. Keep the public repo as a dependency or remote.
2. Pull updates from it when needed.
3. Merge or copy the relevant public tree into your private repo structure.

## Recommended package mapping

Use the same package paths where possible so the private repo feels like a continuation of the public repository.

Examples:

- public: cmd/sop-daemon
- private: cmd/sop-daemon (proprietary daemon runtime)
- public: ai/
- private: ai/ (only if you need private extensions or overlays)

## What to document in the private repo

At minimum, the private repo should contain:

- a README that states the public/private split,
- a dependency note for the public repo,
- the host runtime architecture,
- the HQ/host protocol design,
- the signed-token enforcement model,
- the packaging and deployment instructions.

## Practical migration rule

If the split is needed, do it in layers:

1. first move or duplicate the daemon/runtime boundary,
2. then move the auth and policy layer,
3. then move the deployment and packaging logic,
4. and finally leave the generic engine in the public repo.

## Suggested future handoff note

When resuming later, the next engineer should be able to answer these questions from the private repo docs:

- what belongs in the public repo,
- what belongs in the private repo,
- how the private repo depends on the public repo,
- how the host daemon and HQ communicate,
- and how signed-token enforcement works offline and online.
