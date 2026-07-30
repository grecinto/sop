package codingworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// StepOutcome represents the result of a Step execution.
type StepOutcome string

const (
	StepOutcomeSuccess      StepOutcome = "success"       // Step completed successfully
	StepOutcomeFailed       StepOutcome = "failed"        // Step failed permanently
	StepOutcomeRetry        StepOutcome = "retry"         // Step should be retried
	StepOutcomeNeedsClarify StepOutcome = "needs_clarify" // Step needs user clarification
	StepOutcomeSkipped      StepOutcome = "skipped"       // Step was skipped (condition not met)
	StepOutcomeShortCircuit StepOutcome = "short_circuit" // Step detected terminal failure, stop workflow
	StepOutcomeUserRedirect StepOutcome = "user_redirect" // User wants to change workflow direction
	StepOutcomeUserModify   StepOutcome = "user_modify"   // User wants to modify current step behavior
)

// StepResult captures the outcome and data from a Step execution.
type StepResult struct {
	Outcome         StepOutcome            // Step outcome (success, failed, retry, etc.)
	Message         string                 // Human-readable result message
	Data            map[string]interface{} // Step-specific output data
	Error           error                  // Error if outcome is failed
	RetryAfter      time.Duration          // Suggested retry delay (for retry outcome)
	ClarifyPrompt   string                 // Clarification prompt (for needs_clarify outcome)
	ClarifyOptions  []string               // Options for clarification
	WorkflowSummary string                 // Summary of workflow progress for this step
}

// StepMetadata contains static information about a Step.
type StepMetadata struct {
	ID          string   // Unique identifier for the step
	Name        string   // Human-readable name
	Description string   // What this step does
	IsTerminal  bool     // If true, successful completion ends the workflow
	Tags        []string // Tags for categorization
	MaxRetries  int      // Maximum retry attempts (0 = no retries, -1 = use default)
}

// SignalMode determines how a signal is processed
type SignalMode string

const (
	// SignalModeFunctionCall executes a lightweight function directly without Ask/ReAct loop
	// Use for: KB searches, quick repo lookups, simple data augmentation
	// No user interaction, no tool calls, fast and synchronous
	SignalModeFunctionCall SignalMode = "function_call"

	// SignalModeFullStep creates and executes a full Step with Ask/ReAct pipeline
	// Use for: Complex research requiring LLM reasoning, user interaction, multi-turn conversation
	// Can use all lifecycle hooks, tools, and handle user input
	SignalModeFullStep SignalMode = "full_step"
)

// SignalType identifies the kind of research or analysis requested
type SignalType string

const (
	// Research signals - gather information
	SignalResearchBestPractices SignalType = "research_best_practices" // Search KB/codebase for best practices
	SignalResearchCodePatterns  SignalType = "research_code_patterns"  // Find existing code patterns
	SignalResearchDependencies  SignalType = "research_dependencies"   // Analyze dependencies
	SignalResearchArchitecture  SignalType = "research_architecture"   // Study architecture patterns
	SignalResearchTestCoverage  SignalType = "research_test_coverage"  // Analyze test coverage

	// Analysis signals - interpret data
	SignalAnalyzeImpact     SignalType = "analyze_impact"     // Assess change impact
	SignalAnalyzeComplexity SignalType = "analyze_complexity" // Measure code complexity

	// Validation signals - verify correctness
	SignalValidateDesign   SignalType = "validate_design"   // Validate design decisions
	SignalValidateSecurity SignalType = "validate_security" // Security analysis
)

// SignalPriority determines execution order when multiple signals are emitted
type SignalPriority int

const (
	SignalPriorityLow      SignalPriority = 0
	SignalPriorityMedium   SignalPriority = 1
	SignalPriorityHigh     SignalPriority = 2
	SignalPriorityCritical SignalPriority = 3
)

// StepSignal represents a request for pre-step research or analysis.
// Signals are emitted by Steps via GetSignals() before execution.
//
// Two execution modes:
//
//  1. Function Call (lightweight):
//     - Direct function execution, no Ask/ReAct loop
//     - Used for: KB searches, quick lookups, data augmentation
//     - Example: Search KB for "Go best practices" and inject into prompt
//     - Fast, synchronous, no user interaction
//
//  2. Full Step (powerful):
//     - Creates real Step with Ask/ReAct pipeline
//     - Used for: Complex research requiring LLM reasoning
//     - Example: Analyze codebase architecture with user discussion
//     - Can use tools, ask questions, multi-turn conversation
type StepSignal struct {
	Type        SignalType             // What kind of research is needed
	Mode        SignalMode             // How to execute: function_call or full_step
	Description string                 // Human-readable description
	Context     map[string]interface{} // Additional context for research
	Priority    SignalPriority         // Execution priority (higher first)

	// For function call mode: direct handler function
	Handler func(ctx context.Context, infra *SharedInfrastructure, signal StepSignal) (map[string]interface{}, error)

	// For full step mode: Step factory function
	StepFactory func(signal StepSignal) Step
}

// WorkflowRedirect tracks when a user redirects the workflow to a different direction
type WorkflowRedirect struct {
	FromStep     string    // Step ID where redirect occurred
	UserFeedback string    // User's message that triggered redirect
	NewDirection string    // New workflow direction (TaskType or description)
	Timestamp    time.Time // When the redirect occurred
}

// WorkflowState flows through all steps, accumulating context and research findings.
// Each step can read previous steps' results and research data.
type WorkflowState struct {
	SharedData   map[string]interface{} // General cross-step data
	StepHistory  []StepResult           // Previous step results (ordered)
	ResearchData map[string]interface{} // Research findings keyed by signal type

	// Metadata
	WorkflowID  string
	StartTime   time.Time
	CurrentStep int

	// Re-entrancy and user interaction support
	OriginalWorkflowType string             // Original workflow type before any redirects
	RedirectHistory      []WorkflowRedirect // History of workflow redirects
	PreviousSteps        []StepResult       // Steps from abandoned workflows
	UserInterrupted      bool               // Flag indicating user interrupted workflow
}

// SetResearchData stores research findings for a specific signal type
func (ws *WorkflowState) SetResearchData(signalType SignalType, data interface{}) {
	if ws.ResearchData == nil {
		ws.ResearchData = make(map[string]interface{})
	}
	ws.ResearchData[string(signalType)] = data
}

// GetResearchData retrieves research findings for a specific signal type
func (ws *WorkflowState) GetResearchData(signalType SignalType) (interface{}, bool) {
	if ws.ResearchData == nil {
		return nil, false
	}
	data, ok := ws.ResearchData[string(signalType)]
	return data, ok
}

// GetPreviousStepResult retrieves the result of a previous step by index
func (ws *WorkflowState) GetPreviousStepResult(index int) (StepResult, bool) {
	if index < 0 || index >= len(ws.StepHistory) {
		return StepResult{}, false
	}
	return ws.StepHistory[index], true
}

// GetLastStepResult retrieves the most recent step result
func (ws *WorkflowState) GetLastStepResult() (StepResult, bool) {
	if len(ws.StepHistory) == 0 {
		return StepResult{}, false
	}
	return ws.StepHistory[len(ws.StepHistory)-1], true
}

// StepEventType identifies step lifecycle events
type StepEventType string

const (
	// StepEventBeforeExecution fires before step execution starts
	StepEventBeforeExecution StepEventType = "before_execution"

	// StepEventAfterExecution fires after step execution completes
	StepEventAfterExecution StepEventType = "after_execution"

	// StepEventBeforePrompt fires during prompt construction
	StepEventBeforePrompt StepEventType = "before_prompt"

	// StepEventAfterTool fires after tool execution
	StepEventAfterTool StepEventType = "after_tool"
)

// StepEvent captures context about a step lifecycle event
type StepEvent struct {
	Type          StepEventType          // Event type
	Step          Step                   // The step being executed
	WorkflowState *WorkflowState         // Current workflow state
	Context       map[string]interface{} // Additional event context
}

// StepEventHandler processes step lifecycle events.
// Handlers can inject signals to trigger research before/after steps.
//
// Example use cases:
//   - BestPracticesHandler: Auto-inject best practices research for code gen steps
//   - ComplexityAnalysisHandler: Trigger complexity analysis for large changes
//   - SecurityValidationHandler: Validate security for sensitive operations
type StepEventHandler interface {
	// HandleEvent processes an event and optionally returns signals to inject.
	// Return nil or empty slice if no signals needed.
	HandleEvent(ctx context.Context, event StepEvent) ([]StepSignal, error)

	// CanHandle determines if this handler should process the event.
	// Use this to filter by event type, step tags, or workflow state.
	CanHandle(event StepEvent) bool
}

// Step represents one unit of work in a workflow.
// Each Step is executed as a single Ask transaction through the Ask pipeline.
//
// Lifecycle:
//  1. WorkflowManager calls step.Metadata() to get step info
//  2. WorkflowManager calls step.SetSharedInfrastructure() to inject dependencies
//  3. WorkflowManager feeds Step to Ask pipeline (via queue or direct call)
//  4. Ask pipeline calls lifecycle hooks:
//     - OnStart() - Initialize step state
//     - OnBeforePrompt() - Inject step-specific context
//     - OnAfterToolResult() - React to tool execution, access SharedInfrastructure
//     - OnAfterTurn() - Decide if step continues or completes
//     - OnFinalize() - Return StepResult
//  5. WorkflowManager receives StepResult and decides next action
//
// Shared Infrastructure Access:
//
//	Steps access SharedInfrastructure via direct field:
//	  type MyStep struct {
//	      BaseStep
//	  }
//	  func (s *MyStep) OnAfterToolResult(...) {
//	      astOps := s.infra.NewASTOperations()
//	      parser, root, err := astOps.ParseFile(path, content)
//	  }
//
// Example Step:
//
//	type RunTestsStep struct {
//	    BaseStep
//	    testFile string
//	}
//
//	func (s *RunTestsStep) OnAfterToolResult(ctx, result) {
//	    testOps := s.infra.NewTestOperations()
//	    testResult, err := testOps.RunTests(task, state)
//	    // Process test results...
//	}
type Step interface {
	// Metadata returns static information about this step
	Metadata() StepMetadata

	// SetSharedInfrastructure injects the shared infrastructure for this step.
	// Called by WorkflowManager before step execution.
	SetSharedInfrastructure(infra *SharedInfrastructure)

	// SetWorkflowState injects the workflow state for cross-step data access.
	// Called by WorkflowManager before step execution.
	SetWorkflowState(state *WorkflowState)

	// GetWorkflowState retrieves the workflow state.
	// Steps can access previous step results and research data.
	GetWorkflowState() *WorkflowState

	// GetSignals emits signals for research or analysis BEFORE step execution.
	// WorkflowManager processes signals before running the step.
	//
	// Signals can be:
	//   - Function call mode: Quick KB/repo searches (no Ask loop)
	//   - Full step mode: Complex research with Ask/ReAct pipeline
	//
	// Return nil or empty slice if no research needed.
	GetSignals(ctx context.Context, workflowState *WorkflowState) ([]StepSignal, error)

	// Lifecycle hooks called by Ask pipeline

	// OnStart is called when the step begins execution.
	// Use this to initialize step-specific state.
	OnStart(ctx context.Context, query string, state capabilities.RoutingState) error

	// OnBeforePrompt is called during prompt construction.
	// Return additional context to inject into the system prompt.
	// Steps can inject research findings from workflowState here.
	OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error)

	// OnAfterToolResult is called after each tool execution within the step.
	// This is where steps access SharedInfrastructure to perform operations.
	// Return updated state and whether to continue.
	OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error)

	// OnAfterTurn is called after each ReAct turn.
	// Return whether the step should continue or complete.
	OnAfterTurn(ctx context.Context, turnNumber int, state capabilities.RoutingState) (bool, error)

	// OnFinalize is called when the step completes (or fails).
	// Return the final StepResult.
	OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error)
}

// WorkflowResult captures the overall workflow execution outcome.
type WorkflowResult struct {
	Success        bool                   // Overall workflow success
	CompletedSteps []string               // IDs of steps that completed successfully
	FailedStep     string                 // ID of step that caused failure (if any)
	StepResults    map[string]StepResult  // Results for each step (keyed by step ID)
	Message        string                 // Human-readable summary
	Data           map[string]interface{} // Workflow-level output data
	Error          error                  // Error if workflow failed
	ExecutionTime  time.Duration          // Total execution time
	RetryCount     int                    // Total retry attempts across all steps
}

// WorkflowManager orchestrates a sequence of Steps.
//
// Architecture:
//   - Layer 1: WorkflowManager (orchestrates sequence, tracks outcomes, handles retries)
//   - Layer 2: Step (one unit of work = one Ask transaction, implements lifecycle hooks)
//   - Layer 3: Ask Pipeline (processes Step through lifecycle hooks)
//
// Responsibilities:
//   - Execute Steps sequentially
//   - Track pass/fail for each Step
//   - Handle retry logic (exponential backoff, max attempts)
//   - Short-circuit on terminal failures
//   - Support user clarification mid-workflow
//   - Aggregate step results into WorkflowResult
//
// Usage:
//
//	wm := NewWorkflowManager(orchestrator, maxRetries)
//	steps := []Step{
//	    &RunTestsStep{},
//	    &AnalyzeErrorsStep{},
//	    &ApplyFixStep{},
//	    &VerifyFixStep{isTerminal: true},
//	}
//	result, err := wm.Execute(ctx, steps, task, state)
//
// Guard rail: WorkflowManager does NOT inject Steps into queue for secondary orchestration.
// Instead, it feeds each Step to the Ask pipeline as a single transaction, respecting
// the natural boundaries of the Ask pipeline's lifecycle.
type WorkflowManager struct {
	sharedInfra      *SharedInfrastructure
	defaultMaxRetry  int
	retryBackoffBase time.Duration

	// Event handling
	eventHandlers []StepEventHandler            // Global event handlers
	stepHandlers  map[string][]StepEventHandler // Step-specific handlers (keyed by step ID)

	// Signal caching
	signalCache  *SignalCache  // Cache for signal results
	cacheEnabled bool          // Whether caching is enabled
	cacheTTL     time.Duration // Cache entry TTL
}

// SignalCacheEntry represents a cached signal result
type SignalCacheEntry struct {
	Data      map[string]interface{} // Cached result data
	Timestamp time.Time              // When the entry was created
}

// SignalCache caches signal execution results to avoid redundant research
type SignalCache struct {
	mu      sync.RWMutex
	entries map[string]*SignalCacheEntry // Keyed by signal cache key
}

// NewSignalCache creates a new signal cache
func NewSignalCache() *SignalCache {
	return &SignalCache{
		entries: make(map[string]*SignalCacheEntry),
	}
}

// Get retrieves a cached entry if it exists and hasn't expired
func (sc *SignalCache) Get(key string, ttl time.Duration) (map[string]interface{}, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	entry, exists := sc.entries[key]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Since(entry.Timestamp) > ttl {
		return nil, false
	}

	return entry.Data, true
}

// Set stores a result in the cache
func (sc *SignalCache) Set(key string, data map[string]interface{}) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.entries[key] = &SignalCacheEntry{
		Data:      data,
		Timestamp: time.Now(),
	}
}

// Clear removes all cached entries
func (sc *SignalCache) Clear() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.entries = make(map[string]*SignalCacheEntry)
}

// Size returns the number of cached entries
func (sc *SignalCache) Size() int {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	return len(sc.entries)
}

// Prune removes expired entries
func (sc *SignalCache) Prune(ttl time.Duration) int {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	removed := 0
	for key, entry := range sc.entries {
		if time.Since(entry.Timestamp) > ttl {
			delete(sc.entries, key)
			removed++
		}
	}

	return removed
}

// generateSignalCacheKey creates a cache key from signal type and context
func generateSignalCacheKey(signal StepSignal) string {
	// Create hash of signal type + context for cache key
	contextJSON, _ := json.Marshal(signal.Context)
	hash := sha256.Sum256([]byte(string(signal.Type) + string(contextJSON)))
	return fmt.Sprintf("%s_%x", signal.Type, hash[:8])
}

// NewWorkflowManager creates a WorkflowManager with the provided SharedInfrastructure.
func NewWorkflowManager(sharedInfra *SharedInfrastructure) *WorkflowManager {
	if sharedInfra == nil {
		sharedInfra = NewDefaultSharedInfrastructure()
	}
	wm := &WorkflowManager{
		sharedInfra:      sharedInfra,
		defaultMaxRetry:  2,               // Default: retry failed steps up to 2 times
		retryBackoffBase: 1 * time.Second, // Base backoff for exponential retry
		eventHandlers:    []StepEventHandler{},
		stepHandlers:     make(map[string][]StepEventHandler),
		signalCache:      NewSignalCache(),
		cacheEnabled:     true,            // Enable caching by default
		cacheTTL:         5 * time.Minute, // Default cache TTL
	}

	// Register default handlers
	wm.registerDefaultHandlers()

	return wm
}

// RegisterEventHandler adds a global event handler.
// Global handlers fire for all steps.
func (wm *WorkflowManager) RegisterEventHandler(handler StepEventHandler) {
	wm.eventHandlers = append(wm.eventHandlers, handler)
}

// RegisterStepEventHandler adds a handler for a specific step.
// Step-specific handlers only fire for the specified step ID.
func (wm *WorkflowManager) RegisterStepEventHandler(stepID string, handler StepEventHandler) {
	if wm.stepHandlers == nil {
		wm.stepHandlers = make(map[string][]StepEventHandler)
	}
	wm.stepHandlers[stepID] = append(wm.stepHandlers[stepID], handler)
}

// emitEvent dispatches an event to all applicable handlers.
// Returns signals emitted by handlers (merged).
func (wm *WorkflowManager) emitEvent(ctx context.Context, eventType StepEventType, step Step, workflowState *WorkflowState) []StepSignal {
	event := StepEvent{
		Type:          eventType,
		Step:          step,
		WorkflowState: workflowState,
		Context:       make(map[string]interface{}),
	}

	var signals []StepSignal

	// Global handlers
	for _, handler := range wm.eventHandlers {
		if handler.CanHandle(event) {
			handlerSignals, err := handler.HandleEvent(ctx, event)
			if err != nil {
				// Log warning but continue - event handlers are best-effort
				fmt.Printf("Warning: Event handler failed: %v\n", err)
				continue
			}
			signals = append(signals, handlerSignals...)
		}
	}

	// Step-specific handlers
	stepID := step.Metadata().ID
	if handlers, ok := wm.stepHandlers[stepID]; ok {
		for _, handler := range handlers {
			if handler.CanHandle(event) {
				handlerSignals, err := handler.HandleEvent(ctx, event)
				if err != nil {
					fmt.Printf("Warning: Step event handler failed for step %s: %v\n", stepID, err)
					continue
				}
				signals = append(signals, handlerSignals...)
			}
		}
	}

	return signals
}

// registerDefaultHandlers registers built-in event handlers
func (wm *WorkflowManager) registerDefaultHandlers() {
	// Add default handlers here
	// For now, no default handlers - users can register their own
}

// Execute is intentionally disabled for the capability-based workflow model.
// Callers should participate through the Workflow Manager capability in Ask.
func (wm *WorkflowManager) Execute(ctx context.Context, steps []Step, task Task, state RunState) (WorkflowResult, error) {
	_ = ctx
	_ = steps
	_ = task
	_ = state
	return WorkflowResult{}, fmt.Errorf("controller-style workflow execution is disabled: use the workflow manager capability via Ask")
}

// runWorkflow executes a sequence of Steps and returns the aggregated WorkflowResult.
//
// Execution flow:
//  1. Initialize workflow state
//  2. For each Step:
//     a. Emit BeforeExecution event (collect signals from handlers)
//     b. Call GetSignals() on step (collect step-specific signals)
//     c. Merge and process all signals (function calls or full steps)
//     d. Execute step with retry logic (exponential backoff)
//     e. Emit AfterExecution event
//     f. Process StepResult (success/failed/retry/skip/short-circuit)
//  3. Aggregate results into WorkflowResult
//
// Signal sources (merged in priority order):
//   - Event handlers (BeforeExecution)
//   - Step.GetSignals()
//
// Guard rail: Each Step is executed as ONE Ask transaction. The WorkflowManager
// coordinates the sequence but does NOT create a secondary orchestration loop.
func (wm *WorkflowManager) runWorkflow(ctx context.Context, steps []Step, task Task, state RunState) (WorkflowResult, error) {
	startTime := time.Now()

	// Create workflow state for cross-step data sharing
	workflowState := &WorkflowState{
		SharedData:           make(map[string]interface{}),
		StepHistory:          []StepResult{},
		ResearchData:         make(map[string]interface{}),
		WorkflowID:           fmt.Sprintf("wf_%d", time.Now().UnixNano()),
		StartTime:            startTime,
		OriginalWorkflowType: task.TaskType,
		RedirectHistory:      []WorkflowRedirect{},
		PreviousSteps:        []StepResult{},
		UserInterrupted:      false,
	}

	result := WorkflowResult{
		Success:        true,
		CompletedSteps: []string{},
		StepResults:    make(map[string]StepResult),
		Data:           make(map[string]interface{}),
	}

	// Execute each step sequentially
	for i, step := range steps {
		workflowState.CurrentStep = i

		// Inject SharedInfrastructure and WorkflowState into step
		step.SetSharedInfrastructure(wm.sharedInfra)
		step.SetWorkflowState(workflowState)

		metadata := step.Metadata()
		stepID := metadata.ID
		if stepID == "" {
			stepID = fmt.Sprintf("step_%d", i)
		}

		// === SIGNAL COLLECTION PHASE ===

		// 1. Emit BeforeExecution event (collect signals from handlers)
		eventSignals := wm.emitEvent(ctx, StepEventBeforeExecution, step, workflowState)

		// 2. Get step-specific signals
		stepSignals, err := step.GetSignals(ctx, workflowState)
		if err != nil {
			fmt.Printf("Warning: Failed to get signals from step %s: %v\n", stepID, err)
			stepSignals = nil
		}

		// 3. Merge signals (deduplicate by type, keep highest priority)
		allSignals := wm.mergeSignals(eventSignals, stepSignals)

		// 4. Process signals (execute research before step)
		if len(allSignals) > 0 {
			if err := wm.processSignals(ctx, allSignals, task, state, workflowState, &result); err != nil {
				fmt.Printf("Warning: Signal processing failed for step %s: %v\n", stepID, err)
				// Continue anyway - research is best-effort
			}
		}

		// === STEP EXECUTION PHASE ===

		// Execute step with retry logic
		stepResult, err := wm.executeStepWithRetry(ctx, step, task, state, metadata)
		result.StepResults[stepID] = stepResult
		workflowState.StepHistory = append(workflowState.StepHistory, stepResult)

		// Emit AfterExecution event
		wm.emitEvent(ctx, StepEventAfterExecution, step, workflowState)

		// Handle step outcome
		result.StepResults[stepID] = stepResult

		// Handle step outcome
		switch stepResult.Outcome {
		case StepOutcomeSuccess:
			result.CompletedSteps = append(result.CompletedSteps, stepID)

			// If this is a terminal step, stop workflow successfully
			if metadata.IsTerminal {
				result.Message = fmt.Sprintf("Workflow completed at terminal step: %s", metadata.Name)
				result.ExecutionTime = time.Since(startTime)
				return result, nil
			}

		case StepOutcomeFailed:
			result.Success = false
			result.FailedStep = stepID
			result.Message = fmt.Sprintf("Workflow failed at step: %s - %s", metadata.Name, stepResult.Message)
			result.Error = err
			result.ExecutionTime = time.Since(startTime)
			return result, err

		case StepOutcomeShortCircuit:
			result.Success = false
			result.FailedStep = stepID
			result.Message = fmt.Sprintf("Workflow short-circuited at step: %s - %s", metadata.Name, stepResult.Message)
			result.Error = fmt.Errorf("workflow short-circuited")
			result.ExecutionTime = time.Since(startTime)
			return result, result.Error

		case StepOutcomeSkipped:
			// Step was skipped, continue to next
			continue

		case StepOutcomeUserRedirect:
			// User wants to change workflow direction
			result.CompletedSteps = append(result.CompletedSteps, stepID)

			// Mark workflow as interrupted
			workflowState.UserInterrupted = true

			// Extract user feedback and new direction
			userFeedback := ""
			newDirection := ""
			if feedback, ok := stepResult.Data["user_feedback"].(string); ok {
				userFeedback = feedback
			}
			if direction, ok := stepResult.Data["new_direction"].(string); ok {
				newDirection = direction
			}

			// Record the redirect in history
			redirect := WorkflowRedirect{
				FromStep:     stepID,
				UserFeedback: userFeedback,
				NewDirection: newDirection,
				Timestamp:    time.Now(),
			}
			workflowState.RedirectHistory = append(workflowState.RedirectHistory, redirect)

			// Store current progress before redirect
			if len(workflowState.PreviousSteps) == 0 {
				// First redirect - preserve original steps
				workflowState.PreviousSteps = make([]StepResult, len(workflowState.StepHistory))
				copy(workflowState.PreviousSteps, workflowState.StepHistory)
			}

			// Re-classify user's new direction to determine workflow type
			// Use the feedback as the query for classification
			classifier := DefaultTaskClassifier()
			classification := classifier.Classify(userFeedback, "", "")

			// Create new workflow steps based on classification
			newSteps := wm.createWorkflowSteps(classification.TaskType, task)

			// Replace remaining steps with new workflow
			// Keep completed steps, replace upcoming steps
			steps = append(steps[:i+1], newSteps...)

			// Log the redirect
			fmt.Printf("Workflow redirected at step %s: %s -> %s\n",
				stepID, workflowState.OriginalWorkflowType, classification.TaskType)

			continue

		case StepOutcomeUserModify:
			// User wants to modify current step behavior
			result.CompletedSteps = append(result.CompletedSteps, stepID)

			// Extract user modifications
			userFeedback := ""
			if feedback, ok := stepResult.Data["user_feedback"].(string); ok {
				userFeedback = feedback
			}

			// Store modification in workflow state for next steps to use
			if workflowState.SharedData == nil {
				workflowState.SharedData = make(map[string]interface{})
			}

			// Store latest modification
			workflowState.SharedData["user_modifications"] = userFeedback
			workflowState.SharedData["modified_at_step"] = stepID

			// Accumulate modification history for context
			var modHistory []string
			if existing, ok := workflowState.SharedData["modification_history"].([]string); ok {
				modHistory = existing
			}
			modificationEntry := fmt.Sprintf("[Step %s] %s", stepID, userFeedback)
			modHistory = append(modHistory, modificationEntry)
			workflowState.SharedData["modification_history"] = modHistory

			// Continue with modified context
			fmt.Printf("Workflow modified at step %s: %s\n", stepID, userFeedback)
			continue

		default:
			// Unknown outcome, treat as failure
			result.Success = false
			result.FailedStep = stepID
			result.Message = fmt.Sprintf("Workflow failed with unknown outcome at step: %s", metadata.Name)
			result.Error = fmt.Errorf("unknown step outcome: %s", stepResult.Outcome)
			result.ExecutionTime = time.Since(startTime)
			return result, result.Error
		}
	}

	// All steps completed
	result.Message = "Workflow completed successfully"
	result.ExecutionTime = time.Since(startTime)
	return result, nil
}

// executeStepWithRetry executes a single step with retry logic.
func (wm *WorkflowManager) executeStepWithRetry(
	ctx context.Context,
	step Step,
	task Task,
	state RunState,
	metadata StepMetadata,
) (StepResult, error) {
	maxRetries := metadata.MaxRetries
	if maxRetries < 0 {
		maxRetries = wm.defaultMaxRetry
	}

	var lastResult StepResult
	var lastError error
	retryCount := 0

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Apply exponential backoff for retries
		if attempt > 0 {
			backoff := wm.retryBackoffBase * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return StepResult{
					Outcome: StepOutcomeFailed,
					Message: "Context cancelled during retry backoff",
					Error:   ctx.Err(),
				}, ctx.Err()
			case <-time.After(backoff):
			}
			retryCount++
		}

		// Execute step (this will be fed to Ask pipeline)
		// For now, we return a placeholder result.
		// In the full implementation, this would feed the Step to Ask pipeline
		// and the Ask pipeline would call the lifecycle hooks.
		stepResult, err := wm.executeStepOnce(ctx, step, task, state)

		// Handle outcome
		switch stepResult.Outcome {
		case StepOutcomeSuccess:
			return stepResult, nil

		case StepOutcomeFailed:
			lastResult = stepResult
			lastError = err
			// Retry if attempts remain
			if attempt < maxRetries {
				continue
			}
			return stepResult, err

		case StepOutcomeRetry:
			lastResult = stepResult
			lastError = err
			// Explicit retry request
			if attempt < maxRetries {
				continue
			}
			// Max retries exhausted
			stepResult.Outcome = StepOutcomeFailed
			stepResult.Message = fmt.Sprintf("Max retries exhausted: %s", stepResult.Message)
			return stepResult, fmt.Errorf("max retries exhausted")

		case StepOutcomeNeedsClarify:
			// Handle clarification (would interact with user)
			// For now, treat as failure
			lastResult = stepResult
			lastError = fmt.Errorf("clarification needed: %s", stepResult.ClarifyPrompt)
			if attempt < maxRetries {
				continue
			}
			return stepResult, lastError

		case StepOutcomeShortCircuit:
			// Terminal failure, don't retry
			return stepResult, fmt.Errorf("step short-circuited")

		case StepOutcomeSkipped:
			return stepResult, nil

		default:
			lastResult = stepResult
			lastError = fmt.Errorf("unknown outcome: %s", stepResult.Outcome)
			return stepResult, lastError
		}
	}

	// Should not reach here, but return last result
	return lastResult, lastError
}

// executeStepOnce executes a step once (without retry logic).
// This is where the Step would be fed to the Ask pipeline.
//
// TODO: Integrate with Ask pipeline to call Step lifecycle hooks.
// For now, this is a placeholder that calls the Step's lifecycle hooks directly.
func (wm *WorkflowManager) executeStepOnce(
	ctx context.Context,
	step Step,
	task Task,
	state RunState,
) (StepResult, error) {
	// Build routing state from task
	routingState := capabilities.RoutingState{
		Domain:         "coding",
		CapabilityHint: "coding_workflow",
		// Add more fields as needed
	}

	// Call lifecycle hooks (in real implementation, Ask pipeline calls these)

	// 1. OnStart
	if err := step.OnStart(ctx, task.Description, routingState); err != nil {
		return StepResult{
			Outcome: StepOutcomeFailed,
			Message: fmt.Sprintf("OnStart failed: %v", err),
			Error:   err,
		}, err
	}

	// 2. OnBeforePrompt
	additionalContext, err := step.OnBeforePrompt(ctx, task.Description, routingState)
	if err != nil {
		return StepResult{
			Outcome: StepOutcomeFailed,
			Message: fmt.Sprintf("OnBeforePrompt failed: %v", err),
			Error:   err,
		}, err
	}
	_ = additionalContext // Would be injected into prompt

	// 3. Execute reasoning (would happen in Ask pipeline)
	// For now, simulate one tool execution
	toolName := "example_tool"
	toolResult := map[string]interface{}{"status": "success"}

	// 4. OnAfterToolResult
	updatedState, shouldContinue, err := step.OnAfterToolResult(ctx, toolName, toolResult, routingState)
	if err != nil {
		return StepResult{
			Outcome: StepOutcomeFailed,
			Message: fmt.Sprintf("OnAfterToolResult failed: %v", err),
			Error:   err,
		}, err
	}
	routingState = updatedState
	_ = shouldContinue

	// 5. OnAfterTurn
	shouldContinue, err = step.OnAfterTurn(ctx, 1, routingState)
	if err != nil {
		return StepResult{
			Outcome: StepOutcomeFailed,
			Message: fmt.Sprintf("OnAfterTurn failed: %v", err),
			Error:   err,
		}, err
	}
	_ = shouldContinue

	// 6. OnFinalize
	stepResult, err := step.OnFinalize(ctx, routingState)
	if err != nil {
		return StepResult{
			Outcome: StepOutcomeFailed,
			Message: fmt.Sprintf("OnFinalize failed: %v", err),
			Error:   err,
		}, err
	}

	return stepResult, nil
}

// SetDefaultMaxRetry sets the default maximum retry count for steps.
func (wm *WorkflowManager) SetDefaultMaxRetry(maxRetry int) {
	wm.defaultMaxRetry = maxRetry
}

// SetRetryBackoff sets the base backoff duration for exponential retry.
func (wm *WorkflowManager) SetRetryBackoff(backoff time.Duration) {
	wm.retryBackoffBase = backoff
}

// EnableCache enables signal result caching.
func (wm *WorkflowManager) EnableCache(enabled bool) {
	wm.cacheEnabled = enabled
}

// SetCacheTTL sets the cache entry TTL (time-to-live).
func (wm *WorkflowManager) SetCacheTTL(ttl time.Duration) {
	wm.cacheTTL = ttl
}

// ClearCache clears all cached signal results.
func (wm *WorkflowManager) ClearCache() {
	wm.signalCache.Clear()
}

// PruneCache removes expired cache entries.
// Returns the number of entries removed.
func (wm *WorkflowManager) PruneCache() int {
	return wm.signalCache.Prune(wm.cacheTTL)
}

// GetCacheSize returns the current number of cached entries.
func (wm *WorkflowManager) GetCacheSize() int {
	return wm.signalCache.Size()
}

// createWorkflowSteps creates workflow steps based on task type.
// This is a helper for workflow redirection support.
// Returns a basic set of steps appropriate for the task type.
func (wm *WorkflowManager) createWorkflowSteps(taskType TaskType, task Task) []Step {
	// This is a simplified implementation for redirect support
	// or use a more sophisticated workflow factory

	switch taskType {
	case TaskTypeDebugging:
		return []Step{
			NewAnalyzeCodeStep("Analyze code to identify issues"),
			NewGeneratePatchStep("Generate fix for identified issues"),
			NewApplyFixStep(nil),
		}

	case TaskTypeTesting:
		return []Step{
			NewRunTestsStep(".", []string{"./..."}),
			NewAnalyzeErrorsStep(),
		}

	case TaskTypeImplementation:
		return []Step{
			NewDesignStep("Design implementation approach"),
			NewGenerateCodeStep("Generate implementation code"),
			NewApplyFixStep(nil),
		}

	case TaskTypeRefactoring:
		return []Step{
			NewAnalyzeCodeStep("Analyze code structure for refactoring"),
			NewGeneratePlanStep("Generate refactoring plan"),
			NewApplyRefactoringStep(),
		}

	case TaskTypeArchitecture:
		return []Step{
			NewAnalyzeCodeStep("Analyze codebase structure"),
			NewDesignArchitectureStep("Design architecture proposal"),
			NewDocumentDecisionStep("Document architecture decision"),
		}

	case TaskTypeDocumentation:
		return []Step{
			NewAnalyzeCodeStep("Analyze code structure"),
			NewGenerateDocsStep("Generate documentation"),
		}

	default:
		// General workflow
		return []Step{
			NewGeneralStep("Process request"),
		}
	}
}

// mergeSignals combines signals from multiple sources and deduplicates.
// If multiple signals have the same type, keeps the one with highest priority.
func (wm *WorkflowManager) mergeSignals(signalLists ...[]StepSignal) []StepSignal {
	// Use map to deduplicate by type, keeping highest priority
	signalMap := make(map[SignalType]StepSignal)

	for _, signals := range signalLists {
		for _, signal := range signals {
			if existing, exists := signalMap[signal.Type]; exists {
				// Keep signal with higher priority
				if signal.Priority > existing.Priority {
					signalMap[signal.Type] = signal
				}
			} else {
				signalMap[signal.Type] = signal
			}
		}
	}

	// Convert map back to slice
	var result []StepSignal
	for _, signal := range signalMap {
		result = append(result, signal)
	}

	return result
}

// processSignals executes research steps based on emitted signals.
// Signals are processed in priority order (highest first).
//
// Two execution modes:
//  1. Function Call - Direct function execution, no Ask loop
//  2. Full Step - Creates Step and executes through Ask pipeline
func (wm *WorkflowManager) processSignals(
	ctx context.Context,
	signals []StepSignal,
	task Task,
	state RunState,
	workflowState *WorkflowState,
	result *WorkflowResult,
) error {
	if len(signals) == 0 {
		return nil
	}

	// Sort signals by priority (highest first)
	sortedSignals := make([]StepSignal, len(signals))
	copy(sortedSignals, signals)
	for i := 0; i < len(sortedSignals)-1; i++ {
		for j := i + 1; j < len(sortedSignals); j++ {
			if sortedSignals[j].Priority > sortedSignals[i].Priority {
				sortedSignals[i], sortedSignals[j] = sortedSignals[j], sortedSignals[i]
			}
		}
	}

	for _, signal := range sortedSignals {
		var signalErr error

		switch signal.Mode {
		case SignalModeFunctionCall:
			signalErr = wm.processFunctionCallSignal(ctx, signal, workflowState)
		case SignalModeFullStep:
			signalErr = wm.processFullStepSignal(ctx, signal, task, state, workflowState, result)
		default:
			fmt.Printf("Warning: Unknown signal mode %s, skipping signal type %s\n", signal.Mode, signal.Type)
			continue
		}

		if signalErr != nil {
			fmt.Printf("Warning: Signal processing failed for %s (%s): %v\n", signal.Type, signal.Mode, signalErr)
			// Continue processing other signals - research is best-effort
		}
	}

	return nil
}

// processFunctionCallSignal executes a lightweight function-based signal.
// No Ask/ReAct loop, just direct function call for quick data augmentation.
// Uses caching to avoid redundant executions.
func (wm *WorkflowManager) processFunctionCallSignal(
	ctx context.Context,
	signal StepSignal,
	workflowState *WorkflowState,
) error {
	// Check cache first
	if wm.cacheEnabled {
		cacheKey := generateSignalCacheKey(signal)
		if cachedData, found := wm.signalCache.Get(cacheKey, wm.cacheTTL); found {
			// Cache hit - use cached data
			workflowState.SetResearchData(signal.Type, cachedData)
			return nil
		}
	}

	if signal.Handler == nil {
		// No handler provided, try built-in handlers
		handler := wm.getBuiltInSignalHandler(signal.Type)
		if handler == nil {
			return fmt.Errorf("no handler found for signal type: %s", signal.Type)
		}
		signal.Handler = handler
	}

	// Execute handler function
	data, err := signal.Handler(ctx, wm.sharedInfra, signal)
	if err != nil {
		return fmt.Errorf("handler execution failed: %w", err)
	}

	// Store in cache
	if wm.cacheEnabled {
		cacheKey := generateSignalCacheKey(signal)
		wm.signalCache.Set(cacheKey, data)
	}

	// Store results in workflow state
	workflowState.SetResearchData(signal.Type, data)
	return nil
}

// processFullStepSignal executes a full Step with Ask/ReAct pipeline.
// Uses caching to avoid redundant executions.
func (wm *WorkflowManager) processFullStepSignal(
	ctx context.Context,
	signal StepSignal,
	task Task,
	state RunState,
	workflowState *WorkflowState,
	result *WorkflowResult,
) error {
	// Check cache first
	if wm.cacheEnabled {
		cacheKey := generateSignalCacheKey(signal)
		if cachedData, found := wm.signalCache.Get(cacheKey, wm.cacheTTL); found {
			// Cache hit - use cached data
			workflowState.SetResearchData(signal.Type, cachedData)
			// Track as cached in workflow result
			signalStepID := fmt.Sprintf("signal_%s_cached", signal.Type)
			result.StepResults[signalStepID] = StepResult{
				Outcome: StepOutcomeSuccess,
				Message: "Used cached result",
				Data:    cachedData,
			}
			return nil
		}
	}

	// Create research step from signal
	var researchStep Step
	if signal.StepFactory != nil {
		researchStep = signal.StepFactory(signal)
	} else {
		// Try built-in step factories
		researchStep = wm.createBuiltInResearchStep(signal)
	}

	if researchStep == nil {
		return fmt.Errorf("no step factory found for signal type: %s", signal.Type)
	}

	// Inject infrastructure and workflow state
	researchStep.SetSharedInfrastructure(wm.sharedInfra)
	researchStep.SetWorkflowState(workflowState)

	// Execute research step
	metadata := researchStep.Metadata()
	stepResult, err := wm.executeStepWithRetry(ctx, researchStep, task, state, metadata)

	if err != nil || stepResult.Outcome != StepOutcomeSuccess {
		return fmt.Errorf("research step failed: %w", err)
	}

	// Store in cache
	if wm.cacheEnabled && stepResult.Data != nil {
		cacheKey := generateSignalCacheKey(signal)
		wm.signalCache.Set(cacheKey, stepResult.Data)
	}

	// Store research results in workflow state
	if stepResult.Data != nil {
		workflowState.SetResearchData(signal.Type, stepResult.Data)
	}

	// Track in workflow result
	signalStepID := fmt.Sprintf("signal_%s", signal.Type)
	result.StepResults[signalStepID] = stepResult

	return nil
}

// getBuiltInSignalHandler returns built-in function handlers for common signals.
func (wm *WorkflowManager) getBuiltInSignalHandler(signalType SignalType) func(context.Context, *SharedInfrastructure, StepSignal) (map[string]interface{}, error) {
	switch signalType {
	case SignalResearchBestPractices:
		return handleResearchBestPractices
	case SignalResearchCodePatterns:
		return handleResearchCodePatterns
	default:
		return nil
	}
}

// createBuiltInResearchStep creates built-in research steps for full step mode.
func (wm *WorkflowManager) createBuiltInResearchStep(signal StepSignal) Step {
	switch signal.Type {
	case SignalAnalyzeComplexity:
		return NewComplexityAnalysisStep(signal.Context)
	case SignalValidateSecurity:
		return NewSecurityValidationStep(signal.Context)
	case SignalResearchArchitecture:
		return NewArchitectureResearchStep(signal.Context)
	case SignalResearchDependencies:
		return NewDependencyResearchStep(signal.Context)
	case SignalResearchTestCoverage:
		return NewTestCoverageResearchStep(signal.Context)
	default:
		return nil
	}
}

// Built-in signal handlers (function call mode)

// handleResearchBestPractices searches KB for best practices.
// This is a lightweight function call - no Ask loop needed.
func handleResearchBestPractices(ctx context.Context, infra *SharedInfrastructure, signal StepSignal) (map[string]interface{}, error) {
	language, _ := signal.Context["language"].(string)
	scope, _ := signal.Context["scope"].(string)

	// TODO: Integrate with KB system to search for best practices
	// For now, return placeholder data
	practices := []string{
		"Follow language idioms and conventions",
		"Write clear, self-documenting code",
		"Add error handling for all failure cases",
		"Include unit tests for new functionality",
	}

	return map[string]interface{}{
		"language":  language,
		"scope":     scope,
		"practices": practices,
		"summary":   fmt.Sprintf("Best practices for %s %s", language, scope),
	}, nil
}

// handleResearchCodePatterns searches codebase for existing patterns.
func handleResearchCodePatterns(ctx context.Context, infra *SharedInfrastructure, signal StepSignal) (map[string]interface{}, error) {
	taskType, _ := signal.Context["task_type"].(string)
	complexity, _ := signal.Context["complexity"].(string)

	// Use RepoOperations to find patterns
	repoOps := infra.NewRepoOperations()
	_ = repoOps // TODO: Implement actual search

	// Placeholder return
	return map[string]interface{}{
		"task_type":  taskType,
		"complexity": complexity,
		"count":      3,
		"examples":   []string{"pattern1", "pattern2", "pattern3"},
		"summary":    fmt.Sprintf("Found patterns for %s tasks", taskType),
	}, nil
}
