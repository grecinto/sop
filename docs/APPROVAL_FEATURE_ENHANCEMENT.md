# Approval Feature Enhancement - Production Implementation

## Overview

This document describes the productionized approval mechanism for sensitive operations in the SOP platform, including desktop automation (local shell execution) and coding workflow (file mutations).

## Implementation Summary

The approval feature has been enhanced from "approval-aware" to "approval-driven" by:

1. **Frontend Modal UI**: Added a production-quality approval modal with clear visual indicators
2. **Event Handling**: Wired `action_required` events to trigger the approval modal
3. **Backend Integration**: ACTION_REQUIRED payload detection and event emission already in place
4. **Coding Workflow**: Enhanced SafetyPolicy to emit structured approval requirements
5. **Consistent UX**: Standardized approval pattern across all sensitive operations
6. **Session Auto-Approval**: Added "Always allow for this session" feature for full automation

## Session-Based Auto-Approval 🚀

### Overview

Users can opt into **session-wide auto-approval** for specific action types, enabling full automation without repeated prompts. This is ideal for:

- Development workflows with frequent shell commands
- Batch operations requiring multiple approvals
- Trusted environments where manual approval adds friction

### Features

- **Per-Action-Type**: Shell commands and code patches tracked separately
- **Session Scoped**: Active until page reload or manual clear
- **Visual Feedback**: Badge indicator and auto-approved markers
- **Easy Toggle**: Single checkbox during approval
- **Reversible**: Clear all approvals with one click

### Implementation Details

**Frontend State Management**:
```javascript
let _sessionApprovals = {}; // Tracks { action_type: true }

function isSessionApproved(actionType) {
    return _sessionApprovals[actionType] === true;
}

function setSessionApproval(actionType, approved) {
    if (approved) {
        _sessionApprovals[actionType] = true;
    } else {
        delete _sessionApprovals[actionType];
    }
}
```

**Modal Checkbox**:
- Displays action-type-specific label ("shell commands" or "code patches")
- Checked state captured on approval
- Session approval set before executing action

**Auto-Approval Flow**:
```
1. ACTION_REQUIRED event received
2. Check if action type is session-approved
3. If YES: Execute immediately (skip modal)
4. If NO: Show approval modal
5. User can enable auto-approve via checkbox
```

**Visual Indicators**:
- 🚀 Badge in chat composer (visible when any auto-approvals active)
- *(auto-approved)* marker on executed actions
- "Clear Session Approvals" button in modal footer

## Architecture

### Flow Diagram

```
┌─────────────────┐
│ Agent/Workflow  │
│ Detects         │
│ Sensitive Op    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Emit            │
│ ACTION_REQUIRED │
│ Event           │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Backend         │
│ interpretOutput │
│ Parses Payload  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ sendEvent       │
│ "action_required"│
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Frontend        │
│ renderStreamEvent│
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Approval Modal  │
│ Shows to User   │
└────────┬────────┘
         │
    ┌────┴────┐
    ▼         ▼
┌────────┐ ┌────────┐
│ Allow  │ │ Deny   │
└───┬────┘ └───┬────┘
    │          │
    ▼          ▼
┌────────────────┐
│ Execute or     │
│ Cancel Action  │
└────────────────┘
```

## Components

### 1. Frontend Modal (modals.html)

**Location**: `lib/sop/tools/sopbackend/templates/modals.html`

**Features**:
- Warning icon and colored border for visual prominence
- Command display in monospace font with syntax highlighting
- Optional password prompt indicator
- Working directory display
- Keyboard shortcuts: ESC (deny), Cmd/Ctrl+Enter (allow)

**Structure**:
```html
<div id="action-approval-modal">
  - Warning header with emoji
  - Reasoning text
  - Command display box
  - Optional details (working directory)
  - Optional password requirement warning
  - Allow/Deny buttons
</div>
```

### 2. Event Handler (scripts_part05.html)

**Location**: `lib/sop/tools/sopbackend/templates/scripts_part05.html`

**Key Functions**:

#### `showActionApprovalModal(payload)`
- Parses approval payload
- Displays modal with action details
- Returns Promise that resolves with user decision
- Handles keyboard shortcuts

#### `executeApprovedAction(payload)`
- Executes approved actions via daemon API
- Handles `local_shell_execution` action type
- Formats execution results for chat display
- Error handling with user-friendly messages

#### `renderStreamEvent(event, streamState)`
- Added handler for `action_required` event type
- Calls `showActionApprovalModal()` on detection
- Executes or denies based on user decision

### 3. Backend Event Emission (main.ai.go)

**Location**: `lib/sop/tools/sopbackend/main.ai.go`

**Function**: `interpretOutput(response, sendEvent)`

Already implemented detection of `<<<ACTION_REQUIRED:...>>>` markers and emits structured events:

```go
if idxStart := strings.Index(text, "<<<ACTION_REQUIRED:"); idxStart >= 0 {
    var actionPayload map[string]any
    if err := json.Unmarshal([]byte(candidate), &actionPayload); err == nil {
        sendEvent("action_required", actionPayload)
        return
    }
}
```

### 4. Desktop Automation (copilottools.automation.go)

**Location**: `lib/sop/ai/agent/copilottools.automation.go`

Already implemented ACTION_REQUIRED payload generation:

```go
actionPayload := map[string]any{
    "action_type":              "local_shell_execution",
    "command":                  cmd,
    "reasoning":                reasoning,
    "requires_approval":        requiresApproval,
    "requires_password_prompt": requiresPasswordPrompt,
    "approval_reason":          approvalReason,
    "working_dir":              workingDir,
    "timeout_seconds":          timeoutSeconds,
}
return fmt.Sprintf("<<<ACTION_REQUIRED:%s>>>", jsonPayload), nil
```

### 5. Coding Workflow (orchestrator.go)

**Location**: `lib/sop/ai/agent/codingworkflow/orchestrator.go`

**New Type**: `ApprovalRequiredError`

```go
type ApprovalRequiredError struct {
    ActionType    string
    Reasoning     string
    FilePath      string
    PatchSummary  string
    RequiresInput bool
}

func (e *ApprovalRequiredError) ToActionPayload() map[string]any {
    return map[string]any{
        "action_type":       e.ActionType,
        "reasoning":         e.Reasoning,
        "requires_approval": true,
        "file_path":         e.FilePath,
        "statement":         e.PatchSummary,
    }
}
```

**Updated**: `DefaultSafetyPolicy.Allow()`

Now returns `ApprovalRequiredError` instead of generic error when approval is missing:

```go
return &ApprovalRequiredError{
    ActionType:   "coding_workflow_patch",
    Reasoning:    "This coding task will modify files in your repository.",
    FilePath:     filePath,
    PatchSummary: patchSummary,
}
```

### 6. Capability Integration (capability.go)

**Location**: `lib/sop/ai/agent/codingworkflow/capability.go`

**Updated**: `Run()` method

Detects `ApprovalRequiredError` and emits ACTION_REQUIRED:

```go
result, err := c.Orchestrator.Execute(task)
if err != nil {
    if approvalErr, ok := err.(*ApprovalRequiredError); ok {
        return capabilities.CapabilityResult{
            Summary: approvalErr.Error(),
            Data:    approvalErr.ToActionPayload(),
        }, fmt.Errorf("<<<ACTION_REQUIRED:%s>>>", mustMarshalJSON(approvalErr.ToActionPayload()))
    }
    return capabilities.CapabilityResult{}, err
}
```

## Payload Formats

### Desktop Automation Payload

```json
{
  "action_type": "local_shell_execution",
  "command": "git status",
  "reasoning": "Check git repository status",
  "requires_approval": true,
  "requires_password_prompt": false,
  "approval_reason": "Command is safe but requires user consent",
  "working_dir": "/Users/user/project",
  "timeout_seconds": 600
}
```

### Coding Workflow Payload

```json
{
  "action_type": "coding_workflow_patch",
  "reasoning": "This coding task will modify files in your repository. Please review and approve the changes.",
  "requires_approval": true,
  "file_path": "src/main.go",
  "statement": "Apply patch to src/main.go"
}
```

## User Experience

### Desktop Automation Flow

1. User asks agent to run a sensitive command (e.g., `rm`, `sudo`)
2. Agent detects sensitive operation and generates ACTION_REQUIRED payload
3. Backend emits `action_required` event
4. **Auto-Approval Check**: If action type is session-approved, execute immediately (skip to step 10)
5. Frontend displays approval modal with:
   - Command to be executed
   - Reasoning for the action
   - Working directory
   - Password prompt warning (if applicable)
   - **"Always allow shell commands for this session" checkbox**
6. User reviews and optionally checks the session approval box
7. User clicks "Allow" or "Deny"
8. If session approval checked: Future shell commands auto-execute
9. If session approval checked: 🚀 badge appears in chat UI
10. On Allow: Command executes via daemon, results shown in chat
11. On Deny: Operation cancelled, agent notified

### Coding Workflow Flow

1. User asks agent to modify code files
2. Workflow reaches patch application stage
3. SafetyPolicy detects missing approval
4. Returns ApprovalRequiredError with file details
5. Capability converts error to ACTION_REQUIRED event
6. **Auto-Approval Check**: If action type is session-approved, retry with approval (skip to step 13)
7. Frontend displays approval modal with:
   - Patch summary
   - Target file path
   - Reasoning
   - **"Always allow code patches for this session" checkbox**
8. User reviews and optionally checks the session approval box
9. User clicks "Allow" or "Deny"
10. If session approval checked: Future patches auto-approve
11. If session approval checked: 🚀 badge appears in chat UI
12. On Allow: Task resubmitted with approval metadata
13. Patch applies successfully
14. On Deny: Operation cancelled

## Security Considerations

### Sensitive Command Detection

Desktop automation detects these patterns as sensitive:
- `sudo`
- `rm -rf`, `rm -fr`
- `chmod`
- `chown`
- `kill -9`
- `reboot`, `shutdown`
- `dd` (disk operations)
- SSH/SCP operations

### Password Prompt Detection

Commands requiring elevated privileges:
- `sudo` prefix
- `ssh` operations
- `scp` file transfers

### Approval Bypass Prevention

Approval can only be granted through:
1. User interaction with approval modal (frontend)
2. Explicit metadata: `approval=granted` or `approved=true` (for automated flows)

## Testing

### Manual Testing Checklist

#### Desktop Automation
- [ ] Trigger sensitive command (e.g., `sudo apt update`)
- [ ] Verify modal appears with correct command
- [ ] Test "Allow" - command executes successfully
- [ ] Test "Deny" - command cancelled gracefully
- [ ] Verify password warning shows for `sudo` commands
- [ ] Test keyboard shortcuts (ESC, Cmd+Enter)
- [ ] **Test session approval checkbox**:
  - [ ] Check "Always allow shell commands for this session"
  - [ ] Verify 🚀 badge appears
  - [ ] Trigger another sensitive command
  - [ ] Verify it auto-executes without prompt
  - [ ] Verify *(auto-approved)* marker in output
  - [ ] Click "Clear Session Approvals"
  - [ ] Verify next command prompts again

#### Coding Workflow
- [ ] Request code modification without approval metadata
- [ ] Verify approval modal appears
- [ ] Test file path display is correct
- [ ] Test "Allow" - patch applies successfully
- [ ] Test "Deny" - patch application cancelled
- [ ] **Test session approval checkbox**:
  - [ ] Check "Always allow code patches for this session"
  - [ ] Verify 🚀 badge appears
  - [ ] Request another code modification
  - [ ] Verify it auto-approves
  - [ ] Verify badge updates appropriately

#### Session Approval Management
- [ ] Enable session approval for one action type
- [ ] Verify badge shows correct status
- [ ] Click badge to view status details
- [ ] Enable session approval for second action type
- [ ] Verify badge reflects both types
- [ ] Clear all session approvals
- [ ] Verify badge disappears
- [ ] Reload page
- [ ] Verify session approvals cleared (as expected)

#### Edge Cases
- [ ] Multiple approval requests in sequence
- [ ] Approval modal during existing modal display
- [ ] Network error during command execution
- [ ] Daemon unreachable scenario
- [ ] Very long command display formatting

### Unit Tests

Existing tests updated:
- `TestDefaultSafetyPolicyAllowsExplicitApproval` - Still passes
- Coding workflow tests handle ApprovalRequiredError type

New test scenarios needed:
- Frontend modal state management
- Event handler error cases
- Approval payload marshaling

## Future Enhancements

### Phase 2 (Optional)

1. **Diff Preview for Coding Workflow**
   - Show actual patch diff before approval
   - Syntax-highlighted code preview
   - Line-by-line diff visualization

2. **Approval History**
   - Track approved/denied actions
   - User preference learning
   - Command whitelist/blacklist

3. **Multi-Step Approvals**
   - Approve batch of related operations
   - "Approve all" for session

4. **Audit Trail**
   - Log all approval decisions
   - Export approval history
   - Compliance reporting

5. **Custom Approval Policies**
   - User-defined sensitive patterns
   - Project-specific approval rules
   - Team policy enforcement

## Deployment

### Prerequisites

- Go 1.21+ (backend compilation)
- Node.js 16+ (frontend asset building, if applicable)
- SOP Desktop Daemon running on port 9090 (for local execution)

### Build and Deploy

1. Backend changes automatically compiled with `go build`
2. Frontend templates served directly (no compilation needed)
3. Restart sopbackend service to load changes
4. Restart daemon if automation policy changes needed

### Configuration

No additional configuration required. The approval feature is active by default for:
- All commands detected as sensitive (desktop automation)
- All file mutations (coding workflow)

To disable approval checks (not recommended for production):
```go
// In automation policy
RequireApproval: false

// In coding workflow
task.AllowDestructive = true
```

## Troubleshooting

### Approval Modal Not Appearing

**Symptom**: ACTION_REQUIRED event generated but modal doesn't show

**Checks**:
1. Verify `renderStreamEvent` has `action_required` handler
2. Check browser console for JavaScript errors
3. Confirm modal element exists in DOM: `document.getElementById('action-approval-modal')`
4. Verify backend is emitting `action_required` event type (not `content`)

### Command Not Executing After Approval

**Symptom**: User clicks "Allow" but command doesn't run

**Checks**:
1. Verify daemon is running: `curl http://localhost:9090/health`
2. Check daemon logs for API errors
3. Verify `executeApprovedAction` is handling correct `action_type`
4. Confirm payload has required fields (command, working_dir)

### Coding Workflow Still Blocking

**Symptom**: Coding workflow returns error instead of approval request

**Checks**:
1. Verify `DefaultSafetyPolicy` returns `ApprovalRequiredError` (not `fmt.Errorf`)
2. Confirm `Capability.Run()` type assertion for `*ApprovalRequiredError`
3. Check that approval metadata is not already set in task state
4. Verify ACTION_REQUIRED marker format in capability error message

## Conclusion

The approval feature is now production-ready with:
- ✅ Clean, user-friendly UI
- ✅ Comprehensive backend integration
- ✅ Consistent UX across all sensitive operations
- ✅ Secure approval workflow
- ✅ Graceful error handling
- ✅ Extensible architecture for future enhancements
- ✅ **Session-based auto-approval for full automation**
- ✅ **Visual indicators and status management**

The system successfully transitions from "approval-aware" (policy checks) to "approval-driven" (interactive prompts) with optional **session-wide automation** for trusted workflows. Users can now:

1. **Get prompted for every operation** (default, highest security)
2. **Enable auto-approval per action type** (balanced - shell commands OR code patches)
3. **Enable full automation** (both action types auto-approved, maximum productivity)

This provides the flexibility for both high-security environments (every approval required) and high-productivity development workflows (session-based auto-approval).
