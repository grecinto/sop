package codingworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

func TestPlanningPhaseExposesAssessAndRouteTools(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	tools := cap.ExposeTools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}

	// Check for new Planning-First tools
	expectedTools := []string{
		"planning_assess_and_route",
		"planning_create_succession_path",
	}

	for _, toolName := range expectedTools {
		if !names[toolName] {
			t.Errorf("expected planning capability to expose %s, but it was not found", toolName)
		}
	}
}

func TestPlanningAssessAndRouteDetectsBugFixes(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	result, err := cap.ExecuteTool(context.Background(), "planning_assess_and_route", map[string]any{
		"query": "fix the failing tests in auth module",
	}, capabilities.RoutingState{})

	if err != nil {
		t.Fatalf("assess_and_route failed: %v", err)
	}

	if !strings.Contains(result, "investigation") {
		t.Errorf("expected assessment to recommend investigation phase for bug fix, got: %s", result)
	}

	if !strings.Contains(result, "Bug/error fixing") {
		t.Errorf("expected assessment to identify bug/error characteristics, got: %s", result)
	}
}

func TestPlanningAssessAndRouteDetectsFeatures(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	result, err := cap.ExecuteTool(context.Background(), "planning_assess_and_route", map[string]any{
		"query": "add Redis caching to user service",
	}, capabilities.RoutingState{})

	if err != nil {
		t.Fatalf("assess_and_route failed: %v", err)
	}

	if !strings.Contains(result, "Feature implementation") {
		t.Errorf("expected assessment to identify feature implementation, got: %s", result)
	}
}

func TestPlanningCreateSuccessionPathValidatesPhases(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	// Valid phases should succeed
	result, err := cap.ExecuteTool(context.Background(), "planning_create_succession_path", map[string]any{
		"phases":      []any{"investigation", "planning", "execution", "review", "delivery"},
		"checkpoints": []any{"after_execution"},
		"reasoning":   "Need to investigate bug, plan fix, execute, and validate",
		"confidence":  "high",
	}, capabilities.RoutingState{})

	if err != nil {
		t.Fatalf("create_succession_path failed: %v", err)
	}

	if !strings.Contains(result, "investigation → planning → execution → review → delivery") {
		t.Errorf("expected path to be formatted correctly, got: %s", result)
	}

	if !strings.Contains(result, "after_execution") {
		t.Errorf("expected checkpoints to be included, got: %s", result)
	}

	// Invalid phase should fail
	_, err = cap.ExecuteTool(context.Background(), "planning_create_succession_path", map[string]any{
		"phases": []any{"invalid_phase"},
	}, capabilities.RoutingState{})

	if err == nil {
		t.Error("expected create_succession_path to reject invalid phase name")
	}

	if !strings.Contains(err.Error(), "invalid phase name") {
		t.Errorf("expected error to mention invalid phase name, got: %v", err)
	}
}

func TestPlanningCreateSuccessionPathRequiresPhases(t *testing.T) {
	infra := &SharedInfrastructure{Memory: NewInMemorySessionMemory()}
	cap := NewPlanningPhaseCapability(infra)

	_, err := cap.ExecuteTool(context.Background(), "planning_create_succession_path", map[string]any{
		"reasoning": "some reasoning",
	}, capabilities.RoutingState{})

	if err == nil {
		t.Error("expected create_succession_path to require phases array")
	}

	if !strings.Contains(err.Error(), "phases array is required") {
		t.Errorf("expected error to mention required phases, got: %v", err)
	}
}
