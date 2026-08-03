# LLM Enhancement Plan

**Date**: 2026-08-02  
**Status**: PROPOSED  
**Goal**: Strengthen provider support and model compatibility without weakening Ask ownership, provider-owned loop boundaries, or the participant-pattern architecture

## 1. GPT / OpenAI Assessment

This section preserves the GPT / OpenAI comparative analysis so it remains part of the design record.

Continue's GPT support is stronger as a provider adapter.

- It has a broad OpenAI compatibility layer.
- It normalizes model quirks across Chat Completions, Responses, and OpenAI-like routers.
- It remaps `system` to `developer` for `o` and `gpt-5` families.
- It handles more transport-level vendor variation.

SOP's GPT support is stronger as an agent runtime.

- Ask is the controller.
- The ChatGPT loop is a first-class provider-owned ReAct loop.
- Tool execution, retry, clarification, carryover, and hydration are owned server-side.
- The loop is bounded, testable, and aligned with the participant pattern.

The key difference is architectural:

- Continue distributes tool-loop control across chat/UI recursion and follow-up tool execution.
- SOP keeps orchestration in the Ask pipeline and provider-owned server loops.

Conclusion for GPT support:

- Continue is a good source of transport and compatibility ideas.
- SOP already has the stronger architecture for agentic execution.
- Improvements should focus on compatibility hardening, not importing Continue's UI-driven control flow.

### Design Decision

The architecture direction remains:

- Ask pipeline owns orchestration.
- Provider-owned loops remain the default for providers with real native outer-loop semantics.
- Compatibility improvements should be adopted without importing UI-driven control flow.

## 2. Anthropic Assessment

Continue's Anthropic support is strong as a message-formatting and streaming adapter.

- It has mature conversion between internal chat messages and Anthropic message blocks.
- It supports Anthropic-native content block types such as `tool_use`, `tool_result`, `thinking`, and `redacted_thinking`.
- It handles streamed partial tool-call JSON and emits tool-call deltas incrementally.
- It supports prompt caching at the transport layer for recent conversation and system message reuse.

SOP's Anthropic support is stronger as an agent runtime and provider-owned loop.

- Anthropic is already a provider-owned ReAct loop in SOP.
- Tool execution, retry, clarification, hydration, and carryover stay server-side.
- Prompt caching is treated as a first-class provider behavior in the generator, not as a UI concern.
- Message replay and tool-result continuity are owned directly at the provider seam.
- The Anthropic loop reuses the shared retry kernel without giving up provider-specific outer-loop control.

The key difference is architectural:

- Continue is optimized to adapt Anthropic's message protocol into a generic app chat loop.
- SOP is optimized to preserve Anthropic-specific semantics inside a provider-owned Ask/ReAct runtime.

Conclusion for Anthropic support:

- Continue is a good source of ideas for transport polish, streaming details, and block-shape compatibility.
- SOP already has the better architecture for Anthropic as an agent provider.
- Anthropic work should focus on compatibility hardening, caching refinement, and test coverage rather than architectural change.

### What To Borrow From Continue For Anthropic

- More battle-tested block conversion edge-case handling.
- Streamed partial tool-call parsing and defensive incremental assembly patterns.
- Additional compatibility coverage for thinking and redacted thinking payloads.

### What Not To Borrow From Continue For Anthropic

- UI-recursive orchestration.
- Frontend-owned continuation logic.
- Any flow that weakens provider-owned loop boundaries.

### Design Decision

Anthropic should remain a provider-owned loop in SOP.

The right work is:

- preserve the current provider-owned outer loop
- refine compatibility and transport handling
- expand tests around streaming tool-use blocks, prompt caching, and continuation replay

## 3. Gemini Assessment

Continue's Gemini support is strong as a provider adapter with practical protocol workarounds.

- It has mature request shaping for Gemini's native content model.
- It handles Gemini function-calling payloads and function responses in the provider-specific request format.
- It carries compatibility logic for API-version differences, including system-message handling when native system instructions are limited.
- It preserves Gemini-specific thought-signature details in tool-call flows.
- It supports streaming parsing for mixed text and function-call parts.

SOP's Gemini support is stronger as an agent runtime and provider-owned loop.

- Gemini is already a provider-owned ReAct loop in SOP.
- Continuation state is preserved explicitly through `ToolCallContinuations` rather than flattened back into plain prompt text.
- Gemini-native function-call IDs and thought signatures are carried through the loop for transport continuity.
- The loop includes retry guidance, repair directives, clarification handoff, and carryover state as first-class server-side concerns.
- Gemini-specific continuation prompts and final-turn prompts are already shaped around Ask/ReAct behavior rather than generic chat replay.

The key difference is architectural:

- Continue is optimized to adapt Gemini's protocol into a generalized chat runtime.
- SOP is optimized to preserve Gemini-native continuation semantics inside a provider-owned Ask/ReAct loop.

Conclusion for Gemini support:

- Continue is a good source of ideas for provider-specific compatibility handling and stream parsing.
- SOP already has the better architecture for Gemini as an agent provider.
- Gemini work should focus on compatibility hardening, continuation fidelity, and tests rather than architectural change.

### What To Borrow From Continue For Gemini

- Additional edge-case handling for Gemini API-version differences.
- More defensive parsing of mixed streamed text and function-call parts.
- Extra compatibility coverage for system-instruction fallbacks and provider-specific request shaping.

### What Not To Borrow From Continue For Gemini

- Generic chat-loop adaptation as the main orchestration model.
- Frontend-owned continuation handling.
- Any change that weakens Gemini's provider-owned loop semantics in SOP.

### Design Decision

Gemini should remain a provider-owned loop in SOP.

The right work is:

- preserve the current provider-owned outer loop
- refine compatibility for API-version and transport edge cases
- expand tests around thought signatures, function-call continuity, and streamed tool-call parts

## 4. Deferred Note: Ollama

Ollama is not an active enhancement priority.

- It may remain useful for embeddings or simple local chat.
- It is not currently worth prioritizing as a serious Ask/ReAct provider.
- No provider-owned Ollama loop is planned.

If Ollama support is revisited later, treat it as a narrow transport problem rather than a major runtime target.

## 5. What To Borrow From Continue

- OpenAI transport normalization for model-family quirks.
- Responses-vs-chat fallback behavior.
- More complete OpenAI-compatible router handling.
- Anthropic block-shape and streaming edge-case handling.
- Gemini request-shaping and stream-parsing edge-case handling.
- Native tool-call parsing and provider-specific defensive handling.

## 6. What Not To Borrow From Continue

- UI-recursive tool orchestration.
- Frontend-owned continuation logic as the controller.
- Provider integrations that flatten agent control flow into repeated chat calls.

## 7. Problem Statement

SOP's current provider stack is architecturally strong, but uneven in provider maturity.

The main gaps are:

- OpenAI / ChatGPT support can be hardened further for broader model-family and router compatibility.
- Anthropic support can be hardened further around transport compatibility, streaming edge cases, and provider-specific tests.
- Gemini support can be hardened further around API-version compatibility, stream parsing, and continuation-fidelity tests.
- Provider capabilities are not yet expressed as clearly as they should be in planning and implementation docs.
- Some provider-specific transport logic could be made more explicit without collapsing provider-owned loops into a shared controller.

## 8. Goals

- Improve provider compatibility without weakening Ask ownership.
- Improve Anthropic transport compatibility without changing its provider-owned loop status.
- Improve Gemini transport and continuation compatibility without changing its provider-owned loop status.
- Preserve provider-owned loops for providers that genuinely need them.
- Expand test coverage around provider quirks, native tool calls, and continuation behavior.
- Keep the implementation LLM-as-Orchestrator-first.

## 9. Non-Goals

- Do not replace provider-owned loops with a universal shared outer loop.
- Do not move tool orchestration into a frontend or bridge layer.
- Do not add controller-pattern behavior to capabilities.
- Do not overfit SOP to Continue's app architecture.
- Do not prioritize Ollama as an active provider enhancement target.

## 10. Current Architectural Position

The current architecture already has the right top-level shape.

- ChatGPT uses a provider-owned loop because the Responses API provides meaningful native continuation and tool semantics.
- Gemini uses a provider-owned loop because it has provider-specific continuation shaping and multi-tool behavior.
- Anthropic uses a provider-owned loop because prompt caching, message replay, and hydration timing matter.
- The shared-compatible path remains available for providers that can work with a simpler transport and continuation model.

This means LLM enhancement should focus on improving provider adapters and loop boundaries, not rewriting the orchestration model.

## 11. Enhancement Strategy

### Implementation Status Update

The following hardening work is now implemented in the generator layer:

- ChatGPT / OpenAI
	- official GPT-5 and `o`-series chat-completions developer-role normalization
	- chat-completions tool-definition support in the single-turn path
	- chat-completions tool-call parsing in the single-turn path
	- explicit `/responses` to `/chat/completions` fallback for custom routers when `/responses` is unsupported
	- coverage for both blocking and streamed custom-router fallback behavior

- Anthropic
	- normalized configurable base URL handling for the messages endpoint
	- real transport tests for text and tool-use responses
	- mixed text plus `tool_use` parsing coverage
	- tool-schema sanitization for safer provider compatibility

- Gemini
	- normalized configurable base URL handling for `generateContent`
	- legacy `v1` system-prompt fallback that folds system instructions into the user prompt when needed
	- mixed text plus function-call parsing coverage

- Provider runtime capability metadata
	- explicit runtime capability contract encoded in the shared AI interface layer
	- ChatGPT, Anthropic, Gemini, and fallback now advertise loop mode, native continuation, carryover, multi-tool, and streaming characteristics explicitly
	- agent runtime now consumes that metadata for carryover and loop-mode resolution
	- diagnostics/reporting now expose provider runtime summaries in Ask and carryover logs

The following continuation-sensitive protections were explicitly revalidated after these changes:

- ChatGPT server-side carryover and `previous_response_id` continuation
- Anthropic continuation replay and carryover restore
- Gemini tool-call continuation, carryover, and `thought_signature` continuity

### A. OpenAI / ChatGPT Compatibility Hardening

Enhance the ChatGPT provider in the following ways:

- Expand model-family handling for `o` and `gpt-5` variants. ✅
- Make Responses-vs-chat fallback rules more explicit and testable. ✅
- Improve support for OpenAI-compatible routers and custom base URLs. ✅
- Keep native tool-call extraction robust when proxies return partial or degraded formats.
- Preserve current server-owned ReAct loop design.

Expected outcome:

- better compatibility across OpenAI and OpenAI-like deployments
- fewer transport-specific regressions
- no architectural downgrade in loop ownership

### B. Anthropic Compatibility And Test Hardening

Enhance Anthropic support in the following ways:

- harden streaming parsing for `tool_use`, `tool_result`, `thinking`, and partial JSON argument flows
- expand tests for prompt caching boundaries and continuation replay. Partially complete.
- improve compatibility handling for evolving Anthropic model constraints. Partially complete.
- normalize and sanitize tool input schemas for safer provider compatibility. Complete in current pass.
- preserve the current provider-owned loop architecture

Expected outcome:

- stronger Anthropic reliability without any outer-loop redesign
- better test coverage for provider-native semantics already in use

### C. Gemini Compatibility And Continuation Hardening

Enhance Gemini support in the following ways:

- harden stream parsing for mixed text and function-call response parts. Partially complete.
- expand tests for thought signatures, function-call IDs, and continuation replay. Partially complete.
- improve compatibility handling across Gemini API-version differences. Partially complete.
- preserve the current provider-owned loop architecture

Expected outcome:

- stronger Gemini continuity and transport reliability without outer-loop redesign
- better test coverage for Gemini-native continuation semantics already in use

### D. Provider Capability Clarity

Document and, where useful, encode provider capability distinctions more explicitly.

Important distinctions include:

- provider-owned outer-loop support
- tool-call support
- native continuation support
- live carryover support
- compact carryover support
- multi-tool turn support
- streaming semantics

This clarity helps prevent forcing providers into the wrong orchestration model.

Status: substantially complete for the core runtime path.

- An explicit provider runtime capability contract is now encoded for core generators.
- Carryover and loop-mode selection now consume that metadata.
- Diagnostics/reporting now surface the runtime summary in key Ask/carryover logs.
- Remaining work is to consume that metadata more broadly in user-facing selection or status surfaces where helpful.

### E. Shared Kernel, Not Shared Controller

Continue extracting reusable lower-level behavior where it improves consistency.

Good candidates:

- request normalization helpers
- tool schema adaptation helpers
- recoverable parsing error classification
- event normalization
- conformance test patterns

Do not extract a shared component that starts deciding end-to-end next-step control flow.

## 12. Proposed Work Phases

### Phase 1: Documentation and Capability Inventory

- record provider capability matrix
- define acceptance criteria for Anthropic and Gemini compatibility hardening
- document transport vs orchestration boundaries per provider

### Phase 2: OpenAI Compatibility Improvements

- harden model-family handling. Complete in current pass.
- make fallback rules explicit. Complete in current pass.
- expand tests for Responses, chat fallback, and textual function-call degradation. Mostly complete; further proxy edge cases remain optional refinement.

### Phase 3: Anthropic Hardening

- improve streamed block parsing and edge-case handling
- expand tests for prompt caching, tool-result replay, and continuation behavior. Partially complete.
- sanitize tool schemas for provider compatibility. Complete in current pass.
- keep orchestration on the provider-owned loop

### Phase 4: Gemini Hardening

- improve mixed-part stream parsing and edge-case handling. Partially complete.
- expand tests for thought signatures, function-call IDs, and continuation replay. Partially complete.
- keep orchestration on the provider-owned loop

### Phase 5: Capability Surface Refinement

- make provider capability metadata more explicit. Complete for the core runtime contract; broader surface adoption remains optional refinement.
- tighten docs and tests around loop selection rules
- add validation against unsupported provider/mode combinations

## 13. Risks

### Risk: Accidentally Weakening Ask Ownership

If provider work starts pushing orchestration into adapters, bridges, or UI layers, SOP loses one of its main advantages.

Mitigation:

- keep Ask as the only controller
- treat provider logic as transport and loop specialization only

### Risk: Over-Copying Continue's Architecture

Continue solves a different orchestration problem with different ownership boundaries.

Mitigation:

- borrow transport techniques only
- reject UI-recursive control flow as an architecture pattern for SOP

## 14. Acceptance Criteria

This plan is successful when:

- ChatGPT compatibility improves without changing Ask ownership.
- Anthropic compatibility improves without changing its provider-owned loop design.
- Gemini compatibility improves without changing its provider-owned loop design.
- Provider loop selection remains explicit and architecture-driven.
- Tests cover native tool-call parsing, fallback behavior, and transport quirks.
- Documentation clearly explains why GPT, Anthropic, and Gemini remain owned-loop providers.

## 15. Recommended Immediate Next Step

The next implementation steps should be:

- expand Anthropic provider-native streaming tests while preserving the current provider-owned loop
- expand Gemini edge-case tests for mixed-part and continuation fidelity while preserving the current provider-owned loop

In parallel, the next architecture-hardening step should be:

- keep ChatGPT/OpenAI compatibility refinements limited to transport/proxy edge cases without changing the current provider-owned loop model