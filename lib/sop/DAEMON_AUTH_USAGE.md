# Daemon Authentication - Usage Guide

## Overview

The UI now automatically authenticates with the local daemon using a secure bootstrap flow. This happens transparently when you load any page in the SOP UI.

## Architecture

```
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│   Browser    │─────▶│  Httpserver  │─────▶│    Daemon    │
│   (UI)       │      │  :8080       │      │    :9090     │
└──────────────┘      └──────────────┘      └──────────────┘
       │                      │                      │
       │  1. GET /api/daemon/ │                      │
       │     bootstrap-secret │                      │
       │──────────────────────▶                      │
       │                      │  Read ~/.config/sop/ │
       │                      │  daemon.bootstrap    │
       │                      │──────────────────────▶
       │                      │                      │
       │  2. POST /api/daemon/auth                   │
       │     {bootstrap_secret}                      │
       │────────────────────────────────────────────▶
       │                      │  Validate & Issue JWT│
       │  {access_token, refresh_token}              │
       │◀────────────────────────────────────────────│
       │                      │  Delete bootstrap file
       │                      │  (one-time use)       │
       │                      │                      │
       │  3. POST /api/ask                           │
       │     Authorization: Bearer {token}           │
       │────────────────────────────────────────────▶
       │  {response}          │                      │
       │◀────────────────────────────────────────────│
```

## How It Works

### 1. Daemon Startup
When the daemon starts, it:
- Generates a new 256-bit cryptographically random bootstrap secret
- Writes it to `~/.config/sop/daemon.bootstrap` (0600 permissions)
- Logs: "Generated new daemon bootstrap secret"

### 2. UI Page Load
When you load any UI page (index.html, chat.html), the `daemonAuth` client:
- Automatically attempts bootstrap authentication
- Checks if existing tokens are valid
- If not, proceeds with bootstrap flow

### 3. Bootstrap Flow
The UI:
- Fetches bootstrap secret via httpserver: `GET /api/daemon/bootstrap-secret`
- Authenticates with daemon: `POST /api/daemon/auth`
- Receives JWT access_token (15 min expiry) and refresh_token (48 hrs)
- Stores tokens in localStorage
- **Daemon deletes bootstrap file after successful auth**

### 4. Authenticated Requests
All subsequent requests to daemon endpoints use Bearer token:
```javascript
await window.daemonAuth.request('/api/ask', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query: 'Hello' })
});
```

The client automatically:
- Adds `Authorization: Bearer <token>` header
- Refreshes token if expired
- Re-bootstraps if refresh fails

## JavaScript API

### Global Instance
```javascript
window.daemonAuth  // Available on all pages
```

### Methods

#### `bootstrap()`
Manually trigger bootstrap authentication:
```javascript
const success = await window.daemonAuth.bootstrap();
if (success) {
    console.log('Authenticated with daemon');
}
```

#### `request(endpoint, options)`
Make authenticated request to daemon:
```javascript
const response = await window.daemonAuth.request('/api/ask', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query: 'What is the weather?' })
});

const data = await response.json();
```

#### `refresh()`
Manually refresh access token:
```javascript
const success = await window.daemonAuth.refresh();
```

#### `getToken()`
Get current access token (for debugging):
```javascript
const token = window.daemonAuth.getToken();
console.log('Current token:', token);
```

#### `clearTokens()`
Clear all stored tokens:
```javascript
window.daemonAuth.clearTokens();
```

## Example Usage

### Simple Query to Daemon
```javascript
async function askDaemon(question) {
    try {
        const response = await window.daemonAuth.request('/api/ask', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ query: question })
        });
        
        if (!response.ok) {
            throw new Error(`Request failed: ${response.status}`);
        }
        
        const data = await response.json();
        return data.answer;
    } catch (error) {
        console.error('Daemon request failed:', error);
        return null;
    }
}

// Use it
const answer = await askDaemon('What is 2+2?');
console.log(answer);
```

### Execute Command on Daemon
```javascript
async function executeCommand(command) {
    const response = await window.daemonAuth.request('/api/execute', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command })
    });
    
    const result = await response.json();
    return result;
}

// Use it
const result = await executeCommand('ls -la');
console.log('Exit code:', result.exit_code);
console.log('Output:', result.stdout);
```

### Handoff to Daemon with HQ Context
```javascript
async function handoffToHQ(query, hqContext) {
    const response = await window.daemonAuth.request('/api/handoff', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            query: query,
            auth: {
                access_token: localStorage.getItem('sop_access_token'),
                refresh_token: localStorage.getItem('sop_refresh_token')
            },
            hq: {
                base_url: hqContext.baseUrl,
                user_id: hqContext.userId,
                session_id: hqContext.sessionId
            }
        })
    });
    
    return response.json();
}
```

## Desktop Command-Shell Syntax

The desktop/local-agent experience supports three input styles:

1. Slash-prefixed command execution for fast local execution.
   - Examples: `/c ls`, `/cmd pwd`, `/shell git status`, `/exec go test ./...`
   - These bypass the normal reasoning loop and route directly to the local automation executor.
   - They are ideal for quick shell work, repo inspection, or command-style execution without paying the full reasoning cost.
   - Shell state is preserved per session: `/c cd /Volumes` changes the working directory, and `/c ls` uses that updated directory.

2. Slash commands for built-in SOP actions.
   - Examples: `/list_tools`, `/verbose`, `/run my_script`
   - These are reserved for built-in agent features and workflow automation.

3. Natural language questions and instructions.
   - Examples: "What files are in this folder?", "Explain how to run the daemon"
   - These go through the AI agent for intelligent processing.

When local desktop automation is enabled, the same direct path can also trigger richer automation tools such as repo search, repo grep, test execution, file read/write/patch, directory creation and deletion, path existence checks, and path metadata inspection. These are approval-aware and policy-gated so the local-agent experience remains safe by default.

This keeps the local desktop experience predictable: plain shell commands still work, direct command prefixes provide a lightweight fast path for local work, and `/...` remains reserved for built-in SOP commands.

## UI Notes: Floating AI Window and Local-Agent Experience

The detached AI floater now behaves as a focused companion window instead of a disruptive overlay:
- Opening the floater hides the main widget in the shell so the primary app layout remains stable.
- The detached window presents only the relevant AI controls and removes the confusing back/undo-style behavior.
- The main-window AI launcher still works after the floater has been used.
- The floater header controls are visible and behave like the original AI window.

For the local daemon / local-agent experience, Environment Manager visibility follows the same pattern as the rest of the UI policy:
- Shared RBAC policy stays the general source of truth.
- The current HQ vs local runtime toggle provides an immediate client-side override for the simpler local-agent experience.

## UI Visibility Notes for Environment Manager

The Environment Manager visibility is intentionally driven by two signals in the UI:

1. RBAC / user-role policy from the shared backend policy layer.
2. The current HQ vs local daemon runtime toggle in the client.

Temporary rule for the UI:
- Show the Environment Manager for HQ/httpserver flows.
- Hide it when the UI is operating in local daemon mode.
- Keep the decision aligned with RBAC where possible, but use the runtime toggle as an immediate client-side override until a general UI policy endpoint exists.

This is implemented in the app shell and runtime-toggle handlers so the button updates immediately when the user switches runtime modes.

## Security Features

### Bootstrap Rotation
- **New secret every daemon restart**
- Old secrets immediately invalid
- Attack window: <10 seconds (ephemeral)
- Key space: 2^256 (cryptographically secure)

### One-Time Use
- Bootstrap file deleted after first authentication
- Cannot be reused even if stolen
- Forensics-resistant

### JWT Token Security
- Access token: 15 minutes (configurable via `--access-token-ttl`)
- Refresh token: 48 hours (configurable via `--refresh-token-ttl`)
- HMAC-SHA256 signing
- Automatic refresh before expiry

### CORS Protection
- Daemon accepts requests from localhost only
- No wildcard origins
- Blocks cross-origin attacks

## Debugging

### Check Authentication Status
```javascript
// In browser console
console.log('Has valid token:', window.daemonAuth.hasValidToken());
console.log('Current token:', window.daemonAuth.getToken());
```

### View Logs
```javascript
// All authentication operations are logged with [DaemonAuth] prefix
// Check browser console for:
//   [DaemonAuth] Page loaded, attempting daemon authentication...
//   [DaemonAuth] Retrieved bootstrap secret, authenticating with daemon...
//   [DaemonAuth] ✅ Successfully authenticated with daemon
```

### Common Issues

**"Bootstrap file not found"**
- Daemon is not running
- Start daemon: `./sop-daemon --port 9090`

**"Invalid bootstrap secret"**
- Race condition: bootstrap file deleted between read and auth
- Retry will trigger new bootstrap (daemon generates new secret on restart)

**"401 Unauthorized"**
- Token expired and refresh failed
- Client will automatically re-bootstrap
- Check daemon logs for details

## Testing

Run the automated test suite:
```bash
cd /Volumes/T7/sop-cloud/lib/sop
./test-daemon-auth.sh
```

This verifies:
- ✅ Bootstrap secret generation
- ✅ Httpserver proxy endpoint
- ✅ Daemon authentication
- ✅ One-time use (file deletion)
- ✅ Authenticated requests
- ✅ Token refresh

## Configuration

### Daemon Flags
```bash
./sop-daemon \
    --port 9090 \
    --access-token-ttl 15m \
    --refresh-token-ttl 48h
```

### Client Configuration
Edit `scripts_daemon.html` to change:
- `DAEMON_BASE_URL` - Daemon URL (default: http://localhost:9090)
- Storage keys for tokens
- Retry logic

## Production Deployment

### Recommendations
1. **Keep default TTLs** - 15 min access, 48 hrs refresh provides good balance
2. **Monitor daemon restarts** - Each restart rotates bootstrap secret
3. **Use localhost only** - Never expose daemon to network
4. **Review logs** - Check for repeated auth failures (may indicate attack)

### Enterprise Features
- Bootstrap rotation documented in [SECURITY_PROTOCOL.md](cmd/sop-daemon/SECURITY_PROTOCOL.md)
- Compliance mappings: SOC 2, ISO 27001, NIST, GDPR, PCI DSS, HIPAA
- Audit trail: All auth events logged
- NDA-protected technique (even though mathematically secure)

## Next Steps

1. **Test the flow**: Load the UI and check browser console for auth logs
2. **Make daemon requests**: Use `window.daemonAuth.request()` in your code
3. **Monitor security**: Review [SECURITY_PROTOCOL.md](cmd/sop-daemon/SECURITY_PROTOCOL.md) for details
4. **Customize TTLs**: Adjust token expiry based on your security requirements

---

**Need Help?** Check the comprehensive security documentation: [SECURITY_PROTOCOL.md](cmd/sop-daemon/SECURITY_PROTOCOL.md)
