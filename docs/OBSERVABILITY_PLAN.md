# Observability plan for the new site and shared backend

This document captures the recommended observability approach for the new product site, the shared backend, and the supporting runtime entrypoints.

## Current status

The new site experience is now functionally in place:

- the product-facing site shell is running
- account registration and starter onboarding are available
- subscription changes and unregister flows are wired up
- Swagger docs are exposed from the shared backend path
- the frontend and site backend are buildable and testable

That means the system is at a good checkpoint for adding observability. The product flow is stable enough to instrument meaningfully, and the architecture boundary between product behavior and shared runtime behavior is clear.

## What we should observe

The monitoring plan should focus on the workflows that matter most to users and operators:

- authentication and account lifecycle
  - login
  - registration
  - current-user lookup
  - unregister
- subscription and onboarding
  - subscription change requests
  - starter-plan activation
  - onboarding completion or failure
- runtime health
  - httpserver and daemon startup
  - API health and error rates
  - shared backend initialization failures
- supportability signals
  - correlation IDs for requests
  - account and plan context in logs
  - clear failure reasons for onboarding and subscription issues

## Recommended signal set

### Metrics

Track these as baseline metrics:

- request rate by endpoint
- latency by endpoint
- error rate by endpoint
- auth success/failure rate
- subscription success/failure rate
- unregister success/failure rate
- active accounts and active subscriptions
- site availability and backend availability

### Logs

Use structured logs with these fields when possible:

- request ID or correlation ID
- route or endpoint
- action name such as register, login, subscribe, unregister
- account ID or username when available
- plan value
- outcome success or failure
- error class and error message

### Tracing

Add distributed tracing for the end-to-end path:

- browser or client request
- site backend
- shared backend handlers
- runtime and auth flow
- downstream services if present

This will make issues in the onboarding and subscription flow much easier to debug.

## Alerting priorities

Start with alerts for the highest-impact failures:

1. rising 5xx and 4xx rates on auth and subscription endpoints
2. registration or login failures spike
3. subscription changes fail repeatedly
4. daemon or httpserver startup fails
5. onboarding completion drops unexpectedly

## Dashboard and operations view

Operations and support teams should have a simple dashboard with:

- current service health
- request volume and error rate
- auth success/failure rate
- subscription flow completions
- recent failed operations
- recent registration and unregister events

## Recommended implementation order

### Next implementation step

Track the shared monitorability API work as the next concrete operational milestone:

- standardize the contract across the new site backend, httpserver, and daemon
- expose /healthz, /readyz, and /metrics from each instance
- keep the shared shape consistent for Prometheus scraping
- add product-specific metrics for auth, onboarding, and subscription flows

### Phase 1: baseline observability

Implement the smallest useful layer first:

- structured logs for auth and subscription endpoints
- basic metrics for request rate, latency, and error rate
- health/status endpoints for site and shared backend
- a shared monitorability contract for each instance exposing Prometheus-compatible metrics and health endpoints

### Monitorability API contract

Each instance should expose a small, standard surface that can be consumed by Prometheus, Grafana, and other monitoring tooling:

- GET /healthz
  - lightweight health response suitable for liveness checks and simple status pages
- GET /metrics
  - Prometheus text exposition format for scrape-based monitoring
- optional GET /readyz
  - readiness signal once the instance is fully initialized and dependencies are ready

This contract should be implemented in the shared backend layer so the new site backend, the Data Manager httpserver, and the daemon can all expose the same interface.

### Phase 2: operational dashboards and alerts

Add:

- Grafana or similar dashboards
- alerting for auth and subscription failures
- visibility into startup and runtime health
- a Prometheus scrape configuration example such as [prometheus-scrape-example.yml](prometheus-scrape-example.yml)
- a starter alert rule set such as [prometheus-alerts-example.yml](prometheus-alerts-example.yml)
- a starter Grafana dashboard example such as [grafana-dashboard-example.json](grafana-dashboard-example.json)

### Phase 3: richer tracing and support workflows

Add:

- distributed tracing
- correlation IDs across requests
- support-focused views for account and subscription state

## Readiness assessment

Yes, this is the right point to add observability.

The product surface is now sufficiently shaped to justify instrumentation, and the system has reached a stable enough milestone that monitoring will provide real value instead of just adding noise. The remaining gap is not product functionality; it is operational readiness.

In short:

- functionality: mostly in place
- documentation and architecture: in place
- observability implementation: not yet added
- readiness to add observability: yes, now is the right time
