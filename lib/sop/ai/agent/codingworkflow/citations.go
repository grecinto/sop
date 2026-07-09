package codingworkflow

import (
	"fmt"
	"strings"
)

// CitationConfig controls how inline citations are added to generated code.
type CitationConfig struct {
	Enabled       bool
	CommentPrefix string // e.g., "//" for Go, "#" for Python
	Format        string // "full" or "compact"
}

// DefaultCitationConfig returns a sensible default configuration.
func DefaultCitationConfig() CitationConfig {
	return CitationConfig{
		Enabled:       true,
		CommentPrefix: "//",
		Format:        "compact",
	}
}

// AddInlineCitations enriches generated code with KB pattern attribution.
func AddInlineCitations(content string, kbNames []string, rules []string, config CitationConfig) string {
	if !config.Enabled || len(rules) == 0 {
		return content
	}

	// Build citation header
	citation := buildCitationHeader(kbNames, rules, config)
	if citation == "" {
		return content
	}

	// Insert at the top of the file after package declaration
	lines := strings.Split(content, "\n")
	insertionPoint := findCitationInsertionPoint(lines)

	if insertionPoint < 0 {
		// If no good insertion point, prepend to content
		return citation + "\n" + content
	}

	// Insert citation after insertion point
	result := make([]string, 0, len(lines)+len(strings.Split(citation, "\n")))
	result = append(result, lines[:insertionPoint+1]...)
	result = append(result, "")
	result = append(result, strings.Split(citation, "\n")...)
	result = append(result, lines[insertionPoint+1:]...)

	return strings.Join(result, "\n")
}

// buildCitationHeader creates the citation comment block.
func buildCitationHeader(kbNames []string, rules []string, config CitationConfig) string {
	if len(rules) == 0 {
		return ""
	}

	prefix := config.CommentPrefix + " "

	if config.Format == "compact" {
		// Compact format: just list the patterns
		if len(kbNames) == 1 {
			return fmt.Sprintf("%sKB Patterns Applied: %s (from %s)",
				prefix, strings.Join(rules, ", "), kbNames[0])
		}
		return fmt.Sprintf("%sKB Patterns Applied: %s", prefix, strings.Join(rules, ", "))
	}

	// Full format: detailed attribution
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%sThis code follows these KB patterns:\n", prefix))

	rulesByKB := make(map[string][]string)
	// If we have multiple KBs, try to associate rules with them
	// For now, just group all rules under first KB or "various"
	kb := "KB"
	if len(kbNames) > 0 {
		kb = kbNames[0]
	}
	rulesByKB[kb] = rules

	for kbName, kbRules := range rulesByKB {
		for _, rule := range kbRules {
			b.WriteString(fmt.Sprintf("%s  - %s (%s)\n", prefix, rule, kbName))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// findCitationInsertionPoint finds where to insert the citation in the code.
// Returns the line index after which to insert, or -1 if no good spot found.
func findCitationInsertionPoint(lines []string) int {
	// Strategy: insert after package declaration and imports
	packageLine := -1
	lastImportLine := -1
	inImportBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Find package declaration
		if strings.HasPrefix(trimmed, "package ") {
			packageLine = i
			continue
		}

		// Track import block
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = true
			continue
		}
		if inImportBlock && trimmed == ")" {
			lastImportLine = i
			inImportBlock = false
			continue
		}
		if strings.HasPrefix(trimmed, "import \"") {
			lastImportLine = i
			continue
		}
	}

	// Return insertion point: after imports if present, otherwise after package
	if lastImportLine > packageLine {
		return lastImportLine
	}
	if packageLine >= 0 {
		return packageLine
	}

	return -1
}

// AddFunctionCitation adds a citation comment above a specific function.
func AddFunctionCitation(content string, functionName string, pattern string, kbName string, config CitationConfig) string {
	if !config.Enabled || functionName == "" || pattern == "" {
		return content
	}

	citation := fmt.Sprintf("%s Pattern: %s", config.CommentPrefix, pattern)
	if kbName != "" {
		citation += fmt.Sprintf(" (from %s)", kbName)
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, "func "+functionName) || strings.Contains(line, "func (") && strings.Contains(line, functionName) {
			// Insert citation comment above function
			result := make([]string, 0, len(lines)+1)
			result = append(result, lines[:i]...)
			result = append(result, citation)
			result = append(result, lines[i:]...)
			return strings.Join(result, "\n")
		}
	}

	return content
}

// ExtractRulesFromExpertise extracts rule names from an expertise capsule.
func ExtractRulesFromExpertise(expertise ExpertiseCapsule) []string {
	rules := make([]string, 0)

	// Extract from summary
	if strings.Contains(expertise.Summary, "error handling") || strings.Contains(expertise.Summary, "error-handling") {
		rules = append(rules, "error-handling")
	}
	if strings.Contains(expertise.Summary, "naming") || strings.Contains(expertise.Summary, "conventions") {
		rules = append(rules, "naming-conventions")
	}
	if strings.Contains(expertise.Summary, "test") {
		rules = append(rules, "test-structure")
	}

	// Use category as a rule if no specific rules extracted
	if len(rules) == 0 && expertise.Category != "" {
		rules = append(rules, expertise.Category)
	}

	return rules
}

// ExtractKBNamesFromExpertise extracts KB names from sources.
func ExtractKBNamesFromExpertise(expertise ExpertiseCapsule) []string {
	kbs := make([]string, 0)
	seen := make(map[string]bool)

	for _, source := range expertise.Sources {
		// Parse source strings like "KB:sop-kb" or "Space:my-space"
		parts := strings.Split(source, ":")
		if len(parts) >= 2 {
			kbName := strings.TrimSpace(parts[1])
			if kbName != "" && !seen[kbName] {
				kbs = append(kbs, kbName)
				seen[kbName] = true
			}
		}
	}

	return kbs
}
