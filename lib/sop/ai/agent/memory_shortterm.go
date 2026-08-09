package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/btree"
)

// RunnerSession holds the state for the current agent execution session,
// including script drafting and transaction management.
// This represents the Short-Term / Working Memory of the Agent.
type RunnerSession struct {
	mu                    sync.Mutex
	Playback              bool // True if a script is currently being executed
	AutoSave              bool // If true, the draft is saved to DB after every step
	CurrentScript         *ai.Script
	CurrentScriptName     string // Name of the script being drafted
	CurrentScriptCategory string // Category for the script being drafted
	// TODO: Transaction has to be revisited, specially the interplay to SessionPayload.Transaction.
	Transaction sop.Transaction
	CurrentDB   string         // The database the transaction is bound to
	Variables   map[string]any // Session-scoped variables (e.g. cached stores)
	// Verbose is a temporary bridge for carried runtime state between asks when the caller does not
	// provide a hydrated SessionPayload. The durable preference should move through LTM -> MRU ->
	// SessionPayload instead of living here as a storage authority.
	Verbose  bool
	LastStep *ai.ScriptStep
	// LastInteractionSteps tracks the number of steps added/executed in the last user interaction.
	LastInteractionSteps int
	// LastInteractionToolCalls buffers the tool calls from the last interaction for refactoring.
	LastInteractionToolCalls []ai.ScriptStep

	// Contextual Working Memory for the Session
	MRU                              []MRUItem
	MRUMu                            sync.RWMutex
	ConversationHistory              string
	LastSelectedSpaceContextKey      string
	LastSelectedSpaceReinforcementAt int64

	// PendingRefinement holds the proposed changes for a script from /script refine
	PendingRefinement *RefinementProposal
	// PendingConfirmation holds a user confirmation gate for destructive actions.
	PendingConfirmation *PendingUserConfirmation

	// Memory holds the structured Short-Term Memory of the session.
	// It replaces the flat History slice with threaded topics.
	Memory *ShortTermMemory
}

func (rs *RunnerSession) IsVerbose() bool {
	return rs != nil && rs.Verbose
}

func (rs *RunnerSession) SetVerbose(enabled bool) {
	if rs == nil {
		return
	}
	rs.Verbose = enabled
}

func (rs *RunnerSession) SetCurrentDB(db string) {
	if rs == nil {
		return
	}
	rs.CurrentDB = db
}

func (rs *RunnerSession) GetCurrentDB() string {
	if rs == nil {
		return ""
	}
	return rs.CurrentDB
}

type PendingUserConfirmation struct {
	Kind         string
	SpaceName    string
	DatabaseName string
}

// ConversationRole enum
type ConversationRole string

const (
	RoleUser      ConversationRole = "user"
	RoleAssistant ConversationRole = "assistant"
	RoleSystem    ConversationRole = "system"
)

// Interaction represents a single message in the conversation.
type Interaction struct {
	Role      ConversationRole `json:"role"`
	Content   string           `json:"content"`
	Timestamp int64            `json:"timestamp"`
	Entity    string           `json:"entity,omitempty"`    // OMNI or Avatar name
	ActiveKB  string           `json:"active_kb,omitempty"` // The target KB evaluated in this sequence
}

// WorkStreamState represents the lifecycle state of a work stream
type WorkStreamState string

const (
	WorkStreamActive    WorkStreamState = "active"    // Currently executing
	WorkStreamPaused    WorkStreamState = "paused"    // Paused due to interjection
	WorkStreamCompleted WorkStreamState = "completed" // Successfully finished
	WorkStreamAbandoned WorkStreamState = "abandoned" // User canceled or error
)

// InterjectionType defines how a work stream was created from user input
type InterjectionType string

const (
	InterjectionNone         InterjectionType = "none"          // Initial query
	InterjectionPauseNew     InterjectionType = "pause_new"     // Pause current, start new
	InterjectionClearRestart InterjectionType = "clear_restart" // Clear all, start fresh
	InterjectionRefine       InterjectionType = "refine"        // Stay in current, jump phase
)

// PhasePlan defines the planned sequence of phases for a work stream
type PhasePlan struct {
	PlannedPhases   []string        `json:"planned_phases"`   // Full planned path: ["investigation", "planning", "execution", "review", "delivery"]
	CurrentIndex    int             `json:"current_index"`    // Which phase we're on (0-based)
	CompletedPhases []string        `json:"completed_phases"` // Phases already completed
	RemainingPhases []string        `json:"remaining_phases"` // Phases yet to execute
	Checkpoints     map[string]bool `json:"checkpoints"`      // Re-assess triggers: {"after_investigation": true}
	Reasoning       string          `json:"reasoning"`        // Why this path was chosen
	Confidence      string          `json:"confidence"`       // "high", "medium", "low"
	LastUpdated     int64           `json:"last_updated"`     // Timestamp of last path modification
}

// LoopDetectionState tracks iteration patterns to prevent infinite loops
type LoopDetectionState struct {
	MaxIterations   int      `json:"max_iterations"`    // e.g., 20 total phases (prevents runaway)
	MaxPhaseRepeats int      `json:"max_phase_repeats"` // e.g., 3 times in same phase (prevents stuck phases)
	PatternWindow   int      `json:"pattern_window"`    // e.g., detect loops in last 10 phases
	DetectedLoops   []string `json:"detected_loops"`    // Patterns detected (for logging/debugging)
	EscalationLevel int      `json:"escalation_level"`  // 0=none, 1=warning, 2=user prompt, 3=abort
}

// MultiStepPlan represents a workflow with multiple sequential steps.
// Each step has its own goal and Phase Plan.
type MultiStepPlan struct {
	Steps            []StepDefinition `json:"steps"`              // Ordered list of steps
	CurrentStepIndex int              `json:"current_step_index"` // Cursor: which step we're on (0-based)
	CompletedSteps   []string         `json:"completed_steps"`    // Step IDs completed
	RemainingSteps   []string         `json:"remaining_steps"`    // Step IDs yet to execute
	Reasoning        string           `json:"reasoning"`          // Why this decomposition
	TotalSteps       int              `json:"total_steps"`        // Number of steps
	CreatedAt        int64            `json:"created_at"`         // Timestamp
}

// StepDefinition represents a single step in a multi-step workflow.
type StepDefinition struct {
	StepID        string         `json:"step_id"`       // "step_1_create_store"
	StepNumber    int            `json:"step_number"`   // 1, 2, 3...
	Goal          string         `json:"goal"`          // "Create new store with products"
	Domain        string         `json:"domain"`        // "stores", "coding_workflow", "spaces", "desktop"
	PhasePlan     *PhasePlan     `json:"phase_plan"`    // Phase sequence for THIS step
	Status        string         `json:"status"`        // "pending", "active", "complete", "failed"
	Prerequisites []string       `json:"prerequisites"` // Step IDs that must complete first (for future parallel support)
	Artifacts     map[string]any `json:"artifacts"`     // Results from this step
	StartedAt     int64          `json:"started_at"`    // Timestamp
	CompletedAt   int64          `json:"completed_at"`  // Timestamp
}

// WorkStreamContext captures the phase progression and artifacts
type WorkStreamContext struct {
	CurrentPhase   string                    `json:"current_phase"`   // Current executing phase
	PhaseHistory   []string                  `json:"phase_history"`   // All phases executed: [intake, investigation, planning, ...]
	PhasePlan      *PhasePlan                `json:"phase_plan"`      // THE PLAN - persisted across pause/resume
	PhaseArtifacts map[string]map[string]any `json:"phase_artifacts"` // Artifacts from each phase
	LastToolCalls  []string                  `json:"last_tool_calls"` // Tools called in this stream

	// Loop Detection & Iteration Tracking
	IterationCount     int                 `json:"iteration_count"`      // Total phase executions (for loop detection)
	PhaseIterations    map[string]int      `json:"phase_iterations"`     // Per-phase counters: {"investigation": 3, "execution": 2}
	RecentPhasePattern []string            `json:"recent_phase_pattern"` // Last 10 phases for pattern detection
	LoopDetectionState *LoopDetectionState `json:"loop_detection"`       // Loop tracking configuration

	// Timestamps
	StartedAt   int64 `json:"started_at"`   // When stream created
	PausedAt    int64 `json:"paused_at"`    // When paused (0 if active)
	CompletedAt int64 `json:"completed_at"` // When completed (0 if not done)

	// Work Stream Relationships
	ParentStreamID   sop.UUID         `json:"parent_stream_id"`  // The stream that was paused to start this
	InterjectionType InterjectionType `json:"interjection_type"` // How this stream was created

	// Manual Resume Control
	RequiresManualResume bool   `json:"requires_manual_resume"` // If true, won't auto-resume on completion of child stream
	PauseReason          string `json:"pause_reason"`           // Why this stream was paused (for user reminder)

	// Multi-Step Workflow Support
	MultiStepPlan    *MultiStepPlan `json:"multi_step_plan,omitempty"`    // Multi-step plan (if applicable)
	CurrentStepIndex int            `json:"current_step_index,omitempty"` // Which step we're on (mirrors MultiStepPlan.CurrentStepIndex)

	// Ask completion metadata captured for the active workstream.
	// This mirrors the user-visible summary and file list so it can survive
	// pause/resume and be restored even if the final response text is discarded.
	CompletionSummary string   `json:"completion_summary,omitempty"`
	UpdatedFiles      []string `json:"updated_files,omitempty"`
}

// ConversationThread represents a single conversational thread.
// It starts with a root prompt and spins up a Q&A exchange, eventually leading to a conclusion.
// Now also supports work stream context for phase-based execution with succession paths.
type ConversationThread struct {
	ID         sop.UUID `json:"id"`
	RootPrompt string   `json:"root_prompt"` // The seed sentence that started this thread

	// Transcribed Context (Managed by LLM)
	Label        string `json:"label"`         // Short Topic Name (e.g. "Defining Client")
	Category     string `json:"category"`      // e.g. "Business Logic", "Clarification"
	ContextNotes string `json:"context_notes"` // Important notes/context for the LLM

	// The linear exchange of Q&A within this topic
	Exchanges []Interaction `json:"exchanges"`

	// Termination
	Conclusion string `json:"conclusion"` // Summary or Agreement
	Status     string `json:"status"`     // "active", "concluded"

	// Work Stream Support (Planning-First Architecture)
	StreamState   WorkStreamState    `json:"stream_state"`   // Lifecycle state
	StreamContext *WorkStreamContext `json:"stream_context"` // Phase progression and artifacts
}

// ShortTermMemory manages the history of conversation threads.
type RuntimeCaveat struct {
	ID             string
	Kind           string
	CategoryRoot   string
	Summary        string
	Details        string
	Source         string
	CreatedAt      int64
	LastInjectedAt int64
	Active         bool
}

type SessionBoundaryKind string

const (
	SessionBoundaryStart       SessionBoundaryKind = "start"
	SessionBoundaryTopicSwitch SessionBoundaryKind = "topic_switch"
	SessionBoundaryEnd         SessionBoundaryKind = "end"
)

type SessionBoundary struct {
	Kind      SessionBoundaryKind
	Summary   string
	Details   string
	Timestamp int64
	Source    string
}

type CodingWorkflowExecutionState struct {
	ActiveFile      string
	Description     string
	CurrentStep     string
	Plan            []string
	Symbols         []string
	DependencyFiles []string
	RelatedFiles    []string
	TestFiles       []string
	LastOutcome     string
	LastTestStatus  string
	UpdatedAt       int64
}

type ResearchWorkflowState struct {
	LastPrompt              string
	SuccessfulCategories    []string
	SuccessfulCategoryPaths []string
	SuccessfulItems         []string
	SuccessfulItemPaths     []string
	LastResearchSummary     string
	UpdatedAt               int64
}

type ShortTermMemory struct {
	Threads               map[sop.UUID]*ConversationThread
	Order                 []sop.UUID // Maintains the sequence of threads
	CurrentThreadID       sop.UUID   // The currently active thread
	RuntimeCaveats        []RuntimeCaveat
	SessionBoundaries     []SessionBoundary
	LastRoutingState      *TaskContextClassification
	LastMRUSnapshot       []MRUItem
	LastRecipeSnapshot    []RecipeItem
	LastCarryoverState    *ai.CarryoverState
	CodingWorkflowState   *CodingWorkflowExecutionState
	ResearchWorkflowState *ResearchWorkflowState
}

// NewShortTermMemory initializes the memory structure.
func NewShortTermMemory() *ShortTermMemory {
	return &ShortTermMemory{
		Threads: make(map[sop.UUID]*ConversationThread),
		Order:   make([]sop.UUID, 0),
	}
}

const MaxConversationThreads = 20

// AddThread adds a new thread to memory, enforcing the LRU limit.
func (stm *ShortTermMemory) AddThread(thread *ConversationThread) {
	if len(stm.Order) >= MaxConversationThreads {
		// Remove the pending/oldest thread (index 0)
		oldestID := stm.Order[0]
		stm.Order = stm.Order[1:]
		delete(stm.Threads, oldestID)
	}
	stm.Threads[thread.ID] = thread
	stm.Order = append(stm.Order, thread.ID)
	stm.CurrentThreadID = thread.ID
}

// PromoteThread moves the specified thread ID to the end of the order (most recent).
func (stm *ShortTermMemory) PromoteThread(id sop.UUID) {
	// Find index
	idx := -1
	for i, existingID := range stm.Order {
		if existingID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return
	}
	// Remove from current position
	stm.Order = append(stm.Order[:idx], stm.Order[idx+1:]...)
	// Append to end
	stm.Order = append(stm.Order, id)
	stm.CurrentThreadID = id
}

// GetCurrentThread returns the active conversation thread or nil.
func (stm *ShortTermMemory) GetCurrentThread() *ConversationThread {
	if len(stm.CurrentThreadID) == 0 {
		return nil
	}
	return stm.Threads[stm.CurrentThreadID]
}

// NewWorkStreamContext creates a new work stream context with loop detection
func NewWorkStreamContext(initialPhase string, parentStreamID sop.UUID, interjectionType InterjectionType) *WorkStreamContext {
	return &WorkStreamContext{
		CurrentPhase:       initialPhase,
		PhaseHistory:       []string{},
		PhaseArtifacts:     make(map[string]map[string]any),
		LastToolCalls:      []string{},
		IterationCount:     0,
		PhaseIterations:    make(map[string]int),
		RecentPhasePattern: []string{},
		LoopDetectionState: &LoopDetectionState{
			MaxIterations:   20, // Max 20 phases total
			MaxPhaseRepeats: 3,  // Max 3 times in same phase
			PatternWindow:   10, // Detect in last 10 phases
			DetectedLoops:   []string{},
			EscalationLevel: 0, // Start at no escalation
		},
		StartedAt:        time.Now().Unix(),
		PausedAt:         0,
		CompletedAt:      0,
		ParentStreamID:   parentStreamID,
		InterjectionType: interjectionType,
	}
}

// InitializeWorkStream sets up work stream support for a conversation thread
func (thread *ConversationThread) InitializeWorkStream(initialPhase string) {
	if thread.StreamContext == nil {
		thread.StreamState = WorkStreamActive
		thread.StreamContext = NewWorkStreamContext(initialPhase, sop.UUID{}, InterjectionNone)
	}
}

// RecordPhaseExecution updates work stream tracking when a phase executes
func (ctx *WorkStreamContext) RecordPhaseExecution(phaseName string) {
	if ctx == nil {
		return
	}

	// Update iteration tracking
	ctx.IterationCount++
	ctx.PhaseIterations[phaseName]++

	// Update phase history
	if len(ctx.PhaseHistory) == 0 || ctx.PhaseHistory[len(ctx.PhaseHistory)-1] != phaseName {
		ctx.PhaseHistory = append(ctx.PhaseHistory, phaseName)
	}

	// Update pattern tracking for loop detection
	ctx.RecentPhasePattern = append(ctx.RecentPhasePattern, phaseName)
	if len(ctx.RecentPhasePattern) > ctx.LoopDetectionState.PatternWindow {
		ctx.RecentPhasePattern = ctx.RecentPhasePattern[1:]
	}
}

// CheckForLoop detects if the work stream is stuck in a loop
func (ctx *WorkStreamContext) CheckForLoop() (bool, string) {
	if ctx == nil || ctx.LoopDetectionState == nil {
		return false, ""
	}

	// Check 1: Total iterations exceeded (CRITICAL - abort)
	if ctx.IterationCount >= ctx.LoopDetectionState.MaxIterations {
		ctx.LoopDetectionState.EscalationLevel = 3 // Abort
		msg := fmt.Sprintf("Exceeded max %d iterations", ctx.LoopDetectionState.MaxIterations)
		ctx.LoopDetectionState.DetectedLoops = append(ctx.LoopDetectionState.DetectedLoops, msg)
		return true, msg
	}

	// Check 2: Same phase repeated too many times (MEDIUM - user prompt)
	currentRepeats := ctx.PhaseIterations[ctx.CurrentPhase]
	if currentRepeats >= ctx.LoopDetectionState.MaxPhaseRepeats {
		ctx.LoopDetectionState.EscalationLevel = 2 // User prompt
		msg := fmt.Sprintf("Phase '%s' repeated %d times", ctx.CurrentPhase, currentRepeats)
		ctx.LoopDetectionState.DetectedLoops = append(ctx.LoopDetectionState.DetectedLoops, msg)
		return true, msg
	}

	// Check 3: Repeating pattern (e.g., A→B→C→A→B→C) (LOW - warning)
	if isRepeatingPattern(ctx.RecentPhasePattern) {
		if ctx.LoopDetectionState.EscalationLevel < 1 {
			ctx.LoopDetectionState.EscalationLevel = 1 // Warning
		}
		pattern := strings.Join(ctx.RecentPhasePattern, "→")
		msg := fmt.Sprintf("Detected repeating pattern: %s", pattern)
		ctx.LoopDetectionState.DetectedLoops = append(ctx.LoopDetectionState.DetectedLoops, msg)
		return true, msg
	}

	return false, ""
}

// isRepeatingPattern detects if a sequence has a repeating sub-pattern
func isRepeatingPattern(phases []string) bool {
	n := len(phases)
	if n < 4 {
		return false // Need at least 4 items to detect a 2-item pattern
	}

	// Try pattern lengths from 2 to n/2
	for patternLen := 2; patternLen <= n/2; patternLen++ {
		if n%patternLen != 0 {
			continue // Pattern must divide evenly
		}

		// Check if this pattern repeats
		pattern := phases[:patternLen]
		isRepeating := true
		for i := patternLen; i < n; i += patternLen {
			segment := phases[i : i+patternLen]
			if !slicesEqual(pattern, segment) {
				isRepeating = false
				break
			}
		}

		if isRepeating {
			return true
		}
	}

	return false
}

// PauseWorkStreamWithReason marks a work stream as paused and optionally requires manual resume
func (thread *ConversationThread) PauseWorkStreamWithReason(reason string, requireManualResume bool) {
	if thread == nil || thread.StreamContext == nil {
		return
	}

	thread.StreamState = WorkStreamPaused
	thread.StreamContext.PausedAt = time.Now().Unix()
	thread.StreamContext.PauseReason = reason
	thread.StreamContext.RequiresManualResume = requireManualResume
}

// CanAutoResume checks if a paused work stream can be automatically resumed
func (thread *ConversationThread) CanAutoResume() bool {
	if thread == nil || thread.StreamContext == nil {
		return false
	}

	// Cannot auto-resume if:
	// 1. Not in paused state
	// 2. Requires manual resume flag is set
	if thread.StreamState != WorkStreamPaused {
		return false
	}

	return !thread.StreamContext.RequiresManualResume
}

// IsWaitingForManualResume checks if a thread is paused and waiting for explicit user resume
func (thread *ConversationThread) IsWaitingForManualResume() bool {
	if thread == nil || thread.StreamContext == nil {
		return false
	}

	return thread.StreamState == WorkStreamPaused && thread.StreamContext.RequiresManualResume
}

// ResumeWorkStream marks a paused work stream as active again
func (thread *ConversationThread) ResumeWorkStream() {
	if thread == nil || thread.StreamContext == nil {
		return
	}

	if thread.StreamState == WorkStreamPaused {
		thread.StreamState = WorkStreamActive
		thread.StreamContext.PausedAt = 0
		thread.StreamContext.RequiresManualResume = false
		thread.StreamContext.PauseReason = ""
	}
}

// GetPausedWorkStreams returns all paused threads from short-term memory
func (stm *ShortTermMemory) GetPausedWorkStreams() []*ConversationThread {
	if stm == nil {
		return nil
	}

	paused := []*ConversationThread{}
	for _, thread := range stm.Threads {
		if thread.StreamState == WorkStreamPaused {
			paused = append(paused, thread)
		}
	}

	return paused
}

// GetManualResumeWorkStreams returns threads that are paused and require manual resume
func (stm *ShortTermMemory) GetManualResumeWorkStreams() []*ConversationThread {
	if stm == nil {
		return nil
	}

	manualResume := []*ConversationThread{}
	for _, thread := range stm.Threads {
		if thread.IsWaitingForManualResume() {
			manualResume = append(manualResume, thread)
		}
	}

	return manualResume
}

// FormatPausedWorkStreamsReminder creates a user-friendly reminder about paused work streams
func (stm *ShortTermMemory) FormatPausedWorkStreamsReminder() string {
	paused := stm.GetManualResumeWorkStreams()
	if len(paused) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "📌 **Paused Work Streams** (require manual resume):")

	for i, thread := range paused {
		label := thread.Label
		if label == "" {
			label = thread.RootPrompt
			if len(label) > 50 {
				label = label[:47] + "..."
			}
		}

		pausedDuration := ""
		if thread.StreamContext != nil && thread.StreamContext.PausedAt > 0 {
			duration := time.Now().Unix() - thread.StreamContext.PausedAt
			if duration < 60 {
				pausedDuration = fmt.Sprintf("%ds ago", duration)
			} else if duration < 3600 {
				pausedDuration = fmt.Sprintf("%dm ago", duration/60)
			} else if duration < 86400 {
				pausedDuration = fmt.Sprintf("%dh ago", duration/3600)
			} else {
				pausedDuration = fmt.Sprintf("%dd ago", duration/86400)
			}
		}

		reason := ""
		if thread.StreamContext != nil && thread.StreamContext.PauseReason != "" {
			reason = fmt.Sprintf(" - %s", thread.StreamContext.PauseReason)
		}

		currentPhase := ""
		if thread.StreamContext != nil && thread.StreamContext.CurrentPhase != "" {
			currentPhase = fmt.Sprintf(" (paused in %s phase)", thread.StreamContext.CurrentPhase)
		}

		lines = append(lines, fmt.Sprintf("   %d. **%s**%s%s %s",
			i+1, label, currentPhase, reason, pausedDuration))
	}

	lines = append(lines, "")
	lines = append(lines, "💡 Use 'resume [number]' to continue a paused task")

	return strings.Join(lines, "\n")
}

// slicesEqual checks if two string slices are equal
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// WorkStreamSessionTTL defines how long saved session states are kept
const WorkStreamSessionTTL = 7 * 24 * time.Hour

// Persist saves the entire ShortTermMemory state to B-tree
func (stm *ShortTermMemory) Persist(ctx context.Context, btree btree.BtreeInterface[string, any]) error {
	if stm == nil {
		return nil
	}

	// Serialize threads (handle nested structs)
	threadsJSON, err := json.Marshal(stm.Threads)
	if err != nil {
		return fmt.Errorf("failed to marshal threads: %w", err)
	}

	orderJSON, err := json.Marshal(stm.Order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	currentIDStr := ""
	if !stm.CurrentThreadID.IsNil() {
		currentIDStr = stm.CurrentThreadID.String()
	}

	payload := map[string]any{
		"id":           "stm_session_state",
		"version":      1,
		"threads_json": string(threadsJSON),
		"order_json":   string(orderJSON),
		"current_id":   currentIDStr,
		"updated_at":   time.Now().Unix(),
		"expires_at":   time.Now().Add(WorkStreamSessionTTL).Unix(),
	}

	// Optionally persist caveats and boundaries (they're session-specific)
	if len(stm.RuntimeCaveats) > 0 {
		if caveatsJSON, err := json.Marshal(stm.RuntimeCaveats); err == nil {
			payload["caveats_json"] = string(caveatsJSON)
		}
	}
	if len(stm.SessionBoundaries) > 0 {
		if boundariesJSON, err := json.Marshal(stm.SessionBoundaries); err == nil {
			payload["boundaries_json"] = string(boundariesJSON)
		}
	}

	_, err = btree.Add(ctx, "stm_session_state", payload)
	if err != nil {
		return fmt.Errorf("failed to persist session state: %w", err)
	}

	log.Debug("Persisted ShortTermMemory session state",
		"threads", len(stm.Threads),
		"current", currentIDStr)

	return nil
}

// Rehydrate restores ShortTermMemory state from B-tree
func (stm *ShortTermMemory) Rehydrate(ctx context.Context, btree btree.BtreeInterface[string, any]) error {
	if stm == nil {
		return nil
	}

	// Try to load saved state
	found, err := btree.Find(ctx, "stm_session_state", false)
	if err != nil || !found {
		// No saved state is OK (fresh session)
		return nil
	}

	valItem, err := btree.GetCurrentValue(ctx)
	if err != nil {
		return nil
	}
	payload, ok := valItem.(map[string]any)
	if !ok {
		// Invalid type, start fresh
		return nil
	}

	// Check expiration
	if expiresAt, ok := payload["expires_at"].(int64); ok {
		if time.Now().Unix() > expiresAt {
			log.Info("Session state expired, starting fresh")
			return nil
		}
	}

	// Deserialize threads
	if threadsJSON, ok := payload["threads_json"].(string); ok && threadsJSON != "" {
		threads := make(map[sop.UUID]*ConversationThread)
		if err := json.Unmarshal([]byte(threadsJSON), &threads); err != nil {
			log.Warn("Failed to unmarshal threads, starting fresh", "error", err)
			return nil // Graceful degradation
		}
		stm.Threads = threads
	}

	// Deserialize order
	if orderJSON, ok := payload["order_json"].(string); ok && orderJSON != "" {
		order := []sop.UUID{}
		if err := json.Unmarshal([]byte(orderJSON), &order); err != nil {
			log.Warn("Failed to unmarshal order, starting fresh", "error", err)
			return nil
		}
		stm.Order = order
	}

	// Restore current thread ID
	if currentIDStr, ok := payload["current_id"].(string); ok && currentIDStr != "" {
		if parsedID, err := sop.ParseUUID(currentIDStr); err == nil {
			stm.CurrentThreadID = parsedID
		}
	}

	// Restore caveats (optional)
	if caveatsJSON, ok := payload["caveats_json"].(string); ok && caveatsJSON != "" {
		caveats := []RuntimeCaveat{}
		if err := json.Unmarshal([]byte(caveatsJSON), &caveats); err == nil {
			stm.RuntimeCaveats = caveats
		}
	}

	// Restore boundaries (optional)
	if boundariesJSON, ok := payload["boundaries_json"].(string); ok && boundariesJSON != "" {
		boundaries := []SessionBoundary{}
		if err := json.Unmarshal([]byte(boundariesJSON), &boundaries); err == nil {
			stm.SessionBoundaries = boundaries
		}
	}

	log.Info("Rehydrated ShortTermMemory session state",
		"threads", len(stm.Threads),
		"current", stm.CurrentThreadID.String())

	return nil
}

// PruneExpiredSessions removes old session states (called during sleep cycle)
func PruneExpiredSessions(ctx context.Context, btree btree.BtreeInterface[string, any], now time.Time) (int, error) {
	if btree == nil {
		return 0, nil
	}

	found, err := btree.Find(ctx, "stm_session_state", false)
	if err != nil || !found {
		return 0, nil // No session state to prune
	}

	valItem, err := btree.GetCurrentValue(ctx)
	if err != nil {
		return 0, nil
	}
	payload, ok := valItem.(map[string]any)
	if !ok {
		return 0, nil
	}

	if expiresAt, ok := payload["expires_at"].(int64); ok {
		if now.Unix() > expiresAt {
			if _, err := btree.Remove(ctx, "stm_session_state"); err != nil {
				return 0, err
			}
			log.Info("Pruned expired session state")
			return 1, nil
		}
	}

	return 0, nil
}

func (stm *ShortTermMemory) AddCaveat(caveat string) {
	stm.AddRuntimeCaveat(caveat, "", "caveat", "runtime_guidance", "runtime")
}

func (stm *ShortTermMemory) AddRuntimeCaveat(summary, details, kind, categoryRoot, source string) {
	if stm == nil {
		return
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	if kind == "" {
		kind = "caveat"
	}
	if categoryRoot == "" {
		categoryRoot = "runtime_guidance"
	}
	if source == "" {
		source = "runtime"
	}

	normalized := strings.ToLower(strings.TrimSpace(summary))
	for i := range stm.RuntimeCaveats {
		if strings.EqualFold(strings.TrimSpace(stm.RuntimeCaveats[i].Summary), summary) && strings.EqualFold(strings.TrimSpace(stm.RuntimeCaveats[i].CategoryRoot), categoryRoot) {
			stm.RuntimeCaveats[i].Details = strings.TrimSpace(details)
			stm.RuntimeCaveats[i].Kind = kind
			stm.RuntimeCaveats[i].Source = source
			stm.RuntimeCaveats[i].Active = true
			stm.RuntimeCaveats[i].LastInjectedAt = time.Now().UnixMilli()
			return
		}
		if strings.ToLower(strings.TrimSpace(stm.RuntimeCaveats[i].Summary)) == normalized && strings.EqualFold(strings.TrimSpace(stm.RuntimeCaveats[i].CategoryRoot), categoryRoot) {
			stm.RuntimeCaveats[i].Details = strings.TrimSpace(details)
			stm.RuntimeCaveats[i].Kind = kind
			stm.RuntimeCaveats[i].Source = source
			stm.RuntimeCaveats[i].Active = true
			stm.RuntimeCaveats[i].LastInjectedAt = time.Now().UnixMilli()
			return
		}
	}

	stm.RuntimeCaveats = append(stm.RuntimeCaveats, RuntimeCaveat{
		ID:             sop.NewUUID().String(),
		Kind:           kind,
		CategoryRoot:   categoryRoot,
		Summary:        summary,
		Details:        strings.TrimSpace(details),
		Source:         source,
		CreatedAt:      time.Now().UnixMilli(),
		LastInjectedAt: time.Now().UnixMilli(),
		Active:         true,
	})
}

func (stm *ShortTermMemory) RecordSessionBoundary(kind SessionBoundaryKind, summary, details, source string) {
	if stm == nil {
		return
	}
	kind = normalizeSessionBoundaryKind(kind)
	if kind == "" {
		return
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = string(kind)
	}
	details = strings.TrimSpace(details)
	source = strings.TrimSpace(source)
	if source == "" {
		source = "agent"
	}
	if kind == SessionBoundaryStart {
		for _, existing := range stm.SessionBoundaries {
			if existing.Kind == kind {
				return
			}
		}
	}
	stm.SessionBoundaries = append(stm.SessionBoundaries, SessionBoundary{
		Kind:      kind,
		Summary:   summary,
		Details:   details,
		Timestamp: time.Now().UnixMilli(),
		Source:    source,
	})
}

func normalizeSessionBoundaryKind(kind SessionBoundaryKind) SessionBoundaryKind {
	switch SessionBoundaryKind(strings.ToLower(strings.TrimSpace(string(kind)))) {
	case SessionBoundaryStart:
		return SessionBoundaryStart
	case SessionBoundaryTopicSwitch:
		return SessionBoundaryTopicSwitch
	case SessionBoundaryEnd:
		return SessionBoundaryEnd
	default:
		return ""
	}
}

func (stm *ShortTermMemory) GetSessionBoundaries(limit int) []SessionBoundary {
	if stm == nil || len(stm.SessionBoundaries) == 0 {
		return nil
	}
	if limit <= 0 || limit >= len(stm.SessionBoundaries) {
		return append([]SessionBoundary(nil), stm.SessionBoundaries...)
	}
	start := len(stm.SessionBoundaries) - limit
	if start < 0 {
		start = 0
	}
	return append([]SessionBoundary(nil), stm.SessionBoundaries[start:]...)
}

func (stm *ShortTermMemory) GetRuntimeCaveats(limit int) []RuntimeCaveat {
	if stm == nil || len(stm.RuntimeCaveats) == 0 {
		return nil
	}
	active := make([]RuntimeCaveat, 0, len(stm.RuntimeCaveats))
	for _, caveat := range stm.RuntimeCaveats {
		if caveat.Active {
			active = append(active, caveat)
		}
	}
	if len(active) == 0 {
		return nil
	}
	if limit > 0 && len(active) > limit {
		active = active[len(active)-limit:]
	}
	return active
}

func (stm *ShortTermMemory) ClearRuntimeCaveats(categoryRoot string) int {
	if stm == nil {
		return 0
	}
	categoryRoot = strings.TrimSpace(strings.ToLower(categoryRoot))
	if categoryRoot == "" {
		count := len(stm.RuntimeCaveats)
		stm.RuntimeCaveats = nil
		return count
	}
	kept := stm.RuntimeCaveats[:0]
	cleared := 0
	for _, caveat := range stm.RuntimeCaveats {
		if strings.EqualFold(strings.TrimSpace(caveat.CategoryRoot), categoryRoot) {
			cleared++
			continue
		}
		kept = append(kept, caveat)
	}
	stm.RuntimeCaveats = kept
	return cleared
}

func (stm *ShortTermMemory) SetRoutingState(taskCtx *TaskContextClassification) {
	stm.LastRoutingState = cloneTaskContextClassification(taskCtx)
}

func (stm *ShortTermMemory) GetRoutingState() *TaskContextClassification {
	return cloneTaskContextClassification(stm.LastRoutingState)
}

func (stm *ShortTermMemory) SetMRUSnapshot(items []MRUItem) {
	if len(items) == 0 {
		stm.LastMRUSnapshot = nil
		return
	}
	stm.LastMRUSnapshot = append([]MRUItem(nil), items...)
}

func (stm *ShortTermMemory) GetMRUSnapshot() []MRUItem {
	if len(stm.LastMRUSnapshot) == 0 {
		return nil
	}
	return append([]MRUItem(nil), stm.LastMRUSnapshot...)
}

func (stm *ShortTermMemory) SetRecipeSnapshot(items []RecipeItem) {
	if len(items) == 0 {
		stm.LastRecipeSnapshot = nil
		return
	}
	cloned := make([]RecipeItem, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, cloneRecipeItem(item))
	}
	stm.LastRecipeSnapshot = cloned
}

func (stm *ShortTermMemory) GetRecipeSnapshot() []RecipeItem {
	if len(stm.LastRecipeSnapshot) == 0 {
		return nil
	}
	cloned := make([]RecipeItem, 0, len(stm.LastRecipeSnapshot))
	for _, item := range stm.LastRecipeSnapshot {
		cloned = append(cloned, cloneRecipeItem(item))
	}
	return cloned
}

func (stm *ShortTermMemory) SetCarryoverState(state *ai.CarryoverState) {
	stm.LastCarryoverState = cloneCarryoverState(state)
}

func (stm *ShortTermMemory) GetCarryoverState() *ai.CarryoverState {
	return cloneCarryoverState(stm.LastCarryoverState)
}

func (stm *ShortTermMemory) SetCodingWorkflowState(state *CodingWorkflowExecutionState) {
	if stm == nil {
		return
	}
	if state == nil {
		stm.CodingWorkflowState = nil
		return
	}
	cloned := &CodingWorkflowExecutionState{}
	*cloned = *state
	cloned.Plan = append([]string(nil), state.Plan...)
	cloned.Symbols = append([]string(nil), state.Symbols...)
	cloned.DependencyFiles = append([]string(nil), state.DependencyFiles...)
	cloned.RelatedFiles = append([]string(nil), state.RelatedFiles...)
	cloned.TestFiles = append([]string(nil), state.TestFiles...)
	stm.CodingWorkflowState = cloned
}

func (stm *ShortTermMemory) GetCodingWorkflowState() *CodingWorkflowExecutionState {
	if stm == nil || stm.CodingWorkflowState == nil {
		return nil
	}
	cloned := &CodingWorkflowExecutionState{}
	*cloned = *stm.CodingWorkflowState
	cloned.Plan = append([]string(nil), stm.CodingWorkflowState.Plan...)
	cloned.Symbols = append([]string(nil), stm.CodingWorkflowState.Symbols...)
	cloned.DependencyFiles = append([]string(nil), stm.CodingWorkflowState.DependencyFiles...)
	cloned.RelatedFiles = append([]string(nil), stm.CodingWorkflowState.RelatedFiles...)
	cloned.TestFiles = append([]string(nil), stm.CodingWorkflowState.TestFiles...)
	return cloned
}

func (stm *ShortTermMemory) SetResearchWorkflowState(state *ResearchWorkflowState) {
	if stm == nil {
		return
	}
	if state == nil {
		stm.ResearchWorkflowState = nil
		return
	}
	cloned := &ResearchWorkflowState{}
	*cloned = *state
	cloned.SuccessfulCategories = append([]string(nil), state.SuccessfulCategories...)
	cloned.SuccessfulCategoryPaths = append([]string(nil), state.SuccessfulCategoryPaths...)
	cloned.SuccessfulItems = append([]string(nil), state.SuccessfulItems...)
	cloned.SuccessfulItemPaths = append([]string(nil), state.SuccessfulItemPaths...)
	stm.ResearchWorkflowState = cloned
}

func (stm *ShortTermMemory) GetResearchWorkflowState() *ResearchWorkflowState {
	if stm == nil || stm.ResearchWorkflowState == nil {
		return nil
	}
	cloned := &ResearchWorkflowState{}
	*cloned = *stm.ResearchWorkflowState
	cloned.SuccessfulCategories = append([]string(nil), stm.ResearchWorkflowState.SuccessfulCategories...)
	cloned.SuccessfulCategoryPaths = append([]string(nil), stm.ResearchWorkflowState.SuccessfulCategoryPaths...)
	cloned.SuccessfulItems = append([]string(nil), stm.ResearchWorkflowState.SuccessfulItems...)
	cloned.SuccessfulItemPaths = append([]string(nil), stm.ResearchWorkflowState.SuccessfulItemPaths...)
	return cloned
}

func (stm *ShortTermMemory) ResetProjectionForTopicSwitch() {
	stm.LastRoutingState = nil
	stm.LastCarryoverState = nil
	if len(stm.LastMRUSnapshot) == 0 {
		return
	}
	filtered := stm.LastMRUSnapshot[:0]
	for _, item := range stm.LastMRUSnapshot {
		if shouldPreserveMRUOnTopicSwitch(item) {
			filtered = append(filtered, item)
		}
	}
	stm.LastMRUSnapshot = append([]MRUItem(nil), filtered...)
}

// RefinementProposal holds the proposed changes for a script.
type RefinementProposal struct {
	ScriptName     string
	Category       string
	OriginalScript ai.Script
	NewScript      ai.Script
	Description    string   // The new summary description
	NewParams      []string // List of new parameters
	Replacements   []string // Human readable list of replacements
}

// NewRunnerSession creates a new runner session.
func NewRunnerSession() *RunnerSession {
	return &RunnerSession{
		Memory: NewShortTermMemory(),
	}
}
