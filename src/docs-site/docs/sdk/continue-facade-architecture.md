# Continue Facade Architecture (Compatibility Reference)

**Status:** Historical compatibility page. Continue is not the shipping copilot backend. The standard AI Studio chat flow uses SOP Ask as the outer controller and delegates bounded coding tasks to the configured daemon backend; Cline SDK is the current default.

For the current backend architecture, start with [IDE Copilot Backends](./ide-copilot-backend.md).

The old Continue facade remains in the repository as a compatibility implementation. Its `/api/continue/*` routes and editor-client design should not be treated as the production setup path or as a promise that Continue is installed with SOP Cloud.

## Current Architecture

```text
AI Studio Data Manager Send
    -> SOP Ask/ReAct
    -> capability routing
    -> optional bounded backend task
    -> structured result and SOP-owned history/undo
```

SOP Ask controls the outer workflow. A delegated backend performs one bounded execution and returns a structured result; it does not replace Ask or own platform memory and undo.

For the user-facing Data Manager surface, see [AI Window Experience](../user-guide/ai-window-experience.md). For the reverse composition where an external agent calls SOP Ask as a bounded participant, see [Ask Participant Mode](./ask-participant-mode.md). Continue wire compatibility is documented in the repository's compatibility reference, not as a current SDK installation guide.
