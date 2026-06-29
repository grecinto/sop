# Development Guide

This document is the short orientation guide for future developers and maintainers of this repository.

## Repository shape

This repo is a private overlay on top of the shared SOP implementation.

- [lib/sop](../lib/sop): the mature SOP platform code. This includes the existing Data Manager, shared runtime, and daemon-oriented capabilities.
- [src](../src): the product-facing sop-cloud application layer. Use this tree for site-specific UX, registration flows, onboarding, subscriptions, and account management.
- [docs](.) and the repository root: product stories, implementation notes, and repository-level guidance.

## Two runtime tracks

There are two distinct experiences in this repository:

- The platform is intended to behave as a first-class microservice surface. Developers should think in terms of capabilities and APIs first, and storage backend choice second.
- The mature HTTP server and daemon should expose stable functionality over HTTP so other services and third-party integrations can consume the same capabilities without depending on one storage engine.

1. The mature platform experience
   - Lives in [lib/sop](../lib/sop)
   - Includes the original Data Manager and the shared runtime/daemon workflow
   - Should remain the stable platform layer for core runtime behavior

2. The new sop-cloud product experience
   - Lives in [src](../src)
   - Includes the new site backend and planned frontend experience
   - Holds self-registration, onboarding, account management, and subscription logic
   - Treat register, unregister, and subscription updates as site/product-facing flows rather than core Data Manager or daemon endpoints

## Maintenance rules

Keep the boundary clear:

- Put shared runtime behavior in [lib/sop](../lib/sop)
- Put product-specific behavior in [src](../src)
- Avoid mixing the new site experience into the mature platform code unless the change is genuinely reusable by the wider runtime
- When adding a feature, describe it in terms of the user-facing or integration-facing capability it provides, not in terms of which storage engine happens to back it today
- If the topic is SOP DB internals, keep that discussion in the relevant storage/runtime package documentation rather than in the product-facing layer
- Swagger/OpenAPI docs are available through the shared backend wiring for both the HTTP server and daemon at /swagger.json and /swagger/.
- Observability for the product site, shared backend, onboarding flow, auth, and subscriptions is tracked in [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md).
- Shared monitorability endpoints should follow the Prometheus-compatible contract described in [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md): /healthz for health and /metrics for scrapeable metrics.
- For the new site and shared backend, do not use context.Context as a general-purpose state bucket for request values or domain state. Prefer explicit parameters or explicit request structs in handlers, service methods, and API boundaries.

## Design conventions

- Prefer explicit request structs and explicit parameters over context-based state passing.
- Keep handlers focused on parsing input, validating it, and coordinating calls.
- Put orchestration for the product experience in the site/backend layer, not in the shared runtime.
- Document new product features in the docs tree so future maintainers can find them quickly.

## Where to look first

- [README.md](../README.md): repository purpose and high-level architecture
- [src/README.md](../src/README.md): the product-layer layout and conventions
- [docs/NEW_SITE_STORIES.md](NEW_SITE_STORIES.md): product stories for the new site experience
- [docs/NEW_SITE_IMPLEMENTATION_TASKS.md](NEW_SITE_IMPLEMENTATION_TASKS.md): implementation task list
