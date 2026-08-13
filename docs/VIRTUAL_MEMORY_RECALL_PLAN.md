# Virtual Memory Recall Expansion Plan

## Objective

We want the system to feel like a real virtual memory layer, not just a compact prompt serializer with a few good retrieval hooks.

Today, the architecture is strong on:
- recent continuity
- MRU discipline
- short-term topic retention
- targeted LTM lookup when the query is direct and specific
- bounded, path-aware recall with semantic fallback
- archive-first LTM lifecycle management and storage-pressure compaction

The remaining weakness is no longer the core architecture; it is calibration and operational tuning. The system already implements a production-ready virtual memory policy: broader, bounded, multi-horizon recall that expands around the active topic without exploding token use or depending on provider-side context size.

This plan documents the final operating model and the policy choices that keep recall effective over time.

---

## Final Production Policy

The implemented system follows a strict retrieval ladder:

1. Exact category-path / exact workstream match first
2. Same-branch semantic category match next
3. Short-window STM/MRU continuity after that
4. Semantic neighborhood expansion only when exact or near-exact signals are weak
5. Long-tail stale entries excluded from active recall and archived first when they cold down

This avoids full-history scanning while still preserving broad enough memory recall for adjacent domains and semantically related tasks.

The core principles are:
- exact-path-first retrieval for structure-rich domains
- semantic fallback for approximate topic matching
- bounded candidate budgets per source layer
- stale entry pruning before active recall injection
- archive-first LTM lifecycle, with compaction under storage pressure

---

## Current State Assessment

The current design already covers the basic memory ladder:

1. MRU: short-lived, high-value working memory for the active thread
2. STM: recent episodic memory and runtime continuity
3. LTM: knowledge-base search for deeper, shared, and persistent retrieval

This is a solid base. However, the current recall behavior is still mostly:
- local and narrow for the direct query
- limited to a capped set of MRU categories
- selective STM projection
- KB search on demand with modest result limits

This produces good continuity, but not yet a robust large-memory recall pattern.

The system is better described as a compact memory system than a full virtual memory engine.

---

## Problem Statement

We need memory recall that can:
- widen around the current topic without losing prompt tightness
- promote useful prior context from recent and older work
- recover near-neighbor facts, not just exact keyword hits
- preserve long-tail continuity across related tasks and workstreams
- remain bounded and local so it works without large provider-specific context windows

In short: we need memory recall that is broader than immediate query lookup, but smarter and more constrained than naive full-history injection.

---

## Design Principles

### 1. Recall should be multi-horizon

Memory retrieval must combine:
- MRU: newest and most active working context
- STM: near-term episodic continuity
- LTM: persistent semantic memory
- Shared memory / playbook / selected-KB context: domain-specific grounding

Each layer contributes a different signal, and each signal is weighted by recency, relevance, and semantic relationship.

### 2. Expansion should be bounded

We should never inject “everything relevant” into the prompt. We should instead:
- collect a candidate set from multiple memory surfaces
- rank the candidates
- keep a small top-N slice for final prompt injection

This is how we keep the virtual memory layer high recall without becoming high noise.

### 3. Near-topic recall is intentional

When a thread is active, we should expand the retrieval neighborhood around the current topic, not just around the exact query phrase.

Examples:
- same workstream
- same store / database / repo / domain
- same capability or workflow family
- same semantic cluster or category neighborhood

### 4. Semantic index and category-path retrieval are a first-class recall layer

We should explicitly use the semantic index in LTM for nested category retrieval and path-aware recall where applicable.

This means our retrieval flow should favor:
- exact category-path lookups first
- nested hierarchical semantic fallback when the exact path is missing or approximate
- semantic category neighborhood expansion within the active category hierarchy
- path-scoped recall for repo/workflow/domain contexts where the path is meaningful

This is materially different from a generic raw-text search. It preserves structure and improves relevance for memory retrieval in domains like code, workflows, operations, and project-specific knowledge.

Examples:
- "Engineering/Databases/Postgres" should resolve via path-aware lookup before broad text similarity
- a query like "payment retry pattern" should explore the semantic neighborhood around “Payments/Retry/Recovery” rather than only matching the literal phrase
- for coding memory, repo and code-path recall should resolve through nested category paths and semantic anchors when the category path is approximate

### 5. Memory should be rank-based, not all-or-nothing

The system should decide memory recall using a score, not a binary “use it or ignore it” rule.

Suggested factors:
- recency score
- semantic similarity score
- topic affinity score
- source priority score
- workstream continuity score
- outcome usefulness score

### 5. Memory should support progressive recall

We should support a layered recall ladder:
- Tier 1: exact active context
- Tier 2: recent adjacent facts
- Tier 3: longer-term semantically related memories
- Tier 4: background LTM harvest for topic widening

This avoids needing to stuff all memory into a single prompt at once.

### 6. Recall should be bounded and pressure-aware

The memory system must remain cheap and safe even after long runtime and heavy compaction. Operational rules:
- do not scan the whole LTM on every Ask turn
- limit per-source candidate counts before ranking
- drop stale or cold entries before final recall shaping
- treat archive and compaction as lifecycle operations, not last-minute cleanup
- trigger earlier compaction when storage pressure rises

This is the effective production guardrail that keeps the virtual memory layer from drifting into unbounded memory scans.

---

## Proposed Architecture

### Memory Recall Graph

We introduce a bounded recall graph alongside the current prompt assembly flow.

The graph should contain:
- memory nodes: MRU items, STM episodes, LTM hits, playbook entries, selected-KB memory
- links: same topic, same project, same domain, same workflow, same semantic neighborhood
- metadata: recency, source, scope, confidence, category, workstream ID

This gives us a way to traverse memory neighborhoods rather than retrieving only from exact query text.

### Recall Pipeline

1. Capture active context
   - current query
   - active workstream / thread
   - active KB / selected space
   - current database / domain / store context

2. Gather candidate memories
   - session MRU
   - STM snapshot
   - LTM semantic search
   - playbook / shared memory / selected-KB context

3. Expand around winners
   - same category path
   - same domain / workflow cluster
   - same semantic neighbors
   - same workstream or previous related ask

4. Rank and cut
   - keep top N across all sources
   - preserve source diversity
   - prevent over-concentration from one layer

5. Inject into prompt
   - short summary block for continuity
   - top-ranked memory evidence
   - explicit “this was recalled from memory” signal for transparency

---

## Implementation Plan

### Phase 0: Baseline Measurement and Guardrails

Add memory recall telemetry before changing behavior.

Metrics to track:
- MRU hit rate
- STM projection hit rate
- LTM recall hit count per query
- per-source recall composition
- expansion ratio (how many candidates were considered vs kept)
- prompt budget spent on memory context
- near-topic success rate over a small eval set

This creates a measurable baseline and prevents blind growth.

#### Deliverables
- recall instrumentation hooks in the prompt assembly path
- memory source stats emitted in logs
- a small eval harness for topic continuity / recall quality

---

### Phase 1: Unified Recall Ranking Layer

Create a single recall ranking function that combines MRU, STM, and LTM results into a single candidate set.

#### Design
- Input: current query, active thread, selected KB, active domain, workstream state
- Output: ranked memory snippets with metadata

#### Retrieval ordering rule
The recall ranking should not treat all sources equally. It should use a retrieval priority ladder:
1. exact category/path match
2. exact nested semantic category match
3. semantic neighborhood in the same category tree
4. recent MRU/STM continuity
5. broader LTM semantic candidates

This preserves the strongest structured signals first, while still allowing semantic expansion when exact path information is missing or partial.

#### Scoring formula

A simple first version can use:

score =
  4.0 * recency
+ 3.0 * semantic_similarity
+ 2.5 * topic_affinity
+ 2.0 * source_priority
+ 1.5 * workstream_continuity
+ 1.0 * outcome_usefulness

Where each factor is normalized to a bounded range.

The production version also adds a retention gating step before score-based injection:
- stale/very cold entries are filtered out
- archived items are summarized before they are carried forward
- low-value long-tail memory is compressed, not retained in full detail forever

#### Important constraint
The ranked set should be limited to a fixed number of memory snippets per layer, with a total cap.

Example:
- 6 MRU snippets
- 8 STM snippets
- 10 LTM snippets
- final prompt injection: 12-16 total memory snippets

This preserves prompt density while increasing recall breadth.

#### Operational lifecycle policy
The final system treats lifecycle cleanliness as part of recall quality:
- recent entries remain active
- moderately cold entries are archived rather than deleted immediately
- aged archived entries are pruned on compaction passes
- long-tail memory is summarized instead of kept as full noisy detail
- high storage pressure advances compaction earlier than the normal schedule

This keeps the memory system effective without letting stale memory dominate active recall.

---

### Phase 2: Topic Neighborhood Expansion

Add a deliberate “neighbor recall” pass after initial retrieval.

#### When to trigger
- active thread persists
- no strong exact hit exists
- query is semantically close to a prior topic
- the thread has a recognized domain / store / project context

#### Expansion sources
- same category path
- same DB / store / repo / workflow family
- same capability lifecycle
- same semantic cluster in KB
- recent prior work from the same thread history

This expands link-by-link rather than by dumping raw history.

#### Output
A succinct “Memory Neighborhood” block inserted into prompt assembly:

Memory Neighborhood
- current topic: payments workflow
- adjacent facts: invoice validation, failed retries, audit semantics
- recent actions: patching billing reconciliation
- prior outcomes: instrumented fallback strategy, resolved edge cases

This is much more useful than a giant raw transcript.

#### Semantic path retrieval required behavior
When a query includes a meaningful hierarchical path, or when the active context carries a clear category tree, the memory recall layer should do the following in order:

1. attempt exact lexical category-path lookup
2. if missing, attempt semantic category-path fallback
3. if a matching category is found, retrieve only within that sub-tree first
4. if still sparse, widen to sibling/neighbor categories in the same semantic neighborhood
5. only then fall back to global LTM search

This keeps large-memory recall precise when the category hierarchy is informative and still broadens when it is not.

---

### Phase 3: Episodic-to-Semantic Promotion

We should continue converting recurring useful outcomes into semantic LTM entries.

#### Goal
Make each successful ask contribute to a higher-level reusable memory object.

#### Pattern
- capture the successful final outcome
- summarize the pattern and the domain context
- store a compact semantic memory record with metadata
- link it to the workstream and category path

#### Example metadata
- category
- domain / workflow
- source ask or tool pattern
- success characteristics
- relation to previous memories

This turns one-off continuity into a reusable memory network.

---

### Phase 4: Memory Recall Budget and Compression

We need to separate recall breadth from prompt compression.

A memory budget layer should decide:
- how many total candidate items are collected
- how many are expanded from neighboring sources
- how much text is retained in the final recall block
- whether to compress older or weaker content into a summary

This keeps the memory system wide while keeping the model prompt small and stable.

Suggested policy:
- exact current facts always included
- recent items prioritized
- older domain neighbors summarized
- older weak matches compressed into a short “memory summary” block

---

### Phase 5: Eval Suite and Memory Hardening

Add a small suite of benchmark scenarios:
- follow-up questions within the same thread
- near-topic re-entry after a pause
- semantic continuation without exact keyword match
- switching between related domains
- preserving long-tail facts across multiple asks

Target evaluation properties:
- recall of relevant facts
- absence of stale/faulty memory injection
- correct continuation across topic changes
- compact output under prompt budget

---

## Required Code Touchpoints

The current implementation already has the right seams for this plan:

- Prompt assembly and LTM injection: [lib/sop/ai/agent/service.go](../lib/sop/ai/agent/service.go)
- MRU persistence and projection: [lib/sop/ai/agent/copilot_mru.go](../lib/sop/ai/agent/copilot_mru.go)
- MRU projection / rehydration logic: [lib/sop/ai/agent/copilot_core.go](../lib/sop/ai/agent/copilot_core.go)
- STM lifecycle and episodic memory: [lib/sop/ai/memory/memory_unit.go](../lib/sop/ai/memory/memory_unit.go)
- KB search and semantic retrieval: [lib/sop/ai/memory/knowledge_base.go](../lib/sop/ai/memory/knowledge_base.go)

These are the natural layers for the expansion system.

---

## Suggested First Milestone

The first milestone should be intentionally narrow and safe:

1. add a unified memory recall ranker
2. combine MRU + STM + LTM into one candidate set
3. expand near-topic retrieval by one degree
4. keep final injected memory under a fixed token budget

This gives us the immediate benefit of broader recall without destabilizing the current system.

---

## Success Criteria

We will consider this effort successful when:
- the system can recall adjacent facts even when the wording differs
- recent and related work is reintroduced without raw transcript spam
- LTM recall extends naturally from active topic rather than only direct keyword hits
- prompt memory remains bounded and stable under saturation
- memory recall remains useful across session pauses and topic re-entry

---

## Recommended Positioning

This should be framed as a product-level differentiator:

We are not just a retrieval layer or another vector index.
We are a local virtual memory system that keeps continuity, relevance, and semantic recall close to the agent without depending on cloud-provider context size or remote memory availability.

That is the core product narrative and the correct long-term direction.
