# Coding Workflow Package

This package provides reusable coding-workflow helpers for the shared SOP agent. It is not a separate outer controller. The Ask pipeline owns classification, ReAct execution, queue progression, and continuation; coding-workflow capabilities contribute context and bounded operations as participants.

## Current Package Areas

- [`workflow_manager.go`](workflow_manager.go), [`workflow_steps.go`](workflow_steps.go), and [`step_capability.go`](step_capability.go): workflow state and participant operations.
- [`treesitter.go`](treesitter.go), [`default_components.go`](default_components.go), and [`shared_infrastructure.go`](shared_infrastructure.go): repository context and reusable tooling.
- [`validation_orchestrator.go`](validation_orchestrator.go): validation helpers.
- [`undo_integration.go`](undo_integration.go), [`undo_manager.go`](undo_manager.go), and [`undo_search.go`](undo_search.go): change history and undo support.

File names describe package capabilities, not an independent workflow executor that should loop through a full plan. Each operation returns to Ask; enqueue bounded follow-up work only through the established workstream/queue contract.

## Authoring Rules

- Keep application and workflow state explicit in request/state structures; do not use `context.Context` as a domain-state bag.
- Keep mutations bounded and report validation/results so SOP can preserve history and undo.
- Propagate cancellation into commands and workers.
- Do not batch-enqueue a full phase plan or create a second ReAct/controller loop.

## Canonical References

- [Ask/ReAct architecture](../../../../../docs/ASK_V2_ARCHITECTURE.md)
- [Participant and workflow pattern](../../../../../docs/WORKFLOWS.md)
- [Current coding workflow guide](../../../../../docs/CLI_CODING_WORKFLOW_IMPLEMENTATION.md)
- [Work inventory model](../../../../../docs/WORK_INVENTORY_GENERALIZATION.md)

Run package tests from the repository root with:

```sh
go test ./lib/sop/ai/agent/codingworkflow/...
```

The sibling V1, queue, signal-event, and phase-2 documents are historical implementation notes. Their paths remain as redirects; use this guide and the canonical references above for current architecture.
