package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/codingworkflow"
)

func TestEndToEnd_CodingWorkflow_SDLC(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		response       string
		expectDomain   string
		expectTaskType codingworkflow.TaskType
		expectWorkflow string
	}{
		{
			name:           "Design system architecture",
			query:          "design the authentication system architecture",
			response:       `{"domains":["coding_workflow"],"phase":"planning","phase_plan":["intake","investigation","planning","execution","review"],"intent":"plan","confidence":"high","reasoning":"design architecture"}`,
			expectDomain:   "coding_workflow",
			expectTaskType: codingworkflow.TaskTypeArchitecture,
			expectWorkflow: "orchestrateArchitectureWorkflow",
		},
		{
			name:           "Fix failing tests",
			query:          "fix the failing unit tests in auth module",
			response:       `{"domains":["coding_workflow"],"phase":"execution","phase_plan":["investigation","execution","review"],"intent":"generate_code","confidence":"high","reasoning":"debugging"}`,
			expectDomain:   "coding_workflow",
			expectTaskType: codingworkflow.TaskTypeDebugging,
			expectWorkflow: "orchestrateTestFixWorkflow",
		},
		{
			name:           "Implement feature",
			query:          "implement user login functionality",
			response:       `{"domains":["coding_workflow"],"phase":"execution","phase_plan":["investigation","planning","execution","review"],"intent":"generate_code","confidence":"high","reasoning":"implementation"}`,
			expectDomain:   "coding_workflow",
			expectTaskType: codingworkflow.TaskTypeImplementation,
			expectWorkflow: "orchestrateImplementationWorkflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ag := &CopilotAgent{}
			classification, err := ag.ClassifyAskTaskContext(context.Background(), tt.query, &askRoutingTestGenerator{Response: tt.response})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !containsDomain(classification.Domains, tt.expectDomain) {
				t.Fatalf("expected domains to include %q, got %v", tt.expectDomain, classification.Domains)
			}

			classifier := codingworkflow.DefaultTaskClassifier()
			classificationTask := classifier.Classify(tt.query, "", "/test/repo")
			if classificationTask.TaskType == "" {
				t.Fatalf("Task classification returned empty task type for %q", tt.query)
			}
			if classificationTask.TaskType != tt.expectTaskType {
				t.Fatalf("expected task type %q, got %q", tt.expectTaskType, classificationTask.TaskType)
			}

			workflowName := getExpectedWorkflowForTaskType(classificationTask.TaskType)
			if workflowName != tt.expectWorkflow {
				t.Fatalf("expected workflow %q, got %q", tt.expectWorkflow, workflowName)
			}
		})
	}
}

func TestEndToEnd_DesktopAutomation(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		response     string
		expectDomain string
		expectPhase  string
	}{
		{name: "Git command", query: "run git status", response: `{"domains":["desktop"],"phase":"execution","phase_plan":["execution"],"intent":"read_data","confidence":"high","reasoning":"git status"}`, expectDomain: "desktop", expectPhase: "execution"},
		{name: "Shell command", query: "execute shell command to check disk space", response: `{"domains":["desktop"],"phase":"execution","phase_plan":["execution"],"intent":"read_data","confidence":"high","reasoning":"shell command"}`, expectDomain: "desktop", expectPhase: "execution"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ag := &CopilotAgent{}
			classification, err := ag.ClassifyAskTaskContext(context.Background(), tt.query, &askRoutingTestGenerator{Response: tt.response})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !containsDomain(classification.Domains, tt.expectDomain) {
				t.Fatalf("expected domains to include %q, got %v", tt.expectDomain, classification.Domains)
			}
			if classification.Phase != tt.expectPhase {
				t.Fatalf("expected phase %q, got %q", tt.expectPhase, classification.Phase)
			}
		})
	}
}

func TestEndToEnd_AskClassification_UsesMultiPhasePlansForComplexWork(t *testing.T) {
	ag := &CopilotAgent{}
	classification, err := ag.ClassifyAskTaskContext(context.Background(), "design the authentication system architecture", &askRoutingTestGenerator{Response: `{"domains":["coding_workflow"],"phase":"planning","phase_plan":["intake","investigation","planning","execution","review"],"intent":"plan","confidence":"high","reasoning":"complex architecture design"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(classification.PhasePlan) < 2 {
		t.Fatalf("expected complex work to yield a multi-phase plan, got %v", classification.PhasePlan)
	}
	if got := strings.Join(classification.PhasePlan, ","); got != "planning,execution" {
		t.Fatalf("expected complex coding plan to use the active planning/execution phases, got %q", got)
	}
}

func getExpectedWorkflowForTaskType(taskType codingworkflow.TaskType) string {
	switch taskType {
	case codingworkflow.TaskTypeDebugging:
		return "orchestrateTestFixWorkflow"
	case codingworkflow.TaskTypeArchitecture:
		return "orchestrateArchitectureWorkflow"
	case codingworkflow.TaskTypeDocumentation:
		return "orchestrateDocumentationWorkflow"
	case codingworkflow.TaskTypeImplementation,
		codingworkflow.TaskTypeTesting,
		codingworkflow.TaskTypeRefactoring:
		return "orchestrateImplementationWorkflow"
	default:
		return "orchestrateImplementationWorkflow"
	}
}
