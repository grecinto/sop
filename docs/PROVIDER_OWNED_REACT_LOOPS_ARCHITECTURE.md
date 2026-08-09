# Provider-Owned ReAct Loops Architecture

**Date**: 2026-07-31  
**Status**: ACTIVE  
**Goal**: Keep provider-owned loops as the default architecture, and share only the lower-level kernel where that improves consistency without distorting provider behavior

## Executive Summary

The architecture is provider-owned first.

We do **not** force providers through one shared outer loop. Instead:

- keep provider-owned loops where the transport or control-flow semantics are provider-specific
- allow a narrow shared-loop path only where it is a natural fit
- share lower-level policy and helper code whenever that improves consistency without hurting provider efficiency

The design center is still **LLM-as-Orchestrator**. Shared logic should be extracted only after the orchestration authority and policy boundaries are clear.

## Current Runtime Shape

| Provider / Path | Default Loop Shape | Why |
|---|---|---|
| Compatible shared path | Reserved shared loop | Maintained for future providers that fit a serial one-call-per-turn shape; not the active outer loop for the currently supported provider-owned providers |
| ChatGPT | Owned loop | Responses API owns server continuation, streaming, reasoning payloads, multi-tool turns |
| Gemini | Owned loop | Provider-specific continuation shaping and multi-tool turns |
| Anthropic | Owned loop | Message replay, prompt caching, hydration timing, multi-tool turns |

## Default Rule

Provider-owned loops are the default architecture.

The shared loop is an exception for providers that naturally fit it, not the main architectural target. Today it is effectively a reserved path, kept in case a future provider matches that behavior cleanly.

## What Should Be Shared

These pieces are worth sharing across providers when possible:

- retry classification and escalation policy
- repair directive helpers
- sanitization helpers
- event normalization helpers
- common test patterns for retry and escalation semantics
- the shared ReAct orchestration loop itself, when the provider can fit the same control-flow contract

The current retry layer also includes adaptive retry budget and learned retry memory. Those should be modeled as explicit policy surfaces, with LLM-visible guidance by default and heuristic enforcement only where hard guarantees are required.

## Shared Loop + Provider/Model Profiles

The architecture should favor one shared ReAct orchestration loop with provider-specific and model-specific policy layers.

### Recommended shape

- one shared loop engine for orchestration, tool execution, and repair handling
- one adapter per provider where prompt formatting, continuation semantics, or transport details differ
- one model profile per provider/model tier that controls cost/token tradeoffs without changing the outer loop contract

### Why this helps

This allows the system to use cheaper models for simpler tasks while preserving the same control flow and tool execution behavior. For example:

- Gemini: use the same ReAct loop with different Gemini profiles such as pro versus flash
- Anthropic: use the same loop with different Claude profiles such as Sonnet versus Haiku
- OpenAI/GPT: use the same loop with different GPT profiles such as GPT-4.1 versus GPT-4.1-mini

The loop remains the same; only the policy profile changes.

### What should vary by model profile

A model profile may tune:

- maximum output tokens
- iteration budget
- tool result summarization level
- temperature and reasoning effort
- prompt compactness and instruction density
- repair/retry aggressiveness
- whether the model is allowed to plan deeply or should be steered toward short, direct tool use

### What should stay stable

The following should remain consistent across profiles:

- tool interface contracts
- retry and escalation semantics
- loop state transitions
- result normalization
- carryover/continuation handling at the architectural level

This gives the system a clear path to token conservation without forcing a separate outer-loop implementation for every model.

## Why Full Outer-Loop Unification Was Rejected

The earlier adapter design was too narrow for some providers because it assumed:

- a single tool call per iteration
- continuation state represented mainly as replayable tool turns
- no provider-native hydration or update flow
- minimal transport-specific response state

Those assumptions fit the reserved compatible shared path reasonably well, but not ChatGPT and only partially fit Gemini or Anthropic.

## Shared Loop Contract

The shared loop still has value, but its promise is narrow:

- it is for providers compatible with the shared loop model
- it is not a universal replacement for all provider-owned loops

## LLM as Orchestrator Note

Provider-owned loops are not just a transport decision. They are also where model-visible orchestration context can be preserved most naturally.

The correct direction is:

- design the loop for LAO first
- identify explicit policy points for retry/recovery behavior
- keep state tracking and persistence in shared machinery
- implement each policy point with either LLM-guided recommendations or heuristic/rules enforcement, depending on whether the point is advisory or safety-critical

This keeps the policy layer modular without letting it become a hidden controller.

## Policy Boundary Rule

If a shared component decides overall next-step control flow, it is too high in the stack.

Shared policy code may:

- classify
- validate
- recommend
- enforce bounded rules

Shared policy code should not:

- own the outer retry loop
- silently replace model choice where the model can reason adequately
- turn provider-owned loops into thin shells around a heuristic controller

## Architecture Rule Of Thumb

Use the shared loop only when a provider differs mainly in:

- prompt formatting
- generation options
- simple continuation serialization

Keep an owned loop when a provider differs in:

- transport or session lifecycle
- streaming model
- multi-tool execution semantics
- provider-native hydration timing

## Historical Note

An earlier proposal assumed a unified engine plus adapters for all providers. That is no longer the target architecture. The retained value from that work is the shared kernel extraction and the explicit shared-vs-owned loop selection model.