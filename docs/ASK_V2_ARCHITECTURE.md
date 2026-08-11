# Ask V2 Architecture - LAO-Orchestrated Session Control

**Created**: 2026-07-30  
**Status**: Active and implementation-aligned  
**Purpose**: Describe the current Ask/ReAct/phase/memory/cognition architecture as it exists in the codebase today.

---

## Core Principle: Ask Is the Controller

The current architecture is built around LLM-as-Orchestrator (LAO), not around hidden heuristics or a separate controller loop.

The system behaves like this:

1. The Ask pipeline receives the user request and owns the outer flow.
2. A phase-aware routing step classifies the task and resolves a planned phase sequence.
3. The agent creates domain phase proxies and exposes their tools to the model.
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
- current phase
- phase plan
- intent
- confidence
- CRUD expectations

The output is persisted into the active workstream so later turns can resume from the same plan.

### 3. Phase proxies are built for the selected domains
For each relevant domain, the system creates a phase-capability proxy for the current phase. Those proxies expose tools and instructions to the model but do not own workflow control.

### 4. The model orchestrates tool use inside a provider-owned ReAct loop
The model sees the available tools and chooses the next operation. This remains the main execution loop for tool calls, retries, and recovery.

### 5. Structured state governs the next decision
After each turn, the system inspects structured state for:
- pending clarification
- retry escalation
- terminal outcome
- queue pause conditions

### 6. Phase succession happens only when the current state allows it
If the current phase is complete and no clarification is pending, the next phase can be enqueued. If clarification is pending, the queue pauses and waits for the user to respond.

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

Relevant implementation:
- lib/sop/ai/agent/cognitive_layer.go

### Phase capabilities
Phase capabilities remain passive participants. They expose tools and instructions, and they can raise clarification or phase-transition state, but they do not own the outer orchestration loop.

Relevant implementation:
- lib/sop/ai/agent/codingworkflow/phase_capabilities.go

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
go test ./lib/sop/ai/agent/...
```

That suite passed successfully and covered the new clarification, cognitive-state, and phase-routing behaviors.
