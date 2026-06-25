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
