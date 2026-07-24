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

Provide a practical AI operating layer for knowledge work where users can:

- upload document blobs,
- drag and drop files into a target Store,
- store them as raw files,
- digest them into a Space,
- and later ask AI questions grounded in that Space.

The key product story is not a standalone chatbot. It is a durable body + memory layer for AI that can be used through workflows, grounded research, and tool-driven execution. The underlying reasoning can be powered by any supported LLM provider, while the product value comes from the memory, context continuity, and workflow engine around it.

Core architectural principle: Spaces and Knowledge Bases are semantic memory systems, not just document stores. Categories, documents, workflow items, and references all participate in a shared retrieval model where vector and tensor attributes are first-class data. That means every ingestion, digest, and workflow step strengthens the semantic search layer, so retrieval, grounding, ranking, and similarity-based exploration improve automatically across the product.

### Positioning: an integrated AI memory system

This product should be framed as an AI database and AI memory system, not merely as a retrieval layer or a passive vector index. Competitors may offer similar functionality, but their memory systems are often loosely coupled, asynchronously synchronized, and dependent on large remote infrastructure. In practice, their semantic indexes can lag behind the latest thoughts, documents, and context by hours or more.

Our approach is different. The memory, semantics, and workflow layers are tightly integrated so the system behaves more like a human memory: compact, immediate, and self-contained, with the ingredients needed for recall, grounding, and continuity in one place. That makes it possible to ship the experience as an application that runs on a laptop, desktop, or local machine rather than requiring a giant data-center cluster. The product story is not simply “another cloud search layer”; it is a portable AI memory system that can operate wherever the user works.

This should work for any domain, not just technical or educational content.

---

## Current product model: Space, KB, research, and persona/tone
The current implementation is best described as a grounded expertise stack rather than a separate "avatar" product. In practice:

- Space / KB: the source of truth for domain expertise and curated knowledge.
- Research: retrieval and synthesis over the active Space / KB to find the most relevant material.
- Persona / tone: optional style and role guidance such as "senior engineer", "calm mentor", or "strict reviewer".
- Workflow: structured execution and tool use when the task requires more than a single answer.

This documentation does not describe a futuristic, fully simulated identity. The practical model is: select a Space or KB, optionally attach persona / tone guidance, and let the assistant answer using grounded research and the current workflow context. The product value comes from shipping a reliable body and memory layer to the user, while the underlying LLM can be swapped as needed. Because vector and tensor attributes live at the heart of the Space/KB model, every interaction benefits from richer semantic search, retrieval quality, and contextual grounding.

---

## Space and Store Naming Conventions

### Automatic Name Normalization

When creating Spaces or Stores, the system automatically normalizes names to ensure backend compatibility across all storage systems (including Cassandra, which has strict table naming requirements):

**User Input → Storage → Display**
- User types: `"Core Playbook"` 
- System stores: `"core_playbook"` (lowercase with underscores)
- UI displays: `"Core Playbook"` (title case with spaces)

**Normalization Rules:**
1. **Spaces are converted to underscores**: `"My Space"` → `"my_space"`
2. **Converted to lowercase**: `"MySpace"` → `"myspace"`
3. **Only alphanumeric, underscore, and hyphen allowed**: `[a-zA-Z0-9_-]`
4. **Display names are auto-generated**: Underscores become spaces, words are title-cased

**Examples:**
```
Input              → Stored As          → Displayed As
"Core Playbook"    → "core_playbook"    → "Core Playbook"
"my custom space"  → "my_custom_space"  → "My Custom Space"
"shared_memory"    → "shared_memory"    → "Shared Memory"
"test-space"       → "test-space"       → "Test-Space"
```

**Why This Matters:**
- **Backend Safety**: Ensures compatibility with all storage backends (Cassandra, filesystem, etc.)
- **No Ambiguity**: Prevents confusion between "Core Playbook" and "core_playbook" being treated as different spaces
- **URL Safe**: Names can be safely used in URLs and file paths
- **Predictable**: Users can type naturally with spaces, system handles the rest

**Developer Note**: 
- Always use the normalized (stored) name when referencing Spaces/Stores in code
- Use `resolveSpaceDisplayName()` or `resolveStoreDisplayName()` for UI display
- The normalization happens at the API boundary in `handleCreateSpace` and `handleAddStore`

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

## Space UI model: first-class resources

The Space experience should not treat workflows, documents, and references as hidden metadata. They should be surfaced as native objects in the same explorer used for categories and content items.

### Native object types
- Categories: primary navigation structure and grouping mechanism
- Content items: ordinary knowledge entries or references
- Workflow items: first-class leaf resources with their own editing surface
- Documents: references into the Space document store, surfaced under a built-in Documents root container and optionally under per-category document sections

### UX behavior
- The category tree remains the primary navigation surface.
- A built-in Documents root node exposes every document in the Space and serves as a global browse entry.
- Categories may optionally expose a Documents section so users can see documents associated with that branch.
- Workflow-backed items appear as workflow nodes with their own studio/editor experience instead of being treated as generic content.
- Items can optionally reference another item or category to support semantic indexing, composition, and cross-linking.
- The detail surface adapts to the selected object: workflow studio for workflows, preview/metadata for documents, and the standard item editor for content.

### Proposed document browsing experience
When the user selects the Documents root node or a category that has document associations, the UI should present a document gallery rather than a plain list.

Suggested interaction model:
1. Show each document as a card with title, icon, type, size, upload date, source, and a short excerpt or summary.
2. Allow the user to filter cards by type, category, or recency.
3. Selecting a card opens a detail panel or full-page view that shows document metadata and a content preview.
4. The detail surface should include key metadata such as document ID, storage mode, content type, provider, checksum or payload reference, and the categories/items that reference it.
5. For supported types, the preview area should render the actual blob content instead of only showing metadata.

Recommended renderer strategy:
- Text, Markdown, and HTML: inline reader with syntax-aware rendering.
- PDF: embedded preview if the browser supports it; otherwise a fallback open/download action.
- Images: thumbnail preview with zoom or lightbox support.
- DOCX or PPTX: text extraction preview plus metadata and download fallback.
- Audio or video: lightweight inline player when the content type supports it.

This should be implemented as a type-aware renderer layer that chooses the best preview experience based on the document’s content type and available storage metadata. The minimal version can ship with cards + metadata + text preview, while the richer blob renderer can be layered on top as a second step.

### Storage semantics
The UI should make it clear that documents are stored once in the Space document store and then referenced by category/item associations through DocID-based links. This keeps the product experience simple while preserving the underlying storage model.

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

### H. First-class UI surface for categories, workflows, and documents
The Space UI should present the following as first-class resources:
- categories for navigation and organization,
- workflow-backed items for step-based authoring,
- documents as references into the Space document store,
- and cross-resource links that support semantic indexing and composition.

This experience should be additive to the existing category tree rather than replacing it. Users should be able to browse a Space in a familiar way while still discovering workflow and document resources through explicit entry points such as the Documents root node, per-category document sections, and workflow leaf nodes.

### I. Expert-mode prompting
When the user selects one or more Spaces in an AI session, the system should build a prompt context that makes the model behave like a grounded specialist for that Space.

Prompt instructions should include:
- use the selected Space as the primary source of truth,
- prefer grounded answers over generic speculation,
- explain from the retrieved material,
- mention when information is missing or uncertain,
- maintain the correct tone for the domain,
- and apply any optional persona / tone guidance that was attached to the selected Space or session context.

### J. Automatic Space resolution for research grounding
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

The AI Studio can display all three lists together in the Stores section, while the digest step later materializes curated knowledge into a Space.

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
- [ ] Create KB quality metrics dashboard in AI Studio

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
- “Apply the attached persona / tone guidance when it is relevant.”
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
# Spaces User Guide

**What are Spaces?** Knowledge-backed expertise sources that improve research, coding, and response quality. They are not separate futuristic avatars; they are practical, attachable contexts that provide grounded knowledge and optional persona / tone guidance.

---

## Quick Start

### What You Get

When you ask the AI to write code, **Spaces work behind the scenes** to:
- ✅ Apply proven patterns from your knowledge base
- ✅ Catch common mistakes before you see them
- ✅ Suggest the right approach for each task
- ✅ Learn from your feedback to get better over time

**No setup required.** Spaces activate automatically when you have knowledge bases configured.

---

## How Spaces Work

### 1. Automatic Expert Selection

The AI analyzes your request and picks the most relevant Spaces:

```
You: "implement user authentication"
🤖 AI uses: go-best-practices, security-guidelines
```

```
You: "fix this test failure"
🤖 AI uses: testing-patterns, debugging-checklist
```

**Task Types Recognized:**
- **Implementation**: Building new features
- **Debugging**: Finding and fixing bugs
- **Testing**: Writing or updating tests
- **Refactoring**: Improving existing code
- **Documentation**: Writing docs or comments
- **Architecture**: Design and structure decisions

### 2. Code Validation

Generated code is automatically checked against Space rules:

**Before (without Spaces):**
```go
// Code might have issues
func Process(data string) string {
    result := transform(data)  // What if this panics?
    return result
}
```

**After (with Spaces):**
```go
// Space: go-best-practices applied
func Process(data string) (string, error) {
    result, err := transform(data)
    if err != nil {
        return "", fmt.Errorf("transform failed: %w", err)
    }
    return result, nil
}
```

### 3. Automatic Revision

If validation finds issues, the AI **fixes them automatically** (up to 2 attempts):

```
🤖 Generated code
⚠️  Space found 3 violations: missing error handling
🔧 Revising code...
✅ Fixed! All Space rules satisfied
```

You get the corrected code on the first try.

---

## Subscriber-Only: Import a KB JSON File into a Space

This workflow is available to subscribed users who want to bring their own curated expertise into Spaces.

### When to Use It

Use this flow when you want to:
- Seed a Space with your own coding standards, architecture guidance, or product playbooks
- Import a curated Knowledge Base export that was generated from Markdown or another source
- Make that knowledge available to the copilot during coding, review, and research tasks

### Preparing Your Knowledge Base

**Step 1: Author Curated Markdown**

Create a structured Markdown file using the Item block format:

```markdown
# Coding Best Practices
## Go

- Item: Use structured error handling
  Summary: Return errors explicitly and wrap with context.
  Summary: Use fmt.Errorf with %w for error wrapping.
  Body:
  Go does not use exceptions. Return errors explicitly,
  check them early, and wrap them with context using
  fmt.Errorf with %w. Never ignore errors with _.
  Sources: https://go.dev/doc/effective_go
```

**Step 2: Compile to Self-Contained JSON Bundle**

Use the Knowledge Base Compiler to convert your Markdown into a portable JSON bundle:

```bash
go run ./lib/sop/ai/cmd/knowledge_compiler/main.go -input my_kb.md
```

The compiler automatically:
- ✅ Extracts categories from headings (# ## ###)
- ✅ Parses Item blocks into retrievable knowledge items
- ✅ Discovers referenced documents from Sources fields
- ✅ Embeds full document content into the bundle
- ✅ Creates a self-contained JSON file ready for import

**What Gets Bundled:**
- All Item blocks as retrievable knowledge
- Full content of any .md files referenced in Sources
- Category hierarchy and metadata
- Everything needed for offline use

**Result:** One `.json` file that works anywhere, no external dependencies.

### Import Steps

1. Open the AI Studio or the Space view in your app.
2. Create a new Space or select an existing one as the destination.
3. Click the **Import JSON** action.
4. Choose the Knowledge Base JSON file (compiled bundle).
5. Confirm the import and wait for the Space contents to load.
6. Start a coding or research workflow and let the copilot use the imported expertise.

**What Happens During Import:**
- Items are indexed for semantic search (3-6 items per query)
- Referenced documents are stored in the Space's document store
- Categories are organized for efficient retrieval
- Everything works offline once imported

### Best Practices

**For Markdown Authoring:**
- Keep Items focused on one concept each
- Use 1-5 Summary lines per Item (for semantic matching)
- Put detailed examples in the Body
- Reference supporting docs in Sources (they'll be auto-bundled)

**For Bundle Compilation:**
- Run compiler after updating source Markdown
- Test bundles in a small Space before broad distribution
- Keep category depth to 2-3 levels for best retrieval

**For Space Import:**
- Keep imported KB focused on a clear domain
- Re-import updated JSON after revising source
- Give feedback to tune Space effectiveness

### Note for Subscribers

This import path is part of the premium experience because it enables organization-specific and domain-specific expert knowledge to become first-class context for coding workflows. Built-in Spaces still work for general use, but importing your own KB JSON is how you turn curated guidance into reusable expertise inside a Space.

---

## Giving Feedback

Help Spaces learn what works for you:

### Feedback Buttons

When Spaces are used, you'll see:

```
Space Used: go-best-practices
👍 Helpful  👎 Needs Work
```

**👍 Click "Helpful" when:**
- Code works correctly on first try
- Patterns match your team's style
- Error handling is appropriate
- Tests are well-structured

**👎 Click "Needs Work" when:**
- Code doesn't match your requirements
- Patterns feel too strict or too loose
- Better approach should have been used
- Validation missed real issues

### Why Feedback Matters

Your feedback trains the effectiveness tracker:
- ✅ **Good feedback** → Space used more often for similar tasks
- ❌ **Negative feedback** → Space used less often, alternatives tried
- 📊 **Patterns emerge** → System learns your preferences over time

**Privacy Note:** Feedback is stored locally on your device. No code is sent to external servers.

---

## Understanding Explanations

Click the "How I used expertise" section to see:

### What Spaces Were Used
```
## Knowledge Bases Selected
I selected 2 Spaces: go-best-practices and testing-patterns
```

### Why They Were Chosen
```
## How I Approached This Task
I classified this as an implementation task.
Based on your file types (.go) and keywords like "implement"...
```

### What Changed
```
## How Spaces Influenced the Code
- Added error handling (go-best-practices)
- Used table-driven tests (testing-patterns)
- Applied naming conventions (go-best-practices)
```

### Quality Score
```
## Validation Results
✅ The generated code has 0 Space rule violation(s)
Compliance: 100% (all rules satisfied)
```

---

## Advanced Usage

### Importing a KB JSON File onto a Space

If you have a curated Knowledge Base export, you can import it into a Space so the copilot can use it as domain expertise during coding, research, and review tasks.

#### Categorizer support in phase 1

Phase 1 now preserves and rehydrates a Space's categorizer policy during JSON import/export. If the exported KB config includes a `categorizer_config` block, the imported Space restores that policy and uses it for routing and retrieval.

For the planned v1 general-purpose categorizer design, see [CATEGORIZER_V1_DESIGN_PLAN.md](CATEGORIZER_V1_DESIGN_PLAN.md). The v1 direction is an in-process, taxonomy-aware categorizer that uses embeddings plus category structure rather than an external dependency.

This is intended for domain-specific Spaces such as the coding best-practices KB:
- rule-based mappings can route queries into the right category branches,
- the imported Space can use the same routing policy during research and Copilot consumption,
- and the behavior is backward-compatible with older JSON exports that do not include categorizer config.

Example shape:

```json
{
  "config": {
    "categorizer_config": {
      "type": "rule",
      "default": {
        "categories": [
          { "path": "Coding Best Practices/Go", "keywords": ["go", "golang", "context"] }
        ]
      },
      "fallback": "uncategorized"
    }
  }
}
```

**Complete Workflow:**

1. **Author:** Create structured Markdown with Item blocks
2. **Compile:** Run Knowledge Base Compiler to generate self-contained JSON
   ```bash
   go run ./lib/sop/ai/cmd/knowledge_compiler/main.go -input my_kb.md
   ```
3. **Import:** Open Space view, choose Import JSON, select the bundle
4. **Use:** Space automatically applies during AI-assisted coding

**Bundle Contents:**
- **Items:** Retrievable knowledge (3-6 per query via semantic search)
- **Documents:** Full source docs for detailed context on demand
- **Categories:** Organized hierarchy for efficient navigation
- **Categorizer policy (phase 1):** Optional routing rules that are restored with the Space and used by the runtime for retrieval and workflow routing

**Why Bundled Documents Matter:**
- Items provide quick facts (optimized for retrieval)
- Documents provide deep learning (full guides, examples, tutorials)
- Progressive disclosure: summary → detail as needed
- Fully self-contained: no external file dependencies

This is especially useful for teams that want policy, architecture, coding standards, or domain guidance to become part of the AI workflow automatically.

### Manual Space Selection (Coming Soon)

Disable auto-selection if you prefer manual control:
1. Click settings gear in chat
2. Toggle "Auto-select Spaces based on task" → OFF
3. Select Spaces manually before each query

### Citation Comments

Space attributions are added as comments:

```go
// Space Patterns Applied: [error-handling, table-tests] from [go-best-practices]
func TestUserAuth(t *testing.T) {
    // ...
}
```

**Toggle visibility:**
- Press `Ctrl+Shift+K` to show/hide citations (coming soon)
- Or disable in settings

### Session Override (Coming Soon)

Temporarily disable a Space:
1. Click "Needs Work" feedback
2. Select "Don't use this Space again this session"
3. Space skipped until you restart chat

---

## Quality Dashboard (Admin)

**Location:** http://127.0.0.1:9090/kb-quality (local daemon) or your deployment URL

### What You Can See

**Overview Stats:**
- Total Spaces tracked
- Average success rate across all Spaces
- Critical issues requiring attention
- Total uses (all-time)

**Top Performers:**
- Spaces with highest success rates
- Usage counts and task types
- Confidence scores

**Poor Performers:**
- Spaces with low success rates (<50%)
- Reasons: insufficient data, declining performance, or always failing
- Recommended actions: review, update, or disable

### Actions Available

**Run Quality Check Now:**
- Triggers immediate analysis
- Updates all metrics
- Identifies new issues

**Download Full Report:**
- Text format for offline review
- Includes all metrics and recommendations
- Suitable for team review meetings

**Refresh Data:**
- Reloads dashboard with latest data
- Updates after background jobs complete

---

## Troubleshooting

### "No Spaces available for this task"

**Cause:** No knowledge bases match your task type.

**Solutions:**
1. **Create relevant Spaces:** Add markdown files to your knowledge base folder
2. **Broaden task description:** Use keywords the AI recognizes
3. **Check Space status:** Visit quality dashboard to ensure Spaces are enabled

### "Space validation too strict"

**Cause:** Validation rules catching too many false positives.

**Solutions:**
1. **Give feedback:** Click "Needs Work" to lower Space priority
2. **Review Space content:** Update rules to be more flexible
3. **Use override:** Disable Space for current session (coming soon)

### "Feedback buttons not appearing"

**Cause:** Spaces weren't used for this generation.

**Check:**
1. Task type recognized? (implementation, debugging, testing, etc.)
2. Relevant Spaces exist? (visit quality dashboard)
3. Auto-selection enabled? (check settings)

### "Revision loop taking too long"

**Cause:** Complex validation issues requiring multiple fixes.

**Note:** System limits to 2 revision attempts automatically.

**If persistent:**
1. Give "Needs Work" feedback
2. Review Space rules for conflicts
3. Simplify validation rules temporarily

---

## Best Practices

### For Individual Users

**1. Give Regular Feedback**
- Aim for 50%+ of generations
- Be specific in optional comments
- Consistent feedback improves accuracy faster

**2. Review Explanations**
- Check "How I used expertise" after each generation
- Understand why Spaces were chosen
- Learn which Spaces work best for which tasks

**3. Monitor Quality Dashboard**
- Check weekly for trends
- Act on poor performers (update or disable)
- Celebrate top performers (share patterns with team)

**4. Curate Your Spaces**
- Keep knowledge bases up to date
- Remove outdated patterns
- Add new patterns as you learn

### For Teams

**1. Establish Space Ownership**
- Assign maintainers to each Space
- Regular review cycles (monthly)
- Document decision rationale in Space content

**2. Share Feedback Insights**
- Export quality reports for team meetings
- Discuss poor performers collaboratively
- Identify patterns in successful Spaces

**3. Create Domain-Specific Spaces**
- Backend: error handling, database patterns
- Frontend: component structure, state management
- Testing: test patterns, mocking strategies
- Security: auth patterns, input validation

**4. Version Control Your Spaces**
- Commit Space content to git
- Track changes over time
- Rollback if quality degrades

**5. Onboard New Members**
- Share this guide with new team members
- Explain your team's Spaces and their purposes
- Encourage feedback from fresh perspectives

---

## FAQ

**Q: Can I disable Spaces entirely?**
A: Yes. Simply remove all knowledge bases from your configuration, or disable auto-selection in settings.

**Q: Do Spaces send my code to the cloud?**
A: No. All Space operations run locally on your device. Only your configured LLM provider (Gemini, OpenAI) receives code when generating responses.

**Q: How many Spaces should I have?**
A: Start with 3-5 focused Spaces. Quality > quantity. Add more as you identify gaps.

**Q: Can I use someone else's Spaces?**
A: Yes! Import their knowledge base files into your Spaces folder. Give feedback to tune them for your needs.

**Q: What happens if two Spaces conflict?**
A: The system blends rules and applies the most specific guidance. If conflicts persist, give feedback to help prioritize.

**Q: How long until feedback improves results?**
A: Noticeable improvement after 10-15 feedback submissions per Space. System uses recency weighting (recent feedback counts more).

**Q: Can I see historical effectiveness data?**
A: Yes, via the Quality Dashboard at `/kb-quality`. Shows trends, success rates, and usage over time.

**Q: What if validation blocks legitimate code?**
A: Click "Needs Work" and add a comment explaining why. This trains the system. Future versions will support session overrides.

---

## Getting Help

**Documentation:**
- [Architecture Overview](ARCHITECTURE.md)
- [Space Implementation Guide](SPACE_EXPERT_IMPLEMENTATION_GUIDE.md)
- [KB Quality User Guide](KB_QUALITY_USER_GUIDE.md)
- [KB Custom Validators Guide](KB_CUSTOM_VALIDATORS_GUIDE.md)
- [KB Task Classification Guide](KB_TASK_CLASSIFICATION_GUIDE.md)
- [KB Quality Remaining Enhancements](KB_QUALITY_REMAINING_ENHANCEMENTS.md)

**Developer Resources:**
- `lib/sop/ai/agent/codingworkflow/` - Core implementation
- `lib/sop/ai/memory/kb_effectiveness.go` - Effectiveness tracking
- `lib/sop/tools/sopbackend/templates/kb_quality.html` - Dashboard UI

**Support:**
- Check Quality Dashboard for system health
- Review effectiveness metrics for patterns
- Export reports for detailed analysis

---

## What's Next

**Coming Soon:**
- Zero-config mode toggle (auto vs manual selection)
- Citation visibility keyboard shortcut
- Session-based Space overrides
- Expertise dashboard (per-session usage view)
- Refinement controls ("Apply Stricter", "Try Different Space")

**Planned:**
- Multi-Space blending (combine expertise from 2-3 Spaces)
- Custom validation rules (team-specific patterns)
- Space versioning and rollback
- Community Space marketplace

---

**Remember:** Spaces learn from your feedback. The more you use them and rate the results, the better they become at matching your style and needs.

**Start simple.** Give feedback on every few generations. Check the dashboard weekly. Your AI coding assistant will improve continuously.
