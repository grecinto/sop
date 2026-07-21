package agent

// WorkflowDefinition represents a complete workflow stored in the KnowledgeBase.
// Workflows are stored as JSON at CategoryPaths like "Workflows/TestDebugging/Go/UnitTests".
type WorkflowDefinition struct {
	// Workflow identity
	ID           string `json:"workflow_id"`
	Name         string `json:"name"`
	CategoryPath string `json:"category_path"`
	Description  string `json:"description"`
	Version      string `json:"version,omitempty"`

	// Workflow behavior
	Steps       []WorkflowStep    `json:"steps"`
	ActorLimits map[string]int    `json:"actor_limits,omitempty"`
	Metadata    *WorkflowMetadata `json:"metadata,omitempty"`

	// Detection
	TriggerPatterns []string `json:"trigger_patterns,omitempty"`

	// Protocol guidance (injected into system prompt if needed)
	ProtocolGuidance string `json:"protocol_guidance,omitempty"`
}

// GetID returns the workflow ID (implements WorkflowDefinitionInterface).
func (w *WorkflowDefinition) GetID() string {
	return w.ID
}

// GetName returns the workflow name (implements WorkflowDefinitionInterface).
func (w *WorkflowDefinition) GetName() string {
	return w.Name
}

// WorkflowStep represents a single step in a workflow.
type WorkflowStep struct {
	Step          int      `json:"step"`
	Actor         string   `json:"actor"`
	Priority      int      `json:"priority"`
	QueryTemplate string   `json:"query_template"`
	ToolsExpected []string `json:"tools_expected,omitempty"`

	// Script optimization (Track B)
	ScriptTemplate      string               `json:"script_template,omitempty"`
	OptimizationProfile *OptimizationProfile `json:"optimization_profile,omitempty"`

	// Conditional execution
	Condition *StepCondition `json:"condition,omitempty"`
}

// OptimizationProfile defines how to optimize a step with scripting.
type OptimizationProfile struct {
	PreferScript  bool     `json:"prefer_script"`
	ScriptLibrary []string `json:"script_library,omitempty"`
	FallbackToLLM bool     `json:"fallback_to_llm"`
	MaxScriptTime int      `json:"max_script_time_ms,omitempty"`
}

// StepCondition defines when a step should execute.
type StepCondition struct {
	Type          string `json:"type"` // e.g., "tests_still_failing", "has_error_output"
	MaxIterations int    `json:"max_iterations,omitempty"`
}

// WorkflowMetadata stores additional workflow information.
type WorkflowMetadata struct {
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Language    string   `json:"language,omitempty"`
	Framework   string   `json:"framework,omitempty"`
	LastUpdated string   `json:"last_updated,omitempty"`
}

// ConditionEvaluator is a function that determines if a step should execute.
// It receives the turn result (from workqueue.TurnResult) and returns true if the condition is met.
type ConditionEvaluator func(result TurnResult) bool
