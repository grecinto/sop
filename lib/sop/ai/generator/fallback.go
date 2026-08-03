package generator

import (
	"context"
	"fmt"
	log "log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai"
)

// FallbackGenerator implements multi-provider failover for sustained throughput and quality.
// GUARDRAIL: If one provider experiences outages or traffic spikes, automatically switch
// to an equivalent model on a different provider using the same NDJSON streaming format.
type FallbackGenerator struct {
	primary   ai.Generator
	fallbacks []ai.Generator
	mu        sync.RWMutex
	health    map[string]*providerHealth
}

type providerHealth struct {
	provider         string
	consecutiveFails int
	lastFailure      time.Time
	circuitOpen      bool
	openedAt         time.Time
}

const (
	maxConsecutiveFails   = 3
	circuitBreakerTimeout = 5 * time.Minute
	healthCheckInterval   = 30 * time.Second
)

// NewFallbackGenerator creates a generator with automatic provider failover.
// Example: NewFallbackGenerator(anthropic, []ai.Generator{openai, gemini})
func NewFallbackGenerator(primary ai.Generator, fallbacks []ai.Generator) *FallbackGenerator {
	fg := &FallbackGenerator{
		primary:   primary,
		fallbacks: fallbacks,
		health:    make(map[string]*providerHealth),
	}

	// Initialize health tracking
	if primary != nil {
		fg.health[primary.Name()] = &providerHealth{provider: primary.Name()}
	}
	for _, fb := range fallbacks {
		if fb != nil {
			fg.health[fb.Name()] = &providerHealth{provider: fb.Name()}
		}
	}

	return fg
}

func (fg *FallbackGenerator) Name() string {
	if fg.primary != nil {
		return fg.primary.Name() + "-with-fallback"
	}
	return "fallback-generator"
}

func (fg *FallbackGenerator) CarryoverCapability() ai.CarryoverCapability {
	// Try to get capability from primary if it supports the extended interface
	if fg.primary != nil {
		if ext, ok := fg.primary.(interface{ CarryoverCapability() ai.CarryoverCapability }); ok {
			return ext.CarryoverCapability()
		}
	}
	return ai.CarryoverCapability{SupportsCompact: true}
}

func (fg *FallbackGenerator) ProviderRuntimeCapability() ai.ProviderRuntimeCapability {
	if fg.primary != nil {
		if ext, ok := fg.primary.(ai.ProviderRuntimeCapabilityProvider); ok {
			return ext.ProviderRuntimeCapability()
		}
		capability := ai.ProviderRuntimeCapability{Provider: fg.primary.Name(), SupportsCompactCarryover: true}
		if ext, ok := fg.primary.(interface{ CarryoverCapability() ai.CarryoverCapability }); ok {
			carry := ext.CarryoverCapability()
			capability.Provider = carry.Provider
			capability.Model = carry.Model
			capability.SupportsCompactCarryover = carry.SupportsCompact
			capability.SupportsLiveCarryover = carry.SupportsLive
		}
		return capability
	}
	return ai.ProviderRuntimeCapability{SupportsCompactCarryover: true}
}

func (fg *FallbackGenerator) ReActLoop() ai.ReActLoop {
	// Try to get ReAct loop from primary if it supports the extended interface
	if fg.primary != nil {
		if ext, ok := fg.primary.(interface{ ReActLoop() ai.ReActLoop }); ok {
			return ext.ReActLoop()
		}
	}
	return nil
}

func (fg *FallbackGenerator) ReActTurnStrategy() ai.ReActTurnStrategy {
	// Try to get strategy from primary if it supports the extended interface
	if fg.primary != nil {
		if ext, ok := fg.primary.(interface{ ReActTurnStrategy() ai.ReActTurnStrategy }); ok {
			return ext.ReActTurnStrategy()
		}
	}
	return nil
}

// Generate attempts to generate using the primary provider, falling back to alternatives on failure.
func (fg *FallbackGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	// Try primary first if circuit is closed
	if fg.primary != nil && !fg.isCircuitOpen(fg.primary.Name()) {
		output, err := fg.tryGenerate(ctx, fg.primary, prompt, opts)
		if err == nil {
			fg.recordSuccess(fg.primary.Name())
			return output, nil
		}

		// Check if this is a retryable error
		if !fg.isRetryableError(err) {
			return ai.GenOutput{}, err
		}

		fg.recordFailure(fg.primary.Name(), err)
		log.Warn("Primary provider failed, attempting fallback",
			"provider", fg.primary.Name(),
			"error", err.Error(),
		)
	} else if fg.primary != nil {
		log.Warn("Primary provider circuit open, using fallback",
			"provider", fg.primary.Name(),
		)
	}

	// Try fallback providers
	for _, fallback := range fg.fallbacks {
		if fallback == nil || fg.isCircuitOpen(fallback.Name()) {
			continue
		}

		log.Info("Attempting fallback provider",
			"provider", fallback.Name(),
		)

		output, err := fg.tryGenerate(ctx, fallback, prompt, opts)
		if err == nil {
			fg.recordSuccess(fallback.Name())
			log.Info("Fallback provider succeeded",
				"provider", fallback.Name(),
			)
			return output, nil
		}

		if !fg.isRetryableError(err) {
			return ai.GenOutput{}, err
		}

		fg.recordFailure(fallback.Name(), err)
		log.Warn("Fallback provider failed",
			"provider", fallback.Name(),
			"error", err.Error(),
		)
	}

	return ai.GenOutput{}, fmt.Errorf("all providers failed (primary + %d fallbacks)", len(fg.fallbacks))
}

func (fg *FallbackGenerator) tryGenerate(ctx context.Context, gen ai.Generator, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	// Add timeout to prevent hanging on one provider
	genCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	return gen.Generate(genCtx, prompt, opts)
}

// isRetryableError determines if an error warrants trying another provider.
func (fg *FallbackGenerator) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Network and infrastructure errors - definitely retry
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "dial") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "504") {
		return true
	}

	// Rate limiting and capacity errors - retry with different provider
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "overload") ||
		strings.Contains(errStr, "capacity") {
		return true
	}

	// Server errors - retry
	if strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "internal server") {
		return true
	}

	// API availability errors
	if strings.Contains(errStr, "unavailable") ||
		strings.Contains(errStr, "maintenance") {
		return true
	}

	// Don't retry on authentication, validation, or client errors
	if strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "400") ||
		strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "missing api key") {
		return false
	}

	// Default: try fallback for unknown errors
	return true
}

func (fg *FallbackGenerator) isCircuitOpen(provider string) bool {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	health, exists := fg.health[provider]
	if !exists {
		return false
	}

	// Check if circuit should be reset
	if health.circuitOpen && time.Since(health.openedAt) > circuitBreakerTimeout {
		// Circuit breaker timeout elapsed, allow retry
		return false
	}

	return health.circuitOpen
}

func (fg *FallbackGenerator) recordSuccess(provider string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()

	health, exists := fg.health[provider]
	if !exists {
		return
	}

	// Reset failure counter and close circuit on success
	health.consecutiveFails = 0
	health.circuitOpen = false

	log.Debug("Provider health: success",
		"provider", provider,
	)
}

func (fg *FallbackGenerator) recordFailure(provider string, err error) {
	fg.mu.Lock()
	defer fg.mu.Unlock()

	health, exists := fg.health[provider]
	if !exists {
		return
	}

	health.consecutiveFails++
	health.lastFailure = time.Now()

	// Open circuit breaker if too many consecutive failures
	if health.consecutiveFails >= maxConsecutiveFails {
		if !health.circuitOpen {
			health.circuitOpen = true
			health.openedAt = time.Now()
			log.Warn("Provider circuit breaker opened due to consecutive failures",
				"provider", provider,
				"consecutive_fails", health.consecutiveFails,
				"timeout", circuitBreakerTimeout.String(),
			)
		}
	}

	log.Debug("Provider health: failure",
		"provider", provider,
		"consecutive_fails", health.consecutiveFails,
		"error", err.Error(),
	)
}

func (fg *FallbackGenerator) EstimateCost(inTokens, outTokens int) float64 {
	if fg.primary != nil {
		return fg.primary.EstimateCost(inTokens, outTokens)
	}
	return 0
}

func (fg *FallbackGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	if fg.primary != nil {
		return fg.primary.PrewarmCache(ctx, opts)
	}
	return nil
}

// GetProviderHealth returns the current health status of all providers.
func (fg *FallbackGenerator) GetProviderHealth() map[string]ProviderHealthStatus {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	status := make(map[string]ProviderHealthStatus)
	for name, health := range fg.health {
		status[name] = ProviderHealthStatus{
			Provider:         health.provider,
			ConsecutiveFails: health.consecutiveFails,
			LastFailure:      health.lastFailure,
			CircuitOpen:      health.circuitOpen,
			OpenedAt:         health.openedAt,
		}
	}
	return status
}

// ProviderHealthStatus represents the health status of a provider.
type ProviderHealthStatus struct {
	Provider         string
	ConsecutiveFails int
	LastFailure      time.Time
	CircuitOpen      bool
	OpenedAt         time.Time
}
