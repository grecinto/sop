# Authentication Architecture Assessment

**Date:** 2026-07-14  
**Status:** 🔴 CRITICAL - Double authentication and architectural inconsistencies detected

## Executive Summary

The authentication system currently has **conflicting implementations** causing double login prompts. The new site has overlapping auth endpoints with sopbackend, and the frontend is calling the wrong endpoint. This assessment provides a clear remediation plan to establish a single, unified authentication feature with proper user store overrides.

---

## Current State Analysis

### 1. Sopbackend Authentication (Shared Core)

**Location:** `lib/sop/tools/sopbackend/auth.go`

**Features:**
- Core authentication logic with session management
- Bearer token validation (JWT-signed tokens)
- Session cookies: `sop_access_token`, `sop_refresh_token`
- User store abstraction via `UserStore` interface
- **Override mechanism:** `SetUserStoreOverride(store UserStore)` allows external user store injection
- Default user store: reads from `config.json` users array

**Registered Routes** (via `RegisterAllHandlersWithoutRoot`):
```
/api/auth/login      → handleLogin (sopbackend)
/api/auth/refresh    → handleRefresh
/login               → handleLoginPage (HTML page)
```

**Session Creation:** `CreateSession(ctx, username, role) → (accessToken, refreshToken, error)`

---

### 2. New Site Authentication

**Location:** `src/backend/siteaccount/handlers.go`

**Registered Routes** (via `RegisterHandlers`):
```
/api/site/auth/login   → HandleLogin (siteaccount)
/api/site/auth/logout  → HandleLogout
/api/auth/me           → HandleCurrentUser
/api/auth/status       → HandleStatus
```

**User Store Override:**
- Location: `src/backend/siteaccount/sopbackend_override.go`
- Implementation: `SopbackendUserStoreAdapter` bridges site's btree user store to sopbackend interface
- Installation: `InstallSopbackendUserStoreOverride()` calls `sopbackend.SetUserStoreOverride()`

**Problem:** Site's `HandleLogin` creates sessions via `sopbackend.CreateSession()` but site also exposes sopbackend's own login handler at `/api/auth/login`.

---

### 3. Current Routing Setup (src/backend/site/main.go)

```go
func registerSiteHandlers(mux *http.ServeMux, frontendDir, docsDir string) {
    // Install user store override
    siteaccount.InstallSopbackendUserStoreOverride()
    
    // Register ALL sopbackend handlers (including /api/auth/login)
    sopbackend.RegisterAllHandlersWithoutRoot(mux)
    
    // Register site-specific handlers (including /api/site/auth/login)
    siteaccount.RegisterHandlers(mux)
    
    // Install override AGAIN (redundant)
    siteaccount.InstallSopbackendUserStoreOverride()
    
    // ... other handlers
}
```

**Issue:** The override is installed twice, and both login endpoints coexist.

---

### 4. Frontend Login Flow (src/frontend/src/App.tsx)

**Current Behavior:**
```typescript
// Frontend calls sopbackend's endpoint, NOT the site-specific endpoint
const loginResponse = await fetch('/api/auth/login', {  // ❌ Wrong endpoint
    method: 'POST',
    body: JSON.stringify({ username, password }),
});
```

**Expected:**
```typescript
const loginResponse = await fetch('/api/site/auth/login', {  // ✅ Correct
    method: 'POST',
    body: JSON.stringify({ username, password }),
});
```

---

## Root Cause: Double Authentication

### Symptom
When a user logs into the new site, then launches the Data Manager (`/app`), they are prompted to log in again.

### Analysis

1. **Two Login Endpoints Exist:**
   - `/api/auth/login` (sopbackend's handler, reads from config.json users by default)
   - `/api/site/auth/login` (site's handler, uses btree user store via override)

2. **Frontend Uses Wrong Endpoint:**
   - Frontend calls `/api/auth/login` (sopbackend)
   - Sopbackend's `handleLogin` in `auth.go:3263` processes the request
   - **BUT** sopbackend's login handler may not respect the user store override consistently
   - Session cookies are set, but they may not align with site's user records

3. **Session Validation Mismatch:**
   - When accessing `/app`, sopbackend's `RequireAuth` middleware validates the token
   - The session is valid, but the user lookup may fail or return inconsistent data
   - Sopbackend redirects to `/login` thinking the user is unauthenticated

4. **Override Installation Issues:**
   - The override is installed twice in `registerSiteHandlers` (lines 316, 319)
   - Second installation is redundant and may cause race conditions

---

## Daemon Authentication

**Location:** (needs investigation)

**Expected Behavior:**
- Daemon should override sopbackend auth with OS-level user authentication
- Uses host username/password validation
- Should install its own `UserStore` override via `sopbackend.SetUserStoreOverride()`

**Status:** ⚠️ Requires verification - User mentioned "I think this is already wired in and done"

---

## Architectural Requirements (User's Intent)

### 1. Sopbackend (Shared Core)
- **Owns:** Authentication logic, session management, token validation
- **Default User Store:** `config.json` users array
- **Override Mechanism:** `SetUserStoreOverride(store UserStore)`

### 2. Daemon
- **Override:** Host-based user authentication (OS users)
- **Implementation:** Custom `UserStore` that validates against system users
- **No separate login endpoints:** Uses sopbackend's handlers with overridden store

### 3. New Site
- **Override:** Btree-backed user store (persistent database)
- **Implementation:** `SopbackendUserStoreAdapter` in `sopbackend_override.go`
- **Login Flow:** Should use sopbackend's handlers with overridden store
- **No duplicate endpoints:** Remove `/api/site/auth/login`, use `/api/auth/login` directly

### 4. HTTP Server & Data Manager
- **Behavior:** Seamlessly work with whichever user store is installed
- **Auth Validation:** Use sopbackend's session validation (respects override)
- **No awareness required:** Treat all users the same, regardless of source

---

## Recommended Solution

### Phase 1: Consolidate Authentication Endpoints

#### 1.1 Remove Duplicate Site Login Handler

**File:** `src/backend/siteaccount/handlers.go`

**Action:** Remove `HandleLogin` function entirely. The site should use sopbackend's login handler directly.

**Current:**
```go
func RegisterHandlers(mux *http.ServeMux) {
    mux.HandleFunc("/api/site/auth/login", HandleLogin)  // ❌ Remove
    mux.HandleFunc("/api/site/auth/logout", HandleLogout)
    // ... keep other site-specific handlers
}
```

**After:**
```go
func RegisterHandlers(mux *http.ServeMux) {
    mux.HandleFunc("/api/site/auth/logout", HandleLogout)
    // Keep site-specific handlers only (subscription, billing, etc.)
    // Login is handled by sopbackend
}
```

#### 1.2 Update Frontend Login Calls

**File:** `src/frontend/src/App.tsx`

**Keep using `/api/auth/login`** - This is correct. Sopbackend's handler will use the overridden user store.

---

### Phase 2: Fix User Store Override Installation

#### 2.1 Install Override ONCE, Before Handler Registration

**File:** `src/backend/site/main.go`

**Current:**
```go
func registerSiteHandlers(mux *http.ServeMux, frontendDir, docsDir string) {
    siteaccount.InstallSopbackendUserStoreOverride()       // ❌ First install
    sopbackend.RegisterAllHandlersWithoutRoot(mux)
    siteaccount.RegisterHandlers(mux)
    siteaccount.InstallSopbackendUserStoreOverride()       // ❌ Redundant
    // ...
}
```

**After:**
```go
func registerSiteHandlers(mux *http.ServeMux, frontendDir, docsDir string) {
    // Install user store override ONCE, before registering any handlers
    siteaccount.InstallSopbackendUserStoreOverride()
    
    // Register sopbackend handlers (will use overridden user store)
    sopbackend.RegisterAllHandlersWithoutRoot(mux)
    
    // Register site-specific handlers (billing, subscription, etc.)
    siteaccount.RegisterHandlers(mux)
    
    // Register other handlers...
    mux.HandleFunc("/api/daemon/bootstrap-secret", handleDaemonBootstrapSecret)
    // ...
}
```

---

### Phase 3: Ensure Sopbackend Login Respects Override

#### 3.1 Verify User Lookup in Sopbackend Login Handler

**File:** `lib/sop/tools/sopbackend/auth.go` (around line 3263)

**Requirement:** The login handler MUST use `config.findUserRecord()` which already respects the override.

**Current (verify this is correct):**
```go
func handleLogin(w http.ResponseWriter, r *http.Request) {
    username, password, err := parseLoginRequest(r)
    // ...
    
    // This MUST use the overridden user store
    user, found, err := config.findUserRecord(r.Context(), username)
    // ...
}
```

**Check:** Ensure `config.findUserRecord()` calls `config.UsersStore.Get()` which will use the override if installed.

---

### Phase 4: Verify Session Cookie Consistency

#### 4.1 Ensure Single Session Management

**Requirement:** All session creation and validation must go through sopbackend's `SessionStore`.

**Files to verify:**
- `lib/sop/tools/sopbackend/auth.go` - `CreateSession()`, `ValidateSessionToken()`
- `src/backend/siteaccount/handlers.go` - Remove any custom session logic

**Current site login handler:**
```go
// In HandleLogin (to be REMOVED)
accessToken, refreshToken, err := sopbackend.CreateSession(r.Context(), resolvedUser.Username, resolvedUser.Role)
setSessionCookies(w, accessToken, refreshToken, r.TLS != nil)
```

**After removal:** Sopbackend's login handler already does this internally.

---

### Phase 5: Add Regression Tests

#### 5.1 Test Suite for Unified Auth

**File:** `src/backend/site/auth_integration_test.go` (new file)

**Test Cases:**
1. ✅ Login via `/api/auth/login` with site user (btree store)
2. ✅ Verify session cookies are set correctly
3. ✅ Access `/app` without re-authentication
4. ✅ Validate token via `RequireAuth` middleware
5. ✅ Ensure user lookup uses overridden store
6. ✅ Logout and verify session revocation

#### 5.2 Test User Store Override

**File:** `src/backend/siteaccount/sopbackend_override_test.go`

**Test Cases:**
1. ✅ Install override before registration
2. ✅ Verify sopbackend uses override for user lookup
3. ✅ Ensure override persists across handler calls
4. ✅ Validate btree user CRUD operations

---

## Daemon Authentication - Action Items

### Investigation Required

1. **Locate daemon user store implementation**
   - Expected: Custom `UserStore` that validates OS users
   - Look for: `daemon/auth.go` or similar

2. **Verify override installation**
   - Daemon should call `sopbackend.SetUserStoreOverride()` during startup
   - Check: Daemon initialization code

3. **Test daemon login flow**
   - Daemon users should authenticate via `/api/auth/login` using OS credentials
   - Session management should be identical to site/httpserver

---

## HTTP Server Behavior

**Status:** ✅ Expected to work correctly once overrides are in place

**Reasoning:**
- HTTP server already uses `sopbackend.RegisterAllHandlers()`
- It respects whatever user store is configured (default: `config.json`)
- No changes needed - it will automatically respect overrides

---

## Data Manager Integration

**Issue:** Data Manager prompts for login even after site login

**Root Cause:** Data Manager's `/app` route uses `RequireAuth` middleware which validates tokens via `config.AuthenticateBearerToken()`. If the session was created with the wrong user store (sopbackend's default instead of override), validation may fail.

**Solution:** Once login goes through sopbackend's handler with the override installed, `/app` will work seamlessly.

---

## Migration Plan

### Step 1: Code Changes (Low Risk)
1. Remove `HandleLogin` from `src/backend/siteaccount/handlers.go`
2. Fix double override installation in `src/backend/site/main.go`
3. Verify `config.findUserRecord()` uses override in `lib/sop/tools/sopbackend/auth.go`

### Step 2: Testing (Critical)
1. Add regression tests for unified auth flow
2. Test site login → Data Manager access (no re-login)
3. Test daemon auth (if applicable)
4. Test HTTP server auth (should be unchanged)

### Step 3: Validation
1. Deploy to test environment
2. Verify single login works across all components
3. Monitor session validation logs

### Step 4: Documentation
1. Update developer guide with auth architecture
2. Document user store override pattern
3. Add troubleshooting guide for auth issues

---

## Success Criteria

✅ **Single Login Endpoint:** Only `/api/auth/login` exists (sopbackend)  
✅ **User Store Override:** Site uses btree store, daemon uses OS auth, httpserver uses `config.json`  
✅ **No Double Login:** Logging into site grants immediate Data Manager access  
✅ **Session Consistency:** Tokens created by login are valid for all sopbackend-protected routes  
✅ **Regression Tests:** Automated tests prevent future auth regressions  
✅ **Clear Architecture:** One auth feature, multiple user store backends

---

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                      Sopbackend (Core Auth)                 │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Authentication Logic                                │   │
│  │  - Session Management (JWT tokens)                   │   │
│  │  - Bearer Token Validation                           │   │
│  │  - Session Cookies (sop_access_token, sop_refresh_*) │   │
│  │  - Login/Refresh/Logout Handlers                     │   │
│  └──────────────────────────────────────────────────────┘   │
│                            ↓                                 │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  User Store Interface (Abstraction)                  │   │
│  │  - Save/Get/Delete/List users                        │   │
│  │  - Default: config.json users array                  │   │
│  │  - Override: SetUserStoreOverride(store UserStore)   │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                            ↑ ↑ ↑
              ┌─────────────┘ │ └─────────────┐
              │                │                │
    ┌─────────▼─────────┐  ┌──▼──────────┐  ┌─▼──────────────┐
    │  Daemon            │  │  New Site   │  │  HTTP Server   │
    │  (OS User Auth)    │  │  (Btree DB) │  │  (config.json) │
    ├────────────────────┤  ├─────────────┤  ├────────────────┤
    │ Custom UserStore   │  │ Site User   │  │ Default        │
    │ - Validates OS     │  │ Store       │  │ UserStoreImpl  │
    │   users/passwords  │  │ Adapter     │  │                │
    │                    │  │ - Btree     │  │                │
    │ Installs override  │  │   backed    │  │                │
    │ at startup         │  │             │  │                │
    │                    │  │ Installs    │  │                │
    │                    │  │ override at │  │                │
    │                    │  │ startup     │  │                │
    └────────────────────┘  └─────────────┘  └────────────────┘
             │                      │                  │
             └──────────┬───────────┴──────────────────┘
                        │
                        ↓
          ┌─────────────────────────────────┐
          │  Unified Auth Endpoints         │
          │  - /api/auth/login              │
          │  - /api/auth/refresh            │
          │  - /login (HTML page)           │
          │                                 │
          │  All use sopbackend handlers    │
          │  with installed user store      │
          └─────────────────────────────────┘
                        │
                        ↓
          ┌─────────────────────────────────┐
          │  Data Manager (/app)            │
          │  HTTP Server UI                 │
          │  Daemon Workflows               │
          │                                 │
          │  All respect same auth session  │
          │  No re-authentication needed    │
          └─────────────────────────────────┘
```

---

## Open Questions

1. **Daemon Implementation Status:**
   - Where is the daemon's OS user authentication implemented?
   - Is `sopbackend.SetUserStoreOverride()` called during daemon startup?
   - Testing strategy for daemon auth?

2. **Config.json Users:**
   - Should httpserver keep using `config.json` users, or also use a persistent store?
   - Migration path for existing `config.json` users to btree store?

3. **Session TTL:**
   - Current default: 30 minutes (access token), 7 days (refresh token)
   - Should site/daemon have different TTL policies?

4. **Multi-Tenant Support:**
   - Does the site need per-tenant user isolation?
   - Should user stores be scoped by tenant?

---

## Next Steps

1. **Review this assessment** with the team
2. **Prioritize** Phase 1-2 (consolidate endpoints, fix override installation)
3. **Investigate daemon** auth implementation
4. **Implement regression tests** before making changes
5. **Deploy to staging** and validate end-to-end flow
6. **Monitor production** for session/auth errors after deployment

---

## References

- **Sopbackend Auth:** `lib/sop/tools/sopbackend/auth.go`
- **Site Account:** `src/backend/siteaccount/handlers.go`, `sopbackend_override.go`
- **Site Main:** `src/backend/site/main.go`
- **Frontend:** `src/frontend/src/App.tsx`
- **User Story:** Double authentication when launching Data Manager from new site
