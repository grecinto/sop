package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/agent/parser"
	"github.com/sharedcode/sop/ai/agent/teampatterns"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/generator"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/ai/obfuscation"
	"github.com/sharedcode/sop/jsondb"
)

func (a *CopilotAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	log.Info("Using Ask pipeline (LLM-as-Orchestrator + Phase Factory)")
	return a.runAskPipeline(ctx, query, cfg)
}

// getOrCreateSessionQueue returns the session-level queue for workflow automation
func (a *CopilotAgent) getOrCreateSessionQueue() *WorkQueue {
	a.sessionQueueMu.Lock()
	defer a.sessionQueueMu.Unlock()

	if a.sessionQueue == nil {
		a.sessionQueue = NewWorkQueue()
		a.sessionQueue.SetMaxPerActor("coding_workflow", 10)
		a.sessionQueue.SetMaxPerActor("data_pipeline", 10)
	}

	return a.sessionQueue
}

// GetSessionQueue exposes the queue to capabilities/tools for enqueueing next steps
func (a *CopilotAgent) GetSessionQueue() *WorkQueue {
	return a.getOrCreateSessionQueue()
}

// AskParallel executes independent subqueries in parallel threads.
// Each thread gets its own isolated queue and execution context.
// All threads use PTY workers with unique session IDs for identical command execution behavior.
func (a *CopilotAgent) AskParallel(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	// 1. Detect parallelizable subqueries
	subQueries := detectParallelSubqueries(query)
	if len(subQueries) <= 1 {
		// Fall back to sequential execution
		return a.Ask(ctx, query, cfg)
	}

	log.Info("Parallel execution detected", "total_threads", len(subQueries))

	// 2. Launch each subquery as independent Ask() thread
	results := make([]string, len(subQueries))
	errors := make([]error, len(subQueries))
	var wg sync.WaitGroup

	defaultWorkingDir := resolveParallelWorkingDir(ctx, query)

	for i, subQuery := range subQueries {
		wg.Add(1)

		// Each thread gets its own PTY worker via unique session ID
		// Thread 0 = main session, Thread 1+ = parallel sessions
		threadCtx := ctx
		if i > 0 {
			threadCtx = context.WithValue(ctx, "parallel_execution_mode", true)

			// Create unique session ID for this parallel thread
			// Format: {base_session_id}_thread_{thread_number}
			baseSessionID := ""
			if payload, ok := ctx.Value("session_payload").(*ai.SessionPayload); ok && payload != nil {
				baseSessionID = payload.SessionID
			}
			if baseSessionID == "" {
				baseSessionID = "parallel"
			}
			threadSessionID := fmt.Sprintf("%s_thread_%d", baseSessionID, i)

			// Create new session payload for this thread
			threadPayload := &ai.SessionPayload{
				SessionID: threadSessionID,
			}
			if origPayload, ok := ctx.Value("session_payload").(*ai.SessionPayload); ok && origPayload != nil {
				// Copy other fields from original payload
				threadPayload.UserID = origPayload.UserID
				threadPayload.ClientID = origPayload.ClientID
				threadPayload.ActiveDomain = origPayload.ActiveDomain
				threadPayload.ActiveRepo = origPayload.ActiveRepo
			}
			threadCtx = context.WithValue(threadCtx, "session_payload", threadPayload)
		}

		normalizedSubQuery, explicitWorkingDir := extractParallelTaskWorkingDir(subQuery)
		if explicitWorkingDir != "" {
			threadCtx = context.WithValue(threadCtx, ctxKeyWorkingDir, explicitWorkingDir)
		} else if defaultWorkingDir != "" {
			threadCtx = context.WithValue(threadCtx, ctxKeyWorkingDir, defaultWorkingDir)
		}

		go func(idx int, q string, tCtx context.Context) {
			defer wg.Done()
			sessionID := ""
			if payload, ok := tCtx.Value("session_payload").(*ai.SessionPayload); ok && payload != nil {
				sessionID = payload.SessionID
			}
			log.Info("Parallel thread start", "thread", idx, "query", q, "session_id", sessionID, "working_dir", resolveParallelWorkingDir(tCtx, q))
			results[idx], errors[idx] = a.Ask(tCtx, q, cfg)
			log.Info("Parallel thread complete", "thread", idx, "session_id", sessionID, "error", errors[idx] != nil)
		}(i, normalizedSubQuery, threadCtx)
	}

	// 3. Wait for all threads to complete
	wg.Wait()

	// 4. Aggregate results
	return aggregateParallelResults(subQueries, results, errors), nil
}

// DetectParallelSubqueries exposes the parallel query splitter for shared runtime integrations.
func DetectParallelSubqueries(query string) []string {
	return detectParallelSubqueries(query)
}

// detectParallelSubqueries splits a query into independent parallel subqueries.
// Returns single-element slice if query is not parallelizable.
func detectParallelSubqueries(query string) []string {
	// Phase 4.1: Simple pattern matching
	// Detect common parallel patterns: "X and Y", "X, Y, and Z"

	lowerQuery := strings.ToLower(query)

	// Pattern 1: Comma-separated tasks with "and"
	// "run tests, check formatting, and build docs"
	// Handle this FIRST to avoid splitting on " and " incorrectly
	if strings.Contains(lowerQuery, ",") && strings.Contains(lowerQuery, " and ") {
		// Replace ", and" with "," for uniform splitting
		normalized := strings.ReplaceAll(query, ", and ", ", ")
		normalized = strings.ReplaceAll(normalized, " and ", ", ")
		parts := strings.Split(normalized, ",")
		if len(parts) >= 2 {
			subQueries := make([]string, len(parts))
			for i, part := range parts {
				subQueries[i] = strings.TrimSpace(part)
			}
			return subQueries
		}
	}

	// Pattern 2: "run X and Y" or "do X and Y" (without commas)
	if strings.Contains(lowerQuery, " and ") && !strings.Contains(lowerQuery, ",") {
		// Check for action verbs that indicate parallel tasks
		actionVerbs := []string{"run", "check", "execute", "build", "test", "lint", "format"}
		for _, verb := range actionVerbs {
			if strings.HasPrefix(lowerQuery, verb+" ") {
				// Split on " and "
				parts := strings.Split(query, " and ")
				if len(parts) >= 2 {
					subQueries := make([]string, len(parts))
					for i, part := range parts {
						subQueries[i] = strings.TrimSpace(part)
					}
					return subQueries
				}
			}
		}
	}

	// Pattern 3: Explicit parallel marker "||"
	// "run tests || check formatting"
	if strings.Contains(query, "||") {
		parts := strings.Split(query, "||")
		if len(parts) >= 2 {
			subQueries := make([]string, len(parts))
			for i, part := range parts {
				subQueries[i] = strings.TrimSpace(part)
			}
			return subQueries
		}
	}

	// Not parallelizable - return as single query
	return []string{query}
}

// aggregateParallelResults combines results from parallel threads.
func aggregateParallelResults(queries []string, results []string, errors []error) string {
	var combined strings.Builder

	combined.WriteString("✅ **Parallel Execution Complete**\n\n")

	hasErrors := false
	for _, err := range errors {
		if err != nil {
			hasErrors = true
			break
		}
	}

	for i := range results {
		combined.WriteString(fmt.Sprintf("### Task %d: %s\n\n", i+1, queries[i]))

		if errors[i] != nil {
			combined.WriteString(fmt.Sprintf("❌ **Error**: %v\n\n", errors[i]))
			hasErrors = true
		} else {
			combined.WriteString(results[i])
			combined.WriteString("\n\n")
		}

		if i < len(results)-1 {
			combined.WriteString("---\n\n")
		}
	}

	if hasErrors {
		combined.WriteString("\n⚠️ *Some tasks failed during parallel execution*\n")
	}

	return combined.String()
}

// isParallelExecution checks if the current context is for a parallel background thread.
func isParallelExecution(ctx context.Context) bool {
	val, ok := ctx.Value("parallel_execution_mode").(bool)
	return ok && val
}

func extractParallelTaskWorkingDir(query string) (string, string) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", ""
	}

	patterns := []string{" in ", " under ", " from ", " for ", " at ", " repo ", " workspace "}
	for _, pattern := range patterns {
		lower := strings.ToLower(trimmed)
		idx := strings.Index(lower, pattern)
		if idx < 0 {
			continue
		}
		candidate := strings.TrimSpace(trimmed[idx+len(pattern):])
		if candidate == "" {
			continue
		}
		parts := strings.Fields(candidate)
		if len(parts) == 0 {
			continue
		}
		first := strings.TrimSpace(parts[0])
		if first == "" {
			continue
		}
		if strings.Contains(first, "/") || strings.Contains(first, string(os.PathSeparator)) {
			queryWithoutDir := strings.TrimSpace(trimmed[:idx])
			return strings.TrimSpace(queryWithoutDir), first
		}
	}

	return trimmed, ""
}

func resolveParallelWorkingDir(ctx context.Context, query string) string {
	if _, workingDir := extractParallelTaskWorkingDir(query); workingDir != "" {
		return workingDir
	}

	if ctx != nil {
		if val, ok := ctx.Value(ctxKeyWorkingDir).(string); ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

// Helper functions for Ask modularization

func extractProviderDetails(cfg *ai.ConfigMap) *ProviderDetails {
	if cfg == nil {
		return nil
	}
	if po, ok := cfg.Get("provider_details"); ok {
		if override, ok := po.(*ProviderDetails); ok {
			return override
		}
	}
	return nil
}

func (a *CopilotAgent) noAPIKeyMessage() string {
	return "⚠️ **AI Copilot Disabled**: No valid API Key found.\n\nPlease open the **AI** dropdown in the top-left corner, choose your LLM provider/model, and add your API key there."
}

func (a *CopilotAgent) applyMetaTalkRewrite(ctx context.Context, query string) string {
	if rewrittenQuery, rewritten := a.tryMetaTalkBasedRouting(ctx, query); rewritten {
		log.Info("Copilot Ask MetaTalkBasedRouting Rewrite", "original_query", query, "effective_query", rewrittenQuery)
		return rewrittenQuery
	}
	return query
}

func (a *CopilotAgent) shouldDirectDisplayKB(taskContext *TaskContextClassification) bool {
	return taskContext.DirectDisplay && taskContext.KBSearchResults != ""
}

func (a *CopilotAgent) handleKBDirectDisplay(ctx context.Context, rawQuery string, intent string, taskContext *TaskContextClassification) (string, error) {
	log.Info("KB routing: Direct display path", "match_count", taskContext.KBMatchCount)
	a.trackEpisodeMetadata(ctx, intent)
	response := a.formatKBSearchResultsForDisplay(taskContext.KBSearchResults, taskContext.KBMatchCount)
	a.epilogueAndCleanup(ctx, rawQuery, intent, response, nil, nil, nil, nil, nil)
	return response, nil
}

func (a *CopilotAgent) resolveCapability(ctx context.Context, query string, taskContext *TaskContextClassification) (context.Context, capabilities.Capability) {
	routingState := capabilities.RoutingState{
		Domain:         taskContext.Domain,
		Gate:           taskContext.RoutingGate,
		CapabilityHint: taskContext.CapabilityHint,
	}

	var selectedCap capabilities.Capability
	if cap, found := a.capabilityRegistry.Resolve(query, routingState); found {
		selectedCap = cap
		ctx = context.WithValue(ctx, ctxKeySelectedCapability, cap)

		// If this is a Step, initialize it
		if step, isStep := cap.(cw.Step); isStep {
			ctx = context.WithValue(ctx, ctxKeyActiveStep, step)
			if err := step.OnStart(ctx, query, routingState); err != nil {
				log.Warn("Step OnStart failed", "error", err)
			}
		}
	}

	return ctx, selectedCap
}

func (a *CopilotAgent) enrichQueryWithKBResults(query string, taskContext *TaskContextClassification) string {
	if !taskContext.DirectDisplay && taskContext.KBSearchResults != "" {
		log.Info("KB routing: LLM processing path", "match_count", taskContext.KBMatchCount, "has_instruction", taskContext.LLMInstruction != "")
		cleanQuery := taskContext.CleanQuery
		if cleanQuery == "" {
			cleanQuery = query
		}
		return a.buildKBEnrichedQuery(cleanQuery, taskContext.KBSearchResults, taskContext.LLMInstruction, taskContext.KBMatchCount)
	}
	return query
}

func (a *CopilotAgent) notifySupportingPostTurnObservers(ctx context.Context, query string, routingState capabilities.RoutingState, selectedCap capabilities.Capability, result capabilities.CapabilityResult) {
	if a == nil || a.capabilityRegistry == nil {
		return
	}
	selectedID := ""
	if selectedCap != nil {
		selectedID = selectedCap.ID()
	}
	classified := a.capabilityRegistry.Classify(query, routingState)
	notified := make(map[string]struct{})
	for _, entry := range classified {
		cap := entry.Capability
		if cap == nil || entry.Result.Mode != capabilities.CapabilityModeSupporting {
			continue
		}
		if cap.ID() == selectedID {
			continue
		}
		if _, ok := notified[cap.ID()]; ok {
			continue
		}
		observer, ok := cap.(capabilities.PostTurnObserver)
		if !ok {
			continue
		}
		notified[cap.ID()] = struct{}{}
		if err := observer.ObserveAfterTurn(ctx, query, routingState, result); err != nil {
			log.Warn("Supporting post-turn observer failed", "capability", cap.ID(), "error", err)
		}
	}
}

func buildCapabilityResultData(toolCalls []ai.ToolCall, outcomeFacts []string, outcomeRecipes []ai.LearnedRecipe, carryoverState *ai.CarryoverState, escalationState map[string]any, clarificationRequest *ai.ClarificationRequest) map[string]any {
	return map[string]any{
		"tool_calls":            toolCalls,
		"outcome_facts":         outcomeFacts,
		"outcome_recipes":       outcomeRecipes,
		"carryover_state":       carryoverState,
		"escalation_state":      escalationState,
		"clarification_request": clarificationRequest,
	}
}

func (a *CopilotAgent) applyAskConfig(ctx context.Context, cfg *ai.ConfigMap) context.Context {
	if cfg != nil {
		if val, ok := cfg.Get("default_format"); ok {
			if format, ok := val.(string); ok && strings.TrimSpace(format) != "" {
				ctx = context.WithValue(ctx, ai.CtxKeyDefaultFormat, strings.TrimSpace(format))
			}
		}
		if val, ok := cfg.Get("thinking_level"); ok {
			if level, ok := val.(string); ok && strings.TrimSpace(level) != "" {
				ctx = context.WithValue(ctx, ctxKeyThinkingLevel, strings.TrimSpace(level))
			}
		}
		if val, ok := cfg.Get("is_new_topic"); ok {
			if isNewTopic, ok := val.(bool); ok {
				ctx = context.WithValue(ctx, ai.CtxKeyIsNewTopic, isNewTopic)
			}
		}
	}
	return ctx
}

// ============================================================================
// KB-DRIVEN WORKFLOW FUNCTIONS REMOVED (2026-07-30)
// ============================================================================
// The following functions were removed because KB-driven workflows have been
// deprecated in favor of the Phase system. Workflows are STATIC and reduce
// LLM orchestration capability, resulting in low success rates.
//
// Removed functions:
//   - tryKBWorkflowExecution() - KB workflow matching and execution
//   - tryDetectWorkflow() - Workflow detection
//   - promoteWorkflowToFederation() - Workflow promotion to Core Playbook
//   - countSuccessfulTurns() - Helper function
//   - extractActorsUsed() - Helper function
//
// Replacement: Use Phase system (PlannerCapability + phase capabilities)
// for dynamic, LLM-orchestrated workflows.
// ============================================================================

// ============================================================================
// CONTROLLER PATTERN FUNCTIONS REMOVED (2026-07-29)
// ============================================================================
// The following functions were removed because they implemented Controller Pattern,
// which violates the Participant Pattern architecture of the Ask Domain.
//
// Removed functions:
//   - executePhasePlan() - Controller loop executing phases
//   - executePhaseWithRecovery() - Retry controller
//   - applyCheckpointReassessment() - Checkpoint controller logic
//   - executePhase() - Phase executor
//   - getPhaseCapability() - Helper function
//
// Correct Pattern (Participant):
//   - Phase capture tools read succession path from WorkStream
//   - Each capture advances CurrentIndex
//   - Each capture enqueues NEXT phase only
//   - Ask pipeline is the ONLY controller
//
// See: /memories/repo/participant-pattern-not-controller.md
// ============================================================================

func isExplicitNewTopic(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if isNewTopic, ok := ctx.Value(ai.CtxKeyIsNewTopic).(bool); ok {
		return isNewTopic
	}
	return false
}

func (a *CopilotAgent) recordSessionBoundary(ctx context.Context, kind string, summary string, details string) {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return
	}
	a.service.session.Memory.RecordSessionBoundary(SessionBoundaryKind(kind), summary, details, "ask")
}

func (a *CopilotAgent) logAskStart(ctx context.Context, query string, gen ai.Generator) string {
	var sessionID string
	var currentDB string
	var activeDomain string
	if p := ai.GetSessionPayload(ctx); p != nil {
		sessionID = p.SessionID
		currentDB = p.CurrentDB
		activeDomain = p.ActiveDomain
	}
	currentThreadID := ""
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if thread := a.service.session.Memory.GetCurrentThread(); thread != nil {
			currentThreadID = fmt.Sprintf("%v", thread.ID)
		}
		a.recordSessionBoundary(ctx, string(SessionBoundaryStart), "Session started", fmt.Sprintf("query=%s", truncateString(query, 220)))
	}
	//log.Info("Copilot Ask start", "query", query, "session_id", sessionID)
	log.Info("Copilot Ask Start", "generator", gen.Name(), "default_format", getRequestedOutputFormat(ctx), "session_id", sessionID,
		"provider_runtime", providerRuntimeCapabilitySummary(gen),
		"current_db", currentDB, "active_domain", activeDomain, "thread_id", currentThreadID, "query_chars", len(query))
	return sessionID
}

func (a *CopilotAgent) tryMetaTalkBasedRouting(ctx context.Context, query string) (string, bool) {
	if rewrittenQuery, rewritten := a.rewriteRetryMetaAsk(ctx, query); rewritten {
		return rewrittenQuery, true
	}
	if rewrittenQuery, rewritten := a.rewriteConversationalMetaAsk(ctx, query); rewritten {
		return rewrittenQuery, true
	}
	return "", false
}

func (a *CopilotAgent) rewriteRetryMetaAsk(ctx context.Context, query string) (string, bool) {
	if !isRetryMetaAsk(query) {
		return query, false
	}

	a.rehydrateMRUFromMemory(ctx)
	resolved, ok := a.latestAskOutcomeQuery()
	if !ok || strings.TrimSpace(resolved) == "" {
		return query, false
	}

	if p := ai.GetSessionPayload(ctx); p != nil {
		p.RetryRewriteState = &ai.RetryRewriteState{
			OriginalQuery: query,
			ResolvedQuery: resolved,
			Status:        "resolved",
		}
		p.CurrentUserQuery = resolved
	}

	return resolved, true
}

func (a *CopilotAgent) rewriteConversationalMetaAsk(ctx context.Context, query string) (string, bool) {
	if !isLikelyMetaConversationFollowUp(query) {
		return query, false
	}

	a.rehydrateMRUFromMemory(ctx)
	question, targetQuery, ok := a.pendingOrAnchoredClarification(ctx)
	if !ok {
		return query, false
	}

	clarificationDetails := ""
	if p := ai.GetSessionPayload(ctx); p != nil {
		if p.ClarificationState == nil {
			p.ClarificationState = &ai.ClarificationState{}
		}
		if missingFact := strings.TrimSpace(p.ClarificationState.MissingFact); missingFact != "" {
			clarificationDetails += "\nMissing fact: " + missingFact
		}
		if reason := strings.TrimSpace(p.ClarificationState.Reason); reason != "" {
			clarificationDetails += "\nClarification reason: " + reason
		}
		if source := strings.TrimSpace(p.ClarificationState.Source); source != "" {
			clarificationDetails += "\nClarification source: " + source
		}
		effective := fmt.Sprintf("Target ask: %s\nAssistant clarification question: %s%s\nUser clarification: %s\nAnswer the clarification using the target ask context, then continue the target ask if the clarification resolves it.", targetQuery, question, clarificationDetails, strings.TrimSpace(query))
		p.ClarificationState.TargetQuery = targetQuery
		p.ClarificationState.AssistantQuestion = question
		if strings.TrimSpace(p.ClarificationState.OriginalUserQuery) == "" {
			p.ClarificationState.OriginalUserQuery = targetQuery
		}
		p.ClarificationState.UserClarification = strings.TrimSpace(query)
		p.ClarificationState.EffectiveResumeAsk = effective
		p.ClarificationState.Status = "resolved"
		p.CurrentUserQuery = effective
		return effective, true
	}

	effective := fmt.Sprintf("Target ask: %s\nAssistant clarification question: %s\nUser clarification: %s\nAnswer the clarification using the target ask context, then continue the target ask if the clarification resolves it.", targetQuery, question, strings.TrimSpace(query))
	return effective, true
}

func (a *CopilotAgent) pendingOrAnchoredClarification(ctx context.Context) (string, string, bool) {
	if p := ai.GetSessionPayload(ctx); p != nil && p.ClarificationState != nil {
		state := p.ClarificationState
		if strings.EqualFold(strings.TrimSpace(state.Status), "pending") && strings.TrimSpace(state.AssistantQuestion) != "" && strings.TrimSpace(state.TargetQuery) != "" {
			return strings.TrimSpace(state.AssistantQuestion), strings.TrimSpace(state.TargetQuery), true
		}
	}
	return a.latestMetaConversationAnchor(ctx)
}

func (a *CopilotAgent) latestMetaConversationAnchor(ctx context.Context) (string, string, bool) {
	if a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return "", "", false
	}
	thread := a.service.session.Memory.GetCurrentThread()
	if thread == nil || len(thread.Exchanges) == 0 {
		return "", "", false
	}
	last := thread.Exchanges[len(thread.Exchanges)-1]
	if last.Role != RoleAssistant || !isLikelyMetaQuestion(last.Content) {
		return "", "", false
	}
	targetQuery, ok := a.latestAskOutcomeQuery()
	if !ok || strings.TrimSpace(targetQuery) == "" {
		for i := len(thread.Exchanges) - 2; i >= 0; i-- {
			if thread.Exchanges[i].Role == RoleUser && strings.TrimSpace(thread.Exchanges[i].Content) != "" {
				targetQuery = strings.TrimSpace(thread.Exchanges[i].Content)
				ok = true
				break
			}
		}
	}
	if !ok || strings.TrimSpace(targetQuery) == "" {
		return "", "", false
	}
	return strings.TrimSpace(last.Content), strings.TrimSpace(targetQuery), true
}

func (a *CopilotAgent) latestAskOutcomeQuery() (string, bool) {
	item, ok := a.findMRUItem(askOutcomeMRUCategoryQuery, MRUSourceAskOutcome, true)
	if !ok || normalizeMRUScope(item.Scope) != MRUScopeSession {
		return "", false
	}
	query := trimMRUPrefix(item.Context)
	query = strings.TrimSpace(strings.TrimPrefix(query, "Last user ask:"))
	query = strings.TrimSpace(query)
	if query == "" {
		return "", false
	}
	return query, true
}

func isRetryMetaAsk(query string) bool {
	normalized := normalizeRetryMetaAskPhrase(query)
	if normalized == "" {
		return false
	}
	known := map[string]struct{}{
		"retry same ask":              {},
		"retry the same ask":          {},
		"can we retry same ask":       {},
		"can we retry the same ask":   {},
		"could we retry same ask":     {},
		"could we retry the same ask": {},
		"repeat same ask":             {},
		"repeat the same ask":         {},
		"can we repeat same ask":      {},
		"can we repeat the same ask":  {},
		"rerun same ask":              {},
		"rerun the same ask":          {},
		"run same ask again":          {},
		"run the same ask again":      {},
	}
	if _, ok := known[normalized]; ok {
		return true
	}

	hasSameAsk := strings.Contains(normalized, "same ask") || strings.Contains(normalized, "previous ask")
	if !hasSameAsk {
		return false
	}
	for _, marker := range []string{"retry", "repeat", "rerun", "run again", "try again"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func normalizeRetryMetaAskPhrase(query string) string {
	line := strings.TrimSpace(lastNonEmptyQueryLine(query))
	line = strings.TrimSpace(strings.Trim(line, ".!?"))
	line = strings.ToLower(line)
	return strings.Join(strings.Fields(line), " ")
}

func isLikelyMetaConversationFollowUp(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") {
		return false
	}
	if strings.HasSuffix(trimmed, "?") {
		return true
	}
	if len(trimmed) > 280 {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"yes", "no", "use ", "prefer ", "it is ", "it's ", "flatten", "nested", "only ", "just ", "the ", "this ", "that ", "those ", "these ", "keep ", "make it ", "return ", "show ", "remove ", "add ", "continue ", "proceed ", "go with ", "let's use ", "lets use ", "i want ", "we want "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, marker := range []string{"flat", "flattened", "nested", "dotted", "same ask", "hardcoded queries", "on demand", "that shape", "those fields", "that option", "the first option", "the second option"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isLikelyMetaQuestion(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.Contains(trimmed, "?") {
		return false
	}
	normalized := normalizeMetaQuestionText(trimmed)
	for _, marker := range []string{
		"do you mean",
		"are you asking",
		"should i",
		"should we",
		"should i continue",
		"should i proceed",
		"should we continue",
		"would you like",
		"do you want",
		"do you want me to",
		"which",
		"which output shape",
		"which fields",
		"what kind",
		"what should",
		"what format",
		"can you clarify",
		"can you confirm",
		"is your goal",
		"is the goal",
		"before i proceed",
		"before i continue",
		"to answer this correctly",
		"to continue correctly",
		"so i can continue",
		"how would you like",
		"how should",
		"is it",
		"nested",
		"flattened",
		"flat",
		"projection",
		"join outputs",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func normalizeMetaQuestionText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer("\n", " ", "\t", " ", ",", " ", ":", " ", ";", " ", "(", " ", ")", " ", "`", " ")
	text = replacer.Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func (a *CopilotAgent) handleDirectToolInvocation(ctx context.Context, query string, gen ai.Generator) (bool, string, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false, "", nil
	}

	a.ensureCapabilityRegistry()

	// Try capability-based prefix routing first
	if a.capabilityRegistry != nil {
		if handled, result, err := a.capabilityRegistry.TryPrefixRouting(ctx, trimmed); handled {
			return true, result, err
		}
	}

	if strings.HasPrefix(trimmed, "/") {
		// Register tools for local command parsing
		a.registerTools(ctx)
		handled, res, err := a.handleSlashCommand(ctx, query, gen)
		if handled {
			if err != nil {
				return true, res, err
			}
			return true, res, nil
		}
	}
	return false, "", nil
}

func (a *CopilotAgent) handlePendingUserConfirmation(ctx context.Context, query string) (bool, string, error) {
	if a.service == nil || a.service.session == nil {
		return false, "", nil
	}

	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false, "", nil
	}

	session := a.service.session
	session.mu.Lock()

	if pending := session.PendingConfirmation; pending != nil {
		if isAffirmativeConfirmation(trimmed) {
			session.PendingConfirmation = nil
			session.mu.Unlock()
			args := map[string]any{"kb_name": pending.SpaceName}
			if pending.DatabaseName != "" {
				args["database"] = pending.DatabaseName
			}
			res, err := a.toolDeleteSpace(ctx, args)
			if err == nil {
				res = "[[CLEAR_PENDING_CONFIRMATION]]\n" + res
			}
			return true, res, err
		}
		if isNegativeConfirmation(trimmed) {
			session.PendingConfirmation = nil
			session.mu.Unlock()
			return true, fmt.Sprintf("[[CLEAR_PENDING_CONFIRMATION]]\nCancelled deletion of Space '%s'.", pending.SpaceName), nil
		}
		session.mu.Unlock()
		return true, fmt.Sprintf("Pending deletion confirmation for Space '%s'. Reply 'yes' to confirm or 'no' to cancel.", pending.SpaceName), nil
	}

	spaceName, ok := parseDeleteSpaceRequest(trimmed)
	if !ok {
		session.mu.Unlock()
		return false, "", nil
	}

	dbName := ""
	if p := ai.GetSessionPayload(ctx); p != nil {
		dbName = p.CurrentDB
	}
	session.PendingConfirmation = &PendingUserConfirmation{
		Kind:         "delete_space",
		SpaceName:    spaceName,
		DatabaseName: dbName,
	}
	session.mu.Unlock()
	if dbName != "" {
		return true, fmt.Sprintf("Delete Space '%s' from database '%s'? Reply 'yes' to confirm or 'no' to cancel.", spaceName, dbName), nil
	}
	return true, fmt.Sprintf("Delete Space '%s'? Reply 'yes' to confirm or 'no' to cancel.", spaceName), nil
}

func parseDeleteSpaceRequest(query string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimRight(lastNonEmptyQueryLine(query), ".!?"))
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	patterns := []string{
		"delete space ",
		"delete the space ",
		"remove space ",
		"remove the space ",
		"delete knowledge base ",
		"delete the knowledge base ",
		"remove knowledge base ",
		"remove the knowledge base ",
		"delete kb ",
		"remove kb ",
	}
	for _, pattern := range patterns {
		idx := strings.Index(lower, pattern)
		if idx >= 0 {
			name := strings.TrimSpace(trimmed[idx+len(pattern):])
			name = strings.TrimPrefix(name, ":")
			name = strings.TrimSpace(name)
			name = strings.Trim(name, "\"'`")
			if cut := strings.IndexAny(name, ".!?"); cut >= 0 {
				name = strings.TrimSpace(name[:cut])
			}
			if name != "" {
				return name, true
			}
		}
	}

	reversePrefixes := []string{"delete ", "remove "}
	reverseSuffixes := []string{" space", " kb", " knowledge base"}
	for _, prefix := range reversePrefixes {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		remainder := strings.TrimSpace(trimmed[len(prefix):])
		lowerRemainder := strings.ToLower(remainder)
		for _, suffix := range reverseSuffixes {
			if !strings.HasSuffix(lowerRemainder, suffix) {
				continue
			}
			name := strings.TrimSpace(remainder[:len(remainder)-len(suffix)])
			name = strings.Trim(name, "\"'`")
			if cut := strings.IndexAny(name, ".!?"); cut >= 0 {
				name = strings.TrimSpace(name[:cut])
			}
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

func isAffirmativeConfirmation(query string) bool {
	switch confirmationReplyToken(query) {
	case "yes", "y", "confirm", "confirmed", "delete", "delete it", "do it", "proceed":
		return true
	default:
		return false
	}
}

func isNegativeConfirmation(query string) bool {
	switch confirmationReplyToken(query) {
	case "no", "n", "cancel", "abort", "stop", "don't", "do not":
		return true
	default:
		return false
	}
}

func confirmationReplyToken(query string) string {
	trimmed := strings.TrimSpace(lastNonEmptyQueryLine(query))
	if trimmed == "" {
		return ""
	}

	return strings.ToLower(trimmed)
}

func lastNonEmptyQueryLine(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}

	return ""
}

func isGeneralTaskContext(taskClassification *TaskContextClassification) bool {
	if taskClassification == nil {
		return false
	}
	return taskClassification.GeneralContext
}

func (a *CopilotAgent) buildGeneralTaskContext(ctx context.Context, query string, fallback *TaskContextClassification) *TaskContextClassification {
	generalTaskCtx := &TaskContextClassification{Domain: "General", GeneralContext: true}
	if fallback != nil {
		generalTaskCtx.Entity = fallback.Entity
		generalTaskCtx.DBArtifacts = append([]string(nil), fallback.DBArtifacts...)
		generalTaskCtx.StoresArtifacts = append([]string(nil), fallback.StoresArtifacts...)
		generalTaskCtx.SpacesArtifacts = append([]string(nil), fallback.SpacesArtifacts...)
		generalTaskCtx.Layers = append([]LayerInfo(nil), fallback.Layers...)
		generalTaskCtx.ScriptAuthoring = fallback.ScriptAuthoring
		if strings.TrimSpace(fallback.RoutingGate) != "" {
			generalTaskCtx.RoutingGate = fallback.RoutingGate
		} else {
			generalTaskCtx.RoutingGate = RoutingGateContinuity
		}
	} else {
		generalTaskCtx.RoutingGate = RoutingGateContinuity
	}
	annotateTaskContextIntent(generalTaskCtx, query)
	a.persistRoutingState(ctx, generalTaskCtx)
	return generalTaskCtx
}

func (a *CopilotAgent) evaluateRoutingGates(ctx context.Context, query string, gen ai.Generator) (*TaskContextClassification, error) {
	log.Info("Copilot Ask routing evaluation start", "query", query)
	a.rehydrateMRUFromMemory(ctx)
	isTest := isRoutingTestGenerator(gen)
	anchor := parseRoutingAnchor(query)

	// Fast-path: Check for specialized KB routing patterns (e.g., "sop", "sop:category")
	// before falling through to the standard three-gate flow
	if looksLikeSpecializedRoutingQuery(query) && anchor == nil {
		if handledTaskCtx, handled, err := a.trySpecializedFocusedRouting(ctx, query, "Omni", "Spaces", ""); err != nil {
			return nil, err
		} else if handled {
			return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: handledTaskCtx, matched: true, source: "specialized"}, RoutingGateFocused), nil
		}
	}

	if taskClassification, err := a.tryPrefixBasedRouting(ctx, query, gen, isTest, anchor); err != nil {
		return nil, err
	} else if taskClassification != nil {
		return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: taskClassification, matched: true, source: "prefix"}, RoutingGateFocused), nil
	}

	if !isExplicitNewTopic(ctx) {
		if taskClassification, err := a.tryAskContinuationBasedRouting(ctx, query, gen, isTest, anchor); err != nil {
			return nil, err
		} else if taskClassification != nil {
			return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: taskClassification, matched: true, source: "continuation"}, RoutingGateContinuity), nil
		}
	} else {
		log.Info("Explicit new-topic override: skipping continuation-based routing")
	}

	if taskClassification, err := a.tryColdStartBasedRouting(ctx, query, gen, isTest); err != nil {
		return nil, err
	} else if taskClassification != nil {
		return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: taskClassification, matched: true, source: "coldstart"}, RoutingGateDiscovery), nil
	}

	return nil, nil
}

func (a *CopilotAgent) trackEpisodeMetadata(ctx context.Context, intent string) {
	// Determine current target KB
	currentKBTrack := "sop"
	if p := ai.GetSessionPayload(ctx); p != nil && len(p.SelectedKBs) > 0 {
		var names []string
		for _, kb := range p.SelectedKBs {
			names = append(names, kb.Name)
		}
		currentKBTrack = "sop," + strings.Join(names, ",")
	}

	// --- DYNAMIC MRU INJECTION FROM PREVIOUS EPISODE ---
	if a.service != nil && a.service.session != nil {
		if a.service.session.Memory != nil {
			thread := a.service.session.Memory.GetCurrentThread()
			if thread != nil && len(thread.Exchanges) > 0 {
				lastExchange := thread.Exchanges[len(thread.Exchanges)-1]

				// Inspect the prior Q&A exchange (episode metadata)
				// If the interaction resolves to the same Entity and same Knowledge Base...
				if lastExchange.Entity == intent && lastExchange.ActiveKB == currentKBTrack {
					// Auto-inject the matching contexts into MRU
					kbs := strings.Split(currentKBTrack, ",")
					for _, kb := range kbs {
						kb = strings.TrimSpace(kb)
						if kb != "" {
							a.markMRUCategoryWithSource(playbookMRUCategory(kb), "", MRUSourcePlaybook)
						}
					}
				}
			}
		}
	}
}

func (a *CopilotAgent) reactLoop(ctx context.Context, query string, gen ai.Generator, fullPrompt string) (string, []ai.ToolCall, []string, []ai.LearnedRecipe, *ai.CarryoverState, map[string]any, *ai.ClarificationRequest, error) {
	currentDB := ""
	if p := ai.GetSessionPayload(ctx); p != nil {
		currentDB = p.CurrentDB
	}

	// Obfuscate Prompt if enabled
	if a.shouldObfuscate(currentDB) {
		fullPrompt = obfuscation.GlobalObfuscator.ObfuscateText(fullPrompt)
	}

	// Active Implementation: Native Tools (API-level tool calling)
	engine := &CompatibleReActEngine{
		EnableObfuscation: a.shouldObfuscate(currentDB),
	}

	req := ai.ReasoningRequest{
		SystemPrompt:  fullPrompt, // For baseline, this contains the full aggregated state
		UserQuery:     query,
		Executor:      a, // CopilotAgent implements Executor
		Generator:     gen,
		HydrationSink: a.buildProviderLoopMRUSink(),
	}

	// Wire up the HTTP event streamer so provider-owned ReAct loops (e.g. ChatGPT
	// Responses API) can emit tool_result / assistant_message events directly to the UI.
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		req.Streamer = streamer
	}

	req.Verbose = effectiveVerbose(ctx)

	// Forward the persisted carryover state so provider-owned loops can use server-side
	// continuation (e.g. GPT's previous_response_id). Without this, CarryoverState is nil
	// on every ask and live cross-ask carryover never takes effect on this path.
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if storedState := a.service.session.Memory.GetCarryoverState(); storedState != nil {
			req.CarryoverMode = storedState.Mode
			req.CarryoverState = storedState
		}
	}

	// For Stores asks, inject an execution directive into ContextText so it appears in
	// the model's input turn (not just in the instructions JSON blob). Provider-owned
	// loops (e.g. GPT Responses API) pass ContextText as part of the user message, which
	// is harder for the model to override than background system instructions.
	// ForceToolCall additionally sets tool_choice=required at the API level so the model
	// cannot emit a conversational text response instead of a function call.
	if routingState := activeRoutingState(ctx); routingState != nil {
		if directive := buildStoresExecutionDirective(routingState); directive != "" {
			req.ContextText = directive
			req.ForceToolCall = true
		}
	}

	resp, err := engine.Run(ctx, req)
	if err != nil {
		return "", nil, nil, nil, nil, nil, nil, err
	}

	finalText := resp.FinalText
	if a.shouldObfuscate(currentDB) {
		finalText = obfuscation.GlobalObfuscator.DeobfuscateText(finalText)
	}

	return finalText, resp.ToolCalls, resp.OutcomeFacts, resp.OutcomeRecipes, resp.CarryoverState, resp.EscalationState, resp.ClarificationRequest, nil
}

func splitProviderAndModel(provider, model string) (string, string) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return "", model
	}
	if providerPart, modelPart, ok := strings.Cut(provider, ":"); ok && strings.TrimSpace(modelPart) != "" && model == "" {
		provider = strings.TrimSpace(providerPart)
		model = strings.TrimSpace(modelPart)
	}
	return provider, model
}

// ----------------------------------------------------------------------------
// HELPER METHODS
// ----------------------------------------------------------------------------

func (a *CopilotAgent) resolveLLM(ctx context.Context, providerOverride *ProviderDetails) ai.Generator {
	// Use explicit ProviderDetails parameter first
	if providerOverride != nil && providerOverride.Provider != "" {
		provider, resolvedModel := splitProviderAndModel(providerOverride.Provider, providerOverride.Model)
		if provider == "openai" {
			provider = ProviderChatGPT
		}

		if providerOverride.Model == "" {
			if entry, ok := ai.GetModelCatalogRegistry().SelectBest(ai.ModelSelectionRequest{Provider: provider}); ok {
				resolvedModel = entry.Model
			}
		}

		var err error
		var tempGen ai.Generator

		switch provider {
		case ProviderGemini:
			if providerOverride.APIKey != "" {
				model := resolvedModel
				if model == "" {
					model = DefaultModelGemini
				}
				tempGen, err = generator.New(ProviderGemini, map[string]any{
					"api_key": providerOverride.APIKey,
					"model":   model,
				})
			}
		case ProviderChatGPT:
			if providerOverride.APIKey != "" {
				model := resolvedModel
				if model == "" {
					model = DefaultModelOpenAI
				}
				options := map[string]any{
					"api_key": providerOverride.APIKey,
					"model":   model,
				}
				if providerOverride.BaseURL != "" {
					options["api_url"] = providerOverride.BaseURL
				}
				tempGen, err = generator.New(ProviderChatGPT, options)
			}
		case ProviderAnthropic:
			if providerOverride.APIKey != "" {
				model := resolvedModel
				if model == "" {
					model = DefaultModelAnthropic
				}
				tempGen, err = generator.New(ProviderAnthropic, map[string]any{
					"api_key": providerOverride.APIKey,
					"model":   model,
				})
			}
		}

		if err == nil && tempGen != nil {
			return tempGen
		}
	}

	// Fall back to context for backward compatibility
	return a.resolveGenerator(ctx)
}

func (a *CopilotAgent) resolveGenerator(ctx context.Context) ai.Generator {
	gen := a.brain
	providerOverrideStr, ok := ctx.Value(ai.CtxKeyProvider).(string)
	if !ok || providerOverrideStr == "" {
		return gen
	}
	if strings.Contains(providerOverrideStr, ":") {
		provider, model, _ := strings.Cut(providerOverrideStr, ":")
		if strings.TrimSpace(model) == "" {
			if entry, ok := ai.GetModelCatalogRegistry().SelectBest(ai.ModelSelectionRequest{Provider: provider}); ok {
				providerOverrideStr = provider + ":" + entry.Model
			}
		}
	}
	var modelOverride string
	if provider, model, ok := strings.Cut(providerOverrideStr, ":"); ok {
		providerOverrideStr = provider
		modelOverride = model
	}
	providerOverrideStr, modelOverride = splitProviderAndModel(providerOverrideStr, modelOverride)
	if providerOverrideStr == "openai" {
		providerOverrideStr = ProviderChatGPT
	}

	var err error
	var tempGen ai.Generator

	customAPIKey, _ := ctx.Value(ai.CtxKeyAPIKey).(string)
	customBaseURL, _ := ctx.Value(ai.CtxKeyBaseURL).(string)

	switch providerOverrideStr {
	case ProviderGemini:
		if customAPIKey != "" {
			model := modelOverride
			if model == "" {
				model = DefaultModelGemini
			}
			tempGen, err = generator.New(ProviderGemini, map[string]any{
				"api_key": customAPIKey,
				"model":   model,
			})
		}
	case ProviderChatGPT:
		if customAPIKey != "" {
			model := modelOverride
			if model == "" {
				model = DefaultModelOpenAI
			}
			options := map[string]any{
				"api_key": customAPIKey,
				"model":   model,
				"api_url": customBaseURL,
			}
			tempGen, err = generator.New(ProviderChatGPT, options)
		}
	case ProviderOllama:
		model := modelOverride
		if model == "" {
			model = DefaultModelOllama
		}
		host := customBaseURL
		if host == "" {
			host = os.Getenv(EnvOllamaHost)
		}
		if host == "" {
			host = DefaultHost
		}
		tempGen, err = generator.New(ProviderOllama, map[string]any{
			"base_url": host,
			"model":    model,
		})
	}

	if err == nil && tempGen != nil {
		gen = tempGen
	} else {
		log.Warn("Failed to resolve to valid provider", "provider", providerOverrideStr, "error", err)
	}

	return gen
}

func (a *CopilotAgent) handleSlashCommand(ctx context.Context, query string, gen ai.Generator) (bool, string, error) {
	cmdLine := strings.TrimSpace(query)[1:]
	toolName, args, err := parser.ParseSlashCommand(cmdLine)

	if err == nil && (toolName == "get_verbose" || toolName == "verbose" || toolName == "v") {
		rs := runnerSessionFromContext(ctx)
		if rs == nil {
			return true, "", fmt.Errorf("no runner session found")
		}
		status := "OFF"
		if rs.IsVerbose() {
			status = "ON"
		}
		return true, fmt.Sprintf("Verbose mode: **%s**", status), nil
	}

	if err == nil && (toolName == "clear_caveats" || toolName == "clear_guidance" || toolName == "clear_runtime_caveats") {
		if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
			categoryRoot := ""
			if v, ok := args["category"]; ok {
				categoryRoot = fmt.Sprint(v)
			}
			cleared := a.service.session.Memory.ClearRuntimeCaveats(categoryRoot)
			return true, fmt.Sprintf("Cleared %d runtime caveat(s).", cleared), nil
		}
		return true, "No active runtime memory available to clear.", nil
	}

	if err == nil && toolName != "" {
		res, execErr := a.Execute(ctx, toolName, args)
		if execErr != nil {
			if strings.Contains(execErr.Error(), "unknown tool") {
				if gen != nil {
					log.Warn("Slash command failed locally (unknown tool), falling back to LLM", "tool", toolName, "error", execErr)
					return false, "", nil // Handled by LLM
				}
				return true, fmt.Sprintf("Error executing command '%s' (and no AI Copilot available to interpret it): %v", toolName, execErr), fmt.Errorf("tool fallback failed")
			}
			return true, fmt.Sprintf("Tool '%s' failed.", toolName), fmt.Errorf("tool execution failed")
		}
		return true, res, nil
	}
	return false, "", nil
}

func (a *CopilotAgent) resolvePersona(ctx context.Context) string {
	persona, _ := a.resolvePersonaWithMetadata(ctx)
	return persona
}

func personaSourceCacheKey(agentID string) string {
	return personaSourceMRUCategoryPrefix + agentID
}

func (a *CopilotAgent) resolvePersonaWithMetadata(ctx context.Context) (string, bool) {
	agentID := ai.AgentIDOmni
	cacheKey := "PERSONA_" + agentID
	sourceKey := personaSourceCacheKey(agentID)

	// 1. Try MRU Cache
	if cachedVal, ok := a.getMRUCategoryBySource(cacheKey, MRUSourcePersona, true); ok && cachedVal != "" {
		personaSource, _ := a.getMRUCategoryBySource(sourceKey, MRUSourcePersona, true)
		return cachedVal, strings.EqualFold(strings.TrimSpace(personaSource), "kb")
	}

	persona := ""
	personaFromKB := false
	// buildSystemPrompt is the Omni supervisor path. Avatar persona loading stays in avatar.go.
	if a.systemDB != nil {
		tempDB := database.NewDatabase(a.systemDB.Config())
		if tx, err := tempDB.BeginTransaction(ctx, sop.ForReading); err == nil {
			if kb, err := tempDB.OpenKnowledgeBase(ctx, ai.DefaultKBName, tx, nil, nil, false, false); err == nil {
				if cfg, err := kb.GetConfig(ctx); err == nil && cfg != nil && cfg.SystemPrompt != "" {
					persona = cfg.SystemPrompt + "\n\n"
					personaFromKB = true
				}
			}
			tx.Rollback(ctx)
		}
	}

	if persona == "" {
		persona = "You are a general-purpose intelligent AI Copilot equipped with a human-like 'active memory' system. " +
			"Your primary expertise is in a platform named SOP (which stands for 'Scalable Objects Persistence'). " +
			"SOP is an advanced data and AI platform that provides robust tooling for Knowledge Bases (KBs). " +
			"You aid users in SOP library adoption, technology integration, Spaces management, and data management. " +
			"As an expert in Scalable Objects Persistence, your core knowledge covers Databases, B-Trees, strict ACID Transactions, Swarm Computing, and advanced Storage mechanisms including Erasure Coding. " +
			"You understand that in this platform, a 'Space' or 'Knowledge Base' is a new AI memory subsystem combining VectorDB, Text Search, and a specialized schema (Thoughts: Category/Items), and you manage it differently than raw technical tables. " +
			"You have deep expertise in SOP scripting (AST-based execution), and the SOP HTTP API, covering request/response lifecycles, NDJSON streaming, and session management. " +
			"You derive your foundational knowledge, codebase context, and architectural principles directly from the source repository at https://github.com/sharedcode/sop. " +
			"Assist users dynamically with ANY open-ended request—whether answering general questions, creating and consulting Knowledge Bases, writing code, or managing database queries using the tools provided.\n\n"
	}

	persona += "CRITICAL SYSTEM GUARDRAIL:\n" +
		"1. Autonomous Research: You are an autonomous intelligent entity. You have implicit permission and are EXPECTED to use your 'Read' tools (e.g. Search KB, List/Query DB) to actively research Domains, Spaces (SOP or custom KBs), and codebase schemas as knowledge references. DO NOT proceed blindly if you lack context.\n" +
		"2. Disambiguation: If a user's request is ambiguous or lacks constraints, DO NOT guess or hallucinate parameters. Use your search tools to find relevant constraints first. If self-research fails, halt execution and explicitly consult the user for clarification. Ask one short direct clarification question that starts with a recognizable lead such as 'Do you want...', 'Which...', 'Is your goal...', or 'Before I proceed...'. Keep the clarification question specific to the unresolved choice.\n" +
		"3. Multi-Step Tool Chaining: When tools are available, EXECUTE tasks with tools rather than just describing what should be done. For multi-step tasks (git commit, file edits, data queries, etc.), chain tools together until the task is complete. After each tool result, evaluate: 'Do I need another tool to complete this task, or can I now provide the final answer?' Examples: 'check in files' requires git_status → git_commit → confirm; 'find and fix bug' requires read_file → patch_file → validate; 'query data' may require list_stores → execute_script → analyze results. Do not stop after the first tool if the task requires multiple steps to fully complete.\n" +
		"4. Git Workflow Efficiency: For git operations, follow these best practices:\n" +
		"   • Call git_status ONCE at the start to assess the working tree\n" +
		"   • Identify relevant vs temporary files (e.g., project code vs binaries, build artifacts, untracked docs)\n" +
		"   • Use git_diff strategically only when you need to verify changes (not redundantly)\n" +
		"   • Stage files in batches when possible, then commit with a descriptive message\n" +
		"   • AVOID redundant tool calls - each git_status/git_diff consumes a tool step\n" +
		"   • For 'check in files', typical flow: git_status (assess) → git_commit with files (stage & commit) → done\n" +
		"   • Binary executables (.exe, .dylib, .so, compiled binaries) and build artifacts should NOT be committed\n\n"

	// Inject team patterns context if available
	if a.systemDB != nil {
		if ltm, err := a.getOrCreateLTM(ctx); err == nil {
			if hasPatterns, _ := teampatterns.HasTeamPatterns(ctx, ltm); hasPatterns {
				persona += "## Team-Specific Coding Conventions Available\n\n" +
					"This user has team-specific coding conventions in Long-Term Memory.\n" +
					"When generating or planning code, you SHOULD search for relevant patterns.\n\n" +
					"Pattern types available:\n" +
					"- Authentication patterns (JWT, OAuth, sessions)\n" +
					"- Error handling conventions (wrappers, logging, response format)\n" +
					"- Database patterns (query builders, transactions, migrations)\n" +
					"- Testing utilities (fixtures, mocks, integration tests)\n" +
					"- API design patterns (REST conventions, validation)\n" +
					"- Background job patterns (async processing, queues)\n\n" +
					"Use investigation_search_team_patterns tool to find relevant conventions.\n\n" +
					"CRITICAL: Always prefer team conventions over generic approaches when patterns exist.\n" +
					"Team patterns contain project-specific helpers, security requirements, and team decisions.\n\n" +
					"**Team Patterns Workflow:**\n" +
					"When user adds/updates patterns, watch for completion signals: 'done', 'completed', 'all specified', 'that's all', etc.\n" +
					"Upon completion signal, call investigation_vectorize_team_patterns to index patterns for search.\n" +
					"User doesn't need technical details - just confirm patterns are indexed when vectorization completes.\n\n"
			}
		}
	}

	// 2. Cache in MRU for future turns
	a.markMRUCategoryWithSource(cacheKey, persona, MRUSourcePersona)
	if personaFromKB {
		a.markMRUCategoryWithSource(sourceKey, "kb", MRUSourcePersona)
	} else {
		a.markMRUCategoryWithSource(sourceKey, "fallback", MRUSourcePersona)
	}

	return persona, personaFromKB
}

func (a *CopilotAgent) getScriptToolsPrompt(ctx context.Context) string {
	toolsDef := ""
	if a.systemDB != nil {
		if tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading); err == nil {
			if store, err := a.systemDB.OpenModelStore(ctx, "scripts", tx); err == nil {
				if names, err := store.List(ctx, ai.DefaultScriptCategory); err == nil {
					for _, name := range names {
						var script ai.Script
						if err := store.Load(ctx, ai.DefaultScriptCategory, name, &script); err == nil {
							argsSchema := "()"
							if len(script.Parameters) > 0 {
								var params []string
								for _, p := range script.Parameters {
									params = append(params, fmt.Sprintf("%s: string", p))
								}
								argsSchema = fmt.Sprintf("(%s)", strings.Join(params, ", "))
							}
							toolsDef += fmt.Sprintf("- %s: %s %s\n", name, script.Description, argsSchema)
						}
					}
				}
			}
			tx.Commit(ctx)
		}
	}
	return toolsDef
}

func (a *CopilotAgent) getActiveRepoHint(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if payload := ai.GetSessionPayload(ctx); payload != nil {
		if strings.TrimSpace(payload.ActiveRepo) != "" {
			return strings.TrimSpace(payload.ActiveRepo)
		}
	}
	if a != nil && a.service != nil && a.service.session != nil {
		if state := a.service.session.Memory.GetCodingWorkflowState(); state != nil && strings.TrimSpace(state.Description) != "" {
			if strings.TrimSpace(state.ActiveFile) != "" {
				return strings.TrimSpace(state.ActiveFile)
			}
		}
	}
	return ""
}

// getOrCreateLTM returns the Long-Term Memory knowledge base for team patterns and other persistent memory.
// This is used by team patterns integration to provide access to LTM from phase capabilities.
func (a *CopilotAgent) getOrCreateLTM(ctx context.Context) (*memory.KnowledgeBase[map[string]any], error) {
	if a.systemDB == nil {
		return nil, fmt.Errorf("systemDB not available")
	}

	// Bind session to get proper LTM name
	a.Memory.BindSession(ctx)
	kbName := a.Memory.LongTermMemoryName()

	tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get embedder if available
	var embedder ai.Embeddings
	if a.service != nil && a.service.Domain() != nil {
		embedder = a.service.Domain().Embedder()
	}

	kb, err := a.systemDB.OpenKnowledgeBase(ctx, kbName, tx, nil, embedder, false, true)
	if err != nil {
		return nil, fmt.Errorf("failed to open LTM knowledge base: %w", err)
	}

	return kb, nil
}

func (a *CopilotAgent) getLTMSemanticContext(ctx context.Context, query string) string {
	toolsDef := ""
	if a.systemDB != nil && a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil && a.Memory.AgentID != "" {
		if tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading); err == nil {
			a.Memory.BindSession(ctx)
			kbName := a.Memory.LongTermMemoryName()
			kb, err := a.systemDB.OpenKnowledgeBase(ctx, kbName, tx, nil, nil, false, true)
			if err == nil {
				payload := ai.GetSessionPayload(ctx)
				lifecycleSignal := detectLTMLifecycleSignal(query)
				if err := applyLTMLifecycleSignal(ctx, kb, lifecycleSignal); err != nil {
					log.Warn("CopilotAgent: failed to apply explicit lifecycle signal", "action", lifecycleSignal.Action, "error", err)
				}
				targets := buildLTMRetrievalTargets(query, payload, a.getActiveRepoHint(ctx))
				targets = filterLTMRetrievalTargets(targets, lifecycleSignal)
				for _, target := range targets {
					if strings.EqualFold(target.ScopeType, "repo") && strings.TrimSpace(target.ScopeID) != "" {
						if err := touchRepoScopedLTMLifecycleState(ctx, kb, target.ScopeID, time.Now()); err != nil {
							log.Warn("CopilotAgent: failed to refresh repo lifecycle state", "repo", target.ScopeID, "error", err)
						}
						break
					}
				}
				var allHits []memory.KBDigestHit
				for _, target := range targets {
					queries := []string{query}
					if target.Category != "" && target.Category != "general" {
						queries = append(queries, fmt.Sprintf("%s %s", target.Category, query))
					}
					hits, err := memory.DigestKnowledgeBase(ctx, kb, a.service.Domain().Embedder(), memory.KBDigestRequest{
						Queries:            queries,
						PerQueryLimit:      3,
						MaxResults:         3,
						MinScore:           0.6,
						UseClosestCategory: true,
						KeywordFallback:    true,
					})
					if err == nil {
						for _, hit := range hits {
							if strings.TrimSpace(hit.Text) == "" {
								continue
							}
							allHits = append(allHits, hit)
						}
					}
				}
				for _, hit := range allHits {
					if strings.TrimSpace(hit.ID) != "" {
						if err := updateLTMMemoryLifecycleState(ctx, kb, hit.ID, int(hit.Score*100)); err != nil {
							log.Warn("CopilotAgent: failed to update lifecycle state for LTM hit", "item_id", hit.ID, "error", err)
						}
					}
				}
				if len(allHits) > 0 {
					knowledgeStr := ""
					for _, hit := range allHits {
						knowledgeStr += formatLearnedKnowledgeHit(ctx, kb, hit)
					}
					toolsDef += "\nContext Section (Learned Knowledge):\n" + knowledgeStr
				}
			}
			tx.Rollback(ctx)
		}
	}
	return toolsDef
}

func formatLearnedKnowledgeHit(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], hit memory.KBDigestHit) string {
	label := hit.Category
	if label == "" {
		label = "general"
	}
	text := strings.TrimSpace(hit.Text)
	if text == "" {
		return ""
	}
	sourceLabel := memory.ResolveHitSourceLabel(ctx, kb, hit.DocID)
	if sourceLabel == "" && len(hit.DocID) > 0 && strings.TrimSpace(hit.DocID[0]) != "" {
		sourceLabel = hit.DocID[0]
	}
	if sourceLabel != "" {
		return fmt.Sprintf("- (Score: %.2f) [%s] %s\n  Source: %s\n", hit.Score, label, text, sourceLabel)
	}
	return fmt.Sprintf("- (Score: %.2f) [%s] %s\n", hit.Score, label, text)
}

func buildKnowledgeMiningQueries(domain string, query string) []string {
	queries := []string{query}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return queries
	}

	queries = append(queries, domain+" "+query)
	if strings.EqualFold(domain, "sop") {
		queries = append(queries,
			"SOP architecture "+query,
			"SOP SDLC "+query,
			"SOP onboarding "+query,
			"SOP tech stack "+query,
		)
	}
	return queries
}

func (a *CopilotAgent) getPlaybooksContext(ctx context.Context, query string, targetDomains []string) string {
	toolsDef := ""
	for _, domain := range targetDomains {
		domain = strings.TrimSpace(domain)
		if domain == "" || domain == "custom" {
			continue
		}

		var domainDB *database.Database

		var dbOptsList []sop.DatabaseOptions
		if a.systemDB != nil {
			dbOptsList = append(dbOptsList, a.systemDB.Config())
		}

		for _, dbOpts := range a.databases {
			dbOptsList = append(dbOptsList, dbOpts)
		}

		for _, dbOpts := range dbOptsList {
			tempDB := database.NewDatabase(dbOpts)
			kbs := func(ctx context.Context, tempDB *database.Database) []string {
				var kbs []string
				if tx, err := tempDB.BeginTransaction(ctx, sop.NoCheck); err == nil {
					if stores, err := tx.GetStores(ctx); err == nil {
						for _, s := range stores {
							if strings.HasSuffix(s, "/sys_config") {
								kbs = append(kbs, strings.TrimSuffix(s, "/sys_config"))
							}
						}
					}
					tx.Rollback(ctx)
				}
				return kbs
			}(ctx, tempDB)

			hasDomain := false
			for _, kb := range kbs {
				if kb == domain {
					hasDomain = true
					break
				}
			}
			if hasDomain {
				domainDB = tempDB
				break
			}
		}

		hasGoodHits := false
		if domainDB != nil {
			if tx, err := domainDB.BeginTransaction(ctx, sop.NoCheck); err == nil {
				kb, err := domainDB.OpenKnowledgeBase(ctx, domain, tx, nil, nil, false)
				if err == nil && a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil {
					hits, err := memory.DigestKnowledgeBase(ctx, kb, a.service.Domain().Embedder(), memory.KBDigestRequest{
						Queries:            buildKnowledgeMiningQueries(domain, query),
						PerQueryLimit:      5,
						MaxResults:         5,
						MinScore:           0.6,
						UseClosestCategory: true,
						KeywordFallback:    strings.EqualFold(domain, "sop"),
					})
					accumStr := ""
					if err == nil && len(hits) > 0 {
						for _, hit := range hits {
							hasGoodHits = true
							accumStr += fmt.Sprintf("- Context (Score: %.2f): %s\n", hit.Score, hit.Text)
						}

						if hasGoodHits {
							toolsDef += fmt.Sprintf("\nActive Playbook Context (%s):\n%s", domain, accumStr)
							a.markMRUCategoryWithSource(playbookMRUCategory(domain), fmt.Sprintf("Retrieved Semantics:\n%s", accumStr), MRUSourcePlaybook)
						}
					}
				}
				tx.Rollback(ctx)
			}
		}

		if !hasGoodHits {
			if a.service != nil && a.service.session != nil {
				if carriedOver, ok := a.getMRUCategoryBySource(playbookMRUCategory(domain), MRUSourcePlaybook, true); ok && carriedOver != "" {
					toolsDef += fmt.Sprintf("\nCarried-Over Playbook Context (%s):\n%s\n", domain, carriedOver)
				}
			}
		}
	}
	return toolsDef
}

func (a *CopilotAgent) getSchemaInjectionContext(ctx context.Context) string {
	toolsDef := ""
	if p := ai.GetSessionPayload(ctx); p != nil && p.CurrentDB != "" {
		var db *database.Database
		if p.CurrentDB == SystemDBName {
			db = a.systemDB
		} else if dbOpts, ok := a.databases[p.CurrentDB]; ok {
			db = database.NewDatabase(dbOpts)
		}

		if db != nil {
			if tx, err := db.BeginTransaction(ctx, sop.ForReading); err == nil {
				if stores, err := tx.GetStores(ctx); err == nil {
					toolsDef += fmt.Sprintf("\nActive Database: %s\nAvailable Stores:\n", p.CurrentDB)
					for _, s := range stores {
						if strings.Contains(s, "/") {
							continue
						}
						var schemaInfo string
						if storeAccessor, err := jsondb.OpenStore(ctx, db.Config(), s, tx); err == nil {
							info := storeAccessor.GetStoreInfo()
							// Prefer stored schema from StoreInfo, fallback to runtime inference
							if len(info.Schema) > 0 {
								schemaInfo += fmt.Sprintf(" %s", formatSchema(info.Schema))
							} else if ok, _ := storeAccessor.First(ctx); ok {
								key := storeAccessor.GetCurrentKey()
								if key != nil {
									if val, err := storeAccessor.GetCurrentValue(ctx); err == nil {
										flat := flattenItem(key, val)
										sMap := inferSchema(flat)
										schemaInfo += fmt.Sprintf(" %s", formatSchema(sMap))
									}
								}
							}
							if len(info.Relations) > 0 {
								var rels []string
								for _, r := range info.Relations {
									rels = append(rels, fmt.Sprintf("[%s] -> %s([%s])", strings.Join(r.SourceFields, ", "), r.TargetStore, strings.Join(r.TargetFields, ", ")))
								}
								schemaInfo += fmt.Sprintf(" (Relations: %s)", strings.Join(rels, "; "))
							}
							if info.MapKeyIndexSpecification != "" {
								schemaInfo += fmt.Sprintf(" (Key Schema: %s)", info.MapKeyIndexSpecification)
							}
						}
						toolsDef += fmt.Sprintf("- %s%s\n", s, schemaInfo)
					}
				}
				tx.Rollback(ctx)
			}
		}
	}
	return toolsDef
}

func summarizePromptBudgetTrim(report PromptBudgetReport) string {
	trimmed := report.TrimmedComponents()
	if len(trimmed) == 0 {
		return ""
	}

	parts := make([]string, 0, len(trimmed))
	for _, stat := range trimmed {
		parts = append(parts, fmt.Sprintf("%s:%d->%d", stat.Component, stat.OriginalChars, stat.FinalChars))
	}
	return strings.Join(parts, ",")
}

func summarizeTaskContextForLog(taskClassification TaskContextClassification) string {
	artifacts := taskClassification.DBArtifacts
	if len(artifacts) == 0 {
		artifacts = append(artifacts, taskClassification.StoresArtifacts...)
		artifacts = append(artifacts, taskClassification.SpacesArtifacts...)
	}
	if len(artifacts) > 4 {
		artifacts = append(append([]string(nil), artifacts[:4]...), "...")
	}
	layers := make([]string, 0, len(taskClassification.Layers))
	for _, layer := range taskClassification.Layers {
		layers = append(layers, fmt.Sprintf("%s[%s]", layer.Name, strings.Join(layer.CRUD, "")))
	}
	return fmt.Sprintf("entity=%s domain=%s artifacts=%s layers=%s", taskClassification.Entity, taskClassification.Domain, strings.Join(artifacts, ","), strings.Join(layers, ","))
}

func (a *CopilotAgent) captureRuntimeCaveatsFromText(text string) {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return
	}
	for _, rawLine := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}
		var summary, details, kind, categoryRoot string
		switch {
		case strings.HasPrefix(trimmed, "runtime_caveat:"):
			kind = "caveat"
			categoryRoot = "runtime_guidance"
			summary = strings.TrimSpace(strings.TrimPrefix(trimmed, "runtime_caveat:"))
		case strings.HasPrefix(trimmed, "guidance:"):
			kind = "guidance"
			categoryRoot = "runtime_guidance"
			summary = strings.TrimSpace(strings.TrimPrefix(trimmed, "guidance:"))
		case strings.HasPrefix(trimmed, "note:"):
			kind = "note"
			categoryRoot = "runtime_guidance"
			summary = strings.TrimSpace(strings.TrimPrefix(trimmed, "note:"))
		default:
			continue
		}
		if detailIdx := strings.Index(summary, "|details:"); detailIdx >= 0 {
			parts := strings.SplitN(summary, "|details:", 2)
			summary = strings.TrimSpace(parts[0])
			details = strings.TrimSpace(parts[1])
		}
		if summary == "" {
			continue
		}
		a.service.session.Memory.AddRuntimeCaveat(summary, details, kind, categoryRoot, "llm_generated")
	}
}

func (a *CopilotAgent) getPlannerContext() string {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return ""
	}
	caveats := a.service.session.Memory.GetRuntimeCaveats(5)
	var sb strings.Builder
	var wrote bool
	for _, caveat := range caveats {
		categoryRoot := strings.TrimSpace(caveat.CategoryRoot)
		source := strings.TrimSpace(caveat.Source)
		kind := strings.TrimSpace(caveat.Kind)
		if categoryRoot != "planner" && source != "planner_capability" && kind != "planner" {
			continue
		}
		if !wrote {
			sb.WriteString("Planner Memory:\n")
			wrote = true
		}
		if summary := strings.TrimSpace(caveat.Summary); summary != "" {
			sb.WriteString(fmt.Sprintf("- %s", summary))
		}
		if details := strings.TrimSpace(caveat.Details); details != "" {
			sb.WriteString(fmt.Sprintf(": %s", details))
		}
		sb.WriteString("\n")
	}
	if !wrote {
		return ""
	}
	return strings.TrimSpace(sb.String())
}

func (a *CopilotAgent) getCodingWorkflowContext() string {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return ""
	}
	caveats := a.service.session.Memory.GetRuntimeCaveats(3)
	var sb strings.Builder
	var wrote bool
	for _, caveat := range caveats {
		categoryRoot := strings.TrimSpace(caveat.CategoryRoot)
		source := strings.TrimSpace(caveat.Source)
		kind := strings.TrimSpace(caveat.Kind)
		if categoryRoot != "coding_workflow" && source != "coding_workflow" && kind != "coding_workflow" {
			continue
		}
		if !wrote {
			sb.WriteString("Coding Workflow Memory:\n")
			wrote = true
		}
		if categoryRoot != "" {
			sb.WriteString(fmt.Sprintf("- [%s] %s", strings.TrimSpace(categoryRoot), strings.TrimSpace(caveat.Summary)))
		} else {
			sb.WriteString(fmt.Sprintf("- %s", strings.TrimSpace(caveat.Summary)))
		}
		if strings.TrimSpace(caveat.Details) != "" {
			sb.WriteString(fmt.Sprintf(": %s", strings.TrimSpace(caveat.Details)))
		}
		sb.WriteString("\n")
	}

	// Phase 1: Inject test debugging protocol when user requests test fixes
	if a.isTestFixRequest() {
		if wrote {
			sb.WriteString("\n")
		}
		sb.WriteString(`TEST DEBUGGING PROTOCOL (MANDATORY STEPS):

Step 1: CAPTURE TEST FAILURE
- Run: execute_local_command(command="go test -v -run TestName ./path", reasoning="capture full test error")
- If output shows file_path: IMMEDIATELY call read_tool_result(file_path="...") to read it
- Extract actual error message
- NEVER proceed without seeing the error

Step 2: UNDERSTAND THE TEST
- Call: read_file(path="path/to/test.go", start_line=N, end_line=M)
- Understand what the test is checking
- Identify which functions it calls

Step 3: INSPECT IMPLEMENTATION
- Use search_repo_files or grep_repo_text to find implementation
- Read the code that the test calls
- Compare expected vs actual behavior

Step 4: IDENTIFY ROOT CAUSE
- Match error message to code location
- Determine what's missing or incorrect
- Form hypothesis about the fix

Step 5: APPLY FIX
- Use patch_file for focused changes (preferred)
- Or write_file for complete rewrites
- Include clear reasoning

Step 6: VERIFY FIX
- Re-run the same test command from Step 1
- Confirm test passes
- Show passing output to user

Step 7: ITERATE IF NEEDED
- If test still fails, return to Step 1
- Maximum 3 iterations

CRITICAL RULES:
✗ NEVER claim tests are fixed without running them
✗ NEVER skip reading the error output
✗ NEVER skip reading test and implementation code
✗ NEVER proceed without completing prior step
✓ ALWAYS show passing test output as proof
`)
		wrote = true
	}

	if wrote {
		sb.WriteString("\n")
		sb.WriteString(`FILE-BACKED TOOL RESULTS:
- If a tool result includes file_path or says full output was saved, do not conclude from the truncated preview alone.
- For known signals such as PASS, FAIL, panic, timeout, or ERROR, call grep_tool_result(file_path="...", pattern="...") first.
- For open-ended investigation, call read_tool_result(file_path="...", start_line=X, end_line=Y) or analyze_tool_result(file_path="...", operation="sed"|"head"|"tail"|"wc").
- Use the saved file to confirm the outcome before making a final claim.
`)
	}

	if !wrote {
		return ""
	}
	return strings.TrimSpace(sb.String())
}

func (a *CopilotAgent) isTestFixRequest() bool {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return false
	}
	thread := a.service.session.Memory.GetCurrentThread()
	if thread == nil || len(thread.Exchanges) == 0 {
		return false
	}
	// Check last user message for test-fix keywords
	for i := len(thread.Exchanges) - 1; i >= 0; i-- {
		if thread.Exchanges[i].Role == RoleUser {
			content := strings.ToLower(thread.Exchanges[i].Content)
			return strings.Contains(content, "fix") && (strings.Contains(content, "test") || strings.Contains(content, "failing"))
		}
	}
	return false
}

func (a *CopilotAgent) getCaveatContext() string {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return ""
	}
	caveats := a.service.session.Memory.GetRuntimeCaveats(3)
	if len(caveats) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Runtime Guidance:\n")
	for _, caveat := range caveats {
		if caveat.CategoryRoot != "" {
			sb.WriteString(fmt.Sprintf("- [%s] %s", strings.TrimSpace(caveat.CategoryRoot), strings.TrimSpace(caveat.Summary)))
		} else {
			sb.WriteString(fmt.Sprintf("- %s", strings.TrimSpace(caveat.Summary)))
		}
		if strings.TrimSpace(caveat.Details) != "" {
			sb.WriteString(fmt.Sprintf(": %s", strings.TrimSpace(caveat.Details)))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func (a *CopilotAgent) getSessionMemoryContext() string {
	convHistory := ""
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if thread := a.service.session.Memory.GetCurrentThread(); thread != nil && len(thread.Exchanges) > 0 {
			var sb strings.Builder

			exchanges := thread.Exchanges
			if len(exchanges) > MaxExchangesInHistory {
				exchanges = exchanges[len(exchanges)-MaxExchangesInHistory:]
			}

			for _, ex := range exchanges {
				if ex.Role == RoleUser {
					userText := ex.Content
					if len(userText) > 1000 {
						userText = userText[:1000] + "... [truncated for brevity]"
					}
					sb.WriteString(fmt.Sprintf("User: %s\n", userText))
				} else if ex.Role == RoleAssistant {
					astText := sanitizeAssistantContinuityText(ex.Content)
					for {
						start := strings.Index(astText, "```json")
						if start == -1 {
							break
						}
						end := strings.Index(astText[start+7:], "```")
						if end == -1 {
							break
						}
						astText = astText[:start] + "```json\n[... AST Script/JSON Payload stripped for brevity ...]\n```" + astText[start+7+end+3:]
					}
					if len(astText) > 800 {
						astText = astText[:800] + "... [Assistant response truncated for brevity]"
					}
					sb.WriteString(fmt.Sprintf("Assistant: %s\n", astText))
				}
			}
			if sb.Len() > 0 {
				convHistory = "\n[ACTIVE SESSION MEMORY]\n" + sb.String() + "\n[/ACTIVE SESSION MEMORY]\n\n"
			}
		}
	}
	return convHistory
}

// ============================================================================
// BASELINE ReACT ENGINE (10 Turn Loop Abstraction)
// ============================================================================

type BaselineReActEngine struct {
	Agent *CopilotAgent
}

func (b *BaselineReActEngine) Run(ctx context.Context, req ai.ReasoningRequest) (ai.ReasoningResponse, error) {
	maxTurns := 10
	history := req.SystemPrompt
	isSpaceGeneration := strings.Contains(req.UserQuery, "IMPORTANT SYSTEM RULE: The user is generating a UI Space.")

	for i := 0; i < maxTurns; i++ {
		// Use injected Generator (gen)
		resp, err := req.Generator.Generate(ctx, history, ai.GenOptions{})
		if err != nil {
			return ai.ReasoningResponse{}, err
		}

		text := strings.TrimSpace(resp.Text)
		type localToolCall struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		var toolCalls []localToolCall
		var cleanText string
		if start := strings.Index(text, "```"); start != -1 {
			cleanText = text[start:]
			if strings.HasPrefix(cleanText, "```json") {
				cleanText = strings.TrimPrefix(cleanText, "```json")
			} else {
				cleanText = strings.TrimPrefix(cleanText, "```")
			}
			if end := strings.LastIndex(cleanText, "```"); end != -1 {
				cleanText = cleanText[:end]
			}
		} else {
			cleanText = text
			idxOb := strings.Index(text, "{")
			idxAr := strings.Index(text, "[")
			if idxOb != -1 && (idxAr == -1 || idxOb < idxAr) {
				var scriptSteps []any
				if err := json.Unmarshal([]byte(cleanText), &scriptSteps); err == nil && len(scriptSteps) > 0 {
					isScript := true
					for _, step := range scriptSteps {
						if m, ok := step.(map[string]any); ok {
							if _, hasOp := m["op"]; !hasOp {
								isScript = false
								break
							}
						} else {
							isScript = false
							break
						}
					}
					if isScript {
						log.Info("Auto-detected raw script in LLM output. Wrapping in 'execute_script' tool.")
						toolCalls = []localToolCall{
							{
								Tool: "execute_script",
								Args: map[string]any{
									"script": scriptSteps,
								},
							},
						}
					}
				}
			}

			if len(toolCalls) == 0 {
				var single localToolCall
				if err2 := json.Unmarshal([]byte(cleanText), &single); err2 == nil && single.Tool != "" {
					toolCalls = []localToolCall{single}
				}
			}

			if len(toolCalls) > 0 {
				if isVerboseEnabled(ctx) {
					if w, ok := ctx.Value(ai.CtxKeyWriter).(io.Writer); ok {
						var prettyJSON bytes.Buffer
						if err := json.Indent(&prettyJSON, []byte(cleanText), "", "  "); err == nil {
							fmt.Fprintf(w, "\n[Tool Instructions]:\n%s\n", prettyJSON.String())
						} else {
							fmt.Fprintf(w, "\n[Tool Instructions]:\n%s\n", cleanText)
						}
					}
				}

				if p := ai.GetSessionPayload(ctx); p != nil {
					p.LastInteractionSteps = 0
				}

				var results []string
				for _, tc := range toolCalls {
					if isSpaceGeneration && (tc.Tool == "open_store" || tc.Tool == "execute_script" || tc.Tool == "list_stores") {
						return ai.ReasoningResponse{FinalText: "SYSTEM ERROR: DO NOT output any tool callbacks for Space generation. Please emit purely raw ExportData JSON object without 'tool' formatting."}, nil
					}
					result, err := req.Executor.Execute(ctx, tc.Tool, tc.Args)
					if err != nil {
						result = "Error: " + err.Error()
					}
					results = append(results, result)

					if recorder, ok := ctx.Value(ai.CtxKeyScriptRecorder).(ai.ScriptRecorder); ok {
						recorder.RecordStep(ctx, ai.ScriptStep{
							Type:    "command",
							Command: tc.Tool,
							Args:    tc.Args,
						})
						if p := ai.GetSessionPayload(ctx); p != nil {
							p.LastInteractionSteps++
						}
					}
				}

				finalResp := strings.Join(results, "\n")
				tcBytes, _ := json.Marshal(toolCalls)
				b.Agent.logThought(ctx, req.UserQuery, string(tcBytes), finalResp)

				history += "\n\nAssistant:\n" + text + "\n\n[System Tool Response]:\n" + finalResp + "\n\nUser: Analyze the tool response and decide the next step. If the task is fully complete, provide your final response to the user with NO JSON or tool markup."
				continue
			}
		}

		return ai.ReasoningResponse{FinalText: text}, nil
	}

	return ai.ReasoningResponse{FinalText: "Error: Maximum turns reached."}, nil
}

// ListTools returns the list of available tools.
func (a *CopilotAgent) ListTools(ctx context.Context) ([]ai.ToolDefinition, error) {
	if a == nil {
		return nil, nil
	}
	a.registerTools(ctx)

	exposure := buildNativeToolExposure(activeRoutingState(ctx))
	tools := a.listRegisteredTools(exposure)
	return tools, nil
}

func (a *CopilotAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent not initialized")
	}
	a.registerTools(ctx)

	if args == nil {
		args = make(map[string]any)
	}

	currentDB := ""
	if payload := ai.GetSessionPayload(ctx); payload != nil {
		currentDB = payload.CurrentDB
	}
	sanitizeToolCallArgs(args, a.shouldObfuscate(currentDB))

	if toolName != "save_last_step" {
		a.lastToolCall = &ai.ScriptStep{
			Type:    "command",
			Command: toolName,
			Args:    cloneAnyMap(args),
		}
	}

	tool, ok := a.registry.Get(toolName)
	if !ok || tool.Handler == nil {
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}

	res, err := tool.Handler(ctx, args)
	if err != nil {
		return "", err
	}
	formatted, err := formatToolResult(ctx, res)
	if err != nil {
		return "", err
	}
	return wrapToolResultWithStorage(ctx, toolName, formatted)
}

func (a *CopilotAgent) logThought(ctx context.Context, intent, astPayload, outcome string) {
	if a == nil || a.Memory == nil {
		return
	}
	a.Memory.LogEpisodeToSTM(ctx, intent, map[string]any{"tool_calls": astPayload}, outcome, nil)
}
