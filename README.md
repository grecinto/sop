# SOP Cloud

SOP Cloud is the product and deployment layer around the SOP platform. The repository contains the site experience, the shared AI Studio/Data Manager runtime, and the local daemon used for private workspace operations.

## Product Shape

- The React site and Go site backend provide public pages, account management, subscriptions, and onboarding.
- The Data Manager is the shared AI Studio interface served by `httpserver` or the daemon; it is not the React site.
- The daemon provides local APIs, workspace access, terminal workers, and coding delegation.
- Normal AI Chat sends through `/api/ask`. SOP Ask/ReAct owns classification and orchestration. For suitable coding work, the `ideworker` capability delegates a bounded task to the configured Cline backend.
- The Cline SDK host is the default Cline execution path. The daemon also retains an explicit CLI fallback. Continue is reference/compatibility code, not a shipping dependency.

The user-facing chat has one Send action. Cline is an execution backend behind SOP Ask, not a second chat controller or a separate production composer button. See [Cline integration](docs/CLINE_COPILOT_INTEGRATION_PLAN.md) and the [backend SDK contract](docs/IDE_COPILOT_BACKEND_SDK.md).

## Repository Map

- `src/backend/site` and `src/backend/siteapi`: site hosting and product-facing HTTP routes.
- `src/backend/siteaccount`: site account and billing records.
- `src/frontend`: React site frontend.
- `lib/sop`: shared platform, Data Manager, daemon, AI runtime, memory, and storage code.
- `tools/cline-sdk-host`: Node/TypeScript host for the Cline SDK.
- `docs`: architecture, operations, feature guides, and implementation history. Start at the [documentation index](docs/INDEX.md).

This repository uses a Go workspace. See [Development Guide](docs/DEVELOPMENT_GUIDE.md) for setup, runtime boundaries, and contributor workflow.

## Build and Test

Run the Go workspace tests from the repository root:

```sh
go test ./...
go test ./lib/sop/...
```

Build the Cline SDK host with Node.js 22 or newer:

```sh
cd tools/cline-sdk-host
npm ci
npm run build
```

The host runtime packaging check is available at `tools/cline-sdk-host/package-runtime.sh`. It verifies the Node version, installs the locked dependencies, builds the host, and creates a deployable host directory.

For site startup, daemon setup, and local integration details, use [Development Guide](docs/DEVELOPMENT_GUIDE.md), [AI Studio](docs/AI_STUDIO.md), and [Cline integration](docs/CLINE_COPILOT_INTEGRATION_PLAN.md).

## Architecture References

- [Ask/ReAct architecture](docs/ASK_V2_ARCHITECTURE.md): controller ownership, provider loops, cognition, retry, and phase state.
- [Workflow architecture](docs/WORKFLOWS.md): participant pattern and multi-turn work.
- [Cline integration](docs/CLINE_COPILOT_INTEGRATION_PLAN.md): shipped SDK host, MCP tools, task lifecycle, and remaining production validation.
- [IDE worker SDK](docs/IDE_COPILOT_BACKEND_SDK.md): backend-neutral delegation contract.
- [Authentication](docs/AUTHENTICATION.md): shared auth and session model.
- [Spaces](docs/SPACES.md): knowledge, documents, and expertise sources.

## Boundaries

- SOP Ask is the only outer controller for the standard AI Studio chat flow; capabilities are participants.
- SOP owns durable memory and the canonical undo ledger. Cline checkpoints are task-local recovery aids.
- Site/account behavior belongs in `src`; reusable platform behavior belongs in `lib/sop`.
- Provider prompts may leave the machine when a remote model provider is configured. Local execution does not imply that model traffic is local.
