# sop-cloud backend

This backend area holds the new product-facing Go services for the SOP Cloud site experience.

## Responsibilities

- The site backend serves the product website and the site-facing API.
- The site account package handles onboarding, subscription, billing, and auth/session integration.
- Shared sopbackend integration provides the reusable auth, monitorability, swagger, and SOP Data Manager UI/backend surface.

## UI/backend reuse model

The shared SOP UI/backend supports two distinct deployment contexts:

1. Site-admin/root management: the hosted site app exposes the SOP Data Manager UI/backend so SA/root users can manage environments, databases, stores, and spaces.
2. End-user local workflows: the UI is pointed at a per-user daemon instance running on the user's machine so local-hosted AI workflows can be driven from the same UI experience.

## Packaging model

The new site should be deployable as one container-friendly or baremetal process that includes the site frontend plus the shared UI/backend handlers. The daemon remains a separate headless runtime artifact for release-based deployment and local-hosted use.
