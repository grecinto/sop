package agent

import (
	"context"
	"testing"
)

// EnableTestModeForTest enables test mode and automatically disables it when the test completes.
// This is the recommended way to enable test mode in tests.
//
// Usage:
//
//	func TestSomething(t *testing.T) {
//	    EnableTestModeForTest(t)
//	    // test code that needs fallbacks...
//	}
func EnableTestModeForTest(t *testing.T) {
	EnableTestMode()
	t.Cleanup(DisableTestMode)
}

// TestContextWithTestMode enables test mode at the package level.
// Use this for tests that don't go through the full agent pipeline.
// WARNING: Remember to call DisableTestMode() in a defer or t.Cleanup()
//
// Usage:
//
//	ctx := TestContextWithTestMode(context.Background())
//	result := functionThatMayNeedFallbacks(ctx)
func TestContextWithTestMode(ctx context.Context) context.Context {
	EnableTestMode()
	return ctx
}

// TestConfig creates a minimal test configuration.
// Note: Call EnableTestModeForTest(t) separately if test fallbacks are needed.
func TestConfig(t *testing.T) *Config {
	return &Config{
		ID:          "test-agent",
		Name:        "Test Agent",
		Description: "Test configuration",
	}
}
