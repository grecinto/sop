# Cline Copilot Integration

**Status**: Cline SDK host prototype implemented; canonical workspace-diff import into SOP undo/memory is implemented; authenticated MCP guidance, semantic-memory, undo-history, validation-reporting, and confirmed restore tools are implemented. Cline SDK plus MCP is the target production integration. The CLI backend remains a validated fallback path. Continue remains a non-shipping reference implementation and explicit compatibility option.

The prototype has completed its first hardening pass: Cline and MCP workspace
roots must resolve to existing directories before execution, memory/undo search,
or restore; restore requires explicit confirmation plus backup/hash verification;
and the associated Cline session is marked stale after restoration.

Cline request cancellation is also propagated into the daemon's isolated PTY
worker, so aborting the Data Manager request terminates the running SDK task
instead of merely hiding the response.

## Cline Activity in AI Chat History

The AI Chat window should show a filtered, high-level activity timeline for a
Cline task without dumping Cline's raw SDK loop, model output, or tool JSON into
the conversation. Each task should create one assistant activity bubble that
updates in place and becomes the final result when the task completes.

Example:

```text
Cline SDK
Planning task...

Cline SDK
Investigating workspace...

Cline SDK
Running tests...

Cline SDK
Found 2 failing tests. Updating app.py.

Cline SDK completed
Updated app.py and test_math.py.
Validation: Python assertion passed.
```

### User-facing activity categories

The SDK/daemon event stream should reduce internal events to a small stable
taxonomy:

| Category | Example message |
|---|---|
| `started` | `Cline SDK started` |
| `planning` | `Planning task...` |
| `investigating` | `Investigating workspace...` |
| `tool` | `Reading files...` or `Running tests...` |
| `validation` | `Validating changes...` |
| `checkpoint` | `Checkpoint created` |
| `waiting` | `Waiting for model response...` |
| `completed` | Final summary with files and validation |
| `failed` | Bounded failure summary |
| `cancelled` | `Cline task cancelled` |

Do not expose raw model responses, full prompts, API payloads, credentials,
internal reasoning, or every Cline iteration in the Chat history. Those details
belong in the Terminal pane or daemon logs when diagnostic visibility is needed.

### Timing and transport

The initial `/api/cline/tasks` response should emit `started` immediately. The
Data Manager may poll `/api/cline/tasks/{id}` once per second, but should update
the visible activity bubble only when the message changes and no more often than
every 500 ms. A status that has not changed for five seconds may be refreshed to
show that the task remains active. The final terminal result replaces the
activity bubble rather than creating another verbose message.

The current `DelegationStatus.Progress`, `Message`, and `PartialResult` fields
are sufficient for the first implementation. The stable follow-up contract may
add:

```text
phase          intake | investigation | planning | execution | validation | delivery
activity_kind  planning | tool | validation | checkpoint | waiting
activity_label short user-facing label
updated_at     timestamp
```

The structured activity fields are now implemented in the shared delegation
status contract and populated by the Cline backend. The Data Manager prefers
`activity_label` for its single in-place Chat bubble and falls back to the
legacy progress/message fields for compatibility.

The surfaces remain intentionally separate:

```text
AI Chat history: filtered high-level Cline activity and final summary
Terminal pane:  raw PTY/SDK output and diagnostics
Undo/history:   structured files, diffs, validation, and restore controls
```

The intended product surface is the AI Studio floating AI window. It can remain
visible while the developer works in VS Code, IntelliJ, or another application.
The IDE remains the developer's workspace and editor; AI Studio is the consistent
copilot surface; and Cline SDK is the headless agentic execution engine behind
the daemon. No Cline IDE extension is required for this workflow.

The AI Chat window is the SOP Data Manager UI served by `httpserver`/`sopbackend`
templates. The React site is only the account/site launcher that opens Data
Manager in daemon mode. The normal Chat Send action continues through
`/api/ask` and SOP Ask/RAG orchestration. The production composer has one
primary Send action; SOP classification decides whether the copilot workflow
capability delegates a bounded coding slice to Cline. The direct Cline task
route remains an internal testing/diagnostic surface, not a second production
button. This keeps the UI integration in the existing Data Manager runtime and
preserves SOP as the single controller.

### Manual Data Manager smoke test

1. Start `sop-daemon` with the configured `cline` backend and start `httpserver`.
2. Open the Data Manager AI Chat window in daemon mode from the React site.
3. Configure a provider/model and API key in the existing local AI settings.
4. Type a bounded coding task and use the normal Send control.
5. Confirm SOP classification delegates the coding slice to Cline, the result
   reports completion and modified files, and cancel during a
	long task to verify the Cline worker stops.
6. Use a non-coding question to verify `/api/ask` remains SOP-owned without
	delegating to Cline.

The direct Cline route is retained for backend testing and diagnostics. Users
should see one Send action, with Cline delegation decided by SOP's copilot
workflow capability rather than by exposing backend-specific choices in the UI.

**Update 2026-09-29**: the user's Cline fork (`https://github.com/grecinto/cline.git`) is now cloned locally at `/Volumes/T7/cline` — a sibling of this repo (`/Volumes/T7/sop-cloud`), not nested inside it like `.continue-repo`. Both are opened together via `/Volumes/T7/sop-cloud-cline.code-workspace`. Confirmed Apache-2.0 licensed (permissive, supports porting logic under Option A). It has one uninitialized git submodule (`evals/cline-bench`) not needed for integration work. The rest of this document still describes the plan at a public-knowledge level — the next step is to re-verify every claim below against this real clone before writing integration code, the same way [IDE_COPILOT_CLIENT_INTEGRATION.md](IDE_COPILOT_CLIENT_INTEGRATION.md) was grounded against `.continue-repo`.

## Why Cline

- **Strategic driver**: Continue is no longer an actively supported product, so it remains useful as a reference implementation but is not a shipping dependency. Cline is the maintained, independent agentic copilot selected for production daemon integration.
- Open source, actively developed, strong agentic tool-use loop (plan/act modes, terminal execution, file diffs, checkpoints).
- A second real `ideworker.Backend` implementation would prove the [IDE_COPILOT_BACKEND_SDK.md](IDE_COPILOT_BACKEND_SDK.md) contract is genuinely reusable, not just Continue-shaped (this was tracked as an unstarted item in the Continue tracker).
- Cline has first-class **MCP (Model Context Protocol)** support, which may allow a fundamentally lower-maintenance integration path than forking (see options below).
- Cline publishes a TypeScript SDK (`@cline/sdk` and `@cline/core`) that exposes the same agent engine used by its CLI and IDE surfaces, including session lifecycle, custom tools, MCP integration, and runtime hooks.

## Cline's Own Memory and Undo Systems

Cline has several persistence and recovery mechanisms, but they are not equivalent
to AI Studio's semantic memory and searchable undo ledger:

| Cline system | What it provides | Ownership |
|---|---|---|
| Memory Bank | Project Markdown files such as `projectbrief.md`, `activeContext.md`, `systemPatterns.md`, `techContext.md`, and `progress.md` | Cline/project workspace |
| `.clinerules` and skills | Persistent project or global instructions that shape future agent turns | Cline/project workspace |
| Task history | Persisted Cline sessions, messages, usage, and task metadata | Cline local storage |
| Checkpoints | Shadow Git snapshots after file edits and commands; compare and restore without changing the user's visible Git history | Cline task/workspace |
| MCP | External tools and data sources available to the Cline agent, through local STDIO or remote HTTP/SSE servers | Cline client configuration |

Memory Bank is durable context expressed as files, not a semantic/vector recall
system. Cline checkpoints are excellent task-local rollback points, but they do
not replace AI Studio's `UndoManager`, which records transactions and searchable
code artifacts through the AI Studio memory/storage layer.

### AI Studio is the canonical memory and undo system

The integration deliberately gives ownership of durable engineering memory and
undo history to AI Studio/SOP. Its memory system is architecturally richer than
Cline's project-document Memory Bank because it can combine semantic retrieval,
KnowledgeBase/team-practice recall, session memory, validation history, and
promotion of useful experience across tasks. Its `UndoManager` is also more than
a rollback mechanism: it records structured transactions and modified-file
artifacts that can be searched and used as future engineering context.

Cline should consume this system through injected guidance and, later, scoped
MCP tools. Cline's own Memory Bank, task history, and shadow-Git checkpoints
remain local execution aids and recovery mechanisms. They are not merged into,
or treated as authoritative over, the AI Studio memory or undo ledger.

## Floating AI Studio Copilot Surface

The primary user experience is not an IDE-specific Cline extension:

```text
Developer
	-> AI Studio floating window
		-> SOP Ask/RAG and memory
			-> Cline SDK host
				-> workspace files, terminal, tests, checkpoints
			-> SOP UndoManager and execution history
		-> progress, diffs, validation, and restore controls
```

This lets the developer continue using VS Code, IntelliJ, or another IDE as the
workspace while keeping one consistent AI Studio copilot across applications.
The floater can display the active task, Cline tool progress, test output,
workspace diffs, memory guidance, and SOP restore actions. An IDE extension may
be added later for editor-specific affordances, but it is not a prerequisite for
the daemon, SDK, MCP, or cross-IDE workflow.

The ownership rule for integration is therefore:

- Cline checkpoints remain Cline's internal safety net for its own agent loop.
- AI Studio remains the canonical owner of delegated-task memory, validation
	history, and searchable undo transactions.
- The integration must not attempt to merge Cline's shadow Git history directly
	into the user's repository or treat it as the AI Studio undo ledger.

## Integration Levels and Directionality

| Direction | Current or possible interaction | Status |
|---|---|---|
| AI Studio -> Cline | Send a bounded task, workspace, provider/model, and guidance; Cline runs its own multi-turn plan/act/tool loop and returns NDJSON completion | Implemented through the daemon shell backend |
| AI Studio -> Cline | Stream progress, poll status, and cancel a running task | Implemented at daemon delegation level; per-tool approval is not exposed by the headless adapter |
| Cline -> AI Studio | Read team practices, semantic memory, task guidance, or undo history during a run | Possible through an AI Studio MCP server; not implemented yet |
| Cline -> AI Studio | Record validation, execution summaries, or checkpoint references | Possible through MCP or post-run daemon processing; not implemented yet |
| AI Studio <- Cline | Return file-level changes for automatic AI Studio undo/memory capture | Requires post-run workspace diff capture; current Cline adapter returns summary/metadata only |

The recommended progression is to add post-run diff capture first, then expose
AI Studio memory and undo operations as authenticated MCP tools. This preserves
AI Studio as the outer controller while allowing Cline to request context during
its own agent loop when the MCP direction is enabled.

## Complementary Memory and Undo Design

Cline and AI Studio/SOP are intentionally strong in different dimensions:

| Concern | Cline strength | SOP strength |
|---|---|---|
| Immediate task context | Conversation, active workspace, tool results, rules, and Memory Bank files | Durable context across tasks and sessions |
| Project knowledge | Human-readable Memory Bank, `.clinerules`, and skills | Semantic retrieval, KnowledgeBase, STM/LTM, and team-practice recall |
| Agent execution | Multi-turn inspect/edit/command/test loop | Outer orchestration, policy, guidance, and validation history |
| Undo | Frequent local checkpoints and fast task recovery | Structured, searchable, durable undo transactions |
| Cross-task learning | Limited to persisted task/project material | Experience promotion and reusable engineering memory |

The integration should combine these strengths rather than make either system a
mirror of the other:

1. **Before execution**, SOP retrieves relevant memory, team practices,
	 constraints, prior failures, and success criteria. Cline contributes current
	 workspace context, rules, skills, and Memory Bank content.
2. **During execution**, Cline owns file inspection, edits, commands, tests, and
	 high-frequency local checkpoints. SOP may be consulted through scoped MCP
	 tools, but should not micromanage every Cline tool call.
3. **After execution**, the host imports the meaningful result: summary,
	 validation outcomes, modified files, workspace diff, and correlation IDs.
	 SOP then records the canonical memory brief and undo transaction.

### Two-level undo model

- **Cline checkpoint** is tactical and task-local. It is created frequently,
	supports immediate restore while the agent is working, and preserves Cline's
	conversation context.
- **SOP undo transaction** is strategic and canonical. It represents one logical
	delegated task or workflow phase, stores modified-file artifacts and
	validation metadata, and remains searchable across Cline sessions.

Do not import every Cline checkpoint into SOP. Create one SOP transaction from
the completed task's before/after diff. If SOP restores a completed task,
Cline's active session must be marked stale or refreshed so it does not continue
reasoning from obsolete workspace state.

Imported transactions should retain `delegation_id`, Cline session/task ID,
Cline checkpoint reference when available, workspace root, task/phase, and
validation result. Cline checkpoints remain Cline's local safety net; SOP's
`UndoManager` remains the project's durable undo and history system.

## Three integration strategies to evaluate

**Major finding (2026-09-29, verified against the real clone at `/Volumes/T7/cline`)**: Cline is a monorepo, not a single VS Code extension like Continue. It ships `apps/cli`, a genuine headless CLI (`npm install -g cline`) that explicitly **"shares its agent core with the Cline VS Code extension, JetBrains plugin, and SDK"** (`apps/cli/README.md`). It supports one-shot, non-interactive, auto-approved, NDJSON-streamed execution:

```sh
cline --yolo --json "Fix failing tests"
cline -P anthropic -m claude-sonnet-4-6 -k sk-... --json "task"
```

This changes the assessment substantially — see Option C below, which is now the shipping daemon path; Options A and B remain future surfaces.

### Option A: Fork Cline's VS Code extension, mirror the Continue approach

Same shape as `.continue-repo`: vendor Cline's source, add an `aiStudioWorker`-equivalent module that registers as a delegation worker and exposes a local callback receiver for `delegate_continue_task`-style calls (would likely need a renamed daemon tool/route, e.g. `delegate_cline_task` / `/api/cline/*`, or a generalized `/api/copilot/*` transport shared across IDE backends).

**Update**: Cline is open source under a permissive license and the project explicitly encourages downstream forks/reuse of its agent logic (unlike a closed or restrictively-licensed copilot). This strengthens Option A beyond just "add hooks like we did for Continue" — we can directly bring over Cline's actual agentic tool-execution loop (plan/act mode switching, tool-call parsing, terminal/file-edit tool implementations) as validated, working logic rather than re-deriving our own orchestration glue on top of an unmodified Cline. That reduces the risk that made the Continue integration hard (guessing at internal GUI message wiring that turned out to be dead code) since we would own and understand the ported logic directly. Still verify the exact license terms once Cline is cloned locally before relying on this.

**Tradeoffs**: full control over the integration surface (same as what was built for Continue), and now potentially *less* guesswork than the Continue fork since Cline's reuse-friendly stance means we can port real logic instead of bolting hooks onto an opaque upstream. Still carries fork-maintenance burden (a vendored extension to keep in sync with upstream) and the same category of discovery work Continue required (finding real message listeners, real completion signals) — mitigated, not eliminated, by being able to read and adapt Cline's own logic directly.

### Option B: Expose AI Studio as an MCP server (no fork)

Cline (and increasingly other agentic copilots) can already connect to arbitrary MCP servers for tools and context, configured by the user without any extension fork. If AI Studio exposed an MCP server surface for:
- KnowledgeBase / Space semantic search and team-practice recall (the memory/vector-DB advantage already built)
- Undo/checkpoint operations (list, restore)
- Delegated task guidance/context (team practices, success criteria) as MCP resources/prompts

...then Cline could use AI Studio as a tool provider **without** AI Studio needing to fork or patch Cline at all. This inverts the current delegation direction: instead of AI Studio's daemon calling out to Cline to do coding work, Cline (running its own agent loop with its own LLM) would call *into* AI Studio for memory, KB grounding, and undo/checkpoint tracking.

**Tradeoffs**: much lower maintenance (standard protocol, no vendored fork), but it changes who is "in the driver's seat" — Cline would be the orchestrator for the coding turn, and AI Studio would be a capability provider rather than the delegator. This is closer to the reverse-role/participant mode already described in [CONTINUE_BRIDGE_API_SPEC.md](CONTINUE_BRIDGE_API_SPEC.md)'s "Mode 2: External controller, SOP Ask participates" than to the primary Continue delegation model. Whether this is acceptable depends on whether "Continue/Cline services the prompt, AI Studio just tracks diffs/undo" (the original ask that drove this whole integration effort) is still satisfied — it likely is, since the undo/history/memory ownership stays with AI Studio either way.

### Option C: Run the headless `cline` CLI as a managed subprocess behind `ideworker.Backend`

Instead of forking Cline's VS Code extension (Option A) or inverting orchestration via MCP (Option B), implement `ideworker.Backend.ExecuteTask`/`StartTask` in Go by shelling out to the real `cline` binary as a subprocess, the same way `codingworkflow` already shells out to `go test`/other tools:

```go
exec.Command("cline", "--json", "--cwd", workspaceRoot, "-P", provider, "-m", model, "-k", apiKey, task)
```

**Correction vs the README**: there is no `--yolo` flag in the real installed binary (v3.0.65). One-shot prompts are non-interactive by default already — `--auto-approve <boolean>` defaults to `true`. No extra flag is needed for headless/auto-approved execution.

#### Prototype validation (real, executed 2026-09-29)

Building the fork itself needs Bun 1.4.2 (not installed in the dev environment used for this prototype). Rather than add that dependency just to validate viability, the published `cline` npm package (v3.0.65 — same CLI contract, since no fork-specific changes exist yet) was installed in an isolated scratch directory (`npm install cline --no-save`, no build toolchain required) and run directly:

```sh
cline --json --cwd ./testdir "say hello"
```

Real NDJSON output observed (no valid API key was available in this environment, so this exercised the auth-failure path, not a full successful edit):

```json
{"type":"hook_event","hookEventName":"agent_start","agentId":"...","taskId":"...","parentAgentId":null}
{"type":"agent_event","event":{"type":"iteration_start","iteration":1}}
{"type":"agent_event","event":{"type":"error","error":{"name":"Error","message":"Unauthorized: ..."},"errorClass":"auth","recoverable":false,"iteration":1}}
{"type":"run_result","finishReason":"error","iterations":1,"usage":{...},"aggregateUsage":{...},"durationMs":107,"text":"Unauthorized: ...","model":{...}}
{"type":"error","message":"Unauthorized: ..."}
```

**Confirmed, not guessed**: the terminal/completion event is `{"type":"run_result", "finishReason": ..., "text": ..., "usage": {...}, "model": {...}}` — this is exactly the precise completion signal Option C's design needed, and it now has a verified real shape to map into `ideworker.ExecutionResult` (`finishReason` → `status`, `text` → `summary`). Structured `agent_event` entries with `errorClass`/`recoverable` fields give a clean, parseable error-classification path too.

**Not yet validated**: a full successful run with real file edits and `modified_files`-equivalent reporting — requires a live LLM API key, which wasn't available in this environment. Whoever resumes this work should re-run the same one-shot command with a real `-P/-m/-k` combination against a throwaway repo to capture the happy-path event shape (in particular, how file edits are reported in the event stream) before writing the Go mapper.

- Parse the NDJSON event stream Cline already emits (`--json`) and translate its final result into `ideworker.ExecutionResult` (modified files, summary, status) — the same target shape used for Continue, so the daemon-side undo/history/memory pipeline (`recordContinueDelegatedUndoTransaction`-equivalent) needs no new design, only a new event-shape mapper.
- Run it with `cwd` set to the target workspace root, and pass provider/model/API key per invocation exactly as `TaskRequest.Provider`/`Model` already carry today.
- **Confirmed API key plumbing (verified against `apps/cli/src/commands/auth.ts` and the real `--help` output, not just README prose)**: per-run override flags `-P <provider> -m <model> -k <api-key>` require no persistent Cline-side auth step at all — the daemon can pass its own already-managed provider/model/key pool straight through on each subprocess invocation. Persistent `cline auth --provider ... --apikey ... --modelid ...` and env vars (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY`, `CLINE_API_KEY`, etc.) also exist but are unnecessary for this integration shape.
- No VS Code GUI involvement, no dead-protocol-message guesswork, no heuristic diff-polling for completion — the CLI process exiting (or its `--json` stream reaching the `run_result` terminal event) is by construction the real, precise completion signal that Continue's integration never had.

**Why this is the strongest fit for "bring over Cline's rich copilot workflow"**: it literally runs Cline's own maintained agent core, unmodified, as a black-box tool the daemon's Go RAG agent invokes — not a reimplementation of Cline's logic in Go (which would require rewriting and then maintaining a fork of complex TypeScript agent logic in a different language, forever diverging from upstream), and not a fragile hook into a live GUI extension (which is exactly what made the Continue integration hard: dead protocol messages, no precise completion signal, Agent-mode assumptions). The daemon's Ask/ReAct stays the sole controller (participant pattern preserved); Cline CLI is simply a very capable delegated tool.

**Tradeoffs**: requires the `cline` CLI to be installed on the user's machine (npm-installable, platform binaries published for macOS/Linux/Windows per the README — a reasonable bundling/dependency story, arguably simpler than requiring VS Code + a specific extension version). Loses Cline's live in-editor UI (inline diffs, GUI approvals) since it's headless — acceptable for the daemon-delegation use case, but means Cline's own interactive VS Code extension remains a separate, optional surface for users who want that, not a requirement for this integration.

**Recommendation**: pursue Option C first. It is lower-risk than Option A (no GUI message-passing archaeology required), more aligned with "Cline services the prompt" than Option B (keeps AI Studio as delegator, not just a tool provider), and Cline's own checkpoints/undo (`/undo` in the CLI) can simply be left as Cline's internal safety net during a run, with AI Studio's `UndoManager` remaining the system of record via the same `ExecutionResult.modified_files` capture already designed for Continue.

### Option D (target): Embed the Cline SDK behind a daemon-managed TypeScript host with MCP

The target integration uses Cline's supported SDK rather than treating the CLI
as a black-box executable. The Go daemon remains the outer AI Studio controller,
but delegates the Cline execution to a small Node/TypeScript host process:

```text
Go daemon / Ask
	|
	| JSON or NDJSON task/result bridge
	v
TypeScript Cline SDK host (@cline/sdk or @cline/core)
	|
	+-- Cline agent.run / continue
	+-- Cline built-in coding tools
	+-- Cline checkpoints
	+-- AI Studio MCP server
```

The SDK provides enough surface for the first prototype:

- `Agent.run()` and `Agent.continue()` for bounded and follow-up turns.
- `ClineCore.create()` and `ClineCore.start()` for host-managed sessions.
- Custom tools and MCP integration for AI Studio memory, guidance, and undo.
- Runtime hooks such as `beforeRun`, `beforeTool`, `afterTool`, and `afterRun`.
- Cline checkpoint hooks and session metadata for local rollback and correlation.

The SDK host should expose only a narrow protocol to the daemon:

- `start` or `run`: task, session, workspace, provider/model, and correlation ids.
- `event`: progress, tool activity, checkpoint references, and validation output.
- `result`: canonical summary, status, metadata, and modified-file information.
- `cancel`: terminate the active Cline session.

The AI Studio MCP server is configured inside the Cline SDK host and exposes
authenticated, scoped tools such as `search_memory`, `get_task_guidance`,
`search_undo_history`, `restore_undo_transaction`, and `record_validation`.
The host injects the current delegation/session correlation id into every MCP
call. Cline's own checkpoint state remains local; AI Studio imports the final
workspace diff into its canonical undo and memory pipeline.

#### Prototype readiness

There is enough verified surface to begin a prototype. The minimum prototype
does not need the full VS Code extension or a production MCP server. It needs:

1. A Node/TypeScript host package depending on `@cline/sdk` or `@cline/core`.
2. One non-interactive `run` operation with workspace and provider/model input.
3. An NDJSON stdout protocol for events, completion, errors, and cancellation.
4. A first authenticated MCP tool, preferably `search_memory` or
   `get_task_guidance`, backed by an existing AI Studio endpoint.
5. A post-run workspace diff collector that returns `modified_files`.
6. A Go integration test using a fake SDK host before connecting a live model.

This is sufficient to prove the end-to-end shape: Ask -> SDK agent -> MCP
memory lookup -> file edit/test -> diff/result -> AI Studio undo and memory.
Provider credentials and a live model are needed only for the final smoke test,
not for validating the bridge protocol or MCP tool contract.

**Open question to resolve before building**: does Cline's own built-in checkpoint system (it already snapshots workspace state per task using a shadow git repo) conflict with or duplicate AI Studio's undo system? This is not a simple file-backup log — `codingworkflow.UndoManager` (`lib/sop/ai/agent/codingworkflow/undo_manager.go`, `undo_search.go`) generates vectorized/searchable content per transaction and stores it through the `SpaceClient` semantic-search interface, backed by the embedded SOP DB engine (the lightweight B-tree/vector store this whole project is built on) rather than an external or heavyweight database. That gives AI Studio semantic queries over undo history (e.g. "undo the auth middleware changes") that a plain shadow-git checkpoint stack cannot do. Cline's mechanism is likely a reasonable local safety net for its own edits, but it is not a replacement for AI Studio's vector-searchable undo ledger — the more likely resolution is that AI Studio's `UndoManager` remains the system of record regardless of which strategy (A or B) is chosen, and Cline's own checkpoints (if used) stay local/internal to Cline's own turn rather than being treated as AI Studio's canonical history.

## Recommended first steps when resumed

1. ~~Clone Cline's source locally~~ — done 2026-09-29 at `/Volumes/T7/cline`, Apache-2.0 confirmed.
2. ~~Evaluate integration strategies~~ — done 2026-09-29: Option C (headless CLI subprocess) identified and recommended.
3. ~~Prototype the headless CLI to check viability~~ — done 2026-09-29 against the published `cline` npm package (v3.0.65, same CLI contract as the fork today). Confirmed real flags (`--json`, `-c/--cwd`, `-P/-m/-k`, no `--yolo`) and the real terminal `run_result` NDJSON event shape. Only the auth-failure path was exercised (no live API key available); the happy path with real file edits still needs a run with a real key.
4. Building the fork itself needs **Bun 1.4.2** (not installed in this environment) — install it (`brew install bun` or similar) before building `/Volumes/T7/cline/apps/cli` from source, so testing reflects the actual fork rather than the published upstream package.
5. Run a real one-shot task with a live `-P/-m/-k` combination against a throwaway repo to capture the happy-path event shape (in particular, how file edits are reported in the stream) — still needed before writing the Go mapper.
6. ~~Implement a Go `ideworker.Backend` for Cline~~ — done in `lib/sop/cmd/sop-daemon/cline_worker_bridge.go`. The backend runs `cline --json` through the daemon's existing PTY worker/session process manager, maps the terminal `run_result` event to `ideworker.ExecutionResult`, and supports daemon-side cancellation through the session's existing `stop` command.
7. ~~Implement the Cline SDK host prototype~~ — done in `tools/cline-sdk-host`. It uses `@cline/core`, starts a headless Cline session with workspace/provider/model/task/guidance input, streams SDK events over NDJSON, supports cancellation, and normalizes SDK `finishReason` into the canonical result contract. The daemon backend uses this host by default; set `SOP_CLINE_EXECUTION_MODE=cli` for the legacy CLI fallback.
8. ~~Add authenticated AI Studio MCP tools to the SDK host~~ — done for the first slice. The daemon exposes authenticated `POST /mcp` with `initialize`, `tools/list`, and `tools/call`; the SDK host exposes `ai_studio_get_task_guidance` and `ai_studio_search_memory`, which call the daemon MCP endpoint during the Cline agent loop.
9. ~~Add read-only undo-history MCP access~~ — done. `search_undo_history` delegates to SOP's existing `UndoManager.SemanticSearch`; restore remains an explicit future mutation requiring confirmation and active-session invalidation.
10. ~~Add validation reporting through MCP~~ — done. `record_validation` stores Cline test/build outcomes in the scoped AI Studio session context for guidance, history, and later memory promotion.
11. ~~Add confirmed undo restoration through MCP~~ — done. `restore_undo_transaction` requires `confirm=true`, creates a safety backup, verifies hashes, restores through `UndoManager`, and marks the associated Cline session stale.
12. ~~Add SDK host packaging validation~~ — done for the local release prototype. `tools/cline-sdk-host/package-runtime.sh` verifies Node 22+, runs `npm ci` and the host build, and creates a deployable host directory containing the lockfile, package metadata, config, and `dist/host.js`.
9. ~~Add daemon-side before/after workspace diff capture and import `ModifiedFiles` into the shared SOP undo/memory pipeline~~ — done in `cline_worker_bridge.go`. The daemon hashes the workspace before and after the SDK task, emits modified-file CRUD/diff records, and reuses the shared SOP undo and execution-memory recorders.
10. Keep the daemon-side boundary shared: Continue and Cline both implement `ideworker.Backend`, use the exported `ideworker` capability/result types, and share delegation tracking/status projection. Their execution adapters remain separate because Continue calls a registered editor callback while Cline uses a daemon-managed SDK host. Cline is the default and shipping backend; Continue is retained only as a reference/compatibility implementation.
11. Decide whether/how to support optional IDE-specific surfaces later; no VS Code or IntelliJ extension is required for the core floating-window workflow.

## Related docs

- [IDE_COPILOT_BACKEND_SDK.md](IDE_COPILOT_BACKEND_SDK.md) — the daemon-side interface a Cline backend would implement under Option A.
- [IDE_COPILOT_CLIENT_INTEGRATION.md](IDE_COPILOT_CLIENT_INTEGRATION.md) — the Continue reference implementation and its hard-won lessons (dead protocol messages, heuristic completion detection) that should inform Cline's integration either way.
- [CONTINUE_BRIDGE_API_SPEC.md](CONTINUE_BRIDGE_API_SPEC.md) — the existing "Mode 2: External controller, SOP Ask participates" reverse-role contract, relevant if Option B is chosen.
