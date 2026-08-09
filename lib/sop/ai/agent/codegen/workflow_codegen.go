package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"strings"
	"text/template"
)

// TargetLanguage is the output language for code generation.
type TargetLanguage string

const (
	TargetLanguageGo         TargetLanguage = "go"
	TargetLanguageCSharp     TargetLanguage = "csharp"
	TargetLanguagePython     TargetLanguage = "python"
	TargetLanguageJava       TargetLanguage = "java"
	TargetLanguageRust       TargetLanguage = "rust"
	TargetLanguageTypeScript TargetLanguage = "typescript"
	TargetLanguageJavaScript TargetLanguage = "javascript"
	TargetLanguageReact      TargetLanguage = "react"
	TargetLanguageAngular    TargetLanguage = "angular"
	TargetLanguageHTMLCSS    TargetLanguage = "htmlcss"
)

// WorkflowDefinitionCodegen represents a workflow for code generation
type WorkflowDefinitionCodegen struct {
	WorkflowID      string                `json:"workflow_id"`
	Name            string                `json:"name"`
	Description     string                `json:"description"`
	Version         string                `json:"version"`
	CategoryPath    string                `json:"category_path"`
	TriggerPatterns []string              `json:"trigger_patterns"`
	Steps           []WorkflowStepCodegen `json:"steps"`
	ActorLimits     map[string]int        `json:"actor_limits"`
}

// WorkflowStepCodegen represents a workflow step for code generation
type WorkflowStepCodegen struct {
	Step                int                         `json:"step"`
	Actor               string                      `json:"actor"`
	Priority            int                         `json:"priority"`
	QueryTemplate       string                      `json:"query_template"`
	ToolsExpected       []string                    `json:"tools_expected"`
	OptimizationProfile *OptimizationProfileCodegen `json:"optimization_profile,omitempty"`
}

// OptimizationProfileCodegen represents optimization settings for codegen
type OptimizationProfileCodegen struct {
	PreferScript  bool     `json:"prefer_script"`
	ScriptLibrary []string `json:"script_library"`
	FallbackToLLM bool     `json:"fallback_to_llm"`
	MaxScriptTime int      `json:"max_script_time_ms"`
}

// CompileWorkflowToBinary generates a standalone Go binary from a workflow definition.
// Returns the path to the generated executable.
func CompileWorkflowToBinary(workflowPath, outputBinary string) (string, error) {
	// Generate Go source
	goCode, err := CompileWorkflowToGo(workflowPath)
	if err != nil {
		return "", fmt.Errorf("generate Go code: %w", err)
	}

	// Write to temporary file
	tmpDir := os.TempDir()
	tmpFile := tmpDir + "/workflow_main.go"
	if err := os.WriteFile(tmpFile, []byte(goCode), 0644); err != nil {
		return "", fmt.Errorf("write Go source: %w", err)
	}

	// Note: Actual compilation would require calling `go build`
	// For now, we return the source path
	return tmpFile, nil
}

// CompileWorkflow generates source code from a workflow definition in the requested target language.
func CompileWorkflow(workflowPath string, target TargetLanguage) (string, error) {
	workflow, err := loadWorkflowDefinition(workflowPath)
	if err != nil {
		return "", err
	}

	switch normalizeTargetLanguage(target) {
	case TargetLanguageGo:
		code, err := generateWorkflowGoCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate Go code: %w", err)
		}

		formatted, err := format.Source([]byte(code))
		if err != nil {
			return "", fmt.Errorf("format Go code: %w", err)
		}

		return string(formatted), nil
	case TargetLanguageCSharp:
		code, err := generateWorkflowCSharpCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate C# code: %w", err)
		}
		return code, nil
	case TargetLanguagePython:
		code, err := generateWorkflowPythonCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate Python code: %w", err)
		}
		return code, nil
	case TargetLanguageJava:
		code, err := generateWorkflowJavaCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate Java code: %w", err)
		}
		return code, nil
	case TargetLanguageRust:
		code, err := generateWorkflowRustCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate Rust code: %w", err)
		}
		return code, nil
	case TargetLanguageTypeScript, TargetLanguageJavaScript:
		code, err := generateWorkflowTypeScriptCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate TypeScript code: %w", err)
		}
		return code, nil
	case TargetLanguageReact:
		code, err := generateWorkflowReactCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate React code: %w", err)
		}
		return code, nil
	case TargetLanguageAngular:
		code, err := generateWorkflowAngularCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate Angular code: %w", err)
		}
		return code, nil
	case TargetLanguageHTMLCSS:
		code, err := generateWorkflowHTMLCSSCode(&workflow)
		if err != nil {
			return "", fmt.Errorf("generate HTML/CSS code: %w", err)
		}
		return code, nil
	default:
		return "", fmt.Errorf("unsupported workflow codegen target: %q", target)
	}
}

// CompileWorkflowToGo generates Go source code from a workflow definition.
func CompileWorkflowToGo(workflowPath string) (string, error) {
	return CompileWorkflow(workflowPath, TargetLanguageGo)
}

// CompileWorkflowToCSharp generates C# source code from a workflow definition.
func CompileWorkflowToCSharp(workflowPath string) (string, error) {
	return CompileWorkflow(workflowPath, TargetLanguageCSharp)
}

// CompileWorkflowToPython generates Python source code from a workflow definition.
func CompileWorkflowToPython(workflowPath string) (string, error) {
	return CompileWorkflow(workflowPath, TargetLanguagePython)
}

// CompileWorkflowToJava generates Java source code from a workflow definition.
func CompileWorkflowToJava(workflowPath string) (string, error) {
	return CompileWorkflow(workflowPath, TargetLanguageJava)
}

// CompileWorkflowToRust generates Rust source code from a workflow definition.
func CompileWorkflowToRust(workflowPath string) (string, error) {
	return CompileWorkflow(workflowPath, TargetLanguageRust)
}

// CompileWorkflowToTypeScript generates TypeScript source code from a workflow definition.
func CompileWorkflowToTypeScript(workflowPath string) (string, error) {
	return CompileWorkflow(workflowPath, TargetLanguageTypeScript)
}

func loadWorkflowDefinition(workflowPath string) (WorkflowDefinitionCodegen, error) {
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		return WorkflowDefinitionCodegen{}, fmt.Errorf("read workflow file: %w", err)
	}

	var workflow WorkflowDefinitionCodegen
	if err := json.Unmarshal(data, &workflow); err != nil {
		return WorkflowDefinitionCodegen{}, fmt.Errorf("parse workflow JSON: %w", err)
	}

	return workflow, nil
}

func normalizeTargetLanguage(target TargetLanguage) TargetLanguage {
	normalized := strings.ToLower(strings.TrimSpace(string(target)))
	if normalized == "" {
		return TargetLanguageGo
	}

	switch normalized {
	case "go", "golang":
		return TargetLanguageGo
	case "csharp", "c#", "cs":
		return TargetLanguageCSharp
	case "python", "py":
		return TargetLanguagePython
	case "java":
		return TargetLanguageJava
	case "rust":
		return TargetLanguageRust
	case "typescript", "ts":
		return TargetLanguageTypeScript
	case "javascript", "js":
		return TargetLanguageJavaScript
	case "react", "jsx", "tsx":
		return TargetLanguageReact
	case "angular", "ng", "angularjs":
		return TargetLanguageAngular
	case "html", "htmlcss", "css", "html-css", "web":
		return TargetLanguageHTMLCSS
	default:
		return TargetLanguage(normalized)
	}
}

func toPascalCase(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.' || r == '/'
	})

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(part) == 1 {
			result = append(result, strings.ToUpper(part))
			continue
		}
		result = append(result, strings.ToUpper(part[:1])+strings.ToLower(part[1:]))
	}

	return strings.Join(result, "")
}

func toCamelCase(name string) string {
	pascal := toPascalCase(name)
	if pascal == "" {
		return ""
	}
	return strings.ToLower(pascal[:1]) + pascal[1:]
}

func toRustIdentifier(name string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name))
}

// generateWorkflowGoCode creates Go source from WorkflowDefinition
func generateWorkflowGoCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow").Funcs(template.FuncMap{
		"sanitizeName": sanitizeName,
		"quote":        func(s string) string { return fmt.Sprintf("%q", s) },
		"join":         func(arr []string, sep string) string { return strings.Join(arr, sep) },
	}).Parse(workflowTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

const workflowTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
//
// Workflow: {{ .Name }}
// Version: {{ .Version }}
// Description: {{ .Description }}

package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// WorkflowStep represents a single step in the workflow
type WorkflowStep struct {
	Step          int
	Actor         string
	Priority      int
	QueryTemplate string
	ToolsExpected []string
}

// WorkflowResult represents the result of workflow execution
type WorkflowResult struct {
	Success bool
	Message string
	Steps   []StepResult
}

// StepResult represents the result of a single step
type StepResult struct {
	Step    int
	Actor   string
	Success bool
	Output  string
	Error   error
}

func main() {
	ctx := context.Background()
	
	// Parse command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <query>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s \"fix failing test\"\n", os.Args[0])
		os.Exit(1)
	}
	
	query := os.Args[1]
	
	// Execute workflow
	result, err := executeWorkflow(ctx, query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Workflow failed: %v\n", err)
		os.Exit(1)
	}
	
	// Print results
	fmt.Println("Workflow completed successfully!")
	fmt.Printf("Total steps: %d\n", len(result.Steps))
	for _, step := range result.Steps {
		status := "✓"
		if !step.Success {
			status = "✗"
		}
		fmt.Printf("%s Step %d (%s): %s\n", status, step.Step, step.Actor, step.Output)
	}
}

// executeWorkflow runs the compiled workflow
func executeWorkflow(ctx context.Context, originalQuery string) (*WorkflowResult, error) {
	result := &WorkflowResult{
		Success: true,
		Steps:   make([]StepResult, 0),
	}
	
	// Workflow: {{ .WorkflowID }}
	steps := []WorkflowStep{
		{{ range .Steps }}{
			Step:          {{ .Step }},
			Actor:         {{ quote .Actor }},
			Priority:      {{ .Priority }},
			QueryTemplate: {{ quote .QueryTemplate }},
			ToolsExpected: []string{ {{ range .ToolsExpected }}{{ quote . }}, {{ end }} },
		},
		{{ end }}
	}
	
	// Execute each step
	for _, step := range steps {
		query := expandTemplate(step.QueryTemplate, originalQuery)
		
		stepResult := StepResult{
			Step:    step.Step,
			Actor:   step.Actor,
			Success: true,
		}
		
		// Execute step
		output, err := executeStep(ctx, step.Step, step.Actor, query, step.ToolsExpected)
		if err != nil {
			stepResult.Success = false
			stepResult.Error = err
			stepResult.Output = fmt.Sprintf("Failed: %v", err)
			result.Success = false
		} else {
			stepResult.Output = output
		}
		
		result.Steps = append(result.Steps, stepResult)
		
		// Stop on failure
		if !stepResult.Success {
			break
		}
	}
	
	return result, nil
}

// executeStep executes a single workflow step
func executeStep(ctx context.Context, stepNum int, actor, query string, toolsExpected []string) (string, error) {
	// In a real implementation, this would:
	// 1. Call appropriate tools based on toolsExpected
	// 2. Use LLM for complex reasoning
	// 3. Return structured results
	
	// For now, return a placeholder
	time.Sleep(10 * time.Millisecond) // Simulate work
	return fmt.Sprintf("Step %d executed with actor %s", stepNum, actor), nil
}

// expandTemplate replaces {original_query} with the actual query
func expandTemplate(template, originalQuery string) string {
	// Simple template expansion
	result := template
	result = replaceAll(result, "{original_query}", originalQuery)
	return result
}

// replaceAll is a helper to avoid importing strings
func replaceAll(s, old, new string) string {
	result := ""
	for {
		idx := indexOf(s, old)
		if idx == -1 {
			result += s
			break
		}
		result += s[:idx] + new
		s = s[idx+len(old):]
	}
	return result
}

// indexOf finds the first occurrence of substring
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
`

// generateWorkflowCSharpCode creates C# source from WorkflowDefinition.
func generateWorkflowCSharpCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-csharp").Funcs(template.FuncMap{
		"sanitizeName": sanitizeName,
	}).Parse(workflowCSharpTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute C# template: %w", err)
	}

	return buf.String(), nil
}

// generateWorkflowPythonCode creates Python source from WorkflowDefinition.
func generateWorkflowPythonCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-python").Funcs(template.FuncMap{
		"sanitizeName": sanitizeName,
	}).Parse(workflowPythonTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute Python template: %w", err)
	}

	return buf.String(), nil
}

// generateWorkflowJavaCode creates Java source from WorkflowDefinition.
func generateWorkflowJavaCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-java").Funcs(template.FuncMap{
		"pascalCase": toPascalCase,
	}).Parse(workflowJavaTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute Java template: %w", err)
	}

	return buf.String(), nil
}

// generateWorkflowRustCode creates Rust source from WorkflowDefinition.
func generateWorkflowRustCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-rust").Funcs(template.FuncMap{
		"rustIdentifier": toRustIdentifier,
	}).Parse(workflowRustTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute Rust template: %w", err)
	}

	return buf.String(), nil
}

// generateWorkflowTypeScriptCode creates TypeScript source from WorkflowDefinition.
func generateWorkflowTypeScriptCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-typescript").Funcs(template.FuncMap{
		"camelCase": toCamelCase,
	}).Parse(workflowTypeScriptTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute TypeScript template: %w", err)
	}

	return buf.String(), nil
}

// generateWorkflowHTMLCSSCode creates a simple HTML/CSS starter page from WorkflowDefinition.
func generateWorkflowHTMLCSSCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-htmlcss").Parse(workflowHTMLCSSTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute HTML/CSS template: %w", err)
	}

	return buf.String(), nil
}

// generateWorkflowReactCode creates a simple React component from WorkflowDefinition.
func generateWorkflowReactCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-react").Funcs(template.FuncMap{
		"sanitizeName": sanitizeName,
	}).Parse(workflowReactTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute React template: %w", err)
	}

	return buf.String(), nil
}

// generateWorkflowAngularCode creates a simple Angular component from WorkflowDefinition.
func generateWorkflowAngularCode(workflow *WorkflowDefinitionCodegen) (string, error) {
	tmpl := template.Must(template.New("workflow-angular").Funcs(template.FuncMap{
		"sanitizeName": sanitizeName,
	}).Parse(workflowAngularTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, workflow); err != nil {
		return "", fmt.Errorf("execute Angular template: %w", err)
	}

	return buf.String(), nil
}

const workflowCSharpTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
// Workflow: {{ .Name }}
// Version: {{ .Version }}
// Description: {{ .Description }}

using System;
using System.Collections.Generic;

public sealed class WorkflowStep {
	public int Step { get; set; }
	public string Actor { get; set; } = string.Empty;
	public int Priority { get; set; }
	public string QueryTemplate { get; set; } = string.Empty;
	public List<string> ToolsExpected { get; set; } = new();
}

public static class {{ sanitizeName .WorkflowID }}Workflow {
	public static List<WorkflowStep> BuildSteps() {
		return new List<WorkflowStep> {
{{- range .Steps }}
			new WorkflowStep {
				Step = {{ .Step }},
				Actor = "{{ .Actor }}",
				Priority = {{ .Priority }},
				QueryTemplate = "{{ .QueryTemplate }}",
				ToolsExpected = new List<string> { {{- range $i, $tool := .ToolsExpected }}{{ if $i }}, {{ end }}"{{ $tool }}"{{- end }} }
			},
{{- end }}
		};
	}

	public static string ExpandTemplate(string template, string originalQuery) {
		return template.Replace("{original_query}", originalQuery);
	}

	public static string ExecuteStep(int step, string actor, string query, List<string> toolsExpected) {
		return $"Step {step} executed with actor {actor}";
	}
}
`

const workflowPythonTemplate = `# Code generated by sop-compile-workflow. DO NOT EDIT.
# Workflow: {{ .Name }}
# Version: {{ .Version }}
# Description: {{ .Description }}

from dataclasses import dataclass
from typing import List


@dataclass
class WorkflowStep:
	step: int
	actor: str
	priority: int
	query_template: str
	tools_expected: List[str]


def build_steps() -> List[WorkflowStep]:
	return [
{{- range .Steps }}
		WorkflowStep(
			step={{ .Step }},
			actor="{{ .Actor }}",
			priority={{ .Priority }},
			query_template="{{ .QueryTemplate }}",
			tools_expected=[{{- range $i, $tool := .ToolsExpected }}{{ if $i }}, {{ end }}"{{ $tool }}"{{- end }}],
		),
{{- end }}
	]


def expand_template(template: str, original_query: str) -> str:
	return template.replace("{original_query}", original_query)


def execute_step(step: int, actor: str, query: str, tools_expected: List[str]) -> str:
	return f"Step {step} executed with actor {actor}"
`

const workflowJavaTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
// Workflow: {{ .Name }}
// Version: {{ .Version }}
// Description: {{ .Description }}

import java.util.ArrayList;
import java.util.List;

public class WorkflowApp {
	public static class WorkflowStep {
		public int step;
		public String actor;
		public int priority;
		public String queryTemplate;
		public List<String> toolsExpected;
	}

	public static List<WorkflowStep> buildSteps() {
		List<WorkflowStep> steps = new ArrayList<>();
{{- range .Steps }}
		WorkflowStep step = new WorkflowStep();
		step.step = {{ .Step }};
		step.actor = "{{ .Actor }}";
		step.priority = {{ .Priority }};
		step.queryTemplate = "{{ .QueryTemplate }}";
		step.toolsExpected = new ArrayList<>();
{{- range .ToolsExpected }}
		step.toolsExpected.add("{{ . }}");
{{- end }}
		steps.add(step);
{{- end }}
		return steps;
	}

	public static String expandTemplate(String template, String originalQuery) {
		return template.replace("{original_query}", originalQuery);
	}

	public static String executeStep(int step, String actor, String query, List<String> toolsExpected) {
		return "Step " + step + " executed with actor " + actor;
	}
}
`

const workflowRustTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
// Workflow: {{ .Name }}
// Version: {{ .Version }}
// Description: {{ .Description }}

#[derive(Debug, Clone)]
struct WorkflowStep {
	step: usize,
	actor: String,
	priority: usize,
	query_template: String,
	tools_expected: Vec<String>,
}

fn build_steps() -> Vec<WorkflowStep> {
	let mut steps = Vec::new();
{{- range .Steps }}
	steps.push(WorkflowStep {
		step: {{ .Step }},
		actor: "{{ .Actor }}".to_string(),
		priority: {{ .Priority }},
		query_template: "{{ .QueryTemplate }}".to_string(),
		tools_expected: vec![{{- range $i, $tool := .ToolsExpected }}{{ if $i }}, {{ end }}"{{ $tool }}".to_string(){{- end }}],
	});
{{- end }}
	steps
}

fn expand_template(template: &str, original_query: &str) -> String {
	template.replace("{original_query}", original_query)
}

fn execute_step(step: usize, actor: &str, query: &str, tools_expected: &[String]) -> String {
	format!("Step {} executed with actor {}", step, actor)
}

fn main() {
	let _steps = build_steps();
	println!("Workflow ready");
}
`

const workflowTypeScriptTemplate = "// Code generated by sop-compile-workflow. DO NOT EDIT.\n// Workflow: {{ .Name }}\n// Version: {{ .Version }}\n// Description: {{ .Description }}\n\nexport interface WorkflowStep {\n\tstep: number;\n\tactor: string;\n\tpriority: number;\n\tqueryTemplate: string;\n\ttoolsExpected: string[];\n}\n\nexport function buildSteps(): WorkflowStep[] {\n\treturn [\n{{- range .Steps }}\n\t\t{\n\t\t\tstep: {{ .Step }},\n\t\t\tactor: \"{{ .Actor }}\",\n\t\t\tpriority: {{ .Priority }},\n\t\t\tqueryTemplate: \"{{ .QueryTemplate }}\",\n\t\t\ttoolsExpected: [{{- range $i, $tool := .ToolsExpected }}{{ if $i }}, {{ end }}\"{{ $tool }}\"{{- end }}],\n\t\t},\n{{- end }}\n\t];\n}\n\nexport function expandTemplate(template: string, originalQuery: string): string {\n\treturn template.replace(\"{original_query}\", originalQuery);\n}\n\nexport async function executeWorkflow(originalQuery: string): Promise<string> {\n\treturn `Workflow ready for ${originalQuery}`;\n}\n\nexport async function executeStep(step: number, actor: string, query: string, toolsExpected: string[]): Promise<string> {\n\treturn `Step ${step} executed with actor ${actor}`;\n}\n"

const workflowHTMLCSSTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{ .Name }}</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    .card { border: 1px solid #ddd; border-radius: 8px; padding: 1rem; }
  </style>
</head>
<body>
  <div class="card">
    <h1>{{ .Name }}</h1>
    <p>{{ .Description }}</p>
    <ul>
{{- range .Steps }}
      <li>{{ .Actor }} - {{ .QueryTemplate }}</li>
{{- end }}
    </ul>
  </div>
</body>
</html>
`

const workflowReactTemplate = `import React from "react";

export function {{ sanitizeName .Name }}() {
  return (
    <section>
      <h1>{{ .Name }}</h1>
      <p>{{ .Description }}</p>
      <ul>
{{- range .Steps }}
        <li>{"{{ .Actor }} - {{ .QueryTemplate }}"}</li>
{{- end }}
      </ul>
    </section>
  );
}
`

const workflowAngularTemplate = "import { Component } from \"@angular/core\";\n\n@Component({\n  selector: \"app-{{ sanitizeName .Name }}\",\n  template: `\n    <section>\n      <h1>{{ .Name }}</h1>\n      <p>{{ .Description }}</p>\n      <ul>\n{{- range .Steps }}\n        <li>{{ .Actor }} - {{ .QueryTemplate }}</li>\n{{- end }}\n      </ul>\n    </section>\n  `\n})\nexport class {{ sanitizeName .Name }}Component {}\n"
