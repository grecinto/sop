package codingworkflow

import (
	"context"
	"fmt"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	PlannerCapabilityID   = "dynamic_planner"
	PlannerCapabilityName = "Dynamic Workflow Planner"
)

// PlannerCapability uses task analysis to dynamically generate custom workflow steps.
// It participates in the Ask flow by analyzing the task, generating a plan, and
// enqueueing work items for each step to be executed by the Ask pipeline.
//
// Implements WorkflowPlanner interface: Run() generates plan, OnFinish() sets up workflow.
type PlannerCapability struct {
	orchestrator    *Orchestrator
	workflowManager *WorkflowManager // Injected or created for workflow execution
}

// NewPlannerCapability creates a new dynamic planning capability
func NewPlannerCapability(orchestrator *Orchestrator) *PlannerCapability {
	return &PlannerCapability{
		orchestrator:    orchestrator,
		workflowManager: NewWorkflowManager(orchestrator), // Create workflow manager for execution
	}
}

func (c *PlannerCapability) ID() string   { return PlannerCapabilityID }
func (c *PlannerCapability) Name() string { return PlannerCapabilityName }

func (c *PlannerCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{
			{
				Resource: "filesystem",
				Actions:  []string{"read", "write", "create", "delete"},
				Gate:     capabilities.GateWriteAccess,
			},
			{
				Resource: "shell",
				Actions:  []string{"read"},
				Gate:     capabilities.GateReadAccess,
			},
		},
	}
}

func (c *PlannerCapability) CanHandle(query string, state capabilities.RoutingState) bool {
	if c == nil || c.orchestrator == nil {
		return false
	}

	// Check capability hint
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "dynamic_planner" || hint == "planner" {
			return true
		}
		// Don't handle if another capability is explicitly requested
		if hint != "" {
			return false
		}
	}

	// This capability is opt-in via explicit routing or capability hint
	// Default to false to avoid conflicting with WorkflowCapability
	return false
}

func (c *PlannerCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return `Dynamic Workflow Planner Context:
- Analyzes coding tasks and generates custom workflow plans
- Each plan consists of a sequence of Steps tailored to the specific task
- Steps are executed through WorkflowManager which participates with Ask pipeline
- Plans adapt to codebase structure, available tools, and task complexity
- Uses LLM analysis to determine optimal step sequence and dependencies`
}

func (c *PlannerCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return `Dynamic Planning Instructions:
1. Analyze the task requirements, context, and constraints
2. Generate a custom workflow plan as a sequence of Steps
3. Each Step should have clear:
   - Name: Descriptive step name
   - Description: What the step accomplishes
   - Type: Step type (analyze, generate, apply, verify, research)
   - Dependencies: What the step needs from previous steps
4. Hand the plan to WorkflowManager for execution
5. WorkflowManager participates in Ask pipeline by enqueueing steps

Available Step Types:
- analyze: Analyze code, errors, or requirements
- generate: Generate code, patches, or documentation
- apply: Apply changes to files
- verify: Run tests or validate changes
- research: Search codebase or knowledge bases
- design: Design architecture or approach

` + CodeFormatInstructions
}

func (c *PlannerCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{
		Name:        "dynamic_planner",
		Description: "Analyzes coding task and generates custom workflow plan with tailored Steps",
		Schema:      "{}",
	}}
}

// Run implements the Capability interface
func (c *PlannerCapability) Run(query string, state capabilities.RoutingState) (capabilities.CapabilityResult, error) {
	// Gather task context
	taskContext := c.gatherTaskContext(query, state)

	// Generate plan
	plan, err := c.generatePlan(context.Background(), query, taskContext)
	if err != nil {
		return capabilities.CapabilityResult{}, fmt.Errorf("plan generation failed: %w", err)
	}

	if plan == nil || len(plan.Steps) == 0 {
		return capabilities.CapabilityResult{}, fmt.Errorf("generated plan is empty")
	}

	// Format plan as summary
	summary := c.formatPlanSummary(plan)

	// Return plan - OnFinish() will be called after to set up workflow execution
	return capabilities.CapabilityResult{
		Summary: summary,
		Data: map[string]any{
			"plan":           plan,
			"steps":          plan.Steps,
			"original_query": query,
			"workflow_ready": true,
		},
	}, nil
}

// OnFinish implements the WorkflowPlanner interface
// Called after Run() completes, near the end of the Ask/ReAct loop.
// Returns workflow steps in result data for Ask pipeline to enqueue.
func (c *PlannerCapability) OnFinish(ctx context.Context, result capabilities.CapabilityResult) error {
	// Extract plan from result
	plan, ok := result.Data["plan"].(*WorkflowPlan)
	if !ok || plan == nil {
		return fmt.Errorf("no workflow plan found in result")
	}

	// Convert plan to concrete Steps
	steps, err := c.planToSteps(plan)
	if err != nil {
		return fmt.Errorf("failed to convert plan to steps: %w", err)
	}

	// Store steps back in result data for Ask pipeline to process
	// The Ask pipeline will detect this and enqueue the workflow trigger
	result.Data["workflow_steps_ready"] = steps
	result.Data["workflow_trigger_needed"] = true

	log.Info("Workflow steps prepared for execution",
		"workflow", plan.Name,
		"steps", len(steps))

	return nil
}

// gatherTaskContext collects relevant context for plan generation
func (c *PlannerCapability) gatherTaskContext(query string, state capabilities.RoutingState) string {
	var contextParts []string

	// Add task classification
	classifier := DefaultTaskClassifier()
	classification := classifier.Classify(query, "", "")
	contextParts = append(contextParts, fmt.Sprintf("Task Type: %s (confidence: %.2f)", classification.TaskType, classification.Confidence))

	// Add detected signals
	if len(classification.Signals) > 0 {
		contextParts = append(contextParts, fmt.Sprintf("Signals: %v", classification.Signals))
	}

	// Add KB recommendations
	if len(classification.KBs) > 0 {
		kbNames := make([]string, len(classification.KBs))
		for i, kb := range classification.KBs {
			kbNames[i] = fmt.Sprintf("%s (%.2f)", kb.Name, kb.Confidence)
		}
		contextParts = append(contextParts, fmt.Sprintf("Recommended KBs: %v", kbNames))
	}

	// Add capability hint if present
	if state.CapabilityHint != "" {
		contextParts = append(contextParts, fmt.Sprintf("Capability Hint: %s", state.CapabilityHint))
	}

	return strings.Join(contextParts, "\n")
}

// WorkflowPlan represents a dynamically generated workflow plan
type WorkflowPlan struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Steps       []PlanStep `json:"steps"`
	Rationale   string     `json:"rationale"`
}

// PlanStep represents a step in a dynamic plan
type PlanStep struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Type         string   `json:"type"` // analyze, generate, apply, verify, research, design
	Dependencies []string `json:"dependencies"`
	Tags         []string `json:"tags"`
}

// generatePlan creates a custom workflow plan based on task analysis
// TODO: Enhance with LLM-based dynamic planning in future iteration
func (c *PlannerCapability) generatePlan(ctx context.Context, query string, context string) (*WorkflowPlan, error) {
	// For now, use heuristic-based planning
	// Future enhancement: Use LLM via Generator interface for dynamic plan generation
	return c.createFallbackPlan(query, context), nil
}

// createFallbackPlan generates a plan based on task classification heuristics
func (c *PlannerCapability) createFallbackPlan(query string, context string) *WorkflowPlan {
	// Parse task type from context
	taskType := "implementation"
	if strings.Contains(strings.ToLower(query), "fix") || strings.Contains(strings.ToLower(query), "debug") {
		taskType = "debugging"
	} else if strings.Contains(strings.ToLower(query), "test") {
		taskType = "testing"
	} else if strings.Contains(strings.ToLower(query), "refactor") {
		taskType = "refactoring"
	}

	// Create basic plan based on task type
	plan := &WorkflowPlan{
		Name:        fmt.Sprintf("%s Workflow", strings.Title(taskType)),
		Description: fmt.Sprintf("Generated fallback plan for %s task", taskType),
		Rationale:   "Using default workflow structure as LLM plan generation was unavailable",
	}

	// Add steps based on task type
	switch taskType {
	case "debugging":
		plan.Steps = []PlanStep{
			{Name: "Analyze Error", Description: "Analyze error messages and stack traces", Type: "analyze", Tags: []string{"error_analysis"}},
			{Name: "Locate Issue", Description: "Identify files and functions causing the issue", Type: "research", Dependencies: []string{"Analyze Error"}, Tags: []string{"code_search"}},
			{Name: "Generate Fix", Description: "Create patch to fix the issue", Type: "generate", Dependencies: []string{"Locate Issue"}, Tags: []string{"patch"}},
			{Name: "Apply Fix", Description: "Apply the generated patch", Type: "apply", Dependencies: []string{"Generate Fix"}, Tags: []string{"file_write"}},
			{Name: "Verify Fix", Description: "Run tests to verify fix", Type: "verify", Dependencies: []string{"Apply Fix"}, Tags: []string{"test"}},
		}
	case "testing":
		plan.Steps = []PlanStep{
			{Name: "Run Tests", Description: "Execute test suite", Type: "verify", Tags: []string{"test"}},
			{Name: "Analyze Failures", Description: "Analyze test failures", Type: "analyze", Dependencies: []string{"Run Tests"}, Tags: []string{"error_analysis"}},
			{Name: "Generate Fixes", Description: "Create patches for failing tests", Type: "generate", Dependencies: []string{"Analyze Failures"}, Tags: []string{"patch"}},
			{Name: "Apply Fixes", Description: "Apply patches", Type: "apply", Dependencies: []string{"Generate Fixes"}, Tags: []string{"file_write"}},
			{Name: "Verify Tests", Description: "Re-run tests to verify fixes", Type: "verify", Dependencies: []string{"Apply Fixes"}, Tags: []string{"test"}},
		}
	case "refactoring":
		plan.Steps = []PlanStep{
			{Name: "Analyze Code", Description: "Analyze code structure and patterns", Type: "analyze", Tags: []string{"code_analysis"}},
			{Name: "Plan Refactoring", Description: "Design refactoring approach", Type: "design", Dependencies: []string{"Analyze Code"}, Tags: []string{"planning"}},
			{Name: "Generate Changes", Description: "Create refactoring patches", Type: "generate", Dependencies: []string{"Plan Refactoring"}, Tags: []string{"patch"}},
			{Name: "Apply Changes", Description: "Apply refactoring", Type: "apply", Dependencies: []string{"Generate Changes"}, Tags: []string{"file_write"}},
		}
	default: // implementation
		plan.Steps = []PlanStep{
			{Name: "Design Approach", Description: "Design implementation approach", Type: "design", Tags: []string{"planning"}},
			{Name: "Research Context", Description: "Gather relevant code context", Type: "research", Dependencies: []string{"Design Approach"}, Tags: []string{"code_search"}},
			{Name: "Generate Code", Description: "Generate implementation code", Type: "generate", Dependencies: []string{"Research Context"}, Tags: []string{"codegen"}},
			{Name: "Apply Implementation", Description: "Apply generated code", Type: "apply", Dependencies: []string{"Generate Code"}, Tags: []string{"file_write"}},
			{Name: "Verify Implementation", Description: "Run tests to verify", Type: "verify", Dependencies: []string{"Apply Implementation"}, Tags: []string{"test"}},
		}
	}

	return plan
}

// planToSteps converts a WorkflowPlan to concrete Step implementations
func (c *PlannerCapability) planToSteps(plan *WorkflowPlan) ([]Step, error) {
	if plan == nil || len(plan.Steps) == 0 {
		return nil, fmt.Errorf("empty workflow plan")
	}

	steps := make([]Step, 0, len(plan.Steps))

	for i, planStep := range plan.Steps {
		// Create a concrete Step based on the plan step type
		var step Step

		switch planStep.Type {
		case "analyze":
			step = NewAnalyzeCodeStep(planStep.Description)
		case "generate":
			step = NewGeneratePatchStep(planStep.Description)
		case "apply":
			step = NewApplyFixStep(nil)
		case "verify":
			// Determine if this is a test verification
			if containsTag(planStep.Tags, "test") {
				step = NewVerifyFixStep(".")
			} else {
				// Generic verification step
				step = NewAnalyzeCodeStep(fmt.Sprintf("Verify: %s", planStep.Description))
			}
		case "research":
			// Create a research step (analyze with research focus)
			step = NewAnalyzeCodeStep(fmt.Sprintf("Research: %s", planStep.Description))
		case "design":
			step = NewDesignStep(planStep.Description)
		default:
			// Unknown type, create as analysis step
			step = NewAnalyzeCodeStep(fmt.Sprintf("%s: %s", planStep.Name, planStep.Description))
		}

		// Enrich step metadata with plan information
		metadata := step.Metadata()
		metadata.Tags = append(metadata.Tags, planStep.Tags...)
		metadata.Description = fmt.Sprintf("[%d/%d] %s", i+1, len(plan.Steps), planStep.Description)

		steps = append(steps, step)
	}

	return steps, nil
}

// containsTag checks if a tag exists in a tag slice
func containsTag(tags []string, target string) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}

// buildStepQuery constructs a query for a specific plan step
func (c *PlannerCapability) buildStepQuery(step PlanStep, originalQuery string, stepNum, totalSteps int) string {
	// Build detailed query that includes step context
	var queryParts []string

	// Add step header
	queryParts = append(queryParts, fmt.Sprintf("[Step %d/%d: %s]", stepNum, totalSteps, step.Name))

	// Add step description
	queryParts = append(queryParts, step.Description)

	// Add original task context if relevant
	if stepNum == 1 {
		queryParts = append(queryParts, fmt.Sprintf("\nOriginal task: %s", originalQuery))
	}

	// Add dependencies if present
	if len(step.Dependencies) > 0 {
		queryParts = append(queryParts, fmt.Sprintf("\nDepends on: %s", strings.Join(step.Dependencies, ", ")))
	}

	// Add step type guidance
	switch step.Type {
	case "analyze":
		queryParts = append(queryParts, "\nAnalyze code structure, patterns, and identify relevant files.")
	case "generate":
		queryParts = append(queryParts, "\nGenerate code, patches, or implementation based on analysis.")
	case "apply":
		queryParts = append(queryParts, "\nApply changes to files. Verify before applying.")
	case "verify":
		queryParts = append(queryParts, "\nRun tests or validation to verify changes work correctly.")
	case "research":
		queryParts = append(queryParts, "\nSearch codebase and gather context needed for next steps.")
	case "design":
		queryParts = append(queryParts, "\nDesign approach or architecture before implementation.")
	}

	return strings.Join(queryParts, "\n")
}

// formatPlanSummary creates a human-readable plan summary
func (c *PlannerCapability) formatPlanSummary(plan *WorkflowPlan) string {
	var lines []string

	for i, step := range plan.Steps {
		line := fmt.Sprintf("  %d. [%s] %s", i+1, step.Type, step.Name)
		if len(step.Dependencies) > 0 {
			line += fmt.Sprintf(" (depends on: %s)", strings.Join(step.Dependencies, ", "))
		}
		lines = append(lines, line)
	}

	if plan.Rationale != "" {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Rationale: %s", plan.Rationale))
	}

	return strings.Join(lines, "\n")
}
