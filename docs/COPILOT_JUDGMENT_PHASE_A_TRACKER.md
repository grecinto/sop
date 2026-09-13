# Copilot Judgment Phase A Tracker

## Status
- Phase A target: instrumentation only, no behavior change
- Current status: Phase D in progress / completed in the planner runtime
- Started: 2026-09-12

## Milestones
- [x] Confirm root cause: execution can patch without a structured diagnosis artifact
- [x] Ground fix in existing planner artifact schema and prompt flow
- [x] Add failing regression test proving the diagnosis instrumentation gap
- [x] Implement Phase A helpers for repair-task diagnosis detection and missing-field reporting
- [x] Validate Go package tests for the planner package
- [x] Review whether the instrumentation is sufficient for Phase B soft gate
- [x] Implement Phase C hard gate for repair tasks missing diagnosis metadata
- [x] Implement Phase D shared-cause clustering and blast-radius diagnosis enforcement
- [x] Validate the planner package after Phase D changes

## What Phase A does
- Detects repair-classified tasks via issue/failure wording in summary, plan, and evidence.
- Detects missing diagnosis fields (`root_cause`, `evidence`, `fix_target`) in structured data or captured artifacts.
- Emits a structured warning log when `preview_patch` is used in a repair task without a diagnosis in place.
- Leaves behavior unchanged; no hard gating yet.

## Notes
This is intentionally a non-breaking step. The design still keeps the participant pattern and Ask orchestration intact, but adds observability so we can measure the actual repair-path gap before enforcing anything.

The implementation direction now also includes a distributed runtime proxy object: one logical Ask/ReAct object that is implemented across existing files, but treated as a single owner of turn state and phase progression.

## Phase B/C/D status
- [x] Soft gate implemented: preview_patch now returns a diagnosis-required warning for repair tasks missing root_cause/evidence/fix_target.
- [x] Hard gate implemented: repair preview is blocked until diagnosis includes root_cause, evidence, fix_target, shared_with, and blast_radius.
- [x] Shared-failure clustering is enforced by grouping repeated failure signatures before patch preview proceeds.
- [ ] Validate the warning appears in the app runtime with a real repair prompt and patch preview.

## Proxy Runtime Refactor
- [ ] Introduce a distributed runtime proxy object as the single logical owner of the Ask/ReAct turn.
- [ ] Keep the first pass behavior-compatible by delegating to current helpers and phase files.
- [ ] Move state and coordination into the proxy incrementally after the compatibility pass is green.
- [ ] Add turn-state fields for goal, known facts, missing facts, and transition intent.
- [ ] Make `EvaluateTransaction` consult the proxy state so it can continue, pivot, or short-circuit inside the same Ask/ReAct turn.
- [ ] Preserve existing defaults until the new state-aware path is validated end-to-end.
