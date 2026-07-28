package codingworkflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ts "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	// TODO: Add these imports when needed:
	// "github.com/smacker/go-tree-sitter/python"
	// "github.com/smacker/go-tree-sitter/java"
	// "github.com/smacker/go-tree-sitter/rust"
	// "github.com/smacker/go-tree-sitter/c_sharp"
	// "github.com/smacker/go-tree-sitter/javascript"
	// "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// CodeArtifactExtractor extracts code artifacts from modified files using AST analysis.
type CodeArtifactExtractor struct {
	grammarRegistry *GrammarRegistry
}

// NewCodeArtifactExtractor creates a new code artifact extractor.
func NewCodeArtifactExtractor(grammarRegistry *GrammarRegistry) *CodeArtifactExtractor {
	return &CodeArtifactExtractor{
		grammarRegistry: grammarRegistry,
	}
}

// ExtractFromModifiedFiles extracts code artifacts from a list of modified files.
func (cae *CodeArtifactExtractor) ExtractFromModifiedFiles(files []ModifiedFileRecord, workingDir string) ([]CodeArtifact, error) {
	artifacts := []CodeArtifact{}

	for _, file := range files {
		fullPath := filepath.Join(workingDir, file.FilePath)

		// Read file content
		content, err := os.ReadFile(fullPath)
		if err != nil {
			// File might not exist (deleted or created), skip
			continue
		}

		// Extract artifact via AST
		artifact, err := cae.extractFromFile(file.FilePath, content)
		if err != nil {
			// Fallback to path-based inference
			artifact = cae.inferFromPath(file.FilePath)
		}

		if artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}

	return artifacts, nil
}

// extractFromFile extracts code artifact from a single file using AST.
func (cae *CodeArtifactExtractor) extractFromFile(filePath string, content []byte) (*CodeArtifact, error) {
	// Determine language from file extension
	lang := cae.getLanguageFromPath(filePath)
	if lang == nil {
		return nil, fmt.Errorf("unsupported file type: %s", filePath)
	}

	// Parse with tree-sitter
	parser := ts.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}
	defer tree.Close()

	root := tree.RootNode()

	artifact := &CodeArtifact{
		File:      filePath,
		Functions: []string{},
		Types:     []string{},
	}

	// Extract package/namespace
	artifact.Package = cae.extractPackageName(root, content)
	artifact.Namespace = cae.inferNamespace(filePath)

	// Extract functions/methods
	artifact.Functions = cae.extractFunctions(root, content)

	// Extract types (structs, interfaces, classes)
	artifact.Types = cae.extractTypes(root, content)

	return artifact, nil
}

// getLanguageFromPath returns the tree-sitter language for a file path.
func (cae *CodeArtifactExtractor) getLanguageFromPath(filePath string) *ts.Language {
	ext := filepath.Ext(filePath)

	switch ext {
	case ".go":
		return golang.GetLanguage()
	// Multi-language support - currently only Go is enabled
	// To enable other languages, add the tree-sitter dependencies to go.mod:
	//   go get github.com/smacker/go-tree-sitter/python
	//   go get github.com/smacker/go-tree-sitter/java
	//   go get github.com/smacker/go-tree-sitter/rust
	//   go get github.com/smacker/go-tree-sitter/c_sharp
	// Then uncomment the corresponding cases below:
	//
	// case ".py":
	//     return python.GetLanguage()
	// case ".java":
	//     return java.GetLanguage()
	// case ".rs":
	//     return rust.GetLanguage()
	// case ".cs":
	//     return c_sharp.GetLanguage()
	// case ".js", ".jsx":
	//     return javascript.GetLanguage()
	// case ".ts", ".tsx":
	//     return typescript.GetLanguage()
	default:
		return nil
	}
}

// extractPackageName extracts the package/namespace name from AST.
// Supports multiple languages with language-specific queries.
func (cae *CodeArtifactExtractor) extractPackageName(root *ts.Node, content []byte) string {
	// Determine language from root node type
	rootType := root.Type()

	var query string
	var lang *ts.Language

	// Language-specific package extraction
	if strings.Contains(rootType, "source_file") {
		// Go: package declaration
		query = `(package_clause (package_identifier) @package)`
		lang = golang.GetLanguage()
	}
	// TODO: Add language-specific queries when parsers are available:
	//
	// Python: module docstring or __init__.py inference
	// query = `(module) @module`
	//
	// Java: package declaration
	// query = `(package_declaration (scoped_identifier) @package)`
	//
	// Rust: crate name from Cargo.toml or mod declaration
	// query = `(mod_item name: (identifier) @mod)`
	//
	// C#: namespace declaration
	// query = `(namespace_declaration name: (qualified_name) @namespace)`

	if query == "" || lang == nil {
		return ""
	}

	q, err := ts.NewQuery([]byte(query), lang)
	if err != nil {
		return ""
	}
	defer q.Close()

	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(q, root)

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			return capture.Node.Content(content)
		}
	}

	return ""
}

// extractFunctions extracts function/method names from AST.
// Supports multiple languages with language-specific queries.
func (cae *CodeArtifactExtractor) extractFunctions(root *ts.Node, content []byte) []string {
	functions := []string{}
	rootType := root.Type()

	var queries []string
	var lang *ts.Language

	// Go-specific function and method extraction
	if strings.Contains(rootType, "source_file") {
		queries = []string{
			`(function_declaration name: (identifier) @function)`,
			`(method_declaration name: (field_identifier) @method)`,
		}
		lang = golang.GetLanguage()
	}

	// TODO: Add language-specific queries when parsers are available:
	//
	// Python:
	// queries = []string{
	//     `(function_definition name: (identifier) @function)`,
	//     `(class_definition body: (block (function_definition name: (identifier) @method)))`,
	// }
	//
	// Java:
	// queries = []string{
	//     `(method_declaration name: (identifier) @method)`,
	//     `(constructor_declaration name: (identifier) @constructor)`,
	// }
	//
	// Rust:
	// queries = []string{
	//     `(function_item name: (identifier) @function)`,
	//     `(impl_item (function_item name: (identifier) @method))`,
	// }
	//
	// C#:
	// queries = []string{
	//     `(method_declaration name: (identifier) @method)`,
	//     `(constructor_declaration name: (identifier) @constructor)`,
	// }

	if len(queries) == 0 || lang == nil {
		return functions
	}

	for _, queryStr := range queries {
		q, err := ts.NewQuery([]byte(queryStr), lang)
		if err != nil {
			continue
		}

		cursor := ts.NewQueryCursor()
		cursor.Exec(q, root)

		for {
			match, ok := cursor.NextMatch()
			if !ok {
				break
			}

			for _, capture := range match.Captures {
				name := capture.Node.Content(content)
				functions = append(functions, name)
			}
		}

		cursor.Close()
		q.Close()
	}

	return functions
}

// extractTypes extracts type/class/struct/interface names from AST.
// Supports multiple languages with language-specific queries.
func (cae *CodeArtifactExtractor) extractTypes(root *ts.Node, content []byte) []string {
	types := []string{}
	rootType := root.Type()

	var queries []string
	var lang *ts.Language

	// Go: type declarations (structs, interfaces, aliases)
	if strings.Contains(rootType, "source_file") {
		queries = []string{
			`(type_declaration (type_spec name: (type_identifier) @type))`,
		}
		lang = golang.GetLanguage()
	}

	// TODO: Add language-specific queries when parsers are available:
	//
	// Python:
	// queries = []string{
	//     `(class_definition name: (identifier) @class)`,
	// }
	//
	// Java:
	// queries = []string{
	//     `(class_declaration name: (identifier) @class)`,
	//     `(interface_declaration name: (identifier) @interface)`,
	//     `(enum_declaration name: (identifier) @enum)`,
	// }
	//
	// Rust:
	// queries = []string{
	//     `(struct_item name: (type_identifier) @struct)`,
	//     `(enum_item name: (type_identifier) @enum)`,
	//     `(trait_item name: (type_identifier) @trait)`,
	// }
	//
	// C#:
	// queries = []string{
	//     `(class_declaration name: (identifier) @class)`,
	//     `(interface_declaration name: (identifier) @interface)`,
	//     `(struct_declaration name: (identifier) @struct)`,
	//     `(enum_declaration name: (identifier) @enum)`,
	// }

	if len(queries) == 0 || lang == nil {
		return types
	}

	for _, queryStr := range queries {
		q, err := ts.NewQuery([]byte(queryStr), lang)
		if err != nil {
			continue
		}

		cursor := ts.NewQueryCursor()
		cursor.Exec(q, root)

		for {
			match, ok := cursor.NextMatch()
			if !ok {
				break
			}

			for _, capture := range match.Captures {
				name := capture.Node.Content(content)
				types = append(types, name)
			}
		}

		cursor.Close()
		q.Close()
	}

	return types
}

// inferFromPath creates a basic artifact from file path (fallback).
func (cae *CodeArtifactExtractor) inferFromPath(filePath string) *CodeArtifact {
	parts := strings.Split(filePath, "/")

	artifact := &CodeArtifact{
		File:      filePath,
		Functions: []string{},
		Types:     []string{},
	}

	// Infer package from first directory
	if len(parts) > 1 {
		artifact.Package = parts[0]
	}

	artifact.Namespace = cae.inferNamespace(filePath)

	return artifact
}

// inferNamespace infers namespace from file path.
func (cae *CodeArtifactExtractor) inferNamespace(filePath string) string {
	// For Go: extract from path
	// Example: "lib/sop/ai/agent/file.go" -> "github.com/repo/lib/sop/ai/agent"

	parts := strings.Split(filePath, "/")
	if len(parts) == 0 {
		return ""
	}

	// Remove filename
	parts = parts[:len(parts)-1]

	// TODO: Could look at go.mod to determine full module path
	// For now, just use the relative path
	return strings.Join(parts, "/")
}

// GenerateCategoryPath generates a Space category path from code artifacts.
func (cae *CodeArtifactExtractor) GenerateCategoryPath(artifacts []CodeArtifact) []string {
	if len(artifacts) == 0 {
		return []string{"Uncategorized"}
	}

	// Infer application component from package patterns
	component := cae.inferApplicationComponent(artifacts)
	subComponent := cae.inferSubComponent(artifacts)

	if subComponent != "" {
		return []string{component, subComponent}
	}
	return []string{component}
}

// inferApplicationComponent infers the application component from artifacts.
func (cae *CodeArtifactExtractor) inferApplicationComponent(artifacts []CodeArtifact) string {
	// Collect all package names
	packages := []string{}
	for _, a := range artifacts {
		if a.Package != "" {
			packages = append(packages, a.Package)
		}
	}

	if len(packages) == 0 {
		return "Core System"
	}

	// Use first package as primary indicator
	pkg := packages[0]

	// Map common package patterns to components
	componentMappings := map[string]string{
		"auth":       "Authentication System",
		"middleware": "Authentication System",
		"db":         "Database Layer",
		"database":   "Database Layer",
		"parser":     "Parser Engine",
		"tokenizer":  "Parser Engine",
		"ast":        "Parser Engine",
		"api":        "API Routes",
		"handler":    "API Routes",
		"route":      "API Routes",
		"cache":      "Caching Layer",
		"config":     "Configuration",
		"test":       "Testing Infrastructure",
		"util":       "Utilities",
		"helper":     "Utilities",
	}

	// Check for pattern matches
	pkgLower := strings.ToLower(pkg)
	for pattern, component := range componentMappings {
		if strings.Contains(pkgLower, pattern) {
			return component
		}
	}

	// Default: use package name as component
	return titleCasePackage(pkg)
}

// inferSubComponent infers the sub-component from artifacts.
func (cae *CodeArtifactExtractor) inferSubComponent(artifacts []CodeArtifact) string {
	if len(artifacts) == 0 {
		return ""
	}

	// For now, use package name if available
	if artifacts[0].Package != "" {
		pkg := artifacts[0].Package

		// Sub-component mappings
		subMappings := map[string]string{
			"middleware": "Middleware",
			"service":    "Service",
			"handler":    "Handlers",
			"route":      "Routes",
			"model":      "Models",
			"controller": "Controllers",
			"tokenizer":  "Tokenizer",
			"pool":       "Connection Pool",
			"query":      "Query Builder",
		}

		pkgLower := strings.ToLower(pkg)
		for pattern, sub := range subMappings {
			if strings.Contains(pkgLower, pattern) {
				return sub
			}
		}
	}

	return ""
}

// titleCasePackage converts a package name to title case.
func titleCasePackage(pkg string) string {
	words := strings.Split(pkg, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	result := strings.Join(words, " ")
	return result + " Component"
}
