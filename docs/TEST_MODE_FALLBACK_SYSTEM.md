# Test Mode Fallback System

## Overview

This document describes the test mode fallback system that gates test-specific fallback behaviors behind a configuration flag.

## Problem Statement

Previously, the codebase had fallback behaviors scattered throughout for nil/missing values that were needed for tests but inappropriate for production SDK usage. For example:

```go
// OLD CODE (before refactoring)
func getOrInitScriptContext(ctx context.Context) *ScriptContext {
    p := ai.GetSessionPayload(ctx)
    if p == nil {
        // Always returned a fallback, even in production
        return NewScriptContext()
    }
    // ...
}
```

**Issues:**
- Production SDK users got fallback behavior masking bugs
- Invalid conditions didn't fail fast
- Tests passed with incomplete setup
- No clear distinction between test and production behavior

## Solution: Test Mode Flag

### Architecture

1. **Config-Level Flag** (`Config.TestMode`):
   - Added to `Config` struct in `config.go`
   - Explicitly enables test fallbacks
   - **MUST be `false` (default) in production**

2. **Context Propagation** (`CtxKeyTestMode`):
   - Test mode propagated via context
   - Set in `runAskPipeline` if `Config.TestMode` is enabled
   - Checked at fallback points via `isTestMode(ctx)`

3. **Fail-Fast Production Behavior**:
   - When `TestMode` is disabled (production), code panics/errors on invalid conditions
   - Forces SDK users to fix bugs rather than mask them

### Implementation

#### 1. Configuration

```go
// In config.go
type Config struct {
    // ...
    TestMode bool `json:"test_mode,omitempty"` // Enable test fallbacks
}
```

#### 2. Context Propagation

```go
// In ask_pipeline.go
func (a *CopilotAgent) runAskPipeline(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
    // Enable test mode fallbacks if configured
    if a.Config.TestMode {
        ctx = WithTestMode(ctx)
    }
    // ...
}
```

#### 3. Gated Fallback

```go
// In copilottools.utils.go
func getOrInitScriptContext(ctx context.Context) *ScriptContext {
    p := ai.GetSessionPayload(ctx)
    if p == nil {
        // PRODUCTION: Fail fast
        if isTestMode(ctx) {
            return NewScriptContext() // Test fallback
        }
        // Panic in production - indicates a bug
        panic("getOrInitScriptContext: SessionPayload is required but was nil")
    }
    // ...
}
```

## Usage in Tests

### Method 1: Via Config

```go
func TestMyFeature(t *testing.T) {
    cfg := TestConfig(t) // Creates config with TestMode = true
    agent := NewCopilotAgent(*cfg, databases, systemDB)
    // ...
}
```

### Method 2: Explicit Context

```go
func TestUtilityFunction(t *testing.T) {
    ctx := TestContextWithTestMode(context.Background())
    result := getOrInitScriptContext(ctx) // Will use fallback
    // ...
}
```

### Method 3: Enable on Existing Config

```go
func TestExistingConfig(t *testing.T) {
    cfg := &Config{
        ID: "my-agent",
        // ... other fields
    }
    EnableTestMode(cfg) // Explicitly enable
    agent := NewCopilotAgent(*cfg, databases, systemDB)
    // ...
}
```

## Migration Guide for Existing Tests

### Step 1: Identify Failing Tests

After this refactoring, tests that rely on fallbacks will fail with panics like:

```
panic: getOrInitScriptContext: SessionPayload is required but was nil
```

### Step 2: Enable Test Mode

Add test mode to your test:

```go
// BEFORE
func TestMyFeature(t *testing.T) {
    cfg := Config{ID: "test"}
    agent := NewCopilotAgent(cfg, ...)
    result, _ := agent.Ask(ctx, "query", nil)
}

// AFTER
func TestMyFeature(t *testing.T) {
    cfg := Config{ID: "test"}
    EnableTestMode(&cfg) // Add this line
    agent := NewCopilotAgent(cfg, ...)
    result, _ := agent.Ask(ctx, "query", nil)
}
```

Or use the helper:

```go
// AFTER (using helper)
func TestMyFeature(t *testing.T) {
    cfg := TestConfig(t) // Already has TestMode enabled
    agent := NewCopilotAgent(*cfg, ...)
    result, _ := agent.Ask(ctx, "query", nil)
}
```

### Step 3: Consider Fixing the Test Instead

**Better approach:** Fix the test to provide valid state:

```go
// BEST: Fix the test setup
func TestMyFeature(t *testing.T) {
    cfg := Config{ID: "test"}
    agent := NewCopilotAgent(cfg, ...)
    
    // Provide proper session payload
    payload := &ai.SessionPayload{
        UserID:    "test-user",
        SessionID: "test-session",
        Variables: make(map[string]any),
    }
    ctx := context.WithValue(context.Background(), "session_payload", payload)
    
    result, _ := agent.Ask(ctx, "query", nil)
}
```

## Gated Fallback Patterns

### Pattern 1: Nil Payload Fallback

**Location:** `copilottools.utils.go:getOrInitScriptContext()`

```go
if p == nil {
    if isTestMode(ctx) {
        return NewScriptContext() // Test fallback
    }
    panic("SessionPayload required") // Production fails fast
}
```

**Why:** SessionPayload carries session state. If nil in production, it's a critical bug that should fail fast.

### Pattern 2: Database Resolution Fallback

**Location:** `copilot_executor.go:shouldObfuscate()`

```go
if opts, ok := a.databases[dbName]; ok {
    return opts.EnableObfuscation
}
// No test mode check here - returning false is valid default behavior
return false
```

**Why:** This is legitimate default behavior (no obfuscation if DB not found), not a test-only fallback.

## Rules for Adding New Gated Fallbacks

### When to Gate Behind Test Mode

✅ **Gate if:**
- Returning a default value masks a bug
- Production code should fail fast on this condition
- The fallback is only needed for incomplete test setup
- Example: Returning empty/default object when required parameter is nil

### When NOT to Gate

❌ **Don't gate if:**
- It's legitimate control flow (e.g., "if not found, try alternative")
- It's a valid default value (e.g., "no results" is valid)
- It's graceful degradation (e.g., "if cache miss, compute")
- Example: Returning `false` for a boolean check when optional config is missing

### Implementation Checklist

1. Add `isTestMode(ctx)` check before fallback
2. In test mode: execute fallback
3. In production mode: panic/return error with clear message
4. Add comment explaining why it's gated
5. Update test helpers documentation if adding new pattern

## Example: Adding a New Gated Fallback

```go
// BEFORE (ungated fallback)
func getSomething(ctx context.Context) *Thing {
    payload := getPayload(ctx)
    if payload == nil {
        return &Thing{} // Always returns default
    }
    return payload.Thing
}

// AFTER (gated fallback)
func getSomething(ctx context.Context) *Thing {
    payload := getPayload(ctx)
    if payload == nil {
        if isTestMode(ctx) {
            return &Thing{} // Test fallback
        }
        panic("getSomething: payload is required but was nil. Enable TestMode for tests.")
    }
    return payload.Thing
}
```

## Testing the System

### Verify Test Mode Works

```go
func TestFallbacksWorkInTestMode(t *testing.T) {
    ctx := TestContextWithTestMode(context.Background())
    // This should NOT panic
    result := getOrInitScriptContext(ctx)
    if result == nil {
        t.Error("Expected fallback to return non-nil context")
    }
}
```

### Verify Production Mode Fails

```go
func TestProductionModeFailsFast(t *testing.T) {
    ctx := context.Background() // No test mode
    defer func() {
        if r := recover(); r == nil {
            t.Error("Expected panic in production mode")
        }
    }()
    _ = getOrInitScriptContext(ctx) // Should panic
}
```

## Production Deployment Checklist

Before deploying to production:

- [ ] Verify `Config.TestMode` is `false` in all production configs
- [ ] Verify no code path sets `TestMode = true` outside tests
- [ ] Verify `WithTestMode(ctx)` is only called in test files
- [ ] Run integration tests WITHOUT test mode enabled
- [ ] Confirm SDK documentation warns against enabling TestMode

## Benefits

1. **Fail Fast in Production**: Bugs surface immediately
2. **Clear Test Intent**: Explicitly enables test behavior
3. **Easier Debugging**: Production errors are clear and actionable
4. **Better SDK UX**: Users aren't surprised by masked bugs
5. **Test Clarity**: Tests explicitly declare they need fallbacks

## Related Files

- `config.go` - TestMode flag definition
- `copilottools.utils.go` - isTestMode(), WithTestMode()
- `test_helpers.go` - Test utilities (EnableTestMode, TestConfig, etc.)
- `ask_pipeline.go` - Test mode context propagation

## Future Considerations

1. Add linter rule to prevent `TestMode = true` in non-test files
2. Add runtime check on agent creation warning if TestMode enabled
3. Consider environment variable override for debugging
4. Add metrics/logging when test mode is used
