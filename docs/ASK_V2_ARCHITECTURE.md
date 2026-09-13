# Ask V2 Architecture - LAO-Orchestrated Session Control

**Created**: 2026-07-30  
**Status**: Active and implementation-aligned  
**Purpose**: Describe the current Ask/ReAct/phase/memory/cognition architecture as it exists in the codebase today.

---

## Core Principle: Ask Is the Controller

The current architecture is built around LLM-as-Orchestrator (LAO), not around hidden heuristics or a separate controller loop.

The system behaves like this:

1. The Ask pipeline receives the user request and owns the outer flow.
2. A routing step classifies the task and resolves the domain plus the lightest runtime shape needed to complete it.
3. The agent creates domain capabilities and, for coding tasks, exposes a task-mode-oriented runtime surface to the model.
4. The provider-owned ReAct loop remains the place where the model decides which tool to call next.
5. Structured state is carried between turns for clarification, retry guidance, and phase progression.
6. A lightweight session-cognition snapshot gives the pipeline a shared view of the current turn, memory signals, and policy-relevant flags.

This means the LLM remains the primary orchestrator, while explicit policy modules handle narrow enforcement points such as retry budget, repair rails, and clarification handoff.

---

## Runtime Shape

### 1. Ask receives the user request
The Ask entrypoint accepts a query plus config and prepares the shared session context.

### 2. Cold-start routing classifies the task
The agent resolves a task context with:
- domains
- task shape
- orchestration level
- runtime mode or current phase metadata
- phase plan when persistent continuity still benefits from it
- intent
- confidence
- CRUD expectations

The output is persisted into the active workstream so later turns can resume from the same continuity state.

### 3. Runtime surfaces are built for the selected domains
For each relevant domain, the system creates the runtime surface that best fits the task. In coding workflow, the live Ask prompt uses task-mode wording and exposes a discovery or implementation runtime surface rather than teaching the model a full SDLC phase taxonomy for every turn. Internal phase metadata can still exist for continuity and compatibility, but it is not the primary prompt frame.

The current implementation now adds a middle control layer between raw task mode and internal phase state:

- `task_shape`: what kind of work this is
- `orchestration_level`: whether the ask should remain single-turn or enter a managed workflow
- `phase`: the next disciplined step only when managed workflow control is active

This prevents two failure modes:

- phase-heavy ceremony for simple asks
- ad hoc execution for complex asks like repo-wide test repair

The intended control order is:

1. classify the task shape
2. decide whether the ask stays `single_turn` or becomes `managed_objective`
3. only then activate internal phase control

This should be read as an implementation choice, not a permanent architectural requirement. The important invariant is that phase state remains internal execution context rather than a required conversational transport protocol. If the phase-aligned tool surfaces and prompt context become strong enough, the system can simplify away from explicit task-mode wording without reintroducing brittle phase hops.

### 4. The model orchestrates tool use inside a provider-owned ReAct loop
The model sees the available tools and chooses the next operation. This remains the main execution loop for tool calls, retries, and recovery.

### 5. Structured state governs the next decision
After each turn, the system inspects structured state for:
- pending clarification
- retry escalation
- terminal outcome
- queue pause conditions

### 6. Continuation happens only when the current state allows it
If a later turn is genuinely needed and no clarification is pending, the next continuation item can be enqueued. If clarification is pending, the queue pauses and waits for the user to respond. Coding asks should prefer one continuous Ask/ReAct turn when the task can be completed without an extra handoff.

The same rule applies to internal phase progression: if the next local step is still actionable, Ask should keep going rather than externalizing progress into a visible phase transition.

### 7. Memory and cognition persist across turns
The session payload carries a lightweight cognitive snapshot, while MRU and workstream state preserve continuity for future turns.

---

## Key Architectural Components

### Ask pipeline
The Ask pipeline is the controller of outer-flow behavior. It owns:
- queue processing
- work-item ordering
- phase context preservation
- pause behavior when clarification is pending

Future build-target note: if HQ and daemon should ship different capability surfaces, that separation should happen at build/import time rather than only through runtime registration flags. See [STRUCTURED_PLANNER_EXECUTION_DESIGN.md](STRUCTURED_PLANNER_EXECUTION_DESIGN.md) for the TODO.

For coding workflow specifically, the persisted workstream may still store phase metadata for continuity, but the live Ask routing hint is task-mode-oriented (`discovery` or `implementation`) rather than phase-oriented.

The important refinement is that `implementation` no longer implies immediate execution readiness. A coding ask can be implementation-oriented at the task-mode level while still entering managed `investigation` or `planning` internally when the work requires diagnosis, sequencing, or design.

Relevant implementation:
- lib/sop/ai/agent/ask_pipeline.go

### Provider-owned ReAct loop
The ReAct loop stays provider-native and handles tool execution, retries, and repair guidance. It now consumes structured retry guidance instead of relying only on prose-based hints.

Relevant implementation:
- lib/sop/ai/generator/chatgpt_react_loop.go

### Shared session payload
The shared session payload now carries the state that matters across Ask, ReAct, and phases:
- clarification state
- retry rewrite state
- cognitive snapshot
- conversation history

Relevant implementation:
- lib/sop/ai/interfaces.go

### Cognitive layer
The cognitive layer is a lightweight shared view of the current session. It collects:
- current turn and actor
- current phase and query
- pending clarification and queue status
- retry/escalation tiers
- terminal reason and last decision
- memory signals

The cognitive layer now exposes two structured surfaces:
- a factual `SessionCognitiveObservation` assembled by Ask/ReAct/phase machinery
- an advisory `SessionCognitiveAssessment` produced by deterministic fallback policy plus LLM-backed evaluation on blocked or escalated turns

This split is deliberate. The runtime should gather facts deterministically, while
the policy layer interprets whether the system is progressing, stalling, looping,
needs clarification, or should move backward/forward in phase. Ask remains the
only controller.

Relevant implementation:
- lib/sop/ai/agent/cognitive_layer.go
- lib/sop/ai/interfaces.go
- lib/sop/ai/retry.go

Current status:
- blocked or escalated Ask turns now assemble a structured cognitive observation
- the agent evaluates that observation with a strict-JSON LLM prompt when the turn is clearly in trouble
- the resulting assessment is persisted into session cognitive state and attached to escalation metadata
- phase escalation can honor a suggested recovery phase instead of always using the default backward map

### Phase capabilities
Phase capabilities remain passive participants. They expose tools and instructions, and they can raise clarification or phase-transition state, but they do not own the outer orchestration loop.

Relevant implementation:
- lib/sop/ai/agent/codingworkflow/phase_capabilities.go

### Orchestration-level contract

The runtime now distinguishes between two orchestration levels:

- `single_turn`: the next action is obvious, local, and easy to validate
- `managed_objective`: the ask needs diagnosis, sequencing, tradeoff analysis, or more than one meaningful step

This is the middle ground between the original phase-heavy design and the later over-ad-hoc implementation path.

Examples:

- `rename this symbol` -> usually `single_turn`
- `run this test` -> usually `single_turn`
- `fix failing tests` -> usually `managed_objective`
- `implement this feature across the repo` -> usually `managed_objective`
- `refactor subsystem X` -> usually `managed_objective`

When managed objective execution is selected, execution should begin only when the ask is execution-ready. For issue-oriented asks, that means the system should already have a failing behavior, a root-cause hypothesis, a causal path, and a focused validation target before it treats execution as the active phase.

For coding workflow, execution readiness is no longer just a classifier opinion. The runtime can promote a managed coding ask into execution when a persisted planning artifact is present with `status=ready_for_execution`. This keeps phase entry tied to actual planning output instead of letting write-intent language skip directly into execution.

---

## Clarification and Queue Pausing

Clarification is now a first-class state transition.

When clarification is pending, the Ask queue halts instead of continuing with later queued work. The state is preserved in the session payload and the queue resumes only after the user responds.

This behavior is intentionally explicit and easy to reason about:
- pending clarification => pause
- resolved clarification => continue
- repeated failure and high escalation tier => escalate or stop

---

## Retry and Recovery Policy Surface

The retry layer is not a hidden orchestrator. It exposes explicit policy points for:
- recoverability
- retry tiering
- repair strategy
- retry budget
- clarification/escalation

The current implementation uses:
- hard rails for safety and bounded execution
- structured guidance for provider-owned loops and model-visible recovery context

The new contract layer adds an explicit `CognitivePolicySurface` above the retry
surface. Its responsibility is advisory situation recognition, not loop control.
The current implementation already supports LLM-backed assessment for blocked or
escalated turns and uses the same contract to classify failures such as:
- missing fact
- wrong tool
- wrong phase
- terminal failure
- looping without progress

and recommend structured next actions such as:
- retry same tool
- retry with a different tool
- research first
- ask clarification
- move backward a phase
- exit

The default implementation remains deterministic as a fallback hard rail, while
the agent layer invokes an LLM-backed assessor only when the turn is already
blocked or escalated.

This keeps the model as the decision-maker for orchestration while preserving deterministic guardrails where needed.

---

## Why This Shape Works

The current architecture is now being extended to better support design-first behavior. In addition to repo grounding and phase-specific tool use, planning is being nudged to produce an explicit design brief for architecture and feature-design requests. The expected shape includes:

- problem framing and scope
- constraints and non-goals
- candidate approaches and trade-offs
- relevant repo/domain evidence
- Team Practice / Core Playbook guidance when applicable
- clear risks and validation ideas

That plan is tracked in [docs/ARCHITECTURE_DESIGN_PROMPT_PLAN.md](ARCHITECTURE_DESIGN_PROMPT_PLAN.md).

The design keeps the system aligned with the architectural principles already documented in the repo:
- Ask is the controller
- capabilities are passive participants
- policy layers advise or constrain rather than silently take over
- state handoff is explicit and persisted

In short, the system now behaves like an orchestrator-first agent with a shared session brain rather than a collection of loosely coupled heuristics.

## Structured Capability Guidance

Capabilities should not only expose tools; they should also expose a structured guidance profile that tells the Ask loop how that capability should behave. This profile is now treated as a first-class contract for guidance, alongside tool exposure.

A capability guidance profile contains:
- a role statement
- core principles
- phase-specific rules
- task-situation rules
- tool-selection heuristics
- verification expectations
- guardrails and escalation guidance

This is intentionally layered so that the system can support:
- shared domain-agnostic guidance
- capability-specific behavior
- phase- or task-family-specific steering
- domain-specific evidence requirements

The guidance contract is also progressive. Simple capabilities can stay terse and rely on minimal role/guardrail hints, while ordinary capabilities can use light profiles with a role and a few guardrails, and complex capabilities can opt into deep profiles with richer rules, tool heuristics, and verification expectations. This lets capability authors keep the prompt compact for ordinary behavior and still encode domain-specific expertise where it matters.

Authoring guidance:
- use minimal guidance for trivial capabilities with no special behavior
- use light guidance when a capability needs a clear role or a small set of safety rules
- use deep guidance when the domain has important workflows, evidence requirements, or tool-selection heuristics

The Ask pipeline composes this guidance into the effective system prompt, while the capability remains responsible for defining the policy and behavioral expectations. This makes the guidance stronger, more reusable, and easier to test than free-form context strings.

---

## Verification Notes

The current implementation was verified with:

```bash
go test ./lib/sop/ai ./lib/sop/ai/agent ./lib/sop/ai/agent/codingworkflow -count=1
```

That suite passed successfully and covered the new clarification, cognitive-state, and phase-routing behaviors.
