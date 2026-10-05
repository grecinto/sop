# Continue Compatibility Reference

**Status:** Non-shipping compatibility/reference implementation. Cline SDK is the default coding backend. The Continue code and protocol remain in the repository for compatibility and as a source of integration lessons; they are not the production copilot path.

## Current Boundary

The standard product flow is Data Manager Chat -> `/api/ask` -> SOP Ask/ReAct -> the `ideworker` capability -> configured backend. The backend-neutral contract is documented in [IDE_COPILOT_BACKEND_SDK.md](IDE_COPILOT_BACKEND_SDK.md); the current Cline implementation and release checks are in [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md).

Continue's `/api/continue/*` endpoints and worker adapter are retained compatibility surfaces. They do not replace SOP Ask as the product controller, and no separate Continue action is exposed in the production chat composer.

The daemon mounts these compatibility routes only when `workflow_backend=continue` is explicitly selected. They are not available in the default Cline mode.

For a reusable implementation walkthrough that compares the retained Continue callback pattern with the current Cline SDK host, see [IDE_COPILOT_INTEGRATION_GUIDE.md](IDE_COPILOT_INTEGRATION_GUIDE.md). Use this page for Continue's status and compatibility boundary.

## Retained Integration Lessons

- Keep the outer Ask/ReAct controller in SOP and delegate only bounded work to a backend.
- Keep provider/model intent explicit; do not disguise the daemon as an OpenAI-compatible endpoint.
- Keep session/workstream identity distinct from per-request correlation identity.
- Advertise mutation capability only when the worker can edit, report changed files, and return a result SOP can record and undo.
- Keep IDE-specific auth/session handling inside the IDE client. Never treat local-daemon credentials and HQ credentials as interchangeable.
- Return structured file changes and validation rather than only chat prose.

These are backend-neutral lessons and remain relevant to future adapters. They do not make Continue the selected implementation.

## Detailed Compatibility Documents

- [Continue transport/API specification](CONTINUE_BRIDGE_API_SPEC.md): legacy endpoint and wire details. Verify every route against `lib/sop/cmd/sop-daemon/client_bridge.go` before relying on it.
- [Continue client implementation notes](IDE_COPILOT_CLIENT_INTEGRATION.md): historical extension-side auth, model catalog, and worker callback details. The material is not a release or resume plan.
- [IDE backend contract](IDE_COPILOT_BACKEND_SDK.md): current shared Go interface and result shape.

Older overlapping Continue plans and architecture notes were removed after their useful material was consolidated here. Use this page and the two detailed references above; the retired filenames are no longer maintained entry points.
