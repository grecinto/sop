package ideworker

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/teampatterns"
	"github.com/sharedcode/sop/ai/memory"
)

func ShouldEnrichContinueGuidance(requestedPhase string, variables map[string]any) bool {
	phase := strings.ToLower(strings.TrimSpace(requestedPhase))
	if phase == "planning" || phase == "execution" {
		if taskType, _ := variables["coding_task_type"].(string); strings.EqualFold(strings.TrimSpace(taskType), "implementation") || strings.TrimSpace(taskType) == "" {
			return true
		}
		return false
	}
	return false
}

func BuildDelegationGuidance(ctx context.Context, ltm *memory.KnowledgeBase[map[string]any], query, requestedPhase string, variables map[string]any) (DelegationGuidance, error) {
	if !ShouldEnrichContinueGuidance(strings.TrimSpace(requestedPhase), variables) {
		return DelegationGuidance{}, nil
	}
	if ltm == nil {
		return DelegationGuidance{}, nil
	}
	patterns, err := teampatterns.SearchTeamPatterns(ctx, ltm, strings.TrimSpace(query), "", 3)
	if err != nil || len(patterns) == 0 {
		return DelegationGuidance{}, nil
	}
	guidance := DelegationGuidance{
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
		guidance.ReferenceContext = append(guidance.ReferenceContext, DelegationReference{
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
