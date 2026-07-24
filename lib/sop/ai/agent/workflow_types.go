package agent

import (
	"fmt"
	"strings"
)

// WorkflowDefinition represents a complete workflow stored in the KnowledgeBase.
// Workflows are stored as JSON at CategoryPaths like "Workflows/TestDebugging/Go/UnitTests".
type WorkflowDefinition struct {
	// Workflow identity
	ID           string `json:"workflow_id"`
	Name         string `json:"name"`
	CategoryPath string `json:"category_path"`
	Description  string `json:"description"`
	Version      string `json:"version,omitempty"` // User-facing semantic version (e.g., "1.0.0", "2.1.0")

	// Publish state (for SA workflow management)
	PublishState         string `json:"publish_state,omitempty"`          // "unpublished", "published", "modified"
	LastPublishedVersion string `json:"last_published_version,omitempty"` // Last version pushed to HQ
	LastPublishedAt      string `json:"last_published_at,omitempty"`      // ISO8601 timestamp

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
	Title         string   `json:"title,omitempty"`
	Actor         string   `json:"actor"`
	Priority      int      `json:"priority"`
	QueryTemplate string   `json:"query_template"`
	ToolsExpected []string `json:"tools_expected,omitempty"`

	// Declarative execution guidance for the LLM.
	Instructions    string   `json:"instructions,omitempty"`
	RequiredContext []string `json:"required_context,omitempty"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
	IterationPolicy string   `json:"iteration_policy,omitempty"`

	// Script optimization (Track B)
	ScriptTemplate      string               `json:"script_template,omitempty"`
	OptimizationProfile *OptimizationProfile `json:"optimization_profile,omitempty"`

	// Conditional execution
	Condition *StepCondition `json:"condition,omitempty"`
}

// WorkflowExecutionContext is the structured payload injected into queued workflow turns.
type WorkflowExecutionContext struct {
	WorkflowID          string
	WorkflowName        string
	WorkflowDescription string
	ProtocolGuidance    string
	StepNumber          int
	StepTitle           string
	StepActor           string
	StepInstructions    string
	RequiredContext     []string
	SuccessCriteria     []string
	IterationPolicy     string
	ToolsExpected       []string
}

func (c WorkflowExecutionContext) RenderStepStatus() string {
	if c.StepNumber <= 0 {
		if strings.TrimSpace(c.StepTitle) != "" {
			return c.StepTitle
		}
		return "Workflow step"
	}
	if strings.TrimSpace(c.StepTitle) != "" {
		return fmt.Sprintf("Step %d: %s", c.StepNumber, c.StepTitle)
	}
	return fmt.Sprintf("Step %d", c.StepNumber)
}

func (c WorkflowExecutionContext) RenderStepEventLabel() string {
	if trimmed := strings.TrimSpace(c.StepTitle); trimmed != "" {
		return fmt.Sprintf("#%s", trimmed)
	}
	if c.StepNumber > 0 {
		return fmt.Sprintf("#Step %d", c.StepNumber)
	}
	return "#workflow-step"
}

// RenderPrompt turns the structured context into a compact prompt block that can be prepended to the next turn.
func (c WorkflowExecutionContext) RenderPrompt(query string) string {
	var sb strings.Builder
	sb.WriteString("Workflow execution context:\n")
	if c.WorkflowName != "" {
		sb.WriteString("- Workflow: ")
		sb.WriteString(c.WorkflowName)
		sb.WriteByte('\n')
	}
	if c.WorkflowDescription != "" {
		sb.WriteString("- Description: ")
		sb.WriteString(c.WorkflowDescription)
		sb.WriteByte('\n')
	}
	if c.ProtocolGuidance != "" {
		sb.WriteString("- Protocol: ")
		sb.WriteString(c.ProtocolGuidance)
		sb.WriteByte('\n')
	}
	if c.StepInstructions != "" {
		sb.WriteString("- Instructions: ")
		sb.WriteString(c.StepInstructions)
		sb.WriteByte('\n')
	}
	if len(c.RequiredContext) > 0 {
		sb.WriteString("- Required context: ")
		sb.WriteString(strings.Join(c.RequiredContext, "; "))
		sb.WriteByte('\n')
	}
	if len(c.SuccessCriteria) > 0 {
		sb.WriteString("- Success criteria: ")
		sb.WriteString(strings.Join(c.SuccessCriteria, "; "))
		sb.WriteByte('\n')
	}
	if c.IterationPolicy != "" {
		sb.WriteString("- Iteration policy: ")
		sb.WriteString(c.IterationPolicy)
		sb.WriteByte('\n')
	}
	if len(c.ToolsExpected) > 0 {
		sb.WriteString("- Expected tools: ")
		sb.WriteString(strings.Join(c.ToolsExpected, ", "))
		sb.WriteByte('\n')
	}
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		sb.WriteString("\nTask:\n")
		sb.WriteString(trimmed)
	}
	return sb.String()
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

	// Federation tracking
	FederationVersion int    `json:"federation_version,omitempty"` // Auto-incremented by HQ on each publish
	ContentHash       string `json:"content_hash,omitempty"`       // SHA-256 of workflow content
}

// ConditionEvaluator is a function that determines if a step should execute.
// It receives the turn result (from workqueue.TurnResult) and returns true if the condition is met.
type ConditionEvaluator func(result TurnResult) bool
