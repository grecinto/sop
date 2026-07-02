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
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/agent/domains/desktop"
	"github.com/sharedcode/sop/ai/agent/domains/spaces"
	"github.com/sharedcode/sop/ai/agent/domains/stores"
	"github.com/sharedcode/sop/ai/agent/parser"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/embed"
	"github.com/sharedcode/sop/ai/generator"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/ai/obfuscation"
	"github.com/sharedcode/sop/jsondb"
)

const (
	MaxMRUSize            = 20
	MaxExchangesInHistory = 20
	LTMFlushWindow        = 5 * time.Minute
	LTMFlushMaxCUDItems   = 5000
	LTMFlushMaxReadItems  = 5000
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
	// Preserve source links verbatim so the final answer can remain clickable.
	sb.WriteString("**Link Preservation:** Preserve the exact markdown source links from the KB results verbatim in your answer. Do not replace them with plain relative file paths or bare filenames.\n")

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

	ltmFlushMu          sync.Mutex
	ltmFlushLastFlush   time.Time
	ltmFlushPendingCUD  int
	ltmFlushPendingRead int
	ltmFlushQueue       []ltmBufferedWrite
	ltmFlushWorkerOnce  sync.Once

	// StoreOpener allows mocking the store creation (e.g. for testing)
	StoreOpener func(ctx context.Context, dbOpts sop.DatabaseOptions, storeName string, tx sop.Transaction) (jsondb.StoreAccessor, error)

	// capabilityRegistry provides modular domain capabilities for Ask routing and context building.
	capabilityRegistry *capabilities.Registry
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

func shouldFlushLTMBufferedWrites(now time.Time, lastFlush time.Time, pendingCUDCount int, pendingReadCount int) bool {
	if pendingCUDCount <= 0 && pendingReadCount <= 0 {
		return false
	}
	if lastFlush.IsZero() {
		return false
	}
	if pendingCUDCount >= LTMFlushMaxCUDItems || pendingReadCount >= LTMFlushMaxReadItems {
		return true
	}
	return now.Sub(lastFlush) >= LTMFlushWindow
}

func (a *CopilotAgent) queueLTMBufferedWrite(kind string, fn func(context.Context) error) error {
	if a == nil || fn == nil {
		return nil
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "read" {
		kind = "cud"
	}

	a.ltmFlushMu.Lock()
	a.ltmFlushQueue = append(a.ltmFlushQueue, ltmBufferedWrite{kind: kind, fn: fn})
	if kind == "read" {
		a.ltmFlushPendingRead++
	} else {
		a.ltmFlushPendingCUD++
	}
	now := time.Now()
	shouldFlush := shouldFlushLTMBufferedWrites(now, a.ltmFlushLastFlush, a.ltmFlushPendingCUD, a.ltmFlushPendingRead)
	batch := []ltmBufferedWrite(nil)
	if shouldFlush {
		batch = append(batch, a.ltmFlushQueue...)
		a.ltmFlushQueue = nil
		a.ltmFlushPendingCUD = 0
		a.ltmFlushPendingRead = 0
	}
	a.ltmFlushMu.Unlock()

	if shouldFlush {
		return a.flushLTMBufferedWrites(context.Background(), batch)
	}
	return nil
}

func (a *CopilotAgent) flushLTMBufferedWrites(ctx context.Context, batch []ltmBufferedWrite) error {
	if a == nil || len(batch) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, item := range batch {
		if item.fn == nil {
			continue
		}
		if err := item.fn(ctx); err != nil {
			return err
		}
	}
	a.ltmFlushMu.Lock()
	a.ltmFlushLastFlush = time.Now()
	a.ltmFlushMu.Unlock()
	return nil
}

func (a *CopilotAgent) flushLTMBufferedWritesIfDue(ctx context.Context) {
	if a == nil {
		return
	}
	a.ltmFlushMu.Lock()
	now := time.Now()
	if len(a.ltmFlushQueue) == 0 || !shouldFlushLTMBufferedWrites(now, a.ltmFlushLastFlush, a.ltmFlushPendingCUD, a.ltmFlushPendingRead) {
		a.ltmFlushMu.Unlock()
		return
	}
	batch := append([]ltmBufferedWrite(nil), a.ltmFlushQueue...)
	a.ltmFlushQueue = nil
	a.ltmFlushPendingCUD = 0
	a.ltmFlushPendingRead = 0
	a.ltmFlushMu.Unlock()
	if err := a.flushLTMBufferedWrites(ctx, batch); err != nil {
		log.Warn("CopilotAgent: buffered LTM flush failed", "error", err)
	}
}

func (a *CopilotAgent) startLTMFlushWorker(ctx context.Context) {
	if a == nil || ctx == nil {
		return
	}
	a.ltmFlushWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					a.flushLTMBufferedWritesIfDue(ctx)
				}
			}
		}()
	})
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

	if a.systemDB != nil && a.service != nil {
		if err := a.persistCodingMemoryToLTM(mem); err != nil {
			log.Debug("CopilotAgent: failed persisting coding workflow memory to LTM", "error", err)
		}
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

func (a *CopilotAgent) persistCodingMemoryToLTM(mem cw.CodingMemory) error {
	if a == nil || a.service == nil || a.systemDB == nil {
		return nil
	}
	if a.Memory == nil {
		return nil
	}
	return a.queueLTMBufferedWrite("cud", func(ctx context.Context) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if a.service != nil {
			ctx = context.WithValue(ctx, "session_payload", &ai.SessionPayload{AgentID: a.Memory.AgentID})
		}

		payload := map[string]any{
			"capability":      "coding_workflow",
			"description":     strings.TrimSpace(mem.Description),
			"outcome":         strings.TrimSpace(mem.Outcome),
			"files_touched":   mem.FilesTouched,
			"test_status":     strings.TrimSpace(mem.TestStatus),
			"repository_hint": strings.TrimSpace(mem.RepositoryHint),
			"lesson":          strings.TrimSpace(mem.Lesson),
			"next_step":       strings.TrimSpace(mem.NextStep),
			"recorded_at":     time.Now().UnixMilli(),
		}

		tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)

		embedder := ai.Embeddings(nil)
		if a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil {
			embedder = a.service.Domain().Embedder()
		}

		ltm, err := a.systemDB.OpenKnowledgeBase(ctx, a.Memory.LongTermMemoryName(), tx, a.brain, embedder, false, true)
		if err != nil {
			return err
		}

		categoryPath := "Capabilities/Coding Workflow"
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

		if err := ltm.IngestThoughts(ctx, []memory.Thought[map[string]any]{{
			Summaries:    []string{text},
			CategoryPath: categoryPath,
			Data:         payload,
		}}, a.Memory.AgentID); err != nil {
			return err
		}

		categoryID, err := ltm.Manager.EnsureCategory(ctx, categoryPath)
		if err != nil {
			return err
		}
		categoryExists := false
		if cats, err := ltm.Store.Categories(ctx); err == nil {
			if ok, err := cats.Find(ctx, categoryID, false); err == nil && ok {
				categoryExists = true
			}
		}

		if !categoryExists {
			if err := a.systemDB.VectorizeCategories(ctx, a.Memory.LongTermMemoryName(), a.brain, embedder, 10, []sop.UUID{categoryID}); err != nil {
				return err
			}
		} else {
			if err := a.systemDB.VectorizeItems(ctx, a.Memory.LongTermMemoryName(), a.brain, embedder, 10, categoryID, nil); err != nil {
				return err
			}
		}

		return tx.Commit(ctx)
	})
}

// CapabilityOption customizes the capability registry for a CopilotAgent.
type ltmBufferedWrite struct {
	kind string
	fn   func(context.Context) error
}

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

// WithCodingWorkflowCapability registers the local coding workflow capability for runtimes
// that are expected to support repository editing and inspection locally.
func WithCodingWorkflowCapability(orchestrator *cw.Orchestrator) CapabilityOption {
	return func(a *CopilotAgent) {
		if a == nil {
			return
		}
		a.ensureCapabilityRegistry()
		if orchestrator != nil {
			if orchestrator.ChangedFileInspector == nil {
				orchestrator.ChangedFileInspector = &cw.GitRepoInspector{}
			}
			prevMemorySink := orchestrator.MemorySink
			orchestrator.MemorySink = func(mem cw.CodingMemory) {
				if prevMemorySink != nil {
					prevMemorySink(mem)
				}
				if a != nil {
					a.recordCodingMemory(mem)
				}
			}
			if inspector, ok := orchestrator.ChangedFileInspector.(*cw.GitRepoInspector); ok && inspector.Client == nil {
				inspector.Client = NewWorkflowGitToolAdapter(a)
			}
			if orchestrator.ContextManager == nil {
				orchestrator.ContextManager = &cw.DefaultContextManager{}
			}
			if orchestrator.RepoIndexer == nil {
				orchestrator.RepoIndexer = &cw.DefaultRepoIndexer{}
			}
			if orchestrator.PatchEngine == nil {
				orchestrator.PatchEngine = &cw.DefaultPatchEngine{}
			}
			if orchestrator.DiffPreviewer == nil {
				orchestrator.DiffPreviewer = &cw.DefaultDiffPreviewer{}
			}
			if orchestrator.RepoSearcher == nil {
				orchestrator.RepoSearcher = &cw.FileSearchAdapter{}
			}
			if orchestrator.Memory == nil {
				orchestrator.Memory = cw.NewInMemorySessionMemory()
			}
			if orchestrator.Policy == nil {
				orchestrator.Policy = &cw.DefaultSafetyPolicy{}
			}
			if orchestrator.ExpertiseRetriever == nil {
				orchestrator.ExpertiseRetriever = a
			}
		}
		a.capabilityRegistry.RegisterAtFront(cw.NewCapability(orchestrator))
	}
}

func (a *CopilotAgent) ensureCapabilityRegistry() {
	if a == nil || a.capabilityRegistry != nil {
		return
	}
	a.capabilityRegistry = capabilities.NewRegistry(desktop.NewCapability(), stores.NewCapability(), spaces.NewCapability())
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
	parts = append(parts, "before answering, generating code, or acting, first research the selected Space by using the prompt and prior Ask turns to generate focused nested categories, then read the most relevant items/thoughts in those branches rather than consuming the entire Space")
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

func buildSelectedSpaceResearchQueries(query, kbName string, state *ResearchWorkflowState) []string {
	base := []string{}
	query = strings.TrimSpace(query)
	kbName = strings.TrimSpace(kbName)
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
	tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
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

	return nil
}

// Close cleans up the agent's resources.
func (a *CopilotAgent) Close(ctx context.Context) error {
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
func (a *CopilotAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
	ctx = a.applyAskConfig(ctx, cfg)
	a.clearAskProgressMRU()

	// Extract ProviderDetails from ConfigMap
	var providerDetails *ProviderDetails
	if cfg != nil {
		if po, ok := cfg.Get("provider_details"); ok {
			if override, ok := po.(*ProviderDetails); ok {
				providerDetails = override
			}
		}
	}

	if handled, res, err := a.handlePendingUserConfirmation(ctx, query); handled {
		return res, err
	}

	// Check for direct tool invocation (slash commands) BEFORE checking for API key
	// This allows slash commands to work without an LLM provider configured
	if handled, res, err := a.handleDirectToolInvocation(ctx, query, nil); handled {
		return res, err
	}

	gen := a.resolveGeneratorWithOverride(ctx, providerDetails)
	if gen == nil {
		return "⚠️ **AI Copilot Disabled**: No valid API Key found.\n\nPlease go to **Environment Settings** (HDD icon in bottom left) -> **LLM API Key** to configure your Google Gemini or OpenAI key.", nil
	}

	sessionID := a.logAskStart(ctx, query, gen)
	log.Info("Copilot Ask start", "query", query, "session_id", sessionID)

	rawQuery := query
	if rewrittenQuery, rewritten := a.tryMetaTalkBasedRouting(ctx, query); rewritten {
		log.Info("Copilot Ask MetaTalkBasedRouting Rewrite", "original_query", query, "effective_query", rewrittenQuery)
		query = rewrittenQuery
	}

	// 3. Intent Classification (Router)
	intent := a.classifyIntent(ctx, query, gen)

	// Fast-path routing: If Avatar, execute Avatar Sub-Agent
	if intent != ai.IntentOmni {
		log.Info("Ask: Request classified for Avatar", "avatar", intent)
		return a.executeAvatarSubAgent(ctx, intent, query)
	}
	log.Info("Ask: Request classified for OMNI")

	// 4. Three-Gate Context Classification

	// TODO: process unpacking IsNewTopic from Options, and use that as added signal for routing in Gate 2 (MRU).

	log.Info("Copilot Ask routing evaluation start", "query", query)
	taskContext, err := a.evaluateRoutingGates(ctx, query, gen)
	if err != nil {
		log.Info("Copilot Ask routing evaluation failed", "query", query, "error", err)
		return "", err
	}
	if taskContext == nil {
		taskContext = &TaskContextClassification{}
	}
	log.Info("Copilot Ask Routing",
		"routing_gate", taskContext.RoutingGate,
		"task_context", summarizeTaskContextForLog(*taskContext),
		"query", query,
	)

	// 4a. KB Search Direct Display (Case 1: Few matches)
	if taskContext.DirectDisplay && taskContext.KBSearchResults != "" {
		log.Info("KB routing: Direct display path", "match_count", taskContext.KBMatchCount)
		// Track episode metadata for MRU
		a.trackEpisodeMetadata(ctx, intent)
		// Format and return KB results immediately without LLM delegation
		response := a.formatKBSearchResultsForDisplay(taskContext.KBSearchResults, taskContext.KBMatchCount)
		// Track ask outcome for continuity
		a.epilogueAndCleanup(ctx, rawQuery, intent, response, nil, nil, nil, nil)
		return response, nil
	}

	// 5. Episode Metadata Tracking (MRU Cache)
	a.trackEpisodeMetadata(ctx, intent)

	// 6. System Prompt Construction
	fullPrompt := a.buildSystemPrompt(ctx, query, *taskContext)

	// 6a. KB Search LLM Integration (Case 2: :llm instruction, Case 3: Too many matches)
	if !taskContext.DirectDisplay && taskContext.KBSearchResults != "" {
		log.Info("KB routing: LLM processing path", "match_count", taskContext.KBMatchCount, "has_instruction", taskContext.LLMInstruction != "")
		// Use clean query (without :llm meta-token) for LLM context
		cleanQuery := taskContext.CleanQuery
		if cleanQuery == "" {
			cleanQuery = query // fallback to original if clean query not set
		}
		// Inject KB results into the query for LLM processing
		query = a.buildKBEnrichedQuery(cleanQuery, taskContext.KBSearchResults, taskContext.LLMInstruction, taskContext.KBMatchCount)
	}

	// 7. Reasoning Engine Delegation
	finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState, err := a.delegateToReasoningEngine(ctx, query, gen, fullPrompt)
	if err != nil {
		return "", err
	}
	log.Info("Copilot Ask Complete",
		"session_id", sessionID,
		"response_chars", len(finalText),
	)

	// 8. Epilogue & Cleanup
	a.epilogueAndCleanup(ctx, rawQuery, intent, finalText, toolCalls, outcomeFacts, outcomeRecipes, carryoverState)

	return finalText, nil
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

func isExplicitNewTopic(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if isNewTopic, ok := ctx.Value(ai.CtxKeyIsNewTopic).(bool); ok {
		return isNewTopic
	}
	return false
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
	}
	log.Info("Copilot Ask Start", "generator", gen.Name(), "default_format", getRequestedOutputFormat(ctx), "session_id", sessionID,
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

	effective := fmt.Sprintf("Target ask: %s\nAssistant clarification question: %s\nUser clarification: %s\nAnswer the clarification using the target ask context, then continue the target ask if the clarification resolves it.", targetQuery, question, strings.TrimSpace(query))
	if p := ai.GetSessionPayload(ctx); p != nil {
		if p.ClarificationState == nil {
			p.ClarificationState = &ai.ClarificationState{}
		}
		p.ClarificationState.TargetQuery = targetQuery
		p.ClarificationState.AssistantQuestion = question
		if strings.TrimSpace(p.ClarificationState.OriginalUserQuery) == "" {
			p.ClarificationState.OriginalUserQuery = targetQuery
		}
		p.ClarificationState.UserClarification = strings.TrimSpace(query)
		p.ClarificationState.EffectiveResumeAsk = effective
		p.ClarificationState.Status = "resolved"
		p.CurrentUserQuery = effective
	}

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

	if directCommand, ok := parseDirectCommandInvocation(trimmed); ok {
		a.registerTools(ctx)
		res, err := a.toolExecuteLocalCommand(ctx, map[string]any{
			"command":   directCommand,
			"reasoning": "Direct local command execution requested by user.",
		})
		if err != nil {
			return true, "", err
		}
		return true, res, nil
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

func parseDirectCommandInvocation(query string) (string, bool) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", false
	}

	prefixes := []string{"cmd:", "cmd ", "shell:", "shell ", "exec:", "exec ", "run:", "run "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			command := strings.TrimSpace(trimmed[len(prefix):])
			return command, command != ""
		}
	}
	return "", false
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
	a.rehydrateMRUFromMemory(ctx)
	isTest := isRoutingTestGenerator(gen)
	anchor := parseRoutingAnchor(query)

	// Fast-path: Check for specialized KB routing patterns (e.g., "sop", "sop:category")
	// before falling through to the standard three-gate flow
	if looksLikeSpecializedRoutingQuery(query) && anchor == nil {
		if handledTaskCtx, handled, err := a.trySpecializedFocusedRouting(ctx, query, "Omni", "Spaces", ""); err != nil {
			return nil, err
		} else if handled {
			return handledTaskCtx, nil
		}
	}

	if taskClassification, err := a.tryPrefixBasedRouting(ctx, query, gen, isTest, anchor); err != nil {
		return nil, err
	} else if taskClassification != nil {
		if isGeneralTaskContext(taskClassification) {
			return a.buildGeneralTaskContext(ctx, query, taskClassification), nil
		}
		return taskClassification, nil
	}

	if !isExplicitNewTopic(ctx) {
		if taskClassification, err := a.tryAskContinuationBasedRouting(ctx, query, gen, isTest, anchor); err != nil {
			return nil, err
		} else if taskClassification != nil {
			if isGeneralTaskContext(taskClassification) {
				return a.buildGeneralTaskContext(ctx, query, taskClassification), nil
			}
			return taskClassification, nil
		}
	} else {
		log.Info("Explicit new-topic override: skipping continuation-based routing")
	}

	if taskClassification, err := a.tryColdStartBasedRouting(ctx, query, gen, isTest); err != nil {
		return nil, err
	} else if taskClassification != nil {
		if isGeneralTaskContext(taskClassification) {
			return a.buildGeneralTaskContext(ctx, query, taskClassification), nil
		}
		return taskClassification, nil
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

func (a *CopilotAgent) delegateToReasoningEngine(ctx context.Context, query string, gen ai.Generator, fullPrompt string) (string, []ai.ToolCall, []string, []ai.LearnedRecipe, *ai.CarryoverState, error) {
	currentDB := ""
	if p := ai.GetSessionPayload(ctx); p != nil {
		currentDB = p.CurrentDB
	}

	// Obfuscate Prompt if enabled
	if a.shouldObfuscate(currentDB) {
		fullPrompt = obfuscation.GlobalObfuscator.ObfuscateText(fullPrompt)
	}

	// Active Implementation: Native Tools (API-level tool calling)
	engine := &NativeReActEngine{
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
		return "", nil, nil, nil, nil, err
	}

	finalText := resp.FinalText
	if a.shouldObfuscate(currentDB) {
		finalText = obfuscation.GlobalObfuscator.DeobfuscateText(finalText)
	}

	return finalText, resp.ToolCalls, resp.OutcomeFacts, resp.OutcomeRecipes, resp.CarryoverState, nil
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

func (a *CopilotAgent) resolveGeneratorWithOverride(ctx context.Context, providerOverride *ProviderDetails) ai.Generator {
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
		"2. Disambiguation: If a user's request is ambiguous or lacks constraints, DO NOT guess or hallucinate parameters. Use your search tools to find relevant constraints first. If self-research fails, halt execution and explicitly consult the user for clarification. Ask one short direct clarification question that starts with a recognizable lead such as 'Do you want...', 'Which...', 'Is your goal...', or 'Before I proceed...'. Keep the clarification question specific to the unresolved choice.\n\n"

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

func (a *CopilotAgent) getLTMSemanticContext(ctx context.Context, query string) string {
	toolsDef := ""
	if a.systemDB != nil && a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil && a.Memory.AgentID != "" {
		if tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading); err == nil {
			a.Memory.BindSession(ctx)
			kbName := a.Memory.LongTermMemoryName()
			kb, err := a.systemDB.OpenKnowledgeBase(ctx, kbName, tx, nil, nil, false, true)
			if err == nil {
				hits, err := memory.DigestKnowledgeBase(ctx, kb, a.service.Domain().Embedder(), memory.KBDigestRequest{
					Queries:            []string{query},
					PerQueryLimit:      5,
					MaxResults:         5,
					MinScore:           0.6,
					UseClosestCategory: true,
				})
				if err == nil && len(hits) > 0 {
					knowledgeStr := ""
					for _, hit := range hits {
						knowledgeStr += fmt.Sprintf("- (Score: %.2f) %s\n", hit.Score, hit.Text)
					}
					toolsDef += "\nContext Section (Learned Knowledge):\n" + knowledgeStr
				}
			}
			tx.Rollback(ctx)
		}
	}
	return toolsDef
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
				if tx, err := tempDB.BeginTransaction(ctx, sop.ForReading); err == nil {
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
			if tx, err := domainDB.BeginTransaction(ctx, sop.ForReading); err == nil {
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

func (a *CopilotAgent) getCodingWorkflowContext() string {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return ""
	}
	caveats := a.service.session.Memory.GetRuntimeCaveats(3)
	if len(caveats) == 0 {
		return ""
	}
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
	if !wrote {
		return ""
	}
	return strings.TrimSpace(sb.String())
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
	// 2. Batch long-term persistence writes with window/count-based flushing
	a.startLTMFlushWorker(ctx)

	// 3. Schedule Sleep Cycle consolidator (STM -> LTM)
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
		return text, nil
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
		return ai.IntentOmni
	}

	log.Info("classifyIntent ", "query", query)

	if shouldRouteToOmniInsteadOfAvatar(query) {
		return ai.IntentOmni
	}

	p := ai.GetSessionPayload(ctx)

	// 1. Prefix is the overarching router
	parts := strings.Split(query, ":")
	if len(parts) > 1 {
		prefix := strings.ToUpper(strings.TrimSpace(parts[0]))

		if prefix == ai.IntentOmni {
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
	return ai.IntentOmni
}
