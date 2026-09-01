package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/metrics"
)

func (a *CopilotAgent) renderToolDefinitionContext(title string, toolNames []string) string {
	if a == nil || a.registry == nil || strings.TrimSpace(title) == "" {
		return ""
	}
	var lines []string
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		toolDef, ok := a.registry.Get(name)
		if !ok {
			continue
		}
		description := strings.Join(strings.Fields(strings.TrimSpace(toolDef.Description)), " ")
		if description == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", toolDef.Name, description))
	}
	if len(lines) == 0 {
		return ""
	}
	return title + "\n" + strings.Join(lines, "\n")
}

func (a *CopilotAgent) buildStoresToolDescriptionContext() string {
	return strings.Join([]string{
		"Structured Context: Stores Tools",
		"You are a Stores Database Expert Agent. Translate requests into executable store operations.",
		"CRITICAL RULES:",
		"1. Never guess store names, field names, or join mappings. Use list_stores first whenever schema, relations, or field paths are uncertain.",
		"2. Think through the read/join/filter plan before writing the operation.",
		"3. SOP store queries are not SQL. When the user asks for a query or filter, produce SOP-native JSON tool/script operations, not SQL syntax.",
		"4. When you generate a multi-step store plan using steps such as begin_tx, open_store, scan, filter, sort, project, commit_tx, rollback_tx or return, place those steps inside execute_script.script and call execute_script to submit the plan to the executor. The last step's result is returned automatically or use return to explicitly specify the return value.",
		"5. If execution fails, analyze the error, rewrite the operation, and retry once. If it still fails, ask the user a short clarification question.",
	}, "\n")
}

func buildTaskGuidanceSynthesis(query string, taskClassification TaskContextClassification) string {
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return ""
	}

	if isCodingWorkflowTaskClassification(taskClassification) {
		phase := inferCodingPhase(taskClassification, trimmedQuery)
		if phase == "" {
			phase = "investigation"
		}

		lines := []string{
			"Task Guidance Synthesis",
			fmt.Sprintf("- Selected Phase: %s", phase),
			fmt.Sprintf("- Selected Domain: %s", strings.TrimSpace(taskClassification.Domain)),
			"- Runtime contract: phase semantics are internal continuity metadata for coding tasks; do not wait for explicit phase handoffs inside this Ask/ReAct turn.",
		}

		switch phase {
		case "execution":
			lines = append(lines, "- Primary directive: use the available coding tools adaptively to inspect, patch, validate, and complete the task in one continuous Ask/ReAct turn unless blocked by clarification or approval requirements.")
			lines = append(lines, "- Discovery inside implementation is allowed: if details are missing, inspect or plan just enough, then continue directly to the code/test change in the same turn.")
		case "planning":
			lines = append(lines, "- Primary directive: turn the gathered evidence into a concrete, ordered implementation plan with focused validation steps.")
			lines = append(lines, "- Do not stop at vague planning notes; produce a detailed plan artifact that can drive the next execution turn.")
		default:
			lines = append(lines, "- Primary directive: gather the evidence or design clarity needed to act safely, then continue directly into implementation in the same Ask/ReAct turn when the next step becomes obvious.")
			lines = append(lines, "- Do not stop after a plan unless the user asked for design-only work, explicit review, or approval before implementation.")
		}

		lower := strings.ToLower(trimmedQuery)
		if strings.Contains(lower, "test") || strings.Contains(lower, "failing") || strings.Contains(lower, "error") {
			lines = append(lines, "- Validation note: prefer targeted verification and rerun the relevant checks after changes.")
		}
		if shouldAskForMoreContextForVagueRepoTask(trimmedQuery) {
			lines = append(lines, "- Clarification directive: the repository or working directory is still missing.")
			lines = append(lines, "- Stay in the current Ask/ReAct continuation. Ask one short clarification question using the clarification action instead of returning a standalone answer.")
			lines = append(lines, "- Do not call repo inspection, test execution, or edit tools until the user identifies the repo/workdir.")
		}

		lines = append(lines, "- Guardrail: avoid broad or speculative changes and stay grounded in the available evidence.")
		return strings.Join(lines, "\n")
	}

	phase := strings.TrimSpace(taskClassification.CapabilityHint)
	if phase == "" {
		lower := strings.ToLower(trimmedQuery)
		switch {
		case strings.Contains(lower, "fix") || strings.Contains(lower, "debug") || strings.Contains(lower, "error") || strings.Contains(lower, "failing") || strings.Contains(lower, "broken") || strings.Contains(lower, "problem"):
			phase = "investigation"
		case strings.Contains(lower, "plan") || strings.Contains(lower, "design") || strings.Contains(lower, "architecture") || strings.Contains(lower, "approach"):
			phase = "planning"
		case strings.Contains(lower, "review") || strings.Contains(lower, "validate") || strings.Contains(lower, "check") || strings.Contains(lower, "verify"):
			phase = "review"
		case strings.Contains(lower, "deliver") || strings.Contains(lower, "summary") || strings.Contains(lower, "handoff"):
			phase = "delivery"
		case strings.Contains(lower, "implement") || strings.Contains(lower, "add") || strings.Contains(lower, "update") || strings.Contains(lower, "patch") || strings.Contains(lower, "change"):
			phase = "execution"
		default:
			phase = "intake"
		}
	}

	domain := strings.TrimSpace(taskClassification.Domain)
	if domain == "" {
		domain = "General"
	}

	lines := []string{
		"Task Guidance Synthesis",
		fmt.Sprintf("- Selected Phase: %s", phase),
		fmt.Sprintf("- Selected Domain: %s", domain),
	}

	switch phase {
	case "investigation":
		lines = append(lines, "- Primary directive: inspect the situation, gather evidence, and form a clear hypothesis before acting.")
	case "planning":
		lines = append(lines, "- Primary directive: outline a concrete plan, identify dependencies, and state the main risks or assumptions.")
	case "execution":
		lines = append(lines, "- Primary directive: make the smallest plausible change that addresses the request and validate it immediately.")
	case "review":
		lines = append(lines, "- Primary directive: validate the work against the requirements, check for regressions, and call out unresolved issues clearly.")
	case "delivery":
		lines = append(lines, "- Primary directive: provide a concise handoff summary of the outcome, evidence, and follow-up actions.")
	default:
		lines = append(lines, "- Primary directive: clarify the scope if needed, then proceed with the most relevant next step.")
	}

	lower := strings.ToLower(trimmedQuery)
	if strings.Contains(lower, "test") || strings.Contains(lower, "failing") || strings.Contains(lower, "error") {
		lines = append(lines, "- Validation note: prefer targeted verification and rerun the relevant checks after changes.")
	}
	lines = append(lines, structuralAnalysisPolicyLines(strings.TrimSpace(taskClassification.Domain))...)
	if shouldAskForMoreContextForVagueRepoTask(trimmedQuery) {
		lines = append(lines, "- Clarification directive: the repository or working directory is still missing.")
		lines = append(lines, "- Stay in the current Ask/ReAct continuation. Ask one short clarification question using the clarification action instead of returning a standalone answer.")
		lines = append(lines, "- Do not call repo inspection, test execution, or edit tools until the user identifies the repo/workdir.")
	}

	lines = append(lines, "- Guardrail: avoid broad or speculative changes and stay grounded in the available evidence.")
	return strings.Join(lines, "\n")
}

func structuralAnalysisPolicyLines(domain string) []string {
	normalized := strings.ToLower(strings.TrimSpace(domain))
	if normalized == "coding" || normalized == "codingworkflow" {
		normalized = "coding_workflow"
	}
	line := "- Structural analysis policy: stay text-first when ownership is clear; escalate to AST/tree-sitter only for ambiguous symbols, wrappers/generated code, or multi-file precision, and skip it for local obvious fixes."
	if normalized != "coding_workflow" && normalized != "" && normalized != "general" {
		line = "- Structural analysis policy: stay text-first when ownership is clear; use AST/tree-sitter only when structural ambiguity or multi-file precision makes text-only repair unreliable."
	}
	return []string{line}
}

func isCodingWorkflowTaskClassification(taskClassification TaskContextClassification) bool {
	domain := strings.TrimSpace(strings.ToLower(taskClassification.Domain))
	switch domain {
	case "coding_workflow", "codingworkflow", "coding":
		return true
	default:
		return false
	}
}

func inferCodingPhase(taskClassification TaskContextClassification, query string) string {
	if !isCodingWorkflowTaskClassification(taskClassification) {
		return ""
	}
	hint := strings.TrimSpace(strings.ToLower(taskClassification.CapabilityHint))
	switch hint {
	case "execution", "review", "delivery":
		return "execution"
	case "investigation", "planning", "intake":
		return hint
	}
	if taskClassification.AskClassification != nil {
		phase := resolveActiveCodingRuntimePhase(taskClassification.AskClassification, taskClassification.AskClassification.Phase)
		if phase != "" {
			return phase
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, marker := range []string{"fix", "patch", "implement", "update", "modify", "change", "write", "failing test", "tests fail", "bug"} {
		if strings.Contains(q, marker) {
			return "execution"
		}
	}
	return "investigation"
}

func shouldUseLeanStoresAssembly(taskClassification TaskContextClassification) bool {
	if !strings.EqualFold(taskClassification.Domain, StoresDomain) {
		return false
	}
	if taskClassification.RoutingGate == RoutingGateContinuity {
		return false
	}
	return len(taskClassification.DBArtifacts) > 0 || len(taskClassification.StoresArtifacts) > 0
}

func (a *CopilotAgent) buildSpacesToolDescriptionContext() string {
	return strings.Join([]string{
		"Structured Context: Spaces Tools",
		"- Space mutations are business-critical because they change persisted knowledge. The tool that performs the write also defines the durability boundary for that change.",
		"- When a Space tool manages its own transaction path, treat that tool call as the commit boundary for the requested knowledge mutation.",
		"- mint_to_space: Persist generated or discovered content in a Space. Use the exact kb_name requested by the user; this tool manages its own write transaction.",
		"- delete_space: Remove an entire Space only when the user explicitly wants full deletion; it manages its own deletion path.",
		"- enrich_space: Run the enrichment pipeline only when the user explicitly wants derived knowledge refreshed.",
		"- update_space_config: Change Space routing, persona, or tool settings with a grounded config object.",
		"- read_space_config: Inspect current Space configuration before changing it or when the user asks how it behaves.",
		"- vectorize_space: Refresh embeddings for the whole Space only when the user explicitly asks for vectorization or semantic refresh.",
		"- vectorize_space_categories: Refresh embeddings for specific categories when the request is narrower than full-space vectorization.",
		"- vectorize_space_items: Refresh embeddings for specific items when the user wants the tightest possible vectorization scope.",
	}, "\n")
}

// buildStoresExecutionDirective returns a short execution directive for Stores asks.
// It is injected into ReasoningRequest.ContextText so provider-owned loops surface it
// in the model's input turn rather than only in the background instructions.
func buildStoresExecutionDirective(rs *TaskContextClassification) string {
	if rs == nil || !strings.EqualFold(rs.Domain, StoresDomain) {
		return ""
	}
	if len(collectCRUDFlags(rs.Layers)) == 0 {
		return ""
	}
	return "Execution task: call execute_script as a function_call with the full transaction plan. Do not display the script or plan as assistant text."
}

// buildSystemPrompt, on top of generated System Prompt(below), we rely on native Tool Calling via ListTools,
// which specifies each tool's basic info & supported JSON schema.
func (a *CopilotAgent) buildSystemPrompt(ctx context.Context, query string, taskClassification TaskContextClassification) string {
	a.rehydrateMRUFromMemory(ctx)

	builder := NewSystemPromptBuilder()
	leanStoresAssembly := shouldUseLeanStoresAssembly(taskClassification)
	persona, _ := a.resolvePersonaWithMetadata(ctx)
	semanticMemory := a.getLTMSemanticContext(ctx, query)
	selectedSpaceGuidance := a.selectedSpaceExpertContext(ctx, query, true)
	if leanStoresAssembly {
		persona = trimPromptComponentContent(ComponentPersona, persona, 900)
		semanticMemory = ""
	}

	// 1. Resolve Avatar / Custom KB Persona or Fallback
	builder.With(ComponentPersona, persona)

	// 2. LTM Semantic Resolution (Self-Correction / Working Memory)
	builder.With(ComponentSemanticMemory, semanticMemory)

	// 3. Always inject System Tools loaded into LTM
	systemTools := a.getSystemToolsContext(ctx)
	if focusedTools := compactFocusedToolContextAgainstBaseline(systemTools, a.buildFocusedToolContext(ctx, query, &taskClassification)); focusedTools != "" {
		if systemTools != "" {
			systemTools += "\n\n"
		}
		systemTools += focusedTools
	}
	builder.With(ComponentSystemTools, systemTools)

	// 4. Active Custom KBs / Playbooks Lookups
	// In the future, OMNI will be able to lookup multiple KBs, BUT OMNI will always assume persona from SOP.
	// Detect phase early for conditional component generation
	currentPhase := ""
	if classification := taskClassification.AskClassification; classification != nil {
		currentPhase = normalizePhase(classification.Phase)
	}
	isExecutionPhase := currentPhase == "execution"
	isCodingWorkflow := isCodingWorkflowTaskClassification(taskClassification)

	var domains []string
	if p := ai.GetSessionPayload(ctx); p != nil && p.ActiveDomain != "" {
		domains = strings.Split(p.ActiveDomain, ",")
	} else {
		domains = []string{"sop"}
	}
	playbooksContext := ""
	// Skip playbooks in execution phase for coding workflow (follow the plan, not generate new recipes)
	if !leanStoresAssembly && !(isExecutionPhase && isCodingWorkflow) {
		playbooksContext = a.getPlaybooksContext(ctx, query, domains)
	}
	builder.With(ComponentPlaybooks, playbooksContext)
	builder.With(ComponentRecipes, a.getRecipeContext(taskClassification))

	focusedExecutionContext := a.getFocusedExecutionContext(ctx, query, taskClassification)

	// Memory continuation - always relevant
	if memoryContinuation := a.getMemoryContinuationContext(taskClassification); memoryContinuation != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = memoryContinuation + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = memoryContinuation
		}
	}

	// Ask outcome - always relevant
	if askOutcome := a.getAskOutcomeContext(); askOutcome != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = askOutcome + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = askOutcome
		}
	}

	// Task guidance - Skip detailed guidance in execution phase (phase.BuildContext has it)
	if !isExecutionPhase {
		if guidance := buildTaskGuidanceSynthesis(query, taskClassification); guidance != "" {
			if focusedExecutionContext != "" {
				focusedExecutionContext = guidance + "\n\n" + focusedExecutionContext
			} else {
				focusedExecutionContext = guidance
			}
		}
	}

	// Planner context - NOT needed in execution phase
	if !isExecutionPhase {
		if plannerContext := a.getPlannerContext(); plannerContext != "" {
			if focusedExecutionContext != "" {
				focusedExecutionContext = plannerContext + "\n\n" + focusedExecutionContext
			} else {
				focusedExecutionContext = plannerContext
			}
		}
	}

	// Caveat context - always relevant
	if caveatContext := a.getCaveatContext(); caveatContext != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = caveatContext + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = caveatContext
		}
	}

	// General context - only for non-coding tasks
	if isGeneralTaskContext(&taskClassification) {
		generalContext := "General Ask Context:\n- This is an open-ended request that may cover any topic: coding, research, analysis, creative work, or general information.\n- Use the conversation history, memory continuity, and available tools to provide a helpful and accurate response.\n- Ask a short clarification question only if the request is genuinely ambiguous."
		if focusedExecutionContext != "" {
			focusedExecutionContext = generalContext + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = generalContext
		}
	}

	// Selected space guidance - only relevant for research workflows
	if selectedSpaceGuidance != "" && !isExecutionPhase {
		if focusedExecutionContext != "" {
			focusedExecutionContext = selectedSpaceGuidance + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = selectedSpaceGuidance
		}
	}

	// Research guidance - NOT needed in execution phase
	if taskClassification.ResearchRecommended && !isExecutionPhase {
		researchGuidance := a.buildResearchWorkflowGuidance(ctx, query, taskClassification)
		if researchGuidance != "" {
			if focusedExecutionContext != "" {
				focusedExecutionContext = researchGuidance + "\n\n" + focusedExecutionContext
			} else {
				focusedExecutionContext = researchGuidance
			}
		}
	}

	// Memory recall - Includes MRU and recent exchanges, useful in all phases
	if memoryRecall := a.buildVirtualMemoryRecallContext(ctx, query); memoryRecall != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = memoryRecall + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = memoryRecall
		}
	}
	if leanStoresAssembly && strings.EqualFold(taskClassification.Domain, StoresDomain) {
		focusedExecutionContext = trimPromptComponentContent(ComponentFocusedContext, focusedExecutionContext, 1000)
	}

	// Log context generation efficiency
	slog.Info("Phase-aware context generation",
		"phase", currentPhase,
		"is_execution", isExecutionPhase,
		"focused_context_chars", len(focusedExecutionContext),
		"playbooks_included", playbooksContext != "",
		"skipped_components", func() []string {
			skipped := []string{}
			if isExecutionPhase && isCodingWorkflow {
				skipped = append(skipped, "playbooks", "task_guidance", "planner_context", "research_guidance", "memory_recall", "selected_space_guidance")
			}
			return skipped
		}(),
	)

	// Record metrics for phase-aware context optimization (Aug 31, 2026)
	if currentPhase != "" {
		metrics.GlobalMetrics.RecordContextSize(currentPhase, len(focusedExecutionContext))
	}

	builder.With(ComponentFocusedContext, focusedExecutionContext)

	// 5. Generic schema fallback only when no specific store targets were classified.
	if !taskClassification.GeneralContext && taskClassification.Domain == StoresDomain && len(taskClassification.DBArtifacts) == 0 {
		builder.With(ComponentSchema, a.getSchemaInjectionContext(ctx))
	}

	// 6. Reconstruct the active transcript dynamically from episodic Short-Term Memory
	builder.With(ComponentHistory, a.getSessionMemoryContext())

	// 7. Inject Final Trigger query
	builder.With(ComponentUserQuery, "User: "+query)

	// Render as highly structured JSON elements to prevent Prompt confusion
	fullPrompt, budgetReport := builder.ToJSONWithBudgetReport(a.promptBudgetProfile(taskClassification))
	slog.Info("LLM Context Budget",
		"routing_gate", taskClassification.RoutingGate,
		"original_chars", budgetReport.OriginalTotalChars,
		"final_chars", budgetReport.FinalTotalChars,
		"components_present", summarizePromptComponentsPresent(budgetReport),
		"trimmed_components", summarizePromptBudgetTrim(budgetReport),
	)

	// Record budget trimming events (Aug 31, 2026)
	if budgetReport.OriginalTotalChars > budgetReport.FinalTotalChars && currentPhase != "" {
		metrics.GlobalMetrics.RecordContextTrim(currentPhase)
	}
	slog.Debug("LLM Context (OMNI)", "SystemPrompt", fullPrompt)

	return fullPrompt
}

func (a *CopilotAgent) promptBudgetProfile(taskClassification TaskContextClassification) PromptBudgetProfile {
	profile := PromptBudgetProfile{
		TotalChars: 14000,
		ComponentCharBudgets: map[PromptComponent]int{
			ComponentPersona:        2800,
			ComponentSemanticMemory: 1400,
			ComponentRecipes:        3200,
			ComponentPlaybooks:      1600,
			ComponentFocusedContext: 2600,
			ComponentSchema:         1800,
			ComponentHistory:        1800,
			ComponentUserQuery:      1200,
		},
		TrimPriorityLowToHigh: []PromptComponent{
			ComponentHistory,
			ComponentSchema,
			ComponentPlaybooks,
			ComponentSemanticMemory,
			ComponentSystemTools,
			ComponentRecipes,
			ComponentPersona,
			ComponentFocusedContext,
			ComponentUserQuery,
		},
	}

	// Phase-aware budget adjustment for execution phase
	if isCodingWorkflowTaskClassification(taskClassification) {
		if classification := taskClassification.AskClassification; classification != nil {
			phase := normalizePhase(classification.Phase)
			if phase == "execution" {
				// Execution phase needs more room for phase instructions
				profile.ComponentCharBudgets[ComponentFocusedContext] = 6000
			}
		}
	}

	switch taskClassification.RoutingGate {
	case RoutingGateFocused:
		profile.TotalChars = 12500
		profile.ComponentCharBudgets[ComponentSemanticMemory] = 1000
		profile.ComponentCharBudgets[ComponentSystemTools] = 3200
		profile.ComponentCharBudgets[ComponentPlaybooks] = 1200
		profile.ComponentCharBudgets[ComponentFocusedContext] = 3400
		profile.ComponentCharBudgets[ComponentHistory] = 1200
	case RoutingGateContinuity:
		profile.TotalChars = 15000
		profile.ComponentCharBudgets[ComponentSemanticMemory] = 1800
		profile.ComponentCharBudgets[ComponentSystemTools] = 2400
		profile.ComponentCharBudgets[ComponentPlaybooks] = 1800
		profile.ComponentCharBudgets[ComponentHistory] = 2600
	}

	if isCrossDomain(taskClassification.Layers) {
		profile.TotalChars = 16500
		profile.ComponentCharBudgets[ComponentSystemTools] = 3600
		profile.ComponentCharBudgets[ComponentFocusedContext] = 3200
		profile.ComponentCharBudgets[ComponentPlaybooks] = 1800
	}

	if taskClassification.Domain == StoresDomain && len(taskClassification.DBArtifacts) == 0 {
		profile.ComponentCharBudgets[ComponentSchema] = 2200
	}

	if len(taskClassification.DBArtifacts) > 0 || len(taskClassification.StoresArtifacts) > 0 || len(taskClassification.SpacesArtifacts) > 0 {
		profile.ComponentCharBudgets[ComponentSchema] = 0
		profile.ComponentCharBudgets[ComponentHistory] = 1400
	}

	if taskClassification.RoutingGate == RoutingGateContinuity {
		profile.ComponentCharBudgets[ComponentHistory] = 2600
	}

	if shouldUseLeanStoresAssembly(taskClassification) {
		profile.TotalChars = 11200
		profile.ComponentCharBudgets[ComponentPersona] = 900
		profile.ComponentCharBudgets[ComponentSemanticMemory] = 0
		profile.ComponentCharBudgets[ComponentSystemTools] = 1800
		profile.ComponentCharBudgets[ComponentRecipes] = 2200
		profile.ComponentCharBudgets[ComponentPlaybooks] = 0
		profile.ComponentCharBudgets[ComponentFocusedContext] = 3200
		profile.ComponentCharBudgets[ComponentHistory] = 1000
		profile.ComponentCharBudgets[ComponentUserQuery] = 800
	}

	return profile
}
