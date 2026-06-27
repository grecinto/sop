# Desktop Automation Capability

This package hosts the desktop automation capability for Ask. It is intentionally small and isolated so the core agent can delegate desktop-oriented requests without embedding automation behavior directly in the main package.

## Plug-and-Play Role
The desktop capability contributes domain-specific routing, focused context, and tool guidance for local automation tasks such as shell commands, repository work, and file-system operations.

## What it provides
- Request classification for desktop-oriented prompts
- Focused execution context for local automation scenarios
- Tool exposure for automation-related actions
- A clean boundary so the core Ask runtime stays generic

## Integration Model
The capability is registered with the shared capability registry and selected when the routing state indicates a desktop-related intent. That keeps the runtime extensible while preserving existing Ask behavior.
