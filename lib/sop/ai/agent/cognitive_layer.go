package agent

import (
	"context"
	"strings"

	"github.com/sharedcode/sop/ai"
)

type sessionCognitiveDecision struct {
	Action string
	Reason string
}

func (a *CopilotAgent) buildSessionCognitiveState(ctx context.Context, query string, actor string, turn int, phase string) *ai.SessionCognitiveState {
	payload := ai.GetSessionPayload(ctx)
	if payload == nil {
		return nil
	}

	state := payload.CognitiveState
	if state == nil {
		state = &ai.SessionCognitiveState{}
	}

	state.CurrentTurn = turn
	state.CurrentActor = strings.TrimSpace(actor)
	state.CurrentPhase = strings.TrimSpace(phase)
	state.CurrentQuery = strings.TrimSpace(query)
	if payload.ClarificationState != nil {
		state.PendingClarification = strings.EqualFold(strings.TrimSpace(payload.ClarificationState.Status), "pending") || strings.EqualFold(strings.TrimSpace(payload.ClarificationState.Status), "clarifying")
		state.ClarificationStatus = strings.TrimSpace(payload.ClarificationState.Status)
	} else {
		state.PendingClarification = false
		state.ClarificationStatus = ""
	}
	state.MemorySignals = a.collectSessionMemorySignals(ctx)
	payload.CognitiveState = state
	return state
}

func (a *CopilotAgent) collectSessionMemorySignals(ctx context.Context) []string {
	var signals []string
	payload := ai.GetSessionPayload(ctx)
	if payload == nil || a.service == nil || a.service.session == nil {
		return signals
	}
	for _, item := range a.service.session.MRU {
		if strings.TrimSpace(item.Context) == "" {
			continue
		}
		signals = append(signals, item.Context)
	}
	if payload.CognitiveState != nil && len(payload.CognitiveState.MemorySignals) > 0 {
		return append(signals, payload.CognitiveState.MemorySignals...)
	}
	return signals
}

func (a *CopilotAgent) evaluateSessionCognitivePolicy(ctx context.Context, query string, actor string, turn int, phase string, outcome string, terminalReason string) sessionCognitiveDecision {
	state := a.buildSessionCognitiveState(ctx, query, actor, turn, phase)
	if state == nil {
		return sessionCognitiveDecision{Action: "continue", Reason: "no session payload"}
	}
	state.LastToolOutcome = strings.TrimSpace(outcome)
	state.TerminalReason = strings.TrimSpace(terminalReason)

	if state.PendingClarification {
		state.QueuePaused = true
		state.LastDecision = "pause_for_clarification"
		state.LastDecisionReason = "pending clarification should halt further queue progress"
		return sessionCognitiveDecision{Action: "pause_for_clarification", Reason: state.LastDecisionReason}
	}
	if state.EscalationTier >= 3 || state.RetryTier >= 3 {
		state.LastDecision = "escalate_retry"
		state.LastDecisionReason = "retry policy reached escalation tier"
		return sessionCognitiveDecision{Action: "escalate_retry", Reason: state.LastDecisionReason}
	}
	if strings.Contains(strings.ToLower(terminalReason), "hard stop") || strings.Contains(strings.ToLower(terminalReason), "terminal") {
		state.LastDecision = "stop"
		state.LastDecisionReason = "terminal outcome was observed"
		return sessionCognitiveDecision{Action: "stop", Reason: state.LastDecisionReason}
	}

	state.LastDecision = "continue"
	state.LastDecisionReason = "no blocking clarification or escalation state"
	return sessionCognitiveDecision{Action: "continue", Reason: state.LastDecisionReason}
}
