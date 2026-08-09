package codingworkflow

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestASTOperationsParseFileRoutesByExtension(t *testing.T) {
	infra := NewDefaultSharedInfrastructure()
	ops := infra.NewASTOperations()

	tests := []struct {
		name     string
		filePath string
		content  string
	}{
		{
			name:     "go",
			filePath: "main.go",
			content:  "package main\nfunc hello() {}\n",
		},
		{
			name:     "python",
			filePath: "app.py",
			content:  "def hello():\n    return 1\n",
		},
		{
			name:     "javascript",
			filePath: "app.js",
			content:  "function hello() {}\n",
		},
		{
			name:     "typescript",
			filePath: "app.ts",
			content:  "function hello(): void {}\n",
		},
		{
			name:     "java",
			filePath: "App.java",
			content:  "class App { void run() {} }\n",
		},
		{
			name:     "rust",
			filePath: "main.rs",
			content:  "fn hello() {}\n",
		},
		{
			name:     "csharp",
			filePath: "Program.cs",
			content:  "using System; class Program { static void Main() {} }\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, root, err := ops.ParseFile(tc.filePath, []byte(tc.content))
			if err != nil {
				t.Fatalf("ParseFile failed for %s: %v", tc.filePath, err)
			}
			if root == nil {
				t.Fatalf("expected non-nil root for %s", tc.filePath)
			}
		})
	}
}

func TestTreeSitterLanguageResolverSupportsSupportedExtensions(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{name: "go", filePath: "main.go", want: "go"},
		{name: "python", filePath: "app.py", want: "python"},
		{name: "javascript", filePath: "app.js", want: "javascript"},
		{name: "javascript-jsx", filePath: "component.jsx", want: "javascript"},
		{name: "typescript", filePath: "app.ts", want: "typescript"},
		{name: "typescript-tsx", filePath: "component.tsx", want: "typescript"},
		{name: "java", filePath: "App.java", want: "java"},
		{name: "rust", filePath: "main.rs", want: "rust"},
		{name: "csharp", filePath: "Program.cs", want: "csharp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveTreeSitterLanguageAliasFromExtension(filepath.Ext(tc.filePath)); got != tc.want {
				t.Fatalf("resolveTreeSitterLanguageAliasFromExtension(%q) = %q, want %q", tc.filePath, got, tc.want)
			}
		})
	}
}

func TestTreeSitterLanguageResolverSupportsSupportedAliases(t *testing.T) {
	tests := []struct {
		name  string
		alias string
		want  string
	}{
		{name: "go", alias: "go", want: "go"},
		{name: "golang", alias: "golang", want: "go"},
		{name: "python", alias: "python", want: "python"},
		{name: "py", alias: "py", want: "python"},
		{name: "javascript", alias: "javascript", want: "javascript"},
		{name: "js", alias: "js", want: "javascript"},
		{name: "typescript", alias: "typescript", want: "typescript"},
		{name: "ts", alias: "ts", want: "typescript"},
		{name: "java", alias: "java", want: "java"},
		{name: "rust", alias: "rust", want: "rust"},
		{name: "rs", alias: "rs", want: "rust"},
		{name: "csharp", alias: "csharp", want: "csharp"},
		{name: "cs", alias: "cs", want: "csharp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := resolveTreeSitterLanguageForAlias(tc.alias); got != tc.want {
				t.Fatalf("resolveTreeSitterLanguageForAlias(%q) = %q, want %q", tc.alias, got, tc.want)
			}
		})
	}
}

func TestASTOperationsParseFileReturnsErrorForUnsupportedExtension(t *testing.T) {
	infra := NewDefaultSharedInfrastructure()
	ops := infra.NewASTOperations()

	_, _, err := ops.ParseFile("README.md", []byte("# title\n"))
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		t.Fatalf("expected unsupported file type error, got %v", err)
	}
}

func TestTreeSitterParserFallbackSupportsHTMLAndCSS(t *testing.T) {
	tests := []struct {
		name     string
		language string
		content  string
		want     []string
	}{
		{
			name:     "html",
			language: "html",
			content:  "<main><section id=\"hero\"><h1>Welcome</h1></section></main>",
			want:     []string{"main", "section", "h1"},
		},
		{
			name:     "css",
			language: "css",
			content:  ".hero { color: red; } .card { padding: 1rem; }",
			want:     []string{"hero"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := NewTreeSitterParser(tc.language)
			got, err := parser.ParseSymbols(tc.content)
			if err != nil {
				t.Fatalf("ParseSymbols returned error: %v", err)
			}
			for _, want := range tc.want {
				if !containsStringValue(got, want) {
					t.Fatalf("expected %q in symbols %v", want, got)
				}
			}
		})
	}
}

func TestASTOperationsExtractFunctionsAndImportsMultiLanguage(t *testing.T) {
	infra := NewDefaultSharedInfrastructure()
	ops := infra.NewASTOperations()

	tests := []struct {
		name              string
		filePath          string
		content           string
		expectedFunctions []string
		expectedImports   []string
	}{
		{
			name:              "go",
			filePath:          "pkg/main.go",
			content:           "package main\nimport \"fmt\"\nfunc Hello() {}\n",
			expectedFunctions: []string{"Hello"},
			expectedImports:   []string{"fmt"},
		},
		{
			name:              "python",
			filePath:          "pkg/main.py",
			content:           "import os\nfrom sys import path\ndef run():\n    return 1\nclass A:\n    def method(self):\n        return 2\n",
			expectedFunctions: []string{"run", "method"},
			expectedImports:   []string{"import os", "from sys import path"},
		},
		{
			name:              "javascript",
			filePath:          "pkg/main.js",
			content:           "import React from \"react\";\nfunction hello() {}\nclass C { method() {} }\n",
			expectedFunctions: []string{"hello", "method"},
			expectedImports:   []string{"react"},
		},
		{
			name:              "typescript",
			filePath:          "pkg/main.ts",
			content:           "import { readFile } from \"fs\";\nfunction main(): void {}\n",
			expectedFunctions: []string{"main"},
			expectedImports:   []string{"fs"},
		},
		{
			name:              "java",
			filePath:          "pkg/App.java",
			content:           "import java.util.List;\nclass App { void run() {} }\n",
			expectedFunctions: []string{"run"},
			expectedImports:   []string{"java.util.List"},
		},
		{
			name:              "rust",
			filePath:          "pkg/main.rs",
			content:           "use std::fmt;\nfn run() {}\nimpl Thing { fn method(&self) {} }\n",
			expectedFunctions: []string{"run", "method"},
			expectedImports:   []string{"use std::fmt;"},
		},
		{
			name:              "csharp",
			filePath:          "pkg/Program.cs",
			content:           "using System;\nclass Program { public void Run() {} }\n",
			expectedFunctions: []string{"Run"},
			expectedImports:   []string{"System"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, root, err := ops.ParseFile(tc.filePath, []byte(tc.content))
			if err != nil {
				t.Fatalf("ParseFile failed for %s: %v", tc.filePath, err)
			}

			functions := ops.ExtractFunctions(root, []byte(tc.content))
			for _, want := range tc.expectedFunctions {
				if !containsStringValue(functions, want) {
					t.Fatalf("expected function %q in %v", want, functions)
				}
			}

			imports := ops.ExtractImports(root, []byte(tc.content))
			for _, want := range tc.expectedImports {
				if !containsImport(imports, want) {
					t.Fatalf("expected import containing %q in %v", want, imports)
				}
			}
		})
	}
}

func containsStringValue(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func containsImport(values []string, expected string) bool {
	expectedLower := strings.ToLower(expected)
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), expectedLower) {
			return true
		}
	}
	return false
}
