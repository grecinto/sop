package agent

import (
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func (a *CopilotAgent) updateStructuredPlanProgressAfterTurn(classification *AskTaskContext, toolCalls []ai.ToolCall, outcomeFacts []string, clarificationRequest *ai.ClarificationRequest) {
	if classification == nil || a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return
	}
	thread := a.service.session.Memory.GetCurrentThread()
	if thread == nil || thread.StreamContext == nil || thread.StreamContext.StructuredPlan == nil {
		return
	}
	plan := thread.StreamContext.StructuredPlan
	if len(plan.Steps) == 0 {
		return
	}
	if plan.CurrentStep < 0 || plan.CurrentStep >= len(plan.Steps) {
		plan.CurrentStep = 0
	}
	now := time.Now().UnixMilli()
	current := &plan.Steps[plan.CurrentStep]
	if strings.TrimSpace(current.Status) == "" || current.Status == "pending" {
		current.Status = "active"
		current.UpdatedAt = now
	}
	plan.UpdatedAt = now

	if clarificationRequest != nil {
		return
	}
	if shouldMarkStructuredPlanStepRefining(classification, current, toolCalls, outcomeFacts) {
		current.Status = "refining"
		current.UpdatedAt = now
		if phase := stepPhaseForStructuredPlanStep(*current, plan.CurrentStep); phase != "" {
			thread.StreamContext.CurrentPhase = phase
		}
		return
	}
	if !a.shouldAdvanceStructuredPlanStepForDomain(classification, current, toolCalls, outcomeFacts) {
		return
	}

	current.Status = "complete"
	current.UpdatedAt = now
	if current.StepID != "" && !containsString(plan.CompletedSteps, current.StepID) {
		plan.CompletedSteps = append(plan.CompletedSteps, current.StepID)
	}
	if plan.CurrentStep+1 < len(plan.Steps) {
		plan.CurrentStep++
		next := &plan.Steps[plan.CurrentStep]
		if strings.TrimSpace(next.Status) == "" || next.Status == "pending" {
			next.Status = "active"
			next.UpdatedAt = now
		}
	}
	if plan.CurrentStep >= 0 && plan.CurrentStep < len(plan.Steps) {
		thread.StreamContext.CurrentPhase = stepPhaseForStructuredPlanStep(plan.Steps[plan.CurrentStep], plan.CurrentStep)
	}
}

func (a *CopilotAgent) enqueueStructuredPlanContinuationAfterTurn(query string, classification *AskTaskContext, previousStep int, clarificationRequest *ai.ClarificationRequest) {
	if a == nil || classification == nil || clarificationRequest != nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return
	}
	thread := a.service.session.Memory.GetCurrentThread()
	if thread == nil || thread.StreamContext == nil || thread.StreamContext.StructuredPlan == nil {
		return
	}
	plan := thread.StreamContext.StructuredPlan
	if len(plan.Steps) == 0 || plan.CurrentStep <= previousStep || plan.CurrentStep >= len(plan.Steps) {
		return
	}
	nextPhase := stepPhaseForStructuredPlanStep(plan.Steps[plan.CurrentStep], plan.CurrentStep)
	queue := a.getOrCreateSessionQueue()
	for _, item := range queue.Snapshot() {
		if item.Context == nil {
			continue
		}
		flag, _ := item.Context["structured_plan_continuation"].(bool)
		stepIdx, _ := item.Context["structured_plan_step"].(int)
		if flag && stepIdx == plan.CurrentStep {
			return
		}
	}
	_ = queue.Enqueue(WorkItem{
		Query:    query,
		Actor:    "phase:" + nextPhase,
		Priority: PriorityPhaseSuccession,
		Context: map[string]any{
			"structured_plan_continuation": true,
			"structured_plan_step":         plan.CurrentStep,
		},
	})
	if routing := a.service.session.Memory.GetRoutingState(); routing != nil && routing.AskClassification != nil {
		routing.AskClassification.Phase = nextPhase
		a.service.session.Memory.SetRoutingState(routing)
	}
}

func shouldAdvanceStructuredPlanStep(step *cw.StructuredPlanStep, toolCalls []ai.ToolCall, outcomeFacts []string) bool {
	if step == nil {
		return false
	}
	if len(toolCalls) == 0 && len(outcomeFacts) == 0 {
		return false
	}
	actionText := strings.ToLower(strings.TrimSpace(step.ActionInstruction + " " + step.Goal + " " + strings.Join(step.Validation, " ") + " " + strings.Join(step.ExitCriteria, " ")))
	if actionText == "" {
		return len(toolCalls) > 0 && len(outcomeFacts) > 0
	}
	for _, toolCall := range toolCalls {
		toolName := strings.ToLower(strings.TrimSpace(toolCall.Name))
		if toolMatchesStructuredPlanStep(toolName, actionText) {
			return true
		}
	}
	return len(toolCalls) > 0 && len(outcomeFacts) > 0
}

func shouldMarkStructuredPlanStepRefining(classification *AskTaskContext, step *cw.StructuredPlanStep, toolCalls []ai.ToolCall, outcomeFacts []string) bool {
	if classification == nil || step == nil {
		return false
	}
	if len(toolCalls) == 0 && len(outcomeFacts) == 0 {
		return false
	}
	phase := stepPhaseForStructuredPlanStep(*step, 0)
	if phase != "planning" && phase != "review" {
		return false
	}
	if shouldAdvanceStructuredPlanStep(step, toolCalls, outcomeFacts) {
		return false
	}
	return true
}

func (a *CopilotAgent) shouldAdvanceStructuredPlanStepForDomain(classification *AskTaskContext, step *cw.StructuredPlanStep, toolCalls []ai.ToolCall, outcomeFacts []string) bool {
	if classification == nil || a == nil || a.capabilityRegistry == nil || step == nil {
		return shouldAdvanceStructuredPlanStep(step, toolCalls, outcomeFacts)
	}
	domain := ""
	if len(classification.Domains) > 0 {
		domain = classification.Domains[0]
	}
	routingState := capabilities.RoutingState{Domain: domain, CapabilityHint: classification.Phase}
	capability, ok := a.capabilityRegistry.Resolve(classification.OriginalQuery, routingState)
	if ok && capability != nil {
		if provider, ok := capability.(capabilities.StepCompletionProvider); ok {
			actionText := strings.TrimSpace(step.ActionInstruction + " " + step.Goal)
			return provider.EvaluateStepCompletion(classification.OriginalQuery, routingState, actionText, step.Validation, toolCalls, outcomeFacts)
		}
	}
	return shouldAdvanceStructuredPlanStep(step, toolCalls, outcomeFacts)
}

func toolMatchesStructuredPlanStep(toolName, actionText string) bool {
	switch {
	case actionLooksLikeValidation(actionText):
		return strings.Contains(toolName, "run_tests") || strings.Contains(toolName, "verify") || strings.Contains(toolName, "review")
	case actionLooksLikePatch(actionText):
		return strings.Contains(toolName, "apply_patch") || strings.Contains(toolName, "apply_multi_file_patch") || strings.Contains(toolName, "execute_script")
	case actionLooksLikeInvestigation(actionText):
		return strings.Contains(toolName, "inspect") || strings.Contains(toolName, "search") || strings.Contains(toolName, "list") || strings.Contains(toolName, "scan") || strings.Contains(toolName, "describe") || strings.Contains(toolName, "state")
	case actionLooksLikePlanning(actionText):
		return strings.Contains(toolName, "plan") || strings.Contains(toolName, "design") || strings.Contains(toolName, "build_context")
	default:
		return false
	}
}

var canonicalStructuredPlanPhases = map[string]struct{}{
	"intake":        {},
	"investigation": {},
	"planning":      {},
	"execution":     {},
	"review":        {},
	"delivery":      {},
}

func normalizedStructuredPlanPhase(text string) string {
	phase := strings.TrimSpace(strings.ToLower(text))
	if _, ok := canonicalStructuredPlanPhases[phase]; ok {
		return phase
	}
	return ""
}

func actionLooksLikeValidation(text string) bool {
	for _, marker := range []string{"validate", "validation", "test", "verify", "confirm", "review"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func actionLooksLikePatch(text string) bool {
	for _, marker := range []string{"patch", "apply", "edit", "update", "change", "fix", "implement", "write"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func actionLooksLikeInvestigation(text string) bool {
	for _, marker := range []string{"inspect", "search", "find", "identify", "trace", "gather", "scan", "describe", "localize"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func actionLooksLikePlanning(text string) bool {
	for _, marker := range []string{"plan", "design", "decide", "choose", "define", "assess"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
