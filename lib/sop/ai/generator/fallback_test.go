package generator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai"
)

type mockGenerator struct {
	name           string
	shouldFail     bool
	failureCount   int
	failsRemaining int
	callCount      int
}

func (m *mockGenerator) Name() string { return m.name }

func (m *mockGenerator) CarryoverCapability() ai.CarryoverCapability {
	return ai.CarryoverCapability{Provider: m.name, SupportsCompact: true}
}

func (m *mockGenerator) ReActLoop() ai.ReActLoop                 { return nil }
func (m *mockGenerator) ReActTurnStrategy() ai.ReActTurnStrategy { return nil }

func (m *mockGenerator) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.callCount++

	if m.failsRemaining > 0 {
		m.failsRemaining--
		m.failureCount++
		return ai.GenOutput{}, errors.New("503 service unavailable")
	}

	if m.shouldFail {
		m.failureCount++
		return ai.GenOutput{}, errors.New("503 service unavailable")
	}

	return ai.GenOutput{Text: "Success from " + m.name, TokensUsed: 10}, nil
}

func (m *mockGenerator) EstimateCost(inTokens, outTokens int) float64               { return 0 }
func (m *mockGenerator) PrewarmCache(ctx context.Context, opts ai.GenOptions) error { return nil }

// mockGeneratorWithAuthError always returns authentication errors
type mockGeneratorWithAuthError struct {
	name      string
	callCount int
}

func (m *mockGeneratorWithAuthError) Name() string { return m.name }

func (m *mockGeneratorWithAuthError) CarryoverCapability() ai.CarryoverCapability {
	return ai.CarryoverCapability{Provider: m.name, SupportsCompact: true}
}

func (m *mockGeneratorWithAuthError) ReActLoop() ai.ReActLoop                 { return nil }
func (m *mockGeneratorWithAuthError) ReActTurnStrategy() ai.ReActTurnStrategy { return nil }

func (m *mockGeneratorWithAuthError) Generate(ctx context.Context, prompt string, opts ai.GenOptions) (ai.GenOutput, error) {
	m.callCount++
	return ai.GenOutput{}, errors.New("401 unauthorized")
}

func (m *mockGeneratorWithAuthError) EstimateCost(inTokens, outTokens int) float64 { return 0 }
func (m *mockGeneratorWithAuthError) PrewarmCache(ctx context.Context, opts ai.GenOptions) error {
	return nil
}

func TestFallbackGenerator_PrimarySuccess(t *testing.T) {
	primary := &mockGenerator{name: "primary"}
	fallback := &mockGenerator{name: "fallback"}

	fg := NewFallbackGenerator(primary, []ai.Generator{fallback})

	output, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if output.Text != "Success from primary" {
		t.Errorf("expected primary response, got: %s", output.Text)
	}

	if primary.callCount != 1 {
		t.Errorf("expected primary to be called once, got %d", primary.callCount)
	}

	if fallback.callCount != 0 {
		t.Errorf("expected fallback not to be called, got %d calls", fallback.callCount)
	}
}

func TestFallbackGenerator_ProviderRuntimeCapability_PrefersPrimary(t *testing.T) {
	primary := &chatgpt{model: "gpt-4o", apiKey: "test-key"}
	fallback := &mockGenerator{name: "fallback"}
	fg := NewFallbackGenerator(primary, []ai.Generator{fallback})
	cap := fg.ProviderRuntimeCapability()
	if cap.Provider != "chatgpt" || cap.Model != "gpt-4o" {
		t.Fatalf("expected fallback runtime capability to mirror primary, got %+v", cap)
	}
	if cap.LoopMode != ai.ReActLoopModeOwned || !cap.SupportsNativeContinuation {
		t.Fatalf("expected fallback runtime capability to preserve primary continuation model, got %+v", cap)
	}
}

func TestFallbackGenerator_PrimaryFailsFallbackSucceeds(t *testing.T) {
	primary := &mockGenerator{name: "primary", shouldFail: true}
	fallback := &mockGenerator{name: "fallback"}

	fg := NewFallbackGenerator(primary, []ai.Generator{fallback})

	output, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
	if err != nil {
		t.Fatalf("expected success from fallback, got error: %v", err)
	}

	if output.Text != "Success from fallback" {
		t.Errorf("expected fallback response, got: %s", output.Text)
	}

	if primary.callCount != 1 {
		t.Errorf("expected primary to be called once, got %d", primary.callCount)
	}

	if fallback.callCount != 1 {
		t.Errorf("expected fallback to be called once, got %d", fallback.callCount)
	}
}

func TestFallbackGenerator_AllProvidersFail(t *testing.T) {
	primary := &mockGenerator{name: "primary", shouldFail: true}
	fallback1 := &mockGenerator{name: "fallback1", shouldFail: true}
	fallback2 := &mockGenerator{name: "fallback2", shouldFail: true}

	fg := NewFallbackGenerator(primary, []ai.Generator{fallback1, fallback2})

	_, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}

	if primary.callCount != 1 {
		t.Errorf("expected primary to be called once, got %d", primary.callCount)
	}

	if fallback1.callCount != 1 {
		t.Errorf("expected fallback1 to be called once, got %d", fallback1.callCount)
	}

	if fallback2.callCount != 1 {
		t.Errorf("expected fallback2 to be called once, got %d", fallback2.callCount)
	}
}

func TestFallbackGenerator_CircuitBreaker(t *testing.T) {
	primary := &mockGenerator{name: "primary", failsRemaining: 3}
	fallback := &mockGenerator{name: "fallback"}

	fg := NewFallbackGenerator(primary, []ai.Generator{fallback})

	// First 3 calls should fail primary and succeed on fallback
	for i := 0; i < 3; i++ {
		output, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
		if err != nil {
			t.Fatalf("call %d: expected success from fallback, got error: %v", i+1, err)
		}
		if output.Text != "Success from fallback" {
			t.Errorf("call %d: expected fallback response, got: %s", i+1, output.Text)
		}
	}

	// Circuit should now be open for primary
	health := fg.GetProviderHealth()
	if !health["primary"].CircuitOpen {
		t.Error("expected circuit to be open after 3 consecutive failures")
	}

	// Next call should skip primary (circuit open) and go straight to fallback
	output, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
	if err != nil {
		t.Fatalf("expected success from fallback, got error: %v", err)
	}

	if output.Text != "Success from fallback" {
		t.Errorf("expected fallback response, got: %s", output.Text)
	}

	// Primary should not have been called on the 4th attempt (circuit open)
	if primary.callCount != 3 {
		t.Errorf("expected primary to be called only 3 times (before circuit opened), got %d", primary.callCount)
	}

	if fallback.callCount != 4 {
		t.Errorf("expected fallback to be called 4 times, got %d", fallback.callCount)
	}
}

func TestFallbackGenerator_NonRetryableError(t *testing.T) {
	// Create a special mock that always returns non-retryable error
	primary := &mockGeneratorWithAuthError{name: "primary"}
	fallback := &mockGenerator{name: "fallback"}

	fg := NewFallbackGenerator(primary, []ai.Generator{fallback})

	_, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
	if err == nil {
		t.Fatal("expected error to be returned")
	}

	// Fallback should not be called for non-retryable errors
	if fallback.callCount != 0 {
		t.Errorf("expected fallback not to be called for non-retryable error, got %d calls", fallback.callCount)
	}
}

func TestFallbackGenerator_HealthRecovery(t *testing.T) {
	primary := &mockGenerator{name: "primary", failsRemaining: 2}
	fallback := &mockGenerator{name: "fallback"}

	fg := NewFallbackGenerator(primary, []ai.Generator{fallback})

	// First 2 calls fail primary
	for i := 0; i < 2; i++ {
		_, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
		if err != nil {
			t.Fatalf("call %d: expected success from fallback, got error: %v", i+1, err)
		}
	}

	// Check health before recovery
	health := fg.GetProviderHealth()
	if health["primary"].ConsecutiveFails != 2 {
		t.Errorf("expected 2 consecutive fails, got %d", health["primary"].ConsecutiveFails)
	}

	// Third call should succeed on primary (no more failures remaining)
	output, err := fg.Generate(context.Background(), "test", ai.GenOptions{})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if output.Text != "Success from primary" {
		t.Errorf("expected primary response after recovery, got: %s", output.Text)
	}

	// Check health after recovery
	health = fg.GetProviderHealth()
	if health["primary"].ConsecutiveFails != 0 {
		t.Errorf("expected consecutive fails to reset to 0 after success, got %d", health["primary"].ConsecutiveFails)
	}

	if health["primary"].CircuitOpen {
		t.Error("expected circuit to be closed after successful call")
	}
}

func TestFallbackGenerator_IsRetryableError(t *testing.T) {
	fg := NewFallbackGenerator(nil, nil)

	tests := []struct {
		err       error
		retryable bool
	}{
		{errors.New("503 service unavailable"), true},
		{errors.New("429 rate limit exceeded"), true},
		{errors.New("timeout after 30s"), true},
		{errors.New("connection refused"), true},
		{errors.New("network error"), true},
		{errors.New("500 internal server error"), true},
		{errors.New("quota exceeded"), true},
		{errors.New("401 unauthorized"), false},
		{errors.New("400 bad request"), false},
		{errors.New("403 forbidden"), false},
		{errors.New("missing api key"), false},
		{errors.New("invalid request"), false},
	}

	for _, tt := range tests {
		got := fg.isRetryableError(tt.err)
		if got != tt.retryable {
			t.Errorf("isRetryableError(%q) = %v, want %v", tt.err.Error(), got, tt.retryable)
		}
	}
}

func TestFallbackGenerator_CircuitBreakerTimeout(t *testing.T) {
	// This test would normally take 5 minutes, so we skip it in normal test runs
	if testing.Short() {
		t.Skip("skipping circuit breaker timeout test in short mode")
	}

	primary := &mockGenerator{name: "primary", failsRemaining: 3}
	fallback := &mockGenerator{name: "fallback"}

	fg := NewFallbackGenerator(primary, []ai.Generator{fallback})

	// Trigger circuit breaker
	for i := 0; i < 3; i++ {
		fg.Generate(context.Background(), "test", ai.GenOptions{})
	}

	// Verify circuit is open
	health := fg.GetProviderHealth()
	if !health["primary"].CircuitOpen {
		t.Fatal("expected circuit to be open")
	}

	// Manually set the opened time to simulate timeout
	fg.mu.Lock()
	fg.health["primary"].openedAt = time.Now().Add(-6 * time.Minute)
	fg.mu.Unlock()

	// Circuit should now be considered closed (timeout elapsed)
	if fg.isCircuitOpen("primary") {
		t.Error("expected circuit to be closed after timeout")
	}
}
