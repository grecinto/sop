# IDE Copilot Backends

**Status:** Current architecture

The Data Manager chat has one Send action. The request goes through SOP Ask/ReAct, which owns classification and the outer workflow. For a coding request, Ask may delegate one bounded task to the daemon's configured backend. Cline SDK is the default backend; Continue remains compatibility/reference code.

```text
Data Manager Send
    -> /api/ask
    -> SOP Ask/ReAct
    -> optional bounded backend task
    -> structured result and SOP-owned history/undo
```

## Responsibility Boundary

SOP owns the outer Ask turn, capability routing, session continuity, durable memory, validation history, and canonical undo records. The delegated backend performs the bounded execution and returns a structured result. It does not become a second chat controller.

The default Cline backend runs through a daemon-managed TypeScript SDK host. The daemon supplies the workspace and task context, captures before/after file changes, and records the result in SOP history. Cline's task-local checkpoints remain separate from SOP's searchable undo records.

Model-provider traffic may leave the machine when a remote provider is configured. Local daemon execution does not mean model inference is local.

## Backend Contract

A backend reports session capabilities, accepts explicit task/workspace/provider context, and returns a structured result with status, summary, modified files, and metadata. Long-running implementations report status and must propagate cancellation to the underlying work.

SOP Ask remains the controller if another system embeds SOP as a participant. See [Ask Participant Mode](./ask-participant-mode.md) for that transport boundary.

## Compatibility References

- [Continue Facade Architecture](./continue-facade-architecture.md) is a historical Continue compatibility page, not the current setup guide.
- Maintainers can consult the repository's `docs/CLINE_COPILOT_INTEGRATION_PLAN.md` for daemon routes, MCP tools, runtime requirements, and release-validation gaps.
