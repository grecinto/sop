package codingworkflow

import (
	"strings"
	"testing"
)

func TestAddInlineCitations_BasicGo(t *testing.T) {
	config := DefaultCitationConfig()
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}`

	kbs := []string{"go-best-practices"}
	rules := []string{"error-handling", "naming-conventions"}

	result := AddInlineCitations(content, kbs, rules, config)

	if !strings.Contains(result, "// KB Patterns Applied:") {
		t.Error("expected citation header")
	}

	if !strings.Contains(result, "error-handling") {
		t.Error("expected error-handling rule in citation")
	}

	if !strings.Contains(result, "go-best-practices") {
		t.Error("expected KB name in citation")
	}

	// Citation should appear after package but before main content
	lines := strings.Split(result, "\n")
	citationIndex := -1
	packageIndex := -1
	mainIndex := -1

	for i, line := range lines {
		if strings.HasPrefix(line, "package ") {
			packageIndex = i
		}
		if strings.Contains(line, "KB Patterns") {
			citationIndex = i
		}
		if strings.Contains(line, "func main") {
			mainIndex = i
		}
	}

	if citationIndex < packageIndex {
		t.Error("citation should appear after package declaration")
	}
	if mainIndex >= 0 && citationIndex >= mainIndex {
		t.Error("citation should appear before main function")
	}
}

func TestAddInlineCitations_Disabled(t *testing.T) {
	config := DefaultCitationConfig()
	config.Enabled = false

	content := "package main\nfunc main() {}"
	result := AddInlineCitations(content, []string{"kb"}, []string{"rule"}, config)

	if result != content {
		t.Error("expected unchanged content when citations disabled")
	}
}

func TestAddInlineCitations_NoRules(t *testing.T) {
	config := DefaultCitationConfig()
	content := "package main\nfunc main() {}"
	result := AddInlineCitations(content, []string{"kb"}, []string{}, config)

	if result != content {
		t.Error("expected unchanged content when no rules provided")
	}
}

func TestAddInlineCitations_FullFormat(t *testing.T) {
	config := DefaultCitationConfig()
	config.Format = "full"

	content := `package main

func main() {}`

	kbs := []string{"go-kb"}
	rules := []string{"error-handling", "test-structure"}

	result := AddInlineCitations(content, kbs, rules, config)

	if !strings.Contains(result, "This code follows these KB patterns:") {
		t.Error("expected full format header")
	}

	if !strings.Contains(result, "- error-handling") {
		t.Error("expected bullet point for rule")
	}
}

func TestAddFunctionCitation(t *testing.T) {
	config := DefaultCitationConfig()

	content := `package main

func processData(input string) string {
	return strings.ToUpper(input)
}`

	result := AddFunctionCitation(content, "processData", "naming-conventions", "go-kb", config)

	if !strings.Contains(result, "// Pattern: naming-conventions") {
		t.Error("expected pattern citation")
	}

	if !strings.Contains(result, "(from go-kb)") {
		t.Error("expected KB name in citation")
	}

	// Citation should appear before function
	lines := strings.Split(result, "\n")
	citationIndex := -1
	funcIndex := -1

	for i, line := range lines {
		if strings.Contains(line, "Pattern:") {
			citationIndex = i
		}
		if strings.Contains(line, "func processData") {
			funcIndex = i
		}
	}

	if citationIndex >= funcIndex {
		t.Error("citation should appear before function")
	}
}

func TestAddFunctionCitation_FunctionNotFound(t *testing.T) {
	config := DefaultCitationConfig()
	content := "package main\nfunc other() {}"

	result := AddFunctionCitation(content, "nonexistent", "pattern", "kb", config)

	if result != content {
		t.Error("expected unchanged content when function not found")
	}
}

func TestExtractRulesFromExpertise(t *testing.T) {
	expertise := ExpertiseCapsule{
		Summary:  "Apply error handling and naming conventions best practices",
		Category: "go-best-practices",
		Sources:  []string{"KB:sop-kb"},
	}

	rules := ExtractRulesFromExpertise(expertise)

	if len(rules) == 0 {
		t.Fatal("expected to extract rules from expertise")
	}

	foundErrorHandling := false
	foundNaming := false

	for _, rule := range rules {
		if rule == "error-handling" {
			foundErrorHandling = true
		}
		if rule == "naming-conventions" {
			foundNaming = true
		}
	}

	if !foundErrorHandling {
		t.Error("expected to extract error-handling rule")
	}
	if !foundNaming {
		t.Error("expected to extract naming-conventions rule")
	}
}

func TestExtractRulesFromExpertise_UsesCategory(t *testing.T) {
	expertise := ExpertiseCapsule{
		Summary:  "General guidance",
		Category: "testing-patterns",
		Sources:  []string{},
	}

	rules := ExtractRulesFromExpertise(expertise)

	if len(rules) == 0 {
		t.Fatal("expected to extract category as rule when no specific rules found")
	}

	if rules[0] != "testing-patterns" {
		t.Errorf("expected category as rule, got %s", rules[0])
	}
}

func TestExtractKBNamesFromExpertise(t *testing.T) {
	expertise := ExpertiseCapsule{
		Summary:  "Some guidance",
		Category: "patterns",
		Sources:  []string{"KB:sop-kb", "Space:my-space", "KB:go-kb"},
	}

	kbs := ExtractKBNamesFromExpertise(expertise)

	if len(kbs) != 3 {
		t.Fatalf("expected 3 KB names, got %d", len(kbs))
	}

	expected := map[string]bool{"sop-kb": true, "my-space": true, "go-kb": true}
	for _, kb := range kbs {
		if !expected[kb] {
			t.Errorf("unexpected KB name: %s", kb)
		}
	}
}

func TestExtractKBNamesFromExpertise_Empty(t *testing.T) {
	expertise := ExpertiseCapsule{
		Summary:  "Some guidance",
		Category: "patterns",
		Sources:  []string{},
	}

	kbs := ExtractKBNamesFromExpertise(expertise)

	if len(kbs) != 0 {
		t.Errorf("expected no KB names, got %d", len(kbs))
	}
}

func TestFindCitationInsertionPoint_AfterPackage(t *testing.T) {
	lines := []string{
		"package main",
		"",
		"func main() {}",
	}

	point := findCitationInsertionPoint(lines)

	if point != 0 {
		t.Errorf("expected insertion after package (line 0), got %d", point)
	}
}

func TestFindCitationInsertionPoint_AfterImports(t *testing.T) {
	lines := []string{
		"package main",
		"",
		"import (",
		"    \"fmt\"",
		")",
		"",
		"func main() {}",
	}

	point := findCitationInsertionPoint(lines)

	if point != 4 {
		t.Errorf("expected insertion after imports (line 4), got %d", point)
	}
}

func TestFindCitationInsertionPoint_AfterSingleImport(t *testing.T) {
	lines := []string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func main() {}",
	}

	point := findCitationInsertionPoint(lines)

	if point != 2 {
		t.Errorf("expected insertion after single import (line 2), got %d", point)
	}
}
