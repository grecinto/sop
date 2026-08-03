# Continue IDE Workflow Surface

**Status**: Active

## Purpose

This document captures the actual Continue IDE-side coding workflow surface that AI Studio from Northstar AI can rely on today, the architectural boundary between AI Studio and Continue, and the remaining gaps between a strong engineering loop and broader SDLC coverage.

Core conclusion:

- Continue is currently the strongest open-source substrate in this stack for coding workflows and IDE integration.
- AI Studio should remain the controller.
- Continue should remain the editor-facing execution surface and delegated worker.
- The right strategy is to surface the highest-value Continue IDE capabilities first, then layer AI Studio workflow control and SDLC-specific orchestration above them.
- The daemon should expose Continue capabilities as data: supported phases, phase modes, and each phase's available tools.

## Current implementation posture

The current implementation is now aligned with an Ask2Ask-style integration posture:

- Continue is treated as a delegated editor worker, not as a controller of the full workflow.
- The daemon owns request correlation, session snapshots, and request-scoped execution state through `request_id`.
- Continue only participates in AI Studio delegation for daemon-advertised provider/model pairs, which means the Big 3 provider families are supported through this path while unsupported routes such as local Ollama remain outside the native AI Studio contract.
- Continue remembers its HQ and daemon auth state across editor restarts through VS Code secret storage and rehydrates it on startup.

## Control Boundary

The control boundary does not change as the tool surface grows.

- Ask/ReAct remains the only controller.
- AI Studio and the daemon own orchestration, approvals, memory, workflow state, fallback behavior, and phase progression.
- Continue contributes editor context, local IDE affordances, and bounded delegated execution.
- Continue must not become a controller that runs multi-step workflows on its own.

This keeps the integration aligned with the participant pattern already used across the codebase.

## Why Continue Matters

Without Continue, the project would be materially farther from a practical IDE-integrated product because Continue already provides:

- a cross-editor integration layer
- a chat and agent interaction surface
- a tool invocation model
- file edit workflows
- terminal execution hooks
- provider and model plumbing
- mode-aware gating between passive chat and action-oriented execution

This does not make Continue the product controller. It makes Continue the current leverage point for IDE-native execution.

## Continue Mode Gating

Continue does not expose the same tool surface in all modes.

- `chat` mode: no active tools
- `plan` mode: readonly tools only
- `agent` mode: enabled tool set, including mutating tools when allowed

This means AI Studio should treat Continue mode as part of the execution contract rather than assuming tools are always available.

## Data-Driven Phase Metadata

The integration now supports a data-driven view of Continue rather than a hardcoded one.

The daemon can expose:

- supported Continue phases
- each phase's compatible Continue modes
- each phase's available tool list
- the full Continue IDE tool catalog
- the tool subset actually forwarded on a given request

That lets AI Studio consume Continue as metadata and decide how to align it with its own richer phase system.

This is the important design shift:

- Continue does not need to own the master workflow model.
- AI Studio can ingest Continue phase and tool metadata as input.
- AI Studio capabilities can then act as thin wrappers that call Continue remotely while preserving AI Studio orchestration.

This makes the integration portable to other external agents and other domains, not just Continue coding workflows.

## Current Continue Tool Surface

### Core read and discovery

- `read_file`
- `read_currently_open_file`
- `file_glob_search`
- `grep_search`
- `ls`
- `view_diff`

### Core mutation

- `create_new_file`
- `edit_existing_file`
- `single_find_and_replace`
- `multi_edit`

### Core execution

- `run_terminal_command`

### Optional and experimental discovery

- `codebase`
- `read_file_range`
- `view_repo_map`
- `view_subdirectory`

### Support and auxiliary tools

- `request_rule`
- `read_skill`
- `create_rule_block`
- `fetch_url_content`
- `search_web`

## Newly Surfaced IDE-Native Tools

The following Continue IDE-native capabilities were already present below the tool layer and are now surfaced as first-class built-ins:

- `get_problems`
- `goto_definition`
- `goto_type_definition`
- `get_references`
- `get_document_symbols`
- `get_terminal_contents`

These additions materially improve debugging, navigation, impact analysis, and failure triage.

Implementation surfaces:

- tool registration in `.continue-repo/core/tools/builtIn.ts`
- tool catalog loading in `.continue-repo/core/tools/index.ts`
- dispatch in `.continue-repo/core/tools/callTool.ts`
- definitions in `.continue-repo/core/tools/definitions/`
- implementations in `.continue-repo/core/tools/implementations/`

Validation completed:

- `npx vitest run tools/index.vitest.ts`
- `npx tsc -p ./tsconfig.json --noEmit`

## Still Unsurfaced or Deferred IDE Capabilities

Continue still has useful IDE methods below the tool layer that are not yet all surfaced as first-class tools or are intentionally deferred.

High-value unsurfaced or not yet productized:

- `getOpenFiles`
- `getCurrentFile` as a direct metadata tool rather than only indirect context
- `getPinnedFiles`
- `getSignatureHelp`

Deferred pending stricter policy review:

- `subprocess`

`subprocess` was not surfaced in the first pass because it would create a second command-execution path alongside `run_terminal_command`. That should only be exposed if AI Studio explicitly wants a narrower non-terminal process execution primitive and can define separate policy rails for it.

## SDLC Coverage Assessment

The Continue surface is now strong for the inner engineering loop, but it is still not full SDLC coverage by itself.

### Strong coverage now

- coding
- refactoring
- local code review
- testing through terminal execution
- debugging through diagnostics, references, definitions, symbols, diff, and terminal inspection
- documentation editing inside the repository

### Partial coverage now

- documentation validation
- release preparation
- Git and change-management workflows

### Still missing for broader SDLC completeness

- CI status and build pipeline visibility
- deploy, rollback, rollout, and environment targeting workflows
- production logs and post-deploy verification workflows
- release and pull-request specific abstractions
- doc build, preview, link-check, and release-note workflows

These should not be treated as missing IDE or language-server features.

They are broader workflow capabilities that AI Studio can add later through its own tools and desktop or terminal automation layer. In many cases, command-line access is sufficient for the first implementation, and Continue does not need to own them as IDE-native primitives.

The important distinction is:

- Continue plus the current surfaced tools is enough for strong daily engineering workflows.
- It is not yet enough for complete software delivery lifecycle coverage.

## Recommended Expansion Order

### Phase 1: Done

Surface the highest-value IDE-native debugging and navigation tools.

- `get_problems`
- `goto_definition`
- `goto_type_definition`
- `get_references`
- `get_document_symbols`
- `get_terminal_contents`

### Phase 2: Next Continue-side additions

Add the remaining low-risk IDE context tools.

- open files
- pinned files
- richer current-file metadata
- signature help where it materially improves coding assistance

### Phase 3: Workflow-specific outer-loop tools above Continue

Add explicit AI Studio workflow tools for:

- git status and review state
- CI and build visibility
- deployment and rollback
- documentation validation and release-note generation

That is the step that moves the system from strong engineering workflows toward broader SDLC coverage.

These are AI Studio concerns above the Continue boundary. They may use Continue terminal execution when useful, but they do not depend on Continue exposing additional IDE-native or language-server-native functionality.

## AI Studio Strategy

The strategic goal is not to let Continue become the workflow brain.

The strategic goal is:

- use Continue as the robust open-source IDE execution substrate
- keep AI Studio as the workflow controller
- let AI Studio decide which Continue tools are exposed for a given workflow profile
- add broader SDLC abstractions at the AI Studio layer rather than overloading Continue with product-controller responsibilities
- consume Continue phase and tool metadata as data, not as a hardwired special case

That outer layer can be built with AI Studio-native tools, desktop automation, and command-line workflows without changing the core Continue role.

In practice, that means Continue is the execution surface and AI Studio is the decision surface.

## Thin Wrapper Capability Pattern

The recommended AI Studio-side design is a thin wrapper capability that accepts remote agent metadata and delegates execution without becoming tightly coupled to any one external system.

That wrapper should be able to accept:

- remote phase metadata
- remote tool catalog metadata
- remote mode constraints
- request-scoped available tool lists
- request-scoped tool-choice hints

Then AI Studio can:

- align remote phases to AI Studio phases
- decide which remote tool subset is safe or useful
- delegate one bounded step to the remote participant
- resume the larger workflow under AI Studio control

That pattern is more general than Continue. It means AI Studio can integrate with almost any agent, domain, or tool surface that can describe its capabilities in metadata.

## Workflow Profile Mapping

The current Continue surface is already sufficient to support several AI Studio workflow profiles.

### Feature implementation

- file search and read tools
- document symbols and definitions
- edit tools
- diff review
- terminal execution

### Debugging

- problems
- references and definitions
- document symbols
- terminal contents
- read and diff tools

### Refactoring

- references
- definitions
- symbols
- edit or multi-edit
- diff review

### Testing and validation

- terminal execution
- terminal contents
- diff review

### Documentation

- file read and search tools
- edit and create file tools
- terminal execution for doc build or lint commands when present

## Related Docs

- `docs/CONTINUE_FACADE_ARCHITECTURE.md`
- `docs/CONTINUE_BRIDGE_API_SPEC.md`
- `docs/CONTINUE_BRIDGE_IMPLEMENTATION_PLAN.md`
- `docs/CONTINUE_NATIVE_SETUP.md`