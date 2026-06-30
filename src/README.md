# src

This top-level source tree holds the new product-facing application layer for the SOP Cloud site.

## Layout

- frontend: Vite + React app for the public web experience
- backend: Go services for the new site API and account flow
  - siteapi: self-registration, account, subscription, and onboarding handlers
  - site: standalone backend entrypoint for local site development
- operations: deployment and operational scripts for the sop-cloud product experience

The mature SOP Data Manager and daemon code remain in [lib/sop](../lib/sop) and are not part of this new product layer.

## What belongs here

Use this tree for product-driven behavior that is specific to sop-cloud rather than to the shared SOP runtime:

- public-site experience and landing-page UI
- account registration and self-service flows
- subscription and onboarding handlers
- account unregister and account-management flows
- deployment or operations tooling for the new product experience
- any frontend/backend integration for the new product experience

## What should stay outside this tree

Keep shared platform behavior in [lib/sop](../lib/sop). Avoid adding new site-specific logic there unless it is clearly reusable by the broader runtime.

## Architecture conventions

- Prefer explicit request structs and explicit parameters over passing state through context values.
- Keep backend handlers focused on request parsing, validation, and orchestration.
- Let reusable runtime concerns stay in the shared platform layer, while the site-specific orchestration remains in this tree.
- If a feature is aimed at the product experience rather than the platform, implement it here.

## How to think about the two experiences

- Mature tool stack: run the existing SOP Data Manager apps from [lib/sop](../lib/sop)
  - httpserver for the Data Manager UI and management experience
  - sop-daemon for HQ/local RAG setup and runtime services
- New site/app: use the standalone backend entrypoint in [src/backend/site](backend/site) for the React website experience; do not treat [lib/sop/tools/httpserver](../lib/sop/tools/httpserver) as the launcher for the product site UI.
- Architecture rule for the new site: do not use context.Context as a general-purpose state container for request data or workflow state. Pass explicit parameters or request structs across handlers, services, and API boundaries.
- This is the place for self-registration, landing page, subscriptions, starter-kit flows, account unregister, and other product-specific features.
- Treat register, unregister, and subscription updates as site/product-facing flows rather than core Data Manager or daemon endpoints.
