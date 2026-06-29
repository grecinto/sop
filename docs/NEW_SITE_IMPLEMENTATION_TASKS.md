# New Site Implementation Tasks

## Phase 1: Onboarding flow
- [ ] Add a post-payment download/install step after the subscription confirmation.
- [ ] Link the install step to the SOP GitHub Releases page for the release binary.
- [ ] Add copy explaining what the local agent does and what happens next.

## Phase 2: Returning-user experience
- [ ] Ensure the login flow remains the entry point for returning users.
- [ ] Show a personalized landing page after authentication with plan and subscription state.
- [ ] Keep the account page available after sign-in for recurring account management.

## Phase 3: Subscription and account management
- [x] Add an unsubscribe action from the landing page.
- [x] Persist the unsubscribed state in the user record and reflect it in the UI.
- [x] Add an unregister action that removes the account and revokes sessions.
- [x] Redirect the user to a signed-out experience after unregistering.
- [x] Add a second plan entry for the future HQ + Local Agent experience.
- [x] Mark the second plan as unavailable for regular users while keeping it visible for admin testing.

## Phase 4: Architecture and packaging
- [ ] Create a dedicated React frontend package for the new site/app.
- [ ] Serve the React site/app from httpserver on its own routes without wiring it into the daemon.
- [ ] Keep sopbackend as the shared runtime/business-logic layer used by both httpserver and the daemon.
- [ ] Preserve the existing Data Manager experience as a separate UI surface served by httpserver.

## Phase 5: Polish and validation
- [ ] Add UI tests for the new download/install step and account actions.
- [ ] Add backend tests for subscription-state updates and unregister behavior.
- [ ] Verify the full UI and auth flow end to end.

## Phase 6: Monitorability and operations
- [ ] Standardize the shared monitorability API contract across the new site backend, httpserver, and daemon.
- [ ] Expose /healthz, /readyz, and /metrics from each instance using the shared backend contract.
- [ ] Ensure the daemon and httpserver expose the same MA shape so Prometheus can scrape them consistently.
- [ ] Add site-specific monitoring metrics for onboarding, auth, and subscription flows.
- [ ] Document the Prometheus/Grafana integration path for operators and SAs.
