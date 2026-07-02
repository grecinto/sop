# LTM Hybrid Memory Implementation Plan

Status: In Progress
Last Updated: 2026-07-01

## Objective

Extend the current long-term memory (LTM) system into a general-purpose, capability-aware memory layer that supports:

- coding workflow grounding for repository-specific work,
- research grounding for selected-space and knowledge-based workflows,
- multi-repo and multi-space continuity without mixing contexts incorrectly,
- safe lifecycle management for stale, unused, or orphaned memory artifacts.

This plan is intended to be updated as work is completed.

## Design Principles

- Keep LTM general-purpose and shared across capabilities.
- Add a top-level category so different capabilities can use the same memory store without collisions.
- Add scoped context so retrieval stays precise for repos, spaces, projects, or users.
- Preserve continuity for nearby work while avoiding stale context from unrelated topics.
- Prefer explicit metadata and lifecycle rules over implicit assumptions.

## Relevant Implementation Areas

- Core agent routing and prompt assembly: [lib/sop/ai/agent/copilot.go](../lib/sop/ai/agent/copilot.go)
- Routing and continuity hints: [lib/sop/ai/agent/copilot_routing.go](../lib/sop/ai/agent/copilot_routing.go)
- Selected-space grounding: [lib/sop/ai/agent/copilot_selected_space.go](../lib/sop/ai/agent/copilot_selected_space.go)
- Coding workflow context assembly: [lib/sop/ai/agent/codingworkflow/orchestrator.go](../lib/sop/ai/agent/codingworkflow/orchestrator.go)
- Coding workflow capability registration: [lib/sop/ai/agent/codingworkflow/capability.go](../lib/sop/ai/agent/codingworkflow/capability.go)

## Completed Work

The following items are now implemented and should be reflected as completed in the tracking notes:

- [x] Selected-space grounding now appears only when there is real evidence, avoiding over-anchoring on unrelated spaces.
- [x] Research-before-action guidance is explicit in prompts for research-oriented flows.
- [x] Successful research paths can be persisted and reused for similar future asks.
- [x] Coding workflow now preserves repository-path hints and uses them to maintain continuity across related edits.
- [x] Capability-affinity hints and MRU-based continuity help preserve nearby work without locking onto stale topics forever.
- [x] Topic-switch reset logic helps prevent old routing context from leaking into new unrelated requests.
- [x] Regression tests cover routing, selected-space grounding, and coding-workflow context behavior.
- [x] Added initial LTM metadata scaffolding so encoded memory entries now carry category, capability, scope, and provenance metadata for future scoped retrieval.

## Next Implementation Backlog

### Phase 1 — Memory Identity and Categorization

- [ ] Add a top-level category field for each LTM entry, such as coding, research, planning, general, desktop, writing.
- [ ] Add scoped context fields for repo, space, project, user, or session.
- [ ] Add capability metadata so retrieval can distinguish workflow-specific memory from general memory.
- [ ] Add provenance metadata to track whether the memory came from code, research, chat, docs, or other sources.
- [ ] Add freshness and lifecycle status fields: active, warm, cold, archived, deleted.

Acceptance criteria:
- Memory entries can be filtered by category, scope, and capability.
- A coding entry in repo A does not surface as primary context for repo B unless fallback rules allow it.

### Phase 2 — Retrieval Strategy

- [ ] Implement category-aware retrieval so the query first searches the relevant memory category.
- [ ] Implement capability-aware retrieval so coding memory is not mixed with research memory by default.
- [ ] Implement scope-aware retrieval so repo-scoped and space-scoped memories are resolved separately.
- [ ] Implement a fallback hierarchy: exact scope -> related scope -> same category -> general memory.
- [ ] Add a confidence threshold so weak or irrelevant memory is ignored rather than injected into prompts.

Acceptance criteria:
- Research asks retrieve space-scoped research artifacts first.
- Coding asks retrieve repo-scoped coding artifacts first.
- Weak retrieval results do not degrade prompt quality.

### Phase 3 — Hybrid Context Assembly

- [ ] Add a hybrid retrieval layer for coding workflow that merges deterministic context with semantic memory.
- [ ] Add a hybrid retrieval layer for research workflow that merges selected-space context with stored research memory.
- [ ] Make repository and space hints part of the retrieval query construction.
- [ ] Preserve current curated context behavior as the default backbone, while using retrieval as an enrichment layer.

Acceptance criteria:
- The coding workflow still benefits from active file, symbol, and test context.
- Semantic memory adds useful recall for similar patterns or prior fixes.

### Phase 4 — Multi-Repo and Multi-Space Continuity

- [ ] Support repository switching without losing prior repo memories.
- [ ] Support selected-space switching while preserving the ability to use recent related research memory.
- [ ] Track recent scope affinity so the system can prefer the currently active repo or space but still fall back to nearby recent contexts.
- [ ] Prevent cross-scope contamination when the current task is clearly unrelated.

Acceptance criteria:
- Switching repos does not cause stale coding memory from another repo to dominate.
- Switching spaces does not cause unrelated research context to overpower the current task.

### Phase 5 — Lifecycle, Cleanup, and Retention

- [x] Define retention rules for memory entries by category and scope.
- [x] Add staleness detection based on last use, age, scope validity, and version drift.
- [x] Detect orphaned artifacts when a repo or space no longer exists.
- [x] Implement a soft-delete and compaction flow before hard deletion.
- [x] Add a nightly wakeup servicer that runs cleanup and retention checks for low-value entries.
- [x] Add a weekly compaction pass that archives, merges, or prunes stale memory entries after the nightly sweep.
- [x] Treat hourly metadata updates for active usage as lightweight inline bookkeeping on retrieval/write paths, updating fields such as last_used_at and access_count without introducing a separate hourly job.
- [x] Support explicit user lifecycle cues such as “done with repo X” or “archive this context” as retirement signals.
- [ ] Use lifecycle status to drive visual treatment in the existing Data Manager view, such as dimming or tagging stale categories.

Acceptance criteria:
- Old repo memories are retired when the repo is no longer relevant or no longer exists.
- Low-value memory artifacts are archived or deleted automatically according to policy through the nightly wakeup cleanup loop and the weekly compaction pass.
- Cleanup does not remove active, recent, or high-value memory unexpectedly.
- Lightweight usage bookkeeping updates active-memory metadata inline so the nightly pass can make decisions from fresh recency data.

### Phase 6 — Testing and Documentation

- [ ] Add unit tests for categorization, scoping, and retrieval filtering.
- [ ] Add tests for repo-switch and space-switch behavior.
- [ ] Add tests for stale-memory detection and cleanup behavior.
- [ ] Update this document whenever a milestone is completed.
- [ ] Add or update developer-facing documentation for the new memory model.

## Suggested Implementation Order

1. Add category and scope metadata to LTM entries.
2. Implement retrieval filters and fallback hierarchy.
3. Introduce hybrid retrieval for coding workflow.
4. Extend the same pattern to research and selected-space workflows.
5. Add retention, cleanup, and staleness detection.
6. Validate through end-to-end scenarios and documentation updates.

## Suggested Update Workflow

When a task is completed:

1. Mark the checkbox as completed.
2. Record the date and summary of the change.
3. Move any important notes into the completed work section if they are now part of the stable behavior.
4. Keep this document as the single source of truth for the next implementation steps.
