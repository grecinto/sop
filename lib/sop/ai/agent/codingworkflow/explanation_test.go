package codingworkflow

import (
	"strings"
	"testing"
)

func TestExplanationGenerator_ExplainKBSelection_SingleKB(t *testing.T) {
	gen := DefaultExplanationGenerator()
	gen.VerboseMode = false

	classification := TaskClassification{
		TaskType:   TaskTypeImplementation,
		Confidence: 0.8,
		KBs: []KBCandidate{
			{Name: "go_best_practices", Confidence: 0.9, Reason: "Go language file"},
		},
	}

	explanation := gen.ExplainKBSelection(classification, []string{"go_best_practices"})

	if !strings.Contains(explanation, "go_best_practices") {
		t.Errorf("expected KB name in explanation, got %q", explanation)
	}
	if !strings.Contains(explanation, "Go language file") {
		t.Errorf("expected reason in explanation, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainKBSelection_MultipleKBs(t *testing.T) {
	gen := DefaultExplanationGenerator()

	classification := TaskClassification{
		TaskType:   TaskTypeImplementation,
		Confidence: 0.8,
		KBs: []KBCandidate{
			{Name: "go_best_practices", Confidence: 0.9, Reason: "Go language file"},
			{Name: "architecture_patterns", Confidence: 0.7, Reason: "Implementation task"},
		},
	}

	explanation := gen.ExplainKBSelection(classification, []string{"go_best_practices", "architecture_patterns"})

	if !strings.Contains(explanation, "2 Knowledge Bases") {
		t.Errorf("expected count of KBs, got %q", explanation)
	}
	if !strings.Contains(explanation, "go_best_practices") {
		t.Errorf("expected first KB, got %q", explanation)
	}
	if !strings.Contains(explanation, "architecture_patterns") {
		t.Errorf("expected second KB, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainKBSelection_NoKBs(t *testing.T) {
	gen := DefaultExplanationGenerator()

	classification := TaskClassification{
		TaskType:   TaskTypeImplementation,
		Confidence: 0.5,
	}

	explanation := gen.ExplainKBSelection(classification, []string{})

	if !strings.Contains(explanation, "No Knowledge Bases") {
		t.Errorf("expected 'No Knowledge Bases' message, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainKBSelection_VerboseMode(t *testing.T) {
	gen := DefaultExplanationGenerator()
	gen.VerboseMode = true

	classification := TaskClassification{
		TaskType:   TaskTypeDebugging,
		Confidence: 0.9,
		KBs: []KBCandidate{
			{Name: "debugging_guide", Confidence: 0.95, Reason: "Debugging task"},
		},
		Signals: []string{"keyword:fix", "keyword:debug"},
	}

	explanation := gen.ExplainKBSelection(classification, []string{"debugging_guide"})

	if !strings.Contains(explanation, "debugging") {
		t.Errorf("expected task type in verbose explanation, got %q", explanation)
	}
	if !strings.Contains(explanation, "signals") {
		t.Errorf("expected signals mention in verbose explanation, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainValidationOutcome_Success(t *testing.T) {
	gen := DefaultExplanationGenerator()

	result := ValidationResult{
		Passed:          true,
		ComplianceScore: 0.95,
		RulesChecked:    []string{"error-handling", "naming"},
	}

	explanation := gen.ExplainValidationOutcome(result, 0)

	if !strings.Contains(explanation, "✓") || (!strings.Contains(explanation, "passed") && !strings.Contains(explanation, "validated successfully")) {
		t.Errorf("expected success indicator, got %q", explanation)
	}
	if !strings.Contains(explanation, "95%") {
		t.Errorf("expected compliance score, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainValidationOutcome_Failure(t *testing.T) {
	gen := DefaultExplanationGenerator()

	result := ValidationResult{
		Passed:          false,
		ComplianceScore: 0.6,
		Violations: []Violation{
			{RuleName: "error-handling", Severity: "error", Description: "ignored error"},
			{RuleName: "naming", Severity: "warning", Description: "snake_case name"},
		},
		RulesChecked: []string{"error-handling", "naming"},
	}

	explanation := gen.ExplainValidationOutcome(result, 1)

	if !strings.Contains(explanation, "⚠") || !strings.Contains(explanation, "violation") {
		t.Errorf("expected failure indicator, got %q", explanation)
	}
	if !strings.Contains(explanation, "2") {
		t.Errorf("expected violation count, got %q", explanation)
	}
	if !strings.Contains(explanation, "error-handling") {
		t.Errorf("expected violation details, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainKBImpact(t *testing.T) {
	gen := DefaultExplanationGenerator()

	explanation := gen.ExplainKBImpact(
		[]string{"go_best_practices"},
		[]string{"error-handling", "defer-cleanup"},
		0.9,
	)

	if !strings.Contains(explanation, "go_best_practices") {
		t.Errorf("expected KB name, got %q", explanation)
	}
	if !strings.Contains(explanation, "90%") {
		t.Errorf("expected compliance score, got %q", explanation)
	}
	if !strings.Contains(explanation, "excellent") {
		t.Errorf("expected quality description, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainKBImpact_NoKBs(t *testing.T) {
	gen := DefaultExplanationGenerator()

	explanation := gen.ExplainKBImpact([]string{}, []string{}, 0.0)

	if !strings.Contains(explanation, "No Knowledge Base") {
		t.Errorf("expected 'No Knowledge Base' message, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainTaskClassification(t *testing.T) {
	gen := DefaultExplanationGenerator()

	classification := TaskClassification{
		TaskType:   TaskTypeTesting,
		Confidence: 0.85,
		Signals:    []string{"keyword:test", "file_ext:.go"},
	}

	explanation := gen.ExplainTaskClassification(classification)

	if !strings.Contains(explanation, "testing") {
		t.Errorf("expected task type, got %q", explanation)
	}
	if !strings.Contains(explanation, "85%") {
		t.Errorf("expected confidence, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainRevisionProcess_NoRevisions(t *testing.T) {
	gen := DefaultExplanationGenerator()

	explanation := gen.ExplainRevisionProcess(0, 2, true)

	if !strings.Contains(explanation, "first attempt") {
		t.Errorf("expected 'first attempt' message, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainRevisionProcess_SuccessfulRevision(t *testing.T) {
	gen := DefaultExplanationGenerator()

	explanation := gen.ExplainRevisionProcess(1, 2, true)

	if !strings.Contains(explanation, "revised once") {
		t.Errorf("expected 'revised once' message, got %q", explanation)
	}
	if !strings.Contains(explanation, "passes validation") {
		t.Errorf("expected success message, got %q", explanation)
	}
}

func TestExplanationGenerator_ExplainRevisionProcess_MaxAttempts(t *testing.T) {
	gen := DefaultExplanationGenerator()

	explanation := gen.ExplainRevisionProcess(2, 2, false)

	if !strings.Contains(explanation, "maximum allowed") {
		t.Errorf("expected 'maximum allowed' message, got %q", explanation)
	}
	if !strings.Contains(explanation, "still has violations") {
		t.Errorf("expected failure message, got %q", explanation)
	}
}

func TestExplanationGenerator_GenerateFullExplanation(t *testing.T) {
	gen := DefaultExplanationGenerator()
	gen.VerboseMode = false

	classification := TaskClassification{
		TaskType:   TaskTypeImplementation,
		Confidence: 0.8,
		KBs: []KBCandidate{
			{Name: "go_best_practices", Confidence: 0.9, Reason: "Go file"},
		},
	}

	result := ValidationResult{
		Passed:          true,
		ComplianceScore: 0.95,
		RulesChecked:    []string{"error-handling"},
	}

	explanation := gen.GenerateFullExplanation(
		classification,
		[]string{"go_best_practices"},
		[]string{"error-handling"},
		result,
		0,
		0.95,
	)

	// Should contain all sections
	if !strings.Contains(explanation, "## How I Approached This Task") {
		t.Error("expected task approach section")
	}
	if !strings.Contains(explanation, "## Knowledge Bases Selected") {
		t.Error("expected KB selection section")
	}
	if !strings.Contains(explanation, "## How KBs Influenced the Code") {
		t.Error("expected KB impact section")
	}
	if !strings.Contains(explanation, "## Validation Results") {
		t.Error("expected validation section")
	}
}

func TestExplanationGenerator_IncludeConfidence_False(t *testing.T) {
	gen := DefaultExplanationGenerator()
	gen.IncludeConfidence = false

	classification := TaskClassification{
		TaskType:   TaskTypeImplementation,
		Confidence: 0.8,
		KBs: []KBCandidate{
			{Name: "go_best_practices", Confidence: 0.9, Reason: "Go file"},
		},
	}

	explanation := gen.ExplainKBSelection(classification, []string{"go_best_practices"})

	if strings.Contains(explanation, "%") {
		t.Errorf("should not include confidence percentages, got %q", explanation)
	}
}

func TestExplanationGenerator_IncludeReasoning_False(t *testing.T) {
	gen := DefaultExplanationGenerator()
	gen.IncludeReasoning = false

	classification := TaskClassification{
		TaskType:   TaskTypeImplementation,
		Confidence: 0.8,
		KBs: []KBCandidate{
			{Name: "go_best_practices", Confidence: 0.9, Reason: "Go file"},
		},
	}

	explanation := gen.ExplainKBSelection(classification, []string{"go_best_practices"})

	if strings.Contains(explanation, "Why these Knowledge Bases") {
		t.Errorf("should not include reasoning section, got %q", explanation)
	}
}

func TestExplanationGenerator_VerboseMode_ValidationFailure(t *testing.T) {
	gen := DefaultExplanationGenerator()
	gen.VerboseMode = true

	result := ValidationResult{
		Passed:          false,
		ComplianceScore: 0.6,
		Violations: []Violation{
			{RuleName: "error-handling", Severity: "error", Description: "ignored error", FilePath: "main.go", LineHint: ":10"},
		},
	}

	explanation := gen.ExplainValidationOutcome(result, 0)

	if !strings.Contains(explanation, "main.go") {
		t.Errorf("verbose mode should include file paths, got %q", explanation)
	}
	if !strings.Contains(explanation, "10") {
		t.Errorf("verbose mode should include line numbers, got %q", explanation)
	}
}

func TestExplanationGenerator_FormatKBList(t *testing.T) {
	tests := []struct {
		name     string
		kbs      []string
		expected string
	}{
		{
			name:     "single KB",
			kbs:      []string{"kb1"},
			expected: "**kb1**",
		},
		{
			name:     "two KBs",
			kbs:      []string{"kb1", "kb2"},
			expected: "**kb1** and **kb2**",
		},
		{
			name:     "three KBs",
			kbs:      []string{"kb1", "kb2", "kb3"},
			expected: "**kb1**, **kb2**, and **kb3**",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatKBList(tt.kbs)
			if result != tt.expected {
				t.Errorf("formatKBList(%v) = %q, want %q", tt.kbs, result, tt.expected)
			}
		})
	}
}

func TestExplanationGenerator_QualityDescriptions(t *testing.T) {
	gen := DefaultExplanationGenerator()

	tests := []struct {
		name            string
		complianceScore float64
		expectedQuality string
	}{
		{"excellent quality", 0.95, "excellent"},
		{"good quality", 0.8, "good"},
		{"acceptable quality", 0.65, "acceptable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explanation := gen.ExplainKBImpact(
				[]string{"test_kb"},
				[]string{"rule1"},
				tt.complianceScore,
			)
			if !strings.Contains(explanation, tt.expectedQuality) {
				t.Errorf("expected quality %q for score %.2f, got %q", tt.expectedQuality, tt.complianceScore, explanation)
			}
		})
	}
}
