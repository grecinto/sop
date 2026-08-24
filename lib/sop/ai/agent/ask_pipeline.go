package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	log "log/slog"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/agent/planner"
)

const ctxKeyAskWorkItemActor = "ask_work_item_actor"

// ============================================================================
// Ask Pipeline - Critical Architectural Patterns
// ============================================================================
//
// 1. EXPLICIT PARAMETER PASSING (No Context for State)
//    ✅ State passed as explicit parameters: classification, plan, currentPhase
//    ✅ Context only for: cancellation, timeouts, request IDs
//    ❌ NEVER: ctx.WithValue(ctx, "state", data) for application state
//
// 2. PARTICIPANT PATTERN (Phases are Passive)
//    ✅ Phases respond to: ExposeTools(), BuildInstructions(), ExecuteTool()
//    ✅ ExecuteTool() executes ONE tool, returns result
//    ❌ NEVER: Phases control flow, create loops, or orchestrate
//    ❌ NEVER: ExecuteTool() calls other tools or enqueues multiple items
//
// 3. ASK/LLM IS ORCHESTRATOR (Single Point of Control)
//    ✅ Ask pipeline controls: classification → phase creation → tool registration → ReAct
//    ✅ Ask pipeline decides: when phase complete, which phase next, enqueue work
//    ✅ LLM decides: which tools to call, when, with what arguments
//    ❌ NEVER: Phases decide workflow progression
//
// 4. NO CONTROLLER PATTERN IN PHASES
//    ✅ Phase.ExecuteTool() is a simple dispatch: args → result
//    ❌ NEVER: for loops in ExecuteTool()
//    ❌ NEVER: ExecuteTool() → queue.Enqueue() multiple times
//    ❌ NEVER: ExecuteTool() → ExecuteTool() (recursive/chained calls)
//
// Example Correct Flow:
//   Ask pipeline → createPhases(domains, phase) → registerTools()
//   → reactLoop() → LLM calls tool → phase.ExecuteTool(toolName, args)
//   → phase returns result → Ask pipeline checks completion
//   → Ask pipeline enqueues next phase if needed
//
// ============================================================================

// runAskPipeline is the queue-aware Ask pipeline implementing pure
// LLM-as-Orchestrator behavior.
//
// ARCHITECTURE:
// - Preserves ALL V1 infrastructure (queue, gates, intent, KB, lifecycle)
// - ONLY Gate 3 (Cold Start/Discovery) uses Phase Factory instead of heuristics
// - Gates 1 (Focused) and 2 (Continuity) unchanged
// - Work queue, intent classification, KB routing, epilogue all preserved
//
// V2 CHANGES (Cold Start Gate Only):
// 1. LLM Classification → Multi-domain + Phase
// 2. Phase Factory → Domain-specific phase proxies
// 3. Tool Aggregation → ALL tools from ALL phases
// 4. LLM Orchestration → Pure tool selection
// 5. Phase Succession → Work queue enqueue
//
// PRIORITY QUEUE:
// - User queries (100) > System resume (50) > Phase succession (10)
// - User queries always processed before code-enqueued work
// - LLM sees WorkStream state and decides via tools (pause/resume/abandon)
func (a *CopilotAgent) runAskPipeline(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	// Get or create session queue
	queue := a.getOrCreateSessionQueue()

	// Enqueue user query with highest priority
	if err := queue.Enqueue(WorkItem{
		Query:    query,
		Actor:    "user",
		Priority: PriorityUser, // User always processed first
		ID:       fmt.Sprintf("user-%d", time.Now().UnixNano()),
	}); err != nil {
		return "", fmt.Errorf("failed to enqueue query: %w", err)
	}

	// Process queue until empty (priority-based dequeue)
	var lastResponse string
	for !queue.IsEmpty() {
		item, err := queue.Dequeue() // Returns highest priority item
		if err != nil {
			break
		}

		log.Info("Processing queue item",
			"actor", item.Actor,
			"priority", item.Priority,
			"query_preview", truncateString(item.Query, 80))
		effectiveQuery := buildEffectiveQueueQuery(item)

		// Execute ask (LLM orchestrates WorkStream management via tools)
		itemCtx := context.WithValue(ctx, ctxKeyAskWorkItemActor, item.Actor)
		if preferredModel, ok := item.Context["preferred_model"].(string); ok && strings.TrimSpace(preferredModel) != "" {
			itemCtx = context.WithValue(itemCtx, ai.CtxKeyProvider, preferredModel)
		}
		response, err := a.askCore(itemCtx, effectiveQuery, cfg)
		if err != nil {
			return response, err
		}

		lastResponse = response

		// ReAct returns either:
		// - Completed answer: continue to next queue item
		// - Clarification question: pause queue, return to user
		// Capabilities may have enqueued next steps during execution
		if shouldPauseAskQueue(itemCtx) {
			log.Info("Pausing queue after pending clarification state", "actor", item.Actor)
			break
		}

		// Log queue state after each turn
		queueDepth := queue.Depth()
		log.Info("Queue item completed",
			"actor", item.Actor,
			"remaining_items", queueDepth,
			"continuing", queueDepth > 0)
	}

	return lastResponse, nil
}

// askCore is the core Ask→ReAct transaction with Ask routing for Gate 3.
// Preserves all shared infrastructure (Gates 1/2, intent, KB, lifecycle, epilogue).
// ONLY Cold Start gate (Gate 3) uses Phase Factory + LLM classification.
func shouldPauseAskQueue(ctx context.Context) bool {
	payload := ai.GetSessionPayload(ctx)
	if payload == nil || payload.ClarificationState == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(payload.ClarificationState.Status))
	if status == "" {
		status = "pending"
	}
	return status == "pending" || status == "clarifying" || status == "needs_clarification"
}

type phaseTransactionDecision struct {
	EnqueueNextPhase bool
	NextPhase        string
	Reason           string
}

func buildPhaseTransactionContinuationGuidance(decision phaseTransactionDecision) string {
	nextPhase := strings.TrimSpace(strings.ToLower(decision.NextPhase))
	reason := strings.TrimSpace(decision.Reason)
	switch nextPhase {
	case "planning":
		if reason == "" {
			reason = "Planning is still required before execution can continue."
		}
		return fmt.Sprintf("Controller guidance: continue in planning and produce or refine the execution-ready plan artifact. Missing requirement: %s", reason)
	case "execution":
		if strings.Contains(strings.ToLower(reason), "ready_for_execution") || strings.Contains(strings.ToLower(reason), "execution-ready plan") || strings.Contains(strings.ToLower(reason), "persisted plan") {
			if reason == "" {
				reason = "A persisted execution-ready plan artifact is available."
			}
			return fmt.Sprintf("Controller guidance: continue in execution and work directly from the persisted execution-ready plan artifact before concluding. Transition trigger: %s", reason)
		}
		if reason == "" {
			reason = "Focused validation evidence is still required before completion."
		}
		return fmt.Sprintf("Controller guidance: continue in execution and satisfy the missing validation requirement before completion. Missing requirement: %s", reason)
	default:
		if reason == "" {
			return ""
		}
		return fmt.Sprintf("Controller guidance: continue with the required workflow follow-up. Missing requirement: %s", reason)
	}
}

func (a *CopilotAgent) evaluatePhaseTransaction(ctx context.Context, query string, classification *AskTaskContext, toolCalls []ai.ToolCall, outcomeFacts []string, finalText string, clarificationRequest *ai.ClarificationRequest) phaseTransactionDecision {
	return a.evaluatePhaseTransactionCognitiveDecision(ctx, query, classification, toolCalls, outcomeFacts, finalText, clarificationRequest)
}

func phaseTransactionFoundFailingTests(toolCalls []ai.ToolCall, outcomeFacts []string, finalText string) bool {
	usedTestTool := false
	for _, toolCall := range toolCalls {
		name := strings.TrimSpace(strings.ToLower(toolCall.Name))
		if strings.Contains(name, "run_tests") || strings.Contains(name, "run_repo_tests") || strings.Contains(name, "test") {
			usedTestTool = true
			break
		}
	}
	if !usedTestTool {
		return false
	}
	for _, candidate := range append(append([]string{}, outcomeFacts...), finalText) {
		lower := strings.ToLower(strings.TrimSpace(candidate))
		if strings.Contains(lower, "--- fail:") || strings.Contains(lower, "failing tests") || strings.Contains(lower, "tests failed") || strings.Contains(lower, " fail\t") || strings.Contains(lower, "^fail$") || strings.Contains(lower, "panic:") || strings.Contains(lower, "\nfail\n") {
			return true
		}
		if strings.Contains(lower, "--- fail") || strings.Contains(lower, "\nfail") {
			return true
		}
	}
	return false
}

func phaseTransactionMutatedCode(toolCalls []ai.ToolCall) bool {
	for _, toolCall := range toolCalls {
		name := strings.TrimSpace(strings.ToLower(toolCall.Name))
		if strings.Contains(name, "apply_patch") || strings.Contains(name, "write_file") || strings.Contains(name, "edit_file") || strings.Contains(name, "generate_patch") {
			return true
		}
	}
	return false
}

func phaseTransactionIsStillInvestigating(toolCalls []ai.ToolCall) bool {
	// Detect if execution phase is still in investigation/search mode
	// (hasn't generated patches yet, just searching/building context)
	for _, toolCall := range toolCalls {
		name := strings.TrimSpace(strings.ToLower(toolCall.Name))
		// Context-gathering tools
		if strings.Contains(name, "search_repo") || strings.Contains(name, "build_context") ||
			strings.Contains(name, "inspect_repo") || strings.Contains(name, "index_repo") ||
			strings.Contains(name, "list_changed") || strings.Contains(name, "run_tests") {
			return true
		}
	}
	return false
}

func phaseTransactionHasValidationEvidence(toolCalls []ai.ToolCall, outcomeFacts []string, finalText string) bool {
	for _, toolCall := range toolCalls {
		name := strings.TrimSpace(strings.ToLower(toolCall.Name))
		if strings.Contains(name, "run_tests") || strings.Contains(name, "run_repo_tests") || strings.Contains(name, "validate") || strings.Contains(name, "verify") || strings.Contains(name, "review") {
			return true
		}
	}
	for _, candidate := range append(append([]string{}, outcomeFacts...), finalText) {
		lower := strings.ToLower(strings.TrimSpace(candidate))
		if strings.Contains(lower, "tests passed") || strings.Contains(lower, "validation passed") || strings.Contains(lower, "verified") || strings.Contains(lower, "rerun") || strings.Contains(lower, "ok\t") || strings.Contains(lower, "pass\t") {
			return true
		}
	}
	return false
}

func phaseTransactionHasGenericCompletionEvidence(toolCalls []ai.ToolCall, outcomeFacts []string, finalText string) bool {
	if len(toolCalls) == 0 && len(outcomeFacts) == 0 && strings.TrimSpace(finalText) == "" {
		return false
	}
	for _, candidate := range append(append([]string{}, outcomeFacts...), finalText) {
		lower := strings.ToLower(strings.TrimSpace(candidate))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "complete") || strings.Contains(lower, "done") || strings.Contains(lower, "finished") || strings.Contains(lower, "success") || strings.Contains(lower, "validated") || strings.Contains(lower, "verified") || strings.Contains(lower, "returned") {
			return true
		}
	}
	return len(toolCalls) > 0 && len(outcomeFacts) > 0
}

func (a *CopilotAgent) enqueuePhaseTransactionDecision(query string, decision phaseTransactionDecision) {
	if a == nil || !decision.EnqueueNextPhase || strings.TrimSpace(decision.NextPhase) == "" {
		log.Info("Not enqueueing next phase",
			"decision_enqueue", decision.EnqueueNextPhase,
			"next_phase", decision.NextPhase,
			"reason", "no next phase or enqueue=false")
		return
	}
	queue := a.getOrCreateSessionQueue()
	targetActor := "phase:" + strings.TrimSpace(decision.NextPhase)
	for _, item := range queue.Snapshot() {
		if item.Actor == targetActor {
			log.Info("Phase already enqueued, skipping duplicate",
				"target_actor", targetActor)
			return
		}
	}
	_ = queue.Enqueue(WorkItem{
		Query:    strings.TrimSpace(buildPhaseTransactionContinuationGuidance(decision)),
		Actor:    targetActor,
		Priority: PriorityPhaseSuccession,
		Context: map[string]any{
			"phase_transaction_continuation": true,
			"original_query":                 extractOriginalRequestQuery(query),
			"controller_guidance":            strings.TrimSpace(buildPhaseTransactionContinuationGuidance(decision)),
			"reason":                         strings.TrimSpace(decision.Reason),
		},
	})
	log.Info("Enqueued next phase",
		"target_actor", targetActor,
		"priority", PriorityPhaseSuccession,
		"reason", decision.Reason)
}

func (a *CopilotAgent) askCore(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	// 1. Setup and early validation (same as V1)
	ctx = a.applyAskConfig(ctx, cfg)
	a.clearAskProgressMRU()
	ctx = context.WithValue(ctx, "latest_user_query", query)

	providerDetails := extractProviderDetails(cfg)

	if handled, res, err := a.handlePendingUserConfirmation(ctx, query); handled {
		return res, err
	}
	if handled, res, err := a.handleDirectToolInvocation(ctx, query, nil); handled {
		return res, err
	}

	gen := a.resolveLLM(ctx, providerDetails)
	if gen == nil {
		return a.noAPIKeyMessage(), nil
	}

	sessionID := a.logAskStart(ctx, query, gen)

	// 2. Query preprocessing (same as V1)
	rawQuery := query
	query = a.applyMetaTalkRewrite(ctx, query)
	persistedAskCtx := a.getPersistedAskClassification()

	// 3. Intent classification and routing (same as V1)
	intent := a.classifyIntent(ctx, query, gen)
	if intent != ai.IntentOmni {
		log.Info("Ask: Request classified for Avatar", "avatar", intent)
		return a.executeAvatarSubAgent(ctx, intent, query)
	}

	// 4. Three-gate context classification (Ask pipeline uses Phase Factory for Gate 3)
	taskContext, err := a.evaluateAskRoutingGates(ctx, query, gen)
	if err != nil {
		log.Info("Copilot Ask routing evaluation failed", "query", query, "error", err)
		return "", err
	}
	if taskContext == nil {
		taskContext = &TaskContextClassification{}
	}
	taskContext = a.applyQueuedPhaseActorContext(ctx, taskContext, persistedAskCtx)
	taskContext = normalizeCodingRuntimeRouting(taskContext)
	log.Info("Copilot Ask Routing", "routing_gate", taskContext.RoutingGate, "task_context", summarizeTaskContextForLog(*taskContext), "query", query)
	if req := buildDefaultDomainOfferClarification(query, taskContext); req != nil {
		a.updateClarificationState(ctx, query, "", nil, req)
		return req.AssistantQuestion, nil
	}

	// 5. KB direct display fast path (same as V1)
	if a.shouldDirectDisplayKB(taskContext) {
		return a.handleKBDirectDisplay(ctx, rawQuery, intent, taskContext)
	}
	if response := unsupportedLocalCapabilityResponse(taskContext); response != "" {
		return response, nil
	}

	// 6. Episode tracking and capability setup (same as V1)
	a.trackEpisodeMetadata(ctx, intent)
	ctx, selectedCap := a.resolveCapability(ctx, query, taskContext)

	// 6a. Capability lifecycle: OnBefore (same as V1)
	routingState := capabilities.RoutingState{
		Domain:         taskContext.Domain,
		Gate:           taskContext.RoutingGate,
		CapabilityHint: taskContext.CapabilityHint,
	}
	if selectedCap != nil {
		if err := selectedCap.OnBefore(ctx, query, routingState); err != nil {
			log.Warn("Capability OnBefore failed", "capability", selectedCap.ID(), "error", err)
			return "", err
		}
	}

	// 6b. Phase Factory Integration
	if askCtx := taskContext.AskClassification; askCtx != nil {
		log.Info("Using Ask Phase Factory for WorkStream + tool registration")

		// Initialize ToolResultStorage BEFORE creating phases (so preprocessor can be set)
		if _, hasStorage := ctx.Value(ctxKeyToolResultStorage).(*ToolResultStorage); !hasStorage {
			storage := getOrCreateToolResultStorage(ctx)
			if storage != nil {
				ctx = context.WithValue(ctx, ctxKeyToolResultStorage, storage)
				log.Info("ToolResultStorage initialized before phase creation", "session_id", storage.sessionID)
			}
		}

		// Resolve phase plan from WorkStream or classification
		plan, currentPhase := a.resolvePhasePlan(ctx, askCtx)
		log.Info("Phase plan resolved", "plan", plan, "current_phase", currentPhase)

		// Create domain-specific phase proxies
		phases := a.createPhaseProxies(askCtx.Domains, currentPhase, askCtx)
		log.Info("Phase proxies created", "count", len(phases), "domains", askCtx.Domains)

		// Update ToolResultStorage with preprocessor (explicit parameter passing, not context)
		// Find first phase that implements ResultPreprocessor
		if storage, ok := ctx.Value(ctxKeyToolResultStorage).(*ToolResultStorage); ok && storage != nil {
			log.Info("ToolResultStorage found in context, looking for preprocessor in phases", "phase_count", len(phases))
			for _, phase := range phases {
				if preprocessor, ok := phase.(interface {
					PreprocessResult(toolName, rawOutput, filePath string, maxInlineSize int) string
				}); ok {
					storage.Preprocessor = preprocessor
					log.Info("ToolResultStorage preprocessor set", "phase", phase.ID())
					break
				}
			}
			if storage.Preprocessor == nil {
				log.Warn("No phase implements ResultPreprocessor interface - preprocessing disabled")
			}
		} else {
			log.Warn("ToolResultStorage not found in context - preprocessing unavailable")
		}

		// Aggregate tools from all phases with CRUD filtering
		tools := a.aggregatePhaseTools(phases, askCtx)
		log.Info("Phase tools aggregated with CRUD filtering", "tool_count", len(tools), "crud", askCtx.CRUD)

		// Register phase tools with ReAct
		a.registerPhaseTools(ctx, tools, phases, askCtx)
		log.Info("Phase tools registered successfully")
	}

	// 7. Prompt construction and query enrichment (same as V1)
	fullPrompt := a.buildSystemPrompt(ctx, query, *taskContext)
	query = a.enrichQueryWithKBResults(query, taskContext)
	previousStructuredPlanStep := -1
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if thread := a.service.session.Memory.GetCurrentThread(); thread != nil && thread.StreamContext != nil && thread.StreamContext.StructuredPlan != nil {
			previousStructuredPlanStep = thread.StreamContext.StructuredPlan.CurrentStep
		}
	}

	// 8. Delegate to Reasoning engine/ReAct loop (same as V1)
	finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState, escalationState, clarificationRequest, err := a.reactLoop(ctx, query, gen, fullPrompt)
	if err != nil {
		return "", err
	}
	a.updateStructuredPlanProgressAfterTurn(taskContext.AskClassification, toolCalls, outcomeFacts, clarificationRequest)
	a.enqueueStructuredPlanContinuationAfterTurn(query, taskContext.AskClassification, previousStructuredPlanStep, clarificationRequest)
	a.refreshSessionCognitiveAssessment(ctx, query, taskContext.CapabilityHint, finalText, toolCalls, escalationState, clarificationRequest, gen)
	finalText = appendAskCompletionSummary(ctx, finalText, toolCalls, outcomeFacts)
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if thread := a.service.session.Memory.GetCurrentThread(); thread != nil {
			persistAskCompletionState(ctx, thread, finalText, toolCalls, outcomeFacts)
		}
	}

	// 9. Capability lifecycle: OnAfter (same as V1)
	if selectedCap != nil {
		capResult := capabilities.CapabilityResult{
			Summary: finalText,
			Data:    buildCapabilityResultData(toolCalls, outcomeFacts, outcomeRecipes, carryoverState, escalationState, clarificationRequest),
		}
		if err := selectedCap.OnAfter(ctx, query, routingState, capResult); err != nil {
			log.Warn("Capability OnAfter failed", "capability", selectedCap.ID(), "error", err)
		}
		a.notifySupportingPostTurnObservers(ctx, query, routingState, selectedCap, capResult)
	}
	a.maybePersistPlanningArtifactFromFinalText(taskContext.AskClassification, finalText, toolCalls)
	transactionDecision := a.evaluatePhaseTransaction(ctx, rawQuery, taskContext.AskClassification, toolCalls, outcomeFacts, finalText, clarificationRequest)
	log.Info("Phase transaction decision",
		"enqueue_next", transactionDecision.EnqueueNextPhase,
		"next_phase", transactionDecision.NextPhase,
		"reason", transactionDecision.Reason)
	a.persistPhaseTransactionHandoff(rawQuery, taskContext.AskClassification, transactionDecision, toolCalls, outcomeFacts, finalText)
	a.enqueuePhaseTransactionDecision(rawQuery, transactionDecision)

	// Truncate finalText for logging
	finalTextPreview := finalText
	if len(finalTextPreview) > 200 {
		finalTextPreview = finalTextPreview[:200] + "..."
	}
	log.Info("Copilot Ask Complete",
		"session_id", sessionID,
		"response_chars", len(finalText),
		"response_preview", finalTextPreview,
		"tool_calls_count", len(toolCalls))

	// 10. Epilogue and cleanup (same as V1)
	a.epilogueAndCleanup(ctx, rawQuery, intent, finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState, clarificationRequest)

	return finalText, nil
}

func buildDefaultDomainOfferClarification(query string, taskContext *TaskContextClassification) *ai.ClarificationRequest {
	if taskContext == nil || taskContext.AskClassification == nil || !taskContext.AskClassification.DomainOfferNeeded {
		return nil
	}
	question := ""
	if isDaemonPreferredDomainEnvironment() {
		question = "Do you want to work in coding/repo changes or desktop/local commands?"
	} else {
		question = "Do you want to work in Stores or Spaces?"
	}
	return &ai.ClarificationRequest{
		TargetQuery:       strings.TrimSpace(query),
		AssistantQuestion: question,
		MissingFact:       "domain or area of work",
		Reason:            "default domains are only a suggested offer when the ask is ambiguous",
		Source:            compatibleClarificationToolName,
		Status:            "pending",
	}
}

// evaluateAskRoutingGates is the Ask-pipeline version of three-gate routing.
// Gates 1 (Focused) and 2 (Continuity) unchanged from V1.
// Gate 3 (Discovery/Cold Start) uses Phase Factory + LLM classification instead of heuristics.
func (a *CopilotAgent) evaluateAskRoutingGates(ctx context.Context, query string, gen ai.Generator) (*TaskContextClassification, error) {
	log.Info("Copilot Ask routing evaluation start", "query", query)
	a.rehydrateMRUFromMemory(ctx)
	isTest := isRoutingTestGenerator(gen)
	anchor := parseRoutingAnchor(query)

	// Gate 1 (Focused): Specialized routing (unchanged from V1)
	if looksLikeSpecializedRoutingQuery(query) && anchor == nil {
		if handledTaskCtx, handled, err := a.trySpecializedFocusedRouting(ctx, query, "Omni", "Spaces", ""); err != nil {
			return nil, err
		} else if handled {
			return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: handledTaskCtx, matched: true, source: "specialized"}, RoutingGateFocused), nil
		}
	}

	// Gate 1 (Focused): Prefix-based routing (unchanged from V1)
	if taskClassification, err := a.tryPrefixBasedRouting(ctx, query, gen, isTest, anchor); err != nil {
		return nil, err
	} else if taskClassification != nil {
		return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: taskClassification, matched: true, source: "prefix"}, RoutingGateFocused), nil
	}

	// Gate 2 (Continuity): MRU-based continuation (unchanged from V1)
	if !isExplicitNewTopic(ctx) {
		if taskClassification, err := a.tryAskContinuationBasedRouting(ctx, query, gen, isTest, anchor); err != nil {
			return nil, err
		} else if taskClassification != nil {
			return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: taskClassification, matched: true, source: "continuation"}, RoutingGateContinuity), nil
		}
	} else {
		log.Info("Explicit new-topic override: skipping continuation-based routing")
	}

	// Gate 3 (Discovery/Cold Start): Ask pipeline uses Phase Factory + LLM classification
	if taskClassification, err := a.tryAskColdStartRouting(ctx, query, gen, isTest); err != nil {
		return nil, err
	} else if taskClassification != nil {
		return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: taskClassification, matched: true, source: "coldstart_ask"}, RoutingGateDiscovery), nil
	}

	return nil, nil
}

// tryAskColdStartRouting implements Gate 3 (Discovery) using Phase Factory.
// This is the ONLY difference from V1 - pure LLM classification instead of heuristics.
func (a *CopilotAgent) tryAskColdStartRouting(ctx context.Context, query string, gen ai.Generator, isTest bool) (*TaskContextClassification, error) {
	log.Info("Gate 3 (Discovery): Ask Cold Start with Phase Factory")

	// Skip if test mode without generator
	if isTest && gen == nil {
		return nil, nil
	}

	// Use Ask LLM classification (returns domains + phase + plan)
	classification, err := a.ClassifyAskTaskContext(ctx, query, gen)
	if err != nil {
		log.Warn("Ask classification failed", "error", err)
		return nil, err
	}

	log.Info("Ask Classification Result",
		"domains", classification.Domains,
		"phase", classification.Phase,
		"plan", classification.PhasePlan,
		"intent", classification.Intent,
		"confidence", classification.Confidence)

	// Convert Ask classification to TaskContextClassification for compatibility
	taskCtx := &TaskContextClassification{
		Domain:            strings.Join(classification.Domains, ","), // Multi-domain support
		CapabilityHint:    classification.Phase,                      // Phase name as hint
		RoutingGate:       RoutingGateDiscovery,
		AskClassification: classification,
	}

	return taskCtx, nil
}

func (a *CopilotAgent) applyQueuedPhaseActorContext(ctx context.Context, taskContext *TaskContextClassification, persisted *AskTaskContext) *TaskContextClassification {
	actor, _ := ctx.Value(ctxKeyAskWorkItemActor).(string)
	phaseName, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(actor)), "phase:")
	if !ok || strings.TrimSpace(phaseName) == "" {
		return taskContext
	}
	if taskContext == nil {
		taskContext = &TaskContextClassification{}
	}
	phaseName = normalizePhase(phaseName)
	phaseName = canonicalizeCodingRuntimePhase(persisted, phaseName)
	if persisted != nil {
		a.promoteExecutionReadinessFromPlanningArtifact(persisted)
		persisted.Phase = resolveActiveCodingRuntimePhase(persisted, phaseName)
		persisted.PhasePlan = canonicalizeCodingRuntimePlan(persisted, persisted.PhasePlan)
		taskContext.AskClassification = persisted
		if len(persisted.Domains) == 1 {
			taskContext.Domain = persisted.Domains[0]
		}
	}
	taskContext.CapabilityHint = phaseName
	return taskContext
}

func queuedPhaseActor(ctx context.Context) string {
	actor, _ := ctx.Value(ctxKeyAskWorkItemActor).(string)
	phaseName, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(actor)), "phase:")
	if !ok {
		return ""
	}
	return normalizePhase(phaseName)
}

func normalizeCodingRuntimeRouting(taskContext *TaskContextClassification) *TaskContextClassification {
	if taskContext == nil || taskContext.AskClassification == nil {
		return taskContext
	}
	if !askClassificationHasDomain(taskContext.AskClassification.Domains, "coding_workflow") {
		return taskContext
	}
	phase := resolveActiveCodingRuntimePhase(taskContext.AskClassification, taskContext.AskClassification.Phase)
	if phase == "" {
		phase = canonicalizeCodingRuntimePhase(taskContext.AskClassification, taskContext.CapabilityHint)
	}
	if phase == "" {
		phase = "investigation"
	}
	taskContext.CapabilityHint = phase
	taskContext.Domain = "coding_workflow"
	taskContext.AskClassification.PhasePlan = canonicalizeCodingRuntimePlan(taskContext.AskClassification, taskContext.AskClassification.PhasePlan)
	return taskContext
}

func (a *CopilotAgent) getPersistedAskClassification() *AskTaskContext {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return nil
	}
	routingState := a.service.session.Memory.GetRoutingState()
	if routingState == nil {
		return nil
	}
	return cloneAskTaskContext(routingState.AskClassification)
}

// resolvePhasePlan resolves the PhasePlan from classification or WorkStream.
// Returns: (plan, currentPhase)
//
// WORKSTREAM INTEGRATION:
// - Check if resuming from paused WorkStream with existing PhasePlan
// - If resuming: Use existing plan, advance from current index
// - If new: Use LLM-generated plan or default to single phase
// - Store plan in WorkStream via STM for persistence across turns
func (a *CopilotAgent) resolvePhasePlan(ctx context.Context, classification *AskTaskContext) (plan []string, currentPhase string) {
	a.promoteExecutionReadinessFromPlanningArtifact(classification)

	// Get STM accessor for WorkStream integration
	stm := a.getSTMForPhaseCapabilities()
	if stm == nil {
		log.Warn("No STM available for WorkStream integration")
		// Fallback: use classification plan or default
		if len(classification.PhasePlan) > 0 {
			plan = canonicalizeCodingRuntimePlan(classification, classification.PhasePlan)
			return plan, resolveActiveCodingRuntimePhase(classification, plan[0])
		}
		phase := canonicalizeCodingRuntimePhase(classification, classification.Phase)
		return []string{phase}, resolveActiveCodingRuntimePhase(classification, phase)
	}

	// Check if we're resuming an existing WorkStream
	thread := stm.GetCurrentThread()
	if thread != nil {
		streamCtx := thread.GetStreamContext()
		if streamCtx != nil {
			existingPlan := streamCtx.GetPhasePlan()
			if existingPlan != nil {
				// Resuming existing plan - enforce the active coding runtime contract if this is a coding workflow.
				rawPlannedPhases := existingPlan.GetPlannedPhases()
				plannedPhases := canonicalizeCodingRuntimePlan(classification, rawPlannedPhases)
				currentIndex := existingPlan.GetCurrentIndex()
				if actorPhase := queuedPhaseActor(ctx); actorPhase != "" {
					for idx, plannedPhase := range rawPlannedPhases {
						if normalizePhase(plannedPhase) == actorPhase {
							if currentIndex != idx {
								existingPlan.SetCurrentIndex(idx)
								currentIndex = idx
							}
							break
						}
					}
				}

				if currentIndex < len(rawPlannedPhases) {
					currentPhase = resolveActiveCodingRuntimePhase(classification, rawPlannedPhases[currentIndex])
					if queuedPhaseActor(ctx) == "" && currentPhase == "planning" && shouldPromotePersistedCodingPlanningToExecution(classification) {
						for idx, plannedPhase := range rawPlannedPhases {
							if normalizePhase(plannedPhase) == "execution" {
								existingPlan.SetCurrentIndex(idx)
								currentIndex = idx
								currentPhase = "execution"
								break
							}
						}
					}
					a.attachExecutionBriefToCurrentWorkStream(classification, plannedPhases, classification.OriginalQuery)
					log.Info("Resuming WorkStream from existing PhasePlan",
						"plan", plannedPhases,
						"current_index", currentIndex,
						"current_phase", currentPhase)
					return plannedPhases, currentPhase
				}

				// Plan complete - all phases done
				log.Info("WorkStream PhasePlan complete", "plan", plannedPhases)
				return plannedPhases, "" // Empty phase = completed
			}
		}
	}

	// New WorkStream - use classification plan or default
	if len(classification.PhasePlan) > 0 {
		plan = canonicalizeCodingRuntimePlan(classification, classification.PhasePlan)
	} else {
		// Default: single phase from classification
		plan = []string{canonicalizeCodingRuntimePhase(classification, classification.Phase)}
	}

	phaseAnchor := plan[0]
	if classification != nil && classification.ExecutionReady && strings.EqualFold(strings.TrimSpace(classification.Phase), "execution") {
		phaseAnchor = classification.Phase
	}
	currentPhase = resolveActiveCodingRuntimePhase(classification, phaseAnchor)

	// Store PhasePlan in WorkStream for persistence
	checkpoints := make(map[string]bool)
	// Add checkpoint after investigation if multi-phase plan
	if len(plan) > 1 {
		for _, phase := range plan {
			if phase == "investigation" || phase == "planning" {
				checkpoints["after_"+phase] = true
			}
		}
	}

	if err := stm.CreatePhasePlan(plan, checkpoints, classification.Reasoning, classification.Confidence); err != nil {
		log.Warn("Failed to store PhasePlan in WorkStream", "error", err)
	} else {
		a.attachExecutionBriefToCurrentWorkStream(classification, plan, classification.OriginalQuery)
		log.Info("Created PhasePlan in WorkStream",
			"plan", plan,
			"checkpoints", checkpoints,
			"current_phase", currentPhase)
	}

	return plan, currentPhase
}

func shouldPromotePersistedCodingPlanningToExecution(classification *AskTaskContext) bool {
	if classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return false
	}
	if classification.ExecutionReady {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(classification.Phase), "execution") {
		return true
	}
	return useCodingImplementationBundle(classification, "planning")
}

func (a *CopilotAgent) maybePersistPlanningArtifactFromFinalText(classification *AskTaskContext, finalText string, toolCalls []ai.ToolCall) {
	if a == nil || classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(classification.Phase), "planning") {
		return
	}
	if a.hasStoredPlanningArtifact() {
		return
	}
	for _, toolCall := range toolCalls {
		if strings.EqualFold(strings.TrimSpace(toolCall.Name), "planning_capture") {
			return
		}
	}
	artifact, ok := inferStoredPlanningArtifactFromText(finalText)
	if !ok {
		return
	}
	if a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return
	}
	payload, err := json.Marshal(artifact)
	if err != nil {
		return
	}
	_ = a.service.session.Memory.SaveWorkflowMemory("phase-artifact:planning", map[string]string{
		"artifact_json": string(payload),
		"phase":         "planning",
		"status":        artifact.Status,
		"summary":       artifact.Summary,
	})
	a.attachExecutionBriefToCurrentWorkStream(classification, classification.PhasePlan, classification.OriginalQuery)
}

func inferStoredPlanningArtifactFromText(finalText string) (storedPlanningArtifact, bool) {
	trimmed := strings.TrimSpace(finalText)
	if trimmed == "" {
		return storedPlanningArtifact{}, false
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "execution-ready plan") {
		return storedPlanningArtifact{}, false
	}
	lines := strings.Split(trimmed, "\n")
	summary := ""
	plan := make([]string, 0)
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if summary == "" && strings.Contains(strings.ToLower(line), "execution-ready plan") {
			summary = line
		}
		if step, ok := parseNumberedPlanLine(line); ok {
			plan = append(plan, step)
		}
	}
	if summary == "" {
		summary = "Execution-ready plan captured from planning response"
	}
	if len(plan) == 0 {
		return storedPlanningArtifact{}, false
	}
	return storedPlanningArtifact{
		Summary: summary,
		Status:  "ready_for_execution",
		Plan:    plan,
	}, true
}

func parseNumberedPlanLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	idx := 0
	for idx < len(trimmed) {
		r := rune(trimmed[idx])
		if !unicode.IsDigit(r) {
			break
		}
		idx++
	}
	if idx == 0 || idx >= len(trimmed) || trimmed[idx] != '.' {
		return "", false
	}
	step := strings.TrimSpace(trimmed[idx+1:])
	if step == "" {
		return "", false
	}
	return step, true
}

// canonicalizeCodingRuntimePhase enforces the active coding runtime contract.
// Coding workflow accepts only investigation, planning, and execution; any other
// coding phase token is folded into the active runtime surface rather than reviving
// a broader SDLC model.
func canonicalizeCodingRuntimePhase(classification *AskTaskContext, phase string) string {
	rawPhase := strings.TrimSpace(strings.ToLower(phase))
	if classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return normalizePhase(rawPhase)
	}
	switch rawPhase {
	case "", "intake":
		return "investigation"
	case "testing", "review", "delivery":
		return "execution"
	default:
		return normalizePhase(rawPhase)
	}
}

func resolveActiveCodingRuntimePhase(classification *AskTaskContext, phase string) string {
	resolved := canonicalizeCodingRuntimePhase(classification, phase)
	if classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return resolved
	}
	if strings.TrimSpace(strings.ToLower(classification.OrchestrationLevel)) == orchestrationManagedWorkflow {
		if resolved == "planning" {
			return "planning"
		}
		if classification.ExecutionReady && resolved == "execution" {
			return "execution"
		}
		return "investigation"
	}
	if resolved == "planning" {
		return "planning"
	}
	return "investigation"
}

func canonicalizeCodingRuntimePlan(classification *AskTaskContext, phases []string) []string {
	if len(phases) == 0 {
		return phases
	}
	if classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		cloned := append([]string(nil), phases...)
		for idx, phase := range cloned {
			cloned[idx] = normalizePhase(phase)
		}
		return cloned
	}
	normalized := make([]string, 0, len(phases))
	for _, phase := range phases {
		mapped := canonicalizeCodingRuntimePhase(classification, phase)
		if mapped == "" {
			continue
		}
		if len(normalized) == 0 || normalized[len(normalized)-1] != mapped {
			normalized = append(normalized, mapped)
		}
	}
	if len(normalized) == 0 {
		return []string{"investigation", "planning", "execution"}
	}
	return normalized
}

func askClassificationHasDomain(domains []string, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	if want == "" {
		return false
	}
	for _, domain := range domains {
		if strings.TrimSpace(strings.ToLower(domain)) == want {
			return true
		}
	}
	return false
}

// createPhaseProxies creates domain-specific phase capabilities via Phase Factory.
//
// USER SPECIFICATION:
// For EACH domain in classification:
//  1. Invoke: NewPhase(domain, phaseID, sharedInfra)
//  2. Factory returns domain-specific phase (StoresPhase, SpacesPhase, etc.)
//  3. Each phase is a proxy/delegator for that domain+phase combination
//
// Example:
//
//	domains=["stores", "spaces"], phase="investigation"
//	→ StoresPhase("investigation") + SpacesPhase("investigation")
func (a *CopilotAgent) createPhaseProxies(domains []string, currentPhase string, classification *AskTaskContext) []capabilities.Capability {
	phases := []capabilities.Capability{}

	// Get or create shared infrastructure for phases
	infra := a.getOrCreateSharedInfra()

	// Create phase proxy for each domain
	for _, domain := range domains {
		for _, phaseID := range resolveDomainToolPhases(domain, currentPhase, classification) {
			log.Info("Creating phase proxy",
				"domain", domain,
				"phase", phaseID)

			phase := planner.NewPhase(domain, phaseID, infra)
			if phase != nil {
				phases = append(phases, phase)
				log.Info("Phase proxy created",
					"domain", domain,
					"phase", phaseID,
					"capability_id", phase.ID())
			} else {
				log.Warn("Phase Factory returned nil",
					"domain", domain,
					"phase", phaseID)
			}
		}
	}

	return phases
}

func resolveDomainToolPhases(domain, currentPhase string, classification *AskTaskContext) []string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	currentPhase = normalizePhase(currentPhase)
	if currentPhase == "" {
		currentPhase = "investigation"
	}
	if domain == "coding_workflow" && classification != nil && strings.TrimSpace(strings.ToLower(classification.OrchestrationLevel)) == orchestrationManagedWorkflow {
		switch currentPhase {
		case "planning":
			return []string{"investigation", "planning", "execution"}
		case "investigation":
			// Expose investigation, planning, AND execution so LLM can run tests during investigation
			return []string{"investigation", "planning", "execution"}
		}
	}
	return []string{currentPhase}
}

func useCodingImplementationBundle(classification *AskTaskContext, currentPhase string) bool {
	currentPhase = normalizePhase(currentPhase)
	switch currentPhase {
	case "execution", "review", "delivery":
		return true
	}
	if classification == nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(classification.Intent)) {
	case "generate_code", "mutate_data":
		return true
	}
	for _, crud := range classification.CRUD {
		switch strings.TrimSpace(strings.ToUpper(crud)) {
		case "C", "U", "D":
			return true
		}
	}
	if containsRepoFixIntent(*classification) {
		return true
	}
	query := strings.ToLower(strings.TrimSpace(classification.OriginalQuery))
	for _, marker := range []string{"fix", "patch", "implement", "update", "modify", "change", "write", "failing test", "tests fail", "bug"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

// aggregatePhaseTools collects tools from ALL phase proxies.
//
// USER SPECIFICATION:
// - Collect ALL tools from all phases
// - Phase 3 optimization: Apply CRUD filtering based on classification
// - LLM will decide which tools to call from the filtered set
func (a *CopilotAgent) aggregatePhaseTools(phases []capabilities.Capability, classification *AskTaskContext) []capabilities.ToolDefinition {
	allTools := []capabilities.ToolDefinition{}

	// Collect CRUD flags from classification
	crudFlags := make(map[string]bool)
	for _, crud := range classification.CRUD {
		crudFlags[crud] = true
	}

	for _, phase := range phases {
		phaseTools := phase.ExposeTools()
		phaseTools = filterCodingRuntimeTools(phaseTools, classification)

		// Phase 3 optimization: Filter tools by CRUD flags
		filteredTools := capabilities.FilterToolsByCRUD(phaseTools, crudFlags)

		allTools = append(allTools, filteredTools...)
		log.Debug("Phase tools collected and filtered",
			"phase_id", phase.ID(),
			"original_tool_count", len(phaseTools),
			"filtered_tool_count", len(filteredTools),
			"crud_flags", classification.CRUD)
	}

	log.Info("All tools aggregated with CRUD filtering",
		"total_tool_count", len(allTools),
		"phase_count", len(phases),
		"crud_flags", classification.CRUD)

	return allTools
}

func filterCodingRuntimeTools(tools []capabilities.ToolDefinition, classification *AskTaskContext) []capabilities.ToolDefinition {
	if classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return tools
	}
	filtered := make([]capabilities.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		switch strings.TrimSpace(tool.Name) {
		case "planning_assess_and_route", "planning_create_succession_path":
			continue
		default:
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// registerPhaseTools registers all phase tools in the tool registry.
//
// USER SPECIFICATION:
// - Register ALL tools from all phases
// - NO filtering based on heuristics
// - Each tool handler routes to appropriate phase's ExecuteTool method
//
// PARTICIPANT PATTERN:
// - Tool handler calls phase.ExecuteTool(toolName, args, routingState)
// - Phase executes ONE tool operation and returns result
// - Phase does NOT enqueue work, call other tools, or control flow
// - Ask pipeline is the orchestrator, not the phase
func (a *CopilotAgent) registerPhaseTools(ctx context.Context, allTools []capabilities.ToolDefinition, phases []capabilities.Capability, classification *AskTaskContext) {
	// Build routing state for ExecuteTool calls
	// ✅ EXPLICIT PARAMETERS: routingState passed to ExecuteTool, not hidden in context
	// ✅ Phase 3 optimization: Include CRUD flags for tool filtering
	crudFlags := make(map[string]bool)
	for _, crud := range classification.CRUD {
		crudFlags[crud] = true
	}

	routingState := capabilities.RoutingState{
		Domain:         strings.Join(classification.Domains, ","),
		Gate:           "v2_phase_factory",
		CapabilityHint: classification.Phase,
		CRUDFlags:      crudFlags,
		SessionID:      getSessionIDFromContext(ctx),
	}

	// Register each tool
	for _, toolDef := range allTools {
		toolName := toolDef.Name
		toolDescription := toolDef.Description
		toolSchema := toolDef.Schema

		// Determine which phase owns this tool (by tool name prefix)
		owningPhase := a.findOwningPhase(toolName, phases)

		// Create handler that routes to phase's ExecuteTool
		// ✅ PARTICIPANT PATTERN: Handler calls phase.ExecuteTool(), phase returns result
		// ❌ NEVER: Phase controls flow or enqueues multiple work items
		handler := func(ctx context.Context, args map[string]any) (string, error) {
			log.Info("[TOOL HANDLER] Called", "tool", toolName, "args", args, "phase", func() string {
				if owningPhase != nil {
					return owningPhase.ID()
				}
				return "unknown"
			}())

			if owningPhase == nil {
				return "", fmt.Errorf("no owning phase found for tool: %s", toolName)
			}

			// Route to phase's ExecuteTool
			// Phase executes ONE tool, returns result - that's it
			if executor, ok := owningPhase.(capabilities.ToolExecutor); ok {
				result, err := executor.ExecuteTool(ctx, toolName, args, routingState)
				log.Info("[TOOL HANDLER] ExecuteTool returned", "tool", toolName, "result_len", len(result), "err", err)
				return result, err
			}

			return "", fmt.Errorf("phase %s does not implement ToolExecutor", owningPhase.ID())
		}

		// Register in tool registry
		a.registry.Register(toolName, toolDescription, toolSchema, wrapStringTool(handler))

		log.Debug("Phase tool registered",
			"tool", toolName,
			"owning_phase", func() string {
				if owningPhase != nil {
					return owningPhase.ID()
				}
				return "unknown"
			}())
	}

	log.Info("Phase tools registration complete",
		"aggregated_tool_count", len(allTools),
		"registered_unique_count", len(a.registry.List()),
		"deduplication_savings", len(allTools)-len(a.registry.List()))
}

// findOwningPhase determines which phase capability owns a given tool.
// Tool names are prefixed with phase name (e.g., "investigation_list_stores")
func (a *CopilotAgent) findOwningPhase(toolName string, phases []capabilities.Capability) capabilities.Capability {
	for _, phase := range phases {
		// Check if tool appears in this phase's exposed tools
		for _, tool := range phase.ExposeTools() {
			if tool.Name == toolName {
				return phase
			}
		}
	}
	return nil
}

// buildPhasePrompt builds unified system prompt from all phase proxies.
//
// USER SPECIFICATION:
// - Call BuildInstructions() on each phase proxy
// - Aggregate domain+phase-specific instructions
// - Include conversation history
// - Result: Unified system prompt with all phase guidance
func (a *CopilotAgent) buildPhasePrompt(ctx context.Context, query string, classification *AskTaskContext, phases []capabilities.Capability) string {
	builder := NewSystemPromptBuilder()

	// 1. Base persona
	persona, _ := a.resolvePersonaWithMetadata(ctx)
	builder.With(ComponentPersona, persona)

	// 2. Semantic memory / LTM
	semanticMemory := a.getLTMSemanticContext(ctx, query)
	builder.With(ComponentSemanticMemory, semanticMemory)

	// 3. System tools context
	systemTools := a.getSystemToolsContext(ctx)
	builder.With(ComponentSystemTools, systemTools)

	// 4. Phase-specific instructions from ALL phases
	var phaseInstructions []string

	// Build CRUD flags map for routing state
	crudFlags := make(map[string]bool)
	for _, crud := range classification.CRUD {
		crudFlags[crud] = true
	}

	for _, phase := range phases {
		routingState := capabilities.RoutingState{
			Domain:         strings.Join(classification.Domains, ","),
			Gate:           "v2_phase_factory",
			CapabilityHint: classification.Phase,
			CRUDFlags:      crudFlags,
		}

		// Get context, instructions, and structured guidance from phase
		context := phase.BuildContext(query, routingState)
		instructions := phase.BuildInstructions(query, routingState)
		guidance := capabilities.RenderCapabilityGuidance(phase, query, routingState)

		combined := ""
		if context != "" {
			combined = context
		}
		if guidance != "" {
			if combined != "" {
				combined += "\n\n" + guidance
			} else {
				combined = guidance
			}
		}
		if instructions != "" {
			if combined != "" {
				combined += "\n\n" + instructions
			} else {
				combined = instructions
			}
		}

		if combined != "" {
			phaseInstructions = append(phaseInstructions, combined)
		}
	}

	// Add the compact synthesis first so the model prioritizes the dynamic steering signal.
	synthesis := buildTaskGuidanceSynthesis(query, TaskContextClassification{
		Domain:         strings.Join(classification.Domains, ","),
		CapabilityHint: classification.Phase,
	})

	phaseContext := ""
	if synthesis != "" {
		phaseContext = synthesis + "\n\n"
	}
	if handoff := a.renderPhaseTransactionHandoff(classification, query); handoff != "" {
		phaseContext += handoff + "\n\n"
	}
	if executionBrief := renderExecutionBrief(classification.Phase, a.currentExecutionBrief(classification, query)); executionBrief != "" {
		phaseContext += executionBrief + "\n\n"
	}

	phaseContext += fmt.Sprintf("=== Phase System V2 ===\nCurrent Phase: %s\nDomains: %s\nPhasePlan: %s\n\n%s",
		classification.Phase,
		strings.Join(classification.Domains, ", "),
		strings.Join(classification.PhasePlan, " → "),
		strings.Join(phaseInstructions, "\n\n---\n\n"))

	// Gap Fix #1: Include classification reasoning to help LLM make better tool decisions
	if classification.Reasoning != "" {
		phaseContext += fmt.Sprintf("\n\n=== Classification Context ===\n%s\n", classification.Reasoning)
	}

	builder.With(ComponentFocusedContext, phaseContext)

	// 5. Conversation history
	history := a.getSessionMemoryContext()
	builder.With(ComponentHistory, history)

	// 6. User query
	builder.With(ComponentUserQuery, "User: "+query)

	// Build final prompt
	fullPrompt, budgetReport := builder.ToJSONWithBudgetReport(a.promptBudgetProfile(TaskContextClassification{
		Domain: strings.Join(classification.Domains, ","),
	}))

	log.Debug("System prompt built (Ask phase pipeline)",
		"original_chars", budgetReport.OriginalTotalChars,
		"final_chars", budgetReport.FinalTotalChars,
		"phase_count", len(phases))

	return fullPrompt
}

// isPhaseComplete determines if the current phase is complete.
//
// IMPLEMENTATION:
// Checks the actual artifact status instead of heuristics.
// Phase is complete when artifact status is one of:
// - "complete", "approved", "delivered"
func (a *CopilotAgent) isPhaseComplete(ctx context.Context, currentPhase string, toolCalls []ai.ToolCall, response string) bool {
	// Check if capture tool was called
	captureToolCalled := false
	for _, tc := range toolCalls {
		if strings.Contains(tc.Name, "capture") {
			captureToolCalled = true
			break
		}
	}

	if !captureToolCalled {
		return false // Not complete until artifact captured
	}

	// Load the actual artifact and check its status
	stm := a.getSTMForPhaseCapabilities()
	if stm == nil {
		// Fallback to heuristic if STM not available
		return len(toolCalls) > 0
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return len(toolCalls) > 0
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return len(toolCalls) > 0
	}

	// Get artifact for current phase
	artifacts := streamCtx.GetPhaseArtifacts()
	if phaseArtifacts, ok := artifacts[currentPhase]; ok {
		// PhaseArtifacts is map[string]any with artifact data
		if phaseArtifacts != nil {
			status, _ := phaseArtifacts["status"].(string)
			status = strings.ToLower(strings.TrimSpace(status))
			// Phase is complete when status indicates completion
			return status == "complete" || status == "approved" || status == "delivered"
		}
	}

	// Fallback: If artifact not found but capture was called, assume complete
	// (artifact may be in different storage location)
	return captureToolCalled
}

// getNextPhase returns the next phase in the plan after currentPhase.
// Returns empty string if no more phases.
func (a *CopilotAgent) getNextPhase(plan []string, currentPhase string) string {
	for i, phase := range plan {
		if phase == currentPhase && i+1 < len(plan) {
			return plan[i+1]
		}
	}
	return ""
}

// getOrCreateSharedInfra returns or creates shared infrastructure for phases.
// WORKSTREAM INTEGRATION: Injects STM, PhasePlanMutator, Queue accessor
func (a *CopilotAgent) getOrCreateSharedInfra() *planner.SharedInfrastructure {
	infra := planner.NewDefaultSharedInfrastructure()

	// Inject STM accessor for phase succession (explicit parameter passing, not context)
	infra.STMAccessor = a.getSTMForPhaseCapabilities

	// Inject session queue accessor for work item enqueueing (Participant Pattern)
	infra.GetSessionQueue = a.getSessionQueueForCapabilities

	// Reuse the existing local automation command path for desktop phase tools.
	infra.LocalCommandExecutor = a.toolExecuteLocalCommand
	infra.LLMAccessor = func(ctx context.Context) (any, error) {
		if a == nil || a.brain == nil {
			return nil, fmt.Errorf("LLM (brain) not available")
		}
		return a.brain, nil
	}
	infra.PlanningContextResolver = a.resolvePlanningContextForDomain
	cw.SetDefaultPlanningContextResolver(infra.PlanningContextResolver)
	infra.Planner = cw.NewLLMPlanner(infra.LLMAccessor, infra.Planner)

	// Inject PhasePlanMutator for dynamic phase plan modification (LLM orchestration)
	infra.PhasePlanMutator = planner.NewSTMPhasePlanMutator(a.getSTMForPhaseCapabilities)
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		infra.Memory = &sessionWorkflowMemoryAdapter{stm: a.service.session.Memory}
	}

	// Enable adaptive classification and dynamic plan mutation (LLM orchestration)
	classificationHistory := planner.NewClassificationHistory()
	infra.PhaseClassificationStrategy = planner.NewAdaptiveStrategy(
		planner.WrapGenerator(a.brain), // LLM generator for classification escalation
		classificationHistory,
	)
	infra.ClassificationHistory = classificationHistory

	return infra
}
