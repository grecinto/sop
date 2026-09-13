package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop/ai"
)

// AskTaskContext represents enhanced task classification for the Ask pipeline.
// This is returned by ClassifyAskTaskContext and contains all information needed
// for LLM-as-Orchestrator pattern with Phase Factory integration.
type AskTaskContext struct {
	// Domains: One or more domains this query spans
	// Examples: ["stores"], ["spaces"], ["stores", "spaces"], ["coding_workflow"]
	Domains []string `json:"domains"`

	// TaskShape: Stable description of the requested work.
	TaskShape string `json:"task_shape,omitempty"`

	// OrchestrationLevel: Whether the ask should stay lightweight or enter managed workflow control.
	OrchestrationLevel string `json:"orchestration_level,omitempty"`

	// Phase: The current phase for this query.
	// Coding workflow uses the active runtime phases: investigation, planning, execution.
	// Other domains may still use review/delivery when they expose those surfaces.
	Phase string `json:"phase"`

	// PhasePlan: Complete sequence of phases for this task.
	// Coding workflow phase plans are canonicalized to investigation, planning, execution.
	// Single-phase: ["investigation"]
	// Multi-phase: ["investigation", "planning", "execution"]
	// The Ask pipeline processes phases sequentially via work queue
	PhasePlan []string `json:"phase_plan"`

	// Intent: High-level intent category
	// Values: "read_data", "mutate_data", "generate_code", "analyze", "plan"
	Intent string `json:"intent"`

	// CRUD: Operation type flags for tool filtering (Phase 3 optimization)
	// Values: ["C"], ["R"], ["C", "U"], ["R", "U", "D"], etc.
	// Used to filter which tools are exposed to LLM based on operation intent
	CRUD []string `json:"crud"`

	// Confidence: LLM's confidence in this classification
	// Values: "high", "medium", "low"
	Confidence string `json:"confidence"`

	// Reasoning: LLM's explanation for why it chose these domains/phases
	Reasoning string `json:"reasoning"`

	// ExecutionReady: Whether the request is grounded enough to start in execution.
	ExecutionReady bool `json:"execution_ready,omitempty"`

	// OriginalQuery: The unmodified user query
	OriginalQuery string `json:"original_query"`

	// DomainOfferNeeded indicates the current domains are only environment-default suggestions
	// and should be confirmed with the user before planning/tool registration proceeds.
	DomainOfferNeeded bool `json:"-"`
}

const (
	taskShapeDirectAnswer       = "direct_answer"
	taskShapeDirectAction       = "direct_action"
	taskShapeIssueInvestigation = "issue_investigation"
	taskShapeDesignOrFeature    = "design_or_feature"
	taskShapeRefactorCampaign   = "refactor_campaign"
	taskShapeMultiDomain        = "multi_domain_workflow"

	orchestrationSingleTurn       = "single_turn"
	orchestrationManagedObjective = "managed_objective"
)

func isManagedOrchestrationLevel(level string) bool {
	switch strings.TrimSpace(strings.ToLower(level)) {
	case orchestrationManagedObjective:
		return true
	default:
		return false
	}
}

// ClassifyAskTaskContext performs enhanced LLM-based classification that returns:
// 1. Domain(s) - Can be multiple (e.g., ["stores", "spaces"])
// 2. Phase - Current phase (e.g., "investigation")
// 3. PhasePlan - Full sequence (e.g., ["investigation", "execution"])
// 4. Intent - High-level intent (e.g., "read_data")
// 5. Confidence - LLM's confidence level
//
// This replaces the heuristic-based routing in V1. The LLM analyzes the query
// and makes ALL routing decisions based on query semantics, not keywords.
//
// User Specification Flow:
// 1. LLM classifies domain(s) and phase from query context
// 2. Returns single-phase or multi-phase plan based on complexity
// 3. Phase Factory uses this to create domain-specific phase proxies
// 4. Each proxy exposes domain+phase-specific tools
// 5. LLM sees ALL tools and decides which to call
func (a *CopilotAgent) ClassifyAskTaskContext(ctx context.Context, query string, gen ai.Generator) (*AskTaskContext, error) {
	if gen == nil {
		return nil, fmt.Errorf("generator is required for V2 classification")
	}

	// Get available artifacts for domain hints
	storesList, spacesList, err := a.GetSampleArtifacts(ctx)
	if err != nil {
		log.Warn("Failed to get sample artifacts for classification", "error", err)
		storesList = []string{}
		spacesList = []string{}
	}

	// Build classification prompt
	prompt := a.buildAskClassificationPrompt(ctx, query, storesList, spacesList)

	// Call LLM for classification
	resp, err := gen.Generate(ctx, prompt, ai.GenOptions{
		Temperature: 0.3, // Lower temperature for more consistent classification
		MaxTokens:   500,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM classification failed: %w", err)
	}

	// Parse JSON response
	var result AskTaskContext
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		// Try to extract JSON from markdown code block
		if cleaned := extractJSONFromMarkdown(resp.Text); cleaned != "" {
			if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
				log.Warn("Failed to parse classification result", "response", resp.Text, "error", err)
				return a.buildFallbackClassification(query), nil
			}
		} else {
			log.Warn("Failed to parse classification result", "response", resp.Text, "error", err)
			return a.buildFallbackClassification(query), nil
		}
	}

	// Validate and normalize
	result.OriginalQuery = query
	result = a.normalizeAskTaskContext(result)
	result = a.applyCognitiveStateContinuity(ctx, result)

	log.Info("AskTaskContext Classification",
		"domains", result.Domains,
		"task_shape", result.TaskShape,
		"orchestration_level", result.OrchestrationLevel,
		"phase", result.Phase,
		"phase_plan", result.PhasePlan,
		"intent", result.Intent,
		"execution_ready", result.ExecutionReady,
		"confidence", result.Confidence,
		"reasoning", result.Reasoning)

	return &result, nil
}

// buildAskClassificationPrompt constructs the LLM prompt for enhanced classification.
// This prompt instructs the LLM to identify:
// - Domain(s): Which domain(s) this query involves
// - Phase: What phase this query represents
// - PhasePlan: Full sequence of phases needed
// - Intent: High-level intent category
func (a *CopilotAgent) buildAskClassificationPrompt(ctx context.Context, query string, stores, spaces []string) string {
	storesHint := ""
	if len(stores) > 0 {
		storesHint = fmt.Sprintf("\nAvailable Stores: %s", strings.Join(stores[:min(5, len(stores))], ", "))
	}

	spacesHint := ""
	if len(spaces) > 0 {
		spacesHint = fmt.Sprintf("\nAvailable Spaces: %s", strings.Join(spaces[:min(5, len(spaces))], ", "))
	}

	cognitiveGuidance := buildAskClassificationCognitiveGuidance(ctx)
	environmentGuidance := buildAskClassificationEnvironmentGuidance()

	return fmt.Sprintf(`You are a task classifier for an AI assistant. Analyze the user query and classify it into domains, phases, and intent.

**Available Domains:**
- **stores**: Database/store operations (list, scan, query, insert, update, delete)
- **spaces**: Knowledge base operations (search, read, update, vectorize)
- **coding_workflow**: Code editing, refactoring, bug fixes, feature implementation
- **desktop**: Local file operations, shell commands, git operations

**Available Phases:**
- **investigation**: Read-only exploration, discovery, analysis
- **planning**: Design, strategy, approach planning
- **execution**: Write operations, changes, implementations
- **review**: Validation, testing, verification for domains that expose a separate review surface
- **delivery**: Summary, documentation, cleanup for domains that expose a separate delivery surface

**Session Cognitive State (authoritative continuity signal):**
%s

**Environment default-domain guidance:**
%s

**User Query:**
%s
%s%s

**Your Task:**
Analyze the query and return a JSON object with this structure:

{
  "domains": ["domain1", "domain2"],  // One or more domains (can be multiple)
	"task_shape": "shape_name",          // direct_answer|direct_action|issue_investigation|design_or_feature|refactor_campaign|multi_domain_workflow
	"orchestration_level": "level",      // single_turn|managed_objective
  "phase": "phase_name",              // Current phase for this query
  "phase_plan": ["phase1", "phase2"], // Full sequence of phases (can be single phase)
  "intent": "intent_category",        // High-level intent
  "crud": ["C", "R", "U", "D"],       // CRUD operations (one or more: C=Create, R=Read, U=Update, D=Delete)
  "confidence": "high|medium|low",    // Your confidence level
	"reasoning": "explanation",         // Why you chose these domains/phases
	"execution_ready": false             // true only when the first change is already concrete
}

**Classification Guidelines:**

1. **Priority of existing task continuity**: If the session cognitive state shows an active task, preserve that lead unless the user clearly changes topic. The active task is more authoritative than path metadata or incidental folder mentions.
   - Example: active task = "fix failing tests in repo" and new query says "The current folder ../lib/sop, fix failing tests under this folder" → keep **coding_workflow**.
   - Example: active task = repo repair, and user says "show me git status" or "run ls" → switch to **desktop** only because they explicitly ask to execute shell/git commands.

2. **Path metadata is not a new domain trigger**: A local folder, repo path, or working directory is context for the task, not a separate desktop-action signal.
   - A repo location like "../lib/sop" or "my project folder" does not by itself mean "desktop automation".
   - Use it to scope the repo, not to reclassify the task.

3. **Multi-Domain Detection**: If query involves multiple domains, include ALL relevant ones
   - "Query stores and search spaces" → ["stores", "spaces"]
   - "List stores" → ["stores"]

4. **Phase Selection**: Choose based on what the query is asking to DO
   - Read-only operations → "investigation"
   - Write operations → "execution"
   - Planning/design questions → "planning"
	- Validation/testing → "review" only for domains that still expose a separate review phase
   - Simple direct commands → "execution" (single phase)

4a. **Task shape and orchestration level**:
	- Use "single_turn" only when the next action is obvious, narrow, and easy to validate.
	- Use "managed_objective" when the ask needs diagnosis, sequencing, tradeoffs, or more than one meaningful step.
	- Issue-oriented asks such as "fix failing tests" or "debug why" should normally be task_shape="issue_investigation" and orchestration_level="managed_objective".
	- Design/architecture/approach asks should normally be task_shape="design_or_feature" and orchestration_level="managed_objective".
	- Refactor asks that span multiple files or invariants should normally be task_shape="refactor_campaign" and orchestration_level="managed_objective".
	- Set execution_ready=true only when the first concrete change is already known; write intent alone is not enough.

4b. **Coding workflow uses active phases only**:
	- For **coding_workflow**, return only: "investigation", "planning", or "execution"
	- Do **not** emit "intake", "testing", "review", or "delivery" for coding_workflow
	- In coding_workflow, validation/testing/handoff happen inside the implementation conversation rather than separate phases

5. **PhasePlan Thickness**: Vary plan complexity based on task complexity
   - Simple queries → Single phase: ["investigation"]
   - Medium queries → Two phases: ["investigation", "execution"]
	- Complex coding queries → ["investigation", "planning", "execution"]
   - Direct commands (e.g., /cmd ls) → ["execution"]

6. **Intent Categories:**
   - "read_data": Queries, searches, listings, descriptions
   - "mutate_data": Inserts, updates, deletes, modifications
   - "generate_code": Code generation, refactoring, implementation
   - "analyze": Analysis, investigation, understanding
   - "plan": Planning, design, strategy

7. **CRUD Classification (Phase 3 Optimization):**
   - "C" (Create): Insert, add, create new records/files/resources
   - "R" (Read): Query, list, get, search, scan, describe, read
   - "U" (Update): Modify, edit, update, patch existing records/files
   - "D" (Delete): Remove, delete, drop records/files/resources
   - Include ALL operations the query might need (e.g., ["R", "U"] for "update after checking")

**Examples:**

Query: "List stores in production database"
{
  "domains": ["stores"],
	"task_shape": "direct_answer",
	"orchestration_level": "single_turn",
  "phase": "investigation",
  "phase_plan": ["investigation"],
  "intent": "read_data",
  "crud": ["R"],
  "confidence": "high",
	"reasoning": "Simple read-only store listing operation",
	"execution_ready": true
}

Query: "The current folder ../lib/sop, fix failing tests under this folder."
{
  "domains": ["coding_workflow"],
	"task_shape": "issue_investigation",
	"orchestration_level": "managed_objective",
	"phase": "investigation",
	"phase_plan": ["investigation", "planning", "execution"],
  "intent": "generate_code",
  "crud": ["R", "U"],
  "confidence": "high",
	"reasoning": "A local repo path scopes the task to the current codebase; it does not switch the task out of coding_workflow, and failing tests require diagnosis before repair.",
	"execution_ready": false
}

Query: "Query stores and search spaces for 'authentication'"
{
  "domains": ["stores", "spaces"],
  "phase": "investigation",
  "phase_plan": ["investigation"],
  "intent": "read_data",
  "crud": ["R"],
  "confidence": "high",
  "reasoning": "Read-only operations across two domains"
}

Query: "/cmd ls -la"
{
  "domains": ["desktop"],
  "phase": "execution",
  "phase_plan": ["execution"],
  "intent": "read_data",
  "crud": ["R"],
  "confidence": "high",
  "reasoning": "Direct shell command execution, read-only"
}

Return ONLY the JSON object, no other text.`, cognitiveGuidance, environmentGuidance, query, storesHint, spacesHint)
}

func buildAskClassificationEnvironmentGuidance() string {
	if isDaemonPreferredDomainEnvironment() {
		return "If the query is ambiguous or the domain is not identifiable, lean toward coding_workflow and desktop rather than generic/general routing."
	}
	return "If the query is ambiguous or the domain is not identifiable, lean toward stores and spaces rather than generic/general routing."
}

func buildAskClassificationCognitiveGuidance(ctx context.Context) string {
	payload := ai.GetSessionPayload(ctx)
	if payload == nil || payload.CognitiveState == nil {
		return "No active cognitive state available; classify based on the user's current query and explicit task semantics."
	}
	state := payload.CognitiveState
	parts := []string{"Active cognitive state summary:"}
	if strings.TrimSpace(state.CurrentQuery) != "" {
		parts = append(parts, "- current_query: "+state.CurrentQuery)
	}
	if strings.TrimSpace(state.CurrentPhase) != "" {
		parts = append(parts, "- current_phase: "+state.CurrentPhase)
	}
	if state.LastAssessment != nil {
		if strings.TrimSpace(state.LastAssessment.SuggestedPhase) != "" {
			parts = append(parts, "- last_assessment.suggested_phase: "+state.LastAssessment.SuggestedPhase)
		}
		if strings.TrimSpace(state.LastAssessment.Rationale) != "" {
			parts = append(parts, "- last_assessment.rationale: "+state.LastAssessment.Rationale)
		}
	}
	if strings.TrimSpace(state.LastDecisionReason) != "" {
		parts = append(parts, "- last_decision_reason: "+state.LastDecisionReason)
	}
	if len(state.MemorySignals) > 0 {
		parts = append(parts, "- memory_signals: "+strings.Join(state.MemorySignals, " | "))
	}
	parts = append(parts, "- rule: preserve the active coding_workflow lead when the user adds repo path or folder context, unless they explicitly request shell/git/file execution.")
	return strings.Join(parts, "\n")
}

// normalizeAskTaskContext validates and normalizes the LLM classification result.
func (a *CopilotAgent) normalizeAskTaskContext(result AskTaskContext) AskTaskContext {
	usedDefaultDomains := false
	explicitDomains := inferExplicitDomainsFromQuery(result.OriginalQuery)
	if len(explicitDomains) > 0 {
		result.Domains = explicitDomains
		usedDefaultDomains = false
	}
	// Ensure at least one domain
	if len(result.Domains) == 0 {
		result.Domains = defaultAskDomains()
		usedDefaultDomains = true
	}

	// Normalize domain names
	for i, domain := range result.Domains {
		result.Domains[i] = normalizeDomainName(domain)
	}
	if shouldUseDefaultAskDomains(result.Domains) {
		if len(explicitDomains) > 0 {
			result.Domains = explicitDomains
			usedDefaultDomains = false
		} else {
			result.Domains = defaultAskDomains()
			usedDefaultDomains = true
		}
	}

	// Repo-fix queries that identify a local folder are coding_workflow tasks, not desktop automation tasks.
	// Desktop should only be used for direct shell/git/file operations, not for code repair requests scoped to a repo.
	if containsRepoFixIntent(result) {
		result.Domains = []string{"coding_workflow"}
	}

	if strings.TrimSpace(result.TaskShape) == "" {
		result.TaskShape = inferAskTaskShape(result)
	}
	if strings.TrimSpace(result.OrchestrationLevel) == "" {
		result.OrchestrationLevel = inferAskOrchestrationLevel(result)
	}
	if !result.ExecutionReady {
		result.ExecutionReady = inferExecutionReadiness(result)
	}

	// Ensure phase is set
	if result.Phase == "" {
		result.Phase = "investigation" // Default to investigation
	}
	if isManagedOrchestrationLevel(result.OrchestrationLevel) {
		result.Phase = chooseManagedWorkflowPhase(result)
	}

	// Normalize phase name using the active runtime contract for coding_workflow.
	result.Phase = canonicalizeCodingRuntimePhase(&result, result.Phase)

	// Ensure phase_plan is set
	if len(result.PhasePlan) == 0 {
		result.PhasePlan = defaultPhasePlanForAsk(result)
	}

	// Normalize phase names in plan using the active runtime contract for coding_workflow.
	for i, phase := range result.PhasePlan {
		result.PhasePlan[i] = canonicalizeCodingRuntimePhase(&result, phase)
	}
	result.PhasePlan = canonicalizeCodingRuntimePlan(&result, result.PhasePlan)

	// Ensure current phase is in plan
	if !contains(result.PhasePlan, result.Phase) {
		result.PhasePlan = append([]string{result.Phase}, result.PhasePlan...)
	}
	if isManagedOrchestrationLevel(result.OrchestrationLevel) {
		if len(result.PhasePlan) == 0 || askClassificationHasDomain(result.Domains, "coding_workflow") {
			result.PhasePlan = defaultPhasePlanForAsk(result)
		}
		if !contains(result.PhasePlan, result.Phase) {
			result.PhasePlan = append([]string{result.Phase}, result.PhasePlan...)
		}
		result.PhasePlan = canonicalizeCodingRuntimePlan(&result, result.PhasePlan)
	}

	// Default intent
	if result.Intent == "" {
		result.Intent = "general"
	}

	// Default confidence
	if result.Confidence == "" {
		result.Confidence = "medium"
	}
	result.DomainOfferNeeded = usedDefaultDomains && !containsRepoFixIntent(result)

	return result
}

func inferAskTaskShape(result AskTaskContext) string {
	query := strings.ToLower(strings.TrimSpace(result.OriginalQuery))
	if len(result.Domains) > 1 {
		return taskShapeMultiDomain
	}
	if containsRepoFixIntent(result) || strings.Contains(query, "debug") || strings.Contains(query, "diagnose") || strings.Contains(query, "investigate why") {
		return taskShapeIssueInvestigation
	}
	if strings.Contains(query, "refactor") || strings.Contains(query, "cleanup") || strings.Contains(query, "simplify") {
		return taskShapeRefactorCampaign
	}
	if strings.Contains(query, "design") || strings.Contains(query, "architecture") || strings.Contains(query, "approach") || strings.Contains(query, "plan") || strings.Contains(query, "strategy") || strings.Contains(query, "implement") || strings.Contains(query, "feature") {
		return taskShapeDesignOrFeature
	}
	if isExplicitDesktopDirective(query) {
		return taskShapeDirectAction
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.Intent)), "read") || strings.EqualFold(result.Intent, "analyze") {
		return taskShapeDirectAnswer
	}
	return taskShapeDirectAction
}

func inferAskOrchestrationLevel(result AskTaskContext) string {
	if len(result.PhasePlan) > 1 {
		return orchestrationManagedObjective
	}
	switch result.TaskShape {
	case taskShapeIssueInvestigation, taskShapeDesignOrFeature, taskShapeRefactorCampaign, taskShapeMultiDomain:
		return orchestrationManagedObjective
	default:
		return orchestrationSingleTurn
	}
}

func inferExecutionReadiness(result AskTaskContext) bool {
	if result.OrchestrationLevel == orchestrationSingleTurn {
		return true
	}
	if containsRepoFixIntent(result) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(result.Phase), "execution")
}

func chooseManagedWorkflowPhase(result AskTaskContext) string {
	phase := canonicalizeCodingRuntimePhase(&result, result.Phase)
	if phase == "planning" {
		return "planning"
	}
	if result.ExecutionReady && phase == "execution" {
		return "execution"
	}
	switch result.TaskShape {
	case taskShapeDesignOrFeature, taskShapeRefactorCampaign:
		return "planning"
	default:
		return "investigation"
	}
}

func defaultPhasePlanForAsk(result AskTaskContext) []string {
	if !isManagedOrchestrationLevel(result.OrchestrationLevel) {
		return []string{result.Phase}
	}
	if askClassificationHasDomain(result.Domains, "coding_workflow") {
		switch result.TaskShape {
		case taskShapeDesignOrFeature, taskShapeRefactorCampaign:
			return []string{"planning", "execution"}
		default:
			return []string{"investigation", "planning", "execution"}
		}
	}
	if result.Phase != "" {
		return []string{result.Phase}
	}
	return []string{"investigation"}
}

// buildFallbackClassification creates a safe default classification when LLM fails.
func (a *CopilotAgent) buildFallbackClassification(query string) *AskTaskContext {
	domains := inferExplicitDomainsFromQuery(query)
	domainOfferNeeded := false
	if len(domains) == 0 {
		domains = defaultAskDomains()
		domainOfferNeeded = true
	}
	return &AskTaskContext{
		Domains:            domains,
		TaskShape:          taskShapeDirectAnswer,
		OrchestrationLevel: orchestrationSingleTurn,
		Phase:              "investigation",
		PhasePlan:          []string{"investigation"},
		Intent:             "general",
		ExecutionReady:     true,
		Confidence:         "low",
		Reasoning:          fmt.Sprintf("Fallback classification due to LLM parse failure; leaning to environment defaults: %s", strings.Join(defaultAskDomains(), ", ")),
		OriginalQuery:      query,
		DomainOfferNeeded:  domainOfferNeeded,
	}
}

func inferExplicitDomainsFromQuery(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	if isExplicitDesktopDirective(q) {
		return []string{"desktop"}
	}
	if containsRepoFixIntent(AskTaskContext{OriginalQuery: query}) {
		return []string{"coding_workflow"}
	}
	if strings.Contains(q, "space") || strings.Contains(q, "spaces") || strings.Contains(q, "knowledge base") || strings.Contains(q, "kb") {
		return []string{"spaces"}
	}

	domains := make([]string, 0, 2)
	if strings.Contains(q, "store") || strings.Contains(q, "stores") || strings.Contains(q, "database") || strings.Contains(q, "data") || strings.Contains(q, "table") || strings.Contains(q, "schema") || strings.Contains(q, "row") || strings.Contains(q, "rows") || strings.Contains(q, "record") || strings.Contains(q, "records") || strings.Contains(q, "column") || strings.Contains(q, "columns") || strings.Contains(q, "query") || strings.Contains(q, "join") || strings.Contains(q, "scan") || strings.Contains(q, "filter") || strings.Contains(q, "projection") || strings.Contains(q, "select") || strings.Contains(q, "alias") || strings.Contains(q, "list") || strings.Contains(q, "show") || strings.Contains(q, "fetch") || strings.Contains(q, "find") {
		domains = append(domains, "stores")
	}
	if len(domains) > 0 {
		return domains
	}
	for _, marker := range []string{"code", "coding", "repo", "repository", "patch", "implement", "refactor", "parser", "test", "tests", "bug", "compile", "function", "class", "module"} {
		if strings.Contains(q, marker) {
			return []string{"coding_workflow"}
		}
	}
	return nil
}

func shouldUseDefaultAskDomains(domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	for _, domain := range domains {
		normalized := strings.TrimSpace(strings.ToLower(domain))
		if normalized != "" && normalized != "general" {
			return false
		}
	}
	return true
}

func defaultAskDomains() []string {
	if isDaemonPreferredDomainEnvironment() {
		return []string{"coding_workflow", "desktop"}
	}
	return []string{"stores", "spaces"}
}

func isDaemonPreferredDomainEnvironment() bool {
	return localDesktopAutomationEnabled.Load() || GetAutomationPolicy().Enabled
}

func (a *CopilotAgent) applyCognitiveStateContinuity(ctx context.Context, result AskTaskContext) AskTaskContext {
	payload := ai.GetSessionPayload(ctx)
	if payload == nil || payload.CognitiveState == nil {
		return result
	}
	state := payload.CognitiveState
	if state == nil {
		return result
	}

	userQuery := strings.ToLower(strings.TrimSpace(result.OriginalQuery))
	activeQuery := strings.ToLower(strings.TrimSpace(state.CurrentQuery))
	activePhase := strings.ToLower(strings.TrimSpace(state.CurrentPhase))
	activeReason := strings.ToLower(strings.TrimSpace(state.LastDecisionReason))
	if state.LastAssessment != nil {
		activeReason += " " + strings.ToLower(strings.TrimSpace(state.LastAssessment.Rationale))
	}

	isRepoFixLead := false
	for _, marker := range []string{"fix failing tests", "fix the bug", "debug failing tests", "repair", "troubleshoot", "bug fix", "repo fix", "code fix"} {
		if strings.Contains(activeQuery, marker) || strings.Contains(userQuery, marker) {
			isRepoFixLead = true
			break
		}
	}
	if !isRepoFixLead {
		if strings.Contains(activeReason, "coding_workflow") || strings.Contains(activeReason, "repo-fix") || strings.Contains(activeReason, "current codebase") || strings.Contains(activeReason, "local folder") {
			isRepoFixLead = true
		}
	}

	if isRepoFixLead && !isExplicitDesktopDirective(userQuery) {
		result.Domains = []string{"coding_workflow"}
		if result.Intent == "" || result.Intent == "general" {
			result.Intent = "generate_code"
		}
		if strings.TrimSpace(result.Phase) == "" {
			result.Phase = "investigation"
		}
		if activePhase != "" {
			result.Phase = canonicalizeCodingRuntimePhase(&result, activePhase)
		}
	}
	return result
}

func isExplicitDesktopDirective(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	if strings.HasPrefix(query, "/cmd") || strings.Contains(query, "git status") || strings.Contains(query, "git log") || strings.Contains(query, "ls -") || strings.Contains(query, "pwd") || strings.Contains(query, "run shell") {
		return true
	}
	return strings.Contains(query, "shell") && (strings.Contains(query, "run") || strings.Contains(query, "execute") || strings.Contains(query, "command"))
}

func containsRepoFixIntent(result AskTaskContext) bool {
	query := strings.ToLower(strings.TrimSpace(result.OriginalQuery))
	if query == "" {
		return false
	}
	if !strings.Contains(query, "fix") && !strings.Contains(query, "repair") && !strings.Contains(query, "debug") && !strings.Contains(query, "troubleshoot") {
		return false
	}
	if !strings.Contains(query, "test") && !strings.Contains(query, "tests") && !strings.Contains(query, "bug") && !strings.Contains(query, "failure") && !strings.Contains(query, "error") {
		return false
	}
	if strings.Contains(query, "shell") || strings.Contains(query, "git status") || strings.Contains(query, "ls ") || strings.Contains(query, "pwd") {
		return false
	}
	return true
}

func cloneAskTaskContext(taskCtx *AskTaskContext) *AskTaskContext {
	if taskCtx == nil {
		return nil
	}
	cloned := *taskCtx
	if taskCtx.Domains != nil {
		cloned.Domains = append([]string(nil), taskCtx.Domains...)
	}
	if taskCtx.PhasePlan != nil {
		cloned.PhasePlan = append([]string(nil), taskCtx.PhasePlan...)
	}
	if taskCtx.CRUD != nil {
		cloned.CRUD = append([]string(nil), taskCtx.CRUD...)
	}
	cloned.DomainOfferNeeded = taskCtx.DomainOfferNeeded
	return &cloned
}

// normalizeDomainName normalizes domain names to standard values.
func normalizeDomainName(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	switch domain {
	case "store", "database", "db":
		return "stores"
	case "space", "kb", "knowledge":
		return "spaces"
	case "code", "coding", "repo", "repository":
		return "coding_workflow"
	case "shell", "terminal", "local", "file", "git":
		return "desktop"
	default:
		return domain
	}
}

// normalizePhase normalizes phase names to standard values.
func normalizePhase(phase string) string {
	phase = strings.ToLower(strings.TrimSpace(phase))
	validPhases := []string{"intake", "investigation", "planning", "execution", "review", "delivery"}
	for _, valid := range validPhases {
		if phase == valid {
			return phase
		}
	}
	// Default to investigation if invalid
	return "investigation"
}

// extractJSONFromMarkdown extracts JSON from markdown code blocks.
func extractJSONFromMarkdown(text string) string {
	// Try ```json ... ```
	if start := strings.Index(text, "```json"); start != -1 {
		start += 7
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// Try ``` ... ```
	if start := strings.Index(text, "```"); start != -1 {
		start += 3
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// Try to find JSON object boundaries
	if start := strings.Index(text, "{"); start != -1 {
		if end := strings.LastIndex(text, "}"); end != -1 && end > start {
			return strings.TrimSpace(text[start : end+1])
		}
	}

	return ""
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
