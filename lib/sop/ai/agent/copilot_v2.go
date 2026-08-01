package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/planner"
)

// ============================================================================
// AskV2 Pipeline - Critical Architectural Patterns
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
//    ✅ AskV2() controls: classification → phase creation → tool registration → ReAct
//    ✅ AskV2() decides: when phase complete, which phase next, enqueue work
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
//   AskV2() → createPhases(domains, phase) → registerTools()
//   → reactLoop() → LLM calls tool → phase.ExecuteTool(toolName, args)
//   → phase returns result → AskV2() checks completion
//   → AskV2() enqueues next phase if needed
//
// ============================================================================

// AskV2 is the next-generation Ask pipeline implementing pure LLM-as-Orchestrator pattern.
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
func (a *CopilotAgent) AskV2(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
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

		// Execute ask (LLM orchestrates WorkStream management via tools)
		response, err := a.askV2Core(ctx, item.Query, cfg)
		if err != nil {
			return response, err
		}

		lastResponse = response

		// ReAct returns either:
		// - Completed answer: continue to next queue item
		// - Clarification question: pause queue, return to user
		// Capabilities may have enqueued next steps during execution
	}

	return lastResponse, nil
}

// askV2Core is the core V2 Ask→ReAct transaction with V2 routing for Gate 3.
// Preserves all V1 infrastructure (Gates 1/2, intent, KB, lifecycle, epilogue).
// ONLY Cold Start gate (Gate 3) uses Phase Factory + LLM classification.
func (a *CopilotAgent) askV2Core(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
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

	// 3. Intent classification and routing (same as V1)
	intent := a.classifyIntent(ctx, query, gen)
	if intent != ai.IntentOmni {
		log.Info("Ask: Request classified for Avatar", "avatar", intent)
		return a.executeAvatarSubAgent(ctx, intent, query)
	}

	// 4. Three-gate context classification (V2: uses Phase Factory for Gate 3)
	taskContext, err := a.evaluateRoutingGatesV2(ctx, query, gen)
	if err != nil {
		log.Info("Copilot AskV2 routing evaluation failed", "query", query, "error", err)
		return "", err
	}
	if taskContext == nil {
		taskContext = &TaskContextClassification{}
	}
	log.Info("Copilot AskV2 Routing", "routing_gate", taskContext.RoutingGate, "task_context", summarizeTaskContextForLog(*taskContext), "query", query)

	// 5. KB direct display fast path (same as V1)
	if a.shouldDirectDisplayKB(taskContext) {
		return a.handleKBDirectDisplay(ctx, rawQuery, intent, taskContext)
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

	// 6b. V2 Phase Factory Integration (only for Gate 3 Discovery)
	if v2Class, ok := ctx.Value("v2_classification").(*TaskContextV2); ok && v2Class != nil {
		log.Info("Using V2 Phase Factory for WorkStream + tool registration")

		// Resolve phase plan from WorkStream or classification
		plan, currentPhase := a.resolvePhasePlanV2(ctx, v2Class)
		log.Info("Phase plan resolved", "plan", plan, "current_phase", currentPhase)

		// Create domain-specific phase proxies
		phases := a.createPhaseProxiesV2(v2Class.Domains, currentPhase)
		log.Info("Phase proxies created", "count", len(phases), "domains", v2Class.Domains)

		// Aggregate tools from all phases with CRUD filtering
		tools := a.aggregatePhaseToolsV2(phases, v2Class)
		log.Info("Phase tools aggregated with CRUD filtering", "tool_count", len(tools), "crud", v2Class.CRUD)

		// Register phase tools with ReAct
		a.registerPhaseToolsV2(ctx, tools, phases, v2Class)
		log.Info("Phase tools registered successfully")
	}

	// 7. Prompt construction and query enrichment (same as V1)
	fullPrompt := a.buildSystemPrompt(ctx, query, *taskContext)
	query = a.enrichQueryWithKBResults(query, taskContext)

	// 8. Delegate to Reasoning engine/ReAct loop (same as V1)
	finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState, escalationState, err := a.reactLoop(ctx, query, gen, fullPrompt)
	if err != nil {
		return "", err
	}

	// 9. Capability lifecycle: OnAfter (same as V1)
	if selectedCap != nil {
		capResult := capabilities.CapabilityResult{
			Summary: finalText,
			Data: map[string]any{
				"tool_calls":        toolCalls,
				"outcome_facts":     outcomeFacts,
				"outcome_recipes":   outcomeRecipes,
				"carryover_state":   carryoverState,
				"escalation_state":  escalationState,
			},
		}
		if err := selectedCap.OnAfter(ctx, query, routingState, capResult); err != nil {
			log.Warn("Capability OnAfter failed", "capability", selectedCap.ID(), "error", err)
		}
	}

	log.Info("Copilot AskV2 Complete", "session_id", sessionID, "response_chars", len(finalText))

	// 10. Epilogue and cleanup (same as V1)
	a.epilogueAndCleanup(ctx, rawQuery, intent, finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState)

	return finalText, nil
}

// evaluateRoutingGatesV2 is the V2 version of three-gate routing.
// Gates 1 (Focused) and 2 (Continuity) unchanged from V1.
// Gate 3 (Discovery/Cold Start) uses Phase Factory + LLM classification instead of heuristics.
func (a *CopilotAgent) evaluateRoutingGatesV2(ctx context.Context, query string, gen ai.Generator) (*TaskContextClassification, error) {
	log.Info("Copilot AskV2 routing evaluation start", "query", query)
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

	// Gate 3 (Discovery/Cold Start): V2 uses Phase Factory + LLM classification
	if taskClassification, err := a.tryColdStartBasedRoutingV2(ctx, query, gen, isTest); err != nil {
		return nil, err
	} else if taskClassification != nil {
		return a.finalizeRoutingResult(ctx, query, &routingOutcome{taskCtx: taskClassification, matched: true, source: "coldstart_v2"}, RoutingGateDiscovery), nil
	}

	return nil, nil
}

// tryColdStartBasedRoutingV2 implements Gate 3 (Discovery) using Phase Factory.
// This is the ONLY difference from V1 - pure LLM classification instead of heuristics.
func (a *CopilotAgent) tryColdStartBasedRoutingV2(ctx context.Context, query string, gen ai.Generator, isTest bool) (*TaskContextClassification, error) {
	log.Info("Gate 3 (Discovery): V2 Cold Start with Phase Factory")

	// Skip if test mode without generator
	if isTest && gen == nil {
		return nil, nil
	}

	// Use V2 LLM classification (returns domains + phase + plan)
	classification, err := a.ClassifyTaskContextV2(ctx, query, gen)
	if err != nil {
		log.Warn("V2 classification failed", "error", err)
		return nil, err
	}

	log.Info("V2 Classification Result",
		"domains", classification.Domains,
		"phase", classification.Phase,
		"plan", classification.PhasePlan,
		"intent", classification.Intent,
		"confidence", classification.Confidence)

	// Convert V2 classification to V1 TaskContextClassification for compatibility
	taskCtx := &TaskContextClassification{
		Domain:         strings.Join(classification.Domains, ","), // Multi-domain support
		CapabilityHint: classification.Phase,                      // Phase name as hint
		RoutingGate:    RoutingGateDiscovery,
	}

	// Store V2-specific metadata in context for later use
	ctx = context.WithValue(ctx, "v2_classification", classification)
	ctx = context.WithValue(ctx, "v2_phase_plan", classification.PhasePlan)

	return taskCtx, nil
}

// resolvePhasePlanV2 resolves the PhasePlan from classification or WorkStream.
// Returns: (plan, currentPhase)
//
// WORKSTREAM INTEGRATION:
// - Check if resuming from paused WorkStream with existing PhasePlan
// - If resuming: Use existing plan, advance from current index
// - If new: Use LLM-generated plan or default to single phase
// - Store plan in WorkStream via STM for persistence across turns
func (a *CopilotAgent) resolvePhasePlanV2(ctx context.Context, classification *TaskContextV2) (plan []string, currentPhase string) {
	// Get STM accessor for WorkStream integration
	stm := a.getSTMForPhaseCapabilities()
	if stm == nil {
		log.Warn("No STM available for WorkStream integration")
		// Fallback: use classification plan or default
		if len(classification.PhasePlan) > 0 {
			return classification.PhasePlan, classification.PhasePlan[0]
		}
		return []string{classification.Phase}, classification.Phase
	}

	// Check if we're resuming an existing WorkStream
	thread := stm.GetCurrentThread()
	if thread != nil {
		streamCtx := thread.GetStreamContext()
		if streamCtx != nil {
			existingPlan := streamCtx.GetPhasePlan()
			if existingPlan != nil {
				// Resuming existing plan - use cursor to determine current phase
				plannedPhases := existingPlan.GetPlannedPhases()
				currentIndex := existingPlan.GetCurrentIndex()

				if currentIndex < len(plannedPhases) {
					log.Info("Resuming WorkStream from existing PhasePlan",
						"plan", plannedPhases,
						"current_index", currentIndex,
						"current_phase", plannedPhases[currentIndex])
					return plannedPhases, plannedPhases[currentIndex]
				}

				// Plan complete - all phases done
				log.Info("WorkStream PhasePlan complete", "plan", plannedPhases)
				return plannedPhases, "" // Empty phase = completed
			}
		}
	}

	// New WorkStream - use classification plan or default
	if len(classification.PhasePlan) > 0 {
		plan = classification.PhasePlan
	} else {
		// Default: single phase from classification
		plan = []string{classification.Phase}
	}

	currentPhase = plan[0]

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
		log.Info("Created PhasePlan in WorkStream",
			"plan", plan,
			"checkpoints", checkpoints,
			"current_phase", currentPhase)
	}

	return plan, currentPhase
}

// createPhaseProxiesV2 creates domain-specific phase capabilities via Phase Factory.
//
// USER SPECIFICATION:
// For EACH domain in classification:
//  1. Invoke: NewPhase(domain, currentPhase, sharedInfra)
//  2. Factory returns domain-specific phase (StoresPhase, SpacesPhase, etc.)
//  3. Each phase is a proxy/delegator for that domain+phase combination
//
// Example:
//
//	domains=["stores", "spaces"], phase="investigation"
//	→ StoresPhase("investigation") + SpacesPhase("investigation")
func (a *CopilotAgent) createPhaseProxiesV2(domains []string, currentPhase string) []capabilities.Capability {
	phases := []capabilities.Capability{}

	// Get or create shared infrastructure for phases
	infra := a.getOrCreateSharedInfra()

	// Create phase proxy for each domain
	for _, domain := range domains {
		log.Info("Creating phase proxy",
			"domain", domain,
			"phase", currentPhase)

		phase := planner.NewPhase(domain, currentPhase, infra)
		if phase != nil {
			phases = append(phases, phase)
			log.Info("Phase proxy created",
				"domain", domain,
				"phase", currentPhase,
				"capability_id", phase.ID())
		} else {
			log.Warn("Phase Factory returned nil",
				"domain", domain,
				"phase", currentPhase)
		}
	}

	return phases
}

// aggregatePhaseToolsV2 collects tools from ALL phase proxies.
//
// USER SPECIFICATION:
// - Collect ALL tools from all phases
// - Phase 3 optimization: Apply CRUD filtering based on classification
// - LLM will decide which tools to call from the filtered set
func (a *CopilotAgent) aggregatePhaseToolsV2(phases []capabilities.Capability, classification *TaskContextV2) []capabilities.ToolDefinition {
	allTools := []capabilities.ToolDefinition{}

	// Collect CRUD flags from classification
	crudFlags := make(map[string]bool)
	for _, crud := range classification.CRUD {
		crudFlags[crud] = true
	}

	for _, phase := range phases {
		phaseTools := phase.ExposeTools()

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

// registerPhaseToolsV2 registers all phase tools in the tool registry.
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
// - Ask pipeline (AskV2) is the orchestrator, not the phase
func (a *CopilotAgent) registerPhaseToolsV2(ctx context.Context, allTools []capabilities.ToolDefinition, phases []capabilities.Capability, classification *TaskContextV2) {
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
			if owningPhase == nil {
				return "", fmt.Errorf("no owning phase found for tool: %s", toolName)
			}

			// Route to phase's ExecuteTool
			// Phase executes ONE tool, returns result - that's it
			if executor, ok := owningPhase.(capabilities.ToolExecutor); ok {
				return executor.ExecuteTool(ctx, toolName, args, routingState)
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

	log.Info("All phase tools registered", "tool_count", len(allTools))
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

// buildSystemPromptV2 builds unified system prompt from all phase proxies.
//
// USER SPECIFICATION:
// - Call BuildInstructions() on each phase proxy
// - Aggregate domain+phase-specific instructions
// - Include conversation history
// - Result: Unified system prompt with all phase guidance
func (a *CopilotAgent) buildSystemPromptV2(ctx context.Context, query string, classification *TaskContextV2, phases []capabilities.Capability) string {
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

		// Get context and instructions from phase
		context := phase.BuildContext(query, routingState)
		instructions := phase.BuildInstructions(query, routingState)

		combined := ""
		if context != "" {
			combined = context
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

	// Add phase context header
	phaseContext := fmt.Sprintf("=== Phase System V2 ===\nCurrent Phase: %s\nDomains: %s\nPhasePlan: %s\n\n%s",
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

	log.Debug("System prompt built (V2)",
		"original_chars", budgetReport.OriginalTotalChars,
		"final_chars", budgetReport.FinalTotalChars,
		"phase_count", len(phases))

	return fullPrompt
}

// isPhaseCompleteV2 determines if the current phase is complete.
//
// IMPLEMENTATION:
// Checks the actual artifact status instead of heuristics.
// Phase is complete when artifact status is one of:
// - "complete", "approved", "delivered"
func (a *CopilotAgent) isPhaseCompleteV2(ctx context.Context, currentPhase string, toolCalls []ai.ToolCall, response string) bool {
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

	// Inject PhasePlanMutator for dynamic phase plan modification (LLM orchestration)
	infra.PhasePlanMutator = planner.NewSTMPhasePlanMutator(a.getSTMForPhaseCapabilities)

	// Enable adaptive classification and dynamic plan mutation (LLM orchestration)
	classificationHistory := planner.NewClassificationHistory()
	infra.PhaseClassificationStrategy = planner.NewAdaptiveStrategy(
		planner.WrapGenerator(a.brain), // LLM generator for classification escalation
		classificationHistory,
	)
	infra.ClassificationHistory = classificationHistory

	return infra
}
