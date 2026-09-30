# Spaces and Knowledge Bases

This guide describes the current product model for Spaces and Knowledge Bases. It replaces the older blob-to-Space proposal and its unimplemented acceptance checklist; it is not a roadmap for drag-and-drop ingestion or a separate “expert avatar” product.

## Model

- A **Space** is a named knowledge domain used to organize and retrieve curated content.
- A **Knowledge Base (KB)** is the expertise source associated with a Space and its categories/items.
- A **Store** is a data/storage resource managed by the platform; it is not interchangeable with a Space.
- Categories provide navigable organization and can also be used as semantic context for retrieval.
- Documents are supported knowledge resources, with their storage and browsing details described in [STREAMING_DOCUMENT_STORAGE.md](STREAMING_DOCUMENT_STORAGE.md).

The assistant can use a selected Space/KB as grounded context. It does not guarantee that every answer is fully grounded or that a Space behaves as a separately trained model. Retrieval and response quality depend on the available content, query, and configured model.

## Naming

Names are normalized at API boundaries for storage compatibility. For example, `Core Playbook` is stored as `core_playbook`; UI code should use the display-name helpers where a human-readable label is needed. Use normalized identifiers in code and requests, not display labels.

## Quality and Expertise

KB Quality provides validation and effectiveness tooling around expertise sources. It is documented separately in [KB_QUALITY.md](KB_QUALITY.md); dashboard and metrics details live in [KB_QUALITY_DASHBOARD.md](KB_QUALITY_DASHBOARD.md) and [OBSERVABILITY_KB_QUALITY.md](OBSERVABILITY_KB_QUALITY.md).

Do not assume that importing a KB automatically installs validators, enforces every rule, or creates a full coding workflow. The runtime behavior is defined by the active Ask/capability/tool configuration and the validation features enabled for that deployment.

## Memory and Retrieval

Spaces/KBs are one of several grounding inputs available to the Ask pipeline. They complement session continuity and long-term memory; they do not replace the shared Ask controller or the canonical memory lifecycle. See [LONG_TERM_MEMORY.md](LONG_TERM_MEMORY.md), [VIRTUAL_MEMORY_RECALL_PLAN.md](VIRTUAL_MEMORY_RECALL_PLAN.md), and [ASK_V2_ARCHITECTURE.md](ASK_V2_ARCHITECTURE.md).

## Developer Guidance

- Keep Space-specific UI and resource behavior in the relevant product or shared-runtime layer according to ownership; see [DEVELOPMENT_GUIDE.md](DEVELOPMENT_GUIDE.md).
- Prefer existing retrieval and storage APIs rather than adding a second document or embedding pipeline.
- Treat dated feature plans and implementation reports as historical evidence. Confirm current behavior in code and tests before documenting it as supported.

For the broader topic map, see the [documentation index](INDEX.md).
