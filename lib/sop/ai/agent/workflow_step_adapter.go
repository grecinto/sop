package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// WorkflowStepAdapter adapts a declarative workflow step into the codingworkflow.Step interface.
//
// This preserves workflow-level and step-level guidance for later prompt construction while
// keeping the step usable by the existing coding workflow runtime.
type WorkflowStepAdapter struct {
	cw.BaseStep
	workflow  *WorkflowDefinition
	step      WorkflowStep
	stepIndex int
}

// ToCodingWorkflowSteps converts a workflow definition into coding workflow steps.
func (w *WorkflowDefinition) ToCodingWorkflowSteps() []cw.Step {
	if w == nil {
		return nil
	}

	steps := make([]cw.Step, 0, len(w.Steps))
	for index, step := range w.Steps {
		adapter := &WorkflowStepAdapter{
			workflow:  w,
			step:      step,
			stepIndex: index,
		}

		stepID := fmt.Sprintf("%s_step_%d", w.ID, step.Step)
		if stepID == "_step_0" || w.ID == "" {
			stepID = fmt.Sprintf("workflow_step_%d", index+1)
		}

		name := strings.TrimSpace(step.Title)
		if name == "" {
			name = fmt.Sprintf("Step %d", step.Step)
			if name == "Step 0" {
				name = fmt.Sprintf("Step %d", index+1)
			}
		}

		description := strings.TrimSpace(step.QueryTemplate)
		if description == "" {
			description = fmt.Sprintf("Workflow step from %s", strings.TrimSpace(w.Name))
			if description == "Workflow step from " {
				description = "Workflow step"
			}
		}

		metadata := cw.StepMetadata{
			ID:          stepID,
			Name:        name,
			Description: description,
			Tags:        []string{"core_playbook"},
		}
		adapter.SetMetadata(metadata)
		steps = append(steps, adapter)
	}

	return steps
}

// OnBeforePrompt injects declarative workflow context into the prompt for the step.
func (a *WorkflowStepAdapter) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
	var sections []string

	if a.workflow != nil {
		if a.workflow.ProtocolGuidance != "" {
			sections = append(sections, fmt.Sprintf("Workflow protocol guidance:\n%s", a.workflow.ProtocolGuidance))
		}
		if a.workflow.Description != "" {
			sections = append(sections, fmt.Sprintf("Workflow description:\n%s", a.workflow.Description))
		}
	}

	if a.step.Instructions != "" {
		sections = append(sections, fmt.Sprintf("Instructions:\n%s", a.step.Instructions))
	}

	if len(a.step.RequiredContext) > 0 {
		var contextLines []string
		for _, item := range a.step.RequiredContext {
			contextLines = append(contextLines, fmt.Sprintf("- %s", item))
		}
		sections = append(sections, "Required context:\n"+strings.Join(contextLines, "\n"))
	}

	if len(a.step.SuccessCriteria) > 0 {
		var criteriaLines []string
		for _, item := range a.step.SuccessCriteria {
			criteriaLines = append(criteriaLines, fmt.Sprintf("- %s", item))
		}
		sections = append(sections, "Success criteria:\n"+strings.Join(criteriaLines, "\n"))
	}

	if a.step.IterationPolicy != "" {
		sections = append(sections, fmt.Sprintf("Iteration policy:\n%s", a.step.IterationPolicy))
	}

	if len(a.step.ToolsExpected) > 0 {
		sections = append(sections, fmt.Sprintf("Expected tools:\n%s", strings.Join(a.step.ToolsExpected, ", ")))
	}

	if strings.TrimSpace(query) != "" {
		sections = append(sections, fmt.Sprintf("Current task:\n%s", strings.TrimSpace(query)))
	}

	if len(sections) == 0 {
		return "", nil
	}

	return strings.Join(sections, "\n\n"), nil
}
