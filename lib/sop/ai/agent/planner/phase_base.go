package planner

import (
	"context"
	"encoding/json"
	"fmt"
	log_slog "log/slog"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// BasePhase provides common functionality shared by all domain-specific phase implementations.
// Domain phases embed this struct to inherit shared behavior (artifact storage, classification, etc.)
type PhaseAuthoringProfile struct {
	Domain       string
	Principles   []string
	Guardrails   []capabilities.GuidanceRule
	Instructions map[string]string
	PhaseRules   map[string][]capabilities.GuidanceRule
}

type PhaseAuthoringProvider interface {
	PhaseAuthoringProfile() PhaseAuthoringProfile
}

type BasePhase struct {
	sharedInfra            *SharedInfrastructure
	phaseID                string
	name                   string
	description            string
	domain                 string
	captureTool            string
	stateTool              string
	classificationStrategy PhaseClassificationStrategy
	authoringProfile       PhaseAuthoringProfile
}

// newBasePhase creates a new base phase with common fields
func newBasePhase(phaseID, name, description, domain string, infra *SharedInfrastructure) BasePhase {
	// Use infrastructure's classification strategy if available, otherwise default to heuristic
	var strategy PhaseClassificationStrategy
	if infra != nil && infra.PhaseClassificationStrategy != nil {
		if s, ok := infra.PhaseClassificationStrategy.(PhaseClassificationStrategy); ok {
			strategy = s
		}
	}
	if strategy == nil {
		strategy = NewHeuristicStrategy()
	}

	return BasePhase{
		sharedInfra:            infra,
		phaseID:                phaseID,
		name:                   name,
		description:            description,
		domain:                 domain,
		captureTool:            phaseID + "_capture",
		stateTool:              phaseID + "_state",
		classificationStrategy: strategy,
	}
}

func (b *BasePhase) ID() string {
	return "phase_" + b.phaseID
}

func (b *BasePhase) Name() string {
	return b.name
}

func (b *BasePhase) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{{
			Resource: "filesystem",
			Actions:  []string{"read", "write", "create", "delete"},
			Gate:     capabilities.GateWriteAccess,
		}, {
			Resource: "memory",
			Actions:  []string{"read", "write"},
			Gate:     capabilities.GateReadAccess,
		}},
	}
}

func (b *BasePhase) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	// Use pluggable classification strategy for adaptive behavior
	// Strategy can be: heuristic (fast, keyword-based) → LLM (slower, accurate) → adaptive (auto-escalates)

	// Get classification history from infrastructure if available
	var history *ClassificationHistory
	if b.sharedInfra != nil && b.sharedInfra.ClassificationHistory != nil {
		if h, ok := b.sharedInfra.ClassificationHistory.(*ClassificationHistory); ok {
			history = h
		}
	}

	// Use strategy with context for LLM calls
	ctx := context.Background()
	return b.classificationStrategy.Classify(ctx, b, query, state, history)
}

// SetClassificationStrategy allows runtime strategy switching (heuristic → LLM → adaptive)
func (b *BasePhase) SetClassificationStrategy(strategy PhaseClassificationStrategy) {
	if strategy != nil {
		b.classificationStrategy = strategy
	}
}

// GetPhasePlanMutator returns the phase plan mutator from infrastructure if available
func (b *BasePhase) GetPhasePlanMutator() PhasePlanMutator {
	if b.sharedInfra != nil && b.sharedInfra.PhasePlanMutator != nil {
		if m, ok := b.sharedInfra.PhasePlanMutator.(PhasePlanMutator); ok {
			return m
		}
	}
	return nil
}

func (b *BasePhase) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = query
	_ = state
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}

	artifact, err := b.loadExistingArtifact()
	if err != nil || artifact.PhaseID == "" {
		return nil
	}
	if artifact.ExitRequested {
		return &capabilities.Handoff{Direction: "exit", Prompt: fmt.Sprintf("Complete the %s phase and return to the main workflow.", b.name)}
	}
	direction := strings.ToLower(strings.TrimSpace(artifact.TransitionDirection))
	target := strings.TrimSpace(artifact.TransitionTo)
	if direction == "backward" {
		target = strings.TrimSpace(artifact.TransitionFrom)
	}
	if direction == "forward" && target != "" {
		return &capabilities.Handoff{Direction: "forward", Prompt: fmt.Sprintf("Transition from %s to %s phase.", b.name, target), Target: "phase:" + target}
	}
	if direction == "backward" && target != "" {
		return &capabilities.Handoff{Direction: "backward", Prompt: fmt.Sprintf("Return from %s to %s phase.", b.name, target), Target: "phase:" + target}
	}
	return nil
}

func (b *BasePhase) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (b *BasePhase) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	_ = ctx
	_ = query
	_ = state
	_ = result
	return nil
}

func (b *BasePhase) setAuthoringProfile(profile PhaseAuthoringProfile) {
	if profile.Domain == "" {
		profile.Domain = b.domain
	}
	b.authoringProfile = profile
}

func (b *BasePhase) BuildInstructions(query string, state capabilities.RoutingState) string {
	_ = query
	_ = state

	if instruction, ok := b.authoringProfile.Instructions[b.phaseID]; ok && strings.TrimSpace(instruction) != "" {
		return instruction
	}

	switch b.domain {
	case "stores":
		switch b.phaseID {
		case "investigation":
			return "Use Investigation to inspect store structure and data before designing any operation. Keep the work read-only until the evidence is clear."
		case "planning":
			return "Use Planning to design a concrete store operation plan, including required stores, filters, and validation expectations."
		case "execution":
			return "Use Execution to run the planned store operation safely, prefer previews when available, and capture the result."
		case "review":
			return "Use Review to validate results against the requested behavior and note any remaining risk or ambiguity."
		default:
			return fmt.Sprintf("Use %s capability to work with stores", b.name)
		}
	case "spaces":
		switch b.phaseID {
		case "investigation":
			return "Use Investigation to inspect the relevant space content, configuration, and scope before changing anything."
		case "planning":
			return "Use Planning to define a narrow change scope and the expected knowledge impact before applying mutations."
		case "execution":
			return "Use Execution to apply the planned knowledge change with explicit validation and minimal blast radius."
		case "review":
			return "Use Review to confirm the final knowledge state and ensure the change stayed within the intended scope."
		default:
			return fmt.Sprintf("Use %s capability to work with spaces/knowledge bases", b.name)
		}
	case "desktop":
		switch b.phaseID {
		case "investigation":
			return "Use Investigation to inspect the workspace, environment, and relevant automation context before taking action."
		case "planning":
			return "Use Planning to outline the shell or automation steps explicitly before executing them."
		case "execution":
			return "Use Execution to run the planned automation step-by-step and retain the output for review."
		case "review":
			return "Use Review to confirm the automation outcome and capture any follow-up risk."
		default:
			return fmt.Sprintf("Use %s capability to work with desktop automation", b.name)
		}
	default:
		switch b.phaseID {
		case "intake":
			return "Use Intake to gather scope, constraints, and success criteria before acting."
		case "investigation":
			return "Use Investigation to gather evidence from the repository, tests, and surrounding code before making changes."
		case "planning":
			return "Use Planning to create a concrete design brief with constraints, trade-offs, and validation steps."
		case "testing":
			return "Use Testing to shape the change with the relevant validation surfaces and capture the outcome."
		case "execution":
			return "Use Execution to implement the plan safely and keep the change scoped to the observed need."
		case "review":
			return "Use Review to validate the implementation against the evidence and requirements."
		case "delivery":
			return "Use Delivery to summarize the result, decisions, and follow-up steps clearly."
		default:
			return fmt.Sprintf("Use %s capability to work through the current workflow phase", b.name)
		}
	}
}

func (b *BasePhase) BuildGuidanceProfile(query string, state capabilities.RoutingState) capabilities.GuidanceProfile {
	_ = query
	_ = state

	base := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityLight, fmt.Sprintf("You are the %s capability for %s.", b.name, b.description))
	base.Principles = []string{"Prefer evidence over assumption", "Keep the current phase focused on its responsibilities"}
	base.VerificationRules = []capabilities.GuidanceRule{{Description: "Capture the evidence or validation result that supports the current phase decision", AppliesTo: []string{b.phaseID}}}

	phaseRules := []capabilities.GuidanceRule{{Description: fmt.Sprintf("Operate as the %s phase capability and keep the current phase focused on its job.", b.name), AppliesTo: []string{b.phaseID}}}
	if rules, ok := b.authoringProfile.PhaseRules[b.phaseID]; ok && len(rules) > 0 {
		phaseRules = append([]capabilities.GuidanceRule{}, rules...)
	} else {
		switch b.phaseID {
		case "investigation":
			phaseRules = append(phaseRules, capabilities.GuidanceRule{Description: "Gather evidence before proposing a fix or change", AppliesTo: []string{"investigation"}})
		case "planning":
			phaseRules = append(phaseRules, capabilities.GuidanceRule{Description: "Create a concrete plan with constraints, dependencies, and validation steps", AppliesTo: []string{"planning"}})
		case "execution":
			phaseRules = append(phaseRules, capabilities.GuidanceRule{Description: "Implement only the scoped change and preserve the surrounding contract", AppliesTo: []string{"execution"}})
		case "review":
			phaseRules = append(phaseRules, capabilities.GuidanceRule{Description: "Validate the result against the evidence and surface remaining uncertainty", AppliesTo: []string{"review"}})
		case "delivery":
			phaseRules = append(phaseRules, capabilities.GuidanceRule{Description: "Summarize the outcome, decisions, and follow-up steps clearly", AppliesTo: []string{"delivery"}})
		}
	}

	var guardrails []capabilities.GuidanceRule
	if len(b.authoringProfile.Guardrails) > 0 {
		guardrails = append(guardrails, b.authoringProfile.Guardrails...)
	} else {
		switch b.domain {
		case "stores":
			base.Principles = append(base.Principles, "Never guess store names, field paths, or join mappings")
			guardrails = append(guardrails, capabilities.GuidanceRule{Description: "Do not mutate stores or assume schema without evidence", AppliesTo: []string{"stores"}})
		case "spaces":
			base.Principles = append(base.Principles, "Treat persisted knowledge changes as high-impact operations")
			guardrails = append(guardrails, capabilities.GuidanceRule{Description: "Do not refresh or delete large knowledge surfaces without explicit intent", AppliesTo: []string{"spaces"}})
		case "desktop":
			base.Principles = append(base.Principles, "Keep shell actions scoped to the requested workspace or repository")
			guardrails = append(guardrails, capabilities.GuidanceRule{Description: "Do not execute destructive commands without explicit user intent", AppliesTo: []string{"desktop"}})
		default:
			base.Principles = append(base.Principles, "Use the most relevant tools and evidence for the task")
		}
	}

	if len(b.authoringProfile.Principles) > 0 {
		base.Principles = append(base.Principles, b.authoringProfile.Principles...)
	}

	base.PhaseRules = phaseRules
	base.Guardrails = guardrails

	profile := capabilities.GuidanceProfile{}.
		ApplyDefaults(base)
	return profile
}

// Common helper methods

func (b *BasePhase) phaseToolName(suffix string) string {
	return b.phaseID + "_" + suffix
}

func (b *BasePhase) ensureSharedInfrastructure() *SharedInfrastructure {
	if b.sharedInfra == nil {
		b.sharedInfra = cw.NewDefaultSharedInfrastructure()
	}
	return b.sharedInfra
}

// Artifact management

type phaseArtifact struct {
	PhaseID             string         `json:"phase_id"`
	Summary             string         `json:"summary"`
	Status              string         `json:"status"`
	Requirements        []string       `json:"requirements,omitempty"`
	Plan                []string       `json:"plan,omitempty"`
	Notes               []string       `json:"notes,omitempty"`
	Evidence            []string       `json:"evidence,omitempty"`
	NextSteps           []string       `json:"next_steps,omitempty"`
	TransitionTo        string         `json:"transition_to,omitempty"`
	TransitionFrom      string         `json:"transition_from,omitempty"`
	TransitionDirection string         `json:"transition_direction,omitempty"`
	ExitRequested       bool           `json:"exit,omitempty"`
	StructuredData      map[string]any `json:"structured_data,omitempty"`
	Timestamp           int64          `json:"timestamp"`
}

func (b *BasePhase) runCapture(args map[string]any) (string, error) {
	summary, _ := args["summary"].(string)
	status, _ := args["status"].(string)

	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("summary is required")
	}
	if strings.TrimSpace(status) == "" {
		return "", fmt.Errorf("status is required")
	}

	// Build artifact first to validate completion criteria
	artifact := phaseArtifact{
		PhaseID:   b.phaseID,
		Summary:   summary,
		Status:    status,
		Timestamp: time.Now().Unix(),
	}

	if reqs, ok := args["requirements"].([]any); ok {
		for _, r := range reqs {
			if s, ok := r.(string); ok {
				artifact.Requirements = append(artifact.Requirements, s)
			}
		}
	}
	if plan, ok := args["plan"].([]any); ok {
		for _, p := range plan {
			if s, ok := p.(string); ok {
				artifact.Plan = append(artifact.Plan, s)
			}
		}
	}
	if notes, ok := args["notes"].([]any); ok {
		for _, n := range notes {
			if s, ok := n.(string); ok {
				artifact.Notes = append(artifact.Notes, s)
			}
		}
	}
	if evidence, ok := args["evidence"].([]any); ok {
		for _, e := range evidence {
			if s, ok := e.(string); ok {
				artifact.Evidence = append(artifact.Evidence, s)
			}
		}
	}
	if nextSteps, ok := args["next_steps"].([]any); ok {
		for _, ns := range nextSteps {
			if s, ok := ns.(string); ok {
				artifact.NextSteps = append(artifact.NextSteps, s)
			}
		}
	}

	if transTo, ok := args["transition_to"].(string); ok {
		artifact.TransitionTo = transTo
	}
	if transFrom, ok := args["transition_from"].(string); ok {
		artifact.TransitionFrom = transFrom
	}
	if transDir, ok := args["transition_direction"].(string); ok {
		artifact.TransitionDirection = transDir
	}
	if exit, ok := args["exit"].(bool); ok {
		artifact.ExitRequested = exit
	}
	if structData, ok := args["structured_data"].(map[string]any); ok {
		artifact.StructuredData = structData
	}

	// Validate phase completion criteria before allowing status="complete"
	if err := b.validateCompletionCriteria(artifact); err != nil {
		return "", err
	}

	artifactJSON, err := json.Marshal(artifact)
	if err != nil {
		return "", fmt.Errorf("failed to serialize artifact: %w", err)
	}

	storage := b.ensureSharedInfrastructure()
	if storage.Memory == nil {
		return "", fmt.Errorf("memory storage not available")
	}

	key := "phase-artifact:" + b.phaseID
	err = storage.Memory.Save(key, map[string]string{
		"artifact_json": string(artifactJSON),
		"phase_id":      b.phaseID,
		"status":        status,
		"summary":       summary,
		"timestamp":     fmt.Sprintf("%d", artifact.Timestamp),
	})
	if err != nil {
		return "", fmt.Errorf("failed to persist artifact: %w", err)
	}

	return fmt.Sprintf("✓ %s artifact captured (status: %s)", b.name, status), nil
}

func (b *BasePhase) runState(args map[string]any) (string, error) {
	_ = args
	artifact, err := b.loadExistingArtifact()
	if err != nil {
		return "", err
	}
	if artifact.PhaseID == "" {
		return fmt.Sprintf("No %s artifact found. This is a fresh start.", b.name), nil
	}

	artifactJSON, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to serialize artifact: %w", err)
	}

	return string(artifactJSON), nil
}

func shouldRequireRootCauseEvidence(artifact phaseArtifact) bool {
	summary := strings.ToLower(strings.TrimSpace(artifact.Summary))
	if summary == "" {
		return false
	}
	issueSignals := []string{"issue", "problem", "failure", "failing", "failed", "bug", "error", "regression", "broken", "incorrect", "wrong", "crash", "exception", "fault", "incident", "misbehavior"}
	for _, signal := range issueSignals {
		if strings.Contains(summary, signal) {
			return true
		}
	}
	for _, step := range artifact.Plan {
		stepText := strings.ToLower(step)
		for _, signal := range issueSignals {
			if strings.Contains(stepText, signal) {
				return true
			}
		}
	}
	return false
}

func requireRootCauseEvidence(artifact phaseArtifact, workstream WorkStreamContextReader) error {
	// Check workstream state first - this is the source of truth
	if workstream != nil {
		phaseArtifacts := workstream.GetPhaseArtifacts()

		// Check if investigation phase has captured findings
		if invArtifacts, hasInv := phaseArtifacts["investigation"]; hasInv && len(invArtifacts) > 0 {
			// Investigation phase has run and captured artifacts - that's sufficient
			return nil
		}

		// Check if any phase has captured test results or error evidence
		for _, artifacts := range phaseArtifacts {
			if testResults, hasTest := artifacts["test_results"]; hasTest && testResults != nil {
				return nil
			}
			if evidence, hasEvidence := artifacts["evidence"]; hasEvidence {
				if evidenceSlice, ok := evidence.([]string); ok && len(evidenceSlice) > 0 {
					return nil
				}
			}
		}
	}

	// Fall back to artifact content checking (more lenient than before)
	evidenceText := strings.ToLower(strings.Join(artifact.Evidence, " "))

	// Accept test failure evidence
	hasTestEvidence := (strings.Contains(evidenceText, "test") || strings.Contains(evidenceText, "fail")) &&
		(strings.Contains(evidenceText, "error") ||
			strings.Contains(evidenceText, "panic") ||
			strings.Contains(evidenceText, "expected"))

	// Accept any substantive error evidence
	hasErrorEvidence := len(artifact.Evidence) > 0 && (strings.Contains(evidenceText, "error") ||
		strings.Contains(evidenceText, "fail") ||
		strings.Contains(evidenceText, "stack trace") ||
		strings.Contains(evidenceText, "exception"))

	// Accept investigation structured data
	hasInvestigationData := false
	if artifact.StructuredData != nil {
		if _, hasInv := artifact.StructuredData["investigation_summary"]; hasInv {
			hasInvestigationData = true
		}
		if _, hasTest := artifact.StructuredData["test_results"]; hasTest {
			hasInvestigationData = true
		}
		// Accept any plan with structured execution details (shows work was done)
		if _, hasPlan := artifact.StructuredData["detailed_execution_plan"]; hasPlan {
			hasInvestigationData = true
		}
	}

	// If there's ANY evidence at all, accept it (be lenient)
	if len(artifact.Evidence) > 0 || hasTestEvidence || hasErrorEvidence || hasInvestigationData {
		return nil
	}

	// ADVISORY MODE: Log warning but allow progression
	// The LLM can self-correct by calling investigation tools if it needs more context
	// Blocking here creates loops where LLM searches randomly to satisfy validation
	summaryText := strings.ToLower(strings.TrimSpace(artifact.Summary))
	if shouldRequireRootCauseEvidence(artifact) {
		log_slog.Info("Planning without investigation evidence - LLM may need to call investigation tools for context",
			"summary", summaryText,
			"has_workstream_artifacts", workstream != nil && len(workstream.GetPhaseArtifacts()) > 0)
	}

	// Always allow progression - trust the LLM to orchestrate
	return nil
}

// validateCompletionCriteria checks if phase-specific success criteria are met
// before allowing the phase to be marked as complete.
func (b *BasePhase) validateCompletionCriteria(artifact phaseArtifact) error {
	status := strings.ToLower(strings.TrimSpace(artifact.Status))

	// Only validate when marking phase as complete
	if status != "complete" && status != "approved" && status != "delivered" && status != "ready_for_execution" {
		return nil // In-progress statuses don't need validation
	}

	// Get workstream context for state-based validation
	var workstream WorkStreamContextReader
	if b.sharedInfra != nil && b.sharedInfra.STMAccessor != nil {
		if stm := b.sharedInfra.STMAccessor(); stm != nil {
			if thread := stm.GetCurrentThread(); thread != nil {
				workstream = thread.GetStreamContext()
			}
		}
	}

	if shouldRequireRootCauseEvidence(artifact) {
		if err := requireRootCauseEvidence(artifact, workstream); err != nil {
			return err
		}
	}

	// Phase-specific validation
	switch b.phaseID {
	case "intake":
		// Intake phase should document requirements - ADVISORY
		if len(artifact.Requirements) == 0 {
			log_slog.Info("Intake phase completion without requirements array",
				"summary", strings.TrimSpace(artifact.Summary),
				"status", artifact.Status)
		}
		if strings.TrimSpace(artifact.Summary) == "" {
			log_slog.Info("Intake phase completion without summary",
				"has_requirements", len(artifact.Requirements) > 0,
				"status", artifact.Status)
		}

	case "investigation":
		// Investigation should gather evidence - ADVISORY
		if len(artifact.Evidence) < 2 {
			log_slog.Info("Investigation phase completion with minimal evidence",
				"evidence_count", len(artifact.Evidence),
				"summary", strings.TrimSpace(artifact.Summary))
		}
		if strings.TrimSpace(artifact.Summary) == "" {
			log_slog.Info("Investigation phase completion without summary",
				"evidence_count", len(artifact.Evidence),
				"status", artifact.Status)
		}

	case "planning":
		// Planning should create an implementation plan - ADVISORY
		if len(artifact.Plan) == 0 {
			log_slog.Info("Planning phase completion without plan array - LLM may self-organize via tool calls",
				"summary", strings.TrimSpace(artifact.Summary),
				"has_next_steps", len(artifact.NextSteps) > 0)
		}
		if strings.TrimSpace(artifact.Summary) == "" {
			log_slog.Info("Planning phase completion without summary",
				"has_plan", len(artifact.Plan) > 0,
				"status", artifact.Status)
		}

	case "testing":
		// Testing should document results - ADVISORY
		if strings.TrimSpace(artifact.Summary) == "" {
			log_slog.Info("Testing phase completion without summary",
				"status", artifact.Status,
				"has_evidence", len(artifact.Evidence) > 0)
		}

	case "execution":
		// Execution should document changes - ADVISORY
		if strings.TrimSpace(artifact.Summary) == "" {
			log_slog.Info("Execution phase completion without summary",
				"status", artifact.Status,
				"has_plan", len(artifact.Plan) > 0)
		}

	case "review":
		// Review should use "approved" status - ADVISORY
		if status == "complete" {
			log_slog.Info("Review phase using 'complete' instead of 'approved' status",
				"summary", strings.TrimSpace(artifact.Summary))
		}

	case "delivery":
		// Delivery should have final summary - ADVISORY
		if strings.TrimSpace(artifact.Summary) == "" {
			log_slog.Info("Delivery phase completion without summary",
				"status", artifact.Status)
		}
	}

	return nil
}

func (b *BasePhase) loadExistingArtifact() (phaseArtifact, error) {
	storage := b.ensureSharedInfrastructure()
	if storage.Memory == nil {
		return phaseArtifact{}, fmt.Errorf("memory storage not available")
	}

	values, err := storage.Memory.Load("phase-artifact:" + b.phaseID)
	if err != nil {
		return phaseArtifact{}, nil // No artifact is not an error
	}

	artifactJSON := values["artifact_json"]
	if artifactJSON == "" {
		return phaseArtifact{}, nil
	}

	var artifact phaseArtifact
	if err := json.Unmarshal([]byte(artifactJSON), &artifact); err != nil {
		return phaseArtifact{}, fmt.Errorf("failed to deserialize artifact: %w", err)
	}

	return artifact, nil
}

// Common tool schemas

func captureToolSchema() string {
	return `{
  "type": "object",
  "required": ["summary", "status"],
  "properties": {
    "summary": {"type": "string", "description": "One-sentence phase summary (required)"},
    "status": {"type": "string", "description": "Phase status: exploring|drafting|refining|ready_for_execution|complete|blocked|approved|delivered"},
    "requirements": {"type": "array", "items": {"type": "string"}, "description": "List of requirements (from intake phase)"},
    "plan": {"type": "array", "items": {"type": "string"}, "description": "Ordered implementation steps (concrete, file-specific)"},
    "notes": {"type": "array", "items": {"type": "string"}, "description": "Constraints, risks, considerations"},
    "evidence": {"type": "array", "items": {"type": "string"}, "description": "Findings, patterns, references"},
    "next_steps": {"type": "array", "items": {"type": "string"}, "description": "Guidance for next phase"},
    "transition_to": {"type": "string", "description": "Next phase ID: intake|investigation|planning|execution|review|delivery"},
    "transition_from": {"type": "string", "description": "Previous phase ID"},
    "transition_direction": {"type": "string", "description": "Transition direction: forward|backward"},
    "exit": {"type": "boolean", "description": "Set true to signal phase completion and workflow exit"},
    "structured_data": {"type": "object", "description": "Phase-specific structured data (test results, findings objects, metrics, etc.)"}
  }
}`
}

func stateToolSchema() string {
	return `{
  "type": "object",
  "properties": {},
  "description": "No arguments required. Returns current phase artifact if it exists."
}`
}

// Common classification helpers

func detectStartingPhase(queryLower string) string {
	// Review-oriented tasks
	reviewSignals := []string{
		"review code", "code review", "audit", "check for", "verify that",
		"validate that", "ensure that", "confirm that",
	}
	for _, signal := range reviewSignals {
		if strings.Contains(queryLower, signal) {
			return "review"
		}
	}

	// Investigation-oriented tasks
	investigationSignals := []string{
		"fix", "debug", "error", "failing", "failed", "broken", "issue", "problem",
		"not working", "doesn't work", "won't", "can't", "crash", "exception",
		"incorrect", "wrong", "unexpected", "why", "what's wrong", "troubleshoot",
		"fix bug", "find bug", "bug in", "bug with",
	}
	for _, signal := range investigationSignals {
		if strings.Contains(queryLower, signal) {
			return "investigation"
		}
	}

	// Planning-oriented tasks
	planningSignals := []string{
		"design", "architect", "how should", "approach for", "strategy for",
		"best way to", "should i", "how to implement",
	}
	for _, signal := range planningSignals {
		if strings.Contains(queryLower, signal) {
			return "planning"
		}
	}

	// Direct execution tasks
	executionSignals := []string{
		"apply this patch", "update the version", "change the config",
		"set the value", "add this line", "remove this code",
	}
	for _, signal := range executionSignals {
		if strings.Contains(queryLower, signal) {
			return "execution"
		}
	}

	// Default to intake
	return "intake"
}

func phaseKeywords(phaseID string) []string {
	switch phaseID {
	case "intake":
		return []string{"requirement", "requirements", "context", "goal", "scope", "clarify", "ask", "understand"}
	case "investigation":
		return []string{"inspect", "investigate", "analy", "debug", "trace", "error", "repo", "search"}
	case "planning":
		return []string{"plan", "design", "architecture", "approach", "proposal", "strategy"}
	case "execution":
		return []string{"implement", "execute", "patch", "modify", "write", "change", "fix"}
	case "testing":
		return []string{"test", "testing", "verify", "validate", "assert", "coverage", "regression"}
	case "review":
		return []string{"review", "validate", "verify", "check", "test", "quality"}
	case "delivery":
		return []string{"deliver", "handoff", "summarize", "summary", "report", "complete"}
	default:
		return nil
	}
}

func phaseMetadata(phaseID string) (name, description string) {
	switch phaseID {
	case "intake":
		return "Intake & Context", "requirements gathering and context assembly"
	case "investigation":
		return "Investigation & Analysis", "investigation and technical analysis"
	case "planning":
		return "Planning & Design", "planning and design for implementation"
	case "execution":
		return "Execution & Implementation", "implementation and execution"
	case "testing":
		return "Testing & Validation", "test-first validation and regression checks"
	case "review":
		return "Review & Validation", "validation, verification, and review"
	case "delivery":
		return "Delivery & Handoff", "delivery summarization and handoff"
	default:
		return phaseID, phaseID
	}
}
