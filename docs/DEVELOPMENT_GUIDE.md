# Development Guide

This document is the short orientation guide for future developers and maintainers of this repository.

## Repository shape

This repo is a private overlay on top of the shared SOP implementation.

- [lib/sop](../lib/sop): the mature SOP platform code. This includes the existing AI Studio, shared runtime, and daemon-oriented capabilities.
- [src](../src): the product-facing sop-cloud application layer. Use this tree for site-specific UX, registration flows, onboarding, subscriptions, and account management.
- [docs](.) and the repository root: product stories, implementation notes, and repository-level guidance.

## Two runtime tracks

There are two distinct experiences in this repository:

- The platform is intended to behave as a first-class microservice surface. Developers should think in terms of capabilities and APIs first, and storage backend choice second.
- The mature HTTP server and daemon should expose stable functionality over HTTP so other services and third-party integrations can consume the same capabilities without depending on one storage engine.

1. The mature platform experience
   - Lives in [lib/sop](../lib/sop)
   - Includes the original AI Studio and the shared runtime/daemon workflow
   - Should remain the stable platform layer for core runtime behavior

2. The new sop-cloud product experience
   - Lives in [src](../src)
   - Includes the new site backend and planned frontend experience
   - Holds self-registration, onboarding, account management, and subscription logic
   - Treat register, unregister, and subscription updates as site/product-facing flows rather than core AI Studio or daemon endpoints

## Maintenance rules

Keep the boundary clear:

- Put shared runtime behavior in [lib/sop](../lib/sop)
- Put product-specific behavior in [src](../src)
- Avoid mixing the new site experience into the mature platform code unless the change is genuinely reusable by the wider runtime
- When adding a feature, describe it in terms of the user-facing or integration-facing capability it provides, not in terms of which storage engine happens to back it today
- If the topic is SOP DB internals, keep that discussion in the relevant storage/runtime package documentation rather than in the product-facing layer
- Swagger/OpenAPI docs are available through the shared backend wiring for both the HTTP server and daemon at /swagger.json and /swagger/.
- Observability for the product site, shared backend, onboarding flow, auth, and subscriptions is tracked in [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md).
- Shared monitorability endpoints should follow the Prometheus-compatible contract described in [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md): /healthz for health and /metrics for scrapeable metrics.
- For the new site and shared backend, do not use context.Context as a general-purpose state bucket for request values or domain state. Prefer explicit parameters or explicit request structs in handlers, service methods, and API boundaries.
- LTM lifecycle maintenance should follow the documented policy: use a nightly wakeup servicer for cleanup and retention decisions, run a weekly compaction pass for archive/merge/prune work, and keep active-usage bookkeeping lightweight by updating recency metadata inline during retrieval and write flows.
- The current retention behavior is conservative: a memory item stays active while it is recent and relevant, becomes archived once it is deemed stale, and is removed from the store once the weekly compaction pass observes it has been archived long enough.
- In the Data Manager, lifecycle state is now surfaced directly on space-item cards so archived or stale entries are visibly labeled and can be distinguished from active content without opening the item details.

## Space and Store Naming

**Important**: Space and Store names are automatically normalized to ensure backend compatibility:

- **User input**: `"My Space"` → **Stored as**: `"my_space"` → **Displayed as**: `"My Space"`
- Spaces converted to underscores, lowercased
- Only `[a-zA-Z0-9_-]` characters allowed
- Display names auto-generated from stored names (underscores → spaces, title case)

**When writing code:**
- Always reference Spaces/Stores using their normalized (stored) names: `"core_playbook"`, `"shared_memory"`
- Use `resolveSpaceDisplayName()` and `resolveStoreDisplayName()` for UI display only
- Normalization happens at API boundaries: `handleCreateSpace` and `handleAddStore`
- See [SPACES.md](SPACES.md) for detailed examples and rationale

**Why**: Ensures compatibility with all storage backends (Cassandra has strict table naming requirements, filesystems need safe paths, URLs need safe characters).

## Simple store migration

For operational moves, the simplest supported story is a file-folder migration:

1. Stop the app.
2. Backup the relevant store directories and configuration files.
3. Copy the store folders to the target environment, keeping the same layout and paths.
4. Start the app against the copied data and verify startup, authentication, session handling, and basic store access.

This is a good default because SOP data is stored in filesystem-backed stores and the migration is effectively a copy of those directories. It is straightforward, low-risk, and easy to rollback if needed.

## Authentication Architecture

**CRITICAL:** This codebase uses a **unified authentication system**. Do not create parallel auth logic.

### Single Source of Truth

All authentication is handled by the shared `sopbackend` layer:

- **Token creation:** `sopbackend.CreateSession()`
- **Token validation:** `sopbackend.AuthenticateBearerToken()`
- **Session cookies:** `sop_access_token`, `sop_refresh_token`
- **Auth context:** `sop.ContextWithAuth()` and `sop.GetAuthFromContext()`

### User Store Override Pattern

The site backend installs a user store override so sopbackend validates against the site's persistent user store instead of config.json:

```go
// src/backend/site/main.go (registerSiteHandlers)
siteaccount.InstallSopbackendUserStoreOverride()
sopbackend.RegisterAllHandlersWithoutRoot(mux)
```

After this override is installed:
- `/api/auth/login` uses site users
- `sopbackend.AuthenticateBearerToken()` validates against site users
- `sopbackend.CreateSession()` issues tokens for site users
- All RequireAuth middleware uses site users

## Provisioning Direction

First-boot provisioning is now split by host semantics.

The intended direction is:

- HQ continues to support the guided Setup Wizard flow
- daemon gets its own provisioning implementation under `cmd/sop-daemon`
- daemon provisioning should remain visually consistent with the Setup Wizard, but must not reuse HQ/httpserver provisioning handlers
- daemon provisioning supports both `Simple` setup and `Advanced` setup
- daemon `Simple` setup should perform the current quick-provision behavior explicitly, triggered by user action rather than startup-side auto-mutation
- daemon `Advanced` setup should provision only the System DB and expose only the equivalent of Setup Wizard Page 2: storage mode, path, Redis, stores folders, erasure coding, and hash mod
- missing environment state such as a missing `config.json` should trigger a daemon-specific provisioning status path, not the HQ/shared config-save flow
- configured system DB repair during daemon startup should not silently provision missing state; provisioning should be explicit from the daemon UI

This keeps the user-facing first-run experience stylistically aligned across hosts while preserving a clean architectural boundary between HQ/httpserver provisioning and daemon provisioning.

See [SOPBACKEND_HOST_FRAMEWORK_REFACTOR_PLAN.md](SOPBACKEND_HOST_FRAMEWORK_REFACTOR_PLAN.md) for the refactor plan.

### Where Auth Logic Lives

| Component | Responsibility | Location |
|-----------|---------------|----------|
| Core auth (tokens, sessions) | `sopbackend` | `lib/sop/tools/sopbackend/auth.go` |
| User store override | `siteaccount` | `src/backend/siteaccount/sopbackend_override.go` |
| Site-specific wrappers | `siteaccount` | `src/backend/siteaccount/handlers.go` (`withSiteAuth`) |
| Subscription enforcement | `site` | `src/backend/site/main.go` (`siteAppAccessHandler`) |

### DO NOT

❌ Create duplicate login endpoints  
❌ Implement parallel token validation  
❌ Build custom session management  
❌ Bypass `sopbackend.AuthenticateBearerToken()`  
❌ Create separate auth context population  

### DO

✅ Use `sopbackend.CreateSession()` for new tokens  
✅ Use `sopbackend.AuthenticateBearerToken()` for validation  
✅ Delegate to `sopbackend` handlers where possible  
✅ Add site-specific logic AFTER auth validation, not instead of it  
✅ Read inline comments in `src/backend/site/main.go` and `src/backend/siteaccount/handlers.go`  

### Reference Documentation

See [AUTH_QUICK_REFERENCE.md](AUTH_QUICK_REFERENCE.md) for detailed architecture diagrams and API endpoints.

## Design conventions

- Prefer explicit request structs and explicit parameters over context-based state passing.
- Keep handlers focused on parsing input, validating it, and coordinating calls.
- Put orchestration for the product experience in the site/backend layer, not in the shared runtime.
- Document new product features in the docs tree so future maintainers can find them quickly.
- Shape shared runtime features in `sopbackend` so new host types can reuse them with explicit host profiles rather than app-specific branching.

## Where to look first

- [README.md](../README.md): repository purpose and high-level architecture
- [src/README.md](../src/README.md): the product-layer layout and conventions
- [docs/AUTH_QUICK_REFERENCE.md](AUTH_QUICK_REFERENCE.md): unified authentication architecture (read this before touching auth code)
- [docs/NEW_SITE_STORIES.md](NEW_SITE_STORIES.md): product stories for the new site experience
- [docs/NEW_SITE_IMPLEMENTATION_TASKS.md](NEW_SITE_IMPLEMENTATION_TASKS.md): implementation task list
