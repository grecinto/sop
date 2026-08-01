package codingworkflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
)

type stubContextManager struct {
	pack ContextPack
	err  error
}

func (s stubContextManager) BuildContext(task Task, state RunState) (ContextPack, error) {
	if s.err != nil {
		return ContextPack{}, s.err
	}
	return s.pack, nil
}

func TestPhaseCapabilityToolSchemasUseObjectLevelRequiredFields(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	tools := cap.ExposeTools()
	toolMap := map[string]string{}
	for _, tool := range tools {
		toolMap[tool.Name] = tool.Schema
	}

	captureSchema, ok := toolMap["planning_capture"]
	if !ok {
		t.Fatalf("expected planning capture tool to be exposed")
	}
	var capture map[string]any
	if err := json.Unmarshal([]byte(captureSchema), &capture); err != nil {
		t.Fatalf("expected planning capture schema to be valid JSON: %v", err)
	}

	required, ok := capture["required"].([]any)
	if !ok {
		t.Fatalf("expected planning capture schema to define an object-level required array, got %#v", capture["required"])
	}
	if len(required) != 2 {
		t.Fatalf("expected planning capture schema to require 2 fields, got %d", len(required))
	}
	if !containsRequiredString(required, "summary") || !containsRequiredString(required, "status") {
		t.Fatalf("expected planning capture schema to require summary and status, got %#v", required)
	}

	properties, ok := capture["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected planning capture schema to expose properties")
	}
	for name, prop := range properties {
		propMap, ok := prop.(map[string]any)
		if !ok {
			t.Fatalf("expected property %s to be an object", name)
		}
		if _, exists := propMap["required"]; exists {
			t.Fatalf("expected property %s to avoid inline required flags, got %#v", name, propMap["required"])
		}
	}

	contextSchema, ok := toolMap["planning_build_context"]
	if !ok {
		t.Fatalf("expected planning build_context tool to be exposed")
	}
	var context map[string]any
	if err := json.Unmarshal([]byte(contextSchema), &context); err != nil {
		t.Fatalf("expected planning build_context schema to be valid JSON: %v", err)
	}
	contextRequired, ok := context["required"].([]any)
	if !ok {
		t.Fatalf("expected planning build_context schema to define an object-level required array, got %#v", context["required"])
	}
	if !containsRequiredString(contextRequired, "query") {
		t.Fatalf("expected planning build_context schema to require query, got %#v", contextRequired)
	}
}

func containsRequiredString(values []any, target string) bool {
	for _, value := range values {
		if str, ok := value.(string); ok && str == target {
			return true
		}
	}
	return false
}

func TestInvestigationPhaseBuildContext_UsesGeneralInvestigationGuidance(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewInvestigationPhaseCapability(infra)

	result := cap.BuildContext("fix failing tests", capabilities.RoutingState{})
	checks := []string{"Inspect the request", "Gather evidence", "Transition to planning"}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Fatalf("expected investigation context to include %q, got %q", check, result)
		}
	}
	for _, disallowed := range []string{"reproduce the failure", "go test", "pytest", "rerun the same tests", "smallest plausible fix"} {
		if strings.Contains(result, disallowed) {
			t.Fatalf("expected investigation context to avoid hard-coded %q guidance, got %q", disallowed, result)
		}
	}
}

func TestExecutionPhaseBuildContext_UsesGeneralValidationGuidance(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewExecutionPhaseCapability(infra)

	result := cap.BuildContext("apply a small change", capabilities.RoutingState{})
	checks := []string{"preview", "user approval", "validate"}
	for _, check := range checks {
		if !strings.Contains(strings.ToLower(result), check) {
			t.Fatalf("expected execution context to include %q, got %q", check, result)
		}
	}
	for _, disallowed := range []string{"TEST-FIX LOOP", "reproduce → inspect → patch → rerun targeted tests until green"} {
		if strings.Contains(result, disallowed) {
			t.Fatalf("expected execution context to avoid legacy guidance %q, got %q", disallowed, result)
		}
	}
}

func TestPlanningPhaseCapabilityExposesToolsAndPersistsArtifact(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	tools := cap.ExposeTools()
	if len(tools) < 7 {
		t.Fatalf("expected at least 7 exposed tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	expected := []string{"planning_capture", "planning_state", "planning_build_context", "planning_inspect_repo", "planning_search_repo", "planning_list_changed_files", "planning_plan_changes"}
	for _, name := range expected {
		if !names[name] {
			t.Fatalf("expected planning capability tools to include %s, got %v", name, names)
		}
	}

	result, err := cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary":      "Design the validation flow",
		"requirements": []string{"validate inputs", "preserve compatibility"},
		"plan":         []string{"inspect parser", "add tests"},
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("planning capture failed: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty planning capture result")
	}

	stored, err := infra.Memory.Load("phase-artifact:planning")
	if err != nil {
		t.Fatalf("expected phase artifact to be stored: %v", err)
	}
	if stored["artifact_json"] == "" {
		t.Fatal("expected phase artifact JSON to be persisted")
	}
}

func TestPlanningPhaseCapabilityExecutesWorkflowTool(t *testing.T) {
	infra := &SharedInfrastructure{
		Memory:         NewInMemorySessionMemory(),
		ContextManager: stubContextManager{pack: ContextPack{WorkingDir: "/tmp/demo", RepositorySummary: "demo repo"}},
	}
	cap := NewPlanningPhaseCapability(infra)

	result, err := cap.ExecuteTool(context.Background(), "planning_build_context", map[string]any{
		"query": "plan the parser refactor",
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("planning build context tool failed: %v", err)
	}
	if !strings.Contains(result, "Context Pack") {
		t.Fatalf("expected build context output, got %q", result)
	}
	if !strings.Contains(result, "/tmp/demo") {
		t.Fatalf("expected working directory in result, got %q", result)
	}
}

func TestPlanningPhaseCapabilityBuildContext_IncludesPendingClarification(t *testing.T) {
	infra := &SharedInfrastructure{
		Memory:         NewInMemorySessionMemory(),
		ContextManager: stubContextManager{pack: ContextPack{WorkingDir: "/tmp/demo", RepositorySummary: "demo repo"}},
	}
	cap := NewPlanningPhaseCapability(infra)

	_, err := cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary": "Need clarification before execution",
		"status":  "clarifying",
		"structured_data": map[string]any{
			"clarification_request": map[string]any{
				"assistant_question": "Which output shape do you want?",
				"missing_fact":       "output shape",
				"reason":             "retry_policy_requested_clarification",
				"source":             "execute_script",
			},
		},
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("planning capture failed: %v", err)
	}

	result, err := cap.ExecuteTool(context.Background(), "planning_build_context", map[string]any{
		"query": "plan the parser refactor",
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("planning build context tool failed: %v", err)
	}
	checks := []string{
		"Pending Clarification",
		"Question: Which output shape do you want?",
		"Missing Fact: output shape",
		"Reason: retry_policy_requested_clarification",
		"Source: execute_script",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Fatalf("expected build context to include %q, got %q", check, result)
		}
	}
}

func TestPlanningPhaseCapabilityCapabilityContext_IncludesPendingClarification(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	_, err := cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary": "Need clarification before execution",
		"status":  "clarifying",
		"structured_data": map[string]any{
			"clarification_request": map[string]any{
				"assistant_question": "Which output shape do you want?",
				"missing_fact":       "output shape",
				"reason":             "retry_policy_requested_clarification",
				"source":             "execute_script",
			},
		},
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("planning capture failed: %v", err)
	}

	result := cap.BuildContext("plan the parser refactor", capabilities.RoutingState{})
	checks := []string{
		"PHASE: Planning & Design",
		"Pending Clarification",
		"Question: Which output shape do you want?",
		"Missing Fact: output shape",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Fatalf("expected capability context to include %q, got %q", check, result)
		}
	}
}

func TestPhaseCapabilityCapturesTransitionAndExitSignals(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	_, err := cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary":              "Quick plan",
		"status":               "ready",
		"next_steps":           []string{"try patch"},
		"transition_to":        "execution",
		"transition_from":      "investigation",
		"transition_direction": "forward",
		"exit":                 true,
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("planning capture with transition failed: %v", err)
	}

	stored, err := infra.Memory.Load("phase-artifact:planning")
	if err != nil {
		t.Fatalf("expected transition artifact to be stored: %v", err)
	}

	var artifact phaseArtifact
	if err := json.Unmarshal([]byte(stored["artifact_json"]), &artifact); err != nil {
		t.Fatalf("expected persisted transition artifact to be valid JSON: %v", err)
	}
	if artifact.TransitionTo != "execution" {
		t.Fatalf("expected transition target to be recorded, got %q", artifact.TransitionTo)
	}
	if artifact.TransitionFrom != "investigation" {
		t.Fatalf("expected transition source to be recorded, got %q", artifact.TransitionFrom)
	}
	if artifact.TransitionDirection != "forward" {
		t.Fatalf("expected transition direction to be recorded, got %q", artifact.TransitionDirection)
	}
	if !artifact.ExitRequested {
		t.Fatalf("expected exit request to be recorded")
	}
	if len(artifact.NextSteps) != 1 || artifact.NextSteps[0] != "try patch" {
		t.Fatalf("expected next steps to be preserved, got %v", artifact.NextSteps)
	}
}

func TestPlanningPhaseCapabilityReportsForwardHandoff(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	if _, err := cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary":              "Ready to hand off",
		"transition_to":        "execution",
		"transition_direction": "forward",
	}, capabilities.RoutingState{}); err != nil {
		t.Fatalf("planning capture failed: %v", err)
	}

	handoff := cap.Handoff(context.Background(), "continue the workflow", capabilities.RoutingState{})
	if handoff == nil {
		t.Fatal("expected handoff decision for forward transition")
	}
	if handoff.Direction != "forward" {
		t.Fatalf("expected forward direction, got %q", handoff.Direction)
	}
	if handoff.Target != "execution" {
		t.Fatalf("expected execution target, got %q", handoff.Target)
	}
	if handoff.Prompt == "" {
		t.Fatal("expected handoff prompt to be populated")
	}
}

func TestPlanningPhaseCapabilityHonorsCanceledContext(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handoff := cap.Handoff(ctx, "continue the workflow", capabilities.RoutingState{})
	if handoff != nil {
		t.Fatalf("expected canceled context to suppress handoff, got %+v", handoff)
	}
}

func TestPlanningPhaseCapabilityMaturesAcrossIterations(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	_, err := cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary":    "Initial planning sketch",
		"status":     "exploring",
		"compact":    true,
		"next_steps": []string{"inspect parser"},
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("initial planning capture failed: %v", err)
	}

	_, err = cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary":              "Refined implementation plan",
		"status":               "refining",
		"transition_to":        "execution",
		"transition_from":      "investigation",
		"transition_direction": "forward",
		"requirements":         []string{"validate inputs"},
		"plan":                 []string{"inspect parser", "add tests"},
		"notes":                []string{"check existing test harness"},
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("mature planning capture failed: %v", err)
	}

	stored, err := infra.Memory.Load("phase-artifact:planning")
	if err != nil {
		t.Fatalf("expected matured phase artifact to be stored: %v", err)
	}

	var artifact phaseArtifact
	if err := json.Unmarshal([]byte(stored["artifact_json"]), &artifact); err != nil {
		t.Fatalf("expected persisted artifact to be valid JSON: %v", err)
	}
	if artifact.Iteration != 2 {
		t.Fatalf("expected iteration count to increase to 2, got %d", artifact.Iteration)
	}
	if artifact.TransitionTo != "execution" {
		t.Fatalf("expected transition target to be retained, got %q", artifact.TransitionTo)
	}
	if artifact.TransitionFrom != "investigation" {
		t.Fatalf("expected transition source to be retained, got %q", artifact.TransitionFrom)
	}
	if artifact.TransitionDirection != "forward" {
		t.Fatalf("expected transition direction to be retained, got %q", artifact.TransitionDirection)
	}
	if artifact.Summary != "Refined implementation plan" {
		t.Fatalf("expected latest summary to be retained, got %q", artifact.Summary)
	}
	if len(artifact.Requirements) != 1 || artifact.Requirements[0] != "validate inputs" {
		t.Fatalf("expected requirements to be preserved from later iteration, got %v", artifact.Requirements)
	}
	if len(artifact.NextSteps) != 1 || artifact.NextSteps[0] != "inspect parser" {
		t.Fatalf("expected compact iteration details to be preserved, got %v", artifact.NextSteps)
	}
}

type stubWorkQueue struct{ enqueued int }

func (q *stubWorkQueue) Enqueue(item any) error {
	q.enqueued++
	return nil
}

func TestPhaseCapabilityOnAfter_PersistsClarificationAndSkipsTransition(t *testing.T) {
	queue := &stubWorkQueue{}
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory(), GetSessionQueue: func() WorkQueueAccessor { return queue }}
	cap := NewPlanningPhaseCapability(infra)

	_, err := cap.ExecuteTool(context.Background(), "planning_capture", map[string]any{
		"summary":              "Ready to hand off",
		"status":               "complete",
		"transition_to":        "execution",
		"transition_direction": "forward",
	}, capabilities.RoutingState{})
	if err != nil {
		t.Fatalf("planning capture failed: %v", err)
	}

	err = cap.OnAfter(context.Background(), "Find John orders", capabilities.RoutingState{}, capabilities.CapabilityResult{
		Summary: "Which output shape do you want?",
		Data: map[string]any{
			"clarification_request": &ai.ClarificationRequest{
				TargetQuery:       "Find John orders",
				AssistantQuestion: "Which output shape do you want?",
				MissingFact:       "output shape",
				Reason:            "retry_policy_requested_clarification",
				Source:            "execute_script",
				Status:            "pending",
			},
		},
	})
	if err != nil {
		t.Fatalf("OnAfter failed: %v", err)
	}
	if queue.enqueued != 0 {
		t.Fatalf("expected clarification to suppress phase enqueue, got %d queued items", queue.enqueued)
	}
	artifact, err := cap.loadExistingArtifact()
	if err != nil {
		t.Fatalf("loadExistingArtifact failed: %v", err)
	}
	if artifact.Status != "clarifying" {
		t.Fatalf("expected clarifying status, got %q", artifact.Status)
	}
	if artifact.TransitionTo != "" || artifact.TransitionDirection != "" {
		t.Fatalf("expected clarification to clear pending transition, got %+v", artifact)
	}
	request, ok := artifact.StructuredData["clarification_request"].(map[string]any)
	if !ok {
		t.Fatalf("expected clarification request in structured data, got %#v", artifact.StructuredData)
	}
	if request["missing_fact"] != "output shape" || request["source"] != "execute_script" {
		t.Fatalf("unexpected clarification request payload in artifact: %#v", request)
	}
}

func TestPhaseCapability_ClassifySelectsCorrectStartingPhase(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}

	tests := []struct {
		name          string
		query         string
		expectedPhase string
		expectedScore int
		expectedMode  capabilities.CapabilityMode
	}{
		{
			name:          "investigation for fix failing tests",
			query:         "fix failing tests",
			expectedPhase: "investigation",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "investigation for debug error",
			query:         "debug error in authentication",
			expectedPhase: "investigation",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "investigation for broken feature",
			query:         "the login is broken",
			expectedPhase: "investigation",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "planning for design task",
			query:         "design the authentication flow",
			expectedPhase: "planning",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "planning for architecture",
			query:         "architect a caching system",
			expectedPhase: "planning",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "execution for patch application",
			query:         "apply this patch to the config",
			expectedPhase: "execution",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "execution for version update",
			query:         "update the version to 2.0",
			expectedPhase: "execution",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "review for code review",
			query:         "review code for security issues",
			expectedPhase: "review",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "review for validation",
			query:         "verify that the API works",
			expectedPhase: "review",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
		{
			name:          "intake as default",
			query:         "add a new feature for user profiles",
			expectedPhase: "intake",
			expectedScore: 14,
			expectedMode:  capabilities.CapabilityModePrimary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetCap *PhaseCapability
			switch tt.expectedPhase {
			case "intake":
				targetCap = NewIntakePhaseCapability(infra)
			case "investigation":
				targetCap = NewInvestigationPhaseCapability(infra)
			case "planning":
				targetCap = NewPlanningPhaseCapability(infra)
			case "execution":
				targetCap = NewExecutionPhaseCapability(infra)
			case "review":
				targetCap = NewReviewPhaseCapability(infra)
			case "delivery":
				targetCap = NewDeliveryPhaseCapability(infra)
			}

			result := targetCap.Classify(tt.query, capabilities.RoutingState{})

			if !result.Relevant {
				t.Errorf("expected %s phase to be relevant for %q", tt.expectedPhase, tt.query)
			}
			if result.Score != tt.expectedScore {
				t.Errorf("expected score %d, got %d", tt.expectedScore, result.Score)
			}
			if result.Mode != tt.expectedMode {
				t.Errorf("expected mode %v, got %v", tt.expectedMode, result.Mode)
			}
		})
	}
}

func TestPhaseCapability_ClassifyCapabilityHintTakesPriority(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}

	// Create execution phase capability
	executionCap := NewExecutionPhaseCapability(infra)

	// Query matches investigation ("fix"), but hint specifies execution
	result := executionCap.Classify("fix failing tests", capabilities.RoutingState{
		CapabilityHint: "phase_execution",
	})

	if !result.Relevant {
		t.Errorf("expected execution phase to be relevant with explicit hint")
	}
	if result.Score != 15 {
		t.Errorf("expected hint to have score 15 (highest priority), got %d", result.Score)
	}
	if result.Mode != capabilities.CapabilityModePrimary {
		t.Errorf("expected Primary mode with explicit hint, got %v", result.Mode)
	}
}

func TestPhaseCapability_ClassifyKeywordMatchingProvidesSupportingRole(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}

	// Create review phase capability
	reviewCap := NewReviewPhaseCapability(infra)

	// Query "fix failing tests" should start with investigation (Primary, score 14)
	// but review phase has "test" keyword, so it should be Supporting (score 12)
	result := reviewCap.Classify("fix failing tests", capabilities.RoutingState{})

	if !result.Relevant {
		t.Errorf("expected review phase to be relevant due to 'test' keyword")
	}
	if result.Score != 12 {
		t.Errorf("expected keyword match score 12, got %d", result.Score)
	}
	if result.Mode != capabilities.CapabilityModeSupporting {
		t.Errorf("expected Supporting mode for keyword match, got %v", result.Mode)
	}
}

func TestDetectStartingPhase(t *testing.T) {
	tests := []struct {
		query         string
		expectedPhase string
	}{
		{"fix failing tests", "investigation"},
		{"debug authentication error", "investigation"},
		{"the app crashed", "investigation"},
		{"what's wrong with the login", "investigation"},
		{"troubleshoot the issue", "investigation"},
		{"design a caching system", "planning"},
		{"architect the api", "planning"},
		{"how should I implement this", "planning"},
		{"best way to add pagination", "planning"},
		{"apply this patch", "execution"},
		{"update the version", "execution"},
		{"change the config file", "execution"},
		{"review code for bugs", "review"},
		{"audit the security", "review"},
		{"verify that it works", "review"},
		{"add a new feature", "intake"},
		{"implement user profiles", "intake"},
		{"create a dashboard", "intake"},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := detectStartingPhase(strings.ToLower(tt.query))
			if result != tt.expectedPhase {
				t.Errorf("expected phase %s for query %q, got %s", tt.expectedPhase, tt.query, result)
			}
		})
	}
}
