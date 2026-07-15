# Authentication Architecture - Quick Reference

**Last Updated:** 2026-07-14

---

## Current Problem 🔴

**Symptom:** Double authentication - users must login twice (once on site, again for Data Manager)

**Root Cause:**
1. Two login endpoints exist: `/api/auth/login` and `/api/site/auth/login`
2. User store override installed twice (redundant)
3. Session validation may not consistently use overridden user store

---

## Correct Architecture ✅

### Single Auth Feature (Sopbackend)

```
┌─────────────────────────────────────┐
│     Sopbackend (Core Auth)          │
│  • Session management               │
│  • Token validation                 │
│  • Login/refresh/logout handlers    │
│  • UserStore interface (abstract)   │
└─────────────────────────────────────┘
              ↑ ↑ ↑
              │ │ └── HTTP Server (config.json users)
              │ └──── Daemon (OS user auth)
              └────── New Site (btree user store)
```

### User Store Override Pattern

Each runtime installs its own user store implementation:

```go
// Site (btree-backed persistent users)
siteaccount.InstallSopbackendUserStoreOverride()

// Daemon (OS user authentication)
daemon.InstallOSUserStoreOverride()  // hypothetical

// HTTP Server (default: config.json)
// No override needed - uses default
```

---

## Key Principles

1. **One Auth Feature:** Sopbackend owns all authentication logic
2. **One Login Endpoint:** `/api/auth/login` (sopbackend handler)
3. **One Session Store:** Sopbackend's `SessionStore` (JWT tokens + cookies)
4. **Multiple User Stores:** Each runtime provides its own `UserStore` implementation
5. **Install Once:** User store override installed **before** handler registration
6. **Transparent to Handlers:** All sopbackend handlers automatically use override

---

## Fix Summary

### Changes Required

| Component | File | Action |
|-----------|------|--------|
| Site Login Handler | `src/backend/siteaccount/handlers.go` | **Remove** `HandleLogin` function |
| Site Handler Registration | `src/backend/siteaccount/handlers.go` | **Remove** `/api/site/auth/login` route |
| Site Bootstrap | `src/backend/site/main.go` | **Remove** duplicate override installation (line 319) |
| Site Bootstrap | `src/backend/site/main.go` | **Add** comments explaining override pattern |
| Tests | `src/backend/siteaccount/siteaccount_test.go` | **Remove** tests for deleted `HandleLogin` |
| Tests | `src/backend/site/auth_integration_test.go` | **Add** unified auth flow test |

### No Changes Required

| Component | Reason |
|-----------|--------|
| Frontend (`App.tsx`) | Already calls `/api/auth/login` (correct) |
| Sopbackend Login | Already uses `config.findUserRecord()` (respects override) |
| Session Validation | Already uses overridden user store |

---

## Testing Checklist

### Before Deployment ✅

- [ ] Remove site's `HandleLogin` function
- [ ] Fix double override installation
- [ ] Add integration test for unified auth flow
- [ ] Run all existing tests (should pass)
- [ ] Manual test: login → Data Manager (no re-login)
- [ ] Manual test: refresh token rotation works
- [ ] Manual test: logout clears session

### After Deployment ✅

- [ ] Monitor auth success rate (should stay consistent)
- [ ] Monitor `/app` redirect-to-login rate (should drop)
- [ ] Check for "double login" user complaints (should be zero)
- [ ] Verify session logs show override being used

---

## API Endpoints (After Fix)

### Authentication (Sopbackend)
```
POST   /api/auth/login       → Login (uses overridden user store)
POST   /api/auth/refresh     → Refresh session tokens
GET    /login                → Login page (HTML)
```

### Site-Specific (Siteaccount)
```
POST   /api/auth/register               → Register new site user
GET    /api/auth/me                     → Current user info
GET    /api/auth/status                 → Billing/subscription status
POST   /api/auth/subscription           → Update subscription plan
POST   /api/site/auth/logout            → Logout (site-specific cleanup)
POST   /api/auth/unregister             → Delete account
```

### Protected Routes (Require Session)
```
GET    /app                  → Data Manager (requires auth)
GET    /app/subscribe        → Subscription page
GET    /app/unregister       → Account deletion page
```

---

## Session Cookies

| Cookie | Purpose | TTL | Scope |
|--------|---------|-----|-------|
| `sop_access_token` | JWT access token | 30 min | All routes |
| `sop_refresh_token` | Token rotation | 7 days | Refresh endpoint |

**Properties:**
- `HttpOnly`: Yes (prevents XSS)
- `Secure`: Yes (HTTPS only in production)
- `SameSite`: Lax (CSRF protection)

---

## User Store Implementations

### 1. Config.json Store (Default)
**Used by:** HTTP Server (standalone)  
**Location:** `lib/sop/tools/sopbackend/auth.go` (`UserStoreImpl`)  
**Persistence:** In-memory (loaded from `config.json`)

### 2. Btree Store (Site)
**Used by:** New site backend  
**Location:** `src/backend/siteaccount/sopbackend_override.go` (`SopbackendUserStoreAdapter`)  
**Persistence:** Btree database (persistent across restarts)

### 3. OS User Store (Daemon)
**Used by:** Daemon (hypothetical)  
**Location:** TBD (needs investigation)  
**Persistence:** System user database (OS-level)

---

## Common Pitfalls 🚨

### ❌ Installing Override After Handler Registration
```go
// WRONG - handlers already registered with default store
sopbackend.RegisterAllHandlersWithoutRoot(mux)
siteaccount.InstallSopbackendUserStoreOverride()  // Too late!
```

### ✅ Correct Order
```go
// RIGHT - override installed first
siteaccount.InstallSopbackendUserStoreOverride()
sopbackend.RegisterAllHandlersWithoutRoot(mux)  // Handlers use override
```

### ❌ Creating Site-Specific Login Handler
```go
// WRONG - duplicates sopbackend's login logic
func HandleLogin(w http.ResponseWriter, r *http.Request) {
    // Custom login logic...
    sopbackend.CreateSession(...)  // Why not use sopbackend's handler?
}
```

### ✅ Use Sopbackend's Handler Directly
```go
// RIGHT - sopbackend's handler uses overridden user store automatically
// No site-specific login handler needed
```

### ❌ Installing Override Multiple Times
```go
// WRONG - redundant and potentially racy
siteaccount.InstallSopbackendUserStoreOverride()
sopbackend.RegisterAllHandlersWithoutRoot(mux)
siteaccount.InstallSopbackendUserStoreOverride()  // Redundant!
```

### ✅ Install Once
```go
// RIGHT - single installation before handlers
siteaccount.InstallSopbackendUserStoreOverride()
sopbackend.RegisterAllHandlersWithoutRoot(mux)
```

---

## Troubleshooting

### Issue: "Invalid credentials" after entering correct password

**Cause:** Login handler not using overridden user store

**Debug:**
1. Verify override installed before handlers: `siteaccount.InstallSopbackendUserStoreOverride()`
2. Check sopbackend login uses `config.findUserRecord()` (not direct `config.Users` access)
3. Add log in `SopbackendUserStoreAdapter.Get()` to confirm it's called

---

### Issue: Data Manager redirects to login after site login

**Cause:** Session validation not recognizing site-issued tokens

**Debug:**
1. Check session cookies are set after login (browser DevTools)
2. Verify `/app` request includes cookies
3. Confirm `RequireAuth` middleware uses `config.AuthenticateBearerToken()`
4. Add log in `ValidateSessionToken()` to see token validation flow

---

### Issue: User not found after registration

**Cause:** User saved to wrong store (not persisted)

**Debug:**
1. Verify registration uses overridden user store
2. Check `SopbackendUserStoreAdapter.Save()` persists to btree
3. Verify btree database path is correct and writable

---

## Code Examples

### Correct Site Bootstrap
```go
func registerSiteHandlers(mux *http.ServeMux, frontendDir, docsDir string) {
    // 1. Install user store override ONCE
    siteaccount.InstallSopbackendUserStoreOverride()

    // 2. Register sopbackend handlers (use override)
    sopbackend.RegisterAllHandlersWithoutRoot(mux)

    // 3. Register site-specific handlers
    siteaccount.RegisterHandlers(mux)

    // 4. Register other handlers...
    registerFrontendHandler(mux, frontendDir)
    registerDocsHandler(mux, docsDir)
}
```

### Correct Site Handler Registration
```go
func RegisterHandlers(mux *http.ServeMux) {
    // NO login handler - sopbackend provides /api/auth/login

    mux.HandleFunc("/api/auth/register", HandleRegister)
    mux.HandleFunc("/api/site/auth/logout", HandleLogout)
    mux.HandleFunc("/api/auth/me", withSiteAuth(HandleCurrentUser))
    mux.HandleFunc("/api/auth/subscription", withSiteAuth(HandleSubscriptionUpdate))
    // ... other site-specific handlers
}
```

### Frontend Login Call
```typescript
// Correct - calls sopbackend's login endpoint
const response = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
});

// Session cookies are automatically set by server
// All subsequent requests include cookies
```

---

## Related Documentation

- **Full Assessment:** `docs/AUTH_ARCHITECTURE_ASSESSMENT.md`
- **Action Plan:** `docs/AUTH_FIX_ACTION_PLAN.md`
- **User Store Override:** `src/backend/siteaccount/sopbackend_override.go`
- **Sopbackend Auth:** `lib/sop/tools/sopbackend/auth.go`

---

## Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-07-14 | Remove `/api/site/auth/login` | Duplicate of sopbackend's endpoint; causes confusion |
| 2026-07-14 | Install override once before handlers | Ensures all handlers use overridden user store |
| 2026-07-14 | Keep `/api/site/auth/logout` | May have site-specific cleanup logic |
| 2026-07-14 | Frontend keeps using `/api/auth/login` | Already correct; no changes needed |
