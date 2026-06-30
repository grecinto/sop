# New Site Onboarding and Account Experience

This document captures the product stories for the new sop-cloud site experience that will guide the next implementation pass.

## Goal

Create a lightweight, self-serve onboarding experience that lets a new user:

1. register or sign in,
2. complete a starter subscription flow,
3. download the local agent binary from the SOP GitHub Releases page,
4. return later to sign in and manage their account,
5. unsubscribe or unregister when desired.

## User stories

### 1. New user activation
As a new user, I want to complete a simple starter subscription flow after signing up so that I can activate my account without friction.

Acceptance criteria:
- The landing experience clearly explains the starter trial and the post-trial pricing.
- The user can enter basic payment details and confirm the subscription.
- The system shows a clear success state after the subscription step completes.

### 2. Local agent installation
As a newly subscribed user, I want to be guided to download and install the local agent on my machine so that I can use SOP from my host environment.

Acceptance criteria:
- After payment information is captured, the user is shown a download/install step.
- The page explains what the local agent does and why it is being installed.
- The primary action links to the SOP GitHub Releases page for the release binary.
- The flow clearly communicates that the download is the natural next step after the subscription confirmation.

### 3. Returning user experience
As a returning user, I want to sign in and land on my account page so that I can see my current plan and continue using the service.

Acceptance criteria:
- The login screen is the entry point for returning users.
- After authentication, the user lands on a personalized account page.
- The page shows the current plan, subscription status, and actions available for the account.

### 4. Subscription management
As an authenticated user, I want to manage my subscription from the landing page so that I can change or cancel my plan without contacting support.

Acceptance criteria:
- The landing page includes an unsubscribe action for subscribed users.
- Unsubscribing removes the active subscription state and updates the UI accordingly.
- The user can still access the account page after unsubscribing.

### 5. Account removal
As a user, I want to unregister my account from the system so that I can remove my identity and access cleanly.

### 6. Plan preview for the richer site experience
As a visitor or authenticated user, I want to see the next richer plan in the account landing page so that I understand the future HQ and local-agent experience.

Acceptance criteria:
- The landing page lists a second plan called "HQ + Local Agent".
- The second plan is visibly marked as unavailable for regular users.
- System administrators can see the plan as available for testing and product validation.

## Product notes

- The new site should remain a thin product layer on top of the existing shared auth and user-management backend.
- The initial implementation can use the current backend user record and session model rather than introducing a parallel identity stack.
- Billing integration can be mocked or stubbed at first, but the UI should already reflect the eventual subscription and account-state model.
- The new React site/app is a separate website experience for subscriptions and self-service onboarding. It runs from the standalone site backend and is not launched from the SOP Data Manager apps.
- sopbackend remains the shared runtime/business-logic layer used by the SOP Data Manager apps and the standalone site backend when needed.
- The existing Data Manager experience stays intact as a separate operational UI surface served by the SOP Data Manager apps (daemon/httpserver).

Acceptance criteria:
- The landing page includes an unregister action.
- Unregistering removes the user from the system and revokes current sessions.
- The user is redirected to a signed-out state after the action completes.
