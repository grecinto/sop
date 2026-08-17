package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
)

const phaseTransactionHandoffMemoryPrefix = "phase-handoff:"

type phaseTransactionHandoff struct {
	Version            int                               `json:"version"`
	CapturedAt         int64                             `json:"captured_at"`
	OriginalQuery      string                            `json:"original_query,omitempty"`
	SourcePhase        string                            `json:"source_phase,omitempty"`
	TargetPhase        string                            `json:"target_phase,omitempty"`
	Reason             string                            `json:"reason,omitempty"`
	ControllerGuidance string                            `json:"controller_guidance,omitempty"`
	OutcomeFacts       []string                          `json:"outcome_facts,omitempty"`
	ToolCalls          []phaseTransactionHandoffToolCall `json:"tool_calls,omitempty"`
	FinalText          string                            `json:"final_text,omitempty"`
}

type phaseTransactionHandoffToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

func (a *CopilotAgent) persistPhaseTransactionHandoff(query string, classification *AskTaskContext, decision phaseTransactionDecision, toolCalls []ai.ToolCall, outcomeFacts []string, finalText string) {
	if a == nil || classification == nil || !decision.EnqueueNextPhase || strings.TrimSpace(decision.NextPhase) == "" {
		return
	}
	if a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return
	}
	handoff := phaseTransactionHandoff{
		Version:            1,
		CapturedAt:         time.Now().UnixMilli(),
		OriginalQuery:      firstNonEmpty(strings.TrimSpace(classification.OriginalQuery), strings.TrimSpace(query)),
		SourcePhase:        strings.TrimSpace(classification.Phase),
		TargetPhase:        strings.TrimSpace(decision.NextPhase),
		Reason:             strings.TrimSpace(decision.Reason),
		ControllerGuidance: strings.TrimSpace(buildPhaseTransactionContinuationGuidance(decision)),
		OutcomeFacts:       append([]string(nil), outcomeFacts...),
		ToolCalls:          buildPhaseTransactionHandoffToolCalls(toolCalls),
		FinalText:          strings.TrimSpace(finalText),
	}
	continuationSummary := renderPhaseTransactionHandoffText(handoff)
	bytes, err := json.Marshal(handoff)
	if err != nil {
		return
	}
	_ = a.service.session.Memory.SaveWorkflowMemory(phaseTransactionHandoffMemoryPrefix+handoff.TargetPhase, map[string]string{
		"handoff_json":         string(bytes),
		"continuation_summary": continuationSummary,
		"target_phase":         handoff.TargetPhase,
		"original_query":       handoff.OriginalQuery,
	})
}

func buildPhaseTransactionHandoffToolCalls(toolCalls []ai.ToolCall) []phaseTransactionHandoffToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]phaseTransactionHandoffToolCall, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		out = append(out, phaseTransactionHandoffToolCall{
			Name: strings.TrimSpace(toolCall.Name),
			Args: clonePhaseTransactionArgs(toolCall.Args),
		})
	}
	return out
}

func clonePhaseTransactionArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}

func (a *CopilotAgent) loadPhaseTransactionHandoff(targetPhase string) (phaseTransactionHandoff, bool) {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return phaseTransactionHandoff{}, false
	}
	values, err := a.service.session.Memory.LoadWorkflowMemory(phaseTransactionHandoffMemoryPrefix + strings.TrimSpace(targetPhase))
	if err != nil {
		return phaseTransactionHandoff{}, false
	}
	raw := strings.TrimSpace(values["handoff_json"])
	if raw == "" {
		return phaseTransactionHandoff{}, false
	}
	var handoff phaseTransactionHandoff
	if err := json.Unmarshal([]byte(raw), &handoff); err != nil {
		return phaseTransactionHandoff{}, false
	}
	return handoff, true
}

func (a *CopilotAgent) renderPhaseTransactionHandoff(classification *AskTaskContext, query string) string {
	if a == nil || classification == nil {
		return ""
	}
	targetPhase := strings.TrimSpace(classification.Phase)
	if targetPhase == "" {
		return ""
	}
	handoff, ok := a.loadPhaseTransactionHandoff(targetPhase)
	if !ok {
		return ""
	}
	expectedQuery := firstNonEmpty(strings.TrimSpace(classification.OriginalQuery), strings.TrimSpace(query))
	if expectedQuery != "" && handoff.OriginalQuery != "" && !strings.EqualFold(strings.TrimSpace(handoff.OriginalQuery), expectedQuery) {
		return ""
	}
	return renderPhaseTransactionHandoffText(handoff)
}

func renderPhaseTransactionHandoffText(handoff phaseTransactionHandoff) string {
	lines := []string{"=== Phase Transaction Handoff ==="}
	if handoff.OriginalQuery != "" {
		lines = append(lines, "Original request: "+handoff.OriginalQuery)
	}
	lines = append(lines, fmt.Sprintf("Phase transition: %s -> %s", firstNonEmpty(strings.TrimSpace(handoff.SourcePhase), "unknown"), firstNonEmpty(strings.TrimSpace(handoff.TargetPhase), "unknown")))
	if handoff.Reason != "" {
		lines = append(lines, "Reason: "+handoff.Reason)
	}
	if handoff.ControllerGuidance != "" {
		lines = append(lines, "Controller guidance: "+handoff.ControllerGuidance)
	}
	if len(handoff.ToolCalls) > 0 {
		lines = append(lines, "Observed tool calls:")
		for _, toolCall := range handoff.ToolCalls {
			line := "- " + toolCall.Name
			if len(toolCall.Args) > 0 {
				if raw, err := json.Marshal(toolCall.Args); err == nil {
					line += " " + string(raw)
				}
			}
			lines = append(lines, line)
		}
	}
	if len(handoff.OutcomeFacts) > 0 {
		lines = append(lines, "Grounded outcome facts:")
		for _, fact := range handoff.OutcomeFacts {
			if strings.TrimSpace(fact) == "" {
				continue
			}
			lines = append(lines, "- "+fact)
		}
	}
	if handoff.FinalText != "" {
		lines = append(lines, "Prior-turn analysis:")
		lines = append(lines, handoff.FinalText)
	}
	return strings.Join(lines, "\n")
}
