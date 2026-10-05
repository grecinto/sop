# IDE Copilot Backends

**Status:** Current architecture

The AI Studio chat has one Send action. The request goes through Northstar AI Ask/ReAct, which owns classification and the outer workflow. For a coding request, Ask may delegate one bounded task to the daemon's configured backend. Cline SDK is the default backend; Continue remains compatibility/reference code.

```text
Data Manager Send
    -> /api/ask
    -> Northstar AI Ask/ReAct
    -> optional bounded backend task
    -> structured result and Northstar AI-owned history/undo
```

## Responsibility Boundary

Northstar AI owns the outer Ask turn, capability routing, cross-workstream memory, validation history, and canonical undo records. The delegated backend performs bounded execution and returns a structured result; it does not become a second chat controller. A backend may retain task-local session context for follow-up work on the same Northstar AI workstream, but that context should not become global memory shared by unrelated workstreams.

The default Cline backend runs through a daemon-managed TypeScript SDK host. The daemon supplies workspace and task context, captures before/after file changes, and records the result in Northstar AI history. The host retains tactical Cline context for follow-up tasks in the same Northstar AI workstream and isolates other workstreams and workspace/provider configurations. Daemon restart begins a fresh tactical session; a confirmed Northstar AI restore retires the current host. Cline's task-local checkpoints remain separate from Northstar AI's searchable undo records.

When `workflow_backend=cline`, Cline exclusively owns repository coding execution for that Ask turn: Northstar AI omits overlapping native coding phases and file/shell/Git mutation tools, delegates synchronously, and continues with Cline's structured result. If the Cline runtime is unavailable, Ask returns an actionable setup error instead of silently using the weaker native coding path. PostgreSQL and Splunk automation stay on their specialized Northstar AI paths.

The daemon's legacy Continue callback routes are mounted only when `workflow_backend=continue` is explicitly selected; they are not registered in Cline mode.

The daemon executor setting is mutually exclusive: `cline` (default), `continue`, or `native` for Northstar AI's in-house coding phases. The legacy `daemon`/`shell` values alias `native`. Cline and Continue modes fail with setup/worker guidance when unavailable; neither silently falls back to the native executor.

Model-provider traffic may leave the machine when a remote provider is configured. Local daemon execution does not mean model inference is local.

Joltrin's provider pool and fallback chain apply to Ask's own generation calls. Delegated Cline receives one selected provider configuration, not the pool chain; a Cline provider failure does not automatically retry with another pool entry. Pool selection, retry semantics, preferred-model behavior, and outcome telemetry are documented in the **Joltrin Provider Pool and Preferred LLM** section of `docs/CLINE_COPILOT_INTEGRATION_PLAN.md`.

The primary provider/model remains an explicit user choice when supplied. Cost optimization is manual; the daemon does not automatically pick the cheapest model or infer a “needs a smarter model” category from the coding prompt. It also does not automatically upgrade a Cline workstream's model mid-session.

## Backend Contract

A backend reports session capabilities, accepts explicit task/workspace/provider context, and returns a structured result with status, summary, modified files, and metadata. Long-running implementations report status, forward live updates as Ask NDJSON `progress` events through the event-stream context, and propagate cancellation to the underlying work.

This is an in-process Joltrin Go SDK contract, not a daemon plugin registry or wire protocol. Embedded Go applications can inject an implementation through `CopilotAgent.InjectIDEWorkerBackend`; the daemon separately selects its shipped default. MCP is currently used by Cline to call Northstar AI tools, not for Northstar AI-to-Cline task delegation.

See [Integrating a Coding Agent](./coding-agent-integrations.md) for a step-by-step walkthrough and the Cline/Continue implementation patterns.

Northstar AI Ask remains the controller if another system embeds Northstar AI as a participant through the Joltrin SDK. See [Ask Participant Mode](./ask-participant-mode.md) for that transport boundary.

## Compatibility References

- [Continue Facade Architecture](./continue-facade-architecture.md) is a historical Continue compatibility page, not the current setup guide.
- Maintainers can consult the repository's `docs/CLINE_COPILOT_INTEGRATION_PLAN.md` for daemon routes, MCP tools, runtime requirements, and release-validation gaps.
