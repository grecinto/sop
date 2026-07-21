# Fallback Provider Configuration Guide

## Overview

The AI Studio now supports automatic LLM provider fallback. If your primary provider fails (rate limits, outages, API errors), requests automatically fail over to a backup provider.

## How to Configure

### 1. Open Environment Settings

Click the **HDD icon** (⚙️) in the AI chat interface to open Environment Settings.

### 2. Configure Primary Provider

- **Provider / Model**: Select your primary LLM (e.g., `Anthropic (Claude 3.5 Sonnet)`)
- **API Key**: Enter your API key
- **Base URL**: (Optional) Custom endpoint

Click **Test Connection** to verify.

### 3. Configure Fallback Provider (Optional)

Scroll down to the **"Fallback Provider (Optional)"** section:

- **Provider / Model**: Select a different LLM (e.g., `OpenAI (GPT-4o)`)
- **API Key**: Enter the fallback provider's API key
- **Base URL**: (Optional) Custom endpoint

Click **Test Fallback** to verify.

### 4. Save Settings

Click **Save & Close**. Your configuration is saved to browser localStorage.

## Example Configuration

```
Primary: Anthropic (Claude 3.5 Sonnet)
API Key: sk-ant-api03-xxx

Fallback: OpenAI (GPT-4o)
API Key: sk-xxx
```

## How It Works

When you send a request:
1. AI Studio tries the **primary provider** first
2. If it fails with a retryable error (429 rate limit, 503 service unavailable, timeouts), it automatically tries the **fallback provider**
3. If the fallback succeeds, you get a response
4. If both fail, you see an error

### Circuit Breaker

The system includes a circuit breaker:
- After **3 consecutive failures**, the provider circuit "opens" (disabled for 5 minutes)
- This prevents hammering a failing provider
- After 5 minutes, the circuit automatically closes and retries

### Retryable vs Non-Retryable Errors

**Retryable** (will use fallback):
- 429 Too Many Requests (rate limit)
- 503 Service Unavailable
- 502 Bad Gateway
- 504 Gateway Timeout
- Network timeouts
- "quota exceeded" errors

**Non-retryable** (will NOT use fallback):
- 401 Unauthorized (bad API key)
- 403 Forbidden
- 400 Bad Request (malformed request)

## Best Practices

1. **Use different providers** for primary and fallback (e.g., Anthropic + OpenAI)
   - This protects against provider-specific outages
   - Different rate limit pools

2. **Test both connections** before saving
   - Ensures both API keys are valid
   - Confirms network connectivity

3. **Monitor costs** if using multiple providers
   - Fallback activates automatically on failures
   - Check your provider dashboards periodically

4. **Keep fallback optional**
   - Only configure if you need high availability
   - Single provider works fine for most use cases

## localStorage Schema

Configuration is stored in `localStorage.sop_ai_config`:

```json
{
  "brain_provider": "anthropic",
  "brain_model": "claude-3-5-sonnet-20241022",
  "brain_api_key": "sk-ant-api03-xxx",
  "brain_url": "",
  "fallback_provider": "chatgpt",
  "fallback_model": "gpt-4o",
  "fallback_api_key": "sk-xxx",
  "fallback_url": ""
}
```

## Technical Details

- Fallback timeout: **90 seconds** per provider
- Circuit breaker: Opens after **3 failures**, closes after **5 minutes**
- HTTP headers: `X-LLM-Fallback-Provider`, `X-LLM-Fallback-API-Key`, `X-LLM-Fallback-Model`, `X-LLM-Fallback-URL`
- Backend: Uses `FallbackGenerator` from `lib/sop/ai/generator/fallback.go`

## Troubleshooting

### Fallback not activating

- Check that both providers have valid API keys (Test Connection)
- Verify the error is retryable (check browser console)
- Circuit breaker may be open (wait 5 minutes)

### Both providers failing

- Check API key validity
- Check account quotas (rate limits, billing)
- Check network connectivity
- Review error messages in browser console

### Want to disable fallback

Set the **Fallback Provider** dropdown to **"None (No Fallback)"** and save.

## Related Documentation

- [FALLBACK.md](../../../lib/sop/ai/generator/FALLBACK.md) - FallbackGenerator API reference
- [AI_STUDIO.md](../AI_STUDIO.md) - AI Studio architecture
- [ai-studio-memory-advantage.md](../src/docs-site/docs/ai-studio-memory-advantage.md) - Memory architecture advantages
