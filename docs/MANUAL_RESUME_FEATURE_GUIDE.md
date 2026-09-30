# Manual Resume for Workstreams

**Status:** Implemented. Manual resume controls whether a paused workstream continues automatically or waits for a user action. Workstream continuity is persisted and startup rehydration is documented in [STARTUP_REHYDRATION_IMPLEMENTATION.md](STARTUP_REHYDRATION_IMPLEMENTATION.md).

## Behavior

A paused workstream represents an unfinished objective that should retain its state and context. Manual-resume state prevents an unrelated completion or queue event from silently advancing that work. The user can return to the workstream and explicitly continue it.

The Ask pipeline remains the controller: workstream tools update state and enqueue bounded future work, while each continuation is processed through the normal Ask turn. Capabilities do not run a hidden loop over the paused plan.

## Persistence

Workstream persistence and startup restoration are implemented; do not rely on the older claim that all paused work is lost on process restart. The exact serialized payload and migration behavior are owned by the runtime implementation and may evolve. See [Startup Rehydration](STARTUP_REHYDRATION_IMPLEMENTATION.md) and [Workflows](WORKFLOWS.md).

## Validation

From the repository root, run the focused agent and startup-rehydration tests:

```sh
go test ./lib/sop/ai/agent/...
```

For the broader Ask architecture and state handoff, see [ASK_V2_ARCHITECTURE.md](ASK_V2_ARCHITECTURE.md). The former planning/status links are consolidated into that guide and the startup rehydration record.
