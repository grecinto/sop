package codingworkflow

import (
	"errors"
	"strings"
	"testing"
)

func TestValidationOrchestrator_ValidateAndEnrich_PassesFirstTime(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())
	orchestrator := NewValidationOrchestrator(validator, DefaultCitationConfig())

	patches := []*Patch{
		{
			FilePath: "example.go",
			NewContent: `package main

func processData(input string) (string, error) {
	result, err := transform(input)
	if err != nil {
		return "", err
	}
	return result, nil
}`,
		},
	}

	expertise := ExpertiseCapsule{
		Summary:  "Apply error handling best practices",
		Category: "go-patterns",
		Sources:  []string{"KB:go-kb"},
	}

	finalPatches, validation, attempts, err := orchestrator.ValidateAndEnrich(
		patches,
		expertise,
		"implement data processing",
		nil, // no revision callback needed
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !validation.Passed {
		t.Error("expected validation to pass for clean code")
	}

	if attempts != 0 {
		t.Errorf("expected 0 revision attempts, got %d", attempts)
	}

	if len(finalPatches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(finalPatches))
	}

	// Check that citation was added
	if !strings.Contains(finalPatches[0].NewContent, "KB Patterns") {
		t.Error("expected inline citation to be added")
	}
}

func TestValidationOrchestrator_ValidateAndEnrich_RevisesOnce(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)
	orchestrator := NewValidationOrchestrator(validator, DefaultCitationConfig())

	badPatches := []*Patch{
		{
			FilePath: "example.go",
			NewContent: `package main

func process_data(input string) string {
	data, _ := readFile(input)
	return data
}`,
		},
	}

	goodPatches := []*Patch{
		{
			FilePath: "example.go",
			NewContent: `package main

func processData(input string) (string, error) {
	data, err := readFile(input)
	if err != nil {
		return "", err
	}
	return data, nil
}`,
		},
	}

	expertise := ExpertiseCapsule{
		Summary:  "Apply error handling and naming conventions",
		Category: "go-patterns",
		Sources:  []string{"KB:go-kb"},
	}

	revisionCallCount := 0
	revisionCallback := func(prompt string) ([]*Patch, error) {
		revisionCallCount++
		if !strings.Contains(prompt, "validation issues") {
			t.Error("revision prompt should mention validation issues")
		}
		return goodPatches, nil
	}

	finalPatches, validation, attempts, err := orchestrator.ValidateAndEnrich(
		badPatches,
		expertise,
		"implement data processing",
		revisionCallback,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if revisionCallCount != 1 {
		t.Errorf("expected 1 revision callback, got %d", revisionCallCount)
	}

	if attempts != 1 {
		t.Errorf("expected 1 revision attempt, got %d", attempts)
	}

	if !validation.Passed {
		t.Error("expected validation to pass after revision")
	}

	if len(finalPatches) != 1 {
		t.Fatalf("expected 1 final patch, got %d", len(finalPatches))
	}

	// Should be the good patch, not the bad one
	if strings.Contains(finalPatches[0].NewContent, "process_data") {
		t.Error("expected revised code, still has snake_case")
	}
}

func TestValidationOrchestrator_ValidateAndEnrich_MaxRevisionsLimit(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)
	orchestrator := NewValidationOrchestrator(validator, DefaultCitationConfig())

	badPatches := []*Patch{
		{
			FilePath: "example.go",
			NewContent: `package main
func bad_code() { _ = somethingThatFails() }`,
		},
	}

	expertise := ExpertiseCapsule{
		Summary:  "patterns",
		Category: "go",
		Sources:  []string{"KB:go-kb"},
	}

	revisionCallCount := 0
	revisionCallback := func(prompt string) ([]*Patch, error) {
		revisionCallCount++
		// Always return the same bad code to force max attempts
		return badPatches, nil
	}

	_, _, attempts, err := orchestrator.ValidateAndEnrich(
		badPatches,
		expertise,
		"task",
		revisionCallback,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if attempts != MaxRevisionAttempts {
		t.Errorf("expected %d revision attempts, got %d", MaxRevisionAttempts, attempts)
	}

	if revisionCallCount != MaxRevisionAttempts {
		t.Errorf("expected %d revision callbacks, got %d", MaxRevisionAttempts, revisionCallCount)
	}
}

func TestValidationOrchestrator_ValidateAndEnrich_RevisionError(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())
	orchestrator := NewValidationOrchestrator(validator, DefaultCitationConfig())

	badPatches := []*Patch{
		{
			FilePath:   "example.go",
			NewContent: "package main\nfunc bad() { _ = err }",
		},
	}

	expertise := ExpertiseCapsule{
		Summary:  "patterns",
		Category: "go",
		Sources:  []string{"KB:go-kb"},
	}

	revisionCallback := func(prompt string) ([]*Patch, error) {
		return nil, errors.New("revision failed")
	}

	finalPatches, _, attempts, err := orchestrator.ValidateAndEnrich(
		badPatches,
		expertise,
		"task",
		revisionCallback,
	)

	if err != nil {
		t.Fatalf("should not return error on revision failure, got %v", err)
	}

	if attempts != 0 {
		t.Errorf("expected 0 successful attempts after error, got %d", attempts)
	}

	// Should return original patches when revision fails
	if len(finalPatches) != len(badPatches) {
		t.Error("expected to return original patches on revision failure")
	}
}

func TestValidationOrchestrator_ValidateAndEnrich_RevisionsDisabled(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)
	orchestrator := NewValidationOrchestrator(validator, DefaultCitationConfig())
	orchestrator.SetEnableRevisions(false)

	badPatches := []*Patch{
		{
			FilePath:   "example.go",
			NewContent: "package main\nfunc bad() { _ = err }",
		},
	}

	expertise := ExpertiseCapsule{
		Summary:  "patterns",
		Category: "go",
		Sources:  []string{"KB:go-kb"},
	}

	revisionCallCount := 0
	revisionCallback := func(prompt string) ([]*Patch, error) {
		revisionCallCount++
		return nil, nil
	}

	_, validation, attempts, err := orchestrator.ValidateAndEnrich(
		badPatches,
		expertise,
		"task",
		revisionCallback,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if revisionCallCount != 0 {
		t.Error("revision callback should not be called when revisions disabled")
	}

	if attempts != 0 {
		t.Errorf("expected 0 attempts when revisions disabled, got %d", attempts)
	}

	if validation.Passed {
		t.Error("expected validation to fail for bad code")
	}
}

func TestValidationOrchestrator_ValidateWithoutRevision(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)
	orchestrator := NewValidationOrchestrator(validator, DefaultCitationConfig())

	patches := []*Patch{
		{
			FilePath:   "example.go",
			NewContent: "package main\nfunc bad() { _ = err }",
		},
	}

	expertise := ExpertiseCapsule{
		Summary:  "error handling patterns",
		Category: "go",
		Sources:  []string{"KB:go-kb"},
	}

	result := orchestrator.ValidateWithoutRevision(patches, expertise)

	if result.Passed {
		t.Error("expected validation to fail")
	}

	if len(result.Violations) == 0 {
		t.Error("expected violations to be reported")
	}
}

func TestFormatValidationSummary_Passed(t *testing.T) {
	result := ValidationResult{
		Passed:          true,
		ComplianceScore: 1.0,
		Violations:      []Violation{},
	}

	summary := FormatValidationSummary(result)

	if !strings.Contains(summary, "✓") {
		t.Error("expected checkmark for passed validation")
	}

	if !strings.Contains(summary, "100%") {
		t.Error("expected 100% compliance")
	}
}

func TestFormatValidationSummary_Failed(t *testing.T) {
	result := ValidationResult{
		Passed:          false,
		ComplianceScore: 0.6,
		Violations: []Violation{
			{Severity: SeverityError},
			{Severity: SeverityError},
			{Severity: SeverityWarning},
		},
	}

	summary := FormatValidationSummary(result)

	if !strings.Contains(summary, "⚠") {
		t.Error("expected warning symbol for failed validation")
	}

	if !strings.Contains(summary, "60%") {
		t.Error("expected 60% compliance")
	}

	if !strings.Contains(summary, "2 error(s)") {
		t.Error("expected 2 errors reported")
	}

	if !strings.Contains(summary, "1 warning(s)") {
		t.Error("expected 1 warning reported")
	}
}

func TestCreateCodingMemoryFromValidation(t *testing.T) {
	base := CodingMemory{
		TaskID:      "task-123",
		Description: "implement feature",
		Outcome:     "success",
	}

	result := ValidationResult{
		Passed:          true,
		ComplianceScore: 0.95,
	}

	kbs := []string{"go-kb", "testing-kb"}
	rules := []string{"error-handling", "test-structure"}

	enhanced := CreateCodingMemoryFromValidation(base, result, 1, kbs, rules)

	if enhanced.TaskID != "task-123" {
		t.Error("base fields should be preserved")
	}

	if len(enhanced.KBsUsed) != 2 {
		t.Errorf("expected 2 KBs used, got %d", len(enhanced.KBsUsed))
	}

	if len(enhanced.RulesApplied) != 2 {
		t.Errorf("expected 2 rules applied, got %d", len(enhanced.RulesApplied))
	}

	if enhanced.ComplianceScore != 0.95 {
		t.Errorf("expected compliance score 0.95, got %f", enhanced.ComplianceScore)
	}

	if enhanced.RevisionCount != 1 {
		t.Errorf("expected revision count 1, got %d", enhanced.RevisionCount)
	}
}
