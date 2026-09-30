# Provider Fallback

SOP can construct an ordered provider chain for generation. The configured primary is attempted first, followed by available fallback providers. In daemon Ask flows, provider-keyed credentials can be used to build the chain; legacy primary/fallback settings remain supported for compatibility.

## Configuration

Configure providers and credentials using the AI settings/setup flow or the deployment's daemon configuration. Keep provider secrets out of source control and logs. In local daemon mode, the UI encrypts provider keys through the daemon and stores opaque encrypted values separately from non-secret settings; do not depend on the old plaintext `localStorage.sop_ai_config` schema. See [AI Studio](AI_STUDIO.md) for local secret handling.

The provider chain is ordered. An explicit provider/model request takes precedence where the runtime supports it; otherwise, the daemon resolves the configured eligible providers. See [Retry Orchestration State](RETRY_ORCHESTRATION_STATE.md) for the broader Ask retry policy and [LLM Enhancement Plan](LLM_ENHANCEMENT_PLAN.md) for provider work that remains proposed.

## Runtime Behavior

`FallbackGenerator` ([source](../lib/sop/ai/generator/fallback.go)) uses these rules:

- It attempts the primary provider, then fallback providers in configured order.
- Each provider attempt has a 90-second timeout derived from the caller context.
- Network errors, timeouts, connection failures, HTTP 5xx errors, rate limits, quota/capacity errors, and unavailable/maintenance errors are retryable.
- Authentication and request-validation errors such as 400, 401, 403, invalid requests, or a missing API key are not retried on another provider.
- After three consecutive retryable failures, a provider's circuit opens for five minutes. A successful call resets its failure count and closes the circuit.
- Cancellation and deadlines from the caller context are preserved.

The retry classification is based on the provider error text. Treat it as a bounded resilience mechanism, not a guarantee that every provider error is classified correctly.

## Validation and Troubleshooting

Test each configured provider independently before relying on fallback. Then test a transient failure and confirm the next configured provider succeeds. Do not expect a bad credential or malformed request to fall through automatically; correct the provider configuration first.

Focused tests for provider ordering, retry classification, circuit state, and Ask fallback are in the generator and daemon Ask packages. From the repository root, run:

```sh
go test ./lib/sop/ai/generator/...
go test ./lib/sop/cmd/sop-daemon
```
