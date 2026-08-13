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
- capability-level extension points for editor-native worker delegation

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

First-phase enrichment has now been added at the contract level:
- `SessionCognitiveObservation` for factual runtime state
- `SessionCognitiveAssessment` for structured situation interpretation
- `CognitivePolicySurface` for advisory cognition above retry hard rails

This now also changes blocked-turn handling in one narrow place: escalated or
clarification-bound turns can be assessed by an LLM-backed strict-JSON evaluator,
and the resulting recommendation is persisted in session cognitive state and
surfaced to phase escalation logic as advisory metadata.

Relevant implementation:
- [lib/sop/ai/agent/cognitive_layer.go](lib/sop/ai/agent/cognitive_layer.go)
- [lib/sop/ai/interfaces.go](lib/sop/ai/interfaces.go)
- [lib/sop/ai/retry.go](lib/sop/ai/retry.go)
- [lib/sop/ai/agent/ask_pipeline.go](lib/sop/ai/agent/ask_pipeline.go)
- [lib/sop/ai/agent/codingworkflow/phase_capabilities.go](lib/sop/ai/agent/codingworkflow/phase_capabilities.go)

### Continue delegation capability
Ask now also has the foundations for a Continue-backed capability participant. The correct target shape is LAO-preserving: Continue requests still go through the normal Ask pipeline, and Continue participates as a passive coding executor that exposes one delegated tool embodying Continue's own local Ask/tool loop.

The important architectural point is that the daemon LLM is still the outer orchestrator. It classifies the request as usual, selects phases as usual, and may still use other capabilities such as Spaces or Stores. Continue is used only when a bounded IDE-native coding slice should be completed locally in the editor.

Relevant implementation:
- [lib/sop/ai/agent/domains/ideworker/capability.go](lib/sop/ai/agent/domains/ideworker/capability.go)
- [lib/sop/ai/agent/copilot.go](lib/sop/ai/agent/copilot.go)
- [lib/sop/cmd/sop-daemon/ide_worker_bridge.go](lib/sop/cmd/sop-daemon/ide_worker_bridge.go)

---

## Current validation

The current implementation was verified with:

```bash
go test ./lib/sop/ai/agent ./lib/sop/cmd/sop-daemon
```

That suite passed successfully and covers the clarification pause behavior, cognitive-state handling, Ask routing integration, and the new daemon-backed IDE worker capability path.

---

## Architectural takeaway

The system is no longer described best as a simple “V2 pipeline with a feature flag.” It is better understood as a shared orchestration substrate where:

- Ask controls the outer loop
- phases remain passive participants
- ReAct stays provider-native
- state handoff is explicit and structured
- a lightweight cognitive layer supports future policy growth
- the cognitive layer now has explicit observation vs assessment contracts for future LLM-driven situation handling
- editor-native delegation can be added as a passive capability without creating a second controller
