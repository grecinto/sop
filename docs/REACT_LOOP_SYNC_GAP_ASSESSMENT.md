# ReAct Loop Sync Gap Assessment

**Assessment Date**: 2026-07-31  
**Status**: RESOLVED  
**Previous Issue**: Provider-owned ReAct loops lagged behind the shared-compatible path on progressive retry intelligence

## Resolution Summary

The original sync gap has been closed.

Progressive retry behavior is now implemented in the active provider-owned loops rather than being confined to the reserved compatible/shared path.

The provider-owned loops for ChatGPT, Anthropic, and Gemini now share the lower-level retry kernel and support:

- recoverable error classification
- progressive retry tiers
- repair directives carried into subsequent turns
- repair-step enforcement when the model attempts to switch tools mid-repair
- Tier 3 escalation state propagation
- adaptive retry budgets and retry learnings

## What Changed

### Shared Kernel

The retry kernel now lives in the shared `ai` package and is reusable by both:

- the reserved compatible/shared ReAct path
- the active provider-owned loops

### Provider-Owned Loops

The following provider-owned loops were uplifted to reuse the shared retry kernel:

- ChatGPT owned loop
- Anthropic owned loop
- Gemini owned loop

## Result

The prior state, where most users missed progressive retry behavior because they were on provider-owned loops, no longer applies.

The architecture is now aligned with the chosen design:

- provider-owned loops remain the default outer-loop model
- shared kernel logic is reused where it improves consistency
- no forced outer-loop unification is required to get retry-orchestrator improvements into production paths

## What This Resolves

- provider loops are no longer missing Tier 1/2A/2B/3 retry behavior
- repair sequencing is enforced in active provider-owned loops
- escalation metadata can now propagate from provider-owned loops

## Remaining Work

The original sync gap is resolved, but the broader roadmap still has room for future improvements such as:

- making more of the adaptive retry state LLM-visible rather than silently heuristic-driven
- formalizing the retry/recovery policy surface so advisory decisions and enforcement rails are explicit
- provider-specific optimization on top of the shared retry kernel

Those are forward-looking enhancements, not unresolved sync gaps.
