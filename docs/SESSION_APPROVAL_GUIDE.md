# Session-Based Auto-Approval Guide

## Overview

The approval system now supports **session-based auto-approval**, allowing you to enable full automation for specific action types without repeated prompts. This is perfect for development workflows where you trust the operations and want to maintain flow.

## Quick Start

### Enable Auto-Approval

1. Perform a sensitive operation (shell command or code patch)
2. When the approval modal appears, check: **"Always allow [action type] for this session"**
3. Click **"Allow"**
4. Future operations of that type execute automatically

### Visual Confirmation

- 🚀 **Auto-Approve Active** badge appears near the send button
- Executed actions show *(auto-approved)* marker
- Click the badge to view details

### Disable Auto-Approval

**Option 1**: Click "Clear Session Approvals" button in next approval modal  
**Option 2**: Reload the page (clears all session state)  
**Option 3**: Click the 🚀 badge and respond to clear prompt

## How It Works

### Action Types

Session approval is tracked separately for:

1. **Shell Commands** (`local_shell_execution`)
   - Terminal operations via daemon
   - File system commands
   - System utilities
   - Package management

2. **Code Patches** (`coding_workflow_patch`)
   - File modifications
   - Code refactoring
   - Patch applications
   - Repository edits

### Approval Scope

**Session-Scoped**: Active only for the current browser session
- ✅ Survives tab switching within same session
- ✅ Persists during agent conversations
- ❌ Cleared on page reload
- ❌ Not persisted across browser restarts

**Per-Action-Type**: Each action type tracked independently
- Enable shell commands only
- Enable code patches only
- Enable both for full automation
- Disable either without affecting the other

### Auto-Approval Flow

```
┌─────────────────────┐
│ Sensitive Operation │
│ Detected            │
└──────────┬──────────┘
           │
           ▼
    ┌──────────────┐
    │ Session      │
    │ Approved?    │
    └──┬───────┬───┘
       │       │
    NO │       │ YES
       │       │
       ▼       ▼
  ┌────────┐  ┌──────────┐
  │ Show   │  │ Execute  │
  │ Modal  │  │ Directly │
  └────┬───┘  └─────┬────┘
       │            │
       ▼            │
  ┌────────────┐   │
  │ User       │   │
  │ Decides    │   │
  └─────┬──────┘   │
        │          │
        ▼          │
  ┌─────────────┐  │
  │ Set Session │  │
  │ Approval?   │  │
  └──────┬──────┘  │
         │         │
         ▼         ▼
    ┌────────────────┐
    │ Execute Action │
    └────────────────┘
```

## Usage Examples

### Example 1: Shell Commands During Development

**Scenario**: You're debugging and need to run multiple shell commands

```
User: "Check git status"
Agent: [Shows approval modal]
      ☑ Always allow shell commands for this session
      [Allow]

🚀 Badge appears

User: "Show me the git log"
Agent: ✅ Local Command Executed 🚀 (auto-approved)
      Output: [git log...]

User: "Check disk usage"
Agent: ✅ Local Command Executed 🚀 (auto-approved)
      Output: [df -h...]
```

### Example 2: Code Modifications Workflow

**Scenario**: You're implementing a feature across multiple files

```
User: "Refactor the authentication logic"
Agent: [Shows approval modal]
      ☑ Always allow code patches for this session
      [Allow]

🚀 Badge appears

User: "Now update the user service to use new auth"
Agent: ✅ Patch applied 🚀 (auto-approved)
      Modified: src/services/user.go

User: "Add tests for the changes"
Agent: ✅ Patch applied 🚀 (auto-approved)
      Modified: src/services/user_test.go
```

### Example 3: Mixed Operations with Selective Approval

**Scenario**: Trust code changes but want to review shell commands

```
User: "Fix the bug in handler.go"
Agent: [Shows approval modal for code patch]
      ☑ Always allow code patches for this session
      [Allow]

User: "Run the test suite"
Agent: [Shows approval modal for shell command]
      ☐ Always allow shell commands for this session
      [Allow]  ← Don't check this time

User: "Fix another bug in controller.go"
Agent: ✅ Patch applied 🚀 (auto-approved)

User: "Run tests again"
Agent: [Shows approval modal - not auto-approved]
      [Allow or Deny]
```

## Security Considerations

### When to Enable Auto-Approval

✅ **Safe Scenarios**:
- Trusted development environment
- Well-known codebase
- Familiar command sequences
- Rapid iteration workflows
- Debugging sessions
- Personal projects

⚠️ **Use Caution**:
- Production systems
- Shared environments
- Unfamiliar codebases
- Complex automation scripts
- Critical infrastructure
- Sensitive data operations

### Best Practices

1. **Review First Action**: Always review the first operation before enabling auto-approval
2. **Scope Appropriately**: Enable only the action types you trust
3. **Clear When Done**: Clear session approvals after completing your workflow
4. **Monitor Badges**: Pay attention to the 🚀 indicator to know when auto-approval is active
5. **Reload Resets**: Remember that reloading the page clears all session approvals

### Risk Mitigation

- **No Persistence**: Session approvals never persist across page reloads
- **Type Isolation**: Shell and code approvals are independent
- **Clear Indicators**: Always know when auto-approval is active
- **Easy Disable**: One-click to clear all approvals
- **Password Protection**: Commands requiring passwords still prompt (not auto-filled)

## Technical Details

### Storage

Session approvals are stored in JavaScript memory:
```javascript
let _sessionApprovals = {
    'local_shell_execution': true,  // Shell commands auto-approved
    'coding_workflow_patch': true   // Code patches auto-approved
};
```

**Not** stored in:
- localStorage (would persist across sessions)
- sessionStorage (would persist across page reloads within tab)
- Cookies (would persist and sync across devices)

### Lifecycle

**Created**: When user checks "Always allow..." and approves
**Active**: Until page reload or manual clear
**Destroyed**: On page navigation/reload, or when user clears

### State Management

```javascript
// Check if approved
isSessionApproved('local_shell_execution')  // true/false

// Set approval
setSessionApproval('local_shell_execution', true)

// Clear specific
setSessionApproval('local_shell_execution', false)

// Clear all
clearSessionApprovals()
```

## FAQ

**Q: Will my approvals persist after I reload the page?**  
A: No. Session approvals are memory-only and cleared on page reload. This is intentional for security.

**Q: Can I enable auto-approval without doing an operation first?**  
A: No. You must trigger an operation to see the approval modal where you can enable session approval.

**Q: What happens if I enable both shell and code approvals?**  
A: Both action types will auto-execute without prompts. This is full automation mode.

**Q: Can I enable approval for one command but not others?**  
A: Currently, no. Approvals are per action-type (all shell commands OR all code patches), not per specific command.

**Q: Does the daemon remember my session approvals?**  
A: No. Session approvals are frontend-only. The daemon has no knowledge of them.

**Q: Will sensitive commands like `rm -rf` still show warnings?**  
A: The approval check is bypassed when session-approved, but the command still executes with the same risk. Use session approval responsibly.

**Q: How do I know what's currently auto-approved?**  
A: Click the 🚀 badge to see details, or it's shown in the badge tooltip.

**Q: Can I create custom action types?**  
A: Not without code changes. Currently supports `local_shell_execution` and `coding_workflow_patch` only.

## Troubleshooting

### Badge Not Appearing

**Symptom**: Checked "Always allow..." but no 🚀 badge shows

**Checks**:
1. Verify you clicked "Allow" (not "Deny") after checking the box
2. Check browser console for JavaScript errors
3. Verify `updateSessionApprovalIndicator()` is called

**Debug**:
```javascript
// In browser console
console.log(window._sessionApprovals || 'undefined');
// Should show: { local_shell_execution: true } or similar
```

### Still Being Prompted

**Symptom**: Enabled auto-approval but still seeing prompts

**Checks**:
1. Verify action type matches (shell vs code)
2. Check if page was reloaded (clears session)
3. Verify badge is visible (indicates active state)

**Debug**:
```javascript
// In browser console
isSessionApproved('local_shell_execution')  // Should return true
```

### Auto-Approval Not Working After Page Reload

**Expected Behavior**: This is normal! Session approvals are memory-only and intentionally cleared on reload for security.

## Version History

- **v1.1** (2026-07-07): Session-based auto-approval feature added
- **v1.0** (2024-01-09): Initial approval system with modal prompts

---

**Remember**: Session approvals are a productivity feature, not a security bypass. Always use them responsibly and in trusted environments. When in doubt, leave auto-approval disabled and review each operation manually.
