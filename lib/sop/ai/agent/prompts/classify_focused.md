Classify the user's query using the constraints below.

CONSTRAINTS:
- Entity: "%s"
- Domain: "%s"
- DB Artifact: "%s"

%s

Your job is to identify the intent layers and requested CRUD operations based on the user's query: "%s".
If the Domain or DB Artifact is missing in the constraints, deduce them from the query and the available artifacts.
When the Domain is or may be Stores, parse likely store names from the user's query and match singular/plural variants against available store artifacts before leaving artifact arrays empty.

For Cross-Domain requests, populate `stores_artifacts` and `spaces_artifacts` separately. Use `db_artifacts` only for single-domain compatibility or when there is only one relevant domain-specific artifact list.

If the request is clearly about supported external automation families, populate `automation_families` with only the relevant values from: `postgres`, `splunk`, `git`, `shell`. Leave it empty when the request is not clearly about an automation family.

Operational Layers definition:
- "Single-Domain": Operations restricted to a single domain (either Stores or Spaces).
- "Cross-Domain": Operations coordinating across multiple domains (mixing Stores and Spaces).
  *Disambiguation: If the words "store" or "space" are just part of normal content or a category name, keep the request Single-Domain. Use Cross-Domain only when the request operates across both Stores and Spaces.*

Respond with JSON only in this schema. Fill in the Domain and DB Artifact if they were missing.
{
  "entity": "%s",
  "domain": "%s",
  "db_artifacts": ["%s"],
  "stores_artifacts": ["%s"],
  "spaces_artifacts": ["%s"],
  "automation_families": ["postgres"],
  "layers": [
    {"name": "Single-Domain", "crud": ["C", "R"]}
  ]
}
