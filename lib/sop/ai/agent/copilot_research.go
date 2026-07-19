package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
)

// buildResearchWorkflowGuidance generates research phase instructions for complex tasks
// Only returns guidance if research sources (KB, LTM, playbooks) are actually available
func (a *CopilotAgent) buildResearchWorkflowGuidance(ctx context.Context, query string, taskCtx TaskContextClassification) string {
	// Check what research sources are available
	hasKB := a.hasAvailableKnowledgeBases(ctx)
	hasLTM := a.hasAvailableLTM(ctx)
	hasPlaybooks := len(taskCtx.AutomationFamilies) > 0 || taskCtx.Domain != ""

	// If no research sources available, skip research guidance
	if !hasKB && !hasLTM && !hasPlaybooks {
		return ""
	}

	// Build research workflow based on available sources and task type
	var sources []string
	var workflow []string

	if hasKB {
		sources = append(sources, "Knowledge Bases (Spaces)")
		workflow = append(workflow, "Search relevant Knowledge Bases using search_space for domain expertise, conventions, and best practices")
	}

	if hasLTM {
		sources = append(sources, "Long-Term Memory")
		workflow = append(workflow, "Consult your Long-Term Memory for similar past tasks, learned patterns, and project-specific context")
	}

	if hasPlaybooks {
		sources = append(sources, "Playbooks")
		workflow = append(workflow, "Review playbooks and recipes for systematic approaches to this type of task")
	}

	// Build guidance based on task complexity indicators
	researchReason := a.inferResearchReason(query, taskCtx)

	guidance := fmt.Sprintf(`Research Phase (Advanced Task):
This task has been classified as complex/advanced and would benefit from research before execution.

Available Research Sources: %s

Research Workflow:
%s

Why Research is Recommended:
%s

After Research:
- Synthesize findings into a systematic approach
- Execute with project-specific conventions and best practices
- Validate against learned constraints and patterns

Skip research ONLY if you have high confidence from recent context.`,
		strings.Join(sources, ", "),
		"- "+strings.Join(workflow, "\n- "),
		researchReason,
	)

	return guidance
}

// hasAvailableKnowledgeBases checks if any KBs/Spaces are configured
func (a *CopilotAgent) hasAvailableKnowledgeBases(ctx context.Context) bool {
	if a.systemDB == nil {
		return false
	}

	// Check if there's a selected space
	if payload := ai.GetSessionPayload(ctx); payload != nil {
		if payload.ActiveDomain != "" {
			return true
		}
	}

	// Check if any spaces exist
	if spaces, err := a.systemDB.GetDomains(ctx); err == nil && len(spaces) > 0 {
		return true
	}

	return false
}

// hasAvailableLTM checks if LTM is configured and accessible
func (a *CopilotAgent) hasAvailableLTM(ctx context.Context) bool {
	if a.systemDB == nil {
		return false
	}
	if a.service == nil || a.service.Domain() == nil || a.service.Domain().Embedder() == nil {
		return false
	}
	if a.Memory.AgentID == "" {
		return false
	}
	return true
}

// inferResearchReason explains why research was recommended based on the query
func (a *CopilotAgent) inferResearchReason(query string, taskCtx TaskContextClassification) string {
	queryLower := strings.ToLower(query)

	// Multi-step problem-solving
	if strings.Contains(queryLower, "fix") || strings.Contains(queryLower, "troubleshoot") ||
		strings.Contains(queryLower, "debug") || strings.Contains(queryLower, "resolve") {
		return "Complex problem-solving detected. Research can provide systematic debugging approaches and known solutions."
	}

	// Setup/deployment
	if strings.Contains(queryLower, "setup") || strings.Contains(queryLower, "deploy") ||
		strings.Contains(queryLower, "configure") || strings.Contains(queryLower, "install") {
		return "Setup/deployment task detected. Research can uncover project-specific conventions, dependencies, and deployment runbooks."
	}

	// Investigation
	if strings.Contains(queryLower, "why") || strings.Contains(queryLower, "what's wrong") ||
		strings.Contains(queryLower, "diagnose") || strings.Contains(queryLower, "analyze") {
		return "Investigation query detected. Research can provide diagnostic frameworks and domain knowledge."
	}

	// How-to questions
	if strings.Contains(queryLower, "how do i") || strings.Contains(queryLower, "how to") ||
		strings.Contains(queryLower, "what's the best way") {
		return "How-to question detected. Research can reveal established patterns and recommended approaches."
	}

	// Test failures
	if strings.Contains(queryLower, "test") && (strings.Contains(queryLower, "fail") || strings.Contains(queryLower, "error")) {
		return "Test failure detected. Research can provide test debugging strategies and common failure patterns."
	}

	// Git workflows
	if strings.Contains(queryLower, "merge conflict") || strings.Contains(queryLower, "rebase") ||
		strings.Contains(queryLower, "cherry-pick") {
		return "Complex git workflow detected. Research can provide conflict resolution strategies and workflow best practices."
	}

	// Performance
	if strings.Contains(queryLower, "slow") || strings.Contains(queryLower, "optimization") ||
		strings.Contains(queryLower, "performance") {
		return "Performance issue detected. Research can provide profiling strategies and optimization patterns."
	}

	// Database operations
	if strings.Contains(queryLower, "migrate") || strings.Contains(queryLower, "schema change") {
		return "Database migration detected. Research can provide migration strategies and safety guardrails."
	}

	// CI/CD
	if strings.Contains(queryLower, "pipeline") || strings.Contains(queryLower, "ci") ||
		(strings.Contains(queryLower, "build") && strings.Contains(queryLower, "fail")) {
		return "CI/CD issue detected. Research can provide pipeline debugging approaches and build configuration patterns."
	}

	// Default for automation families
	if len(taskCtx.AutomationFamilies) > 0 {
		return fmt.Sprintf("Advanced automation task involving: %s. Research can provide domain-specific conventions and best practices.",
			strings.Join(taskCtx.AutomationFamilies, ", "))
	}

	return "Complex multi-step task detected. Research can provide systematic approaches and reduce trial-and-error."
}
