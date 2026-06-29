# Maintainer Guidance: public/private positioning

This repository is a private overlay built on top of the shared public SOP implementation. The public upstream engine remains the foundation for generic runtime, AI, database, and coordination behavior, while this overlay carries the deployment-specific and product-specific experience.

## Recommended posture

- Keep the public repository useful, credible, and broadly reusable.
- Keep the differentiated product experience private or selectively exposed.
- Treat the public repo as the platform foundation, not as the full business product.

## What should stay public

The public-facing repository should continue to host capabilities that are broadly useful to developers and that do not reveal the full differentiated product strategy:

- generic runtime primitives
- core AI and database behavior
- shared engine capabilities
- reusable platform pieces that can help others build on top of SOP

## What should stay private or overlay-specific

Keep the following in the private overlay or product layer unless there is a strong reason to upstream them:

- product site UX and onboarding flows
- self-registration, account-management, and subscription workflows
- MA/API experience and product-facing API layers
- private deployment wiring, observability patterns, and support workflows
- any logic that would make the proprietary product experience easy to copy

## How to decide where a change belongs

When introducing or moving work, ask:

1. Is this a general runtime capability that other developers would reasonably want upstream?
2. Or is it a product-specific experience that exists to serve the private platform or hosted offering?

If it is product-specific, keep it in the overlay or product tree. If it is broadly reusable and not tied to the private product experience, it can be considered for the public upstream.

## Release narrative

The best public story is:

- “SOP Public” is the open-source engine and platform foundation.
- “SOP Cloud / the richer product experience” is the private or hosted layer that adds the product UX, MA capabilities, workflows, and commercial value.

That keeps the public repo valuable without giving away the full moat.

## Maintainer rule of thumb

When in doubt, prefer this principle:

- publish the foundation,
- keep the differentiated experience private,
- and document the boundary clearly so future maintainers do not accidentally move product value into the public layer.
