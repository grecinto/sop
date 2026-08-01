package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
)

type ownedLoopRetryState struct {
	kernel             *ai.RetryCoordinator
	policy             ai.RetryPolicySurface
	failureCounts      map[string]int
	escalationState    map[string]any
	pendingRepair      *ai.RepairPlan
	learnings          []ai.RetryLearning
	learningContext    ai.RetryLearningContext
	allowedTurns       int
	maxAllowedTurns    int
	lastFailures       map[string]ownedLoopFailureContext
	progressExtensions int
	lastRecommendation *ai.RetryLearningRecommendation
}

type ownedLoopFailureContext struct {
	err  error
	plan ai.RepairPlan
}

func newOwnedLoopRetryState(baseTurns int, query string, carryover *ai.CarryoverState) *ownedLoopRetryState {
	return newOwnedLoopRetryStateWithPolicy(baseTurns, query, carryover, nil)
}

func newOwnedLoopRetryStateWithPolicy(baseTurns int, query string, carryover *ai.CarryoverState, policy ai.RetryPolicySurface) *ownedLoopRetryState {
	var learnings []ai.RetryLearning
	if carryover != nil && len(carryover.RetryLearnings) > 0 {
		learnings = append([]ai.RetryLearning(nil), carryover.RetryLearnings...)
	}
	kernel := ai.NewRetryCoordinator()
	if policy == nil {
		policy = ai.NewDefaultRetryPolicySurfaceWithCoordinator(kernel)
	}
	budget := policy.InitialRetryBudget(ai.RetryBudgetInput{BaseTurns: baseTurns, Query: query, Learnings: learnings})
	allowed := budget.SoftLimit
	maxAllowed := budget.HardLimit
	if allowed <= 0 {
		allowed = baseTurns
	}
	if maxAllowed < allowed {
		maxAllowed = allowed
	}
	return &ownedLoopRetryState{
		kernel:          kernel,
		policy:          policy,
		failureCounts:   make(map[string]int),
		learnings:       learnings,
		allowedTurns:    allowed,
		maxAllowedTurns: maxAllowed,
		lastFailures:    make(map[string]ownedLoopFailureContext),
	}
}

func (s *ownedLoopRetryState) configureLearningContext(ctx ai.RetryLearningContext) {
	s.learningContext = ctx
}

func (s *ownedLoopRetryState) resetTool(toolName string) {
	if strings.TrimSpace(toolName) == "" {
		return
	}
	s.failureCounts[toolName] = 0
}

func (s *ownedLoopRetryState) shouldDeferTool(toolName string) (*ai.RepairPlan, bool) {
	if s == nil || s.pendingRepair == nil {
		return nil, false
	}
	if s.policy.AllowsTool(*s.pendingRepair, toolName) {
		return s.pendingRepair, false
	}
	return s.pendingRepair, true
}

func (s *ownedLoopRetryState) reminderResult(attemptedTool string) ai.ReActToolResult {
	if s == nil || s.pendingRepair == nil {
		return ai.ReActToolResult{}
	}
	return ai.ReActToolResult{
		Name:   s.pendingRepair.ToolName,
		Result: s.policy.FormatRepairDirective(*s.pendingRepair, attemptedTool),
	}
}

func (s *ownedLoopRetryState) isRecoverableFailure(err error) bool {
	if s == nil || err == nil {
		return false
	}
	if s.policy == nil {
		return s.kernel.IsRecoverableToolError(err)
	}
	return s.policy.IsRecoverable(err)
}

func (s *ownedLoopRetryState) completeSuccessfulTool(toolName string) {
	if s == nil {
		return
	}
	if ctx, ok := s.lastFailures[toolName]; ok {
		s.learnings = s.kernel.RecordOutcome(s.learnings, toolName, ctx.err, ctx.plan, true, s.learningContext)
		delete(s.lastFailures, toolName)
	}
	if s.pendingRepair == nil {
		s.resetTool(toolName)
		return
	}
	if s.pendingRepair.Strategy == ai.RepairStrategyResearchFirst && toolName != s.pendingRepair.ToolName {
		s.pendingRepair = nil
		return
	}
	if toolName == s.pendingRepair.ToolName {
		s.resetTool(toolName)
		s.pendingRepair = nil
	}
}

func (s *ownedLoopRetryState) handleRecoverableFailure(toolCall ai.ToolCall, err error, toolResults []ai.ReActToolResult) (ai.ReActToolResult, bool) {
	currentFailureCount := s.failureCounts[toolCall.Name]
	lastHint := latestOwnedLoopHint(toolResults)
	recentlyResearched := wasOwnedLoopResearchRecentlyDone(toolResults, toolCall.Name)
	// Candidate for interface capture toward LAO and/or heuristic policy enforcer:
	// this is the owned-loop seam where provider-native orchestration consumes policy output.
	// Keep the loop authoritative here; if more policy types appear, inject them rather than
	// letting retry state accumulate hidden decision logic.
	decision := s.policy.EvaluateRetryDecision(ai.RetryDecisionInput{
		ToolName:           toolCall.Name,
		Err:                err,
		FailureCount:       currentFailureCount,
		Hint:               lastHint,
		RecentlyResearched: recentlyResearched,
		Learnings:          s.learnings,
		LearningContext:    s.learningContext,
	})
	plan := decision.Plan
	tier := decision.Tier
	s.lastRecommendation = decision.Recommendation
	if previous, ok := s.lastFailures[toolCall.Name]; ok {
		s.learnings = s.kernel.RecordOutcome(s.learnings, toolCall.Name, previous.err, previous.plan, false, s.learningContext)
	}
	s.failureCounts[toolCall.Name] = currentFailureCount + 1
	s.lastFailures[toolCall.Name] = ownedLoopFailureContext{err: err, plan: plan}

	if decision.RequiresClarify || tier == ai.RetryTierClarify {
		guidance := ai.BuildRetryGuidance(toolCall.Name, plan, decision.Recommendation, currentFailureCount+1, s.allowedTurns, s.maxAllowedTurns, true)
		s.learnings = s.kernel.RecordOutcome(s.learnings, toolCall.Name, err, plan, false, s.learningContext)
		delete(s.lastFailures, toolCall.Name)
		s.pendingRepair = nil
		s.escalationState = map[string]any{
			"tier":             3,
			"failed_tool":      toolCall.Name,
			"failure_count":    currentFailureCount,
			"strategy":         string(plan.Strategy),
			"suggested_action": "backward_phase_transition",
			"error_type":       err.Error(),
		}
		return ai.ReActToolResult{
			Name:          toolCall.Name,
			Args:          cloneRetryArgs(toolCall.Args),
			Result:        formatOwnedLoopClarificationResult(plan, toolCall.Args, err, guidance),
			RetryGuidance: guidance,
			Hint:          lastHint,
		}, true
	}
	planCopy := plan
	s.pendingRepair = &planCopy
	guidance := ai.BuildRetryGuidance(toolCall.Name, plan, decision.Recommendation, currentFailureCount+1, s.allowedTurns, s.maxAllowedTurns, false)

	return ai.ReActToolResult{
		Name:          toolCall.Name,
		Args:          cloneRetryArgs(toolCall.Args),
		Result:        formatOwnedLoopRetryResult(plan, toolCall.Args, err, guidance),
		RetryGuidance: guidance,
		Hint:          lastHint,
	}, false
}

func (s *ownedLoopRetryState) maybeExtendBudget(toolResults []ai.ReActToolResult, currentIteration int) bool {
	if s == nil || currentIteration < s.allowedTurns || s.allowedTurns >= s.maxAllowedTurns {
		return false
	}
	explicitProgress, _ := ownedLoopProgressSignals(toolResults)
	// Candidate for interface capture toward LAO: the extension decision is policy-driven,
	// but the guidance surface is still text formatting downstream. A future provider-native
	// advisory envelope can consume this decision without collapsing back into heuristics-first flow.
	decision := s.policy.EvaluateRetryBudgetExtension(ai.RetryBudgetExtensionInput{
		AllowedTurns:     s.allowedTurns,
		MaxAllowedTurns:  s.maxAllowedTurns,
		CurrentIteration: currentIteration,
		ProgressHint:     latestOwnedLoopHint(toolResults),
		ExplicitProgress: explicitProgress,
	})
	if !decision.Allow {
		return false
	}
	s.allowedTurns += decision.AdditionalTurns
	if s.allowedTurns > s.maxAllowedTurns {
		s.allowedTurns = s.maxAllowedTurns
	}
	s.progressExtensions++
	return true
}

func (s *ownedLoopRetryState) exportedLearnings() []ai.RetryLearning {
	if s == nil || len(s.learnings) == 0 {
		return nil
	}
	return append([]ai.RetryLearning(nil), s.learnings...)
}

func latestOwnedLoopHint(toolResults []ai.ReActToolResult) *ai.ToolProgressHint {
	if len(toolResults) == 0 {
		return nil
	}
	return toolResults[len(toolResults)-1].Hint
}

func wasOwnedLoopResearchRecentlyDone(toolResults []ai.ReActToolResult, failedTool string) bool {
	if len(toolResults) < 2 {
		return false
	}
	for i := len(toolResults) - 1; i >= 0; i-- {
		result := toolResults[i]
		if result.Name == "list_stores" && !strings.Contains(strings.ToLower(result.Result), "error") {
			for j := i - 1; j >= 0 && j >= i-3; j-- {
				prev := toolResults[j]
				if prev.Name == failedTool &&
					(strings.Contains(prev.Result, "Retry instruction:") || strings.Contains(prev.Result, "Tool execution error:")) {
					return true
				}
			}
		}
		if i < len(toolResults)-6 {
			break
		}
	}
	return false
}

func toolResultSignalsRecoverableFailure(resultText string) bool {
	trimmed := strings.TrimSpace(resultText)
	if trimmed == "" {
		return false
	}
	recoverableSignals := []string{
		"execute_script validation error [",
		"Tool execution error:",
		"Retry instruction:",
		"Clarification required:",
	}
	for _, signal := range recoverableSignals {
		if strings.Contains(trimmed, signal) {
			return true
		}
	}
	return false
}

func ownedLoopProgressSignals(toolResults []ai.ReActToolResult) (explicit bool, implicit bool) {
	if len(toolResults) == 0 {
		return false, false
	}
	last := toolResults[len(toolResults)-1]
	if hint := last.Hint; hint != nil {
		status := strings.ToLower(strings.TrimSpace(hint.Status))
		if status == "progressing" || hint.CompletionDelta > 0 || len(hint.Clues) > 0 || len(hint.SuggestedNextTools) > 0 {
			return true, false
		}
	}
	if last.Name == "list_stores" && !strings.Contains(strings.ToLower(last.Result), "error") {
		return false, true
	}
	return false, false
}

func formatOwnedLoopRetryResult(plan ai.RepairPlan, args map[string]any, err error, guidance *ai.RetryGuidance) string {
	strategyLine := ""
	if plan.Strategy != "" {
		strategyLine = fmt.Sprintf("\nRepair strategy: %s", plan.Strategy)
	}
	retryInstruction := fmt.Sprintf("Retry instruction: Call %s next with corrected arguments. Do not switch tools or provide a final answer until the repair attempt is made.", plan.ToolName)
	if plan.Strategy == ai.RepairStrategyResearchFirst {
		researchTool := plan.ResearchTool
		if researchTool == "" {
			researchTool = "list_stores"
		}
		retryInstruction = fmt.Sprintf("Retry instruction: Call %s next, preferably with scoped stores:[...] for the likely targets, use its schema or relations output as source of truth, then return to %s. Do not switch tools or provide a final answer until the research attempt is made.", researchTool, plan.ToolName)
	}
	guidanceText := formatOwnedLoopLLMGuidance(guidance)
	return fmt.Sprintf(
		"Tool execution error: %v\nTool: %s%s\nAttempted args:\n%s\n%s%s",
		err,
		plan.ToolName,
		strategyLine,
		formatRetryArgs(args),
		retryInstruction,
		guidanceText,
	)
}

func formatOwnedLoopClarificationResult(plan ai.RepairPlan, args map[string]any, err error, guidance *ai.RetryGuidance) string {
	strategyLine := ""
	if plan.Strategy != "" {
		strategyLine = fmt.Sprintf("\nRepair strategy: %s", plan.Strategy)
	}
	guidanceText := formatOwnedLoopLLMGuidance(guidance)
	return fmt.Sprintf(
		"Clarification required: The tool %s failed repeatedly and still needs user guidance before more tool calls.\nTool execution error: %v\nTool: %s%s\nAttempted args:\n%s\nUser-facing next step: Ask one short clarification question that resolves the ambiguity blocking this tool call. Do not call more tools until the user answers.%s",
		plan.ToolName,
		err,
		plan.ToolName,
		strategyLine,
		formatRetryArgs(args),
		guidanceText,
	)
}

func formatOwnedLoopLLMGuidance(guidance *ai.RetryGuidance) string {
	return ai.FormatRetryGuidanceForLLM(guidance)
}

func formatRetryArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func cloneRetryArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}

func deriveRetryLearningContext(ctx context.Context, req ai.ReasoningRequest, provider, model string) ai.RetryLearningContext {
	learningContext := ai.RetryLearningContext{
		Provider:  strings.TrimSpace(provider),
		Model:     strings.TrimSpace(model),
		TaskClass: deriveRetryTaskClass(req.UserQuery),
	}
	if payload := ai.GetSessionPayload(ctx); payload != nil {
		learningContext.DomainScope = strings.TrimSpace(payload.ActiveDomain)
		learningContext.RepoScope = strings.TrimSpace(payload.ActiveRepo)
	}
	return learningContext
}

func deriveRetryTaskClass(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	switch {
	case strings.Contains(query, "refactor"):
		return "refactor"
	case strings.Contains(query, "test"):
		return "testing"
	case strings.Contains(query, "bug") || strings.Contains(query, "fix"):
		return "debugging"
	case strings.Contains(query, "implement") || strings.Contains(query, "build"):
		return "implementation"
	case strings.Contains(query, "join") || strings.Contains(query, "query") || strings.Contains(query, "store"):
		return "data"
	default:
		return "general"
	}
}
