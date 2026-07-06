# Space Federation Manual Testing Guide

This guide demonstrates the real value of Space Federation by showing how a daemon's copilot workflow improves when it receives expert knowledge from HQ.

## 🆕 New Features Covered

1. **Auto-Share Flag**: Mark Spaces for automatic distribution to all daemons
2. **Automatic Sync**: New daemons automatically receive auto-shared Spaces on first sync
3. **SA Control**: System administrators can toggle which Spaces are federated
4. **Selective Push**: Manually push sensitive Spaces to specific daemons only
5. **🆕 LTM Category Auto-Share**: SA can control which HQ LTM categories are shared to daemons
6. **🆕 Category-Level Filtering**: Filter out non-valuable categories from daemon sync
7. **🆕 SA-Only Controls**: Category auto-share knobs only visible to SystemAdmin role on HQ

## Scenario: Improving Daemon Copilot with Federated Go Best Practices

### Prerequisites

1. **HQ Backend Running**:
   ```bash
   cd /Volumes/T7/sop-cloud/lib/sop
   go build -o sop-backend ./tools/sopbackend
   ./sop-backend
   # Runs on http://localhost:8080
   ```

2. **Daemon Running** (in separate terminal):
   ```bash
   cd /Volumes/T7/sop-cloud/lib/sop
   export SOP_HQ_BASE_URL="http://localhost:8080"
   export SOP_DAEMON_CALLBACK_URL="http://localhost:9090"
   export SOP_DAEMON_ID="test-daemon-001"
   go build -o sop-daemon ./cmd/sop-daemon
   ./sop-daemon
   # Runs on http://localhost:9090
   ```

---

## Phase 1: Baseline - Daemon WITHOUT Federation

### Test Query Before Federation

Ask the daemon a Go coding question:

```bash
curl -X POST http://localhost:9090/api/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How should I handle errors in Go when multiple operations can fail?",
    "session_id": "test-session-001"
  }'
```

**Expected Result:**
- Generic answer based only on daemon's local knowledge
- May lack specific best practices or patterns
- Limited context about error wrapping, error types, etc.

---

## Phase 2: Create Expert Knowledge on HQ

### 1. Create a Space with Go Best Practices

Create a Space on HQ with expert-level Go coding patterns:

```bash
curl -X POST http://localhost:8080/api/space/create \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "description": "Expert Go programming patterns and best practices"
  }'
```

### 2. Add Expert Content to the Space

Add multiple artifacts with valuable Go knowledge:

**Error Handling Best Practices:**
```bash
curl -X POST http://localhost:8080/api/memory/add \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "key": "error-handling-patterns",
    "content": "# Go Error Handling Best Practices\n\n## Multiple Operation Errors\n\n1. **Wrap errors with context**: Use fmt.Errorf with %w to maintain error chain\n   ```go\n   if err := saveToDatabase(data); err != nil {\n       return fmt.Errorf(\"failed to save user data: %w\", err)\n   }\n   ```\n\n2. **Use custom error types** for expected errors:\n   ```go\n   type ValidationError struct {\n       Field string\n       Msg   string\n   }\n   func (e *ValidationError) Error() string {\n       return fmt.Sprintf(\"%s: %s\", e.Field, e.Msg)\n   }\n   ```\n\n3. **Aggregate multiple errors** using multierror pattern:\n   ```go\n   var errs []error\n   if err := step1(); err != nil {\n       errs = append(errs, err)\n   }\n   if err := step2(); err != nil {\n       errs = append(errs, err)\n   }\n   if len(errs) > 0 {\n       return fmt.Errorf(\"multiple errors: %v\", errs)\n   }\n   ```\n\n4. **Defer cleanup with error checking**:\n   ```go\n   func processFile(path string) (err error) {\n       f, err := os.Open(path)\n       if err != nil {\n           return err\n       }\n       defer func() {\n           if cerr := f.Close(); cerr != nil && err == nil {\n               err = cerr\n           }\n       }()\n       // process file...\n   }\n   ```",
    "metadata": {
      "author": "senior-go-expert",
      "topic": "error-handling",
      "difficulty": "intermediate"
    }
  }'
```

**Concurrency Patterns:**
```bash
curl -X POST http://localhost:8080/api/memory/add \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "key": "concurrency-patterns",
    "content": "# Go Concurrency Patterns\n\n## Worker Pool Pattern\n\n```go\nfunc workerPool(tasks <-chan Task, results chan<- Result, workers int) {\n    var wg sync.WaitGroup\n    for i := 0; i < workers; i++ {\n        wg.Add(1)\n        go func() {\n            defer wg.Done()\n            for task := range tasks {\n                results <- processTask(task)\n            }\n        }()\n    }\n    wg.Wait()\n    close(results)\n}\n```\n\n## Context-Based Cancellation\n\n```go\nfunc processWithTimeout(ctx context.Context, data Data) error {\n    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)\n    defer cancel()\n    \n    done := make(chan error, 1)\n    go func() {\n        done <- heavyOperation(data)\n    }()\n    \n    select {\n    case err := <-done:\n        return err\n    case <-ctx.Done():\n        return ctx.Err()\n    }\n}\n```",
    "metadata": {
      "author": "senior-go-expert",
      "topic": "concurrency"
    }
  }'
```

**Testing Best Practices:**
```bash
curl -X POST http://localhost:8080/api/memory/add \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "key": "testing-patterns",
    "content": "# Go Testing Best Practices\n\n## Table-Driven Tests\n\n```go\nfunc TestValidateEmail(t *testing.T) {\n    tests := []struct {\n        name    string\n        email   string\n        wantErr bool\n    }{\n        {\"valid email\", \"user@example.com\", false},\n        {\"missing @\", \"userexample.com\", true},\n        {\"empty\", \"\", true},\n    }\n    \n    for _, tt := range tests {\n        t.Run(tt.name, func(t *testing.T) {\n            err := validateEmail(tt.email)\n            if (err != nil) != tt.wantErr {\n                t.Errorf(\"validateEmail() error = %v, wantErr %v\", err, tt.wantErr)\n            }\n        })\n    }\n}\n```\n\n## Test Fixtures and Helpers\n\n```go\nfunc setupTestDB(t *testing.T) *sql.DB {\n    t.Helper()\n    db, err := sql.Open(\"sqlite3\", \":memory:\")\n    if err != nil {\n        t.Fatalf(\"failed to open test db: %v\", err)\n    }\n    t.Cleanup(func() { db.Close() })\n    return db\n}\n```",
    "metadata": {
      "author": "senior-go-expert",
      "topic": "testing"
    }
  }'
```

---

## Phase 3: Push Space to Daemon (Federation)

### Method 1: Enable Auto-Share (Recommended for New Daemons)

**Enable automatic sharing for the Space:**
```bash
curl -X POST http://localhost:8080/api/space/auto-share \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "database_name": "aidb",
    "enabled": true
  }'
```

**Expected Response:**
```json
{
  "status": "success",
  "message": "Auto-share enabled for Space 'go-best-practices'",
  "enabled": true
}
```

**What Happens:**
- Space is immediately pushed to all currently registered daemons
- Any new daemon that registers with HQ will automatically receive this Space during sync
- Perfect for organization-wide knowledge that all daemons should have

**Verify auto-shared Spaces:**
```bash
curl http://localhost:8080/api/space/auto-shared?database=aidb
```

**Expected:** Should list "go-best-practices" with auto-share enabled.

### Method 2: Manual Push to Specific Daemon

Push the Space from HQ to a specific daemon:

```bash
curl -X POST http://localhost:8080/api/space/push \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "daemon_id": "test-daemon-001"
  }'
```

**Expected Response:**
```json
{
  "status": "success",
  "message": "Space 'go-best-practices' pushed to daemon test-daemon-001",
  "daemon_url": "http://localhost:9090",
  "artifacts_pushed": 3
}
```

### Verify Space Received on Daemon

Check daemon logs - you should see:
```
Received Space from HQ: go-best-practices
Imported 3 artifacts into Space: go-best-practices
Space federation state: delivered
```

Query daemon's federated Spaces:
```bash
curl http://localhost:9090/api/spaces
```

**Expected:** Should list "go-best-practices" with `federated: true`

---

## Phase 4: Test Improved Daemon Responses

### Same Query - Now WITH Federation

Ask the SAME question again:

```bash
curl -X POST http://localhost:9090/api/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How should I handle errors in Go when multiple operations can fail?",
    "session_id": "test-session-002"
  }'
```

**Expected Improvement:**
- Answer includes specific patterns from HQ's knowledge base
- Mentions error wrapping with `%w`
- Suggests multierror pattern for aggregating errors
- Includes code examples from the federated Space
- References defer cleanup with error checking
- More authoritative and detailed response

### Additional Test Queries

**Test Concurrency Knowledge:**
```bash
curl -X POST http://localhost:9090/api/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Show me how to implement a worker pool in Go with proper cancellation",
    "session_id": "test-session-003"
  }'
```

**Expected:** Response includes worker pool pattern and context-based cancellation from federated Space.

**Test Testing Knowledge:**
```bash
curl -X POST http://localhost:9090/api/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is the best way to structure Go tests with multiple test cases?",
    "session_id": "test-session-004"
  }'
```

**Expected:** Response includes table-driven test pattern from federated Space.

---

## Phase 5: Bi-Directional Flow (Daemon → HQ)

### Daemon Creates Derived Insight

When daemon answers a question using federated knowledge, it can create derived insights and push them back to HQ.

**Trigger Derived Content Creation:**
```bash
curl -X POST http://localhost:9090/api/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How do I combine error handling with worker pools in Go?",
    "session_id": "test-session-005",
    "research_depth": "deep"
  }'
```

The daemon will:
1. Query federated "go-best-practices" Space
2. Combine error-handling + concurrency patterns
3. Generate a synthesized answer
4. Create a derived artifact in local Space
5. **Push derived content back to HQ** (if it meets promotion criteria)

### Verify Derived Content on HQ

Check HQ for new derived artifacts:
```bash
curl "http://localhost:8080/api/memory/query?space=go-best-practices&tag=derived"
```

**Expected:** May see new artifacts like:
- "error-handling-with-worker-pools" (synthesized pattern)
- Tagged with `derived: true`, `source_daemon: test-daemon-001`

---

## Phase 6.5: New Daemon Registration (Auto-Sync Test)

This demonstrates the real value for new users: **zero-configuration knowledge distribution**.

### Scenario: New Employee Downloads Daemon

**Start a brand-new daemon** (simulating a new employee):
```bash
# In a new terminal
cd /Volumes/T7/sop-cloud/lib/sop
export SOP_HQ_BASE_URL="http://localhost:8080"
export SOP_DAEMON_CALLBACK_URL="http://localhost:9092"
export SOP_DAEMON_ID="new-employee-daemon"
./sop-daemon -port 9092
```

**What Happens Automatically:**

1. **Daemon starts and syncs with HQ**
2. **HQ detects new daemon registration**
3. **HQ automatically pushes all auto-shared Spaces** (like "go-best-practices")
4. **New daemon immediately has expert knowledge**

**No manual intervention required!**

### Verify Auto-Sync Worked

Check new daemon's Spaces:
```bash
curl http://localhost:9092/api/spaces
```

**Expected:** "go-best-practices" appears in the list with `federated: true`

Ask the new daemon a question:
```bash
curl -X POST http://localhost:9092/api/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How should I handle errors in Go when multiple operations can fail?",
    "session_id": "new-employee-001"
  }'
```

**Expected:** Immediately gets high-quality answer with patterns from auto-shared Space, even though this daemon just started!

### Value Demonstration

**Without Auto-Share:**
- New employee downloads daemon
- Gets generic answers
- Needs manual Space setup
- Knowledge inconsistent across team

**With Auto-Share:**
- ✅ New employee downloads daemon
- ✅ Immediately has organization's best practices
- ✅ Zero configuration needed
- ✅ Consistent knowledge across entire fleet
- ✅ IT/SA controls which Spaces are shared

---

## Phase 7: System Administrator Controls

### List All Auto-Shared Spaces

```bash
curl "http://localhost:8080/api/space/auto-shared?database=aidb"
```

### Disable Auto-Share for a Space

```bash
curl -X POST http://localhost:8080/api/space/auto-share \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "database_name": "aidb",
    "enabled": false
  }'
```

**Effect:** Space no longer pushed to new daemons (existing daemons keep their copy).

### Create Selective Spaces

**Example: Sensitive internal docs should NOT be auto-shared:**
```bash
# Create Space without auto-share
curl -X POST http://localhost:8080/api/space/create \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "executive-internal",
    "database_name": "aidb"
  }'

# Add content...

# Manually push only to specific daemons
curl -X POST http://localhost:8080/api/space/push \
  -d '{
    "space_name": "executive-internal",
    "daemon_id": "cto-daemon-001"
  }'
```

---

## Phase 8: LTM Category Auto-Share Controls (SA Only)

This demonstrates fine-grained control over which **HQ LTM categories** are shared to daemons.

### Why This Matters

HQ accumulates knowledge from all daemons in its LTM (Long-Term Memory). However, not all categories are valuable for daemons in the field:
- **Debugging logs** - useful for HQ analytics, not for daemons
- **System metrics** - HQ-only monitoring data
- **Experimental categories** - work-in-progress knowledge not ready for distribution

System Administrators can now control which categories flow back down to daemons.

### List All LTM Categories and Auto-Share Settings

**View category auto-share settings** (SA only):
```bash
curl -H "Authorization: Bearer <SA_TOKEN>" \
  "http://localhost:8080/api/ltm/category/auto-share/list?space_name=shared_memory&database_name=systemdb"
```

**Expected Response:**
```json
{
  "space_name": "shared_memory",
  "database": "systemdb",
  "categories": [
    {
      "path": "shared_memory",
      "name": "Shared Memory",
      "auto_share_to_daemons": true,
      "is_default": true
    },
    {
      "path": "best_practices/error_handling",
      "name": "Error Handling",
      "auto_share_to_daemons": true,
      "is_default": false
    },
    {
      "path": "debug_logs",
      "name": "Debug Logs",
      "auto_share_to_daemons": false,
      "is_default": false
    }
  ],
  "count": 3
}
```

### Disable Auto-Share for a Category

**Mark a category as NOT shareable to daemons:**
```bash
curl -X POST http://localhost:8080/api/ltm/category/auto-share \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <SA_TOKEN>" \
  -d '{
    "space_name": "shared_memory",
    "database_name": "systemdb",
    "category_path": "debug_logs",
    "enabled": false
  }'
```

**Effect:**
- Artifacts in "debug_logs" category will NOT be included when daemons pull from HQ
- Existing daemon knowledge unaffected (no automatic removal)
- Future HQ retrieval requests will filter out this category

### Enable Auto-Share for a Category

**Explicitly mark a category for daemon sharing:**
```bash
curl -X POST http://localhost:8080/api/ltm/category/auto-share \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <SA_TOKEN>" \
  -d '{
    "space_name": "shared_memory",
    "database_name": "systemdb",
    "category_path": "best_practices/golang",
    "enabled": true
  }'
```

### Bulk Update Multiple Categories

**Efficient bulk operations** for managing many categories:
```bash
curl -X POST http://localhost:8080/api/ltm/category/auto-share/bulk \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <SA_TOKEN>" \
  -d '{
    "space_name": "shared_memory",
    "database_name": "systemdb",
    "updates": [
      {"category_path": "debug_logs", "enabled": false},
      {"category_path": "system_metrics", "enabled": false},
      {"category_path": "experimental", "enabled": false},
      {"category_path": "best_practices", "enabled": true},
      {"category_path": "coding_standards", "enabled": true}
    ]
  }'
```

**Expected Response:**
```json
{
  "status": "success",
  "message": "Updated 5 categories",
  "success_count": 5,
  "failed_count": 0,
  "failed_paths": []
}
```

### Test Category Filtering

**Scenario: Daemon should NOT receive debug logs**

1. **SA disables debug_logs category** (as shown above)

2. **Daemon pulls from HQ:**
```bash
9. **🆕 LTM Category Filtering**: SAs can exclude non-valuable categories from daemon sync
10. **🆕 Fine-Grained Control**: Per-category control over HQ→daemon knowledge flow

## Key Features Summary

### Auto-Share Flag (Spaces)
- **Purpose**: Mark Spaces for automatic distribution to all daemons
- **Control**: System administrators toggle via API
- **Use Case**: Organization-wide best practices, coding standards, shared knowledge
- **Benefit**: New employees instantly have expert knowledge

### LTM Category Auto-Share (NEW)
- **Purpose**: Control which HQ LTM categories are shared to daemons during sync
- **Control**: SystemAdmin-only API endpoints
- **Use Case**: Filter out debug logs, metrics, experimental content
- **Benefit**: Daemons receive only high-value knowledge, not noise
- **Default**: Categories are shared unless explicitly disabled (nil = true)

### Manual Push
- **Purpose**: Distribute specific Spaces to specific daemons
- **Control**: Explicit API call per daemon
- **Use Case**: Sensitive content, team-specific knowledge, limited distribution
- **Benefit**: Fine-grained control over knowledge distribution

### Automatic Sync
- **Trigger**: Daemon registration during HQ sync
- **Behavior**: HQ automatically pushes all auto-shared Spaces
- **Benefit**: Zero-configuration knowledge distribution for new daemons

### UI Visibility Controls
- **HQ SA**: All controls visible (Space + Category auto-share)
- **HQ User**: No federation controls visible
- **Daemon UI**: Federation controls always hidden (end users are not SAs)

This demonstrates the **real value** of Space Federation: transforming isolated daemon instances into a coordinated knowledge network where expertise flows from HQ to daemons, and new insights flow back from daemons to HQ, with **intelligent control** over what gets shared and to whom, at both Space and Category granularity
  -H "Authorization: Bearer <USER_TOKEN>" \
  -d '{
    "space_name": "shared_memory",
    "database_name": "systemdb",
    "category_path": "debug_logs",
    "enabled": false
  }'
```

**Expected:** `403 Forbidden: SystemAdmin role required`

### UI Visibility Rules

**On HQ (SystemAdmin logged in):**
- ✅ Category auto-share controls visible
- ✅ Can list, enable, disable categories
- ✅ Bulk operations available

**On HQ (Regular user logged in):**
- ❌ Category auto-share controls hidden
- ❌ API returns 403 Forbidden

**On Daemon UI:**
- ❌ Category auto-share controls always hidden
- ❌ End users are not SAs, cannot control federation

---

## Phase 6: Real-World Coding Scenario

### Simulate Real Developer Workflow

**Developer Working on Daemon:**

1. **Starts a coding task:**
   ```bash
   curl -X POST http://localhost:9090/api/ask \
     -H "Content-Type: application/json" \
     -d '{
       "query": "I need to process 1000 files concurrently with error handling and timeout. Show me the pattern.",
       "session_id": "real-work-001"
     }'
   ```

2. **Gets high-quality answer** combining:
   - Worker pool from federated Space
   - Error aggregation from federated Space
   - Context timeout from federated Space

3. **Asks follow-up:**
   ```bash
   curl -X POST http://localhost:9090/api/ask \
     -H "Content-Type: application/json" \
     -d '{
       "query": "How should I test the file processing function?",
       "session_id": "real-work-001"
     }'
   ```

4. **Gets testing guidance** from federated testing patterns

---

## Measuring Value

### Before Federation (Phase 1)
- Generic answers
- Limited code examples
- No specific patterns or best practices
- Developer needs to search external docs

### After Federation (Phase 4+)
- ✅ Expert-level answers with specific patterns
- ✅ Production-ready code examples
- ✅ Consistent with organization's best practices
- ✅ Contextually relevant to the question
- ✅ Reduces context-switching and external searches
- ✅ Knowledge stays within organization

### Quantifiable Improvements
- **Response Quality**: Compare answer length, code example count, specificity
- **Developer Velocity**: Time to get usable answer
- **Knowledge Consistency**: Same patterns across all daemons
- **Organizational Learning**: Derived insights flow back to HQ

---

## Advanced: Multi-Daemon Federation

### Push Space to Multiple Daemons

Start a second daemon:
```bash
export SOP_DAEMON_ID="test-daemon-002"
export SOP_DAEMON_CALLBACK_URL="http://localhost:9091"
./sop-daemon -port 9091
```

Push Space to both:
```bash
curl -X POST http://localhost:8080/api/space/reshare \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "go-best-practices",
    "daemon_ids": ["test-daemon-001", "test-daemon-002"]
  }'
```

**Value:** All daemons now have the same expert knowledge, ensuring consistent answers across the fleet.

---

## Troubleshooting

### Space Not Appearing on Daemon
- Check daemon logs for import errors
- Verify daemon_id matches in push request
- Ensure daemon registered with HQ (check daemon registry)

### Responses Not Using Federated Content
- Verify Space imported: `curl http://localhost:9090/api/spaces`
- Check research phase logs (should show federated Space query)
- Ensure query relevance matches Space content

### Daemon Not Pushing Derived Content
- Check promotion criteria in rag.go
- Verify HQ URL configured: `echo $SOP_HQ_BASE_URL`
- Check daemon logs for push failures

---

## Clean Up

Stop both processes (Ctrl+C) and clean up:
```bash
rm -f ./sop-backend ./sop-daemon
```

---

## Expected Outcomes

After completing this test, you should observe:

1. **Clear Quality Improvement**: Daemon responses significantly better with federated knowledge
2. **Knowledge Distribution**: HQ's expert content available on all daemons
3. **Bi-Directional Learning**: Daemons contribute derived insights back to HQ
4. **Developer Productivity**: Faster, more accurate answers without leaving the daemon
5. **Organizational Memory**: Best practices centralized on HQ, distributed to edge
6. **🆕 Zero-Config Onboarding**: New daemons automatically receive organization knowledge
7. **🆕 Centralized Control**: SAs can control which Spaces are auto-shared
8. **🆕 Selective Distribution**: Sensitive Spaces can be manually pushed to specific daemons only

## Key Features Summary

### Auto-Share Flag
- **Purpose**: Mark Spaces for automatic distribution to all daemons
- **Control**: System administrators toggle via API
- **Use Case**: Organization-wide best practices, coding standards, shared knowledge
- **Benefit**: New employees instantly have expert knowledge

### Manual Push
- **Purpose**: Distribute specific Spaces to specific daemons
- **Control**: Explicit API call per daemon
- **Use Case**: Sensitive content, team-specific knowledge, limited distribution
- **Benefit**: Fine-grained control over knowledge distribution

### Automatic Sync
- **Trigger**: Daemon registration during HQ sync
- **Behavior**: HQ automatically pushes all auto-shared Spaces
- **Benefit**: Zero-configuration knowledge distribution for new daemons

This demonstrates the **real value** of Space Federation: transforming isolated daemon instances into a coordinated knowledge network where expertise flows from HQ to daemons, and new insights flow back from daemons to HQ, with **intelligent control** over what gets shared and to whom.
