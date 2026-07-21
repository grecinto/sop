# Multi-Provider Fallback System

## Overview

The **Multi-Provider Fallback Generator** implements automatic provider failover to ensure sustained throughput and quality even when one LLM provider experiences outages, rate limits, or traffic spikes.

## Key Features

### 1. Automatic Provider Switching
When the primary provider fails with a retryable error (network issues, rate limits, capacity problems), the system automatically switches to fallback providers without user intervention.

### 2. Circuit Breaker Pattern
After 3 consecutive failures, a provider's circuit breaker opens for 5 minutes, preventing repeated attempts to a failing service and allowing faster fallback.

### 3. Health Tracking
All providers are continuously monitored with:
- Consecutive failure counts
- Last failure timestamp
- Circuit breaker status
- Automatic recovery on success

### 4. Intelligent Error Classification
The system distinguishes between:
- **Retryable errors**: Network timeouts, rate limits (429), service unavailable (503), capacity issues
- **Non-retryable errors**: Authentication (401), forbidden (403), invalid requests (400)

Only retryable errors trigger failover.

## Usage

### Basic Setup

```go
import (
    "github.com/sharedcode/sop/ai"
    "github.com/sharedcode/sop/ai/generator"
)

// Create individual generators
anthropic, _ := generator.New("anthropic", map[string]any{
    "api_key": anthropicKey,
    "model": "claude-3-5-sonnet-20241022",
})

openai, _ := generator.New("chatgpt", map[string]any{
    "api_key": openaiKey,
    "model": "gpt-4o",
})

gemini, _ := generator.New("gemini", map[string]any{
    "api_key": geminiKey,
    "model": "gemini-2.0-flash",
})

// Create fallback generator with automatic switching
gen := generator.NewFallbackGenerator(
    anthropic,              // Primary provider
    []ai.Generator{openai, gemini},  // Fallback chain
)

// Use normally - failover is automatic
output, err := gen.Generate(ctx, "Write a function...", ai.GenOptions{
    SystemPrompt: "You are a code expert",
    Temperature: 0.1,
})
```

### Configuration-Based Setup

```go
fallbackConfig := map[string]any{
    "primary": map[string]any{
        "type": "anthropic",
        "api_key": anthropicKey,
        "model": "claude-3-5-sonnet-20241022",
    },
    "fallbacks": []map[string]any{
        {
            "type": "chatgpt",
            "api_key": openaiKey,
            "model": "gpt-4o",
        },
        {
            "type": "gemini",
            "api_key": geminiKey,
            "model": "gemini-2.0-flash",
        },
    },
}

gen := createFallbackGeneratorFromConfig(fallbackConfig)
```

### Monitoring Provider Health

```go
health := gen.GetProviderHealth()
for provider, status := range health {
    fmt.Printf("Provider: %s\n", status.Provider)
    fmt.Printf("  Consecutive Fails: %d\n", status.ConsecutiveFails)
    fmt.Printf("  Circuit Open: %v\n", status.CircuitOpen)
    if status.CircuitOpen {
        fmt.Printf("  Opened At: %s\n", status.OpenedAt)
    }
}
```

## How It Works

### 1. Normal Operation
```
User Request → Primary Provider → Success → Return Result
```

### 2. Primary Failure (Retryable)
```
User Request → Primary Provider → Failure (503)
            → Fallback 1 → Success → Return Result
```

### 3. Circuit Breaker Opens
```
Request 1 → Primary Fails
Request 2 → Primary Fails  
Request 3 → Primary Fails → Circuit Opens

Request 4 → Skip Primary (circuit open)
         → Fallback 1 → Success → Return Result
         
(After 5 minutes, circuit closes and primary is retried)
```

### 4. All Providers Fail
```
User Request → Primary → Fail
            → Fallback 1 → Fail
            → Fallback 2 → Fail
            → Return Error
```

## Error Handling

### Retryable Errors (Trigger Fallback)
- Network timeouts
- Connection refused
- HTTP 429 (rate limit)
- HTTP 503 (service unavailable)
- HTTP 502/504 (gateway errors)
- HTTP 500 (internal server error)
- Quota exceeded
- Capacity/overload issues
- Provider unavailable/maintenance

### Non-Retryable Errors (Immediate Failure)
- HTTP 401 (unauthorized/bad API key)
- HTTP 403 (forbidden)
- HTTP 400 (bad request)
- Invalid request format
- Missing API key

## Configuration Parameters

### Circuit Breaker
- **Max Consecutive Failures**: 3 (opens circuit)
- **Circuit Timeout**: 5 minutes (auto-closes after)
- **Health Check Interval**: 30 seconds

### Request Timeouts
- **Per-Provider Timeout**: 90 seconds
- Prevents one slow provider from blocking the entire chain

## Best Practices

### 1. Choose Compatible Models
Select models with similar capabilities across providers:
- **Tier 1**: Claude Opus 4.8, GPT-5.4, Gemini 3.1 Pro
- **Tier 2**: Claude Sonnet 4.6, GPT-4o, Gemini 2.0 Flash
- **Tier 3**: Claude Haiku, GPT-4o-mini, Gemini Flash Lite

### 2. Order Fallbacks by Preference
Put your preferred provider first, then alternatives:
```go
// Primary: Best quality/cost ratio
// Fallback 1: Similar quality, higher availability  
// Fallback 2: Lower cost, highest availability
NewFallbackGenerator(claude, []ai.Generator{gpt, gemini})
```

### 3. Monitor Health Metrics
Regularly check provider health to identify patterns:
- Repeated circuit opens → consider swapping primary/fallback
- One provider consistently failing → investigate API key/quotas
- All providers failing → check network/infrastructure

### 4. Temperature Consistency
All providers use the same strict temperature control (0.1) by default, ensuring consistent behavior across failovers for code generation tasks.

### 5. System Prompt Propagation
System prompts are deterministically prepended to all provider requests, ensuring consistent behavior regardless of which provider handles the request.

## Integration with Existing Code

The FallbackGenerator implements the standard `ai.Generator` interface, so it's a drop-in replacement:

```go
// Before
gen, _ := generator.New("anthropic", config)

// After - with automatic fallback
primary, _ := generator.New("anthropic", primaryConfig)
fallback, _ := generator.New("chatgpt", fallbackConfig)
gen := generator.NewFallbackGenerator(primary, []ai.Generator{fallback})

// All existing code works unchanged
output, err := gen.Generate(ctx, prompt, opts)
```

## Testing

Run the comprehensive test suite:
```bash
go test ./lib/sop/ai/generator -run TestFallbackGenerator
```

Key test scenarios:
- Primary success (no fallback needed)
- Primary failure with fallback success
- All providers failing
- Circuit breaker opening after 3 failures
- Circuit breaker auto-recovery after timeout
- Non-retryable error handling
- Health tracking and recovery

## Advantages Over Consumer Copilots

Unlike GitHub Copilot, Cursor, or Cody, which hide their provider infrastructure:

1. **Transparent Control**: You control the fallback chain and can inspect provider health
2. **No Hidden Changes**: Provider behavior doesn't change unexpectedly with product updates
3. **Cost Control**: You manage your own API keys and can optimize costs
4. **Privacy**: All data stays within your chosen providers
5. **Sustained Quality**: Failover happens automatically without degrading the user experience

## Performance Impact

- **Latency**: Primary request latency unchanged; fallback adds ~1-2s on failure
- **Throughput**: Maintains full throughput even when one provider is degraded
- **Cost**: No additional cost unless fallback is triggered
- **Memory**: Minimal overhead (~50 bytes per provider for health tracking)

## Troubleshooting

### Circuit Keeps Opening
- Check provider API quotas
- Verify API keys are valid
- Monitor provider status pages
- Consider increasing rate limits or upgrading plan

### Fallback Never Triggered
- Ensure primary is actually failing (check logs)
- Verify error is retryable (check error classification)
- Test with intentional failures

### All Providers Fail
- Check network connectivity
- Verify API keys for all providers
- Check provider status pages
- Review request format/parameters
