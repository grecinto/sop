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
- [x] The shared SOP UI/backend is intended to support two use-cases: site-admin/root management for the hosted environment and per-user local workflows that target an end-user daemon instance.
- [x] The new site backend is the intended host for the admin-facing UI routes, while the daemon remains a headless runtime for local-hosted AI workflows and release-based deployment.
- [x] The packaging model is container-friendly and baremetal-friendly: one app process can host the product site frontend plus the shared SOP UI/backend, while the daemon remains a separate headless binary/package.
- [x] The shared sopbackend auth path now supports a pluggable user-store override so the new site can own persisted user management without changing the default package behavior.

## UI/backend usage model

The shared SOP UI/backend is expected to act in two different deployment contexts:

1. Site-admin/root management: the hosted site app exposes the SOP Data Manager UI/backend so SA/root users can manage environments, databases, stores, and spaces.
2. End-user local workflows: the UI is repointed to an end-user daemon instance running on the user's machine so local-hosted AI workflows can be driven from the same UI experience.

This split keeps the admin and personal workflows separate while reusing the same underlying UI and backend contracts.

## Phase 0: Agent and workflow foundation

These are the next product- and runtime-level tasks for the v1 copilot experience.

- [ ] Implement a terminal-first entrypoint and a floating-window interaction surface for coding tasks.
- [ ] Add AST/tree-sitter-aware editing primitives for safe refactors, targeted edits, and code transformations.
- [ ] Add repository and knowledge-base retrieval so the assistant can ground responses in project context and expertise.
- [ ] Add provider abstraction for BYO API keys and the initial LLM routing layer.
- [ ] Add local-host persistence and visualization primitives for workflow history and generated artifacts.
- [ ] Add a lightweight evaluation harness using repo-based tasks and regression cases, rather than relying only on anecdotal testing.
- [ ] Reuse proven open-source building blocks for orchestration, parsing, retrieval, and evaluation to accelerate delivery.
- [ ] Define the top v1 workflows that will be marketed as the subscription value proposition, including repository-aware edits, code review support, and knowledge-base-guided automation.

## Phase 0.5: Federation and Fleet Intelligence (✅ COMPLETE)

**Status**: ✅ V1 Complete - Production Ready  
**Completed**: 2026-07-05

The central memory federation system enables daemon nodes to share learned patterns with HQ, creating fleet-wide intelligence that improves all nodes over time.

### ✅ Completed Features

#### Core Infrastructure
- [x] Core shared memory primitives (`agent.PromoteSharedMemoryArtifact`, `agent.RetrieveSharedMemory`)
- [x] Daemon bootstrap token handoff (UI session → daemon tokens)
- [x] HQ-side root user caching for efficient periodic syncs
- [x] Complete test coverage (daemon + HQ handlers)

#### Bidirectional Sync (Push + Pull)
- [x] **PUSH**: Daemon → HQ periodic sync (1 hour + jitter)
- [x] **PUSH**: Sync batch assembly with artifacts array
- [x] **PUSH**: HQ endpoint `/api/daemon/hq-sync` with auth
- [x] **PUSH**: HQ ingestion into root SA LTM with provenance
- [x] **PULL**: HQ endpoint `/api/daemon/hq-retrieve` with semantic search
- [x] **PULL**: Automatic pull after successful push
- [x] **PULL**: Daemon retrieval with configurable limits

#### Federated Content System (Enhanced V1)
- [x] **13 Content Categories**: workflow_pattern (9), error_resolution (5), repair_strategy (5), tool_preference (4), context_gathering (4), security_pattern (3), anti_pattern (3), performance_optimization (3), testing_strategy (3), code_quality (3), safety_heuristic (2), api_design (2), verified_playbook (0, learned over time)
- [x] **49 Seed Patterns**: Comprehensive baseline intelligence (3.5x increase from initial 14)
  - Framework-specific workflows (React, Node.js, databases, Git)
  - Security best practices (input validation, auth, secrets)
  - Performance optimization (caching, batching, lazy loading)
  - Comprehensive error resolution (types, nulls, async, races)
  - Testing methodologies (TDD, isolation, coverage)
  - Code quality maintenance (extraction, consistency, readability)
  - API design standards (REST, error responses)
  - Systematic debugging and refactoring
- [x] **Intelligent Promotion**: Automatic categorization, confidence scoring (0.65-0.95), value filtering
- [x] **Context-Aware Retrieval**: Category-based search, intent detection, ranked results
- [x] **Privacy Safeguards**: Text truncation (2000 chars), metadata filtering, no PII
- [x] Implementation in `cmd/sop-daemon/federation_content.go` (~1,100 lines)
- [x] Integration with ask handler in `cmd/sop-daemon/rag.go`
- [x] **Competitive Positioning**: Patterns comparable to Codex and Claude Code federation systems

#### Documentation
- [x] [FEDERATION_DESIGN_SUMMARY.md](FEDERATION_DESIGN_SUMMARY.md) - Complete architecture and implementation details
- [x] [FEDERATED_CONTENT_STRATEGY.md](FEDERATED_CONTENT_STRATEGY.md) - Content categories, promotion rules, examples
- [x] [CENTRAL_BRAIN_MEMORY_PROPOSAL.md](CENTRAL_BRAIN_MEMORY_PROPOSAL.md) - Original proposal and design rationale

#### Test Results
- [x] All daemon tests passing (1.609s)
- [x] All HQ sync tests passing (0.832s)
- [x] Bootstrap token tests passing (0.324s)
- [x] 7 HQ handler tests: sync acceptance, rejection, retrieval

### Implementation Files

| File | Purpose | Lines | Status |
|------|---------|-------|--------|
| `lib/sop/ai/agent/shared_memory.go` | Core primitives | ~250 | ✅ Complete |
| `lib/sop/cmd/sop-daemon/federation_content.go` | 13 categories, 49 seeds, auto-categorization | ~1,100 | ✅ Complete |
| `lib/sop/cmd/sop-daemon/rag.go` | Intelligent promotion and retrieval | ~670 | ✅ Complete |
| `lib/sop/cmd/sop-daemon/auth.go` | Bidirectional sync (push + pull) | ~600 | ✅ Complete |
| `lib/sop/tools/sopbackend/hq_sync_handler.go` | HQ receiver endpoints | ~440 | ✅ Complete |
| `lib/sop/tools/sopbackend/hq_sync_handler_test.go` | HQ handler tests | ~140 | ✅ Complete |

### What Makes This Valuable

| Local Daemon Alone | With Enhanced Federation |
|-------------------|-----------------|
| Learns from one user | Learns from entire fleet |
| Rediscovers solutions | Applies proven patterns immediately |
| No safety baselines | **49 critical patterns** seeded on startup |
| Generic error handling | **13 categories** of context-aware patterns |
| Trial-and-error | Pattern-guided workflows |
| No security/performance guidance | Security best practices, performance patterns built-in |
| Limited knowledge | Fleet-wide collective intelligence |
| **Competitive with**: Basic code completion | **Competitive with**: Codex, Claude Code federation systems |

### V2 Future Enhancements
- [ ] Observability metrics for sync success/failure rates
- [ ] Daemon-side retry logic with exponential backoff
- [ ] LTM garbage collection for stale shared artifacts
- [ ] Recurrence-based confidence boosting
- [ ] Policy layer for artifact quality filtering
- [ ] Admin UI for viewing fleet-wide patterns
- [ ] Pattern effectiveness tracking and ranking

---

## Phase 1: Auth handoff and continuity

These are the highest-priority tasks for the next implementation pass.

- [ ] Make site login issue a shared sopbackend session token and set the standard access cookie/header contract.
- [ ] Make the new site use the same bearer-token semantics as the existing httpserver experience so the user session is compatible across both surfaces.
- [ ] Ensure the auth context carries the same subscription plan and subscription feature list to httpserver after a site login or redirect.
- [ ] Add a redirect path from the site flow into the httpserver UI while preserving the authenticated session identity.
- [ ] Add a token-forwarding bridge so httpserver can continue the session without a second login.
- [ ] Add a host-side daemon bootstrap path that reuses the same authenticated identity and token material for local-agent communication.

## Phase 2: Onboarding flow
- [ ] Add a post-payment download/install step after the subscription confirmation.
- [ ] Link the install step to the SOP GitHub Releases page for the release binary.
- [ ] Add copy explaining what the local agent does and what happens next.

## Phase 3: Returning-user experience
- [ ] Ensure the login flow remains the entry point for returning users.
- [ ] Show a personalized landing page after authentication with plan and subscription state.
- [ ] Keep the account page available after sign-in for recurring account management.

## Phase 4: Subscription and account management
- [x] Add an unsubscribe action from the landing page.
- [x] Persist the unsubscribed state in the user record and reflect it in the UI.
- [x] Add an unregister action that removes the account and revokes sessions.
- [x] Redirect the user to a signed-out experience after unregistering.
- [x] Add a second plan entry for the future HQ + Local Agent experience.
- [x] Mark the second plan as unavailable for regular users while keeping it visible for admin testing.

## Phase 5: Architecture and packaging
- [x] Create a dedicated React frontend package for the new site/app.
- [x] Serve the React site/app from the standalone site backend on its own routes without wiring it into the daemon or httpserver.
- [x] Keep sopbackend as the shared runtime/business-logic layer used by the SOP Data Manager apps and the standalone site backend.
- [x] Preserve the existing Data Manager experience as a separate UI surface served by the SOP Data Manager apps (daemon/httpserver).

## Phase 6: Polish and validation
- [ ] Add UI tests for the new download/install step and account actions.
- [ ] Add backend tests for subscription-state updates and unregister behavior.
- [ ] Add end-to-end tests covering site login, token reuse, httpserver access, and daemon bootstrap.
- [ ] Verify the full UI and auth flow end to end.

## Phase 7: Monitorability and operations
- [ ] Standardize the shared monitorability API contract across the new site backend, httpserver, and daemon.
- [ ] Expose /healthz, /readyz, and /metrics from each instance using the shared backend contract.
- [ ] Ensure the daemon and httpserver expose the same MA shape so Prometheus can scrape them consistently.
- [ ] Add site-specific monitoring metrics for onboarding, auth, and subscription flows.
- [ ] Document the Prometheus/Grafana integration path for operators and SAs.

## Phase 8: Space expert / digest-to-Space feature
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
