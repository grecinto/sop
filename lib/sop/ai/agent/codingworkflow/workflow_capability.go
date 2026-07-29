package codingworkflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
	WorkflowCapabilityID   = "workflow_manager"
	WorkflowCapabilityName = "Workflow Manager"
)

// WorkflowCapability is the new capability that uses WorkflowManager
// to execute structured workflows instead of the old queue-based approach.
type WorkflowCapability struct {
	manager     *WorkflowManager
	sharedInfra *SharedInfrastructure
}

// NewWorkflowCapability creates a new workflow-based capability
func NewWorkflowCapability(sharedInfra *SharedInfrastructure) *WorkflowCapability {
	if sharedInfra == nil {
		sharedInfra = NewDefaultSharedInfrastructure()
	}
	return &WorkflowCapability{
		manager:     NewWorkflowManager(sharedInfra),
		sharedInfra: sharedInfra,
	}
}

func (c *WorkflowCapability) ID() string   { return WorkflowCapabilityID }
func (c *WorkflowCapability) Name() string { return WorkflowCapabilityName }

func (c *WorkflowCapability) Entitlement() capabilities.Entitlement {
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

func (c *WorkflowCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil || c.manager == nil {
		return capabilities.ClassificationResult{}
	}

	// Check capability hint
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if hint == "workflow_manager" || hint == "coding_workflow" || hint == "codingworkflow" || hint == "repo_edit" {
			return capabilities.ClassificationResult{Relevant: true, Score: 10, Mode: capabilities.CapabilityModePrimary}
		}
		if hint == "desktop_automation" || hint == "local_ops" {
			return capabilities.ClassificationResult{}
		}
	}

	// Check query keywords
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return capabilities.ClassificationResult{}
	}

	if strings.Contains(q, "fix") ||
		strings.Contains(q, "refactor") ||
		strings.Contains(q, "patch") ||
		strings.Contains(q, "implement") ||
		strings.Contains(q, "modify") ||
		strings.Contains(q, "search") ||
		strings.Contains(q, "edit") ||
		strings.Contains(q, "repo") ||
		strings.Contains(q, "code") ||
		strings.Contains(q, "test") ||
		strings.Contains(q, "debug") ||
		strings.Contains(q, "architect") ||
		strings.Contains(q, "document") {
		return capabilities.ClassificationResult{Relevant: true, Score: 8, Mode: capabilities.CapabilityModePrimary}
	}
	return capabilities.ClassificationResult{}
}

func (c *WorkflowCapability) BuildContext(query string, state capabilities.RoutingState) string {
	return "Structured Context: Workflow Manager\n- Use structured workflows for code edits, testing, and analysis tasks.\n- Each workflow is broken into discrete Steps executed in sequence.\n- Steps have access to SharedInfrastructure for AST, patches, tests, and repo operations.\n- Workflows support retry logic and can adapt based on outcomes."
}

func (c *WorkflowCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	return "Use the workflow manager for structured multi-step coding tasks. Each workflow breaks the task into explicit Steps with clear responsibilities. Steps have lifecycle hooks to access infrastructure and make decisions. Keep actions explicit, previewable, and safe.\n\n" + CodeFormatInstructions
}

func (c *WorkflowCapability) ExposeTools() []capabilities.ToolDefinition {
	return []capabilities.ToolDefinition{{
		Name:        "workflow_manager",
		Description: "Executes structured multi-step workflows for coding tasks (test-fix, debugging, implementation, refactoring, architecture, documentation)",
		Schema:      "{}",
	}}
}

func (c *WorkflowCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = ctx
	_ = query
	_ = state
	return nil
}

// Execute implements the capability's main execution logic
// This is called when the LLM invokes the workflow_manager tool
func (c *WorkflowCapability) Execute(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	if c == nil || c.manager == nil || c.sharedInfra == nil {
		return "", fmt.Errorf("workflow capability not properly initialized")
	}

	// Classify the task to determine workflow type
	classifier := DefaultTaskClassifier()
	classification := classifier.Classify(query, "", "")

	// Create workflow Steps based on classification
	var steps []Step
	var workflowName string

	// Check for test-fix first (takes priority)
	lowerQuery := strings.ToLower(query)
	isTestFix := strings.Contains(lowerQuery, "fix") &&
		(strings.Contains(lowerQuery, "test") ||
			strings.Contains(lowerQuery, "failing") ||
			strings.Contains(lowerQuery, "failed"))

	if isTestFix {
		workflowName = "Test Fix"
		steps = c.createTestFixWorkflow(".", []string{"./..."})
	} else {
		// Select workflow based on classification
		switch classification.TaskType {
		case TaskTypeDebugging:
			workflowName = "Debugging"
			steps = c.createDebuggingWorkflow()
		case TaskTypeImplementation:
			workflowName = "Implementation"
			steps = c.createImplementationWorkflow()
		case TaskTypeTesting:
			workflowName = "Testing"
			steps = c.createTestingWorkflow(".", []string{"./..."})
		case TaskTypeRefactoring:
			workflowName = "Refactoring"
			steps = c.createRefactoringWorkflow()
		case TaskTypeArchitecture:
			workflowName = "Architecture"
			steps = c.createArchitectureWorkflow()
		case TaskTypeDocumentation:
			workflowName = "Documentation"
			steps = c.createDocumentationWorkflow()
		default:
			workflowName = "General"
			steps = c.createGeneralWorkflow()
		}
	}

	// Determine TaskType string for Task
	taskTypeStr := ""
	if isTestFix {
		taskTypeStr = "test_fix"
	} else {
		taskTypeStr = string(classification.TaskType)
	}

	// Create task from query
	task := Task{
		WorkingDir:  ".",
		Description: query,
		TaskType:    taskTypeStr,
	}

	// Create RunState from RoutingState
	runState := RunState{
		Args:     make(map[string]string),
		Metadata: make(map[string]string),
	}

	// Execute workflow using the workflow manager's shared step runner.
	result, err := c.manager.runWorkflow(ctx, steps, task, runState)
	if err != nil {
		return "", fmt.Errorf("workflow execution failed: %w", err)
	}

	// Format result
	if !result.Success {
		return fmt.Sprintf("%s Workflow failed at step: %s\nError: %v\n\nCompleted steps: %v",
			workflowName, result.FailedStep, result.Error, result.CompletedSteps), result.Error
	}

	return fmt.Sprintf("%s Workflow completed successfully.\n\nCompleted %d steps in %v:\n%v\n\nOutcome: %s",
		workflowName, len(result.CompletedSteps), result.ExecutionTime, result.CompletedSteps, result.Message), nil
}

// createTestFixWorkflow creates the 7-step test-fix workflow
func (c *WorkflowCapability) createTestFixWorkflow(workingDir string, testArgs []string) []Step {
	return []Step{
		NewRunTestsStep(workingDir, testArgs),
		NewAnalyzeErrorsStep(),
		NewEnhancedReadTestSourceStep(),     // Use enhanced version with AST
		NewEnhancedReadImplementationStep(), // Use enhanced version with AST
		NewApplyFixStep(nil),                // Patch will be generated during execution
		NewVerifyFixStep(workingDir),
		// Iteration step is implicit via retry logic in WorkflowManager
	}
}

// createDebuggingWorkflow creates the debugging workflow
func (c *WorkflowCapability) createDebuggingWorkflow() []Step {
	return []Step{
		NewAnalyzeCodeStep("Analyze code to identify the bug"),
		NewGeneratePatchStep("Generate patch to fix the identified bug"),
		NewApplyFixStep(nil),
	}
}

// createImplementationWorkflow creates the implementation workflow
func (c *WorkflowCapability) createImplementationWorkflow() []Step {
	return []Step{
		NewDesignStep("Design implementation approach"),
		NewGenerateCodeStep("Generate implementation code"),
		NewApplyFixStep(nil),
	}
}

// createTestingWorkflow creates the testing workflow
func (c *WorkflowCapability) createTestingWorkflow(workingDir string, testArgs []string) []Step {
	return []Step{
		NewRunTestsStep(workingDir, testArgs),
	}
}

// createRefactoringWorkflow creates the refactoring workflow
func (c *WorkflowCapability) createRefactoringWorkflow() []Step {
	return []Step{
		NewAnalyzeCodeStep("Analyze code structure for refactoring"),
		NewGeneratePlanStep("Generate refactoring plan"),
		NewApplyRefactoringStep(),
	}
}

// createArchitectureWorkflow creates the architecture workflow
func (c *WorkflowCapability) createArchitectureWorkflow() []Step {
	return []Step{
		NewAnalyzeCodeStep("Analyze codebase structure with AST"),
		NewMapComponentsStep("Map components and dependencies"),
		NewDesignArchitectureStep("Design architecture proposal"),
		NewValidateDesignStep("Validate design against best practices"),
		NewDocumentDecisionStep("Document architecture decision"),
	}
}

// createDocumentationWorkflow creates the documentation workflow
func (c *WorkflowCapability) createDocumentationWorkflow() []Step {
	return []Step{
		NewAnalyzeCodeStep("Analyze code structure with AST"),
		NewExtractDocsStep("Extract existing documentation and identify gaps"),
		NewGenerateDocsStep("Generate comprehensive documentation"),
		NewReviewDocsStep("Review documentation for accuracy"),
	}
}

// createGeneralWorkflow creates a general-purpose workflow
func (c *WorkflowCapability) createGeneralWorkflow() []Step {
	return []Step{
		NewGeneralStep("Process request"),
	}
}
