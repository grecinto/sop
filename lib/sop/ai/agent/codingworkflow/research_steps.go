package codingworkflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// ComplexityAnalysisStep performs deep complexity analysis with LLM reasoning.
// This is a Full Step that uses the Ask/ReAct pipeline.
type ComplexityAnalysisStep struct {
	BaseStep
	threshold string
}

// NewComplexityAnalysisStep creates a complexity analysis research step
func NewComplexityAnalysisStep(context map[string]interface{}) Step {
	step := &ComplexityAnalysisStep{}
	if threshold, ok := context["threshold"].(string); ok {
		step.threshold = threshold
	} else {
		step.threshold = "high"
	}

	step.SetMetadata(StepMetadata{
		ID:          "research_complexity_analysis",
		Name:        "Complexity Analysis",
		Description: "Analyze code complexity and provide recommendations",
		MaxRetries:  0,
		Tags:        []string{"research", "analysis", "complexity"},
	})
	return step
}

func (s *ComplexityAnalysisStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	// Get context from previous step if available
	ws := s.GetWorkflowState()
	var targetFiles []string
	if ws != nil {
		if lastResult, ok := ws.GetLastStepResult(); ok {
			if files, ok := lastResult.Data["files"].([]string); ok {
				targetFiles = files
			}
		}
	}

	filesContext := ""
	if len(targetFiles) > 0 {
		filesContext = fmt.Sprintf("\n\nFiles to analyze:\n- %s", strings.Join(targetFiles, "\n- "))
	}

	return fmt.Sprintf(`You are performing complexity analysis.

Threshold: %s complexity

Analyze the code for:
1. Cyclomatic complexity (branches, loops, conditions)
2. Cognitive complexity (readability, understandability)
3. Dependency complexity (coupling, fan-in/fan-out)
4. Data flow complexity (variable usage, state management)

Provide specific recommendations for simplification.%s

Use available tools to read files, analyze AST, and inspect the codebase.`, s.threshold, filesContext), nil
}

func (s *ComplexityAnalysisStep) OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error) {
	// Track which files were analyzed
	if toolName == "read_file" || toolName == "parse_ast" {
		if result, ok := toolResult.(string); ok && result != "" {
			s.SetStepState("analyzed_content", result)
		}
	}

	// Let LLM continue reasoning
	return state, true, nil
}

func (s *ComplexityAnalysisStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	// Extract analysis from step state
	analysis := s.GetStepState()

	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Complexity analysis completed",
		Data: map[string]interface{}{
			"analysis":  analysis,
			"threshold": s.threshold,
			"summary":   fmt.Sprintf("Analyzed code complexity (threshold: %s)", s.threshold),
		},
	}, nil
}

// SecurityValidationStep performs security validation with potential user interaction.
type SecurityValidationStep struct {
	BaseStep
	sensitiveAreas []string
}

// NewSecurityValidationStep creates a security validation research step
func NewSecurityValidationStep(context map[string]interface{}) Step {
	step := &SecurityValidationStep{}
	if areas, ok := context["sensitive_areas"].([]string); ok {
		step.sensitiveAreas = areas
	}

	step.SetMetadata(StepMetadata{
		ID:          "research_security_validation",
		Name:        "Security Validation",
		Description: "Validate security implications of changes",
		MaxRetries:  0,
		Tags:        []string{"research", "security", "validation"},
	})
	return step
}

func (s *SecurityValidationStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	areasText := "general security"
	if len(s.sensitiveAreas) > 0 {
		areasText = strings.Join(s.sensitiveAreas, ", ")
	}

	return fmt.Sprintf(`You are performing security validation.

Focus areas: %s

Check for:
1. Input validation and sanitization
2. Authentication and authorization
3. Data exposure (sensitive data handling)
4. Injection vulnerabilities (SQL, XSS, command injection)
5. Cryptographic practices
6. Error handling (information leakage)
7. Access control

Provide specific security recommendations and flag any concerns.

Use available tools to inspect code, check patterns, and analyze data flow.`, areasText), nil
}

func (s *SecurityValidationStep) OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error) {
	// Track security findings
	if toolName == "read_file" || toolName == "grep_repo_text" {
		if result, ok := toolResult.(string); ok && result != "" {
			findings := s.GetStepState()["findings"]
			if findings == nil {
				findings = []string{}
			}
			s.SetStepState("findings", append(findings.([]string), result))
		}
	}

	return state, true, nil
}

func (s *SecurityValidationStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	findings := s.GetStepState()

	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Security validation completed",
		Data: map[string]interface{}{
			"findings":        findings,
			"sensitive_areas": s.sensitiveAreas,
			"summary":         "Security validation analysis",
		},
	}, nil
}

// ArchitectureResearchStep performs architecture pattern research with LLM.
type ArchitectureResearchStep struct {
	BaseStep
	focus string
}

// NewArchitectureResearchStep creates an architecture research step
func NewArchitectureResearchStep(context map[string]interface{}) Step {
	step := &ArchitectureResearchStep{}
	if focus, ok := context["focus"].(string); ok {
		step.focus = focus
	} else {
		step.focus = "general architecture"
	}

	step.SetMetadata(StepMetadata{
		ID:          "research_architecture",
		Name:        "Architecture Research",
		Description: "Research architecture patterns and design decisions",
		MaxRetries:  0,
		Tags:        []string{"research", "architecture", "design"},
	})
	return step
}

func (s *ArchitectureResearchStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return fmt.Sprintf(`You are researching architecture patterns.

Focus: %s

Analyze:
1. Current architecture patterns in the codebase
2. Component dependencies and relationships
3. Design patterns used (MVC, Repository, Factory, etc.)
4. Layering and separation of concerns
5. Scalability and maintainability considerations

Provide recommendations for architectural improvements.

Use available tools to explore the codebase structure, read files, and analyze dependencies.`, s.focus), nil
}

func (s *ArchitectureResearchStep) OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error) {
	// Track architecture findings
	if toolName == "list_directory" || toolName == "read_file" || toolName == "search_repo_files" {
		if result, ok := toolResult.(string); ok && result != "" {
			s.SetStepState("architecture_context", result)
		}
	}

	return state, true, nil
}

func (s *ArchitectureResearchStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	findings := s.GetStepState()

	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Architecture research completed",
		Data: map[string]interface{}{
			"findings": findings,
			"focus":    s.focus,
			"summary":  fmt.Sprintf("Architecture research: %s", s.focus),
		},
	}, nil
}

// DependencyResearchStep researches dependencies and their implications.
type DependencyResearchStep struct {
	BaseStep
	language string
}

// NewDependencyResearchStep creates a dependency research step
func NewDependencyResearchStep(context map[string]interface{}) Step {
	step := &DependencyResearchStep{}
	if lang, ok := context["language"].(string); ok {
		step.language = lang
	} else {
		step.language = "go"
	}

	step.SetMetadata(StepMetadata{
		ID:          "research_dependencies",
		Name:        "Dependency Research",
		Description: "Research dependencies and their implications",
		MaxRetries:  0,
		Tags:        []string{"research", "dependencies"},
	})
	return step
}

func (s *DependencyResearchStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return fmt.Sprintf(`You are researching dependencies.

Language: %s

Analyze:
1. Direct dependencies (imports, requires)
2. Transitive dependencies
3. Dependency versions and compatibility
4. Security vulnerabilities in dependencies
5. Dependency bloat and unused dependencies
6. Alternative lighter dependencies

Provide recommendations for dependency management.

Use available tools to read dependency files (go.mod, package.json, requirements.txt, etc.) and analyze imports.`, s.language), nil
}

func (s *DependencyResearchStep) OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error) {
	if toolName == "read_file" {
		if result, ok := toolResult.(string); ok && result != "" {
			s.SetStepState("dependency_file", result)
		}
	}

	return state, true, nil
}

func (s *DependencyResearchStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	findings := s.GetStepState()

	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Dependency research completed",
		Data: map[string]interface{}{
			"findings": findings,
			"language": s.language,
			"summary":  fmt.Sprintf("Dependency analysis for %s", s.language),
		},
	}, nil
}

// TestCoverageResearchStep analyzes test coverage.
type TestCoverageResearchStep struct {
	BaseStep
	targetPath string
}

// NewTestCoverageResearchStep creates a test coverage research step
func NewTestCoverageResearchStep(context map[string]interface{}) Step {
	step := &TestCoverageResearchStep{}
	if path, ok := context["target_path"].(string); ok {
		step.targetPath = path
	} else {
		step.targetPath = "."
	}

	step.SetMetadata(StepMetadata{
		ID:          "research_test_coverage",
		Name:        "Test Coverage Research",
		Description: "Analyze test coverage and identify gaps",
		MaxRetries:  0,
		Tags:        []string{"research", "testing", "coverage"},
	})
	return step
}

func (s *TestCoverageResearchStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	return fmt.Sprintf(`You are analyzing test coverage.

Target path: %s

Analyze:
1. Existing test files and test patterns
2. Code coverage (functions, branches, lines)
3. Missing test cases
4. Edge cases not covered
5. Test quality (assertions, mocks, fixtures)

Provide recommendations for improving test coverage.

Use available tools to list test files, read tests, and analyze implementation files.`, s.targetPath), nil
}

func (s *TestCoverageResearchStep) OnAfterToolResult(ctx context.Context, toolName string, toolResult interface{}, state capabilities.RoutingState) (capabilities.RoutingState, bool, error) {
	if toolName == "run_tests" || toolName == "read_file" {
		if result, ok := toolResult.(string); ok && result != "" {
			s.SetStepState("coverage_data", result)
		}
	}

	return state, true, nil
}

func (s *TestCoverageResearchStep) OnFinalize(ctx context.Context, state capabilities.RoutingState) (StepResult, error) {
	findings := s.GetStepState()

	return StepResult{
		Outcome: StepOutcomeSuccess,
		Message: "Test coverage research completed",
		Data: map[string]interface{}{
			"findings":    findings,
			"target_path": s.targetPath,
			"summary":     fmt.Sprintf("Test coverage analysis for %s", s.targetPath),
		},
	}, nil
}
