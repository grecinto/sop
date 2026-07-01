# New Site Implementation Tasks

This tracker captures the current implementation state for the new site experience and the next pass that will wire site registration, subscription, and auth handoff into the shared sopbackend experience.

Completed items in the current implementation are marked with [x]. Remaining work for the next pass is left unchecked so it is easy to resume later.

## Current architecture baseline

The current implementation has the structural pieces in place:

- [x] A standalone site backend exists and serves the product-site frontend from [src/backend/site/main.go](../src/backend/site/main.go).
- [x] Site-specific account, subscription, payment, and billing state live in [src/backend/siteaccount/account.go](../src/backend/siteaccount/account.go).
- [x] Site-specific API handlers live in [src/backend/siteaccount/handlers.go](../src/backend/siteaccount/handlers.go).
- [x] Shared auth/session/token logic remains in [lib/sop/tools/sopbackend/auth.go](../lib/sop/tools/sopbackend/auth.go) and [lib/sop/tools/sopbackend/scripts.go](../lib/sop/tools/sopbackend/scripts.go).
- [x] The shared auth middleware already populates the auth context used by httpserver and other backend paths.

## Phase 0: Auth handoff and continuity

These are the highest-priority tasks for the next implementation pass.

- [ ] Make site login issue a shared sopbackend session token and set the standard access cookie/header contract.
- [ ] Make the new site use the same bearer-token semantics as the existing httpserver experience so the user session is compatible across both surfaces.
- [ ] Ensure the auth context carries the same subscription plan and subscription feature list to httpserver after a site login or redirect.
- [ ] Add a redirect path from the site flow into the httpserver UI while preserving the authenticated session identity.
- [ ] Add a token-forwarding bridge so httpserver can continue the session without a second login.
- [ ] Add a host-side daemon bootstrap path that reuses the same authenticated identity and token material for local-agent communication.

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
- [x] Create a dedicated React frontend package for the new site/app.
- [x] Serve the React site/app from the standalone site backend on its own routes without wiring it into the daemon or httpserver.
- [x] Keep sopbackend as the shared runtime/business-logic layer used by the SOP Data Manager apps and the standalone site backend.
- [x] Preserve the existing Data Manager experience as a separate UI surface served by the SOP Data Manager apps (daemon/httpserver).

## Phase 5: Polish and validation
- [ ] Add UI tests for the new download/install step and account actions.
- [ ] Add backend tests for subscription-state updates and unregister behavior.
- [ ] Add end-to-end tests covering site login, token reuse, httpserver access, and daemon bootstrap.
- [ ] Verify the full UI and auth flow end to end.

## Phase 6: Monitorability and operations
- [ ] Standardize the shared monitorability API contract across the new site backend, httpserver, and daemon.
- [ ] Expose /healthz, /readyz, and /metrics from each instance using the shared backend contract.
- [ ] Ensure the daemon and httpserver expose the same MA shape so Prometheus can scrape them consistently.
- [ ] Add site-specific monitoring metrics for onboarding, auth, and subscription flows.
- [ ] Document the Prometheus/Grafana integration path for operators and SAs.

## Phase 7: Space expert / digest-to-Space feature
- [ ] Implement a folder-based blob store for uploaded documents.
- [ ] Add extraction and parsing for common document formats such as PDF, TXT, Markdown, HTML, and DOCX.
- [ ] Add a drag-and-drop digest workflow where the user can drop a source store onto a target Space, choose a template such as Courseware or Code Expert, and track progress.
- [ ] Support an optional advanced custom-instruction mode for power users who want to override or refine the generated prompt.
- [ ] Reuse existing Space / KnowledgeBase primitives for curated knowledge storage.
- [ ] Add expert-mode AI prompt grounding so selected Spaces become an active context source for coding, debugging, and domain-specific questions.
- [ ] Reuse the existing Space document-reference model so each generated knowledge item points back to its source document via doc_id, storage_mode, and payload_ref.
- [ ] Document the end-to-end workflow in [docs/SPACE_EXPERT_IMPLEMENTATION_GUIDE.md](SPACE_EXPERT_IMPLEMENTATION_GUIDE.md).

## Implementation notes for pause/resume

- The auth handoff is the next critical boundary. Site auth should be compatible with the bearer-token and cookie model already used by sopbackend.
- The new site should not introduce a parallel identity stack. It should reuse the shared session, token validation, and entitlement flow.
- The httpserver experience should be treated as a continuation of the same authenticated session, not as a second login experience.
- The daemon should be treated as a host-side delegated client that inherits the authenticated identity from the UI/session rather than creating its own independent login state.

## Definition of done for the next pass

- A user can register or sign in on the new site.
- The user receives a shared sopbackend-compatible access token.
- The user can be redirected into the httpserver UI with that same session intact.
- The httpserver and local daemon continue to operate with the same authenticated identity and entitlement context.
- The implementation is covered by tests and documented for future continuation.

## Resume entrypoint

Start the next implementation pass from the auth-handoff work in Phase 0 and the detailed implementation contract in [docs/NEW_SITE_IMPLEMENTATION_PLAN.md](NEW_SITE_IMPLEMENTATION_PLAN.md). The expected sequence is:

1. site registration/login issues shared session material,
2. the site redirects into httpserver with the same auth context,
3. the local daemon boots from the same token identity,
4. regression tests cover the full path.
