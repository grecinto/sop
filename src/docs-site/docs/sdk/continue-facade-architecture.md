# Continue Facade Architecture (Compatibility Reference)

**Status:** Historical compatibility page. Continue is not the shipping copilot backend. The standard AI Studio chat flow sends through Northstar AI Ask, which acts as the outer controller and delegates bounded coding tasks through the Joltrin SDK to the configured daemon backend; Cline SDK is the current default.

For the current backend architecture, start with [IDE Copilot Backends](./ide-copilot-backend.md). For a reusable integration walkthrough comparing the current Cline host with the retained Continue callback pattern, see [Integrating a Coding Agent](./coding-agent-integrations.md).

The old Continue facade remains in the repository as a compatibility implementation. Its `/api/continue/*` routes and editor-client design should not be treated as the production setup path or as a promise that Continue is installed with Northstar AI.

## Current Architecture

```text
AI Studio Send
    -> Northstar AI Ask/ReAct
    -> capability routing
    -> optional bounded backend task
    -> structured result and Northstar AI-owned history/undo
```

Northstar AI Ask controls the outer workflow. A delegated backend performs one bounded execution and returns a structured result; it does not replace Ask or own platform memory and undo.

For the user-facing [AI Studio interface](../user-guide/ai-window-experience.md), see its experience guide. For the reverse composition where an external agent calls Northstar AI Ask through the Joltrin SDK, see [Ask Participant Mode](./ask-participant-mode.md). Continue wire compatibility is documented in the repository's compatibility reference, not as a current SDK installation guide.
