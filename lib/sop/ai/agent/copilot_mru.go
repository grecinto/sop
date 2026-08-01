package agent

import (
	"context"
	"fmt"
	log "log/slog"
	"sort"
	"strings"
	"time"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// MarkMRUCategory adds or updates a category in the global working memory MRU.
func (a *CopilotAgent) MarkMRUCategory(category string, context string) {
	a.markMRUCategoryWithSource(category, context, MRUSourceUnknown)
}

func (a *CopilotAgent) markMRUCategoryWithSource(category string, context string, source string) {
	a.markMRUCategory(category, context, source, MRUScopeSession)
}

func (a *CopilotAgent) markMRUCategory(category string, context string, source string, scope string) {
	if a.service == nil || a.service.session == nil {
		return
	}
	sess := a.service.session
	sess.MRUMu.Lock()
	defer sess.MRUMu.Unlock()

	ts := time.Now().UnixMilli()
	scope = normalizeMRUScope(scope)

	for i, item := range sess.MRU {
		if item.Category == category {
			sess.MRU[i].LastAccessed = ts
			if context != "" {
				sess.MRU[i].Context = context
			}
			if source != "" {
				sess.MRU[i].Source = source
			}
			sess.MRU[i].Scope = scope
			return
		}
	}

	sess.MRU = append(sess.MRU, MRUItem{
		Category:     category,
		LastAccessed: ts,
		Context:      context,
		Source:       source,
		Scope:        scope,
	})
	if len(sess.MRU) > MaxMRUSize {
		sort.Slice(sess.MRU, func(i, j int) bool {
			return sess.MRU[i].LastAccessed > sess.MRU[j].LastAccessed
		})
		sess.MRU = sess.MRU[:MaxMRUSize]
	}
}

func normalizeMRUScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case "", MRUScopeSession:
		return MRUScopeSession
	case MRUScopeAsk:
		return MRUScopeAsk
	default:
		return MRUScopeSession
	}
}

func (a *CopilotAgent) clearMRUCategory(category string) {
	if a.service == nil || a.service.session == nil {
		return
	}
	sess := a.service.session
	sess.MRUMu.Lock()
	defer sess.MRUMu.Unlock()

	filtered := sess.MRU[:0]
	for _, item := range sess.MRU {
		if item.Category != category {
			filtered = append(filtered, item)
		}
	}
	sess.MRU = filtered
}

func (a *CopilotAgent) clearMRUBySourceAndScope(source string, scope string) {
	if a.service == nil || a.service.session == nil {
		return
	}
	sess := a.service.session
	sess.MRUMu.Lock()
	defer sess.MRUMu.Unlock()

	scope = normalizeMRUScope(scope)
	filtered := sess.MRU[:0]
	for _, item := range sess.MRU {
		if item.Source == source && normalizeMRUScope(item.Scope) == scope {
			continue
		}
		filtered = append(filtered, item)
	}
	sess.MRU = filtered
}

func (a *CopilotAgent) clearMRUForTopicSwitch() {
	if a.service == nil || a.service.session == nil {
		return
	}
	sess := a.service.session
	sess.MRUMu.Lock()
	defer sess.MRUMu.Unlock()

	filtered := sess.MRU[:0]
	for _, item := range sess.MRU {
		if shouldPreserveMRUOnTopicSwitch(item) {
			filtered = append(filtered, item)
		}
	}
	sess.MRU = filtered
	if sess.Memory != nil {
		sess.Memory.ResetProjectionForTopicSwitch()
	}
}

func playbookMRUCategory(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	return "PLAYBOOK_" + domain
}

// GetMRUCategory retrieves a category from the global working memory MRU.
func (a *CopilotAgent) GetMRUCategory(category string) (string, bool) {
	return a.getMRUCategoryBySource(category, MRUSourceUnknown, false)
}

func (a *CopilotAgent) getMRUCategoryBySource(category string, source string, allowLegacyUnknown bool) (string, bool) {
	if item, ok := a.findMRUItem(category, source, allowLegacyUnknown); ok {
		return item.Context, true
	}
	return "", false
}

func (a *CopilotAgent) findMRUItem(category string, source string, allowLegacyUnknown bool) (MRUItem, bool) {
	if a.service == nil || a.service.session == nil {
		return MRUItem{}, false
	}
	sess := a.service.session
	sess.MRUMu.RLock()
	defer sess.MRUMu.RUnlock()

	var legacyFallback MRUItem
	var hasLegacyFallback bool
	for _, item := range sess.MRU {
		if item.Category == category {
			if source == MRUSourceUnknown || item.Source == source {
				return item, true
			}
			if allowLegacyUnknown && item.Source == MRUSourceUnknown && !hasLegacyFallback {
				legacyFallback = item
				hasLegacyFallback = true
			}
		}
	}
	if hasLegacyFallback {
		return legacyFallback, true
	}
	return MRUItem{}, false
}

func (a *CopilotAgent) getMRUSnapshot() []MRUItem {
	if a.service == nil || a.service.session == nil {
		return nil
	}
	sess := a.service.session
	sess.MRUMu.RLock()
	defer sess.MRUMu.RUnlock()
	return append([]MRUItem(nil), sess.MRU...)
}

func (a *CopilotAgent) buildProviderLoopMRUSink() ai.MemoryHydrationSink {
	if a == nil || a.service == nil || a.service.session == nil {
		return nil
	}
	a.clearAskProgressMRU()
	return func(update ai.MemoryHydrationUpdate) {
		a.persistAskProgressMRU(update)
	}
}

func (a *CopilotAgent) clearAskProgressMRU() {
	a.clearMRUBySourceAndScope(MRUSourceAskProgress, MRUScopeAsk)
}

func (a *CopilotAgent) epilogueAndCleanup(ctx context.Context, query string, intent string, finalText string, toolCalls []ai.ToolCall, outcomeFacts []string, outcomeRecipes []ai.LearnedRecipe, carryoverState *ai.CarryoverState, clarificationRequest *ai.ClarificationRequest) {
	// Step OnFinalize hook
	if step, ok := ctx.Value(ctxKeyActiveStep).(cw.Step); ok && step != nil {
		if cap, ok := ctx.Value(ctxKeySelectedCapability).(capabilities.Capability); ok && cap != nil {
			routingState := capabilities.RoutingState{
				Domain:         "",
				Gate:           "",
				CapabilityHint: "",
			}
			if stepResult, stepErr := step.OnFinalize(ctx, routingState); stepErr != nil {
				log.Warn("Step OnFinalize failed", "error", stepErr)
			} else {
				log.Info("Step finalized", "outcome", stepResult.Outcome, "message", stepResult.Message)
				// Could store stepResult in context or return it somehow
				_ = stepResult
			}
		}
	}

	if a.service != nil && a.service.session != nil {
		if a.service.session.Memory == nil {
			a.service.session.Memory = NewShortTermMemory()
		}
		thread := a.service.session.Memory.GetCurrentThread()
		if thread == nil {
			newThreadID := sop.NewUUID()
			thread = &ConversationThread{
				ID:         newThreadID,
				RootPrompt: query,
				Label:      "Copilot Session",
				Category:   "General",
				Exchanges:  make([]Interaction, 0),
				Status:     "active",
			}
			a.service.session.Memory.AddThread(thread)
			a.service.session.Memory.CurrentThreadID = newThreadID
		}

		kbTrack := "sop"
		if p := ai.GetSessionPayload(ctx); p != nil && len(p.SelectedKBs) > 0 {
			var names []string
			for _, kb := range p.SelectedKBs {
				names = append(names, kb.Name)
			}
			kbTrack = strings.Join(names, ",")
		}

		thread.Exchanges = append(thread.Exchanges, Interaction{
			Role:      RoleUser,
			Content:   query,
			Timestamp: time.Now().UnixMilli(),
			Entity:    intent,
			ActiveKB:  kbTrack,
		})
		thread.Exchanges = append(thread.Exchanges, Interaction{
			Role:      RoleAssistant,
			Content:   finalText,
			Timestamp: time.Now().UnixMilli(),
			Entity:    intent,
			ActiveKB:  kbTrack,
		})
	}
	a.persistAskOutcomeMRU(ctx, query, finalText, toolCalls, outcomeFacts)
	a.clearAskProgressMRU()
	a.updateClarificationState(ctx, query, finalText, toolCalls, clarificationRequest)
	a.clearRetryRewriteState(ctx)

	mruSnapshot := a.getMRUSnapshot()
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		thread := a.service.session.Memory.GetCurrentThread()
		a.service.session.Memory.SetMRUSnapshot(mruSnapshot)
		existingRecipes := a.service.session.Memory.GetRecipeSnapshot()
		learnedRecipes := recipeItemsFromLearned(outcomeRecipes)
		a.service.session.Memory.SetRecipeSnapshot(mergeRecipeSnapshots(existingRecipes, learnedRecipes))
		persistCarryoverState(a.service.session.Memory, buildCarryoverState(ctx, a.service.session, a.brain, thread, query, finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState))
	}

	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		a.captureRuntimeCaveatsFromText(finalText)
	}

	if a.Memory != nil {
		thoughtPayload := map[string]any{
			"query":            query,
			"response":         finalText,
			"active_context":   mruSnapshot,
			"tool_calls":       toolCalls,
			"outcome_facts":    outcomeFacts,
			"interaction_kind": "user_interaction",
		}

		go a.Memory.LogEpisodeToSTM(context.Background(), "user_interaction", thoughtPayload, "Interacted with user", nil)
	}
}

func (a *CopilotAgent) clearRetryRewriteState(ctx context.Context) {
	if p := ai.GetSessionPayload(ctx); p != nil && p.RetryRewriteState != nil {
		p.RetryRewriteState = nil
	}
}

func (a *CopilotAgent) updateClarificationState(ctx context.Context, query string, finalText string, toolCalls []ai.ToolCall, clarificationRequest *ai.ClarificationRequest) {
	p := ai.GetSessionPayload(ctx)
	if p == nil {
		return
	}
	if clarificationRequest != nil {
		targetQuery := strings.TrimSpace(clarificationRequest.TargetQuery)
		if targetQuery == "" {
			targetQuery = strings.TrimSpace(askOutcomeQueryForPersistence(ctx, query))
		}
		if targetQuery == "" {
			targetQuery = strings.TrimSpace(query)
		}
		p.ClarificationState = &ai.ClarificationState{
			TargetQuery:       targetQuery,
			AssistantQuestion: strings.TrimSpace(clarificationRequest.AssistantQuestion),
			OriginalUserQuery: strings.TrimSpace(query),
			Status:            firstNonEmptyStatus(strings.TrimSpace(clarificationRequest.Status), "pending"),
			Source:            strings.TrimSpace(clarificationRequest.Source),
			MissingFact:       strings.TrimSpace(clarificationRequest.MissingFact),
			Reason:            strings.TrimSpace(clarificationRequest.Reason),
		}
		return
	}
	if strings.TrimSpace(finalText) == "" {
		return
	}
	if len(toolCalls) == 0 && isLikelyMetaQuestion(finalText) {
		targetQuery := strings.TrimSpace(askOutcomeQueryForPersistence(ctx, query))
		if targetQuery == "" {
			targetQuery = strings.TrimSpace(query)
		}
		p.ClarificationState = &ai.ClarificationState{
			TargetQuery:       targetQuery,
			AssistantQuestion: strings.TrimSpace(finalText),
			OriginalUserQuery: strings.TrimSpace(query),
			Status:            "pending",
		}
		return
	}
	if p.ClarificationState != nil {
		p.ClarificationState = nil
	}
}

func firstNonEmptyStatus(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func summarizeAskOutcomeMRU(ctx context.Context, query string, finalText string, toolCalls []ai.ToolCall, outcomeFacts []string) string {
	return renderAskOutcomeMRUItems(buildAskOutcomeMRUItems(ctx, query, finalText, toolCalls, outcomeFacts, nil))
}

func buildAskOutcomeMRUItems(ctx context.Context, query string, finalText string, toolCalls []ai.ToolCall, outcomeFacts []string, carryoverState *ai.CarryoverState) []MRUItem {
	query = askOutcomeQueryForPersistence(ctx, query)
	query = compactMRUText(query, 180)
	finalText = sanitizeAssistantContinuityText(finalText)
	finalText = compactMRUText(finalText, 260)
	if query == "" && finalText == "" {
		return nil
	}

	items := []MRUItem{{Category: askOutcomeMRUCategoryHeader, Context: "Recent Ask Outcome:", Source: MRUSourceAskOutcome, Scope: MRUScopeSession}}
	if p := ai.GetSessionPayload(ctx); p != nil {
		if currentDB := strings.TrimSpace(p.CurrentDB); currentDB != "" {
			items = append(items, MRUItem{Category: askOutcomeMRUCategoryDatabase, Context: fmt.Sprintf("- Database: %s", currentDB), Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
		}
		if activeDomain := strings.TrimSpace(p.ActiveDomain); activeDomain != "" {
			items = append(items, MRUItem{Category: askOutcomeMRUCategoryDomain, Context: fmt.Sprintf("- Domain: %s", activeDomain), Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
		}
	}
	if query != "" {
		items = append(items, MRUItem{Category: askOutcomeMRUCategoryQuery, Context: fmt.Sprintf("- Last user ask: %s", query), Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
	}
	if finalText != "" {
		items = append(items, MRUItem{Category: askOutcomeMRUCategoryResult, Context: fmt.Sprintf("- Last outcome: %s", finalText), Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
	}
	items = append(items, buildAskOutcomeFactMRUItems(outcomeFacts)...)
	if carryoverState != nil {
		payload := make([]string, 0, 2)
		if conversationID := strings.TrimSpace(carryoverState.ConversationID); conversationID != "" {
			payload = append(payload, fmt.Sprintf("conversation_id=%s", conversationID))
		}
		if conversationHandle := strings.TrimSpace(carryoverState.ConversationHandle); conversationHandle != "" {
			payload = append(payload, fmt.Sprintf("response_id=%s", conversationHandle))
		}
		if len(payload) > 0 {
			items = append(items, MRUItem{Category: askOutcomeMRUCategoryCarryover, Context: fmt.Sprintf("- Carryover payload: %s", strings.Join(payload, "; ")), Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
		}
	}
	if toolPattern := summarizeAskOutcomeToolPattern(toolCalls); toolPattern != "" {
		items = append(items, MRUItem{Category: askOutcomeMRUCategoryToolPattern, Context: fmt.Sprintf("- Tool pattern: %s", toolPattern), Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
	}
	items = append(items, MRUItem{Category: askOutcomeMRUCategoryGuidance, Context: "- Reuse confirmed facts and successful patterns from this outcome before broadening scope.", Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
	return items
}

func buildAskProgressMRUItems(finalText string, toolCalls []ai.ToolCall, outcomeFacts []string) []MRUItem {
	finalText = compactMRUText(finalText, 220)
	facts := compactOutcomeFacts(outcomeFacts)
	if finalText == "" && len(facts) == 0 && len(toolCalls) == 0 {
		return nil
	}
	items := []MRUItem{{Category: askProgressMRUCategoryHeader, Context: "In-Flight Ask Progress:", Source: MRUSourceAskProgress, Scope: MRUScopeAsk}}
	if finalText != "" {
		items = append(items, MRUItem{Category: askProgressMRUCategoryResult, Context: fmt.Sprintf("- Latest progress: %s", finalText), Source: MRUSourceAskProgress, Scope: MRUScopeAsk})
	}
	for index, fact := range facts {
		items = append(items, MRUItem{Category: fmt.Sprintf("%s_%02d", askProgressMRUCategoryConfirmed, index+1), Context: fmt.Sprintf("- Confirmed so far: %s", fact), Source: MRUSourceAskProgress, Scope: MRUScopeAsk})
	}
	if toolPattern := summarizeAskOutcomeToolPattern(toolCalls); toolPattern != "" {
		items = append(items, MRUItem{Category: askProgressMRUCategoryToolPattern, Context: fmt.Sprintf("- Tool pattern so far: %s", toolPattern), Source: MRUSourceAskProgress, Scope: MRUScopeAsk})
	}
	items = append(items, MRUItem{Category: askProgressMRUCategoryGuidance, Context: "- Treat these as provisional in-loop signals until the ask completes.", Source: MRUSourceAskProgress, Scope: MRUScopeAsk})
	return items
}

func (a *CopilotAgent) persistAskProgressMRU(update ai.MemoryHydrationUpdate) {
	a.clearMRUBySourceAndScope(MRUSourceAskProgress, MRUScopeAsk)
	for _, item := range buildAskProgressMRUItems(update.FinalText, update.ToolCalls, update.OutcomeFacts) {
		a.markMRUCategory(item.Category, item.Context, item.Source, item.Scope)
	}
}

func askOutcomeQueryForPersistence(ctx context.Context, fallback string) string {
	if p := ai.GetSessionPayload(ctx); p != nil {
		if p.ClarificationState != nil && strings.TrimSpace(p.ClarificationState.TargetQuery) != "" {
			return strings.TrimSpace(p.ClarificationState.TargetQuery)
		}
		if p.RetryRewriteState != nil && strings.TrimSpace(p.RetryRewriteState.ResolvedQuery) != "" {
			return strings.TrimSpace(p.RetryRewriteState.ResolvedQuery)
		}
	}
	return fallback
}

func buildAskOutcomeFactMRUItems(outcomeFacts []string) []MRUItem {
	facts := compactOutcomeFacts(outcomeFacts)
	if len(facts) == 0 {
		return nil
	}

	items := make([]MRUItem, 0, len(facts))
	genericIndex := 1
	for _, fact := range facts {
		category := ""
		if joinSelectionKey, ok := classifyExecuteScriptJoinFactKey(fact); ok {
			category = fmt.Sprintf("%s_%s", askOutcomeMRUCategoryJoinSelection, normalizeMRUFactKey(joinSelectionKey))
		} else if filterSelectionKey, ok := classifyExecuteScriptFilterFactKey(fact); ok {
			category = fmt.Sprintf("%s_%s", askOutcomeMRUCategoryFilterSelection, normalizeMRUFactKey(filterSelectionKey))
		} else if details, ok := classifyConfirmedStoreFact(fact); ok && details.FactType == confirmedStoreFactTypeSchema {
			category = fmt.Sprintf("%s_%s", askOutcomeMRUCategoryStoreSchema, normalizeMRUFactKey(details.StoreName))
		} else if details, ok := classifyConfirmedStoreFact(fact); ok && details.FactType == confirmedStoreFactTypeRelations {
			category = fmt.Sprintf("%s_%s", askOutcomeMRUCategoryRelations, normalizeMRUFactKey(details.CategoryKey()))
		} else {
			category = fmt.Sprintf("%s_%02d", askOutcomeMRUCategoryConfirmed, genericIndex)
			genericIndex++
		}
		items = append(items, MRUItem{Category: category, Context: fmt.Sprintf("- Confirmed: %s", fact), Source: MRUSourceAskOutcome, Scope: MRUScopeSession})
	}
	return items
}

const (
	confirmedStoreFactTypeSchema    = "schema"
	confirmedStoreFactTypeRelations = "relations"
)

type confirmedStoreFactDetails struct {
	StoreName      string
	FactType       string
	FactValue      string
	RelationTarget string
}

func (d confirmedStoreFactDetails) CategoryKey() string {
	if d.FactType != confirmedStoreFactTypeRelations || strings.TrimSpace(d.RelationTarget) == "" {
		return d.StoreName
	}
	return d.StoreName + "__" + d.RelationTarget + "__" + trimRelationFactValue(d.FactValue)
}

func classifyConfirmedStoreFact(fact string) (confirmedStoreFactDetails, bool) {
	fact = strings.TrimSpace(fact)
	if !strings.HasPrefix(fact, "list_stores confirmed ") {
		return confirmedStoreFactDetails{}, false
	}
	remainder := strings.TrimPrefix(fact, "list_stores confirmed ")
	if storeName, factValue, ok := classifyConfirmedStoreFactByMarker(remainder, "schema="); ok {
		return confirmedStoreFactDetails{StoreName: storeName, FactType: confirmedStoreFactTypeSchema, FactValue: factValue}, true
	}
	if storeName, factValue, ok := classifyConfirmedStoreFactByMarker(remainder, "relations="); ok {
		return confirmedStoreFactDetails{StoreName: storeName, FactType: confirmedStoreFactTypeRelations, FactValue: factValue, RelationTarget: extractPrimaryRelationTarget(factValue)}, true
	}
	return confirmedStoreFactDetails{}, false
}

func classifyExecuteScriptJoinFactKey(fact string) (string, bool) {
	fact = strings.TrimSpace(fact)
	if !strings.HasPrefix(fact, "execute_script confirmed ") {
		return "", false
	}
	remainder := strings.TrimPrefix(fact, "execute_script confirmed ")
	storeIdx := strings.Index(remainder, " store=")
	onIdx := strings.Index(remainder, " on=")
	if storeIdx <= 0 || onIdx <= storeIdx {
		return "", false
	}
	op := strings.TrimSpace(remainder[:storeIdx])
	store := strings.TrimSpace(remainder[storeIdx+len(" store=") : onIdx])
	on := strings.TrimSpace(remainder[onIdx+len(" on="):])
	if op == "" || store == "" || on == "" {
		return "", false
	}
	return op + "__" + store + "__" + on, true
}

func classifyExecuteScriptFilterFactKey(fact string) (string, bool) {
	fact = strings.TrimSpace(fact)
	if !strings.HasPrefix(fact, "execute_script confirmed filter ") {
		return "", false
	}
	remainder := strings.TrimPrefix(fact, "execute_script confirmed filter ")
	fieldIdx := strings.Index(remainder, "field=")
	opIdx := strings.Index(remainder, " op=")
	if fieldIdx != 0 || opIdx <= len("field=") {
		return "", false
	}
	field := strings.TrimSpace(remainder[len("field="):opIdx])
	op := strings.TrimSpace(remainder[opIdx+len(" op="):])
	if field == "" || op == "" {
		return "", false
	}
	return field + "__" + op, true
}

func classifyConfirmedStoreFactByMarker(remainder string, marker string) (string, string, bool) {
	idx := strings.Index(remainder, " "+marker)
	if idx <= 0 {
		return "", "", false
	}
	storeName := strings.TrimSpace(remainder[:idx])
	if storeName == "" {
		return "", "", false
	}
	factValue := strings.TrimSpace(remainder[idx+1+len(marker):])
	if factValue == "" {
		return "", "", false
	}
	return storeName, factValue, true
}

func extractPrimaryRelationTarget(factValue string) string {
	factValue = trimRelationFactValue(factValue)
	if factValue == "" {
		return ""
	}
	entry := factValue
	if idx := strings.Index(entry, ","); idx >= 0 {
		entry = entry[:idx]
	}
	entry = strings.TrimSpace(entry)
	if idx := strings.Index(entry, "("); idx > 0 {
		return strings.TrimSpace(entry[:idx])
	}
	return entry
}

func trimRelationFactValue(factValue string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(factValue), "]"), "["))
}

func normalizeMRUFactKey(key string) string {
	key = strings.TrimSpace(strings.ToUpper(key))
	if key == "" {
		return "UNKNOWN"
	}
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	normalized := strings.Trim(b.String(), "_")
	if normalized == "" {
		return "UNKNOWN"
	}
	return normalized
}

func compactOutcomeFacts(facts []string) []string {
	if len(facts) == 0 {
		return nil
	}
	compacted := make([]string, 0, len(facts))
	seen := make(map[string]bool, len(facts))
	for _, fact := range facts {
		fact = compactMRUText(fact, 180)
		if fact == "" || seen[fact] {
			continue
		}
		seen[fact] = true
		compacted = append(compacted, fact)
	}
	return compacted
}

func renderAskOutcomeMRUItems(items []MRUItem) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Context) == "" {
			continue
		}
		parts = append(parts, item.Context)
	}
	return strings.Join(parts, "\n")
}

func summarizeAskOutcomeToolPattern(toolCalls []ai.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	pattern := make([]string, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		name := strings.TrimSpace(toolCall.Name)
		if name == "" {
			continue
		}
		pattern = append(pattern, name)
	}
	if len(pattern) == 0 {
		return ""
	}
	return strings.Join(pattern, " -> ")
}

func compactMRUText(text string, maxLen int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

func sanitizeAssistantContinuityText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	markers := []string{"```json", "```", "<function_call", "Function_call", "{\"script\"", "{ \"script\"", "\n{\n \"script\"", "\n{\n\t\"script\""}
	cut := -1
	for _, marker := range markers {
		idx := strings.Index(trimmed, marker)
		if idx >= 0 && (cut == -1 || idx < cut) {
			cut = idx
		}
	}
	if cut >= 0 {
		prefix := strings.TrimSpace(trimmed[:cut])
		if prefix == "" {
			return "[tool call/script content stripped]"
		}
		return prefix + " [tool call/script content stripped]"
	}
	return trimmed
}

func (a *CopilotAgent) getAskOutcomeContext() string {
	if a.service == nil || a.service.session == nil {
		return ""
	}
	orderedCategories := []string{
		askOutcomeMRUCategoryHeader,
		askOutcomeMRUCategoryDatabase,
		askOutcomeMRUCategoryDomain,
		askOutcomeMRUCategoryQuery,
		askOutcomeMRUCategoryResult,
	}
	items := make([]MRUItem, 0, len(orderedCategories))
	for _, category := range orderedCategories {
		if item, ok := a.findMRUItem(category, MRUSourceAskOutcome, true); ok && normalizeMRUScope(item.Scope) == MRUScopeSession {
			items = append(items, item)
		}
	}
	items = append(items, a.collectAskOutcomeItemsByPrefix(askOutcomeMRUCategoryStoreSchema+"_")...)
	items = append(items, a.collectAskOutcomeItemsByPrefix(askOutcomeMRUCategoryRelations+"_")...)
	items = append(items, a.collectAskOutcomeItemsByPrefix(askOutcomeMRUCategoryJoinSelection+"_")...)
	items = append(items, a.collectAskOutcomeItemsByPrefix(askOutcomeMRUCategoryFilterSelection+"_")...)
	items = append(items, a.collectAskOutcomeItemsByPrefix(askOutcomeMRUCategoryConfirmed+"_")...)
	if item, ok := a.findMRUItem(askOutcomeMRUCategoryToolPattern, MRUSourceAskOutcome, true); ok && normalizeMRUScope(item.Scope) == MRUScopeSession {
		items = append(items, item)
	}
	if item, ok := a.findMRUItem(askOutcomeMRUCategoryGuidance, MRUSourceAskOutcome, true); ok && normalizeMRUScope(item.Scope) == MRUScopeSession {
		items = append(items, item)
	}
	return renderAskOutcomeMRUItems(items)
}

func (a *CopilotAgent) getMemoryContinuationContext(taskClassification TaskContextClassification) string {
	if taskClassification.RoutingGate != RoutingGateContinuity {
		return ""
	}
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return ""
	}
	state := a.service.session.Memory.GetCarryoverState()
	return memoryContinuationSummary(state, ai.CarryoverResetUnsupportedProvider)
}

func (a *CopilotAgent) collectAskOutcomeItemsByPrefix(prefix string) []MRUItem {
	if prefix == "" {
		return nil
	}
	snapshot := a.getMRUSnapshot()
	items := make([]MRUItem, 0, len(snapshot))
	for _, item := range snapshot {
		if item.Source != MRUSourceAskOutcome || normalizeMRUScope(item.Scope) != MRUScopeSession {
			continue
		}
		if strings.HasPrefix(item.Category, prefix) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].LastAccessed < items[j].LastAccessed
	})
	if len(items) > maxProjectedAskOutcomeEntries {
		items = items[:maxProjectedAskOutcomeEntries]
	}
	return items
}

func (a *CopilotAgent) persistAskOutcomeMRU(ctx context.Context, query string, finalText string, toolCalls []ai.ToolCall, outcomeFacts []string) {
	a.clearMRUBySourceAndScope(MRUSourceAskOutcome, MRUScopeSession)
	for _, item := range buildAskOutcomeMRUItems(ctx, query, finalText, toolCalls, outcomeFacts, nil) {
		a.markMRUCategory(item.Category, item.Context, item.Source, item.Scope)
	}
}
