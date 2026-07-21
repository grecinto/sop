# Workflow Runtime Implementation

## Scope

This document summarizes the implementation path for workflow-driven execution in the current agent runtime. It focuses on what is now wired up, how the workflow data is represented, and how it is validated.

## Current implementation

The workflow runtime is built around four pieces:

- Workflow definitions: the schema includes workflow identity, description, protocol guidance, and ordered steps.
- Workflow parsing: workflow payloads are read from knowledge-base entries and converted into structured workflow objects.
- Queue execution: each step becomes a queued work item with its own context and prompt guidance.
- Event visibility: workflow steps are surfaced in the chat history with hash-prefixed labels so the user can follow progress.

## Key runtime pieces

### Workflow model

The workflow model carries:

- step number and step title
- actor and priority
- step instructions and context requirements
- success criteria and iteration policy
- expected tools

A small rendering helper formats the step label as `Step N: Title` for internal context and `#Title` for chat history visibility.

### Workflow manager

The workflow manager expands step templates, builds the execution context, and enqueues each step. This means the workflow logic is driven from structured data rather than hard-coded behavior.

### Queue integration

Before each queued turn runs, the runtime checks whether it belongs to a workflow step. If it does, it emits a visible workflow-step event so that the user sees the step transition in real time.

## Authoring format

Workflow content can be authored in the knowledge base using a markdown entry with a structured JSON payload. The payload includes:

- `workflow_name`
- `description`
- `workflow_template`

The `workflow_template` contains the ordered steps and their metadata.

## Reliability hardening (Phase 5)

The workflow runtime can also opt into Phase 5 hardening for multi-turn execution. These safeguards are useful when LLM calls or downstream capabilities are transiently flaky.

### What is included

- retry with exponential backoff for transient errors such as timeouts, rate limits, and temporary service failures
- a circuit breaker that fails fast once repeated failures indicate a broken capability
- queue metrics for success rate, duration, actor distribution, and error types
- optional checkpoint persistence so a queued workflow can be resumed after a restart

### Enabling it

```go
copilotAgent := agent.NewCopilotAgent()
copilotAgent.EnablePhase5(nil, nil, nil, nil)
```

For more control, provide custom runtime components:

```go
retryStrategy := &agent.RetryStrategy{
    MaxAttempts:   3,
    InitialDelay:  1 * time.Second,
    MaxDelay:      30 * time.Second,
    BackoffFactor: 2.0,
}

circuitBreaker := agent.NewCircuitBreaker(3, 2*time.Minute)
metrics := agent.NewQueueMetrics()

copilotAgent.EnablePhase5(retryStrategy, circuitBreaker, metrics, nil)
```

### Operational visibility

Use `copilotAgent.GetPhase5Metrics()` to inspect counters and the current circuit-breaker state. This is the simplest way to observe runtime health during development and in production.

### Persistence

Checkpoint persistence is optional and requires SOP b-tree stores. It is useful when a workflow needs to survive a restart or brief outage without losing its queue state.

## Validation

The current implementation is covered by targeted tests that verify:

- workflow steps retain their titles during parsing
- workflow step labels render correctly
- workflow context is injected into queued work items

## Status

The core workflow path is now in place and visible to the user during execution. The remaining work is to grow the workflow catalog and refine the authoring experience for broader use.
