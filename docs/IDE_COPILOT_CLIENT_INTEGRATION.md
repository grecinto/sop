# Continue Client Integration (Reference)

**Status:** Historical compatibility notes only. This is not an active resume checklist, release plan, or shipping client specification. Cline SDK is the selected daemon coding backend; Continue remains non-shipping compatibility code.

## What This Reference Covers

The retained Continue client work connected a VS Code extension to daemon-native `/api/continue/*` routes. The detailed legacy wire contract is in [CONTINUE_BRIDGE_API_SPEC.md](CONTINUE_BRIDGE_API_SPEC.md). The generic worker interface is now documented in [IDE_COPILOT_BACKEND_SDK.md](IDE_COPILOT_BACKEND_SDK.md), and current Cline behavior is in [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md).

The old client design used these separations:

- IDE client state and daemon bearer credentials were separate from HQ credentials.
- `session_id` represented workstream identity; `request_id` represented one execution.
- Provider/model selection was sent explicitly rather than inferred from a daemon-side OpenAI facade.
- Worker registration was loopback-bound, and advertised mutation capabilities were expected to reflect tested end-to-end behavior.
- Results were expected to carry changed files and validation so the daemon could maintain Northstar AI-owned history and undo.

These are useful adapter-design lessons, not a claim that every legacy client path is currently built, tested, or supported for release.

## Verification Boundary

The prior implementation notes described Continue-specific GUI messages, token refresh, model catalog sync, worker callbacks, and diff-based completion detection. Treat those descriptions as dated investigation results. Before changing or depending on the retained extension, verify the actual source in `.continue-repo` and the matching daemon handlers. Do not use the former “Resume Here” checklist as current project state.

For repository-wide architecture and status, see [CONTINUE_REFERENCE.md](CONTINUE_REFERENCE.md). For the production direction and remaining release validation, see [Cline SDK integration](CLINE_COPILOT_INTEGRATION_PLAN.md).
