# Continue Token Optimization Plan

**Status**: Proposed  
**Last Updated**: 2026-08-06

## Goal

Achieve Continue- and Cursor-class token savings while keeping SOP Ask as the sole orchestrator.

The architecture rule does not change:

- Continue remains the editor-facing client and delegated worker.
- The daemon Ask pipeline remains the controller.
- Provider-specific caching and continuation behavior remain owned at the provider seam.

## Why This Plan Exists

The current native Continue integration already gives SOP structured editor context without needing an OpenAI-compatible `messages[]` passthrough.

Current Continue request shape already carries the main optimization inputs:

- `message`
- `provider`
- `model`
- `workspace.root`
- `workspace.active_file`
- `workspace.selection`
- `context.selected_text`
- `context.open_files`
- `tools`
- `tool_choice`

That means SOP can implement the same token-saving effects as Continue-style proxy heuristics, but on the native `/api/continue/*` contract.

## Non-Goals

- Do not restore `/v1/chat/completions` as the canonical integration surface.
- Do not make Continue the outer workflow controller.
- Do not move memory, approvals, or phase progression into the bridge client.
- Do not replace provider-owned prompt caching with proxy-owned prompt replay.

## Architecture Position

### What stays the same

- Continue sends structured IDE context to the daemon.
- The daemon converts that request into Ask input.
- Ask still classifies, routes, phases, and orchestrates capabilities.
- Delegated Continue execution stays bounded and request-scoped.

### What changes

- The bridge adds token-efficiency normalization before prompt assembly.
- The daemon prefers compact edit/result contracts instead of whole-file rewrites.
- Prompt stability is made explicit so provider caching stays effective.

## Optimization Strategy

### 1. Native model tiering

Use request metadata and lightweight heuristics to choose provider/model before Ask execution while preserving the daemon as controller.

Candidate signals:

- short explanation or summary requests
- single-file questions with a non-empty `selected_text`
- explicit review, architecture, debugging, or multi-file intent
- Continue mode and requested phase
- tool bounds such as `tool_choice` and available tools

Routing policy:

- cheap model for narrow explanation, summarization, and low-risk local transformations
- premium model for architecture, ambiguous debugging, multi-file reasoning, or heavy workflow steps
- explicit user-selected provider/model still wins when policy says not to override

Implementation seam:

- native Continue request normalization before `buildContinueBridgeEnvelope(...)`
- provider/model propagation still flows through Ask variables and provider details

Guardrail:

- this is request shaping, not a second controller

### 2. Patch-first delegated editing

Avoid paying whole-file prompt cost for localized edits.

Policy:

- prefer unified diff, search/replace blocks, or narrow structural edit instructions
- send `selected_text`, active file, selection range, and bounded open-file context before broad file bodies
- require delegated result contracts to return `modified_files`, `diff_delta`, and artifact refs for large diffs

Existing support already present:

- `result_contract.include_modified_files`
- `result_contract.include_diff_deltas`
- `result_contract.prefer_artifact_refs`
- `result_contract.max_inline_diff_bytes`

Needed hardening:

- bias Continue delegation toward patch-style outcomes in capability instructions
- avoid prompting the model to rewrite a full file when the requested change is local

### 3. Scoped native context trimming

Token reduction should happen before prompt text is expanded.

Use native fields, not raw transcript parsing.

High-value context order:

1. `selected_text`
2. active file path
3. selection range
4. open files
5. Continue tool metadata

Trimming rules:

- drop duplicated path mentions
- collapse oversized open-file lists to the highest-signal subset
- prefer exact selection over whole-file content
- strip obviously low-signal generated context when the bridge later gains richer file payloads
- keep comments and whitespace trimming conservative unless the request explicitly asks for code transformation only

Guardrail:

- do not silently discard user-provided context that materially changes meaning

### 4. Stable prompt prefix for provider caching

Prompt caching should remain provider-owned, but bridge-assembled prompt order must help it.

Stable prefix order:

1. system prompt
2. core SOP instructions
3. stable tool definitions
4. stable memory or policy blocks
5. volatile retrieval/context blocks
6. immediate user request

Current fit:

- Anthropic prompt caching already exists at the provider seam
- Continue bridge prompt assembly should avoid injecting volatile context ahead of stable instructions

Needed hardening:

- document and test ordering expectations for Continue-originated requests
- keep request-specific file context at the bottom of the assembled prompt shape

## Feasibility Assessment

### Can we match the effect without losing orchestration?

Yes.

The token-saving effect does not require surrendering controller ownership. It requires:

- better request normalization
- better tool/result contracts
- better prompt assembly discipline
- provider-aware caching alignment

Those changes fit the current architecture cleanly.

### What we should not copy literally

- OpenAI-style `messages[]` proxy dependency
- frontend-owned continuation logic
- controller logic in the Continue client
- provider-specific façade contracts as the main product boundary

## Implementation Plan

### Phase 1: Bridge normalization

Target files:

- `lib/sop/cmd/sop-daemon/client_bridge.go`
- `lib/sop/cmd/sop-daemon/rag.go`

Work:

- add a Continue request normalization step before prompt/envelope assembly
- add a lightweight model-tiering decision helper that respects explicit provider/model intent
- trim oversized open-file context before it becomes prompt text
- preserve full fidelity in structured request state even when prompt text is trimmed

Success criteria:

- no transport contract regression
- provider/model still propagate through Ask and worker delegation
- normalized requests produce smaller prompt text for narrow IDE asks

### Phase 2: Delegation contract tightening

Target files:

- `lib/sop/cmd/sop-daemon/client_bridge.go`
- `lib/sop/ai/agent/domains/ideworker/capability.go`

Work:

- strengthen Continue delegation instructions to request patch-first outputs
- prefer bounded edit objectives over whole-file rewrite framing
- ensure fallback execution preserves the same compact result contract

Success criteria:

- delegated edit responses return compact diffs for local changes
- large edits use artifact refs instead of bloated inline payloads

### Phase 3: Prompt-order validation

Target files:

- `lib/sop/cmd/sop-daemon/main_test.go`
- provider-specific caching tests when needed

Work:

- add tests for Continue-originated request shaping and prompt ordering
- add tests ensuring volatile editor context appears after stable instructions
- verify provider/model selection still reaches Ask config and worker payloads

Success criteria:

- test coverage pins prompt-order assumptions and routing behavior
- future refactors cannot accidentally defeat provider caching

### Phase 4: Telemetry and refinement

Work:

- measure prompt size before and after normalization
- measure cheap-model hit rate vs premium-model escalation rate
- capture delegated result sizes for inline diffs vs artifact refs
- tune heuristics only after real request traces exist

Success criteria:

- evidence-backed threshold tuning
- no architecture drift toward proxy-owned orchestration

## Risks

### Risk: over-aggressive trimming

If trimming removes decisive context, answer quality drops.

Mitigation:

- trim only derived or low-signal context first
- prefer selection-first reduction before content elision
- preserve structured request fields even when prompt text is compressed

### Risk: silent model override surprises

If a user explicitly selected a model, automatic tiering can feel incorrect.

Mitigation:

- treat explicit provider/model as authoritative unless policy explicitly allows downgrade/upgrade
- log or surface routing decisions in metadata for debugging

### Risk: prompt caching regression through prompt reordering

Volatile context inserted above stable instructions will reduce cache reuse.

Mitigation:

- pin ordering in tests
- keep Continue-specific context injection close to the bottom of the assembled prompt

## Resume Checklist

When resuming this work after a refresh or reboot:

1. Re-read this document.
2. Re-read `docs/CONTINUE_FACADE_ARCHITECTURE.md` for the control boundary.
3. Inspect `buildContinueBridgeEnvelope(...)` and `buildContinueBridgePrompt(...)` in `lib/sop/cmd/sop-daemon/client_bridge.go`.
4. Inspect `continueChatRequest` in `lib/sop/cmd/sop-daemon/rag.go`.
5. Confirm Anthropic prompt caching behavior in `lib/sop/ai/generator/anthropic.go`.
6. Validate that no work item reintroduces `/v1/chat/completions` as the primary bridge.

## Validation Targets

Use these checks once implementation begins:

```bash
export GOTELEMETRY=off && go test ./lib/sop/cmd/sop-daemon
```

```bash
export GOTELEMETRY=off && go test ./lib/sop/ai/agent/domains/ideworker
```

## Related Docs

- `docs/CONTINUE_FACADE_ARCHITECTURE.md`
- `docs/CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md`
- `docs/TOKEN_CONSUMPTION_ANALYSIS.md`
- `docs/TOKEN_CONSUMPTION_SUMMARY.md`
- `docs/LLM_ENHANCEMENT_PLAN.md`