package generator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
)

type ownedLoopRetryState struct {
	coordinator     *ai.RetryCoordinator
	failureCounts   map[string]int
	escalationState map[string]any
	pendingRepair   *ai.RepairPlan
	learnings       []ai.RetryLearning
	allowedTurns    int
	maxAllowedTurns int
	lastFailures    map[string]ownedLoopFailureContext
	progressExtensions int
}

type ownedLoopFailureContext struct {
	err  error
	plan ai.RepairPlan
}


func newOwnedLoopRetryState(baseTurns int, query string, carryover *ai.CarryoverState) *ownedLoopRetryState {
	var learnings []ai.RetryLearning
	if carryover != nil && len(carryover.RetryLearnings) > 0 {
		learnings = append([]ai.RetryLearning(nil), carryover.RetryLearnings...)
	}
	coordinator := ai.NewRetryCoordinator()
	allowed := coordinator.CalculateAdaptiveBudget(baseTurns, query, learnings)
	maxAllowed := baseTurns + 3
	if maxAllowed < allowed {
		maxAllowed = allowed
	}
	return &ownedLoopRetryState{
		coordinator:   coordinator,
		failureCounts: make(map[string]int),
		learnings:     learnings,
		allowedTurns:  allowed,
		maxAllowedTurns: maxAllowed,
		lastFailures:  make(map[string]ownedLoopFailureContext),
	}
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
	if s.coordinator.AllowsTool(*s.pendingRepair, toolName) {
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
		Result: s.coordinator.FormatRepairDirective(*s.pendingRepair, attemptedTool),
	}
}

func (s *ownedLoopRetryState) completeSuccessfulTool(toolName string) {
	if s == nil {
		return
	}
	if ctx, ok := s.lastFailures[toolName]; ok {
		s.learnings = s.coordinator.RecordOutcome(s.learnings, toolName, ctx.err, ctx.plan, true)
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
	tier := s.coordinator.DetermineRetryTier(err.Error(), currentFailureCount, lastHint)
	plan := s.coordinator.ClassifyRepair(toolCall.Name, err, lastHint, recentlyResearched)
	plan = s.coordinator.ApplyLearnedStrategy(plan, toolCall.Name, err, s.learnings)
	if plan.Strategy == ai.RepairStrategyResearchFirst && tier < ai.RetryTierResearch {
		tier = ai.RetryTierResearch
	}
	plan.Tier = tier
	if previous, ok := s.lastFailures[toolCall.Name]; ok {
		s.learnings = s.coordinator.RecordOutcome(s.learnings, toolCall.Name, previous.err, previous.plan, false)
	}
	s.failureCounts[toolCall.Name] = currentFailureCount + 1
	s.lastFailures[toolCall.Name] = ownedLoopFailureContext{err: err, plan: plan}

	if tier == ai.RetryTierClarify {
		s.learnings = s.coordinator.RecordOutcome(s.learnings, toolCall.Name, err, plan, false)
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
			Name:   toolCall.Name,
			Args:   cloneRetryArgs(toolCall.Args),
			Result: formatOwnedLoopClarificationResult(plan, toolCall.Args, err),
			Hint:   lastHint,
		}, true
	}
	planCopy := plan
	s.pendingRepair = &planCopy

	return ai.ReActToolResult{
		Name:   toolCall.Name,
		Args:   cloneRetryArgs(toolCall.Args),
		Result: formatOwnedLoopRetryResult(plan, toolCall.Args, err),
		Hint:   lastHint,
	}, false
}

func (s *ownedLoopRetryState) maybeExtendBudget(toolResults []ai.ReActToolResult, currentIteration int) bool {
	if s == nil || currentIteration < s.allowedTurns || s.allowedTurns >= s.maxAllowedTurns {
		return false
	}
	explicitProgress, implicitProgress := ownedLoopProgressSignals(toolResults)
	if !explicitProgress && !implicitProgress {
		return false
	}
	if implicitProgress && s.progressExtensions > 0 {
		return false
	}
		extension := 1
		if hint := latestOwnedLoopHint(toolResults); hint != nil && hint.CompletionDelta >= 0.25 {
			extension = 2
		}
		s.allowedTurns += extension
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

func formatOwnedLoopRetryResult(plan ai.RepairPlan, args map[string]any, err error) string {
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
	return fmt.Sprintf(
		"Tool execution error: %v\nTool: %s%s\nAttempted args:\n%s\n%s",
		err,
		plan.ToolName,
		strategyLine,
		formatRetryArgs(args),
		retryInstruction,
	)
}

func formatOwnedLoopClarificationResult(plan ai.RepairPlan, args map[string]any, err error) string {
	strategyLine := ""
	if plan.Strategy != "" {
		strategyLine = fmt.Sprintf("\nRepair strategy: %s", plan.Strategy)
	}
	return fmt.Sprintf(
		"Clarification required: The tool %s failed repeatedly and still needs user guidance before more tool calls.\nTool execution error: %v\nTool: %s%s\nAttempted args:\n%s\nUser-facing next step: Ask one short clarification question that resolves the ambiguity blocking this tool call. Do not call more tools until the user answers.",
		plan.ToolName,
		err,
		plan.ToolName,
		strategyLine,
		formatRetryArgs(args),
	)
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
