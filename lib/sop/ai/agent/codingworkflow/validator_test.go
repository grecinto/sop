package codingworkflow

import (
	"strings"
	"testing"
)

func TestValidatePatch_ErrorHandling_IgnoredError(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main

func main() {
	file, err := os.Open("test.txt")
	_ = err  // Ignored error
	defer file.Close()
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	if len(result.Violations) == 0 {
		t.Fatal("expected violations for ignored error")
	}

	foundIgnored := false
	for _, v := range result.Violations {
		if v.RuleName == "error-handling-ignored" {
			foundIgnored = true
			if v.Severity != SeverityWarning {
				t.Errorf("expected warning severity, got %s", v.Severity)
			}
		}
	}

	if !foundIgnored {
		t.Error("expected to find error-handling-ignored violation")
	}
}

func TestValidatePatch_ErrorHandling_MissingCheck(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main

func main() {
	data, _ := ioutil.ReadFile("test.txt")
	fmt.Println(string(data))
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	foundMissing := false
	for _, v := range result.Violations {
		if v.RuleName == "error-handling-missing" {
			foundMissing = true
			if v.Severity != SeverityError {
				t.Errorf("expected error severity, got %s", v.Severity)
			}
		}
	}

	if !foundMissing {
		t.Error("expected to find error-handling-missing violation")
	}
}

func TestValidatePatch_ErrorHandling_ProperHandling(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main

func main() {
	data, err := ioutil.ReadFile("test.txt")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	// Should not flag this as a violation since error is handled
	for _, v := range result.Violations {
		if v.RuleName == "error-handling-missing" {
			t.Errorf("should not flag properly handled errors: %s", v.Description)
		}
	}
}

func TestValidatePatch_NamingConventions_SnakeCase(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main

func process_data(input string) string {
	return strings.ToUpper(input)
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	foundSnakeCase := false
	for _, v := range result.Violations {
		if v.RuleName == "naming-snake-case-function" {
			foundSnakeCase = true
			if !strings.Contains(v.Description, "process_data") {
				t.Errorf("expected violation to mention process_data, got: %s", v.Description)
			}
			if !strings.Contains(v.Suggestion, "processData") {
				t.Errorf("expected suggestion to include camelCase version, got: %s", v.Suggestion)
			}
		}
	}

	if !foundSnakeCase {
		t.Error("expected to find naming-snake-case-function violation")
	}
}

func TestValidatePatch_NamingConventions_ShortVariable(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main

func main() {
	d := fetchData()
	fmt.Println(d)
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	foundShort := false
	for _, v := range result.Violations {
		if v.RuleName == "naming-short-variable" {
			foundShort = true
			if v.Severity != SeverityInfo {
				t.Errorf("expected info severity for short variable, got %s", v.Severity)
			}
		}
	}

	if !foundShort {
		t.Error("expected to find naming-short-variable violation")
	}
}

func TestValidatePatch_NamingConventions_CommonShortVarsAllowed(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main

func main() {
	for i := 0; i < 10; i++ {
		fmt.Println(i)
	}
	
	s := "test"
	n := 42
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	// Common short vars like i, s, n should not be flagged
	for _, v := range result.Violations {
		if v.RuleName == "naming-short-variable" {
			t.Errorf("should not flag common short variables: %s", v.Description)
		}
	}
}

func TestValidatePatch_TestStructure_MissingHelper(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)

	patch := &Patch{
		FilePath: "example_test.go",
		NewContent: `package main

func setupHelper(t *testing.T) {
	// Setup code
}

func Test_Something(t *testing.T) {
	setupHelper(t)
	// test code
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	foundHelper := false
	for _, v := range result.Violations {
		if v.RuleName == "test-helper-missing-marker" {
			foundHelper = true
			if !strings.Contains(v.Description, "setupHelper") {
				t.Errorf("expected violation to mention setupHelper, got: %s", v.Description)
			}
		}
	}

	if !foundHelper {
		t.Error("expected to find test-helper-missing-marker violation")
	}
}

func TestValidatePatch_TestStructure_MissingAssertion(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true // Enable strict mode to catch warnings
	validator := NewValidator(config)

	patch := &Patch{
		FilePath: "example_test.go",
		NewContent: `package main

func TestSomething(t *testing.T) {
	result := doSomething()
	fmt.Println(result)
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	foundMissing := false
	for _, v := range result.Violations {
		t.Logf("Violation: %s - %s", v.RuleName, v.Description)
		if v.RuleName == "test-missing-assertion" {
			foundMissing = true
		}
	}

	if !foundMissing {
		t.Errorf("expected to find test-missing-assertion violation, found %d violations total", len(result.Violations))
	}
}

func TestValidatePatch_TestStructure_WithAssertion(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())

	patch := &Patch{
		FilePath: "example_test.go",
		NewContent: `package main

func TestSomething(t *testing.T) {
	result := doSomething()
	if result != expected {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	for _, v := range result.Violations {
		if v.RuleName == "test-missing-assertion" {
			t.Errorf("should not flag tests with assertions: %s", v.Description)
		}
	}
}

func TestValidatePatches_MultiplePatches(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())

	patches := []*Patch{
		{
			FilePath: "file1.go",
			NewContent: `package main
func process_data(x string) string { return x }`,
		},
		{
			FilePath: "file2.go",
			NewContent: `package main
func main() {
	data, err := ioutil.ReadFile("test.txt")
	fmt.Println(data)
}`,
		},
	}

	result := validator.ValidatePatches(patches, nil)

	if result.Passed {
		t.Error("expected validation to fail with multiple violations")
	}

	if len(result.Violations) < 1 {
		t.Errorf("expected at least 1 violation, got %d", len(result.Violations))
	}

	if result.ComplianceScore >= 1.0 {
		t.Errorf("expected compliance score < 1.0, got %f", result.ComplianceScore)
	}
}

func TestValidatePatches_EmptyPatches(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())

	result := validator.ValidatePatches([]*Patch{}, nil)

	if !result.Passed {
		t.Error("expected empty patches to pass validation")
	}

	if result.ComplianceScore != 1.0 {
		t.Errorf("expected compliance score 1.0 for empty patches, got %f", result.ComplianceScore)
	}
}

func TestValidatorConfig_StrictMode(t *testing.T) {
	config := DefaultValidatorConfig()
	config.StrictMode = true
	validator := NewValidator(config)

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main
func process_data(x string) string { return x }`,
	}

	result := validator.ValidatePatch(patch, nil)

	// In strict mode, warnings should also cause failure
	if result.Passed {
		t.Error("expected strict mode to fail on warnings")
	}
}

func TestValidatorConfig_DisabledRules(t *testing.T) {
	config := DefaultValidatorConfig()
	config.EnableNaming = false
	validator := NewValidator(config)

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main
func process_data(x string) string { return x }`,
	}

	result := validator.ValidatePatch(patch, nil)

	// With naming disabled, should not flag snake_case
	for _, v := range result.Violations {
		if strings.Contains(v.RuleName, "naming") {
			t.Errorf("should not check naming rules when disabled: %s", v.RuleName)
		}
	}
}

func TestComplianceScore_NoViolations(t *testing.T) {
	validator := NewValidator(DefaultValidatorConfig())

	patch := &Patch{
		FilePath: "example.go",
		NewContent: `package main

func processData(input string) (string, error) {
	data, err := transform(input)
	if err != nil {
		return "", err
	}
	return data, nil
}`,
	}

	result := validator.ValidatePatch(patch, nil)

	if result.ComplianceScore != 1.0 {
		t.Errorf("expected perfect compliance score, got %f", result.ComplianceScore)
	}
}

func TestBuildRevisionPrompt(t *testing.T) {
	violations := []Violation{
		{
			RuleName:    "error-handling-missing",
			Severity:    SeverityError,
			Description: "Missing error check after os.Open",
			LineHint:    "line 5",
			Suggestion:  "Add if err != nil { return err }",
		},
		{
			RuleName:    "naming-snake-case-function",
			Severity:    SeverityWarning,
			Description: "Function uses snake_case",
			Suggestion:  "Rename to camelCase",
		},
	}

	prompt := BuildRevisionPrompt(violations, "implement file reader")

	if !strings.Contains(prompt, "validation issues") {
		t.Error("prompt should mention validation issues")
	}

	if !strings.Contains(prompt, "error-handling-missing") {
		t.Error("prompt should include rule name")
	}

	if !strings.Contains(prompt, "Missing error check") {
		t.Error("prompt should include violation description")
	}

	if !strings.Contains(prompt, "implement file reader") {
		t.Error("prompt should include original task")
	}
}

func TestBuildRevisionPrompt_Empty(t *testing.T) {
	prompt := BuildRevisionPrompt([]Violation{}, "some task")

	if prompt != "" {
		t.Error("expected empty prompt for no violations")
	}
}
