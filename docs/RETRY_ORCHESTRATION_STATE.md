# Retry Orchestration State

**Date**: 2026-07-31  
**Status**: ACTIVE  
**Purpose**: Document the current retry/orchestration machinery and the corrected architecture direction: design for LLM-as-Orchestrator first, then define explicit policy interfaces for places where LLM guidance or heuristic enforcement may be used.

## Summary

The earlier framing was backwards.

The correct architecture is:

1. design the loop around **LLM-as-Orchestrator**
2. identify the narrow functional points where policy is needed
3. define explicit interfaces for those policy points
4. allow each policy point to be implemented by either:
	- LLM-facing recommendation logic, or
	- heuristic/rules enforcement, where hard guarantees are required

The LLM remains the primary orchestrator. Policy machinery exists to constrain, inform, or validate specific decisions, not to become a hidden outer controller.

## Architectural Principle

The retry layer should not be designed as “heuristics plus some guidance.”

It should be designed as:

- provider-owned loops that preserve model-native orchestration
- explicit policy surfaces for retry and recovery decisions
- minimal enforcement-only rails for safety and correctness

This is the same architectural rule already used elsewhere in the system:

- Ask/ReAct is the controller
- participants provide capabilities and state
- policy modules do not own flow

## Policy Surface

The retry system has a small number of functional policy points.

These policy points should be treated as interfaces, not as an excuse to centralize orchestration into heuristic code.

### 1. Recoverability Policy

Question: is this failure retryable, terminal, or clarification-worthy?

Possible implementations:

- heuristic classifier for known transport, validation, or schema failures
- LLM-informed reasoning based on full tool/result context

### 2. Retry Tier Policy

Question: should the next attempt be quick-fix, research-first, or clarify?

Possible implementations:

- heuristic tiering based on failure count and error family
- LLM-guided selection from explicit retry options

### 3. Repair Strategy Policy

Question: if retryable, what repair shape is appropriate?

Possible implementations:

- same-tool repair
- research-first repair
- LLM-authored strategy selection informed by retry history

### 4. Retry Budget Policy

Question: should the loop continue, stop, or allow a bounded extension?

Possible implementations:

- heuristic ceilings and progress-based extensions
- LLM-visible soft budget guidance inside a hard upper bound

### 5. Clarification / Escalation Policy

Question: when should the system stop retrying and ask for clarification or escalate?

Possible implementations:

- heuristic thresholds for repeated failure or dead-end states
- LLM recommendation to request clarification earlier based on context

## Interface Rule

Each policy point should have a narrow contract:

- accept the local retry state, failure context, and carryover history
- return recommendation, constraint, or enforcement outcome
- never own the outer conversation loop
- never silently replace the model as orchestrator

In other words, policy modules advise or constrain. They do not drive.

## Hard Rails

Some policy points must remain heuristic/rules-enforced because they protect correctness or bounded execution.

These are the current hard rails:

- maximum retry ceilings
- repair-step enforcement (do not switch tools while a repair is pending)
- terminal hard-error short-circuiting
- carryover and persistence correctness
- phase completion validation

These are enforcement surfaces, not orchestration surfaces.

## Model-Visible Guidance

Everything else should trend toward model-visible policy context rather than silent pre-decision.

Current examples:

- retry tier context
- learned retry strategy recommendations
- retry history confidence and scope
- soft retry budget context
- repair directive context
- clarification-required context after repeated failure

## Shared Retry Kernel

The shared retry machinery lives in:

- `lib/sop/ai/retry.go`

It should be understood as shared policy substrate, not a shared orchestrator.

The current code shape now exposes explicit policy interfaces:

- `RecoverabilityPolicy`
- `RetryDecisionPolicy`
- `RetryBudgetPolicy`
- `RepairEnforcementPolicy`
- `RetryPolicySurface`

It currently owns:

- retry tiers
- repair strategies
- retry learnings
- structured `RetryGuidance` payload construction
- scope-aware recommendation logic
- error fingerprint normalization

## Provider-Owned Loop State

Provider-owned retry state lives in:

- `lib/sop/ai/generator/owned_loop_retry.go`

This is where provider-native continuation and model-visible orchestration context should remain centered.

It currently owns:

- failure counts
- pending repair state
- escalation state
- adaptive turn budget
- progress-based extension
- carryover retry learnings

## Carryover And Persistence

Retry learnings now flow through:

- `CarryoverState.RetryLearnings`
- short-term carryover merge
- hydration cloning
- durable persistence through the agent layer

This allows:

- adjacent-ask retry continuity
- cross-session reuse of successful retry patterns
- provider/domain/repo/task-class scoped reuse

## Current State

The current implementation is transitional.

What exists now:

- shared retry kernel
- provider-owned retry state
- learned retry persistence
- model-visible retry guidance in provider-owned loops
- structured retry guidance carried on `ReActToolResult.RetryGuidance`
- provider request builders now consume structured retry guidance in native continuation state instead of relying only on reparsed retry prose
- Gemini continuation/final-turn phase selection and allowed-next-action shaping now derive from structured retry guidance rather than `RepairDirective` string presence alone
- Anthropic latest-turn continuation state and repair/final prompt shaping now derive from structured retry guidance rather than relying only on tool-error prose
- the shared-compatible adapter summary, repair-directive, and ask-anchored MRU helpers now prefer `RetryGuidance` over retry-text parsing when both are available
- the shared-compatible adapter now also wraps carried tool continuations with structured `react_state` and `retry_guidance`, so strategy consumers do not need to infer retry intent from raw tool-result prose alone
- clarification handoff is now also structured via `ClarificationRequest`, allowing pending clarification state and resume metadata to persist without relying only on assistant question text heuristics
- the shared-compatible path now exposes an explicit `request_clarification` action/tool contract so the model can request one missing fact as a first-class terminal handoff instead of only falling back to final-text question conventions
- heuristic enforcement for hard rails

What still needs architectural tightening:

- make the policy points explicit as interfaces
- distinguish recommendation surfaces from enforcement surfaces more formally
- ensure adaptive machinery does not silently decide what the model should decide

## Current Tier 2C State

Implemented:

- adaptive retry budgets
- progress-based budget extension
- learned retry strategy persistence
- scope-aware learned strategy recommendations
- normalized error fingerprints

Refined for LAO:

- learned strategy is surfaced as guidance rather than only silent override
- adaptive budget is moving toward soft-budget signaling instead of purely heuristic extension

## Next Improvement Direction

The next quality step is not “more heuristics with better prompts.”

It is to formalize the retry/recovery policy surface so the system can intentionally choose, per policy point, between:

- LLM-guided decision support
- heuristic/rules enforcement

while keeping the LLM as the orchestrator of the overall loop.