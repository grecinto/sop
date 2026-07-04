# New Site Implementation Plan

## Purpose

This document is the implementation reference for the next pass of the new site experience. It captures the architecture, the integration boundary with sopbackend, the auth handoff path into httpserver, and the local daemon bootstrap path so work can pause and resume without losing context.

## Goals

- Allow users to register, subscribe, and complete onboarding on the new site.
- Reuse the shared sopbackend auth/session/token model instead of introducing a separate identity stack.
- Let the user continue into the httpserver UI without a second login.
- Preserve the same authenticated identity, role, subscription plan, and feature list when the user reaches the local daemon experience.
- Position v1 around a terminal-first, floating-window copilot experience rather than a full IDE integration.
- Make AST/tree-sitter-aware editing a core differentiator for precise code changes, refactors, and workflow automation.
- Ground the copilot in knowledge bases and repository context so it behaves like an expert rather than a generic autocomplete layer.
- Support BYO API keys and local-hosted persistence/visualization as the initial privacy and trust story.
- Make the v1 value proposition explicit: for $5/month, users get a precise coding copilot that is grounded in their own repository and knowledge base, not just a generic chat surface.
- Treat the initial product story as a focused utility layer for real coding work: refactors, code reviews, workflow automation, and domain-specific guidance.

## Scope

### In scope

- Site signup and sign-in flows.
- Subscription and payment state handling.
- Shared session issuance and token compatibility with sopbackend.
- Redirect handoff into the httpserver UI.
- Local daemon bootstrap using the same authenticated identity.
- Documentation, test coverage, and pause/resume notes.

### Out of scope for the next pass

- Full production billing provider integration.
- Advanced host automation beyond a simple token-based bootstrap path.
- Major UI redesign beyond the current onboarding and account experience.
- Full IDE integration or IntelliSense-style parity with VS Code/Cursor.
- A broad desktop-agent experience that attempts to replace the entire editor workflow in v1.

## Current implementation baseline

The current codebase already has the major structure in place:

- The standalone site backend serves the new product-site frontend from src/backend/site/main.go.
- Site-specific account and subscription state live in src/backend/siteaccount/account.go.
- Site endpoints live in src/backend/siteaccount/handlers.go.
- Shared auth and session logic remains in lib/sop/tools/sopbackend/auth.go and lib/sop/tools/sopbackend/scripts.go.
- Shared auth middleware already populates the auth context used by httpserver and other downstream paths.

## Product direction for v1

The next implementation pass should treat the copilot as a terminal-first and floating-window experience, not as a full IDE replacement. The edge is not IntelliSense or editor-native autocomplete; the edge is fast, context-aware assistance for real coding workflows.

### v1 product posture

- The primary experience is a lightweight command-line entrypoint plus a floating window UI.
- The copilot should be able to reason over repository context, knowledge bases, and task intent.
- AST/tree-sitter-aware editing should be a first-class capability for safe refactors, targeted edits, and structured transformations.
- Users should be able to bring their own API keys and keep the main trust story centered on local-hosted persistence and visualization rather than server-side chat history storage.
- The product should be positioned as a more precise, expert-guided coding assistant than a generic coding chatbot, not as a direct clone of Cursor or Codex.

### Acceleration strategy

To catch up quickly without reinventing the full stack, the first implementation should reuse mature open-source building blocks:

- Agent orchestration: LangGraph, OpenAI Agents SDK, or AutoGen.
- Code parsing and transformation: tree-sitter and language-specific grammars.
- Retrieval over repo and knowledge context: FAISS, pgvector, Qdrant, or LanceDB.
- Evaluation: SWE-bench, LiveCodeBench, or repo-specific task suites.

This keeps v1 focused on the differentiators that matter most: precise code awareness, grounded expert workflows, and a non-IDE interaction model. That combination is the core $5/month value proposition: useful enough for daily coding work, but narrow enough to be consistently high-signal and practical.

## Architecture contract

### 1. Site auth remains shared

The new site should not introduce a parallel user model. It should reuse the same:

- user identity model,
- session token model,
- bearer-token contract,
- subscription plan model,
- feature-list model,
- auth-context population logic.

### 2. Site login issues shared session material

Successful site registration or sign-in should create or reuse a sopbackend-compatible session. The response should expose the same session materials that the existing backend expects:

- access token,
- refresh token if applicable,
- auth cookie/header contract,
- user identity and role.

### 3. httpserver receives the same session context

After the site flow completes onboarding or sign-in, the user should be redirected into the httpserver UI with the same session identity intact. The handoff can use one of the following patterns:

- same access token passed through a redirect query or fragment,
- same access token persisted in browser storage and consumed by httpserver,
- same access token set via a short-lived bootstrap handshake on the server side.

The first implementation should prefer the simplest reliable path that preserves the existing sopbackend middleware contract.

### 4. Local daemon inherits the same identity

The local daemon should behave as a delegated host-side client. It should receive the authenticated identity and token material from the UI layer over localhost and continue using that identity without a second login.

### 5. MFA and session policy should stay practical

The first implementation should support MFA without making routine use feel burdensome. A balanced policy is:

- Password sign-in remains the default entry point.
- MFA should be optional on initial login for trusted users, and can be required for new devices, suspicious activity, or high-risk actions.
- Refresh tokens should expire on a reasonable cadence such as weekly or monthly, forcing a fresh sign-in when they are no longer valid.
- Trusted devices may remain signed in for a limited window, but the app should require re-authentication every 60–90 days or for sensitive actions.
- MFA should not be required on every single token refresh; that is too disruptive for everyday use.

This keeps the security model strong enough for account protection while preserving a smooth user experience.

## Implementation phases

### Phase 0 — agent and workflow foundation

- [ ] Add a terminal-first entrypoint and a floating-window interaction surface for coding tasks.
- [ ] Add AST/tree-sitter-aware edit planning and validation for common refactor and transformation workflows.
- [ ] Add repository and knowledge-base retrieval so the assistant can ground responses in the right context.
- [ ] Add provider abstraction for BYO API keys and the initial LLM routing layer.
- [ ] Add local-host persistence and visualization primitives for workflow history and generated artifacts.
- [ ] Add an evaluation harness with repo-based task suites and regression cases.
- [ ] Define and track a small set of high-value v1 workflows that justify the subscription, such as targeted refactors, code review assistance, repository-aware Q&A, and knowledge-base-driven task automation.

### Phase A — shared auth compatibility

- [ ] Ensure site registration and sign-in can create or reuse a sopbackend-compatible session.
- [ ] Make the site API return the expected access-token and cookie contract.
- [ ] Ensure the auth context includes role, subscription plan, and feature list.

### Phase B — httpserver handoff

- [ ] Add a redirect or bootstrap path from the site to httpserver that carries the authenticated session.
- [ ] Ensure httpserver accepts the session without forcing an additional sign-in.
- [ ] Preserve the same entitlement and feature context during the handoff.

### Phase C — local daemon bootstrap

- [ ] Add a lightweight local bootstrap path for the daemon to receive the token material.
- [ ] Use the same identity and entitlement state for the daemon-side runtime.
- [ ] Keep the daemon bootstrap path local-only and explicit.

### Phase D — validation and regression safety

- [ ] Add tests for site registration, login, and token issuance.
- [ ] Add tests for httpserver continuity after the handoff.
- [ ] Add tests for local daemon bootstrap reuse.
- [ ] Run the full SOP test suite and the site/backend test suite.

## Suggested implementation order

1. Extend the site auth endpoints so they issue shared sopbackend session material.
2. Add the handoff path into httpserver using the same bearer-token model.
3. Add the daemon bootstrap bridge with the same token context.
4. Add regression tests and document the final flow.

## Pause/resume checklist

When work pauses, the next engineer should be able to resume from this checklist:

- Confirm the site login/register path can issue a shared access token.
- Confirm the token is accepted by the existing sopbackend auth middleware.
- Confirm httpserver can continue the same session after the redirect.
- Confirm the local daemon can bootstrap with the same token identity.
- Confirm the tests covering all three steps are present and passing.

## Acceptance criteria

The feature is complete when all of the following are true:

- A user can register or sign in on the new site.
- The user receives a sopbackend-compatible access token.
- The user can continue into the httpserver UI without a second login.
- The local daemon can start from the same authenticated identity and entitlement context.
- The implementation is tested and documented for future continuation.
