package codingworkflow

import (
	"context"
	"fmt"
)

// BestPracticesHandler automatically injects best practices research
// for code generation steps.
//
// Usage:
//
//	wm := NewWorkflowManager(orchestrator)
//	wm.RegisterEventHandler(&BestPracticesHandler{
//	    Languages: []string{"go", "python"},
//	})
type BestPracticesHandler struct {
	Languages []string // Which languages to apply to (empty = all languages)
}

// CanHandle checks if this handler should process the event.
// Only triggers for code generation steps during BeforeExecution.
func (h *BestPracticesHandler) CanHandle(event StepEvent) bool {
	if event.Type != StepEventBeforeExecution {
		return false
	}

	// Check if step has code_generation tag
	metadata := event.Step.Metadata()
	for _, tag := range metadata.Tags {
		if tag == "code_generation" || tag == "implementation" {
			return true
		}
	}

	return false
}

// HandleEvent injects a best practices research signal.
func (h *BestPracticesHandler) HandleEvent(ctx context.Context, event StepEvent) ([]StepSignal, error) {
	// Determine language from workflow state or default to "go"
	language := "go"
	if event.WorkflowState != nil {
		if lang, ok := event.WorkflowState.SharedData["language"].(string); ok {
			language = lang
		}
	}

	// Filter by language if specified
	if len(h.Languages) > 0 {
		found := false
		for _, lang := range h.Languages {
			if lang == language {
				found = true
				break
			}
		}
		if !found {
			return nil, nil // Skip this language
		}
	}

	return []StepSignal{{
		Type:        SignalResearchBestPractices,
		Mode:        SignalModeFunctionCall,
		Description: fmt.Sprintf("Research %s best practices before code generation", language),
		Context: map[string]interface{}{
			"language": language,
			"scope":    "code_generation",
		},
		Priority: SignalPriorityHigh,
	}}, nil
}

// ComplexityAnalysisHandler injects complexity analysis for steps
// that follow high-complexity detection.
//
// Usage:
//
//	wm := NewWorkflowManager(orchestrator)
//	wm.RegisterEventHandler(&ComplexityAnalysisHandler{
//	    Threshold: "high",
//	})
type ComplexityAnalysisHandler struct {
	Threshold string // Complexity threshold to trigger (e.g., "high", "medium")
}

// CanHandle checks if previous step indicated high complexity.
func (h *ComplexityAnalysisHandler) CanHandle(event StepEvent) bool {
	if event.Type != StepEventBeforeExecution {
		return false
	}

	if event.WorkflowState == nil {
		return false
	}

	// Check if previous step indicated high complexity
	if lastResult, ok := event.WorkflowState.GetLastStepResult(); ok {
		if complexity, ok := lastResult.Data["complexity"].(string); ok {
			threshold := h.Threshold
			if threshold == "" {
				threshold = "high"
			}
			return complexity == threshold
		}
	}

	return false
}

// HandleEvent injects a complexity analysis signal.
func (h *ComplexityAnalysisHandler) HandleEvent(ctx context.Context, event StepEvent) ([]StepSignal, error) {
	return []StepSignal{{
		Type:        SignalAnalyzeComplexity,
		Mode:        SignalModeFullStep, // Complex analysis needs Ask loop
		Description: "Analyze code complexity before proceeding",
		Context: map[string]interface{}{
			"threshold": h.Threshold,
		},
		Priority: SignalPriorityCritical,
	}}, nil
}

// SecurityValidationHandler injects security validation for sensitive operations.
//
// Usage:
//
//	wm := NewWorkflowManager(orchestrator)
//	wm.RegisterEventHandler(&SecurityValidationHandler{
//	    SensitiveTags: []string{"authentication", "authorization", "payment"},
//	})
type SecurityValidationHandler struct {
	SensitiveTags []string // Tags that trigger security validation
}

// CanHandle checks if step has sensitive tags.
func (h *SecurityValidationHandler) CanHandle(event StepEvent) bool {
	if event.Type != StepEventBeforeExecution {
		return false
	}

	metadata := event.Step.Metadata()
	for _, stepTag := range metadata.Tags {
		for _, sensitiveTag := range h.SensitiveTags {
			if stepTag == sensitiveTag {
				return true
			}
		}
	}

	return false
}

// HandleEvent injects a security validation signal.
func (h *SecurityValidationHandler) HandleEvent(ctx context.Context, event StepEvent) ([]StepSignal, error) {
	return []StepSignal{{
		Type:        SignalValidateSecurity,
		Mode:        SignalModeFullStep, // Security validation may need user input
		Description: "Validate security implications before execution",
		Priority:    SignalPriorityCritical,
	}}, nil
}

// CodePatternHandler injects code pattern research for complex implementations.
//
// Usage:
//
//	wm := NewWorkflowManager(orchestrator)
//	wm.RegisterEventHandler(&CodePatternHandler{})
type CodePatternHandler struct{}

// CanHandle checks if step is implementation-related.
func (h *CodePatternHandler) CanHandle(event StepEvent) bool {
	if event.Type != StepEventBeforeExecution {
		return false
	}

	metadata := event.Step.Metadata()
	for _, tag := range metadata.Tags {
		if tag == "implementation" || tag == "refactoring" {
			return true
		}
	}

	return false
}

// HandleEvent injects a code pattern research signal.
func (h *CodePatternHandler) HandleEvent(ctx context.Context, event StepEvent) ([]StepSignal, error) {
	return []StepSignal{{
		Type:        SignalResearchCodePatterns,
		Mode:        SignalModeFunctionCall,
		Description: "Research existing code patterns",
		Context: map[string]interface{}{
			"task_type": "implementation",
		},
		Priority: SignalPriorityMedium,
	}}, nil
}
