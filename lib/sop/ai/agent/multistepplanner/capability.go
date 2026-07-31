package multistepplanner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

const (
	MultiStepPlannerCapabilityID   = "multi_step_planner"
	MultiStepPlannerCapabilityName = "Multi-Step Planner"
	MaxSteps                       = 5 // Maximum number of steps allowed
)

// Local type definitions (mirrors types from agent package to avoid circular import)
type multiStepPlan struct {
	Steps            []stepDefinition `json:"steps"`
	CurrentStepIndex int              `json:"current_step_index"`
	CompletedSteps   []string         `json:"completed_steps"`
	RemainingSteps   []string         `json:"remaining_steps"`
	Reasoning        string           `json:"reasoning"`
	TotalSteps       int              `json:"total_steps"`
	CreatedAt        int64            `json:"created_at"`
}

type stepDefinition struct {
	StepID        string         `json:"step_id"`
	StepNumber    int            `json:"step_number"`
	Goal          string         `json:"goal"`
	Domain        string         `json:"domain"`
	PhasePlan     *phasePlan     `json:"phase_plan"`
	Status        string         `json:"status"`
	Prerequisites []string       `json:"prerequisites"`
	Artifacts     map[string]any `json:"artifacts"`
	StartedAt     int64          `json:"started_at"`
	CompletedAt   int64          `json:"completed_at"`
}

type phasePlan struct {
	PlannedPhases   []string        `json:"planned_phases"`
	RemainingPhases []string        `json:"remaining_phases"`
	CurrentIndex    int             `json:"current_index"`
	Reasoning       string          `json:"reasoning"`
	Confidence      string          `json:"confidence"`
	Checkpoints     map[string]bool `json:"checkpoints"`
	LastUpdated     int64           `json:"last_updated"`
}

type workItem struct {
	Actor   string         `json:"actor"`
	Query   string         `json:"query"`
	Context map[string]any `json:"context"`
}

// MultiStepPlannerCapability decomposes multi-goal queries into sequential steps.
// Each step gets its own goal, domain, and Phase Plan.
type MultiStepPlannerCapability struct {
	sharedInfra *cw.SharedInfrastructure
}

func NewMultiStepPlannerCapability(sharedInfra *cw.SharedInfrastructure) *MultiStepPlannerCapability {
	if sharedInfra == nil {
		sharedInfra = cw.NewDefaultSharedInfrastructure()
	}
	return &MultiStepPlannerCapability{sharedInfra: sharedInfra}
}

func (c *MultiStepPlannerCapability) ID() string   { return MultiStepPlannerCapabilityID }
func (c *MultiStepPlannerCapability) Name() string { return MultiStepPlannerCapabilityName }

func (c *MultiStepPlannerCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{{
			Resource: "memory",
			Actions:  []string{"read", "write"},
			Gate:     capabilities.GateReadAccess,
		}},
	}
}

func (c *MultiStepPlannerCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil {
		return capabilities.ClassificationResult{}
	}

	// Check for capability hint
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		switch hint {
		case "multi_step_planner", "multi_step", "multistep", "workflow":
			return capabilities.ClassificationResult{Relevant: true, Score: 20, Mode: capabilities.CapabilityModePrimary}
		}
	}

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return capabilities.ClassificationResult{}
	}

	// Strong multi-goal signals
	strongSignals := []string{
		" and then ", " after that ", " also ", " additionally ",
		" both ", " first ", " second ", " step 1", " step 2",
		" followed by ", " next ", " after ",
	}

	for _, signal := range strongSignals {
		if strings.Contains(q, signal) {
			return capabilities.ClassificationResult{
				Relevant: true,
				Score:    18, // Higher than Planner (16) for multi-goal queries
				Mode:     capabilities.CapabilityModePrimary,
			}
		}
	}

	// Weaker signal: " and " with multiple action verbs
	if strings.Contains(q, " and ") {
		verbCount := countActionVerbs(q)
		if verbCount >= 2 {
			return capabilities.ClassificationResult{
				Relevant: true,
				Score:    15,
				Mode:     capabilities.CapabilityModePrimary,
			}
		}
	}

	return capabilities.ClassificationResult{}
}

func (c *MultiStepPlannerCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Multi-Step Planner: Decomposes complex queries with multiple goals into sequential steps. Each step gets its own Phase Plan and can use different domains (stores, coding, spaces, desktop)."
}

func (c *MultiStepPlannerCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return fmt.Sprintf(`For queries with multiple distinct goals, use the decompose_multi_step_plan tool to break them into sequential steps.

Guidelines:
- Each step should have ONE clear goal
- Assign appropriate domain to each step (stores, coding_workflow, spaces, desktop)
- Estimate phases needed for each step (investigation, planning, execution, review, delivery)
- Maximum %d steps allowed
- Steps execute sequentially (Step 2 starts after Step 1 completes)
- Later steps can access artifacts from earlier steps

Example: "Create a store AND generate report"
  Step 1: Create store (domain: stores, phases: [investigation, planning, execution, review])
  Step 2: Generate report (domain: stores, phases: [investigation, execution])`, MaxSteps)
}

func (c *MultiStepPlannerCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{
		{
			Name:        "decompose_multi_step_plan",
			Description: fmt.Sprintf("Decompose a multi-goal query into N sequential steps (max %d). Each step has a goal, domain, and estimated phases.", MaxSteps),
			Tags:        []string{"C"}, // Creates a multi-step plan
			Schema: `{
				"type": "object",
				"properties": {
					"steps": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"step_number": {"type": "integer", "description": "Step number (1, 2, 3...)"},
								"goal": {"type": "string", "description": "Clear goal for this step"},
								"domain": {"type": "string", "description": "Domain: stores, coding_workflow, spaces, desktop"},
								"estimated_phases": {
									"type": "array", 
									"items": {"type": "string"},
									"description": "Phases needed: investigation, planning, execution, review, delivery"
								},
								"reasoning": {"type": "string", "description": "Why these phases for this goal"}
							},
							"required": ["step_number", "goal", "domain", "estimated_phases"]
						}
					},
					"reasoning": {"type": "string", "description": "Why this decomposition"},
					"confidence": {"type": "string", "description": "high, medium, low"}
				},
				"required": ["steps", "reasoning"]
			}`,
		},
	}
}

func (c *MultiStepPlannerCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	if toolName == "decompose_multi_step_plan" {
		return c.decomposeSteps(ctx, args)
	}
	return "", fmt.Errorf("unknown tool: %s", toolName)
}

func (c *MultiStepPlannerCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	return nil
}

func (c *MultiStepPlannerCapability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	return nil
}

func (c *MultiStepPlannerCapability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	return nil
}

// decomposeSteps decomposes a multi-goal query into sequential steps
func (c *MultiStepPlannerCapability) decomposeSteps(ctx context.Context, args map[string]any) (string, error) {
	// Parse steps from args
	stepsRaw, ok := args["steps"].([]any)
	if !ok {
		return "", fmt.Errorf("steps must be an array")
	}

	if len(stepsRaw) > MaxSteps {
		return "", fmt.Errorf("too many steps: %d (max %d allowed)", len(stepsRaw), MaxSteps)
	}

	if len(stepsRaw) == 0 {
		return "", fmt.Errorf("at least one step required")
	}

	steps := make([]stepDefinition, 0, len(stepsRaw))
	for _, stepRaw := range stepsRaw {
		stepMap, ok := stepRaw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("each step must be an object")
		}

		stepNum, ok := stepMap["step_number"].(float64)
		if !ok {
			return "", fmt.Errorf("step_number must be a number")
		}

		goal, ok := stepMap["goal"].(string)
		if !ok || goal == "" {
			return "", fmt.Errorf("goal must be a non-empty string")
		}

		domain, ok := stepMap["domain"].(string)
		if !ok || domain == "" {
			return "", fmt.Errorf("domain must be a non-empty string")
		}

		step := stepDefinition{
			StepID:     fmt.Sprintf("step_%d", int(stepNum)),
			StepNumber: int(stepNum),
			Goal:       goal,
			Domain:     strings.ToLower(domain),
			Status:     "pending",
		}

		// Parse estimated phases
		phasesRaw, ok := stepMap["estimated_phases"].([]any)
		if !ok || len(phasesRaw) == 0 {
			return "", fmt.Errorf("estimated_phases must be a non-empty array for step %d", step.StepNumber)
		}

		phases := make([]string, len(phasesRaw))
		for i, p := range phasesRaw {
			phaseStr, ok := p.(string)
			if !ok {
				return "", fmt.Errorf("phase must be a string in step %d", step.StepNumber)
			}
			phases[i] = strings.ToLower(phaseStr)
		}

		// Create PhasePlan for this step
		reasoning := ""
		if r, ok := stepMap["reasoning"].(string); ok {
			reasoning = r
		}

		step.PhasePlan = &phasePlan{
			PlannedPhases:   phases,
			RemainingPhases: phases,
			CurrentIndex:    0,
			Reasoning:       reasoning,
			Confidence:      "medium",
			LastUpdated:     time.Now().Unix(),
		}

		steps = append(steps, step)
	}

	// Get reasoning (confidence not used in MultiStepPlan)
	reasoning := ""
	if r, ok := args["reasoning"].(string); ok {
		reasoning = r
	}

	_ = args["confidence"] // Acknowledged but not used

	// Create MultiStepPlan
	multiStepPlan := &multiStepPlan{
		Steps:            steps,
		CurrentStepIndex: 0,
		CompletedSteps:   []string{},
		RemainingSteps:   collectStepIDs(steps),
		Reasoning:        reasoning,
		TotalSteps:       len(steps),
		CreatedAt:        time.Now().Unix(),
	}

	// Store in WorkStreamContext via STMReader
	if c.sharedInfra == nil || c.sharedInfra.STMAccessor == nil {
		return "", fmt.Errorf("STM not available")
	}

	stm := c.sharedInfra.STMAccessor()
	if err := stm.StoreMultiStepPlan(multiStepPlan); err != nil {
		return "", fmt.Errorf("failed to store multi-step plan: %w", err)
	}

	// Enqueue first step's first phase
	firstStep := steps[0]
	if len(firstStep.PhasePlan.PlannedPhases) == 0 {
		return "", fmt.Errorf("first step has no phases planned")
	}

	firstPhase := firstStep.PhasePlan.PlannedPhases[0]
	queue := c.sharedInfra.GetSessionQueue()
	if queue == nil {
		return "", fmt.Errorf("session queue not available")
	}

	err := queue.Enqueue(workItem{
		Actor: fmt.Sprintf("phase:%s", firstPhase),
		Query: firstStep.Goal,
		Context: map[string]any{
			"step_id": firstStep.StepID,
			"domain":  firstStep.Domain,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to enqueue first phase: %w", err)
	}

	return formatMultiStepPlanSummary(multiStepPlan), nil
}

// formatMultiStepPlanSummary formats the plan for user display
func formatMultiStepPlanSummary(plan *multiStepPlan) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Created %d-step workflow:\n\n", plan.TotalSteps))

	for i, step := range plan.Steps {
		sb.WriteString(fmt.Sprintf("**Step %d**: %s\n", step.StepNumber, step.Goal))
		sb.WriteString(fmt.Sprintf("  - Domain: %s\n", step.Domain))
		sb.WriteString(fmt.Sprintf("  - Phases: %v\n", step.PhasePlan.PlannedPhases))
		if i < len(plan.Steps)-1 {
			sb.WriteString("\n")
		}
	}

	sb.WriteString(fmt.Sprintf("\n*Reasoning*: %s\n", plan.Reasoning))
	sb.WriteString(fmt.Sprintf("\nStarting Step 1: %s", plan.Steps[0].Goal))

	return sb.String()
}

// collectStepIDs extracts step IDs from steps
func collectStepIDs(steps []stepDefinition) []string {
	ids := make([]string, len(steps))
	for i, step := range steps {
		ids[i] = step.StepID
	}
	return ids
}

// countActionVerbs counts action verbs in the query
func countActionVerbs(query string) int {
	verbs := []string{
		"create", "generate", "build", "make", "update", "delete",
		"query", "find", "list", "show", "fix", "debug", "implement",
		"design", "run", "execute", "check", "verify", "test",
		"add", "remove", "modify", "change", "refactor",
	}
	count := 0
	for _, verb := range verbs {
		if strings.Contains(query, verb) {
			count++
		}
	}
	return count
}
