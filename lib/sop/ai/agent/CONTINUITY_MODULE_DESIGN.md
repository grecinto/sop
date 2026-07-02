# Shared Continuity Pipeline Module Design

## Objective
Make continuity a general pipeline capability that improves every capability that uses memory, prompt context, or multi-turn state. The goal is to keep the agent coherent across Ask, coding workflow, stores, spaces, and future domains without forcing each capability to invent its own continuity behavior.

## Architectural layering
The shared workflow module and the coding workflow capability should be treated as separate layers.

- **Shared workflow module**: owns task-mode classification, context packing, planning, execution-result shaping, and continuity hooks.
- **Coding workflow capability**: a domain adapter that invokes the shared workflow for code, patch, repo, generation, and analysis tasks.
- **Stores and Spaces capabilities**: other domain adapters that can opt into the same workflow engine for their own generation-oriented and analysis-oriented tasks.

This keeps the shared workflow engine general and reusable instead of making it a coding-only feature.

## Proposed module shape
A shared module should live under the pipeline layer rather than inside Ask alone. A natural home is:

- `ai/pipeline/continuity`

This module should be responsible for continuity decisions, state shaping, prompt hydration, and fallback reconstruction while leaving provider-specific continuation behavior to the transport layer.

## Core responsibilities
The shared continuity module should own four responsibilities:

In addition, the shared workflow layer should classify work into reusable task modes so continuity and planning are consistent across domains:

- Code: edit, patch, and test workflows.
- Generation: compose structured outputs like JSON or LEGO-style scripts.
- Analysis: explore records, chunk data, and derive a solution over multiple turns.

This makes the same continuity and carryover behavior available to coding workflow, Stores, Spaces, and future capabilities without each one inventing its own model.

1. Continuity state management
   - maintain a compact, transport-agnostic continuity state object;
   - track the last meaningful user/assistant exchange, facts, learned recipes, tool patterns, and topic context.

2. Continuity policy
   - decide whether continuity is off, compact, or live;
   - reset when the topic changes, the provider changes, the KB changes, or the budget/TTL is exceeded.

3. Prompt and memory hydration
   - assemble a compact continuity block for prompt construction;
   - feed the same state into MRU, STM, and LTM-oriented context layers without duplicating logic.

4. Capability signal integration
   - allow each capability to contribute domain-specific signals such as task outcomes, repo edits, store facts, or space-specific learnings;
   - keep those signals lightweight and structured so the shared layer can reason over them consistently.

## Suggested concepts
The module can be expressed around a few simple concepts:

- ContinuityState
  - the canonical persisted state for the current thread or session;
  - includes mode, provider metadata, topic fingerprint, summaries, and lightweight outcome signals.

- ContinuitySignal
  - a small structured event emitted by a capability after a turn completes;
  - examples include user intent, confirmed fact, tool pattern, task outcome, or learned recipe.

- ContinuityDecision
  - the output of the policy layer;
  - determines whether to use no continuity, compact continuity, or live continuation.

- ContinuityEnvelope
  - the prompt-facing payload assembled for the next turn;
  - is the shared input to Ask and other capabilities.

### Coding-workflow memory payloads
The coding workflow should not persist only a loose summary string. It should emit a structured payload that can be written into LTM and retrieved through two distinct index routes that both resolve to the same canonical document.

The canonical document should be the single source of truth for the full payload:

- `workspace_id` / `repo_id`
- `module_path`
- `relative_path`
- `active_file`
- `structural_path`
- `task_type`
- `description`
- `ast_leaf_nodes`
- `symbol_name`
- `symbol_kind`
- `imports`
- `related_tests`
- `outcome_status`
- `files_touched`
- `lesson`
- `next_step`
- `evidence_snippet`
- `test_summary`
- `semantic_categories`
- `category_path`

The storage layer should then maintain separate index references to that document:

- a deterministic path index, such as `a/b/c -> document_id`
- a semantic nested-category index, such as `x/y/z -> document_id`

This keeps the payload canonical while allowing both retrieval modes to work. The indexes should contain lightweight references rather than duplicated full payloads so updates remain consistent and storage stays compact.

## Runtime flow
The intended flow is:

1. A capability completes a turn and emits a continuity signal.
2. The shared continuity module updates the continuity state.
3. The module evaluates policy and chooses a continuity mode.
4. The module builds a continuity envelope for the next turn.
5. The prompt assembly path for Ask and other capabilities consumes that envelope.
6. If provider continuation is available, it is used as an enhancement; otherwise the module falls back to memory-based continuity.

This keeps provider-specific behavior as a transport concern while the shared module remains the semantic continuity authority.

## Integration strategy
### Ask
Ask should be the first consumer because it already has a mature continuity pattern and clear prompt integration points.

### Coding workflow
The coding workflow should contribute task outcomes, repo-edit learnings, and generation/analysis artifacts into the shared continuity stream rather than maintaining a separate memory path.

For coding workflow, LTM access should support two retrieval modes:

1. **Path-based retrieval** using category paths derived from repo structure and AST leaves.
   - This is a direct, deterministic lookup route and is not vectorized.
   - Best for exact file/symbol recall, same-module reuse, and deterministic follow-up edits.
   - Example path shape: `repo/module/file/symbol`.

2. **Semantic-category retrieval** using nested conceptual categories that cross-cut files and modules.
   - This is a semantic, vectorized retrieval route that uses category meaning rather than exact path identity.
   - Best for planning, debugging, refactoring, and cross-file pattern recall.
   - Example categories: `coding-workflow`, `ast-context`, `parser-repair`, `test-fix`, `refactor`.

The workflow should write one canonical payload and register it under both retrieval surfaces:
- a deterministic path-based index, and
- a semantic nested-category index.

Both indexes should point back to the same document ID. This keeps LTM retrieval aligned with the two different ways developers think about code: by location and by concept.

### Stores and spaces
These domains should use the same module so they inherit the same continuity semantics and avoid parallel continuity implementations. Their generation-oriented and data-analysis-oriented tasks can reuse the same workflow planning and continuation model as coding.

## Design principles
- Continuity should improve the system broadly, not just the Ask experience.
- The shared layer should stay lightweight and explicit so it can be inspected and reasoned about.
- Memory and prompt construction should be aligned so continuity is visible across turns.
- New capabilities should be able to opt into the same model instead of inventing their own patterns.

## Non-goals
This module should not:
- replace the existing KB, STM, LTM, or MRU systems;
- become a generic long-term memory store for all raw history;
- force live provider continuation on every path.

## Migration posture
The design should be introduced as an additive layer first:

1. Document and stabilize the shared continuity concepts.
2. Route Ask through the shared module while preserving current behavior as a fallback.
3. Introduce continuity signals from coding workflow and other capabilities.
4. Gradually replace Ask-specific continuity logic where the shared layer is clearly stronger.

## Open questions
- How much signal should each capability contribute before the prompt becomes noisy?
- How should continuity state be versioned as the schema evolves?
- What is the right balance between compact summaries and richer but more expensive continuity payloads?
