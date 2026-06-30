# HQ Checkbox Not Visible - Diagnostic Guide

## Problem
The HQ checkbox is not rendering in the UI despite using an SA root account with the `hq_runtime` feature.

## Root Cause Areas

The checkbox visibility depends on this chain:
1. **Session Token** → Browser must send `sop_access_token` cookie with /app request
2. **Token Validation** → Server must validate token against system DB session store
3. **Feature Check** → User's `SubscriptionFeatures` must include `hq_runtime`
4. **Template Render** → `RuntimeSwitchEnabled` flag must be true

## Diagnostic Steps

### Step 1: Verify Your Config Has the Feature

Check your config file (e.g., `config.json`):

```bash
cat ~/Library/Application\ Support/sop/config.json | jq '.users[] | select(.username=="root") | {username, role, subscription_features}'
```

Expected output should show:
```json
{
  "username": "root",
  "role": "system-admin",
  "subscription_features": ["hq_runtime", "daemon_runtime"]
}
```

### Step 2: Run Server with Debug Logging

```bash
cd /Volumes/T7/sop-cloud
go run ./lib/sop/tools/httpserver -port 8081 -log-level debug
```

### Step 3: Access the App and Watch Logs

1. Open browser to http://localhost:8081/login
2. Log in with your root SA account
3. Navigate to http://localhost:8081/app
4. Watch the terminal logs for these key messages:

**Expected successful flow:**
```
DEBUG ensureAuthContextFromRequest: attempting token validation token_prefix=abc123...
DEBUG ensureAuthContextFromRequest: token validated user=root role=system-admin features=[hq_runtime daemon_runtime]
DEBUG planRuntimeMetadata: checking HQ feature for user user=root features=[hq_runtime daemon_runtime]
DEBUG planRuntimeMetadata: HQ runtime enabled for user user=root
```

**Common failure patterns:**

#### Pattern A: No session token
```
DEBUG ensureAuthContextFromRequest: no session token found in request
DEBUG planRuntimeMetadata: no authenticated user, HQ runtime disabled
```
**Fix:** Clear browser cookies and log in again.

#### Pattern B: Token validation fails
```
DEBUG ensureAuthContextFromRequest: attempting token validation token_prefix=abc123...
WARN ensureAuthContextFromRequest: token validation failed ok=false error=...
DEBUG planRuntimeMetadata: no authenticated user, HQ runtime disabled
```
**Fix:** System DB might not be accessible. Check config has `system_db` configured.

#### Pattern C: Feature not in list
```
DEBUG ensureAuthContextFromRequest: token validated user=root role=system-admin features=[]
DEBUG planRuntimeMetadata: checking HQ feature for user user=root features=[]
DEBUG hasHQRuntimeFeature: hq_runtime not found available_features=[]
```
**Fix:** Update user record to include the feature (see Step 4).

### Step 4: Add Feature to User if Missing

If the feature list is empty or missing `hq_runtime`:

```bash
# Update via API (if server is running and you're logged in)
curl -X POST http://localhost:8081/api/admin/users/root/subscription \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "subscription_plan": "enterprise",
    "subscription_features": ["hq_runtime", "daemon_runtime"]
  }'
```

Or edit the config file directly and restart the server.

### Step 5: Verify Browser Session

Open browser DevTools → Application → Cookies → http://localhost:8081

Check for:
- `sop_access_token` (should be present after login)
- `sop_refresh_token` (should be present after login)

If cookies are missing, log in again.

### Step 6: Verify DOM Rendering

Open browser DevTools → Console, run:

```javascript
// Check if the control is in the DOM
document.getElementById('chat-runtime-controls')

// Check its display style
getComputedStyle(document.getElementById('chat-runtime-controls')).display

// Check the checkbox itself
document.getElementById('chat-runtime-hq')
```

Expected:
- Element should exist
- Display should be `flex` (not `none`)
- Checkbox should be visible

### Step 7: Hard Refresh

Sometimes browser caching causes issues:
- Chrome/Edge: Ctrl+Shift+R (Cmd+Shift+R on Mac)
- Firefox: Ctrl+F5 (Cmd+Shift+R on Mac)
- Safari: Cmd+Option+R

## Quick Fix Commands

### Reset Everything
```bash
# Stop server
pkill -f httpserver

# Clear session data
rm -rf ~/.local/share/sop/system_db

# Restart with debug logging
cd /Volumes/T7/sop-cloud
go run ./lib/sop/tools/httpserver -port 8081 -log-level debug
```

### Verify Config Setup
```bash
# Check if system DB is configured
cat ~/Library/Application\ Support/sop/config.json | jq '.system_db'

# If null or missing, the session store won't work
```

## Most Common Issue

**Session token validation failing due to no system DB**

If your config doesn't have a `system_db` section, add it:

```json
{
  "system_db": {
    "name": "system",
    "path": "/Users/YOUR_USERNAME/.local/share/sop/system_db"
  },
  "users": [
    {
      "username": "root",
      "role": "system-admin",
      "subscription_features": ["hq_runtime", "daemon_runtime"],
      ...
    }
  ]
}
```

Then restart the server and log in fresh.
