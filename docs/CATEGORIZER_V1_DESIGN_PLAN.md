# General-Purpose Categorizer v1 Design Plan

## Summary

Ship a strong, in-process categorizer for Spaces and KnowledgeBases as the v1 default path. The categorizer should be able to assign incoming content to the most relevant sub-category in a Space taxonomy using semantic signals, taxonomy structure, and lightweight lexical matching.

This v1 should not require any external application or customer-managed dependency. It should be packaged with the product and work out of the box when a Space has a taxonomy. The goal is to make well-organized Spaces immediately useful for Research, Coding workflow, manual exploration, and enrichment workflows such as turning an LLM into a specialist for a Mechanic or support KB.

From a product perspective, this is not just a separate categorizer feature. It is the default organization and routing layer that improves the existing KnowledgeBase Search APIs used by Research and the coding workflow.

## Goals for v1

- Provide a generally useful categorizer that works well for ordinary knowledge and support content.
- Make the default categorizer useful for end-user-authored Spaces with a clear taxonomy.
- Use the Space taxonomy as the primary decision scaffold.
- Keep the implementation fully in-process and deterministic enough to debug.
- Improve retrieval and workflow routing quality without making the categorizer a fragile or user-visible feature.
- Preserve compatibility with the existing categorizer interface and config model.
- Let the categorizer improve Research, Coding workflow, and manual browsing experiences without requiring a separate setup step.

## Non-goals for v1

- No external binary or developer-provided runtime dependency.
- No customer-uploaded custom categorizer plugins.
- No requirement that every item must be categorized with high confidence.
- No hard dependency on a single LLM or external service for categorization.

## Proposed v1 approach

### 1. Use embeddings as the semantic core

The categorizer should use the existing embedder to score how semantically similar the incoming text is to each category path.

This gives the system a strong signal for questions like:

- “billing issue” vs “subscription issue”
- “password reset” vs “account access”
- “deployment failed” vs “infrastructure outage”

The embedder is not treated as a perfect classifier by itself, but it is a strong semantic prior.

### 2. Use taxonomy structure as the decision scaffold

The Space taxonomy should be treated as the primary structure that the categorizer reasons over.

For each category, the categorizer should use:

- category name and path
- parent/child relationships
- optional descriptions
- optional example phrases or sample items
- optional keyword/synonym hints

This allows the system to prefer a category that is both semantically relevant and structurally appropriate for the current Space taxonomy.

### 3. Combine semantic similarity with lexical evidence

The scoring model should combine multiple signals:

- embedding similarity between input text and category representations
- lexical overlap with category names, aliases, and sample terms
- parent/ancestor bonus for categories that are semantically close but not an exact leaf fit
- penalty for over-assigning to very broad parent categories when a more specific child category is better

The final result should be a ranked list of candidate categories, not just a single hard assignment.

### 4. Return confidence and support the fallback path

The categorizer should return:

- one or more ranked category candidates
- a confidence score
- a fallback bucket when confidence is low

If confidence is weak, the runtime should fall back gracefully to:

- a parent category
- an uncategorized bucket
- the existing legacy behavior for the current KB

This protects retrieval quality and avoids overcommitting to a bad label.

## Runtime integration plan

### Primary use cases

The v1 categorizer should serve three main product experiences:

1. Research and Coding workflow
   - classify incoming knowledge into the most relevant sub-category before retrieval and routing happen
   - help Space-backed expertise become more precise for coding, review, testing, and analysis tasks

2. Manual user research
   - let a user browse or query a Space using a taxonomy that is already structured and meaningful
   - make the Space feel more like a curated knowledge base instead of a flat collection of items

3. Enrichment and specialist-mode workflows
   - allow the system to turn an LLM into a more capable specialist by grounding it in a well-categorized Space
   - support scenarios such as a Mechanic KB or support KB where the taxonomy provides the specialist framing

### Existing interface

The implementation should fit the current categorizer abstraction in the memory runtime:

- keep the categorizer interface as the runtime contract
- implement a new in-process categorizer that uses embeddings + taxonomy structure
- wire it into the existing memory manager configuration path
- make the result available to the existing KnowledgeBase Search path so Research, coding workflow, and user LLM asks all benefit automatically

This keeps the system compatible with the current code shape and avoids introducing a new dependency path.

### Configuration

The v1 categorizer should be enabled by default when a taxonomy is present and no stronger external policy is configured.

The existing config model remains the compatibility surface:

- preserve existing categorizer config fields
- keep import/export compatibility intact
- avoid exposing the feature as a primary customer-facing workflow

## Suggested algorithmic shape

### Input

- text to classify
- Space taxonomy and category paths
- optional domain or persona context
- optional example items for categories

### Candidate generation

1. Build an embedding for the input text.
2. Try the highest-signal retrieval paths first:
   - exact category-path lookup via CatByPath
   - semantic category-path matching via the triangulation-based path search
   - semantic text search on the resolved leaf category when a category is found
3. If those paths fail to produce strong candidates, fall back to a broader semantic walk over nested categories.
4. Score semantic similarity.
5. Add lexical and synonym-based evidence.
6. Promote the best-matching subtree and parent/child paths.

### Decision rule

The system should prefer the highest-scoring path that is both:

- semantically relevant
- structurally valid under the Space taxonomy

The retrieval flow should be ordered as:

1. exact CatByPath match
2. high-quality semantic category-path match
3. semantic text search on the resolved leaf category
4. parent-category fallback when the leaf category has no hits
5. nested semantic category walk plus lexical strengthening as the fallback path

If several siblings are close, use the path with the strongest combined evidence and the best fit to the current context.

## Authoring and taxonomy quality

A good categorizer depends on a good taxonomy, so v1 should assume that the Space author provides the taxonomy structure.

The categorizer should be designed to work well when the taxonomy contains:

- clear category names
- short descriptions
- a few examples or synonyms
- a meaningful hierarchy

The system should not require a perfect taxonomy to be useful. It should become better as the taxonomy improves.

## Evaluation plan

Before broad rollout, the categorizer should be evaluated with a small benchmark set of labeled examples.

Suggested metrics:

- top-1 accuracy
- top-3 recall
- fallback rate
- average confidence calibration
- retrieval uplift on a small held-out set

The first evaluation pass should focus on whether the system is meaningfully better than a fallback-only or naive keyword approach.

## Later extension: developer-supplied categorizer binaries

After v1 is stable, we can add a second phase for developer-uploaded categorizer implementations.

That later extension should:

- preserve the same runtime interface
- allow a compiled Go implementation or CGO-backed binary to satisfy the interface
- keep the feature opt-in and developer-facing
- avoid making it a requirement for normal Spaces

This later path should be treated as an extensibility mechanism, not the default product experience.

## Recommendation

Ship v1 as a strong, in-process, taxonomy-aware categorizer built from:

- embeddings for semantic matching
- taxonomy-aware ranking
- lexical evidence for precision
- confidence-based fallback

Use it as the default routing and organization layer behind the existing KnowledgeBase Search APIs. That gives Research, Coding workflow, and user LLM asks a stronger built-in experience without introducing a separate customer-facing feature surface.
