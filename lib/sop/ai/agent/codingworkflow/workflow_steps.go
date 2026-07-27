package codingworkflow

import (
	"context"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// NewReadTestSourceStep creates a step to read failing test source code
func NewReadTestSourceStep() *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "read_test_source",
		Name:        "Read Test Source",
		Description: "Read the failing test source code to understand what it's testing",
		MaxRetries:  0,
		Tags:        []string{"testing", "analysis"},
	})
	return step
}

// NewReadImplementationStep creates a step to read implementation code
func NewReadImplementationStep() *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "read_implementation",
		Name:        "Read Implementation",
		Description: "Find and read the implementation code that the test is checking",
		MaxRetries:  0,
		Tags:        []string{"analysis", "reading"},
	})
	return step
}

// NewAnalyzeCodeStep creates a step to analyze code
func NewAnalyzeCodeStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "analyze_code",
		Name:        "Analyze Code",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"analysis"},
	})
	return step
}

// NewGeneratePatchStep creates a step to generate a patch
func NewGeneratePatchStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "generate_patch",
		Name:        "Generate Patch",
		Description: description,
		MaxRetries:  2,
		Tags:        []string{"patching"},
	})
	return step
}

// NewDesignStep creates a step for design work
func NewDesignStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "design",
		Name:        "Design",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"design", "planning"},
	})
	return step
}

// NewGenerateCodeStep creates a step to generate code
func NewGenerateCodeStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "generate_code",
		Name:        "Generate Code",
		Description: description,
		MaxRetries:  2,
		Tags:        []string{"implementation", "generation"},
	})
	return step
}

// NewGeneratePlanStep creates a step to generate a plan
func NewGeneratePlanStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "generate_plan",
		Name:        "Generate Plan",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"planning"},
	})
	return step
}

// NewApplyRefactoringStep creates a step to apply refactoring
func NewApplyRefactoringStep() *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "apply_refactoring",
		Name:        "Apply Refactoring",
		Description: "Apply refactoring changes to the codebase",
		MaxRetries:  2,
		Tags:        []string{"refactoring", "modification"},
	})
	return step
}

// NewMapComponentsStep creates a step to map components
func NewMapComponentsStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "map_components",
		Name:        "Map Components",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"architecture", "analysis"},
	})
	return step
}

// NewDesignArchitectureStep creates a step to design architecture
func NewDesignArchitectureStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "design_architecture",
		Name:        "Design Architecture",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"architecture", "design"},
	})
	return step
}

// NewValidateDesignStep creates a step to validate design
func NewValidateDesignStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "validate_design",
		Name:        "Validate Design",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"validation", "review"},
	})
	return step
}

// NewDocumentDecisionStep creates a step to document decisions
func NewDocumentDecisionStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "document_decision",
		Name:        "Document Decision",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"documentation"},
	})
	return step
}

// NewExtractDocsStep creates a step to extract documentation
func NewExtractDocsStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "extract_docs",
		Name:        "Extract Documentation",
		Description: description,
		MaxRetries:  0,
		Tags:        []string{"documentation", "extraction"},
	})
	return step
}

// NewGenerateDocsStep creates a step to generate documentation
func NewGenerateDocsStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "generate_docs",
		Name:        "Generate Documentation",
		Description: description,
		MaxRetries:  2,
		Tags:        []string{"documentation", "generation"},
	})
	return step
}

// NewReviewDocsStep creates a step to review documentation
func NewReviewDocsStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "review_docs",
		Name:        "Review Documentation",
		Description: description,
		MaxRetries:  1,
		Tags:        []string{"documentation", "review"},
	})
	return step
}

// NewGeneralStep creates a general-purpose step
func NewGeneralStep(description string) *BaseStep {
	step := &BaseStep{}
	step.SetMetadata(StepMetadata{
		ID:          "general",
		Name:        "General Task",
		Description: description,
		MaxRetries:  2,
		Tags:        []string{"general"},
	})
	return step
}

// Enhanced step implementations with specific logic

// EnhancedReadTestSourceStep reads test source with AST parsing
type EnhancedReadTestSourceStep struct {
	BaseStep
}

func (s *EnhancedReadTestSourceStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return "Read the failing test source code to understand:\n- What functionality is being tested\n- What functions/methods are called\n- What assertions are made\n- What test data is used", nil
}

func (s *EnhancedReadTestSourceStep) OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error) {
	// If we read a test file, store it for next step
	if toolName == "read_file" && s.infra != nil {
		if content, ok := toolResult.(string); ok && content != "" {
			// Store for next step (AST parsing can happen later if needed)
			s.SetStepState("test_content", content)
		}
	}
	return state, true, nil
}

// EnhancedReadImplementationStep reads implementation with AST analysis
type EnhancedReadImplementationStep struct {
	BaseStep
}

func (s *EnhancedReadImplementationStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return "Find and read the implementation code:\n- Use grep_repo_text or search_repo_files if needed\n- Read the actual implementation file\n- Identify the specific function/method being tested", nil
}

func (s *EnhancedReadImplementationStep) OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error) {
	// If we read implementation, store it for analysis
	if (toolName == "read_file" || toolName == "read_repo_store_file") && s.infra != nil {
		if content, ok := toolResult.(string); ok && content != "" {
			s.SetStepState("impl_content", content)
			// AST parsing can happen later during fix generation
		}
	}
	return state, true, nil
}

// Enhanced factory functions that return the enhanced versions

// NewEnhancedReadTestSourceStep creates an enhanced test source reading step
func NewEnhancedReadTestSourceStep() Step {
	step := &EnhancedReadTestSourceStep{}
	step.SetMetadata(StepMetadata{
		ID:          "read_test_source",
		Name:        "Read Test Source",
		Description: "Read the failing test source code with AST analysis",
		MaxRetries:  0,
		Tags:        []string{"testing", "analysis", "ast"},
	})
	return step
}

// NewEnhancedReadImplementationStep creates an enhanced implementation reading step
func NewEnhancedReadImplementationStep() Step {
	step := &EnhancedReadImplementationStep{}
	step.SetMetadata(StepMetadata{
		ID:          "read_implementation",
		Name:        "Read Implementation",
		Description: "Find and read implementation code with AST analysis",
		MaxRetries:  0,
		Tags:        []string{"analysis", "reading", "ast"},
	})
	return step
}
