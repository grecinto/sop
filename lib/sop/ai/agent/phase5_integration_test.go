package agent

import (
	"testing"
)

func TestNewCopilotAgent_EnablesPhase5InProductionMode(t *testing.T) {
	agent := NewCopilotAgent(Config{ProductionMode: true}, nil, nil)

	if !agent.enablePhase5 {
		t.Fatal("expected Phase 5 to be enabled in production mode")
	}
	if agent.retryStrategy == nil {
		t.Fatal("expected default retry strategy to be initialized")
	}
	if agent.circuitBreaker == nil {
		t.Fatal("expected default circuit breaker to be initialized")
	}
	if agent.queueMetrics == nil {
		t.Fatal("expected default queue metrics to be initialized")
	}
}

func TestNewCopilotAgent_DisablesPhase5WhenProductionModeIsOff(t *testing.T) {
	agent := NewCopilotAgent(Config{}, nil, nil)

	if agent.enablePhase5 {
		t.Fatal("expected Phase 5 to remain disabled when production mode is off")
	}
}
