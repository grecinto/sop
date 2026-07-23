package main

import (
	"regexp"
	"strings"
)

var prefixRegex = regexp.MustCompile(`^(\d+(\.\d+)*[\.\)]?|[A-Za-z][\.\)]|\b[IVXLCDMivxlcdm]+[\.\)])(\s+|$)`)
var headingPrefixRegex = regexp.MustCompile(`(?m)^\s*#+\s*(?:\d+(?:\.\d+)*[\.\)]?|[A-Za-z][\.\)]|\b[IVXLCDMivxlcdm]+[\.\)])?\s*`)

func removePrefix(s string, prefix string) string {
	if prefix == "" {
		return s
	}
	if strings.HasPrefix(s, prefix) {
		s, _ = strings.CutPrefix(s, prefix)
	}
	return s
}

func cleanText(s string) string {
	s = headingPrefixRegex.ReplaceAllString(s, "")
	s = prefixRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func normalizeCategoryName(s string) string {
	s = strings.TrimLeft(s, "# ")
	s = prefixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	// Remove [workflow] marker if present
	s = strings.TrimSuffix(s, "[workflow]")
	s = strings.TrimSuffix(s, "[Workflow]")
	s = strings.TrimSpace(s)
	// Remove spaces around slashes in category names (e.g., "CLI / Examples" -> "CLI/Examples")
	s = strings.ReplaceAll(s, " / ", "/")

	// Keep category names stable; language-specific canonicalization happens in the runtime memory pipeline.
	return s
}

// hasWorkflowMarker checks if the original category name has [workflow] marker
func hasWorkflowMarker(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasSuffix(s, "[workflow]") || strings.HasSuffix(s, "[Workflow]")
}

// isWorkflowCategoryPath checks if category path has explicit workflow marker
func isWorkflowCategoryPath(pathContext []string) bool {
	// Only check for explicit [workflow] marker, not name-based detection
	// Categories are marked as workflow-enabled when they contain workflow items
	for _, part := range pathContext {
		if hasWorkflowMarker(part) {
			return true
		}
	}
	return false
}
