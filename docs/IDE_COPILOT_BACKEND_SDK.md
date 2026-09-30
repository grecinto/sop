# IDE Copilot Backend Contract

**Status:** Current shared interface. The daemon selects Cline SDK as its default coding backend. Continue remains reference/compatibility code; this interface is intentionally backend-neutral.

## Ownership

For the standard Data Manager Chat path, SOP Ask is the outer controller. It classifies the user request and may delegate one bounded coding task to an `ideworker.Backend`. The backend runs its own bounded execution and returns a structured result. It does not re-enter or replace the Ask controller.

```text
Chat Send -> /api/ask -> SOP Ask/ReAct -> ideworker capability -> backend -> structured result -> SOP history/undo
```

The production UI has one Send action. Backend-specific routes are diagnostic or integration surfaces, not parallel production chat controllers. Product-specific Cline behavior, runtime setup, MCP tools, and manual validation are documented in [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md).

The Ask-visible tool currently retains the legacy name `delegate_continue_task` for compatibility. It delegates through the shared interface and does not imply Continue is the selected backend.

## Go Interface

The contract is defined in [`capability.go`](../lib/sop/ai/agent/domains/ideworker/capability.go):

```go
type Backend interface {
    InspectSession(ctx context.Context, sessionID string) (SessionState, error)
    ExecuteTask(ctx context.Context, req TaskRequest) (string, error)
    StartTask(ctx context.Context, req TaskRequest) (DelegationHandle, error)
    GetTaskStatus(ctx context.Context, delegationID string) (DelegationStatus, error)
    CancelTask(ctx context.Context, delegationID string) (DelegationStatus, error)
}
```

`SessionState` reports connectivity, backend/IDE identity, workspace context, and supported capabilities. `TaskRequest` carries explicit correlation, task, provider/model, workspace, allowed-tool, and guidance fields. Application state belongs in these request/state fields, not in `context.Context`; context is for cancellation and request-scoped infrastructure.

Credentials are sensitive. A backend must not log keys or expose them in command text, process arguments, status messages, diffs, or user-facing activity. Preserve cancellation from `ctx` through the backend's execution process.

## Result Contract

Implementations return JSON text matching [`ExecutionResult`](../lib/sop/ai/agent/domains/ideworker/result.go), either directly or as the terminal result event of an NDJSON stream:

```go
type ExecutionResult struct {
    Status        string         `json:"status,omitempty"`
    Summary       string         `json:"summary,omitempty"`
    ModifiedFiles []ModifiedFile `json:"modified_files,omitempty"`
    Artifacts     []Artifact     `json:"artifacts,omitempty"`
    Metadata      map[string]any `json:"metadata,omitempty"`
}
```

Include created, updated, and deleted files where available. `ModifiedFile` carries a path, CRUD operation, diff summary/delta/format, optional artifact reference, and metadata. `DefaultResultContract()` sets a 32 KiB inline diff limit and prefers artifact references for larger results. `ParseExecutionResult()` is the shared parser.

Structured results let SOP record file backups, undo transactions, validation, and execution memory without backend-specific result parsing. Returning only free-form prose loses file-level history and undo data.

## Capability Advertising and Routing

Use the shared capability identifiers in `result.go`:

- `read_context`
- `apply_patch`
- `edit_code`
- `diff_review`
- `plan_changes`

Advertise only behavior the backend can complete end to end. In particular, `apply_patch` is a mutation capability and may activate the Ask-side mandatory-delegation gate for a connected worker. Do not advertise it when editing or result capture is incomplete.

Backend availability controls whether delegation is exposed. With no usable backend, Ask retains its normal SOP tools. Backend selection and session capability checks are daemon concerns; adapters should not independently orchestrate multiple Ask turns.

## Lifecycle

- `InspectSession` reports whether a worker/session is available and what it can do.
- `ExecuteTask` runs a bounded synchronous task.
- `StartTask`, `GetTaskStatus`, and `CancelTask` support asynchronous work.
- Status should report bounded progress and terminal state. The shared `DelegationStatus` includes legacy progress/message fields and structured phase/activity metadata.
- Cancellation must stop underlying work, not merely discard the response.
- A restored workspace may invalidate the backend's active task/session context; report or refresh that state rather than continuing from stale assumptions.

## Adding a Backend

1. Implement `ideworker.Backend` in the owning daemon integration package.
2. Inject/register it through the existing capability/runtime wiring.
3. Validate capability reporting against actual edit/result behavior.
4. Return a parseable `ExecutionResult` and propagate cancellation.
5. Add focused tests for session inspection, task execution, status, cancellation, workspace validation, and result parsing.
6. Verify the normal `/api/ask` path decides delegation and that non-coding prompts remain on the SOP Ask path.

Do not add backend-specific UI buttons or make the backend the outer controller. Do not copy backend chat history into SOP; return the bounded task result and structured changes needed for SOP-owned history and undo.

## Current Implementations

- **Cline SDK:** default daemon backend, hosted by the Node/TypeScript package in `tools/cline-sdk-host`. Details and remaining end-to-end validation: [Cline SDK integration](CLINE_COPILOT_INTEGRATION_PLAN.md).
- **Continue:** non-shipping reference/compatibility backend. Its older wire and extension notes are indexed under [Continue references](INDEX.md#cline-and-continue); those documents are not the default product contract.
