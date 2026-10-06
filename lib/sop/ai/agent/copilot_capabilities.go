package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/agent/domains/desktop"
	"github.com/sharedcode/sop/ai/agent/domains/ideworker"
	"github.com/sharedcode/sop/ai/agent/domains/spaces"
	"github.com/sharedcode/sop/ai/agent/domains/stores"
	multistepplanner "github.com/sharedcode/sop/ai/agent/multistepplanner"
	plannerpkg "github.com/sharedcode/sop/ai/agent/planner"
)

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
	if a == nil {
		return
	}
	if a.capabilityRegistry == nil {
		a.capabilityRegistry = capabilities.NewRegistry()
	}
	// Build capability list dynamically based on feature flags.
	// Desktop automation and coding workflow are local-runtime only and must stay unavailable in HQ.
	caps := []capabilities.Capability{}
	if backendOwnsCodingWorkflow(a.ideWorkerBackend) {
		ideWorkerCap := ideworker.NewCapability()
		ideWorkerCap.SetBackend(a.ideWorkerBackend)
		caps = append(caps, ideWorkerCap)
	}

	// Stores feature disabled by default for v1 (AI Copilot focus)
	if StoresFeatureEnabled() {
		caps = append(caps, stores.NewCapability())
	}

	caps = append(caps, spaces.NewCapability())

	if localDesktopAutomationEnabled.Load() || GetAutomationPolicy().Enabled {
		desktopCap := desktop.NewCapability()
		desktopCap.SetCommandExecutor(a.toolExecuteLocalCommand)    // For LLM tool calls
		desktopCap.SetRawCommandExecutor(a.rawLocalCommandExecutor) // For direct /c commands
		caps = append(caps, desktopCap)
	}

	if localDesktopAutomationEnabled.Load() {
		cwfInfra := cw.NewDefaultSharedInfrastructure()
		// Inject STM accessor for phase succession (explicit parameter passing, not context)
		cwfInfra.STMAccessor = a.getSTMForPhaseCapabilities

		// Inject session queue accessor for work item enqueueing (Participant Pattern)
		cwfInfra.GetSessionQueue = a.getSessionQueueForCapabilities

		// Reuse the existing local automation command path for desktop phase tools.
		cwfInfra.LocalCommandExecutor = a.toolExecuteLocalCommand

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
		cwfInfra.PlanningContextResolver = a.resolvePlanningContextForDomain
		cw.SetDefaultPlanningContextResolver(cwfInfra.PlanningContextResolver)
		cwfInfra.Planner = cw.NewLLMPlanner(cwfInfra.LLMAccessor, cwfInfra.Planner)

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

		// Domain phase Capabilities are created per Ask turn by the Phase Factory.
	}

	for _, cap := range caps {
		a.capabilityRegistry.Register(cap)
	}
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

func (w *workStreamContextAdapter) GetStructuredPlan() cw.StructuredPlanReader {
	if w == nil || w.ctx == nil || w.ctx.StructuredPlan == nil {
		return nil
	}
	return &structuredPlanAdapter{plan: w.ctx.StructuredPlan}
}

func (w *workStreamContextAdapter) SetStructuredPlan(plan cw.StructuredPlanReader) {
	if w == nil || w.ctx == nil || plan == nil {
		return
	}
	w.ctx.StructuredPlan = &cw.StructuredPlan{
		Objective:      plan.GetObjective(),
		WorkflowShape:  plan.GetWorkflowShape(),
		Domain:         plan.GetDomain(),
		SystemContext:  plan.GetSystemContext(),
		Steps:          append([]cw.StructuredPlanStep(nil), plan.GetSteps()...),
		CurrentStep:    plan.GetCurrentStep(),
		CompletedSteps: append([]string(nil), plan.GetCompletedSteps()...),
		CompletionGate: plan.GetCompletionGate(),
	}
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

type structuredPlanAdapter struct {
	plan *cw.StructuredPlan
}

func (s *structuredPlanAdapter) GetObjective() string {
	if s == nil || s.plan == nil {
		return ""
	}
	return s.plan.Objective
}

func (s *structuredPlanAdapter) GetWorkflowShape() string {
	if s == nil || s.plan == nil {
		return ""
	}
	return s.plan.WorkflowShape
}

func (s *structuredPlanAdapter) GetDomain() string {
	if s == nil || s.plan == nil {
		return ""
	}
	return s.plan.Domain
}

func (s *structuredPlanAdapter) GetSystemContext() cw.StructuredSystemContext {
	if s == nil || s.plan == nil {
		return cw.StructuredSystemContext{}
	}
	return s.plan.SystemContext
}

func (s *structuredPlanAdapter) GetSteps() []cw.StructuredPlanStep {
	if s == nil || s.plan == nil {
		return nil
	}
	return s.plan.Steps
}

func (s *structuredPlanAdapter) SetSteps(steps []cw.StructuredPlanStep) {
	if s == nil || s.plan == nil {
		return
	}
	s.plan.Steps = append([]cw.StructuredPlanStep(nil), steps...)
}

func (s *structuredPlanAdapter) GetCurrentStep() int {
	if s == nil || s.plan == nil {
		return -1
	}
	return s.plan.CurrentStep
}

func (s *structuredPlanAdapter) SetCurrentStep(idx int) {
	if s == nil || s.plan == nil {
		return
	}
	s.plan.CurrentStep = idx
}

func (s *structuredPlanAdapter) GetCompletedSteps() []string {
	if s == nil || s.plan == nil {
		return nil
	}
	return s.plan.CompletedSteps
}

func (s *structuredPlanAdapter) SetCompletedSteps(steps []string) {
	if s == nil || s.plan == nil {
		return
	}
	s.plan.CompletedSteps = append([]string(nil), steps...)
}

func (s *structuredPlanAdapter) GetCompletionGate() cw.StructuredCompletionGate {
	if s == nil || s.plan == nil {
		return cw.StructuredCompletionGate{}
	}
	return s.plan.CompletionGate
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

// InjectIDEWorkerBackend injects an IDE worker backend into the IDE worker capability.
func (a *CopilotAgent) InjectIDEWorkerBackend(backend ideworker.Backend) {
	if a == nil {
		return
	}
	a.ideWorkerBackend = backend
	a.ensureCapabilityRegistry()
	if a.capabilityRegistry == nil {
		return
	}
	if !backendOwnsCodingWorkflow(backend) {
		a.capabilityRegistry.Unregister(ideworker.CapabilityID)
		return
	}
	a.unregisterNativeCodingPhaseCapabilities()
	for _, cap := range a.capabilityRegistry.ListAll() {
		if ideCap, ok := cap.(*ideworker.Capability); ok {
			ideCap.SetBackend(backend)
			return
		}
	}
	ideCap := ideworker.NewCapability()
	ideCap.SetBackend(backend)
	a.capabilityRegistry.Register(ideCap)
}

func backendOwnsCodingWorkflow(backend ideworker.Backend) bool {
	owner, ok := backend.(ideworker.CodingWorkflowOwner)
	return ok && owner.OwnsCodingWorkflow()
}

func (a *CopilotAgent) unregisterNativeCodingPhaseCapabilities() {
	if a == nil || a.capabilityRegistry == nil {
		return
	}
	for _, cap := range a.capabilityRegistry.ListAll() {
		if cap != nil && strings.HasPrefix(cap.ID(), "phase_coding_workflow_") {
			a.capabilityRegistry.Unregister(cap.ID())
		}
	}
}

func (a *CopilotAgent) ideWorkerOwnsCodingWorkflow() bool {
	if a == nil || a.capabilityRegistry == nil {
		return false
	}
	for _, cap := range a.capabilityRegistry.ListAll() {
		if ideCap, ok := cap.(*ideworker.Capability); ok {
			return ideCap.OwnsCodingWorkflow()
		}
	}
	return false
}
