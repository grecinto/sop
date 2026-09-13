# Work Resume Tracker

## Current status
- Admin CLI/UI support for user management has been implemented and verified.
- The CLI now acts as a thin client over HTTP server endpoints for admin operations.
- The admin users page and JSON API are wired and tested.
- The federation and fleet-intelligence work is now implemented and verified end to end.
- The current coding runtime is now centered on a generic objective-driven work inventory instead of a workflow-specific runtime.
- The repo inventory compiler is task-scoped and resumes correctly from the active inventory item.

## 2026-09-09: Objective-driven work inventory runtime (ACTIVE)

### What was completed
- Canonicalized orchestration naming to `managed_objective` in the Ask runtime and classification flow.
- Removed the stale `managed_workflow` naming and aligned tests/docs to the objective-managed runtime.
- Kept the generic Ask/ReAct loop as the single controller; capabilities remain passive participants.
- Implemented objective-scoped metadata on compiled artifact inventories:
  - `objective`
  - `objective_profile`
- Kept inventory selection/completion behavior generic across repo analysis, repair, and implementation tasks.
- Fixed a real runtime issue where routing state persistence would panic without a session payload; it now logs and continues safely when no payload is present.

### Key implementation files
- lib/sop/ai/agent/copilot_routing.go
- lib/sop/ai/agent/ask_task_context.go
- lib/sop/ai/agent/planner/phase_coding.go
- lib/sop/ai/agent/memory_shortterm.go
- lib/sop/ai/agent/focused_context.go
- docs/WORK_INVENTORY_GENERALIZATION.md

### Verified status
Verified with:
- go test ./lib/sop/ai/agent -run 'TestAskCore_RejectsLocalOnlyWorkflowWhenDisabled|TestAskCore_ResumesClarificationFlow|TestNormalizeAskTaskContext_AcceptsManagedObjectiveLevel|TestNormalizeAskTaskContext_ClearCodingRepoFixDoesNotNeedOffer'
- go test ./lib/sop/ai/agent/planner -run 'TestCodingPhaseBuildArtifactInventory_UsesContextSignals|TestCodingPhaseBuildArtifactInventory_AddsRelationshipGroups|TestCodingPhaseBuildArtifactInventory_PrioritizesPackageScope|TestCodingPhaseBuildArtifactInventory_TracksExecutionSurfaceEdges|TestResolveTaskActiveFileFromInventory_UsesSelectedArtifactWhenExplicitFileMissing|TestCodingPhaseBuildTask_UsesActiveInventoryFileWhenNoActiveFileProvided|TestArtifactInventorySelectionAndCompletion_AdvancesActiveItem'

Result:
- ask pipeline regression tests passed
- coding inventory regression tests passed
- objective-scoped runtime continues to behave as a generic worklist model

### Next resume point
- Persist the active objective and inventory state across longer turns more explicitly in the session payload / stream context.
- Extend the objective profile layer so repair, design, analysis, and implementation share the same runtime but vary ranking and completion rules.
- Treat `fix failing tests` as one objective profile inside the same inventory-driven controller, not as a specialized workflow branch.

## What was completed
- Added a standalone admin command wrapper in tools/sopadmin.
- Added a Make target to install the binary as sop-admin.
- Implemented HTTP-backed admin CLI commands:
  - list-users
  - set-role
  - unregister
- Added admin users UI page at /admin/users.
- Added admin users API at /api/admin/users.
- Added regression tests for the CLI and admin UI/API behavior.
- Implemented daemon-to-HQ and HQ-to-daemon federation sync.
- Added local shared-memory promotion and retrieval for federated artifacts.
- Added a rich federated content taxonomy with seeded patterns for workflow, security, performance, testing, and debugging guidance.
- Added daemon-side integration so successful ask flows can promote and reuse shared patterns.
- Added regression tests for federation content heuristics and sync/auth behavior.

## Key files
- tools/sopbackend/admin_cli_ui.go
- tools/sopbackend/backend.go
- tools/sopbackend/exports.go
- tools/sopbackend/admin_ui_cli_test.go
- tools/sopadmin/main.go
- tools/sopadmin/main_test.go
- Makefile
- README.md

## Verification
Verified with:
- go test ./tools/sopadmin ./tools/sopbackend -run 'Test(RunAdminCLI|HandleAdminUsers)' -v
- go test ./cmd/sop-daemon ./tools/sopbackend

Result:
- 5 admin tests passed
- 0 failures
- daemon and backend packages passed
- federation and sync paths verified

## Notes for next task
- The current work is focused on admin/user-management surfaces.
- If switching tasks, this checkpoint can be used as the base state for the next implementation pass.
- If broader admin capabilities are needed later, the next natural step is to extend the CLI/UI with additional endpoints such as create-user, password reset, or richer role management.

---

## 2026-08-30: LLM Performance Bottleneck - RESOLVED ✅

### Problem Identified
**Symptoms**: LLM taking 6 iterations and 12 tool calls without completing tasks. Daemon logs showed:
- Large tool results (200KB inspect_repo, 195KB list_changed_files) 
- Sequential file reads with overlaps (auth_test.go lines 1-260 then 150-340)
- Multiple round-trips to make simple decisions

**Root Cause**: Architecture issue - **Tool Proliferation & Decision Paralysis**
- Every file-backed result showed generic **6-option menu**: grep_tool_result, read_tool_result, analyze_tool_result (with grep/sed/head/tail/wc variants)
- No contextual intelligence - same menu for test failures, git logs, directory listings, build output
- LLM couldn't decide which option to use → picked one → still needed more info → another round-trip → repeat

### Solution Implemented

**1. Smart Context-Aware Recommendations** (`lib/sop/ai/agent/tool_result_storage.go`)
- Replaced `createStandardSummary()` generic 6-option menu with intelligent single recommendation
- Added `getSmartRecommendation()` function that detects:
  - Tool type (inspect_repo, git_log, build, test, search, etc.)
  - Content patterns (errors, failures, warnings, match counts)
  - Output size (warns when >100KB requires focus)
  
**Examples of smart recommendations**:
```
// Massive repo scan:
"⚠️  Large repository scan (195KB). This output is too broad to analyze directly.
Next: Focus your query on specific files, directories, or patterns."

// Build with errors:
"Build errors detected. Find them: grep_tool_result(file_path=\"...\", pattern=\"error|fail\")"

// Git log:
"To find specific commits: grep_tool_result(file_path=\"...\", pattern=\"your-search-term\")"

// Search with many results:
"Found 127+ matches. Consider refining your search pattern for better results."
```

**2. Increased Thresholds** (fewer file-backed results)
- `toolResultThreshold`: 4KB → **16KB** (most outputs now inline)
- `toolResultMaxInline`: 1536 bytes → **2KB** (better preview context)

**3. Test Updates** (`lib/sop/ai/agent/tool_result_storage_test.go`)
- Fixed expectations to match new smart recommendation format
- All 6 TestStoreResult_* tests passing ✅

### Files Modified
- `lib/sop/ai/agent/tool_result_storage.go` - Core recommendation logic (lines 18-22, 283-370)
- `lib/sop/ai/agent/tool_result_storage_test.go` - Test expectations (lines 163-195)

### Verification
```bash
go test -v ./lib/sop/ai/agent -run TestStoreResult
# Result: PASS (all 6 tests)

go build -o sop-daemon ./lib/sop/cmd/sop-daemon
# Result: Build successful ✅
```

### Expected Impact
**Before**: 6 iterations, 12 tool calls, task incomplete (decision paralysis from 6 options per result)  
**After**: ONE actionable recommendation per result → faster decisions, fewer round-trips, tasks complete

**Key Insight**: File-backed approach was correct for avoiding context overflow. Problem was presentation layer overwhelming LLM with too many generic choices. Fix: Progressive disclosure with smart, context-aware single recommendations.

### Next Steps
- Monitor real daemon usage to validate improvement
- If still slow, consider further consolidation of redundant tools (e.g., grep_tool_result vs analyze_tool_result with operation="grep")
- Track metrics: iterations per task, tool calls per task, task completion rate
