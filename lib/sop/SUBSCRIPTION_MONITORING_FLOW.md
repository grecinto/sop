# Subscription Monitoring Flow

## Overview

The daemon implements automatic subscription monitoring by requiring users to re-authenticate with HQ every 48 hours (configurable). This ensures usage tracking while providing a seamless user experience.

## User Experience

From the user's perspective, it feels natural:

1. **Day 1-2**: User works freely with the daemon, no interruptions
2. **After 48 hours**: User sees: *"Session expired. Please login to continue (subscription monitoring)"*
3. User logs into HQ (takes 5 seconds)
4. **Immediately back to work** - no additional steps needed

Behind the scenes, everything happens automatically.

## Technical Flow

### Initial Setup (First Use)

```
┌─────────────┐
│   Daemon    │
│   Starts    │
└──────┬──────┘
       │
       ├─ Generates 256-bit bootstrap secret
       ├─ Writes to ~/.config/sop/daemon.bootstrap
       └─ Ready for UI authentication
       
┌─────────────┐
│  UI Loads   │
└──────┬──────┘
       │
       ├─ Auto-bootstrap on page load
       ├─ Receives JWT tokens (access: 15m, refresh: 48h)
       └─ User authenticated for 48 hours
```

### Normal Operation (Within 48 Hours)

```
User makes request
    ↓
Has valid token? ──Yes──→ Request succeeds ✅
    ↓ No
    │
Refresh token (background)
    ↓
Token refreshed ──→ Request succeeds ✅
```

### After 48 Hours (Token Expiry = Lease Expiry)

```
User makes request
    ↓
Has valid token? ──No──→ Try refresh
    ↓
Refresh token expired (401)
    ↓
┌────────────────────────────────────┐
│ DAEMON: Automatically regenerates  │
│ bootstrap file for next login      │
└────────────────────────────────────┘
    ↓
UI detects: HQ login required
    ↓
Redirect to /login?message=Session+expired...
    ↓
┌────────────────────────────────────┐
│ USER: Logs into HQ                 │
│ (5 seconds)                        │
└────────────────────────────────────┘
    ↓
markHQLoginCompleted() ← Updates timestamp
    ↓
Redirect back to /app
    ↓
Page loads → DOMContentLoaded
    ↓
Auto-bootstrap with new file
    ↓
JWT tokens issued (fresh 48 hours) ✅
    ↓
User continues working
```

### Daemon Restart (Anytime)

```
Daemon restarts
    ↓
Generates NEW bootstrap secret
    ↓
Existing user tokens? ──Still valid──→ No interruption ✅
    ↓ Expired
    │
User logs in → Bootstrap with new file ✅
```

## Key Design Decisions

### 1. Automatic Bootstrap Regeneration

**Why:** Seamless user experience after HQ login.

**When:** Daemon regenerates bootstrap file whenever refresh token expires (401).

**Result:** User doesn't need to restart daemon or manually trigger anything.

### 2. 48-Hour Renewal Period

**Why:** Balances security with user convenience.

**Configurable:** `--refresh-token-ttl` flag on daemon.

**Subscription monitoring:** HQ sees user login every 48 hours = active subscription.

### 3. One-Time Bootstrap Use

**Why:** Security - prevents reuse of stolen secrets.

**Lifecycle:**
- Created: On daemon startup or after token expiry
- Used: Once for authentication
- Deleted: Immediately after successful auth
- Regenerated: Automatically on next token expiry

### 4. No Manual Intervention

**Why:** Professional product - should "just work".

**Implementation:**
- Auto-bootstrap on page load
- Auto-refresh tokens before expiry
- Auto-regenerate bootstrap on lease expiry
- Auto-redirect to HQ login when needed

## Configuration

### Daemon Flags

```bash
./sop-daemon \
    --port 9090 \
    --access-token-ttl 15m \
    --refresh-token-ttl 48h  # Subscription monitoring interval
```

### Adjusting Renewal Period

For tighter monitoring (e.g., enterprise):
```bash
--refresh-token-ttl 24h  # Daily login required
```

For longer grace period (e.g., power users):
```bash
--refresh-token-ttl 168h  # Weekly login required
```

## Subscription Monitoring Benefits

### For SOP (Provider)

✅ **Accurate usage tracking** - Every user logs in regularly  
✅ **Active subscription verification** - No login = inactive user  
✅ **Usage analytics** - Frequency, patterns, engagement  
✅ **Compliance** - Audit trail of user activity  
✅ **License management** - Identify dormant accounts

### For Customers (Subscribers)

✅ **Minimal friction** - Only login every 48 hours  
✅ **Seamless experience** - Automatic re-authentication  
✅ **No service interruption** - Works during renewal  
✅ **Transparent** - Clear message about why login is needed  
✅ **Fast** - 5 second login process

## Error Handling

### Scenario: Daemon Not Running

```
User tries to use daemon
    ↓
Bootstrap file not found (404)
    ↓
Console: "Daemon not running or bootstrap file not found"
    ↓
Silent failure (doesn't block UI)
    ↓
User starts daemon → Bootstrap succeeds ✅
```

### Scenario: HQ Unavailable

```
User clicks login link
    ↓
HQ login page unreachable
    ↓
Browser shows connection error
    ↓
User retries when HQ is back online ✅
```

### Scenario: Bootstrap Race Condition

```
User 1: Gets bootstrap secret
User 2: Gets same bootstrap secret
User 1: Authenticates successfully → file deleted
User 2: Authentication fails (invalid secret)
    ↓
User 2 retries → Gets new secret → Succeeds ✅
```

## Security Properties

### Bootstrap File

- **Location:** `~/.config/sop/daemon.bootstrap`
- **Permissions:** 0600 (owner read/write only)
- **Lifetime:** <10 seconds (ephemeral)
- **Cryptography:** 256-bit random (2^256 key space)

### JWT Tokens

- **Access token:** 15 minutes (short-lived, frequent refresh)
- **Refresh token:** 48 hours (renewal = subscription check)
- **Signing:** HMAC-SHA256
- **Storage:** localStorage (browser-side)

### Renewal Security

- **No bypass:** Cannot extend 48-hour window without HQ login
- **Audit trail:** Every login logged to HQ
- **Tamper-proof:** Tokens signed by daemon, can't be forged
- **Automatic expiry:** Tokens expire, no manual revocation needed

## Logs to Monitor

### Daemon Logs

```
[Startup] Generated new daemon bootstrap secret at ~/.config/sop/daemon.bootstrap
[Auth] UI authenticated successfully via bootstrap
[Auth] Bootstrap file deleted (one-time use completed)
[Refresh] Token refresh failed (likely expired): ...
[Auth] Bootstrap file regenerated (refresh token expired, ready for HQ re-login)
[Auth] Token refreshed successfully
```

### UI Console Logs

```
[DaemonAuth] Page loaded, attempting daemon authentication...
[DaemonAuth] Retrieved bootstrap secret, authenticating with daemon...
[DaemonAuth] ✅ Successfully authenticated with daemon
[DaemonAuth] Token refresh failed (status: 401)
[DaemonAuth] 🔒 HQ re-login required for subscription monitoring
[Login] HQ login completed, daemon usage authorized for 48 hours
```

## Testing the Flow

### Test 1: Initial Authentication

```bash
# Start daemon
./sop-daemon --port 9090 --refresh-token-ttl 2m  # 2 min for testing

# Start httpserver
./httpserver --port 8080

# Open browser: http://localhost:8080
# Login to HQ
# Check console: Should see "Successfully authenticated with daemon"
```

### Test 2: Token Expiry

```bash
# Wait 2 minutes (refresh token expires)
# Make a daemon request in UI
# Should see: "Session expired. Please login to continue"
# Login again
# Should immediately work without daemon restart ✅
```

### Test 3: Daemon Restart

```bash
# Kill and restart daemon while user is active
./sop-daemon --port 9090

# User's existing tokens still work (until expiry)
# No interruption to user experience ✅
```

## Comparison with Alternatives

### Alternative 1: Always Require HQ Connection

**Pros:** Real-time subscription check  
**Cons:** Daemon can't work offline, slower performance, single point of failure

### Alternative 2: No Subscription Monitoring

**Pros:** Maximum convenience  
**Cons:** No usage tracking, license abuse risk, no analytics

### Alternative 3: Manual License Renewal

**Pros:** Explicit control  
**Cons:** Poor UX, users forget, support overhead

### ✅ Our Approach: Time-Based Renewal

**Pros:**
- Works offline for 48 hours
- Automatic subscription monitoring
- Minimal user friction
- Clean audit trail
- Enterprise-grade security

**Cons:**
- Requires user to be online every 48 hours (acceptable for connected workflows)

## Future Enhancements

### Possible Improvements

1. **Grace period:** Allow 72 hours but warn at 48 hours
2. **Offline mode:** Cache HQ credentials for disaster recovery
3. **Team licenses:** Share renewal across team members
4. **Usage quotas:** Track API calls, not just time
5. **Progressive UX:** Show countdown timer "Next renewal in 47h 23m"

### NOT Recommended

❌ **Extend token lifetime indefinitely** - defeats subscription monitoring  
❌ **Allow token forgery** - security risk  
❌ **Skip renewal for admins** - creates loopholes  
❌ **Store passwords** - security anti-pattern

## Summary

The subscription monitoring system provides:

- **For users:** Seamless experience with minimal interruption (one 5-second login every 48 hours)
- **For SOP:** Accurate usage tracking and active subscription verification
- **For security:** Enterprise-grade cryptography with automatic secret rotation
- **For reliability:** Works during daemon restarts, network issues, and edge cases

The system is production-ready and designed for scale.
