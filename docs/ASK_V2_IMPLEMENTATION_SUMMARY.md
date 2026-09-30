# Ask Pipeline Implementation Status

**Updated:** 2026-09-29

This is a short implementation snapshot. The maintained architecture and behavioral contract are in [ASK_V2_ARCHITECTURE.md](ASK_V2_ARCHITECTURE.md); this file is not a second architecture specification. “Ask V2” is a historical document name, not a separate feature-flagged runtime path.

## Current implementation

- Ask owns the outer request, queue, and continuation decisions.
- Task routing selects relevant domains and capability/runtime surfaces.
- Provider-owned ReAct loops run the model/tool interaction.
- Clarification, retry/escalation, phase, workstream, and cognitive state are carried explicitly across turns.
- Capabilities remain participants: they classify, provide context, expose tools, and execute bounded tool calls.
- IDE coding delegation uses the shared `ideworker.Backend` contract. Cline SDK is the daemon's default backend; Continue is retained as compatibility/reference code.

## Code references

- Ask flow: [`ask_pipeline.go`](../lib/sop/ai/agent/ask_pipeline.go)
- Shared session contracts: [`interfaces.go`](../lib/sop/ai/interfaces.go)
- Cognitive layer: [`cognitive_layer.go`](../lib/sop/ai/agent/cognitive_layer.go)
- Provider ReAct implementation example: [`chatgpt_react_loop.go`](../lib/sop/ai/generator/chatgpt_react_loop.go)
- IDE worker interface: [`capability.go`](../lib/sop/ai/agent/domains/ideworker/capability.go)
- Cline daemon implementation: [`cline_worker_bridge.go`](../lib/sop/cmd/sop-daemon/cline_worker_bridge.go)

## Validation

Run from the repository root:

```sh
go test ./lib/sop/ai/agent/...
go test ./lib/sop/cmd/sop-daemon
```

For end-to-end Cline release validation requirements, see [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md). The legacy feature-flag rollout record remains in [ASK_V2_VERIFICATION.md](ASK_V2_VERIFICATION.md) and is historical only.
