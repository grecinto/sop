# Ask V2 Implementation Summary

**Created**: 2026-07-30  
**Status**: Active and aligned with the current codebase  
**Location**: [lib/sop/ai/agent](lib/sop/ai/agent)

---

## What is implemented now

The current implementation is a session-oriented Ask pipeline with:

- queue-based Ask orchestration
- phase-aware task routing
- provider-owned ReAct execution
- structured clarification state
- structured retry guidance
- a shared session-cognition snapshot

The design is intentionally LAO-first: the model remains the primary orchestrator, while the system provides explicit policy surfaces for recovery, escalation, and guardrails.

---

## Key implementation points

### Ask pipeline
The Ask pipeline now owns the outer flow and pauses the queue when clarification is pending. It also preserves the actor and phase context for queued follow-up work.

Relevant implementation:
- [lib/sop/ai/agent/ask_pipeline.go](lib/sop/ai/agent/ask_pipeline.go)

### Phase and workstream state
Phase plans and phase progression are persisted in the workstream context so that a paused or resumed task can continue from the same plan.

### Structured retry and clarification state
Retry guidance and clarification requests are now carried as structured state rather than inferred from loose prose. That makes handoff across Ask, ReAct, and phase capabilities more reliable.

Relevant implementation:
- [lib/sop/ai/interfaces.go](lib/sop/ai/interfaces.go)
- [lib/sop/ai/generator/chatgpt_react_loop.go](lib/sop/ai/generator/chatgpt_react_loop.go)

### Shared cognition snapshot
A lightweight cognitive layer now collects turn state, memory signals, and policy-relevant flags in one shared place.

Relevant implementation:
- [lib/sop/ai/agent/cognitive_layer.go](lib/sop/ai/agent/cognitive_layer.go)

---

## Current validation

The current implementation was verified with:

```bash
go test ./lib/sop/ai/agent/...
```

That suite passed successfully and covers the new clarification pause behavior, cognitive-state handling, and Ask routing integration.

---

## Architectural takeaway

The system is no longer described best as a simple “V2 pipeline with a feature flag.” It is better understood as a shared orchestration substrate where:

- Ask controls the outer loop
- phases remain passive participants
- ReAct stays provider-native
- state handoff is explicit and structured
- a lightweight cognitive layer supports future policy growth
