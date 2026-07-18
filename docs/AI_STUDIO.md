# AI Studio - Local Daemon Integration

Complete guide for the AI Studio feature that enables authenticated users to connect the Data Manager UI to their local SOP daemon.

---

## Table of Contents
- [Overview](#overview)
- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [Implementation](#implementation)
- [Constants Reference](#constants-reference)
- [Troubleshooting](#troubleshooting)
- [Development](#development)

---

## Overview

The **AI Studio** button allows authenticated site users to launch the Data Manager UI in daemon mode, connecting to their local daemon (`http://127.0.0.1:9090`) instead of the HQ backend.

### Key Benefits
- 🔒 **Privacy**: Data stays local, no network transmission
- ⚡ **Performance**: Direct local connection, no latency
- 🎯 **Flexibility**: Custom daemon ports supported
- 🔑 **Security**: Uses existing site session for authentication

### Command Execution Features
- 🖥️ **Terminal Launch**: Interactive commands (sudo, ssh, vim) open in native terminal
- ⚡ **Background Execution**: Simple commands run with output captured to chat
- 🚫 **Cancellation**: Long-running commands can be aborted with proper cleanup
- 📊 **Error Handling**: Partial output preserved on failures or cancellation

**See Also**: [Terminal Launch Feature](TERMINAL_LAUNCH_FEATURE.md) for detailed execution mode documentation

### Access Control
- **HQ Mode** (`/app`): Admins only
- **Daemon Mode** (`/app?mode=daemon`): All authenticated users
- **Rationale**: Daemon is local to user's machine, no privileged access needed

---

## Quick Start

### For Users

1. **Start your local daemon**:
   ```bash
   sop-daemon --port 9090
   ```

2. **Click "AI Studio"** button on site homepage

3. **Data Manager opens** in new tab, auto-connects to daemon

### For Developers

**Critical Files**:
- `src/frontend/src/App.tsx` - "AI Studio" button
- `src/backend/site/main.go` - Access control
- `lib/sop/tools/sopbackend/ui_routes.go` - Template data
- `lib/sop/tools/sopbackend/templates/scripts_daemon.html` - Auth client

**Key URLs**:
- `/app` → HQ mode (admins only)
- `/app?mode=daemon` → Daemon mode (all users)
- `/app?mode=daemon&daemon_url=http://localhost:9091` → Custom port

---

## Architecture

### Component Diagram

```
┌─────────────────┐
│  Site Frontend  │ "AI Studio" button
│   (React/TS)    │ Opens /app?mode=daemon
└────────┬────────┘
         │
         v
┌─────────────────┐
│  Site Backend   │ Access control
│   (Go port      │ Serves Data Manager UI
│    8080)        │ with daemon config
└────────┬────────┘
         │
         v
┌─────────────────┐
│ Data Manager UI │ HTML/JS templates
│  (Templates)    │ Auto-bootstrap auth
└────────┬────────┘
         │
         v
┌─────────────────┐
│     Daemon      │ Local backend
│   (Go port      │ Validates tokens
│    9090)        │ Serves API
└─────────────────┘
```

### Authentication Flow

```
1. User clicks "AI Studio"
   ↓
2. Site opens /app?mode=daemon
   - Sets DaemonRuntimeEnabled = true
   - Sets DefaultRuntimeMode = "local"
   - Sets localStorage.sop_chat_runtime = 'local'
   ↓
3. Data Manager UI auto-bootstraps
   - Reads localStorage.sop_access_token (site session)
   - Sends POST /api/daemon/auth { bootstrap_token: token }
   - Daemon validates via sessionStore.ValidateTokenWithoutAccountCheck()
   - Returns daemon-specific access/refresh tokens
   ↓
4. UI makes API calls to http://127.0.0.1:9090/api/*
   - Uses daemon tokens in Authorization header
   - Falls back to bootstrap secret if no session token
```

**Why this works**: Site and daemon share `sopbackend.SessionStore()` with the same JWT signing secrets, so daemon can validate site session tokens directly.

---

## Implementation

### Backend: Template Data Generation

**File**: `lib/sop/tools/sopbackend/ui_routes.go`

```go
// When ?mode=daemon query parameter is present, configure daemon mode
daemonRuntimeEnabled := false
daemonURL := DefaultDaemonURL
defaultRuntimeMode := "hq"

if mode := strings.TrimSpace(req.URL.Query().Get("mode")); mode == "daemon" {
    daemonRuntimeEnabled = true
    defaultRuntimeMode = "local"
    
    // Allow custom daemon URL
    if customURL := strings.TrimSpace(req.URL.Query().Get("daemon_url")); customURL != "" {
        daemonURL = customURL
    }
}

return map[string]any{
    "DaemonRuntimeEnabled": daemonRuntimeEnabled,
    "DaemonURL":            daemonURL,
    "DefaultRuntimeMode":   defaultRuntimeMode,
    // ...
}
```

### Frontend: Template Rendering

**File**: `lib/sop/tools/sopbackend/templates/index.html`

```html
{{if .DaemonRuntimeEnabled}}
<script>
(function() {
    // Set daemon mode in localStorage
    localStorage.setItem('sop_chat_runtime', 'local');
    localStorage.setItem('sop_daemon_url', '{{.DaemonURL}}');
    window.SOP_DAEMON_BASE_URL = '{{.DaemonURL}}';
})();
</script>
{{end}}
```

### Frontend: Bootstrap Authentication

**File**: `lib/sop/tools/sopbackend/templates/scripts_daemon.html`

```javascript
async bootstrap() {
    const runtimeMode = getCurrentRuntimeMode();
    if (runtimeMode !== 'local') {
        return false;
    }

    // Try site session token first (preferred method)
    const siteToken = localStorage.getItem('sop_access_token');
    
    const response = await fetch(`${getDaemonBaseURL()}/api/daemon/auth`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            bootstrap_token: siteToken,  // Site session token
            bootstrap_secret: this.getBootstrapSecret()  // Fallback
        })
    });

    if (response.ok) {
        const data = await response.json();
        this.accessToken = data.access_token;
        this.refreshToken = data.refresh_token;
        // Store daemon tokens...
        return true;
    }
    return false;
}
```

### Backend: Access Control

**File**: `src/backend/site/main.go`

```go
// Daemon mode allows ANY authenticated user (not just admins)
if isDaemonMode {
    h.next.ServeHTTP(w, r)
    return
}

// HQ mode requires admin permissions
if !isSiteAdminAuthContext(auth) {
    http.Error(w, "access denied", http.StatusForbidden)
    return
}
```

### Frontend: "AI Studio" Button

**File**: `src/frontend/src/App.tsx`

```typescript
async function openLocalWorkflow(event: MouseEvent<HTMLAnchorElement>): Promise<void> {
  event.preventDefault();
  
  // Check for custom daemon URL
  const customDaemonURL = window.localStorage.getItem('sop_daemon_url');
  
  let url = '/app?mode=daemon';
  if (customDaemonURL && customDaemonURL.trim() !== '' && customDaemonURL !== DEFAULT_DAEMON_URL) {
    url += `&daemon_url=${encodeURIComponent(customDaemonURL)}`;
  }
  
  window.open(url, '_blank', 'noopener,noreferrer');
}
```

---

## Constants Reference

### The Default Daemon URL

**Value**: `http://127.0.0.1:9090`

This constant must be **synchronized across all locations**:

| File | Line | Scope | Purpose |
|------|------|-------|---------|
| `lib/sop/tools/sopbackend/ui_routes.go` | 24 | Go const | Backend default |
| `lib/sop/tools/sopbackend/templates/scripts_part01.html` | 3 | JS global | Primary JS declaration |
| `lib/sop/tools/sopbackend/templates/scripts_daemon.html` | 18 | IIFE scope | Auth client fallback |
| `src/frontend/src/App.tsx` | 62 | TS const | React button logic |

### ⚠️ CRITICAL WARNING

**Do NOT add duplicate `const` declarations** in template files!

JavaScript will throw:
```
Uncaught SyntaxError: Identifier 'DEFAULT_DAEMON_URL' has already been declared
```

This error is **fatal** - it stops script execution and prevents functions like `toggleChatWidget()` from loading.

**Why only these declarations are safe**:
1. `scripts_part01.html` → Global scope (loaded first) ✅
2. `scripts_daemon.html` → IIFE scope `(function() { ... })()` ✅
3. `App.tsx` → Separate TypeScript module ✅

Any other global declaration will conflict with #1.

### Test Protection

**File**: `src/backend/site/daemon_mode_test.go`

```go
func TestDefaultDaemonURLConstant(t *testing.T) {
    expected := "http://127.0.0.1:9090"
    
    if sopbackend.DefaultDaemonURL != expected {
        t.Errorf("DefaultDaemonURL changed! Expected %s, got %s", expected, sopbackend.DefaultDaemonURL)
        t.Error("BREAKING CHANGE: You must update all JavaScript/TypeScript constants")
    }
}
```

This test will **fail** if someone changes the Go constant without updating JS/TS.

---

## Troubleshooting

### Error: "toggleChatWidget is not defined"

**Cause**: Duplicate `const DEFAULT_DAEMON_URL` declarations in template scripts.

**Solution**: Remove any duplicate declarations. Only `scripts_part01.html` should declare it globally.

**How it happened**: During documentation work, the constant was accidentally added to `scripts_part05.html`, causing:
```
Uncaught SyntaxError: Identifier 'DEFAULT_DAEMON_URL' has already been declared
```

This prevented the rest of the script from loading, including `toggleChatWidget()`.

**Fix**: [Removed duplicate](https://github.com/your-repo/commit/abc123)

### Daemon Not Reachable

**Symptoms**:
- "Connection refused" errors
- Bootstrap fails
- API calls timeout

**Checklist**:
- [ ] Daemon is running: `ps aux | grep sop-daemon`
- [ ] Daemon is on correct port: Check logs for "Listening on :9090"
- [ ] No firewall blocking: `curl http://127.0.0.1:9090/health`
- [ ] Custom URL configured correctly: Check `localStorage.sop_daemon_url`

### Access Denied

**Symptoms**:
- "access denied" when opening `/app?mode=daemon`

**Checklist**:
- [ ] User is authenticated: Check `localStorage.sop_access_token`
- [ ] Session hasn't expired: Try refreshing site homepage
- [ ] Access control logic: Check `src/backend/site/main.go` line 392-424

### Wrong Backend Connection

**Symptoms**:
- UI connects to HQ instead of daemon
- "Store not found" for local stores

**Checklist**:
- [ ] Query parameter present: URL has `?mode=daemon`
- [ ] Template variable set: Check `DaemonRuntimeEnabled` in page source
- [ ] Runtime mode: Check `localStorage.sop_chat_runtime === 'local'`
- [ ] Console logs: Look for `[DaemonAuth]` bootstrap messages

### Authentication Failed After Mode Switching

**Symptoms**:
- Login works initially
- After toggling between daemon/HQ modes and logging out
- Subsequent login fails with "authentication failed" + 500 Internal Server Error

**Root Cause**: Daemon-specific localStorage tokens were not cleared during logout, corrupting the authentication state.

**Checklist**:
- [ ] Clear browser localStorage manually: Open DevTools → Application → Clear storage
- [ ] Verify fix is deployed: Check that logout clears these keys:
  - `sop_daemon_access_token`
  - `sop_daemon_refresh_token`
  - `sop_daemon_token_expiry`
  - `sop_last_subscription_check_timestamp`
  - `sop_chat_runtime`
  - `sop_daemon_url`

**Fixed**: 2026-07-15 - Logout now clears all daemon tokens to prevent state corruption.

### Floating AI Window Shows "Access Denied"

**Symptoms**:
- Main Data Manager UI works fine in daemon mode
- Clicking the "⧉" button opens floating chat window
- Floating window shows "access denied" error

**Root Cause**: The floating window was opening `/app?chatFloater=1` without including `?mode=daemon`, so it wasn't initialized with daemon authentication.

**Checklist**:
- [ ] Verify fix is deployed: Check that floating window URL includes both parameters
- [ ] Browser console: Look for `[DaemonAuth]` bootstrap messages in floater window
- [ ] Network tab: Check that floater is connecting to `http://127.0.0.1:9090` not HQ

**Technical Details**:
- The `openChatPiP()` function in `scripts_part05.html` builds the floater URL
- Without `?mode=daemon`, template doesn't set `DaemonRuntimeEnabled = true`
- Without daemon auth initialization, API calls to daemon fail authentication
- Fix: Include `?mode=daemon` when opening floater in local runtime mode

**Fixed**: 2026-07-15 - Floater now includes `?mode=daemon` parameter for proper daemon auth.

---

## Development

### Running Tests

```bash
# Unit tests
cd src/backend/site
go test -v -run TestDaemonMode

# Integration test
# 1. Start site
cd src/backend/site
go run main.go

# 2. Start daemon
sop-daemon --port 9090

# 3. Open browser
open http://localhost:8080

# 4. Click "AI Studio" button
# 5. Verify Data Manager connects to daemon
```

### Adding Features

When modifying daemon mode:

1. **Read this doc** - Understand the full flow
2. **Check constants** - Ensure synchronization
3. **Update tests** - Add test cases in `daemon_mode_test.go`
4. **Test both flows** - Direct launch AND runtime toggle
5. **Update comments** - Keep inline docs accurate

### Code Comments

All critical sections have **comprehensive inline comments**:
- `ui_routes.go` line 456-494: Daemon mode configuration
- `main.go` line 392-424: Access control rationale  
- `App.tsx` line 669-692: Button functionality

### Common Mistakes

❌ **DON'T**:
- Change default URL without updating all constants
- Remove `?mode=daemon` access control check
- Add duplicate `const` declarations
- Make daemon mode admin-only

✅ **DO**:
- Keep constants synchronized
- Test both direct launch and runtime toggle
- Support custom daemon ports
- Maintain backward compatibility with bootstrap secret

### Feature Flags

```javascript
// Runtime toggle enabled?
const RUNTIME_SWITCH_ENABLED = {{ .RuntimeSwitchEnabled }};

// Default mode (set by query param)
const DEFAULT_RUNTIME_MODE = "{{ .DefaultRuntimeMode }}";

// Daemon mode active?
const DAEMON_RUNTIME_ENABLED = {{ .DaemonRuntimeEnabled }};
```

### Related Code

- **Daemon auth handler**: `lib/sop/cmd/sop-daemon/auth.go` (lines 869-940)
- **Session store**: `lib/sop/tools/sopbackend/session.go`
- **UI router**: `lib/sop/tools/sopbackend/ui_routes.go`
- **Access middleware**: `src/backend/site/main.go` (lines 356-463)

---

## History

### 2026-07-15: Duplicate Constant Bug Fix

**Issue**: `toggleChatWidget is not defined` error when launching AI copilot.

**Root Cause**: Duplicate `const DEFAULT_DAEMON_URL` declaration in `scripts_part05.html` caused fatal JavaScript error.

**Fix**: Removed duplicate declaration, kept only:
- `scripts_part01.html` (global scope)
- `scripts_daemon.html` (IIFE scope)
- `App.tsx` (TypeScript module)

**Impact**: High - broke core AI copilot functionality

**Prevention**: Added test protection and documentation warnings

### 2026-07-14: Initial Implementation

**Added**:
- "AI Studio" button in site frontend
- Daemon mode configuration via `?mode=daemon`
- Bootstrap authentication using site session tokens
- Custom daemon port support via `daemon_url` parameter
- Access control allowing all authenticated users in daemon mode

**Why**: Enable users to use Data Manager with their local daemon without manual configuration

---

## Additional Resources

- **Session Store Architecture**: `lib/sop/tools/sopbackend/session.go`
- **Daemon Bootstrap**: `lib/sop/cmd/sop-daemon/auth.go`
- **Template System**: `lib/sop/tools/sopbackend/templates/`
- **Site Backend**: `src/backend/site/main.go`

---

*Last Updated: 2026-07-15*
*Maintainer: Development Team*
