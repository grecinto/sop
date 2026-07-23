# Workflow Orchestration

## Status

This document captures the current design for workflow-driven assistance in SOP. The goal is simple: when the assistant handles a common task such as debugging a failing test or carrying out implementation work, it should follow a visible, repeatable sequence instead of improvising from a single prompt.

## Why it exists

Workflows make the assistant more dependable in two ways:

- They turn common multi-step tasks into a structured path that is easier to author, review, and update.
- They give the user a clear view of what is happening as the task progresses.

That means workflows are not just an internal orchestration feature; they are part of the product experience.

## Design goals

1. Make workflows authorable from knowledge-base content.
2. Keep the runtime data-driven rather than hard-coded.
3. Show each workflow step clearly in the chat experience.
4. Preserve enough structure for the runtime to inject the right context, tools, and guidance for each step.

## Core experience

A workflow is expressed as a set of steps, each with:

- a title that is shown to the user
- short instructions for the assistant
- required context and success criteria
- expected tools and iteration guidance

During execution, the runtime queues each step and surfaces it in the chat history with a hash-prefixed label such as `#Review and polish`. This gives the user a visible separation between workflow phases and ordinary shell-style commands.

## Architecture

The workflow system is intentionally layered:

- Knowledge base layer: workflow definitions live in curated markdown and structured payloads.
- Runtime layer: a workflow manager reads those definitions, expands templates, and enqueues steps.
- Execution layer: the queue runner processes each step with the full tool and context environment.
- UI layer: workflow steps are emitted as visible events in the chat stream.

This keeps the experience understandable while allowing the system to evolve without embedding new logic in the agent code.

## What this enables

- More consistent handling of tasks like fixing failing tests.
- Easier reuse of proven operating patterns across tasks.
- Better transparency for the user during long-running work.
- A foundation for richer workflows later, including conditional steps and scripted acceleration.

## Industry alignment

Our data-driven workflow approach aligns with emerging industry patterns:

### Comparable frameworks

- **LangGraph** (LangChain's declarative paradigm): Graph-based agent topologies with conditional edges and loops
- **OpenAI Assistants API**: JSON-defined agents with declarative tool assignments
- **Vertex AI Agent Builder**: Google's visual workflow editor with data-driven step definitions
- **DSPy Signatures**: Declarative prompt templates with learnable parameters

Our `workflow_template` structure maps directly to these patterns:
- Each step = LangGraph node
- Step ordering = DAG execution
- Success criteria = DSPy assertions
- Tool expectations = OpenAI function definitions

### Schema versioning

All workflows include a `schema_version` field (currently "1.0") to enable safe evolution:

```json
{
  "workflow_template": {
    "schema_version": "1.0",
    "name": "Debug Test Failure",
    "steps": [...]
  }
}
```

**Benefits**:
- **Backward compatibility**: Runtime can handle multiple schema versions simultaneously
- **Safe migration**: Old workflows continue working when new features are added
- **Clear contracts**: Each version documents which fields are required vs optional
- **Gradual adoption**: Teams can upgrade workflows incrementally, not all at once

**Version history**:
- `1.0` (2026-07): Initial release - linear step sequences with iteration policies

**Planned versions**:
- `1.1` (Q3 2026): Add conditional branching with `next` field
- `1.2` (Q4 2026): Add parallel execution with `parallel` and `sync` fields
- `2.0` (Q1 2027): Breaking change - consolidate step metadata format

This follows industry best practices from OpenAPI, JSON Schema, and Kubernetes API versioning.

### Why Go + data-driven wins

**Architecture advantage**: Our AI database is native Go, so writing the AI runtime in Go provides:
- Direct memory access to vector stores without serialization overhead
- Type-safe schema validation for workflow definitions
- Consistent transaction semantics across database and agent operations
- Lower latency for RAG retrieval (no cross-process boundaries)

**Data-driven advantage**: Workflow definitions as structured data enable:
- Version control: workflows are Git-trackable JSON/YAML
- A/B testing: swap workflows without code deploys
- Portability: same workflow works across different execution environments
- Observability: structured logging of workflow execution paths
- Non-technical authoring: product managers can tune agent behavior

Industry frameworks (LangChain, LlamaIndex) optimize for rapid prototyping in Python. Our Go approach trades initial velocity for production characteristics: performance, safety, and database integration.

## Roadmap: Next evolution

### Phase 1: Conditional branching (Q3 2026)

Add decision nodes to workflow schema:

```yaml
workflow:
  steps:
    - id: research
      tool: search_kb
      next:
        - condition: "result_count > 5"
          step: summarize
        - condition: "result_count == 0"
          step: escalate
        - default: analyze
```

**Implementation**: Extend `workflow_template.steps` to include `next` array with condition evaluators. ReAct engine checks conditions after each tool execution.

### Phase 2: Parallel execution (Q4 2026)

Fan-out independent steps:

```yaml
steps:
  - id: validate
    parallel:
      - tool: run_unit_tests
      - tool: run_integration_tests
      - tool: check_code_style
    wait: all  # or "any" or "majority"
```

**Implementation**: Queue manager spawns goroutines for parallel steps, waits on sync barrier before continuing.

### Phase 3: Prompt templates as data (Q1 2027)

Move from hardcoded prompts to template library:

```go
type PromptTemplate struct {
    Name      string
    Template  string   // Go text/template format
    Variables []string
    Examples  []PromptExample
    Metadata  map[string]any
}
```

**Benefits**:
- Prompt versioning and rollback
- Per-workflow prompt customization
- Easier testing of prompt variations
- Foundation for learned prompt optimization (DSPy-style)

### Phase 4: Workflow marketplace (Q2 2027)

Built-in workflow catalog for common patterns:
- Debug failing test workflow
- Implement feature from spec workflow
- Database migration workflow
- Code review workflow

Users can:
- Import workflows from shared library
- Export custom workflows to team repository
- Rate and comment on workflow effectiveness

## Open direction

The immediate next step is to expand the workflow catalog and implement conditional branching. This will unlock autonomous handling of edge cases that currently require human clarification.
