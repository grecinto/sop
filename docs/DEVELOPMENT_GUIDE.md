# Development Guide

This guide describes the current repository layout and the quickest checks for changes in each runtime.

## Repository Layout

- `src/backend/site` and `src/backend/siteapi`: standalone site host and product-facing routes.
- `src/backend/siteaccount`: site account and billing data/services.
- `src/frontend`: React site frontend; it is not the Data Manager UI.
- `lib/sop`: shared SOP runtime, Data Manager templates, HTTP server, daemon, AI/Ask pipeline, memory, and storage.
- `tools/cline-sdk-host`: Node/TypeScript bridge to the Cline SDK.
- `docs`: current architecture/operations guides and dated implementation references.

The repository is a Go workspace. Keep product/site behavior in `src`; put behavior in `lib/sop` only when it is reusable by the shared runtime.

## Runtime Boundaries

- The React site and site backend handle public product pages, account flows, subscriptions, and onboarding.
- The Data Manager is served by the shared `httpserver` or daemon using `sopbackend` templates and handlers.
- The daemon provides local workspace, API, and PTY worker capabilities.
- Normal Data Manager Chat goes through `/api/ask`. SOP Ask/ReAct owns classification and orchestration; the configured Cline SDK backend executes bounded coding work when Ask delegates.
- Continue remains reference/compatibility code, not a shipping dependency.

See [AI Studio](AI_STUDIO.md), [Ask architecture](ASK_V2_ARCHITECTURE.md), [Cline integration](CLINE_COPILOT_INTEGRATION_PLAN.md), and the [IDE backend contract](IDE_COPILOT_BACKEND_SDK.md).

## Authentication and State

The shared `sopbackend` layer owns token/session validation and auth context. `siteaccount` owns product account and billing data and coordinates site-facing account flows. Do not add parallel token validation or assume billing fields live in the SOP auth user record.

Prefer explicit request structs and function parameters for domain state. Use `context.Context` for cancellation, deadlines, and request-scoped infrastructure values, not as an application state bag. Preserve caller contexts through daemon and worker code.

See [AUTHENTICATION.md](AUTHENTICATION.md), [ENTITLEMENT_ARCHITECTURE.md](ENTITLEMENT_ARCHITECTURE.md), and [PAYMENTS.md](PAYMENTS.md).

## Build and Test

Run from the repository root:

```sh
go test ./...
go test ./lib/sop/...
```

The Cline SDK host requires Node.js 22 or newer:

```sh
cd tools/cline-sdk-host
npm ci
npm run build
```

The site package can be tested independently with `go test ./src/backend/site ./src/backend/siteapi`. For daemon behavior, run `go test ./lib/sop/cmd/sop-daemon`.

## Site Development

The site Go backend is launched from the repository root with:

```sh
go run ./src/backend/site
```

Build the React frontend from `src/frontend` using its package scripts. The site frontend, shared Data Manager, and local daemon are separate surfaces; test the route/runtime you changed rather than assuming one serves the others.

## Working Conventions

- Ask/ReAct is the standard outer controller; capabilities are participants. See [WORKFLOWS.md](WORKFLOWS.md).
- Keep request/session state explicit and ensure cancellation reaches subprocesses.
- Update the canonical feature guide when a runtime contract changes. Use dated implementation records for historical evidence, not as current setup instructions.
- Prefer existing shared APIs and domain boundaries over duplicating behavior in a host-specific package.

For the complete map, start at the [documentation index](INDEX.md).
