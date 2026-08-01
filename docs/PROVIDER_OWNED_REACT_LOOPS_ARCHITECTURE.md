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