# Local RAG as a Lightweight App Experience

## Overview

The SOP experience can be framed as a simple app model:

- HQ is the app store entry point.
- Users sign in there, discover and download the local RAG experience, and install it locally.
- The local AI chat window becomes the app surface they use every day.
- The app is secure, authenticated, and rate-limited through HQ.
- It is powered by the portable SOP database platform and the shared local runtime.

## Product Mental Model

Instead of treating this as a complex desktop application ecosystem, the product can be presented as a very simple workflow:

1. User signs in to HQ.
2. User downloads or enables the local RAG experience.
3. The local desktop experience starts.
4. The AI chat window becomes the daily operating surface.
5. The user can interact with the system safely through chat, with approvals and policy checks enforced from HQ.

This is intentionally lightweight.

## Why This Works

The experience is simple because:

- it uses the same authenticated session model as the UI,
- the local runtime is a delegated extension of the signed-in user,
- the SOP database is portable and can run locally as a library-backed runtime,
- and HQ only needs to handle the minimal coordination layer: authentication, policy, rate limits, and enforcement.

## The "App Store" Concept

HQ acts as the app store in a very minimal form:

- it is where users sign up or sign in,
- it is where they discover the local RAG experience,
- it is where they receive the authenticated tokens needed to bootstrap the local runtime,
- and it is where policy and rate limiting are enforced.

There is no need for a complex app-store management system.

The workflow is simply:

- sign in,
- download/enable local RAG,
- run the local experience,
- use the AI chat window as the app surface,
- remain authenticated and rate-limited through HQ.

## Relationship to the Floating AI Chat Window

The floating AI chat window is the user-facing app shell.

It should feel like:

- the user opens their AI app,
- it stays available as a lightweight overlay,
- it can be pinned to stay on top,
- and it can be dismissed or restored without changing the underlying workflow.

This makes the experience feel like a personal AI desktop assistant rather than a browser-only chat panel.

## Security Model

The design remains simple and secure:

- UI login is the primary authentication entry point.
- The local runtime inherits the authenticated identity from the UI.
- HQ validates the delegated token and enforces usage limits.
- The local runtime can still work autonomously for local actions, but it remains governed by HQ policy.

## Core Message

The product story is:

- SOP is a portable database platform,
- HQ is the simple app store and authentication control plane,
- the local AI chat window is the app experience,
- and the user can safely use it every day as an authenticated, rate-limited, policy-enforced copilot.
