package agent

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/ai"
)

func TestEvaluateSessionCognitivePolicy_PausesForPendingClarification(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any), ClarificationState: &ai.ClarificationState{Status: "pending"}}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "continue the workflow", "phase:planning", 3, "planning", "tool_error", "clarification needed")
	if decision.Action != "pause_for_clarification" {
		t.Fatalf("expected pause_for_clarification action, got %q", decision.Action)
	}
	if decision.Reason == "" {
		t.Fatal("expected a non-empty reason")
	}
}

func TestEvaluateSessionCognitivePolicy_EscalatesRetryWhenTierIsHigh(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	payload.CognitiveState = &ai.SessionCognitiveState{EscalationTier: 3}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	decision := ag.evaluateSessionCognitivePolicy(ctx, "retry the same ask", "phase:execution", 2, "execution", "retry_error", "tier 3 escalation")
	if decision.Action != "escalate_retry" {
		t.Fatalf("expected escalate_retry action, got %q", decision.Action)
	}
}

func TestBuildSessionCognitiveState_UsesMemorySignals(t *testing.T) {
	ag := NewCopilotAgent(Config{}, nil, nil)
	ag.service = &Service{session: NewRunnerSession()}
	ag.service.session.MRU = []MRUItem{{Category: "ask_outcome", Context: "previous task failed", Source: MRUSourceAskOutcome, Scope: MRUScopeSession}}
	payload := &ai.SessionPayload{Variables: make(map[string]any)}
	ctx := context.WithValue(context.Background(), "session_payload", payload)

	state := ag.buildSessionCognitiveState(ctx, "continue the workflow", "phase:planning", 1, "planning")
	if len(state.MemorySignals) == 0 {
		t.Fatal("expected memory signals to be populated")
	}
	if state.CurrentActor != "phase:planning" {
		t.Fatalf("expected current actor to be preserved, got %q", state.CurrentActor)
	}
}
