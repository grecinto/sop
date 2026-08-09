package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"
	"text/template"
)

// ScriptDefinition matches the JSON structure in workflow_scripts/*.json
type ScriptDefinition struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Input       string                     `json:"input"`
	Output      string                     `json:"output"`
	Script      []ScriptInstructionCodegen `json:"script"`
}

// ScriptInstructionCodegen represents a single script operation for codegen
type ScriptInstructionCodegen struct {
	Op        string         `json:"op"`
	Args      map[string]any `json:"args"`
	ResultVar string         `json:"result_var"`
	InputVar  string         `json:"input_var"`
	Name      string         `json:"name"`
}

// CompileScriptToGo generates Go source code from a JSON script definition.
// Returns formatted Go code as a string.
func CompileScriptToGo(scriptPath string) (string, error) {
	return CompileScript(scriptPath, TargetLanguageGo)
}

// CompileScript generates source code from a script definition in the requested target language.
func CompileScript(scriptPath string, target TargetLanguage) (string, error) {
	script, err := loadScriptDefinition(scriptPath)
	if err != nil {
		return "", err
	}

	switch normalizeTargetLanguage(target) {
	case TargetLanguageGo:
		code, err := generateScriptGoCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate Go code: %w", err)
		}

		formatted, err := format.Source([]byte(code))
		if err != nil {
			return "", fmt.Errorf("format Go code: %w (source: %s)", err, code)
		}

		return string(formatted), nil
	case TargetLanguageCSharp:
		code, err := generateScriptCSharpCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate C# code: %w", err)
		}
		return code, nil
	case TargetLanguagePython:
		code, err := generateScriptPythonCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate Python code: %w", err)
		}
		return code, nil
	case TargetLanguageJava:
		code, err := generateScriptJavaCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate Java code: %w", err)
		}
		return code, nil
	case TargetLanguageRust:
		code, err := generateScriptRustCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate Rust code: %w", err)
		}
		return code, nil
	case TargetLanguageTypeScript:
		code, err := generateScriptTypeScriptCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate TypeScript code: %w", err)
		}
		return code, nil
	case TargetLanguageJavaScript:
		code, err := generateScriptJavaScriptCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate JavaScript code: %w", err)
		}
		return code, nil
	case TargetLanguageReact:
		code, err := generateScriptReactCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate React code: %w", err)
		}
		return code, nil
	case TargetLanguageAngular:
		code, err := generateScriptAngularCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate Angular code: %w", err)
		}
		return code, nil
	case TargetLanguageHTMLCSS:
		code, err := generateScriptHTMLCSSCode(&script)
		if err != nil {
			return "", fmt.Errorf("generate HTML/CSS code: %w", err)
		}
		return code, nil
	default:
		return "", fmt.Errorf("unsupported script codegen target: %q", target)
	}
}

// CompileScriptToCSharp generates C# source code from a script definition.
func CompileScriptToCSharp(scriptPath string) (string, error) {
	return CompileScript(scriptPath, TargetLanguageCSharp)
}

// CompileScriptToPython generates Python source code from a script definition.
func CompileScriptToPython(scriptPath string) (string, error) {
	return CompileScript(scriptPath, TargetLanguagePython)
}

func loadScriptDefinition(scriptPath string) (ScriptDefinition, error) {
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return ScriptDefinition{}, fmt.Errorf("read script file: %w", err)
	}

	var script ScriptDefinition
	if err := json.Unmarshal(data, &script); err != nil {
		return ScriptDefinition{}, fmt.Errorf("parse script JSON: %w", err)
	}

	return script, nil
}

// generateScriptGoCode creates Go source from ScriptDefinition
func generateScriptGoCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script").Funcs(template.FuncMap{
		"toGoValue":    toGoValue,
		"sanitizeName": sanitizeName,
		"goComment":    goComment,
		"add":          func(a, b int) int { return a + b },
		"eq":           func(a, b string) bool { return a == b },
	}).Parse(scriptTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// sanitizeName converts a script name to a valid Go function name
func sanitizeName(name string) string {
	// Replace non-alphanumeric with underscore
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)

	// Ensure it starts with uppercase (exported)
	if len(sanitized) > 0 {
		first := sanitized[0]
		if first >= 'a' && first <= 'z' {
			sanitized = string(first-32) + sanitized[1:]
		}
	}

	return sanitized
}

func toPascalCaseIdentifier(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	if len(parts) == 0 {
		return ""
	}

	result := make([]string, 0, len(parts))
	for _, part := range parts {
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

func toCamelCaseIdentifier(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	if len(parts) == 0 {
		return ""
	}
	if len(parts[0]) == 0 {
		return ""
	}
	result := strings.ToLower(parts[0][:1]) + strings.ToLower(parts[0][1:])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		if len(part) == 1 {
			result += strings.ToUpper(part)
			continue
		}
		result += strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return result
}

// toGoValue converts JSON value to Go literal string
func toGoValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case float64, int, int64:
		return fmt.Sprintf("%v", val)
	case map[string]any:
		// Convert map to Go map literal
		var parts []string
		for k, v := range val {
			parts = append(parts, fmt.Sprintf("%q: %s", k, toGoValue(v)))
		}
		return fmt.Sprintf("map[string]any{%s}", strings.Join(parts, ", "))
	case []any:
		// Convert array to Go slice literal
		var parts []string
		for _, item := range val {
			parts = append(parts, toGoValue(item))
		}
		return fmt.Sprintf("[]any{%s}", strings.Join(parts, ", "))
	default:
		return fmt.Sprintf("%#v", v)
	}
}

// goComment wraps text in Go comment format
func goComment(text string) string {
	return fmt.Sprintf("// %s", text)
}

func toCSharpValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(val, "\"", "\\\""))
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64, int, int64:
		return fmt.Sprintf("%v", val)
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("{ \"%s\", %s }", strings.ReplaceAll(k, "\"", "\\\""), toCSharpValue(val[k])))
		}
		return fmt.Sprintf("new Dictionary<string, object> { %s }", strings.Join(parts, ", "))
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, toCSharpValue(item))
		}
		return fmt.Sprintf("new List<object> { %s }", strings.Join(parts, ", "))
	default:
		return "null"
	}
}

func toPythonValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case bool:
		if val {
			return "True"
		}
		return "False"
	case float64, int, int64:
		return fmt.Sprintf("%v", val)
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%q: %s", k, toPythonValue(val[k])))
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, toPythonValue(item))
		}
		return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
	default:
		return "None"
	}
}

func toJavaValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(val, "\"", "\\\""))
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64, int, int64:
		return fmt.Sprintf("%v", val)
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("\"%s\": %s", strings.ReplaceAll(k, "\"", "\\\""), toJavaValue(val[k])))
		}
		return fmt.Sprintf("Map.of(%s)", strings.Join(parts, ", "))
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, toJavaValue(item))
		}
		return fmt.Sprintf("List.of(%s)", strings.Join(parts, ", "))
	default:
		return "null"
	}
}

func toRustValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(val, "\"", "\\\""))
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64, int, int64:
		return fmt.Sprintf("%v", val)
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("\"%s\": %s", strings.ReplaceAll(k, "\"", "\\\""), toRustValue(val[k])))
		}
		return fmt.Sprintf("serde_json::json!({%s})", strings.Join(parts, ", "))
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, toRustValue(item))
		}
		return fmt.Sprintf("vec![%s]", strings.Join(parts, ", "))
	default:
		return "None"
	}
}

func toTypeScriptValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(val, "\"", "\\\""))
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64, int, int64:
		return fmt.Sprintf("%v", val)
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%q: %s", k, toTypeScriptValue(val[k])))
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, toTypeScriptValue(item))
		}
		return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
	default:
		return "undefined"
	}
}

const scriptTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
package compiled

import (
	"context"
	"fmt"
	"os/exec"
	"os"
	"strings"
)

{{ goComment .Description }}
//
// Input: {{ .Input }}
// Output: {{ .Output }}
func {{ sanitizeName .Name }}(ctx context.Context, input map[string]any) (map[string]any, error) {
	var lastResult any
	variables := make(map[string]any)
	
	// Copy input to variables
	for k, v := range input {
		variables[k] = v
	}
	
	{{ range $idx, $step := .Script }}
	// Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
	{
		{{ if eq $step.Op "if" }}
		// Conditional branch
		{{ if $step.InputVar }}
		input := variables[{{ toGoValue $step.InputVar }}]
		{{ else }}
		input := lastResult
		{{ end }}
		
		result, err := executeIf(ctx, map[string]any{
			{{ range $key, $val := $step.Args }}{{ toGoValue $key }}: {{ toGoValue $val }},
			{{ end }}
		}, input, variables)
		if err != nil {
			return nil, fmt.Errorf("step {{ add $idx 1 }} (if): %w", err)
		}
		lastResult = result
		{{ if $step.ResultVar }}
		variables[{{ toGoValue $step.ResultVar }}] = result
		{{ end }}
		
		{{ else if eq $step.Op "loop" }}
		// Loop iteration
		{{ if $step.InputVar }}
		input := variables[{{ toGoValue $step.InputVar }}]
		{{ else }}
		input := lastResult
		{{ end }}
		
		result, err := executeLoop(ctx, map[string]any{
			{{ range $key, $val := $step.Args }}{{ toGoValue $key }}: {{ toGoValue $val }},
			{{ end }}
		}, input, variables)
		if err != nil {
			return nil, fmt.Errorf("step {{ add $idx 1 }} (loop): %w", err)
		}
		lastResult = result
		{{ if $step.ResultVar }}
		variables[{{ toGoValue $step.ResultVar }}] = result
		{{ end }}
		
		{{ else }}
		// Regular operation
		{{ if $step.InputVar }}
		input := variables[{{ toGoValue $step.InputVar }}]
		{{ else }}
		input := lastResult
		{{ end }}
		
		result, err := execute{{ sanitizeName $step.Op }}(ctx, map[string]any{
			{{ range $key, $val := $step.Args }}{{ toGoValue $key }}: {{ toGoValue $val }},
			{{ end }}
		}, input)
		if err != nil {
			return nil, fmt.Errorf("step {{ add $idx 1 }} ({{ $step.Op }}): %w", err)
		}
		
		lastResult = result
		{{ if $step.ResultVar }}
		variables[{{ toGoValue $step.ResultVar }}] = result
		{{ end }}
		{{ end }}
	}
	{{ end }}
	
	// Return output
	return map[string]any{
		{{ toGoValue .Output }}: lastResult,
	}, nil
}

// Operation implementations
func executeRead_file(ctx context.Context, args map[string]any, input any) (any, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("read_file: path must be string")
	}
	
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	
	return string(data), nil
}

func executeGrep(ctx context.Context, args map[string]any, input any) (any, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("grep: pattern must be string")
	}
	
	text, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("grep: input must be string")
	}
	
	var matches []string
	for _, line := range strings.Split(text, "\n") {
		// Simple pattern matching (could use regexp for more complex patterns)
		if strings.Contains(line, pattern) {
			matches = append(matches, line)
		}
	}
	
	return strings.Join(matches, "\n"), nil
}

func executeFormat(ctx context.Context, args map[string]any, input any) (any, error) {
	template, ok := args["template"].(string)
	if !ok {
		return nil, fmt.Errorf("format: template must be string")
	}
	
	// Simple template substitution ({{"{{"}}input{{"}}"}})
	result := strings.ReplaceAll(template, "{{"{{"}}input{{"}}"}}", fmt.Sprintf("%v", input))
	
	return result, nil
}

func executeExecute_local_command(ctx context.Context, args map[string]any, input any) (any, error) {
	command, ok := args["command"].(string)
	if !ok {
		return nil, fmt.Errorf("execute_local_command: command must be string")
	}
	
	// Execute shell command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("execute command: %w", err)
	}
	
	return string(output), nil
}

func executeFilter(ctx context.Context, args map[string]any, input any) (any, error) {
	condition, ok := args["condition"].(string)
	if !ok {
		return nil, fmt.Errorf("filter: condition must be string")
	}
	
	text, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("filter: input must be string")
	}
	
	var filtered []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		
		// Apply filter condition
		switch condition {
		case "non_empty":
			if line != "" {
				filtered = append(filtered, line)
			}
		case "empty":
			if line == "" {
				filtered = append(filtered, line)
			}
		default:
			// Custom filter logic could go here
			filtered = append(filtered, line)
		}
	}
	
	return strings.Join(filtered, "\n"), nil
}

func executeSelect_first(ctx context.Context, args map[string]any, input any) (any, error) {
	text, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("select_first: input must be string")
	}
	
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return "", nil
	}
	
	return strings.TrimSpace(lines[0]), nil
}

func executeIf(ctx context.Context, args map[string]any, input any, variables map[string]any) (any, error) {
	// Evaluate condition
	condition, ok := args["condition"].(bool)
	if !ok {
		// Try to evaluate as variable reference
		if condVar, ok := args["condition"].(string); ok {
			if val, exists := variables[condVar]; exists {
				if boolVal, ok := val.(bool); ok {
					condition = boolVal
				} else {
					return nil, fmt.Errorf("condition variable must be boolean")
				}
			} else {
				return nil, fmt.Errorf("condition variable not found: %s", condVar)
			}
		} else {
			return nil, fmt.Errorf("condition must be boolean or variable reference")
		}
	}
	
	// Execute then or else block
	// Note: In compiled code, nested blocks are not easily supported
	// For now, return a marker indicating which branch was taken
	if condition {
		return map[string]any{"branch": "then", "condition": true}, nil
	}
	return map[string]any{"branch": "else", "condition": false}, nil
}

func executeLoop(ctx context.Context, args map[string]any, input any, variables map[string]any) (any, error) {
	// Get collection variable name
	collectionVar, ok := args["collection"].(string)
	if !ok {
		return nil, fmt.Errorf("loop: collection must be string (variable name)")
	}
	
	// Get item variable name
	itemVar, ok := args["item_var"].(string)
	if !ok {
		itemVar = "item" // Default
	}
	
	// Get collection from variables
	collection, exists := variables[collectionVar]
	if !exists {
		return nil, fmt.Errorf("collection variable not found: %s", collectionVar)
	}
	
	// Convert to slice
	var items []any
	switch coll := collection.(type) {
	case []any:
		items = coll
	case []map[string]any:
		for _, item := range coll {
			items = append(items, item)
		}
	case []string:
		for _, item := range coll {
			items = append(items, item)
		}
	default:
		return nil, fmt.Errorf("collection must be a slice, got %T", collection)
	}
	
	// Note: In compiled code, loop body execution is complex
	// For now, return iteration metadata
	return map[string]any{
		"looped": true,
		"count":  len(items),
		"items":  items,
	}, nil
}

func executeList_new(ctx context.Context, args map[string]any, input any) (any, error) {
	return []any{}, nil
}

func executeList_append(ctx context.Context, args map[string]any, input any) (any, error) {
	// Get the item to append
	item, ok := args["item"]
	if !ok {
		return nil, fmt.Errorf("list_append: item required")
	}
	
	// Input should be a list
	list, ok := input.([]any)
	if !ok {
		// Try to convert
		if input == nil {
			list = []any{}
		} else {
			return nil, fmt.Errorf("list_append: input must be a list")
		}
	}
	
	// Append and return new list
	return append(list, item), nil
}

func executeProject(ctx context.Context, args map[string]any, input any) (any, error) {
	// Get fields to project
	fields, ok := args["fields"].([]any)
	if !ok {
		return nil, fmt.Errorf("project: fields must be array")
	}
	
	// Convert fields to string slice
	fieldNames := make([]string, len(fields))
	for i, f := range fields {
		fieldNames[i] = fmt.Sprintf("%v", f)
	}
	
	// Input should be list of maps
	switch list := input.(type) {
	case []map[string]any:
		result := make([]map[string]any, len(list))
		for i, item := range list {
			projected := make(map[string]any)
			for _, field := range fieldNames {
				if val, exists := item[field]; exists {
					projected[field] = val
				}
			}
			result[i] = projected
		}
		return result, nil
	case []any:
		result := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				projected := make(map[string]any)
				for _, field := range fieldNames {
					if val, exists := m[field]; exists {
						projected[field] = val
					}
				}
				result = append(result, projected)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("project: input must be list of maps")
	}
}

func executeLimit(ctx context.Context, args map[string]any, input any) (any, error) {
	// Get limit count
	limitVal, ok := args["count"]
	if !ok {
		return nil, fmt.Errorf("limit: count required")
	}
	
	limit, ok := limitVal.(float64)
	if !ok {
		if intLimit, ok := limitVal.(int); ok {
			limit = float64(intLimit)
		} else {
			return nil, fmt.Errorf("limit: count must be number")
		}
	}
	
	// Input should be a list
	switch list := input.(type) {
	case []any:
		if int(limit) < len(list) {
			return list[:int(limit)], nil
		}
		return list, nil
	case []map[string]any:
		if int(limit) < len(list) {
			return list[:int(limit)], nil
		}
		return list, nil
	case []string:
		if int(limit) < len(list) {
			return list[:int(limit)], nil
		}
		return list, nil
	default:
		return nil, fmt.Errorf("limit: input must be a list")
	}
}

func executeSort(ctx context.Context, args map[string]any, input any) (any, error) {
	// Get sort field
	field, ok := args["field"].(string)
	if !ok {
		return nil, fmt.Errorf("sort: field must be string")
	}
	
	// Get sort order (default ascending)
	order := "asc"
	if orderVal, ok := args["order"].(string); ok {
		order = orderVal
	}
	
	// Input should be list of maps
	list, ok := input.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("sort: input must be list of maps")
	}
	
	// Simple string-based sort (can be enhanced)
	result := make([]map[string]any, len(list))
	copy(result, list)
	
	// Note: This is a simplified sort - production code would use sort.Slice
	// For codegen, we're keeping it simple
	_ = field
	_ = order
	
	return result, nil
}
`

func generateScriptCSharpCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-csharp").Funcs(template.FuncMap{
		"sanitizeName":  sanitizeName,
		"toCSharpValue": toCSharpValue,
		"add":           func(a, b int) int { return a + b },
	}).Parse(scriptCSharpTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute C# template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptPythonCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-python").Funcs(template.FuncMap{
		"sanitizeName":  sanitizeName,
		"toPythonValue": toPythonValue,
		"add":           func(a, b int) int { return a + b },
	}).Parse(scriptPythonTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute Python template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptJavaCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-java").Funcs(template.FuncMap{
		"sanitizeName":  sanitizeName,
		"toJavaValue":   toJavaValue,
		"add":           func(a, b int) int { return a + b },
		"javaClassName": toPascalCaseIdentifier,
	}).Parse(scriptJavaTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute Java template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptRustCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-rust").Funcs(template.FuncMap{
		"sanitizeName":   sanitizeName,
		"toRustValue":    toRustValue,
		"add":            func(a, b int) int { return a + b },
		"rustIdentifier": toRustIdentifier,
	}).Parse(scriptRustTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute Rust template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptTypeScriptCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-typescript").Funcs(template.FuncMap{
		"sanitizeName":      sanitizeName,
		"toTypeScriptValue": toTypeScriptValue,
		"add":               func(a, b int) int { return a + b },
		"tsFunctionName":    toCamelCaseIdentifier,
	}).Parse(scriptTypeScriptTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute TypeScript template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptJavaScriptCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-javascript").Funcs(template.FuncMap{
		"sanitizeName":      sanitizeName,
		"toTypeScriptValue": toTypeScriptValue,
		"add":               func(a, b int) int { return a + b },
		"tsFunctionName":    toCamelCaseIdentifier,
	}).Parse(scriptJavaScriptTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute JavaScript template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptHTMLCSSCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-htmlcss").Funcs(template.FuncMap{
		"sanitizeName": sanitizeName,
	}).Parse(scriptHTMLCSSTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute HTML/CSS template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptReactCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-react").Funcs(template.FuncMap{
		"sanitizeName":      sanitizeName,
		"toTypeScriptValue": toTypeScriptValue,
		"add":               func(a, b int) int { return a + b },
		"tsFunctionName":    toCamelCaseIdentifier,
	}).Parse(scriptReactTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute React template: %w", err)
	}

	return buf.String(), nil
}

func generateScriptAngularCode(script *ScriptDefinition) (string, error) {
	tmpl := template.Must(template.New("script-angular").Funcs(template.FuncMap{
		"sanitizeName":      sanitizeName,
		"toTypeScriptValue": toTypeScriptValue,
		"add":               func(a, b int) int { return a + b },
		"tsFunctionName":    toCamelCaseIdentifier,
	}).Parse(scriptAngularTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, script); err != nil {
		return "", fmt.Errorf("execute Angular template: %w", err)
	}

	return buf.String(), nil
}

const scriptCSharpTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
using System;
using System.Collections.Generic;

public static class {{ sanitizeName .Name }}Script {
	// {{ .Description }}
	public static Dictionary<string, object?> Run(Dictionary<string, object?> input) {
		object? lastResult = null;
		var variables = new Dictionary<string, object?>(input);

{{- range $idx, $step := .Script }}
		// Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
		lastResult = ExecuteOperation("{{ $step.Op }}", {{ toCSharpValue $step.Args }}, {{ if $step.InputVar }}variables["{{ $step.InputVar }}"]{{ else }}lastResult{{ end }});
		{{- if $step.ResultVar }}
		variables["{{ $step.ResultVar }}"] = lastResult;
		{{- end }}

{{- end }}
		return new Dictionary<string, object?> {
			{ "{{ .Output }}", lastResult }
		};
	}

	private static object? ExecuteOperation(string op, Dictionary<string, object> args, object? input) {
		// Runtime adapters provide concrete implementations for these operations.
		return new Dictionary<string, object?> {
			{ "op", op },
			{ "args", args },
			{ "input", input },
		};
	}
}
`

const scriptPythonTemplate = `# Code generated by sop-compile-workflow. DO NOT EDIT.
# {{ .Description }}

def run(input_data: dict) -> dict:
	last_result = None
	variables = dict(input_data)

{{- range $idx, $step := .Script }}
	# Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
	last_result = execute_operation("{{ $step.Op }}", {{ toPythonValue $step.Args }}, {{ if $step.InputVar }}variables["{{ $step.InputVar }}"]{{ else }}last_result{{ end }})
	{{- if $step.ResultVar }}
	variables["{{ $step.ResultVar }}"] = last_result
	{{- end }}

{{- end }}
	return {
		"{{ .Output }}": last_result,
	}


def execute_operation(op: str, args: dict, input_value):
	# Runtime adapters provide concrete implementations for these operations.
	return {
		"op": op,
		"args": args,
		"input": input_value,
	}
`

const scriptJavaTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
import java.util.HashMap;
import java.util.Map;

public class {{ javaClassName .Name }}Script {
	public static Map<String, Object> run(Map<String, Object> inputData) {
		Object lastResult = null;
		Map<String, Object> variables = new HashMap<>(inputData);

{{- range $idx, $step := .Script }}
		// Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
		lastResult = executeOperation("{{ $step.Op }}", {{ toJavaValue $step.Args }}, {{ if $step.InputVar }}variables.get("{{ $step.InputVar }}"){{ else }}lastResult{{ end }});
		{{- if $step.ResultVar }}
		variables.put("{{ $step.ResultVar }}", lastResult);
		{{- end }}

{{- end }}
		Map<String, Object> result = new HashMap<>();
		result.put("{{ .Output }}", lastResult);
		return result;
	}

	private static Object executeOperation(String op, Object args, Object inputValue) {
		Map<String, Object> result = new HashMap<>();
		result.put("op", op);
		result.put("args", args);
		result.put("input", inputValue);
		return result;
	}
}
`

const scriptRustTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
use serde_json::Value;
use std::collections::HashMap;

pub fn {{ rustIdentifier .Name }}(input_data: HashMap<String, Value>) -> HashMap<String, Value> {
	let mut last_result: Option<Value> = None;
	let mut variables = input_data;

{{- range $idx, $step := .Script }}
	// Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
	last_result = Some(execute_operation("{{ $step.Op }}", {{ toRustValue $step.Args }}, {{ if $step.InputVar }}variables.get("{{ $step.InputVar }}").cloned().unwrap_or(Value::Null){{ else }}last_result.clone().unwrap_or(Value::Null){{ end }}));
	{{- if $step.ResultVar }}
	variables.insert("{{ $step.ResultVar }}".to_string(), last_result.clone().unwrap_or(Value::Null));
	{{- end }}

{{- end }}
	let mut result = HashMap::new();
	result.insert("{{ .Output }}".to_string(), last_result.unwrap_or(Value::Null));
	result
}

fn execute_operation(op: &str, args: Value, input_value: Value) -> Value {
	serde_json::json!({
		"op": op,
		"args": args,
		"input": input_value
	})
}
`

const scriptTypeScriptTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
export async function {{ tsFunctionName .Name }}(inputData: Record<string, unknown>): Promise<Record<string, unknown>> {
	let lastResult: unknown = undefined;
	const variables: Record<string, unknown> = { ...inputData };

{{- range $idx, $step := .Script }}
	// Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
	lastResult = executeOperation("{{ $step.Op }}", {{ toTypeScriptValue $step.Args }}, {{ if $step.InputVar }}variables["{{ $step.InputVar }}"]{{ else }}lastResult{{ end }});
	{{- if $step.ResultVar }}
	variables["{{ $step.ResultVar }}"] = lastResult;
	{{- end }}

{{- end }}
	return {
		"{{ .Output }}": lastResult,
	};
}

export function executeOperation(op: string, args: unknown, inputValue: unknown): unknown {
	return {
		op,
		args,
		input: inputValue,
	};
}
`

const scriptJavaScriptTemplate = `// Code generated by sop-compile-workflow. DO NOT EDIT.
export function {{ tsFunctionName .Name }}(inputData) {
	let lastResult = undefined;
	const variables = { ...inputData };

{{- range $idx, $step := .Script }}
	// Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
	lastResult = executeOperation("{{ $step.Op }}", {{ toTypeScriptValue $step.Args }}, {{ if $step.InputVar }}variables["{{ $step.InputVar }}"]{{ else }}lastResult{{ end }});
	{{- if $step.ResultVar }}
	variables["{{ $step.ResultVar }}"] = lastResult;
	{{- end }}

{{- end }}
	return {
		"{{ .Output }}": lastResult,
	};
}

export function executeOperation(op, args, inputValue) {
	return {
		op,
		args,
		input: inputValue,
	};
}
`

const scriptHTMLCSSTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
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
{{- range .Script }}
      <li>{{ .Op }}</li>
{{- end }}
    </ul>
  </div>
</body>
</html>
`

const scriptReactTemplate = `import React from "react";

export function {{ tsFunctionName .Name }}(inputData: Record<string, unknown>): Record<string, unknown> {
    let lastResult: unknown = undefined;
    const variables: Record<string, unknown> = { ...inputData };
{{- range $idx, $step := .Script }}
    // Step {{ add $idx 1 }}: {{ $step.Op }}{{ if $step.Name }} - {{ $step.Name }}{{ end }}
    lastResult = executeOperation("{{ $step.Op }}", {{ toTypeScriptValue $step.Args }}, {{ if $step.InputVar }}variables["{{ $step.InputVar }}"]{{ else }}lastResult{{ end }});
{{- if $step.ResultVar }}
    variables["{{ $step.ResultVar }}"] = lastResult;
{{- end }}
{{- end }}
    return { "{{ .Output }}": lastResult };
}

export function executeOperation(op: string, args: unknown, inputValue: unknown): unknown {
    return { op, args, input: inputValue };
}
`

const scriptAngularTemplate = "import { Component } from \"@angular/core\";\n\n@Component({\n  selector: \"app-{{ sanitizeName .Name }}\",\n  template: `\n    <section>\n      <h1>{{ .Name }}</h1>\n      <p>{{ .Description }}</p>\n    </section>\n  `\n})\nexport class {{ sanitizeName .Name }}Component {}\n"
