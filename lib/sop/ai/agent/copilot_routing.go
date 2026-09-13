package agent

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/memory"
)

type routingOutcome struct {
	taskCtx *TaskContextClassification
	matched bool
	source  string
}

func (a *CopilotAgent) classifyIntent(ctx context.Context, query string, gen ai.Generator) string {
	_ = ctx
	_ = gen
	query = strings.TrimSpace(query)
	if query == "" {
		return ai.IntentOmni
	}
	upper := strings.ToUpper(query)
	if strings.HasPrefix(upper, "/") {
		return ai.IntentOmni
	}
	return ai.IntentOmni
}

func isRoutingTestGenerator(gen ai.Generator) bool {
	if gen == nil {
		return false
	}
	name := strings.ToLower(gen.Name())
	return strings.Contains(name, "test") || strings.Contains(name, "mock")
}

func parseRoutingAnchor(query string) *TaskContextClassification {
	_ = query
	return nil
}

func (a *CopilotAgent) finalizeRoutingResult(ctx context.Context, query string, outcome *routingOutcome, gate string) *TaskContextClassification {
	if outcome == nil || outcome.taskCtx == nil {
		return nil
	}

	taskCtx := cloneTaskContextClassification(outcome.taskCtx)
	if taskCtx == nil {
		return nil
	}
	if strings.TrimSpace(taskCtx.RoutingGate) == "" {
		taskCtx.RoutingGate = gate
	}
	if strings.TrimSpace(taskCtx.Domain) == "" {
		taskCtx.Domain = GeneralDomain
	}
	if taskCtx.RoutingGate == RoutingGateContinuity {
		a.applyCapabilityRoutingHints(ctx, taskCtx, query)
	}
	a.persistRoutingState(ctx, taskCtx)
	return taskCtx
}

func (a *CopilotAgent) applyCapabilityRoutingHints(ctx context.Context, taskCtx *TaskContextClassification, query string) {
	_ = ctx
	_ = query
	if a == nil || taskCtx == nil || taskCtx.RoutingGate != RoutingGateContinuity {
		return
	}
	if taskCtx.CapabilityHint != "" && taskCtx.IntentFamily != "" {
		return
	}
	item, ok := a.findMRUItem(capabilityAffinityMRUCategory, MRUSourceAskOutcome, true)
	if !ok {
		return
	}
	contextText := strings.TrimSpace(item.Context)
	if taskCtx.CapabilityHint == "" {
		if cap := extractRoutingValue(contextText, "capability="); cap != "" {
			taskCtx.CapabilityHint = cap
		}
	}
	if taskCtx.IntentFamily == "" {
		if intent := extractRoutingValue(contextText, "intent="); intent != "" {
			taskCtx.IntentFamily = intent
		}
	}
}

func extractRoutingValue(text, prefix string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, segment := range strings.Split(text, ";") {
		segment = strings.TrimSpace(segment)
		if strings.HasPrefix(strings.ToLower(segment), strings.ToLower(prefix)) {
			return strings.TrimSpace(strings.TrimPrefix(segment, prefix))
		}
	}
	return ""
}

func (a *CopilotAgent) resetRoutingForTopicSwitch(query string, payload *ai.SessionPayload) {
	_ = query
	if a == nil {
		return
	}
	a.clearMRUCategory(capabilityAffinityMRUCategory)
	if payload != nil && payload.Variables != nil {
		delete(payload.Variables, "RoutingState")
	}
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		a.service.session.Memory.SetRoutingState(nil)
	}
}

func (a *CopilotAgent) persistRoutingState(ctx context.Context, taskCtx *TaskContextClassification) {
	if a == nil || taskCtx == nil {
		return
	}
	if a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		a.service.session.Memory.SetRoutingState(taskCtx)
	}
	p := ai.GetSessionPayload(ctx)
	if p == nil {
		slog.Warn("persistRoutingState: no session payload available; skipping persisted routing state")
		return
	}
	if p.Variables == nil {
		p.Variables = make(map[string]any)
	}
	p.Variables["RoutingState"] = taskCtx
}

func (a *CopilotAgent) tryPrefixBasedRouting(ctx context.Context, query string, gen ai.Generator, isTest bool, anchor *TaskContextClassification) (*TaskContextClassification, error) {
	_ = ctx
	_ = gen
	_ = isTest
	_ = anchor
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || !looksLikeSpecializedRoutingQuery(trimmed) {
		return nil, nil
	}
	if strings.Contains(strings.ToLower(trimmed), "stores") {
		taskCtx := &TaskContextClassification{Domain: StoresDomain, RoutingGate: RoutingGateFocused}
		taskCtx.StoresArtifacts = []string{"stores"}
		annotateTaskContextIntent(taskCtx, query)
		return taskCtx, nil
	}
	if strings.Contains(strings.ToLower(trimmed), "spaces") {
		taskCtx := &TaskContextClassification{Domain: SpacesDomain, RoutingGate: RoutingGateFocused}
		taskCtx.SpacesArtifacts = []string{"spaces"}
		annotateTaskContextIntent(taskCtx, query)
		return taskCtx, nil
	}
	taskCtx := &TaskContextClassification{Domain: GeneralDomain, RoutingGate: RoutingGateFocused}
	annotateTaskContextIntent(taskCtx, query)
	return taskCtx, nil
}

func (a *CopilotAgent) tryAskContinuationBasedRouting(ctx context.Context, query string, gen ai.Generator, isTest bool, anchor *TaskContextClassification) (*TaskContextClassification, error) {
	_ = ctx
	_ = gen
	_ = isTest
	_ = query
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return nil, nil
	}
	persisted := a.service.session.Memory.GetRoutingState()
	if persisted == nil && len(a.getMRUSnapshot()) == 0 {
		return nil, nil
	}
	taskCtx := cloneTaskContextClassification(persisted)
	if taskCtx == nil {
		taskCtx = &TaskContextClassification{}
	}
	if synthesized := a.activeCodingWorkstreamContinuationTaskContext(query, taskCtx); synthesized != nil {
		taskCtx = synthesized
	}
	if strings.TrimSpace(taskCtx.RoutingGate) == "" {
		taskCtx.RoutingGate = RoutingGateContinuity
	}
	if strings.TrimSpace(taskCtx.Domain) == "" {
		taskCtx.Domain = GeneralDomain
	}
	if anchor != nil {
		if strings.TrimSpace(taskCtx.Domain) == "" || strings.TrimSpace(taskCtx.Domain) == GeneralDomain {
			taskCtx.Domain = anchor.Domain
		}
		if len(taskCtx.StoresArtifacts) == 0 && len(anchor.StoresArtifacts) > 0 {
			taskCtx.StoresArtifacts = append([]string(nil), anchor.StoresArtifacts...)
		}
		if len(taskCtx.SpacesArtifacts) == 0 && len(anchor.SpacesArtifacts) > 0 {
			taskCtx.SpacesArtifacts = append([]string(nil), anchor.SpacesArtifacts...)
		}
	}
	simplifyCodingWorkflowContinuation(taskCtx, query)
	a.applyCapabilityRoutingHints(ctx, taskCtx, query)
	return taskCtx, nil
}

func (a *CopilotAgent) activeCodingWorkstreamContinuationTaskContext(query string, fallback *TaskContextClassification) *TaskContextClassification {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return nil
	}
	if !isLikelyMetaConversationFollowUp(query) && !isRetryMetaAsk(query) && !isShortWorkstreamContinuationPrompt(query) {
		return nil
	}
	if fallback != nil && strings.EqualFold(strings.TrimSpace(fallback.Domain), "coding_workflow") {
		return nil
	}

	thread := a.service.session.Memory.GetCurrentThread()
	if thread == nil || thread.StreamContext == nil || thread.StreamContext.PhasePlan == nil {
		return nil
	}
	plan := thread.StreamContext.PhasePlan
	if plan.ExecutionBrief == nil {
		return nil
	}

	plannedPhases := canonicalizeCodingRuntimePlan(&AskTaskContext{Domains: []string{"coding_workflow"}}, append([]string(nil), plan.PlannedPhases...))
	if len(plannedPhases) == 0 {
		plannedPhases = []string{"investigation", "planning", "execution"}
	}
	currentPhase := normalizePhase(strings.TrimSpace(thread.StreamContext.CurrentPhase))
	if currentPhase == "" {
		idx := plan.CurrentIndex
		if idx >= 0 && idx < len(plan.PlannedPhases) {
			currentPhase = normalizePhase(plan.PlannedPhases[idx])
		}
	}
	currentPhase = canonicalizeCodingRuntimePhase(&AskTaskContext{Domains: []string{"coding_workflow"}}, currentPhase)
	if currentPhase == "" {
		currentPhase = "investigation"
	}
	if pattern, _, ok := classifyContinuationPrompt(query); ok && pattern == "advance_request" && currentPhase == "planning" && plan.ExecutionBrief != nil {
		currentPhase = "execution"
	}

	taskCtx := cloneTaskContextClassification(fallback)
	if taskCtx == nil {
		taskCtx = &TaskContextClassification{}
	}
	taskCtx.Domain = "coding_workflow"
	taskCtx.RoutingGate = RoutingGateContinuity
	taskCtx.CapabilityHint = currentPhase
	taskCtx.GeneralContext = false
	taskCtx.IntentFamily = intentFamilyRepoEdit

	askCtx := cloneAskTaskContext(taskCtx.AskClassification)
	if askCtx == nil {
		askCtx = &AskTaskContext{}
	}
	askCtx.Domains = []string{"coding_workflow"}
	askCtx.TaskShape = taskShapeIssueInvestigation
	askCtx.OrchestrationLevel = orchestrationManagedObjective
	askCtx.Phase = currentPhase
	askCtx.PhasePlan = plannedPhases
	askCtx.Intent = "generate_code"
	askCtx.CRUD = []string{"R", "U"}
	askCtx.ExecutionReady = currentPhase == "execution" && plan.ExecutionBrief != nil
	askCtx.Confidence = firstNonEmpty(strings.TrimSpace(askCtx.Confidence), "high")
	askCtx.Reasoning = "Continuity preserved the active coding workstream for a short follow-up while the persisted routing state had drifted."
	askCtx.OriginalQuery = firstNonEmpty(strings.TrimSpace(askCtx.OriginalQuery), strings.TrimSpace(thread.RootPrompt), strings.TrimSpace(query))
	a.promoteExecutionReadinessFromPlanningArtifact(askCtx)
	taskCtx.AskClassification = askCtx
	return taskCtx
}

func isShortWorkstreamContinuationPrompt(query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(query))
	switch normalized {
	case "proceed", "continue", "go on", "keep going", "did you fix it", "did you fix it?":
		return true
	default:
		return false
	}
}

func simplifyCodingWorkflowContinuation(taskCtx *TaskContextClassification, query string) {
	if taskCtx == nil || isExplicitDesktopDirective(query) {
		return
	}

	isCodingWorkflow := false
	if taskCtx.AskClassification != nil && askClassificationHasDomain(taskCtx.AskClassification.Domains, "coding_workflow") {
		isCodingWorkflow = true
	}
	switch strings.TrimSpace(strings.ToLower(taskCtx.Domain)) {
	case "coding_workflow", "coding", "codingworkflow":
		isCodingWorkflow = true
	}
	switch strings.TrimSpace(strings.ToLower(taskCtx.CapabilityHint)) {
	case "coding_workflow", "coding", "codingworkflow", "investigation", "planning", "execution", "review", "delivery":
		isCodingWorkflow = true
	}
	if strings.EqualFold(taskCtx.IntentFamily, intentFamilyRepoEdit) {
		isCodingWorkflow = true
	}
	if !isCodingWorkflow {
		return
	}

	taskCtx.Domain = "coding_workflow"
	taskCtx.CapabilityHint = "execution"
	if strings.TrimSpace(taskCtx.IntentFamily) == "" || strings.EqualFold(taskCtx.IntentFamily, intentFamilyGeneral) {
		taskCtx.IntentFamily = intentFamilyRepoEdit
	}

	if taskCtx.AskClassification == nil {
		taskCtx.AskClassification = &AskTaskContext{
			Domains:       []string{"coding_workflow"},
			Phase:         "investigation",
			PhasePlan:     []string{"investigation", "planning", "execution"},
			Intent:        "generate_code",
			CRUD:          []string{"R", "U"},
			Confidence:    "high",
			Reasoning:     "Continuity keeps additive coding follow-ups in the coding workflow while the runtime surface moves directly into implementation mode.",
			OriginalQuery: query,
		}
		return
	}

	taskCtx.AskClassification.Domains = []string{"coding_workflow"}
	taskCtx.AskClassification.PhasePlan = canonicalizeCodingRuntimePlan(taskCtx.AskClassification, taskCtx.AskClassification.PhasePlan)
	if !contains(taskCtx.AskClassification.PhasePlan, "execution") {
		taskCtx.AskClassification.PhasePlan = append(taskCtx.AskClassification.PhasePlan, "execution")
	}
	if len(taskCtx.AskClassification.PhasePlan) == 0 {
		taskCtx.AskClassification.PhasePlan = []string{"investigation", "planning", "execution"}
	}
	if strings.TrimSpace(taskCtx.AskClassification.Phase) == "" {
		taskCtx.AskClassification.Phase = "investigation"
	}
	switch strings.TrimSpace(strings.ToLower(taskCtx.AskClassification.Intent)) {
	case "", "general", "analyze", "plan":
		taskCtx.AskClassification.Intent = "generate_code"
	}
	if len(taskCtx.AskClassification.CRUD) == 0 {
		taskCtx.AskClassification.CRUD = []string{"R", "U"}
	}
	if strings.TrimSpace(taskCtx.AskClassification.OriginalQuery) == "" {
		taskCtx.AskClassification.OriginalQuery = query
	}
}

func (a *CopilotAgent) tryColdStartBasedRouting(ctx context.Context, query string, gen ai.Generator, isTest bool) (*TaskContextClassification, error) {
	_ = isTest
	if gen == nil {
		return nil, nil
	}
	classificationResult, err := a.ClassifyAskTaskContext(ctx, query, gen)
	if err != nil {
		return nil, err
	}
	if classificationResult == nil {
		return nil, nil
	}
	taskCtx := &TaskContextClassification{
		Domain:            strings.Join(classificationResult.Domains, ","),
		CapabilityHint:    classificationResult.Phase,
		RoutingGate:       RoutingGateDiscovery,
		AskClassification: classificationResult,
	}
	if strings.TrimSpace(taskCtx.Domain) == "" {
		taskCtx.Domain = GeneralDomain
	}
	annotateTaskContextIntent(taskCtx, query)
	return taskCtx, nil
}

func buildCodingWorkflowExpertiseQuery(task cw.Task, pack cw.ContextPack) string {
	parts := []string{}
	if strings.TrimSpace(task.Description) != "" {
		parts = append(parts, strings.TrimSpace(task.Description))
	}
	if strings.TrimSpace(pack.TaskType) != "" {
		parts = append(parts, fmt.Sprintf("task type: %s", strings.TrimSpace(pack.TaskType)))
	}
	if strings.TrimSpace(pack.ActiveFile) != "" {
		parts = append(parts, fmt.Sprintf("active file: %s", strings.TrimSpace(pack.ActiveFile)))
	}
	if language := inferCodingWorkflowLanguage(pack.ActiveFile); language != "" {
		parts = append(parts, fmt.Sprintf("language: %s", language))
	}
	if subArea := inferCodingWorkflowSubArea(pack); subArea != "" {
		parts = append(parts, fmt.Sprintf("sub-area: %s", subArea))
	}
	if len(pack.Symbols) > 0 {
		parts = append(parts, fmt.Sprintf("symbols: %s", strings.Join(pack.Symbols, ", ")))
	}
	if len(pack.DependencyFiles) > 0 {
		parts = append(parts, fmt.Sprintf("dependencies: %s", strings.Join(pack.DependencyFiles, ", ")))
	}
	if len(pack.PriorPaths) > 0 {
		parts = append(parts, fmt.Sprintf("prior paths: %s", strings.Join(pack.PriorPaths, ", ")))
	}
	if len(parts) == 0 {
		return "coding best practices for implementation and testing"
	}
	return strings.Join(parts, " | ")
}

func inferCodingWorkflowLanguage(activeFile string) string {
	activeFile = strings.TrimSpace(activeFile)
	if activeFile == "" {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(activeFile))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".java":
		return "java"
	case ".cs":
		return "csharp"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	default:
		return ""
	}
}

func inferCodingWorkflowSubArea(pack cw.ContextPack) string {
	candidates := make([]string, 0, 4)
	for _, leaf := range pack.ASTLeafNodes {
		leaf = strings.TrimSpace(leaf)
		if leaf == "" {
			continue
		}
		leafLower := strings.ToLower(leaf)
		switch {
		case strings.Contains(leafLower, "parser"):
			candidates = append(candidates, "parser")
		case strings.Contains(leafLower, "validator"):
			candidates = append(candidates, "validation")
		case strings.Contains(leafLower, "handler"):
			candidates = append(candidates, "http")
		case strings.Contains(leafLower, "test"):
			candidates = append(candidates, "testing")
		case strings.Contains(leafLower, "retry"):
			candidates = append(candidates, "retry")
		case strings.Contains(leafLower, "cache"):
			candidates = append(candidates, "caching")
		}
	}
	if len(candidates) == 0 {
		if strings.TrimSpace(pack.StructuralPath) != "" {
			parts := strings.Split(pack.StructuralPath, "/")
			for _, part := range parts {
				part = strings.TrimSpace(strings.ToLower(part))
				if part == "" {
					continue
				}
				if part == "repo" || part == "src" || part == "lib" || part == "app" || part == "code" {
					continue
				}
				candidates = append(candidates, part)
				if len(candidates) >= 2 {
					break
				}
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return strings.Join(uniqueStrings(candidates), ",")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func selectExpertiseKnowledgeBaseCandidates(query string, available []string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]struct{})
	ordered := make([]string, 0, len(available)+2)
	push := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		ordered = append(ordered, name)
	}
	push(ai.DefaultKBName)
	if query != "" {
		for _, name := range available {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			lower := strings.ToLower(name)
			if strings.Contains(lower, "parser") || strings.Contains(lower, "code") || strings.Contains(lower, "engineering") || strings.Contains(lower, "dev") || strings.Contains(lower, "testing") || strings.Contains(lower, "qa") || strings.Contains(lower, "review") {
				push(name)
			}
		}
		for _, name := range available {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			lower := strings.ToLower(name)
			if lower == ai.DefaultKBName {
				continue
			}
			if strings.Contains(lower, "guide") || strings.Contains(lower, "playbook") || strings.Contains(lower, "pattern") || strings.Contains(lower, "best") || strings.Contains(lower, "practice") || strings.Contains(lower, "workflow") {
				push(name)
			}
		}
		for _, name := range available {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			lower := strings.ToLower(name)
			if lower == ai.DefaultKBName {
				continue
			}
			if strings.Contains(query, "parser") && strings.Contains(lower, "parser") {
				push(name)
			}
			if strings.Contains(query, "test") && strings.Contains(lower, "test") {
				push(name)
			}
			if strings.Contains(query, "bug") && strings.Contains(lower, "bug") {
				push(name)
			}
			if strings.Contains(query, "review") && strings.Contains(lower, "review") {
				push(name)
			}
		}
	}
	for _, name := range available {
		push(name)
	}
	return ordered
}

func buildSelectedSpaceGenerationGuidance(systemPrompt, purpose, query string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	purpose = strings.TrimSpace(purpose)
	query = strings.ToLower(strings.TrimSpace(query))

	parts := make([]string, 0, 3)
	if systemPrompt != "" {
		parts = append(parts, fmt.Sprintf("persona: %s", systemPrompt))
	}
	if purpose != "" {
		parts = append(parts, fmt.Sprintf("purpose: %s", purpose))
	}
	if strings.Contains(query, "test") || strings.Contains(query, "coverage") || strings.Contains(query, "parser") || strings.Contains(query, "implement") || strings.Contains(query, "bug") || strings.Contains(query, "fix") {
		parts = append(parts, "for code changes, prefer minimal diffs, add tests where appropriate, and surface uncertainties")
	}
	parts = append(parts, "before answering, generating code, or acting, first research the selected knowledge base/Space or LTM memory by using the prompt and prior Ask turns to generate focused nested categories, then read the most relevant items/thoughts in those branches rather than consuming the entire knowledge source")
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Actionable generation guidance: %s", strings.Join(parts, " | "))
}

func buildSelectedSpaceResearchMemoryHints(query string, state *ResearchWorkflowState) string {
	parts := buildSelectedSpaceResearchContextParts(query, state)
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Research memory hints: %s", strings.Join(parts, " | "))
}

func buildSelectedSpaceResearchQueries(query, kbName string, state *ResearchWorkflowState, categorizerPaths []string) []string {
	base := []string{}
	query = strings.TrimSpace(query)
	kbName = strings.TrimSpace(kbName)
	for _, semanticPath := range buildSemanticResearchPaths(query) {
		if semanticPath != "" && !containsString(base, semanticPath) {
			base = append(base, semanticPath)
		}
	}
	if query != "" {
		base = append(base, query)
	}
	if kbName != "" {
		base = append(base, kbName)
	}
	if state == nil {
		return base
	}
	for _, category := range filterResearchMemoryValues(state.SuccessfulCategoryPaths) {
		if category != "" && !containsString(base, category) {
			base = append(base, category)
		}
	}
	for _, category := range filterResearchMemoryValues(state.SuccessfulCategories) {
		if category != "" && !containsString(base, category) {
			base = append(base, category)
		}
	}
	for _, item := range filterResearchMemoryValues(state.SuccessfulItemPaths) {
		if item != "" && !containsString(base, item) {
			base = append(base, item)
		}
	}
	for _, item := range filterResearchMemoryValues(state.SuccessfulItems) {
		if item != "" && !containsString(base, item) {
			base = append(base, item)
		}
	}
	if query != "" {
		for _, value := range filterResearchMemoryValues(state.SuccessfulCategories) {
			if value != "" {
				base = append(base, query+" "+value)
			}
		}
		for _, value := range filterResearchMemoryValues(state.SuccessfulItems) {
			if value != "" {
				base = append(base, query+" "+value)
			}
		}
	}
	if len(categorizerPaths) > 0 {
		filtered := make([]string, 0, len(base))
		for _, candidate := range base {
			if strings.Contains(candidate, "Semantic/") || strings.Contains(candidate, "/") {
				filtered = append(filtered, candidate)
			}
		}
		if len(filtered) > 0 {
			return filtered
		}
	}
	return base
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func filterResearchMemoryValues(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return filtered
}

func joinSelectedSpaceExpertiseSections(parts []string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, " | ")
}

func (a *CopilotAgent) Retrieve(task cw.Task, state cw.RunState, pack cw.ContextPack) (cw.ExpertiseCapsule, error) {
	return a.retrieveCodingWorkflowExpertise(context.Background(), task, state, pack)
}

func (a *CopilotAgent) hasExpertiseEntitlement(state cw.RunState) bool {
	if a == nil {
		return false
	}
	if state.Metadata == nil {
		return false
	}

	for _, key := range []string{"expertise_enabled", "expertise_retrieval_enabled", "premium_expertise", "entitlement_expertise_retrieval", "entitlement:expertise_retrieval"} {
		if value, ok := state.Metadata[key]; ok {
			normalized := strings.ToLower(strings.TrimSpace(value))
			if normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on" || normalized == "enabled" {
				return true
			}
			if normalized == "false" || normalized == "0" || normalized == "no" || normalized == "off" || normalized == "disabled" {
				return false
			}
		}
	}
	if value, ok := state.Metadata["subscription_features"]; ok {
		for _, feature := range strings.Split(value, ",") {
			feature = strings.ToLower(strings.TrimSpace(feature))
			if feature == "expertise_retrieval" || feature == "premium_expertise" || feature == "expertise" {
				return true
			}
		}
	}
	if value, ok := state.Metadata["subscription_plan"]; ok {
		plan := strings.ToLower(strings.TrimSpace(value))
		return plan == "premium" || plan == "pro" || plan == "enterprise" || plan == "team"
	}
	return false
}

func (a *CopilotAgent) retrieveCodingWorkflowExpertise(ctx context.Context, task cw.Task, state cw.RunState, pack cw.ContextPack) (cw.ExpertiseCapsule, error) {
	if a == nil {
		return cw.ExpertiseCapsule{}, fmt.Errorf("agent is nil")
	}
	if !a.hasExpertiseEntitlement(state) {
		return cw.ExpertiseCapsule{}, nil
	}
	if a.systemDB == nil {
		return cw.ExpertiseCapsule{}, fmt.Errorf("system database is nil")
	}
	query := buildCodingWorkflowExpertiseQuery(task, pack)
	if strings.TrimSpace(query) == "" {
		return cw.ExpertiseCapsule{}, fmt.Errorf("empty expertise query")
	}
	tx, err := a.systemDB.BeginTransaction(ctx, sop.NoCheck)
	if err != nil {
		return cw.ExpertiseCapsule{}, fmt.Errorf("begin expertise tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var embedder ai.Embeddings
	if a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil {
		embedder = a.service.Domain().Embedder()
	}

	selectedSpaceSections := []string{}
	if p := ai.GetSessionPayload(ctx); p != nil && len(p.SelectedKBs) > 0 {
		for _, kbRef := range p.SelectedKBs {
			name := strings.TrimSpace(kbRef.Name)
			if name == "" {
				continue
			}
			if section := a.selectedSpaceExpertiseSection(ctx, name, query); section != "" {
				selectedSpaceSections = append(selectedSpaceSections, section)
			}
		}
	}

	availableKBs := []string{ai.DefaultKBName}
	if tx != nil {
		if stores, err := tx.GetStores(ctx); err == nil {
			for _, store := range stores {
				if strings.HasSuffix(store, "/sys_config") {
					name := strings.TrimSuffix(store, "/sys_config")
					if strings.TrimSpace(name) != "" {
						availableKBs = append(availableKBs, name)
					}
				}
			}
		}
	}
	candidates := selectExpertiseKnowledgeBaseCandidates(query, availableKBs)
	results := make([]string, 0, 6)
	sources := make([]string, 0, len(candidates))
	for _, kbName := range candidates {
		kbName = strings.TrimSpace(kbName)
		if kbName == "" {
			continue
		}
		kb, err := a.systemDB.OpenKnowledgeBase(ctx, kbName, tx, a.brain, embedder, false)
		if err != nil {
			continue
		}
		searchReq := memory.SearchRequest[map[string]any]{Text: query, Limit: 3}
		if semanticPaths := buildSemanticResearchPaths(query); len(semanticPaths) > 0 {
			searchReq.CategoryPaths = append([]string(nil), semanticPaths...)
		}
		batch, err := kb.Search(ctx, []memory.SearchRequest[map[string]any]{searchReq})
		if err != nil || len(batch) == 0 || len(batch[0]) == 0 {
			continue
		}
		for _, hit := range batch[0] {
			text := extractHitText(hit.Payload)
			if strings.TrimSpace(text) != "" {
				results = append(results, text)
				if len(results) >= 6 {
					break
				}
			}
		}
		sources = append(sources, kbName)
		if len(results) >= 6 {
			break
		}
	}
	if len(results) == 0 && len(selectedSpaceSections) == 0 {
		return cw.ExpertiseCapsule{}, nil
	}
	if len(results) == 0 {
		return cw.ExpertiseCapsule{Summary: joinSelectedSpaceExpertiseSections(selectedSpaceSections), Category: "selected-space", Sources: sources}, nil
	}
	if len(selectedSpaceSections) == 0 {
		return cw.ExpertiseCapsule{Summary: strings.Join(results, " | "), Category: "kb", Sources: sources}, nil
	}
	return cw.ExpertiseCapsule{Summary: joinSelectedSpaceExpertiseSections(append([]string{strings.Join(results, " | ")}, selectedSpaceSections...)), Category: "kb+selected-space", Sources: sources}, nil
}
