package agent

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/classification"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/jsondb"
)

func cloneTaskContextClassification(taskCtx *TaskContextClassification) *TaskContextClassification {
	if taskCtx == nil {
		return nil
	}
	cloned := *taskCtx
	if taskCtx.DBArtifacts != nil {
		cloned.DBArtifacts = append([]string(nil), taskCtx.DBArtifacts...)
	}
	if taskCtx.StoresArtifacts != nil {
		cloned.StoresArtifacts = append([]string(nil), taskCtx.StoresArtifacts...)
	}
	if taskCtx.SpacesArtifacts != nil {
		cloned.SpacesArtifacts = append([]string(nil), taskCtx.SpacesArtifacts...)
	}
	if taskCtx.Layers != nil {
		cloned.Layers = append([]LayerInfo(nil), taskCtx.Layers...)
	}
	if taskCtx.AskClassification != nil {
		askCtxClone := *taskCtx.AskClassification
		if taskCtx.AskClassification.Domains != nil {
			askCtxClone.Domains = append([]string(nil), taskCtx.AskClassification.Domains...)
		}
		if taskCtx.AskClassification.PhasePlan != nil {
			askCtxClone.PhasePlan = append([]string(nil), taskCtx.AskClassification.PhasePlan...)
		}
		if taskCtx.AskClassification.CRUD != nil {
			askCtxClone.CRUD = append([]string(nil), taskCtx.AskClassification.CRUD...)
		}
		cloned.AskClassification = &askCtxClone
	}
	return &cloned
}

// formatKBSourceLinks renders every available DocID as a clickable markdown link.
func (a *CopilotAgent) formatKBSourceLinks(ctx context.Context, docIDs memory.DocIDs) string {
	if len(docIDs) == 0 {
		return ""
	}

	parts := make([]string, 0, len(docIDs))
	for _, docID := range docIDs {
		docID = strings.TrimSpace(docID)
		if docID == "" {
			continue
		}

		viewerURL := &url.URL{Path: "/viewer"}
		q := viewerURL.Query()
		q.Set("docID", docID)
		viewerURL.RawQuery = q.Encode()

		if ctx != nil {
			if baseURL, ok := ctx.Value(ai.CtxKeyAppBaseURL).(string); ok && strings.TrimSpace(baseURL) != "" {
				base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
				if base != "" {
					parsedBase, err := url.Parse(base)
					if err == nil {
						parsedBase.Path = strings.TrimRight(parsedBase.Path, "/") + viewerURL.Path
						parsedBase.RawQuery = viewerURL.RawQuery
						viewerURL = parsedBase
					}
				}
			}
		}

		parts = append(parts, fmt.Sprintf("[%s](%s)", docID, viewerURL.String()))
	}
	if len(parts) == 0 {
		return ""
	}

	return fmt.Sprintf("\nSources: %s", strings.Join(parts, ", "))
}

// formatKBSearchResultsForDisplay formats KB search results for direct display (Case 1: Few matches)
func (a *CopilotAgent) formatKBSearchResultsForDisplay(results string, matchCount int) string {
	var sb strings.Builder

	// Check if this is category navigation (matchCount=0 but results contain "Available Categories" or "Subcategories")
	isCategoryNavigation := matchCount == 0 && (strings.Contains(results, "Available Categories:") || strings.Contains(results, "Subcategories under"))

	if isCategoryNavigation {
		// Direct category navigation - just return the formatted results
		sb.WriteString(results)
		sb.WriteString("\n\n---\n")
		sb.WriteString("*Tip: Use `:llm <instruction>` to filter results with AI assistance*")
	} else {
		// Regular search results
		sb.WriteString(fmt.Sprintf("**Found %d knowledge base match", matchCount))
		if matchCount != 1 {
			sb.WriteString("es")
		}
		sb.WriteString(":**\n\n")
		sb.WriteString(results)
		sb.WriteString("\n\n---\n")
		sb.WriteString("*Tip: Use `:llm <instruction>` to filter results with AI assistance*")
	}

	return sb.String()
}

// buildKBEnrichedQuery enriches the query with KB search results for LLM processing (Case 2 & 3)
// The cleanQuery parameter should be the user's intent WITHOUT the :llm meta-token
func (a *CopilotAgent) buildKBEnrichedQuery(cleanQuery, kbResults, llmInstruction string, matchCount int) string {
	var sb strings.Builder

	// Start with the clean query (without :llm meta-token)
	if strings.TrimSpace(cleanQuery) != "" {
		sb.WriteString(cleanQuery)
		sb.WriteString("\n\n")
	}

	// Add KB context header
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("**Knowledge Base Search Results (%d matches):**\n", matchCount))
	sb.WriteString(kbResults)
	sb.WriteString("\n---\n\n")

	// Add instruction
	if llmInstruction != "" {
		sb.WriteString(fmt.Sprintf("**Instruction:** %s\n", llmInstruction))
	} else {
		// Default reduction instruction for too many matches (Case 3)
		sb.WriteString("**Instruction:** Please analyze the search results above and present the most relevant matches to the user.\n")
	}
	// Preserve source labels and links verbatim so the final answer can remain readable and clickable.
	sb.WriteString("**Source Preservation:** Preserve the exact source labels and markdown source links from the KB results verbatim in your answer. Do not replace them with plain relative file paths or bare filenames.\n")

	return sb.String()
}

func shouldPreserveMRUOnTopicSwitch(item MRUItem) bool {
	return item.Source == MRUSourcePersona || strings.HasPrefix(item.Category, "PERSONA_")
}

func isRehydratableMRUItem(item MRUItem) bool {
	if normalizeMRUScope(item.Scope) != MRUScopeSession {
		return false
	}
	switch item.Source {
	case MRUSourcePersona, MRUSourceSystemTools, MRUSourceAskOutcome, MRUSourcePlaybook:
		return true
	default:
		return strings.HasPrefix(item.Category, "PERSONA_")
	}
}

func projectedMRUSourcePriority(source string) int {
	switch source {
	case MRUSourcePersona:
		return 0
	case MRUSourceSystemTools:
		return 1
	case MRUSourceAskOutcome:
		return 2
	case MRUSourcePlaybook:
		return 3
	default:
		return 4
	}
}

func projectedMRUSourceLimit(source string) int {
	switch source {
	case MRUSourcePersona:
		return maxProjectedPersonaEntries
	case MRUSourceSystemTools:
		return maxProjectedSystemToolEntries
	case MRUSourceAskOutcome:
		return maxProjectedAskOutcomeEntries
	case MRUSourcePlaybook:
		return maxProjectedPlaybookEntries
	default:
		return 0
	}
}

func projectMRUItemsFromSTM(snapshot []MRUItem, activeKB string) []MRUItem {
	if len(snapshot) == 0 && strings.TrimSpace(activeKB) == "" {
		return nil
	}

	candidates := make([]MRUItem, 0, len(snapshot)+4)
	for _, item := range snapshot {
		if !isRehydratableMRUItem(item) {
			continue
		}
		candidates = append(candidates, item)
	}

	for _, kb := range strings.Split(activeKB, ",") {
		kb = strings.TrimSpace(kb)
		if kb == "" {
			continue
		}
		candidates = append(candidates, MRUItem{
			Category: playbookMRUCategory(kb),
			Source:   MRUSourcePlaybook,
			Scope:    MRUScopeSession,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		leftPriority := projectedMRUSourcePriority(candidates[i].Source)
		rightPriority := projectedMRUSourcePriority(candidates[j].Source)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if candidates[i].LastAccessed != candidates[j].LastAccessed {
			return candidates[i].LastAccessed > candidates[j].LastAccessed
		}
		return candidates[i].Category < candidates[j].Category
	})

	seenCategories := make(map[string]bool, len(candidates))
	perSourceCounts := make(map[string]int, 3)
	projected := make([]MRUItem, 0, len(candidates))
	for _, item := range candidates {
		if seenCategories[item.Category] {
			continue
		}
		limit := projectedMRUSourceLimit(item.Source)
		if limit == 0 || perSourceCounts[item.Source] >= limit {
			continue
		}
		seenCategories[item.Category] = true
		perSourceCounts[item.Source]++
		projected = append(projected, item)
	}

	return projected
}

func summarizeProjectedMRU(items []MRUItem) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := item.Category
		if item.Source != "" {
			label = item.Source + ":" + item.Category
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ",")
}

func summarizePromptComponentsPresent(report PromptBudgetReport) string {
	present := make([]string, 0, len(report.ComponentStats))
	for _, stat := range report.ComponentStats {
		if stat.FinalChars > 0 {
			present = append(present, string(stat.Component))
		}
	}
	return strings.Join(present, ",")
}

func (a *CopilotAgent) rehydrateMRUFromMemory(ctx context.Context) {
	if a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return
	}

	session := a.service.session
	stm := session.Memory

	if payload := ai.GetSessionPayload(ctx); payload != nil {
		if payload.Variables == nil {
			payload.Variables = make(map[string]any)
		}
		if _, ok := payload.Variables["RoutingState"].(*TaskContextClassification); !ok {
			if restored := stm.GetRoutingState(); restored != nil {
				payload.Variables["RoutingState"] = restored
			}
		}
	}

	activeKB := ""
	if thread := stm.GetCurrentThread(); thread != nil && len(thread.Exchanges) > 0 {
		activeKB = thread.Exchanges[len(thread.Exchanges)-1].ActiveKB
	}

	var codingContext string
	for _, caveat := range stm.GetRuntimeCaveats(3) {
		if caveat.CategoryRoot != "coding_workflow" && caveat.Source != "coding_workflow" && caveat.Kind != "coding_workflow" {
			continue
		}
		if codingContext == "" {
			codingContext = strings.TrimSpace(caveat.Summary)
		} else {
			codingContext += "\n" + strings.TrimSpace(caveat.Summary)
		}
		if details := strings.TrimSpace(caveat.Details); details != "" {
			codingContext += ": " + details
		}
	}
	if codingContext != "" {
		a.markMRUCategoryWithSource("CODING_WORKFLOW_LAST", codingContext, MRUSourceAskOutcome)
	}

	projected := projectMRUItemsFromSTM(stm.GetMRUSnapshot(), activeKB)
	for _, item := range projected {
		a.markMRUCategoryWithSource(item.Category, item.Context, item.Source)
	}
	if summary := summarizeProjectedMRU(projected); summary != "" {
		log.Info("STM MRU Projection", "projected_items", summary)
	}
}

// CopilotAgent is a specialized agent for database administration tasks.
// It implements the ai.Agent interface.
type LTMPressureNotification struct {
	Timestamp     time.Time
	Severity      string
	Title         string
	Message       string
	UsageRatio    float64
	PressureState string
	Threshold     float64
	Action        string
	Retriiggered  bool
	PrunedCount   int
	ArchivedCount int
	FreeUpTargets []string
	FreeUpContext string
}

type LTMPressureNotifier func(notification LTMPressureNotification)

var (
	defaultLTMPressureNotifier LTMPressureNotifier = func(LTMPressureNotification) {}
	ltmPressureEventHookMu     sync.Mutex
	ltmPressureEventHook       func(LTMPressureNotification)
)

func SetDefaultLTMPressureNotifier(notifier LTMPressureNotifier) {
	if notifier == nil {
		defaultLTMPressureNotifier = func(LTMPressureNotification) {}
		return
	}
	defaultLTMPressureNotifier = notifier
}

func DefaultLTMPressureNotifier() LTMPressureNotifier {
	return defaultLTMPressureNotifier
}

func SetLTMPressureEventHook(hook func(LTMPressureNotification)) {
	ltmPressureEventHookMu.Lock()
	defer ltmPressureEventHookMu.Unlock()
	ltmPressureEventHook = hook
}

func EmitLTMPressureNotification(notification LTMPressureNotification) {
	ltmPressureEventHookMu.Lock()
	hook := ltmPressureEventHook
	ltmPressureEventHookMu.Unlock()
	if hook != nil {
		hook(notification)
	}
}

type CopilotAgent struct {
	Config       Config
	brain        ai.Generator
	registry     *Registry
	databases    map[string]sop.DatabaseOptions
	systemDB     *database.Database
	lastToolCall *ai.ScriptStep
	service      *Service // Reference back to main service for cache invalidation

	// Encapsulated memory and context boundaries
	Memory *memory.MemoryUnit

	// LTM storage pressure user transparency
	LTMPressureNotifier      LTMPressureNotifier
	ltmPressureAlertMu       sync.Mutex
	lastLTMPressureAlert     time.Time
	ltmPressureAlertCooldown time.Duration

	// Compiled Scripts Cache
	compiledScripts   map[string]CachedScript
	compiledScriptsMu sync.RWMutex

	// StoreOpener allows mocking the store creation (e.g. for testing)
	StoreOpener func(ctx context.Context, dbOpts sop.DatabaseOptions, storeName string, tx sop.Transaction) (jsondb.StoreAccessor, error)

	// capabilityRegistry provides modular domain capabilities for Ask routing and context building.
	capabilityRegistry *capabilities.Registry
	ideWorkerBackend   ideworker.Backend

	// Gap 1: Progressive authorization system
	authzManager *capabilities.AuthorizationManager

	// Gap 3: Lifecycle management
	lifecycleManager *capabilities.LifecycleManager

	// Gap 2: Classification engine (MRU + keyword + LLM)
	classificationEngine classification.ClassificationEngine

	// Phase 5: Production hardening components
	retryStrategy  *RetryStrategy
	circuitBreaker *CircuitBreaker
	queueMetrics   *QueueMetrics
	queuePersist   *QueuePersistence
	enablePhase5   bool

	// Session queue for workflow automation
	sessionQueue   *WorkQueue
	sessionQueueMu sync.Mutex

	// memoryWorkersCancel stops background STM workers when the agent is closed.
	memoryWorkersCancel context.CancelFunc
}

// Clone creates a new isolated instance of the agent sharing read-only components.
func (a *CopilotAgent) Clone() ai.Agent[map[string]any] {
	return &CopilotAgent{
		Config:    a.Config,
		brain:     a.brain,
		registry:  a.registry, // Pointer to registry
		databases: a.databases,
		systemDB:  a.systemDB,
		Memory: &memory.MemoryUnit{
			AgentID: ai.AgentIDOmni, // By default Clone behaves as Omni, Avatars explicitly override
		},
		lastToolCall:     nil,
		service:          nil, // Caller should populate this
		compiledScripts:  make(map[string]CachedScript),
		StoreOpener:      a.StoreOpener,
		ideWorkerBackend: a.ideWorkerBackend,
	}
}

// SetGenerator sets the generator for the agent.
func (a *CopilotAgent) SetGenerator(gen ai.Generator) {
	a.brain = gen
}

func (a *CopilotAgent) recordCodingMemory(mem cw.CodingMemory) {
	if a == nil {
		return
	}
	if a.service == nil || a.service.session == nil {
		return
	}
	sess := a.service.session
	if sess.Memory == nil {
		sess.Memory = NewShortTermMemory()
	}

	// Write coding memory to STM for consolidation via sleep cycle
	if a.Memory != nil {
		// Format coding memory as intent/outcome for STM logging
		intent := fmt.Sprintf("coding_workflow: %s", strings.TrimSpace(mem.Description))
		outcome := strings.TrimSpace(mem.Outcome)

		// Create structured payload for STM
		astPayload := map[string]any{
			"event_type":      "coding_workflow",
			"capability":      "coding_workflow",
			"description":     strings.TrimSpace(mem.Description),
			"outcome":         outcome,
			"files_touched":   mem.FilesTouched,
			"test_status":     strings.TrimSpace(mem.TestStatus),
			"repository_hint": strings.TrimSpace(mem.RepositoryHint),
			"lesson":          strings.TrimSpace(mem.Lesson),
			"next_step":       strings.TrimSpace(mem.NextStep),
			"plan":            mem.Plan,
			"active_file":     mem.ActiveFile,
		}
		if len(mem.KBsUsed) > 0 {
			astPayload["kbs_used"] = mem.KBsUsed
		}

		a.Memory.LogEpisodeToSTM(context.Background(), intent, astPayload, outcome, nil)
	}

	summary := strings.TrimSpace(mem.Description)
	if summary == "" {
		summary = "Recent coding workflow"
	}
	details := make([]string, 0, 4)
	if mem.Outcome != "" {
		details = append(details, mem.Outcome)
	}
	if len(mem.FilesTouched) > 0 {
		details = append(details, "files="+strings.Join(mem.FilesTouched, ","))
	}
	if mem.TestStatus != "" {
		details = append(details, "tests="+mem.TestStatus)
	}
	if mem.RepositoryHint != "" {
		details = append(details, "repo="+mem.RepositoryHint)
	}
	if len(mem.Plan) > 0 {
		details = append(details, "plan="+strings.Join(mem.Plan, " | "))
	}
	if len(details) > 0 {
		sess.Memory.AddRuntimeCaveat(summary, strings.Join(details, "; "), "coding_workflow", "coding_workflow", "coding_workflow")
	}

	ctx := summary
	if mem.Outcome != "" {
		ctx = fmt.Sprintf("Recent coding workflow: %s\n- outcome: %s", summary, mem.Outcome)
	}
	if len(mem.FilesTouched) > 0 {
		ctx += fmt.Sprintf("\n- files: %s", strings.Join(mem.FilesTouched, ", "))
	}
	if mem.TestStatus != "" {
		ctx += fmt.Sprintf("\n- tests: %s", mem.TestStatus)
	}
	if len(mem.Plan) > 0 {
		ctx += fmt.Sprintf("\n- plan: %s", strings.Join(mem.Plan, " | "))
	}
	if sess.Memory != nil {
		sess.Memory.SetCodingWorkflowState(&CodingWorkflowExecutionState{
			ActiveFile:     mem.ActiveFile,
			Description:    mem.Description,
			CurrentStep:    mem.NextStep,
			Plan:           append([]string(nil), mem.Plan...),
			LastOutcome:    mem.Outcome,
			LastTestStatus: mem.TestStatus,
			UpdatedAt:      time.Now().UnixMilli(),
		})
	}
	if strings.TrimSpace(mem.Outcome) != "" && (strings.TrimSpace(mem.TestStatus) == "" || !strings.Contains(strings.ToLower(mem.TestStatus), "fail")) {
		a.promoteSuccessfulOutcomeToLTM(context.Background(), summary, mem.Outcome)
	}
	a.markMRUCategoryWithSource("CODING_WORKFLOW_LAST", ctx, MRUSourceAskOutcome)
}

func (a *CopilotAgent) promoteSuccessfulOutcomeToLTM(ctx context.Context, query, outcome string) {
	if a == nil || a.systemDB == nil || strings.TrimSpace(outcome) == "" {
		return
	}
	if a.Memory == nil {
		a.Memory = memory.NewMemoryUnit(ai.AgentIDOmni)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	category := buildRecallPromotionCategoryPath(query)
	if category == "" {
		category = "Coding/Semantic/general"
	}

	payload := map[string]any{
		"type":        "successful_outcome",
		"query":       strings.TrimSpace(query),
		"outcome":     strings.TrimSpace(outcome),
		"category":    category,
		"source":      "durable_ltm_promotion",
		"promoted_at": time.Now().UTC().Format(time.RFC3339),
	}

	var embedder ai.Embeddings
	if a.service != nil && a.service.Domain() != nil {
		embedder = a.service.Domain().Embedder()
	}

	tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	kb, err := a.systemDB.OpenKnowledgeBase(ctx, a.Memory.LongTermMemoryName(), tx, a.brain, embedder, false, true)
	if err != nil {
		return
	}
	if err := kb.IngestThought(ctx, strings.TrimSpace(outcome), category, "system", nil, payload); err != nil {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}
}

func (a *CopilotAgent) recordPlannerPlan(query string, planText string) {
	if a == nil {
		return
	}
	if a.service == nil || a.service.session == nil {
		return
	}
	sess := a.service.session
	if sess.Memory == nil {
		sess.Memory = NewShortTermMemory()
	}

	normalized := strings.TrimSpace(planText)
	if normalized == "" {
		return
	}

	summary := "Proposed plan"
	if trimmedQuery := strings.TrimSpace(query); trimmedQuery != "" {
		summary = fmt.Sprintf("Plan for: %s", trimmedQuery)
	}

	details := sanitizeAssistantContinuityText(normalized)
	sess.Memory.AddRuntimeCaveat(summary, details, "planner", "planner", "planner_capability")
	a.markMRUCategoryWithSource("PLANNER_PLAN_LAST", fmt.Sprintf("Planning context: %s\n- plan:\n%s", summary, details), MRUSourceAskOutcome)
}
