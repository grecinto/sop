package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// BasePhase provides common functionality shared by all domain-specific phase implementations.
// Domain phases embed this struct to inherit shared behavior (artifact storage, classification, etc.)
type BasePhase struct {
	sharedInfra            *SharedInfrastructure
	phaseID                string
	name                   string
	description            string
	domain                 string
	captureTool            string
	stateTool              string
	classificationStrategy PhaseClassificationStrategy
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
	case "review":
		return "Review & Validation", "validation, verification, and review"
	case "delivery":
		return "Delivery & Handoff", "delivery summarization and handoff"
	default:
		return phaseID, phaseID
	}
}
