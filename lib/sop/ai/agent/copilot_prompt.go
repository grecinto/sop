package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sharedcode/sop/ai"
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
	if focusedTools := compactFocusedToolContextAgainstBaseline(systemTools, a.buildFocusedToolContext(&taskClassification)); focusedTools != "" {
		if systemTools != "" {
			systemTools += "\n\n"
		}
		systemTools += focusedTools
	}
	builder.With(ComponentSystemTools, systemTools)

	// 4. Active Custom KBs / Playbooks Lookups
	// In the future, OMNI will be able to lookup multiple KBs, BUT OMNI will always assume persona from SOP.
	var domains []string
	if p := ai.GetSessionPayload(ctx); p != nil && p.ActiveDomain != "" {
		domains = strings.Split(p.ActiveDomain, ",")
	} else {
		domains = []string{"sop"}
	}
	playbooksContext := ""
	if !leanStoresAssembly {
		playbooksContext = a.getPlaybooksContext(ctx, query, domains)
	}
	builder.With(ComponentPlaybooks, playbooksContext)
	builder.With(ComponentRecipes, a.getRecipeContext(taskClassification))
	focusedExecutionContext := a.getFocusedExecutionContext(ctx, query, taskClassification)
	if memoryContinuation := a.getMemoryContinuationContext(taskClassification); memoryContinuation != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = memoryContinuation + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = memoryContinuation
		}
	}
	if askOutcome := a.getAskOutcomeContext(); askOutcome != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = askOutcome + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = askOutcome
		}
	}
	if codingWorkflowContext := a.getCodingWorkflowContext(); codingWorkflowContext != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = codingWorkflowContext + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = codingWorkflowContext
		}
	}
	if caveatContext := a.getCaveatContext(); caveatContext != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = caveatContext + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = caveatContext
		}
	}
	if isGeneralTaskContext(&taskClassification) {
		generalContext := "General Ask Context:\n- This is an open-ended request that may cover any topic: coding, research, analysis, creative work, or general information.\n- Use the conversation history, memory continuity, and available tools to provide a helpful and accurate response.\n- Ask a short clarification question only if the request is genuinely ambiguous."
		if focusedExecutionContext != "" {
			focusedExecutionContext = generalContext + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = generalContext
		}
	}
	if selectedSpaceGuidance != "" {
		if focusedExecutionContext != "" {
			focusedExecutionContext = selectedSpaceGuidance + "\n\n" + focusedExecutionContext
		} else {
			focusedExecutionContext = selectedSpaceGuidance
		}
	}

	// Inject research workflow guidance for complex/advanced tasks
	if taskClassification.ResearchRecommended {
		researchGuidance := a.buildResearchWorkflowGuidance(ctx, query, taskClassification)
		if researchGuidance != "" {
			if focusedExecutionContext != "" {
				focusedExecutionContext = researchGuidance + "\n\n" + focusedExecutionContext
			} else {
				focusedExecutionContext = researchGuidance
			}
		}
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
	slog.Debug("LLM Context (OMNI)", "SystemPrompt", fullPrompt)

	return fullPrompt
}

func shouldUseLeanStoresAssembly(taskClassification TaskContextClassification) bool {
	if !strings.EqualFold(taskClassification.Domain, StoresDomain) {
		return false
	}
	if taskClassification.RoutingGate == RoutingGateContinuity {
		return false
	}
	if isCrossDomain(taskClassification.Layers) || taskClassification.ScriptAuthoring {
		return false
	}
	return len(taskClassification.DBArtifacts) > 0 || len(taskClassification.StoresArtifacts) > 0
}

func (a *CopilotAgent) promptBudgetProfile(taskClassification TaskContextClassification) PromptBudgetProfile {
	profile := PromptBudgetProfile{
		TotalChars: 14000,
		ComponentCharBudgets: map[PromptComponent]int{
			ComponentPersona:        2800,
			ComponentSemanticMemory: 1400,
			ComponentSystemTools:    2600,
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
