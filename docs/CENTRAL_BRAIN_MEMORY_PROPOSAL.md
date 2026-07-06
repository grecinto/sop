# Central Brain Memory Proposal

## Summary

This proposal describes a narrow, optional shared-memory layer for Northstar that sits on top of the existing local-first copilot model rather than replacing it. The goal is to promote only a small set of high-value, privacy-safe workflow patterns into a shared layer when that shared layer clearly helps future local copilots.

The core idea to model is a shared pattern engine: each local node learns from its own experience, generalizes what is repeatedly useful, and contributes that generalized knowledge back to the fleet so future nodes can benefit from it.

## Product Principle

The central brain should capture reusable behavior, not raw user-specific detail.

## V1 Implementation Choice

For V1, the shared central-memory layer should reuse the existing root service-account LTM store rather than introducing a separate backing store. In backend handlers, the logical concept of "central memory" should resolve to the root SA user's LTM, represented by a constant such as `ltm_root`.

### Requirements

- The feature must be additive and must not break existing local-memory, routing, or workflow behavior.
- Existing capabilities should continue to work unchanged when the shared-memory layer is absent, disabled, or unavailable.
- The shared-memory layer should be introduced as an optional enhancement that improves behavior when present.
- The rollout should preserve backward compatibility for current clients and existing LTM flows.

This keeps the architecture simple and practical:
- central memory remains a distinct logical scope for routing and policy
- the backing store is the existing LTM store for the root SA user
- the system can evolve later to a more isolated or dedicated shared-memory service if needed

For now, the separation is logical and policy-driven, while the physical storage remains the root SA LTM.

This should be clearly separated from the Knowledge Base feature, while also acknowledging that the dynamic LTM is a form of knowledge base too.

There are two different layers:
- Static Knowledge Base: curated, explicit knowledge that can be generated, exported, and preloaded onto client nodes
- Dynamic Knowledge Base / HQ LTM: learned, generalized knowledge that emerges from many users and is promoted to the shared central layer through memory

The static KB is for durable, authored guidance such as language-specific best practices, framework patterns, and shipped playbooks. The dynamic KB / HQ LTM is for behavior that is learned over time from actual usage and is only promoted when it is broadly useful.

At a broader level, LTM is the full memory system. Over time it can contain many kinds of knowledge, including:
- domain knowledge
- workflow and pattern knowledge
- interaction experience with the user
- user-specific preferences and habits when appropriate
- generalized lessons that become reusable across sessions and nodes

In that sense, LTM is not just one KB. It is the memory substrate that can hold many KBs and many forms of learned experience over time.

Examples of static KB content:
- Go best practices and idioms
- Python patterns and anti-patterns
- Rust safety guidance
- language-specific bindings and supported workflows for each of the supported languages

Examples of dynamic HQ LTM content:
- a repair sequence that repeatedly works across many nodes
- a safe verification pattern that improves outcomes across the fleet
- a playbook that emerges after many successful uses
- a shared anti-pattern to avoid

This proposal is intentionally aligned with the model you already have: local-first memory remains the primary substrate, and any shared layer is only an optional extension for patterns that are worth promoting across hosts. It is not a replacement for the existing model, and it should not be treated as a step backward to a central-brain-first architecture.

The system should promote only patterns that are:
- high-confidence
- reusable across sessions
- privacy-safe
- useful to a future copilot
- applicable to a future task

If a detail is too specific, too private, or too brittle to generalize, it should not be promoted.

## Why a Central Brain

A host-local copilot is ideal for:
- immediate task context
- private state
- fast execution
- environment-specific behavior

That means your existing model already covers the core operating layer well. The central brain is only worth adding where there is a clear shared benefit that a single host cannot discover or consolidate on its own.

The value here is not a second full memory system. The value is multi-user learning: when many nodes encounter similar situations, the system can identify patterns that are broadly useful and make them available to all future nodes.

A central brain is worth adding when the value comes from sharing across many hosts rather than from any single host acting alone. This is the part that a local-only system cannot do well.

Examples that do not need an HQ brain:
- personal preferences for how a user likes to work
- repo-specific shortcuts that only apply to one machine
- private notes or temporary task state
- one-off troubleshooting steps that are not reusable

Examples that do need an HQ brain:
- a repair sequence that works for a common authentication failure across many sites
- a safe verification pattern that repeatedly prevents regressions
- a shared anti-pattern that should be avoided by every copilot after it appears in multiple environments
- a proven workflow for triaging dependency or test failures that should become a standard playbook for the fleet
- a behavior that becomes valuable only after it is observed repeatedly across hosts and promoted to a shared standard

The central brain should not attempt to replace the local mind. It should make the local mind better over time by turning repeated local successes into shared, reusable intelligence for future hosts.

## Design Goal

The central brain should be a shared behavior-pattern layer for copilots.

Its role is to support multi-user learning by turning repeated local outcomes into a compact, reusable signal that can be retrieved by other nodes when relevant.

It should store:
- what worked
- why it worked
- when it should be reused
- what to avoid

It should not store:
- raw chats
- sensitive content
- private repo specifics
- brittle one-off details
- anything that does not clearly improve future copilot behavior

## Core Model

### 1. Local Signal
A local copilot emits a compact signal after a task or workflow run.

Suggested fields:
- id
- source_copilot_id
- host_id
- session_id
- timestamp
- capability
- task_type
- observed_outcome
- confidence
- scope
- safety_flags

### 2. Behavior Pattern
A generalized, reusable pattern derived from one or more signals.

Suggested fields:
- id
- capability
- trigger_conditions
- recommended_action
- rationale
- applicability
- confidence_score
- recurrence_count
- outcome_quality
- safety_constraints
- lifecycle_state
- provenance

### 3. Central Brain Memory Object
A promoted memory artifact that can be retrieved by future hosts.

Suggested fields:
- id
- pattern_id
- summary
- behavior_pattern
- applicability_scope
- confidence_score
- recurrence_count
- created_at
- updated_at
- lifecycle_state
- embedding_ref

### 4. Promotion Policy
Rules that decide whether a signal becomes shared memory.

Suggested fields:
- capability
- minimum_confidence
- minimum_recurrence
- allowed_scopes
- safety_rules
- review_required
- decay_rules

## What Should Be Promoted

The highest-value items for the central brain are:

1. Proven workflow patterns
   - how the copilot usually handles a refactor
   - how it diagnoses a failing test
   - how it gathers context before editing
   - how it validates a change

2. Safe decision heuristics
   - when to inspect dependencies before editing
   - when to ask for confirmation before destructive changes
   - when to prefer targeted validation over broad validation
   - when to switch from search to direct action

3. Verified success playbooks
   - sequences that reliably solved a class of problem
   - strategies that consistently improved completion rates
   - repair patterns that repeatedly worked

4. Tool-use preferences
   - preferred order of operations
   - preferred verification steps
   - preferred context-gathering tools
   - preferred fallback behavior when a tool fails

5. Failure and anti-pattern knowledge
   - common mistakes to avoid
   - patterns that tend to create regressions
   - contexts where the copilot should slow down and verify

## What Should Not Be Promoted

Do not promote:
- raw chats
- exact prompts
- sensitive or private content
- one-off quirks
- brittle specifics
- anything that cannot generalize into future action

## Flow From Host to Central Brain

### Host-side emission
Each local copilot records compact signals from successful or informative task runs.

These signals should capture:
- task intent
- what action sequence was taken
- what outcome occurred
- what evidence supports the result
- any safety or verification steps

### HQ/site-side normalization
The central brain service normalizes signals into reusable behavior patterns.

This pipeline should:
- deduplicate similar signals
- group by capability and task type
- score confidence and recurrence
- suppress unsafe or low-value patterns

### Promotion decision
Only patterns meeting policy thresholds should be promoted.

Suggested threshold example:
- confidence >= 0.8
- observed in at least 3 sessions
- passes safety and privacy checks

## Flow Back From Central Brain to Each Host

The central brain should not push everything to every host.

Instead, the local host should request or receive only relevant memory objects based on current context.

### Host-side request
The local copilot sends a compact retrieval request containing:
- capability
- task type
- current scope
- repo/domain context
- environment hints
- safety constraints

### HQ/site-side retrieval
The central brain ranks shared memory objects by:
- capability match
- task-type match
- scope match
- recency
- confidence
- safety compliance

### Host-side merge
The local copilot merges the returned memory objects with local runtime context and uses them to guide the next decision.

The central-brain memory should influence the next action, but it should not override the host’s live local state.

## Recommended Sync Model

The architecture should be:
- local mind for immediate execution
- shared pattern layer for promoted, reusable behavior memory
- retrieval-based sync for relevant patterns

This gives the product two layers of intelligence:
1. local, immediate, private
2. shared, durable, reusable across the fleet

In other words, the central layer should behave like a pattern compiler for multi-user learning: it should take many local experiences, distill the useful structure, and return that structure to future nodes as guidance.

## MVP Scope

An initial implementation should focus on a very small set of high-value shared capabilities:
- workflow patterns
- safe decision heuristics
- verified success playbooks
- tool-use preferences
- failure/anti-pattern guidance

That is enough to support the multi-user learning loop without creating noisy or low-value memory.

## Success Criteria

The feature should be considered successful when the central brain helps future copilots:
- choose better next actions
- avoid repeated mistakes
- reuse proven workflows
- improve success rate over time
- stay safe and privacy-preserving

## Recommendation

Proceed with a narrow, high-signal central-brain feature only if it can capture something that clearly helps future copilots. If the system cannot identify a reusable pattern that improves later decisions, it should not be built.

In practice, that means the feature should be treated as a go/no-go product decision:
- go only when the shared memory can produce better next actions, fewer repeated mistakes, or safer execution
- no-go when the content is too private, too specific, too noisy, or too weak to improve future behavior

This keeps the feature focused on genuine value rather than on storing data for its own sake.
