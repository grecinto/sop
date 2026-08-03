package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/teampatterns"
)

type ContinueDelegationReference struct {
	Kind     string
	Source   string
	Title    string
	Content  string
	Metadata map[string]any
}

type ContinueDelegationGuidance struct {
	TeamPractices        []string
	SuccessCriteria      []string
	Constraints          []string
	ImplementationNotes  []string
	ExpectedResultFormat string
	ReferenceContext     []ContinueDelegationReference
	Metadata             map[string]any
}

func (a *CopilotAgent) BuildContinueDelegationGuidance(ctx context.Context, query, requestedPhase string, variables map[string]any) (ContinueDelegationGuidance, error) {
	if !shouldEnrichContinueGuidance(strings.TrimSpace(requestedPhase), variables) {
		return ContinueDelegationGuidance{}, nil
	}
	if a == nil || a.systemDB == nil {
		return ContinueDelegationGuidance{}, nil
	}
	ltm, err := a.getOrCreateLTM(ctx)
	if err != nil {
		return ContinueDelegationGuidance{}, nil
	}
	patterns, err := teampatterns.SearchTeamPatterns(ctx, ltm, strings.TrimSpace(query), "", 3)
	if err != nil || len(patterns) == 0 {
		return ContinueDelegationGuidance{}, nil
	}
	guidance := ContinueDelegationGuidance{
		ExpectedResultFormat: "structured_execution_result",
		Metadata: map[string]any{
			"source":        "ltm_team_patterns",
			"pattern_count": len(patterns),
		},
	}
	for _, pattern := range patterns {
		if pattern == nil {
			continue
		}
		if text := strings.TrimSpace(pattern.Convention); text != "" {
			guidance.TeamPractices = append(guidance.TeamPractices, text)
		}
		if text := strings.TrimSpace(pattern.AntiPattern); text != "" {
			guidance.Constraints = append(guidance.Constraints, fmt.Sprintf("Avoid: %s", text))
		}
		if text := strings.TrimSpace(pattern.Reasoning); text != "" {
			guidance.ImplementationNotes = append(guidance.ImplementationNotes, text)
		}
		if text := strings.TrimSpace(pattern.WhenToUse); text != "" {
			guidance.SuccessCriteria = append(guidance.SuccessCriteria, fmt.Sprintf("Apply when: %s", text))
		}
		guidance.ReferenceContext = append(guidance.ReferenceContext, ContinueDelegationReference{
			Kind:    "ltm_team_pattern",
			Source:  pattern.BuildCategoryPath(),
			Title:   strings.TrimSpace(pattern.PatternName),
			Content: buildContinuePatternReference(pattern),
			Metadata: map[string]any{
				"pattern_type": pattern.PatternType,
				"scope_type":   pattern.ScopeType,
				"scope_id":     pattern.ScopeID,
			},
		})
	}
	return guidance, nil
}

func shouldEnrichContinueGuidance(requestedPhase string, variables map[string]any) bool {
	phase := strings.ToLower(strings.TrimSpace(requestedPhase))
	if phase == "planning" || phase == "execution" {
		if taskType, _ := variables["coding_task_type"].(string); strings.EqualFold(strings.TrimSpace(taskType), "implementation") || strings.TrimSpace(taskType) == "" {
			return true
		}
		return false
	}
	return false
}

func buildContinuePatternReference(pattern *teampatterns.TeamPattern) string {
	if pattern == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if text := strings.TrimSpace(pattern.Convention); text != "" {
		parts = append(parts, "Convention: "+text)
	}
	if text := strings.TrimSpace(pattern.AntiPattern); text != "" {
		parts = append(parts, "Anti-pattern: "+text)
	}
	if text := strings.TrimSpace(pattern.Reasoning); text != "" {
		parts = append(parts, "Reasoning: "+text)
	}
	if text := strings.TrimSpace(pattern.Example); text != "" {
		parts = append(parts, "Example: "+text)
	}
	return strings.Join(parts, "\n")
}
