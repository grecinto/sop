package ai

import (
	"encoding/json"
	"fmt"
	log "log/slog"
	"strings"
	"time"
)

// RetryCoordinator implements progressive retry logic (Tier 1/2A/2B/3) shared across
// provider-owned and shared-compatible ReAct loops.
type RetryCoordinator struct{}

func NewRetryCoordinator() *RetryCoordinator {
	return &RetryCoordinator{}
}

type RetryTier int

const (
	RetryTierQuickFix RetryTier = 1
	RetryTierResearch RetryTier = 2
	RetryTierClarify  RetryTier = 3
)

type RepairStrategy string

const (
	RepairStrategySameTool      RepairStrategy = "same_tool"
	RepairStrategyResearchFirst RepairStrategy = "research_first"
)

type RepairPlan struct {
	ToolName       string
	Strategy       RepairStrategy
	Tier           RetryTier
	ResearchTool   string
	ResearchReason string
}

type RetryLearning struct {
	Provider            string         `json:"provider,omitempty"`
	Model               string         `json:"model,omitempty"`
	RepoScope           string         `json:"repo_scope,omitempty"`
	DomainScope         string         `json:"domain_scope,omitempty"`
	TaskClass           string         `json:"task_class,omitempty"`
	ToolName            string         `json:"tool_name,omitempty"`
	ErrorPattern        string         `json:"error_pattern,omitempty"`
	PreferredStrategy   RepairStrategy `json:"preferred_strategy,omitempty"`
	SuccessCount        int            `json:"success_count,omitempty"`
	FailureCount        int            `json:"failure_count,omitempty"`
	LastUsedAtUnixMilli int64          `json:"last_used_at_unix_milli,omitempty"`
}

type RetryLearningContext struct {
	Provider    string
	Model       string
	RepoScope   string
	DomainScope string
	TaskClass   string
}

type RetryLearningRecommendation struct {
	Strategy RepairStrategy
	Score    float64
	Reason   string
}

type RetryGuidance struct {
	ToolName             string          `json:"tool_name,omitempty"`
	Tier                 RetryTier       `json:"tier,omitempty"`
	Strategy             RepairStrategy  `json:"strategy,omitempty"`
	FailureCount         int             `json:"failure_count,omitempty"`
	SoftRetryBudget      int             `json:"soft_retry_budget,omitempty"`
	HardRetryBudget      int             `json:"hard_retry_budget,omitempty"`
	AllowedTool          string          `json:"allowed_tool,omitempty"`
	ResearchTool         string          `json:"research_tool,omitempty"`
	ClarificationNeeded  bool            `json:"clarification_needed,omitempty"`
	RecommendedStrategy  RepairStrategy  `json:"recommended_strategy,omitempty"`
	RecommendationScore  float64         `json:"recommendation_score,omitempty"`
	RecommendationReason string          `json:"recommendation_reason,omitempty"`
	NextAction           ReActNextAction `json:"next_action,omitempty"`
}

func BuildClarificationRequest(targetQuery string, guidance *RetryGuidance, question string) *ClarificationRequest {
	question = strings.TrimSpace(question)
	targetQuery = strings.TrimSpace(targetQuery)
	if question == "" && guidance == nil {
		return nil
	}
	request := &ClarificationRequest{
		TargetQuery:       targetQuery,
		AssistantQuestion: question,
		Status:            "pending",
	}
	if guidance != nil {
		request.Source = strings.TrimSpace(guidance.ToolName)
		request.MissingFact = strings.TrimSpace(firstNonEmptyString(guidance.RecommendationReason, guidance.ToolName))
		if guidance.ClarificationNeeded {
			request.Reason = "retry_policy_requested_clarification"
		}
	}
	return request
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type RecoverabilityPolicy interface {
	IsRecoverable(err error) bool
}

type RetryDecisionPolicy interface {
	EvaluateRetryDecision(input RetryDecisionInput) RetryDecision
}

type RetryBudgetPolicy interface {
	InitialRetryBudget(input RetryBudgetInput) RetryBudgetDecision
	EvaluateRetryBudgetExtension(input RetryBudgetExtensionInput) RetryBudgetExtensionDecision
}

type RepairEnforcementPolicy interface {
	AllowsTool(plan RepairPlan, toolName string) bool
	FormatRepairDirective(plan RepairPlan, attemptedToolName string) string
}

type RetryPolicySurface interface {
	RecoverabilityPolicy
	RetryDecisionPolicy
	RetryBudgetPolicy
	RepairEnforcementPolicy
}

// Candidate for interface capture toward LAO: if advisory model guidance and
// heuristic hard-rail enforcement keep evolving independently, split this input
// into narrower advisory vs enforcement contracts instead of growing one mixed surface.

type RetryDecisionInput struct {
	ToolName           string
	Err                error
	FailureCount       int
	Hint               *ToolProgressHint
	RecentlyResearched bool
	Learnings          []RetryLearning
	LearningContext    RetryLearningContext
}

type RetryDecision struct {
	Tier              RetryTier
	Plan              RepairPlan
	Recommendation    *RetryLearningRecommendation
	RequiresClarify   bool
	IsRecoverable     bool
	EnforcementReason string
}

type RetryBudgetInput struct {
	BaseTurns int
	Query     string
	Learnings []RetryLearning
}

type RetryBudgetDecision struct {
	SoftLimit int
	HardLimit int
}

type RetryBudgetExtensionInput struct {
	AllowedTurns     int
	MaxAllowedTurns  int
	CurrentIteration int
	ProgressHint     *ToolProgressHint
	ExplicitProgress bool
}

type RetryBudgetExtensionDecision struct {
	Allow           bool
	AdditionalTurns int
}

type DefaultRetryPolicySurface struct {
	coordinator *RetryCoordinator
}

func NewDefaultRetryPolicySurface() RetryPolicySurface {
	return NewDefaultRetryPolicySurfaceWithCoordinator(nil)
}

func NewDefaultRetryPolicySurfaceWithCoordinator(coordinator *RetryCoordinator) RetryPolicySurface {
	if coordinator == nil {
		coordinator = NewRetryCoordinator()
	}
	return &DefaultRetryPolicySurface{coordinator: coordinator}
}

func (p *DefaultRetryPolicySurface) IsRecoverable(err error) bool {
	return p.coordinator.IsRecoverableToolError(err)
}

func (p *DefaultRetryPolicySurface) EvaluateRetryDecision(input RetryDecisionInput) RetryDecision {
	// Candidate for interface capture toward LAO and/or heuristic policy enforcer:
	// this default implementation still composes heuristic tiering, repair selection,
	// and learned guidance in one place. If provider-native LAO policy becomes richer,
	// keep this as the hard-rail fallback and move advisory selection behind a dedicated interface.
	plan := p.coordinator.ClassifyRepair(input.ToolName, input.Err, input.Hint, input.RecentlyResearched)
	tier := p.coordinator.DetermineRetryTier(errorText(input.Err), input.FailureCount, input.Hint)
	var recommendation *RetryLearningRecommendation
	if learned, ok := p.coordinator.RecommendStrategyDetails(input.ToolName, input.Err, input.Learnings, input.LearningContext); ok {
		recommendation = &learned
	}
	if plan.Strategy == RepairStrategyResearchFirst && tier < RetryTierResearch {
		tier = RetryTierResearch
	}
	plan.Tier = tier
	return RetryDecision{
		Tier:            tier,
		Plan:            plan,
		Recommendation:  recommendation,
		RequiresClarify: tier == RetryTierClarify,
		IsRecoverable:   p.coordinator.IsRecoverableToolError(input.Err),
	}
}

func (p *DefaultRetryPolicySurface) InitialRetryBudget(input RetryBudgetInput) RetryBudgetDecision {
	softLimit := p.coordinator.CalculateAdaptiveBudget(input.BaseTurns, input.Query, input.Learnings)
	hardLimit := input.BaseTurns + 3
	if hardLimit < softLimit {
		hardLimit = softLimit
	}
	return RetryBudgetDecision{SoftLimit: softLimit, HardLimit: hardLimit}
}

func (p *DefaultRetryPolicySurface) EvaluateRetryBudgetExtension(input RetryBudgetExtensionInput) RetryBudgetExtensionDecision {
	// Candidate for interface capture toward LAO: the hard ceiling belongs here,
	// but the meaning of "progress" may later need a separate advisory policy so
	// providers can surface budget tradeoffs to the model in a native way.
	if input.CurrentIteration < input.AllowedTurns || input.AllowedTurns >= input.MaxAllowedTurns || !input.ExplicitProgress {
		return RetryBudgetExtensionDecision{}
	}
	additionalTurns := 1
	if input.ProgressHint != nil && input.ProgressHint.CompletionDelta >= 0.25 {
		additionalTurns = 2
	}
	if input.AllowedTurns+additionalTurns > input.MaxAllowedTurns {
		additionalTurns = input.MaxAllowedTurns - input.AllowedTurns
	}
	if additionalTurns <= 0 {
		return RetryBudgetExtensionDecision{}
	}
	return RetryBudgetExtensionDecision{Allow: true, AdditionalTurns: additionalTurns}
}

func (p *DefaultRetryPolicySurface) AllowsTool(plan RepairPlan, toolName string) bool {
	return p.coordinator.AllowsTool(plan, toolName)
}

func (p *DefaultRetryPolicySurface) FormatRepairDirective(plan RepairPlan, attemptedToolName string) string {
	return p.coordinator.FormatRepairDirective(plan, attemptedToolName)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func BuildRetryGuidance(toolName string, plan RepairPlan, recommendation *RetryLearningRecommendation, failureCount int, softBudget int, hardBudget int, clarificationNeeded bool) *RetryGuidance {
	guidance := &RetryGuidance{
		ToolName:            strings.TrimSpace(toolName),
		Tier:                plan.Tier,
		Strategy:            plan.Strategy,
		FailureCount:        failureCount,
		SoftRetryBudget:     softBudget,
		HardRetryBudget:     hardBudget,
		AllowedTool:         strings.TrimSpace(plan.ToolName),
		ResearchTool:        strings.TrimSpace(plan.ResearchTool),
		ClarificationNeeded: clarificationNeeded,
	}
	if clarificationNeeded {
		guidance.NextAction = ReActNextActionAskClarification
	} else if plan.Strategy == RepairStrategyResearchFirst {
		researchTool := strings.TrimSpace(plan.ResearchTool)
		if researchTool == "" {
			researchTool = "list_stores"
		}
		guidance.AllowedTool = researchTool
		guidance.NextAction = ReActNextActionCallTool
	} else {
		guidance.NextAction = ReActNextActionRetrySameTool
	}
	if recommendation != nil {
		guidance.RecommendedStrategy = recommendation.Strategy
		guidance.RecommendationScore = recommendation.Score
		guidance.RecommendationReason = recommendation.Reason
	}
	return guidance
}

func FormatRetryGuidanceForLLM(guidance *RetryGuidance) string {
	if guidance == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if guidance.RecommendedStrategy != "" {
		parts = append(parts, fmt.Sprintf("LLM retry guidance: A learned pattern suggests strategy=%s (score=%.2f, %s). Use this as guidance, not as a mandatory action.", guidance.RecommendedStrategy, guidance.RecommendationScore, guidance.RecommendationReason))
	}
	parts = append(parts, fmt.Sprintf("Retry context: failure_count=%d, soft_retry_budget=%d, hard_retry_budget=%d.", guidance.FailureCount, guidance.SoftRetryBudget, guidance.HardRetryBudget))
	parts = append(parts, fmt.Sprintf("Retry policy: tier=%d, strategy=%s, allowed_tool=%s, next_action=%s.", guidance.Tier, guidance.Strategy, guidance.AllowedTool, guidance.NextAction))
	parts = append(parts, "The model should decide whether to retry the same tool, research first, change tools appropriately, or ask for clarification within these hard limits.")
	return "\n" + strings.Join(parts, "\n")
}

func AppendRetryGuidanceState(resultText string, guidance *RetryGuidance) string {
	trimmed := strings.TrimSpace(resultText)
	if guidance == nil {
		return trimmed
	}
	raw, err := json.Marshal(guidance)
	if err != nil {
		return trimmed
	}
	if trimmed == "" {
		return "Retry policy state: " + string(raw)
	}
	return trimmed + "\nRetry policy state: " + string(raw)
}

func BuildRepairDirectiveFromRetryGuidance(toolName string, guidance *RetryGuidance) string {
	if guidance == nil {
		return ""
	}
	toolName = strings.TrimSpace(toolName)
	if guidance.ClarificationNeeded {
		return "Clarification directive: The last tool failure still remains unresolved after the first repair attempt. Do not call another tool yet. Ask the user one short, concrete clarification question that names the blocker and wait for the answer."
	}
	if guidance.Strategy == RepairStrategyResearchFirst {
		allowedTool := strings.TrimSpace(guidance.AllowedTool)
		if allowedTool == "" {
			allowedTool = "list_stores"
		}
		return fmt.Sprintf("Repair directive: The last tool call to %s failed because grounded schema or relation facts are still missing. Your next step should be to call %s first, prefer scoped stores:[...] for likely targets, reuse its schema/relations output as the source of truth, and only then return to %s if needed. Do not summarize the error as a final answer unless correction is impossible.", toolName, allowedTool, toolName)
	}
	return fmt.Sprintf("Repair directive: The last tool call to %s failed because its arguments were invalid. Your next step should be to call the same tool again with corrected arguments using the repair guidance below. Preserve valid script slices and change only the malformed condition or join. Do not summarize the error as a final answer unless correction is impossible.", toolName)
}

func (rc *RetryCoordinator) IsRecoverableToolError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	recoverableHints := []string{
		"missing",
		"required",
		"not a string",
		"invalid type",
		"must be",
		"expected",
		"validation failed",
		"grammar validation",
		"unnecessary nested joins",
		"relation",
		"schema",
		"join mapping",
		"not found",
		"does not exist",
	}
	for _, hint := range recoverableHints {
		if strings.Contains(message, hint) {
			return true
		}
	}
	return false
}

func (rc *RetryCoordinator) DetermineRetryTier(errorType string, failureCount int, hint *ToolProgressHint) RetryTier {
	if hint != nil && hint.SuggestedAction == "research_before_retry" {
		return RetryTierResearch
	}
	if hint != nil && hint.SuggestedAction == "give_up_and_clarify" {
		return RetryTierClarify
	}
	if failureCount == 0 {
		return RetryTierQuickFix
	}
	if failureCount == 1 {
		if rc.isResearchableError(errorType) {
			return RetryTierResearch
		}
		return RetryTierQuickFix
	}
	return RetryTierClarify
}

func (rc *RetryCoordinator) ClassifyRepair(toolName string, err error, hint *ToolProgressHint, recentlyResearched bool) RepairPlan {
	plan := RepairPlan{
		ToolName: toolName,
		Strategy: RepairStrategySameTool,
		Tier:     RetryTierQuickFix,
	}

	if hint != nil && hint.SuggestedAction != "" {
		switch hint.SuggestedAction {
		case "research_before_retry":
			plan.Strategy = RepairStrategyResearchFirst
			plan.ResearchTool = "list_stores"
			if len(hint.SuggestedNextTools) > 0 {
				plan.ResearchTool = hint.SuggestedNextTools[0]
			}
			if len(hint.Tips) > 0 {
				plan.ResearchReason = hint.Tips[0]
			} else {
				plan.ResearchReason = "Tool suggested research before retry"
			}
			log.Info("Tier 2A: Using hint-driven retry strategy",
				"tool", toolName,
				"strategy", "research_first",
				"research_tool", plan.ResearchTool,
				"reason", plan.ResearchReason)
			return plan
		case "give_up_and_clarify":
			log.Info("Tier 2A: Tool suggests giving up", "tool", toolName)
		}
	}

	if toolName != "execute_script" || err == nil {
		return plan
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "relation") ||
		strings.Contains(message, "schema") ||
		strings.Contains(message, "join mapping") {
		if !recentlyResearched {
			plan.Strategy = RepairStrategyResearchFirst
			plan.ResearchTool = "list_stores"
			plan.ResearchReason = "The failure text points to unresolved schema or relation ambiguity."
		}
	}

	return plan
}

func (rc *RetryCoordinator) ApplyLearnedStrategy(plan RepairPlan, toolName string, err error, learnings []RetryLearning, context RetryLearningContext) RepairPlan {
	strategy, ok := rc.RecommendStrategy(toolName, err, learnings, context)
	if !ok {
		return plan
	}
	plan.Strategy = strategy
	if strategy == RepairStrategyResearchFirst {
		if plan.ResearchTool == "" {
			plan.ResearchTool = "list_stores"
		}
		if plan.ResearchReason == "" {
			plan.ResearchReason = "Learned from prior successful retries for a similar failure."
		}
		if plan.Tier < RetryTierResearch {
			plan.Tier = RetryTierResearch
		}
	}
	return plan
}

func (rc *RetryCoordinator) RecommendStrategyDetails(toolName string, err error, learnings []RetryLearning, context RetryLearningContext) (RetryLearningRecommendation, bool) {
	pattern := rc.ErrorPattern(toolName, err)
	bestScore := -1.0
	var bestStrategy RepairStrategy
	var matched RetryLearning
	for _, learning := range learnings {
		if learning.ToolName != toolName || learning.ErrorPattern != pattern {
			continue
		}
		if !rc.learningMatchesContext(learning, context) {
			continue
		}
		score := rc.learningScore(learning, context)
		if score > bestScore && learning.SuccessCount > 0 {
			bestScore = score
			bestStrategy = learning.PreferredStrategy
			matched = learning
		}
	}
	if bestScore <= 0 || bestStrategy == "" {
		return RetryLearningRecommendation{}, false
	}
	reasonParts := []string{fmt.Sprintf("prior successes=%d failures=%d", matched.SuccessCount, matched.FailureCount)}
	if strings.TrimSpace(matched.RepoScope) != "" {
		reasonParts = append(reasonParts, "repo-scoped")
	}
	if strings.TrimSpace(matched.DomainScope) != "" {
		reasonParts = append(reasonParts, "domain-scoped")
	}
	if strings.TrimSpace(matched.TaskClass) != "" {
		reasonParts = append(reasonParts, "task-class="+matched.TaskClass)
	}
	return RetryLearningRecommendation{
		Strategy: bestStrategy,
		Score:    bestScore,
		Reason:   strings.Join(reasonParts, ", "),
	}, true
}

func (rc *RetryCoordinator) RecommendStrategy(toolName string, err error, learnings []RetryLearning, context RetryLearningContext) (RepairStrategy, bool) {
	pattern := rc.ErrorPattern(toolName, err)
	bestScore := -1.0
	var bestStrategy RepairStrategy
	for _, learning := range learnings {
		if learning.ToolName != toolName || learning.ErrorPattern != pattern {
			continue
		}
		if !rc.learningMatchesContext(learning, context) {
			continue
		}
		score := rc.learningScore(learning, context)
		if score > bestScore && learning.SuccessCount > 0 {
			bestScore = score
			bestStrategy = learning.PreferredStrategy
		}
	}
	if bestScore <= 0 || bestStrategy == "" {
		return "", false
	}
	return bestStrategy, true
}

func (rc *RetryCoordinator) RecordOutcome(learnings []RetryLearning, toolName string, err error, plan RepairPlan, success bool, context RetryLearningContext) []RetryLearning {
	pattern := rc.ErrorPattern(toolName, err)
	now := time.Now().UnixMilli()
	for i := range learnings {
		if learnings[i].ToolName != toolName || learnings[i].ErrorPattern != pattern || learnings[i].PreferredStrategy != plan.Strategy {
			continue
		}
		if !rc.learningMatchesContext(learnings[i], context) {
			continue
		}
		if success {
			learnings[i].SuccessCount++
		} else {
			learnings[i].FailureCount++
		}
		learnings[i].LastUsedAtUnixMilli = now
		return learnings
	}
	learning := RetryLearning{
		Provider:            strings.TrimSpace(context.Provider),
		Model:               strings.TrimSpace(context.Model),
		RepoScope:           strings.TrimSpace(context.RepoScope),
		DomainScope:         strings.TrimSpace(context.DomainScope),
		TaskClass:           strings.TrimSpace(context.TaskClass),
		ToolName:            toolName,
		ErrorPattern:        pattern,
		PreferredStrategy:   plan.Strategy,
		LastUsedAtUnixMilli: now,
	}
	if success {
		learning.SuccessCount = 1
	} else {
		learning.FailureCount = 1
	}
	learnings = append([]RetryLearning{learning}, learnings...)
	if len(learnings) > 16 {
		learnings = learnings[:16]
	}
	return learnings
}

func (rc *RetryCoordinator) CalculateAdaptiveBudget(base int, query string, learnings []RetryLearning) int {
	if base <= 0 {
		base = 1
	}
	budget := base
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(queryLower, "workflow") ||
		strings.Contains(queryLower, "implement") ||
		strings.Contains(queryLower, "refactor") ||
		strings.Contains(queryLower, "fix") ||
		strings.Contains(queryLower, "commit") {
		budget++
	}
	for _, learning := range learnings {
		if learning.SuccessCount > learning.FailureCount {
			budget++
			break
		}
	}
	if budget > base+3 {
		budget = base + 3
	}
	return budget
}

func (rc *RetryCoordinator) ErrorPattern(toolName string, err error) string {
	if err == nil {
		return strings.TrimSpace(toolName) + ":unknown"
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	message = rc.normalizeErrorPattern(message)
	if idx := strings.Index(message, " example fix:"); idx >= 0 {
		message = strings.TrimSpace(message[:idx])
	}
	if len(message) > 160 {
		message = message[:160]
	}
	return strings.TrimSpace(toolName) + ":" + message
}

func (rc *RetryCoordinator) learningScore(learning RetryLearning, context RetryLearningContext) float64 {
	if learning.SuccessCount <= 0 {
		return -1
	}
	score := float64(learning.SuccessCount*2 - learning.FailureCount)
	if strings.TrimSpace(context.RepoScope) != "" && strings.EqualFold(strings.TrimSpace(learning.RepoScope), strings.TrimSpace(context.RepoScope)) {
		score += 2
	}
	if strings.TrimSpace(context.DomainScope) != "" && strings.EqualFold(strings.TrimSpace(learning.DomainScope), strings.TrimSpace(context.DomainScope)) {
		score += 1.5
	}
	if strings.TrimSpace(context.TaskClass) != "" && strings.EqualFold(strings.TrimSpace(learning.TaskClass), strings.TrimSpace(context.TaskClass)) {
		score += 1
	}
	if strings.TrimSpace(context.Model) != "" && strings.EqualFold(strings.TrimSpace(learning.Model), strings.TrimSpace(context.Model)) {
		score += 0.5
	}
	if learning.LastUsedAtUnixMilli > 0 {
		ageHours := float64(time.Now().UnixMilli()-learning.LastUsedAtUnixMilli) / float64(time.Hour.Milliseconds())
		if ageHours > 0 {
			decay := 1.0 / (1.0 + (ageHours / 168.0))
			score *= decay
		}
	}
	return score
}

func (rc *RetryCoordinator) learningMatchesContext(learning RetryLearning, context RetryLearningContext) bool {
	if strings.TrimSpace(learning.Provider) != "" && !strings.EqualFold(strings.TrimSpace(learning.Provider), strings.TrimSpace(context.Provider)) {
		return false
	}
	if strings.TrimSpace(learning.Model) != "" && !strings.EqualFold(strings.TrimSpace(learning.Model), strings.TrimSpace(context.Model)) {
		return false
	}
	if strings.TrimSpace(learning.DomainScope) != "" && !strings.EqualFold(strings.TrimSpace(learning.DomainScope), strings.TrimSpace(context.DomainScope)) {
		return false
	}
	if strings.TrimSpace(learning.RepoScope) != "" && !strings.EqualFold(strings.TrimSpace(learning.RepoScope), strings.TrimSpace(context.RepoScope)) {
		return false
	}
	if strings.TrimSpace(learning.TaskClass) != "" && !strings.EqualFold(strings.TrimSpace(learning.TaskClass), strings.TrimSpace(context.TaskClass)) {
		return false
	}
	return true
}

func (rc *RetryCoordinator) normalizeErrorPattern(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if strings.HasPrefix(message, "execute_script validation error [") {
		remainder := strings.TrimPrefix(message, "execute_script validation error [")
		if closeIdx := strings.Index(remainder, "]:"); closeIdx >= 0 {
			category := strings.TrimSpace(remainder[:closeIdx])
			return "validation:" + category
		}
	}
	if strings.Contains(message, "argument '") {
		start := strings.Index(message, "argument '") + len("argument '")
		if end := strings.Index(message[start:], "'"); end >= 0 {
			field := strings.TrimSpace(message[start : start+end])
			if strings.Contains(message, "missing") {
				return "argument_missing:" + field
			}
			if strings.Contains(message, "not a string") {
				return "argument_type:" + field
			}
		}
	}
	message = strings.ReplaceAll(message, "\"<value>\"", "<value>")
	return message
}

func (rc *RetryCoordinator) ShouldEscalateToClarification(repairAttempts int) bool {
	return repairAttempts >= 2
}

func (rc *RetryCoordinator) FormatRepairDirective(plan RepairPlan, attemptedToolName string) string {
	if plan.Strategy == RepairStrategyResearchFirst {
		allowedTool := plan.ResearchTool
		if allowedTool == "" {
			allowedTool = "list_stores"
		}
		return fmt.Sprintf(
			"Repair required before continuing.\nTool: %s\nRepair note: The model attempted %s instead.\nRetry instruction: Call %s next, preferably with scoped stores:[...] for the likely targets, use its schema/relations output as source of truth, then return to %s. Do not switch to unrelated tools or provide a final answer until the research attempt is made.",
			plan.ToolName,
			attemptedToolName,
			allowedTool,
			plan.ToolName,
		)
	}
	return fmt.Sprintf(
		"Repair required before continuing.\nTool: %s\nRepair note: The model attempted %s instead.\nRetry instruction: Call %s next with corrected arguments. Do not switch tools or provide a final answer until the repair attempt is made.",
		plan.ToolName,
		attemptedToolName,
		plan.ToolName,
	)
}

func (rc *RetryCoordinator) AllowsTool(plan RepairPlan, toolName string) bool {
	if plan.Strategy == RepairStrategyResearchFirst {
		researchTool := plan.ResearchTool
		if researchTool == "" {
			researchTool = "list_stores"
		}
		return toolName == researchTool
	}
	return toolName == plan.ToolName
}

func (rc *RetryCoordinator) isResearchableError(errorType string) bool {
	researchable := []string{
		"table", "relation", "schema", "store", "column",
		"join", "mapping", "not found", "does not exist",
	}
	errorLower := strings.ToLower(errorType)
	for _, keyword := range researchable {
		if strings.Contains(errorLower, keyword) {
			return true
		}
	}
	return false
}
