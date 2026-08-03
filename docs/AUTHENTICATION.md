# Authentication Architecture

Complete reference for the unified authentication system across Site, Daemon, and HTTP Server.

---

## Table of Contents
- [Overview](#overview)
- [Architecture](#architecture)
- [User Store Pattern](#user-store-pattern)
- [Implementation Guide](#implementation-guide)
- [Token Management](#token-management)
- [Security](#security)

---

## Overview

**Current Status**: ✅ Unified authentication fully operational

SOP uses a **single authentication feature** built on sopbackend's session/token layer. Each runtime (Site, Daemon, HTTP Server) provides its own user store implementation while sharing the same authentication logic.

### Key Principles

1. **One Auth Feature**: Sopbackend owns all authentication logic
2. **One Login Endpoint**: `/api/auth/login` (sopbackend handler)
3. **One Session Store**: Sopbackend's SessionStore (JWT tokens + cookies)
4. **Multiple User Stores**: Each runtime provides its own UserStore implementation
5. **Install Once**: User store override installed before handler registration
6. **Transparent to Handlers**: All sopbackend handlers automatically use override

---

## Architecture

### Component Diagram

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
              └────── Site (btree user store)
```

### User Store Override Pattern

Each runtime installs its own user store implementation:

```go
// Site (btree-backed persistent users)
siteaccount.InstallSopbackendUserStoreOverride()

// Daemon (OS user authentication)
daemon.InstallOSUserStoreOverride()

// HTTP Server (default: config.json)
// No override needed - uses default
```

### Authentication Flow

```
Request → Subscription Check → Token Validation → Handler
  │              │                    │               │
  │              │                    │               └─→ Business Logic
  │              │                    └─→ Sopbackend validates JWT
  │              └─→ Site enforces subscription (if needed)
  └─→ User/Session context attached
```

**Important**: Subscription enforcement (siteAppAccessHandler) works WITH unified auth, not instead of it. It's an additional policy layer, not a duplicate authentication system.

---

## User Store Pattern

### Interface

**File**: `lib/sop/tools/sopbackend/auth.go`

```go
type UserStore interface {
    // GetUser retrieves user by username
    GetUser(ctx context.Context, username string) (User, error)
    
    // ValidateCredentials checks username/password
    ValidateCredentials(ctx context.Context, username, password string) (User, error)
}

type User struct {
    Username string
    Role     string
    // Additional fields...
}
```

### Site Implementation

**File**: `src/backend/siteaccount/sopbackend_override.go`

```go
type SopbackendUserStoreAdapter struct {
    siteStore *UserStore  // Site's btree-backed store
}

func (a *SopbackendUserStoreAdapter) GetUser(ctx context.Context, username string) (sopbackend.User, error) {
    // Bridge to site's user store
    siteUser, err := a.siteStore.Get(ctx, username)
    if err != nil {
        return sopbackend.User{}, err
    }
    
    // Convert to sopbackend.User format
    return sopbackend.User{
        Username: siteUser.Username,
        Role:     siteUser.Role,
    }, nil
}

func InstallSopbackendUserStoreOverride() {
    adapter := &SopbackendUserStoreAdapter{
        siteStore: GetUserStore(),
    }
    sopbackend.SetUserStoreOverride(adapter)
}
```

### Installation Order

**File**: `src/backend/site/main.go`

```go
func registerSiteHandlers(mux *http.ServeMux) {
    // STEP 1: Install user store override FIRST
    siteaccount.InstallSopbackendUserStoreOverride()
    
    // STEP 2: Register sopbackend handlers (uses override automatically)
    sopbackend.RegisterAllHandlersWithoutRoot(mux)
    
    // STEP 3: Register site-specific handlers
    siteaccount.RegisterHandlers(mux)
}
```

**Critical**: Install override BEFORE registering handlers. Handlers will automatically use the override for all user lookups.

---

## Implementation Guide

### Adding a New Runtime

1. **Create User Store Implementation**:
   ```go
   type MyRuntimeUserStore struct {
       // Your storage backend
   }
   
   func (s *MyRuntimeUserStore) GetUser(ctx context.Context, username string) (sopbackend.User, error) {
       // Your implementation
   }
   
   func (s *MyRuntimeUserStore) ValidateCredentials(ctx context.Context, username, password string) (sopbackend.User, error) {
       // Your implementation
   }
   ```

2. **Create Override Installer**:
   ```go
   func InstallMyRuntimeUserStoreOverride() {
       store := &MyRuntimeUserStore{}
       sopbackend.SetUserStoreOverride(store)
   }
   ```

3. **Install Before Handler Registration**:
   ```go
   func main() {
       mux := http.NewServeMux()
       
       // Install override FIRST
       myruntime.InstallMyRuntimeUserStoreOverride()
       
       // Register handlers
       sopbackend.RegisterAllHandlersWithoutRoot(mux)
       myruntime.RegisterHandlers(mux)
   }
   ```

### Frontend Login Flow

**File**: `src/frontend/src/App.tsx`

```typescript
async function handleLogin(username: string, password: string) {
    // Call sopbackend's login endpoint
    const response = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
        credentials: 'include'  // Include cookies
    });
    
    if (response.ok) {
        const data = await response.json();
        // Store tokens in localStorage
        localStorage.setItem('sop_access_token', data.access_token);
        localStorage.setItem('sop_refresh_token', data.refresh_token);
        return true;
    }
    return false;
}
```

### Token Refresh

```typescript
async function refreshToken() {
    const refreshToken = localStorage.getItem('sop_refresh_token');
    
    const response = await fetch('/api/auth/refresh', {
        method: 'POST',
        headers: { 
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${refreshToken}`
        }
    });
    
    if (response.ok) {
        const data = await response.json();
        localStorage.setItem('sop_access_token', data.access_token);
        return true;
    }
    return false;
}
```

---

## Token Management
### Future Evolution

- TODO: Add support for switching the database transaction mode later (for example between NoCheck and stricter modes) without changing the auth/session flow contracts.
### Session Creation

**File**: `lib/sop/tools/sopbackend/auth.go`

```go
func CreateSession(ctx context.Context, username, role string) (accessToken, refreshToken string, err error) {
    // Generate JWT tokens
    accessToken = generateJWT(username, role, 15*time.Minute)
    refreshToken = generateJWT(username, role, 48*time.Hour)
    
    // Set HTTP-only cookies
    setSecureCookie("sop_access_token", accessToken)
    setSecureCookie("sop_refresh_token", refreshToken)
    
    return accessToken, refreshToken, nil
}
```

### Token Lifetimes

| Token Type | Lifetime | Storage | Purpose |
|------------|----------|---------|---------|
| Access Token | 15 minutes | localStorage + cookie | API authentication |
| Refresh Token | 48 hours | localStorage + cookie | Token renewal |

### Validation

```go
func ValidateToken(ctx context.Context, token string) (username, role string, err error) {
    // Verify JWT signature
    claims, err := jwt.Parse(token, signingKey)
    if err != nil {
        return "", "", ErrInvalidToken
    }
    
    // Check expiration
    if claims.ExpiresAt < time.Now().Unix() {
        return "", "", ErrTokenExpired
    }
    
    return claims.Username, claims.Role, nil
}
```

---

## Security

### Best Practices

1. **HTTP-Only Cookies**: Tokens stored in HTTP-only cookies prevent XSS attacks
2. **Short Access Token Lifetime**: 15-minute expiry limits exposure
3. **Secure Flag**: Cookies marked secure in production (HTTPS only)
4. **Same-Site**: Cookies use SameSite=Strict to prevent CSRF
5. **Token Rotation**: Refresh tokens rotate on each use

### Protected Routes

```go
func withAuth(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Extract token from header or cookie
        token := extractToken(r)
        if token == "" {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        
        // Validate token
        username, role, err := ValidateToken(r.Context(), token)
        if err != nil {
            http.Error(w, "invalid token", http.StatusUnauthorized)
            return
        }
        
        // Add auth context to request
        ctx := context.WithValue(r.Context(), "username", username)
        ctx = context.WithValue(ctx, "role", role)
        
        next.ServeHTTP(w, r.WithContext(ctx))
    }
}
```

### Daemon Bootstrap Authentication

Daemons can authenticate using site session tokens:

**File**: `lib/sop/cmd/sop-daemon/auth.go`

```go
func HandleBootstrap(w http.ResponseWriter, r *http.Request) {
    var req BootstrapRequest
    json.NewDecoder(r.Body).Decode(&req)
    
    // Validate site session token
    if req.BootstrapToken != "" {
        username, role, err := sessionStore.ValidateTokenWithoutAccountCheck(req.BootstrapToken)
        if err == nil {
            // Create daemon-specific session
            accessToken, refreshToken, _ := CreateSession(username, role)
            json.NewEncoder(w).Encode(map[string]string{
                "access_token": accessToken,
                "refresh_token": refreshToken,
            })
            return
        }
    }
    
    // Fallback to bootstrap secret
    if req.BootstrapSecret != "" {
        // Validate secret...
    }
}
```

This same bootstrap and refresh pattern is also the expected local-auth path for Continue.

- Continue obtains daemon-local access and refresh tokens through `POST /api/daemon/auth`.
- Continue renews daemon-local access through `POST /api/daemon/refresh`.
- Continue must not treat those daemon-local tokens as HQ credentials.

### Continue, Daemon, and HQ session boundaries

Continue operates with two independent authenticated sessions:

1. HQ session
    - authenticates Continue directly with Northstar HQ
    - owns HQ access token, HQ refresh token, and forced re-authentication cadence
2. Daemon session
    - authenticates Continue to the local daemon bridge
    - owns daemon access token and daemon refresh token

The daemon session is local-runtime auth only. The HQ session is remote control-plane auth only.

For user experience, the HQ session may be handed over from daemon to Continue after the user authenticated through AI Studio UI. That avoids a second immediate HQ login when the user switches from the AI Studio surface into IDE coding.

When Continue calls daemon bridge endpoints, daemon bearer auth still happens via the `Authorization` header. Separately, Continue should pass its current HQ connectivity information into the daemon request body/config so daemon-side federation logic can later talk to HQ under the same Continue-owned session.

That propagated HQ context should include:

- HQ base URL
- HQ user ID
- HQ session ID
- HQ access token
- HQ refresh token when daemon background sync and renewal need to continue beyond the current request lifetime

The reverse handoff is also supported: daemon may provide the current HQ connectivity and token context back to Continue when invoking a registered Continue worker. After that handoff, Continue continues operating and refreshing under that adopted HQ session until a normal re-authentication boundary is reached.

---

## Common Patterns

### Subscription Enforcement Layer

The site adds a subscription check BEFORE token validation:

```go
func siteAppAccessHandler(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        auth := sop.GetAuthContext(r.Context())
        
        // Check subscription status (ADDITIONAL policy layer)
        if !hasActiveSubscription(auth) {
            http.Error(w, "subscription required", http.StatusPaymentRequired)
            return
        }
        
        // Continue to auth validation + handler
        next.ServeHTTP(w, r)
    })
}
```

This is NOT duplicate authentication - it's an additional policy check that works WITH the unified auth system.

### Role-Based Access Control

```go
func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
    return withAuth(func(w http.ResponseWriter, r *http.Request) {
        role := r.Context().Value("role").(string)
        if role != "admin" {
            http.Error(w, "forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

---

## Testing

### Unit Tests

**File**: `src/backend/site/main_test.go`

```go
func TestUnifiedAuthentication(t *testing.T) {
    // Install override
    siteaccount.InstallSopbackendUserStoreOverride()
    
    // Test login
    resp := testLogin(t, "testuser", "password")
    if resp.StatusCode != http.StatusOK {
        t.Fatal("Login failed")
    }
    
    // Extract tokens
    var data map[string]string
    json.NewDecoder(resp.Body).Decode(&data)
    accessToken := data["access_token"]
    
    // Test authenticated request
    req := httptest.NewRequest("GET", "/api/protected", nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)
    
    resp = testRequest(t, req)
    if resp.StatusCode != http.StatusOK {
        t.Fatal("Auth failed")
    }
}
```

---

## Anti-Patterns (DO NOT DO)

❌ **Creating separate login endpoints per runtime**:
```go
// BAD - duplicates auth logic
mux.HandleFunc("/api/site/auth/login", siteaccount.HandleLogin)
mux.HandleFunc("/api/daemon/auth/login", daemon.HandleLogin)
```

❌ **Validating tokens in multiple places**:
```go
// BAD - inconsistent validation
func myHandler(w http.ResponseWriter, r *http.Request) {
    token := r.Header.Get("Authorization")
    if !validateMyWay(token) {  // Don't reinvent validation
        // ...
    }
}
```

❌ **Installing override multiple times**:
```go
// BAD - second install overwrites first
siteaccount.InstallSopbackendUserStoreOverride()
sopbackend.RegisterAllHandlersWithoutRoot(mux)
siteaccount.InstallSopbackendUserStoreOverride()  // Redundant!
```

✅ **Correct Pattern**:
```go
// Install once, register handlers, done
siteaccount.InstallSopbackendUserStoreOverride()
sopbackend.RegisterAllHandlersWithoutRoot(mux)
siteaccount.RegisterHandlers(mux)
```

---

## Migration Notes

### History: Double Authentication Bug (Fixed 2026-07-14)

**Problem**: Site had both `/api/auth/login` (sopbackend) and `/api/site/auth/login` (site-specific), causing double login prompts.

**Root Cause**: Site's `HandleLogin` was registered alongside sopbackend's, creating two authentication systems.

**Fix**: 
1. Removed site's `HandleLogin` implementation
2. Removed `/api/site/auth/login` route registration
3. Updated frontend to call `/api/auth/login` (sopbackend)
4. Verified user store override works correctly

**Result**: Single authentication system, consistent behavior across all runtimes.

---

## Additional Resources

- **Session Store**: `lib/sop/tools/sopbackend/session.go`
- **User Store Interface**: `lib/sop/tools/sopbackend/auth.go`
- **Site Override**: `src/backend/siteaccount/sopbackend_override.go`
- **Site Handlers**: `src/backend/siteaccount/handlers.go`
- **Main Bootstrap**: `src/backend/site/main.go`

---

*Last Updated: 2026-07-15*
*Status: Production*
