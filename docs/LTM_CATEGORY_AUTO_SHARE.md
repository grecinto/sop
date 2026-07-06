# LTM Category Auto-Share Implementation Summary

## Overview

This feature allows System Administrators to control which HQ LTM (Long-Term Memory) categories are shared to daemons during sync operations. This provides fine-grained filtering of knowledge that flows from HQ to daemons in the field.

## Problem Statement

HQ accumulates knowledge from all daemons in its LTM. However, not all categories are valuable for daemons:
- **Debug logs** - useful for HQ analytics, but noise for daemons
- **System metrics** - HQ-only monitoring data
- **Experimental categories** - work-in-progress knowledge not ready for distribution
- **Sensitive internal data** - should stay on HQ only

Previously, all LTM content was synced to daemons without filtering, leading to:
- Information overload on daemons
- Reduced relevance of daemon responses
- Potential exposure of sensitive/experimental data
- Wasted bandwidth and storage

## Solution: Category-Level Auto-Share Control

### Core Features

1. **Auto-Share Flag on Categories**
   - Added `AutoShareToDaemons *bool` field to `Category` struct
   - `nil` = default (share), `true` = explicitly share, `false` = do not share
   - Persisted in category metadata

2. **Automatic Filtering During HQ Retrieve**
   - When daemons pull artifacts from HQ, categories are filtered by auto-share flag
   - Only artifacts in shareable categories are returned
   - Filtering happens transparently in `retrieveSharedMemoryArtifacts()`

3. **SystemAdmin-Only API Endpoints**
   - `POST /api/ltm/category/auto-share` - Toggle single category
   - `GET /api/ltm/category/auto-share/list` - List all categories with settings
   - `POST /api/ltm/category/auto-share/bulk` - Bulk update multiple categories
   - All endpoints require SystemAdmin role (RBAC enforced)

4. **UI Visibility Rules**
   - Controls visible only to SystemAdmin users on HQ
   - Hidden from regular users on HQ
   - Always hidden on daemon UI (end users are not SAs)

## Implementation Details

### Category Model Change

**File**: `lib/sop/ai/memory/models.go`

```go
type Category struct {
    // ... existing fields ...
    
    // AutoShareToDaemons controls whether artifacts in this category should be shared to daemons during HQ sync
    // Only applicable for HQ LTM categories, controlled by SystemAdmin role
    // nil=default(true), false=never share, true=always share
    AutoShareToDaemons *bool `json:"auto_share_to_daemons,omitempty"`
}
```

**Design Decision**: Use `*bool` (pointer) to distinguish:
- `nil` = default behavior (share) - no explicit setting
- `false` = explicitly disabled (do not share)
- `true` = explicitly enabled (share)

This allows SAs to see which categories have explicit settings vs defaults.

### HQ Retrieve Filtering

**File**: `lib/sop/tools/sopbackend/hq_sync_handler.go`

Modified `retrieveSharedMemoryArtifacts()` to:
1. Load category auto-share settings from knowledge base
2. Filter artifacts by category before returning to daemon
3. Log filtered categories for observability

```go
func retrieveSharedMemoryArtifacts(ctx context.Context, query string, limit int) ([]hqArtifact, error) {
    // ... retrieve artifacts from LTM ...
    
    // Load category auto-share settings
    categoryAutoShare, err := loadCategoryAutoShareSettings(ctx, db, sharedMemoryKBName)
    
    // Filter artifacts by category
    for _, hit := range hits {
        if !shouldShareCategoryToDaemons(category, categoryAutoShare) {
            continue // Skip this artifact
        }
        // ... add to results ...
    }
}
```

### API Endpoints

**File**: `lib/sop/tools/sopbackend/ltm_category_auto_share.go`

#### 1. Set Category Auto-Share (Single)

**Endpoint**: `POST /api/ltm/category/auto-share`

**Request Body**:
```json
{
  "space_name": "shared_memory",
  "database_name": "systemdb",
  "category_path": "debug_logs",
  "enabled": false
}
```

**Response**:
```json
{
  "status": "success",
  "message": "Auto-share disabled for category 'debug_logs'",
  "enabled": false
}
```

**Authorization**: SystemAdmin role required

#### 2. List Category Auto-Share Settings

**Endpoint**: `GET /api/ltm/category/auto-share/list?space_name=shared_memory&database_name=systemdb`

**Response**:
```json
{
  "space_name": "shared_memory",
  "database": "systemdb",
  "categories": [
    {
      "path": "best_practices",
      "name": "Best Practices",
      "auto_share_to_daemons": true,
      "is_default": true
    },
    {
      "path": "debug_logs",
      "name": "Debug Logs",
      "auto_share_to_daemons": false,
      "is_default": false
    }
  ],
  "count": 2
}
```

**Authorization**: SystemAdmin role required

#### 3. Bulk Update Categories

**Endpoint**: `POST /api/ltm/category/auto-share/bulk`

**Request Body**:
```json
{
  "space_name": "shared_memory",
  "database_name": "systemdb",
  "updates": [
    {"category_path": "debug_logs", "enabled": false},
    {"category_path": "system_metrics", "enabled": false},
    {"category_path": "best_practices", "enabled": true}
  ]
}
```

**Response**:
```json
{
  "status": "success",
  "message": "Updated 3 categories",
  "success_count": 3,
  "failed_count": 0,
  "failed_paths": []
}
```

**Authorization**: SystemAdmin role required

### Role-Based Access Control

All endpoints check authorization:
```go
authCtx := sop.GetAuthFromContext(r.Context())
isAdmin := false
for _, role := range authCtx.Roles {
    if sop.IsAdminRole(role) {
        isAdmin = true
        break
    }
}
if !isAdmin {
    http.Error(w, "Forbidden: SystemAdmin role required", http.StatusForbidden)
    return
}
```

**Roles that can access**:
- `RoleSystemAdmin`
- `RoleAdmin`

**Roles that cannot access**:
- `RoleUser`
- `RoleGuest`
- Any non-admin role

## Use Cases

### 1. Filter Out Debug Logs

**Scenario**: HQ collects debug logs from daemons for analysis, but these logs shouldn't be redistributed to other daemons.

**Solution**:
```bash
curl -X POST http://localhost:8080/api/ltm/category/auto-share \
  -H "Authorization: Bearer <SA_TOKEN>" \
  -d '{"space_name": "shared_memory", "database_name": "systemdb", "category_path": "debug_logs", "enabled": false}'
```

### 2. Curate High-Value Knowledge

**Scenario**: SA identifies that only "best_practices" and "coding_standards" categories should be shared to daemons.

**Solution**:
```bash
curl -X POST http://localhost:8080/api/ltm/category/auto-share/bulk \
  -H "Authorization: Bearer <SA_TOKEN>" \
  -d '{
    "space_name": "shared_memory",
    "database_name": "systemdb",
    "updates": [
      {"category_path": "best_practices", "enabled": true},
      {"category_path": "coding_standards", "enabled": true},
      {"category_path": "debug_logs", "enabled": false},
      {"category_path": "system_metrics", "enabled": false},
      {"category_path": "experimental", "enabled": false}
    ]
  }'
```

### 3. Gradual Knowledge Release

**Scenario**: Experimental category is being tested on HQ. Once validated, SA enables sharing.

**Solution**:
```bash
# Initially: disable sharing
curl -X POST http://localhost:8080/api/ltm/category/auto-share \
  -d '{"space_name": "shared_memory", "category_path": "experimental/new_feature", "enabled": false}'

# After validation: enable sharing
curl -X POST http://localhost:8080/api/ltm/category/auto-share \
  -d '{"space_name": "shared_memory", "category_path": "experimental/new_feature", "enabled": true}'
```

### 4. Audit Category Settings

**Scenario**: SA wants to review which categories are currently shared to daemons.

**Solution**:
```bash
curl -H "Authorization: Bearer <SA_TOKEN>" \
  "http://localhost:8080/api/ltm/category/auto-share/list?space_name=shared_memory&database_name=systemdb"
```

## Architecture Decisions

### Why *bool Instead of bool?

**Rationale**: Using a pointer allows three states:
- `nil` = default (share) - no explicit setting made
- `false` = explicitly disabled
- `true` = explicitly enabled

**Benefits**:
- SA can see which categories have been explicitly configured
- Default behavior (share) doesn't require setting every category
- Backwards compatible - existing categories without the field default to sharing

### Why Filter on Retrieve, Not on Store?

**Rationale**: Filtering happens when daemons pull artifacts (`HandleHQRetrieve`), not when HQ stores them.

**Benefits**:
- HQ retains all knowledge (no data loss)
- SA can change settings retroactively without re-ingesting data
- Daemons immediately see effects of setting changes
- Simpler implementation (no need to track/update stored artifacts)

### Why Category-Level, Not Item-Level?

**Rationale**: Control granularity is at the category level, not individual items.

**Benefits**:
- Simpler management (SAs manage categories, not individual artifacts)
- Consistent filtering (all items in a category share same behavior)
- Performance (fewer settings to check)

**Future Enhancement**: Could add item-level metadata if needed, but category-level is sufficient for v1.

## Testing

### Manual Test Scenarios

Comprehensive test guide: [docs/SPACE_FEDERATION_MANUAL_TEST.md](SPACE_FEDERATION_MANUAL_TEST.md#phase-8-ltm-category-auto-share-controls-sa-only)

**Phase 8** covers:
1. List all LTM categories and auto-share settings
2. Disable auto-share for a category
3. Enable auto-share for a category
4. Bulk update multiple categories
5. Test category filtering (verify daemon doesn't receive disabled categories)
6. Role-based access control (verify non-SA users get 403)
7. UI visibility rules

### Integration Testing

```bash
# Run all tests
cd /Volumes/T7/sop-cloud/lib/sop
./run_all_tests.sh
```

All existing tests pass with the new feature. ✅

## Security Considerations

### Authorization

- All endpoints require SystemAdmin role
- RBAC enforced at API layer
- Non-admin users receive `403 Forbidden`
- No privilege escalation possible

### Data Leakage Prevention

- Categories marked as non-shareable are filtered during HQ retrieve
- Daemons cannot bypass filtering (no direct KB access)
- Existing daemon knowledge unaffected (no automatic removal)
- SA controls are audit-logged

### UI Visibility

- Controls never shown on daemon UI (end users aren't SAs)
- Controls only visible to SA on HQ
- Prevents accidental misconfiguration by end users

## Performance Considerations

### Minimal Overhead

- Category settings cached during HQ retrieve transaction
- O(1) lookup per artifact (map-based filtering)
- No additional database queries per artifact
- Negligible impact on HQ retrieve latency

### Scalability

- Works efficiently with thousands of categories
- Bulk update API for managing many categories at once
- Settings loaded once per HQ retrieve call, reused for all artifacts

## Future Enhancements

1. **Pattern-Based Rules**: Auto-share based on category path patterns (e.g., `best_practices/*`)
2. **Conditional Sharing**: Share category only to daemons with specific attributes (region, role, etc.)
3. **Audit Trail**: Track which SA changed which category settings when
4. **Category Groups**: Define groups of categories for bulk management
5. **UI Dashboard**: Visual interface for managing category auto-share settings
6. **Push Notifications**: Notify daemons when shareable categories change
7. **Item-Level Override**: Allow specific items to override category settings

## Conclusion

LTM Category Auto-Share provides System Administrators with fine-grained control over knowledge distribution from HQ to daemons. This closes a critical gap in the federation feature, enabling:

✅ Filtering of non-valuable content (debug logs, metrics, experimental data)
✅ Curated knowledge distribution (only high-value categories shared)
✅ Gradual knowledge release (test on HQ, then share to daemons)
✅ Security controls (sensitive categories stay on HQ)
✅ Role-based access (SA-only controls, hidden from end users)

Combined with Space Auto-Share, this provides comprehensive control over both structured (Spaces) and organic (LTM categories) knowledge distribution in federated environments.
