package codingworkflow

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationResult captures the outcome of validating generated code against KB rules.
type ValidationResult struct {
	Passed          bool
	ComplianceScore float64 // 0.0 to 1.0
	Violations      []Violation
	RulesChecked    []string
}

// Violation describes a specific rule violation in generated code.
type Violation struct {
	RuleName    string
	Severity    ViolationSeverity
	Description string
	FilePath    string
	LineHint    string
	Suggestion  string
}

// ViolationSeverity indicates how critical a violation is.
type ViolationSeverity string

const (
	SeverityError   ViolationSeverity = "error"
	SeverityWarning ViolationSeverity = "warning"
	SeverityInfo    ViolationSeverity = "info"
)

// ValidatorConfig controls which rules are checked and their weights.
type ValidatorConfig struct {
	EnableErrorHandling bool
	EnableNaming        bool
	EnableTestStructure bool
	EnableComments      bool
	StrictMode          bool // If true, warnings also count as violations
}

// DefaultValidatorConfig returns a sensible default configuration.
func DefaultValidatorConfig() ValidatorConfig {
	return ValidatorConfig{
		EnableErrorHandling: true,
		EnableNaming:        true,
		EnableTestStructure: true,
		EnableComments:      false, // Start lenient
		StrictMode:          false,
	}
}

// Validator checks generated code against KB patterns and best practices.
type Validator struct {
	config ValidatorConfig
}

// NewValidator creates a validator with the given configuration.
func NewValidator(config ValidatorConfig) *Validator {
	return &Validator{config: config}
}

// ValidatePatch validates a single patch against KB rules.
func (v *Validator) ValidatePatch(patch *Patch, kbRules []string) ValidationResult {
	if patch == nil {
		return ValidationResult{Passed: true, ComplianceScore: 1.0}
	}

	violations := make([]Violation, 0)
	rulesChecked := make([]string, 0)

	// Check new content for violations
	newContent := patch.NewContent

	if v.config.EnableErrorHandling {
		rulesChecked = append(rulesChecked, "error-handling")
		violations = append(violations, v.checkErrorHandling(newContent, patch.FilePath)...)
	}

	if v.config.EnableNaming {
		rulesChecked = append(rulesChecked, "naming-conventions")
		violations = append(violations, v.checkNamingConventions(newContent, patch.FilePath)...)
	}

	if v.config.EnableTestStructure && isTestFile(patch.FilePath) {
		rulesChecked = append(rulesChecked, "test-structure")
		violations = append(violations, v.checkTestStructure(newContent, patch.FilePath)...)
	}

	// Filter violations by severity if not in strict mode
	if !v.config.StrictMode {
		violations = filterBySeverity(violations, SeverityError)
	}

	passed := len(violations) == 0
	score := v.computeComplianceScore(len(violations), len(rulesChecked))

	return ValidationResult{
		Passed:          passed,
		ComplianceScore: score,
		Violations:      violations,
		RulesChecked:    rulesChecked,
	}
}

// ValidatePatches validates multiple patches and returns an aggregate result.
func (v *Validator) ValidatePatches(patches []*Patch, kbRules []string) ValidationResult {
	if len(patches) == 0 {
		return ValidationResult{Passed: true, ComplianceScore: 1.0}
	}

	allViolations := make([]Violation, 0)
	rulesCheckedSet := make(map[string]bool)

	for _, patch := range patches {
		result := v.ValidatePatch(patch, kbRules)
		allViolations = append(allViolations, result.Violations...)
		for _, rule := range result.RulesChecked {
			rulesCheckedSet[rule] = true
		}
	}

	rulesChecked := make([]string, 0, len(rulesCheckedSet))
	for rule := range rulesCheckedSet {
		rulesChecked = append(rulesChecked, rule)
	}

	passed := len(allViolations) == 0
	score := v.computeComplianceScore(len(allViolations), len(rulesChecked))

	return ValidationResult{
		Passed:          passed,
		ComplianceScore: score,
		Violations:      allViolations,
		RulesChecked:    rulesChecked,
	}
}

// checkErrorHandling validates error handling patterns.
func (v *Validator) checkErrorHandling(content, filePath string) []Violation {
	violations := make([]Violation, 0)

	// Check for ignored errors (common anti-pattern)
	if strings.Contains(content, "_ = ") && strings.Contains(content, "err") {
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if strings.Contains(line, "_ = ") && strings.Contains(line, "err") {
				violations = append(violations, Violation{
					RuleName:    "error-handling-ignored",
					Severity:    SeverityWarning,
					Description: "Error return value appears to be ignored",
					FilePath:    filePath,
					LineHint:    fmt.Sprintf("line ~%d", i+1),
					Suggestion:  "Handle errors explicitly: if err != nil { return err } or document why the error is safe to ignore",
				})
			}
		}
	}

	// Check for missing error checks after function calls that likely return errors
	errorPronePatterns := []string{
		":= os.Open(",
		":= ioutil.ReadFile(",
		":= json.Unmarshal(",
		":= http.Get(",
		":= db.Query(",
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		for _, pattern := range errorPronePatterns {
			if strings.Contains(line, pattern) {
				// Check if next few lines have error handling
				hasErrorCheck := false
				for j := i + 1; j < len(lines) && j < i+3; j++ {
					if strings.Contains(lines[j], "if err") || strings.Contains(lines[j], "err !=") {
						hasErrorCheck = true
						break
					}
				}
				if !hasErrorCheck {
					violations = append(violations, Violation{
						RuleName:    "error-handling-missing",
						Severity:    SeverityError,
						Description: "Function call that returns an error appears to lack error handling",
						FilePath:    filePath,
						LineHint:    fmt.Sprintf("line ~%d", i+1),
						Suggestion:  "Add error handling: if err != nil { return err }",
					})
				}
			}
		}
	}

	return violations
}

// checkNamingConventions validates naming patterns.
func (v *Validator) checkNamingConventions(content, filePath string) []Violation {
	violations := make([]Violation, 0)

	// Check for snake_case in function names (should be camelCase or PascalCase)
	snakeCaseFuncPattern := regexp.MustCompile(`func\s+([a-z]+_[a-z_]+)\s*\(`)
	matches := snakeCaseFuncPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			violations = append(violations, Violation{
				RuleName:    "naming-snake-case-function",
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("Function name '%s' uses snake_case instead of camelCase", match[1]),
				FilePath:    filePath,
				LineHint:    match[1],
				Suggestion:  fmt.Sprintf("Rename to camelCase or PascalCase, e.g., '%s'", toCamelCase(match[1])),
			})
		}
	}

	// Check for overly short variable names in important contexts
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Look for single-letter variables that aren't loop counters or common abbreviations
		if strings.Contains(line, ":= ") {
			parts := strings.Split(line, ":=")
			if len(parts) > 0 {
				varPart := strings.TrimSpace(parts[0])
				if len(varPart) == 1 && !isCommonShortVar(varPart) && !strings.Contains(line, "for ") {
					violations = append(violations, Violation{
						RuleName:    "naming-short-variable",
						Severity:    SeverityInfo,
						Description: fmt.Sprintf("Variable '%s' is very short and may not be descriptive", varPart),
						FilePath:    filePath,
						LineHint:    fmt.Sprintf("line ~%d", i+1),
						Suggestion:  "Consider a more descriptive name unless this is a well-known convention",
					})
				}
			}
		}
	}

	return violations
}

// checkTestStructure validates test function structure.
func (v *Validator) checkTestStructure(content, filePath string) []Violation {
	violations := make([]Violation, 0)

	// Check for test functions without t.Helper() in helper functions
	helperPattern := regexp.MustCompile(`func\s+([a-zA-Z]+Helper[a-zA-Z]*)\s*\(`)
	helperMatches := helperPattern.FindAllStringSubmatch(content, -1)

	for _, match := range helperMatches {
		if len(match) > 1 {
			helperName := match[1]
			// Look for t.Helper() call within the function
			funcStart := strings.Index(content, "func "+helperName)
			if funcStart >= 0 {
				funcEnd := findFunctionEnd(content, funcStart)
				funcBody := content[funcStart:funcEnd]
				if !strings.Contains(funcBody, "t.Helper()") {
					violations = append(violations, Violation{
						RuleName:    "test-helper-missing-marker",
						Severity:    SeverityWarning,
						Description: fmt.Sprintf("Test helper function '%s' should call t.Helper()", helperName),
						FilePath:    filePath,
						LineHint:    helperName,
						Suggestion:  "Add t.Helper() as the first line of the helper function",
					})
				}
			}
		}
	}

	// Check for tests without assertion
	testPattern := regexp.MustCompile(`func\s+(Test[A-Z][a-zA-Z0-9]*)\s*\(`)
	testMatches := testPattern.FindAllStringSubmatch(content, -1)

	for _, match := range testMatches {
		if len(match) > 1 {
			testName := match[1]
			funcStart := strings.Index(content, "func "+testName)
			if funcStart >= 0 {
				funcEnd := findFunctionEnd(content, funcStart)
				funcBody := content[funcStart:funcEnd]
				// Look for common assertion patterns
				hasAssertion := strings.Contains(funcBody, "t.Fatal") ||
					strings.Contains(funcBody, "t.Error") ||
					strings.Contains(funcBody, "t.Fail") ||
					strings.Contains(funcBody, "t.Log") && (strings.Contains(funcBody, "t.Fail") || strings.Contains(funcBody, "t.Error")) ||
					strings.Contains(funcBody, "assert.") ||
					strings.Contains(funcBody, "require.") ||
					strings.Contains(funcBody, "if ") && (strings.Contains(funcBody, "t.Fatal") || strings.Contains(funcBody, "t.Error"))

				if !hasAssertion {
					violations = append(violations, Violation{
						RuleName:    "test-missing-assertion",
						Severity:    SeverityWarning,
						Description: fmt.Sprintf("Test function '%s' appears to lack assertions", testName),
						FilePath:    filePath,
						LineHint:    testName,
						Suggestion:  "Add assertions using t.Fatal, t.Error, or an assertion library",
					})
				}
			}
		}
	}

	return violations
}

// computeComplianceScore calculates a 0.0-1.0 score based on violations.
func (v *Validator) computeComplianceScore(violationCount, rulesChecked int) float64 {
	if rulesChecked == 0 {
		return 1.0
	}

	// Simple linear scoring: each violation reduces the score
	// More sophisticated scoring could weight by severity
	maxViolationsPerRule := 3.0 // Assume max 3 violations per rule before score hits 0
	maxPossibleViolations := float64(rulesChecked) * maxViolationsPerRule

	if violationCount == 0 {
		return 1.0
	}

	score := 1.0 - (float64(violationCount) / maxPossibleViolations)
	if score < 0.0 {
		score = 0.0
	}

	return score
}

// Helper functions

func isTestFile(filePath string) bool {
	return strings.HasSuffix(filePath, "_test.go")
}

func filterBySeverity(violations []Violation, minSeverity ViolationSeverity) []Violation {
	filtered := make([]Violation, 0)
	for _, v := range violations {
		if v.Severity == SeverityError || (minSeverity == SeverityWarning && v.Severity == SeverityWarning) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func isCommonShortVar(name string) bool {
	common := map[string]bool{
		"i": true, "j": true, "k": true, // loop counters
		"x": true, "y": true, "z": true, // coordinates
		"n": true, "m": true, // counts
		"s": true, // string (sometimes acceptable)
		"t": true, // testing.T
		"b": true, // testing.B
	}
	return common[name]
}

func toCamelCase(snakeCase string) string {
	parts := strings.Split(snakeCase, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func findFunctionEnd(content string, start int) int {
	// Simple brace matching to find function end
	braceCount := 0
	inFunc := false

	for i := start; i < len(content); i++ {
		if content[i] == '{' {
			inFunc = true
			braceCount++
		} else if content[i] == '}' {
			braceCount--
			if inFunc && braceCount == 0 {
				return i + 1
			}
		}
	}

	return len(content)
}

// BuildRevisionPrompt creates a prompt for fixing validation violations.
func BuildRevisionPrompt(violations []Violation, originalTask string) string {
	if len(violations) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("The generated code has validation issues that need to be addressed:\n\n")

	// Group by rule
	ruleViolations := make(map[string][]Violation)
	for _, v := range violations {
		ruleViolations[v.RuleName] = append(ruleViolations[v.RuleName], v)
	}

	for rule, viols := range ruleViolations {
		b.WriteString(fmt.Sprintf("**%s** (%d issue(s)):\n", rule, len(viols)))
		for _, v := range viols {
			b.WriteString(fmt.Sprintf("- %s", v.Description))
			if v.LineHint != "" {
				b.WriteString(fmt.Sprintf(" (near %s)", v.LineHint))
			}
			b.WriteString("\n")
			if v.Suggestion != "" {
				b.WriteString(fmt.Sprintf("  Suggestion: %s\n", v.Suggestion))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("Please revise the code to address these issues while maintaining the original intent: ")
	b.WriteString(originalTask)

	return b.String()
}
