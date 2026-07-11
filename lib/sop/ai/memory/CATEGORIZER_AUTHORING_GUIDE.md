# Categorizer Authoring Guide

This guide explains how to author a categorizer that is aware of the current taxonomy and can optionally use an LLM as a fallback or batch-assistance layer.

## Core principle

A categorizer should generate categories from the current taxonomy, not from unrelated storage structures such as UUID indexes, vector indexes, or item collections.

Think of it like building index cards in a library: each card belongs in a shelf, a drawer, and a section, and the author decides where it fits by looking at the existing structure, the density of nearby cards, and the way the hierarchy is growing.

The most useful input for generation is the current category path view, especially the existing hierarchy exposed through the path index.

## Recommended authoring model

A categorizer should usually follow this flow:

1. Inspect the current taxonomy context and understand the existing hierarchy.
2. Prefer existing parent/child branches when possible.
3. Consider density and item counts to decide whether the current branch is crowded enough to justify a new child or a new parent.
4. If confidence is low, optionally consult an LLM for a suggested category or a set of categories.
5. Return the best-fitting path, or a broader parent path if the signal is weak.

## Context available to the categorizer

The manager injects a lightweight context object into categorizers that opt in to it. The author should treat this as a read-only view of the current taxonomy.

The context should be used to:
- discover existing category paths,
- prefer paths that align with the current hierarchy,
- inspect item counts per branch to estimate density,
- decide whether to keep a concept under an existing parent, create a child, or promote it to a new parent,
- avoid inventing duplicate or disconnected categories,
- propose a new category only when it fits the taxonomy well.

## Authoring options

### Rule-based categorizer

Use this when the taxonomy is well known and the mapping is stable.

Good fit for:
- explicit domain rules,
- stable naming conventions,
- inherited category paths.

### Hybrid categorizer

Use this when you want a deterministic fast path plus LLM assistance for ambiguous cases.

Good fit for:
- evolving domains,
- partially known taxonomies,
- category sets that benefit from semantic interpretation.

### LLM-assisted categorizer

Use this when the taxonomy is broad, the domain is ambiguous, or the input set is semantically rich.

Good fit for:
- batch categorization,
- new topic discovery,
- complex multi-signal inputs,
- suggestions that should stay aligned to the existing taxonomy.

## Recommended behavior for authors

- Prefer existing category paths over inventing new ones.
- Use broader parent categories when confidence is low.
- Keep generated paths consistent with the current hierarchy.
- Use density and item-count signals to decide if a branch is becoming too crowded for a single bucket.
- When a branch is dense, consider a more specific child path; when a topic is broad and cross-cutting, consider a new parent.
- Use LLM suggestions as a fallback or batch-assistance mechanism, not as a blind replacement for the taxonomy.

## Suggested authoring pattern

```go
type MyCategorizer struct {
    fallback string
}

func (c *MyCategorizer) Categorize(text string, opts memory.CategorizeOptions) (string, error) {
    // 1. Try deterministic rules.
    // 2. If low confidence, optionally ask an LLM.
    // 3. Return the best path.
    return c.fallback, nil
}
```

## Practical guidance

- Keep the logic simple and predictable.
- Make fallback behavior explicit.
- Expose confidence so the caller can decide whether to trust the result.
- Prefer category paths that are already present over newly minted ones.

## Summary

The best categorizer authoring experience is:
- taxonomy-aware by default,
- deterministic first,
- LLM-assisted second,
- and explicitly guided by the current category paths.
