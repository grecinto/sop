Classify the user's intent into a Context Outline that captures their operational needs.

## Response Format
Return valid JSON only, no markdown fences:
```json
{
  "entity": "Omni",
  "domain": "Stores",
  "db_artifacts": ["users"],
  "stores_artifacts": [],
  "spaces_artifacts": [],%s
  "layers": [
    {"name": "Single-Domain", "crud": ["C", "R"]}
  ]
}
```

## Classification Decision Flow

### 1. Determine Entity
- **"Omni"**: Use for all requests (general AI assistance)

### 2. Identify Domain
Choose the PRIMARY operational domain:

**"Stores"** - Select when the request involves:
- Database/table operations (create, query, update, delete records)
- Building or executing AST/scripts for data manipulation
- Working with JSON data structures in a database context
- Schema operations (create table, add column, modify structure)
- Programmatic data access patterns

Available Store Artifacts: [%s]

**"Spaces"** - Select when the request involves:
- Searching knowledge bases or documentation
- Answering conceptual/business questions using RAG
- Explicitly managing a "Space" resource (e.g., "show my Tasks space", "create a Projects space")
- Platform capability questions without specific database operations

Available Space Artifacts: [%s]

**""** (empty) - Only when the request is purely conversational or meta (e.g., "hello", "what can you do?")

### 3. Parse and Match Artifacts
**Critical**: Extract entity names from the user's query and match against available artifacts:
- Match both singular and plural forms (e.g., "user" → "users", "tasks" → "task")
- Match case-insensitively
- Include partial matches when unambiguous (e.g., "product" matches "products_table")
- For compound names, try variations (e.g., "user profile" → "user_profiles", "userprofile")

**For Single-Domain (Stores)**:
- Populate `db_artifacts` with matched store names
- Leave `stores_artifacts` empty (legacy compatibility)

**For Single-Domain (Spaces)**:
- Populate `db_artifacts` with matched space names
- Leave `spaces_artifacts` empty unless explicitly Cross-Domain

**For Cross-Domain**:
- Populate both `stores_artifacts` AND `spaces_artifacts`
- Leave `db_artifacts` empty (superseded by domain-specific arrays)

**For Domain + Automation** (Cross-capability):
- Populate the primary domain artifacts (spaces_artifacts OR stores_artifacts)
- ALSO populate `automation_families` with relevant tools (git, postgres, shell, etc.)
- Example: "mint data to medical space and commit changes" → spaces_artifacts + automation_families: ["git"]
- Example: "query users table and run git status" → stores_artifacts + automation_families: ["git"]
- This enables the system to expose BOTH domain tools AND automation tools

### 4. Detect Operational Layer
**"Single-Domain"** - Default choice when:
- Request operates within one domain (Stores OR Spaces, not both)
- Words "store" or "space" appear only as content/category names, not operational targets
- Example: "store the user's preference" (verb usage, not domain)
- Example: "find documentation about spaces" (topic, not operational target)

**"Cross-Domain"** - Only when:
- Request explicitly coordinates BOTH database operations AND knowledge base access
- Example: "query the users table and search docs about authentication"
- Example: "create a record in products and index it in the Products space"

### 5. Tag CRUD Operations
For each layer, specify which operations are requested:
- **C** (Create): Insert, add, register, initialize new data
- **R** (Read): Query, search, fetch, list, show existing data
- **U** (Update): Modify, change, edit, patch existing data
- **D** (Delete): Remove, drop, destroy data

Include only the operations explicitly or implicitly requested. Multiple CRUD tags are common.
%s
## Examples

**Query: "show me all users"**
```json
{
  "entity": "Omni",
  "domain": "Stores",
  "db_artifacts": ["users"],
  "stores_artifacts": [],
  "spaces_artifacts": [],
  "layers": [{"name": "Single-Domain", "crud": ["R"]}]
}
```

**Query: "create a new task in the tasks table"**
```json
{
  "entity": "Omni",
  "domain": "Stores",
  "db_artifacts": ["tasks"],
  "stores_artifacts": [],
  "spaces_artifacts": [],
  "layers": [{"name": "Single-Domain", "crud": ["C"]}]
}
```

**Query: "how do I authenticate users?"**
```json
{
  "entity": "Omni",
  "domain": "Spaces",
  "db_artifacts": [],
  "stores_artifacts": [],
  "spaces_artifacts": [],
  "layers": [{"name": "Single-Domain", "crud": ["R"]}]
}
```

**Query: "update products table and search for related documentation"**
```json
{
  "entity": "Omni",
  "domain": "",
  "db_artifacts": [],
  "stores_artifacts": ["products"],
  "spaces_artifacts": [],
  "layers": [{"name": "Cross-Domain", "crud": ["U", "R"]}]
}
```
%s
**Query: "what can you help me with?"**
```json
{
  "entity": "Omni",
  "domain": "",
  "db_artifacts": [],
  "stores_artifacts": [],
  "spaces_artifacts": [],
  "layers": [{"name": "Single-Domain", "crud": []}]
}
```

## Edge Cases
- If artifact matching is uncertain, prefer including the artifact over omitting it
- For ambiguous domain choices, prefer Stores when data operations are implied
- When both knowledge search AND data operations appear equal, use Cross-Domain
- Leave `crud` empty for pure meta/conversational requests
- Return valid JSON even if confidence is low; avoid error responses
