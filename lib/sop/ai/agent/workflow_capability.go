package agent

import (
	"context"
	"fmt"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// WorkflowManagerCapability wraps WorkflowManager as a Capability.
// It tracks workflow execution state and delegates to individual step capabilities.
//
// Deprecated: this capability belongs to the legacy WorkflowManager path.
// Keep it only as dormant compatibility code. PlannerCapability,
// MultiStepPlannerCapability, and the phase/workstream system replace it for
// active development.
type WorkflowManagerCapability struct {
	manager *WorkflowManager

	// Execution state (per-session)
	currentWorkflow  *WorkflowDefinition
	currentStepIndex int
	turnResults      []TurnResult
	originalQuery    string
	completed        bool
}

// NewWorkflowManagerCapability creates the deprecated workflow manager capability.
func NewWorkflowManagerCapability(manager *WorkflowManager) *WorkflowManagerCapability {
	return &WorkflowManagerCapability{
		manager:          manager,
		currentStepIndex: 0,
		turnResults:      make([]TurnResult, 0),
	}
}

// ID returns the capability identifier.
func (wmc *WorkflowManagerCapability) ID() string {
	return "kb_workflow"
}

// Name returns the human-readable capability name.
func (wmc *WorkflowManagerCapability) Name() string {
	return "KB Workflow Manager"
}

// Entitlement describes the capability's availability.
func (wmc *WorkflowManagerCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: true,
	}
}

// Classify checks whether there's a workflow matching the query.
func (wmc *WorkflowManagerCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	// If we're already executing a workflow, handle all queries until complete
	if wmc.currentWorkflow != nil && !wmc.completed {
		return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
	}

	// Check if capability hint matches
	if state.CapabilityHint == wmc.ID() || state.CapabilityHint == "workflow" {
		return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModeSupporting}
	}

	// Try to find a workflow (this is a quick check, actual execution happens in Run)
	// For now, return false to avoid expensive KB lookups during routing
	// KB-driven workflows are deprecated (2026-07-30) - Phase system is used instead
	return capabilities.ClassificationResult{}
}

// BuildContext returns context information for the LLM.
func (wmc *WorkflowManagerCapability) BuildContext(query string, state capabilities.RoutingState) string {
	if wmc.currentWorkflow == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Workflow Execution Context\n\n")
	sb.WriteString(fmt.Sprintf("**Workflow:** %s\n", wmc.currentWorkflow.Name))
	if wmc.currentWorkflow.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", wmc.currentWorkflow.Description))
	}

	if wmc.currentStepIndex < len(wmc.currentWorkflow.Steps) {
		step := wmc.currentWorkflow.Steps[wmc.currentStepIndex]
		sb.WriteString(fmt.Sprintf("\n**Current Step:** %d/%d\n", wmc.currentStepIndex+1, len(wmc.currentWorkflow.Steps)))
		if step.Title != "" {
			sb.WriteString(fmt.Sprintf("**Step Title:** %s\n", step.Title))
		}
		if step.Instructions != "" {
			sb.WriteString(fmt.Sprintf("**Instructions:** %s\n", step.Instructions))
		}
		if len(step.RequiredContext) > 0 {
			sb.WriteString(fmt.Sprintf("**Required Context:** %s\n", strings.Join(step.RequiredContext, ", ")))
		}
		if len(step.SuccessCriteria) > 0 {
			sb.WriteString(fmt.Sprintf("**Success Criteria:** %s\n", strings.Join(step.SuccessCriteria, ", ")))
		}
	}

	return sb.String()
}

// ExposeTools returns tools that this capability provides.
func (wmc *WorkflowManagerCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{
		{
			Name:        "execute_workflow",
			Description: "Execute a KB-driven workflow",
			Schema:      `{"workflow_query": "string describing the task"}`,
			Tags:        []string{"R", "U"}, // Reads workflow, updates execution state
		},
	}
}

// BuildInstructions returns instructions for the LLM.
func (wmc *WorkflowManagerCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	if wmc.currentWorkflow == nil {
		return "You are executing a knowledge-base driven workflow. Follow the workflow steps in order."
	}

	if wmc.currentStepIndex >= len(wmc.currentWorkflow.Steps) {
		return "Workflow execution complete. Summarize the results."
	}

	step := wmc.currentWorkflow.Steps[wmc.currentStepIndex]
	var sb strings.Builder

	sb.WriteString("You are executing step ")
	sb.WriteString(fmt.Sprintf("%d of %d", wmc.currentStepIndex+1, len(wmc.currentWorkflow.Steps)))
	sb.WriteString(" in the workflow.\n\n")

	if step.Instructions != "" {
		sb.WriteString("**Instructions:** ")
		sb.WriteString(step.Instructions)
		sb.WriteString("\n\n")
	}

	if len(step.ToolsExpected) > 0 {
		sb.WriteString("**Expected Tools:** ")
		sb.WriteString(strings.Join(step.ToolsExpected, ", "))
		sb.WriteString("\n\n")
	}

	if step.IterationPolicy != "" {
		sb.WriteString("**Iteration Policy:** ")
		sb.WriteString(step.IterationPolicy)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (wmc *WorkflowManagerCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (wmc *WorkflowManagerCapability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	_ = ctx
	_ = query
	_ = state
	return nil
}

func (wmc *WorkflowManagerCapability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	_ = ctx
	_ = query
	_ = state
	_ = result
	return nil
}

// RunWithQueue executes the current step and enqueues the next step to the queue.
// This is the queue-aware version for use with KB workflow execution.
func (wmc *WorkflowManagerCapability) RunWithQueue(ctx context.Context, query string, queue *WorkQueue) (capabilities.CapabilityResult, error) {
	// If no workflow is active, try to find one
	if wmc.currentWorkflow == nil {
		workflow, err := wmc.manager.getWorkflowInternal(ctx, query)
		if err != nil {
			return capabilities.CapabilityResult{}, fmt.Errorf("no workflow found: %w", err)
		}

		wmc.currentWorkflow = workflow
		wmc.currentStepIndex = 0
		wmc.originalQuery = query
		wmc.completed = false
		wmc.turnResults = make([]TurnResult, 0)

		log.Info("Workflow execution started (with queue)", "workflow", workflow.Name, "steps", len(workflow.Steps))

		// Enqueue the first step
		if err := wmc.manager.EnqueueNextStep(wmc.currentWorkflow, wmc.currentStepIndex, queue, wmc.originalQuery); err != nil {
			return capabilities.CapabilityResult{}, fmt.Errorf("failed to enqueue first step: %w", err)
		}

		return capabilities.CapabilityResult{
			Summary: fmt.Sprintf("Workflow '%s' started, first step enqueued", wmc.currentWorkflow.Name),
			Data: map[string]any{
				"workflow_started": true,
				"total_steps":      len(wmc.currentWorkflow.Steps),
			},
		}, nil
	}

	// Check if there are more steps
	if wmc.currentStepIndex+1 < len(wmc.currentWorkflow.Steps) {
		// Advance to next step
		wmc.currentStepIndex++

		// Enqueue the next step
		if err := wmc.manager.EnqueueNextStep(wmc.currentWorkflow, wmc.currentStepIndex, queue, wmc.originalQuery); err != nil {
			return capabilities.CapabilityResult{}, fmt.Errorf("failed to enqueue step %d: %w", wmc.currentStepIndex+1, err)
		}

		step := wmc.currentWorkflow.Steps[wmc.currentStepIndex]
		return capabilities.CapabilityResult{
			Summary: fmt.Sprintf("Step %d/%d enqueued: %s", wmc.currentStepIndex+1, len(wmc.currentWorkflow.Steps), step.Title),
			Data: map[string]any{
				"step_enqueued": true,
				"step_index":    wmc.currentStepIndex,
				"total_steps":   len(wmc.currentWorkflow.Steps),
			},
		}, nil
	}

	// Workflow complete
	wmc.completed = true
	return capabilities.CapabilityResult{
		Summary: fmt.Sprintf("Workflow '%s' completed", wmc.currentWorkflow.Name),
		Data: map[string]any{
			"workflow_complete": true,
			"steps_executed":    wmc.currentStepIndex + 1,
		},
	}, nil
}

// AdvanceStep moves to the next step in the workflow.
// This should be called after a step completes successfully.
func (wmc *WorkflowManagerCapability) AdvanceStep(turnResult TurnResult) bool {
	if wmc.currentWorkflow == nil || wmc.currentStepIndex >= len(wmc.currentWorkflow.Steps) {
		return false
	}

	step := wmc.currentWorkflow.Steps[wmc.currentStepIndex]

	// Record turn result
	wmc.turnResults = append(wmc.turnResults, turnResult)

	// Check step condition before advancing
	if step.Condition != nil {
		if evaluator, ok := wmc.manager.conditionRegistry[step.Condition.Type]; ok {
			if !evaluator(turnResult) {
				// Condition not met, stay on this step
				log.Info("Workflow step condition not met, retrying", "step", wmc.currentStepIndex+1)
				return false
			}
		}
	}

	// Advance to next step
	wmc.currentStepIndex++

	log.Info("Workflow step advanced",
		"step", wmc.currentStepIndex,
		"total", len(wmc.currentWorkflow.Steps),
		"completed", wmc.currentStepIndex >= len(wmc.currentWorkflow.Steps))

	return true
}

// Reset clears the workflow execution state.
func (wmc *WorkflowManagerCapability) Reset() {
	wmc.currentWorkflow = nil
	wmc.currentStepIndex = 0
	wmc.turnResults = nil
	wmc.originalQuery = ""
	wmc.completed = false
}

// WorkflowStepCapability represents a single workflow step as a capability.
type WorkflowStepCapability struct {
	step      WorkflowStep
	workflow  *WorkflowDefinition
	stepIndex int
}

// NewWorkflowStepCapability creates a capability for a single workflow step.
func NewWorkflowStepCapability(step WorkflowStep, workflow *WorkflowDefinition, stepIndex int) *WorkflowStepCapability {
	return &WorkflowStepCapability{
		step:      step,
		workflow:  workflow,
		stepIndex: stepIndex,
	}
}

// ID returns the step capability identifier.
func (wsc *WorkflowStepCapability) ID() string {
	return fmt.Sprintf("workflow_step_%s_%d", wsc.workflow.ID, wsc.stepIndex)
}

// Name returns the step name.
func (wsc *WorkflowStepCapability) Name() string {
	if wsc.step.Title != "" {
		return fmt.Sprintf("Step %d: %s", wsc.step.Step, wsc.step.Title)
	}
	return fmt.Sprintf("Step %d", wsc.step.Step)
}

// Entitlement describes the step's availability.
func (wsc *WorkflowStepCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: true,
	}
}

// CanHandle always returns true (step is part of workflow execution).
func (wsc *WorkflowStepCapability) CanHandle(query string, state capabilities.RoutingState) bool {
	return true
}

// Classify marks workflow-step capabilities as relevant for the current workflow execution.
func (wsc *WorkflowStepCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if wsc == nil {
		return capabilities.ClassificationResult{}
	}
	return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
}

// BuildContext returns context for this step.
func (wsc *WorkflowStepCapability) BuildContext(query string, state capabilities.RoutingState) string {
	var sb strings.Builder

	if wsc.workflow.ProtocolGuidance != "" {
		sb.WriteString("**Protocol Guidance:** ")
		sb.WriteString(wsc.workflow.ProtocolGuidance)
		sb.WriteString("\n\n")
	}

	if len(wsc.step.RequiredContext) > 0 {
		sb.WriteString("**Required Context:** ")
		sb.WriteString(strings.Join(wsc.step.RequiredContext, ", "))
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// ExposeTools returns empty (step uses workflow-level tools).
func (wsc *WorkflowStepCapability) ExposeTools() []capabilities.ToolDefinition {
	return nil
}

// BuildInstructions returns instructions for this step.
func (wsc *WorkflowStepCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	var sb strings.Builder

	if wsc.step.Instructions != "" {
		sb.WriteString(wsc.step.Instructions)
		sb.WriteString("\n\n")
	}

	if len(wsc.step.SuccessCriteria) > 0 {
		sb.WriteString("**Success Criteria:**\n")
		for _, criterion := range wsc.step.SuccessCriteria {
			sb.WriteString("- ")
			sb.WriteString(criterion)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(wsc.step.ToolsExpected) > 0 {
		sb.WriteString("**Expected Tools:** ")
		sb.WriteString(strings.Join(wsc.step.ToolsExpected, ", "))
		sb.WriteString("\n")
	}

	return sb.String()
}
