# Continue Native Transport Implementation Plan

**Status**: Active

## Goal

Keep AI Studio and the daemon as the controller while exposing Continue as an IDE-native participant for coding workflows.

The integration target is the native Continue transport already implemented under `/api/continue/*`, with explicit provider/model attributes on each request.

## Non-Goals

- Do not restore daemon-as-OpenAI transport.
- Do not add `/v1/chat/completions`, `/v1/models`, or `/api/continue/config` back as canonical surfaces.
- Do not let Continue become the workflow controller.
- Do not move workflow state, approvals, or memory ownership into the editor client.

## Current Architecture

### Controller path

```text
Continue chat or AI Studio handoff
  -> /api/continue/chat
  -> daemon Ask/ReAct runtime
  -> normal classification, Phase Factory, passive capabilities, daemon-owned tools
  -> streamed result back to Continue
```

### Delegated editor-execution path

```text
daemon Ask turn
  -> Continue capability participates like any other capability
  -> daemon LLM decides whether bounded IDE coding work belongs in Continue
  -> delegate_continue_task
  -> /api/continue/agent/execute
  -> registered Continue worker callback when capability-matched
  -> Continue completes the bounded subtask through its own Ask/tool loop
  -> daemon resumes control with the result
```

## Target Ask-Side Design

### LAO rule

Continue does not get a special side-channel around Ask.

When the prompt comes in from Continue, it should still flow through the same daemon Ask pipeline:

1. normal Ask classification
2. normal domain/phase detection
3. normal Phase Factory participation
4. normal capability/tool orchestration

Nothing special happens to the pipeline itself. The special behavior lives only in the Continue capability.

### Continue capability target

The target capability should expose one tool only:

- `delegate_continue_task`

That tool is not a low-level proxy for Continue's internal read/search/edit/navigation primitives.

Instead, it represents Continue's own local Ask pipeline as a bounded execution substrate. The daemon LLM uses it when the current slice of work is IDE-native coding that Continue should complete locally.

### BuildContext and BuildInstructions requirements

The Continue capability should be dynamic and metadata-aware.

`BuildContext()` should tell the daemon LLM:

- Continue is available as the editor-native coding orchestrator
- the current request originated from Continue when bridge metadata says so
- what phase/tool bounds apply to this request
- that the daemon remains the outer orchestrator

`BuildInstructions()` should tell the daemon LLM how to use the sole tool:

- delegate one bounded coding objective
- include phase intent and scope
- include allowed tools and preferred tool choice when present
- ask Continue to fully perform the IDE coding work and return the result
- resume daemon orchestration after the returned result

### Why this is the target

The goal is not for AI Studio to re-create Continue by composing Continue's low-level tools from the daemon side.

The goal is for Continue to successfully complete the IDE coding work it is already best at, then return the outcome so AI Studio can continue the broader platform, workflow, and multi-capability orchestration.

## Implemented

- native Continue chat transport at `/api/continue/chat`
- native Continue tool transport at `/api/continue/tools/{toolName}`
- daemon-owned session snapshots at `/api/continue/sessions/{sessionId}`
- daemon-owned request snapshots at `/api/continue/requests/{requestId}`
- worker registration at `/api/continue/worker/register`
- worker inspection at `/api/continue/workers/{sessionId}`
- metadata at `/api/continue/metadata`
- daemon-owned delegated execution at `/api/continue/agent/execute`
- transport and backend foundations for daemon-owned Continue delegation
- request-scoped execution correlation through `request_id` while keeping `session_id` as workstream identity
- typed delegated execution results persisted in daemon-owned session and request snapshots, including daemon-local fallback execution
- incremental worker NDJSON parsing with bounded diagnostic buffering so large structured delegated results do not require whole-body mirroring in daemon memory
- typed worker streamed events (including progress and file-change) persisted in daemon-owned session and request snapshots with bounded event payload fields
- current Ask-side capability scaffolding that can be evolved into the single-tool Continue participant
- Ask-visible Continue capability surface converged to a single delegated tool (`delegate_continue_task`) with lifecycle helpers kept as non-primary internal helpers
- Continue capability context/instructions/classification tightened so Continue-originated requests remain normal Ask turns and delegation stays bounded under daemon orchestration
- provider/model propagation from native Continue chat into Ask payload variables
- provider/model propagation from the delegated Continue task contract into Continue worker execution payloads
- daemon-side auto model-tier selection for Continue bridge requests when provider/model are not explicitly pinned (compact -> lower-thinking option, deliberate -> higher-thinking option)
- verified current key-handling baseline: daemon Ask and bridge contracts support one active LLM provider/key/model/url plus one optional fallback LLM provider/key/model/url, with embedder key handled separately
- completed gap assessment and implementation design for expanding from single+fallback provider key handling to a three-provider Ask pool (OpenAI, Anthropic, Gemini) while preserving backward compatibility
- Phase 1 contract foundation for three-provider Ask pool implemented: Ask and UI handoff now accept provider-keyed `llm_credentials` payloads alongside legacy single-provider fields
- deterministic precedence implemented for mixed payloads: legacy `llm_provider`/`llm_api_key`/`llm_model`/`llm_url` still win when present; provider-keyed pool is used when legacy fields are omitted
- malformed provider-pool encrypted secrets are now rejected with explicit request validation errors before streaming begins
- provider-keyed credentials are now persisted in daemon session context and reused on subsequent Ask turns when request-level provider fields are omitted
- Phase 2 runtime chain implemented in daemon Ask: ordered provider chain now resolves active provider first and then remaining credentialed providers from pool
- daemon Ask now constructs multi-provider `FallbackGenerator` chains when multiple provider credentials are available, with degraded initialization that skips invalid providers without aborting request execution
- table-driven runtime behavior coverage added for chain ordering and fallback activation paths, including legacy-primary-plus-pool scenarios
- Phase 3 secret-model foundation implemented in UI scripts: provider-keyed LLM secrets (`openai_api_key`, `anthropic_api_key`, `gemini_api_key`) are now persisted alongside legacy `brain_api_key`/`fallback_api_key` slots
- secure local-daemon encryption path extended to provider-keyed secrets, and legacy slots are auto-hydrated from provider-keyed values for backward compatibility
- setup wizard and chat settings flows now support dedicated OpenAI/Anthropic/Gemini key inputs, persist provider-keyed secret fields directly, and preserve existing payload fields and runtime behavior
- metadata flags declaring that provider/model are explicit transport attributes
- bridge endpoint bearer-token behavior validated across chat, tools, worker registration, delegated execution, and snapshot reads
- expired-session recovery UX surfaced through a shared daemon bootstrap recovery payload on protected endpoint auth failures (no Continue-specific auth scheme)
- Continue provider forwarding of `tools` and `tool_choice`
- translation of daemon `tool_call` events into native Continue `assistant.toolCalls`
- surfaced Continue IDE-native tools for problems, definitions, type definitions, references, document symbols, and terminal contents
- removal of daemon route tests and code for `/v1/chat/completions`, `/v1/models`, and `/api/continue/config`
- removal of the AI Studio UI control that opened daemon-generated Continue config

## Remaining Work

Detailed workflow-surface analysis and SDLC boundary are documented in [docs/CONTINUE_IDE_WORKFLOW_SURFACE.md](docs/CONTINUE_IDE_WORKFLOW_SURFACE.md).

Token-efficiency planning for Continue-originated requests is documented in [docs/CONTINUE_TOKEN_OPTIMIZATION_PLAN.md](docs/CONTINUE_TOKEN_OPTIMIZATION_PLAN.md).

### 2. Continue client wiring and workflow shaping

- ensure the Continue-side integration always calls the native `/api/continue/*` endpoints
- ensure the Continue-side request builder sends provider and model explicitly on chat and delegated execution requests when user intent pins the model; otherwise allow daemon auto tier routing
- ensure worker registration advertises only capabilities the editor callback truly supports
- define AI Studio workflow profiles that deliberately decide which Continue tools are exposed per request

### 2a. Three-provider API key pool expansion (new)

- add backward-compatible Ask/handoff contract fields for provider-keyed credentials (OpenAI, Anthropic, Gemini), while retaining existing `llm_provider`, `llm_api_key`, `llm_model`, and `llm_url` behavior
- extend daemon session context persistence to store provider-keyed LLM credentials instead of a single active provider tuple
- update secure UI config/secret handling so provider credentials are stored per provider key, encrypted in local daemon mode, and migrated from current primary/fallback slots
- extend runtime LLM resolution to build an ordered provider chain (active first, then other configured providers) and construct `FallbackGenerator` with multiple fallbacks
- keep current primary+fallback headers and request paths operational for compatibility while new provider-pool fields are rolled out
- update Continue auto tier-routing to prefer only providers/models that have configured credentials available for the current runtime
- add regression tests for legacy single-provider requests, existing primary+fallback flow, encrypted secret handling, and new multi-provider pool routing/failover

#### Phased rollout timeline

##### Phase 1: Contract and compatibility foundations

- completed: provider-keyed credential fields added to Ask and handoff payloads while preserving all existing single-provider fields
- completed: existing request behavior preserved when new fields are absent
- completed: decode/validation tests added for mixed legacy/new payload shapes and malformed encrypted provider-pool secrets

##### Phase 2: Daemon session persistence and runtime selection

- completed: provider-keyed LLM credentials are persisted in daemon session context and reused when request-level legacy fields are omitted
- completed: runtime provider chain now orders explicit active provider first, then remaining credentialed pool providers
- completed: Ask runtime now initializes `FallbackGenerator` with multiple fallbacks from the resolved chain
- completed: tests added for chain ordering, credential filtering, and degraded initialization paths

##### Phase 3: UI secret model and migration

- completed: provider-keyed secret persistence model added in AI settings storage path (`openai_api_key`, `anthropic_api_key`, `gemini_api_key`) while retaining legacy slots
- completed: provider-keyed secrets use existing local daemon encryption flow
- completed: legacy slot migration/hydration path implemented so stored provider-keyed secrets backfill `brain_api_key`/`fallback_api_key` when needed
- completed: explicit dedicated OpenAI/Anthropic/Gemini key input controls are wired across setup wizard and chat settings save/sync/test flows
- completed: dedicated migration and wiring regression tests cover provider-keyed persistence and legacy hydration token paths in templates/scripts
- completed: manual HQ/daemon mode validation checklist documented below
- completed: daemon advanced provisioning flow now includes Step 3 LLM/key entry (local mode progresses Step 2 -> Step 3), ensuring provider pool keys can be entered during daemon-first onboarding
- completed: Continue onboarding can hide initial API-key/pool input tab when daemon metadata reports an already-populated provider pool, reducing duplicate key-entry prompts

Phase 3 manual validation notes:

- HQ mode: enter OpenAI, Anthropic, and Gemini keys in setup wizard; finalize; reopen chat settings and confirm all three keys persist in dedicated fields after page refresh
- HQ mode: pick each provider as primary in turn and verify save succeeds without re-entering key when dedicated field is already populated
- daemon mode: repeat key-entry and refresh checks with local runtime enabled (`mode=daemon`) to confirm encrypted persistence path is used
- daemon mode: validate malformed encrypted value recovery path by clearing one dedicated field and re-saving; confirm no auth/runtime regression in chat request flow

##### Phase 4: Continue bridge routing and hardening

- ensure Continue auto model-tier routing only selects among credentialed providers/models
- add regression tests for tier-routing with partial provider availability
- document rollout completion criteria and operational fallback expectations

##### Phase 5: Bidirectional client/daemon pool sync and convergence

- define daemon session LLM pool metadata contract with `pool_version`, `pool_updated_at_ms`, and `pool_updated_by`
- add per-provider metadata (`updated_at_ms`, `deleted_at_ms`) for `openai`, `anthropic`, and `gemini` pool entries
- require clients (AI Studio and Continue bridge) to send full pool snapshot plus metadata on daemon connect/handoff and settings save
- implement deterministic merge rules so newer daemon state syncs daemon -> client, newer client state syncs client -> daemon
- return daemon-canonical merged snapshot in connect/handoff response so clients converge immediately after one round trip
- keep legacy primary/fallback fields operational by hydrating from the daemon-canonical merged pool

Bidirectional sync resolution rules:

- primary ordering key: `pool_version` (higher wins)
- secondary ordering key: `pool_updated_at_ms` (higher wins when versions tie)
- tie-breaker: lexical compare on `pool_updated_by` (stable deterministic winner)
- per-provider merge: newest provider entry wins independently, so newer OpenAI data does not overwrite newer Anthropic/Gemini data
- deletion handling: tombstones (`deleted_at_ms`) beat older non-deleted values and prevent resurrection from stale clients

Connect-time sync behavior:

- daemon newer than client: daemon returns canonical pool, client overwrites local snapshot
- client newer than daemon: daemon applies client snapshot/merge, bumps `pool_version`, returns canonical pool
- equal metadata: no-op merge, daemon still returns canonical pool for client confirmation

Clock drift and safety notes:

- treat `pool_version` as canonical ordering source; timestamps are conflict-resolution fallback only
- after any accepted merge, daemon increments `pool_version` and stamps a new server-side `pool_updated_at_ms`
- clients should never assume local wall-clock authority over daemon version ordering

#### Exit criteria and validation matrix

##### Phase 1 exit criteria

- legacy requests using only `llm_provider`/`llm_api_key`/`llm_model`/`llm_url` behave exactly as before
- mixed payloads (legacy fields + provider-keyed pool fields) are decoded deterministically with documented precedence
- invalid encrypted or malformed provider-pool secrets return explicit request validation errors

Validation:

- add focused tests under daemon Ask/handoff handlers for payload decode, precedence, and validation failures
- rerun daemon package suite: `export GOTELEMETRY=off && go test ./lib/sop/cmd/sop-daemon`

##### Phase 2 exit criteria

- daemon session context persists and reloads provider-keyed credentials per session/workstream
- runtime provider chain ordering is stable: explicit active provider first, then remaining credentialed providers
- multi-fallback generator initialization degrades gracefully when one configured provider cannot initialize

Validation:

- add table-driven tests for chain ordering, provider filtering, and degraded initialization paths
- add failover tests proving fallback progression across more than one fallback provider

##### Phase 3 exit criteria

- AI settings surfaces can store OpenAI, Anthropic, and Gemini credentials independently
- local daemon mode encrypts provider-keyed secrets before persistence and preserves unreadable ciphertext in storage
- migration from current primary/fallback slots to provider-keyed entries is idempotent

Validation:

- add frontend/script tests for migration idempotency and write/read behavior
- add backend tests for encrypted secret resolution and malformed ciphertext handling
- execute manual smoke checks in both HQ and daemon mode

##### Phase 4 exit criteria

- Continue bridge auto-tier routing chooses only models for providers with available credentials
- explicit provider/model pinning still overrides auto-tier selection
- partial pool scenarios (1 of 3 configured, 2 of 3 configured) produce deterministic routing outcomes

Validation:

- add routing tests in Continue bridge coverage for credential-aware auto-tier behavior
- rerun daemon and IDE worker suites:
  - `export GOTELEMETRY=off && go test ./lib/sop/cmd/sop-daemon`
  - `export GOTELEMETRY=off && go test ./lib/sop/ai/agent/domains/ideworker`

##### Phase 5 exit criteria

- client connect/handoff always exchanges full pool metadata and returns daemon-canonical snapshot
- daemon/client conflict resolution follows documented deterministic ordering and per-provider merge behavior
- stale client snapshots cannot overwrite newer daemon entries
- deleted provider entries are preserved via tombstones and are not resurrected by stale payloads

Validation:

- add daemon tests for version-first ordering, timestamp fallback ordering, and `pool_updated_by` tie-break behavior
- add daemon tests for per-provider merge with mixed recency across providers
- add daemon tests for tombstone precedence over stale non-deleted values
- add client integration tests verifying connect-time daemon -> client and client -> daemon convergence

##### Rollout complete definition

- all phase exit criteria above are green in CI and local verification
- no regressions in existing Continue native transport endpoints and auth-protected flows
- plan status in this document is updated from active rollout to completed for section 2a

### 3. Authentication hardening

- no additional Continue-bridge-specific auth gaps are currently open after bearer coverage and expired-session recovery validation; keep endpoint coverage in regression suites

### 4. Session and workflow polish

- expand daemon-owned session snapshots when more editor state is useful
- add explicit request TTL and cancellation reconciliation across daemon restarts when a durable execution ledger is introduced
- keep Continue delegation steering focused on bounded execution and review slices
- extend tests around worker capability mismatch and local fallback behavior as needed
- consider surfacing low-risk remaining IDE context primitives such as open files, pinned files, and signature help

### 5. Broader SDLC layers above Continue

- add Git and review-specific workflow tools
- add CI/build visibility tools
- add deploy, rollback, logs, and environment-targeting workflows
- add documentation validation workflows such as build, preview, and link-check support

These are intentionally described as layers above Continue rather than Continue gaps. They are not primarily IDE-native or language-server-native capabilities, and they can be introduced later through AI Studio-native tools, desktop automation, and command-line execution patterns.

### 6. Continue-side setup guidance

- document the native participant flow for the VS Code/Continue integration
- avoid shipping workspace examples that imply daemon-owned OpenAI model entries are still the supported path

## Validation Commands

Primary daemon package validation:

```bash
export GOTELEMETRY=off && go test ./lib/sop/cmd/sop-daemon
```

Focused IDE worker validation:

```bash
export GOTELEMETRY=off && go test ./lib/sop/ai/agent/domains/ideworker
```

## Resume Checklist

Inspect these code surfaces first:

- `lib/sop/cmd/sop-daemon/client_bridge.go`
- `lib/sop/cmd/sop-daemon/rag.go`
- `lib/sop/cmd/sop-daemon/ide_worker_bridge.go`
- `lib/sop/ai/agent/domains/ideworker/capability.go`
- `lib/sop/cmd/sop-daemon/main_test.go`

Validate these behaviors before extending the design:

- `/api/continue/chat` still sends provider/model through Ask context and payload variables
- `/api/continue/agent/execute` still preserves provider/model through worker proxying and fallback execution
- delegated Continue execution remains correlated by `request_id` even when multiple requests share one `session_id`
- the Continue capability remains a passive participant and does not orchestrate phases
- the daemon Ask pipeline remains unchanged for Continue-originated requests
- the daemon LLM uses a single delegated Continue Ask tool instead of trying to mirror Continue's internal tool choreography
- capability matching is required before proxying to a registered Continue worker
- `/api/continue/metadata` still advertises the native contract and no daemon-owned config endpoint
- AI Studio does not expose a launcher for daemon-generated Continue config

## Guardrails

- Keep the daemon as the single workflow controller.
- Treat Continue as an editor surface and delegated worker.
- Prefer explicit request attributes over provider-specific transport semantics.
- Preserve provider neutrality at the daemon boundary.
- Avoid adding compatibility layers that lock the product into one provider's request shape.
