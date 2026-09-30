# Copilot Engineering Judgment — Design Plan

Status: Draft for review
Owner: Coding Workflow / Phase System
Related: [PHASE_SYSTEM_STATUS_AUG_2026.md](PHASE_SYSTEM_STATUS_AUG_2026.md), [PHASE_AWARE_CONTEXT_OPTIMIZATION.md](PHASE_AWARE_CONTEXT_OPTIMIZATION.md), [DOMAIN_AGNOSTIC_PHASE_SYSTEM.md](DOMAIN_AGNOSTIC_PHASE_SYSTEM.md)

## 1. Problem Statement

The coding copilot currently has two separate deficiencies that are often conflated:

1. **Orchestration weakness**: the agent stalls in execution phase re-investigating instead of patching, due to soft phase gates, generous tool exposure, and prompt-only directives.
2. **Engineering judgment weakness** (the deeper problem): even when the agent does patch, it has no structured mechanism to:
   - cluster failures by shared root cause instead of treating each failing test as an independent item
   - form and record a falsifiable hypothesis before editing code
   - decide whether the defect is in the implementation or in the test itself
   - assess blast radius (callers, invariants, contracts) before editing
   - prefer the smallest correct diff for a stated reason, not just because it was told to

This document proposes a design to close gap #2, using the existing phase/artifact infrastructure rather than inventing a parallel system.

## 2. Design Principles

- **Reuse the existing phase artifact mechanism** (`phaseArtifact`, `structured_data`, `capture`/`state` tools) instead of adding new capability types.
- **Make judgment steps structurally required**, not advisory log lines. Currently `requireRootCauseEvidence` in [lib/sop/ai/agent/planner/phase_base.go](../lib/sop/ai/agent/planner/phase_base.go#L484-L556) is deliberately lenient ("Always allow progression — trust the LLM to orchestrate"). That leniency is the correct default for general tasks, but wrong for the repair/patch path specifically.
- **Keep the Participant Pattern**: phases still only respond to `BuildContext` / `ExecuteTool`; the Ask pipeline in `copilot.go` remains the sole controller. No new capability should loop internally.
- **Prefer schema-level enforcement over prompt-level pleading.** Every "CRITICAL: do X now" line in [lib/sop/ai/agent/planner/phase_coding.go](../lib/sop/ai/agent/planner/phase_coding.go#L78-L95) is a signal that the real fix belongs in tool availability/schema requirements, not prose.

## 2.1 New Concept: Distributed Runtime Proxy Object

The Ask/ReAct system should be modeled as one logical runtime object, even though its implementation is spread across multiple files today.

The proxy object is the conceptual owner of the full turn lifecycle:
- turn input normalization
- routing/classification
- phase setup and tool exposure
- ReAct execution
- transaction evaluation
- persistence and queue advancement

The implementation may remain distributed across the current helpers and files, but the mental model should be singular: the outer Ask pipeline invokes one runtime object, and that object manages itself through a stable interface.

This avoids overfitting the design to file layout or helper boundaries. The proxy is not a new parallel system; it is the explicit wrapper around the existing distributed behavior.

## 2.2 New Concept: Stateful Turn Decision Model

The proxy object should also carry a live turn model so the Ask/ReAct loop can reason about its own progress instead of waiting for a separate post-turn evaluator.

At minimum, the object should track:
- the current goal for the active turn
- the criteria that satisfy that goal
- known facts already gathered
- missing facts still required to proceed
- the current mode or substate of the turn
- the last transition decision
- confidence in the current judgment

This state lets the runtime answer three questions during the turn:
- Is the current goal already satisfied?
- Should the loop keep gathering facts?
- Should the runtime pivot to another phase or action immediately?

In this model, `EvaluateTransaction` becomes a state-sensitive decision method on the proxy object. It should not only answer whether to enqueue a next phase after the turn; it should also be able to short-circuit the current loop, continue within the same loop, or request a pivot when the current state makes that obvious.

The first implementation pass should remain compatibility-first:
- keep the existing helpers and post-turn transaction evaluation
- route the current Ask/ReAct execution through the proxy object
- let the proxy update and inspect its own state before deciding whether the loop should continue
- preserve current defaults unless the state is clearly sufficient to stop, continue, or pivot

This is the mechanism that allows fluid state changes without flattening the existing guardrails.

## 3. New Concept: Diagnosis Artifact

Introduce a required structured object, `diagnosis`, captured before any patch tool becomes usable for repair-style tasks (test failures, bug reports, regressions).

### 3.1 Schema

```json
{
  "root_cause": "string — the specific mechanism causing the failure, not a symptom restatement",
  "evidence": ["string — concrete evidence: stack trace line, failing assertion, log excerpt"],
  "defect_location": "string — file:symbol believed to be the actual defect",
  "fix_target": "code | test | both",
  "fix_target_reason": "string — why code vs test vs both",
  "shared_with": ["string — other failing test names/ids that share this same root cause"],
  "blast_radius": {
    "callers_checked": true,
    "callers": ["string — file:symbol of callers/consumers"],
    "invariants_at_risk": ["string — behavior/contract that could break"]
  },
  "confidence": "low | medium | high"
}
```

This is stored the same way existing artifacts are stored — as `structured_data.diagnosis` inside the `phaseArtifact` captured during investigation/execution — so no new storage subsystem is needed. See existing `structured_data` field in [captureToolSchema()](../lib/sop/ai/agent/planner/phase_base.go#L699-L716).

### 3.2 Why this shape

- `root_cause` + `evidence` force a hypothesis, not just "read some files."
- `fix_target` forces the code-vs-test decision explicitly, closing the gap where the agent defaults to "always patch source."
- `shared_with` is what turns 38 failing tests into a handful of real defects (clustering).
- `blast_radius` forces a callers/invariants check before edit, addressing the "no blast radius awareness" gap.
- `confidence` lets the transaction evaluator decide whether more investigation is warranted vs proceeding.

## 4. Enforcement Points (where leniency becomes a gate)

### 4.1 Execution phase tool gating

Today, execution phase always exposes `preview_patch`, `apply_patch`, `apply_multi_file_patch` (see [phase_coding.go GetTools "execution"](../lib/sop/ai/agent/planner/phase_coding.go#L340-L352)).

Change: for repair-classified tasks (test failures, bug fix, regression — reuse the existing `shouldRequireRootCauseEvidence` signal detection), `preview_patch` requires a `diagnosis` object to already exist in the phase artifact / workstream state. If absent, `preview_patch` returns a structured error telling the model exactly which fields are missing, rather than silently succeeding.

This replaces prompt text like:

> "CRITICAL: If you have test failures and source context, IMMEDIATELY generate patches."

with an actual contract the tool enforces.

### 4.2 Strengthen `requireRootCauseEvidence`

Currently this function (see [lib/sop/ai/agent/planner/phase_base.go](../lib/sop/ai/agent/planner/phase_base.go#L484-L556)) accepts almost any non-empty evidence and always returns `nil` ("Always allow progression — trust the LLM to orchestrate"). 

Change: split it into two tiers:
- **General tasks** (current behavior): stay lenient/advisory.
- **Repair tasks with a diagnosis requirement flag**: require the structured `diagnosis` object with non-empty `root_cause`, `evidence`, and `fix_target` before `status` can move to `ready_for_execution`.

This is additive — it does not change behavior for non-repair work.

### 4.3 Failure clustering before work-item creation

The existing inventory mechanism (`compile_artifact_inventory`, `select_inventory_item`, `complete_inventory_item` — see tool registration logs and [phase_coding.go](../lib/sop/ai/agent/planner/phase_coding.go)) currently appears to create one inventory item per failing test.

Change: when `run_tests` produces multiple failures, run a clustering pass before inventory compilation:
- group by shared top-of-stack file:line or shared assertion message pattern
- each cluster becomes one inventory item with `member_tests: []string`
- `complete_inventory_item` marks all member tests complete together, not one at a time

This directly targets "38 failures might be 3 bugs."

### 4.4 Blast radius as a real (cheap) tool, not a prompt line

Add a lightweight tool, `find_callers` (or extend `search_repo` with a `symbol_callers` mode), that returns direct callers/usages of a given symbol. Require it to be called at least once when `defect_location` names a function/method with more than N call sites, before `apply_patch` is allowed for that file.

Keep this cheap: this is a `grep`/AST-based lookup, not a new capability domain — reuse existing `search_repo`/`index_repo_structure` machinery already present in the tool registry.

## 5. Phased Rollout Plan

### Phase A — Instrumentation only (no behavior change)
- Add `diagnosis` to the `structured_data` schema as optional.
- Log (not block) when `preview_patch` is called without a `diagnosis` present, for repair-classified tasks.
- Goal: measure how often this already happens today, establishing a baseline.

### Phase B — Soft gate
- When `diagnosis` is missing for a repair task, return a structured warning result from `preview_patch` (not an error) that names the missing fields, and let the model self-correct.
- Track whether models comply without hard failure.

### Phase C — Hard gate for repair-classified tasks only
- `preview_patch` refuses (structured error, not exception) when `diagnosis.root_cause` or `diagnosis.fix_target` is empty, only for tasks where `shouldRequireRootCauseEvidence`-style detection fires.
- General (non-repair) coding tasks remain unaffected.

### Phase D — Clustering + blast radius
- Add failure clustering to inventory compilation.
- Add `find_callers` tool and its pre-`apply_patch` requirement for multi-caller symbols.

### Phase E — Metrics and tuning
- Reuse the existing metrics pattern from [lib/sop/ai/agent/metrics/phase_aware_context.go](../lib/sop/ai/agent/metrics/phase_aware_context.go) to track:
  - iterations before first `preview_patch` (existing metric)
  - % of `preview_patch` calls with a valid `diagnosis` attached (new)
  - number of inventory items before/after clustering (new)
  - post-patch regression rate, if measurable (new)

## 6. What Explicitly Does Not Change

- The Participant Pattern / Ask-pipeline-as-sole-controller design is untouched — this is purely new schema + gating logic inside existing phases.
- Non-repair coding tasks (features, refactors without a stated bug) are not subject to the hard gate in Phase C.
- No new domain/capability is introduced; this lives inside `coding_workflow`'s existing `investigation`/`planning`/`execution` phases.

## 6.1 Refactor Boundary

The first implementation step should preserve behavior and route the current Ask/ReAct flow through the proxy object without changing routing semantics, phase ownership, or tool contracts.

That means the proxy initially delegates to existing helpers in `ask_pipeline.go`, `copilot_runtime.go`, `cognitive_layer.go`, and the planner phase files. The refactor only becomes invasive after compatibility is proven.

## 6.2 Runtime Decision Boundary

The runtime object should own the distinction between:
- loop continuation within the same Ask/ReAct turn
- immediate phase pivot inside the turn
- post-turn phase enqueueing
- completion with no additional work

That keeps `EvaluateTransaction` expressive enough to react to live state, while still leaving the outer Ask pipeline as the guardrail and queue controller.

## 7. Open Questions for Review

1. Should `fix_target: test` ever be allowed to proceed without human approval, given it changes the spec rather than the implementation?
2. What is an acceptable false-positive rate for the hard gate in Phase C before it becomes a productivity drag instead of a quality gate?
3. Should `find_callers` results be cached per session to avoid repeated cost when multiple patches touch the same symbol?

## 8. Summary

The current system optimizes for "reach `apply_patch` sooner." This plan optimizes for "know why before you touch code, know if you're fixing the right thing, and know what else it affects" — using the same artifact/phase infrastructure already in place, escalating from advisory to enforced only where evidence shows the leniency is being exploited (i.e., patches generated with no real diagnosis).
