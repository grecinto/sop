# Approval Feature - Quick Reference

## What Was Enhanced

The approval feature has been productionized to provide interactive approval prompts for sensitive operations with **session-based auto-approval** for full automation. Previously, the system was "approval-aware" (had policies) but not "approval-driven" (didn't prompt users). Now it provides both interactive prompts and optional session-wide auto-approval.

## Session Auto-Approval 🚀

### Full Automation for a Session

When approving a sensitive operation, check the **"Always allow [action type] for this session"** checkbox to enable auto-approval. Once enabled:

- ✅ No more approval prompts for that action type
- ✅ Operations execute automatically
- ✅ Visual indicator appears in chat interface
- ✅ Active until page reload or manual clear
- ✅ Separate per action type (shell commands vs code patches)

### Managing Session Approvals

**Enable**: Check the box when approving any action  
**View Status**: Click the 🚀 badge or type `/session_approvals` (if supported)  
**Clear All**: Click "Clear Session Approvals" button in approval modal, or reload page  

**Visual Indicators**:
- 🚀 **Auto-Approve Active** badge appears near send button
- Actions show *(auto-approved)* badge when executed
- Hover over badge to see which action types are auto-approved

### Action Types

Session approval is tracked separately for:
1. **Shell Commands** (`local_shell_execution`) - Terminal/daemon operations
2. **Code Patches** (`coding_workflow_patch`) - File modifications

You can enable auto-approval for one without the other.

## Affected Operations

### 1. Desktop Automation (Local Shell Execution)

**Sensitive Commands Requiring Approval:**
- `sudo` - Elevated privileges
- `rm -rf`, `rm -fr` - Recursive deletion
- `chmod`, `chown` - Permission changes
- `kill -9` - Force process termination
- `reboot`, `shutdown` - System operations
- `dd` - Disk operations
- `ssh`, `scp` - Remote operations

**Example:**
```
User: "Run sudo apt update"
Agent: [Displays approval modal]
        ⚠️ Action Requires Approval
        
        This command requires elevated privileges.
        
        Command: sudo apt update
        
        ⚡ Password Required: This command may prompt for elevated privileges.
        
        [❌ Deny] [✅ Allow]
```

### 2. Coding Workflow (File Mutations)

**Operations Requiring Approval:**
- Applying patches to files
- Creating new files with code
- Modifying existing code
- Refactoring operations

**Example:**
```
User: "Fix the bug in main.go"
Agent: [Displays approval modal]
        ⚠️ Action Requires Approval
        
        This coding task will modify files in your repository.
        
        Statement: Apply patch to src/main.go
        
        [❌ Deny] [✅ Allow]
```

## User Controls

### Keyboard Shortcuts
- **ESC**: Deny the action
- **Cmd+Enter** (Mac) or **Ctrl+Enter** (Windows/Linux): Allow the action

### Approval Modal Elements
1. **Warning Icon**: Orange warning triangle indicates sensitive operation
2. **Reasoning**: Plain English explanation of why approval is needed
3. **Command/Statement**: The actual operation to be performed
4. **Details**: Additional context (working directory, file path, etc.)
5. **Password Warning**: Appears if the command may request password
6. **Action Buttons**: Clear "Deny" and "Allow" options

## What Happens When...

### User Clicks "Allow"

**Desktop Automation:**
1. Command executes via SOP Desktop Daemon
2. Output (stdout/stderr) captured
3. Results displayed in chat
4. Agent can see execution results and continue conversation

**Coding Workflow:**
1. Task resubmitted with approval metadata
2. Patch applied to files
3. Success message shown
4. Agent can continue with next steps

### User Clicks "Deny"

**Desktop Automation:**
1. Command execution cancelled
2. "Action denied by user" message shown
3. Agent notified that user rejected the operation
4. Agent can suggest alternatives or ask for clarification

**Coding Workflow:**
1. Patch application cancelled
2. Files remain unchanged
3. Agent notified of denial
4. Agent can propose different approach

## For Developers

### Bypassing Approval (Not Recommended for Production)

If you need to bypass approval for testing:

**Desktop Automation:**
```go
// In automation policy configuration
RequireApproval: false
```

**Coding Workflow:**
```go
// In task execution
task.AllowDestructive = true
```

Or provide approval metadata:
```go
state.Metadata["approval"] = "granted"
// or
state.Args["approved"] = "true"
```

### Adding Custom Sensitive Patterns

Edit `isSensitiveCommand()` in `lib/sop/ai/agent/copilottools.automation.go`:

```go
func isSensitiveCommand(cmd string) bool {
    lower := strings.ToLower(strings.TrimSpace(cmd))
    sensitivePatterns := []string{
        "sudo",
        "rm -rf",
        "your-custom-pattern",  // Add here
    }
    // ...
}
```

### Checking Approval Status

Approval is granted when:
- User clicks "Allow" in modal (sets metadata internally)
- Explicit metadata provided: `approval=granted` or `approved=true`

Approval is required when:
- Desktop automation detects sensitive command pattern
- Coding workflow has patches and no approval metadata
- SafetyPolicy.Allow() returns ApprovalRequiredError

## Troubleshooting

### Modal Doesn't Appear

**Check:**
1. Browser console for JavaScript errors
2. Backend logs for ACTION_REQUIRED event emission
3. Frontend is receiving streaming events

**Debug:**
```javascript
// In browser console
document.getElementById('action-approval-modal')
// Should return the modal element
```

### Approved Command Doesn't Execute

**Check:**
1. SOP Desktop Daemon running: `curl http://localhost:9090/health`
2. Network connectivity between frontend and daemon
3. Daemon logs for API errors

**Debug:**
```bash
# Check daemon status
ps aux | grep sop-daemon

# Check daemon logs
tail -f /path/to/daemon/logs
```

### Coding Workflow Errors

**Check:**
1. ApprovalRequiredError being thrown (not generic error)
2. Capability.Run() catching ApprovalRequiredError correctly
3. ACTION_REQUIRED marker format in error message

**Debug:**
```bash
# Run coding workflow tests
cd lib/sop/ai/agent/codingworkflow
go test -v -run TestDefaultSafetyPolicy
```

## Implementation Files

**Frontend:**
- `/lib/sop/tools/sopbackend/templates/modals.html` - Approval modal UI
- `/lib/sop/tools/sopbackend/templates/scripts_part05.html` - Event handlers and execution logic

**Backend:**
- `/lib/sop/tools/sopbackend/main.ai.go` - ACTION_REQUIRED parsing and event emission
- `/lib/sop/ai/agent/copilottools.automation.go` - Desktop automation approval payload generation
- `/lib/sop/ai/agent/codingworkflow/orchestrator.go` - SafetyPolicy and ApprovalRequiredError
- `/lib/sop/ai/agent/codingworkflow/capability.go` - Coding workflow approval handling

**Documentation:**
- `/docs/APPROVAL_FEATURE_ENHANCEMENT.md` - Complete implementation documentation
- `/docs/APPROVAL_QUICK_REFERENCE.md` - This file

## Related Features

### Space Deletion Confirmation
Similar approval pattern for destructive space operations:
- Text-based "yes/no" confirmation
- Uses PendingUserConfirmation struct
- Located in `lib/sop/ai/agent/copilot.go` and `service.go`

### Password Prompts
For commands requiring elevated privileges:
- Detected via `requiresPasswordPrompt()` function
- Shown as warning in approval modal
- User types password directly in terminal (not in modal for security)

## Best Practices

1. **Always Show Reasoning**: Users should understand why approval is needed
2. **Display Full Command**: No hidden operations or truncated commands
3. **Provide Context**: Show working directory, affected files, etc.
4. **Clear Actions**: Use explicit "Allow" and "Deny" labels, not "Yes/No"
5. **Visual Indicators**: Use colors, icons, and formatting to highlight importance
6. **Keyboard Shortcuts**: Power users appreciate ESC and Enter shortcuts
7. **Error Messages**: If execution fails, explain clearly what went wrong

## Security Notes

- **No Credential Prompts in Modal**: Passwords are entered in terminal, not web UI
- **No Implicit Approvals**: Every sensitive operation must be explicitly approved
- **Approval Not Cached**: Each operation requires fresh approval (no "remember choice")
- **Audit Trail**: Consider logging approval decisions for compliance (future enhancement)

## Examples

### Desktop Automation Example Session

```
User: Can you check if there are any pending git changes?

Agent: [Uses git status without approval - safe command]
       Output shows 3 modified files.

User: Please commit those changes with message "Fix bugs"

Agent: [Shows approval modal]
       ⚠️ Action Requires Approval
       
       Git commit will permanently record changes to repository history.
       
       Command: git commit -am "Fix bugs"
       Working Directory: /Users/user/project
       
       [❌ Deny] [✅ Allow]

User: [Clicks Allow]

Agent: ✅ Changes committed successfully.
       Commit hash: abc123...
```

### Coding Workflow Example Session

```
User: Refactor the authentication logic in auth.go

Agent: [Analyzes code, prepares patch]
       [Shows approval modal]
       
       ⚠️ Action Requires Approval
       
       This coding task will modify files in your repository.
       
       Statement: Apply patch to src/auth.go
       File Path: src/auth.go
       
       [❌ Deny] [✅ Allow]

User: [Clicks Allow]

Agent: ✅ Patch applied successfully.
       Modified: src/auth.go
       Changes: Extracted validateCredentials() function
```

---

**Version**: 1.0  
**Last Updated**: 2024-01-09  
**Status**: Production Ready
