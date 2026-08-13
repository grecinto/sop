# Workflow Architecture

This document covers the current AI Studio workflow model: a dynamic, phase-capability system controlled by the Ask pipeline.

## Overview

AI Studio's workflow model is built around phase capabilities. The system does this:

1. **Classify the task** in the context of the current workstream
2. **Select the most relevant phase capability** such as intake, investigation, planning, execution, review, or delivery
3. **Expose only the tools and guidance relevant to that phase**
4. **Let the Ask pipeline remain the orchestrator** across turns, tool calls, and recovery decisions
5. **Persist phase state in workstream memory** so the task can pause, resume, or jump safely

This keeps the architecture flexible: the LLM decides what to do next, while the capability layer provides structured context, phase-specific tools, and artifact capture.

## Supported Coding Workflow Languages

The shared coding workflow stack supports the following languages for reading, analysis, and workflow/script generation:

- **Go**
- **Python**
- **JavaScript / TypeScript**
- **Java**
- **Rust**
- **C#**
- **HTML / CSS**
- **React**
- **Angular**

This coverage is exposed through both the reader/analyzer layer and the workflow/script generation layer, so teams can use the same workflow model across front-end, back-end, and full-stack projects.

## The Ask Pipeline

The Ask pipeline (`CopilotAgent.ask()`) embodies the **LLM as Orchestrator** paradigm: the LLM decides the execution path, controls flow, and chooses the right capability or phase at each turn.

### How Decisions Are Made

The LLM reasons about the task, selects the relevant phase capability, chooses tools, and adapts based on evidence and user feedback.

### Pipeline Stages

1. **Routing & Classification**
   - prefix, continuation, and cold-start routing
   - phase and domain classification
   - explicit capability hints and context selection

2. **Capability Selection**
   - registry lookup and capability matching
   - phase guidance and relevant tool exposure
   - memory and STM context loading

3. **System Prompt Construction**
   - capability instructions assembled into the active prompt
   - workstream state integrated into the current context
   - domain-specific tools and guardrails applied

4. **Reasoning Engine Delegation**
   - provider-specific ReAct loop
   - tool call execution via capability handlers
   - multi-turn continuity with memory kept intact

5. **Post-Execution Hooks**
   - state capture, artifact persistence, and cleanup
   - phase transitions and workstream updates
   - resume, pause, or recovery decisions as needed

### What This Provides

The Ask pipeline provides:

- **Provider reasoning continuity** across turns
- **Tool framework integration** with authorization and observability
- **Memory integration** through STM and LTM
- **Adaptive phase control** that stays responsive to the task
- **Strong recovery behavior** for user corrections and blocked execution

By keeping the phase model inside the Ask pipeline, every phase inherits the same orchestration, memory, and tool access benefits.

### Why This Is Token-Efficient

The same architecture improves cost efficiency without forcing each turn into a rigid execution path.

- **Phases reduce prompt bloat** by narrowing the active tool surface and task focus.
- **Domains reduce prompt bloat** by excluding unrelated capabilities.
- **Recovery reduces wasted turns** by allowing the system to pause, jump, or stop instead of continuing in the wrong phase.

This keeps each turn focused, dense, and relevant.

## Phase Capability Model

The current workflow architecture is centered on phase capabilities. Each capability represents one stage of the work and exposes only the tools, guardrails, and context appropriate for that stage.

### Core Phase Set

The active phase model includes:

- **Intake** - clarify scope, constraints, and objectives
- **Investigation** - inspect code, tests, and repo context
- **Planning** - shape the fix or approach before implementation
- **Execution** - apply changes and run the relevant implementation steps
- **Review** - validate outputs and confirm correctness
- **Delivery** - summarize the result and handoff

Each phase participates in the overall flow while the Ask loop keeps the broader task coordinated.

### Example Phase Capability

```go
type PhaseCapability struct {
    sharedInfra *SharedInfrastructure
    phaseID     string
    name        string
    description string
    captureTool string
    stateTool   string
}
```

The capability uses `Classify()` to determine whether the current query fits the phase and `BuildGuidanceProfile()` to provide phase-specific guidance and guardrails.

```go
func (c *PhaseCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
    if c == nil {
        return capabilities.ClassificationResult{}
    }

    if state.Domain != "" && state.Domain != "coding_workflow" && state.Domain != "CodingWorkflow" {
        return capabilities.ClassificationResult{}
    }

    lower := strings.ToLower(strings.TrimSpace(query))
    if lower == "" {
        return capabilities.ClassificationResult{}
    }

    startingPhase := detectStartingPhase(lower)
    if startingPhase == c.phaseID {
        return capabilities.ClassificationResult{Relevant: true, Score: 14, Mode: capabilities.CapabilityModePrimary}
    }

    for _, keyword := range phaseKeywords(c.phaseID) {
        if strings.Contains(lower, keyword) {
            return capabilities.ClassificationResult{Relevant: true, Score: 12, Mode: capabilities.CapabilityModeSupporting}
        }
    }

    return capabilities.ClassificationResult{}
}
```

This approach gives the model a clear entry point while still allowing it to adjust as new evidence appears.

## Dynamic Phase Planning and Recovery

The phase system is intentionally adaptive. Plans are not rigid scripts; they are dynamic phase structures the LLM can revise as the task evolves.

### Adaptive Flow

The phase system is designed to respond to the task as it unfolds. It does not assume a single predetermined path:

- the model can skip a phase when it is unnecessary
- it can jump forward or backward when the task changes
- it can pause for clarification without abandoning the context
- it can recover after failure by returning to a more appropriate phase

This is exactly where the workstream and phase-plan mutation layer matters.

### WorkStream Management Tools

The system exposes explicit orchestration tools to allow the LLM to change flow in a controlled way:

- `list_workstreams`
- `get_workstream_details`
- `jump_to_phase`
- `pause_workstream`
- `resume_workstream`
- `abandon_workstream`

These tools let the LLM manage the current workstream state while the Ask pipeline keeps the broader execution flow consistent.

### Example: Adaptive Phase Movement

```text
User: "Skip planning and implement directly"
→ LLM recognizes user interjection
→ Calls jump_to_phase(phase="execution")
→ Workstream state is updated
→ Next turn continues from execution phase with prior context retained
```

```text
User: "Actually, we need to investigate the data layer first"
→ LLM judges the task needs more evidence
→ Calls jump_to_phase(phase="investigation")
→ Existing artifacts remain available
→ The task resumes from the correct analytical stage
```

This gives the system a flexible plan while preserving continuity across the full task.

## WorkStream Persistence and Recovery

Phase progress is stored in workstream memory so it survives across turns. That means the model can recover context even when the user pauses, asks a clarifying question, or changes direction.

### Persistent State Includes

- current phase
- planned phases and current index
- artifact history and capture metadata
- phase iteration counts
- pause/resume lifecycle state

This allows the system to treat work as resumable execution, not a one-shot prompt run.

## External Agent Integration

The same phase-based architecture can connect external agents and tool surfaces while keeping the task connected to AI Studio.

### Metadata-Driven Participants

An external integration can describe itself in metadata:

- supported phases
- phase-specific available tools
- constraint information
- request-scoped callable tool subsets
- request-scoped hints for delegation

AI Studio can then load that metadata into a capability and treat the remote system as a passive participant.

### Thin Wrapper Capability

The recommended pattern is a thin wrapper capability that:

- ingests remote capability metadata
- maps remote phases onto AI Studio phases
- exposes a bounded delegation tool when the remote system is itself an agent/orchestrator
- forwards execution to the remote backend
- returns control to the Ask pipeline after each bounded step

This keeps responsibilities clear:

- AI Studio remains the orchestrator
- the external system remains a tool substrate or delegated worker
- no external agent becomes the workflow controller

This pattern is domain-agnostic. It works for coding integrations like Continue and can also be extended to other agent surfaces as long as they can describe their capabilities as data.

## Ask as Participant

The same runtime can also be reused under an external controller, but the internal boundary remains clear.

This is a different boundary from the participant pattern inside Ask:

- externally, AI Studio Ask may be one participant in a larger multi-agent system
- within the delegated turn, AI Studio Ask remains the orchestrator of its own capabilities and tools

The supported composition is:

1. An outer agent decides a bounded AI Studio Ask turn is useful.
2. The outer agent calls SOP through a typed request or participant HTTP endpoint.
3. AI Studio Ask runs its normal routing, capability selection, ReAct loop, and memory access.
4. AI Studio returns a structured result to the outer controller.

This lets you use Continue or another controller as the outer layer while AI Studio continues to manage the delegated turn.

For concrete embedding and streaming examples, see [Ask Participant Mode](./ask-participant-mode).

## Conversational Feedback

The Ask pipeline also includes a conversational feedback layer that makes the phase flow transparent without making the phase capabilities themselves the controller.

### Design Goals

1. **Grounding confirmation** before tools execute
2. **Plan visibility** before the ReAct loop begins
3. **Finding summaries** during execution
4. **User control** through collapsible feedback output

### Emission Points

The feedback system emits at the orchestrator level:

```go
if v2Class, ok := ctx.Value("v2_classification").(*TaskContextV2); ok {
    a.emitGroundingSummary(ctx, query, v2Class)
}

if v2Class, ok := ctx.Value("v2_classification").(*TaskContextV2); ok {
    a.emitPlanDescription(ctx, query, v2Class, phases)
}

a.emitFindingsSummary(ctx, toolName, result, significance)
```

This keeps the experience clear:

- the orchestrator emits user-visible reasoning summaries
- phases stay focused on their own responsibilities
- the LLM still decides what to do next

## Benefits of the Phase Model

The phase capability workflow gives AI Studio a few practical advantages:

- **Adaptive reasoning** when the task shape changes
- **Safe recovery** when the user interjects or the current phase is wrong
- **Context continuity** across turns and pauses
- **Token efficiency** by limiting active instructions and tools to the relevant phase
- **Single orchestration model** with the LLM in control

The result is a workflow architecture that is dynamic, resumable, and clear without sacrificing structure.

## Best Practices for SDK Integration

### When to Use Phase Capabilities

- complex or ambiguous coding tasks that need an adaptive plan
- debugging, refactoring, or implementation work with uncertain scope
- tasks that need user correction or mid-flight recovery
- scenarios where tool exposure should stay narrow and phase-specific

### Where Adaptive Phases Help Most

- when the user may revise the path mid-task
- when the task may require re-entry to earlier evidence-gathering stages
- when the execution flow should remain under the Ask pipeline rather than a separate controller

The phase capability model works especially well for agentic programming, recovery, and multi-turn reasoning.
