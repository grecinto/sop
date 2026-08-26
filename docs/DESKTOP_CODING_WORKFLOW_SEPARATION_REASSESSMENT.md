# Desktop Automation vs Coding Workflow - Architectural Reassessment

## Issue Identified: 2026-08-26

During investigation of failing test fix capability, discovered significant duplication and confusion between Desktop Automation and Coding Workflow tool implementations.

## Current State

### Desktop Automation (`copilottools.automation.go`)
- **Requires**: Desktop capability enabled + shell automation policy
- **Implementation**: Shell commands via Desktop daemon
- **Tools**: `read_file`, `write_file`, `patch_file`, git ops, etc.
- **read_file implementation**: `sed -n '1,200p' path` (shell-based)
- **Scope**: Ad-hoc user automation requests

### Coding Workflow Phases (`phase_capabilities.go`)
- **Requires**: No special capability (pure domain logic)
- **Implementation**: Pure Go (os.ReadFile, os.WriteFile, etc.)
- **Tools**: `build_context`, `search_repo`, `run_tests`, etc.
- **read_file implementation**: Added today, uses Go's os.ReadFile + line slicing
- **Scope**: Structured coding task workflows

## Problems

### 1. Duplicate Implementations
Both systems implement file reading:
- Desktop: Shell-based via `sed`
- Coding: Go-based via `os.ReadFile`

Both implement file writing/patching with nearly identical logic.

### 2. Capability Confusion
From LLM's perspective, unclear when to use:
- Desktop's `read_file` (requires Desktop capability)
- Coding's `build_context` with `active_file` (hidden file reading)
- **NEW**: Coding's `read_file` (pure Go, always available)

### 3. Policy Misalignment
Desktop automation requires:
- User approval for shell commands
- Desktop daemon running
- Automation policy enabled

Coding workflow should be:
- Always available (core functionality)
- No external dependencies
- Streamlined for common tasks

### 4. Token Efficiency Gap
Before today's fix, LLM couldn't read specific line ranges in coding workflow:
- `build_context` read entire files but didn't expose contents
- `search_repo` returned snippets but not full context
- **Result**: LLM hit tool limits without seeing code

## Recommended Reassessment Areas

### Option A: Converge on Pure Go
- Coding workflow phases implement all file ops in pure Go
- Desktop automation delegates file ops to coding tools
- Desktop only handles **true shell automation** (running scripts, commands, git)
- **Benefit**: Single source of truth, no shell dependency for file ops

### Option B: Explicit Layering
- **Layer 1**: Core file system operations (pure Go, no approval needed)
  - `read_file`, `list_directory`, `path_exists`
- **Layer 2**: Safe mutations (requires approval)
  - `write_file`, `patch_file`, `delete_file`
- **Layer 3**: Shell automation (requires Desktop + approval)
  - `execute_local_command`, `run_repo_tests` (via go test)
- **Benefit**: Clear security boundaries, explicit approval gates

### Option C: Unified Tool Registry
- Both Desktop and Coding register the same tool names
- Implementation dispatched based on context:
  - In coding workflow phase → use pure Go version
  - In Desktop capability → use shell version (for consistency with other shell ops)
- **Benefit**: Consistent LLM interface, context-driven implementation

## Questions for Reassessment

1. **Should file reading/writing require Desktop capability?**
   - Current: Desktop tools require it, Coding tools don't
   - Proposal: Only shell execution requires Desktop

2. **Should coding workflow be able to write files?**
   - Current: Has `apply_patch` but requires approval
   - Question: Should approval be Desktop-scoped or Action-scoped?

3. **Should shell-based and Go-based file ops coexist?**
   - Current: Yes, with duplication
   - Proposal: Converge on Go for file ops, shell only when necessary

4. **What's the role of build_context's hidden file reading?**
   - Current: Reads file into `ActiveFileSnippet` but doesn't expose it
   - Fixed Today: Added explicit `read_file` tool
   - Question: Keep both or remove hidden reading?

## Immediate Fix Applied Today

Added `read_file` universal tool to coding workflow phases:
- Pure Go implementation (no shell dependency)
- Line-range support for token efficiency
- Available to all phases
- **Impact**: LLM can now fix failing tests by reading specific code sections

## Follow-Up Actions

1. **Architecture Review**: Evaluate Options A/B/C above
2. **Policy Audit**: Align file operation approvals with security model
3. **Deprecation Plan**: Remove duplicate implementations
4. **Documentation**: Update capability boundaries and tool responsibilities
5. **Testing**: Ensure coding workflow works without Desktop capability

## References

- Desktop automation: `lib/sop/ai/agent/copilottools.automation.go`
- Coding workflow: `lib/sop/ai/agent/codingworkflow/phase_capabilities.go`
- Approval system: `docs/APPROVALS.md`
- Capability architecture: `docs/CAPABILITY_ARCHITECTURE_ASSESSMENT.md`
