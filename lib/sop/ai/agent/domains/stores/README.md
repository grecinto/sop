# Stores Capability

This package hosts the Stores capability for Ask. It is intentionally small and isolated so the core agent can delegate store-oriented requests without hard-coding domain behavior directly in the main runtime.

## Plug-and-Play Role
The stores capability adds focused context and routing for store-management requests so Ask can reason about data stores without hard-coding their behavior into the main agent pipeline.

## What it provides
- Capability-owned classification for store-related prompts
- Specialized execution context for store operations
- Tool guidance that can be composed with other capabilities
- A modular boundary for future store-specific enhancements

## Integration Model
It participates in the shared capability registry as a first-class capability. Ask can combine Stores with other relevant capabilities such as Spaces or CommandLine when a request spans multiple domains, rather than forcing a single late-binding route.
