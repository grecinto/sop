# Space Expert / Blob-to-Space Digest Feature Guide

## Purpose

This document defines a new feature that turns uploaded documents into a curated, reusable knowledge source inside a Space and then uses that Space as the grounding context for AI-assisted work.

The feature is intended to support multiple use cases:

- courseware or lecture material ingestion,
- coding-session expertise from repo patterns or reference repositories,
- debugging playbooks and troubleshooting knowledge,
- testing and unit-test conventions,
- operational documentation and internal SOPs,
- domain-specific knowledge from PDFs, Word documents, text files, and similar sources,
- drag-and-drop ingestion of files into a target Store for fast, user-friendly uploads.

The core idea is simple:

1. Upload files into a blob-backed document store.
2. Extract and digest their contents.
3. Curate the result into a target Space.
4. Use that Space as a structured expert context for future AI questions.

---

## Product goal

Provide a general-purpose “expert memory” workflow where users can:

- upload document blobs,
- drag and drop files into a target Store,
- store them as raw files,
- digest them into a Space,
- and later ask AI questions grounded in that Space.

This should work for any domain, not just technical or educational content.

---

## Core user stories

### 1. Drag and drop files into a target Store
As a user, I want to drag files from my desktop into a target Store so that uploading is fast and intuitive.

Acceptance criteria:
- The UI exposes a visible drop zone for files.
- Dragging and dropping PDFs, Word documents, TXT, Markdown, and HTML files starts the upload flow immediately.
- The system accepts multiple files in a single drop and shows progress for each upload.
- Unsupported files are rejected with a clear message and do not silently fail.

### 2. Upload and store raw documents
As a user, I want to upload files such as PDF, DOCX, TXT, Markdown, and HTML so that they can be preserved as raw document blobs.

Acceptance criteria:
- The system accepts common document formats.
- Files are stored in a simple folder-based blob store.
- File metadata is recorded, including name, type, size, checksum, upload timestamp, and owner context.

### 3. Digest files into a Space
As a user, I want to drag a source store onto a target Space and choose a digest template so that uploaded files become searchable, curated knowledge inside that Space.

Acceptance criteria:
- The user can drag a source store from the left pane onto a target Space.
- A lightweight digest picker opens with selectable templates such as Courseware, Code Expert, Research Digest, and General Knowledge.
- The user can optionally refine the generated instruction with a short topic or audience hint without being forced to write a full LLM prompt.
- A background or asynchronous job processes the files.
- The uploaded content is extracted, chunked, summarized, and ingested into the Space according to the selected template.
- Progress and error states are visible to the user.
- Advanced users can still open a custom-instruction mode for more precise control.

### 4. Use a Space as an AI expert context
As a user, I want to select a Space during an AI session so that the assistant answers using the curated knowledge from that Space.

Acceptance criteria:
- The AI session supports selecting one or more Spaces as active expertise sources.
- The prompt includes the selected Space context in a structured way.
- Answers are grounded in the Space contents rather than only generic model knowledge.

### 5. Curate patterns from code repositories
As a developer, I want to digest repository patterns, reference implementations, or curated notes into a Space so that the AI can help with coding, debugging, and testing in a consistent way.

Acceptance criteria:
- Code-related materials can be ingested into a Space.
- The Space can represent categories such as architecture, tests, debugging, APIs, and patterns.
- The user can provide explicit guidance such as “focus on variable naming, testing conventions, and error-handling patterns” or “extract best practices and coding guidelines from this repo.”
- The AI uses the curated patterns and the user’s guidance when answering coding questions.

### 6. Support debugging and testing workflows
As a developer, I want the AI to use the curated Space when debugging failures or writing unit tests.

Acceptance criteria:
- The system can answer debugging questions using the curated knowledge.
- The system can suggest test patterns and conventions from the selected Space.
- The answer can reference relevant conceptual categories or source material from the Space.

---

## Functional requirements

### A. Blob storage
The system must support a new folder-based blob store for raw document files.

Requirements:
- Store files on disk in a simple directory structure.
- Keep per-file metadata in a sidecar JSON file or equivalent metadata record.
- Support organization by owner, tenant, source, or Space.
- Avoid using the existing B-tree store for raw file persistence.

Suggested storage model:
- root: `data/blobs/{store_id}/`
- file path: `data/blobs/{store_id}/{sha256}/{filename}`
- metadata: `data/blobs/{store_id}/{sha256}/meta.json`

### B. Supported document formats
The first implementation should support:

- PDF
- TXT
- Markdown
- HTML
- DOCX
- DOC

Future support may include:
- PPTX
- CSV
- EPUB
- image OCR-based extraction

### C. Drag-and-drop upload experience
The user experience should make file ingestion feel simple and natural.

Requirements:
- Provide a drop zone in the target Store UI that accepts one or more files at a time.
- Support drag-and-drop for common document formats such as PDF, DOCX, DOC, TXT, Markdown, and HTML.
- Show upload progress, success/error states, and a clear indication of which files were accepted.
- Allow users to continue working while uploads complete in the background.
- Reject unsupported file types gracefully and explain the reason.

### D. User-guided digestion instructions
The digest workflow should not be a blind one-click action. It should be guided by the user’s intent, but the primary entry point should be a simple drag-and-drop template flow.

Requirements:
- The UI should allow the user to trigger digestion by dragging a source store onto a target Space.
- The first experience should present a small set of digest templates instead of a free-form prompt field.
- The selected template should define what the LLM should prioritize while extracting, summarizing, and categorizing material.
- The system should use the template plus optional audience/topic hints to influence chunking strategy, summarization style, category selection, and item-level knowledge extraction.
- Advanced users can optionally open a custom-instruction mode for more precise control.
- The resolved instruction, template ID, and any optional hints should be stored with the digestion job so it can be reviewed later.

Examples:
- “Courseware”: turn the source material into a student-friendly study guide with topics, key concepts, and review notes.
- “Code Expert”: extract best practices, coding conventions, architecture patterns, testing approaches, and debugging guidance.
- “Research Digest”: extract themes, evidence, structured summaries, and references for later retrieval.
- “General Knowledge”: capture the important themes and summarize them for future use.

### E. Ingestion pipeline
The ingestion workflow should be domain-agnostic and reusable.

Pipeline stages:
1. upload file
2. validate file type
3. store raw file in blob store
4. extract text from the file
5. normalize text
6. split into chunks
7. produce summaries or structured notes
8. create knowledge entries in the target Space

### F. Space-oriented knowledge representation
The target Space should store structured knowledge units that can be retrieved in a targeted way.

Recommended structure:
- categories for major themes,
- items for individual pieces of knowledge,
- summaries for concise semantic signals,
- optional metadata for source file, chunk ID, and document origin.

### G. Integration with Space document references
This feature should be designed to complement the existing Space document-reference capability rather than replace it.

The Space already supports document references through fields such as:
- `doc_id`
- `storage_mode`
- `payload_ref`
- `content_type`
- `cloud_provider`

That means the digest workflow should do the following:

1. Store the uploaded file in the new blob-backed document store.
2. Create or reuse a Space document record that points to that stored blob.
3. Attach the resulting `doc_id` to the generated Space knowledge item.
4. Preserve the payload reference so the item can later surface the source document in the UI.
5. Use the document reference as the source-of-truth link between the curated knowledge item and the raw uploaded file.

This creates a tight relationship between:
- the curated knowledge entry in the Space, and
- the original source document that justified or generated it.

This is especially useful for:
- source tracing,
- document review,
- auditability,
- and later “open source document” experiences inside the Space UI.

### H. Expert-mode prompting
When the user selects one or more Spaces in an AI session, the system should build a prompt context that makes the model behave like a specialist for that Space.

Prompt instructions should include:
- use the selected Space as the primary source of truth,
- prefer grounded answers over generic speculation,
- explain from the retrieved material,
- mention when information is missing or uncertain,
- and maintain the correct tone for the domain.

### I. Automatic Space resolution for research grounding
Research-oriented prompts now support automatic Space selection when the user has not explicitly chosen one.

The resolver follows a simple priority order:
1. Preserve explicit user selection when one or more Spaces are already chosen.
2. Infer the most relevant Space from repo/language context, current task intent, and available Space artifacts.
3. Fall back to LTM only when no suitable Space is confidently identified.

This behavior is intentionally designed to keep Spaces separate from LTM memory. A Space is treated as a first-class, independently attachable expertise source, so switching domains or detaching a Space does not require LTM cleanup or memory churn.

In practice, this means:
- Copilot can automatically switch to a Go, Python, or other language-specific Space when the active work context suggests it.
- The selected Space still receives explicit precedence if the user has already chosen one.
- The system can continue to use LTM as a general fallback without conflating it with the curated Space context.

---

## Proposed architecture

### 1. Storage layer
A new blob store service handles raw file persistence.

Responsibilities:
- save uploaded files,
- create metadata records,
- expose file retrieval,
- provide checksum and file-type validation.

### 2. Ingestion layer
A document-processing service converts raw files into structured knowledge units.

Responsibilities:
- extract text,
- chunk content,
- summarize content,
- create categories and items,
- and submit them to the target Space.

### 3. Space knowledge layer
The existing Space and KnowledgeBase system is reused as the semantic knowledge layer.

This is already aligned with the current repository structure:
- [lib/sop/tools/sopbackend/api_models.go](../lib/sop/tools/sopbackend/api_models.go)
- [lib/sop/tools/sopbackend/space_manager.go](../lib/sop/tools/sopbackend/space_manager.go)
- [lib/sop/tools/sopbackend/main.ai.go](../lib/sop/tools/sopbackend/main.ai.go)

### 4. Expert AI layer
At request time, the system uses the selected Space as an expertise source.

Responsibilities:
- load the active Space context,
- retrieve relevant knowledge chunks/categories,
- build an expert prompt,
- generate a grounded answer.

This should not be tied to a specific domain; it should be generic and reusable.

---

## Source-store side-car model

The digest-to-Space workflow uses a separate side-car source-store layer rather than overloading the core ACID store graph.

### Why this is a side-car model

The main SOP stores remain the authoritative transactional layer for structured runtime data. The new source stores are different:

- they are intake containers for raw material,
- they are lightweight placeholders for future digestion into a Space,
- they can be recreated or refreshed without disturbing the core store model,
- and they are intentionally non-ACID so the implementation stays simple and focused on ingestion and later curation.

### Supported source-store kinds

1. Folder source stores
   - Represent a directory-like container for uploaded files.
   - A folder store is basically a folder plus optional metadata and a manifest for later processing.

2. Repo source stores
   - Represent a repository-backed intake source.
   - A repo store keeps a manifest of repository definitions and any needed credentials references.
   - The manifest is intentionally simple and not a full transactional database record.

### Storage format

The side-car layer is manifest-driven and lives beside the existing store registry files in the database root:

- `storelist.txt` for the current transactional stores
- `folderstorelist.txt` for folder source stores
- `repostorelist.txt` for repo source stores

The Data Manager can display all three lists together in the Stores section, while the digest step later materializes curated knowledge into a Space.

### Lifecycle expectations

A source store is not meant to become a permanent primary storage object. It is a staging object that can be:

- created quickly,
- updated with new files or repo references,
- re-digested into a Space,
- and optionally kept around for future re-curation.

This keeps the primary Space workflow clean while still giving users a low-friction way to accumulate raw source material before turning it into curated expert knowledge.

## Current status and next tasks

### Completed in this pass
- [x] Documented a balanced coding-workflow operating model that favors design-first implementation while treating TDD as an assistive companion.
- [x] Documented late-bound expertise retrieval for coding workflows so the Ask flow stays lean and expertise is consulted near the point of code generation or patching.
- [x] Defined domain-neutral expertise categories including coding best practices, QA/testing practices, code review standards, and implementation quality heuristics.
- [x] Aligned the capability with subscription positioning as a premium value layer for higher-quality engineering assistance.
- [x] Recorded the explicit-state rule: workflow inputs and state must be passed through explicit parameters rather than via ambient context or hidden runtime state.
- [x] Documented the entitlement model: expertise retrieval should be exposed as a plan- or subscription-gated capability, with premium users receiving expert grounding and non-entitled users falling back to standard Ask behavior.

### Immediate next implementation tasks
- [x] Implement late-bound expertise retrieval in the coding workflow path so expertise is consulted only before code generation or patching.
- [ ] Add focused retrieval of short, high-signal expertise capsules from one or more relevant Spaces or knowledge bases.
- [ ] Support domain-neutral expertise sources for coding, QA, and code review guidance.
- [ ] Introduce subscription or entitlement gating for a premium expert-coding mode.
- [ ] Add observability and usage metrics for retrieval hit rate, quality, and adoption.
- [ ] Add product copy and onboarding for the premium experience.

### KB Quality & Expertise Enforcement (New Priority)
**Goal**: Transform KB from "searchable docs" to "automatic expert layer" that enforces quality

See detailed plan: [KB_EXPERTISE_QUALITY_IMPROVEMENT.md](KB_EXPERTISE_QUALITY_IMPROVEMENT.md)

**Phase 1: Enforcement & Validation (Critical)**
- [ ] Implement post-generation pattern validator for error handling, test structure, naming
- [ ] Add automatic revision loop when code violates KB rules
- [ ] Add inline grounding citations (e.g., `// Pattern: error-handling-v2 from Engineering KB`)
- [ ] Track compliance score in CodingMemory
- [ ] Limit revision attempts to prevent loops

**Phase 2: Selection Intelligence (High Priority)**
- [ ] Build task classifier to map task description → KB candidates with confidence scores
- [ ] Implement historical success tracking (task type + KB → outcome)
- [ ] Add automatic KB expansion when initial retrieval is sparse
- [ ] Support multi-KB blending when expertise overlaps
- [ ] Expose KB effectiveness query API

**Phase 3: Feedback & Learning (Medium Priority)**
- [ ] Extend CodingMemory with KBsUsed, RulesApplied, ComplianceScore
- [ ] Implement KB effectiveness scoring (success rate per KB per task type)
- [ ] Add user feedback capture (thumbs up/down) linked to KB usage
- [ ] Build automatic KB pruning for rules with <30% success rate
- [ ] Create KB quality metrics dashboard in Data Manager

**Phase 4: User Experience (Medium Priority)**
- [ ] Implement zero-config automatic KB selection mode
- [ ] Add expertise dashboard UI showing KBs consulted, rules applied, confidence
- [ ] Add explanation UI: "I used X pattern because task mentioned Y"
- [ ] Add refinement controls: "apply stricter adherence", "use different expertise"
- [ ] Make inline annotations toggleable

**Success Metrics**:
- 70%+ generated code passes validation without revision
- 85%+ task classifier accuracy for KB selection
- 90%+ outcomes linked to specific KBs and rules
- KB effectiveness scores correlate with user feedback (r > 0.7)

## Recommended implementation phases

### Phase 1: Blob storage foundation
Tasks:
- add a folder-based blob-store module,
- define metadata schema,
- add upload endpoint and file handling,
- add a drag-and-drop upload experience for the target Store UI,
- validate supported content types,
- add storage tests.

### Phase 2: Document extraction and parsing
Tasks:
- add text extraction support for PDF, TXT, MD, HTML, and DOCX,
- normalize extracted text,
- chunk content,
- add basic error handling and logging.

### Phase 3: Digest-to-Space workflow
Tasks:
- support a chat-driven digest flow from the AI chat window,
- let the user provide a rich natural-language prompt that specifies the target Space, the intended focus, and the desired transformation,
- add a background job to ingest parsed content into the selected Space,
- preserve document references by creating or reusing a Space document entry for each uploaded source file,
- attach the resulting `doc_id` and payload reference to the generated knowledge items,
- show per-file and overall progress,
- support retry and failure states.

### Phase 4: Expert-mode retrieval for AI sessions
Tasks:
- expose a selected-Space context in the AI request payload,
- add prompt-template support for expert-mode behavior,
- route questions to the most relevant categories/items,
- ground answers in the selected Space.

### Phase 5: Coding-session optimization
Tasks:
- support curated code-pattern Spaces,
- add categories for debugging, testing, architecture, and implementation patterns,
- let the AI answer questions like “how do we structure this?” or “what test pattern do we use?” using the selected Space,
- add optional references or citations to the retrieved knowledge.

### Phase 6: Quality and operations
Tasks:
- add observability for uploads, ingestion, and retrieval,
- add metrics and logs for failures and processing time,
- add integration tests for ingestion and prompt grounding,
- add docs and operator guidance.

---

## Suggested UI experience

### Document store view
- list uploaded files,
- show status such as stored, processing, ingested, failed,
- allow delete or reprocess,
- show associated Space and ingestion job state.

### Drag-and-drop digest action
- drag a source store from the left pane onto a target Space,
- choose a digest template such as Courseware or Code Expert,
- optionally provide a short topic or audience hint,
- see status progress and completion,
- and keep an advanced custom-instruction mode available for power users.

### AI session experience
- include a Space dropdown or multi-select,
- optionally enable “expert mode”,
- show which Space(s) are active,
- and allow the AI to answer in a grounded, domain-specific way.

---

## Example use cases

### Courseware example
A user uploads lecture notes and slides. The system digests them into a Space called “Algorithms and Data Structures”. Later the user asks, “Help me write a hashmap.” The assistant uses the curated materials in that Space to explain hashmaps, collisions, complexity, and implementation patterns.

### Coding-session example
A developer uploads architecture notes, internal patterns, or code examples into a Space called “Engineering Playbook”. During a coding session, the developer asks, “How should I structure this handler?” The assistant replies using the curated patterns from that Space.

### Debugging example
A Space contains prior incident notes and fix patterns. The AI uses those materials to help debug a failure or suggest a test case.

### Unit-testing example
A Space contains testing conventions, patterns, and example tests. The AI uses that knowledge to propose test structure and edge-case coverage.

---

## Prompting strategy for expert behavior

The AI should use a structured instruction pattern, such as:

- “You are the expert assistant for the selected knowledge Space.”
- “Use the Space as your primary source of truth.”
- “Prefer grounded answers over generic speculation.”
- “If the information is not present in the selected Space, say so clearly.”
- “Explain the concept first, then provide implementation guidance where appropriate.”

A robust implementation should separate the workflow into:
1. intent classification,
2. category/subspace routing,
3. retrieval from the selected Space,
4. answer generation,
5. optional citation or evidence summary.

---

## Acceptance criteria for MVP

The MVP should deliver:
- raw document upload and storage,
- basic parsing for common text-based documents,
- one-click “Digest to Space” action,
- ingestion into an existing Space,
- selected-Space grounding in AI questions,
- a generic, reusable implementation that works across domains.

The MVP does not need to include:
- full OCR for scanned images,
- perfect category auto-generation,
- highly optimized retrieval,
- or multi-agent orchestration.

---

## Notes for implementation in this repository

The current code already has the right building blocks for this feature:
- existing knowledge-base / Space concepts,
- ingestion models and handlers,
- and AI chat/session context that already supports selected knowledge sources.

This feature should be layered on top of those existing primitives rather than implemented as a separate, disconnected subsystem.
