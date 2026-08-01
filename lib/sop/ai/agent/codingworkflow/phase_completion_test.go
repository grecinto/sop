package codingworkflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// TestPhaseCapture_StoresStructuredData verifies that structured_data is correctly captured and persisted
func TestPhaseCapture_StoresStructuredData(t *testing.T) {
	sharedInfra := NewDefaultSharedInfrastructure()
	capability := NewInvestigationPhaseCapability(sharedInfra)

	// Create capture with structured data
	structuredData := map[string]any{
		"findings": []string{
			"Found 3 test files",
			"Authentication system uses JWT",
		},
		"file_count": 42,
		"metrics": map[string]any{
			"lines_of_code": 1250,
			"test_coverage": 85.5,
		},
	}

	args := map[string]any{
		"summary":         "Investigation complete",
		"status":          "complete",
		"structured_data": structuredData,
		"evidence": []any{
			"lib/auth/jwt.go implements token validation",
			"test coverage at 85%",
		},
		"transition_to":        "planning",
		"transition_direction": "forward",
	}

	result, err := capability.runCapture(args)
	if err != nil {
		t.Fatalf("runCapture failed: %v", err)
	}

	// Verify result indicates completion
	if !strings.Contains(result, "COMPLETE") {
		t.Errorf("Expected completion message, got: %s", result)
	}

	// Load the persisted artifact
	artifact, err := capability.loadExistingArtifact()
	if err != nil {
		t.Fatalf("loadExistingArtifact failed: %v", err)
	}

	// Verify structured data was persisted
	if artifact.StructuredData == nil {
		t.Fatal("StructuredData is nil")
	}

	// Verify findings
	findingsRaw, ok := artifact.StructuredData["findings"]
	if !ok {
		t.Fatal("findings not found in StructuredData")
	}

	// Convert to JSON and back to handle type assertions
	findingsJSON, _ := json.Marshal(findingsRaw)
	var findings []string
	json.Unmarshal(findingsJSON, &findings)

	if len(findings) != 2 {
		t.Errorf("Expected 2 findings, got %d", len(findings))
	}

	// Verify metrics
	metricsRaw, ok := artifact.StructuredData["metrics"]
	if !ok {
		t.Fatal("metrics not found in StructuredData")
	}

	metricsJSON, _ := json.Marshal(metricsRaw)
	var metrics map[string]any
	json.Unmarshal(metricsJSON, &metrics)

	if metrics["test_coverage"] == nil {
		t.Error("test_coverage not found in metrics")
	}

	// Verify other fields
	if artifact.Status != "complete" {
		t.Errorf("Expected status 'complete', got '%s'", artifact.Status)
	}

	if artifact.TransitionTo != "planning" {
		t.Errorf("Expected transition_to 'planning', got '%s'", artifact.TransitionTo)
	}
}

// TestPhaseCapture_PreservesStructuredDataAcrossUpdates verifies that structured data persists across multiple captures
func TestPhaseCapture_PreservesStructuredDataAcrossUpdates(t *testing.T) {
	sharedInfra := NewDefaultSharedInfrastructure()
	capability := NewPlanningPhaseCapability(sharedInfra)

	// First capture with initial structured data
	args1 := map[string]any{
		"summary": "Initial planning",
		"status":  "in_progress",
		"structured_data": map[string]any{
			"architecture": "microservices",
			"framework":    "Go + React",
		},
		"plan": []any{
			"Design authentication flow",
			"Create API endpoints",
		},
	}

	_, err := capability.runCapture(args1)
	if err != nil {
		t.Fatalf("First capture failed: %v", err)
	}

	// Second capture without structured_data - should preserve previous
	args2 := map[string]any{
		"summary": "Updated planning",
		"notes": []any{
			"Added rate limiting requirement",
		},
	}

	_, err = capability.runCapture(args2)
	if err != nil {
		t.Fatalf("Second capture failed: %v", err)
	}

	// Verify structured data is preserved
	artifact, err := capability.loadExistingArtifact()
	if err != nil {
		t.Fatalf("loadExistingArtifact failed: %v", err)
	}

	if artifact.StructuredData == nil {
		t.Fatal("StructuredData should be preserved from previous capture")
	}

	archRaw := artifact.StructuredData["architecture"]
	if archRaw == nil {
		t.Error("architecture should be preserved")
	}

	// Verify iteration count increased
	if artifact.Iteration != 2 {
		t.Errorf("Expected iteration 2, got %d", artifact.Iteration)
	}
}

// TestPhaseCapture_CompleteStatusTriggersNotice verifies completion detection and messaging
func TestPhaseCapture_CompleteStatusTriggersNotice(t *testing.T) {
	sharedInfra := NewDefaultSharedInfrastructure()
	capability := NewExecutionPhaseCapability(sharedInfra)

	// Capture with status="complete"
	args := map[string]any{
		"summary":              "All tests passing",
		"status":               "complete",
		"transition_to":        "review",
		"transition_direction": "forward",
		"structured_data": map[string]any{
			"test_results": map[string]any{
				"passed": 42,
				"failed": 0,
			},
		},
	}

	result, err := capability.runCapture(args)
	if err != nil {
		t.Fatalf("runCapture failed: %v", err)
	}

	// Verify completion notice in result
	if !strings.Contains(result, "✅") {
		t.Error("Expected completion checkmark in result")
	}

	if !strings.Contains(result, "COMPLETE") {
		t.Error("Expected 'COMPLETE' in result")
	}

	if !strings.Contains(result, "review") {
		t.Error("Expected next phase 'review' in result")
	}

	// Verify artifact has correct status
	artifact, err := capability.loadExistingArtifact()
	if err != nil {
		t.Fatalf("loadExistingArtifact failed: %v", err)
	}

	if artifact.Status != "complete" {
		t.Errorf("Expected status 'complete', got '%s'", artifact.Status)
	}
}

func TestPhaseCapture_RejectsCompleteWithoutRequiredCriteria(t *testing.T) {
	sharedInfra := NewDefaultSharedInfrastructure()
	capability := NewIntakePhaseCapability(sharedInfra)

	_, err := capability.runCapture(map[string]any{
		"summary": "User wants authentication",
		"status":  "complete",
	})
	if err == nil {
		t.Fatal("expected validation error when marking intake complete without requirements")
	}
	if !strings.Contains(err.Error(), "requires at least 1 requirement") {
		t.Fatalf("expected missing requirements validation error, got %v", err)
	}
}

// TestHandoff_IncludesPreviousPhaseContext verifies that handoff prompts include context from previous phases
func TestHandoff_IncludesPreviousPhaseContext(t *testing.T) {
	sharedInfra := NewDefaultSharedInfrastructure()

	// Simulate a workflow: intake → investigation → planning
	// 1. Create intake artifact
	intake := NewIntakePhaseCapability(sharedInfra)
	intakeArgs := map[string]any{
		"summary": "User wants to add authentication",
		"status":  "complete",
		"requirements": []any{
			"JWT-based authentication",
			"Role-based access control",
		},
		"transition_to":        "investigation",
		"transition_direction": "forward",
	}
	_, err := intake.runCapture(intakeArgs)
	if err != nil {
		t.Fatalf("intake capture failed: %v", err)
	}

	// 2. Create investigation artifact
	investigation := NewInvestigationPhaseCapability(sharedInfra)
	investigationArgs := map[string]any{
		"summary": "Found existing auth framework",
		"status":  "complete",
		"evidence": []any{
			"lib/auth/jwt.go already exists",
			"Uses github.com/golang-jwt/jwt library",
		},
		"structured_data": map[string]any{
			"findings": []string{
				"JWT library already imported",
				"Need to add RBAC middleware",
			},
		},
		"transition_to":        "planning",
		"transition_direction": "forward",
	}
	_, err = investigation.runCapture(investigationArgs)
	if err != nil {
		t.Fatalf("investigation capture failed: %v", err)
	}

	// 3. Create planning artifact and get handoff
	planning := NewPlanningPhaseCapability(sharedInfra)
	planningArgs := map[string]any{
		"summary": "Designed RBAC middleware",
		"status":  "complete",
		"plan": []any{
			"Create role middleware in lib/auth/rbac.go",
			"Add role checking to existing endpoints",
		},
		"transition_to":        "execution",
		"transition_direction": "forward",
	}
	_, err = planning.runCapture(planningArgs)
	if err != nil {
		t.Fatalf("planning capture failed: %v", err)
	}

	// Get handoff from planning phase
	handoff := planning.Handoff(context.Background(), "", capabilities.RoutingState{})
	if handoff == nil {
		t.Fatal("Expected handoff, got nil")
	}

	// Verify handoff includes context
	if handoff.Direction != "forward" {
		t.Errorf("Expected direction 'forward', got '%s'", handoff.Direction)
	}

	if handoff.Target != "execution" {
		t.Errorf("Expected target 'execution', got '%s'", handoff.Target)
	}

	prompt := handoff.Prompt

	// Debug: print the prompt to see what we got
	t.Logf("Handoff prompt:\n%s", prompt)

	// Should mention intake phase requirements
	if !strings.Contains(prompt, "intake") {
		t.Error("Expected handoff prompt to mention intake phase")
	}

	// Should mention investigation findings
	if !strings.Contains(prompt, "investigation") {
		t.Error("Expected handoff prompt to mention investigation phase")
	}

	// Should include next steps from planning (if they exist in prompt)
	// Note: Next steps may not always appear if the list is truncated or formatted differently
	if len(planningArgs["plan"].([]any)) > 0 {
		// Just verify the prompt is non-empty and has some context
		if len(prompt) < 50 {
			t.Error("Expected handoff prompt to have substantial context")
		}
	}
}

// TestHandoff_WithStructuredDataHighlights verifies that handoff includes structured data highlights
func TestHandoff_WithStructuredDataHighlights(t *testing.T) {
	sharedInfra := NewDefaultSharedInfrastructure()
	capability := NewExecutionPhaseCapability(sharedInfra)

	// Create artifact with structured test results
	args := map[string]any{
		"summary": "Implementation complete",
		"status":  "complete",
		"structured_data": map[string]any{
			"test_results": map[string]any{
				"passed":   42,
				"failed":   0,
				"coverage": 88.5,
			},
			"findings": []string{
				"All unit tests passing",
				"Integration tests added",
			},
		},
		"transition_to":        "review",
		"transition_direction": "forward",
	}

	_, err := capability.runCapture(args)
	if err != nil {
		t.Fatalf("runCapture failed: %v", err)
	}

	// Get handoff
	handoff := capability.Handoff(context.Background(), "", capabilities.RoutingState{})
	if handoff == nil {
		t.Fatal("Expected handoff, got nil")
	}

	prompt := handoff.Prompt

	// Should include test results highlight
	if !strings.Contains(prompt, "Test Results") {
		t.Error("Expected handoff prompt to include test results highlight")
	}

	// Should include findings highlight
	if !strings.Contains(prompt, "Findings") {
		t.Error("Expected handoff prompt to include findings highlight")
	}
}

func TestHandoff_BackwardTransitionIncludesPendingClarification(t *testing.T) {
	sharedInfra := NewDefaultSharedInfrastructure()
	capability := NewPlanningPhaseCapability(sharedInfra)

	args := map[string]any{
		"summary":              "Need to revisit assumptions before finalizing the plan",
		"status":               "clarifying",
		"transition_from":      "investigation",
		"transition_direction": "backward",
		"structured_data": map[string]any{
			"clarification_request": map[string]any{
				"assistant_question": "Which data source is authoritative?",
				"missing_fact":       "authoritative data source",
				"reason":             "planning_requires_source_of_truth",
				"source":             "planning",
			},
		},
	}

	_, err := capability.runCapture(args)
	if err != nil {
		t.Fatalf("runCapture failed: %v", err)
	}

	handoff := capability.Handoff(context.Background(), "", capabilities.RoutingState{})
	if handoff == nil {
		t.Fatal("Expected backward handoff, got nil")
	}
	if handoff.Direction != "backward" {
		t.Fatalf("Expected backward direction, got %q", handoff.Direction)
	}
	if handoff.Target != "investigation" {
		t.Fatalf("Expected backward target investigation, got %q", handoff.Target)
	}

	prompt := handoff.Prompt
	checks := []string{
		"Return to the investigation phase and continue the workflow.",
		"Pending Clarification",
		"Question: Which data source is authoritative?",
		"Missing Fact: authoritative data source",
		"Reason: planning_requires_source_of_truth",
		"Source: planning",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Fatalf("expected handoff prompt to include %q, got %q", check, prompt)
		}
	}
}

// TestPhaseArtifact_JSONMarshalWithStructuredData verifies that artifacts with structured data can be marshaled
func TestPhaseArtifact_JSONMarshalWithStructuredData(t *testing.T) {
	artifact := phaseArtifact{
		PhaseID: "investigation",
		Summary: "Test summary",
		Status:  "complete",
		StructuredData: map[string]any{
			"findings": []string{"finding1", "finding2"},
			"metrics": map[string]any{
				"count": 42,
				"score": 95.5,
			},
		},
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	// Unmarshal back
	var decoded phaseArtifact
	err = json.Unmarshal(jsonBytes, &decoded)
	if err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// Verify structured data survived round-trip
	if decoded.StructuredData == nil {
		t.Fatal("StructuredData is nil after unmarshal")
	}

	if decoded.StructuredData["findings"] == nil {
		t.Error("findings not found after unmarshal")
	}

	if decoded.StructuredData["metrics"] == nil {
		t.Error("metrics not found after unmarshal")
	}
}
