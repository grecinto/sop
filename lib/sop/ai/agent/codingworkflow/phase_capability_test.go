package codingworkflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
