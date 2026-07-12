# Proposal: KB + Categorizer Bundle Support for Space JSON

## Goal

Enable a Space/KnowledgeBase export to carry the Categorizer it depends on, so importing the JSON recreates both the KB and its paired Categorizer in the same workflow.

For the planned v1 general-purpose categorizer approach, see [CATEGORIZER_V1_DESIGN_PLAN.md](CATEGORIZER_V1_DESIGN_PLAN.md).

## Why this is needed

The current export/import flow already preserves KB config, but it does not make the Categorizer pairing explicit. That creates three problems:

- The pairing is implicit rather than durable.
- Rich Categorizer implementations (hybrid, domain-specific, registry-backed) are harder to recover reliably after import.
- The import workflow cannot confidently restore the same semantic routing behavior that the original KB used during retrieval and storage.

## Proposed model

We can support this with a backward-compatible bundle contract that uses the existing KB config as the portable core payload.

### Option A: Minimal, low-risk path

Keep the existing `config.categorizer_config` field as the canonical portable definition and ensure it is fully round-tripped during Space JSON import/export.

This is already aligned with the current memory model in [lib/sop/ai/memory/models.go](lib/sop/ai/memory/models.go) and the runtime wiring in [lib/sop/ai/memory/manager.go](lib/sop/ai/memory/manager.go).

### Option B: Explicit pairing path

Add an optional top-level bundle object to the exported Space JSON and a lightweight reference in KB config.

Example shape:

```json
{
  "config": {
    "type": "space",
    "categorizer_ref": "acme-kb-categorizer",
    "categorizer_config": {
      "type": "hybrid",
      "default": {
        "categories": [
          { "path": "ops/incident", "keywords": ["incident", "outage"] }
        ]
      },
      "fallback": "uncategorized"
    }
  },
  "categorizer_bundle": {
    "id": "acme-kb-categorizer",
    "version": "1",
    "kind": "hybrid",
    "config": {
      "type": "hybrid",
      "default": {
        "categories": [
          { "path": "ops/incident", "keywords": ["incident", "outage"] }
        ]
      },
      "fallback": "uncategorized"
    },
    "source": "embedded"
  },
  "categories": [],
  "items": []
}
```

This keeps the existing format usable while making the KB-to-Categorizer pairing explicit.

## Recommended implementation plan

### Phase 1: Preserve and reapply Categorizer config

Use the existing KB config path as the first milestone.

- Extend the export/import path in [lib/sop/ai/memory/knowledge_base_export.go](lib/sop/ai/memory/knowledge_base_export.go) to ensure the Categorizer config is preserved exactly.
- On import, apply the imported config back into the KB runtime using the existing configuration path in [lib/sop/ai/memory/manager.go](lib/sop/ai/memory/manager.go).
- Keep this fully backward-compatible with older Space JSON files.
- For rebuild-style imports such as daemon_user_kb, add an opt-in bootstrap path: when the payload has no explicit Categorizer policy and the importer is asked to bootstrap, synthesize a lightweight rule-based policy from the imported category structure and attach it to the KB.

The import option should be explicit and conservative:

- If a Categorizer policy already exists, do not overwrite it.
- If no policy exists and bootstrapping is requested, generate one from the imported category paths/names.
- If bootstrapping is not requested, leave the KB without a Categorizer policy.

### Phase 2: Add explicit bundle metadata

Introduce a new optional field in the export payload:

- `categorizer_bundle`
- `categorizer_ref` inside KB config

The importer should then:

1. read the KB config,
2. read the optional bundle,
3. resolve the bundle by `id` if a registry is available,
4. otherwise install from the embedded config,
5. attach the resulting Categorizer to the KB.

### Phase 3: Registry-backed pairing

For richer workflows, support named bundles and registry lookups.

- A bundle can be identified by a stable `id` and `version`.
- Import can prefer a local registry entry if available.
- If a bundle is missing, the importer falls back to the embedded config and preserves usable behavior.

## Workflow integration points

### Import/Export APIs

The existing Space import/export paths should become the canonical place for bundle handling.

- Export should include the bundle when a KB has a configured Categorizer.
- Import should restore the Categorizer and make it active for that KB.

### Agent and Space APIs

The Space management workflow in [lib/sop/ai/agent/api_space.go](lib/sop/ai/agent/api_space.go) should preserve the pairing when a Space is created or updated.

That means:

- `UpdateSpaceConfig` should accept and preserve `categorizer_ref` and the related config.
- `ImportJSON` should rehydrate the Categorizer immediately after the KB config is restored.
- `CreateSpace` should allow a bundle-aware import flow to pair the new KB with a Categorizer on creation.

### UI / CLI experience

For users, the workflow should feel like a single bundle operation:

- Export Space JSON includes the KB + Categorizer bundle automatically.
- Import Space JSON applies the bundle and reports whether the pairing was restored.
- If a bundle cannot be resolved, the UI should show a clear fallback message and preserve the embedded config.

## Data contract recommendation

The lowest-friction contract is:

- keep `categorizer_config` in KB config for compatibility,
- add `categorizer_bundle` as optional top-level metadata,
- add `categorizer_ref` as a stable KB-level pointer to the bundle.

This gives us a portable schema without breaking existing JSON imports.

## Suggested implementation sketch

1. Add a `CategorizerBundle` struct to the memory models.
2. Extend the export payload with an optional `categorizer_bundle` field.
3. On import, rehydrate the KB config first, then attach the Categorizer from the bundle or fallback config.
4. Add regression tests for:
   - round-trip export/import of a KB with a Categorizer,
   - import fallback when no bundle is present,
   - import reapplication of a domain-specific Categorizer.

## Why this fits the existing codebase

This proposal fits the current architecture well because:

- the KB config already has a Categorizer payload,
- the memory manager already knows how to turn config into a runtime Categorizer,
- the export/import layer already owns the Space JSON contract,
- the agent APIs already work with KB config and Space lifecycle operations.

## Recommendation

Proceed with Phase 1 immediately and add Phase 2 as the next step. That gives us a robust, backwards-compatible workflow quickly while keeping a clear path to richer bundle-based pairing later.

## Localization and future multilingual support

The current categorizer implementation is intentionally simple and works best when the category path and keywords are expressed in the same language as the input text. That is a good fit for the initial English-first rollout, but it should not become the long-term contract.

### Design goals for future localization

- Keep the categorizer policy portable and not hard-wired to a single human language.
- Allow the same taxonomy to be reused across languages by carrying locale-aware keyword sets and optionally locale-specific aliases.
- Support a future `locale` or `language` field in the categorizer policy so the runtime can select the appropriate keyword table for a given input.
- Preserve the existing English-first config format for backward compatibility while making it easy to add localized alternatives later.

### Suggested extension shape

A forward-compatible shape could look like this:

```json
{
  "type": "rule",
  "default": {
    "categories": [
      {
        "path": "Engineering/Go",
        "keywords": ["go", "golang", "context"],
        "aliases": {
          "zh-CN": ["Go", "Golang", "上下文"]
        }
      }
    ]
  },
  "fallback": "uncategorized",
  "locale": "en"
}
```

### Runtime behavior recommendation

When localization is introduced, the categorizer should:

1. normalize the input text using a locale-aware tokenizer,
2. select the matching keyword set for the requested locale or a fallback locale,
3. score the category path against the relevant localized aliases,
4. preserve the same taxonomy path structure so routing remains consistent across languages.

### Why this matters

This keeps the routing model scalable while avoiding the trap of making the category taxonomy itself language-dependent. The taxonomy remains the stable routing scaffold, while the keyword/alias layer becomes language-aware.

### Implementation guidance

For the next phase, we should keep the runtime behavior compatible by:

- treating the existing `keywords` field as the default language entry,
- adding optional locale-specific maps later without breaking older configs,
- and ensuring the categorizer score path uses the selected locale rather than assuming English tokens.
