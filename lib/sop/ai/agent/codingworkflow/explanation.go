package codingworkflow

import (
	"fmt"
	"strings"
)

// ExplanationGenerator creates human-readable explanations for KB selection and application
type ExplanationGenerator struct {
	IncludeConfidence bool // Whether to include confidence scores in explanations
	IncludeReasoning  bool // Whether to include detailed reasoning
	VerboseMode       bool // Whether to generate verbose explanations
}

// DefaultExplanationGenerator returns a generator with sensible defaults
func DefaultExplanationGenerator() *ExplanationGenerator {
	return &ExplanationGenerator{
		IncludeConfidence: true,
		IncludeReasoning:  true,
		VerboseMode:       false,
	}
}

// ExplainKBSelection generates an explanation for why specific KBs were selected
func (e *ExplanationGenerator) ExplainKBSelection(classification TaskClassification, selectedKBs []string) string {
	if len(selectedKBs) == 0 {
		return "No Knowledge Bases were selected for this task."
	}

	parts := []string{}

	// Opening statement
	if e.VerboseMode {
		parts = append(parts, fmt.Sprintf("I analyzed your task and classified it as **%s**", classification.TaskType))
		if e.IncludeConfidence && classification.Confidence > 0 {
			parts = append(parts, fmt.Sprintf("with %.0f%% confidence", classification.Confidence*100))
		}
		parts = append(parts, ".")
	}

	// KB selection explanation
	if len(selectedKBs) == 1 {
		parts = append(parts, fmt.Sprintf("\nI selected the **%s** Knowledge Base", selectedKBs[0]))
	} else {
		parts = append(parts, fmt.Sprintf("\nI selected %d Knowledge Bases: %s", len(selectedKBs), formatKBList(selectedKBs)))
	}

	// Reasoning for each selected KB
	if e.IncludeReasoning && len(classification.KBs) > 0 {
		parts = append(parts, "\n\n**Why these Knowledge Bases:**")
		for _, kb := range classification.KBs {
			// Only explain selected KBs
			if containsKB(selectedKBs, kb.Name) {
				reasonText := fmt.Sprintf("\n- **%s**", kb.Name)
				if e.IncludeConfidence {
					reasonText += fmt.Sprintf(" (%.0f%% confidence)", kb.Confidence*100)
				}
				reasonText += ": " + kb.Reason
				parts = append(parts, reasonText)
			}
		}
	}

	// Signals used
	if e.VerboseMode && len(classification.Signals) > 0 {
		parts = append(parts, fmt.Sprintf("\n\nI based this selection on %d signals from your task description and file context.", len(classification.Signals)))
	}

	return strings.Join(parts, "")
}

// ExplainValidationOutcome generates an explanation for validation results
func (e *ExplanationGenerator) ExplainValidationOutcome(result ValidationResult, revisionAttempts int) string {
	if result.Passed {
		return e.explainValidationSuccess(result)
	}
	return e.explainValidationFailure(result, revisionAttempts)
}

func (e *ExplanationGenerator) explainValidationSuccess(result ValidationResult) string {
	parts := []string{}

	if e.VerboseMode {
		parts = append(parts, "✓ The generated code passed all Knowledge Base validation checks.")
	} else {
		parts = append(parts, "✓ Code validated successfully")
	}

	if e.IncludeConfidence && result.ComplianceScore > 0 {
		parts = append(parts, fmt.Sprintf(" (%.0f%% compliance)", result.ComplianceScore*100))
	}

	if e.VerboseMode && len(result.RulesChecked) > 0 {
		parts = append(parts, fmt.Sprintf("\n\nValidated against %d KB rules: %s", len(result.RulesChecked), strings.Join(result.RulesChecked, ", ")))
	}

	return strings.Join(parts, "")
}

func (e *ExplanationGenerator) explainValidationFailure(result ValidationResult, revisionAttempts int) string {
	parts := []string{}

	if revisionAttempts > 0 {
		parts = append(parts, fmt.Sprintf("⚠ After %d revision(s), the code has %d remaining KB rule violation(s)", revisionAttempts, len(result.Violations)))
	} else {
		parts = append(parts, fmt.Sprintf("⚠ The generated code has %d KB rule violation(s)", len(result.Violations)))
	}

	if e.IncludeConfidence && result.ComplianceScore > 0 {
		parts = append(parts, fmt.Sprintf(" (%.0f%% compliance)", result.ComplianceScore*100))
	}

	if e.IncludeReasoning && len(result.Violations) > 0 {
		parts = append(parts, "\n\n**Violations:**")
		for i, v := range result.Violations {
			if i >= 5 && !e.VerboseMode {
				parts = append(parts, fmt.Sprintf("\n- ... and %d more", len(result.Violations)-5))
				break
			}
			parts = append(parts, fmt.Sprintf("\n- **%s** (%s): %s", v.RuleName, v.Severity, v.Description))
			if e.VerboseMode && v.FilePath != "" {
				parts = append(parts, fmt.Sprintf(" [%s%s]", v.FilePath, v.LineHint))
			}
		}
	}

	return strings.Join(parts, "")
}

// ExplainKBImpact generates an explanation of how KBs influenced the code
func (e *ExplanationGenerator) ExplainKBImpact(kbsUsed []string, rulesApplied []string, complianceScore float64) string {
	if len(kbsUsed) == 0 {
		return "No Knowledge Base patterns were applied to this code."
	}

	parts := []string{}

	// KB impact summary
	if len(kbsUsed) == 1 {
		parts = append(parts, fmt.Sprintf("The **%s** Knowledge Base influenced this code", kbsUsed[0]))
	} else {
		parts = append(parts, fmt.Sprintf("%d Knowledge Bases influenced this code: %s", len(kbsUsed), formatKBList(kbsUsed)))
	}

	// Rules applied
	if e.IncludeReasoning && len(rulesApplied) > 0 {
		if len(rulesApplied) <= 3 {
			parts = append(parts, fmt.Sprintf(", applying the following patterns: %s", strings.Join(rulesApplied, ", ")))
		} else if e.VerboseMode {
			parts = append(parts, fmt.Sprintf(", applying %d patterns: %s", len(rulesApplied), strings.Join(rulesApplied, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf(", applying %d patterns", len(rulesApplied)))
		}
	}

	parts = append(parts, ".")

	// Compliance score
	if e.IncludeConfidence && complianceScore > 0 {
		quality := "good"
		if complianceScore >= 0.9 {
			quality = "excellent"
		} else if complianceScore < 0.7 {
			quality = "acceptable"
		}
		parts = append(parts, fmt.Sprintf(" The code achieved %.0f%% compliance (%s quality).", complianceScore*100, quality))
	}

	return strings.Join(parts, "")
}

// ExplainTaskClassification generates an explanation of how the task was classified
func (e *ExplanationGenerator) ExplainTaskClassification(classification TaskClassification) string {
	parts := []string{}

	parts = append(parts, fmt.Sprintf("I classified this as a **%s** task", classification.TaskType))

	if e.IncludeConfidence {
		parts = append(parts, fmt.Sprintf(" (%.0f%% confidence)", classification.Confidence*100))
	}

	if e.VerboseMode && len(classification.Signals) > 0 {
		// Extract signal types
		keywordCount := 0
		fileExtCount := 0
		for _, sig := range classification.Signals {
			if strings.HasPrefix(sig, "keyword:") {
				keywordCount++
			} else if strings.HasPrefix(sig, "file_ext:") {
				fileExtCount++
			}
		}

		signalDesc := []string{}
		if keywordCount > 0 {
			signalDesc = append(signalDesc, fmt.Sprintf("%d keywords", keywordCount))
		}
		if fileExtCount > 0 {
			signalDesc = append(signalDesc, fmt.Sprintf("file extension"))
		}

		if len(signalDesc) > 0 {
			parts = append(parts, fmt.Sprintf(" based on %s", strings.Join(signalDesc, " and ")))
		}
	}

	parts = append(parts, ".")

	return strings.Join(parts, "")
}

// ExplainRevisionProcess generates an explanation of the revision loop
func (e *ExplanationGenerator) ExplainRevisionProcess(attempts int, maxAttempts int, finalPassed bool) string {
	if attempts == 0 {
		if finalPassed {
			return "The code passed validation on the first attempt."
		}
		return "The code did not require revisions."
	}

	parts := []string{}

	if finalPassed {
		if attempts == 1 {
			parts = append(parts, "The code was revised once to fix KB rule violations and now passes validation.")
		} else {
			parts = append(parts, fmt.Sprintf("The code was revised %d times to fix KB rule violations and now passes validation.", attempts))
		}
	} else {
		if attempts >= maxAttempts {
			parts = append(parts, fmt.Sprintf("The code was revised %d times (maximum allowed) but still has violations.", attempts))
		} else {
			parts = append(parts, fmt.Sprintf("The code was revised %d times but still has violations.", attempts))
		}
	}

	if e.VerboseMode {
		parts = append(parts, " This automatic revision process helps enforce KB patterns without manual intervention.")
	}

	return strings.Join(parts, "")
}

// GenerateFullExplanation creates a comprehensive explanation of the entire workflow
func (e *ExplanationGenerator) GenerateFullExplanation(
	classification TaskClassification,
	kbsUsed []string,
	rulesApplied []string,
	validationResult ValidationResult,
	revisionAttempts int,
	complianceScore float64,
) string {
	sections := []string{}

	// Task classification
	sections = append(sections, "## How I Approached This Task\n")
	sections = append(sections, e.ExplainTaskClassification(classification))

	// KB selection
	if len(classification.KBs) > 0 {
		sections = append(sections, "\n\n## Knowledge Bases Selected\n")
		sections = append(sections, e.ExplainKBSelection(classification, kbsUsed))
	}

	// KB impact
	if len(kbsUsed) > 0 {
		sections = append(sections, "\n\n## How KBs Influenced the Code\n")
		sections = append(sections, e.ExplainKBImpact(kbsUsed, rulesApplied, complianceScore))
	}

	// Validation outcome
	sections = append(sections, "\n\n## Validation Results\n")
	sections = append(sections, e.ExplainValidationOutcome(validationResult, revisionAttempts))

	// Revision process
	if revisionAttempts > 0 {
		sections = append(sections, "\n\n## Revision Process\n")
		sections = append(sections, e.ExplainRevisionProcess(revisionAttempts, 2, validationResult.Passed))
	}

	return strings.Join(sections, "")
}

// Helper functions

func formatKBList(kbs []string) string {
	if len(kbs) == 0 {
		return ""
	}
	if len(kbs) == 1 {
		return fmt.Sprintf("**%s**", kbs[0])
	}
	if len(kbs) == 2 {
		return fmt.Sprintf("**%s** and **%s**", kbs[0], kbs[1])
	}

	formatted := []string{}
	for i, kb := range kbs {
		if i == len(kbs)-1 {
			formatted = append(formatted, fmt.Sprintf("and **%s**", kb))
		} else {
			formatted = append(formatted, fmt.Sprintf("**%s**", kb))
		}
	}
	return strings.Join(formatted, ", ")
}

func containsKB(kbs []string, target string) bool {
	for _, kb := range kbs {
		if kb == target {
			return true
		}
	}
	return false
}
