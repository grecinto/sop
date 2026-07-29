package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

func (a *CopilotAgent) selectedSpaceExpertContextForRouting(ctx context.Context, query string, taskClassification TaskContextClassification) string {
	// Auto-resolve research sources from task context when appropriate
	p := ai.GetSessionPayload(ctx)
	if p != nil && len(p.SelectedKBs) == 0 {
		resolvedSources := a.ResolveResearchSources(ctx, query, taskClassification)
		if len(resolvedSources) > 0 {
			// Temporarily augment the payload with resolved sources
			p.SelectedKBs = resolvedSources
		}
	}
	return a.selectedSpaceExpertContext(ctx, query, a.isContinuationPromptContext(taskClassification))
}

func selectedSpaceHasEvidence(configText, briefText, digestText string) bool {
	for _, part := range []string{configText, briefText, digestText} {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(part), "purpose:") || strings.Contains(strings.ToLower(part), "systemprompt:") || strings.Contains(strings.ToLower(part), "description:") || strings.Contains(strings.ToLower(part), "digest highlights:") || strings.Contains(strings.ToLower(part), "structured expert brief:") || strings.Contains(strings.ToLower(part), "role:") || strings.Contains(strings.ToLower(part), "key concepts:") || strings.Contains(strings.ToLower(part), "research workflow:") || strings.Contains(strings.ToLower(part), "common constraints:") || strings.Contains(strings.ToLower(part), "score:") {
			return true
		}
	}
	return false
}

func (a *CopilotAgent) selectedSpaceExpertContext(ctx context.Context, query string, compact bool) string {
	p := ai.GetSessionPayload(ctx)
	if p == nil {
		return ""
	}

	// Auto-resolve research sources if no explicit selection exists
	if len(p.SelectedKBs) == 0 {
		// Attempt to resolve from task classification context
		var taskCtx TaskContextClassification
		if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
			if routing := a.service.session.Memory.GetRoutingState(); routing != nil {
				taskCtx = *routing
			}
		}
		resolvedSources := a.ResolveResearchSources(ctx, query, taskCtx)
		if len(resolvedSources) > 0 {
			// Temporarily augment the payload with resolved sources
			p.SelectedKBs = resolvedSources
		} else {
			return ""
		}
	}

	selectedKey := selectedSpaceContextKey(p.SelectedKBs)
	if selectedKey != "" && a != nil && a.service != nil && a.service.session != nil {
		session := a.service.session
		shouldReinforce := a.shouldReinjectSelectedSpaceExpertContext(session, selectedKey)
		if shouldReinforce {
			compact = false
			session.LastSelectedSpaceContextKey = selectedKey
			session.LastSelectedSpaceReinforcementAt = time.Now().UnixMilli()
		} else {
			if session.LastSelectedSpaceContextKey != selectedKey {
				session.LastSelectedSpaceContextKey = selectedKey
			}
			compact = true
		}
	}

	var selected []string
	var sections []string
	for _, kbRef := range p.SelectedKBs {
		name := strings.TrimSpace(kbRef.Name)
		if name == "" {
			continue
		}
		selected = append(selected, name)

		var details []string
		details = append(details, fmt.Sprintf("Space: %s", name))
		var configText string
		var briefText string
		var digestText string
		if !compact {
			configText = a.selectedSpaceConfigContext(ctx, kbRef)
			if configText != "" {
				details = append(details, configText)
			}
			briefText = a.selectedSpaceStructuredBriefContext(ctx, query, kbRef)
			if briefText != "" {
				details = append(details, briefText)
			}
		}
		digestText = a.selectedSpaceDigestContext(ctx, query, kbRef)
		if digestText != "" {
			details = append(details, digestText)
		}
		if selectedSpaceHasEvidence(configText, briefText, digestText) {
			sections = append(sections, strings.Join(details, "\n"))
		}
	}
	if len(selected) == 0 {
		return ""
	}

	if compact {
		if len(selected) == 1 {
			return fmt.Sprintf("Selected Space Expert Context:\nContinuing with the selected knowledge base/Space %q or LTM memory. Maintain that knowledge source's expert framing when it is relevant, otherwise draw from LTM memory, and clearly say when the requested information is not present in either source.", selected[0])
		}
		return fmt.Sprintf("Selected Space Expert Context:\nContinuing with the selected knowledge base/Spaces %s or LTM memory. Maintain their expert framing when it is relevant, otherwise draw from LTM memory, and clearly say when the requested information is not present in either source.", strings.Join(selected, ", "))
	}

	if len(sections) == 0 {
		if len(selected) == 1 {
			return fmt.Sprintf("Selected Space Expert Context:\nExpert grounding active: treat the selected knowledge base/Space %q or LTM memory as the available grounding source for this request. Prefer the selected knowledge base/Space when it is relevant and otherwise use LTM memory, and clearly say when the requested information is not present in either source.", selected[0])
		}
		return "Selected Space Expert Context:\nExpert grounding active: treat the selected knowledge base/Space or LTM memory as the available grounding source for this request. Prefer the selected knowledge base/Space when it is relevant and otherwise use LTM memory, and clearly say when the requested information is not present in either source."
	}

	var intro string
	if len(selected) == 1 {
		intro = fmt.Sprintf("Expert grounding active: treat the selected knowledge base/Space %q or LTM memory as the available grounding source for this request. Prefer the selected knowledge base/Space when it is relevant and otherwise use LTM memory, and clearly say when the requested information is not present in either source.", selected[0])
	} else {
		intro = "Expert grounding active: treat the selected knowledge base/Space or LTM memory as the available grounding source for this request. Prefer the selected knowledge base/Space when it is relevant and otherwise use LTM memory, and clearly say when the requested information is not present in either source."
	}

	return "Selected Space Expert Context:\n" + intro + "\n\n" + strings.Join(sections, "\n\n")
}

func (a *CopilotAgent) selectedSpaceConfigContext(ctx context.Context, kbRef ai.ArtifactReference) string {
	db := a.resolveSelectedSpaceDatabase(ctx, kbRef.DatabaseName)
	if db == nil {
		return ""
	}

	tx, err := db.BeginTransaction(ctx, sop.NoCheck)
	if err != nil {
		return ""
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, kbRef.Name, tx, nil, nil, false, true)
	if err != nil {
		return ""
	}
	cfg, err := kb.GetConfig(ctx)
	if err != nil || cfg == nil {
		return ""
	}

	var details []string
	if cfg.SystemPrompt != "" {
		details = append(details, fmt.Sprintf("SystemPrompt: %s", strings.TrimSpace(cfg.SystemPrompt)))
	}
	if cfg.Description != "" {
		details = append(details, fmt.Sprintf("Description: %s", strings.TrimSpace(cfg.Description)))
	}
	if cfg.IsPersona {
		details = append(details, "Role: persona")
	}
	if len(details) == 0 {
		return ""
	}
	return strings.Join(details, "\n")
}

func (a *CopilotAgent) selectedSpaceStructuredBriefContext(ctx context.Context, query string, kbRef ai.ArtifactReference) string {
	db := a.resolveSelectedSpaceDatabase(ctx, kbRef.DatabaseName)
	if db == nil {
		return ""
	}

	tx, err := db.BeginTransaction(ctx, sop.NoCheck)
	if err != nil {
		return ""
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, kbRef.Name, tx, nil, nil, false, true)
	if err != nil || kb == nil {
		return ""
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		cfg = nil
	}

	purpose := strings.TrimSpace(cfg.Description)
	if purpose == "" && cfg != nil {
		purpose = strings.TrimSpace(cfg.SystemPrompt)
	}
	if purpose == "" {
		purpose = fmt.Sprintf("Use the %q knowledge base/Space as the authoritative grounding source when it is relevant; otherwise rely on LTM memory for the current request.", kbRef.Name)
	}

	var categories []string
	if cats, _, catErr := kb.ListCategories(ctx, memory.ListCategoriesParam{Limit: 6}); catErr == nil {
		for _, cat := range cats {
			name := strings.TrimSpace(cat.Name)
			if name != "" {
				categories = append(categories, name)
			}
		}
	}
	if len(categories) == 0 {
		categories = append(categories, "general")
	}

	keyConcepts := []string{}
	for _, hit := range a.selectedSpaceBriefDigestHits(ctx, query, kbRef) {
		text := strings.TrimSpace(hit)
		if text == "" {
			continue
		}
		keyConcepts = append(keyConcepts, text)
		if len(keyConcepts) >= 3 {
			break
		}
	}
	if len(keyConcepts) == 0 {
		keyConcepts = append(keyConcepts, "Ground answers in the Space's documented guidance and surface uncertainty when evidence is incomplete.")
	}

	patterns := []string{
		"Answer from the Space first and cite the most relevant guidance.",
		"Prefer precise, grounded wording over generic speculation.",
		"Call out missing evidence or ambiguity rather than inventing details.",
	}
	examples := []string{
		fmt.Sprintf("If the request concerns %s, anchor the response in the Space's curated knowledge and note the most relevant evidence.", kbRef.Name),
	}
	constraints := []string{
		"Do not invent facts that are not present in the Space.",
		"Mention when requested information is not available.",
		"Use conservative language when the evidence is limited.",
	}
	researchWorkflow := []string{
		"Investigate the selected knowledge base/Space or LTM memory methodically by treating categories as semantic anchors: start with the top-level categories, then drill into nested categories and subcategories as needed.",
		"Use search_space first when a selected Space is available, based on the formulated semantic categories at each level, to find the most relevant content before broad browsing.",
		"Use list_space_categories as a fallback when semantic search does not yield enough structure, and use list_space_items to inspect items within a category once a promising branch is identified.",
		"If the selected Space is not the best fit, fall back to LTM memory and gather evidence from the most relevant remembered guidance before answering.",
		"Formulate and compare candidate categories for the prompt, then hunt for the most relevant category branches before answering.",
		"Treat the available knowledge source as a hierarchy of knowledge: categories -> subcategories -> items/thoughts; gather evidence from the most specific relevant branch before answering.",
		"If the query is broad, first identify the likely semantic branches, then gather and synthesize evidence from those branches.",
	}

	return strings.Join([]string{
		"Structured Expert Brief:",
		fmt.Sprintf("Purpose: %s", purpose),
		fmt.Sprintf("Categories: %s", strings.Join(categories, ", ")),
		fmt.Sprintf("Key Concepts: %s", strings.Join(keyConcepts, "; ")),
		fmt.Sprintf("Research Workflow: %s", strings.Join(researchWorkflow, " ")),
		fmt.Sprintf("Patterns: %s", strings.Join(patterns, " ")),
		fmt.Sprintf("Examples: %s", strings.Join(examples, " ")),
		fmt.Sprintf("Common Constraints: %s", strings.Join(constraints, " ")),
	}, "\n")
}

func (a *CopilotAgent) selectedSpaceBriefDigestHits(ctx context.Context, query string, kbRef ai.ArtifactReference) []string {
	if a.service == nil || a.service.Domain() == nil || a.service.Domain().Embedder() == nil {
		return nil
	}

	db := a.resolveSelectedSpaceDatabase(ctx, kbRef.DatabaseName)
	if db == nil {
		return nil
	}

	tx, err := db.BeginTransaction(ctx, sop.NoCheck)
	if err != nil {
		return nil
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, kbRef.Name, tx, nil, nil, false, true)
	if err != nil || kb == nil {
		return nil
	}

	queries := buildSelectedSpaceResearchQueries(query, kbRef.Name, nil, nil)
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if state := a.service.session.Memory.GetResearchWorkflowState(); state != nil {
			queries = buildSelectedSpaceResearchQueries(query, kbRef.Name, state, nil)
		}
	}
	if len(queries) == 0 {
		return nil
	}

	hits, err := memory.DigestKnowledgeBase(ctx, kb, a.service.Domain().Embedder(), memory.KBDigestRequest{
		Queries:            queries,
		PerQueryLimit:      5,
		MaxResults:         3,
		MinScore:           0.5,
		UseClosestCategory: true,
	})
	if err != nil || len(hits) == 0 {
		return nil
	}

	lines := make([]string, 0, len(hits))
	for _, hit := range hits {
		text := strings.TrimSpace(hit.Text)
		if text == "" {
			continue
		}
		text = strings.ReplaceAll(text, "\n", " ")
		if len(text) > 180 {
			text = text[:177] + "..."
		}
		lines = append(lines, text)
	}
	return lines
}

func (a *CopilotAgent) selectedSpaceDigestContext(ctx context.Context, query string, kbRef ai.ArtifactReference) string {
	if a.service == nil || a.service.Domain() == nil || a.service.Domain().Embedder() == nil {
		return ""
	}

	db := a.resolveSelectedSpaceDatabase(ctx, kbRef.DatabaseName)
	if db == nil {
		return ""
	}

	tx, err := db.BeginTransaction(ctx, sop.NoCheck)
	if err != nil {
		return ""
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, kbRef.Name, tx, nil, nil, false, true)
	if err != nil {
		return ""
	}

	queries := buildSelectedSpaceResearchQueries(query, kbRef.Name, nil, nil)
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if state := a.service.session.Memory.GetResearchWorkflowState(); state != nil {
			queries = buildSelectedSpaceResearchQueries(query, kbRef.Name, state, nil)
		}
	}
	if len(queries) == 0 {
		return ""
	}

	hits, err := memory.DigestKnowledgeBase(ctx, kb, a.service.Domain().Embedder(), memory.KBDigestRequest{
		Queries:            queries,
		PerQueryLimit:      5,
		MaxResults:         5,
		MinScore:           0.5,
		UseClosestCategory: true,
	})
	if err != nil || len(hits) == 0 {
		return ""
	}

	var lines []string
	for _, hit := range hits {
		lines = append(lines, fmt.Sprintf("- (Score: %.2f) %s", hit.Score, hit.Text))
	}
	return "Digest highlights:\n" + strings.Join(lines, "\n")
}

func (a *CopilotAgent) selectedSpaceExpertiseSection(ctx context.Context, kbName string, query string) string {
	if a == nil || strings.TrimSpace(kbName) == "" {
		return ""
	}
	kbRef := ai.ArtifactReference{Name: kbName}
	parts := []string{}
	var systemPrompt, purpose string
	var researchHints string
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		if state := a.service.session.Memory.GetResearchWorkflowState(); state != nil {
			researchHints = buildSelectedSpaceResearchMemoryHints(query, state)
		}
	}
	if configText := a.selectedSpaceConfigContext(ctx, kbRef); configText != "" {
		parts = append(parts, configText)
		for _, line := range strings.Split(configText, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "SystemPrompt:"):
				systemPrompt = strings.TrimSpace(strings.TrimPrefix(line, "SystemPrompt:"))
			case strings.HasPrefix(line, "Description:"):
				purpose = strings.TrimSpace(strings.TrimPrefix(line, "Description:"))
			}
		}
	}
	if briefText := a.selectedSpaceStructuredBriefContext(ctx, query, kbRef); briefText != "" {
		parts = append(parts, briefText)
		for _, line := range strings.Split(briefText, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Purpose:") {
				purpose = strings.TrimSpace(strings.TrimPrefix(line, "Purpose:"))
			}
		}
	}
	if digestText := a.selectedSpaceDigestContext(ctx, query, kbRef); digestText != "" {
		parts = append(parts, digestText)
	}
	if len(parts) == 0 {
		return ""
	}
	if guidance := buildSelectedSpaceGenerationGuidance(systemPrompt, purpose, query); guidance != "" {
		parts = append([]string{guidance}, parts...)
	}
	if researchHints != "" {
		parts = append(parts, researchHints)
	}
	return fmt.Sprintf("selected space %q guidance: %s", kbName, strings.Join(parts, " | "))
}

func (a *CopilotAgent) isContinuationPromptContext(taskClassification ...TaskContextClassification) bool {
	if len(taskClassification) > 0 && taskClassification[0].RoutingGate == RoutingGateContinuity {
		return true
	}
	if a == nil || a.service == nil || a.service.session == nil {
		return false
	}
	if a.service.session.Memory == nil {
		return false
	}
	thread := a.service.session.Memory.GetCurrentThread()
	return thread != nil && len(thread.Exchanges) > 0
}

func selectedSpaceContextKey(refs []ai.ArtifactReference) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			continue
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, "|")
}

func (a *CopilotAgent) shouldReinjectSelectedSpaceExpertContext(session *RunnerSession, selectedKey string) bool {
	if a == nil || a.service == nil || session == nil || selectedKey == "" {
		return false
	}
	if session.LastSelectedSpaceContextKey != selectedKey {
		return true
	}
	if session.LastSelectedSpaceReinforcementAt == 0 {
		return true
	}
	if time.Since(time.UnixMilli(session.LastSelectedSpaceReinforcementAt)) <= selectedSpaceReinforcementWindow {
		return false
	}
	return true
}

func (a *CopilotAgent) resolveSelectedSpaceDatabase(ctx context.Context, dbName string) *database.Database {
	if strings.TrimSpace(dbName) == "" {
		if p := ai.GetSessionPayload(ctx); p != nil && strings.TrimSpace(p.CurrentDB) != "" {
			dbName = p.CurrentDB
		}
	}
	if strings.TrimSpace(dbName) == "" || strings.EqualFold(dbName, SystemDBName) {
		if a.systemDB != nil {
			return a.systemDB
		}
		return nil
	}
	if opts, ok := a.databases[dbName]; ok {
		return database.NewDatabase(opts)
	}
	return nil
}

func buildSelectedSpaceResearchContextParts(query string, state *ResearchWorkflowState) []string {
	query = strings.TrimSpace(query)
	if state == nil {
		return nil
	}
	parts := make([]string, 0, 4)
	if strings.TrimSpace(state.LastPrompt) != "" {
		parts = append(parts, fmt.Sprintf("recent prompt: %s", strings.TrimSpace(state.LastPrompt)))
	}
	if len(state.SuccessfulCategoryPaths) > 0 {
		parts = append(parts, fmt.Sprintf("prior successful category paths: %s", strings.Join(filterResearchMemoryValues(state.SuccessfulCategoryPaths), ", ")))
	} else if len(state.SuccessfulCategories) > 0 {
		parts = append(parts, fmt.Sprintf("prior successful categories: %s", strings.Join(filterResearchMemoryValues(state.SuccessfulCategories), ", ")))
	}
	if len(state.SuccessfulItemPaths) > 0 {
		parts = append(parts, fmt.Sprintf("prior successful item paths: %s", strings.Join(filterResearchMemoryValues(state.SuccessfulItemPaths), ", ")))
	} else if len(state.SuccessfulItems) > 0 {
		parts = append(parts, fmt.Sprintf("prior successful items: %s", strings.Join(filterResearchMemoryValues(state.SuccessfulItems), ", ")))
	}
	if strings.TrimSpace(state.LastResearchSummary) != "" {
		parts = append(parts, fmt.Sprintf("recent research summary: %s", strings.TrimSpace(state.LastResearchSummary)))
	}
	if len(parts) == 0 {
		return nil
	}
	if query != "" {
		parts = append(parts, fmt.Sprintf("for the current ask %q, prioritize branches and items that match these prior successes", query))
	}
	return parts
}
