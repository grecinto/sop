# Joltrin SDK History

Joltrin is the current name of the SDK. It originated as Scalable Objects Persistence (SOP). The project repository is now [github.com/sharedcode/joltrin](https://github.com/sharedcode/joltrin); Go imports and the module declaration continue to use `github.com/sharedcode/sop` for compatibility.

The open-source project formerly known as SOP is the foundation behind Joltrin. It is released under the MIT License and remains the long-term community base for the architecture, runtime, and data platform concepts that Joltrin builds on.

## Why the SDK exists separately

The Joltrin SDK carries the newest working and stable source code for subscribers.

The public Joltrin repository continues to receive the architecture after it has had more time to mature in production and stabilize across real-world usage. In practice, that means the community version is intentionally slightly older than the subscriber Joltrin SDK source.

This is mainly a release-maturity decision, not a change in technical direction. New features need time to solidify before they are pushed into the public Joltrin line. Because of that stabilization window, public Joltrin updates for newer SDK capabilities are typically at least six months behind the subscriber source release.

## Source availability model

- Joltrin open-source repository: community-accessible and MIT licensed
- Joltrin SDK subscriber source: latest working, stable version available as part of the monthly subscription plan
- Public Joltrin updates: follow after features have matured enough for broader open-source release

This model gives subscribers earlier access to the latest stable Joltrin implementation, while the broader community continues to receive a more time-tested public release.