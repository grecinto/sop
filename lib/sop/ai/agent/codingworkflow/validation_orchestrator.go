package codingworkflow

import (
	"fmt"
	"strings"
)

// MaxRevisionAttempts limits how many times we'll try to fix validation issues.
const MaxRevisionAttempts = 2

// ValidationOrchestrator manages the validation and revision loop for code generation.
type ValidationOrchestrator struct {
	validator       *Validator
	citationConfig  CitationConfig
	enableRevisions bool
}

// NewValidationOrchestrator creates a new validation orchestrator.
func NewValidationOrchestrator(validator *Validator, citationConfig CitationConfig) *ValidationOrchestrator {
	return &ValidationOrchestrator{
		validator:       validator,
		citationConfig:  citationConfig,
		enableRevisions: true,
	}
}

// ValidateAndEnrich validates patches and adds citations if validation passes.
// Returns the (possibly revised) patches, validation result, and number of revision attempts.
func (vo *ValidationOrchestrator) ValidateAndEnrich(
	patches []*Patch,
	expertise ExpertiseCapsule,
	taskDescription string,
	revisionCallback func(revisionPrompt string) ([]*Patch, error),
) ([]*Patch, ValidationResult, int, error) {

	if len(patches) == 0 {
		return patches, ValidationResult{Passed: true, ComplianceScore: 1.0}, 0, nil
	}

	currentPatches := patches
	revisionAttempts := 0
	var lastValidation ValidationResult

	// Extract KB information for citations
	kbNames := ExtractKBNamesFromExpertise(expertise)
	rules := ExtractRulesFromExpertise(expertise)

	// Validation and revision loop
	for revisionAttempts <= MaxRevisionAttempts {
		// Validate current patches
		lastValidation = vo.validator.ValidatePatches(currentPatches, rules)

		// If validation passed or revisions disabled, break
		if lastValidation.Passed || !vo.enableRevisions {
			break
		}

		// If we've hit max attempts, stop trying
		if revisionAttempts >= MaxRevisionAttempts {
			break
		}

		// Build revision prompt from violations
		revisionPrompt := BuildRevisionPrompt(lastValidation.Violations, taskDescription)
		if revisionPrompt == "" {
			break
		}

		// Call back to get revised patches
		if revisionCallback == nil {
			break
		}

		revisedPatches, err := revisionCallback(revisionPrompt)
		if err != nil {
			// Revision failed, keep current patches
			break
		}

		if len(revisedPatches) == 0 {
			// No revised patches produced, keep current
			break
		}

		currentPatches = revisedPatches
		revisionAttempts++
	}

	// Add inline citations to final patches
	if vo.citationConfig.Enabled && len(rules) > 0 {
		currentPatches = vo.addCitationsToPatches(currentPatches, kbNames, rules)
	}

	return currentPatches, lastValidation, revisionAttempts, nil
}

// addCitationsToPatches adds inline citations to all patches.
func (vo *ValidationOrchestrator) addCitationsToPatches(patches []*Patch, kbNames []string, rules []string) []*Patch {
	result := make([]*Patch, len(patches))

	for i, patch := range patches {
		enrichedPatch := *patch // Copy
		enrichedPatch.NewContent = AddInlineCitations(
			patch.NewContent,
			kbNames,
			rules,
			vo.citationConfig,
		)
		result[i] = &enrichedPatch
	}

	return result
}

// ValidateWithoutRevision just validates patches without attempting fixes.
// Useful for reporting or testing.
func (vo *ValidationOrchestrator) ValidateWithoutRevision(patches []*Patch, expertise ExpertiseCapsule) ValidationResult {
	rules := ExtractRulesFromExpertise(expertise)
	return vo.validator.ValidatePatches(patches, rules)
}

// SetEnableRevisions controls whether the orchestrator will attempt revisions.
func (vo *ValidationOrchestrator) SetEnableRevisions(enable bool) {
	vo.enableRevisions = enable
}

// FormatValidationSummary creates a human-readable summary of validation results.
func FormatValidationSummary(result ValidationResult) string {
	if result.Passed {
		return fmt.Sprintf("✓ Validation passed (compliance: %.0f%%)", result.ComplianceScore*100)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("⚠ Validation issues found (compliance: %.0f%%):\n", result.ComplianceScore*100))

	// Group by severity
	errors := 0
	warnings := 0
	for _, v := range result.Violations {
		if v.Severity == SeverityError {
			errors++
		} else if v.Severity == SeverityWarning {
			warnings++
		}
	}

	if errors > 0 {
		b.WriteString(fmt.Sprintf("  %d error(s)\n", errors))
	}
	if warnings > 0 {
		b.WriteString(fmt.Sprintf("  %d warning(s)\n", warnings))
	}

	return b.String()
}

// CreateCodingMemoryFromValidation enriches a CodingMemory with validation data.
func CreateCodingMemoryFromValidation(
	base CodingMemory,
	result ValidationResult,
	revisionAttempts int,
	kbNames []string,
	rules []string,
) CodingMemory {
	enhanced := base
	enhanced.KBsUsed = kbNames
	enhanced.RulesApplied = rules
	enhanced.ComplianceScore = result.ComplianceScore
	enhanced.RevisionCount = revisionAttempts

	return enhanced
}
