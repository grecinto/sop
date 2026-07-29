package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestWorkflowDefinitionToCodingWorkflowStepsPreservesDeclarativeContext(t *testing.T) {
	workflow := &WorkflowDefinition{
		ID:               "workflow-123",
		Name:             "Debugging Workflow",
		Description:      "Investigate and fix failing code",
		ProtocolGuidance: "Follow the protocol guidance",
		Steps: []WorkflowStep{{
			Step:          1,
			Title:         "Inspect Failure",
			Actor:         "assistant",
			QueryTemplate: "Investigate {original_query}",
			Instructions:  "Inspect the failing test and error output",
			RequiredContext: []string{
				"repo state",
				"test output",
			},
			SuccessCriteria: []string{"root cause identified"},
			IterationPolicy: "one-pass",
			ToolsExpected:   []string{"grep", "read_file"},
		}},
	}

	steps := workflow.ToCodingWorkflowSteps()
	if len(steps) != 1 {
		t.Fatalf("expected 1 coding workflow step, got %d", len(steps))
	}

	metadata := steps[0].Metadata()
	if metadata.ID != "workflow-123_step_1" {
		t.Fatalf("unexpected metadata ID: %q", metadata.ID)
	}
	if metadata.Name != "Inspect Failure" {
		t.Fatalf("unexpected metadata name: %q", metadata.Name)
	}
	if !containsTag(metadata.Tags, "core_playbook") {
		t.Fatalf("expected core_playbook tag, got %v", metadata.Tags)
	}

	prompt, err := steps[0].OnBeforePrompt(context.Background(), "Fix failing test", capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("OnBeforePrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "Inspect the failing test and error output") {
		t.Fatalf("prompt did not include instructions: %q", prompt)
	}
	if !strings.Contains(prompt, "Follow the protocol guidance") {
		t.Fatalf("prompt did not include workflow protocol guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "Required context") {
		t.Fatalf("prompt did not include required context section: %q", prompt)
	}

	if _, ok := steps[0].(*WorkflowStepAdapter); !ok {
		t.Fatalf("expected adapter to implement codingworkflow.Step, got %T", steps[0])
	}

	var _ cw.Step = (*WorkflowStepAdapter)(nil)
}

func containsTag(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
