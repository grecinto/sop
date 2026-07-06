# LTM Hybrid Memory Implementation Plan

Status: Milestone complete / roadmap ongoing
Last Updated: 2026-07-02

## Objective

Extend the current long-term memory (LTM) system into a general-purpose, capability-aware memory layer that supports:

- coding workflow grounding for repository-specific work,
- research grounding for selected-space and knowledge-based workflows,
- multi-repo and multi-space continuity without mixing contexts incorrectly,
- safe lifecycle management for stale, unused, or orphaned memory artifacts.

This plan is intended to be updated as work is completed.

## V1 Shared Central-Memory Scope

For V1, the HQ layer should remain deliberately narrow. The shared layer should only cover the capabilities that are meaningfully shared across nodes and that cannot be supplied well by local-only memory.

The four V1 capabilities to prioritize are:

For V1, the shared central-memory layer should be implemented as a logical scope in the backend handlers that maps to the existing root service-account LTM store. In practice, this means the central-memory path will resolve to the root SA user's LTM, represented by `ltm_root`, while still being treated as a distinct shared-memory scope for routing, policy, and retrieval.

This gives us a minimal, practical architecture:
- local memory remains the primary runtime memory for private and immediate context
- central memory is a shared logical layer for promoted patterns and reusable guidance
- the backing store is the existing root SA LTM for now, which is sufficient to validate the feature end to end

### Implementation requirements

- The shared-memory work must be additive and must not break existing features.
- Existing local-memory, routing, and workflow flows must continue to work unchanged when the shared layer is absent or unavailable.
- Central-memory behavior should be opt-in or gracefully degraded so current users are not affected.
- The initial implementation should preserve backward compatibility with current client and server behavior.

The four V1 capabilities to prioritize are:

1. Shared learned playbooks
   - promote successful repair, debug, refactor, and verification patterns from many local runs into reusable playbooks
2. Shared anti-pattern and failure memory
   - capture recurring mistakes, regressions, and failure modes so future nodes can avoid them
3. Cross-node retrieval and ranking
   - allow each client node to ask HQ for the most relevant shared patterns for its current repo, language, task, or domain
4. Versioned language KB distribution
   - centrally manage and ship curated KBs for supported stacks such as Go, Python, Rust, and others, then update them centrally for all clients

These capabilities should complement local memory rather than replace it. Local memory remains the home for private context, environment-specific state, and fast local execution. HQ memory is for generalizable, shared intelligence.

## HQ Server and Bandwidth Requirements

V1 requires a modest but reliable HQ service. The shared-memory backend should include:

- an API and ingestion service for local nodes to submit compact signals and retrieve shared patterns
- a vector retrieval service or embedding-backed search layer
- durable storage for promoted memory objects, KB payloads, and metadata
- auth, policy enforcement, logging, and observability

Suggested baseline for an initial V1 rollout:

- 2 x 16-core CPU nodes or equivalent, for roughly 32 vCPU total
- 64 GB RAM
- 2 TB NVMe SSD storage for indexes, memory objects, and KB payloads
- 1 Gbps uplink minimum, 10 Gbps preferred
- 99.9% availability target for the shared-memory service
- optional managed Redis/Postgres/vector database services for faster retrieval and operational simplicity

A GPU is not required for V1 if embeddings are generated on the client side or via a managed embedding service. The main bottlenecks are low-latency retrieval, durable storage, and predictable network bandwidth for syncing KBs and shared patterns.

## V1 Coding-Workflow Direction

For V1, the coding workflow should be built around high-quality repository-aware memory rather than a generic chat memory layer. The system should be able to retrieve the right repo context, remember prior edits and patterns, and ground future work in the active project.

The implementation should use a hybrid memory model:

- deterministic context for the active file, symbol, tests, and nearby package/module structure
- semantic memory for similar prior work, recurring patterns, and repo-specific recall
- structured metadata for scope, capability, lifecycle, and provenance

For code-aware retrieval, the system should derive compact memory objects from source files rather than storing full parse trees. The recommended memory objects are:

- repo-level summaries and architecture notes
- file-level summaries and purpose notes
- symbol-level summaries for functions, classes, methods, and interfaces
- relationship facts such as imports, calls, and ownership
- vector embeddings for semantic similarity and retrieval ranking
- lifecycle metadata for staleness, archival, and retention

This keeps retrieval precise and explainable while still allowing semantic search for high-quality coding assistance.

## Finalized Design Decision: Categories and Items for Coding LTM

Yes — for a high-quality coding workflow, both categories and items should participate in semantic retrieval.

### Category design

Categories should be the coarse-routing layer for LTM. They represent the semantic bucket or topic node for a repo/module/feature area. A category should be vectorized so the system can quickly identify the right topic cluster even when the wording differs from the current request.

Recommended category fields:
- stable ID and parent/child hierarchy
- human-readable name and path
- short description or semantic summary
- scope metadata such as repo, space, capability, and user/project
- lifecycle metadata such as active, warm, cold, archived
- a category-level embedding anchor or vector reference

### Item design

Items should be the fine-grained retrieval layer. They represent the actual memory artifact inside a category, such as a file summary, symbol summary, debugging note, task outcome, or guidance capsule. Items should be vectorized so the system can retrieve semantically similar memories even when the exact vocabulary changes.

Recommended item fields:
- stable ID and category linkage
- a concise title or summary
- a compact semantic payload derived from the original artifact
- source metadata such as repo path, symbol name, doc ID, or task ID
- scope and capability metadata
- lifecycle and freshness metadata
- an item-level embedding or vector reference

### Retrieval model

The optimal design is hybrid, not vector-only:
1. deterministic filters first (repo, capability, scope, lifecycle, category path)
2. category-level semantic routing second
3. item-level semantic matching third
4. reranking with path/symbol proximity and recency

This keeps semantic search powerful while preserving the precision needed for coding work.

### Storage recommendation

The actual embeddings should not be duplicated inside every prompt or stored as raw full-file parse trees. The clean design is:
- store compact text summaries and metadata on the category/item object
- store or reference the actual embedding in the vector subsystem keyed by category/item ID
- keep a lightweight vector hash or version marker on the object for de-duplication and refresh logic

## Design Principles

- Keep LTM general-purpose and shared across capabilities.
- Add a top-level category so different capabilities can use the same memory store without collisions.
- Add scoped context so retrieval stays precise for repos, spaces, projects, or users.
- Preserve continuity for nearby work while avoiding stale context from unrelated topics.
- Prefer explicit metadata and lifecycle rules over implicit assumptions.

## Current Lifecycle Semantics

Repo-scoped coding memory now uses a lightweight lifecycle heartbeat driven by actual usage and retrieval activity.

- A repository memory becomes active when it is used in a coding workflow, touched by retrieval, or explicitly reactivated.
- Each relevant access refreshes the last-used timestamp and increments the access count, which prevents a repo from drifting into stale or archived state while it remains in active use.
- Lifecycle state is stored on the repo-scoped category payload and surfaced in the Data Manager view with a simple badge plus a reactivation action for non-active repos.
- Retention and compaction remain conservative: stale entries may be archived over time, but active or recently used repositories stay visible and usable.

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
- [x] Coding-workflow memory now persists canonical repo-scoped payloads plus dual path/semantic indexes, with retrieval resolving back to the canonical document content.
- [x] Repo-scoped lifecycle signals now support retire/reactivate behavior for coding memory and keep lifecycle metadata in the canonical repo payload rather than item payloads.
- [x] Explicit lifecycle retention and compaction logic is implemented for stale or archived repo memories, with lightweight inline recency bookkeeping during retrieval/write paths.
- [x] Repo lifecycle state now surfaces in the Data Manager view via a category-level badge and a touch-to-reactivate control for non-active repo categories.
- [x] Repo-aware LTM metadata now carries category, capability, scope, provenance, lifecycle, and recency fields, with regression tests covering the core semantics.

## Next Implementation Backlog

### Phase 1 — Memory Identity and Categorization

- [x] Add a top-level category field for each LTM entry, such as coding, research, planning, general, desktop, writing.
- [x] Add scoped context fields for repo, space, project, user, or session.
- [x] Add capability metadata so retrieval can distinguish workflow-specific memory from general memory.
- [x] Add provenance metadata to track whether the memory came from code, research, chat, docs, or other sources.
- [x] Add freshness and lifecycle status fields: active, warm, cold, archived, deleted.

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

### Phase 2A — Code-Aware Memory and Vector Retrieval

- [x] Add a code-memory ingestion path that extracts repo/file/symbol summaries from source content.
- [x] Add lightweight parser or AST/tree-sitter-based extraction for functions, classes, interfaces, imports, and package/module boundaries.
- [x] Store derived code-memory objects with summary text, symbol identity, relationships, and vector embeddings.
- [x] Add retrieval ranking that blends path-based signals, symbol proximity, and semantic similarity.
- [x] Add a repo-aware fallback hierarchy so active-repo memory is preferred before cross-repo memory.

Acceptance criteria:
- Coding retrieval can surface the most relevant file or symbol even when the exact token overlap is low.
- Code memory remains compact and explainable rather than storing raw parse trees for every file.
- The system can retrieve both concrete repo context and semantically related prior work for the same project.

### Phase 3 — Hybrid Context Assembly

- [x] Add a hybrid retrieval layer for coding workflow that merges deterministic context with semantic memory.
- [x] Add a hybrid retrieval layer for research workflow that merges selected-space context with stored research memory.
- [x] Make repository and space hints part of the retrieval query construction.
- [x] Preserve current curated context behavior as the default backbone, while using retrieval as an enrichment layer.

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

Remaining high-priority follow-ups:
- [x] Surface repo lifecycle state in the existing Data Manager or repo-facing UI so archived/stale memories are visibly distinguishable via a category-level badge and a touch-to-reactivate control.
- [x] Add broader regression coverage for multi-repo and multi-space switching behavior via the retrieval-target coverage in the LTM metadata tests.
- [x] Add any remaining docs polish for the new repo-aware memory model and lifecycle semantics by updating this implementation plan with the completed behavior and remaining guidance.

Acceptance criteria:
- Old repo memories are retired when the repo is no longer relevant or no longer exists.
- Low-value memory artifacts are archived or deleted automatically according to policy through the nightly wakeup cleanup loop and the weekly compaction pass.
- Cleanup does not remove active, recent, or high-value memory unexpectedly.
- Lightweight usage bookkeeping updates active-memory metadata inline so the nightly pass can make decisions from fresh recency data.

### Phase 6 — Testing and Documentation

- [x] Add unit tests for categorization, scoping, and retrieval filtering.
- [x] Add tests for repo-switch and space-switch behavior.
- [x] Add tests for stale-memory detection and cleanup behavior.
- [x] Update this document whenever a milestone is completed.
- [x] Add or update developer-facing documentation for the new memory model.

### Phase 7 — HQ Shared-Memory Infrastructure and Rollout

- [ ] Implement shared playbook promotion from local nodes to HQ.
- [ ] Implement shared anti-pattern and failure-memory promotion and retrieval.
- [ ] Implement cross-node retrieval and ranking for shared patterns.
- [ ] Implement versioned language-KB distribution and update pipelines.
- [ ] Stand up HQ API, storage, auth, logging, and observability for the shared layer.
- [ ] Define capacity planning, traffic budgets, and scaling rules for the initial rollout.

Acceptance criteria:
- Shared patterns can be promoted from local usage into HQ and retrieved by future nodes.
- The HQ service can serve retrieval requests with acceptable latency for the V1 beta audience.
- Curated KBs can be published centrally and distributed to client nodes without manual per-host updates.

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
