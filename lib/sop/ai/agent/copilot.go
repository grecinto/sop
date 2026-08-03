package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/classification"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/agent/domains/desktop"
	"github.com/sharedcode/sop/ai/agent/domains/spaces"
	"github.com/sharedcode/sop/ai/agent/domains/stores"
	multistepplanner "github.com/sharedcode/sop/ai/agent/multistepplanner"
	"github.com/sharedcode/sop/ai/agent/parser"
	plannerpkg "github.com/sharedcode/sop/ai/agent/planner"
	"github.com/sharedcode/sop/ai/agent/teampatterns"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/embed"
	"github.com/sharedcode/sop/ai/generator"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/ai/obfuscation"
	"github.com/sharedcode/sop/btree"
	"github.com/sharedcode/sop/jsondb"
)

const (
	MaxMRUSize            = 20
	MaxExchangesInHistory = 20
	// selectedSpaceReinforcementWindow is a TTL for selected-space expert framing.
	// We only fully re-inject the Space guidance when the current Space has not been
	// reinforced recently, otherwise we keep the prompt compact for continuity.
	selectedSpaceReinforcementWindow = 30 * time.Minute
)

const (
	MRUSourceUnknown     = ""
	MRUSourcePersona     = "persona"
	MRUSourceSystemTools = "system_tools"
	MRUSourceAskOutcome  = "ask_outcome"
	MRUSourceAskProgress = "ask_progress"
	MRUSourcePlaybook    = "playbook"
)

const (
	MRUScopeSession = "session"
	MRUScopeAsk     = "ask"
)

const (
	maxProjectedPersonaEntries    = 2
	maxProjectedSystemToolEntries = 1
	maxProjectedAskOutcomeEntries = 10
	maxProjectedPlaybookEntries   = 4
)

const (
	ctxKeyDeferImplicitSessionTxClose = "_defer_implicit_session_tx_close"
	ctxKeyWorkingDir                  = "working_dir"
	ctxKeySelectedCapability          = "selected_capability"
	ctxKeyCapabilityResult            = "capability_result"
	ctxKeyActiveStep                  = "active_step"
)

const (
	askOutcomeMRUCategoryHeader          = "ASK_OUTCOME_HEADER"
	askOutcomeMRUCategoryDatabase        = "ASK_OUTCOME_DATABASE"
	askOutcomeMRUCategoryDomain          = "ASK_OUTCOME_DOMAIN"
	askOutcomeMRUCategoryQuery           = "ASK_OUTCOME_QUERY"
	askOutcomeMRUCategoryResult          = "ASK_OUTCOME_RESULT"
	askOutcomeMRUCategoryStoreSchema     = "ASK_OUTCOME_STORE_SCHEMA"
	askOutcomeMRUCategoryRelations       = "ASK_OUTCOME_STORE_RELATIONS"
	askOutcomeMRUCategoryJoinSelection   = "ASK_OUTCOME_JOIN_SELECTION"
	askOutcomeMRUCategoryFilterSelection = "ASK_OUTCOME_FILTER_SELECTION"
	askOutcomeMRUCategoryConfirmed       = "ASK_OUTCOME_CONFIRMED"
	askOutcomeMRUCategoryToolPattern     = "ASK_OUTCOME_TOOL_PATTERN"
	askOutcomeMRUCategoryCarryover       = "ASK_OUTCOME_CARRYOVER"
	askOutcomeMRUCategoryGuidance        = "ASK_OUTCOME_GUIDANCE"
	askProgressMRUCategoryHeader         = "ASK_PROGRESS_HEADER"
	askProgressMRUCategoryResult         = "ASK_PROGRESS_RESULT"
	askProgressMRUCategoryToolPattern    = "ASK_PROGRESS_TOOL_PATTERN"
	askProgressMRUCategoryConfirmed      = "ASK_PROGRESS_CONFIRMED"
	askProgressMRUCategoryGuidance       = "ASK_PROGRESS_GUIDANCE"
)

const personaSourceMRUCategoryPrefix = "PERSONA_SOURCE_"

// MRUItem represents a single category currently in working memory
type MRUItem struct {
	Category     string
	LastAccessed int64
	Context      string
	Source       string
	Scope        string
}

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

	// Compiled Scripts Cache
	compiledScripts   map[string]CachedScript
	compiledScriptsMu sync.RWMutex

	// StoreOpener allows mocking the store creation (e.g. for testing)
	StoreOpener func(ctx context.Context, dbOpts sop.DatabaseOptions, storeName string, tx sop.Transaction) (jsondb.StoreAccessor, error)

	// capabilityRegistry provides modular domain capabilities for Ask routing and context building.
	capabilityRegistry *capabilities.Registry

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
		lastToolCall:    nil,
		service:         nil, // Caller should populate this
		compiledScripts: make(map[string]CachedScript),
		StoreOpener:     a.StoreOpener,
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
	a.markMRUCategoryWithSource("CODING_WORKFLOW_LAST", ctx, MRUSourceAskOutcome)
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

func buildLTMMemoryCategoryPath(category, capability, scopeType, scopeID string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		category = "General"
	}
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	parts := []string{strings.Title(strings.ToLower(category))}
	if scopeType != "" && scopeID != "" {
		parts = append(parts, strings.Title(strings.ToLower(scopeType)))
		parts = append(parts, scopeID)
	} else if capability != "" {
		parts = append(parts, strings.Title(strings.ToLower(strings.ReplaceAll(capability, "_", " "))))
	}
	return strings.Join(parts, "/")
}

type codingMemoryLTMIndexEntry struct {
	Kind         string
	CategoryPath string
	Payload      map[string]any
}

func shouldVectorizeLTMIndexEntry(entry codingMemoryLTMIndexEntry) bool {
	return strings.EqualFold(strings.TrimSpace(entry.Kind), "semantic")
}

func buildCodingMemoryLTMPayloadAndIndexes(mem cw.CodingMemory) (map[string]any, []codingMemoryLTMIndexEntry) {
	docID := sop.NewUUID()
	scopeType := "repo"
	scopeID := strings.TrimSpace(mem.RepositoryHint)
	if scopeID == "" {
		scopeID = "default"
	}
	canonicalPayload := map[string]any{
		"document_id":      docID.String(),
		"payload_type":     "coding_workflow_memory",
		"description":      strings.TrimSpace(mem.Description),
		"active_file":      strings.TrimSpace(mem.ActiveFile),
		"files_touched":    mem.FilesTouched,
		"outcome":          strings.TrimSpace(mem.Outcome),
		"test_status":      strings.TrimSpace(mem.TestStatus),
		"lesson":           strings.TrimSpace(mem.Lesson),
		"next_step":        strings.TrimSpace(mem.NextStep),
		"repository_hint":  strings.TrimSpace(mem.RepositoryHint),
		"structural_path":  strings.TrimSpace(mem.StructuralPath),
		"ast_leaf_nodes":   mem.ASTLeafNodes,
		"status":           "active",
		"lifecycle_status": "active",
		"scope_type":       scopeType,
		"scope_id":         scopeID,
		"recorded_at":      time.Now().UnixMilli(),
	}
	pathIndexPath := buildCodingMemoryPathIndexCategoryPath(mem)
	codeStructureIndexPath := buildCodingMemoryCodeStructureCategoryPath(mem)
	semanticIndexPath := buildCodingMemorySemanticCategoryPath(mem)
	indexPayload := map[string]any{
		"document_id":          docID.String(),
		"payload_type":         "coding_workflow_memory",
		"canonical_payload":    canonicalPayload,
		"path_index":           pathIndexPath,
		"code_structure_index": codeStructureIndexPath,
		"semantic_index":       semanticIndexPath,
	}
	entries := []codingMemoryLTMIndexEntry{
		{Kind: "path", CategoryPath: pathIndexPath, Payload: indexPayload},
		{Kind: "code_structure", CategoryPath: codeStructureIndexPath, Payload: indexPayload},
		{Kind: "semantic", CategoryPath: semanticIndexPath, Payload: indexPayload},
	}
	return indexPayload, entries
}

func buildCodingMemoryLTMThoughts(mem cw.CodingMemory, documentID string, entries []codingMemoryLTMIndexEntry) []memory.Thought[map[string]any] {
	thoughts := make([]memory.Thought[map[string]any], 0, len(entries))
	for _, entry := range entries {
		text := fmt.Sprintf("Coding workflow outcome: %s", strings.TrimSpace(mem.Description))
		if mem.Outcome != "" {
			text = fmt.Sprintf("%s\nOutcome: %s", text, mem.Outcome)
		}
		if len(mem.FilesTouched) > 0 {
			text = fmt.Sprintf("%s\nFiles: %s", text, strings.Join(mem.FilesTouched, ", "))
		}
		if mem.TestStatus != "" {
			text = fmt.Sprintf("%s\nTests: %s", text, mem.TestStatus)
		}
		mode := memory.IndexPopulationModeFull
		if strings.EqualFold(strings.TrimSpace(entry.Kind), "path") || strings.EqualFold(strings.TrimSpace(entry.Kind), "code_structure") {
			mode = memory.IndexPopulationModeReference
		}
		thoughts = append(thoughts, memory.Thought[map[string]any]{
			Summaries:           []string{text},
			CategoryPath:        entry.CategoryPath,
			DocID:               memory.DocIDs{documentID},
			IndexPopulationMode: mode,
			Data: map[string]any{
				"document_id":          documentID,
				"index_kind":           entry.Kind,
				"canonical_payload":    entry.Payload["canonical_payload"],
				"path_index":           entry.Payload["path_index"],
				"code_structure_index": entry.Payload["code_structure_index"],
				"semantic_index":       entry.Payload["semantic_index"],
			},
		})
	}
	return thoughts
}

func buildCodingMemoryPathIndexCategoryPath(mem cw.CodingMemory) string {
	parts := []string{"Coding", "Workflow"}
	repoHint := strings.TrimSpace(mem.RepositoryHint)
	if repoHint != "" {
		parts = append(parts, "Repo", repoHint)
	} else {
		parts = append(parts, "Repo", "Default")
	}
	if activeFile := strings.TrimSpace(mem.ActiveFile); activeFile != "" {
		parts = append(parts, "File", filepath.Base(activeFile))
	}
	return strings.Join(parts, "/")
}

// buildCodingMemoryCodeStructureCategoryPath builds hierarchical code-tree category paths
// for O(1) categorical lookup by package/type/method.
func buildCodingMemoryCodeStructureCategoryPath(mem cw.CodingMemory) string {
	// Use AST/Tree-sitter structural path if available
	// Builds code-tree paths like: "Coding/Go/Package/ai.agent.codingworkflow/Type/Orchestrator/Method/Execute"
	if structuralPath := strings.TrimSpace(mem.StructuralPath); structuralPath != "" {
		return buildCodeTreeCategoryPath(mem.ActiveFile, structuralPath, mem.ASTLeafNodes)
	}
	// No structural path available
	return ""
}

// buildCodingMemorySemanticCategoryPath builds NESTED SEMANTIC category paths
// for vector search within semantic "pigeon holes".
//
// Key: Use the SAME FORMULA during storage and retrieval so queries land in correct buckets.
// This solves the scaling problem - can't do flat vector search over millions of snippets,
// but CAN if pre-organized into semantic buckets.
//
// Spaces semantic navigation handles fuzzy matching between vocabulary variants:
//
//	"error-handling" ≈ "exception-handling" ≈ "fault-tolerance"
//	"http-networking" ≈ "http" ≈ "rest-api"
//	Just like: "Technology/Silicon Valley" ≈ "Tech/Palo Alto"
//
// Examples:
//   - "Coding/Semantic/error-handling/retry-logic"
//   - "Coding/Semantic/data-persistence/database/sql-query"
//   - "Coding/Semantic/http-networking/middleware/authentication"
func buildCodingMemorySemanticCategoryPath(mem cw.CodingMemory) string {
	parts := []string{"Coding", "Semantic"}
	seed := []string{}
	if desc := strings.TrimSpace(mem.Description); desc != "" {
		seed = append(seed, desc)
	}
	if outcome := strings.TrimSpace(mem.Outcome); outcome != "" {
		seed = append(seed, outcome)
	}
	if lesson := strings.TrimSpace(mem.Lesson); lesson != "" {
		seed = append(seed, lesson)
	}
	if nextStep := strings.TrimSpace(mem.NextStep); nextStep != "" {
		seed = append(seed, nextStep)
	}
	if activeFile := strings.TrimSpace(mem.ActiveFile); activeFile != "" {
		seed = append(seed, filepath.Base(activeFile))
	}
	for _, candidate := range seed {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		normalized = strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-").Replace(normalized)
		normalized = strings.Trim(normalized, "-")
		if normalized == "" {
			continue
		}
		parts = append(parts, normalized)
		if len(parts) >= 6 {
			break
		}
	}
	// Extract semantic concepts from code content
	// Use deterministic keyword extraction (same formula for storage & retrieval)
	concepts := extractSemanticConcepts(mem)

	// Build nested semantic category path (max 5 levels deep for pigeon holes)
	for i, concept := range concepts {
		if i >= 3 { // Limit depth: Coding/Semantic/{c1}/{c2}/{c3}
			break
		}
		parts = append(parts, concept)
	}

	// Ensure at least one semantic concept
	if len(parts) == 2 {
		parts = append(parts, "general")
	}

	return strings.Join(parts, "/")
}

// extractSemanticConcepts extracts semantic concept keywords from coding memory
// using a DETERMINISTIC formula (same for storage and retrieval).
//
// This is the key to the "pigeon hole" approach:
// 1. Storage: extract concepts from code → build semantic path → store in bucket
// 2. Retrieval: extract concepts from query → build semantic path → search in bucket
// 3. Match: same formula means query lands in correct semantic bucket
func extractSemanticConcepts(mem cw.CodingMemory) []string {
	text := strings.Join([]string{
		mem.Description,
		mem.Outcome,
		mem.Lesson,
		mem.NextStep,
	}, " ")
	concepts := extractSemanticConceptsFromText(text)
	for _, node := range mem.ASTLeafNodes {
		nodeLower := strings.ToLower(node)
		if strings.Contains(nodeLower, "handler") {
			if !containsString(concepts, "http") {
				concepts = append(concepts, "http")
			}
		} else if strings.Contains(nodeLower, "parser") {
			if !containsString(concepts, "parsing") {
				concepts = append(concepts, "parsing")
			}
		} else if strings.Contains(nodeLower, "validator") {
			if !containsString(concepts, "validation") {
				concepts = append(concepts, "validation")
			}
		}
	}
	return concepts
}

func extractSemanticConceptsFromText(text string) []string {
	var concepts []string
	seenConcepts := make(map[string]bool)

	// Semantic keyword patterns (deterministic extraction)
	// Note: Spaces semantic navigation handles fuzzy matching, so:
	//   "error-handling" ≈ "exception-handling" ≈ "fault-tolerance"
	//   "retry-logic" ≈ "retry-pattern" ≈ "backoff-strategy"
	// We just need consistent conceptual vocabulary - Spaces bridges variants.
	semanticPatterns := []struct {
		concept  string
		keywords []string
	}{
		{concept: "parsing", keywords: []string{"parse", "parser", "decode", "unmarshal", "deserialize", "lexer", "syntax"}},
		{concept: "retry-logic", keywords: []string{"retry", "backoff", "exponential", "circuit-breaker", "resilience"}},
		{concept: "error-handling", keywords: []string{"error", "exception", "failure", "fault", "crash", "panic"}},
		{concept: "data-persistence", keywords: []string{"database", "query", "sql", "orm", "repository", "storage", "persist"}},
		{concept: "http-networking", keywords: []string{"http", "rest", "api", "endpoint", "request", "response", "client", "server"}},
		{concept: "authentication", keywords: []string{"auth", "login", "token", "jwt", "session", "credential", "oauth", "sso"}},
		{concept: "validation", keywords: []string{"validate", "validator", "check", "verify", "sanitize", "constraint"}},
		{concept: "observability", keywords: []string{"log", "logger", "trace", "debug", "monitor", "metric", "telemetry"}},
		{concept: "testing", keywords: []string{"test", "mock", "assert", "fixture", "suite", "unit", "integration"}},
		{concept: "caching", keywords: []string{"cache", "redis", "memcache", "ttl", "memoize", "invalidate"}},
		{concept: "concurrency", keywords: []string{"goroutine", "thread", "async", "parallel", "mutex", "lock", "race", "sync"}},
		{concept: "configuration", keywords: []string{"config", "settings", "env", "environment", "flag", "parameter"}},
		{concept: "middleware", keywords: []string{"middleware", "interceptor", "filter", "handler", "pipeline"}},
		{concept: "serialization", keywords: []string{"json", "xml", "yaml", "proto", "marshal", "encode", "format"}},
		{concept: "security", keywords: []string{"security", "encrypt", "decrypt", "hash", "sign", "certificate", "tls"}},
		{concept: "routing", keywords: []string{"route", "router", "path", "url", "match", "dispatch"}},
		{concept: "transformation", keywords: []string{"transform", "convert", "map", "reduce", "filter", "process"}},
		{concept: "scheduling", keywords: []string{"schedule", "cron", "timer", "interval", "periodic", "job"}},
	}

	textLower := strings.ToLower(text)

	// Match semantic patterns
	for _, pattern := range semanticPatterns {
		for _, keyword := range pattern.keywords {
			if strings.Contains(textLower, keyword) {
				if !seenConcepts[pattern.concept] {
					concepts = append(concepts, pattern.concept)
					seenConcepts[pattern.concept] = true
					break
				}
			}
		}
	}

	return concepts
}

func buildSemanticResearchPaths(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	concepts := extractSemanticConceptsFromText(query)
	if len(concepts) == 0 {
		return nil
	}
	parts := []string{"Coding", "Semantic"}
	for _, concept := range concepts {
		if len(parts) >= 5 {
			break
		}
		parts = append(parts, concept)
	}
	if len(parts) <= 2 {
		return nil
	}
	return []string{strings.Join(parts, "/")}
}

// buildCodeTreeCategoryPath constructs hierarchical code-tree category paths
// from tree-sitter/AST structural information for O(1) categorical lookup.
//
// Examples:
//   - "Coding/Go/Package/ai.agent.codingworkflow/Type/Orchestrator/Method/Execute"
//   - "Coding/Python/Package/retriever/Class/VectorRetriever/Method/search"
//   - "Coding/TypeScript/Package/utils/Namespace/format/Function/formatDate"
func buildCodeTreeCategoryPath(activeFile, structuralPath string, astLeafNodes []string) string {
	// Detect language from file extension
	ext := strings.ToLower(filepath.Ext(activeFile))
	language := "Generic"
	symbolType := "Function"

	switch ext {
	case ".go":
		language = "Go"
		// In Go: could be Function or Method depending on receiver
		// For simplicity, use Type if multiple AST nodes (suggesting struct + methods)
		if len(astLeafNodes) > 1 {
			symbolType = "Type"
		}
	case ".py":
		language = "Python"
		symbolType = "Class"
	case ".ts", ".tsx":
		language = "TypeScript"
		symbolType = "Namespace"
	case ".js", ".jsx":
		language = "JavaScript"
		symbolType = "Namespace"
	case ".java":
		language = "Java"
		symbolType = "Class"
	case ".cs":
		language = "CSharp"
		symbolType = "Namespace"
	case ".rb":
		language = "Ruby"
		symbolType = "Class"
	case ".rs":
		language = "Rust"
		symbolType = "Module"
	}

	// Parse structural path: "sop-cloud/ai/agent/Execute"
	// Extract: package/module path and symbol name
	pathParts := strings.Split(structuralPath, "/")
	if len(pathParts) < 2 {
		// Fallback: use simple path
		return fmt.Sprintf("Coding/%s/%s/%s", language, symbolType, structuralPath)
	}

	// Skip repo name (first part), extract package path and symbol
	var packagePath string
	var symbolName string

	if len(pathParts) == 2 {
		// "repo/Symbol"
		symbolName = pathParts[1]
		packagePath = "main"
	} else {
		// "repo/path/to/package/Symbol"
		symbolName = pathParts[len(pathParts)-1]
		packageParts := pathParts[1 : len(pathParts)-1]
		packagePath = strings.Join(packageParts, ".")
	}

	// Build hierarchical code-tree path
	parts := []string{"Coding", language, "Package", packagePath}

	// Add symbol type and name
	if len(astLeafNodes) > 1 {
		// Multiple AST nodes suggest Type with Methods
		parts = append(parts, symbolType, astLeafNodes[0])
		if len(astLeafNodes) > 1 {
			parts = append(parts, "Method", astLeafNodes[1])
		}
	} else if len(astLeafNodes) == 1 {
		// Single AST node: use as function/method
		parts = append(parts, symbolType, astLeafNodes[0])
	} else {
		// No AST nodes: use extracted symbol from path
		parts = append(parts, symbolType, symbolName)
	}

	return strings.Join(parts, "/")
}

func buildLTMMemoryMetadata(category, capability, scopeType, scopeID string) map[string]any {
	category = strings.TrimSpace(category)
	capability = strings.TrimSpace(capability)
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	if category == "" {
		category = "general"
	}
	if capability == "" {
		capability = "general"
	}
	if scopeType == "" {
		scopeType = "global"
	}
	if scopeID == "" {
		scopeID = "global"
	}
	now := time.Now().UnixMilli()
	return map[string]any{
		"category":                    category,
		"capability":                  capability,
		"scope_type":                  scopeType,
		"scope_id":                    scopeID,
		"provenance":                  "agent",
		"status":                      "active",
		"lifecycle_status":            "active",
		"lifecycle_access_count":      1,
		"lifecycle_last_used_at":      now,
		"lifecycle_last_evaluated_at": now,
		"updated_at":                  now,
	}
}

func isRepoScopedLTMPayload(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	scopeType := strings.TrimSpace(fmt.Sprint(payload["scope_type"]))
	if scopeType == "" {
		scopeType = strings.TrimSpace(fmt.Sprint(payload["scopeType"]))
	}
	if scopeType != "" && !strings.EqualFold(scopeType, "repo") {
		return false
	}
	if scopeType == "repo" {
		return true
	}
	repoHint := strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
	if repoHint == "" {
		repoHint = strings.TrimSpace(fmt.Sprint(payload["scope_id"]))
	}
	if repoHint == "" {
		repoHint = strings.TrimSpace(fmt.Sprint(payload["scopeID"]))
	}
	return repoHint != ""
}

func touchLTMLifecycleForRepoAccess(payload map[string]any, now time.Time) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if !isRepoScopedLTMPayload(payload) {
		return payload
	}
	if now.IsZero() {
		now = time.Now()
	}
	accessCount := 1
	if existingCount, ok := payload["lifecycle_access_count"].(int); ok {
		accessCount = existingCount + 1
	} else if existingCount, ok := payload["lifecycle_access_count"].(int64); ok {
		accessCount = int(existingCount) + 1
	} else if existingCount, ok := payload["lifecycle_access_count"].(float64); ok {
		accessCount = int(existingCount) + 1
	}
	lastUsedAt := now.UnixMilli()
	payload["lifecycle_access_count"] = accessCount
	payload["lifecycle_last_used_at"] = lastUsedAt
	payload["lifecycle_last_evaluated_at"] = lastUsedAt
	payload["updated_at"] = lastUsedAt
	payload["status"] = "active"
	payload["lifecycle_status"] = "active"
	payload["scope_valid"] = true
	delete(payload, "archived_at")
	delete(payload, "archive_reason")
	return payload
}

func touchRepoScopedLTMLifecycleState(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], scopeID string, now time.Time) error {
	if ctx == nil || kb == nil || strings.TrimSpace(scopeID) == "" {
		return nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}
	scopeID = strings.TrimSpace(scopeID)
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		payload, okPayload := any(item.Data).(map[string]any)
		if !okPayload || payload == nil {
			payload = map[string]any{}
		}
		if isRepoScopedLTMPayload(payload) {
			payloadScopeID := strings.TrimSpace(fmt.Sprint(payload["scope_id"]))
			if payloadScopeID == "" {
				payloadScopeID = strings.TrimSpace(fmt.Sprint(payload["scopeID"]))
			}
			if payloadScopeID == "" {
				payloadScopeID = strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
			}
			if strings.EqualFold(payloadScopeID, scopeID) {
				payload = touchLTMLifecycleForRepoAccess(payload, now)
				item.Data = payload
				if _, err := itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item); err != nil {
					return err
				}
			}
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func updateLTMMemoryLifecycleState(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], itemID string, relevanceScore int) error {
	if ctx == nil || kb == nil || strings.TrimSpace(itemID) == "" {
		return nil
	}

	parsedID, err := sop.ParseUUID(itemID)
	if err != nil {
		return nil
	}

	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}

	ok, err := itemsTree.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		if item.ID == parsedID {
			payload, ok := any(item.Data).(map[string]any)
			if !ok {
				payload = map[string]any{}
			}
			if payload == nil {
				payload = map[string]any{}
			}
			now := time.Now()
			payload = touchLTMLifecycleForRepoAccess(payload, now)
			if !isRepoScopedLTMPayload(payload) {
				accessCount := 1
				if existingCount, ok := payload["lifecycle_access_count"].(int); ok {
					accessCount = existingCount + 1
					payload["lifecycle_access_count"] = accessCount
				} else if existingCount, ok := payload["lifecycle_access_count"].(int64); ok {
					accessCount = int(existingCount) + 1
					payload["lifecycle_access_count"] = accessCount
				} else if existingCount, ok := payload["lifecycle_access_count"].(float64); ok {
					accessCount = int(existingCount) + 1
					payload["lifecycle_access_count"] = accessCount
				} else {
					payload["lifecycle_access_count"] = accessCount
				}
				lastUsedAt := now.UnixMilli()
				if existingLastUsedAt, ok := payload["lifecycle_last_used_at"].(int64); ok {
					lastUsedAt = existingLastUsedAt
				} else if existingLastUsedAt, ok := payload["lifecycle_last_used_at"].(int); ok {
					lastUsedAt = int64(existingLastUsedAt)
				} else if existingLastUsedAt, ok := payload["lifecycle_last_used_at"].(float64); ok {
					lastUsedAt = int64(existingLastUsedAt)
				}
				recordedAt := now.UnixMilli()
				if existingRecordedAt, ok := payload["recorded_at"].(int64); ok {
					recordedAt = existingRecordedAt
				} else if existingRecordedAt, ok := payload["recorded_at"].(int); ok {
					recordedAt = int64(existingRecordedAt)
				} else if existingRecordedAt, ok := payload["recorded_at"].(float64); ok {
					recordedAt = int64(existingRecordedAt)
				}
				lastUsedDays := 0
				if now.UnixMilli() > lastUsedAt {
					lastUsedDays = int((now.UnixMilli() - lastUsedAt) / int64(24*time.Hour/time.Millisecond))
				}
				ageDays := 0
				if now.UnixMilli() > recordedAt {
					ageDays = int((now.UnixMilli() - recordedAt) / int64(24*time.Hour/time.Millisecond))
				}
				scopeValid := true
				if existingScopeValid, ok := payload["scope_valid"].(bool); ok {
					scopeValid = existingScopeValid
				}
				status := evaluateLTMLifecycleStatus(lastUsedDays, ageDays, accessCount, relevanceScore, scopeValid)
				payload["lifecycle_last_used_at"] = now.UnixMilli()
				payload["lifecycle_last_evaluated_at"] = now.UnixMilli()
				payload["updated_at"] = now.UnixMilli()
				payload["status"] = status.State
				payload["lifecycle_status"] = status.State
			}
			item.Data = payload
			_, err = itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item)
			return err
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

type ltmRetrievalTarget struct {
	Category   string
	Capability string
	ScopeType  string
	ScopeID    string
}

type ltmLifecycleRetentionDecision struct {
	Action     string
	Reason     string
	Confidence int
}

type ltmLifecycleSignal struct {
	Action     string
	ScopeType  string
	ScopeID    string
	Reason     string
	Confidence int
}

type ltmLifecycleStatus struct {
	State        string
	Reason       string
	LastUsedDays int
	AgeDays      int
	Score        int
}

func applyLTMLifecycleSignal(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], signal ltmLifecycleSignal) error {
	if ctx == nil || kb == nil || signal.Action == "" {
		return nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		payload, okPayload := any(item.Data).(map[string]any)
		if !okPayload || payload == nil {
			payload = map[string]any{}
		}
		if !ltmLifecycleSignalMatchesPayload(payload, signal) {
			ok, err = itemsTree.Next(ctx)
			if err != nil {
				return err
			}
			continue
		}
		switch strings.ToLower(signal.Action) {
		case "retire":
			payload["lifecycle_status"] = "archived"
			payload["status"] = "archived"
			payload["archived_at"] = now
			payload["archive_reason"] = strings.TrimSpace(signal.Reason)
			if signal.Reason == "" {
				payload["archive_reason"] = "explicit_user_cue"
			}
			payload["lifecycle_last_evaluated_at"] = now
			payload["updated_at"] = now
		case "reactivate":
			payload["lifecycle_status"] = "active"
			payload["status"] = "active"
			payload["scope_valid"] = true
			delete(payload, "archived_at")
			delete(payload, "archive_reason")
			payload["lifecycle_last_used_at"] = now
			payload["lifecycle_last_evaluated_at"] = now
			payload["updated_at"] = now
			if existingCount, ok := payload["lifecycle_access_count"].(int); ok {
				payload["lifecycle_access_count"] = existingCount + 1
			} else if existingCount, ok := payload["lifecycle_access_count"].(int64); ok {
				payload["lifecycle_access_count"] = int(existingCount) + 1
			} else if existingCount, ok := payload["lifecycle_access_count"].(float64); ok {
				payload["lifecycle_access_count"] = int(existingCount) + 1
			} else {
				payload["lifecycle_access_count"] = 1
			}
		}
		item.Data = payload
		if _, err := itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item); err != nil {
			return err
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func ltmLifecycleSignalMatchesPayload(payload map[string]any, signal ltmLifecycleSignal) bool {
	if payload == nil || len(payload) == 0 {
		return signal.ScopeType == "" || signal.ScopeType == "global"
	}
	if signal.ScopeType == "" || signal.ScopeType == "global" {
		return true
	}
	scopeType := strings.TrimSpace(fmt.Sprint(payload["scope_type"]))
	if scopeType == "" {
		scopeType = strings.TrimSpace(fmt.Sprint(payload["scopeType"]))
	}
	scopeID := strings.TrimSpace(fmt.Sprint(payload["scope_id"]))
	if scopeID == "" {
		scopeID = strings.TrimSpace(fmt.Sprint(payload["scopeID"]))
	}
	if scopeType == "" && scopeID == "" {
		scopeType = strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
		scopeID = strings.TrimSpace(fmt.Sprint(payload["repository_hint"]))
	}
	if scopeType == "" {
		return false
	}
	if signal.ScopeType != "" && !strings.EqualFold(scopeType, signal.ScopeType) {
		return false
	}
	if signal.ScopeID != "" && signal.ScopeID != "global" {
		if scopeID == "" {
			return false
		}
		return strings.EqualFold(scopeID, signal.ScopeID)
	}
	return true
}

func evaluateLTMLifecycleStatus(lastUsedDays, ageDays, accessCount, relevanceScore int, scopeValid bool) ltmLifecycleStatus {
	if lastUsedDays <= 14 && ageDays <= 30 && accessCount >= 2 && relevanceScore >= 10 {
		return ltmLifecycleStatus{State: "active", Reason: "recent_and_relevant", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 20}
	}
	if lastUsedDays <= 60 && accessCount >= 1 {
		return ltmLifecycleStatus{State: "warm", Reason: "recently_seen", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 45}
	}
	if !scopeValid {
		return ltmLifecycleStatus{State: "stale", Reason: "invalid_scope", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 100}
	}
	return ltmLifecycleStatus{State: "stale", Reason: "cold_or_unused", LastUsedDays: lastUsedDays, AgeDays: ageDays, Score: 80}
}

func decideLTMLifecycleRetention(payload map[string]any, now time.Time) ltmLifecycleRetentionDecision {
	if len(payload) == 0 || now.IsZero() {
		return ltmLifecycleRetentionDecision{}
	}
	status := strings.TrimSpace(fmt.Sprint(payload["lifecycle_status"]))
	if status == "" {
		status = strings.TrimSpace(fmt.Sprint(payload["status"]))
	}
	if status == "" || strings.EqualFold(status, "active") || strings.EqualFold(status, "warm") {
		return ltmLifecycleRetentionDecision{}
	}
	if strings.EqualFold(status, "archived") || strings.EqualFold(status, "deleted") {
		return ltmLifecycleRetentionDecision{}
	}

	lastUsedAt := int64(0)
	if v, ok := payload["lifecycle_last_used_at"].(int64); ok {
		lastUsedAt = v
	} else if v, ok := payload["lifecycle_last_used_at"].(int); ok {
		lastUsedAt = int64(v)
	} else if v, ok := payload["lifecycle_last_used_at"].(float64); ok {
		lastUsedAt = int64(v)
	}
	recordedAt := int64(0)
	if v, ok := payload["recorded_at"].(int64); ok {
		recordedAt = v
	} else if v, ok := payload["recorded_at"].(int); ok {
		recordedAt = int64(v)
	} else if v, ok := payload["recorded_at"].(float64); ok {
		recordedAt = int64(v)
	}
	if lastUsedAt == 0 {
		lastUsedAt = recordedAt
	}
	if recordedAt == 0 {
		recordedAt = lastUsedAt
	}
	accessCount := 1
	if v, ok := payload["lifecycle_access_count"].(int); ok {
		accessCount = v
	} else if v, ok := payload["lifecycle_access_count"].(int64); ok {
		accessCount = int(v)
	} else if v, ok := payload["lifecycle_access_count"].(float64); ok {
		accessCount = int(v)
	}
	scopeValid := true
	if v, ok := payload["scope_valid"].(bool); ok {
		scopeValid = v
	}

	lastUsedDays := 0
	if lastUsedAt > 0 && now.UnixMilli() > lastUsedAt {
		lastUsedDays = int((now.UnixMilli() - lastUsedAt) / int64(24*time.Hour/time.Millisecond))
	}
	ageDays := 0
	if recordedAt > 0 && now.UnixMilli() > recordedAt {
		ageDays = int((now.UnixMilli() - recordedAt) / int64(24*time.Hour/time.Millisecond))
	}

	if !scopeValid {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "invalid_scope", Confidence: 90}
	}
	if strings.EqualFold(status, "stale") && (lastUsedDays > 90 || ageDays > 180 || (accessCount <= 1 && ageDays > 90)) {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "cold_or_unused", Confidence: 80}
	}
	if lastUsedDays > 180 && accessCount <= 1 {
		return ltmLifecycleRetentionDecision{Action: "archive", Reason: "unused_for_long_time", Confidence: 75}
	}
	return ltmLifecycleRetentionDecision{}
}

func (a *CopilotAgent) applyLTMLifecycleRetention(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], now time.Time) (int, error) {
	if ctx == nil || kb == nil || a == nil || now.IsZero() {
		return 0, nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return 0, err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return 0, err
		}
		payload, ok := any(item.Data).(map[string]any)
		if !ok {
			payload = map[string]any{}
		}
		decision := decideLTMLifecycleRetention(payload, now)
		if decision.Action == "archive" {
			payload["lifecycle_status"] = "archived"
			payload["status"] = "archived"
			payload["lifecycle_last_evaluated_at"] = now.UnixMilli()
			payload["updated_at"] = now.UnixMilli()
			payload["archived_at"] = now.UnixMilli()
			payload["archive_reason"] = decision.Reason
			item.Data = payload
			if _, err := itemsTree.UpdateCurrentItem(ctx, currentKey.Key, item); err != nil {
				return 0, err
			}
			count++
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

func decideLTMLifecycleCompaction(payload map[string]any, now time.Time) bool {
	if payload == nil || len(payload) == 0 || now.IsZero() {
		return false
	}
	status := strings.TrimSpace(fmt.Sprint(payload["lifecycle_status"]))
	if status == "" {
		status = strings.TrimSpace(fmt.Sprint(payload["status"]))
	}
	if !strings.EqualFold(status, "archived") && !strings.EqualFold(status, "deleted") {
		return false
	}
	archivedAt := int64(0)
	if v, ok := payload["archived_at"].(int64); ok {
		archivedAt = v
	} else if v, ok := payload["archived_at"].(int); ok {
		archivedAt = int64(v)
	} else if v, ok := payload["archived_at"].(float64); ok {
		archivedAt = int64(v)
	}
	if archivedAt == 0 {
		return false
	}
	return now.UnixMilli()-archivedAt >= int64(7*24*time.Hour/time.Millisecond)
}

func (a *CopilotAgent) applyLTMLifecycleCompaction(ctx context.Context, kb *memory.KnowledgeBase[map[string]any], now time.Time) (int, error) {
	if ctx == nil || kb == nil || a == nil || now.IsZero() {
		return 0, nil
	}
	itemsTree, err := kb.Store.Items(ctx)
	if err != nil {
		return 0, err
	}
	var pruneKeys []memory.ItemKey
	ok, err := itemsTree.First(ctx)
	if err != nil {
		return 0, err
	}
	for ok {
		currentKey := itemsTree.GetCurrentKey()
		item, err := itemsTree.GetCurrentValue(ctx)
		if err != nil {
			return 0, err
		}
		payload, okPayload := any(item.Data).(map[string]any)
		if !okPayload {
			payload = map[string]any{}
		}
		if decideLTMLifecycleCompaction(payload, now) {
			pruneKeys = append(pruneKeys, memory.ItemKey{CategoryID: currentKey.Key.CategoryID, ItemID: currentKey.Key.ItemID})
		}
		ok, err = itemsTree.Next(ctx)
		if err != nil {
			return 0, err
		}
	}
	if len(pruneKeys) == 0 {
		return 0, nil
	}
	if err := kb.DeleteItems(ctx, pruneKeys); err != nil {
		return 0, err
	}
	return len(pruneKeys), nil
}

func detectLTMLifecycleSignal(query string) ltmLifecycleSignal {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ltmLifecycleSignal{}
	}
	for _, pattern := range []struct {
		prefix string
		action string
		scope  string
	}{
		{prefix: "done with repo ", action: "retire", scope: "repo"},
		{prefix: "done with space ", action: "retire", scope: "space"},
		{prefix: "finished with repo ", action: "retire", scope: "repo"},
		{prefix: "finished with space ", action: "retire", scope: "space"},
		{prefix: "continue with repo ", action: "reactivate", scope: "repo"},
		{prefix: "continue with space ", action: "reactivate", scope: "space"},
		{prefix: "resume repo ", action: "reactivate", scope: "repo"},
		{prefix: "resume space ", action: "reactivate", scope: "space"},
		{prefix: "archive this context", action: "archive", scope: "global"},
		{prefix: "forget this", action: "archive", scope: "global"},
	} {
		if strings.HasPrefix(q, pattern.prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(q, pattern.prefix))
			value = strings.Trim(value, " .,:;!?\"")
			if value == "" {
				value = "global"
			}
			return ltmLifecycleSignal{Action: pattern.action, ScopeType: pattern.scope, ScopeID: value, Reason: "explicit_user_cue", Confidence: 90}
		}
	}
	return ltmLifecycleSignal{}
}

func filterLTMRetrievalTargets(targets []ltmRetrievalTarget, signal ltmLifecycleSignal) []ltmRetrievalTarget {
	if signal.Action == "" {
		return targets
	}
	if signal.ScopeType == "" || signal.ScopeID == "" {
		return targets
	}
	filtered := make([]ltmRetrievalTarget, 0, len(targets))
	for _, target := range targets {
		if target.ScopeType == signal.ScopeType && target.ScopeID == signal.ScopeID {
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered
}

func buildLTMRetrievalTargets(query string, payload *ai.SessionPayload, repoHint string) []ltmRetrievalTarget {
	var targets []ltmRetrievalTarget
	query = strings.TrimSpace(query)
	if payload != nil && len(payload.SelectedKBs) > 0 {
		for _, kb := range payload.SelectedKBs {
			name := strings.TrimSpace(kb.Name)
			if name == "" {
				continue
			}
			if strings.EqualFold(string(kb.Type), string(ai.ArtifactTypeSpace)) || strings.EqualFold(string(kb.Type), "space") {
				targets = append(targets, ltmRetrievalTarget{Category: "research", Capability: "research_workflow", ScopeType: "space", ScopeID: name})
			}
		}
	}

	repoHint = strings.TrimSpace(repoHint)
	if repoHint != "" {
		targets = append(targets, ltmRetrievalTarget{Category: "coding", Capability: "coding_workflow", ScopeType: "repo", ScopeID: repoHint})
	}
	if len(targets) == 0 {
		if query != "" {
			targets = append(targets, ltmRetrievalTarget{Category: "general", Capability: "general", ScopeType: "global", ScopeID: "global"})
		}
	}
	return targets
}

// CapabilityOption customizes the capability registry for a CopilotAgent.
type CapabilityOption func(*CopilotAgent)

// WithCapability appends a capability to the agent's registry.
func WithCapability(cap capabilities.Capability) CapabilityOption {
	return func(a *CopilotAgent) {
		if a == nil || cap == nil {
			return
		}
		a.ensureCapabilityRegistry()
		a.capabilityRegistry.Register(cap)
	}
}

func (a *CopilotAgent) ensureCapabilityRegistry() {
	if a == nil || a.capabilityRegistry != nil {
		return
	}
	desktopCap := desktop.NewCapability()
	desktopCap.SetCommandExecutor(a.toolExecuteLocalCommand)    // For LLM tool calls
	desktopCap.SetRawCommandExecutor(a.rawLocalCommandExecutor) // For direct /c commands

	// Build capability list dynamically based on feature flags
	caps := []capabilities.Capability{desktopCap}

	// Stores feature disabled by default for v1 (AI Copilot focus)
	if StoresFeatureEnabled() {
		caps = append(caps, stores.NewCapability())
	}

	caps = append(caps, spaces.NewCapability())

	// Register the local coding workflow capability by default so repository-oriented
	// requests can still be routed through the established workflow entry point.
	cwfInfra := cw.NewDefaultSharedInfrastructure()
	// Inject STM accessor for phase succession (explicit parameter passing, not context)
	cwfInfra.STMAccessor = a.getSTMForPhaseCapabilities

	// Inject session queue accessor for work item enqueueing (Participant Pattern)
	cwfInfra.GetSessionQueue = a.getSessionQueueForCapabilities

	// Inject LTM accessor for team patterns (explicit parameter passing, not context)
	cwfInfra.LTMAccessor = func(ctx context.Context) (any, error) {
		return a.getOrCreateLTM(ctx)
	}

	// Inject database accessor for team patterns vectorization
	cwfInfra.DatabaseAccessor = func(ctx context.Context) (any, error) {
		if a.systemDB == nil {
			return nil, fmt.Errorf("systemDB not available")
		}
		return a.systemDB, nil
	}

	// Inject LTM name accessor for team patterns vectorization
	cwfInfra.LTMName = func() string {
		a.Memory.BindSession(context.Background())
		return a.Memory.LongTermMemoryName()
	}

	// Inject LLM accessor for team patterns vectorization
	cwfInfra.LLMAccessor = func(ctx context.Context) (any, error) {
		if a.brain == nil {
			return nil, fmt.Errorf("LLM (brain) not available")
		}
		return a.brain, nil
	}

	// Inject embedder accessor for team patterns vectorization
	cwfInfra.EmbedderAccessor = func(ctx context.Context) (any, error) {
		if a.service == nil || a.service.Domain() == nil {
			return nil, fmt.Errorf("service domain not available")
		}
		embedder := a.service.Domain().Embedder()
		if embedder == nil {
			return nil, fmt.Errorf("embedder not available")
		}
		return embedder, nil
	}

	// Enable adaptive classification and dynamic plan mutation (LLM orchestration)
	classificationHistory := plannerpkg.NewClassificationHistory()
	wrappedGenerator := plannerpkg.WrapGenerator(a.brain)
	cwfInfra.PhaseClassificationStrategy = plannerpkg.NewAdaptiveStrategy(
		wrappedGenerator, // LLM generator for classification escalation
		classificationHistory,
	)
	cwfInfra.ClassificationHistory = classificationHistory
	cwfInfra.PhasePlanMutator = plannerpkg.NewSTMPhasePlanMutator(a.getSTMForPhaseCapabilities)

	// Planner (universal - works across all domains)
	caps = append(caps, plannerpkg.NewPlannerCapability(cwfInfra))

	// Multi-Step Planner (decomposes multi-goal queries into sequential steps)
	// Note: No StepExecutor - phases execute directly (Participant Pattern)
	caps = append(caps, multistepplanner.NewMultiStepPlannerCapability(cwfInfra))

	// WorkStream Management (LLM orchestrates pause/resume/jump via tools)
	// Provides: list_workstreams, get_workstream_details, jump_to_phase,
	//           pause_workstream, resume_workstream, abandon_workstream
	// Note: Uses direct session STM adapter so pause/resume state mutations persist.
	caps = append(caps, capabilities.NewWorkStreamManagement(
		func() capabilities.STMReader {
			if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
				return nil
			}
			return &sessionSTMReaderAdapter{stm: a.service.session.Memory}
		},
		func() capabilities.PhasePlanMutator {
			if cwfInfra.PhasePlanMutator != nil {
				if m, ok := cwfInfra.PhasePlanMutator.(capabilities.PhasePlanMutator); ok {
					return m
				}
			}
			return nil
		},
	))

	// Note: No explicit InterjectionInterpreter needed
	// LLM interprets user interjections naturally and calls WorkStream management tools
	// The tool calls (jump_to_phase, pause_workstream, etc.) ARE the orchestration signals
	// This aligns with implicit orchestration model used in Stores/Spaces domains

	// Coding domain phases (full SDLC workflow)
	for _, phaseID := range []string{"intake", "investigation", "planning", "execution", "review", "delivery"} {
		caps = append(caps, plannerpkg.NewPhase("coding_workflow", phaseID, cwfInfra))
	}

	// Stores domain phases (methodical query workflow)
	// Investigation = Read-only (list, scan, describe)
	// Planning = Query design
	// Execution = Mutations (create, update, delete)
	// Review = Validation
	for _, phaseID := range []string{"investigation", "planning", "execution", "review"} {
		caps = append(caps, plannerpkg.NewPhase("stores", phaseID, cwfInfra))
	}

	// Spaces domain phases (knowledge management workflow)
	// Investigation = Read-only (read_config, list_items)
	// Execution = Mutations (mint, enrich)
	// Review = Validation
	for _, phaseID := range []string{"investigation", "execution", "review"} {
		caps = append(caps, plannerpkg.NewPhase("spaces", phaseID, cwfInfra))
	}

	// Desktop domain phases (automation workflow)
	// Planning = Script creation
	// Execution = Command execution
	for _, phaseID := range []string{"planning", "execution"} {
		caps = append(caps, plannerpkg.NewPhase("desktop", phaseID, cwfInfra))
	}

	a.capabilityRegistry = capabilities.NewRegistry(caps...)
}

type sessionSTMReaderAdapter struct {
	stm *ShortTermMemory
}

func (s *sessionSTMReaderAdapter) GetCurrentThread() capabilities.ThreadReader {
	if s == nil || s.stm == nil {
		return nil
	}
	thread := s.stm.GetCurrentThread()
	if thread == nil {
		return nil
	}
	return &sessionThreadReaderAdapter{thread: thread}
}

type sessionThreadReaderAdapter struct {
	thread *ConversationThread
}

func (t *sessionThreadReaderAdapter) GetStreamContext() capabilities.WorkStreamContextReader {
	if t == nil || t.thread == nil || t.thread.StreamContext == nil {
		return nil
	}
	return &sessionWorkStreamContextReaderAdapter{ctx: t.thread.StreamContext}
}

func (t *sessionThreadReaderAdapter) GetStreamState() capabilities.WorkStreamState {
	if t == nil || t.thread == nil {
		return capabilities.WorkStreamActive
	}
	return capabilities.WorkStreamState(t.thread.StreamState)
}

func (t *sessionThreadReaderAdapter) SetStreamState(state capabilities.WorkStreamState) {
	if t == nil || t.thread == nil {
		return
	}
	t.thread.StreamState = WorkStreamState(state)
}

func (t *sessionThreadReaderAdapter) PauseWorkStreamWithReason(reason string, requireManualResume bool) {
	if t == nil || t.thread == nil {
		return
	}
	t.thread.PauseWorkStreamWithReason(reason, requireManualResume)
}

func (t *sessionThreadReaderAdapter) ResumeWorkStream() {
	if t == nil || t.thread == nil {
		return
	}
	t.thread.ResumeWorkStream()
}

func (t *sessionThreadReaderAdapter) GetPauseReason() string {
	if t == nil || t.thread == nil || t.thread.StreamContext == nil {
		return ""
	}
	return t.thread.StreamContext.PauseReason
}

func (t *sessionThreadReaderAdapter) RequiresManualResume() bool {
	if t == nil || t.thread == nil || t.thread.StreamContext == nil {
		return false
	}
	return t.thread.StreamContext.RequiresManualResume
}

type sessionWorkStreamContextReaderAdapter struct {
	ctx *WorkStreamContext
}

func (w *sessionWorkStreamContextReaderAdapter) GetCurrentPhase() string {
	if w == nil || w.ctx == nil {
		return ""
	}
	return w.ctx.CurrentPhase
}

func (w *sessionWorkStreamContextReaderAdapter) GetPhaseHistory() []string {
	if w == nil || w.ctx == nil {
		return nil
	}
	return w.ctx.PhaseHistory
}

func (w *sessionWorkStreamContextReaderAdapter) GetPhasePlan() capabilities.PhasePlanReader {
	if w == nil || w.ctx == nil || w.ctx.PhasePlan == nil {
		return nil
	}
	return &sessionPhasePlanReaderAdapter{plan: w.ctx.PhasePlan}
}

func (w *sessionWorkStreamContextReaderAdapter) GetPhaseArtifacts() map[string]any {
	if w == nil || w.ctx == nil || w.ctx.PhaseArtifacts == nil {
		return nil
	}
	result := make(map[string]any, len(w.ctx.PhaseArtifacts))
	for phaseID, artifact := range w.ctx.PhaseArtifacts {
		result[phaseID] = artifact
	}
	return result
}

func (w *sessionWorkStreamContextReaderAdapter) GetIterationCount() int {
	if w == nil || w.ctx == nil {
		return 0
	}
	return w.ctx.IterationCount
}

func (w *sessionWorkStreamContextReaderAdapter) GetPhaseIterations() map[string]int {
	if w == nil || w.ctx == nil || w.ctx.PhaseIterations == nil {
		return nil
	}
	result := make(map[string]int, len(w.ctx.PhaseIterations))
	for phaseID, count := range w.ctx.PhaseIterations {
		result[phaseID] = count
	}
	return result
}

type sessionPhasePlanReaderAdapter struct {
	plan *PhasePlan
}

func (p *sessionPhasePlanReaderAdapter) GetPlannedPhases() []string {
	if p == nil || p.plan == nil {
		return nil
	}
	return p.plan.PlannedPhases
}

func (p *sessionPhasePlanReaderAdapter) GetCurrentIndex() int {
	if p == nil || p.plan == nil {
		return -1
	}
	return p.plan.CurrentIndex
}

func (p *sessionPhasePlanReaderAdapter) GetCompletedPhases() []string {
	if p == nil || p.plan == nil {
		return nil
	}
	return p.plan.CompletedPhases
}

func (p *sessionPhasePlanReaderAdapter) GetRemainingPhases() []string {
	if p == nil || p.plan == nil {
		return nil
	}
	return p.plan.RemainingPhases
}

// getSTMForPhaseCapabilities returns the agent's STM for phase succession access
func (a *CopilotAgent) getSTMForPhaseCapabilities() cw.STMReader {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return nil
	}
	return &stmAdapter{stm: a.service.session.Memory}
}

// getSessionQueueForCapabilities returns a WorkQueueAccessor for capabilities to enqueue work
func (a *CopilotAgent) getSessionQueueForCapabilities() cw.WorkQueueAccessor {
	if a == nil || a.service == nil || a.service.session == nil {
		return nil
	}
	queue := a.GetSessionQueue()
	if queue == nil {
		return nil
	}
	return &sessionWorkQueueAdapter{queue: queue}
}

// sessionWorkQueueAdapter adapts WorkQueue to WorkQueueAccessor interface
type sessionWorkQueueAdapter struct {
	queue *WorkQueue
}

func (w *sessionWorkQueueAdapter) Enqueue(item any) error {
	if w == nil || w.queue == nil {
		return fmt.Errorf("no queue available")
	}

	// Try direct type assertion first
	if workItem, ok := item.(WorkItem); ok {
		return w.queue.Enqueue(workItem)
	}

	// Handle struct literals by marshaling/unmarshaling through JSON
	// This allows capabilities to pass anonymous structs with the right fields
	itemJSON, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal work item: %w", err)
	}

	var workItem WorkItem
	if err := json.Unmarshal(itemJSON, &workItem); err != nil {
		return fmt.Errorf("failed to unmarshal work item: %w", err)
	}

	return w.queue.Enqueue(workItem)
}

// stmAdapter adapts ShortTermMemory to STMReader interface
type stmAdapter struct {
	stm *ShortTermMemory
}

func (s *stmAdapter) GetCurrentThread() cw.WorkStreamReader {
	if s == nil || s.stm == nil {
		return nil
	}
	thread := s.stm.GetCurrentThread()
	if thread == nil {
		return nil
	}
	return &workStreamAdapter{thread: thread}
}

func (s *stmAdapter) CreatePhasePlan(phases []string, checkpoints map[string]bool, reasoning, confidence string) error {
	if s == nil || s.stm == nil {
		return fmt.Errorf("no STM available")
	}

	thread := s.stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active thread")
	}

	// Initialize WorkStream if needed
	if thread.StreamContext == nil {
		thread.InitializeWorkStream(phases[0])
	}

	// Create PhasePlan
	plan := &PhasePlan{
		PlannedPhases:   phases,
		CurrentIndex:    0,
		CompletedPhases: []string{},
		RemainingPhases: phases,
		Checkpoints:     checkpoints,
		Reasoning:       reasoning,
		Confidence:      confidence,
		LastUpdated:     time.Now().UnixMilli(),
	}

	thread.StreamContext.PhasePlan = plan
	thread.StreamState = WorkStreamActive

	return nil
}

func (s *stmAdapter) StoreMultiStepPlan(planData any) error {
	if s == nil || s.stm == nil {
		return fmt.Errorf("no STM available")
	}

	thread := s.stm.GetCurrentThread()
	if thread == nil {
		return fmt.Errorf("no active thread")
	}

	if thread.StreamContext == nil {
		thread.InitializeWorkStream("")
	}

	// Marshal and unmarshal to convert to agent.MultiStepPlan type
	planJSON, err := json.Marshal(planData)
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	var multiStepPlan MultiStepPlan
	if err := json.Unmarshal(planJSON, &multiStepPlan); err != nil {
		return fmt.Errorf("failed to unmarshal plan: %w", err)
	}

	thread.StreamContext.MultiStepPlan = &multiStepPlan
	thread.StreamContext.CurrentStepIndex = 0

	// Set the first step's PhasePlan as the active plan
	if len(multiStepPlan.Steps) > 0 && multiStepPlan.Steps[0].PhasePlan != nil {
		thread.StreamContext.PhasePlan = multiStepPlan.Steps[0].PhasePlan
	}

	return nil
}

// workStreamAdapter adapts ConversationThread to WorkStreamReader
type workStreamAdapter struct {
	thread *ConversationThread
}

func (w *workStreamAdapter) GetStreamContext() cw.WorkStreamContextReader {
	if w == nil || w.thread == nil || w.thread.StreamContext == nil {
		return nil
	}
	return &workStreamContextAdapter{ctx: w.thread.StreamContext}
}

// workStreamContextAdapter adapts WorkStreamContext to WorkStreamContextReader
type workStreamContextAdapter struct {
	ctx *WorkStreamContext
}

func (w *workStreamContextAdapter) GetPhasePlan() cw.PhasePlanReader {
	if w == nil || w.ctx == nil || w.ctx.PhasePlan == nil {
		return nil
	}
	return &phasePlanAdapter{plan: w.ctx.PhasePlan}
}

func (w *workStreamContextAdapter) SetPhasePlan(plan cw.PhasePlanReader) {
	if w == nil || w.ctx == nil {
		return
	}
	// Note: This is a simplified setter - in practice, plan is already in ctx
	// The adapter pattern allows phase capabilities to work with the plan
}

func (w *workStreamContextAdapter) GetMultiStepPlan() cw.MultiStepPlanReader {
	if w == nil || w.ctx == nil {
		return nil
	}
	if w.ctx.MultiStepPlan == nil {
		return nil
	}
	return &multiStepPlanAdapter{plan: w.ctx.MultiStepPlan}
}

func (w *workStreamContextAdapter) SetMultiStepPlan(plan cw.MultiStepPlanReader) {
	if w == nil || w.ctx == nil {
		return
	}
	// Adapter already wraps the pointer, so the plan is already updated
}

func (w *workStreamContextAdapter) GetPhaseArtifacts() map[string]map[string]any {
	if w == nil || w.ctx == nil {
		return nil
	}
	return w.ctx.PhaseArtifacts
}

// multiStepPlanAdapter adapts MultiStepPlan to MultiStepPlanReader interface
type multiStepPlanAdapter struct {
	plan *MultiStepPlan
}

func (m *multiStepPlanAdapter) GetSteps() []cw.StepReader {
	if m == nil || m.plan == nil {
		return nil
	}
	steps := make([]cw.StepReader, len(m.plan.Steps))
	for i := range m.plan.Steps {
		steps[i] = &stepAdapter{step: &m.plan.Steps[i]}
	}
	return steps
}

func (m *multiStepPlanAdapter) GetCurrentStepIndex() int {
	if m == nil || m.plan == nil {
		return -1
	}
	return m.plan.CurrentStepIndex
}

func (m *multiStepPlanAdapter) SetCurrentStepIndex(idx int) {
	if m == nil || m.plan == nil {
		return
	}
	m.plan.CurrentStepIndex = idx
}

func (m *multiStepPlanAdapter) GetCompletedSteps() []string {
	if m == nil || m.plan == nil {
		return nil
	}
	return m.plan.CompletedSteps
}

func (m *multiStepPlanAdapter) SetCompletedSteps(steps []string) {
	if m == nil || m.plan == nil {
		return
	}
	m.plan.CompletedSteps = steps
}

func (m *multiStepPlanAdapter) GetTotalSteps() int {
	if m == nil || m.plan == nil {
		return 0
	}
	return m.plan.TotalSteps
}

// stepAdapter adapts StepDefinition to StepReader interface
type stepAdapter struct {
	step *StepDefinition
}

func (s *stepAdapter) GetStepID() string {
	if s == nil || s.step == nil {
		return ""
	}
	return s.step.StepID
}

func (s *stepAdapter) GetStepNumber() int {
	if s == nil || s.step == nil {
		return 0
	}
	return s.step.StepNumber
}

func (s *stepAdapter) GetGoal() string {
	if s == nil || s.step == nil {
		return ""
	}
	return s.step.Goal
}

func (s *stepAdapter) GetDomain() string {
	if s == nil || s.step == nil {
		return ""
	}
	return s.step.Domain
}

func (s *stepAdapter) GetStatus() string {
	if s == nil || s.step == nil {
		return ""
	}
	return s.step.Status
}

func (s *stepAdapter) SetStatus(status string) {
	if s == nil || s.step == nil {
		return
	}
	s.step.Status = status
}

func (s *stepAdapter) GetArtifacts() map[string]any {
	if s == nil || s.step == nil {
		return nil
	}
	return s.step.Artifacts
}

func (s *stepAdapter) SetArtifacts(artifacts map[string]any) {
	if s == nil || s.step == nil {
		return
	}
	s.step.Artifacts = artifacts
}

func (s *stepAdapter) SetCompletedAt(t int64) {
	if s == nil || s.step == nil {
		return
	}
	s.step.CompletedAt = t
}

func (s *stepAdapter) GetPhasePlan() cw.PhasePlanReader {
	if s == nil || s.step == nil || s.step.PhasePlan == nil {
		return nil
	}
	return &phasePlanAdapter{plan: s.step.PhasePlan}
}

// phasePlanAdapter adapts PhasePlan to PhasePlanReader interface
type phasePlanAdapter struct {
	plan *PhasePlan
}

func (p *phasePlanAdapter) GetPlannedPhases() []string {
	if p == nil || p.plan == nil {
		return nil
	}
	return p.plan.PlannedPhases
}

func (p *phasePlanAdapter) GetCurrentIndex() int {
	if p == nil || p.plan == nil {
		return -1
	}
	return p.plan.CurrentIndex
}

func (p *phasePlanAdapter) SetCurrentIndex(idx int) {
	if p == nil || p.plan == nil {
		return
	}
	p.plan.CurrentIndex = idx
}

func (p *phasePlanAdapter) GetCheckpoints() map[string]bool {
	if p == nil || p.plan == nil {
		return nil
	}
	return p.plan.Checkpoints
}

func (p *phasePlanAdapter) AdvancePhase() (nextPhase string, hasNext bool) {
	if p == nil || p.plan == nil {
		return "", false
	}

	// Advance to next phase
	p.plan.CurrentIndex++

	// Check if there's a next phase
	if p.plan.CurrentIndex >= len(p.plan.PlannedPhases) {
		return "", false // No more phases
	}

	return p.plan.PlannedPhases[p.plan.CurrentIndex], true
}

// stmReaderAdapter adapts codingworkflow.STMReader to capabilities.STMReader
// This allows WorkStreamManagement capability to work with the existing STM infrastructure
type stmReaderAdapter struct {
	cwSTM cw.STMReader
}

func (s *stmReaderAdapter) GetCurrentThread() capabilities.ThreadReader {
	if s == nil || s.cwSTM == nil {
		return nil
	}
	thread := s.cwSTM.GetCurrentThread()
	if thread == nil {
		return nil
	}
	return &threadReaderAdapter{cwThread: thread}
}

// threadReaderAdapter adapts codingworkflow.WorkStreamReader to capabilities.ThreadReader
type threadReaderAdapter struct {
	cwThread cw.WorkStreamReader
}

func (t *threadReaderAdapter) GetStreamContext() capabilities.WorkStreamContextReader {
	if t == nil || t.cwThread == nil {
		return nil
	}
	ctx := t.cwThread.GetStreamContext()
	if ctx == nil {
		return nil
	}
	return &wsContextReaderAdapter{cwCtx: ctx}
}

func (t *threadReaderAdapter) GetStreamState() capabilities.WorkStreamState {
	// Note: codingworkflow.WorkStreamReader doesn't have GetStreamState
	// Return default active state
	return capabilities.WorkStreamActive
}

func (t *threadReaderAdapter) SetStreamState(state capabilities.WorkStreamState) {
	// Note: codingworkflow.WorkStreamReader doesn't have SetStreamState
	// This is a no-op for now
}

// wsContextReaderAdapter adapts codingworkflow.WorkStreamContextReader to capabilities.WorkStreamContextReader
type wsContextReaderAdapter struct {
	cwCtx cw.WorkStreamContextReader
}

func (w *wsContextReaderAdapter) GetCurrentPhase() string {
	// Note: codingworkflow.WorkStreamContextReader doesn't have GetCurrentPhase
	// Extract from PhasePlan if available
	plan := w.GetPhasePlan()
	if plan == nil {
		return ""
	}
	phases := plan.GetPlannedPhases()
	idx := plan.GetCurrentIndex()
	if idx >= 0 && idx < len(phases) {
		return phases[idx]
	}
	return ""
}

func (w *wsContextReaderAdapter) GetPhaseHistory() []string {
	// Note: codingworkflow.WorkStreamContextReader doesn't have GetPhaseHistory
	// Return completed phases from plan if available
	plan := w.GetPhasePlan()
	if plan == nil {
		return nil
	}
	phases := plan.GetPlannedPhases()
	idx := plan.GetCurrentIndex()
	if idx <= 0 {
		return nil
	}
	if idx > len(phases) {
		return phases
	}
	return phases[:idx]
}

func (w *wsContextReaderAdapter) GetPhasePlan() capabilities.PhasePlanReader {
	if w == nil || w.cwCtx == nil {
		return nil
	}
	plan := w.cwCtx.GetPhasePlan()
	if plan == nil {
		return nil
	}
	return &phasePlanReaderAdapter{cwPlan: plan}
}

func (w *wsContextReaderAdapter) GetPhaseArtifacts() map[string]any {
	if w == nil || w.cwCtx == nil {
		return nil
	}
	// Note: codingworkflow returns map[string]map[string]any
	// capabilities expects map[string]any
	// Flatten the structure
	cwArtifacts := w.cwCtx.GetPhaseArtifacts()
	if cwArtifacts == nil {
		return nil
	}
	result := make(map[string]any)
	for phaseID, artifact := range cwArtifacts {
		result[phaseID] = artifact
	}
	return result
}

func (w *wsContextReaderAdapter) GetIterationCount() int {
	// Note: codingworkflow.WorkStreamContextReader doesn't have GetIterationCount
	return 0
}

func (w *wsContextReaderAdapter) GetPhaseIterations() map[string]int {
	// Note: codingworkflow.WorkStreamContextReader doesn't have GetPhaseIterations
	return nil
}

// phasePlanReaderAdapter adapts codingworkflow.PhasePlanReader to capabilities.PhasePlanReader
type phasePlanReaderAdapter struct {
	cwPlan cw.PhasePlanReader
}

func (p *phasePlanReaderAdapter) GetPlannedPhases() []string {
	if p == nil || p.cwPlan == nil {
		return nil
	}
	return p.cwPlan.GetPlannedPhases()
}

func (p *phasePlanReaderAdapter) GetCurrentIndex() int {
	if p == nil || p.cwPlan == nil {
		return -1
	}
	return p.cwPlan.GetCurrentIndex()
}

func (p *phasePlanReaderAdapter) GetCompletedPhases() []string {
	// Extract from plan
	if p == nil || p.cwPlan == nil {
		return nil
	}
	phases := p.cwPlan.GetPlannedPhases()
	idx := p.cwPlan.GetCurrentIndex()
	if idx <= 0 {
		return nil
	}
	if idx > len(phases) {
		return phases
	}
	return phases[:idx]
}

func (p *phasePlanReaderAdapter) GetRemainingPhases() []string {
	// Extract from plan
	if p == nil || p.cwPlan == nil {
		return nil
	}
	phases := p.cwPlan.GetPlannedPhases()
	idx := p.cwPlan.GetCurrentIndex()
	if idx < 0 || idx >= len(phases) {
		return nil
	}
	return phases[idx:]
}

// InjectShellSessionManager injects a shell session manager into the desktop capability
func (a *CopilotAgent) InjectShellSessionManager(mgr desktop.ShellSessionManager) {
	if a == nil || a.capabilityRegistry == nil {
		return
	}
	// Find the desktop capability and inject the shell session manager
	for _, cap := range a.capabilityRegistry.ListAll() {
		if desktopCap, ok := cap.(*desktop.Capability); ok {
			desktopCap.SetShellSessionManager(mgr)
			return
		}
	}
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

// NewCopilotAgent creates a new instance of CopilotAgent.
func NewCopilotAgent(cfg Config, databases map[string]sop.DatabaseOptions, systemDB *database.Database, opts ...CapabilityOption) *CopilotAgent {
	// Initialize the "Brain" (Generator) from configuration
	var gen ai.Generator
	var err error

	if cfg.Generator.Type != "" {
		gen, err = generator.New(cfg.Generator.Type, cfg.Generator.Options)
		if err != nil {
			log.Error(fmt.Sprintf("Failed to initialize generator from config (Type: %s): %v", cfg.Generator.Type, err))
		}
	}

	if err != nil {
		log.Error("Failed to initialize AI generator", "error", err)
	}

	agent := &CopilotAgent{
		Config:          cfg,
		brain:           gen,
		registry:        NewRegistry(),
		databases:       databases,
		systemDB:        systemDB,
		Memory:          memory.NewMemoryUnit(ai.AgentIDOmni),
		compiledScripts: make(map[string]CachedScript),
	}
	agent.ensureCapabilityRegistry()

	// Gap 1: Initialize authorization manager (default to GateSystemAccess for local mode)
	agent.authzManager = capabilities.NewAuthorizationManager(capabilities.GateSystemAccess)

	// Gap 3: Initialize lifecycle manager
	agent.lifecycleManager = capabilities.NewLifecycleManager(agent.capabilityRegistry)

	// Gap 2: Initialize classification engine with chained classifiers
	agent.initializeClassificationEngine()

	if cfg.ProductionMode {
		agent.EnablePhase5(nil, nil, nil, nil)
	}

	// Wire default coding workflow SharedInfrastructure with MemorySink for LTM integration
	sharedInfra := cw.NewDefaultSharedInfrastructure()
	sharedInfra.MemorySink = func(mem cw.CodingMemory) {
		if agent != nil {
			agent.recordCodingMemory(mem)
		}
	}
	// Inject LTM accessor for team patterns
	sharedInfra.LTMAccessor = func(ctx context.Context) (any, error) {
		if agent != nil {
			return agent.getOrCreateLTM(ctx)
		}
		return nil, fmt.Errorf("agent not initialized")
	}
	// Inject database accessor for team patterns vectorization
	sharedInfra.DatabaseAccessor = func(ctx context.Context) (any, error) {
		if agent != nil && agent.systemDB != nil {
			return agent.systemDB, nil
		}
		return nil, fmt.Errorf("systemDB not available")
	}
	// Inject LTM name accessor
	sharedInfra.LTMName = func() string {
		if agent != nil {
			agent.Memory.BindSession(context.Background())
			return agent.Memory.LongTermMemoryName()
		}
		return ""
	}
	// Inject LLM accessor
	sharedInfra.LLMAccessor = func(ctx context.Context) (any, error) {
		if agent != nil && agent.brain != nil {
			return agent.brain, nil
		}
		return nil, fmt.Errorf("LLM (brain) not available")
	}
	// Inject embedder accessor
	sharedInfra.EmbedderAccessor = func(ctx context.Context) (any, error) {
		if agent != nil && agent.service != nil && agent.service.Domain() != nil {
			embedder := agent.service.Domain().Embedder()
			if embedder != nil {
				return embedder, nil
			}
		}
		return nil, fmt.Errorf("embedder not available")
	}
	// Phase capabilities now handle tool registration
	for _, opt := range opts {
		if opt != nil {
			opt(agent)
		}
	}
	// Tools are registered dynamically in Open() or Ask() to ensure context propagation
	// context.Background() is used here to ensure tools are available even if Open() is not called or context context is missing
	agent.registerTools(context.Background())

	return agent
}

// SetService sets the reference to the main service (for cache invalidation).
func (a *CopilotAgent) SetService(s *Service) {
	a.service = s
}

// initializeClassificationEngine sets up the chained classification engine (Gap 2)
func (a *CopilotAgent) initializeClassificationEngine() {
	// MRU classifier (Priority 1 - highest)
	// Simplified: Returns empty for now (full MRU integration comes later)
	mruClassifier := classification.NewMRUClassifier(func(ctx context.Context) (string, string, bool) {
		// TODO: Integrate with actual MRU tracking in agent Memory
		return "", "", false
	})

	// Keyword classifier (Priority 2)
	keywordClassifier := classification.NewKeywordClassifier()

	// LLM classifier (Priority 3 - lowest, fallback)
	// For now, we use a no-op LLM classifier (LLM integration comes later)
	llmClassifier := classification.NewLLMClassifier(nil)

	// Create chained classifier
	a.classificationEngine = classification.NewChainedClassifier(
		mruClassifier,
		keywordClassifier,
		llmClassifier,
	)

	// Register approval handlers (Gap 1)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalFileWrite, a.handleFileWriteApproval)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalShellExec, a.handleShellExecApproval)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalDBMutation, a.handleDBMutationApproval)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalSecureInput, a.handleSecureInputApproval)
}

// handleFileWriteApproval prompts user for file write approval (Gap 1)
// Future: Integrate with chat UI to prompt user with:
//
//	"⚠️ Approve file write to: {resource}?"
//	[Approve] [Deny]
func (a *CopilotAgent) handleFileWriteApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via UI)
	log.Info("File write approval requested", "type", opCtx.OperationType, "resource", opCtx.Resource)
	return true, nil
}

// handleShellExecApproval prompts user for shell execution approval (Gap 1)
// Future: Integrate with chat UI to prompt user with:
//
//	"⚠️ Approve shell command: {command}?"
//	[Approve] [Deny]
//
// If opCtx.SecureInputRequired=true, show privacy notice:
//
//	"🔒 This command requires secure input (password/token)"
//	"✓ Input will be sent DIRECTLY to shell (not to LLM)"
//	"✓ Your sensitive data will NOT be visible in chat history"
//	Enter value: [secure input field]
func (a *CopilotAgent) handleShellExecApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via UI)
	if opCtx.SecureInputRequired {
		log.Info("🔒 Secure input approval requested",
			"type", opCtx.OperationType,
			"prompt", opCtx.SecureInputPrompt,
			"privacy", "input_bypasses_llm")
	} else {
		log.Info("Shell execution approval requested", "type", opCtx.OperationType, "command", opCtx.Resource)
	}
	return true, nil
}

// handleDBMutationApproval prompts user for database mutation approval (Gap 1)
// Future: Integrate with chat UI to prompt user with:
//
//	"⚠️ Approve database mutation: {operation}?"
//	[Approve] [Deny]
func (a *CopilotAgent) handleDBMutationApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via UI)
	log.Info("Database mutation approval requested", "type", opCtx.OperationType, "resource", opCtx.Resource)
	return true, nil
}

// handleSecureInputApproval prompts user for secure data entry with privacy guarantee (Gap 1)
// This handler is invoked when a capability needs sensitive input (passwords, API keys, tokens)
//
// Privacy-First Data Flow:
//  1. User is prompted via chat UI with CLEAR privacy notice
//  2. User enters sensitive data in chat editbox
//  3. Frontend sends data DIRECTLY to tool via special secure channel
//  4. LLM NEVER sees the sensitive data (not in prompt, not in history)
//  5. Data is passed to shell/tool execution only
//
// Example UI Flow:
//
//	Agent: "🔒 Secure Input Required"
//	Agent: "Command needs: database password"
//	Agent: "✓ Your input will be sent DIRECTLY to the command"
//	Agent: "✓ The AI model will NOT see your password"
//	Agent: "✓ Not stored in chat history"
//	User: [types password in editbox with masked display]
//	System: [routes to shell, bypasses LLM context]
func (a *CopilotAgent) handleSecureInputApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via secure UI flow)
	log.Info("🔒 Secure input approval requested",
		"type", opCtx.OperationType,
		"prompt", opCtx.SecureInputPrompt,
		"privacy_guarantee", "input_bypasses_llm_context",
		"flow", "user_prompt_via_chat_ui")
	return true, nil
}

// shouldObfuscate determines if obfuscation should be applied for a given database.
func (a *CopilotAgent) shouldObfuscate(dbName string) bool {
	// Look up database options
	if opts, ok := a.databases[dbName]; ok {
		return opts.EnableObfuscation
	}
	// Fallback to mode if no DB options found (e.g. legacy or system)
	// But usually systemDB is also in databases map.
	return false
}

// SetVerbose enables or disables verbose output.
func (a *CopilotAgent) SetVerbose(v bool) {
	a.Config.Verbose = v
}

// Open initializes the agent's resources.
func (a *CopilotAgent) Open(ctx context.Context) error {
	p := ai.GetSessionPayload(ctx)

	// If CurrentDB is set, start a transaction
	if p != nil && p.CurrentDB != "" {
		dbName := p.CurrentDB

		log.Debug(fmt.Sprintf("CopilotAgent.Open: Checking DB '%s', SystemDB available: %v", dbName, a.systemDB != nil))

		// Check for system DB
		if dbName == SystemDBName && a.systemDB != nil {
			if p.Transaction == nil {
				tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
				if err != nil {
					return fmt.Errorf("failed to begin transaction on system database: %w", err)
				}
				p.Transaction = tx
			}
		} else if dbOpts, ok := a.databases[dbName]; ok {
			db := database.NewDatabase(dbOpts)
			if p.Transaction == nil {
				tx, err := db.BeginTransaction(ctx, sop.ForWriting)
				if err != nil {
					return fmt.Errorf("failed to begin transaction on database '%s': %w", dbName, err)
				}
				p.Transaction = tx
			}
		} else {
			return fmt.Errorf("database '%s' not found in agent configuration", dbName)
		}
	}

	// Initialize strict physical cognitive buffers natively
	if err := a.InitializePhysicalMemory(ctx); err != nil {
		log.Warn("CopilotAgent: STM Initialization failure", "error", err)
	}

	// Register tools now that we have a context (and potentially transactions)
	a.registerTools(ctx)

	// Gap 3: Enable all registered capabilities
	if a.lifecycleManager != nil && a.capabilityRegistry != nil {
		for _, cap := range a.capabilityRegistry.ListAll() {
			if err := a.lifecycleManager.EnableCapability(ctx, cap.ID()); err != nil {
				log.Warn("Failed to enable capability", "capability", cap.ID(), "error", err)
			}
		}
	}

	return nil
}

// Close cleans up the agent's resources.
func (a *CopilotAgent) Close(ctx context.Context) error {
	// Gap 3: Shutdown all capabilities gracefully
	if a.lifecycleManager != nil {
		if err := a.lifecycleManager.ShutdownAll(ctx); err != nil {
			log.Warn("Error during capability shutdown", "error", err)
		}
	}
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		a.recordSessionBoundary(ctx, string(SessionBoundaryEnd), "Session ended", "agent closed")
	}

	// Transaction lifecycle is managed by Service.Close(), not here.
	// CopilotAgent has no agent-specific cleanup needed.
	return nil
}

// Search performs a search using the agent's capabilities.
func (a *CopilotAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return []ai.Hit[map[string]any]{}, nil
}

// Ask is the primary entry point for processing a user's conversational query to the AI Copilot.
//
// The functional flow follows a multi-layered ReAct architecture:
//
//  1. Generator Resolution:
//     Dynamically selects the appropriate LLM provider (e.g., Gemini, OpenAI, Claude) based on
//     the active session context or falling back to the system default configuration.
//
//  2. Direct Invocation Handling:
//     Checks if the query is a direct tool invocation (e.g., "/help" or "/clear_memory").
//     If so, it bypasses the LLM reasoning loop to immediately execute the deterministic command.
//
//  3. Intent Classification (Router):
//     Evaluates if the query should be routed to a specific sub-agent (Avatar/Persona).
//     If the intent indicates a specialized Avatar (e.g., a "Legal Auditor" persona), it delegates
//     execution entirely to that sub-agent.
//
//  4. Context Classification:
//     For generic queries (intent == "OMNI"), it performs a lightweight three-gate classification to
//     identify the semantic domain (e.g., "Spaces" or "Stores"), likely artifacts, and CRUD layer.
//     This stage is classification-only: it updates routing state, but does not assemble prompt slices
//     or mutate System_Tools context.
//
//  5. Episode Metadata Tracking (MRU Cache):
//     Analyzes the user's prior chat exchange inside the short-term episodic memory. If the user
//     remains engaged in the same topic and database context, it pulls the Most-Recently-Used (MRU)
//     semantic boundaries so the LLM retains coherent situational context across turns.
//
//  6. System Prompt Construction:
//     Assembles the multi-part context prompt (using SystemPromptBuilder) linking the Core Persona,
//     Active Playbooks/KBs, stable System Tools context, targeted focused execution/tool guidance
//     derived from the current classification, semantic memory boundaries, and conversation history.
//
//  7. Reasoning Engine Delegation:
//     Packages the assembled context and delegates execution to the ReAct engine. The engine loops
//     autonomously over the LLM generation and local tool executions (API-level tool calling) until
//     it produces a final answer.
//
//  8. Epilogue & Cleanup:
//     Records the completed dialogue and active track-state into the short-term memory transcript,
//     clears the volatile MRU buffer, and returns the final text response to the client.
//
// Ask is the main entry point for processing queries. For single-turn execution only.
// For multi-turn/workflow execution, use entry points that create queues internally.
// Ask is the public API that processes queries through a session queue.
// The queue enables workflow automation where capabilities can enqueue next steps.
// ReAct loop handles multi-turn execution and clarifications natively.
// Ask is the main entry point for Copilot queries and delegates to the
// canonical LLM-as-Orchestrator + Phase Factory pipeline.
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
	return "⚠️ **AI Copilot Disabled**: No valid API Key found.\n\nPlease go to **Environment Settings** (HDD icon in bottom left) -> **LLM API Key** to configure your Google Gemini or OpenAI key."
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
	a.registerTools(ctx)

	var tools []ai.ToolDefinition
	routingState := activeRoutingState(ctx)
	exposure := buildNativeToolExposure(routingState)
	tools = append(tools, a.listRegisteredTools(exposure)...)
	tools = append(tools, a.listStoredScriptTools(ctx)...)

	return tools, nil
}

func allowedNativeDomainTools(routingState *TaskContextClassification) (map[string]bool, map[string]bool) {
	allowedSpacesTools := make(map[string]bool)
	allowedStoresTools := make(map[string]bool)
	if routingState == nil {
		return allowedSpacesTools, allowedStoresTools
	}

	crudParams := collectCRUDFlags(routingState.Layers)
	cross := isCrossDomain(routingState.Layers)
	allow := func(target map[string]bool, names ...string) {
		for _, name := range names {
			target[name] = true
		}
	}

	if cross || strings.EqualFold(routingState.Domain, SpacesDomain) {
		if crudParams["R"] {
			allow(allowedSpacesTools, "read_space_config", "search_space")
		}
		if crudParams["C"] || crudParams["U"] {
			allow(allowedSpacesTools, "mint_to_space", "enrich_space", "update_space_config", "vectorize_space", "vectorize_space_categories", "vectorize_space_items")
		}
		if crudParams["D"] {
			allow(allowedSpacesTools, "delete_space")
		}
	}

	if cross || strings.EqualFold(routingState.Domain, StoresDomain) {
		allow(allowedStoresTools, "execute_script", "list_stores", "manage_transaction", "begin_tx", "commit_tx", "rollback_tx")
		if crudParams["R"] {
			allow(allowedStoresTools, "select", "join", "join_right", "explain_join", "open_store", "scan", "filter", "sort", "project", "limit")
		}
		if crudParams["C"] {
			allow(allowedStoresTools, "add")
		}
		if crudParams["U"] {
			allow(allowedStoresTools, "update")
		}
		if crudParams["D"] {
			allow(allowedStoresTools, "delete")
		}
	}

	return allowedSpacesTools, allowedStoresTools
}

// InitializePhysicalMemory creates strictly isolated B-Tree (STM) and Vector (LTM) structures for this Agent ID
func (a *CopilotAgent) InitializePhysicalMemory(ctx context.Context) error {
	if a.Memory == nil {
		return fmt.Errorf("MemoryUnit is nil")
	}
	if a.systemDB == nil {
		log.Warn("CopilotAgent: systemDB is nil, skipping physical STM/LTM initialization")
		return nil
	}
	if a.Memory.AgentID == "" {
		a.Memory.AgentID = ai.AgentIDOmni
	}

	tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for isolated STM: %w", err)
	}

	var embedder ai.Embeddings
	if a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil {
		embedder = a.service.Domain().Embedder()
	}

	// Initialize Memory Unit.
	_, err = a.Memory.OpenShortTermMemory(ctx, a.systemDB, tx)
	if err != nil {
		tx.Rollback(ctx)
		return fmt.Errorf("failed to open STM: %w", err)
	}

	// Rehydrate session state (work streams, conversation threads)
	if a.service != nil && a.service.session != nil {
		if a.service.session.Memory == nil {
			a.service.session.Memory = NewShortTermMemory()
		}

		// Try to load saved session from B-tree
		btreeSTM := a.Memory.STMStore().Primary()
		if btreeSTM != nil {
			if err := a.service.session.Memory.Rehydrate(ctx, btreeSTM); err != nil {
				log.Warn("Failed to rehydrate session state, starting fresh", "error", err)
			}

			// Show reminder about paused work streams
			reminder := a.service.session.Memory.FormatPausedWorkStreamsReminder()
			if reminder != "" {
				log.Info("Restored paused work streams", "reminder", reminder)
				// TODO: Show to user via system message in first response
			}
		}

		// Hook into periodic STM commits for automatic session state persistence
		stmStore := a.Memory.STMStore()
		if stmStore != nil {
			stmStore.SetSessionStatePersister(func(ctx context.Context, bt btree.BtreeInterface[string, any]) error {
				if a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
					return nil
				}
				return a.service.session.Memory.Persist(ctx, bt)
			})
		}
	}

	_, err = a.Memory.OpenLongTermMemory(ctx, a.systemDB, tx, a.brain, embedder)
	if err != nil {
		tx.Rollback(ctx)
		return fmt.Errorf("failed to open LTM: %w", err)
	}

	err = tx.Commit(ctx)
	a.Memory.CloseShortTermMemory()
	if err != nil {
		return fmt.Errorf("failed to commit isolated physical memory initialization: %w", err)
	}

	tx, err = a.systemDB.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return fmt.Errorf("failed 2nd begin transaction for isolated STM: %w", err)
	}

	log.Info("Initialized Isolated Physical Memory for Avatar", "agent_id", a.Memory.AgentID,
		"stm_store", a.Memory.ShortTermMemoryName(), "ltm_store", a.Memory.LongTermMemoryName())

	// Hook up SOP KB's system tools directly into the Avatar's semantic LTM synchronously
	if embedder != nil {
		alreadySeeded := false
		opts := &memory.SearchOptions[map[string]any]{Limit: 1, CategoryPath: "System_Tools"}
		dummyVec := make([]float32, embedder.Dim())
		ltm, err := a.Memory.OpenLongTermMemory(ctx, a.systemDB, tx, a.brain, embedder)
		if err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to open LTM: %w", err)
		}

		if hits, err := ltm.Store.Query(ctx, dummyVec, opts); err == nil && len(hits) > 0 {
			alreadySeeded = true
		}

		if !alreadySeeded {
			sopKB, err := a.systemDB.OpenKnowledgeBase(ctx, ai.DefaultKBName, tx, a.brain, embedder, false)
			if err != nil {
				tx.Rollback(ctx)
				return err
			}

			config, err := sopKB.GetConfig(ctx)
			if err != nil {
				tx.Rollback(ctx)
				return err
			}

			var toolThoughts []memory.Thought[map[string]any]
			seen := make(map[sop.UUID]bool)

			//opts := &memory.SearchOptions[map[string]any]{Limit: 2, CategoryPath: toolQueries[i].Category}
			result, err := sopKB.SearchByPath(ctx, config.ToolQueries)
			if err != nil {
				tx.Rollback(ctx)
				return err
			}
			for _, res := range result {
				if !seen[res.ID] {
					seen[res.ID] = true
					toolThoughts = append(toolThoughts, memory.Thought[map[string]any]{
						Summaries:    []string{"System Tool Semantic Interface"},
						CategoryPath: "System_Tools",
						Data:         res.Data,
					})
				}
			}

			if len(toolThoughts) > 0 {
				if err := ltm.IngestThoughts(ctx, toolThoughts, a.Memory.AgentID); err != nil {
					tx.Rollback(ctx)
					return err
				}
			}
		}
	}

	err = tx.Commit(ctx)
	a.Memory.CloseShortTermMemory()
	if err != nil {
		return err
	}

	// Wire up Cognitive Memory background workers for the Avatar
	// 1. Batch short-term flushing (Working Memory -> STM)
	a.Memory.StartMemoryWorkers(ctx, a.systemDB)

	// 2. Schedule Sleep Cycle consolidator (STM -> LTM every 30 min)
	hourlyInterval := a.Config.SleepCycleIntervalHours
	idleTimeout := a.Config.IdleSleepTimeoutMinutes

	a.StartSleepCycle(ctx, hourlyInterval, idleTimeout, nil)

	return nil
}

func (a *CopilotAgent) logThought(ctx context.Context, query string, toolsExecuted string, outcome string) {
	if a.systemDB == nil {
		return
	}

	if a.Memory == nil {
		return
	}
	a.Memory.BindSession(ctx)
	kbName := a.Memory.LongTermMemoryName()

	var embedder ai.Embeddings
	if a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil {
		embedder = a.service.Domain().Embedder()
	}
	if embedder == nil {
		return
	}

	status := "Success"
	thought := fmt.Sprintf("Intent: %s\nAST: %s\nStatus: %s\nOutcome: %s\n", query, toolsExecuted, status, outcome)

	go func() {
		embedCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		vecs, err := embed.DocumentTexts(embedCtx, embedder, []string{thought})
		if err != nil || len(vecs) == 0 {
			return
		}
		vec := vecs[0]

		tx, err := a.systemDB.BeginTransaction(embedCtx, sop.ForWriting)
		if err != nil {
			return
		}

		kb, err := a.systemDB.OpenKnowledgeBase(embedCtx, kbName, tx, a.brain, embedder, false, true)
		if err != nil {
			tx.Rollback(embedCtx)
			return
		}

		hash := sha256.Sum256([]byte(thought))
		itemID := fmt.Sprintf("%x", hash)

		payload := map[string]any{
			"id":      itemID,
			"intent":  query,
			"ast":     toolsExecuted,
			"status":  status,
			"outcome": outcome,
			"ts":      time.Now().UnixMilli(),
			"type":    "episode",
		}
		if err := kb.IngestThought(embedCtx, thought, "", "System", vec, payload); err != nil {
			tx.Rollback(embedCtx)
			return
		}
		tx.Commit(embedCtx)
		log.Debug("Active Memory: Episode logged successfully", "temp_id", itemID)
	}()
}

// Execute executes the requested tool against the session payload.
func (a *CopilotAgent) Execute(ctx context.Context, toolName string, args map[string]any) (string, error) {
	resolvePayloadTx := func(payload *ai.SessionPayload, targetDB string) sop.Transaction {
		if payload == nil {
			return nil
		}
		if targetDB != "" && payload.Transactions != nil {
			if tAny, ok := payload.Transactions[targetDB]; ok {
				if tx, ok := tAny.(sop.Transaction); ok {
					return tx
				}
			}
		}
		if payload.Transaction != nil {
			if tx, ok := payload.Transaction.(sop.Transaction); ok {
				return tx
			}
		}
		return nil
	}

	clearPayloadTx := func(payload *ai.SessionPayload, targetDB string, tx sop.Transaction) {
		if payload == nil || tx == nil {
			return
		}
		if targetDB != "" && payload.Transactions != nil {
			if current, ok := payload.Transactions[targetDB].(sop.Transaction); ok && current == tx {
				delete(payload.Transactions, targetDB)
			}
		}
		if current, ok := payload.Transaction.(sop.Transaction); ok && current == tx {
			payload.Transaction = nil
		}
		payload.Variables = nil
	}

	// Determine if we should deobfuscate
	dbName, _ := args["database"].(string)
	if dbName == "" {
		if p := ai.GetSessionPayload(ctx); p != nil {
			dbName = p.CurrentDB
		}
	}

	shouldDeobfuscate := a.shouldObfuscate(dbName)

	// De-obfuscate Args if enabled
	if shouldDeobfuscate {
		// Log before deobfuscation
		if b, err := json.Marshal(args); err == nil {
			log.Debug(fmt.Sprintf("Args before deobfuscation: %s", string(b)))
		}

		a.deobfuscateMap(args)

		// Log after deobfuscation
		if b, err := json.MarshalIndent(args, "", "  "); err == nil {
			log.Debug(fmt.Sprintf("Args after deobfuscation: %s", string(b)))
		}
	}

	// Save as Last Tool Call (for script drafting/refactoring)
	// We clone args to avoid mutation issues
	// BUT: If the tool is "save_last_step", we should NOT overwrite the last tool call yet!
	// We need to let it run using the *previous* last tool call.
	// So we defer the update of lastToolCall until AFTER execution, OR we skip it for meta-tools.

	isMetaTool := toolName == "save_last_step"

	savedArgs := deepCopyMap(args)

	if !isMetaTool {
		a.lastToolCall = &ai.ScriptStep{
			Type:    "command",
			Command: toolName,
			Args:    savedArgs,
		}
	}

	// Notify Recorder (Service) if available
	// This ensures that the Service knows about the tool execution for /last-tool and drafting
	if recorder, ok := ctx.Value(ai.CtxKeyScriptRecorder).(ai.ScriptRecorder); ok {
		// Debug: Check script content
		if script, ok := savedArgs["script"]; ok {
			log.Debug(fmt.Sprintf("Drafting script step. Type: %T, Value: %+v", script, script))
		}

		// We record it even if it's a meta-tool, because from the Service's perspective, it's an action.
		// However, for "save_last_step", the user might want to see the *previous* tool.
		// But strictly speaking, "last-tool" should show the LAST executed tool.
		recorder.RecordStep(ctx, ai.ScriptStep{
			Type:    "command",
			Command: toolName,
			Args:    savedArgs,
		})
	}

	// Get Session Payload
	p := ai.GetSessionPayload(ctx)
	if p == nil {
		return "", fmt.Errorf("no session payload found")
	}

	// Resolve Database
	// Priority:
	// 1. Explicit 'database' argument in tool call
	// 2. CurrentDB in SessionPayload
	var dbFound bool
	dbName, _ = args["database"].(string)
	if dbName != "" {
		if dbName == SystemDBName && a.systemDB != nil {
			dbFound = true
		} else if _, ok := a.databases[dbName]; ok {
			dbFound = true
		}
	} else {
		// No explicit database arg, use CurrentDB from session
		dbName = p.CurrentDB
		if dbName == SystemDBName && a.systemDB != nil {
			dbFound = true
		} else if _, ok := a.databases[dbName]; ok {
			dbFound = true
		}
	}

	// If explicit database is provided, we might need to update the context/payload for the tool execution
	// so that tools that rely on p.CurrentDB (like toolSelect) see the correct DB.
	if dbName != "" && dbName != p.CurrentDB {
		// Clone payload
		newPayload := *p
		newPayload.CurrentDB = dbName
		// If switching DB, we cannot use the existing transaction
		newPayload.Transaction = nil
		ctx = context.WithValue(ctx, SessionPayloadKey, &newPayload)
	}
	p = ai.GetSessionPayload(ctx)
	preExistingTx := resolvePayloadTx(p, dbName)

	if !dbFound && toolName != "list_databases" && toolName != "list_scripts" && toolName != "get_script_details" && toolName != "list_tools" && toolName != "handoff_to_avatar" && toolName != "execute_local_command" && toolName != "send_email" && toolName != "search_repo_files" && toolName != "grep_repo_text" && toolName != "run_repo_tests" && toolName != "read_file" && toolName != "write_file" && toolName != "patch_file" && toolName != "create_directory" && toolName != "rename_file" && toolName != "delete_file" && toolName != "delete_directory" && toolName != "copy_file" && toolName != "list_directory" && toolName != "path_exists" && toolName != "stat_path" && toolName != "git_status" && toolName != "git_diff" && toolName != "git_log" && toolName != "git_branch" && toolName != "git_checkout" && toolName != "git_commit" && toolName != "git_pull" && toolName != "git_push" {
		// Debugging
		var keys []string
		if a.systemDB != nil {
			keys = append(keys, SystemDBName)
		}
		for k := range a.databases {
			keys = append(keys, k)
		}
		return "", fmt.Errorf("database not found or not selected (Copilot). Requested: '%s', Available: %v", dbName, keys)
	}

	// This is the registry dispatch point for native LLM tool calls.
	// If the model asked for "search_space", the name resolves here to toolSearchKB.
	if toolDef, ok := a.registry.Get(toolName); ok {
		log.Info("LLM Tool Call", "tool", toolName)
		res, err := toolDef.Handler(ctx, args)

		// Step OnAfterToolResult hook
		if step, ok := ctx.Value(ctxKeyActiveStep).(cw.Step); ok && step != nil {
			if cap, ok := ctx.Value(ctxKeySelectedCapability).(capabilities.Capability); ok && cap != nil {
				routingState := capabilities.RoutingState{
					Domain:         "", // Would need to be passed through context if needed
					Gate:           "",
					CapabilityHint: "",
				}
				if updatedState, shouldContinue, stepErr := step.OnAfterToolResult(ctx, toolName, res, routingState); stepErr != nil {
					log.Warn("Step OnAfterToolResult failed", "error", stepErr)
				} else if !shouldContinue {
					log.Info("Step requested early termination", "tool", toolName)
					// Update context with new state
					_ = updatedState
				}
			}
		}

		if deferClose, _ := ctx.Value(ctxKeyDeferImplicitSessionTxClose).(bool); !deferClose {
			if pAfter := ai.GetSessionPayload(ctx); preExistingTx == nil && pAfter != nil && !pAfter.ExplicitTransaction {
				if tx := resolvePayloadTx(pAfter, dbName); tx != nil {
					if err != nil {
						tx.Rollback(ctx)
					} else if tx.HasBegun() {
						if commitErr := tx.Commit(ctx); commitErr != nil {
							clearPayloadTx(pAfter, dbName, tx)
							return "", fmt.Errorf("tool execution succeeded but transaction commit failed: %w", commitErr)
						}
					}
					clearPayloadTx(pAfter, dbName, tx)
				}
			}
		}
		if err != nil {
			return "", err
		}
		text, fmtErr := formatToolResult(ctx, res)
		if fmtErr != nil {
			return "", fmtErr
		}

		// Apply file-backed storage for large results (general-purpose feature)
		// Keep the model-facing tool result compact and resilient to missing backing files.
		return wrapToolResultWithStorage(ctx, toolName, text)
	}

	// Dump registry keys if tool not found (Debug)
	var keys []string
	for _, t := range a.registry.List() {
		keys = append(keys, t.Name)
	}
	log.Debug(fmt.Sprintf("Tool '%s' not found. Available tools: %v", toolName, keys))

	// Check if it's a script
	if a.systemDB != nil {
		// Try to load script
		// We need a transaction to read from system DB
		// But we might already be in a transaction on the user DB?
		// System DB is separate.
		tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
		if err == nil {
			defer tx.Rollback(ctx)
			store, err := a.systemDB.OpenModelStore(ctx, "scripts", tx)
			if err == nil {
				var script ai.Script
				if err := store.Load(ctx, ai.DefaultScriptCategory, toolName, &script); err == nil {
					// Found script! Execute it.
					return a.runScript(ctx, toolName, script, args)
				}
			}
		}
	}

	return "", fmt.Errorf("unknown tool: %s", toolName)
}

// wrapToolResultWithStorage applies file-backed storage for large tool results.
// This is a general-purpose feature that works with all reasoning engines.
func wrapToolResultWithStorage(ctx context.Context, toolName, result string) (string, error) {
	storage := getOrCreateToolResultStorage(ctx)
	if storage == nil {
		// No storage available, return result as-is
		return result, nil
	}

	unwrappedResult, existingHint, _, _ := unwrapToolResultEnvelopeFull(result)
	storageInput := unwrappedResult
	if strings.TrimSpace(storageInput) == "" {
		storageInput = result
	}

	// Check if result needs file backing
	summary, filePath, fileSize, err := storage.StoreResult(toolName, storageInput)
	if err != nil {
		log.Warn("Failed to store tool result", "error", err, "tool", toolName)
		return result, nil // Fallback to inline
	}

	if filePath == "" {
		// Result was small, not stored to file
		return result, nil
	}

	// Result was stored to file, wrap in envelope with metadata
	log.Info("Tool result stored to file", "tool", toolName, "file", filePath, "size", fileSize)

	envelope := ai.ToolResultEnvelope{
		ToolResult:   json.RawMessage(fmt.Sprintf("%q", summary)),
		ProgressHint: existingHint,
		FileMetadata: &ai.ToolFileMetadata{
			FilePath: filePath,
			FileSize: int64(fileSize),
		},
	}

	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		log.Warn("Failed to marshal tool result envelope", "error", err)
		return result, nil // Fallback
	}

	return string(envelopeBytes), nil
}

// scriptTransaction encapsulates transaction management for scripts
type scriptTransaction struct {
	tx           sop.Transaction
	commitFunc   func() error
	rollbackFunc func()
	dbName       string
}

// setupScriptScope initializes the scope with provided arguments
func setupScriptScope(args map[string]any) map[string]any {
	scope := make(map[string]any)
	for k, v := range args {
		scope[k] = v
	}
	return scope
}

// trackInitialTransactions captures the initial state of transactions
func trackInitialTransactions(ctx context.Context) map[string]sop.Transaction {
	initialTransactions := make(map[string]sop.Transaction)
	p := ai.GetSessionPayload(ctx)
	if p == nil {
		return initialTransactions
	}

	if p.Transactions != nil {
		for dbName, tAny := range p.Transactions {
			if existingTx, ok := tAny.(sop.Transaction); ok {
				initialTransactions[dbName] = existingTx
			}
		}
	}

	if p.Transaction != nil {
		if existingTx, ok := p.Transaction.(sop.Transaction); ok && p.CurrentDB != "" {
			if _, exists := initialTransactions[p.CurrentDB]; !exists {
				initialTransactions[p.CurrentDB] = existingTx
			}
		}
	}

	return initialTransactions
}

// setupImplicitTransaction creates an implicit transaction for non-single mode scripts
func (a *CopilotAgent) setupImplicitTransaction(ctx context.Context, script ai.Script) (*scriptTransaction, error) {
	if script.TransactionMode == "single" {
		return nil, nil
	}

	p := ai.GetSessionPayload(ctx)
	if p == nil {
		return nil, nil
	}

	dbName := script.Database
	if dbName == "" {
		dbName = p.CurrentDB
	}
	if dbName == "" {
		return nil, nil
	}

	// Check if transaction already exists
	var existing sop.Transaction
	if p.Transactions != nil {
		if tAny, ok := p.Transactions[dbName]; ok {
			existing, _ = tAny.(sop.Transaction)
		}
	}
	if existing == nil && p.Transaction != nil && (dbName == p.CurrentDB || dbName == "") {
		existing, _ = p.Transaction.(sop.Transaction)
	}

	if existing != nil {
		return nil, nil
	}

	// Create new transaction
	db, err := a.resolveDatabase(dbName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve database '%s' for implicit script transaction: %w", dbName, err)
	}

	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return nil, fmt.Errorf("failed to begin implicit script transaction: %w", err)
	}

	log.Info("setupImplicitTransaction: Created implicit transaction", "database", dbName, "tx_id", tx.GetID())

	if p.Transactions == nil {
		p.Transactions = make(map[string]any)
	}
	p.Transactions[dbName] = tx
	p.Transaction = tx

	return &scriptTransaction{
		tx:     tx,
		dbName: dbName,
		rollbackFunc: func() {
			log.Info("scriptTransaction: Rolling back implicit transaction", "database", dbName, "tx_id", tx.GetID())
			tx.Rollback(ctx)
			delete(p.Transactions, dbName)
			if current, ok := p.Transaction.(sop.Transaction); ok && current == tx {
				p.Transaction = nil
			}
			p.Variables = nil
		},
		commitFunc: func() error {
			log.Info("scriptTransaction: Committing implicit transaction", "database", dbName, "tx_id", tx.GetID())
			err := tx.Commit(ctx)
			delete(p.Transactions, dbName)
			if current, ok := p.Transaction.(sop.Transaction); ok && current == tx {
				p.Transaction = nil
			}
			p.Variables = nil
			return err
		},
	}, nil
}

// setupSingleTransaction creates a single global transaction for the script
func (a *CopilotAgent) setupSingleTransaction(ctx context.Context, script ai.Script) (context.Context, *scriptTransaction, error) {
	dbName := script.Database
	if dbName == "" {
		if p := ai.GetSessionPayload(ctx); p != nil {
			dbName = p.CurrentDB
		}
	}
	if dbName == "" {
		dbName = SystemDBName
	}

	db, err := a.resolveDatabase(dbName)
	if err != nil {
		return ctx, nil, fmt.Errorf("failed to resolve database '%s' for global transaction: %w", dbName, err)
	}

	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return ctx, nil, fmt.Errorf("failed to begin global transaction: %w", err)
	}

	log.Info("setupSingleTransaction: Created global transaction", "database", dbName, "tx_id", tx.GetID())

	// Inject into context
	if p := ai.GetSessionPayload(ctx); p != nil {
		newPayload := *p
		if newPayload.Transactions == nil {
			newPayload.Transactions = make(map[string]any)
		}
		newPayload.Transactions[dbName] = tx
		newPayload.Transaction = tx
		ctx = context.WithValue(ctx, SessionPayloadKey, &newPayload)
	}

	return ctx, &scriptTransaction{
		tx:     tx,
		dbName: dbName,
		rollbackFunc: func() {
			log.Info("scriptTransaction: Rolling back global transaction", "database", dbName, "tx_id", tx.GetID())
			tx.Rollback(ctx)
		},
		commitFunc: func() error {
			log.Info("scriptTransaction: Committing global transaction", "database", dbName, "tx_id", tx.GetID())
			err := tx.Commit(ctx)
			if err == nil {
				log.Info("scriptTransaction: Successfully committed global transaction", "database", dbName, "tx_id", tx.GetID())
			}
			return err
		},
	}, nil
}

// resolveStepArgs resolves template arguments for a step
func resolveStepArgs(step ai.ScriptStep, scope map[string]any) map[string]any {
	resolvedArgs := make(map[string]any)
	for k, v := range step.Args {
		if strVal, ok := v.(string); ok {
			resolvedArgs[k] = resolveTemplate(strVal, scope)
		} else {
			resolvedArgs[k] = v
		}
	}
	return resolvedArgs
}

// createStepContext creates a context with database override if needed
func createStepContext(ctx context.Context, step ai.ScriptStep) context.Context {
	if step.Database == "" {
		return ctx
	}

	p := ai.GetSessionPayload(ctx)
	if p == nil {
		return ctx
	}

	newPayload := *p
	newPayload.CurrentDB = step.Database
	if p.CurrentDB != step.Database {
		newPayload.Transaction = nil
	}
	return context.WithValue(ctx, SessionPayloadKey, &newPayload)
}

// executeScriptSteps executes all script steps and returns the output
func (a *CopilotAgent) executeScriptSteps(ctx context.Context, script ai.Script, scope map[string]any) (string, error) {
	var sb strings.Builder

	for i, step := range script.Steps {
		if step.Type != "command" {
			sb.WriteString(fmt.Sprintf("Skipping step %d (type '%s' not supported in tool execution)\n", i+1, step.Type))
			continue
		}

		resolvedArgs := resolveStepArgs(step, scope)
		stepCtx := createStepContext(ctx, step)

		res, err := a.Execute(stepCtx, step.Command, resolvedArgs)
		if err != nil {
			if !step.ContinueOnError || shouldShortCircuitScriptOnError(step.Command, resolvedArgs, err) {
				return "", fmt.Errorf("step %d (%s) failed: %w", i+1, step.Command, err)
			}
			sb.WriteString(fmt.Sprintf("Step %d failed: %v\n", i+1, err))
		} else {
			sb.WriteString(fmt.Sprintf("%s\n\n", res))
		}
	}

	return sb.String(), nil
}

// commitImplicitTransactions commits any implicit transactions created during script execution
func commitImplicitTransactions(ctx context.Context, initialTransactions map[string]sop.Transaction) error {
	p := ai.GetSessionPayload(ctx)
	if p == nil || p.ExplicitTransaction || p.Transactions == nil {
		return nil
	}

	for dbName, tAny := range p.Transactions {
		tx, ok := tAny.(sop.Transaction)
		if !ok || tx == nil {
			continue
		}

		// Skip if this transaction existed before the script
		if existingTx, exists := initialTransactions[dbName]; exists && existingTx == tx {
			continue
		}

		if tx.HasBegun() {
			log.Info("commitImplicitTransactions: Committing implicit transaction", "database", dbName, "tx_id", tx.GetID())
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("failed to commit implicit script transaction for '%s': %w", dbName, err)
			}
			log.Info("commitImplicitTransactions: Successfully committed", "database", dbName, "tx_id", tx.GetID())
		}

		delete(p.Transactions, dbName)
		if current, ok := p.Transaction.(sop.Transaction); ok && current == tx {
			p.Transaction = nil
		}
	}

	return nil
}

func (a *CopilotAgent) runScript(ctx context.Context, name string, script ai.Script, args map[string]any) (string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Running script '%s'...\n", name))
	ctx = context.WithValue(ctx, ctxKeyDeferImplicitSessionTxClose, true)

	// Initialize scope and track initial transactions
	scope := setupScriptScope(args)
	initialTransactions := trackInitialTransactions(ctx)

	// Setup transaction based on script mode
	var scriptTx *scriptTransaction
	var err error

	// TODO: Finalize transaction strategy for scripts. For now, implicit handling is what is in. Plus, LLM manages it explicitly.
	// Implicit only creates trans if missing to guard against LLM not managing transactions correctly.
	if script.TransactionMode == "single" {
		ctx, scriptTx, err = a.setupSingleTransaction(ctx, script)
		if err != nil {
			return "", err
		}
		sb.WriteString(fmt.Sprintf("Global Transaction Started (%s)\n", scriptTx.dbName))
	} else {
		scriptTx, err = a.setupImplicitTransaction(ctx, script)
		if err != nil {
			return "", err
		}
		if scriptTx != nil {
			sb.WriteString(fmt.Sprintf("Implicit Session Transaction Started (%s)\n", scriptTx.dbName))
		}
	}

	// Ensure rollback on failure
	defer func() {
		if scriptTx != nil && scriptTx.rollbackFunc != nil {
			scriptTx.rollbackFunc()
		}
	}()

	// Execute all script steps
	stepsOutput, err := a.executeScriptSteps(ctx, script, scope)
	if err != nil {
		return "", err
	}
	sb.WriteString(stepsOutput)

	// Commit transactions
	if scriptTx != nil && scriptTx.commitFunc != nil {
		if err := scriptTx.commitFunc(); err != nil {
			return "", fmt.Errorf("failed to commit global transaction: %w", err)
		}
		scriptTx.rollbackFunc = nil // Prevent defer from rolling back
	} else {
		if err := commitImplicitTransactions(ctx, initialTransactions); err != nil {
			return "", err
		}
	}

	return sb.String(), nil
}

func (a *CopilotAgent) runScriptRaw(ctx context.Context, script ai.Script, args map[string]any) (string, error) {
	// Scope for template resolution
	scope := make(map[string]any)
	for k, v := range args {
		scope[k] = v
	}

	var lastResult string

	for i, step := range script.Steps {
		if step.Type == "command" {
			// Resolve args
			resolvedArgs := make(map[string]any)
			for k, v := range step.Args {
				if strVal, ok := v.(string); ok {
					resolvedArgs[k] = resolveTemplate(strVal, scope)
				} else {
					resolvedArgs[k] = v
				}
			}

			// Handle Database Override
			stepCtx := ctx
			if step.Database != "" {
				if p := ai.GetSessionPayload(ctx); p != nil {
					// Clone payload to update CurrentDB for this step only
					newPayload := *p
					newPayload.CurrentDB = step.Database
					// Clear transaction if switching DB, as the existing transaction is bound to the old DB
					if p.CurrentDB != step.Database {
						newPayload.Transaction = nil
					}
					stepCtx = context.WithValue(ctx, SessionPayloadKey, &newPayload)
				}
			}

			res, err := a.Execute(stepCtx, step.Command, resolvedArgs)
			if err != nil {
				if !step.ContinueOnError || shouldShortCircuitScriptOnError(step.Command, resolvedArgs, err) {
					return "", fmt.Errorf("step %d (%s) failed: %w", i+1, step.Command, err)
				}
			}
			lastResult = res
		}
	}
	return lastResult, nil
}

func resolveTemplate(tmplStr string, scope map[string]any) string {
	if tmplStr == "" {
		return ""
	}
	if tmpl, err := template.New("tmpl").Parse(tmplStr); err == nil {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, scope); err == nil {
			return buf.String()
		}
	}
	return tmplStr
}

func (a *CopilotAgent) deobfuscateMap(m map[string]any) {
	// We need to handle key deobfuscation which usually requires removing the old key and adding the new one.
	// Since we can't safely modify keys during range, we collect changes first.
	type keyChange struct {
		oldKey string
		newKey string
		value  any
	}
	var changes []keyChange

	for k, v := range m {
		// 1. Deobfuscate Value (Recursive)
		newVal := a.deobfuscateValue(v)

		// 2. Deobfuscate Key
		newKey := obfuscation.GlobalObfuscator.DeobfuscateText(k)

		if newKey != k {
			changes = append(changes, keyChange{
				oldKey: k,
				newKey: newKey,
				value:  newVal,
			})
		} else {
			// If key didn't change, just update value in place
			m[k] = newVal
		}
	}

	// Apply Key Changes
	for _, c := range changes {
		delete(m, c.oldKey)
		m[c.newKey] = c.value
	}
}

func (a *CopilotAgent) deobfuscateValue(v any) any {
	switch val := v.(type) {
	case string:
		s := strings.Trim(val, "*_`")
		s = strings.ReplaceAll(s, "\u00a0", " ")
		s = strings.TrimSpace(s)
		return obfuscation.GlobalObfuscator.DeobfuscateText(s)
	case []any:
		for i, item := range val {
			val[i] = a.deobfuscateValue(item)
		}
		return val
	case map[string]any:
		a.deobfuscateMap(val)
		return val
	default:
		return v
	}
}

func deepCopyMap(m map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		result[k] = deepCopyValue(v)
	}
	return result
}

func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		newSlice := make([]any, len(val))
		for i, item := range val {
			newSlice[i] = deepCopyValue(item)
		}
		return newSlice
	default:
		return v
	}
}

func shouldRouteToOmniInsteadOfAvatar(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}

	lower := strings.ToLower(trimmed)
	if lower == "sop" || strings.HasPrefix(lower, "sop:") {
		return true
	}

	if strings.HasPrefix(lower, "omni:") {
		parts := strings.Split(trimmed, ":")
		if len(parts) >= 2 {
			kbName := strings.TrimSpace(strings.ToLower(parts[1]))
			return kbName == "sop"
		}
	}

	return false
}

func (a *CopilotAgent) classifyIntent(ctx context.Context, query string, gen ai.Generator) string {
	if flag.Lookup("test.v") != nil {
		// Bypass intent routing in unit tests
		log.Info("Ask: Request classified for OMNI")
		return ai.IntentOmni
	}

	log.Info("classifyIntent ", "query", query)

	if shouldRouteToOmniInsteadOfAvatar(query) {
		log.Info("Ask: Request classified for OMNI")
		return ai.IntentOmni
	}

	p := ai.GetSessionPayload(ctx)

	// 1. Prefix is the overarching router
	parts := strings.Split(query, ":")
	if len(parts) > 1 {
		prefix := strings.ToUpper(strings.TrimSpace(parts[0]))

		if prefix == ai.IntentOmni {
			log.Info("Ask: Request classified for OMNI")
			return ai.IntentOmni
		}

		if p != nil && len(p.SelectedKBs) > 0 {
			for _, kb := range p.SelectedKBs {
				// We compare case-insensitively just in case, but return the canonical KB name
				if prefix == strings.ToUpper(kb.Name) || prefix == kb.Name {
					return kb.Name
				}
			}
		}
	}

	// 2. Without prefix, fall back to explicit UI selected state
	if p != nil {
		if len(p.SelectedKBs) == 1 {
			// Exactly one KB selected -> route to that Avatar
			return p.SelectedKBs[0].Name
		}
		// len(p.SelectedKBs) > 1 -> Future-proofing: multiple KBs selected means we need Omni's cross-KB capabilities
		// len(p.SelectedKBs) == 0 -> No explicit scope, default to Omni
	}

	// 3. Final Default
	log.Info("Ask: Request classified for OMNI")
	return ai.IntentOmni
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
