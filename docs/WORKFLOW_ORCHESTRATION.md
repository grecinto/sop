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

## Open direction

The next step is to expand the workflow catalog and improve the authoring experience so new workflows can be added without deep runtime changes.
