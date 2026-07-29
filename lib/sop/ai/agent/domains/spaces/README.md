# Spaces Capability

This package hosts the Spaces capability for Ask. It is intentionally small and isolated so the core agent can delegate space-oriented requests without embedding domain behavior directly in the main runtime.

## Plug-and-Play Role
The spaces capability provides domain-specific routing and context for knowledge-space workflows, allowing Ask to handle space-oriented requests without embedding that logic into the core agent.

## What it provides
- Capability-owned classification for space-oriented prompts
- Focused execution context for space operations
- Tool guidance for knowledge-base workflows
- A modular boundary for future space-specific capabilities

## Integration Model
Like the other domain modules, it is registered in the shared capability registry and composed into the Ask runtime when relevant. When a request needs both knowledge-space and store or command context, Ask can combine the relevant capabilities additively during prompt assembly.
