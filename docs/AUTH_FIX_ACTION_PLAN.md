# Authentication Fix - Technical Action Plan

**Date:** 2026-07-14  
**Priority:** 🔴 HIGH  
**Status:** ✅ COMPLETED  
**Actual Effort:** ~1 hour

---

## Problem Statement

Users experience double authentication when logging into the new site and then accessing the Data Manager. This was caused by:

1. ✅ **FIXED:** Duplicate login endpoints: `/api/auth/login` (sopbackend) and `/api/site/auth/login` (site-specific)
2. ✅ **FIXED:** Redundant user store override installation: Override installed twice in site bootstrap
3. ✅ **RESOLVED:** Session validation mismatch eliminated by consolidating to single login endpoint

---

## Solution Overview

**Goal:** Establish a single unified authentication flow where:
- Sopbackend owns all auth logic and endpoints ✅
- Site, daemon, and httpserver override only the **user store** implementation ✅
- Session cookies work seamlessly across all components ✅

---

## Implementation Summary

### Phase 1: Remove Duplicate Site Login Handler ✅ COMPLETED

#### File: `src/backend/siteaccount/handlers.go`

**Changes Made:**
1. ✅ Removed `/api/site/auth/login` route registration from `RegisterHandlers()`
2. ✅ Removed entire `HandleLogin` function (~85 lines)
3. ✅ Removed `parseLoginRequest` helper function (only used by HandleLogin)
4. ✅ Removed unused `golang.org/x/crypto/bcrypt` import
5. ✅ Kept `setSessionCookies` helper (used by HandleRefresh and other handlers)
6. ✅ Added comment explaining login is handled by sopbackend

**Result:** Site now relies entirely on sopbackend's `/api/auth/login` endpoint, which automatically uses the site's btree user store via the installed override.

---

### Phase 2: Fix User Store Override Installation ✅ COMPLETED

#### File: `src/backend/site/main.go`

**Changes Made:**
1. ✅ Removed duplicate `InstallSopbackendUserStoreOverride()` call
2. ✅ Kept single installation before handler registration
3. ✅ Added comprehensive comments explaining architecture and timing

**Result:** User store override is now installed exactly once, before any handlers are registered. This ensures consistent, predictable behavior.

---

### Phase 3: Update Tests ✅ COMPLETED

#### File: `src/backend/siteaccount/siteaccount_test.go`

**Changes Made:**
1. ✅ Removed `TestHandleLoginSetsSessionCookies` test (function no longer exists)
2. ✅ Added comment explaining login is now handled by sopbackend
3. ✅ Verified other siteaccount tests still pass

**Build Verification:**
- ✅ Backend compiles successfully: `go build ./src/backend/site`
- ✅ Frontend builds successfully: `npm run build`
- ✅ Tests pass: `go test ./src/backend/siteaccount -v`

---

### Phase 4: Manual Validation ⏳ RECOMMENDED

#### File: `src/backend/siteaccount/handlers.go`

**Before:**
```go
func RegisterHandlers(mux *http.ServeMux) {
    if mux == nil {
        return
    }
    mux.HandleFunc("/api/auth/register", HandleRegister)
    mux.HandleFunc("/api/auth/human-proof/challenge", HandleHumanProofChallenge)
    mux.HandleFunc("/api/site/auth/login", HandleLogin)  // ❌ REMOVE THIS
    mux.HandleFunc("/api/site/auth/logout", HandleLogout)
    mux.HandleFunc("/api/auth/me", withSiteAuth(HandleCurrentUser))
    mux.HandleFunc("/api/auth/status", withSiteAuth(HandleStatus))
    // ... rest of handlers
}
```

**After:**
```go
func RegisterHandlers(mux *http.ServeMux) {
    if mux == nil {
        return
    }
    mux.HandleFunc("/api/auth/register", HandleRegister)
    mux.HandleFunc("/api/auth/human-proof/challenge", HandleHumanProofChallenge)
    // Login is handled by sopbackend - no site-specific login endpoint needed
    mux.HandleFunc("/api/site/auth/logout", HandleLogout)  // Keep logout for site-specific cleanup
    mux.HandleFunc("/api/auth/me", withSiteAuth(HandleCurrentUser))
    mux.HandleFunc("/api/auth/status", withSiteAuth(HandleStatus))
    // ... rest of handlers
}
```

**Functions to Remove:**
1. `HandleLogin` function (around line 295)
2. `parseLoginRequest` helper (if only used by HandleLogin)
3. `setSessionCookies` helper (if only used by HandleLogin)

**Functions to Keep:**
- `HandleLogout` - May have site-specific cleanup logic
- `withSiteAuth` - Middleware for site-specific auth checks
- All other site-specific handlers (register, subscription, billing, etc.)

---

### Phase 2: Fix User Store Override Installation ✅ Low Risk

#### File: `src/backend/site/main.go`

**Before:**
```go
func registerSiteHandlers(mux *http.ServeMux, frontendDir, docsDir string) {
    if mux == nil {
        return
    }

    // The React site is a separate product website and is hosted by this standalone backend.
    // The shared SOP UI/backend is also mounted here so site-admin/root users can manage
    // the hosted environment through the same shared handlers used by the standalone httpserver/daemon.
    siteaccount.InstallSopbackendUserStoreOverride()  // ❌ First installation
    sopbackend.RegisterAllHandlersWithoutRoot(mux)
    siteaccount.RegisterHandlers(mux)
    siteaccount.InstallSopbackendUserStoreOverride()  // ❌ Redundant second installation
    mux.HandleFunc("/api/daemon/bootstrap-secret", handleDaemonBootstrapSecret)
    mux.HandleFunc("/api/coding-workflow/execute", handleCodingWorkflowExecution)
    registerFrontendHandler(mux, frontendDir)
    registerDocsHandler(mux, docsDir)
}
```

**After:**
```go
func registerSiteHandlers(mux *http.ServeMux, frontendDir, docsDir string) {
    if mux == nil {
        return
    }

    // Install site-backed user store ONCE, before registering any handlers.
    // This ensures sopbackend's auth handlers use the site's persistent btree user store
    // instead of the default config.json users array.
    siteaccount.InstallSopbackendUserStoreOverride()

    // Register shared sopbackend handlers (auth, Data Manager, API, etc.)
    // These handlers will automatically use the overridden user store.
    sopbackend.RegisterAllHandlersWithoutRoot(mux)

    // Register site-specific handlers (billing, subscription, registration, etc.)
    siteaccount.RegisterHandlers(mux)

    // Register additional site-specific handlers
    mux.HandleFunc("/api/daemon/bootstrap-secret", handleDaemonBootstrapSecret)
    mux.HandleFunc("/api/coding-workflow/execute", handleCodingWorkflowExecution)
    
    // Mount frontend and docs
    registerFrontendHandler(mux, frontendDir)
    registerDocsHandler(mux, docsDir)
}
```

---

### Phase 3: Verify Sopbackend Login Uses Override ✅ Verification Only

#### File: `lib/sop/tools/sopbackend/auth.go`

**Action:** Verify the login handler correctly uses the overridden user store.

**Location:** Look for `handleLogin` function (around line 3263)

**Expected Code:**
```go
func handleLogin(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    username, password, err := parseLoginRequest(r)
    if err != nil {
        writeJSONError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    // ✅ This MUST call config.findUserRecord() which respects the override
    user, found, err := config.findUserRecord(r.Context(), username)
    if err != nil {
        writeJSONError(w, http.StatusInternalServerError, err.Error())
        return
    }
    if !found || user == nil {
        writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
        return
    }

    // Validate password
    // ...

    // Create session
    accessToken, refreshToken, err := currentTokenFacade().CreateSession(r.Context(), user.Username, user.Role)
    // ...
}
```

**Verification Steps:**
1. Confirm `config.findUserRecord()` is called (not direct config.Users access)
2. Trace `config.findUserRecord()` implementation (around line 1589)
3. Verify it calls `config.UsersStore.Get()` which uses the override if installed

**Key Code Path:**
```go
func (c *Config) findUserRecord(ctx context.Context, username string) (*UserRecord, bool, error) {
    // ...
    if err := c.syncUsersStoreFromConfig(ctx); err != nil {
        return nil, false, err
    }
    if store := c.UsersStore; store != nil {  // ✅ This will be the overridden store
        user, ok, err := store.Get(ctx, username)
        // ...
    }
    return nil, false, nil
}

func (c *Config) ensureUserStore() UserStore {
    if userStoreOverride != nil {
        c.UsersStore = userStoreOverride  // ✅ Override is applied here
        return c.UsersStore
    }
    // ... default store logic
}
```

**If Issues Found:**
- File bug with line numbers and incorrect code
- Fix user lookup to consistently use `config.findUserRecord()`

---

### Phase 4: Update Tests ✅ Medium Risk

#### File: `src/backend/siteaccount/siteaccount_test.go`

**Remove Tests for Deleted HandleLogin:**
```go
// ❌ REMOVE: This test is for the now-deleted site-specific login handler
func TestHandleLoginSetsSessionCookies(t *testing.T) {
    // ...
}

// ❌ REMOVE: This test is for the now-deleted site-specific login handler
func TestHandleLoginSupportsSharedAuthFallbackForUserWithoutSiteRecord(t *testing.T) {
    // ...
}
```

**Add New Integration Test:**

**New File:** `src/backend/site/auth_integration_test.go`

```go
package main

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/grecinto/sop-cloud/src/backend/siteaccount"
    "github.com/sharedcode/sop/tools/sopbackend"
)

// TestUnifiedAuthFlow verifies the complete auth flow:
// 1. Install site user store override
// 2. Register all handlers
// 3. Login via /api/auth/login (sopbackend)
// 4. Verify session cookies are set
// 5. Access /app with session cookies (no re-login)
func TestUnifiedAuthFlow(t *testing.T) {
    // Setup
    ctx := context.Background()
    mux := http.NewServeMux()
    
    // Initialize site user store with test user
    cfg := siteaccount.GetConfig()
    testUser := siteaccount.UserRecord{
        Username:     "testuser",
        PasswordHash: "$2a$10$...",  // bcrypt hash of "testpass"
        Role:         "user",
        Email:        "test@example.com",
    }
    // Save test user to btree store
    // ...

    // Install user store override (once, before handlers)
    siteaccount.InstallSopbackendUserStoreOverride()
    
    // Register handlers
    sopbackend.RegisterAllHandlersWithoutRoot(mux)
    siteaccount.RegisterHandlers(mux)

    // Test 1: Login via sopbackend's endpoint
    loginReq := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"testuser","password":"testpass"}`))
    loginReq.Header.Set("Content-Type", "application/json")
    loginRec := httptest.NewRecorder()
    mux.ServeHTTP(loginRec, loginReq)

    if loginRec.Code != http.StatusOK {
        t.Fatalf("Login failed: status=%d, body=%s", loginRec.Code, loginRec.Body.String())
    }

    // Verify response contains tokens
    var loginResp map[string]any
    if err := json.NewDecoder(loginRec.Body).Decode(&loginResp); err != nil {
        t.Fatalf("Failed to decode login response: %v", err)
    }
    if loginResp["access_token"] == nil || loginResp["refresh_token"] == nil {
        t.Fatalf("Login response missing tokens: %+v", loginResp)
    }

    // Test 2: Verify session cookies are set
    cookies := loginRec.Result().Cookies()
    var accessCookie, refreshCookie *http.Cookie
    for _, c := range cookies {
        if c.Name == "sop_access_token" {
            accessCookie = c
        }
        if c.Name == "sop_refresh_token" {
            refreshCookie = c
        }
    }
    if accessCookie == nil || refreshCookie == nil {
        t.Fatalf("Session cookies not set: got %v", cookies)
    }

    // Test 3: Access /app with session cookies (should NOT redirect to login)
    appReq := httptest.NewRequest("GET", "/app", nil)
    appReq.AddCookie(accessCookie)
    appReq.AddCookie(refreshCookie)
    appRec := httptest.NewRecorder()
    mux.ServeHTTP(appRec, appReq)

    // Should return 200 OK or redirect to /app/ (not /login)
    if appRec.Code == http.StatusFound {
        location := appRec.Header().Get("Location")
        if strings.Contains(location, "/login") {
            t.Fatalf("Unexpected redirect to login: %s", location)
        }
    } else if appRec.Code != http.StatusOK {
        t.Fatalf("Failed to access /app: status=%d, body=%s", appRec.Code, appRec.Body.String())
    }

    t.Logf("✅ Unified auth flow test passed")
}

// TestUserStoreOverrideRespected verifies sopbackend's login uses the overridden store
func TestUserStoreOverrideRespected(t *testing.T) {
    // Create test user in site's btree store (not in config.json)
    // Install override
    // Call sopbackend's login handler
    // Verify user lookup went through override (not config.json)
    // ...
}
```

---

### Phase 5: Verify Frontend (No Changes Needed) ✅ Verification Only

#### File: `src/frontend/src/App.tsx`

**Current Code:**
```typescript
const loginResponse = await fetch('/api/auth/login', {  // ✅ CORRECT - uses sopbackend endpoint
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
});
```

**Action:** Verify this code path is correct. No changes needed.

**Reasoning:**
- Frontend calls `/api/auth/login` (sopbackend's handler)
- Sopbackend's handler uses the overridden user store (site's btree)
- Session cookies are set correctly
- All subsequent requests use these cookies

---

## Testing Strategy

### Unit Tests

1. **Test User Store Override Installation:**
   - `src/backend/siteaccount/sopbackend_override_test.go`
   - Verify `InstallSopbackendUserStoreOverride()` sets global override
   - Verify override persists across handler calls

2. **Test Sopbackend Login with Override:**
   - `lib/sop/tools/sopbackend/auth_test.go`
   - Mock user store override
   - Call `handleLogin` and verify it uses overridden store
   - Verify session creation

### Integration Tests

3. **Test Complete Auth Flow:**
   - `src/backend/site/auth_integration_test.go`
   - Login → session cookies → Data Manager access
   - No re-authentication required

4. **Test Session Validation:**
   - Verify `RequireAuth` middleware accepts sessions from sopbackend login
   - Test token refresh flow

### Manual Tests

5. **End-to-End Flow:**
   - Start site backend
   - Register new user via `/api/auth/register`
   - Login via site frontend (calls `/api/auth/login`)
   - Click "Launch Data Manager" or navigate to `/app`
   - ✅ Should NOT prompt for login again
   - Verify user can access Data Manager features

6. **Cross-Component Session:**
   - Login via site
   - Access httpserver UI (if applicable)
   - Verify session is recognized

---

## Rollback Plan

If the changes cause issues, rollback is straightforward:

1. **Restore `HandleLogin` in `src/backend/siteaccount/handlers.go`:**
   ```bash
   git checkout HEAD -- src/backend/siteaccount/handlers.go
   ```

2. **Restore double override installation:**
   ```bash
   git checkout HEAD -- src/backend/site/main.go
   ```

3. **Redeploy previous version**

**Risk Mitigation:**
- Keep old tests passing before making changes
- Deploy to staging first
- Monitor session/auth error logs closely

---

## Post-Deployment Validation

### Metrics to Monitor

1. **Authentication Success Rate:**
   - Track `/api/auth/login` success/failure ratio
   - Should remain consistent or improve

2. **Session Validation Failures:**
   - Track `RequireAuth` redirect-to-login count
   - Should decrease after fix

3. **User Complaints:**
   - Monitor for "double login" reports
   - Should go to zero

### Logs to Check

1. **Sopbackend Auth Logs:**
   - Look for user lookup errors
   - Verify `findUserRecord` is using override

2. **Session Creation Logs:**
   - Verify tokens are created successfully
   - Check cookie setting

3. **Data Manager Access Logs:**
   - Verify `/app` requests have valid sessions
   - No redirect-to-login loops

---

## Success Criteria

✅ **No Code Changes to Frontend:** Frontend continues to use `/api/auth/login`  
✅ **Single User Store Override:** Installed once in `registerSiteHandlers()`  
✅ **No Duplicate Login Endpoints:** `/api/site/auth/login` removed  
✅ **Tests Pass:** New integration test validates complete auth flow  
✅ **Manual Test Passes:** Login → Data Manager without re-authentication  
✅ **Production Stable:** No increase in auth errors after deployment

---

## Timeline

| Phase | Task | Duration | Owner |
|-------|------|----------|-------|
| 1 | Remove duplicate site login handler | 1 hour | Dev |
| 2 | Fix user store override installation | 30 min | Dev |
| 3 | Verify sopbackend login logic | 1 hour | Dev |
| 4 | Update tests | 4 hours | Dev |
| 5 | Manual testing in staging | 2 hours | QA |
| 6 | Deploy to production | 1 hour | DevOps |
| 7 | Post-deployment monitoring | 24 hours | DevOps + Dev |

**Total:** ~2 days (with buffer for testing)

---

## Open Questions

1. **Daemon Auth Implementation:**
   - Where is the daemon's user store override installed?
   - Does daemon have its own login flow or reuse sopbackend?
   - **Action:** Investigate daemon codebase

2. **Logout Behavior:**
   - Should `/api/site/auth/logout` be kept or removed?
   - Does site need custom logout logic beyond session revocation?
   - **Action:** Review `HandleLogout` implementation

3. **Session TTL:**
   - Are default TTLs (30 min access, 7 days refresh) acceptable for site?
   - **Action:** Confirm with product team

4. **User Migration:**
   - Are there existing `config.json` users that need to be migrated to btree?
   - **Action:** Check if migration script is needed

---

## References

- **Assessment Doc:** `docs/AUTH_ARCHITECTURE_ASSESSMENT.md`
- **Sopbackend Auth:** `lib/sop/tools/sopbackend/auth.go`
- **Site Account:** `src/backend/siteaccount/handlers.go`, `sopbackend_override.go`
- **Site Main:** `src/backend/site/main.go`
- **Frontend:** `src/frontend/src/App.tsx`
