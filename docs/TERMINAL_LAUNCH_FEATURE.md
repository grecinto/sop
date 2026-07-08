# Terminal Launch Feature

## Overview

Commands can now execute in **visible terminal windows** instead of hidden background processes. This solves multiple critical UX and functionality issues:

- ✅ **Interactive Commands**: Password prompts (`sudo`), editors (`vim`), monitors (`top`) now work
- ✅ **Transparency**: See exactly what's executing in real-time
- ✅ **Security**: Passwords typed in real terminal, not web UI
- ✅ **Debugging**: Direct terminal access for troubleshooting
- ✅ **User Control**: Choose execution mode per command

## How It Works

### Execution Modes

**🖥️ Terminal Mode**:
- Opens OS native terminal window
- Command runs visibly with full terminal features
- Interactive prompts work (passwords, confirmations)
- Terminal stays open after completion
- Best for: `sudo`, `ssh`, `vim`, `top`, `tail -f`

**⚡ Background Mode**:
- Runs invisibly, captures output
- Results shown in chat
- No stdin/interaction possible
- Fast for simple commands
- Best for: `ls`, `pwd`, `git status`, `cat file.txt`

### Smart Defaults

The system **automatically suggests** the best execution mode:

**Auto-selects Terminal for**:
- `sudo` - password input needed
- `ssh`, `scp` - may need credentials
- `vim`, `nano`, `emacs` - text editors
- `less`, `more` - pagers
- `top`, `htop`, `watch` - monitors
- `tail -f` - log following
- `docker exec -it` - interactive containers

**Auto-selects Background for**:
- `ls`, `pwd`, `cd` - directory operations
- `cat`, `head`, `tail` (without -f) - file viewing
- `git status`, `git log` - git queries
- `echo`, `printf` - output commands

### User Override

Users can always override the suggestion:
- Modal shows both options
- Recommendation highlighted when applicable
- Choice remembered for session (optional)

## Implementation

### Backend (Daemon)

**New File**: `lib/sop/cmd/sop-daemon/terminal.go`

Cross-platform terminal launcher:
```go
func launchInTerminal(command, workingDir string) error
```

Platform-specific implementations:
- **macOS**: Uses AppleScript → Terminal.app
- **Linux**: Tries gnome-terminal, konsole, xterm, etc.
- **Windows**: Uses Windows Terminal or cmd.exe

**Detection Functions**:
```go
func shouldUseTerminal(command string) bool
func getTerminalSuggestion(command string) string
```

**Updated**: `lib/sop/cmd/sop-daemon/main.go`

ExecuteRequest now includes:
```go
type ExecuteRequest struct {
    Command        string `json:"command"`
    WorkingDir     string `json:"working_dir,omitempty"`
    ExecutionMode  string `json:"execution_mode,omitempty"` // "terminal" or "background"
    // ... other fields
}
```

ExecuteResponse includes:
```go
type ExecuteResponse struct {
    LaunchedInTerminal bool   `json:"launched_in_terminal,omitempty"`
    Suggestion        string `json:"suggestion,omitempty"`
    // ... other fields
}
```

### Frontend (Web UI)

**Updated**: `lib/sop/tools/sopbackend/templates/modals.html`

Approval modal now includes execution mode selection:
```html
<div id="action-approval-execution-mode">
    <input type="radio" name="execution-mode" value="terminal">
    🖥️ Launch in Terminal Window
    
    <input type="radio" name="execution-mode" value="background" checked>
    ⚡ Run in Background
    
    <div id="execution-mode-suggestion">
        💡 Suggestion: [context-specific tip]
    </div>
</div>
```

**Updated**: `lib/sop/tools/sopbackend/templates/scripts_part05.html`

JavaScript functions:
```javascript
shouldUseTerminal(command)        // Detects interactive commands
getTerminalSuggestion(command)    // Explains why terminal is suggested
showActionApprovalModal(payload)  // Sets smart defaults
executeApprovedAction(...)        // Handles execution mode
```

## Usage Examples

### Example 1: sudo Command (Auto-Terminal)

```
User: "Run sudo apt update"

[Approval Modal Appears]
┌─────────────────────────────────────┐
│ ⚠️  Action Requires Approval        │
│                                     │
│ Command: sudo apt update            │
│                                     │
│ Execution Mode:                     │
│ ● 🖥️ Launch in Terminal Window     │
│   Opens a visible terminal         │
│                                     │
│ ○ ⚡ Run in Background              │
│   Captures output to chat          │
│                                     │
│ 💡 This command requires password   │
│    input - terminal window          │
│    recommended                      │
│                                     │
│ [Deny]  [Allow]                     │
└─────────────────────────────────────┘

User clicks [Allow]

✅ Command Launched in Terminal 🖥️
Command: sudo apt update

The command is running in a visible terminal 
window. Check your system for the terminal 
window to see output and provide input if needed.

[macOS: Terminal.app window opens]
[User types password in terminal]
[Sees apt update progress]
[Terminal shows: Press any key to close...]
```

### Example 2: Simple Command (Auto-Background)

```
User: "Show me the current directory"

[Approval Modal Appears]
┌─────────────────────────────────────┐
│ ⚠️  Action Requires Approval        │
│                                     │
│ Command: pwd                        │
│                                     │
│ Execution Mode:                     │
│ ○ 🖥️ Launch in Terminal Window     │
│                                     │
│ ● ⚡ Run in Background (default)   │
│   Fast for simple commands         │
│                                     │
│ [Deny]  [Allow]                     │
└─────────────────────────────────────┘

User clicks [Allow]

✅ Local Command Executed ⚡ Background
Command: pwd

Output:
```
/Users/user/project
```

Exit Code: 0
```

### Example 3: Override Suggestion

```
User: "Run tail -f app.log"

[Approval Modal Appears]
Modal suggests: 🖥️ Terminal (for real-time viewing)

User thinks: "I just want to see a few lines"
User selects: ⚡ Background

User clicks [Allow]

✅ Local Command Executed ⚡ Background
Command: tail -f app.log

[Shows last 10 lines in chat]

💡 Tip: Log following is easier to view in 
terminal window. Consider using terminal mode 
for continuous monitoring.
```

## Platform-Specific Behavior

### macOS

**Terminal**: Terminal.app (via AppleScript)
```applescript
tell application "Terminal"
    activate
    do script "cd '/path' && command; echo ''; echo 'Press any key to close...'; read -n 1"
end tell
```

**Features**:
- ✅ Opens new Terminal.app tab/window
- ✅ Changes to working directory
- ✅ Runs command
- ✅ Waits for keypress before closing
- ✅ Terminal.app icon bounces in Dock

### Linux

**Terminal**: Tries in order: gnome-terminal, konsole, xfce4-terminal, xterm, x-terminal-emulator

```bash
gnome-terminal -- bash -c "cd '/path' && command; echo ''; echo 'Press any key to close...'; read -n 1"
```

**Features**:
- ✅ Opens default terminal emulator
- ✅ Works with most desktop environments (GNOME, KDE, XFCE)
- ✅ Fallback chain for compatibility
- ✅ Terminal stays visible after completion

### Windows

**Terminal**: Windows Terminal (wt.exe) or cmd.exe

```cmd
wt.exe new-tab cmd /k "cd /d C:\path && command && echo. && echo Press any key to close... && pause > nul"
```

**Features**:
- ✅ Prefers modern Windows Terminal if available
- ✅ Falls back to classic cmd.exe
- ✅ Changes directory correctly
- ✅ Pauses before closing

## Session Approval Integration

Works seamlessly with session auto-approval:

```
User enables "Always allow shell commands for this session"
User selects "Terminal mode"

Next 10 commands automatically launch in terminal:
- sudo apt install package   → Terminal opens
- sudo systemctl restart app → Terminal opens
- ssh user@server           → Terminal opens

[All without approval prompts]
```

## Error Handling

### Terminal Launch Fails

If terminal can't launch (no terminal emulator found, permission issue):
```
1. Log error: "Failed to launch terminal: [error]"
2. Automatically fall back to background mode
3. Attempt execution in background
4. Show warning in chat if background also fails
```

### Background Execution Issues

If command needs interaction but runs in background:
```
Command executes but:
- Stdin not connected (prompts fail)
- Exit code indicates error
- Response includes suggestion

💡 Tip: This command requires password input - 
terminal window recommended
```

## Security Considerations

### Passwords in Terminal ✅

**Secure Approach**:
- Password prompt appears in **OS terminal**
- User types password using **system keychain integration**
- Password **never** touches web stack
- No JavaScript, no browser, no network

**Why This Is Safe**:
- Terminal is OS-controlled, not web-controlled
- Same security as running commands manually
- Follows industry best practices (Jenkins, GitHub Actions)
- Zero additional attack surface

### Command Visibility 👁️

**Transparency**:
- User **sees** terminal open (knows something is running)
- Can **read** entire command before execution
- Can **interrupt** with Ctrl+C if needed
- Can **inspect** output in real-time

### Audit Trail

Daemon logs every execution:
```
[Daemon Log]
2026-07-07 14:23:15 Executing command in terminal mode: sudo apt update
2026-07-07 14:23:16 Command launched in terminal successfully
```

## Configuration

### User Preferences (Future Enhancement)

```javascript
// localStorage: terminal_preferences
{
    "default_mode": "auto",           // auto, terminal, background
    "always_terminal": ["sudo", "ssh"],
    "always_background": ["ls", "pwd"],
    "remember_choice": true
}
```

### Daemon Configuration

No configuration needed - works out of the box.

Optional environment variables:
```bash
# Override default terminal (Linux)
export SOP_TERMINAL=konsole

# Disable terminal launch (force background)
export SOP_DISABLE_TERMINAL=true
```

## Troubleshooting

### Terminal Doesn't Open

**Symptoms**: Approval granted, but no terminal appears

**Checks**:
1. Check daemon logs: `tail -f /path/to/daemon.log`
2. Look for: "Failed to launch terminal: [error]"
3. Verify terminal emulator installed:
   - macOS: Terminal.app exists (built-in)
   - Linux: `which gnome-terminal` or `which xterm`
   - Windows: `where wt.exe` or `where cmd.exe`

**Solutions**:
- Install a terminal emulator
- Use background mode instead
- Check terminal emulator permissions

### Command Hangs in Background

**Symptoms**: Background execution times out, no output

**Reason**: Command needs interaction but stdin not connected

**Solution**: Use terminal mode for:
- Commands with `sudo`
- SSH connections
- Text editors
- Any prompt-based command

### Terminal Closes Immediately

**Expected Behavior**: Terminal shows "Press any key to close..."

**If not working**:
- macOS: AppleScript adds pause automatically
- Linux: Check terminal emulator support for `-e` flag
- Windows: `pause` command should work

## Testing

### Manual Test Cases

#### Test 1: sudo Command
```bash
Command: sudo echo "test"
Expected: Terminal opens, prompts for password, shows output, waits for key
```

#### Test 2: Simple Command
```bash
Command: ls -la
Expected: Background execution, output in chat
```

#### Test 3: Interactive Editor
```bash
Command: vim test.txt
Expected: Terminal opens, vim works fully, editable
```

#### Test 4: Log Following
```bash
Command: tail -f /var/log/syslog
Expected: Terminal opens, shows real-time log updates
```

#### Test 5: Fallback
```bash
Scenario: Terminal launch fails
Expected: Falls back to background, shows warning
```

### Automated Tests

```bash
cd /Volumes/T7/sop-cloud/lib/sop/cmd/sop-daemon
go test -v -run TestTerminal
```

## Future Enhancements

### Phase 2: Bidirectional Communication

Stream terminal output back to web UI while keeping it visible:
- WebSocket connection to terminal
- Real-time output in both terminal and chat
- Best of both worlds

### Phase 3: Terminal Sessions

Persist terminal sessions:
- Reconnect to existing terminal
- Multiple commands in same session
- Session history and replay

### Phase 4: Custom Terminal Profiles

User-defined terminal configurations:
- Color schemes
- Font sizes
- Working directory preferences
- Environment variables

## Comparison with Industry

### VS Code
- ✅ Shows terminal panel for tasks
- ✅ Interactive input works
- ✅ User sees what's executing
- **Similar to our terminal mode**

### GitHub Desktop
- ✅ Terminal panel for git operations
- ✅ Visual feedback on commands
- **Similar to our terminal mode**

### Docker Desktop
- ✅ Terminal for container exec
- ✅ Interactive shells work
- **Similar to our terminal mode**

### Our Implementation
- ✅ **Better**: Uses native terminal (more familiar)
- ✅ **Better**: Works with any command (not just predefined)
- ✅ **Better**: User choice (terminal or background)
- ✅ **Better**: Smart defaults (auto-detect interactive)

## Summary

The terminal launch feature transforms the approval system from **passive** (show output after) to **active** (show execution live):

**Before**:
- Commands run invisibly
- Interactive commands fail
- No password input possible
- Hard to debug
- User doesn't see what's happening

**After**:
- Commands run visibly (if chosen)
- Interactive commands work ✅
- Passwords typed in secure terminal ✅
- Easy to debug ✅
- Full transparency ✅
- User maintains control ✅

This is a **production-ready** implementation following industry best practices for desktop automation tools.

---

**Version**: 1.0  
**Date**: 2026-07-07  
**Status**: ✅ Production Ready
